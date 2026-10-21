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

func TestControlFlowEdgeListsAreCutFromOneBacking(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpJumpIfFalse, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		tier1Jump(0),
		returnVoid(),
	}
	require.True(t, program.SetJumpTarget(body, 1, 4))
	require.True(t, program.SetJumpTarget(body, 3, 0))

	require.Equal(t, [][]int{{3}, {0}, {1}, {2}, {1}}, buildAliasPredecessors(body),
		"a word's fall-through predecessor comes before the jumps that land on it")
	require.Equal(t, [][]int{{1}, {2, 4}, {3}, {0}, nil}, buildAliasSuccessors(body),
		"a word with no successor has no list")
}
