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
// Safe when the read runs before any exit edge, or when a dominating dereference proves
// the receiver non-nil, nothing reads the destination before the read, and the
// destination is dead on every exit edge the read sits behind.
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
	if len(exits) == 0 {
		return true
	}
	if !receiverProvenNonNilAtHeader(compiledFunction, loop.header, hoistedReadReceiverRegister(body, readPC), dominators) {
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
	for _, exitPC := range exits {
		if !registerDeadFrom(compiledFunction, body, exitPC, destKind, destReg) {
			return false
		}
	}
	return true
}

// loopExitsBeforeRead lists the targets of the exit edges between the loop header and the
// read at readPC.
//
// A jump in [loop.Header, readPC) that lands outside [loop.Header, loop.Latch] is an exit
// edge and its target is recorded. A jump that stays in the loop but lands after the read
// could reach the latch without running the read, so the read is refused; one that lands
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
		inst := body[pc]
		if program.IsReturnInstruction(inst) {
			return nil, false
		}
		target, isJump := program.JumpTargetAt(body, pc)
		if !isJump {
			if !isLinearFallThroughFrom(inst) {
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
		pc += program.JumpFootprint(inst)
	}
	return exits, true
}

// receiverProvenNonNilAtHeader reports whether the general register receiver is provably
// non-nil whenever control reaches headerPC.
//
// The proof is a dominating dereference: a struct-field read or write through receiver at
// a PC that dominates the header ran without faulting on every path to the header, so the
// register held a non-nil value there. It still holds that value if every write to the
// register dominates the dereference: a write executed after the dereference would then
// have to be followed by the dereference again before the header, because a path from
// that write to the header that avoided the dereference, spliced onto the first visit of
// the write, would be a path from entry to the header that avoids it, which dominance
// rules out.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes headerPC (int) which is the loop header the receiver is used at.
// Takes receiver (uint8) which is the general register holding the receiver.
// Takes dominators (*FunctionDominators) which is the body's dominator relation.
//
// Returns true when a dominating dereference proves receiver non-nil at headerPC.
func receiverProvenNonNilAtHeader(compiledFunction *program.CompiledFunction, headerPC int, receiver uint8, dominators *functionDominators) bool {
	if dominators == nil {
		return false
	}
	body := compiledFunction.Body
	writePCs := generalRegisterWritePCs(compiledFunction, receiver)
	for pc := range headerPC {
		if !dereferencesReceiverAt(body, pc, receiver) || !dominators.Dominates(pc, headerPC) {
			continue
		}
		if everyPCDominates(dominators, writePCs, pc) {
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
// Each instruction is classified by instructionMayWriteRegister(), so calls count only
// when a call site returns into reg while undescribed and opaque writers always count.
//
// Takes compiledFunction (*CompiledFunction) whose body and call sites are scanned.
// Takes reg (uint8) which is the general register of interest.
//
// Returns the PCs in ascending order.
func generalRegisterWritePCs(compiledFunction *program.CompiledFunction, reg uint8) []int {
	var writePCs []int
	for pc, inst := range compiledFunction.Body {
		if instructionMayWriteRegister(compiledFunction, inst, isa.RegisterGeneral, reg) {
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
// Calls are classified through their call sites; other instructions through their operand
// shapes, with undescribed shapes read conservatively. A jump's extension words and
// padding carry only its offset, so its whole footprint is skipped after the jump itself
// is classified. Other extension words are attributed to the instruction that owns them:
// an int-bank layout the ISA records (isa.ExtensionIntUse()) is decoded exactly, any
// other extension word counts as a read when a byte equals reg.
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
		inst := body[pc]
		if inst.Op == isa.OpExt {
			if reads, _ := walk.extensionWordUse(ownerPC, pc); reads {
				return true
			}
			pc++
			continue
		}
		ownerPC = pc
		if reads, _ := walk.classify(inst); reads {
			return true
		}
		if layout, _ := isa.JumpLayoutOf(inst); layout != isa.JumpLayoutNone {
			pc += program.JumpFootprint(inst)
			continue
		}
		pc++
	}
	return false
}

// instructionMayWriteRegister reports whether inst may write register reg of bank kind.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes inst (isa.Instruction) which is the instruction to classify.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns true when inst may write reg.
func instructionMayWriteRegister(compiledFunction *program.CompiledFunction, inst isa.Instruction, kind isa.RegisterKind, reg uint8) bool {
	if IsCallInstruction(inst) {
		replacesFrame := isa.InstrIsTier1SubOp(inst, isa.SubOpTailCall)
		_, writes := callSiteRegisterUse(compiledFunction, inst, kind, reg, replacesFrame)
		return writes
	}
	return instructionWritesRegisterInBank(inst, isa.RoleForKind(kind), reg)
}
