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

// maxLicmConstHoistsPerFunction caps the constant loads the loop-invariant code motion
// pass lifts per CompiledFunction, separately from the struct-field read cap so the two
// families never compete for a budget.
const maxLicmConstHoistsPerFunction = 8

// hoistLoopInvariantConstantLoads lifts loop-invariant constant loads to the pre-header,
// excluding fresh-storage loads that must produce a new value each execution.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes analysis (*FunctionAnalysis) which supplies loops and dominators and is
// invalidated by every hoist; nil is tolerated.
//
// Returns error when cancellation interrupts the pass.
func hoistLoopInvariantConstantLoads(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("hoistLoopInvariantConstantLoads cancelled: %w", err)
	}
	for range maxLicmConstHoistsPerFunction {
		if !tryOneConstantLoadHoist(compiledFunction, analysis) {
			return nil
		}
	}
	return nil
}

// tryOneConstantLoadHoist performs at most one constant-load hoist.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes analysis (*FunctionAnalysis) which supplies the loop list when non-nil.
//
// Returns true when a hoist was applied.
func tryOneConstantLoadHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) bool {
	loops := mergeLoopsSharingHeader(loopsFor(analysis, compiledFunction.Body))
	for _, loop := range loops {
		if !loopHeaderReachableByFallThrough(compiledFunction.Body, loop) {
			continue
		}
		loadPC, ok := findHoistableConstantLoadInLoop(compiledFunction, loop, analysis)
		if !ok {
			continue
		}
		applyLoopHoist(compiledFunction, analysis, loop.header, loadPC)
		return true
	}
	return false
}

// findHoistableConstantLoadInLoop locates the lowest-PC constant load in loop that
// dominates the latch, is the loop's only writer of its destination and is safe on the
// entry path.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes loop (LoopRange) which is the loop to search.
// Takes analysis (*FunctionAnalysis) which supplies dominators when non-nil.
//
// Returns the load's PC and true on success; (0, false) otherwise.
func findHoistableConstantLoadInLoop(compiledFunction *program.CompiledFunction, loop loopRange, analysis *functionAnalysis) (int, bool) {
	body := compiledFunction.Body
	dominators := dominatorsFor(analysis, body)
	if dominators == nil {
		return 0, false
	}
	for pc := loop.header; pc <= loop.latch; pc++ {
		kind, reg, ok := constantLoadDestination(body[pc])
		if !ok || !dominators.Dominates(pc, loop.latch) {
			continue
		}
		if !constantLoadIsLoopInvariant(compiledFunction, loop, pc, kind, reg) {
			continue
		}
		if !constantHoistSafeAcrossLoopEntry(compiledFunction, loop, pc, kind, reg) {
			continue
		}
		return pc, true
	}
	return 0, false
}

// constantLoadDestination classifies inst as a hoistable constant load and names the
// register it writes.
//
// Takes inst (isa.Instruction) which is the candidate instruction.
//
// Returns kind (isa.RegisterKind) which is the destination bank.
// Returns reg (uint8) which is the destination register.
// Returns ok (bool) which is false when inst is not in the hoistable family.
func constantLoadDestination(inst isa.Instruction) (kind isa.RegisterKind, reg uint8, ok bool) {
	switch inst.Op {
	case isa.OpLoadIntConst:
		return isa.RegisterInt, inst.A, true
	case isa.OpLoadFloatConst:
		return isa.RegisterFloat, inst.A, true
	case isa.OpLoadStringConst:
		return isa.RegisterString, inst.A, true
	case isa.OpLoadUintConst:
		return isa.RegisterUint, inst.A, true
	case isa.OpDrillTier1:
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpLoadIntConstSmall:
			return isa.RegisterInt, inst.B, true
		case isa.SubOpLoadUintConstSmall:
			return isa.RegisterUint, inst.B, true
		default:
			return 0, 0, false
		}
	default:
		return 0, 0, false
	}
}

// constantLoadIsLoopInvariant reports whether the load at loadPC is the only instruction
// in the loop that may write its destination.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes loop (LoopRange) which is the loop being analysed.
// Takes loadPC (int) which is the PC of the candidate load.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns true when no other instruction in the loop may write the destination.
func constantLoadIsLoopInvariant(compiledFunction *program.CompiledFunction, loop loopRange, loadPC int, kind isa.RegisterKind, reg uint8) bool {
	for pc := loop.header; pc <= loop.latch; pc++ {
		if pc == loadPC {
			continue
		}
		if instructionMayWriteRegister(compiledFunction, compiledFunction.Body[pc], kind, reg) {
			return false
		}
	}
	return true
}

// constantHoistSafeAcrossLoopEntry reports whether running the load at loadPC in the
// pre-header is unobservable on the entry path.
//
// It is when the load runs before any exit edge of the loop, or when nothing in the loop
// before the load reads the destination and the destination is dead at every exit the
// load sits behind. A constant load cannot fault, so no receiver proof is needed.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes loop (LoopRange) which is the loop being analysed.
// Takes loadPC (int) which is the PC of the candidate load.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns true when the hoist is safe on entry.
func constantHoistSafeAcrossLoopEntry(compiledFunction *program.CompiledFunction, loop loopRange, loadPC int, kind isa.RegisterKind, reg uint8) bool {
	body := compiledFunction.Body
	exits, ok := loopExitsBeforeRead(body, loop, loadPC)
	if !ok {
		return false
	}
	if len(exits) == 0 {
		return true
	}
	if registerReadInRange(compiledFunction, loop.header, loadPC, kind, reg) {
		return false
	}
	for _, exitPC := range exits {
		if !registerDeadFrom(compiledFunction, body, exitPC, kind, reg) {
			return false
		}
	}
	return true
}
