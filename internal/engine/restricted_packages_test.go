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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/policy"
)

func helperRestrictedRegistry() *symtab.SymbolRegistry {
	return symtab.NewSymbolRegistry(symtab.SymbolExports{
		"reflect": {
			"TypeOf":    reflect.ValueOf(reflect.TypeOf),
			"ValueOf":   reflect.ValueOf(reflect.ValueOf),
			"DeepEqual": reflect.ValueOf(reflect.DeepEqual),
			"MakeMap":   reflect.ValueOf(reflect.MakeMap),
			"StructOf":  reflect.ValueOf(reflect.StructOf),
		},
		"runtime/debug": {
			"PrintStack": reflect.ValueOf(func() {}),
		},
		"strings": {
			"ToUpper":  reflect.ValueOf(func(s string) string { return s }),
			"Contains": reflect.ValueOf(func(string, string) bool { return false }),
		},
	})
}

func TestApplyRestrictedSurfaceFullyDeniedRemovesPackage(t *testing.T) {
	t.Parallel()
	registry := helperRestrictedRegistry()
	surface := policy.RestrictedPackageSet{
		FullyDenied: map[string]struct{}{"runtime/debug": {}},
	}
	removed := symtab.ApplyRestrictedSurface(registry, surface)
	require.Equal(t, 1, removed, "expected 1 symbol removed")
	_, ok := registry.PackageSymbols("runtime/debug")
	require.False(t, ok, "runtime/debug should be fully denied")
}

func TestApplyRestrictedSurfaceAllowsListedSymbols(t *testing.T) {
	t.Parallel()
	registry := helperRestrictedRegistry()
	surface := policy.RestrictedPackageSet{
		Allowed: map[string]map[string]struct{}{
			"reflect": {
				"TypeOf":    {},
				"DeepEqual": {},
			},
		},
	}
	removed := symtab.ApplyRestrictedSurface(registry, surface)
	require.NotZero(t, removed, "expected some symbols removed")
	reflectPkg, ok := registry.PackageSymbols("reflect")
	require.True(t, ok, "reflect should still be registered")
	_, hasTypeOf := reflectPkg["TypeOf"]
	assert.True(t, hasTypeOf, "reflect.TypeOf should be allowed")
	_, hasMakeMap := reflectPkg["MakeMap"]
	assert.False(t, hasMakeMap, "reflect.MakeMap should have been removed")
	_, hasStructOf := reflectPkg["StructOf"]
	assert.False(t, hasStructOf, "reflect.StructOf should have been removed")
}

func TestApplyRestrictedSurfacePackagesNotInPolicyAreUntouched(t *testing.T) {
	t.Parallel()
	registry := helperRestrictedRegistry()
	surface := policy.RestrictedPackageSet{
		FullyDenied: map[string]struct{}{"runtime/debug": {}},
	}
	symtab.ApplyRestrictedSurface(registry, surface)
	stringsPkg, ok := registry.PackageSymbols("strings")
	require.True(t, ok, "strings should still be registered")
	assert.Len(t, stringsPkg, 2, "strings should retain all entries")
}

func TestDefaultRestrictedSurfaceShape(t *testing.T) {
	t.Parallel()
	surface := policy.DefaultRestrictedSurface()
	_, deniesUnsafe := surface.FullyDenied["unsafe"]
	assert.True(t, deniesUnsafe, "default policy should deny unsafe")
	reflectAllowed, hasReflect := surface.Allowed["reflect"]
	assert.True(t, hasReflect, "default policy should allowlist reflect")
	_, hasMakeFunc := reflectAllowed["MakeFunc"]
	assert.False(t, hasMakeFunc, "default policy should NOT permit reflect.MakeFunc")
	_, hasTypeOf := reflectAllowed["TypeOf"]
	assert.True(t, hasTypeOf, "default policy should permit reflect.TypeOf")
}

func TestApplyRestrictedSurfaceNilRegistryIsNoop(t *testing.T) {
	t.Parallel()
	removed := symtab.ApplyRestrictedSurface(nil, policy.RestrictedPackageSet{})
	require.Zero(t, removed, "nil registry should remove nothing")
}

func TestDescribeRestrictedSurfaceFormatsSummary(t *testing.T) {
	t.Parallel()
	surface := policy.DefaultRestrictedSurface()
	description := policy.DescribeRestrictedSurface(surface)
	require.NotEmpty(t, description, "DescribeRestrictedSurface should not return empty")
}
