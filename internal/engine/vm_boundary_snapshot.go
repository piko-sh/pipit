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
	"unsafe"
)

const (
	// boundaryChunkInitialBytes is the byte budget of a type's first boundary-snapshot
	// chunk. A callback VM that allocates one struct literal of a type pays this much, not
	// boundaryChunkSize slots of it: 4 KiB is one page, and the second chunk doubles.
	boundaryChunkInitialBytes = 4096

	// boundaryChunkMaxBytes caps a boundary-snapshot chunk's byte size, matching
	// genericBytesSlabChunk, so a large struct type never allocates a multi-megabyte chunk
	// for its first few values.
	boundaryChunkMaxBytes = 1 << 20
)

// boundaryChunk holds a pre-allocated typed slice used as backing storage for
// boundary-arena struct/array snapshots that need GC-typed scanning.
type boundaryChunk struct {
	// slab is the reflect.MakeSlice([]T, n) backing the chunk's slots; element type T is
	// determined at chunk creation and n by boundaryChunkSlots.
	slab reflect.Value

	// retired holds the slabs of the type's most recent full chunks so ownsBoundarySnapshot
	// keeps recognising slots handed out from chunks that have since overflowed.
	retired []reflect.Value

	// nextIdx is the next free slot index inside slab; on reaching slab.Len() the next
	// acquire allocates a fresh chunk.
	nextIdx int
}

// boundaryChunkSlots returns the slot count of the next boundary-snapshot chunk for an
// element type.
//
// Takes elementSize (uintptr) which is the element type's size; zero-size elements take
// boundaryChunkSize slots because they cost no memory.
// Takes previous (int) which is the slot count of the type's previous chunk, zero for the
// first.
//
// Returns the slot count, at least one.
func boundaryChunkSlots(elementSize uintptr, previous int) int {
	if elementSize == 0 {
		return boundaryChunkSize
	}
	capSlots := max(1, min(boundaryChunkSize, int(boundaryChunkMaxBytes/elementSize)))
	if previous <= 0 {
		return max(1, min(capSlots, int(boundaryChunkInitialBytes/elementSize)))
	}
	return min(capSlots, previous*2)
}

// ownsBoundarySnapshot reports whether p lies inside one of the boundary snapshot chunks,
// whose slots are elements of a pooled slice and therefore never the start of a Go
// allocation.
//
// Takes p (unsafe.Pointer) which is the address to classify.
//
// Returns bool which is true when a chunk holds the address.
func (vm *VM) ownsBoundarySnapshot(p unsafe.Pointer) bool {
	address := uintptr(p)
	for _, chunk := range vm.boundarySnapshotChunks {
		if chunk == nil {
			continue
		}
		if slabHoldsAddress(chunk.slab, address) {
			return true
		}
		for _, retired := range chunk.retired {
			if slabHoldsAddress(retired, address) {
				return true
			}
		}
	}
	return false
}

// slabHoldsAddress reports whether address lies inside slab's backing array.
//
// Takes slab (reflect.Value) which is a slice value, possibly of zero length.
// Takes address (uintptr) which is the address to classify.
//
// Returns bool which is true when the slab's element storage covers the address.
func slabHoldsAddress(slab reflect.Value, address uintptr) bool {
	if slab.Len() == 0 {
		return false
	}
	base := slab.Pointer()
	size := slab.Type().Elem().Size() * uintptr(slab.Len())
	return address >= base && address < base+size
}

// acquireBoundarySnapshot returns a fresh addressable reflect.Value of type t.
//
// Backed by a per-type chunked slab with bump allocation; GC-rooted via the slab's typed
// backing array.
//
// Takes t (reflect.Type) which is the element type of the desired addressable Value;
// selects (and lazily allocates) the per-type chunk.
//
// Returns an addressable reflect.Value of type t backed by the chunk slab.
func (vm *VM) acquireBoundarySnapshot(t reflect.Type) reflect.Value {
	abiTyp := reflectValueABIType(t)
	chunk, ok := vm.boundarySnapshotChunks[abiTyp]
	if !ok || chunk.nextIdx >= chunk.slab.Len() {
		chunk = vm.newBoundaryChunk(t, abiTyp, chunk)
	}
	slot := chunk.slab.Index(chunk.nextIdx)
	chunk.nextIdx++
	return slot
}

// newBoundaryChunk allocates the next boundary-snapshot chunk of type t, sized from the
// chunk it replaces, retires the full chunk's slab into the new chunk's retained list and
// installs the new chunk in the per-type map.
//
// Takes t (reflect.Type) which is the element type.
// Takes abiTyp (unsafe.Pointer) which is t's map key (reflectValueABIType).
// Takes previous (*boundaryChunk) which is the full chunk being replaced, nil for the
// type's first chunk.
//
// Returns the installed chunk with nextIdx zero.
func (vm *VM) newBoundaryChunk(t reflect.Type, abiTyp unsafe.Pointer, previous *boundaryChunk) *boundaryChunk {
	previousSlots := 0
	var retired []reflect.Value
	if previous != nil {
		previousSlots = previous.slab.Len()
		previous.retired = append(previous.retired, previous.slab)
		trimRetainedSlabs(&previous.retired)
		retired = previous.retired
	}
	slots := boundaryChunkSlots(t.Size(), previousSlots)
	chunk := &boundaryChunk{
		slab:    reflect.MakeSlice(reflect.SliceOf(t), slots, slots),
		retired: retired,
		nextIdx: 0,
	}
	if vm.boundarySnapshotChunks == nil {
		vm.boundarySnapshotChunks = make(map[unsafe.Pointer]*boundaryChunk)
	}
	vm.boundarySnapshotChunks[abiTyp] = chunk
	return chunk
}
