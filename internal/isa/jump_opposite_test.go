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

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func instructionsOfEveryOperation() []Instruction {
	instructions := make([]Instruction, 0, 3*SlotsPerTier)
	for code := range SlotsPerTier {
		instructions = append(instructions,
			NewInstruction(Opcode(code), 1, 2, 2),
			NewTier1Instruction(SubOpcode(code), 1, 2),
			NewTier2Instruction(SubOpcodeTier2(code), 3))
	}
	return instructions
}

func TestOppositeConditionalJumpPairsAJumpWithOneOfTheSameOperandsAndLayout(t *testing.T) {
	t.Parallel()

	paired := 0
	for _, instruction := range instructionsOfEveryOperation() {
		opposite, ok := OppositeConditionalJump(instruction)
		if !ok {
			require.Equal(t, instruction, opposite, "an instruction with no opposite comes back as it was")
			continue
		}
		paired++
		require.NotEqual(t, instruction, opposite)
		back, ok := OppositeConditionalJump(opposite)
		require.True(t, ok)
		require.Equal(t, instruction, back, "the opposite of the opposite is the jump itself")

		layout, trailing := JumpLayoutOf(instruction)
		oppositeLayout, oppositeTrailing := JumpLayoutOf(opposite)
		require.Contains(t, []JumpLayout{JumpLayoutWord, JumpLayoutExtension}, layout)
		require.Equal(t, layout, oppositeLayout)
		require.Equal(t, trailing, oppositeTrailing)

		shape, oppositeShape := ShapeForInstruction(instruction), ShapeForInstruction(opposite)
		require.Equal(t, shape.Reads, oppositeShape.Reads)
		require.Equal(t, shape.Writes, oppositeShape.Writes)
		require.Equal(t, [NumInstructionOperands]OperandRole{shape.A, shape.B, shape.C},
			[NumInstructionOperands]OperandRole{oppositeShape.A, oppositeShape.B, oppositeShape.C})
		require.Equal(t, DispatchForInstruction(instruction), DispatchForInstruction(opposite),
			"a jump and its opposite run by the same route")
	}
	require.Equal(t, 20, paired, "four register tests, two bool tests and seven pairs of comparisons")
}

func TestOppositeConditionalJumpRefusesAJumpWithNoExactOpposite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
	}{
		{name: "an unconditional jump", instruction: NewTier1Instruction(SubOpJump, 1, 0)},
		{name: "a comparison with an immediate has no opposite form", instruction: NewTier1Instruction(SubOpEqualUint64ImmediateThenJumpIfFalse, 1, 2)},
		{name: "a comparison with a string constant has no opposite form", instruction: NewTier1Instruction(SubOpEqualStringConstantThenJumpIfFalse, 1, 2)},
		{name: "a string length test", instruction: NewTier1Instruction(SubOpLessThanLengthStringThenJumpIfFalse, 1, 2)},
		{name: "a range check", instruction: NewTier2Instruction(SubOpTier2InRangeUint64ThenJumpIfFalse, 1)},
		{name: "a loop latch writes its counter", instruction: NewTier1Instruction(SubOpIncrementInt64ThenJumpIfLessThan, 1, 2)},
		{name: "a map lookup writes its value", instruction: NewInstruction(OpGetMapInt64Int64OkThenJumpIfFalse, 1, 2, 3)},
		{name: "an addition that jumps", instruction: NewInstruction(OpAddInt64ConstantThenJump, 1, 2, 3)},
		{name: "an addition", instruction: NewInstruction(OpAddInt64, 1, 2, 3)},
		{name: "an extension word", instruction: NewInstruction(OpExtension, 1, 2, 3)},
		{name: "a no-op", instruction: NewInstruction(OpNoOperation, 0, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opposite, ok := OppositeConditionalJump(tt.instruction)

			require.False(t, ok)
			require.Equal(t, tt.instruction, opposite)
		})
	}
}
