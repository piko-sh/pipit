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

// conditionLivenessBudget bounds the words one liveness query inspects across every path
// it follows, so an adversarial body costs at most this many steps per candidate fusion.
const conditionLivenessBudget = 4096

const (
	// walkContinue means the word neither read nor wrote the register; the walk moves on.
	walkContinue walkVerdict = iota

	// walkDead means the register was written or the frame ended: dead on this path.
	walkDead

	// walkLive means the register may have been read: live, and the search is refuted.
	walkLive

	// walkBranch means the word is a jump whose edges branch() must follow.
	walkBranch
)

// registerLivenessWalk is a depth-first search for a read of one register that no prior
// write precedes.
type registerLivenessWalk struct {
	// entered tracks branch targets already visited so cycles terminate.
	entered map[int]bool

	// compiledFunction is the function whose body is being walked.
	compiledFunction *program.CompiledFunction

	// body is the instruction stream being scanned.
	body []isa.Instruction

	// budget is the remaining instruction inspection allowance.
	budget int

	// kind is the register bank of the target register.
	kind isa.RegisterKind

	// role is the operand role corresponding to the bank.
	role isa.OperandRole

	// reg is the register slot being tracked.
	reg uint8
}

// conditionRegisterDeadAfterBranch reports whether ints[reg] is provably dead on both
// edges of the branch at branchPC.
//
// The compare-and-branch fusions stop writing the condition register, so they are sound
// only when nothing reads it afterwards. The compiler reclaims that temporary as soon as
// the branch is emitted, which makes the write dead by construction in code it emits, but
// the fusers run after inlining and register remapping and nothing else re-checks the
// invariant; this does. A register that is one of the function's int result slots is read
// by every return, so it is never dead.
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds say what
// calls and returns read.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes branchPC (int) which is the branch whose two edges are searched.
// Takes reg (uint8) which is the int register the fusion would stop writing.
//
// Returns true when every path out of the branch writes reg, or ends the frame, before
// any read of it.
func conditionRegisterDeadAfterBranch(compiledFunction *program.CompiledFunction, body []isa.Instruction, branchPC int, reg uint8) bool {
	if int(reg) < returnSlotCount(compiledFunction, isa.RegisterInt) {
		return false
	}
	target, ok := program.JumpTargetAt(body, branchPC)
	if !ok {
		return false
	}
	walk := newRegisterLivenessWalk(compiledFunction, body, isa.RegisterInt, reg)
	return walk.deadFrom(branchPC+program.JumpFootprint(body[branchPC])) && walk.deadFrom(target)
}

// registerDeadFrom reports whether the register reg of bank kind is provably dead at
// startPC.
//
// When the function defers a call, a result slot is never dead because a recovered panic
// returns the slot's current contents.
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds say what
// calls and returns read.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes startPC (int) which is the first program counter to inspect.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns true when reg is provably dead from startPC.
func registerDeadFrom(compiledFunction *program.CompiledFunction, body []isa.Instruction, startPC int, kind isa.RegisterKind, reg uint8) bool {
	if resultSlotReadByRecover(compiledFunction, kind, reg) {
		return false
	}
	return newRegisterLivenessWalk(compiledFunction, body, kind, reg).deadFrom(startPC)
}

// resultSlotReadByRecover reports whether reg is a result slot in a function that defers
// a call, making the slot implicitly live everywhere.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds and body are inspected.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns true when reg is a result slot and the body contains a defer.
func resultSlotReadByRecover(compiledFunction *program.CompiledFunction, kind isa.RegisterKind, reg uint8) bool {
	if int(reg) >= returnSlotCount(compiledFunction, kind) {
		return false
	}
	for _, inst := range compiledFunction.Body {
		if inst.Op == isa.OpDefer {
			return true
		}
	}
	return false
}

// newRegisterLivenessWalk builds a walk for one register of one bank.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register index within that bank.
//
// Returns the walk, ready for deadFrom().
func newRegisterLivenessWalk(compiledFunction *program.CompiledFunction, body []isa.Instruction, kind isa.RegisterKind, reg uint8) *registerLivenessWalk {
	return &registerLivenessWalk{
		compiledFunction: compiledFunction,
		body:             body,
		entered:          make(map[int]bool),
		budget:           conditionLivenessBudget,
		kind:             kind,
		role:             isa.RoleForKind(kind),
		reg:              reg,
	}
}

// deadFrom searches forward from start and reports whether the register is written, or
// the frame ends, on every path before any read of it.
//
// Takes start (int) which is the first program counter to inspect.
//
// Returns true when the register is provably dead from start.
func (w *registerLivenessWalk) deadFrom(start int) bool {
	if w.entered[start] {
		return true
	}
	w.entered[start] = true
	ownerPC := -1
	for pc := start; pc < len(w.body); {
		if w.budget <= 0 {
			return false
		}
		w.budget--
		verdict, next := w.step(pc, &ownerPC)
		switch verdict {
		case walkDead:
			return true
		case walkLive:
			return false
		case walkBranch:
			return w.branch(pc)
		default:
			pc = next
		}
	}
	return true
}

// walkVerdict is what one step of the liveness walk concluded about the word it read.
type walkVerdict uint8

// step classifies the word at pc: an extension word against the instruction that owns it,
// a return as a read of the function's result slots and otherwise the end of the frame,
// and any other instruction by its call site or operand shape. A jump is left to
// branch().
//
// Takes pc (int) which is the word's program counter.
// Takes ownerPC (*int) which tracks the last instruction seen, so extension words are
// attributed; it is updated when the word is an instruction.
//
// Returns the verdict and, for walkContinue, the next program counter.
func (w *registerLivenessWalk) step(pc int, ownerPC *int) (walkVerdict, int) {
	instr := w.body[pc]
	if instr.Op == isa.OpExt {
		return verdictFor(w.extensionWordUse(*ownerPC, pc)), pc + 1
	}
	if program.IsReturnInstruction(instr) {
		if w.compiledFunction == nil || int(w.reg) < returnSlotCount(w.compiledFunction, w.kind) {
			return walkLive, 0
		}
		return walkDead, 0
	}
	reads, writes := w.classify(instr)
	if reads || writes {
		return verdictFor(reads, writes), 0
	}
	*ownerPC = pc
	if layout, _ := isa.JumpLayoutOf(instr); layout != isa.JumpLayoutNone {
		return walkBranch, 0
	}
	return walkContinue, pc + 1
}

// verdictFor turns a read/write classification into a verdict: a possible read refutes, a
// write proves deadness, and neither continues the walk.
//
// Takes reads (bool) which is true when the word may read the register.
// Takes writes (bool) which is true when the word provably writes it.
//
// Returns the verdict.
func verdictFor(reads, writes bool) walkVerdict {
	switch {
	case reads:
		return walkLive
	case writes:
		return walkDead
	default:
		return walkContinue
	}
}

// branch follows the jump at pc: an unconditional jump continues at its target, a
// conditional branch needs both edges dead, and a multi-way dispatch or an undecodable
// target refutes the search.
//
// Takes pc (int) which is the jump's program counter.
//
// Returns true when the register is dead along every edge out of the jump.
func (w *registerLivenessWalk) branch(pc int) bool {
	instr := w.body[pc]
	if layout, _ := isa.JumpLayoutOf(instr); layout == isa.JumpLayoutMultiWay {
		return false
	}
	target, ok := program.JumpTargetAt(w.body, pc)
	if !ok {
		return false
	}
	if isa.InstrIsTier1SubOp(instr, isa.SubOpJump) {
		return w.deadFrom(target)
	}
	return w.deadFrom(pc+program.JumpFootprint(instr)) && w.deadFrom(target)
}

// extensionWordUse reports whether the extension word at pc may read the register, or
// provably writes it, as part of the instruction at ownerPC.
//
// An unknown owner or unrecorded layout is read conservatively. Only the int bank has
// recorded layouts via isa.ExtensionIntUse(); other banks are always conservative.
//
// Takes ownerPC (int) which is the program counter of the owning instruction, or -1.
// Takes pc (int) which is the extension word's program counter.
//
// Returns reads (bool) which is true when the word may read reg.
// Returns writes (bool) which is true when the word names reg as a written register.
func (w *registerLivenessWalk) extensionWordUse(ownerPC, pc int) (reads, writes bool) {
	word := w.body[pc]
	anyByte := word.A == w.reg || word.B == w.reg || word.C == w.reg
	if ownerPC < 0 || w.kind != isa.RegisterInt {
		return anyByte, false
	}
	readMask, writeMask, ok := isa.ExtensionIntUse(w.body[ownerPC], w.body[ownerPC+1:pc+1])
	if !ok {
		return anyByte, false
	}
	return w.maskNamesReg(readMask, word), w.maskNamesReg(writeMask, word)
}

// maskNamesReg reports whether any byte of word that mask marks equals reg.
//
// Takes mask (isa.ExtensionIntMask) which marks the int-register bytes.
// Takes word (isa.Instruction) which is the extension word.
//
// Returns true when a marked byte equals reg.
func (w *registerLivenessWalk) maskNamesReg(mask isa.ExtensionIntMask, word isa.Instruction) bool {
	return (mask[0] && word.A == w.reg) || (mask[1] && word.B == w.reg) || (mask[2] && word.C == w.reg)
}

// classify reports whether instr may read, or provably writes, the register.
//
// Takes instr (isa.Instruction) which is the instruction to classify.
//
// Returns reads (bool) which is true when instr may read reg.
// Returns writes (bool) which is true when instr provably writes reg.
func (w *registerLivenessWalk) classify(instr isa.Instruction) (reads, writes bool) {
	if instr.Op == isa.OpDrillTier1 {
		if reads, writes, ok := w.classifyCall(instr); ok {
			return reads, writes
		}
	}
	return instructionRegisterUse(instr, w.kind, w.role, w.reg)
}

// instructionRegisterUse reports whether a non-call instruction may read, or provably
// writes, register reg of bank kind, according to its operand shape.
//
// An undescribed operation is reported as a read when any operand byte equals reg, and
// never as a write. A described operation is read and written through the operands whose
// role can name the bank (operandMayNameBank()).
//
// Takes instr (isa.Instruction) which is the instruction to classify.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes role (isa.OperandRole) which is the operand role that names that bank exactly.
// Takes reg (uint8) which is the register index within the bank.
//
// Returns reads (bool) which is true when instr may read reg.
// Returns writes (bool) which is true when instr provably writes reg.
func instructionRegisterUse(instr isa.Instruction, kind isa.RegisterKind, role isa.OperandRole, reg uint8) (reads, writes bool) {
	shape := isa.ShapeForInstruction(instr)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return instr.A == reg || instr.B == reg || instr.C == reg, false
	}
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
	for position, operandRole := range roles {
		if operands[position] != reg || !operandMayNameBank(instr, shape, operandRole, role, kind) {
			continue
		}
		reads = reads || shape.Reads[position]
		writes = writes || shape.Writes[position]
	}
	return reads, writes
}

// classifyCall answers classify() for the tier-1 call family that names its registers
// through a call site.
//
// Takes instr (isa.Instruction) which is the tier-1 instruction.
//
// Returns reads (bool) which is true when the call reads reg.
// Returns writes (bool) which is true when the call writes reg.
// Returns ok (bool) which is false when instr is not such a call.
func (w *registerLivenessWalk) classifyCall(instr isa.Instruction) (reads, writes, ok bool) {
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpCallMethod, isa.SubOpCallMethodInlineable,
		isa.SubOpCallNative, isa.SubOpCallIIFE:
		reads, writes = callSiteRegisterUse(w.compiledFunction, instr, w.kind, w.reg, false)
		return reads, writes, true
	case isa.SubOpTailCall:
		reads, writes = callSiteRegisterUse(w.compiledFunction, instr, w.kind, w.reg, true)
		return reads, writes, true
	default:
		return false, false, false
	}
}

// callSiteRegisterUse reports whether the call at instr passes register reg of bank kind
// as an argument or receives a result into it.
//
// A site that cannot be resolved is reported as a read, so an unreadable site never
// proves a register dead. A tail call replaces the frame, so when it does not read the
// register the register is dead. Only a plain register result counts as a write; a
// spilled, indirect or upvalue result lands somewhere else.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes instr (isa.Instruction) which is the call instruction.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register of interest.
// Takes replacesFrame (bool) which is true for a tail call.
//
// Returns reads (bool) which is true when an argument of that bank is reg.
// Returns writes (bool) which is true when a result of that bank lands in reg, or the
// call replaces the frame without reading reg.
func callSiteRegisterUse(compiledFunction *program.CompiledFunction, instr isa.Instruction, kind isa.RegisterKind, reg uint8, replacesFrame bool) (reads, writes bool) {
	siteIndex := int(instr.WideIndex())
	if compiledFunction == nil || siteIndex >= len(compiledFunction.CallSites) {
		return true, false
	}
	site := &compiledFunction.CallSites[siteIndex]
	for _, argument := range site.Arguments {
		if argument.Kind == kind && argument.Register == reg {
			return true, false
		}
	}
	if replacesFrame {
		return false, true
	}
	for _, result := range site.Returns {
		if result.Kind != kind || result.Register != reg {
			continue
		}
		if result.IsSpilled || result.IsIndirect || result.IsUpvalue {
			continue
		}
		return false, true
	}
	return false, false
}

// operandMayNameBank reports whether an operand with the given role can address the bank:
// the bank's own role always can, a dynamic role can when its kind-marker sibling names
// the bank or is absent, and every other role never can.
//
// Takes instr (isa.Instruction) which carries the kind-marker byte when the shape has
// one.
// Takes shape (isa.OperandShape) which is the instruction's operand shape.
// Takes role (isa.OperandRole) which is the operand's role.
// Takes bankRole (isa.OperandRole) which is the role that names the bank exactly.
// Takes kind (isa.RegisterKind) which is the bank a kind marker would name.
//
// Returns true when the operand may name a register of the bank.
func operandMayNameBank(instr isa.Instruction, shape isa.OperandShape, role, bankRole isa.OperandRole, kind isa.RegisterKind) bool {
	switch role {
	case bankRole:
		return true
	case isa.RoleRegDynamic:
		markerKind, ok := kindMarkerOperandOf(instr, shape)
		return !ok || markerKind == kind
	default:
		return false
	}
}
