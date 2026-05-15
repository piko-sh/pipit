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

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestASMLoopCancellable(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
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
	ctx, cancel := context.WithTimeout(context.Background(), fusedLoopCancelDeadline)
	defer cancel()
	start := time.Now()
	_, err := app.NewService().Execute(ctx, compiledFunction)
	elapsed := time.Since(start)
	require.Error(t, err)
	require.Less(t, elapsed, fusedLoopCancelGrace, "fused back edge must observe the deadline")
}

func buildIncIntJumpLtLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bound := bb.addIntConst(math.MaxInt64)
	bb.intRegisters(2).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
	bb.Emit(isa.OpLoadIntConst, 1, bound, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 0, 1)
	bb.emitExt(fusedLoopSelfOffset)
	bb.Emit(isa.OpNop, 0, 0, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func buildLenStringLtJumpFalseLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	text := bb.addStringConst(strings.Repeat("x", fusedLoopStringLength))
	bb.intRegisters(1).stringRegisters(1).returnInt()
	bb.Emit(isa.OpLoadStringConst, 0, text, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, fusedLoopStringLength+1)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
	bb.emitExt(fusedLoopSelfOffset)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func buildRangeCheckUintJumpFalseLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bb.intRegisters(1).uintRegisters(1).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadUintConstSmall), 0, 50)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2RangeCheckUintJumpFalse), 0)
	bb.Emit(isa.OpExt, 1, 10, 0)
	bb.emitExt(rangeCheckSelfOffset)
	for range engine.RangeCheckUintFusionNopCount {
		bb.Emit(isa.OpNop, 0, 0, 0)
	}
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func buildAddIntJumpLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	one := bb.addIntConst(1)
	bb.intRegisters(1).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
	bb.Emit(isa.OpAddIntJump, 0, 0, one)
	bb.emitExt(fusedLoopSelfOffset)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func buildEqIntConstJumpTrueLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	zero := bb.addIntConst(0)
	bb.intRegisters(1).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, zero)
	bb.emitExt(fusedLoopSelfOffset)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func buildLtIntConstJumpFalseLoop() *program.CompiledFunction {
	bb := newBytecodeBuilder()
	three := bb.addIntConst(3)
	bb.intRegisters(1).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 5)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLtIntConstJumpFalse), 0, three)
	bb.emitExt(fusedLoopSelfOffset)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	return bb.build()
}

func compileFusedLoopSource(t *testing.T, code string) *program.CompiledFunction {
	t.Helper()
	compiledFunction, err := app.NewService().Compile(context.Background(), code)
	require.NoError(t, err)
	return compiledFunction
}

func TestASMFusedLoopsCancellable(t *testing.T) {
	t.Parallel()

	synthetic := []struct {
		build  func() *program.CompiledFunction
		assert func(t *testing.T, compiledFunction *program.CompiledFunction)
		name   string
	}{
		{
			name:  "inc_int_jump_lt counted loop",
			build: buildIncIntJumpLtLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpIncIntJumpLt)
			},
		},
		{
			name:  "len_string_lt_jump_false loop",
			build: buildLenStringLtJumpFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLenStringLtJumpFalse)
			},
		},
		{
			name:  "range_check_uint_jump_false loop",
			build: buildRangeCheckUintJumpFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier2SubOp(t, compiledFunction, isa.SubOpTier2RangeCheckUintJumpFalse)
			},
		},
		{
			name:  "add_int_jump loop",
			build: buildAddIntJumpLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsOpcode(t, compiledFunction, isa.OpAddIntJump)
			},
		},
		{
			name:  "eq_int_const_jump_true loop",
			build: buildEqIntConstJumpTrueLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpEqIntConstJumpTrue)
			},
		},
		{
			name:  "lt_int_const_jump_false loop",
			build: buildLtIntConstJumpFalseLoop,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLtIntConstJumpFalse)
			},
		},
	}
	for _, tc := range synthetic {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := tc.build()
			tc.assert(t, compiledFunction)
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
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLtIntConstJumpFalse)
			},
		},
		{
			name: "len(s) loop",
			code: `s := "abc"; x := 0; for i := 0; i < len(s); i++ { x += int(s[i]); if x > 0 { i = 0 } }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpLenStringLtJumpFalse)
			},
		},
		{
			name: "uint range-check loop",
			code: `x := 0; u := uint(5); for u >= 1 && u <= 10 { x++; if x < 0 { u = 100 } }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier2SubOp(t, compiledFunction, isa.SubOpTier2RangeCheckUintJumpFalse)
			},
		},
		{
			name: "for x != 0 loop",
			code: `x := 1; for x != 0 { x = x + 1 }; x`,
			assert: func(t *testing.T, compiledFunction *program.CompiledFunction) {
				requireContainsTier1SubOp(t, compiledFunction, isa.SubOpJump)
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
