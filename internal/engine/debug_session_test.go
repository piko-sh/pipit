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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestDebugStopRequestedIsFreeWithoutASession(t *testing.T) {
	vm := newTestVM(t)
	frame := &CallFrame{Function: &program.CompiledFunction{}}
	require.Zero(t, testing.AllocsPerRun(1000, func() {
		if vm.debugStopRequested(frame) {
			t.Fatal("no session must never stop")
		}
	}))
	vm.Limits.Debug = NewDebugSession()
	require.Zero(t, testing.AllocsPerRun(1000, func() {
		if vm.debugStopRequested(frame) {
			t.Fatal("an idle session must never stop")
		}
	}), "an idle session costs one atomic load and no allocation")
}

func TestDebugSessionActiveFlagFollowsPauseConditions(t *testing.T) {
	t.Parallel()
	session := NewDebugSession()
	require.Zero(t, session.active.Load())

	id := session.SetBreakpoint("main.go", 3, DebugBreakpoint{Condition: "", HitCondition: program.HitCondition{}, ID: 0, HitCount: 0})
	require.Equal(t, 1, id)
	require.EqualValues(t, 1, session.active.Load())
	session.ClearBreakpoint("main.go", 3)
	require.Zero(t, session.active.Load())

	session.SetFunctionBreakpoint("main.run", DebugBreakpoint{Condition: "", HitCondition: program.HitCondition{}, ID: 0, HitCount: 0})
	require.EqualValues(t, 1, session.active.Load())
	session.ClearFunctionBreakpoints()
	require.Zero(t, session.active.Load())

	session.SetPauseOnPanic(true)
	require.EqualValues(t, 1, session.active.Load())
	session.SetPauseOnPanic(false)
	session.SetStopOnEntry(true)
	require.EqualValues(t, 1, session.active.Load())
	session.SetStopOnEntry(false)
	require.Zero(t, session.active.Load())
}

func TestDebugSessionIdleControlsAreRefused(t *testing.T) {
	t.Parallel()
	session := NewDebugSession()
	require.ErrorIs(t, session.RequestPause(), fault.ErrDebugNotRunning)
	require.ErrorIs(t, session.Resume(1, program.DebugActionContinue), fault.ErrDebugNotPaused)
	require.ErrorIs(t, session.WaitQuiescent(context.Background()), fault.ErrDebugNotPaused)
	session.Stop()
	require.False(t, session.terminated, "stopping an idle session is a no-op")
	require.Empty(t, session.Threads())
	_, ok := session.FocusThread()
	require.False(t, ok)
}

func TestDebugSessionRegistryTracksRunLoops(t *testing.T) {
	t.Parallel()
	session := NewDebugSession()
	vm := newTestVM(t)
	vm.Limits.Debug = session
	vm.debugKind = debugThreadGoroutine

	vm.debugEnter()
	require.NotNil(t, vm.debugThread)
	vm.debugEnter()
	require.Len(t, session.Threads(), 1, "a nested run does not register twice")
	info := session.Threads()[0]
	require.Equal(t, debugThreadGoroutine, info.Kind)
	require.Equal(t, debugThreadRunning, info.State)
	require.True(t, session.Live())

	vm.debugLeave()
	require.Len(t, session.Threads(), 1, "leaving the nested run keeps the registration")
	vm.debugLeave()
	require.Empty(t, session.Threads())
	require.Nil(t, vm.debugThread)

	started, ok := session.TryNextEvent()
	require.True(t, ok)
	require.Equal(t, DebugSessionEventThreadStarted, started.Kind)
	exited, ok := session.TryNextEvent()
	require.True(t, ok)
	require.Equal(t, DebugSessionEventThreadExited, exited.Kind)
	require.Equal(t, started.Thread.ID, exited.Thread.ID)
	_, ok = session.TryNextEvent()
	require.False(t, ok)
}

func TestDebugSessionEventQueueDropsThreadEventsFirst(t *testing.T) {
	t.Parallel()
	session := NewDebugSession()
	pause := new(DebugPause)
	session.mu.Lock()
	session.appendEventLocked(DebugSessionEvent{Pause: pause, Thread: DebugThreadInfo{}, Kind: DebugSessionEventPaused})
	for range debugPendingEventCap + 10 {
		session.appendEventLocked(DebugSessionEvent{Pause: nil, Thread: DebugThreadInfo{}, Kind: DebugSessionEventThreadStarted})
	}
	session.mu.Unlock()
	first, ok := session.TryNextEvent()
	require.True(t, ok)
	require.Equal(t, DebugSessionEventPaused, first.Kind, "pause events are never dropped")
	count := 1
	for {
		if _, ok := session.TryNextEvent(); !ok {
			break
		}
		count++
	}
	require.Equal(t, debugPendingEventCap, count)
}

func TestDebugSessionNextEventHonoursCancellation(t *testing.T) {
	t.Parallel()
	session := NewDebugSession()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := session.nextEvent(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDebugFramesUseRuntimeNames(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	callee := &program.CompiledFunction{Name: "<closure>", RuntimeName: "main.run.func1", Body: make([]isa.Instruction, 4)}
	caller := &program.CompiledFunction{Name: "run", Body: make([]isa.Instruction, 4)}
	vm.CallStack = []CallFrame{{Function: caller, ProgramCounter: 3}, {Function: callee, ProgramCounter: 1}}
	vm.FramePointer = 1
	frames := vm.debugFrames(0)
	require.Len(t, frames, 2)
	require.Equal(t, "main.run.func1", frames[0].FunctionName)
	require.Equal(t, 0, frames[0].PC, "the paused frame reports the instruction about to execute")
	require.Equal(t, 1, frames[0].callStackIndex)
	require.Equal(t, "run", frames[1].FunctionName, "a function without a runtime name falls back to its table name")
	require.Equal(t, 2, frames[1].PC, "a caller reports the call instruction")
}
