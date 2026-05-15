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
	"pipit.sh/pipit/internal/isa"
)

const destructureTypes = `
type point struct {
	X int
	Y int
	W uint64
	F float64
	B bool
	S string
	P *int
}

type holder struct {
	items []point
}
`

var fusedDestructureOpcodes = []isa.Opcode{
	isa.OpSliceIndexStructFieldInt,
	isa.OpSliceIndexStructFieldUint,
	isa.OpSliceIndexStructFieldFloat,
	isa.OpSliceIndexStructFieldBool,
	isa.OpSliceIndexStructFieldString,
	isa.OpGetStructFieldSliceIndexScalar,
}

func requireNoFusedDestructure(t *testing.T, compiled *program.CompiledFunction) {
	t.Helper()
	for _, op := range fusedDestructureOpcodes {
		require.Falsef(t, bodyContainsOpcode(compiled, op),
			"the declaration does not qualify for destructuring, so %s must not be emitted", op)
	}
}

func TestSliceElementDeclarationDestructuresItsReadFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []isa.Opcode
	}{
		{
			name:   "a single integer field read",
			source: "func f(s []point, i int) int { p := s[i]; return p.X }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldInt},
		},
		{
			name:   "two integer fields read",
			source: "func f(s []point, i int) int { p := s[i]; return p.X + p.Y }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldInt},
		},
		{
			name:   "an unsigned field read",
			source: "func f(s []point, i int) uint64 { p := s[i]; return p.W }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldUint},
		},
		{
			name:   "a floating-point field read",
			source: "func f(s []point, i int) float64 { p := s[i]; return p.F }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldFloat},
		},
		{
			name:   "a boolean field read",
			source: "func f(s []point, i int) bool { p := s[i]; return p.B }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldBool},
		},
		{
			name:   "fields of three different banks read together",
			source: "func f(s []point, i int) (int, float64, bool) { p := s[i]; return p.X, p.F, p.B }",
			want: []isa.Opcode{
				isa.OpSliceIndexStructFieldInt,
				isa.OpSliceIndexStructFieldFloat,
				isa.OpSliceIndexStructFieldBool,
			},
		},
		{
			name:   "a field read inside a loop body",
			source: "func f(s []point, i int) int { p := s[i]; n := 0; for k := 0; k < 3; k++ { n += p.X }; return n }",
			want:   []isa.Opcode{isa.OpSliceIndexStructFieldInt},
		},
		{
			name:   "a slice field indexed through a pointer receiver",
			source: "func f(h *holder, i int) int { p := h.items[i]; return p.X }",
			want:   []isa.Opcode{isa.OpGetStructFieldSliceIndexScalar},
		},
		{
			name:   "two fields read through a pointer receiver",
			source: "func f(h *holder, i int) (int, bool) { p := h.items[i]; return p.X, p.B }",
			want:   []isa.Opcode{isa.OpGetStructFieldSliceIndexScalar},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+destructureTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			for _, op := range tt.want {
				require.Truef(t, bodyContainsOpcode(compiled, op),
					"every consumed field loads straight into its bank, so %s must be emitted", op)
			}
		})
	}
}

func TestSliceElementDeclarationKeepsTheBoxedCopyWhenDestructuringWouldChangeMeaning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "a string field is not a full-width scalar",
			source: "func f(s []point, i int) string { p := s[i]; return p.S }",
		},
		{
			name:   "a pointer field is not a full-width scalar",
			source: "func f(s []point, i int) *int { p := s[i]; return p.P }",
		},
		{
			name:   "the element is passed on whole",
			source: "func g(point) int { return 0 }\nfunc f(s []point, i int) int { p := s[i]; return g(p) }",
		},
		{
			name:   "a field is written through the local",
			source: "func f(s []point, i int) int { p := s[i]; p.X = 1; return p.X }",
		},
		{
			name:   "a field is incremented through the local",
			source: "func f(s []point, i int) int { p := s[i]; p.X++; return p.X }",
		},
		{
			name:   "the local is captured by a closure",
			source: "func f(s []point, i int) int { p := s[i]; g := func() int { return p.X }; return g() }",
		},
		{
			name:   "the address of a field is taken",
			source: "func f(s []point, i int) int { p := s[i]; q := &p.X; return *q }",
		},
		{
			name:   "the index is unsigned rather than an int",
			source: "func f(s []point, i uint) int { p := s[i]; return p.X }",
		},
		{
			name:   "the collection is an array rather than a slice",
			source: "func f(a [4]point, i int) int { p := a[i]; return p.X }",
		},
		{
			name:   "the collection is a map rather than a slice",
			source: "func f(m map[int]point, i int) int { p := m[i]; return p.X }",
		},
		{
			name:   "the element is not a struct",
			source: "func f(s []int, i int) int { p := s[i]; return p }",
		},
		{
			name:   "the declaration binds the blank identifier alongside a value",
			source: "func f(s []point, i int) int { _, n := s[i], 1; return n }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+destructureTypes+"\n"+tt.source+"\n")

			requireNoFusedDestructure(t, findCompiledFunction(t, root, "f"))
		})
	}
}
