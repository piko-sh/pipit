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
	"slices"
	"unsafe"
	"weak"

	"piko.sh/safeconv"
)

// arenaKRef is an arena's KRef chunk class.
type arenaKRef struct {
	// classes maps an element type's ABI type to its chunks.
	classes map[unsafe.Pointer]*krefClass

	// sorted holds every chunk by base address, for krefFind.
	sorted []*krefChunk

	// pointeeSites records the epoch's reference-holding pointee placements
	// (register_arena_kref_placement.go).
	pointeeSites []pointeeSite

	// used is true once the epoch took a slot.
	used bool
}

const (
	// krefMaxClasses bounds the element types an arena keeps KRef chunks for. A further type
	// takes the heap path.
	krefMaxClasses = 64

	// krefChunkInitialBytes is the byte budget of a type's first chunk, as for the
	// boundary-snapshot chunks it replaces; later chunks double.
	krefChunkInitialBytes = 4096

	// krefChunkMaxBytes caps a chunk's byte size.
	krefChunkMaxBytes = 1 << 22

	// krefRetainClassBytes bounds the chunk bytes Reset keeps for one type.
	krefRetainClassBytes = 1 << 20

	// krefRetainBytes bounds the chunk bytes Reset keeps for the arena.
	krefRetainBytes = 4 << 20

	// krefIdleResets is the number of resets a type may go unused before Reset evicts its
	// chunks.
	krefIdleResets = 8
)

// krefChunk is a typed slab of slots bump-allocated within an epoch.
type krefChunk struct {
	// class is the chunk's element-type class.
	class *krefClass

	// slab is the chunk's []T, length and capacity slots.
	slab reflect.Value

	// base is the address of the first slot, the chunk's key in the licence.
	base uintptr

	// end is one past the last slot's last byte.
	end uintptr

	// used is the number of slots handed out since the last Reset.
	used int
}

// krefClass is the KRef chunks of one element type.
type krefClass struct {
	// elementType is the slots' type.
	elementType reflect.Type

	// chunks holds the type's chunks in allocation order; the epoch fills them in order.
	chunks []*krefChunk

	// current is the index of the chunk the next slot comes from.
	current int

	// idle counts the resets since the type was last used.
	idle int

	// used is true once the epoch took a slot of the type.
	used bool

	// memberVetoed records whether release detached a revoked chunk of its element type.
	memberVetoed bool

	// snapshotPlaced is true once the epoch placed a struct literal snapshot of the type.
	snapshotPlaced bool

	// snapshotVetoed is true once an epoch that placed struct literal snapshots of the type
	// ran out of its walk budget (settleSnapshotPlacements).
	snapshotVetoed bool

	// lastUsed records the slots used in the epoch whose release dropped the class's chunks.
	lastUsed int
}

// krefPlaces reports whether a licensed arena can place values in KRef chunks.
//
// Returns bool which is true when KRef placement applies.
func (a *RegisterArena) krefPlaces() bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && a != nil && a.licensed && !a.heapData
}

// krefClassFor returns the chunk class of elementType, making it when the cap allows.
//
// Takes elementType (reflect.Type) which is the slots' type, of non-zero size.
//
// Returns *krefClass which is nil when the class cap is reached.
func (a *RegisterArena) krefClassFor(elementType reflect.Type) *krefClass {
	key := reflectValueABIType(elementType)
	if class, found := a.kref.classes[key]; found {
		return class
	}
	if len(a.kref.classes) >= krefMaxClasses {
		return nil
	}
	if a.kref.classes == nil {
		a.kref.classes = make(map[unsafe.Pointer]*krefClass)
	}
	class := &krefClass{elementType: elementType, chunks: nil, current: 0, idle: 0, used: false, memberVetoed: false, snapshotPlaced: false, snapshotVetoed: false, lastUsed: 0}
	a.kref.classes[key] = class
	return class
}

// krefSlot hands out one zeroed, addressable slot of type valueType from the arena's KRef
// chunks.
//
// Takes valueType (reflect.Type) which is the slot's type.
//
// Returns reflect.Value which is the slot.
// Returns bool which is false when the class refused (KRef placement does not apply, the
// cap is reached, or the type has zero size), so the caller takes its heap path.
func (a *RegisterArena) krefSlot(valueType reflect.Type) (reflect.Value, bool) {
	chunk, index, ok := a.krefTake(valueType, 1)
	if !ok {
		return reflect.Value{}, false
	}
	return chunk.slab.Index(index), true
}

// krefRun hands out count contiguous zeroed slots of element type elementType, as the
// backing of a slice of capacity count.
//
// Takes elementType (reflect.Type) which is the element type.
// Takes count (int) which is the number of slots, greater than zero.
//
// Returns unsafe.Pointer which is the first slot.
// Returns bool which is false when the class refused.
func (a *RegisterArena) krefRun(elementType reflect.Type, count int) (unsafe.Pointer, bool) {
	if a.krefPlaces() {
		a.enforceSingleAllocLimit(count)
	}
	chunk, index, ok := a.krefTake(elementType, count)
	if !ok {
		return nil, false
	}
	return chunk.slab.Index(index).Addr().UnsafePointer(), true
}

// krefTake finds count free slots of type valueType in the current or a later chunk of
// valueType's class, or makes a chunk for them, and marks them used.
//
// Takes valueType (reflect.Type) which is the slots' type.
// Takes count (int) which is the number of slots.
//
// Returns the chunk, the first slot's index and true, or false when the class refused.
func (a *RegisterArena) krefTake(valueType reflect.Type, count int) (*krefChunk, int, bool) {
	size := valueType.Size()
	if !a.krefPlaces() || size == 0 || count <= 0 || uintptr(count) > krefChunkMaxBytes/size {
		if licenceCounting {
			krefChunkCounters.refused.Add(1)
		}
		return nil, 0, false
	}
	class := a.krefClassFor(valueType)
	if class == nil {
		if licenceCounting {
			krefChunkCounters.refused.Add(1)
		}
		return nil, 0, false
	}
	for class.current < len(class.chunks) {
		chunk := class.chunks[class.current]
		if chunk.slots()-chunk.used >= count {
			return a.krefMark(class, chunk, count), chunk.used - count, true
		}
		class.current++
	}
	chunk := a.krefNewChunk(class, count)
	return a.krefMark(class, chunk, count), chunk.used - count, true
}

// krefMark marks count slots of chunk used.
//
// Takes class (*krefClass) which owns the chunk.
// Takes chunk (*krefChunk) which has count free slots.
// Takes count (int) which is the number of slots.
//
// Returns *krefChunk which is chunk.
func (a *RegisterArena) krefMark(class *krefClass, chunk *krefChunk, count int) *krefChunk {
	chunk.used += count
	class.used = true
	a.kref.used = true
	if licenceCounting {
		krefChunkCounters.slots.Add(1)
	}
	return chunk
}

// krefNewChunk makes the class's next chunk, large enough for count slots, and publishes
// it to the licence registry before any value can reference it.
//
// Takes class (*krefClass) which receives the chunk.
// Takes count (int) which is the number of slots it must hold.
//
// Returns *krefChunk which is the new, current chunk.
func (a *RegisterArena) krefNewChunk(class *krefClass, count int) *krefChunk {
	previous := 0
	if len(class.chunks) != 0 {
		previous = class.chunks[len(class.chunks)-1].slots()
	}
	size := class.elementType.Size()
	slots := max(count, krefChunkSlots(size, previous))
	if previous == 0 && class.lastUsed > 0 {
		slots = max(count, min(slots, 2*class.lastUsed))
	}
	slots = a.fitGrow(slots, count, uint64(size), 0, 0)
	chunkBytes := safeconv.IntToUint64(slots) * uint64(size)
	a.admitGrow(chunkBytes, 0)
	slab := reflect.MakeSlice(reflect.SliceOf(class.elementType), slots, slots)
	base := slab.Pointer()
	chunk := &krefChunk{slab: slab, base: base, end: base + uintptr(slots)*size, class: class, used: 0}
	class.chunks = append(class.chunks, chunk)
	class.current = len(class.chunks) - 1
	at, _ := slices.BinarySearchFunc(a.kref.sorted, base, func(c *krefChunk, base uintptr) int {
		return compareUintptr(c.base, base)
	})
	a.kref.sorted = slices.Insert(a.kref.sorted, at, chunk)
	if licenceCounting {
		krefChunkCounters.chunks.Add(1)
	}
	a.publishLicenceSlabs()
	a.ChargeArenaAllocation(chunkBytes, 0)
	return chunk
}

// krefChunkSlots returns a type's next chunk size in slots.
//
// Takes size (uintptr) which is the element size, greater than zero.
// Takes previous (int) which is the slot count of the type's previous chunk, or zero.
//
// Returns int which is at least one.
func krefChunkSlots(size uintptr, previous int) int {
	capSlots := max(1, min(boundaryChunkSize, int(krefChunkMaxBytes/size)))
	if previous <= 0 {
		return max(1, min(capSlots, int(krefChunkInitialBytes/size)))
	}
	return min(capSlots, previous*2)
}

// krefContains reports whether pointer lies in one of the arena's KRef chunks.
//
// Takes pointer (unsafe.Pointer) which is the address; nil answers false.
//
// Returns bool which is true for a KRef address.
func (a *RegisterArena) krefContains(pointer unsafe.Pointer) bool {
	if pointer == nil || len(a.kref.sorted) == 0 {
		return false
	}
	_, found := a.krefFind(uintptr(pointer))
	return found
}

// appendKRefRanges appends a licence range for every KRef chunk, in address order. Every
// chunk must be published, or a walk would take it for Go heap.
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
//
// Returns []licenceSlabRange which is out extended.
func (a *RegisterArena) appendKRefRanges(out []licenceSlabRange, owner uint64) []licenceSlabRange {
	for _, chunk := range a.kref.sorted {
		out = append(out, licenceSlabRange{live: weak.Pointer[RegisterArena]{}, start: chunk.base, end: chunk.end, owner: owner, class: licenceSlabData})
	}
	return out
}

// krefDropRevoked detaches every revoked KRef chunk, uncleared, to the collector,
// counting the outcome, and keeps the rest.
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) krefDropRevoked(tally *licenceReleaseTally) {
	if len(a.kref.sorted) == 0 {
		return
	}
	dropped := false
	for _, class := range a.kref.classes {
		class.lastUsed = 0
		write := 0
		for _, chunk := range class.chunks {
			cause, revoked := a.licenceState.revoked[chunk.base]
			if !revoked {
				tally.recycled++
				class.chunks[write] = chunk
				write++
				continue
			}
			tally.abandoned++
			tally.causes[cause] = true
			dropped = true
			class.lastUsed += chunk.used
			if licenceCounting {
				krefChunkCounters.detached.Add(1)
			}
			class.memberVetoed = true
		}
		clear(class.chunks[write:])
		class.chunks = class.chunks[:write]
		class.current = min(class.current, max(len(class.chunks)-1, 0))
	}
	if dropped {
		a.krefRebuildSorted()
	}
}

// krefDropAll leaves every KRef chunk uncleared for the collector after epoch revocation,
// abandonment, a heap-data drop or replacement by the reuse audit.
//
// The empty classes remain available.
func (a *RegisterArena) krefDropAll() {
	if len(a.kref.sorted) == 0 {
		return
	}
	if licenceCounting {
		krefChunkCounters.detached.Add(int64(len(a.kref.sorted)))
	}
	for _, class := range a.kref.classes {
		class.lastUsed = 0
		for _, chunk := range class.chunks {
			class.lastUsed += chunk.used
		}
		clear(class.chunks)
		class.chunks = class.chunks[:0]
		class.current = 0
	}
	clear(a.kref.sorted)
	a.kref.sorted = a.kref.sorted[:0]
}

// krefRebuildSorted rebuilds the address index from the classes.
func (a *RegisterArena) krefRebuildSorted() {
	sorted := a.kref.sorted[:0]
	for _, class := range a.kref.classes {
		sorted = append(sorted, class.chunks...)
	}
	clear(a.kref.sorted[len(sorted):cap(a.kref.sorted)])
	slices.SortFunc(sorted, func(x, y *krefChunk) int { return compareUintptr(x.base, y.base) })
	a.kref.sorted = sorted
}

// krefReset is Reset's part for the KRef class.
//
// The used prefix of every kept chunk is typed-cleared, the classes start again at their
// first chunk, and the retention bounds are applied, evicting (detaching) what they do
// not keep.
//
// It runs after the release has detached every revoked chunk, so every chunk it clears is
// clean.
func (a *RegisterArena) krefReset() {
	a.kref.used = false
	clear(a.kref.pointeeSites)
	a.kref.pointeeSites = a.kref.pointeeSites[:0]
	if len(a.kref.classes) == 0 {
		return
	}
	evicted := false
	total := 0
	for key, class := range a.kref.classes {
		if class.used {
			class.idle = 0
		} else {
			class.idle++
		}
		class.used = false
		class.current = 0
		if class.idle > krefIdleResets {
			if licenceCounting {
				krefChunkCounters.detached.Add(int64(len(class.chunks)))
			}
			evicted = evicted || len(class.chunks) != 0
			delete(a.kref.classes, key)
			continue
		}
		var detached bool
		total, detached = class.retainChunks(total)
		evicted = evicted || detached
	}
	if evicted {
		a.krefRebuildSorted()
	}
}

// retainChunks clears and keeps the class's chunks while they fit the retention bounds,
// and detaches the rest.
//
// Takes total (int) which is the bytes the arena's earlier classes kept.
//
// Returns kept (int) which is total plus the bytes this class kept.
// Returns detached (bool) which is true when a chunk was detached.
func (class *krefClass) retainChunks(total int) (kept int, detached bool) {
	write, classBytes := 0, 0
	for _, chunk := range class.chunks {
		size := chunk.bytes()
		if classBytes+size > krefRetainClassBytes || total+size > krefRetainBytes {
			if licenceCounting {
				krefChunkCounters.detached.Add(1)
			}
			detached = true
			continue
		}
		if chunk.used != 0 {
			chunk.slab.Slice(0, chunk.used).Clear()
			chunk.used = 0
		}
		classBytes += size
		total += size
		class.chunks[write] = chunk
		write++
		if licenceCounting {
			krefChunkCounters.reused.Add(1)
		}
	}
	clear(class.chunks[write:])
	class.chunks = class.chunks[:write]
	return total, detached
}

// slots returns the chunk's slot count.
//
// Returns int which is the count.
func (c *krefChunk) slots() int { return c.slab.Len() }

// bytes returns the chunk's size in bytes.
//
// Returns int which is the size.
func (c *krefChunk) bytes() int { return int(c.end - c.base) }

// compareUintptr orders two addresses.
//
// Takes left (uintptr) which is the first address.
// Takes right (uintptr) which is the second address.
//
// Returns int which is negative, zero or positive.
func compareUintptr(left, right uintptr) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

// krefFind returns the KRef chunk holding address.
//
// Takes address (uintptr) which is the address.
//
// Returns *krefChunk which is the chunk.
// Returns bool which is true when found.
func (a *RegisterArena) krefFind(address uintptr) (*krefChunk, bool) {
	sorted := a.kref.sorted
	if len(sorted) == 0 || address < sorted[0].base {
		return nil, false
	}
	low, high := 0, len(sorted)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if sorted[middle].base <= address {
			low = middle + 1
		} else {
			high = middle
		}
	}
	chunk := sorted[low-1]
	if address >= chunk.end {
		return nil, false
	}
	return chunk, true
}
