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
	"reflect"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// maxCseFieldReadScanWindow caps the forward-scan distance from a candidate first read
	// to its potential redundant successor.
	maxCseFieldReadScanWindow = 16

	// cseAliasSetCapacity caps the alias set size. Beyond this the CSE pass stops adding new
	// aliases (but keeps the existing entries), trading completeness for bounded scan cost.
	cseAliasSetCapacity = 4
)

// cseAliasSet tracks registers known to hold the cached struct-field value, seeded from
// the original read destination and extended by same-bank moves.
type cseAliasSet struct {
	// entries holds the alias register indices; valid up to length.
	entries [cseAliasSetCapacity]uint8

	// length is the number of valid entries currently in the set.
	length uint8
}

// newCseAliasSet seeds the set with the original read destination as the only live alias.
//
// Takes priorDest (uint8) which is the original read's destination register index.
//
// Returns a cseAliasSet containing priorDest as its sole entry.
func newCseAliasSet(priorDest uint8) cseAliasSet {
	var set cseAliasSet
	set.entries[0] = priorDest
	set.length = 1
	return set
}

// add inserts reg into the alias set when capacity remains and reg is not already
// present. Silently no-ops when the set is full.
//
// Takes reg (uint8) which is the register index to add to the set.
func (set *cseAliasSet) add(reg uint8) {
	if set.contains(reg) {
		return
	}
	if int(set.length) >= len(set.entries) {
		return
	}
	set.entries[set.length] = reg
	set.length++
}

// contains reports whether reg is currently in the alias set.
//
// Takes reg (uint8) which is the register index to test for membership.
//
// Returns true when reg is present, false otherwise.
func (set *cseAliasSet) contains(reg uint8) bool {
	for i := uint8(0); i < set.length; i++ {
		if set.entries[i] == reg {
			return true
		}
	}
	return false
}

// empty reports whether all aliases have been killed.
//
// Returns true when the set contains no live aliases.
func (set *cseAliasSet) empty() bool {
	return set.length == 0
}

// priorDest returns the original first-read destination.
//
// Useful for continuing to detect receiver-register writes regardless of which alias the
// rewrite ultimately selects.
//
// Returns the original read's destination register, or 0 when the set is empty.
func (set *cseAliasSet) priorDest() uint8 {
	if set.length == 0 {
		return 0
	}
	return set.entries[0]
}

// preferredSource returns the smallest-index alias from the set. Picking the smallest
// register makes the downstream MOVE-elimination pass more likely to coalesce the
// inserted move into existing register state.
//
// Returns the smallest-index register in the set, or 0 when the set is empty.
func (set *cseAliasSet) preferredSource() uint8 {
	if set.length == 0 {
		return 0
	}
	best := set.entries[0]
	for i := uint8(1); i < set.length; i++ {
		if set.entries[i] < best {
			best = set.entries[i]
		}
	}
	return best
}

// dropRegistersWrittenBy removes any alias whose register the instruction writes in the
// matching bank.
//
// Takes inst (instruction) whose writes are checked against the set.
// Takes bank (isa.OperandRole) which is the destination bank role to test for;
// isa.RoleNone skips the scan.
func (set *cseAliasSet) dropRegistersWrittenBy(inst isa.Instruction, bank isa.OperandRole) {
	if bank == isa.RoleNone {
		return
	}
	write := 0
	for read := uint8(0); read < set.length; read++ {
		reg := set.entries[read]
		if instructionWritesRegisterInBank(inst, bank, reg) {
			continue
		}
		set.entries[write] = reg
		write++
	}
	set.length = uint8(write)
}

// ElideRedundantStructFieldRead replaces a redundant struct-field reload with a same-bank
// move from the prior destination, covering tier-0 and tier-1 reads.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
//
// Returns error when context cancellation fires mid-rewrite.
func ElideRedundantStructFieldRead(ctx context.Context, compiledFunction *program.CompiledFunction, body []isa.Instruction) error {
	if len(body) < 2 {
		return nil
	}
	return elideRedundantStructFieldRead(ctx, compiledFunction, body, newFunctionAnalysis(compiledFunction))
}

// elideRedundantStructFieldRead is ElideRedundantStructFieldRead with the jump-target set
// supplied by the caller.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
// Takes analysis (*FunctionAnalysis) which supplies the jump-target set on demand.
//
// Returns error when context cancellation fires mid-rewrite.
func elideRedundantStructFieldRead(ctx context.Context, compiledFunction *program.CompiledFunction, body []isa.Instruction, analysis *functionAnalysis) error {
	if len(body) < 2 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("elideRedundantStructFieldRead cancelled: %w", err)
	}
	jumpTargets := analysis.JumpTargets()
	for i := range body {
		if i&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("elideRedundantStructFieldRead cancelled: %w", err)
			}
		}
		if isTier0StructFieldRead(body[i].Op) {
			elideTier0Read(compiledFunction, body, i, jumpTargets)
			continue
		}
		if isTier1StructFieldReadAt(body, i) {
			elideTier1Read(compiledFunction, body, i, jumpTargets)
			continue
		}
		if isGeneralBankFieldWrite(body[i]) {
			elidePostSetRead(compiledFunction, body, i, jumpTargets)
		}
	}
	return nil
}

// elideTier0Read rewrites a redundant tier-0 struct-field read at firstIdx as a MOVE from
// the prior live alias, recording the rewrite.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
// Takes firstIdx (int) which is the index of the candidate first read in body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
func elideTier0Read(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) {
	matchIdx, srcReg, found := findRedundantTier0StructFieldRead(compiledFunction, body, firstIdx, jumpTargets)
	if !found {
		return
	}

	if tier0ReadDestinationFedToFieldWrite(body, matchIdx, body[matchIdx].A,
		tier0FieldReadDestRole(body[firstIdx].Op), jumpTargets) {
		return
	}
	body[matchIdx] = emitMoveForTier0ReadOp(body[firstIdx].Op, body[matchIdx].A, srcReg)
	RecordPeepholeRewrite(compiledFunction, matchIdx, peepholeRewriteCseTier0, firstIdx)
}

// elideTier1Read rewrites a redundant tier-1 struct-field read at firstIdx as a same-bank
// tier-1 MOVE, nopping the extension word.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
// Takes firstIdx (int) which is the umbrella-word index of the candidate first read in
// body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
func elideTier1Read(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) {
	matchIdx, srcReg, found := findRedundantTier1StructFieldRead(compiledFunction, body, firstIdx, jumpTargets)
	if !found {
		return
	}

	if tier0ReadDestinationFedToFieldWrite(body, matchIdx, body[matchIdx].B,
		tier1FieldReadDestRole(isa.SubOpcode(body[matchIdx].A)), jumpTargets) {
		return
	}
	rewriteTier1ReadAsMove(body, matchIdx, srcReg)
	RecordPeepholeRewrite(compiledFunction, matchIdx, peepholeRewriteCseTier1Umbrella, firstIdx)
	RecordPeepholeRewrite(compiledFunction, matchIdx+1, peepholeRewriteCseTier1Ext, firstIdx)
}

// elidePostSetRead rewrites a struct-field read following a matching general-bank field
// write as a MOVE_GENERAL from the setter's source.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the function's instruction stream rewritten in
// place.
// Takes firstIdx (int) which is the index of the field-write instruction in body.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
func elidePostSetRead(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) {
	if layoutLeafIsAggregate(compiledFunction, body[firstIdx].C) {
		return
	}
	matchIdx, srcReg, found := findGetAfterGeneralFieldSet(compiledFunction, body, firstIdx, jumpTargets)
	if !found {
		return
	}
	body[matchIdx] = isa.NewInstruction(isa.OpMoveGeneral, body[matchIdx].A, srcReg, engine.MoveGeneralModeAlias)
	RecordPeepholeRewrite(compiledFunction, matchIdx, peepholeRewriteCsePostSet, firstIdx)
}

// layoutLeafIsAggregate reports whether the struct-field layout at layoutIdx describes a
// Struct- or Array-kind leaf, for which no post-set read elision is sound. The setter
// copies bytes via typedmemmove, so only a real re-read yields the addressable view.
//
// Takes compiledFunction (*program.CompiledFunction) which carries the struct layout
// table.
// Takes layoutIdx (uint8) which is the setter's layout table index.
//
// Returns true for Struct and Array leaves, and for any index the layout table cannot
// resolve.
func layoutLeafIsAggregate(compiledFunction *program.CompiledFunction, layoutIdx uint8) bool {
	if compiledFunction == nil || int(layoutIdx) >= len(compiledFunction.StructLayoutTable) {
		return true
	}
	switch reflect.Kind(compiledFunction.StructLayoutTable[layoutIdx].Kind) {
	case reflect.Struct, reflect.Array:
		return true
	default:
		return false
	}
}

// tier0ReadDestinationFedToFieldWrite reports whether the destination register of the
// read at matchIdx is used as a struct-field-write receiver before being overwritten.
// Prevents a MOVE replacement from severing the addressable alias the original GET
// provides.
//
// Takes body ([]isa.Instruction) which is the function's instruction stream.
// Takes matchIdx (int) which is the index of the candidate redundant read.
// Takes destReg (uint8) which is the matched read's destination register.
// Takes destBank (isa.OperandRole) which is the bank destReg belongs to.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when matchIdx's destination is observed as a struct-field-write receiver
// before being overwritten.
func tier0ReadDestinationFedToFieldWrite(body []isa.Instruction, matchIdx int, destReg uint8, destBank isa.OperandRole, jumpTargets map[int]bool) bool {
	if matchIdx < 0 || matchIdx >= len(body) || destBank != isa.RoleRegGeneral {
		return false
	}
	limit := min(matchIdx+maxCseFieldReadScanWindow, len(body))
	for j := matchIdx + 1; j < limit; j++ {
		if jumpTargets[j] {
			return false
		}
		inst := body[j]
		if receiver, ok := structFieldWriteReceiver(inst); ok && receiver == destReg {
			return true
		}
		if instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, destReg) {
			return false
		}
	}
	return false
}

// isGeneralBankFieldWrite reports whether inst is a general-bank struct-field write
// (isa.OpSetStructFieldGeneral) whose post-write value can be referenced from a MOVE.
//
// Takes inst (instruction) which is the candidate instruction to classify.
//
// Returns true when inst is isa.OpSetStructFieldGeneral.
func isGeneralBankFieldWrite(inst isa.Instruction) bool {
	return inst.Op == isa.OpSetStructFieldGeneral
}

// findGetAfterGeneralFieldSet locates a redundant read after a matching write. When the
// same (receiver, layout) pair is read within the scan window, the read rewrites to a
// MOVE_GENERAL from the setter's source.
//
// Takes compiledFunction (*CompiledFunction) which carries alias info.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes firstIdx (int) which is the index of the candidate general-bank field write.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns the read index, the source register, and true on success.
func findGetAfterGeneralFieldSet(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) (int, uint8, bool) {
	first := body[firstIdx]
	receiverReg := first.A
	layoutIdx := first.C
	valueReg := first.B
	destBank := isa.RoleRegGeneral
	aliases := newCseAliasSet(valueReg)
	limit := min(firstIdx+maxCseFieldReadScanWindow, len(body))
	for j := firstIdx + 1; j < limit; j++ {
		if jumpTargets[j] {
			return 0, 0, false
		}
		inst := body[j]
		if isGeneralBankMatchingRead(inst.Op) && inst.B == receiverReg && inst.C == layoutIdx {
			return j, aliases.preferredSource(), true
		}
		if newAlias, srcReg, ok := detectAliasingMove(inst, destBank); ok && aliases.contains(srcReg) {
			aliases.add(newAlias)
			continue
		}
		if structFieldReadScanBlocksAt(compiledFunction, j, inst, receiverReg, aliases.priorDest(), destBank) {
			return 0, 0, false
		}
		aliases.dropRegistersWrittenBy(inst, destBank)
		if aliases.empty() {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// isGeneralBankMatchingRead reports whether op reads the value last written by
// isa.OpSetStructFieldGeneral.
//
// isa.OpGetStructFieldGeneral matches directly; the cycle-broken raw-pointer
// specialisation also reads the eface header that isa.OpSetStructFieldGeneral writes, so
// it matches too.
//
// Takes op (opcode) which is the candidate read opcode to classify.
//
// Returns true for isa.OpGetStructFieldGeneral or isa.OpGetStructFieldRawPointerT0.
func isGeneralBankMatchingRead(op isa.Opcode) bool {
	return op == isa.OpGetStructFieldGeneral || op == isa.OpGetStructFieldRawPointerT0
}

// isTier0StructFieldRead reports whether op is one of the single-word struct-field read
// opcodes the CSE pass recognises.
//
// Takes op (opcode) which is the candidate opcode to classify.
//
// Returns true for any recognised single-word struct-field read opcode.
func isTier0StructFieldRead(op isa.Opcode) bool {
	switch op {
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0,
		isa.OpGetStructFieldIntT0, isa.OpGetStructFieldUint,
		isa.OpGetStructFieldFloat, isa.OpGetStructFieldBool,
		isa.OpGetField, isa.OpGetFieldInt:
		return true
	default:
	}
	return false
}

// tier0FieldReadDestRole returns the bank role of a tier-0 struct-field read's
// destination register. Used by the scan to detect writes to the prior destination in the
// matching bank.
//
// Takes op (opcode) which is a tier-0 struct-field read opcode.
//
// Returns the destination bank role; isa.RoleNone for unrecognised opcodes.
func tier0FieldReadDestRole(op isa.Opcode) isa.OperandRole {
	switch op {
	case isa.OpGetStructFieldIntT0, isa.OpGetFieldInt:
		return isa.RoleRegInt
	case isa.OpGetStructFieldUint:
		return isa.RoleRegUint
	case isa.OpGetStructFieldFloat:
		return isa.RoleRegFloat
	case isa.OpGetStructFieldBool:
		return isa.RoleRegBool
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0, isa.OpGetField:
		return isa.RoleRegGeneral
	default:
	}
	return isa.RoleNone
}

// emitMoveForTier0ReadOp returns the same-bank move instruction that copies a prior
// struct-field read destination into a fresh destination. General-bank reads use alias
// mode so the addressable view is preserved rather than deep-copied.
//
// Takes readOp (opcode) which is the original read opcode at the matched site.
// Takes destination (uint8) which is the destination register of the matched read.
// Takes source (uint8) which is the destination register of the earlier matching read.
//
// Returns the rewritten instruction.
func emitMoveForTier0ReadOp(readOp isa.Opcode, destination, source uint8) isa.Instruction {
	switch readOp {
	case isa.OpGetStructFieldGeneral, isa.OpGetStructFieldRawPointerT0, isa.OpGetField:
		return isa.NewInstruction(isa.OpMoveGeneral, destination, source, engine.MoveGeneralModeAlias)
	case isa.OpGetStructFieldIntT0, isa.OpGetFieldInt:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveInt), destination, source)
	case isa.OpGetStructFieldUint:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveUint), destination, source)
	case isa.OpGetStructFieldFloat:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveFloat), destination, source)
	case isa.OpGetStructFieldBool:
		return isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpMoveBool), destination, source)
	default:
	}
	return isa.NewInstruction(isa.OpNop, 0, 0, 0)
}

// findRedundantTier0StructFieldRead scans forward from a tier-0 read at firstIdx for a
// matching read with identical opcode, receiver, and layout index.
//
// Takes compiledFunction (*CompiledFunction) which carries alias info.
// Takes body ([]instruction) which is the instruction stream.
// Takes firstIdx (int) which is the index of the first tier-0 read.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns the matching index, the best source alias register, and true on success.
func findRedundantTier0StructFieldRead(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) (int, uint8, bool) {
	first := body[firstIdx]
	receiverReg := first.B
	layoutIdx := first.C
	destBank := tier0FieldReadDestRole(first.Op)
	aliases := newCseAliasSet(first.A)
	limit := min(firstIdx+maxCseFieldReadScanWindow, len(body))
	for j := firstIdx + 1; j < limit; j++ {
		if jumpTargets[j] {
			return 0, 0, false
		}
		inst := body[j]
		if inst.Op == first.Op && inst.B == receiverReg && inst.C == layoutIdx {
			return j, aliases.preferredSource(), true
		}
		if newAlias, srcReg, ok := detectAliasingMove(inst, destBank); ok && aliases.contains(srcReg) {
			aliases.add(newAlias)
			continue
		}
		if structFieldReadScanBlocksAt(compiledFunction, j, inst, receiverReg, aliases.priorDest(), destBank) {
			return 0, 0, false
		}
		aliases.dropRegistersWrittenBy(inst, destBank)
		if aliases.empty() {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// detectAliasingMove reports whether inst is a same-bank move that copies a value into a
// new register. General-bank scans accept only alias-mode moves because the alias set
// tracks address identity, not value identity.
//
// Takes inst (instruction) which is the candidate instruction.
// Takes bank (isa.OperandRole) which is the destination bank of the active read.
//
// Returns (newAlias, srcReg, true) when inst is a same-bank move.
func detectAliasingMove(inst isa.Instruction, bank isa.OperandRole) (newAlias, srcReg uint8, ok bool) {
	if inst.Op == isa.OpMoveGeneral && bank == isa.RoleRegGeneral {
		return inst.A, inst.B, inst.C == engine.MoveGeneralModeAlias
	}
	if inst.Op != isa.OpDrillTier1 {
		return 0, 0, false
	}
	if subOpMatchesBankMove(isa.SubOpcode(inst.A), bank) {
		return inst.B, inst.C, true
	}
	return 0, 0, false
}

// subOpMatchesBankMove reports whether sub is a same-bank tier-1 move targeting bank.
//
// Takes sub (isa.SubOpcode) which is the candidate tier-1 sub-opcode.
// Takes bank (isa.OperandRole) which is the bank role the caller wants the move to
// target.
//
// Returns true when sub is the same-bank move sub-op for bank.
func subOpMatchesBankMove(sub isa.SubOpcode, bank isa.OperandRole) bool {
	switch sub {
	case isa.SubOpMoveInt:
		return bank == isa.RoleRegInt
	case isa.SubOpMoveUint:
		return bank == isa.RoleRegUint
	case isa.SubOpMoveFloat:
		return bank == isa.RoleRegFloat
	case isa.SubOpMoveBool:
		return bank == isa.RoleRegBool
	case isa.SubOpMoveString:
		return bank == isa.RoleRegString
	case isa.SubOpMoveComplex:
		return bank == isa.RoleRegComplex
	default:
	}
	return false
}

// isTier1StructFieldReadAt reports whether body[i] is the umbrella word of a tier-1
// struct-field read with a following isa.OpExt layout extension. The CSE pass only
// considers tier-1 reads when both words are present and the next word is an isa.OpExt.
//
// Takes body ([]instruction) which is the function's instruction stream.
// Takes i (int) which is the candidate umbrella-word index.
//
// Returns true when body[i] and body[i+1] form a tier-1 struct-field read.
func isTier1StructFieldReadAt(body []isa.Instruction, i int) bool {
	if i+1 >= len(body) {
		return false
	}
	if body[i].Op != isa.OpDrillTier1 {
		return false
	}
	if body[i+1].Op != isa.OpExt {
		return false
	}
	return isTier1StructFieldReadSubOp(isa.SubOpcode(body[i].A))
}

// isTier1StructFieldReadSubOp reports whether a tier-1 sub-op is one of the
// layout-table-driven struct-field reads.
//
// Takes sub (isa.SubOpcode) which is the candidate tier-1 sub-opcode.
//
// Returns true for any subOpGetStructFieldX read sub-op.
func isTier1StructFieldReadSubOp(sub isa.SubOpcode) bool {
	switch sub {
	case isa.SubOpGetStructFieldInt, isa.SubOpGetStructFieldUint,
		isa.SubOpGetStructFieldFloat, isa.SubOpGetStructFieldBool,
		isa.SubOpGetStructFieldString:
		return true
	default:
	}
	return false
}

// tier1FieldReadDestRole returns the bank role of a tier-1 struct-field read sub-op's
// destination register.
//
// Takes sub (isa.SubOpcode) which is a tier-1 struct-field read sub-opcode.
//
// Returns the destination bank role; isa.RoleNone for unrecognised sub-ops.
func tier1FieldReadDestRole(sub isa.SubOpcode) isa.OperandRole {
	switch sub {
	case isa.SubOpGetStructFieldInt:
		return isa.RoleRegInt
	case isa.SubOpGetStructFieldUint:
		return isa.RoleRegUint
	case isa.SubOpGetStructFieldFloat:
		return isa.RoleRegFloat
	case isa.SubOpGetStructFieldBool:
		return isa.RoleRegBool
	case isa.SubOpGetStructFieldString:
		return isa.RoleRegString
	default:
	}
	return isa.RoleNone
}

// tier1ReadToMoveSubOp maps a tier-1 struct-field read sub-op to the matching same-bank
// move sub-op used as the CSE rewrite target.
//
// Takes sub (isa.SubOpcode) which is a tier-1 struct-field read sub-opcode.
//
// Returns the matching move sub-op and true on success; (0, false) for unrecognised
// sub-ops.
func tier1ReadToMoveSubOp(sub isa.SubOpcode) (isa.SubOpcode, bool) {
	switch sub {
	case isa.SubOpGetStructFieldInt:
		return isa.SubOpMoveInt, true
	case isa.SubOpGetStructFieldUint:
		return isa.SubOpMoveUint, true
	case isa.SubOpGetStructFieldFloat:
		return isa.SubOpMoveFloat, true
	case isa.SubOpGetStructFieldBool:
		return isa.SubOpMoveBool, true
	case isa.SubOpGetStructFieldString:
		return isa.SubOpMoveString, true
	default:
	}
	return 0, false
}

// findRedundantTier1StructFieldRead scans forward from a two-word tier-1 read at firstIdx
// for a matching read with identical sub-opcode, receiver, and 16-bit EXT layout index.
//
// Takes compiledFunction (*CompiledFunction) which carries alias info.
// Takes body ([]instruction) which is the function's instruction stream.
// Takes firstIdx (int) which is the umbrella-word index of the first read.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns the redundant read's index, the best source alias, and true on success.
func findRedundantTier1StructFieldRead(compiledFunction *program.CompiledFunction, body []isa.Instruction, firstIdx int, jumpTargets map[int]bool) (int, uint8, bool) {
	first := body[firstIdx]
	firstExt := body[firstIdx+1]
	sub := isa.SubOpcode(first.A)
	receiverReg := first.C
	destBank := tier1FieldReadDestRole(sub)
	aliases := newCseAliasSet(first.B)
	layoutLow := firstExt.A
	layoutHigh := firstExt.B
	limit := min(firstIdx+maxCseFieldReadScanWindow, len(body))
	for j := firstIdx + 2; j < limit; j++ {
		if jumpTargets[j] {
			return 0, 0, false
		}
		if matchesTier1Read(body, j, sub, receiverReg, layoutLow, layoutHigh) {
			return j, aliases.preferredSource(), true
		}
		inst := body[j]
		if newAlias, srcReg, ok := detectAliasingMove(inst, destBank); ok && aliases.contains(srcReg) {
			aliases.add(newAlias)
			continue
		}
		if structFieldReadScanBlocksAt(compiledFunction, j, inst, receiverReg, aliases.priorDest(), destBank) {
			return 0, 0, false
		}
		aliases.dropRegistersWrittenBy(inst, destBank)
		if aliases.empty() {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// matchesTier1Read reports whether the tier-1 read at PC j has the same sub-opcode,
// receiver register, and extension-word layout (split across low/high bytes) as the
// cached read being tracked.
//
// Takes body ([]instruction) which is the function's instruction stream.
// Takes j (int) which is the umbrella-word index of the candidate read.
// Takes sub (isa.SubOpcode) which is the cached read's sub-opcode to match against.
// Takes receiverReg (uint8) which is the cached read's receiver register to match
// against.
// Takes layoutLow (uint8) which is the low byte of the cached read's layout index.
// Takes layoutHigh (uint8) which is the high byte of the cached read's layout index.
//
// Returns true when body[j..j+1] match the cached read on all keys.
func matchesTier1Read(body []isa.Instruction, j int, sub isa.SubOpcode, receiverReg, layoutLow, layoutHigh uint8) bool {
	if !isTier1StructFieldReadAt(body, j) {
		return false
	}
	inst := body[j]
	if isa.SubOpcode(inst.A) != sub || inst.C != receiverReg {
		return false
	}
	ext := body[j+1]
	return ext.A == layoutLow && ext.B == layoutHigh
}

// rewriteTier1ReadAsMove replaces the tier-1 read at matchIdx with the corresponding
// same-bank tier-1 move, and nops out the trailing isa.OpExt word that carries the read's
// layout index.
//
// Takes body ([]instruction) which is the function's instruction stream.
// Takes matchIdx (int) which is the umbrella-word index of the matched read.
// Takes priorReadDest (uint8) which is the destination register of the earlier matching
// read (becomes the move's source).
func rewriteTier1ReadAsMove(body []isa.Instruction, matchIdx int, priorReadDest uint8) {
	matchedReadDest := body[matchIdx].B
	moveSub, ok := tier1ReadToMoveSubOp(isa.SubOpcode(body[matchIdx].A))
	if !ok {
		return
	}
	body[matchIdx] = isa.NewInstruction(isa.OpDrillTier1, uint8(moveSub), matchedReadDest, priorReadDest)
	body[matchIdx+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
}

// structFieldReadScanBlocksAt is the alias-aware scan-blocker check.
//
// When pc is non-negative and the function has populated alias info, struct-field-write
// instructions whose receiver provably does not alias the cached-read's receiver no
// longer invalidate the scan. Calls and undescribed opcodes remain blanket blockers
// because those routes can mutate any heap state.
//
// compiledFunction is consulted at call instructions: a call to a callee classified as
// HeapPureCallee by RunHeapPurityAnalysis does not bail the scan. nil compiledFunction
// falls back to the call-blanket-blocker behaviour.
//
// Takes compiledFunction (*CompiledFunction) which is the function being analysed; nil
// disables alias refinement.
// Takes pc (int) which is the program counter of inst within compiledFunction.body; pass
// -1 when no PC context is available (the fallback path skips the alias refinement).
// Takes inst (instruction) which is the candidate instruction to classify.
// Takes receiverReg (uint8) which is the cached read's receiver register.
// Takes priorDest (uint8) which is the cached read's prior destination register.
// Takes destBank (isa.OperandRole) which is the cached read's destination bank role.
//
// Returns true when inst must terminate the scan, false when it is safe to skip.
func structFieldReadScanBlocksAt(compiledFunction *program.CompiledFunction, pc int, inst isa.Instruction, receiverReg, priorDest uint8, destBank isa.OperandRole) bool {
	if structFieldWriteWithKnownReceiverDistinct(compiledFunction, pc, inst, receiverReg) {
		return false
	}
	if invalidatesCachedFieldReads(compiledFunction, inst) {
		return true
	}
	if !instructionShapeAllowsCseScan(inst) {
		return true
	}
	if instructionWritesRegisterInBank(inst, isa.RoleRegGeneral, receiverReg) {
		return true
	}
	if destBank != isa.RoleNone && instructionWritesRegisterInBank(inst, destBank, priorDest) {
		return true
	}
	return false
}

// structFieldWriteWithKnownReceiverDistinct reports whether inst is a struct-field write
// whose receiver provably does not alias the cached-read's receiver.
//
// Takes compiledFunction (*CompiledFunction) which carries alias info.
// Takes pc (int) which is the program counter of inst.
// Takes inst (instruction) which is the candidate instruction.
// Takes cachedReceiverReg (uint8) which is the cached read's receiver register.
//
// Returns true when the alias info confirms the two receivers are distinct.
func structFieldWriteWithKnownReceiverDistinct(compiledFunction *program.CompiledFunction, pc int, inst isa.Instruction, cachedReceiverReg uint8) bool {
	if compiledFunction == nil || compiledFunction.AliasInfo == nil {
		return false
	}
	if pc <= 0 || pc >= len(compiledFunction.Body) {
		return false
	}
	writeReceiverReg, ok := structFieldWriteReceiver(inst)
	if !ok {
		return false
	}
	queryPC := pc - 1
	return !compiledFunction.AliasInfo.MayAlias(queryPC, writeReceiverReg, cachedReceiverReg)
}

// structFieldWriteReceiver returns the general-bank receiver register of a
// struct-field-write opcode, or (0, false) when inst is not a recognised write.
//
// Takes inst (instruction) which is the candidate instruction to classify.
//
// Returns the receiver register and true on success; (0, false) otherwise.
func structFieldWriteReceiver(inst isa.Instruction) (uint8, bool) {
	switch inst.Op {
	case isa.OpSetStructFieldGeneral, isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint,
		isa.OpSetStructFieldFloat, isa.OpSetStructFieldBool,
		isa.OpSwapStructFieldsGeneralT0, isa.OpSetField:
		return inst.A, true
	case isa.OpDrillTier1:
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpSetStructFieldInt, isa.SubOpSetStructFieldUint,
			isa.SubOpSetStructFieldFloat, isa.SubOpSetStructFieldBool,
			isa.SubOpSetStructFieldString,
			isa.SubOpIncStructFieldInt, isa.SubOpDecStructFieldInt,
			isa.SubOpIncStructFieldUint, isa.SubOpDecStructFieldUint:
			return inst.B, true
		default:
		}
	default:
	}
	return 0, false
}

// invalidatesCachedFieldReads reports whether inst may mutate heap state that a cached
// struct-field read would observe. Calls defer to the callee's heap-purity class when
// resolvable, and conservatively invalidate otherwise.
//
// Takes compiledFunction (*CompiledFunction) which carries heap-purity info, or nil.
// Takes inst (instruction) which is the candidate instruction.
//
// Returns true when inst may invalidate a cached field read.
func invalidatesCachedFieldReads(compiledFunction *program.CompiledFunction, inst isa.Instruction) bool {
	if InstructionDirectlyMutatesHeap(inst) {
		return true
	}
	if !IsCallInstruction(inst) {
		return false
	}
	if compiledFunction == nil {
		return true
	}
	return CallInvalidatesPurity(compiledFunction, inst)
}

// instructionShapeAllowsCseScan reports whether the operand-shape table describes inst
// well enough to reason about its writes. Returns false for undescribed or opaque-write
// opcodes.
//
// Takes inst (instruction) which is the candidate instruction to classify.
//
// Returns true when the shape table fully describes inst's register effects.
func instructionShapeAllowsCseScan(inst isa.Instruction) bool {
	flags := isa.ShapeForInstruction(inst).Flags
	if flags&isa.ShapeFlagOpaqueWrites != 0 {
		return false
	}
	return flags&isa.ShapeFlagDescribed != 0
}

// instructionWritesRegisterInBank reports whether inst writes the given register index in
// the given bank. Undescribed or opaque-write shapes conservatively return true.
//
// Takes inst (instruction) which is the candidate instruction.
// Takes bank (isa.OperandRole) which is the destination bank to test for.
// Takes reg (uint8) which is the register index to test for.
//
// Returns true when inst writes register reg in bank.
func instructionWritesRegisterInBank(inst isa.Instruction, bank isa.OperandRole, reg uint8) bool {
	if bank == isa.RoleNone {
		return false
	}

	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return true
	}
	if shape.Writes[0] && operandWriteMatchesBank(inst, 0, shape.A, bank) && inst.A == reg {
		return true
	}
	if shape.Writes[1] && operandWriteMatchesBank(inst, 1, shape.B, bank) && inst.B == reg {
		return true
	}
	if shape.Writes[2] && operandWriteMatchesBank(inst, 2, shape.C, bank) && inst.C == reg {
		return true
	}
	return false
}

// operandWriteMatchesBank reports whether the write at operand position targets the
// requested bank. Dynamic roles resolve via resolveDynamicWriteBank; unresolvable roles
// conservatively match.
//
// Takes inst (instruction) which is the candidate instruction.
// Takes pos (int) which is the operand position (0, 1, or 2).
// Takes observed (isa.OperandRole) which is the shape table's role for pos.
// Takes bank (isa.OperandRole) which is the destination bank to test for.
//
// Returns true when the write at pos matches bank.
func operandWriteMatchesBank(inst isa.Instruction, pos int, observed, bank isa.OperandRole) bool {
	if observed == bank {
		return true
	}
	if observed != isa.RoleRegDynamic {
		return false
	}
	resolved, ok := resolveDynamicWriteBank(inst, pos)
	if !ok {
		return true
	}
	return resolved == bank
}

// resolveDynamicWriteBank decodes the concrete bank a dynamic-bank write targets from the
// instruction's side channel (kind marker or tier-1 sub-op discriminator).
//
// Takes inst (instruction) which is the candidate instruction.
// Takes pos (int) which is the operand position (0, 1, or 2).
//
// Returns the resolved bank and true on success, or (RoleNone, false) when unresolvable.
func resolveDynamicWriteBank(inst isa.Instruction, pos int) (isa.OperandRole, bool) {
	switch inst.Op {
	case isa.OpTruncateNarrow:
		if pos != 0 {
			return isa.RoleNone, false
		}
		kind := isa.RegisterKind(inst.C)
		return isa.RoleForKind(kind), true
	case isa.OpDrillTier1:
		if pos == 1 {
			if bank, ok := resolveTier1WriteBank(isa.SubOpcode(inst.A)); ok {
				return bank, true
			}
		}
	default:
	}

	if kind, ok := kindMarkerOperandOf(inst, isa.ShapeForInstruction(inst)); ok {
		return isa.RoleForKind(kind), true
	}
	return isa.RoleNone, false
}

// resolveTier1WriteBank reports the destination bank of a tier-1 sub-op that writes
// operand B. Covers same-bank moves, cross-bank moves, struct-field reads, struct-field
// inc/dec ops, append helpers, and a handful of other arithmetic / load sub-ops whose
// target bank is statically determined by the sub-opcode.
//
// Takes sub (isa.SubOpcode) which is the tier-1 sub-opcode to classify.
//
// Returns (bank, true) on a recognised sub-op; (isa.RoleNone, false) otherwise. The CSE /
// LICM passes treat the false return as "bank unknown" and conservatively
// over-invalidate.
//
//nolint:revive // dispatch table
func resolveTier1WriteBank(sub isa.SubOpcode) (isa.OperandRole, bool) {
	switch sub {
	case isa.SubOpMoveInt, isa.SubOpMoveGeneralToInt:
		return isa.RoleRegInt, true
	case isa.SubOpMoveUint:
		return isa.RoleRegUint, true
	case isa.SubOpMoveFloat, isa.SubOpMoveGeneralToFloat:
		return isa.RoleRegFloat, true
	case isa.SubOpMoveBool:
		return isa.RoleRegBool, true
	case isa.SubOpMoveString, isa.SubOpMoveGeneralToString:
		return isa.RoleRegString, true
	case isa.SubOpMoveComplex:
		return isa.RoleRegComplex, true
	case isa.SubOpMoveIntToGeneral, isa.SubOpMoveFloatToGeneral,
		isa.SubOpMoveStringToGeneral:
		return isa.RoleRegGeneral, true
	case isa.SubOpGetStructFieldInt:
		return isa.RoleRegInt, true
	case isa.SubOpGetStructFieldUint:
		return isa.RoleRegUint, true
	case isa.SubOpGetStructFieldFloat:
		return isa.RoleRegFloat, true
	case isa.SubOpGetStructFieldBool:
		return isa.RoleRegBool, true
	case isa.SubOpGetStructFieldString:
		return isa.RoleRegString, true
	case isa.SubOpIncStructFieldInt, isa.SubOpDecStructFieldInt:
		return isa.RoleRegInt, true
	case isa.SubOpIncStructFieldUint, isa.SubOpDecStructFieldUint:
		return isa.RoleRegUint, true
	case isa.SubOpLen, isa.SubOpLenString,
		isa.SubOpLenSliceIntDirect, isa.SubOpLenSliceFloatDirect,
		isa.SubOpLenSliceStringDirect, isa.SubOpLenSliceBoolDirect,
		isa.SubOpLenSliceUintDirect, isa.SubOpLenSliceByteDirect,
		isa.SubOpCapSliceIntDirect, isa.SubOpCapSliceFloatDirect,
		isa.SubOpCapSliceStringDirect, isa.SubOpCapSliceBoolDirect,
		isa.SubOpCapSliceUintDirect, isa.SubOpCapSliceByteDirect:
		return isa.RoleRegInt, true
	default:
	}
	return isa.RoleNone, false
}
