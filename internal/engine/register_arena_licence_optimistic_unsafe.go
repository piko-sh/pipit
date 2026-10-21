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

// optimisticOwnStore reports whether a store into the slot at p is a store into this
// arena's own, uncovered data storage, which is not a hand-off. Covered storage may
// already be reachable from outside, so a store into it stays a hand-off.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes p (unsafe.Pointer) which is the destination slot.
//
// Returns bool which is true when the store needs no barrier.
func optimisticOwnStore(arena *RegisterArena, p unsafe.Pointer) bool {
	if arena != nil && arena.krefContains(p) {
		return krefMemberStore(arena, p)
	}
	return optimisticArena(arena) && ownUncoveredStorage(arena, p)
}

// ownUncoveredStorage reports whether pointer lies in this arena's own data storage and
// the licence does not cover it. A KRef chunk counts only while its slots are members of
// the arena's private set (krefMembersOwned), since a store into it then needs no
// barrier.
//
// Takes arena (*RegisterArena) which is non-nil and licensed.
// Takes pointer (unsafe.Pointer) which is the address. Nil answers false.
//
// Returns bool which is true for own, uncovered storage.
func ownUncoveredStorage(arena *RegisterArena, pointer unsafe.Pointer) bool {
	if pointer != nil && len(arena.kref.sorted) != 0 && arena.krefContains(pointer) && !arena.krefMembersOwned() {
		return false
	}
	return ownUncoveredDataStorage(arena, pointer)
}

// ownUncoveredDataStorage reports whether pointer lies in one of this arena's data slabs,
// KRef chunks included, that the licence does not cover.
//
// Takes arena (*RegisterArena) which is non-nil and licensed.
// Takes pointer (unsafe.Pointer) which is the address. Nil answers false.
//
// Returns bool which is true for own, uncovered data storage.
func ownUncoveredDataStorage(arena *RegisterArena, pointer unsafe.Pointer) bool {
	return ownUncoveredDataAddress(arena, uintptr(pointer))
}

// ownUncoveredDataAddress is ownUncoveredDataStorage for an address.
//
// Takes arena (*RegisterArena) which is non-nil and licensed.
// Takes address (uintptr) which is the address. Zero answers false.
//
// Returns bool which is true for uncovered storage of the arena.
func ownUncoveredDataAddress(arena *RegisterArena, address uintptr) bool {
	if arena.licensed && arena.licenceState.epochRevoked {
		return false
	}
	if address == 0 || !licenceRegisteredPage(arena, address) {
		return false
	}
	base, found := licenceDataSlabOf(arena, address)
	if !found {
		return false
	}
	return arena.licenceCoverEmpty() || !arena.licenceCoveredSlab(base)
}

// noteKeptString notes the hand-off of a string kept in place.
//
// Takes arena (*RegisterArena) which is licensed and owns the string.
// Takes text (string) which is the string kept.
func noteKeptString(arena *RegisterArena, text string) {
	base, _, _ := licenceListSpan(arena.byteSlab, arena.oldByteSlabs, uintptr(unsafe.Pointer(unsafe.StringData(text))))
	const cause = handOffIdentityKept
	if abortInjectBuilt {
		injectHandOffAbort(arena)
	}
	if storeCensusBuilt {
		censusNoteHandOff(arena, cause, reflect.ValueOf(text))
	}
	if !arena.licensed || !licenceRevokes(cause) {
		return
	}
	arena.licenceState.noteSeq++
	if licenceCounting {
		arena.stats().handOffs[cause].Add(1)
	}
	if arena.revokeWithoutWalk(cause) {
		return
	}
	state := &arena.licenceState
	if state.walkBudget <= 0 {
		reason := epochProbeExhausted
		if state.walkBudget < licenceWalkLeafBudget {
			reason = epochWalkBudget
		}
		arena.revokeEpoch(reason, cause)
		arena.revokeBlind()
		return
	}
	state.walkBudget--
	arena.revokeSlab(base, cause)
}

// optimisticOwnSlotStore is optimisticOwnStore for a general-path store of value into the
// slot at pointer. A string that may lie in another arena's slab still takes the barrier,
// as the typed string stores do (materialiseStringForSliceStore).
//
// Takes arena (*RegisterArena) which may be nil.
// Takes pointer (unsafe.Pointer) which is the slot; nil when it has no address.
// Takes value (reflect.Value) which is the value stored.
//
// Returns bool which is true when the store needs no barrier.
func optimisticOwnSlotStore(arena *RegisterArena, pointer unsafe.Pointer, value reflect.Value) bool {
	if arena != nil && arena.krefContains(pointer) {
		return krefMemberSlotStore(arena, pointer, value)
	}
	if !optimisticOwnStore(arena, pointer) {
		return false
	}
	if value.IsValid() && value.Kind() == reflect.String && arena.mayHoldForeignString(value.String()) && !arena.keepsForeignStrings() {
		return false
	}
	return true
}

// addressOf returns the address of an addressable value, or nil.
//
// Takes v (reflect.Value) which is the value.
//
// Returns unsafe.Pointer which is v's address when it has one.
func addressOf(v reflect.Value) unsafe.Pointer {
	if !v.IsValid() || !v.CanAddr() {
		return nil
	}
	return unsafe.Pointer(v.UnsafeAddr())
}

// optimisticOwnValueStore is optimisticOwnSlotStore for a destination held as a
// reflect.Value; outside optimism it returns before taking the address.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes destination (reflect.Value) which is the slot.
// Takes value (reflect.Value) which is the value stored.
//
// Returns bool which is true when the store needs no barrier.
func optimisticOwnValueStore(arena *RegisterArena, destination, value reflect.Value) bool {
	if !optimisticArena(arena) {
		if arena == nil || len(arena.kref.sorted) == 0 {
			return false
		}
		pointer := addressOf(destination)
		return arena.krefContains(pointer) && krefMemberSlotStore(arena, pointer, value)
	}
	return optimisticOwnSlotStore(arena, addressOf(destination), value)
}

// optimisticCloneSliceStrings applies the string clone budget inside a []string handed
// off by identity.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes value (reflect.Value) which is the slice handed off.
func optimisticCloneSliceStrings(arena *RegisterArena, value reflect.Value) {
	if !optimisticArena(arena) || value.Kind() != reflect.Slice || value.Type().Elem().Kind() != reflect.String || value.Cap() == 0 {
		return
	}
	backing := value.UnsafePointer()
	if !ownUncoveredStorage(arena, backing) || arena.licenceShared(0) {
		return
	}
	optimisticCloneAllOrNone(arena, unsafe.Slice((*string)(backing), value.Cap()))
}

// optimisticCloneAllOrNone is optimisticCloneSliceStrings' loop.
//
// The slice's own strings are cloned only if the epoch's budget covers all of them, and
// none is cloned otherwise.
//
// Takes arena (*RegisterArena) which is unshared, licensed and under the optimistic
// rules.
// Takes elements ([]string) which is the backing up to its capacity.
func optimisticCloneAllOrNone(arena *RegisterArena, elements []string) {
	if arena.licenceState.epochRevoked {
		return
	}
	cost := 0
	for _, element := range elements {
		if arena.OwnsString(element) && !optimisticByteSlabRevoked(arena, element) {
			cost += len(element) + optimisticCloneOverhead
		}
	}
	if cost == 0 || arena.optimisticClonedBytes+cost > optimisticCloneBudget(arena) {
		return
	}
	arena.optimisticClonedBytes += cost
	for index, element := range elements {
		if arena.OwnsString(element) && !optimisticByteSlabRevoked(arena, element) {
			elements[index] = cloneArenaString(arena, element)
			countOptimistic(OptimisticClonedInPlace)
		}
	}
}

// optimisticByteSlabRevoked reports whether text, an own string, lies in a byte slab the
// licence has already revoked.
//
// Takes arena (*RegisterArena) which owns text.
// Takes text (string) which is the string.
//
// Returns bool which is true when the slab is revoked.
func optimisticByteSlabRevoked(arena *RegisterArena, text string) bool {
	state := &arena.licenceState
	if len(state.revoked) == 0 {
		return false
	}
	base, _, found := licenceListSpan(arena.byteSlab, arena.oldByteSlabs, uintptr(unsafe.Pointer(unsafe.StringData(text))))
	if !found {
		return false
	}
	_, revoked := state.revoked[base]
	return revoked
}

// optimisticNew is reflect.New for a pointer-free type in the arena's byte slab.
//
// It returns a *T to zeroed arena storage.
//
// Takes arena (*RegisterArena) which provides the slab.
// Takes reflectType (reflect.Type) which is pointer-free with an alignment the slab
// guarantees (optimisticPointee checked both).
//
// Returns reflect.Value which is the pointer.
func optimisticNew(arena *RegisterArena, reflectType reflect.Type) reflect.Value {
	size := reflectType.Size()
	align := uintptr(reflectType.Align())
	if align == 0 {
		align = 1
	}
	pointer := arena.AllocBytes(size, align)
	if size > 0 {
		clear(unsafe.Slice((*byte)(pointer), size))
	}
	return reflect.NewAt(reflectType, pointer)
}

// optimisticStringSlotStore writes a string into a same-typed slot in a licensed arena.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes slot (reflect.Value) which is the destination; settable.
// Takes value (reflect.Value) which is the value to store.
//
// Returns bool which is true when the store was made.
func optimisticStringSlotStore(arena *RegisterArena, slot, value reflect.Value) bool {
	if !optimisticArena(arena) || !value.IsValid() || value.Kind() != reflect.String || !slot.CanSet() || slot.Type() != value.Type() {
		return false
	}
	text := value.String()
	if !optimisticOwnSlotStore(arena, addressOf(slot), value) {
		text = materialiseStringUnconditional(arena, text)
	}
	slot.SetString(text)
	return true
}

// ownCellStoreByValue reports whether a `*pointer = value` writes into this arena's own
// uncovered storage in a licensed arena, so that value needs no escape copy.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes pointer (unsafe.Pointer) which is the pointee's address.
// Takes value (reflect.Value) which is the value stored.
//
// Returns bool which is true when the store needs no barrier.
func ownCellStoreByValue(arena *RegisterArena, pointer unsafe.Pointer, value reflect.Value) bool {
	if !arenaUsesUnsafeSlabs || !licenceExactProbes || arena == nil || !arena.licensed || !value.IsValid() {
		return false
	}
	switch {
	case value.Kind() == reflect.String:
		if arena.mayHoldForeignString(value.String()) && !arena.keepsForeignStrings() {
			return false
		}
	case arena.cloneUnownedStrings && !typeIsPointerFree(value.Type()):
		return false
	default:
	}
	return ownUncoveredStorage(arena, pointer)
}
