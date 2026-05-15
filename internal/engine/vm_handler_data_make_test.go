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

func newTypeTableFrame(t *testing.T, extC uint8, types ...reflect.Type) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.typeTable = types
	builder.Emit(isa.OpMakeSlice, 0, 1, 2)
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, 0, 0, extC))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestMakeSliceHandlerBuildsTheRequestedShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		sliceType reflect.Type
		name      string
		length    int64
		capacity  int64
	}{
		{name: "an integer slice", sliceType: reflect.TypeFor[[]int](), length: 2, capacity: 4},
		{name: "a string slice", sliceType: reflect.TypeFor[[]string](), length: 1, capacity: 1},
		{name: "a struct slice", sliceType: reflect.TypeFor[[]leafCounter](), length: 2, capacity: 2},
		{name: "a zero-length slice", sliceType: reflect.TypeFor[[]int](), length: 0, capacity: 0},
		{name: "a length below the capacity", sliceType: reflect.TypeFor[[]int](), length: 1, capacity: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTypeTableFrame(t, isa.MakeSliceExtHeapFlag, tt.sliceType)
			registers.Ints[1] = tt.length
			registers.Ints[2] = tt.capacity

			require.Equal(t, opContinue, handleMakeSlice(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, int(tt.length), registers.General[0].Len())
			require.Equal(t, int(tt.capacity), registers.General[0].Cap())
			require.Equal(t, tt.sliceType, registers.General[0].Type())
		})
	}
}

func TestMakeSliceHandlerRefusesImpossibleOrOversizedRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		length       int64
		capacity     int64
		maxAllocSize int
		wantLimit    bool
	}{
		{name: "a negative length", length: -1, capacity: 0},
		{name: "a negative capacity", length: 0, capacity: -1},
		{name: "a capacity below the length", length: 4, capacity: 2},
		{name: "a length past the allocation cap", length: 8, capacity: 8, maxAllocSize: 4, wantLimit: true},
		{name: "a capacity past the allocation cap", length: 1, capacity: 8, maxAllocSize: 4, wantLimit: true},
	}

	for _, tt := range tests {
		t.Run(tt.name+" is refused", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTypeTableFrame(t, isa.MakeSliceExtHeapFlag, reflect.TypeFor[[]int]())
			vm.Limits.MaxAllocSize = tt.maxAllocSize
			registers.Ints[1] = tt.length
			registers.Ints[2] = tt.capacity

			got := handleMakeSlice(vm, frame, registers, op(0, 1, 2))

			require.NotEqual(t, opContinue, got)
			require.Error(t, vm.evalError)
			if tt.wantLimit {
				require.ErrorIs(t, vm.evalError, fault.ErrAllocationLimit)
			}
		})
	}

	t.Run("a type index past the table is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, isa.MakeSliceExtHeapFlag)

		require.Equal(t, opPanicError, handleMakeSlice(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}

func TestMakeMapHandlerBuildsAnEmptyMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mapType reflect.Type
		name    string
		hintLog uint8
	}{
		{name: "a string-keyed map with no hint", mapType: reflect.TypeFor[map[string]int](), hintLog: 0},
		{name: "an integer-keyed map with a small hint", mapType: reflect.TypeFor[map[int]string](), hintLog: 3},
		{name: "a map of slices", mapType: reflect.TypeFor[map[string][]int](), hintLog: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newTypeTableFrame(t, tt.hintLog, tt.mapType)

			require.Equal(t, opContinue, handleMakeMap(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, tt.mapType, registers.General[0].Type())
			require.Equal(t, 0, registers.General[0].Len())
		})
	}

	t.Run("a size hint past the allocation cap is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 6, reflect.TypeFor[map[string]int]())
		vm.Limits.MaxAllocSize = 8

		got := handleMakeMap(vm, frame, registers, op(0, 1, 2))

		require.Equal(t, opPanicError, got)
		require.ErrorIs(t, vm.evalError, fault.ErrAllocationLimit,
			"the map hint reserves space up front, so it must respect the same cap as make slice")
	})

	t.Run("a struct type allocates a struct literal instead", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0, reflect.TypeFor[leafCounter]())

		require.Equal(t, opContinue, handleMakeMap(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, reflect.Struct, registers.General[0].Kind())
	})

	t.Run("a type index past the table is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0)

		require.Equal(t, opPanicError, handleMakeMap(vm, frame, registers, op(0, 1, 2)))
	})
}

func TestMakeChannelHandlerHonoursTheBufferCap(t *testing.T) {
	t.Parallel()

	t.Run("an unbuffered channel is built", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0, reflect.TypeFor[chan int]())
		registers.Ints[1] = 0

		require.Equal(t, opContinue, handleMakeChannel(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, reflect.Chan, registers.General[0].Kind())
		require.Equal(t, 0, registers.General[0].Cap())
	})

	t.Run("a buffered channel is built", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0, reflect.TypeFor[chan int]())
		registers.Ints[1] = 4

		require.Equal(t, opContinue, handleMakeChannel(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 4, registers.General[0].Cap())
	})

	t.Run("a negative buffer is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0, reflect.TypeFor[chan int]())
		registers.Ints[1] = -1

		require.NotEqual(t, opContinue, handleMakeChannel(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})

	t.Run("a buffer past the allocation cap is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newTypeTableFrame(t, 0, reflect.TypeFor[chan int]())
		vm.Limits.MaxAllocSize = 2
		registers.Ints[1] = 8

		require.Equal(t, opPanicError, handleMakeChannel(vm, frame, registers, op(0, 1, 2)))
		require.ErrorIs(t, vm.evalError, fault.ErrAllocationLimit)
	})
}
