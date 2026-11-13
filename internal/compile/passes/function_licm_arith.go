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
const maxLicmArithmeticHoistsPerFunction = 64

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
	var visits loopVisits
	for range maxLicmArithmeticHoistsPerFunction {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("hoistLoopInvariantArithmetic cancelled: %w", err)
		}
		if !tryOneArithmeticHoist(compiledFunction, analysis, &visits) {
			return nil
		}
	}
	return nil
}

// tryOneArithmeticHoist hoists, or renames to make hoistable, one invariant operation.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loops.
// Takes analysis (*functionAnalysis) which caches control-flow facts.
// Takes visits (*loopVisits) which holds the loops already searched to the end.
//
// Returns true when the body changed.
func tryOneArithmeticHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, visits *loopVisits) bool {
	body := compiledFunction.Body
	for _, loop := range mergeLoopsSharingHeader(loopsFor(analysis, body)) {
		if visits.finished(loop) || !loopOpenToHoisting(body, loop) {
			continue
		}
		if tryOneArithmeticHoistInLoop(compiledFunction, newLoopFacts(analysis, loop), visits) {
			return true
		}
		visits.finish(loop)
	}
	return false
}

// tryOneArithmeticHoistInLoop hoists, or renames to make hoistable, the first invariant
// operation of one loop.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes facts (*loopFacts) which supplies the loop, its members and its dominators.
// Takes visits (*loopVisits) which follows the words a hoist moves.
//
// Returns bool which is true when the body changed.
func tryOneArithmeticHoistInLoop(compiledFunction *program.CompiledFunction, facts *loopFacts, visits *loopVisits) bool {
	body := compiledFunction.Body
	loop := facts.loop
	for pc := loop.header; pc <= loop.latch; pc++ {
		kind, reg, ok := hoistableArithmeticDestination(body[pc])
		if !ok {
			continue
		}
		members := facts.loopMembers()
		if len(members) == 0 {
			return false
		}
		if tryHoistArithmeticAt(compiledFunction, facts, members, pc, kind, reg, visits) {
			return true
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
// Takes visits (*loopVisits) which follows the words a hoist moves.
//
// Returns bool which is true when the body changed.
func tryHoistArithmeticAt(
	compiledFunction *program.CompiledFunction, facts *loopFacts, members []int, pc int, kind isa.RegisterKind, reg uint8, visits *loopVisits,
) bool {
	if !facts.isMember(pc) || !arithmeticInputsInvariant(compiledFunction, members, pc, kind, reg) {
		return false
	}
	if isa.InstrIsTier1SubOp(compiledFunction.Body[pc], isa.SubOpLengthGeneral) && !loopKeepsSliceLength(compiledFunction, members, pc) {
		return false
	}
	width := arithmeticUnitWidth(compiledFunction.Body, facts.analysis.JumpTargets(), pc, kind, reg)
	if loopWritesRegisterOutside(compiledFunction, members, pc, pc+width, kind, reg) {
		return renameDefinition(compiledFunction, facts.analysis, pc, width, kind, reg)
	}
	if !definitionHoistsUnobserved(compiledFunction, facts, pc, kind, reg) {
		return false
	}
	if arithmeticCanFault(compiledFunction.Body[pc]) &&
		(!facts.dominatesLatch(pc) || !prefixCannotFault(compiledFunction.Body, facts.loop.header, pc)) {
		return false
	}
	applyLoopHoistWords(compiledFunction, facts.analysis, facts.loop.header, pc, width)
	visits.lifted(facts.loop.header, pc, width)
	return true
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
	if body[pc].Op == isa.OpTruncateToWidth || next >= len(body) || jumpTargets[next] {
		return 1
	}
	if truncation := body[next]; truncation.Op == isa.OpTruncateToWidth && truncation.A == reg && isa.RegisterKind(truncation.C) == kind {
		return 2
	}
	return 1
}

// hoistableArithmeticDestination identifies the destination of an operation that cannot
// fault.
//
// Truncation is accepted because applying it again to its own result changes nothing.
//
// Takes inst (isa.Instruction) which is the candidate.
//
// Returns isa.RegisterKind which is the destination bank.
// Returns uint8 which is the destination register.
// Returns bool which is true when the instruction qualifies.
func hoistableArithmeticDestination(inst isa.Instruction) (isa.RegisterKind, uint8, bool) {
	switch inst.Op {
	case isa.OpAddInt64, isa.OpSubtractInt64, isa.OpMultiplyInt64, isa.OpAddInt64Constant, isa.OpSubtractInt64Constant, isa.OpMultiplyInt64Constant,
		isa.OpBitAndInt64, isa.OpBitOrInt64, isa.OpBitXorInt64:
		return isa.RegisterInt64, inst.A, true
	case isa.OpAddUint64, isa.OpSubtractUint64, isa.OpMultiplyUint64, isa.OpBitAndUint64, isa.OpBitOrUint64, isa.OpBitXorUint64,
		isa.OpShiftLeftUint64, isa.OpShiftRightUint64:
		return isa.RegisterUint64, inst.A, true
	case isa.OpTruncateToWidth:
		kind := isa.RegisterKind(inst.C)
		return kind, inst.A, kind == isa.RegisterInt64 || kind == isa.RegisterUint64
	case isa.OpDrillTier1:
		switch {
		case isa.InstrIsTier1SubOp(inst, isa.SubOpConvertUint64ToInt64):
			return isa.RegisterInt64, inst.B, true
		case isa.InstrIsTier1SubOp(inst, isa.SubOpConvertInt64ToUint64):
			return isa.RegisterUint64, inst.B, true
		case isa.InstrIsTier1SubOp(inst, isa.SubOpUnboxSliceByte):
			return isa.RegisterSliceByte, inst.B, true
		case readsLengthOfItsRegister(isa.SubOpcode(inst.A)), isa.InstrIsTier1SubOp(inst, isa.SubOpLengthGeneral):
			return isa.RegisterInt64, inst.B, true
		}
	}
	return isa.RegisterInt64, 0, false
}

// readsLengthOfItsRegister reports whether a tier-1 operation sets ints[B] to the length
// of the string or the typed slice held in register C.
//
// The length of a general register is not among them. Such a register can hold a view of
// a slice variable, whose length a store elsewhere changes, so it is hoisted only out of
// a loop loopKeepsSliceLength accepts.
//
// Takes sub (isa.SubOpcode) which is the tier-1 operation.
//
// Returns bool which is true for a length that depends on the register alone.
func readsLengthOfItsRegister(sub isa.SubOpcode) bool {
	switch sub {
	case isa.SubOpLengthString, isa.SubOpLengthSliceInt64, isa.SubOpLengthSliceFloat64, isa.SubOpLengthSliceString,
		isa.SubOpLengthSliceBool, isa.SubOpLengthSliceUint64, isa.SubOpLengthSliceByte:
		return true
	default:
		return false
	}
}

// arithmeticCanFault reports whether a hoistable operation can raise a panic.
//
// Takes inst (isa.Instruction) which is the operation.
//
// Returns bool which is true for an operation that can fault.
func arithmeticCanFault(inst isa.Instruction) bool {
	return isa.InstrIsTier1SubOp(inst, isa.SubOpUnboxSliceByte)
}

// loopKeepsSliceLength reports whether the general register whose length the instruction
// at pc reads holds a slice or an array whose length nothing in the loop can change.
//
// The register may hold a view of a slice variable, so the loop must store to no memory
// and make no call that could, and must not wait on another goroutine, whose stores it
// would then be entitled to see. It must also index the register as a slice or an array
// somewhere. The length of a channel changes under another goroutine with no store in
// this one, and nothing else tells the two apart here.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the loop.
// Takes members ([]int) which lists the loop's instructions.
// Takes pc (int) which is the length read.
//
// Returns bool which is true when the length read before the loop is the length every
// iteration would read.
func loopKeepsSliceLength(compiledFunction *program.CompiledFunction, members []int, pc int) bool {
	collection := compiledFunction.Body[pc].C
	indexed := false
	for _, member := range members {
		inst := compiledFunction.Body[member]
		if member == pc || inst.Op == isa.OpExtension {
			continue
		}
		if InstructionDirectlyMutatesHeap(inst) || meetsAnotherGoroutine(inst) {
			return false
		}
		if IsCallInstruction(inst) && CallInvalidatesPurity(compiledFunction, inst) {
			return false
		}
		indexed = indexed || indexesGeneralRegister(inst, collection)
	}
	return indexed
}

// meetsAnotherGoroutine reports whether an instruction starts or can wait for a
// goroutine.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns bool which is true for those three.
func meetsAnotherGoroutine(inst isa.Instruction) bool {
	return inst.Op == isa.OpGo || isa.InstrIsTier1SubOp(inst, isa.SubOpSelect) || isa.InstrIsTier1SubOp(inst, isa.SubOpRangeNext)
}

// indexesGeneralRegister reports whether an instruction reads an element of the slice or
// array held in a general register by an integer index.
//
// Takes inst (isa.Instruction) which is the instruction.
// Takes collection (uint8) which is the general register.
//
// Returns bool which is true for the element reads that accept nothing but a slice, an
// array or a pointer to an array.
func indexesGeneralRegister(inst isa.Instruction, collection uint8) bool {
	switch inst.Op {
	case isa.OpGetElement, isa.OpGetElementInt64, isa.OpGetElementUint64, isa.OpGetElementFloat64,
		isa.OpGetElementBool, isa.OpGetElementString,
		isa.OpGetElementFieldInt64, isa.OpGetElementFieldUint64, isa.OpGetElementFieldFloat64,
		isa.OpGetElementFieldBool, isa.OpGetElementFieldString:
		return inst.B == collection
	default:
		return false
	}
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
		if kind == destKind && operands[position] == dest && inst.Op != isa.OpTruncateToWidth {
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
		if (member < from || member >= to) && inst.Op != isa.OpExtension && instructionMayWriteRegister(compiledFunction, inst, kind, reg) {
			return true
		}
	}
	return false
}
