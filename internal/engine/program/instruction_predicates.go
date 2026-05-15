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
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// InstructionByteShift is the bit shift applied when packing the upper byte of a uint16
	// operand into the trailing C byte of an instruction.
	InstructionByteShift = 8
)

// IsReturnInstruction reports whether instr is a return encoding, matching both the
// SubOpTier2Return and SubOpTier3ReturnVoid drill paths.
//
// Takes instr (instruction) which is the candidate instruction.
//
// Returns true when instr is a return encoding.
func IsReturnInstruction(instr isa.Instruction) bool {
	if instr.Op != isa.OpDrillTier1 {
		return false
	}
	if isa.SubOpcode(instr.A) != isa.SubOpDrillTier2 {
		return false
	}
	switch isa.SubOpcodeTier2(instr.B) {
	case isa.SubOpTier2Return:
		return true
	case isa.SubOpTier2DrillTier3:
		return isa.SubOpcodeTier3(instr.C) == isa.SubOpTier3ReturnVoid
	default:
	}
	return false
}

// DecodeJumpOffset extracts the signed 16-bit offset from a word-layout jump instruction.
// Extension-word jumps must go through JumpTargetAt instead.
//
// Takes instr (isa.Instruction) whose B and C bytes carry the packed offset.
//
// Returns the sign-extended int offset.
func DecodeJumpOffset(instr isa.Instruction) int {
	lo := uint16(instr.B)
	hi := uint16(instr.C)
	raw := lo | hi<<InstructionByteShift
	return int(safeconv.Uint16ToInt16(raw))
}
