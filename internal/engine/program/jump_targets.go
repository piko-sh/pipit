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
	// extension24Limit is one past the largest offset a 24-bit extension word can carry.
	extension24Limit = 1 << 24
)

// JumpTargetAt decodes the absolute target of the jump at pc.
//
// Every layout isa.JumpLayoutOf() knows is decoded here, with the same arithmetic the
// handlers use: word-layout jumps land at pc+1+offset, extension-layout jumps at the word
// after their extension words and any OpNop padding, and the range-next steps at pc+2
// plus their 24-bit forward offset. The type-switch dispatch has no single target and is
// reported as not a jump; its OpTypeSwitchCase rows and default jump decode on their own.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the candidate jump's program counter.
//
// Returns target (int) which is the absolute program counter the jump lands on.
// Returns ok (bool) which is false when body[pc] is not a jump or its extension words are
// missing.
func JumpTargetAt(body []isa.Instruction, pc int) (target int, ok bool) {
	if pc < 0 || pc >= len(body) {
		return 0, false
	}
	instr := body[pc]
	layout, trailingNops := isa.JumpLayoutOf(instr)
	switch layout {
	case isa.JumpLayoutWord:
		return pc + 1 + int(instr.SignedOffset()), true
	case isa.JumpLayoutExtension:
		ext, found := extensionWordAt(body, pc+1)
		if !found {
			return 0, false
		}
		return pc + layout.WordCount() + trailingNops + int(isa.JoinOffset(ext.A, ext.B)), true
	case isa.JumpLayoutSecondExtension:
		if _, found := extensionWordAt(body, pc+1); !found {
			return 0, false
		}
		ext, found := extensionWordAt(body, pc+2)
		if !found {
			return 0, false
		}
		return pc + layout.WordCount() + trailingNops + int(isa.JoinOffset(ext.A, ext.B)), true
	case isa.JumpLayoutExtension24:
		ext, found := extensionWordAt(body, pc+1)
		if !found {
			return 0, false
		}
		return pc + layout.WordCount() + isa.DecodeExtension24(ext), true
	default:
		return 0, false
	}
}

// SetJumpTarget rewrites the jump at pc so that it lands on target, keeping the layout
// isa.JumpLayoutOf reports for it.
//
// Takes body ([]isa.Instruction) which is the instruction stream to patch in place.
// Takes pc (int) which is the jump's program counter.
// Takes target (int) which is the absolute program counter the jump must land on.
//
// Returns ok (bool) which is false when the instruction is not a supported jump, its
// extension words are missing, or the offset does not fit.
func SetJumpTarget(body []isa.Instruction, pc, target int) (ok bool) {
	if pc < 0 || pc >= len(body) {
		return false
	}
	layout, trailingNops := isa.JumpLayoutOf(body[pc])
	switch layout {
	case isa.JumpLayoutWord:
		return setWordOffset(&body[pc].B, &body[pc].C, target-(pc+1))
	case isa.JumpLayoutExtension:
		if _, found := extensionWordAt(body, pc+1); !found {
			return false
		}
		return setWordOffset(&body[pc+1].A, &body[pc+1].B, target-(pc+layout.WordCount()+trailingNops))
	case isa.JumpLayoutSecondExtension:
		if _, found := extensionWordAt(body, pc+1); !found {
			return false
		}
		if _, found := extensionWordAt(body, pc+2); !found {
			return false
		}
		second := &body[pc+2] //nolint:gosec // extensionWordAt() checked the index
		return setWordOffset(&second.A, &second.B, target-(pc+layout.WordCount()+trailingNops))
	case isa.JumpLayoutExtension24:
		if _, found := extensionWordAt(body, pc+1); !found {
			return false
		}
		offset := target - (pc + layout.WordCount())
		if offset < 0 || offset >= extension24Limit {
			return false
		}
		body[pc+1] = isa.NewInstruction(isa.OpExt,
			uint8(offset), uint8(offset>>isa.WideBitShift), uint8(offset>>(2*isa.WideBitShift))) //nolint:gosec // range-checked above
		return true
	default:
		return false
	}
}

// JumpWordCount returns how many instruction words the jump at instr occupies before any
// OpNop padding: one for a word-layout jump, two when the offset lives in the first
// extension word, three when it lives in the second, and one for anything that is not a
// jump.
//
// Takes instr (isa.Instruction) which is the candidate jump.
//
// Returns int which is the word count.
func JumpWordCount(instr isa.Instruction) int {
	layout, _ := isa.JumpLayoutOf(instr)
	return layout.WordCount()
}

// JumpFootprint returns how many words the jump at instr spans in total, including the
// OpNop padding a fusion left after its extension words. The program counter of the
// fall-through successor is pc plus this value.
//
// Takes instr (isa.Instruction) which is the candidate jump.
//
// Returns int which is the span in words, and 1 for anything that is not a jump.
func JumpFootprint(instr isa.Instruction) int {
	layout, trailingNops := isa.JumpLayoutOf(instr)
	return layout.WordCount() + trailingNops
}

// JumpTargetsOf returns the set of program counters that some jump in body lands on.
//
// Takes body ([]isa.Instruction) which is the instruction stream to scan.
//
// Returns map[int]bool whose keys are every targeted program counter.
func JumpTargetsOf(body []isa.Instruction) map[int]bool {
	targets := make(map[int]bool, len(body)/4)
	for pc := range body {
		if target, ok := JumpTargetAt(body, pc); ok {
			targets[target] = true
		}
	}
	return targets
}

// BuildJumpTargets pre-computes the set of jump-destination indices for O(1) lookup.
//
// Takes body ([]isa.Instruction) which specifies the instruction sequence to scan.
//
// Returns a map of instruction indices that are jump targets.
func (*CompiledFunction) BuildJumpTargets(body []isa.Instruction) map[int]bool {
	return JumpTargetsOf(body)
}

// extensionWordAt returns body[pc] when it is an OpExt word.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the expected position of the extension word.
//
// Returns ext (isa.Instruction) which is the extension word.
// Returns ok (bool) which is false when pc is out of range or the word is not OpExt.
func extensionWordAt(body []isa.Instruction, pc int) (ext isa.Instruction, ok bool) {
	if pc < 0 || pc >= len(body) || body[pc].Op != isa.OpExt {
		return isa.Instruction{}, false
	}
	return body[pc], true
}

// setWordOffset stores a signed 16-bit offset into the two bytes that carry it, refusing
// an offset outside the encodable range.
//
// Takes lo (*uint8) which receives the low byte.
// Takes hi (*uint8) which receives the high byte.
// Takes offset (int) which is the relative offset to store.
//
// Returns ok (bool) which is false when offset does not fit in 16 bits.
func setWordOffset(lo, hi *uint8, offset int) (ok bool) {
	if !FitsJumpOffset(offset) {
		return false
	}
	*lo, *hi = isa.SplitOffset(safeconv.MustIntToInt16(offset))
	return true
}
