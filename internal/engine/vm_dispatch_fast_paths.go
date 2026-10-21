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

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
)

// branchOffsetSignShift is the shift used to sign-extend a 16-bit branch offset.
const branchOffsetSignShift = 48

// stepOverBranch steps over a compare-and-jump's extension word and takes the jump it
// encodes when taken is set.
//
// Takes frame (*CallFrame) whose program counter is on the extension word.
// Takes taken (bool) which is true when the jump is taken.
func stepOverBranch(frame *CallFrame, taken bool) {
	programCounter := frame.ProgramCounter
	extensionWord := frame.Function.Body[programCounter]
	programCounter++
	if taken {
		programCounter += int((int64(extensionWord.A) | int64(extensionWord.B)<<isa.WideBitShift) << branchOffsetSignShift >> branchOffsetSignShift)
	}
	frame.ProgramCounter = programCounter
}

// fastGetSliceByteElementInt64 handles an in-bounds byte read into the int bank.
//
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastGetSliceByteElementInt64(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	slice := registers.slicesByte[instruction.C]
	index := registers.Ints[readExtensionWord(frame).A]
	if index < 0 || index >= int64(len(slice)) {
		return false
	}
	frame.ProgramCounter++
	registers.Ints[instruction.B] = int64(slice[index])
	return true
}

// fastGetSliceByteElement handles an in-bounds byte read into the uint bank.
//
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastGetSliceByteElement(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	slice := registers.slicesByte[instruction.C]
	index := registers.Ints[readExtensionWord(frame).A]
	if index < 0 || index >= int64(len(slice)) {
		return false
	}
	frame.ProgramCounter++
	registers.Uints[instruction.B] = uint64(slice[index])
	return true
}

// fastSetSliceByteElement handles an in-bounds byte store.
//
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastSetSliceByteElement(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	extensionWord := readExtensionWord(frame)
	slice := registers.slicesByte[instruction.B]
	index := registers.Ints[instruction.C] + int64(extensionWord.B)
	if index < 0 || index >= int64(len(slice)) {
		return false
	}
	frame.ProgramCounter++
	slice[index] = safeconv.Uint64ToByteTruncate(registers.Uints[extensionWord.A])
	return true
}

// fastMultiplyInt64ConstantThenAdd handles multiply-add with a valid constant index.
//
// Takes frame (*CallFrame) which supplies the constants and the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastMultiplyInt64ConstantThenAdd(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	addend := registers.Ints[readExtensionWord(frame).A]
	frame.ProgramCounter++
	registers.Ints[instruction.A] = addend + registers.Ints[instruction.B]*constants[instruction.C]
	return true
}

// fastBitAndUint64Constant applies a uint mask with a valid constant index.
//
// Takes frame (*CallFrame) which supplies the constants and the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastBitAndUint64Constant(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	extensionWord := readExtensionWord(frame)
	constants := frame.Function.UintConstants
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if index >= len(constants) {
		return false
	}
	frame.ProgramCounter++
	registers.Uints[instruction.B] = registers.Uints[instruction.C] & constants[index]
	return true
}

// fastLoadZero zeroes register B in the scalar bank C names.
//
// Takes registers (*Registers) which holds the destination.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for a bank that holds pointers.
func fastLoadZero(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt64:
		registers.Ints[instruction.B] = 0
	case isa.RegisterUint64:
		registers.Uints[instruction.B] = 0
	case isa.RegisterFloat64:
		registers.Floats[instruction.B] = 0
	case isa.RegisterBool:
		registers.Bools[instruction.B] = false
	default:
		return false
	}
	return true
}

// fastIncrementInt64ThenJumpIfLessThanConstant increments and tests a constant bound.
//
// Takes frame (*CallFrame) which supplies the constants and the jump.
// Takes registers (*Registers) which holds the counter.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastIncrementInt64ThenJumpIfLessThanConstant(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	registers.Ints[instruction.B]++
	stepOverBranch(frame, registers.Ints[instruction.B] < constants[instruction.C])
	return true
}

// fastLessThanInt64ConstantThenJumpIfFalse compares an int with a constant and branches.
//
// Takes frame (*CallFrame) which supplies the constants and the jump.
// Takes registers (*Registers) which holds the operand.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastLessThanInt64ConstantThenJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	stepOverBranch(frame, registers.Ints[instruction.B] >= constants[instruction.C])
	return true
}

// fastGreaterOrEqualInt64ConstantThenJumpIfFalse compares an int with a constant and
// branches.
//
// Takes frame (*CallFrame) which supplies the constants and the jump.
// Takes registers (*Registers) which holds the operand.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastGreaterOrEqualInt64ConstantThenJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	stepOverBranch(frame, registers.Ints[instruction.B] < constants[instruction.C])
	return true
}

// fastEqualInt64ConstantThenJumpIfFalse compares an int with a constant and branches.
//
// Takes frame (*CallFrame) which supplies the constants and the jump.
// Takes registers (*Registers) which holds the operand.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastEqualInt64ConstantThenJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	stepOverBranch(frame, registers.Ints[instruction.B] != constants[instruction.C])
	return true
}

// fastEqualInt64ConstantThenJumpIfTrue compares an int with a constant and branches.
//
// Takes frame (*CallFrame) which supplies the constants and the jump.
// Takes registers (*Registers) which holds the operand.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastEqualInt64ConstantThenJumpIfTrue(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	constants := frame.Function.IntConstants
	if int(instruction.C) >= len(constants) {
		return false
	}
	stepOverBranch(frame, registers.Ints[instruction.B] == constants[instruction.C])
	return true
}

// fastGetGlobal reads an unshared integer or general global of a source-compiled
// function.
//
// Takes vm (*VM) which supplies the global store.
// Takes frame (*CallFrame) which supplies the function.
// Takes registers (*Registers) which holds the destination.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for a shared store, a function loaded from a bundle (its
// globals are offset), another bank, or an index outside the bank.
func fastGetGlobal(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	globals := vm.Globals
	if globals.shared.Load() || frame.Function.GlobalBases != nil {
		return false
	}
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt64:
		if int(instruction.B) >= len(globals.ints) {
			return false
		}
		registers.Ints[instruction.A] = globals.ints[instruction.B]
	case isa.RegisterGeneral:
		if int(instruction.B) >= len(globals.general) {
			return false
		}
		registers.General[instruction.A] = globals.general[instruction.B]
	default:
		return false
	}
	return true
}

// fastGetGlobalWide reads an integer global at a wide index under fastGetGlobal's
// conditions.
//
// Takes vm (*VM) which supplies the global store.
// Takes frame (*CallFrame) which supplies the function and the extension word.
// Takes registers (*Registers) which holds the destination.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for a shared store, a function loaded from a bundle,
// another bank, or an index outside the bank.
func fastGetGlobalWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	globals := vm.Globals
	extensionWord := readExtensionWord(frame)
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if globals.shared.Load() || frame.Function.GlobalBases != nil ||
		isa.RegisterKind(instruction.C) != isa.RegisterInt64 || index >= len(globals.ints) {
		return false
	}
	frame.ProgramCounter++
	registers.Ints[instruction.B] = globals.ints[index]
	return true
}

// fastSetGlobal writes an unshared integer global of a source-compiled function.
//
// Takes vm (*VM) which supplies the global store.
// Takes frame (*CallFrame) which supplies the function.
// Takes registers (*Registers) which holds the value.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for a shared store, a function loaded from a bundle,
// another bank, or an index outside the bank.
func fastSetGlobal(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	globals := vm.Globals
	if globals.shared.Load() || frame.Function.GlobalBases != nil ||
		isa.RegisterKind(instruction.C) != isa.RegisterInt64 || int(instruction.B) >= len(globals.ints) {
		return false
	}
	globals.ints[instruction.B] = registers.Ints[instruction.A]
	return true
}

// fastSetGlobalWide writes an integer global at a wide index under fastSetGlobal's
// conditions.
//
// Takes vm (*VM) which supplies the global store.
// Takes frame (*CallFrame) which supplies the function and the extension word.
// Takes registers (*Registers) which holds the value.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false where fastSetGlobal's is.
func fastSetGlobalWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	globals := vm.Globals
	extensionWord := readExtensionWord(frame)
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if globals.shared.Load() || frame.Function.GlobalBases != nil ||
		isa.RegisterKind(instruction.C) != isa.RegisterInt64 || index >= len(globals.ints) {
		return false
	}
	frame.ProgramCounter++
	globals.ints[index] = registers.Ints[instruction.B]
	return true
}

// fastMoveGeneral copies a general register when C is MoveGeneralModeAlias.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for a move that may have to copy the value.
func fastMoveGeneral(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	if instruction.C != MoveGeneralModeAlias {
		return false
	}
	registers.General[instruction.A] = registers.General[instruction.B]
	return true
}

// fastIncrementInt64ThenJumpIfLessThan increments ints[B] and jumps when ints[B] <
// ints[C].
//
// Takes frame (*CallFrame) which supplies the jump.
// Takes registers (*Registers) which holds the counter and its bound.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is always true.
func fastIncrementInt64ThenJumpIfLessThan(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	registers.Ints[instruction.B]++
	stepOverBranch(frame, registers.Ints[instruction.B] < registers.Ints[instruction.C])
	return true
}

// fastAddUint64Constant adds a uint constant with a valid pool index.
//
// Takes frame (*CallFrame) which supplies the constants and the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastAddUint64Constant(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	extensionWord := readExtensionWord(frame)
	constants := frame.Function.UintConstants
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if index >= len(constants) {
		return false
	}
	frame.ProgramCounter++
	registers.Uints[instruction.B] = registers.Uints[instruction.C] + constants[index]
	return true
}

// fastSubtractUint64Constant subtracts a uint constant with a valid pool index.
//
// Takes frame (*CallFrame) which supplies the constants and the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastSubtractUint64Constant(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	extensionWord := readExtensionWord(frame)
	constants := frame.Function.UintConstants
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if index >= len(constants) {
		return false
	}
	frame.ProgramCounter++
	registers.Uints[instruction.B] = registers.Uints[instruction.C] - constants[index]
	return true
}

// fastAddUint32Constant adds a constant and truncates the result to uint32.
//
// Takes frame (*CallFrame) which supplies the constants and the extension word.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the constant index is outside the pool.
func fastAddUint32Constant(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	extensionWord := readExtensionWord(frame)
	constants := frame.Function.UintConstants
	index := int(extensionWord.A) | int(extensionWord.B)<<isa.WideBitShift
	if index >= len(constants) {
		return false
	}
	frame.ProgramCounter++
	registers.Uints[instruction.B] = (registers.Uints[instruction.C] + constants[index]) & math.MaxUint32
	return true
}

// fastGetStringByte handles an in-bounds string-byte read.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastGetStringByte(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	text := registers.Strings[instruction.B]
	index := registers.Ints[instruction.C]
	if index < 0 || index >= int64(len(text)) {
		return false
	}
	registers.Uints[instruction.A] = uint64(text[index])
	return true
}

// fastGetSliceInt64Element handles an in-bounds int-slice read.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastGetSliceInt64Element(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	slice := registers.SlicesInt[instruction.B]
	index := registers.Ints[instruction.C]
	if index < 0 || index >= int64(len(slice)) {
		return false
	}
	registers.Ints[instruction.A] = slice[index]
	return true
}

// fastSetSliceInt64Element handles an in-bounds int-slice store.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false when the index is out of range.
func fastSetSliceInt64Element(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	slice := registers.SlicesInt[instruction.A]
	index := registers.Ints[instruction.B]
	if index < 0 || index >= int64(len(slice)) {
		return false
	}
	slice[index] = registers.Ints[instruction.C]
	return true
}

// fastGetElementUint64 reads an in-bounds byte from a general-register slice into the
// uint bank.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for any other collection and for an index out of range.
func fastGetElementUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	bytes := byteSliceElements(&registers.General[instruction.B])
	index := registers.Ints[instruction.C]
	if index < 0 || index >= int64(len(bytes)) {
		return false
	}
	registers.Uints[instruction.A] = uint64(bytes[index])
	return true
}

// fastGetElementInt64 reads an in-bounds byte from a general-register slice into the int
// bank.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for any other collection and for an index out of range.
func fastGetElementInt64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	bytes := byteSliceElements(&registers.General[instruction.B])
	index := registers.Ints[instruction.C]
	if index < 0 || index >= int64(len(bytes)) {
		return false
	}
	registers.Ints[instruction.A] = int64(bytes[index])
	return true
}

// fastSetElementUint64 handles an in-bounds byte store into a general-register slice.
//
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which is the instruction word.
//
// Returns bool which is false for any other collection, for one that may not be written
// through and for an index out of range.
func fastSetElementUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) bool {
	bytes := writableByteSliceElements(&registers.General[instruction.A])
	index := registers.Ints[instruction.B]
	if index < 0 || index >= int64(len(bytes)) {
		return false
	}
	bytes[index] = safeconv.Uint64ToByteTruncate(registers.Uints[instruction.C])
	return true
}
