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
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

func execSyntheticGoDispatch(t *testing.T, compiledFunction *program.CompiledFunction) (any, error) {
	t.Helper()
	service := app.NewService(app.WithForceGoDispatch())
	return service.Execute(context.Background(), compiledFunction)
}

func requireSyntheticGoDispatchResult(t *testing.T, compiledFunction *program.CompiledFunction, expect any) {
	t.Helper()
	result, err := execSyntheticGoDispatch(t, compiledFunction)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != expect {
		t.Fatalf("expected %v, got %v", expect, result)
	}
}

func TestFusedOpcodeEqIntConstJumpTrue(t *testing.T) {
	t.Parallel()

	t.Run("equal jumps over overwrite", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(42)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 42)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 42)
	})

	t.Run("not equal falls through", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(42)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 41)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})

	t.Run("zero constant", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(0)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 0)
	})

	t.Run("max int64", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(math.MaxInt64)
		loadIndex := bb.addIntConst(math.MaxInt64)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpLoadIntConst, 0, loadIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), math.MaxInt64)
	})
}

func TestFusedOpcodeGeIntConstJumpFalse(t *testing.T) {
	t.Parallel()

	t.Run("greater than constant no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGeIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})

	t.Run("equal to constant no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 5)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGeIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})

	t.Run("less than constant jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 3)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGeIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 3)
	})
}

func TestFusedOpcodeGtIntConstJumpFalse(t *testing.T) {
	t.Parallel()

	t.Run("greater than constant no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGtIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})

	t.Run("equal to constant jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 5)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGtIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 5)
	})

	t.Run("less than constant jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(5)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 3)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpGtIntConstJumpFalse), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 3)
	})
}

func TestFusedOpcodeAddIntJump(t *testing.T) {
	t.Parallel()

	t.Run("basic addition with jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(32)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 10)
		bb.Emit(isa.OpAddIntJump, 0, 1, constIndex)
		bb.emitExt(1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 42)
	})

	t.Run("negative constant", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(-8)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 50)
		bb.Emit(isa.OpAddIntJump, 0, 1, constIndex)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 42)
	})

	t.Run("overflow wraps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		loadIndex := bb.addIntConst(math.MaxInt64)
		constIndex := bb.addIntConst(1)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpLoadIntConst, 1, loadIndex, 0)
		bb.Emit(isa.OpAddIntJump, 0, 1, constIndex)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), math.MinInt64)
	})

	t.Run("zero constant", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(0)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 42)
		bb.Emit(isa.OpAddIntJump, 0, 1, constIndex)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 42)
	})
}

func TestFusedOpcodeIncIntJumpLt(t *testing.T) {
	t.Parallel()

	t.Run("increment and fall through when equal", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 9)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 0, 1)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 10)
	})

	t.Run("increment and fall through when greater", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 0, 1)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 11)
	})

	t.Run("loop summing 0 to 4", func(t *testing.T) {
		t.Parallel()

		bb := newBytecodeBuilder()
		bb.intRegisters(3).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 2, 5)

		bb.Emit(isa.OpAddInt, 0, 0, 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 1, 2)
		bb.emitExt(-3)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 10)
	})

	t.Run("overflow wraps and compares correctly", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		maxIndex := bb.addIntConst(math.MaxInt64)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpLoadIntConst, 0, maxIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 0, 1)
		bb.emitExt(0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

		requireSyntheticResult(t, bb.build(), math.MinInt64)
	})
}

func TestFusedOpcodeLenStringLtJumpFalse(t *testing.T) {
	t.Parallel()

	t.Run("index less than length no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("hello")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})

	t.Run("index equals length jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("hello")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 5)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 5)
	})

	t.Run("index greater than length jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("hello")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 10)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 10)
	})

	t.Run("empty string always jumps", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 0)
	})

	t.Run("negative index no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("x")
		negIndex := bb.addIntConst(-1)
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpLoadIntConst, 0, negIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 99)
	})
}

func TestFusedOpcodeHandlersMulIntConst(t *testing.T) {
	t.Parallel()

	t.Run("basic multiplication", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(7)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 6)
		bb.Emit(isa.OpMulIntConst, 0, 1, constIndex)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, bb.build(), 42)
	})
}

func TestFusedOpcodeExtStandalone(t *testing.T) {
	t.Parallel()

	bb := newBytecodeBuilder()
	bb.intRegisters(1).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 42)
	bb.emitExt(12345)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	requireSyntheticResult(t, bb.build(), 42)
}

func TestFusedOpcodesMatchUnderGoDispatch(t *testing.T) {
	t.Parallel()

	t.Run("EqIntConstJumpTrue", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(42)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 42)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 42)
	})

	t.Run("EqIntConstJumpTrue fall through", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(42)
		bb.intRegisters(1).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 41)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpEqIntConstJumpTrue), 0, constIndex)
		bb.emitExt(1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 99)
	})

	t.Run("AddIntJump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		constIndex := bb.addIntConst(32)
		bb.intRegisters(2).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 10)
		bb.Emit(isa.OpAddIntJump, 0, 1, constIndex)
		bb.emitExt(1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 42)
	})

	t.Run("IncIntJumpLt loop", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		bb.intRegisters(3).returnInt()
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 2, 5)
		bb.Emit(isa.OpAddInt, 0, 0, 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), 1, 2)
		bb.emitExt(-3)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 10)
	})

	t.Run("LenStringLtJumpFalse", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("hello")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 5)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 5)
	})

	t.Run("LenStringLtJumpFalse no jump", func(t *testing.T) {
		t.Parallel()
		bb := newBytecodeBuilder()
		strIndex := bb.addStringConst("hello")
		bb.intRegisters(1).stringRegisters(1).returnInt()
		bb.Emit(isa.OpLoadStringConst, 0, strIndex, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), 0, 0)
		bb.emitExt(2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 99)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticGoDispatchResult(t, bb.build(), 99)
	})
}
