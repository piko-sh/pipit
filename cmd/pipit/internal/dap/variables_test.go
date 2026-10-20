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
	"testing"

	"github.com/google/go-dap"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugview"
	"pipit.sh/pipit/internal/isa"
)

func TestExpandReflectValueStruct(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type point struct {
		X int
		Y int
	}
	value := reflect.ValueOf(point{X: 3, Y: 4})

	variables := srv.expandReflectValue(stop, value, 0, 0)
	require.Len(t, variables, 2)
	names := []string{variables[0].Name, variables[1].Name}
	require.Equal(t, []string{"X", "Y"}, names)
	require.Equal(t, "3", variables[0].Value)
}

func TestExpandReflectValueMapSortsByKey(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	value := reflect.ValueOf(map[string]int{"b": 2, "a": 1, "c": 3})
	variables := srv.expandReflectValue(stop, value, 0, 0)

	require.Len(t, variables, 3)
	names := []string{variables[0].Name, variables[1].Name, variables[2].Name}
	require.Equal(t, []string{"a", "b", "c"}, names)
}

func TestExpandReflectValueSlice(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	value := reflect.ValueOf([]string{"alpha", "beta"})
	variables := srv.expandReflectValue(stop, value, 0, 0)

	require.Len(t, variables, 2)
	require.Equal(t, "[0]", variables[0].Name)
	require.Equal(t, "[1]", variables[1].Name)
	require.Contains(t, variables[0].Value, "alpha")
}

func TestMakeVariableAllocatesReferenceForComposites(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	scalar := srv.makeVariable(stop, "n", "int", reflect.ValueOf(42))
	require.Equal(t, 0, scalar.VariablesReference, "a scalar has no variables reference")

	composite := srv.makeVariable(stop, "pt", "Point", reflect.ValueOf(struct{ X int }{X: 1}))
	require.NotEqual(t, 0, composite.VariablesReference, "a composite has a variables reference, so the IDE can expand it")
}

func TestMakeVariableNoReferenceForEmptyComposites(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	empty := srv.makeVariable(stop, "m", "map[string]int", reflect.ValueOf(map[string]int{}))
	require.Equal(t, 0, empty.VariablesReference, "an empty map has no variables reference")

	nilSlice := srv.makeVariable(stop, "s", "[]int", reflect.ValueOf([]int(nil)))
	require.Equal(t, 0, nilSlice.VariablesReference, "a nil slice has no variables reference")
}

func TestExpandReflectValueWalksThroughPointer(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type box struct{ N int }
	value := reflect.ValueOf(&box{N: 7})

	variables := srv.expandReflectValue(stop, value, 0, 0)
	require.Len(t, variables, 1)
	require.Equal(t, "N", variables[0].Name)
	require.Equal(t, "7", variables[0].Value)
}

func TestExpandReflectValueNilPointerYieldsNothing(t *testing.T) {
	t.Parallel()
	stop := newStopState(pipit.DebugEvent{})
	srv := &server{}

	type box struct{ N int }
	var nilBox *box

	require.Nil(t, srv.expandReflectValue(stop, reflect.ValueOf(nilBox), 0, 0))
}

func TestFormatLeafQuotesStrings(t *testing.T) {
	t.Parallel()
	require.Equal(t, `"hi"`, debugview.Leaf(reflect.ValueOf("hi")), "debugview.Leaf(\"hi\")")
}

func TestStoppedReasonMapsKnownEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reason pipit.StopReason
		want   string
	}{
		{name: "breakpoint", reason: debug.StopReasonBreakpoint, want: "breakpoint"},
		{name: "step", reason: debug.StopReasonStep, want: "step"},
		{name: "entry", reason: debug.StopReasonEntry, want: "entry"},
		{name: "pause", reason: debug.StopReasonPause, want: "pause"},
		{name: "panic", reason: debug.StopReasonPanic, want: "exception"},
		{name: "function breakpoint", reason: debug.StopReasonFunctionBreakpoint, want: "function breakpoint"},
		{name: "another thread", reason: debug.StopReasonOtherThread, want: "pause"},
		{name: "unknown reason", reason: pipit.StopReason(255), want: "pause"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, stoppedReason(tt.reason))
		})
	}
}

func TestExpandReflectValueRendersAWindow(t *testing.T) {
	t.Parallel()
	type pair struct {
		left  int
		right bool
	}
	elements := reflect.ValueOf([]int{10, 11, 12, 13, 14})

	tests := []struct {
		name       string
		value      reflect.Value
		start      int
		count      int
		wantNames  []string
		wantValues []string
	}{
		{name: "lowercase fields render their values", value: reflect.ValueOf(pair{left: 3, right: true}), wantNames: []string{"left", "right"}, wantValues: []string{"3", "true"}},
		{name: "a window of a slice", value: elements, start: 1, count: 2, wantNames: []string{"[1]", "[2]"}, wantValues: []string{"11", "12"}},
		{name: "a window running past the end", value: elements, start: 4, count: 3, wantNames: []string{"[4]"}, wantValues: []string{"14"}},
		{name: "numeric map keys in numeric order", value: reflect.ValueOf(map[int]int{10: 1, 2: 2}), wantNames: []string{"2", "10"}, wantValues: []string{"2", "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := &server{}
			variables := srv.expandReflectValue(newStopState(pipit.DebugEvent{}), tt.value, tt.start, tt.count)
			names := make([]string, 0, len(variables))
			values := make([]string, 0, len(variables))
			for _, variable := range variables {
				names = append(names, variable.Name)
				values = append(values, variable.Value)
			}
			require.Equal(t, tt.wantNames, names)
			require.Equal(t, tt.wantValues, values)
		})
	}
}

func TestMakeVariableCountsOnlyUserFields(t *testing.T) {
	t.Parallel()
	synthesised := reflect.StructOf([]reflect.StructField{
		{Name: "A", Type: reflect.TypeFor[int]()},
		{Name: isa.SynthesisedIDFieldPrefix + "pair", Type: reflect.TypeFor[struct{}](), PkgPath: "main"},
	})
	variable := (&server{}).makeVariable(newStopState(pipit.DebugEvent{}), "p", "pair", reflect.New(synthesised).Elem())
	require.Equal(t, 1, variable.NamedVariables)
}

func TestDispatchAnswersAPanickingRequest(t *testing.T) {
	t.Parallel()
	srv, out, _ := newTestServer()
	srv.debugger = nil
	request := &dap.ThreadsRequest{Request: dap.Request{ProtocolMessage: dap.ProtocolMessage{Seq: 7, Type: "request"}, Command: "threads"}}

	srv.dispatch(t.Context(), request)

	messages := readOutMessages(t, out)
	require.Len(t, messages, 1)
	response, ok := messages[0].(*dap.ErrorResponse)
	require.True(t, ok, "the message is a *dap.ErrorResponse, not %T", messages[0])
	require.False(t, response.Success, "the handler has no debugger, so the request fails")
	require.Equal(t, 7, response.RequestSeq)
	require.Contains(t, response.Message, "internal error")
}
