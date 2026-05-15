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

package extract

import (
	"bytes"
	"compress/gzip"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestExtractMathPackage(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"math"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]
	require.Equal(t, "math", pkg.ImportPath)
	require.Equal(t, "math", pkg.Name)

	symMap := make(map[string]ExtractedSymbol)
	for _, sym := range pkg.Symbols {
		symMap[sym.Name] = sym
	}

	pi, ok := symMap["Pi"]
	require.True(t, ok, "math.Pi should be extracted")
	require.False(t, pi.IsUntypedConst, "math.Pi is representable as float64 and should be emitted")
	require.Equal(t, SymbolConst, pi.Kind)
	require.Equal(t, "math.Pi", pi.ConstValue)

	maxUint64, ok := symMap["MaxUint64"]
	require.True(t, ok, "math.MaxUint64 should be extracted")
	require.True(t, maxUint64.IsUntypedConst, "math.MaxUint64 overflows default type int and must stay skipped")

	sqrt, ok := symMap["Sqrt"]
	require.True(t, ok, "math.Sqrt should be extracted")
	require.Equal(t, SymbolFunc, sqrt.Kind)

	_, ok = symMap["MaxFloat64"]
	require.True(t, ok, "math.MaxFloat64 should be extracted")
}

func TestExtractOsPackage(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"os"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]

	symMap := make(map[string]ExtractedSymbol)
	for _, sym := range pkg.Symbols {
		symMap[sym.Name] = sym
	}

	stdin, ok := symMap["Stdin"]
	require.True(t, ok, "os.Stdin should be extracted")
	require.Equal(t, SymbolVar, stdin.Kind)

	file, ok := symMap["File"]
	require.True(t, ok, "os.File should be extracted")
	require.Equal(t, SymbolType, file.Kind)

	exit, ok := symMap["Exit"]
	require.True(t, ok, "os.Exit should be extracted")
	require.Equal(t, SymbolFunc, exit.Kind)
}

func TestExtractInvalidPackage(t *testing.T) {
	t.Parallel()

	_, err := Extract([]string{"nonexistent/fake/package"}, nil)
	require.Error(t, err)
}

func TestExtractMultiplePackages(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"fmt", "strings", "math"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 3)

	paths := make(map[string]bool)
	for _, pkg := range pkgs {
		paths[pkg.ImportPath] = true
	}
	require.True(t, paths["fmt"])
	require.True(t, paths["strings"])
	require.True(t, paths["math"])
}

func TestFormatConstantLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  constant.Value
		expect string
	}{
		{"Int", constant.MakeInt64(42), "42"},
		{"String", constant.MakeString("hello"), `"hello"`},
		{"Zero", constant.MakeInt64(0), "0"},
		{"Bool", constant.MakeBool(true), "true"},
		{"NegativeInt", constant.MakeInt64(-7), "-7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expect, FormatConstantLiteral(tt.input))
		})
	}
}

func TestGenerateFileConstSymbol(t *testing.T) {
	t.Parallel()

	pkg := ExtractedPackage{
		ImportPath: "math",
		Name:       "math",
		Symbols: []ExtractedSymbol{
			{Name: "MaxFloat64", Kind: SymbolConst, ConstValue: "math.MaxFloat64"},
			{Name: "Sqrt", Kind: SymbolFunc},
		},
	}

	source, err := GenerateFile(pkg, "test_pkg", PackageConfig{})
	require.NoError(t, err)
	require.NotNil(t, source)

	src := string(source)

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "test.go", src, 0)
	require.NoError(t, parseErr, "generated code should be valid Go:\n%s", src)

	require.Contains(t, src, `reflect.ValueOf(math.MaxFloat64)`)
}

func TestImportAliasReflectCollision(t *testing.T) {
	t.Parallel()

	pkg := ExtractedPackage{
		ImportPath: "some/reflect",
		Name:       "reflect",
		Symbols: []ExtractedSymbol{
			{Name: "Something", Kind: SymbolFunc},
		},
	}

	source, err := GenerateFile(pkg, "test_pkg", PackageConfig{})
	require.NoError(t, err)
	require.NotNil(t, source)

	src := string(source)

	require.Contains(t, src, `_reflect "some/reflect"`)
}

func TestGenerateTypesLoaderFile(t *testing.T) {
	t.Parallel()

	source, err := GenerateTypesLoaderFile("test_pkg")
	require.NoError(t, err)
	require.NotNil(t, source)

	src := string(source)

	require.Contains(t, src, "//go:build !js || !wasm")
	require.Contains(t, src, "package test_pkg")

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, parseErr, "types loader code should be valid Go:\n%s", src)

	require.NotContains(t, src, `"go/importer"`)
	require.Contains(t, src, `"golang.org/x/tools/go/gcexportdata"`)
	require.Contains(t, src, "//go:embed gen_types_export/*")
	require.Contains(t, src, "func TypesPackages() map[string]*types.Package")
	require.Contains(t, src, "func TypesPackagesLoadError() error")
}

func TestGenerateTypesLoaderWASMFile(t *testing.T) {
	t.Parallel()

	source, err := GenerateTypesLoaderWASMFile("test_pkg")
	require.NoError(t, err)
	require.NotNil(t, source)

	src := string(source)

	require.Contains(t, src, "//go:build js && wasm")

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, parseErr, "WASM loader code should be valid Go:\n%s", src)

	require.Contains(t, src, "func TypesPackages() map[string]*types.Package")
	require.Contains(t, src, "func TypesPackagesLoadError() error")
	require.NotContains(t, src, "//go:embed")
}

func TestGenerateTypesExportBlobsRoundTrip(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"bufio", "io"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 2)

	blobs, err := GenerateTypesExportBlobs(pkgs)
	require.NoError(t, err)
	require.Len(t, blobs, 2)
	require.Equal(t, "bufio", blobs[0].ImportPath)
	require.Equal(t, "gen_types_export/bufio.bin.gz", blobs[0].FileName)
	require.Equal(t, []string{"io"}, blobs[0].Dependencies)
	require.Equal(t, "io", blobs[1].ImportPath)
	require.Empty(t, blobs[1].Dependencies)

	again, err := GenerateTypesExportBlobs(pkgs)
	require.NoError(t, err)
	require.Equal(t, blobs[0].Data, again[0].Data, "encoding must be deterministic")

	fset := token.NewFileSet()
	imports := make(map[string]*types.Package)
	for _, blob := range []TypesExportBlob{blobs[1], blobs[0]} {
		decoded, decodeErr := decodeTestBlob(fset, imports, blob)
		require.NoError(t, decodeErr)
		require.True(t, decoded.Complete())
		require.Equal(t, blob.ImportPath, decoded.Path())
	}
	require.NotNil(t, imports["bufio"].Scope().Lookup("NewReader"))
}

func TestGenerateTypesExportBlobsRejectsMissingTypes(t *testing.T) {
	t.Parallel()

	_, err := GenerateTypesExportBlobs([]ExtractedPackage{{ImportPath: "x/y"}})
	require.ErrorIs(t, err, errTypesExportMissingPackage)
}

func TestGenerateTypesExportBlobsRejectsFileNameCollision(t *testing.T) {
	t.Parallel()

	first := types.NewPackage("a/b_c", "b_c")
	second := types.NewPackage("a_b/c", "c")
	first.MarkComplete()
	second.MarkComplete()
	_, err := GenerateTypesExportBlobs([]ExtractedPackage{
		{ImportPath: first.Path(), TypesPackage: first},
		{ImportPath: second.Path(), TypesPackage: second},
	})
	require.ErrorIs(t, err, errTypesExportBlobCollision)
}

func TestGenerateTypesManifestFile(t *testing.T) {
	t.Parallel()

	blobs := []TypesExportBlob{
		{ImportPath: "bufio", FileName: "gen_types_export/bufio.bin.gz", Dependencies: []string{"io"}},
		{ImportPath: "io", FileName: "gen_types_export/io.bin.gz"},
	}
	source, err := GenerateTypesManifestFile(blobs, "test_pkg", "go1.99.0")
	require.NoError(t, err)

	src := string(source)
	require.Contains(t, src, "//go:build !js || !wasm")
	require.Contains(t, src, `const typesExportGoVersion = "go1.99.0"`)
	require.Contains(t, src, `{Path: "bufio", File: "gen_types_export/bufio.bin.gz", Dependencies: []string{"io"}},`)
	require.Contains(t, src, `{Path: "io", File: "gen_types_export/io.bin.gz"},`)

	fset := token.NewFileSet()
	_, parseErr := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	require.NoError(t, parseErr, "manifest code should be valid Go:\n%s", src)
}

func decodeTestBlob(fset *token.FileSet, imports map[string]*types.Package, blob TypesExportBlob) (*types.Package, error) {
	reader, err := gzip.NewReader(bytes.NewReader(blob.Data))
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return gcexportdata.Read(bytes.NewReader(data), fset, imports, blob.ImportPath)
}

func TestExtractEncodingJsonPackage(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"encoding/json"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]
	require.Equal(t, "encoding/json", pkg.ImportPath)
	require.Equal(t, "json", pkg.Name)

	symMap := make(map[string]ExtractedSymbol)
	for _, sym := range pkg.Symbols {
		symMap[sym.Name] = sym
	}

	marshal, ok := symMap["Marshal"]
	require.True(t, ok, "json.Marshal should be extracted")
	require.Equal(t, SymbolFunc, marshal.Kind)

	decoder, ok := symMap["Decoder"]
	require.True(t, ok, "json.Decoder should be extracted")
	require.Equal(t, SymbolType, decoder.Kind)
}

func TestExtractSortPackage(t *testing.T) {
	t.Parallel()

	pkgs, err := Extract([]string{"sort"}, nil)
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]

	symMap := make(map[string]ExtractedSymbol)
	for _, sym := range pkg.Symbols {
		symMap[sym.Name] = sym
	}

	iface, ok := symMap["Interface"]
	require.True(t, ok, "sort.Interface should be extracted")
	require.Equal(t, SymbolType, iface.Kind)

	stringsFunc, ok := symMap["Strings"]
	require.True(t, ok, "sort.Strings should be extracted")
	require.Equal(t, SymbolFunc, stringsFunc.Kind)
}

func TestExtractWithGenericConfigPopulatesTypesPackage(t *testing.T) {
	t.Parallel()

	config := PackageConfig{
		ImportPath:   "slices",
		ElementTypes: []string{"string"},
	}

	pkgs, err := Extract([]string{"slices"}, map[string]PackageConfig{"slices": config})
	require.NoError(t, err)
	require.Len(t, pkgs, 1)

	pkg := pkgs[0]
	require.NotNil(t, pkg.TypesPackage, "TypesPackage should be populated for generic packages")
	require.Equal(t, "slices", pkg.TypesPackage.Name())
}
