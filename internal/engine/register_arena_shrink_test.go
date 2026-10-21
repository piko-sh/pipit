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

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
)

func arenaAbortError(body func()) (err error) {
	defer func() {
		if recovered, ok := recover().(error); ok {
			err = recovered
		}
	}()
	body()
	return nil
}

func keptStringBackingArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := NewRegisterArena()
	fillTypedBackings(arena, initialStringBackingSize*maxArenaMultiplier*4, 1)
	arena.Reset()
	require.Greater(t, len(arena.stringBackingSlab), initialStringBackingSize*maxArenaMultiplier, "test harness: the string slab is kept")
	return arena
}

func TestSetRunBudgetKeepsABackingSlabInUse(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	held := arena.AllocStringBacking(initialStringBackingSize * maxArenaMultiplier * 4)
	held[0] = "still read by the run"
	slab := unsafe.SliceData(arena.stringBackingSlab)

	arena.setRunBudget(1)
	require.Equal(t, slab, unsafe.SliceData(arena.stringBackingSlab), "a slab that has handed out memory is not replaced")
	require.Equal(t, "still read by the run", held[0])
}

func TestApplyCallbackLimitsBoundsAnUnusedKeptSlab(t *testing.T) {
	t.Parallel()

	vm := newGCTestVM()
	vm.Arena.growStringBackingSlab(initialStringBackingSize * maxArenaMultiplier * 4)
	require.Zero(t, vm.Arena.stringBackingIndex, "test harness: the grown slab has handed nothing out")

	vm.applyCallbackLimits(VMLimits{MaxArenaBytes: initialStringBackingSize * uint64(arenaSizeString) * keptBackingSlabBudgetShare})
	require.Len(t, vm.Arena.stringBackingSlab, initialStringBackingSize, "a cached callback arena keeps no slab over the callback budget's share")
}

func TestSetRunBudgetChargesKeptBackingSlabs(t *testing.T) {
	t.Parallel()

	arena := keptStringBackingArena(t)
	kept := len(arena.stringBackingSlab)
	excess := uint64(kept-initialStringBackingSize) * arenaSizeString
	budget := uint64(kept) * arenaSizeString * keptBackingSlabBudgetShare

	arena.setRunBudget(budget)
	require.Len(t, arena.stringBackingSlab, kept, "a kept slab within the share is carried into the run")
	require.Equal(t, excess, arena.totalAllocatedBytes, "the kept capacity beyond the initial slab is charged to the run")

	grow := int((budget-excess/2)/arenaSizeInt64) + InitialIntBackingSize
	fresh := NewRegisterArena()
	fresh.setRunBudget(budget)
	require.NoError(t, arenaAbortError(func() { fresh.GrowIntBackingSlab(grow) }), "a fresh arena has room for the grow")
	require.ErrorIs(t, arenaAbortError(func() { arena.GrowIntBackingSlab(grow) }), fault.ErrArenaBudgetExceeded,
		"the kept slab and the grow together exceed the budget")
}

func TestSetRunBudgetCapsTheBytesKeptAcrossTypes(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	fillTypedBackings(arena, initialStringBackingSize*maxArenaMultiplier*4, InitialIntBackingSize*maxArenaMultiplier*4)
	arena.Reset()
	stringBytes := uint64(len(arena.stringBackingSlab)) * arenaSizeString
	intBytes := uint64(len(arena.intBackingSlab)) * arenaSizeInt64
	require.Greater(t, len(arena.intBackingSlab), InitialIntBackingSize*maxArenaMultiplier, "test harness: the int slab is kept")
	budget := max(stringBytes, intBytes) * keptBackingSlabBudgetShare

	arena.setRunBudget(budget)
	stringKept := len(arena.stringBackingSlab) > initialStringBackingSize
	intKept := len(arena.intBackingSlab) > InitialIntBackingSize
	require.True(t, stringKept || intKept, "the larger slab that fits the share is kept")
	require.False(t, stringKept && intKept, "the slabs kept across a Reset share one cap, so both do not fit")
	require.Equal(t, keptBackingBytes(arena)-keptBackingBytesAtInitialSize(), arena.totalAllocatedBytes, "what is kept is charged")
}

func TestResetLeavesKeptBackingSlabsUnchargedUntilTheNextRun(t *testing.T) {
	t.Parallel()

	arena := keptStringBackingArena(t)
	require.Zero(t, arena.totalAllocatedBytes, "an idle pooled arena carries no charge")
	arena.setRunBudget(0)
	charged := arena.totalAllocatedBytes
	require.NotZero(t, charged)
	arena.setRunBudget(0)
	require.Equal(t, charged, arena.totalAllocatedBytes, "setting the budget again does not charge the kept slabs twice")
}

func keptBackingBytes(arena *RegisterArena) uint64 {
	return uint64(len(arena.intBackingSlab))*arenaSizeInt64 +
		uint64(len(arena.floatBackingSlab))*arenaSizeFloat64 +
		uint64(len(arena.stringBackingSlab))*arenaSizeString +
		uint64(len(arena.boolBackingSlab))*arenaSizeBool +
		uint64(len(arena.uintBackingSlab))*arenaSizeUint64
}

func keptBackingBytesAtInitialSize() uint64 {
	return InitialIntBackingSize*arenaSizeInt64 +
		initialFloatBackingSize*arenaSizeFloat64 +
		initialStringBackingSize*arenaSizeString +
		initialBoolBackingSize*arenaSizeBool +
		initialUintBackingSize*arenaSizeUint64
}

func dirtyEveryTypedBacking(arena *RegisterArena, n int) int {
	return dirtyBacking(arena.AllocIntBacking(n), 3) +
		dirtyBacking(arena.AllocFloatBacking(n), 3) +
		dirtyBacking(arena.AllocStringBacking(n), "earlier") +
		dirtyBacking(arena.AllocBoolBacking(n), true) +
		dirtyBacking(arena.AllocUintBacking(n), 3)
}

func dirtyBacking[T comparable](backing []T, filler T) int {
	var zero T
	stale := 0
	for index := range backing {
		if backing[index] != zero {
			stale++
		}
		backing[index] = filler
	}
	return stale
}

func TestEveryTypedBackingAllocatorHandsOutZeroedMemory(t *testing.T) {
	t.Parallel()

	sizes := []int{1, 7, InitialIntBackingSize / 3, InitialIntBackingSize * maxArenaMultiplier * 3}
	for _, licensed := range []bool{false, true} {
		arena := NewRegisterArena()
		arena.licensed = licensed
		for run := range 12 {
			arena.setRunBudget(uint64(run%3) * defaultMaxArenaBytes)
			for _, size := range sizes[:1+run%len(sizes)] {
				require.Zero(t, dirtyEveryTypedBacking(arena, size), "licensed=%v, run %d, %d elements", licensed, run, size)
			}
			arena.Reset()
		}
	}
}

func fillTypedBackings(arena *RegisterArena, strings, ints int) (staleStrings, staleInts int) {
	stringBacking := arena.AllocStringBacking(strings)
	for index := range stringBacking {
		if stringBacking[index] != "" {
			staleStrings++
		}
		stringBacking[index] = "left by an earlier run"
	}
	intBacking := arena.AllocIntBacking(ints)
	for index := range intBacking {
		if intBacking[index] != 0 {
			staleInts++
		}
		intBacking[index] = 7
	}
	return staleStrings, staleInts
}

func TestResetHandsOutZeroedBackingMemoryAgain(t *testing.T) {
	t.Parallel()

	sizes := []struct {
		name    string
		strings int
		ints    int
	}{
		{name: "within the initial slabs", strings: initialStringBackingSize / 2, ints: InitialIntBackingSize / 2},
		{name: "past the shrink threshold", strings: initialStringBackingSize * maxArenaMultiplier * 4, ints: InitialIntBackingSize * maxArenaMultiplier * 4},
	}
	for _, size := range sizes {
		for _, licensed := range []bool{false, true} {
			arena := NewRegisterArena()
			arena.licensed = licensed
			for run := range 4 {
				staleStrings, staleInts := fillTypedBackings(arena, size.strings, size.ints)
				require.Zero(t, staleStrings, "%s, licensed=%v, run %d: the string backing is zeroed", size.name, licensed, run)
				require.Zero(t, staleInts, "%s, licensed=%v, run %d: the int backing is zeroed", size.name, licensed, run)
				arena.Reset()
			}
		}
	}
}

func TestResetKeepsABackingSlabTheRunFilled(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	grown := initialStringBackingSize * maxArenaMultiplier * 4
	for range 3 {
		fillTypedBackings(arena, grown, 1)
		arena.Reset()
		require.GreaterOrEqual(t, len(arena.stringBackingSlab), grown, "a slab the run filled is kept for the next run")
	}

	arena.Reset()
	require.Len(t, arena.stringBackingSlab, initialStringBackingSize, "a slab the run left idle is replaced by one of the initial size")
}

func TestResetReplacesABackingSlabGrownButUnused(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	for range 12 {
		arena.GrowIntBackingSlab(1)
	}
	require.Greater(t, len(arena.intBackingSlab), InitialIntBackingSize*maxArenaMultiplier)
	arena.Reset()
	require.Len(t, arena.intBackingSlab, InitialIntBackingSize, "growth with nothing handed out does not survive a Reset")
}

func TestSetRunBudgetDropsAKeptBackingSlabTheBudgetIsTooSmallFor(t *testing.T) {
	t.Parallel()

	arena := NewRegisterArena()
	grown := initialStringBackingSize * maxArenaMultiplier * 4
	fillTypedBackings(arena, grown, 1)
	arena.Reset()
	kept := len(arena.stringBackingSlab)
	require.GreaterOrEqual(t, kept, grown)

	keptBytes := uint64(kept) * arenaSizeString
	arena.setRunBudget(keptBytes * keptBackingSlabBudgetShare)
	require.Len(t, arena.stringBackingSlab, kept, "a kept slab within its share of the budget is carried into the run")

	arena.setRunBudget(keptBytes*keptBackingSlabBudgetShare - 1)
	require.Len(t, arena.stringBackingSlab, initialStringBackingSize, "a kept slab over its share of the budget is replaced")
	require.Len(t, arena.intBackingSlab, InitialIntBackingSize, "a slab of the initial size is left alone")
}
