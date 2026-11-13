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
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const namedRetypeDeclarations = `package main

type Ints []int
type Pair [2]int
type Index map[string]int
type Lane chan int
type Op func(int) int
type holder struct{ ints Ints; op Op }

func (o Op) twice(x int) int { return o(o(x)) }

func takes(Ints, ...Op) {}
func gives() Ints { return []int{1} }
`

func collectNamedRetypesIn(t *testing.T, body string) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", namedRetypeDeclarations+"func f() {\n"+body+"\n}\n", 0)
	require.NoError(t, err)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, err = (&types.Config{Importer: importer.Default()}).Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err)

	compiler := &Compiler{programContext: &programContext{Info: info}}
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, ok := node.(*ast.FuncLit); ok {
			saved := compiler.currentResultTypes
			compiler.currentResultTypes = nil
			for result := range info.TypeOf(literal).(*types.Signature).Results().Variables() {
				compiler.currentResultTypes = append(compiler.currentResultTypes, result.Type())
			}
			ast.Inspect(literal.Body, func(inner ast.Node) bool {
				compiler.noteNamedRetypes(inner)
				return true
			})
			compiler.currentResultTypes = saved
			return false
		}
		compiler.noteNamedRetypes(node)
		return true
	})
	found := make([]string, 0, len(compiler.namedRetypes))
	for operand, destination := range compiler.namedRetypes {
		found = append(found, types.ExprString(operand)+" -> "+destination.String())
	}
	slices.Sort(found)
	if len(found) == 0 {
		return nil
	}
	return found
}

func TestImplicitConversionsToNamedCompositeTypesAreRecorded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want []string
	}{
		{name: "an assignment", body: "var a Ints\na = []int{1}\n_ = a", want: []string{"[]int{…} -> main.Ints"}},
		{name: "a declaration with a type", body: "var p Pair = [2]int{1, 2}\n_ = p", want: []string{"[2]int{…} -> main.Pair"}},
		{name: "a call argument and variadic arguments", body: "takes([]int{}, func(x int) int { return x })",
			want: []string{"(func(x int) int literal) -> main.Op", "[]int{} -> main.Ints"}},
		{name: "a result", body: "_ = func() Index { return map[string]int{} }", want: []string{"map[string]int{} -> main.Index"}},
		{name: "a struct field", body: "_ = holder{ints: []int{2}}", want: []string{"[]int{…} -> main.Ints"}},
		{name: "a slice element and an appended element", body: "s := []Lane{make(chan int)}\ns = append(s, make(chan int))",
			want: []string{"make(chan int) -> main.Lane", "make(chan int) -> main.Lane"}},
		{name: "a send", body: "c := make(chan Ints, 1)\nc <- []int{3}", want: []string{"[]int{…} -> main.Ints"}},
		{name: "a comparison and a switch case", body: "p := Pair{}\n_ = p == [2]int{}\nswitch p {\ncase [2]int{1, 1}:\n}",
			want: []string{"[2]int{} -> main.Pair", "[2]int{…} -> main.Pair"}},
		{name: "nil is assigned but not compared", body: "var a Ints = nil\n_ = a == nil", want: []string{"nil -> main.Ints"}},
		{name: "a method value", body: "var o Op\nvar twice Op = o.twice\n_ = twice", want: []string{"o.twice -> main.Op"}},
		{name: "a value of the named type itself", body: "a := gives()\nvar b Ints = a\n_ = b", want: nil},
		{name: "a short declaration", body: "a := []int{}\n_ = a", want: nil},
		{name: "a conversion", body: "_ = Ints([]int{})", want: nil},
		{name: "a value into an interface", body: "var x any = []int{}\n_ = x", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, collectNamedRetypesIn(t, tt.body))
		})
	}
}

func TestAReturnInARangeOverFuncBodyIsRecordedAgainstTheEnclosingResults(t *testing.T) {
	t.Parallel()

	source := namedRetypeDeclarations + `
func seq(yield func(int) bool) {}

func f() Ints {
	for range seq {
		return []int{1}
	}
	return nil
}
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", source, 0)
	require.NoError(t, err)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, err = (&types.Config{Importer: importer.Default()}).Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err)
	var inner *ast.ReturnStmt
	ast.Inspect(file, func(node ast.Node) bool {
		if loop, ok := node.(*ast.RangeStmt); ok {
			statement, isReturn := loop.Body.List[0].(*ast.ReturnStmt)
			require.True(t, isReturn)
			inner = statement
		}
		return inner == nil
	})
	require.NotNil(t, inner)
	ints := info.TypeOf(inner.Results[0])

	yieldBody := &Compiler{programContext: &programContext{Info: info}}
	yieldBody.currentResultTypes = []types.Type{types.Typ[types.Bool]}
	yieldBody.noteNamedRetypes(inner)
	require.Empty(t, yieldBody.namedRetypes, "against the yield closure's own result nothing converts")

	yieldBody.rangeOverFunction = &rangeOverFunctionContext{resultTypes: []types.Type{info.TypeOf(file.Decls[len(file.Decls)-1].(*ast.FuncDecl).Type.Results.List[0].Type)}}
	yieldBody.noteNamedRetypes(inner)
	require.Len(t, yieldBody.namedRetypes, 1)
	require.Equal(t, "main.Ints", yieldBody.namedRetypes[inner.Results[0]].String())
	require.Equal(t, "[]int", ints.String())
}

const childCompileEnvironment = "PIPIT_COMPILE_CHILD_PROGRAM"

func TestANamedPointerTypeOnACycleOfNamedCompositeTypesCompiles(t *testing.T) {
	if os.Getenv(childCompileEnvironment) == "1" {
		compileSnippet(t, "package main\n\ntype L []P\ntype P *map[P]L\n\nfunc main() {\n\tvar l L\n\tl = append(l, nil)\n\t_ = l\n}\n")
		return
	}
	t.Parallel()

	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable,
		"-test.run=^TestANamedPointerTypeOnACycleOfNamedCompositeTypesCompiles$", "-test.count=1")
	command.Env = append(os.Environ(), childCompileEnvironment+"=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "type L []P; type P *map[P]L must compile in the child:\n%s", output)
}

func TestARingOfAThousandNamedSliceTypesCompiles(t *testing.T) {
	t.Parallel()

	const count = 1000
	var source strings.Builder
	source.WriteString("package main\n\n")
	for index := range count {
		fmt.Fprintf(&source, "type T%d []T%d\n", index, (index+1)%count)
	}
	source.WriteString("\nfunc main() {\n\tvar first T0\n\t_ = first\n}\n")
	compileSnippet(t, source.String())
}
