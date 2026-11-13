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

package isa

// oppositeTier1Jumps pairs each fused compare-and-jump with the one that jumps on exactly
// the other outcomes of the same operands.
var oppositeTier1Jumps = [][2]SubOpcode{
	{SubOpEqualInt64ThenJumpIfFalse, SubOpNotEqualInt64ThenJumpIfFalse},
	{SubOpLessThanInt64ThenJumpIfFalse, SubOpGreaterOrEqualInt64ThenJumpIfFalse},
	{SubOpLessOrEqualInt64ThenJumpIfFalse, SubOpGreaterThanInt64ThenJumpIfFalse},
	{SubOpEqualInt64ConstantThenJumpIfFalse, SubOpEqualInt64ConstantThenJumpIfTrue},
	{SubOpLessThanInt64ConstantThenJumpIfFalse, SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse},
	{SubOpLessOrEqualInt64ConstantThenJumpIfFalse, SubOpGreaterThanInt64ConstantThenJumpIfFalse},
	{SubOpEqualUint64ThenJumpIfFalse, SubOpNotEqualUint64ThenJumpIfFalse},
}

// OppositeConditionalJump returns the conditional jump that is taken exactly when the
// given one is not, with the same operands and the same place for its offset.
//
// Takes instruction (Instruction) which is the conditional jump.
//
// Returns Instruction which is the opposite jump, its operands and offset those of
// instruction, or instruction itself when it has none.
// Returns bool which is false when instruction is not a conditional jump with an
// opposite.
func OppositeConditionalJump(instruction Instruction) (Instruction, bool) {
	opposite := instruction
	switch {
	case instruction.Op == OpJumpIfFalse:
		opposite.Op = OpJumpIfTrue
	case instruction.Op == OpJumpIfTrue:
		opposite.Op = OpJumpIfFalse
	case instruction.Op == OpJumpIfNil:
		opposite.Op = OpJumpIfNotNil
	case instruction.Op == OpJumpIfNotNil:
		opposite.Op = OpJumpIfNil
	case InstrIsTier2SubOp(instruction, SubOpTier2JumpIfFalseBool):
		opposite.B = uint8(SubOpTier2JumpIfTrueBool)
	case InstrIsTier2SubOp(instruction, SubOpTier2JumpIfTrueBool):
		opposite.B = uint8(SubOpTier2JumpIfFalseBool)
	case instruction.Op == OpDrillTier1:
		other, paired := oppositeTier1Jump(SubOpcode(instruction.A))
		if !paired {
			return instruction, false
		}
		opposite.A = uint8(other)
	default:
		return instruction, false
	}
	layout, trailing := JumpLayoutOf(instruction)
	if oppositeLayout, oppositeTrailing := JumpLayoutOf(opposite); oppositeLayout != layout || oppositeTrailing != trailing {
		return instruction, false
	}
	return opposite, true
}

// oppositeTier1Jump looks a fused compare-and-jump up in oppositeTier1Jumps.
//
// Takes subOpcode (SubOpcode) which is the compare-and-jump.
//
// Returns SubOpcode which is its opposite, or subOpcode itself when it has none.
// Returns bool which is false when subOpcode has none.
func oppositeTier1Jump(subOpcode SubOpcode) (SubOpcode, bool) {
	for _, pair := range oppositeTier1Jumps {
		switch subOpcode {
		case pair[0]:
			return pair[1], true
		case pair[1]:
			return pair[0], true
		default:
		}
	}
	return subOpcode, false
}
