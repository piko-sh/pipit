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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func jumpOverJumpBody(t *testing.T, conditional isa.Instruction) (body []isa.Instruction, over int) {
	t.Helper()
	body = []isa.Instruction{conditional}
	if layout, _ := isa.JumpLayoutOf(conditional); layout != isa.JumpLayoutWord {
		body = append(body, mk(isa.OpExtension, 0, 0, 0))
	}
	over = len(body)
	body = append(body, tier1Jump(0), mk(isa.OpAddInt64, 1, 1, 0), mk(isa.OpAddInt64, 2, 2, 0), returnVoid())
	require.True(t, program.SetJumpTarget(body, 0, over+1))
	require.True(t, program.SetJumpTarget(body, over, len(body)-1))
	return body, over
}

func TestCompactTurnsAConditionalJumpOverAJumpIntoItsOpposite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		conditional isa.Instruction
		opposite    isa.Instruction
	}{
		{name: "a test of an int register", conditional: mk(isa.OpJumpIfFalse, 7, 0, 0), opposite: mk(isa.OpJumpIfTrue, 7, 0, 0)},
		{name: "a test of an int register for true", conditional: mk(isa.OpJumpIfTrue, 7, 0, 0), opposite: mk(isa.OpJumpIfFalse, 7, 0, 0)},
		{name: "a nil test", conditional: mk(isa.OpJumpIfNil, 4, 0, 0), opposite: mk(isa.OpJumpIfNotNil, 4, 0, 0)},
		{
			name:        "a test of a bool register",
			conditional: isa.NewTier2Instruction(isa.SubOpTier2JumpIfFalseBool, 3),
			opposite:    isa.NewTier2Instruction(isa.SubOpTier2JumpIfTrueBool, 3),
		},
		{
			name:        "an unsigned comparison for difference",
			conditional: isa.NewTier1Instruction(isa.SubOpNotEqualUint64ThenJumpIfFalse, 5, 6),
			opposite:    isa.NewTier1Instruction(isa.SubOpEqualUint64ThenJumpIfFalse, 5, 6),
		},
		{
			name:        "a signed comparison for equality",
			conditional: isa.NewTier1Instruction(isa.SubOpEqualInt64ThenJumpIfFalse, 5, 6),
			opposite:    isa.NewTier1Instruction(isa.SubOpNotEqualInt64ThenJumpIfFalse, 5, 6),
		},
		{
			name:        "a signed comparison for less",
			conditional: isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 5, 6),
			opposite:    isa.NewTier1Instruction(isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, 5, 6),
		},
		{
			name:        "a signed comparison for greater",
			conditional: isa.NewTier1Instruction(isa.SubOpGreaterThanInt64ThenJumpIfFalse, 5, 6),
			opposite:    isa.NewTier1Instruction(isa.SubOpLessOrEqualInt64ThenJumpIfFalse, 5, 6),
		},
		{
			name:        "a comparison with a constant for equality",
			conditional: isa.NewTier1Instruction(isa.SubOpEqualInt64ConstantThenJumpIfFalse, 5, 2),
			opposite:    isa.NewTier1Instruction(isa.SubOpEqualInt64ConstantThenJumpIfTrue, 5, 2),
		},
		{
			name:        "a comparison with a constant for less",
			conditional: isa.NewTier1Instruction(isa.SubOpLessThanInt64ConstantThenJumpIfFalse, 5, 2),
			opposite:    isa.NewTier1Instruction(isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, 5, 2),
		},
		{
			name:        "a comparison with a constant for greater",
			conditional: isa.NewTier1Instruction(isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse, 5, 2),
			opposite:    isa.NewTier1Instruction(isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse, 5, 2),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, over := jumpOverJumpBody(t, tt.conditional)
			compiledFunction := &program.CompiledFunction{Body: body}

			changed, err := compactBody(t.Context(), compiledFunction)

			require.NoError(t, err)
			require.True(t, changed)
			want := append(slices.Clone(body[:over]), mk(isa.OpAddInt64, 1, 1, 0), mk(isa.OpAddInt64, 2, 2, 0), returnVoid())
			want[0] = tt.opposite
			require.True(t, program.SetJumpTarget(want, 0, len(want)-1))
			require.Equal(t, want, compiledFunction.Body,
				"one jump is left, taken when the first was not, to where the second went")
		})
	}
}

func TestInvertJumpsOverJumpsKeepsAJumpAConditionalJumpDoesNotOnlyStepOver(t *testing.T) {
	t.Parallel()
	different := isa.NewTier1Instruction(isa.SubOpNotEqualUint64ThenJumpIfFalse, 5, 6)

	tests := []struct {
		name  string
		build func(t *testing.T) []isa.Instruction
	}{
		{
			name: "another jump lands on the unconditional jump",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, over := jumpOverJumpBody(t, different)
				body = append(body, mk(isa.OpJumpIfFalse, 7, 0, 0), returnVoid())
				require.True(t, program.SetJumpTarget(body, len(body)-2, over))
				return body
			},
		},
		{
			name: "the conditional jump lands further on",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, over := jumpOverJumpBody(t, different)
				require.True(t, program.SetJumpTarget(body, 0, over+2))
				return body
			},
		},
		{
			name: "the conditional jump has no opposite",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := jumpOverJumpBody(t, isa.NewTier1Instruction(isa.SubOpEqualStringConstantThenJumpIfFalse, 5, 2))
				return body
			},
		},
		{
			name: "a loop latch is followed by a jump",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := jumpOverJumpBody(t, isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThan, 5, 6))
				return body
			},
		},
		{
			name: "the body holds a type-switch table that does not end in a default jump",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := jumpOverJumpBody(t, different)
				return append(body, typeSwitchHead(), wordJumpRow(0), returnVoid())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := tt.build(t)
			original := slices.Clone(body)

			require.False(t, invertJumpsOverJumps(body))

			require.Equal(t, original, body, "a pair that stays leaves every word as it was")
		})
	}
}

func TestInvertJumpsOverJumpsTakesAPairBesideAWholeTypeSwitchTable(t *testing.T) {
	t.Parallel()
	body, over := jumpOverJumpBody(t, mk(isa.OpJumpIfFalse, 7, 0, 0))
	table := len(body)
	body = append(body, typeSwitchHead(), wordJumpRow(0), tier1Jump(0), returnVoid())
	require.True(t, program.SetJumpTarget(body, table+1, table+3))
	require.True(t, program.SetJumpTarget(body, table+2, table+3))
	tableWords := slices.Clone(body[table:])

	require.True(t, invertJumpsOverJumps(body))

	require.Equal(t, isa.OpJumpIfTrue, body[0].Op)
	require.Equal(t, nop(), body[over])
	require.Equal(t, tableWords, body[table:], "the table's head, row and default jump are not a pair to replace")
}

func TestCompactKeepsAJumpOverAJumpInADebugBuild(t *testing.T) {
	t.Parallel()
	different := isa.NewTier1Instruction(isa.SubOpNotEqualUint64ThenJumpIfFalse, 5, 6)
	body, over := jumpOverJumpBody(t, different)
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), DebugVarTable: &program.DebugVarTable{}}

	_, err := compactBody(t.Context(), compiledFunction)

	require.NoError(t, err)
	require.Equal(t, different, compiledFunction.Body[0])
	require.Equal(t, body[over].A, compiledFunction.Body[over].A, "the unconditional jump is still there, for a breakpoint to bind to")
	require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[over], isa.SubOpJump))
}

func TestInvertJumpsOverJumpsLeavesAPairWhoseOneJumpCouldNotReachTheTarget(t *testing.T) {
	t.Parallel()
	const furthest = 1<<15 - 1
	body := make([]isa.Instruction, 0, furthest+4)
	body = append(body, mk(isa.OpJumpIfFalse, 7, 0, 0), tier1Jump(0))
	for range furthest {
		body = append(body, mk(isa.OpAddInt64, 2, 2, 0))
	}
	body = append(body, returnVoid())
	require.True(t, program.SetJumpTarget(body, 0, 2))
	require.True(t, program.SetJumpTarget(body, 1, len(body)-1), "the unconditional jump reaches its furthest target")
	original := slices.Clone(body)

	require.False(t, invertJumpsOverJumps(body), "the conditional jump starts one word earlier and cannot reach it")
	require.Equal(t, original, body)
}
