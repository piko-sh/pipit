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
	"pipit.sh/pipit/internal/isa"
)

func TestFuseNarrow32FoldsTruncations(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpAddInt64, 3, 1, 2), truncate(3, isa.RegisterInt64),
		mk(isa.OpSubtractInt64Constant, 4, 3, 0), nop(), truncate(4, isa.RegisterInt64),
		isa.NewTier2Instruction(isa.SubOpTier2DecrementInt64, 5), truncate(5, isa.RegisterInt64),
		mk(isa.OpAddUint64, 6, 1, 2), truncate(6, isa.RegisterUint64),
		mk(isa.OpAddUint64, 7, 1, 2),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, fuseNarrow32(body, map[int]bool{}))
	require.Equal(t, mk(isa.OpAddInt32, 3, 1, 2), body[0])
	require.Equal(t, mk(isa.OpSubtractInt32Constant, 4, 3, 0), body[2])
	require.Equal(t, isa.NewTier2Instruction(isa.SubOpTier2DecrementInt32, 5), body[5])
	require.Equal(t, mk(isa.OpAddUint32, 6, 1, 2), body[7])
	for _, programCounter := range []int{1, 4, 6, 8} {
		require.True(t, isCanonicalNoOperation(body[programCounter]), "truncation at %d folded", programCounter)
	}
	require.Equal(t, mk(isa.OpAddUint64, 7, 1, 2), body[9], "an untruncated add stays")
}

func TestFuseNarrow32FoldsUnsignedProductsAndConstantSums(t *testing.T) {
	t.Parallel()
	constant := isa.NewInstruction(isa.OpExtension, 2, 0, 0)
	body := []isa.Instruction{
		mk(isa.OpMultiplyUint64, 3, 1, 2), truncate(3, isa.RegisterUint64),
		mk(isa.OpSubtractUint64, 4, 1, 2), nop(), truncate(4, isa.RegisterUint64),
		isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 5, 1), constant, truncate(5, isa.RegisterUint64),
		isa.NewTier1Instruction(isa.SubOpSubtractUint64Constant, 6, 1), constant, nop(), truncate(6, isa.RegisterUint64),
		isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 7, 1), constant, truncate(1, isa.RegisterUint64),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, fuseNarrow32(body, map[int]bool{}))
	require.Equal(t, mk(isa.OpMultiplyUint32, 3, 1, 2), body[0])
	require.Equal(t, mk(isa.OpSubtractUint32, 4, 1, 2), body[2])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 5, 1), body[5])
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpSubtractUint32Constant, 6, 1), body[8])
	for _, programCounter := range []int{6, 9} {
		require.Equal(t, constant, body[programCounter], "the extension word at %d still names the constant", programCounter)
	}
	for _, programCounter := range []int{1, 4, 7, 11} {
		require.True(t, isCanonicalNoOperation(body[programCounter]), "truncation at %d folded", programCounter)
	}
	require.Equal(t, isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 7, 1), body[12], "a sum whose own result is not truncated stays")
	require.Equal(t, truncate(1, isa.RegisterUint64), body[14])
}

func TestFuseNarrow32KeepsUnsafePairs(t *testing.T) {
	t.Parallel()
	ret := isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)
	for name, tc := range map[string]struct {
		body    []isa.Instruction
		targets map[int]bool
	}{
		"other register":   {body: []isa.Instruction{mk(isa.OpAddInt64, 3, 1, 2), truncate(4, isa.RegisterInt64), ret}},
		"other width":      {body: []isa.Instruction{mk(isa.OpAddInt64, 3, 1, 2), mk(isa.OpTruncateToWidth, 3, 16, uint8(isa.RegisterInt64)), ret}},
		"branch between":   {body: []isa.Instruction{mk(isa.OpAddInt64, 3, 1, 2), truncate(3, isa.RegisterInt64), ret}, targets: map[int]bool{1: true}},
		"masked uint tail": {body: []isa.Instruction{mk(isa.OpAddUint64, 3, 1, 2), truncate(3, isa.RegisterUint64), isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 3), ret}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := slices.Clone(tc.body)
			require.False(t, fuseNarrow32(body, tc.targets))
			require.Equal(t, tc.body, body)
		})
	}
}
