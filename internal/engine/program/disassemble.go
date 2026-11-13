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

package program

import (
	"fmt"
	"strings"

	"pipit.sh/pipit/internal/isa"
)

const (
	// MaxDisassembleStringLen is the maximum string constant length shown in disassembly
	// comments before truncation.
	MaxDisassembleStringLen = 40

	// TruncatedDisassembleStringLen is the prefix length kept when truncating.
	TruncatedDisassembleStringLen = 37
)

// Disassemble returns a human-readable listing of the function's bytecode. Each line
// shows the program counter, opcode mnemonic, operands, and an inline comment for
// constant-loading instructions.
//
// Returns the complete disassembly as a string, or an empty string when the function body
// is empty.
func (compiledFunction *CompiledFunction) Disassemble() string {
	if len(compiledFunction.Body) == 0 {
		return ""
	}
	return compiledFunction.DisassembleRange(0, len(compiledFunction.Body))
}

// DisassembleRange returns a human-readable listing for the instruction range [start,
// end).
//
// Takes start (int) which is the inclusive start index.
// Takes end (int) which is the exclusive end index.
//
// Returns the disassembly for the range as a string.
func (compiledFunction *CompiledFunction) DisassembleRange(start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(compiledFunction.Body) {
		end = len(compiledFunction.Body)
	}
	if start >= end {
		return ""
	}

	var builder strings.Builder
	for programCounter := start; programCounter < end; programCounter++ {
		instruction := compiledFunction.Body[programCounter]
		label := isa.InstructionDisplayName(instruction)
		comment := compiledFunction.disassembleCommentAt(instruction, programCounter, end)
		if comment != "" {
			fmt.Fprintf(&builder, "%04d  %-*s %3d %3d %3d    ; %s\n",
				programCounter, isa.DisplayLabelWidth, label, instruction.A, instruction.B, instruction.C, comment)
		} else {
			fmt.Fprintf(&builder, "%04d  %-*s %3d %3d %3d\n",
				programCounter, isa.DisplayLabelWidth, label, instruction.A, instruction.B, instruction.C)
		}
	}
	return builder.String()
}

// DisassembleComment returns an inline comment for instructions that reference constant
// pools or have special semantics.
//
// Takes instruction (instruction) which is the instruction to annotate.
//
// Returns the comment string, or an empty string if none applies.
func (compiledFunction *CompiledFunction) DisassembleComment(instruction isa.Instruction) string {
	if comment := compiledFunction.disassembleLoadComment(instruction); comment != "" {
		return comment
	}
	return disassembleControlComment(instruction)
}

// disassembleCommentAt is the position-aware variant used by the range printer.
//
// Takes instruction (instruction) which is the current instruction word.
// Takes programCounter (int) which is the position of instruction within the body.
// Takes end (int) which is the exclusive upper bound on body access.
//
// Returns the disassembly comment string.
func (compiledFunction *CompiledFunction) disassembleCommentAt(instruction isa.Instruction, programCounter, end int) string {
	if programCounter+1 < end {
		index, kind, ok := isa.ExtensionConstantIndex(instruction, compiledFunction.Body[programCounter+1])
		if ok && kind == isa.RegisterUint64 && index < len(compiledFunction.UintConstants) {
			return fmt.Sprintf("uconst = %d", compiledFunction.UintConstants[index])
		}
	}
	return compiledFunction.DisassembleComment(instruction)
}

// disassembleLoadComment handles constant-loading and data instructions.
//
// Takes instruction (instruction) which is the instruction to annotate.
//
// Returns a comment string for constant-load opcodes, or empty string for other opcodes.
func (compiledFunction *CompiledFunction) disassembleLoadComment(instruction isa.Instruction) string {
	if isa.InstrIsTier1SubOp(instruction, isa.SubOpLoadBoolConstant) {
		if int(instruction.C) < len(compiledFunction.BoolConstants) {
			return fmt.Sprintf("bools[%d] = %v", instruction.B, compiledFunction.BoolConstants[instruction.C])
		}
	}
	if isa.InstrIsTier1SubOp(instruction, isa.SubOpLoadInt64Immediate) {
		return fmt.Sprintf("ints[%d] = %d", instruction.B, instruction.C)
	}
	if isa.InstrIsTier1SubOp(instruction, isa.SubOpLoadBoolImmediateAsInt64) {
		if instruction.C != 0 {
			return fmt.Sprintf("ints[%d] = true", instruction.B)
		}
		return fmt.Sprintf("ints[%d] = false", instruction.B)
	}
	if isa.InstrIsTier1SubOp(instruction, isa.SubOpLoadUint64Immediate) {
		return fmt.Sprintf("uints[%d] = %d", instruction.B, instruction.C)
	}
	switch instruction.Op {
	case isa.OpLoadInt64Constant:
		index := instruction.WideIndex()
		if int(index) < len(compiledFunction.IntConstants) {
			return fmt.Sprintf("ints[%d] = %d", instruction.A, compiledFunction.IntConstants[index])
		}
	case isa.OpLoadFloat64Constant:
		index := instruction.WideIndex()
		if int(index) < len(compiledFunction.FloatConstants) {
			return fmt.Sprintf("floats[%d] = %g", instruction.A, compiledFunction.FloatConstants[index])
		}
	case isa.OpLoadStringConstant:
		return compiledFunction.disassembleStringConstComment(instruction)
	case isa.OpAddInt64Constant, isa.OpSubtractInt64Constant, isa.OpMultiplyInt64Constant, isa.OpAddInt32Constant, isa.OpSubtractInt32Constant,
		isa.OpMultiplyInt64ConstantThenAdd:
		if int(instruction.C) < len(compiledFunction.IntConstants) {
			return fmt.Sprintf("const = %d", compiledFunction.IntConstants[instruction.C])
		}
	default:
	}
	return ""
}

// disassembleStringConstComment formats a string constant load comment.
//
// Takes instruction (instruction) which is the isa.OpLoadStringConstant instruction.
//
// Returns a quoted comment showing the destination register and value.
func (compiledFunction *CompiledFunction) disassembleStringConstComment(instruction isa.Instruction) string {
	index := instruction.WideIndex()
	if int(index) >= len(compiledFunction.StringConstants) {
		return ""
	}
	s := compiledFunction.StringConstants[index]
	if len(s) > MaxDisassembleStringLen {
		s = s[:TruncatedDisassembleStringLen] + "..."
	}
	return fmt.Sprintf("strings[%d] = %q", instruction.A, s)
}

// disassembleControlComment handles control flow and return instructions.
//
// Takes instruction (instruction) which is the instruction to annotate.
//
// Returns a comment string for jump and return opcodes, or empty string for other
// opcodes.
func disassembleControlComment(instruction isa.Instruction) string {
	if isa.InstrIsTier1SubOp(instruction, isa.SubOpJump) {
		return fmt.Sprintf("goto %d", int(instruction.SignedOffset()))
	}
	if instruction.Op == isa.OpDrillTier1 &&
		isa.SubOpcode(instruction.A) == isa.SubOpDrillTier2 &&
		isa.SubOpcodeTier2(instruction.B) == isa.SubOpTier2Return {
		return fmt.Sprintf("%d values", instruction.C)
	}
	switch instruction.Op {
	case isa.OpJumpIfTrue:
		return fmt.Sprintf("if ints[%d] != 0 goto %+d", instruction.A, int(instruction.SignedOffset()))
	case isa.OpJumpIfFalse:
		return fmt.Sprintf("if ints[%d] == 0 goto %+d", instruction.A, int(instruction.SignedOffset()))
	default:
	}
	return ""
}
