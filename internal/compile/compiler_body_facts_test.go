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
)

func bodyFactsOf(t *testing.T, body string) (bodyFacts, bodyFacts) {
	t.Helper()
	source := "package p\n\nimport \"runtime\"\n\nvar _ = runtime.NumCPU\n\nfunc f() {\n" + body + "\n}\n"
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "p.go", source, parser.SkipObjectResolution)
	require.NoError(t, err)
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object), Types: make(map[ast.Expr]types.TypeAndValue)}
	_, err = (&types.Config{Importer: importer.Default()}).Check("p", files, []*ast.File{file}, info)
	require.NoError(t, err)
	var declaration *ast.FuncDecl
	for _, candidate := range file.Decls {
		if function, ok := candidate.(*ast.FuncDecl); ok {
			declaration = function
		}
	}
	require.NotNil(t, declaration)
	return scanBodyFacts(info, declaration.Body), scanBodyFacts(nil, declaration.Body)
}

func bodyShapeOf(t *testing.T, body string) (bodyShape, bodyShape) {
	t.Helper()
	source := "package p\n\nfunc f(words []string, n int) {\n" + body + "\n}\n"
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "p.go", source, parser.SkipObjectResolution)
	require.NoError(t, err)
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object), Types: make(map[ast.Expr]types.TypeAndValue)}
	_, err = (&types.Config{Importer: importer.Default()}).Check("p", files, []*ast.File{file}, info)
	require.NoError(t, err)
	declaration, isFunction := file.Decls[0].(*ast.FuncDecl)
	require.True(t, isFunction)
	_, typed := scanBody(info, declaration.Body)
	_, untyped := scanBody(nil, declaration.Body)
	return typed, untyped
}

func TestScanBodyFindsTheShapeTheAnalysesAskAbout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bodyShape
	}{
		{name: "an empty body has none", body: ``, want: bodyShape{}},
		{name: "a function literal", body: `g := func() {}; g()`, want: bodyShape{literals: true}},
		{name: "an address taken", body: `p := &n; _ = p`, want: bodyShape{addresses: true}},
		{name: "a bitwise and is no address", body: `_ = n & 3`, want: bodyShape{}},
		{name: "a conversion of one value", body: `_ = []byte(words[0])`, want: bodyShape{conversions: true}},
		{name: "a call of one argument is no conversion", body: `_ = len(words)`, want: bodyShape{}},
		{name: "an append", body: `words = append(words, "x")`, want: bodyShape{appends: true}},
		{name: "an append inside a literal", body: `g := func() { words = append(words, "x") }; g()`,
			want: bodyShape{literals: true, appends: true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			typed, untyped := bodyShapeOf(t, testCase.body)
			require.Equal(t, testCase.want, typed)
			require.Equal(t, testCase.want.literals, untyped.literals)
			require.Equal(t, testCase.want.addresses, untyped.addresses)
			require.Equal(t, testCase.want.appends, untyped.appends)
			require.True(t, untyped.conversions || !testCase.want.conversions, "without type information a conversion is never missed")
		})
	}
	_, none := scanBody(nil, nil)
	require.Equal(t, bodyShape{}, none, "a nil body has no shape")
}

func TestScanBodyFactsFindsEachFactInOneWalk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bodyFacts
	}{
		{name: "an empty body has none", body: ``, want: bodyFacts{}},
		{name: "a defer of its own", body: `defer func() {}()`, want: bodyFacts{defers: true}},
		{name: "a defer inside a function literal is that closure's", body: `g := func() { defer func() {}() }; g()`, want: bodyFacts{}},
		{name: "a defer after a function literal still counts", body: `g := func() {}; defer g()`, want: bodyFacts{defers: true}},
		{name: "a recover in a deferred literal", body: `defer func() { _ = recover() }()`, want: bodyFacts{defers: true, recovers: true}},
		{name: "a local named recover is not the builtin", body: `recover := func() any { return nil }; _ = recover()`, want: bodyFacts{}},
		{name: "a call-stack inspector named as a value", body: `g := runtime.Caller; _ = g`, want: bodyFacts{inspectsCallStack: true}},
		{name: "a call-stack inspector inside a literal", body: `g := func() { runtime.Caller(0) }; g()`, want: bodyFacts{inspectsCallStack: true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			typed, untyped := bodyFactsOf(t, testCase.body)
			require.Equal(t, testCase.want, typed)
			require.Equal(t, bodyFacts{recovers: true, inspectsCallStack: true, defers: testCase.want.defers}, untyped,
				"without type information the two checks that need it answer true")
		})
	}
	require.Equal(t, bodyFacts{}, scanBodyFacts(nil, nil), "a nil body has no facts")
}
