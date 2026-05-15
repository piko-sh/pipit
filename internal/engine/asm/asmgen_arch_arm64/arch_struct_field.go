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

package asmgen_arch_arm64

import (
	"fmt"
	"strings"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmarm64"
	core "piko.sh/asmgen/asmgen_arch_arm64"
)

// LoadComplexHalfToFloatBank implements BytecodeArchPort.
//
// Loads one float64 half of a complex128 element from the complex register bank and
// stores it into the float register bank. Each complex slot is 16 bytes (real then imag).
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
// Takes indexRegister (string) which holds the source complex slot index.
// Takes halfOffset (string) which is "0" or "8".
// Takes destinationFloatIndexRegister (string) which holds the destination index in the
// float register bank.
func (*BytecodeARM64Arch) LoadComplexHalfToFloatBank(e *asmgen.Emitter, contextOffset, indexRegister, halfOffset, destinationFloatIndexRegister string) {
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+indexRegister+", R6")
	inst5(e, asmarm64.OperationAdd, "R6, R5, R5")
	inst5(e, asmarm64.OperationFloatMove64Bits, halfOffset+"(R5), F0")
	inst5(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationFloatIndexRegister+"<<3)")
}

// LoadTypedSliceHeaderLength implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
// Takes indexRegister (string) which holds the slot index.
// Takes destinationRegister (string) which receives the length.
func (*BytecodeARM64Arch) LoadTypedSliceHeaderLength(e *asmgen.Emitter, contextOffset, indexRegister, destinationRegister string) {
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R6")
	inst5(e, asmarm64.OperationMultiply, "R6, "+indexRegister+", R7")
	inst5(e, asmarm64.OperationAdd, "R7, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), "+destinationRegister)
}

// LoadFromBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes bank (asmgen.RegisterBank) which selects the register bank to load from.
// Takes indexRegister (string) which holds the register index.
// Takes destinationRegister (string) which receives the loaded value.
func (*BytecodeARM64Arch) LoadFromBank(e *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string) {
	base, mnemonic := bankAccess(bank)
	shift := bankShift(bank)
	inst5(e, mnemonic, "("+base+")("+indexRegister+"<<"+shift+"), "+destinationRegister)
}

// StoreToBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes bank (asmgen.RegisterBank) which selects the register bank to store to.
// Takes sourceRegister (string) which holds the value to store.
// Takes indexRegister (string) which holds the register index.
func (*BytecodeARM64Arch) StoreToBank(e *asmgen.Emitter, bank asmgen.RegisterBank, sourceRegister, indexRegister string) {
	base, mnemonic := bankAccess(bank)
	shift := bankShift(bank)
	inst5(e, mnemonic, sourceRegister+", ("+base+")("+indexRegister+"<<"+shift+")")
}

// LoadConstant implements BytecodeArchPort.
//
// The generator names the banks needing a different base register or mnemonic and puts
// the integer-bank form in the default, so a new bank emits the common sequence rather
// than nothing. Any mistake here changes emitted assembly, which make
// generate-asmgen-validate compares byte for byte.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes bank (asmgen.RegisterBank) which selects the constant pool to load from.
// Takes indexRegister (string) which holds the constant index.
// Takes destinationRegister (string) which receives the loaded value.
func (*BytecodeARM64Arch) LoadConstant(e *asmgen.Emitter, bank asmgen.RegisterBank, indexRegister, destinationRegister string) {
	switch bank {
	case asmgen.RegisterBankInteger:
		inst5(e, asmarm64.OperationMove64Bits, "(R26)("+indexRegister+"<<3), "+destinationRegister)
	case asmgen.RegisterBankFloat:
		inst5(e, asmarm64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R19), "+destinationRegister)
	default:
	}
}

// LoadFloatConstantToBank implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the destination float register index.
// Takes constantIndex (string) which is the float constant pool index.
func (*BytecodeARM64Arch) LoadFloatConstantToBank(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst(e, asmarm64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R19), R5", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatMove64Bits, "(R5)("+constantIndex+"<<3), F0", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
}

// LoadContextField implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes offset (string) which is the byte offset into the context.
// Takes destinationRegister (string) which receives the loaded value.
func (*BytecodeARM64Arch) LoadContextField(e *asmgen.Emitter, offset, destinationRegister string) {
	inst5(e, asmarm64.OperationMove64Bits, offset+"(R19), "+destinationRegister)
}

// StoreContextField implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sourceRegister (string) which holds the value to store.
// Takes offset (string) which is the byte offset into the context.
func (*BytecodeARM64Arch) StoreContextField(e *asmgen.Emitter, sourceRegister, offset string) {
	inst5(e, asmarm64.OperationMove64Bits, sourceRegister+", "+offset+"(R19)")
}

// StoreContextImmediate implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes value (string) which is the immediate value to store.
// Takes offset (string) which is the byte offset into the context.
func (*BytecodeARM64Arch) StoreContextImmediate(e *asmgen.Emitter, value, offset string) {
	inst5(e, asmarm64.OperationMove64Bits, value+", R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, "+offset+"(R19)")
}

// IntegerBinaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the destination register index.
// Takes leftSourceIndex (string) which is the left operand register index.
// Takes rightSourceIndex (string) which is the right operand register index.
func (*BytecodeARM64Arch) IntegerBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+leftSourceIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+rightSourceIndex+"<<3), R7")
	mnemonic := intOpMnemonic(operation)
	inst5(e, mnemonic, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+destinationIndex+"<<3)")
}

// IntegerBinaryOperationConstant implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the destination register index.
// Takes sourceIndex (string) which is the source register index.
// Takes constantIndex (string) which is the constant pool index.
func (*BytecodeARM64Arch) IntegerBinaryOperationConstant(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex, constantIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+sourceIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R26)("+constantIndex+"<<3), R7")
	mnemonic := intOpMnemonic(operation)
	inst5(e, mnemonic, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+destinationIndex+"<<3)")
}

// UintBinaryOperation implements BytecodeArchPort.
//
// Bit-pattern-identical to IntegerBinaryOperation, but addresses the uint register bank
// via CTX_UINTS_BASE loaded into R5 rather than the preserved R23 used for the int bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the destination uint register index.
// Takes leftSourceIndex (string) which is the left operand uint register index.
// Takes rightSourceIndex (string) which is the right operand uint register index.
func (*BytecodeARM64Arch) UintBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+leftSourceIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+rightSourceIndex+"<<3), R7")
	mnemonic := intOpMnemonic(operation)
	inst5(e, mnemonic, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R8)("+destinationIndex+"<<3)")
}

// UintShift implements BytecodeArchPort.
//
// Right shifts use LSR (logical) rather than ASR (arithmetic) because uint64 has no sign
// bit to preserve. Left shifts use LSL identical to the int variant. Value and amount
// both live in the uint bank (matches isa.OpShiftLeftUint / isa.OpShiftRightUint
// semantics).
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes direction (string) which is LEFT or RIGHT.
// Takes destinationIndex (string) which is the destination uint register index.
// Takes valueIndex (string) which is the value uint register index.
// Takes amountIndex (string) which is the shift amount uint register index.
func (*BytecodeARM64Arch) UintShift(e *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+valueIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+amountIndex+"<<3), R7")
	switch direction {
	case "LEFT":
		inst5(e, asmarm64.OperationLogicalShiftLeft, "R7, R6, R6")
	case "RIGHT":
		inst5(e, asmarm64.OperationLogicalShiftRight, "R7, R6, R6")
	}

	inst5(e, asmarm64.OperationCompare, "$64, R7")
	inst5(e, asmarm64.OperationConditionalSelect, "HS, ZR, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R8)("+destinationIndex+"<<3)")
}

// UintCompareAndSet implements BytecodeArchPort.
//
// Maps the condition names (EQ/NE/LT/LE/GT/GE) to unsigned condition codes for the
// inequality cases (LO/LS/HI/HS). Operands come from the uint bank; the boolean result is
// written into the int bank (booleans are stored as int64).
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the abstract comparison name.
// Takes destinationIndex (string) which is the destination int register index.
// Takes leftIndex (string) which is the left operand uint register index.
// Takes rightIndex (string) which is the right operand uint register index.
func (*BytecodeARM64Arch) UintCompareAndSet(e *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+leftIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+rightIndex+"<<3), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	var cset string
	switch condition {
	case conditionEQ:
		cset = conditionEQ
	case conditionNE:
		cset = conditionNE
	case "LT":
		cset = "LO"
	case "LE":
		cset = "LS"
	case "GT":
		cset = "HI"
	case "GE":
		cset = "HS"
	}
	inst5(e, asmarm64.OperationConditionalSet, cset+", R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+destinationIndex+"<<3)")
}

// IntegerUnaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the unary operation name.
// Takes destinationIndex (string) which is the destination register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeARM64Arch) IntegerUnaryOperation(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+sourceIndex+"<<3), R5")
	switch operation {
	case "NEG":
		inst5(e, asmarm64.OperationNegate, "R5, R5")
	case "NOT":
		inst5(e, asmarm64.OperationMoveNegated, "R5, R5")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R5, (R23)("+destinationIndex+"<<3)")
}

// IntegerInPlace implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the in-place operation name (INC or DEC).
// Takes indexRegister (string) which is the register index to modify.
func (*BytecodeARM64Arch) IntegerInPlace(e *asmgen.Emitter, operation string, indexRegister string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+indexRegister+"<<3), R5")
	switch operation {
	case "INC":
		inst5(e, asmarm64.OperationAdd, "$1, R5, R5")
	case "DEC":
		inst5(e, asmarm64.OperationSubtract, "$1, R5, R5")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R5, (R23)("+indexRegister+"<<3)")
}

// UintInPlace implements BytecodeArchPort.
//
// Loads CTX_UINTS_BASE into baseScratch, then performs a load-modify-store on the indexed
// uint64 (arm64 has no memory-form INC/DEC, so we use the classic three-instruction
// sequence with R5 as the value scratch). Mirrors LoadFromUintBank's base-loading
// pattern.
//
// Takes e (*asmgen.Emitter) which receives emitted instructions.
// Takes operation (string) which is INC or DEC.
// Takes indexRegister (string) which holds the uint register index.
// Takes baseScratch (string) which receives the loaded uint base.
func (*BytecodeARM64Arch) UintInPlace(e *asmgen.Emitter, operation string, indexRegister string, baseScratch string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), "+baseScratch)
	inst5(e, asmarm64.OperationMove64Bits, "("+baseScratch+")("+indexRegister+"<<3), R5")
	switch operation {
	case "INC":
		inst5(e, asmarm64.OperationAdd, "$1, R5, R5")
	case "DEC":
		inst5(e, asmarm64.OperationSubtract, "$1, R5, R5")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R5, ("+baseScratch+")("+indexRegister+"<<3)")
}

// IntegerDivide implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dividendIndex (string) which is the dividend register index.
// Takes divisorIndex (string) which is the divisor register index.
// Takes quotientDestinationIndex (string) which is the quotient destination index, or
// empty to skip.
// Takes remainderDestinationIndex (string) which is the remainder destination index, or
// empty to skip.
// Takes zeroLabel (string) which is the branch target for division by zero.
func (*BytecodeARM64Arch) IntegerDivide(e *asmgen.Emitter, dividendIndex, divisorIndex, quotientDestinationIndex, remainderDestinationIndex, zeroLabel string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+divisorIndex+"<<3), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, "+zeroLabel)
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+dividendIndex+"<<3), R6")
	inst5(e, asmarm64.OperationSignedDivide, "R7, R6, R6")
	if quotientDestinationIndex != "" {
		inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+quotientDestinationIndex+"<<3)")
	}
	if remainderDestinationIndex != "" {
		inst5(e, asmarm64.OperationMove64Bits, "(R23)("+dividendIndex+"<<3), R8")
		inst5(e, asmarm64.OperationSignedDivide, "R7, R8, R6")
		inst5(e, asmarm64.OperationMultiply, "R7, R6, R6")
		inst5(e, asmarm64.OperationSubtract, "R6, R8, R8")
		inst5(e, asmarm64.OperationMove64Bits, "R8, (R23)("+remainderDestinationIndex+"<<3)")
	}
}

// IntegerShift implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes direction (string) which is LEFT or RIGHT.
// Takes destinationIndex (string) which is the destination register index.
// Takes valueIndex (string) which is the value register index.
// Takes amountIndex (string) which is the shift amount register index.
func (*BytecodeARM64Arch) IntegerShift(e *asmgen.Emitter, direction string, destinationIndex, valueIndex, amountIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+valueIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+amountIndex+"<<3), R7")

	switch direction {
	case "LEFT":
		inst5(e, asmarm64.OperationLogicalShiftLeft, "R7, R6, R6")
		inst5(e, asmarm64.OperationCompare, "$64, R7")
		inst5(e, asmarm64.OperationConditionalSelect, "HS, ZR, R6, R6")
	case "RIGHT":
		inst5(e, asmarm64.OperationArithmeticShiftRight, "$63, R6, R8")
		inst5(e, asmarm64.OperationArithmeticShiftRight, "R7, R6, R6")
		inst5(e, asmarm64.OperationCompare, "$64, R7")
		inst5(e, asmarm64.OperationConditionalSelect, "HS, R8, R6, R6")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+destinationIndex+"<<3)")
}

// IntegerCompareAndSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the comparison condition code.
// Takes destinationIndex (string) which is the destination register index.
// Takes leftIndex (string) which is the left operand register index.
// Takes rightIndex (string) which is the right operand register index.
func (*BytecodeARM64Arch) IntegerCompareAndSet(e *asmgen.Emitter, condition string, destinationIndex, leftIndex, rightIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+leftIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+rightIndex+"<<3), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	inst5(e, asmarm64.OperationConditionalSet, condition+", R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)("+destinationIndex+"<<3)")
}

// IntegerCompareAndBranch implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the comparison condition code.
// Takes leftIndex (string) which is the left operand register index.
// Takes rightIndex (string) which is the right operand register index.
// Takes label (string) which is the branch target label.
func (*BytecodeARM64Arch) IntegerCompareAndBranch(e *asmgen.Emitter, condition string, leftIndex, rightIndex, label string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+leftIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+rightIndex+"<<3), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	branchCond := "B" + condition
	inst5(e, branchCond, label)
}

// IntegerCompareConstantAndBranch implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the comparison condition code.
// Takes registerIndex (string) which is the register index to compare.
// Takes constantIndex (string) which is the constant pool index.
// Takes label (string) which is the branch target label.
func (*BytecodeARM64Arch) IntegerCompareConstantAndBranch(e *asmgen.Emitter, condition string, registerIndex, constantIndex, label string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+registerIndex+"<<3), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R26)("+constantIndex+"<<3), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	branchCond := "B" + condition
	inst5(e, branchCond, label)
}

// StringLengthRead implements BytecodeArchPort. R5, R6 and R7 are clobbered and
// sourceIndex is destructively shifted.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the int-bank destination register.
// Takes sourceIndex (string) which is the string-bank source register.
func (*BytecodeARM64Arch) StringLengthRead(e *asmgen.Emitter, destinationIndex, sourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+sourceIndex+", "+sourceIndex)
	inst5(e, asmarm64.OperationAdd, sourceIndex+", R5, R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R23)("+destinationIndex+"<<3)")
}

// StringCopy implements BytecodeArchPort. Both index registers are destructively shifted.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the string-bank destination register.
// Takes sourceIndex (string) which is the string-bank source register.
func (*BytecodeARM64Arch) StringCopy(e *asmgen.Emitter, destinationIndex, sourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+destinationIndex+", "+destinationIndex)
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+sourceIndex+", "+sourceIndex)
	inst5(e, asmarm64.OperationAdd, destinationIndex+", R5, R6")
	inst5(e, asmarm64.OperationAdd, sourceIndex+", R5, R7")
	inst5(e, asmarm64.OperationMove64Bits, "(R7), R8")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R8, (R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 8(R6)")
}

// StringConstLoad implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the strings-bank destination register;
// destructively shifted by 4 during the emit.
// Takes constantIndex (string) which is the constant pool index; destructively shifted by
// 4 during the emit.
func (*BytecodeARM64Arch) StringConstLoad(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STR_CONSTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+destinationIndex+", "+destinationIndex)
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, "+constantIndex+", "+constantIndex)
	inst5(e, asmarm64.OperationAdd, destinationIndex+", R6, R7")
	inst5(e, asmarm64.OperationAdd, constantIndex+", R5, R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R8), R9")
	inst5(e, asmarm64.OperationMove64Bits, "8(R8), R10")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R7)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, 8(R7)")
}

// BoolConstLoad implements BytecodeArchPort. Copies a single byte from the bool constant
// table into the bools register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the bools-bank destination register index
// (unscaled byte offset).
// Takes constantIndex (string) which is the constant pool index (unscaled byte offset).
func (*BytecodeARM64Arch) BoolConstLoad(e *asmgen.Emitter, destinationIndex, constantIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOL_CONSTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, constantIndex+", R5, R7")
	inst5(e, asmarm64.OperationAdd, destinationIndex+", R6, R8")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R7), R9")
	inst5(e, asmarm64.OperationMove8Bits, "R9, (R8)")
}

// FloatBinaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the arithmetic operation name.
// Takes destinationIndex (string) which is the destination float register index.
// Takes leftSourceIndex (string) which is the left operand float register index.
// Takes rightSourceIndex (string) which is the right operand float register index.
func (*BytecodeARM64Arch) FloatBinaryOperation(e *asmgen.Emitter, operation string, destinationIndex, leftSourceIndex, rightSourceIndex string) {
	inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+leftSourceIndex+"<<3), F0", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+rightSourceIndex+"<<3), F1", mnemonicColumnWidth)
	mnemonic := floatOpMnemonic(operation)
	inst(e, mnemonic, "F1, F0, F0", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
}

// FloatUnaryOperation implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which is the unary operation name.
// Takes destinationIndex (string) which is the destination float register index.
// Takes sourceIndex (string) which is the source float register index.
func (*BytecodeARM64Arch) FloatUnaryOperation(e *asmgen.Emitter, operation string, destinationIndex, sourceIndex string) {
	switch operation {
	case "NEG":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatNegate64Bits, "F0, F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
	case "SQRT":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatSquareRoot64Bits, "F0, F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
	case "ABS":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatAbsolute64Bits, "F0, F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
	case "FLOOR":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatRoundToMinus64Bits, "F0, F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", roundingColumnWidth)
	case "CEIL":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatRoundToPlus64Bits, "F0, F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", roundingColumnWidth)
	case "TRUNC":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatRoundToZero64Bits, "F0, F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", roundingColumnWidth)
	case "ROUND":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatRoundToNearestAway64Bits, "F0, F0", roundingColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", roundingColumnWidth)
	}
}

// FloatCompareAndSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the comparison condition code.
// Takes integerDestinationIndex (string) which is the integer bank destination index.
// Takes floatLeftIndex (string) which is the left float operand index.
// Takes floatRightIndex (string) which is the right float operand index.
func (*BytecodeARM64Arch) FloatCompareAndSet(e *asmgen.Emitter, condition string, integerDestinationIndex, floatLeftIndex, floatRightIndex string) {
	inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+floatLeftIndex+"<<3), F0", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+floatRightIndex+"<<3), F1", mnemonicColumnWidth)
	inst(e, asmarm64.OperationFloatCompare64Bits, "F1, F0", mnemonicColumnWidth)
	armCond := floatConditionCode(condition)
	inst(e, asmarm64.OperationConditionalSet, armCond+", R6", mnemonicColumnWidth)
	inst(e, asmarm64.OperationMove64Bits, "R6, (R23)("+integerDestinationIndex+"<<3)", mnemonicColumnWidth)
}

// FloatConversion implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes direction (string) which is INTEGER_TO_FLOAT or FLOAT_TO_INTEGER.
// Takes destinationIndex (string) which is the destination register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeARM64Arch) FloatConversion(e *asmgen.Emitter, direction string, destinationIndex, sourceIndex string) {
	switch direction {
	case "INTEGER_TO_FLOAT":
		inst5(e, asmarm64.OperationMove64Bits, "(R23)("+sourceIndex+"<<3), R5")
		inst(e, asmarm64.OperationSignedIntConvertToFloat64Bits, "R5, F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
	case "FLOAT_TO_INTEGER":
		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatConvertToSignedInt64Bits, "F0, R5", conversionColumnWidth)
		inst5(e, asmarm64.OperationMove64Bits, "R5, (R23)("+destinationIndex+"<<3)")
	case "UNSIGNED_TO_FLOAT":

		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R6")
		inst5(e, asmarm64.OperationMove64Bits, "(R6)("+sourceIndex+"<<3), R5")
		inst(e, asmarm64.OperationUnsignedIntConvertToFloat64Bits, "R5, F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)("+destinationIndex+"<<3)", mnemonicColumnWidth)
	case "FLOAT_TO_UNSIGNED":

		inst(e, asmarm64.OperationFloatMove64Bits, "(R24)("+sourceIndex+"<<3), F0", mnemonicColumnWidth)
		inst(e, asmarm64.OperationFloatConvertToUnsignedInt64Bits, "F0, R5", conversionColumnWidth)
		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R6")
		inst5(e, asmarm64.OperationMove64Bits, "R5, (R6)("+destinationIndex+"<<3)")
	}
}

// LoadFromUintBank implements BytecodeArchPort. Loads CTX_UINTS_BASE into baseScratch
// then loads the indexed uint64 into the destination.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes indexRegister (string) which holds the register index.
// Takes destinationRegister (string) which receives the loaded value.
// Takes baseScratch (string) which is a scratch register for the base.
func (*BytecodeARM64Arch) LoadFromUintBank(e *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), "+baseScratch)
	inst5(e, asmarm64.OperationMove64Bits, "("+baseScratch+")("+indexRegister+"<<3), "+destinationRegister)
}

// StoreToUintBank implements BytecodeArchPort. Loads CTX_UINTS_BASE into baseScratch then
// stores the source register at the index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sourceRegister (string) which holds the value to store.
// Takes indexRegister (string) which holds the destination index.
// Takes baseScratch (string) which is a scratch register for the base.
func (*BytecodeARM64Arch) StoreToUintBank(e *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), "+baseScratch)
	inst5(e, asmarm64.OperationMove64Bits, sourceRegister+", ("+baseScratch+")("+indexRegister+"<<3)")
}

// LoadFromBoolBank implements BytecodeArchPort.
//
// Bools are 1-byte elements; arm64 provides MOVBU for zero-extended byte loads.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes indexRegister (string) which holds the register index.
// Takes destinationRegister (string) which receives the loaded value.
// Takes baseScratch (string) which is a scratch register for the base.
func (*BytecodeARM64Arch) LoadFromBoolBank(e *asmgen.Emitter, indexRegister, destinationRegister, baseScratch string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), "+baseScratch)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "("+baseScratch+")("+indexRegister+"), "+destinationRegister)
}

// StoreToBoolBank implements BytecodeArchPort. Stores the low byte of the source register
// into the bool bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sourceRegister (string) which holds the value to store.
// Takes indexRegister (string) which holds the destination index.
// Takes baseScratch (string) which is a scratch register for the base.
func (*BytecodeARM64Arch) StoreToBoolBank(e *asmgen.Emitter, sourceRegister, indexRegister, baseScratch string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), "+baseScratch)
	inst5(e, asmarm64.OperationMove8Bits, sourceRegister+", ("+baseScratch+")("+indexRegister+")")
}

// BitwiseNotInPlace implements BytecodeArchPort.
//
// arm64's MVN performs bitwise complement (move-not); the two-operand form writes the
// result back to the source register.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes register (string) which is the register to complement.
func (*BytecodeARM64Arch) BitwiseNotInPlace(e *asmgen.Emitter, register string) {
	inst5(e, asmarm64.OperationMoveNegated, register+", "+register)
}

// LogicalSetNonZero implements BytecodeArchPort. CMP source against 0 then CSET NE writes
// 1 to destination if not-equal flag set, 0 otherwise.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationRegister (string) which receives 0 or 1.
// Takes sourceRegister (string) which is tested for non-zero.
func (*BytecodeARM64Arch) LogicalSetNonZero(e *asmgen.Emitter, destinationRegister, sourceRegister string) {
	inst5(e, asmarm64.OperationCompare, "$0, "+sourceRegister)
	inst5(e, asmarm64.OperationConditionalSet, "NE, "+destinationRegister)
}

// LogicalNot implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationIndex (string) which is the destination register index.
// Takes sourceIndex (string) which is the source register index.
func (*BytecodeARM64Arch) LogicalNot(e *asmgen.Emitter, destinationIndex, sourceIndex string) {
	inst5(e, asmarm64.OperationMove64Bits, "(R23)("+sourceIndex+"<<3), R5")
	inst5(e, asmarm64.OperationCompare, "$0, R5")
	inst5(e, asmarm64.OperationConditionalSet, "EQ, R5")
	inst5(e, asmarm64.OperationMove64Bits, "R5, (R23)("+destinationIndex+"<<3)")
}

// DispatchNext implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the dispatch macro invocation.
func (*BytecodeARM64Arch) DispatchNext(e *asmgen.Emitter) { e.Instruction(macroDispatchNext) }

// DivisionByZeroExit implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) DivisionByZeroExit(e *asmgen.Emitter) {
	e.Instruction(macroDivByZeroExit)
}

// EmitTruncateNarrow implements BytecodeArchitecturePort.
//
// arm64 conventions used here:
//
//	R0  - instruction word (provided by DISPATCH_NEXT in the prior op).
//	R19 - dispatchContext base.
//	R23 - intsBase.
//	R1..R6 - scratch.
//
// arm64 shift instructions take the count as a register operand directly (no CL
// constraint), so the int path computes (64-B) in R4 and then LSL/ASR with R4 as the
// shift amount.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitTruncateNarrow(e *asmgen.Emitter) {
	e.IndentedComment("Extract A (register index) into R1.")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	e.Blank()

	e.IndentedComment("Extract B (bit width) into R2.")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R2")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R2, R2")
	e.Blank()

	e.IndentedComment("Extract C (registerKind) into R3.")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	e.Blank()

	e.IndentedComment("Branch on registerKind == registerUint (5).")
	inst5(e, asmarm64.OperationCompare, "$5, R3")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "handler_truncate_narrow_int")
	e.Blank()

	e.IndentedComment("--- Uint path: uints[A] &= (1 << B) - 1 ---")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R4")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "R2, R4, R4")
	inst5(e, asmarm64.OperationSubtract, "$1, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R1<<3), R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R5)(R1<<3)")
	inst5(e, asmarm64.OperationBranch, "handler_truncate_narrow_done")
	e.Blank()

	e.Label("handler_truncate_narrow_int")
	e.IndentedComment("--- Int path: ints[A] = (ints[A] << (64-B)) >> (64-B)  (arithmetic) ---")
	inst5(e, asmarm64.OperationMove64Bits, "$64, R4")
	inst5(e, asmarm64.OperationSubtract, "R2, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R1<<3), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "R4, R6, R6")
	inst5(e, asmarm64.OperationArithmeticShiftRight, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)(R1<<3)")
	e.Blank()

	e.Label("handler_truncate_narrow_done")
	e.Instruction(macroDispatchNext)
}

// EmitJumpTableBootstrap implements BytecodeArchitecturePort.
//
// arm64 has no string-copy instruction; emulate the amd64 REP MOVSQ with a counted loop
// using LDP/STP (load-pair / store-pair) so each iteration moves two 8-byte slots.
// entriesPerTable / 2 iterations per source table.
//
// Each source gets its own loop label suffixed with the source index so multiple
// bootstrap calls in the same file do not collide.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
// Takes destSymbol (string) which is the Plan-9 symbol of the destination table.
// Takes sourceSymbols ([]string) which are the source-table symbols in destination-layout
// order.
// Takes entriesPerTable (int) which is the number of 8-byte slots contributed by each
// source; must be even.
func (*BytecodeARM64Arch) EmitJumpTableBootstrap(
	e *asmgen.Emitter,
	destSymbol string,
	sourceSymbols []string,
	entriesPerTable int,
) {
	pairsPerTable := entriesPerTable / 2
	inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("$%s(SB), R1", destSymbol))
	for i, source := range sourceSymbols {
		label := fmt.Sprintf("copy_table_%d", i)
		e.Blank()
		inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("$%s(SB), R0", source))
		inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("$%d, R2", pairsPerTable))
		e.Label(label)
		inst5(e, asmarm64.OperationLoadPair, "(R0), (R3, R4)")
		inst5(e, asmarm64.OperationStorePair, "(R3, R4), (R1)")
		inst5(e, asmarm64.OperationAdd, "$16, R0, R0")
		inst5(e, asmarm64.OperationAdd, "$16, R1, R1")
		inst5(e, asmarm64.OperationSubtract, "$1, R2, R2")
		inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, fmt.Sprintf("R2, %s", label))
	}
	e.Blank()
	inst5(e, asmarm64.OperationReturn, "")
}

// ExitWithReason implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes reason (string) which is the exit reason constant name.
func (*BytecodeARM64Arch) ExitWithReason(e *asmgen.Emitter, reason string) {
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$"+reason+", R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationReturn, "")
}

// IncrementProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) IncrementProgramCounter(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
}

// DecrementProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) DecrementProgramCounter(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
}

// AddToProgramCounter implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes register (string) which holds the value to add to the program counter.
func (*BytecodeARM64Arch) AddToProgramCounter(e *asmgen.Emitter, register string) {
	inst5(e, asmarm64.OperationAdd, register+", R20, R20")
}

// EmitBackEdgePoll implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted back-edge poll instructions.
func (*BytecodeARM64Arch) EmitBackEdgePoll(e *asmgen.Emitter) {
	emitBackEdgePoll(e)
}

// LoadNextInstructionWord implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationRegister (string) which receives the loaded instruction word.
func (*BytecodeARM64Arch) LoadNextInstructionWord(e *asmgen.Emitter, destinationRegister string) {
	inst(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R0", mnemonicColumnWidth)
	inst(e, asmarm64.OperationAdd, "$1, R20, R20", mnemonicColumnWidth)
	inst(e, asmarm64.OperationLogicalShiftRight, "$8, R0, "+destinationRegister, mnemonicColumnWidth)
	inst(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, "+destinationRegister+", "+destinationRegister, mnemonicColumnWidth)
	inst(e, asmarm64.OperationLogicalShiftLeft, "$48, "+destinationRegister+", "+destinationRegister, mnemonicColumnWidth)
	inst(e, asmarm64.OperationArithmeticShiftRight, "$48, "+destinationRegister+", "+destinationRegister, mnemonicColumnWidth)
}

// DispatchMacros implements BytecodeArchPort.
//
// Returns string which is the C preprocessor macro definitions for the dispatch loop.
func (*BytecodeARM64Arch) DispatchMacros() string {
	return arm64DispatchMacrosBody
}

// InitialiseJumpTableEntry implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes handlerSymbol (string) which is the handler function symbol name.
// Takes tableRegister (string) which holds the jump table base address.
// Takes offset (int) which is the byte offset into the jump table.
func (*BytecodeARM64Arch) InitialiseJumpTableEntry(e *asmgen.Emitter, handlerSymbol, tableRegister string, offset int) {
	inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("\xc2\xb7%s(SB), R0", handlerSymbol))
	inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("R0, %d(%s)", offset, tableRegister))
}

// StringOperations implements BytecodeArchPort.
//
// Returns asmgen.StringOperationsPort which provides the arm64 string operation emitters.
func (*BytecodeARM64Arch) StringOperations() asmgen.StringOperationsPort { return &arm64StringOps{} }

// InitialisationOperations implements BytecodeArchPort.
//
// Returns asmgen.InitialisationOperationsPort which provides the arm64 initialisation
// emitters.
func (a *BytecodeARM64Arch) InitialisationOperations() asmgen.InitialisationOperationsPort {
	return &arm64InitOps{entries: a.jumpTableEntries}
}

// InlineCallOperations implements BytecodeArchPort.
//
// Returns asmgen.InlineCallOperationsPort which provides the arm64 inline call emitters.
func (*BytecodeARM64Arch) InlineCallOperations() asmgen.InlineCallOperationsPort {
	return &arm64InlineCallOps{}
}

// EmitCallMethodInline implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitCallMethodInline(e *asmgen.Emitter) {
	(&arm64InlineCallOps{}).EmitCallMethodInline(e)
}

// EmitPathBCallShim / EmitTier2CallShimNarrow / EmitTier2CallShimReal for the arm64
// architecture live in tier2_shim.go (sibling file in this package). Register
// conventions:
//
// 	R19 - ctx pointer
// 	R20 - pipit PC (spilled to CTX_PC across the BL; also CTX_SAVED_PC
// 	      in the wide variant)
// 	R21 - codeLength
// 	R22 - codeBase
// 	R23 - intsBase
// 	R24 - floatsBase
// 	R25 - jumpTable
// 	R26 - intConstsBase
// 	R0  - holds the 4-byte instruction word

// EmitGetUpvalueScalar implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpGetUpvalue on direct scalar cells: bank(C)[A] =
// upvalues[B].scalarField(C). The C operand names the destination isa.RegisterKind and
// selects both the upvalueCell field to read and the register bank to write; the four
// scalar banks (int, float, uint, bool) carry no pointers so no write barrier is
// required. Indirect cells, out-of-range indexes, nil cell pointers and non-scalar kinds
// branch to the tier-2 shim with R0 intact, which re-runs the instruction through the Go
// handler.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim, e.g.
// "handlerPathBShimGetUpvalue".
func (*BytecodeARM64Arch) EmitGetUpvalueScalar(e *asmgen.Emitter, shimSymbol string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CURRENT_FRAME(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_UPVALUES_PTR+8)(R4), R5")
	inst5(e, asmarm64.OperationCompare, "R5, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "gu_shim")
	inst5(e, asmarm64.OperationMove64Bits, "CF_UPVALUES_PTR(R4), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R3<<3), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, gu_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CELL_IS_INDIRECT(R6), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R7, gu_shim")
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R7, R7")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	e.Blank()

	inst5(e, asmarm64.OperationCompare, "$REGKIND_INT, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "gu_not_int")
	inst5(e, asmarm64.OperationMove64Bits, "CELL_INT_VALUE(R6), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "gu_done")
	e.Label("gu_not_int")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_FLOAT, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "gu_not_float")
	inst5(e, asmarm64.OperationMove64Bits, "CELL_FLOAT_VALUE(R6), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R24)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "gu_done")
	e.Label("gu_not_float")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_UINT, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "gu_not_uint")
	inst5(e, asmarm64.OperationMove64Bits, "CELL_UINT_VALUE(R6), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R5)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "gu_done")
	e.Label("gu_not_uint")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_BOOL, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "gu_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CELL_BOOL_VALUE(R6), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove8Bits, "R9, (R5)(R3)")
	e.Label("gu_done")
	e.Instruction(macroDispatchNext)
	e.Label("gu_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldSliceLen implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim.
func (*BytecodeARM64Arch) EmitStructFieldSliceLen(e *asmgen.Emitter, shimSymbol string) {
	emitStructFieldCommonPrologueARM64(e, "$23", "", "$16")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R9")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldSliceIndexScalar implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpGetStructFieldSliceIndexScalar: bank(ext2.C)[A] =
// receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide). The slice header is
// read in place through the receiver (no header value materialised, no element copy
// boxed) and the full-width scalar sub-field loads directly into the destination bank.
// The extension words are peeked without advancing R20; only the success path advances
// past them, so shim fallbacks - including out-of-range indexes, which the Go handler
// raises as interpreted panics - re-run the op from the original program counter.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes shimSymbol (string) which is the fallback shim.
func (*BytecodeARM64Arch) EmitStructFieldSliceIndexScalar(e *asmgen.Emitter, shimSymbol string) {
	emitStructFieldCommonPrologueARM64(e, "$23", "", "$16")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R3")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R4")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R4<<2), R5")
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R3, R7")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R7<<3), R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R7")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sf_shim")
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R3, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R4, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R4")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R5, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R4, R4")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R5, R5")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	e.Blank()

	inst5(e, asmarm64.OperationCompare, "$REGKIND_INT, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sfsi_not_int")
	inst5(e, asmarm64.OperationMove64Bits, "(R7), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "sfsi_done")
	e.Label("sfsi_not_int")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_FLOAT, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sfsi_not_float")
	inst5(e, asmarm64.OperationMove64Bits, "(R7), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R24)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "sfsi_done")
	e.Label("sfsi_not_float")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_UINT, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sfsi_not_uint")
	inst5(e, asmarm64.OperationMove64Bits, "(R7), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R4)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "sfsi_done")
	e.Label("sfsi_not_uint")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_BOOL, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R7), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove8Bits, "R9, (R4)(R3)")
	e.Label("sfsi_done")
	inst5(e, asmarm64.OperationAdd, "$2, R20, R20")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// New creates a new bytecode-specific ARM64 architecture adapter, optionally
// pre-populated with initJumpTable entries.
//
// Takes entries ([]JumpTableEntry variadic) which is the flat list of handler-name ->
// byte-offset pairs to install.
//
// Returns *BytecodeARM64Arch ready for use.
func New(entries ...JumpTableEntry) *BytecodeARM64Arch {
	return &BytecodeARM64Arch{jumpTableEntries: entries, ARM64Arch: core.ARM64Arch{}}
}

// emitBackEdgePoll emits the back-edge poll sequence. R0 must be dead at the call site
// because the poll uses it as scratch.
//
// Takes e (*asmgen.Emitter) which receives the emitted back-edge poll instructions.
func emitBackEdgePoll(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_POLL_BUDGET(R19), R0")
	inst5(e, asmarm64.OperationSubtract, "$1, R0, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_POLL_BUDGET(R19)")
	inst5(e, asmarm64.OperationCompare, "$0, R0")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, "poll_ok")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_POLL, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationReturn, "")
	e.Label("poll_ok")
}

// inst emits a tab-indented instruction with mnemonic padded to the given column width.
//
// Takes e (*asmgen.Emitter) which receives the emitted instruction.
// Takes mnemonic (string) which is the instruction mnemonic.
// Takes operands (string) which is the operand string.
// Takes pad (int) which is the column width for mnemonic padding.
func inst(e *asmgen.Emitter, mnemonic, operands string, pad int) {
	padding := max(pad-len(mnemonic), 1)
	e.Instruction(mnemonic + strings.Repeat(" ", padding) + operands)
}

// inst5 emits with default column padding for arm64.
//
// Takes e (*asmgen.Emitter) which receives the emitted instruction.
// Takes mnemonic (string) which is the instruction mnemonic.
// Takes operands (string) which is the operand string.
func inst5(e *asmgen.Emitter, mnemonic, operands string) {
	inst(e, mnemonic, operands, defaultColumnWidth)
}

// emitTypedSliceGetPrologueARM64 emits the common Get-shape prologue for a typed-slice
// umbrella sub-op.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func emitTypedSliceGetPrologueARM64(e *asmgen.Emitter, sliceContextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationMove64Bits, sliceContextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R9, R4, R7")
	inst5(e, asmarm64.OperationAdd, "R7, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R7")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R8")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R8, R8")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R8<<3), R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, labelBoundsFail)
}

// emitTypedSliceSetPrologueARM64 emits the common Set-shape prologue.
//
// For a typed-slice umbrella sub-op of the form slicesX[B][ints[C]] = XBank[ext.A]. After
// execution R6 holds the slice's data pointer, R8 holds the validated element index
// (passed bounds check), and R3 is free for ext-word peeking. Branches to bounds_fail on
// out-of-range access.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func emitTypedSliceSetPrologueARM64(e *asmgen.Emitter, sliceContextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R4<<3), R8")
	inst5(e, asmarm64.OperationMove64Bits, sliceContextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R9, R3, R7")
	inst5(e, asmarm64.OperationAdd, "R7, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, labelBoundsFail)
}

// emitPeekExtensionWordAFieldARM64 emits the sequence that peeks the next instruction
// word and extracts its A field as a uint8 into the destination register.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes destinationRegister (string) which is the register that receives the extracted A
// field byte.
func emitPeekExtensionWordAFieldARM64(e *asmgen.Emitter, destinationRegister string) {
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), "+destinationRegister)
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, "+destinationRegister+", "+destinationRegister)
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, "+destinationRegister+", "+destinationRegister)
}

// emitTypedSliceTailARM64 emits the standard tail for a bounds- checked slice sub-op:
// advance PC past the consumed extension word, tail-call DISPATCH_NEXT, and emit the
// bounds_fail label that branches to the tier-2 fallback symbol.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitTypedSliceTailARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
	e.Label(labelBoundsFail)
	inst5(e, asmarm64.OperationBranch, "·pathBFallback(SB)")
}

// emitTypedSliceByteSliceExtractARM64 extracts dstReg and srcReg from the current
// instruction word into R3 and R4, then loads the ext1 word into R0 ready for the bounds
// + header phases.
//
// Takes e (*asmgen.Emitter) which receives the extract sequence.
func emitTypedSliceByteSliceExtractARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R0")
}

// emitTypedSliceByteSliceLoadAndBoundsARM64 decodes the ext1 fields, loads the source
// slice header, and runs the three-step bounds check. Branches to labelBoundsFail on any
// failure.
//
// Takes e (*asmgen.Emitter) which receives the load and bounds sequence.
// Takes contextOffset (string) which is the offset expression used to address the
// dispatch context.
func emitTypedSliceByteSliceLoadAndBoundsARM64(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R0, R6")
	inst5(e, asmarm64.OperationCompare, "$3, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelBoundsFail)

	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R6, R6")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R7, R7")

	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R6<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R7<<3), R7")

	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R9, R4, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "16(R5), R8")

	inst5(e, asmarm64.OperationCompare, "$0, R6")
	inst5(e, asmarm64.OperationBranchIfLessSigned, labelBoundsFail)
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelBoundsFail)
	inst5(e, asmarm64.OperationCompare, "R8, R7")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelBoundsFail)
}

// emitTypedSliceByteSliceWriteHeaderARM64 computes the destination slot pointer and
// writes the adjusted Data/Len/Cap into the new 24-byte header. Assumes the bounds +
// header values are already in R4 (Data), R6 (low), R7 (high), R8 (Cap), R9 (stride 24),
// and the instruction's dstReg in R3.
//
// Takes e (*asmgen.Emitter) which receives the header-write sequence.
// Takes contextOffset (string) which is the offset expression used to address the
// dispatch context.
func emitTypedSliceByteSliceWriteHeaderARM64(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R4")
	inst5(e, asmarm64.OperationAdd, "R6, R4, R4")
	inst5(e, asmarm64.OperationSubtract, "R6, R7, R7")
	inst5(e, asmarm64.OperationSubtract, "R6, R8, R8")

	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMultiply, "R9, R3, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")

	inst5(e, asmarm64.OperationMove64Bits, "R4, 0(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 16(R5)")
}

// emitTypedSliceSliceSliceWriteHeaderARM64 mirrors
// emitTypedSliceByteSliceWriteHeaderARM64 but shifts the low bound by elementSizeShift
// before adding to the source Data pointer for non- byte typed-slice banks.
//
// Takes e (*asmgen.Emitter) which receives the header-write sequence.
// Takes contextOffset (string) which is the offset expression used to address the
// dispatch context.
// Takes elementSizeShift (uint8) which is the log2 of the element stride.
func emitTypedSliceSliceSliceWriteHeaderARM64(e *asmgen.Emitter, contextOffset string, elementSizeShift uint8) {
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R4")
	if elementSizeShift != 0 {
		inst5(e, asmarm64.OperationLogicalShiftLeft, "$"+shiftLiteralARM64(elementSizeShift)+", R6, R9")
		inst5(e, asmarm64.OperationAdd, "R9, R4, R4")
	} else {
		inst5(e, asmarm64.OperationAdd, "R6, R4, R4")
	}
	inst5(e, asmarm64.OperationSubtract, "R6, R7, R7")
	inst5(e, asmarm64.OperationSubtract, "R6, R8, R8")

	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMultiply, "R9, R3, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")

	inst5(e, asmarm64.OperationMove64Bits, "R4, 0(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 16(R5)")
}

// shiftLiteralARM64 converts a small element-size shift count into its decimal Plan-9
// immediate-operand representation. Mirror of the amd64 helper; centralised here to keep
// the emitter helpers free of strconv usage and stay correct for the shifts the
// typed-slice banks use.
//
// Takes shift (uint8) which is the log2 of the element stride.
//
// Returns the decimal literal as a string.
func shiftLiteralARM64(shift uint8) string {
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

// bankAccess returns the base register and load/store mnemonic for a given register bank.
//
// The generator names the banks needing a different base register or mnemonic and puts
// the integer-bank form in the default, so a new bank emits the common sequence rather
// than nothing. Any mistake here changes emitted assembly, which make
// generate-asmgen-validate compares byte for byte.
//
// Takes bank (asmgen.RegisterBank) which selects the register bank.
//
// Returns base (string) which is the base register name.
// Returns mnemonic (string) which is the load/store instruction mnemonic.
func bankAccess(bank asmgen.RegisterBank) (base, mnemonic string) {
	switch bank {
	case asmgen.RegisterBankFloat:
		return "R24", asmarm64.OperationFloatMove64Bits
	case asmgen.RegisterBankString, asmgen.RegisterBankBoolean, asmgen.RegisterBankUnsignedInteger:
		return "", asmarm64.OperationMove64Bits
	default:
		return "R23", asmarm64.OperationMove64Bits
	}
}

// bankShift returns the shift amount string for indexing into a register bank.
//
// Takes bank (asmgen.RegisterBank) which selects the register bank.
//
// Returns string which is the shift amount for address computation.
func bankShift(bank asmgen.RegisterBank) string {
	switch bank {
	case asmgen.RegisterBankString:
		return "4"
	default:
		return "3"
	}
}

// floatConditionCode maps abstract condition names to arm64 CSET condition codes that are
// NaN-safe after FCMPD.
//
// Takes condition (string) which is the abstract condition name.
//
// Returns string which is the arm64 condition code.
func floatConditionCode(condition string) string {
	switch condition {
	case conditionEQ:
		return conditionEQ
	case conditionNE:
		return conditionNE
	case "LT":
		return "MI"
	case "LE":
		return "LS"
	case "GT":
		return "GT"
	case "GE":
		return "GE"
	default:
		return condition
	}
}

// intOpMnemonic maps an abstract integer operation name to its arm64 mnemonic.
//
// Takes op (string) which is the abstract operation name.
//
// Returns string which is the arm64 instruction mnemonic.
func intOpMnemonic(op string) string {
	switch op {
	case asmarm64.OperationAdd:
		return asmarm64.OperationAdd
	case asmarm64.OperationSubtract:
		return asmarm64.OperationSubtract
	case asmarm64.OperationMultiply:
		return asmarm64.OperationMultiply
	case asmarm64.OperationBitwiseAnd:
		return asmarm64.OperationBitwiseAnd
	case "OR":
		return asmarm64.OperationBitwiseOr
	case "XOR":
		return asmarm64.OperationExclusiveOr
	case "ANDNOT":
		return asmarm64.OperationBitwiseAndNot
	default:
		return op
	}
}

// floatOpMnemonic maps an abstract float operation name to its arm64 mnemonic.
//
// Takes op (string) which is the abstract operation name.
//
// Returns string which is the arm64 instruction mnemonic.
func floatOpMnemonic(op string) string {
	switch op {
	case asmarm64.OperationAdd:
		return asmarm64.OperationFloatAddScalarDouble
	case asmarm64.OperationSubtract:
		return asmarm64.OperationFloatSubtractScalarDouble
	case asmarm64.OperationMultiply:
		return asmarm64.OperationFloatMultiplyScalarDouble
	case "DIV":
		return asmarm64.OperationFloatDivideScalarDouble
	default:
		return "F" + op + "D"
	}
}
