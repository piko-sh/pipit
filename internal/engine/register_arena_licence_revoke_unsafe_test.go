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
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

type pointerFreeValue struct{ a, b uint32 }

func heapOutsideEveryArena[T any](t *testing.T, arena *RegisterArena, newValue func() *T) *T {
	t.Helper()
	held := make([]*T, 0, 64)
	for attempt := range 64 {
		if attempt > 0 && attempt%16 == 0 {
			runtime.GC()
		}
		value := newValue()
		if licenceOutsideEveryArena(arena, uintptr(unsafe.Pointer(value))) {
			runtime.KeepAlive(held)
			return value
		}
		held = append(held, value)
	}
	require.FailNow(t, "64 heap values in a row lay in recorded slab ranges")
	return nil
}

func TestRevokeTriviallySettlesAPointerFreePointeeOutsideTheArena(t *testing.T) {
	arena := newArena(t)
	t.Cleanup(arena.Reset)
	heap := heapOutsideEveryArena(t, arena, func() *pointerFreeValue { return &pointerFreeValue{a: 1} })
	require.True(t, arena.revokeTrivially(reflect.ValueOf(heap), false), "a heap pointee is settled")

	inArena := (*pointerFreeValue)(arena.AllocBytes(unsafe.Sizeof(pointerFreeValue{}), unsafe.Alignof(pointerFreeValue{})))
	base, owned := licenceDataSlabOf(arena, uintptr(unsafe.Pointer(inArena)))
	require.True(t, owned, "test harness: the value lies in a data slab")
	require.False(t, arena.revokeTrivially(reflect.ValueOf(inArena), false), "an arena pointee is walked")
	arena.revokeValue(reflect.ValueOf(inArena), false, handOffUntracedContainer, false)
	require.Contains(t, arena.licenceState.revoked, base, "the walk revokes the pointee's slab")
}

func TestBorrowingNativeArgumentDoesNotRevoke(t *testing.T) {
	arena := newArena(t)
	require.False(t, licenceRevokes(handOffNativeArgBorrows))
	require.False(t, licenceRevokes(handOffNativeArgResultAliases))
	require.True(t, licenceRevokes(handOffNativeArgRetains))
	require.Equal(t, BorrowRetains, arena.nativeBorrowClass(reflect.ValueOf(errors.New)))
}

func TestHandOffAfterAnEpochRevocationSkipsTheWalkFailingClosed(t *testing.T) {
	arena := newArena(t)
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, arena.licenceState.epochRevoked)
	other := newArena(t)
	before := licenceBlindProcess.all.Load()
	escaped := arenaString(arena, "handed off once the epoch is revoked")
	noteHandOffString(arena, handOffUntracedContainer, escaped)
	require.Empty(t, arena.licenceState.revoked, "the walk is skipped once release abandons every data slab anyway")
	require.True(t, arena.licenceState.banksRevoked, "a skipped walk might have named a register bank")
	require.Greater(t, licenceBlindProcess.all.Load(), before, "a skipped walk after another arena's reading must be counted")
	counted := licenceBlindProcess.all.Load()
	noteHandOffString(arena, handOffUntracedContainer, escaped)
	require.Equal(t, counted, licenceBlindProcess.all.Load(), "with no reading since the count moved, a skipped walk needs no increment")
	require.True(t, other.readBlindHandOffs().all, "a skipped walk must be visible to every other arena's release")
	other.Reset()
}

func licenceCoveredAtBySearch(arena *RegisterArena, pointer unsafe.Pointer) bool {
	if !arena.licensed || pointer == nil || arena.licenceCoverEmpty() {
		return false
	}
	base, found := licenceDataSlabOf(arena, uintptr(pointer))
	return found && arena.licenceCoveredSlab(base)
}

func TestLicenceCoverAnswersMatchTheFullSearch(t *testing.T) {
	arena := newArena(t)
	heap := new([4]string)
	text := arenaString(arena, "own byte slab")
	backing := arena.AllocStringBacking(4)
	addresses := map[string]unsafe.Pointer{
		"heap":           unsafe.Pointer(heap),
		"byte slab":      unsafe.Pointer(unsafe.StringData(text)),
		"string backing": unsafe.Pointer(unsafe.SliceData(backing)),
	}
	check := func(state string, wantBacking bool) {
		for name, p := range addresses {
			want := licenceCoveredAtBySearch(arena, p)
			require.Equal(t, want, arena.licenceCoveredAt(p), "%s, %s", state, name)
		}
		require.Equal(t, wantBacking, arena.licenceCoveredStringBacking(addresses["string backing"]), state)
		require.Equal(t, licenceCoveredAtBySearch(arena, addresses["string backing"]), arena.licenceCoveredStringBacking(addresses["string backing"]), state)
		for name, p := range addresses {
			require.Equal(t, optimisticOwnStoreBySearch(arena, p), optimisticOwnStore(arena, p), "%s, %s: own store", state, name)
		}
	}
	check("nothing covered", false)
	arena.revokeSlab(uintptr(unsafe.Pointer(unsafe.SliceData(arena.byteSlab))), handOffNativeArgUnknown)
	require.False(t, arena.licenceCoverEmpty())
	require.False(t, arena.licenceCoveredAt(addresses["heap"]), "a Go-heap address is never covered")
	require.True(t, arena.licenceCoveredAt(addresses["byte slab"]))
	check("byte slab revoked", false)
	arena.revokeSlab(uintptr(unsafe.Pointer(unsafe.SliceData(arena.stringBackingSlab))), handOffNativeArgUnknown)
	check("string backing revoked", true)
	arena.revokeEpoch(epochProbeExhausted, handOffNativeArgUnknown)
	check("epoch revoked", true)
	require.False(t, arena.licenceCoveredAt(addresses["heap"]), "a Go-heap address is never covered")
}

func optimisticOwnStoreBySearch(arena *RegisterArena, pointer unsafe.Pointer) bool {
	if !optimisticArena(arena) || pointer == nil {
		return false
	}
	if _, found := licenceDataSlabOf(arena, uintptr(pointer)); !found {
		return false
	}
	return !licenceCoveredAtBySearch(arena, pointer)
}
