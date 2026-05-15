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

const compoundTypes = `
type counters struct {
	N  int
	U  uint64
	F  float64
	B  bool
	S  string
	Sl []int
}

type wrapper struct {
	counters
	Tag string
}

var globalInt int
var globalFloat float64
var globalString string
`

func TestCompoundAssignmentLowersToTheBankOfItsTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "an integer local", source: "func f(a, b int) int { a += b; return a }", want: isa.OpAddInt},
		{name: "an integer local subtracted", source: "func f(a, b int) int { a -= b; return a }", want: isa.OpSubInt},
		{name: "an integer local multiplied", source: "func f(a, b int) int { a *= b; return a }", want: isa.OpMulInt},
		{name: "an integer local divided", source: "func f(a, b int) int { a /= b; return a }", want: isa.OpDivInt},
		{name: "an integer local reduced modulo", source: "func f(a, b int) int { a %= b; return a }", want: isa.OpRemInt},
		{name: "an integer local masked", source: "func f(a, b int) int { a &= b; return a }", want: isa.OpBitAnd},
		{name: "an integer local unioned", source: "func f(a, b int) int { a |= b; return a }", want: isa.OpBitOr},
		{name: "an integer local exclusive-ored", source: "func f(a, b int) int { a ^= b; return a }", want: isa.OpBitXor},
		{name: "an integer local cleared of bits", source: "func f(a, b int) int { a &^= b; return a }", want: isa.OpBitAndNot},
		{name: "an integer local shifted left", source: "func f(a, b int) int { a <<= b; return a }", want: isa.OpShiftLeft},
		{name: "an integer local shifted right", source: "func f(a, b int) int { a >>= b; return a }", want: isa.OpShiftRight},
		{name: "a floating-point local", source: "func f(a, b float64) float64 { a += b; return a }", want: isa.OpAddFloat},
		{name: "a floating-point local divided", source: "func f(a, b float64) float64 { a /= b; return a }", want: isa.OpDivFloat},
		{name: "an unsigned local", source: "func f(a, b uint64) uint64 { a += b; return a }", want: isa.OpAddUint},
		{name: "an unsigned local shifted", source: "func f(a, b uint64) uint64 { a <<= b; return a }", want: isa.OpShiftLeftUint},
		{name: "a string local", source: "func f(a, b string) string { a += b; return a }", want: isa.OpConcatString},
		{name: "a value behind a pointer", source: "func f(p *int, b int) { *p += b }", want: isa.OpAddInt},
		{name: "a floating-point value behind a pointer", source: "func f(p *float64, b float64) { *p += b }", want: isa.OpAddFloat},
		{name: "a string behind a pointer", source: "func f(p *string, b string) { *p += b }", want: isa.OpConcatString},
		{name: "a package-level integer", source: "func f(b int) { globalInt += b }", want: isa.OpAddInt},
		{name: "a package-level float", source: "func f(b float64) { globalFloat *= b }", want: isa.OpMulFloat},
		{name: "a package-level string", source: "func f(b string) { globalString += b }", want: isa.OpConcatString},
		{name: "a slice element", source: "func f(s []int, i, b int) { s[i] += b }", want: isa.OpAddInt},
		{name: "a floating-point slice element", source: "func f(s []float64, i int, b float64) { s[i] += b }", want: isa.OpAddFloat},
		{name: "a string slice element", source: "func f(s []string, i int, b string) { s[i] += b }", want: isa.OpConcatString},
		{name: "an array element", source: "func f(a *[4]int, i, b int) { a[i] += b }", want: isa.OpAddInt},
		{name: "a map element of a non-fused operator", source: "func f(m map[int]int, k, b int) { m[k] -= b }", want: isa.OpSubInt},
		{name: "a map element of a non-integer value", source: "func f(m map[string]string, k, b string) { m[k] += b }", want: isa.OpConcatString},
		{name: "a struct field", source: "func f(c *counters, b int) { c.N += b }", want: isa.OpAddInt},
		{name: "a floating-point struct field", source: "func f(c *counters, b float64) { c.F += b }", want: isa.OpAddFloat},
		{name: "a string struct field", source: "func f(c *counters, b string) { c.S += b }", want: isa.OpConcatString},
		{name: "a promoted field of an embedded struct", source: "func f(w *wrapper, b int) { w.N += b }", want: isa.OpAddInt},
		{name: "a field reached through the embedded name", source: "func f(w *wrapper, b int) { w.counters.N += b }", want: isa.OpAddInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"a compound operator reads, applies and writes back in the target's own bank, so %s must be emitted", tt.want)
		})
	}
}

func TestCompoundAssignmentOnACapturedVariableGoesThroughItsUpvalue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{
			name:   "an integer read back and written through the cell",
			source: "func f() func(int) { n := 0; return func(b int) { n += b } }",
			want:   isa.OpAddInt,
		},
		{
			name:   "a string read back and written through the cell",
			source: "func f() func(string) { s := \"\"; return func(b string) { s += b } }",
			want:   isa.OpConcatString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			closure := findCompiledFunction(t, root, "<closure>")

			require.Truef(t, bodyContainsOpcode(closure, tt.want),
				"the captured variable keeps its bank, so %s must be emitted inside the closure", tt.want)
			require.True(t, bodyContainsOpcode(closure, isa.OpGetUpvalue),
				"the current value has to be read out of the shared cell before the operator applies")
			require.True(t, bodyContainsOpcode(closure, isa.OpSetUpvalue),
				"the result has to be written back into the shared cell, not left in a register")
		})
	}
}

func TestCompoundAssignmentOnAScalarFieldUsesTheDirectFieldOps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		wantGet isa.Opcode
		wantSet isa.Opcode
	}{
		{
			name:    "an integer field",
			source:  "func f(c *counters, b int) { c.N += b }",
			wantGet: isa.OpGetStructFieldIntT0,
			wantSet: isa.OpSetStructFieldIntT0,
		},
		{
			name:    "an unsigned field",
			source:  "func f(c *counters, b uint64) { c.U += b }",
			wantGet: isa.OpGetStructFieldUint,
			wantSet: isa.OpSetStructFieldUint,
		},
		{
			name:    "a floating-point field",
			source:  "func f(c *counters, b float64) { c.F += b }",
			wantGet: isa.OpGetStructFieldFloat,
			wantSet: isa.OpSetStructFieldFloat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.wantGet),
				"a scalar field of a resolved layout is read with %s rather than boxed through OpGetField", tt.wantGet)
			require.Truef(t, bodyContainsOpcode(compiled, tt.wantSet),
				"the result is written back with %s rather than boxed through OpSetField", tt.wantSet)
			require.False(t, bodyContainsOpcode(compiled, isa.OpGetField),
				"the boxed field read is the fallback, and must not appear once the direct path applies")
		})
	}
}

func TestAddingToAMapEntryFusesIntoASingleProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{
			name:   "an integer key written with a compound operator",
			source: "func f(m map[int]int, k, b int) { m[k] += b }",
			want:   isa.OpMapAddIntInt,
		},
		{
			name:   "a string key written with a compound operator",
			source: "func f(m map[string]int, k string, b int) { m[k] += b }",
			want:   isa.OpMapAddStringInt,
		},
		{
			name:   "an integer key written as a read-modify-write assignment",
			source: "func f(m map[int]int, k, b int) { m[k] = m[k] + b }",
			want:   isa.OpMapAddIntInt,
		},
		{
			name:   "an integer key with the entry on the right of the sum",
			source: "func f(m map[int]int, k, b int) { m[k] = b + m[k] }",
			want:   isa.OpMapAddIntInt,
		},
		{
			name:   "a string key written as a read-modify-write assignment",
			source: "func f(m map[string]int, k string, b int) { m[k] = m[k] + b }",
			want:   isa.OpMapAddStringInt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"adding to an entry hashes the key once, so %s must replace the get-add-set trio", tt.want)
			require.False(t, bodyContainsOpcode(compiled, isa.OpMapSetIntInt),
				"a fused add writes through its own opcode and needs no separate store")
		})
	}
}

func TestReadModifyWriteIsOnlyRewrittenWhenBothSidesNameTheSameEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "the read names a different key",
			source: "func f(m map[int]int, j, k, b int) { m[k] = m[j] + b }",
		},
		{
			name:   "the read names a different map",
			source: "func f(m, n map[int]int, k, b int) { m[k] = n[k] + b }",
		},
		{
			name:   "the operator is not addition",
			source: "func f(m map[int]int, k, b int) { m[k] = m[k] - b }",
		},
		{
			name:   "the right-hand side is not a sum at all",
			source: "func f(m map[int]int, k, b int) { m[k] = b }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.False(t, bodyContainsOpcode(compiled, isa.OpMapAddIntInt),
				"the fused add changes which entry is read, so it may only replace a genuine self-referential sum")
		})
	}
}
