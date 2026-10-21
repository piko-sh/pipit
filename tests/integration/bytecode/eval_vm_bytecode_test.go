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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestVMSyntheticConstants(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "load_int_constant_and_return",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				index := b.addIntConst(42)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 0, index, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "load_small_int_and_return",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 99)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 99,
		},
		{
			name: "boolean_load_true",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "boolean_load_false",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0,
		},
		{
			name: "return_void",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
				return b.build()
			},
			want: nil,
		},
		{
			name: "nop_has_no_effect",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpNoOperation, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
	})
}

func TestVMSyntheticIntegerArithmetic(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "integer_addition",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.addIntConst(32)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpAddInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "integer_subtraction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(50)
				b.addIntConst(8)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpSubtractInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "integer_multiplication",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(6)
				b.addIntConst(7)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpMultiplyInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "integer_division",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(84)
				b.addIntConst(2)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpDivideInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "integer_remainder",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(47)
				b.addIntConst(5)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpRemainderInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 2,
		},
		{
			name: "division_by_zero",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(0)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpDivideInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			wantErr: true,
		},
		{
			name: "remainder_by_zero",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(0)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpRemainderInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			wantErr: true,
		},
		{
			name: "integer_negation",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegateInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: -42,
		},
		{
			name: "inc_int",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(41)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncrementInt64), 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "dec_int",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(43)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecrementInt64), 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "add_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(40)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpAddInt64Constant, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "sub_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(44)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpSubtractInt64Constant, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "mul_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(21)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpMultiplyInt64Constant, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
	})
}

func TestVMSyntheticFloatArithmetic(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "float_addition",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(1.5)
				b.addFloatConst(2.5)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
				b.Emit(isa.OpAddFloat64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(4.0),
		},
		{
			name: "float_subtraction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(5.0)
				b.addFloatConst(2.5)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
				b.Emit(isa.OpSubtractFloat64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(2.5),
		},
		{
			name: "float_multiplication",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.0)
				b.addFloatConst(14.0)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
				b.Emit(isa.OpMultiplyFloat64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(42.0),
		},
		{
			name: "float_division",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(84.0)
				b.addFloatConst(2.0)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
				b.Emit(isa.OpDivideFloat64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(42.0),
		},
		{
			name: "float_negation",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.0)
				b.floatRegisters(2).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegateFloat64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(-42.0),
		},
		{
			name: "int_to_float_conversion",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1).floatRegisters(1).returnFloat()
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToFloat64), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(42.0),
		},
		{
			name: "float_to_int_conversion",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.7)
				b.intRegisters(1).floatRegisters(1).returnInt()
				b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertFloat64ToInt64), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
	})
}

func TestVMSyntheticComparisons(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "not_true_gives_false",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNotInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0,
		},
		{
			name: "not_false_gives_true",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNotInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_comparison_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 0, 0)
				b.Emit(isa.OpEqualInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_comparison_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(43)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpEqualInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0,
		},
		{
			name: "int_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(43)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpNotEqualInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_less_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.addIntConst(20)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpLessThanInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_less_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 0, 0)
				b.Emit(isa.OpLessOrEqualInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_greater_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(20)
				b.addIntConst(10)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpGreaterThanInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "int_greater_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(20)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 0, 0)
				b.Emit(isa.OpGreaterOrEqualInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "float_comparison_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.14)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpEqualFloat64, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "float_comparison_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.14)
				b.addFloatConst(2.71)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 1, 1, 0)
				b.Emit(isa.OpNotEqualFloat64, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
		{
			name: "float_less_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(1.0)
				b.addFloatConst(2.0)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
				b.Emit(isa.OpLoadFloat64Constant, 1, 1, 0)
				b.Emit(isa.OpLessThanFloat64, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 1,
		},
	})
}

func TestVMSyntheticMovesAndJumps(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "move_int_copies_register",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "move_float",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.0)
				b.floatRegisters(2).returnFloat()
				b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveFloat64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: float64(42.0),
		},
		{
			name: "jump_forward_skips_instruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.EmitJump(isa.OpDrillTier1, uint8(isa.SubOpJump), 1)
				b.Emit(isa.OpLoadInt64Constant, 0, 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "jump_if_true_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 1)
				b.EmitJump(isa.OpJumpIfTrue, 1, 1)
				b.Emit(isa.OpLoadInt64Constant, 0, 1, 0)
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "jump_if_true_not_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 0)
				b.EmitJump(isa.OpJumpIfTrue, 1, 1)
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "jump_if_false_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 0)
				b.EmitJump(isa.OpJumpIfFalse, 1, 1)
				b.Emit(isa.OpLoadInt64Constant, 0, 1, 0)
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "jump_if_false_not_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 1)
				b.EmitJump(isa.OpJumpIfFalse, 1, 1)
				b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
	})
}

func TestVMSyntheticBitwise(t *testing.T) {
	t.Parallel()
	runSyntheticBytecode(t, []syntheticBytecodeCase{
		{
			name: "bitwise_and",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpBitAndInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0x0F,
		},
		{
			name: "bitwise_or",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xF0)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpBitOrInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0xFF,
		},
		{
			name: "bitwise_xor",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpBitXorInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0xF0,
		},
		{
			name: "bitwise_and_not",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpBitAndNotInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 0xF0,
		},
		{
			name: "bitwise_not",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBitNotInt64), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: -1,
		},
		{
			name: "shift_left",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(4)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpShiftLeftInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 16,
		},
		{
			name: "shift_right",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(16)
				b.addIntConst(2)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
				b.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
				b.Emit(isa.OpShiftRightInt64, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 4,
		},
	})
}

type syntheticBytecodeCase struct {
	name    string
	build   func() *program.CompiledFunction
	want    any
	wantErr bool
}

func runSyntheticBytecode(t *testing.T, tests []syntheticBytecodeCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := app.NewService().Execute(t.Context(), tt.build())
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}
