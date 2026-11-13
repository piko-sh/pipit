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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestASMLoopCancellable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := app.NewService().Eval(ctx, "func spin() int { x := 0; for { x++ } }\nspin()")
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 5*time.Second, "CPU-bound ASM loop must be interrupted by the deadline")
}

const (
	fusedLoopCancelDeadline = 300 * time.Millisecond
	fusedLoopCancelGrace    = 5 * time.Second
	fusedLoopSelfOffset     = -2
	rangeCheckSelfOffset    = -8
	fusedLoopStringLength   = 3
)

func requireFusedLoopCancelled(t *testing.T, compiledFunction *program.CompiledFunction) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), fusedLoopCancelDeadline)
	defer cancel()
	start := time.Now()
	_, err := app.NewService().Execute(ctx, compiledFunction)
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, fusedLoopCancelGrace, "fused back edge must observe the deadline")
}

func buildIncrementInt64ThenJumpIfLessThanLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	bound := block.addIntConst(math.MaxInt64)
	block.intRegisters(2).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 0)
	block.Emit(isa.OpLoadInt64Constant, 1, bound, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncrementInt64ThenJumpIfLessThan), 0, 1)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpNoOperation, 0, 0, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildIncrementInt64ThenJumpIfLessThanConstantLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	bound := block.addIntConst(math.MaxInt64)
	block.intRegisters(1).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant), 0, bound)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpNoOperation, 0, 0, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildLessThanLengthStringThenJumpIfFalseLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	text := block.addStringConst(strings.Repeat("x", fusedLoopStringLength))
	block.intRegisters(1).stringRegisters(1).returnInt()
	block.Emit(isa.OpLoadStringConstant, 0, text, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, fusedLoopStringLength+1)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLessThanLengthStringThenJumpIfFalse), 0, 0)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildInRangeUint64ThenJumpIfFalseLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	block.intRegisters(1).uintRegisters(1).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), 0, 50)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2InRangeUint64ThenJumpIfFalse), 0)
	block.Emit(isa.OpExtension, 1, 10, 0)
	block.emitExt(rangeCheckSelfOffset)
	for range engine.RangeCheckUintFusionNoOperationCount {
		block.Emit(isa.OpNoOperation, 0, 0, 0)
	}
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildAddInt64ConstantThenJumpLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	one := block.addIntConst(1)
	block.intRegisters(1).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 0)
	block.Emit(isa.OpAddInt64ConstantThenJump, 0, 0, one)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildEqualInt64ConstantThenJumpIfTrueLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	zero := block.addIntConst(0)
	block.intRegisters(1).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 0)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqualInt64ConstantThenJumpIfTrue), 0, zero)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func buildLessThanInt64ConstantThenJumpIfFalseLoop() *program.CompiledFunction {
	block := newBytecodeBuilder()
	three := block.addIntConst(3)
	block.intRegisters(1).returnInt()
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 5)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpLessThanInt64ConstantThenJumpIfFalse), 0, three)
	block.emitExt(fusedLoopSelfOffset)
	block.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return block.build()
}

func compileFusedLoopSource(t *testing.T, code string) *program.CompiledFunction {
	t.Helper()
	compiledFunction, err := app.NewService().Compile(t.Context(), code)
	require.NoError(t, err)
	return compiledFunction
}

func TestASMFusedLoopsCancellable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build  func() *program.CompiledFunction
		assert func(t *testing.T, compiledFunction *program.CompiledFunction)
		name   string
	}{
		{
			name:  "inc_int_jump_lt counted loop",
			build: buildIncrementInt64ThenJumpIfLessThanLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpIncrementInt64ThenJumpIfLessThan)
			},
		},
		{
			name:  "inc_int_jump_lt_const counted loop",
			build: buildIncrementInt64ThenJumpIfLessThanConstantLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpIncrementInt64ThenJumpIfLessThanConstant)
			},
		},
		{
			name:  "len_string_lt_jump_false loop",
			build: buildLessThanLengthStringThenJumpIfFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLessThanLengthStringThenJumpIfFalse)
			},
		},
		{
			name:  "range_check_uint_jump_false loop",
			build: buildInRangeUint64ThenJumpIfFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier2SubOp(t, compiledFunction, isa.SubOpTier2InRangeUint64ThenJumpIfFalse)
			},
		},
		{
			name:  "add_int_jump loop",
			build: buildAddInt64ConstantThenJumpLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsOpcode(t, compiledFunction, isa.OpAddInt64ConstantThenJump)
			},
		},
		{
			name:  "eq_int_const_jump_true loop",
			build: buildEqualInt64ConstantThenJumpIfTrueLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpEqualInt64ConstantThenJumpIfTrue)
			},
		},
		{
			name:  "lt_int_const_jump_false loop",
			build: buildLessThanInt64ConstantThenJumpIfFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLessThanInt64ConstantThenJumpIfFalse)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := tt.build()
			tt.assert(t, compiledFunction)
			requireFusedLoopCancelled(t, compiledFunction)
		})
	}

	source := []struct {
		assert func(t *testing.T, compiledFunction *program.CompiledFunction)
		name   string
		code   string
	}{
		{
			name: "counted loop",
			code: `x := 0; for i := 0; i < 9223372036854775807; i++ { x += i }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLessThanInt64ConstantThenJumpIfFalse)
			},
		},
		{
			name: "len(s) loop",
			code: `s := "abc"; x := 0; for i := 0; i < len(s); i++ { x += int(s[i]); if x > 0 { i = 0 } }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLessThanLengthStringThenJumpIfFalse)
			},
		},
		{
			name: "uint range-check loop",
			code: `x := 0; u := uint(5); for u >= 1 && u <= 10 { x++; if x < 0 { u = 100 } }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier2SubOp(t, compiledFunction, isa.SubOpTier2InRangeUint64ThenJumpIfFalse)
			},
		},
		{
			name: "for x != 0 loop",
			code: `x := 1; for x != 0 { x = x + 1 }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpEqualInt64ConstantThenJumpIfFalse)
			},
		},
		{
			name: "counted loop with a constant bound",
			code: `x := 0; for i := 0; i < 9; i++ { x++; i = 0 }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse)
			},
		},
		{
			name: "counted loop with a register bound",
			code: `x := 0; n := 9; for i := 0; i < n; i++ { x++; i = 0 }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				t.Helper()
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpIncrementInt64ThenJumpIfLessThan)
			},
		},
	}
	for _, tc := range source {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := compileFusedLoopSource(t, tc.code)
			tc.assert(t, compiledFunction)
			requireFusedLoopCancelled(t, compiledFunction)
		})
	}
}
