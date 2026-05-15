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

	"github.com/stretchr/testify/require"
)

func growSliceHeaderSlabToChunk(t *testing.T, a *RegisterArena) {
	t.Helper()
	for len(a.sliceHeaderSlab) < sliceHeaderSlabChunk {
		a.growSliceHeaderSlab()
	}
}

func deadSliceHeaderState(a *RegisterArena) *gcMarkState {
	return &gcMarkState{
		OldSliceHeaderLive:  make([]bool, len(a.oldSliceHeaderSlabs)),
		OldGenericBytesLive: make([]bool, len(a.oldGenericByteSlabs)),
	}
}

func TestSliceHeaderSlabGrowthCapsAtChunk(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)

	for range 24 {
		a.growSliceHeaderSlab()
		require.LessOrEqual(t, len(a.sliceHeaderSlab), sliceHeaderSlabChunk)
	}
	require.Equal(t, sliceHeaderSlabChunk, len(a.sliceHeaderSlab))
	for _, old := range a.oldSliceHeaderSlabs {
		require.LessOrEqual(t, cap(old), sliceHeaderSlabChunk)
	}

	for range 24 {
		a.growGenericBytesSlab(1)
		require.LessOrEqual(t, len(a.genericBytesSlab), genericBytesSlabChunk)
	}
	require.Equal(t, genericBytesSlabChunk, len(a.genericBytesSlab))
	a.growGenericBytesSlab(genericBytesSlabChunk * 2)
	require.Equal(t, genericBytesSlabChunk*2, len(a.genericBytesSlab), "an oversized request is honoured with an oversized slab")
}

func TestChunkedSlabGrowthChargesEachFullChunk(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)

	before := a.totalAllocatedBytes
	a.growSliceHeaderSlab()
	require.Equal(t, before+sliceHeaderSlabChunk*arenaSizeSliceHeader, a.totalAllocatedBytes,
		"replacing a full chunk with another must charge the whole chunk, not a zero delta")
}

func TestCompactRecyclesDeadHeaderChunks(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)
	a.growSliceHeaderSlab()
	retired := a.oldSliceHeaderSlabs[len(a.oldSliceHeaderSlabs)-1]
	require.Equal(t, sliceHeaderSlabChunk, cap(retired))
	budgetBefore := a.totalAllocatedBytes

	reclaimed, recycled := a.compactPhase(deadSliceHeaderState(a))
	require.Len(t, a.freeSliceHeaderChunks, 1)
	require.Empty(t, a.oldSliceHeaderSlabs)
	require.Equal(t, int64(sliceHeaderSlabChunk)*arenaSliceHeaderBytes, recycled)
	require.GreaterOrEqual(t, reclaimed, recycled)
	require.Same(t, &retired[0], &a.freeSliceHeaderChunks[0][0], "the dead chunk itself is parked on the free list")

	a.growSliceHeaderSlab()
	require.Same(t, &retired[0], &a.sliceHeaderSlab[0], "the next growth reinstalls the recycled chunk")
	require.Empty(t, a.freeSliceHeaderChunks)
	require.Equal(t, budgetBefore, a.totalAllocatedBytes, "a recycled chunk is neither released nor recharged")
	require.Zero(t, a.sliceHeaderIndex)
}

func TestCompactRecyclesDeadGenericByteChunks(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	for len(a.genericBytesSlab) < genericBytesSlabChunk {
		a.growGenericBytesSlab(1)
	}
	a.growGenericBytesSlab(1)
	retired := a.oldGenericByteSlabs[len(a.oldGenericByteSlabs)-1]
	require.Equal(t, genericBytesSlabChunk, cap(retired))

	_, recycled := a.compactPhase(deadSliceHeaderState(a))
	require.Len(t, a.freeGenericByteChunks, 1)
	require.Equal(t, int64(genericBytesSlabChunk), recycled)

	a.growGenericBytesSlab(64)
	require.Same(t, &retired[0], &a.genericBytesSlab[0])
	require.Empty(t, a.freeGenericByteChunks)
}

func TestRecycledChunkIsZeroed(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)
	for i := range a.sliceHeaderSlab {
		a.sliceHeaderSlab[i].Len = i + 1
		a.sliceHeaderSlab[i].Cap = i + 1
	}
	a.growSliceHeaderSlab()

	a.compactPhase(deadSliceHeaderState(a))
	require.Len(t, a.freeSliceHeaderChunks, 1)
	for _, header := range a.freeSliceHeaderChunks[0] {
		require.Equal(t, arenaSliceHeader{}, header)
	}
}

func TestCompactDropsDeadChunksBeyondFreeListCap(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)
	a.dropRetainedOldSlabs()
	a.growSliceHeaderSlab()
	require.Len(t, a.oldSliceHeaderSlabs, 1)
	for range maxFreeSlabChunks {
		a.freeSliceHeaderChunks = append(a.freeSliceHeaderChunks, make([]arenaSliceHeader, sliceHeaderSlabChunk))
	}

	reclaimed, recycled := a.compactPhase(deadSliceHeaderState(a))
	require.Len(t, a.freeSliceHeaderChunks, maxFreeSlabChunks)
	require.Zero(t, recycled)
	require.Equal(t, int64(sliceHeaderSlabChunk)*arenaSliceHeaderBytes, reclaimed)
	require.Empty(t, a.oldSliceHeaderSlabs)
}

func TestResetDropsFreeChunks(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)
	a.growSliceHeaderSlab()
	a.compactPhase(deadSliceHeaderState(a))
	require.Len(t, a.freeSliceHeaderChunks, 1)

	a.Reset()
	require.Empty(t, a.freeSliceHeaderChunks)
	require.Empty(t, a.freeGenericByteChunks)
}

func TestChunkedSlabCapacityPolicy(t *testing.T) {
	t.Parallel()
	require.Equal(t, 2048, chunkedSlabCapacity(0, 1, 2048, 32768))
	require.Equal(t, 4096, chunkedSlabCapacity(2048, 1, 2048, 32768))
	require.Equal(t, 32768, chunkedSlabCapacity(16384, 1, 2048, 32768))
	require.Equal(t, 32768, chunkedSlabCapacity(32768, 1, 2048, 32768))
	require.Equal(t, 32768, chunkedSlabCapacity(32768, 32768, 2048, 32768))
	require.Equal(t, 40000, chunkedSlabCapacity(32768, 40000, 2048, 32768))
}

func TestDeadChunksDroppedWhenRecyclingDisabled(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = false
	setArenaBudget(t, a, 1<<30)
	growSliceHeaderSlabToChunk(t, a)
	a.growSliceHeaderSlab()
	retired := a.oldSliceHeaderSlabs[len(a.oldSliceHeaderSlabs)-1]
	retired[0].Len = 7

	_, recycled := a.compactPhase(deadSliceHeaderState(a))
	require.Zero(t, recycled)
	require.Empty(t, a.freeSliceHeaderChunks)
	require.Empty(t, a.oldSliceHeaderSlabs)
	require.Equal(t, 7, retired[0].Len, "a dropped chunk is never zeroed")
}

func TestArenasSeedRecyclingFromBuildDefault(t *testing.T) {
	t.Parallel()
	require.Equal(t, ArenaChunkRecyclingEnabled, NewRegisterArena().recycleChunks)
	pooled := GetRegisterArena()
	defer PutRegisterArena(pooled)
	require.Equal(t, ArenaChunkRecyclingEnabled, pooled.recycleChunks, "pooled arenas follow the build default")
}
