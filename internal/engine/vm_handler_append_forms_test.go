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

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

type namedInts []int

func TestScalarAppendHandlersGrowTheirSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  any
		load    func(*Registers)
		want    any
	}{
		{
			name: "an int element onto an int slice", handler: handleAppendInt, source: []int64{1},
			load: func(r *Registers) { r.Ints[2] = 2 }, want: []int64{1, 2},
		},
		{
			name: "an int element onto a named int slice", handler: handleAppendInt, source: namedInts{1},
			load: func(r *Registers) { r.Ints[2] = 2 }, want: namedInts{1, 2},
		},
		{
			name: "a float element", handler: handleAppendFloat, source: []float64{1.5},
			load: func(r *Registers) { r.Floats[2] = 2.5 }, want: []float64{1.5, 2.5},
		},
		{
			name: "a string element", handler: handleAppendString, source: []string{"a"},
			load: func(r *Registers) { r.Strings[2] = "b" }, want: []string{"a", "b"},
		},
		{
			name: "a bool element", handler: handleAppendBool, source: []bool{true},
			load: func(r *Registers) { r.Bools[2] = false }, want: []bool{true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.source)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestScalarAppendHandlersSeedAnUnsetDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		load    func(*Registers)
		want    any
	}{
		{name: "an int element", handler: handleAppendInt, load: func(r *Registers) { r.Ints[2] = 2 }, want: []int{2}},
		{name: "a float element", handler: handleAppendFloat, load: func(r *Registers) { r.Floats[2] = 2.5 }, want: []float64{2.5}},
		{name: "a string element", handler: handleAppendString, load: func(r *Registers) { r.Strings[2] = "b" }, want: []string{"b"}},
		{name: "a bool element", handler: handleAppendBool, load: func(r *Registers) { r.Bools[2] = true }, want: []bool{true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestScalarAppendInPlaceFormReusesTheDestination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  any
		load    func(*Registers)
		want    any
	}{
		{
			name: "an int element", handler: handleAppendInt, source: []int64{1},
			load: func(r *Registers) { r.Ints[2] = 2 }, want: []int64{1, 2},
		},
		{
			name: "a float element", handler: handleAppendFloat, source: []float64{1.5},
			load: func(r *Registers) { r.Floats[2] = 2.5 }, want: []float64{1.5, 2.5},
		},
		{
			name: "a string element", handler: handleAppendString, source: []string{"a"},
			load: func(r *Registers) { r.Strings[2] = "b" }, want: []string{"a", "b"},
		},
		{
			name: "a bool element", handler: handleAppendBool, source: []bool{true},
			load: func(r *Registers) { r.Bools[2] = false }, want: []bool{true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[0] = reflect.ValueOf(tt.source)
			tt.load(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 0, 2)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestScalarAppendRefusesToPassTheAllocationCap(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newStandardVM(t)
	vm.Limits.MaxAllocSize = 1
	registers.General[1] = reflect.ValueOf([]int64{1, 2})
	registers.Ints[2] = 3

	got := handleAppendInt(vm, frame, registers, op(0, 1, 2))
	requireEvalError(t, vm, got, fault.ErrAllocationLimit)
}

func TestByteAppendFastFormsBuildTheBuffer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler opcodeHandler
		source  reflect.Value
		want    []byte
	}{
		{
			name: "onto an existing byte slice", handler: handleAppendByteFast,
			source: reflect.ValueOf([]byte{1}), want: []byte{1, 2},
		},
		{
			name: "onto an unset register", handler: handleAppendByteFast,
			source: reflect.Value{}, want: []byte{2},
		},
		{
			name: "the in-place form onto an existing slice", handler: handleAppendByteFastInPlace,
			source: reflect.ValueOf([]byte{1}), want: []byte{1, 2},
		},
		{
			name: "the in-place form onto an unset register", handler: handleAppendByteFastInPlace,
			source: reflect.Value{}, want: []byte{2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[1] = tt.source
			registers.Uints[2] = 2

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestByteAppendFastFormsHandleOtherElementTypes(t *testing.T) {
	t.Parallel()

	t.Run("a named byte slice takes the reflect path", func(t *testing.T) {
		t.Parallel()

		type namedBytes []byte

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(namedBytes{1})
		registers.Uints[2] = 2

		require.Equal(t, opContinue, handleAppendByteFast(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, namedBytes{1, 2}, registers.General[0].Interface())
	})

	t.Run("a register that is not a slice falls back", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf([]byte{1})
		registers.Uints[2] = 2

		require.Equal(t, opContinue, handleAppendByteFastInPlace(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte{1, 2}, registers.General[0].Interface())
	})

	t.Run("a pointer to a byte slice appends through the pointer", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []byte{1}
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Uints[2] = 2

		require.Equal(t, opContinue, handleAppendByteFastInPlace(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte{1, 2}, backing, "the caller's slice must see the appended byte")
	})

	t.Run("a nil pointer raises the dereference Go would", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*[]byte)(nil))
		registers.Uints[2] = 2

		got := handleAppendByteFastInPlace(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("the cap is honoured", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 1
		registers.General[1] = reflect.ValueOf([]byte{1, 2})
		registers.Uints[2] = 3

		got := handleAppendByteFast(vm, frame, registers, op(0, 1, 2))
		requireEvalError(t, vm, got, fault.ErrAllocationLimit)
	})
}

func TestGeneralAppendInPlaceFallsBackToTheAllocatingForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source reflect.Value
		want   any
	}{
		{name: "an unset register", source: reflect.Value{}, want: []string{"b"}},
		{name: "a heap-backed slice", source: reflect.ValueOf([]string{"a"}), want: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))
			registers.General[1] = tt.source
			registers.General[2] = reflect.ValueOf("b")

			require.Equal(t, opContinue, handleAppendInPlace(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestGeneralAppendInPlaceRaisesOnANilPointer(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))
	registers.General[1] = reflect.ValueOf((*[]string)(nil))
	registers.General[2] = reflect.ValueOf("b")

	got := handleAppendInPlace(vm, frame, registers, op(0, 1, 2))
	requireRuntimePanic(t, vm, got, "nil pointer dereference")
}

func TestGeneralAppendInPlaceWritesThroughAPointer(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))
	backing := []string{"a"}
	registers.General[1] = reflect.ValueOf(&backing)
	registers.General[2] = reflect.ValueOf("b")

	require.Equal(t, opContinue, handleAppendInPlace(vm, frame, registers, op(0, 1, 2)))
	require.Equal(t, []string{"a", "b"}, backing, "the caller's slice must see the appended element")
}

func TestUintAppendSubOpCoversEveryUnsignedWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source reflect.Value
		want   any
	}{
		{name: "a byte slice", source: reflect.ValueOf([]byte{1}), want: []byte{1, 2}},
		{name: "a uint64 slice", source: reflect.ValueOf([]uint64{1}), want: []uint64{1, 2}},
		{name: "a uint slice", source: reflect.ValueOf([]uint{1}), want: []uint{1, 2}},
		{name: "a uint32 slice", source: reflect.ValueOf([]uint32{1}), want: []uint32{1, 2}},
		{name: "an unset register seeds a byte slice", source: reflect.Value{}, want: []byte{2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
			registers.General[2] = tt.source
			registers.Uints[3] = 2

			require.Equal(t, opContinue, handleSubOpAppendUint(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, 2, frame.ProgramCounter, "the element word must be stepped over")
			require.Equal(t, tt.want, registers.General[1].Interface())
		})
	}
}

func TestUintAppendSubOpRefusesToPassTheAllocationCap(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
	vm.Limits.MaxAllocSize = 1
	registers.General[2] = reflect.ValueOf([]byte{1, 2})
	registers.Uints[3] = 3

	got := handleSubOpAppendUint(vm, frame, registers, op(0, 1, 2))
	requireEvalError(t, vm, got, fault.ErrAllocationLimit)
}

func TestAppendStringSpreadWidensIntoAByteSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		destination reflect.Value
		want        any
	}{
		{name: "onto an existing byte slice", destination: reflect.ValueOf([]byte("ab")), want: []byte("abcd")},
		{name: "onto an unset destination", destination: reflect.Value{}, want: []byte("cd")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, registers := newStandardVM(t)

			got := handleAppendStringSpread(vm, registers, op(0, 1, 2), tt.destination, reflect.ValueOf("cd"))
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestAppendStringSpreadRefusesAnIncompatibleDestination(t *testing.T) {
	t.Parallel()

	t.Run("a named byte slice is converted element by element", func(t *testing.T) {
		t.Parallel()

		type namedBytes []byte

		vm, _, registers := newStandardVM(t)

		got := handleAppendStringSpread(vm, registers, op(0, 1, 2), reflect.ValueOf(namedBytes{}), reflect.ValueOf("cd"))
		require.Equal(t, opContinue, got)
		require.Equal(t, namedBytes("cd"), registers.General[0].Interface())
	})

	t.Run("a slice of another element type is refused", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)

		got := handleAppendStringSpread(vm, registers, op(0, 1, 2), reflect.ValueOf([]int{}), reflect.ValueOf("cd"))
		requireEvalError(t, vm, got, nil)
		require.Contains(t, vm.evalError.Error(), "cannot spread string")
	})

	t.Run("the cap is honoured", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 1

		got := handleAppendStringSpread(vm, registers, op(0, 1, 2), reflect.ValueOf([]byte("ab")), reflect.ValueOf("cd"))
		requireEvalError(t, vm, got, fault.ErrAllocationLimit)
	})
}

func TestAppendSpreadInPlaceFallsBackForHeapSlices(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 0, uint8(isa.RegisterGeneral)))
	registers.General[1] = reflect.ValueOf([]int{1})
	registers.General[2] = reflect.ValueOf([]int{2, 3})

	require.Equal(t, opContinue, handleAppendSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
	require.Equal(t, []int{1, 2, 3}, registers.General[0].Interface())
}

func TestByteSpreadInPlaceAppendsWholeSlices(t *testing.T) {
	t.Parallel()

	t.Run("onto a slice register", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf([]byte("ab"))
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		require.Equal(t, opContinue, handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte("abcd"), registers.General[1].Interface())
	})

	t.Run("through a pointer register", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		require.Equal(t, opContinue, handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte("abcd"), backing, "the caller's slice must see the appended bytes")
	})

	t.Run("an unset destination falls back", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		require.Equal(t, opContinue, handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
	})

	t.Run("a source that is not a byte slice falls back", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf([]byte("ab"))
		registers.General[2] = reflect.ValueOf([]int{1})

		require.Equal(t, opContinue, handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
	})

	t.Run("a nil pointer destination falls back", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*[]byte)(nil))
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		require.Equal(t, opContinue, handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2)))
	})

	t.Run("the cap is honoured", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 1
		registers.General[1] = reflect.ValueOf([]byte("ab"))
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		got := handleSubOpAppendByteSpreadInPlace(vm, frame, registers, op(0, 1, 2))
		requireEvalError(t, vm, got, fault.ErrAllocationLimit)
	})
}

func TestStarByteAppendWritesThroughThePointer(t *testing.T) {
	t.Parallel()

	t.Run("a single byte", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Uints[2] = 'c'

		require.Equal(t, opContinue, handleSubOpStarAppendByteFast(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte("abc"), backing)
	})

	t.Run("a whole slice", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)
		registers.General[2] = reflect.ValueOf([]byte("cd"))

		require.Equal(t, opContinue, handleSubOpStarAppendByteSpread(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []byte("abcd"), backing)
	})

	t.Run("a source that is not a byte slice falls back", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)
		registers.General[2] = reflect.ValueOf([]int{1})

		require.Equal(t, opContinue, handleSubOpStarAppendByteSpread(vm, frame, registers, op(0, 1, 2)))
	})

	t.Run("the cap is honoured", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 1
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Uints[2] = 'c'

		got := handleSubOpStarAppendByteFast(vm, frame, registers, op(0, 1, 2))
		requireEvalError(t, vm, got, fault.ErrAllocationLimit)
	})
}

func TestStarByteAppendFallbackNamesTheBrokenOperand(t *testing.T) {
	t.Parallel()

	t.Run("an unset register raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleSubOpStarAppendByteFastFallback(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a register that is not a pointer is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf([]byte("ab"))

		require.Panics(t, func() { handleSubOpStarAppendByteFastFallback(vm, frame, registers, op(0, 1, 2)) },
			"the compiler only emits the star form for a *[]byte register")
	})

	t.Run("a nil pointer is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*[]byte)(nil))

		require.Panics(t, func() { handleSubOpStarAppendByteFastFallback(vm, frame, registers, op(0, 1, 2)) })
	})

	t.Run("a live pointer passes the defensive checks", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []byte("ab")
		registers.General[1] = reflect.ValueOf(&backing)

		require.Equal(t, opContinue, handleSubOpStarAppendByteFastFallback(vm, frame, registers, op(0, 1, 2)))
	})
}
