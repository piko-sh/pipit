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

// maxSliceChainLoopWords bounds code growth when retaining a loop's initial traversal.
const maxSliceChainLoopWords = 128

// hoistLoopInvariantSliceChains retains the initial loop traversal and removes its stable
// global-to-byte-field chain from later traversals.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes analysis (*functionAnalysis) which caches control-flow facts and is invalidated
// by a rewrite.
func hoistLoopInvariantSliceChains(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) {
	body := compiledFunction.Body
	if typeSwitchFor(analysis, body) {
		return
	}
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if loop.latch-loop.header+1 <= maxSliceChainLoopWords && peelSliceChainInLoop(compiledFunction, analysis, loop) {
			analysis.invalidate()
			return
		}
	}
}

// peelSliceChainInLoop peels the first stable slice chain found in loop.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes analysis (*functionAnalysis) which supplies dominators.
// Takes loop (loopRange) which is the candidate loop.
//
// Returns true when the loop was peeled.
func peelSliceChainInLoop(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, loop loopRange) bool {
	body := compiledFunction.Body
	for pc := loop.header; pc+2 < loop.latch; pc++ {
		chain, ok := sliceChainAt(body, pc)
		if !ok || !sliceChainLoopPreservesRead(compiledFunction, loop, pc, chain) {
			continue
		}
		width := chain.width
		if !sliceChainLoopHasSingleEntry(body, loop) {
			return false
		}
		dominators := dominatorsFor(analysis, body)
		if dominators != nil && dominators.Dominates(pc, loop.latch) && peelSliceChainLoop(compiledFunction, loop, pc, width) {
			return true
		}
	}
	return false
}

// sliceChain is a byte-slice field read the loop copy can reuse, optionally with the
// global load that produces its receiver.
type sliceChain struct {
	// width counts the chain's words.
	width int

	// receiver is the general register the field is read through.
	receiver uint8

	// sliceReg is the byte-slice register the field is read into.
	sliceReg uint8

	// definesReceiver is true when the chain starts with the receiver's global load.
	definesReceiver bool
}

// sliceChainAt matches a byte-slice field read, preceded by the general global load that
// defines its receiver when there is one.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the candidate chain start.
//
// Returns the chain and whether one matched.
func sliceChainAt(body []isa.Instruction, pc int) (sliceChain, bool) {
	none := sliceChain{width: 0, receiver: 0, sliceReg: 0, definesReceiver: false}
	if read, ok := byteSliceFieldReadAt(body, pc); ok {
		return sliceChain{width: wideFieldReadWords, receiver: read.C, sliceReg: read.B, definesReceiver: false}, true
	}
	kind, receiver, width, ok := globalLoadDestination(body, pc)
	if !ok || kind != isa.RegisterGeneral {
		return none, false
	}
	read, ok := byteSliceFieldReadAt(body, pc+width)
	if !ok || read.C != receiver {
		return none, false
	}
	return sliceChain{width: width + wideFieldReadWords, receiver: receiver, sliceReg: read.B, definesReceiver: true}, true
}

// byteSliceFieldReadAt matches a byte-slice field read and its extension word.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the candidate read.
//
// Returns the read and whether it matched.
func byteSliceFieldReadAt(body []isa.Instruction, pc int) (isa.Instruction, bool) {
	if pc+1 >= len(body) || !isa.InstrIsTier1SubOp(body[pc], isa.SubOpGetStructFieldSliceByte) || body[pc+1].Op != isa.OpExt {
		return isa.Instruction{}, false
	}
	return body[pc], true
}

// sliceChainLoopHasSingleEntry checks for an unconditional latch and rejects side entries
// and nested back-edges.
//
// Takes body ([]isa.Instruction) which contains the instruction stream.
// Takes loop (loopRange) which identifies the loop header and latch.
//
// Returns true when copying the loop preserves its entry paths.
func sliceChainLoopHasSingleEntry(body []isa.Instruction, loop loopRange) bool {
	if !loopHeaderReachableByFallThrough(body, loop) || !isa.InstrIsTier1SubOp(body[loop.latch], isa.SubOpJump) {
		return false
	}
	for pc := range body {
		target, jump := program.JumpTargetAt(body, pc)
		if !jump {
			continue
		}
		inside := pc >= loop.header && pc <= loop.latch
		if !inside && target > loop.header && target <= loop.latch {
			return false
		}
		if inside && target <= pc && (pc != loop.latch || target != loop.header) {
			return false
		}
	}
	return true
}

// sliceChainLoopPreservesRead verifies that a cached byte-field chain remains valid
// across loop traversals.
//
// A chain that loads its receiver needs the receiver dead after it, since the copy no
// longer defines it; a chain that reads through an existing receiver needs no loop
// instruction to write that receiver.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes loop (loopRange) which identifies the loop header and latch.
// Takes readPC (int) which starts the candidate chain.
// Takes chain (sliceChain) which describes the chain.
//
// Returns false for possible header mutations, calls, register clobbers, or interior
// branches.
func sliceChainLoopPreservesRead(compiledFunction *program.CompiledFunction, loop loopRange, readPC int, chain sliceChain) bool {
	body := compiledFunction.Body
	if chain.definesReceiver && !registerDeadFrom(compiledFunction, body, readPC+chain.width, isa.RegisterGeneral, chain.receiver) {
		return false
	}
	for pc := loop.header; pc <= loop.latch; pc++ {
		if target, jump := program.JumpTargetAt(body, pc); jump && target > readPC && target < readPC+chain.width {
			return false
		}
		if (pc < readPC || pc >= readPC+chain.width) && !sliceChainSurvives(compiledFunction, body[pc], chain) {
			return false
		}
	}
	return true
}

// sliceChainSurvives reports whether inst, a loop instruction outside the chain, leaves
// the cached byte slice valid.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the call sites.
// Takes inst (isa.Instruction) which is the loop instruction.
// Takes chain (sliceChain) which describes the chain.
//
// Returns false for calls, opaque shapes, writes to the slice or receiver, and heap
// writes other than element stores.
func sliceChainSurvives(compiledFunction *program.CompiledFunction, inst isa.Instruction, chain sliceChain) bool {
	if IsCallInstruction(inst) || !instructionShapeAllowsCseScan(inst) ||
		instructionMayWriteRegister(compiledFunction, inst, isa.RegisterSliceByte, chain.sliceReg) {
		return false
	}
	if !chain.definesReceiver && inst.Op != isa.OpExt && instructionMayWriteRegister(compiledFunction, inst, isa.RegisterGeneral, chain.receiver) {
		return false
	}
	return !InstructionDirectlyMutatesHeap(inst) || elementStorePreservesBinding(inst, isa.RegisterGeneral)
}

// peeledLoopPCMap maps an original loop to its retained initial traversal and a copy
// without the invariant load chain.
type peeledLoopPCMap struct {
	// loop is the original instruction range, including its latch.
	loop loopRange

	// readPC starts the invariant chain removed from the copy.
	readPC int

	// width is the number of words in the chain.
	width int
}

// original maps a PC in the retained instruction stream.
//
// Takes pc (int) which identifies an original instruction word.
//
// Returns int which is the relocated PC.
func (m peeledLoopPCMap) original(pc int) int {
	if pc > m.loop.latch {
		return pc + m.loop.latch - m.loop.header + 1 - m.width
	}
	return pc
}

// copied maps a PC into the repeated loop body, mapping omitted words to their successor.
//
// Takes pc (int) which identifies an original instruction word.
//
// Returns int which is the relocated PC.
func (m peeledLoopPCMap) copied(pc int) int {
	if pc < m.loop.header || pc > m.loop.latch+1 {
		return m.original(pc)
	}
	return m.loop.latch + 1 + pc - m.loop.header - min(max(pc-m.readPC, 0), m.width)
}

// omitted identifies words in the load chain removed from the repeated loop body.
//
// Takes pc (int) which identifies an original instruction word.
//
// Returns true when the word is omitted.
func (m peeledLoopPCMap) omitted(pc int) bool { return pc >= m.readPC && pc < m.readPC+m.width }

// inCopy reports whether the word at pc also appears in the loop copy.
//
// Takes pc (int) which identifies an original instruction word.
//
// Returns true for loop words outside the omitted chain.
func (m peeledLoopPCMap) inCopy(pc int) bool {
	return pc >= m.loop.header && pc <= m.loop.latch && !m.omitted(pc)
}

// peelSliceChainLoop retains the initial loop traversal and removes the invariant chain
// from a repeated-traversal copy.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes loop (loopRange) which identifies the loop header and latch.
// Takes readPC (int) which starts the invariant chain.
// Takes width (int) which counts its instruction words.
//
// Returns false without changing the function when metadata or a jump cannot be remapped.
func peelSliceChainLoop(compiledFunction *program.CompiledFunction, loop loopRange, readPC, width int) bool {
	old := compiledFunction.Body
	sourceMap := compiledFunction.DebugSourceMap
	if sourceMap != nil && len(sourceMap.Positions) != len(old) {
		return false
	}
	mapping := peeledLoopPCMap{loop: loop, readPC: readPC, width: width}
	body := peelWords(old, mapping)
	if !retargetPeeledJumps(old, body, mapping) {
		return false
	}
	if sourceMap != nil {
		sourceMap.Positions = peelWords(sourceMap.Positions, mapping)
	}
	compiledFunction.PeepholeProvenance = remapPeeledProvenance(compiledFunction.PeepholeProvenance, mapping)
	compiledFunction.ArenaSafeAllocPCs = remapPeeledLoopEntries(compiledFunction.ArenaSafeAllocPCs, mapping)
	compiledFunction.FieldStoreArenaSafePCs = remapPeeledLoopEntries(compiledFunction.FieldStoreArenaSafePCs, mapping)
	compiledFunction.InPlaceHeaderReusePCs = remapPeeledLoopEntries(compiledFunction.InPlaceHeaderReusePCs, mapping)
	compiledFunction.GetMethodReceiverTypeNames = remapPeeledLoopEntries(compiledFunction.GetMethodReceiverTypeNames, mapping)
	remapPeeledLoopVariables(compiledFunction.DebugVarTable, mapping, len(old))
	compiledFunction.Body = body
	compiledFunction.AliasInfo = nil
	RecordPeepholeRewrite(compiledFunction, loop.latch+1, peepholeRewriteLicmPeel, readPC)
	return true
}

// peelWords lays out a PC-parallel slice for the peeled body: the original words, then a
// copy of the loop without the chain, then the words after the loop.
//
// Takes words ([]T) which is parallel to the old body.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
//
// Returns the slice parallel to the new body.
func peelWords[T any](words []T, mapping peeledLoopPCMap) []T {
	loop := mapping.loop
	peeled := make([]T, 0, len(words)+loop.latch-loop.header+1-mapping.width)
	peeled = append(peeled, words[:loop.latch+1]...)
	peeled = append(peeled, words[loop.header:mapping.readPC]...)
	peeled = append(peeled, words[mapping.readPC+mapping.width:loop.latch+1]...)
	return append(peeled, words[loop.latch+1:]...)
}

// retargetPeeledJumps re-encodes every jump of the original words and of the loop copy.
// The original latch falls into the copy; jumps in the copy that stay in the loop stay in
// the copy.
//
// Takes old ([]isa.Instruction) which is the body before peeling.
// Takes body ([]isa.Instruction) which is the peeled body, rewritten in place.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
//
// Returns false when a jump cannot be encoded.
func retargetPeeledJumps(old, body []isa.Instruction, mapping peeledLoopPCMap) bool {
	loop := mapping.loop
	for pc := range old {
		target, jump := program.JumpTargetAt(old, pc)
		if !jump {
			continue
		}
		next := mapping.original(target)
		if pc == loop.latch {
			next = loop.latch + 1
		}
		if !program.SetJumpTarget(body, mapping.original(pc), next) {
			return false
		}
		if !mapping.inCopy(pc) {
			continue
		}
		next = mapping.original(target)
		if target >= loop.header && target <= loop.latch {
			next = mapping.copied(target)
		}
		if !program.SetJumpTarget(body, mapping.copied(pc), next) {
			return false
		}
	}
	return true
}

// remapPeeledProvenance moves each annotation to the original word and, for loop words,
// duplicates it on the copy with its origin moved into the copy too.
//
// Takes provenance (map[int]program.PeepholeAnnotation) which is the old map.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
//
// Returns the rebuilt map.
func remapPeeledProvenance(provenance map[int]program.PeepholeAnnotation, mapping peeledLoopPCMap) map[int]program.PeepholeAnnotation {
	rebuilt := make(map[int]program.PeepholeAnnotation, len(provenance))
	for pc, annotation := range provenance {
		copied := annotation
		if annotation.Origin >= 0 {
			annotation.Origin = mapping.original(annotation.Origin)
			copied.Origin = mapping.copied(copied.Origin)
		}
		rebuilt[mapping.original(pc)] = annotation
		if mapping.inCopy(pc) {
			rebuilt[mapping.copied(pc)] = copied
		}
	}
	return rebuilt
}

// remapPeeledLoopEntries duplicates and relocates PC-keyed entries.
//
// Takes entries (map[K]V) which contains the original annotations.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
//
// Returns map[K]V containing remapped entries, preserving nil input.
func remapPeeledLoopEntries[K ~int | ~uint32, V any](entries map[K]V, mapping peeledLoopPCMap) map[K]V {
	if entries == nil {
		return nil
	}
	result := make(map[K]V, len(entries))
	for key, value := range entries {
		pc := int(key)
		result[K(mapping.original(pc))] = value
		if mapping.inCopy(pc) {
			result[K(mapping.copied(pc))] = value
		}
	}
	return result
}

// remapPeeledLoopVariables preserves variable scope intervals across a loop copy.
//
// Takes table (*program.DebugVarTable) which holds the variable scopes or is nil.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
// Takes bodyLen (int) which resolves scopes extending to the original function end.
func remapPeeledLoopVariables(table *program.DebugVarTable, mapping peeledLoopPCMap, bodyLen int) {
	if table == nil {
		return
	}
	var entries []program.DebugVarEntry
	for _, entry := range table.Entries {
		entries = appendPeeledScope(entries, entry, mapping, bodyLen)
	}
	table.Entries = entries
}

// appendPeeledScope appends the pieces of one variable scope: its part up to the original
// latch, its part inside the loop again for the copy, and its part after the loop.
//
// Takes entries ([]program.DebugVarEntry) which receives the pieces.
// Takes entry (program.DebugVarEntry) which is the scope before peeling.
// Takes mapping (peeledLoopPCMap) which describes the loop copy and omitted chain.
// Takes bodyLen (int) which resolves a scope that runs to the function end.
//
// Returns the extended entries.
func appendPeeledScope(entries []program.DebugVarEntry, entry program.DebugVarEntry, mapping peeledLoopPCMap, bodyLen int) []program.DebugVarEntry {
	end := entry.EndPC
	if end == 0 {
		end = bodyLen
	}
	appendRange := func(start, stop int) {
		if start >= stop {
			return
		}
		piece := entry
		piece.StartPC, piece.EndPC = start, stop
		if entry.EndPC == 0 && stop == mapping.original(bodyLen) {
			piece.EndPC = 0
		}
		entries = append(entries, piece)
	}
	boundary := mapping.loop.latch + 1
	appendRange(entry.StartPC, min(end, boundary))
	if start, stop := max(entry.StartPC, mapping.loop.header), min(end, boundary); start < stop {
		appendRange(mapping.copied(start), mapping.copied(stop))
	}
	if end > boundary {
		appendRange(mapping.original(max(entry.StartPC, boundary)), mapping.original(end))
	}
	return entries
}
