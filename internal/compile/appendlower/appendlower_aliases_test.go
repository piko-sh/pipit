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

package appendlower

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func checkedFunction(t *testing.T, source string) (*ast.FuncDecl, *types.Info) {
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
	config := &types.Config{Importer: importer.Default()}
	_, err = config.Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err, "the fixture source must type-check")

	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "f" {
			return function, info
		}
	}
	require.FailNow(t, "the fixture source must declare a function named f")
	return nil, nil
}

func firstAssignment(t *testing.T, function *ast.FuncDecl) *ast.AssignStmt {
	t.Helper()
	for _, statement := range function.Body.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok {
			return assignment
		}
	}
	require.FailNow(t, "the fixture function must contain an assignment")
	return nil
}

func appendsIn(t *testing.T, info *types.Info, block *ast.BlockStmt) []*ast.AssignStmt {
	t.Helper()
	var appends []*ast.AssignStmt
	ast.Inspect(block, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if len(typed.Lhs) == 1 && len(typed.Rhs) == 1 {
				if _, _, matched := MatchShape(info, typed.Lhs[0], typed.Rhs[0]); matched {
					appends = append(appends, typed)
				}
			}
		}
		return true
	})
	return appends
}

func sharedAtEachAppend(t *testing.T, info *types.Info, aliases *Aliases, block *ast.BlockStmt) []bool {
	t.Helper()
	appends := appendsIn(t, info, block)
	shared := make([]bool, 0, len(appends))
	for _, assignment := range appends {
		target, call, _ := MatchShape(info, assignment.Lhs[0], assignment.Rhs[0])
		shared = append(shared, aliases.Shared(target, call.End()))
	}
	return shared
}

const aliasFixtureHeader = `package main

type ints []int

type list []int

func (l list) size() int { return len(l) }

func keep(xs []int) {}

func keepInt(xs []int) int { return 0 }

func produce() []int { return nil }

`

func TestCollectAliasesRefusesEveryAppendThroughASharedHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   []bool
	}{
		{
			name:   "a local that is only grown is never shared",
			source: "func f() { var xs []int; xs = append(xs, 1); xs = append(xs, 2) }",
			want:   []bool{false, false},
		},
		{
			name:   "a copy shares both names from the copy on",
			source: "func f() { var xs []int; xs = append(xs, 1); ys := xs; xs = append(xs, 2); ys = append(ys, 3) }",
			want:   []bool{false, true, true},
		},
		{
			name:   "a parenthesised copy",
			source: "func f() { var p []int; p = append(p, 1); q := (p); p = append(p, 2); q = append(q, 3) }",
			want:   []bool{false, true, true},
		},
		{
			name:   "a conversion to a named slice type",
			source: "func f() { var b []int; b = append(b, 1); a := ints(b); b = append(b, 2); a = append(a, 3) }",
			want:   []bool{false, true, true},
		},
		{
			name:   "nested conversions and parentheses",
			source: "func f() { var b []int; b = append(b, 1); a := ints(([]int)(ints(b))); b = append(b, 2); a = append(a, 3) }",
			want:   []bool{false, true, true},
		},
		{
			name:   "a conversion to an interface",
			source: "func f() { var d []int; d = append(d, 1); kept := any(d); d = append(d, 2); _ = kept }",
			want:   []bool{false, true},
		},
		{
			name:   "a conversion of a string copies, so the result is fresh",
			source: "func f(s string) { bs := []byte(s); bs = append(bs, 'x'); rs := []rune(s); rs = append(rs, 'y') }",
			want:   []bool{false, false},
		},
		{
			name:   "a fresh literal, make, nil, slice expression or append",
			source: "func f() { a := []int{1}; b := make([]int, 1); var c []int = nil; d := a[:1]; e := append(b[:0], 1); a = append(a, 1); b = append(b, 1); c = append(c, 1); d = append(d, 1); e = append(e, 1) }",
			want:   []bool{false, false, false, false, false, false},
		},
		{
			name:   "a method value shares its receiver",
			source: "func f() { var m list; m = append(m, 1); size := m.size; m = append(m, 2); _ = size }",
			want:   []bool{false, true},
		},
		{
			name:   "a method call shares its receiver from the call on",
			source: "func f() { var m list; m = append(m, 1); _ = m.size(); m = append(m, 2) }",
			want:   []bool{false, true},
		},
		{
			name:   "a parameter is shared from entry",
			source: "func f(xs []int) { xs = append(xs, 1) }",
			want:   []bool{true},
		},
		{
			name:   "a variadic parameter is shared from entry",
			source: "func f(xs ...int) { xs = append(xs, 1) }",
			want:   []bool{true},
		},
		{
			name:   "a call result may have another handle",
			source: "func f() { xs := produce(); xs = append(xs, 1) }",
			want:   []bool{true},
		},
		{
			name:   "a call argument after the loop leaves the loop's appends alone",
			source: "func f() { var ps []int; for i := 0; i < 3; i++ { ps = append(ps, i) }; keep(ps); ps = append(ps, 9) }",
			want:   []bool{false, true},
		},
		{
			name:   "a call argument inside the loop shares from the loop's start",
			source: "func f() { var ps []int; for i := 0; i < 3; i++ { ps = append(ps, i); keep(ps) } }",
			want:   []bool{true},
		},
		{
			name:   "a call argument before the loop",
			source: "func f() { var ps []int; keep(ps); for i := 0; i < 3; i++ { ps = append(ps, i) } }",
			want:   []bool{true},
		},
		{
			name:   "a call argument in an inner loop shares from the outer loop's start",
			source: "func f() { var ps []int; for j := 0; j < 2; j++ { ps = append(ps, j); for range 2 { keep(ps) } } }",
			want:   []bool{true},
		},
		{
			name:   "an element that passes the slice on runs before the append",
			source: "func f() { var xs []int; xs = append(xs, keepInt(xs)) }",
			want:   []bool{true},
		},
		{
			name:   "goto makes every event count from entry",
			source: "func f() { var ps []int; ps = append(ps, 1); goto done\ndone:\n\tkeep(ps) }",
			want:   []bool{true},
		},
		{
			name:   "an event inside a function literal counts from entry",
			source: "func f() { var ps []int; ps = append(ps, 1); g := func() { keep(ps) }; _ = g }",
			want:   []bool{true},
		},
		{
			name:   "a shadowing variable is a different variable",
			source: "func f() { xs := []int{}; { xs := []int{}; keep(xs) }; xs = append(xs, 1) }",
			want:   []bool{false},
		},
		{
			name:   "a type switch binding holds the switched value",
			source: "func f(v any) { switch xs := v.(type) { case []int: xs = append(xs, 1); _ = xs } }",
			want:   []bool{true},
		},
		{
			name:   "taking the address shares from there on",
			source: "func f() { var ps []int; ps = append(ps, 1); p := &ps; ps = append(ps, 2); _ = p }",
			want:   []bool{false, true},
		},
		{
			name:   "a sent value shares from the send on",
			source: "func f(ch chan []int) { var ps []int; ps = append(ps, 1); ch <- ps; ps = append(ps, 2) }",
			want:   []bool{false, true},
		},
		{
			name:   "a range value shares the element it is copied from",
			source: "func f(groups [][]int) { for _, group := range groups { group = append(group, 1) } }",
			want:   []bool{true},
		},
		{
			name:   "a builtin keeps nothing it is given",
			source: "func f() { xs := make([]int, 1); ys := make([]int, 1); _ = len(xs) + cap(xs); copy(ys, xs); ys = append(ys, xs...); xs = append(xs, 1) }",
			want:   []bool{false, false},
		},
		{
			name:   "a var declaration with a shared value",
			source: "func f() { var xs []int; xs = append(xs, 1); var ys = xs; xs = append(xs, 2); ys = append(ys, 3) }",
			want:   []bool{false, true, true},
		},
		{
			name:   "a tuple assignment shares every target",
			source: "func pair() ([]int, int) { return nil, 0 }\nfunc f() { xs, n := pair(); xs = append(xs, n) }",
			want:   []bool{true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, info := checkedFunction(t, aliasFixtureHeader+tt.source+"\n")

			aliases := CollectAliases(info, Body{Receiver: function.Recv, Parameters: function.Type.Params, Entry: nil, Block: function.Body, Repeats: false})

			require.Equal(t, tt.want, sharedAtEachAppend(t, info, aliases, function.Body),
				"an append through a shared header has to keep allocating")
		})
	}
}

func TestCollectAliasesSharesAValueReceiverFromEntry(t *testing.T) {
	t.Parallel()
	function, info := checkedFunction(t, aliasFixtureHeader+"func (l list) f() int { l = append(l, 9); return len(l) }\n")

	aliases := CollectAliases(info, Body{Receiver: function.Recv, Parameters: function.Type.Params, Entry: nil, Block: function.Body, Repeats: false})

	require.Equal(t, []bool{true}, sharedAtEachAppend(t, info, aliases, function.Body),
		"a value receiver's header is the caller's until the method replaces it")
}

func TestCollectAliasesOfARepeatingBodyCountsFromEntry(t *testing.T) {
	t.Parallel()
	function, info := checkedFunction(t, aliasFixtureHeader+
		"func f(seq func(func(int, []int) bool)) { for k, v := range seq { var ys []int; ys = append(ys, k); v = append(v, 1); keep(ys) } }\n")
	var loop *ast.RangeStmt
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if statement, ok := node.(*ast.RangeStmt); ok {
			loop = statement
		}
		return loop == nil
	})
	require.NotNil(t, loop)

	aliases := CollectAliases(info, Body{Receiver: nil, Parameters: nil, Entry: []ast.Expr{loop.Key, loop.Value}, Block: loop.Body, Repeats: true})

	require.Equal(t, []bool{true, true}, sharedAtEachAppend(t, info, aliases, loop.Body),
		"a range-over-func body runs once per element, so a later event reaches an earlier append, and its value is the iterator's")
}

func TestAliasesWithoutAnAnalysisShareEverything(t *testing.T) {
	t.Parallel()
	function, info := checkedFunction(t, aliasFixtureHeader+"func f() { var xs []int; xs = append(xs, 1) }\n")

	require.Nil(t, CollectAliases(nil, Body{Receiver: nil, Parameters: nil, Entry: nil, Block: function.Body, Repeats: false}),
		"without type information there is no analysis")
	var missing *Aliases
	require.Equal(t, []bool{true}, sharedAtEachAppend(t, info, missing, function.Body),
		"a body that was never analysed must keep allocating")
}

func TestSameSliceHoldsOnlyWhenBothSidesNameOneObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "the append grows the slice it is assigned back to",
			source: "package main\nfunc f(xs []int) { xs = append(xs, 1) }\n",
			want:   true,
		},
		{
			name:   "the append grows a different slice",
			source: "package main\nfunc f(xs, ys []int) { xs = append(ys, 1) }\n",
			want:   false,
		},
		{
			name:   "the first argument is not a plain name",
			source: "package main\nfunc g() []int { return nil }\nfunc f(xs []int) { xs = append(g(), 1) }\n",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, info := checkedFunction(t, tt.source)
			assignment := firstAssignment(t, function)

			lhsIdent, callRHS, matched := MatchShape(info, assignment.Lhs[0], assignment.Rhs[0])
			require.True(t, matched, "the fixture is written in the shape under test")

			require.Equal(t, tt.want, SameSlice(info, lhsIdent, callRHS),
				"growing in place is only sound when the value read and the value written are the same variable")
		})
	}
}

func TestElementKindNamesTheUnderlyingElementAndSpread(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantKind   string
		wantSpread bool
		wantOK     bool
	}{
		{
			name:     "a slice of int",
			source:   "package main\nfunc f(xs []int) { xs = append(xs, 1) }\n",
			wantKind: "int",
			wantOK:   true,
		},
		{
			name:     "a slice of string",
			source:   "package main\nfunc f(xs []string) { xs = append(xs, \"a\") }\n",
			wantKind: "string",
			wantOK:   true,
		},
		{
			name:     "a slice of a named element reports the underlying type",
			source:   "package main\ntype id int\nfunc f(xs []id) { xs = append(xs, 1) }\n",
			wantKind: "int",
			wantOK:   true,
		},
		{
			name:     "a named slice type reports its element",
			source:   "package main\ntype row []float64\nfunc f(xs row) { xs = append(xs, 1) }\n",
			wantKind: "float64",
			wantOK:   true,
		},
		{
			name:       "a spread append of another slice",
			source:     "package main\nfunc f(xs, ys []int) { xs = append(xs, ys...) }\n",
			wantKind:   "int",
			wantSpread: true,
			wantOK:     true,
		},
		{
			name:       "a spread append of a string into a byte slice",
			source:     "package main\nfunc f(bs []byte, s string) { bs = append(bs, s...) }\n",
			wantKind:   "byte",
			wantSpread: true,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, info := checkedFunction(t, tt.source)
			assignment := firstAssignment(t, function)

			_, callRHS, matched := MatchShape(info, assignment.Lhs[0], assignment.Rhs[0])
			require.True(t, matched, "the fixture is written in the shape under test")

			kind, spread, ok := ElementKind(info, callRHS)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantKind, kind,
				"the element name is what picks the typed in-place append, so it has to be the underlying one")
			require.Equal(t, tt.wantSpread, spread)
		})
	}
}

func TestElementKindRefusesWhatItCannotResolve(t *testing.T) {
	t.Parallel()

	function, info := checkedFunction(t, "package main\nfunc f(xs []int) { xs = append(xs, 1) }\n")
	assignment := firstAssignment(t, function)
	_, callRHS, matched := MatchShape(info, assignment.Lhs[0], assignment.Rhs[0])
	require.True(t, matched)

	kind, spread, ok := ElementKind(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}}, callRHS)

	require.False(t, ok, "without a resolved type the lowering has no element width to work from")
	require.Empty(t, kind)
	require.False(t, spread)
}
