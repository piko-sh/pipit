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

//go:build integration

package bytecode_test

import (
	"testing"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func buildMoveAcrossJumpBody(landingReadsDestination bool) *program.CompiledFunction {
	bb := newBytecodeBuilder().intRegisters(4).returnInt()
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpMoveInt), 2, 1)
	bb.EmitJump(isa.OpJumpIfFalse, 0, 1)
	bb.Emit(isa.OpAddInt, 3, 2, 0)
	if landingReadsDestination {
		bb.Emit(isa.OpAddInt, 3, 2, 0)
	} else {
		bb.Emit(isa.OpAddInt, 2, 0, 0)
	}
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return bb.build()
}

func TestMoveEliminationCrossesDominatedJump(t *testing.T) {
	t.Parallel()
	compiledFunction := buildMoveAcrossJumpBody(false)
	dom := passes.ComputeFunctionDominators(compiledFunction.Body)
	require.NotNil(t, dom)
	require.True(t, dom.Dominates(0, 3))

	passes.EliminateMovesAcrossDominatedJumps(compiledFunction, compiledFunction.Body, dom)

	require.Equal(t, isa.OpNop, compiledFunction.Body[0].Op, "the MOVE must be eliminated across the dominated forward jump")
	require.Equal(t, uint8(1), compiledFunction.Body[2].B, "the unique reader must read the MOVE's source")
	require.Equal(t, isa.OpAddInt, compiledFunction.Body[3].Op)
}

func TestMoveEliminationKeepsMoveWhenOtherEdgeReadsDestination(t *testing.T) {
	t.Parallel()
	compiledFunction := buildMoveAcrossJumpBody(true)
	dom := passes.ComputeFunctionDominators(compiledFunction.Body)
	require.NotNil(t, dom)

	passes.EliminateMovesAcrossDominatedJumps(compiledFunction, compiledFunction.Body, dom)

	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[0].Op, "a read on the jump's target path must keep the MOVE")
	require.Equal(t, uint8(2), compiledFunction.Body[2].B)
}

func TestMoveEliminationWithoutDominatorsStaysInBlock(t *testing.T) {
	t.Parallel()
	compiledFunction := buildMoveAcrossJumpBody(false)
	jumpTargets := passes.BuildAllJumpTargets(compiledFunction.Body)
	candidate := passes.MoveCandidate{Kind: isa.RegisterInt, Destination: 2, Source: 1}

	require.False(t, passes.ScanAndEliminateMove(compiledFunction.Body, 0, len(compiledFunction.Body), jumpTargets, candidate))
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[0].Op)
}

func TestJumpTargetAtDecodesFusedExtensionOffsets(t *testing.T) {
	t.Parallel()
	bb := newBytecodeBuilder().intRegisters(2)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpLtIntConstJumpFalse), 0, 0)
	bb.emitExt(3)
	bb.Emit(isa.OpNop, 0, 0, 0)
	bb.Emit(isa.OpNop, 0, 0, 0)
	bb.Emit(isa.OpNop, 0, 0, 0)
	bb.Emit(isa.OpDrillTier1, uint8(isa.SubOpJump), 0, 0)
	body := bb.build().Body

	target, ok := program.JumpTargetAt(body, 0)
	require.True(t, ok)
	require.Equal(t, 5, target, "fused jumps land relative to the word after their extension")
	_, ok = program.JumpTargetAt(body, 1)
	require.False(t, ok, "an extension word is not itself a jump")
	require.True(t, passes.BuildAllJumpTargets(body)[5])
}
