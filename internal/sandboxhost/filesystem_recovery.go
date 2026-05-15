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

package sandboxhost

import (
	"context"
	"log/slog"
	"sync"
)

// IsolatedFilesystemRecovery owns exclusive recovery of a terminated filesystem host. It
// grants no script execution.
type IsolatedFilesystemRecovery struct {
	// recovery is the platform-specific recovery backend.
	recovery isolatedFilesystemRecovery

	// logger is the logger the host configured, and may be nil. Recover() runs under the
	// host's own context rather than the constructor's, so this API root keeps the logger
	// and attaches it again on every call.
	logger *slog.Logger

	// mutex serialises Recover and Close calls.
	mutex sync.Mutex
}

// NewIsolatedFilesystemRecovery claims recovery records from a terminated host. Supply
// the original host-approved configuration.
//
// Takes config (IsolatedFilesystemConfig) which is the original host-selected
// configuration.
//
// Returns exclusive recovery ownership, or an error without a weaker fallback.
func NewIsolatedFilesystemRecovery(ctx context.Context, config IsolatedFilesystemConfig) (*IsolatedFilesystemRecovery, error) {
	if err := validateIsolatedFilesystemConfig(ctx, &config); err != nil {
		return nil, err
	}
	ctx = isolatedLoggerContext(ctx, config.Worker.Logger)
	recovery, err := newIsolatedFilesystemRecovery(ctx, &config)
	if recovery == nil {
		return nil, err
	}
	if err != nil {
		return nil, cleanupOrFail(err, recovery.Close)
	}
	return &IsolatedFilesystemRecovery{recovery: recovery, logger: config.Worker.Logger, mutex: sync.Mutex{}}, nil
}

// Recover terminates original descendants and cleans approved writes.
//
// Takes a context for this attempt, separate from the acquisition context.
//
// Returns cleanup errors without dropping ownership.
//
// Safe for concurrent use; callers are serialised by an internal mutex.
func (owner *IsolatedFilesystemRecovery) Recover(ctx context.Context) error {
	if owner == nil || owner.recovery == nil {
		return ErrIsolatedUsed
	}
	if ctx == nil {
		return ErrInvalidIsolatedConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.recovery.Recover(isolatedLoggerContext(ctx, owner.logger))
}

// Close releases an unused claim or completed recovery.
//
// Returns closure errors; nil owners are harmless.
//
// Safe for concurrent use; callers are serialised by an internal mutex.
func (owner *IsolatedFilesystemRecovery) Close() error {
	if owner == nil || owner.recovery == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.recovery.Close()
}

// isolatedFilesystemRecovery is the platform-specific recovery backend.
type isolatedFilesystemRecovery interface {
	// Recover runs cleanup of a terminated filesystem host.
	//
	// Returns error when cleanup fails.
	Recover(context.Context) error

	// Close releases the recovery claim.
	//
	// Returns error when the claim cannot be released.
	Close() error
}
