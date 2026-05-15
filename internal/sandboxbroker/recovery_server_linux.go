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
	"time"

	"pipit.sh/pipit/internal/sandboxwire"
)

// FilesystemRecoveryProfile identifies a sealed, one-shot cleanup batch.
const FilesystemRecoveryProfile = "filesystem-broker-recovery-v1"

// recover executes only the sealed batch, never paths selected on the wire.
//
// Returns error on the first mismatch; previously removed entries remain removed.
//
// Safe for concurrent use by multiple goroutines.
func (backend *LinuxFilesystem) recover() error {
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.closed || !backend.sealed || !backend.protocolStarted || len(backend.recovery) == 0 {
		return ErrClosed
	}
	for index := range backend.recovery {
		record := backend.recovery[index]
		root := linuxRecoveryRoot{file: backend.roots[record.Root], identity: backend.identities[record.Root]}
		if err := recoverLinuxStaging(root, backend.namespace, record); err != nil {
			return err
		}
	}
	return nil
}

// serveFilesystemRecovery accepts only an empty request for the sealed cleanup batch.
//
// Takes stream (filesystemTransport) which is the private broker transport.
// Takes backend (*LinuxFilesystem) which is a completely confined backend already
// admitted by beginProtocol.
//
// Returns success only after all recorded staging entries are absent and synced.
func serveFilesystemRecovery(stream filesystemTransport, backend *LinuxFilesystem) error {
	if err := stream.SetDeadline(time.Now().Add(brokerStartupTimeout)); err != nil {
		return err
	}
	connection, err := newFilesystemConnection(stream)
	if err != nil {
		return err
	}
	if err := connection.send(sandboxwire.Hello, 0, FilesystemConfiguration{Profile: FilesystemRecoveryProfile}); err != nil {
		return err
	}
	message, err := connection.receive()
	if err != nil {
		return err
	}
	var configuration FilesystemConfiguration
	if err := message.DecodePayload(&configuration); err != nil {
		return err
	}
	if message.Kind != sandboxwire.Configure || configuration.Profile != FilesystemRecoveryProfile {
		return sandboxwire.ErrProtocol
	}
	if err := connection.send(sandboxwire.Ready, 0, struct{}{}); err != nil {
		return err
	}
	message, err = connection.receive()
	if err != nil {
		return err
	}
	if message.Kind != sandboxwire.Run || message.ID != 1 {
		return sandboxwire.ErrProtocol
	}
	if err := message.DecodePayload(&struct{}{}); err != nil {
		return err
	}
	if err := stream.SetDeadline(time.Now().Add(brokerOperationTimeout)); err != nil {
		return err
	}
	if err := backend.recover(); err != nil {
		return err
	}
	return connection.send(sandboxwire.Result, 1, struct{}{})
}
