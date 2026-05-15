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

package dap

import (
	"reflect"
	"strings"
	"testing"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
)

func TestExpandReflectValueStruct(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type point struct {
		X int
		Y int
	}
	value := reflect.ValueOf(point{X: 3, Y: 4})

	variables := srv.expandReflectValue(stop, value)
	if got := len(variables); got != 2 {
		t.Fatalf("expanded fields: got %d, want 2", got)
	}
	names := []string{variables[0].Name, variables[1].Name}
	if !reflect.DeepEqual(names, []string{"X", "Y"}) {
		t.Fatalf("field names: got %v, want [X Y]", names)
	}
	if variables[0].Value != "3" {
		t.Fatalf("X value: got %q, want \"3\"", variables[0].Value)
	}
}

func TestExpandReflectValueMapSortsByKey(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	value := reflect.ValueOf(map[string]int{"b": 2, "a": 1, "c": 3})
	variables := srv.expandReflectValue(stop, value)

	if got := len(variables); got != 3 {
		t.Fatalf("map entries: got %d, want 3", got)
	}
	names := []string{variables[0].Name, variables[1].Name, variables[2].Name}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatalf("map key order: got %v, want [a b c] (sorted)", names)
	}
}

func TestExpandReflectValueSlice(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	value := reflect.ValueOf([]string{"alpha", "beta"})
	variables := srv.expandReflectValue(stop, value)

	if got := len(variables); got != 2 {
		t.Fatalf("slice elements: got %d, want 2", got)
	}
	if variables[0].Name != "[0]" || variables[1].Name != "[1]" {
		t.Fatalf("element names: got %q,%q, want [0],[1]", variables[0].Name, variables[1].Name)
	}
	if !strings.Contains(variables[0].Value, "alpha") {
		t.Fatalf("element 0 value: got %q, want to contain \"alpha\"", variables[0].Value)
	}
}

func TestMakeVariableAllocatesReferenceForComposites(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	scalar := srv.makeVariable(stop, "n", "int", reflect.ValueOf(42))
	if scalar.VariablesReference != 0 {
		t.Fatalf("scalar got reference %d; want 0 (no children)", scalar.VariablesReference)
	}

	composite := srv.makeVariable(stop, "pt", "Point", reflect.ValueOf(struct{ X int }{X: 1}))
	if composite.VariablesReference == 0 {
		t.Fatalf("composite got reference 0; want non-zero so the IDE can expand it")
	}
}

func TestMakeVariableNoReferenceForEmptyComposites(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	empty := srv.makeVariable(stop, "m", "map[string]int", reflect.ValueOf(map[string]int{}))
	if empty.VariablesReference != 0 {
		t.Fatalf("empty map got reference %d; want 0 (no children to expand)", empty.VariablesReference)
	}

	nilSlice := srv.makeVariable(stop, "s", "[]int", reflect.ValueOf([]int(nil)))
	if nilSlice.VariablesReference != 0 {
		t.Fatalf("nil slice got reference %d; want 0", nilSlice.VariablesReference)
	}
}

func TestExpandReflectValueWalksThroughPointer(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type box struct{ N int }
	value := reflect.ValueOf(&box{N: 7})

	variables := srv.expandReflectValue(stop, value)
	if got := len(variables); got != 1 {
		t.Fatalf("pointer-to-struct fields: got %d, want 1", got)
	}
	if variables[0].Name != "N" || variables[0].Value != "7" {
		t.Fatalf("field: got name=%q value=%q, want name=N value=7", variables[0].Name, variables[0].Value)
	}
}

func TestExpandReflectValueNilPointerYieldsNothing(t *testing.T) {
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type box struct{ N int }
	var nilBox *box

	if got := srv.expandReflectValue(stop, reflect.ValueOf(nilBox)); got != nil {
		t.Fatalf("nil pointer expansion: got %v, want nil", got)
	}
}

func TestFormatLeafQuotesStrings(t *testing.T) {
	if got := formatLeaf(reflect.ValueOf("hi")); got != `"hi"` {
		t.Fatalf("formatLeaf(\"hi\"): got %q, want \"hi\" with quotes", got)
	}
}

func TestStoppedReasonMapsKnownEvents(t *testing.T) {
	cases := []struct {
		reason pipit.StopReason
		want   string
	}{
		{debug.StopReasonBreakpoint, "breakpoint"},
		{debug.StopReasonStep, "step"},
		{debug.StopReasonEntry, "entry"},
		{debug.StopReasonPause, "pause"},
		{debug.StopReasonPanic, "exception"},
		{debug.StopReasonFunctionBreakpoint, "function breakpoint"},
		{debug.StopReasonOtherThread, "pause"},
		{pipit.StopReason(255), "pause"},
	}
	for _, c := range cases {
		if got := stoppedReason(c.reason); got != c.want {
			t.Fatalf("stoppedReason(%v): got %q, want %q", c.reason, got, c.want)
		}
	}
}
