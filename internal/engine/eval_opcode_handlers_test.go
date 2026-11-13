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
	bb.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
	bb.Emit(isa.OpLoadInt64Constant, 2, 1, 0)
	bb.Emit(op, 0, 1, 2)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func buildFloatBinaryOp(op isa.Opcode, a, b float64) *program.CompiledFunction {
	bb := newBytecodeBuilder()
	bb.addFloatConst(a)
	bb.addFloatConst(b)
	bb.floatRegisters(3).returnFloat()
	bb.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
	bb.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
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
	bb.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
	bb.Emit(isa.OpLoadFloat64Constant, 1, 1, 0)
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

	t.Run("opAddInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpAddInt64, 10, 32), 42)
	})
	t.Run("opSubtractInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpSubtractInt64, 50, 8), 42)
	})
	t.Run("opMultiplyInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpMultiplyInt64, 6, 7), 42)
	})
	t.Run("opDivideInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpDivideInt64, 84, 2), 42)
	})
	t.Run("opDivInt_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildIntBinaryOp(isa.OpDivideInt64, 1, 0))
	})
	t.Run("opRemainderInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpRemainderInt64, 47, 5), 2)
	})
	t.Run("opRemInt_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildIntBinaryOp(isa.OpRemainderInt64, 1, 0))
	})
	t.Run("opNegateInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegateInt64), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -42)
	})
	t.Run("opIncrementInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(41)
		b.intRegisters(1).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncrementInt64), 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opDecrementInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(43)
		b.intRegisters(1).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecrementInt64), 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
}

func TestOpcodeHandlersFloat(t *testing.T) {
	t.Parallel()

	t.Run("opAddFloat64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpAddFloat64, 1.5, 2.5), float64(4.0))
	})
	t.Run("opSubtractFloat64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpSubtractFloat64, 5.0, 2.5), float64(2.5))
	})
	t.Run("opMultiplyFloat64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpMultiplyFloat64, 3.0, 14.0), float64(42.0))
	})
	t.Run("opDivideFloat64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatBinaryOp(isa.OpDivideFloat64, 84.0, 2.0), float64(42.0))
	})
	t.Run("opNegateFloat64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.0)
		b.floatRegisters(2).returnFloat()
		b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegateFloat64), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(-42.0))
	})
}

func TestOpcodeHandlersBitwise(t *testing.T) {
	t.Parallel()

	t.Run("opBitAndInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitAndInt64, 0xFF, 0x0F), 0x0F)
	})
	t.Run("opBitOrInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitOrInt64, 0xF0, 0x0F), 0xFF)
	})
	t.Run("opBitXorInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitXorInt64, 0xFF, 0x0F), 0xF0)
	})
	t.Run("opBitAndNotInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpBitAndNotInt64, 0xFF, 0x0F), 0xF0)
	})
	t.Run("opBitNotInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBitNotInt64), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -1)
	})
	t.Run("opShiftLeftInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpShiftLeftInt64, 1, 4), 16)
	})
	t.Run("opShiftRightInt64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntBinaryOp(isa.OpShiftRightInt64, 16, 2), 4)
	})
}

func TestOpcodeHandlersComparisons(t *testing.T) {
	t.Parallel()

	t.Run("opEqInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpEqualInt64, 42, 42), 1)
	})
	t.Run("opEqInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpEqualInt64, 42, 43), 0)
	})
	t.Run("opNeInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpNotEqualInt64, 42, 43), 1)
	})
	t.Run("opNeInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpNotEqualInt64, 42, 42), 0)
	})
	t.Run("opLtInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLessThanInt64, 10, 20), 1)
	})
	t.Run("opLtInt_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLessThanInt64, 20, 10), 0)
	})
	t.Run("opLeInt_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpLessOrEqualInt64, 10, 10), 1)
	})
	t.Run("opGtInt_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpGreaterThanInt64, 20, 10), 1)
	})
	t.Run("opGeInt_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildIntComparisonOp(isa.OpGreaterOrEqualInt64, 10, 10), 1)
	})
	t.Run("opNot_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNotInt64), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})
	t.Run("opNot_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNotInt64), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opEqFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpEqualFloat64, 3.14, 3.14), 1)
	})
	t.Run("opEqFloat_false", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpEqualFloat64, 3.14, 2.71), 0)
	})
	t.Run("opNeFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpNotEqualFloat64, 3.14, 2.71), 1)
	})
	t.Run("opLtFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpLessThanFloat64, 1.0, 2.0), 1)
	})
	t.Run("opLeFloat_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpLessOrEqualFloat64, 1.0, 1.0), 1)
	})
	t.Run("opGtFloat_true", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpGreaterThanFloat64, 2.0, 1.0), 1)
	})
	t.Run("opGeFloat_equal", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildFloatComparisonOp(isa.OpGreaterOrEqualFloat64, 1.0, 1.0), 1)
	})
}

func TestOpcodeHandlersMath(t *testing.T) {
	t.Parallel()

	buildMathUnary := func(subOp isa.SubOpcode, input float64) *program.CompiledFunction {
		b := newBytecodeBuilder()
		b.addFloatConst(input)
		b.floatRegisters(2).returnFloat()
		b.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
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

	t.Run("opConvertInt64ToFloat64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToFloat64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(42.0))
	})
	t.Run("opConvertFloat64ToInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.9)
		b.intRegisters(1).floatRegisters(1).returnInt()
		b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertFloat64ToInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opIntToFloat_negative", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(-42)
		b.intRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToFloat64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(-42.0))
	})
}

func TestOpcodeHandlersSuperinstructions(t *testing.T) {
	t.Parallel()

	t.Run("opAddInt64Constant", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(40)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
		b.Emit(isa.OpAddInt64Constant, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opSubtractInt64Constant", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(44)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
		b.Emit(isa.OpSubtractInt64Constant, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
	t.Run("opMultiplyInt64Constant", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(21)
		b.addIntConst(2)
		b.intRegisters(2).returnInt()
		b.Emit(isa.OpLoadInt64Constant, 1, 0, 0)
		b.Emit(isa.OpMultiplyInt64Constant, 0, 1, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})
}
