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
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

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

	aliases := buildImportAliasMap(files)
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

	aliases := buildImportAliasMap(files)
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

	aliases := buildImportAliasMap(files)
	_, hasBlank := aliases["_"]
	_, hasDot := aliases["."]
	assert.False(t, hasBlank, "blank imports should be excluded")
	assert.False(t, hasDot, "dot imports should be excluded")
	assert.Equal(t, "github.com/example/normalpkg", aliases["normalpkg"])
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

	_, err := service.CompileProgram(context.Background(), "testmod", sources)
	require.Error(t, err)
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
	_, err := service.CompileProgram(context.Background(), "testmod", sources)
	require.Error(t, err)
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

	_, err := service.CompileFileSet(context.Background(), sources)
	require.Error(t, err)
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
			_, err := service.Eval(context.Background(), tt.code)
			require.Error(t, err)
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
			_, err := service.Eval(context.Background(), tt.code)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestEvalUnregisteredPackageImport(t *testing.T) {
	t.Parallel()

	service := newTestServiceWithSymbols(t, symtab.SymbolExports{})

	_, err := service.Eval(context.Background(), `import "example.com/missing/pkg"
_ = 1`)
	require.Error(t, err)
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
			_, err := service.Eval(context.Background(), tt.code)
			require.Error(t, err)
		})
	}
}

func TestEvalTypeAssertionFailure(t *testing.T) {
	t.Parallel()

	t.Run("failing type assertion produces error", func(t *testing.T) {
		t.Parallel()
		service := NewService()
		_, err := service.Eval(context.Background(), "var x interface{} = 42\nv := x.(string)\nv")
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
		{"double quoted", "package main\nimport _ \"unsafe\"\n"},
		{"raw string", "package main\nimport _ " + "`unsafe`" + "\n"},
		{"escaped", "package main\nimport _ \"\\x75nsafe\"\n"},
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

	entrypoints := []struct {
		name string
		run  func(ctx context.Context, service *Service) error
	}{
		{"Eval", func(ctx context.Context, service *Service) error {
			_, err := service.Eval(ctx, mixedCode)
			return err
		}},
		{"Compile", func(ctx context.Context, service *Service) error {
			_, err := service.Compile(ctx, mixedCode)
			return err
		}},
		{"CompileFileSet", func(ctx context.Context, service *Service) error {
			_, err := service.CompileFileSet(ctx, map[string]string{"main.go": fileSource})
			return err
		}},
		{"CompileProgram", func(ctx context.Context, service *Service) error {
			_, err := service.CompileProgram(ctx, "testmod", programSources)
			return err
		}},
	}

	for _, entrypoint := range entrypoints {
		t.Run(entrypoint.name, func(t *testing.T) {
			t.Parallel()
			service := NewService(WithImportAllowlist("testmod/lib"))
			err := entrypoint.run(context.Background(), service)
			require.Error(t, err)
			require.ErrorContains(t, err, "not permitted")
		})
	}
}
