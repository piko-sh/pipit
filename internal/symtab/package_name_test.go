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

package symtab

import (
	"bufio"
	"go/types"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/policy"
)

type versionedNode struct {
	Value string
}

func readAssumedPackageNames(t *testing.T) map[string]string {
	t.Helper()

	file, err := os.Open("testdata/assumed_package_names.txt")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	cases := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		importPath, name, found := strings.Cut(line, "\t")
		require.True(t, found, "malformed line %q", line)
		cases[importPath] = name
	}
	require.NoError(t, scanner.Err())
	require.NotEmpty(t, cases)
	return cases
}

func TestAssumedPackageName(t *testing.T) {
	t.Parallel()

	for importPath, want := range readAssumedPackageNames(t) {
		assert.Equal(t, want, AssumedPackageName(importPath), importPath)
	}
}

func packageNameRegistry() *SymbolRegistry {
	return NewSymbolRegistry(SymbolExports{
		"gopkg.in/yaml.v3":           {"Marshal": reflect.ValueOf(func(any) ([]byte, error) { return nil, nil })},
		"github.com/example/go-yaml": {PackageNameSymbol: reflect.ValueOf("goyaml")},
		"github.com/example/invalid": {PackageNameSymbol: reflect.ValueOf("not valid")},
		"github.com/example/notext":  {PackageNameSymbol: reflect.ValueOf(42)},
		"example.com/typed":          {PackageNameSymbol: reflect.ValueOf("tablename")},
	})
}

func TestSymbolRegistryPackageNameSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		importPath string
		want       string
	}{
		{name: "derived from the path", importPath: "gopkg.in/yaml.v3", want: "yaml"},
		{name: "table entry", importPath: "github.com/example/go-yaml", want: "goyaml"},
		{name: "an invalid entry is ignored", importPath: "github.com/example/invalid", want: "invalid"},
		{name: "an entry that is not a string is ignored", importPath: "github.com/example/notext", want: "notext"},
		{name: "unregistered path", importPath: "example.com/unknown", want: "unknown"},
		{name: "a table without an entry gets the goimports name", importPath: "k8s.io/api/core/v1", want: "core"},
		{name: "a path the rule cannot name keeps its last element", importPath: "example.com/go-", want: "go-"},
		{name: "table entry without a types package", importPath: "example.com/typed", want: "tablename"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, packageNameRegistry().PackageName(tt.importPath))
		})
	}
}

func TestSymbolRegistryPackageNamePrefersTheTypesPackage(t *testing.T) {
	t.Parallel()
	registry := packageNameRegistry()
	registry.RegisterTypesPackage("example.com/typed", types.NewPackage("example.com/typed", "realname"))
	require.Equal(t, "realname", registry.PackageName("example.com/typed"), "the go/types package beats a conflicting entry")
}

func TestSymbolRegistrySynthesisesDeclaredPackageName(t *testing.T) {
	t.Parallel()

	registry := NewSymbolRegistry(SymbolExports{
		"gopkg.in/yaml.v3": {
			"Node": reflect.ValueOf((*versionedNode)(nil)),
		},
		"github.com/example/go-widgets": {
			PackageNameSymbol: reflect.ValueOf("widgetkit"),
			"Count":           reflect.ValueOf(func() int { return 0 }),
		},
	})

	yaml, err := registry.Import(t.Context(), "gopkg.in/yaml.v3")
	require.NoError(t, err)
	require.Equal(t, "yaml", yaml.Name())

	widgets, err := registry.Import(t.Context(), "github.com/example/go-widgets")
	require.NoError(t, err)
	require.Equal(t, "widgetkit", widgets.Name())
	require.NotNil(t, widgets.Scope().Lookup("Count"))
	require.Nil(t, widgets.Scope().Lookup(PackageNameSymbol), "the name entry is not a symbol")

	registeredType, ok := registry.ReflectTypeForNamedByPackageName("yaml", "Node")
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[versionedNode](), registeredType)
}

func TestSymbolRegistryScopedKeepsPackageName(t *testing.T) {
	t.Parallel()

	registry := NewSymbolRegistry(SymbolExports{
		"github.com/example/go-widgets": {
			PackageNameSymbol: reflect.ValueOf("widgetkit"),
			"Count":           reflect.ValueOf(func() int { return 0 }),
			"Reset":           reflect.ValueOf(func() {}),
		},
	})

	scoped := registry.Scoped([]string{"github.com/example/go-widgets.Count"})
	symbols, ok := scoped.PackageSymbols("github.com/example/go-widgets")
	require.True(t, ok)
	require.Len(t, symbols, 2)
	require.Equal(t, "widgetkit", scoped.PackageName("github.com/example/go-widgets"))
}

func TestApplyRestrictedSurfaceKeepsPackageName(t *testing.T) {
	t.Parallel()

	registry := NewSymbolRegistry(SymbolExports{
		"github.com/example/go-widgets": {
			PackageNameSymbol: reflect.ValueOf("widgetkit"),
			"Count":           reflect.ValueOf(func() int { return 0 }),
			"Reset":           reflect.ValueOf(func() {}),
		},
	})

	removed := ApplyRestrictedSurface(registry, policy.RestrictedPackageSet{
		Allowed: map[string]map[string]struct{}{
			"github.com/example/go-widgets": {"Count": {}},
		},
		FullyDenied: nil,
	})
	require.Equal(t, 1, removed)
	require.Equal(t, "widgetkit", registry.PackageName("github.com/example/go-widgets"))

	removed = ApplyRestrictedSurface(registry, policy.RestrictedPackageSet{
		Allowed:     nil,
		FullyDenied: map[string]struct{}{"github.com/example/go-widgets": {}},
	})
	require.Equal(t, 1, removed, "the name entry is not a symbol")
}
