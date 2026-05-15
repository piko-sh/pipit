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

package engine

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func buildIntBinaryOp(op isa.Opcode, a, b int64) *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bb.addIntConst(a)
	bb.addIntConst(b)
	bb.intRegisters(3).returnInt()
	bb.Emit(isa.OpLoadIntConst, 1, 0, 0)
	bb.Emit(isa.OpLoadIntConst, 2, 1, 0)
	bb.Emit(op, 0, 1, 2)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func buildFloatBinaryOp(op isa.Opcode, a, b float64) *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bb.addFloatConst(a)
	bb.addFloatConst(b)
	bb.floatRegisters(3).returnFloat()
	bb.Emit(isa.OpLoadFloatConst, 1, 0, 0)
	bb.Emit(isa.OpLoadFloatConst, 2, 1, 0)
	bb.Emit(op, 0, 1, 2)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func buildIntComparisonOp(op isa.Opcode, a, b int64) *program.CompiledFunction {
	return buildIntBinaryOp(op, a, b)
}

func buildFloatComparisonOp(op isa.Opcode, a, b float64) *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bb.addFloatConst(a)
	bb.addFloatConst(b)
	bb.intRegisters(1).floatRegisters(2).returnInt()
	bb.Emit(isa.OpLoadFloatConst, 0, 0, 0)
	bb.Emit(isa.OpLoadFloatConst, 1, 1, 0)
	bb.Emit(op, 0, 0, 1)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func execSynthetic(t *testing.T, compiledFunction *program.CompiledFunction) (any, error) {
	t.Helper()
	return executeTestBytecode(t, compiledFunction)
}

func requireSyntheticResult(t *testing.T, compiledFunction *program.CompiledFunction, expect any) {
	t.Helper()
	result, err := execSynthetic(t, compiledFunction)
	require.NoError(t, err)
	require.Equal(t, expect, result)
}

func requireSyntheticError(t *testing.T, compiledFunction *program.CompiledFunction) {
	t.Helper()
	_, err := execSynthetic(t, compiledFunction)
	require.Error(t, err)
}

func TestOpcodeHandlersArithmetic(t *testing.T) {
	t.Parallel()

	t.Run("opAddInt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpAddInt, 10, 32), 42)
	})
	t.Run("opSubInt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpSubInt, 50, 8), 42)
	})
	t.Run("opMulInt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpMulInt, 6, 7), 42)
	})
	t.Run("opDivInt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpDivInt, 84, 2), 42)
	})
	t.Run("opDivInt_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildIntBinaryOp(isa.OpDivInt, 1, 0))
	})
	t.Run("opRemInt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpRemInt, 47, 5), 2)
	})
	t.Run("opRemInt_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildIntBinaryOp(isa.OpRemInt, 1, 0))
	})
	t.Run("opNegInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadIntConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegInt), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -42)
	})
	t.Run("opIncInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(41)
		b.intRegisters(1).returnInt()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opDecInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(43)
		b.intRegisters(1).returnInt()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecInt), 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
}

func TestOpcodeHandlersFloat(t *testing.T) {
	t.Parallel()

	t.Run("opAddFloat", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpAddFloat, 1.5, 2.5), float64(4.0))
	})
	t.Run("opSubFloat", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpSubFloat, 5.0, 2.5), float64(2.5))
	})
	t.Run("opMulFloat", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpMulFloat, 3.0, 14.0), float64(42.0))
	})
	t.Run("opDivFloat", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpDivFloat, 84.0, 2.0), float64(42.0))
	})
	t.Run("opNegFloat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.0)
		b.floatRegisters(2).returnFloat()
		b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegFloat), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(-42.0))
	})
}

func TestOpcodeHandlersBitwise(t *testing.T) {
	t.Parallel()

	t.Run("opBitAnd", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitAnd, 0xFF, 0x0F), 0x0F)
	})
	t.Run("opBitOr", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitOr, 0xF0, 0x0F), 0xFF)
	})
	t.Run("opBitXor", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitXor, 0xFF, 0x0F), 0xF0)
	})
	t.Run("opBitAndNot", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitAndNot, 0xFF, 0x0F), 0xF0)
	})
	t.Run("opBitNot", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBitNot), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -1)
	})
	t.Run("opShiftLeft", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpShiftLeft, 1, 4), 16)
	})
	t.Run("opShiftRight", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpShiftRight, 16, 2), 4)
	})
}

func TestOpcodeHandlersComparisons(t *testing.T) {
	t.Parallel()

	t.Run("opEqInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpEqInt, 42, 42), 1)
	})
	t.Run("opEqInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpEqInt, 42, 43), 0)
	})
	t.Run("opNeInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpNeInt, 42, 43), 1)
	})
	t.Run("opNeInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpNeInt, 42, 42), 0)
	})
	t.Run("opLtInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLtInt, 10, 20), 1)
	})
	t.Run("opLtInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLtInt, 20, 10), 0)
	})
	t.Run("opLeInt_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLeInt, 10, 10), 1)
	})
	t.Run("opGtInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpGtInt, 20, 10), 1)
	})
	t.Run("opGeInt_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpGeInt, 10, 10), 1)
	})
	t.Run("opNot_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNot), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})
	t.Run("opNot_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBool), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNot), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opEqFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpEqFloat, 3.14, 3.14), 1)
	})
	t.Run("opEqFloat_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpEqFloat, 3.14, 2.71), 0)
	})
	t.Run("opNeFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpNeFloat, 3.14, 2.71), 1)
	})
	t.Run("opLtFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpLtFloat, 1.0, 2.0), 1)
	})
	t.Run("opLeFloat_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpLeFloat, 1.0, 1.0), 1)
	})
	t.Run("opGtFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpGtFloat, 2.0, 1.0), 1)
	})
	t.Run("opGeFloat_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpGeFloat, 1.0, 1.0), 1)
	})
}

func TestOpcodeHandlersMath(t *testing.T) {
	t.Parallel()

	buildMathUnary := func(subOp isa.SubOpcode, input float64) *program.CompiledFunction {
		b := newBytecodeBuilder()
		b.addFloatConst(input)
		b.floatRegisters(2).returnFloat()
		b.Emit(isa.OpLoadFloatConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(subOp), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return b.build()
	}

	t.Run("opMathSqrt", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathSqrt, 16.0), float64(4.0))
	})
	t.Run("opMathAbs_positive", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathAbs, 42.0), float64(42.0))
	})
	t.Run("opMathAbs_negative", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathAbs, -42.0), float64(42.0))
	})
	t.Run("opMathFloor", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathFloor, 3.7), float64(3.0))
	})
	t.Run("opMathCeil", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathCeil, 3.2), float64(4.0))
	})
	t.Run("opMathTrunc", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathTrunc, 3.7), float64(3.0))
	})
	t.Run("opMathRound", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathUnary(isa.SubOpMathRound, 3.5), float64(4.0))
	})
	t.Run("opMathSqrt_NaN", func(t *testing.T) {
		t.Parallel()
		result, err := execSynthetic(t, buildMathUnary(isa.SubOpMathSqrt, -1.0))
		require.NoError(t, err)
		require.True(t, math.IsNaN(result.(float64)))
	})
}

func TestOpcodeHandlersConversions(t *testing.T) {
	t.Parallel()

	t.Run("opIntToFloat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToFloat), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(42.0))
	})
	t.Run("opFloatToInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.9)
		b.intRegisters(1).floatRegisters(1).returnInt()
		b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpFloatToInt), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opIntToFloat_negative", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(-42)
		b.intRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToFloat), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(-42.0))
	})
}

func TestOpcodeHandlersSuperinstructions(t *testing.T) {
	t.Parallel()

	t.Run("opAddIntConst", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(40)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadIntConst, 1, 0, 0)
		b.Emit(isa.OpAddIntConst, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opSubIntConst", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(44)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadIntConst, 1, 0, 0)
		b.Emit(isa.OpSubIntConst, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opMulIntConst", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(21)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadIntConst, 1, 0, 0)
		b.Emit(isa.OpMulIntConst, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
}
