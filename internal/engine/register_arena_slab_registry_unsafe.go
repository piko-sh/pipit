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
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"
)

// arenaSlabRange is one recorded byte slab.
type arenaSlabRange struct {
	// live points weakly at the arena that installed the slab (RegisterArena.slabLive).
	live weak.Pointer[RegisterArena]

	// start is the slab's first byte address.
	start uintptr

	// end is one past the slab's last byte address.
	end uintptr

	// owner identifies the arena that installed the slab.
	owner uint64
}

var (
	// arenaSlabRanges holds the recorded slabs, sorted by start; a stored slice is never
	// modified.
	arenaSlabRanges atomic.Pointer[[]arenaSlabRange]

	// arenaSlabRangesMu serialises updates to arenaSlabRanges.
	arenaSlabRangesMu sync.Mutex

	// arenaSlabOwners numbers arenas for the registry.
	arenaSlabOwners atomic.Uint64
)

// recordArenaSlab adds slab, installed by the arena owner, to the registry.
//
// Takes owner (uint64) which identifies the arena.
// Takes live (weak.Pointer[RegisterArena]) which points weakly at the arena.
// Takes slab ([]byte) which is the byte slab. An empty slab is ignored.
//
// Safe for concurrent use. A mutex serialises registry updates, and readers load an
// immutable snapshot published atomically.
func recordArenaSlab(owner uint64, live weak.Pointer[RegisterArena], slab []byte) {
	if len(slab) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	entry := arenaSlabRange{start: start, end: start + uintptr(len(slab)), live: live, owner: owner}
	arenaSlabRangesMu.Lock()
	defer arenaSlabRangesMu.Unlock()
	current := loadArenaSlabRanges()
	index, found := slices.BinarySearchFunc(current, start, compareArenaSlabStart)
	if found && current[index] == entry {
		return
	}
	slabPagesAdd(entry.start, entry.end, 1)
	next := make([]arenaSlabRange, 0, len(current)+1)
	next = appendArenaSlabReplacing(next, current, entry, 0)
	arenaSlabRanges.Store(&next)
	uncountArenaSlabsReplaced(current, entry, 0)
}

// appendArenaSlabReplacing appends to out the entries of current, in order, with entry
// inserted at its place, leaving out any entry starting at previous (when previous is
// non-zero) and any entry entry overlaps.
//
// Takes out ([]arenaSlabRange) which receives the entries.
// Takes current ([]arenaSlabRange) which is the registry, sorted by start.
// Takes entry (arenaSlabRange) which is the slab being recorded.
// Takes previous (uintptr) which is the start of a slab the recording arena replaces, or
// 0.
//
// Returns []arenaSlabRange which is out extended.
func appendArenaSlabReplacing(out, current []arenaSlabRange, entry arenaSlabRange, previous uintptr) []arenaSlabRange {
	inserted := false
	for _, existing := range current {
		if arenaSlabReplaced(existing, entry, previous) {
			continue
		}
		if !inserted && existing.start >= entry.start {
			out = append(out, entry)
			inserted = true
		}
		out = append(out, existing)
	}
	if !inserted {
		out = append(out, entry)
	}
	return out
}

// uncountArenaSlabsReplaced uncounts, in the slab page table, the pages of every entry of
// current that appendArenaSlabReplacing left out. It runs after the new snapshot is
// published, so the table never reports a recorded slab's page empty.
//
// Takes current ([]arenaSlabRange) which is the replaced snapshot.
// Takes entry (arenaSlabRange) which is the slab recorded.
// Takes previous (uintptr) which is the start of the slab it replaced, or 0.
func uncountArenaSlabsReplaced(current []arenaSlabRange, entry arenaSlabRange, previous uintptr) {
	for _, existing := range current {
		if arenaSlabReplaced(existing, entry, previous) {
			slabPagesAdd(existing.start, existing.end, -1)
		}
	}
}

// arenaSlabReplaced reports whether recording entry removes existing.
//
// Removal occurs when entry replaces the arena's previous slab or overlaps a stale
// registry entry.
//
// Takes existing (arenaSlabRange) which is a recorded entry.
// Takes entry (arenaSlabRange) which is the slab being recorded.
// Takes previous (uintptr) which is the start of the slab the recording arena replaces,
// or 0.
//
// Returns bool which is true when existing leaves the registry.
func arenaSlabReplaced(existing, entry arenaSlabRange, previous uintptr) bool {
	return (previous != 0 && existing.start == previous) || (existing.start < entry.end && entry.start < existing.end)
}

// swapArenaSlab replaces previous, a byte slab the arena no longer uses, with slab in one
// copy of the registry rather than one to forget the old slab and another to record the
// new one. Either may be empty.
//
// Takes owner (uint64) which identifies the arena.
// Takes live (weak.Pointer[RegisterArena]) which points weakly at the arena.
// Takes previous ([]byte) which is the slab being replaced.
// Takes slab ([]byte) which is its replacement.
//
// Safe for concurrent use. A mutex serialises registry updates, and readers load an
// immutable snapshot published atomically.
func swapArenaSlab(owner uint64, live weak.Pointer[RegisterArena], previous, slab []byte) {
	if len(previous) == 0 {
		recordArenaSlab(owner, live, slab)
		return
	}
	if len(slab) == 0 {
		forgetArenaSlab(previous)
		return
	}
	old := uintptr(unsafe.Pointer(unsafe.SliceData(previous)))
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	entry := arenaSlabRange{start: start, end: start + uintptr(len(slab)), live: live, owner: owner}
	arenaSlabRangesMu.Lock()
	defer arenaSlabRangesMu.Unlock()
	current := loadArenaSlabRanges()
	slabPagesAdd(entry.start, entry.end, 1)
	next := make([]arenaSlabRange, 0, len(current)+1)
	next = appendArenaSlabReplacing(next, current, entry, old)
	arenaSlabRanges.Store(&next)
	uncountArenaSlabsReplaced(current, entry, old)
}

// forgetArenaSlab removes slab from the registry.
//
// Takes slab ([]byte) which is a byte slab the arena no longer uses.
func forgetArenaSlab(slab []byte) {
	if len(slab) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	forgetArenaSlabRanges(func(entry arenaSlabRange) bool { return entry.start == start })
}

// forgetArenaSlabOwner removes every slab owner recorded, here and in the licence
// registry.
//
// Takes owner (uint64) which identifies the collected arena.
func forgetArenaSlabOwner(owner uint64) {
	forgetArenaSlabRanges(func(entry arenaSlabRange) bool { return entry.owner == owner })
	forgetLicenceSlabOwner(owner)
}

// forgetArenaSlabRanges removes the recorded slabs drop selects.
//
// Takes drop (func(arenaSlabRange) bool) which reports whether to remove an entry.
//
// Safe for concurrent use; a mutex serialises registry updates, and readers load an
// immutable snapshot published atomically. The drop callback runs under the mutex and
// must not call registry writers.
func forgetArenaSlabRanges(drop func(arenaSlabRange) bool) {
	arenaSlabRangesMu.Lock()
	defer arenaSlabRangesMu.Unlock()
	current := loadArenaSlabRanges()
	if !slices.ContainsFunc(current, drop) {
		return
	}
	next := slices.DeleteFunc(slices.Clone(current), drop)
	arenaSlabRanges.Store(&next)
	for _, entry := range current {
		if drop(entry) {
			slabPagesAdd(entry.start, entry.end, -1)
		}
	}
}

// arenaSlabContaining finds the registered byte slab that holds pointer.
//
// Takes pointer (uintptr) which is a string's data address.
//
// Returns the slab's start and end addresses, and true when some arena may reuse the
// memory. A slab whose arena is no longer reachable is not found.
func arenaSlabContaining(pointer uintptr) (start uintptr, end uintptr, found bool) {
	ranges := loadArenaSlabRanges()
	low, high := 0, len(ranges)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if ranges[middle].start <= pointer {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low == 0 || pointer >= ranges[low-1].end || !slabHolderLive(ranges[low-1].live) {
		return 0, 0, false
	}
	return ranges[low-1].start, ranges[low-1].end, true
}

// arenaSlabHolder finds the registered byte slab that holds pointer and the arena that
// installed it.
//
// Takes pointer (uintptr) which is the address to look up.
//
// Returns the slab's start address and its arena's identity, and true when found and the
// arena is still reachable.
func arenaSlabHolder(pointer uintptr) (start uintptr, owner uint64, found bool) {
	ranges := loadArenaSlabRanges()
	index, _ := slices.BinarySearchFunc(ranges, pointer+1, compareArenaSlabStart)
	if index == 0 || pointer >= ranges[index-1].end || !slabHolderLive(ranges[index-1].live) {
		return 0, 0, false
	}
	return ranges[index-1].start, ranges[index-1].owner, true
}

// loadArenaSlabRanges returns the current snapshot.
//
// Returns []arenaSlabRange which must not be modified.
func loadArenaSlabRanges() []arenaSlabRange {
	if ranges := arenaSlabRanges.Load(); ranges != nil {
		return *ranges
	}
	return nil
}

// compareArenaSlabStart orders a recorded slab against an address by its start.
//
// Takes entry (arenaSlabRange) which is the recorded slab.
// Takes pointer (uintptr) which is the address searched for.
//
// Returns int which is negative, zero or positive as entry starts before, at or after
// pointer.
func compareArenaSlabStart(entry arenaSlabRange, pointer uintptr) int {
	switch {
	case entry.start < pointer:
		return -1
	case entry.start > pointer:
		return 1
	default:
		return 0
	}
}

// slabHolderLive reports whether the arena a registry entry names is still reachable, so
// it may still reuse the slab.
//
// A collected arena's slab memory may go to a new Go allocation before the arena's
// cleanup removes its entries (runtime.AddCleanup runs after the collector frees the
// memory). A weak pointer reads nil from the moment the collector finds the arena
// unreachable, which is before any of that memory can be reused, so a lookup that checks
// it never mistakes a new heap allocation for arena storage. An entry with no weak
// pointer (one a test builds directly) counts as live.
//
// Takes live (weak.Pointer[RegisterArena]) which is the entry's weak pointer.
//
// Returns bool which is true while the arena is reachable or the entry names none.
func slabHolderLive(live weak.Pointer[RegisterArena]) bool {
	return live == (weak.Pointer[RegisterArena]{}) || live.Value() != nil
}
