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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestStatementFormsCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a short variable declaration", source: "func f() int { a := 1; return a }"},
		{name: "a var declaration with a type", source: "func f() int { var a int; return a }"},
		{name: "a var declaration with a value", source: "func f() int { var a int = 1; return a }"},
		{name: "a var block", source: "func f() int { var ( a int; b int ); return a + b }"},
		{name: "a multiple assignment", source: "func f() int { a, b := 1, 2; return a + b }"},
		{name: "a swap through multiple assignment", source: "func f() int { a, b := 1, 2; a, b = b, a; return a - b }"},
		{name: "a blank identifier on the left", source: "func f() int { _, b := 1, 2; return b }"},
		{name: "a destructured multiple return", source: "func g() (int, int) { return 1, 2 }\nfunc f() int { a, b := g(); return a + b }"},
		{name: "a shadowed variable in a block", source: "func f() int { a := 1; { a := 2; _ = a }; return a }"},
		{name: "a nested block", source: "func f() int { a := 1; { { a++ } }; return a }"},
		{name: "an expression statement", source: "func g() int { return 1 }\nfunc f() { g() }"},
		{name: "a local constant", source: "func f() int { const n = 3; return n }"},
		{name: "a local type declaration", source: "func f() int { type pair struct{ X int }; p := pair{X: 1}; return p.X }"},
		{name: "a goto and label", source: "func f() int { n := 0; goto done; n = 1; done: return n }"},
		{name: "a labelled continue", source: "func f() int { n := 0; outer: for i := 0; i < 2; i++ { for j := 0; j < 2; j++ { continue outer }; n++ }; return n }"},
		{name: "a select over a channel", source: "func f(c chan int) int { select { case v := <-c: return v; default: return 0 } }"},
		{name: "a channel send", source: "func f(c chan int) { c <- 1 }"},
		{name: "a channel receive", source: "func f(c chan int) int { return <-c }"},
		{name: "a channel close", source: "func f(c chan int) { close(c) }"},
		{name: "a goroutine launch", source: "func f() { go func() { }() }"},
		{name: "a deferred method call", source: "type P struct{}\nfunc (P) M() {}\nfunc f(p P) { defer p.M() }"},
		{name: "several defers", source: "func f() { defer func() {}(); defer func() {}() }"},
		{name: "an anonymous struct", source: "func f() int { v := struct{ X int }{X: 1}; return v.X }"},
		{name: "a function value", source: "func f() int { g := func() int { return 1 }; return g() }"},
		{name: "a method value", source: "type P struct{ X int }\nfunc (p P) Get() int { return p.X }\nfunc f(p P) int { g := p.Get; return g() }"},
		{name: "a method expression", source: "type P struct{ X int }\nfunc (p P) Get() int { return p.X }\nfunc f(p P) int { g := P.Get; return g(p) }"},
		{name: "a nested closure", source: "func f() int { n := 1; g := func() func() int { return func() int { return n } }; return g()() }"},
		{name: "an immediately invoked function", source: "func f() int { return func() int { return 1 }() }"},
		{name: "a pointer dereference", source: "func f(p *int) int { return *p }"},
		{name: "an address-of expression", source: "func f() *int { n := 1; return &n }"},
		{name: "a write through a pointer", source: "func f(p *int) { *p = 1 }"},
		{name: "a logical and short circuit", source: "func f(a, b bool) bool { return a && b }"},
		{name: "a logical or short circuit", source: "func f(a, b bool) bool { return a || b }"},
		{name: "a chained comparison through logical operators", source: "func f(a int) bool { return a > 0 && a < 10 }"},
		{name: "a conditional expression through if", source: "func f(a int) int { if a > 0 { return 1 }; return 2 }"},
		{name: "an empty return in a void function", source: "func f(a int) { if a > 0 { return }; _ = a }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotNil(t, findCompiledFunction(t, root, "f"))
		})
	}
}

func TestInterfaceAndGenericFormsCompile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "an interface with one method", source: "type S interface{ M() int }\nfunc f(s S) int { return s.M() }"},
		{name: "an embedded interface", source: "type A interface{ M() int }\ntype B interface{ A }\nfunc f(b B) int { return b.M() }"},
		{name: "an empty interface parameter", source: "func f(v any) any { return v }"},
		{name: "a type switch with a binding", source: "func f(v any) int { switch n := v.(type) { case int: return n; case string: return len(n) }; return 0 }"},
		{name: "a generic identity function", source: "func g[T any](v T) T { return v }\nfunc f() string { return g(\"a\") }"},
		{name: "a generic function with a constraint", source: "type Num interface{ ~int | ~float64 }\nfunc g[T Num](a, b T) T { return a + b }\nfunc f() int { return g(1, 2) }"},
		{name: "a generic function instantiated twice", source: "func g[T any](v T) T { return v }\nfunc f() int { _ = g(\"a\"); return g(1) }"},
		{name: "a generic type", source: "type Box[T any] struct{ V T }\nfunc f() int { b := Box[int]{V: 1}; return b.V }"},
		{name: "a method on a generic type", source: "type Box[T any] struct{ V T }\nfunc (b Box[T]) Get() T { return b.V }\nfunc f() int { return Box[int]{V: 1}.Get() }"},
		{name: "a struct implementing an interface", source: "type S interface{ M() int }\ntype P struct{}\nfunc (P) M() int { return 1 }\nfunc f() int { var s S = P{}; return s.M() }"},
		{name: "a pointer receiver satisfying an interface", source: "type S interface{ M() int }\ntype P struct{}\nfunc (*P) M() int { return 1 }\nfunc f() int { var s S = &P{}; return s.M() }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n\n"+tt.source+"\n")

			require.NotNil(t, findCompiledFunction(t, root, "f"))
		})
	}
}

func TestCompilerRefusesInputPastItsConfiguredLimits(t *testing.T) {
	t.Parallel()

	t.Run("an expression nested past the depth ceiling is refused", func(t *testing.T) {
		t.Parallel()
		const depth = 64
		source := "package main\n\nfunc f(a int) int { return " +
			strings.Repeat("a + (", depth) + "a" + strings.Repeat(")", depth) + " }\n"

		_, err := tryCompileSnippetWithLimits(t, source, 0, 8)

		require.ErrorIs(t, err, fault.ErrCompileDepthLimit,
			"an over-deep expression must be refused rather than exhausting the host stack")
	})

	t.Run("a literal with more elements than the ceiling is refused", func(t *testing.T) {
		t.Parallel()
		elements := strings.TrimSuffix(strings.Repeat("1, ", 50), ", ")
		source := "package main\n\nfunc f() []int { return []int{" + elements + "} }\n"

		_, err := tryCompileSnippetWithLimits(t, source, 4, 0)

		require.Error(t, err, "a literal past the element ceiling must be refused")
	})

	t.Run("input within the limits still compiles", func(t *testing.T) {
		t.Parallel()
		source := "package main\n\nfunc f() []int { return []int{1, 2} }\n"

		root, err := tryCompileSnippetWithLimits(t, source, 8, 64)

		require.NoError(t, err)
		require.NotNil(t, findCompiledFunction(t, root, "f"))
	})
}
