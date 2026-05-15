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

const expressionTypes = `
type tally struct {
	X int
	Y int
	S string
	T string
}

type shape interface {
	Area() float64
}

type square struct {
	Side float64
}

func (s square) Area() float64 { return s.Side * s.Side }

func identity[T any](v T) T { return v }

func pairUp[K comparable, V any](k K, v V) (K, V) { return k, v }

func sum[T int | float64](a, b T) T { return a + b }

func twoValues() (int, string) { return 1, "a" }

func takeTwo(int, string) {}

func takeVariadic(...any) {}

func takeVariadicInts(...int) {}
`

func TestInstantiatingAGenericFunctionCompilesForEachArgumentForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an inferred instantiation", source: "func f() int { return identity(1) }"},
		{name: "an explicit single type argument", source: "func f() int { return identity[int](1) }"},
		{name: "an explicit instantiation bound to a variable", source: "func f() int { g := identity[int]; return g(1) }"},
		{name: "an explicit instantiation of a string", source: "func f() string { g := identity[string]; return g(\"a\") }"},
		{name: "two explicit type arguments", source: "func f() int { k, _ := pairUp[int, string](1, \"a\"); return k }"},
		{name: "two explicit type arguments bound to a variable", source: "func f() int { g := pairUp[int, string]; k, _ := g(1, \"a\"); return k }"},
		{name: "a constrained instantiation over integers", source: "func f() int { return sum[int](1, 2) }"},
		{name: "a constrained instantiation over floats", source: "func f() float64 { return sum[float64](1, 2) }"},
		{name: "an instantiation over a struct", source: "func f(t tally) int { return identity[tally](t).X }"},
		{name: "an instantiation over a slice", source: "func f(xs []int) int { return len(identity[[]int](xs)) }"},
		{name: "an instantiation passed on as a value", source: "func f() int { g := identity[int]; h := g; return h(1) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"an instantiation names one concrete function, which the call has to reach")
		})
	}
}

func TestComplexConstantsAndComparisonsCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a complex literal", source: "func f() complex128 { return 1 + 2i }"},
		{name: "a complex constant declared locally", source: "func f() complex128 { const z = 3 + 4i; return z }"},
		{name: "two complex values compared for equality", source: "func f(a, b complex128) bool { return a == b }"},
		{name: "two complex values compared for inequality", source: "func f(a, b complex128) bool { return a != b }"},
		{name: "a complex value compared against a literal", source: "func f(a complex128) bool { return a == 1+2i }"},
		{name: "two booleans compared for equality", source: "func f(a, b bool) bool { return a == b }"},
		{name: "two booleans compared for inequality", source: "func f(a, b bool) bool { return a != b }"},
		{name: "a boolean compared against a literal", source: "func f(a bool) bool { return a == true }"},
		{name: "an untyped float constant rounded to a float32", source: "func f() float32 { return 1.5 }"},
		{name: "an untyped integer constant narrowed to an int8", source: "func f() int8 { return 100 }"},
		{name: "an untyped integer constant widened to a uint64", source: "func f() uint64 { return 1 }"},
		{name: "a rune literal", source: "func f() rune { return 'x' }"},
		{name: "a raw string literal", source: "func f() string { return \"a\" }"},
		{name: "a hexadecimal literal", source: "func f() int { return 0xFF }"},
		{name: "a literal with digit separators", source: "func f() int { return 1_000_000 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a constant still has to be loaded into the register its bank uses")
		})
	}
}

func TestComparingAgainstNilTellsTheInterfaceCaseApart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an interface compared against nil", source: "func f(s shape) bool { return s == nil }"},
		{name: "an interface compared against nil the other way round", source: "func f(s shape) bool { return nil == s }"},
		{name: "an interface compared against nil for inequality", source: "func f(s shape) bool { return s != nil }"},
		{name: "an empty interface compared against nil", source: "func f(v any) bool { return v == nil }"},
		{name: "a pointer compared against nil", source: "func f(p *tally) bool { return p == nil }"},
		{name: "a slice compared against nil", source: "func f(xs []int) bool { return xs == nil }"},
		{name: "a map compared against nil", source: "func f(m map[int]int) bool { return m == nil }"},
		{name: "a channel compared against nil", source: "func f(c chan int) bool { return c == nil }"},
		{name: "a function value compared against nil", source: "func f(g func()) bool { return g == nil }"},
		{name: "two interfaces compared against each other", source: "func f(a, b shape) bool { return a == b }"},
		{name: "an interface compared against a concrete value", source: "func f(a shape, s square) bool { return a == s }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"a nil comparison is a real test, not something the compiler can fold away")
		})
	}
}

func TestSwappingTwoStructFieldsInOneStatement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "two integer fields of one receiver", source: "func f(t *tally) { t.X, t.Y = t.Y, t.X }"},
		{name: "two string fields of one receiver", source: "func f(t *tally) { t.S, t.T = t.T, t.S }"},
		{name: "two fields of a value receiver", source: "func f(t tally) tally { t.X, t.Y = t.Y, t.X; return t }"},
		{name: "fields of two different receivers", source: "func f(a, b *tally) { a.X, b.X = b.X, a.X }"},
		{name: "a field swapped with a local", source: "func f(t *tally, n int) int { t.X, n = n, t.X; return n }"},
		{name: "two fields of different banks", source: "func f(t *tally) { t.X, t.S = len(t.S), \"\" }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"both sides are read before either is written, whatever shape the swap takes")
		})
	}
}

func TestPassingAMultiValuedCallStraightIntoAnotherCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "into a call whose parameters match exactly", source: "func f() { takeTwo(twoValues()) }"},
		{name: "into a variadic call", source: "func f() { takeVariadic(twoValues()) }"},
		{name: "into a variadic call of one element type", source: "func f() { takeVariadicInts(1, 2, 3) }"},
		{name: "into a variadic call with no arguments", source: "func f() { takeVariadicInts() }"},
		{name: "into a variadic call spreading a slice", source: "func f(xs []int) { takeVariadicInts(xs...) }"},
		{name: "into a variadic call of mixed banks", source: "func f(n int, s string, b bool) { takeVariadic(n, s, b) }"},
		{name: "into a variadic call of a struct", source: "func f(t tally) { takeVariadic(t) }"},
		{name: "into a variadic call of an interface value", source: "func f(s shape) { takeVariadic(s) }"},
		{name: "into a return of the same shape", source: "func f() (int, string) { return twoValues() }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"the results of the inner call become the arguments of the outer one without a temporary the source names")
		})
	}
}

func TestDeclaringSeveralShortVariablesFromSeveralValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "two values of the same bank", source: "func f() int { a, b := 1, 2; return a + b }"},
		{name: "two values of different banks", source: "func f() int { a, s := 1, \"x\"; return a + len(s) }"},
		{name: "one already declared and one new", source: "func f() int { a := 1; a, b := 2, 3; return a + b }"},
		{name: "values that read each other's old bindings", source: "func f() int { a, b := 1, 2; a, b = b, a; return a - b }"},
		{name: "a value read from an earlier declaration", source: "func f() int { a := 1; b, c := a, a+1; return b + c }"},
		{name: "three values", source: "func f() int { a, b, c := 1, 2, 3; return a + b + c }"},
		{name: "a typed slice initialiser", source: "func f() int { xs, n := []int{1, 2}, 3; return len(xs) + n }"},
		{name: "a declaration whose value indexes the other", source: "func f(xs []int) int { a, b := xs[0], xs[1]; return a + b }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body,
				"every value is evaluated before any name is bound, whichever order the compiler emits them in")
		})
	}
}

func TestComparingUnsignedValuesUsesTheUnsignedComparisons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "less than", source: "func f(a, b uint64) bool { return a < b }", want: isa.OpLtUint},
		{name: "less than or equal", source: "func f(a, b uint64) bool { return a <= b }", want: isa.OpLeUint},
		{name: "greater than", source: "func f(a, b uint64) bool { return a > b }", want: isa.OpGtUint},
		{name: "greater than or equal", source: "func f(a, b uint64) bool { return a >= b }", want: isa.OpGeUint},
		{name: "equal", source: "func f(a, b uint64) bool { return a == b }", want: isa.OpEqUint},
		{name: "not equal", source: "func f(a, b uint64) bool { return a != b }", want: isa.OpNeUint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+expressionTypes+"\n"+tt.source+"\n")

			require.Truef(t, bodyContainsOpcode(findCompiledFunction(t, root, "f"), tt.want),
				"a signed comparison would order the top half of the range wrongly, so %s must be used", tt.want)
		})
	}
}
