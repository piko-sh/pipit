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

// handleEqualUint64ThenJumpIfFalse compares two uint registers and branches by the
// extension-word offset when they differ.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the uint operands.
// Takes instruction (isa.Instruction) which encodes the operand registers in B and C.
//
// Returns OpResult indicating the next execution step.
func handleEqualUint64ThenJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Uints[instruction.B] != registers.Uints[instruction.C])
}

// handleNotEqualUint64ThenJumpIfFalse compares two uint registers and branches by the
// extension-word offset when they are equal.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the uint operands.
// Takes instruction (isa.Instruction) which encodes the operand registers in B and C.
//
// Returns OpResult indicating the next execution step.
func handleNotEqualUint64ThenJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Uints[instruction.B] == registers.Uints[instruction.C])
}

// handleIncrementInt64ThenJumpIfLessOrEqual increments ints[B] and jumps when ints[B] <=
// ints[C].
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the counter and its bound.
// Takes instruction (isa.Instruction) which encodes the counter in B and the bound in C.
//
// Returns OpResult indicating the next execution step.
func handleIncrementInt64ThenJumpIfLessOrEqual(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.B]++
	return conditionalJump(frame, registers.Ints[instruction.B] <= registers.Ints[instruction.C])
}

// handleJumpIfFalseBool branches by the extension-word offset when bools[C] is false.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the condition.
// Takes instruction (isa.Instruction) which encodes the condition's register in C.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfFalseBool(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, !registers.Bools[instruction.C])
}

// handleJumpIfTrueBool branches by the extension-word offset when bools[C] is true.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the condition.
// Takes instruction (isa.Instruction) which encodes the condition's register in C.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfTrueBool(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Bools[instruction.C])
}
