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
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/verify"
)

type typedProvider struct {
	exports  symtab.SymbolExports
	packages map[string]*types.Package
}

const registerPressurePrelude = `package main

type Node struct {
	Name     string
	Children []*Node
}

type Arena struct{}

func (a *Arena) GetNode() *Node { return &Node{} }

func (a *Arena) GetChildSlice(n int) (int, []*Node) { return n, make([]*Node, 0, n) }

`

type layoutHost struct {
	A int
	B string
	C string
	N bool
}

type layoutProvider struct {
	fields func(pkg *types.Package) []*types.Var
}

func (p typedProvider) Exports() symtab.SymbolExports { return p.exports }

func (p typedProvider) TypesPackages() map[string]*types.Package { return p.packages }

func TestBuildImportAliasMap_DefaultAlias(t *testing.T) {
	t.Parallel()

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"github.com/example/content_domain"
)

func main() { _ = content_domain.Foo }
`,
	})

	aliases := buildImportAliasMap(files, nil)
	assert.Equal(t, "github.com/example/content_domain", aliases["content_domain"])
}

func TestBuildImportAliasMap_ExplicitAlias(t *testing.T) {
	t.Parallel()

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	cd "github.com/example/content_domain"
)

func main() { _ = cd.Foo }
`,
	})

	aliases := buildImportAliasMap(files, nil)
	assert.Equal(t, "github.com/example/content_domain", aliases["cd"])
	_, hasDefault := aliases["content_domain"]
	assert.False(t, hasDefault, "default alias should not be present when explicit alias is used")
}

func TestBuildImportAliasMap_BlankAndDotImports(t *testing.T) {
	t.Parallel()

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	_ "github.com/example/sideeffect"
)
import (
	. "github.com/example/dotpkg"
)
import (
	"github.com/example/normalpkg"
)

func main() {}
`,
	})

	aliases := buildImportAliasMap(files, nil)
	_, hasBlank := aliases["_"]
	_, hasDot := aliases["."]
	assert.False(t, hasBlank, "blank imports should be excluded")
	assert.False(t, hasDot, "dot imports should be excluded")
	assert.Equal(t, "github.com/example/normalpkg", aliases["normalpkg"])
}

func TestBuildImportAliasMap_DeclaredPackageNames(t *testing.T) {
	t.Parallel()

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"gopkg.in/yaml.v3"
	"github.com/example/go-widgets"
	"github.com/example/mod/v2"
	"github.com/example/renamed"
)

func main() {}
`,
	})

	t.Run("assumed", func(t *testing.T) {
		t.Parallel()
		aliases := buildImportAliasMap(files, nil)
		assert.Equal(t, "gopkg.in/yaml.v3", aliases["yaml"])
		assert.Equal(t, "github.com/example/go-widgets", aliases["widgets"])
		assert.Equal(t, "github.com/example/mod/v2", aliases["mod"])
		assert.Equal(t, "github.com/example/renamed", aliases["renamed"])
	})

	t.Run("declared", func(t *testing.T) {
		t.Parallel()
		registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
			"github.com/example/renamed": {
				symtab.PackageNameSymbol: reflect.ValueOf("other"),
			},
		})
		aliases := buildImportAliasMap(files, registry.PackageName)
		assert.Equal(t, "github.com/example/renamed", aliases["other"])
		_, hasBase := aliases["renamed"]
		assert.False(t, hasBase, "the declared name replaces the path element")
	})
}

func TestEnrichTypeCheckError_UndefinedInVersionedPackage(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("gopkg.in/yaml.v3", map[string]reflect.Value{
		"Marshal": reflect.ValueOf(func(any) ([]byte, error) { return nil, nil }),
	})

	origErr := types.Error{Fset: token.NewFileSet(), Msg: "undefined: yaml.Unmarshal"}
	files := parseTestFiles(t, map[string]string{
		"main.go": "package main\n\nimport \"gopkg.in/yaml.v3\"\n\nfunc main() { _ = yaml.Unmarshal }\n",
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.Contains(t, enriched.Error(), `symbol "Unmarshal" is not registered`)
	assert.Contains(t, enriched.Error(), `"gopkg.in/yaml.v3"`)
}

func TestEnrichTypeCheckError_UndefinedInNativePackage(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("github.com/example/content_domain", map[string]reflect.Value{
		"SomeFunc": reflect.ValueOf(func() string { return "" }),
	})

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: content_domain.AnnotatedField",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"github.com/example/content_domain"
)

func main() { _ = content_domain.AnnotatedField{} }
`,
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.NotEqual(t, origErr.Error(), enriched.Error(), "error should be enriched")
	assert.Contains(t, enriched.Error(), `symbol "AnnotatedField" is not registered`)
	assert.Contains(t, enriched.Error(), `"github.com/example/content_domain"`)
	assert.Contains(t, enriched.Error(), `pipit extract`)
}

func TestEnrichTypeCheckError_UndefinedInInterpretedPackage(t *testing.T) {
	t.Parallel()

	service := NewService()

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: mylib.MissingFunc",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"testmod/mylib"
)

func main() { mylib.MissingFunc() }
`,
	})

	interpretedPaths := map[string]bool{
		"testmod/mylib": true,
	}

	enriched := service.enrichTypeCheckError(origErr, files, interpretedPaths)
	assert.Equal(t, origErr.Error(), enriched.Error(), "interpreted packages should not get extract hint")
}

func TestEnrichTypeCheckError_NonUndefinedMessage(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("github.com/example/pkg", map[string]reflect.Value{
		"Foo": reflect.ValueOf(func() {}),
	})

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "cannot use x (variable of type int) as string value",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"github.com/example/pkg"
)

func main() { pkg.Foo() }
`,
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(), "non-undefined errors should not be enriched")
}

func TestEnrichTypeCheckError_UndefinedLocalVariable(t *testing.T) {
	t.Parallel()

	service := NewService()

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: myVariable",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

func main() {}
`,
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(), "local variable undefined errors should not be enriched")
}

func TestEnrichTypeCheckError_PackageNotInRegistry(t *testing.T) {
	t.Parallel()

	service := NewService()

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: unknownpkg.Foo",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"github.com/example/unknownpkg"
)

func main() { unknownpkg.Foo() }
`,
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(), "unregistered packages should not get extract hint")
}

func TestEnrichTypeCheckError_NilSymbols(t *testing.T) {
	t.Parallel()

	service := &Service{}

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: content_domain.AnnotatedField",
	}

	enriched := service.enrichTypeCheckError(origErr, nil, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(), "nil symbols should not panic")
}

func TestEnrichTypeCheckError_NotTypesError(t *testing.T) {
	t.Parallel()

	service := NewService()

	origErr := errors.New("some random error")

	enriched := service.enrichTypeCheckError(origErr, nil, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(), "non-types.Error should not be enriched")
}

func TestEnrichTypeCheckError_SymbolAlreadyRegistered(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("github.com/example/content_domain", map[string]reflect.Value{
		"AnnotatedField": reflect.ValueOf((*struct{})(nil)),
	})

	origErr := types.Error{
		Fset: token.NewFileSet(),
		Msg:  "undefined: content_domain.AnnotatedField",
	}

	files := parseTestFiles(t, map[string]string{
		"main.go": `package main

import (
	"github.com/example/content_domain"
)

func main() { _ = content_domain.AnnotatedField{} }
`,
	})

	enriched := service.enrichTypeCheckError(origErr, files, nil)
	assert.Equal(t, origErr.Error(), enriched.Error(),
		"should not add extract hint when the symbol IS registered")
}

func TestCompileProgram_StaleSymbolRegistryHint(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("custom/content_domain", map[string]reflect.Value{
		"SomeFunc": reflect.ValueOf(func() string { return "hello" }),
	})

	sources := map[string]map[string]string{
		"": {
			"main.go": `package main

import (
	"custom/content_domain"
)

func entrypoint() bool {
	return content_domain.MissingType{} != content_domain.MissingType{}
}

func main() {}
`,
		},
	}

	_, err := service.CompileProgram(t.Context(), "testmod", sources)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	assert.Contains(t, err.Error(), `pipit extract`,
		"error should contain extract hint for missing symbol in registered package")
	assert.Contains(t, err.Error(), `"MissingType" is not registered`)
}

func TestCompileProgram_InterpretedPackageMissingSymbol_NoHint(t *testing.T) {
	t.Parallel()

	sources := map[string]map[string]string{
		"": {
			"main.go": `package main

import (
	"testmod/mylib"
)

func entrypoint() int {
	return mylib.Nonexistent()
}

func main() {}
`,
		},
		"mylib": {
			"mylib.go": `package mylib

func Add(a, b int) int { return a + b }
`,
		},
	}

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "testmod", sources)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	assert.NotContains(t, err.Error(), `pipit extract`,
		"interpreted packages should not trigger extract hint")
}

func TestCompileFileSet_StaleSymbolRegistryHint(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("custom/mypkg", map[string]reflect.Value{
		"Hello": reflect.ValueOf(func() string { return "world" }),
	})

	sources := map[string]string{
		"main.go": `package main

import (
	"custom/mypkg"
)

func run() string {
	return mypkg.Missing()
}

func main() {}
`,
	}

	_, err := service.CompileFileSet(t.Context(), sources)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	assert.Contains(t, err.Error(), `pipit extract`,
		"CompileFileSet should also enrich type check errors")
	assert.Contains(t, err.Error(), `"Missing" is not registered`)
}

func parseTestFiles(t *testing.T, sources map[string]string) []*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for name, source := range sources {
		file, err := parser.ParseFile(fset, name, source, parser.ParseComments)
		require.NoError(t, err, "parsing %s", name)
		files = append(files, file)
	}
	return files
}

func TestEvalDivisionByZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		code    string
	}{
		{name: "integer division by zero literal", code: `1 / 0`, wantErr: fault.ErrTypeCheck},
		{name: "integer remainder by zero literal", code: `1 % 0`, wantErr: fault.ErrTypeCheck},
		{name: "integer division by zero variable", code: "x := 0\n10 / x", wantErr: fault.ErrDivisionByZero},
		{name: "integer remainder by zero variable", code: "x := 0\n10 % x", wantErr: fault.ErrDivisionByZero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			_, err := service.Eval(t.Context(), tt.code)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEvalCompilationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		code    string
	}{
		{name: "syntax error", code: `func {{{`, wantErr: fault.ErrParse},
		{name: "undefined variables", code: `x + y`, wantErr: fault.ErrTypeCheck},
		{name: "type mismatch assignment", code: "var x int\nx = \"hello\"", wantErr: fault.ErrTypeCheck},
		{name: "mismatched operand types", code: `1 + "hello"`, wantErr: fault.ErrTypeCheck},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			_, err := service.Eval(t.Context(), tt.code)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEvalUnregisteredPackageImport(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSymbols(t, symtab.SymbolExports{})

	_, err := service.Eval(t.Context(), `import "example.com/missing/pkg"
_ = 1`)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.ErrorIs(t, err, fault.ErrPackageNotInRegistry)
	require.Contains(t, err.Error(), "pipit extract discover")
	require.Contains(t, err.Error(), "example.com/missing/pkg")
}

func TestExtractMissingPackagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "standard go/types shape",
			message: `could not import example.com/pkg (package "example.com/pkg" not registered with interpreter)`,
			want:    "example.com/pkg",
		},
		{
			name:    "missing prefix returns empty",
			message: `something unexpected about example.com/pkg`,
			want:    "",
		},
		{
			name:    "empty after prefix returns empty",
			message: "could not import ",
			want:    "",
		},
		{
			name:    "no space after path returns empty",
			message: "could not import example.com/only",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, extractMissingPackagePath(tt.message))
		})
	}
}

func TestEvalIndexOutOfBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
	}{
		{name: "slice index out of bounds", code: "s := []int{1, 2, 3}\ns[5]"},
		{name: "string index out of bounds", code: "s := \"hello\"\ns[10]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			_, err := service.Eval(t.Context(), tt.code)
			require.Error(t, err)
		})
	}
}

func TestEvalTypeAssertionFailure(t *testing.T) {
	t.Parallel()

	t.Run("failing type assertion produces error", func(t *testing.T) {
		t.Parallel()
		service := NewService()
		_, err := service.Eval(t.Context(), "var x interface{} = 42\nv := x.(string)\nv")
		require.Error(t, err)
	})
}

func parseImportBoundaryFile(t *testing.T, source string) *ast.File {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "boundary_test.go", source, 0)
	require.NoError(t, err)
	return file
}

func TestCheckFileImportsCanonicalisesImportPath(t *testing.T) {
	t.Parallel()
	service := NewService(WithDeniedImports("unsafe"))

	tests := []struct {
		name   string
		source string
	}{
		{name: "double quoted", source: "package main\nimport _ \"unsafe\"\n"},
		{name: "raw string", source: "package main\nimport _ " + "`unsafe`" + "\n"},
		{name: "escaped", source: "package main\nimport _ \"\\x75nsafe\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := service.checkFileImports(nil, parseImportBoundaryFile(t, tt.source))
			require.ErrorIs(t, err, fault.ErrParse)
			require.ErrorContains(t, err, "not permitted")
		})
	}
}

func TestCheckFileImportsDenylistOverridesLocalPackage(t *testing.T) {
	t.Parallel()
	service := NewService(WithImportAllowlist("host/widgets"), WithDeniedImports("unsafe"))
	parsed := map[string]*parsedPackage{"unsafe": {importPath: "unsafe"}}

	err := service.checkFileImports(parsed, parseImportBoundaryFile(t, "package main\nimport _ \"unsafe\"\n"))
	require.ErrorIs(t, err, fault.ErrParse)
	require.ErrorContains(t, err, "not permitted")
}

func TestCheckFileImportsAllowlist(t *testing.T) {
	t.Parallel()

	t.Run("permits allowlisted and local imports", func(t *testing.T) {
		t.Parallel()
		service := NewService(WithImportAllowlist("host/widgets"))
		parsed := map[string]*parsedPackage{"testmod/lib": {importPath: "testmod/lib"}}
		require.NoError(t, service.checkFileImports(parsed, parseImportBoundaryFile(t, "package main\nimport _ \"host/widgets\"\n")))
		require.NoError(t, service.checkFileImports(parsed, parseImportBoundaryFile(t, "package main\nimport _ \"testmod/lib\"\n")))
	})

	t.Run("denies non-allowlisted external import", func(t *testing.T) {
		t.Parallel()
		service := NewService(WithImportAllowlist("host/widgets"))
		err := service.checkFileImports(nil, parseImportBoundaryFile(t, "package main\nimport _ \"net/http\"\n"))
		require.ErrorContains(t, err, "not permitted")
	})

	t.Run("empty allowlist denies every external import", func(t *testing.T) {
		t.Parallel()
		service := NewService(WithImportAllowlist())
		err := service.checkFileImports(nil, parseImportBoundaryFile(t, "package main\nimport _ \"fmt\"\n"))
		require.ErrorContains(t, err, "not permitted")
	})
}

func TestImportBoundaryDeniedAcrossEntrypoints(t *testing.T) {
	t.Parallel()
	const deniedImport = "os"
	mixedCode := "import \"" + deniedImport + "\"\n_ = " + deniedImport + ".Getpid()"
	fileSource := "package main\nimport _ \"" + deniedImport + "\"\n"
	programSources := map[string]map[string]string{
		"": {"main.go": "package main\nimport _ \"" + deniedImport + "\"\n\nfunc main() {}\n"},
	}

	tests := []struct {
		name string
		run  func(ctx context.Context, service *Service) error
	}{
		{name: "Eval", run: func(ctx context.Context, service *Service) error {
			_, err := service.Eval(ctx, mixedCode)
			return err
		}},
		{name: "Compile", run: func(ctx context.Context, service *Service) error {
			_, err := service.Compile(ctx, mixedCode)
			return err
		}},
		{name: "CompileFileSet", run: func(ctx context.Context, service *Service) error {
			_, err := service.CompileFileSet(ctx, map[string]string{"main.go": fileSource})
			return err
		}},
		{name: "CompileProgram", run: func(ctx context.Context, service *Service) error {
			_, err := service.CompileProgram(ctx, "testmod", programSources)
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(WithImportAllowlist("testmod/lib"))
			err := tt.run(t.Context(), service)
			require.Error(t, err)
			require.ErrorContains(t, err, "not permitted")
		})
	}
}

func TestCompileProgramReportsTheProblemsOfEveryPackage(t *testing.T) {
	t.Parallel()

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"a": {"a.go": "package a\n\nfunc A() int { return undefinedA }\n"},
		"b": {"b.go": "package b\n\nfunc B() string { return 1 }\n\nfunc C() int { return undefinedC }\n"},
		"c": {"c.go": "package c\n\n//go:embed c.txt\nvar content string\n"},
		"d": {"d.go": "package d\n\nfunc D() int { return 4 }\n"},
		"":  {"main.go": "package main\n\nimport (\n\t\"example.com/m/a\"\n\t\"example.com/m/d\"\n)\n\nfunc main() { _ = a.A() + d.D() }\n"},
	})
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.ErrorIs(t, err, fault.ErrCompileEmbedUnsupported)

	var list *fault.ProblemList
	require.ErrorAs(t, err, &list)
	problems := fault.Problems(err)
	require.Len(t, problems, 3, "one problem per failed package: %v", err)
	require.Contains(t, problems[0].Error(), "c.go:3:1", "parse and embed problems come first")
	require.Contains(t, problems[1].Error(), "undefinedA")
	require.Contains(t, problems[2].Error(), "undefinedC")
	require.Contains(t, problems[2].Error(), "cannot use 1", "every type error of a package is kept")
	require.NotContains(t, err.Error(), "could not import", "a package importing a failed one is skipped")
}

func TestCompileProgramKeepsTheShapeOfASingleProblem(t *testing.T) {
	t.Parallel()

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"": {"main.go": "package main\n\nfunc main() { _ = undefinedName }\n"},
	})
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	var list *fault.ProblemList
	require.NotErrorAs(t, err, &list, "one problem is not wrapped in a list")
	require.Len(t, fault.Problems(err), 1)
}

func TestCompileFileSetReportsEveryTypeErrorUpToTheCap(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("package main\n\nfunc main() {\n")
	for index := range maxTypeErrorsPerPackage + 3 {
		fmt.Fprintf(&source, "\t_ = missing%d\n", index)
	}
	source.WriteString("}\n")

	service := NewService()
	_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source.String()})
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	message := err.Error()
	for index := range maxTypeErrorsPerPackage {
		require.Contains(t, message, fmt.Sprintf("missing%d", index))
	}
	require.NotContains(t, message, fmt.Sprintf("missing%d", maxTypeErrorsPerPackage))
	require.Contains(t, message, errTooManyTypeErrors.Error())
}

func TestEveryProgramProblemNamesItsPackage(t *testing.T) {
	t.Parallel()

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"a": {"a.go": "package a\n\nfunc A() int { return }\n"},
		"b": {"b.go": "//go:build linux &&\n\npackage b\n"},
		"c": {"c.go": "package c\n\n//go:embed c.txt\nvar content string\n"},
		"d": {"d.go": "package d\n\nfunc D() int { return missing }\n"},
		"e": {"e.go": "package e\n\nvar E = func() int { return E() }\n"},
	})
	problems := fault.Problems(err)
	require.Len(t, problems, 5, "%v", err)
	named := make(map[string]bool)
	for _, problem := range problems {
		message := problem.Error()
		require.NotContains(t, message, "parsing packages:")
		for _, suffix := range []string{"a", "b", "c", "d", "e"} {
			packagePath := "example.com/m/" + suffix
			if strings.Contains(message, packagePath+":") {
				require.False(t, named[packagePath], "%s has two problems", packagePath)
				named[packagePath] = true
				require.Equal(t, 1, strings.Count(message, packagePath), "the package is named once: %v", problem)
			}
		}
	}
	require.Len(t, named, 5, "every problem names its package: %v", err)
}

func TestCompileProgramJoinsEveryParseErrorOfAPackage(t *testing.T) {
	t.Parallel()

	service := NewService()
	_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"a": {
			"one.go":   "package a\n\nfunc One() int { return }}\n",
			"two.go":   "package a\n\nfunc Two( {\n",
			"three.go": "//go:build (\n\npackage a\n",
		},
	})
	require.ErrorIs(t, err, fault.ErrParse)
	problems := fault.Problems(err)
	require.Len(t, problems, 1, "the parse errors of one package are one problem")
	for _, file := range []string{"one.go", "two.go", "three.go"} {
		require.Contains(t, problems[0].Error(), file)
	}
	require.True(t, strings.HasPrefix(err.Error(), "compiling program: parse failed: example.com/m/a: "), "%v", err)
}

func TestCompileProgramKeepsParseProblemsBesideLaterFailures(t *testing.T) {
	t.Parallel()

	t.Run("an import the policy refuses", func(t *testing.T) {
		t.Parallel()

		service := NewService(WithDeniedImports("os", "os/exec"))
		_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
			"a": {"a.go": "package a\n\nfunc A( {\n"},
			"b": {
				"b.go": "package b\n\nimport (\n\t\"os\"\n\t\"os/exec\"\n)\n\nvar _, _ = os.Args, exec.Command\n",
				"c.go": "package b\n\nimport \"os\"\n\nvar _ = os.Args\n",
			},
		})
		require.ErrorIs(t, err, fault.ErrParse)
		problems := fault.Problems(err)
		require.Len(t, problems, 2, "%v", err)
		require.Contains(t, problems[0].Error(), "example.com/m/a")
		require.Contains(t, problems[1].Error(), `example.com/m/b: imports "os", "os/exec" are not permitted`)
	})

	t.Run("an import cycle", func(t *testing.T) {
		t.Parallel()

		service := NewService()
		_, err := service.CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
			"a": {"a.go": "package a\n\nfunc A( {\n"},
			"b": {"b.go": "package b\n\nimport \"example.com/m/c\"\n\nvar B = c.C\n"},
			"c": {"c.go": "package c\n\nimport \"example.com/m/b\"\n\nvar C = b.B\n"},
		})
		require.ErrorIs(t, err, fault.ErrParse)
		require.ErrorIs(t, err, fault.ErrCyclicImport)
		require.Len(t, fault.Problems(err), 2, "%v", err)
	})
}

func TestCompileFileSetResolvesDeclaredPackageNames(t *testing.T) {
	t.Parallel()

	const source = `package main

import (
	"gopkg.in/yaml.v3"
	"github.com/example/go-widgets"
	"github.com/example/mod/v2"
	"k8s.io/api/core/v1"
	lib "example.com/go-"
)

func entry() int {
	return yaml.Double(20) + widgetkit.Count() + mod.One() + v1.Version() + lib.Seven()
}

func main() {}
`
	service := NewService(WithSymbolProvider(typedProvider{
		exports: symtab.SymbolExports{
			"gopkg.in/yaml.v3": {"Double": reflect.ValueOf(func(n int) int { return n * 2 })},
			"github.com/example/go-widgets": {
				symtab.PackageNameSymbol: reflect.ValueOf("widgetkit"),
				"Count":                  reflect.ValueOf(func() int { return 3 }),
			},
			"github.com/example/mod/v2": {"One": reflect.ValueOf(func() int { return 1 })},
			"k8s.io/api/core/v1":        {"Version": reflect.ValueOf(func() int { return 10 })},
			"example.com/go-":           {"Seven": reflect.ValueOf(func() int { return 7 })},
		},
		packages: map[string]*types.Package{
			"k8s.io/api/core/v1": intFuncPackage("k8s.io/api/core/v1", "v1", "Version"),
		},
	}))

	ctx := t.Context()
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(ctx, compiled, "entry")
	require.NoError(t, err)
	require.EqualValues(t, 61, result)
}

func TestUndefinedPackageNameExplainsTheAssumedName(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("k8s.io/api/apps/v1", map[string]reflect.Value{
		"Kind": reflect.ValueOf(func() string { return "Deployment" }),
	})
	const source = "package main\n\nimport \"k8s.io/api/apps/v1\"\n\nfunc entry() string { return v1.Kind() }\n\nfunc main() {}\n"
	_, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.ErrorContains(t, err, "undefined: v1")
	require.ErrorContains(t, err, `package "k8s.io/api/apps/v1" is known as "apps", not "v1"`)
	require.ErrorContains(t, err, `import v1 "k8s.io/api/apps/v1"`)

	const aliased = "package main\n\nimport v1 \"k8s.io/api/apps/v1\"\n\nfunc entry() string { return v1.Kind() }\n\nfunc main() {}\n"
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": aliased})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "entry")
	require.NoError(t, err)
	require.Equal(t, "Deployment", result)
}

func TestCompileProgramResolvesLateRegisteredPackageName(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.RegisterPackage("gopkg.in/yaml.v3", map[string]reflect.Value{
		"Double": reflect.ValueOf(func(n int) int { return n * 2 }),
	})

	sources := map[string]map[string]string{
		"": {"main.go": "package main\n\nimport \"gopkg.in/yaml.v3\"\n\nfunc entry() int { return yaml.Double(21) }\n\nfunc main() {}\n"},
	}
	ctx := t.Context()
	compiled, err := service.CompileProgram(ctx, "testmod", sources)
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(ctx, compiled, "entry")
	require.NoError(t, err)
	require.EqualValues(t, 42, result)
}

func TestDeclaringStatementsReleaseTheirTemporaries(t *testing.T) {
	t.Parallel()

	const nodes = 450
	var source strings.Builder
	source.WriteString(registerPressurePrelude)
	source.WriteString("func BuildAST() int {\n\tarena := &Arena{}\n\troot := arena.GetNode()\n")
	for index := range nodes {
		fmt.Fprintf(&source, "\tn%d := arena.GetNode()\n\tn%[1]d.Name = \"div\"\n", index)
		fmt.Fprintf(&source, "\t_, n%dSlice := arena.GetChildSlice(2)\n\tn%[1]d.Children = n%[1]dSlice\n", index)
		fmt.Fprintf(&source, "\tcount%d, _ := arena.GetChildSlice(%[1]d)\n\t_ = count%[1]d\n", index)
		fmt.Fprintf(&source, "\troot.Children = append(root.Children, n%d)\n", index)
	}
	source.WriteString("\treturn len(root.Children)\n}\n\nfunc main() {}\n")

	result, err := NewService().EvalFile(t.Context(), source.String(), "BuildAST")
	require.NoError(t, err)
	require.EqualValues(t, nodes, result)
}

func TestWideCallsAfterManyLocalsRecompileWithMoreSpilling(t *testing.T) {
	t.Parallel()

	shapes := map[string]struct {
		open  string
		close string
	}{
		"function":               {open: "func run() int {\n", close: "\treturn t\n}\n"},
		"closure":                {open: "func run() int {\n\tf := func() int {\n", close: "\treturn t\n\t}\n\treturn f()\n}\n"},
		"range-over-func body":   {open: "func seq(yield func(int) bool) { yield(1) }\n\nfunc run() int {\n\tfor x := range seq {\n\t_ = x\n", close: "\treturn t\n\t}\n\treturn -1\n}\n"},
		"generic specialisation": {open: "func run() int { return work[int](0) }\n\nfunc work[T any](_ T) int {\n", close: "\treturn t\n}\n"},
	}
	for name, shape := range shapes {
		for _, arity := range []int{12, 20, 40} {
			for _, debugInfo := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d arguments/debug %t", name, arity, debugInfo), func(t *testing.T) {
					t.Parallel()

					parameters := make([]string, arity)
					for position := range parameters {
						parameters[position] = fmt.Sprintf("a%d", position)
					}
					var source strings.Builder
					fmt.Fprintf(&source, "package main\n\nfunc g(n int) int { return n }\n\nfunc sum(%s int) int { return %s }\n\n",
						strings.Join(parameters, ", "), strings.Join(parameters, " + "))
					source.WriteString(shape.open)
					expected := wideCallBody(&source, arity)
					source.WriteString(shape.close)
					source.WriteString("\nfunc main() {}\n")

					var options []Option
					if debugInfo {
						options = append(options, WithDebugInfo())
					}
					result, err := NewService(options...).EvalFile(t.Context(), source.String(), "run")
					require.NoError(t, err)
					require.EqualValues(t, expected, result)
				})
			}
		}
	}
}

func TestRegisterOverflowNamesEveryFunctionAndStatement(t *testing.T) {
	t.Parallel()

	call := "count(" + manyConstants(300) + ")"
	source := "package main\n\nvar count = func(values ...int) int { return len(values) }\n\n" +
		"func first() int {\n\treturn " + call + "\n}\n\n" +
		"func second() int {\n\ttotal := 1\n\ttotal += " + call + "\n\treturn total\n}\n\nfunc main() {}\n"

	_, err := NewService().CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"": {"main.go": source},
	})
	require.ErrorIs(t, err, fault.ErrRegisterOverflow)
	message := err.Error()
	require.Contains(t, message, `function "first"`)
	require.Contains(t, message, `function "second"`)
	require.Contains(t, message, "main.go:6:2")
	require.Contains(t, message, "main.go:11:2")
}

func TestRegisterOverflowReportCapsTheFunctionsListed(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("package main\n\nvar count = func(values ...int) int { return len(values) }\n\n")
	for index := range 12 {
		fmt.Fprintf(&source, "func f%02d() int {\n\treturn count(%s)\n}\n\n", index, manyConstants(300))
	}
	source.WriteString("func main() {}\n")

	_, err := NewService().CompileProgram(t.Context(), "example.com/m", map[string]map[string]string{
		"": {"main.go": source.String()},
	})
	require.ErrorIs(t, err, fault.ErrRegisterOverflow)
	message := err.Error()
	require.Equal(t, 10, strings.Count(message, `in function "f`))
	require.Contains(t, message, "register overflow in 2 more functions (not listed)")
}

func TestVariadicCallOfManyConstantsCompiles(t *testing.T) {
	t.Parallel()

	source := "package main\n\nfunc count(values ...int) int { return len(values) }\n\n" +
		"func run() int {\n\treturn count(" + manyConstants(300) + ")\n}\n\nfunc main() {}\n"
	result, err := NewService().EvalFile(t.Context(), source, "run")
	require.NoError(t, err)
	require.EqualValues(t, 300, result)
}

func TestRegisterRetryWithClosuresAndASpecialisationRunsCorrectly(t *testing.T) {
	t.Parallel()

	const locals, arity = 300, 20
	var source strings.Builder
	source.WriteString("package main\n\nfunc g(n int) int { return n }\n\n")
	source.WriteString("func id[T any](v T) T {\n\tf := func() T { return v }\n\treturn f()\n}\n\n")
	parameters := make([]string, arity)
	for position := range parameters {
		parameters[position] = fmt.Sprintf("a%d", position)
	}
	fmt.Fprintf(&source, "func sum(%s int) int { return %s }\n\n", strings.Join(parameters, ", "), strings.Join(parameters, " + "))
	source.WriteString("func run() int {\n\tfirst := func() int { return 1 }\n\tt := id(2) + first()\n")
	expected := 3
	for index := range locals {
		fmt.Fprintf(&source, "\tv%d := g(%d)\n", index, index)
		expected += index
	}
	arguments := make([]string, arity)
	for position := range arguments {
		arguments[position] = fmt.Sprintf("v%d", locals-arity+position)
		expected += locals - arity + position
	}
	fmt.Fprintf(&source, "\tt += sum(%s)\n\tsecond := func() int { return 3 }\n\tt += second()\n", strings.Join(arguments, ", "))
	expected += 3
	for index := range locals {
		fmt.Fprintf(&source, "\tt += v%d\n", index)
	}
	source.WriteString("\treturn t\n}\n\nfunc main() {}\n")

	result, err := NewService().EvalFile(t.Context(), source.String(), "run")
	require.NoError(t, err)
	require.EqualValues(t, expected, result)
}

func TestRegisterRetryCompactsNestedCallArguments(t *testing.T) {
	t.Parallel()

	const arity = 60
	parameters := make([]string, arity)
	arguments := make([]string, arity)
	expected := 0
	for position := range parameters {
		parameters[position] = fmt.Sprintf("a%d", position)
		arguments[position] = fmt.Sprintf("g3(h(x+%d), h(x+1), h(x+2))", position)
		expected += (1 + position) + 2 + 3
	}
	source := fmt.Sprintf("package main\n\nfunc h(n int) int { return n }\n\nfunc g3(a, b, c int) int { return a + b + c }\n\n"+
		"func sum(%s int) int { return %s }\n\nfunc run() int {\n\tx := 1\n\treturn sum(%s)\n}\n\nfunc main() {}\n",
		strings.Join(parameters, ", "), strings.Join(parameters, " + "), strings.Join(arguments, ", "))

	result, err := NewService().EvalFile(t.Context(), source, "run")
	require.NoError(t, err)
	require.EqualValues(t, expected, result)
}

func TestParseAllPackagesStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewService().parseAllPackages(ctx, "example.com/m", map[string]map[string]string{
		"":  {"main.go": "package main\n"},
		"a": {"a.go": "package a\n"},
	})
	require.ErrorIs(t, err, fault.ErrExecutionCancelled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestStopsCompilationAfterAPanic(t *testing.T) {
	t.Parallel()

	require.True(t, stopsCompilation(t.Context(), fmt.Errorf("%w: boom", fault.ErrCompilePanic)))
	require.False(t, stopsCompilation(t.Context(), fault.ErrTypeCheck))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.True(t, stopsCompilation(ctx, fault.ErrTypeCheck))
}

func intFuncPackage(importPath, name, function string) *types.Package {
	pkg := types.NewPackage(importPath, name)
	result := types.NewTuple(types.NewVar(0, pkg, "", types.Typ[types.Int]))
	pkg.Scope().Insert(types.NewFunc(0, pkg, function, types.NewSignatureType(nil, nil, nil, nil, result, false)))
	pkg.MarkComplete()
	return pkg
}

func wideCallBody(source *strings.Builder, arity int) int {
	const locals = 300
	expected := 0
	for index := range locals {
		fmt.Fprintf(source, "\tv%d := g(%d)\n", index, index)
		expected += index
	}
	arguments := make([]string, arity)
	for position := range arguments {
		arguments[position] = fmt.Sprintf("v%d", locals-arity+position)
		expected += locals - arity + position
	}
	fmt.Fprintf(source, "\tt := sum(%s)\n", strings.Join(arguments, ", "))
	for index := range locals {
		fmt.Fprintf(source, "\tt += v%d\n", index)
	}
	return expected
}

func manyConstants(n int) string {
	arguments := make([]string, n)
	for index := range arguments {
		arguments[index] = fmt.Sprintf("%d", index)
	}
	return strings.Join(arguments, ", ")
}

func (layoutProvider) Exports() symtab.SymbolExports {
	return symtab.SymbolExports{"layouthost": {
		"T":    reflect.ValueOf((*layoutHost)(nil)),
		"Make": reflect.ValueOf(func() *layoutHost { return &layoutHost{} }),
	}}
}

func (p layoutProvider) TypesPackages() map[string]*types.Package {
	pkg := types.NewPackage("layouthost", "layouthost")
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "T", nil), types.NewStruct(p.fields(pkg), nil), nil)
	pkg.Scope().Insert(named.Obj())
	result := types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.NewPointer(named)))
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Make", types.NewSignatureType(nil, nil, nil, nil, result, false)))
	pkg.MarkComplete()
	return map[string]*types.Package{"layouthost": pkg}
}

func layoutField(pkg *types.Package, name string, kind types.BasicKind) *types.Var {
	return types.NewField(token.NoPos, pkg, name, types.Typ[kind], false)
}

func staleLayoutProvider() layoutProvider {
	return layoutProvider{fields: func(pkg *types.Package) []*types.Var {
		return []*types.Var{layoutField(pkg, "A", types.Int), layoutField(pkg, "N", types.String),
			layoutField(pkg, "B", types.String), layoutField(pkg, "C", types.String)}
	}}
}

func TestBridgePackageExportsReturnsARejectedTypesPackage(t *testing.T) {
	service := NewService()
	stale := staleLayoutProvider()
	result := &packageCompileResult{
		functionTable:       map[string]uint16{},
		globalVariables:     nil,
		info:                &types.Info{Defs: map[*ast.Ident]types.Object{}},
		typesPackage:        stale.TypesPackages()["layouthost"],
		rootFunction:        program.NewNamedFunction("<root>"),
		initFunctionIndices: nil,
		exportedVars:        []packageVarExport{{reflectType: reflect.TypeFor[*layoutHost](), storage: reflect.ValueOf((*layoutHost)(nil)), name: "T"}},
	}
	err := service.bridgePackageExports("layouthost", result)
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout)
	require.ErrorIs(t, err, fault.ErrCompilation)
	require.ErrorContains(t, err, `"layouthost"`)
	require.Empty(t, service.pendingVarBridges, "a rejected package bridges no variables")
}

func TestStaleTypesPackageLayoutIsRejected(t *testing.T) {
	const source = `package main

import "layouthost"

func Run() *layouthost.T {
	v := layouthost.Make()
	v.B = "to-B"
	return v
}
`
	stale := staleLayoutProvider()
	registry := symtab.NewSymbolRegistry(stale.Exports())
	err := registry.RegisterTypesPackage("layouthost", stale.TypesPackages()["layouthost"])
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout)
	require.ErrorContains(t, err, "layouthost.T: field 1 is N in the export data, B natively")
	_, err = registry.Import(t.Context(), "layouthost")
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout, "a rejected package is not synthesised from reflect instead")

	service := NewService()
	require.ErrorIs(t, service.UseSymbolProviders(stale), symtab.ErrTypesPackageLayout)
	_, err = service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.Error(t, err)
	require.ErrorContains(t, err, "types package layout disagrees with the native type")
	require.ErrorContains(t, err, "make generate-selfhost-symbols")

	offset := layoutProvider{fields: func(pkg *types.Package) []*types.Var {
		return []*types.Var{layoutField(pkg, "A", types.Int32), layoutField(pkg, "B", types.String),
			layoutField(pkg, "C", types.String), layoutField(pkg, "N", types.Bool)}
	}}
	err = symtab.NewSymbolRegistry(offset.Exports()).RegisterTypesPackage("layouthost", offset.TypesPackages()["layouthost"])
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout)
	require.ErrorContains(t, err, "field 0 (A): kind int32 in the export data, int natively")

	current := layoutProvider{fields: func(pkg *types.Package) []*types.Var {
		return []*types.Var{layoutField(pkg, "A", types.Int), layoutField(pkg, "B", types.String),
			layoutField(pkg, "C", types.String), layoutField(pkg, "N", types.Bool)}
	}}
	service = NewService()
	require.NoError(t, service.UseSymbolProviders(current))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "Run")
	require.NoError(t, err)
	host, ok := result.(*layoutHost)
	require.True(t, ok)
	require.Equal(t, layoutHost{B: "to-B"}, *host)
}

type compileCorpusProgram struct {
	name   string
	source string
	want   int64
}

var compileCorpus = []compileCorpusProgram{
	{name: "inlined leaves of every kind", want: 117, source: `package main

func addInt(a, b int) int { return a + b }
func addFloat(a, b float64) float64 { return a + b }
func joinText(a, b string) string { return a + b }
func both(a, b bool) bool { return a && b }
func addUint(a, b uint) uint { return a + b }
func addComplex(a, b complex128) complex128 { return a + b }
func first(values []int) int { return values[0] }
func twice(f func(int) int, x int) int { return f(f(x)) }

func run() int {
	total := 0
	for index := range 10 {
		total = addInt(total, index)
	}
	length := len(joinText("ab", "cd"))
	if both(true, length == 4) {
		total += int(addFloat(1.5, 2.5))
	}
	total += int(addUint(3, 4)) + int(real(addComplex(1+2i, 3+4i)))
	total += first([]int{9, 8}) + twice(func(x int) int { return x * 2 }, 12)
	return total
}
`},
	{name: "locals escaping into closures, the heap and results", want: 61, source: `package main

type box struct{ value int }

func newBox(value int) *box {
	local := box{value: value}
	return &local
}

func counter() func() int {
	count := 0
	return func() int {
		count++
		return count
	}
}

func run() int {
	next := counter()
	next()
	next()
	values := make([]int, 0, 4)
	for index := range 4 {
		values = append(values, index*index)
	}
	pointer := &values[3]
	*pointer += 1
	sum := 0
	for _, value := range values {
		sum += value
	}
	captured := 0
	func() { captured = sum }()
	return next() + newBox(40).value + captured + len(values) - 1
}
`},
	{name: "loops the optimisers rewrite", want: 5704, source: `package main

type point struct{ x, y int32 }

var table = []int{1, 2, 3, 4, 5, 6, 7, 8}

func run() int {
	points := []point{{1, 2}, {3, 4}, {5, 6}}
	scale := point{2, 3}
	var narrow int32
	for index := range 100 {
		for _, current := range points {
			narrow += current.x*scale.x + current.y*scale.y + int32(index&1)
		}
	}
	total := 0
	for index := 0; index < len(table); index++ {
		total += table[index] * table[index]
	}
	for _, character := range "héllo" {
		total += len(string(character))
	}
	var small int8 = 100
	small += 100
	return int(narrow) + total + int(small)
}
`},
	{name: "appends, maps and self-recursion", want: 96, source: `package main

func fibonacci(n int) int {
	if n < 2 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

func run() int {
	words := map[string]int{}
	for _, word := range []string{"a", "b", "a", "c", "a"} {
		words[word]++
	}
	buffer := []byte{}
	for index := range 5 {
		buffer = append(buffer, byte('a'+index))
	}
	buffer = buffer[:len(buffer)-1]
	total := fibonacci(10) + words["a"] + len(buffer) + len(words)
	if total > 60 && total != 61 {
		total++
	}
	switch {
	case total < 10:
		total = 0
	case total >= 60:
		total += 30
	}
	return total
}
`},

	{name: "inlined methods over typed slices", want: 148, source: `package main

type vector struct{ x, y float64 }

func (v vector) dot(other vector) float64 { return v.x*other.x + v.y*other.y }

func (v *vector) scale(factor float64) { v.x *= factor; v.y *= factor }

type counter struct{ hits []int }

func (c *counter) note(index int) { c.hits[index]++ }

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func maxFloat(values []float64) float64 {
	best := values[0]
	for _, value := range values[1:] {
		if value > best {
			best = value
		}
	}
	return best
}

func flags(values []bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func label(values []string) string { return values[len(values)-1] }

func run() int {
	first := vector{1, 2}
	second := vector{3, 4}
	total := 0.0
	for range 10 {
		total += first.dot(second)
	}
	first.scale(2)
	tally := &counter{hits: make([]int, 3)}
	for index := range 9 {
		tally.note(index % 3)
	}
	ints := []int{4, 5, 6}
	floats := []float64{1.5, 7.25, 3}
	bools := []bool{true, false, true}
	words := []string{"a", "bb", "ccc"}
	return int(total) + int(first.x) + sumInts(tally.hits) + sumInts(ints) + int(maxFloat(floats)) + flags(bools) + len(label(words))
}
`},
	{name: "type switches, channels, copies and generic specialisations", want: 25, source: `package main

func pick[T int | float64](values []T, index int) T { return values[index] }

func describe(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case string:
		return len(typed)
	case []int:
		return len(typed)
	default:
		return -1
	}
}

func run() int {
	var buffer = make([]int, 4)
	var copied = make([]int, 2)
	copiedCount := copy(copied, buffer)
	results := make(chan int, 2)
	done := make(chan bool)
	go func() {
		for index := range 3 {
			select {
			case results <- index + 1:
			default:
			}
		}
		done <- true
	}()
	<-done
	total := 0
	for len(results) > 0 {
		total += <-results
	}
	ints := []int{1, 2, 3}
	floats := []float64{0.5, 1.5}
	total += pick(ints, 2) + int(pick(floats, 1)) + copiedCount
	total += describe(7) + describe("four") + describe(ints) + describe(2.5)
	var holder any = ints
	if values, ok := holder.([]int); ok {
		total += len(values)
	}
	return total
}
`},
	{name: "inlined callees that write their parameters", want: 97, source: `package main

type pair struct{ left, right int }

func bumpInt(x int) int { x++; return x }

func halveFloat(x float64) float64 { x /= 2; return x }

func shoutText(x string) string { x += "!"; return x }

func flipBool(x bool) bool { x = !x; return x }

func growUint(x uint) uint { x += 3; return x }

func narrow(x int32) int32 { x++; x--; x++; return x }

func boxed() any { return pair{1, 2} }

func adder(base int) func(int) int { return func(x int) int { return x + base } }

func swap(p pair) pair { p.left, p.right = p.right, p.left; return p }

func countdown(n int) int {
	if n <= 0 {
		return 0
	}
	return 1 + countdown(n-1)
}

func run() int {
	total := 0
	for index := range 4 {
		total += bumpInt(index)
		total += int(halveFloat(float64(index) * 4))
		total += len(shoutText("hi"))
		if flipBool(index%2 == 0) {
			total++
		}
		total += int(growUint(uint(index)))
		total += int(narrow(int32(index)))
	}
	if value, ok := boxed().(pair); ok {
		total += value.left + value.right
	}
	total += adder(10)(5) + swap(pair{3, 9}).left + countdown(6)
	return total
}
`},
}

func TestTheCompileCorpusComputesWhatGoComputesWithAndWithoutOptimisations(t *testing.T) {
	t.Parallel()
	settings := map[string]passes.Options{"every optimisation": passes.DefaultOptions(), "no optimisation": {}}
	for setting, options := range settings {
		for _, corpusProgram := range compileCorpus {
			t.Run(setting+"/"+corpusProgram.name, func(t *testing.T) {
				t.Parallel()
				require.Equal(t, corpusProgram.want, runCompileCorpusProgram(t, options, corpusProgram))
			})
		}
	}
}

func runCompileCorpusProgram(t *testing.T, options passes.Options, corpusProgram compileCorpusProgram) int64 {
	t.Helper()
	service := NewService(WithOptimisations(options))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": corpusProgram.source})
	require.NoError(t, err)
	_, err = verify.VerifyBytecode(t.Context(), compiled.Root())
	require.NoError(t, err, "the bytecode verifies")
	require.NoError(t, verify.VerifyOperandBounds(t.Context(), compiled.Root()))
	value, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
	require.NoError(t, err)
	switch result := value.(type) {
	case int64:
		return result
	case int:
		return int64(result)
	default:
		require.Failf(t, "run returns an int", "got %T", value)
		return 0
	}
}
