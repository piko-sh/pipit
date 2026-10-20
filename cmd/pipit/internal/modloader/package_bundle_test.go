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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
)

func failOnWarning(t *testing.T) func(error) {
	t.Helper()
	return func(err error) {
		assert.NoError(t, err, "the cache reports no warning")
	}
}

func TestLoadOrCompilePackagePinsBundleOnBothPaths(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	interpreter := pipit.NewInterpreter()
	cache := NewBytecodeCache(t.TempDir())
	sources := map[string]string{"lib.go": "package lib\n\nfunc Answer() int { return 42 }\n"}

	compiled, hit, err := LoadOrCompilePackage(ctx, interpreter, cache, "example.com/lib", "v1.0.0", sources, failOnWarning(t))
	require.NoError(t, err)
	require.False(t, hit, "the compile path reports a miss")
	require.NotEmpty(t, compiled.Descriptor.Ref.Pin, "the compile path returns a pinned bundle")

	cached, hit, err := LoadOrCompilePackage(ctx, interpreter, cache, "example.com/lib", "v1.0.0", sources, failOnWarning(t))
	require.NoError(t, err)
	require.True(t, hit, "the cache path reports a hit")
	require.Equal(t, compiled.Descriptor.Ref.Pin, cached.Descriptor.Ref.Pin)
}

func TestPackageCacheInvalidatesChangedSource(t *testing.T) {
	t.Parallel()
	cache := NewBytecodeCache(t.TempDir())
	sources := map[string]string{"lib.go": "package lib; func Answer() int { return 42 }"}
	original, _, err := LoadOrCompilePackage(t.Context(), pipit.NewInterpreter(), cache, "example.com/lib", "v1", sources, failOnWarning(t))
	require.NoError(t, err)
	sources["lib.go"] = "package lib; func Answer() int { return 99 }"
	changed, hit, err := LoadOrCompilePackage(t.Context(), pipit.NewInterpreter(), cache, "example.com/lib", "v1", sources, failOnWarning(t))
	require.NoError(t, err, "changed source does not reuse stale bytecode")
	require.False(t, hit)
	require.NotEqual(t, original.Descriptor.Ref.Pin, changed.Descriptor.Ref.Pin)
}

func TestStdlibPackageSetClassifiesStandardLibraryImports(t *testing.T) {
	t.Parallel()
	set := StdlibPackageSet()
	_, ok := set["strings"]
	require.True(t, ok, "strings is in the stdlib set")
	_, ok = set["example.com/lib"]
	require.False(t, ok, "a module path is not stdlib")
}
