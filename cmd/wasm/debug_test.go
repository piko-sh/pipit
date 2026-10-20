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

//go:build js && wasm

package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const debugProgram = `package main

import "fmt"

type pair struct{ A, B int }

func add(a, b int) int {
	sum := a + b
	return sum
}

func main() {
	fmt.Println("start")
	total := 0
	p := pair{A: 1, B: 2}
	for i := 1; i <= 3; i++ {
		total = add(total, i)
	}
	fmt.Println("total", total, p.A)
}
`

func asList(t *testing.T, value any) []any {
	t.Helper()
	list, ok := value.([]any)
	require.True(t, ok, "the value is a list: %T", value)
	return list
}

func asRow(t *testing.T, value any) map[string]any {
	t.Helper()
	row, ok := value.(map[string]any)
	require.True(t, ok, "the value is an object: %T", value)
	return row
}

func local(t *testing.T, snapshot map[string]any, name string) map[string]any {
	t.Helper()
	locals := asList(t, snapshot["locals"])
	for _, row := range locals {
		variable := asRow(t, row)
		if variable["name"] == name {
			return variable
		}
	}
	require.Failf(t, "local not found", "local %q not in %+v", name, locals)
	return nil
}

func expandableRef(t *testing.T, snapshot map[string]any, name string) int {
	t.Helper()
	row := local(t, snapshot, name)
	ref, isInt := row["ref"].(int)
	require.True(t, isInt, "local %q has a variables reference: %+v", name, row)
	require.NotZero(t, ref, "local %q is expandable: %+v", name, row)
	return ref
}

func expectPaused(t *testing.T, snapshot map[string]any, line int) {
	t.Helper()
	require.Subset(t, snapshot, map[string]any{resultKeyOK: true, "state": statePaused, "line": line}, "the session pauses at line %d", line)
}

func endSession(t *testing.T) {
	t.Helper()
	if session := activeSession(); session != nil {
		session.stop()
	}
	require.False(t, browserBusy.Load(), "the session releases the busy flag")
}

func TestDebugBreakpointsAndStepping(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	require.Subset(t, snapshot, map[string]any{"reason": "breakpoint", "hitBreakpoint": true, "stdout": "start\n"})
	require.Equal(t, "1", local(t, snapshot, "i")["value"])
	require.True(t, browserBusy.Load(), "a paused session holds the busy flag")
	session := activeSession()

	snapshot = session.resume("continue", 0)
	expectPaused(t, snapshot, 17)
	require.Equal(t, "1", local(t, snapshot, "total")["value"], "total after one iteration")
	require.Subset(t, snapshot, map[string]any{"stdout": ""}, "output is not repeated across pauses")

	snapshot = session.resume("stepIn", 0)
	expectPaused(t, snapshot, 8)
	stack := asList(t, snapshot["stack"])
	require.GreaterOrEqual(t, len(stack), 2, "step in adds a frame: %+v", stack)
	require.True(t, strings.HasSuffix(fmt.Sprint(asRow(t, stack[0])["function"]), "add"), "step in enters add: %+v", stack)

	snapshot = session.resume("stepOver", 0)
	expectPaused(t, snapshot, 9)
	require.Equal(t, "3", local(t, snapshot, "sum")["value"])

	snapshot = session.resume("stepOut", 0)
	require.Equal(t, statePaused, snapshot["state"], "step out pauses: %+v", snapshot)
	require.True(t, strings.HasSuffix(fmt.Sprint(snapshot["function"]), "main"), "step out returns to main: %+v", snapshot)

	require.Subset(t, session.setBreakpoints(nil), map[string]any{resultKeyOK: true}, "the breakpoints clear")
	snapshot = session.resume("continue", 0)
	require.Subset(t, snapshot, map[string]any{"state": stateExited, resultKeyOK: true, "stdout": "total 6 1\n"})
	require.False(t, browserBusy.Load(), "an exited session releases the busy flag")
	require.Nil(t, activeSession(), "an exited session is released")
}

func TestDebugInspection(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	session := activeSession()

	ref := expandableRef(t, snapshot, "p")
	require.Subset(t, local(t, snapshot, "p"), map[string]any{"children": 2})
	children := session.variables(variablesRequest{ref: ref})
	require.Subset(t, children, map[string]any{resultKeyOK: true, "total": 2})
	fields := asList(t, children["variables"])
	require.Len(t, fields, 2)
	require.Equal(t, "2", asRow(t, fields[1])["value"])

	require.Subset(t, session.evaluate(0, 0, "total + i*10"), map[string]any{resultKeyOK: true, "value": "10"})
	require.Subset(t, session.evaluate(0, 0, "add(1, 2)"), map[string]any{resultKeyOK: false}, "evaluation does not call functions")
	require.Subset(t, session.variables(variablesRequest{scope: "registers"}), map[string]any{resultKeyOK: false}, "an unknown scope is refused")

	session.resume("continue", 0)
	require.Subset(t, session.variables(variablesRequest{ref: ref}), map[string]any{resultKeyOK: false}, "a reference does not survive a resume")
}

func TestDebugBreakpointVerification(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{4}, true)
	require.Subset(t, snapshot, map[string]any{"state": statePaused, "reason": "entry"}, "the session stops on entry")
	breakpoints := asList(t, snapshot["breakpoints"])
	require.Len(t, breakpoints, 1)
	first := asRow(t, breakpoints[0])
	require.False(t, first["verified"] == true && first["line"] == 4, "a blank line is not accepted as a breakpoint: %+v", first)
}

func TestDebugStopWhilePaused(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	require.Subset(t, activeSession().stop(), map[string]any{"state": stateExited, resultKeyOK: true})
	require.False(t, browserBusy.Load(), "a stopped session releases the busy flag")
	require.Nil(t, activeSession(), "a stopped session is released")
	again := startDebugSession(debugProgram, nil, false)
	require.Subset(t, again, map[string]any{"state": stateExited, "stdout": "start\ntotal 6 1\n"}, "a new session runs after a stop")
}

func TestDebugRejectsOtherOperations(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	second := startDebugSession(debugProgram, nil, false)
	require.Subset(t, second, map[string]any{resultKeyOK: false, resultKeyError: "browser interpreter is busy"}, "a second session is refused")
	require.True(t, browserBusy.CompareAndSwap(true, true), "run and format are refused during a session")
	require.Subset(t, activeSession().resume("jump", 0), map[string]any{resultKeyOK: false}, "an unknown action is refused")
}

func TestDebugCompileErrorAndPanic(t *testing.T) {
	defer endSession(t)

	broken := startDebugSession("package main\nfunc main() { undefined() }", nil, false)
	require.Subset(t, broken, map[string]any{"state": stateExited, resultKeyOK: false})
	require.NotEmpty(t, broken[resultKeyError], "a compile error is reported")
	require.False(t, browserBusy.Load(), "a compile error releases the busy flag")

	panicking := startDebugSession("package main\n\nfunc main() {\n\tpanic(\"boom\")\n}\n", nil, false)
	require.Subset(t, panicking, map[string]any{"state": statePaused, "reason": "exception"})
	require.Contains(t, fmt.Sprint(panicking["panic"]), "boom")
	final := activeSession().resume("continue", 0)
	require.Subset(t, final, map[string]any{"state": stateExited, resultKeyOK: false})
	require.Contains(t, fmt.Sprint(final[resultKeyError]), "boom")
}

func TestDebugPauseWhileBlocked(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nimport \"time\"\n\nfunc main() {\n\tfor {\n\t\ttime.Sleep(10 * time.Millisecond)\n\t}\n}\n"
	started := make(chan map[string]any, 1)
	go func() { started <- startDebugSession(source, nil, false) }()

	require.Eventually(t, func() bool {
		session := activeSession()
		return session != nil && session.pause()[resultKeyOK] == true
	}, 2*time.Second, 20*time.Millisecond, "the running session accepts a pause")
	snapshot := <-started
	require.Subset(t, snapshot, map[string]any{"state": statePaused, "reason": "pause"})
}

func TestDebugContinueAdvancesPastHoistedConstants(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nimport \"fmt\"\n\nfunc fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-1) + fib(n-2)\n}\n\nfunc main() {\n\tfor i := 0; i < 12; i++ {\n\t\tfmt.Printf(\"fib(%2d) = %d\\n\", i, fib(i))\n\t}\n}\n"
	snapshot := startDebugSession(source, []int{14}, false)
	session := activeSession()
	for pause := range 3 {
		expectPaused(t, snapshot, 14)
		require.Equal(t, fmt.Sprint(pause), local(t, snapshot, "i")["value"], "pause %d", pause)
		snapshot = session.resume("continue", 0)
	}
}

func TestElementTypeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{name: "slice", typeName: "[]int", want: "int"},
		{name: "pointer to slice", typeName: "*[]string", want: "string"},
		{name: "array", typeName: "[4]float64", want: "float64"},
		{name: "map", typeName: "map[string]int", want: "int"},
		{name: "map with an array key", typeName: "map[[2]int][]string", want: "[]string"},
		{name: "named struct", typeName: "pair", want: ""},
		{name: "empty", typeName: "", want: ""},
		{name: "func", typeName: "func()", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, elementTypeName(tt.typeName))
		})
	}
}

func TestDebugVariablesPages(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nfunc main() {\n\tbig := make([]int, 1200)\n\tfor i := range big {\n\t\tbig[i] = i\n\t}\n\tprintln(len(big))\n}\n"
	snapshot := startDebugSession(source, []int{8}, false)
	expectPaused(t, snapshot, 8)
	session := activeSession()
	ref := expandableRef(t, snapshot, "big")

	tests := []struct {
		name      string
		wantFirst string
		start     int
		wantRows  int
	}{
		{name: "first page", start: 0, wantRows: 500, wantFirst: "0"},
		{name: "second page", start: 500, wantRows: 500, wantFirst: "500"},
		{name: "last partial page", start: 1000, wantRows: 200, wantFirst: "1000"},
		{name: "past the end", start: 1200, wantRows: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := session.variables(variablesRequest{ref: ref, start: tt.start})
			require.Subset(t, result, map[string]any{resultKeyOK: true, "total": 1200})
			rows := asList(t, result["variables"])
			require.Len(t, rows, tt.wantRows)
			if tt.wantRows > 0 {
				require.Equal(t, tt.wantFirst, asRow(t, rows[0])["value"])
			}
		})
	}

	scope := session.variables(variablesRequest{scope: scopeLocals})
	require.Subset(t, scope, map[string]any{resultKeyOK: true})
	require.Equal(t, len(asList(t, scope["variables"])), scope["total"], "the scope total counts its listing")
}

func TestDebugRendersUnusualValues(t *testing.T) {
	defer endSession(t)

	source := "package main\n\ntype point struct{ x, y int }\n\nfunc main() {\n\tp := point{x: 3, y: 4}\n\tvar loop any\n\tloop = &loop\n\t_, _ = p, loop\n}\n"
	snapshot := startDebugSession(source, []int{9}, false)
	expectPaused(t, snapshot, 9)
	session := activeSession()

	fields := session.variables(variablesRequest{ref: expandableRef(t, snapshot, "p")})
	require.Subset(t, fields, map[string]any{resultKeyOK: true})
	rows := asList(t, fields["variables"])
	require.Len(t, rows, 2, "lowercase fields are listed")
	require.Equal(t, "3", asRow(t, rows[0])["value"])
	require.Equal(t, "4", asRow(t, rows[1])["value"])
	value, isString := local(t, snapshot, "loop")["value"].(string)
	require.True(t, isString, "a self-referential local renders as text")
	require.NotEmpty(t, value, "a self-referential local renders")
}

func TestDebugCeilingReleasesAPausedSession(t *testing.T) {
	defer endSession(t)
	previous := debugSessionCeiling
	debugSessionCeiling = 300 * time.Millisecond
	defer func() { debugSessionCeiling = previous }()

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)

	require.Eventually(t, func() bool { return activeSession() == nil }, 5*time.Second, 20*time.Millisecond,
		"a session left paused past the ceiling releases the interpreter")
	require.False(t, browserBusy.Load(), "a session left paused past the ceiling releases the busy flag")
	debugSessionCeiling = previous
	again := startDebugSession(debugProgram, nil, false)
	require.Subset(t, again, map[string]any{"state": stateExited}, "the interpreter is free for a new session")
}
