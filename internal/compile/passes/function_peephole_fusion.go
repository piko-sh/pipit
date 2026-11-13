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
	"slices"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// rangeCheckShape carries the per-bound immediate and destination register decoded from
// the fuseInRangeUint64ThenJumpIfFalse window.
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
	return fuseCompareConstJumps(compiledFunction, body, i, n, jumpTargets) ||
		fuseStringConstJump(compiledFunction, body, i, n, jumpTargets) ||
		fuseNilTestJump(compiledFunction, body, i, n, jumpTargets) ||
		fuseIncrementInt64ThenJumpIfLessThan(compiledFunction, body, i, n, jumpTargets) ||
		fuseLessThanLengthStringThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets) ||
		fuseEqualUint64ImmediateThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapInt64Int64Ok, isa.OpGetMapInt64Int64OkThenJumpIfFalse) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapStringInt64Ok, isa.OpGetMapStringInt64OkThenJumpIfFalse) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapStringStringOk, isa.OpGetMapStringStringOkThenJumpIfFalse) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapInt64StringOk, isa.OpGetMapInt64StringOkThenJumpIfFalse) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapInt64GeneralOk, isa.OpGetMapInt64GeneralOkThenJumpIfFalse) ||
		fuseGetMapOkThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets, isa.OpGetMapStringGeneralOk, isa.OpGetMapStringGeneralOkThenJumpIfFalse)
}

// fuseCompareConstJumps tries each compare-against-constant fusion at i.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseCompareConstJumps(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	for _, fusion := range compareJumpFusions {
		if fuseCompareConstJump(compiledFunction, body, i, n, jumpTargets, fusion) {
			return true
		}
	}
	return false
}

// FuseArithConst fuses LoadInt64Constant or LoadUint64Constant with an arithmetic op into
// a constant-operand superinstruction.
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

// FuseAddInt64ConstantThenJump fuses AddInt64Constant + Jump into
// isa.OpAddInt64ConstantThenJump + isa.OpExtension.
//
// The offset is carried over unchanged. A jump's offset counts from the word after it,
// and the fused pair's counts from the word after its extension word, which is the same
// word.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseAddInt64ConstantThenJump(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		body[i].Op != isa.OpAddInt64Constant || !isa.InstrIsTier1SubOp(body[i+1], isa.SubOpJump) ||
		jumpTargets[i+1] {
		return false
	}
	lo, hi := isa.SplitOffset(body[i+1].SignedOffset())
	body[i] = isa.NewInstruction(isa.OpAddInt64ConstantThenJump, body[i].A, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	return true
}

// FuseConcatRune fuses RuneToString + ConcatenateString into isa.OpConcatenateStringRune.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseConcatRune(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n ||
		!isa.InstrIsTier1SubOp(body[i], isa.SubOpConvertRuneToString) || body[i+1].Op != isa.OpConcatenateString ||
		body[i].B != body[i+1].C ||
		body[i].B == body[i+1].B ||
		jumpTargets[i+1] {
		return false
	}
	if !writeDroppedSafely(compiledFunction, body, i+2, isa.RegisterString, body[i].B, body[i+1].A) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpConcatenateStringRune, body[i+1].A, body[i+1].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// FuseAppendMove fuses APPEND_xxx + MOVE_GENERAL into an in-place APPEND_xxx where the
// destination equals the source-slice register, so the handler takes the in-place fast
// path instead of wrapping a fresh reflect.Value.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseAppendMove(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+1 >= n || jumpTargets[i+1] {
		return false
	}
	appendInstr := body[i]
	if appendInstr.Op == isa.OpAppend {
		return tryFuseTier0AppendMove(compiledFunction, body, i, appendInstr)
	}
	return tryFuseTier1AppendMove(compiledFunction, body, i, n, jumpTargets, appendInstr)
}

// OptimiseLoadInt64Constant rewrites LoadInt64Constant to LoadIntConstSmall when the
// constant value fits in [0, 255].
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current instruction index.
func OptimiseLoadInt64Constant(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int) {
	if body[i].Op != isa.OpLoadInt64Constant || body[i].C != 0 {
		return
	}
	if int(body[i].B) >= len(compiledFunction.IntConstants) {
		return
	}
	value := compiledFunction.IntConstants[body[i].B]
	if value >= 0 && value <= program.MaxSmallConstant {
		body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLoadInt64Immediate), body[i].A, safeconv.MustIntToUint8(int(value)))
	}
}

// OptimiseLoadUint64Constant rewrites LoadUint64Constant to LoadUintConstSmall when the
// constant fits in the 8-bit inline immediate slot.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence to scan and rewrite in
// place.
// Takes i (int) which is the index of the candidate instruction.
func OptimiseLoadUint64Constant(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int) {
	if body[i].Op != isa.OpLoadUint64Constant || body[i].C != 0 {
		return
	}
	if int(body[i].B) >= len(compiledFunction.UintConstants) {
		return
	}
	value := compiledFunction.UintConstants[body[i].B]
	if value <= uint64(program.MaxSmallConstant) {
		body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), body[i].A, safeconv.MustIntToUint8(int(value)))
	}
}

// FuseGetStringByteAsInt64 fuses isa.OpGetStringByte + isa.SubOpConvertUint64ToInt64 into
// isa.OpGetStringByteAsInt64, avoiding the intermediate uint register and one tier-2
// trampoline per string byte access converted to int.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseGetStringByteAsInt64(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	return fuseUnsignedReadIntoConversion(compiledFunction, body, i, n, jumpTargets, isa.OpGetStringByte, isa.OpGetStringByteAsInt64)
}

// FuseGetElementUint64AsInt64 combines an unsigned element read with conversion to int.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseGetElementUint64AsInt64(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	return fuseUnsignedReadIntoConversion(compiledFunction, body, i, n, jumpTargets, isa.OpGetElementUint64, isa.OpGetElementInt64)
}

// fuseUnsignedReadIntoConversion combines an unsigned read and its conversion to int.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes read (isa.Opcode) which is the read into the uint bank, with its destination in
// A.
// Takes fused (isa.Opcode) which is the same read into the int bank.
//
// Returns true if the pattern was matched and applied.
func fuseUnsignedReadIntoConversion(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool, read, fused isa.Opcode,
) bool {
	if i+1 >= n ||
		body[i].Op != read || !isa.InstrIsTier1SubOp(body[i+1], isa.SubOpConvertUint64ToInt64) ||
		body[i].A != body[i+1].C ||
		jumpTargets[i+1] {
		return false
	}
	if !registerDeadFrom(compiledFunction, body, i+2, isa.RegisterUint64, body[i].A) {
		return false
	}
	body[i] = isa.NewInstruction(fused, body[i+1].B, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// SyncSourceMapAfterOptimise zeroes source positions for instructions that were replaced
// with NO_OPERATION words during optimisation, keeping the source map consistent with the
// instruction body.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which specifies the instruction sequence to synchronise.
func SyncSourceMapAfterOptimise(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if compiledFunction.DebugSourceMap == nil {
		return
	}
	for i, instr := range body {
		if isCanonicalNoOperation(instr) && i < len(compiledFunction.DebugSourceMap.Positions) {
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
	return fuseInRangeUint64ThenJumpIfFalse(compiledFunction, body, i, n, jumpTargets)
}

// fuseInRangeUint64ThenJumpIfFalse fuses the canonical `value >= lo && value <= hi` byte
// classifier pattern into a single tier-2 sub-op plus two extension words, replacing the
// 8-instruction window with three instruction words and five isa.OpNoOperation padding
// slots.
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
func fuseInRangeUint64ThenJumpIfFalse(compiledFunction *program.CompiledFunction,
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
	valueReg, condReg, ok := matchRangeCheckRegisters(body, i, shape.vRegLo, shape.vRegHi)
	if !ok {
		return false
	}
	off2, ok := validateRangeCheckOffsets(body, i)
	if !ok {
		return false
	}
	if !rangeCheckDroppedWritesDead(compiledFunction, body, i, shape, valueReg, condReg) {
		return false
	}
	offLo, offHi := isa.SplitOffset(off2)
	body[i] = isa.NewTier2Instruction(isa.SubOpTier2InRangeUint64ThenJumpIfFalse, valueReg)
	body[i+1] = isa.NewInstruction(isa.OpExtension, shape.loImm, shape.hiImm, 0)
	body[i+2] = isa.NewInstruction(isa.OpExtension, offLo, offHi, 0)
	for slot := i + program.RangeCheckFirstNoOperationOffset; slot < i+program.RangeCheckWindowSize; slot++ {
		body[slot] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
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
	if body[i+1].Op != isa.OpGreaterOrEqualUint64 ||
		!isa.InstrIsTier1SubOp(body[i+2], isa.SubOpMoveInt64) ||
		body[i+program.RangeCheckFirstJumpOffset].Op != isa.OpJumpIfFalse {
		return rangeCheckShape{}, false
	}
	shape.hiImm, shape.vRegHi, ok = matchSmallUintConstLoad(compiledFunction, body[i+program.RangeCheckSecondLoadOffset])
	if !ok {
		return rangeCheckShape{}, false
	}
	if body[i+program.RangeCheckSecondLoadOffset+1].Op != isa.OpLessOrEqualUint64 ||
		!isa.InstrIsTier1SubOp(body[i+program.RangeCheckSecondMoveOffset], isa.SubOpMoveInt64) ||
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

// fuseEqualUint64ImmediateThenJumpIfFalse fuses the 3-op byte-switch dispatch pattern
// `LoadUintConstSmall + EqualUint64 + JumpIfFalse` into a single tier-1 sub-op
// (isa.SubOpEqualUint64ImmediateThenJumpIfFalse) plus one extension word and one trailing
// isa.OpNoOperation. Hits the canonical `switch byteValue` chain (brainfuck dispatch,
// tokenisers, hand-rolled lexers) where each case becomes one of these triples.
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
func fuseEqualUint64ImmediateThenJumpIfFalse(compiledFunction *program.CompiledFunction,
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
	if body[i+1].Op != isa.OpEqualUint64 || body[i+2].Op != isa.OpJumpIfFalse {
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
	if body[i+2].A != condReg || valueReg == constReg {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterUint64, constReg) ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, condReg) {
		return false
	}
	rawOffset := body[i+2].SignedOffset()
	offLo, offHi := isa.SplitOffset(rawOffset)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpEqualUint64ImmediateThenJumpIfFalse), valueReg, immVal)
	body[i+1] = isa.NewInstruction(isa.OpExtension, offLo, offHi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseGetMapOkThenJumpIfFalse fuses a typed `opMapIndexOkXXX + ext + isa.OpJumpIfFalse`
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
// (e.g. isa.OpGetMapInt64GeneralOk).
// Takes targetOpcode (opcode) which is the fused replacement opcode (e.g.
// isa.OpGetMapInt64GeneralOkThenJumpIfFalse).
//
// Returns true if the pattern matched and was applied.
func fuseGetMapOkThenJumpIfFalse(_ *program.CompiledFunction,
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
	body[i+1] = isa.NewInstruction(isa.OpExtension, okReg, offLo, offHi)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// countIncomingJumps counts branches targeting targetPC, excluding the instruction at
// sourcePC.
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
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes instr (instruction) which is the candidate load instruction to inspect.
//
// Returns the 8-bit immediate value when matched, the load destination when matched, and
// true when instr is a recognised small-uint-constant load.
func matchSmallUintConstLoad(compiledFunction *program.CompiledFunction, instr isa.Instruction) (immediate, destinationRegister uint8, ok bool) {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpLoadUint64Immediate) {
		return instr.C, instr.B, true
	}
	if instr.Op != isa.OpLoadUint64Constant || instr.C != 0 {
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

// fuseIntArithConst fuses a LoadInt64Constant or LoadIntConstSmall followed by AddInt64,
// SubtractInt64, or MultiplyInt64 into the corresponding *IntConst superinstruction.
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
	if i+1 >= n || jumpTargets[i+1] {
		return false
	}
	loaded, ok := intConstLoadDestination(body[i])
	next := body[i+1]
	if !ok || loaded != next.C {
		return false
	}
	var fusedOp isa.Opcode
	switch next.Op {
	case isa.OpSubtractInt64:
		fusedOp = isa.OpSubtractInt64Constant
	case isa.OpAddInt64:
		fusedOp = isa.OpAddInt64Constant
	case isa.OpMultiplyInt64:
		fusedOp = isa.OpMultiplyInt64Constant
	default:
		return false
	}
	if !loadedRegisterConsumedOnlyByFusedOp(compiledFunction, body, i, loaded, isa.RegisterInt64) {
		return false
	}
	index, ok := intConstPoolIndex(compiledFunction, body[i])
	if !ok {
		return false
	}
	body[i] = isa.NewInstruction(fusedOp, next.A, next.B, index)
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// intConstLoadDestination names the register an int constant load writes.
//
// Takes inst (isa.Instruction) which is the candidate load.
//
// Returns the destination and whether inst is a pool or small int constant load.
func intConstLoadDestination(inst isa.Instruction) (uint8, bool) {
	if inst.Op == isa.OpLoadInt64Constant && inst.C == 0 {
		return inst.A, true
	}
	if isa.InstrIsTier1SubOp(inst, isa.SubOpLoadInt64Immediate) {
		return inst.B, true
	}
	return 0, false
}

// intConstPoolIndex returns the 8-bit pool index an *IntConst op needs for the constant
// load inst, adding a small load's value to the pool when it is missing and there is
// room.
//
// Takes compiledFunction (*program.CompiledFunction) whose int pool is searched.
// Takes inst (isa.Instruction) which is the constant load.
//
// Returns the index and false when it does not fit in one byte.
func intConstPoolIndex(compiledFunction *program.CompiledFunction, inst isa.Instruction) (uint8, bool) {
	if inst.Op == isa.OpLoadInt64Constant {
		return inst.B, true
	}
	value := int64(inst.C)
	if index := slices.Index(compiledFunction.IntConstants, value); index >= 0 && index <= math.MaxUint8 {
		return uint8(index), true
	}
	if len(compiledFunction.IntConstants) > math.MaxUint8 {
		return 0, false
	}
	compiledFunction.IntConstants = append(slices.Clip(compiledFunction.IntConstants), value)
	compiledFunction.PrecomputedAllocCountsValid = false
	return safeconv.MustIntToUint8(len(compiledFunction.IntConstants) - 1), true
}

// uintConstPoolIndex returns the 16-bit pool index the uint *Const ops carry in their
// extension word for the constant load inst, adding a small load's value to the pool when
// it is missing.
//
// Takes compiledFunction (*program.CompiledFunction) whose uint pool is searched.
// Takes inst (isa.Instruction) which is the constant load.
//
// Returns the index and false when it does not fit in 16 bits.
func uintConstPoolIndex(compiledFunction *program.CompiledFunction, inst isa.Instruction) (uint16, bool) {
	if inst.Op == isa.OpLoadUint64Constant {
		return isa.JoinWide(inst.B, inst.C), true
	}
	value := uint64(inst.C)
	if index := slices.Index(compiledFunction.UintConstants, value); index >= 0 && index <= math.MaxUint16 {
		return uint16(index), true
	}
	if len(compiledFunction.UintConstants) > math.MaxUint16 {
		return 0, false
	}
	compiledFunction.UintConstants = append(slices.Clip(compiledFunction.UintConstants), value)
	compiledFunction.PrecomputedAllocCountsValid = false
	return safeconv.MustIntToUint16(len(compiledFunction.UintConstants) - 1), true
}

// loadedRegisterConsumedOnlyByFusedOp reports whether the register written by the
// constant load at i is consumed solely as the right operand of the instruction at i+1,
// following every path after it. A register holding a named variable stays written, so
// the debugger still sees the variable's value.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the index of the constant load.
// Takes loaded (uint8) which is the register the load writes.
// Takes kind (isa.RegisterKind) which is the bank of the loaded register.
//
// Returns true when folding the load into body[i+1] preserves every later read.
func loadedRegisterConsumedOnlyByFusedOp(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, loaded uint8, kind isa.RegisterKind) bool {
	next := body[i+1]
	if next.B == loaded {
		return false
	}
	if next.A == loaded {
		return true
	}
	return int(loaded) >= returnSlotCount(compiledFunction, kind) &&
		!namesDebugVariable(compiledFunction, kind, loaded, i, i+1) &&
		registerDeadFrom(compiledFunction, body, i+2, kind, loaded)
}

// fuseUintArithConst fuses a LoadUint64Constant or LoadUintConstSmall followed by
// AddUint64, SubtractUint64, or BitAndUint64 into a tier-1 subop carrying the constant
// via a trailing isa.OpExtension word.
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
	if i+1 >= n || jumpTargets[i+1] {
		return false
	}
	_, loaded, ok := matchSmallUintConstLoad(compiledFunction, body[i])
	if body[i].Op == isa.OpLoadUint64Constant {
		loaded, ok = body[i].A, true
	}
	next := body[i+1]
	if !ok || loaded != next.C {
		return false
	}
	var fusedSubOp isa.SubOpcode
	switch next.Op {
	case isa.OpAddUint64:
		fusedSubOp = isa.SubOpAddUint64Constant
	case isa.OpSubtractUint64:
		fusedSubOp = isa.SubOpSubtractUint64Constant
	case isa.OpBitAndUint64:
		fusedSubOp = isa.SubOpBitAndUint64Constant
	default:
		return false
	}
	if !loadedRegisterConsumedOnlyByFusedOp(compiledFunction, body, i, loaded, isa.RegisterUint64) {
		return false
	}
	index, ok := uintConstPoolIndex(compiledFunction, body[i])
	if !ok {
		return false
	}
	lo, hi := isa.SplitWide(index)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(fusedSubOp), next.A, next.B)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
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

// rangeCheckDroppedWritesDead reports whether the range-check fusion may stop writing the
// five registers the unfused window writes: the two loaded bounds, the two comparison
// results and the shared condition.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction sequence, before the rewrite.
// Takes i (int) which is the window start.
// Takes shape (rangeCheckShape) which holds the bound registers.
// Takes valueReg (uint8) which is the uint register being classified.
// Takes condReg (uint8) which is the int condition register.
//
// Returns true when none of the dropped writes can be observed.
func rangeCheckDroppedWritesDead(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, shape rangeCheckShape, valueReg, condReg uint8) bool {
	if valueReg == shape.vRegLo || valueReg == shape.vRegHi {
		return false
	}
	branchPC := i + program.RangeCheckSecondJumpOffset
	dropped := []struct {
		kind isa.RegisterKind
		reg  uint8
	}{
		{kind: isa.RegisterUint64, reg: shape.vRegLo},
		{kind: isa.RegisterUint64, reg: shape.vRegHi},
		{kind: isa.RegisterInt64, reg: body[i+1].A},
		{kind: isa.RegisterInt64, reg: body[i+program.RangeCheckSecondLoadOffset+1].A},
		{kind: isa.RegisterInt64, reg: condReg},
	}
	for _, write := range dropped {
		if !registerDeadAfterBranch(compiledFunction, body, branchPC, write.kind, write.reg) {
			return false
		}
	}
	return true
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
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which is the instruction sequence to rewrite.
// Takes i (int) which is the isa.OpAppend index.
// Takes appendInstr (instruction) which is body[i].
//
// Returns true when the fusion fires.
func tryFuseTier0AppendMove(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, appendInstr isa.Instruction) bool {
	moveInstr := body[i+1]
	if moveInstr.Op != isa.OpMoveGeneral {
		return false
	}
	if moveInstr.A != appendInstr.B || moveInstr.B != appendInstr.A {
		return false
	}
	if !writeDroppedSafely(compiledFunction, body, i+2, isa.RegisterGeneral, appendInstr.A, appendInstr.B) {
		return false
	}
	body[i] = isa.NewInstruction(appendInstr.Op, appendInstr.B, appendInstr.B, appendInstr.C)
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// tryFuseTier1AppendMove handles the DRILL_TIER1(subOpAppend*) + EXTENSION + MOVE_GEN
// pattern. The MOVE_GEN lives at body[i+2] (after the extension word) and the rewrite
// rebrands the append destination so the runtime adapter takes the in-place helper path.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]instruction) which is the instruction sequence to rewrite.
// Takes i (int) which is the isa.OpDrillTier1 index.
// Takes n (int) which is the body length.
// Takes jumpTargets (map[int]bool) which marks branch destinations.
// Takes appendInstr (instruction) which is body[i].
//
// Returns true when the fusion fires.
func tryFuseTier1AppendMove(compiledFunction *program.CompiledFunction, body []isa.Instruction, i, n int, jumpTargets map[int]bool, appendInstr isa.Instruction) bool {
	if appendInstr.Op != isa.OpDrillTier1 {
		return false
	}
	if !isTier1AppendSubOp(isa.SubOpcode(appendInstr.A)) {
		return false
	}
	if i+2 >= n || jumpTargets[i+2] {
		return false
	}
	if body[i+1].Op != isa.OpExtension {
		return false
	}
	moveInstr := body[i+2]
	if moveInstr.Op != isa.OpMoveGeneral {
		return false
	}
	if moveInstr.A != appendInstr.C || moveInstr.B != appendInstr.B {
		return false
	}
	if !writeDroppedSafely(compiledFunction, body, i+3, isa.RegisterGeneral, appendInstr.B, appendInstr.C) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpDrillTier1, appendInstr.A, appendInstr.C, appendInstr.C)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
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
	case isa.SubOpAppendInt64Wide, isa.SubOpAppendStringWide, isa.SubOpAppendFloat64Wide, isa.SubOpAppendBoolWide, isa.SubOpAppendUint64Wide:
		return true
	default:
	}
	return false
}

// isCanonicalNoOperation reports whether instr is the tier-3 canonical NO_OPERATION
// encoding emitted by the peephole passes.
//
// The canonical NO_OPERATION is the tier-3 form {isa.OpDrillTier1, isa.SubOpDrillTier2,
// isa.SubOpTier2DrillTier3, isa.SubOpTier3NoOperation} rather than an all-zero word so it
// dispatches deterministically through the drill cascade.
//
// Takes instr (instruction) which is the instruction to test.
//
// Returns true when instr is the canonical NO_OPERATION encoding.
func isCanonicalNoOperation(instr isa.Instruction) bool {
	return instr.Op == isa.OpDrillTier1 &&
		isa.SubOpcode(instr.A) == isa.SubOpDrillTier2 &&
		isa.SubOpcodeTier2(instr.B) == isa.SubOpTier2DrillTier3 &&
		isa.SubOpcodeTier3(instr.C) == isa.SubOpTier3NoOperation
}
