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
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func sliceStart[T any](s []T) uintptr { return uintptr(unsafe.Pointer(unsafe.SliceData(s))) }

func TestPageRevocationRevokesOnlyThePagesAHandOffReaches(t *testing.T) {
	arena := pagedArena(t)
	backing := arena.AllocIntBacking(4)
	base := sliceStart(arena.intBackingSlab)
	require.Equal(t, base, sliceStart(backing), "test harness: the first backing starts the slab")
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(backing))
	require.Contains(t, arena.licenceState.revoked, base, "the slab counts as revoked for everything that reads slabs")
	set, paged := arena.licenceState.pages[base]
	require.True(t, paged, "page revocation records the pages")
	require.Equal(t, 1, set.count(), "a 32-byte backing reaches one page")
	require.True(t, set.revoked(0))

	box := arena.allocIntBox(7)
	boxBase := sliceStart(arena.intBoxSlab)
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(box))
	boxSet, boxPaged := arena.licenceState.pages[boxBase]
	require.True(t, boxPaged, "a box is revoked by page too")
	require.Equal(t, 1, boxSet.count())

	arena.revokeAddress(sliceStart(backing), noHandOffCause)
	_, stillPaged := arena.licenceState.pages[base]
	require.False(t, stillPaged, "a revocation without an extent revokes the whole slab")
	arena.Reset()
}

func TestPageRevocationReleaseKeepsTheCleanRunAndSkipsRetiredPages(t *testing.T) {
	arena := pagedArena(t)
	handed := arena.AllocIntBacking(4)
	for i := range handed {
		handed[i] = int64(100 + i)
	}
	base, length := sliceStart(arena.intBackingSlab), len(arena.intBackingSlab)
	for range 6 {
		arena.AllocIntBacking(4)
	}
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(handed))
	box := arena.allocStringBox(arenaString(arena, "boxed"))
	boxBase, boxLength := sliceStart(arena.stringBoxSlab), len(arena.stringBoxSlab)
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(box))
	for range 20 {
		arena.allocStringBox("other")
	}
	kept := pageRevocationStatistics().RunsKept
	arena.Reset()
	perPage := licencePageBytes / 8
	require.Equal(t, base+licencePageBytes, sliceStart(arena.intBackingSlab), "the run starts after the retired page")
	require.Equal(t, length-perPage, len(arena.intBackingSlab))
	require.Equal(t, boxBase+licencePageBytes, sliceStart(arena.stringBoxSlab))
	require.Equal(t, boxLength-licencePageBytes/16, len(arena.stringBoxSlab))
	require.Equal(t, kept+2, pageRevocationStatistics().RunsKept)
	require.Equal(t, "boxed", *box, "Reset's box clear skips the retired page")

	arena.attachLicencePolicy(&VMLimits{})
	for range 64 {
		reused := arena.AllocIntBacking(4)
		for i := range reused {
			reused[i] = -1
		}
		*arena.allocStringBox("reused") = "overwritten"
	}
	require.Equal(t, []int64{100, 101, 102, 103}, handed, "the next run's allocations skip the retired page")
	require.Equal(t, "boxed", *box)
	arena.Reset()
}

func handOffTwoPageExtent(t *testing.T) []int64 {
	t.Helper()
	arena := pagedArena(t)
	perPage := licencePageBytes / 8
	handed := arena.AllocIntBacking(perPage + 4)
	for i := range handed {
		handed[i] = int64(i)
	}
	arena.AllocIntBacking(4)
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(handed))
	arena.Reset()
	arena.attachLicencePolicy(&VMLimits{})
	for range 64 {
		reused := arena.AllocIntBacking(8)
		for i := range reused {
			reused[i] = -1
		}
	}
	arena.Reset()
	return handed
}

func TestPageRevocationRetiresEveryPageOfTheExtent(t *testing.T) {
	if !arenaUsesUnsafeSlabs || !licenceExactProbes {
		t.Skip("page-granular revocation needs exact probes")
	}
	handed := handOffTwoPageExtent(t)
	for i, v := range handed {
		require.Equal(t, int64(i), v, "element %d of a handed-off two-page backing", i)
	}
}

func TestPageRevocationCapacityFallbackRevokesTheWholeSlab(t *testing.T) {
	arena := pagedArena(t)
	tail := arena.AllocIntBacking(len(arena.intBackingSlab))
	base := sliceStart(arena.intBackingSlab)

	header := struct {
		data     unsafe.Pointer
		len, cap int
	}{unsafe.Pointer(unsafe.SliceData(tail)), 1, len(tail) + 64}
	beyond := *(*[]int64)(unsafe.Pointer(&header))
	fallbacks := pageRevocationStatistics().CapacityFallbacks
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(beyond))
	require.Equal(t, fallbacks+1, pageRevocationStatistics().CapacityFallbacks, "an extent past the slab's end fails the run-time check that every extent lies in its slab")
	require.Contains(t, arena.licenceState.revoked, base)
	_, paged := arena.licenceState.pages[base]
	require.False(t, paged, "the fallback revokes the whole slab")
	arena.Reset()
	require.NotEqual(t, base, sliceStart(arena.intBackingSlab), "and release replaces it")
}

func TestPageRevocationShortRunReplacesTheSlab(t *testing.T) {
	arena := pagedArena(t)
	most := arena.AllocIntBacking(len(arena.intBackingSlab) - 8)
	base := sliceStart(arena.intBackingSlab)
	noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(most))
	short := pageRevocationStatistics().RunsTooShort
	arena.Reset()
	require.NotEqual(t, base, sliceStart(arena.intBackingSlab), "a run under half the slab is not kept")
	require.Equal(t, short+1, pageRevocationStatistics().RunsTooShort)
	arena.Reset()
}

func TestRevokedPageSetRuns(t *testing.T) {
	set := &revokedPageSet{}
	require.Equal(t, 2, set.mark(1, 2))
	require.Zero(t, set.mark(2, 2))
	set.mark(70, 70)
	start, end := set.longestClean(80)
	require.Equal(t, []int{3, 70}, []int{start, end})
	start, end = set.longestClean(2)
	require.Equal(t, []int{0, 1}, []int{start, end})
	full := &revokedPageSet{}
	full.mark(0, 3)
	start, end = full.longestClean(4)
	require.Equal(t, start, end, "no clean page")
	require.Equal(t, 3, set.count())
}

func TestPageRevocationReleaseAllocatesAtMostTheReplacement(t *testing.T) {
	if raceDetectorOn {
		t.Skip("allocation counts are not meaningful under the race detector")
	}
	arena := newArena(t)
	limits := &VMLimits{}
	var escaped []string
	value := reflect.ValueOf(&escaped).Elem()
	cycle := func() {
		escaped = arena.AllocStringBacking(2)
		arena.revokeHandOff(handOffIdentityKept, value, false, false)
		arena.Reset()
		arena.attachLicencePolicy(limits)
	}
	cycle()
	kept := pageRevocationStatistics().RunsKept
	require.LessOrEqual(t, testing.AllocsPerRun(50, cycle), 1.0, "a kept run costs nothing, a replacement one slab")
	if arenaUsesUnsafeSlabs && licenceExactProbes {
		require.Greater(t, pageRevocationStatistics().RunsKept, kept, "test harness: page revocation kept runs")
	}
	require.NotNil(t, unsafe.SliceData(escaped))
	arena.Reset()
}
