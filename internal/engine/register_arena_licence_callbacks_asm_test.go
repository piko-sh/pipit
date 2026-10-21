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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func parkedCallerPair(t *testing.T) (caller, callback *VM, backing []string, leave func()) {
	t.Helper()
	if !goroutineTokens {
		t.Skip("no goroutine identity in this build")
	}
	caller = runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	callback = runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	callback.Arena.licenceAlwaysShared = true
	backing = caller.Arena.AllocStringBacking(4)
	for index := range backing {
		backing[index] = arenaString(caller.Arena, "parked-"+strings.Repeat("x", index+1))
	}
	registered := enterServedChains(t, caller, callback)
	return caller, callback, backing, func() {
		registered()
		callback.Arena.Reset()
		caller.Arena.Reset()
	}
}

func TestCallbackWalkReadsItsParkedCallersStorage(t *testing.T) {
	caller, callback, backing, leave := parkedCallerPair(t)
	defer leave()
	require.EqualValues(t, 1, caller.Arena.licenceState.cbActive.Load(), "the callback counts on the VM it serves")
	before := licenceTotal()
	blindBytes := before.blindByteHandOffs.Load()
	noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(backing))
	after := licenceTotal()
	require.False(t, callback.Arena.licenceState.epochRevoked, "the walk reads x's storage instead of stopping")
	require.Equal(t, blindBytes, after.blindByteHandOffs.Load(), "no blind hand-off")
	start := dataSlabStart(t, caller.Arena, unsafe.Pointer(unsafe.SliceData(backing)))
	licenceForeignRevokedMu.Lock()
	_, pending := licenceForeignRevoked.lookup(licenceForeignKey{start: start, owner: caller.Arena.slabOwner})
	licenceForeignRevokedMu.Unlock()
	require.True(t, pending, "x's backing is named by a pending foreign revocation")
}

func TestParkedCallerWalkFallsBackToBlindWhenAConditionFails(t *testing.T) {
	cases := []struct {
		name    string
		breakIt func(t *testing.T, x, y *VM)
	}{
		{name: "second-active-callback", breakIt: func(_ *testing.T, x, _ *VM) { parkedCallerBegin(x.Arena) }},
		{name: "running-goroutines", breakIt: func(_ *testing.T, x, _ *VM) {
			x.hasGoroutines, x.spawnedGoroutines = true, true
			x.Limits.Tracker = &ResourceTracker{}
			x.Limits.Tracker.GoroutineCount.Add(1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			caller, callback, backing, leave := parkedCallerPair(t)
			defer leave()
			tc.breakIt(t, caller, callback)
			blindBytes := licenceTotal().blindByteHandOffs.Load()
			noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(backing))
			require.Greater(t, licenceTotal().blindByteHandOffs.Load(), blindBytes, "the walk falls back to the blind stop")
		})
	}
}

func TestParkedCallerWalkStopsWhenACallbackStartsDuringIt(t *testing.T) {
	caller, callback, backing, leave := parkedCallerPair(t)
	defer leave()
	started := false
	restore := setParkedWalkHookForTest(func() {

		started = true
		parkedCallerBegin(caller.Arena)
	})
	defer restore()
	noteHandOff(callback.Arena, handOffIdentityKept, reflect.ValueOf(backing))
	require.True(t, started, "test harness: the walk read x's storage")
	t.Logf("callback started during the walk: walk stopped=%v", callback.Arena.licenceState.epochRevoked)
	require.True(t, callback.Arena.licenceState.epochRevoked,
		"a callback that starts during the walk turns it into a stop")
	parkedCallerEnd(caller.Arena)
}

func servedArenaPair(t *testing.T) (callerArena *RegisterArena, callbackArena *RegisterArena, leave func()) {
	t.Helper()
	if !goroutineTokens {
		t.Skip("no goroutine identity in this build")
	}
	caller := runningVMOnArena(newArenaInTree(t, &ResourceTracker{}), false)
	callback := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	callback.hasGoroutines = true
	callback.Arena.cloneUnownedStrings = true
	return caller.Arena, callback.Arena, enterServedChains(t, caller, callback)
}

func sameData(a, b string) bool { return unsafe.StringData(a) == unsafe.StringData(b) }

func TestServedStoreKeepsTheServedArenasString(t *testing.T) {
	callerArena, callbackArena, leave := servedArenaPair(t)
	defer leave()
	text := arenaString(callerArena, "the caller's string")
	backing := callerArena.AllocStringBacking(4)
	before := servedStoreStatistics()
	pending := licenceForeignRevokedCount.Load()
	stored := MaterialiseStringForTypedSliceStore(callbackArena, backing, text)
	require.True(t, sameData(text, stored), "a string of the served arena stored into its uncovered storage is not cloned")
	require.Equal(t, before.Stores+1, servedStoreStatistics().Stores)
	require.Equal(t, pending, licenceForeignRevokedCount.Load(), "a served store revokes nothing")
	require.Empty(t, callerArena.licenceState.revoked, "and covers nothing of the served arena")

	field := callerArena.AllocStringBacking(1)
	require.True(t, sameData(text, MaterialiseStringForFieldStore(callbackArena, unsafe.Pointer(unsafe.SliceData(field)), text)), "the field store barrier applies it too")
	callerArena.Reset()
	callbackArena.Reset()
}

func TestServedStoreRefusesWhatTheLabelDoesNotAllow(t *testing.T) {
	callerArena, callbackArena, leave := servedArenaPair(t)
	defer leave()
	text := arenaString(callerArena, "the caller's string")
	own := arenaString(callbackArena, "the callback's own string")
	third := newArena(t)
	other := arenaString(third, "a third arena's string")
	backing := callerArena.AllocStringBacking(4)
	full := callerArena.AllocStringBacking(2)
	before := servedStoreStatistics()

	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(backing)), own), "the callback's own string is not the served arena's")
	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(backing)), other), "nor is a third arena's")
	heap := make([]string, 2)
	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(heap)), text), "a heap target is not the served arena's storage")
	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(third.AllocStringBacking(2))), text), "nor is a third arena's")
	require.NotEqual(t, unsafe.StringData(MaterialiseStringForArenaAppend(callbackArena, full, text)), (*byte)(nil))
	require.Equal(t, before.Stores, servedStoreStatistics().Stores, "an append into a full slice grows into the callback's arena: no served store")

	noteHandOff(callerArena, handOffUntracedContainer, reflect.ValueOf(backing))
	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(backing)), text), "covered (handed-off) storage of the served arena is not a served store's target")

	unshared := backingNotRevoked(t, callerArena)
	callbackArena.licenceVM.hasGoroutines = false
	require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(unshared)), text), "a serving VM that would walk unshared falls back")
	require.Greater(t, servedStoreStatistics().Unshared, before.Unshared)
	callbackArena.licenceVM.hasGoroutines = true

	var wg sync.WaitGroup
	wg.Go(func() {
		require.False(t, callbackArena.servedStore(unsafe.Pointer(unsafe.SliceData(unshared)), text), "off the served arena's goroutine the registry establishes no chain")
	})
	wg.Wait()
	require.Equal(t, before.Stores, servedStoreStatistics().Stores)
	third.Reset()
	callerArena.Reset()
	callbackArena.Reset()
}

func backingNotRevoked(t *testing.T, arena *RegisterArena) []string {
	t.Helper()
	backing := arena.AllocStringBacking(cap(arena.stringBackingSlab) + 1)
	_, revoked := arena.licenceState.revoked[uintptr(unsafe.Pointer(unsafe.SliceData(arena.stringBackingSlab)))]
	require.False(t, revoked, "test harness: a grown slab is clean")
	return backing
}

func TestServedStoreKeepsOnlyTheServedArenasOwnStrings(t *testing.T) {
	callerArena, callbackArena, leave := servedArenaPair(t)
	innerCallback := runningVMOnArena(newCallbackArenaInTree(t, &ResourceTracker{}), true)
	innerCallback.hasGoroutines = true
	innerCallback.Arena.cloneUnownedStrings = true
	leaveZ := enterServedChains(t, innerCallback)
	backing := callerArena.AllocStringBacking(2)
	backing[0] = MaterialiseStringForTypedSliceStore(innerCallback.Arena, backing, arenaString(callbackArena, "made by the middle callback"))
	backing[1] = MaterialiseStringForTypedSliceStore(innerCallback.Arena, backing, arenaString(callerArena, "made by the root"))
	require.True(t, callerArena.OwnsString(backing[1]), "test harness: the root's own string is a served store")
	leaveZ()
	leave()
	callbackArena.Reset()
	arenaString(callbackArena, "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	require.Equal(t, "made by the middle callback", backing[0], "another arena's string outlives its arena only as a clone")
	require.Equal(t, "made by the root", backing[1])
	innerCallback.Arena.Reset()
	callerArena.Reset()
	callbackArena.Reset()
}
