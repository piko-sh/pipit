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
	"context"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// maxCompactRounds bounds the repeated rebuilds that straighten blocks moved into other
	// moved blocks.
	maxCompactRounds = 4

	// maxJumpThreadSteps bounds how many unconditional jumps a branch is threaded through.
	maxJumpThreadSteps = 8
)

// compactBodyPass deletes the words earlier passes left behind that do nothing at run
// time: NOPs and unconditional jumps to the next instruction. It runs last, so every
// other pass can keep relying on stable PCs.
type compactBodyPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (compactBodyPass) Name() string { return "compact" }

// Run applies compactBody().
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rebuilt.
//
// Returns error (always nil).
func (compactBodyPass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if compactBody(compiledFunction) {
		state.Analysis.invalidate()
	}
	return nil
}

// compactBody moves out-of-line blocks back to the jump that enters them, deletes dead
// words, then threads jumps through unconditional jumps, repeating until nothing changes.
// Straightening runs first so threading cannot add a second entry to a block.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rebuilt.
//
// Returns true when the body changed.
func compactBody(compiledFunction *program.CompiledFunction) bool {
	if len(compiledFunction.Body) == 0 || bodyHasTypeSwitch(compiledFunction.Body) {
		return false
	}
	changed := false
	for range maxCompactRounds {
		compacted := compactOnce(compiledFunction)
		threaded := threadJumps(compiledFunction.Body)
		if !compacted && !threaded {
			return changed
		}
		changed = true
	}
	return changed
}

// compactOnce performs one rebuild: straightened blocks move to their jump, and dead
// words are deleted.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rebuilt.
//
// Returns true when the body changed.
func compactOnce(compiledFunction *program.CompiledFunction) bool {
	body := compiledFunction.Body
	dead := markDeadWords(body)
	live := keepSoleSourceLines(compiledFunction.DebugSourceMap, dead)
	blocks := findStraightenableBlocks(body, dead)
	for jumpPC, block := range blocks {
		if !dropKeepsSourceLines(compiledFunction.DebugSourceMap, live, jumpPC, block.end) {
			delete(blocks, jumpPC)
		}
	}
	rewrite := newBodyRewrite(len(body), len(body))
	moved := make([]bool, len(body))
	for _, block := range blocks {
		for pc := block.start; pc <= block.end; pc++ {
			moved[pc] = true
		}
	}
	removed := len(blocks) > 0
	for pc := range body {
		switch {
		case moved[pc]:
			continue
		case blocks[pc].end > 0:
			placeStraightenedBlock(rewrite, body, dead, pc, blocks[pc])
		case dead[pc]:
			rewrite.drop(pc)
			removed = true
		default:
			rewrite.keep(body, pc)
		}
	}
	return removed && rewrite.apply(compiledFunction)
}

// placeStraightenedBlock replaces the jump at jumpPC with the block it entered, dropping
// the jump and the block's closing jump back.
//
// Takes rewrite (*bodyRewrite) which receives the words.
// Takes body ([]isa.Instruction) which is the old body.
// Takes dead ([]bool) which marks deletable words inside the block.
// Takes jumpPC (int) which is the jump being replaced.
// Takes block (straightBlock) which is the block to place.
func placeStraightenedBlock(rewrite *bodyRewrite, body []isa.Instruction, dead []bool, jumpPC int, block straightBlock) {
	rewrite.drop(jumpPC)
	for pc := block.start; pc < block.end; pc++ {
		if dead[pc] {
			rewrite.drop(pc)
			continue
		}
		rewrite.keep(body, pc)
	}
	rewrite.drop(block.end)
}

// markDeadWords marks the NOPs that start an instruction, then, until nothing changes,
// the unconditional jumps whose target is the next live instruction.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns the per-word mask of deletable words.
func markDeadWords(body []isa.Instruction) []bool {
	dead := make([]bool, len(body))
	var jumps []int
	for pc := 0; pc < len(body); {
		width := instructionFootprint(body, pc)
		switch {
		case width == 1 && isCanonicalNop(body[pc]):
			dead[pc] = true
		case width == 1 && isa.InstrIsTier1SubOp(body[pc], isa.SubOpJump):
			jumps = append(jumps, pc)
		}
		pc += width
	}
	for changed := true; changed; {
		changed = false
		for _, pc := range jumps {
			target, ok := program.JumpTargetAt(body, pc)
			if !dead[pc] && ok && nextLiveWord(dead, target) == nextLiveWord(dead, pc+1) {
				dead[pc] = true
				changed = true
			}
		}
	}
	return dead
}

// instructionFootprint returns how many words the instruction at pc spans: a jump's
// footprint including its padding, otherwise the word and its extension words.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the start of an instruction.
//
// Returns the word count, at least 1.
func instructionFootprint(body []isa.Instruction, pc int) int {
	if layout, _ := isa.JumpLayoutOf(body[pc]); layout != isa.JumpLayoutNone {
		return program.JumpFootprint(body[pc])
	}
	width := 1
	for pc+width < len(body) && body[pc+width].Op == isa.OpExt {
		width++
	}
	return width
}

// nextLiveWord returns the first PC at or after pc that is not dead, or the body length.
//
// Takes dead ([]bool) which marks the deletable words.
// Takes pc (int) which is the starting PC.
//
// Returns the PC execution reaches from pc once dead words are gone.
func nextLiveWord(dead []bool, pc int) int {
	for pc < len(dead) && dead[pc] {
		pc++
	}
	return pc
}

// sourceLine identifies a source line a breakpoint can bind to.
type sourceLine struct {
	// line is the 1-based line number.
	line int32

	// file is the source map's file index.
	file uint16
}

// liveSourceLines counts, for each source line, the live words carrying it.
//
// Takes sourceMap (*program.SourceMap) which may be nil.
// Takes dead ([]bool) which marks the deletable words.
//
// Returns the counts, or nil when there is no parallel source map.
func liveSourceLines(sourceMap *program.SourceMap, dead []bool) map[sourceLine]int {
	if sourceMap == nil || len(sourceMap.Positions) != len(dead) {
		return nil
	}
	live := make(map[sourceLine]int)
	for pc, position := range sourceMap.Positions {
		if !dead[pc] {
			live[sourceLine{line: position.Line, file: position.FileID}]++
		}
	}
	return live
}

// keepSoleSourceLines revives a dead word that is the only word carrying its source line,
// so a breakpoint on that line still has somewhere to bind.
//
// Takes sourceMap (*program.SourceMap) which may be nil.
// Takes dead ([]bool) which marks the deletable words and is updated in place.
//
// Returns the live-word count per source line after reviving, or nil without a source
// map.
func keepSoleSourceLines(sourceMap *program.SourceMap, dead []bool) map[sourceLine]int {
	live := liveSourceLines(sourceMap, dead)
	if live == nil {
		return nil
	}
	for pc, position := range sourceMap.Positions {
		key := sourceLine{line: position.Line, file: position.FileID}
		if dead[pc] && position.Line != 0 && live[key] == 0 {
			dead[pc] = false
			live[key]++
		}
	}
	return live
}

// dropKeepsSourceLines reports whether deleting the live words at pcs leaves every source
// line they carry on some other word.
//
// Takes sourceMap (*program.SourceMap) which may be nil.
// Takes live (map[sourceLine]int) which counts live words per line; nil skips the check.
// Takes pcs ([]int) which are the live words to delete.
//
// Returns true when no line loses its last word.
func dropKeepsSourceLines(sourceMap *program.SourceMap, live map[sourceLine]int, pcs ...int) bool {
	if live == nil {
		return true
	}
	removed := make(map[sourceLine]int, len(pcs))
	for _, pc := range pcs {
		position := sourceMap.Positions[pc]
		if position.Line == 0 {
			continue
		}
		key := sourceLine{line: position.Line, file: position.FileID}
		removed[key]++
		if live[key] <= removed[key] {
			return false
		}
	}
	return true
}

// straightBlock is an out-of-line block entered only by one forward jump and left only by
// a jump back to the word after it.
type straightBlock struct {
	// start is the block's first word, the entering jump's target.
	start int

	// end is the block's closing jump back.
	end int
}

// findStraightenableBlocks finds, for each unconditional forward jump, the block it
// enters when the block can be moved to replace the jump.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes dead ([]bool) which marks words that will be deleted.
//
// Returns the blocks keyed by the PC of their entering jump; a zero end means none.
func findStraightenableBlocks(body []isa.Instruction, dead []bool) map[int]straightBlock {
	sources := jumpSourcesByTarget(body)
	claimed := make([]bool, len(body))
	blocks := make(map[int]straightBlock)
	for pc := 0; pc < len(body); pc += instructionFootprint(body, pc) {
		if dead[pc] || claimed[pc] || !isa.InstrIsTier1SubOp(body[pc], isa.SubOpJump) {
			continue
		}
		block, ok := straightBlockFor(body, dead, sources, pc)
		if !ok || claimedRange(claimed, block) {
			continue
		}
		for word := block.start; word <= block.end; word++ {
			claimed[word] = true
		}
		claimed[pc] = true
		blocks[pc] = block
	}
	return blocks
}

// straightBlockFor checks whether the jump at jumpPC enters a block that can replace it.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes dead ([]bool) which marks words that will be deleted.
// Takes sources (map[int][]int) which lists the jumps landing on each PC.
// Takes jumpPC (int) which is the candidate entering jump.
//
// Returns the block and whether it qualifies.
func straightBlockFor(body []isa.Instruction, dead []bool, sources map[int][]int, jumpPC int) (straightBlock, bool) {
	start, ok := program.JumpTargetAt(body, jumpPC)
	if !ok || start <= jumpPC+1 || start >= len(body) || fallsInto(body, dead, start) {
		return straightBlock{start: 0, end: 0}, false
	}
	back := nextLiveWord(dead, jumpPC+1)
	for pc := start; pc < len(body); pc += instructionFootprint(body, pc) {
		target, jump := program.JumpTargetAt(body, pc)
		if !jump || !isa.InstrIsTier1SubOp(body[pc], isa.SubOpJump) || nextLiveWord(dead, target) != back {
			continue
		}
		block := straightBlock{start: start, end: pc}
		return block, enteredOnlyBy(sources, block, jumpPC)
	}
	return straightBlock{start: 0, end: 0}, false
}

// fallsInto reports whether control can fall from the last live instruction before pc
// into pc.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes dead ([]bool) which marks words that will be deleted.
// Takes pc (int) which is the block start.
//
// Returns true unless that instruction is an unconditional jump or a return.
func fallsInto(body []isa.Instruction, dead []bool, pc int) bool {
	previous := -1
	for start := 0; start < pc; start += instructionFootprint(body, start) {
		if !dead[start] {
			previous = start
		}
	}
	if previous < 0 {
		return true
	}
	inst := body[previous]
	return !program.IsReturnInstruction(inst) && !isa.InstrIsTier1SubOp(inst, isa.SubOpJump)
}

// enteredOnlyBy reports whether every jump into block comes from inside it, except the
// entering jump at jumpPC, which may land on its first word.
//
// Takes sources (map[int][]int) which lists the jumps landing on each PC.
// Takes block (straightBlock) which is the candidate block.
// Takes jumpPC (int) which is the entering jump.
//
// Returns true when the block has no other entry.
func enteredOnlyBy(sources map[int][]int, block straightBlock, jumpPC int) bool {
	for pc := block.start; pc <= block.end; pc++ {
		for _, source := range sources[pc] {
			inside := source >= block.start && source <= block.end
			if !inside && (source != jumpPC || pc != block.start) {
				return false
			}
		}
	}
	return true
}

// claimedRange reports whether block overlaps a block or jump already chosen.
//
// Takes claimed ([]bool) which marks chosen words.
// Takes block (straightBlock) which is the candidate block.
//
// Returns true on overlap.
func claimedRange(claimed []bool, block straightBlock) bool {
	for pc := block.start; pc <= block.end; pc++ {
		if claimed[pc] {
			return true
		}
	}
	return false
}

// jumpSourcesByTarget lists the jumps landing on each PC.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns the source PCs keyed by target PC.
func jumpSourcesByTarget(body []isa.Instruction) map[int][]int {
	sources := make(map[int][]int)
	for pc := range body {
		if target, ok := program.JumpTargetAt(body, pc); ok {
			sources[target] = append(sources[target], pc)
		}
	}
	return sources
}

// threadJumps points every jump whose target is an unconditional jump at that jump's
// final target, leaving any jump whose encoding cannot reach it.
//
// Takes body ([]isa.Instruction) which is rewritten in place.
//
// Returns true when some jump was retargeted.
func threadJumps(body []isa.Instruction) bool {
	changed := false
	for pc := 0; pc < len(body); pc += instructionFootprint(body, pc) {
		target, ok := program.JumpTargetAt(body, pc)
		if !ok {
			continue
		}
		final := target
		for range maxJumpThreadSteps {
			next, jump := program.JumpTargetAt(body, final)
			if final >= len(body) || !jump || !isa.InstrIsTier1SubOp(body[final], isa.SubOpJump) || next == pc {
				break
			}
			final = next
		}
		if final != target && program.SetJumpTarget(body, pc, final) {
			changed = true
		}
	}
	return changed
}
