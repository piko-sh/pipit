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

func TestIndexReadsFromSlicesArraysAndPointers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() reflect.Value
		check func(t *testing.T, registers *Registers)
		name  string
		index int64
	}{
		{
			name:  "a slice element",
			build: func() reflect.Value { return reflect.ValueOf([]int{10, 20, 30}) },
			index: 1,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(20), registers.General[0].Int()) },
		},
		{
			name:  "the first slice element",
			build: func() reflect.Value { return reflect.ValueOf([]int{10, 20}) },
			index: 0,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(10), registers.General[0].Int()) },
		},
		{
			name:  "the last slice element",
			build: func() reflect.Value { return reflect.ValueOf([]int{10, 20}) },
			index: 1,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(20), registers.General[0].Int()) },
		},
		{
			name:  "an array element",
			build: func() reflect.Value { return reflect.ValueOf([3]string{"a", "b", "c"}) },
			index: 2,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, "c", registers.General[0].String()) },
		},
		{
			name:  "a pointer to an array is followed",
			build: func() reflect.Value { return reflect.ValueOf(&[2]int{7, 8}) },
			index: 1,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(8), registers.General[0].Int()) },
		},
		{
			name:  "an interface element is unwrapped to its dynamic value",
			build: func() reflect.Value { return reflect.ValueOf([]any{42}) },
			index: 0,
			check: func(t *testing.T, registers *Registers) { require.Equal(t, reflect.Int, registers.General[0].Kind()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.General[1] = tt.build()
			registers.Ints[2] = tt.index

			require.Equal(t, opContinue, handleIndex(vm, nil, registers, op(0, 1, 2)))
			tt.check(t, registers)
		})
	}
}

func TestIndexRefusesAnOutOfRangePosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() reflect.Value
		name  string
		index int64
	}{
		{name: "one past the end of a slice", build: func() reflect.Value { return reflect.ValueOf([]int{1, 2}) }, index: 2},
		{name: "far past the end of a slice", build: func() reflect.Value { return reflect.ValueOf([]int{1, 2}) }, index: 1000},
		{name: "a negative index", build: func() reflect.Value { return reflect.ValueOf([]int{1, 2}) }, index: -1},
		{name: "any index into an empty slice", build: func() reflect.Value { return reflect.ValueOf([]int{}) }, index: 0},
		{name: "any index into a nil slice", build: func() reflect.Value { return reflect.ValueOf([]int(nil)) }, index: 0},
		{name: "one past the end of an array", build: func() reflect.Value { return reflect.ValueOf([2]int{1, 2}) }, index: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.General[1] = tt.build()
			registers.Ints[2] = tt.index

			got := handleIndex(vm, nil, registers, op(0, 1, 2))

			require.NotEqual(t, opContinue, got, "an out-of-range index must not read past the collection")
			require.Error(t, vm.evalError)
		})
	}
}

func TestIndexSetWritesThroughToTheBackingArray(t *testing.T) {
	t.Parallel()

	t.Run("a slice element is replaced in place", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := []int{1, 2, 3}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 1
		registers.General[2] = reflect.ValueOf(42)

		require.Equal(t, opContinue, handleIndexSet(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []int{1, 42, 3}, target, "the write must reach the caller's backing array")
	})

	t.Run("an interface element accepts any dynamic value", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := []any{nil, nil}
		registers.General[0] = reflect.ValueOf(target)
		registers.Ints[1] = 0
		registers.General[2] = reflect.ValueOf("boxed")

		require.Equal(t, opContinue, handleIndexSet(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, "boxed", target[0])
	})

	t.Run("an out-of-range write is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf([]int{1})
		registers.Ints[1] = 5
		registers.General[2] = reflect.ValueOf(42)

		require.NotEqual(t, opContinue, handleIndexSet(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}

func TestMapIndexReturnsTheElementZeroValueOnAMiss(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() reflect.Value
		key   reflect.Value
		check func(t *testing.T, registers *Registers)
		name  string
	}{
		{
			name:  "a present string key",
			build: func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			key:   reflect.ValueOf("k"),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.General[0].Int()) },
		},
		{
			name:  "an absent string key",
			build: func() reflect.Value { return reflect.ValueOf(map[string]int{"k": 42}) },
			key:   reflect.ValueOf("absent"),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(0), registers.General[0].Int()) },
		},
		{
			name:  "an absent key in a map of slices yields a nil slice",
			build: func() reflect.Value { return reflect.ValueOf(map[string][]int{}) },
			key:   reflect.ValueOf("absent"),
			check: func(t *testing.T, registers *Registers) {
				require.Equal(t, reflect.Slice, registers.General[0].Kind())
				require.True(t, registers.General[0].IsNil())
			},
		},
		{
			name:  "a present integer key",
			build: func() reflect.Value { return reflect.ValueOf(map[int]string{7: "v"}) },
			key:   reflect.ValueOf(7),
			check: func(t *testing.T, registers *Registers) { require.Equal(t, "v", registers.General[0].String()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = tt.build()
			registers.General[2] = tt.key

			require.Equal(t, opContinue, handleMapIndex(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers)
		})
	}

	t.Run("a non-map operand is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf("not a map")
		registers.General[2] = reflect.ValueOf("k")

		require.Equal(t, opPanicError, handleMapIndex(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}

func TestMapDeleteRemovesOnlyTheNamedKey(t *testing.T) {
	t.Parallel()

	t.Run("a string key is removed", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{"a": 1, "b": 2}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf("a")

		require.Equal(t, opContinue, handleMapDelete(vm, frame, registers, op(0, 1, 0)))
		require.Len(t, target, 1)
		require.Equal(t, 2, target["b"], "the untouched entry must survive")
	})

	t.Run("an integer key is removed through the fast path", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{1: 10, 2: 20}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf(1)

		require.Equal(t, opContinue, handleMapDelete(vm, frame, registers, op(0, 1, 0)))
		require.Len(t, target, 1)
		require.Equal(t, 20, target[2])
	})

	t.Run("deleting an absent key leaves the map alone", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{"a": 1}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf("absent")

		require.Equal(t, opContinue, handleMapDelete(vm, frame, registers, op(0, 1, 0)))
		require.Len(t, target, 1)
	})

	t.Run("a non-map operand is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(42)
		registers.General[1] = reflect.ValueOf("k")

		require.Equal(t, opPanicError, handleMapDelete(vm, frame, registers, op(0, 1, 0)))
		require.Error(t, vm.evalError)
	})
}

func TestMapSetStoresThroughTheDeclaredTypes(t *testing.T) {
	t.Parallel()

	t.Run("a string-keyed map accepts a new entry", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf("k")
		registers.General[2] = reflect.ValueOf(42)

		require.Equal(t, opContinue, handleMapSet(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 42, target["k"])
	})

	t.Run("a map of interfaces accepts any dynamic value", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]any{}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf("k")
		registers.General[2] = reflect.ValueOf([]int{1})

		require.Equal(t, opContinue, handleMapSet(vm, frame, registers, op(0, 1, 2)))
		require.Len(t, target, 1)
	})

	t.Run("an existing entry is replaced rather than duplicated", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[string]int{"k": 1}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf("k")
		registers.General[2] = reflect.ValueOf(42)

		require.Equal(t, opContinue, handleMapSet(vm, frame, registers, op(0, 1, 2)))
		require.Len(t, target, 1)
		require.Equal(t, 42, target["k"])
	})

	t.Run("a non-map operand is refused rather than panicking through reflect", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(42)
		registers.General[1] = reflect.ValueOf("k")
		registers.General[2] = reflect.ValueOf(1)

		got := handleMapSet(vm, frame, registers, op(0, 1, 2))

		require.Equal(t, opPanicError, got,
			"every map handler reports a non-map operand through the dispatch loop, not a raw panic")
		require.Error(t, vm.evalError)
		require.Contains(t, vm.evalError.Error(), "non-map")
	})

	t.Run("an integer-keyed map stores through the fast path", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		target := map[int]int{}
		registers.General[0] = reflect.ValueOf(target)
		registers.General[1] = reflect.ValueOf(7)
		registers.General[2] = reflect.ValueOf(42)

		require.Equal(t, opContinue, handleMapSet(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 42, target[7])
	})
}

func TestMapIndexOkWritesThePresenceBitAlongsideTheValue(t *testing.T) {
	t.Parallel()

	const okRegister uint8 = 6

	newOkFrame := func(t *testing.T) (*VM, *CallFrame, *Registers) {
		t.Helper()
		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(8)
		builder.Emit(isa.OpMapIndexOk, 0, 1, 2)
		builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, okRegister, 0, 0))
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
		vm, frame, registers := newFramedVM(t, builder.build())
		frame.ProgramCounter = 1
		return vm, frame, registers
	}

	t.Run("a present key sets the bit", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newOkFrame(t)
		registers.General[1] = reflect.ValueOf(map[string]int{"k": 42})
		registers.General[2] = reflect.ValueOf("k")

		require.Equal(t, opContinue, handleMapIndexOk(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(42), registers.General[0].Int())
		require.Equal(t, int64(1), registers.Ints[okRegister])
	})

	t.Run("an absent key clears the bit and yields the zero value", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newOkFrame(t)
		registers.General[1] = reflect.ValueOf(map[string]int{"k": 42})
		registers.General[2] = reflect.ValueOf("absent")
		registers.Ints[okRegister] = 1

		require.Equal(t, opContinue, handleMapIndexOk(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(0), registers.General[0].Int())
		require.Equal(t, int64(0), registers.Ints[okRegister])
	})
}
