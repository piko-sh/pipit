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

package program

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestDisassembleShowsTheUintConstantAnExtensionWordIndexes(t *testing.T) {
	t.Parallel()

	operations := []isa.SubOpcode{
		isa.SubOpAddUint64Constant, isa.SubOpSubtractUint64Constant, isa.SubOpBitAndUint64Constant,
		isa.SubOpAddUint32Constant, isa.SubOpSubtractUint32Constant,
	}
	for _, operation := range operations {
		t.Run(operation.String(), func(t *testing.T) {
			t.Parallel()
			function := NewNamedFunction("f")
			function.UintConstants = []uint64{7, 4294967295}
			function.Body = []isa.Instruction{
				isa.NewTier1Instruction(operation, 1, 2),
				isa.NewInstruction(isa.OpExtension, 1, 0, 0),
			}

			require.Contains(t, function.Disassemble(), "uconst = 4294967295")
		})
	}
}

func TestDisassembleLeavesOutAConstantItCannotFind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
	}{
		{name: "an index past the pool", body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 1, 2), isa.NewInstruction(isa.OpExtension, 9, 0, 0),
		}},
		{name: "an index whose high byte takes it past the pool", body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 1, 2), isa.NewInstruction(isa.OpExtension, 0, 1, 0),
		}},
		{name: "no extension word", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 1, 2)}},
		{name: "an operation that names no constant there", body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpEqualUint64ThenJumpIfFalse, 1, 2), isa.NewInstruction(isa.OpExtension, 1, 0, 0),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function := NewNamedFunction("f")
			function.UintConstants = []uint64{7, 4294967295}
			function.Body = tt.body

			require.NotContains(t, function.Disassemble(), "uconst")
		})
	}
}
