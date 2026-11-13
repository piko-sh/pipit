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
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// propagateGvnCopy makes every read of the register a value-numbering move wrote read the
// register it copied instead, then blanks the move.
//
// The reads are those the move's value reaches before its register is written again. They
// must be reached from the move alone, and nothing between the move and the end of its
// value may write the copied register, so the two registers agree at every read. A
// register that is a result slot, or that names a variable a debugger can show, keeps its
// move.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
// Takes analysis (*functionAnalysis) which supplies predecessor edges. It stays current,
// because the rewrite changes register operands and no word's place or target.
// Takes movePC (int) which is the move.
//
// Returns bool which is true when the move was removed.
func propagateGvnCopy(compiledFunction *program.CompiledFunction, analysis *functionAnalysis, movePC int) bool {
	body := compiledFunction.Body
	bank, destination, source, ok := gvnMoveWithinBank(body[movePC])
	if !ok {
		return false
	}
	if destination == source {
		body[movePC] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
		return true
	}
	kind := gvnBankKind(bank)
	if isResultSlot(compiledFunction, kind, destination) || analysis.MayCreateSharedCells() ||
		resultSlotReadByRecover(compiledFunction, kind, destination) {
		return false
	}
	found, ok := collectDefinitionUses(compiledFunction, movePC+1, kind, destination)
	reads := found.reachChecks()
	if !ok || !usesReachedOnlyFrom(analysis, &found.members, reads, &found.redefiners, movePC, movePC+1) ||
		!wordsBeforeReadsKeepRegister(compiledFunction, analysis, reads, movePC, kind, source) {
		return false
	}
	last := movePC
	for _, use := range reads {
		last = max(last, use)
	}
	if namesDebugVariable(compiledFunction, kind, destination, movePC, last) || touchesDebugRange(compiledFunction, kind, destination, reads) {
		return false
	}
	for _, use := range found.uses {
		body[use] = renameRegisterReads(body[use], kind, destination, source)
	}
	for _, use := range found.extensionUses {
		body[use.pc] = renameExtensionReads(body[use.pc], use.bytes, source)
	}
	body[movePC] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// wordsBeforeReadsKeepRegister reports whether a register keeps its value on every way
// from a move to the reads the move reaches.
//
// The words in question are those that come before some read on a path from the move. A
// read may itself write the register, since it reads first, as long as no other read
// follows it.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the body and the call
// sites.
// Takes analysis (*functionAnalysis) which supplies predecessor edges.
// Takes reads ([]int) which lists the reading instructions and extension-word owners,
// each reached from the move alone.
// Takes movePC (int) which is the move.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
//
// Returns bool which is true when none of those words writes the register.
func wordsBeforeReadsKeepRegister(
	compiledFunction *program.CompiledFunction, analysis *functionAnalysis, reads []int, movePC int, kind isa.RegisterKind, reg uint8,
) bool {
	analysis.buildCFG()
	pending := slices.Clone(reads)
	before := reachedWords{set: nil, count: 0}
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, predecessor := range analysis.predecessors[pc] {
			if predecessor == movePC || !analysis.reachable[predecessor] || before.has(predecessor) {
				continue
			}
			if wordWritesRegister(compiledFunction, predecessor, kind, reg) {
				return false
			}
			before.add(predecessor)
			pending = append(pending, predecessor)
		}
	}
	return true
}

// wordWritesRegister reports whether the word at pc may write a register, whether it is
// an instruction or one of an instruction's extension words.
//
// An extension word whose owner does not describe it is taken to write the register.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the body and the call
// sites.
// Takes pc (int) which is the word.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
//
// Returns bool which is true when the word may write the register.
func wordWritesRegister(compiledFunction *program.CompiledFunction, pc int, kind isa.RegisterKind, reg uint8) bool {
	body := compiledFunction.Body
	word := body[pc]
	if word.Op != isa.OpExtension {
		return instructionMayWriteRegister(compiledFunction, word, kind, reg)
	}
	owner := pc
	for owner > 0 && body[owner].Op == isa.OpExtension {
		owner--
	}
	_, writes, described := isa.ExtensionRegisterUse(body[owner], body[owner+1:pc+1], kind)
	if !described {
		return true
	}
	for position, operand := range [isa.NumInstructionOperands]uint8{word.A, word.B, word.C} {
		if writes[position] && operand == reg {
			return true
		}
	}
	return false
}

// gvnBankKind names the register bank of a bank index of this pass that has a move.
//
// Takes bank (int) which is gvnBankIndexInt, gvnBankIndexUint or gvnBankIndexFloat.
//
// Returns isa.RegisterKind which is the bank.
func gvnBankKind(bank int) isa.RegisterKind {
	switch bank {
	case gvnBankIndexUint:
		return isa.RegisterUint64
	case gvnBankIndexFloat:
		return isa.RegisterFloat64
	default:
		return isa.RegisterInt64
	}
}
