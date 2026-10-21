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

package sandboxwire

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// CleanupTimeout bounds one cleanup that runs after a failure or a cancellation.
const CleanupTimeout = 5 * time.Second

var (
	// ErrCleanupTimedOut is the cause of a cleanup context whose bound passed.
	ErrCleanupTimedOut = errors.New("sandbox cleanup did not finish in time")

	// ErrLifetimeEnded is the cause of a native process lifetime context whose bound passed.
	ErrLifetimeEnded = errors.New("sandbox process lifetime ended")

	// ErrExecutionTimedOut is the cause of an execution phase context whose bound passed.
	ErrExecutionTimedOut = errors.New("sandbox execution did not finish in time")
)

// CleanupContext returns a context for cleanup that must run after ctx has ended. It
// keeps ctx's values, such as the bound logger, drops its cancellation and bounds the
// cleanup by CleanupTimeout, with ErrCleanupTimedOut as the cause when the bound passes.
//
// Returns context.Context which is the cleanup context.
// Returns context.CancelFunc which releases it.
func CleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(context.WithoutCancel(ctx), CleanupTimeout, ErrCleanupTimedOut)
}

// ContextErr reports why ctx ended with ctx.Err(), wrapped with the context's cause when
// the cause is a different error, such as ErrCleanupTimedOut, so that both match
// errors.Is.
//
// Returns error which is nil while ctx is live.
func ContextErr(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(err, cause) {
		return fmt.Errorf("%w: %w", cause, err)
	}
	return err
}
