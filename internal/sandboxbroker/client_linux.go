// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

//go:build linux && (amd64 || arm64)

package sandboxbroker

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

// supervisedFilesystemTransport enforces deadlines independently of protocol I/O. Close
// must cancel, reap and attempt cleanup; repeated Close retries cleanup.
type supervisedFilesystemTransport interface {
	filesystemTransport

	// Wait blocks until the process exits and reports any failure.
	//
	// Returns error when the process exits abnormally.
	Wait() error

	// Close cancels, reaps and attempts cleanup of the process.
	//
	// Returns error when cancellation or cleanup fails.
	Close(ctx context.Context) error
}

// FilesystemClient owns host-side authorisation and one dedicated broker channel. The
// native owner must remain available to retry cleanup if opening or closing this client
// fails.
type FilesystemClient struct {
	// stream is the supervised transport owning the broker process.
	stream supervisedFilesystemTransport

	// connection holds the codec and protocol state machine.
	connection *filesystemConnection

	// budget tracks cumulative reservation quotas.
	budget *FilesystemBudget

	// recovery is the optional recovery authority for journalled mode.
	recovery *LinuxRecoveryAuthority

	// journal is the optional durable write journal.
	journal *LinuxRecoveryJournal

	// nextID is the monotonic call identifier for the next request.
	nextID uint64

	// active is true while an operation holds exclusive protocol access.
	active atomic.Bool

	// closed is true after Close or terminal failure.
	closed atomic.Bool
}

// Execute admits one bounded request without queuing concurrent callers. Any admitted
// failure is terminal; caller cancellation kills and reaps the broker.
//
// Takes payload (json.RawMessage) which is the exact filesystem request copied before
// sending.
//
// Returns a validated response, or an error without partial results.
func (client *FilesystemClient) Execute(ctx context.Context, payload json.RawMessage) (response FilesystemResponse, err error) {
	if client == nil || client.closed.Load() {
		return FilesystemResponse{}, ErrClosed
	}
	if !client.active.CompareAndSwap(false, true) {
		return FilesystemResponse{}, errBusy
	}
	defer client.active.Store(false)
	if ctx == nil {
		return FilesystemResponse{}, errors.Join(sandboxwire.ErrProtocol, client.abortWithoutContext())
	}
	finish := client.watchCancellation(ctx)
	defer func() {
		err = errors.Join(err, finish())
		if err != nil {
			err = errors.Join(err, client.abort(ctx))
			response = FilesystemResponse{Code: "", Data: nil, Entries: nil, Written: 0, Skipped: 0}
		}
	}()
	if err := ctx.Err(); err != nil {
		return FilesystemResponse{}, err
	}
	return client.exchange(ctx, payload)
}

// Close prevents admission and reaps the dedicated process, retaining retry ownership.
// Idle sessions receive a bounded graceful close; active sessions are cancelled.
//
// Returns any shutdown, execution or cleanup error.
func (client *FilesystemClient) Close(ctx context.Context) error {
	if client == nil {
		return nil
	}
	if client.closed.Swap(true) || !client.active.CompareAndSwap(false, true) {
		return client.abort(ctx)
	}
	defer client.active.Store(false)
	client.budget.Close()
	return client.shutdown(ctx, true)
}

// handshake accepts only the expected broker profile and exact empty Ready payload.
//
// Returns an error before any filesystem request on incompatible or malformed peers.
func (client *FilesystemClient) handshake() error {
	if err := client.stream.SetDeadline(time.Now().Add(brokerStartupTimeout)); err != nil {
		return err
	}
	message, err := client.receive()
	if err != nil {
		return err
	}
	var configuration FilesystemConfiguration
	if err := message.DecodePayload(&configuration); err != nil {
		return err
	}
	if message.Kind != sandboxwire.Hello || configuration.Profile != filesystemProfile {
		return sandboxwire.ErrProtocol
	}
	if client.recovery != nil {
		configuration.Profile = JournalledFilesystemProfile
	}
	payload, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	if err := client.send(sandboxwire.Message{Kind: sandboxwire.Configure, ID: 0, Payload: payload}); err != nil {
		return err
	}
	message, err = client.receive()
	if err != nil {
		return err
	}
	if message.Kind != sandboxwire.Ready {
		return sandboxwire.ErrProtocol
	}
	if err := message.DecodePayload(&struct{}{}); err != nil {
		return err
	}
	return client.stream.SetDeadline(time.Now().Add(brokerIdleTimeout))
}

// exchange reserves the worst-case cost before sending one request.
//
// Takes payload (json.RawMessage) which is the caller payload while holding exclusive
// protocol admission.
//
// Returns validated output only after accounting and enforcing the next deadline.
func (client *FilesystemClient) exchange(ctx context.Context, payload json.RawMessage) (response FilesystemResponse, err error) {
	if client.closed.Load() {
		return FilesystemResponse{}, ErrClosed
	}
	if len(payload) > maximumRequestBytes {
		return FilesystemResponse{}, sandboxwire.ErrProtocol
	}
	message := sandboxwire.Message{Kind: sandboxwire.Call, ID: client.nextID, Payload: append(json.RawMessage(nil), payload...)}
	call, err := client.budget.Admit(message)
	if err != nil {
		return FilesystemResponse{}, err
	}
	used := 0
	defer func() { err = errors.Join(err, call.Finish(used)) }()
	if err := client.stream.SetDeadline(time.Now().Add(brokerOperationTimeout)); err != nil {
		return FilesystemResponse{}, err
	}
	message.Kind = sandboxwire.Run
	if err := client.send(message); err != nil {
		return FilesystemResponse{}, err
	}
	response, err = client.receiveFilesystemResult(call)
	if err != nil {
		return FilesystemResponse{}, err
	}
	used = len(response.Data) + len(response.Entries) + response.Written
	client.nextID++
	if client.budget.calls == client.budget.limits.Calls {
		client.closed.Store(true)
		client.budget.Close()
		if err := client.shutdown(ctx, client.budget.limits.Calls < defaultCalls); err != nil {
			return FilesystemResponse{}, err
		}
	} else {
		if err := client.stream.SetDeadline(time.Now().Add(brokerIdleTimeout)); err != nil {
			return FilesystemResponse{}, err
		}
		if client.closed.Load() {
			return FilesystemResponse{}, ErrClosed
		}
	}
	return response, nil
}

// send validates host lifecycle transitions before writing.
//
// Takes message (sandboxwire.Message) which is one bounded host-owned protocol message.
//
// Returns framing, state or transport errors.
func (client *FilesystemClient) send(message sandboxwire.Message) error {
	if err := client.connection.machine.Observe(sandboxwire.HostToWorker, message); err != nil {
		return err
	}
	return client.connection.codec.Write(message)
}

// receive validates broker lifecycle transitions before domain decoding.
//
// Returns a bounded message whose payload still needs operation-specific validation.
func (client *FilesystemClient) receive() (sandboxwire.Message, error) {
	message, err := client.connection.codec.Read()
	if err != nil {
		return sandboxwire.Message{}, err
	}
	if err := client.connection.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return sandboxwire.Message{}, err
	}
	return message, nil
}

// shutdown waits for normal exit under a hard deadline before closing the owner.
//
// Takes graceful (bool) which is false only when the final protocol call requires broker
// exit.
//
// Returns errors without suppressing resource cleanup failures.
func (client *FilesystemClient) shutdown(ctx context.Context, graceful bool) error {
	if graceful {
		if err := client.stream.SetDeadline(time.Now().Add(brokerOperationTimeout)); err != nil {
			return errors.Join(err, closeForCleanup(ctx, client.stream))
		}
		if err := client.send(sandboxwire.Message{Kind: sandboxwire.Close, ID: 0, Payload: json.RawMessage("{}")}); err != nil {
			return errors.Join(err, closeForCleanup(ctx, client.stream))
		}
	}
	return errors.Join(client.stream.Wait(), closeForCleanup(ctx, client.stream))
}

// abort closes authority and cancels the process without concurrent protocol writes.
//
// Returns execution and cleanup errors, allowing the retained owner to retry.
func (client *FilesystemClient) abort(ctx context.Context) error {
	client.closed.Store(true)
	client.budget.Close()
	return closeForCleanup(ctx, client.stream)
}

// abortWithoutContext aborts for a caller that passed no context, which is a programming
// error, under a bounded background context.
//
// Returns execution and cleanup errors.
func (client *FilesystemClient) abortWithoutContext() error {
	client.closed.Store(true)
	client.budget.Close()
	return closeWithoutContext(client.stream)
}

// watchCancellation joins cancellation cleanup before an operation returns.
//
// Returns a completion function that stops or joins the cancellation callback.
func (client *FilesystemClient) watchCancellation(ctx context.Context) func() error {
	done := make(chan struct{})
	var cleanupErr error
	stop := context.AfterFunc(ctx, func() {
		cleanupErr = client.abort(ctx)
		close(done)
	})
	return func() error {
		if !stop() {
			<-done
		}
		return errors.Join(ctx.Err(), cleanupErr)
	}
}

// OpenFilesystemClient validates the handshake of an independently confined broker.
// Grants and limits must be the same immutable host policy used for native bootstrap.
//
// Takes stream (supervisedFilesystemTransport) which is the supervised broker transport.
// Takes grants ([]RootGrant) which hold the host-approved root authority.
// Takes limits (FilesystemLimits) which are the finite cumulative quotas.
//
// Returns a client, or an error after cancelling and reaping the supplied process.
func OpenFilesystemClient(ctx context.Context, stream supervisedFilesystemTransport, grants []RootGrant, limits FilesystemLimits) (*FilesystemClient, error) {
	if stream == nil {
		return nil, sandboxwire.ErrProtocol
	}
	if ctx == nil {
		return nil, errors.Join(sandboxwire.ErrProtocol, closeWithoutContext(stream))
	}
	budget, err := NewFilesystemBudget(grants, limits)
	if err != nil {
		return nil, errors.Join(err, closeForCleanup(ctx, stream))
	}
	return openFilesystemClient(ctx, stream, budget, nil, nil)
}

// closeForCleanup closes stream after a failure under ctx, keeping ctx's values without
// its cancellation and bounding the close (sandboxwire.CleanupContext).
//
// Takes stream (supervisedFilesystemTransport) which is closed.
//
// Returns error from the close.
func closeForCleanup(ctx context.Context, stream supervisedFilesystemTransport) error {
	cleanupCtx, cancel := sandboxwire.CleanupContext(ctx)
	defer cancel()
	return stream.Close(cleanupCtx)
}

// closeWithoutContext closes stream for a caller that passed no context, which is a
// programming error, so the close has no context of its own to keep and runs under a
// bounded background context.
//
// Takes stream (supervisedFilesystemTransport) which is closed.
//
// Returns error from the close.
func closeWithoutContext(stream supervisedFilesystemTransport) error {
	return closeForCleanup(context.Background(), stream)
}

// openFilesystemClient starts a host client with an already selected authority ledger.
//
// Takes stream (supervisedFilesystemTransport) which is the supervised broker transport.
// Takes budget (*FilesystemBudget) which is the already validated reservation ledger.
// Takes recovery (*LinuxRecoveryAuthority) which is the optional recovery authority.
// Takes journal (*LinuxRecoveryJournal) which is the optional durable write journal.
//
// Returns a client after handshake, or error after reaping the supplied transport.
func openFilesystemClient(ctx context.Context, stream supervisedFilesystemTransport, budget *FilesystemBudget,
	recovery *LinuxRecoveryAuthority, journal *LinuxRecoveryJournal,
) (*FilesystemClient, error) {
	connection, err := newFilesystemConnection(stream)
	if err != nil {
		return nil, errors.Join(err, closeForCleanup(ctx, stream))
	}
	client := &FilesystemClient{
		stream: stream, connection: connection, budget: budget, nextID: 1,
		recovery: recovery, journal: journal,
		active: atomic.Bool{}, closed: atomic.Bool{},
	}
	finish := client.watchCancellation(ctx)
	err = client.handshake()
	err = errors.Join(err, finish())
	if err != nil {
		return nil, errors.Join(err, client.abort(ctx))
	}
	return client, nil
}
