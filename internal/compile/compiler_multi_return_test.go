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

const multiReturnTypes = `
type node struct {
	N int
	S string
}

type inner struct {
	N int
}

func (i inner) Split() (int, int) { return i.N, i.N }

type outer struct {
	inner
	Tag string
}

func (n node) Pair() (int, string) { return n.N, n.S }

func (n *node) PtrPair() (int, string) { return n.N, n.S }

func (n node) Three() (int, string, bool) { return n.N, n.S, true }

type splitter interface {
	Pair() (int, string)
}

func two() (int, string) { return 1, "a" }

func three() (int, string, bool) { return 1, "a", true }

func choose() func() (int, string) { return two }
`

func TestBindingSeveralResultsOfACallWorksForEveryCalleeForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a plain function declared here", source: "func f() int { a, _ := two(); return a }"},
		{name: "a plain function with three results", source: "func f() bool { _, _, c := three(); return c }"},
		{name: "a value method", source: "func f(n node) int { a, _ := n.Pair(); return a }"},
		{name: "a pointer method", source: "func f(n *node) int { a, _ := n.PtrPair(); return a }"},
		{name: "a value method reached through a pointer", source: "func f(n *node) int { a, _ := n.Pair(); return a }"},
		{name: "a method promoted from an embedded field", source: "func f(o outer) int { a, _ := o.Split(); return a }"},
		{name: "a method reached through the embedded name", source: "func f(o outer) int { a, _ := o.inner.Split(); return a }"},
		{name: "a method on an interface", source: "func f(s splitter) int { a, _ := s.Pair(); return a }"},
		{name: "a function held in a variable", source: "func f() int { g := two; a, _ := g(); return a }"},
		{name: "a function returned by another call", source: "func f() int { a, _ := choose()(); return a }"},
		{name: "a function value taken from a parameter", source: "func f(g func() (int, string)) int { a, _ := g(); return a }"},
		{name: "results assigned to existing variables", source: "func f() int { var a int; var b string; a, b = two(); _ = b; return a }"},
		{name: "results assigned over the blank identifier", source: "func f() int { var a int; a, _ = two(); return a }"},
		{name: "every result discarded", source: "func f() { _, _ = two() }"},
		{name: "results bound inside a loop body", source: "func f() int { n := 0; for i := 0; i < 2; i++ { a, _ := two(); n += a }; return n }"},
		{name: "results of a method bound inside a loop body", source: "func f(n node) int { t := 0; for i := 0; i < 2; i++ { a, _ := n.Pair(); t += a }; return t }"},
		{name: "results bound to a spilled target", source: "func f() int { a, b := two(); g := func() int { return a + len(b) }; return g() }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+multiReturnTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a call with several results has to place each one where its target lives")
		})
	}
}

func TestReceivingFromAChannelWithACommaOkTargetEmitsTheReceiveWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an integer channel", source: "func f(c chan int) int { v, ok := <-c; if !ok { return -1 }; return v }"},
		{name: "a string channel", source: "func f(c chan string) string { v, ok := <-c; if !ok { return \"\" }; return v }"},
		{name: "a float channel", source: "func f(c chan float64) float64 { v, ok := <-c; if !ok { return 0 }; return v }"},
		{name: "a struct channel", source: "func f(c chan node) int { v, ok := <-c; if !ok { return -1 }; return v.N }"},
		{name: "a receive-only channel", source: "func f(c <-chan int) int { v, ok := <-c; if !ok { return -1 }; return v }"},
		{name: "targets that already exist", source: "func f(c chan int) int { var v int; var ok bool; v, ok = <-c; if !ok { return -1 }; return v }"},
		{name: "a parenthesised receive", source: "func f(c chan int) int { v, ok := (<-c); if !ok { return -1 }; return v }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+multiReturnTypes+"\n"+tt.source+"\n")

			require.True(t, bodyContainsTier1SubOp(findCompiledFunction(t, root, "f"), isa.SubOpChannelReceive),
				"the comma-ok form of a receive is the same word as a plain one; only the second result is kept")
		})
	}
}

func TestLookingUpAMapWithACommaOkTargetUsesTheTypedProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{
			name:   "an integer key and integer value",
			source: "func f(m map[int]int, k int) int { v, ok := m[k]; if !ok { return -1 }; return v }",
			want:   isa.OpMapIndexOkIntInt,
		},
		{
			name:   "an integer key and string value",
			source: "func f(m map[int]string, k int) string { v, ok := m[k]; if !ok { return \"\" }; return v }",
			want:   isa.OpMapIndexOkIntString,
		},
		{
			name:   "a string key and integer value",
			source: "func f(m map[string]int, k string) int { v, ok := m[k]; if !ok { return -1 }; return v }",
			want:   isa.OpMapIndexOkStringInt,
		},
		{
			name:   "a string key and string value",
			source: "func f(m map[string]string, k string) string { v, ok := m[k]; if !ok { return \"\" }; return v }",
			want:   isa.OpMapIndexOkStringString,
		},
		{
			name:   "a string key and struct value",
			source: "func f(m map[string]node, k string) int { v, ok := m[k]; if !ok { return -1 }; return v.N }",
			want:   isa.OpMapIndexOkStringGeneral,
		},
		{
			name:   "an integer key and struct value",
			source: "func f(m map[int]node, k int) int { v, ok := m[k]; if !ok { return -1 }; return v.N }",
			want:   isa.OpMapIndexOkIntGeneral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+multiReturnTypes+"\n"+tt.source+"\n")

			require.Truef(t, bodyContainsOpcode(findCompiledFunction(t, root, "f"), tt.want),
				"presence and value come from one probe, so %s must replace a lookup followed by a test", tt.want)
		})
	}
}

func TestLookingUpAMapWhoseBanksHaveNoTypedProbeStillCompiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a float key", source: "func f(m map[float64]int, k float64) int { v, ok := m[k]; if !ok { return -1 }; return v }"},
		{name: "a boolean key", source: "func f(m map[bool]int, k bool) int { v, ok := m[k]; if !ok { return -1 }; return v }"},
		{name: "a float value", source: "func f(m map[int]float64, k int) float64 { v, ok := m[k]; if !ok { return 0 }; return v }"},
		{name: "a boolean value", source: "func f(m map[int]bool, k int) bool { v, ok := m[k]; if !ok { return false }; return v }"},
		{name: "a struct key", source: "func f(m map[node]int, k node) int { v, ok := m[k]; if !ok { return -1 }; return v }"},
		{name: "targets that already exist", source: "func f(m map[int]int, k int) int { var v int; var ok bool; v, ok = m[k]; if !ok { return -1 }; return v }"},
		{name: "the value discarded", source: "func f(m map[int]int, k int) bool { _, ok := m[k]; return ok }"},
		{name: "the presence flag discarded", source: "func f(m map[int]int, k int) int { v, _ := m[k]; return v }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+multiReturnTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a bank pair with no typed probe falls back to the boxed lookup rather than refusing")
		})
	}
}

func TestATypeAssertionWithACommaOkTargetCompilesForEveryTargetBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an integer", source: "func f(v any) int { n, ok := v.(int); if !ok { return -1 }; return n }"},
		{name: "a string", source: "func f(v any) string { s, ok := v.(string); if !ok { return \"\" }; return s }"},
		{name: "a float", source: "func f(v any) float64 { x, ok := v.(float64); if !ok { return 0 }; return x }"},
		{name: "a boolean", source: "func f(v any) bool { b, ok := v.(bool); if !ok { return false }; return b }"},
		{name: "a struct", source: "func f(v any) int { n, ok := v.(node); if !ok { return -1 }; return n.N }"},
		{name: "a pointer to a struct", source: "func f(v any) int { n, ok := v.(*node); if !ok { return -1 }; return n.N }"},
		{name: "a slice", source: "func f(v any) int { xs, ok := v.([]int); if !ok { return -1 }; return len(xs) }"},
		{name: "an interface", source: "func f(v any) int { s, ok := v.(splitter); if !ok { return -1 }; n, _ := s.Pair(); return n }"},
		{name: "targets that already exist", source: "func f(v any) int { var n int; var ok bool; n, ok = v.(int); if !ok { return -1 }; return n }"},
		{name: "the value discarded", source: "func f(v any) bool { _, ok := v.(int); return ok }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+multiReturnTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"the comma-ok form of an assertion must not panic, so it compiles to a test rather than a check")
		})
	}
}
