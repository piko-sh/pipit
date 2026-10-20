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

// MaterialiseStringForArenaAppend applies the string store barrier for
// arenaAppendString. Foreign arena strings are cloned even into local arena storage
// because the escape walk only recognises local strings.
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
	return materialiseStringForSliceStore(arena, unsafe.Pointer(unsafe.SliceData(source)), s)
}

// MaterialiseStringForFieldStore applies the string store barrier when writing through
// a struct's raw base pointer.
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
	if arena.OwnsString(s) || arena.inAnArenaSlab(s) {
		return strings.Clone(s)
	}
	return s
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
// Returns a heap-cloned copy when s is arena-backed and the destination is not, or s
// unchanged.
func materialiseStringForSliceStore(arena *RegisterArena, backing unsafe.Pointer, s string) string {
	if arena == nil || !arenaUsesUnsafeSlabs || (arena.ownsStringBacking(backing) && !arena.mayHoldForeignString(s)) {
		return s
	}
	return materialiseStringUnconditional(arena, s)
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
