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
	"maps"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// gvnRegisterBankCount is the number of typed register banks the GVN state tracks (int,
	// float, string, general, bool, uint, complex).
	gvnRegisterBankCount = 7

	// gvnRegisterBankSize is the per-bank register-index space, matching the VM's
	// 256-register banks.
	gvnRegisterBankSize = 256

	// gvnRegisterTrackedBanks aliases gvnRegisterBankCount for readability at iteration
	// sites.
	gvnRegisterTrackedBanks = gvnRegisterBankCount

	// gvnNoDefiningPC marks a register whose last-defining PC is unknown or has been
	// invalidated.
	gvnNoDefiningPC = -1

	// gvnFirstUnseenValue is the first name given to the value of a register read before any
	// definition of it was seen.
	gvnFirstUnseenValue = -2

	// gvnBankIndexInvalid is the sentinel returned by gvnBankForArithmetic when the opcode
	// does not classify into one of the tracked banks.
	gvnBankIndexInvalid = -1

	// noJumpEntry marks a word no jump lands on.
	noJumpEntry = -1
)

const (
	// gvnBankIndexInt is the bank discriminator for int-typed registers in gvnState's
	// regLastDef array.
	gvnBankIndexInt = iota

	// gvnBankIndexFloat is the bank discriminator for float-typed registers.
	gvnBankIndexFloat

	// gvnBankIndexString is the bank discriminator for string-typed registers.
	gvnBankIndexString

	// gvnBankIndexGeneral is the bank discriminator for general-purpose registers.
	gvnBankIndexGeneral

	// gvnBankIndexBool is the bank discriminator for bool-typed registers.
	gvnBankIndexBool

	// gvnBankIndexUint is the bank discriminator for uint-typed registers.
	gvnBankIndexUint

	// gvnBankIndexComplex is the bank discriminator for complex-typed registers.
	gvnBankIndexComplex
)

// gvnState carries the per-function GVN tables: the value-key lookup, the per-register
// last-defining PC, and the per-PC value number.
type gvnState struct {
	// valueTable maps each computed value key to the PC that produced it.
	valueTable map[gvnValueKey]int

	// perPCValueNum records the canonical value number assigned to each PC.
	perPCValueNum []int

	// rewritten lists the computations the scan replaced with a move.
	rewritten []int

	// nextUnseenValue is the name the next register read before any definition of it will be
	// given.
	nextUnseenValue int

	// regLastDef holds the last-defining PC for every tracked register slot, indexed by bank
	// then register, or the name of the value the register held when it was first read.
	regLastDef [gvnRegisterTrackedBanks][gvnRegisterBankSize]int
}

// newGvnState builds an empty GVN state sized for a function body of bodyLen
// instructions.
//
// Takes bodyLen (int) which is the instruction count used to size the value tables.
//
// Returns the freshly initialised *gvnState.
func newGvnState(bodyLen int) *gvnState {
	state := &gvnState{valueTable: make(map[gvnValueKey]int, bodyLen), nextUnseenValue: gvnFirstUnseenValue, rewritten: nil,
		perPCValueNum: make([]int, bodyLen), regLastDef: [gvnRegisterTrackedBanks][gvnRegisterBankSize]int{}}
	for bank := range gvnRegisterTrackedBanks {
		for reg := range gvnRegisterBankSize {
			state.regLastDef[bank][reg] = gvnNoDefiningPC
		}
	}
	for pc := range bodyLen {
		state.perPCValueNum[pc] = gvnNoDefiningPC
	}
	return state
}

// updateRegLastDef refreshes the regLastDef table for the destination of inst.
//
// Takes inst (instruction) which is the instruction whose destination effects update the
// table.
// Takes pc (int) which is the program counter assigned as the new last-defining PC.
func (state *gvnState) updateRegLastDef(inst isa.Instruction, pc int) {
	if state.followMove(inst, pc) {
		return
	}
	if bank, reg, ok := gvnInstructionDest(inst); ok {
		state.regLastDef[bank][reg] = pc
		if pc >= 0 && pc < len(state.perPCValueNum) {
			state.perPCValueNum[pc] = pc
		}
		return
	}
	if !instructionWritesNoOperand(inst) {
		state.forgetWrittenRegisters(inst)
	}
}

// forgetWrittenRegisters drops the last definition of every register an instruction the
// pass does not model may write.
//
// Takes inst (isa.Instruction) which is the instruction.
func (state *gvnState) forgetWrittenRegisters(inst isa.Instruction) {
	for bank := range gvnRegisterTrackedBanks {
		role, ok := gvnBankToRole(bank)
		if !ok {
			continue
		}
		writes := registersWrittenInBank(inst, role)
		if writes.any {
			for reg := range gvnRegisterBankSize {
				state.regLastDef[bank][reg] = gvnNoDefiningPC
			}
			continue
		}
		for _, reg := range writes.registers() {
			state.regLastDef[bank][reg] = gvnNoDefiningPC
		}
	}
}

// followMove gives the destination of a register move the value number of its source.
//
// Takes inst (isa.Instruction) which is the instruction at pc.
// Takes pc (int) which becomes the destination's last-defining PC.
//
// Returns bool which is false when the instruction is not such a move.
func (state *gvnState) followMove(inst isa.Instruction, pc int) bool {
	bank, destination, source, ok := gvnMoveWithinBank(inst)
	if !ok {
		return false
	}
	copied, known := gvnLookupValueNumber(state, source, bank)
	if !known {
		return false
	}
	state.regLastDef[bank][destination] = pc
	state.perPCValueNum[pc] = copied
	return true
}

// jumpEntries records, for each word a jump lands on, the lowest and the highest word
// that jumps there.
type jumpEntries struct {
	// lowest holds, per word, the lowest jumping word, or noJumpEntry.
	lowest []int

	// highest holds, per word, the highest jumping word, or noJumpEntry.
	highest []int
}

// forgetWhatAJumpInBypasses drops, on reaching a word a jump lands on, every earlier
// value the scan can no longer vouch for.
//
// A jump from a later word drops every value. A forward jump drops values and register
// definitions after its lowest source. Earlier definitions and registers only read so far
// are kept.
//
// Takes entries (jumpEntries) which records the jump sources of each word.
// Takes pc (int) which is the word the scan has reached.
func (state *gvnState) forgetWhatAJumpInBypasses(entries jumpEntries, pc int) {
	if entries.lowest[pc] == noJumpEntry {
		return
	}
	if entries.highest[pc] > pc {
		state.forgetEverything()
		return
	}
	lowest := entries.lowest[pc]
	maps.DeleteFunc(state.valueTable, func(_ gvnValueKey, candidatePC int) bool { return candidatePC > lowest })
	for bank := range gvnRegisterTrackedBanks {
		for reg := range gvnRegisterBankSize {
			if state.regLastDef[bank][reg] > lowest {
				state.regLastDef[bank][reg] = gvnNoDefiningPC
			}
		}
	}
}

// forgetEverything wipes every value-table entry and resets every register's
// last-defining PC.
func (state *gvnState) forgetEverything() {
	clear(state.valueTable)
	for bank := range gvnRegisterTrackedBanks {
		for reg := range gvnRegisterBankSize {
			state.regLastDef[bank][reg] = gvnNoDefiningPC
		}
	}
}

// recordRewrite updates the per-PC value number to point at the candidate so subsequent
// ops that read this PC's destination see it as the same value as candidatePC.
//
// Takes inst (instruction) which is the rewritten instruction whose destination register
// is being tracked.
// Takes pc (int) which is the PC of the rewritten instruction.
// Takes candidatePC (int) which is the PC whose value number now applies to pc.
func (state *gvnState) recordRewrite(inst isa.Instruction, pc, candidatePC int) {
	bankIndex, reg, ok := gvnInstructionDest(inst)
	if !ok {
		return
	}
	state.regLastDef[bankIndex][reg] = pc
	if pc >= 0 && pc < len(state.perPCValueNum) {
		state.perPCValueNum[pc] = candidatePC
	}
}

// gvnValueKey is the hash-table key uniquely naming the computation at one PC. Two PCs
// hash to the same key iff their opcodes match and their operands' value-numbers match
// (after commutative canonicalisation).
type gvnValueKey struct {
	// op identifies the opcode that produces the value.
	op isa.Opcode

	// operand0 is the canonical value number of the first operand.
	operand0 int

	// operand1 is the canonical value number of the second operand.
	operand1 int
}

// instructionWritesNoOperand reports whether an instruction writes no register.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns bool which is false for a shape that is not described or has writes it does not
// describe.
func instructionWritesNoOperand(inst isa.Instruction) bool {
	shape := isa.ShapeForInstruction(inst)
	return shape.Flags&isa.ShapeFlagDescribed != 0 && shape.Flags&isa.ShapeFlagOpaqueWrites == 0 &&
		shape.Writes == ([isa.NumInstructionOperands]bool{})
}

// jumpEntriesOf builds the jump-entry record of a body.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns jumpEntries which holds the lowest and highest jump source of each target.
func jumpEntriesOf(body []isa.Instruction) jumpEntries {
	entries := jumpEntries{lowest: make([]int, len(body)), highest: make([]int, len(body))}
	for pc := range body {
		entries.lowest[pc], entries.highest[pc] = noJumpEntry, noJumpEntry
	}
	for _, edge := range controlFlowEdgesOf(body).jumps {
		if entries.lowest[edge.to] == noJumpEntry || edge.from < entries.lowest[edge.to] {
			entries.lowest[edge.to] = edge.from
		}
		entries.highest[edge.to] = max(entries.highest[edge.to], edge.from)
	}
	return entries
}

// RunFunctionGvn rewrites later equal-value computations as MOVE from an earlier
// dominator-validated definition, then removes each such move whose reads can take the
// earlier register instead.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to optimise.
//
// Returns error when context cancellation fires.
func RunFunctionGvn(ctx context.Context, compiledFunction *program.CompiledFunction) error {
	return runFunctionGvn(ctx, compiledFunction, nil)
}

// runFunctionGvn is RunFunctionGvn with an optional shared analysis supplying the
// dominator table and the jump-target set. Nil computes both privately.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes analysis (*FunctionAnalysis) which is the pipeline's shared analysis, or nil.
//
// Returns error when context cancellation fires.
func runFunctionGvn(ctx context.Context, compiledFunction *program.CompiledFunction, analysis *functionAnalysis) error {
	body := compiledFunction.Body
	if len(body) < 2 {
		return nil
	}
	const maxBodyForGvn = 2000
	if len(body) > maxBodyForGvn {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("runFunctionGvn cancelled: %w", err)
	}
	dom := dominatorsFor(analysis, body)
	if dom == nil {
		return nil
	}
	state := newGvnState(len(body))
	entries := jumpEntriesOf(body)
	for pc := range body {
		if pc&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("runFunctionGvn cancelled: %w", err)
			}
		}
		state.forgetWhatAJumpInBypasses(entries, pc)
		applyGvnAtPC(compiledFunction, body, pc, dom, state)
	}
	if len(state.rewritten) > 0 && analysis == nil {
		analysis = newFunctionAnalysis(compiledFunction)
	}
	for _, pc := range state.rewritten {
		propagateGvnCopy(compiledFunction, analysis, pc)
	}
	eliminateMovesAcrossDominatedJumps(compiledFunction, body, dom, jumpTargetsFor(analysis, body))
	return nil
}

// applyGvnAtPC runs the GVN per-instruction logic at pc against state, either clearing on
// calls, rewriting redundant computations, or updating the regLastDef table for unhandled
// writes.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function body being analysed.
// Takes pc (int) which is the current program counter.
// Takes dom (*FunctionDominators) which is the dominator information used to decide
// whether a rewrite is sound.
// Takes state (*gvnState) which tracks per-PC value numbers and the regLastDef table.
func applyGvnAtPC(compiledFunction *program.CompiledFunction, body []isa.Instruction, pc int, dom *functionDominators, state *gvnState) {
	inst := body[pc]
	if IsCallInstruction(inst) {
		state.forgetEverything()
		return
	}
	key, ok := computeGvnValueKey(inst, state)
	if ok {
		candidatePC, found := state.valueTable[key]
		if found && gvnRewriteIfSafe(compiledFunction, body, pc, candidatePC, dom, state) {
			state.recordRewrite(inst, pc, candidatePC)
			state.rewritten = append(state.rewritten, pc)
			return
		}
		state.valueTable[key] = pc
	}
	state.updateRegLastDef(inst, pc)
}

// gvnRewriteIfSafe attempts to rewrite the instruction at pc as a MOVE from the
// candidate's destination register.
//
// Safety checks include the dominator condition (candidatePC must dominate pc) and a
// liveness check (the candidate's destination register must still hold the candidate's
// value at pc, meaning the last-write-PC equals candidatePC).
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function body whose instruction at pc may be
// rewritten.
// Takes pc (int) which is the program counter of the redundant computation.
// Takes candidatePC (int) which is the earlier PC whose value can be reused.
// Takes dom (*FunctionDominators) which is the dominator information used for the
// dominance check.
// Takes state (*gvnState) which is the per-function GVN state holding register liveness.
//
// Returns true when the rewrite was applied; false otherwise.
func gvnRewriteIfSafe(compiledFunction *program.CompiledFunction, body []isa.Instruction, pc, candidatePC int, dom *functionDominators, state *gvnState) bool {
	if !dom.Dominates(candidatePC, pc) {
		return false
	}
	candidateInst := body[candidatePC]
	bankIndex, candidateDest, ok := gvnInstructionDest(candidateInst)
	if !ok {
		return false
	}
	if state.regLastDef[bankIndex][candidateDest] != candidatePC {
		return false
	}
	currentInst := body[pc]
	_, currentDest, ok := gvnInstructionDest(currentInst)
	if !ok {
		return false
	}
	moveInst, ok := gvnEmitMoveForBank(bankIndex, currentDest, candidateDest)
	if !ok {
		return false
	}
	body[pc] = moveInst
	RecordPeepholeRewrite(compiledFunction, pc, peepholeRewriteGvn, candidatePC)
	return true
}

// gvnBankToRole returns the isa.OperandRole that classifies a register in the given
// GVN-bank index.
//
// Used by updateRegLastDef to drive instructionWritesRegisterInBank for kill-on-write.
//
// Takes bank (int) which is the GVN bank index whose role mapping is required.
//
// Returns the isa.OperandRole corresponding to bank, and true when bank maps to a tracked
// role; false otherwise.
func gvnBankToRole(bank int) (isa.OperandRole, bool) {
	switch bank {
	case gvnBankIndexInt:
		return isa.RoleRegInt, true
	case gvnBankIndexFloat:
		return isa.RoleRegFloat, true
	case gvnBankIndexString:
		return isa.RoleRegString, true
	case gvnBankIndexGeneral:
		return isa.RoleRegGeneral, true
	case gvnBankIndexBool:
		return isa.RoleRegBool, true
	case gvnBankIndexUint:
		return isa.RoleRegUint, true
	case gvnBankIndexComplex:
		return isa.RoleRegComplex, true
	}
	return isa.RoleNone, false
}

// computeGvnValueKey returns the value-key for inst when inst is a pure value-producing
// opcode whose result depends only on the observed values of its operand registers.
//
// Takes inst (instruction) which is the instruction whose value key is required.
// Takes state (*gvnState) which is the per-function GVN state used to resolve operand
// value numbers.
//
// Returns the computed gvnValueKey when inst is modelled precisely, and a boolean true
// when a key was produced. It returns false for opcodes outside the model.
func computeGvnValueKey(inst isa.Instruction, state *gvnState) (gvnValueKey, bool) {
	switch inst.Op {
	case isa.OpAddInt64, isa.OpMultiplyInt64, isa.OpAddUint64, isa.OpMultiplyUint64, isa.OpAddFloat64, isa.OpMultiplyFloat64:
		left, leftOK := gvnLookupValueNumber(state, inst.B, gvnBankForArithmetic(inst.Op))
		right, rightOK := gvnLookupValueNumber(state, inst.C, gvnBankForArithmetic(inst.Op))
		if !leftOK || !rightOK {
			return gvnValueKey{}, false
		}
		if left > right {
			left, right = right, left
		}
		return gvnValueKey{op: inst.Op, operand0: left, operand1: right}, true
	case isa.OpSubtractInt64, isa.OpSubtractUint64, isa.OpSubtractFloat64, isa.OpDivideInt64, isa.OpDivideUint64, isa.OpDivideFloat64:
		left, leftOK := gvnLookupValueNumber(state, inst.B, gvnBankForArithmetic(inst.Op))
		right, rightOK := gvnLookupValueNumber(state, inst.C, gvnBankForArithmetic(inst.Op))
		if !leftOK || !rightOK {
			return gvnValueKey{}, false
		}
		return gvnValueKey{op: inst.Op, operand0: left, operand1: right}, true
	case isa.OpAddInt64Constant, isa.OpSubtractInt64Constant, isa.OpMultiplyInt64Constant:
		source, ok := gvnLookupValueNumber(state, inst.B, gvnBankIndexInt)
		if !ok {
			return gvnValueKey{}, false
		}
		return gvnValueKey{op: inst.Op, operand0: source, operand1: int(inst.C)}, true
	default:
	}
	return gvnValueKey{}, false
}

// gvnLookupValueNumber returns the canonical value number of register reg in bank.
//
// Takes state (*gvnState) which holds the register tables.
// Takes reg (uint8) which is the register slot.
// Takes bank (int) which is the bank index.
//
// Returns int which is the canonical value number.
// Returns bool which is false when the bank is not tracked.
func gvnLookupValueNumber(state *gvnState, reg uint8, bank int) (int, bool) {
	if bank < 0 || bank >= gvnRegisterTrackedBanks {
		return 0, false
	}
	def := state.regLastDef[bank][reg]
	if def == gvnNoDefiningPC {
		def = state.nextUnseenValue
		state.nextUnseenValue--
		state.regLastDef[bank][reg] = def
	}
	if def < 0 || def >= len(state.perPCValueNum) {
		return def, true
	}
	if state.perPCValueNum[def] != gvnNoDefiningPC {
		return state.perPCValueNum[def], true
	}
	return def, true
}

// gvnBankForArithmetic returns the int-encoded bank index for the arithmetic opcode's
// operand registers.
//
// Takes op (opcode) which is the arithmetic opcode whose operand bank is required.
//
// Returns the bank index, or gvnBankIndexInvalid when op is not arithmetic.
func gvnBankForArithmetic(op isa.Opcode) int {
	switch op {
	case isa.OpAddInt64, isa.OpSubtractInt64, isa.OpMultiplyInt64, isa.OpDivideInt64:
		return gvnBankIndexInt
	case isa.OpAddUint64, isa.OpSubtractUint64, isa.OpMultiplyUint64, isa.OpDivideUint64:
		return gvnBankIndexUint
	case isa.OpAddFloat64, isa.OpSubtractFloat64, isa.OpMultiplyFloat64, isa.OpDivideFloat64:
		return gvnBankIndexFloat
	default:
	}
	return gvnBankIndexInvalid
}

// gvnInstructionDest returns the destination bank index and register of inst, or zero
// values when inst does not write a register in a tracked bank. Opcodes outside this set
// are handled by updateRegLastDef which clears their regLastDef entries.
//
// Takes inst (instruction) which is the instruction whose destination is being
// classified.
//
// Returns the destination bank index, the destination register slot and a boolean true
// when inst writes a tracked register; false otherwise.
func gvnInstructionDest(inst isa.Instruction) (int, uint8, bool) {
	switch inst.Op {
	case isa.OpAddInt64, isa.OpSubtractInt64, isa.OpMultiplyInt64, isa.OpDivideInt64, isa.OpRemainderInt64,
		isa.OpAddInt64Constant, isa.OpSubtractInt64Constant, isa.OpMultiplyInt64Constant,
		isa.OpLoadInt64Constant:
		return gvnBankIndexInt, inst.A, true
	case isa.OpAddUint64, isa.OpSubtractUint64, isa.OpMultiplyUint64, isa.OpDivideUint64, isa.OpRemainderUint64,
		isa.OpLoadUint64Constant:
		return gvnBankIndexUint, inst.A, true
	case isa.OpAddFloat64, isa.OpSubtractFloat64, isa.OpMultiplyFloat64, isa.OpDivideFloat64,
		isa.OpLoadFloat64Constant:
		return gvnBankIndexFloat, inst.A, true
	default:
	}
	return 0, 0, false
}

// gvnMoveWithinBank recognises a register move the pass can follow.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns bank (int) which is the bank both registers are in.
// Returns destination (uint8) which is the register written.
// Returns source (uint8) which is the register read.
// Returns ok (bool) which is true for a move of an int, uint or float register.
func gvnMoveWithinBank(inst isa.Instruction) (bank int, destination, source uint8, ok bool) {
	if inst.Op != isa.OpDrillTier1 {
		return 0, 0, 0, false
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpMoveInt64:
		return gvnBankIndexInt, inst.B, inst.C, true
	case isa.SubOpMoveUint64:
		return gvnBankIndexUint, inst.B, inst.C, true
	case isa.SubOpMoveFloat64:
		return gvnBankIndexFloat, inst.B, inst.C, true
	default:
		return 0, 0, 0, false
	}
}

// gvnEmitMoveForBank returns the same-bank move instruction for the given GVN bank. The
// general bank is deliberately unsupported because the correct MoveGeneral mode depends
// on the replaced opcode.
//
// Takes bank (int) which is the GVN bank index.
// Takes destination (uint8) which is the destination register slot.
// Takes source (uint8) which is the source register slot.
//
// Returns the encoded MOVE instruction and true, or a zero instruction and false when the
// bank has no supported MOVE encoding.
func gvnEmitMoveForBank(bank int, destination, source uint8) (isa.Instruction, bool) {
	switch bank {
	case gvnBankIndexInt:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), destination, source), true
	case gvnBankIndexUint:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveUint64), destination, source), true
	case gvnBankIndexFloat:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveFloat64), destination, source), true
	}
	return isa.Instruction{}, false
}
