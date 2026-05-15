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

package isa

const (
	// JumpLayoutNone marks an operation that carries no jump offset.
	JumpLayoutNone JumpLayout = iota

	// JumpLayoutWord keeps a signed 16-bit offset in B|(C<<8) of the instruction word
	// itself. The target is pc + 1 + offset.
	JumpLayoutWord

	// JumpLayoutExtension keeps a signed 16-bit offset in A|(B<<8) of the OpExt word that
	// follows. The target is pc + 2 + trailingNops + offset.
	JumpLayoutExtension

	// JumpLayoutSecondExtension keeps a signed 16-bit offset in A|(B<<8) of the second OpExt
	// word that follows. The target is pc + 3 + trailingNops + offset.
	JumpLayoutSecondExtension

	// JumpLayoutExtension24 keeps an unsigned 24-bit forward offset across all three operand
	// bytes of the OpExt word that follows. The target is pc + 2 + offset.
	JumpLayoutExtension24

	// JumpLayoutMultiWay marks a dispatch whose targets live in the OpTypeSwitchCase rows
	// that follow it, each of which is a JumpLayoutWord jump of its own, terminated by the
	// default SubOpJump. It has no single target to decode.
	JumpLayoutMultiWay
)

const (
	// EqUintConstJumpFalseNopCount is the number of OpNop words that follow the extension
	// word of SubOpEqUintConstJumpFalse. The fusion collapses three words into two and pads
	// the third so later instructions keep their indices.
	EqUintConstJumpFalseNopCount = 1

	// RangeCheckUintFusionNopCount is the number of OpNop words that follow the two
	// extension words of SubOpTier2RangeCheckUintJumpFalse. The fusion collapses eight words
	// into three and pads the other five.
	RangeCheckUintFusionNopCount = 5
)

const (
	// singleJumpWordCount is the footprint of a jump whose offset sits in its own word.
	singleJumpWordCount = 1

	// extensionJumpWordCount is the footprint of a jump whose offset sits in the first
	// extension word.
	extensionJumpWordCount = 2

	// secondExtensionJumpWordCount is the footprint of a jump whose offset sits in the
	// second extension word.
	secondExtensionJumpWordCount = 3
)

// JumpLayout says where an operation keeps its relative jump offset.
type JumpLayout uint8

// JumpLayoutOf classifies the jump layout of an instruction.
//
// Takes instr (Instruction) which is the instruction to classify.
//
// Returns layout (JumpLayout) which says where the offset lives.
// Returns trailingNops (int) which is the number of OpNop words after the operation's
// extension words that the target is computed past.
func JumpLayoutOf(instr Instruction) (layout JumpLayout, trailingNops int) {
	row, ok := SpecForInstruction(instr)
	if !ok || !row.Has(SpecJump) {
		return JumpLayoutNone, 0
	}
	if InstrIsTier2SubOp(instr, SubOpTier2RangeCheckUintJumpFalse) {
		return JumpLayoutSecondExtension, RangeCheckUintFusionNopCount
	}
	if InstrIsTier1SubOp(instr, SubOpEqUintConstJumpFalse) {
		return JumpLayoutExtension, EqUintConstJumpFalseNopCount
	}
	if InstrIsTier1SubOp(instr, SubOpTypeSwitchJump) {
		return JumpLayoutMultiWay, 0
	}
	if instr.Op == OpRangeNextSliceInt || instr.Op == OpRangeNextSliceByte {
		return JumpLayoutExtension24, 0
	}
	if ShapeForInstruction(instr).Flags&ShapeFlagFollowsExtension != 0 {
		return JumpLayoutExtension, 0
	}
	return JumpLayoutWord, 0
}

// WordCount returns how many instruction words an operation with this layout occupies
// before any OpNop padding: one for a word-layout jump, two when the offset lives in the
// first extension word, three when it lives in the second.
//
// Returns int which is the word count, and 1 for layouts that carry no offset.
func (layout JumpLayout) WordCount() int {
	switch layout {
	case JumpLayoutExtension, JumpLayoutExtension24:
		return extensionJumpWordCount
	case JumpLayoutSecondExtension:
		return secondExtensionJumpWordCount
	default:
		return singleJumpWordCount
	}
}
