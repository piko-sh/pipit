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
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParenthesesAroundTargetsAreRemoved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "an incremented variable", source: "(x)++", want: "x++"},
		{name: "a doubly parenthesised decrement", source: "((x))--", want: "x--"},
		{name: "a compound assignment", source: "(x) += 1", want: "x += 1"},
		{name: "a plain assignment", source: "(x) = 1", want: "x = 1"},
		{name: "a multiple assignment", source: "(x), (y) = y, x", want: "x, y = y, x"},
		{name: "a dereference", source: "(*p)++", want: "*p++"},
		{name: "a field reached through a parenthesised base", source: "(t).n = 1", want: "t.n = 1"},
		{name: "an element reached through a parenthesised base", source: "(s)[0]--", want: "s[0]--"},
		{name: "a nested path", source: "((t).a)[1] += 2", want: "t.a[1] += 2"},
		{name: "an address operand", source: "q = &(x)", want: "q = &x"},
		{name: "an address operand's path", source: "q = &(t).n", want: "q = &t.n"},
		{name: "range targets", source: "for (k), (v) = range s {\n}", want: "for k, v = range s {\n}"},
		{name: "a target inside a closure", source: "f = func() { (x)++ }", want: "f = func() { x++ }"},
		{name: "a value keeps its parentheses", source: "x = (y) + (z)", want: "x = (y) + (z)"},
		{name: "a short declaration is left alone", source: "w := (y)", want: "w := (y)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fileSet := token.NewFileSet()
			file, err := parser.ParseFile(fileSet, "main.go", "package main\nfunc f() {\n"+tt.source+"\n}\n", 0)
			require.NoError(t, err)

			unparenthesiseTargets([]*ast.File{file})
			unparenthesiseTargets([]*ast.File{file})

			declaration, ok := file.Decls[0].(*ast.FuncDecl)
			require.True(t, ok)
			body := declaration.Body
			var printed bytes.Buffer
			for _, statement := range body.List {
				require.NoError(t, format.Node(&printed, fileSet, statement))
			}
			require.Equal(t, tt.want, printed.String())
		})
	}
}

func TestADereferenceInsideAPathKeepsItsOperand(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "main.go", "package main\nfunc f() { (*(p)).n = 1 }\n", 0)
	require.NoError(t, err)

	unparenthesiseTargets([]*ast.File{file})

	declaration, ok := file.Decls[0].(*ast.FuncDecl)
	require.True(t, ok)
	assignment, ok := declaration.Body.List[0].(*ast.AssignStmt)
	require.True(t, ok)
	selector, ok := assignment.Lhs[0].(*ast.SelectorExpr)
	require.True(t, ok)
	star, ok := selector.X.(*ast.StarExpr)
	require.True(t, ok, "the selector's parenthesised base is removed")
	_, isParen := star.X.(*ast.ParenExpr)
	require.True(t, isParen, "a dereference's operand is a value, left as written")
}
