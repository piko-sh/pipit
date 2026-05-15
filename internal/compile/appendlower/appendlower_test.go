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
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseExpression(t *testing.T, source string) ast.Expr {
	t.Helper()
	expression, err := parser.ParseExpr(source)
	require.NoError(t, err, "the fixture expression must parse")
	return expression
}

func parseAssignment(t *testing.T, source string) (ast.Expr, ast.Expr) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", "package main\nfunc f() {\n"+source+"\n}\n", 0)
	require.NoError(t, err, "the fixture statement must parse")

	declaration, ok := file.Decls[0].(*ast.FuncDecl)
	require.True(t, ok, "the fixture declares a function")
	assignment, ok := declaration.Body.List[0].(*ast.AssignStmt)
	require.True(t, ok, "the fixture statement is an assignment")
	return assignment.Lhs[0], assignment.Rhs[0]
}

func TestMatchShapeRecognisesASingleElementAppendBackToItself(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "an append of one element", source: "xs = append(xs, 1)", want: true},
		{name: "an append to a different name still matches the shape", source: "ys = append(xs, 1)", want: true},
		{name: "an append of two elements is not the lowered shape", source: "xs = append(xs, 1, 2)", want: false},
		{name: "an append of no elements is not the lowered shape", source: "xs = append(xs)", want: false},
		{name: "another builtin is not an append", source: "xs = copy(xs, ys)", want: false},
		{name: "a non-call right-hand side is not an append", source: "xs = ys", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			leftHandSide, rightHandSide := parseAssignment(t, tt.source)

			ident, call, ok := MatchShape(leftHandSide, rightHandSide)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.NotNil(t, ident)
				require.NotNil(t, call)
			}
		})
	}

	t.Run("an index expression on the left is not the lowered shape", func(t *testing.T) {
		t.Parallel()
		leftHandSide := parseExpression(t, "xs[0]")
		rightHandSide := parseExpression(t, "append(xs, 1)")

		_, _, ok := MatchShape(leftHandSide, rightHandSide)

		require.False(t, ok, "the destination must be a plain name for the in-place lowering")
	})
}
