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

	"pipit.sh/pipit/internal/isa"
)

// handleAddInt32 sets ints[A] = int64(int32(ints[B] + ints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleAddInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] + registers.Ints[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleSubtractInt32 sets ints[A] = int64(int32(ints[B] - ints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleSubtractInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] - registers.Ints[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleMultiplyInt32 sets ints[A] = int64(int32(ints[B] * ints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleMultiplyInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] * registers.Ints[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleAddInt32Constant sets ints[A] = int64(int32(ints[B] + intConstants[C])).
//
// Takes vm (*VM) which reports a constant index out of range.
// Takes frame (*CallFrame) which provides the constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination, source and pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleAddInt32Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] + frame.Function.IntConstants[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleSubtractInt32Constant sets ints[A] = int64(int32(ints[B] - intConstants[C])).
//
// Takes vm (*VM) which reports a constant index out of range.
// Takes frame (*CallFrame) which provides the constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination, source and pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleSubtractInt32Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] - frame.Function.IntConstants[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleIncrementInt32Body sets ints[A] = int64(int32(ints[A] + 1)).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the register in A.
//
// Returns OpResult indicating the next execution step.
func handleIncrementInt32Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.A] + 1)) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleDecrementInt32Body sets ints[A] = int64(int32(ints[A] - 1)).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the register in A.
//
// Returns OpResult indicating the next execution step.
func handleDecrementInt32Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.A] - 1)) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleAddUint32 sets uints[A] = uint64(uint32(uints[B] + uints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleAddUint32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = uint64(uint32(registers.Uints[instruction.B] + registers.Uints[instruction.C])) //nolint:gosec // uint32 wrap is the semantics
	return opContinue
}

// handleSubtractUint32 sets uints[A] = uint64(uint32(uints[B] - uints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleSubtractUint32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = (registers.Uints[instruction.B] - registers.Uints[instruction.C]) & math.MaxUint32
	return opContinue
}

// handleMultiplyUint32 sets uints[A] = uint64(uint32(uints[B] * uints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleMultiplyUint32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = (registers.Uints[instruction.B] * registers.Uints[instruction.C]) & math.MaxUint32
	return opContinue
}

// handleAddUint32Constant sets uints[B] = uint64(uint32(uints[C] + uintConstants[ext])),
// the constant's index being in the extension word.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which holds the function constants and program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which is the tier-1 word, its operands in B and C.
//
// Returns OpResult indicating the next execution step.
func handleAddUint32Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constant, ok := uintConstantOfExtension(vm, frame)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.B] = (registers.Uints[instruction.C] + constant) & math.MaxUint32
	return opContinue
}

// handleSubtractUint32Constant sets uints[B] = uint64(uint32(uints[C] -
// uintConstants[ext])), the constant's index being in the extension word.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which holds the function constants and program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which is the tier-1 word, its operands in B and C.
//
// Returns OpResult indicating the next execution step.
func handleSubtractUint32Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constant, ok := uintConstantOfExtension(vm, frame)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.B] = (registers.Uints[instruction.C] - constant) & math.MaxUint32
	return opContinue
}
