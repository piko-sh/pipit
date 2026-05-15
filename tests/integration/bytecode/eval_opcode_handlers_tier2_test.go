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

	t.Run("opLoadStringConst", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).returnString()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opMoveString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("world")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveString), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "world")
	})

	t.Run("opConcatString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.addStringConst(" world")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpLoadStringConst, 2, 1, 0)
		b.Emit(isa.OpConcatString, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello world")
	})

	t.Run("opConcatString_empty", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("test")
		b.addStringConst("")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpLoadStringConst, 2, 1, 0)
		b.Emit(isa.OpConcatString, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "test")
	})

	t.Run("opLenString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 5)
	})

	t.Run("opLenString_empty", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("")
		b.stringRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLenString), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opEqString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("abc")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpEqString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opEqString_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpEqString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opNeString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpNeString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opLtString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("abc")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpLtString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opGtString_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("xyz")
		b.addStringConst("abc")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpGtString, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opRuneToString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(65)
		b.intRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpRuneToString), 0, 0)
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

	t.Run("opLoadGeneralConst", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(42))
		b.generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadGeneralConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opMoveGeneral", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf("from general"))
		b.generalRegisters(2).returnGeneral()
		b.Emit(isa.OpLoadGeneralConst, 1, 0, 0)
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
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opLoadBoolConst_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(false)
		b.boolRegisters(1).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), false)
	})

	t.Run("opMoveBool", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(true)
		b.boolRegisters(2).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveBool), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opBoolToInt_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(true)
		b.boolRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoolToInt), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 1)
	})

	t.Run("opBoolToInt_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addBoolConst(false)
		b.boolRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadBoolConst), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpBoolToInt), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 0)
	})

	t.Run("opIntToBool_nonzero", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToBool), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opIntToBool_zero", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.intRegisters(1).boolRegisters(1).returnBool()
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToBool), 0, 0)
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
		bb.Emit(isa.OpLoadUintConst, 1, 0, 0)
		bb.Emit(isa.OpLoadUintConst, 2, 1, 0)
		bb.Emit(op, 0, 1, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	t.Run("opAddUint", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpAddUint, 10, 32), uint64(42))
	})
	t.Run("opSubUint", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpSubUint, 50, 8), uint64(42))
	})
	t.Run("opMulUint", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpMulUint, 6, 7), uint64(42))
	})
	t.Run("opDivUint", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpDivUint, 84, 2), uint64(42))
	})
	t.Run("opDivUint_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildUintBinaryOp(isa.OpDivUint, 1, 0))
	})
	t.Run("opRemUint", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildUintBinaryOp(isa.OpRemUint, 47, 5), uint64(2))
	})
	t.Run("opRemUint_zero", func(t *testing.T) {
		t.Parallel()
		requireSyntheticError(t, buildUintBinaryOp(isa.OpRemUint, 1, 0))
	})

	t.Run("opIntToUint", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).uintRegisters(1).returnUint()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpIntToUint), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), uint64(42))
	})

	t.Run("opUintToInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.uintConstants = append(b.uintConstants, 42)
		b.uintRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadUintConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpUintToInt), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opUintToFloat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.uintConstants = append(b.uintConstants, uint64(42))
		b.uintRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadUintConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpUintToFloat), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(42.0))
	})

	t.Run("opFloatToUint", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(42.9)
		b.floatRegisters(1).uintRegisters(1).returnUint()
		b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpFloatToUint), 0, 0)
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
		bb.numRegisters[isa.RegisterComplex] = 3
		bb.resultKinds = []isa.RegisterKind{isa.RegisterComplex}
		bb.Emit(isa.OpLoadComplexConst, 1, 0, 0)
		bb.Emit(isa.OpLoadComplexConst, 2, 1, 0)
		bb.Emit(op, 0, 1, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	t.Run("opAddComplex", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpAddComplex, 1+2i, 3+4i), 4+6i)
	})
	t.Run("opSubComplex", func(t *testing.T) {
		t.Parallel()
		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpSubComplex, 5+7i, 1+2i), 4+5i)
	})
	t.Run("opMulComplex", func(t *testing.T) {
		t.Parallel()

		requireSyntheticResult(t, buildComplexBinaryOp(isa.OpMulComplex, 2+3i, 4+5i), -7+22i)
	})
	t.Run("opNegComplex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3+4i)
		b.numRegisters[isa.RegisterComplex] = 2
		b.resultKinds = []isa.RegisterKind{isa.RegisterComplex}
		b.Emit(isa.OpLoadComplexConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpNegComplex), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -3-4i)
	})

	t.Run("opRealComplex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3.5+4.5i)
		b.numRegisters[isa.RegisterComplex] = 1
		b.floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadComplexConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpRealComplex), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.5))
	})

	t.Run("opImagComplex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.complexConstants = append(b.complexConstants, 3.5+4.5i)
		b.numRegisters[isa.RegisterComplex] = 1
		b.floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadComplexConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpImagComplex), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(4.5))
	})

	t.Run("opBuildComplex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(3.0)
		b.addFloatConst(4.0)
		b.floatRegisters(2)
		b.numRegisters[isa.RegisterComplex] = 1
		b.resultKinds = []isa.RegisterKind{isa.RegisterComplex}
		b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
		b.Emit(isa.OpLoadFloatConst, 1, 1, 0)
		b.Emit(isa.OpBuildComplex, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 3+4i)
	})
}

func TestOpcodeHandlersCrossBankMoves(t *testing.T) {
	t.Parallel()

	t.Run("opMoveIntToGeneral", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveIntToGeneral), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opMoveFloatToGeneral", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addFloatConst(3.14)
		b.floatRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveFloatToGeneral), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.14))
	})

	t.Run("opMoveStringToGeneral", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(1).generalRegisters(1).returnGeneral()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveStringToGeneral), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opMoveGeneralToInt", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(42))
		b.generalRegisters(1).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadGeneralConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToInt), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 42)
	})

	t.Run("opMoveGeneralToFloat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf(float64(3.14)))
		b.generalRegisters(1).floatRegisters(1).returnFloat()
		b.Emit(isa.OpLoadGeneralConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToFloat), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), float64(3.14))
	})

	t.Run("opMoveGeneralToString", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addGeneralConst(reflect.ValueOf("hello"))
		b.generalRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadGeneralConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveGeneralToString), 0, 0)
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
		bb.Emit(isa.OpLoadFloatConst, 1, 0, 0)
		bb.Emit(isa.OpLoadFloatConst, 2, 1, 0)
		bb.Emit(op, 0, 1, 2)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	buildUmbrellaUnary := func(sub isa.SubOpcode, input float64) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.addFloatConst(input)
		bb.floatRegisters(2).returnFloat()
		bb.Emit(isa.OpLoadFloatConst, 1, 0, 0)
		bb.Emit(isa.OpDrillTier1, uint8(sub), 0, 1)
		bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		return bb.build()
	}

	buildUmbrellaBinary := func(sub isa.SubOpcode, a, b float64) *program.CompiledFunction {
		bb := newBytecodeBuilder()
		bb.addFloatConst(a)
		bb.addFloatConst(b)
		bb.floatRegisters(3).returnFloat()
		bb.Emit(isa.OpLoadFloatConst, 1, 0, 0)
		bb.Emit(isa.OpLoadFloatConst, 2, 1, 0)
		bb.Emit(isa.OpDrillTier1, uint8(sub), 0, 1)
		bb.Emit(isa.OpExt, 2, 0, 0)
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

	t.Run("opStrToUpper", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrToUpper), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "HELLO")
	})

	t.Run("opStrToLower", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("HELLO")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrToLower), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStrTrimSpace", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("  hello  ")
		b.stringRegisters(2).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrTrimSpace), 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStrContains_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrContains, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStrContains_false", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("xyz")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrContains, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), false)
	})

	t.Run("opStrHasPrefix_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("hello")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrHasPrefix, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStrHasSuffix_true", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).boolRegisters(1).returnBool()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrHasSuffix, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), true)
	})

	t.Run("opStrIndex", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("world")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrIndex, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 6)
	})

	t.Run("opStrIndex_not_found", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello")
		b.addStringConst("xyz")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrIndex, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), -1)
	})

	t.Run("opStrCount", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("banana")
		b.addStringConst("an")
		b.stringRegisters(2).intRegisters(1).returnInt()
		b.Emit(isa.OpLoadStringConst, 0, 0, 0)
		b.Emit(isa.OpLoadStringConst, 1, 1, 0)
		b.Emit(isa.OpStrCount, 0, 0, 1)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), 2)
	})

	t.Run("opStrRepeat", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("ab")
		b.addIntConst(3)
		b.stringRegisters(2).intRegisters(1).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpStrRepeat, 0, 1, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "ababab")
	})

	t.Run("opStrTrimPrefix", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst("hello ")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpLoadStringConst, 2, 1, 0)
		b.Emit(isa.OpStrTrimPrefix, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "world")
	})

	t.Run("opStrTrimSuffix", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addStringConst("hello world")
		b.addStringConst(" world")
		b.stringRegisters(3).returnString()
		b.Emit(isa.OpLoadStringConst, 1, 0, 0)
		b.Emit(isa.OpLoadStringConst, 2, 1, 0)
		b.Emit(isa.OpStrTrimSuffix, 0, 1, 2)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "hello")
	})

	t.Run("opStrconvItoa", func(t *testing.T) {
		t.Parallel()
		b := newBytecodeBuilder()
		b.addIntConst(42)
		b.intRegisters(1).stringRegisters(1).returnString()
		b.Emit(isa.OpLoadIntConst, 0, 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpStrconvItoa), 0, 0)
		b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
		requireSyntheticResult(t, b.build(), "42")
	})
}

func (b *bytecodeBuilder) returnUint() *bytecodeBuilder {
	b.resultKinds = []isa.RegisterKind{isa.RegisterUint}
	return b
}
