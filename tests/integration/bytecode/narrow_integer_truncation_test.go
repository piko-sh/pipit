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

//go:build integration

package bytecode_test

import (
	"go/types"
	"testing"

	"pipit.sh/pipit/internal/compile/typemap"

	"pipit.sh/pipit/internal/engine"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestNarrowIntegerBitWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		t        types.Type
		name     string
		expected uint8
	}{
		{name: "uint8", t: types.Typ[types.Uint8], expected: 8},
		{name: "uint16", t: types.Typ[types.Uint16], expected: 16},
		{name: "uint32", t: types.Typ[types.Uint32], expected: 32},
		{name: "uint64_no_truncation", t: types.Typ[types.Uint64], expected: 0},
		{name: "uint_no_truncation", t: types.Typ[types.Uint], expected: 0},
		{name: "uintptr_no_truncation", t: types.Typ[types.Uintptr], expected: 0},
		{name: "int8", t: types.Typ[types.Int8], expected: 8},
		{name: "int16", t: types.Typ[types.Int16], expected: 16},
		{name: "int32", t: types.Typ[types.Int32], expected: 32},
		{name: "int64_no_truncation", t: types.Typ[types.Int64], expected: 0},
		{name: "int_no_truncation", t: types.Typ[types.Int], expected: 0},
		{name: "string_no_truncation", t: types.Typ[types.String], expected: 0},
		{name: "float64_no_truncation", t: types.Typ[types.Float64], expected: 0},
		{name: "bool_no_truncation", t: types.Typ[types.Bool], expected: 0},
		{name: "nil_no_truncation", t: nil, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, typemap.NarrowIntegerBitWidth(tt.t))
		})
	}
}

func TestNarrowIntegerBitWidthDefinedType(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("testpkg", "test")
	byteType := types.NewNamed(types.NewTypeName(0, pkg, "Byte", nil), types.Typ[types.Uint8], nil)
	require.Equal(t, uint8(8), typemap.NarrowIntegerBitWidth(byteType),
		"defined types over uint8 should report 8-bit width via the underlying basic kind")

	wordType := types.NewNamed(types.NewTypeName(0, pkg, "Word", nil), types.Typ[types.Uint16], nil)
	require.Equal(t, uint8(16), typemap.NarrowIntegerBitWidth(wordType),
		"defined types over uint16 should report 16-bit width via the underlying basic kind")
}

func TestHandleTruncateNarrowUint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    uint64
		bitWidth uint8
		expected uint64
	}{
		{name: "uint8_no_overflow", input: 100, bitWidth: 8, expected: 100},
		{name: "uint8_wrap_at_256", input: 256, bitWidth: 8, expected: 0},
		{name: "uint8_wrap_at_300", input: 300, bitWidth: 8, expected: 44},
		{name: "uint8_max_unwrapped_underflow", input: 0xFFFFFFFFFFFFFFFF, bitWidth: 8, expected: 0xFF},
		{name: "uint16_wrap_at_65536", input: 65536, bitWidth: 16, expected: 0},
		{name: "uint16_wrap_at_70000", input: 70000, bitWidth: 16, expected: 4464},
		{name: "uint32_wrap_at_2_to_32", input: 1 << 32, bitWidth: 32, expected: 0},
		{name: "uint32_max", input: 0xFFFFFFFF, bitWidth: 32, expected: 0xFFFFFFFF},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := engine.NewRegisters(testRegCounts())
			registers.Uints[0] = tt.input
			engine.HandleTruncateNarrow(nil, nil, &registers, isa.Instruction{A: 0, B: tt.bitWidth, C: uint8(isa.RegisterUint)})
			require.Equal(t, tt.expected, registers.Uints[0])
		})
	}
}

func TestHandleTruncateNarrowInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    int64
		bitWidth uint8
		expected int64
	}{
		{name: "int8_no_overflow", input: 50, bitWidth: 8, expected: 50},
		{name: "int8_negative_in_range", input: -50, bitWidth: 8, expected: -50},
		{name: "int8_wrap_128_to_neg128", input: 128, bitWidth: 8, expected: -128},
		{name: "int8_wrap_neg129_to_127", input: -129, bitWidth: 8, expected: 127},
		{name: "int8_max_127", input: 127, bitWidth: 8, expected: 127},
		{name: "int8_min_neg128", input: -128, bitWidth: 8, expected: -128},
		{name: "int16_wrap_32768_to_neg32768", input: 32768, bitWidth: 16, expected: -32768},
		{name: "int16_wrap_neg32769_to_32767", input: -32769, bitWidth: 16, expected: 32767},
		{name: "int32_wrap_2_to_31_to_neg", input: 1 << 31, bitWidth: 32, expected: -1 << 31},
		{name: "int32_max", input: (1 << 31) - 1, bitWidth: 32, expected: (1 << 31) - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := engine.NewRegisters(testRegCounts())
			registers.Ints[0] = tt.input
			engine.HandleTruncateNarrow(nil, nil, &registers, isa.Instruction{A: 0, B: tt.bitWidth, C: uint8(isa.RegisterInt)})
			require.Equal(t, tt.expected, registers.Ints[0])
		})
	}
}

func TestHandleTruncateNarrowDescriptor(t *testing.T) {
	t.Parallel()

	shape := isa.OperandShapeFor(isa.OpTruncateNarrow)
	require.NotZero(t, shape.Flags&isa.ShapeFlagDescribed,
		"opTruncateNarrow must have an operand-shape descriptor so the verifier and emit funnel can see its contract")
	require.Equal(t, isa.RoleRegDynamic, shape.A,
		"operand A is the dynamic-bank register being truncated in place")
	require.Equal(t, isa.RoleImmediate, shape.B,
		"operand B is the bit width immediate (8/16/32)")
	require.Equal(t, isa.RoleKindMarker, shape.C,
		"operand C is the registerKind selecting the bank to truncate (registerInt or registerUint)")
	require.True(t, shape.Reads[0],
		"operand A is read in place")
	require.True(t, shape.Writes[0],
		"operand A is written in place")
}
