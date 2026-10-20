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
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseFirstDecl(t *testing.T, source string) ast.Decl {
	t.Helper()
	wrapped := "package main\n" + source
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", wrapped, parser.ParseComments)
	require.NoError(t, err)
	require.NotEmpty(t, file.Decls, "test source has no top-level declarations")
	return file.Decls[0]
}

func TestDeclNamesIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   []SessionDecl
	}{
		{name: "function", source: `func foo() {}`, want: []SessionDecl{{Name: "foo", Kind: SessionDeclFunc}}},
		{name: "init is not a named declaration", source: `func init() {}`, want: nil},
		{name: "one variable", source: `var x int`, want: []SessionDecl{{Name: "x", Kind: SessionDeclVar}}},
		{
			name:   "variable block",
			source: "var (\n\tx int\n\ty string\n)",
			want: []SessionDecl{
				{Name: "x", Kind: SessionDeclVar},
				{Name: "y", Kind: SessionDeclVar},
			},
		},
		{name: "constant", source: `const a = 1`, want: []SessionDecl{{Name: "a", Kind: SessionDeclConst}}},
		{name: "type", source: `type Point struct { X, Y int }`, want: []SessionDecl{{Name: "Point", Kind: SessionDeclType}}},
		{name: "blank variable", source: `var _ = 42`, want: nil},
		{name: "import", source: `import "fmt"`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, declNamesIn(parseFirstDecl(t, tt.source)))
		})
	}
}

func TestDeclNamesIn_BadDeclReturnsNil(t *testing.T) {
	t.Parallel()
	var bad ast.Decl = &ast.BadDecl{}
	require.Nil(t, declNamesIn(bad))
}

func TestIsInitFuncDecl_PositiveAndNegatives(t *testing.T) {
	t.Parallel()

	init := parseFirstDecl(t, `func init() {}`)
	require.True(t, isInitFunctionDecl(init))

	notInit := parseFirstDecl(t, `func foo() {}`)
	require.False(t, isInitFunctionDecl(notInit))

	notFunc := parseFirstDecl(t, `var x int`)
	require.False(t, isInitFunctionDecl(notFunc))

	require.False(t, isInitFunctionDecl(&ast.BadDecl{}),
		"bad decl with no name must not be classified as init")
}

func TestInitDeclSignature_StableFingerprint(t *testing.T) {
	t.Parallel()
	first := parseFirstDecl(t, `func init() { _ = 1 }`)
	second := parseFirstDecl(t, `func init() { _ = 1 }`)
	require.Equal(t, initDeclSignature(first), initDeclSignature(second))
}

func TestInitDeclSignature_DifferentBodiesDifferentSig(t *testing.T) {
	t.Parallel()
	a := parseFirstDecl(t, `func init() { _ = 1 }`)
	b := parseFirstDecl(t, `func init() { _ = 2 }`)
	require.NotEqual(t, initDeclSignature(a), initDeclSignature(b))
}

func TestInitDeclSignature_NonInitReturnsEmpty(t *testing.T) {
	t.Parallel()
	decl := parseFirstDecl(t, `func foo() {}`)
	require.Empty(t, initDeclSignature(decl))
}

func TestImportSpecsIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   []sessionImport
	}{
		{name: "one import", source: `import "fmt"`, want: []sessionImport{{path: "fmt"}}},
		{name: "import block", source: "import (\n\t\"fmt\"\n\t\"strings\"\n)", want: []sessionImport{{path: "fmt"}, {path: "strings"}}},
		{
			name:   "aliases are kept",
			source: "import (\n\tf \"fmt\"\n\t. \"strings\"\n\t_ \"embed\"\n)",
			want: []sessionImport{
				{path: "fmt", alias: "f"},
				{path: "strings", alias: "."},
				{path: "embed", alias: "_"},
			},
		},
		{name: "a raw string path is unquoted", source: "import `fmt`", want: []sessionImport{{path: "fmt"}}},
		{name: "a variable is not an import", source: `var x int`, want: nil},
		{name: "a function is not an import", source: `func foo() {}`, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, importSpecsIn(parseFirstDecl(t, tt.source)))
		})
	}
}

func TestFuncDeclNames_AnonymousReturnsNil(t *testing.T) {
	t.Parallel()
	decl := &ast.FuncDecl{Name: nil}
	require.Nil(t, functionDeclNames(decl))
}

func TestValueSpecNames_VarVsConst(t *testing.T) {
	t.Parallel()
	spec := &ast.ValueSpec{
		Names: []*ast.Ident{{Name: "a"}, {Name: "b"}},
	}

	varNames := valueSpecNames(token.VAR, spec)
	require.Equal(t,
		[]SessionDecl{
			{Name: "a", Kind: SessionDeclVar},
			{Name: "b", Kind: SessionDeclVar},
		},
		varNames,
	)

	constNames := valueSpecNames(token.CONST, spec)
	require.Equal(t,
		[]SessionDecl{
			{Name: "a", Kind: SessionDeclConst},
			{Name: "b", Kind: SessionDeclConst},
		},
		constNames,
	)
}

func TestValueSpecNames_BlankSkipped(t *testing.T) {
	t.Parallel()
	spec := &ast.ValueSpec{
		Names: []*ast.Ident{{Name: "_"}, {Name: "x"}, nil, {Name: "_"}},
	}
	got := valueSpecNames(token.VAR, spec)
	require.Equal(t, []SessionDecl{{Name: "x", Kind: SessionDeclVar}}, got)
}

func TestInitDeclSignature_NonInitWithEmptyBody(t *testing.T) {
	t.Parallel()
	decl := &ast.FuncDecl{Name: &ast.Ident{Name: "init2"}}
	got := initDeclSignature(decl)
	require.Empty(t, got, "non-init name must produce empty signature")
	require.False(t, strings.HasPrefix(got, "func"))
}
