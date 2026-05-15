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

import "testing"

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
	for _, mutate := range []func([]ResolvedModule){
		func(modules []ResolvedModule) { modules[0].Path = "example.com/other" },
		func(modules []ResolvedModule) { modules[0].Version = "v2" },
		func(modules []ResolvedModule) { modules[0].Packages[""]["helper.go"] = "package module; const Value=2" },
		func(modules []ResolvedModule) {
			modules[0].Packages["child"]["child.go"] = "package child; const Value=2"
		},
		func(modules []ResolvedModule) {
			modules[0].Packages["extra"] = map[string]string{"extra.go": "package extra"}
		},
		func(modules []ResolvedModule) { modules[0].PackageOrder = []string{"", "child"} },
		func(modules []ResolvedModule) { delete(modules[0].Packages[""], "helper.go") },
	} {
		changed := fixture()
		mutate(changed)
		if ResolvedCacheIdentity(changed) == original {
			t.Fatal("changed dependency input retained cache identity")
		}
	}
	same := fixture()
	same[0].Packages[""] = map[string]string{"helper.go": "package module; const Value=1", "main.go": "package module"}
	if ResolvedCacheIdentity(same) != original {
		t.Fatal("source map insertion order changed identity")
	}
	if ResolvedCacheIdentity(append(fixture(), fixture()...)) == original {
		t.Fatal("module set cardinality was not bound")
	}
}

func TestBytecodeCacheScopesComposeWithoutMutation(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	first := cache.WithIdentity("first").WithIdentity("dependencies")
	second := cache.WithIdentity("second").WithIdentity("dependencies")
	if first.path("module", "version") == second.path("module", "version") {
		t.Fatal("dependency scope erased its parent identity")
	}
	if err := first.Put("module", "version", BytecodeCacheEntry{Bytecode: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	for _, other := range []*BytecodeCache{cache, second} {
		if _, hit, err := other.Get("module", "version"); err != nil || hit {
			t.Fatalf("scope leaked entries: hit=%v error=%v", hit, err)
		}
	}
	again := cache.WithIdentity("first").WithIdentity("dependencies")
	if entry, hit, err := again.Get("module", "version"); err != nil || !hit || string(entry.Bytecode) != "first" {
		t.Fatalf("same context did not reuse cache: hit=%v error=%v", hit, err)
	}
}
