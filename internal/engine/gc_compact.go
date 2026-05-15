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
	// arenaSliceHeaderBytes is the size in bytes of a slice header tracked per arena slab
	// when reclaiming dead generations.
	arenaSliceHeaderBytes int64 = 24

	// int64BackingBytes is the size in bytes of an int64 backing slot used to compute
	// reclaimed memory for dead generations.
	int64BackingBytes int64 = 8

	// float64BackingBytes is the size in bytes of a float64 backing slot used to compute
	// reclaimed memory for dead generations.
	float64BackingBytes int64 = 8

	// stringBackingBytes is the size in bytes of a string header backing slot used to
	// compute reclaimed memory for dead generations.
	stringBackingBytes int64 = 16

	// uint64BackingBytes is the size in bytes of a uint64 backing slot used to compute
	// reclaimed memory for dead generations.
	uint64BackingBytes int64 = 8
)

// compactPhase consumes mark-phase liveness output to reclaim memory.
//
// Strategy: drop fully-dead OLD slab generations. The current-generation slabs are left
// untouched; their bytes are reused by subsequent bump allocations. This conservative
// strategy avoids the pointer-rewriting machinery that full Cheney-style compaction would
// require, at the cost of not reclaiming dead bytes within an actively-growing slab
// generation.
//
// Dead chunk-sized generic-bytes and slice-header slabs are recycled onto the arena's
// free lists rather than dropped.
//
// Takes state (*gcMarkState) which carries the per-slab liveness bitmaps populated during
// the mark phase.
//
// Returns the number of bytes reclaimed and the subset retained on the free lists.
func (a *RegisterArena) compactPhase(state *gcMarkState) (reclaimed, recycled int64) {
	if state == nil {
		return 0, 0
	}
	reclaimed += compactOldByteSlabs(&a.oldByteSlabs, state.OldByteSlabLive)
	genericReclaimed, genericRecycled := compactOldGenericBytes(&a.oldGenericByteSlabs, state.OldGenericBytesLive, a.genericByteChunkFreeList())
	reclaimed += genericReclaimed
	recycled += genericRecycled
	headerReclaimed, headerRecycled := compactOldSliceHeaders(&a.oldSliceHeaderSlabs, state.OldSliceHeaderLive, a.sliceHeaderChunkFreeList())
	reclaimed += headerReclaimed
	recycled += headerRecycled
	reclaimed += compactOldIntBackings(&a.oldIntBackings, state.OldIntBackingLive)
	reclaimed += compactOldFloatBackings(&a.oldFloatBackings, state.OldFloatBackingLive)
	reclaimed += compactOldStringBackings(&a.oldStringBackings, state.OldStringBackingLive)
	reclaimed += compactOldBoolBackings(&a.oldBoolBackings, state.OldBoolBackingLive)
	reclaimed += compactOldUintBackings(&a.oldUintBackings, state.OldUintBackingLive)
	return reclaimed, recycled
}

// compactOldByteSlabs drops byte slabs from the retention list whose corresponding
// liveness flag is false, preserving relative order of surviving slabs.
//
// Modifies *slabs in place: shifts live entries down and reslices to the live count.
// Dropped slabs become unreachable and Go GC will reclaim their backing.
//
// Takes slabs (*[][]byte) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap from the mark phase.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldByteSlabs(slabs *[][]byte, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab))
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldGenericBytes removes generic-bytes slabs whose liveness flag is false,
// recycling chunk-sized ones onto the free list and dropping the rest.
//
// Takes slabs (*[][]byte) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[][]byte) which is the free list that receives recycled chunks.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactOldGenericBytes(slabs *[][]byte, live []bool, free *[][]byte) (reclaimed, recycled int64) {
	return compactDeadSlabs(slabs, live, free, genericBytesSlabChunk, 1)
}

// compactOldSliceHeaders removes slice-header slabs whose liveness flag is false,
// recycling chunk-sized ones onto the free list and dropping the rest.
//
// Takes slabs (*[][]arenaSliceHeader) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[][]arenaSliceHeader) which is the free list that receives recycled
// chunks.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactOldSliceHeaders(slabs *[][]arenaSliceHeader, live []bool, free *[][]arenaSliceHeader) (reclaimed, recycled int64) {
	return compactDeadSlabs(slabs, live, free, sliceHeaderSlabChunk, arenaSliceHeaderBytes)
}

// compactDeadSlabs is the shared body of the chunked-class compactors: live slabs are
// kept in order, dead chunk-sized slabs are zeroed and moved to the free list while it
// has room, and every other dead slab is dropped for the Go garbage collector.
//
// Takes slabs (*[]S) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[]S) which is the free list that receives recycled chunks.
// Takes chunk (int) which is the class's chunk size in elements.
// Takes elementBytes (int64) which is the byte size of one element.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactDeadSlabs[T any](slabs *[][]T, live []bool, free *[][]T, chunk int, elementBytes int64) (reclaimed, recycled int64) {
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		slabBytes := int64(cap(slab)) * elementBytes
		reclaimed += slabBytes
		if recycleDeadChunk(free, slab, chunk) {
			recycled += slabBytes
		}
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed, recycled
}

// compactOldIntBackings drops int64-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 8 bytes).
//
// Takes slabs (*[][]int64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldIntBackings(slabs *[][]int64, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab)) * int64BackingBytes
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldFloatBackings drops float64-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 8 bytes).
//
// Takes slabs (*[][]float64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldFloatBackings(slabs *[][]float64, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab)) * float64BackingBytes
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldStringBackings drops string-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 16 bytes, the size of a Go string header).
//
// Takes slabs (*[][]string) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldStringBackings(slabs *[][]string, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab)) * stringBackingBytes
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldBoolBackings drops bool-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 1 byte).
//
// Takes slabs (*[][]bool) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldBoolBackings(slabs *[][]bool, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab))
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldUintBackings drops uint64-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 8 bytes).
//
// Takes slabs (*[][]uint64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldUintBackings(slabs *[][]uint64, live []bool) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab)) * uint64BackingBytes
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}
