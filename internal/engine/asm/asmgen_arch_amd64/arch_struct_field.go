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

package asmgen_arch_amd64

import (
	"fmt"
	"strings"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmamd64"
	core "piko.sh/asmgen/asmgen_arch_amd64"
)

// eqUintConstJumpFalseSkipWords is the number of words after the primary word of
// isa.SubOpEqUintConstJumpFalse (the extension word plus the trailing no-ops), as an
// immediate for the program-counter adjustment.
const eqUintConstJumpFalseSkipWords = "2"

// EmitTypedRangeNextByte implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedRangeNextByte(e *asmgen.Emitter, contextOffset string) {
	emitTypedRangeNextByteOperands(e)
	emitTypedRangeNextByteLoadAndCompare(e, contextOffset)
	emitTypedRangeNextByteBodyAndEpilogue(e)
}

// EmitRangeCheckUintJumpFalse implements BytecodeArchPort. It emits the fused range-check
// super-instruction that jumps by a signed 16-bit offset when uints[valueReg] falls
// outside [loConst, hiConst].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitRangeCheckUintJumpFalse(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")

	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), AX")

	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")

	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")

	inst(e, asmamd64.OperationShiftRight64Bits, "$16, DX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "DL, DI")

	inst(e, asmamd64.OperationCompare64Bits, "AX, CX")
	inst(e, asmamd64.OperationJumpIfBelow, "rangeCheckUintTakeJump")

	inst(e, asmamd64.OperationCompare64Bits, "AX, DI")
	inst(e, asmamd64.OperationJumpIfAbove, "rangeCheckUintTakeJump")

	inst(e, asmamd64.OperationAdd64Bits, "$6, R14")
	inst(e, asmamd64.OperationJump, "rangeCheckUintDispatch")

	e.Label("rangeCheckUintTakeJump")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, AX")
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, "AX, AX")
	inst(e, asmamd64.OperationAdd64Bits, "$5, R14")
	inst(e, asmamd64.OperationAdd64Bits, "AX, R14")
	emitBackEdgePoll(e)

	e.Label("rangeCheckUintDispatch")
	e.Instruction(macroDispatchNext)
}

// EmitEqUintConstJumpFalse implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitEqUintConstJumpFalse(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, DX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "DL, CX")

	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, CX")
	inst(e, asmamd64.OperationJumpIfNotEqual, "eqUintConstTakeJump")

	inst(e, asmamd64.OperationAdd64Bits, "$"+eqUintConstJumpFalseSkipWords+", R14")
	inst(e, asmamd64.OperationJump, "eqUintConstDispatch")

	e.Label("eqUintConstTakeJump")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationAdd64Bits, "$"+eqUintConstJumpFalseSkipWords+", R14")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, AX")
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, "AX, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, R14")
	emitBackEdgePoll(e)

	e.Label("eqUintConstDispatch")
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceStringGet implements BytecodeArchPort. It emits a bounds-checked load for
// strings[B] = slicesString[C][ints[ext.A]].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesString bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceStringGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueAMD64(e, contextOffset)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "0(DI)(BX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(DI)(BX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(SI)(AX*1)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 8(SI)(AX*1)")
	emitTypedSliceTailAMD64(e)
}

// EmitComplexCopy implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
func (*BytecodeAMD64Arch) EmitComplexCopy(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI)(BX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(SI)(AX*1)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 8(SI)(AX*1)")
	e.Instruction(macroDispatchNext)
}

// EmitComplexNegate implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
func (*BytecodeAMD64Arch) EmitComplexNegate(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI)(BX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "$0x8000000000000000, DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DI, CX")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DI, DX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(SI)(AX*1)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 8(SI)(AX*1)")
	e.Instruction(macroDispatchNext)
}

// LoadComplexHalfToFloatBank implements BytecodeArchPort. It loads one float64 half of a
// complex128 element into the float register bank.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
// Takes indexRegister (string) which holds the source complex slot index.
// Takes halfOffset (string) which is "0" or "8".
// Takes destinationFloatIndexRegister (string) which holds the destination index in the
// float register bank.
func (*BytecodeAMD64Arch) LoadComplexHalfToFloatBank(e *asmgen.Emitter, contextOffset, indexRegister, halfOffset, destinationFloatIndexRegister string) {
	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+indexRegister)
	inst(e, asmamd64.OperationAdd64Bits, indexRegister+", SI")
	inst(e, asmamd64.OperationMoveScalarDouble, halfOffset+"(SI), X0")
	inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationFloatIndexRegister+"*8)")
}

// LoadTypedSliceHeaderLength implements BytecodeArchPort. It reads the length field of a
// 24-byte typed-slice header from the dispatch context.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
// Takes indexRegister (string) which holds the slot index.
// Takes destinationRegister (string) which receives the length.
func (*BytecodeAMD64Arch) LoadTypedSliceHeaderLength(e *asmgen.Emitter, contextOffset, indexRegister, destinationRegister string) {
	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, "+indexRegister)
	inst(e, asmamd64.OperationAdd64Bits, indexRegister+", SI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), "+destinationRegister)
}

// LoadFromBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes bank (asmgen.RegisterBank) which selects the register bank.
// Takes indexRegister (string) which holds the register index.
// Takes destinationRegister (string) which receives the loaded value.
func (*BytecodeAMD64Arch) LoadFromBank(e *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string) {
	base, mnemonic := bankAccess(bank)
	inst(e, mnemonic, "("+base+")("+indexRegister+"*8), "+destinationRegister)
}

// StoreToBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes bank (asmgen.RegisterBank) which selects the register bank.
// Takes sourceRegister (string) which holds the value to store.
// Takes indexRegister (string) which holds the destination index.
func (*BytecodeAMD64Arch) StoreToBank(e *asmgen.Emitter, bank asmgen.RegisterBank, sourceRegister, indexRegister string) {
	base, mnemonic := bankAccess(bank)
	inst(e, mnemonic, sourceRegister+", ("+base+")("+indexRegister+"*8)")
}

// LoadConstant implements BytecodeArchPort.
//
// The generator names the banks needing a different base register or mnemonic and puts
// the integer-bank form in the default, so a new bank emits the common sequence rather
// than nothing. Any mistake here changes emitted assembly, which make
// generate-asmgen-validate compares byte for byte.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes bank (asmgen.RegisterBank) which selects the constant pool bank.
// Takes indexRegister (string) which holds the constant index.
// Takes destinationRegister (string) which receives the loaded constant.
func (*BytecodeAMD64Arch) LoadConstant(e *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string) {
	switch bank {
	case asmgen.RegisterBankInteger:
		inst(e, asmamd64.OperationMove64Bits, "(R11)("+indexRegister+"*8), "+destinationRegister)
	case asmgen.RegisterBankFloat:
		inst(e, asmamd64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R15), "+destinationRegister)
	default:
	}
}

// LoadFloatConstantToBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationIndex (string) which is the float bank destination index.
// Takes constantIndex (string) which is the float constant pool index.
func (*BytecodeAMD64Arch) LoadFloatConstantToBank(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMoveScalarDouble, "(SI)("+constantIndex+"*8), X0")
	inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
}

// LoadContextField implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes offset (string) which is the byte offset into the context.
// Takes destinationRegister (string) which receives the loaded value.
func (*BytecodeAMD64Arch) LoadContextField(e *asmgen.Emitter, offset, destinationRegister string) {
	inst(e, asmamd64.OperationMove64Bits, offset+"(R15), "+destinationRegister)
}

// StoreContextField implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes sourceRegister (string) which holds the value to store.
// Takes offset (string) which is the byte offset into the context.
func (*BytecodeAMD64Arch) StoreContextField(e *asmgen.Emitter, sourceRegister, offset string) {
	inst(e, asmamd64.OperationMove64Bits, sourceRegister+", "+offset+"(R15)")
}

// StoreContextImmediate implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes value (string) which is the immediate value to store.
// Takes offset (string) which is the byte offset into the context.
func (*BytecodeAMD64Arch) StoreContextImmediate(e *asmgen.Emitter, value, offset string) {
	inst(e, asmamd64.OperationMove64Bits, value+", "+offset+"(R15)")
}

// IntegerBinaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes leftSourceIndex (string) which is the left operand register index.
// Takes rightSourceIndex (string) which is the right operand register index.
func (*BytecodeAMD64Arch) IntegerBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	if operation == "ANDNOT" {
		inst(e, asmamd64.OperationMove64Bits, "(R8)("+rightSourceIndex+"*8), SI")
		inst(e, asmamd64.OperationBitwiseNot64Bits, "SI")
		inst(e, asmamd64.OperationBitwiseAnd64Bits, "(R8)("+leftSourceIndex+"*8), SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
		return
	}
	mnemonic := intOpMnemonic(operation)
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+leftSourceIndex+"*8), SI")
	inst(e, mnemonic, "(R8)("+rightSourceIndex+"*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// IntegerBinaryOperationConstant implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes sourceIndex (string) which is the source register index.
// Takes constantIndex (string) which is the constant pool index.
func (*BytecodeAMD64Arch) IntegerBinaryOperationConstant(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex, constantIndex string) {
	mnemonic := intOpMnemonic(operation)
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+sourceIndex+"*8), SI")
	inst(e, mnemonic, "(R11)("+constantIndex+"*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// UintBinaryOperation implements BytecodeArchPort.
//
// Bit-pattern-identical to IntegerBinaryOperation, but addresses the uint register bank
// via CTX_UINTS_BASE loaded into DI rather than the preserved R8 used for the int bank.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes leftSourceIndex (string) which is the left operand register index.
// Takes rightSourceIndex (string) which is the right operand register index.
func (*BytecodeAMD64Arch) UintBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	if operation == "ANDNOT" {
		inst(e, asmamd64.OperationMove64Bits, "(DI)("+rightSourceIndex+"*8), SI")
		inst(e, asmamd64.OperationBitwiseNot64Bits, "SI")
		inst(e, asmamd64.OperationBitwiseAnd64Bits, "(DI)("+leftSourceIndex+"*8), SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (DI)("+destinationIndex+"*8)")
		return
	}
	mnemonic := intOpMnemonic(operation)
	inst(e, asmamd64.OperationMove64Bits, "(DI)("+leftSourceIndex+"*8), SI")
	inst(e, mnemonic, "(DI)("+rightSourceIndex+"*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DI)("+destinationIndex+"*8)")
}

// UintShift implements BytecodeArchPort.
//
// Right shifts use SHRQ (logical) rather than SARQ (arithmetic) because uint64 has no
// sign bit to preserve. Left shifts use SHLQ identical to the int variant. Value and
// amount both live in the uint bank (matches isa.OpShiftLeftUint / isa.OpShiftRightUint
// semantics).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes direction (string) which is the shift direction (LEFT or RIGHT).
// Takes destinationIndex (string) which is the result register index.
// Takes valueIndex (string) which is the value register index.
// Takes amountIndex (string) which is the shift amount register index.
func (*BytecodeAMD64Arch) UintShift(e *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI)("+amountIndex+"*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "(DI)("+valueIndex+"*8), SI")
	switch direction {
	case "LEFT":
		inst(e, asmamd64.OperationShiftLeft64Bits, "CL, SI")
	case "RIGHT":
		inst(e, asmamd64.OperationShiftRight64Bits, "CL, SI")
	}
	inst(e, asmamd64.OperationBitwiseXor64Bits, "BX, BX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $64")
	inst(e, asmamd64.OperationConditionalMove64BitsIfCarryClear, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DI)("+destinationIndex+"*8)")
}

// UintCompareAndSet implements BytecodeArchPort.
//
// Maps the condition names (EQ/NE/LT/LE/GT/GE) to unsigned condition codes for the
// inequality cases. The compare reads from the uint bank; the boolean result is written
// into the int bank (booleans are stored as int64).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the comparison condition code.
// Takes destinationIndex (string) which is the result register index.
// Takes leftIndex (string) which is the left operand register index.
// Takes rightIndex (string) which is the right operand register index.
func (*BytecodeAMD64Arch) UintCompareAndSet(e *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI)("+leftIndex+"*8), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, (DI)("+rightIndex+"*8)")
	inst(e, asmamd64.OperationMove64Bits, "$0, SI")
	var setCond string
	switch condition {
	case "EQ":
		setCond = asmamd64.OperationSetIfEqual
	case "NE":
		setCond = asmamd64.OperationSetIfNotEqual
	case "LT":
		setCond = asmamd64.OperationSetIfCarrySet
	case "LE":
		setCond = asmamd64.OperationSetIfLowerOrSame
	case "GT":
		setCond = asmamd64.OperationSetIfHigher
	case "GE":
		setCond = asmamd64.OperationSetIfCarryClear
	}
	inst(e, setCond, "SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// IntegerUnaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the unary operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeAMD64Arch) IntegerUnaryOperation(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+sourceIndex+"*8), SI")
	switch operation {
	case "NEG":
		inst(e, asmamd64.OperationNegate64Bits, "SI")
	case "NOT":
		inst(e, asmamd64.OperationBitwiseNot64Bits, "SI")
	}
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// IntegerInPlace implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the in-place operation name.
// Takes indexRegister (string) which is the register index to modify.
func (*BytecodeAMD64Arch) IntegerInPlace(e *asmgen.Emitter, operation string, indexRegister string) {
	switch operation {
	case "INC":
		inst(e, asmamd64.OperationIncrement64Bits, "(R8)("+indexRegister+"*8)")
	case "DEC":
		inst(e, asmamd64.OperationDecrement64Bits, "(R8)("+indexRegister+"*8)")
	}
}

// UintInPlace implements BytecodeArchPort.
//
// Loads CTX_UINTS_BASE into baseScratch, then INCQ/DECQ the indexed uint64 in memory
// (single read-modify-write per the amd64 INC/DEC mem-operand encoding). The uint bank
// base isn't pinned (unlike R8 for int), so the load is required.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
// Takes operation (string) which is INC or DEC.
// Takes indexRegister (string) which holds the uint register index.
// Takes baseScratch (string) which receives the loaded uint base.
func (*BytecodeAMD64Arch) UintInPlace(e *asmgen.Emitter, operation string, indexRegister string, baseScratch string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), "+baseScratch)
	switch operation {
	case "INC":
		inst(e, asmamd64.OperationIncrement64Bits, "("+baseScratch+")("+indexRegister+"*8)")
	case "DEC":
		inst(e, asmamd64.OperationDecrement64Bits, "("+baseScratch+")("+indexRegister+"*8)")
	}
}

// IntegerDivide implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dividendIndex (string) which is the dividend register index.
// Takes divisorIndex (string) which is the divisor register index.
// Takes quotientDestinationIndex (string) which is the quotient destination index.
// Takes remainderDestinationIndex (string) which is the remainder destination index.
// Takes zeroLabel (string) which is the label to jump to on division by zero.
func (*BytecodeAMD64Arch) IntegerDivide(e *asmgen.Emitter, dividendIndex, divisorIndex, quotientDestinationIndex, remainderDestinationIndex, zeroLabel string) {
	destIndex := quotientDestinationIndex
	if destIndex == "" {
		destIndex = remainderDestinationIndex
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, SI")
	inst(e, asmamd64.OperationMove64Bits, destIndex+", DI")
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+divisorIndex+"*8), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, zeroLabel)
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+dividendIndex+"*8), AX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $-1")
	inst(e, asmamd64.OperationJumpIfNotEqual, "div_no_overflow")
	if quotientDestinationIndex != "" {
		inst(e, asmamd64.OperationNegate64Bits, "AX")
		inst(e, asmamd64.OperationMove64Bits, "AX, (R8)(DI*8)")
	}
	if remainderDestinationIndex != "" {
		inst(e, asmamd64.OperationMove64Bits, "$0, (R8)(DI*8)")
	}
	inst(e, asmamd64.OperationJump, "div_overflow_done")
	e.Label("div_no_overflow")
	inst(e, asmamd64.OperationConvertQuadToOctword, "")
	inst(e, asmamd64.OperationSignedDivide64Bits, "CX")
	if quotientDestinationIndex != "" {
		inst(e, asmamd64.OperationMove64Bits, "AX, (R8)(DI*8)")
	}
	if remainderDestinationIndex != "" {
		inst(e, asmamd64.OperationMove64Bits, "DX, (R8)(DI*8)")
	}
	e.Label("div_overflow_done")
	inst(e, asmamd64.OperationMove64Bits, "SI, DX")
}

// IntegerShift implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes direction (string) which is the shift direction (LEFT or RIGHT).
// Takes destinationIndex (string) which is the result register index.
// Takes valueIndex (string) which is the value register index.
// Takes amountIndex (string) which is the shift amount register index.
func (*BytecodeAMD64Arch) IntegerShift(e *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+amountIndex+"*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+valueIndex+"*8), SI")
	switch direction {
	case "LEFT":
		inst(e, asmamd64.OperationShiftLeft64Bits, "CL, SI")
		inst(e, asmamd64.OperationBitwiseXor64Bits, "BX, BX")
		inst(e, asmamd64.OperationCompare64Bits, "CX, $64")
		inst(e, asmamd64.OperationConditionalMove64BitsIfCarryClear, "BX, SI")
	case "RIGHT":
		inst(e, asmamd64.OperationMove64Bits, "SI, BX")
		inst(e, asmamd64.OperationShiftRightArithmetic64Bits, "$63, BX")
		inst(e, asmamd64.OperationShiftRightArithmetic64Bits, "CL, SI")
		inst(e, asmamd64.OperationCompare64Bits, "CX, $64")
		inst(e, asmamd64.OperationConditionalMove64BitsIfCarryClear, "BX, SI")
	}
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// IntegerCompareAndSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the comparison condition code.
// Takes destinationIndex (string) which is the result register index.
// Takes leftIndex (string) which is the left operand register index.
// Takes rightIndex (string) which is the right operand register index.
func (*BytecodeAMD64Arch) IntegerCompareAndSet(e *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+leftIndex+"*8), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, (R8)("+rightIndex+"*8)")
	inst(e, asmamd64.OperationMove64Bits, "$0, SI")
	setCond := "SET" + condition
	inst(e, setCond, "SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
}

// IntegerCompareAndBranch implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the comparison condition code.
// Takes leftIndex (string) which is the left operand register index.
// Takes rightIndex (string) which is the right operand register index.
// Takes label (string) which is the branch target label.
func (*BytecodeAMD64Arch) IntegerCompareAndBranch(e *asmgen.Emitter, condition string, leftIndex, rightIndex, label string) {
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+leftIndex+"*8), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, (R8)("+rightIndex+"*8)")
	jmpCond := "J" + condition
	inst(e, jmpCond, label)
}

// IntegerCompareConstantAndBranch implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the comparison condition code.
// Takes registerIndex (string) which is the register index to compare.
// Takes constantIndex (string) which is the constant pool index.
// Takes label (string) which is the branch target label.
func (*BytecodeAMD64Arch) IntegerCompareConstantAndBranch(e *asmgen.Emitter, condition string, registerIndex, constantIndex, label string) {
	inst(e, asmamd64.OperationMove64Bits, "(R8)("+registerIndex+"*8), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, (R11)("+constantIndex+"*8)")
	jmpCond := "J" + condition
	inst(e, jmpCond, label)
}

// StringLengthRead implements BytecodeArchPort. It reads the length of
// strings[sourceIndex] into ints[destinationIndex].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationIndex (string) which is the int-bank destination register.
// Takes sourceIndex (string) which is the string-bank source register; it is
// destructively shifted by 4 during the emit.
func (*BytecodeAMD64Arch) StringLengthRead(e *asmgen.Emitter, destinationIndex, sourceIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+sourceIndex)
	inst(e, asmamd64.OperationMove64Bits, "8(SI)("+sourceIndex+"*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)("+destinationIndex+"*8)")
}

// StringCopy implements BytecodeArchPort. It copies the 16-byte string header from
// strings[sourceIndex] to strings[destinationIndex].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationIndex (string) which is the string-bank destination register;
// destructively shifted by 4 during the emit.
// Takes sourceIndex (string) which is the string-bank source register; destructively
// shifted by 4 during the emit.
func (*BytecodeAMD64Arch) StringCopy(e *asmgen.Emitter, destinationIndex, sourceIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+destinationIndex)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+sourceIndex)
	inst(e, asmamd64.OperationMove64Bits, "(SI)("+sourceIndex+"*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)("+sourceIndex+"*1), DI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)("+destinationIndex+"*1)")
	inst(e, asmamd64.OperationMove64Bits, "DI, 8(SI)("+destinationIndex+"*1)")
}

// StringConstLoad implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationIndex (string) which is the strings-bank destination register;
// destructively shifted by 4 during the emit.
// Takes constantIndex (string) which is the constant pool index; destructively shifted by
// 4 during the emit.
func (*BytecodeAMD64Arch) StringConstLoad(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_STR_CONSTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), DI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+destinationIndex)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, "+constantIndex)
	inst(e, asmamd64.OperationMove64Bits, "(SI)("+constantIndex+"*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)("+constantIndex+"*1), "+constantIndex)
	inst(e, asmamd64.OperationMove64Bits, "CX, (DI)("+destinationIndex+"*1)")
	inst(e, asmamd64.OperationMove64Bits, constantIndex+", 8(DI)("+destinationIndex+"*1)")
}

// BoolConstLoad implements BytecodeArchPort.
//
// Copies a single byte from the bool constant table into the bools register bank. Both
// indices are 1-byte-strided so no scaling is required.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationIndex (string) which is the bools-bank destination register index
// (unscaled byte offset).
// Takes constantIndex (string) which is the constant pool index (unscaled byte offset).
func (*BytecodeAMD64Arch) BoolConstLoad(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOL_CONSTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove8Bits, "(SI)("+constantIndex+"*1), CL")
	inst(e, asmamd64.OperationMove8Bits, "CL, (DI)("+destinationIndex+"*1)")
}

// FloatBinaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes leftSourceIndex (string) which is the left operand register index.
// Takes rightSourceIndex (string) which is the right operand register index.
func (*BytecodeAMD64Arch) FloatBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	mnemonic := floatOpMnemonic(operation)
	inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+leftSourceIndex+"*8), X0")
	inst(e, mnemonic, "(R9)("+rightSourceIndex+"*8), X0")
	inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
}

// FloatUnaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which is the unary operation name.
// Takes destinationIndex (string) which is the result register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeAMD64Arch) FloatUnaryOperation(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex string) {
	switch operation {
	case "NEG":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0x8000000000000000, SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, X1")
		inst(e, asmamd64.OperationXorPackedDoubles, "X1, X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "SQRT":
		inst(e, asmamd64.OperationSquareRootScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "ABS":
		inst(e, asmamd64.OperationMove64Bits, "(R9)("+sourceIndex+"*8), SI")
		inst(e, asmamd64.OperationBitTestAndReset64Bits, "$63, SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R9)("+destinationIndex+"*8)")
	case "FLOOR":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationRoundScalarDouble, "$1, X0, X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "CEIL":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationRoundScalarDouble, "$2, X0, X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "TRUNC":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationRoundScalarDouble, "$3, X0, X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "ROUND":

		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0x3FE0000000000000, SI")
		inst(e, asmamd64.OperationMove64Bits, "$0x8000000000000000, CX")
		inst(e, asmamd64.OperationMove64Bits, "X0, DI")
		inst(e, asmamd64.OperationBitwiseAnd64Bits, "CX, DI")
		inst(e, asmamd64.OperationBitwiseOr64Bits, "DI, SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, X1")
		inst(e, asmamd64.OperationAddScalarDouble, "X1, X0")
		inst(e, asmamd64.OperationRoundScalarDouble, "$3, X0, X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	}
}

// FloatCompareAndSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the comparison condition code.
// Takes integerDestinationIndex (string) which is the integer bank destination index.
// Takes floatLeftIndex (string) which is the left float operand register index.
// Takes floatRightIndex (string) which is the right float operand register index.
func (*BytecodeAMD64Arch) FloatCompareAndSet(e *asmgen.Emitter, condition string, integerDestinationIndex, floatLeftIndex, floatRightIndex string) {
	switch condition {
	case "EQ":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfEqual, "SI")
		inst(e, asmamd64.OperationSetIfParityClear, "CL")
		inst(e, asmamd64.OperationBitwiseAnd8Bits, "CL, SIB")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	case "NE":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfNotEqual, "SI")
		inst(e, asmamd64.OperationSetIfParitySet, "CL")
		inst(e, asmamd64.OperationBitwiseOr8Bits, "CL, SIB")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	case "LT":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfHigher, "SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	case "LE":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfCarryClear, "SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	case "GT":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfHigher, "SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	case "GE":
		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+floatLeftIndex+"*8), X0")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "(R9)("+floatRightIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0, SI")
		inst(e, asmamd64.OperationSetIfCarryClear, "SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+integerDestinationIndex+"*8)")
	}
}

// FloatConversion implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes direction (string) which is the conversion direction.
// Takes destinationIndex (string) which is the result register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeAMD64Arch) FloatConversion(e *asmgen.Emitter, direction string, destinationIndex, sourceIndex string) {
	switch direction {
	case "INTEGER_TO_FLOAT":
		inst(e, asmamd64.OperationConvertSignedQuadToScalarDouble, "(R8)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "FLOAT_TO_INTEGER":
		inst(e, asmamd64.OperationConvertTruncatedScalarDoubleToSignedQuad, "(R9)("+sourceIndex+"*8), SI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (R8)("+destinationIndex+"*8)")
	case "UNSIGNED_TO_FLOAT":

		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove64Bits, "(DI)("+sourceIndex+"*8), SI")
		inst(e, asmamd64.OperationTest64Bits, "SI, SI")
		inst(e, asmamd64.OperationJumpIfSign, "unsigned_to_float_high_bit_"+destinationIndex)
		inst(e, asmamd64.OperationConvertSignedQuadToScalarDouble, "SI, X0")
		inst(e, asmamd64.OperationJump, "unsigned_to_float_done_"+destinationIndex)
		e.Label("unsigned_to_float_high_bit_" + destinationIndex)
		inst(e, asmamd64.OperationMove64Bits, "SI, CX")
		inst(e, asmamd64.OperationBitwiseAnd64Bits, "$1, CX")
		inst(e, asmamd64.OperationShiftRight64Bits, "$1, SI")
		inst(e, asmamd64.OperationBitwiseOr64Bits, "CX, SI")
		inst(e, asmamd64.OperationConvertSignedQuadToScalarDouble, "SI, X0")
		inst(e, asmamd64.OperationAddScalarDouble, "X0, X0")
		e.Label("unsigned_to_float_done_" + destinationIndex)
		inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)("+destinationIndex+"*8)")
	case "FLOAT_TO_UNSIGNED":

		inst(e, asmamd64.OperationMoveScalarDouble, "(R9)("+sourceIndex+"*8), X0")
		inst(e, asmamd64.OperationMove64Bits, "$0x43E0000000000000, CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, X1")
		inst(e, asmamd64.OperationCompareUnorderedScalarDouble, "X1, X0")
		inst(e, asmamd64.OperationJumpIfBelow, "float_to_unsigned_small_"+destinationIndex)
		inst(e, asmamd64.OperationSubtractScalarDouble, "X1, X0")
		inst(e, asmamd64.OperationConvertTruncatedScalarDoubleToSignedQuad, "X0, SI")
		inst(e, asmamd64.OperationMove64Bits, "$0x8000000000000000, CX")
		inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, SI")
		inst(e, asmamd64.OperationJump, "float_to_unsigned_done_"+destinationIndex)
		e.Label("float_to_unsigned_small_" + destinationIndex)
		inst(e, asmamd64.OperationConvertTruncatedScalarDoubleToSignedQuad, "X0, SI")
		e.Label("float_to_unsigned_done_" + destinationIndex)
		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove64Bits, "SI, (DI)("+destinationIndex+"*8)")
	}
}

// ExitWithReason implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes reason (string) which is the exit reason constant name.
func (*BytecodeAMD64Arch) ExitWithReason(e *asmgen.Emitter, reason string) {
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$"+reason+", CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// IncrementProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) IncrementProgramCounter(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
}

// DecrementProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) DecrementProgramCounter(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
}

// AddToProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes register (string) which holds the value to add to the program counter.
func (*BytecodeAMD64Arch) AddToProgramCounter(e *asmgen.Emitter, register string) {
	inst(e, asmamd64.OperationAdd64Bits, register+", R14")
}

// EmitBackEdgePoll implements BytecodeArchPort. It decrements the poll budget and exits
// to Go when the budget is exhausted, without clobbering any general-purpose register.
//
// Takes e (*asmgen.Emitter) which receives the emitted back-edge poll instructions.
func (*BytecodeAMD64Arch) EmitBackEdgePoll(e *asmgen.Emitter) {
	emitBackEdgePoll(e)
}

// LoadNextInstructionWord implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes destinationRegister (string) which receives the loaded instruction word.
func (*BytecodeAMD64Arch) LoadNextInstructionWord(e *asmgen.Emitter, destinationRegister string) {
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, "+destinationRegister)
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, destinationRegister+", "+destinationRegister)
}

// DispatchMacros implements BytecodeArchPort.
//
// Returns string which is the dispatch macro header content.
func (*BytecodeAMD64Arch) DispatchMacros() string {
	return amd64DispatchMacrosBody
}

// InitialiseJumpTableEntry implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes handlerSymbol (string) which is the handler function symbol name.
// Takes tableRegister (string) which holds the jump table base address.
// Takes offset (int) which is the byte offset into the jump table.
func (*BytecodeAMD64Arch) InitialiseJumpTableEntry(e *asmgen.Emitter, handlerSymbol, tableRegister string, offset int) {
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, fmt.Sprintf("\xc2\xb7%s(SB), AX", handlerSymbol))
	inst(e, asmamd64.OperationMove64Bits, fmt.Sprintf("AX, %d(%s)", offset, tableRegister))
}

// StringOperations implements BytecodeArchPort.
//
// Returns asmgen.StringOperationsPort which provides string operation emitters.
func (*BytecodeAMD64Arch) StringOperations() asmgen.StringOperationsPort { return &amd64StringOps{} }

// InitialisationOperations implements BytecodeArchPort.
//
// Returns asmgen.InitialisationOperationsPort which provides initialisation operation
// emitters.
func (a *BytecodeAMD64Arch) InitialisationOperations() asmgen.InitialisationOperationsPort {
	return &amd64InitOps{entries: a.jumpTableEntries}
}

// InlineCallOperations implements BytecodeArchPort.
//
// Returns asmgen.InlineCallOperationsPort which provides inline call operation emitters.
func (*BytecodeAMD64Arch) InlineCallOperations() asmgen.InlineCallOperationsPort {
	return &amd64InlineCallOps{}
}

// EmitCallMethodInline implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeAMD64Arch) EmitCallMethodInline(e *asmgen.Emitter) {
	(&amd64InlineCallOps{}).EmitCallMethodInline(e)
}

// EmitGetUpvalueScalar implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpGetUpvalue on direct scalar cells: bank(C)[A] =
// upvalues[B].scalarField(C). The C operand names the destination isa.RegisterKind and
// selects both the upvalueCell field to read and the register bank to write; the four
// scalar banks (int, float, uint, bool) carry no pointers so no write barrier is
// required. Indirect cells, out-of-range indexes, nil cell pointers and non-scalar kinds
// jump to the tier-2 shim with DX intact, which re-runs the instruction through the Go
// handler.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim, e.g.
// "handlerPathBShimGetUpvalue".
func (*BytecodeAMD64Arch) EmitGetUpvalueScalar(e *asmgen.Emitter, shimSymbol string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CURRENT_FRAME(R15), SI")
	inst(e, asmamd64.OperationCompare64Bits, "BX, (CF_UPVALUES_PTR+8)(SI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "gu_shim")
	inst(e, asmamd64.OperationMove64Bits, "CF_UPVALUES_PTR(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "gu_shim")
	inst(e, asmamd64.OperationCompare8Bits, "CELL_IS_INDIRECT(DI), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gu_shim")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	e.Blank()

	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_INT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gu_not_int")
	inst(e, asmamd64.OperationMove64Bits, "CELL_INT_VALUE(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	inst(e, asmamd64.OperationJump, "gu_done")
	e.Label("gu_not_int")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_FLOAT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gu_not_float")
	inst(e, asmamd64.OperationMove64Bits, "CELL_FLOAT_VALUE(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R9)(AX*8)")
	inst(e, asmamd64.OperationJump, "gu_done")
	e.Label("gu_not_float")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_UINT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gu_not_uint")
	inst(e, asmamd64.OperationMove64Bits, "CELL_UINT_VALUE(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(AX*8)")
	inst(e, asmamd64.OperationJump, "gu_done")
	e.Label("gu_not_uint")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_BOOL")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gu_shim")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CELL_BOOL_VALUE(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove8Bits, "CL, (SI)(AX*1)")
	e.Label("gu_done")
	e.Instruction(macroDispatchNext)
	e.Label("gu_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldSliceLen implements BytecodeArchPort. It emits a tier-0 fast path for
// ints[A] = len(receiver[B].field(layout[C])), reading the slice length in place.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim, e.g.
// "handlerPathBShimGetStructFieldSliceLen".
func (*BytecodeAMD64Arch) EmitStructFieldSliceLen(e *asmgen.Emitter, shimSymbol string) {
	emitStructFieldCommonPrologueAMD64(e, "$23", "", "$16")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldSliceIndexScalar implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpGetStructFieldSliceIndexScalar: bank(ext2.C)[A] =
// receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide). The slice header is
// read in place through the receiver (no header value materialised, no element copy
// boxed) and the full-width scalar sub-field loads directly into the destination bank.
// The extension words are peeked without advancing R14; only the success path advances
// past them, so shim fallbacks - including out-of-range indexes, which the Go handler
// raises as interpreted panics - re-run the op from the original program counter.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim.
func (*BytecodeAMD64Arch) EmitStructFieldSliceIndexScalar(e *asmgen.Emitter, shimSymbol string) {
	emitStructFieldCommonPrologueAMD64(e, "$23", "", "$16")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), BX")
	inst(e, asmamd64.OperationMove32Bits, "4(R12)(R14*4), SI")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "BX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, AX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, 8(DI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sf_shim")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "BX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0xFFFF, CX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, AX")
	inst(e, asmamd64.OperationAdd64Bits, "(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "SI, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0xFFFF, CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, AX")
	e.Blank()

	inst(e, asmamd64.OperationShiftRight64Bits, "$24, SI")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	e.Blank()

	inst(e, asmamd64.OperationCompare64Bits, "SI, $REGKIND_INT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sfsi_not_int")
	inst(e, asmamd64.OperationMove64Bits, "(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(BX*8)")
	inst(e, asmamd64.OperationJump, "sfsi_done")
	e.Label("sfsi_not_int")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $REGKIND_FLOAT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sfsi_not_float")
	inst(e, asmamd64.OperationMove64Bits, "(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R9)(BX*8)")
	inst(e, asmamd64.OperationJump, "sfsi_done")
	e.Label("sfsi_not_float")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $REGKIND_UINT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sfsi_not_uint")
	inst(e, asmamd64.OperationMove64Bits, "(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(BX*8)")
	inst(e, asmamd64.OperationJump, "sfsi_done")
	e.Label("sfsi_not_uint")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $REGKIND_BOOL")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove8Bits, "CL, (SI)(BX*1)")
	e.Label("sfsi_done")
	inst(e, asmamd64.OperationAdd64Bits, "$2, R14")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// New creates a new bytecode AMD64 architecture adapter.
//
// Takes entries ([]JumpTableEntry variadic) which are the handler-offset pairs to
// install.
//
// Returns *BytecodeAMD64Arch ready for use.
func New(entries ...JumpTableEntry) *BytecodeAMD64Arch {
	return &BytecodeAMD64Arch{jumpTableEntries: entries, AMD64Arch: core.AMD64Arch{}}
}

// emitBackEdgePoll emits the back-edge poll sequence as a package-level helper, shared by
// fused-jump bodies outside the arch port.
//
// Takes e (*asmgen.Emitter) which receives the emitted back-edge poll instructions.
func emitBackEdgePoll(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationDecrement64Bits, "CTX_POLL_BUDGET(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, "poll_ok")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_POLL, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
	e.Label("poll_ok")
}

// inst emits a tab-indented instruction line with the mnemonic padded to
// mnemonicColumnWidth columns (the standard alignment for amd64 Plan 9 assembly).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes mnemonic (string) which is the instruction mnemonic.
// Takes operands (string) which is the operand string.
func inst(e *asmgen.Emitter, mnemonic, operands string) {
	padding := max(mnemonicColumnWidth-len(mnemonic), 1)
	e.Instruction(mnemonic + strings.Repeat(" ", padding) + operands)
}

// emitTypedSliceGetPrologueAMD64 emits the common Get-shape prologue for typed-slice
// loads with bounds checking.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func emitTypedSliceGetPrologueAMD64(e *asmgen.Emitter, sliceContextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, sliceContextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, labelBoundsFail)
}

// emitTypedSliceSetPrologueAMD64 emits the common Set-shape prologue for typed-slice
// stores with bounds checking.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func emitTypedSliceSetPrologueAMD64(e *asmgen.Emitter, sliceContextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	inst(e, asmamd64.OperationMove64Bits, sliceContextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, labelBoundsFail)
}

// emitPeekExtensionWordAFieldAMD64 emits the sequence that peeks the next instruction
// word (without advancing PC) and extracts its A field as a uint8 into the destination
// register.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationRegister (string) which receives ext.A as a zero-extended uint8.
func emitPeekExtensionWordAFieldAMD64(e *asmgen.Emitter, destinationRegister string) {
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), "+destinationRegister)
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, "+destinationRegister)
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, lowByteOf(destinationRegister)+", "+destinationRegister)
}

// emitTypedSliceTailAMD64 emits the standard tail for a bounds- checked slice sub-op:
// advance PC past the consumed extension word, tail-call DISPATCH_NEXT, and emit the
// bounds_fail label that branches to pathBFallback. The Go-side handleUmbrella re-runs
// from the umbrella opcode and produces the proper error message.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitTypedSliceTailAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
	e.Label(labelBoundsFail)
	inst(e, asmamd64.OperationJump, "·pathBFallback(SB)")
}

// emitTypedSliceByteSliceExtractOperands extracts dstReg and srcReg from the current
// instruction word into AX and BX, and loads the ext1 word into DX in preparation for the
// bounds + header phases.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitTypedSliceByteSliceExtractOperands(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")

	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")

	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
}

// emitTypedSliceByteSliceLoadExtAndBounds extracts the flags / low / high fields from the
// ext1 word, loads the source slice header, and runs the three-step bounds check.
// Branches to labelBoundsFail on any failure.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func emitTypedSliceByteSliceLoadExtAndBounds(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $3")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelBoundsFail)

	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, DI")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, DI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, lowByteOf("DI")+", DI")

	inst(e, asmamd64.OperationMove64Bits, "(R8)(CX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(DI*8), DI")

	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), DX")

	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfSign, labelBoundsFail)
	inst(e, asmamd64.OperationCompare64Bits, "CX, DI")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelBoundsFail)
	inst(e, asmamd64.OperationCompare64Bits, "DI, DX")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelBoundsFail)
}

// emitTypedSliceSliceSliceWriteHeader writes the adjusted Data/Len/Cap header for a
// stride-parameterised typed-slice sub-slice.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
// Takes elementSizeShift (uint8) which is the log2 of the element stride.
func emitTypedSliceSliceSliceWriteHeader(e *asmgen.Emitter, contextOffset string, elementSizeShift uint8) {
	inst(e, asmamd64.OperationMove64Bits, "0(SI), BX")
	if elementSizeShift != 0 {
		inst(e, asmamd64.OperationMove64Bits, "CX, R10")
		inst(e, asmamd64.OperationShiftLeft64Bits, "$"+shiftLiteral(elementSizeShift)+", R10")
		inst(e, asmamd64.OperationAdd64Bits, "R10, BX")
	} else {
		inst(e, asmamd64.OperationAdd64Bits, "CX, BX")
	}
	inst(e, asmamd64.OperationSubtract64Bits, "CX, DI")
	inst(e, asmamd64.OperationSubtract64Bits, "CX, DX")

	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, SI")

	inst(e, asmamd64.OperationMove64Bits, "BX, 0(SI)")
	inst(e, asmamd64.OperationMove64Bits, "DI, 8(SI)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 16(SI)")
}

// shiftLiteral converts a small element-size shift count into its decimal Plan-9
// immediate-operand representation. Centralising the digit selection keeps the emitter
// helpers free of strconv usage and stays correct for the four shifts the typed-slice
// banks actually use (0, 1, 3, 4).
//
// Takes shift (uint8) which is the log2 of the element stride.
//
// Returns the decimal literal as a string.
func shiftLiteral(shift uint8) string {
	switch shift {
	case elementStrideShiftByte:
		return "1"
	case elementStrideShiftQword:
		return "3"
	case elementStrideShiftXmm:
		return "4"
	}
	return "0"
}

// emitTypedSliceByteSliceWriteHeader computes the destination slot pointer and writes the
// adjusted Data/Len/Cap into the new 24-byte header. Assumes bounds have already been
// validated by the caller.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func emitTypedSliceByteSliceWriteHeader(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "0(SI), BX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, BX")
	inst(e, asmamd64.OperationSubtract64Bits, "CX, DI")
	inst(e, asmamd64.OperationSubtract64Bits, "CX, DX")

	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, SI")

	inst(e, asmamd64.OperationMove64Bits, "BX, 0(SI)")
	inst(e, asmamd64.OperationMove64Bits, "DI, 8(SI)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 16(SI)")
}

// emitTypedRangeNextByteOperands extracts idxReg, srcReg and dstReg from the instruction
// word into AX, BX and DI respectively, and bumps the index in place (ints[idxReg]++)
// before the bounds compare.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitTypedRangeNextByteOperands(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")

	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")

	inst(e, asmamd64.OperationMove64Bits, "DX, DI")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, DI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, lowByteOf("DI")+", DI")

	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
	inst(e, asmamd64.OperationIncrement64Bits, "CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
}

// emitTypedRangeNextByteLoadAndCompare computes &slicesByte[srcReg], loads the slice's
// Len, and branches to the end-of-range label when index >= Len.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func emitTypedRangeNextByteLoadAndCompare(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), DX")

	inst(e, asmamd64.OperationCompare64Bits, "CX, DX")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, "rangeByteEnd")
}

// emitTypedRangeNextByteBodyAndEpilogue emits the in-range body (load source.Data[index]
// into uints[dstReg] and advance PC), then the end-of-range jump-offset decode, then the
// shared DISPATCH_NEXT landing pad.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitTypedRangeNextByteBodyAndEpilogue(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "0(SI), SI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(SI)(CX*1), AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "AX, (SI)(DI*8)")

	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationJump, "rangeByteDispatch")

	e.Label("rangeByteEnd")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0xFFFFFF, AX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationAdd64Bits, "AX, R14")
	emitBackEdgePoll(e)

	e.Label("rangeByteDispatch")
	e.Instruction(macroDispatchNext)
}

// bankAccess returns the base register and load/store mnemonic for a register bank.
//
// The generator names the banks needing a different base register or mnemonic and puts
// the integer-bank form in the default, so a new bank emits the common sequence rather
// than nothing. Any mistake here changes emitted assembly, which make
// generate-asmgen-validate compares byte for byte.
//
// Takes bank (asmgen.RegisterBank) which selects the register bank.
//
// Returns string which is the base register name.
// Returns string which is the load/store mnemonic.
func bankAccess(bank asmgen.RegisterBank) (base, mnemonic string) {
	switch bank {
	case asmgen.RegisterBankFloat:
		return "R9", asmamd64.OperationMoveScalarDouble
	case asmgen.RegisterBankString, asmgen.RegisterBankBoolean, asmgen.RegisterBankUnsignedInteger:
		return "", asmamd64.OperationMove64Bits
	default:
		return "R8", asmamd64.OperationMove64Bits
	}
}

// intOpMnemonic maps an abstract integer operation name to its amd64 mnemonic.
//
// Takes op (string) which is the abstract operation name (e.g. ADD, SUB).
//
// Returns string which is the corresponding amd64 mnemonic.
func intOpMnemonic(op string) string {
	switch op {
	case "ADD":
		return asmamd64.OperationAdd64Bits
	case "SUB":
		return asmamd64.OperationSubtract64Bits
	case "MUL":
		return asmamd64.OperationSignedMultiply64Bits
	case "AND":
		return asmamd64.OperationBitwiseAnd64Bits
	case "OR":
		return asmamd64.OperationBitwiseOr64Bits
	case "XOR":
		return asmamd64.OperationBitwiseXor64Bits
	default:
		return op + "Q"
	}
}

// floatOpMnemonic maps an abstract float operation name to its amd64 mnemonic.
//
// Takes op (string) which is the abstract operation name (e.g. ADD, SUB).
//
// Returns string which is the corresponding amd64 mnemonic.
func floatOpMnemonic(op string) string {
	switch op {
	case "ADD":
		return asmamd64.OperationAddScalarDouble
	case "SUB":
		return asmamd64.OperationSubtractScalarDouble
	case "MUL":
		return asmamd64.OperationMultiplyScalarDouble
	case "DIV":
		return asmamd64.OperationDivideScalarDouble
	default:
		return op + "SD"
	}
}
