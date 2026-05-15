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

const (
	// sliceHeaderSlabChunk caps the slice-header slab at a fixed number of headers. Once the
	// slab reaches this size, growth installs another chunk of the same size instead of
	// doubling, so retired chunks are uniform and can be recycled after a MinorGC finds them
	// dead.
	sliceHeaderSlabChunk = 32768

	// genericBytesSlabChunk caps the generic-bytes slab at a fixed byte size for the same
	// reason as sliceHeaderSlabChunk. A single allocation larger than the chunk still gets
	// an oversized slab, which is never recycled.
	genericBytesSlabChunk = 1 << 20

	// maxFreeSlabChunks caps how many dead chunks the arena keeps per class for reuse.
	maxFreeSlabChunks = maxRetainedOldSlabsPerType
)

// chargeChunkedSlab charges a freshly made slab in a chunked class. Below the chunk size
// it charges the delta over the retired slab, at or above it charges the full amount.
//
// Takes newBytes (uint64) which is the byte capacity of the new slab.
// Takes oldBytes (uint64) which is the byte capacity of the retired slab.
// Takes chunkBytes (uint64) which is the byte size of one chunk.
func (a *RegisterArena) chargeChunkedSlab(newBytes, oldBytes, chunkBytes uint64) {
	if oldBytes >= chunkBytes {
		a.ChargeArenaAllocation(newBytes, 0)
		return
	}
	a.ChargeArenaAllocation(newBytes, oldBytes)
}

// dropFreeChunks releases both free lists so the Go garbage collector can reclaim the
// recycled chunks. Called from Reset alongside dropRetainedOldSlabs.
func (a *RegisterArena) dropFreeChunks() {
	clear(a.freeSliceHeaderChunks)
	a.freeSliceHeaderChunks = a.freeSliceHeaderChunks[:0]
	clear(a.freeGenericByteChunks)
	a.freeGenericByteChunks = a.freeGenericByteChunks[:0]
}

// sliceHeaderChunkFreeList returns the slice-header free list when chunk recycling is
// enabled for this arena, or nil so dead chunks are dropped.
//
// Returns the free list pointer or nil.
func (a *RegisterArena) sliceHeaderChunkFreeList() *[][]arenaSliceHeader {
	if !a.recycleChunks {
		return nil
	}
	return &a.freeSliceHeaderChunks
}

// genericByteChunkFreeList returns the generic-bytes free list when chunk recycling is
// enabled for this arena, or nil so dead chunks are dropped.
//
// Returns the free list pointer or nil.
func (a *RegisterArena) genericByteChunkFreeList() *[][]byte {
	if !a.recycleChunks {
		return nil
	}
	return &a.freeGenericByteChunks
}

// chunkedSlabCapacity computes the capacity for the next slab in a chunked class. Growth
// doubles until the chunk size and then stays there, so steady-state retirements produce
// uniform recyclable chunks.
//
// Takes current (int) which is the length of the slab being retired.
// Takes minCap (int) which is the smallest capacity the caller needs.
// Takes initial (int) which is the class's initial capacity.
// Takes chunk (int) which is the class's chunk size.
//
// Returns the capacity of the replacement slab.
func chunkedSlabCapacity(current, minCap, initial, chunk int) int {
	if minCap > chunk {
		return minCap
	}
	return min(max(current*2, minCap, initial), chunk)
}

// popFreeChunk takes a recycled chunk off a free list when the requested capacity is
// exactly the chunk size. Recycled chunks are already zeroed and already counted in the
// arena's working set, so the caller installs one without charging the budget.
//
// Takes free (*[][]T) which is the free list to pop from.
// Takes wantCap (int) which is the capacity the caller is about to allocate.
// Takes chunk (int) which is the class's chunk size.
//
// Returns the recycled chunk and true, or nil and false when none is usable.
func popFreeChunk[T any](free *[][]T, wantCap, chunk int) ([]T, bool) {
	list := *free
	if wantCap != chunk || len(list) == 0 {
		return nil, false
	}
	last := len(list) - 1
	recycled := list[last]
	list[last] = nil
	*free = list[:last]
	return recycled, true
}

// recycleDeadChunk clears a dead chunk-sized slab and pushes it onto a free list when
// there is room. Clearing is required so stale Data pointers stop retaining backings
// while the chunk waits for reuse.
//
// Takes free (*[][]T) which is the free list to push onto.
// Takes slab ([]T) which is the dead slab.
// Takes chunk (int) which is the class's chunk size.
//
// Returns true when the slab was recycled, false when it must be dropped instead.
func recycleDeadChunk[T any](free *[][]T, slab []T, chunk int) bool {
	if free == nil || cap(slab) != chunk || len(*free) >= maxFreeSlabChunks {
		return false
	}
	slab = slab[:chunk]
	clear(slab)
	*free = append(*free, slab)
	return true
}
