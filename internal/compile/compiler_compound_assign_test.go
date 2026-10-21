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
	"cmp"
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
		{name: "an integer local", source: "func f(a, b int) int { a += b; return a }", want: isa.OpAddInt64},
		{name: "an integer local subtracted", source: "func f(a, b int) int { a -= b; return a }", want: isa.OpSubtractInt64},
		{name: "an integer local multiplied", source: "func f(a, b int) int { a *= b; return a }", want: isa.OpMultiplyInt64},
		{name: "an integer local divided", source: "func f(a, b int) int { a /= b; return a }", want: isa.OpDivideInt64},
		{name: "an integer local reduced modulo", source: "func f(a, b int) int { a %= b; return a }", want: isa.OpRemainderInt64},
		{name: "an integer local masked", source: "func f(a, b int) int { a &= b; return a }", want: isa.OpBitAndInt64},
		{name: "an integer local unioned", source: "func f(a, b int) int { a |= b; return a }", want: isa.OpBitOrInt64},
		{name: "an integer local exclusive-ored", source: "func f(a, b int) int { a ^= b; return a }", want: isa.OpBitXorInt64},
		{name: "an integer local cleared of bits", source: "func f(a, b int) int { a &^= b; return a }", want: isa.OpBitAndNotInt64},
		{name: "an integer local shifted left", source: "func f(a, b int) int { a <<= b; return a }", want: isa.OpShiftLeftInt64},
		{name: "an integer local shifted right", source: "func f(a, b int) int { a >>= b; return a }", want: isa.OpShiftRightInt64},
		{name: "a floating-point local", source: "func f(a, b float64) float64 { a += b; return a }", want: isa.OpAddFloat64},
		{name: "a floating-point local divided", source: "func f(a, b float64) float64 { a /= b; return a }", want: isa.OpDivideFloat64},
		{name: "an unsigned local", source: "func f(a, b uint64) uint64 { a += b; return a }", want: isa.OpAddUint64},
		{name: "an unsigned local shifted", source: "func f(a, b uint64) uint64 { a <<= b; return a }", want: isa.OpShiftLeftUint64},
		{name: "a string local", source: "func f(a, b string) string { a += b; return a }", want: isa.OpConcatenateString},
		{name: "a value behind a pointer", source: "func f(p *int, b int) { *p += b }", want: isa.OpAddInt64},
		{name: "a floating-point value behind a pointer", source: "func f(p *float64, b float64) { *p += b }", want: isa.OpAddFloat64},
		{name: "a string behind a pointer", source: "func f(p *string, b string) { *p += b }", want: isa.OpConcatenateString},
		{name: "a package-level integer", source: "func f(b int) { globalInt += b }", want: isa.OpAddInt64},
		{name: "a package-level float", source: "func f(b float64) { globalFloat *= b }", want: isa.OpMultiplyFloat64},
		{name: "a package-level string", source: "func f(b string) { globalString += b }", want: isa.OpConcatenateString},
		{name: "a slice element", source: "func f(s []int, i, b int) { s[i] += b }", want: isa.OpAddInt64},
		{name: "a floating-point slice element", source: "func f(s []float64, i int, b float64) { s[i] += b }", want: isa.OpAddFloat64},
		{name: "a string slice element", source: "func f(s []string, i int, b string) { s[i] += b }", want: isa.OpConcatenateString},
		{name: "an array element", source: "func f(a *[4]int, i, b int) { a[i] += b }", want: isa.OpAddInt64},
		{name: "a map element of a non-fused operator", source: "func f(m map[int]int, k, b int) { m[k] -= b }", want: isa.OpSubtractInt64},
		{name: "a map element of a non-integer value", source: "func f(m map[string]string, k, b string) { m[k] += b }", want: isa.OpConcatenateString},
		{name: "a struct field", source: "func f(c *counters, b int) { c.N += b }", want: isa.OpAddInt64},
		{name: "a floating-point struct field", source: "func f(c *counters, b float64) { c.F += b }", want: isa.OpAddFloat64},
		{name: "a string struct field", source: "func f(c *counters, b string) { c.S += b }", want: isa.OpConcatenateString},
		{name: "a promoted field of an embedded struct", source: "func f(w *wrapper, b int) { w.N += b }", want: isa.OpAddInt64},
		{name: "a field reached through the embedded name", source: "func f(w *wrapper, b int) { w.counters.N += b }", want: isa.OpAddInt64},
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
			want:   isa.OpAddInt64,
		},
		{
			name:   "a string read back and written through the cell",
			source: "func f() func(string) { s := \"\"; return func(b string) { s += b } }",
			want:   isa.OpConcatenateString,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			closure := findCompiledFunction(t, root, "<closure>")

			require.Truef(t, bodyContainsOpcode(closure, tt.want),
				"the captured variable keeps its bank, so %s must be emitted inside the closure", tt.want)
			require.True(t, bodyContainsOpcode(closure, isa.OpGetCapturedVariable),
				"the current value has to be read out of the shared cell before the operator applies")
			require.True(t, bodyContainsOpcode(closure, isa.OpSetCapturedVariable),
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
			wantGet: isa.OpGetStructFieldInt64,
			wantSet: isa.OpSetStructFieldInt64,
		},
		{
			name:    "an unsigned field",
			source:  "func f(c *counters, b uint64) { c.U += b }",
			wantGet: isa.OpGetStructFieldUint64,
			wantSet: isa.OpSetStructFieldUint64,
		},
		{
			name:    "a floating-point field",
			source:  "func f(c *counters, b float64) { c.F += b }",
			wantGet: isa.OpGetStructFieldFloat64,
			wantSet: isa.OpSetStructFieldFloat64,
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
			want:   isa.OpAddToMapInt64Int64,
		},
		{
			name:   "a string key written with a compound operator",
			source: "func f(m map[string]int, k string, b int) { m[k] += b }",
			want:   isa.OpAddToMapStringInt64,
		},
		{
			name:   "an integer key written as a read-modify-write assignment",
			source: "func f(m map[int]int, k, b int) { m[k] = m[k] + b }",
			want:   isa.OpAddToMapInt64Int64,
		},
		{
			name:   "an integer key with the entry on the right of the sum",
			source: "func f(m map[int]int, k, b int) { m[k] = b + m[k] }",
			want:   isa.OpAddToMapInt64Int64,
		},
		{
			name:   "a string key written as a read-modify-write assignment",
			source: "func f(m map[string]int, k string, b int) { m[k] = m[k] + b }",
			want:   isa.OpAddToMapStringInt64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+compoundTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"adding to an entry hashes the key once, so %s must replace the get-add-set trio", tt.want)
			require.False(t, bodyContainsOpcode(compiled, isa.OpSetMapInt64Int64),
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

			require.False(t, bodyContainsOpcode(compiled, isa.OpAddToMapInt64Int64),
				"the fused add changes which entry is read, so it may only replace a genuine self-referential sum")
		})
	}
}

func TestCompoundAssignmentTruncatesItsResultOnlyWhereTheStoreKeepsEveryBit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		function  string
		truncates bool
	}{
		{name: "a slice of bytes", source: "func f(s []byte, i int, v byte) { s[i] ^= v }"},
		{name: "an array of bytes", source: "func f(s *[32]byte, i int, v byte) { s[i] += v }"},
		{name: "a slice of int8", source: "func f(s []int8, i int, v int8) { s[i] -= v }"},
		{name: "a slice of int16", source: "func f(s []int16, i int, v int16) { s[i] *= v }"},
		{name: "a slice of uint16", source: "func f(s []uint16, i int) { s[i] <<= 3 }"},
		{name: "a slice of int32", source: "func f(s []int32, i int, v int32) { s[i] += v }"},
		{name: "a slice of uint32", source: "func f(s []uint32, i int, v uint32) { s[i] *= v }"},
		{name: "a slice of a named byte type", source: "type cell uint8\nfunc f(s []cell, i int) { s[i] += 200 }"},
		{name: "an array held in a struct", source: "type tape struct{ cells [8]uint8 }\nfunc f(t *tape, i int) { t.cells[i] += 1 }"},
		{name: "a local of a narrow type", source: "func f(x, v uint8) uint8 { x += v; return x }", truncates: true},
		{name: "a narrow local read after the store", source: "func f(s []byte, i int, v byte) byte { v += 200; s[i] = v; return v }", truncates: true},
		{name: "an unsigned 8-bit global", source: "var g uint8\nfunc f(v uint8) { g += v }", truncates: true},
		{name: "a signed 16-bit global", source: "var g int16\nfunc f(v int16) { g -= v }", truncates: true},
		{name: "an unsigned 32-bit global", source: "var g uint32\nfunc f(v uint32) { g *= v }", truncates: true},
		{name: "a signed 32-bit global shifted", source: "var g int32\nfunc f(n uint) { g <<= n }", truncates: true},
		{name: "a global of a named narrow type", source: "type level uint8\nvar g level\nfunc f(v level) { g += v }", truncates: true},
		{name: "a narrow global masked, which cannot leave its type", source: "var g uint16\nfunc f(v uint16) { g &= v }"},
		{name: "a full-width global", source: "var g uint64\nfunc f(v uint64) { g += v }"},
		{name: "an int global", source: "var g int\nfunc f(v int) { g += v }"},
		{
			name:      "a narrow variable a closure captured",
			source:    "func f() func(uint8) uint8 { var total uint8; return func(v uint8) uint8 { total += v; return total } }",
			function:  "<closure>",
			truncates: true,
		},
		{
			name:     "a captured narrow variable shifted right, which cannot leave its type",
			source:   "func f() func(uint) uint8 { var total uint8 = 200; return func(n uint) uint8 { total >>= n; return total } }",
			function: "<closure>",
		},
		{
			name:      "a parameter of a type parameter given a narrow type",
			source:    "func f[T ~uint8](a, b T) T { a += b; return a }\nfunc g() uint8 { return f[uint8](200, 100) }",
			function:  "f[spec:uint8]",
			truncates: true,
		},
		{
			name:     "a parameter of a type parameter given a full-width type",
			source:   "func f[T ~uint8 | ~uint64](a, b T) T { a += b; return a }\nfunc g() uint64 { return f[uint64](200, 100) }",
			function: "f[spec:uint64]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, cmp.Or(tt.function, "f"))

			require.Equal(t, tt.truncates, bodyHoldsANarrowing(compiled),
				"a result is narrowed before it is kept only where what keeps it holds a whole register and the operation can leave the type")
		})
	}
}
