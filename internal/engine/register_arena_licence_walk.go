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

// licenceRegisteredPage reports whether address lies on a page touched by any licensed
// arena slab or by any arena's byte slab, including this arena's storage.
//
// Takes arena (*RegisterArena) which is the walking arena.
// Takes address (uintptr) which is the address.
//
// Returns bool which is false when no registered slab touches address's page.
//
// Panics when the licence page check is enabled and arena-owned storage lies on a page
// absent from the registry.
func licenceRegisteredPage(arena *RegisterArena, address uintptr) bool {
	if slabPagesMayHold(address) {
		return true
	}
	if licencePageCheck {
		if _, class := arena.licenceSlabOf(address); class != licenceSlabNone {
			panic(newInvariantError("licence page check: %#x is in the arena's own storage (class %d) but on no registered page", address, class))
		}
	}
	return false
}

// licenceNonDataSlabOf is licenceSlabOf for an address in none of the arena's data slabs.
//
// Takes address (uintptr) which is the address.
//
// Returns uintptr which is the closure chunk base, or zero for other storage classes.
// Returns licenceSlabClass which identifies a closure chunk, upvalue, register bank or
// unmatched address.
func (a *RegisterArena) licenceNonDataSlabOf(address uintptr) (uintptr, licenceSlabClass) {
	if base, class, ok := a.closureChunkSlabOf(address); ok {
		return base, class
	}
	if _, ok := licenceSlabBase(a.upvalueCellSlab, address); ok {
		return 0, licenceSlabUpvalue
	}
	if _, ok := licenceSlabBase(a.upvalueReferenceSlab, address); ok {
		return 0, licenceSlabUpvalue
	}
	if a.registerBanksHold(address) {
		return 0, licenceSlabBank
	}
	return 0, licenceSlabNone
}

// licenceDataSlabOf finds the current or retired data slab holding p.
//
// It searches byte slabs, generic bytes, slice headers, typed backings, KRef chunks,
// boxes and free chunks.
//
// Takes a (*RegisterArena) which owns the slabs.
// Takes p (uintptr) which is the address.
//
// Returns the slab's start address and true when found.
func licenceDataSlabOf(a *RegisterArena, p uintptr) (uintptr, bool) {
	base, _, ok := licenceDataSlabSpan(a, p)
	return base, ok
}

// licenceDataSlabSpan is licenceDataSlabOf that also returns the slab's end.
//
// Takes arena (*RegisterArena) which owns the slabs.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func licenceDataSlabSpan(arena *RegisterArena, address uintptr) (start, end uintptr, found bool) {
	if base, end, ok := licenceListSpan(arena.byteSlab, arena.oldByteSlabs, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.mutByteSlab, arena.oldMutByteSlabs, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.genericBytesSlab, arena.oldGenericByteSlabs, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(nil, arena.freeGenericByteChunks, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.sliceHeaderSlab, arena.oldSliceHeaderSlabs, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(nil, arena.freeSliceHeaderChunks, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.intBackingSlab, arena.oldIntBackings, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.floatBackingSlab, arena.oldFloatBackings, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.stringBackingSlab, arena.oldStringBackings, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.boolBackingSlab, arena.oldBoolBackings, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceListSpan(arena.uintBackingSlab, arena.oldUintBackings, address); ok {
		return base, end, true
	}
	if chunk, ok := arena.krefFind(address); ok {
		return chunk.base, chunk.end, true
	}
	return licenceBoxSlabSpan(arena, address)
}

// licenceSlabBase reports whether p falls within slab's capacity, which covers every
// element a reslice can reach.
//
// Takes slab ([]T) which is the slab.
// Takes p (uintptr) which is the address.
//
// Returns the slab's start address and true when p is inside it.
func licenceSlabBase[T any](slab []T, p uintptr) (uintptr, bool) {
	base, _, ok := licenceSlabSpan(slab, p)
	return base, ok
}

// licenceListSpan is licenceSlabSpan over a current slab and its retention list.
//
// Takes current ([]T) which is the current slab.
// Takes retired ([][]T) which is the retention list.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func licenceListSpan[T any](current []T, retired [][]T, address uintptr) (start, end uintptr, found bool) {
	if base, end, ok := licenceSlabSpan(current, address); ok {
		return base, end, true
	}
	for _, slab := range retired {
		if base, end, ok := licenceSlabSpan(slab, address); ok {
			return base, end, true
		}
	}
	return 0, 0, false
}

// licenceSlabOf finds what p points into.
//
// Takes p (uintptr) which is the address.
//
// Returns the start address of the data slab holding p, and the class of storage.
func (a *RegisterArena) licenceSlabOf(p uintptr) (uintptr, licenceSlabClass) {
	if base, ok := licenceDataSlabOf(a, p); ok {
		return base, licenceSlabData
	}
	return a.licenceNonDataSlabOf(p)
}

// licenceBoxSlabSpan finds the string or scalar box slab holding address.
//
// Takes arena (*RegisterArena) which owns the slabs.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func licenceBoxSlabSpan(arena *RegisterArena, address uintptr) (start, end uintptr, found bool) {
	if base, end, ok := licenceSlabSpan(arena.stringBoxSlab, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceSlabSpan(arena.intBoxSlab, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceSlabSpan(arena.floatBoxSlab, address); ok {
		return base, end, true
	}
	if base, end, ok := licenceSlabSpan(arena.uintBoxSlab, address); ok {
		return base, end, true
	}
	return licenceSlabSpan(arena.complexBoxSlab, address)
}

// registerBanksHold reports whether address points into a register bank.
//
// Takes address (uintptr) which is the address.
//
// Returns bool which is true for a register-bank address.
func (a *RegisterArena) registerBanksHold(address uintptr) bool {
	return licenceSlabHolds(a.intSlab, address) || licenceSlabHolds(a.floatSlab, address) ||
		licenceSlabHolds(a.stringSlab, address) || licenceSlabHolds(a.generalSlab, address) ||
		licenceSlabHolds(a.boolSlab, address) || licenceSlabHolds(a.uintSlab, address) ||
		licenceSlabHolds(a.complexSlab, address) || licenceSlabHolds(a.slicesIntSlab, address) ||
		licenceSlabHolds(a.slicesFloatSlab, address) || licenceSlabHolds(a.slicesStringSlab, address) ||
		licenceSlabHolds(a.slicesBoolSlab, address) || licenceSlabHolds(a.slicesUintSlab, address) ||
		licenceSlabHolds(a.slicesByteSlab, address)
}

// licenceSlabHolds reports whether p falls within slab's capacity.
//
// Takes slab ([]T) which is the slab.
// Takes p (uintptr) which is the address.
//
// Returns bool which is true when p is inside the slab.
func licenceSlabHolds[T any](slab []T, p uintptr) bool {
	_, ok := licenceSlabBase(slab, p)
	return ok
}
