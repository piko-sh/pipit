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

// IsolatedRecovery reclaims what dead hosts left under a tenant's state directory.
type IsolatedRecovery struct {
	// recovery is the platform-specific recovery backend.
	recovery isolatedRecovery

	// logger is the logger the host configured, and may be nil. Recover() runs under the
	// host's own context rather than the constructor's, so this API root keeps the logger
	// and attaches it again on every call.
	logger *slog.Logger

	// mutex serialises Recover and Close calls.
	mutex sync.Mutex
}

// NewIsolatedRecovery claims the tenant's recoverable launch records under
// config.StateDirectory. The configuration must be the one the launches used: the host
// binding is derived from it, and a record recorded under another configuration is
// refused.
//
// Takes config (IsolatedConfig) which names the state directory, the tenant and the
// approved executables.
//
// Returns the recovery, or an error when the configuration is invalid or the platform has
// no native isolation.
func NewIsolatedRecovery(ctx context.Context, config IsolatedConfig) (*IsolatedRecovery, error) {
	if err := validateIsolatedConfig(ctx, &config); err != nil {
		return nil, err
	}
	ctx = isolatedLoggerContext(ctx, config.Logger)
	recovery, err := newIsolatedRecovery(ctx, &config)
	if recovery == nil {
		return nil, err
	}
	if err != nil {
		return nil, cleanupOrFail(err, recovery.Close)
	}
	return &IsolatedRecovery{recovery: recovery, logger: config.Logger, mutex: sync.Mutex{}}, nil
}

// Recover runs the recovery sequence over every claimed record.
//
// Returns error which joins the failures of the records that could not be recovered.
//
// Safe for concurrent use; callers are serialised by an internal mutex.
func (owner *IsolatedRecovery) Recover(ctx context.Context) error {
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

// Close releases the claims of records that were not recovered.
//
// Returns error when a claim cannot be released.
//
// Safe for concurrent use; callers are serialised by an internal mutex.
func (owner *IsolatedRecovery) Close() error {
	if owner == nil || owner.recovery == nil {
		return nil
	}
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	return owner.recovery.Close()
}

// isolatedRecovery is the platform recovery behind IsolatedRecovery.
type isolatedRecovery interface {
	// Recover runs cleanup of terminated worker launch records.
	//
	// Returns error when cleanup fails.
	Recover(ctx context.Context) error

	// Close releases unclaimed recovery records.
	//
	// Returns error when a claim cannot be released.
	Close() error
}
