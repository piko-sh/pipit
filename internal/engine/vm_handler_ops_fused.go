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

import "pipit.sh/pipit/internal/isa"

// handleEqIntConstJumpFalse compares a register against an integer constant and branches
// when equality is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqIntConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] != constantValue)
}

// handleEqIntConstJumpTrue compares a register against an integer constant and branches
// when equality is true.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqIntConstJumpTrue(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] == constantValue)
}

// handleGeIntConstJumpFalse compares a register against an integer constant and branches
// when the greater-or-equal condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleGeIntConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] < constantValue)
}

// handleGtIntConstJumpFalse compares a register against an integer constant and branches
// when the greater-than condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleGtIntConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] <= constantValue)
}

// handleAddIntJump adds an integer constant to a register value and then unconditionally
// branches by the extension word offset.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleAddIntJump(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] + frame.Function.IntConstants[instruction.C]
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
	frame.ProgramCounter += int(offset)
	return opContinue
}

// handleIncIntJumpLt increments a signed integer register and branches if the result is
// less than a comparison register.
//
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target and comparison register
// indices.
//
// Returns OpResult indicating the next execution step.
func handleIncIntJumpLt(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A]++
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	if registers.Ints[instruction.A] < registers.Ints[instruction.B] {
		offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleLenStringLtJumpFalse fuses a len(string) < int comparison with a conditional
// jump.
//
// Jumps if ints[A] >= len(strings[B]), i.e. when the for-loop condition `i < len(s)` is
// false.
//
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which provides the integer and string register banks.
// Takes instruction (instruction) which encodes the counter and string register indices.
//
// Returns OpResult indicating the next execution step.
func handleLenStringLtJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	if registers.Ints[instruction.A] >= int64(len(registers.Strings[instruction.B])) {
		offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleMulIntConst multiplies a register value by an integer constant from the function
// constant pool.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleMulIntConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] * frame.Function.IntConstants[instruction.C]
	return opContinue
}

// handleEqStringConstJumpFalse compares a string register against a string constant and
// branches when equality is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which provides the string register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqStringConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := stringConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Strings[instruction.B] != constantValue)
}

// handleMoveIntToGeneral boxes a signed integer register value into a reflect.Value in a
// general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the integer and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveIntToGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = boxInt64ToGeneral(vm.Arena, registers.Ints[instruction.B])
	return opContinue
}

// handleMoveGeneralToInt unboxes an integer from a general register reflect.Value into a
// signed integer register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveGeneralToInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).Int()
	return opContinue
}

// handleMoveFloatToGeneral boxes a floating-point register value into a reflect.Value in
// a general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the float and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveFloatToGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = boxFloat64ToGeneral(vm.Arena, registers.Floats[instruction.B])
	return opContinue
}

// handleMoveGeneralToFloat unboxes a float from a general register reflect.Value into a
// float register.
//
// Takes registers (*Registers) which provides the general and float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveGeneralToFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).Float()
	return opContinue
}

// handleMoveStringToGeneral boxes a string register value into a reflect.Value in a
// general register.
//
// Takes vm (*VM) which provides the arena allocator for the string box slab; the packed
// string stays arena-backed until the store barrier detaches it on escape.
// Takes registers (*Registers) which provides the string and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveStringToGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = BoxStringToGeneral(vm.Arena, registers.Strings[instruction.B])
	return opContinue
}

// handleMoveGeneralToString unboxes a string from a general register reflect.Value into a
// string register.
//
// Takes registers (*Registers) which provides the general and string register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveGeneralToString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).String()
	return opContinue
}

// handleTestNilJumpTrue tests whether a general register holds nil and branches if the
// value is nil or invalid.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and branch offset
// operands.
//
// Returns OpResult indicating the next execution step.
func handleTestNilJumpTrue(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	offset := instruction.SignedOffset()
	if !v.IsValid() || isNilableAndNil(v) {
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleTestNilJumpFalse tests whether a general register holds nil and branches if the
// value is non-nil and valid.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and branch offset
// operands.
//
// Returns OpResult indicating the next execution step.
func handleTestNilJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	offset := instruction.SignedOffset()
	if v.IsValid() && !isNilableAndNil(v) {
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}
