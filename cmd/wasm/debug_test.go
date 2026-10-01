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
	if !ok {
		t.Fatalf("expected a list, got %T", value)
	}
	return list
}

func asRow(t *testing.T, value any) map[string]any {
	t.Helper()
	row, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected an object, got %T", value)
	}
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
	t.Fatalf("local %q not in %+v", name, locals)
	return nil
}

func expectPaused(t *testing.T, snapshot map[string]any, line int) {
	t.Helper()
	if snapshot[resultKeyOK] != true || snapshot["state"] != statePaused || snapshot["line"] != line {
		t.Fatalf("expected pause at line %d, got %+v", line, snapshot)
	}
}

func endSession(t *testing.T) {
	t.Helper()
	if session := activeSession(); session != nil {
		session.stop()
	}
	if browserBusy.Load() {
		t.Fatal("session did not release the busy flag")
	}
}

func TestDebugBreakpointsAndStepping(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	if snapshot["reason"] != "breakpoint" || snapshot["hitBreakpoint"] != true {
		t.Fatalf("unexpected reason: %+v", snapshot)
	}
	if snapshot["stdout"] != "start\n" {
		t.Fatalf("output before the pause: %q", snapshot["stdout"])
	}
	if got := local(t, snapshot, "i")["value"]; got != "1" {
		t.Fatalf("i = %v, want 1", got)
	}
	if !browserBusy.Load() {
		t.Fatal("paused session does not hold the busy flag")
	}
	session := activeSession()

	snapshot = session.resume("continue", 0)
	expectPaused(t, snapshot, 17)
	if got := local(t, snapshot, "total")["value"]; got != "1" {
		t.Fatalf("total after one iteration = %v, want 1", got)
	}
	if snapshot["stdout"] != "" {
		t.Fatalf("output repeated across pauses: %q", snapshot["stdout"])
	}

	snapshot = session.resume("stepIn", 0)
	expectPaused(t, snapshot, 8)
	stack := asList(t, snapshot["stack"])
	if len(stack) < 2 || !strings.HasSuffix(fmt.Sprint(asRow(t, stack[0])["function"]), "add") {
		t.Fatalf("stack after step in: %+v", stack)
	}

	snapshot = session.resume("stepOver", 0)
	expectPaused(t, snapshot, 9)
	if got := local(t, snapshot, "sum")["value"]; got != "3" {
		t.Fatalf("sum = %v, want 3", got)
	}

	snapshot = session.resume("stepOut", 0)
	if snapshot["state"] != statePaused || !strings.HasSuffix(fmt.Sprint(snapshot["function"]), "main") {
		t.Fatalf("step out did not return to main: %+v", snapshot)
	}

	if result := session.setBreakpoints(nil); result[resultKeyOK] != true {
		t.Fatalf("clearing breakpoints failed: %+v", result)
	}
	snapshot = session.resume("continue", 0)
	if snapshot["state"] != stateExited || snapshot[resultKeyOK] != true || snapshot["stdout"] != "total 6 1\n" {
		t.Fatalf("unexpected exit: %+v", snapshot)
	}
	if browserBusy.Load() || activeSession() != nil {
		t.Fatal("exited session was not released")
	}
}

func TestDebugInspection(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	session := activeSession()

	row := local(t, snapshot, "p")
	ref, isInt := row["ref"].(int)
	if !isInt || ref == 0 || row["children"] != 2 {
		t.Fatalf("struct local is not expandable: %+v", row)
	}
	children := session.variables(variablesRequest{ref: ref})
	fields := asList(t, children["variables"])
	if children[resultKeyOK] != true || len(fields) != 2 || asRow(t, fields[1])["value"] != "2" || children["total"] != 2 {
		t.Fatalf("struct expansion: %+v", children)
	}

	evaluated := session.evaluate(0, 0, "total + i*10")
	if evaluated[resultKeyOK] != true || evaluated["value"] != "10" {
		t.Fatalf("evaluate: %+v", evaluated)
	}
	if denied := session.evaluate(0, 0, "add(1, 2)"); denied[resultKeyOK] != false {
		t.Fatalf("evaluation called a function: %+v", denied)
	}
	if bad := session.variables(variablesRequest{scope: "registers"}); bad[resultKeyOK] != false {
		t.Fatalf("unknown scope accepted: %+v", bad)
	}

	session.resume("continue", 0)
	if stale := session.variables(variablesRequest{ref: ref}); stale[resultKeyOK] != false {
		t.Fatalf("reference survived a resume: %+v", stale)
	}
}

func TestDebugBreakpointVerification(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{4}, true)
	if snapshot["state"] != statePaused || snapshot["reason"] != "entry" {
		t.Fatalf("stop on entry: %+v", snapshot)
	}
	breakpoints := asList(t, snapshot["breakpoints"])
	if len(breakpoints) != 1 {
		t.Fatalf("breakpoints: %+v", breakpoints)
	}
	if first := asRow(t, breakpoints[0]); first["verified"] == true && first["line"] == 4 {
		t.Fatalf("blank line accepted as a breakpoint: %+v", first)
	}
}

func TestDebugStopWhilePaused(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	final := activeSession().stop()
	if final["state"] != stateExited || final[resultKeyOK] != true {
		t.Fatalf("stop: %+v", final)
	}
	if browserBusy.Load() || activeSession() != nil {
		t.Fatal("stopped session was not released")
	}
	if again := startDebugSession(debugProgram, nil, false); again["state"] != stateExited || again["stdout"] != "start\ntotal 6 1\n" {
		t.Fatalf("new session after stop: %+v", again)
	}
}

func TestDebugRejectsOtherOperations(t *testing.T) {
	defer endSession(t)

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)
	if second := startDebugSession(debugProgram, nil, false); second[resultKeyOK] != false || second[resultKeyError] != "browser interpreter is busy" {
		t.Fatalf("second session started: %+v", second)
	}
	if !browserBusy.CompareAndSwap(true, true) {
		t.Fatal("run and format would be admitted during a session")
	}
	if bad := activeSession().resume("jump", 0); bad[resultKeyOK] != false {
		t.Fatalf("unknown action accepted: %+v", bad)
	}
}

func TestDebugCompileErrorAndPanic(t *testing.T) {
	defer endSession(t)

	broken := startDebugSession("package main\nfunc main() { undefined() }", nil, false)
	if broken["state"] != stateExited || broken[resultKeyOK] != false || broken[resultKeyError] == "" || browserBusy.Load() {
		t.Fatalf("compile error: %+v", broken)
	}

	panicking := startDebugSession("package main\n\nfunc main() {\n\tpanic(\"boom\")\n}\n", nil, false)
	if panicking["state"] != statePaused || panicking["reason"] != "exception" || !strings.Contains(fmt.Sprint(panicking["panic"]), "boom") {
		t.Fatalf("panic pause: %+v", panicking)
	}
	final := activeSession().resume("continue", 0)
	if final["state"] != stateExited || final[resultKeyOK] != false || !strings.Contains(fmt.Sprint(final[resultKeyError]), "boom") {
		t.Fatalf("panic exit: %+v", final)
	}
}

func TestDebugPauseWhileBlocked(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nimport \"time\"\n\nfunc main() {\n\tfor {\n\t\ttime.Sleep(10 * time.Millisecond)\n\t}\n}\n"
	started := make(chan map[string]any, 1)
	go func() { started <- startDebugSession(source, nil, false) }()

	var acknowledged map[string]any
	for range 100 {
		time.Sleep(20 * time.Millisecond)
		if session := activeSession(); session != nil {
			if acknowledged = session.pause(); acknowledged[resultKeyOK] == true {
				break
			}
		}
	}
	if acknowledged[resultKeyOK] != true {
		t.Fatalf("pause was never accepted: %+v", acknowledged)
	}
	snapshot := <-started
	if snapshot["state"] != statePaused || snapshot["reason"] != "pause" {
		t.Fatalf("pause: %+v", snapshot)
	}
}

func TestDebugContinueAdvancesPastHoistedConstants(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nimport \"fmt\"\n\nfunc fib(n int) int {\n\tif n < 2 {\n\t\treturn n\n\t}\n\treturn fib(n-1) + fib(n-2)\n}\n\nfunc main() {\n\tfor i := 0; i < 12; i++ {\n\t\tfmt.Printf(\"fib(%2d) = %d\\n\", i, fib(i))\n\t}\n}\n"
	snapshot := startDebugSession(source, []int{14}, false)
	session := activeSession()
	for want := range 3 {
		expectPaused(t, snapshot, 14)
		if got := local(t, snapshot, "i")["value"]; got != fmt.Sprint(want) {
			t.Fatalf("pause %d: i = %v", want, got)
		}
		snapshot = session.resume("continue", 0)
	}
}

func TestElementTypeName(t *testing.T) {
	for typeName, want := range map[string]string{
		"[]int": "int", "*[]string": "string", "[4]float64": "float64", "map[string]int": "int",
		"map[[2]int][]string": "[]string", "pair": "", "": "", "func()": "",
	} {
		if got := elementTypeName(typeName); got != want {
			t.Fatalf("elementTypeName(%q) = %q, want %q", typeName, got, want)
		}
	}
}

func TestDebugVariablesPages(t *testing.T) {
	defer endSession(t)

	source := "package main\n\nfunc main() {\n\tbig := make([]int, 1200)\n\tfor i := range big {\n\t\tbig[i] = i\n\t}\n\tprintln(len(big))\n}\n"
	snapshot := startDebugSession(source, []int{8}, false)
	expectPaused(t, snapshot, 8)
	session := activeSession()
	ref, isInt := local(t, snapshot, "big")["ref"].(int)
	if !isInt || ref == 0 {
		t.Fatalf("slice local is not expandable: %+v", local(t, snapshot, "big"))
	}

	pages := []struct {
		start     int
		wantRows  int
		wantFirst string
	}{
		{start: 0, wantRows: 500, wantFirst: "0"},
		{start: 500, wantRows: 500, wantFirst: "500"},
		{start: 1000, wantRows: 200, wantFirst: "1000"},
		{start: 1200, wantRows: 0, wantFirst: ""},
	}
	for _, page := range pages {
		result := session.variables(variablesRequest{ref: ref, start: page.start})
		rows := asList(t, result["variables"])
		if result[resultKeyOK] != true || result["total"] != 1200 || len(rows) != page.wantRows {
			t.Fatalf("page from %d: %d rows, total %v, want %d rows of 1200", page.start, len(rows), result["total"], page.wantRows)
		}
		if page.wantRows > 0 && asRow(t, rows[0])["value"] != page.wantFirst {
			t.Fatalf("page from %d starts with %+v, want %s", page.start, rows[0], page.wantFirst)
		}
	}

	scope := session.variables(variablesRequest{scope: scopeLocals})
	if scope[resultKeyOK] != true || scope["total"] != len(asList(t, scope["variables"])) {
		t.Fatalf("scope listing: %+v", scope)
	}
}

func TestDebugRendersUnusualValues(t *testing.T) {
	defer endSession(t)

	source := "package main\n\ntype point struct{ x, y int }\n\nfunc main() {\n\tp := point{x: 3, y: 4}\n\tvar loop any\n\tloop = &loop\n\t_, _ = p, loop\n}\n"
	snapshot := startDebugSession(source, []int{9}, false)
	expectPaused(t, snapshot, 9)
	session := activeSession()

	ref, isInt := local(t, snapshot, "p")["ref"].(int)
	if !isInt || ref == 0 {
		t.Fatalf("struct local is not expandable: %+v", local(t, snapshot, "p"))
	}
	fields := session.variables(variablesRequest{ref: ref})
	rows := asList(t, fields["variables"])
	if fields[resultKeyOK] != true || len(rows) != 2 || asRow(t, rows[0])["value"] != "3" || asRow(t, rows[1])["value"] != "4" {
		t.Fatalf("lowercase fields: %+v", fields)
	}
	if value, _ := local(t, snapshot, "loop")["value"].(string); value == "" {
		t.Fatalf("self-referential local did not render: %+v", local(t, snapshot, "loop"))
	}
}

func TestDebugCeilingReleasesAPausedSession(t *testing.T) {
	defer endSession(t)
	previous := debugSessionCeiling
	debugSessionCeiling = 300 * time.Millisecond
	defer func() { debugSessionCeiling = previous }()

	snapshot := startDebugSession(debugProgram, []int{17}, false)
	expectPaused(t, snapshot, 17)

	deadline := time.Now().Add(5 * time.Second)
	for activeSession() != nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if activeSession() != nil || browserBusy.Load() {
		t.Fatal("a session left paused past the ceiling still holds the interpreter")
	}
	debugSessionCeiling = previous
	if again := startDebugSession(debugProgram, nil, false); again["state"] != stateExited {
		t.Fatalf("the interpreter was not free for a new session: %+v", again)
	}
}
