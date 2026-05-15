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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type pointerStringer struct{ text string }

func (s *pointerStringer) String() string { return s.text }

type plainStruct struct{ value int }

func newTypeAssertFrame(t *testing.T, typeTable []reflect.Type, methods [][]string, ext isa.Instruction) (*VM, *CallFrame, *Registers) {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.typeTable = typeTable
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, ext)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	compiledFunction := builder.build()
	compiledFunction.TypeTableInterfaceMethods = methods

	vm, frame, registers := newFramedVM(t, compiledFunction)
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestTypeAssertHandlerWritesTheValueAndFlag(t *testing.T) {
	t.Parallel()

	t.Run("a matching assertion publishes the value and sets the flag", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[int]()}, nil, op(0, 0, typeAssertModePanic))
		registers.General[1] = reflect.ValueOf(7)

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, 7, registers.General[3].Interface())
		require.Equal(t, int64(1), registers.Ints[4])
		require.Equal(t, 2, frame.ProgramCounter, "the type-index word must be stepped over")
	})

	t.Run("an interface-wrapped source is unwrapped before matching", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[string]()}, nil, op(0, 0, TypeAssertModeTypeSwitch))
		boxed := map[string]any{"k": "wrapped"}
		registers.General[1] = reflect.ValueOf(boxed).MapIndex(reflect.ValueOf("k"))

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(1), registers.Ints[4], "the dynamic type is what a Go assertion inspects")
		require.Equal(t, "wrapped", registers.General[3].Interface())
	})

	t.Run("a failed panic-mode assertion names both types", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[string]()}, nil, op(0, 0, typeAssertModePanic))
		registers.General[1] = reflect.ValueOf(7)

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		requireRuntimePanic(t, vm, got, "interface conversion: interface {} is int, not string")
	})

	t.Run("a failed comma-ok assertion writes the target zero value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[string]()}, nil, op(0, 0, 0))
		registers.General[1] = reflect.ValueOf(7)

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[4])
		require.Equal(t, "", registers.General[3].Interface(), "a failed comma-ok assertion yields the zero value")
	})

	t.Run("a failed type-switch arm leaves the destination untouched", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[string]()}, nil, op(0, 0, TypeAssertModeTypeSwitch))
		registers.General[1] = reflect.ValueOf(7)
		registers.General[3] = reflect.ValueOf("untouched")

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[4])
		require.Equal(t, "untouched", registers.General[3].Interface(), "the next arm reads the same source register")
	})

	t.Run("a type index past the table is a bounds fault", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newTypeAssertFrame(t, []reflect.Type{reflect.TypeFor[int]()}, nil, op(9, 0, 0))
		registers.General[1] = reflect.ValueOf(7)

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opPanicError, got)
		require.Error(t, vm.evalError)
	})

	t.Run("a collapsed interface still enforces its recorded method set", func(t *testing.T) {
		t.Parallel()

		table := []reflect.Type{reflect.TypeFor[any]()}
		methods := [][]string{{"String/0/1"}}

		vm, frame, registers := newTypeAssertFrame(t, table, methods, op(0, 0, TypeAssertModeTypeSwitch))
		registers.General[1] = reflect.ValueOf(plainStruct{value: 1})

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(0), registers.Ints[4], "a plain struct must not satisfy a Stringer arm")
	})

	t.Run("a type implementing the recorded method set matches", func(t *testing.T) {
		t.Parallel()

		table := []reflect.Type{reflect.TypeFor[any]()}
		methods := [][]string{{"String/0/1"}}

		vm, frame, registers := newTypeAssertFrame(t, table, methods, op(0, 0, TypeAssertModeTypeSwitch))
		registers.General[1] = reflect.ValueOf(assertStringer{})

		got := handleTypeAssert(vm, frame, registers, op(3, 1, 4))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(1), registers.Ints[4])
	})
}

func TestTypeAssertMissingMatchHonoursTheMode(t *testing.T) {
	t.Parallel()

	t.Run("a nil source names itself in the panic message", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)

		got := handleTypeAssertMissingMatch(vm, registers, op(3, 1, 4), op(0, 0, typeAssertModePanic), reflect.Value{}, reflect.TypeFor[int]())
		requireRuntimePanic(t, vm, got, "interface {} is nil, not int")
	})

	t.Run("a comma-ok failure against a nil target clears the destination", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		registers.General[3] = reflect.ValueOf("stale")

		got := handleTypeAssertMissingMatch(vm, registers, op(3, 1, 4), op(0, 0, 0), reflect.ValueOf(1), nil)
		require.Equal(t, opContinue, got)
		require.False(t, registers.General[3].IsValid(), "there is no zero value to write for a nil target")
	})
}

func TestInterfaceSatisfactionWalksBothMethodSets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   reflect.Value
		required []string
		want     bool
	}{
		{name: "an empty requirement list is satisfied by anything", source: reflect.ValueOf(plainStruct{}), required: nil, want: true},
		{name: "a value receiver method is found", source: reflect.ValueOf(assertStringer{}), required: []string{"String"}, want: true},
		{
			name:   "a pointer receiver method is found by widening to the pointer type",
			source: reflect.ValueOf(pointerStringer{}), required: []string{"String"}, want: true,
		},
		{name: "a missing method is refused", source: reflect.ValueOf(plainStruct{}), required: []string{"String"}, want: false},
		{
			name: "an arity that disagrees is refused", source: reflect.ValueOf(assertStringer{}),
			required: []string{"String/1/1"}, want: false,
		},
		{
			name: "an arity that agrees is accepted", source: reflect.ValueOf(assertStringer{}),
			required: []string{"String/0/1"}, want: true,
		},
		{name: "an invalid source satisfies nothing", source: reflect.Value{}, required: []string{"String"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, tt.want, sourceImplementsAllMethods(vm, tt.source, tt.required))
		})
	}
}

func TestNativeMethodArityMatchesTreatsNegativeCountsAsUnchecked(t *testing.T) {
	t.Parallel()

	method, ok := reflect.TypeFor[assertStringer]().MethodByName("String")
	require.True(t, ok)

	tests := []struct {
		name    string
		params  int
		results int
		want    bool
	}{
		{name: "the recorded shape agrees", params: 0, results: 1, want: true},
		{name: "too many parameters are refused", params: 1, results: 1, want: false},
		{name: "too many results are refused", params: 0, results: 2, want: false},
		{name: "an unrecorded parameter count is unchecked", params: -1, results: 9, want: true},
		{name: "an unrecorded result count is unchecked", params: 9, results: -1, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, nativeMethodArityMatches(method, tt.params, tt.results))
		})
	}
}
