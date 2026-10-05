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

// maxLicmArithmeticHoistsPerFunction bounds the arithmetic hoists, and the renames that
// enable them, in one function.
const maxLicmArithmeticHoistsPerFunction = 16

// hoistLoopInvariantArithmetic moves integer arithmetic whose inputs do not change in a
// loop to the loop's pre-header, one instruction at a time so that a chain of invariant
// operations hoists in order.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loops.
// Takes analysis (*functionAnalysis) which caches control-flow facts and is invalidated
// by a rewrite.
//
// Returns an error when cancellation interrupts the pass.
func hoistLoopInvariantArithmetic(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	if analysis == nil {
		analysis = newFunctionAnalysis(compiledFunction)
	}
	for range maxLicmArithmeticHoistsPerFunction {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("hoistLoopInvariantArithmetic cancelled: %w", err)
		}
		if !tryOneArithmeticHoist(compiledFunction, analysis) {
			return nil
		}
	}
	return nil
}

// tryOneArithmeticHoist hoists, or renames to make hoistable, one invariant operation.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loops.
// Takes analysis (*functionAnalysis) which caches control-flow facts.
//
// Returns true when the body changed.
func tryOneArithmeticHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) bool {
	body := compiledFunction.Body
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if !loopHeaderReachableByFallThrough(body, loop) {
			continue
		}
		facts := newLoopFacts(analysis, loop)
		for pc := loop.header; pc <= loop.latch; pc++ {
			kind, reg, ok := hoistableArithmeticDestination(body[pc])
			if !ok {
				continue
			}
			members := facts.loopMembers()
			if len(members) == 0 {
				break
			}
			if changed, stop := tryHoistArithmeticAt(compiledFunction, facts, members, pc, kind, reg); changed || stop {
				return changed
			}
		}
	}
	return false
}

// tryHoistArithmeticAt hoists the operation at pc when its inputs are invariant, renaming
// its destination first when another loop instruction writes the same register.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes facts (*loopFacts) which supplies the loop's dominators.
// Takes members ([]int) which lists the loop's instructions.
// Takes pc (int) which is the candidate.
// Takes kind (isa.RegisterKind) which is the destination bank.
// Takes reg (uint8) which is the destination.
//
// Returns whether the body changed, and whether the search must stop because dominance is
// unknown.
func tryHoistArithmeticAt(compiledFunction *program.CompiledFunction, facts *loopFacts, members []int, pc int, kind isa.RegisterKind, reg uint8) (changed, stop bool) {
	if !arithmeticInputsInvariant(compiledFunction, members, pc, kind, reg) || !facts.dominatesLatch(pc) {
		return false, facts.dominators() == nil
	}
	width := arithmeticUnitWidth(compiledFunction.Body, facts.analysis.JumpTargets(), pc, kind, reg)
	if loopWritesRegisterOutside(compiledFunction, members, pc, pc+width, kind, reg) {
		return renameDefinition(compiledFunction, facts.analysis, pc, width, kind, reg), false
	}
	if !constantHoistSafeAcrossLoopEntry(compiledFunction, facts.loop, pc, kind, reg) {
		return false, false
	}
	applyLoopHoistWords(compiledFunction, facts.analysis, facts.loop.header, pc, width)
	return true, false
}

// arithmeticUnitWidth returns 2 when the operation at pc is followed by a truncation of
// its own result that nothing jumps to, so the pair hoists and renames as one unit, and 1
// otherwise.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes pc (int) which is the operation.
// Takes kind (isa.RegisterKind) which is its destination bank.
// Takes reg (uint8) which is its destination.
//
// Returns the unit's word count.
func arithmeticUnitWidth(body []isa.Instruction, jumpTargets map[int]bool, pc int, kind isa.RegisterKind, reg uint8) int {
	next := pc + 1
	if body[pc].Op == isa.OpTruncateNarrow || next >= len(body) || jumpTargets[next] {
		return 1
	}
	if truncation := body[next]; truncation.Op == isa.OpTruncateNarrow && truncation.A == reg && isa.RegisterKind(truncation.C) == kind {
		return 2
	}
	return 1
}

// hoistableArithmeticDestination accepts the single-word integer operations that cannot
// fault, and names the register they write. Truncation is accepted though it reads its
// destination, because applying it again to its own result changes nothing.
//
// Takes inst (isa.Instruction) which is the candidate.
//
// Returns the destination bank and register, and whether inst qualifies.
func hoistableArithmeticDestination(inst isa.Instruction) (isa.RegisterKind, uint8, bool) {
	switch inst.Op {
	case isa.OpAddInt, isa.OpSubInt, isa.OpMulInt, isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst,
		isa.OpBitAnd, isa.OpBitOr, isa.OpBitXor:
		return isa.RegisterInt, inst.A, true
	case isa.OpAddUint, isa.OpSubUint, isa.OpMulUint, isa.OpBitAndUint, isa.OpBitOrUint, isa.OpBitXorUint,
		isa.OpShiftLeftUint, isa.OpShiftRightUint:
		return isa.RegisterUint, inst.A, true
	case isa.OpTruncateNarrow:
		kind := isa.RegisterKind(inst.C)
		return kind, inst.A, kind == isa.RegisterInt || kind == isa.RegisterUint
	case isa.OpDrillTier1:
		switch {
		case isa.InstrIsTier1SubOp(inst, isa.SubOpUintToInt):
			return isa.RegisterInt, inst.B, true
		case isa.InstrIsTier1SubOp(inst, isa.SubOpIntToUint):
			return isa.RegisterUint, inst.B, true
		}
	}
	return isa.RegisterInt, 0, false
}

// arithmeticInputsInvariant reports whether no loop instruction other than the candidate
// writes a register the candidate reads, and the candidate does not read its own
// destination unless it is an idempotent truncation.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes members ([]int) which lists the loop's instructions.
// Takes pc (int) which is the candidate.
// Takes destKind (isa.RegisterKind) which is the destination bank.
// Takes dest (uint8) which is the destination.
//
// Returns true when every input is invariant.
func arithmeticInputsInvariant(compiledFunction *program.CompiledFunction, members []int, pc int, destKind isa.RegisterKind, dest uint8) bool {
	inst := compiledFunction.Body[pc]
	shape := isa.ShapeForInstruction(inst)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	for position, role := range roles {
		if !shape.Reads[position] {
			continue
		}
		kind, ok := isa.KindForRole(role)
		if role == isa.RoleRegDynamic {
			kind, ok = isa.RegisterKind(inst.C), true
		}
		if !ok || loopWritesRegisterElsewhere(compiledFunction, members, pc, kind, operands[position]) {
			return false
		}
		if kind == destKind && operands[position] == dest && inst.Op != isa.OpTruncateNarrow {
			return false
		}
	}
	return true
}

// loopWritesRegisterElsewhere reports whether a loop instruction other than the one at pc
// may write reg.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes members ([]int) which lists the loop's instructions.
// Takes pc (int) which is the instruction to ignore.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reg (uint8) which is the register.
//
// Returns true when another member may write it.
func loopWritesRegisterElsewhere(compiledFunction *program.CompiledFunction, members []int, pc int, kind isa.RegisterKind, reg uint8) bool {
	return loopWritesRegisterOutside(compiledFunction, members, pc, pc+1, kind, reg)
}

// loopWritesRegisterOutside reports whether a loop instruction outside [from, to) may
// write reg.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes members ([]int) which lists the loop's instructions.
// Takes from (int) which is the first PC to ignore.
// Takes to (int) which is the PC after the last one to ignore.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reg (uint8) which is the register.
//
// Returns true when another member may write it.
func loopWritesRegisterOutside(compiledFunction *program.CompiledFunction, members []int, from, to int, kind isa.RegisterKind, reg uint8) bool {
	for _, member := range members {
		inst := compiledFunction.Body[member]
		if (member < from || member >= to) && inst.Op != isa.OpExt && instructionMayWriteRegister(compiledFunction, inst, kind, reg) {
			return true
		}
	}
	return false
}
