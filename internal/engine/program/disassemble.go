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

	// extensionHighByteShift is the left shift applied to the high byte of a 16-bit constant
	// pool index packed into an extension word.
	extensionHighByteShift = 8
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
// end). Out-of-bounds indices are clamped to the body length.
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
	for pc := start; pc < end; pc++ {
		instr := compiledFunction.Body[pc]
		label := isa.InstructionDisplayName(instr)
		comment := compiledFunction.disassembleCommentAt(instr, pc, end)
		if comment != "" {
			fmt.Fprintf(&builder, "%04d  %-26s %3d %3d %3d    ; %s\n",
				pc, label, instr.A, instr.B, instr.C, comment)
		} else {
			fmt.Fprintf(&builder, "%04d  %-26s %3d %3d %3d\n",
				pc, label, instr.A, instr.B, instr.C)
		}
	}
	return builder.String()
}

// DisassembleComment returns an inline comment for instructions that reference constant
// pools or have special semantics.
//
// Takes instr (instruction) which is the instruction to annotate.
//
// Returns the comment string, or an empty string if none applies.
func (compiledFunction *CompiledFunction) DisassembleComment(instr isa.Instruction) string {
	if comment := compiledFunction.disassembleLoadComment(instr); comment != "" {
		return comment
	}
	return disassembleControlComment(instr)
}

// disassembleCommentAt is the position-aware variant used by the range printer.
//
// Some tier-1 subops (e.g. fused uint-const arith) carry their constant-pool index in the
// following isa.OpExt word; this variant peeks at body[pc+1] to render the constant
// value.
//
// Takes instr (instruction) which is the current instruction word.
// Takes pc (int) which is the position of instr within the body.
// Takes end (int) which is the exclusive upper bound on body access.
//
// Returns the disassembly comment string.
func (compiledFunction *CompiledFunction) disassembleCommentAt(instr isa.Instruction, pc, end int) string {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpAddUintConst) ||
		isa.InstrIsTier1SubOp(instr, isa.SubOpSubUintConst) ||
		isa.InstrIsTier1SubOp(instr, isa.SubOpBitAndUintConst) {
		if pc+1 < end {
			ext := compiledFunction.Body[pc+1]
			constIdx := int(ext.A) | (int(ext.B) << extensionHighByteShift)
			if constIdx < len(compiledFunction.UintConstants) {
				return fmt.Sprintf("uconst = %d", compiledFunction.UintConstants[constIdx])
			}
		}
	}
	return compiledFunction.DisassembleComment(instr)
}

// disassembleLoadComment handles constant-loading and data instructions.
//
// Takes instr (instruction) which is the instruction to annotate.
//
// Returns a comment string for constant-load opcodes, or empty string for other opcodes.
func (compiledFunction *CompiledFunction) disassembleLoadComment(instr isa.Instruction) string {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadBoolConst) {
		if int(instr.C) < len(compiledFunction.BoolConstants) {
			return fmt.Sprintf("bools[%d] = %v", instr.B, compiledFunction.BoolConstants[instr.C])
		}
	}
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadIntConstSmall) {
		return fmt.Sprintf("ints[%d] = %d", instr.B, instr.C)
	}
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadBool) {
		if instr.C != 0 {
			return fmt.Sprintf("ints[%d] = true", instr.B)
		}
		return fmt.Sprintf("ints[%d] = false", instr.B)
	}
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadUintConstSmall) {
		return fmt.Sprintf("uints[%d] = %d", instr.B, instr.C)
	}
	switch instr.Op {
	case isa.OpLoadIntConst:
		index := instr.WideIndex()
		if int(index) < len(compiledFunction.IntConstants) {
			return fmt.Sprintf("ints[%d] = %d", instr.A, compiledFunction.IntConstants[index])
		}
	case isa.OpLoadFloatConst:
		index := instr.WideIndex()
		if int(index) < len(compiledFunction.FloatConstants) {
			return fmt.Sprintf("floats[%d] = %g", instr.A, compiledFunction.FloatConstants[index])
		}
	case isa.OpLoadStringConst:
		return compiledFunction.disassembleStringConstComment(instr)
	case isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst:
		if int(instr.C) < len(compiledFunction.IntConstants) {
			return fmt.Sprintf("const = %d", compiledFunction.IntConstants[instr.C])
		}
	default:
	}
	return ""
}

// disassembleStringConstComment formats a string constant load comment.
//
// Takes instr (instruction) which is the isa.OpLoadStringConst instruction.
//
// Returns a quoted comment showing the destination register and value.
func (compiledFunction *CompiledFunction) disassembleStringConstComment(instr isa.Instruction) string {
	index := instr.WideIndex()
	if int(index) >= len(compiledFunction.StringConstants) {
		return ""
	}
	s := compiledFunction.StringConstants[index]
	if len(s) > MaxDisassembleStringLen {
		s = s[:TruncatedDisassembleStringLen] + "..."
	}
	return fmt.Sprintf("strings[%d] = %q", instr.A, s)
}

// disassembleControlComment handles control flow and return instructions.
//
// Takes instr (instruction) which is the instruction to annotate.
//
// Returns a comment string for jump and return opcodes, or empty string for other
// opcodes.
func disassembleControlComment(instr isa.Instruction) string {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpJump) {
		return fmt.Sprintf("goto %d", int(instr.SignedOffset()))
	}
	if instr.Op == isa.OpDrillTier1 &&
		isa.SubOpcode(instr.A) == isa.SubOpDrillTier2 &&
		isa.SubOpcodeTier2(instr.B) == isa.SubOpTier2Return {
		return fmt.Sprintf("%d values", instr.C)
	}
	switch instr.Op {
	case isa.OpJumpIfTrue:
		return fmt.Sprintf("if ints[%d] != 0 goto %+d", instr.A, int(instr.SignedOffset()))
	case isa.OpJumpIfFalse:
		return fmt.Sprintf("if ints[%d] == 0 goto %+d", instr.A, int(instr.SignedOffset()))
	default:
	}
	return ""
}
