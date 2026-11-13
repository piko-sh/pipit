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
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"time"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
)

const (
	// unwindShareComponents counts the allowance's components: cost, arena and wall time.
	unwindShareComponents = 3

	// defaultAbortUnwindCost is the total cost, in cost-table units, an aborted VM's defers
	// may spend between them, unless VMLimits.AbortUnwindCost sets it.
	defaultAbortUnwindCost = 100_000

	// abortUnwindDeferCostMax caps one defer's cost slice.
	abortUnwindDeferCostMax = 50_000

	// abortUnwindArenaBytes is the total arena working set an aborted VM's defers may use
	// above the arena budget.
	abortUnwindArenaBytes = 256 << 10

	// abortUnwindDeferArenaMax caps one defer's arena slice.
	abortUnwindDeferArenaMax = 128 << 10

	// defaultAbortUnwindWall is the total wall time an aborted VM's defers may spend
	// waiting, unless VMLimits.AbortUnwindWall sets it.
	defaultAbortUnwindWall = time.Second

	// defaultAbortUnwindDeferWallMax caps one defer's wall slice, unless
	// VMLimits.AbortUnwindDeferWallMax sets the cap.
	defaultAbortUnwindDeferWallMax = 500 * time.Millisecond

	// abortUnwindNestedCostFloor sets the minimum nested reserve retained by a defer pending
	// at abort, when its share is at least twice that size.
	abortUnwindNestedCostFloor = 64

	// abortUnwindNestedArenaFloor is the least arena headroom kept as a nested reserve.
	abortUnwindNestedArenaFloor = 4 << 10

	// abortUnwindNestedWallFloor is the least wall time kept as a nested reserve.
	abortUnwindNestedWallFloor = time.Millisecond

	// defaultAbortUnwindNativeGrace is how long past its wall deadline a defer may stay
	// inside a native call before the supervisor reports it blocked, unless
	// VMLimits.AbortUnwindNativeGrace sets it. Interruptible waits wake at the deadline
	// itself; the grace covers their wake-up.
	defaultAbortUnwindNativeGrace = 200 * time.Millisecond

	// abortUnwindSuperviseTick is how often the supervisor looks at a running unwinding.
	abortUnwindSuperviseTick = 10 * time.Millisecond

	// defaultLeakedUnwinderCap is how many unwindings a service may leave blocked in native
	// code, unless VMLimits.LeakedUnwinderCap sets it. The unwinding that reaches it fails
	// the service closed, so no later execution can leak another goroutine, VM and arena.
	defaultLeakedUnwinderCap = 64
)

// deferOutcome is how one deferred call of an unwinding ended.
type deferOutcome uint8

const (
	// deferRan means the call returned, possibly with a guest panic that was dropped.
	deferRan deferOutcome = iota

	// deferCut means the deferred call exhausted its slice, hit a budget abort or had an
	// interruptible wait stopped by its wall deadline.
	deferCut

	// deferStopsUnwinding means an interpreter invariant failed or the supervisor abandoned
	// the VM, so unwinding must end.
	deferStopsUnwinding
)

// errUnwindAbandoned is raised on an unwinding goroutine whose native call returned after
// the supervisor had given up on it, to leave the VM at once.
var errUnwindAbandoned = errors.New("abort unwinding abandoned after a blocked native call")

// errUnwindDeferWallSpent is the cause of a deferred call's context when the call's share
// of the unwinding's wall time runs out.
var errUnwindDeferWallSpent = errors.New("deferred call's share of the abort unwinding's wall time spent")

// abortUnwindCounters counts the unwinding of aborted VMs, for tests and diagnostics.
type abortUnwindCounters struct {
	// started counts unwindings that found at least one pending defer.
	started atomic.Int64

	// completed counts unwindings that reached the last pending defer.
	completed atomic.Int64

	// cut counts defers cut off by their slice, a budget abort or their wall deadline.
	cut atomic.Int64

	// deferPanics counts guest panics raised by a defer during an unwinding.
	deferPanics atomic.Int64

	// defersRun counts defers the unwindings started.
	defersRun atomic.Int64

	// nativeBlocked counts unwindings left blocked in native code.
	nativeBlocked atomic.Int64

	// stopped counts unwindings an interpreter invariant failure ended early.
	stopped atomic.Int64
}

// abortUnwinds is the process-wide unwinding counts.
var abortUnwinds abortUnwindCounters

// AbortUnwindCounts is a snapshot of the unwinding counts.
type AbortUnwindCounts struct {
	// Started counts unwindings that found at least one pending defer.
	Started int64

	// Completed counts unwindings that reached the last pending defer.
	Completed int64

	// Cut counts defers cut off by their slice, a budget abort or their wall deadline.
	Cut int64

	// DeferPanics counts guest panics raised by a defer during an unwinding.
	DeferPanics int64

	// DefersRun counts defers the unwindings started.
	DefersRun int64

	// NativeBlocked counts unwindings left blocked in native code and reported.
	NativeBlocked int64

	// Stopped counts unwindings an interpreter invariant failure ended early.
	Stopped int64
}

// unwindBlockedError is the unwind report.
//
// An unwinding left a defer blocked in native code.
type unwindBlockedError struct {
	// pending is how many defers had not run when the owner gave up.
	pending int

	// waited is how long the blocked defer had run.
	waited time.Duration
}

// abortUnwindRun holds one unwinding's remaining allowance, current defer deadline and
// state shared by the supervisor and unwinding goroutine.
type abortUnwindRun struct {
	// vm is the aborted VM.
	vm *VM

	// tracker is the execution's tracker, read before the unwinding goroutine starts, or
	// nil.
	tracker *ResourceTracker

	// done closes when the unwinding goroutine has finished.
	done chan struct{}

	// failure is the invariant error of an unwinding goroutine that panicked outside a
	// deferred call. Written before done closes, read by the supervisor after.
	failure error

	// deadline is the current defer's wall deadline in Unix nanoseconds, 0 between defers.
	// Written by the unwinding goroutine, read by the supervisor.
	deadline atomic.Int64

	// deferStarted is when the current defer started, in Unix nanoseconds.
	deferStarted atomic.Int64

	// pendingNow is the number of defers still pending, for the unwind report.
	pendingNow atomic.Int64

	// arenaCeiling is the arena budget at the abort plus abortUnwindArenaBytes.
	arenaCeiling uint64

	// allowance is the cost and wall allowance the VM's limits gave the unwinding.
	allowance abortUnwindAllowance

	// nativeGrace is how long past its wall deadline a defer may stay inside a native call.
	nativeGrace time.Duration

	// leakCap is how many leaked unwindings the service allows.
	leakCap int64

	// remainder is the shared remainder (abortUnwindSharedRemainder) that funds deferred
	// calls registered during the unwinding once their own source is spent.
	remainder unwindShare
}

// leakedUnwindersError reports that a service's stopped executions left as many
// unwindings blocked in native code as it allows.
type leakedUnwindersError struct {
	// leaked is how many unwindings were left blocked when the service failed closed.
	leaked int64
}

// unwindShareSources says, per allowance component (cost, arena, wall), whether a nested
// call's slice came from its registering defer's own share.
type unwindShareSources [unwindShareComponents]bool

// unwindShare is one defer's slice of the unwinding allowance, or, after it ran, what it
// left of that slice for the deferred calls it registered itself.
type unwindShare struct {
	// cost is in cost-table units.
	cost int64

	// arena is arena working set above the budget, in bytes.
	arena int64

	// wall is wall time.
	wall time.Duration
}

// abortUnwindSaved is the VM state an unwinding replaces and gives back.
type abortUnwindSaved struct {
	// ctx is the execution context restored after the deferred call. It saves VM.ctx, which
	// the VM holds for the reason given there.
	ctx context.Context

	// blockCtx is the context used to interrupt blocking operations.
	blockCtx context.Context

	// panicValue is the pending guest panic restored after unwinding.
	panicValue any

	// pollCause records the stop observed by the dispatch poll.
	pollCause error

	// ctxDone is the cancellation channel observed by the dispatch loop.
	ctxDone <-chan struct{}

	// blockCancel cancels blocking operations owned by the VM.
	blockCancel context.CancelFunc

	// callbackDeadline bounds execution within a native callback.
	callbackDeadline time.Time

	// limits holds the execution limits restored after unwinding.
	limits VMLimits

	// costRemaining is the instruction budget restored after unwinding.
	costRemaining int64

	// baseFramePointer identifies the frame at which the deferred run began.
	baseFramePointer int

	// arenaMaxBytes is the arena's saved allocation budget.
	arenaMaxBytes uint64

	// cancelled records the VM's saved cancellation state.
	cancelled uint32

	// arenaDisableMinorGC records whether the arena collector was disabled.
	arenaDisableMinorGC bool

	// recoverEligibleFrame identifies the frame allowed to recover a guest panic.
	recoverEligibleFrame int
}

// abortUnwindStats returns the process-wide unwinding counts.
//
// Returns AbortUnwindCounts which is a snapshot.
func abortUnwindStats() AbortUnwindCounts {
	return AbortUnwindCounts{
		Started:       abortUnwinds.started.Load(),
		Completed:     abortUnwinds.completed.Load(),
		Cut:           abortUnwinds.cut.Load(),
		DeferPanics:   abortUnwinds.deferPanics.Load(),
		DefersRun:     abortUnwinds.defersRun.Load(),
		NativeBlocked: abortUnwinds.nativeBlocked.Load(),
		Stopped:       abortUnwinds.stopped.Load(),
	}
}

// unwindsAfter reports whether a hard arena-budget abort or cost stop requires the VM's
// pending defers to run.
//
// Takes err (error) which is the stop's error.
//
// Returns bool which is true for such a stop.
func unwindsAfter(err error) bool {
	return errors.Is(err, fault.ErrArenaBudgetExceeded) || errors.Is(err, fault.ErrCostBudgetExceeded)
}

// unwindsAfterRun reports whether a stop returned from the outermost dispatch requires
// pending defers to run.
//
// It accepts an abort or cost stop recognised by unwindsAfter, or context cancellation
// recognised by stoppedByCancellation.
//
// Takes err (error) which is the run's error.
//
// Returns bool which is true for such a stop.
func (vm *VM) unwindsAfterRun(err error) bool {
	return unwindsAfter(err) || vm.stoppedByCancellation(err)
}

// stoppedByCancellation reports whether cancellation or a callback deadline stopped the
// VM's run.
//
// Takes err (error) which is the run's error.
//
// Returns bool which is true for such a stop.
func (vm *VM) stoppedByCancellation(err error) bool {
	if !isCancellationStop(err) {
		return false
	}
	if _, guestPanic := errors.AsType[*uncaughtPanicError](err); !guestPanic {
		return true
	}
	if vm.ctx == nil || vm.ctx.Err() == nil {
		return false
	}
	cause := context.Cause(vm.ctx)
	return !errors.Is(cause, fault.ErrDeadlock) && !errors.Is(cause, fault.ErrDebuggerStop)
}

// isCancellationStop reports whether err reports a cancellation or a deadline, other than
// a deadlock or a debugger stop.
//
// Takes err (error) which is the error, possibly nil.
//
// Returns bool which is true for such an error.
func isCancellationStop(err error) bool {
	if err == nil || errors.Is(err, fault.ErrDeadlock) || errors.Is(err, fault.ErrDebuggerStop) {
		return false
	}
	return errors.Is(err, fault.ErrExecutionCancelled) || errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// isUnwindingAbort reports whether a recovered hard abort, cost stop or abandoned
// unwinding must bypass guest panic handling.
//
// unwindsAfter identifies the hard abort and cost stop cases.
//
// Takes value (any) which is a recovered panic value.
//
// Returns bool which is true for such a value.
func isUnwindingAbort(value any) bool {
	err, ok := value.(error)
	return ok && (unwindsAfter(err) || errors.Is(err, errUnwindAbandoned))
}

// Error describes the blocked defer.
//
// Returns string which is the message.
func (e *unwindBlockedError) Error() string {
	return fmt.Sprintf("%v after %v; %d deferred calls not run; the goroutine is leaked",
		fault.ErrUnwindBlocked, e.waited.Round(time.Millisecond), e.pending)
}

// Unwrap exposes fault.ErrUnwindBlocked.
//
// Returns error which is the sentinel.
func (*unwindBlockedError) Unwrap() error { return fault.ErrUnwindBlocked }

// joinUnwindReport adds an unwind report to the stop it belongs to.
//
// Takes stop (error) which is the VM's abort.
// Takes report (error) which is unwindAfterAbort's result, nil when nothing blocked.
//
// Returns error which is stop, or stop joined with the report.
func joinUnwindReport(stop, report error) error {
	if report == nil {
		return stop
	}
	return errors.Join(stop, report)
}

// abortUnwindSlice divides the remaining allowance by the number of pending defers,
// including the current one, and caps the result at perDeferMax.
//
// Takes left (int64) which is what the earlier defers left of the total.
// Takes pending (int) which counts the defers still to run, this one included.
// Takes perDeferMax (int64) which caps one defer's slice.
//
// Returns int64 which is the slice, never negative.
func abortUnwindSlice(left int64, pending int, perDeferMax int64) int64 {
	if left <= 0 {
		return 0
	}
	return min(left/int64(max(pending, 1)), perDeferMax)
}

// hasPendingDefers reports whether any frame of the VM still has a deferred call to run.
//
// Returns bool which is true when one has.
func (vm *VM) hasPendingDefers() bool {
	return vm.countPendingDefers() > 0
}

// countPendingDefers counts armed trivial-defer slots not yet consumed and defer-stack
// entries not yet started across the VM's frames.
//
// Returns int which is the count.
func (vm *VM) countPendingDefers() int {
	pending := 0
	for index := min(vm.FramePointer, len(vm.CallStack)-1); index >= 0; index-- {
		if record := vm.CallStack[index].simpleDefer; record != nil && record.active && !record.consumed {
			pending++
		}
	}
	for index := range vm.deferStack {
		if !vm.deferStack[index].started {
			pending++
		}
	}
	return pending
}

// unwindAfterAbort runs the VM's pending defers after a hard abort, a cost stop or a stop
// by cancellation under the unwinding allowance policy, supervising native calls against
// a wall-clock deadline.
//
// Returns error which is nil, or the unwind report when a defer was left blocked in
// native code (the VM and its arena are then leaked), or an invariant error when the VM
// has no context or the unwinding goroutine failed outside a deferred call.
//
// Concurrency: May run deferred calls in a supervised goroutine; the stopped VM must not
// be dispatched by another caller during unwinding.
func (vm *VM) unwindAfterAbort() error {
	vm.noteStopped()
	if vm.abortUnwinding || vm.CallStack == nil || vm.FramePointer < 0 || !vm.hasPendingDefers() {
		return nil
	}
	if vm.ctx == nil {
		return newInvariantError("abort unwinding found the VM without an execution context")
	}
	tracker := vm.Limits.Tracker
	if tracker != nil {
		tracker.unwinding.Add(1)
		defer tracker.unwinding.Add(-1)
	}
	abortUnwinds.started.Add(1)
	if vm.liveCtx != nil && vm.Arena != nil && vm.liveCtx.arenaBanksDirty != 0 {
		syncArenaIndicesFromASM(vm.Arena, vm.liveCtx)
		vm.liveCtx.arenaBanksDirty = 0
	}
	run := &abortUnwindRun{
		vm:           vm,
		tracker:      tracker,
		done:         make(chan struct{}),
		failure:      nil,
		deadline:     atomic.Int64{},
		deferStarted: atomic.Int64{},
		pendingNow:   atomic.Int64{},
		arenaCeiling: 0,
		remainder:    unwindShare{},
		allowance:    vm.Limits.abortUnwindAllowance(),
		nativeGrace:  vm.Limits.abortUnwindNativeGrace(),
		leakCap:      vm.Limits.leakedUnwinderCap(),
	}
	if vm.Arena != nil {
		run.arenaCeiling = vm.Arena.arenaBudgetLimit() + abortUnwindArenaBytes
	}
	go run.unwind(context.WithoutCancel(vm.ctx))
	return run.supervise()
}

// supervise waits for the unwinding goroutine, and gives up on it when a defer stays
// inside a native call past its wall deadline plus its native grace.
//
// Returns error which is nil when the unwinding finished, the invariant error when it
// failed outside a deferred call, or the unwind report.
func (run *abortUnwindRun) supervise() error {
	ticker := time.NewTicker(abortUnwindSuperviseTick)
	defer ticker.Stop()
	for {
		select {
		case <-run.done:
			return run.failure
		case now := <-ticker.C:
			deadline := run.deadline.Load()
			if deadline == 0 || run.vm.unwindNative.Load() == 0 || now.UnixNano() < deadline+int64(run.nativeGrace) {
				continue
			}
			return run.abandon(now)
		}
	}
}

// abandon marks the VM and its arena leaked and builds the unwind report. The unwinding
// goroutine stays blocked in its native call; when that returns it sees unwindLeaked and
// leaves the VM untouched.
//
// Takes now (time.Time) which is when the supervisor gave up.
//
// Returns error which is the report.
func (run *abortUnwindRun) abandon(now time.Time) error {
	vm := run.vm
	vm.unwindLeaked.Store(true)
	if vm.Arena != nil {
		vm.Arena.unwindLeaked = true
	}
	abortUnwinds.nativeBlocked.Add(1)
	blocked := &unwindBlockedError{
		pending: int(run.pendingNow.Load()),
		waited:  now.Sub(time.Unix(0, run.deferStarted.Load())),
	}
	if run.tracker == nil {
		return blocked
	}
	if limit := run.tracker.noteLeakedUnwinder(run.leakCap); limit != nil {
		return errors.Join(blocked, limit)
	}
	return blocked
}

// Error gives the count and the consequence.
//
// Returns string which is the message.
func (e *leakedUnwindersError) Error() string {
	return fmt.Sprintf("%v: %d left blocked; every later execution on this service fails",
		fault.ErrUnwindLeakLimit, e.leaked)
}

// Unwrap exposes fault.ErrUnwindLeakLimit.
//
// Returns error which is the sentinel.
func (*leakedUnwindersError) Unwrap() error { return fault.ErrUnwindLeakLimit }

// noteLeakedUnwinder counts an unwinding left blocked in native code and fails the
// service closed when the count reaches limit.
//
// Takes limit (int64) which is how many leaked unwindings the service allows.
//
// Returns error which is the fault.ErrUnwindLeakLimit report when this leak reached the
// limit, or nil.
func (t *ResourceTracker) noteLeakedUnwinder(limit int64) error {
	leaked := t.LeakedUnwinders.Add(1)
	if leaked < limit {
		return nil
	}
	report := &leakedUnwindersError{leaked: leaked}
	if !t.unwindersFailed.CompareAndSwap(nil, report) {
		return nil
	}
	return report
}

// unwind is the unwinding goroutine's body.
//
// It installs the unwinding state, runs every pending defer innermost first, and restores
// the VM, unless the supervisor gave up on it. A Go panic outside a deferred call (which
// callDeferred classifies) is an interpreter failure: it is logged once and handed to the
// supervisor as an invariant error rather than ending the host process.
func (run *abortUnwindRun) unwind(baseCtx context.Context) {
	vm := run.vm
	defer close(run.done)
	defer func() { run.recoverUnwind(baseCtx, recover()) }()
	unregister := registerAbortUnwinder(vm)
	defer unregister()
	restore := vm.enterAbortUnwind()
	defer func() {
		if !vm.unwindLeaked.Load() {
			restore()
		}
	}()
	if storeCensusBuilt {
		state, was := censusEnterBoundary(vm.Arena)
		defer censusLeaveBoundary(state, was)
	}
	if run.runPendingDefers(baseCtx) {
		abortUnwinds.completed.Add(1)
	}
}

// recoverUnwind turns a Go panic that escaped the unwinding goroutine, as its deferred
// call recovered it, into the invariant error the supervisor returns.
//
// Takes recovered (any) which is the recovered value, or nil when nothing panicked.
func (run *abortUnwindRun) recoverUnwind(baseCtx context.Context, recovered any) {
	if recovered == nil {
		return
	}
	logRecoveredUnwindPanic(baseCtx, "abort unwinding failed", recovered)
	abortUnwinds.stopped.Add(1)
	run.failure = newInvariantError("abort unwinding failed: %v", recovered)
}

// logRecoveredUnwindPanic logs a Go panic recovered during an unwinding, with its stack.
//
// Takes message (string) which says where it was recovered.
// Takes recovered (any) which is the recovered value.
func logRecoveredUnwindPanic(ctx context.Context, message string, recovered any) {
	_, logger := logging.From(ctx)
	logger.Warn(message,
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(debug.Stack())))
}

// abortUnwindShareAndReserve divides each allowance into pending-defer shares and nested
// reserves at abort.
//
// Takes total (int64) which is the allowance.
// Takes pending (int) which counts the defers pending at the abort.
// Takes perDeferMax (int64) which caps a share and a reserve.
// Takes floor (int64) which is the least reserve the share gives up units for.
//
// Returns share (int64) which is one defer's share.
// Returns reserve (int64) which is one defer's nested reserve.
func abortUnwindShareAndReserve(total int64, pending int, perDeferMax, floor int64) (share, reserve int64) {
	share = abortUnwindSlice(total, pending+1, perDeferMax)
	reserve = abortUnwindSlice(total-int64(max(pending, 0))*share, pending+1, perDeferMax)
	if raised := min(floor, share/2); reserve < raised {
		share, reserve = share-(raised-reserve), raised
	}
	return share, reserve
}

// abortUnwindReservedShare reserves min(total / (pending+1), perDeferMax) of each
// allowance for every defer pending at abort.
//
// Takes allowance (abortUnwindAllowance) which is the unwinding's cost and wall
// allowance.
// Takes pending (int) which counts the defers pending at the abort.
//
// Returns unwindShare which is one defer's share.
func abortUnwindReservedShare(allowance abortUnwindAllowance, pending int) unwindShare {
	cost, _ := abortUnwindShareAndReserve(allowance.cost, pending, abortUnwindDeferCostMax, abortUnwindNestedCostFloor)
	arena, _ := abortUnwindShareAndReserve(abortUnwindArenaBytes, pending, abortUnwindDeferArenaMax, abortUnwindNestedArenaFloor)
	wall, _ := abortUnwindShareAndReserve(int64(allowance.wall), pending, int64(allowance.deferWallMax), int64(abortUnwindNestedWallFloor))
	return unwindShare{cost: cost, arena: arena, wall: time.Duration(wall)}
}

// abortUnwindRemainder returns the allowance left after reserving shares for the defers
// pending at abort.
//
// Takes allowance (abortUnwindAllowance) which is the unwinding's cost and wall
// allowance.
// Takes pending (int) which counts the defers pending at the abort.
//
// Returns unwindShare which is the remainder.
func abortUnwindRemainder(allowance abortUnwindAllowance, pending int) unwindShare {
	share := abortUnwindReservedShare(allowance, pending)
	count := int64(max(pending, 0))
	return unwindShare{
		cost:  allowance.cost - count*share.cost,
		arena: abortUnwindArenaBytes - count*share.arena,
		wall:  allowance.wall - time.Duration(count)*share.wall,
	}
}

// abortUnwindNestedReserve reserves allowance for calls left pending when an original
// defer is cut off.
//
// Takes allowance (abortUnwindAllowance) which is the unwinding's cost and wall
// allowance.
// Takes pending (int) which counts the defers pending at the abort.
//
// Returns unwindShare which is one defer's reserve.
func abortUnwindNestedReserve(allowance abortUnwindAllowance, pending int) unwindShare {
	_, cost := abortUnwindShareAndReserve(allowance.cost, pending, abortUnwindDeferCostMax, abortUnwindNestedCostFloor)
	_, arena := abortUnwindShareAndReserve(abortUnwindArenaBytes, pending, abortUnwindDeferArenaMax, abortUnwindNestedArenaFloor)
	_, wall := abortUnwindShareAndReserve(int64(allowance.wall), pending, int64(allowance.deferWallMax), int64(abortUnwindNestedWallFloor))
	return unwindShare{cost: cost, arena: arena, wall: time.Duration(wall)}
}

// abortUnwindSharedRemainder subtracts pending defers' nested reserves from the
// remainder.
//
// Takes allowance (abortUnwindAllowance) which is the unwinding's cost and wall
// allowance.
// Takes pending (int) which counts the defers pending at the abort.
//
// Returns unwindShare which is the shared remainder.
func abortUnwindSharedRemainder(allowance abortUnwindAllowance, pending int) unwindShare {
	rest, reserve := abortUnwindRemainder(allowance, pending), abortUnwindNestedReserve(allowance, pending)
	count := int64(max(pending, 0))
	return unwindShare{
		cost:  rest.cost - count*reserve.cost,
		arena: rest.arena - count*reserve.arena,
		wall:  rest.wall - time.Duration(count)*reserve.wall,
	}
}

// fundNested funds a deferred call registered during unwinding, one allowance component
// at a time.
//
// Takes own (unwindShare) which is what the registering defer left.
// Takes remainder (unwindShare) which is the shared remainder.
// Takes pending (int) which counts the calls the registering defer left, this one
// included.
// Takes deferWallMax (time.Duration) which caps the wall slice.
//
// Returns unwindShare which is the slice.
// Returns unwindShareSources which says, per component (cost, arena, wall), whether it
// came from own.
func fundNested(own, remainder unwindShare, pending int, deferWallMax time.Duration) (unwindShare, unwindShareSources) {
	pick := func(own, remainder, perDeferMax int64) (int64, bool) {
		if own > 0 {
			return abortUnwindSlice(own, pending, perDeferMax), true
		}
		return abortUnwindSlice(remainder, pending, perDeferMax), false
	}
	var fromOwn unwindShareSources
	var share unwindShare
	share.cost, fromOwn[0] = pick(own.cost, remainder.cost, abortUnwindDeferCostMax)
	share.arena, fromOwn[1] = pick(own.arena, remainder.arena, abortUnwindDeferArenaMax)
	wall, wallOwn := pick(int64(own.wall), int64(remainder.wall), int64(deferWallMax))
	share.wall, fromOwn[2] = time.Duration(wall), wallOwn
	return share, fromOwn
}

// settleNested charges the sources used by fundNested for a nested call's consumption.
//
// Takes own (*unwindShare) which is what the registering defer left.
// Takes remainder (*unwindShare) which is the shared remainder.
// Takes spent (unwindShare) which is what the call is charged.
// Takes fromOwn (unwindShareSources) which is fundNested's per-component source.
func settleNested(own, remainder *unwindShare, spent unwindShare, fromOwn unwindShareSources) {
	take := func(fromOwn bool, own, remainder *int64, spent int64) {
		if fromOwn {
			*own = max(*own-spent, 0)
		} else {
			*remainder = max(*remainder-spent, 0)
		}
	}
	take(fromOwn[0], &own.cost, &remainder.cost, spent.cost)
	take(fromOwn[1], &own.arena, &remainder.arena, spent.arena)
	ownWall, remainderWall := int64(own.wall), int64(remainder.wall)
	take(fromOwn[2], &ownWall, &remainderWall, int64(spent.wall))
	own.wall, remainder.wall = time.Duration(ownWall), time.Duration(remainderWall)
}

// less returns pool less what a defer spent.
//
// Takes spent (unwindShare) which is the spend.
//
// Returns unwindShare which is the rest, never negative.
func (pool unwindShare) less(spent unwindShare) unwindShare {
	return unwindShare{cost: max(pool.cost-spent.cost, 0), arena: max(pool.arena-spent.arena, 0), wall: max(pool.wall-spent.wall, 0)}
}

// plus returns pool with extra added.
//
// Takes extra (unwindShare) which is the amount added.
//
// Returns unwindShare which is the sum.
func (pool unwindShare) plus(extra unwindShare) unwindShare {
	return unwindShare{cost: pool.cost + extra.cost, arena: pool.arena + extra.arena, wall: pool.wall + extra.wall}
}

// runPendingDefers runs the VM's pending defers innermost first.
//
// Each of the N defers pending at the abort gets the share abortUnwindReservedShare(N)
// reserved for it then, whatever the others do, and a nested reserve
// (abortUnwindNestedReserve) for the deferred calls it leaves pending if it is cut off;
// the shared remainder (abortUnwindSharedRemainder) is kept for deferred calls registered
// during the unwinding. Those, registered by a defer while it ran and left pending when
// it was cut off, run next, as under runtime.Goexit, funded by fundNested.
//
// Returns bool which is true when it reached the last pending defer.
func (run *abortUnwindRun) runPendingDefers(baseCtx context.Context) bool {
	vm := run.vm
	vm.FramePointer = min(vm.FramePointer, len(vm.CallStack)-1)
	pending := vm.countPendingDefers()
	reserved := abortUnwindReservedShare(run.allowance, pending)
	reserve := abortUnwindNestedReserve(run.allowance, pending)
	run.remainder = abortUnwindSharedRemainder(run.allowance, pending)
	return run.runDefersDownTo(baseCtx, 0, func(int) (unwindShare, func(unwindShare), unwindShare) {
		return reserved, func(unwindShare) {}, reserve
	})
}

// runFundedDefers runs the deferred calls a defer left pending, until floor remain, each
// with the slice fundNested gives it from own, what that defer left of its slice, or the
// unreserved remainder.
//
// Takes floor (int) which is the pending count to stop at.
// Takes own (unwindShare) which is what the registering defer left of its slice.
//
// Returns bool which is false when the unwinding must stop.
func (run *abortUnwindRun) runFundedDefers(baseCtx context.Context, floor int, own unwindShare) bool {
	return run.runDefersDownTo(baseCtx, floor, func(pending int) (unwindShare, func(unwindShare), unwindShare) {
		share, fromOwn := fundNested(own, run.remainder, pending-floor, run.allowance.deferWallMax)
		return share, func(spent unwindShare) { settleNested(&own, &run.remainder, spent, fromOwn) }, unwindShare{}
	})
}

// runDefersDownTo runs pending defers innermost first until floor remain.
//
// Takes floor (int) which is the pending count to stop at.
// Takes fund (func(int) (unwindShare, func(unwindShare), unwindShare)) which gives a
// defer its slice from the pending count, the function that settles its spend, and the
// reserve added to what it leaves for its own deferred calls.
//
// Returns bool which is false when the unwinding must stop.
func (run *abortUnwindRun) runDefersDownTo(baseCtx context.Context, floor int, fund func(int) (unwindShare, func(unwindShare), unwindShare)) bool {
	vm := run.vm
	for vm.FramePointer >= 0 {
		pending := vm.countPendingDefers()
		if pending <= floor {
			return true
		}
		call := vm.nextUnwindDefer()
		if call == nil {
			continue
		}
		if !run.runFundedDefer(baseCtx, call, pending, fund) {
			return false
		}
	}
	return true
}

// nextUnwindDefer takes the current frame's next deferred call for the unwinding.
//
// A consumed simple defer runs at once, a deferred call already started is dropped, and a
// frame with no defers left is popped; each returns nil so the caller looks again.
//
// Returns func() which runs the deferred call, or nil.
func (vm *VM) nextUnwindDefer() func() {
	frame := vm.currentFrame()
	switch record := frame.simpleDefer; {
	case record != nil && record.active && record.consumed:
		vm.runFrameSimpleDefer(frame)
		return nil
	case record != nil && record.active:
		return func() { vm.runFrameSimpleDefer(frame) }
	case len(vm.deferStack) > frame.deferBase:
		deferred := vm.deferStack[len(vm.deferStack)-1]
		if deferred.started {
			vm.deferStack = vm.deferStack[:len(vm.deferStack)-1]
			return nil
		}
		return func() {
			vm.deferStack = vm.deferStack[:len(vm.deferStack)-1]
			vm.executeDeferredCall(deferred)
		}
	default:
		vm.popFrame()
		return nil
	}
}

// runFundedDefer runs one deferred call on the slice fund gives it, settles what it
// spent, and funds the deferred calls it registered itself from what it left.
//
// Takes call (func()) which runs the deferred call.
// Takes pending (int) which counts the pending defers, this one included.
// Takes fund (func(int) (unwindShare, func(unwindShare), unwindShare)) which slices the
// allowance for it.
//
// Returns bool which is false when the call or a nested one stopped the unwinding.
func (run *abortUnwindRun) runFundedDefer(baseCtx context.Context, call func(), pending int, fund func(int) (unwindShare, func(unwindShare), unwindShare)) bool {
	share, settle, reserve := fund(pending)
	left, ok := run.runOneDefer(baseCtx, call, share)
	handsOn := ok && run.vm.countPendingDefers() > pending-1
	if handsOn {
		settle(share)
	} else {
		settle(share.less(left))
	}
	if !ok {
		return false
	}
	return !handsOn || run.runFundedDefers(baseCtx, pending-1, left.plus(reserve))
}

// runOneDefer runs one deferred call under its instruction allowance and settles what it
// spent.
//
// The call's context is a deadline on baseCtx at the end of its wall slice, with
// errUnwindDeferWallSpent as its cause.
//
// Takes call (func()) which pops and runs the deferred call.
// Takes share (unwindShare) which is the call's slice.
//
// Returns unwindShare which is what the call left of its slice.
// Returns bool which is false when the unwinding must stop.
func (run *abortUnwindRun) runOneDefer(baseCtx context.Context, call func(), share unwindShare) (unwindShare, bool) {
	vm := run.vm
	run.pendingNow.Store(int64(vm.countPendingDefers()))

	started := time.Now()
	ctx, cancel := context.WithDeadlineCause(baseCtx, started.Add(share.wall), errUnwindDeferWallSpent)
	vm.ctx, vm.ctxDone, vm.unwindDone = ctx, ctx.Done(), ctx.Done()
	vm.Cancelled.Store(0)
	vm.pollCause = nil
	vm.CostRemaining = share.cost
	vm.abortUnwindExhausted = false
	vm.evalError, vm.panicking, vm.panicValue = nil, false, nil
	var workingBefore uint64
	if arena := vm.Arena; arena != nil {
		workingBefore = arena.sharedWorkingSetBytes()
		arena.MaxArenaBytes = min(run.arenaCeiling, workingBefore+safeconv.Int64ToUint64(share.arena))
	}
	savedRecoverEligible := vm.recoverEligibleFrame
	savedPanicDeferFrames := len(vm.panicDeferFrames)
	run.deferStarted.Store(started.UnixNano())
	run.deadline.Store(started.Add(share.wall).UnixNano())
	abortUnwinds.defersRun.Add(1)

	outcome := run.callDeferred(call)

	run.deadline.Store(0)
	cancel()
	vm.recoverEligibleFrame = savedRecoverEligible
	if len(vm.panicDeferFrames) > savedPanicDeferFrames {
		vm.panicDeferFrames = vm.panicDeferFrames[:savedPanicDeferFrames]
	}
	left := unwindShare{wall: max(share.wall-time.Since(started), 0), cost: 0, arena: 0}
	if !vm.abortUnwindExhausted && vm.CostRemaining > 0 {
		left.cost = min(vm.CostRemaining, share.cost)
	}
	left.arena = share.arena
	if arena := vm.Arena; arena != nil {
		if working := arena.sharedWorkingSetBytes(); working > workingBefore {
			left.arena = max(share.arena-safeconv.Uint64ToInt64(min(working-workingBefore, safeconv.Int64ToUint64(share.arena))), 0)
		}
	}
	vm.evalError, vm.panicking, vm.panicValue = nil, false, nil
	vm.abortUnwindExhausted = false
	switch outcome {
	case deferCut:
		abortUnwinds.cut.Add(1)
	case deferStopsUnwinding:
		abortUnwinds.stopped.Add(1)
		return left, false
	default:
	}
	return left, true
}

// callDeferred runs one deferred call and classifies how it ended.
//
// Takes call (func()) which pops and runs the deferred call.
//
// Returns deferOutcome which is how it ended.
func (run *abortUnwindRun) callDeferred(call func()) (outcome deferOutcome) {
	vm := run.vm
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if vm.unwindLeaked.Load() {
			outcome = deferStopsUnwinding
			return
		}
		if isUnwindingAbort(recovered) {
			outcome = deferCut
			return
		}
		logRecoveredUnwindPanic(vm.ctx, "deferred call panicked during abort unwinding", recovered)
		if _, invariant := recovered.(*invariantError); invariant {
			outcome = deferStopsUnwinding
			return
		}
		abortUnwinds.deferPanics.Add(1)
		outcome = deferRan
	}()
	call()
	if vm.unwindLeaked.Load() {
		return deferStopsUnwinding
	}
	return classifyDeferredRun(vm)
}

// classifyDeferredRun classifies a deferred call that returned.
//
// Takes vm (*VM) which ran the call.
//
// Returns deferOutcome which is how it ended.
func classifyDeferredRun(vm *VM) deferOutcome {
	err := vm.evalError
	if vm.abortUnwindExhausted {
		return deferCut
	}
	if err == nil {
		if vm.panicking {
			abortUnwinds.deferPanics.Add(1)
		}
		return deferRan
	}
	if errors.Is(err, fault.ErrInterpreterInvariant) {
		return deferStopsUnwinding
	}
	if _, guestPanic := errors.AsType[*uncaughtPanicError](err); guestPanic && !unwindsAfter(err) {
		abortUnwinds.deferPanics.Add(1)
		return deferRan
	}
	return deferCut
}

// enterAbortUnwind installs metered Go dispatch and the unwinding cost table on the VM.
//
// It disables budget refills, goroutine-panic polling and the arena collector.
//
// Returns func() which restores what it changed.
func (vm *VM) enterAbortUnwind() func() {
	saved := abortUnwindSaved{
		limits:               vm.Limits,
		ctx:                  vm.ctx,
		blockCtx:             vm.blockCtx,
		panicValue:           vm.panicValue,
		pollCause:            vm.pollCause,
		ctxDone:              vm.ctxDone,
		blockCancel:          vm.blockCancel,
		callbackDeadline:     vm.callbackDeadline,
		costRemaining:        vm.CostRemaining,
		baseFramePointer:     vm.baseFramePointer,
		cancelled:            vm.Cancelled.Load(),
		recoverEligibleFrame: vm.recoverEligibleFrame,
		arenaMaxBytes:        0,
		arenaDisableMinorGC:  false,
	}
	vm.Limits.ForceGoDispatch = true
	vm.Limits.CostBudget = vm.Limits.abortUnwindAllowance().cost
	if vm.Limits.CostTable == nil {
		vm.Limits.CostTable = isa.SharedDefaultCostTable()
	}
	vm.abortUnwinding = true
	vm.abortUnwindExhausted = false
	vm.baseFramePointer = 0
	vm.panicking = false
	vm.panicValue = nil
	vm.evalError = nil
	vm.callbackDeadline = time.Time{}
	vm.blockCtx, vm.blockCancel = nil, nil
	vm.checkpointFlags &^= checkpointFlagGCPending
	arena := vm.Arena
	if arena != nil {
		saved.arenaMaxBytes, saved.arenaDisableMinorGC = arena.MaxArenaBytes, arena.disableMinorGC
		arena.disableMinorGC = true
	}
	return func() {
		vm.Limits = saved.limits
		vm.CostRemaining = saved.costRemaining
		vm.baseFramePointer = saved.baseFramePointer
		vm.panicValue = saved.panicValue
		vm.panicking = false
		vm.evalError = nil
		vm.ctx, vm.ctxDone = saved.ctx, saved.ctxDone
		vm.blockCtx, vm.blockCancel = saved.blockCtx, saved.blockCancel
		vm.callbackDeadline = saved.callbackDeadline
		vm.pollCause = saved.pollCause
		vm.recoverEligibleFrame = saved.recoverEligibleFrame
		vm.Cancelled.Store(saved.cancelled)
		if saved.ctx != nil && saved.ctx.Err() != nil {
			vm.Cancelled.Store(1)
		}
		vm.unwindDone = nil
		vm.abortUnwinding = false
		vm.abortUnwindExhausted = false
		if arena != nil {
			arena.MaxArenaBytes, arena.disableMinorGC = saved.arenaMaxBytes, saved.arenaDisableMinorGC
		}
	}
}

// unwindPeriodicCheck is runPeriodicChecks while the VM unwinds.
//
// Returns bool which is true when the defer must stop.
// Returns error which is the stop's error.
func (vm *VM) unwindPeriodicCheck() (bool, error) {
	select {
	case <-vm.unwindDone:
		vm.Cancelled.Store(1)
		return true, cancellationError(vm.ctx)
	default:
		return false, nil
	}
}

// enterUnwindNative marks the start of a native call made while the VM unwinds, so the
// supervisor can tell a defer blocked in native code.
//
// Returns bool which is true when leaveUnwindNative must be called.
func (vm *VM) enterUnwindNative() bool {
	if !vm.abortUnwinding {
		return false
	}
	vm.unwindNative.Add(1)
	return true
}

// leaveUnwindNative ends a native call made while the VM unwinds. When the supervisor
// gave up on the VM while the call was blocked, the unwinding goroutine leaves at once.
//
// Panics with errUnwindAbandoned when the VM was abandoned.
func (vm *VM) leaveUnwindNative() {
	vm.unwindNative.Add(-1)
	if vm.unwindLeaked.Load() {
		panic(errUnwindAbandoned)
	}
}
