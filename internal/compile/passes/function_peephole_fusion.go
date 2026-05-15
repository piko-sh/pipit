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
	"math"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// rangeCheckShape carries the per-bound immediate and destination register decoded from
// the fuseRangeCheckUintJumpFalse window.
type rangeCheckShape struct {
	// loImm is the low-bound immediate decoded from the first LoadUintConstSmall.
	loImm uint8

	// vRegLo is the destination register of the low-bound load.
	vRegLo uint8

	// hiImm is the high-bound immediate decoded from the second LoadUintConstSmall.
	hiImm uint8

	// vRegHi is the destination register of the high-bound load.
	vRegHi uint8
}

// FuseThreeInstrPatterns dispatches all 3-instruction fusion patterns. Must run before
// 2-instruction patterns to avoid partial consumption.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseThreeInstrPatterns(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	return fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpLeInt, jump: isa.OpJumpIfFalse, fused: isa.SubOpLeIntConstJumpFalse}) ||
		fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpLtInt, jump: isa.OpJumpIfFalse, fused: isa.SubOpLtIntConstJumpFalse}) ||
		fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpEqInt, jump: isa.OpJumpIfFalse, fused: isa.SubOpEqIntConstJumpFalse}) ||
		fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpEqInt, jump: isa.OpJumpIfTrue, fused: isa.SubOpEqIntConstJumpTrue}) ||
		fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpGeInt, jump: isa.OpJumpIfFalse, fused: isa.SubOpGeIntConstJumpFalse}) ||
		fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, compareConstFusion{compare: isa.OpGtInt, jump: isa.OpJumpIfFalse, fused: isa.SubOpGtIntConstJumpFalse}) ||
		fuseStringConstJump(compiledFunction, body, i, n, jumpTargets) ||
		fuseNilTestJump(compiledFunction, body, i, n, jumpTargets) ||
		fuseIncIntJumpLt(compiledFunction, body, i, n, jumpTargets) ||
		fuseLenStringLtJump(compiledFunction, body, i, n, jumpTargets) ||
		fuseEqUintConstJumpFalse(compiledFunction, body, i, n, jumpTargets) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkIntInt, isa.OpMapIndexOkJumpIfFalseIntInt) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkStringInt, isa.OpMapIndexOkJumpIfFalseStringInt) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkStringString, isa.OpMapIndexOkJumpIfFalseStringString) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkIntString, isa.OpMapIndexOkJumpIfFalseIntString) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkIntGeneral, isa.OpMapIndexOkJumpIfFalseIntGeneral) ||
		fuseMapIndexOkJumpFalse(compiledFunction, body, i, n, jumpTargets, isa.OpMapIndexOkStringGeneral, isa.OpMapIndexOkJumpIfFalseStringGeneral)
}

// FuseArithConst fuses LoadIntConst or LoadUintConst with an arithmetic op into a
// constant-operand superinstruction.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseArithConst(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if fuseIntArithConst(compiledFunction, body, i, n, jumpTargets) {
		return true
	}
	return fuseUintArithConst(compiledFunction, body, i, n, jumpTargets)
}

// FuseAddIntJump fuses AddIntConst + Jump into isa.OpAddIntJump + isa.OpExt.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseAddIntJump(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		body[i].Op != isa.OpAddIntConst || !isa.InstrIsTier1SubOp(body[i+1], isa.SubOpJump) ||
		jumpTargets[i+1] {
		return false
	}
	raw := body[i+1].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewInstruction(isa.OpAddIntJump, body[i].A, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	return true
}

// FuseConcatRune fuses RuneToString + ConcatString into isa.OpConcatRuneString.
//
// Operand layout: isa.SubOpRuneToString lives in tier-1 form {isa.OpDrillTier1,
// isa.SubOpRuneToString, destination=B, source=C}. The fusion reads destination from
// body[i].b and source from body[i].c.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseConcatRune(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		!isa.InstrIsTier1SubOp(body[i], isa.SubOpRuneToString) || body[i+1].Op != isa.OpConcatString ||
		body[i].B != body[i+1].C ||
		jumpTargets[i+1] {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpConcatRuneString, body[i+1].A, body[i+1].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// FuseAppendMove fuses APPEND_xxx + MOVE_GENERAL into an in-place APPEND_xxx where the
// destination equals the source-slice register, so the handler takes the in-place fast
// path instead of wrapping a fresh reflect.Value.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseAppendMove(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n || jumpTargets[i+1] {
		return false
	}
	appendInstr := body[i]
	if appendInstr.Op == isa.OpAppend {
		return tryFuseTier0AppendMove(body, i, appendInstr)
	}
	return tryFuseTier1AppendMove(body, i, n, jumpTargets, appendInstr)
}

// OptimiseLoadIntConst rewrites LoadIntConst to LoadIntConstSmall when the constant value
// fits in [0, 255].
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current instruction index.
func OptimiseLoadIntConst(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int) {
	if body[i].Op != isa.OpLoadIntConst || body[i].C != 0 {
		return
	}
	if int(body[i].B) >= len(compiledFunction.IntConstants) {
		return
	}
	value := compiledFunction.IntConstants[body[i].B]
	if value >= 0 && value <= program.MaxSmallConstant {
		body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), body[i].A, safeconv.MustIntToUint8(int(value)))
	}
}

// OptimiseLoadUintConst rewrites LoadUintConst to LoadUintConstSmall when the constant
// fits in the 8-bit inline immediate slot.
//
// Mirror of OptimiseLoadIntConst for the uint bank.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence to scan and rewrite in
// place.
// Takes i (int) which is the index of the candidate instruction.
func OptimiseLoadUintConst(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int) {
	if body[i].Op != isa.OpLoadUintConst || body[i].C != 0 {
		return
	}
	if int(body[i].B) >= len(compiledFunction.UintConstants) {
		return
	}
	value := compiledFunction.UintConstants[body[i].B]
	if value <= uint64(program.MaxSmallConstant) {
		body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLoadUintConstSmall), body[i].A, safeconv.MustIntToUint8(int(value)))
	}
}

// FuseStringIndexToInt fuses isa.OpStringIndex + isa.SubOpUintToInt into
// isa.OpStringIndexToInt, avoiding the intermediate uint register and one tier-2
// trampoline per string byte access converted to int.
//
// The fuser matches STRING_INDEX(R_uint, R_str, R_idx) followed by {isa.OpDrillTier1,
// isa.SubOpUintToInt, R_int=B, R_uint=C} and rewrites the pair to
// STRING_INDEX_TO_INT(R_int, R_str, R_idx) + NOP. isa.SubOpUintToInt lives in tier-1 form
// {isa.OpDrillTier1, isa.SubOpUintToInt, destination=B, source=C}.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseStringIndexToInt(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		body[i].Op != isa.OpStringIndex || !isa.InstrIsTier1SubOp(body[i+1], isa.SubOpUintToInt) ||
		body[i].A != body[i+1].C ||
		jumpTargets[i+1] {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpStringIndexToInt, body[i+1].B, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// SyncSourceMapAfterOptimise zeroes source positions for instructions that were replaced
// with NOPs during optimisation, keeping the source map consistent with the instruction
// body.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence to synchronise.
func SyncSourceMapAfterOptimise(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if compiledFunction.DebugSourceMap == nil {
		return
	}
	for i, instr := range body {
		if isCanonicalNop(instr) && i < len(compiledFunction.DebugSourceMap.Positions) {
			compiledFunction.DebugSourceMap.Positions[i] = program.SourcePosition{Line: 0, Column: 0, FileID: 0, Inlined: false}
		}
	}
}

// fuseLongPatterns dispatches all multi-instruction fusion patterns that span more than
// three opcodes. Runs before FuseThreeInstrPatterns so longer patterns get first dibs and
// are not partially consumed by the shorter compare-jump or compare-const-jump fusers.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseLongPatterns(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	return fuseRangeCheckUintJumpFalse(compiledFunction, body, i, n, jumpTargets)
}

// fuseRangeCheckUintJumpFalse fuses the canonical `value >= lo && value <= hi` byte
// classifier pattern into a single tier-2 sub-op plus two extension words, replacing the
// 8-instruction window with three instruction words and five isa.OpNop padding slots.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if the pattern matched and was applied.
func fuseRangeCheckUintJumpFalse(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+program.RangeCheckWindowSize > n {
		return false
	}
	shape, ok := matchRangeCheckShape(compiledFunction, body, i)
	if !ok {
		return false
	}
	if !rangeCheckJumpTargetsClear(compiledFunction, body, i, jumpTargets) {
		return false
	}
	valueReg, _, ok := matchRangeCheckRegisters(body, i, shape.vRegLo, shape.vRegHi)
	if !ok {
		return false
	}
	off2, ok := validateRangeCheckOffsets(body, i)
	if !ok {
		return false
	}
	offLo, offHi := isa.SplitOffset(off2)
	body[i] = isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, valueReg)
	body[i+1] = isa.NewInstruction(isa.OpExt, shape.loImm, shape.hiImm, 0)
	body[i+2] = isa.NewInstruction(isa.OpExt, offLo, offHi, 0)
	for slot := i + program.RangeCheckFirstNopOffset; slot < i+program.RangeCheckWindowSize; slot++ {
		body[slot] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	}
	return true
}

// matchRangeCheckShape verifies the opcode shape of the 8-instruction range-check window
// and returns the small-uint immediates plus their destination registers from the two
// LoadUintConstSmall positions.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the candidate window start.
//
// Returns the decoded shape (immediates plus destination registers).
// Returns true when every opcode at the expected positions matches.
func matchRangeCheckShape(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int) (rangeCheckShape, bool) {
	var shape rangeCheckShape
	var ok bool
	shape.loImm, shape.vRegLo, ok = matchSmallUintConstLoad(compiledFunction, body[i])
	if !ok {
		return rangeCheckShape{}, false
	}
	if body[i+1].Op != isa.OpGeUint ||
		!isa.InstrIsTier1SubOp(body[i+2], isa.SubOpMoveInt) ||
		body[i+program.RangeCheckFirstJumpOffset].Op != isa.OpJumpIfFalse {
		return rangeCheckShape{}, false
	}
	shape.hiImm, shape.vRegHi, ok = matchSmallUintConstLoad(compiledFunction, body[i+program.RangeCheckSecondLoadOffset])
	if !ok {
		return rangeCheckShape{}, false
	}
	if body[i+program.RangeCheckSecondLoadOffset+1].Op != isa.OpLeUint ||
		!isa.InstrIsTier1SubOp(body[i+program.RangeCheckSecondMoveOffset], isa.SubOpMoveInt) ||
		body[i+program.RangeCheckSecondJumpOffset].Op != isa.OpJumpIfFalse {
		return rangeCheckShape{}, false
	}
	return shape, true
}

// rangeCheckJumpTargetsClear returns true when no jump destination lands inside the
// window's interior. The second jump may land on itself when it is the target of the
// first jump (the chained design); that case is allowed as long as no other incoming jump
// reaches the second-jump position.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the window start.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
//
// Returns true when the rewrite is safe with respect to jump targets.
func rangeCheckJumpTargetsClear(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	for k := i + 1; k <= i+program.RangeCheckSecondMoveOffset; k++ {
		if jumpTargets[k] {
			return false
		}
	}
	if jumpTargets[i+program.RangeCheckSecondJumpOffset] && countIncomingJumps(compiledFunction, body, i+program.RangeCheckSecondJumpOffset, i+program.RangeCheckFirstJumpOffset) > 0 {
		return false
	}
	return true
}

// fuseEqUintConstJumpFalse fuses the 3-op byte-switch dispatch pattern
// `LoadUintConstSmall + EqUint + JumpIfFalse` into a single tier-1 sub-op
// (isa.SubOpEqUintConstJumpFalse) plus one extension word and one trailing isa.OpNop.
// Hits the canonical `switch byteValue` chain (brainfuck dispatch, tokenisers,
// hand-rolled lexers) where each case becomes one of these triples.
//
// Pre-fusion the window holds LoadUintConstSmall(constReg, immVal), EqUint(condReg,
// valueReg, constReg), then JumpIfFalse(condReg, off). Post-fusion the window becomes the
// umbrella op carrying valueReg+immVal, an isa.OpExt word holding the split offset, and a
// trailing isa.OpNop that keeps the body length stable so unrelated jump targets land at
// unchanged indices. The handler reads uints[valueReg] and advances programCounter by the
// encoded offset when the value does not match immVal.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if the pattern matched and was applied.
func fuseEqUintConstJumpFalse(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+program.ThreeInstructionWindow > n {
		return false
	}
	immVal, constReg, ok := matchSmallUintConstLoad(compiledFunction, body[i])
	if !ok {
		return false
	}
	if body[i+1].Op != isa.OpEqUint || body[i+2].Op != isa.OpJumpIfFalse {
		return false
	}
	if jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	var valueReg uint8
	switch {
	case body[i+1].C == constReg:
		valueReg = body[i+1].B
	case body[i+1].B == constReg:
		valueReg = body[i+1].C
	default:
		return false
	}
	condReg := body[i+1].A
	if body[i+2].A != condReg {
		return false
	}
	rawOffset := body[i+2].SignedOffset()
	offLo, offHi := isa.SplitOffset(rawOffset)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpEqUintConstJumpFalse), valueReg, immVal)
	body[i+1] = isa.NewInstruction(isa.OpExt, offLo, offHi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// fuseMapIndexOkJumpFalse fuses a typed `opMapIndexOkXXX + ext + isa.OpJumpIfFalse`
// triple into a single fused dispatch opcode.
//
// Strict adjacency is required because intervening ops could read the moved value on the
// jump-taken branch.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes sourceOpcode (opcode) which is the unfused typed mapIndexOk opcode to recognise
// (e.g. isa.OpMapIndexOkIntGeneral).
// Takes targetOpcode (opcode) which is the fused replacement opcode (e.g.
// isa.OpMapIndexOkJumpIfFalseIntGeneral).
//
// Returns true if the pattern matched and was applied.
func fuseMapIndexOkJumpFalse(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
	sourceOpcode isa.Opcode, targetOpcode isa.Opcode,
) bool {
	if i+program.ThreeInstructionWindow > n {
		return false
	}
	if body[i].Op != sourceOpcode {
		return false
	}
	if body[i+2].Op != isa.OpJumpIfFalse {
		return false
	}
	if jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	okReg := body[i+1].A
	if body[i+2].A != okReg {
		return false
	}
	origOffset := body[i+2].SignedOffset()
	packedOffset := int32(origOffset) + 1
	if packedOffset > math.MaxInt16 || packedOffset < math.MinInt16 {
		return false
	}
	offLo, offHi := isa.SplitOffset(int16(packedOffset))
	body[i] = isa.NewInstruction(targetOpcode, body[i].A, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExt, okReg, offLo, offHi)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// countIncomingJumps counts branches targeting targetPC, excluding the instruction at
// sourcePC.
//
// Used by fuseRangeCheckUintJumpFalse to verify that the chained jump from the inner
// JumpIfFalse to the outer JumpIfFalse is the ONLY entry into the outer JumpIfFalse, so
// collapsing both into a single fused op is safe even when the outer position appears in
// jumpTargets.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes targetPC (int) which is the destination index to count entries for.
// Takes sourcePC (int) which is the index of the instruction whose jump should NOT be
// counted (the legitimate intra-pattern chain).
//
// Returns the count of jumps from any other instruction targeting targetPC.
func countIncomingJumps(_ *program.CompiledFunction, body []isa.Instruction, targetPC, sourcePC int) int {
	count := 0
	for j := range body {
		if j == sourcePC {
			continue
		}
		if target, ok := program.JumpTargetAt(body, j); ok && target == targetPC {
			count++
		}
	}
	return count
}

// matchSmallUintConstLoad recognises both forms of a uint constant load that fits in a
// byte immediate.
//
// Recognises the tier-0 isa.OpLoadUintConst with a pool reference whose value is in
// 0..math.MaxUint8, and the post-rewrite tier-1 isa.SubOpLoadUintConstSmall with the
// immediate already inlined. Callers use this to fuse patterns that read a small uint
// constant regardless of whether OptimiseLoadUintConst has already rewritten the load
// instruction.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes instr (instruction) which is the candidate load instruction to inspect.
//
// Returns the 8-bit immediate value when matched, the load destination when matched, and
// true when instr is a recognised small-uint-constant load.
func matchSmallUintConstLoad(compiledFunction *program.CompiledFunction, instr isa.Instruction) (immediate, destinationRegister uint8, ok bool) {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadUintConstSmall) {
		return instr.C, instr.B, true
	}
	if instr.Op != isa.OpLoadUintConst || instr.C != 0 {
		return 0, 0, false
	}
	if int(instr.B) >= len(compiledFunction.UintConstants) {
		return 0, 0, false
	}
	value := compiledFunction.UintConstants[instr.B]
	if value > math.MaxUint8 {
		return 0, 0, false
	}
	return uint8(value), instr.A, true
}

// fuseIntArithConst fuses a LoadIntConst followed by AddInt, SubInt, or MulInt into the
// corresponding *IntConst superinstruction.
//
// The load must write the same register the arith op reads as its right operand, and the
// slot immediately after the load must not be a jump target.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the current index.
// Takes n (int) which is the length of body.
// Takes jumpTargets (map[int]bool) which marks protected jump destinations.
//
// Returns true when the pattern matched and was rewritten in place.
func fuseIntArithConst(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		body[i].Op != isa.OpLoadIntConst || body[i].C != 0 ||
		jumpTargets[i+1] {
		return false
	}
	next := body[i+1]
	if body[i].A != next.C {
		return false
	}
	var fusedOp isa.Opcode
	switch next.Op {
	case isa.OpSubInt:
		fusedOp = isa.OpSubIntConst
	case isa.OpAddInt:
		fusedOp = isa.OpAddIntConst
	case isa.OpMulInt:
		fusedOp = isa.OpMulIntConst
	default:
		return false
	}
	if !loadedRegisterConsumedOnlyByFusedOp(compiledFunction, body, i, isa.RegisterInt, jumpTargets) {
		return false
	}
	body[i] = isa.NewInstruction(fusedOp, next.A, next.B, body[i].B)
	body[i+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// loadedRegisterConsumedOnlyByFusedOp reports whether the register written by the
// constant load at i is consumed solely as the right operand of the instruction at i+1.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the index of the constant load.
// Takes kind (isa.RegisterKind) which is the bank of the loaded register.
// Takes jumpTargets (map[int]bool) which marks protected jump destinations.
//
// Returns true when folding the load into body[i+1] preserves every later read.
// moveScanContinue means keep scanning, so it is expressed by falling out of the switch
// into the next loop iteration rather than by an arm.
func loadedRegisterConsumedOnlyByFusedOp(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, kind isa.RegisterKind, jumpTargets map[int]bool) bool {
	loaded := body[i].A
	next := body[i+1]
	if next.B == loaded {
		return false
	}
	if next.A == loaded {
		return true
	}
	if int(loaded) < returnSlotCount(compiledFunction, kind) {
		return false
	}
	candidate := MoveCandidate{Kind: kind, Destination: loaded, Source: loaded}
	limit := min(i+2+moveElimScanWindow, len(body))
	for k := i + 2; k < limit; k++ {
		if jumpTargets[k] {
			return false
		}
		if program.IsReturnInstruction(body[k]) {
			return true
		}
		switch classifyDeadPathStep(body[k], candidate) {
		case moveScanEliminate:
			return true
		case moveScanBail:
			return false
		default:
		}
	}
	return limit == len(body)
}

// fuseUintArithConst fuses a LoadUintConst followed by AddUint, SubUint, or BitAndUint
// into a tier-1 subop carrying the constant via a trailing isa.OpExt word.
//
// The uint constant pool index can be up to 16 bits, so it does not fit in the tier-1
// operand budget alone; the isa.OpExt word holds the full index. The load's destination
// must match the arith op's right operand register.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the current index.
// Takes n (int) which is the length of body.
// Takes jumpTargets (map[int]bool) which marks protected jump destinations.
//
// Returns true when the pattern matched and was rewritten in place.
func fuseUintArithConst(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		body[i].Op != isa.OpLoadUintConst ||
		jumpTargets[i+1] {
		return false
	}
	load := body[i]
	next := body[i+1]
	if load.A != next.C {
		return false
	}
	var fusedSubOp isa.SubOpcode
	switch next.Op {
	case isa.OpAddUint:
		fusedSubOp = isa.SubOpAddUintConst
	case isa.OpSubUint:
		fusedSubOp = isa.SubOpSubUintConst
	case isa.OpBitAndUint:
		fusedSubOp = isa.SubOpBitAndUintConst
	default:
		return false
	}
	if !loadedRegisterConsumedOnlyByFusedOp(compiledFunction, body, i, isa.RegisterUint, jumpTargets) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(fusedSubOp), next.A, next.B)
	body[i+1] = isa.NewInstruction(isa.OpExt, load.B, load.C, 0)
	return true
}

// matchRangeCheckRegisters verifies the per-instruction register references inside the
// window agree with the canonical pattern and returns the inferred valueReg and condReg.
//
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the window start.
// Takes vRegLo (uint8) which is the low-bound load destination.
// Takes vRegHi (uint8) which is the high-bound load destination.
//
// Returns valueReg (uint8) which is the uint register holding the value being classified.
// Returns condReg (uint8) which is the int register propagated through the move and jump
// pair.
// Returns ok (bool) which is true when every register reference matches.
func matchRangeCheckRegisters(body []isa.Instruction, i int, vRegLo, vRegHi uint8) (valueReg, condReg uint8, ok bool) {
	if body[i+1].C != vRegLo {
		return 0, 0, false
	}
	cmpLo := body[i+1].A
	valueReg = body[i+1].B
	if body[i+2].C != cmpLo {
		return 0, 0, false
	}
	condReg = body[i+2].B
	if body[i+program.RangeCheckFirstJumpOffset].A != condReg {
		return 0, 0, false
	}
	if body[i+program.RangeCheckSecondLoadOffset+1].B != valueReg || body[i+program.RangeCheckSecondLoadOffset+1].C != vRegHi {
		return 0, 0, false
	}
	cmpHi := body[i+program.RangeCheckSecondLoadOffset+1].A
	if body[i+program.RangeCheckSecondMoveOffset].C != cmpHi || body[i+program.RangeCheckSecondMoveOffset].B != condReg {
		return 0, 0, false
	}
	if body[i+program.RangeCheckSecondJumpOffset].A != condReg {
		return 0, 0, false
	}
	return valueReg, condReg, true
}

// validateRangeCheckOffsets ensures the first jump chains to the second jump's position
// and the second jump's offset is non-negative (forward only).
//
// Takes body ([]isa.Instruction) which is the instruction sequence.
// Takes i (int) which is the window start.
//
// Returns int16 which is the second jump's signed offset that the rewrite encodes into
// the fused extension word.
// Returns bool which is true when both offsets are well-formed.
func validateRangeCheckOffsets(body []isa.Instruction, i int) (int16, bool) {
	if body[i+program.RangeCheckFirstJumpOffset].SignedOffset() != program.RangeCheckFirstJumpDelta {
		return 0, false
	}
	off2 := body[i+program.RangeCheckSecondJumpOffset].SignedOffset()
	if off2 < 0 {
		return 0, false
	}
	return off2, true
}

// tryFuseTier0AppendMove handles the contiguous isa.OpAppend + isa.OpMoveGeneral pattern.
// Returns true when the fusion fires.
//
// Takes body ([]instruction) which is the instruction sequence to rewrite.
// Takes i (int) which is the isa.OpAppend index.
// Takes appendInstr (instruction) which is body[i].
//
// Returns true when the fusion fires.
func tryFuseTier0AppendMove(body []isa.Instruction, i int, appendInstr isa.Instruction) bool {
	moveInstr := body[i+1]
	if moveInstr.Op != isa.OpMoveGeneral {
		return false
	}
	if moveInstr.A != appendInstr.B || moveInstr.B != appendInstr.A {
		return false
	}
	body[i] = isa.NewInstruction(appendInstr.Op, appendInstr.B, appendInstr.B, appendInstr.C)
	body[i+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// tryFuseTier1AppendMove handles the DRILL_TIER1(subOpAppend*) + EXT + MOVE_GEN pattern.
// The MOVE_GEN lives at body[i+2] (after the extension word) and the rewrite rebrands the
// append destination so the runtime adapter takes the in-place helper path.
//
// Takes body ([]instruction) which is the instruction sequence to rewrite.
// Takes i (int) which is the isa.OpDrillTier1 index.
// Takes n (int) which is the body length.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes appendInstr (instruction) which is body[i].
//
// Returns true when the fusion fires.
func tryFuseTier1AppendMove(body []isa.Instruction, i, n int, jumpTargets map[int]bool, appendInstr isa.Instruction) bool {
	if appendInstr.Op != isa.OpDrillTier1 {
		return false
	}
	if !isTier1AppendSubOp(isa.SubOpcode(appendInstr.A)) {
		return false
	}
	if i+2 >= n || jumpTargets[i+2] {
		return false
	}
	if body[i+1].Op != isa.OpExt {
		return false
	}
	moveInstr := body[i+2]
	if moveInstr.Op != isa.OpMoveGeneral {
		return false
	}
	if moveInstr.A != appendInstr.C || moveInstr.B != appendInstr.B {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpDrillTier1, appendInstr.A, appendInstr.C, appendInstr.C)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// isTier1AppendSubOp reports whether sub is one of the typed subOpAppend* sub-opcodes
// that this fusion recognises.
//
// Takes sub (isa.SubOpcode) which is the candidate sub-opcode.
//
// Returns true when sub is in the supported append family.
func isTier1AppendSubOp(sub isa.SubOpcode) bool {
	switch sub {
	case isa.SubOpAppendInt, isa.SubOpAppendString, isa.SubOpAppendFloat, isa.SubOpAppendBool, isa.SubOpAppendUint:
		return true
	default:
	}
	return false
}

// isCanonicalNop reports whether instr is the tier-3 canonical NOP encoding emitted by
// the peephole passes.
//
// The canonical NOP is the tier-3 form {isa.OpDrillTier1, isa.SubOpDrillTier2,
// isa.SubOpTier2DrillTier3, isa.SubOpTier3Nop} rather than an all-zero word so it
// dispatches deterministically through the drill cascade.
//
// Takes instr (instruction) which is the instruction to test.
//
// Returns true when instr is the canonical NOP encoding.
func isCanonicalNop(instr isa.Instruction) bool {
	return instr.Op == isa.OpDrillTier1 &&
		isa.SubOpcode(instr.A) == isa.SubOpDrillTier2 &&
		isa.SubOpcodeTier2(instr.B) == isa.SubOpTier2DrillTier3 &&
		isa.SubOpcodeTier3(instr.C) == isa.SubOpTier3Nop
}
