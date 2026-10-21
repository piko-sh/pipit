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

const (
	// minTruncateAfterBitAndWindow is the minimum body length for which
	// elideRedundantTruncateAfterBitAnd has a complete match window (BIT_AND_UINT64_CONSTANT
	// + EXTENSION + TRUNCATE_TO_WIDTH).
	minTruncateAfterBitAndWindow = 3

	// minBitAndAfterTruncateWindow is the minimum body length for which
	// elideRedundantBitAndAfterTruncate has a complete match window (TRUNCATE_TO_WIDTH +
	// BIT_AND_UINT64_CONSTANT + EXTENSION + MOVE_UINT64).
	minBitAndAfterTruncateWindow = 4

	// truncateToWidth32 is the bit width passed to isa.OpTruncateToWidth to denote "truncate
	// to 32 bits". The fast-mask peepholes only fire for this width.
	truncateToWidth32 = 32

	// uint32Mask is the 32-bit all-ones constant the fast-mask peepholes expect to find in
	// the uintConstants pool.
	uint32Mask = 0xFFFFFFFF
)

// uintRegReadDecision is the per-instruction verdict from classifyUintRegRead.
type uintRegReadDecision uint8

const (
	// uintRegReadContinue means the instruction does not read reg and the scan should
	// advance.
	uintRegReadContinue uintRegReadDecision = iota

	// uintRegReadSafe means the instruction is an end-of-scan boundary (function return) and
	// the elision is safe.
	uintRegReadSafe

	// uintRegReadEscapes means the instruction may read reg and the elision must be refused.
	uintRegReadEscapes
)

// elideRedundantTruncateAfterBitAnd nops a TRUNCATE_TO_WIDTH that follows a BIT_AND_INT64
// with a 32-bit mask, because the BIT_AND_INT64 already clears every bit above 32.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence to scan and rewrite in
// place.
func elideRedundantTruncateAfterBitAnd(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if len(body) < minTruncateAfterBitAndWindow {
		return
	}
	jumpTargets := compiledFunction.BuildJumpTargets(body)
	for i := 0; i+2 < len(body); i++ {
		if !matchesTruncateAfterBitAnd(compiledFunction, body, i, jumpTargets) {
			continue
		}
		body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
}

// matchesTruncateAfterBitAnd reports whether the three-instruction window starting at i
// is the redundant BIT_AND_UINT64_CONSTANT(0xFFFFFFFF) + EXTENSION +
// TRUNCATE_TO_WIDTH(32) pattern safe to collapse.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the candidate start index.
// Takes jumpTargets (map[int]bool) which marks branch destinations whose PCs must not be
// rewritten.
//
// Returns true when every match condition holds for this window.
func matchesTruncateAfterBitAnd(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if body[i].Op != isa.OpDrillTier1 || isa.SubOpcode(body[i].A) != isa.SubOpBitAndUint64Constant {
		return false
	}
	if body[i+1].Op != isa.OpExtension {
		return false
	}
	if body[i+2].Op != isa.OpTruncateToWidth {
		return false
	}
	if body[i+2].A != body[i].B {
		return false
	}
	if body[i+2].B != truncateToWidth32 {
		return false
	}
	constantIndex := int(body[i+1].A)
	if constantIndex >= len(compiledFunction.UintConstants) || compiledFunction.UintConstants[constantIndex] != uint32Mask {
		return false
	}
	return !jumpTargets[i+2]
}

// elideRedundantBitAndAfterTruncate folds out a BIT_AND_INT64(0xFFFFFFFF) that follows a
// TRUNCATE_TO_WIDTH(32), rewriting the trailing MOVE source and replacing the
// BIT_AND_INT64 + EXTENSION pair with isa.OpNoOperation.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence to scan and rewrite in
// place.
func elideRedundantBitAndAfterTruncate(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	if len(body) < minBitAndAfterTruncateWindow {
		return
	}
	jumpTargets := compiledFunction.BuildJumpTargets(body)
	for i := 0; i+minTruncateAfterBitAndWindow < len(body); i++ {
		x, _, moveIndex, ok := matchBitAndAfterTruncate(compiledFunction, body, i, jumpTargets)
		if !ok {
			continue
		}
		body[moveIndex].C = x
		body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
		body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
}

// matchBitAndAfterTruncate reports whether the window starting at i matches the
// TRUNCATE_TO_WIDTH(X, 32) + BIT_AND_UINT64_CONSTANT(Y, X, mask) + EXTENSION +
// (isa.OpNoOperation)* + MOVE_UINT64(Z, Y) collapse pattern.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// optimised.
// Takes body ([]instruction) which is the instruction sequence.
// Takes i (int) which is the candidate start index.
// Takes jumpTargets (map[int]bool) which marks branch destinations whose PCs must not be
// rewritten.
//
// Returns x (the truncate destination retained as the MOVE source).
// Returns y (the BIT_AND_INT64 destination scheduled for elision).
// Returns moveIndex (the index of the trailing MOVE_UINT64 to rewrite).
// Returns ok (true when every match condition holds).
func matchBitAndAfterTruncate(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) (x, y uint8, moveIndex int, ok bool) {
	if body[i].Op != isa.OpTruncateToWidth || body[i].B != truncateToWidth32 {
		return 0, 0, 0, false
	}
	x = body[i].A
	if body[i+1].Op != isa.OpDrillTier1 || isa.SubOpcode(body[i+1].A) != isa.SubOpBitAndUint64Constant {
		return 0, 0, 0, false
	}
	if body[i+1].C != x {
		return 0, 0, 0, false
	}
	y = body[i+1].B
	if body[i+2].Op != isa.OpExtension {
		return 0, 0, 0, false
	}
	constantIndex := int(body[i+2].A)
	if constantIndex >= len(compiledFunction.UintConstants) || compiledFunction.UintConstants[constantIndex] != uint32Mask {
		return 0, 0, 0, false
	}
	moveIndex = skipTrailingNops(body, i+minTruncateAfterBitAndWindow)
	if moveIndex >= len(body) {
		return 0, 0, 0, false
	}
	if body[moveIndex].Op != isa.OpDrillTier1 || isa.SubOpcode(body[moveIndex].A) != isa.SubOpMoveUint64 {
		return 0, 0, 0, false
	}
	if body[moveIndex].C != y {
		return 0, 0, 0, false
	}
	if jumpTargets[i+1] || jumpTargets[i+2] || jumpTargets[moveIndex] {
		return 0, 0, 0, false
	}
	if uintRegReadAfter(compiledFunction, body, moveIndex+1, y, jumpTargets) {
		return 0, 0, 0, false
	}
	return x, y, moveIndex, true
}

// uintRegReadAfter reports whether any instruction at or after startIndex reads the given
// uint-bank register.
//
// Conservative straight-line scan that returns true at a jump target inside the window,
// because a predecessor outside the linear range could observe reg.
//
// Takes body ([]instruction) which is the instruction sequence to scan.
// Takes startIndex (int) which is the first index to inspect.
// Takes reg (uint8) which is the uint-bank register to check for reads.
// Takes jumpTargets (map[int]bool) which marks every branch destination in the function;
// a target inside the window forces a conservative "may read" verdict.
//
// Returns true when any subsequent instruction may read reg.
func uintRegReadAfter(_ *program.CompiledFunction, body []isa.Instruction, startIndex int, reg uint8, jumpTargets map[int]bool) bool {
	for j := startIndex; j < len(body); j++ {
		if jumpTargets[j] {
			return true
		}
		decision := classifyUintRegRead(body[j], reg)
		if decision == uintRegReadContinue {
			continue
		}
		return decision == uintRegReadEscapes
	}
	return false
}

// skipTrailingNops advances past any isa.OpNoOperation placeholders (encoded as
// isa.OpDrillTier1 with all-zero operands) starting at startIndex.
//
// Takes body ([]instruction) which is the instruction sequence.
// Takes startIndex (int) which is the first index to inspect.
//
// Returns the first index at or after startIndex that is not isa.OpNoOperation.
func skipTrailingNops(body []isa.Instruction, startIndex int) int {
	moveIndex := startIndex
	for moveIndex < len(body) && body[moveIndex].Op == isa.OpDrillTier1 &&
		body[moveIndex].A == 0 && body[moveIndex].B == 0 && body[moveIndex].C == 0 {
		moveIndex++
	}
	return moveIndex
}

// classifyUintRegRead returns the per-instruction decision for the "is reg read after
// this point?" scan. False positives block the peephole, while false negatives would
// corrupt the rewrite.
//
// Takes instr (instruction) which is the candidate instruction.
// Takes reg (uint8) which is the uint-bank register to check for reads.
//
// Returns the decision verdict.
func classifyUintRegRead(instr isa.Instruction, reg uint8) uintRegReadDecision {
	if instr.Op == isa.OpDrillTier1 && instr.A == 0 && instr.B == 0 && instr.C == 0 {
		return uintRegReadContinue
	}
	switch instr.Op {
	case isa.OpAddUint64, isa.OpSubtractUint64, isa.OpMultiplyUint64, isa.OpEqualUint64, isa.OpGreaterOrEqualUint64, isa.OpLessOrEqualUint64:
		if instr.B == reg || instr.C == reg {
			return uintRegReadEscapes
		}
		return uintRegReadContinue
	case isa.OpTruncateToWidth:
		if instr.A == reg {
			return uintRegReadEscapes
		}
		return uintRegReadContinue
	case isa.OpDrillTier1:
		return classifyUintRegReadTier1(instr, reg)
	case isa.OpExtension:
		return uintRegReadEscapes
	default:
	}
	return uintRegReadEscapes
}

// classifyUintRegReadTier1 specialises classifyUintRegRead for isa.OpDrillTier1
// instructions whose sub-opcode lives in instr.a.
//
// Takes instr (instruction) which is the isa.OpDrillTier1 wrapper.
// Takes reg (uint8) which is the uint-bank register to check.
//
// Returns the decision verdict for this sub-op.
func classifyUintRegReadTier1(instr isa.Instruction, reg uint8) uintRegReadDecision {
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpMoveUint64, isa.SubOpBitAndUint64Constant, isa.SubOpAddUint64Constant, isa.SubOpSubtractUint64Constant:
		if instr.C == reg {
			return uintRegReadEscapes
		}
		return uintRegReadContinue
	case isa.SubOpLoadUint64Immediate:
		return uintRegReadContinue
	case isa.SubOpDrillTier2:
		if isa.SubOpcodeTier2(instr.B) == isa.SubOpTier2Return {
			return uintRegReadSafe
		}
		return uintRegReadEscapes
	default:
	}
	return uintRegReadEscapes
}
