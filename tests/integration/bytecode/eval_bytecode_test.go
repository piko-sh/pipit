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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestCompilerEmitsExpectedBytecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want func(t *testing.T, compiledFunction *program.CompiledFunction)
		name string
		code string
	}{
		{
			name: "integer_literal_loads_constant",
			code: "42",
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()

				hasLoad := findOpcode(compiledFunction, isa.OpLoadInt64Constant) >= 0 ||
					findTier1SubOp(compiledFunction, isa.SubOpLoadInt64Immediate) >= 0
				require.True(t, hasLoad, "expected int load in:\n%s", compiledFunction.Disassemble())
			},
		},
		{
			name: "small_int_uses_small_load",
			code: "5",
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLoadInt64Immediate)
			},
		},
		{
			name: "integer_addition_compiles_to_add_int",
			code: `x := 1; y := 2; x + y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsAnyOpcode(t, compiledFunction, isa.OpAddInt64, isa.OpAddInt64Constant)
			},
		},
		{
			name: "integer_subtraction_compiles_to_sub_int",
			code: `x := 10; y := 3; x - y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsAnyOpcode(t, compiledFunction, isa.OpSubtractInt64, isa.OpSubtractInt64Constant)
			},
		},
		{
			name: "integer_multiplication_compiles_to_mul_int",
			code: `x := 3; y := 4; x * y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsAnyOpcode(t, compiledFunction, isa.OpMultiplyInt64, isa.OpMultiplyInt64Constant)
			},
		},
		{
			name: "integer_division_compiles_to_div_int",
			code: `x := 10; y := 3; x / y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpDivideInt64)
			},
		},
		{
			name: "float_addition_compiles_to_add_float",
			code: `x := 1.5; y := 2.5; x + y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpAddFloat64)
			},
		},
		{
			name: "float_subtraction_compiles_to_sub_float",
			code: `x := 5.0; y := 2.0; x - y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpSubtractFloat64)
			},
		},
		{
			name: "string_concatenation_compiles_to_concat_string",
			code: `x := "hello"; y := " world"; x + y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpConcatenateString)
			},
		},
		{
			name: "comparison_compiles_to_eq_int",
			code: `x := 5; x == 5`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()

				hasEq := findOpcode(compiledFunction, isa.OpEqualInt64) >= 0 ||
					findTier1SubOp(compiledFunction, isa.SubOpEqualInt64ConstantThenJumpIfFalse) >= 0 ||
					findTier1SubOp(compiledFunction, isa.SubOpEqualInt64ConstantThenJumpIfTrue) >= 0
				require.True(t, hasEq, "expected equality comparison in:\n%s", compiledFunction.Disassemble())
			},
		},
		{
			name: "conditional_compiles_to_jump_if_false",
			code: `x := 5; if x > 3 { x = 100 }; x`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()

				hasJump := findOpcode(compiledFunction, isa.OpJumpIfFalse) >= 0 ||
					findTier1SubOp(compiledFunction, isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse) >= 0
				require.True(t, hasJump, "expected conditional jump in:\n%s", compiledFunction.Disassemble())
			},
		},
		{
			name: "for_loop_compiles_to_jump",
			code: `sum := 0; for i := 0; i < 10; i++ { sum += i }; sum`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				require.True(t, hasBackwardJump(compiledFunction),
					"expected a jump back to the loop's top in:\n%s", compiledFunction.Disassemble())
			},
		},
		{
			name: "boolean_not_compiles_to_not",
			code: `x := true; !x`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpNotInt64)
			},
		},
		{
			name: "bitwise_and_compiles_to_bit_and",
			code: `x := 0xFF; y := 0x0F; x & y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpBitAndInt64)
			},
		},
		{
			name: "negation_compiles_to_neg_int",
			code: `x := 42; -x`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpNegateInt64)
			},
		},
		{
			name: "float_negation_compiles_to_neg_float",
			code: `x := 3.14; -x`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpNegateFloat64)
			},
		},
		{
			name: "closure_literal_exists_as_sub_function",
			code: `f := func() int { return 42 }; _ = f`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				require.NotEmpty(t, program.ExportFunctions(compiledFunction))
				requireContainsTier2SubOp(t, program.ExportFunctions(compiledFunction)[0], isa.SubOpTier2Return)
			},
		},
		{
			name: "int_constants_stored_in_pool",
			code: "12345",
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				found := slices.Contains(compiledFunction.IntConstants, 12345)
				require.True(t, found, "expected 12345 in IntConstants: %v", compiledFunction.IntConstants)
			},
		},
		{
			name: "float_constants_stored_in_pool",
			code: "3.14159",
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				found := slices.Contains(compiledFunction.FloatConstants, 3.14159)
				require.True(t, found, "expected 3.14159 in FloatConstants: %v", compiledFunction.FloatConstants)
			},
		},
		{
			name: "string_constants_stored_in_pool",
			code: `"hello world"`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				found := slices.Contains(compiledFunction.StringConstants, "hello world")
				require.True(t, found, "expected \"hello world\" in StringConstants: %v", compiledFunction.StringConstants)
			},
		},
		{
			name: "disassemble_produces_output",
			code: `x := 1; y := 2; x + y`,
			want: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				dis := compiledFunction.Disassemble()
				require.NotEmpty(t, dis)
				require.Contains(t, dis, "ADD_INT64")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := compileExpression(t, tt.code)
			tt.want(t, compiledFunction)
		})
	}
}
