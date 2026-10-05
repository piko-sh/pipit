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
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if loop.latch-loop.header+1 > maxSliceChainLoopWords {
			continue
		}
		for pc := loop.header; pc+2 < loop.latch; pc++ {
			kind, receiver, width, ok := globalLoadDestination(body, pc)
			if !ok || kind != isa.RegisterGeneral || pc+width+1 >= len(body) {
				continue
			}
			read := body[pc+width]
			if !isa.InstrIsTier1SubOp(read, isa.SubOpGetStructFieldSliceByte) || read.C != receiver || body[pc+width+1].Op != isa.OpExt {
				continue
			}
			width += 2
			if !sliceChainLoopPreservesRead(compiledFunction, loop, pc, width, receiver, read.B) {
				continue
			}
			if !sliceChainLoopHasSingleEntry(body, loop) {
				break
			}
			dominators := dominatorsFor(analysis, body)
			if dominators == nil || !dominators.Dominates(pc, loop.latch) {
				continue
			}
			if peelSliceChainLoop(compiledFunction, loop, pc, width) {
				analysis.invalidate()
				return
			}
		}
	}
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
	for pc, inst := range body {
		if isa.InstrIsTier1SubOp(inst, isa.SubOpTypeSwitchJump) {
			return false
		}
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
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes loop (loopRange) which identifies the loop header and latch.
// Takes readPC (int) which starts the candidate chain.
// Takes width (int) which counts the chain instruction words.
// Takes receiver (uint8) which names the general register that must be dead after the
// chain.
// Takes sliceReg (uint8) which names the cached byte-slice register.
//
// Returns false for possible header mutations, calls, register clobbers, or interior
// branches.
func sliceChainLoopPreservesRead(compiledFunction *program.CompiledFunction, loop loopRange, readPC, width int, receiver, sliceReg uint8) bool {
	body := compiledFunction.Body
	if !registerDeadFrom(compiledFunction, body, readPC+width, isa.RegisterGeneral, receiver) {
		return false
	}
	for pc := loop.header; pc <= loop.latch; pc++ {
		inst := body[pc]
		if target, jump := program.JumpTargetAt(body, pc); jump && target > readPC && target < readPC+width {
			return false
		}
		if pc >= readPC && pc < readPC+width {
			continue
		}
		if IsCallInstruction(inst) || !instructionShapeAllowsCseScan(inst) || instructionMayWriteRegister(compiledFunction, inst, isa.RegisterSliceByte, sliceReg) {
			return false
		}
		if InstructionDirectlyMutatesHeap(inst) && !isa.InstrIsTier1SubOp(inst, isa.SubOpSliceSetByteDirect) && inst.Op != isa.OpSliceSetUint {
			return false
		}
	}
	return true
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
	if sm := compiledFunction.DebugSourceMap; sm != nil && len(sm.Positions) != len(old) {
		return false
	}
	mapping := peeledLoopPCMap{loop: loop, readPC: readPC, width: width}
	body := make([]isa.Instruction, 0, len(old)+loop.latch-loop.header+1-width)
	body = append(body, old[:loop.latch+1]...)
	body = append(body, old[loop.header:readPC]...)
	body = append(body, old[readPC+width:loop.latch+1]...)
	body = append(body, old[loop.latch+1:]...)
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
		if pc >= loop.header && pc <= loop.latch && !mapping.omitted(pc) {
			if target >= loop.header && target <= loop.latch {
				next = mapping.copied(target)
			} else {
				next = mapping.original(target)
			}
			if !program.SetJumpTarget(body, mapping.copied(pc), next) {
				return false
			}
		}
	}
	if sm := compiledFunction.DebugSourceMap; sm != nil && len(sm.Positions) == len(old) {
		positions := append(sm.Positions[:0:0], sm.Positions[:loop.latch+1]...)
		positions = append(positions, sm.Positions[loop.header:readPC]...)
		positions = append(positions, sm.Positions[readPC+width:loop.latch+1]...)
		sm.Positions = append(positions, sm.Positions[loop.latch+1:]...)
	}
	provenance := make(map[int]program.PeepholeAnnotation, len(compiledFunction.PeepholeProvenance))
	for pc, ann := range compiledFunction.PeepholeProvenance {
		copyAnn := ann
		if ann.Origin >= 0 {
			ann.Origin = mapping.original(ann.Origin)
			copyAnn.Origin = mapping.copied(copyAnn.Origin)
		}
		provenance[mapping.original(pc)] = ann
		if pc >= loop.header && pc <= loop.latch && !mapping.omitted(pc) {
			provenance[mapping.copied(pc)] = copyAnn
		}
	}
	compiledFunction.PeepholeProvenance = provenance
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
		if pc >= mapping.loop.header && pc <= mapping.loop.latch && !mapping.omitted(pc) {
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
	boundary := mapping.loop.latch + 1
	for _, entry := range table.Entries {
		end := entry.EndPC
		if end == 0 {
			end = bodyLen
		}
		appendRange := func(start, stop int) {
			if start >= stop {
				return
			}
			copyEntry := entry
			copyEntry.StartPC = start
			copyEntry.EndPC = stop
			if entry.EndPC == 0 && stop == mapping.original(bodyLen) {
				copyEntry.EndPC = 0
			}
			entries = append(entries, copyEntry)
		}
		appendRange(entry.StartPC, min(end, boundary))
		start, stop := max(entry.StartPC, mapping.loop.header), min(end, boundary)
		if start < stop {
			appendRange(mapping.copied(start), mapping.copied(stop))
		}
		if end > boundary {
			appendRange(mapping.original(max(entry.StartPC, boundary)), mapping.original(end))
		}
	}
	table.Entries = entries
}
