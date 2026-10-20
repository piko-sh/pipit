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

//go:build pipit_arena_paranoid

package engine

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParanoidResetPoisonsStringBytes(t *testing.T) {
	t.Parallel()

	t.Run("the current slab", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		stale := arena.AllocStringBytes(5)
		copy(stale, "hello")

		arena.Reset()

		require.Equal(t, bytes.Repeat([]byte{ArenaPoisonByte}, 5), stale,
			"a string the arena released must read as canary bytes")
	})

	t.Run("a retired slab", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		stale := arena.AllocStringBytes(5)
		copy(stale, "hello")
		arena.GrowByteSlab(len(arena.byteSlab) + 1)

		arena.Reset()

		require.Equal(t, bytes.Repeat([]byte{ArenaPoisonByte}, 5), stale)
	})

	t.Run("an abandoned slab keeps what escaped from it", func(t *testing.T) {
		t.Parallel()
		arena := NewRegisterArena()
		escaped := arena.AllocStringBytes(5)
		copy(escaped, "hello")

		arena.abandonEscapedSlabs()
		arena.Reset()

		require.Equal(t, []byte("hello"), escaped, "a value the abandon path left behind must stay readable")
	})
}
