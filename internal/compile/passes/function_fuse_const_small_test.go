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
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func runFuseConstOperands(t *testing.T, cf *program.CompiledFunction) {
	t.Helper()
	require.NoError(t, (fuseConstOperandsPass{}).Run(context.Background(), &PassContext{Analysis: newFunctionAnalysis(cf)}, cf))
}

func TestFuseConstOperandsFoldsSmallIntLoads(t *testing.T) {
	t.Parallel()
	cf := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 9, 1),
			mk(isa.OpAddInt, 10, 8, 9),
			isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 9, 3),
			mk(isa.OpAddInt, 11, 8, 9),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
		IntConstants:                []int64{7, 1},
		PrecomputedAllocCountsValid: true,
	}
	runFuseConstOperands(t, cf)
	require.Equal(t, mk(isa.OpAddIntConst, 10, 8, 1), cf.Body[0], "an existing pool entry is reused")
	require.Equal(t, mk(isa.OpAddIntConst, 11, 8, 2), cf.Body[2], "a missing value is added to the pool")
	require.Equal(t, []int64{7, 1, 3}, cf.IntConstants)
	require.False(t, cf.PrecomputedAllocCountsValid)
}

func TestFuseConstOperandsFoldsSmallUintLoads(t *testing.T) {
	t.Parallel()
	cf := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadUintConstSmall, 4, 127),
			mk(isa.OpBitAndUint, 5, 6, 4),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
	}
	runFuseConstOperands(t, cf)
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpBitAndUintConst, 5, 6), cf.Body[0])
	require.Equal(t, mk(isa.OpExt, 0, 0, 0), cf.Body[1])
	require.Equal(t, []uint64{127}, cf.UintConstants)
}

func TestFuseConstOperandsKeepsLoadsStillRead(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 9, 1),
		mk(isa.OpAddInt, 10, 8, 9),
		mk(isa.OpAddInt, 11, 9, 9),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: append([]isa.Instruction(nil), body...)}
	runFuseConstOperands(t, cf)
	require.Equal(t, body, cf.Body)
	require.Empty(t, cf.IntConstants)
}

func TestFuseConstOperandsKeepsNamedVariableWrites(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 1, 3),
		mk(isa.OpAddInt, 2, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{
		Body:          append([]isa.Instruction(nil), body...),
		DebugVarTable: &program.DebugVarTable{Entries: []program.DebugVarEntry{{Name: "i", Location: program.VarLocation{Kind: isa.RegisterInt, Register: 1}, StartPC: 1, EndPC: 3}}},
	}
	runFuseConstOperands(t, cf)
	require.Equal(t, body, cf.Body)
}
