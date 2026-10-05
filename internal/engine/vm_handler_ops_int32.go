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
	"pipit.sh/pipit/internal/isa"
)

// The handlers in this file fuse an integer operation with the 32-bit truncation of its
// result, which Go's int32 and uint32 arithmetic needs after every step. Each one stores
// exactly what the operation followed by isa.OpTruncateNarrow would have stored.

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

// handleSubInt32 sets ints[A] = int64(int32(ints[B] - ints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleSubInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] - registers.Ints[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleMulInt32 sets ints[A] = int64(int32(ints[B] * ints[C])).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination and operands.
//
// Returns OpResult indicating the next execution step.
func handleMulInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] * registers.Ints[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleAddInt32Const sets ints[A] = int64(int32(ints[B] + intConstants[C])).
//
// Takes vm (*VM) which reports a constant index out of range.
// Takes frame (*CallFrame) which provides the constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination, source and pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleAddInt32Const(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] + frame.Function.IntConstants[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleSubInt32Const sets ints[A] = int64(int32(ints[B] - intConstants[C])).
//
// Takes vm (*VM) which reports a constant index out of range.
// Takes frame (*CallFrame) which provides the constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the destination, source and pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleSubInt32Const(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.B] - frame.Function.IntConstants[instruction.C])) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleIncInt32 sets ints[A] = int64(int32(ints[A] + 1)).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the register in A.
//
// Returns OpResult indicating the next execution step.
func handleIncInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(int32(registers.Ints[instruction.A] + 1)) //nolint:gosec // int32 wrap is the semantics
	return opContinue
}

// handleDecInt32 sets ints[A] = int64(int32(ints[A] - 1)).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the register in A.
//
// Returns OpResult indicating the next execution step.
func handleDecInt32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
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
