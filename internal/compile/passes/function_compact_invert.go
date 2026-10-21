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

package passes

import (
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// invertJumpsOverJumps replaces a conditional jump over an unconditional jump with the
// opposite conditional jump to its target.
//
// The unconditional jump must be entered from nowhere else, and neither word may belong
// to a type switch's table. A pair whose new offset does not fit is left alone.
//
// Takes body ([]isa.Instruction) which is rewritten in place.
//
// Returns bool which is true when some pair was replaced.
func invertJumpsOverJumps(body []isa.Instruction) bool {
	pinned, wellFormed := pinnedTypeSwitchWords(body)
	if !wellFormed {
		return false
	}
	var sources map[int][]int
	changed := false
	for pc := 0; pc < len(body); pc += instructionFootprint(body, pc) {
		over, final, found := jumpSteppedOver(body, pinned, pc)
		if !found {
			continue
		}
		if sources == nil {
			sources = jumpSourcesByTarget(body)
		}
		if len(sources[over]) != 0 {
			continue
		}
		original := body[pc]
		opposite, invertible := isa.OppositeConditionalJump(original)
		if !invertible {
			continue
		}
		body[pc] = opposite
		if !program.SetJumpTarget(body, pc, final) {
			body[pc] = original
			continue
		}
		body[over] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
		changed = true
	}
	return changed
}

// jumpSteppedOver finds the unconditional jump that the conditional jump at pc exists
// only to step over.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pinned ([]bool) which marks the type-switch table words, or is nil.
// Takes pc (int) which is the candidate conditional jump.
//
// Returns over (int) which is the unconditional jump's program counter.
// Returns final (int) which is where that jump lands.
// Returns found (bool) which is false unless pc holds a conditional jump that has an
// opposite and lands on the word after an unconditional jump that directly follows it.
func jumpSteppedOver(body []isa.Instruction, pinned []bool, pc int) (over, final int, found bool) {
	if _, invertible := isa.OppositeConditionalJump(body[pc]); !invertible {
		return 0, 0, false
	}
	over = pc + instructionFootprint(body, pc)
	if over >= len(body) || !isa.InstrIsTier1SubOp(body[over], isa.SubOpJump) || pinnedAt(pinned, pc) || pinnedAt(pinned, over) {
		return 0, 0, false
	}
	if target, ok := program.JumpTargetAt(body, pc); !ok || target != over+1 {
		return 0, 0, false
	}
	final, found = program.JumpTargetAt(body, over)
	return over, final, found
}
