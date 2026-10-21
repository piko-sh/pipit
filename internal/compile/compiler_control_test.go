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

package compile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func countOpcode(compiledFunction *program.CompiledFunction, op isa.Opcode) int {
	count := 0
	for _, instruction := range compiledFunction.Body {
		if instruction.Op == op {
			count++
		}
	}
	return count
}

func TestALoopVariableWhoseAddressIsTakenGetsAFreshCellForEachIteration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		cells  int
	}{
		{
			name:   "an int loop variable",
			source: "func f() []*int { var ps []*int; for i := 0; i < 3; i++ { ps = append(ps, &i) }; return ps }",
			cells:  2,
		},
		{
			name:   "two loop variables, both addressed",
			source: "func f() ([]*int, []*string) { var ps []*int; var qs []*string; for i, s := 0, \"a\"; i < 3; i, s = i+1, s+\"b\" { ps = append(ps, &i); qs = append(qs, &s) }; return ps, qs }",
			cells:  4,
		},
		{
			name:   "a struct loop variable",
			source: "type pt struct{ x int }\nfunc f() []*pt { var ps []*pt; for p := (pt{}); p.x < 3; p.x++ { ps = append(ps, &p) }; return ps }",
			cells:  2,
		},
		{
			name:   "a variable declared before the loop is one variable",
			source: "func f() []*int { var ps []*int; i := 0; for ; i < 3; i++ { ps = append(ps, &i) }; return ps }",
			cells:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiled := findCompiledFunction(t, compileSnippet(t, "package main\n"+tt.source+"\n"), "f")

			require.Equal(t, tt.cells, countOpcode(compiled, isa.OpAllocateIndirect),
				"each per-iteration variable gets its cell at the declaration and a fresh one before the post statement")
		})
	}
}

func TestALoopInAFileDeclaringGoBefore122RefusesVariablesThatCouldBeTold(t *testing.T) {
	t.Parallel()

	_, err := tryCompileSnippet(t, "//go:build go1.21\n\npackage main\n\nfunc f() []*int { var ps []*int; for i := 0; i < 3; i++ { ps = append(ps, &i) }; return ps }\n")
	require.ErrorIs(t, err, fault.ErrCompileSharedLoopVariables,
		"such a file gives the loop one variable for every iteration, which the interpreter does not implement")

	_, err = tryCompileSnippet(t, "//go:build go1.21\n\npackage main\n\nfunc f() []func() int { var fs []func() int; for _, v := range []int{1, 2} { fs = append(fs, func() int { return v }) }; return fs }\n")
	require.ErrorIs(t, err, fault.ErrCompileSharedLoopVariables, "a range loop's captured variable is refused too")

	_, err = tryCompileSnippet(t, "//go:build go1.21\n\npackage main\n\nfunc f() int { n := 0; for i := 0; i < 3; i++ { n += i }; return n }\n")
	require.NoError(t, err, "a loop whose variables are neither captured nor addressed behaves the same either way")

	_, err = tryCompileSnippet(t, "//go:build go1.22\n\npackage main\n\nfunc f() []*int { var ps []*int; for i := 0; i < 3; i++ { ps = append(ps, &i) }; return ps }\n")
	require.NoError(t, err)
}
