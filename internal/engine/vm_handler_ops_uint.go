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

// handleMoveUint copies an unsigned integer value between virtual machine registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B]
	return opContinue
}

// handleLoadUintConst loads an unsigned integer constant from the function constant pool
// into a register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadUintConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.UintConstants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, int(index), len(frame.Function.UintConstants))
		return opPanicError
	}
	registers.Uints[instruction.A] = frame.Function.UintConstants[index]
	return opContinue
}

// handleAddUint performs unsigned integer addition of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] + registers.Uints[instruction.C]
	return opContinue
}

// handleSubUint performs unsigned integer subtraction of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSubUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] - registers.Uints[instruction.C]
	return opContinue
}

// handleMulUint performs unsigned integer multiplication of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMulUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] * registers.Uints[instruction.C]
	return opContinue
}

// handleDivUint performs unsigned integer division of two register operands in the
// virtual machine.
//
// When the divisor is zero, raises an interpreted divide-by-zero panic via
// raiseNativePanicAsInterpreted instead of continuing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleDivUint(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Uints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] / divisor
	return opContinue
}

// handleRemUint computes the unsigned integer remainder of two register operands in the
// virtual machine.
//
// When the divisor is zero, raises an interpreted divide-by-zero panic via
// raiseNativePanicAsInterpreted instead of continuing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleRemUint(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Uints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] % divisor
	return opContinue
}

// handleBitAndUint performs a bitwise AND of two unsigned integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAndUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] & registers.Uints[instruction.C]
	return opContinue
}

// readExtensionWideIndex consumes the following isa.OpExt word and returns the 16-bit
// payload packed in its first two operand bytes.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
//
// Returns the uint16 payload assembled from the extension's A and B bytes.
func readExtensionWideIndex(frame *CallFrame) uint16 {
	ext := readExtensionWord(frame)
	frame.ProgramCounter++
	return uint16(ext.A) | (uint16(ext.B) << extensionWideIndexHighByteShift)
}

// handleAddUintConst adds a uint constant pool entry to a uint register.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleAddUintConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constIdx := readExtensionWideIndex(frame)
	if int(constIdx) >= len(frame.Function.UintConstants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, int(constIdx), len(frame.Function.UintConstants))
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] + frame.Function.UintConstants[constIdx]
	return opContinue
}

// handleSubUintConst subtracts a uint constant pool entry from a uint register.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleSubUintConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constIdx := readExtensionWideIndex(frame)
	if int(constIdx) >= len(frame.Function.UintConstants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, int(constIdx), len(frame.Function.UintConstants))
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] - frame.Function.UintConstants[constIdx]
	return opContinue
}

// handleBitAndUintConst bitwise-ANDs a uint register with a uint constant pool entry.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the bytecode body and constant pool.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand slots.
//
// Returns OpResult indicating the next execution step.
func handleBitAndUintConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constIdx := readExtensionWideIndex(frame)
	if int(constIdx) >= len(frame.Function.UintConstants) {
		vMBoundsError(vm, frame, boundsTableUintConstant, int(constIdx), len(frame.Function.UintConstants))
		return opPanicError
	}
	registers.Uints[instruction.A] = registers.Uints[instruction.B] & frame.Function.UintConstants[constIdx]
	return opContinue
}

// handleBitOrUint performs a bitwise OR of two unsigned integer register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitOrUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] | registers.Uints[instruction.C]
	return opContinue
}

// handleBitXorUint performs a bitwise XOR of two unsigned integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitXorUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] ^ registers.Uints[instruction.C]
	return opContinue
}

// handleBitAndNotUint performs a bitwise AND NOT of two unsigned integer register
// operands in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAndNotUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] &^ registers.Uints[instruction.C]
	return opContinue
}

// handleBitNotUint performs a bitwise complement of an unsigned integer register value in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitNotUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = ^registers.Uints[instruction.B]
	return opContinue
}

// handleShiftLeftUint performs a left bit shift of an unsigned integer register by the
// amount in another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftLeftUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] << registers.Uints[instruction.C]
	return opContinue
}

// handleShiftRightUint performs a right bit shift of an unsigned integer register by the
// amount in another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftRightUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = registers.Uints[instruction.B] >> registers.Uints[instruction.C]
	return opContinue
}

// handleEqUint tests equality of two unsigned integer register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] == registers.Uints[instruction.C])
	return opContinue
}

// handleNeUint tests inequality of two unsigned integer register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] != registers.Uints[instruction.C])
	return opContinue
}

// handleLtUint tests whether the first unsigned integer register is less than the second
// and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLtUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] < registers.Uints[instruction.C])
	return opContinue
}

// handleLeUint tests whether the first unsigned integer register is less than or equal to
// the second.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLeUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] <= registers.Uints[instruction.C])
	return opContinue
}

// handleGtUint tests whether the first unsigned integer register is greater than the
// second and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGtUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] > registers.Uints[instruction.C])
	return opContinue
}

// handleGeUint tests whether the first unsigned integer register is greater than or equal
// to the second.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGeUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Uints[instruction.B] >= registers.Uints[instruction.C])
	return opContinue
}

// handleIncUint increments an unsigned integer register value by one in the virtual
// machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleIncUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A]++
	return opContinue
}

// handleDecUint decrements an unsigned integer register value by one in the virtual
// machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleDecUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A]--
	return opContinue
}
