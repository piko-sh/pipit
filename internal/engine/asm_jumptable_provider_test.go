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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvideAsmHandlerJumpTableEntries_ProducesNonEmpty(t *testing.T) {
	t.Parallel()
	entries := ProvideAsmHandlerJumpTableEntries()
	require.NotEmpty(t, entries, "the asmgen entry-point must produce at least one handler installation entry")
}

func TestProvideAsmHandlerJumpTableEntries_NamesAreUnique(t *testing.T) {
	t.Parallel()
	entries := ProvideAsmHandlerJumpTableEntries()

	seen := make(map[string]int, len(entries))
	for index, entry := range entries {
		require.NotEmpty(t, entry.Name, "entry %d has empty Name", index)
		_, duplicate := seen[entry.Name]
		require.False(t, duplicate,
			"entry %d duplicates name %q first seen at index %d", index, entry.Name, seen[entry.Name])
		seen[entry.Name] = index
	}
}

func TestProvideAsmHandlerJumpTableEntries_ShimEntriesAppended(t *testing.T) {
	t.Parallel()
	staticEntries := buildStaticJumpTableEntries()
	allEntries := ProvideAsmHandlerJumpTableEntries()

	require.GreaterOrEqual(t, len(allEntries), len(staticEntries),
		"appendPathBShimEntries must not drop any static entry")
}

func TestBuildStaticJumpTableEntries_NonEmpty(t *testing.T) {
	t.Parallel()
	entries := buildStaticJumpTableEntries()
	require.NotEmpty(t, entries)
}
