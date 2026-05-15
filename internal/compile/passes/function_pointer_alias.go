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

const (

	// maxAliasWorklistIterations caps worklist iterations as a defence-in-depth safety net.
	// When exceeded the analysis abandons the partial result, leaving AliasInfo nil so
	// mayAlias() falls back to its conservative "true".
	maxAliasWorklistIterations = 200000
)

// aliasClassMinter allocates fresh AliasClass values as the analysis encounters new
// origins. Wraps at the uint16 ceiling; after that every new origin collapses to wild
// (sound, just imprecise).
type aliasClassMinter struct {
	// next is the alias class identifier handed out by the next call to fresh.
	next program.AliasClass
}

// fresh returns the next unused alias class, or wild when the 16-bit space is exhausted.
//
// Returns the next alias class identifier, or AliasClassWild on overflow.
func (mint *aliasClassMinter) fresh() program.AliasClass {
	if mint.next == ^program.AliasClass(0) {
		return program.AliasClassWild
	}
	mint.next++
	return mint.next
}

// fieldClassMemo memoises deriveFieldAliasClass keyed by (pc, parentClass).
//
// Without memoisation the worklist would mint a fresh class on every revisit and the
// fixpoint could never converge.
type fieldClassMemo struct {
	// classes maps a (pc, parentClass) pair to the derived field class.
	classes map[fieldClassMemoKey]program.AliasClass
}

// newFieldClassMemo returns an empty field-class memo.
//
// Returns a memo ready to record derivations.
func newFieldClassMemo() *fieldClassMemo {
	return &fieldClassMemo{classes: make(map[fieldClassMemoKey]program.AliasClass)}
}

// freshForPC returns the memoised fresh class for a PC-only origin, minting on first use
// so worklist revisits are idempotent.
//
// Takes pc (int) which is the PC of the origin-producing instruction.
// Takes minter (*aliasClassMinter) which mints the class on first use.
//
// Returns the stable fresh class for pc.
func (memo *fieldClassMemo) freshForPC(pc int, minter *aliasClassMinter) program.AliasClass {
	key := fieldClassMemoKey{pc: pc, parentClass: program.AliasClassWild}
	if existing, ok := memo.classes[key]; ok {
		return existing
	}
	derived := minter.fresh()
	memo.classes[key] = derived
	return derived
}

// fieldClassMemoKey identifies one memoised field-class derivation.
type fieldClassMemoKey struct {
	// pc is the program counter of the field-load instruction.
	pc int

	// parentClass is the receiver register's alias class at that PC.
	parentClass program.AliasClass
}

// RunPointerAliasAnalysis populates compiledFunction.AliasInfo with the per-PC alias
// environment computed by a worklist forward dataflow analysis. Sound by construction:
// every transfer function over-approximates the runtime behaviour, and merge-points
// conservatively widen to wild when predecessors disagree.
//
// Skipped on empty bodies. Skipped silently when the body is too large to analyse
// efficiently (cap at 2000 instructions; very long functions are rare and the analysis
// cost grows superlinearly).
//
// Takes compiledFunction (*CompiledFunction) whose body and parameter registers the
// analysis inspects.
//
// Returns error when the context is cancelled before completion.
func RunPointerAliasAnalysis(ctx context.Context, compiledFunction *program.CompiledFunction) error {
	body := compiledFunction.Body
	if len(body) == 0 {
		return nil
	}
	const maxBodyForAliasAnalysis = 2000
	if len(body) > maxBodyForAliasAnalysis {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("runPointerAliasAnalysis cancelled: %w", err)
	}
	info := &program.PointerAliasInfo{
		PerPCEnv: make([]program.AliasEnvironment, len(body)),
	}
	minter := aliasClassMinter{next: 0}
	memo := newFieldClassMemo()
	entryEnv := seedEntryEnvironment(compiledFunction, &minter)
	predecessors := buildAliasPredecessors(body)
	successors := buildAliasSuccessors(body)
	worklist := []int{0}
	enqueued := make([]bool, len(body))
	enqueued[0] = true
	iteration := 0
	for len(worklist) > 0 {
		if iteration&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("runPointerAliasAnalysis cancelled: %w", err)
			}
		}
		iteration++
		if iteration > maxAliasWorklistIterations {
			compiledFunction.AliasInfo = nil
			return nil
		}
		pc := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]
		enqueued[pc] = false
		newEnv := applyAliasTransfer(compiledFunction, pc, new(mergeIncomingAliasEnvs(info, predecessors[pc], &entryEnv, pc == 0)), &minter, memo)
		if aliasEnvironmentEqual(&info.PerPCEnv[pc], &newEnv) {
			continue
		}
		info.PerPCEnv[pc] = newEnv
		worklist = enqueueAliasSuccessors(successors[pc], len(body), enqueued, worklist)
	}
	compiledFunction.AliasInfo = info
	return nil
}

// releaseAliasInfo drops the per-PC alias environment once its last consumer has run.
//
// Takes compiledFunction (*program.CompiledFunction) whose alias environment is released.
func releaseAliasInfo(compiledFunction *program.CompiledFunction) {
	if compiledFunction == nil {
		return
	}

	compiledFunction.AliasInfo = nil
}

// enqueueAliasSuccessors appends each in-range successor to the worklist when it is not
// already pending, marking it as enqueued.
//
// Takes successors ([]int) which are the candidate next PCs.
// Takes bodyLength (int) which bounds-checks successor entries.
// Takes enqueued ([]bool) which tracks pending PCs.
// Takes worklist ([]int) which is the worklist receiving the new PCs.
//
// Returns the updated worklist slice.
func enqueueAliasSuccessors(successors []int, bodyLength int, enqueued []bool, worklist []int) []int {
	for _, succ := range successors {
		if succ < 0 || succ >= bodyLength {
			continue
		}
		if enqueued[succ] {
			continue
		}
		enqueued[succ] = true
		worklist = append(worklist, succ)
	}
	return worklist
}

// seedEntryEnvironment produces the alias environment at function entry.
//
// Each general-bank parameter receives a fresh alias class. Non-parameter general
// registers start wild.
//
// Takes compiledFunction (*CompiledFunction) which is the compiled function whose
// parameter kinds determine seeding.
// Takes minter (*aliasClassMinter) which is the class minter used to allocate fresh
// per-parameter classes.
//
// Returns the seeded entry environment.
func seedEntryEnvironment(compiledFunction *program.CompiledFunction, minter *aliasClassMinter) program.AliasEnvironment {
	var env program.AliasEnvironment
	for index, kind := range compiledFunction.ParameterKinds {
		if kind != isa.RegisterGeneral {
			continue
		}
		if index >= program.GeneralAliasBankSize {
			continue
		}
		env.Class[index] = minter.fresh()
	}
	return env
}

// buildAliasPredecessors returns, per PC, the list of PCs whose execution can immediately
// precede this PC.
//
// Predecessors include the linear fall-through (PC-1 when that instruction is not an
// unconditional terminator) plus any in-body branch source whose target equals this PC.
//
// Takes body ([]instruction) which is the function body whose control-flow predecessors
// are being computed.
//
// Returns a per-PC slice of predecessor PCs.
func buildAliasPredecessors(body []isa.Instruction) [][]int {
	predecessors := make([][]int, len(body))
	for pc := 1; pc < len(body); pc++ {
		if isLinearFallThroughFrom(body[pc-1]) {
			predecessors[pc] = append(predecessors[pc], pc-1)
		}
	}
	for pc := range body {
		target, isJump := program.JumpTargetAt(body, pc)
		if !isJump || target < 0 || target >= len(body) {
			continue
		}
		predecessors[target] = append(predecessors[target], pc)
	}
	return predecessors
}

// buildAliasSuccessors returns, per PC, the list of PCs whose execution can immediately
// follow this PC.
//
// Mirrors buildAliasPredecessors and is used by the worklist to propagate updates.
//
// Takes body ([]instruction) which is the function body whose control-flow successors are
// being computed.
//
// Returns a per-PC slice of successor PCs.
func buildAliasSuccessors(body []isa.Instruction) [][]int {
	successors := make([][]int, len(body))
	for pc, inst := range body {
		if isLinearFallThroughFrom(inst) && pc+1 < len(body) {
			successors[pc] = append(successors[pc], pc+1)
		}
		target, isJump := program.JumpTargetAt(body, pc)
		if !isJump || target < 0 || target >= len(body) {
			continue
		}
		successors[pc] = append(successors[pc], target)
	}
	return successors
}

// mergeIncomingAliasEnvs combines predecessor post-environments into the pre-environment
// for pc.
//
// When predecessors disagree on a register's class, the merge widens to wild (safe but
// imprecise). At the entry PC the merge incorporates the function's seed environment so
// parameter classes flow through.
//
// Takes info (*PointerAliasInfo) which is the per-function alias output whose perPCEnv
// supplies predecessor environments.
// Takes preds ([]int) which is the predecessor PC list for the current PC.
// Takes entry (*AliasEnvironment) which is the seeded entry environment used when isEntry
// is true.
// Takes isEntry (bool) which is true when the current PC is the function entry PC.
//
// Returns the merged incoming environment.
func mergeIncomingAliasEnvs(info *program.PointerAliasInfo, preds []int, entry *program.AliasEnvironment, isEntry bool) program.AliasEnvironment {
	var merged program.AliasEnvironment
	first := true
	if isEntry {
		merged = *entry
		first = false
	}
	for _, pred := range preds {
		if pred < 0 || pred >= len(info.PerPCEnv) {
			continue
		}
		predEnv := &info.PerPCEnv[pred]
		if first {
			merged = *predEnv
			first = false
			continue
		}
		mergeAliasEnvironmentInto(&merged, predEnv)
	}
	return merged
}

// mergeAliasEnvironmentInto widens destination to the least-upper-bound of destination
// and source.
//
// For each register: if classes match, keep; otherwise widen to wild.
//
// Takes destination (*AliasEnvironment) which is the environment widened in place.
// Takes source (*AliasEnvironment) which is the environment merged into destination.
func mergeAliasEnvironmentInto(destination *program.AliasEnvironment, source *program.AliasEnvironment) {
	for index := range destination.Class {
		if destination.Class[index] != source.Class[index] {
			destination.Class[index] = program.AliasClassWild
		}
	}
}

// aliasEnvironmentEqual reports whether two environments hold identical classes in every
// register slot.
//
// Takes a (*AliasEnvironment) which is the first environment.
// Takes b (*AliasEnvironment) which is the second environment.
//
// Returns true when every register slot agrees; false otherwise.
func aliasEnvironmentEqual(a, b *program.AliasEnvironment) bool {
	for index := range a.Class {
		if a.Class[index] != b.Class[index] {
			return false
		}
	}
	return true
}

// applyAliasTransfer computes the post-environment for body[pc] given the merged
// pre-environment.
//
// Takes compiledFunction (*CompiledFunction) which is the compiled function whose
// body[pc] is being transferred.
// Takes pc (int) which is the program counter being processed.
// Takes env (*AliasEnvironment) which is the pre-environment merged from predecessors.
// Takes minter (*aliasClassMinter) which is the class minter used for fresh class
// allocation.
// Takes memo (*fieldClassMemo) which keeps field-load class derivations stable across
// worklist revisits.
//
// Returns the post-execution environment for body[pc].
func applyAliasTransfer(compiledFunction *program.CompiledFunction, pc int, env *program.AliasEnvironment, minter *aliasClassMinter, memo *fieldClassMemo) program.AliasEnvironment {
	result := *env
	inst := compiledFunction.Body[pc]
	if isAliasCallInstruction(inst) {
		clearAllGeneralAliasClasses(&result)
		return result
	}
	destination, ok := aliasGeneralDestRegister(inst)
	if !ok {
		return result
	}
	result.Class[destination] = computeAliasClassForWrite(&result, pc, inst, minter, memo)
	return result
}

// isAliasCallInstruction reports whether inst transfers control to another function in a
// way that may shuffle, alias, or return arbitrary pointers in the general bank.
//
// Takes inst (instruction) which is the instruction being classified.
//
// Returns true when inst is treated as a call boundary; false otherwise.
func isAliasCallInstruction(inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return false
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCall, isa.SubOpTailCall, isa.SubOpCallMethod,
		isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return true
	default:
	}
	return false
}

// clearAllGeneralAliasClasses widens every register's class to wild.
//
// Used at call boundaries where the callee may have aliased or reassigned any
// general-bank register the caller observes.
//
// Takes env (*AliasEnvironment) which is the environment whose classes are reset to wild
// in place.
func clearAllGeneralAliasClasses(env *program.AliasEnvironment) {
	for index := range env.Class {
		env.Class[index] = program.AliasClassWild
	}
}

// aliasGeneralDestRegister returns the general-bank destination register of inst, or (0,
// false) when inst writes no tracked general register.
//
// Takes inst (instruction) which is the instruction being inspected.
//
// Returns the general-bank destination register slot and true when inst writes a tracked
// general register; (0, false) otherwise.
func aliasGeneralDestRegister(inst isa.Instruction) (uint8, bool) {
	switch inst.Op {
	case isa.OpAllocIndirect, isa.OpAddr, isa.OpLoadGeneralConst,
		isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0,
		isa.OpGetField, isa.OpMoveGeneral, isa.OpIndex, isa.OpMakeSlice,
		isa.OpGetStructFieldIndexGeneral,
		isa.OpMakeClosure, isa.OpPackInterface, isa.OpUnpackInterface:
		return inst.A, true
	default:
	}
	if inst.Op == isa.OpDrillTier1 {
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpMoveIntToGeneral, isa.SubOpMoveFloatToGeneral,
			isa.SubOpMoveStringToGeneral:
			return inst.B, true
		default:
		}
	}
	return 0, false
}

// computeAliasClassForWrite returns the alias class for inst's destination after the
// transfer.
//
// Allocations, address-of and const loads mint a memoised fresh class per PC. Field loads
// derive from the receiver class. Same-bank moves propagate the source class. Everything
// else yields wild.
//
// Takes env (*AliasEnvironment) which is the pre-write environment supplying source
// classes.
// Takes pc (int) which is the PC of the write being analysed.
// Takes inst (instruction) which is the instruction performing the write.
// Takes minter (*aliasClassMinter) which is the class minter used for fresh allocations.
// Takes memo (*fieldClassMemo) which keeps class derivations stable across worklist
// revisits.
//
// Returns the class assigned to inst's destination after the write.
func computeAliasClassForWrite(env *program.AliasEnvironment, pc int, inst isa.Instruction, minter *aliasClassMinter, memo *fieldClassMemo) program.AliasClass {
	switch inst.Op {
	case isa.OpAllocIndirect, isa.OpAddr, isa.OpLoadGeneralConst, isa.OpGetField:
		return memo.freshForPC(pc, minter)
	case isa.OpMoveGeneral:
		return env.Class[inst.B]
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0:
		return deriveFieldAliasClass(env.Class[inst.B], pc, minter, memo)
	default:
	}
	return program.AliasClassWild
}

// deriveFieldAliasClass returns the alias class for a field load at pc whose receiver has
// parentClass.
//
// Wild receivers produce wild results. Otherwise a fresh class is minted on first visit
// and memoised by (pc, parentClass) so the fixpoint can converge.
//
// Takes parentClass (AliasClass) which is the alias class of the receiver register.
// Takes pc (int) which is the PC of the field-load instruction.
// Takes minter (*aliasClassMinter) which is the class minter used to allocate fresh
// classes.
// Takes memo (*fieldClassMemo) which records derivations for stability.
//
// Returns the derived class for the field load result.
func deriveFieldAliasClass(parentClass program.AliasClass, pc int, minter *aliasClassMinter, memo *fieldClassMemo) program.AliasClass {
	if parentClass == program.AliasClassWild {
		return program.AliasClassWild
	}
	key := fieldClassMemoKey{pc: pc, parentClass: parentClass}
	if existing, ok := memo.classes[key]; ok {
		return existing
	}
	derived := minter.fresh()
	memo.classes[key] = derived
	return derived
}
