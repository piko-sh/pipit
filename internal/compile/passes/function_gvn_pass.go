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

	// gvnBankIndexInvalid is the sentinel returned by gvnBankForArithmetic when the opcode
	// does not classify into one of the tracked banks.
	gvnBankIndexInvalid = -1
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

	// regLastDef holds the last-defining PC for every tracked register slot, indexed by bank
	// then register.
	regLastDef [gvnRegisterTrackedBanks][gvnRegisterBankSize]int
}

// newGvnState builds an empty GVN state sized for a function body of bodyLen
// instructions.
//
// Every regLastDef and perPCValueNum entry is seeded to gvnNoDefiningPC so the first read
// of any register before any write returns "no value number".
//
// Takes bodyLen (int) which is the instruction count used to size the value tables.
//
// Returns the freshly initialised *gvnState.
func newGvnState(bodyLen int) *gvnState {
	state := &gvnState{valueTable: make(map[gvnValueKey]int, bodyLen),
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
// When gvnInstructionDest reports a tracked-bank destination, the dest's last-defining PC
// becomes this PC. For other ops, every tracked register that
// instructionWritesRegisterInBank reports as written has its last-defining PC cleared to
// gvnNoDefiningPC, preventing stale value numbers from leaking past spills, reloads, or
// unmodelled writes.
//
// Takes inst (instruction) which is the instruction whose destination effects update the
// table.
// Takes pc (int) which is the program counter assigned as the new last-defining PC.
func (state *gvnState) updateRegLastDef(inst isa.Instruction, pc int) {
	if bank, reg, ok := gvnInstructionDest(inst); ok {
		state.regLastDef[bank][reg] = pc
		if pc >= 0 && pc < len(state.perPCValueNum) {
			state.perPCValueNum[pc] = pc
		}
		return
	}
	for bank := range gvnRegisterTrackedBanks {
		role, ok := gvnBankToRole(bank)
		if !ok {
			continue
		}
		for reg := range gvnRegisterBankSize {
			if state.regLastDef[bank][reg] == gvnNoDefiningPC {
				continue
			}
			if instructionWritesRegisterInBank(inst, role, uint8(reg)) {
				state.regLastDef[bank][reg] = gvnNoDefiningPC
			}
		}
	}
}

// clearOnCall wipes every value-table entry and resets every register's last-defining PC.
// Called when the analysis encounters any call opcode, mirroring the alias analysis's
// "calls go wild" rule.
func (state *gvnState) clearOnCall(_ int) {
	for k := range state.valueTable {
		delete(state.valueTable, k)
	}
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

// RunFunctionGvn rewrites later equal-value computations as MOVE from an earlier
// dominator-validated definition. Skips functions over 2000 instructions.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to optimise.
//
// Returns error when context cancellation fires.
func RunFunctionGvn(ctx context.Context, compiledFunction *program.CompiledFunction) error {
	return runFunctionGvn(ctx, compiledFunction, nil)
}

// runFunctionGvn is RunFunctionGvn with an optional shared analysis supplying the
// dominator table and the jump-target set; nil computes both privately.
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
	for pc := range body {
		if pc&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("runFunctionGvn cancelled: %w", err)
			}
		}
		applyGvnAtPC(compiledFunction, body, pc, dom, state)
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
		state.clearOnCall(pc)
		return
	}
	key, ok := computeGvnValueKey(inst, state)
	if ok {
		candidatePC, found := state.valueTable[key]
		if found && gvnRewriteIfSafe(compiledFunction, body, pc, candidatePC, dom, state) {
			state.recordRewrite(inst, pc, candidatePC)
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
// Commutative ops (isa.OpAddInt, isa.OpMulInt, isa.OpAddUint, isa.OpMulUint,
// isa.OpAddFloat, isa.OpMulFloat) sort their operand value-numbers so the key is
// invariant under operand-order swaps.
//
// Takes inst (instruction) which is the instruction whose value key is required.
// Takes state (*gvnState) which is the per-function GVN state used to resolve operand
// value numbers.
//
// Returns the computed gvnValueKey when inst is modelled precisely, and a boolean true
// when a key was produced; false for opcodes outside the model.
func computeGvnValueKey(inst isa.Instruction, state *gvnState) (gvnValueKey, bool) {
	switch inst.Op {
	case isa.OpAddInt, isa.OpMulInt, isa.OpAddUint, isa.OpMulUint, isa.OpAddFloat, isa.OpMulFloat:
		left, leftOK := gvnLookupValueNumber(state, inst.B, gvnBankForArithmetic(inst.Op))
		right, rightOK := gvnLookupValueNumber(state, inst.C, gvnBankForArithmetic(inst.Op))
		if !leftOK || !rightOK {
			return gvnValueKey{}, false
		}
		if left > right {
			left, right = right, left
		}
		return gvnValueKey{op: inst.Op, operand0: left, operand1: right}, true
	case isa.OpSubInt, isa.OpSubUint, isa.OpSubFloat, isa.OpDivInt, isa.OpDivUint, isa.OpDivFloat:
		left, leftOK := gvnLookupValueNumber(state, inst.B, gvnBankForArithmetic(inst.Op))
		right, rightOK := gvnLookupValueNumber(state, inst.C, gvnBankForArithmetic(inst.Op))
		if !leftOK || !rightOK {
			return gvnValueKey{}, false
		}
		return gvnValueKey{op: inst.Op, operand0: left, operand1: right}, true
	case isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst:
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
// Returns the canonical value number and true, or (0, false) when the register has not
// been written within the visible scope.
func gvnLookupValueNumber(state *gvnState, reg uint8, bank int) (int, bool) {
	if bank < 0 || bank >= gvnRegisterTrackedBanks {
		return 0, false
	}
	def := state.regLastDef[bank][reg]
	if def == gvnNoDefiningPC {
		return 0, false
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
	case isa.OpAddInt, isa.OpSubInt, isa.OpMulInt, isa.OpDivInt:
		return gvnBankIndexInt
	case isa.OpAddUint, isa.OpSubUint, isa.OpMulUint, isa.OpDivUint:
		return gvnBankIndexUint
	case isa.OpAddFloat, isa.OpSubFloat, isa.OpMulFloat, isa.OpDivFloat:
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
	case isa.OpAddInt, isa.OpSubInt, isa.OpMulInt, isa.OpDivInt, isa.OpRemInt,
		isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst,
		isa.OpLoadIntConst:
		return gvnBankIndexInt, inst.A, true
	case isa.OpAddUint, isa.OpSubUint, isa.OpMulUint, isa.OpDivUint, isa.OpRemUint,
		isa.OpLoadUintConst:
		return gvnBankIndexUint, inst.A, true
	case isa.OpAddFloat, isa.OpSubFloat, isa.OpMulFloat, isa.OpDivFloat,
		isa.OpLoadFloatConst:
		return gvnBankIndexFloat, inst.A, true
	default:
	}
	return 0, 0, false
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
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveInt), destination, source), true
	case gvnBankIndexUint:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveUint), destination, source), true
	case gvnBankIndexFloat:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveFloat), destination, source), true
	}
	return isa.Instruction{}, false
}
