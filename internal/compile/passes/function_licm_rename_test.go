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

func TestRenameRefusesAJoinReachedThroughARedefiningUse(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpMultiplyInt64, 10, 8, 9),
		mk(isa.OpJumpIfFalse, 7, 0, 0),
		mk(isa.OpAddInt64, 10, 10, 9),
		mk(isa.OpAddInt64, 11, 11, 10),
	)
	require.True(t, program.SetJumpTarget(compiledFunction.Body, 3, 5))
	compiledFunction.NumRegisters[isa.RegisterBool] = 8
	original := slices.Clone(compiledFunction.Body)
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, original, compiledFunction.Body, "the read at the join sees the redefined register on one arm, so the product keeps its register and stays in the loop")
}

func TestRenameKeepsAJoinReachedOnlyFromTheDefinition(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpMultiplyInt64, 10, 8, 9),
		mk(isa.OpJumpIfFalse, 7, 0, 0),
		mk(isa.OpAddInt64, 12, 10, 0),
		mk(isa.OpAddInt64, 11, 11, 10),
		mk(isa.OpAddInt64, 10, 0, 0),
	)
	require.True(t, program.SetJumpTarget(compiledFunction.Body, 3, 5))
	compiledFunction.NumRegisters[isa.RegisterBool] = 8
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, mk(isa.OpMultiplyInt64, 16, 8, 9), compiledFunction.Body[1], "the product leaves the loop in a fresh register")
	require.Equal(t, mk(isa.OpAddInt64, 12, 16, 0), compiledFunction.Body[4], "the read on the arm uses the fresh register")
	require.Equal(t, mk(isa.OpAddInt64, 11, 11, 16), compiledFunction.Body[5], "the read at the join uses the fresh register")
	require.Equal(t, mk(isa.OpAddInt64, 10, 0, 0), compiledFunction.Body[6], "the later writer keeps its register")
}

func TestRenameKeepsARedefiningUseThatEndsEveryPath(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpMultiplyInt64, 10, 8, 9),
		mk(isa.OpJumpIfFalse, 7, 0, 0),
		mk(isa.OpAddInt64, 12, 10, 0),
		mk(isa.OpAddInt64, 10, 10, 0),
		mk(isa.OpAddInt64, 11, 11, 10),
	)
	require.True(t, program.SetJumpTarget(compiledFunction.Body, 3, 5))
	compiledFunction.NumRegisters[isa.RegisterBool] = 8
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, mk(isa.OpMultiplyInt64, 16, 8, 9), compiledFunction.Body[1], "the product leaves the loop in a fresh register")
	require.Equal(t, mk(isa.OpAddInt64, 12, 16, 0), compiledFunction.Body[4], "the read on the arm uses the fresh register")
	require.Equal(t, mk(isa.OpAddInt64, 10, 16, 0), compiledFunction.Body[5], "the join's redefining read uses the fresh register and still writes its own")
	require.Equal(t, mk(isa.OpAddInt64, 11, 11, 10), compiledFunction.Body[6], "the read after the redefinition keeps its register")
}

func TestRenameFollowsAUseInAnExtensionWord(t *testing.T) {
	t.Parallel()
	const value, fresh = 5, 8
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), value, 255),
			isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 2, 3),
			mk(isa.OpExtension, value, value, 0),
			mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), value, 7),
			returnVoid(),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 8, isa.RegisterUint64: fresh},
	}
	require.True(t, renameDefinition(compiledFunction, newFunctionAnalysis(compiledFunction), 0, 1, isa.RegisterUint64, value))
	require.Equal(t, mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), fresh, 255), compiledFunction.Body[0], "the load writes a fresh register")
	require.Equal(t, mk(isa.OpExtension, fresh, value, 0), compiledFunction.Body[2],
		"the stored value follows the load, and the displacement byte that happens to equal the old register is left alone")
	require.Equal(t, mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), value, 7), compiledFunction.Body[3], "the later writer keeps its register")
}

func TestRenameRefusesAnExtensionWordWhoseLayoutIsNotRecorded(t *testing.T) {
	t.Parallel()
	const value = 5
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), value, 255),
			mk(isa.OpBindMethod, 1, 2, 0),
			mk(isa.OpExtension, value, 0, 0),
			mk(isa.OpDrillTier1, uint8(isa.SubOpLoadUint64Immediate), value, 7),
			returnVoid(),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterGeneral: 8, isa.RegisterUint64: 8},
	}
	original := slices.Clone(compiledFunction.Body)
	require.False(t, renameDefinition(compiledFunction, newFunctionAnalysis(compiledFunction), 0, 1, isa.RegisterUint64, value))
	require.Equal(t, original, compiledFunction.Body, "a byte that may or may not name the register cannot be rewritten")
}

func TestRenameRefusesARedefiningUseWhoseExtensionWordAlsoReadsTheRegister(t *testing.T) {
	t.Parallel()
	const index = 5
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			smallIntConst(index, 1),
			mk(isa.OpAddInt64, 6, index, index),
			mk(isa.OpGetStructFieldElementField, index, 2, 0),
			mk(isa.OpExtension, 0, 0, index),
			returnVoid(),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 8, isa.RegisterGeneral: 8},
	}
	original := slices.Clone(compiledFunction.Body)
	require.False(t, renameDefinition(compiledFunction, newFunctionAnalysis(compiledFunction), 0, 1, isa.RegisterInt64, index))
	require.Equal(t, original, compiledFunction.Body, "the element read indexes with the register it then overwrites")
}
