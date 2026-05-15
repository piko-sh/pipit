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

package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

type debugRunResult struct {
	value any
	err   error
}

type debugHarness struct {
	dbg     *debug.Debugger
	service *Service
	done    chan debugRunResult
}

func newDebugHarness(t *testing.T, source string, exports map[string]reflect.Value, setup func(*debug.Debugger)) *debugHarness {
	t.Helper()
	dbg := debug.NewDebugger()
	service := NewService(WithDebugger(dbg))
	if exports != nil {
		service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": exports}))
	}
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	if setup != nil {
		setup(dbg)
	}
	harness := &debugHarness{dbg: dbg, service: service, done: make(chan debugRunResult, 1)}
	go func() {
		value, runErr := service.ExecuteEntrypoint(context.Background(), cfs, "run")
		harness.done <- debugRunResult{value: value, err: runErr}
	}()
	return harness
}

func (h *debugHarness) pause(t *testing.T) debug.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	event, err := h.dbg.WaitForPause(ctx)
	require.NoError(t, err)
	return event
}

func (h *debugHarness) finish(t *testing.T) debugRunResult {
	t.Helper()
	select {
	case result := <-h.done:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("the debugged execution did not finish")
		return debugRunResult{}
	}
}

func (h *debugHarness) waitLive(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !h.dbg.Session().Live() {
		require.Less(t, time.Now(), deadline, "the execution never started")
		time.Sleep(time.Millisecond)
	}
}

func TestDebuggerBreakpointInGoroutine(t *testing.T) {
	t.Parallel()
	source := `package main

func worker(out chan int) {
	v := 40
	out <- v + 2
}

func run() int {
	out := make(chan int)
	go worker(out)
	return <-out
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 5) })
	event := h.pause(t)
	require.Equal(t, debug.StopReasonBreakpoint, event.Reason)
	require.NotEqual(t, uint64(1), event.GoroutineID, "the worker runs as its own goroutine")
	require.Equal(t, "main.worker", event.Location.Function)
	require.Len(t, h.dbg.Threads(), 2)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 42, result.value)
}

func TestDebuggerBreakpointInHostCallback(t *testing.T) {
	t.Parallel()
	exports := map[string]reflect.Value{
		"Walk": reflect.ValueOf(func(fn func(int) int) int {
			total := 0
			for i := range 3 {
				total += fn(i)
			}
			return total
		}),
	}
	source := `package main

import "host"

func run() int {
	base := 10
	return host.Walk(func(i int) int {
		v := base + i
		return v
	})
}
`
	h := newDebugHarness(t, source, exports, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 9) })
	for hit := range 3 {
		event := h.pause(t)
		require.Equal(t, debug.StopReasonBreakpoint, event.Reason, "hit %d", hit)
		require.Equal(t, uint64(1), event.GoroutineID, "a callback shares its parent's goroutine")
		require.Equal(t, "main.run.func1", event.Location.Function)
		threads := h.dbg.Threads()
		var callback *debug.Thread
		for index := range threads {
			if threads[index].ID == event.ThreadID {
				callback = &threads[index]
			}
		}
		require.NotNil(t, callback)
		require.Equal(t, engine.DebugThreadCallback, callback.Kind)
		locals, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeLocals)
		require.NoError(t, err)
		require.Contains(t, localNames(locals), "v")
		require.NoError(t, h.dbg.Continue())
	}
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 33, result.value)
}

func TestDebuggerBreakpointInBoundMethod(t *testing.T) {
	t.Parallel()
	exports := map[string]reflect.Value{
		"Call": reflect.ValueOf(func(fn func(int) int) int { return fn(7) }),
	}
	source := `package main

import "host"

type counter struct{ n int }

func (c *counter) add(x int) int {
	c.n += x
	return c.n
}

func run() int {
	c := &counter{}
	return host.Call(c.add)
}
`
	h := newDebugHarness(t, source, exports, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 9) })
	event := h.pause(t)
	require.Equal(t, debug.StopReasonBreakpoint, event.Reason)
	require.Equal(t, "main.(*counter).add", event.Location.Function)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 7, result.value)
}

func TestDebuggerStopTheWorldParksOtherGoroutine(t *testing.T) {
	t.Parallel()
	source := `package main

func spin(stop chan bool, done chan int) {
	n := 0
	for {
		select {
		case <-stop:
			done <- n
			return
		default:
			n++
		}
	}
}

func run() int {
	stop := make(chan bool)
	done := make(chan int)
	go spin(stop, done)
	x := 1
	stop <- true
	return <-done + x
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 21) })
	event := h.pause(t)
	require.Equal(t, uint64(1), event.GoroutineID)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, h.dbg.Session().WaitQuiescent(ctx), "the spinning goroutine parks at its next instruction")
	threads := h.dbg.Threads()
	for deadline := time.Now().Add(10 * time.Second); len(threads) < 2 && time.Now().Before(deadline); threads = h.dbg.Threads() {

		time.Sleep(time.Millisecond)
		require.NoError(t, h.dbg.Session().WaitQuiescent(ctx))
	}
	require.Len(t, threads, 2)
	for _, thread := range threads {
		require.Equal(t, debug.ThreadPaused, thread.State, "thread %d", thread.ID)
		frames, err := h.dbg.StackTrace(thread.ID)
		require.NoError(t, err)
		require.NotEmpty(t, frames)
	}
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.Positive(t, result.value)
}

func TestDebuggerStepIntoCallbackAndOut(t *testing.T) {
	t.Parallel()
	exports := map[string]reflect.Value{
		"Call": reflect.ValueOf(func(fn func(int) int) int { return fn(7) }),
	}
	source := `package main

import "host"

func run() int {
	twice := func(i int) int {
		v := i * 2
		return v
	}
	total := host.Call(twice)
	total++
	return total
}
`
	h := newDebugHarness(t, source, exports, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 10) })
	event := h.pause(t)
	require.Equal(t, 10, event.Location.Line)
	require.NoError(t, h.dbg.StepIn(event.ThreadID))
	inside := h.pause(t)
	require.Equal(t, debug.StopReasonStep, inside.Reason)
	require.Equal(t, "main.run.func1", inside.Location.Function, "step-in entered the callback VM")
	require.Equal(t, event.GoroutineID, inside.GoroutineID)
	require.NoError(t, h.dbg.StepOut(inside.ThreadID))
	back := h.pause(t)
	require.Equal(t, debug.StopReasonStep, back.Reason)
	require.Equal(t, "main.run", back.Location.Function, "step-out returned to the parent VM")
	require.GreaterOrEqual(t, back.Location.Line, 10)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 15, result.value)
}

func TestDebuggerPauseOnPanicBeforeRecover(t *testing.T) {
	t.Parallel()
	source := `package main

func run() (r int) {
	defer func() {
		if recover() != nil {
			r = 7
		}
	}()
	panic("boom")
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) {
		dbg.SetExceptionBreakpoints([]debug.ExceptionFilter{debug.ExceptionFilterPanic})
	})
	event := h.pause(t)
	require.Equal(t, debug.StopReasonPanic, event.Reason)
	require.NotNil(t, event.Panic)
	require.Equal(t, "boom", event.Panic.Text)
	require.Equal(t, "main.run", event.Location.Function)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err, "the deferred recover still runs after the pause")
	require.EqualValues(t, 7, result.value)
}

func TestDebuggerPanicPauseThenStop(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	panic("boom")
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) {
		dbg.SetExceptionBreakpoints([]debug.ExceptionFilter{debug.ExceptionFilterPanic})
	})
	event := h.pause(t)
	require.Equal(t, debug.StopReasonPanic, event.Reason)
	h.dbg.Stop()
	result := h.finish(t)
	require.ErrorIs(t, result.err, fault.ErrDebuggerStop)
}

func TestDebuggerFunctionBreakpoint(t *testing.T) {
	t.Parallel()
	source := `package main

func helper(x int) int {
	return x + 1
}

func run() int {
	f := func() int { return helper(1) }
	return f() + helper(2)
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) {
		results := dbg.SetFunctionBreakpoints([]debug.FunctionBreakpoint{{Name: "main.helper", Condition: "", HitCondition: ""}, {Name: "main.run.func1", Condition: "", HitCondition: ""}})
		require.Len(t, results, 2)
		require.True(t, results[0].Verified)
	})
	first := h.pause(t)
	require.Equal(t, debug.StopReasonFunctionBreakpoint, first.Reason)
	require.Equal(t, "main.run.func1", first.Location.Function)
	require.NoError(t, h.dbg.Continue())
	second := h.pause(t)
	require.Equal(t, "main.helper", second.Location.Function)
	require.NoError(t, h.dbg.Continue())
	third := h.pause(t)
	require.Equal(t, "main.helper", third.Location.Function)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 5, result.value)
}

func TestDebuggerStopWhileRunningFree(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	n := 0
	for {
		n++
	}
}
`
	h := newDebugHarness(t, source, nil, nil)
	h.waitLive(t)
	h.dbg.Stop()
	result := h.finish(t)
	require.ErrorIs(t, result.err, fault.ErrDebuggerStop)
	require.Empty(t, h.dbg.Threads())
}

func TestDebuggerPauseRequestParksRunningLoop(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	n := 0
	for {
		n++
	}
}
`
	h := newDebugHarness(t, source, nil, nil)
	h.waitLive(t)
	require.NoError(t, h.dbg.Pause())
	event := h.pause(t)
	require.Equal(t, debug.StopReasonPause, event.Reason)
	require.Equal(t, "main.run", event.Location.Function)
	require.ErrorIs(t, h.dbg.Pause(), fault.ErrDebugAlreadyPaused)
	h.dbg.Stop()
	result := h.finish(t)
	require.ErrorIs(t, result.err, fault.ErrDebuggerStop)
}

func TestDebuggerEntryStopsOnEntrypointNotVarInit(t *testing.T) {
	t.Parallel()
	source := `package main

func compute() int {
	x := 20
	return x + 1
}

var g = compute()

func init() {
	g++
}

func run() int {
	return g * 2
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) { dbg.SetPauseOnEntry(true) })
	event := h.pause(t)
	require.Equal(t, debug.StopReasonEntry, event.Reason)
	require.Equal(t, "main.run", event.Location.Function)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 44, result.value)
}

func TestDebuggerBreakpointsInVarInitAndInit(t *testing.T) {
	t.Parallel()
	source := `package main

func compute() int {
	x := 20
	return x + 1
}

var g = compute()

func init() {
	g++
}

func run() int {
	return g * 2
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) {
		dbg.SetBreakpoint("main.go", 5)
		dbg.SetBreakpoint("main.go", 11)
	})
	first := h.pause(t)
	require.Equal(t, 5, first.Location.Line, "the variable initialiser is debuggable")
	require.NoError(t, h.dbg.Continue())
	second := h.pause(t)
	require.Equal(t, 11, second.Location.Line, "init functions are debuggable")
	require.Equal(t, "main.init", second.Location.Function)
	require.NoError(t, h.dbg.Continue())
	result := h.finish(t)
	require.NoError(t, result.err)
	require.EqualValues(t, 44, result.value)
}

func TestDebuggerExitedEventFollowsThreadEvents(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int { return 3 }
`
	h := newDebugHarness(t, source, nil, nil)
	result := h.finish(t)
	require.NoError(t, result.err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var kinds []debug.EventKind
	for {
		event, err := h.dbg.WaitForEvent(ctx)
		require.NoError(t, err)
		kinds = append(kinds, event.Kind)
		if event.Kind == debug.EventExited {
			require.NoError(t, event.Err)
			break
		}
	}
	require.Equal(t, debug.EventThreadStarted, kinds[0])
	require.Equal(t, debug.EventThreadExited, kinds[len(kinds)-2])
	quiet, quietCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer quietCancel()
	_, err := h.dbg.WaitForPause(quiet)
	require.ErrorIs(t, err, context.DeadlineExceeded, "nothing else is queued")
}

func TestDebuggerInitsRunOnceAcrossExecuteInitsAndEntrypoint(t *testing.T) {
	t.Parallel()
	calls := 0
	exports := map[string]reflect.Value{"Count": reflect.ValueOf(func() { calls++ })}
	source := `package main

import "host"

func init() { host.Count() }

func run() int { return 1 }
`
	dbg := debug.NewDebugger()
	service := NewService(WithDebugger(dbg))
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": exports}))
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(context.Background(), cfs))
	_, err = service.ExecuteEntrypoint(context.Background(), cfs, "run")
	require.NoError(t, err)
	require.Equal(t, 1, calls, "ExecuteInits followed by ExecuteEntrypoint runs init once")
	_, err = service.ExecuteEntrypoint(context.Background(), cfs, "run")
	require.NoError(t, err)
	require.Equal(t, 2, calls, "a later entrypoint run initialises again as before")
}

func TestDebuggerCancelWakesPausedThread(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	x := 1
	return x
}
`
	dbg := debug.NewDebugger()
	service := NewService(WithDebugger(dbg))
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	dbg.SetBreakpoint("main.go", 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, runErr := service.ExecuteEntrypoint(ctx, cfs, "run")
		done <- runErr
	}()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer waitCancel()
	_, err = dbg.WaitForPause(waitCtx)
	require.NoError(t, err)
	cancel()
	select {
	case runErr := <-done:
		require.ErrorIs(t, runErr, fault.ErrExecutionCancelled)
	case <-time.After(30 * time.Second):
		t.Fatal("cancellation did not wake the paused VM")
	}
}

func localNames(variables []debug.VariableInfo) []string {
	names := make([]string, 0, len(variables))
	for _, variable := range variables {
		names = append(names, variable.Name)
	}
	return names
}
