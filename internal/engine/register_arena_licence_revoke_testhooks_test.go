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

//go:build pipit_testhooks && !safe && !(js && wasm)

package engine

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestPanicInTheForeignWindowLeavesTheRevocationPending(t *testing.T) {
	arena := newArena(t)
	escaped := arenaString(arena, "revoked by another arena's hand-off")
	base := uintptr(unsafe.Pointer(unsafe.SliceData(arena.byteSlab)))
	key := licenceForeignKey{start: base, owner: arena.slabOwner}
	revokeForeignSlab(key, handOffUntracedContainer)
	require.NoError(t, ArmAbortInjection("foreign-window", 1))
	err := recoveredError(arena.adoptForeignRevocations)
	report := DisarmAbortInjection()
	require.True(t, report.Fired, "the adoption must reach the window")
	require.Error(t, err)
	require.True(t, arena.licenceRevokedBase(base), "the revocation was recorded before the window")
	licenceForeignRevokedMu.Lock()
	_, pending := licenceForeignRevoked.lookup(key)
	licenceForeignRevokedMu.Unlock()
	require.True(t, pending, "the pending entry must not be forgotten before it is applied")
	arena.Reset()
	require.NotEqual(t, base, uintptr(unsafe.Pointer(unsafe.SliceData(arena.byteSlab))), "the revoked slab must be abandoned, not reused")
	licenceForeignRevokedMu.Lock()
	_, pending = licenceForeignRevoked.lookup(key)
	licenceForeignRevokedMu.Unlock()
	require.False(t, pending, "the release adopts the entry the panic left pending")
	require.Equal(t, "revoked by another arena's hand-off", escaped)
}
