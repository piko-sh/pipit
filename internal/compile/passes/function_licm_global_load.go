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
	var dominators *functionDominators
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if !loopHeaderReachableByFallThrough(body, loop) {
			continue
		}
		var members []int
		for pc := loop.header; pc <= loop.latch; pc++ {
			kind, reg, width, ok := globalLoadDestination(body, pc)
			if !ok {
				continue
			}
			if members == nil {
				members = naturalLoopMembers(analysis, loop)
				if len(members) == 0 {
					break
				}
			}
			if !globalLoadIsLoopInvariant(compiledFunction, members, pc, kind, reg, width) {
				continue
			}
			if registerReadInRange(compiledFunction, loop.header, pc, kind, reg) || !constantHoistSafeAcrossLoopEntry(compiledFunction, loop, pc, kind, reg) {
				continue
			}
			if dominators == nil {
				dominators = dominatorsFor(analysis, body)
				if dominators == nil {
					return false
				}
			}
			if !dominators.Dominates(loop.header, pc) || !dominators.Dominates(pc, loop.latch) {
				continue
			}
			applyLoopHoistWords(compiledFunction, analysis, loop.header, pc, width)
			return true
		}
	}
	return false
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

// globalLoadIsLoopInvariant checks the binding and destination of a global load across
// every loop member.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the input bytecode and
// metadata.
// Takes members ([]int) which lists the loop instruction PCs.
// Takes loadPC (int) which starts the candidate load.
// Takes kind (isa.RegisterKind) which selects the destination bank.
// Takes reg (uint8) which identifies the destination register.
// Takes width (int) which counts the load instruction words.
//
// Returns false for calls, unresolved writes, or changes to either value.
func globalLoadIsLoopInvariant(compiledFunction *program.CompiledFunction, members []int, loadPC int, kind isa.RegisterKind, reg uint8, width int) bool {
	for _, pc := range members {
		if pc >= loadPC && pc < loadPC+width {
			continue
		}
		inst := compiledFunction.Body[pc]
		if IsCallInstruction(inst) || !instructionShapeAllowsCseScan(inst) || instructionMayWriteRegister(compiledFunction, inst, kind, reg) {
			return false
		}
		if InstructionDirectlyMutatesHeap(inst) {
			if kind != isa.RegisterGeneral {
				return false
			}
			if !isa.InstrIsTier1SubOp(inst, isa.SubOpSliceSetByteDirect) && inst.Op != isa.OpSliceSetUint {
				return false
			}
		}
	}
	return true
}
