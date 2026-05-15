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

	"github.com/stretchr/testify/require"
)

func TestCollectionLengthOrCapHonoursTheArrayPointerRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value   reflect.Value
		name    string
		wantLen int64
		wantCap int64
		hasCap  bool
	}{
		{name: "a slice", value: reflect.ValueOf(make([]int, 2, 5)), wantLen: 2, wantCap: 5, hasCap: true},
		{name: "an empty slice", value: reflect.ValueOf([]int{}), wantLen: 0, wantCap: 0, hasCap: true},
		{name: "a nil slice", value: reflect.ValueOf([]int(nil)), wantLen: 0, wantCap: 0, hasCap: true},
		{name: "an array", value: reflect.ValueOf([3]int{}), wantLen: 3, wantCap: 3, hasCap: true},
		{name: "a pointer to an array reports the array length", value: reflect.ValueOf(&[4]int{}), wantLen: 4, wantCap: 4, hasCap: true},
		{name: "a nil pointer to an array still reports the length", value: reflect.ValueOf((*[4]int)(nil)), wantLen: 4, wantCap: 4, hasCap: true},
		{name: "a string measures its bytes", value: reflect.ValueOf("pipit"), wantLen: 5},
		{name: "a map measures its entries", value: reflect.ValueOf(map[string]int{"a": 1}), wantLen: 1},
		{name: "an invalid value measures zero", value: reflect.Value{}, wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantLen, collectionLengthOrCap(tt.value, reflect.Value.Len))
			if !tt.hasCap {

				return
			}
			require.Equal(t, tt.wantCap, collectionLengthOrCap(tt.value, reflect.Value.Cap))
		})
	}
}

func TestLenHandlerReadsTheGeneralBankCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value any
		name  string
		want  int64
	}{
		{name: "a slice", value: []int{1, 2, 3}, want: 3},
		{name: "a string", value: "pipit", want: 5},
		{name: "a map", value: map[string]int{"a": 1, "b": 2}, want: 2},
		{name: "an array", value: [4]int{}, want: 4},
		{name: "a nil slice", value: []int(nil), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()
			registers.General[1] = reflect.ValueOf(tt.value)

			require.Equal(t, opContinue, handleLen(nil, nil, &registers, op(0, 1, 0)))
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestAllocationTooLargeSpotsAnOverflowingRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		count       int
		elementSize uintptr
		want        bool
	}{
		{name: "a small request fits", count: 10, elementSize: 8, want: false},
		{name: "a zero count fits", count: 0, elementSize: 8, want: false},
		{name: "a zero element size fits", count: math.MaxInt64, elementSize: 0, want: false},
		{name: "a request that overflows the byte count is refused", count: math.MaxInt64, elementSize: 8, want: true},
		{name: "a request past the heap ceiling is refused", count: math.MaxInt32, elementSize: 1 << 20, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, allocationTooLarge(tt.count, tt.elementSize))
		})
	}
}

func TestMakeMapWithHintReservesSpaceWithoutAddingEntries(t *testing.T) {
	t.Parallel()

	const largestSafeHintLog2 = 8

	mapType := reflect.TypeFor[map[string]int]()

	tests := []struct {
		name    string
		hintLog uint8
	}{
		{name: "no hint allocates an unsized map", hintLog: 0},
		{name: "a hint of one", hintLog: 1},
		{name: "a small hint", hintLog: 3},
		{name: "the largest hint this test will allocate", hintLog: largestSafeHintLog2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.LessOrEqual(t, tt.hintLog, uint8(largestSafeHintLog2),
				"a larger hint would preallocate more memory than a test should ask for")

			made := makeMapWithHint(mapType, tt.hintLog)

			require.Equal(t, mapType, made.Type())
			require.Equal(t, 0, made.Len(), "a size hint reserves space without adding entries")
			made.SetMapIndex(reflect.ValueOf("k"), reflect.ValueOf(1))
			require.Equal(t, 1, made.Len())
		})
	}
}

func TestRefuseMakeSliceMatchesTheRuntimeChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		length   int
		capacity int
		refused  bool
		message  string
	}{
		{name: "a plain request is allowed", length: 2, capacity: 4},
		{name: "a zero request is allowed", length: 0, capacity: 0},
		{name: "a negative length is refused", length: -1, capacity: 0, refused: true, message: "len out of range"},
		{name: "a negative capacity is refused", length: 0, capacity: -1, refused: true, message: "cap out of range"},
		{name: "a capacity below the length is refused", length: 5, capacity: 2, refused: true, message: "cap out of range"},
		{name: "a length past the heap ceiling is refused", length: math.MaxInt64, capacity: math.MaxInt64, refused: true, message: "len out of range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, _ := newStandardVM(t)

			_, refused := refuseMakeSlice(vm, tt.length, tt.capacity, 8)

			require.Equal(t, tt.refused, refused)
			if tt.refused {
				require.Error(t, vm.evalError)
				require.Contains(t, vm.evalError.Error(), tt.message)
			}
		})
	}
}

func TestCopyHandlerRefusesOperandsItCannotCopy(t *testing.T) {
	t.Parallel()

	t.Run("copying between slices reports the element count", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		destination := make([]int, 3)
		registers.General[1] = reflect.ValueOf(destination)
		registers.General[2] = reflect.ValueOf([]int{1, 2, 3, 4})

		require.Equal(t, opContinue, handleCopy(vm, nil, registers, op(0, 1, 2)))
		require.Equal(t, int64(3), registers.Ints[0], "copy stops at the shorter operand")
		require.Equal(t, []int{1, 2, 3}, destination)
	})

	t.Run("copying a string into a byte slice is allowed", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		destination := make([]byte, 2)
		registers.General[1] = reflect.ValueOf(destination)
		registers.General[2] = reflect.ValueOf("pipit")

		require.Equal(t, opContinue, handleCopy(vm, nil, registers, op(0, 1, 2)))
		require.Equal(t, int64(2), registers.Ints[0])
	})

	tests := []struct {
		destination any
		source      any
		name        string
		message     string
	}{
		{name: "a non-slice destination", destination: 42, source: []int{1}, message: "destination is not a slice"},
		{name: "a map destination", destination: map[string]int{}, source: []int{1}, message: "destination is not a slice"},
		{name: "a non-collection source", destination: []int{1}, source: 42, message: "not a slice, array, or string"},
		{name: "a map source", destination: []int{1}, source: map[string]int{}, message: "not a slice, array, or string"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" is refused", func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.destination)
			registers.General[2] = reflect.ValueOf(tt.source)

			require.Equal(t, opPanicError, handleCopy(vm, nil, registers, op(0, 1, 2)))
			require.Error(t, vm.evalError)
			require.Contains(t, vm.evalError.Error(), tt.message)
		})
	}

	t.Run("an invalid source is refused", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf([]int{1})

		require.Equal(t, opPanicError, handleCopy(vm, nil, registers, op(0, 1, 2)))
		require.Contains(t, vm.evalError.Error(), "source is invalid")
	})
}

func TestPointerKindPredicatesAgree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want bool
	}{
		{name: "a pointer", kind: reflect.Pointer, want: true},
		{name: "an unsafe pointer", kind: reflect.UnsafePointer, want: true},
		{name: "a slice is not a pointer kind", kind: reflect.Slice, want: false},
		{name: "a map is not a pointer kind", kind: reflect.Map, want: false},
		{name: "an integer is not a pointer kind", kind: reflect.Int, want: false},
		{name: "a channel is not a pointer kind", kind: reflect.Chan, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isPointerKind(tt.kind))
			require.Equal(t, isPointerKind(tt.kind), isUnsafePointerKind(tt.kind),
				"the two predicates admit the same kinds")
		})
	}
}
