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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func fusedExt(offset int16) isa.Instruction {
	lo, hi := isa.SplitOffset(offset)
	return mk(isa.OpExt, lo, hi, 0)
}

func TestIdentifyLoopsSeesFusedLatch(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		isa.NewTier1Instruction(isa.SubOpIncIntJumpLt, 3, 6),
		fusedExt(-4),
		mk(isa.OpNop, 0, 0, 0),
	}

	loops := identifyLoops(body)
	require.Equal(t, []loopRange{{header: 0, latch: 2}}, loops)
}

func TestApplyLoopHoistKeepsFusedJumpOperands(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		isa.NewTier1Instruction(isa.SubOpIncIntJumpLt, 3, 6),
		fusedExt(-4),
		mk(isa.OpNop, 0, 0, 0),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	applyLoopHoist(compiledFunction, nil, 0, 1)

	require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[0].Op)
	require.Equal(t, isa.OpLoadIntConst, compiledFunction.Body[1].Op)
	latch := compiledFunction.Body[2]
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpIncIntJumpLt, 3, 6), latch, "the latch's register operands must survive the repoint")
	target, ok := program.JumpTargetAt(compiledFunction.Body, 2)
	require.True(t, ok)
	require.Equal(t, 1, target, "the back edge skips the hoisted read and lands on the shifted header word")
}

func TestApplyLoopHoistShiftsForwardFusedJumpIntoRotatedSegment(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 1, 2),
		fusedExt(1),
		mk(isa.OpLoadIntConst, 7, 0, 0),
		mk(isa.OpLoadIntConst, 8, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		tier1Jump(-4),
	}
	before, ok := program.JumpTargetAt(body, 0)
	require.True(t, ok)
	require.Equal(t, 3, before)

	compiledFunction := &program.CompiledFunction{Body: body}
	applyLoopHoist(compiledFunction, nil, 2, 4)

	after, ok := program.JumpTargetAt(compiledFunction.Body, 0)
	require.True(t, ok)
	require.Equal(t, 4, after, "a forward jump into the rotated segment moves by the hoisted width")
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 1, 2), compiledFunction.Body[0])
	require.Equal(t, uint8(8), compiledFunction.Body[4].A, "the jump still lands on the same word")
	backEdge, ok := program.JumpTargetAt(compiledFunction.Body, 5)
	require.True(t, ok)
	require.Equal(t, 3, backEdge, "the back edge skips the hoisted read")
}

func TestLoopRemainderIsHeapPureIgnoresNonJumpExtensions(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{
		mk(isa.OpMoveGeneral, 1, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMakeSliceInt, 0, 1),
		mk(isa.OpExt, 0xFD, 0xFF, 0),
		isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
	}}

	require.False(t, loopRemainderIsHeapPure(compiledFunction, 0),
		"no back edge exists, so there is no loop remainder to call pure")
}

func TestScanAndEliminateMoveKeepsMoveReadByFusedJump(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpMoveInt, 2, 1),
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 2, 3),
		fusedExt(1),
		mk(isa.OpNop, 0, 0, 0),
		isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
	}
	original := body[0]
	candidate := MoveCandidate{Kind: isa.RegisterInt, Destination: 2, Source: 1}

	eliminated := ScanAndEliminateMove(body, 0, len(body), BuildAllJumpTargets(body), candidate)

	require.False(t, eliminated)
	require.Equal(t, original, body[0])
}
