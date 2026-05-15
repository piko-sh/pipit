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

package astpattern

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/compile/patterns"
)

const loopPreamble = `package main

func f(a, b, d []float64, ints []int, k float64, n int, ok bool) {
	s := 0.0
	m := 0.0
	_, _, _, _, _, _, _, _ = a, b, d, ints, k, n, ok, m
	_ = s
`

func checkedLoop(t *testing.T, body string) (*ast.ForStmt, *types.Info) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", loopPreamble+body+"\n}\n", 0)
	require.NoError(t, err, "the fixture loop must parse")

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	config := &types.Config{Importer: importer.Default()}
	_, err = config.Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err, "the fixture loop must type-check")

	declaration, ok := file.Decls[0].(*ast.FuncDecl)
	require.True(t, ok, "the fixture declares a function")
	for _, statement := range declaration.Body.List {
		if loop, isLoop := statement.(*ast.ForStmt); isLoop {
			return loop, info
		}
	}
	t.Fatal("the fixture must contain a for statement")
	return nil, nil
}

func bothOptions() passes.Options {
	return passes.Options{SIMDKernels: true, LoopUnroll: true}
}

func TestTheDefaultRegistryRecognisesTheCanonicalNumericalKernels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "a dot product over a length", body: "for i := 0; i < len(a); i++ { s += a[i] * b[i] }"},
		{name: "a dot product over a constant", body: "for i := 0; i < 4; i++ { s += a[i] * b[i] }"},
		{name: "a sum reduction over a length", body: "for i := 0; i < len(a); i++ { s += a[i] }"},
		{name: "an element-wise add over a length", body: "for i := 0; i < len(a); i++ { d[i] = a[i] + b[i] }"},
		{name: "an in-place scale over a length", body: "for i := 0; i < len(a); i++ { a[i] *= k }"},
		{name: "a bound taken from the second operand", body: "for i := 0; i < len(b); i++ { s += a[i] * b[i] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)

			matched, matchToken, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: bothOptions()}, loop)

			require.True(t, ok, "the kernel recogniser has to claim the shape it was written for")
			require.Equal(t, "simd.kernels", matched.Name(),
				"a kernel shape is claimed by the narrow recogniser, not by the unroller behind it")
			require.NotNil(t, matchToken, "the match token is what the emitter reads the operands from")
		})
	}
}

func TestTheKernelRecogniserRefusesShapesItCannotVectorise(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "operands of the wrong element type", body: "for i := 0; i < len(ints); i++ { n += ints[i] * ints[i] }"},
		{name: "a destination that is also a source", body: "for i := 0; i < len(a); i++ { a[i] = a[i] + b[i] }"},
		{name: "a bound taken from a slice the body never reads", body: "for i := 0; i < len(d); i++ { s += a[i] * b[i] }"},
		{name: "an index that is not the loop variable", body: "for i := 0; i < len(a); i++ { s += a[n] * b[n] }"},
		{name: "a sum written with a plain assignment", body: "for i := 0; i < len(a); i++ { s = a[i] }"},
		{name: "a scale by something that is not a scalar", body: "for i := 0; i < len(a); i++ { a[i] *= b[i] }"},
		{name: "a counter that starts at one", body: "for i := 1; i < len(a); i++ { s += a[i] * b[i] }"},
		{name: "a counter that counts down", body: "for i := 0; i < len(a); i-- { s += a[i] * b[i] }"},
		{name: "a condition on a different name", body: "for i := 0; n < len(a); i++ { s += a[i] * b[i] }"},
		{name: "a post clause on a different name", body: "for i := 0; i < len(a); n++ { s += a[i] * b[i] }"},
		{name: "a body of two statements", body: "for i := 0; i < len(a); i++ { s += a[i] * b[i]; n++ }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)

			matched, _, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: passes.Options{SIMDKernels: true}}, loop)

			require.False(t, ok, "a shape the kernel cannot express must fall back to the scalar loop")
			require.Nil(t, matched)
		})
	}
}

func TestTheUnrollerClaimsASmallConstantBoundLoop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "a counter declared in the loop", body: "for i := 0; i < 4; i++ { n += i }"},
		{name: "a counter assigned in the loop", body: "i := 0\nfor i = 0; i < 4; i++ { n += i }"},
		{name: "a single trip", body: "for i := 0; i < 1; i++ { n += i }"},
		{name: "the largest trip count the budget allows", body: "for i := 0; i < 8; i++ { n += i }"},
		{name: "a body of eight statements", body: "for i := 0; i < 8; i++ { n++; n++; n++; n++; n++; n++; n++; n++ }"},
		{name: "a kernel shape whose element type the kernel refuses", body: "for i := 0; i < 4; i++ { n += ints[i] * ints[i] }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)

			matched, matchToken, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: bothOptions()}, loop)

			require.True(t, ok, "a small constant-bound loop is worth flattening")
			require.Equal(t, "loop.unroll", matched.Name())
			require.NotNil(t, matchToken)
		})
	}
}

func TestTheUnrollerRefusesWhatItCannotFlattenSafely(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "a trip count past the budget", body: "for i := 0; i < 16; i++ { n += i }"},
		{name: "a product of trips and statements past the budget", body: "for i := 0; i < 8; i++ { n++; n++; n++; n++; n++; n++; n++; n++; n++ }"},
		{name: "a bound of zero", body: "for i := 0; i < 0; i++ { n += i }"},
		{name: "a bound that is not a constant", body: "for i := 0; i < n; i++ { n += i }"},
		{name: "an empty body", body: "for i := 0; i < 4; i++ {}"},
		{name: "a body that breaks", body: "for i := 0; i < 4; i++ { if ok { break } }"},
		{name: "a body that continues", body: "for i := 0; i < 4; i++ { if ok { continue } }"},
		{name: "a body that takes the address of the counter", body: "for i := 0; i < 4; i++ { p := &i; _ = p }"},
		{name: "a body that writes the counter", body: "for i := 0; i < 4; i++ { i = 2 }"},
		{name: "a body that steps the counter itself", body: "for i := 0; i < 4; i++ { i++ }"},
		{name: "a body holding a closure", body: "for i := 0; i < 4; i++ { g := func() int { return i }; _ = g }"},
		{name: "a counter advanced by more than one", body: "for i := 0; i < 4; i += 2 { n += i }"},
		{name: "a bound that is inclusive", body: "for i := 0; i <= 4; i++ { n += i }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)

			matched, _, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: passes.Options{LoopUnroll: true}}, loop)

			require.False(t, ok, "an unroll that changes control flow or bloats the body is worse than the loop it replaces")
			require.Nil(t, matched)
		})
	}
}

func TestARecogniserWhoseOptimisationIsOffDeclinesItsOwnShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		options passes.Options
		want    string
	}{
		{
			name:    "a kernel shape with both switched on goes to the kernel",
			body:    "for i := 0; i < 4; i++ { s += a[i] * b[i] }",
			options: bothOptions(),
			want:    "simd.kernels",
		},
		{
			name:    "the same shape with kernels off falls through to the unroller",
			body:    "for i := 0; i < 4; i++ { s += a[i] * b[i] }",
			options: passes.Options{LoopUnroll: true},
			want:    "loop.unroll",
		},
		{
			name:    "with the unroller off the kernel still claims it",
			body:    "for i := 0; i < 4; i++ { s += a[i] * b[i] }",
			options: passes.Options{SIMDKernels: true},
			want:    "simd.kernels",
		},
		{
			name:    "with both off nothing claims it",
			body:    "for i := 0; i < 4; i++ { s += a[i] * b[i] }",
			options: passes.Options{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			loop, info := checkedLoop(t, tt.body)

			matched, _, ok := DefaultRegistry().TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: tt.options}, loop)

			if tt.want == "" {
				require.False(t, ok, "every recogniser reads the options itself, so both off means the plain loop")
				return
			}
			require.True(t, ok)
			require.Equal(t, tt.want, matched.Name())
		})
	}
}

func TestEveryRecogniserInTheDefaultRegistryNamesItself(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()

	require.NotNil(t, registry, "the default registry is what a compilation with no overrides uses")

	loop, info := checkedLoop(t, "for i := 0; i < 4; i++ { s += a[i] * b[i] }")
	kernel, _, ok := registry.TryRecogniseForStmt(patterns.RecogniseContext{Info: info, Options: bothOptions()}, loop)

	require.True(t, ok)
	require.Positive(t, kernel.Priority(),
		"the kernel recogniser has to outrank the unroller, or the broader shape would win the bucket")
	require.NotEmpty(t, kernel.Name(), "a recogniser's name is what the disassembler annotates the loop with")
	require.NotEmpty(t, kernel.AcceptedSignatures(), "a recogniser with no accepted signature would never be dispatched to")
}
