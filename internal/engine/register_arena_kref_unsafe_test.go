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

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/fault"
)

func TestKRefChunksAreRegisteredFoundAndOnRegisteredPages(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	address := slotAddress(slot)
	require.True(t, arena.krefContains(slot.Addr().UnsafePointer()))
	require.True(t, arena.holdsEscapableData())
	chunk, found := arena.krefFind(address)
	require.True(t, found)
	base, end, ok := licenceDataSlabSpan(arena, address)
	require.True(t, ok, "licenceDataSlabSpan finds a KRef chunk")
	require.Equal(t, chunk.base, base)
	require.Equal(t, chunk.end, end)
	_, class := arena.licenceSlabOf(address)
	require.Equal(t, licenceSlabData, class)
	require.True(t, licenceRegisteredPage(arena, address), "the chunk's page is in the slab page table")
	entry, registered := licenceSlabContaining(address)
	require.True(t, registered, "the chunk is published to the licence registry")
	require.Equal(t, arena.slabOwner, entry.owner)
	require.Equal(t, licenceSlabData, entry.class)
	require.Equal(t, chunk.base, entry.start)
	require.Equal(t, 1+arena.countDataSlabs()-1, arena.countDataSlabs())
	arena.Reset()
}

func TestKRefChunksNeedALicenceAndASizedType(t *testing.T) {
	plain := NewRegisterArena()
	_, ok := plain.krefSlot(krefTestNodeType)
	require.False(t, ok, "an arena without a licence has no KRef class")

	other := newArena(t)
	_, ok = other.krefSlot(reflect.TypeFor[struct{}]())
	require.False(t, ok, "a zero-size type never takes a chunk (they would share the zero base)")
	other.Reset()
}

func TestKRefWalkRevokesTheChunksASlotReachesPerChunk(t *testing.T) {
	arena := newArena(t)
	first := krefTestSlot(t, arena, krefTestNodeType)
	other := krefTestSlot(t, arena, reflect.TypeFor[*krefTestNode]())
	second := krefTestSlot(t, arena, krefTestNodeType)
	node := requireAs[*krefTestNode](t, first.Addr().Interface())
	next := requireAs[*krefTestNode](t, second.Addr().Interface())
	node.Next = next
	firstChunk, _ := arena.krefFind(slotAddress(first))
	otherChunk, _ := arena.krefFind(slotAddress(other))
	arena.revokeHandOff(handOffNativeArgRetains, reflect.ValueOf(node), false, false)
	require.True(t, arena.licenceRevokedBase(firstChunk.base), "the hand-off revokes the slot's chunk")
	require.False(t, arena.licenceRevokedBase(otherChunk.base), "and no chunk of another type")
	require.False(t, arena.licenceState.epochRevoked, "per chunk, not the epoch")
	arena.Reset()
}

func TestKRefReleaseDetachesRevokedChunksAndResetClearsAndReusesTheRest(t *testing.T) {
	arena := newArena(t)
	kept := krefTestSlot(t, arena, krefTestNodeType)
	kept.Addr().Interface().(*krefTestNode).Name = "kept"
	escaped := krefTestSlot(t, arena, reflect.TypeFor[krefTestStrings]())
	escapedValue := requireAs[*krefTestStrings](t, escaped.Addr().Interface())
	escapedValue.A = "escaped"
	keptChunk, _ := arena.krefFind(slotAddress(kept))
	escapedChunk, _ := arena.krefFind(slotAddress(escaped))
	arena.revokeHandOff(handOffNativeArgRetains, reflect.ValueOf(escapedValue), false, false)
	before := krefChunkStatistics()
	arena.Reset()
	require.Equal(t, "escaped", escapedValue.A, "a detached chunk is left uncleared")
	_, found := arena.krefFind(escapedChunk.base)
	require.False(t, found, "the revoked chunk is detached")
	_, found = arena.krefFind(keptChunk.base)
	require.True(t, found, "the clean chunk is kept")
	require.Zero(t, keptChunk.used)
	require.Equal(t, "", kept.Addr().Interface().(*krefTestNode).Name, "Reset typed-clears the kept chunk")
	require.False(t, arena.holdsEscapableData())
	after := krefChunkStatistics()
	require.Greater(t, after.Detached, before.Detached)
	require.Greater(t, after.Reused, before.Reused)

	arena.attachLicencePolicy(&VMLimits{})
	again := krefTestSlot(t, arena, krefTestNodeType)
	require.Equal(t, slotAddress(kept), slotAddress(again), "the next epoch reuses the kept chunk's first slot")
	arena.Reset()
}

func TestKRefClassCapsAndEviction(t *testing.T) {
	arena := newArena(t)
	types := make([]reflect.Type, 0, krefMaxClasses+1)
	for index := range krefMaxClasses + 1 {
		types = append(types, reflect.ArrayOf(index+1, reflect.TypeFor[*int]()))
	}
	for _, typ := range types[:krefMaxClasses] {
		krefTestSlot(t, arena, typ)
	}
	_, ok := arena.krefSlot(types[krefMaxClasses])
	require.False(t, ok, "a type beyond krefMaxClasses takes the heap path")
	arena.Reset()
	require.Len(t, arena.kref.classes, krefMaxClasses, "every class used in the epoch is kept")
	for range krefIdleResets + 1 {
		arena.attachLicencePolicy(&VMLimits{})
		krefTestSlot(t, arena, types[0])
		arena.Reset()
	}
	require.Len(t, arena.kref.classes, 1, "idle classes are evicted")
	require.Equal(t, 1, arena.krefChunkCount())

	arena.attachLicencePolicy(&VMLimits{})
	big := reflect.ArrayOf(1024, reflect.TypeFor[*int]())
	for range 2 * krefRetainClassBytes / int(big.Size()) {
		krefTestSlot(t, arena, big)
	}
	arena.Reset()
	class := arena.kref.classes[reflectValueABIType(big)]
	total := 0
	for _, chunk := range class.chunks {
		total += chunk.bytes()
	}
	require.LessOrEqual(t, total, krefRetainClassBytes, "Reset keeps at most krefRetainClassBytes of a type")
}

func TestKRefForeignWalkNamesAChunk(t *testing.T) {
	owner := newArena(t)
	walker := newArena(t)
	slot := krefTestSlot(t, owner, krefTestNodeType)
	chunk, _ := owner.krefFind(slotAddress(slot))
	walker.revokeHandOff(handOffNativeArgRetains, slot.Addr(), false, false)
	require.False(t, owner.licenceRevokedBase(chunk.base), "the revocation is pending until the owner adopts it")
	require.True(t, owner.licenceCoveredAt(slot.Addr().UnsafePointer()), "a pending foreign revocation covers the chunk, which member stores rely on")
	require.True(t, owner.licenceRevokedBase(chunk.base), "and the cover adopts it")
	walker.Reset()
	owner.Reset()
}

func TestKRefSharedWalkStopsAtARevokedChunkPerType(t *testing.T) {
	arena := newArena(t)
	nodeSlot := krefTestSlot(t, arena, krefTestNodeType)
	stringsSlot := krefTestSlot(t, arena, reflect.TypeFor[krefTestStrings]())
	nodeChunk, _ := arena.krefFind(slotAddress(nodeSlot))
	stringsChunk, _ := arena.krefFind(slotAddress(stringsSlot))
	arena.revokeSlab(nodeChunk.base, handOffNativeArgRetains)
	arena.revokeSlab(stringsChunk.base, handOffNativeArgRetains)

	before := licenceStatistics().BlindByteHandOffs
	arena.revokeHandOff(handOffSharedStore, stringsSlot, false, true)
	require.False(t, arena.licenceState.epochRevoked, "a revoked chunk of a strings-only type is skipped byte-blind, not a full stop")
	require.Greater(t, licenceStatistics().BlindByteHandOffs, before)

	arena.revokeHandOff(handOffSharedStore, nodeSlot, false, true)
	require.True(t, arena.licenceState.epochRevoked, "a revoked chunk of a pointer-bearing type is a full stop for a shared walk")
	arena.Reset()
}

func TestKRefMarkTracesThroughASlotAndNeverCompactsChunks(t *testing.T) {
	vm := newGCTestVM()
	vm.Arena.attachLicencePolicy(&VMLimits{})
	arena := vm.Arena
	type holder struct {
		Numbers []int64
		Self    *holder
	}
	slot := krefTestSlot(t, arena, reflect.TypeFor[holder]())
	value := requireAs[*holder](t, slot.Addr().Interface())
	value.Numbers = arena.AllocIntBacking(16)
	value.Self = value
	holdInFrame(vm, reflect.ValueOf(value))
	arena.GrowIntBackingSlab(InitialIntBackingSize)
	require.Len(t, arena.oldIntBackings, 1)
	chunks := arena.krefChunkCount()
	arena.MinorGC(vm)
	require.Len(t, arena.oldIntBackings, 1, "the mark traces through the KRef slot to the backing it references")
	require.Equal(t, chunks, arena.krefChunkCount(), "the minor collection never drops a KRef chunk")
	vm.CallStack = nil
	arena.Reset()
}

func TestKRefHandOffRevokesTheSlotsChunk(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	chunk, _ := arena.krefFind(slotAddress(slot))
	arena.revokeHandOff(handOffNativeArgRetains, slot.Addr(), false, false)
	require.True(t, arena.licenceRevokedBase(chunk.base), "the hand-off of a pointer into a KRef chunk revokes the chunk")
	arena.Reset()
}

func TestKRefChunksAreChargedToTheArenaBudget(t *testing.T) {
	arena := newArena(t)
	before := arena.totalAllocatedBytes
	slot := krefTestSlot(t, arena, krefTestNodeType)
	chunk, found := arena.krefFind(slotAddress(slot))
	require.True(t, found)
	require.Equal(t, before+uint64(chunk.bytes()), arena.totalAllocatedBytes, "a new chunk is charged to the run")
	krefTestSlot(t, arena, krefTestNodeType)
	require.Equal(t, before+uint64(chunk.bytes()), arena.totalAllocatedBytes, "a slot from an existing chunk charges nothing more")
	arena.Reset()

	require.Equal(t, 1, arena.krefChunkCount(), "test harness: Reset keeps the chunk")
	arena.attachLicencePolicy(&VMLimits{})
	arena.setRunBudget(0)
	require.Equal(t, uint64(chunk.bytes()), arena.totalAllocatedBytes, "a chunk kept across Reset is charged to the next run")
	arena.Reset()
}

func TestKRefRunsRespectTheArenaLimits(t *testing.T) {
	elementType := reflect.TypeFor[*int]()

	overBudget := newArena(t)
	overBudget.MaxArenaBytes = 1 << 16
	err := arenaAbortError(func() { overBudget.krefRun(elementType, (1<<16)/int(elementType.Size())+1) })
	require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded, "a KRef run larger than the budget is refused")
	overBudget.Reset()

	overSize := newArena(t)
	overSize.MaxAllocSize = 8
	err = arenaAbortError(func() { overSize.krefRun(elementType, 9) })
	require.ErrorIs(t, err, fault.ErrAllocationLimit, "a KRef run longer than MaxAllocSize is refused")
	overSize.Reset()
}
