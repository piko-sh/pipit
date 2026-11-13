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

package inline

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestComputeInLoopMaskSeesFusedBackEdge(t *testing.T) {
	t.Parallel()

	lo, hi := isa.SplitOffset(-3)
	caller := &program.CompiledFunction{Body: []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 0, 1),
		isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 0, 1),
		isa.NewInstruction(isa.OpExtension, lo, hi, 0),
		isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
	}}

	mask := computeInLoopMask(caller)

	require.Equal(t, []bool{true, true, false, false}, mask)
}

func TestExtendInLoopMaskRecomputesForAppendedFusedBackEdge(t *testing.T) {
	t.Parallel()

	lo, hi := isa.SplitOffset(-3)
	caller := &program.CompiledFunction{Body: []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
	}}
	mask := computeInLoopMask(caller)
	caller.Body = append(caller.Body,
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 0, 1),
		isa.NewTier1Instruction(isa.SubOpLessThanInt64ThenJumpIfFalse, 0, 1),
		isa.NewInstruction(isa.OpExtension, lo, hi, 0),
	)

	extendInLoopMaskForAppendedBody(caller, &mask)

	require.Equal(t, []bool{false, true, true, false}, mask)
}

func TestMarkInLoopMarksEveryWordFromTheFirstAppendedOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		mask  []bool
		want  []bool
		start int
	}{
		{name: "the appended words of a loop's call", mask: []bool{false, true, false, false}, start: 2, want: []bool{false, true, true, true}},
		{name: "words before the first appended one keep their mark", mask: []bool{true, false, false}, start: 1, want: []bool{true, true, true}},
		{name: "nothing appended", mask: []bool{false, false}, start: 2, want: []bool{false, false}},
		{name: "an empty mask", mask: nil, start: 0, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			markInLoop(tt.mask, tt.start)

			require.Equal(t, tt.want, tt.mask, "a callee spliced at a site inside a loop runs once for each pass of the loop")
		})
	}
}
