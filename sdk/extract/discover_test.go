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
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

const fixtureModule = "module example.com/fixture\n\ngo 1.22\n"

func TestDiscoverFiltersProvidedImports(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod": fixtureModule,
		"main.go": `package main

import (
	"fmt"
	"strings"
)

func main() { fmt.Println(strings.ToUpper("world")) }
`,
	})

	result, err := Discover(context.Background(), DiscoverOptions{Root: root, AlreadyProvided: testProvidedPaths()})
	require.NoError(t, err)
	require.Empty(t, result.RequiredImports,
		"fmt and strings are provided and must be filtered, got %v", result.RequiredImports)
	require.Empty(t, result.SkippedCgo)
	require.Empty(t, result.GenericCandidates)
}

func TestDiscoverSurfacesUnprovidedStdlib(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod": fixtureModule,
		"main.go": `package main

import (
	"expvar"
	"log/syslog"
)

var _ = expvar.Do
var _ = syslog.LOG_INFO
`,
	})

	result, err := Discover(context.Background(), DiscoverOptions{Root: root})
	require.NoError(t, err)
	require.Contains(t, result.RequiredImports, "expvar",
		"unprovided stdlib must surface so the user can register it")
	require.Contains(t, result.RequiredImports, "log/syslog",
		"unprovided stdlib must surface so the user can register it")
}

func TestDiscoverIncludesProjectGoPackages(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":                 fixtureModule,
		"pkg/helpers/helpers.go": "package helpers\n\nfunc Double(x int) int { return x * 2 }\n",
		"main.go":                "package main\n\nimport \"example.com/fixture/pkg/helpers\"\n\nvar _ = helpers.Double\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{Root: root})
	require.NoError(t, err)
	require.Contains(t, result.RequiredImports, "example.com/fixture/pkg/helpers",
		"project packages surface unless the host ignores its own module")
}

func TestDiscoverIgnoredPrefixesDropOwnModule(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":                 fixtureModule,
		"pkg/helpers/helpers.go": "package helpers\n\nfunc Double(x int) int { return x * 2 }\n",
		"main.go":                "package main\n\nimport \"example.com/fixture/pkg/helpers\"\n\nvar _ = helpers.Double\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{
		Root:            root,
		IgnoredPrefixes: []string{"example.com/fixture"},
	})
	require.NoError(t, err)
	require.NotContains(t, result.RequiredImports, "example.com/fixture/pkg/helpers")
}

func TestDiscoverDropsVirtualPathsWithoutGoFiles(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":                    fixtureModule,
		"pkg/not_a_package/doc.txt": "this folder has no Go files",
		"main.go":                   "package main\n\nimport _ \"example.com/fixture/pkg/not_a_package\"\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{Root: root})
	require.NoError(t, err)
	require.NotContains(t, result.RequiredImports, "example.com/fixture/pkg/not_a_package",
		"paths without Go source must not surface as required")
}

func TestDiscoverRespectsExtraIgnored(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":  fixtureModule,
		"main.go": "package main\n\nimport \"example.com/external/thing\"\n\nvar _ = thing.Name\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{
		Root:         root,
		ExtraIgnored: []string{"example.com/external/thing"},
	})
	require.NoError(t, err)
	require.NotContains(t, result.RequiredImports, "example.com/external/thing")
}

func TestDiscoverUsesSuppliedScanners(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":             fixtureModule,
		"pkg/local/local.go": "package local\n\nfunc Something() int { return 1 }\n",
		"pages/home.tmpl":    "<div></div>\n<script>\npackage main\n\nimport \"example.com/fixture/pkg/local\"\n\nvar _ = local.Something\n</script>\n",
		"pages/ignored.go":   "package pages\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{
		Root:       root,
		SourceDirs: []string{"pages"},
		Scanners:   []SourceScanner{scriptBlockScanner{}},
	})
	require.NoError(t, err)
	require.Contains(t, result.RequiredImports, "example.com/fixture/pkg/local",
		"a supplied scanner reads its own file format")
	require.NotContains(t, result.RequiredImports, "expvar",
		"supplied scanners replace the default Go file scanner")
}

func TestDiscoverSortsAndDeduplicates(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":   fixtureModule,
		"a/a.go":   "package a\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
		"b/b.go":   "package b\n\nimport \"expvar\"\n\nvar _ = expvar.Do\n",
		"c/c.go":   "package c\n\nimport \"log/syslog\"\n\nvar _ = syslog.LOG_INFO\n",
		"d/fmt.go": "package d\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n",
	})

	result, err := Discover(context.Background(), DiscoverOptions{Root: root, AlreadyProvided: testProvidedPaths()})
	require.NoError(t, err)
	require.Equal(t, []string{"expvar", "log/syslog"}, result.RequiredImports)
	require.True(t, slices.IsSorted(result.RequiredImports))
}

func TestDiscoverMissingDirectoriesIsOK(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod": fixtureModule,
	})

	result, err := Discover(context.Background(), DiscoverOptions{
		Root:       root,
		SourceDirs: []string{"no_such_dir"},
	})
	require.NoError(t, err)
	require.Empty(t, result.RequiredImports)
}

func TestDiscoverRejectsCancelledContext(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":  fixtureModule,
		"main.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Discover(ctx, DiscoverOptions{Root: root})
	require.ErrorIs(t, err, context.Canceled)
}

func TestCollectSourceImportsSkipsToolDirectories(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"main.go":          "package main\n\nimport \"example.com/kept\"\n",
		"vendor/v/v.go":    "package v\n\nimport \"example.com/vendored\"\n",
		"testdata/t/t.go":  "package t\n\nimport \"example.com/testdata\"\n",
		".hidden/h.go":     "package h\n\nimport \"example.com/hidden\"\n",
		"_skipped/s.go":    "package s\n\nimport \"example.com/underscore\"\n",
		"sub/_ignored.go":  "package sub\n\nimport \"example.com/underscore-file\"\n",
		"sub/also_kept.go": "package sub\n\nimport \"example.com/also-kept\"\n",
	})

	imports, err := collectSourceImports(context.Background(), root, []string{"."}, []SourceScanner{GoFileScanner{}})
	require.NoError(t, err)
	require.Equal(t, []string{"example.com/also-kept", "example.com/kept"}, sortedKeys(imports))
}

func TestGoFileScannerIgnoresGeneratedFiles(t *testing.T) {
	t.Parallel()

	generated := []byte("// Code generated by 'pipit extract'. DO NOT EDIT.\n\npackage symbols\n\nimport \"golang.org/x/tools/go/gcexportdata\"\n")
	imports, err := GoFileScanner{}.Imports(context.Background(), "gen_types_loader.go", generated)
	require.NoError(t, err)
	require.Empty(t, imports, "a generated file is not a project source")

	written := []byte("package main\n\nimport \"expvar\"\n")
	imports, err = GoFileScanner{}.Imports(context.Background(), "main.go", written)
	require.NoError(t, err)
	require.Equal(t, []string{"expvar"}, imports)
}

func TestReadBoundedFileRejectsOversize(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "huge.go"), make([]byte, 2048), 0o644))

	root, err := os.OpenRoot(tmp)
	require.NoError(t, err)
	defer func() { require.NoError(t, root.Close()) }()

	_, err = readBoundedFile(root, "huge.go", 1024)
	require.ErrorIs(t, err, errSourceFileTooLarge)
}

func TestReadBoundedFileAcceptsUnderLimit(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	payload := []byte("hello world")
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "ok.go"), payload, 0o644))

	root, err := os.OpenRoot(tmp)
	require.NoError(t, err)
	defer func() { require.NoError(t, root.Close()) }()

	got, err := readBoundedFile(root, "ok.go", 1024)
	require.NoError(t, err)
	require.Equal(t, payload, got)
}

func TestPackageExportsGenericType(t *testing.T) {
	t.Parallel()

	genericPkg := loadPackageFromSource(t, "example.com/gen", `package gen

type Box[T any] struct { Value T }

func NewBox[T any](v T) Box[T] { return Box[T]{Value: v} }
`)
	require.True(t, packageExportsGenericType(genericPkg),
		"package with exported generic type must be flagged")

	nonGenericPkg := loadPackageFromSource(t, "example.com/nogen", `package nogen

type Simple struct { Name string }
`)
	require.False(t, packageExportsGenericType(nonGenericPkg),
		"package without generic types must not be flagged")

	nilPkg := &packages.Package{PkgPath: "example.com/empty"}
	require.False(t, packageExportsGenericType(nilPkg),
		"package without Types must not be flagged")
}

func TestPackageUsesCgo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pkg  *packages.Package
		want bool
	}{
		{
			name: "no cgo",
			pkg:  &packages.Package{PkgPath: "example.com/pure"},
			want: false,
		},
		{
			name: "imports C pseudo-package",
			pkg: &packages.Package{
				PkgPath: "example.com/cgoimport",
				Imports: map[string]*packages.Package{
					"C": {PkgPath: "C"},
				},
			},
			want: true,
		},
		{
			name: "has cgo1 generated file",
			pkg: &packages.Package{
				PkgPath: "example.com/cgogen",
				GoFiles: []string{"/tmp/x.cgo1.go"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, packageUsesCgo(tt.pkg))
		})
	}
}

func loadPackageFromSource(t *testing.T, importPath, source string) *packages.Package {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "source.go", source, parser.SkipObjectResolution)
	require.NoError(t, err)

	conf := types.Config{}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	pkg, err := conf.Check(importPath, fileSet, []*ast.File{file}, info)
	require.NoError(t, err)

	return &packages.Package{
		PkgPath: importPath,
		Name:    pkg.Name(),
		Types:   pkg,
	}
}

type fixtureFiles map[string]string

func writeFixtureProject(t *testing.T, files fixtureFiles) string {
	t.Helper()
	root := t.TempDir()
	for relativePath, content := range files {
		full := filepath.Join(root, relativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

type scriptBlockScanner struct{}

func (scriptBlockScanner) Match(name string) bool { return strings.HasSuffix(name, ".tmpl") }

func (scriptBlockScanner) Imports(_ context.Context, path string, data []byte) ([]string, error) {
	text := string(data)
	start := strings.Index(text, "<script>")
	end := strings.Index(text, "</script>")
	if start < 0 || end < start {
		return nil, nil
	}
	return ParseGoImports(path, []byte(text[start+len("<script>"):end]))
}

func testProvidedPaths() []string {
	return []string{"fmt", "strings"}
}
