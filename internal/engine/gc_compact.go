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

import "piko.sh/safeconv"

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
// The collector drops fully dead old slab generations. It leaves current-generation slabs
// untouched so later bump allocations can reuse their bytes.
//
// Dead chunk-sized generic-bytes and slice-header slabs are recycled onto the arena's
// free lists rather than dropped.
//
// A mark that failed closed, or was skipped, vouches for no slab, so the collector then
// reclaims nothing, with or without a licence.
//
// Takes state (*gcMarkState) which carries the per-slab liveness bitmaps populated during
// the mark phase.
//
// Returns the number of bytes reclaimed and the subset retained on the free lists.
func (a *RegisterArena) compactPhase(state *gcMarkState) (reclaimed, recycled int64) {
	if state == nil {
		return 0, 0
	}
	if a.licensed {
		if licenceCounting {
			a.stats().compactions.Add(1)
		}
		a.vetoRevokedBytePlacements()
	}
	if state.failClosed != 0 {
		if a.licensed && licenceCounting {
			a.stats().compactionsFailedClosed[state.failClosed].Add(1)
		}
		return 0, 0
	}
	if a.licensed {
		a.adoptForeignRevocations()
		a.applyBlindHandOffs()
		defer a.publishLicenceSlabs()
	}
	genericFree, headerFree := a.genericByteChunkFreeList(), a.sliceHeaderChunkFreeList()
	if arenaAuditEnabled {
		finishAuditPhase := a.auditCompactPhase(state)
		defer finishAuditPhase()
		genericFree, headerFree = nil, nil
	}
	reclaimed += compactOldByteSlabs(a, &a.oldByteSlabs, state.OldByteSlabLive, &state.droppedCharge)
	state.mutBytesDropped, state.mutChargeDropped = compactOldMutByteSlabs(a, &a.oldMutByteSlabs, state.OldMutByteSlabLive)
	reclaimed += state.mutBytesDropped
	state.droppedCharge += state.mutChargeDropped
	genericReclaimed, genericRecycled := compactOldGenericBytes(a, &a.oldGenericByteSlabs, state.OldGenericBytesLive, genericFree, &state.droppedCharge)
	reclaimed += genericReclaimed
	recycled += genericRecycled
	headerReclaimed, headerRecycled := compactOldSliceHeaders(a, &a.oldSliceHeaderSlabs, state.OldSliceHeaderLive, headerFree, &state.droppedCharge)
	reclaimed += headerReclaimed
	recycled += headerRecycled
	reclaimed += compactOldIntBackings(a, &a.oldIntBackings, state.OldIntBackingLive, &state.droppedCharge)
	reclaimed += compactOldFloatBackings(a, &a.oldFloatBackings, state.OldFloatBackingLive, &state.droppedCharge)
	reclaimed += compactOldStringBackings(a, &a.oldStringBackings, state.OldStringBackingLive, &state.droppedCharge)
	reclaimed += compactOldBoolBackings(a, &a.oldBoolBackings, state.OldBoolBackingLive, &state.droppedCharge)
	reclaimed += compactOldUintBackings(a, &a.oldUintBackings, state.OldUintBackingLive, &state.droppedCharge)
	return reclaimed, recycled
}

// compactOldByteSlabs drops byte slabs from the retention list whose corresponding
// liveness flag is false, preserving relative order of surviving slabs.
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]byte) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap from the mark phase.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldByteSlabs(arena *RegisterArena, slabs *[][]byte, live []bool, released *uint64) int64 {
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
		*released += takeSlabCharge(arena, slab, 1)
		forgetArenaSlab(slab)
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}

// compactOldMutByteSlabs drops each retired mutable byte slab the mark found no live
// value reaching, leaving it to the Go collector, and never reuses one.
//
// A collection whose mark failed closed compacts nothing.
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]byte) which is the retention list, compacted in place.
// Takes live ([]bool) which is the per-slab liveness bitmap (OldMutByteSlabLive).
//
// Returns the dropped slabs' capacity in bytes and the budget charge they carry.
func compactOldMutByteSlabs(arena *RegisterArena, slabs *[][]byte, live []bool) (dropped int64, charge uint64) {
	before := len(*slabs)
	dropped = compactRetiredSlabs(arena, slabs, live, 1, &charge)
	if licenceCounting && before != len(*slabs) {
		mutableByteSlabsDropped.Add(int64(before - len(*slabs)))
		mutableByteBytesDropped.Add(dropped)
	}
	return dropped, charge
}

// compactOldGenericBytes removes generic-bytes slabs whose liveness flag is false,
// recycling chunk-sized ones onto the free list and dropping the rest.
//
// Takes a (*RegisterArena) which owns the slabs and their licence.
// Takes slabs (*[][]byte) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[][]byte) which is the free list that receives recycled chunks.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactOldGenericBytes(a *RegisterArena, slabs *[][]byte, live []bool, free *[][]byte, released *uint64) (reclaimed, recycled int64) {
	return compactDeadSlabs(a, slabs, live, free, genericBytesSlabChunk, 1, released)
}

// compactOldSliceHeaders removes slice-header slabs whose liveness flag is false,
// recycling chunk-sized ones onto the free list and dropping the rest.
//
// Takes a (*RegisterArena) which owns the slabs and their licence.
// Takes slabs (*[][]arenaSliceHeader) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[][]arenaSliceHeader) which is the free list that receives recycled
// chunks.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactOldSliceHeaders(a *RegisterArena, slabs *[][]arenaSliceHeader, live []bool, free *[][]arenaSliceHeader, released *uint64) (reclaimed, recycled int64) {
	return compactDeadSlabs(a, slabs, live, free, sliceHeaderSlabChunk, arenaSliceHeaderBytes, released)
}

// compactDeadSlabs is the shared body of the chunked-class compactors.
//
// Live slabs are kept in order, dead chunk-sized slabs are zeroed and moved to the free
// list while it has room, and every other dead slab is dropped for the Go garbage
// collector.
//
// A dead slab the licence has revoked is always dropped, never recycled.
//
// Takes arena (*RegisterArena) which owns the slabs and their licence.
// Takes slabs (*[]S) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes free (*[]S) which is the free list that receives recycled chunks.
// Takes chunk (int) which is the class's chunk size in elements.
// Takes elementBytes (int64) which is the byte size of one element.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dead slabs and the subset kept on the free list.
func compactDeadSlabs[T any](arena *RegisterArena, slabs *[][]T, live []bool, free *[][]T, chunk int, elementBytes int64, released *uint64) (reclaimed, recycled int64) {
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
		switch {
		case licenceRevokedSlab(arena, slab):
			if licenceCounting {
				arena.stats().deadChunksKeptFromReuse.Add(1)
			}
			*released += takeSlabCharge(arena, slab, safeconv.Int64ToUint64(elementBytes))
		case recycleDeadChunk(free, slab, chunk):
			recycled += slabBytes
		default:
			*released += takeSlabCharge(arena, slab, safeconv.Int64ToUint64(elementBytes))
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
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]int64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldIntBackings(arena *RegisterArena, slabs *[][]int64, live []bool, released *uint64) int64 {
	return compactRetiredSlabs(arena, slabs, live, int64BackingBytes, released)
}

// compactOldFloatBackings drops float64-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 8 bytes).
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]float64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldFloatBackings(arena *RegisterArena, slabs *[][]float64, live []bool, released *uint64) int64 {
	return compactRetiredSlabs(arena, slabs, live, float64BackingBytes, released)
}

// compactOldStringBackings drops string-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 16 bytes, the size of a Go string header).
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]string) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldStringBackings(arena *RegisterArena, slabs *[][]string, live []bool, released *uint64) int64 {
	return compactRetiredSlabs(arena, slabs, live, stringBackingBytes, released)
}

// compactOldBoolBackings drops bool-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 1 byte).
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]bool) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldBoolBackings(arena *RegisterArena, slabs *[][]bool, live []bool, released *uint64) int64 {
	return compactRetiredSlabs(arena, slabs, live, 1, released)
}

// compactOldUintBackings drops uint64-backing slabs whose liveness flag is false. Each
// entry's backing capacity is (cap * 8 bytes).
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]uint64) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactOldUintBackings(arena *RegisterArena, slabs *[][]uint64, live []bool, released *uint64) int64 {
	return compactRetiredSlabs(arena, slabs, live, uint64BackingBytes, released)
}

// compactRetiredSlabs is the shared body of the typed-backing compactors. It keeps live
// slabs in order and drops every dead slab for the Go garbage collector.
//
// Takes arena (*RegisterArena) which owns the slabs and the budget.
// Takes slabs (*[][]T) which is the retention list to compact.
// Takes live ([]bool) which is the per-slab liveness bitmap.
// Takes elementBytes (int64) which is the byte size of one element.
// Takes released (*uint64) which accumulates the budget charge the dropped slabs carry.
//
// Returns total bytes reclaimed across dropped slabs.
func compactRetiredSlabs[T any](arena *RegisterArena, slabs *[][]T, live []bool, elementBytes int64, released *uint64) int64 {
	var reclaimed int64
	writeIndex := 0
	source := *slabs
	for i, slab := range source {
		if i < len(live) && live[i] {
			source[writeIndex] = slab
			writeIndex++
			continue
		}
		reclaimed += int64(cap(slab)) * elementBytes
		*released += takeSlabCharge(arena, slab, safeconv.Int64ToUint64(elementBytes))
		source[i] = nil
	}
	for i := writeIndex; i < len(source); i++ {
		source[i] = nil
	}
	*slabs = source[:writeIndex]
	return reclaimed
}
