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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"fmt"
	"image"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type methodExprPromoted struct {
	Tag int
	image.Point
}

type methodExprFunc func() string

type methodExprCounter struct{ n int }

func (f methodExprFunc) Name() string { return f() }

func (c *methodExprCounter) Add(k int) int { c.n += k; return c.n }

func retStringSite() *program.CallSite {
	return &program.CallSite{NativeRegister: 0, MethodReceiverRegister: 1, IsMethod: true,
		Returns: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}}}
}

func callCachedMethod(t *testing.T, site *program.CallSite, registers *Registers, methods []reflect.Value, read func() any) []any {
	t.Helper()
	vm := &VM{}
	registers.General[site.NativeRegister] = methods[0]
	ok, tag, panicValue := tryNativeFastPath(vm, site, methods[0].Interface(), registers)
	require.True(t, ok)
	require.Nil(t, panicValue)
	publishNativeFastPath(site, newNativeMethodEntry(tag, methods[0]))
	results := make([]any, 0, len(methods))
	results = append(results, read())
	for _, method := range methods[1:] {
		registers.General[site.NativeRegister] = method
		_, dispatched := dispatchCachedNativeFastPath(vm, site, registers, loadNativeFastPath(site))
		require.True(t, dispatched)
		results = append(results, read())
		require.Nil(t, loadNativeFastPath(site).fn, "a method site's entry holds no receiver-bound method value")
	}
	return results
}

func TestNativeMethodShapePassesTheCurrentReceiver(t *testing.T) {
	t.Parallel()
	points := []image.Point{image.Pt(1, 2), image.Pt(3, 4)}
	stringers := []fmt.Stringer{image.Pt(5, 6)}
	stringerIndex := 0
	tests := []struct {
		name    string
		methods func() []reflect.Value
		want    []any
		shape   nativeMethodReceiver
	}{
		{name: "a value receiver held by value", shape: nativeMethodReceiverValue,
			methods: func() []reflect.Value {
				return []reflect.Value{reflect.ValueOf(image.Pt(1, 1)).MethodByName("String"), reflect.ValueOf(image.Pt(2, 2)).MethodByName("String")}
			},
			want: []any{"(1,1)", "(2,2)"}},
		{name: "a value receiver that is a slice element, reassigned between calls", shape: nativeMethodReceiverValue,
			methods: func() []reflect.Value {
				first := reflect.ValueOf(points).Index(0).MethodByName("String")
				points[0] = image.Pt(7, 8)
				return []reflect.Value{first, reflect.ValueOf(points).Index(0).MethodByName("String"), reflect.ValueOf(points).Index(1).MethodByName("String")}
			},
			want: []any{"(7,8)", "(7,8)", "(3,4)"}},
		{name: "a pointer receiver", shape: nativeMethodReceiverPointer,
			methods: func() []reflect.Value {
				return []reflect.Value{reflect.ValueOf(&image.Point{X: 1}).MethodByName("String"), reflect.ValueOf(&image.Point{Y: 9}).MethodByName("String")}
			},
			want: []any{"(1,0)", "(0,9)"}},
		{name: "a promoted method at a non-zero offset, by value then by pointer", shape: nativeMethodReceiverPointer,
			methods: func() []reflect.Value {
				return []reflect.Value{reflect.ValueOf(methodExprPromoted{Tag: 1, Point: image.Pt(4, 4)}).MethodByName("String"),
					reflect.ValueOf(&methodExprPromoted{Tag: 2, Point: image.Pt(5, 5)}).MethodByName("String")}
			},
			want: []any{"(4,4)", "(5,5)"}},
		{name: "an interface receiver whose dynamic type changes", shape: nativeMethodReceiverInterface,
			methods: func() []reflect.Value {
				first := reflect.ValueOf(stringers).Index(0).Method(stringerIndex)
				second := reflect.ValueOf([]fmt.Stringer{image.Rect(1, 2, 3, 4)}).Index(0).Method(stringerIndex)
				third := reflect.ValueOf([]fmt.Stringer{&image.Point{X: 8}}).Index(0).Method(stringerIndex)
				return []reflect.Value{first, second, third, first}
			},
			want: []any{"(5,6)", "(1,2)-(3,4)", "(8,0)", "(5,6)"}},
		{name: "a pointer-shaped value receiver held directly takes the bound path", shape: nativeMethodReceiverNone,
			methods: func() []reflect.Value {
				return []reflect.Value{reflect.ValueOf(methodExprFunc(func() string { return "a" })).MethodByName("Name"),
					reflect.ValueOf(methodExprFunc(func() string { return "b" })).MethodByName("Name")}
			},
			want: []any{"a", "b"}},
		{name: "a plain function at a method site takes the bound path", shape: nativeMethodReceiverNone,
			methods: func() []reflect.Value {
				return []reflect.Value{reflect.ValueOf(func() string { return "c" }), reflect.ValueOf(func() string { return "d" })}
			},
			want: []any{"c", "d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registers := makeRegisters()
			site := retStringSite()
			got := callCachedMethod(t, site, &registers, tt.methods(), func() any { return registers.Strings[0] })
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.shape, loadNativeFastPath(site).method.receiver)
		})
	}
}

func TestNativeMethodShapeCallsPointerMethodsWithResults(t *testing.T) {
	t.Parallel()
	var first, second strings.Builder
	registers := makeRegisters()
	site := &program.CallSite{NativeRegister: 0, MethodReceiverRegister: 1, IsMethod: true,
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}, {Register: 1, Kind: isa.RegisterGeneral}}}
	registers.Strings[0] = "xy"
	got := callCachedMethod(t, site, &registers,
		[]reflect.Value{reflect.ValueOf(&first).MethodByName("WriteString"), reflect.ValueOf(&second).MethodByName("WriteString"),
			reflect.ValueOf(&first).MethodByName("WriteString")},
		func() any { return registers.Ints[0] })
	require.Equal(t, []any{int64(2), int64(2), int64(2)}, got)
	require.Equal(t, "xyxy|xy", first.String()+"|"+second.String())
	require.Equal(t, nativeMethodReceiverPointer, loadNativeFastPath(site).method.receiver)

	counter := &methodExprCounter{}
	counterSite := &program.CallSite{NativeRegister: 0, MethodReceiverRegister: 1, IsMethod: true,
		Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}},
		Returns:   []program.VarLocation{{Register: 0, Kind: isa.RegisterInt64}}}
	registers.Ints[0] = 5
	add := reflect.ValueOf(counter).MethodByName("Add")
	counts := callCachedMethod(t, counterSite, &registers, []reflect.Value{add, add, add}, func() any {
		result := registers.Ints[0]
		registers.Ints[0] = 5
		return result
	})
	require.Equal(t, []any{int64(5), int64(10), int64(15)}, counts)
}

func TestNativeMethodShapeRejectsAMismatchedSignature(t *testing.T) {
	t.Parallel()
	method := reflect.ValueOf(image.Pt(1, 2)).MethodByName("String")
	_, ok := buildNativeMethodShape(method, fastPathTagRetInt)
	require.False(t, ok, "a shape is only built for the tag of the method's own signature")
	_, ok = buildNativeMethodShape(method, fastPathTagSprintfString)
	require.False(t, ok, "a tag without a method-expression dispatcher builds no shape")
	shape, ok := buildNativeMethodShape(method, fastPathTagRetString)
	require.True(t, ok)
	_, matched := shape.receiverWord(reflect.ValueOf(image.Rect(0, 0, 1, 1)).MethodByName("String"))
	require.False(t, matched, "a method value of another receiver type is not the shape")
}
