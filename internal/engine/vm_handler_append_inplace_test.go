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

func newAppendInPlaceFrame(t *testing.T, elementRegister uint8) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpAppendUintInPlace, 1, 2))
	builder.body = append(builder.body, isa.NewInstruction(isa.OpExt, elementRegister, 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	return vm, frame, registers
}

func TestAppendUintInPlaceGrowsThroughTheFallbackForHeapSlices(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slice   any
		check   func(t *testing.T, got reflect.Value)
		name    string
		element uint64
	}{
		{
			name: "a byte slice", slice: []byte{1, 2}, element: 3,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 3, got.Len())
				require.Equal(t, uint64(3), got.Index(2).Uint())
			},
		},
		{
			name: "an unsigned slice", slice: []uint64{1}, element: 2,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, uint64(2), got.Index(1).Uint())
			},
		},
		{
			name: "a narrow unsigned slice", slice: []uint32{1}, element: 2,
			check: func(t *testing.T, got reflect.Value) {
				require.Equal(t, 2, got.Len())
				require.Equal(t, uint64(2), got.Index(1).Uint())
			},
		},
		{
			name: "an empty slice", slice: []uint64{}, element: 1,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, 1, got.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newAppendInPlaceFrame(t, 3)
			registers.General[2] = reflect.ValueOf(tt.slice)
			registers.Uints[3] = tt.element

			require.Equal(t, opContinue, handleSubOpAppendUintInPlace(vm, frame, registers, op(0, 1, 2)))
			tt.check(t, registers.General[1])
		})
	}
}

func TestAppendUintInPlaceSeedsFromAnInvalidSlice(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newAppendInPlaceFrame(t, 3)
	registers.Uints[3] = 7

	require.Equal(t, opContinue, handleSubOpAppendUintInPlace(vm, frame, registers, op(0, 1, 2)))

	require.Equal(t, 1, registers.General[1].Len(),
		"an invalid source starts a fresh one-element slice rather than faulting")
	require.Equal(t, uint64(7), registers.General[1].Index(0).Uint())
}

func TestAppendUintInPlaceRefusesPastTheAllocationCap(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newAppendInPlaceFrame(t, 3)
	vm.Limits.MaxAllocSize = 2
	registers.General[2] = reflect.ValueOf([]uint64{1, 2})
	registers.Uints[3] = 3

	got := handleSubOpAppendUintInPlace(vm, frame, registers, op(0, 1, 2))

	require.Equal(t, opPanicError, got)
	require.Error(t, vm.evalError)
}

func TestAppendUintGrowFallbackConvertsToTheElementType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slice   any
		check   func(t *testing.T, got reflect.Value)
		name    string
		element uint64
	}{
		{
			name: "an unsigned slice keeps the value", slice: []uint64{1}, element: 42,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, uint64(42), got.Index(1).Uint()) },
		},
		{
			name: "a byte slice narrows the value", slice: []byte{1}, element: 7,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, uint64(7), got.Index(1).Uint()) },
		},
		{
			name: "a narrow unsigned slice narrows the value", slice: []uint16{1}, element: 300,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, uint64(300), got.Index(1).Uint()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := standardRegisters()

			got := handleSubOpAppendUintGrowFallback(&registers, op(0, 1, 0), reflect.ValueOf(tt.slice), tt.element)

			require.Equal(t, opContinue, got)
			require.Equal(t, 2, registers.General[1].Len())
			tt.check(t, registers.General[1])
		})
	}
}

func TestAppendUintFallbackInvalidStartsAByteSlice(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()

	got := handleSubOpAppendUintFallbackInvalid(&registers, op(0, 1, 0), 7)

	require.Equal(t, opContinue, got)
	require.Equal(t, 1, registers.General[1].Len())
	require.Equal(t, uint64(7), registers.General[1].Index(0).Uint())
}
