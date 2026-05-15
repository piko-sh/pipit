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
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestVMSyntheticBytecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expect    any
		build     func() *program.CompiledFunction
		name      string
		expectErr bool
	}{
		{
			name: "load_int_constant_and_return",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				index := b.addIntConst(42)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, index, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "load_small_int_and_return",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 99,
		},
		{
			name: "integer_addition",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.addIntConst(32)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpAddInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "integer_subtraction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(50)
				b.addIntConst(8)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpSubInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "integer_multiplication",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(6)
				b.addIntConst(7)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpMulInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "integer_division",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(84)
				b.addIntConst(2)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpDivInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "integer_remainder",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(47)
				b.addIntConst(5)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpRemInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 2,
		},
		{
			name: "division_by_zero",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(0)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpDivInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expectErr: true,
		},
		{
			name: "remainder_by_zero",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(0)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpRemInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expectErr: true,
		},
		{
			name: "integer_negation",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegInt), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: -42,
		},
		{
			name: "float_addition",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(1.5)
				b.addFloatConst(2.5)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 2, 1, 0)
				b.Emit(isa.OpAddFloat, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(4.0),
		},
		{
			name: "float_subtraction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(5.0)
				b.addFloatConst(2.5)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 2, 1, 0)
				b.Emit(isa.OpSubFloat, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(2.5),
		},
		{
			name: "float_multiplication",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.0)
				b.addFloatConst(14.0)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 2, 1, 0)
				b.Emit(isa.OpMulFloat, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(42.0),
		},
		{
			name: "float_division",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(84.0)
				b.addFloatConst(2.0)
				b.floatRegisters(3).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 2, 1, 0)
				b.Emit(isa.OpDivFloat, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(42.0),
		},
		{
			name: "float_negation",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.0)
				b.floatRegisters(2).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegFloat), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(-42.0),
		},
		{
			name: "boolean_load_true",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "boolean_load_false",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0,
		},
		{
			name: "not_true_gives_false",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNot), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0,
		},
		{
			name: "not_false_gives_true",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNot), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_comparison_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 0, 0)
				b.Emit(isa.OpEqInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_comparison_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(43)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpEqInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0,
		},
		{
			name: "int_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(43)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpNeInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_less_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.addIntConst(20)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpLtInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_less_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(10)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 0, 0)
				b.Emit(isa.OpLeInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_greater_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(20)
				b.addIntConst(10)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpGtInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "int_greater_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(20)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 0, 0)
				b.Emit(isa.OpGeInt, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "move_int_copies_register",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveInt), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "move_float",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.0)
				b.floatRegisters(2).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveFloat), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(42.0),
		},
		{
			name: "nop_has_no_effect",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpNop, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "return_void",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
				return b.build()
			},
			expect: nil,
		},
		{
			name: "jump_forward_skips_instruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.EmitJump(isa.OpDrillTier1, uint8(isa.SubOpJump), 1)
				b.Emit(isa.OpLoadIntConst, 0, 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "jump_if_true_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 1)
				b.EmitJump(isa.OpJumpIfTrue, 1, 1)
				b.Emit(isa.OpLoadIntConst, 0, 1, 0)
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "jump_if_true_not_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 0)
				b.EmitJump(isa.OpJumpIfTrue, 1, 1)
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "jump_if_false_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.addIntConst(99)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 0)
				b.EmitJump(isa.OpJumpIfFalse, 1, 1)
				b.Emit(isa.OpLoadIntConst, 0, 1, 0)
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "jump_if_false_not_taken",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 1)
				b.EmitJump(isa.OpJumpIfFalse, 1, 1)
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "bitwise_and",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpBitAnd, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0x0F,
		},
		{
			name: "bitwise_or",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xF0)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpBitOr, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0xFF,
		},
		{
			name: "bitwise_xor",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpBitXor, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0xF0,
		},
		{
			name: "bitwise_and_not",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(0xFF)
				b.addIntConst(0x0F)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpBitAndNot, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 0xF0,
		},
		{
			name: "bitwise_not",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBitNot), 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: -1,
		},
		{
			name: "shift_left",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(1)
				b.addIntConst(4)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpShiftLeft, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 16,
		},
		{
			name: "shift_right",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(16)
				b.addIntConst(2)
				b.intRegisters(3).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpLoadIntConst, 2, 1, 0)
				b.Emit(isa.OpShiftRight, 0, 1, 2)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 4,
		},
		{
			name: "int_to_float_conversion",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1).floatRegisters(1).returnFloat()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToFloat), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: float64(42.0),
		},
		{
			name: "float_to_int_conversion",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(42.7)
				b.intRegisters(1).floatRegisters(1).returnInt()
				b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpFloatToInt), 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "inc_int",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(41)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "dec_int",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(43)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecInt), 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "add_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(40)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpAddIntConst, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "sub_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(44)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpSubIntConst, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "mul_int_const_superinstruction",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(21)
				b.addIntConst(2)
				b.intRegisters(2).returnInt()
				b.Emit(isa.OpLoadIntConst, 1, 0, 0)
				b.Emit(isa.OpMulIntConst, 0, 1, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 42,
		},
		{
			name: "float_comparison_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.14)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
				b.Emit(isa.OpEqFloat, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "float_comparison_not_equal",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(3.14)
				b.addFloatConst(2.71)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 1, 1, 0)
				b.Emit(isa.OpNeFloat, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
		{
			name: "float_less_than",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(1.0)
				b.addFloatConst(2.0)
				b.intRegisters(1).floatRegisters(2).returnInt()
				b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				b.Emit(isa.OpLoadFloatConst, 1, 1, 0)
				b.Emit(isa.OpLtFloat, 0, 0, 1)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			expect: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := app.NewService()
			compiledFunction := tt.build()
			result, err := service.Execute(context.Background(), compiledFunction)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expect, result)
			}
		})
	}
}
