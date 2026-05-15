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
	"context"
	"encoding/json"
	"errors"
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

// sendRecovery validates a fixed host transition before sending its exact schema.
//
// Takes kind (sandboxwire.Kind) which is the role-specific message kind.
// Takes identity (uint64) which is the correlation identifier.
// Takes payload (any) which is the fixed protocol payload with no script-supplied data.
//
// Returns protocol or transport errors without a fallback profile.
func (connection *filesystemConnection) sendRecovery(kind sandboxwire.Kind, identity uint64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	message := sandboxwire.Message{Kind: kind, ID: identity, Payload: encoded}
	if err := connection.machine.Observe(sandboxwire.HostToWorker, message); err != nil {
		return err
	}
	return connection.codec.Write(message)
}

// receiveRecovery accepts only the expected correlated recovery response.
//
// Takes kind (sandboxwire.Kind) which is the required message kind.
// Takes identity (uint64) which is the required correlation identifier.
// Takes destination (any) which is the strict host-selected payload schema to decode
// into.
//
// Returns an error on unsolicited capability calls or ambiguous output.
func (connection *filesystemConnection) receiveRecovery(kind sandboxwire.Kind, identity uint64, destination any) error {
	message, err := connection.codec.Read()
	if err != nil {
		return err
	}
	if err := connection.machine.Observe(sandboxwire.WorkerToHost, message); err != nil {
		return err
	}
	if message.Kind != kind || message.ID != identity {
		return sandboxwire.ErrProtocol
	}
	return message.DecodePayload(destination)
}

// ExchangeFilesystemRecovery drives one sealed batch on an already confined process. The
// caller retains native ownership for retry if process cleanup fails.
//
// Takes stream (supervisedFilesystemTransport) which is the supervised recovery-only
// transport.
//
// Returns success only after the exact result, process exit and resource cleanup.
func ExchangeFilesystemRecovery(ctx context.Context, stream supervisedFilesystemTransport) (result error) {
	if stream == nil {
		return ErrInvalidPolicy
	}
	if ctx == nil {
		return errors.Join(ErrInvalidPolicy, stream.Close())
	}
	finished := make(chan struct{})
	var cancellation error
	stop := context.AfterFunc(ctx, func() {
		cancellation = stream.Close()
		close(finished)
	})
	defer func() {
		result = errors.Join(result, stream.Close())
		if !stop() {
			<-finished
		}
		result = errors.Join(result, cancellation, ctx.Err())
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	connection, err := newFilesystemConnection(stream)
	if err != nil {
		return err
	}
	if err := stream.SetDeadline(time.Now().Add(brokerStartupTimeout)); err != nil {
		return err
	}
	var configuration FilesystemConfiguration
	if err := connection.receiveRecovery(sandboxwire.Hello, 0, &configuration); err != nil {
		return err
	}
	if configuration.Profile != FilesystemRecoveryProfile {
		return sandboxwire.ErrProtocol
	}
	if err := connection.sendRecovery(sandboxwire.Configure, 0, configuration); err != nil {
		return err
	}
	if err := connection.receiveRecovery(sandboxwire.Ready, 0, &struct{}{}); err != nil {
		return err
	}
	if err := stream.SetDeadline(time.Now().Add(brokerOperationTimeout)); err != nil {
		return err
	}
	if err := connection.sendRecovery(sandboxwire.Run, 1, struct{}{}); err != nil {
		return err
	}
	if err := connection.receiveRecovery(sandboxwire.Result, 1, &struct{}{}); err != nil {
		return err
	}
	return stream.Wait()
}
