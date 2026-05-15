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

package verify

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

var nop = isa.NewInstruction(isa.OpNop, 0, 0, 0)

func TestControlFlowSuccessorsDecodeEveryJumpLayout(t *testing.T) {
	t.Parallel()
	lo, hi := isa.SplitOffset(2)
	cases := []struct {
		name   string
		branch []isa.Instruction
	}{
		{name: "word layout", branch: []isa.Instruction{isa.NewInstruction(isa.OpJumpIfFalse, 1, lo, hi)}},
		{name: "extension layout", branch: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 1, 2), isa.NewInstruction(isa.OpExt, lo, hi, 0)}},
		{name: "extension layout with padding", branch: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 1, 2), isa.NewInstruction(isa.OpExt, lo, hi, 0), nop}},
		{name: "second extension layout", branch: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, 1), isa.NewInstruction(isa.OpExt, 3, 9, 0), isa.NewInstruction(isa.OpExt, lo, hi, 0), nop, nop, nop, nop, nop}},
		{name: "twenty-four bit layout", branch: []isa.Instruction{isa.NewInstruction(isa.OpRangeNextSliceInt, 1, 2, 3), isa.NewInstruction(isa.OpExt, 2, 0, 0)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := append([]isa.Instruction(nil), testCase.branch...)
			for range 6 {
				body = append(body, nop)
			}
			instr := body[0]
			footprint := program.JumpFootprint(instr)
			target, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok, "the layout must decode")
			require.Equal(t, footprint+2, target, "the offset of two lands two words past the footprint")

			successors := controlFlowSuccessors(body, 0, footprint)

			require.ElementsMatch(t, []int{footprint, target}, successors)
		})
	}
}

func TestTerminatorSuccessorsFollowTablesAndJumps(t *testing.T) {
	t.Parallel()
	lo, hi := isa.SplitOffset(3)
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpJump, lo, hi),
		nop, nop, nop,
		isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 0, 1),
		isa.NewInstruction(isa.OpTypeSwitchCase, 0, 1, 0),
		nop, nop,
	}

	require.Equal(t, []int{4}, terminatorSuccessors(body, 0, body[0]))
	require.Equal(t, []int{5}, terminatorSuccessors(body, 4, body[4]))
	require.Nil(t, terminatorSuccessors(body, len(body)-1, isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 0, 1)),
		"a dispatch at the very end has no rows to walk")
}

func TestVerifierFollowsFusedCompareJumpTargets(t *testing.T) {
	t.Parallel()
	toDispatch, toDispatchHi := isa.SplitOffset(1)
	backLo, backHi := isa.SplitOffset(-4)
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1),
		isa.NewInstruction(isa.OpExt, toDispatch, toDispatchHi, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 0, 1),
		isa.NewInstruction(isa.OpTypeSwitchCase, 0, backLo, backHi),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{
		Name:         "fused",
		Body:         body,
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2, isa.RegisterGeneral: 1},
	}

	report := &VerificationReport{}
	require.NoError(t, RunFunctionVerifier(t.Context(), compiledFunction, report))

	require.True(t, report.HasErrors(), "the backward case row behind the fused branch must be reported")
}
