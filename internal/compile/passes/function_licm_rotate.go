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
	"fmt"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// loopHoistPCMap describes where each pre-hoist program counter lands after a rotation
// moves [ReadPC, ReadPC+Width) to HeaderPC.
type loopHoistPCMap struct {
	// HeaderPC is the loop header where words rotate to.
	HeaderPC int

	// ReadPC is the pre-rotation PC of the hoisted instruction.
	ReadPC int

	// Width is the word count of the hoisted instruction.
	Width int
}

// newPC maps a pre-rotation program counter to its post-rotation position.
//
// Takes pc (int) which is a program counter in the pre-rotation body.
//
// Returns the program counter the same word occupies after the rotation.
func (m loopHoistPCMap) newPC(pc int) int {
	switch {
	case pc < m.HeaderPC || pc >= m.ReadPC+m.Width:
		return pc
	case pc >= m.ReadPC:
		return m.HeaderPC + (pc - m.ReadPC)
	default:
		return pc + m.Width
	}
}

// newJumpTarget maps the target of the jump at sourcePC.
//
// The header splits by where the jump comes from: a jump from before the loop enters it
// and must run the hoisted words, so it keeps landing on the header; a jump from inside
// or after the loop is a back-edge or an in-loop branch and must skip them. A jump that
// landed on the hoisted instruction itself lands on that instruction's successor, whose
// position does not change. Every other target follows newPC().
//
// Takes sourcePC (int) which is the jump's pre-rotation program counter.
// Takes target (int) which is the jump's pre-rotation target.
//
// Returns the post-rotation target.
func (m loopHoistPCMap) newJumpTarget(sourcePC, target int) int {
	switch {
	case target == m.HeaderPC && sourcePC < m.HeaderPC:
		return m.HeaderPC
	case target == m.HeaderPC:
		return m.HeaderPC + m.Width
	case target >= m.ReadPC && target < m.ReadPC+m.Width:
		return m.ReadPC + m.Width
	default:
		return m.newPC(target)
	}
}

// jumpRetarget records where one jump must point after the rotation.
type jumpRetarget struct {
	// sourcePC is the jump instruction's post-rotation PC.
	sourcePC int

	// target is the post-rotation PC the jump must point to.
	target int
}

// applyLoopHoist rotates the instruction at readPC to the loop header, remapping jumps,
// source map, provenance and PC-keyed annotation sets.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which is invalidated by the rotation; nil is
// tolerated.
// Takes headerPC (int) which is the loop header PC (insertion site for the hoist).
// Takes readPC (int) which is the original PC of the loop-invariant instruction; readPC
// must satisfy readPC >= headerPC.
//
// Panics when a jump cannot be repointed after the rotation.
func applyLoopHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, headerPC, readPC int) {
	body := compiledFunction.Body
	pcMap := loopHoistPCMap{HeaderPC: headerPC, ReadPC: readPC, Width: hoistedReadWordCount(body, readPC)}
	retargets := collectJumpRetargets(body, pcMap)
	rotateWordsToFront(body, headerPC, readPC, pcMap.Width)
	if compiledFunction.DebugSourceMap != nil && readPC+pcMap.Width <= len(compiledFunction.DebugSourceMap.Positions) {
		rotateWordsToFront(compiledFunction.DebugSourceMap.Positions, headerPC, readPC, pcMap.Width)
	}
	compiledFunction.PeepholeProvenance = remapPeepholeProvenance(compiledFunction.PeepholeProvenance, pcMap)
	compiledFunction.ArenaSafeAllocPCs = remapPCKeyedSet(compiledFunction.ArenaSafeAllocPCs, pcMap)
	compiledFunction.FieldStoreArenaSafePCs = remapPCKeyedSet(compiledFunction.FieldStoreArenaSafePCs, pcMap)
	compiledFunction.InPlaceHeaderReusePCs = remapPCKeyedSet(compiledFunction.InPlaceHeaderReusePCs, pcMap)
	for _, retarget := range retargets {
		if !program.SetJumpTarget(body, retarget.sourcePC, retarget.target) {
			panic(fmt.Sprintf("licm: jump at pc %d cannot be repointed at %d after hoisting pc %d to %d",
				retarget.sourcePC, retarget.target, readPC, headerPC))
		}
	}
	for offset := range pcMap.Width {
		RecordPeepholeRewrite(compiledFunction, headerPC+offset, peepholeRewriteLicmHoist, readPC+offset)
	}
	compiledFunction.AliasInfo = nil
	analysis.invalidate()
}

// collectJumpRetargets decodes every jump of the pre-rotation body and records where it
// must point, and from where, once the rotation has been applied.
//
// Takes body ([]isa.Instruction) which is the pre-rotation instruction stream.
// Takes pcMap (loopHoistPCMap) which describes the rotation.
//
// Returns one entry per jump, in ascending source order.
func collectJumpRetargets(body []isa.Instruction, pcMap loopHoistPCMap) []jumpRetarget {
	var retargets []jumpRetarget
	for pc := range body {
		target, isJump := program.JumpTargetAt(body, pc)
		if !isJump {
			continue
		}
		retargets = append(retargets, jumpRetarget{sourcePC: pcMap.newPC(pc), target: pcMap.newJumpTarget(pc, target)})
	}
	return retargets
}

// rotateWordsToFront moves words[at:at+width] to position front, shifting words[front:at]
// up by width so the sequence keeps every other word in order.
//
// Takes words ([]T) which is the slice rotated in place.
// Takes front (int) which is the position the moved words take.
// Takes at (int) which is the first moved word's current position; at must be >= front.
// Takes width (int) which is the number of words to move.
func rotateWordsToFront[T any](words []T, front, at, width int) {
	if at == front || width == 0 {
		return
	}
	moved := slices.Clone(words[at : at+width])
	copy(words[front+width:at+width], words[front:at])
	copy(words[front:front+width], moved)
}

// remapPeepholeProvenance rebuilds the provenance map after a rotation.
//
// Takes provenance (map[int]program.PeepholeAnnotation) which is the pre-rotation map;
// nil is returned unchanged.
// Takes pcMap (loopHoistPCMap) which describes the rotation.
//
// Returns the rebuilt map.
func remapPeepholeProvenance(provenance map[int]program.PeepholeAnnotation, pcMap loopHoistPCMap) map[int]program.PeepholeAnnotation {
	if len(provenance) == 0 {
		return provenance
	}
	rebuilt := make(map[int]program.PeepholeAnnotation, len(provenance))
	for pc, annotation := range provenance {
		if annotation.Origin >= 0 {
			annotation.Origin = pcMap.newPC(annotation.Origin)
		}
		rebuilt[pcMap.newPC(pc)] = annotation
	}
	return rebuilt
}

// remapPCKeyedSet rebuilds a PC-keyed annotation set after a rotation so each entry keeps
// naming the instruction it was recorded for.
//
// Takes set (map[int]bool) which is the pre-rotation set; nil or empty is returned
// unchanged.
// Takes pcMap (loopHoistPCMap) which describes the rotation.
//
// Returns the rebuilt set.
func remapPCKeyedSet(set map[int]bool, pcMap loopHoistPCMap) map[int]bool {
	if len(set) == 0 {
		return set
	}
	rebuilt := make(map[int]bool, len(set))
	for pc, flagged := range set {
		rebuilt[pcMap.newPC(pc)] = flagged
	}
	return rebuilt
}
