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

func TestSliceOpAppliesOnlyTheSuppliedBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		collection any
		flags      uint8
		low        int64
		high       int64
		want       any
		wantCap    int
	}{
		{name: "no bounds copies the whole slice", collection: []int{1, 2, 3}, want: []int{1, 2, 3}, wantCap: 3},
		{
			name: "a low bound alone runs to the end", collection: []int{1, 2, 3}, flags: isa.SliceLowBoundFlag,
			low: 1, want: []int{2, 3}, wantCap: 2,
		},
		{
			name: "a high bound alone starts at zero", collection: []int{1, 2, 3}, flags: isa.SliceHighBoundFlag,
			high: 2, want: []int{1, 2}, wantCap: 3,
		},
		{
			name: "both bounds cut from each end", collection: []int{1, 2, 3, 4},
			flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 1, high: 3, want: []int{2, 3}, wantCap: 3,
		},
		{
			name: "an empty window is allowed", collection: []int{1, 2, 3},
			flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 2, high: 2, want: []int{}, wantCap: 1,
		},
		{
			name: "a string slice keeps its element type", collection: []string{"a", "b"},
			flags: isa.SliceLowBoundFlag, low: 1, want: []string{"b"}, wantCap: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(tt.flags, 4, 5))
			registers.General[2] = reflect.ValueOf(tt.collection)
			registers.Ints[4] = tt.low
			registers.Ints[5] = tt.high

			got := handleSliceOp(vm, frame, registers, op(0, 1, 2))
			require.Equal(t, opContinue, got)
			require.Equal(t, 2, frame.ProgramCounter, "the bounds word must be stepped over")
			require.Equal(t, tt.want, registers.General[1].Interface())
			require.Equal(t, tt.wantCap, registers.General[1].Cap())
		})
	}
}

func TestSliceOpSlicesAnArrayByItsLength(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(isa.SliceHighBoundFlag, 4, 5))

	array := reflect.New(reflect.TypeFor[[3]int]()).Elem()
	array.Set(reflect.ValueOf([3]int{1, 2, 3}))
	registers.General[2] = array
	registers.Ints[5] = 2

	got := handleSliceOp(vm, frame, registers, op(0, 1, 2))
	require.Equal(t, opContinue, got)
	require.Equal(t, []int{1, 2}, registers.General[1].Interface())
	require.Equal(t, 3, registers.General[1].Cap(), "an array slice takes its capacity from the array length")
}

func TestSliceOpHonoursTheThirdIndex(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t,
		op(isa.SliceLowBoundFlag|isa.SliceHighBoundFlag|isa.SliceMaxBitFlag, 4, 5),
		op(6, 0, 0),
	)
	registers.General[2] = reflect.ValueOf([]int{1, 2, 3, 4, 5})
	registers.Ints[4] = 1
	registers.Ints[5] = 3
	registers.Ints[6] = 4

	got := handleSliceOp(vm, frame, registers, op(0, 1, 2))
	require.Equal(t, opContinue, got)
	require.Equal(t, 3, frame.ProgramCounter, "the three-index form carries a second bounds word")
	require.Equal(t, []int{2, 3}, registers.General[1].Interface())
	require.Equal(t, 3, registers.General[1].Cap(), "the capacity bound is measured from the low bound")
}

func TestSliceOpRefusesBoundsOutsideTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flags   uint8
		low     int64
		high    int64
		maximum int64
		wantMsg string
	}{
		{
			name: "a negative low bound", flags: isa.SliceLowBoundFlag, low: -1,
			wantMsg: "slice bounds out of range [-1:]",
		},
		{
			name: "a crossed pair", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 3, high: 1,
			wantMsg: "slice bounds out of range [3:1]",
		},
		{
			name: "a high bound past the capacity", flags: isa.SliceHighBoundFlag, high: 9,
			wantMsg: "slice bounds out of range [:9] with capacity 4",
		},
		{
			name:  "a capacity bound below the high bound",
			flags: isa.SliceHighBoundFlag | isa.SliceMaxBitFlag, high: 3, maximum: 2,
			wantMsg: "slice bounds out of range [:3:2]",
		},
		{
			name:  "a capacity bound past the storage",
			flags: isa.SliceHighBoundFlag | isa.SliceMaxBitFlag, high: 1, maximum: 9,
			wantMsg: "slice bounds out of range [:1:9] with capacity 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(tt.flags, 4, 5), op(6, 0, 0))
			registers.General[2] = reflect.ValueOf([]int{1, 2, 3, 4})
			registers.Ints[4] = tt.low
			registers.Ints[5] = tt.high
			registers.Ints[6] = tt.maximum

			got := handleSliceOp(vm, frame, registers, op(0, 1, 2))
			requireRuntimePanic(t, vm, got, tt.wantMsg)
		})
	}
}

func TestSetZeroClearsTheTargetInPlace(t *testing.T) {
	t.Parallel()

	t.Run("a struct is reset to its zero value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		holder := &recordHolder{Count: 3, Label: "text"}
		registers.General[0] = reflect.ValueOf(holder).Elem()

		got := handleSetZero(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got)
		require.Equal(t, recordHolder{}, *holder)
	})

	t.Run("an unset register is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		require.Panics(t, func() { handleSetZero(vm, frame, registers, op(0, 0, 0)) })
	})
}
