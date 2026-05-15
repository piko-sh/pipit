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
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func helperRegistry() *SymbolRegistry {
	return NewSymbolRegistry(SymbolExports{
		"net/http": {
			"Get":  reflect.ValueOf(func(string) any { return nil }),
			"Post": reflect.ValueOf(func(string) any { return nil }),
			"Head": reflect.ValueOf(func(string) any { return nil }),
		},
		"strings": {
			"ToUpper":  reflect.ValueOf(strings.ToUpper),
			"Contains": reflect.ValueOf(strings.Contains),
		},
	})
}

func TestScopedNilAllowlistReturnsParent(t *testing.T) {
	t.Parallel()
	parent := helperRegistry()
	scoped := parent.Scoped(nil)
	require.Same(t, parent, scoped, "nil allowlist should return parent registry")
}

func TestScopedEmptyAllowlistExposesNothing(t *testing.T) {
	t.Parallel()
	parent := helperRegistry()
	scoped := parent.Scoped([]string{})
	_, httpVisible := scoped.PackageSymbols("net/http")
	require.False(t, httpVisible, "empty allowlist should hide net/http")
	_, stringsVisible := scoped.PackageSymbols("strings")
	require.False(t, stringsVisible, "empty allowlist should hide strings")
}

func TestScopedWildcardExposesPackage(t *testing.T) {
	t.Parallel()
	parent := helperRegistry()
	scoped := parent.Scoped([]string{"net/http/*"})

	httpPkg, ok := scoped.PackageSymbols("net/http")
	require.True(t, ok, "net/http should be in scoped registry")
	for _, want := range []string{"Get", "Post", "Head"} {
		_, found := httpPkg[want]
		assert.True(t, found, "net/http.%s missing under wildcard", want)
	}
	_, stringsVisible := scoped.PackageSymbols("strings")
	require.False(t, stringsVisible, "strings should NOT be in scoped registry")
}

func TestScopedSpecificSymbols(t *testing.T) {
	t.Parallel()
	parent := helperRegistry()
	scoped := parent.Scoped([]string{
		"net/http.Get",
		"strings.ToUpper",
	})

	httpPkg, ok := scoped.PackageSymbols("net/http")
	require.True(t, ok, "net/http should be in scoped registry")
	_, hasGet := httpPkg["Get"]
	assert.True(t, hasGet, "net/http.Get missing")
	_, hasPost := httpPkg["Post"]
	assert.False(t, hasPost, "net/http.Post leaked via named allowlist")

	stringsPkg, _ := scoped.PackageSymbols("strings")
	_, hasToUpper := stringsPkg["ToUpper"]
	assert.True(t, hasToUpper, "strings.ToUpper missing")
	_, hasContains := stringsPkg["Contains"]
	assert.False(t, hasContains, "strings.Contains leaked via named allowlist")
}

func TestScopedShareTypeCaches(t *testing.T) {
	t.Parallel()
	parent := helperRegistry()
	parent.SynthesiseAll()
	scoped := parent.Scoped([]string{"strings/*"})
	require.NotNil(t, scoped.reflectToTypes, "scoped registry must share reflectToTypes")
	require.NotNil(t, scoped.synthesised, "scoped registry must share synthesised cache")
}

func TestParseAllowlistEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input      string
		wantPath   string
		wantSymbol string
	}{
		{input: "net/http.Get", wantPath: "net/http", wantSymbol: "Get"},
		{input: "strings.ToUpper", wantPath: "strings", wantSymbol: "ToUpper"},
		{input: "net/http/*", wantPath: "net/http", wantSymbol: "*"},
		{input: "strings", wantPath: "strings", wantSymbol: ""},
		{input: "", wantPath: "", wantSymbol: ""},
		{input: "example.com/foo/v2.Type", wantPath: "example.com/foo/v2", wantSymbol: "Type"},
	}
	for _, tt := range tests {
		gotPath, gotSymbol := parseAllowlistEntry(tt.input)
		assert.Equal(t, tt.wantPath, gotPath, "parseAllowlistEntry(%q) path", tt.input)
		assert.Equal(t, tt.wantSymbol, gotSymbol, "parseAllowlistEntry(%q) symbol", tt.input)
	}
}
