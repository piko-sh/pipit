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
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// coalesceScanWindow limits how far back move coalescing searches for a source
	// definition.
	coalesceScanWindow = 64

	// maxCoalesceSweeps bounds the repeated sweeps that let one coalesced move expose
	// another.
	maxCoalesceSweeps = 4
)

// coalesceMovesPass removes same-bank moves by writing the moved value straight into its
// destination, or by reading the source directly in the extension word of the only
// instruction that uses the destination.
type coalesceMovesPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (coalesceMovesPass) Name() string { return "coalesce-moves" }

// Run applies coalesceMoves().
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when cancellation fires.
func (coalesceMovesPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if program.MayCreateSharedCells(compiledFunction) {
		return nil
	}
	for range maxCoalesceSweeps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !coalesceMoves(compiledFunction, state.Analysis.JumpTargets()) {
			return nil
		}
		state.Analysis.invalidate()
	}
	return nil
}

// coalesceCandidate describes one same-bank move.
type coalesceCandidate struct {
	// movePC is the move's PC.
	movePC int

	// kind is the move's register bank.
	kind isa.RegisterKind

	// destination is the register the move writes.
	destination uint8

	// source is the register the move reads.
	source uint8
}

// resultSlotAnswer caches whether only returns read the result slots during one sweep.
type resultSlotAnswer struct {
	// known is true once the answer has been worked out.
	known bool

	// free is the answer.
	free bool
}

// seenOnlyByReturns answers resultSlotsSeenOnlyByReturns() for the function, working it
// out on the first call.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being swept.
//
// Returns bool which is true when a move may be coalesced into a result slot.
func (answer *resultSlotAnswer) seenOnlyByReturns(compiledFunction *program.CompiledFunction) bool {
	if !answer.known {
		answer.free, answer.known = resultSlotsSeenOnlyByReturns(compiledFunction), true
	}
	return answer.free
}

// registerUse describes how one instruction names a renaming target and its replacement.
type registerUse struct {
	// readsTarget is true when an operand reads the target register.
	readsTarget bool

	// writesTarget is true when an operand writes the target register.
	writesTarget bool

	// touchesOther is true when an operand reads or writes the replacement register.
	touchesOther bool

	// writesOther is true when an operand writes the replacement register.
	writesOther bool
}

// note adds one operand's use of target or other.
//
// Takes operand (uint8) which is the register the operand names.
// Takes target (uint8) which is the register being renamed.
// Takes other (uint8) which is the register it is renamed to.
// Takes reads (bool) which reports whether the operand is read.
// Takes writes (bool) which reports whether the operand is written.
func (use *registerUse) note(operand, target, other uint8, reads, writes bool) {
	switch operand {
	case target:
		use.readsTarget = use.readsTarget || reads
		use.writesTarget = use.writesTarget || writes
	case other:
		use.touchesOther = true
		use.writesOther = use.writesOther || writes
	}
}

// definesTarget reports whether the instruction writes the target without reading it,
// which is where a rename walk ends.
//
// Returns bool which is true for a plain definition of the target.
func (use registerUse) definesTarget() bool {
	return use.writesTarget && !use.readsTarget
}

// allowsRename reports whether an instruction can sit inside a renamed stretch.
//
// Returns bool which is false when the replacement register is in use across the stretch.
func (use registerUse) allowsRename() bool {
	return !use.touchesOther || (use.definesTarget() && !use.writesOther)
}

// coalesceMoves makes one sweep over the body's same-bank moves.
//
// A move out of a result slot is left alone. A move into one is coalesced only where a
// return is all that reads the slot (resultSlotsSeenOnlyByReturns()).
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when some move was removed.
func coalesceMoves(compiledFunction *program.CompiledFunction, jumpTargets map[int]bool) bool {
	body := compiledFunction.Body
	changed := false
	var resultSlots resultSlotAnswer
	var joins joinEntries
	for pc := range body {
		kind, ok := moveOpcodeKind(body[pc])
		if !ok || kind == isa.RegisterComplex128 {
			continue
		}
		destination, source := moveOperands(body[pc])
		if destination == source || isResultSlot(compiledFunction, kind, source) {
			continue
		}
		if isResultSlot(compiledFunction, kind, destination) && !resultSlots.seenOnlyByReturns(compiledFunction) {
			continue
		}
		move := coalesceCandidate{kind: kind, movePC: pc, destination: destination, source: source}
		var removed bool
		if jumpTargets[pc] {
			removed = renameDefinitionsIntoJoinMove(compiledFunction, jumpTargets, joins.of(body), move)
		} else {
			removed = renameDefinitionIntoMove(compiledFunction, jumpTargets, move) || forwardMoveIntoExtension(compiledFunction, jumpTargets, move)
		}
		if removed {
			body[pc] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
			changed = true
		}
	}
	return changed
}

// isResultSlot reports whether reg is one of the function's result slots in bank kind,
// which returns read through metadata rather than operands.
//
// Takes compiledFunction (*program.CompiledFunction) whose result kinds are counted.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reg (uint8) which is the register.
//
// Returns true for a result slot.
func isResultSlot(compiledFunction *program.CompiledFunction, kind isa.RegisterKind, reg uint8) bool {
	return int(reg) < returnSlotCount(compiledFunction, kind)
}

// resultSlotsSeenOnlyByReturns reports whether a value may be computed straight into a
// result slot of the function, in place of a temporary and a move.
//
// Two things rule it out. A function that defers a call returns its result slots as they
// stand after a recovered panic, so a value computed early could be returned by a
// function that never reached the move. A function with named results keeps each result
// in a register of its own, which a return and the closures that capture it read without
// naming it. The temporary the move reads may be that register, and renaming it away
// would leave the named result unwritten.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function.
//
// Returns bool which is true when only a return reads the function's result slots.
func resultSlotsSeenOnlyByReturns(compiledFunction *program.CompiledFunction) bool {
	return len(compiledFunction.NamedResultLocations) == 0 &&
		!slices.ContainsFunc(compiledFunction.Body, func(inst isa.Instruction) bool { return inst.Op == isa.OpDefer })
}

// renameDefinitionIntoMove rewrites `t = ...; [in-place updates of t]; d = t` so the
// definition and updates name d, when t dies at the move and nothing between touches d.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which is the move.
//
// Returns true when the move became redundant.
func renameDefinitionIntoMove(compiledFunction *program.CompiledFunction, jumpTargets map[int]bool, move coalesceCandidate) bool {
	body := compiledFunction.Body
	renamed, ok := findRenameRange(body, jumpTargets, move)
	if !ok {
		return false
	}
	definition := renamed[len(renamed)-1]
	if namesDebugVariable(compiledFunction, move.kind, move.source, definition, move.movePC) ||
		!registerDeadFrom(compiledFunction, body, move.movePC+1, move.kind, move.source) {
		return false
	}
	for _, pc := range renamed {
		body[pc] = renameRegister(body[pc], move.kind, move.source, move.destination)
	}
	return true
}

// findRenameRange walks back from a move to its source definition, collecting
// instructions that use the source.
//
// The definition may also read the destination for an in-place update. Every other
// instruction in the range must leave the destination alone.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which is the move.
//
// Returns []int which lists the PCs to rename, with the definition last.
// Returns bool which is false on a branch destination, an unrenamable instruction or a
// use of the destination.
func findRenameRange(body []isa.Instruction, jumpTargets map[int]bool, move coalesceCandidate) ([]int, bool) {
	if jumpTargets[move.movePC] {
		return nil, false
	}
	return findRenameRangeBefore(body, jumpTargets, move, move.movePC)
}

// findRenameRangeBefore is findRenameRange for the straight line that ends just before a
// word, which may itself be a branch destination.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which names the registers.
// Takes end (int) which is the word the line runs up to.
//
// Returns []int which lists the PCs to rename, with the definition last.
// Returns bool which is false on a branch destination inside the line, an unrenamable
// instruction or a use of the destination.
func findRenameRangeBefore(body []isa.Instruction, jumpTargets map[int]bool, move coalesceCandidate, end int) ([]int, bool) {
	var renamed []int
	for pc := end - 1; pc >= max(0, end-coalesceScanWindow); pc-- {
		if pc+1 != end && jumpTargets[pc+1] {
			return nil, false
		}
		var use registerUse
		var ok bool
		if body[pc].Op == isa.OpExtension {
			pc, use, ok = extensionOwnerRegisterUse(body, jumpTargets, pc, move)
		} else {
			use, ok = plainRegisterUse(body[pc], move.kind, move.source, move.destination)
		}
		if !ok || !use.allowsRename() {
			return nil, false
		}
		if use.readsTarget || use.writesTarget {
			renamed = append(renamed, pc)
		}
		if use.definesTarget() {
			return renamed, true
		}
	}
	return nil, false
}

// forwardMoveIntoExtension rewrites `d = s; ...; op ... ext(d)` so the extension word
// reads s, when the words between neither name d nor write s, and d dies after op.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which is the move.
//
// Returns true when the move became redundant.
func forwardMoveIntoExtension(compiledFunction *program.CompiledFunction, jumpTargets map[int]bool, move coalesceCandidate) bool {
	body := compiledFunction.Body
	owner, ok := findExtensionReader(body, jumpTargets, move)
	if !ok {
		return false
	}
	extension := owner + 1
	if namesDebugVariable(compiledFunction, move.kind, move.destination, move.movePC, owner) ||
		!registerDeadFrom(compiledFunction, body, owner+instructionFootprint(body, owner), move.kind, move.destination) {
		return false
	}
	body[extension].A = move.source
	return true
}

// findExtensionReader finds the first instruction after the move that names its
// destination, requiring it to read the destination only through the A byte of its single
// extension word.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which is the move.
//
// Returns the owner's PC and false when no such reader follows.
func findExtensionReader(body []isa.Instruction, jumpTargets map[int]bool, move coalesceCandidate) (int, bool) {
	for pc := move.movePC + 1; pc < min(len(body), move.movePC+coalesceScanWindow); pc++ {
		if jumpTargets[pc] {
			return 0, false
		}
		if extensionReadsDestination(body, pc, move) {
			return pc, !jumpTargets[pc+1]
		}
		use, ok := plainRegisterUse(body[pc], move.kind, move.destination, move.source)
		if !ok || use.readsTarget || use.writesTarget {
			return 0, false
		}
		if use.touchesOther {
			if _, writesSource := instructionRegisterUse(body[pc], move.kind, isa.RoleForKind(move.kind), move.source); writesSource {
				return 0, false
			}
		}
	}
	return 0, false
}

// extensionReadsDestination reports whether the instruction at pc reads the move's
// destination through the A byte of one following extension word, and through nothing
// else, and leaves the source alone.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the candidate owner.
// Takes move (coalesceCandidate) which is the move.
//
// Returns true for such an owner.
func extensionReadsDestination(body []isa.Instruction, pc int, move coalesceCandidate) bool {
	if pc+1 >= len(body) || body[pc+1].Op != isa.OpExtension || IsCallInstruction(body[pc]) {
		return false
	}
	extension := body[pc+1]
	reads, writes, ok := isa.ExtensionRegisterUse(body[pc], body[pc+1:pc+2], move.kind)
	if !ok || !reads[0] || writes[0] || extension.A != move.destination ||
		extension.B == move.destination || extension.C == move.destination {
		return false
	}
	ownerReads, ownerWrites := instructionRegisterUse(body[pc], move.kind, isa.RoleForKind(move.kind), move.destination)
	_, writesSource := instructionRegisterUse(body[pc], move.kind, isa.RoleForKind(move.kind), move.source)
	return !ownerReads && !ownerWrites && !writesSource
}

// plainRegisterUse classifies how plain register operands name target and other.
//
// Takes inst (isa.Instruction) which is the instruction to classify.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes target (uint8) which is the register being renamed.
// Takes other (uint8) which is the register it is renamed to.
//
// Returns registerUse which classifies the operands.
// Returns bool which is false when the instruction cannot take part in a rename.
func plainRegisterUse(inst isa.Instruction, kind isa.RegisterKind, target, other uint8) (registerUse, bool) {
	use := registerUse{readsTarget: false, writesTarget: false, touchesOther: false, writesOther: false}
	if isCanonicalNoOperation(inst) {
		return use, true
	}
	shape := isa.ShapeForInstruction(inst)
	if refusesRename(inst, &shape) {
		return use, false
	}
	return describedRegisterUse(inst, shape, kind, target, other)
}

// describedRegisterUse classifies how the plain operands of inst name target and other in
// bank kind, for an instruction the caller has found renameable.
//
// Takes inst (isa.Instruction) which is the instruction to classify.
// Takes shape (isa.OperandShape) which is its shape.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes target (uint8) which is the register being renamed.
// Takes other (uint8) which is the register it is renamed to.
//
// Returns registerUse which classifies the operands.
// Returns bool which is false when an operand's bank cannot be told.
func describedRegisterUse(inst isa.Instruction, shape isa.OperandShape, kind isa.RegisterKind, target, other uint8) (registerUse, bool) {
	use := registerUse{readsTarget: false, writesTarget: false, touchesOther: false, writesOther: false}
	bankRole := isa.RoleForKind(kind)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	for position, role := range roles {
		if !operandMayNameBank(inst, shape, role, bankRole, kind) {
			continue
		}
		if role == isa.RoleRegDynamic {
			if _, marked := kindMarkerOperandOf(inst, shape); !marked {
				return use, false
			}
		}
		use.note(operands[position], target, other, shape.Reads[position], shape.Writes[position])
	}
	return use, true
}

// extensionOwnerRegisterUse classifies a head whose extension words name no register of
// the move's bank.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes last (int) which is the instruction's last extension word.
// Takes move (coalesceCandidate) which is the move being coalesced.
//
// Returns int which is the PC of the instruction's head word.
// Returns registerUse which is how the head word names the move's registers.
// Returns bool which is false when a jump lands inside the instruction, an extension word
// names a register of the bank or is not described, or the head word cannot be renamed.
func extensionOwnerRegisterUse(body []isa.Instruction, jumpTargets map[int]bool, last int, move coalesceCandidate) (int, registerUse, bool) {
	none := registerUse{readsTarget: false, writesTarget: false, touchesOther: false, writesOther: false}
	head := last
	for head >= 0 && body[head].Op == isa.OpExtension {
		if jumpTargets[head] {
			return head, none, false
		}
		head--
	}
	if head < 0 {
		return head, none, false
	}
	for word := head + 1; word <= last; word++ {
		reads, writes, known := isa.ExtensionRegisterUse(body[head], body[head+1:word+1], move.kind)
		if !known || reads != (isa.ExtensionIntMask{}) || writes != (isa.ExtensionIntMask{}) {
			return head, none, false
		}
	}
	shape := isa.ShapeForInstruction(body[head])
	if shape.Flags&isa.ShapeFlagFollowsExtension == 0 || refusesRenameOfHead(body[head], &shape) {
		return head, none, false
	}
	use, ok := describedRegisterUse(body[head], shape, move.kind, move.source, move.destination)
	return head, use, ok
}

// refusesRename reports whether control flow, opaque writes or operands outside the
// ordinary fields prevent register renaming.
//
// Takes inst (isa.Instruction) which is the instruction.
// Takes shape (*isa.OperandShape) which is its shape.
//
// Returns bool which is true when the instruction cannot take part in a rename.
func refusesRename(inst isa.Instruction, shape *isa.OperandShape) bool {
	return shape.Flags&isa.ShapeFlagFollowsExtension != 0 || refusesRenameOfHead(inst, shape)
}

// refusesRenameOfHead is refusesRename for a head word whose extension words are
// accounted for separately.
//
// Takes inst (isa.Instruction) which is the instruction.
// Takes shape (*isa.OperandShape) which is its shape.
//
// Returns bool which is true when the head word cannot take part in a rename.
func refusesRenameOfHead(inst isa.Instruction, shape *isa.OperandShape) bool {
	if inst.Op == isa.OpExtension || IsCallInstruction(inst) || shape.Flags&isa.ShapeFlagDescribed == 0 {
		return true
	}
	return shape.Flags&(isa.ShapeFlagOpaqueWrites|isa.ShapeFlagControlFlow|isa.ShapeFlagTerminator) != 0
}

// renameRegister replaces every operand of inst that names target in bank kind with
// replacement.
//
// Takes inst (isa.Instruction) which plainRegisterUse() accepted.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes target (uint8) which is the register being renamed.
// Takes replacement (uint8) which is the new register.
//
// Returns the rewritten instruction.
func renameRegister(inst isa.Instruction, kind isa.RegisterKind, target, replacement uint8) isa.Instruction {
	shape := isa.ShapeForInstruction(inst)
	bankRole := isa.RoleForKind(kind)
	if inst.A == target && operandMayNameBank(inst, shape, shape.A, bankRole, kind) {
		inst.A = replacement
	}
	if inst.B == target && operandMayNameBank(inst, shape, shape.B, bankRole, kind) {
		inst.B = replacement
	}
	if inst.C == target && operandMayNameBank(inst, shape, shape.C, bankRole, kind) {
		inst.C = replacement
	}
	return inst
}

// namesDebugVariable reports whether reg of bank kind holds a named variable anywhere in
// [from, to], so renaming it would show the debugger a stale value.
//
// Takes compiledFunction (*program.CompiledFunction) whose variable table is consulted.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reg (uint8) which is the register.
// Takes from (int) which is the first PC of the rewritten range.
// Takes to (int) which is the last PC of the rewritten range.
//
// Returns true when a variable scope names the register within the range.
func namesDebugVariable(compiledFunction *program.CompiledFunction, kind isa.RegisterKind, reg uint8, from, to int) bool {
	table := compiledFunction.DebugVarTable
	if table == nil {
		return false
	}
	for _, entry := range table.Entries {
		location := entry.Location
		if location.IsUpvalue || location.IsSpilled || location.Kind != kind || location.Register != reg {
			continue
		}
		end := entry.EndPC
		if end == 0 {
			end = len(compiledFunction.Body)
		}
		if entry.StartPC <= to && from < end {
			return true
		}
	}
	return false
}
