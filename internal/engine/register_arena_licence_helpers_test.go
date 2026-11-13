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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func newArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	require.True(t, arena.licensed)
	return arena
}

func newArenaInTree(t *testing.T, tracker *ResourceTracker) *RegisterArena {
	t.Helper()
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{Tracker: tracker})
	require.True(t, arena.licensed)
	return arena
}

func newCallbackArenaInTree(t *testing.T, tracker *ResourceTracker) *RegisterArena {
	t.Helper()
	a := newArenaInTree(t, tracker)
	a.setLicenceVM(&VM{reentrantInterpreterVM: true, debugKind: DebugThreadCallback})
	return a
}

func newGoroutineArenaInTree(t *testing.T, tracker *ResourceTracker) *RegisterArena {
	t.Helper()
	arena := newArenaInTree(t, tracker)
	arena.setLicenceVM(&VM{})
	require.True(t, arena.servesNoNativeCall())
	return arena
}

func revokedEpochGoroutineArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := newGoroutineArenaInTree(t, &ResourceTracker{})
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	noteHandOffString(arena, handOffUntracedContainer, "blind once the epoch is revoked")
	require.True(t, arena.licenceState.epochRevoked)
	require.True(t, arena.licenceState.banksRevoked)
	return arena
}

func pagedArena(t *testing.T) *RegisterArena {
	t.Helper()
	return newArena(t)
}

func bytesStart(b []byte) uintptr { return uintptr(unsafe.Pointer(unsafe.SliceData(b))) }

func requireByteSlabRevoked(t *testing.T, arena *RegisterArena, text string) {
	t.Helper()
	base, class := arena.licenceSlabOf(uintptr(unsafe.Pointer(unsafe.StringData(text))))
	require.Equal(t, licenceSlabData, class, "the string must lie in one of the arena's data slabs")
	require.Contains(t, arena.licenceState.revoked, base,
		"the hand-off must revoke the byte slab the appended or copied string lives in")
}

func foreignPendingAt(t *testing.T, address uintptr) bool {
	t.Helper()
	entry, registered := licenceSlabContaining(address)
	require.True(t, registered, "test harness: the slab must be in the licence registry")
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	_, found := licenceForeignRevoked.lookup(licenceForeignKey{start: entry.start, owner: entry.owner})
	return found
}

func recoveredError(body func()) (err error) {
	defer func() {
		if recovered, ok := recover().(error); ok {
			err = recovered
		}
	}()
	body()
	return nil
}
