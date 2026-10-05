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

// globalSliceChainWords is the word count of a global load, a byte-slice field read
// through it, and the read's layout extension word.
const globalSliceChainWords = 3

// elideRepeatedGlobalSliceFieldRead reuses a byte-slice field loaded from an unchanged
// global receiver within a basic block. Byte element stores preserve the header.
//
// A repeated chain becomes NOPs, which the final compaction deletes.
//
// Takes compiledFunction (*program.CompiledFunction) which receives rewrite provenance.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes first (int) which is the global load preceding the field read.
// Takes targets (map[int]bool) which marks branch destinations.
func elideRepeatedGlobalSliceFieldRead(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int, targets map[int]bool) {
	if !globalSliceChainAt(body, first, targets) {
		return
	}
	chain := body[first : first+globalSliceChainWords]
	for pc := first + globalSliceChainWords; pc < min(len(body), first+4*maxCseFieldReadScanWindow); pc++ {
		if targets[pc] {
			return
		}
		if globalSliceChainRepeats(body, pc, chain, targets) {
			for offset := range globalSliceChainWords {
				body[pc+offset] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
			}
			RecordPeepholeRewrite(compiledFunction, pc, peepholeRewriteCseGlobalSlice, first)
			pc += globalSliceChainWords - 1
			continue
		}
		if globalSliceChainInvalidated(compiledFunction, body[pc], chain) {
			return
		}
	}
}

// globalSliceChainAt reports whether a general global load at first is followed by a
// byte-slice field read through it, with no jump landing inside the chain.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes first (int) which is the candidate global load.
// Takes targets (map[int]bool) which marks branch destinations.
//
// Returns true when the chain matches.
func globalSliceChainAt(body []isa.Instruction, first int, targets map[int]bool) bool {
	if first+globalSliceChainWords > len(body) || targets[first+1] || targets[first+2] {
		return false
	}
	global, read, layout := body[first], body[first+1], body[first+2]
	return global.Op == isa.OpGetGlobal && isa.RegisterKind(global.C) == isa.RegisterGeneral &&
		isa.InstrIsTier1SubOp(read, isa.SubOpGetStructFieldSliceByte) && read.C == global.A && layout.Op == isa.OpExt
}

// globalSliceChainRepeats reports whether the words at pc repeat chain exactly, with no
// jump landing inside them.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the candidate repeat.
// Takes chain ([]isa.Instruction) which is the first chain.
// Takes targets (map[int]bool) which marks branch destinations.
//
// Returns true when the repeat can be elided.
func globalSliceChainRepeats(body []isa.Instruction, pc int, chain []isa.Instruction, targets map[int]bool) bool {
	if pc+globalSliceChainWords > len(body) {
		return false
	}
	for offset, inst := range chain {
		if body[pc+offset] != inst || (offset > 0 && targets[pc+offset]) {
			return false
		}
	}
	return true
}

// globalSliceChainInvalidated reports whether inst may change the global, its field, or
// either register the chain wrote.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the call sites.
// Takes inst (isa.Instruction) which follows the first chain.
// Takes chain ([]isa.Instruction) which is the first chain.
//
// Returns true when a later repeat must reload.
func globalSliceChainInvalidated(compiledFunction *program.CompiledFunction, inst isa.Instruction, chain []isa.Instruction) bool {
	if invalidatesCachedFieldReads(compiledFunction, inst) && !isa.InstrIsTier1SubOp(inst, isa.SubOpSliceSetByteDirect) {
		return true
	}
	return !instructionShapeAllowsCseScan(inst) ||
		instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, chain[0].A) ||
		instructionWritesRegisterInBank(inst, isa.RoleForKind(isa.RegisterSliceByte), chain[1].B)
}
