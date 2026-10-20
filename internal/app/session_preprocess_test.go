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

package app

import (
	"go/ast"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreprocessShortVar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		want    string
		wantErr string
	}{
		{name: "rewrites a top-level definition", source: `a := 1`, want: "var a = 1"},
		{name: "leaves an indented definition alone", source: `  a := 1`, want: "  a := 1"},
		{name: "leaves an if statement alone", source: `if x := 1; x > 0 { _ = x }`, want: `if x := 1; x > 0 { _ = x }`},
		{name: "leaves a for statement alone", source: `for i := 0; i < 1; i++ { _ = i }`, want: `for i := 0; i < 1; i++ { _ = i }`},
		{name: "leaves a switch statement alone", source: `switch x := 1; x { case 1: _ = x }`, want: `switch x := 1; x { case 1: _ = x }`},
		{name: "leaves a go statement alone", source: `go run()`, want: `go run()`},
		{name: "leaves a defer statement alone", source: `defer cleanup()`, want: `defer cleanup()`},
		{name: "leaves a return statement alone", source: `return result`, want: `return result`},
		{name: "leaves a function body alone", source: "func main() {\na := 1\n}", want: "func main() {\na := 1\n}"},
		{name: "empty input", source: "", want: ""},
		{name: "a right-hand side with braces", source: `x := []int{1, 2, 3}`, want: "var x = []int{1, 2, 3}"},
		{name: "several names", source: `a, b := f()`, wantErr: "multiple names"},
		{name: "blank identifier", source: `_ := f()`, wantErr: "blank identifier"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := preprocessShortVar(tt.source)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, out)
		})
	}
}

func TestPreprocessShortVar_PreservesMultiLineProgram(t *testing.T) {
	t.Parallel()
	source := `import "fmt"
a := 42
func run() string { return fmt.Sprintf("%d", a) }`
	out, err := preprocessShortVar(source)
	require.NoError(t, err)
	require.Contains(t, out, "var a = 42")
	require.Contains(t, out, `import "fmt"`)
	require.Contains(t, out, `func run()`)
}

func TestStartsWithControlKeyword_AllPrefixes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code string
		want bool
	}{
		{code: "if x := 1", want: true}, {code: "if\tx := 1", want: true},
		{code: "for i := 0", want: true}, {code: "for\ti := 0", want: true},
		{code: "switch x := 1", want: true}, {code: "switch\tx := 1", want: true},
		{code: "select ", want: true}, {code: "select\t", want: true},
		{code: "go run()", want: true}, {code: "go\trun()", want: true},
		{code: "defer cleanup()", want: true}, {code: "defer\tcleanup()", want: true},
		{code: "return result", want: true}, {code: "return\tresult", want: true},
		{code: "a := 1", want: false},
		{code: "if(x)", want: false},
		{code: "ifx := 1", want: false},
		{code: "goto label", want: false},
		{code: "", want: false},
	}
	for _, tt := range tests {
		t.Run(strconv.Quote(tt.code), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, startsWithControlKeyword(tt.code))
		})
	}
}

func TestContainsTopLevelDefine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "a top-level definition", source: `a := 1`, want: true},
		{name: "a definition before a brace block", source: `x := struct{}{}`, want: true},
		{name: "no definition at all", source: `map[string]struct{}{a: {}}`, want: false},
		{name: "a definition inside a function body", source: `func f() { a := 1 }`, want: false},
		{name: "empty input", source: ``, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, containsTopLevelDefine(tt.source))
		})
	}
}

func TestParseSingleStatement_HappyPath(t *testing.T) {
	t.Parallel()
	stmt, ok := parseSingleStatement(`a := 1`)
	require.True(t, ok)
	assign, isAssign := stmt.(*ast.AssignStmt)
	require.True(t, isAssign, "expected *ast.AssignStmt, got %T", stmt)
	require.Len(t, assign.Lhs, 1)
	require.Len(t, assign.Rhs, 1)
}

func TestParseSingleStatement_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "a parse error", source: `!!!`},
		{name: "several statements", source: `a := 1; b := 2`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := parseSingleStatement(tt.source)
			require.False(t, ok)
		})
	}
}

func TestBraceTracker_NestingAndUnnesting(t *testing.T) {
	t.Parallel()
	tracker := newBraceTracker()
	require.True(t, tracker.atTopLevel())

	tracker.advance(`func main() {`)
	require.False(t, tracker.atTopLevel(), "after entering func body, not at top level")

	tracker.advance(`if true {`)
	require.False(t, tracker.atTopLevel(), "still inside two braces")

	tracker.advance(`}`)
	require.False(t, tracker.atTopLevel(), "one close still inside func")

	tracker.advance(`}`)
	require.True(t, tracker.atTopLevel(), "two closes returns to package scope")
}

func TestBraceTracker_ExtraCloseDoesNotUnderflow(t *testing.T) {
	t.Parallel()
	tracker := newBraceTracker()
	tracker.advance(`}`)
	require.True(t, tracker.atTopLevel(), "unmatched close should clamp depth to zero")
}

func TestBraceTracker_BracesInStringLiteralIgnored(t *testing.T) {
	t.Parallel()
	tracker := newBraceTracker()
	tracker.advance(`s := "{}"`)
	require.True(t, tracker.atTopLevel(),
		"braces inside string literal should not affect depth")
}

func TestRenderExprList(t *testing.T) {
	t.Parallel()
	expressions := []ast.Expr{
		&ast.Ident{Name: "a"},
		&ast.Ident{Name: "b"},
		&ast.BasicLit{Value: "3"},
	}
	require.Equal(t, "a, b, 3", renderExprList(expressions))
}

func TestExprAsString_FallbackBranch(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a", exprAsString(&ast.Ident{Name: "a"}))
	require.Equal(t, "42", exprAsString(&ast.BasicLit{Value: "42"}))
	require.Equal(t, "<expr>", exprAsString(&ast.BinaryExpr{
		X: &ast.Ident{Name: "a"}, Y: &ast.Ident{Name: "b"},
	}),
		"binary expression should hit the fallback branch")
}

func TestPreprocessShortVar_TrailingWhitespaceTolerated(t *testing.T) {
	t.Parallel()
	out, err := preprocessShortVar("a := 1   \t\r")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "var a = 1"),
		"trailing whitespace should not break the rewrite")
}
