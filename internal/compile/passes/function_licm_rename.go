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
	"slices"
)

// renameScanLimit bounds the words visited while collecting a definition's uses.
const renameScanLimit = 512

// definitionUses holds the words a definition reaches before its register is written
// again, and the instructions among them that read it.
type definitionUses struct {
	// uses lists the instructions that read the register.
	uses []int

	// extensionUses lists the extension words that read the register.
	extensionUses []extensionUse

	// members marks every word reached from the definition.
	members reachedWords

	// redefiners marks the uses that also write the register, where the definition's value
	// stops.
	redefiners reachedWords
}

// extensionUse is one extension word that reads the register being renamed.
type extensionUse struct {
	// pc is the extension word.
	pc int

	// owner is the instruction the word belongs to.
	owner int

	// bytes marks the bytes of the word that name the register as a read.
	bytes isa.ExtensionIntMask
}

// reachChecks lists reading instructions and extension-word owners to check.
//
// Returns []int which holds their program counters.
func (found *definitionUses) reachChecks() []int {
	checks := slices.Clone(found.uses)
	for _, use := range found.extensionUses {
		checks = append(checks, use.owner)
	}
	return checks
}

// renameDefinition gives a definition and the uses it reaches a fresh register.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes analysis (*functionAnalysis) which supplies predecessor edges. It stays current,
// because a rename changes register operands and no word's place or target.
// Takes definitionPC (int) which is the defining instruction.
// Takes width (int) which counts the definition's words. It is 2 when a truncation
// follows the result and is renamed with it, or 1 otherwise.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register the definition writes.
//
// Returns true when the definition was renamed.
func renameDefinition(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, definitionPC, width int, kind isa.RegisterKind, reg uint8) bool {
	body := compiledFunction.Body
	if compiledFunction.NumRegisters[kind] >= isa.GeneralRegisterBankSize || isResultSlot(compiledFunction, kind, reg) ||
		program.MayCreateSharedCells(compiledFunction) || resultSlotReadByRecover(compiledFunction, kind, reg) {
		return false
	}
	if use, ok := plainRegisterUse(body[definitionPC], kind, reg, reg); !ok || use.readsTarget || !use.writesTarget {
		return false
	}
	defEnd := definitionPC + max(width, instructionFootprint(body, definitionPC))
	found, ok := collectDefinitionUses(compiledFunction, defEnd, kind, reg)
	reads := found.reachChecks()
	if !ok || !usesReachedOnlyFrom(analysis, &found.members, reads, &found.redefiners, definitionPC, defEnd) {
		return false
	}
	last := definitionPC
	for _, use := range reads {
		last = max(last, use)
	}
	if namesDebugVariable(compiledFunction, kind, reg, min(definitionPC, last), last) || touchesDebugRange(compiledFunction, kind, reg, reads) {
		return false
	}
	fresh := uint8(compiledFunction.NumRegisters[kind])
	compiledFunction.NumRegisters[kind]++
	compiledFunction.PrecomputedAllocCountsValid = false
	for pc := definitionPC; pc < definitionPC+width; pc++ {
		body[pc] = renameRegister(body[pc], kind, reg, fresh)
	}
	for _, use := range found.uses {
		body[use] = renameRegisterReads(body[use], kind, reg, fresh)
	}
	for _, use := range found.extensionUses {
		body[use.pc] = renameExtensionReads(body[use.pc], use.bytes, fresh)
	}
	return true
}

// renameExtensionReads replaces the marked bytes of an extension word with replacement.
//
// Takes word (isa.Instruction) which is the extension word.
// Takes bytes (isa.ExtensionIntMask) which marks the bytes that name the renamed register
// as a read.
// Takes replacement (uint8) which is the new register.
//
// Returns the rewritten word.
func renameExtensionReads(word isa.Instruction, bytes isa.ExtensionIntMask, replacement uint8) isa.Instruction {
	operands := [isa.NumInstructionOperands]*uint8{&word.A, &word.B, &word.C}
	for position, operand := range operands {
		if bytes[position] {
			*operand = replacement
		}
	}
	return word
}

// renameRegisterReads replaces the reading operands of a register with a replacement.
//
// Takes instruction (isa.Instruction) which renamableUse() accepted.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes target (uint8) which is the register being renamed.
// Takes replacement (uint8) which is the new register.
//
// Returns the rewritten instruction.
func renameRegisterReads(instruction isa.Instruction, kind isa.RegisterKind, target, replacement uint8) isa.Instruction {
	shape := isa.ShapeForInstruction(instruction)
	bankRole := isa.RoleForKind(kind)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]*uint8{&instruction.A, &instruction.B, &instruction.C}
	for position, role := range roles {
		if *operands[position] != target || shape.Writes[position] || !operandMayNameBank(instruction, shape, role, bankRole, kind) {
			continue
		}
		*operands[position] = replacement
	}
	return instruction
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
// Returns definitionUses which holds the collected uses.
// Returns bool which is false when a use cannot be renamed or the walk is too large.
func collectDefinitionUses(compiledFunction *program.CompiledFunction, start int, kind isa.RegisterKind, reg uint8) (definitionUses, bool) {
	found := definitionUses{
		members: reachedWords{set: nil, count: 0}, redefiners: reachedWords{set: nil, count: 0}, uses: nil, extensionUses: nil,
	}
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
	for pc < len(w.body) && !w.found.members.has(pc) {
		if w.found.members.count >= renameScanLimit {
			return false
		}
		instruction := w.body[pc]
		verdict, next := w.liveness.step(pc, &owner)
		if verdict == walkDead {
			return true
		}
		w.found.members.add(pc)
		switch verdict {
		case walkLive:
			ended, ok := w.use(pc, owner, instruction)
			if ended {
				return ok
			}
			if instruction.Op != isa.OpExtension {
				owner = pc
			}
			pc++
		case walkBranch:
			return w.branch(pc, instruction)
		default:
			pc = next
		}
	}
	return true
}

// use records the read of the register at pc.
//
// Takes pc (int) which is the reading word.
// Takes owner (int) which is the last instruction seen on the path, the owner of an
// extension word at pc, or -1 when there is none.
// Takes instruction (isa.Instruction) which is the instruction there.
//
// Returns ended (bool) which is true when the path stops at pc.
// Returns ok (bool) which is false when the use cannot be renamed or the jump cannot be
// followed.
func (w *useWalk) use(pc, owner int, instruction isa.Instruction) (ended, ok bool) {
	if instruction.Op == isa.OpExtension {
		return w.extensionUse(pc, owner)
	}
	renamable, redefines := renamableUse(instruction, w.kind, w.reg)
	if !renamable {
		return true, false
	}
	if redefines && w.liveness.extensionWordsRead(pc) {
		return true, false
	}
	w.found.uses = append(w.found.uses, pc)
	if redefines {
		w.found.redefiners.add(pc)
		return true, true
	}
	if layout, _ := isa.JumpLayoutOf(instruction); layout != isa.JumpLayoutNone {
		return true, w.branch(pc, instruction)
	}
	return false, true
}

// extensionUse records the read of the register by the extension word at pc.
//
// The word is renamable when its owner's extension layout is recorded for the bank, so
// the bytes that name a register are told apart from offsets and indices, and when no
// byte names the register as a write. A call's or a return's words are never renamed.
//
// Takes pc (int) which is the extension word.
// Takes owner (int) which is the instruction it belongs to, or -1.
//
// Returns ended (bool) which is true when the path stops at pc.
// Returns ok (bool) which is false when the word cannot be renamed.
func (w *useWalk) extensionUse(pc, owner int) (ended, ok bool) {
	if owner < 0 || IsCallInstruction(w.body[owner]) || program.IsReturnInstruction(w.body[owner]) {
		return true, false
	}
	reads, writes, known := isa.ExtensionRegisterUse(w.body[owner], w.body[owner+1:pc+1], w.kind)
	if !known {
		return true, false
	}
	word := w.body[pc]
	var named isa.ExtensionIntMask
	for position, operand := range [isa.NumInstructionOperands]uint8{word.A, word.B, word.C} {
		if operand != w.reg {
			continue
		}
		if writes[position] {
			return true, false
		}
		named[position] = reads[position]
	}
	if named == (isa.ExtensionIntMask{}) {
		return true, false
	}
	w.found.extensionUses = append(w.found.extensionUses, extensionUse{pc: pc, owner: owner, bytes: named})
	return false, true
}

// branch queues the successors of the jump at pc.
//
// Takes pc (int) which is the jump.
// Takes instruction (isa.Instruction) which is the jump instruction.
//
// Returns false for a multi-way dispatch or an undecodable target.
func (w *useWalk) branch(pc int, instruction isa.Instruction) bool {
	target, ok := program.JumpTargetAt(w.body, pc)
	if !ok {
		return false
	}
	footprint := program.JumpFootprint(instruction)
	for word := pc + 1; word < pc+footprint; word++ {
		w.found.members.add(word)
	}
	w.pending = append(w.pending, target)
	if !isa.InstrIsTier1SubOp(instruction, isa.SubOpJump) {
		w.pending = append(w.pending, pc+footprint)
	}
	return true
}

// renamableUse checks whether plain operands cover a register's reads and redefinition.
//
// Takes instruction (isa.Instruction) which reads the register.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
//
// Returns renamable (bool) which is true when renaming its reading operands is enough.
// Returns redefines (bool) which is true when another operand writes the register, so the
// definition being renamed reaches no further along this path.
func renamableUse(instruction isa.Instruction, kind isa.RegisterKind, reg uint8) (renamable, redefines bool) {
	if instruction.Op == isa.OpExtension || IsCallInstruction(instruction) || program.IsReturnInstruction(instruction) {
		return false, false
	}
	shape := isa.ShapeForInstruction(instruction)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return false, false
	}
	bankRole := isa.RoleForKind(kind)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instruction.A, instruction.B, instruction.C}
	named := false
	for position, role := range roles {
		if operands[position] != reg || !operandMayNameBank(instruction, shape, role, bankRole, kind) {
			continue
		}
		if !renamableOperand(instruction, shape, position, role) {
			return false, false
		}
		if shape.Writes[position] {
			redefines = true
			continue
		}
		named = true
	}
	return named, redefines
}

// renamableOperand reports whether the operand at position, which names the register
// being renamed, either only reads it or only writes it.
//
// Takes instruction (isa.Instruction) which is the instruction.
// Takes shape (isa.OperandShape) which describes its operands.
// Takes position (int) which is the operand's position.
// Takes role (isa.OperandRole) which is the operand's role.
//
// Returns bool which is false when the use cannot be renamed.
func renamableOperand(instruction isa.Instruction, shape isa.OperandShape, position int, role isa.OperandRole) bool {
	if role == isa.RoleRegDynamic {
		if _, marked := kindMarkerOperandOf(instruction, shape); !marked {
			return false
		}
	}
	return !shape.Writes[position] || !shape.Reads[position]
}

// usesReachedOnlyFrom proves every reachable path to a use starts at the definition, so
// no other definition of the register reaches it.
//
// Takes analysis (*functionAnalysis) which supplies predecessor edges.
// Takes members (*reachedWords) which marks the words reached from the definition.
// Takes uses ([]int) which are the uses to prove.
// Takes redefiners (*reachedWords) which marks uses that rewrite the register, or nil.
// Takes defStart (int) which is the definition's first word.
// Takes defEnd (int) which is the word after the definition's last word.
//
// Returns true when every predecessor chain stays inside the reached words, and passes
// through no rewriting use, until the definition.
func usesReachedOnlyFrom(analysis *functionAnalysis, members *reachedWords, uses []int, redefiners *reachedWords, defStart, defEnd int) bool {
	analysis.buildCFG()
	pending := append([]int(nil), uses...)
	checked := reachedWords{set: nil, count: 0}
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if (pc >= defStart && pc < defEnd) || checked.has(pc) {
			continue
		}
		if pc == 0 || !members.has(pc) {
			return false
		}
		checked.add(pc)
		var ok bool
		if pending, ok = appendPredecessors(analysis, pending, pc, redefiners); !ok {
			return false
		}
	}
	return true
}

// appendPredecessors queues the reachable predecessors of pc for usesReachedOnlyFrom.
//
// Takes analysis (*functionAnalysis) which supplies predecessor edges.
// Takes pending ([]int) which receives the predecessors.
// Takes pc (int) which is the word whose predecessors are queued.
// Takes redefiners (*reachedWords) which marks the uses that rewrite the register, or
// nil.
//
// Returns []int which is pending extended.
// Returns bool which is false when a predecessor rewrites the register, so the
// definition's value does not reach pc along that edge.
func appendPredecessors(analysis *functionAnalysis, pending []int, pc int, redefiners *reachedWords) ([]int, bool) {
	for _, predecessor := range analysis.predecessors[pc] {
		if !analysis.reachable[predecessor] {
			continue
		}
		if redefiners.has(predecessor) {
			return pending, false
		}
		pending = append(pending, predecessor)
	}
	return pending, true
}
