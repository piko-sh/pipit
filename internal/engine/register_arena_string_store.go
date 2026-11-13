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
	"reflect"
	"strings"
	"unsafe"
)

// MaterialiseStringForTypedSliceStore applies materialiseStringForSliceStore to a typed
// []string register whose element is being overwritten in place.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes destination ([]string) which is the slice receiving the element.
// Takes s (string) which is the element about to be stored.
//
// Returns the element to store.
func MaterialiseStringForTypedSliceStore(arena *RegisterArena, destination []string, s string) string {
	return materialiseStringForSliceStore(arena, unsafe.Pointer(unsafe.SliceData(destination)), s)
}

// MaterialiseStringForGoAppend is the store barrier for an element appended with Go's
// append. With spare capacity the element lands in source's backing, so the ownership of
// that backing decides; without spare capacity Go reallocates on the heap and the element
// always needs the plain barrier.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes source ([]string) which is the slice being appended to.
// Takes s (string) which is the element about to be appended.
//
// Returns the element to append.
func MaterialiseStringForGoAppend(arena *RegisterArena, source []string, s string) string {
	if len(source) == cap(source) {
		return materialiseString(arena, s)
	}
	return materialiseStringForSliceStore(arena, unsafe.Pointer(unsafe.SliceData(source)), s)
}

// MaterialiseStringForArenaAppend applies the string store barrier for arenaAppendString.
// Foreign arena strings are cloned even into local arena storage because the escape walk
// only recognises local strings.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes source ([]string) which is the slice being appended to.
// Takes s (string) which is the element about to be appended.
//
// Returns the element to append.
func MaterialiseStringForArenaAppend(arena *RegisterArena, source []string, s string) string {
	if cap(source) == 0 {
		if arena != nil && arena.mayHoldForeignString(s) {
			return strings.Clone(s)
		}
		return s
	}

	return materialiseStringForBackingStore(arena, unsafe.Pointer(unsafe.SliceData(source)), s, len(source) < cap(source))
}

// MaterialiseStringForFieldStore applies the string store barrier when writing through a
// struct's raw base pointer.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes base (unsafe.Pointer) which is the address of the receiving struct.
// Takes s (string) which is the value about to be stored.
//
// Returns a heap-cloned copy when s is arena-backed and the struct is not, or s
// unchanged.
func MaterialiseStringForFieldStore(arena *RegisterArena, base unsafe.Pointer, s string) string {
	if arena == nil || !arenaUsesUnsafeSlabs || (arena.ownsBytePointer(base) && !arena.mayHoldForeignString(s)) {
		return s
	}
	if arena.mayHoldForeignString(s) && (arena.ownUncoveredStringSlot(base) || arena.servedStore(base, s)) {
		return s
	}
	if krefMemberStringStore(arena, base, s) {
		return s
	}
	return materialiseStringUnconditional(arena, s)
}

// materialiseString is the store barrier for strings leaving the register file for heap
// memory, returning a heap-backed copy when s points into the arena's byte slabs.
//
// Takes arena (*RegisterArena) which is the arena whose byte slabs are checked.
// Takes s (string) which is the string to materialise.
//
// Returns a cloned string if arena-backed, or s unchanged.
func materialiseString(arena *RegisterArena, s string) string {
	if arena == nil || !arenaUsesUnsafeSlabs {
		return s
	}
	return materialiseStringUnconditional(arena, s)
}

// materialiseStringUnconditional clones arena-backed strings at escape boundaries,
// including builds where store barriers are disabled.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes s (string) which is the candidate string.
//
// Returns a heap-cloned copy when arena-backed or possibly foreign, or s unchanged.
func materialiseStringUnconditional(arena *RegisterArena, s string) string {
	if optimisticArena(arena) {
		return materialiseStringOptimistic(arena, s)
	}
	return cloneArenaString(arena, s)
}

// cloneArenaString clones text when it is arena-backed or possibly foreign.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes text (string) which is the candidate string.
//
// Returns a heap-cloned copy when arena-backed or possibly foreign, or text unchanged.
func cloneArenaString(arena *RegisterArena, text string) string {
	if arena.OwnsString(text) {
		return strings.Clone(text)
	}
	if arena.inAnArenaSlab(text) {
		if arena.callbackRoot != nil {
			arena.callbackRoot.NoteCallbackForeignClone()
		}
		return strings.Clone(text)
	}
	return text
}

// mayHoldForeignString reports whether s may point into another VM's arena when cloning
// unowned strings is enabled.
//
// Takes s (string) which is the candidate string.
//
// Returns true when s must be cloned as possibly foreign.
func (a *RegisterArena) mayHoldForeignString(s string) bool {
	return a.inAnArenaSlab(s) && !a.OwnsString(s)
}

// inAnArenaSlab checks whether s lies in a registered slab when cloning unowned strings
// is enabled.
//
// The cached slab may have left the registry, which can cause an unnecessary but safe
// clone.
//
// Takes s (string) which is the candidate string.
//
// Returns true when s lies in some arena's byte slab and this arena clones unowned
// strings.
func (a *RegisterArena) inAnArenaSlab(s string) bool {
	if !arenaUsesUnsafeSlabs || !a.cloneUnownedStrings || len(s) == 0 {
		return false
	}
	pointer := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	if pointer-a.foreignSlabStart < a.foreignSlabLength {
		return true
	}
	start, end, found := arenaSlabContaining(pointer)
	if found {
		a.foreignSlabStart, a.foreignSlabLength = start, end-start
	}
	return found
}

// materialiseStringForSliceStore applies the string store barrier to an element written
// into a []string backing.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes backing (unsafe.Pointer) which is the destination slice's data pointer.
// Takes s (string) which is the element about to be stored.
//
// Returns a heap-cloned copy when s is arena-backed and the destination is not, or is
// storage the licence covers (a store into handed-off storage is itself a hand-off), or s
// unchanged.
func materialiseStringForSliceStore(arena *RegisterArena, backing unsafe.Pointer, s string) string {
	return materialiseStringForBackingStore(arena, backing, s, true)
}

// materialiseStringForBackingStore is materialiseStringForSliceStore, with the served
// store (register_arena_licence_callbacks.go) applied only when inPlace says the store
// writes backing.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes backing (unsafe.Pointer) which is the destination slice's data pointer.
// Takes s (string) which is the element about to be stored.
// Takes inPlace (bool) which is true when the element is written into backing itself.
//
// Returns the string to store, as materialiseStringForSliceStore.
func materialiseStringForBackingStore(arena *RegisterArena, backing unsafe.Pointer, s string, inPlace bool) string {
	if arena != nil && arenaUsesUnsafeSlabs && !arena.licensed && len(arena.kref.sorted) == 0 {
		return materialiseStringForBackingStoreWithoutLicence(arena, backing, s)
	}
	return materialiseStringForBackingStoreFull(arena, backing, s, inPlace)
}

// materialiseStringForBackingStoreWithoutLicence is materialiseStringForBackingStore for
// an arena without a licence (the abandon fallback's, or a heap-data arena) holding no
// KRef chunk.
//
// Takes arena (*RegisterArena) which is non-nil, not licensed and holds no KRef chunk.
// Takes backing (unsafe.Pointer) which is the destination's backing.
// Takes s (string) which is the string stored.
//
// Returns string which is s or its clone.
func materialiseStringForBackingStoreWithoutLicence(arena *RegisterArena, backing unsafe.Pointer, s string) string {
	if arena.ownsStringBacking(backing) && !arena.mayHoldForeignString(s) {
		return s
	}
	return cloneArenaString(arena, s)
}

// materialiseStringForBackingStoreFull is materialiseStringForBackingStore's full rule,
// for every other arena: the licence's cover and its rules included.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes backing (unsafe.Pointer) which is the destination's backing.
// Takes text (string) which is the string stored.
// Takes inPlace (bool) which is true for a store into an existing element.
//
// Returns string which is text or its clone.
func materialiseStringForBackingStoreFull(arena *RegisterArena, backing unsafe.Pointer, text string, inPlace bool) string {
	if storesStringAsItIs(arena, text) {
		return text
	}
	return materialiseStringForBackingStoreByRules(arena, backing, text, inPlace)
}

// storesStringAsItIs reports whether every rule of
// materialiseStringForBackingStoreByRules stores text as it is, whatever the backing:
// text is not the arena's own string, and is in no arena's byte slab or is one the arena
// does not clone (cloneUnownedStrings off).
//
// Takes arena (*RegisterArena) which may be nil.
// Takes text (string) which is the string stored.
//
// Returns bool which is true when the string needs no barrier.
func storesStringAsItIs(arena *RegisterArena, text string) bool {
	return arena == nil || !arenaUsesUnsafeSlabs || !arena.OwnsString(text) && !arena.inAnArenaSlab(text)
}

// materialiseStringForBackingStoreByRules applies materialiseStringForBackingStoreFull's
// rules to a string storesStringAsItIs did not take.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes backing (unsafe.Pointer) which is the destination's backing.
// Takes text (string) which is the string stored.
// Takes inPlace (bool) which is true for a store into an existing element.
//
// Returns string which is text or its clone.
func materialiseStringForBackingStoreByRules(arena *RegisterArena, backing unsafe.Pointer, text string, inPlace bool) string {
	if arena == nil || !arenaUsesUnsafeSlabs ||
		(arena.ownsStringBacking(backing) && !arena.mayHoldForeignString(text) && !arena.licenceCoveredStringBacking(backing)) {
		return text
	}
	if arena.mayHoldForeignString(text) && (arena.ownUncoveredStringSlot(backing) || inPlace && arena.servedStore(backing, text)) {
		return text
	}
	if krefMemberStringStore(arena, backing, text) {
		return text
	}
	return materialiseStringUnconditional(arena, text)
}

// materialiseStringForIndexedStore applies materialiseStringForSliceStore to a reflect
// collection resolved by resolveIndexCollection. Arrays have no separable backing and
// take the plain barrier.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes collection (reflect.Value) which is the slice or array receiving the element.
// Takes s (string) which is the element about to be stored.
//
// Returns the element to store.
func materialiseStringForIndexedStore(arena *RegisterArena, collection reflect.Value, s string) string {
	if collection.Kind() != reflect.Slice {
		return materialiseString(arena, s)
	}
	return materialiseStringForSliceStore(arena, collection.UnsafePointer(), s)
}

// materialiseSliceForFieldStore is the store barrier for a typed slice written into a
// struct field. A backing this arena owns is copied to the heap, as MaterialiseArenaValue
// does for a general field, unless the receiving struct itself lives in the arena, which
// copies its fields when it escapes.
//
// Takes arena (*RegisterArena) which owns the candidate slabs; nil stores slice as it is.
// Takes base (unsafe.Pointer) which is the receiving struct, or nil when unknown.
// Takes slice (S) which is the slice about to be stored.
//
// Returns slice, or a heap copy of it when its backing is in the arena.
func materialiseSliceForFieldStore[S ~[]E, E any](arena *RegisterArena, base unsafe.Pointer, slice S) S {
	if arena == nil || cap(slice) == 0 || arena.ownsBytePointer(base) || !arena.OwnsSliceBacking(unsafe.Pointer(unsafe.SliceData(slice))) ||
		krefMemberStoreOf(arena, base, true) {
		return slice
	}
	detached, ok := reflect.TypeAssert[S](MaterialiseArenaSliceUnconditional(arena, reflect.ValueOf(slice)))
	if !ok {
		return slice
	}
	return detached
}

// storeSliceFieldBarrier is materialiseSliceForFieldStore for the safe build's
// typed-slice field stores, which write the header through writeSliceFieldSameLayout.
//
// Takes arena (*RegisterArena) which owns the candidate slabs; may be nil.
// Takes field (reflect.Value) which is the destination field.
// Takes slice (S) which is the slice about to be stored.
//
// Returns slice, or a heap copy of it when its backing is in the arena.
func storeSliceFieldBarrier[S ~[]E, E any](arena *RegisterArena, field reflect.Value, slice S) S {
	var base unsafe.Pointer
	if field.CanAddr() {
		base = unsafe.Pointer(field.UnsafeAddr())
	}
	return materialiseSliceForFieldStore(arena, base, slice)
}
