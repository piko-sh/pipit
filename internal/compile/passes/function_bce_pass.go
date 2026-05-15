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
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// minBceBodyLength is the smallest function body the BCE pass can usefully analyse;
	// three instructions are required to form the minimal LEN, LT, JUMP_IF_FALSE pattern
	// that establishes a proof.
	minBceBodyLength = 3

	// bceGenBankFlag tags a lengthOf or safeIndex entry as belonging to the reflect
	// (general) register bank, so the two banks share one state table without colliding.
	bceGenBankFlag uint16 = 0x8000
)

// derefStabilityTracker tracks idempotent DEREF writes to general-bank registers. A
// byte-identical DEREF whose source has not been written since the prior one preserves
// length facts, preventing `(*p)[i]` patterns from invalidating the BCE proof established
// by an earlier `len(*p)` call.
type derefStabilityTracker struct {
	// lastProducer holds the most recent instruction that wrote each general-bank register.
	lastProducer [isa.GeneralRegisterBankSize]isa.Instruction

	// writeVersion holds the monotonic version stamp of the most recent non-idempotent write
	// per register.
	writeVersion [isa.GeneralRegisterBankSize]uint32

	// nextVersion is the version stamp assigned to the next non-idempotent write.
	nextVersion uint32
}

// newDerefStabilityTracker builds a tracker primed so the first real DEREF write
// registers as fresh.
//
// Returns a tracker ready for use at the start of a basic block.
func newDerefStabilityTracker() derefStabilityTracker {
	return derefStabilityTracker{nextVersion: 1, lastProducer: [isa.GeneralRegisterBankSize]isa.Instruction{}, writeVersion: [isa.GeneralRegisterBankSize]uint32{}}
}

// Reset clears tracker state at basic-block boundaries (jump targets), matching the rest
// of the BCE pass's reset semantics.
func (t *derefStabilityTracker) Reset() {
	t.lastProducer = [isa.GeneralRegisterBankSize]isa.Instruction{}
	t.writeVersion = [isa.GeneralRegisterBankSize]uint32{}
	t.nextVersion = 1
}

// isIdempotentGeneralWrite reports whether writing inst to general[reg] reproduces the
// value already held there. Restricted to isa.OpDeref.
//
// Takes inst (isa.Instruction) which is the candidate instruction about to write
// general[reg].
// Takes reg (uint8) which is the destination general-bank register index.
//
// Returns true when the write reproduces the existing value.
func (t *derefStabilityTracker) isIdempotentGeneralWrite(inst isa.Instruction, reg uint8) bool {
	if inst.Op != isa.OpDeref {
		return false
	}
	if inst != t.lastProducer[reg] {
		return false
	}
	return t.writeVersion[inst.B] <= t.writeVersion[reg]
}

// recordGeneralWrite snapshots a non-idempotent write to general[reg] so subsequent
// idempotency checks have a producer to compare against.
//
// Takes inst (isa.Instruction) which is the instruction that performed the write.
// Takes reg (uint8) which is the destination general-bank register index.
func (t *derefStabilityTracker) recordGeneralWrite(inst isa.Instruction, reg uint8) {
	t.lastProducer[reg] = inst
	t.writeVersion[reg] = t.nextVersion
	t.nextVersion++
}

// ElideRedundantBoundsChecks rewrites bounds-checked slice and string access opcodes to
// their Unchecked variants where a prior LEN + LT + JUMP_IF_FALSE pattern proves the
// index is in range.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
func ElideRedundantBoundsChecks(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if len(body) < minBceBodyLength {
		return
	}
	elideRedundantBoundsChecks(compiledFunction, body, newFunctionAnalysis(compiledFunction))
}

// elideRedundantBoundsChecks is ElideRedundantBoundsChecks with the jump-target set
// supplied by the caller.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
// Takes analysis (*FunctionAnalysis) which supplies the jump-target set on demand.
func elideRedundantBoundsChecks(compiledFunction *program.CompiledFunction, body []isa.Instruction, analysis *functionAnalysis) {
	if len(body) < minBceBodyLength {
		return
	}
	jumpTargets := analysis.JumpTargets()
	var lengthOf [isa.GeneralRegisterBankSize]uint16
	var safeIndex [isa.GeneralRegisterBankSize]uint16
	var proofPCOf [isa.GeneralRegisterBankSize]uint16
	var nonNegative [isa.GeneralRegisterBankSize]bool
	tracker := newDerefStabilityTracker()
	for pc := range body {
		if jumpTargets[pc] {
			lengthOf = [isa.GeneralRegisterBankSize]uint16{}
			safeIndex = [isa.GeneralRegisterBankSize]uint16{}
			proofPCOf = [isa.GeneralRegisterBankSize]uint16{}
			nonNegative = [isa.GeneralRegisterBankSize]bool{}
			tracker.Reset()
		}
		inst := body[pc]
		rewriteIfBoundsSafe(compiledFunction, body, pc, &safeIndex, &proofPCOf, &nonNegative)
		invalidateBoundsFacts(inst, &lengthOf, &safeIndex, &tracker)
		invalidateProofPCsForCleared(&safeIndex, &proofPCOf)
		invalidateNonNegativeFacts(inst, &nonNegative)
		recordLenFact(inst, &lengthOf, &safeIndex)
		recordNonNegativeFact(compiledFunction, body, pc, &nonNegative)
		recordLtJumpFact(body, pc, &lengthOf, &safeIndex, &proofPCOf)
	}
}

// rewriteIfBoundsSafe rewrites a checked slice access at pc to its Unchecked variant when
// the safeIndex table proves the index is in range.
//
// The Unchecked rewrite fires only when the index register is BOTH recorded in safeIndex
// (proven index < len) AND flagged in nonNegative (proven index >= 0). The signed
// isa.OpLtInt comparison behind safeIndex does not exclude negative indices, so the
// second proof is required for soundness; without it the rewrite is refused.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]isa.Instruction) which is the instruction stream rewritten in place.
// Takes pc (int) which is the index of the candidate access instruction.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table consulted for the proof.
// Takes proofPCOf (*[isa.GeneralRegisterBankSize]uint16) which is the proof-source PC
// table used for annotation origins.
// Takes nonNegative (*[isa.GeneralRegisterBankSize]bool) which is the per-register
// non-negativity table that gates the rewrite.
func rewriteIfBoundsSafe(compiledFunction *program.CompiledFunction,
	body []isa.Instruction,
	pc int,
	safeIndex *[isa.GeneralRegisterBankSize]uint16,
	proofPCOf *[isa.GeneralRegisterBankSize]uint16,
	nonNegative *[isa.GeneralRegisterBankSize]bool,
) {
	inst := body[pc]
	idxReg := uint8(0)
	switch inst.Op {
	case isa.OpSliceGetIntDirect:
		if safeIndex[inst.C] != uint16(inst.B)+1 || !nonNegative[inst.C] {
			return
		}
		idxReg = inst.C
		body[pc] = isa.NewInstruction(isa.OpSliceGetIntDirectUnchecked, inst.A, inst.B, inst.C)
	case isa.OpSliceSetIntDirect:
		if safeIndex[inst.B] != uint16(inst.A)+1 || !nonNegative[inst.B] {
			return
		}
		idxReg = inst.B
		body[pc] = isa.NewInstruction(isa.OpSliceSetIntDirectUnchecked, inst.A, inst.B, inst.C)
	case isa.OpSliceGetInt:
		if safeIndex[inst.C] != uint16(inst.B)+1|bceGenBankFlag || !nonNegative[inst.C] {
			return
		}
		idxReg = inst.C
		body[pc] = isa.NewInstruction(isa.OpSliceGetIntUnchecked, inst.A, inst.B, inst.C)
	case isa.OpSliceSetInt:
		if safeIndex[inst.B] != uint16(inst.A)+1|bceGenBankFlag || !nonNegative[inst.B] {
			return
		}
		idxReg = inst.B
		body[pc] = isa.NewInstruction(isa.OpSliceSetIntUnchecked, inst.A, inst.B, inst.C)
	default:
		return
	}
	origin := pc
	if proofPCOf[idxReg] != 0 {
		origin = int(proofPCOf[idxReg]) - 1
	}
	RecordPeepholeRewrite(compiledFunction, pc, peepholeRewriteBce, origin)
}

// recordNonNegativeFact marks an int-bank register as provably non-negative when the
// instruction at pc loads a non-negative constant. Required because the signed LtInt
// comparison does not exclude negative indices.
//
// Takes compiledFunction (*program.CompiledFunction) which is the compiled function
// supplying the constant pool.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the current program counter.
// Takes nonNegative (*[isa.GeneralRegisterBankSize]bool) which is the per-register
// non-negativity table updated in place.
func recordNonNegativeFact(compiledFunction *program.CompiledFunction, body []isa.Instruction, pc int, nonNegative *[isa.GeneralRegisterBankSize]bool) {
	inst := body[pc]
	if inst.Op == isa.OpDrillTier1 && isa.SubOpcode(inst.A) == isa.SubOpLoadIntConstSmall {
		nonNegative[inst.B] = true
		return
	}
	if inst.Op != isa.OpLoadIntConst || inst.C != 0 {
		return
	}
	poolIndex := int(inst.B)
	if poolIndex < 0 || poolIndex >= len(compiledFunction.IntConstants) {
		return
	}
	if compiledFunction.IntConstants[poolIndex] >= 0 {
		nonNegative[inst.A] = true
	}
}

// invalidateNonNegativeFacts clears the non-negativity fact for every int-bank register
// that inst writes.
//
// Takes inst (isa.Instruction) which is the instruction whose writes are applied.
// Takes nonNegative (*[isa.GeneralRegisterBankSize]bool) which is the per-register
// non-negativity table updated in place.
func invalidateNonNegativeFacts(inst isa.Instruction, nonNegative *[isa.GeneralRegisterBankSize]bool) {
	for reg := range isa.GeneralRegisterBankSize {
		if !nonNegative[reg] {
			continue
		}
		if instructionWritesRegisterInBank(inst, isa.RoleRegInt, uint8(reg)) {
			nonNegative[reg] = false
		}
	}
}

// invalidateProofPCsForCleared clears the proofPCOf entry for any safeIndex slot that the
// invalidation step cleared.
//
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table.
// Takes proofPCOf (*[isa.GeneralRegisterBankSize]uint16) which is the proof-source PC
// table for each safeIndex slot.
func invalidateProofPCsForCleared(safeIndex *[isa.GeneralRegisterBankSize]uint16, proofPCOf *[isa.GeneralRegisterBankSize]uint16) {
	for k := range safeIndex {
		if safeIndex[k] == 0 {
			proofPCOf[k] = 0
		}
	}
}

// recordLenFact captures lengthOf[lenReg] = sliceReg + 1 when inst is a recognised length
// opcode (SubOpLenSliceIntDirect or SubOpLen). Reflect-bank facts are tagged with
// bceGenBankFlag to keep the two banks distinct.
//
// Takes inst (isa.Instruction) which is the candidate instruction to inspect.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// update.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to clear when lenReg is overwritten.
func recordLenFact(inst isa.Instruction, lengthOf *[isa.GeneralRegisterBankSize]uint16, safeIndex *[isa.GeneralRegisterBankSize]uint16) {
	if inst.Op != isa.OpDrillTier1 {
		return
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpLenSliceIntDirect:
		lenReg := inst.B
		sliceReg := inst.C
		clearFactsTargetingLenReg(lengthOf, safeIndex, lenReg)
		lengthOf[lenReg] = uint16(sliceReg) + 1
	case isa.SubOpLen:
		lenReg := inst.B
		sliceReg := inst.C
		clearFactsTargetingLenReg(lengthOf, safeIndex, lenReg)
		lengthOf[lenReg] = uint16(sliceReg) + 1 | bceGenBankFlag
	default:
	}
}

// recordLtJumpFact captures safeIndex[idxReg] = sliceReg + 1 when the two-instruction
// window starting at pc matches the bounds-proof pattern.
//
// The pattern is isa.OpLtInt followed by isa.OpJumpIfFalse with a matching condition
// register, and the right-hand side of the comparison must resolve to a known length via
// lengthOf.
//
// On the fall-through edge (subsequent PCs in linear order), the index register is
// provably less than the slice's length.
//
// Takes body ([]isa.Instruction) which is the instruction stream being analysed.
// Takes pc (int) which is the start index of the candidate two-instruction window.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table
// consulted to verify the comparison.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table updated on success.
// Takes proofPCOf (*[isa.GeneralRegisterBankSize]uint16) which is the proof-source PC
// table recorded for later attribution.
func recordLtJumpFact(body []isa.Instruction, pc int, lengthOf *[isa.GeneralRegisterBankSize]uint16, safeIndex *[isa.GeneralRegisterBankSize]uint16, proofPCOf *[isa.GeneralRegisterBankSize]uint16) {
	if pc+1 >= len(body) {
		return
	}
	cmp := body[pc]
	jmp := body[pc+1]
	var idxReg, lenReg uint8
	switch {
	case cmp.Op == isa.OpLtInt && jmp.Op == isa.OpJumpIfFalse && cmp.A == jmp.A:
		idxReg, lenReg = cmp.B, cmp.C
	case isa.InstrIsTier1SubOp(cmp, isa.SubOpLtIntJumpFalse) && jmp.Op == isa.OpExt:

		idxReg, lenReg = cmp.B, cmp.C
	default:
		return
	}
	if lengthOf[lenReg] == 0 {
		return
	}
	safeIndex[idxReg] = lengthOf[lenReg]
	proofPCOf[idxReg] = safeconv.IntToUint16(pc) + 1
}

// invalidateBoundsFacts kills facts whose underlying registers were just written, or
// clears all state when the instruction has effects the analysis cannot model.
//
// Takes inst (isa.Instruction) which is the instruction whose effects are being applied.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// update.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to update.
// Takes tracker (*derefStabilityTracker) which is the deref-stability tracker used to
// preserve idempotent re-DEREF facts.
func invalidateBoundsFacts(
	inst isa.Instruction,
	lengthOf *[isa.GeneralRegisterBankSize]uint16,
	safeIndex *[isa.GeneralRegisterBankSize]uint16,
	tracker *derefStabilityTracker,
) {
	if instructionClearsAllBoundsFacts(inst) {
		*lengthOf = [isa.GeneralRegisterBankSize]uint16{}
		*safeIndex = [isa.GeneralRegisterBankSize]uint16{}
		tracker.Reset()
		return
	}
	invalidateIntRegisterFacts(inst, lengthOf, safeIndex)
	invalidateSliceIntRegisterFacts(inst, lengthOf, safeIndex)
	invalidateGeneralRegisterFacts(inst, lengthOf, safeIndex, tracker)
}

// instructionClearsAllBoundsFacts reports whether inst has effects the BCE analysis
// cannot model precisely.
//
// The block list covers calls (callee may mutate any slice's length or any register),
// append (changes len), and slice-operation primitives that materially reshape slice
// headers. The pass clears all tracked facts when this returns true.
//
// Takes inst (instruction) which is the instruction to classify.
//
// Returns true when inst belongs to the conservative block list.
func instructionClearsAllBoundsFacts(inst isa.Instruction) bool {
	switch inst.Op {
	case isa.OpMakeClosure, isa.OpAppend, isa.OpAppendSpread, isa.OpAppendByteFast, isa.OpAppendByteFastInPlace,
		isa.OpAppendInPlace, isa.OpAppendSpreadInPlace,
		isa.OpSliceString:
		return true
	default:
	}
	if inst.Op == isa.OpDrillTier1 {
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpAppendInt, isa.SubOpAppendUint, isa.SubOpAppendFloat,
			isa.SubOpAppendString, isa.SubOpAppendBool,
			isa.SubOpStarAppendByteFast, isa.SubOpStarAppendByteSpread,
			isa.SubOpCall, isa.SubOpTailCall, isa.SubOpCallMethod,
			isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE,
			isa.SubOpDrillTier2:
			return true
		default:
		}
	}
	return false
}

// invalidateIntRegisterFacts kills lengthOf[X] and safeIndex[X] for every int-bank
// register X that inst writes.
//
// The CSE pass's register-write detection is reused so the analysis stays consistent with
// downstream peephole passes.
//
// Takes inst (isa.Instruction) which is the instruction whose writes are being applied.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// update.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to update.
func invalidateIntRegisterFacts(inst isa.Instruction, lengthOf *[isa.GeneralRegisterBankSize]uint16, safeIndex *[isa.GeneralRegisterBankSize]uint16) {
	for reg := range isa.GeneralRegisterBankSize {
		if lengthOf[reg] == 0 && safeIndex[reg] == 0 {
			continue
		}
		if instructionWritesRegisterInBank(inst, isa.RoleRegInt, uint8(reg)) {
			lengthOf[reg] = 0
			safeIndex[reg] = 0
		}
	}
}

// invalidateSliceIntRegisterFacts kills any fact whose underlying sliceReg matches a
// register written by inst in the slicesInt bank.
//
// A write to slicesInt[X] (for example via isa.SubOpMakeSliceInt) can change the slice's
// length, so any lengthOf and safeIndex entry naming X is stale.
//
// Takes inst (isa.Instruction) which is the instruction whose writes are being applied.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// update.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to update.
func invalidateSliceIntRegisterFacts(inst isa.Instruction, lengthOf *[isa.GeneralRegisterBankSize]uint16, safeIndex *[isa.GeneralRegisterBankSize]uint16) {
	for reg := range isa.GeneralRegisterBankSize {
		if !instructionWritesRegisterInBank(inst, isa.RoleRegSliceInt, uint8(reg)) {
			continue
		}
		target := uint16(reg) + 1
		for k := range lengthOf {
			if lengthOf[k] == target {
				lengthOf[k] = 0
			}
		}
		for k := range safeIndex {
			if safeIndex[k] == target {
				safeIndex[k] = 0
			}
		}
	}
}

// clearFactsTargetingLenReg removes any fact whose target register is lenReg, called
// before a fresh length fact is installed.
//
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// clear.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to clear.
// Takes lenReg (uint8) which is the int-bank register about to receive a new length fact.
func clearFactsTargetingLenReg(lengthOf *[isa.GeneralRegisterBankSize]uint16, safeIndex *[isa.GeneralRegisterBankSize]uint16, lenReg uint8) {
	lengthOf[lenReg] = 0
	target := uint16(lenReg) + 1
	for k := range safeIndex {
		if safeIndex[k]&^bceGenBankFlag == target {
			safeIndex[k] = 0
		}
	}
}

// invalidateGeneralRegisterFacts kills reflect-bank facts whose sliceReg matches a
// register written by inst in the general bank.
//
// A write to general[X] (for example via isa.OpMakeSlice into an addressable destination)
// can change the slice's length, so any lengthOf and safeIndex entry naming X in the
// reflect bank is stale, except when the write is an idempotent re-DEREF; see
// derefStabilityTracker for the soundness argument. Idempotent writes preserve the value
// general[X] already held, so any length fact pointing at general[X] remains valid.
//
// Takes inst (isa.Instruction) which is the instruction whose writes are being applied.
// Takes lengthOf (*[isa.GeneralRegisterBankSize]uint16) which is the length-fact table to
// update.
// Takes safeIndex (*[isa.GeneralRegisterBankSize]uint16) which is the safe-index fact
// table to update.
// Takes tracker (*derefStabilityTracker) which is the deref-stability tracker consulted
// for idempotent writes.
func invalidateGeneralRegisterFacts(
	inst isa.Instruction,
	lengthOf *[isa.GeneralRegisterBankSize]uint16,
	safeIndex *[isa.GeneralRegisterBankSize]uint16,
	tracker *derefStabilityTracker,
) {
	for reg := range isa.GeneralRegisterBankSize {
		if !instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, uint8(reg)) {
			continue
		}
		if tracker.isIdempotentGeneralWrite(inst, uint8(reg)) {
			continue
		}
		target := uint16(reg) + 1 | bceGenBankFlag
		for k := range lengthOf {
			if lengthOf[k] == target {
				lengthOf[k] = 0
			}
		}
		for k := range safeIndex {
			if safeIndex[k] == target {
				safeIndex[k] = 0
			}
		}
		tracker.recordGeneralWrite(inst, uint8(reg))
	}
}
