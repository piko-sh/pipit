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
	// copyFusionLookaheadLimit bounds the forward scan inside the liveness check used by
	// FuseCopyStructFieldGeneralT0, keeping compile-time bounded on pathologically large
	// functions.
	copyFusionLookaheadLimit = 24
)

// FuseCopyStructFieldGeneralT0 fuses an adjacent get/set struct-field pair into a single
// copy when the temp register is dead and the layouts are byte-compatible.
//
// Rewrites
//
//	GET_STRUCT_FIELD_GENERAL_T0 destination=T, sourceReceiver=A, sourceField=Fa
//	SET_STRUCT_FIELD_GENERAL_T0 destinationReceiver=B, value=T, destinationField=Fb
//
// into
//
//	COPY_STRUCT_FIELD_GENERAL_T0 sourceReceiver=A, destinationReceiver=B, sourceField=Fa
//	  isa.OpExt a=Fb
//
// A gap variant tolerates one neutral intermediate instruction between the GET and the
// SET (typically a CSE-emitted isa.OpMoveGeneral preparing the SET's receiver).
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes i (int) which is the candidate window start (GET position).
// Takes n (int) which is the length of body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true on a successful rewrite.
func FuseCopyStructFieldGeneralT0(compiledFunction *program.CompiledFunction,
	body []isa.Instruction,
	i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n {
		return false
	}

	getOp := body[i].Op
	if getOp != isa.OpGetStructFieldGeneral && getOp != isa.OpGetStructFieldRawPointerT0 {
		return false
	}
	if tryFuseCopyAdjacent(compiledFunction, body, i, n, jumpTargets) {
		return true
	}
	return tryFuseCopyWithGap(compiledFunction, body, i, n, jumpTargets)
}

// tryFuseCopyAdjacent handles the strict GET; SET adjacency case.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes i (int) which is the candidate window start (GET position).
// Takes n (int) which is the length of body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true on a successful rewrite.
func tryFuseCopyAdjacent(compiledFunction *program.CompiledFunction,
	body []isa.Instruction,
	i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n {
		return false
	}
	if body[i+1].Op != isa.OpSetStructFieldGeneral {
		return false
	}

	if jumpTargets[i+1] {
		return false
	}
	tempReg := body[i].A
	if body[i+1].B != tempReg {
		return false
	}
	sourceLayoutIndex := body[i].C
	destinationLayoutIndex := body[i+1].C
	if !copyFusionLayoutsCompatible(compiledFunction, sourceLayoutIndex, destinationLayoutIndex) {
		return false
	}

	if !generalRegisterIsDeadAfter(compiledFunction, body, n, i+2, tempReg, jumpTargets) {
		return false
	}
	sourceReceiver := body[i].B
	destinationReceiver := body[i+1].A
	body[i] = isa.NewInstruction(isa.OpCopyStructFieldGeneralT0, sourceReceiver, destinationReceiver, sourceLayoutIndex)
	body[i+1] = isa.NewInstruction(isa.OpExt, destinationLayoutIndex, 0, 0)
	return true
}

// copyFusionLayoutsCompatible reports whether two struct-field layouts have identical
// storage shapes for a direct typedmemmove. Both Kind (byte size) and FieldTypeIndex (GC
// pointer map) must match for the copy to be sound.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes sourceLayoutIndex (uint8) which is the source GET op's layout table index.
// Takes destinationLayoutIndex (uint8) which is the destination SET op's layout table
// index.
//
// Returns true when the two layouts are byte-for-byte interchangeable.
func copyFusionLayoutsCompatible(compiledFunction *program.CompiledFunction, sourceLayoutIndex, destinationLayoutIndex uint8) bool {
	if int(sourceLayoutIndex) >= len(compiledFunction.StructLayoutTable) {
		return false
	}
	if int(destinationLayoutIndex) >= len(compiledFunction.StructLayoutTable) {
		return false
	}
	sourceLayout := compiledFunction.StructLayoutTable[sourceLayoutIndex]
	destinationLayout := compiledFunction.StructLayoutTable[destinationLayoutIndex]
	if sourceLayout.Kind != destinationLayout.Kind {
		return false
	}
	if sourceLayout.FieldTypeIndex != destinationLayout.FieldTypeIndex {
		return false
	}
	return true
}

// tryFuseCopyWithGap handles the GET; X; SET case where a single neutral instruction X
// sits between the GET and the SET.
//
// The intermediate is typically a CSE-emitted isa.OpMoveGeneral preparing the SET's
// receiver. X is moved BEFORE the GET so the fused layout
//
//	X
//	COPY sourceReceiver=A, destinationReceiver=B, sourceField=Fa
//	EXT  destinationLayoutIndex=Fb
//
// preserves observable semantics. Safety requires that X neither reads nor writes the
// GET's destination register T (reads would change T's observed value once X runs before
// GET; writes are also caught by the post-SET liveness scan), X does not write the GET's
// source receiver register A (the COPY needs A intact), and PC i+1 and i+2 are not jump
// targets.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes i (int) which is the candidate window start (GET position).
// Takes n (int) which is the length of body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true on a successful rewrite.
func tryFuseCopyWithGap(compiledFunction *program.CompiledFunction,
	body []isa.Instruction,
	i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n {
		return false
	}
	if body[i+2].Op != isa.OpSetStructFieldGeneral {
		return false
	}
	if jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	tempReg := body[i].A
	sourceReceiver := body[i].B
	if body[i+2].B != tempReg {
		return false
	}
	sourceLayoutIndex := body[i].C
	destinationLayoutIndex := body[i+2].C
	if !copyFusionLayoutsCompatible(compiledFunction, sourceLayoutIndex, destinationLayoutIndex) {
		return false
	}
	intermediate := body[i+1]
	if intermediate.Op == isa.OpNop || intermediate.Op == isa.OpExt {
		return false
	}
	intermediateReads, intermediateWrites := classifyGeneralRegisterUse(intermediate, tempReg)
	if intermediateReads || intermediateWrites {
		return false
	}

	_, writesSource := classifyGeneralRegisterUse(intermediate, sourceReceiver)
	if writesSource {
		return false
	}

	if !generalRegisterIsDeadAfter(compiledFunction, body, n, i+3, tempReg, jumpTargets) {
		return false
	}
	destinationReceiver := body[i+2].A

	body[i] = intermediate
	body[i+1] = isa.NewInstruction(isa.OpCopyStructFieldGeneralT0, sourceReceiver, destinationReceiver, sourceLayoutIndex)
	body[i+2] = isa.NewInstruction(isa.OpExt, destinationLayoutIndex, 0, 0)
	return true
}

// generalRegisterIsDeadAfter reports whether a general-bank register is provably dead at
// startPC by scanning forward at most copyFusionLookaheadLimit instructions.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds say which general
// registers a return reads.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes n (int) which is the length of body.
// Takes startPC (int) which is the first PC the scan inspects.
// Takes reg (uint8) which is the general-bank register whose liveness is checked.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true on a clean write-before-read, false on a read-before-write or budget
// exhaustion.
func generalRegisterIsDeadAfter(compiledFunction *program.CompiledFunction, body []isa.Instruction, n, startPC int, reg uint8, jumpTargets map[int]bool) bool {
	return generalRegisterIsDeadFrom(compiledFunction, body, n, startPC, reg, jumpTargets, copyFusionLookaheadLimit)
}

// generalRegisterIsDeadFrom is the bounded forward scan behind
// generalRegisterIsDeadAfter. The budget caps total instructions inspected, following
// known branch layouts.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds say which general
// registers a return reads.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes n (int) which is the length of body.
// Takes startPC (int) which is the first PC the scan inspects.
// Takes reg (uint8) which is the general-bank register whose liveness is checked.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes budget (int) which is the remaining instruction inspection budget.
//
// Returns true when reg is provably dead within the budget, false otherwise.
func generalRegisterIsDeadFrom(compiledFunction *program.CompiledFunction, body []isa.Instruction, n, startPC int, reg uint8, jumpTargets map[int]bool, budget int) bool {
	pc := startPC
	for budget > 0 && pc < n {
		instr := body[pc]
		readsReg, writesReg := classifyGeneralRegisterUse(instr, reg)
		if readsReg {
			return false
		}
		if writesReg {
			return true
		}
		if program.IsReturnInstruction(instr) {
			return int(reg) >= returnSlotCount(compiledFunction, isa.RegisterGeneral)
		}
		layout, _ := isa.JumpLayoutOf(instr)
		if layout == isa.JumpLayoutNone {
			budget--
			pc++
			continue
		}
		targetPC, ok := program.JumpTargetAt(body, pc)
		if !ok || targetPC < 0 || targetPC >= n {
			return false
		}
		if isa.InstrIsTier1SubOp(instr, isa.SubOpJump) {
			budget--
			pc = targetPC
			continue
		}
		return scanBothConditionalSuccessors(compiledFunction, body, pc, targetPC, reg, jumpTargets, budget)
	}
	return false
}

// scanBothConditionalSuccessors recurses into the fall-through and taken edges of a
// conditional jump with a halved budget each.
//
// Both branches must independently confirm reg is dead; either reading it forces the
// fuser to refuse.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds say which general
// registers a return reads.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes pc (int) which is the conditional jump's PC.
// Takes targetPC (int) which is the branch's decoded taken-edge target.
// Takes reg (uint8) which is the general-bank register whose liveness is checked.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes budget (int) which is the remaining instruction inspection budget.
//
// Returns true when both successors confirm reg is dead.
func scanBothConditionalSuccessors(compiledFunction *program.CompiledFunction, body []isa.Instruction, pc, targetPC int, reg uint8, jumpTargets map[int]bool, budget int) bool {
	n := len(body)
	halfBudget := budget / 2
	if halfBudget < 1 {
		return false
	}

	fallThrough := pc + program.JumpFootprint(body[pc])
	if !generalRegisterIsDeadFrom(compiledFunction, body, n, fallThrough, reg, jumpTargets, halfBudget) {
		return false
	}
	return generalRegisterIsDeadFrom(compiledFunction, body, n, targetPC, reg, jumpTargets, halfBudget)
}

// classifyGeneralRegisterUse reports whether instr reads and writes general-bank register
// reg.
//
// Cases are grouped by the (reads, writes) signature so the table is dense and the switch
// has no identical branches. Anything not enumerated conservatively reports a read
// whenever reg appears as an operand, which is safe (fusion refused) but
// over-restrictive.
//
// Takes instr (instruction) which is the instruction to classify.
// Takes reg (uint8) which is the general-bank register being queried.
//
// Returns reads (bool) which is true when instr reads reg.
// Returns writes (bool) which is true when instr writes reg.
func classifyGeneralRegisterUse(instr isa.Instruction, reg uint8) (reads, writes bool) {
	if instr.Op == isa.OpDrillTier1 {
		switch isa.SubOpcode(instr.A) {
		case isa.SubOpCall, isa.SubOpTailCall, isa.SubOpCallMethod,
			isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE, isa.SubOpCallScalar:

			return true, false
		default:
			return classifyGeneralRegisterUseByShape(instr, reg)
		}
	}
	switch instr.Op {
	case isa.OpJumpIfFalse, isa.OpJumpIfTrue, isa.OpExt:

		return false, false
	case isa.OpGo, isa.OpDefer:

		return true, false
	case isa.OpLoadGeneralConst:

		return false, instr.A == reg
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0, isa.OpMoveGeneral:

		return instr.B == reg, instr.A == reg
	case isa.OpSetStructFieldGeneral, isa.OpCopyStructFieldGeneralT0:

		return instr.A == reg || instr.B == reg, false
	case isa.OpGetStructFieldIntT0, isa.OpGetStructFieldUint, isa.OpGetStructFieldFloat, isa.OpGetStructFieldBool:

		return instr.B == reg, false
	case isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint, isa.OpSetStructFieldFloat, isa.OpSetStructFieldBool,
		isa.OpTestNilJumpFalse, isa.OpTestNilJumpTrue:

		return instr.A == reg, false
	case isa.OpEqGeneral, isa.OpNeGeneral, isa.OpLtGeneral, isa.OpLeGeneral, isa.OpGtGeneral, isa.OpGeGeneral:

		return instr.B == reg || instr.C == reg, false
	default:
		return classifyGeneralRegisterUseByShape(instr, reg)
	}
}

// classifyGeneralRegisterUseByShape answers classifyGeneralRegisterUse from the operand
// shape of the operation the instruction executes.
//
// An operand counts when its role is the general bank, or a dynamic bank whose
// kind-marker sibling names the general bank or cannot be resolved. A shape with
// isa.ShapeFlagOpaqueWrites writes registers its operands do not name, so it is reported
// as a read, which refuses whatever fusion asked; an undescribed operation falls back to
// the conservative "any operand byte equal to reg is a read" rule.
//
// Takes instr (instruction) which is the instruction to classify.
// Takes reg (uint8) which is the general-bank register of interest.
//
// Returns reads (bool) which is true when instr may read reg.
// Returns writes (bool) which is true when instr provably writes reg.
func classifyGeneralRegisterUseByShape(instr isa.Instruction, reg uint8) (reads, writes bool) {
	shape := isa.ShapeForInstruction(instr)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return instr.A == reg || instr.B == reg || instr.C == reg, false
	}
	if shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return true, false
	}
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
	for position, role := range roles {
		if operands[position] != reg || !operandMayNameGeneral(instr, shape, role) {
			continue
		}
		reads = reads || shape.Reads[position]
		writes = writes || shape.Writes[position]
	}
	return reads, writes
}

// operandMayNameGeneral reports whether an operand with the given role can address the
// general bank: a general-bank role always can, a dynamic role can when its kind-marker
// sibling names the general bank or is absent, and every other role never can.
//
// Takes instr (instruction) which carries the kind-marker byte when the shape has one.
// Takes shape (isa.OperandShape) which is the instruction's operand shape.
// Takes role (isa.OperandRole) which is the operand's role.
//
// Returns true when the operand may name a general-bank register.
func operandMayNameGeneral(instr isa.Instruction, shape isa.OperandShape, role isa.OperandRole) bool {
	switch role {
	case isa.RoleRegGeneral:
		return true
	case isa.RoleRegDynamic:
		kind, ok := kindMarkerOperandOf(instr, shape)
		return !ok || kind == isa.RegisterGeneral
	default:
		return false
	}
}

// kindMarkerOperandOf returns the register kind carried by the shape's isa.RoleKindMarker
// operand, when it has one.
//
// Takes instr (instruction) which carries the marker byte.
// Takes shape (isa.OperandShape) which says which operand is the marker.
//
// Returns kind (isa.RegisterKind) which is the bank the marker names.
// Returns ok (bool) which is false when the shape has no kind-marker operand.
func kindMarkerOperandOf(instr isa.Instruction, shape isa.OperandShape) (kind isa.RegisterKind, ok bool) {
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
	for position, role := range roles {
		if role == isa.RoleKindMarker {
			return isa.RegisterKind(operands[position]), true
		}
	}
	return 0, false
}
