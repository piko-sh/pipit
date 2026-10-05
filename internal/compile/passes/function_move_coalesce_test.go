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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func runCoalesce(t *testing.T, cf *program.CompiledFunction) {
	t.Helper()
	require.NoError(t, (coalesceMovesPass{}).Run(context.Background(), &PassContext{Analysis: newFunctionAnalysis(cf)}, cf))
}

func TestCoalesceRenamesDefinitionThroughInPlaceUpdates(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpBitAndUint, 7, 5, 6),
		mk(isa.OpTruncateNarrow, 7, 32, uint8(isa.RegisterUint)),
		isa.NewTier1Instruction(isa.SubOpMoveUint, 3, 7),
		mk(isa.OpAddUint, 4, 3, 3),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: body}
	runCoalesce(t, cf)
	require.Equal(t, []isa.Instruction{
		mk(isa.OpBitAndUint, 3, 5, 6),
		mk(isa.OpTruncateNarrow, 3, 32, uint8(isa.RegisterUint)),
		nop(),
		mk(isa.OpAddUint, 4, 3, 3),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}, cf.Body)
}

func TestCoalesceForwardsSourceIntoExtensionWord(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpMoveUint, 1, 2),
		isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 0, 8), mk(isa.OpExt, 1, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMoveUint, 1, 3),
		isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 0, 9), mk(isa.OpExt, 1, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: body}
	runCoalesce(t, cf)
	require.Equal(t, []isa.Instruction{
		nop(),
		body[1], mk(isa.OpExt, 2, 0, 0),
		nop(),
		body[4], mk(isa.OpExt, 3, 0, 0),
		body[6],
	}, cf.Body)
}

func TestCoalesceRefusesUnsafeRewrites(t *testing.T) {
	t.Parallel()
	ret := isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)
	for name, tc := range map[string]struct {
		body []isa.Instruction
		vars []program.DebugVarEntry
	}{
		"source read after move":        {body: []isa.Instruction{mk(isa.OpAddInt, 7, 5, 6), isa.NewTier1Instruction(isa.SubOpMoveInt, 3, 7), mk(isa.OpAddInt, 4, 7, 3), ret}},
		"destination read between":      {body: []isa.Instruction{mk(isa.OpAddInt, 7, 5, 6), mk(isa.OpAddInt, 4, 3, 3), isa.NewTier1Instruction(isa.SubOpMoveInt, 3, 7), ret}},
		"definition reads destination":  {body: []isa.Instruction{mk(isa.OpAddInt, 7, 3, 6), isa.NewTier1Instruction(isa.SubOpMoveInt, 3, 7), ret}},
		"increment is not a definition": {body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2IncInt, 7), isa.NewTier1Instruction(isa.SubOpMoveInt, 3, 7), ret}},
		"named variable": {
			body: []isa.Instruction{mk(isa.OpAddInt, 7, 5, 6), isa.NewTier1Instruction(isa.SubOpMoveInt, 3, 7), ret},
			vars: []program.DebugVarEntry{{Name: "v", Location: program.VarLocation{Kind: isa.RegisterInt, Register: 7}, StartPC: 0, EndPC: 2}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cf := &program.CompiledFunction{Body: slices.Clone(tc.body)}
			if tc.vars != nil {
				cf.DebugVarTable = &program.DebugVarTable{Entries: tc.vars}
			}
			runCoalesce(t, cf)
			require.Equal(t, tc.body, cf.Body)
		})
	}
}

func TestCoalesceForwardsAcrossUnrelatedInstructions(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpMoveUint, 1, 3),
		isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 9, 1),
		mk(isa.OpAddInt, 10, 8, 9),
		isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 0, 10), mk(isa.OpExt, 1, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLoadUintConstSmall, 1, 255),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	runCoalesce(t, cf)
	want := slices.Clone(body)
	want[0] = nop()
	want[4] = mk(isa.OpExt, 3, 0, 0)
	require.Equal(t, want, cf.Body)

	blocked := slices.Clone(body)
	blocked[1] = isa.NewTier1Instruction(isa.SubOpLoadUintConstSmall, 3, 7)
	cf = &program.CompiledFunction{Body: slices.Clone(blocked)}
	runCoalesce(t, cf)
	require.Equal(t, blocked, cf.Body, "the source is overwritten before the store")
}
