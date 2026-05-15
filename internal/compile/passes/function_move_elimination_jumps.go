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

// moveScan carries the state of one MOVE-elimination scan: the candidate, the block
// boundaries, the optional dominator table that licenses crossing forward jumps, the jump
// targets proved dead for the destination, and the single observed read.
type moveScan struct {
	// jumpTargets marks slots that are jump destinations.
	jumpTargets map[int]bool

	// deadTargets marks jump targets on whose path the destination is provably dead.
	deadTargets map[int]bool

	// dom is the dominator table, or nil when the scan must stay inside its block.
	dom *functionDominators

	// body is the instruction stream under analysis.
	body []isa.Instruction

	// movePC is the index of the MOVE under consideration.
	movePC int

	// readJ is the slot of the single observed read of destination, or -1.
	readJ int

	// readPos is the operand position of that read, or -1.
	readPos int

	// candidate carries the MOVE's bank, destination and source.
	candidate MoveCandidate
}

// EliminateMovesAcrossDominatedJumps reruns MOVE elimination over the optimised body with
// dominator information, so a MOVE whose unique reader sits past a forward jump the MOVE
// Dominates is eliminated as well.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes dom (*FunctionDominators) which is the dominator table computed by GVN.
func EliminateMovesAcrossDominatedJumps(compiledFunction *program.CompiledFunction, body []isa.Instruction, dom *functionDominators) {
	if dom == nil {
		return
	}
	eliminateMovesAcrossDominatedJumps(compiledFunction, body, dom, BuildAllJumpTargets(body))
}

// classifyJumpScanStep decides whether the scan may pass a jump at body[j].
//
// Without a dominator table every jump bails. With one, a forward jump whose target the
// MOVE dominates and on whose target path the destination is dead before any read is safe
// to pass. An unconditional jump then kills the MOVE outright, and a conditional jump
// lets the scan continue along the fallthrough edge after accounting for the jump's own
// reads.
//
// Takes scan (*moveScan) which carries the candidate, dominators and bookkeeping.
// Takes j (int) which is the jump's slot.
//
// Returns the decision and the number of words the jump occupies.
func classifyJumpScanStep(scan *moveScan, j int) (moveScanDecision, int) {
	if scan.dom == nil {
		return moveScanBail, 1
	}
	body := scan.body
	target, ok := program.JumpTargetAt(body, j)
	if !ok || target <= j || !scan.dom.Dominates(scan.movePC, target) {
		return moveScanBail, 1
	}
	if !destinationDeadAt(scan, target) {
		return moveScanBail, 1
	}
	if scan.deadTargets == nil {
		scan.deadTargets = make(map[int]bool)
	}
	scan.deadTargets[target] = true
	width := program.JumpWordCount(body[j])
	if isa.InstrIsTier1SubOp(body[j], isa.SubOpJump) {
		return moveScanEliminate, width
	}
	shape := isa.ShapeForInstruction(body[j])
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return moveScanBail, width
	}
	decision := classifyDescribedScanStep(scan, j, shape)
	if decision == moveScanEliminate {
		decision = moveScanContinue
	}
	return decision, width
}

// destinationDeadAt reports whether every path from start writes the candidate's
// destination before reading it, examining the straight-line code from start and refusing
// at the first merge point, control transfer or unclassifiable instruction.
//
// Takes scan (*moveScan) which carries the candidate and jump targets.
// Takes start (int) which is the first slot of the path.
//
// Returns true when the destination is provably dead on entry to start.
func destinationDeadAt(scan *moveScan, start int) bool {
	limit := min(start+moveElimScanWindow, len(scan.body))
	for k := start; k < limit; k++ {
		if k != start && scan.jumpTargets[k] {
			return false
		}
		decision := classifyDeadPathStep(scan.body[k], scan.candidate)
		if decision != moveScanContinue {
			return decision == moveScanEliminate
		}
	}
	return false
}

// classifyDeadPathStep inspects one instruction on a jump's target path: a write to the
// destination or a terminator proves the destination dead (moveScanEliminate), a read or
// an unclassifiable instruction refutes it (moveScanBail), and anything else keeps
// looking (moveScanContinue).
//
// Takes instr (instruction) which is the instruction under inspection.
// Takes candidate (MoveCandidate) which carries the MOVE's bank, destination and source.
//
// Returns the decision for the dead-path walk.
func classifyDeadPathStep(instr isa.Instruction, candidate MoveCandidate) moveScanDecision {
	if instr.Op == isa.OpDrillTier1 {
		return classifyDeadPathTier1Step(instr, candidate)
	}
	shape := isa.OperandShapeFor(instr.Op)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&(isa.ShapeFlagFollowsExtension|isa.ShapeFlagControlFlow) != 0 {
		return moveScanBail
	}
	readCount, _, writesDestination, _ := classifyOperands(instr, shape, candidate.Kind, candidate.Destination, candidate.Source)
	if readCount > 0 {
		return moveScanBail
	}
	if writesDestination || shape.Flags&isa.ShapeFlagTerminator != 0 {
		return moveScanEliminate
	}
	return moveScanContinue
}

// classifyDeadPathTier1Step is the tier-1 case of classifyDeadPathStep: only same-bank
// MOVEs are classifiable, every other sub-op refutes the walk.
//
// Takes instr (instruction) which is the tier-1 instruction under inspection.
// Takes candidate (MoveCandidate) which carries the MOVE's bank, destination and source.
//
// Returns the decision for the dead-path walk.
func classifyDeadPathTier1Step(instr isa.Instruction, candidate MoveCandidate) moveScanDecision {
	kind, isMove := moveOpcodeKind(instr)
	if !isMove {
		return moveScanBail
	}
	if kind != candidate.Kind {
		return moveScanContinue
	}
	destination, source := moveOperands(instr)
	if source == candidate.Destination {
		return moveScanBail
	}
	if destination == candidate.Destination {
		return moveScanEliminate
	}
	return moveScanContinue
}

// eliminateMovesAcrossDominatedJumps is EliminateMovesAcrossDominatedJumps with the
// jump-target set supplied by the caller.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes dom (*FunctionDominators) which is the dominator table; nil skips the pass.
// Takes jumpTargets (map[int]bool) which marks every branch destination in body.
func eliminateMovesAcrossDominatedJumps(compiledFunction *program.CompiledFunction, body []isa.Instruction, dom *functionDominators, jumpTargets map[int]bool) {
	if dom == nil {
		return
	}
	n := len(body)
	for i := range body {
		candidate, eligible := classifyMoveEliminationCandidate(compiledFunction, body, i, n, jumpTargets)
		if !eligible || candidate.Destination == candidate.Source {
			continue
		}
		if int(candidate.Destination) < returnSlotCount(compiledFunction, candidate.Kind) || int(candidate.Source) < returnSlotCount(compiledFunction, candidate.Kind) {
			continue
		}
		scanAndEliminateMoveAcrossJumps(body, i, n, jumpTargets, candidate, dom)
	}
}
