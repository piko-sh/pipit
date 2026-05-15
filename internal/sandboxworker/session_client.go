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

package sandboxworker

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

var (
	// ErrSessionClosed reports a terminal host-side worker session.
	ErrSessionClosed = errors.New("isolated worker session is closed")

	// ErrSessionBusy reports an already active submission without retaining a queue.
	ErrSessionBusy = errors.New("isolated worker session is busy")

	// ErrSessionLimit reports a host-counted cumulative session limit.
	ErrSessionLimit = errors.New("isolated worker session limit exceeded")

	// ErrSessionEvaluation reports a script evaluation failure inside the worker; the
	// wrapped text is the worker's untrusted diagnostic.
	ErrSessionEvaluation = errors.New("isolated worker session evaluation failed")
)

// sessionTransport must enforce deadlines by terminating the process independently of
// I/O, account native diagnostics and script output together, and reap on Close. Wait
// must wait for exit and cleanup under the currently enforced process deadline.
type sessionTransport interface {
	HostTransport

	// Wait blocks until the worker exits and cleanup completes.
	//
	// Returns error when exit or cleanup fails.
	Wait() error

	// Close terminates the worker and releases its resources.
	//
	// Returns error when termination or cleanup fails.
	Close() error
}

// SessionClient owns one dedicated confined worker, never a shared or reusable pool.
// Worker cost reports are telemetry, not evidence of enforced CPU or memory bounds.
type SessionClient struct {
	// stream is the supervised worker channel with deadline enforcement.
	stream sessionTransport

	// connection holds framing and lifecycle state for the worker.
	connection *connection

	// expires is the absolute deadline for the entire session.
	expires time.Time

	// config stores the immutable reviewed import grants.
	config Configuration

	// nextID is the next protocol correlation identifier.
	nextID uint64

	// sourceRemaining tracks cumulative source bytes still allowed.
	sourceRemaining int

	// outputRemaining tracks cumulative output bytes still allowed.
	outputRemaining int

	// submissionsRemaining tracks how many submissions remain.
	submissionsRemaining int

	// active is true while a submission is in progress.
	active atomic.Bool

	// closed is true after the session has been permanently closed.
	closed atomic.Bool
}

// Submit performs two correlated phases without permitting overlapping submissions. Any
// admitted failure permanently closes the worker before returning an error.
//
// Takes source (string) which is charged to the host's cumulative budget before sending.
//
// Returns a validated response only while all native and host-accounted limits hold.
func (session *SessionClient) Submit(source string) (response Response, err error) {
	if session == nil || session.stream == nil || session.closed.Load() {
		return Response{}, ErrSessionClosed
	}
	if !session.active.CompareAndSwap(false, true) {
		return Response{}, ErrSessionBusy
	}
	defer session.active.Store(false)
	completed := false
	defer func() {
		if !completed {
			err = ErrSessionClosed
		}
		if err != nil {
			session.closed.Store(true)
			err = errors.Join(err, session.stream.Close())
			response = Response{Output: "", Error: "", Code: "", Value: nil, CostUsed: 0, OutputTruncated: false}
		}
	}()
	response, err = session.exchange(source)
	completed = true
	return response, err
}

// Close permanently closes admission and reaps the worker. Idle workers receive a bounded
// graceful shutdown; active workers are cancelled without racing protocol writes.
//
// Returns an error for failed execution, shutdown or resource cleanup.
func (session *SessionClient) Close() error {
	if session == nil || session.stream == nil {
		return nil
	}
	alreadyClosed := session.closed.Swap(true)
	if alreadyClosed || !session.active.CompareAndSwap(false, true) {
		return session.stream.Close()
	}
	defer session.active.Store(false)
	stream := sessionDeadlineTransport{transport: session.stream, expires: session.expires}
	if err := stream.SetDeadline(time.Now().Add(resultTimeout)); err != nil {
		return errors.Join(err, session.stream.Close())
	}
	if err := session.connection.hostSend(sandboxwire.Close, 0, struct{}{}); err != nil {
		return errors.Join(err, session.stream.Close())
	}
	return errors.Join(session.stream.Wait(), session.stream.Close())
}

// exchange runs an exclusively admitted submission with host-owned cumulative counters.
//
// Takes source (string) which has not yet been sent to the worker.
//
// Returns a response only after validating policy, accounting and the next deadline.
func (session *SessionClient) exchange(source string) (Response, error) {
	if session.closed.Load() {
		return Response{}, ErrSessionClosed
	}
	request := Request{Kind: "submission", Source: source, Entrypoint: ""}
	if err := validateSessionSubmission(session.config, request); err != nil {
		return Response{}, err
	}
	if len(source) > session.sourceRemaining || session.submissionsRemaining == 0 {
		return Response{}, ErrSessionLimit
	}
	session.sourceRemaining -= len(source)
	session.submissionsRemaining--
	stream := sessionDeadlineTransport{transport: session.stream, expires: session.expires}
	response, err := session.connection.exchangeSubmission(stream, request, session.nextID)
	if err != nil {
		return Response{}, err
	}
	session.nextID += 2

	produced := len(response.Output) + len(response.Value)
	if produced > session.outputRemaining {
		return Response{}, ErrSessionLimit
	}
	session.outputRemaining -= produced
	if err := session.stream.ChargeOutput(produced); err != nil {
		return Response{}, err
	}
	if response.Code != "ok" || response.OutputTruncated {
		return Response{}, fmt.Errorf("%w: %s", ErrSessionEvaluation, response.Error)
	}
	if session.submissionsRemaining == 0 {
		session.closed.Store(true)
		if err := errors.Join(session.stream.Wait(), session.stream.Close()); err != nil {
			return Response{}, err
		}
		return response, nil
	}
	if err := session.armIdle(); err != nil {
		return Response{}, err
	}
	if session.closed.Load() {
		return Response{}, ErrSessionClosed
	}
	return response, nil
}

// armIdle enforces inactivity even when no caller is reading or writing the channel.
//
// Returns an error when the native process cannot enforce the remaining idle interval.
func (session *SessionClient) armIdle() error {
	return session.stream.SetDeadline(earlierDeadline(time.Now().Add(sessionIdleTimeout), session.expires))
}

// sessionDeadlineTransport wraps a transport to cap every phase deadline at the immutable
// session lifetime.
type sessionDeadlineTransport struct {
	transport

	// expires is the absolute session lifetime bound.
	expires time.Time
}

// SetDeadline caps every phase at the immutable total session deadline.
//
// Takes deadline (time.Time) selected by the trusted phase controller.
//
// Returns error if the underlying supervisor cannot enforce the bounded deadline.
func (stream sessionDeadlineTransport) SetDeadline(deadline time.Time) error {
	return stream.transport.SetDeadline(earlierDeadline(deadline, stream.expires))
}

// OpenSession configures one already confined worker with immutable reviewed grants. The
// caller must retain the native process owner and retry Close after cleanup errors.
//
// Takes stream (SessionTransport) which enforces hard deadline and process lifecycle.
// Takes config (Configuration) which selects host-approved reviewed imports.
//
// Returns *SessionClient for the configured session.
// Returns error after attempting to close the supplied worker.
func OpenSession(stream sessionTransport, config Configuration) (*SessionClient, error) {
	if stream == nil {
		return nil, sandboxwire.ErrProtocol
	}
	config.Imports = append([]string(nil), config.Imports...)
	if err := validateSessionSubmission(config, Request{Kind: "submission", Source: "", Entrypoint: ""}); err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	expires := time.Now().Add(sessionLifetime)
	connection, err := handshake(stream, config)
	if err != nil {
		return nil, errors.Join(err, stream.Close())
	}
	session := &SessionClient{
		stream: stream, connection: connection, expires: expires, config: config, nextID: 1,
		sourceRemaining: sessionSourceBytes, outputRemaining: sessionOutputBytes,
		submissionsRemaining: sessionSubmissions, active: atomic.Bool{}, closed: atomic.Bool{},
	}
	if err := session.armIdle(); err != nil {
		return nil, errors.Join(err, session.Close())
	}
	return session, nil
}
