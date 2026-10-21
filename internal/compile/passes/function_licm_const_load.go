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
const maxLicmConstHoistsPerFunction = 64

// hoistLoopInvariantConstantLoads lifts loop-invariant constant loads to the pre-header,
// excluding fresh-storage loads that must produce a new value each execution.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes analysis (*functionAnalysis) which supplies loops and dominators and is
// invalidated by every hoist. Nil is tolerated.
//
// Returns error when cancellation interrupts the pass.
func hoistLoopInvariantConstantLoads(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	if analysis == nil {
		analysis = newFunctionAnalysis(compiledFunction)
	}
	var visits loopVisits
	for budget := maxLicmConstHoistsPerFunction; budget > 0; {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("hoistLoopInvariantConstantLoads cancelled: %w", err)
		}
		hoisted := hoistConstantLoadsOfOneLoop(compiledFunction, analysis, &visits, budget)
		if hoisted == 0 {
			return nil
		}
		budget -= hoisted
	}
	return nil
}

// hoistConstantLoadsOfOneLoop lifts every hoistable constant load of the first loop that
// has one, in a single batch.
//
// The loads are found against one snapshot of the loop and lifted from the highest PC
// down, so each lift shifts the ones still to come by its own width and nothing else
// about them changes, since no two of them write one register and none reads another's.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes analysis (*functionAnalysis) which supplies the loop list.
// Takes visits (*loopVisits) which holds the loops already searched to the end.
// Takes limit (int) which is the most loads to lift.
//
// Returns int which is the number of loads lifted, zero when no loop has one.
func hoistConstantLoadsOfOneLoop(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, visits *loopVisits, limit int) int {
	loops := mergeLoopsSharingHeader(loopsFor(analysis, compiledFunction.Body))
	for _, loop := range loops {
		if visits.finished(loop) || !loopOpenToHoisting(compiledFunction.Body, loop) {
			continue
		}
		loads := hoistableConstantLoadsInLoop(compiledFunction, newLoopFacts(analysis, loop), limit)
		for lifted := range loads {
			readPC := loads[len(loads)-1-lifted] + lifted
			applyLoopHoist(compiledFunction, analysis, loop.header, readPC)
			visits.lifted(loop.header, readPC, 1)
		}
		if len(loads) > 0 {
			return len(loads)
		}
		visits.finish(loop)
	}
	return 0
}

// hoistableConstantLoadsInLoop lists the constant loads in loop that are the loop's only
// writer of their destination and are safe on the entry path, in ascending PC order.
//
// A load that shares its register with another writer is given a register of its own
// first when it can be (renameDefinition()), which changes operands and no PC, so the
// search carries on with the same loop facts.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes facts (*loopFacts) which supplies the loop's members and dominators.
// Takes limit (int) which is the most loads to list.
//
// Returns []int which holds the loads' PCs, nil when there are none.
func hoistableConstantLoadsInLoop(compiledFunction *program.CompiledFunction, facts *loopFacts, limit int) []int {
	body := compiledFunction.Body
	loop := facts.loop
	var loads []int
	for pc := loop.header; pc <= loop.latch && len(loads) < limit; pc++ {
		kind, reg, ok := constantLoadDestination(body[pc])
		if !ok {
			continue
		}
		members := facts.loopMembers()
		if len(members) == 0 {
			return nil
		}
		if !facts.isMember(pc) {
			continue
		}
		if !constantLoadIsLoopInvariant(compiledFunction, members, pc, kind, reg) {
			if !renameDefinition(compiledFunction, facts.analysis, pc, 1, kind, reg) {
				continue
			}
			_, reg, _ = constantLoadDestination(body[pc])
		}
		if definitionHoistsUnobserved(compiledFunction, facts, pc, kind, reg) {
			loads = append(loads, pc)
		}
	}
	return loads
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
	case isa.OpLoadInt64Constant:
		return isa.RegisterInt64, inst.A, true
	case isa.OpLoadFloat64Constant:
		return isa.RegisterFloat64, inst.A, true
	case isa.OpLoadStringConstant:
		return isa.RegisterString, inst.A, true
	case isa.OpLoadUint64Constant:
		return isa.RegisterUint64, inst.A, true
	case isa.OpDrillTier1:
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpLoadInt64Immediate:
			return isa.RegisterInt64, inst.B, true
		case isa.SubOpLoadUint64Immediate:
			return isa.RegisterUint64, inst.B, true
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
// Takes members ([]int) which lists every PC in the natural loop.
// Takes loadPC (int) which is the PC of the candidate load.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns true when no other instruction in the loop may write the destination.
func constantLoadIsLoopInvariant(compiledFunction *program.CompiledFunction, members []int, loadPC int, kind isa.RegisterKind, reg uint8) bool {
	for _, pc := range members {
		if pc == loadPC {
			continue
		}
		if instructionMayWriteRegister(compiledFunction, compiledFunction.Body[pc], kind, reg) {
			return false
		}
	}
	return true
}

// definitionHoistsUnobserved reports whether a definition that cannot fault, and is the
// loop's only writer of its register, may run in the pre-header in place of its position
// in the loop.
//
// It may when the register is dead at the loop header, which is to say every path from
// the header writes the register before reading it. The definition must be a loop member,
// because the claim that it is the only writer is made of the members alone, and an arm
// that leaves the loop is free to write the register again.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes facts (*loopFacts) which supplies the loop and its dominators.
// Takes pc (int) which is the definition.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination register.
//
// Returns true when the hoist is unobservable.
func definitionHoistsUnobserved(compiledFunction *program.CompiledFunction, facts *loopFacts, pc int, kind isa.RegisterKind, reg uint8) bool {
	if registerDeadFrom(compiledFunction, compiledFunction.Body, facts.loop.header, kind, reg) {
		return true
	}
	return facts.dominatesLatch(pc) && constantHoistSafeAcrossLoopEntry(compiledFunction, facts.loop, pc, kind, reg)
}

// constantHoistSafeAcrossLoopEntry reports whether running the load at loadPC in the
// pre-header is unobservable on the entry path.
//
// It is when nothing in the loop before the load reads the destination, since the first
// traversal would otherwise see the hoisted value early, and the destination is dead at
// every exit the load sits behind. A constant load cannot fault, so no receiver proof is
// needed.
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
	if !ok || registerReadInRange(compiledFunction, loop.header, loadPC, kind, reg) {
		return false
	}
	for _, exitPC := range exits {
		if !registerDeadFrom(compiledFunction, body, exitPC, kind, reg) {
			return false
		}
	}
	return true
}
