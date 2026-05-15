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

package escape

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func checkedBody(t *testing.T, source string) (*ast.BlockStmt, *types.Info) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", source, 0)
	require.NoError(t, err, "the fixture source must parse")

	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	_, err = (&types.Config{Importer: importer.Default()}).Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err, "the fixture source must type-check")

	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "f" {
			return function.Body, info
		}
	}
	t.Fatal("the fixture source must declare a function named f")
	return nil, nil
}

func wrapBody(body string) string {
	return "package main\n\ntype record struct {\n\tN int\n\tS string\n}\n\nfunc sink(...any) {}\n\nfunc f() {\n" + body + "\n}\n"
}

func requireNameSet(t *testing.T, want, got map[string]bool, message string) {
	t.Helper()
	if len(want) == 0 {
		require.Empty(t, got, message)
		return
	}
	require.Equal(t, want, got, message)
}

func TestTheUnfilteredCaptureScanNamesEveryFreeIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{
			name: "a local read inside a closure",
			body: "n := 1\ng := func() int { return n }\nsink(g)",
			want: map[string]bool{"n": true},
		},
		{
			name: "a local written inside a closure",
			body: "n := 1\ng := func() { n = 2 }\nsink(g, n)",
			want: map[string]bool{"n": true},
		},
		{
			name: "a capture through a nested closure",
			body: "n := 1\ng := func() func() int { return func() int { return n } }\nsink(g)",
			want: map[string]bool{"n": true},
		},
		{

			name: "a package-level name used inside a closure",
			body: "n := 1\ngo func() { sink(n) }()",
			want: map[string]bool{"n": true, "sink": true},
		},
		{
			name: "a builtin used inside a closure",
			body: "s := \"a\"\ng := func() int { return len(s) }\nsink(g)",
			want: map[string]bool{"len": true, "s": true},
		},
		{
			name: "a local of the closure itself is not free",
			body: "g := func() int { m := 1; return m }\nsink(g)",
			want: map[string]bool{},
		},
		{
			name: "a parameter of the closure is not free",
			body: "g := func(m int) int { return m }\nsink(g)",
			want: map[string]bool{},
		},
		{
			name: "a body with no closure at all",
			body: "n := 1\nsink(n)",
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, _ := checkedBody(t, wrapBody(tt.body))

			requireNameSet(t, tt.want, CollectClosureCapturedNamesAll(body),
				"a captured local outlives the frame, so missing one would leave a closure reading freed registers")
		})
	}
}

func TestTheFilteredCaptureScanKeepsOnlyTheCapturesThatAreWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{

			name: "a capture that is only read",
			body: "n := 1\ng := func() int { return n }\nsink(g)",
			want: map[string]bool{},
		},
		{
			name: "a capture that is written",
			body: "n := 1\ng := func() { n = 2 }\nsink(g, n)",
			want: map[string]bool{"n": true},
		},
		{
			name: "one capture written and one only read",
			body: "n := 1\nm := 2\ng := func() int { m = n; return m }\nsink(g, m)",
			want: map[string]bool{"m": true},
		},
		{
			name: "a capture written inside a goroutine literal",
			body: "n := 1\ngo func() { n = 2 }()\nsink(n)",
			want: map[string]bool{"n": true},
		},
		{
			name: "a capture written inside a deferred literal",
			body: "n := 1\ndefer func() { n = 2 }()\nsink(n)",
			want: map[string]bool{"n": true},
		},
		{
			name: "a package-level name is never a capture",
			body: "n := 1\ngo func() { sink(n) }()",
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, info := checkedBody(t, wrapBody(tt.body))

			requireNameSet(t, tt.want, CollectClosureCapturedNamesFiltered(&Context{Info: info}, body),
				"only a capture the closure writes needs a shared cell")
		})
	}
}

func TestScansOfAnAbsentBodyFindNothing(t *testing.T) {
	t.Parallel()

	require.Nil(t, CollectClosureCapturedNamesAll(nil),
		"a function with no body captures nothing")
	require.Nil(t, CollectWrittenLocalNames(nil))
	require.Nil(t, CollectDirectlyAssignedNames(nil, nil))
	require.Nil(t, ClassifyTypedSliceLocals(nil, nil))
	require.Nil(t, ClassifyHeapTypedSliceLocals(nil, nil))
}

func TestCollectingWrittenLocalNamesFollowsEveryWriteToItsRootName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{name: "a plain assignment", body: "n := 1\nn = 2\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a compound assignment", body: "n := 1\nn += 2\nsink(n)", want: map[string]bool{"n": true}},
		{name: "an increment", body: "n := 1\nn++\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a decrement", body: "n := 1\nn--\nsink(n)", want: map[string]bool{"n": true}},
		{name: "two names written at once", body: "n := 1\nm := 2\nn, m = m, n\nsink(n, m)", want: map[string]bool{"n": true, "m": true}},
		{name: "a field written through the name", body: "r := record{}\nr.N = 1\nsink(r)", want: map[string]bool{"r": true}},
		{name: "an element written through the name", body: "xs := []int{1}\nxs[0] = 2\nsink(xs)", want: map[string]bool{"xs": true}},
		{name: "the address of the name taken", body: "n := 1\nsink(&n)", want: map[string]bool{"n": true}},
		{name: "a loop counter stepped by its own body", body: "for i := 0; i < 2; i++ { i = i + 1 }", want: map[string]bool{"i": true}},
		{name: "a name only read", body: "n := 1\nsink(n)", want: map[string]bool{}},
		{name: "a name only declared and shadowed", body: "n := 1\n{ n := 2; sink(n) }\nsink(n)", want: map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, _ := checkedBody(t, wrapBody(tt.body))

			requireNameSet(t, tt.want, CollectWrittenLocalNames(body),
				"a local that is written cannot be folded into the value it was declared with")
		})
	}
}

func TestCollectingDirectlyAssignedNamesSeparatesAssignmentFromEveryOtherWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{name: "a name assigned a whole value", body: "n := 1\nm := 2\nn = m\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a name assigned an expression", body: "n := 1\nn = n + 1\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a name stepped", body: "n := 1\nn++\nsink(n)", want: map[string]bool{"n": true}},
		{name: "two names assigned at once", body: "n := 1\nm := 2\nn, m = m, n\nsink(n, m)", want: map[string]bool{"n": true, "m": true}},
		{name: "a type switch binding", body: "var v any\nswitch x := v.(type) { case int: sink(x) }", want: map[string]bool{"x": true}},
		{name: "a name only declared", body: "n := 1\nsink(n)", want: map[string]bool{}},
		{name: "a field assigned rather than the name", body: "r := record{}\nr.N = 1\nsink(r)", want: map[string]bool{}},
		{name: "the address of the name taken", body: "n := 1\nsink(&n)", want: map[string]bool{}},
		{name: "a name shadowed in an inner block", body: "n := 1\n{ n := 2; sink(n) }\nsink(n)", want: map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, info := checkedBody(t, wrapBody(tt.body))

			requireNameSet(t, tt.want, CollectDirectlyAssignedNames(info, body),
				"only a whole-value assignment counts here, not a write through a field or a pointer")
		})
	}
}

func TestCollectingLocalDefsNamesEveryDeclarationForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{name: "a short declaration", body: "n := 1\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a var declaration", body: "var n int\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a var block", body: "var (\n\tn int\n\ts string\n)\nsink(n, s)", want: map[string]bool{"n": true, "s": true}},
		{name: "a range declaration", body: "for i, v := range []int{1} { sink(i, v) }", want: map[string]bool{"i": true, "v": true}},
		{name: "a loop counter", body: "for i := 0; i < 2; i++ { sink(i) }", want: map[string]bool{"i": true}},
		{name: "a type switch binding", body: "var v any\nswitch x := v.(type) { case int: sink(x) }", want: map[string]bool{"v": true, "x": true}},
		{name: "a declaration inside a nested block", body: "{ n := 1; sink(n) }", want: map[string]bool{"n": true}},
		{name: "a body with no declarations", body: "sink(1)", want: map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, _ := checkedBody(t, wrapBody(tt.body))

			definitions := make(map[string]bool)
			CollectLocalDefs(body, definitions)

			requireNameSet(t, tt.want, definitions,
				"a name the body declares is not free, so the declaration forms have to be known")
		})
	}
}

func TestCollectingHeapPromotedNamesCoversCapturesAndAddressTaking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{name: "an addressed local", body: "n := 1\np := &n\nsink(p)", want: map[string]bool{"n": true}},
		{name: "two locals addressed", body: "n := 1\nm := 2\nsink(&n, &m)", want: map[string]bool{"n": true, "m": true}},
		{name: "a field addressed through the local", body: "r := record{}\nsink(&r.N)", want: map[string]bool{"r": true}},
		{name: "a capture that is written", body: "n := 1\ng := func() { n = 2 }\nsink(g, n)", want: map[string]bool{"n": true}},
		{name: "a capture written from a deferred literal", body: "n := 1\ndefer func() { n = 2 }()\nsink(n)", want: map[string]bool{"n": true}},
		{name: "a capture that is only read stays in its register", body: "n := 1\ng := func() int { return n }\nsink(g)", want: nil},
		{name: "a local neither captured nor addressed", body: "n := 1\nsink(n)", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, info := checkedBody(t, wrapBody(tt.body))

			requireNameSet(t, tt.want, CollectHeapPromotedNames(&Context{Info: info}, body),
				"a local whose address outlives its register has to live on the heap instead")
		})
	}
}

func TestCollectingFreeVariablesOfOneLiteralIgnoresItsOwnNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{name: "one free name", body: "n := 1\ng := func() int { return n }\nsink(g)", want: map[string]bool{"n": true}},
		{name: "a name declared inside", body: "g := func() int { m := 1; return m }\nsink(g)", want: map[string]bool{}},
		{name: "a parameter of the literal", body: "g := func(m int) int { return m }\nsink(g)", want: map[string]bool{}},
		{name: "a name declared inside shadowing a free one", body: "n := 1\ng := func() int { n := 2; return n }\nsink(g, n)", want: map[string]bool{}},
		{name: "a range variable of the literal", body: "g := func(xs []int) int { t := 0; for _, v := range xs { t += v }; return t }\nsink(g)", want: map[string]bool{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, _ := checkedBody(t, wrapBody(tt.body))

			var literal *ast.FuncLit
			ast.Inspect(body, func(node ast.Node) bool {
				if found, ok := node.(*ast.FuncLit); ok && literal == nil {
					literal = found
				}
				return literal == nil
			})
			require.NotNil(t, literal, "the fixture must contain a function literal")

			captured := make(map[string]bool)
			CollectFreeVarsForLit(literal, captured)

			requireNameSet(t, tt.want, captured,
				"only a name the literal does not declare itself is captured from the enclosing frame")
		})
	}
}

func TestClassifyingTypedSliceLocalsKeepsTheCanonicalWidthsOnTheirBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]isa.RegisterKind
	}{
		{name: "a slice of int", body: "xs := make([]int, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceInt}},
		{name: "a slice of float64", body: "xs := make([]float64, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceFloat}},
		{name: "a slice of string", body: "xs := make([]string, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceString}},
		{name: "a slice of bool", body: "xs := make([]bool, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceBool}},
		{name: "a slice of byte", body: "xs := make([]byte, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceByte}},
		{name: "a slice of uint64", body: "xs := make([]uint64, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceUint}},
		{name: "a slice declared with var", body: "var xs = make([]int, 1)\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceInt}},
		{name: "a slice read out of a map", body: "m := map[string][]int{}\nxs := m[\"k\"]\n_ = len(xs)", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceInt}},
		{name: "a slice boxed into an interface keeps its bank", body: "xs := make([]int, 1)\nvar ys any = xs\n_ = ys", want: map[string]isa.RegisterKind{"xs": isa.RegisterSliceInt}},
		{name: "a slice of a narrow element", body: "xs := make([]int32, 1)\n_ = len(xs)", want: nil},
		{name: "a slice of a struct", body: "xs := make([]record, 1)\n_ = len(xs)", want: nil},
		{name: "a slice whose address is taken", body: "xs := make([]int, 1)\n_ = &xs", want: nil},
		{name: "a slice passed to a variadic interface parameter", body: "xs := make([]int, 1)\nsink(xs)", want: nil},
		{name: "a slice built as a literal rather than made", body: "xs := []int{1}\n_ = len(xs)", want: nil},
		{name: "a value that is not a slice", body: "n := 1\n_ = n", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, info := checkedBody(t, wrapBody(tt.body))

			got := ClassifyTypedSliceLocals(&Context{Info: info}, body)

			if len(tt.want) == 0 {
				require.Empty(t, got,
					"a local the typed banks cannot hold falls back to the boxed header")
				return
			}
			require.Equal(t, tt.want, got,
				"a typed bank avoids boxing the header, which only the canonical element widths have")
		})
	}
}

func TestClassifyingHeapTypedSliceLocalsPicksTheOnesAClosureHolds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want map[string]bool
	}{
		{
			name: "a typed slice captured by a closure",
			body: "xs := make([]int, 1)\ng := func() int { return len(xs) }\n_ = g",
			want: map[string]bool{"xs": true},
		},
		{
			name: "a typed slice used only in its own frame",
			body: "xs := make([]int, 1)\n_ = len(xs)",
			want: map[string]bool{},
		},
		{
			name: "a typed slice boxed into an interface",
			body: "xs := make([]int, 1)\nvar ys any = xs\n_ = ys",
			want: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, info := checkedBody(t, wrapBody(tt.body))
			typedLocals := ClassifyTypedSliceLocals(&Context{Info: info}, body)

			requireNameSet(t, tt.want, ClassifyHeapTypedSliceLocals(body, typedLocals),
				"a typed slice a closure outlives has to be allocated where the closure can still reach it")
		})
	}
}
