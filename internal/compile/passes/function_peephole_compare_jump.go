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

// registerCompareJumpFusion names the fusion of a comparison of two registers with the
// branch that follows it.
type registerCompareJumpFusion struct {
	// compare is the tier-0 comparison opcode to match.
	compare isa.Opcode

	// jump is the tier-0 conditional branch opcode to match.
	jump isa.Opcode

	// register is the fused tier-1 sub-op for two register operands.
	register isa.SubOpcode
}

// compareJumpFusion names a comparison-and-branch fusion that also has a form for a
// constant right operand.
type compareJumpFusion struct {
	registerCompareJumpFusion

	// constant is the fused tier-1 sub-op for a constant right operand.
	constant isa.SubOpcode
}

// compareJumpFusions lists the compare-and-branch fusions.
//
// An integer comparison negates exactly, so a compare followed by a branch on true is the
// opposite compare followed by a branch on false. The constant forms have no not-equal
// sub-op, so they branch on true through the equal one.
var compareJumpFusions = [...]compareJumpFusion{
	{registerCompareJumpFusion{isa.OpLessThanInt64, isa.OpJumpIfFalse, isa.SubOpLessThanInt64ThenJumpIfFalse}, isa.SubOpLessThanInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpLessOrEqualInt64, isa.OpJumpIfFalse, isa.SubOpLessOrEqualInt64ThenJumpIfFalse}, isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpGreaterThanInt64, isa.OpJumpIfFalse, isa.SubOpGreaterThanInt64ThenJumpIfFalse}, isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpGreaterOrEqualInt64, isa.OpJumpIfFalse, isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse}, isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpEqualInt64, isa.OpJumpIfFalse, isa.SubOpEqualInt64ThenJumpIfFalse}, isa.SubOpEqualInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpNotEqualInt64, isa.OpJumpIfFalse, isa.SubOpNotEqualInt64ThenJumpIfFalse}, isa.SubOpEqualInt64ConstantThenJumpIfTrue},
	{registerCompareJumpFusion{isa.OpLessThanInt64, isa.OpJumpIfTrue, isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse}, isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpLessOrEqualInt64, isa.OpJumpIfTrue, isa.SubOpGreaterThanInt64ThenJumpIfFalse}, isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpGreaterThanInt64, isa.OpJumpIfTrue, isa.SubOpLessOrEqualInt64ThenJumpIfFalse}, isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpGreaterOrEqualInt64, isa.OpJumpIfTrue, isa.SubOpLessThanInt64ThenJumpIfFalse}, isa.SubOpLessThanInt64ConstantThenJumpIfFalse},
	{registerCompareJumpFusion{isa.OpEqualInt64, isa.OpJumpIfTrue, isa.SubOpNotEqualInt64ThenJumpIfFalse}, isa.SubOpEqualInt64ConstantThenJumpIfTrue},
	{registerCompareJumpFusion{isa.OpNotEqualInt64, isa.OpJumpIfTrue, isa.SubOpEqualInt64ThenJumpIfFalse}, isa.SubOpEqualInt64ConstantThenJumpIfFalse},
}

// uintCompareJumpFusions lists the compare-and-branch fusions of two uint registers.
var uintCompareJumpFusions = [...]registerCompareJumpFusion{
	{isa.OpEqualUint64, isa.OpJumpIfFalse, isa.SubOpEqualUint64ThenJumpIfFalse},
	{isa.OpNotEqualUint64, isa.OpJumpIfFalse, isa.SubOpNotEqualUint64ThenJumpIfFalse},
	{isa.OpEqualUint64, isa.OpJumpIfTrue, isa.SubOpNotEqualUint64ThenJumpIfFalse},
	{isa.OpNotEqualUint64, isa.OpJumpIfTrue, isa.SubOpEqualUint64ThenJumpIfFalse},
}

// incrementLatchFusions pairs register comparisons with their increment-and-branch forms.
var incrementLatchFusions = map[isa.SubOpcode]isa.SubOpcode{
	isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse: isa.SubOpIncrementInt64ThenJumpIfLessThan,
	isa.SubOpGreaterThanInt64ThenJumpIfFalse:    isa.SubOpIncrementInt64ThenJumpIfLessOrEqual,
}

// FuseCompareRegJump dispatches the register-to-register compare-and-branch fusions.
//
// Runs after FuseThreeInstrPatterns so the three-instruction constant form
// (LoadInt64Constant + compare + branch) gets first refusal on a sequence that matches
// both.
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds the
// liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func FuseCompareRegJump(compiledFunction *program.CompiledFunction, body []isa.Instruction, i, n int, jumpTargets map[int]bool) bool {
	for _, fusion := range compareJumpFusions {
		if fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, fusion.registerCompareJumpFusion) {
			return true
		}
	}
	for _, fusion := range uintCompareJumpFusions {
		if fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, fusion) {
			return true
		}
	}
	return false
}

// fuseCompareRegJump fuses `compare(dest, x, y)` + `jump(dest, off)` into the tier-1
// `fused(x, y)` + `isa.OpExtension(offLo, offHi)`.
//
// The fused form no longer writes dest, so the fusion requires dest to be dead on both
// edges.
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds the
// liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes fusion (registerCompareJumpFusion) which names the compare and branch matched and
// the fused sub-op that replaces them.
//
// Returns true if the pattern matched and was applied.
func fuseCompareRegJump(
	compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
	fusion registerCompareJumpFusion,
) bool {
	if i+1 >= n || body[i].Op != fusion.compare {
		return false
	}
	jump := i + 1
	for jump < n && isCanonicalNoOperation(body[jump]) && !jumpTargets[jump] {
		jump++
	}
	if jump >= n || body[jump].Op != fusion.jump || body[i].A != body[jump].A || jumpTargets[jump] {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, jump, isa.RegisterInt64, body[i].A) {
		return false
	}
	lo, hi := isa.SplitOffset(body[jump].SignedOffset())
	fused := isa.NewTier1Instruction(fusion.register, body[i].B, body[i].C)
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[jump-1] = fused
	body[jump] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	return true
}

// fuseCompareConstJump attempts to fuse an int constant load + cmpOp + jumpOp sequence
// into a single superinstruction.
//
// The fused form writes neither the loaded constant's register nor the condition
// register, so the fusion is refused unless both are provably dead on both edges of the
// branch (registerDeadAfterBranch()).
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds the
// liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes fusion (compareJumpFusion) which names the compare and jump opcodes matched and
// the fused sub-op that replaces them.
//
// Returns true if the pattern matched and was applied.
func fuseCompareConstJump(
	compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
	fusion compareJumpFusion,
) bool {
	if i+2 >= n || body[i+1].Op != fusion.compare || body[i+2].Op != fusion.jump || jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	loaded, ok := intConstLoadDestination(body[i])
	if !ok ||
		loaded != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		body[i+1].B == loaded {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i+1].A) ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, loaded) {
		return false
	}
	index, ok := intConstPoolIndex(compiledFunction, body[i])
	if !ok {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewTier1Instruction(fusion.constant, body[i+1].B, index)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseIncrementInt64ThenJumpIfLessThan fuses IncrementInt64(R) + LessThanInt64(cmp, R, B)
// + JumpIfTrue(cmp, off) into isa.SubOpIncrementInt64ThenJumpIfLessThan(R, B, 0) +
// isa.OpExtension(lo, hi, 0) + isa.OpNoOperation.
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
func fuseIncrementInt64ThenJumpIfLessThan(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2IncrementInt64) || body[i+1].Op != isa.OpLessThanInt64 ||
		body[i+2].Op != isa.OpJumpIfTrue ||
		body[i+1].B != body[i].C ||
		body[i+2].A != body[i+1].A ||
		body[i+1].A == body[i].C ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i+1].A) {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpIncrementInt64ThenJumpIfLessThan), body[i].C, body[i+1].C)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseIncrementThenCompareJump fuses an increment with its following compare-and-branch.
//
// Only no-operation words may lie between the two, and no jump may land after the
// increment, since it would reach the test without incrementing.
//
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate increment.
// Takes n (int) which is the body's length.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the pair was fused.
func fuseIncrementThenCompareJump(body []isa.Instruction, i, n int, jumpTargets map[int]bool) bool {
	if !isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2IncrementInt64) {
		return false
	}
	latch := i + 1
	for latch < n && isCanonicalNoOperation(body[latch]) && !jumpTargets[latch] {
		latch++
	}
	if latch >= n || jumpTargets[latch] || body[latch].Op != isa.OpDrillTier1 {
		return false
	}
	counter := body[i].C
	fused, known := incrementLatchFusions[isa.SubOpcode(body[latch].A)]
	if !known || body[latch].B != counter || body[latch].C == counter {
		return false
	}
	body[latch] = isa.NewTier1Instruction(fused, counter, body[latch].C)
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseLessThanLengthStringThenJumpIfFalse fuses isa.SubOpLengthString +
// isa.OpLessThanInt64 + isa.OpJumpIfFalse into
// isa.SubOpLessThanLengthStringThenJumpIfFalse + isa.OpExtension, collapsing the entire
// for-loop condition `i < len(s)` into a single fused instruction.
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
func fuseLessThanLengthStringThenJumpIfFalse(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier1SubOp(body[i], isa.SubOpLengthString) || body[i+1].Op != isa.OpLessThanInt64 ||
		body[i+2].Op != isa.OpJumpIfFalse ||
		body[i].B != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		body[i+1].B == body[i].B ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i].B) ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i+1].A) {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLessThanLengthStringThenJumpIfFalse), body[i+1].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseNilTestJump fuses LoadNil(R) + EqualGeneral/NotEqualGeneral(C, X, R) + Jump into
// isa.OpJumpIfNil or isa.OpJumpIfNotNil(X, lo, hi) + isa.OpNoOperation +
// isa.OpNoOperation.
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
func fuseNilTestJump(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2LoadNil) ||
		(body[i+1].Op != isa.OpEqualGeneral && body[i+1].Op != isa.OpNotEqualGeneral) ||
		(body[i+2].Op != isa.OpJumpIfTrue && body[i+2].Op != isa.OpJumpIfFalse) ||
		body[i+1].A != body[i+2].A ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	nilRegister := body[i].C
	var testRegister uint8
	if body[i+1].B == nilRegister {
		testRegister = body[i+1].C
	} else if body[i+1].C == nilRegister {
		testRegister = body[i+1].B
	} else {
		return false
	}
	if testRegister == nilRegister ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterGeneral, nilRegister) ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i+1].A) {
		return false
	}
	wantNilJump := (body[i+1].Op == isa.OpEqualGeneral && body[i+2].Op == isa.OpJumpIfTrue) ||
		(body[i+1].Op == isa.OpNotEqualGeneral && body[i+2].Op == isa.OpJumpIfFalse)
	raw := body[i+2].SignedOffset()
	adj := raw + 2
	lo, hi := isa.SplitOffset(adj)
	var fusedOp isa.Opcode
	if wantNilJump {
		fusedOp = isa.OpJumpIfNil
	} else {
		fusedOp = isa.OpJumpIfNotNil
	}
	body[i] = isa.NewInstruction(fusedOp, testRegister, lo, hi)
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseStringConstJump fuses LoadStringConstant(R, index) + EqualString(C, X, R) +
// JumpIfFalse(C, off) into isa.OpEqualStringConstantThenJumpIfFalse(X, index, 0) +
// isa.OpExtension(lo, hi, 0) + isa.OpNoOperation.
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
func fuseStringConstJump(compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		body[i].Op != isa.OpLoadStringConstant || body[i+1].Op != isa.OpEqualString ||
		body[i+2].Op != isa.OpJumpIfFalse ||
		body[i].C != 0 ||
		body[i].A != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		body[i+1].B == body[i].A ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	if !registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterString, body[i].A) ||
		!registerDeadAfterBranch(compiledFunction, body, i+2, isa.RegisterInt64, body[i+1].A) {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewTier1Instruction(isa.SubOpEqualStringConstantThenJumpIfFalse, body[i+1].B, body[i].B)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}
