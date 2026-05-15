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
	"context"
	"testing"

	"pipit.sh/pipit"
)

func failOnWarning(t *testing.T) func(error) {
	t.Helper()
	return func(err error) {
		t.Errorf("unexpected cache warning: %v", err)
	}
}

func TestLoadOrCompilePackagePinsBundleOnBothPaths(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	interpreter := pipit.NewInterpreter()
	cache := NewBytecodeCache(t.TempDir())
	sources := map[string]string{"lib.go": "package lib\n\nfunc Answer() int { return 42 }\n"}

	compiled, hit, err := LoadOrCompilePackage(ctx, interpreter, cache, "example.com/lib", "v1.0.0", sources, failOnWarning(t))
	if err != nil {
		t.Fatalf("compile path: %v", err)
	}
	if hit {
		t.Fatalf("compile path reported a cache hit")
	}
	if compiled.Descriptor.Ref.Pin == "" {
		t.Fatalf("compile path returned an unpinned bundle")
	}

	cached, hit, err := LoadOrCompilePackage(ctx, interpreter, cache, "example.com/lib", "v1.0.0", sources, failOnWarning(t))
	if err != nil {
		t.Fatalf("cache path: %v", err)
	}
	if !hit {
		t.Fatalf("cache path reported a miss")
	}
	if cached.Descriptor.Ref.Pin != compiled.Descriptor.Ref.Pin {
		t.Fatalf("cache path pin = %q, want %q", cached.Descriptor.Ref.Pin, compiled.Descriptor.Ref.Pin)
	}
}

func TestPackageCacheInvalidatesChangedSource(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	sources := map[string]string{"lib.go": "package lib; func Answer() int { return 42 }"}
	original, _, err := LoadOrCompilePackage(context.Background(), pipit.NewInterpreter(), cache, "example.com/lib", "v1", sources, failOnWarning(t))
	if err != nil {
		t.Fatal(err)
	}
	sources["lib.go"] = "package lib; func Answer() int { return 99 }"
	changed, hit, err := LoadOrCompilePackage(context.Background(), pipit.NewInterpreter(), cache, "example.com/lib", "v1", sources, failOnWarning(t))
	if err != nil || hit || changed.Descriptor.Ref.Pin == original.Descriptor.Ref.Pin {
		t.Fatalf("changed source reused stale bytecode: hit=%v error=%v", hit, err)
	}
}

func TestStdlibPackageSetClassifiesStandardLibraryImports(t *testing.T) {
	t.Parallel()
	set := StdlibPackageSet()
	if _, ok := set["strings"]; !ok {
		t.Fatalf("strings is missing from the stdlib set")
	}
	if _, ok := set["example.com/lib"]; ok {
		t.Fatalf("a module path was classified as stdlib")
	}
}
