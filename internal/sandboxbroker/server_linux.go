//go:build linux && (amd64 || arm64)

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

package sandboxbroker

import (
	"encoding/json"
	"errors"
	"io"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

const (
	// filesystemProfile names the confined broker protocol, not an authority grant.
	filesystemProfile = "filesystem-broker-v1"

	// JournalledFilesystemProfile requires a prepared-write acknowledgement.
	JournalledFilesystemProfile = "filesystem-broker-journal-v1"

	// brokerStartupTimeout is the hard deadline for handshake completion.
	brokerStartupTimeout = 10 * time.Second

	// brokerIdleTimeout is the hard deadline between operations.
	brokerIdleTimeout = 60 * time.Second

	// brokerOperationTimeout is the hard deadline for a single operation.
	brokerOperationTimeout = 2 * time.Second
)

// filesystemTransport carries only the private, deadline-capable broker protocol.
type filesystemTransport interface {
	io.Reader

	io.Writer

	// SetDeadline sets the read and write deadline on the transport.
	//
	// Takes deadline (time.Time) which is the absolute deadline.
	//
	// Returns error when the underlying connection rejects the deadline.
	SetDeadline(time.Time) error
}

// FilesystemConfiguration confirms the role without changing sealed startup authority.
type FilesystemConfiguration struct {
	// Profile identifies the negotiated broker protocol version.
	Profile string `json:"profile"`
}

// filesystemConnection holds the framed codec and lifecycle state machine.
type filesystemConnection struct {
	// codec is the bounded frame reader and writer.
	codec *sandboxwire.Codec

	// machine tracks the protocol lifecycle transitions.
	machine *sandboxwire.Machine

	// journalled is true when prepared-write acknowledgement is required.
	journalled bool
}

// newFilesystemConnection creates independent framing and lifecycle validators.
//
// Takes stream (filesystemTransport) which is the private broker transport.
//
// Returns a fresh connection without granting filesystem authority.
func newFilesystemConnection(stream filesystemTransport) (*filesystemConnection, error) {
	codec, err := sandboxwire.New(stream, stream, sandboxwire.Limits{MaxFrameBytes: maximumBrokerFrameBytes, MaxJSONDepth: 0, MaxJSONValues: 0})
	if err != nil {
		return nil, err
	}
	machine, err := sandboxwire.NewMachine(sandboxwire.SessionLimits{MaxCallsPerRun: 1, MaxOutstandingCalls: 1})
	if err != nil {
		return nil, err
	}
	return &filesystemConnection{codec: codec, machine: machine, journalled: false}, nil
}

// serveFilesystemOperations bounds the total operation count without resetting quotas.
//
// Takes stream (filesystemTransport) which is the private transport after a successful
// handshake.
// Takes backend (*LinuxFilesystem) which is the sealed backend.
//
// Returns after Close, quota exhaustion, an operation error or transport failure.
func (connection *filesystemConnection) serveFilesystemOperations(stream filesystemTransport, backend *LinuxFilesystem) error {
	for range defaultCalls {
		if err := stream.SetDeadline(time.Now().Add(brokerIdleTimeout)); err != nil {
			return err
		}
		message, err := connection.receive()
		if err != nil {
			return err
		}
		if message.Kind == sandboxwire.Close {
			return message.DecodePayload(&struct{}{})
		}
		if err := stream.SetDeadline(time.Now().Add(brokerOperationTimeout)); err != nil {
			return err
		}
		message.Kind = sandboxwire.Call
		result, operationErr := backend.execute(message, connection)
		if errors.Is(operationErr, sandboxwire.ErrProtocol) {
			return operationErr
		}
		response := filesystemResponse(result, operationErr)
		if err := connection.send(sandboxwire.Result, message.ID, response); err != nil {
			return err
		}
		if operationErr != nil {
			return operationErr
		}
	}
	return nil
}

// send validates each outbound transition before encoding one bounded frame.
//
// Takes kind (sandboxwire.Kind) which is the message kind.
// Takes identity (uint64) which is the correlation identity.
// Takes payload (any) which is the trusted response schema.
//
// Returns error for invalid transitions, encoding or transport failure.
func (connection *filesystemConnection) send(kind sandboxwire.Kind, identity uint64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	message := sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
	if err := connection.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return err
	}
	return connection.codec.Write(message)
}

// receive checks frame bounds and host message ordering before domain validation.
//
// Returns a message whose payload still requires its exact domain schema.
func (connection *filesystemConnection) receive() (sandboxwire.Message, error) {
	message, err := connection.codec.Read()
	if err != nil {
		return sandboxwire.Message{}, err
	}
	if err := connection.machine.Observe(sandboxwire.HostToWorker, message); err != nil {
		return sandboxwire.Message{}, err
	}
	return message, nil
}

// beginProtocol admits one protocol owner only after successful process sealing.
//
// Returns error for an unsealed, closed, previously used or already serving backend.
//
// Safe for concurrent use by multiple goroutines.
func (backend *LinuxFilesystem) beginProtocol() error {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed || backend.protocolStarted || backend.budget.calls != 0 {
		return ErrClosed
	}
	if !backend.sealed {
		return ErrConfinement
	}
	backend.protocolStarted = true
	return nil
}

// ServeFilesystemBroker handles a bounded sequence after complete native bootstrap. Any
// operation error is terminal and returns no partial data.
//
// Takes stream (filesystemTransport) which is the private broker transport.
// Takes backend (*LinuxFilesystem) which is the sealed backend after native bootstrap.
//
// Returns error on invalid protocol, failed operation or transport failure.
func ServeFilesystemBroker(stream filesystemTransport, backend *LinuxFilesystem) error {
	if stream == nil || backend == nil {
		return sandboxwire.ErrProtocol
	}
	if err := backend.beginProtocol(); err != nil {
		return err
	}
	if backend.recovery != nil {
		return serveFilesystemRecovery(stream, backend)
	}
	if err := stream.SetDeadline(time.Now().Add(brokerStartupTimeout)); err != nil {
		return err
	}
	connection, err := newFilesystemConnection(stream)
	if err != nil {
		return err
	}
	if err := connection.send(sandboxwire.Hello, 0, FilesystemConfiguration{Profile: filesystemProfile}); err != nil {
		return err
	}
	message, err := connection.receive()
	if err != nil {
		return err
	}
	if message.Kind == sandboxwire.Close {
		return message.DecodePayload(&struct{}{})
	}
	var configuration FilesystemConfiguration
	if err := message.DecodePayload(&configuration); err != nil {
		return err
	}
	if configuration.Profile != filesystemProfile && configuration.Profile != JournalledFilesystemProfile {
		return sandboxwire.ErrProtocol
	}
	connection.journalled = configuration.Profile == JournalledFilesystemProfile
	if err := connection.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
		return err
	}
	return connection.serveFilesystemOperations(stream, backend)
}

// filesystemResponse separates bounded protocol status from trusted native diagnostics.
//
// Takes result (FilesystemResult) which is returned by the fixed compiled backend.
// Takes err (error) which is returned by the fixed compiled backend.
//
// Returns success data, or a sanitised failure without any partial result.
func filesystemResponse(result FilesystemResult, err error) FilesystemResponse {
	if err == nil {
		return FilesystemResponse{Data: result.Data, Entries: result.Entries, Written: result.Written, Skipped: result.skipped, Code: ""}
	}
	code := "io"
	switch {
	case errors.Is(err, ErrDenied):
		code = "denied"
	case errors.Is(err, errLimit), errors.Is(err, errBusy):
		code = "limit"
	case errors.Is(err, ErrClosed), errors.Is(err, ErrConfinement):
		code = "closed"
	}
	return FilesystemResponse{Data: nil, Entries: nil, Written: 0, Skipped: 0, Code: code}
}
