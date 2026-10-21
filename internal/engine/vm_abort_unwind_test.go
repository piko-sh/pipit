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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/logging"
)

func TestUnwindsAfterRunCoversEveryStop(t *testing.T) {
	cancelledWith := func(cause error) context.Context {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(cause)
		return ctx
	}
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancelExpired()
	executionDeadline, cancelExecutionDeadline := context.WithTimeoutCause(t.Context(), 0, fault.ErrExecutionCancelled)
	defer cancelExecutionDeadline()
	mainReturned := cancelledWith(fault.ErrMainReturned)
	hostCancelled := cancelledWith(nil)
	deadlocked := cancelledWith(fault.ErrDeadlock)
	debuggerStopped := cancelledWith(fault.ErrDebuggerStop)
	callbackTimeout := (&VM{ctx: t.Context(), pollCause: fault.ErrCallbackTimeout}).stopError()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "main returned", ctx: mainReturned, err: cancellationError(mainReturned), want: true},
		{name: "host cancelled", ctx: hostCancelled, err: cancellationError(hostCancelled), want: true},
		{name: "deadline", ctx: expired, err: cancellationError(expired), want: true},
		{name: "execution deadline", ctx: executionDeadline, err: cancellationError(executionDeadline), want: true},
		{name: "callback deadline", ctx: t.Context(), err: callbackTimeout, want: true},
		{name: "deadlock", ctx: deadlocked, err: cancellationError(deadlocked), want: false},
		{name: "debugger stop", ctx: debuggerStopped, err: cancellationError(debuggerStopped), want: false},
		{name: "guest panic after main returned", ctx: mainReturned, err: newGoroutinePanicError("boom"), want: false},
		{name: "guest panic carrying a cancellation, live context", ctx: t.Context(), err: newGoroutinePanicError(context.Canceled), want: false},
		{name: "sibling's cancellation, own context cancelled", ctx: hostCancelled, err: newGoroutinePanicError(cancellationError(hostCancelled)), want: true},
		{name: "sibling's cancellation, own context deadlocked", ctx: deadlocked, err: newGoroutinePanicError(cancellationError(hostCancelled)), want: false},
		{name: "arena abort", ctx: t.Context(), err: fmt.Errorf("arena: %w", fault.ErrArenaBudgetExceeded), want: true},
		{name: "cost stop", ctx: t.Context(), err: fault.ErrCostBudgetExceeded, want: true},
		{name: "plain error", ctx: hostCancelled, err: errors.New("boom"), want: false},
	}
	for _, c := range cases {
		vm := &VM{ctx: c.ctx}
		require.Equal(t, c.want, vm.unwindsAfterRun(c.err), "%s: %v", c.name, c.err)
	}
	require.True(t, errors.Is(cancellationError(mainReturned), fault.ErrExecutionCancelled), "the stop is a cancellation")
}

func TestUnwindingIsCountedOnTheTracker(t *testing.T) {
	vm, frame, _ := newStandardVM(t)
	frame.deferBase = 0
	tracker := &ResourceTracker{}
	vm.Limits.Tracker = tracker
	var during int32
	vm.deferStack = []deferredCall{{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {
		during = tracker.unwinding.Load()
	})}}

	require.NoError(t, vm.unwindAfterAbort())

	require.Equal(t, int32(1), during, "the VM was counted while its defer ran")
	require.Zero(t, tracker.unwinding.Load(), "the count was released")
}

func TestAbortUnwindSlicePolicy(t *testing.T) {
	require.Equal(t, int64(0), abortUnwindSlice(0, 3, 50))
	require.Equal(t, int64(0), abortUnwindSlice(-5, 3, 50))
	require.Equal(t, int64(50), abortUnwindSlice(1000, 1, 50))
	require.Equal(t, int64(33), abortUnwindSlice(100, 3, 50))
	require.Equal(t, int64(100), abortUnwindSlice(100, 0, 1000), "a count of zero is treated as one")

	for _, total := range []int64{defaultAbortUnwindCost, 7, 1000, 99_999} {
		for pending := 1; pending <= 40; pending++ {
			floor := min(total/int64(pending), abortUnwindDeferCostMax)
			left := total
			for remaining := pending; remaining >= 1; remaining-- {
				slice := abortUnwindSlice(left, remaining, abortUnwindDeferCostMax)
				require.GreaterOrEqual(t, slice, floor, "total %d pending %d remaining %d", total, pending, remaining)
				left -= slice
			}
			require.GreaterOrEqual(t, left, int64(0))
		}
	}
}

func TestAbortUnwindReservedShares(t *testing.T) {
	require.Equal(t, unwindShare{cost: abortUnwindDeferCostMax, arena: abortUnwindDeferArenaMax, wall: defaultAbortUnwindDeferWallMax}, abortUnwindReservedShare(defaultAbortUnwindAllowance, 1))
	for pending := 1; pending <= 64; pending++ {
		share, rest := abortUnwindReservedShare(defaultAbortUnwindAllowance, pending), abortUnwindRemainder(defaultAbortUnwindAllowance, pending)
		count := int64(pending)
		require.Equal(t, int64(defaultAbortUnwindCost), share.cost*count+rest.cost)
		require.Equal(t, int64(abortUnwindArenaBytes), share.arena*count+rest.arena)
		require.Equal(t, defaultAbortUnwindWall, share.wall*time.Duration(count)+rest.wall)
		require.GreaterOrEqual(t, rest.cost, share.cost)
		require.GreaterOrEqual(t, rest.wall, share.wall)
		reserve, shared := abortUnwindNestedReserve(defaultAbortUnwindAllowance, pending), abortUnwindSharedRemainder(defaultAbortUnwindAllowance, pending)
		require.Equal(t, rest, shared.plus(unwindShare{cost: reserve.cost * count, arena: reserve.arena * count, wall: reserve.wall * time.Duration(count)}))
		require.GreaterOrEqual(t, shared.cost, int64(0))
		require.GreaterOrEqual(t, shared.wall, time.Duration(0))
		require.Positive(t, reserve.cost, "pending %d: a cut-off defer's unlock has a reserve", pending)
	}
	own := unwindShare{cost: 900, wall: 90 * time.Millisecond}
	remainder := unwindShare{cost: 30_000, arena: 3000, wall: 300 * time.Millisecond}
	share, fromOwn := fundNested(own, remainder, 3, defaultAbortUnwindDeferWallMax)
	require.Equal(t, unwindShare{cost: 300, arena: 1000, wall: 30 * time.Millisecond}, share)
	require.Equal(t, unwindShareSources{true, false, true}, fromOwn)
	settleNested(&own, &remainder, unwindShare{cost: 300, arena: 400, wall: 10 * time.Millisecond}, fromOwn)
	require.Equal(t, unwindShare{cost: 600, wall: 80 * time.Millisecond}, own)
	require.Equal(t, unwindShare{cost: 30_000, arena: 2600, wall: 300 * time.Millisecond}, remainder)
	share, fromOwn = fundNested(unwindShare{}, remainder, 2, defaultAbortUnwindDeferWallMax)
	require.Equal(t, unwindShare{cost: 15_000, arena: 1300, wall: 150 * time.Millisecond}, share)
	require.Equal(t, unwindShareSources{}, fromOwn, "a spent registrar's nested calls draw on the remainder")
	require.Equal(t, unwindShare{}, own.less(unwindShare{cost: 1000, arena: 1000, wall: time.Second}))
}

func TestAbortUnwindNestedReserveHasAFloor(t *testing.T) {
	share, reserve := abortUnwindReservedShare(defaultAbortUnwindAllowance, 329), abortUnwindNestedReserve(defaultAbortUnwindAllowance, 329)
	require.Equal(t, int64(239), share.cost)
	require.Equal(t, int64(64), reserve.cost)
	require.Equal(t, time.Millisecond, reserve.wall)
	for pending := 1; pending <= 49_999; pending++ {
		count := int64(pending)
		share, reserve := abortUnwindReservedShare(defaultAbortUnwindAllowance, pending), abortUnwindNestedReserve(defaultAbortUnwindAllowance, pending)
		share0 := abortUnwindSlice(defaultAbortUnwindCost, pending+1, abortUnwindDeferCostMax)
		reserve0 := abortUnwindSlice(defaultAbortUnwindCost-count*share0, pending+1, abortUnwindDeferCostMax)
		require.GreaterOrEqual(t, reserve.cost, min(abortUnwindNestedCostFloor, share0/2), "pending %d", pending)
		require.Positive(t, reserve.cost, "pending %d: a cut-off defer's unlock has a reserve", pending)
		require.Equal(t, share0+reserve0, share.cost+reserve.cost, "pending %d: the floor comes from the share", pending)
		require.GreaterOrEqual(t, share.cost, share0/2, "pending %d", pending)
		require.Positive(t, reserve.wall, "pending %d", pending)
		shared := abortUnwindSharedRemainder(defaultAbortUnwindAllowance, pending)
		require.Equal(t, int64(defaultAbortUnwindCost), count*share.cost+count*reserve.cost+shared.cost, "pending %d", pending)
		require.Equal(t, int64(abortUnwindArenaBytes), count*share.arena+count*reserve.arena+shared.arena, "pending %d", pending)
		require.Equal(t, defaultAbortUnwindWall, time.Duration(count)*(share.wall+reserve.wall)+shared.wall, "pending %d", pending)
		require.GreaterOrEqual(t, shared.cost, int64(0))
	}
}

func TestAbortUnwindNeverSpendsMoreThanTheTotal(t *testing.T) {
	vm, frame, _ := newStandardVM(t)
	frame.deferBase = 0
	var spent int64
	var ran []string
	native := func(name string, body func()) deferredCall {
		return deferredCall{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {
			ran = append(ran, name)
			body()
		})}
	}
	spendAll := func() {
		spent += vm.CostRemaining
		vm.CostRemaining = 0
		panic(fault.ErrCostBudgetExceeded)
	}
	cutWithCostLeft := func(name string) deferredCall {
		return native(name, func() {
			vm.deferStack = append(vm.deferStack, native(name+".loop", spendAll))
			spent++
			vm.CostRemaining--
			panic(fault.ErrArenaBudgetExceeded)
		})
	}
	vm.deferStack = []deferredCall{native("top", func() {
		vm.deferStack = append(vm.deferStack, cutWithCostLeft("first"), cutWithCostLeft("second"))
		spendAll()
	})}

	require.NoError(t, vm.unwindAfterAbort())

	require.Equal(t, []string{"top", "second", "second.loop", "first", "first.loop"}, ran)
	require.LessOrEqual(t, spent, int64(defaultAbortUnwindCost), "the unwinding spent more than its total")
}

func TestAbortUnwindDeferDeadlineNamesItsCause(t *testing.T) {
	vm, frame, _ := newStandardVM(t)
	frame.deferBase = 0
	var cause error
	vm.deferStack = []deferredCall{{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {
		<-vm.ctx.Done()
		cause = context.Cause(vm.ctx)
	})}}

	require.NoError(t, vm.unwindAfterAbort())

	require.ErrorIs(t, cause, errUnwindDeferWallSpent, "a defer cut by its wall slice says so")
}

func TestAbortUnwindWithoutAContextIsAnInvariantFailure(t *testing.T) {
	vm, frame, _ := newStandardVM(t)
	frame.deferBase = 0
	ran := false
	vm.deferStack = []deferredCall{{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {
		ran = true
	})}}
	vm.ctx = nil

	err := vm.unwindAfterAbort()

	require.ErrorIs(t, err, fault.ErrInterpreterInvariant)
	require.False(t, ran, "no defer runs without the execution's context")
}

func newLoggedVM(t *testing.T) (*VM, *CallFrame, *bytes.Buffer) {
	t.Helper()
	vm, frame, _ := newStandardVM(t)
	frame.deferBase = 0
	var logged bytes.Buffer
	vm.ctx = logging.WithLogger(vm.ctx, slog.New(slog.NewTextHandler(&logged, nil)))
	return vm, frame, &logged
}

func TestAbortUnwindLogsAGoPanicInADefer(t *testing.T) {
	cases := []struct {
		name   string
		raise  any
		want   deferOutcome
		logged bool
	}{
		{name: "engine panic", raise: "engine bug", want: deferRan, logged: true},
		{name: "invariant failure", raise: newInvariantError("broken frame"), want: deferStopsUnwinding, logged: true},
		{name: "cost stop", raise: fault.ErrCostBudgetExceeded, want: deferCut, logged: false},
		{name: "arena abort", raise: fmt.Errorf("arena: %w", fault.ErrArenaBudgetExceeded), want: deferCut, logged: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			vm, _, logged := newLoggedVM(t)
			run := &abortUnwindRun{vm: vm}

			outcome := run.callDeferred(func() { panic(testCase.raise) })

			require.Equal(t, testCase.want, outcome)
			if !testCase.logged {
				require.Empty(t, logged.String(), "a budget stop is not an engine failure")
				return
			}
			require.Contains(t, logged.String(), fmt.Sprint(testCase.raise))
			require.Contains(t, logged.String(), "stack=", "the stack is logged")
			require.Equal(t, 1, bytes.Count(logged.Bytes(), []byte("level=WARN")), "logged once")
		})
	}
}

func TestAbortUnwindRecoversAPanicOutsideADefer(t *testing.T) {
	vm, _, logged := newLoggedVM(t)
	later := deferredCall{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {})}
	vm.deferStack = []deferredCall{{frameIndex: vm.FramePointer, nativeFunction: reflect.ValueOf(func() {
		vm.deferStack = append(vm.deferStack, later)
		vm.CallStack = nil
	})}}

	err := vm.unwindAfterAbort()

	require.ErrorIs(t, err, fault.ErrInterpreterInvariant, "the supervisor reports the failed unwinding")
	require.Contains(t, logged.String(), "stack=")
}

func TestLeakedUnwindersFailTheServiceClosed(t *testing.T) {
	tracker := &ResourceTracker{}
	leak := func() error {
		vm, _, _ := newStandardVM(t)
		vm.Limits.Tracker = tracker
		require.NoError(t, vm.failedClosed())
		run := &abortUnwindRun{vm: vm, tracker: tracker, leakCap: vm.Limits.leakedUnwinderCap()}
		run.deferStarted.Store(time.Now().UnixNano())
		return run.abandon(time.Now())
	}
	for range defaultLeakedUnwinderCap - 1 {
		report := leak()
		require.ErrorIs(t, report, fault.ErrUnwindBlocked)
		require.NotErrorIs(t, report, fault.ErrUnwindLeakLimit)
	}

	report := leak()

	require.ErrorIs(t, report, fault.ErrUnwindBlocked)
	require.ErrorIs(t, report, fault.ErrUnwindLeakLimit, "the unwinding that reaches the cap says so")
	vm, _, _ := newStandardVM(t)
	vm.Limits.Tracker = tracker
	require.ErrorIs(t, vm.failedClosed(), fault.ErrUnwindLeakLimit, "every later execution fails")
	require.Equal(t, int64(defaultLeakedUnwinderCap), tracker.LeakedUnwinders.Load())
}
