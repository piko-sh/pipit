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

package app

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

func TestEnsureASMCallInfoBasesMatchesCompiledProgram(t *testing.T) {
	t.Parallel()
	root := compileExpression(t, `
func twice(x int) int { return x + x }
func thrice(x int) int { return twice(x) + x }
total := 0
for i := 0; i < 10; i++ {
	total += thrice(i)
}
total
`)
	bases := engine.EnsureASMCallInfoBases(root)
	again := engine.EnsureASMCallInfoBases(root)
	require.Len(t, bases, len(root.Functions)+1)
	require.Same(t, &bases[0], &again[0], "the array is built once per root")

	populated := 0
	for i, function := range root.Functions {
		require.Equal(t, function.ASMCallInfoBase, bases[i], "function %d", i)
		if bases[i] != 0 {
			populated++
		}
	}
	require.Positive(t, populated, "a program with calls must publish at least one table base")
	require.Zero(t, bases[len(bases)-1])

	vm := newTestVM(t)
	result, err := vm.Execute(root)
	require.NoError(t, err)
	require.EqualValues(t, 135, result)
	require.Same(t, &bases[0], &engine.EnsureASMCallInfoBases(root)[0], "executing the root does not rebuild the array")
}
