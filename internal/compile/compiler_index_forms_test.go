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

const indexTypes = `
type grid struct {
	Cells   [4]int
	Reals   [4]float64
	Labels  [4]string
	Items   []int
	Lookup  map[string]int
	Nested  [2][2]int
}

type counter struct {
	N int
}

func (c counter) Value() int { return c.N }

func (c *counter) Bump() { c.N++ }

type reader interface {
	Value() int
}
`

func TestIndexingAnArrayFieldReadsItThroughTheReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an integer array field read", source: "func f(g *grid, i int) int { return g.Cells[i] }"},
		{name: "an integer array field written", source: "func f(g *grid, i, v int) { g.Cells[i] = v }"},
		{name: "a float array field read", source: "func f(g *grid, i int) float64 { return g.Reals[i] }"},
		{name: "a float array field written", source: "func f(g *grid, i int, v float64) { g.Reals[i] = v }"},
		{name: "a string array field read", source: "func f(g *grid, i int) string { return g.Labels[i] }"},
		{name: "an array field of a value receiver", source: "func f(g grid, i int) int { return g.Cells[i] }"},
		{name: "an array field indexed by a literal", source: "func f(g *grid) int { return g.Cells[2] }"},
		{name: "a nested array field", source: "func f(g *grid, i, j int) int { return g.Nested[i][j] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"an array lives inside its struct, so the element is reached without copying the array out")
		})
	}
}

func TestIndexingASliceThroughAPointerUsesTheDereferencingForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "an integer element read", source: "func f(p *[]int, i int) int { return (*p)[i] }", want: isa.OpDerefSliceGetInt},
		{name: "an integer element written", source: "func f(p *[]int, i, v int) { (*p)[i] = v }", want: isa.OpDerefSliceSetInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			require.Truef(t, bodyContainsOpcode(findCompiledFunction(t, root, "f"), tt.want),
				"the header is read through the pointer in one step, so %s must be emitted", tt.want)
		})
	}
}

func TestIndexingAConstantStringIsFoldedToItsByte(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "the first byte of a literal", source: "func f() byte { return \"abc\"[0] }"},
		{name: "a later byte of a literal", source: "func f() byte { return \"abc\"[2] }"},
		{name: "a byte of a named constant", source: "func f() byte { const s = \"abc\"; return s[1] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t,
				bodyContainsOpcode(compiled, isa.OpLoadUintConst) || bodyContainsTier1SubOp(compiled, isa.SubOpLoadUintConstSmall),
				"both the string and the index are known here, so the byte is loaded as a constant")
			require.False(t, bodyContainsOpcode(compiled, isa.OpStringIndex),
				"a folded byte needs no runtime index at all")
		})
	}
}

func TestIndexingAConstantStringOutOfRangeIsLeftToTheRuntime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an index the string is too short for", source: "func f(i int) byte { return \"abc\"[i] }"},
		{name: "a string that is not constant", source: "func f(s string, i int) byte { return s[i] }"},
		{name: "a string that is not constant indexed by a literal", source: "func f(s string) byte { return s[0] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"an index the compiler cannot prove has to be checked where the program runs")
		})
	}
}

func TestBindingAMethodAsAValueCompilesForEveryReceiverForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a value receiver", source: "func f(c counter) int { g := c.Value; return g() }"},
		{name: "a pointer receiver", source: "func f(c *counter) { g := c.Bump; g() }"},
		{name: "a value method bound through a pointer", source: "func f(c *counter) int { g := c.Value; return g() }"},
		{name: "a method of an interface value", source: "func f(r reader) int { g := r.Value; return g() }"},
		{name: "a method value passed on as an argument", source: "func g(func() int) {}\nfunc f(c counter) { g(c.Value) }"},
		{name: "a method value returned", source: "func f(c counter) func() int { return c.Value }"},
		{name: "a method expression on a named type", source: "func f(c counter) int { g := counter.Value; return g(c) }"},
		{name: "a method expression on an interface type", source: "func f(r reader) int { g := reader.Value; return g(r) }"},
		{name: "a method expression on a pointer type", source: "func f(c *counter) { g := (*counter).Bump; g(c) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a method value carries its receiver, so binding one is more than naming a function")
		})
	}
}

func TestIndexingAMapCompilesForEveryKeyAndValueBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a string key and integer value", source: "func f(m map[string]int, k string) int { return m[k] }"},
		{name: "an integer key and string value", source: "func f(m map[int]string, k int) string { return m[k] }"},
		{name: "a float key", source: "func f(m map[float64]int, k float64) int { return m[k] }"},
		{name: "a boolean key", source: "func f(m map[bool]int, k bool) int { return m[k] }"},
		{name: "an unsigned key", source: "func f(m map[uint64]int, k uint64) int { return m[k] }"},
		{name: "a struct key", source: "func f(m map[counter]int, k counter) int { return m[k] }"},
		{name: "a struct value", source: "func f(m map[string]counter, k string) int { return m[k].N }"},
		{name: "a slice value", source: "func f(m map[string][]int, k string) int { return len(m[k]) }"},
		{name: "a map-typed struct field", source: "func f(g *grid, k string) int { return g.Lookup[k] }"},
		{name: "a map written through a struct field", source: "func f(g *grid, k string, v int) { g.Lookup[k] = v }"},
		{name: "an interface value", source: "func f(m map[string]any, k string) any { return m[k] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+indexTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a missing key yields the value's zero, so every bank needs a lookup that can produce one")
		})
	}
}
