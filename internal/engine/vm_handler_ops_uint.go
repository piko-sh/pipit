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

// handleMoveUint64Body copies an unsigned integer value between virtual machine
// registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveUint64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B]
	return opContinue
}

// handleLoadUint64Constant loads an unsigned integer constant from the function constant
// pool into a register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadUint64Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.UintConstants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, int(index), len(frame.Function.UintConstants))
		return opPanicError
	}
	registers.Uints[instruction.A] = frame.Function.UintConstants[index]
	return opContinue
}

// handleAddUint64 performs unsigned integer addition of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] + registers.Uints[instruction.C]
	return opContinue
}

// handleSubtractUint64 performs unsigned integer subtraction of two register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSubtractUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] - registers.Uints[instruction.C]
	return opContinue
}

// handleMultiplyUint64 performs unsigned integer multiplication of two register operands
// in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMultiplyUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] * registers.Uints[instruction.C]
	return opContinue
}

// handleDivideUint64 performs unsigned integer division of two register operands in the
// virtual machine.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleDivideUint64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Uints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] / divisor
	return opContinue
}

// handleRemainderUint64 computes the unsigned integer remainder of two register operands
// in the virtual machine.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleRemainderUint64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Uints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] % divisor
	return opContinue
}

// handleBitAndUint64 performs a bitwise AND of two unsigned integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAndUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] & registers.Uints[instruction.C]
	return opContinue
}

// readExtensionWideIndex consumes the following isa.OpExtension word and returns the
// 16-bit payload packed in its first two operand bytes.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
//
// Returns the uint16 payload assembled from the extension's A and B bytes.
func readExtensionWideIndex(frame *CallFrame) uint16 {
	ext := readExtensionWord(frame)
	frame.ProgramCounter++
	return uint16(ext.A) | (uint16(ext.B) << extensionWideIndexHighByteShift)
}

// uintConstantOfExtension steps over the extension word and returns the uint constant it
// indexes, raising a bounds error for an index outside the pool.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which holds the function constants and program counter.
//
// Returns uint64 which is the constant.
// Returns bool which is false when the index is outside the pool and the error is raised.
func uintConstantOfExtension(vm *VM, frame *CallFrame) (uint64, bool) {
	index := int(readExtensionWideIndex(frame))
	constants := frame.Function.UintConstants
	if index >= len(constants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, index, len(constants))
		return 0, false
	}
	return constants[index], true
}

// handleAddUint64ConstantBody adds a uint constant pool entry to a uint register.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleAddUint64ConstantBody(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constant, ok := uintConstantOfExtension(vm, frame)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] + constant
	return opContinue
}

// handleSubtractUint64ConstantBody subtracts a uint constant pool entry from a uint
// register.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleSubtractUint64ConstantBody(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constant, ok := uintConstantOfExtension(vm, frame)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] - constant
	return opContinue
}

// handleBitAndUint64ConstantBody bitwise-ANDs a uint register with a uint constant pool
// entry.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleBitAndUint64ConstantBody(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constant, ok := uintConstantOfExtension(vm, frame)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] & constant
	return opContinue
}

// handleBitOrUint64 performs a bitwise OR of two unsigned integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitOrUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] | registers.Uints[instruction.C]
	return opContinue
}

// handleBitXorUint64 performs a bitwise XOR of two unsigned integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitXorUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] ^ registers.Uints[instruction.C]
	return opContinue
}

// handleBitAndNotUint64 performs a bitwise AND NOT of two unsigned integer register
// operands in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAndNotUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] &^ registers.Uints[instruction.C]
	return opContinue
}

// handleBitNotUint64Body performs a bitwise complement of an unsigned integer register
// value in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitNotUint64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = ^registers.Uints[instruction.B]
	return opContinue
}

// handleShiftLeftUint64 performs a left bit shift of an unsigned integer register by the
// amount in another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftLeftUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] << registers.Uints[instruction.C]
	return opContinue
}

// handleShiftRightUint64 performs a right bit shift of an unsigned integer register by
// the amount in another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftRightUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] >> registers.Uints[instruction.C]
	return opContinue
}

// handleEqualUint64 tests equality of two unsigned integer register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqualUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] == registers.Uints[instruction.C])
	return opContinue
}

// handleNotEqualUint64 tests inequality of two unsigned integer register values and
// stores the boolean result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNotEqualUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] != registers.Uints[instruction.C])
	return opContinue
}

// handleLessThanUint64 tests whether the first unsigned integer register is less than the
// second and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLessThanUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] < registers.Uints[instruction.C])
	return opContinue
}

// handleLessOrEqualUint64 tests whether the first unsigned integer register is less than
// or equal to the second.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLessOrEqualUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] <= registers.Uints[instruction.C])
	return opContinue
}

// handleGreaterThanUint64 tests whether the first unsigned integer register is greater
// than the second and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGreaterThanUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] > registers.Uints[instruction.C])
	return opContinue
}

// handleGreaterOrEqualUint64 tests whether the first unsigned integer register is greater
// than or equal to the second.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGreaterOrEqualUint64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] >= registers.Uints[instruction.C])
	return opContinue
}

// handleIncrementUint64Body increments an unsigned integer register value by one in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleIncrementUint64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A]++
	return opContinue
}

// handleDecrementUint64Body decrements an unsigned integer register value by one in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleDecrementUint64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A]--
	return opContinue
}
