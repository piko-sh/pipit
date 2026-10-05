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

func truncate(reg uint8, kind isa.RegisterKind) isa.Instruction {
	return mk(isa.OpTruncateNarrow, reg, 32, uint8(kind))
}

func TestRangedTruncateElisionRemovesProvenNoOps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 8, 127),
		mk(isa.OpBitAnd, 9, 7, 8),
		truncate(9, isa.RegisterInt),
		truncate(7, isa.RegisterInt),
		mk(isa.OpShiftRight, 10, 7, 8),
		truncate(10, isa.RegisterInt),
		mk(isa.OpAddInt, 11, 7, 7),
		truncate(11, isa.RegisterInt),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	require.True(t, elideRangedTruncates(cf))
	require.True(t, isCanonicalNop(cf.Body[2]), "a value masked to 127 fits")
	require.Equal(t, body[3], cf.Body[3], "an unknown value needs its truncation")
	require.True(t, isCanonicalNop(cf.Body[5]), "a right shift of a truncated value fits")
	require.Equal(t, body[7], cf.Body[7], "the sum of two int32 values may not fit")
}

func TestRangedTruncateElisionFollowsLoops(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		truncate(0, isa.RegisterUint),
		mk(isa.OpLoadIntConst, 9, 0, 0),
		mk(isa.OpJumpIfFalse, 9, 0, 0),
		mk(isa.OpShiftRightUint, 5, 0, 4),
		truncate(5, isa.RegisterUint),
		mk(isa.OpAddUint, 6, 0, 1),
		truncate(6, isa.RegisterUint),
		isa.NewTier1Instruction(isa.SubOpMoveUint, 0, 6),
		tier1Jump(0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 2, 9))
	require.True(t, program.SetJumpTarget(body, 8, 1))
	cf := &program.CompiledFunction{Body: slices.Clone(body), IntConstants: []int64{1}}
	require.True(t, elideRangedTruncates(cf))
	require.True(t, isCanonicalNop(cf.Body[4]), "position stays 32-bit around the loop")
	require.Equal(t, body[6], cf.Body[6], "the addition can carry past 32 bits")
}

func TestRangedTruncateElisionForgetsAcrossCalls(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 8, 127),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		truncate(8, isa.RegisterInt),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	require.False(t, elideRangedTruncates(cf))
	require.Equal(t, body, cf.Body)
}

func TestRangedTruncateElisionUsesShiftCounts(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		truncate(0, isa.RegisterUint),
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 3, uint8(isa.RegisterGeneral)), mk(isa.OpExt, 1, 1, 0),
		isa.NewTier1Instruction(isa.SubOpLoadUintConstSmall, 12, 26),
		mk(isa.OpShiftRightUint, 2, 0, 12),
		isa.NewTier1Instruction(isa.SubOpUintToInt, 1, 2),
		truncate(1, isa.RegisterInt),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	require.True(t, elideRangedTruncates(cf))
	require.True(t, isCanonicalNop(cf.Body[6]), "a 32-bit value shifted right by 26 fits int32, across a wide global load")
}
