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
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func newConvertFrame(t *testing.T, target reflect.Type) (*VM, *CallFrame, *Registers) {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.typeTable = []reflect.Type{target}
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, 0, 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestConvertHandlerReachesEveryConversionShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		target reflect.Type
		source reflect.Value
		want   any
	}{
		{name: "a widening integer conversion", target: reflect.TypeFor[int64](), source: reflect.ValueOf(int32(3)), want: int64(3)},
		{name: "a narrowing integer conversion", target: reflect.TypeFor[int8](), source: reflect.ValueOf(300), want: int8(44)},
		{name: "an integer to string conversion", target: reflect.TypeFor[string](), source: reflect.ValueOf(int32(65)), want: "A"},
		{name: "a float to float conversion", target: reflect.TypeFor[float32](), source: reflect.ValueOf(1.5), want: float32(1.5)},
		{name: "a slice to string conversion", target: reflect.TypeFor[string](), source: reflect.ValueOf([]byte("hi")), want: "hi"},
		{
			name: "a slice to array conversion", target: reflect.TypeFor[[2]int](),
			source: reflect.ValueOf([]int{1, 2, 3}), want: [2]int{1, 2},
		},
		{
			name: "a nil slice to a zero-length array", target: reflect.TypeFor[[0]int](),
			source: reflect.ValueOf([]int(nil)), want: [0]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newConvertFrame(t, tt.target)
			registers.General[1] = tt.source

			require.Equal(t, opContinue, handleConvert(vm, frame, registers, op(0, 1, 0)))
			require.Equal(t, 2, frame.ProgramCounter, "the type-index word must be stepped over")
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestConvertHandlerAllocatesWhenTheFlagAsksFor(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newConvertFrame(t, reflect.TypeFor[int]())

	require.Equal(t, opContinue, handleConvert(vm, frame, registers, op(0, 1, 1)))
	require.Equal(t, reflect.Pointer, registers.General[0].Kind(),
		"the allocate form makes a pointer rather than converting an operand")
	require.Equal(t, 0, registers.General[0].Elem().Interface())
}

func TestConvertHandlerRefusesBrokenOperands(t *testing.T) {
	t.Parallel()

	t.Run("a type index past the table is a bounds fault", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newConvertFrame(t, reflect.TypeFor[int]())
		frame.Function.Body[1] = isa.NewInstruction(isa.OpExt, 9, 0, 0)
		registers.General[1] = reflect.ValueOf(1)

		require.Equal(t, opPanicError, handleConvert(vm, frame, registers, op(0, 1, 0)))
		require.Error(t, vm.evalError)
	})

	t.Run("an unset source register is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newConvertFrame(t, reflect.TypeFor[int]())

		require.Panics(t, func() { handleConvert(vm, frame, registers, op(0, 1, 0)) })
	})

	t.Run("a slice too short for the array raises the runtime error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newConvertFrame(t, reflect.TypeFor[[4]int]())
		registers.General[1] = reflect.ValueOf([]int{1, 2})

		got := handleConvert(vm, frame, registers, op(0, 1, 0))
		requireRuntimePanic(t, vm, got, "cannot convert slice with length 2 to array or pointer to array with length 4")
	})
}

func TestSliceToArrayConversionFollowsGoRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		source      reflect.Value
		target      reflect.Type
		wantHandled bool
		wantError   bool
		want        any
	}{
		{
			name: "an exact-length array", source: reflect.ValueOf([]int{1, 2}), target: reflect.TypeFor[[2]int](),
			wantHandled: true, want: [2]int{1, 2},
		},
		{
			name: "a longer slice keeps the leading elements", source: reflect.ValueOf([]int{1, 2, 3}),
			target: reflect.TypeFor[[2]int](), wantHandled: true, want: [2]int{1, 2},
		},
		{
			name: "a short slice is refused", source: reflect.ValueOf([]int{1}), target: reflect.TypeFor[[2]int](),
			wantHandled: true, wantError: true,
		},
		{
			name: "a nil slice to a zero-length array pointer", source: reflect.ValueOf([]int(nil)),
			target: reflect.TypeFor[*[0]int](), wantHandled: true, want: (*[0]int)(nil),
		},
		{
			name: "a target that is not an array is not this conversion", source: reflect.ValueOf([]int{1}),
			target: reflect.TypeFor[[]int](), wantHandled: false,
		},
		{
			name: "a source that is not a slice is not this conversion", source: reflect.ValueOf(3),
			target: reflect.TypeFor[[2]int](), wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			converted, handled, err := convertSliceToArray(tt.source, tt.target)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantHandled {
				require.Equal(t, tt.want, converted.Interface())
			}
		})
	}
}

func TestSaturatingFloatToIntConvertMatchesGoCasts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source float64
		target reflect.Type
		want   any
	}{
		{name: "an in-range int", source: 3.7, target: reflect.TypeFor[int](), want: 3},
		{name: "an in-range int8", source: -3.7, target: reflect.TypeFor[int8](), want: int8(-3)},
		{name: "an in-range int16", source: 300, target: reflect.TypeFor[int16](), want: int16(300)},
		{name: "an in-range int32", source: 7, target: reflect.TypeFor[int32](), want: int32(7)},
		{name: "an in-range int64", source: 7, target: reflect.TypeFor[int64](), want: int64(7)},
		{name: "an in-range uint", source: 7, target: reflect.TypeFor[uint](), want: uint(7)},
		{name: "an in-range uint8", source: 200, target: reflect.TypeFor[uint8](), want: uint8(200)},
		{name: "an in-range uint16", source: 500, target: reflect.TypeFor[uint16](), want: uint16(500)},
		{name: "an in-range uint32", source: 7, target: reflect.TypeFor[uint32](), want: uint32(7)},
		{name: "an in-range uint64", source: 7, target: reflect.TypeFor[uint64](), want: uint64(7)},
		{name: "an in-range uintptr", source: 7, target: reflect.TypeFor[uintptr](), want: uintptr(7)},
		{name: "an overflowing int32 saturates", source: 1e20, target: reflect.TypeFor[int32](), want: int32(math.MinInt32)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			arena := newTestArena(t)

			converted, ok := saturatingFloatToIntConvert(arena, reflect.ValueOf(tt.source), tt.target)
			require.True(t, ok)
			require.Equal(t, tt.want, converted.Interface())

			fallback, fallbackOK := saturatingFloatToIntConvert(nil, reflect.ValueOf(tt.source), tt.target)
			require.True(t, fallbackOK, "the arena-free path must reach the same answer")
			require.Equal(t, tt.want, fallback.Interface())
		})
	}
}

func TestSaturatingFloatToIntConvertDeclinesOtherShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source reflect.Value
		target reflect.Type
	}{
		{name: "an invalid source", source: reflect.Value{}, target: reflect.TypeFor[int]()},
		{name: "a non-float source", source: reflect.ValueOf(3), target: reflect.TypeFor[int]()},
		{name: "a non-integer target", source: reflect.ValueOf(1.5), target: reflect.TypeFor[string]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, ok := saturatingFloatToIntConvert(newTestArena(t), tt.source, tt.target)
			require.False(t, ok)
		})
	}
}

func TestUnsafePointerConvertNeededOnlyForOneSide(t *testing.T) {
	t.Parallel()

	unsafePointer := reflect.TypeFor[unsafe.Pointer]()
	intPointer := reflect.TypeFor[*int]()

	tests := []struct {
		name        string
		source      reflect.Type
		destination reflect.Type
		want        bool
	}{
		{name: "a typed pointer to unsafe.Pointer", source: intPointer, destination: unsafePointer, want: true},
		{name: "unsafe.Pointer to a typed pointer", source: unsafePointer, destination: intPointer, want: true},
		{name: "unsafe.Pointer to itself", source: unsafePointer, destination: unsafePointer, want: false},
		{name: "two typed pointers", source: intPointer, destination: reflect.TypeFor[*string](), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, unsafePointerConvertNeeded(tt.source, tt.destination))
		})
	}
}

func TestPointerWordReadsTheMachineWord(t *testing.T) {
	t.Parallel()

	value := 7

	tests := []struct {
		name   string
		source reflect.Value
		want   uintptr
	}{
		{name: "a uintptr", source: reflect.ValueOf(uintptr(16)), want: 16},
		{name: "an unsigned integer", source: reflect.ValueOf(uint32(16)), want: 16},
		{name: "a signed integer", source: reflect.ValueOf(16), want: 16},
		{name: "a pointer", source: reflect.ValueOf(&value), want: reflect.ValueOf(&value).Pointer()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, pointerWord(tt.source))
		})
	}
}

func TestConvertClosureToFuncTypeOnlyFiresForClosures(t *testing.T) {
	t.Parallel()

	t.Run("a non-func target declines", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		closure := reflect.ValueOf(&RuntimeClosure{})

		_, ok := convertClosureToFuncType(vm, closure, reflect.TypeFor[int]())
		require.False(t, ok)
	})

	t.Run("a value that is not a closure declines", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		_, ok := convertClosureToFuncType(vm, reflect.ValueOf(3), reflect.TypeFor[func()]())
		require.False(t, ok)
	})

	t.Run("a nil closure pointer declines", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		_, ok := convertClosureToFuncType(vm, reflect.ValueOf((*RuntimeClosure)(nil)), reflect.TypeFor[func()]())
		require.False(t, ok)
	})

	t.Run("a closure converted to a plain func type clears the named type", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		closure := reflect.ValueOf(&RuntimeClosure{namedType: "Op"})

		converted, ok := convertClosureToFuncType(vm, closure, reflect.TypeFor[func()]())
		require.True(t, ok)
		require.Empty(t, closureFromValue(converted).namedType)
	})
}
