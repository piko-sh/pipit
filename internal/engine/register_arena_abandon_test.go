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

func TestPutRegisterArenaAbandoningEscapesReturnsTheArena(t *testing.T) {
	tests := []struct {
		name  string
		dirty func(a *RegisterArena)
	}{
		{name: "string bytes", dirty: func(a *RegisterArena) { a.AllocStringBytes(8) }},
		{name: "byte backing", dirty: func(a *RegisterArena) { a.AllocByteBacking(8) }},
		{name: "int backing", dirty: func(a *RegisterArena) { a.AllocIntBacking(4) }},
		{name: "string backing", dirty: func(a *RegisterArena) { a.AllocStringBacking(2) }},
		{name: "slice header", dirty: func(a *RegisterArena) { a.allocSliceHeader() }},
		{name: "box index", dirty: func(a *RegisterArena) { a.intBoxIndex = 1 }},
		{name: "upvalue cell index", dirty: func(a *RegisterArena) { a.UpvalueCellIndex = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arena := GetRegisterArena()
			arena.AttachBudgetTracker(&ResourceTracker{})
			tt.dirty(arena)
			require.True(t, arena.holdsEscapableData(), "the fixture must dirty an escapable slab")

			putRegisterArenaAbandoningEscapes(arena)

			require.Nil(t, arena.budgetTracker, "PutRegisterArena must run and detach the tracker")
			require.False(t, arena.holdsEscapableData())
			require.Nil(t, arena.ownerVM)
		})
	}
}

func TestAbandonEscapedSlabsLeavesEscapedMemoryIntact(t *testing.T) {
	arena := GetRegisterArena()
	defer PutRegisterArena(arena)
	escaped := arena.AllocStringBytes(4)
	copy(escaped, "pipt")
	backing := arena.AllocIntBacking(2)
	backing[0], backing[1] = 7, 9
	oldByteSlab, oldIntBacking := arena.byteSlab, arena.intBackingSlab

	arena.abandonEscapedSlabs()
	arena.Reset()

	require.Equal(t, "pipt", string(escaped), "an escaped string must survive the reset")
	require.Equal(t, []int64{7, 9}, backing, "an escaped backing must survive the reset")
	require.NotSame(t, &oldByteSlab[0], &arena.byteSlab[0], "the byte slab must be a fresh allocation")
	require.NotSame(t, &oldIntBacking[0], &arena.intBackingSlab[0], "the int backing must be a fresh allocation")
	require.Equal(t, InitialByteSlabSize, len(arena.byteSlab))
	require.Zero(t, arena.byteIndex)
	require.Zero(t, arena.intBackingIndex)
}
