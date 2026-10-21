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
	"cmp"
	"math/bits"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"
)

const (
	// licenceShardRegionShift sets the shard region size to 1 MiB.
	licenceShardRegionShift = 20

	// licenceSmallShards is the number of region shards; the large shard follows them.
	licenceSmallShards = 64

	// licenceLargeShard is the index of the shard holding ranges larger than a region.
	licenceLargeShard = licenceSmallShards

	// licenceDropWordShift turns a position into its bit word's index.
	licenceDropWordShift = 6

	// licenceDropWordMask turns a position into its bit's index within the word.
	licenceDropWordMask = 1<<licenceDropWordShift - 1

	// licencePublishWindowLimit bounds the ranges, counted on both sides, of the window a
	// publication merges rather than sorting the whole gathering.
	licencePublishWindowLimit = 256
)

// licenceDropSet marks the positions of the entries one publication drops from a shard's
// table, one bit per position, in words the shard keeps between publications
// (licenceRegistryShard.dropWords), so a publication marks drops without allocating
// however large the table is.
type licenceDropSet struct {
	// words holds one bit per table position, all clear when the set is made.
	words []uint64

	// count counts the marks made.
	count int
}

// licenceRegistryBuffer is one generation of a licence registry shard.
type licenceRegistryBuffer struct {
	// ranges holds the shard's published slabs, sorted by start and non-overlapping. A
	// writer modifies it only while no reader is counted in and the buffer is not current.
	ranges []licenceSlabRange

	// readers counts the readers inside the buffer (acquire).
	readers atomic.Int32
}

// licenceRegistryShard is one shard of the licence registry.
type licenceRegistryShard struct {
	// current is the shard's current generation.
	current atomic.Pointer[licenceRegistryBuffer]

	// spares holds the retired generations a writer may recycle once no reader is counted
	// into them.
	spares [2]*licenceRegistryBuffer

	// dropWords is the bit words of the drop set each publication marks (dropSet), kept
	// under mu so a publication allocates none once the table has stopped growing.
	dropWords []uint64

	// mu serialises updates to current and guards spares and dropWords.
	mu sync.Mutex

	_ [64]byte
}

// licenceRegistryShards holds the region shards and, last, the large shard.
var licenceRegistryShards [licenceSmallShards + 1]licenceRegistryShard

// licenceRegionOf returns the shard region an address lies in.
//
// Takes p (uintptr) which is the address.
//
// Returns uintptr which is the region number.
func licenceRegionOf(p uintptr) uintptr { return p >> licenceShardRegionShift }

// licenceRegionShard returns the shard index of a region.
//
// Takes region (uintptr) which is the region number.
//
// Returns int which is the index.
func licenceRegionShard(region uintptr) int { return int(region % licenceSmallShards) }

// licenceShardOf returns the index of the shard a range lives in.
//
// Takes entry (licenceSlabRange) which is the range.
//
// Returns int which is the shard index.
func licenceShardOf(entry licenceSlabRange) int {
	first := licenceRegionOf(entry.start)
	if licenceRegionOf(entry.end-1) > first+1 {
		return licenceLargeShard
	}
	return licenceRegionShard(first)
}

// acquire counts the caller into the shard's current buffer, which no writer modifies
// until the caller releases it.
//
// Returns *licenceRegistryBuffer which is nil when the shard was never published.
func (s *licenceRegistryShard) acquire() *licenceRegistryBuffer {
	for {
		buffer := s.current.Load()
		if buffer == nil {
			return nil
		}
		buffer.readers.Add(1)
		if s.current.Load() == buffer {
			return buffer
		}
		buffer.readers.Add(-1)
	}
}

// release counts the caller out of the buffer.
func (b *licenceRegistryBuffer) release() {
	if b != nil {
		b.readers.Add(-1)
	}
}

// find returns the published slab holding address.
//
// Takes address (uintptr) which is the address.
//
// Returns licenceSlabRange which describes the published slab containing the address.
// Returns bool which is true when the address was found in a published slab.
func (b *licenceRegistryBuffer) find(address uintptr) (licenceSlabRange, bool) {
	if b == nil {
		return licenceSlabRange{}, false
	}
	ranges := b.ranges
	low, high := 0, len(ranges)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if ranges[middle].start <= address {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low == 0 || address >= ranges[low-1].end {
		return licenceSlabRange{}, false
	}
	return ranges[low-1], true
}

// licenceFindIn looks p up in one shard. It checks that the slab's arena is still
// reachable (slabHolderLive) only after leaving the buffer, so a weak-pointer read that
// waits for the collector never keeps a writer from recycling the buffer.
//
// Takes index (int) which is the shard.
// Takes p (uintptr) which is the address.
//
// Returns licenceSlabRange which describes the published slab containing the address.
// Returns bool which is true when the address was found in a published slab whose arena
// is still reachable.
func licenceFindIn(index int, p uintptr) (licenceSlabRange, bool) {
	buffer := licenceRegistryShards[index].acquire()
	entry, found := buffer.find(p)
	buffer.release()
	if found && !slabHolderLive(entry.live) {
		return licenceSlabRange{}, false
	}
	return entry, found
}

// licenceOwnerID returns the arena's registry identity, assigning one on first use.
//
// Returns uint64 which is the identity.
func (a *RegisterArena) licenceOwnerID() uint64 {
	return a.ensureSlabOwner()
}

// publishLicenceSlabs records the storage a licensed arena holds in the licence registry,
// and drops its own foreign revocations pending against slabs it no longer holds, after
// it has removed them from the registry (dropForeignRevocations). advancePublication
// states what the publication guarantees its readers.
func (a *RegisterArena) publishLicenceSlabs() {
	if !a.licensed {
		return
	}
	owner := a.licenceOwnerID()
	state := &a.licenceState
	if !state.advancePublication(a.appendLicenceRanges(state.gatherSpare[:0], owner)) {
		return
	}
	applyLicenceRanges(owner, state.addedScratch, state.removedScratch)
	if state.foreignLocked {
		dropForeignRevocationsLocked(owner, state.droppedScratch)
		return
	}
	dropForeignRevocations(owner, state.droppedScratch)
}

// advancePublication decides an arena's next publication from a new gathering of its
// ranges.
//
// Published ranges stay sorted and do not overlap. A range held before and after a
// publication stays in every buffer a reader can acquire meanwhile. A range the arena
// gave up leaves the registry before its foreign revocations are dropped.
//
// Takes gathered ([]licenceSlabRange) which is the arena's ranges in gathering order, in
// the spare gathering buffer.
//
// Returns bool which is true when the publication changed and the registry needs the
// difference applied.
func (s *arenaLicenceExactState) advancePublication(gathered []licenceSlabRange) bool {
	previous := s.gathered
	prefix, suffix := licenceGatherWindow(previous, gathered)
	if prefix == len(previous) && prefix == len(gathered) {
		s.gatherSpare = gathered
		return false
	}
	s.gathered, s.gatherSpare = gathered, previous
	if changed, merged := s.mergeLicencePublication(previous[prefix:len(previous)-suffix], gathered[prefix:len(gathered)-suffix]); merged {
		s.publishMerged++
		return changed
	}
	s.publishSorted++
	return s.sortLicencePublication(gathered)
}

// licenceGatherWindow measures what two gatherings share at their start and their end.
//
// Takes previous ([]licenceSlabRange) which is the earlier gathering.
// Takes gathered ([]licenceSlabRange) which is the new one.
//
// Returns prefix (int) which counts the equal leading ranges.
// Returns suffix (int) which counts the equal trailing ranges after them, so that
// prefix+suffix never exceeds either length.
func licenceGatherWindow(previous, gathered []licenceSlabRange) (prefix int, suffix int) {
	shorter := min(len(previous), len(gathered))
	for prefix < shorter && previous[prefix] == gathered[prefix] {
		prefix++
	}
	for suffix < shorter-prefix && previous[len(previous)-1-suffix] == gathered[len(gathered)-1-suffix] {
		suffix++
	}
	return prefix, suffix
}

// mergeLicencePublication tries to publish a gathering by merging the difference of its
// window into published (advancePublication).
//
// Takes before ([]licenceSlabRange) which is the previous gathering's differing window.
// Takes after ([]licenceSlabRange) which is the new gathering's differing window.
//
// Returns changed (bool) which is true when the publication changed.
// Returns merged (bool) which is false when the caller must fall back to the full sort,
// in which case published is untouched.
func (s *arenaLicenceExactState) mergeLicencePublication(before, after []licenceSlabRange) (changed bool, merged bool) {
	if len(before)+len(after) > licencePublishWindowLimit {
		return false, false
	}
	s.windowBefore = append(s.windowBefore[:0], before...)
	s.windowAfter = append(s.windowAfter[:0], after...)
	slices.SortFunc(s.windowBefore, compareLicenceRanges)
	slices.SortFunc(s.windowAfter, compareLicenceRanges)
	added, removed, ok := licenceWindowDifference(s.addedScratch[:0], s.removedScratch[:0], s.windowBefore, s.windowAfter)
	s.addedScratch, s.removedScratch = added, removed
	if !ok {
		return false, false
	}
	if len(added) == 0 && len(removed) == 0 {
		return false, licenceRangesIncrease(s.published)
	}
	next, ok := mergePublishedRanges(s.publishSpare[:0], s.published, added, removed)
	s.publishSpare = next
	if !ok {
		return false, false
	}
	dropped := s.droppedScratch[:0]
	for _, entry := range removed {
		if _, found := slices.BinarySearchFunc(next, entry.start, licenceRangeStartCompare); !found {
			dropped = append(dropped, entry.start)
		}
	}
	s.droppedScratch = dropped
	s.published, s.publishSpare = next, s.published
	return true, true
}

// sortLicencePublication publishes a gathering by sorting all of it and comparing the
// result with published, the fallback advancePublication takes.
//
// Takes gathered ([]licenceSlabRange) which is the new gathering.
//
// Returns bool which is true when the publication changed.
func (s *arenaLicenceExactState) sortLicencePublication(gathered []licenceSlabRange) bool {
	s.publishSpare = append(s.publishSpare[:0], gathered...)
	next := s.publishSpare
	slices.SortFunc(next, compareLicenceRanges)
	if slices.Equal(next, s.published) {
		return false
	}
	s.droppedScratch = licenceRangesMissing(s.droppedScratch[:0], s.published, next)
	s.addedScratch = licenceRangesNotIn(s.addedScratch[:0], next, s.published)
	s.removedScratch = licenceRangesNotIn(s.removedScratch[:0], s.published, next)
	s.published, s.publishSpare = next, s.published
	return true
}

// licenceWindowDifference compares two sorted windows by start. A range only before has
// is removed, one only after has is added, and two different ranges at one start are
// both.
//
// Takes added ([]licenceSlabRange) which receives the added ranges.
// Takes removed ([]licenceSlabRange) which receives the removed ranges.
// Takes before ([]licenceSlabRange) which is the previous window, sorted by start.
// Takes after ([]licenceSlabRange) which is the new window, sorted by start.
//
// Returns addedRanges ([]licenceSlabRange) which is added extended, sorted by start.
// Returns removedRanges ([]licenceSlabRange) which is removed extended, sorted by start.
// Returns ok (bool) which is false when either window is not strictly increasing by
// start.
func licenceWindowDifference(added, removed, before, after []licenceSlabRange) (addedRanges []licenceSlabRange, removedRanges []licenceSlabRange, ok bool) {
	if !licenceRangesIncrease(before) || !licenceRangesIncrease(after) {
		return added, removed, false
	}
	index := 0
	other := 0
	for index < len(before) || other < len(after) {
		switch {
		case other == len(after) || index < len(before) && before[index].start < after[other].start:
			removed = append(removed, before[index])
			index++
		case index == len(before) || after[other].start < before[index].start:
			added = append(added, after[other])
			other++
		default:
			if before[index] != after[other] {
				removed = append(removed, before[index])
				added = append(added, after[other])
			}
			index++
			other++
		}
	}
	return added, removed, true
}

// licenceRangesIncrease reports whether ranges are strictly increasing by start.
//
// Takes ranges ([]licenceSlabRange) which are the ranges.
//
// Returns bool which is true when every start exceeds the one before it.
func licenceRangesIncrease(ranges []licenceSlabRange) bool {
	for index := 1; index < len(ranges); index++ {
		if ranges[index-1].start >= ranges[index].start {
			return false
		}
	}
	return true
}

// mergePublishedRanges builds an arena's next publication from published in one pass,
// leaving out each removed range and merging in each added one by start.
//
// Takes out ([]licenceSlabRange) which receives the publication, empty.
// Takes published ([]licenceSlabRange) which is the previous publication.
// Takes added ([]licenceSlabRange) which are the new ranges, strictly increasing by
// start.
// Takes removed ([]licenceSlabRange) which are the ranges given up, strictly increasing
// by start.
//
// Returns []licenceSlabRange which is out holding the publication.
// Returns bool which is false when published is not strictly increasing by start, a
// removed range is not in it exactly, or the result is not strictly increasing by start.
func mergePublishedRanges(out, published, added, removed []licenceSlabRange) ([]licenceSlabRange, bool) {
	cursor := 0
	gone := 0
	ok := true
	for position, entry := range published {
		if position > 0 && published[position-1].start >= entry.start {
			return out, false
		}
		for ; ok && cursor < len(added) && added[cursor].start < entry.start; cursor++ {
			out, ok = appendLicenceRangeIncreasing(out, added[cursor])
		}
		switch {
		case !ok:
			return out, false
		case gone < len(removed) && removed[gone] == entry:
			gone++
		case gone < len(removed) && removed[gone].start <= entry.start:
			return out, false
		default:
			out, ok = appendLicenceRangeIncreasing(out, entry)
		}
	}
	for ; ok && cursor < len(added); cursor++ {
		out, ok = appendLicenceRangeIncreasing(out, added[cursor])
	}
	return out, ok && gone == len(removed)
}

// appendLicenceRangeIncreasing appends entry to out when it starts after out's last
// range.
//
// Takes out ([]licenceSlabRange) which is strictly increasing by start.
// Takes entry (licenceSlabRange) which is the range.
//
// Returns []licenceSlabRange which is out, extended when the order allows.
// Returns bool which is false when entry does not start after out's last range.
func appendLicenceRangeIncreasing(out []licenceSlabRange, entry licenceSlabRange) ([]licenceSlabRange, bool) {
	if len(out) > 0 && out[len(out)-1].start >= entry.start {
		return out, false
	}
	return append(out, entry), true
}

// compareLicenceRanges orders two ranges by start.
//
// Takes x (licenceSlabRange) which is the first range.
// Takes y (licenceSlabRange) which is the second range.
//
// Returns int which is negative, zero or positive as x starts before, with or after y.
func compareLicenceRanges(x, y licenceSlabRange) int { return cmp.Compare(x.start, y.start) }

// appendLicenceRanges appends a range for every slab the arena holds, each carrying the
// arena's weak pointer (slabLive).
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
//
// Returns []licenceSlabRange which is out extended.
func (a *RegisterArena) appendLicenceRanges(out []licenceSlabRange, owner uint64) []licenceSlabRange {
	first := len(out)
	out = a.appendLicenceSlabRanges(out, owner)
	for index := first; index < len(out); index++ {
		out[index].live = a.slabLive
	}
	return out
}

// appendLicenceSlabRanges is appendLicenceRanges before the weak pointer is set.
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
//
// Returns []licenceSlabRange which is out extended.
func (a *RegisterArena) appendLicenceSlabRanges(out []licenceSlabRange, owner uint64) []licenceSlabRange {
	out = appendLicenceList(out, owner, licenceSlabData, a.byteSlab, a.oldByteSlabs)
	out = appendLicenceList(out, owner, licenceSlabData, a.mutByteSlab, a.oldMutByteSlabs)
	out = appendLicenceList(out, owner, licenceSlabData, a.genericBytesSlab, a.oldGenericByteSlabs)
	out = appendLicenceList(out, owner, licenceSlabData, nil, a.freeGenericByteChunks)
	out = appendLicenceList(out, owner, licenceSlabData, a.sliceHeaderSlab, a.oldSliceHeaderSlabs)
	out = appendLicenceList(out, owner, licenceSlabData, nil, a.freeSliceHeaderChunks)
	out = appendLicenceList(out, owner, licenceSlabData, a.intBackingSlab, a.oldIntBackings)
	out = appendLicenceList(out, owner, licenceSlabData, a.floatBackingSlab, a.oldFloatBackings)
	out = appendLicenceList(out, owner, licenceSlabData, a.stringBackingSlab, a.oldStringBackings)
	out = appendLicenceList(out, owner, licenceSlabData, a.boolBackingSlab, a.oldBoolBackings)
	out = appendLicenceList(out, owner, licenceSlabData, a.uintBackingSlab, a.oldUintBackings)
	out = a.appendKRefRanges(out, owner)
	out = appendLicenceRange(out, owner, licenceSlabData, a.stringBoxSlab)
	out = appendLicenceRange(out, owner, licenceSlabData, a.intBoxSlab)
	out = appendLicenceRange(out, owner, licenceSlabData, a.floatBoxSlab)
	out = appendLicenceRange(out, owner, licenceSlabData, a.uintBoxSlab)
	out = appendLicenceRange(out, owner, licenceSlabData, a.complexBoxSlab)
	out = appendLicenceRange(out, owner, licenceSlabUpvalue, a.upvalueCellSlab)
	out = appendLicenceRange(out, owner, licenceSlabUpvalue, a.upvalueReferenceSlab)
	out = a.appendClosureChunkRanges(out, owner)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.intSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.floatSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.stringSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.generalSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.boolSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.uintSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.complexSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.slicesIntSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.slicesFloatSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.slicesStringSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.slicesBoolSlab)
	out = appendLicenceRange(out, owner, licenceSlabBank, a.slicesUintSlab)
	return appendLicenceRange(out, owner, licenceSlabBank, a.slicesByteSlab)
}

// appendLicenceRange appends slab's range when it has capacity.
//
// Takes out ([]licenceSlabRange) which receives the range.
// Takes owner (uint64) which is the arena's identity.
// Takes class (licenceSlabClass) which is the slab's class.
// Takes slab ([]T) which is the slab.
//
// Returns []licenceSlabRange which is out extended.
func appendLicenceRange[T any](out []licenceSlabRange, owner uint64, class licenceSlabClass, slab []T) []licenceSlabRange {
	if cap(slab) == 0 {
		return out
	}
	var zero T
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	return append(out, licenceSlabRange{live: weak.Pointer[RegisterArena]{}, start: start, end: start + uintptr(cap(slab))*unsafe.Sizeof(zero), owner: owner, class: class})
}

// appendLicenceList is appendLicenceRange over a current slab and a retention list.
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
// Takes class (licenceSlabClass) which is the slabs' class.
// Takes current ([]T) which is the current slab, or nil.
// Takes retired ([][]T) which is the retention or free list.
//
// Returns []licenceSlabRange which is out extended.
func appendLicenceList[T any](out []licenceSlabRange, owner uint64, class licenceSlabClass, current []T, retired [][]T) []licenceSlabRange {
	out = appendLicenceRange(out, owner, class, current)
	for _, slab := range retired {
		out = appendLicenceRange(out, owner, class, slab)
	}
	return out
}

// appendLicenceListBackwards is appendLicenceRange over a list, last slab first.
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes owner (uint64) which is the arena's identity.
// Takes class (licenceSlabClass) which is the slabs' class.
// Takes list ([][]T) which is the list.
//
// Returns []licenceSlabRange which is out extended.
func appendLicenceListBackwards[T any](out []licenceSlabRange, owner uint64, class licenceSlabClass, list [][]T) []licenceSlabRange {
	for _, slab := range slices.Backward(list) {
		out = appendLicenceRange(out, owner, class, slab)
	}
	return out
}

// licenceRangesMissing appends to out the start of each range in previous that next
// lacks.
//
// Takes out ([]uintptr) which receives the dropped starts.
// Takes previous ([]licenceSlabRange) which is the earlier publication, sorted.
// Takes next ([]licenceSlabRange) which is the new one, sorted.
//
// Returns []uintptr which is out extended.
func licenceRangesMissing(out []uintptr, previous, next []licenceSlabRange) []uintptr {
	index := 0
	for _, entry := range previous {
		for index < len(next) && next[index].start < entry.start {
			index++
		}
		if index == len(next) || next[index].start != entry.start {
			out = append(out, entry.start)
		}
	}
	return out
}

// licenceRangesNotIn appends to out each range of from that sorted lacks exactly.
//
// Takes out ([]licenceSlabRange) which receives the ranges.
// Takes from ([]licenceSlabRange) which is sorted by start.
// Takes sorted ([]licenceSlabRange) which is sorted by start.
//
// Returns []licenceSlabRange which is out extended.
//
//go:noinline
func licenceRangesNotIn(out, from, sorted []licenceSlabRange) []licenceSlabRange {
	index := 0
	for _, entry := range from {
		for index < len(sorted) && sorted[index].start < entry.start {
			index++
		}
		if index == len(sorted) || sorted[index] != entry {
			out = append(out, entry)
		}
	}
	return out
}

// writable returns a retired buffer of the shard no reader holds, emptied, with room for
// capacity ranges, or a new one. The caller holds s.mu.
//
// Takes capacity (int) which is the number of ranges the merge may write.
//
// Returns *licenceRegistryBuffer which no reader can see.
func (s *licenceRegistryShard) writable(capacity int) *licenceRegistryBuffer {
	for index, spare := range s.spares {
		if spare == nil || spare.readers.Load() != 0 {
			continue
		}
		s.spares[index] = nil
		if cap(spare.ranges) < capacity {
			spare.ranges = make([]licenceSlabRange, 0, 2*capacity)
		}
		spare.ranges = spare.ranges[:0]
		return spare
	}
	return &licenceRegistryBuffer{ranges: make([]licenceSlabRange, 0, 2*capacity), readers: atomic.Int32{}}
}

// retire keeps buffer for reuse once no reader holds it. The caller holds s.mu.
//
// Takes buffer (*licenceRegistryBuffer) which was current until the caller replaced it.
func (s *licenceRegistryShard) retire(buffer *licenceRegistryBuffer) {
	if buffer == nil {
		return
	}
	for index, spare := range s.spares {
		if spare == nil {
			s.spares[index] = buffer
			return
		}
	}
	for index, spare := range s.spares {
		if spare.readers.Load() == 0 {
			s.spares[index] = buffer
			return
		}
	}
}

// licenceOverlapsAny reports whether entry overlaps a range of sorted.
//
// Takes sorted ([]licenceSlabRange) which is sorted by start and non-overlapping.
// Takes entry (licenceSlabRange) which is the range.
//
// Returns bool which is true on an overlap.
func licenceOverlapsAny(sorted []licenceSlabRange, entry licenceSlabRange) bool {
	low, high := 0, len(sorted)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if sorted[middle].end <= entry.start {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low < len(sorted) && sorted[low].start < entry.end
}

// licenceShardsToTouch identifies the shards affected by a registry publication.
//
// Takes added ([]licenceSlabRange) which are the new ranges.
// Takes removed ([]licenceSlabRange) which are the ranges no longer held.
//
// Returns small (uint64) which has bit i set for region shard i.
// Returns large (bool) which is true for the large shard.
func licenceShardsToTouch(added, removed []licenceSlabRange) (small uint64, large bool) {
	for _, entry := range removed {
		if index := licenceShardOf(entry); index == licenceLargeShard {
			large = true
		} else {
			small |= 1 << uint(index)
		}
	}
	for _, entry := range added {
		home := licenceShardOf(entry)
		if home == licenceLargeShard {
			large = true
		} else {
			small |= 1 << uint(home)
		}
		if home != licenceLargeShard && licenceShardOverlaps(licenceLargeShard, entry) {
			large = true
		}
		small = licenceRegionShardsOverlapped(entry, home, small)
	}
	return small, large
}

// licenceRegionShardsOverlapped adds the region shards, other than the range's home, that
// hold an entry the range overlaps: those of its regions and the region before them.
//
// Takes entry (licenceSlabRange) which is the added range.
// Takes home (int) which is its home shard.
// Takes small (uint64) which has bit i set for each region shard already touched.
//
// Returns uint64 which is small with the overlapped shards' bits set.
func licenceRegionShardsOverlapped(entry licenceSlabRange, home int, small uint64) uint64 {
	first, last := licenceRegionOf(entry.start), licenceRegionOf(entry.end-1)
	if first > 0 {
		first--
	}
	if last-first >= licenceSmallShards {
		first, last = 0, licenceSmallShards-1
	}
	for region := first; region <= last; region++ {
		index := licenceRegionShard(region)
		if index != home && small&(1<<uint(index)) == 0 && licenceShardOverlaps(index, entry) {
			small |= 1 << uint(index)
		}
	}
	return small
}

// licenceShardOverlaps reports whether a shard holds an entry entry overlaps, read
// lock-free.
//
// Takes index (int) which is the shard.
// Takes entry (licenceSlabRange) which is the range.
//
// Returns bool which is true on an overlap.
func licenceShardOverlaps(index int, entry licenceSlabRange) bool {
	shard := &licenceRegistryShards[index]
	if shard.current.Load() == nil {
		return false
	}
	buffer := shard.acquire()
	overlaps := buffer != nil && licenceOverlapsAny(buffer.ranges, entry)
	buffer.release()
	return overlaps
}

// applyLicenceRanges applies owner's publication difference to the registry.
//
// Takes owner (uint64) which is the publishing arena.
// Takes added ([]licenceSlabRange) which are its new ranges, sorted.
// Takes removed ([]licenceSlabRange) which are the ranges it no longer holds, sorted.
//
// Safe for concurrent use; each shard's mutex serialises its updates.
func applyLicenceRanges(owner uint64, added, removed []licenceSlabRange) {
	small, large := licenceShardsToTouch(added, removed)
	for small != 0 {
		index := bits.TrailingZeros64(small)
		small &= small - 1
		shard := &licenceRegistryShards[index]
		shard.mu.Lock()
		shard.applyLocked(index, owner, added, removed)
		shard.mu.Unlock()
	}
	if large {
		shard := &licenceRegistryShards[licenceLargeShard]
		shard.mu.Lock()
		shard.applyLocked(licenceLargeShard, owner, added, removed)
		shard.mu.Unlock()
	}
}

// applyLocked rewrites one shard for a publication difference (applyLicenceRanges), with
// s.mu held.
//
// Takes index (int) which is the shard's index.
// Takes added ([]licenceSlabRange) which are its new ranges, sorted.
// Takes removed ([]licenceSlabRange) which are the ranges it no longer holds, sorted.
func (s *licenceRegistryShard) applyLocked(index int, _ uint64, added, removed []licenceSlabRange) {
	current := s.current.Load()
	var ranges []licenceSlabRange
	if current != nil {
		ranges = current.ranges
	}
	drops := s.dropSet(len(ranges))
	homeAdds := drops.collect(ranges, index, added, removed)
	if drops.count == 0 && homeAdds == 0 {
		return
	}
	for _, entry := range added {
		if licenceShardOf(entry) == index && !licenceRangesContain(ranges, entry) {
			slabPagesAdd(entry.start, entry.end, 1)
		}
	}
	merged := s.writable(len(ranges) + homeAdds)
	merged.ranges = mergeLicenceRanges(merged.ranges, ranges, &drops, added, index)
	s.current.Store(merged)
	s.retire(current)
	for position, entry := range ranges {
		if drops.has(position) {
			slabPagesAdd(entry.start, entry.end, -1)
		}
	}
}

// collect marks the positions of a shard's entries a publication drops: the owner's
// removed ranges homed in the shard, found exactly, and every other entry an added range
// overlaps.
//
// Takes ranges ([]licenceSlabRange) which is the shard's published table, sorted.
// Takes index (int) which is the shard's index.
// Takes added ([]licenceSlabRange) which are the owner's new ranges, sorted.
// Takes removed ([]licenceSlabRange) which are the ranges it no longer holds, sorted.
//
// Returns homeAdds (int) which counts the added ranges homed in the shard.
func (d *licenceDropSet) collect(ranges []licenceSlabRange, index int, added, removed []licenceSlabRange) (homeAdds int) {
	for _, entry := range removed {
		if licenceShardOf(entry) != index {
			continue
		}
		if position, found := slices.BinarySearchFunc(ranges, entry.start, licenceRangeStartCompare); found && ranges[position] == entry {
			d.add(position)
		}
	}
	for _, entry := range added {
		home := licenceShardOf(entry) == index
		if home {
			homeAdds++
		}
		d.addOverlapped(ranges, entry, home)
	}
	return homeAdds
}

// addOverlapped marks every entry an added range overlaps, except the range itself in its
// home shard.
//
// Takes ranges ([]licenceSlabRange) which is the shard's published table, sorted.
// Takes entry (licenceSlabRange) which is the added range.
// Takes home (bool) which is true when the shard is the range's home.
func (d *licenceDropSet) addOverlapped(ranges []licenceSlabRange, entry licenceSlabRange, home bool) {
	position, _ := slices.BinarySearchFunc(ranges, entry.start, licenceRangeEndCompare)
	for ; position < len(ranges) && ranges[position].start < entry.end; position++ {
		if home && ranges[position] == entry || d.has(position) {
			continue
		}
		d.add(position)
	}
}

// dropSet returns an empty drop set for a table of length positions, over the shard's
// kept words, which grow with headroom when the table outgrows them. The caller holds
// s.mu.
//
// Takes length (int) which is the table's length.
//
// Returns licenceDropSet which marks no position.
func (s *licenceRegistryShard) dropSet(length int) licenceDropSet {
	words := (length + licenceDropWordMask) >> licenceDropWordShift
	if cap(s.dropWords) < words {
		s.dropWords = make([]uint64, 2*words)
	}
	s.dropWords = s.dropWords[:words]
	clear(s.dropWords)
	return licenceDropSet{words: s.dropWords, count: 0}
}

// add marks position dropped.
//
// Takes position (int) which is the entry's position in the shard's table.
func (d *licenceDropSet) add(position int) {
	d.count++
	d.words[position>>licenceDropWordShift] |= 1 << uint(position&licenceDropWordMask)
}

// has reports whether position is dropped.
//
// Takes position (int) which is the entry's position in the shard's table.
//
// Returns bool which is true when it is.
func (d *licenceDropSet) has(position int) bool {
	return d.words[position>>licenceDropWordShift]&(1<<uint(position&licenceDropWordMask)) != 0
}

// mergeLicenceRanges builds a shard's next table: the entries it keeps, with the added
// ranges homed in the shard merged in by start.
//
// Takes out ([]licenceSlabRange) which receives the table, empty.
// Takes ranges ([]licenceSlabRange) which is the shard's published table, sorted.
// Takes drops (*licenceDropSet) which marks the entries dropped.
// Takes added ([]licenceSlabRange) which are the owner's new ranges, sorted.
// Takes index (int) which is the shard's index.
//
// Returns []licenceSlabRange which is out holding the next table, sorted.
func mergeLicenceRanges(out, ranges []licenceSlabRange, drops *licenceDropSet, added []licenceSlabRange, index int) []licenceSlabRange {
	cursor := 0
	for position, entry := range ranges {
		if drops.has(position) {
			continue
		}
		for cursor < len(added) && added[cursor].start <= entry.start {
			if licenceShardOf(added[cursor]) == index && added[cursor] != entry {
				out = append(out, added[cursor])
			}
			cursor++
		}
		out = append(out, entry)
	}
	for ; cursor < len(added); cursor++ {
		if licenceShardOf(added[cursor]) == index {
			out = append(out, added[cursor])
		}
	}
	return out
}

// licenceRangeStartCompare orders a range against an address by its start.
//
// Takes entry (licenceSlabRange) which is a range.
// Takes p (uintptr) which is the address.
//
// Returns int which is negative, zero or positive as entry starts before, at or after p.
func licenceRangeStartCompare(entry licenceSlabRange, p uintptr) int {
	return cmp.Compare(entry.start, p)
}

// licenceRangeEndCompare places p among ranges by their ends.
//
// The first range whose end exceeds p is the first that can contain or follow it.
//
// Takes entry (licenceSlabRange) which is a range.
// Takes p (uintptr) which is the address.
//
// Returns int which is negative while entry ends at or before p.
func licenceRangeEndCompare(entry licenceSlabRange, p uintptr) int {
	if entry.end <= p {
		return -1
	}
	return 1
}

// licenceRangesContain reports whether sorted holds entry exactly.
//
// Takes sorted ([]licenceSlabRange) which is sorted by start.
// Takes entry (licenceSlabRange) which is the range.
//
// Returns bool which is true when an equal range is present.
func licenceRangesContain(sorted []licenceSlabRange, entry licenceSlabRange) bool {
	low, high := 0, len(sorted)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if sorted[middle].start < entry.start {
			low = middle + 1
		} else {
			high = middle
		}
	}
	return low < len(sorted) && sorted[low] == entry
}

// forgetLicenceSlabOwner removes every range owner published and then every foreign
// revocation pending against owner's slabs (dropOwnerForeignRevocations).
//
// Takes owner (uint64) which identifies the arena.
//
// Concurrency: Serialises removal within each registry shard; the caller must prevent the
// retired owner from publishing more slabs.
func forgetLicenceSlabOwner(owner uint64) {
	for index := range licenceRegistryShards {
		s := &licenceRegistryShards[index]
		s.mu.Lock()
		s.forgetOwnerLocked(owner)
		s.mu.Unlock()
	}
	dropOwnerForeignRevocations(owner)
}

// forgetOwnerLocked removes every range owner published in the shard, with s.mu held.
//
// Takes owner (uint64) which identifies the arena.
func (s *licenceRegistryShard) forgetOwnerLocked(owner uint64) {
	current := s.current.Load()
	if current == nil || !slices.ContainsFunc(current.ranges, func(entry licenceSlabRange) bool { return entry.owner == owner }) {
		return
	}
	next := s.writable(len(current.ranges))
	for _, entry := range current.ranges {
		if entry.owner != owner {
			next.ranges = append(next.ranges, entry)
		}
	}
	s.current.Store(next)
	s.retire(current)
	for _, entry := range current.ranges {
		if entry.owner == owner {
			slabPagesAdd(entry.start, entry.end, -1)
		}
	}
}

// lockLicenceRegistry holds every shard's lock, in index order, so the registry reads the
// same until unlockLicenceRegistry (the typed-walk diff).
//
// Concurrency: Acquires every registry shard's writer mutex in order; the caller must
// pair it with unlockLicenceRegistry.
func lockLicenceRegistry() {
	for index := range licenceRegistryShards {
		licenceRegistryShards[index].mu.Lock()
	}
}

// unlockLicenceRegistry releases what lockLicenceRegistry holds.
//
// Concurrency: Requires the caller to hold every registry shard's writer mutex acquired
// by lockLicenceRegistry.
func unlockLicenceRegistry() {
	for index := len(licenceRegistryShards) - 1; index >= 0; index-- {
		licenceRegistryShards[index].mu.Unlock()
	}
}

// licenceSlabContaining finds the published slab holding address by searching its
// region's shard, the preceding region's shard for a crossing range, and the large shard.
//
// Takes address (uintptr) which is the address.
//
// Returns licenceSlabRange which describes the published slab containing the address.
// Returns bool which is true when the address was found in a published slab.
func licenceSlabContaining(address uintptr) (licenceSlabRange, bool) {
	if !slabPagesMayHold(address) {
		return licenceSlabRange{}, false
	}
	region := licenceRegionOf(address)
	if entry, found := licenceFindIn(licenceRegionShard(region), address); found {
		return entry, true
	}
	if region > 0 {
		if entry, found := licenceFindIn(licenceRegionShard(region-1), address); found {
			return entry, true
		}
	}
	return licenceFindIn(licenceLargeShard, address)
}

// licencePublishes checks lock-free whether owner's registry contains a range beginning
// at start.
//
// Takes owner (uint64) which identifies the arena.
// Takes start (uintptr) which is the range's start.
//
// Returns bool which is true while the range is published.
func licencePublishes(owner uint64, start uintptr) bool {
	if !slabPagesMayHold(start) {
		return false
	}
	for _, index := range [...]int{licenceRegionShard(licenceRegionOf(start)), licenceLargeShard} {
		if entry, found := licenceFindIn(index, start); found && entry.start == start && entry.owner == owner {
			return true
		}
	}
	return false
}

// licenceRegistryRanges returns a copy of every published range, sorted, for tests.
//
// Returns []licenceSlabRange which is the copy.
func licenceRegistryRanges() []licenceSlabRange {
	var out []licenceSlabRange
	for index := range licenceRegistryShards {
		buffer := licenceRegistryShards[index].acquire()
		if buffer != nil {
			out = append(out, buffer.ranges...)
		}
		buffer.release()
	}
	slices.SortFunc(out, compareLicenceRanges)
	return out
}
