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

package debug_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugtest"
	"pipit.sh/pipit/internal/fault"
)

const debuggedProgram = `package main

var total = 5

func add(a, b int) int {
	sum := a + b
	return sum
}

func run() int {
	values := []int{1, 2, 3}
	count := 0
	for _, value := range values {
		count = add(count, value)
	}
	total += count
	return total
}
`

type session struct {
	debugger  *debug.Debugger
	execution *debugtest.Execution
}

func startSession(t *testing.T, source string, setup func(*debug.Debugger)) *session {
	t.Helper()
	debugger := debug.NewDebugger()
	service := app.NewService(app.WithDebugger(debugger))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	if setup != nil {
		setup(debugger)
	}
	execution := debugtest.Run(t, debugger, func() (any, error) {
		return service.ExecuteEntrypoint(t.Context(), compiled, "run")
	})
	return &session{debugger: debugger, execution: execution}
}

func variable(t *testing.T, variables []debug.VariableInfo, name string) string {
	t.Helper()
	for _, candidate := range variables {
		if candidate.Name == name {
			return fmt.Sprint(candidate.Value)
		}
	}
	require.Failf(t, "variable not listed", "%s is not among %v", name, variables)
	return ""
}

func TestABreakpointPausesWithTheFramesScopesAndVariablesOfItsLine(t *testing.T) {
	t.Parallel()
	var results []debug.BreakpointResult
	run := startSession(t, debuggedProgram, func(debugger *debug.Debugger) {
		results = debugger.SetBreakpoints("main.go", []debug.Breakpoint{
			{Line: 6, Condition: "a > 0", HitCondition: ""},
			{Line: 400, Condition: "", HitCondition: ""},
			{Line: 7, Condition: "", HitCondition: "not a number"},
		})
	})
	require.Len(t, results, 3)
	require.True(t, results[0].Verified)
	require.Equal(t, 6, results[0].Line)
	require.False(t, results[2].Verified, "a hit condition that does not parse is refused")

	event := debugtest.WaitPause(t, run.debugger)
	require.Equal(t, debug.StopReasonBreakpoint, event.Reason)
	require.Equal(t, "breakpoint", event.Reason.String())
	require.Equal(t, "paused", event.Kind.String())
	require.Equal(t, "main.add", event.Location.Function)
	require.Equal(t, 6, event.Location.Line)

	threads := run.debugger.Threads()
	require.NotEmpty(t, threads)
	frames := debugtest.StackOf(t, run.debugger, event)
	require.GreaterOrEqual(t, len(frames), 2)
	require.Equal(t, "main.add", frames[0].Function)
	require.Equal(t, "main.run", frames[1].Function)

	scopes, err := run.debugger.Scopes(event.ThreadID, 0)
	require.NoError(t, err)
	require.NotEmpty(t, scopes)
	locals, err := run.debugger.Variables(event.ThreadID, 0, debug.ScopeLocals)
	require.NoError(t, err)
	require.Equal(t, "1", variable(t, locals, "a"), "the condition skipped the call with a == 0")
	globals, err := run.debugger.Variables(event.ThreadID, 0, debug.ScopeGlobals)
	require.NoError(t, err)
	require.Equal(t, "5", variable(t, globals, "total"))

	evaluated, err := run.debugger.Evaluate(t.Context(), event.ThreadID, 0, "a*10 + b", debug.EvalOptions{Timeout: 0, AllowCalls: false})
	require.NoError(t, err)
	require.EqualValues(t, 12, evaluated.Value)
	_, err = run.debugger.Evaluate(t.Context(), event.ThreadID, 0, "missing + 1", debug.EvalOptions{Timeout: 0, AllowCalls: false})
	require.Error(t, err)

	_, err = run.debugger.SetVariable(event.ThreadID, 0, "b", 100)
	require.NoError(t, err)
	_, err = run.debugger.Variables(event.ThreadID, 99, debug.ScopeLocals)
	require.ErrorIs(t, err, fault.ErrDebugFrameOutOfRange)

	run.debugger.ClearBreakpoint("main.go", 6)
	require.NoError(t, run.debugger.Continue())
	value, err := run.execution.Wait(t)
	require.NoError(t, err)
	require.EqualValues(t, 5+1+100+3, value, "the value set in the paused frame took effect")
}

func TestSteppingMovesThroughTheProgram(t *testing.T) {
	t.Parallel()
	run := startSession(t, debuggedProgram, func(debugger *debug.Debugger) {
		debugger.SetBreakpoint("main.go", 14)
	})
	event := debugtest.WaitPause(t, run.debugger)
	require.Equal(t, 14, event.Location.Line)
	run.debugger.ClearBreakpoint("main.go", 14)

	require.NoError(t, run.debugger.StepIn(event.ThreadID))
	event = debugtest.WaitPause(t, run.debugger)
	require.Equal(t, debug.StopReasonStep, event.Reason)
	require.Equal(t, "main.add", event.Location.Function)

	require.NoError(t, run.debugger.StepOver(event.ThreadID))
	event = debugtest.WaitPause(t, run.debugger)
	require.Equal(t, "main.add", event.Location.Function)

	require.NoError(t, run.debugger.StepOut(event.ThreadID))
	event = debugtest.WaitPause(t, run.debugger)
	require.Equal(t, "main.run", event.Location.Function)

	require.NoError(t, run.debugger.Continue())
	value, err := run.execution.Wait(t)
	require.NoError(t, err)
	require.EqualValues(t, 11, value)
	require.ErrorIs(t, run.debugger.Continue(), fault.ErrDebugNotPaused)
}

func TestAFunctionBreakpointAndAHitConditionPauseWhereTheySay(t *testing.T) {
	t.Parallel()
	var results []debug.BreakpointResult
	run := startSession(t, debuggedProgram, func(debugger *debug.Debugger) {
		results = debugger.SetFunctionBreakpoints([]debug.FunctionBreakpoint{
			{Name: "main.add", Condition: "", HitCondition: "2"},
			{Name: "main.add", Condition: "", HitCondition: "not a number"},
		})
	})
	require.Len(t, results, 2)
	require.True(t, results[0].Verified)
	require.False(t, results[1].Verified, "a hit condition that does not parse is refused")

	event := debugtest.WaitPause(t, run.debugger)
	require.Equal(t, debug.StopReasonFunctionBreakpoint, event.Reason)
	locals, err := run.debugger.Variables(event.ThreadID, 0, debug.ScopeLocals)
	require.NoError(t, err)
	require.Equal(t, "2", variable(t, locals, "b"), "the second hit")
	run.debugger.SetFunctionBreakpoints(nil)
	require.NoError(t, run.debugger.Continue())
	_, err = run.execution.Wait(t)
	require.NoError(t, err)
}

func TestAPanicPausesWhenExceptionsAreCaught(t *testing.T) {
	t.Parallel()
	source := "package main\n\nfunc run() int {\n\tvar values []int\n\treturn values[3]\n}\n"
	run := startSession(t, source, func(debugger *debug.Debugger) {
		debugger.SetExceptionBreakpoints([]debug.ExceptionFilter{debug.ExceptionFilterPanic})
	})
	event := debugtest.WaitPause(t, run.debugger)
	require.Equal(t, debug.StopReasonPanic, event.Reason)
	require.Equal(t, "exception", event.Reason.String())
	require.NotNil(t, event.Panic)
	require.Contains(t, event.Panic.Text, "index out of range")
	require.NoError(t, run.debugger.Continue())
	_, err := run.execution.Wait(t)
	require.Error(t, err)
}

func TestPauseOnEntryStopsBeforeTheFirstLine(t *testing.T) {
	t.Parallel()
	run := startSession(t, debuggedProgram, func(debugger *debug.Debugger) {
		debugger.SetPauseOnEntry(true)
	})
	event := debugtest.WaitPause(t, run.debugger)
	require.Equal(t, debug.StopReasonEntry, event.Reason)
	require.Equal(t, "entry", event.Reason.String())
	run.debugger.SetPauseOnEntry(false)
	require.NoError(t, run.debugger.Continue())
	value, err := run.execution.Wait(t)
	require.NoError(t, err)
	require.EqualValues(t, 11, value)
}

func TestStopEndsAPausedRun(t *testing.T) {
	t.Parallel()
	run := startSession(t, debuggedProgram, func(debugger *debug.Debugger) {
		debugger.SetBreakpoint("main.go", 6)
	})
	debugtest.WaitPause(t, run.debugger)
	run.debugger.Stop()
	_, err := run.execution.Wait(t)
	require.ErrorIs(t, err, fault.ErrDebuggerStop)
}

func TestEventKindsAndStopReasonsPrintTheirNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "thread started", debug.EventThreadStarted.String())
	require.Equal(t, "thread exited", debug.EventThreadExited.String())
	require.Equal(t, "exited", debug.EventExited.String())
	require.Equal(t, "unknown", debug.EventKind(200).String())
	require.Equal(t, "step", debug.StopReasonStep.String())
	require.Equal(t, "function breakpoint", debug.StopReasonFunctionBreakpoint.String())
	require.Equal(t, "paused by another thread", debug.StopReasonOtherThread.String())
	require.Equal(t, "pause", debug.StopReason(200).String())
}
