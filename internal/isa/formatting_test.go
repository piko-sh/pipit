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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpcodeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		opcode   Opcode
	}{
		{name: "drill_tier1", opcode: OpDrillTier1, expected: "DRILL_TIER1"},
		{name: "add_int", opcode: OpAddInt64, expected: "ADD_INT64"},
		{name: "jump_if_false", opcode: OpJumpIfFalse, expected: "JUMP_IF_FALSE"},

		{name: "eq_int", opcode: OpEqualInt64, expected: "EQUAL_INT64"},
		{name: "load_int_const", opcode: OpLoadInt64Constant, expected: "LOAD_INT64_CONSTANT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.opcode.String())
		})
	}
}

func TestRegisterKindString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		kind     RegisterKind
	}{
		{name: "int64", kind: RegisterInt64, expected: "int64"},
		{name: "float64", kind: RegisterFloat64, expected: "float64"},
		{name: "string", kind: RegisterString, expected: "string"},
		{name: "general", kind: RegisterGeneral, expected: "general"},
		{name: "unknown", kind: RegisterKind(99), expected: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, tt.kind.String())
		})
	}
}

func TestInstructionString(t *testing.T) {
	t.Parallel()

	instr := NewInstruction(OpAddInt64, 0, 1, 2)
	got := instr.String()
	require.Contains(t, got, "ADD_INT64")
}

func TestInstructionSignedOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		b        uint8
		c        uint8
		expected int16
	}{
		{name: "positive_small", b: 1, c: 0, expected: 1},
		{name: "negative_one", b: 0xFF, c: 0xFF, expected: -1},
		{name: "positive_256", b: 0, c: 1, expected: 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instr := NewInstruction(OpDrillTier1, uint8(SubOpJump), tt.b, tt.c)
			require.Equal(t, tt.expected, instr.SignedOffset())
		})
	}
}

func TestInstructionWideIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		b        uint8
		c        uint8
		expected uint16
	}{
		{name: "low_byte_only", b: 1, c: 0, expected: 1},
		{name: "high_byte_only", b: 0, c: 1, expected: 256},
		{name: "max_value", b: 0xFF, c: 0xFF, expected: 65535},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instr := NewInstruction(OpLoadInt64Constant, 0, tt.b, tt.c)
			require.Equal(t, tt.expected, instr.WideIndex())
		})
	}
}
