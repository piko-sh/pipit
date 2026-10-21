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

func TestAnIntConstantLoadIsNotFusedIntoAnUnsignedComparison(t *testing.T) {
	t.Parallel()
	const condition = 3
	body := append([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 2, 9)},
		compareJumpBody(isa.OpEqualUint64, isa.OpJumpIfFalse, condition,
			[]isa.Instruction{mk(isa.OpLoadInt64Constant, condition, 0, 0)}, []isa.Instruction{returnVoid()})...)
	want := slices.Clone(body)
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}

	require.False(t, FuseThreeInstrPatterns(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)),
		"the load fills int register 2 and the comparison reads uint register 2")
	require.Equal(t, want, body)
}

func TestFuseGetMapOkThenJumpIfFalseLandsWhereTheOriginalDid(t *testing.T) {
	t.Parallel()
	const okRegister = 6
	cases := []struct {
		source isa.Opcode
		fused  isa.Opcode
	}{
		{source: isa.OpGetMapInt64Int64Ok, fused: isa.OpGetMapInt64Int64OkThenJumpIfFalse},
		{source: isa.OpGetMapStringInt64Ok, fused: isa.OpGetMapStringInt64OkThenJumpIfFalse},
		{source: isa.OpGetMapStringStringOk, fused: isa.OpGetMapStringStringOkThenJumpIfFalse},
		{source: isa.OpGetMapInt64StringOk, fused: isa.OpGetMapInt64StringOkThenJumpIfFalse},
		{source: isa.OpGetMapInt64GeneralOk, fused: isa.OpGetMapInt64GeneralOkThenJumpIfFalse},
		{source: isa.OpGetMapStringGeneralOk, fused: isa.OpGetMapStringGeneralOkThenJumpIfFalse},
	}
	for _, testCase := range cases {
		t.Run(testCase.fused.String(), func(t *testing.T) {
			t.Parallel()
			low, high := jumpOffset(2)
			body := []isa.Instruction{
				mk(testCase.source, 1, 2, 3),
				mk(isa.OpExtension, okRegister, 0, 0),
				mk(isa.OpJumpIfFalse, okRegister, low, high),
				mk(isa.OpNoOperation, 0, 0, 0),
				returnVoid(),
				returnVoid(),
			}
			before, ok := program.JumpTargetAt(body, 2)
			require.True(t, ok)

			require.True(t, FuseThreeInstrPatterns(&program.CompiledFunction{Body: body}, body, 0, len(body), program.JumpTargetsOf(body)))

			require.Equal(t, mk(testCase.fused, 1, 2, 3), body[0])
			require.Equal(t, uint8(okRegister), body[1].A, "the fused branch still writes the ok register")
			after, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok, "the fused branch must be decodable")
			require.Equal(t, before, after, "the fused branch lands where the original did")
			require.Equal(t, 2, program.JumpFootprint(body[0]), "a hit falls through onto the padding nop")
		})
	}
}

func TestCountIncomingJumpsSeesFusedForms(t *testing.T) {
	t.Parallel()
	fusedLo, fusedHi := jumpOffset(1)
	wordLo, wordHi := jumpOffset(-2)
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 1, 2),
		mk(isa.OpExtension, fusedLo, fusedHi, 0),
		mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 5, wordLo, wordHi),
	}
	for _, source := range []int{0, 4} {
		target, ok := program.JumpTargetAt(body, source)
		require.True(t, ok)
		require.Equal(t, 3, target, "both branches land on index 3")
	}

	require.Equal(t, 2, countIncomingJumps(nil, body, 3, -1))
	require.Equal(t, 1, countIncomingJumps(nil, body, 3, 0), "excluding the fused source leaves the word jump")
	require.Equal(t, 1, countIncomingJumps(nil, body, 3, 4), "excluding the word source leaves the fused jump")

	mapLo, mapHi := jumpOffset(1)
	withMapBranch := []isa.Instruction{
		mk(isa.OpGetMapInt64Int64OkThenJumpIfFalse, 1, 2, 3),
		mk(isa.OpExtension, 6, mapLo, mapHi),
		mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpNoOperation, 0, 0, 0),
	}
	require.Equal(t, 1, countIncomingJumps(nil, withMapBranch, 3, -1), "a fused map-index-ok branch is an incoming jump")
}
