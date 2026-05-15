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

package isa

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWideOperandPackingRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value uint16
	}{
		{name: "zero", value: 0},
		{name: "a single byte", value: 42},
		{name: "one below the byte boundary", value: 255},
		{name: "the byte boundary", value: 256},
		{name: "the largest wide operand", value: math.MaxUint16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			low, high := SplitWide(tt.value)

			require.Equal(t, tt.value, JoinWide(low, high))
			require.Equal(t, tt.value, NewInstruction(OpAddInt, 0, low, high).WideIndex(),
				"a wide operand is decoded from operands B and C")
		})
	}
}

func TestSignedOffsetPackingRoundTrips(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int16
	}{
		{name: "zero", value: 0},
		{name: "a small forward offset", value: 42},
		{name: "a small backward offset", value: -42},
		{name: "minus one, which sets every bit", value: -1},
		{name: "the largest forward offset", value: math.MaxInt16},
		{name: "the largest backward offset", value: math.MinInt16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			low, high := SplitOffset(tt.value)

			require.Equal(t, tt.value, JoinOffset(low, high))
			require.Equal(t, tt.value, NewInstruction(OpJumpIfFalse, 0, low, high).SignedOffset(),
				"a jump offset is decoded from operands B and C")
		})
	}
}

func TestDecodeExtension24ReadsAllThreeOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int
	}{
		{name: "zero", value: 0},
		{name: "a single byte", value: 0xFF},
		{name: "two bytes", value: 0xFFFF},
		{name: "a mixed pattern", value: 0xABCDEF},
		{name: "the largest payload", value: 0xFFFFFF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			extension := NewInstruction(OpExt,
				uint8(tt.value&0xFF),
				uint8((tt.value>>8)&0xFF),
				uint8((tt.value>>16)&0xFF),
			)

			require.Equal(t, tt.value, DecodeExtension24(extension))
		})
	}
}

func TestInstructionConstructorsBuildTheDrillEncoding(t *testing.T) {
	t.Parallel()

	t.Run("a main-tier instruction keeps its opcode and operands", func(t *testing.T) {
		t.Parallel()
		instruction := NewInstruction(OpAddInt, 1, 2, 3)

		require.Equal(t, OpAddInt, instruction.Op)
		require.Equal(t, uint8(1), instruction.A)
		require.Equal(t, uint8(2), instruction.B)
		require.Equal(t, uint8(3), instruction.C)
	})

	t.Run("a first-tier instruction spends operand A on the sub-opcode", func(t *testing.T) {
		t.Parallel()
		instruction := NewTier1Instruction(SubOpNegInt, 2, 3)

		require.Equal(t, OpDrillTier1, instruction.Op)
		require.Equal(t, uint8(SubOpNegInt), instruction.A)
		require.Equal(t, uint8(2), instruction.B)
		require.Equal(t, uint8(3), instruction.C)
	})

	t.Run("a second-tier instruction spends operands A and B", func(t *testing.T) {
		t.Parallel()
		instruction := NewTier2Instruction(SubOpTier2Return, 3)

		require.Equal(t, OpDrillTier1, instruction.Op)
		require.Equal(t, uint8(SubOpDrillTier2), instruction.A)
		require.Equal(t, uint8(SubOpTier2Return), instruction.B)
		require.Equal(t, uint8(3), instruction.C)
	})

	t.Run("a third-tier instruction spends every operand", func(t *testing.T) {
		t.Parallel()
		instruction := NewTier3Instruction(SubOpTier3ReturnVoid)

		require.Equal(t, OpDrillTier1, instruction.Op)
		require.Equal(t, uint8(SubOpDrillTier2), instruction.A)
		require.Equal(t, uint8(SubOpTier2DrillTier3), instruction.B)
		require.Equal(t, uint8(SubOpTier3ReturnVoid), instruction.C)
	})
}

func TestInstrIsTier1SubOpMatchesOnlyItsOwnSubOpcode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instruction Instruction
		name        string
		subOp       SubOpcode
		want        bool
	}{
		{name: "the matching sub-opcode", instruction: NewTier1Instruction(SubOpNegInt, 0, 0), subOp: SubOpNegInt, want: true},
		{name: "a different sub-opcode", instruction: NewTier1Instruction(SubOpNegInt, 0, 0), subOp: SubOpMoveUint, want: false},
		{name: "a main-tier instruction is never a tier-one sub-op", instruction: NewInstruction(OpAddInt, 0, 0, 0), subOp: SubOpNegInt, want: false},
		{name: "an extension word is never a tier-one sub-op", instruction: NewInstruction(OpExt, 0, 0, 0), subOp: SubOpNegInt, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, InstrIsTier1SubOp(tt.instruction, tt.subOp))
		})
	}
}

func TestIsTypedSliceKindPartitionsTheBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind RegisterKind
		want bool
	}{
		{name: "the integer bank is a scalar bank", kind: RegisterInt, want: false},
		{name: "the float bank is a scalar bank", kind: RegisterFloat, want: false},
		{name: "the string bank is a scalar bank", kind: RegisterString, want: false},
		{name: "the general bank is a scalar bank", kind: RegisterGeneral, want: false},
		{name: "the boolean bank is a scalar bank", kind: RegisterBool, want: false},
		{name: "the unsigned bank is a scalar bank", kind: RegisterUint, want: false},
		{name: "the complex bank is a scalar bank", kind: RegisterComplex, want: false},
		{name: "the integer slice bank is typed", kind: RegisterSliceInt, want: true},
		{name: "the float slice bank is typed", kind: RegisterSliceFloat, want: true},
		{name: "the string slice bank is typed", kind: RegisterSliceString, want: true},
		{name: "the boolean slice bank is typed", kind: RegisterSliceBool, want: true},
		{name: "the unsigned slice bank is typed", kind: RegisterSliceUint, want: true},
		{name: "the byte slice bank is typed", kind: RegisterSliceByte, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsTypedSliceKind(tt.kind))
		})
	}
}

func TestElementKindForTypedSliceNamesTheScalarBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind RegisterKind
		want RegisterKind
	}{
		{name: "an integer slice holds integers", kind: RegisterSliceInt, want: RegisterInt},
		{name: "a float slice holds floats", kind: RegisterSliceFloat, want: RegisterFloat},
		{name: "a string slice holds strings", kind: RegisterSliceString, want: RegisterString},
		{name: "a boolean slice holds booleans", kind: RegisterSliceBool, want: RegisterBool},
		{name: "an unsigned slice holds unsigned values", kind: RegisterSliceUint, want: RegisterUint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ElementKindForTypedSlice(tt.kind))
		})
	}
}
