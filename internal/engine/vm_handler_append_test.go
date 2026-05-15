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
)

func TestAppendHandlerGrowsTheGeneralBankSlice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slice   any
		element any
		check   func(t *testing.T, got reflect.Value)
		name    string
	}{
		{
			name: "an integer slice", slice: []int{1, 2}, element: 3,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 3, got.Len())
				require.Equal(t, int64(3), got.Index(2).Int())
			},
		},
		{
			name: "a string slice", slice: []string{"a"}, element: "b",
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, "b", got.Index(1).String())
			},
		},
		{
			name: "a slice of interfaces accepts any element", slice: []any{1}, element: "mixed",
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
		{
			name: "a slice of structs", slice: []leafCounter{{Signed: 1}}, element: leafCounter{Signed: 2},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
		{
			name: "an empty slice grows to one", slice: []int{}, element: 1,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) },
		},
		{
			name: "a nil slice grows to one", slice: []int(nil), element: 1,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) },
		},
		{
			name: "a narrower element is converted to the element type", slice: []int64{1}, element: int32(2),
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, int64(2), got.Index(1).Int())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.slice)
			registers.General[2] = reflect.ValueOf(tt.element)

			require.Equal(t, opContinue, handleAppend(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers.General[0])
		})
	}
}

func TestAppendHandlerRefusesWhatItCannotType(t *testing.T) {
	t.Parallel()

	t.Run("appending an invalid element to a nil slice is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)

		got := handleAppend(vm, frame, registers, op(0, 1, 2))

		require.Equal(t, opPanicError, got, "there is no element type to build a slice from")
		require.Error(t, vm.evalError)
	})

	t.Run("appending past the allocation limit is refused", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 2
		registers.General[1] = reflect.ValueOf([]int{1, 2})
		registers.General[2] = reflect.ValueOf(3)

		got := handleAppend(vm, frame, registers, op(0, 1, 2))

		require.Equal(t, opPanicError, got)
		require.Error(t, vm.evalError)
	})
}

func TestTypedAppendHandlersWriteThroughTheScalarBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slice   any
		seed    func(registers *Registers)
		check   func(t *testing.T, got reflect.Value)
		handler opcodeHandler
		name    string
	}{
		{
			name: "an integer element", handler: handleAppendInt, slice: []int64{1},
			seed: func(registers *Registers) { registers.Ints[2] = 2 },
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, int64(2), got.Index(1).Int())
			},
		},
		{
			name: "an integer element onto a plain int slice", handler: handleAppendInt, slice: []int{1},
			seed:  func(registers *Registers) { registers.Ints[2] = 2 },
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
		{
			name: "a string element", handler: handleAppendString, slice: []string{"a"},
			seed: func(registers *Registers) { registers.Strings[2] = "b" },
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, "b", got.Index(1).String())
			},
		},
		{
			name: "a float element", handler: handleAppendFloat, slice: []float64{1},
			seed: func(registers *Registers) { registers.Floats[2] = 2.5 },
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.InDelta(t, 2.5, got.Index(1).Float(), 0)
			},
		},
		{
			name: "a boolean element", handler: handleAppendBool, slice: []bool{true},
			seed: func(registers *Registers) { registers.Bools[2] = false },
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.False(t, got.Index(1).Bool())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.slice)
			tt.seed(registers)

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers.General[0])
		})
	}

	t.Run("an integer element onto an invalid slice creates one", func(t *testing.T) {
		t.Parallel()
		vm, frame, registers := newStandardVM(t)
		registers.Ints[2] = 7

		require.Equal(t, opContinue, handleAppendInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, 1, registers.General[0].Len())
		require.Equal(t, int64(7), registers.General[0].Index(0).Int())
	})
}

func TestAppendSpreadJoinsTwoSlices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		destination any
		source      any
		check       func(t *testing.T, got reflect.Value)
		name        string
	}{
		{
			name: "two integer slices", destination: []int{1}, source: []int{2, 3},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 3, got.Len()) },
		},
		{
			name: "two string slices", destination: []string{"a"}, source: []string{"b"},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
		{
			name: "two byte slices", destination: []byte("ab"), source: []byte("cd"),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 4, got.Len()) },
		},
		{
			name: "an empty source leaves the destination length alone", destination: []int{1, 2}, source: []int{},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
		{
			name: "an empty destination takes the source", destination: []int{}, source: []int{1, 2},
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 2, got.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			registers.General[1] = reflect.ValueOf(tt.destination)
			registers.General[2] = reflect.ValueOf(tt.source)

			require.Equal(t, opContinue, handleAppendSpread(vm, nil, registers, op(0, 1, 2)))
			tt.check(t, registers.General[0])
		})
	}
}

func TestAppendScalarWritesTheDestinationRegister(t *testing.T) {
	t.Parallel()

	t.Run("an integer element lands in the general bank", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.General[1] = reflect.ValueOf([]int{1})

		require.Equal(t, opContinue, appendScalar(&registers, op(0, 1, 2), 2))
		require.Equal(t, 2, registers.General[0].Len())
	})

	t.Run("a string element lands in the general bank", func(t *testing.T) {
		t.Parallel()
		registers := standardRegisters()
		registers.General[1] = reflect.ValueOf([]string{"a"})

		require.Equal(t, opContinue, appendScalar(&registers, op(0, 1, 2), "b"))
		require.Equal(t, 2, registers.General[0].Len())
	})
}
