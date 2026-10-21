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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestOpcodeHandlersStringOps(t *testing.T) {
	t.Parallel()

	t.Run("opLoadStringConstant", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).returnString()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opMoveString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("world")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveString), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "world")
	})

	t.Run("opConcatenateString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.addStringConst(" world")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 2, 1, 0)
		b.Emit(isa.OpConcatenateString, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello world")
	})

	t.Run("opConcatString_empty", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("test")
		b.addStringConst("")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 2, 1, 0)
		b.Emit(isa.OpConcatenateString, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "test")
	})

	t.Run("opLengthString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLengthString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 5)
	})

	t.Run("opLenString_empty", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("")
		b.stringRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLengthString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opEqString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("abc")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpEqualString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opEqString_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpEqualString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opNeString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpNotEqualString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opLtString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpLessThanString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opGtString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("xyz")
		b.addStringConst("abc")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpGreaterThanString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opConvertRuneToString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(65)
		b.intRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertRuneToString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "A")
	})
}

func TestOpcodeHandlersGeneralOps(t *testing.T) {
	t.Parallel()

	t.Run("opLoadNil", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.generalRegisters(1).returnGeneral()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		result, err := execSynthetic(t, b.build())
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("opLoadGeneralConstant", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(42))
		b.generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadGeneralConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opMoveGeneral", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf("from general"))
		b.generalRegisters(2).returnGeneral()
		b.Emit(isa.OpLoadGeneralConstant, 1, 0, 0)
		b.Emit(isa.OpMoveGeneral, 0, 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "from general")
	})
}

func TestOpcodeHandlersBoolOps(t *testing.T) {
	t.Parallel()

	t.Run("opLoadBoolConst_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(true)
		b.boolRegisters(1).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opLoadBoolConst_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(false)
		b.boolRegisters(1).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), false)
	})

	t.Run("opMoveBool", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(true)
		b.boolRegisters(2).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveBool), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opBoolToInt_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(true)
		b.boolRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opBoolToInt_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(false)
		b.boolRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConstant), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opIntToBool_nonzero", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToBool), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opIntToBool_zero", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(1).boolRegisters(1).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToBool), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), false)
	})
}

func TestOpcodeHandlersUintOps(t *testing.T) {
	t.Parallel()

	buildUintBinaryOp := func(op isa.Opcode, a, b uint64) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.uintConstants = append(bb.uintConstants, a)
		bb.uintConstants = append(bb.uintConstants, b)
		bb.uintRegisters(3).returnUint()
		bb.Emit(isa.OpLoadUint64Constant, 1, 0, 0)
		bb.Emit(isa.OpLoadUint64Constant, 2, 1, 0)
		bb.Emit(op, 0, 1, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	t.Run("opAddUint64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpAddUint64, 10, 32), uint64(42))
	})
	t.Run("opSubtractUint64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpSubtractUint64, 50, 8), uint64(42))
	})
	t.Run("opMultiplyUint64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpMultiplyUint64, 6, 7), uint64(42))
	})
	t.Run("opDivideUint64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpDivideUint64, 84, 2), uint64(42))
	})
	t.Run("opDivUint_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildUintBinaryOp(isa.OpDivideUint64, 1, 0))
	})
	t.Run("opRemainderUint64", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpRemainderUint64, 47, 5), uint64(2))
	})
	t.Run("opRemUint_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildUintBinaryOp(isa.OpRemainderUint64, 1, 0))
	})

	t.Run("opConvertInt64ToUint64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).uintRegisters(1).returnUint()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToUint64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), uint64(42))
	})

	t.Run("opConvertUint64ToInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.uintConstants = append(b.uintConstants, 42)
		b.uintRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadUint64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opConvertUint64ToFloat64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.uintConstants = append(b.uintConstants, uint64(42))
		b.uintRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadUint64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToFloat64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(42.0))
	})

	t.Run("opConvertFloat64ToUint64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.9)
		b.floatRegisters(1).uintRegisters(1).returnUint()
		b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpConvertFloat64ToUint64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), uint64(42))
	})
}

func TestOpcodeHandlersComplexOps(t *testing.T) {
	t.Parallel()

	buildComplexBinaryOp := func(op isa.Opcode, a, b complex128) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.complexConstants = append(bb.complexConstants, a)
		bb.complexConstants = append(bb.complexConstants, b)
		bb.numRegisters[isa.RegisterComplex128] = 3
		bb.resultKinds = []isa.RegisterKind{isa.RegisterComplex128}
		bb.Emit(isa.OpLoadComplex128Constant, 1, 0, 0)
		bb.Emit(isa.OpLoadComplex128Constant, 2, 1, 0)
		bb.Emit(op, 0, 1, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	t.Run("opAddComplex128", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpAddComplex128, 1+2i, 3+4i), 4+6i)
	})
	t.Run("opSubtractComplex128", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpSubtractComplex128, 5+7i, 1+2i), 4+5i)
	})
	t.Run("opMultiplyComplex128", func(t *testing.T) {
		t.Parallel()

		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpMultiplyComplex128, 2+3i, 4+5i), -7+22i)
	})
	t.Run("opNegateComplex128", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3+4i)
		b.numRegisters[isa.RegisterComplex128] = 2
		b.resultKinds = []isa.RegisterKind{isa.RegisterComplex128}
		b.Emit(isa.OpLoadComplex128Constant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegateComplex128), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -3-4i)
	})

	t.Run("opRealPartComplex128", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3.5+4.5i)
		b.numRegisters[isa.RegisterComplex128] = 1
		b.floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadComplex128Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpRealPartComplex128), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.5))
	})

	t.Run("opImaginaryPartComplex128", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3.5+4.5i)
		b.numRegisters[isa.RegisterComplex128] = 1
		b.floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadComplex128Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpImaginaryPartComplex128), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(4.5))
	})

	t.Run("opMakeComplex128", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(3.0)
		b.addFloatConst(4.0)
		b.floatRegisters(2)
		b.numRegisters[isa.RegisterComplex128] = 1
		b.resultKinds = []isa.RegisterKind{isa.RegisterComplex128}
		b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
		b.Emit(isa.OpLoadFloat64Constant, 1, 1, 0)
		b.Emit(isa.OpMakeComplex128, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 3+4i)
	})
}

func TestOpcodeHandlersCrossBankMoves(t *testing.T) {
	t.Parallel()

	t.Run("opBoxInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoxInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opBoxFloat64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(3.14)
		b.floatRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadFloat64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoxFloat64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.14))
	})

	t.Run("opBoxString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoxString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opUnboxInt64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(42))
		b.generalRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadGeneralConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpUnboxInt64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opUnboxFloat64", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(float64(3.14)))
		b.generalRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadGeneralConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpUnboxFloat64), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.14))
	})

	t.Run("opUnboxString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf("hello"))
		b.generalRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadGeneralConstant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpUnboxString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})
}

func TestOpcodeHandlersCoverMathIntrinsics(t *testing.T) {
	t.Parallel()

	buildMathBinary := func(op isa.Opcode, a, b float64) *program.CompiledFunction {
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

	buildUmbrellaUnary := func(sub isa.SubOpcode, input float64) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.addFloatConst(input)
		bb.floatRegisters(2).returnFloat()
		bb.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(sub), 0, 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	buildUmbrellaBinary := func(sub isa.SubOpcode, a, b float64) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.addFloatConst(a)
		bb.addFloatConst(b)
		bb.floatRegisters(3).returnFloat()
		bb.Emit(isa.OpLoadFloat64Constant, 1, 0, 0)
		bb.Emit(isa.OpLoadFloat64Constant, 2, 1, 0)
		bb.Emit(isa.OpDrillTier1, uint8(sub), 0, 1)
		bb.Emit(isa.OpExtension, 2, 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	t.Run("opMathPow", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildMathBinary(isa.OpMathPow, 2.0, 10.0), float64(1024.0))
	})
	t.Run("opMathExp", func(t *testing.T) {
		t.Parallel()
		result, err := execSynthetic(t, buildUmbrellaUnary(isa.SubOpMathExp, 0.0))
		require.NoError(t, err)
		require.InDelta(t, 1.0, result.(float64), 1e-10)
	})
	t.Run("opMathSin", func(t *testing.T) {
		t.Parallel()
		result, err := execSynthetic(t, buildUmbrellaUnary(isa.SubOpMathSin, math.Pi/2))
		require.NoError(t, err)
		require.InDelta(t, 1.0, result.(float64), 1e-10)
	})
	t.Run("opMathCos", func(t *testing.T) {
		t.Parallel()
		result, err := execSynthetic(t, buildUmbrellaUnary(isa.SubOpMathCos, 0.0))
		require.NoError(t, err)
		require.InDelta(t, 1.0, result.(float64), 1e-10)
	})
	t.Run("opMathTan", func(t *testing.T) {
		t.Parallel()
		result, err := execSynthetic(t, buildUmbrellaUnary(isa.SubOpMathTan, math.Pi/4))
		require.NoError(t, err)
		require.InDelta(t, 1.0, result.(float64), 1e-10)
	})
	t.Run("opMathMod", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUmbrellaBinary(isa.SubOpMathMod, 7.5, 3.0), float64(1.5))
	})
}

func TestOpcodeHandlersStringIntrinsics(t *testing.T) {
	t.Parallel()

	t.Run("opStringsToUpper", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStringsToUpper), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "HELLO")
	})

	t.Run("opStringsToLower", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("HELLO")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStringsToLower), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStringsTrimSpace", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("  hello  ")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStringsTrimSpace), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStrContains_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsContains, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStrContains_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("xyz")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsContains, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), false)
	})

	t.Run("opStrHasPrefix_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("hello")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsHasPrefix, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStrHasSuffix_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsHasSuffix, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStringsIndex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsIndex, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 6)
	})

	t.Run("opStrIndex_not_found", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsIndex, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -1)
	})

	t.Run("opStringsCount", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("banana")
		b.addStringConst("an")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConstant, 0, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 1, 1, 0)
		b.Emit(isa.OpStringsCount, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 2)
	})

	t.Run("opStringsRepeat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("ab")
		b.addIntConst(3)
		b.stringRegisters(2).intRegisters(1).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpStringsRepeat, 0, 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "ababab")
	})

	t.Run("opStringsTrimPrefix", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("hello ")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 2, 1, 0)
		b.Emit(isa.OpStringsTrimPrefix, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "world")
	})

	t.Run("opStringsTrimSuffix", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst(" world")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConstant, 1, 0, 0)
		b.Emit(isa.OpLoadStringConstant, 2, 1, 0)
		b.Emit(isa.OpStringsTrimSuffix, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStrconvItoa", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadInt64Constant, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvItoa), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "42")
	})
}

func (b *bytecodeBuilder) returnUint() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterUint64}
	return b
}
