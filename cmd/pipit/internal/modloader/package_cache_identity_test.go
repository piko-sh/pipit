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

package modloader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolvedCacheIdentityIncludesEveryInput(t *testing.T) {
	t.Parallel()
	fixture := func() []ResolvedModule {
		return []ResolvedModule{{
			Path: "example.com/module", Version: "v1",
			Packages: map[string]map[string]string{
				"":      {"main.go": "package module", "helper.go": "package module; const Value=1"},
				"child": {"child.go": "package child"},
			},
			PackageOrder: []string{"child", ""},
		}}
	}
	original := ResolvedCacheIdentity(fixture())
	same := fixture()
	same[0].Packages[""] = map[string]string{"helper.go": "package module; const Value=1", "main.go": "package module"}
	require.Equal(t, original, ResolvedCacheIdentity(same), "source map insertion order does not change the identity")
	require.NotEqual(t, original, ResolvedCacheIdentity(append(fixture(), fixture()...)), "the module set size is bounded")
	tests := []struct {
		name   string
		mutate func([]ResolvedModule)
	}{
		{name: "module path", mutate: func(modules []ResolvedModule) { modules[0].Path = "example.com/other" }},
		{name: "version", mutate: func(modules []ResolvedModule) { modules[0].Version = "v2" }},
		{name: "package source", mutate: func(modules []ResolvedModule) { modules[0].Packages[""]["helper.go"] = "package module; const Value=2" }},
		{name: "child package source", mutate: func(modules []ResolvedModule) {
			modules[0].Packages["child"]["child.go"] = "package child; const Value=2"
		}},
		{name: "extra package", mutate: func(modules []ResolvedModule) {
			modules[0].Packages["extra"] = map[string]string{"extra.go": "package extra"}
		}},
		{name: "package order", mutate: func(modules []ResolvedModule) { modules[0].PackageOrder = []string{"", "child"} }},
		{name: "removed file", mutate: func(modules []ResolvedModule) { delete(modules[0].Packages[""], "helper.go") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			changed := fixture()
			tt.mutate(changed)
			require.NotEqual(t, original, ResolvedCacheIdentity(changed), "a changed dependency input changes the cache identity")
		})
	}
}

func TestBytecodeCacheScopesComposeWithoutMutation(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	first := cache.WithIdentity("first").WithIdentity("dependencies")
	second := cache.WithIdentity("second").WithIdentity("dependencies")
	require.NotEqual(t, first.path("module", "version"), second.path("module", "version"), "a dependency scope keeps its parent identity")
	require.NoError(t, first.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("first")}))
	for _, other := range []*BytecodeCache{cache, second} {
		_, hit, err := other.Get("module", "version")
		require.NoError(t, err, "the scope holds only its own entries")
		require.False(t, hit)
	}
	again := cache.WithIdentity("first").WithIdentity("dependencies")
	entry, hit, err := again.Get("module", "version")
	require.NoError(t, err, "the same context reuses the cache")
	require.True(t, hit)
	require.Equal(t, "first", string(entry.Bytecode))
}
