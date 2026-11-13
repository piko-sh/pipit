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

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/typemap"
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

func TestTruncateToWidthDescriptor(t *testing.T) {
	t.Parallel()

	shape := isa.OperandShapeFor(isa.OpTruncateToWidth)
	require.NotZero(t, shape.Flags&isa.ShapeFlagDescribed,
		"OpTruncateToWidth must have an operand-shape descriptor so the verifier and emit funnel can see its contract")
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
