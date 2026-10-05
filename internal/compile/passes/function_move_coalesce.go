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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// coalesceScanWindow bounds how far back a move looks for the definition of its source.
	coalesceScanWindow = 16

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

// coalesceMoves makes one sweep over the body's same-bank moves.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when some move was removed.
func coalesceMoves(compiledFunction *program.CompiledFunction, jumpTargets map[int]bool) bool {
	body := compiledFunction.Body
	changed := false
	for pc := range body {
		kind, ok := moveOpcodeKind(body[pc])
		if !ok || kind == isa.RegisterComplex || jumpTargets[pc] {
			continue
		}
		destination, source := moveOperands(body[pc])
		if destination == source || isResultSlot(compiledFunction, kind, destination) || isResultSlot(compiledFunction, kind, source) {
			continue
		}
		move := coalesceCandidate{kind: kind, movePC: pc, destination: destination, source: source}
		if renameDefinitionIntoMove(compiledFunction, jumpTargets, move) || forwardMoveIntoExtension(compiledFunction, jumpTargets, move) {
			body[pc] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
			changed = true
		}
	}
	return changed
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

// findRenameRange walks back from the move to the instruction that defines its source
// without reading it, collecting every instruction that names the source.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes move (coalesceCandidate) which is the move.
//
// Returns the PCs to rename, the definition last, and false when the walk meets a branch
// destination, an instruction it cannot rename, or a use of the destination.
func findRenameRange(body []isa.Instruction, jumpTargets map[int]bool, move coalesceCandidate) ([]int, bool) {
	var renamed []int
	for pc := move.movePC - 1; pc >= max(0, move.movePC-coalesceScanWindow); pc-- {
		if jumpTargets[pc+1] {
			return nil, false
		}
		use, ok := plainRegisterUse(body[pc], move.kind, move.source, move.destination)
		if !ok || use.touchesOther {
			return nil, false
		}
		if use.readsTarget || use.writesTarget {
			renamed = append(renamed, pc)
		}
		if use.writesTarget && !use.readsTarget {
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
	if pc+1 >= len(body) || body[pc+1].Op != isa.OpExt || IsCallInstruction(body[pc]) {
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

// registerUse describes how one instruction names a renaming target and its replacement.
type registerUse struct {
	// readsTarget is true when an operand reads the target register.
	readsTarget bool

	// writesTarget is true when an operand writes the target register.
	writesTarget bool

	// touchesOther is true when an operand reads or writes the replacement register.
	touchesOther bool
}

// plainRegisterUse classifies how inst names target and other in bank kind, refusing
// instructions whose register use is not fully described by plain operands: calls,
// control flow, extension-word owners, opaque writers and undescribed shapes.
//
// Takes inst (isa.Instruction) which is the instruction to classify.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes target (uint8) which is the register being renamed.
// Takes other (uint8) which is the register it is renamed to.
//
// Returns the use and false when the instruction cannot take part in a rename.
func plainRegisterUse(inst isa.Instruction, kind isa.RegisterKind, target, other uint8) (registerUse, bool) {
	use := registerUse{readsTarget: false, writesTarget: false, touchesOther: false}
	if isCanonicalNop(inst) {
		return use, true
	}
	shape := isa.ShapeForInstruction(inst)
	if inst.Op == isa.OpExt || IsCallInstruction(inst) || shape.Flags&isa.ShapeFlagDescribed == 0 ||
		shape.Flags&(isa.ShapeFlagOpaqueWrites|isa.ShapeFlagFollowsExtension|isa.ShapeFlagControlFlow|isa.ShapeFlagTerminator) != 0 {
		return use, false
	}
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
		switch operands[position] {
		case target:
			use.readsTarget = use.readsTarget || shape.Reads[position]
			use.writesTarget = use.writesTarget || shape.Writes[position]
		case other:
			use.touchesOther = true
		}
	}
	return use, true
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
