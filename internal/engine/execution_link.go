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

package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/fault"
)

// executionLink connects callbacks, bound methods and goroutines to their originating
// execution without retaining its VM.
//
// Callbacks follow cancellation while the execution runs and use a detached context after
// it ends. Returning from main lets existing callbacks finish.
//
// The link holds contexts because native code calls a callback through a Go function
// value whose signature the host chose (reflect.MakeFunc), which carries no context. The
// link is how such a call finds the execution it belongs to. From there the context is
// passed as a parameter.
type executionLink struct {
	// ctx is the execution's context.
	ctx context.Context

	// detached is ctx without its cancellation; values are kept.
	detached context.Context

	// callbackCtx follows ctx's cancellation, except the main-returned cause, which only
	// ends the link. Built on first use under mu.
	callbackCtx context.Context

	// published holds callbackCtx once built, for the lock-free fast path.
	published atomic.Pointer[context.Context]

	// cancel fails the execution with a cause; nil when the execution has no cancel
	// function.
	cancel context.CancelCauseFunc

	// relayCancel cancels callbackCtx; the relay calls it.
	relayCancel context.CancelCauseFunc

	// stopRelay deregisters the relay from ctx.
	stopRelay func() bool

	// failure holds the first callback failure for the main run to report.
	failure atomic.Pointer[error]

	// mu guards building and stopping the relay.
	mu sync.Mutex

	// ended is set once the execution has returned or its main function has.
	ended atomic.Bool
}

// newExecutionLink returns a live link for an execution running under ctx.
//
// Takes cancel (context.CancelCauseFunc) which fails the execution; may be nil.
//
// Returns the link.
func newExecutionLink(ctx context.Context, cancel context.CancelCauseFunc) *executionLink {
	return &executionLink{
		ctx:         ctx,
		detached:    context.WithoutCancel(ctx),
		callbackCtx: nil,
		published:   atomic.Pointer[context.Context]{},
		cancel:      cancel,
		relayCancel: nil,
		stopRelay:   nil,
		failure:     atomic.Pointer[error]{},
		mu:          sync.Mutex{},
		ended:       atomic.Bool{},
	}
}

// endedExecutionLink returns a detached link for code running outside an execution.
//
// Returns the ended link.
func endedExecutionLink(ctx context.Context) *executionLink {
	link := newExecutionLink(ctx, nil)
	link.ended.Store(true)
	return link
}

// live reports whether the execution is still running.
//
// Returns bool which is false once the link has ended.
func (l *executionLink) live() bool {
	return !l.ended.Load()
}

// callbackContext returns the execution's cancellation relay while it runs, or a detached
// context after it ends. The relay excludes fault.ErrMainReturned so existing callbacks
// can finish.
//
// Returns context.Context which the run polls.
//
// Safe for concurrent use through the mutex and atomic publication of execution state.
func (l *executionLink) callbackContext() context.Context {
	if !l.live() {
		return l.detached
	}
	if published := l.published.Load(); published != nil {
		return *published
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.live() {
		return l.detached
	}
	if l.callbackCtx == nil {
		l.callbackCtx, l.relayCancel = context.WithCancelCause(l.detached)
		if l.ctx.Err() != nil {
			l.relay()
		} else {
			l.stopRelay = context.AfterFunc(l.ctx, l.relay)
		}
		published := l.callbackCtx
		l.published.Store(&published)
	}
	if !l.live() {
		return l.detached
	}
	return l.callbackCtx
}

// relay passes the execution's end on to callbackCtx, unless main returned.
func (l *executionLink) relay() {
	cause := context.Cause(l.ctx)
	if errors.Is(cause, fault.ErrMainReturned) {
		l.ended.Store(true)
		l.published.Store(nil)
		return
	}
	l.relayCancel(cause)
}

// spawnContext returns the execution's context while it runs, including cancellation when
// main returns, or a detached context after it ends.
//
// Returns context.Context which the goroutine's VM follows.
func (l *executionLink) spawnContext() context.Context {
	if l.live() {
		return l.ctx
	}
	return l.detached
}

// end marks the execution ended and stops the relay. Callbacks still running keep the
// context they started with, which is no longer cancelled.
//
// Safe for concurrent use; a mutex serialises relay creation and shutdown, and the
// execution state is updated atomically.
func (l *executionLink) end() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended.Store(true)
	l.published.Store(nil)
	if l.stopRelay != nil {
		l.stopRelay()
	}
}

// fail records the first failure and cancels a live execution. Without a cancel function,
// the failure is reported when the main run returns.
//
// Takes cause (error) which becomes the execution's failure and its context's cause.
//
// Returns bool which is true when the execution was live.
func (l *executionLink) fail(cause error) bool {
	if !l.live() {
		return false
	}
	l.failure.CompareAndSwap(nil, &cause)
	if l.cancel != nil {
		l.cancel(cause)
	}
	return true
}

// failedError returns the execution's failure as an error that satisfies errors.Is
// against fault.ErrExecutionCancelled, or nil when the execution has not failed.
//
// Returns error which describes the failure.
func (l *executionLink) failedError() error {
	failure := l.failure.Load()
	if failure == nil {
		return nil
	}
	if l.ctx.Err() != nil {
		return cancellationError(l.ctx)
	}
	if errors.Is(*failure, fault.ErrExecutionCancelled) {
		return *failure
	}
	return fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, *failure)
}
