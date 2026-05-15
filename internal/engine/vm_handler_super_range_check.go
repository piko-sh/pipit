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

const (
	// RangeCheckUintFusionNopCount is the trailing-nop count after a fused range-check
	// pattern's two extension words.
	RangeCheckUintFusionNopCount = isa.RangeCheckUintFusionNopCount

	// eqUintConstJumpFalseNopCount is the trailing-nop count for the equality-const fusion.
	eqUintConstJumpFalseNopCount = isa.EqUintConstJumpFalseNopCount
)

// handleSubOpRangeCheckUintJumpFalse implements the fused range-check super-instruction
// that collapses `value >= lo && value <= hi` into a single dispatch.
//
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which provides the uint register bank.
// Takes instr (instruction) which encodes the value register index in its C field.
//
// Returns OpResult indicating the next execution step.
func handleSubOpRangeCheckUintJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	ext1 := readExtensionWord(frame)
	frame.ProgramCounter++
	ext2 := readExtensionWord(frame)
	frame.ProgramCounter++
	frame.ProgramCounter += RangeCheckUintFusionNopCount
	value := registers.Uints[instr.C]
	lo := uint64(ext1.A)
	hi := uint64(ext1.B)
	if value < lo || value > hi {
		offset := isa.JoinOffset(ext2.A, ext2.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleSubOpEqUintConstJumpFalse implements the equality-and-jump fusion that collapses
// `LoadUintConstSmall + EqUint + JumpIfFalse` into a single dispatch.
//
// Takes frame (*CallFrame) which provides the bytecode body and program counter.
// Takes registers (*Registers) which provides the uint register bank.
// Takes instr (instruction) which encodes the value register in B and the immediate
// constant in C.
//
// Returns OpResult indicating the next execution step.
func handleSubOpEqUintConstJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	ext1 := readExtensionWord(frame)
	frame.ProgramCounter++
	frame.ProgramCounter += eqUintConstJumpFalseNopCount
	if registers.Uints[instr.B] != uint64(instr.C) {
		offset := isa.JoinOffset(ext1.A, ext1.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}
