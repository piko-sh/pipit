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

//go:build pipit_arena_paranoid && !safe && !(js && wasm)

package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func releaseRevokedString(t *testing.T) string {
	t.Helper()
	arena := newArena(t)
	escaped := arenaString(arena, "escaped before a poisoned release")
	noteHandOffString(arena, handOffUntracedContainer, escaped)
	arena.Reset()
	return strings.Clone(escaped)
}

func TestReleaseDetachesARevokedSlabBeforeReset(t *testing.T) {
	require.Equal(t, "escaped before a poisoned release", releaseRevokedString(t),
		"release must detach the revoked slab before Reset poisons it")
}
