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

import "piko.sh/safeconv"

const (
	// JumpLayoutNone marks an operation that carries no jump offset.
	JumpLayoutNone JumpLayout = iota

	// JumpLayoutWord keeps a signed 16-bit offset in B|(C<<8) of the instruction word
	// itself. The target is pc + 1 + offset.
	JumpLayoutWord

	// JumpLayoutExtension keeps a signed 16-bit offset in A|(B<<8) of the OpExtension word
	// that follows. The target is pc + 2 + trailingNops + offset.
	JumpLayoutExtension

	// JumpLayoutSecondExtension keeps a signed 16-bit offset in A|(B<<8) of the second
	// OpExtension word that follows. The target is pc + 3 + trailingNops + offset.
	JumpLayoutSecondExtension

	// JumpLayoutExtension24 keeps an unsigned 24-bit forward offset across all three operand
	// bytes of the OpExtension word that follows. The target is pc + 2 + offset.
	JumpLayoutExtension24

	// JumpLayoutMultiWay marks a dispatch whose targets live in the OpTypeSwitchCase rows
	// that follow it, each of which is a JumpLayoutWord jump of its own, terminated by the
	// default SubOpJump. It has no single target to decode.
	JumpLayoutMultiWay

	// JumpLayoutExtensionBC keeps a signed 16-bit offset in B|(C<<8) of the next OpExtension
	// word.
	//
	// The target is pc + 2 + offset. The word's A byte names a register the operation
	// writes.
	JumpLayoutExtensionBC
)

const (
	// EqualUint64ImmediateThenJumpIfFalseNoOperationCount is the number of OpNoOperation
	// words that follow the extension word of SubOpEqualUint64ImmediateThenJumpIfFalse.
	EqualUint64ImmediateThenJumpIfFalseNoOperationCount = 1

	// RangeCheckUintFusionNoOperationCount is the number of OpNoOperation words that follow
	// the two extension words of SubOpTier2InRangeUint64ThenJumpIfFalse.
	RangeCheckUintFusionNoOperationCount = 5
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

// jumpLayoutEntry is one operation's jump layout as JumpLayoutOf reports it.
type jumpLayoutEntry struct {
	// layout says where the offset lives.
	layout JumpLayout

	// trailingNops is the number of OpNoOperation words after the operation's extension
	// words that the target is computed past.
	trailingNops uint8
}

// jumpLayouts holds every operation's jump layout by flat dispatch slot.
var jumpLayouts [FlatDispatchSlots]jumpLayoutEntry

// JumpLayoutOf classifies the jump layout of an instruction.
//
// Takes instr (Instruction) which is the instruction to classify.
//
// Returns layout (JumpLayout) which says where the offset lives.
// Returns trailingNops (int) which is the number of OpNoOperation words after the
// operation's extension words that the target is computed past.
func JumpLayoutOf(instr Instruction) (layout JumpLayout, trailingNops int) {
	entry := jumpLayouts[FlatIndexFor(instr)]
	return entry.layout, int(entry.trailingNops)
}

// buildJumpLayouts fills jumpLayouts after spec rows and operand shapes are registered.
func buildJumpLayouts() {
	for slot := range jumpLayouts {
		instr := instructionForFlatSlot(slot)
		if FlatIndexFor(instr) != slot {
			continue
		}
		layout, trailingNops := deriveJumpLayout(instr)
		jumpLayouts[slot] = jumpLayoutEntry{layout: layout, trailingNops: trailingNops}
	}
}

// instructionForFlatSlot returns an instruction that dispatches to a flat slot, with
// every operand byte the encoding leaves free set to zero.
//
// A slot whose code is its tier's drill marker has no instruction of its own, since the
// marker sends the decode a tier deeper. The instruction returned for such a slot
// dispatches elsewhere, which FlatIndexFor shows.
//
// Takes slot (int) which is the flat dispatch slot.
//
// Returns Instruction which dispatches to the slot when one can.
func instructionForFlatSlot(slot int) Instruction {
	code := safeconv.MustIntToUint8(slot % SlotsPerTier)
	switch Tier(safeconv.MustIntToUint8(slot / SlotsPerTier)) {
	case TierMain:
		return Instruction{Op: Opcode(code), A: 0, B: 0, C: 0}
	case TierSub1:
		return Instruction{Op: OpDrillTier1, A: code, B: 0, C: 0}
	case TierSub2:
		return Instruction{Op: OpDrillTier1, A: uint8(SubOpDrillTier2), B: code, C: 0}
	default:
		return Instruction{Op: OpDrillTier1, A: uint8(SubOpDrillTier2), B: uint8(SubOpTier2DrillTier3), C: code}
	}
}

// deriveJumpLayout works out the jump layout of an instruction from its spec row and
// operand shape.
//
// Takes instr (Instruction) which is the instruction to classify.
//
// Returns layout (JumpLayout) which says where the offset lives.
// Returns trailingNops (uint8) which is the number of OpNoOperation words after the
// operation's extension words that the target is computed past.
func deriveJumpLayout(instr Instruction) (layout JumpLayout, trailingNops uint8) {
	row := specRowForInstruction(instr)
	if row == nil || row.Flags&SpecJump == 0 {
		return JumpLayoutNone, 0
	}
	if InstrIsTier2SubOp(instr, SubOpTier2InRangeUint64ThenJumpIfFalse) {
		return JumpLayoutSecondExtension, RangeCheckUintFusionNoOperationCount
	}
	if InstrIsTier1SubOp(instr, SubOpEqualUint64ImmediateThenJumpIfFalse) {
		return JumpLayoutExtension, EqualUint64ImmediateThenJumpIfFalseNoOperationCount
	}
	if InstrIsTier1SubOp(instr, SubOpTypeSwitchJump) {
		return JumpLayoutMultiWay, 0
	}
	if isGetMapOkThenJumpIfFalse(instr.Op) {
		return JumpLayoutExtensionBC, 0
	}
	if instr.Op == OpRangeNextSliceInt64 || instr.Op == OpRangeNextSliceByte {
		return JumpLayoutExtension24, 0
	}
	if ShapeForInstruction(instr).Flags&ShapeFlagFollowsExtension != 0 {
		return JumpLayoutExtension, 0
	}
	return JumpLayoutWord, 0
}

// isGetMapOkThenJumpIfFalse reports whether op is one of the fused map-index-ok branches,
// whose extension word carries the ok register in A and the offset in B|(C<<8).
//
// Takes op (Opcode) which is the operation to test.
//
// Returns true for the six OpMapIndexOkJumpIfFalse* operations.
func isGetMapOkThenJumpIfFalse(op Opcode) bool {
	switch op {
	case OpGetMapInt64Int64OkThenJumpIfFalse, OpGetMapStringInt64OkThenJumpIfFalse,
		OpGetMapStringStringOkThenJumpIfFalse, OpGetMapInt64StringOkThenJumpIfFalse,
		OpGetMapInt64GeneralOkThenJumpIfFalse, OpGetMapStringGeneralOkThenJumpIfFalse:
		return true
	default:
		return false
	}
}

// WordCount returns how many instruction words an operation with this layout occupies
// before any OpNoOperation padding, which is one for a word-layout jump, two when the
// offset lives in the first extension word, three when it lives in the second.
//
// Returns int which is the word count, and 1 for layouts that carry no offset.
func (layout JumpLayout) WordCount() int {
	switch layout {
	case JumpLayoutExtension, JumpLayoutExtension24, JumpLayoutExtensionBC:
		return extensionJumpWordCount
	case JumpLayoutSecondExtension:
		return secondExtensionJumpWordCount
	default:
		return singleJumpWordCount
	}
}
