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
	// maxLicmHoistsPerFunction caps the number of struct-field reads that the loop-invariant
	// code motion pass will lift per CompiledFunction. Each hoist allocates one instruction
	// word in the body and may inflate register lifetimes; capping the count bounds the
	// cost.
	maxLicmHoistsPerFunction = 8

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
	return hoistLoopInvariantStructFieldReads(ctx, compiledFunction, nil)
}

// loopHeaderReachableByFallThrough reports whether linear fall-through enters the loop
// header.
//
// Takes body which is the compiled function's instruction stream.
// Takes loop which is the natural loop being considered for hoisting.
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
// Takes body which is the instruction stream to scan for jump targets.
//
// Returns a map whose keys are every PC reached by any jump in body.
func BuildAllJumpTargets(body []isa.Instruction) map[int]bool {
	return program.JumpTargetsOf(body)
}

// hoistLoopInvariantStructFieldReads is HoistLoopInvariantStructFieldReads with an
// optional shared analysis supplying the loop list; every hoist invalidates it because
// the insertion shifts every later PC.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which is the pipeline's shared analysis, or nil.
//
// Returns error when cancellation interrupts the pass.
func hoistLoopInvariantStructFieldReads(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("hoistLoopInvariantStructFieldReads cancelled: %w", err)
	}
	for range maxLicmHoistsPerFunction {
		if !tryOneStructFieldHoist(compiledFunction, analysis) {
			return nil
		}
	}
	return nil
}

// tryOneStructFieldHoist performs at most one hoist on compiledFunction.body.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which supplies the loop list when non-nil.
//
// Returns true when a hoist was applied, false when no hoistable read is found anywhere
// in the body.
func tryOneStructFieldHoist(compiledFunction *program.CompiledFunction, analysis *functionAnalysis) bool {
	loops := mergeLoopsSharingHeader(loopsFor(analysis, compiledFunction.Body))
	for _, loop := range loops {
		if !loopHeaderReachableByFallThrough(compiledFunction.Body, loop) {
			continue
		}
		readPC, ok := findHoistableReadInLoop(compiledFunction, loop, analysis)
		if !ok {
			continue
		}
		applyLoopHoist(compiledFunction, analysis, loop.header, readPC)
		return true
	}
	return false
}

// findHoistableReadInLoop locates the first hoistable read that dominates the back-edge,
// is loop-invariant and is safe to run on the entry path.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes loop (loopRange) which is the natural loop within which to search for a hoistable
// read.
// Takes analysis (*functionAnalysis) which supplies the dominator and loop information.
//
// Returns the first dominating read's PC and true on success; (0, false) otherwise.
func findHoistableReadInLoop(compiledFunction *program.CompiledFunction, loop loopRange, analysis *functionAnalysis) (int, bool) {
	body := compiledFunction.Body
	dominators := dominatorsFor(analysis, body)
	if dominators == nil {
		return 0, false
	}
	for pc := loop.header; pc <= loop.latch; pc++ {
		if !dominators.Dominates(pc, loop.latch) {
			continue
		}
		if !isHoistableReadAt(body, pc) {
			continue
		}
		if !readIsLoopInvariant(compiledFunction, loop, pc) {
			continue
		}
		if !hoistSafeAcrossLoopEntry(compiledFunction, loop, pc, dominators) {
			continue
		}
		return pc, true
	}
	return 0, false
}

// readIsLoopInvariant reports whether a struct-field read is loop-invariant, meaning no
// instruction in the loop writes the receiver or destination register and no heap
// mutation could change the receiver's field.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes loop which is the natural loop being analysed.
// Takes readPC which is the PC of the candidate read.
//
// Returns true when the read is loop-invariant and safe to hoist.
func readIsLoopInvariant(compiledFunction *program.CompiledFunction, loop loopRange, readPC int) bool {
	body := compiledFunction.Body
	receiverReg := hoistedReadReceiverRegister(body, readPC)
	destReg := hoistedReadDestRegister(body, readPC)
	destBank := hoistedReadDestBank(body, readPC)
	width := hoistedReadWordCount(body, readPC)
	for pc := loop.header; pc <= loop.latch; pc++ {
		if pc >= readPC && pc < readPC+width {
			continue
		}
		if !loopBodyInstructionPreservesRead(compiledFunction, pc, receiverReg, destReg, destBank) {
			return false
		}
	}
	return true
}

// loopBodyInstructionPreservesRead reports whether the instruction at pc keeps a hoisted
// struct-field read valid. The instruction must not mutate the read's heap state, must
// have a described operand shape, and must not clobber the receiver or destination
// register.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes pc which is the PC of the loop-body instruction being analysed.
// Takes receiverReg which is the general-bank register holding the read's receiver.
// Takes destReg which is the destination register the hoisted read writes.
// Takes destBank which is the operand bank role of destReg.
//
// Returns true when the instruction at pc preserves the hoisted read.
func loopBodyInstructionPreservesRead(compiledFunction *program.CompiledFunction, pc int, receiverReg, destReg uint8, destBank isa.OperandRole) bool {
	inst := compiledFunction.Body[pc]
	if invalidatesCachedFieldReads(compiledFunction, inst) && !structFieldWriteWithKnownReceiverDistinct(compiledFunction, pc, inst, receiverReg) {
		return false
	}
	if !instructionShapeAllowsCseScan(inst) {
		return false
	}
	if instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, receiverReg) {
		return false
	}
	if destBank != isa.RoleNone && instructionWritesRegisterInBank(inst, destBank, destReg) {
		return false
	}
	return true
}

// identifyLoops scans the body for natural loops defined by back-edges.
//
// Takes body which is the instruction stream to scan.
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

// isLinearFallThroughFrom reports whether inst falls through to the next PC. Conditional
// jumps return true because they fall through on the not-taken side.
//
// Takes inst which is the instruction whose control-flow behaviour is being classified.
//
// Returns true when inst falls through to the next sequential PC.
func isLinearFallThroughFrom(inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return true
	}
	if isa.SubOpcode(inst.A) == isa.SubOpTailCall {
		return false
	}
	return !tier1RedirectsControlFlow(inst)
}

// tier1RedirectsControlFlow reports whether a tier-1 instruction transfers control rather
// than falling through: direct jumps, returns, and the return-void tier-3 sub-op.
//
// Takes inst which is the tier-1 instruction whose control-flow behaviour is being
// classified.
//
// Returns true when inst redirects control rather than falling through.
func tier1RedirectsControlFlow(inst isa.Instruction) bool {
	sub := isa.SubOpcode(inst.A)
	if sub == isa.SubOpJump || sub == isa.SubOpTypeSwitchJump {
		return true
	}
	if sub != isa.SubOpDrillTier2 {
		return false
	}
	tier2 := isa.SubOpcodeTier2(inst.B)
	if tier2 == isa.SubOpTier2Return {
		return true
	}
	return tier2 == isa.SubOpTier2DrillTier3 && isa.SubOpcodeTier3(inst.C) == isa.SubOpTier3ReturnVoid
}

// isHoistableReadAt reports whether body[pc] is a struct-field read eligible for LICM.
//
// Takes body which is the compiled function's instruction stream.
// Takes pc which is the candidate PC to classify.
//
// Returns true when body[pc] begins a hoistable read.
func isHoistableReadAt(body []isa.Instruction, pc int) bool {
	if pc >= len(body) {
		return false
	}
	if isTier0StructFieldRead(body[pc].Op) {
		return true
	}
	if isTier1StructFieldReadAt(body, pc) {
		return true
	}
	return false
}

// hoistedReadWordCount returns the number of instruction words a hoistable read at pc
// occupies in the body. Tier-0 reads are one word; tier-1 reads are two (the
// isa.OpDrillTier1 umbrella plus the isa.OpExt layout extension).
//
// Behaviour is undefined when isHoistableReadAt(body, pc) is false.
//
// Takes body which is the compiled function's instruction stream.
// Takes pc which is the PC of the hoistable read whose width is being measured.
//
// Returns the instruction-word width of the read.
func hoistedReadWordCount(body []isa.Instruction, pc int) int {
	if isTier1StructFieldReadAt(body, pc) {
		return 2
	}
	return 1
}

// hoistedReadDestBank returns the destination bank role of the read starting at pc. Used
// by the invariance analysis to detect writes that would clobber the cached value once
// the read is lifted to the pre-header.
//
// Takes body which is the compiled function's instruction stream.
// Takes pc which is the PC of the hoistable read whose destination bank is being queried.
//
// Returns the isa.OperandRole identifying the destination bank.
func hoistedReadDestBank(body []isa.Instruction, pc int) isa.OperandRole {
	if isTier1StructFieldReadAt(body, pc) {
		return tier1FieldReadDestRole(isa.SubOpcode(body[pc].A))
	}
	return tier0FieldReadDestRole(body[pc].Op)
}

// hoistedReadDestRegister returns the destination register index of the read starting at
// pc.
//
// Takes body which is the compiled function's instruction stream.
// Takes pc which is the PC of the hoistable read whose destination register is being
// queried.
//
// Returns the destination register index.
func hoistedReadDestRegister(body []isa.Instruction, pc int) uint8 {
	if isTier1StructFieldReadAt(body, pc) {
		return body[pc].B
	}
	return body[pc].A
}

// hoistedReadReceiverRegister returns the general-bank register holding the struct
// receiver for the read starting at pc.
//
// Takes body which is the compiled function's instruction stream.
// Takes pc which is the PC of the hoistable read whose receiver register is being
// queried.
//
// Returns the receiver register index.
func hoistedReadReceiverRegister(body []isa.Instruction, pc int) uint8 {
	if isTier1StructFieldReadAt(body, pc) {
		return body[pc].C
	}
	return body[pc].B
}
