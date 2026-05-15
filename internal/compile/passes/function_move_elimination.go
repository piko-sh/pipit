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

const (
	// moveElimScanWindow bounds how far the pass walks forward looking for the unique
	// downstream reader of a MOVE's destination. Wide enough for the reader to sit past a
	// crossed forward jump and the chained MOVE patterns that arise from tuple assigns.
	moveElimScanWindow = 64

	// tier1ScanOperandPositionC is the operand index of `c` (the source operand of a tier-1
	// same-bank MOVE) within an instruction.
	tier1ScanOperandPositionC = 2
)

// MoveCandidate carries the recognised MOVE's bank kind, destination register, and source
// register from the entry-condition classifier down to the scanner. Bundling the trio
// reduces argument count on every helper that consumes them.
type MoveCandidate struct {
	// Kind holds the register bank in which the candidate MOVE operates.
	Kind isa.RegisterKind

	// Destination holds the destination register index of the candidate MOVE.
	Destination uint8

	// Source holds the source register index of the candidate MOVE.
	Source uint8
}

// moveScanDecision describes the outcome of inspecting a single instruction during
// MOVE-elimination scanning.
type moveScanDecision uint8

const (
	// moveScanContinue keeps scanning forward.
	moveScanContinue moveScanDecision = iota

	// moveScanBail abandons elimination (destination may have additional readers beyond what
	// the scan can prove safe).
	moveScanBail

	// moveScanEliminate concludes that destination is killed and the MOVE can be replaced
	// with isa.OpNop.
	moveScanEliminate
)

// ScanAndEliminateMove walks the basic block forward from i+1 to rewrite or remove a
// same-bank MOVE.
//
// Takes body ([]instruction) which is the instruction stream under analysis.
// Takes i (int) which is the index of the MOVE under consideration.
// Takes n (int) which is the body length bounding the forward walk.
// Takes jumpTargets (map[int]bool) which marks slots that are jump destinations.
// Takes candidate (MoveCandidate) which carries the MOVE's bank, destination, and source.
//
// Returns true when the MOVE was eliminated; false otherwise.
func ScanAndEliminateMove(body []isa.Instruction, i, n int, jumpTargets map[int]bool, candidate MoveCandidate) bool {
	return scanAndEliminateMoveAcrossJumps(body, i, n, jumpTargets, candidate, nil)
}

// fuseMoveElimination implements a conservative single-use forward-propagation pass for
// redundant same-bank MOVE instructions.
//
// When destination is read exactly once before either side is overwritten, the reader is
// rewritten to use source directly and the MOVE becomes isa.OpNop. When destination is
// written before being read, the MOVE is dead and is replaced unconditionally.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence being optimised.
// Takes i (int) which specifies the candidate slot index.
// Takes n (int) which specifies the body length.
// Takes jumpTargets (map[int]bool) which specifies the set of instruction indices that
// are jump destinations (basic-block boundary proxy).
//
// Returns true if a MOVE was eliminated at this slot, false otherwise.
func fuseMoveElimination(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	candidate, eligible := classifyMoveEliminationCandidate(compiledFunction, body, i, n, jumpTargets)
	if !eligible {
		return false
	}
	if candidate.Destination == candidate.Source {
		body[i] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
		return true
	}
	if int(candidate.Destination) < returnSlotCount(compiledFunction, candidate.Kind) || int(candidate.Source) < returnSlotCount(compiledFunction, candidate.Kind) {
		return false
	}
	return ScanAndEliminateMove(body, i, n, jumpTargets, candidate)
}

// classifyMoveEliminationCandidate checks the entry conditions for MOVE elimination at
// body[i]: the slot must hold a recognised MOVE, must not itself be a jump target, and
// the function must not capture shared cells (isa.OpMakeClosure).
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction stream under analysis.
// Takes i (int) which is the candidate slot index.
// Takes n (int) which is the body length used to bound lookahead checks.
// Takes jumpTargets (map[int]bool) which marks slots that are jump destinations.
//
// Returns the parsed move candidate and true when the slot qualifies for elimination; the
// zero value and false otherwise.
func classifyMoveEliminationCandidate(compiledFunction *program.CompiledFunction, body []isa.Instruction, i, n int, jumpTargets map[int]bool) (MoveCandidate, bool) {
	if i+1 >= n {
		return MoveCandidate{}, false
	}
	kind, ok := moveOpcodeKind(body[i])
	if !ok {
		return MoveCandidate{}, false
	}
	if jumpTargets[i] {
		return MoveCandidate{}, false
	}
	if program.MayCreateSharedCells(compiledFunction) {
		return MoveCandidate{}, false
	}
	destination, source := moveOperands(body[i])
	return MoveCandidate{Kind: kind, Destination: destination, Source: source}, true
}

// returnSlotCount counts bank-counted result slots for the given kind.
//
// These slots are read by handleReturn / handleTailCall via resultKinds metadata rather
// than via instruction operand bytes, so the MOVE-elimination pass treats them as live
// for the entire function body and refuses to eliminate MOVEs that read or write them.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes kind (isa.RegisterKind) which selects which result-bank to count.
//
// Returns the number of return slots in that bank.
func returnSlotCount(compiledFunction *program.CompiledFunction, kind isa.RegisterKind) int {
	count := 0
	for _, k := range compiledFunction.ResultKinds {
		if k == kind {
			count++
		}
	}
	return count
}

// scanAndEliminateMoveAcrossJumps is ScanAndEliminateMove with dominator information:
// when dom is non-nil the scan may cross a forward jump whose target the MOVE dominates
// and on whose target path the destination is provably dead, and may conclude at a jump
// target it proved dead that way.
//
// Takes body ([]instruction) which is the instruction stream under analysis.
// Takes i (int) which is the index of the MOVE under consideration.
// Takes n (int) which is the body length bounding the forward walk.
// Takes jumpTargets (map[int]bool) which marks slots that are jump destinations.
// Takes candidate (MoveCandidate) which carries the MOVE's bank, destination, and source.
// Takes dom (*FunctionDominators) which is the dominator table, or nil to stay inside the
// basic block.
//
// Returns true when the MOVE was eliminated; false otherwise.
func scanAndEliminateMoveAcrossJumps(body []isa.Instruction, i, n int, jumpTargets map[int]bool, candidate MoveCandidate, dom *functionDominators) bool {
	scan := &moveScan{body: body, movePC: i, jumpTargets: jumpTargets, candidate: candidate, dom: dom, readJ: -1, readPos: -1, deadTargets: nil}
	limit := min(i+1+moveElimScanWindow, n)
	for j := i + 1; j < limit; {
		decision, advance := classifyMoveScanStep(scan, j)
		switch decision {
		case moveScanBail:
			return false
		case moveScanEliminate:
			if scan.readJ != -1 {
				rewriteOperand(body, scan.readJ, scan.readPos, candidate.Source)
			}
			body[i] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
			return true
		case moveScanContinue:
		}
		j += advance
	}
	return false
}

// classifyMoveScanStep evaluates one instruction at body[j] and returns the decision the
// outer scanner should make together with how many words to advance. Updates the scan's
// readJ and readPos when a single qualifying read of destination is observed.
//
// Takes scan (*moveScan) which carries the body, candidate, jump targets, dominators and
// the read bookkeeping.
// Takes j (int) which is the current slot being inspected.
//
// Returns the moveScanDecision selecting the next scanner action and the number of
// instruction words consumed.
func classifyMoveScanStep(scan *moveScan, j int) (moveScanDecision, int) {
	body := scan.body
	candidate := scan.candidate
	if scan.jumpTargets[j] {
		if scan.deadTargets[j] {
			return moveScanEliminate, 1
		}
		return moveScanBail, 1
	}
	if isControlFlowJump(body[j]) || isa.OperandShapeFor(body[j].Op).Flags&isa.ShapeFlagControlFlow != 0 {
		return classifyJumpScanStep(scan, j)
	}
	if body[j].Op == isa.OpDrillTier1 {
		return classifyTier1ScanStep(body[j], j, candidate, &scan.readJ, &scan.readPos), 1
	}
	shape := isa.OperandShapeFor(body[j].Op)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return moveScanBail, 1
	}
	if shape.Flags&isa.ShapeFlagFollowsExtension != 0 {
		return moveScanBail, 1
	}
	return classifyDescribedScanStep(scan, j, shape), 1
}

// classifyDescribedScanStep applies the read/write rules of a described, non-jump
// instruction to the scan.
//
// Takes scan (*moveScan) which carries the candidate and read bookkeeping.
// Takes j (int) which is the slot being inspected.
// Takes shape (isa.OperandShape) which describes the instruction.
//
// Returns the decision for the outer scanner.
func classifyDescribedScanStep(scan *moveScan, j int, shape isa.OperandShape) moveScanDecision {
	candidate := scan.candidate
	readCount, writesSource, writesDestination, pos := classifyOperands(scan.body[j], shape, candidate.Kind, candidate.Destination, candidate.Source)
	if readCount > 1 {
		return moveScanBail
	}
	if readCount == 1 {
		if scan.readJ != -1 || writesSource {
			return moveScanBail
		}
		scan.readJ = j
		scan.readPos = pos
	}
	if writesSource && scan.readJ == -1 {
		return moveScanBail
	}
	if writesDestination {
		return moveScanEliminate
	}
	if shape.Flags&isa.ShapeFlagTerminator != 0 {
		return moveScanEliminate
	}
	return moveScanContinue
}

// classifyOperands examines body[j] for reads and writes of destination and source in the
// given register bank.
//
// Takes instr (instruction) which is the instruction word to classify.
// Takes shape (isa.OperandShape) which records each operand's role/flags.
// Takes kind (isa.RegisterKind) which selects the bank under analysis.
// Takes destination (uint8) which is the destination register of the MOVE.
// Takes source (uint8) which is the source register of the MOVE.
//
// Returns readCount, the number of operand positions that read destination, writesSource
// (true when the instruction writes source), writesDestination (true when the instruction
// writes destination), and readPos (the operand position 0/1/2 of the sole destination
// read when readCount == 1; -1 otherwise).
func classifyOperands(
	instr isa.Instruction, shape isa.OperandShape,
	kind isa.RegisterKind, destination, source uint8,
) (readCount int, writesSource, writesDestination bool, readPos int) {
	readPos = -1
	operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	for p := range isa.NumInstructionOperands {
		opKind, isReg := isa.KindForRole(roles[p])
		if !isReg || opKind != kind {
			continue
		}
		if shape.Reads[p] && operands[p] == destination {
			readCount++
			readPos = p
		}
		if shape.Writes[p] {
			switch operands[p] {
			case source:
				writesSource = true
			case destination:
				writesDestination = true
			}
		}
	}
	return readCount, writesSource, writesDestination, readPos
}

// rewriteOperand replaces one of body[j]'s operand bytes (position 0=a, 1=b, 2=c) with
// newReg, leaving the other operands and opcode untouched. Implemented as full
// reconstruction via makeInstruction to match the existing fuser style.
//
// Takes body ([]instruction) which is the in-place instruction stream.
// Takes j (int) which is the index of the instruction to rewrite.
// Takes position (int) which selects which operand byte to overwrite.
// Takes newReg (uint8) which is the replacement register index.
func rewriteOperand(body []isa.Instruction, j, position int, newReg uint8) {
	cur := body[j]
	switch position {
	case 0:
		body[j] = isa.NewInstruction(cur.Op, newReg, cur.B, cur.C)
	case 1:
		body[j] = isa.NewInstruction(cur.Op, cur.A, newReg, cur.C)
	case 2:
		body[j] = isa.NewInstruction(cur.Op, cur.A, cur.B, newReg)
	}
}

// moveOpcodeKind classifies an instruction as a same-bank MOVE, returning the register
// bank it operates within.
//
// Cross-bank moves are excluded because they cross allocation boundaries.
// isa.OpMoveGeneral is excluded because its snapshot mode may deep-copy the value, and
// eliminating the MOVE would lose those semantics.
//
// Takes instr (instruction) which is the candidate slot.
//
// Returns the register kind the MOVE targets and true when the instruction is a
// recognised same-bank MOVE; zero/false otherwise.
func moveOpcodeKind(instr isa.Instruction) (isa.RegisterKind, bool) {
	if instr.Op != isa.OpDrillTier1 {
		return 0, false
	}
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpMoveInt:
		return isa.RegisterInt, true
	case isa.SubOpMoveFloat:
		return isa.RegisterFloat, true
	case isa.SubOpMoveString:
		return isa.RegisterString, true
	case isa.SubOpMoveBool:
		return isa.RegisterBool, true
	case isa.SubOpMoveUint:
		return isa.RegisterUint, true
	case isa.SubOpMoveComplex:
		return isa.RegisterComplex, true
	default:
	}
	return 0, false
}

// isControlFlowJump reports whether instr is a jump that could skip past the candidate
// kill point.
//
// Bailing on these is mandatory: if a conditional jump branches around the
// writesDestination we observed, the branch target reaches an unset destination register,
// breaking the rewrite's invariant that the read can be safely retargeted to source.
//
// Takes instr (instruction) which is the slot under inspection.
//
// Returns true when instr is a recognised control-flow jump; false otherwise.
func isControlFlowJump(instr isa.Instruction) bool {
	switch instr.Op {
	case isa.OpJumpIfTrue, isa.OpJumpIfFalse:
		return true
	default:
	}
	if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpJump {
		return true
	}
	return false
}

// classifyTier1ScanStep handles the isa.OpDrillTier1 case during MOVE-elimination
// scanning.
//
// The generic classifyOperands cannot see register accesses through tier-1 sub-ops
// because the shared operand shape uses isa.RoleRegDynamic; isa.KindForRole returns false
// for that role, hiding the read/write from the analysis. This helper special-cases the
// tier-1 same-bank MOVE encoding (`isa.OpDrillTier1, subOpMove*, b=destination,
// c=source`) so reads of and writes to the candidate's destination and source are
// observed. Tier-1 sub-ops other than the recognised same-bank MOVEs (arithmetic, jumps,
// loads, etc.) cannot be safely classified here and must bail.
//
// Takes instr (instruction) which is the tier-1 instruction at body[j].
// Takes j (int) which is the instruction index used to record readJ.
// Takes candidate (MoveCandidate) which carries the original MOVE's kind, destination,
// and source.
// Takes readJ (*int) which is updated when a single qualifying read of destination is
// observed.
// Takes readPos (*int) which is updated when a single qualifying read of destination is
// observed.
//
// Returns the decision for the outer scanner.
func classifyTier1ScanStep(instr isa.Instruction, j int, candidate MoveCandidate, readJ, readPos *int) moveScanDecision {
	subKind, isMove := moveOpcodeKind(instr)
	if !isMove {
		return moveScanBail
	}
	if subKind != candidate.Kind {
		return moveScanContinue
	}
	destination, source := moveOperands(instr)
	if source == candidate.Destination {
		if *readJ != -1 {
			return moveScanBail
		}
		*readJ = j
		*readPos = tier1ScanOperandPositionC
	}
	if destination == candidate.Destination {
		return moveScanEliminate
	}
	if destination == candidate.Source && *readJ == -1 {
		return moveScanBail
	}
	return moveScanContinue
}

// moveOperands extracts destination and source register indices from a same-bank tier-1
// MOVE.
//
// The Compiler's emitMoveTyped elides self-MOVEs so callers can assume destination !=
// source. Tier-1 subOpMove* encoding: {isa.OpDrillTier1, subOpMoveX, b=destination,
// c=source}.
//
// Takes instr (instruction) which is the candidate slot (callers must verify with
// moveOpcodeKind first).
//
// Returns the destination and source register indices.
func moveOperands(instr isa.Instruction) (destination, source uint8) {
	return instr.B, instr.C
}
