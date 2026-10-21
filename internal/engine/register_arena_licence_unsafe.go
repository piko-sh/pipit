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
	"weak"
)

const (
	// licenceWalkDepth bounds the nesting the revocation walk follows.
	licenceWalkDepth = 128

	// licenceExactProbes is true where the arena's ownership probes compare real slab
	// bounds, so a revocation can name the slab a hand-off references.
	licenceExactProbes = true

	// abiTypePtrBytesOffset is the byte offset of abi.Type.PtrBytes, which follows Size_.
	abiTypePtrBytesOffset = unsafe.Sizeof(uintptr(0))
)

// arenaLicenceExactState is the part of arenaLicenceState that only builds with exact
// probes use: the walk's buffers and the licence registry's publication buffers.
type arenaLicenceExactState struct {
	// pages holds, for each slab in revoked that is revoked page by page, its revoked pages
	// (register_arena_licence_pages.go); a slab in revoked without an entry is revoked
	// whole.
	pages map[uintptr]*revokedPageSet

	// typedPlanABI and typedPlan are the typed walk's last plan lookup that missed its
	// walk's own, so the arena's walks of one type look it up in the cache once.
	typedPlanABI unsafe.Pointer

	// typedPlan is the plan of typedPlanABI's type.
	typedPlan *licenceTypedPlan

	// published is what publishLicenceSlabs last recorded in the licence registry, sorted.
	// It is always the sorted form of gathered (advancePublication).
	published []licenceSlabRange

	// publishSpare is the buffer the next publication is built in.
	publishSpare []licenceSlabRange

	// gathered is the arena's ranges as the last publication gathered them, unsorted. Equal
	// gatherings need no sort.
	gathered []licenceSlabRange

	// gatherSpare is the buffer the next gathering fills.
	gatherSpare []licenceSlabRange

	// windowBefore receives, sorted, the part of the previous gathering that differs from
	// the new one, when a publication merges its difference into published.
	windowBefore []licenceSlabRange

	// windowAfter receives, sorted, the part of the new gathering that differs from the
	// previous one, when a publication merges its difference into published.
	windowAfter []licenceSlabRange

	// droppedScratch receives the starts a publication drops.
	droppedScratch []uintptr

	// addedScratch receives a publication's ranges that are new, sorted by start (the
	// sharded registry applies only the difference).
	addedScratch []licenceSlabRange

	// removedScratch receives the previous publication's ranges the arena no longer holds,
	// sorted by start.
	removedScratch []licenceSlabRange

	// adoptScratch receives the arena's ranges while it adopts foreign revocations.
	adoptScratch []licenceSlabRange

	// pageSetsFree holds page sets earlier epochs freed, for reuse.
	pageSetsFree []*revokedPageSet

	// keptRuns bounds the runs of clean pages the latest release kept as slabs, for the
	// reuse audit.
	keptRuns []keptPageRun

	// closureScanned records the reference-bearing slabs closeOverRevoked has scanned.
	closureScanned []uintptr

	// closureSpans is the sorted, disjoint table of the arena's data slabs that one release
	// closure searches instead of licenceDataSlabSpan's lists; closureSpansOK is true only
	// while a closure runs with a usable table.
	closureSpans []licenceDataSpan

	// scratch holds the values the walks copy map keys and elements into.
	scratch []licenceScratchSlot

	// walkFresh is the buffer a shared walk records its newly revoked slabs in.
	walkFresh []uintptr

	// wrapperSeen is the buffer the walks record the closures and cells they visited in, so
	// each is walked once.
	wrapperSeen []unsafe.Pointer

	// memo records the Go-heap containers the epoch's walks have followed to the end.
	memo licenceWalkMemo

	// publishMerged counts the publications that merged their difference into published.
	publishMerged uint64

	// publishSorted counts the publications that fell back to sorting the whole gathering.
	publishSorted uint64

	// closureSpansOK is true while closeOverRevoked runs with closureSpans built.
	closureSpansOK bool
}

// licenceSlabRange is one slab published in the licence registry.
type licenceSlabRange struct {
	// live points weakly at the arena that holds the slab (RegisterArena.slabLive). A lookup
	// ignores the range once that arena is unreachable (slabHolderLive).
	live weak.Pointer[RegisterArena]

	// start is the slab's first byte address.
	start uintptr

	// end is one past the slab's last byte address, by capacity.
	end uintptr

	// owner identifies the arena that holds the slab (RegisterArena.slabOwner).
	owner uint64

	// class records whether revoking the slab affects that slab alone or the whole epoch.
	class licenceSlabClass
}

// licenceTypeHoldsNoPointers reports whether values of t contain no pointers, so the
// revocation walk has nothing to follow inside them. It reads the runtime's own answer,
// abi.Type.PtrBytes, rather than consulting the typeIsPointerFree cache, which costs a
// map lookup on every element and field the walk visits.
//
// Takes t (reflect.Type) which is the type; nil holds pointers as far as the walk knows.
//
// Returns bool which is true when the type's pointer-bearing prefix is empty.
func licenceTypeHoldsNoPointers(t reflect.Type) bool {
	if t == nil {
		return false
	}
	return *(*uintptr)(unsafe.Add(reflectValueABIType(t), abiTypePtrBytesOffset)) == 0
}

// licenceReadable returns v without its read-only flag, so the walk can iterate a map it
// reached through an unexported field with MapIter.SetIterKey, which refuses a read-only
// map. The walk only reads through the result.
//
// Takes v (reflect.Value) which is the value.
//
// Returns reflect.Value which is v, readable through every reflect accessor.
func licenceReadable(v reflect.Value) reflect.Value {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&v))
	raw.flag &^= licenceFlagRO
	return v
}

// licenceKeepReadOnly returns copied carrying v's read-only flags, so a copy of a value
// read through an unexported field (made from licenceReadable(v)) stays as read-only as v
// was.
//
// Takes copied (reflect.Value) which is the copy.
// Takes v (reflect.Value) which is the original.
//
// Returns reflect.Value which is copied with v's read-only flags.
func licenceKeepReadOnly(copied, v reflect.Value) reflect.Value {
	source := (*unsafeReflectValue)(unsafe.Pointer(&v))
	target := (*unsafeReflectValue)(unsafe.Pointer(&copied))
	target.flag |= source.flag & licenceFlagRO
	return copied
}

// licenceSliceElement returns element index of the slice whose first element is at data,
// addressable as reflect.Value.Index would return it, without the heap allocation that
// re-slicing to the capacity costs.
//
// Takes elementType (unsafe.Pointer) which is the element's *abi.Type.
// Takes kind (reflect.Kind) which is the element's kind.
// Takes size (uintptr) which is the element's size.
// Takes data (unsafe.Pointer) which is the backing's first element.
// Takes index (int) which is below the slice's capacity.
//
// Returns reflect.Value which is the element.
func licenceSliceElement(elementType unsafe.Pointer, kind reflect.Kind, size uintptr, data unsafe.Pointer, index int) reflect.Value {
	return unsafeNewAt(elementType, unsafe.Add(data, uintptr(index)*size), kind)
}

// reflectValueHeld returns the reflect.Value stored in value, a value of type
// reflect.Value, so walks follow what it holds rather than its internal fields.
//
// Takes value (reflect.Value) which holds a reflect.Value.
//
// Returns reflect.Value which is the held value, or the zero Value when it cannot be
// read.
func reflectValueHeld(value reflect.Value) reflect.Value {
	if value.CanInterface() {
		held, _ := reflect.TypeAssert[reflect.Value](value)
		return held
	}
	if pointer := ReflectValuePtr(value); pointer != nil && value.Kind() == reflect.Struct {
		raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
		if raw.flag&flagIndir != 0 {
			return *(*reflect.Value)(pointer)
		}
	}
	return reflect.Value{}
}
