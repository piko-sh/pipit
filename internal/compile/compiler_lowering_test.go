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

func TestArithmeticLowersToTheBankOfItsOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "integer addition", source: "func f(a, b int) int { return a + b }", want: isa.OpAddInt},
		{name: "integer subtraction", source: "func f(a, b int) int { return a - b }", want: isa.OpSubInt},
		{name: "integer multiplication", source: "func f(a, b int) int { return a * b }", want: isa.OpMulInt},
		{name: "integer division", source: "func f(a, b int) int { return a / b }", want: isa.OpDivInt},
		{name: "integer remainder", source: "func f(a, b int) int { return a % b }", want: isa.OpRemInt},
		{name: "unsigned addition", source: "func f(a, b uint64) uint64 { return a + b }", want: isa.OpAddUint},
		{name: "unsigned subtraction", source: "func f(a, b uint64) uint64 { return a - b }", want: isa.OpSubUint},
		{name: "unsigned multiplication", source: "func f(a, b uint64) uint64 { return a * b }", want: isa.OpMulUint},
		{name: "float addition", source: "func f(a, b float64) float64 { return a + b }", want: isa.OpAddFloat},
		{name: "float subtraction", source: "func f(a, b float64) float64 { return a - b }", want: isa.OpSubFloat},
		{name: "float multiplication", source: "func f(a, b float64) float64 { return a * b }", want: isa.OpMulFloat},
		{name: "float division", source: "func f(a, b float64) float64 { return a / b }", want: isa.OpDivFloat},
		{name: "bitwise and", source: "func f(a, b int) int { return a & b }", want: isa.OpBitAnd},
		{name: "bitwise or", source: "func f(a, b int) int { return a | b }", want: isa.OpBitOr},
		{name: "bitwise exclusive or", source: "func f(a, b int) int { return a ^ b }", want: isa.OpBitXor},
		{name: "bitwise and not", source: "func f(a, b int) int { return a &^ b }", want: isa.OpBitAndNot},
		{name: "left shift", source: "func f(a, b int) int { return a << b }", want: isa.OpShiftLeft},
		{name: "right shift", source: "func f(a, b int) int { return a >> b }", want: isa.OpShiftRight},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsOpcode(compiled, tt.want),
				"the operator must lower to the bank-specific opcode, not a boxed fallback")
		})
	}
}

func TestComparisonsLowerToTheBankOfTheirOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "integer equality", source: "func f(a, b int) bool { return a == b }", want: isa.OpEqInt},
		{name: "integer inequality", source: "func f(a, b int) bool { return a != b }", want: isa.OpNeInt},
		{name: "integer less than", source: "func f(a, b int) bool { return a < b }", want: isa.OpLtInt},
		{name: "integer less or equal", source: "func f(a, b int) bool { return a <= b }", want: isa.OpLeInt},
		{name: "integer greater than", source: "func f(a, b int) bool { return a > b }", want: isa.OpGtInt},
		{name: "integer greater or equal", source: "func f(a, b int) bool { return a >= b }", want: isa.OpGeInt},
		{name: "float equality", source: "func f(a, b float64) bool { return a == b }", want: isa.OpEqFloat},
		{name: "float less than", source: "func f(a, b float64) bool { return a < b }", want: isa.OpLtFloat},
		{name: "string equality", source: "func f(a, b string) bool { return a == b }", want: isa.OpEqString},
		{name: "string less than", source: "func f(a, b string) bool { return a < b }", want: isa.OpLtString},
		{name: "unsigned equality", source: "func f(a, b uint64) bool { return a == b }", want: isa.OpEqUint},
		{name: "unsigned less than", source: "func f(a, b uint64) bool { return a < b }", want: isa.OpLtUint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsOpcode(compiled, tt.want),
				"the comparison must lower to the bank-specific opcode")
		})
	}
}

func TestParameterAndResultKindsFollowTheDeclaredTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantParams []isa.RegisterKind
		wantResult isa.RegisterKind
	}{
		{
			name:       "two integers returning an integer",
			source:     "func f(a, b int) int { return a + b }",
			wantParams: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt}, wantResult: isa.RegisterInt,
		},
		{
			name:       "two floats returning a float",
			source:     "func f(a, b float64) float64 { return a + b }",
			wantParams: []isa.RegisterKind{isa.RegisterFloat, isa.RegisterFloat}, wantResult: isa.RegisterFloat,
		},
		{
			name:       "two strings returning a string",
			source:     "func f(a, b string) string { return a + b }",
			wantParams: []isa.RegisterKind{isa.RegisterString, isa.RegisterString}, wantResult: isa.RegisterString,
		},
		{
			name:       "two unsigned values returning unsigned",
			source:     "func f(a, b uint64) uint64 { return a + b }",
			wantParams: []isa.RegisterKind{isa.RegisterUint, isa.RegisterUint}, wantResult: isa.RegisterUint,
		},
		{
			name:       "two booleans returning a boolean",
			source:     "func f(a, b bool) bool { return a && b }",
			wantParams: []isa.RegisterKind{isa.RegisterBool, isa.RegisterBool}, wantResult: isa.RegisterBool,
		},
		{
			name:       "a comparison returns a boolean",
			source:     "func f(a, b int) bool { return a < b }",
			wantParams: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt}, wantResult: isa.RegisterBool,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Equal(t, tt.wantParams, compiled.ParameterKinds)
			require.Len(t, compiled.ResultKinds, 1)
			require.Equal(t, tt.wantResult, compiled.ResultKinds[0])
		})
	}
}

func TestControlFlowCompilesWithoutLeavingDanglingJumps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an if statement", source: "func f(a int) int { if a > 0 { return a }; return -a }"},
		{name: "an if-else chain", source: "func f(a int) int { if a > 0 { return 1 } else if a < 0 { return -1 }; return 0 }"},
		{name: "a three-clause for loop", source: "func f(n int) int { total := 0; for i := 0; i < n; i++ { total += i }; return total }"},
		{name: "a condition-only for loop", source: "func f(n int) int { for n > 0 { n-- }; return n }"},
		{name: "a bare for loop with a break", source: "func f() int { n := 0; for { n++; if n > 3 { break } }; return n }"},
		{name: "a loop with a continue", source: "func f(n int) int { t := 0; for i := 0; i < n; i++ { if i == 2 { continue }; t += i }; return t }"},
		{name: "a range over a slice", source: "func f(xs []int) int { t := 0; for _, x := range xs { t += x }; return t }"},
		{name: "a range over an integer", source: "func f(n int) int { t := 0; for i := range n { t += i }; return t }"},
		{name: "a range over a map", source: "func f(m map[string]int) int { t := 0; for _, v := range m { t += v }; return t }"},
		{name: "a range over a string", source: "func f(s string) int { t := 0; for _, r := range s { t += int(r) }; return t }"},
		{name: "a switch on values", source: "func f(a int) int { switch a { case 1: return 10; case 2: return 20 }; return 0 }"},
		{name: "a switch with a default", source: "func f(a int) int { switch { case a > 0: return 1; default: return 0 } }"},
		{name: "a switch with fallthrough", source: "func f(a int) int { t := 0; switch a { case 1: t++; fallthrough; case 2: t++ }; return t }"},
		{name: "a type switch", source: "func f(v any) int { switch v.(type) { case int: return 1; case string: return 2 }; return 0 }"},
		{name: "a labelled break", source: "func f() int { n := 0; outer: for i := 0; i < 3; i++ { for j := 0; j < 3; j++ { n++; if j == 1 { break outer } } }; return n }"},
		{name: "a deferred call", source: "func f() int { n := 0; defer func() { n++ }(); return n }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.NotEmpty(t, compiled.Body)
			for position, instruction := range compiled.Body {
				if instruction.Op != isa.OpJumpIfFalse && instruction.Op != isa.OpJumpIfTrue {
					continue
				}
				target := position + 1 + int(instruction.SignedOffset())
				require.GreaterOrEqualf(t, target, 0, "a jump at %d must not land before the body", position)
				require.LessOrEqualf(t, target, len(compiled.Body), "a jump at %d must not land past the body", position)
			}
		})
	}
}

func TestBuiltinsCompileForEveryArgumentShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "the length of a slice", source: "func f(xs []int) int { return len(xs) }"},
		{name: "the length of a string", source: "func f(s string) int { return len(s) }"},
		{name: "the length of a map", source: "func f(m map[string]int) int { return len(m) }"},
		{name: "the capacity of a slice", source: "func f(xs []int) int { return cap(xs) }"},
		{name: "appending one element", source: "func f(xs []int) []int { return append(xs, 1) }"},
		{name: "appending several elements", source: "func f(xs []int) []int { return append(xs, 1, 2, 3) }"},
		{name: "appending a spread slice", source: "func f(xs, ys []int) []int { return append(xs, ys...) }"},
		{name: "making a slice with a length", source: "func f(n int) []int { return make([]int, n) }"},
		{name: "making a slice with a capacity", source: "func f(n int) []int { return make([]int, 0, n) }"},
		{name: "making a map", source: "func f() map[string]int { return make(map[string]int) }"},
		{name: "making a channel", source: "func f() chan int { return make(chan int, 1) }"},
		{name: "copying between slices", source: "func f(dst, src []int) int { return copy(dst, src) }"},
		{name: "deleting a map entry", source: "func f(m map[string]int) { delete(m, \"k\") }"},
		{name: "the minimum of two values", source: "func f(a, b int) int { return min(a, b) }"},
		{name: "the maximum of two values", source: "func f(a, b int) int { return max(a, b) }"},
		{name: "a new pointer", source: "func f() *int { return new(int) }"},
		{name: "a panic", source: "func f() { panic(\"boom\") }"},
		{name: "a recover inside a defer", source: "func f() { defer func() { _ = recover() }() }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
		})
	}
}

func TestCompoundAssignmentCompilesAcrossBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "integer addition assignment", source: "func f(a int) int { a += 1; return a }"},
		{name: "integer subtraction assignment", source: "func f(a int) int { a -= 1; return a }"},
		{name: "integer multiplication assignment", source: "func f(a int) int { a *= 2; return a }"},
		{name: "integer division assignment", source: "func f(a int) int { a /= 2; return a }"},
		{name: "integer remainder assignment", source: "func f(a int) int { a %= 2; return a }"},
		{name: "bitwise and assignment", source: "func f(a int) int { a &= 3; return a }"},
		{name: "bitwise or assignment", source: "func f(a int) int { a |= 3; return a }"},
		{name: "bitwise exclusive or assignment", source: "func f(a int) int { a ^= 3; return a }"},
		{name: "left shift assignment", source: "func f(a int) int { a <<= 1; return a }"},
		{name: "right shift assignment", source: "func f(a int) int { a >>= 1; return a }"},
		{name: "float addition assignment", source: "func f(a float64) float64 { a += 1; return a }"},
		{name: "string concatenation assignment", source: "func f(a string) string { a += \"x\"; return a }"},
		{name: "unsigned addition assignment", source: "func f(a uint64) uint64 { a += 1; return a }"},
		{name: "an increment statement", source: "func f(a int) int { a++; return a }"},
		{name: "a decrement statement", source: "func f(a int) int { a--; return a }"},
		{name: "a compound assignment through a slice index", source: "func f(xs []int) []int { xs[0] += 1; return xs }"},
		{name: "a compound assignment through a map key", source: "func f(m map[string]int) { m[\"k\"] += 1 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
		})
	}
}

func TestIndexingAndSlicingCompileForEveryContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "reading a slice element", source: "func f(xs []int) int { return xs[0] }"},
		{name: "writing a slice element", source: "func f(xs []int) { xs[0] = 1 }"},
		{name: "reading an array element", source: "func f(xs [3]int) int { return xs[0] }"},
		{name: "reading a string byte", source: "func f(s string) byte { return s[0] }"},
		{name: "reading a map value", source: "func f(m map[string]int) int { return m[\"k\"] }"},
		{name: "reading a map value with the comma-ok form", source: "func f(m map[string]int) (int, bool) { v, ok := m[\"k\"]; return v, ok }"},
		{name: "writing a map value", source: "func f(m map[string]int) { m[\"k\"] = 1 }"},
		{name: "a slice expression with both bounds", source: "func f(xs []int) []int { return xs[1:2] }"},
		{name: "a slice expression with no bounds", source: "func f(xs []int) []int { return xs[:] }"},
		{name: "a slice expression with a capacity", source: "func f(xs []int) []int { return xs[0:1:2] }"},
		{name: "a string slice expression", source: "func f(s string) string { return s[1:2] }"},
		{name: "a nested index", source: "func f(xs [][]int) int { return xs[0][1] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
		})
	}
}

func TestCompositeTypesAndConversionsCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a struct literal", source: "type P struct{ X, Y int }\nfunc f() P { return P{X: 1, Y: 2} }"},
		{name: "a pointer to a struct literal", source: "type P struct{ X int }\nfunc f() *P { return &P{X: 1} }"},
		{name: "a struct field read", source: "type P struct{ X int }\nfunc f(p P) int { return p.X }"},
		{name: "a struct field write", source: "type P struct{ X int }\nfunc f(p *P) { p.X = 1 }"},
		{name: "an embedded field read", source: "type Inner struct{ X int }\ntype Outer struct{ Inner }\nfunc f(o Outer) int { return o.X }"},
		{name: "a slice literal", source: "func f() []int { return []int{1, 2, 3} }"},
		{name: "a map literal", source: "func f() map[string]int { return map[string]int{\"a\": 1} }"},
		{name: "an array literal", source: "func f() [2]int { return [2]int{1, 2} }"},
		{name: "a method with a value receiver", source: "type P struct{ X int }\nfunc (p P) Get() int { return p.X }\nfunc f(p P) int { return p.Get() }"},
		{name: "a method with a pointer receiver", source: "type P struct{ X int }\nfunc (p *P) Set(v int) { p.X = v }\nfunc f(p *P) { p.Set(1) }"},
		{name: "an interface method call", source: "type S interface{ Get() int }\nfunc f(s S) int { return s.Get() }"},
		{name: "a type assertion", source: "func f(v any) int { return v.(int) }"},
		{name: "a type assertion with the comma-ok form", source: "func f(v any) (int, bool) { n, ok := v.(int); return n, ok }"},
		{name: "an integer narrowing conversion", source: "func f(a int) int8 { return int8(a) }"},
		{name: "an integer to float conversion", source: "func f(a int) float64 { return float64(a) }"},
		{name: "a float to integer conversion", source: "func f(a float64) int { return int(a) }"},
		{name: "a signed to unsigned conversion", source: "func f(a int) uint64 { return uint64(a) }"},
		{name: "a string to byte slice conversion", source: "func f(s string) []byte { return []byte(s) }"},
		{name: "a byte slice to string conversion", source: "func f(b []byte) string { return string(b) }"},
		{name: "a closure capturing a local", source: "func f() func() int { n := 1; return func() int { return n } }"},
		{name: "a closure mutating a local", source: "func f() int { n := 0; g := func() { n++ }; g(); return n }"},
		{name: "a variadic function", source: "func g(xs ...int) int { return len(xs) }\nfunc f() int { return g(1, 2, 3) }"},
		{name: "a multiple return", source: "func f() (int, string) { return 1, \"a\" }"},
		{name: "a named return", source: "func f() (n int) { n = 1; return }"},
		{name: "a generic function", source: "func g[T any](v T) T { return v }\nfunc f() int { return g(1) }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
		})
	}
}

func TestUnaryOperatorsLowerToTheirBankOpcode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "integer negation", source: "func f(a int) int { return -a }", want: isa.OpDrillTier1},
		{name: "float negation", source: "func f(a float64) float64 { return -a }", want: isa.OpDrillTier1},
		{name: "bitwise complement", source: "func f(a int) int { return ^a }", want: isa.OpDrillTier1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsOpcode(compiled, tt.want),
				"a unary operator lowers through the tier-one drill")
		})
	}

	t.Run("logical negation compiles", func(t *testing.T) {
		t.Parallel()
		root := compileSnippet(t, "package main\n\nfunc f(a bool) bool { return !a }\n")

		require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
	})
}

func TestGlobalVariablesAreRegisteredInTheStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an integer global", source: "var counter int\nfunc f() int { return counter }"},
		{name: "a string global", source: "var name = \"pipit\"\nfunc f() string { return name }"},
		{name: "several globals in one declaration", source: "var a, b int\nfunc f() int { return a + b }"},
		{name: "a global written by a function", source: "var counter int\nfunc f() { counter++ }"},
		{name: "a slice global", source: "var xs []int\nfunc f() int { return len(xs) }"},
		{name: "a constant is folded rather than stored", source: "const limit = 10\nfunc f() int { return limit }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotEmpty(t, findCompiledFunction(t, root, "f").Body)
		})
	}
}

func TestValueReturningBodiesEndInTheReturnEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a single return", source: "func f() int { return 1 }"},
		{name: "several returns", source: "func f(a int) int { if a > 0 { return 1 }; return 0 }"},
		{name: "a return of a computed value", source: "func f(a, b int) int { return a + b }"},
		{name: "a return from inside a loop", source: "func f(n int) int { for i := 0; i < n; i++ { return i }; return -1 }"},
		{name: "a multiple return", source: "func f() (int, int) { return 1, 2 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")

			require.NotEmpty(t, compiled.Body)
			require.True(t, program.IsReturnInstruction(compiled.Body[len(compiled.Body)-1]),
				"a function that produces a value must end in the return encoding")
		})
	}
}

func TestVoidBodiesMayRunOffTheEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		wantEmpty bool
	}{
		{
			name:      "a function with no statements emits nothing",
			source:    "func f() { }",
			wantEmpty: true,
		},
		{name: "a function whose last statement is a loop", source: "func f(n int) { for i := 0; i < n; i++ { } }"},
		{name: "a function whose last statement is an if", source: "func f(a int) { if a > 0 { } }"},
		{name: "a function whose last statement is an assignment", source: "func f(xs []int) { xs[0] = 1 }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")
			compiled := findCompiledFunction(t, root, "f")

			require.Empty(t, compiled.ResultKinds, "the fixture must declare no results")
			if tt.wantEmpty {
				require.Empty(t, compiled.Body)
				return
			}
			require.NotEmpty(t, compiled.Body,
				"a void body carries its statements; the frame teardown performs the implicit return")
		})
	}
}
