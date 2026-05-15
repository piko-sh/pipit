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

const builtinTypes = `
type box struct {
	Items  []int
	Names  []string
	Bytes  []byte
	Counts map[string]int
}
`

func builtinCallID(compiledFunction *program.CompiledFunction) (uint8, bool) {
	for _, instruction := range compiledFunction.Body {
		if isa.InstrIsTier1SubOp(instruction, isa.SubOpCallBuiltin) {
			return instruction.B, true
		}
	}
	return 0, false
}

func TestTheBuiltinDispatchFamilyCarriesItsOwnIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		want     uint8
		wantArgs uint8
	}{
		{name: "a print of one value", source: "func f(n int) { print(n) }", want: isa.BuiltinPrint, wantArgs: 1},
		{name: "a print of three values", source: "func f(n int, s string, b bool) { print(n, s, b) }", want: isa.BuiltinPrint, wantArgs: 3},
		{name: "a print of nothing", source: "func f() { print() }", want: isa.BuiltinPrint, wantArgs: 0},
		{name: "a println of one value", source: "func f(n int) { println(n) }", want: isa.BuiltinPrintln, wantArgs: 1},
		{name: "a map cleared", source: "func f(m map[string]int) { clear(m) }", want: isa.BuiltinClear, wantArgs: 1},
		{name: "a slice cleared", source: "func f(xs []int) { clear(xs) }", want: isa.BuiltinClear, wantArgs: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")

			got, found := builtinCallID(findCompiledFunction(t, root, "f"))

			require.True(t, found, "a dispatched builtin is one word carrying its identifier")
			require.Equal(t, tt.want, got)
		})
	}
}

func TestTheBuiltinDispatchFamilyRecordsItsArgumentCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantArgs uint8
	}{
		{name: "no arguments", source: "func f() { print() }", wantArgs: 0},
		{name: "one argument", source: "func f(n int) { print(n) }", wantArgs: 1},
		{name: "two arguments", source: "func f(n int, s string) { print(n, s) }", wantArgs: 2},
		{name: "four arguments", source: "func f(n int, s string, b bool, x float64) { print(n, s, b, x) }", wantArgs: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")

			var found bool
			for _, instruction := range compiled.Body {
				if isa.InstrIsTier1SubOp(instruction, isa.SubOpCallBuiltin) {
					require.Equal(t, tt.wantArgs, instruction.C,
						"each argument follows the dispatch word as its own extension, so the count has to match")
					found = true
					break
				}
			}
			require.True(t, found)
		})
	}
}

func TestComplexBuiltinsLowerToTheComplexBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		wantOp   isa.Opcode
		wantSub  isa.SubOpcode
		useSubOp bool
	}{
		{name: "a complex value built from two floats", source: "func f(re, im float64) complex128 { return complex(re, im) }", wantOp: isa.OpBuildComplex},
		{name: "the real part extracted", source: "func f(z complex128) float64 { return real(z) }", wantSub: isa.SubOpRealComplex, useSubOp: true},
		{name: "the imaginary part extracted", source: "func f(z complex128) float64 { return imag(z) }", wantSub: isa.SubOpImagComplex, useSubOp: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")

			if tt.useSubOp {
				require.Truef(t, bodyContainsTier1SubOp(compiled, tt.wantSub),
					"a complex part is read with one word, so %v must be emitted", tt.wantSub)
				return
			}
			require.Truef(t, bodyContainsOpcode(compiled, tt.wantOp),
				"the two halves are joined in one instruction, so %s must be emitted", tt.wantOp)
		})
	}
}

func TestMakingATypedSliceUsesTheSubOpOfItsBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.SubOpcode
	}{
		{name: "a slice of int", source: "func f(n int) int { xs := make([]int, n); return len(xs) }", want: isa.SubOpMakeSliceInt},
		{name: "a slice of float64", source: "func f(n int) int { xs := make([]float64, n); return len(xs) }", want: isa.SubOpMakeSliceFloat},
		{name: "a slice of string", source: "func f(n int) int { xs := make([]string, n); return len(xs) }", want: isa.SubOpMakeSliceString},
		{name: "a slice of bool", source: "func f(n int) int { xs := make([]bool, n); return len(xs) }", want: isa.SubOpMakeSliceBool},
		{name: "a slice of uint64", source: "func f(n int) int { xs := make([]uint64, n); return len(xs) }", want: isa.SubOpMakeSliceUint},
		{name: "a slice of byte", source: "func f(n int) int { bs := make([]byte, n); return len(bs) }", want: isa.SubOpMakeSliceByte},
		{name: "a slice with a separate capacity", source: "func f(n, c int) int { xs := make([]int, n, c); return len(xs) }", want: isa.SubOpMakeSliceInt},
		{name: "a slice of length zero", source: "func f() int { xs := make([]int, 0); return len(xs) }", want: isa.SubOpMakeSliceInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")

			require.Truef(t, bodyContainsTier1SubOp(findCompiledFunction(t, root, "f"), tt.want),
				"a typed slice bank has its own allocation word, so %v must be emitted", tt.want)
		})
	}
}

func TestTheRemainingBuiltinsCompileForEveryShapeTheyAccept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "the length of a slice", source: "func f(xs []int) int { return len(xs) }"},
		{name: "the length of a string", source: "func f(s string) int { return len(s) }"},
		{name: "the length of a map", source: "func f(m map[string]int) int { return len(m) }"},
		{name: "the length of an array", source: "func f(a [4]int) int { return len(a) }"},
		{name: "the length of a channel", source: "func f(c chan int) int { return len(c) }"},
		{name: "the length of a slice-typed struct field", source: "func f(b *box) int { return len(b.Items) }"},
		{name: "the length of a string-slice struct field", source: "func f(b *box) int { return len(b.Names) }"},
		{name: "the length of a byte-slice struct field", source: "func f(b *box) int { return len(b.Bytes) }"},
		{name: "the capacity of a slice", source: "func f(xs []int) int { return cap(xs) }"},
		{name: "the capacity of a channel", source: "func f(c chan int) int { return cap(c) }"},
		{name: "the capacity of an array", source: "func f(a [4]int) int { return cap(a) }"},
		{name: "a map made with a size hint", source: "func f(n int) map[string]int { return make(map[string]int, n) }"},
		{name: "a map made with a literal size hint", source: "func f() map[string]int { return make(map[string]int, 8) }"},
		{name: "a map made with no hint", source: "func f() map[string]int { return make(map[string]int) }"},
		{name: "a channel made unbuffered", source: "func f() chan int { return make(chan int) }"},
		{name: "a channel made buffered", source: "func f(n int) chan int { return make(chan int, n) }"},
		{name: "a key deleted from a map", source: "func f(m map[string]int, k string) { delete(m, k) }"},
		{name: "a slice copied", source: "func f(d, s []int) int { return copy(d, s) }"},
		{name: "a byte slice copied from a string", source: "func f(d []byte, s string) int { return copy(d, s) }"},
		{name: "a value allocated with new", source: "func f() *int { return new(int) }"},
		{name: "a struct allocated with new", source: "func f() *box { return new(box) }"},
		{name: "the smaller of two integers", source: "func f(a, b int) int { return min(a, b) }"},
		{name: "the larger of two integers", source: "func f(a, b int) int { return max(a, b) }"},
		{name: "the smaller of three integers", source: "func f(a, b, c int) int { return min(a, b, c) }"},
		{name: "the smaller of two floats", source: "func f(a, b float64) float64 { return min(a, b) }"},
		{name: "the larger of two strings", source: "func f(a, b string) string { return max(a, b) }"},
		{name: "an append of one element", source: "func f(xs []int, v int) []int { return append(xs, v) }"},
		{name: "an append of several elements", source: "func f(xs []int) []int { return append(xs, 1, 2, 3) }"},
		{name: "an append of another slice", source: "func f(xs, ys []int) []int { return append(xs, ys...) }"},
		{name: "an append of a string to a byte slice", source: "func f(bs []byte, s string) []byte { return append(bs, s...) }"},
		{name: "an append of one byte", source: "func f(bs []byte, b byte) []byte { return append(bs, b) }"},
		{name: "an append of a float", source: "func f(xs []float64, v float64) []float64 { return append(xs, v) }"},
		{name: "an append of a string", source: "func f(xs []string, v string) []string { return append(xs, v) }"},
		{name: "an append of a bool", source: "func f(xs []bool, v bool) []bool { return append(xs, v) }"},
		{name: "an append of an unsigned value", source: "func f(xs []uint64, v uint64) []uint64 { return append(xs, v) }"},
		{name: "an append of a struct", source: "func f(xs []box, v box) []box { return append(xs, v) }"},
		{name: "an append to a slice-typed struct field", source: "func f(b *box, v int) { b.Items = append(b.Items, v) }"},
		{name: "an append to an empty slice literal", source: "func f(v int) []int { return append([]int{}, v) }"},
		{name: "a value recovered outside a defer", source: "func f() any { return recover() }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"every accepted builtin shape has to lower to something the machine can run")
		})
	}
}

func TestThePanicFamilyAndChannelCloseLowerToTheirOwnTierTwoWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		function string
		want     isa.SubOpcodeTier2
	}{
		{name: "a panic raised", source: "func f(s string) { panic(s) }", function: "f", want: isa.SubOpTier2Panic},
		{name: "a panic of a value", source: "func f(n int) { panic(n) }", function: "f", want: isa.SubOpTier2Panic},
		{name: "a recover in a deferred closure", source: "func f() { defer func() { _ = recover() }() }", function: "<closure>", want: isa.SubOpTier2Recover},
		{name: "a recover outside a defer", source: "func f() any { return recover() }", function: "f", want: isa.SubOpTier2Recover},
		{name: "a channel closed", source: "func f(c chan int) { close(c) }", function: "f", want: isa.SubOpTier2ChannelClose},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+builtinTypes+"\n"+tt.source+"\n")

			require.Truef(t, bodyContainsTier2SubOp(findCompiledFunction(t, root, tt.function), tt.want),
				"these builtins are not dispatched through the builtin table, so %v must be emitted directly", tt.want)
		})
	}
}
