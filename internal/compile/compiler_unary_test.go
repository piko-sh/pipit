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

	"pipit.sh/pipit/internal/isa"
)

func TestUnsignedNegationKeepsItsResultRegister(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		source string

		op isa.Opcode
	}{
		{name: "negation plus a conversion", source: "func f(x uint64, k int) uint64 { return -x + uint64(k) }", op: isa.OpAddUint64},
		{name: "negation times a conversion", source: "func f(x uint64, k int) uint64 { return -x * uint64(k) }", op: isa.OpMultiplyUint64},
		{name: "negation times a local", source: "func f(x, y uint64) uint64 { z := -x * (y + 1); return z }", op: isa.OpMultiplyUint64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")
			found := false
			for _, instruction := range compiled.Body {
				if instruction.Op != tt.op {
					continue
				}
				found = true
				require.NotEqual(t, instruction.B, instruction.C, "the negation's register was reused for the other operand")
			}
			require.True(t, found, "f has no %s", tt.op)
		})
	}
}
