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

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/fault"
)

const evaluateSource = `package main

import "host"

type point struct{ X, Y int }

func (p point) Sum() int { return p.X + p.Y }

var counter = 5

func run() int {
	x := 41
	s := "hello"
	xs := []int{1, 2, 3}
	p := point{X: 3, Y: 4}
	f := func() int {
		inner := x + 1
		return inner + len(s) + xs[1] + p.Sum() + counter
	}
	return f() + host.Twice(x)
}
`

func newEvaluateHarness(t *testing.T, line int) *debugHarness {
	t.Helper()
	exports := map[string]reflect.Value{"Twice": reflect.ValueOf(func(v int) int { return 2 * v })}
	return newDebugHarness(t, evaluateSource, exports, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", line) })
}

func TestDebuggerEvaluateExpressions(t *testing.T) {
	t.Parallel()
	h := newEvaluateHarness(t, 18)
	event := h.pause(t)
	tests := []struct {
		name       string
		expression string
		want       any
		wantType   string
	}{
		{name: "captured int arithmetic", expression: "x + 1", want: int64(42), wantType: "int"},
		{name: "captured struct field", expression: "p.X * p.Y", want: int64(12), wantType: "int"},
		{name: "captured slice index", expression: "xs[1]", want: int64(2), wantType: "int"},
		{name: "builtin len", expression: "len(s)", want: int64(5), wantType: "int"},
		{name: "method call", expression: "p.Sum()", want: int64(7), wantType: "int"},
		{name: "global read", expression: "counter * 2", want: int64(10), wantType: "int"},
		{name: "host call", expression: "host.Twice(x)", want: int64(82), wantType: "int"},
		{name: "local declared in closure", expression: "inner - x", want: int64(1), wantType: "int"},
		{name: "string concatenation", expression: `s + "!"`, want: "hello!", wantType: "string"},
		{name: "comparison", expression: "x > 40 && s == \"hello\"", want: true, wantType: "untyped bool"},
	}
	for _, tc := range tests {
		result, err := h.dbg.Evaluate(context.Background(), event.ThreadID, 0, tc.expression, debug.EvalOptions{Timeout: 0, AllowCalls: true})
		require.NoError(t, err, tc.name)
		require.EqualValues(t, tc.want, result.Value, tc.name)
		require.Equal(t, tc.wantType, result.Type, tc.name)
	}
	_, err := h.dbg.Evaluate(context.Background(), event.ThreadID, 0, "nosuch + 1", debug.EvalOptions{Timeout: 0, AllowCalls: true})
	require.Error(t, err, "an undefined identifier is a type error")
	_, err = h.dbg.Evaluate(context.Background(), event.ThreadID, 0, "host.Twice(x)", debug.EvalOptions{Timeout: 0, AllowCalls: false})
	require.ErrorIs(t, err, fault.ErrCallsNotAllowed)
	result, err := h.dbg.Evaluate(context.Background(), event.ThreadID, 0, "len(xs)", debug.EvalOptions{Timeout: 0, AllowCalls: false})
	require.NoError(t, err, "builtins are not calls")
	require.EqualValues(t, 3, result.Value)
	outer, err := h.dbg.Evaluate(context.Background(), event.ThreadID, 1, "x + len(xs)", debug.EvalOptions{Timeout: 0, AllowCalls: true})
	require.NoError(t, err, "the caller frame evaluates its own locals")
	require.EqualValues(t, 44, outer.Value)
	require.NoError(t, h.dbg.Continue())
	run := h.finish(t)
	require.NoError(t, run.err)
	require.EqualValues(t, 42+5+2+7+5+82, run.value)
}

func TestDebuggerScopesAndClosureVariables(t *testing.T) {
	t.Parallel()
	h := newEvaluateHarness(t, 18)
	event := h.pause(t)
	scopes, err := h.dbg.Scopes(event.ThreadID, 0)
	require.NoError(t, err)
	names := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		names = append(names, scope.Name)
	}
	require.Equal(t, []string{"Locals", "Closure", "Globals"}, names)

	captured, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeClosure)
	require.NoError(t, err)
	byName := variablesByName(captured)
	require.EqualValues(t, 41, byName["x"].Value)
	require.Equal(t, "int", byName["x"].Type)
	require.Equal(t, "hello", byName["s"].Value)
	require.Equal(t, []int{1, 2, 3}, byName["xs"].Value, "a captured slice reads its elements")
	require.Equal(t, "[]int", byName["xs"].Type)

	locals, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeLocals)
	require.NoError(t, err)
	require.EqualValues(t, 42, variablesByName(locals)["inner"].Value)

	globals, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeGlobals)
	require.NoError(t, err)
	require.EqualValues(t, 5, variablesByName(globals)["counter"].Value)
	require.Equal(t, "int", variablesByName(globals)["counter"].Type)

	require.NoError(t, h.dbg.Continue())
	require.NoError(t, h.finish(t).err)
}

func TestDebuggerVariablesFollowHeapPromotedLocal(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	x := 5
	p := &x
	*p = 6
	y := x + 1
	return y
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 8) })
	event := h.pause(t)
	locals, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeLocals)
	require.NoError(t, err)
	byName := variablesByName(locals)
	require.EqualValues(t, 6, byName["x"].Value, "the var table follows the local into its heap cell")
	require.EqualValues(t, 7, byName["y"].Value)
	require.NoError(t, h.dbg.Continue())
	require.NoError(t, h.finish(t).err)
}

func TestDebuggerSetVariable(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	x := 5
	s := "a"
	y := x + len(s)
	return y
}
`
	h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) { dbg.SetBreakpoint("main.go", 6) })
	event := h.pause(t)
	updated, err := h.dbg.SetVariable(event.ThreadID, 0, "x", 40)
	require.NoError(t, err)
	require.EqualValues(t, 40, updated.Value)
	_, err = h.dbg.SetVariable(event.ThreadID, 0, "s", "abc")
	require.NoError(t, err)
	_, err = h.dbg.SetVariable(event.ThreadID, 0, "x", "not a number")
	require.ErrorIs(t, err, fault.ErrVariableNotWritable)
	_, err = h.dbg.SetVariable(event.ThreadID, 0, "nosuch", 1)
	require.ErrorIs(t, err, fault.ErrVariableUnavailable)
	require.NoError(t, h.dbg.Continue())
	run := h.finish(t)
	require.NoError(t, run.err)
	require.EqualValues(t, 43, run.value, "the write is visible to the resumed program")
}

func TestDebuggerConditionalBreakpoints(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	total := 0
	for i := 0; i < 6; i++ {
		total += i
	}
	return total
}
`
	tests := []struct {
		name       string
		breakpoint debug.Breakpoint
		wantPauses int
		wantFirst  int64
		wantMsg    bool
	}{
		{name: "condition selects one iteration", breakpoint: debug.Breakpoint{Condition: "i == 3", HitCondition: "", Line: 6}, wantPauses: 1, wantFirst: 3},
		{name: "hit count equal", breakpoint: debug.Breakpoint{Condition: "", HitCondition: "3", Line: 6}, wantPauses: 1, wantFirst: 2},
		{name: "hit count greater or equal", breakpoint: debug.Breakpoint{Condition: "", HitCondition: ">= 5", Line: 6}, wantPauses: 2, wantFirst: 4},
		{name: "hit count modulo", breakpoint: debug.Breakpoint{Condition: "", HitCondition: "% 2", Line: 6}, wantPauses: 3, wantFirst: 1},
		{name: "condition and hit count combine", breakpoint: debug.Breakpoint{Condition: "i > 1", HitCondition: "2", Line: 6}, wantPauses: 1, wantFirst: 3},
		{name: "condition error pauses with a message", breakpoint: debug.Breakpoint{Condition: "nosuch > 1", HitCondition: "", Line: 6}, wantPauses: 6, wantFirst: 0, wantMsg: true},
		{name: "non-boolean condition pauses with a message", breakpoint: debug.Breakpoint{Condition: "i", HitCondition: "", Line: 6}, wantPauses: 6, wantFirst: 0, wantMsg: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newDebugHarness(t, source, nil, func(dbg *debug.Debugger) {
				results := dbg.SetBreakpoints("main.go", []debug.Breakpoint{tc.breakpoint})
				require.True(t, results[0].Verified)
			})
			for pause := range tc.wantPauses {
				event := h.pause(t)
				require.Equal(t, debug.StopReasonBreakpoint, event.Reason)
				if tc.wantMsg {
					require.NotEmpty(t, event.Message)
				} else {
					require.Empty(t, event.Message)
				}
				if pause == 0 {
					locals, err := h.dbg.Variables(event.ThreadID, 0, debug.ScopeLocals)
					require.NoError(t, err)
					require.EqualValues(t, tc.wantFirst, variablesByName(locals)["i"].Value)
				}
				require.NoError(t, h.dbg.Continue())
			}
			run := h.finish(t)
			require.NoError(t, run.err)
			require.EqualValues(t, 15, run.value)
		})
	}
}

func TestDebuggerBreakpointLineVerification(t *testing.T) {
	t.Parallel()
	source := `package main

func run() int {
	x := 1

	// a comment line

	return x
}
`
	dbg := debug.NewDebugger()
	service := NewService(WithDebugger(dbg))
	_, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(t, err)
	results := dbg.SetBreakpoints("main.go", []debug.Breakpoint{
		{Condition: "", HitCondition: "", Line: 4},
		{Condition: "", HitCondition: "", Line: 6},
		{Condition: "", HitCondition: "", Line: 40},
		{Condition: "", HitCondition: "bogus", Line: 4},
	})
	require.Len(t, results, 4)
	require.True(t, results[0].Verified)
	require.Equal(t, 4, results[0].Line)
	require.True(t, results[1].Verified, "a comment line moves to the next line with code")
	require.Equal(t, 8, results[1].Line)
	require.NotEmpty(t, results[1].Message)
	require.False(t, results[2].Verified, "a line past the end has no code")
	require.False(t, results[3].Verified, "a bad hit condition is reported")
	require.Contains(t, results[3].Message, "hit condition")
}

func variablesByName(variables []debug.VariableInfo) map[string]debug.VariableInfo {
	byName := make(map[string]debug.VariableInfo, len(variables))
	for _, variable := range variables {
		byName[variable.Name] = variable
	}
	return byName
}
