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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func analyseVarInitSource(t *testing.T, source string) VarInitReusePlan {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", source, 0)
	require.NoError(t, err)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	config := types.Config{Importer: importer.ForCompiler(fileSet, "source", nil)}
	_, err = config.Check("main", fileSet, []*ast.File{file}, info)
	require.NoError(t, err)
	globals := map[string]program.GlobalVariableInfo{}
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if name.Name != "_" {
					globals[name.Name] = program.GlobalVariableInfo{Index: len(globals), Kind: isa.RegisterGeneral}
				}
			}
		}
	}
	return AnalyseVarInitReuse([]*ast.File{file}, info, globals)
}

func TestAnalyseVarInitReuse(t *testing.T) {
	for _, testCase := range []struct {
		name, body, reason string
	}{
		{"read-only tables", `
var table = map[string]int{"a": 1}
var names = []string{"a"}
var n = len(names)
func run() int { return table["a"] + len(names) + n + int(names[0][0]) }`, ""},
		{"scalar written", `
var n = 1
func run() int { n++; return n }`, ""},
		{"closure value in an initialiser is runtime code", `
var handler = func() int64 { return time.Now().Unix() }
func run() int64 { return handler() }`, ""},
		{"global passed to a native", `
var names = []string{"b", "a"}
func run() string { sort.Strings(names); return strings.Join(names, "") }`, ""},
		{"impure native", `
var start = time.Now()
func run() int64 { return start.Unix() }`, "calls native time.Now"},
		{"host variable read", `
var args = os.Args
func run() int { return len(args) }`, "reads host variable os.Args"},
		{"impure guest callee", `
var v = build()
func build() int64 { return time.Now().Unix() }
func run() int64 { return v }`, "calls native time.Unix"},
		{"goroutine in an initialiser", `
var c = func() chan int { ch := make(chan int, 1); go func() { ch <- 1 }(); return ch }()
func run() int { return len(c) }`, "a goroutine is started"},
		{"map written", `
var m = map[string]int{}
func run() int { m["k"] = 1; return len(m) }`, "global m: written in place"},
		{"slice element written", `
var s = []int{1}
func run() int { s[0] = 2; return s[0] }`, "global s: written in place"},
		{"address taken", `
var n = 1
func run() *int { return &n }`, "global n: its address is taken"},
		{"reference returned", `
var s = []int{1}
func run() []int { return s }`, "global s: escapes (ReturnStmt)"},
		{"passed to guest", `
var s = []int{1}
func use(x []int) int { return len(x) }
func run() int { return use(s) }`, "global s: passed to guest function use"},
		{"deferred native", `
var names = []string{"b"}
func run() { defer sort.Strings(names) }`, "global names: passed to a go or defer call"},
		{"aliased by a local", `
var s = []int{1}
func run() int { x := s; x[0] = 3; return x[0] }`, "global s: assigned to another variable"},
		{"appended to", `
var s = make([]int, 0, 4)
func run() int { t := append(s, 1); return len(t) }`, "global s: passed to builtin append"},
		{"range into reference values", `
var rows = [][]int{{1}}
func run() int { for _, r := range rows { r[0] = 2 }; return rows[0][0] }`, "global rows: ranged over into a reference-typed variable"},
		{"method on a reference global", `
var b = &strings.Builder{}
func run() int { b.WriteString("x"); return b.Len() }`, "global b: its method WriteString is used"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := "package main\n\nimport (\n\t\"os\"\n\t\"sort\"\n\t\"strings\"\n\t\"time\"\n)\n\nvar _ = os.Getpid\nvar _ = sort.Ints\nvar _ = strings.Join\nvar _ = time.Now\n" + testCase.body
			plan := analyseVarInitSource(t, source)
			require.Equal(t, testCase.reason == "", plan.Eligible, "reason %q", plan.Reason)
			require.Equal(t, testCase.reason, plan.Reason)
		})
	}
}

func TestAnalyseVarInitReuseClassifiesEachUse(t *testing.T) {
	for _, testCase := range []struct {
		name, body, reason string
	}{
		{"value method called on a by-value global", `
type point struct{ x, y int }
func (p point) sum() int { return p.x + p.y }
var origin = point{1, 2}
func run() int { return origin.sum() }`, ""},
		{"value method taken as a value", `
type point struct{ x, y int }
func (p point) sum() int { return p.x + p.y }
var origin = point{1, 2}
func run() func() int { return origin.sum }`, "global origin: its method sum is used"},
		{"pointer method called on a by-value global", `
type counter struct{ n int }
func (c *counter) bump() { c.n++ }
var total = counter{}
func run() { total.bump() }`, "global total: its method bump is used"},
		{"a field of a by-value global written", `
type point struct{ x, y int }
var origin = point{1, 2}
func run() int { origin.x++; origin.y = 3; return origin.x }`, ""},
		{"an array of scalars copied", `
var grid = [2][2]int{{1, 2}, {3, 4}}
func run() int { row := grid[1]; return row[0] + len(grid) + cap(grid[0]) }`, ""},
		{"a by-value global ranged into", `
var last int
var rows = []int{1, 2}
func run() int { for _, last = range rows { }; return last }`, ""},
		{"a reference global ranged into", `
var last []int
var rows = [][]int{{1}}
func run() int { for _, last = range rows { }; return len(last) }`, "global last: written by a range"},
		{"a channel received from", `
var events = make(chan int, 1)
func run() int { return <-events }`, "global events: received from"},
		{"a channel ranged over", `
var events = make(chan int)
func run() int { n := 0; for v := range events { n += v }; return n }`, "global events: received from by a range"},
		{"a channel sent on", `
var events = make(chan int, 1)
func run() { events <- 1 }`, "global events: used in a send"},
		{"scalars ranged over", `
var rows = []int{1, 2}
func run() int { n := 0; for i, _ := range rows { n += i }; for range rows { n++ }; return n }`, ""},
		{"a pointer used as a map key", `
var key = new(int)
var seen = map[*int]bool{}
func run() bool { return seen[key] }`, "global key: used as an index"},
		{"a reference global passed to a method", `
var names = []string{"a"}
var b strings.Builder
func run() int { b.WriteString(names[0]); n, _ := b.WriteString(strings.Join(names, "")); return n }`, "global b: its method WriteString is used"},
		{"a reference global passed to a generic function", `
func count[T any](xs []T) int { return len(xs) }
var names = []string{"a"}
func run() int { return count[string](names) }`, "global names: passed to a generic function"},
		{"a reference global passed to a computed function", `
var names = []string{"a"}
func measure() func([]string) int { return func(xs []string) int { return len(xs) } }
func run() int { return measure()(names) }`, "global names: passed to a computed function"},
		{"a reference global passed to a function value", `
var names = []string{"a"}
func run() int { measure := func(xs []string) int { return len(xs) }; return measure(names) }`, "global names: passed to a function value"},
		{"a reference global passed to an element of a function slice", `
var names = []string{"a"}
var measures = []func([]string) int{func(xs []string) int { return len(xs) }}
func run() int { return measures[0](names) }`, "global names: passed to a function value"},
		{"a reference global passed to a host method", `
var names = []string{"a"}
var replacer = strings.NewReplacer("a", "b")
func run() string { return replacer.Replace(names[0]) }`, "global replacer: its method Replace is used"},
		{"projections of a reference global", `
var table = map[string][]int{"a": {1, 2}}
var boxed any = []int{1}
func run() int { return len(table["a"][1:]) + len((boxed).([]int)) + len([]int(table["a"])) }`, ""},
		{"a dereferenced global", `
var p = new(int)
func run() int { return *p }`, ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := "package main\n\nimport \"strings\"\n\nvar _ = strings.Join\n" + testCase.body
			plan := analyseVarInitSource(t, source)
			require.Equal(t, testCase.reason, plan.Reason)
			require.Equal(t, testCase.reason == "", plan.Eligible, "reason %q", plan.Reason)
		})
	}
}

func TestByValueTypeRejectsReferences(t *testing.T) {
	scalars := types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "n", types.Typ[types.Int], false)}, nil)
	withPointer := types.NewStruct([]*types.Var{types.NewField(token.NoPos, nil, "p", types.NewPointer(types.Typ[types.Int]), false)}, nil)
	require.True(t, byValueType(scalars))
	require.True(t, byValueType(types.NewArray(types.Typ[types.Float64], 3)))
	require.False(t, byValueType(withPointer))
	require.False(t, byValueType(types.NewArray(types.NewPointer(types.Typ[types.Int]), 1)))
	require.False(t, byValueType(types.Typ[types.UnsafePointer]))
	require.False(t, byValueType(types.Typ[types.UntypedNil]))
	require.False(t, byValueType(types.NewSlice(types.Typ[types.Int])))
}
