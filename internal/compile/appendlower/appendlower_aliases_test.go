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
	t.Fatal("the fixture source must declare a function named f")
	return nil, nil
}

func firstAssignment(t *testing.T, function *ast.FuncDecl) *ast.AssignStmt {
	t.Helper()
	for _, statement := range function.Body.List {
		if assignment, ok := statement.(*ast.AssignStmt); ok {
			return assignment
		}
	}
	t.Fatal("the fixture function must contain an assignment")
	return nil
}

func TestCollectAliasesRecordsEverySliceWhoseHeaderIsShared(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   map[string]bool
	}{
		{
			name:   "a slice parameter can always be shared with the caller",
			source: "package main\nfunc f(xs []int) {}\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "two slice parameters are both recorded",
			source: "package main\nfunc f(xs, ys []int) {}\n",
			want:   map[string]bool{"xs": true, "ys": true},
		},
		{
			name:   "a non-slice parameter shares nothing",
			source: "package main\nfunc f(n int) {}\n",
			want:   nil,
		},
		{
			name:   "an array parameter is copied, not shared",
			source: "package main\nfunc f(a [4]int) {}\n",
			want:   nil,
		},
		{
			name:   "a blank parameter name has nothing to record",
			source: "package main\nfunc f(_ []int) {}\n",
			want:   nil,
		},
		{
			name:   "assigning a local to another name shares its header",
			source: "package main\nfunc f() { xs := make([]int, 1); ys := xs; ys[0] = 1 }\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "taking the address of a local shares it",
			source: "package main\nfunc g(*[]int) {}\nfunc f() { xs := make([]int, 1); g(&xs) }\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "a var declaration with a value shares it",
			source: "package main\nfunc f() { xs := make([]int, 1); var ys = xs; ys[0] = 1 }\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "a local that is only grown in place is not shared",
			source: "package main\nfunc f() { xs := make([]int, 1); xs = append(xs, 1); xs[0] = 2 }\n",
			want:   nil,
		},
		{
			name: "a name handed to the blank identifier is recorded all the same",

			source: "package main\nfunc f() { xs := make([]int, 1); _ = xs }\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "a chain of two assignments records both sources",
			source: "package main\nfunc f() { xs := make([]int, 1); ys := xs; zs := ys; zs[0] = 1 }\n",
			want:   map[string]bool{"xs": true, "ys": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, info := checkedFunction(t, tt.source)

			got := CollectAliases(info, function.Type.Params, function.Body)

			require.Equal(t, tt.want, got,
				"an append through a shared header has to keep allocating, so every sharing form must be recorded")
		})
	}
}

func TestCollectAliasesFallsBackToSyntaxWithoutTypeInformation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   map[string]bool
	}{
		{
			name:   "a slice parameter is recognised from its syntax alone",
			source: "package main\nfunc f(xs []int) {}\n",
			want:   map[string]bool{"xs": true},
		},
		{
			name:   "an array parameter is told apart by its length",
			source: "package main\nfunc f(a [4]int) {}\n",
			want:   nil,
		},
		{
			name:   "a map parameter is not a slice",
			source: "package main\nfunc f(m map[int]int) {}\n",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function, _ := checkedFunction(t, tt.source)

			require.Equal(t, tt.want, CollectAliases(nil, function.Type.Params, function.Body))
		})
	}
}

func TestCollectAliasesOfNothingIsNothing(t *testing.T) {
	t.Parallel()

	require.Nil(t, CollectAliases(nil, nil, nil),
		"a function with neither parameters nor a body shares no header at all")
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

			lhsIdent, callRHS, matched := MatchShape(assignment.Lhs[0], assignment.Rhs[0])
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

			_, callRHS, matched := MatchShape(assignment.Lhs[0], assignment.Rhs[0])
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

	function, _ := checkedFunction(t, "package main\nfunc f(xs []int) { xs = append(xs, 1) }\n")
	assignment := firstAssignment(t, function)
	_, callRHS, matched := MatchShape(assignment.Lhs[0], assignment.Rhs[0])
	require.True(t, matched)

	kind, spread, ok := ElementKind(&types.Info{Types: map[ast.Expr]types.TypeAndValue{}}, callRHS)

	require.False(t, ok, "without a resolved type the lowering has no element width to work from")
	require.Empty(t, kind)
	require.False(t, spread)
}
