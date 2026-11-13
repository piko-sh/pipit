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

func TestGlobalSliceFieldReusePreservesMemoryBarriers(t *testing.T) {
	t.Parallel()
	chain := []isa.Instruction{
		isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
		isa.NewInstruction(isa.OpExtension, 3, 0, 0),
	}
	tests := []struct {
		name    string
		between []isa.Instruction
		reuse   bool
	}{
		{name: "byte element", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 2, 0), isa.NewInstruction(isa.OpExtension, 0, 0, 0)}, reuse: true},
		{name: "call", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, reuse: false},
		{name: "global assignment", between: []isa.Instruction{isa.NewInstruction(isa.OpSetGlobal, 1, 7, uint8(isa.RegisterGeneral))}, reuse: false},
		{name: "receiver overwrite", between: []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 1, 4, 1)}, reuse: false},
		{name: "slice overwrite", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveSliceByte, 2, 4)}, reuse: false},
		{name: "field assignment", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetStructFieldSliceByte, 1, 2), isa.NewInstruction(isa.OpExtension, 3, 0, 0)}, reuse: false},
		{name: "indirect store", between: []isa.Instruction{isa.NewInstruction(isa.OpSetElement, 1, 0, 3)}, reuse: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := append([]isa.Instruction{}, chain...)
			body = append(body, tt.between...)
			second := len(body)
			body = append(body, chain...)
			cf := &program.CompiledFunction{Body: body}
			require.NoError(t, ElideRedundantStructFieldRead(t.Context(), cf, body))
			reused := isCanonicalNoOperation(body[second]) && isCanonicalNoOperation(body[second+1]) && isCanonicalNoOperation(body[second+2])
			require.Equal(t, tt.reuse, reused)
		})
	}
}

func TestGlobalSliceFieldReuseRejectsBranchEntry(t *testing.T) {
	t.Parallel()
	for _, target := range []int{4, 5, 6} {
		body := []isa.Instruction{
			isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
			isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
			isa.NewInstruction(isa.OpExtension, 3, 0, 0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
			isa.NewInstruction(isa.OpGetGlobal, 1, 7, uint8(isa.RegisterGeneral)),
			isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 2, 1),
			isa.NewInstruction(isa.OpExtension, 3, 0, 0),
		}
		cf := &program.CompiledFunction{Body: body}
		elideRepeatedGlobalSliceFieldRead(cf, body, 0, map[int]bool{target: true})
		require.Equal(t, isa.OpGetGlobal, body[4].Op)
	}
}
