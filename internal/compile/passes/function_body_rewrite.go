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

// pcMapper maps an old program counter to its new one, reporting false when the word at
// that PC no longer exists.
type pcMapper func(pc int) (int, bool)

// bodyRewrite describes a body rebuilt by inserting, deleting and reordering words.
type bodyRewrite struct {
	// body is the new instruction stream. Jumps still hold their old offsets; apply
	// re-encodes them.
	body []isa.Instruction

	// origin holds, for each new word, the old PC it was copied from, or -1 for an inserted
	// word.
	origin []int

	// positionOf holds, for each new word, the old PC whose source position it takes.
	positionOf []int

	// newPC holds, for each old PC and for the old body length, the new PC. A deleted word
	// maps to the word appended after it, so jumps to it fall through as before.
	newPC []int
}

// newBodyRewrite allocates a rewrite of a body with oldLength words.
//
// Takes oldLength (int) which is the length of the original body.
// Takes capacity (int) which is the expected length of the new body.
//
// Returns the empty rewrite.
func newBodyRewrite(oldLength, capacity int) *bodyRewrite {
	return &bodyRewrite{
		body:       make([]isa.Instruction, 0, capacity),
		origin:     make([]int, 0, capacity),
		positionOf: make([]int, 0, capacity),
		newPC:      make([]int, oldLength+1),
	}
}

// keep appends the old word at pc unchanged and records where it landed.
//
// Takes old ([]isa.Instruction) which is the original body.
// Takes pc (int) which is the old PC of the word to keep.
func (rewrite *bodyRewrite) keep(old []isa.Instruction, pc int) {
	rewrite.keepAs(old[pc], pc)
}

// keepAs appends inst in place of the old word at pc. The word keeps pc's annotations, so
// inst must not be a jump unless the old word was the same jump.
//
// Takes inst (isa.Instruction) which replaces the old word.
// Takes pc (int) which is the old PC of the replaced word.
func (rewrite *bodyRewrite) keepAs(inst isa.Instruction, pc int) {
	rewrite.newPC[pc] = len(rewrite.body)
	rewrite.body = append(rewrite.body, inst)
	rewrite.origin = append(rewrite.origin, pc)
	rewrite.positionOf = append(rewrite.positionOf, pc)
}

// insert appends a new word that takes the source position of the old word at positionPC.
//
// Takes inst (isa.Instruction) which is the inserted word.
// Takes positionPC (int) which is the old PC whose source position the word takes.
func (rewrite *bodyRewrite) insert(inst isa.Instruction, positionPC int) {
	rewrite.body = append(rewrite.body, inst)
	rewrite.origin = append(rewrite.origin, -1)
	rewrite.positionOf = append(rewrite.positionOf, positionPC)
}

// drop records that the old word at pc is deleted. Jumps to it land on the next word
// appended, inserted or kept, so drop a replaced word before inserting its replacement.
//
// Takes pc (int) which is the old PC of the deleted word.
func (rewrite *bodyRewrite) drop(pc int) {
	rewrite.newPC[pc] = len(rewrite.body)
}

// finish maps the old body length to the new one, so jumps that fall off the end of the
// body still do.
func (rewrite *bodyRewrite) finish() {
	rewrite.newPC[len(rewrite.newPC)-1] = len(rewrite.body)
}

// survivor maps an old PC to its new PC, reporting false when the word was deleted or
// inserted words replaced it; the PC is then that of the word that took its place.
//
// Takes pc (int) which is the old PC.
//
// Returns the new PC and whether the word survived.
func (rewrite *bodyRewrite) survivor(pc int) (int, bool) {
	next := rewrite.scopePC(pc)
	return next, pc >= 0 && next < len(rewrite.origin) && rewrite.origin[next] == pc
}

// remapScopes moves each variable scope to the span of its surviving words, so a scope
// stays correct when words are reordered as well as inserted or deleted.
//
// A scope whose words were all deleted becomes empty at the word that replaced them. An
// end PC of zero means "to the end of the function" and is kept.
//
// Takes table (*program.DebugVarTable) which may be nil.
func (rewrite *bodyRewrite) remapScopes(table *program.DebugVarTable) {
	if table == nil {
		return
	}
	oldLength := len(rewrite.newPC) - 1
	for index := range table.Entries {
		entry := &table.Entries[index]
		start, end := max(entry.StartPC, 0), entry.EndPC
		if end == 0 || end > oldLength {
			end = oldLength
		}
		first, last := len(rewrite.body), -1
		for pc := start; pc < end; pc++ {
			if newPC, ok := rewrite.survivor(pc); ok {
				first, last = min(first, newPC), max(last, newPC)
			}
		}
		if last < 0 {
			first, last = rewrite.scopePC(start), rewrite.scopePC(start)-1
		}
		entry.StartPC = first
		if entry.EndPC != 0 {
			entry.EndPC = last + 1
		}
	}
}

// scopePC maps a variable-scope bound, clamping bounds past the old body to its end.
//
// Takes pc (int) which is an old scope bound.
//
// Returns the new bound.
func (rewrite *bodyRewrite) scopePC(pc int) int {
	return rewrite.newPC[min(max(pc, 0), len(rewrite.newPC)-1)]
}

// apply re-encodes every jump and moves the PC-keyed metadata, then installs the new
// body. Nothing changes when a jump cannot be re-encoded.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the old body.
//
// Returns false when the rewrite was refused.
func (rewrite *bodyRewrite) apply(compiledFunction *program.CompiledFunction) bool {
	old := compiledFunction.Body
	sourceMap := compiledFunction.DebugSourceMap
	if sourceMap != nil && len(sourceMap.Positions) != len(old) {
		return false
	}
	rewrite.finish()
	for newPC, oldPC := range rewrite.origin {
		if oldPC < 0 {
			continue
		}
		target, jump := program.JumpTargetAt(old, oldPC)
		if jump && !program.SetJumpTarget(rewrite.body, newPC, rewrite.newPC[target]) {
			return false
		}
	}
	if sourceMap != nil {
		positions := make([]program.SourcePosition, len(rewrite.body))
		for newPC, oldPC := range rewrite.positionOf {
			positions[newPC] = sourceMap.Positions[oldPC]
		}
		sourceMap.Positions = positions
	}
	rewrite.remapScopes(compiledFunction.DebugVarTable)
	remapPCKeyedMetadata(compiledFunction, rewrite.survivor)
	compiledFunction.Body = rewrite.body
	compiledFunction.AliasInfo = nil
	return true
}

// remapPCKeyedMetadata moves every PC-keyed annotation of compiledFunction through
// mapper, dropping entries whose word no longer exists.
//
// Takes compiledFunction (*program.CompiledFunction) whose annotations are rewritten.
// Takes mapper (pcMapper) which maps old PCs to new ones.
func remapPCKeyedMetadata(compiledFunction *program.CompiledFunction, mapper pcMapper) {
	compiledFunction.PeepholeProvenance = remapProvenance(compiledFunction.PeepholeProvenance, mapper)
	compiledFunction.ArenaSafeAllocPCs = remapPCKeyed(compiledFunction.ArenaSafeAllocPCs, mapper)
	compiledFunction.FieldStoreArenaSafePCs = remapPCKeyed(compiledFunction.FieldStoreArenaSafePCs, mapper)
	compiledFunction.InPlaceHeaderReusePCs = remapPCKeyed(compiledFunction.InPlaceHeaderReusePCs, mapper)
	compiledFunction.GetMethodReceiverTypeNames = remapPCKeyed(compiledFunction.GetMethodReceiverTypeNames, mapper)
}

// remapPCKeyed rebuilds a PC-keyed map through mapper, dropping entries whose word no
// longer exists.
//
// Takes entries (map[K]V) which is the old map; nil or empty is returned unchanged.
// Takes mapper (pcMapper) which maps old PCs to new ones.
//
// Returns the rebuilt map.
func remapPCKeyed[K ~int | ~uint32, V any](entries map[K]V, mapper pcMapper) map[K]V {
	if len(entries) == 0 {
		return entries
	}
	rebuilt := make(map[K]V, len(entries))
	for key, value := range entries {
		if pc, ok := mapper(int(key)); ok {
			rebuilt[K(pc)] = value
		}
	}
	return rebuilt
}

// remapProvenance rebuilds the provenance map through mapper, moving each annotation's
// origin as well and dropping annotations whose word no longer exists.
//
// Takes provenance (map[int]program.PeepholeAnnotation) which is the old map.
// Takes mapper (pcMapper) which maps old PCs to new ones.
//
// Returns the rebuilt map.
func remapProvenance(provenance map[int]program.PeepholeAnnotation, mapper pcMapper) map[int]program.PeepholeAnnotation {
	if len(provenance) == 0 {
		return provenance
	}
	rebuilt := make(map[int]program.PeepholeAnnotation, len(provenance))
	for pc, annotation := range provenance {
		newPC, ok := mapper(pc)
		if !ok {
			continue
		}
		if annotation.Origin >= 0 {
			annotation.Origin, _ = mapper(annotation.Origin)
		}
		rebuilt[newPC] = annotation
	}
	return rebuilt
}

// remapDebugVarScopes moves each variable scope's bounds through newPC. An end PC of zero
// means "to the end of the function" and is kept.
//
// Takes table (*program.DebugVarTable) which may be nil.
// Takes newPC (func(int) int) which maps an old PC, or the old body length, to a new one.
func remapDebugVarScopes(table *program.DebugVarTable, newPC func(int) int) {
	if table == nil {
		return
	}
	for index := range table.Entries {
		entry := &table.Entries[index]
		entry.StartPC = newPC(entry.StartPC)
		if entry.EndPC != 0 {
			entry.EndPC = newPC(entry.EndPC)
		}
	}
}
