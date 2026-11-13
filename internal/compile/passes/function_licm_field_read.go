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
	"cmp"
	"context"
	"fmt"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// maxLicmHoistsPerFunction caps field-read and global-load hoists per function.
	maxLicmHoistsPerFunction = 32

	// initialLoopCapacity is the initial size of the loops slice used by identifyLoops. Most
	// functions in practice contain a small number of loops, so pre-allocating four avoids
	// the early grow steps.
	initialLoopCapacity = 4
)

// loopRange describes a natural loop discovered from a back-edge.
type loopRange struct {
	// header is the PC at the top of the loop body (the back-edge's target). Linear
	// fall-through enters the loop here.
	header int

	// latch is the PC of the back-edge instruction (the source of the negative-offset jump).
	// The loop body covers [Header, latch].
	latch int
}

// HoistLoopInvariantStructFieldReads lifts loop-invariant struct-field reads to the
// pre-header, running to a fixed point of at most maxLicmHoistsPerFunction lifts.
//
// Must be invoked before any pass that depends on stable instruction indices.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to optimise.
//
// Returns error when cancellation interrupts the pass.
func HoistLoopInvariantStructFieldReads(ctx context.Context, compiledFunction *program.CompiledFunction) error {
	return hoistLoopInvariantStructFieldReads(ctx, compiledFunction, nil, false)
}

// loopHeaderReachableByFallThrough reports whether linear fall-through enters the loop
// header.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes loop (loopRange) which is the natural loop being considered for hoisting.
//
// Returns true when the loop header is reached via fall-through, false otherwise.
func loopHeaderReachableByFallThrough(body []isa.Instruction, loop loopRange) bool {
	if loop.header <= 0 {
		return true
	}
	return isLinearFallThroughFrom(body[loop.header-1])
}

// BuildAllJumpTargets returns every jump or branch target PC in body.
//
// Takes body ([]isa.Instruction) which is the instruction stream to scan for jump
// targets.
//
// Returns a map whose keys are every PC reached by any jump in body.
func BuildAllJumpTargets(body []isa.Instruction) map[int]bool {
	return program.JumpTargetsOf(body)
}

// hoistLoopInvariantStructFieldReads hoists field reads using an optional shared
// analysis.
//
// Each hoist invalidates the analysis because later PCs move.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*functionAnalysis) which is the pipeline's shared analysis, or nil.
// Takes elementStoresKeepFields (bool) which is ProgramFacts.ElementStoresKeepFields.
//
// Returns error when cancellation interrupts the pass.
func hoistLoopInvariantStructFieldReads(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis, elementStoresKeepFields bool) error {
	if analysis == nil {
		analysis = newFunctionAnalysis(compiledFunction)
	}
	var visits loopVisits
	for range maxLicmHoistsPerFunction {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("hoistLoopInvariantStructFieldReads cancelled: %w", err)
		}
		if !tryOneStructFieldHoist(compiledFunction, analysis, &visits, elementStoresKeepFields) {
			return nil
		}
	}
	return nil
}

// tryOneStructFieldHoist performs at most one hoist on compiledFunction.body, or one
// rename that makes a read hoistable the next time round.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which supplies the loop list when non-nil.
// Takes visits (*loopVisits) which holds the loops already searched to the end.
// Takes elementStoresKeepFields (bool) which is ProgramFacts.ElementStoresKeepFields.
//
// Returns true when a hoist or a rename was applied, false when no read anywhere in the
// body can be lifted or made liftable.
func tryOneStructFieldHoist(
	compiledFunction *program.CompiledFunction, analysis *functionAnalysis, visits *loopVisits, elementStoresKeepFields bool,
) bool {
	loops := mergeLoopsSharingHeader(loopsFor(analysis, compiledFunction.Body))
	for _, loop := range loops {
		if visits.finished(loop) || !loopOpenToHoisting(compiledFunction.Body, loop) {
			continue
		}
		if !loopHoldsAHoistableRead(compiledFunction.Body, loop) {
			visits.finish(loop)
			continue
		}
		readPC, ok := findHoistableReadInLoop(compiledFunction, loop, analysis, elementStoresKeepFields)
		if ok {
			width := hoistedReadWordCount(compiledFunction.Body, readPC)
			applyLoopHoistWords(compiledFunction, analysis, loop.header, readPC, width)
			visits.lifted(loop.header, readPC, width)
			return true
		}
		if renameReadWithAReusedDestination(compiledFunction, loop, analysis, elementStoresKeepFields) {
			return true
		}
		visits.finish(loop)
	}
	return false
}

// loopHoldsAHoistableRead reports whether the loop contains a candidate read.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes loop (loopRange) which is the loop to scan.
//
// Returns bool which is true when the loop has a candidate.
func loopHoldsAHoistableRead(body []isa.Instruction, loop loopRange) bool {
	for pc := loop.header; pc <= loop.latch; pc++ {
		if isHoistableReadAt(body, pc) {
			return true
		}
	}
	return false
}

// findHoistableReadInLoop locates the first hoistable read that is loop-invariant and
// safe to run on the entry path.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes loop (loopRange) which is the natural loop within which to search for a hoistable
// read.
// Takes analysis (*functionAnalysis) which supplies the dominator and loop information.
// Takes elementStoresKeepFields (bool) which is ProgramFacts.ElementStoresKeepFields.
//
// Returns int which is the first hoistable read's PC, or zero when none is found.
// Returns bool which is true when a hoistable read is found.
func findHoistableReadInLoop(compiledFunction *program.CompiledFunction, loop loopRange, analysis *functionAnalysis, elementStoresKeepFields bool) (int, bool) {
	body := compiledFunction.Body
	facts := newLoopFacts(analysis, loop)
	for pc := loop.header; pc <= loop.latch; pc++ {
		if !isHoistableReadAt(body, pc) {
			continue
		}
		members := facts.loopMembers()
		if len(members) == 0 {
			return 0, false
		}
		if !readIsLoopInvariant(compiledFunction, members, pc, elementStoresKeepFields, false) {
			continue
		}
		if facts.dominatesLatch(pc) && hoistSafeAcrossLoopEntry(compiledFunction, loop, pc, facts.dominators()) {
			return pc, true
		}
		if readHoistsFromAnArm(compiledFunction, facts, pc) {
			return pc, true
		}
	}
	return 0, false
}

// readHoistsFromAnArm reports whether a loop-invariant read that does not run on every
// traversal may still move to the pre-header.
//
// It may when a dereference of the same receiver ahead of the loop proves the receiver
// non-nil, so the read cannot fault where the loop would not have run it, and when the
// destination is dead at the header, so writing it early changes nothing. The read must
// be a loop member, since invariance was judged against the members alone.
//
// Takes compiledFunction (*program.CompiledFunction) whose body holds the loop.
// Takes facts (*loopFacts) which supplies the loop, its members and the dominators.
// Takes readPC (int) which is the candidate read.
//
// Returns true when the read may be lifted.
func readHoistsFromAnArm(compiledFunction *program.CompiledFunction, facts *loopFacts, readPC int) bool {
	body := compiledFunction.Body
	destKind, ok := isa.KindForRole(hoistedReadDestBank(body, readPC))
	if !ok || !facts.isMember(readPC) {
		return false
	}
	if !receiverProvenNonNilAtHeader(compiledFunction, facts.loop.header, hoistedReadReceiverRegister(body, readPC), facts.dominators()) {
		return false
	}
	return registerDeadFrom(compiledFunction, body, facts.loop.header, destKind, hoistedReadDestRegister(body, readPC))
}

// readIsLoopInvariant reports whether a struct-field read is loop-invariant, meaning no
// instruction in the loop writes the receiver or destination register and no heap
// mutation could change the receiver's field.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes members ([]int) which lists every PC in the natural loop.
// Takes readPC (int) which is the PC of the candidate read.
// Takes elementStoresKeepFields (bool) which is ProgramFacts.ElementStoresKeepFields.
// Takes apartFromItsDestination (bool) which leaves writes of the destination register
// out of account, to find a read that a rename would make invariant.
//
// Returns true when the read is loop-invariant and safe to hoist.
func readIsLoopInvariant(
	compiledFunction *program.CompiledFunction, members []int, readPC int, elementStoresKeepFields, apartFromItsDestination bool,
) bool {
	body := compiledFunction.Body
	receiverReg := hoistedReadReceiverRegister(body, readPC)
	destReg := hoistedReadDestRegister(body, readPC)
	destBank := hoistedReadDestBank(body, readPC)
	writtenBank := destBank
	if apartFromItsDestination {
		writtenBank = isa.RoleNone
	}
	width := hoistedReadWordCount(body, readPC)
	for _, pc := range members {
		if pc >= readPC && pc < readPC+width {
			continue
		}
		if elementStoresKeepFields && unsignedElementStoreKeepsRead(body[pc], destBank) {
			continue
		}
		if !loopBodyInstructionPreservesRead(compiledFunction, pc, receiverReg, destReg, writtenBank) {
			return false
		}
	}
	return true
}

// renameReadWithAReusedDestination gives a field read its own destination register.
//
// Only single-word reads are renamed.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes loop (loopRange) which is the natural loop to search.
// Takes analysis (*functionAnalysis) which supplies the loop and reachability facts.
// Takes elementStoresKeepFields (bool) which is ProgramFacts.ElementStoresKeepFields.
//
// Returns true when a read was renamed.
func renameReadWithAReusedDestination(
	compiledFunction *program.CompiledFunction, loop loopRange, analysis *functionAnalysis, elementStoresKeepFields bool,
) bool {
	body := compiledFunction.Body
	facts := newLoopFacts(analysis, loop)
	members := facts.loopMembers()
	for pc := loop.header; pc <= loop.latch && len(members) != 0; pc++ {
		if !isHoistableReadAt(body, pc) || hoistedReadWordCount(body, pc) != 1 || !facts.isMember(pc) {
			continue
		}
		destKind, ok := isa.KindForRole(hoistedReadDestBank(body, pc))
		if !ok || readIsLoopInvariant(compiledFunction, members, pc, elementStoresKeepFields, false) ||
			!readIsLoopInvariant(compiledFunction, members, pc, elementStoresKeepFields, true) {
			continue
		}
		if renameDefinition(compiledFunction, facts.analysis, pc, 1, destKind, hoistedReadDestRegister(body, pc)) {
			return true
		}
	}
	return false
}

// unsignedElementStoreKeepsRead reports whether an unsigned element store preserves a
// read.
//
// Reads into the general bank are not excused because they may hold the very elements
// written. The caller must hold ProgramFacts.ElementStoresKeepFields, since the unsafe
// package can lay a slice over anything.
//
// Takes instruction (isa.Instruction) which is the loop instruction.
// Takes destBank (isa.OperandRole) which is the bank the read writes.
//
// Returns true when the store leaves the read's value as it was.
func unsignedElementStoreKeepsRead(instruction isa.Instruction, destBank isa.OperandRole) bool {
	if instruction.Op != isa.OpSetElementUint64 && !isa.InstrIsTier1SubOp(instruction, isa.SubOpSetSliceByteElement) {
		return false
	}
	return destBank != isa.RoleRegGeneral && destBank != isa.RoleNone
}

// loopBodyInstructionPreservesRead checks for mutation or register writes that invalidate
// a read.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc (int) which is the PC of the loop-body instruction being analysed.
// Takes receiverReg (uint8) which is the general-bank register holding the read's
// receiver.
// Takes destReg (uint8) which is the destination register the hoisted read writes.
// Takes destBank (isa.OperandRole) which is the operand bank role of destReg.
//
// Returns true when the instruction at pc preserves the hoisted read.
func loopBodyInstructionPreservesRead(compiledFunction *program.CompiledFunction, pc int, receiverReg, destReg uint8, destBank isa.OperandRole) bool {
	instruction := compiledFunction.Body[pc]
	if invalidatesCachedFieldReads(compiledFunction, instruction) && !structFieldWriteWithKnownReceiverDistinct(compiledFunction, pc, instruction, receiverReg) {
		return false
	}
	if !instructionShapeAllowsCseScan(instruction) {
		return false
	}
	if instructionWritesRegisterInBank(instruction, isa.RoleRegGeneral, receiverReg) {
		return false
	}
	if destBank != isa.RoleNone && instructionWritesRegisterInBank(instruction, destBank, destReg) {
		return false
	}
	return true
}

// identifyLoops scans the body for natural loops defined by back-edges.
//
// Takes body ([]isa.Instruction) which is the instruction stream to scan.
//
// Returns the loops in a deterministic order that places tight inner loops ahead of wider
// outer ones when the hoist cap is reached.
func identifyLoops(body []isa.Instruction) []loopRange {
	loops := make([]loopRange, 0, initialLoopCapacity)
	for pc := range body {
		targetPC, isJump := program.JumpTargetAt(body, pc)
		if !isJump || targetPC < 0 || targetPC > pc {
			continue
		}
		loops = append(loops, loopRange{header: targetPC, latch: pc})
	}
	sortLoopRangesAscendingSpan(loops)
	return loops
}

// sortLoopRangesAscendingSpan reorders loops with the smallest body span first so
// innermost loops get hoist preference.
//
// Takes loops ([]LoopRange) which is the slice of loop ranges to sort in place.
func sortLoopRangesAscendingSpan(loops []loopRange) {
	slices.SortFunc(loops, func(a, b loopRange) int {
		return cmp.Or(cmp.Compare(a.latch-a.header, b.latch-b.header), cmp.Compare(a.header, b.header))
	})
}

// isLinearFallThroughFrom reports whether an instruction can fall through to the next PC.
//
// Takes instruction (isa.Instruction) which is the instruction whose control-flow
// behaviour is being classified.
//
// Returns true when instruction falls through to the next sequential PC.
func isLinearFallThroughFrom(instruction isa.Instruction) bool {
	if instruction.Op != isa.OpDrillTier1 {
		return true
	}
	if isa.SubOpcode(instruction.A) == isa.SubOpTailCall {
		return false
	}
	return !tier1RedirectsControlFlow(instruction)
}

// tier1RedirectsControlFlow reports whether a tier-1 instruction transfers control.
//
// Takes instruction (isa.Instruction) which is the tier-1 instruction whose control-flow
// behaviour is being classified.
//
// Returns true when instruction redirects control rather than falling through.
func tier1RedirectsControlFlow(instruction isa.Instruction) bool {
	sub := isa.SubOpcode(instruction.A)
	if sub == isa.SubOpJump || sub == isa.SubOpTypeSwitchJump {
		return true
	}
	if sub != isa.SubOpDrillTier2 {
		return false
	}
	tier2 := isa.SubOpcodeTier2(instruction.B)
	if tier2 == isa.SubOpTier2Return {
		return true
	}
	return tier2 == isa.SubOpTier2DrillTier3 && isa.SubOpcodeTier3(instruction.C) == isa.SubOpTier3ReturnVoid
}

// isHoistableReadAt reports whether body[pc] is a struct-field read eligible for LICM.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the candidate PC to classify.
//
// Returns true when body[pc] begins a hoistable read.
func isHoistableReadAt(body []isa.Instruction, pc int) bool {
	if pc >= len(body) {
		return false
	}
	if isTier0StructFieldRead(body[pc].Op) || body[pc].Op == isa.OpLengthStructField {
		return true
	}
	return isTier1StructFieldReadAt(body, pc) || isSliceFieldReadAt(body, pc)
}

// isSliceFieldReadAt reports whether body[pc] and the extension word after it read a
// slice field of a struct into a typed-slice register.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the candidate PC to classify.
//
// Returns true for such a read.
func isSliceFieldReadAt(body []isa.Instruction, pc int) bool {
	if pc+1 >= len(body) || body[pc].Op != isa.OpDrillTier1 || body[pc+1].Op != isa.OpExtension {
		return false
	}
	_, ok := sliceFieldReadBank(isa.SubOpcode(body[pc].A))
	return ok
}

// sliceFieldReadBank returns the typed-slice bank a slice-field read writes.
//
// Takes sub (isa.SubOpcode) which is the tier-1 operation.
//
// Returns isa.RegisterKind which is the bank.
// Returns bool which is false when sub is not a slice-field read.
func sliceFieldReadBank(sub isa.SubOpcode) (isa.RegisterKind, bool) {
	switch sub {
	case isa.SubOpGetStructFieldSliceInt64:
		return isa.RegisterSliceInt64, true
	case isa.SubOpGetStructFieldSliceFloat64:
		return isa.RegisterSliceFloat64, true
	case isa.SubOpGetStructFieldSliceUint64:
		return isa.RegisterSliceUint64, true
	case isa.SubOpGetStructFieldSliceString:
		return isa.RegisterSliceString, true
	case isa.SubOpGetStructFieldSliceBool:
		return isa.RegisterSliceBool, true
	case isa.SubOpGetStructFieldSliceByte:
		return isa.RegisterSliceByte, true
	default:
		return 0, false
	}
}

// hoistedReadWordCount returns the number of instruction words a hoistable read at pc
// occupies in the body. Tier-0 reads are one word.
//
// Behaviour is undefined when isHoistableReadAt(body, pc) is false.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the PC of the hoistable read whose width is being measured.
//
// Returns the instruction-word width of the read.
func hoistedReadWordCount(body []isa.Instruction, pc int) int {
	if isTier1StructFieldReadAt(body, pc) || isSliceFieldReadAt(body, pc) {
		return 2
	}
	return 1
}

// hoistedReadDestBank returns the destination bank role of the read starting at pc. Used
// by the invariance analysis to detect writes that would clobber the cached value once
// the read is lifted to the pre-header.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the PC of the hoistable read whose destination bank is being
// queried.
//
// Returns the isa.OperandRole identifying the destination bank.
func hoistedReadDestBank(body []isa.Instruction, pc int) isa.OperandRole {
	if isTier1StructFieldReadAt(body, pc) {
		return tier1FieldReadDestRole(isa.SubOpcode(body[pc].A))
	}
	if isSliceFieldReadAt(body, pc) {
		bank, _ := sliceFieldReadBank(isa.SubOpcode(body[pc].A))
		return isa.RoleForKind(bank)
	}
	if body[pc].Op == isa.OpLengthStructField {
		return isa.RoleRegInt
	}
	return tier0FieldReadDestRole(body[pc].Op)
}

// hoistedReadDestRegister returns the destination register index of the read starting at
// pc.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the PC of the hoistable read whose destination register is
// being queried.
//
// Returns the destination register index.
func hoistedReadDestRegister(body []isa.Instruction, pc int) uint8 {
	if isTier1StructFieldReadAt(body, pc) || isSliceFieldReadAt(body, pc) {
		return body[pc].B
	}
	return body[pc].A
}

// hoistedReadReceiverRegister returns the general-bank register holding the struct
// receiver for the read starting at pc.
//
// Takes body ([]isa.Instruction) which is the compiled function's instruction stream.
// Takes pc (int) which is the PC of the hoistable read whose receiver register is being
// queried.
//
// Returns the receiver register index.
func hoistedReadReceiverRegister(body []isa.Instruction, pc int) uint8 {
	if isTier1StructFieldReadAt(body, pc) || isSliceFieldReadAt(body, pc) {
		return body[pc].C
	}
	return body[pc].B
}
