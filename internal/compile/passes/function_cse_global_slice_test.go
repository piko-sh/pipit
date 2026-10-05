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

func TestGlobalSliceFieldReusePreservesMemoryBarriers(t *testing.T) {
	t.Parallel()
	chain := []isa.Instruction{
		isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
		isa.NewInstruction(isa.OpExt, 3, 0, 0),
	}
	cases := []struct {
		name    string
		between []isa.Instruction
		reuse   bool
	}{
		{"byte element", []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceSetByteDirect, 2, 0), isa.NewInstruction(isa.OpExt, 0, 0, 0)}, true},
		{"call", []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, false},
		{"global assignment", []isa.Instruction{isa.NewInstruction(isa.OpSetGlobal, 1, 7, uint8(isa.RegisterGeneral))}, false},
		{"receiver overwrite", []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 1, 4, 1)}, false},
		{"slice overwrite", []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveSliceByte, 2, 4)}, false},
		{"field assignment", []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetStructFieldSliceByte, 1, 2), isa.NewInstruction(isa.OpExt, 3, 0, 0)}, false},
		{"indirect store", []isa.Instruction{isa.NewInstruction(isa.OpIndexSet, 1, 0, 3)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := append([]isa.Instruction{}, chain...)
			body = append(body, tc.between...)
			second := len(body)
			body = append(body, chain...)
			cf := &program.CompiledFunction{Body: body}
			require.NoError(t, ElideRedundantStructFieldRead(context.Background(), cf, body))
			reused := isCanonicalNop(body[second]) && isCanonicalNop(body[second+1]) && isCanonicalNop(body[second+2])
			require.Equal(t, tc.reuse, reused)
		})
	}
}

func TestGlobalSliceFieldReuseRejectsBranchEntry(t *testing.T) {
	t.Parallel()
	for _, target := range []int{4, 5, 6} {
		body := []isa.Instruction{
			isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
			isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
			isa.NewInstruction(isa.OpExt, 3, 0, 0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
			isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
			isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
			isa.NewInstruction(isa.OpExt, 3, 0, 0),
		}
		cf := &program.CompiledFunction{Body: body}
		elideRepeatedGlobalSliceFieldRead(cf, body, 0, map[int]bool{target: true})
		require.Equal(t, isa.OpGetGlobal, body[4].Op)
	}
}
