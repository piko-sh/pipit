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
)

// The slab registry lets store barriers recognise strings in another VM's arena. Every
// installed byte slab must be registered to prevent foreign strings from escaping
// uncloned.
//
// Readers use immutable snapshots published atomically. Writers copy the snapshot under
// a mutex, and removing a slab does not invalidate memory still referenced elsewhere.

// arenaSlabRange is one recorded byte slab.
type arenaSlabRange struct {
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
// Takes slab ([]byte) which is the byte slab; an empty slab is ignored.
//
// Safe for concurrent use; a mutex serialises registry updates, and readers load an
// immutable snapshot published atomically.
func recordArenaSlab(owner uint64, slab []byte) {
	if len(slab) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	entry := arenaSlabRange{start: start, end: start + uintptr(len(slab)), owner: owner}
	arenaSlabRangesMu.Lock()
	defer arenaSlabRangesMu.Unlock()
	current := loadArenaSlabRanges()
	index, found := slices.BinarySearchFunc(current, start, compareArenaSlabStart)
	if found && current[index] == entry {
		return
	}
	next := make([]arenaSlabRange, 0, len(current)+1)
	next = append(next, current[:index]...)
	next = append(next, entry)
	next = append(next, current[index:]...)
	arenaSlabRanges.Store(&next)
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

// forgetArenaSlabOwner removes every slab owner recorded. It runs as the arena's cleanup.
//
// Takes owner (uint64) which identifies the collected arena.
func forgetArenaSlabOwner(owner uint64) {
	forgetArenaSlabRanges(func(entry arenaSlabRange) bool { return entry.owner == owner })
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
}

// arenaSlabContaining finds the registered byte slab that holds pointer.
//
// Takes pointer (uintptr) which is a string's data address.
//
// Returns the slab's start and end addresses, and true when some arena may reuse the
// memory.
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
	if low == 0 || pointer >= ranges[low-1].end {
		return 0, 0, false
	}
	return ranges[low-1].start, ranges[low-1].end, true
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
