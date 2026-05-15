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

package sandboxlinux

import (
	"context"
	"encoding/json"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// FilesystemBroker owns one confined broker and its validating host client. A non-nil
// owner must be closed even when opening fails.
type FilesystemBroker struct {
	// process holds the supervised broker child process.
	process *WorkerProcess

	// client holds the validating host-side filesystem client.
	client *sandboxbroker.FilesystemClient

	// recovery holds journalled cleanup state, nil for non-journalled owners.
	recovery *filesystemBrokerRecovery
}

// Execute forwards only a bounded filesystem request to the validating client. The client
// checks host authority before sending and rejects concurrent operations.
//
// Takes payload (json.RawMessage) which contains the exact filesystem request.
//
// Returns validated data, or an error without exposing partial broker output.
func (owner *FilesystemBroker) Execute(ctx context.Context, payload json.RawMessage) (sandboxbroker.FilesystemResponse, error) {
	if owner == nil || owner.client == nil {
		return sandboxbroker.FilesystemResponse{}, sandboxbroker.ErrClosed
	}
	if owner.recovery != nil {
		if !owner.recovery.beginOperation() {
			return sandboxbroker.FilesystemResponse{}, sandboxbroker.ErrClosed
		}
		defer owner.recovery.active.Done()
	}
	return owner.client.Execute(ctx, payload)
}

// Close shuts down the client and retains native cleanup ownership for retries.
//
// Returns any execution, shutdown or resource cleanup error.
func (owner *FilesystemBroker) Close() error {
	if owner == nil {
		return nil
	}
	if owner.recovery != nil {
		return owner.closeJournalled()
	}
	if owner.client != nil {
		return owner.client.Close()
	}
	if owner.process != nil {
		return owner.process.Close()
	}
	return nil
}

// Released reports whether the broker's native resources are gone: its process cleaned up
// and no journalled recovery pending.
//
// Returns bool which is true for a nil owner or one whose cleanup completed.
func (owner *FilesystemBroker) Released() bool {
	if owner == nil {
		return true
	}
	return !owner.RecoveryPending() && owner.process.Released()
}

// RecoveryPending reports retained journalled cleanup work after a Close attempt. Callers
// must not close the aggregate parent while this returns true.
//
// Returns false for nil owners or those without journalled recovery.
//
// Safe for concurrent use by multiple goroutines.
func (owner *FilesystemBroker) RecoveryPending() bool {
	if owner == nil || owner.recovery == nil {
		return false
	}
	owner.recovery.cleanup.Lock()
	defer owner.recovery.cleanup.Unlock()
	return !owner.recovery.complete
}
