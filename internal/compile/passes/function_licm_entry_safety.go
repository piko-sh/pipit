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

// mergeLoopsSharingHeader collapses the natural loops that share a header into one range
// reaching the widest latch.
//
// identifyLoops() reports one range per back-edge, so a header with two back-edges yields
// two overlapping ranges. A hoist judged against the narrower one would only check
// invariance over part of the code that re-enters the header, and the re-entry through
// the wider back-edge skips the pre-header, so the cached value could be stale. Every
// loop-invariance and entry-safety question in this pass is therefore asked of the union.
//
// Takes loops ([]LoopRange) which are the per-back-edge ranges.
//
// Returns the merged ranges, innermost first as sortLoopRangesAscendingSpan() orders
// them.
func mergeLoopsSharingHeader(loops []loopRange) []loopRange {
	if len(loops) == 0 {
		return loops
	}
	widest := make(map[int]int, len(loops))
	for _, loop := range loops {
		if latch, seen := widest[loop.header]; !seen || loop.latch > latch {
			widest[loop.header] = loop.latch
		}
	}
	merged := make([]loopRange, 0, len(widest))
	for header, latch := range widest {
		merged = append(merged, loopRange{header: header, latch: latch})
	}
	sortLoopRangesAscendingSpan(merged)
	return merged
}

// hoistSafeAcrossLoopEntry reports whether lifting the read at readPC to the pre-header
// of loop is unobservable on the entry path.
//
// Nothing in the loop before the read may read the destination. Then it is safe when a
// dominating dereference proves the receiver non-nil and the destination is dead on every
// exit edge the read sits behind. A read with no exit before it needs no such proof when
// the words between the header and the read are quiet (entryPrefixIsQuiet()), because a
// fault then happens in the same state either way.
//
// Takes compiledFunction (*CompiledFunction) whose body holds the loop.
// Takes loop (LoopRange) which is the loop the read belongs to.
// Takes readPC (int) which is the PC of the loop-invariant read.
// Takes dominators (*FunctionDominators) which is the body's dominator relation.
//
// Returns true when the hoist is safe on entry.
func hoistSafeAcrossLoopEntry(compiledFunction *program.CompiledFunction, loop loopRange, readPC int, dominators *functionDominators) bool {
	body := compiledFunction.Body
	exits, ok := loopExitsBeforeRead(body, loop, readPC)
	if !ok {
		return false
	}
	destKind, ok := isa.KindForRole(hoistedReadDestBank(body, readPC))
	if !ok {
		return false
	}
	destReg := hoistedReadDestRegister(body, readPC)
	if registerReadInRange(compiledFunction, loop.header, readPC, destKind, destReg) {
		return false
	}
	receiver := hoistedReadReceiverRegister(body, readPC)
	if len(exits) == 0 && entryPrefixIsQuiet(body, loop.header, readPC, receiver) {
		return true
	}
	if !receiverProvenNonNilAtHeader(compiledFunction, loop.header, receiver, dominators) {
		return false
	}
	for _, exitPC := range exits {
		if !registerDeadFrom(compiledFunction, body, exitPC, destKind, destReg) {
			return false
		}
	}
	return true
}

// prefixCannotFault reports whether every word in [from, to) only computes registers and
// cannot fault, so an operation that faults at to faults in the same state at from.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes from (int) which is the loop header.
// Takes to (int) which is the candidate.
//
// Returns true when no word of the prefix can fault or change memory.
func prefixCannotFault(body []isa.Instruction, from, to int) bool {
	for pc := from; pc < to; pc++ {
		if instruction := body[pc]; instruction.Op != isa.OpExtension && !isCanonicalNoOperation(instruction) && !quietRegisterOperation(body, pc) {
			return false
		}
	}
	return true
}

// entryPrefixIsQuiet reports whether every word in [from, to) can neither fault nor
// change anything but registers, so a read that faults at to faults in the same state at
// from.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes from (int) which is the loop header.
// Takes to (int) which is the candidate read.
// Takes receiver (uint8) which is the general register the read goes through.
//
// Returns true when the prefix is quiet.
func entryPrefixIsQuiet(body []isa.Instruction, from, to int, receiver uint8) bool {
	for pc := from; pc < to; pc++ {
		instruction := body[pc]
		if instruction.Op == isa.OpExtension || isCanonicalNoOperation(instruction) {
			continue
		}
		if isHoistableReadAt(body, pc) {
			if hoistedReadReceiverRegister(body, pc) != receiver {
				return false
			}
			continue
		}
		if !quietRegisterOperation(body, pc) {
			return false
		}
	}
	return true
}

// quietRegisterOperation reports whether the instruction at pc only computes a register
// from registers, constants or a global, and cannot fault.
//
// The signed shifts are among them because the engine shifts by the count as an unsigned
// number and raises nothing for a negative one.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the instruction.
//
// Returns true for such an instruction.
func quietRegisterOperation(body []isa.Instruction, pc int) bool {
	instruction := body[pc]
	if arithmeticCanFault(instruction) {
		return false
	}
	if _, _, ok := constantLoadDestination(instruction); ok {
		return true
	}
	if _, _, ok := hoistableArithmeticDestination(instruction); ok {
		return true
	}
	if _, _, _, ok := globalLoadDestination(body, pc); ok {
		return true
	}
	switch instruction.Op {
	case isa.OpLessThanInt64, isa.OpLessOrEqualInt64, isa.OpGreaterThanInt64, isa.OpGreaterOrEqualInt64,
		isa.OpEqualInt64, isa.OpNotEqualInt64, isa.OpJumpIfTrue, isa.OpJumpIfFalse,
		isa.OpShiftLeftInt64, isa.OpShiftRightInt64:
		return true
	case isa.OpDrillTier1:
		return quietTier1Operation(isa.SubOpcode(instruction.A))
	default:
		return false
	}
}

// quietTier1Operation answers quietRegisterOperation() for the tier-1 scalar moves and
// the jumps that test nothing but integer registers.
//
// Takes sub (isa.SubOpcode) which is the tier-1 operation.
//
// Returns true for such an operation.
func quietTier1Operation(sub isa.SubOpcode) bool {
	switch sub {
	case isa.SubOpMoveInt64, isa.SubOpMoveUint64, isa.SubOpMoveFloat64, isa.SubOpMoveString, isa.SubOpMoveBool, isa.SubOpJump:
		return true
	default:
	}
	for _, fusion := range compareJumpFusions {
		if sub == fusion.register || sub == fusion.constant {
			return true
		}
	}
	return false
}

// loopExitsBeforeRead lists the targets of the exit edges between the loop header and the
// read at readPC.
//
// A jump in [loop.Header, readPC) that lands outside [loop.Header, loop.Latch] is an exit
// edge and its target is recorded. A jump that stays in the loop but lands after the read
// could reach the latch without running the read, so the read is refused. One that lands
// at or before the read cannot skip it. A return, tail call or type-switch dispatch in
// the prefix leaves the frame or fans out to a table and refuses as well.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes loop (LoopRange) which is the loop being analysed.
// Takes readPC (int) which is the PC of the candidate read.
//
// Returns exits ([]int) which are the exit targets, empty when the read runs before any
// exit.
// Returns ok (bool) which is false when the prefix contains a word the analysis refuses.
func loopExitsBeforeRead(body []isa.Instruction, loop loopRange, readPC int) (exits []int, ok bool) {
	for pc := loop.header; pc < readPC; {
		instruction := body[pc]
		if program.IsReturnInstruction(instruction) {
			return nil, false
		}
		target, isJump := program.JumpTargetAt(body, pc)
		if !isJump {
			if !isLinearFallThroughFrom(instruction) {
				return nil, false
			}
			pc++
			continue
		}
		switch {
		case target < loop.header || target > loop.latch:
			exits = append(exits, target)
		case target > readPC:
			return nil, false
		}
		pc += program.JumpFootprint(instruction)
	}
	return exits, true
}

// receiverProvenNonNilAtHeader reports whether the general register receiver is provably
// non-nil whenever control reaches headerPC.
//
// The proof is a dominating dereference. A struct-field read or write through receiver at
// a PC that dominates the header ran without faulting on every path to the header, so the
// register held a non-nil value there. It still holds that value if every write to the
// register dominates the dereference.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes headerPC (int) which is the loop header the receiver is used at.
// Takes receiver (uint8) which is the general register holding the receiver.
// Takes dominators (*FunctionDominators) which is the body's dominator relation.
//
// A dereference that also writes the register, as p = p.next does, proves only that the
// old value was non-nil, and is not taken as proof.
//
// Returns true when a dominating dereference proves receiver non-nil at headerPC.
func receiverProvenNonNilAtHeader(compiledFunction *program.CompiledFunction, headerPC int, receiver uint8, dominators *functionDominators) bool {
	if receiverDereferencedOnTheWayIntoHeader(compiledFunction, headerPC, receiver) {
		return true
	}
	if dominators == nil {
		return false
	}
	body := compiledFunction.Body
	writePCs := generalRegisterWritePCs(compiledFunction, receiver)
	for pc := range headerPC {
		if !dereferencesReceiverAt(body, pc, receiver) || !dominators.Dominates(pc, headerPC) ||
			instructionMayWriteRegister(compiledFunction, body[pc], isa.RegisterGeneral, receiver) {
			continue
		}
		if everyPCDominates(dominators, writePCs, pc) {
			return true
		}
	}
	return false
}

// receiverDereferencedOnTheWayIntoHeader reports whether the words that fall straight
// into the loop header dereference the receiver and leave it alone afterwards.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes headerPC (int) which is the loop header.
// Takes receiver (uint8) which is the general register holding the receiver.
//
// Returns true when such a dereference stands ahead of the header.
func receiverDereferencedOnTheWayIntoHeader(compiledFunction *program.CompiledFunction, headerPC int, receiver uint8) bool {
	body := compiledFunction.Body
	dereference := -1
	for pc := headerPC - 1; pc >= 0 && dereference < 0; pc-- {
		instruction := body[pc]
		if instruction.Op == isa.OpExtension || isCanonicalNoOperation(instruction) {
			continue
		}
		if _, jumps := program.JumpTargetAt(body, pc); jumps || !isLinearFallThroughFrom(instruction) {
			return false
		}
		if instructionMayWriteRegister(compiledFunction, instruction, isa.RegisterGeneral, receiver) {
			return false
		}
		if dereferencesReceiverAt(body, pc, receiver) {
			dereference = pc
		}
	}
	return dereference >= 0 && !jumpBypassesDereference(body, dereference, headerPC)
}

// jumpBypassesDereference reports whether a jump can enter the loop without its preceding
// dereference.
//
// Back edges onto the header come from inside the loop and do not count.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes dereference (int) which is the program counter of the dereference.
// Takes headerPC (int) which is the loop header.
//
// Returns true when such a jump exists.
func jumpBypassesDereference(body []isa.Instruction, dereference, headerPC int) bool {
	for pc := range body {
		target, jumps := program.JumpTargetAt(body, pc)
		if !jumps || target <= dereference {
			continue
		}
		if target < headerPC || (target == headerPC && pc < headerPC) {
			return true
		}
	}
	return false
}

// dereferencesReceiverAt reports whether the instruction at pc is a struct-field read or
// write whose receiver is the general register receiver.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the PC to classify.
// Takes receiver (uint8) which is the general register of interest.
//
// Returns true when body[pc] dereferences receiver.
func dereferencesReceiverAt(body []isa.Instruction, pc int, receiver uint8) bool {
	if isHoistableReadAt(body, pc) {
		return hoistedReadReceiverRegister(body, pc) == receiver
	}
	writeReceiver, ok := structFieldWriteReceiver(body[pc])
	return ok && writeReceiver == receiver
}

// generalRegisterWritePCs lists every PC whose instruction may write the general register
// reg.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes reg (uint8) which is the general register of interest.
//
// Returns the PCs in ascending order.
func generalRegisterWritePCs(compiledFunction *program.CompiledFunction, reg uint8) []int {
	var writePCs []int
	for pc, instruction := range compiledFunction.Body {
		if instructionMayWriteRegister(compiledFunction, instruction, isa.RegisterGeneral, reg) {
			writePCs = append(writePCs, pc)
		}
	}
	return writePCs
}

// everyPCDominates reports whether each PC in pcs dominates usePC.
//
// Takes dominators (*FunctionDominators) which is the body's dominator relation.
// Takes pcs ([]int) which are the definition sites to test.
// Takes usePC (int) which is the PC they must all dominate.
//
// Returns true when pcs is empty or every entry dominates usePC.
func everyPCDominates(dominators *functionDominators, pcs []int, usePC int) bool {
	for _, pc := range pcs {
		if !dominators.Dominates(pc, usePC) {
			return false
		}
	}
	return true
}

// registerReadInRange reports whether any instruction in [from, to) may read register reg
// of bank kind.
//
// Undescribed operand shapes are read conservatively. Jump offsets and padding are
// skipped. Extension-word layouts recorded in isa.ExtensionIntUse are decoded exactly.
// Any other extension word counts as a read when a byte equals reg.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes from (int) which is the first PC of the range.
// Takes to (int) which is the PC after the last one inspected.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns true when some word in the range may read reg.
func registerReadInRange(compiledFunction *program.CompiledFunction, from, to int, kind isa.RegisterKind, reg uint8) bool {
	body := compiledFunction.Body
	walk := newRegisterLivenessWalk(compiledFunction, body, kind, reg)
	ownerPC := -1
	for pc := from; pc < to && pc < len(body); {
		instruction := body[pc]
		if instruction.Op == isa.OpExtension {
			if reads, _ := walk.extensionWordUse(ownerPC, pc); reads {
				return true
			}
			pc++
			continue
		}
		ownerPC = pc
		if reads, _ := walk.classify(instruction); reads {
			return true
		}
		if layout, _ := isa.JumpLayoutOf(instruction); layout != isa.JumpLayoutNone {
			pc += program.JumpFootprint(instruction)
			continue
		}
		pc++
	}
	return false
}

// instructionMayWriteRegister reports whether an instruction may write a register.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes instruction (isa.Instruction) which is the instruction to classify.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns true when instruction may write reg.
func instructionMayWriteRegister(compiledFunction *program.CompiledFunction, instruction isa.Instruction, kind isa.RegisterKind, reg uint8) bool {
	if IsCallInstruction(instruction) {
		replacesFrame := isa.InstrIsTier1SubOp(instruction, isa.SubOpTailCall)
		_, writes := callSiteRegisterUse(compiledFunction, instruction, kind, reg, replacesFrame)
		return writes
	}
	return instructionWritesRegisterInBank(instruction, isa.RoleForKind(kind), reg)
}
