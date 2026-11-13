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
	"unsafe"
)

// lazyReplacementRatio is how many times longer than its used part a slab must be for a
// lazy replacement to pay.
const lazyReplacementRatio = 4

// licenceDataSpan is one data slab's address range, [base, end).
type licenceDataSpan struct {
	// base is the slab's first address.
	base uintptr

	// end is one past the slab's last address.
	end uintptr
}

// revokedSlabScan is one pass of scanRevokedReferenceSlabs.
type revokedSlabScan struct {
	// arena is the releasing arena.
	arena *RegisterArena

	// scanned records the slabs already scanned, by start address.
	scanned []uintptr
}

// appendLicenceSlabSpan appends slab's span, as licenceSlabSpan measures it (its
// capacity), unless it is empty.
//
// Takes out ([]licenceDataSpan) which receives the span.
// Takes slab ([]T) which is the slab.
//
// Returns []licenceDataSpan which is out, extended.
func appendLicenceSlabSpan[T any](out []licenceDataSpan, slab []T) []licenceDataSpan {
	if cap(slab) == 0 {
		return out
	}
	var zero T
	base := uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
	end := base + uintptr(cap(slab))*unsafe.Sizeof(zero)
	if end == base {
		return out
	}
	return append(out, licenceDataSpan{base: base, end: end})
}

// appendLicenceListSpans appends the spans of a current slab and its retired ones, the
// lists licenceListSpan searches.
//
// Takes out ([]licenceDataSpan) which receives the spans.
// Takes current ([]T) which is the current slab.
// Takes retired ([][]T) which are the retired slabs.
//
// Returns []licenceDataSpan which is out, extended.
func appendLicenceListSpans[T any](out []licenceDataSpan, current []T, retired [][]T) []licenceDataSpan {
	out = appendLicenceSlabSpan(out, current)
	for _, slab := range retired {
		out = appendLicenceSlabSpan(out, slab)
	}
	return out
}

// appendLicenceDataSpans builds a sorted table of the slabs searched by
// licenceDataSlabSpan.
//
// Takes out ([]licenceDataSpan) which is reused for the table.
//
// Returns []licenceDataSpan which is the sorted table.
// Returns bool which is true when its spans are pairwise disjoint.
func (a *RegisterArena) appendLicenceDataSpans(out []licenceDataSpan) ([]licenceDataSpan, bool) {
	out = appendLicenceListSpans(out, a.byteSlab, a.oldByteSlabs)
	out = appendLicenceListSpans(out, a.mutByteSlab, a.oldMutByteSlabs)
	out = appendLicenceListSpans(out, a.genericBytesSlab, a.oldGenericByteSlabs)
	out = appendLicenceListSpans(out, nil, a.freeGenericByteChunks)
	out = appendLicenceListSpans(out, a.sliceHeaderSlab, a.oldSliceHeaderSlabs)
	out = appendLicenceListSpans(out, nil, a.freeSliceHeaderChunks)
	out = appendLicenceListSpans(out, a.intBackingSlab, a.oldIntBackings)
	out = appendLicenceListSpans(out, a.floatBackingSlab, a.oldFloatBackings)
	out = appendLicenceListSpans(out, a.stringBackingSlab, a.oldStringBackings)
	out = appendLicenceListSpans(out, a.boolBackingSlab, a.oldBoolBackings)
	out = appendLicenceListSpans(out, a.uintBackingSlab, a.oldUintBackings)
	for _, chunk := range a.kref.sorted {
		if chunk.end > chunk.base {
			out = append(out, licenceDataSpan{base: chunk.base, end: chunk.end})
		}
	}
	out = appendLicenceSlabSpan(out, a.stringBoxSlab)
	out = appendLicenceSlabSpan(out, a.intBoxSlab)
	out = appendLicenceSlabSpan(out, a.floatBoxSlab)
	out = appendLicenceSlabSpan(out, a.uintBoxSlab)
	out = appendLicenceSlabSpan(out, a.complexBoxSlab)
	slices.SortFunc(out, func(left, right licenceDataSpan) int {
		switch {
		case left.base < right.base:
			return -1
		case left.base > right.base:
			return 1
		case left.end < right.end:
			return -1
		case left.end > right.end:
			return 1
		default:
			return 0
		}
	})
	out = slices.Compact(out)
	for i := 1; i < len(out); i++ {
		if out[i].base < out[i-1].end {
			return out, false
		}
	}
	return out, true
}

// findLicenceDataSpan finds the span holding address in a sorted, disjoint table.
//
// Takes spans ([]licenceDataSpan) which is the table.
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
func findLicenceDataSpan(spans []licenceDataSpan, address uintptr) (start, end uintptr, found bool) {
	low, high := 0, len(spans)
	for low < high {
		middle := int(uint(low+high) >> 1)
		if spans[middle].base <= address {
			low = middle + 1
		} else {
			high = middle
		}
	}
	if low == 0 || address >= spans[low-1].end {
		return 0, 0, false
	}
	return spans[low-1].base, spans[low-1].end, true
}

// closureDataSlabSpan is licenceDataSlabSpan for a release closure running with its
// table.
//
// Takes address (uintptr) which is the address.
//
// Returns start (uintptr) which is the holding span's first address.
// Returns end (uintptr) which is one past its last byte by capacity.
// Returns found (bool) which is false when no span holds address.
//
// Panics when the licence page check is enabled and the cached span table disagrees with
// the arena slab lookup.
func (a *RegisterArena) closureDataSlabSpan(address uintptr) (start, end uintptr, found bool) {
	base, end, ok := findLicenceDataSpan(a.licenceState.closureSpans, address)
	if licencePageCheck {
		wantBase, wantEnd, wantOK := licenceDataSlabSpan(a, address)
		if wantOK != ok || ok && (wantBase != base || wantEnd != end) {
			panic(newInvariantError("release closure span of %#x is (%#x, %#x, %v); licenceDataSlabSpan says (%#x, %#x, %v)", address, base, end, ok, wantBase, wantEnd, wantOK))
		}
	}
	return base, end, ok
}

// keepUnlessRevoked returns slab when it is clean, or a fresh slab of the same length in
// its place when it is revoked, leaving the revoked one, uncleared, to the collector.
//
// Takes a (*RegisterArena) which owns the licence.
// Takes slab ([]T) which is a current slab.
// Takes tally (*licenceReleaseTally) which counts the outcome.
//
// Returns []T which is the slab to keep.
func keepUnlessRevoked[T any](a *RegisterArena, slab []T, tally *licenceReleaseTally) []T {
	return keepUnlessRevokedSized(a, slab, tally, 0)
}

// keepUnlessRevokedSized replaces revoked typed storage at the size required after
// shrinking.
//
// Takes arena (*RegisterArena) which owns the licence.
// Takes slab ([]T) which is a current slab.
// Takes tally (*licenceReleaseTally) which counts the outcome.
// Takes initial (int) which is the kind's initial length, or zero for no shrink rule.
//
// Returns []T which is slab when clean, else its replacement.
func keepUnlessRevokedSized[T any](arena *RegisterArena, slab []T, tally *licenceReleaseTally, initial int) []T {
	if cap(slab) == 0 {
		return slab
	}
	cause, revoked := arena.licenceState.revoked[uintptr(unsafe.Pointer(unsafe.SliceData(slab)))]
	if !revoked {
		tally.recycled++
		return slab
	}
	tally.abandoned++
	tally.causes[cause] = true
	length := len(slab)
	if initial > 0 && length > initial*maxArenaMultiplier {
		length = initial
	}
	return make([]T, length)
}

// dropUnlessClean is keepUnlessRevoked without the replacement.
//
// Takes arena (*RegisterArena) which owns the licence.
// Takes slab ([]T) which is a current slab.
// Takes tally (*licenceReleaseTally) which counts the outcome.
//
// Returns []T which is slab when clean, else nil.
func dropUnlessClean[T any](arena *RegisterArena, slab []T, tally *licenceReleaseTally) []T {
	if cap(slab) == 0 {
		return slab
	}
	cause, revoked := arena.licenceState.revoked[uintptr(unsafe.Pointer(unsafe.SliceData(slab)))]
	if !revoked {
		tally.recycled++
		return slab
	}
	tally.abandoned++
	tally.causes[cause] = true
	return nil
}

// lazyWorthIt reports whether a revoked slab should remain empty instead of being
// replaced at its current length.
//
// Takes length (int) which is the slab's length.
// Takes used (int) which is the epoch's use of its kind.
//
// Returns bool which is true for a lazy replacement.
func lazyWorthIt(length, used int) bool {
	return lazyReplacementRatio*used < length
}

// lazyRegrowFloor computes the first replacement size for an abandoned slab kind.
//
// Takes length (int) which is the dropped slab's length.
// Takes used (int) which is the epoch's use of the kind.
// Takes initial (int) which is the kind's initial size.
//
// Returns int which is the floor.
func lazyRegrowFloor(length, used, initial int) int {
	return min(length, max(initial, 2*used))
}

// lazyRegrowWanted reports whether one of the slabs dropRevokedSlabs leaves empty is
// revoked, so the epoch's use must be recorded for the next sizing.
//
// Returns bool which is true when one is.
func (a *RegisterArena) lazyRegrowWanted() bool {
	revoked := a.licenceState.revoked
	for _, base := range [...]uintptr{licenceSlabBase0(a.byteSlab), licenceSlabBase0(a.mutByteSlab), licenceSlabBase0(a.genericBytesSlab), licenceSlabBase0(a.sliceHeaderSlab)} {
		if base != 0 {
			if _, ok := revoked[base]; ok {
				return true
			}
		}
	}
	return false
}

// licenceSlabBase0 returns a slab's base address, or zero when it has no capacity.
//
// Takes slab ([]T) which is the slab.
//
// Returns uintptr which is the base.
func licenceSlabBase0[T any](slab []T) uintptr {
	if cap(slab) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
}

// dropRevoked removes revoked slabs from a retention or free list, leaving them,
// uncleared, to the collector.
//
// Takes arena (*RegisterArena) which owns the licence.
// Takes list ([][]T) which is the list, compacted in place.
// Takes tally (*licenceReleaseTally) which counts the outcome.
// Takes onDrop (func([]T)) which is called for each dropped slab, or nil.
//
// Returns [][]T which is the compacted list.
func dropRevoked[T any](arena *RegisterArena, list [][]T, tally *licenceReleaseTally, onDrop func([]T)) [][]T {
	write := 0
	for _, slab := range list {
		cause, revoked := arena.licenceState.revoked[uintptr(unsafe.Pointer(unsafe.SliceData(slab)))]
		if cap(slab) == 0 || !revoked {
			tally.recycled++
			list[write] = slab
			write++
			continue
		}
		tally.abandoned++
		tally.causes[cause] = true
		if onDrop != nil {
			onDrop(slab)
		}
	}
	clear(list[write:])
	return list[:write]
}

// closeOverRevoked repeatedly revokes storage referenced by revoked reference-bearing
// slabs until no further revocations occur. It reads those slabs in an owner section
// (enterOwnerSection) when the VM is shared only because it exported guest code.
func (a *RegisterArena) closeOverRevoked() {
	if !licenceExactProbes {
		return
	}
	strings, headers := a.revokedReferenceSlabs()
	if !strings && !headers {
		return
	}
	state := &a.licenceState
	shared, owner := a.enterOwnerSection(a.licenceShared(0))
	if owner != nil {
		defer owner.leaveOwnerSection()
	}
	if shared {
		if licenceCounting {
			a.stats().sharedWalks.Add(1)
		}
		if headers {
			a.revokeEpoch(epochSharedContainer, noHandOffCause)
			a.revokeBlind()
		} else {
			a.revokeBlindBytes()
		}
		return
	}
	scanned := state.closureScanned[:0]

	state.closureSpans, state.closureSpansOK = a.appendLicenceDataSpans(state.closureSpans[:0])
	defer func() { state.closureSpansOK = false }()

	for {
		before := state.revision
		scanned = a.scanRevokedReferenceSlabs(scanned)
		if state.revision == before {
			state.closureScanned = scanned[:0]
			return
		}
	}
}

// revokedReferenceSlabs reports which reference-bearing slabs that hold anything are
// revoked, alone or with the epoch.
//
// Returns strings (bool) which is true for a string backing or string box slab.
// Returns headers (bool) which is true for a slice-header slab.
func (a *RegisterArena) revokedReferenceSlabs() (strings bool, headers bool) {
	state := &a.licenceState
	if state.epochRevoked {
		return a.stringBackingIndex > 0 || len(a.oldStringBackings) > 0 || a.stringBoxIndex > 0,
			a.sliceHeaderIndex > 0 || len(a.oldSliceHeaderSlabs) > 0
	}
	if len(state.revoked) == 0 {
		return false, false
	}
	revoked := func(base uintptr) bool {
		_, found := state.revoked[base]
		return found
	}
	strings = revoked(slabBaseOf(a.stringBackingSlab)) || revoked(slabBaseOf(a.stringBoxSlab)) ||
		listRevoked(a.oldStringBackings, revoked)
	headers = revoked(slabBaseOf(a.sliceHeaderSlab)) || listRevoked(a.oldSliceHeaderSlabs, revoked) ||
		listRevoked(a.freeSliceHeaderChunks, revoked)
	return strings, headers
}

// scanRevokedReferenceSlabs revokes what each revoked, not yet scanned, reference-bearing
// slab references. Under a revoked epoch every such slab is revoked, and only what lies
// outside the arena still needs naming.
//
// Takes scanned ([]uintptr) which records the slabs already scanned.
//
// Returns []uintptr which is scanned extended by the slabs this pass scanned.
func (a *RegisterArena) scanRevokedReferenceSlabs(scanned []uintptr) []uintptr {
	scan := revokedSlabScan{arena: a, scanned: scanned}
	scan.strings(a.stringBackingSlab, a.stringBackingIndex)
	for _, slab := range a.oldStringBackings {
		scan.strings(slab, len(slab))
	}
	scan.strings(a.stringBoxSlab, a.stringBoxIndex)
	scan.headers(a.sliceHeaderSlab, a.sliceHeaderIndex)
	for _, slab := range a.oldSliceHeaderSlabs {
		scan.headers(slab, len(slab))
	}
	for _, slab := range a.freeSliceHeaderChunks {
		scan.headers(slab, len(slab))
	}
	return scan.scanned
}

// take reports whether the slab at base is revoked and not yet scanned, and records it as
// scanned when it is.
//
// Takes base (uintptr) which is the slab's start address, or zero for an empty slab.
//
// Returns bool which is true when the slab is to be scanned now.
func (scan *revokedSlabScan) take(base uintptr) bool {
	if base == 0 || slices.Contains(scan.scanned, base) {
		return false
	}
	state := &scan.arena.licenceState
	if _, found := state.revoked[base]; !found && !state.epochRevoked {
		return false
	}
	scan.scanned = append(scan.scanned, base)
	return true
}

// strings revokes the string data a revoked string slab's used elements reference.
//
// Takes slab ([]string) which is the slab.
// Takes used (int) which is how many leading elements are in use.
func (scan *revokedSlabScan) strings(slab []string, used int) {
	if !scan.take(slabBaseOf(slab)) {
		return
	}
	for _, element := range slab[:min(used, len(slab))] {
		if len(element) > 0 {
			scan.arena.revokeClosureAddress(uintptr(unsafe.Pointer(unsafe.StringData(element))))
		}
	}
}

// headers revokes the backings a revoked slice-header slab's used headers reference.
//
// Takes slab ([]arenaSliceHeader) which is the slab.
// Takes used (int) which is how many leading headers are in use.
func (scan *revokedSlabScan) headers(slab []arenaSliceHeader, used int) {
	if !scan.take(slabBaseOf(slab)) {
		return
	}
	for _, header := range slab[:min(used, len(slab))] {
		if header.Cap != 0 {
			scan.arena.revokeClosureAddress(uintptr(header.Data))
		}
	}
}

// revokeClosureAddress is revokeAddress for an address found in a revoked slab, counting
// what it newly revokes.
//
// Takes address (uintptr) which is the address.
func (a *RegisterArena) revokeClosureAddress(address uintptr) {
	if !licenceRegisteredPage(a, address) {
		return
	}
	before := a.licenceState.revision
	if state := &a.licenceState; state.closureSpansOK && address != 0 {
		if base, _, ok := a.closureDataSlabSpan(address); ok {
			a.revokeSlab(base, noHandOffCause)
		} else {
			a.revokeNonDataAddress(address, noHandOffCause)
		}
	} else {
		a.revokeAddress(address, noHandOffCause)
	}
	if a.licenceState.revision != before {
		if licenceCounting {
			a.stats().closureRevokes.Add(1)
		}
	}
}

// slabBaseOf returns slab's start address, or zero for an empty slab.
//
// Takes slab ([]T) which is the slab.
//
// Returns uintptr which is the address.
func slabBaseOf[T any](slab []T) uintptr {
	if cap(slab) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(slab)))
}

// listRevoked reports whether any slab in list is revoked.
//
// Takes list ([][]T) which is a retention or free list.
// Takes revoked (func(uintptr) bool) which tests one start address.
//
// Returns bool which is true when one is.
func listRevoked[T any](list [][]T, revoked func(uintptr) bool) bool {
	for _, slab := range list {
		if revoked(slabBaseOf(slab)) {
			return true
		}
	}
	return false
}

// dropRevokedSlabs replaces each revoked current slab with a fresh one and removes each
// revoked slab from the retention and free lists. A typed-backing or box slab revoked
// page by page keeps its longest run of clean pages instead (keepCleanPages).
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) dropRevokedSlabs(tally *licenceReleaseTally) {
	if a.lazyRegrowWanted() {
		a.licenceState.regrow.record(a)
	}
	a.dropRevokedByteSlabs(tally)
	a.intBackingSlab = keepCleanPagesSized(a, a.intBackingSlab, &a.intBackingIndex, tally, InitialIntBackingSize)
	a.oldIntBackings = dropRevoked(a, a.oldIntBackings, tally, nil)
	a.floatBackingSlab = keepCleanPagesSized(a, a.floatBackingSlab, &a.floatBackingIndex, tally, initialFloatBackingSize)
	a.oldFloatBackings = dropRevoked(a, a.oldFloatBackings, tally, nil)
	a.stringBackingSlab = keepCleanPagesSized(a, a.stringBackingSlab, &a.stringBackingIndex, tally, initialStringBackingSize)
	a.oldStringBackings = dropRevoked(a, a.oldStringBackings, tally, nil)
	a.boolBackingSlab = keepCleanPagesSized(a, a.boolBackingSlab, &a.boolBackingIndex, tally, initialBoolBackingSize)
	a.oldBoolBackings = dropRevoked(a, a.oldBoolBackings, tally, nil)
	a.uintBackingSlab = keepCleanPagesSized(a, a.uintBackingSlab, &a.uintBackingIndex, tally, initialUintBackingSize)
	a.oldUintBackings = dropRevoked(a, a.oldUintBackings, tally, nil)
	a.stringBoxSlab = keepCleanPages(a, a.stringBoxSlab, &a.stringBoxIndex, tally)
	a.intBoxSlab = keepCleanPages(a, a.intBoxSlab, &a.intBoxIndex, tally)
	a.floatBoxSlab = keepCleanPages(a, a.floatBoxSlab, &a.floatBoxIndex, tally)
	a.uintBoxSlab = keepCleanPages(a, a.uintBoxSlab, &a.uintBoxIndex, tally)
	a.complexBoxSlab = keepCleanPages(a, a.complexBoxSlab, &a.complexBoxIndex, tally)
	a.krefDropRevoked(tally)
	a.dropRevokedClosureChunks(tally)
}

// dropRevokedByteSlabs drops or keeps the byte, mutable byte, generic byte and slice
// header slabs and their lists, replacing a revoked current slab with a fresh one, lazily
// when the slab is mostly unused.
//
// Takes tally (*licenceReleaseTally) which counts the outcome.
func (a *RegisterArena) dropRevokedByteSlabs(tally *licenceReleaseTally) {
	floor := &a.licenceState.lazyFloor
	if used := a.byteIndex + a.slabRetiredSinceReset.bytes; lazyWorthIt(len(a.byteSlab), used) {
		if fresh := dropUnlessClean(a, a.byteSlab, tally); unsafe.SliceData(fresh) != unsafe.SliceData(a.byteSlab) {
			floor.bytes = lazyRegrowFloor(len(a.byteSlab), used, InitialByteSlabSize)
			a.replaceByteSlab(fresh, false)
			a.byteIndex = 0
		}
	} else if fresh := keepUnlessRevoked(a, a.byteSlab, tally); unsafe.SliceData(fresh) != unsafe.SliceData(a.byteSlab) {
		a.replaceByteSlab(fresh, false)
	}
	a.oldByteSlabs = dropRevoked(a, a.oldByteSlabs, tally, forgetArenaSlab)
	if lazyWorthIt(len(a.mutByteSlab), a.mutByteIndex) {
		if fresh := dropUnlessClean(a, a.mutByteSlab, tally); unsafe.SliceData(fresh) != unsafe.SliceData(a.mutByteSlab) {
			floor.mutBytes = lazyRegrowFloor(len(a.mutByteSlab), a.mutByteIndex, initialMutByteSlabSize)
			a.mutByteSlab, a.mutByteIndex = fresh, 0
		}
	} else {
		a.mutByteSlab = keepUnlessRevoked(a, a.mutByteSlab, tally)
	}
	a.oldMutByteSlabs = dropRevoked(a, a.oldMutByteSlabs, tally, nil)
	if used := a.genericBytesIndex + a.slabRetiredSinceReset.genericBytes; lazyWorthIt(len(a.genericBytesSlab), used) {
		if fresh := dropUnlessClean(a, a.genericBytesSlab, tally); unsafe.SliceData(fresh) != unsafe.SliceData(a.genericBytesSlab) {
			floor.genericBytes = lazyRegrowFloor(len(a.genericBytesSlab), used, initialGenericBytesCapacity)
			a.genericBytesSlab, a.genericBytesIndex = fresh, 0
		}
	} else {
		a.genericBytesSlab = keepUnlessRevoked(a, a.genericBytesSlab, tally)
	}
	a.oldGenericByteSlabs = dropRevoked(a, a.oldGenericByteSlabs, tally, nil)
	a.freeGenericByteChunks = dropRevoked(a, a.freeGenericByteChunks, tally, nil)
	if used := a.sliceHeaderIndex + a.slabRetiredSinceReset.sliceHeaders; lazyWorthIt(len(a.sliceHeaderSlab), used) {
		if fresh := dropUnlessClean(a, a.sliceHeaderSlab, tally); unsafe.SliceData(fresh) != unsafe.SliceData(a.sliceHeaderSlab) {
			floor.sliceHeaders = lazyRegrowFloor(len(a.sliceHeaderSlab), used, initialSliceHeaderCapacity)
			a.sliceHeaderSlab, a.sliceHeaderIndex = fresh, 0
		}
	} else {
		a.sliceHeaderSlab = keepUnlessRevoked(a, a.sliceHeaderSlab, tally)
	}
	a.oldSliceHeaderSlabs = dropRevoked(a, a.oldSliceHeaderSlabs, tally, nil)
	a.freeSliceHeaderChunks = dropRevoked(a, a.freeSliceHeaderChunks, tally, nil)
}
