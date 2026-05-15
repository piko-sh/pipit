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

// FuseCompareRegJump dispatches the register-to-register compare-and-branch fusions.
//
// Runs after FuseThreeInstrPatterns so the three-instruction constant form (LoadIntConst
// + compare + branch) gets first refusal on a sequence that matches both.
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
	return fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpLtInt, isa.SubOpLtIntJumpFalse) ||
		fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpLeInt, isa.SubOpLeIntJumpFalse) ||
		fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpGtInt, isa.SubOpGtIntJumpFalse) ||
		fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpGeInt, isa.SubOpGeIntJumpFalse) ||
		fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpEqInt, isa.SubOpEqIntJumpFalse) ||
		fuseCompareRegJump(compiledFunction, body, i, n, jumpTargets, isa.OpNeInt, isa.SubOpNeIntJumpFalse)
}

// fuseCompareRegJump fuses `cmpOp(dest, x, y)` + `JumpIfFalse(dest, off)` into the tier-1
// `fusedSubOp(x, y)` + `isa.OpExt(offLo, offHi)`.
//
// Two words in, two words out, so no offset adjustment is needed. The fused form no
// longer writes dest, so the fusion requires dest to be dead on both edges.
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds the
// liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes cmpOp (opcode) which specifies the comparison opcode.
// Takes fusedSubOp (isa.SubOpcode) which specifies the fused compare-and-branch sub-op.
//
// Returns true if the pattern matched and was applied.
func fuseCompareRegJump(
	compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
	cmpOp isa.Opcode, fusedSubOp isa.SubOpcode,
) bool {
	if i+1 >= n ||
		body[i].Op != cmpOp ||
		body[i+1].Op != isa.OpJumpIfFalse ||
		body[i].A != body[i+1].A ||
		jumpTargets[i+1] {
		return false
	}
	if !conditionRegisterDeadAfterBranch(compiledFunction, body, i+1, body[i].A) {
		return false
	}
	lo, hi := isa.SplitOffset(body[i+1].SignedOffset())
	body[i] = isa.NewTier1Instruction(fusedSubOp, body[i].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	return true
}

// compareConstFusion names one compare-against-constant fusion: the tier-0 compare and
// jump opcodes it matches and the tier-1 sub-op that replaces them.
type compareConstFusion struct {
	// compare is the tier-0 comparison opcode to match.
	compare isa.Opcode

	// jump is the tier-0 conditional branch opcode to match.
	jump isa.Opcode

	// fused is the tier-1 sub-op that replaces the pair.
	fused isa.SubOpcode
}

// fuseCompareConstJump attempts to fuse a LoadIntConst + cmpOp + jumpOp sequence into a
// single superinstruction.
//
// The fused form writes neither the loaded constant's register nor the condition
// register, so the fusion is refused unless both are provably dead on both edges of the
// branch (conditionRegisterDeadAfterBranch()).
//
// Takes compiledFunction (*CompiledFunction) whose call sites and result kinds the
// liveness guard reads.
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
// Takes fusion (compareConstFusion) which names the compare and jump opcodes matched and
// the fused sub-op that replaces them.
//
// Returns true if the pattern matched and was applied.
func fuseCompareConstJump(
	compiledFunction *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
	fusion compareConstFusion,
) bool {
	if i+2 >= n ||
		body[i].Op != isa.OpLoadIntConst || body[i+1].Op != fusion.compare ||
		body[i+2].Op != fusion.jump ||
		body[i].A != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		body[i].C != 0 ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	if !conditionRegisterDeadAfterBranch(compiledFunction, body, i+2, body[i+1].A) ||
		!conditionRegisterDeadAfterBranch(compiledFunction, body, i+2, body[i].A) {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewTier1Instruction(fusion.fused, body[i+1].B, body[i].B)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// fuseIncIntJumpLt fuses IncInt(R) + LtInt(cmp, R, B) + JumpIfTrue(cmp, off) into
// isa.SubOpIncIntJumpLt(R, B, 0) + isa.OpExt(lo, hi, 0) + isa.OpNop.
//
// Operand layout: isa.SubOpTier2IncInt lives in tier-2 form {isa.OpDrillTier1,
// isa.SubOpDrillTier2, isa.SubOpTier2IncInt, R=C}.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseIncIntJumpLt(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2IncInt) || body[i+1].Op != isa.OpLtInt ||
		body[i+2].Op != isa.OpJumpIfTrue ||
		body[i+1].B != body[i].C ||
		body[i+2].A != body[i+1].A ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpIncIntJumpLt), body[i].C, body[i+1].C)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// fuseLenStringLtJump fuses isa.SubOpLenString + isa.OpLtInt + isa.OpJumpIfFalse into
// isa.SubOpLenStringLtJumpFalse + isa.OpExt, collapsing the entire for-loop condition `i
// < len(s)` into a single fused instruction.
//
// The fuser matches {isa.OpDrillTier1, isa.SubOpLenString, R_len, R_str} + LT_INT(R_bool,
// R_i, R_len) + JUMP_IF_FALSE(R_bool, lo, hi) and rewrites them to
// LEN_STRING_LT_JUMP_FALSE(R_i, R_str, 0) + EXT(lo, hi, 0) + NOP.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseLenStringLtJump(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier1SubOp(body[i], isa.SubOpLenString) || body[i+1].Op != isa.OpLtInt ||
		body[i+2].Op != isa.OpJumpIfFalse ||
		body[i].B != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewInstruction(isa.OpDrillTier1, uint8(isa.SubOpLenStringLtJumpFalse), body[i+1].B, body[i].C)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// fuseNilTestJump fuses LoadNil(R) + EqGeneral/NeGeneral(C, X, R) + Jump into
// isa.OpTestNilJumpTrue/False(X, lo, hi) + isa.OpNop + isa.OpNop.
//
// Operand layout: isa.SubOpTier2LoadNil lives in tier-2 form {isa.OpDrillTier1,
// isa.SubOpDrillTier2, isa.SubOpTier2LoadNil, R=C}.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseNilTestJump(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		!isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2LoadNil) ||
		(body[i+1].Op != isa.OpEqGeneral && body[i+1].Op != isa.OpNeGeneral) ||
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
	wantNilJump := (body[i+1].Op == isa.OpEqGeneral && body[i+2].Op == isa.OpJumpIfTrue) ||
		(body[i+1].Op == isa.OpNeGeneral && body[i+2].Op == isa.OpJumpIfFalse)
	raw := body[i+2].SignedOffset()
	adj := raw + 2
	lo, hi := isa.SplitOffset(adj)
	var fusedOp isa.Opcode
	if wantNilJump {
		fusedOp = isa.OpTestNilJumpTrue
	} else {
		fusedOp = isa.OpTestNilJumpFalse
	}
	body[i] = isa.NewInstruction(fusedOp, testRegister, lo, hi)
	body[i+1] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}

// fuseStringConstJump fuses LoadStringConst(R, index) + EqString(C, X, R) +
// JumpIfFalse(C, off) into isa.OpEqStringConstJumpFalse(X, index, 0) + isa.OpExt(lo, hi,
// 0) + isa.OpNop.
//
// Takes body ([]instruction) which specifies the instruction sequence.
// Takes i (int) which specifies the current index.
// Takes n (int) which specifies the length.
// Takes jumpTargets (map[int]bool) which specifies the set of protected jump
// destinations.
//
// Returns true if a pattern was matched and applied.
func fuseStringConstJump(_ *program.CompiledFunction,
	body []isa.Instruction, i, n int,
	jumpTargets map[int]bool,
) bool {
	if i+2 >= n ||
		body[i].Op != isa.OpLoadStringConst || body[i+1].Op != isa.OpEqString ||
		body[i+2].Op != isa.OpJumpIfFalse ||
		body[i].C != 0 ||
		body[i].A != body[i+1].C ||
		body[i+1].A != body[i+2].A ||
		jumpTargets[i+1] || jumpTargets[i+2] {
		return false
	}
	raw := body[i+2].SignedOffset()
	adj := raw + 1
	lo, hi := isa.SplitOffset(adj)
	body[i] = isa.NewTier1Instruction(isa.SubOpEqStringConstJumpFalse, body[i+1].B, body[i].B)
	body[i+1] = isa.NewInstruction(isa.OpExt, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
	return true
}
