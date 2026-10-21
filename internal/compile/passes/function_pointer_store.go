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
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// pointerStoreWindow bounds how far back from a store the dereference that made its
// collection is looked for.
const pointerStoreWindow = 24

// pointerStoreChain is the dereference, and the move if there is one, that put a view of
// *p in the register an element store names.
type pointerStoreChain struct {
	// dereference is the PC of `DEREFERENCE view, pointer`.
	dereference int

	// move is the PC of `MOVE_GENERAL collection, view`, or -1 when the store names the view
	// itself.
	move int

	// pointer is the general register holding p.
	pointer uint8

	// view is the general register the dereference writes.
	view uint8

	// collection is the general register the store names.
	collection uint8
}

// storeElementThroughItsPointer rewrites an element store through a dereferenced pointer
// as a direct store through that pointer.
//
//   - The words from the dereference to the store form a straight line that no jump lands
//     in. They contain only register arithmetic, register moves, dereferences and int
//     element reads and stores, which cannot change a slice header or p.
//   - p is used as a pointer to a slice of 8-byte ints somewhere in that line.
//   - Nothing that can fault or be observed lies between the dereference and the next
//     instruction that dereferences p, so a nil p still fails before anything else
//     happens.
//   - The view and collection register are read by nothing else and are dead after the
//     store.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate store.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the store was rewritten.
func storeElementThroughItsPointer(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	store := body[i]
	if store.Op != isa.OpSetElementInt64 {
		return false
	}
	chain, found := dereferenceBehindStore(body, jumpTargets, i)
	if !found || !lineKeepsThePointerStore(body, chain, i) {
		return false
	}
	if !registerDeadFrom(compiledFunction, body, i+1, isa.RegisterGeneral, chain.collection) ||
		!registerDeadFrom(compiledFunction, body, i+1, isa.RegisterGeneral, chain.view) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpSetElementInt64ViaPointer, chain.pointer, store.B, store.C)
	body[chain.dereference] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	if chain.move >= 0 {
		body[chain.move] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
	return true
}

// dereferenceBehindStore walks back from the store at i to the dereference that made its
// collection, through at most one register move.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
// Takes i (int) which is the store.
//
// Returns chain (pointerStoreChain) which is the dereference and the move.
// Returns found (bool) which is false when a jump lands on the way, the register is made
// some other way, or the dereference is further back than the window.
func dereferenceBehindStore(body []isa.Instruction, jumpTargets map[int]bool, i int) (chain pointerStoreChain, found bool) {
	chain = pointerStoreChain{dereference: -1, move: -1, pointer: 0, view: body[i].A, collection: body[i].A}
	role := isa.RoleForKind(isa.RegisterGeneral)
	for pc := i - 1; pc >= max(0, i-pointerStoreWindow) && !jumpTargets[pc+1]; pc-- {
		instruction := body[pc]
		switch {
		case isCanonicalNoOperation(instruction):
		case instruction.Op == isa.OpDereference && instruction.A == chain.view:
			chain.dereference, chain.pointer = pc, instruction.B
			return chain, instruction.C != engine.DerefSnapshot
		case instruction.Op == isa.OpMoveGeneral && instruction.A == chain.view && chain.move < 0 &&
			(instruction.C == engine.MoveGeneralModeDynamic || instruction.C == engine.MoveGeneralModeAlias):
			chain.move, chain.view = pc, instruction.B
		default:
			if _, writes := instructionRegisterUse(instruction, isa.RegisterGeneral, role, chain.view); writes {
				return chain, false
			}
		}
	}
	return chain, false
}

// lineKeepsThePointerStore checks the words between a chain's dereference and the store
// at i against the conditions storeElementThroughItsPointer() lists.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes chain (pointerStoreChain) which is the dereference and the move.
// Takes i (int) which is the store.
//
// Returns bool which is true when the store may go through the pointer.
func lineKeepsThePointerStore(body []isa.Instruction, chain pointerStoreChain, i int) bool {
	role := isa.RoleForKind(isa.RegisterGeneral)
	dereferenced, witnessed := false, false
	for pc := chain.dereference + 1; pc < i; pc++ {
		instruction := body[pc]
		if pc == chain.move || isCanonicalNoOperation(instruction) {
			continue
		}
		if _, writes := instructionRegisterUse(instruction, isa.RegisterGeneral, role, chain.pointer); writes {
			return false
		}
		if namesRegister(instruction, role, chain.view) || namesRegister(instruction, role, chain.collection) {
			return false
		}
		switch {
		case quietInStraightLine(body, pc) || instruction.Op == isa.OpMoveGeneral:
		case readsThroughPointer(instruction, chain.pointer):
			dereferenced, witnessed = true, true
		case instruction.Op == isa.OpDereference && instruction.B == chain.pointer && instruction.C != engine.DerefSnapshot:
			dereferenced = true
		case dereferenced && touchesOnlyIntElements(instruction):
		default:
			return false
		}
	}
	return witnessed
}

// namesRegister reports whether an instruction reads or writes a general register.
//
// Takes instruction (isa.Instruction) which is the instruction.
// Takes role (isa.OperandRole) which is the role that names the general bank.
// Takes register (uint8) which is the register.
//
// Returns bool which is true when the instruction names it.
func namesRegister(instruction isa.Instruction, role isa.OperandRole, register uint8) bool {
	reads, writes := instructionRegisterUse(instruction, isa.RegisterGeneral, role, register)
	return reads || writes
}

// readsThroughPointer reports whether an instruction reads or writes an element of the
// slice of 8-byte ints that a general register points at.
//
// Takes instruction (isa.Instruction) which is the instruction.
// Takes pointer (uint8) which is the general register.
//
// Returns bool which is true for an element access through that register.
func readsThroughPointer(instruction isa.Instruction, pointer uint8) bool {
	switch instruction.Op {
	case isa.OpGetElementInt64ViaPointer:
		return instruction.B == pointer
	case isa.OpSetElementInt64ViaPointer:
		return instruction.A == pointer
	default:
		return false
	}
}

// touchesOnlyIntElements reports whether an instruction is a dereference or an int
// element read or store, none of which changes a slice header or a pointer.
//
// Takes instruction (isa.Instruction) which is the instruction.
//
// Returns bool which is true for such an instruction.
func touchesOnlyIntElements(instruction isa.Instruction) bool {
	switch instruction.Op {
	case isa.OpGetElementInt64ViaPointer, isa.OpSetElementInt64ViaPointer, isa.OpGetElementInt64, isa.OpSetElementInt64:
		return true
	case isa.OpDereference:
		return instruction.C != engine.DerefSnapshot
	default:
		return false
	}
}

// quietInStraightLine reports whether the instruction at pc only computes a register from
// registers and constants, cannot fault and does not jump.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the instruction.
//
// Returns bool which is true for such an instruction.
func quietInStraightLine(body []isa.Instruction, pc int) bool {
	if layout, _ := isa.JumpLayoutOf(body[pc]); layout != isa.JumpLayoutNone {
		return false
	}
	return quietRegisterOperation(body, pc)
}
