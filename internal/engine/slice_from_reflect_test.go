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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

func TestReadTypedSliceFieldFallbackConvertsWidthIdenticalElements(t *testing.T) {
	t.Parallel()
	type holder struct {
		Ints    []int
		Strings []string
		Bytes   []byte
	}
	value := reflect.ValueOf(holder{Ints: []int{7, 8}, Strings: []string{"a"}, Bytes: []byte("xyz")})

	tests := []struct {
		name  string
		read  func(vm *VM, registers *Registers) (OpResult, any)
		want  any
		wantR OpResult
	}{
		{name: "[]int into the int bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(0), &registers.SlicesInt[0])
			return result, registers.SlicesInt[0]
		}, want: []int64{7, 8}, wantR: opContinue},
		{name: "[]string into the string bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(1), &registers.slicesString[0])
			return result, registers.slicesString[0]
		}, want: []string{"a"}, wantR: opContinue},
		{name: "[]byte into the byte bank", read: func(vm *VM, registers *Registers) (OpResult, any) {
			result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(2), &registers.slicesByte[0])
			return result, registers.slicesByte[0]
		}, want: []byte("xyz"), wantR: opContinue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vm := newFallbackTestVM(t, value)
			result, got := tc.read(vm, &vm.currentFrame().Registers)
			require.Equal(t, tc.wantR, result)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestReadTypedSliceFieldFallbackRejectsOtherWidths(t *testing.T) {
	t.Parallel()
	value := reflect.ValueOf(struct{ Halves []int32 }{Halves: []int32{1, 2}})
	vm := newFallbackTestVM(t, value)
	registers := &vm.currentFrame().Registers
	registers.SlicesInt[0] = []int64{99}
	result := readTypedSliceFieldFallback(vm, vm.currentFrame(), registers, 0, value.Field(0), &registers.SlicesInt[0])
	require.Equal(t, opPanicError, result)
	require.ErrorIs(t, vm.evalError, fault.ErrTypeMismatch)
	require.Equal(t, []int64{99}, registers.SlicesInt[0], "a mismatch must not touch the destination")

	miss := newFallbackTestVM(t, value)
	missRegisters := &miss.currentFrame().Registers
	require.Equal(t, opContinue, readTypedSliceFieldFallback(miss, miss.currentFrame(), missRegisters, 0, reflect.Value{}, &missRegisters.SlicesInt[0]))
}

func newFallbackTestVM(tb testing.TB, receiver reflect.Value) *VM {
	vm := NewVM(tb.Context(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	var counts [isa.NumRegisterKinds]uint32
	for kind := range counts {
		counts[kind] = 1
	}
	vm.CallStack = []CallFrame{{Function: program.NewNamedFunction("fallback"), Registers: NewRegisters(counts)}}
	vm.FramePointer = 0
	vm.CallStack[0].Registers.General[0] = receiver
	return vm
}

func TestTypedSliceOfAliasesASliceOfTheElementType(t *testing.T) {
	t.Parallel()

	named := testNamedCompositeType(t, "Readings", reflect.TypeFor[[]float64]())
	value := reflect.MakeSlice(named, 2, 4)

	view, ok := typedSliceOf[float64](value)
	require.True(t, ok)
	require.Len(t, view, 2)
	require.Equal(t, 4, cap(view))
	view[1] = 2.5
	require.InDelta(t, 2.5, value.Index(1).Float(), 0, "the view shares the named slice's elements")

	empty, ok := typedSliceOf[float64](reflect.MakeSlice(named, 0, 0))
	require.True(t, ok)
	require.NotNil(t, empty, "an empty slice stays non-nil")
	none, ok := typedSliceOf[float64](reflect.Zero(named))
	require.True(t, ok)
	require.Nil(t, none)

	_, ok = typedSliceOf[int64](value)
	require.False(t, ok, "another element type")
	_, ok = typedSliceOf[float64](reflect.ValueOf(1.5))
	require.False(t, ok, "not a slice")
}

type sliceReinterpretHolder struct {
	Pointers []*int
	Floats   []float64
	Pairs    []sliceReinterpretPair
	Celsius  []sliceReinterpretCelsius
	counts   []int
	readings []float64
}

type sliceReinterpretCelsius float64

type sliceReinterpretPair struct {
	First  uintptr
	Second uintptr
}

func TestSliceFromReflectFieldRefusesOtherElementKinds(t *testing.T) {
	t.Parallel()
	value := reflect.ValueOf(sliceReinterpretHolder{
		Pointers: []*int{new(7)},
		Floats:   []float64{1.5},
		Pairs:    []sliceReinterpretPair{{First: 1, Second: 2}},
	})

	_, ok := sliceFromReflectField[int64](value.FieldByName("Pointers"))
	require.False(t, ok, "pointer words must not reach the int bank")
	_, ok = sliceFromReflectField[uint64](value.FieldByName("Pointers"))
	require.False(t, ok, "pointer words must not reach the uint bank")
	_, ok = sliceFromReflectField[int64](value.FieldByName("Floats"))
	require.False(t, ok, "float bits must not reach the int bank")
	_, ok = sliceFromReflectField[string](value.FieldByName("Pairs"))
	require.False(t, ok, "two arbitrary words must not be read as a string header")
	_, ok = sliceFromReflectField[int64](reflect.Value{})
	require.False(t, ok, "an invalid value")
}

func TestSliceFromReflectFieldAliasesLayoutIdenticalElements(t *testing.T) {
	t.Parallel()
	holder := &sliceReinterpretHolder{
		Celsius:  []sliceReinterpretCelsius{20, 21},
		counts:   []int{3, 4},
		readings: []float64{0.5},
	}
	value := reflect.ValueOf(holder).Elem()

	celsius, ok := sliceFromReflectField[float64](value.FieldByName("Celsius"))
	require.True(t, ok)
	celsius[1] = 22
	require.InDelta(t, 22.0, float64(holder.Celsius[1]), 0, "the view shares the named elements")

	counts, ok := sliceFromReflectField[int64](value.FieldByName("counts"))
	require.True(t, ok, "a read-only field is still readable")
	counts[0] = 9
	require.Equal(t, []int{9, 4}, holder.counts, "the view shares the unexported field's elements")
}

func TestTypedSliceOfToleratesInvalidAndReadOnlyValues(t *testing.T) {
	t.Parallel()
	holder := &sliceReinterpretHolder{readings: []float64{0.5, 1.5}}
	readings := reflect.ValueOf(holder).Elem().FieldByName("readings")

	require.NotPanics(t, func() {
		none, ok := typedSliceOf[float64](reflect.Value{})
		require.False(t, ok)
		require.Nil(t, none)
	})
	require.NotPanics(t, func() {
		view, ok := typedSliceOf[float64](readings)
		require.True(t, ok)
		view[0] = 2.5
	})
	require.Equal(t, []float64{2.5, 1.5}, holder.readings, "the view shares the unexported field's elements")
}
