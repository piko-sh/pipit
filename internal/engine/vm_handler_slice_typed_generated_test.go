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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestTypedSliceLengthAndCapacityReadTheHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed    func(registers *Registers)
		name    string
		wantLen int64
		wantCap int64
	}{
		{
			name:    "an integer slice",
			seed:    func(registers *Registers) { registers.SlicesInt[2] = make([]int64, 3, 8) },
			wantLen: 3, wantCap: 8,
		},
		{
			name:    "an empty integer slice",
			seed:    func(registers *Registers) { registers.SlicesInt[2] = []int64{} },
			wantLen: 0, wantCap: 0,
		},
		{
			name:    "a nil integer slice",
			seed:    func(registers *Registers) { registers.SlicesInt[2] = nil },
			wantLen: 0, wantCap: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			tt.seed(&registers)

			require.Equal(t, opContinue, handleSubOpLenSliceIntDirect(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, tt.wantLen, registers.Ints[1])

			require.Equal(t, opContinue, handleSubOpCapSliceIntDirect(nil, nil, &registers, op(0, 1, 2)))
			require.Equal(t, tt.wantCap, registers.Ints[1])
		})
	}

	t.Run("every typed bank reports its own length", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.slicesFloat[2] = []float64{1, 2}
		registers.slicesString[2] = []string{"a"}
		registers.slicesBool[2] = []bool{true, false, true}
		registers.slicesUint[2] = []uint64{1, 2, 3, 4}
		registers.slicesByte[2] = []byte("hello")

		require.Equal(t, opContinue, handleSubOpLenSliceFloatDirect(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, int64(2), registers.Ints[1])

		require.Equal(t, opContinue, handleSubOpLenSliceStringDirect(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, int64(1), registers.Ints[1])

		require.Equal(t, opContinue, handleSubOpLenSliceBoolDirect(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, int64(3), registers.Ints[1])

		require.Equal(t, opContinue, handleSubOpLenSliceUintDirect(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, int64(4), registers.Ints[1])

		require.Equal(t, opContinue, handleSubOpLenSliceByteDirect(nil, nil, &registers, op(0, 1, 2)))
		require.Equal(t, int64(5), registers.Ints[1])
	})
}

func TestTypedSliceDirectReadsRefuseAnOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		seed    func(registers *Registers)
		check   func(t *testing.T, registers *Registers)
		handler opcodeHandler
		name    string
	}{
		{
			name:    "a float slice read",
			handler: handleSubOpSliceGetFloatDirect,
			seed:    func(registers *Registers) { registers.slicesFloat[2] = []float64{1.5, 2.5} },
			check:   func(t *testing.T, registers *Registers) { require.InDelta(t, 2.5, registers.Floats[1], 0) },
		},
		{
			name:    "a string slice read",
			handler: handleSubOpSliceGetStringDirect,
			seed:    func(registers *Registers) { registers.slicesString[2] = []string{"a", "b"} },
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, "b", registers.Strings[1]) },
		},
		{
			name:    "a boolean slice read",
			handler: handleSubOpSliceGetBoolDirect,
			seed:    func(registers *Registers) { registers.slicesBool[2] = []bool{false, true} },
			check:   func(t *testing.T, registers *Registers) { require.True(t, registers.Bools[1]) },
		},
		{
			name:    "an unsigned slice read",
			handler: handleSubOpSliceGetUintDirect,
			seed:    func(registers *Registers) { registers.slicesUint[2] = []uint64{7, 42} },
			check:   func(t *testing.T, registers *Registers) { require.Equal(t, uint64(42), registers.Uints[1]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" at a valid index", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
			tt.seed(registers)
			registers.Ints[3] = 1

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers)
			require.Equal(t, 2, frame.ProgramCounter, "the extension word is consumed exactly once")
		})

		t.Run(tt.name+" past the end", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
			tt.seed(registers)
			registers.Ints[3] = 99

			require.NotEqual(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Error(t, vm.evalError)
		})

		t.Run(tt.name+" at a negative index", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
			tt.seed(registers)
			registers.Ints[3] = -1

			require.NotEqual(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)),
				"a negative index must be refused, not read as a large unsigned offset")
		})
	}
}

func TestTypedSliceDirectWritesReachTheBackingArray(t *testing.T) {
	t.Parallel()

	t.Run("a float slice write", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		target := []float64{1, 2}
		registers.slicesFloat[1] = target
		registers.Ints[2] = 1
		registers.Floats[3] = 9.5

		require.Equal(t, opContinue, handleSubOpSliceSetFloatDirect(vm, frame, registers, op(0, 1, 2)))
		require.InDelta(t, 9.5, target[1], 0)
	})

	t.Run("an unsigned slice write", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		target := []uint64{1, 2}
		registers.slicesUint[1] = target
		registers.Ints[2] = 0
		registers.Uints[3] = 42

		require.Equal(t, opContinue, handleSubOpSliceSetUintDirect(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, uint64(42), target[0])
	})

	t.Run("a write past the end is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		registers.slicesFloat[1] = []float64{1}
		registers.Ints[2] = 5

		require.NotEqual(t, opContinue, handleSubOpSliceSetFloatDirect(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}

func TestTypedSliceDirectAppendGrowsTheDestination(t *testing.T) {
	t.Parallel()

	t.Run("an integer append adds the element", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		registers.SlicesInt[2] = []int64{1, 2}
		registers.Ints[3] = 3

		require.Equal(t, opContinue, handleSubOpAppendSliceIntDirect(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []int64{1, 2, 3}, registers.SlicesInt[1])
	})

	t.Run("an append to a nil slice creates one", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		registers.SlicesInt[2] = nil
		registers.Ints[3] = 1

		require.Equal(t, opContinue, handleSubOpAppendSliceIntDirect(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, []int64{1}, registers.SlicesInt[1])
	})

	t.Run("an append past the allocation limit is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		vm.Limits.MaxAllocSize = 2
		registers.SlicesInt[2] = []int64{1, 2}
		registers.Ints[3] = 3

		require.Equal(t, opPanicError, handleSubOpAppendSliceIntDirect(vm, frame, registers, op(0, 1, 2)))
		require.Error(t, vm.evalError)
	})
}

func TestTypedSliceReslicingHonoursItsBoundFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flags   uint8
		low     int64
		high    int64
		wantLen int
		wantCap int
		refused bool
	}{
		{name: "no bounds keeps the whole slice", flags: 0, wantLen: 4, wantCap: 8},
		{name: "a low bound trims the front", flags: isa.SliceLowBoundFlag, low: 1, wantLen: 3, wantCap: 7},
		{name: "a high bound trims the back", flags: isa.SliceHighBoundFlag, high: 2, wantLen: 2, wantCap: 8},
		{name: "both bounds trim each end", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 1, high: 3, wantLen: 2, wantCap: 7},
		{name: "a low bound past the high bound is refused", flags: isa.SliceLowBoundFlag | isa.SliceHighBoundFlag, low: 3, high: 1, refused: true},
		{name: "a negative low bound is refused", flags: isa.SliceLowBoundFlag, low: -1, refused: true},
		{name: "a high bound past the capacity is refused", flags: isa.SliceHighBoundFlag, high: 99, refused: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(tt.flags, 4, 5))
			registers.SlicesInt[2] = make([]int64, 4, 8)
			registers.Ints[4] = tt.low
			registers.Ints[5] = tt.high

			got := handleSubOpSliceSliceIntDirect(vm, frame, registers, op(0, 1, 2))

			if tt.refused {
				require.Equal(t, opPanicError, got, "an out-of-range reslice must not hand back a bad header")
				require.Error(t, vm.evalError)
				return
			}
			require.Equal(t, opContinue, got)
			require.Len(t, registers.SlicesInt[1], tt.wantLen)
			require.Equal(t, tt.wantCap, cap(registers.SlicesInt[1]))
		})
	}
}

func TestTypedSliceMakeRefusesANegativeOrOversizedRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		length   int64
		capacity int64
		refused  bool
	}{
		{name: "a plain length and capacity", length: 2, capacity: 4},
		{name: "a zero length", length: 0, capacity: 0},
		{name: "a negative length is refused", length: -1, capacity: 4, refused: true},
		{name: "a negative capacity is refused", length: 0, capacity: -1, refused: true},
		{name: "a length past the capacity is refused", length: 5, capacity: 2, refused: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(4, 0, 0))
			registers.Ints[2] = tt.length
			registers.Ints[4] = tt.capacity

			got := handleSubOpMakeSliceInt(vm, frame, registers, op(0, 1, 2))

			if tt.refused {
				require.NotEqual(t, opContinue, got, "an impossible slice request must be refused")
				return
			}
			require.Equal(t, opContinue, got)
			require.Len(t, registers.SlicesInt[1], int(tt.length))
		})
	}
}

func TestTypedSliceMakeCoversEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		check   func(t *testing.T, registers *Registers)
		name    string
	}{
		{
			name: "the integer bank", handler: handleSubOpMakeSliceInt,
			check: func(t *testing.T, registers *Registers) { require.Len(t, registers.SlicesInt[1], 2) },
		},
		{
			name: "the float bank", handler: handleSubOpMakeSliceFloat,
			check: func(t *testing.T, registers *Registers) { require.Len(t, registers.slicesFloat[1], 2) },
		},
		{
			name: "the string bank", handler: handleSubOpMakeSliceString,
			check: func(t *testing.T, registers *Registers) { require.Len(t, registers.slicesString[1], 2) },
		},
		{
			name: "the boolean bank", handler: handleSubOpMakeSliceBool,
			check: func(t *testing.T, registers *Registers) { require.Len(t, registers.slicesBool[1], 2) },
		},
		{
			name: "the unsigned bank", handler: handleSubOpMakeSliceUint,
			check: func(t *testing.T, registers *Registers) { require.Len(t, registers.slicesUint[1], 2) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(4, 0, 0))
			registers.Ints[2] = 2
			registers.Ints[4] = 4

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers)
		})

		t.Run(tt.name+" refuses a negative length", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(4, 0, 0))
			registers.Ints[2] = -1
			registers.Ints[4] = 4

			require.NotEqual(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)),
				"an impossible length must be refused in every bank")
		})
	}
}

func TestTypedSliceAppendCoversEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		seed    func(registers *Registers)
		check   func(t *testing.T, registers *Registers)
		name    string
	}{
		{
			name: "the integer bank", handler: handleSubOpAppendSliceIntDirect,
			seed:  func(r *Registers) { r.SlicesInt[2] = []int64{1}; r.Ints[3] = 2 },
			check: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1, 2}, r.SlicesInt[1]) },
		},
		{
			name: "the float bank", handler: handleSubOpAppendSliceFloatDirect,
			seed:  func(r *Registers) { r.slicesFloat[2] = []float64{1}; r.Floats[3] = 2 },
			check: func(t *testing.T, r *Registers) { require.Len(t, r.slicesFloat[1], 2) },
		},
		{
			name: "the string bank", handler: handleSubOpAppendSliceStringDirect,
			seed:  func(r *Registers) { r.slicesString[2] = []string{"a"}; r.Strings[3] = "b" },
			check: func(t *testing.T, r *Registers) { require.Equal(t, []string{"a", "b"}, r.slicesString[1]) },
		},
		{
			name: "the boolean bank", handler: handleSubOpAppendSliceBoolDirect,
			seed:  func(r *Registers) { r.slicesBool[2] = []bool{true}; r.Bools[3] = false },
			check: func(t *testing.T, r *Registers) { require.Len(t, r.slicesBool[1], 2) },
		},
		{
			name: "the unsigned bank", handler: handleSubOpAppendSliceUintDirect,
			seed:  func(r *Registers) { r.slicesUint[2] = []uint64{1}; r.Uints[3] = 2 },
			check: func(t *testing.T, r *Registers) { require.Equal(t, []uint64{1, 2}, r.slicesUint[1]) },
		},
		{
			name: "the byte bank", handler: handleSubOpAppendSliceByteDirect,
			seed:  func(r *Registers) { r.slicesByte[2] = []byte("a"); r.Uints[3] = 'b' },
			check: func(t *testing.T, r *Registers) { require.Equal(t, []byte("ab"), r.slicesByte[1]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
			tt.seed(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers)
		})
	}
}

func TestTypedSliceReslicingCoversEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		handler opcodeHandler
		seed    func(registers *Registers)
		length  func(registers *Registers) int
		name    string
	}{
		{
			name: "the integer bank", handler: handleSubOpSliceSliceIntDirect,
			seed:   func(r *Registers) { r.SlicesInt[2] = make([]int64, 4, 8) },
			length: func(r *Registers) int { return len(r.SlicesInt[1]) },
		},
		{
			name: "the float bank", handler: handleSubOpSliceSliceFloatDirect,
			seed:   func(r *Registers) { r.slicesFloat[2] = make([]float64, 4, 8) },
			length: func(r *Registers) int { return len(r.slicesFloat[1]) },
		},
		{
			name: "the string bank", handler: handleSubOpSliceSliceStringDirect,
			seed:   func(r *Registers) { r.slicesString[2] = make([]string, 4, 8) },
			length: func(r *Registers) int { return len(r.slicesString[1]) },
		},
		{
			name: "the boolean bank", handler: handleSubOpSliceSliceBoolDirect,
			seed:   func(r *Registers) { r.slicesBool[2] = make([]bool, 4, 8) },
			length: func(r *Registers) int { return len(r.slicesBool[1]) },
		},
		{
			name: "the unsigned bank", handler: handleSubOpSliceSliceUintDirect,
			seed:   func(r *Registers) { r.slicesUint[2] = make([]uint64, 4, 8) },
			length: func(r *Registers) int { return len(r.slicesUint[1]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" trims to the requested bounds", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(isa.SliceLowBoundFlag|isa.SliceHighBoundFlag, 4, 5))
			tt.seed(registers)
			registers.Ints[4] = 1
			registers.Ints[5] = 3

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, 2, tt.length(registers))
		})

		t.Run(tt.name+" refuses bounds outside the source", func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newExtWordFrame(t, op(isa.SliceHighBoundFlag, 4, 5))
			tt.seed(registers)
			registers.Ints[5] = 99

			require.Equal(t, opPanicError, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Error(t, vm.evalError)
		})
	}
}

func TestRangeNextAdvancesTheIteratorForEachBank(t *testing.T) {
	t.Parallel()

	t.Run("an integer slice yields each element in turn", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(0, 0, 0))
		registers.SlicesInt[1] = []int64{10, 20}
		registers.Ints[0] = -1

		for _, want := range []int64{10, 20} {
			frame.ProgramCounter = 1
			require.Equal(t, opContinue, handleRangeNextSliceInt(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, want, registers.Ints[2], "the element register carries the current element")
		}

		frame.ProgramCounter = 1
		require.Equal(t, opContinue, handleRangeNextSliceInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(2), registers.Ints[0], "the cursor comes to rest at the length")
	})

	t.Run("an exhausted slice jumps by the packed offset", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(3, 0, 0))
		registers.SlicesInt[1] = nil

		frame.ProgramCounter = 1
		require.Equal(t, opContinue, handleRangeNextSliceInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 5, frame.ProgramCounter, "the loop exit jumps past the body")
	})
}
