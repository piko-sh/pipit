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
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestByteBlindHandOffRevokesStringBytesOnly(t *testing.T) {
	arena := newArena(t)
	arena.AllocByteBacking(8)
	arenaString(arena, "strings")
	arena.revokeByteSlabs()
	require.Contains(t, arena.licenceState.revoked, bytesStart(arena.byteSlab))
	require.NotContains(t, arena.licenceState.revoked, bytesStart(arena.mutByteSlab),
		"a walk that skipped only strings cannot have named a []byte")
	arena.Reset()
}

func TestCallbackBlindHandOffReachesACallerInAnotherTree(t *testing.T) {
	creatorTree, callerTree, otherTree := &ResourceTracker{}, &ResourceTracker{}, &ResourceTracker{}
	caller := newArenaInTree(t, callerTree)
	callback := newCallbackArenaInTree(t, creatorTree)
	ordinary := newArenaInTree(t, otherTree)
	require.False(t, caller.readBlindHandOffs().all, "test harness: the caller starts with a clean blind interval")

	noteHandOff(ordinary, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, ordinary.licenceState.epochRevoked, "test harness: an opaque function value revokes the epoch")
	require.False(t, caller.readBlindHandOffs().all,
		"a blind hand-off by an ordinary arena of another tree must not reach the caller")

	callbackBlind := licenceTotal().blindCallbackHandOffs.Load()
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToLower))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's epoch is revoked")
	reading := caller.readBlindHandOffs()
	require.True(t, reading.all,
		"a callback VM's blind hand-off must reach the caller's tree although the callback counts in the wrapper creator's tree")
	require.True(t, caller.blindSinceReading(), "the store barrier's check must see it too")
	require.Greater(t, licenceTotal().blindCallbackHandOffs.Load(), callbackBlind, "the callback's blind hand-off is counted as such")
	require.False(t, callback.readBlindHandOffs().all, "the callback's own mark moves past its own increment")

	caller.commitBlindReading(reading)
	noteHandOffString(callback, handOffUntracedContainer, "bytes only")
	require.False(t, caller.readBlindHandOffs().all,
		"a string handed off in the revoked epoch is probed, and a constant names nothing")
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.TrimSpace))
	require.True(t, caller.readBlindHandOffs().all, "every later callback blind hand-off reaches the caller")

	for _, a := range []*RegisterArena{caller, callback, ordinary} {
		a.Reset()
	}
}

func TestCallbackBlindHandOffStillCountsInItsOwnTree(t *testing.T) {
	creatorTree := &ResourceTracker{}
	sibling := newArenaInTree(t, creatorTree)
	callback := newCallbackArenaInTree(t, creatorTree)
	treeBlind := creatorTree.licenceBlind.all.Load()
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.Greater(t, creatorTree.licenceBlind.all.Load(), treeBlind, "the callback still counts in its own tree")
	require.True(t, sibling.readBlindHandOffs().all)
	sibling.Reset()
	callback.Reset()
}

func TestReuseHazardCallbackInAnotherTreeSparesTheCallersBank(t *testing.T) {
	observations := &hazardObservations{}
	caller := newArenaInTree(t, &ResourceTracker{})
	callback := newCallbackArenaInTree(t, &ResourceTracker{})
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's epoch must be revoked")
	slot := callerBankPointer(t, caller)
	blind := licenceTotal().blindHandOffs.Load()
	noteHandOff(callback, handOffUntracedContainer, reflect.ValueOf(slot))
	observations.note("callback hand-off counted blind=%v", licenceTotal().blindHandOffs.Load() > blind)
	callback.Reset()
	reuseCallerBank(caller, slot, observations)
	assertNoHazard(t, observations, "")
}

func TestCallbackBlindHandOffStillReachesTheCallerItServes(t *testing.T) {
	caller := newArenaInTree(t, &ResourceTracker{})
	callback := newCallbackArenaInTree(t, &ResourceTracker{})
	t.Cleanup(func() { caller.Reset(); callback.Reset() })
	require.False(t, callback.blindLocal(), "a callback VM serves a native call, so its blind hand-offs are not kept local")
	require.False(t, caller.readBlindHandOffs().all, "test harness: the caller starts with a clean blind interval")
	noteHandOff(callback, handOffIdentityKept, reflect.ValueOf(strings.ToLower))
	require.True(t, callback.licenceState.epochRevoked, "test harness: the callback's epoch is revoked")
	require.True(t, caller.readBlindHandOffs().all, "a callback VM's blind hand-off must reach the caller it serves")
	noteHandOffString(callback, handOffUntracedContainer, arenaString(caller, "the caller's string"))
	require.True(t, caller.readBlindHandOffs().all)
}

func TestCallbackWalkRecordsForeignRevocations(t *testing.T) {
	caller := newArenaInTree(t, &ResourceTracker{})
	callback := newCallbackArenaInTree(t, &ResourceTracker{})
	t.Cleanup(func() { callback.Reset(); caller.Reset() })
	require.False(t, callback.foreignLocal(), "a callback VM serves a native call, so its foreign revocations are recorded")
	borrowed := arenaString(caller, "the caller's string, reached by the callback")
	noteHandOffString(callback, handOffUntracedContainer, borrowed)
	require.True(t, foreignPendingAt(t, uintptr(unsafe.Pointer(unsafe.StringData(borrowed)))),
		"the callback's walk must record a foreign revocation against the caller's byte slab")
}

func TestGoroutineBlindHandOffStaysLocal(t *testing.T) {
	tree := &ResourceTracker{}
	sibling := newArenaInTree(t, tree)
	goroutine := newGoroutineArenaInTree(t, tree)
	require.False(t, sibling.readBlindHandOffs().all, "test harness: the sibling starts clean")
	treeBlind, local := tree.licenceBlind.all.Load(), blindScopeStatistics().LocalBlindHandOffs
	blind := licenceTotal().blindHandOffs.Load()
	noteHandOff(goroutine, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, goroutine.licenceState.epochRevoked)
	noteHandOffString(goroutine, handOffUntracedContainer, "blind once the epoch is revoked")
	require.True(t, goroutine.licenceState.banksRevoked, "the blind hand-off still revokes the VM's own banks")
	require.Greater(t, licenceTotal().blindHandOffs.Load(), blind, "it still counts as blind")
	require.Equal(t, treeBlind, tree.licenceBlind.all.Load(), "the tree counter does not move")
	require.False(t, sibling.readBlindHandOffs().all, "the sibling's reading sees nothing")
	require.Greater(t, blindScopeStatistics().LocalBlindHandOffs, local)
	sibling.Reset()
	goroutine.Reset()
}

func TestGoroutineByteBlindHandOffStaysLocal(t *testing.T) {
	tree := &ResourceTracker{}
	sibling := newArenaInTree(t, tree)
	goroutine := newGoroutineArenaInTree(t, tree)
	goroutine.licenceAlwaysShared = true
	own := arenaString(goroutine, "the goroutine's own string")
	require.False(t, sibling.readBlindHandOffs().bytes, "test harness: the sibling starts clean")
	host := []string{own}
	noteHandOff(goroutine, handOffNativeArgUnknown, reflect.ValueOf(host))
	requireByteSlabRevoked(t, goroutine, own)
	require.False(t, sibling.readBlindHandOffs().bytes, "the byte counter does not move")
	sibling.Reset()
	goroutine.Reset()
}

func TestGoroutineWalkRecordsNoForeignRevocation(t *testing.T) {
	parent := newArenaInTree(t, &ResourceTracker{})
	goroutine := newGoroutineArenaInTree(t, &ResourceTracker{})
	skips := blindScopeStatistics().ForeignLocalSkips
	reached := arenaString(parent, "the parent's string, reached by the goroutine")
	noteHandOffString(goroutine, handOffUntracedContainer, reached)
	pending := foreignPendingAt(t, uintptr(unsafe.Pointer(unsafe.StringData(reached))))
	require.False(t, pending)
	require.Greater(t, blindScopeStatistics().ForeignLocalSkips, skips)
	goroutine.Reset()
	parent.Reset()
}

func TestBlindAndForeignScopingNeedALicence(t *testing.T) {
	a := NewRegisterArena()
	a.setLicenceVM(&VM{})
	require.False(t, a.blindLocal())
	require.False(t, a.foreignLocal())
}

func TestClosureWrapperSharesItsMakersMemory(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	wrapper, isFunc := reflect.TypeAssert[func() int](makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]()))
	require.True(t, isFunc)
	require.True(t, arena.licenceShared(0),
		"native code holding the wrapper may run it on another goroutine (sync.WaitGroup.Go), writing what the closure reaches, so the maker's walks and mark must not read it")
	require.Equal(t, 42, wrapper())
}

func TestClosureWrapperForABorrowingNativeKeepsItsMakerUnshared(t *testing.T) {
	maker, closure, arena := exportFixture(t)
	require.True(t, maker.beginBorrowingNativeCall(reflect.ValueOf(sort.Slice), BorrowUnknown, false), "sort.Slice keeps nothing once it returns")
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	require.False(t, arena.licenceShared(0), "a wrapper made for a borrowing native runs only inside the call")
	maker.endBorrowingNativeCall()

	require.False(t, maker.beginBorrowingNativeCall(reflect.ValueOf(time.AfterFunc), BorrowUnknown, false), "time.AfterFunc keeps its function")
	makeClosureWrapperFunc(maker, closure, reflect.TypeFor[func() int]())
	require.True(t, arena.licenceShared(0))
}

func TestCallbackExportSharesTheVMsParkedBelowIt(t *testing.T) {
	maker, _, arena := exportFixture(t)
	maker.borrowingNativeCall = true
	callback := newTestVM(t)
	callback.servedChainRun.below = maker
	callback.exportGuestCode()
	require.True(t, callback.guestCodeExported)
	require.True(t, arena.licenceShared(0),
		"guest code a callback exports may be its parked maker's, whose borrowing call does not cover the callback's own exports")
}

func TestAnotherExecutionsExportSharesTheFamilysCollections(t *testing.T) {
	globals := &GlobalStore{}
	exporter := newTestVM(t)
	exporter.Globals = globals
	exporter.Limits.Tracker = &ResourceTracker{}
	collector := newTestVM(t)
	collector.Globals = globals
	collector.Limits.Tracker = &ResourceTracker{}
	collector.CallStack = []CallFrame{{}}
	arena := newArenaInTree(t, collector.Limits.Tracker)
	arena.setLicenceVM(collector)
	collector.Arena = arena
	require.False(t, arena.licenceShared(0), "test harness: no VM of the family exported guest code")

	exporter.markGuestCodeExported()
	arena.refreshFamilyShared()

	require.True(t, arena.licenceShared(0),
		"guest code another execution exported may write the family's globals while this VM reads them")
	shared, owner := arena.enterOwnerSection(true)
	require.True(t, shared, "the VM's own gate does not exclude another execution's callbacks")
	require.Nil(t, owner)
	runtime.KeepAlive(exporter)
}

func TestAnExecutionsOwnExportLeavesItsFamilySectionsUnshared(t *testing.T) {
	globals := &GlobalStore{}
	tracker := &ResourceTracker{}
	exporter := newTestVM(t)
	exporter.Globals = globals
	exporter.Limits.Tracker = tracker
	arena := newArenaInTree(t, tracker)
	arena.setLicenceVM(exporter)
	exporter.Arena = arena

	exporter.markGuestCodeExported()
	arena.refreshFamilyShared()

	require.False(t, arena.licenceFamilyShared, "the execution's own exports are excluded by its own gates")
	shared, owner := arena.enterOwnerSection(true)
	require.False(t, shared)
	require.Same(t, exporter, owner)
	owner.leaveOwnerSection()
}

func TestACollectionDoesNotReadGlobalsAnotherExecutionsCallbackWrites(t *testing.T) {
	globals := &GlobalStore{general: make([]reflect.Value, 1), Strings: make([]string, 1)}
	exporter := newTestVM(t)
	exporter.Globals = globals
	exporter.Limits.Tracker = &ResourceTracker{}
	exporter.markGuestCodeExported()
	collector := newTestVM(t)
	collector.Globals = globals
	collector.Limits.Tracker = &ResourceTracker{}
	collector.CallStack = []CallFrame{{}}
	arena := newArenaInTree(t, collector.Limits.Tracker)
	setArenaBudget(t, arena, 1<<30)
	arena.setLicenceVM(collector)
	collector.Arena = arena

	stop := make(chan struct{})
	var rounds atomic.Int64
	var callback sync.WaitGroup
	callback.Go(func() {
		for round := 0; ; round++ {
			select {
			case <-stop:
				return
			default:
			}
			globals.general[0] = reflect.ValueOf(&round)
			globals.Strings[0] = strings.Repeat("g", round%8)
			rounds.Add(1)
		}
	})
	for collections := 0; collections < 64 || rounds.Load() < 4096; collections++ {
		arena.MinorGC(collector)
	}
	close(stop)
	callback.Wait()

	require.True(t, arena.licenceFamilyShared, "every collection ran shared, so none read the globals the callback wrote (checked under -race)")
	runtime.KeepAlive(exporter)
}
