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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type trampolineCounters struct {
	Signed   int64
	Unsigned uint64
	Narrow   int32
	Small    uint8
	Text     string
}

func newTrampolineContext(t *testing.T) (*VM, *Registers, *dispatchContext) {
	t.Helper()

	vm, _, registers := newStandardVM(t)
	context := &dispatchContext{}
	vm.buildDispatchContext(context, nil)
	return vm, registers, context
}

func TestMathTrampolinesWriteTheFloatBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline func(*dispatchContext, int64, int64) *dispatchContext
		operand    float64
		want       float64
	}{
		{name: "sine", trampoline: asmCallMathSin, operand: 0, want: 0},
		{name: "cosine", trampoline: asmCallMathCos, operand: 0, want: 1},
		{name: "exponential", trampoline: asmCallMathExp, operand: 0, want: 1},
		{name: "tangent", trampoline: asmCallMathTan, operand: 0, want: 0},
		{name: "sine of a quarter turn", trampoline: asmCallMathSin, operand: math.Pi / 2, want: 1},
		{name: "cosine of a half turn", trampoline: asmCallMathCos, operand: math.Pi, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, registers, context := newTrampolineContext(t)
			registers.Floats[1] = tt.operand

			require.Same(t, context, tt.trampoline(context, 0, 1), "the context is handed back so the caller can reload it")
			require.InDelta(t, tt.want, registers.Floats[0], 1e-9)
		})
	}
}

func TestMathModTrampolineTakesTwoOperands(t *testing.T) {
	t.Parallel()

	_, registers, context := newTrampolineContext(t)
	registers.Floats[1] = 7.5
	registers.Floats[2] = 2

	asmCallMathMod(context, 0, 1, 2)
	require.InDelta(t, 1.5, registers.Floats[0], 1e-9)
}

func TestStrconvTrampolinesWriteTheStringBank(t *testing.T) {
	t.Parallel()

	t.Run("itoa renders base ten", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.Ints[1] = -42

		asmCallStrconvItoa(context, 0, 1)
		require.Equal(t, "-42", registers.Strings[0])
	})

	t.Run("format int honours the base operand", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			value int64
			base  int64
			want  string
		}{
			{name: "base ten", value: 42, base: 10, want: "42"},
			{name: "base two", value: 5, base: 2, want: "101"},
			{name: "base sixteen", value: 255, base: 16, want: "ff"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				_, registers, context := newTrampolineContext(t)
				registers.Ints[1] = tt.value
				registers.Ints[2] = tt.base

				asmCallStrconvFormatInt(context, 0, 1, 2)
				require.Equal(t, tt.want, registers.Strings[0])
			})
		}
	})
}

func TestCollectionTrampolinesReadTheGeneralBank(t *testing.T) {
	t.Parallel()

	t.Run("capacity is read from the slice header", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.General[1] = reflect.ValueOf(make([]int, 2, 7))

		asmCallCap(context, 0, 1)
		require.Equal(t, int64(7), registers.Ints[0])
	})

	t.Run("a byte slice converts to a string", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.General[1] = reflect.ValueOf([]byte("text"))

		asmCallBytesToString(context, 0, 1)
		require.Equal(t, "text", registers.Strings[0])
	})

	t.Run("an int slice boxes into the general bank", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.SlicesInt[1] = []int64{1, 2, 3}

		asmCallBoxSliceInt(context, 0, 1)
		require.Equal(t, 3, registers.General[0].Len())
	})
}

func TestMakeSliceTrampolinesHonourLengthAndCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline func(*dispatchContext, int64, int64, int64) *dispatchContext
		measure    func(*Registers) (int, int)
	}{
		{
			name: "an int slice", trampoline: asmCallMakeSliceInt,
			measure: func(r *Registers) (int, int) { return len(r.SlicesInt[0]), cap(r.SlicesInt[0]) },
		},
		{
			name: "a float slice", trampoline: asmCallMakeSliceFloat,
			measure: func(r *Registers) (int, int) { return len(r.slicesFloat[0]), cap(r.slicesFloat[0]) },
		},
		{
			name: "a string slice", trampoline: asmCallMakeSliceString,
			measure: func(r *Registers) (int, int) { return len(r.slicesString[0]), cap(r.slicesString[0]) },
		},
		{
			name: "a bool slice", trampoline: asmCallMakeSliceBool,
			measure: func(r *Registers) (int, int) { return len(r.slicesBool[0]), cap(r.slicesBool[0]) },
		},
		{
			name: "a uint slice", trampoline: asmCallMakeSliceUint,
			measure: func(r *Registers) (int, int) { return len(r.slicesUint[0]), cap(r.slicesUint[0]) },
		},
		{
			name: "a byte slice", trampoline: asmCallMakeSliceByte,
			measure: func(r *Registers) (int, int) { return len(r.slicesByte[0]), cap(r.slicesByte[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, registers, context := newTrampolineContext(t)
			registers.Ints[1] = 2
			registers.Ints[2] = 5

			tt.trampoline(context, 0, 1, 2)
			length, capacity := tt.measure(registers)
			require.Equal(t, 2, length)
			require.Equal(t, 5, capacity)
		})
	}
}

func TestAppendTrampolinesGrowTheTypedBanks(t *testing.T) {
	t.Parallel()

	t.Run("an int element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.SlicesInt[1] = []int64{1}
		registers.Ints[2] = 9

		asmCallAppendSliceIntDirect(context, 0, 1, 2)
		require.Equal(t, []int64{1, 9}, registers.SlicesInt[0])
	})

	t.Run("a float element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.slicesFloat[1] = []float64{1.5}
		registers.Floats[2] = 2.5

		asmCallAppendSliceFloatDirect(context, 0, 1, 2)
		require.Equal(t, []float64{1.5, 2.5}, registers.slicesFloat[0])
	})

	t.Run("a string element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.slicesString[1] = []string{"a"}
		registers.Strings[2] = "b"

		asmCallAppendSliceStringDirect(context, 0, 1, 2)
		require.Equal(t, []string{"a", "b"}, registers.slicesString[0])
	})

	t.Run("a bool element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.slicesBool[1] = []bool{true}
		registers.Bools[2] = false

		asmCallAppendSliceBoolDirect(context, 0, 1, 2)
		require.Equal(t, []bool{true, false}, registers.slicesBool[0])
	})

	t.Run("a uint element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.slicesUint[1] = []uint64{1}
		registers.Uints[2] = 2

		asmCallAppendSliceUintDirect(context, 0, 1, 2)
		require.Equal(t, []uint64{1, 2}, registers.slicesUint[0])
	})

	t.Run("a byte element is appended", func(t *testing.T) {
		t.Parallel()

		_, registers, context := newTrampolineContext(t)
		registers.slicesByte[1] = []byte{1}
		registers.Uints[2] = 2

		asmCallAppendSliceByteDirect(context, 0, 1, 2)
		require.Equal(t, []byte{1, 2}, registers.slicesByte[0])
	})
}

func TestStructFieldStepTrampolinesWalkEveryWidth(t *testing.T) {
	t.Parallel()

	counterType := reflect.TypeFor[trampolineCounters]()

	tests := []struct {
		name      string
		fieldName string
		kind      reflect.Kind
		increment func(*dispatchContext, int64, int64) *dispatchContext
		decrement func(*dispatchContext, int64, int64) *dispatchContext
		read      func(*trampolineCounters) int64
	}{
		{
			name: "a signed 64-bit field", fieldName: "Signed", kind: reflect.Int64,
			increment: asmCallIncStructFieldInt, decrement: asmCallDecStructFieldInt,
			read: func(c *trampolineCounters) int64 { return c.Signed },
		},
		{
			name: "a signed 32-bit field", fieldName: "Narrow", kind: reflect.Int32,
			increment: asmCallIncStructFieldInt, decrement: asmCallDecStructFieldInt,
			read: func(c *trampolineCounters) int64 { return int64(c.Narrow) },
		},
		{
			name: "an unsigned 64-bit field", fieldName: "Unsigned", kind: reflect.Uint64,
			increment: asmCallIncStructFieldUint, decrement: asmCallDecStructFieldUint,
			read: func(c *trampolineCounters) int64 { return int64(c.Unsigned) },
		},
		{
			name: "an unsigned 8-bit field", fieldName: "Small", kind: reflect.Uint8,
			increment: asmCallIncStructFieldUint, decrement: asmCallDecStructFieldUint,
			read: func(c *trampolineCounters) int64 { return int64(c.Small) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			field, ok := counterType.FieldByName(tt.fieldName)
			require.True(t, ok)

			vm, registers, context := newTrampolineContext(t)
			vm.CallStack[vm.FramePointer].Function.StructLayoutTable = []program.StructFieldLayout{
				{Offset: uint32(field.Offset), Kind: uint8(tt.kind)},
			}
			counters := &trampolineCounters{}
			registers.General[1] = reflect.ValueOf(counters)

			tt.increment(context, 1, 0)
			tt.increment(context, 1, 0)
			require.Equal(t, int64(2), tt.read(counters), "each increment must land in the field itself")

			tt.decrement(context, 1, 0)
			require.Equal(t, int64(1), tt.read(counters))
		})
	}
}

func TestStructFieldStepTrampolinesFallBackOnAMismatch(t *testing.T) {
	t.Parallel()

	counterType := reflect.TypeFor[trampolineCounters]()
	field, ok := counterType.FieldByName("Text")
	require.True(t, ok)

	t.Run("a field kind the fast path does not handle falls through", func(t *testing.T) {
		t.Parallel()

		vm, registers, context := newTrampolineContext(t)
		vm.CallStack[vm.FramePointer].Function.StructLayoutTable = []program.StructFieldLayout{
			{Offset: uint32(field.Offset), Kind: uint8(reflect.String), PathLength: 1},
		}
		counters := &trampolineCounters{Text: "unchanged"}
		registers.General[1] = reflect.ValueOf(counters)

		require.NotPanics(t, func() { asmCallIncStructFieldInt(context, 1, 0) },
			"a shape the fast path declines routes through the reflect walk, not a crash")
		require.Equal(t, "unchanged", counters.Text)
	})

	t.Run("a value receiver still reaches the field through the reflect walk", func(t *testing.T) {
		t.Parallel()

		unsigned, found := counterType.FieldByName("Unsigned")
		require.True(t, found)

		vm, registers, context := newTrampolineContext(t)
		vm.CallStack[vm.FramePointer].Function.StructLayoutTable = []program.StructFieldLayout{
			{Offset: uint32(unsigned.Offset), Kind: uint8(reflect.Uint64), PathLength: 1, Path: [isa.StructFieldLayoutMaxPathDepth]uint8{1}},
		}
		receiver := reflect.New(counterType).Elem()
		registers.General[1] = receiver

		require.NotPanics(t, func() { asmCallIncStructFieldUint(context, 1, 0) },
			"a receiver the fast path declines routes through the reflect walk, not a crash")
		require.Equal(t, uint64(1), receiver.Interface().(trampolineCounters).Unsigned)
	})
}

func TestTrampolinePointerBaseAcceptsOnlyPointers(t *testing.T) {
	t.Parallel()

	counters := &trampolineCounters{}

	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{name: "a pointer receiver", value: reflect.ValueOf(counters), want: true},
		{name: "an unset register", value: reflect.Value{}, want: false},
		{name: "a value receiver", value: reflect.ValueOf(trampolineCounters{}), want: false},
		{name: "a nil pointer", value: reflect.ValueOf((*trampolineCounters)(nil)), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			registers.General[1] = tt.value

			base, ok := trampolinePointerBase(&registers, 1)
			require.Equal(t, tt.want, ok)
			if tt.want {
				require.NotNil(t, base)
			}
		})
	}
}

func TestVMRegistersForCtxFollowsTheLiveFramePointer(t *testing.T) {
	t.Parallel()

	vm, registers, context := newTrampolineContext(t)
	registers.Ints[0] = 5

	require.Equal(t, registers, vmRegistersForCtx(context),
		"the trampolines read the frame the assembly loop is on, not the Go-side mirror")
	require.Equal(t, int64(vm.FramePointer), context.framePointer)
	require.Equal(t, registers, &frameForCtx(context).Registers)
}

func TestPathBTrampolineRunsTheHandlerAndAdvancesTheCounter(t *testing.T) {
	t.Parallel()

	vm, _, registers := newStandardVM(t)
	context := &dispatchContext{}
	vm.buildDispatchContext(context, nil)
	registers.Ints[1] = 20
	registers.Ints[2] = 22

	word := isa.Instruction{Op: isa.OpAddInt, A: 0, B: 1, C: 2}
	pathBTrampoline(context, *(*uint32)(unsafe.Pointer(&word)), handleAddInt)

	require.Equal(t, int64(42), registers.Ints[0], "the handler runs against the live register bank")
	require.Zero(t, context.handlerResult, "a clean continue leaves no handler result to surface")
}
