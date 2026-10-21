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
	"slices"

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

// controlFlowEdge is one way control passes from a word to another.
type controlFlowEdge struct {
	// from is the word control leaves.
	from int

	// to is the word control reaches.
	to int
}

// controlFlowEdges holds a body's fall-through edges, ascending, and then its jump edges,
// ascending by source.
type controlFlowEdges struct {
	// falls holds the fall-through edges.
	falls []controlFlowEdge

	// jumps holds the jump edges.
	jumps []controlFlowEdge
}

// controlFlowLists holds a body's control flow as adjacency lists in both directions.
type controlFlowLists struct {
	// successors lists, per word, the words control can reach in one step.
	successors [][]int

	// predecessors lists, per word, the words control can arrive from in one step.
	predecessors [][]int
}

// lists groups the edges into one list per word.
//
// Takes words (int) which is the number of words in the body.
// Takes ends (func(controlFlowEdge) (int, int)) which names the word an edge is listed
// under and the word listed there.
//
// Returns [][]int which holds, per word, its fall-through edge and then its jump edges,
// and nil for a word with neither.
func (edges controlFlowEdges) lists(words int, ends func(controlFlowEdge) (int, int)) [][]int {
	counts := make([]int, words)
	for _, group := range [][]controlFlowEdge{edges.falls, edges.jumps} {
		for _, edge := range group {
			under, _ := ends(edge)
			counts[under]++
		}
	}
	backing := make([]int, len(edges.falls)+len(edges.jumps))
	lists := make([][]int, words)
	offset := 0
	for word, count := range counts {
		if count != 0 {
			lists[word] = backing[offset : offset : offset+count]
			offset += count
		}
	}
	for _, group := range [][]controlFlowEdge{edges.falls, edges.jumps} {
		for _, edge := range group {
			under, listed := ends(edge)
			lists[under] = append(lists[under], listed)
		}
	}
	return lists
}

// RunPointerAliasAnalysis computes the per-PC alias environment.
//
// Bodies longer than 2000 instructions are skipped.
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
	width := aliasTrackedWidth(compiledFunction)
	info := program.NewPointerAliasInfo(len(body), width)
	if width == 0 {
		compiledFunction.AliasInfo = info
		return nil
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
		stored := info.ClassesAt(pc)
		if slices.Equal(stored, newEnv.Class[:len(stored)]) {
			continue
		}
		copy(stored, newEnv.Class[:])
		worklist = enqueueAliasSuccessors(successors[pc], len(body), enqueued, worklist)
	}
	compiledFunction.AliasInfo = info
	return nil
}

// aliasTrackedWidth returns how many general-bank registers, counted from register zero,
// the analysis can give a class other than wild.
//
// Those are the registers the entry environment seeds and the ones some instruction
// writes. A register past them is wild at every PC, so an environment that stops there
// answers every query as one as wide as the bank would.
//
// Takes compiledFunction (*program.CompiledFunction) whose parameters and body are read.
//
// Returns int which is the number of registers worth tracking.
func aliasTrackedWidth(compiledFunction *program.CompiledFunction) int {
	width := 0
	for index, kind := range compiledFunction.ParameterKinds {
		if kind == isa.RegisterGeneral && index < program.GeneralAliasBankSize {
			width = index + 1
		}
	}
	for _, inst := range compiledFunction.Body {
		if destination, ok := aliasGeneralDestRegister(inst); ok {
			width = max(width, int(destination)+1)
		}
	}
	return width
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
// Takes body ([]instruction) which is the function body whose control-flow predecessors
// are being computed.
//
// Returns a per-PC slice of predecessor PCs.
func buildAliasPredecessors(body []isa.Instruction) [][]int {
	edges := controlFlowEdgesOf(body)
	return edges.lists(len(body), func(edge controlFlowEdge) (int, int) { return edge.to, edge.from })
}

// controlFlowEdgesOf decodes every edge of body once.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns controlFlowEdges which holds the edges whose target lies in the body.
func controlFlowEdgesOf(body []isa.Instruction) controlFlowEdges {
	edges := controlFlowEdges{falls: make([]controlFlowEdge, 0, len(body)), jumps: nil}
	for pc, inst := range body {
		if pc+1 < len(body) && isLinearFallThroughFrom(inst) {
			edges.falls = append(edges.falls, controlFlowEdge{from: pc, to: pc + 1})
		}
		target, isJump := program.JumpTargetAt(body, pc)
		if isJump && target >= 0 && target < len(body) {
			edges.jumps = append(edges.jumps, controlFlowEdge{from: pc, to: target})
		}
	}
	return edges
}

// buildAliasSuccessors returns, per PC, the list of PCs whose execution can immediately
// follow this PC.
//
// Takes body ([]instruction) which is the function body whose control-flow successors are
// being computed.
//
// Returns a per-PC slice of successor PCs.
func buildAliasSuccessors(body []isa.Instruction) [][]int {
	edges := controlFlowEdgesOf(body)
	return edges.lists(len(body), func(edge controlFlowEdge) (int, int) { return edge.from, edge.to })
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
		classes := info.ClassesAt(pred)
		if classes == nil {
			continue
		}
		if first {
			copy(merged.Class[:], classes)
			first = false
			continue
		}
		mergeAliasClassesInto(&merged, classes)
	}
	return merged
}

// mergeAliasClassesInto widens destination to the least-upper-bound of destination and
// source.
//
// Takes destination (*AliasEnvironment) which is the environment widened in place.
// Takes source ([]AliasClass) which holds the classes of the tracked registers merged
// into destination.
func mergeAliasClassesInto(destination *program.AliasEnvironment, source []program.AliasClass) {
	for index, class := range source {
		if destination.Class[index] != class {
			destination.Class[index] = program.AliasClassWild
		}
	}
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
	case isa.OpAllocateIndirect, isa.OpAddress, isa.OpLoadGeneralConstant,
		isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointer,
		isa.OpGetField, isa.OpMoveGeneral, isa.OpGetElement, isa.OpMakeSlice,
		isa.OpGetStructFieldElementGeneral,
		isa.OpMakeClosure, isa.OpBoxAsInterface, isa.OpUnboxFromInterface:
		return inst.A, true
	default:
	}
	if inst.Op == isa.OpDrillTier1 {
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpBoxInt64, isa.SubOpBoxFloat64,
			isa.SubOpBoxString:
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
	case isa.OpAllocateIndirect, isa.OpAddress, isa.OpLoadGeneralConstant, isa.OpGetField:
		return memo.freshForPC(pc, minter)
	case isa.OpMoveGeneral:
		return env.Class[inst.B]
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointer:
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
