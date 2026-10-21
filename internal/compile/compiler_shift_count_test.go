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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestShiftCountIsCheckedOnlyWhenItCanBeNegative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		checked bool
	}{
		{name: "a signed count", source: "func f(x, n int) int { return x << n }", checked: true},
		{name: "a signed count in a compound assignment", source: "func f(x, n int) int { x >>= n; return x }", checked: true},
		{name: "an unsigned count", source: "func f(x int, n uint) int { return x << n }", checked: false},
		{name: "a constant count", source: "func f(x, _ int) int { x <<= 3; return x }", checked: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+test.source+"\n\nfunc main() { _ = f(1, 2) }\n")
			body := findCompiledFunction(t, root, "f").Body
			checked := slices.ContainsFunc(body, func(instruction isa.Instruction) bool {
				return isa.InstrIsTier2SubOp(instruction, isa.SubOpTier2CheckShiftCount)
			})
			require.Equal(t, test.checked, checked, "%s", findCompiledFunction(t, root, "f").Disassemble())
		})
	}
}

func TestCompoundShiftOfASliceElementKeepsTheTypedPath(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, "package main\n\nfunc f(s []int, n uint) {\n\ts[0] <<= 3\n\ts[1] >>= n\n}\n\nfunc main() { f([]int{1, 2}, 1) }\n")
	compiledFunction := findCompiledFunction(t, root, "f")
	boxed := slices.ContainsFunc(compiledFunction.Body, func(instruction isa.Instruction) bool {
		return instruction.Op == isa.OpGetElement || instruction.Op == isa.OpUnboxFromInterface
	})
	require.False(t, boxed, "a count in another bank still shifts the element in its own bank:\n%s", compiledFunction.Disassemble())
}
