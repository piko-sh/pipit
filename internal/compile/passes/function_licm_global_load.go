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
	"fmt"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// hoistLoopInvariantGlobalLoads moves stable global loads into loop preheaders.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes analysis (*functionAnalysis) which caches control-flow facts and is invalidated
// by a rewrite.
//
// Returns error when cancellation interrupts the pass.
func hoistLoopInvariantGlobalLoads(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	if analysis == nil {
		analysis = newFunctionAnalysis(compiledFunction)
	}
	for range maxLicmHoistsPerFunction {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("hoistLoopInvariantGlobalLoads cancelled: %w", err)
		}
		if !tryOneGlobalLoadHoist(compiledFunction, analysis) {
			return nil
		}
	}
	return nil
}

// tryOneGlobalLoadHoist moves one invariant global load.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes analysis (*functionAnalysis) which caches control-flow facts and is invalidated
// by a rewrite.
//
// Returns true when a load was moved.
func tryOneGlobalLoadHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) bool {
	body := compiledFunction.Body
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if !loopHeaderReachableByFallThrough(body, loop) {
			continue
		}
		facts := newLoopFacts(analysis, loop)
		if pc, width, ok := findHoistableGlobalLoad(compiledFunction, facts); ok {
			applyLoopHoistWords(compiledFunction, analysis, loop.header, pc, width)
			return true
		}
		if facts.renamed {
			return true
		}
	}
	return false
}

// findHoistableGlobalLoad finds the first global load in the loop that can move to its
// pre-header.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes facts (*loopFacts) which supplies the loop's members and dominators.
//
// Returns the load's PC and word count, and whether one was found.
func findHoistableGlobalLoad(compiledFunction *program.CompiledFunction, facts *loopFacts) (pc, width int, ok bool) {
	body := compiledFunction.Body
	loop := facts.loop
	for pc = loop.header; pc <= loop.latch; pc++ {
		kind, reg, width, isLoad := globalLoadDestination(body, pc)
		if !isLoad {
			continue
		}
		members := facts.loopMembers()
		if len(members) == 0 {
			return 0, 0, false
		}
		if globalLoadHoistable(compiledFunction, facts, members, pc, kind, reg, width) {
			return pc, width, true
		}
		if facts.renamed {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// globalLoadHoistable reports whether the global load at pc is invariant, safe to run on
// loop entry, and runs on every traversal.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes facts (*loopFacts) which supplies the loop's dominators.
// Takes members ([]int) which lists the loop instruction PCs.
// Takes pc (int) which starts the candidate load.
// Takes kind (isa.RegisterKind) which selects the destination bank.
// Takes reg (uint8) which identifies the destination register.
// Takes width (int) which counts the load instruction words.
//
// Returns true when the load can move to the pre-header.
func globalLoadHoistable(compiledFunction *program.CompiledFunction, facts *loopFacts, members []int, pc int, kind isa.RegisterKind, reg uint8, width int) bool {
	loop := facts.loop
	if !globalBindingStableInLoop(compiledFunction, members, pc, kind, width) {
		return false
	}
	if loopWritesRegisterElsewhere(compiledFunction, members, pc, kind, reg) {
		if width == 1 && facts.dominatesLatch(pc) && renameDefinition(compiledFunction, facts.analysis, pc, 1, kind, reg) {
			facts.renamed = true
		}
		return false
	}
	if registerReadInRange(compiledFunction, loop.header, pc, kind, reg) || !constantHoistSafeAcrossLoopEntry(compiledFunction, loop, pc, kind, reg) {
		return false
	}
	dominators := facts.dominators()
	return dominators != nil && dominators.Dominates(loop.header, pc) && dominators.Dominates(pc, loop.latch)
}

// globalLoadDestination decodes a global-load instruction.
//
// Takes body ([]isa.Instruction) which contains the instruction stream.
// Takes pc (int) which identifies an original instruction word.
//
// Returns isa.RegisterKind which selects the destination bank.
// Returns uint8 which identifies the destination register.
// Returns int which counts the load instruction words.
// Returns bool which is true when the global load is supported.
func globalLoadDestination(body []isa.Instruction, pc int) (isa.RegisterKind, uint8, int, bool) {
	inst := body[pc]
	if inst.Op == isa.OpGetGlobal {
		return isa.RegisterKind(inst.C), inst.A, 1, true
	}
	if isa.InstrIsTier1SubOp(inst, isa.SubOpGetGlobalWide) && pc+1 < len(body) && body[pc+1].Op == isa.OpExt {
		return isa.RegisterKind(inst.C), inst.B, 2, true
	}
	return 0, 0, 0, false
}

// globalBindingStableInLoop checks that no loop member can change the global a load
// reads: no calls, no undescribed shapes, and no heap writes other than element stores
// that cannot rebind a general-bank global.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes members ([]int) which lists the loop instruction PCs.
// Takes loadPC (int) which starts the candidate load.
// Takes kind (isa.RegisterKind) which selects the destination bank.
// Takes width (int) which counts the load instruction words.
//
// Returns false when the global may change during the loop.
func globalBindingStableInLoop(compiledFunction *program.CompiledFunction, members []int, loadPC int, kind isa.RegisterKind, width int) bool {
	for _, pc := range members {
		if pc >= loadPC && pc < loadPC+width {
			continue
		}
		inst := compiledFunction.Body[pc]
		if IsCallInstruction(inst) || !instructionShapeAllowsCseScan(inst) {
			return false
		}
		if InstructionDirectlyMutatesHeap(inst) && !elementStorePreservesBinding(inst, kind) {
			return false
		}
	}
	return true
}

// elementStorePreservesBinding reports whether a heap-mutating instruction leaves a
// global's binding unchanged: a byte or uint element store changes slice contents, never
// a general-bank global's value or a slice header.
//
// Takes inst (isa.Instruction) which mutates the heap.
// Takes kind (isa.RegisterKind) which is the global's bank.
//
// Returns true when the store cannot change the global.
func elementStorePreservesBinding(inst isa.Instruction, kind isa.RegisterKind) bool {
	return kind == isa.RegisterGeneral && (isa.InstrIsTier1SubOp(inst, isa.SubOpSliceSetByteDirect) || inst.Op == isa.OpSliceSetUint)
}
