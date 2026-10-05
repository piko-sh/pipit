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
		mk(isa.OpAddInt, 3, 1, 2), truncate(3, isa.RegisterInt),
		mk(isa.OpSubIntConst, 4, 3, 0), nop(), truncate(4, isa.RegisterInt),
		isa.NewTier2Instruction(isa.SubOpTier2DecInt, 5), truncate(5, isa.RegisterInt),
		mk(isa.OpAddUint, 6, 1, 2), truncate(6, isa.RegisterUint),
		mk(isa.OpAddUint, 7, 1, 2),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, fuseNarrow32(body, map[int]bool{}))
	require.Equal(t, mk(isa.OpAddInt32, 3, 1, 2), body[0])
	require.Equal(t, mk(isa.OpSubInt32Const, 4, 3, 0), body[2])
	require.Equal(t, isa.NewTier2Instruction(isa.SubOpTier2DecInt32, 5), body[5])
	require.Equal(t, mk(isa.OpAddUint32, 6, 1, 2), body[7])
	for _, pc := range []int{1, 4, 6, 8} {
		require.True(t, isCanonicalNop(body[pc]), "truncation at %d folded", pc)
	}
	require.Equal(t, mk(isa.OpAddUint, 7, 1, 2), body[9], "an untruncated add stays")
}

func TestFuseNarrow32KeepsUnsafePairs(t *testing.T) {
	t.Parallel()
	ret := isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)
	for name, tc := range map[string]struct {
		body    []isa.Instruction
		targets map[int]bool
	}{
		"other register":   {body: []isa.Instruction{mk(isa.OpAddInt, 3, 1, 2), truncate(4, isa.RegisterInt), ret}},
		"other width":      {body: []isa.Instruction{mk(isa.OpAddInt, 3, 1, 2), mk(isa.OpTruncateNarrow, 3, 16, uint8(isa.RegisterInt)), ret}},
		"branch between":   {body: []isa.Instruction{mk(isa.OpAddInt, 3, 1, 2), truncate(3, isa.RegisterInt), ret}, targets: map[int]bool{1: true}},
		"masked uint tail": {body: []isa.Instruction{mk(isa.OpAddUint, 3, 1, 2), truncate(3, isa.RegisterUint), isa.NewTier1Instruction(isa.SubOpMoveUint, 0, 3), ret}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := slices.Clone(tc.body)
			require.False(t, fuseNarrow32(body, tc.targets))
			require.Equal(t, tc.body, body)
		})
	}
}
