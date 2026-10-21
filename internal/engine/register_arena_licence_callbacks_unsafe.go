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
	"sync/atomic"
	"unsafe"
)

var (
	// parkedWalkHook, when set (tests only), runs inside a walk the first time it reads the
	// parked caller's storage, so a test can start another callback during the walk.
	parkedWalkHook atomic.Pointer[func()]
)

// parkedPrivate reports whether a shared callback walk may read address as its own.
//
// Takes address (uintptr) which is an address outside the walking arena's storage.
//
// Returns bool which is true when the walk may read address.
func (w *licenceWalk) parkedPrivate(address uintptr) bool {
	parked := w.parked
	if parked == nil || parked.licenceState.epochRevoked {
		return false
	}
	base, found := licenceDataSlabOf(parked, address)
	if !found {
		return false
	}
	if _, revoked := parked.licenceState.revoked[base]; revoked {
		return false
	}
	if !w.usedParked {
		w.usedParked = true
		if hook := parkedWalkHook.Load(); hook != nil {
			(*hook)()
		}
	}
	if licenceCounting {
		parkedCallerCounters.parkedReads.Add(1)
	}
	return true
}

// beginParkedWalk looks for a parked caller a shared callback walk may read, at its
// start.
//
// Takes walk (*licenceWalk) which is the walk about to start.
func (a *RegisterArena) beginParkedWalk(walk *licenceWalk) {
	if !walk.shared || !parkedCallerReadsApply(a) {
		return
	}
	parked, generation := a.parkedCaller()
	if parked == nil {
		if licenceCounting {
			parkedCallerCounters.declined.Add(1)
		}
		return
	}
	walk.parked, walk.parkedGeneration = parked, generation
}

// endParkedWalk checks whether the conditions for reading parked caller storage still
// hold when the walk ends.
//
// Takes walk (*licenceWalk) which is the walk that ended.
func endParkedWalk(walk *licenceWalk) {
	if walk.parked == nil || !walk.usedParked {
		return
	}
	if parkedCallerHeld(walk.parked, walk.parkedGeneration) {
		if licenceCounting {
			parkedCallerCounters.parkedWalks.Add(1)
		}
		return
	}
	if licenceCounting {
		parkedCallerCounters.fallbacks.Add(1)
	}
	walk.sharedStop, walk.exhausted = true, true
}

// servedStoreApplies reports whether the arena may use served stores.
//
// Returns bool which is true when servedStore is worth asking.
func (a *RegisterArena) servedStoreApplies() bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && a != nil && a.licensed && a.cloneUnownedStrings
}

// servedStore reports whether a store of text into destination by this (callback) arena
// is a served store.
//
// Takes destination (unsafe.Pointer) which is the destination the caller writes.
// Takes text (string) which is the string stored.
//
// Returns bool which is true when the clone is skipped.
func (a *RegisterArena) servedStore(destination unsafe.Pointer, text string) bool {
	if destination == nil || len(text) == 0 || !a.servedStoreApplies() {
		return false
	}
	chain, ok := a.servedChain()
	if !ok {
		return false
	}
	for _, served := range chain {
		if served == a || !served.licensed || !licenceRegisteredPage(served, uintptr(destination)) {
			continue
		}
		if base, owned := licenceDataSlabOf(served, uintptr(destination)); owned {
			return a.servedStoreInto(served, base, destination, text)
		}
	}
	return false
}

// servedStoreInto decides a store into uncovered data storage of the served arena served,
// which the store's target selected.
//
// Takes served (*RegisterArena) which is the served arena holding the destination.
// Takes base (uintptr) which is the start of the destination's data slab.
// Takes destination (unsafe.Pointer) which is the destination the caller writes.
// Takes text (string) which is the string stored.
//
// Returns bool which is true when the clone is skipped.
func (a *RegisterArena) servedStoreInto(served *RegisterArena, base uintptr, destination unsafe.Pointer, text string) bool {
	if !servedValueOwnedBy(served, text) || !served.licenceCoverEmpty() && served.licenceCoveredSlab(base) {
		return false
	}
	if !a.servedWalksAreShared() {
		if licenceCounting {
			servedUnshared.Add(1)
		}
		return false
	}
	if storeCensusBuilt {
		censusNoteServedStore(a, served, destination, text)
	}
	if licenceCounting {
		servedStores.Add(1)
	}
	return true
}

// servedValueOwnedBy is the served store's value test.
//
// Takes x (*RegisterArena) which is the served arena.
// Takes s (string) which is the string stored.
//
// Returns bool which is true when s is x's.
func servedValueOwnedBy(x *RegisterArena, s string) bool {
	return x.OwnsString(s)
}

// servedWalksAreShared reports whether the storing arena meets the ordering condition of
// served stores.
//
// A VM serving a native call walks shared, so neither it nor any sibling serving the same
// arena reads that arena's strings in a walk.
//
// Returns bool which is true when the condition holds.
func (a *RegisterArena) servedWalksAreShared() bool {
	return a.servesNativeCaller() && a.licenceShared(0)
}

// parkedCallerReadsApply reports whether the arena's walks may read a parked caller's
// storage.
//
// Takes arena (*RegisterArena) which is the walking arena.
//
// Returns bool which is true when a shared walk may look for a parked caller.
func parkedCallerReadsApply(arena *RegisterArena) bool {
	return licenceExactProbes && arenaUsesUnsafeSlabs && goroutineTokens &&
		arena.licensed && servedChainEngaged.Load() && arena.servesNativeCaller()
}

// parkedCaller returns the arena of the VM a callback arena's walk may read as its own,
// with the generation of its callback starts, when every condition for reading it holds
// now.
//
// Returns *RegisterArena which is the parked caller's arena, or nil.
// Returns uint64 which is its start generation when the count was read.
func (a *RegisterArena) parkedCaller() (*RegisterArena, uint64) {
	vm := a.licenceVM
	if vm == nil {
		vm = a.ownerVM
	}
	if vm == nil || vm.Arena != a && vm.Arena != nil {
		return nil, 0
	}
	state := &vm.servedChainRun
	token := state.token.Load()
	if token == 0 || currentGoroutineToken() != token {
		return nil, 0
	}
	top := servedChainTop(token)
	served := state.below
	if top != vm || served == nil || served.servedChainRun.token.Load() != token || served.reentrantInterpreterVM {
		return nil, 0
	}
	servedArena := served.Arena
	if servedArena == nil || servedArena == a || !servedArena.licensed || servedArena.licenceState.epochRevoked || servedArena.licenceShared(0) {
		return nil, 0
	}
	generation := servedArena.licenceState.cbStarts.Load()
	if servedArena.licenceState.cbActive.Load() != 1 {
		return nil, 0
	}
	return servedArena, generation
}

// parkedCallerHeld reports whether the conditions the walk began under still hold at its
// end.
//
// Takes x (*RegisterArena) which is the parked caller's arena.
// Takes generation (uint64) which is the start generation read before the walk.
//
// Returns bool which is true when the walk may keep what it read.
func parkedCallerHeld(x *RegisterArena, generation uint64) bool {
	return x.licenceState.cbActive.Load() == 1 && x.licenceState.cbStarts.Load() == generation &&
		!x.licenceState.epochRevoked && !x.licenceShared(0)
}

// setParkedWalkHookForTest installs hook to run inside a walk the first time it reads its
// parked caller's storage, or removes it (nil), and returns a function restoring the
// previous hook.
//
// Takes hook (func()) which is the hook, or nil.
//
// Returns func() which restores the previous hook.
func setParkedWalkHookForTest(hook func()) func() {
	var previous *func()
	if hook == nil {
		previous = parkedWalkHook.Swap(nil)
	} else {
		previous = parkedWalkHook.Swap(&hook)
	}
	return func() { parkedWalkHook.Store(previous) }
}
