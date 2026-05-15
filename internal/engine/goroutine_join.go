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
	"time"

	"pipit.sh/pipit/internal/fault"
)

const (
	// GoroutineJoinGrace bounds the post-return goroutine cancellation wait, measured from
	// the last observed progress so a tree of children finishing sequentially is not cut
	// off.
	GoroutineJoinGrace = time.Second

	// GoroutineJoinScheduleCap bounds how long the join keeps waiting for goroutines whose
	// body the Go scheduler has not started yet. Such goroutines cannot be stuck in user
	// code, so they extend the grace under heavy load (GC-torture lanes, saturated CI
	// runners) up to this cap.
	GoroutineJoinScheduleCap = 10 * time.Second

	// goroutineJoinPollInterval is how often the join re-examines the tracker's counters
	// while waiting.
	goroutineJoinPollInterval = time.Millisecond
)

// JoinSpawnedGoroutines cancels and waits for the execution's goroutines, counting any
// still running after GoroutineJoinGrace as leaked so the Service is not reused. The call
// is idempotent and a no-op on nested or child VMs.
//
// Returns errGoroutineJoinTimeout wrapping the straggler count when the grace period
// elapses, or nil when every goroutine has exited.
func (vm *VM) JoinSpawnedGoroutines() error {
	if vm.goroutinesJoined || vm.executionCancel == nil {
		return vm.goroutineJoinError
	}
	vm.goroutinesJoined = true
	vm.executionCancel(fault.ErrMainReturned)
	tracker := vm.Limits.Tracker
	if tracker == nil || tracker.GoroutineCount.Load() == 0 {
		return nil
	}
	released := vm.releaseAroundBlock()
	joined := WaitForGoroutines(tracker, GoroutineJoinGrace)
	vm.reacquireAfterBlock(released)
	if joined {
		return nil
	}
	stragglers := int64(tracker.GoroutineCount.Load())
	tracker.LeakedGoroutines.Add(stragglers)
	vm.goroutinesLeaked = true
	vm.goroutineJoinError = fmt.Errorf("%w: %d still running", fault.ErrGoroutineJoinTimeout, stragglers)
	return vm.goroutineJoinError
}

// NewExecutionContext derives the cancellable context that a top-level execution and
// every goroutine it spawns run under, so the main goroutine can cancel its children once
// it returns. The caller must defer the returned cancel function.
//
// Returns the derived context and its cancel function.
func NewExecutionContext(ctx context.Context) (context.Context, context.CancelCauseFunc) {
	return context.WithCancelCause(ctx)
}

// WaitForGoroutines waits on the tracker's join group until every registered goroutine
// has exited or the grace period elapses without progress. The deadline is pushed out
// whenever a child exits, and while goroutines are still waiting to be scheduled (bounded
// by GoroutineJoinScheduleCap), so only goroutines that are genuinely stuck exhaust it.
//
// Takes tracker (*ResourceTracker) which owns the join group.
// Takes grace (time.Duration) which bounds the wait since the last progress.
//
// Returns true when every registered goroutine exited before the grace ran out.
func WaitForGoroutines(tracker *ResourceTracker, grace time.Duration) bool {
	if tracker.GoroutineCount.Load() == 0 {
		return true
	}
	started := time.Now()
	deadline := started.Add(grace)
	lastCount := tracker.GoroutineCount.Load()
	ticker := time.NewTicker(goroutineJoinPollInterval)
	defer ticker.Stop()
	for now := range ticker.C {
		count := tracker.GoroutineCount.Load()
		if count == 0 {
			return true
		}
		awaitingScheduler := tracker.GoroutinesUnscheduled.Load() > 0 && now.Sub(started) < GoroutineJoinScheduleCap
		if count < lastCount || awaitingScheduler {
			lastCount = count
			deadline = now.Add(grace)
		}
		if !now.Before(deadline) {
			return false
		}
	}
	return false
}

// stoppedBecauseMainReturned reports whether a child goroutine's dispatch error is the
// expected consequence of the execution being cancelled after its main function returned,
// in which case it must not be recorded as a goroutine failure.
//
// Takes err (error) which is the child's dispatch error.
//
// Returns true when the child stopped because of fault.ErrMainReturned.
func stoppedBecauseMainReturned(ctx context.Context, err error) bool {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, fault.ErrExecutionCancelled) {
		return false
	}
	return errors.Is(context.Cause(ctx), fault.ErrMainReturned)
}
