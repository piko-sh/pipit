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

// renameScanLimit bounds the words visited while collecting a definition's uses.
const renameScanLimit = 512

// definitionUses holds the words a definition reaches before its register is written
// again, and the instructions among them that read it.
type definitionUses struct {
	// members marks every word reached from the definition.
	members map[int]bool

	// uses lists the instructions that read the register.
	uses []int
}

// renameDefinition gives the definition at defPC a fresh register, rewriting every use it
// reaches, so a loop hoist is no longer blocked by other writers of the stack-allocated
// temporary it shared.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes analysis (*functionAnalysis) which supplies predecessor edges; invalidated on
// success.
// Takes defPC (int) which is the defining instruction.
// Takes width (int) which counts the definition's words: 1, or 2 when a truncation of the
// result follows it, which is renamed with it.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register the definition writes.
//
// Returns true when the definition was renamed.
func renameDefinition(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, defPC, width int, kind isa.RegisterKind, reg uint8) bool {
	body := compiledFunction.Body
	if compiledFunction.NumRegisters[kind] >= isa.GeneralRegisterBankSize || isResultSlot(compiledFunction, kind, reg) ||
		program.MayCreateSharedCells(compiledFunction) || resultSlotReadByRecover(compiledFunction, kind, reg) {
		return false
	}
	if use, ok := plainRegisterUse(body[defPC], kind, reg, reg); !ok || use.readsTarget || !use.writesTarget {
		return false
	}
	defEnd := defPC + max(width, instructionFootprint(body, defPC))
	found, ok := collectDefinitionUses(compiledFunction, defEnd, kind, reg)
	if !ok || !usesReachedOnlyFrom(analysis, found.members, found.uses, defPC, defEnd) {
		return false
	}
	last := defPC
	for _, use := range found.uses {
		last = max(last, use)
	}
	if namesDebugVariable(compiledFunction, kind, reg, min(defPC, last), last) || touchesDebugRange(compiledFunction, kind, reg, found.uses) {
		return false
	}
	fresh := uint8(compiledFunction.NumRegisters[kind])
	compiledFunction.NumRegisters[kind]++
	compiledFunction.PrecomputedAllocCountsValid = false
	for pc := defPC; pc < defPC+width; pc++ {
		body[pc] = renameRegister(body[pc], kind, reg, fresh)
	}
	for _, use := range found.uses {
		body[use] = renameRegister(body[use], kind, reg, fresh)
	}
	analysis.invalidate()
	return true
}

// touchesDebugRange reports whether reg names a debugger variable at any use, which
// matters when a use sits before the definition in PC order, as in a loop.
//
// Takes compiledFunction (*program.CompiledFunction) whose variable table is consulted.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reg (uint8) which is the register.
// Takes uses ([]int) which are the instructions that read it.
//
// Returns true when some use falls in a variable's scope.
func touchesDebugRange(compiledFunction *program.CompiledFunction, kind isa.RegisterKind, reg uint8, uses []int) bool {
	for _, use := range uses {
		if namesDebugVariable(compiledFunction, kind, reg, use, use) {
			return true
		}
	}
	return false
}

// collectDefinitionUses walks every path from start until reg is written or the frame
// ends, collecting the instructions that read it.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is walked.
// Takes start (int) which is the first word after the definition.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
//
// Returns the uses and false when a use cannot be renamed or the walk is too large.
func collectDefinitionUses(compiledFunction *program.CompiledFunction, start int, kind isa.RegisterKind, reg uint8) (definitionUses, bool) {
	found := definitionUses{members: make(map[int]bool), uses: nil}
	walk := useWalk{
		body:     compiledFunction.Body,
		liveness: newRegisterLivenessWalk(compiledFunction, compiledFunction.Body, kind, reg),
		found:    &found,
		pending:  []int{start},
		kind:     kind,
		reg:      reg,
	}
	for len(walk.pending) > 0 {
		pc := walk.pending[len(walk.pending)-1]
		walk.pending = walk.pending[:len(walk.pending)-1]
		if !walk.follow(pc) {
			return found, false
		}
	}
	return found, true
}

// useWalk follows the paths a definition reaches.
type useWalk struct {
	// body is the instruction stream.
	body []isa.Instruction

	// liveness classifies each word against the register.
	liveness *registerLivenessWalk

	// found collects the visited words and uses.
	found *definitionUses

	// pending holds path starts still to follow.
	pending []int

	// kind is the register's bank.
	kind isa.RegisterKind

	// reg is the register.
	reg uint8
}

// follow walks one path from pc until the register is written, the frame ends, the path
// joins a visited word, or a branch queues its successors.
//
// Takes pc (int) which is the first word of the path.
//
// Returns false when the path holds a use that cannot be renamed.
func (w *useWalk) follow(pc int) bool {
	owner := -1
	for pc < len(w.body) && !w.found.members[pc] {
		if len(w.found.members) >= renameScanLimit {
			return false
		}
		inst := w.body[pc]
		verdict, next := w.liveness.step(pc, &owner)
		if verdict == walkDead {
			return true
		}
		w.found.members[pc] = true
		switch verdict {
		case walkLive:
			if !renamableUse(inst, w.kind, w.reg) {
				return false
			}
			w.found.uses = append(w.found.uses, pc)
			owner = pc
			if layout, _ := isa.JumpLayoutOf(inst); layout != isa.JumpLayoutNone {
				return w.branch(pc, inst)
			}
			pc++
		case walkBranch:
			return w.branch(pc, inst)
		default:
			pc = next
		}
	}
	return true
}

// branch queues the successors of the jump at pc.
//
// Takes pc (int) which is the jump.
// Takes inst (isa.Instruction) which is the jump instruction.
//
// Returns false for a multi-way dispatch or an undecodable target.
func (w *useWalk) branch(pc int, inst isa.Instruction) bool {
	target, ok := program.JumpTargetAt(w.body, pc)
	if !ok {
		return false
	}
	footprint := program.JumpFootprint(inst)
	for word := pc + 1; word < pc+footprint; word++ {
		w.found.members[word] = true
	}
	w.pending = append(w.pending, target)
	if !isa.InstrIsTier1SubOp(inst, isa.SubOpJump) {
		w.pending = append(w.pending, pc+footprint)
	}
	return true
}

// renamableUse reports whether inst reads reg only through plain register operands and
// does not write it.
//
// Takes inst (isa.Instruction) which reads the register.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
//
// Returns true when renaming its operands is enough.
func renamableUse(inst isa.Instruction, kind isa.RegisterKind, reg uint8) bool {
	if inst.Op == isa.OpExt || IsCallInstruction(inst) || program.IsReturnInstruction(inst) {
		return false
	}
	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return false
	}
	bankRole := isa.RoleForKind(kind)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	named := false
	for position, role := range roles {
		if operands[position] != reg || !operandMayNameBank(inst, shape, role, bankRole, kind) {
			continue
		}
		if role == isa.RoleRegDynamic {
			if _, marked := kindMarkerOperandOf(inst, shape); !marked {
				return false
			}
		}
		if shape.Writes[position] {
			return false
		}
		named = true
	}
	return named
}

// usesReachedOnlyFrom proves every reachable path to a use starts at the definition, so
// no other definition of the register reaches it.
//
// Takes analysis (*functionAnalysis) which supplies predecessor edges.
// Takes members (map[int]bool) which marks the words reached from the definition.
// Takes uses ([]int) which are the uses to prove.
// Takes defStart (int) which is the definition's first word.
// Takes defEnd (int) which is the word after the definition's last word.
//
// Returns true when every predecessor chain stays inside members until the definition.
func usesReachedOnlyFrom(analysis *functionAnalysis, members map[int]bool, uses []int, defStart, defEnd int) bool {
	analysis.buildCFG()
	pending := append([]int(nil), uses...)
	checked := make(map[int]bool)
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if (pc >= defStart && pc < defEnd) || checked[pc] {
			continue
		}
		if pc == 0 || !members[pc] {
			return false
		}
		checked[pc] = true
		for _, pred := range analysis.predecessors[pc] {
			if analysis.reachable[pred] {
				pending = append(pending, pred)
			}
		}
	}
	return true
}
