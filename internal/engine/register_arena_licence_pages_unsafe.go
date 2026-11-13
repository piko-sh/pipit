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
	"math/bits"
	"unsafe"
)

// revokedPageWordBits is the number of pages one word of a revoked-page set records.
const revokedPageWordBits = 64

const (
	// licencePageBytes is the page size of page-granular revocation. It divides every
	// element size of the slabs it applies to (1, 8 and 16 bytes).
	licencePageBytes = 256
)

// revokedPageSet is the set of revoked pages of one slab, one bit per page from its
// start.
type revokedPageSet struct {
	// words holds the bits.
	words []uint64
}

// keptPageRun bounds a run of clean pages a release kept as a slab, for the audit.
type keptPageRun struct {
	// start and end bound the run.
	start, end uintptr
}

// revokeExtent revokes the host-accessible extent [address, address+size) of a slice
// backing, pointee or boxed value.
//
// Takes address (uintptr) which is the extent's start.
// Takes size (uintptr) which is its length in bytes.
func (w *licenceWalk) revokeExtent(address, size uintptr) {
	arena := w.arena
	if size == 0 || !arena.pageRevocationApplies() {
		w.revoke(address)
		return
	}
	if address == 0 || address >= w.cachedBase && address < w.cachedEnd || w.cachedSpan(address) {
		return
	}
	if !licenceRegisteredPage(arena, address) {
		return
	}
	base, end, ok := w.dataSlabSpan(address)
	if !ok || !arena.pagedSlab(base) {
		w.revoke(address)
		return
	}
	if w.shared {
		if _, revoked := arena.licenceState.revoked[base]; !revoked {
			w.fresh = append(w.fresh, base)
		}
	}
	arena.revokePages(base, end, address, size, w.cause)
	if _, paged := arena.licenceState.pages[base]; !paged {
		w.cacheSpan(base, end)
	}
}

// pagedSlab reports whether base starts a current typed-backing or box slab that supports
// per-page revocation.
//
// Takes base (uintptr) which is a data slab's start.
//
// Returns bool which is true for such a slab.
func (a *RegisterArena) pagedSlab(base uintptr) bool {
	if base == 0 {
		return false
	}
	switch base {
	case slabBaseOf(a.intBackingSlab), slabBaseOf(a.floatBackingSlab), slabBaseOf(a.stringBackingSlab),
		slabBaseOf(a.boolBackingSlab), slabBaseOf(a.uintBackingSlab), slabBaseOf(a.stringBoxSlab),
		slabBaseOf(a.intBoxSlab), slabBaseOf(a.floatBoxSlab), slabBaseOf(a.uintBoxSlab), slabBaseOf(a.complexBoxSlab):
		return true
	default:
		return false
	}
}

// keepCleanPages retains a sufficiently large contiguous run of unrevoked slab pages.
//
// Takes a (*RegisterArena) which owns the licence.
// Takes slab ([]T) which is a current typed-backing or box slab.
// Takes index (*int) which is the slab's bump index.
// Takes tally (*licenceReleaseTally) which counts the outcome.
//
// Returns []T which is the slab to keep.
func keepCleanPages[T any](a *RegisterArena, slab []T, index *int, tally *licenceReleaseTally) []T {
	return keepCleanPagesSized(a, slab, index, tally, 0)
}

// keepCleanPagesSized is keepCleanPages for a typed backing whose replacement Reset's
// shrink rule sizes (keepUnlessRevokedSized).
//
// Takes arena (*RegisterArena) which owns the licence.
// Takes slab ([]T) which is the kind's current slab.
// Takes index (*int) which is its bump index, rebased onto a kept run.
// Takes tally (*licenceReleaseTally) which counts the outcome.
// Takes initial (int) which is the kind's initial length, or zero for no shrink rule.
//
// Returns []T which is the slab, a kept run of it, or its replacement.
func keepCleanPagesSized[T any](arena *RegisterArena, slab []T, index *int, tally *licenceReleaseTally, initial int) []T {
	if cap(slab) == 0 {
		return slab
	}
	base := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	state := &arena.licenceState
	set, paged := state.pages[base]
	if !paged || !arena.pageRevocationApplies() {
		return keepUnlessRevokedSized(arena, slab, tally, initial)
	}
	var zero T
	elementBytes := int(unsafe.Sizeof(zero))
	perPage := licencePageBytes / elementBytes
	length := len(slab)
	pages := (length + perPage - 1) / perPage
	first, final := set.longestClean(pages)
	start, end := first*perPage, min(final*perPage, length)
	if 2*(end-start) < length || end-start < *index {
		if licenceCounting {
			pageCounters.runsTooShort.Add(1)
		}
		arena.forgetRevokedPages(base)
		return keepUnlessRevokedSized(arena, slab, tally, initial)
	}
	retired := set.count()
	delete(state.revoked, base)
	state.coverCoveredKnown = false
	arena.forgetRevokedPages(base)
	run := slab[start:end:end]
	*index = min(max(*index-start, 0), len(run))
	runStart := uintptr(unsafe.Pointer(unsafe.SliceData(run)))
	state.keptRuns = append(state.keptRuns, keptPageRun{start: runStart, end: runStart + uintptr(len(run)*elementBytes)})
	if licenceCounting {
		pageCounters.runsKept.Add(1)
		pageCounters.pagesRetired.Add(int64(retired))
	}
	tally.recycled++
	return run
}

// mark sets the bits of pages first to last, inclusive.
//
// Takes first (uintptr) which is the first page to mark, inclusive.
// Takes last (uintptr) which is the last page to mark, inclusive.
//
// Returns int which is the number of pages newly set.
func (set *revokedPageSet) mark(first, last uintptr) int {
	added := 0
	for page := first; page <= last; page++ {
		word, bit := page/revokedPageWordBits, uint64(1)<<(page%revokedPageWordBits)
		for uintptr(len(set.words)) <= word {
			set.words = append(set.words, 0)
		}
		if set.words[word]&bit == 0 {
			set.words[word] |= bit
			added++
		}
	}
	return added
}

// revoked reports whether page is set.
//
// Takes page (int) which is the page.
//
// Returns bool which is true when it is revoked.
func (set *revokedPageSet) revoked(page int) bool {
	word := page / revokedPageWordBits
	return word < len(set.words) && set.words[word]&(uint64(1)<<(page%revokedPageWordBits)) != 0
}

// count returns the number of pages set.
//
// Returns int which is the count.
func (set *revokedPageSet) count() int {
	total := 0
	for _, word := range set.words {
		total += bits.OnesCount64(word)
	}
	return total
}

// longestClean finds the longest run of clean pages among the first pages pages.
//
// Takes pages (int) which is the slab's page count.
//
// Returns start (int) which is the first clean page in the selected run.
// Returns end (int) which is the exclusive end of the run; equal to start when every page
// is revoked.
func (set *revokedPageSet) longestClean(pages int) (start int, end int) {
	runStart := 0
	for page := 0; page <= pages; page++ {
		if page < pages && !set.revoked(page) {
			continue
		}
		if page-runStart > end-start {
			start, end = runStart, page
		}
		runStart = page + 1
	}
	return start, end
}

// pageRevocationApplies reports whether the arena supports page-granular revocation.
//
// It requires a licensed arena in an exact-probe build using unsafe slabs.
//
// Returns bool which is true when page-granular revocation applies.
func (a *RegisterArena) pageRevocationApplies() bool {
	return arenaUsesUnsafeSlabs && licenceExactProbes && a.licensed
}

// revokePages revokes the pages of the slab [base, end) that the extent [address,
// address+size) intersects. A slab revoked whole stays so; an extent that leaves the slab
// revokes it whole (the run-time check that host access stays within the extent).
//
// Takes base (uintptr) which is the slab's inclusive starting address.
// Takes end (uintptr) which is the slab's exclusive ending address.
// Takes address (uintptr) which is the extent's starting address.
// Takes size (uintptr) which is the extent's size in bytes.
// Takes cause (HandOffCause) which is the hand-off's cause.
func (a *RegisterArena) revokePages(base, end, address, size uintptr, cause HandOffCause) {
	state := &a.licenceState
	_, revoked := state.revoked[base]
	set, paged := state.pages[base]
	if revoked && !paged {
		return
	}
	last := address + size - 1
	if address < base || last < address || last >= end {
		if licenceCounting {
			pageCounters.capacityFallbacks.Add(1)
		}
		a.revokeSlab(base, cause)
		return
	}
	first, final := (address-base)/licencePageBytes, (last-base)/licencePageBytes
	if !revoked {
		a.revokeSlab(base, cause)
		if _, recorded := state.revoked[base]; !recorded {
			return
		}
		set = a.revokedPageSetFor(base)
	}
	if added := set.mark(first, final); added != 0 {
		if paged {
			state.revision++
		}
		if licenceCounting {
			pageCounters.pagesRevoked.Add(int64(added))
		}
	}
}

// revokedPageSetFor returns an empty page set registered for the slab at base, reusing a
// set an earlier epoch freed.
//
// Takes base (uintptr) which is the slab's start.
//
// Returns *revokedPageSet which is the slab's set.
func (a *RegisterArena) revokedPageSetFor(base uintptr) *revokedPageSet {
	state := &a.licenceState
	if state.pages == nil {
		state.pages = make(map[uintptr]*revokedPageSet)
	}
	var set *revokedPageSet
	if count := len(state.pageSetsFree); count != 0 {
		set = state.pageSetsFree[count-1]
		state.pageSetsFree = state.pageSetsFree[:count-1]
		clear(set.words)
		set.words = set.words[:0]
	} else {
		set = &revokedPageSet{}
	}
	state.pages[base] = set
	return set
}

// forgetRevokedPages drops the page set of the slab at base, if any.
//
// The slab is now revoked whole, or has left the arena.
//
// Takes base (uintptr) which is the slab's start.
//
// Returns bool which is true when the slab had one.
func (a *RegisterArena) forgetRevokedPages(base uintptr) bool {
	state := &a.licenceState
	set, paged := state.pages[base]
	if !paged {
		return false
	}
	delete(state.pages, base)
	state.pageSetsFree = append(state.pageSetsFree, set)
	return true
}

// clearRetiredPages forgets every page set, at the start of an epoch.
func (a *RegisterArena) clearRetiredPages() {
	state := &a.licenceState
	for base := range state.pages {
		a.forgetRevokedPages(base)
	}
	state.keptRuns = state.keptRuns[:0]
}
