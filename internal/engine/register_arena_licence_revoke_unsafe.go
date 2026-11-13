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
	"unsafe"
)

// walkHandOff is revokeValue's walk, for a hand-off revokeWithoutWalk and revokeTrivially
// did not settle.
//
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes cause (HandOffCause) which is the hand-off's cause.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches. When only guest code the arena's VM exported may be, the walk runs as an owner
// section (enterOwnerSection) and reads unshared if it gets the VM's gate.
//
// The walk memo holds the containers it records until the epoch ends, which keeps them
// reachable, so a program that calls runtime.SetFinalizer walks without it, because a
// finaliser must run once the program drops its last reference, not when the epoch ends.
func (a *RegisterArena) walkHandOff(value reflect.Value, boxed bool, cause HandOffCause, shared bool) {
	shared, owner := a.enterOwnerSection(shared)
	if owner != nil {
		defer owner.leaveOwnerSection()
	}
	a.walkHandOffAs(value, boxed, cause, shared)
}

// walkHandOffAs is walkHandOff with the sharing settled.
//
// Takes value (reflect.Value) which is the value handed off.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes cause (HandOffCause) which is the hand-off's cause.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
func (a *RegisterArena) walkHandOffAs(value reflect.Value, boxed bool, cause HandOffCause, shared bool) {
	state := &a.licenceState
	state.noteSeq++

	var walk licenceWalk
	walk.arena, walk.cause, walk.budget, walk.budgetAtStart = a, cause, state.walkBudget, state.walkBudget
	walk.shared, walk.banksPrivate, walk.fresh = shared, !state.banksRevoked, state.walkFresh[:0]

	walk.memo, walk.memoReadOnly = &state.memo, shared
	if vm := a.licenceVM; vm != nil && vm.allocatesForFinalisers() {
		walk.memo = nil
	}
	if a.containerStoreSkipsMemo() {
		walk.unmemoisedDepth = licenceWalkDepth
	}
	a.beginParkedWalk(&walk)
	walk.visitTop(value, boxed, licenceWalkDepth)
	endParkedWalk(&walk)
	state.walkFresh = walk.fresh[:0]
	state.walkBudget = max(walk.budget, 0)
	if walk.epochStop {
		a.revokeBlind()
		return
	}
	if walk.sharedStop {
		if licenceCounting {
			a.stats().sharedWalks.Add(1)
		}
		a.revokeEpoch(epochSharedContainer, cause)
		a.revokeBlind()
		return
	}
	if walk.stringsSkipped {
		if licenceCounting {
			a.stats().sharedWalks.Add(1)
		}
		a.revokeBlindBytes()
	}
	if walk.exhausted {
		reason := epochProbeExhausted
		if walk.budget <= 0 && walk.budgetAtStart < licenceWalkLeafBudget {
			reason = epochWalkBudget
		}
		a.revokeEpoch(reason, cause)
		a.revokeBlind()
		return
	}
	if walk.opaque {
		a.revokeBlind()
	}
}

// pointerFreePointeeOutside reports whether v, a non-nil pointer to a pointer-free type,
// points outside every arena's storage, so a walk of it would name nothing.
//
// Takes v (reflect.Value) which is a non-nil pointer whose element holds no pointers.
//
// Returns bool which is true when the hand-off is settled.
func (a *RegisterArena) pointerFreePointeeOutside(v reflect.Value) bool {
	return licenceExactProbes && licenceOutsideEveryArena(a, uintptr(v.UnsafePointer()))
}

// revokeTrivially resolves hand-offs whose storage can be classified without a walk.
//
// Takes value (reflect.Value) which is the value handed off, not boxed.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns bool which is true when the hand-off needs no walk.
func (a *RegisterArena) revokeTrivially(value reflect.Value, shared bool) bool {
	switch value.Kind() {
	case reflect.String:
		return licenceStringNamesNothing(a, value.String())
	case reflect.Pointer:
		if value.IsNil() {
			return false
		}
		if licenceTypeHoldsNoPointers(value.Type().Elem()) {
			return a.pointerFreePointeeOutside(value)
		}
		return a.memoSettled(value, a.memoSettles(value.UnsafePointer(), reflectValueABIType(value.Type().Elem()), 0, shared))
	case reflect.Slice:
		if value.Cap() == 0 || licenceTypeHoldsNoPointers(value.Type().Elem()) {
			return false
		}
		return a.memoSettled(value, a.memoSettles(value.UnsafePointer(), reflectValueABIType(value.Type().Elem()), value.Cap(), shared))
	case reflect.Map:
		if value.IsNil() {
			return false
		}
		return a.memoSettled(value, a.memoSettles(value.UnsafePointer(), reflectValueABIType(value.Type()), 0, shared))
	default:
		return false
	}
}

// memoSettled passes a memo-covered container to the census to check that it references
// no uncovered arena storage.
//
// Outside the census build it only returns settled.
//
// Takes v (reflect.Value) which is the container handed off.
// Takes settled (bool) which is memoSettles' answer.
//
// Returns bool which is settled.
func (a *RegisterArena) memoSettled(v reflect.Value, settled bool) bool {
	if storeCensusBuilt && settled {
		censusNoteMemoSettled(a, v)
	}
	return settled
}

// memoSettles reports whether the arena's memo covers the Go-heap container at pointer,
// so a hand-off of it needs no walk. A shared hand-off reads the memo without refreshing
// the entry and counts the hit.
//
// Takes pointer (unsafe.Pointer) which is the container's address.
// Takes typ (unsafe.Pointer) which is its type.
// Takes extent (int) which is the slice capacity, or zero.
// Takes shared (bool) which is true for a shared hand-off.
//
// Returns bool which is true when the memo settles the hand-off.
func (a *RegisterArena) memoSettles(pointer, typ unsafe.Pointer, extent int, shared bool) bool {
	memo := &a.licenceState.memo
	if !shared {
		return memo.covers(pointer, typ, extent) && licenceOutsideEveryArena(a, uintptr(pointer))
	}
	if !memo.peek(pointer, typ, extent) || !licenceOutsideEveryArena(a, uintptr(pointer)) {
		return false
	}
	if licenceCounting {
		blindScopeCounters.sharedMemoHits.Add(1)
	}
	return true
}

// licenceStringNamesNothing reports whether a hand-off of s names no storage.
//
// Takes a (*RegisterArena) which is the arena noting the hand-off.
// Takes s (string) which is the string handed off.
//
// Returns bool which is true when the walk of s would revoke nothing.
func licenceStringNamesNothing(a *RegisterArena, s string) bool {
	return len(s) == 0 || !licenceRegisteredPage(a, uintptr(unsafe.Pointer(unsafe.StringData(s))))
}

// licenceCoveredAt reports whether pointer lies in an own data slab covered by the
// licence.
//
// A slab is covered if it or the epoch is revoked, a foreign revocation is pending, or a
// walk skipped since the last reading might have named it.
//
// Takes pointer (unsafe.Pointer) which is the destination of a store.
//
// Returns bool which is true when the destination is covered storage of this arena.
func (a *RegisterArena) licenceCoveredAt(pointer unsafe.Pointer) bool {
	if !a.licensed || pointer == nil {
		return false
	}
	if a.licenceCoverEmpty() || !licenceRegisteredPage(a, uintptr(pointer)) {
		return false
	}
	base, found := licenceDataSlabOf(a, uintptr(pointer))
	if !found {
		return false
	}
	return a.licenceCoveredSlab(base)
}

// licenceCoveredStringBacking is licenceCoveredAt for pointer in one of the arena's
// []string backings (ownsStringBacking), which a store of a string element asks.
//
// Takes pointer (unsafe.Pointer) which is the destination backing.
//
// Returns bool which is licenceCoveredAt's answer.
func (a *RegisterArena) licenceCoveredStringBacking(pointer unsafe.Pointer) bool {
	if !a.licensed || pointer == nil || a.licenceCoverEmpty() {
		return false
	}
	base, _, found := licenceListSpan(a.stringBackingSlab, a.oldStringBackings, uintptr(pointer))
	if !found {
		return a.licenceCoveredAt(pointer)
	}
	return a.licenceCoveredSlab(base)
}

// licenceRevokedSlab is licenceRevokedBase for a slab value.
//
// Takes a (*RegisterArena) which is the arena whose revocation state is checked.
// Takes slab ([]T) which is the slab.
//
// Returns bool which is true for a revoked slab or a revoked epoch.
func licenceRevokedSlab[T any](a *RegisterArena, slab []T) bool {
	if cap(slab) == 0 {
		return false
	}
	return a.licenceRevokedBase(uintptr(unsafe.Pointer(unsafe.SliceData(slab))))
}

// revokeAddress revokes the storage holding address.
//
// Own data slabs are revoked individually, while upvalue and register-bank addresses
// revoke the epoch. Foreign storage is revoked through the licence registry, or through
// the byte-slab registry for an arena outside it.
//
// Takes address (uintptr) which is the address referenced.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) revokeAddress(address uintptr, cause HandOffCause) {
	if address == 0 {
		return
	}
	if base, _, ok := licenceDataSlabSpan(a, address); ok {
		a.revokeSlab(base, cause)
		return
	}
	a.revokeNonDataAddress(address, cause)
}

// revokeNonDataAddress is revokeAddress for an address in none of this arena's data
// slabs.
//
// An upvalue or register-bank address revokes the epoch, and another arena's storage is
// revoked through the licence registry (or the byte-slab registry).
//
// Takes address (uintptr) which is the address referenced, outside the arena's data
// slabs.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) revokeNonDataAddress(address uintptr, cause HandOffCause) {
	base, class := a.licenceNonDataSlabOf(address)
	switch class {
	case licenceSlabCell:
		a.revokeSlab(base, cause)
	case licenceSlabUpvalue:
		a.revokeEpoch(epochUpvalueCell, cause)
	case licenceSlabBank:
		a.revokeEpoch(epochRegisterBank, cause)
	default:
		if slabPagesMayHold(address) {
			a.revokeForeignAddress(address, cause)
		}
	}
}

// revokeForeignAddress records the revocation of the slab another arena holds at address,
// when one does and this arena's foreign revocations are not kept local.
//
// Takes address (uintptr) which is the address.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) revokeForeignAddress(address uintptr, cause HandOffCause) {
	var holder licenceForeignKey
	found := false
	if entry, registered := licenceSlabContaining(address); registered {
		holder, found = licenceForeignKey{start: entry.start, owner: entry.owner}, entry.owner != a.slabOwner
		if found && a.foreignLocal() {
			if licenceCounting {
				blindScopeCounters.foreignLocal.Add(1)
			}
			return
		}
	} else {
		holder.start, holder.owner, found = arenaSlabHolder(address)
	}
	switch {
	case !found:
	case a.licenceState.foreignLocked:
		revokeForeignSlabLocked(holder, cause)
	default:
		revokeForeignSlab(holder, cause)
	}
}

// revokeForeignSlab records the revocation of a slab held by another arena, which that
// arena applies at its release or next acquisition.
//
// Takes holder (licenceForeignKey) which is the slab's start and the arena the registry
// named as its holder.
// Takes cause (HandOffCause) which is the hand-off's cause.
//
// Concurrency: Holds licenceForeignRevokedMu while validating the published holder and
// recording a pending revocation.
func revokeForeignSlab(holder licenceForeignKey, cause HandOffCause) {
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	revokeForeignSlabLocked(holder, cause)
}

// revokeForeignSlabLocked is revokeForeignSlab with licenceForeignRevokedMu held.
//
// The registry was read without the lock, so before it records a new revocation it checks
// that a registry still names the holder at the slab's start. Every arena that stops
// publishing a slab removes it from the registry first and then drops its own pending
// revocations against it (dropForeignRevocations, dropOwnerForeignRevocations), so a
// revocation recorded here is either seen by that drop or finds the slab already gone. A
// slab gone from the registry is one its holder no longer holds. The holder never
// recycles it again, and the Go allocator cannot hand its address to another arena while
// the value being handed off still references it. Such a revocation has nothing left to
// protect, and recording it would leave an entry that no arena adopts or drops.
//
// Takes holder (licenceForeignKey) which is the slab's start and its holder.
// Takes cause (HandOffCause) which is the hand-off's cause.
func revokeForeignSlabLocked(holder licenceForeignKey, cause HandOffCause) {
	if _, already := licenceForeignRevoked.lookup(holder); already {
		return
	}
	licenceForeignRevokedCount.Add(1)
	if !licenceHolderPublishes(holder) {
		licenceForeignRevokedCount.Add(-1)
		if licenceCounting {
			licenceStats().foreignDropped.Add(1)
		}
		return
	}
	licenceForeignRevoked.record(holder, cause)
	if licenceCounting {
		licenceStats().foreignRevokes.Add(1)
	}
}

// licenceHolderPublishes reports whether the licence registry, or for a byte slab outside
// it the arena slab registry, still names holder's owner as holding a slab that starts at
// holder's start.
//
// Takes holder (licenceForeignKey) which is the slab's start and its holder.
//
// Returns bool which is true while the slab is published as the holder's.
func licenceHolderPublishes(holder licenceForeignKey) bool {
	if licencePublishes(holder.owner, holder.start) {
		return true
	}
	start, owner, found := arenaSlabHolder(holder.start)
	return found && start == holder.start && owner == holder.owner
}

// dropForeignRevocations removes owner's foreign revocations pending against starts,
// slabs it no longer holds and has already removed from the licence registry, so it can
// never reuse them.
//
// Takes owner (uint64) which identifies the arena that gave the slabs up.
// Takes starts ([]uintptr) which are slab start addresses.
//
// Concurrency: Protects pending foreign revocations with licenceForeignRevokedMu; the
// caller must remove the owner's ranges from the registry first.
func dropForeignRevocations(owner uint64, starts []uintptr) {
	if len(starts) == 0 || licenceForeignRevokedCount.Load() == 0 {
		return
	}
	licenceForeignRevokedMu.Lock()
	defer licenceForeignRevokedMu.Unlock()
	dropForeignRevocationsLocked(owner, starts)
}

// dropForeignRevocationsLocked is dropForeignRevocations with licenceForeignRevokedMu
// held, for an arena whose goroutine holds it through its sealed release (foreignLocked).
//
// Takes owner (uint64) which identifies the arena that gave the slabs up.
// Takes starts ([]uintptr) which are slab start addresses.
func dropForeignRevocationsLocked(owner uint64, starts []uintptr) {
	if len(starts) == 0 || licenceForeignRevokedCount.Load() == 0 {
		return
	}
	for _, start := range starts {
		key := licenceForeignKey{start: start, owner: owner}
		if licenceForeignRevoked.forget(key) {
			licenceForeignRevokedCount.Add(-1)
			if licenceCounting {
				licenceStats().foreignDropped.Add(1)
			}
		}
	}
}

// adoptForeignRevocationsLocked is adoptForeignRevocations with licenceForeignRevokedMu
// held.
//
// A data slab is revoked on its own; an upvalue slab or register bank revokes the epoch,
// as the arena's own walk would. Each revocation is recorded in the arena's licence
// before its pending entry is forgotten.
func (a *RegisterArena) adoptForeignRevocationsLocked() {
	if licenceForeignRevoked.len() == 0 {
		return
	}
	a.licenceState.adoptScratch = a.appendLicenceRanges(a.licenceState.adoptScratch[:0], a.slabOwner)
	for _, entry := range a.licenceState.adoptScratch {
		key := licenceForeignKey{start: entry.start, owner: entry.owner}
		cause, found := licenceForeignRevoked.lookup(key)
		if !found {
			continue
		}
		switch entry.class {
		case licenceSlabUpvalue:
			a.revokeEpoch(epochUpvalueCell, cause)
		case licenceSlabBank:
			a.revokeEpoch(epochRegisterBank, cause)
		default:
			a.revokeSlab(entry.start, cause)
		}
		forgetForeignRevocationLocked(key)
	}
}
