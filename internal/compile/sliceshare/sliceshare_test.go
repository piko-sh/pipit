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

package sliceshare

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func checkedExpression(t *testing.T, declarations, expression string) (ast.Expr, *types.Info) {
	t.Helper()
	source := "package main\n" + declarations + "\nfunc f(xs []int, s string, b []byte) {\n\t_ = " + expression + "\n}\n"
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", source, 0)
	require.NoError(t, err, "the fixture source must parse")
	info := &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	_, err = (&types.Config{}).Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err, "the fixture source must type-check")
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "f" {
			assignment, isAssign := function.Body.List[0].(*ast.AssignStmt)
			require.True(t, isAssign, "the fixture body is one assignment")
			return assignment.Rhs[0], info
		}
	}
	require.FailNow(t, "the fixture source must declare f")
	return nil, nil
}

func TestStripSharing(t *testing.T) {
	t.Parallel()

	const declarations = "type ints []int\nfunc g(xs []int) []int { return xs }"
	tests := []struct {
		name       string
		expression string
		want       string
	}{
		{name: "a plain name", expression: "xs", want: "xs"},
		{name: "a parenthesised name", expression: "(xs)", want: "xs"},
		{name: "a named slice conversion", expression: "ints(xs)", want: "xs"},
		{name: "nested conversions and parentheses", expression: "([]int)((ints)(xs))", want: "xs"},
		{name: "a conversion to an interface", expression: "any(xs)", want: "xs"},
		{name: "a conversion to a pointer to an array", expression: "(*[2]int)(xs)", want: "xs"},
		{name: "a conversion to an array copies", expression: "[2]int(xs)", want: "[2]int(xs)"},
		{name: "a conversion to a string copies", expression: "string(b)", want: "string(b)"},
		{name: "a conversion from a string copies", expression: "[]byte(s)", want: "[]byte(s)"},
		{name: "a function call is not a conversion", expression: "g(xs)", want: "g(xs)"},
		{name: "a conversion of a call stops at the call", expression: "ints(g(xs))", want: "g(xs)"},
		{name: "a slice expression is not stripped", expression: "ints(xs[1:])", want: "xs[1:]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expression, info := checkedExpression(t, declarations, tt.expression)

			require.Equal(t, tt.want, types.ExprString(StripSharing(info, expression)))
		})
	}

	t.Run("without type information only parentheses are removed", func(t *testing.T) {
		t.Parallel()
		expression, _ := checkedExpression(t, declarations, "((ints(xs)))")

		require.Equal(t, "ints(xs)", types.ExprString(StripSharing(nil, expression)))
	})
}

func TestBuiltinName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		declarations string
		expression   string
		withInfo     bool
		wantName     string
		wantBuiltin  bool
	}{
		{name: "append", expression: "append(xs, 1)", withInfo: true, wantName: "append", wantBuiltin: true},
		{name: "a parenthesised builtin", expression: "(len)(xs)", withInfo: true, wantName: "len", wantBuiltin: true},
		{
			name:         "a function that shadows append",
			declarations: "func append(xs []int, x int) []int { return xs }",
			expression:   "append(xs, 1)",
			withInfo:     true,
			wantName:     "append",
			wantBuiltin:  false,
		},
		{
			name:         "a shadowing function counts as the builtin without type information",
			declarations: "func append(xs []int, x int) []int { return xs }",
			expression:   "append(xs, 1)",
			withInfo:     false,
			wantName:     "append",
			wantBuiltin:  true,
		},
		{
			name:         "a user function",
			declarations: "func g(xs []int) []int { return xs }",
			expression:   "g(xs)",
			withInfo:     true,
			wantName:     "g",
			wantBuiltin:  false,
		},
		{name: "a conversion", expression: "[]int(xs)", withInfo: true, wantName: "", wantBuiltin: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			expression, info := checkedExpression(t, tt.declarations, tt.expression)
			call, ok := expression.(*ast.CallExpr)
			require.True(t, ok, "the fixture expression is a call")
			if !tt.withInfo {
				info = nil
			}

			name, builtin := BuiltinName(info, call)

			require.Equal(t, tt.wantName, name)
			require.Equal(t, tt.wantBuiltin, builtin)
		})
	}
}
