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

// MaterialiseStringForArenaAppend is the store barrier for an element appended with
// arenaAppendString, whose backing ownership is sticky: an arena-born slice grows in the
// arena and a heap-born one on the heap, so the ownership of source's backing decides in
// every case except an empty slice, which always starts in the arena.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes source ([]string) which is the slice being appended to.
// Takes s (string) which is the element about to be appended.
//
// Returns the element to append.
func MaterialiseStringForArenaAppend(arena *RegisterArena, source []string, s string) string {
	if cap(source) == 0 {
		return s
	}
	return materialiseStringForSliceStore(arena, unsafe.Pointer(unsafe.SliceData(source)), s)
}

// MaterialiseStringForFieldStore is the store barrier for a string written into a struct
// field through a raw base pointer. The clone is skipped when the struct itself lives in
// the arena's byte slab: an arena struct dies with the arena and its string fields are
// materialised by materialiseArenaCompositeFields when it escapes.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes base (unsafe.Pointer) which is the address of the receiving struct.
// Takes s (string) which is the value about to be stored.
//
// Returns a heap-cloned copy when s is arena-backed and the struct is not, or s
// unchanged.
func MaterialiseStringForFieldStore(arena *RegisterArena, base unsafe.Pointer, s string) string {
	if arena == nil || !arenaUsesUnsafeSlabs || arena.ownsBytePointer(base) {
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

// materialiseStringUnconditional heap-clones s when it points into the arena in every
// build. Used by boundary paths (return values, panic captures, closure capture) that
// must detach a string even where the safe build's store barrier is folded away.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes s (string) which is the candidate string.
//
// Returns a heap-cloned copy when arena-backed, or s unchanged.
func materialiseStringUnconditional(arena *RegisterArena, s string) string {
	if arena.OwnsString(s) {
		return strings.Clone(s)
	}
	return s
}

// materialiseStringForSliceStore is the store barrier for a string element written into a
// []string backing. The clone is skipped when the backing is itself arena-owned: such a
// slice dies with the arena, its elements are roots for the arena collector, and the
// escape boundary re-copies it with materialised elements
// (MaterialiseArenaSliceUnconditional).
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes backing (unsafe.Pointer) which is the destination slice's data pointer.
// Takes s (string) which is the element about to be stored.
//
// Returns a heap-cloned copy when s is arena-backed and the destination is not, or s
// unchanged.
func materialiseStringForSliceStore(arena *RegisterArena, backing unsafe.Pointer, s string) string {
	if arena == nil || !arenaUsesUnsafeSlabs || arena.ownsStringBacking(backing) {
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
