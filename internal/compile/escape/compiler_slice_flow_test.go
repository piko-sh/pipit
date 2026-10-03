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
)

const sliceFlowFixtureHeader = `package main

type box struct{ buf []byte }

type raw []byte

var global []byte

func keep(b []byte) box { return box{buf: b} }

func ignore(b []byte) int { return len(b) }

func pack(xs ...[]byte) int { return len(xs) }

var packValue = func(xs ...[]byte) int { return len(xs) }

func (b *box) set(x []byte) { b.buf = x }

func (b *box) size(x []byte) int { return len(x) }

func (r raw) keep() { global = r }

func (r raw) size() int { return len(r) }

type setter interface{ set(x []byte) }

type sizer interface{ size(x []byte) int }

type outer struct{ setter }

func f() {
`

func checkedSliceFlowFile(t *testing.T, source string) (*ast.File, *types.Info) {
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
	return file, info
}

func functionDeclarations(file *ast.File) map[string]*ast.FuncDecl {
	declarations := make(map[string]*ast.FuncDecl)
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			declarations[function.Name.Name] = function
		}
	}
	return declarations
}

func everyDeclaration(file *ast.File) []*ast.FuncDecl {
	var declarations []*ast.FuncDecl
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			declarations = append(declarations, function)
		}
	}
	return declarations
}

func TestCollectFlowingSliceNamesFollowsEveryStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		body        string
		wantFlowing bool
	}{
		{name: "a struct literal field", body: `s := []byte{1}; _ = box{buf: s}`, wantFlowing: true},
		{name: "an array literal element", body: `s := []byte{1}; _ = [2][]byte{s, s}`, wantFlowing: true},
		{name: "a slice literal element", body: `s := []byte{1}; _ = [][]byte{s}`, wantFlowing: true},
		{name: "a map literal value", body: `s := []byte{1}; _ = map[int][]byte{1: s}`, wantFlowing: true},
		{name: "a field store", body: `s := []byte{1}; var b box; b.buf = s; _ = b`, wantFlowing: true},
		{name: "an index store", body: `s := []byte{1}; rows := make([][]byte, 1); rows[0] = s`, wantFlowing: true},
		{name: "a package variable store", body: `s := []byte{1}; global = s`, wantFlowing: true},
		{name: "a re-slice of the name", body: `s := []byte{1, 2}; _ = box{buf: s[1:]}`, wantFlowing: true},
		{name: "a conversion of the name", body: `s := []byte{1}; _ = [1]raw{raw(s)}`, wantFlowing: true},
		{name: "a conversion to an interface", body: `s := []byte{1}; _ = [1]any{any(s)}`, wantFlowing: true},
		{name: "a conversion to an array pointer", body: `s := []byte{1}; _ = [1]*[1]byte{(*[1]byte)(s)}`, wantFlowing: true},
		{name: "a returned string conversion", body: `_ = func() string { s := []byte{1}; return string(s) }()`, wantFlowing: false},
		{name: "a stored array conversion", body: `s := []byte{1}; _ = [1][1]byte{[1]byte(s)}`, wantFlowing: false},
		{name: "an appended element", body: `s := []byte{1}; var rows [][]byte; rows = append(rows, s); _ = rows`, wantFlowing: true},
		{name: "a sent value", body: `s := []byte{1}; ch := make(chan []byte, 1); ch <- s`, wantFlowing: true},
		{name: "a returned value", body: `_ = func() []byte { s := []byte{1}; return s }()`, wantFlowing: true},
		{name: "a go argument", body: `s := []byte{1}; go ignore(s)`, wantFlowing: true},
		{name: "an argument packed for a declared variadic", body: `s := []byte{1}; _ = pack(s, s)`, wantFlowing: true},
		{name: "an argument packed for a function value", body: `s := []byte{1}; _ = packValue(s, s)`, wantFlowing: false},
		{name: "an argument to a flowing parameter", body: `s := []byte{1}; _ = keep(s)`, wantFlowing: true},
		{name: "an argument to a flowing method parameter", body: `s := []byte{1}; var b box; b.set(s)`, wantFlowing: true},
		{name: "an argument to a flowing method expression parameter", body: `s := []byte{1}; var b box; (*box).set(&b, s)`, wantFlowing: true},
		{name: "a receiver its method stores", body: `s := raw{1}; s.keep()`, wantFlowing: true},
		{name: "a receiver its method only measures", body: `s := raw{1}; _ = s.size()`, wantFlowing: false},
		{name: "an argument to a method parameter that stays local", body: `s := []byte{1}; var b box; _ = b.size(s)`, wantFlowing: false},
		{name: "an argument to a parameter that stays local", body: `s := []byte{1}; _ = ignore(s)`, wantFlowing: false},
		{name: "a local copy", body: `s := []byte{1}; t := s; _ = t`, wantFlowing: false},
		{name: "a local copy that is stored", body: `s := []byte{1}; t := s; _ = box{buf: t}`, wantFlowing: true},
		{name: "a re-slice assigned to a local that is stored", body: `s := []byte{1, 2}; var t []byte; t = s[1:]; _ = box{buf: t}`, wantFlowing: true},
		{name: "a var-declared copy that is stored", body: `s := []byte{1}; var t = s; global = t`, wantFlowing: true},
		{name: "a copy of a copy that is stored", body: `s := []byte{1}; t := s; u, n := t, 0; _ = n; _ = box{buf: u}`, wantFlowing: true},
		{name: "a stored local that copies from a string conversion", body: `s := []byte{1}; t := []byte(string(s)); _ = box{buf: t}`, wantFlowing: false},
		{name: "an element write into the slice itself", body: `s := []byte{1}; s[0] = 2`, wantFlowing: false},
		{name: "a spread variadic argument", body: `s := [][]byte{}; _ = pack(s...)`, wantFlowing: false},
		{name: "an argument to an interface method an implementation stores", body: `s := []byte{1}; var st setter = &box{}; st.set(s)`, wantFlowing: true},
		{name: "an argument to an embedded interface's method", body: `s := []byte{1}; o := outer{&box{}}; o.set(s)`, wantFlowing: true},
		{name: "an argument to an interface method no implementation stores", body: `s := []byte{1}; var sz sizer = &box{}; _ = sz.size(s)`, wantFlowing: false},
		{name: "an argument to a function value a candidate stores", body: `s := []byte{1}; var fn func([]byte); fn(s)`, wantFlowing: true},
		{name: "an argument to a method value that stores", body: `s := []byte{1}; var b box; fn := b.set; fn(s)`, wantFlowing: true},
		{name: "an argument to a function value no candidate stores", body: `s := []byte{1}; var fn func([]byte) int; _ = fn(s)`, wantFlowing: false},
		{name: "an argument to a closure that stores a captured variable", body: `s := []byte{1}; var kept []byte; set := func(x []byte) { kept = x }; set(s); _ = kept`, wantFlowing: true},
		{name: "an argument to an invoked literal that stores", body: `s := []byte{1}; func(x []byte) { global = x }(s)`, wantFlowing: true},
		{name: "an argument to an invoked literal that only measures", body: `s := []byte{1}; _ = func(x []byte) int { return len(x) }(s)`, wantFlowing: false},
		{name: "a name a closure stores in a captured variable", body: `s := []byte{1}; var local []byte; g := func() { local = s }; g(); _ = local`, wantFlowing: true},
		{name: "a name assigned to a captured variable outside any closure", body: `s := []byte{1}; var local []byte; g := func() int { return len(local) }; local = s; _ = g`, wantFlowing: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, info := checkedSliceFlowFile(t, sliceFlowFixtureHeader+"\t"+tt.body+"\n}\n")
			flows := ComputeSliceFlows(info, everyDeclaration(file))
			function := functionDeclarations(file)["f"]

			flowing := CollectFlowingSliceNames(info, function.Type, function.Body, flows.Lookup)

			require.Equal(t, tt.wantFlowing, flowing["s"])
		})
	}
}

func TestComputeSliceFlowsReachesAFixedPoint(t *testing.T) {
	t.Parallel()

	source := `package main

type box struct{ buf []byte }

func keep(b []byte) box { return box{buf: b} }

func local(b []byte) int { return len(b) }

func outer(b []byte) box { return keep(b) }

func mixed(a, b []byte) box { _ = len(a); return box{buf: b} }

func blank(_ []byte, b []byte) int { return len(b) }

func (x box) with(b []byte) box { return box{buf: b} }

type raw []byte

var kept raw

func (r raw) retain() { kept = r }

func generic[T any](b []T) [][]T { return [][]T{b} }

func callsGeneric(b []byte) [][]byte { return generic(b) }

func noParameters() {}

func loop(b []byte) box {
	if len(b) < 2 {
		return box{buf: b}
	}
	return loop(b[1:])
}

var stashed []byte

func stash(b []byte) { stashed = b }

func apply(fn func([]byte), b []byte) { fn(b) }

func applyGeneric[T any](fn func([]T), v []T) { fn(v) }

type putter interface{ put(b []byte) }

func (x *box) put(b []byte) { x.buf = b }

func viaInterface(p putter, b []byte) { p.put(b) }

type measurer interface{ measure(b []byte) int }

func (x box) measure(b []byte) int { return len(b) }

func viaMeasurer(m measurer, b []byte) int { return m.measure(b) }

func viaClosure(b []byte) {
	var local []byte
	g := func() { local = b }
	g()
	_ = local
}

func readsInClosure(b []byte) int {
	g := func() int { return len(b) }
	return g()
}
`
	file, info := checkedSliceFlowFile(t, source)
	declarations := functionDeclarations(file)
	flows := ComputeSliceFlows(info, everyDeclaration(file))

	tests := []struct {
		name     string
		function string
		want     []bool
	}{
		{name: "a parameter stored in a returned struct", function: "keep", want: []bool{true}},
		{name: "a parameter only measured", function: "local", want: []bool{false}},
		{name: "a parameter passed on to a flowing parameter", function: "outer", want: []bool{true}},
		{name: "one flowing parameter of two", function: "mixed", want: []bool{false, true}},
		{name: "a blank parameter", function: "blank", want: []bool{false, false}},
		{name: "a recursive function", function: "loop", want: []bool{true}},
		{name: "a method's receiver and parameter", function: "with", want: []bool{false, true}},
		{name: "a stored receiver", function: "retain", want: []bool{true}},
		{name: "a parameter passed to a generic function", function: "callsGeneric", want: []bool{true}},
		{name: "a function without parameters", function: "noParameters", want: []bool{}},
		{name: "a parameter passed to a function value a candidate stores", function: "apply", want: []bool{false, true}},
		{name: "a parameter passed to a generic function value", function: "applyGeneric", want: []bool{false, true}},
		{name: "a parameter passed to an interface method an implementation stores", function: "viaInterface", want: []bool{false, true}},
		{name: "a parameter passed to an interface method no implementation stores", function: "viaMeasurer", want: []bool{false, false}},
		{name: "a parameter a closure stores in a captured variable", function: "viaClosure", want: []bool{true}},
		{name: "a parameter a closure only reads", function: "readsInClosure", want: []bool{false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, flows.declared[info.Defs[declarations[tt.function].Name]].flags)
		})
	}
}
