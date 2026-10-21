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

	"pipit.sh/pipit/internal/engine/program"
)

func TestKRefMembersAreOwnStorageUntilCovered(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	pointer := slot.Addr().UnsafePointer()
	text := arenaString(arena, "member")
	require.True(t, krefMemberStringStore(arena, pointer, text), "a store into an uncovered member is plain")
	require.True(t, krefMemberSlotStore(arena, pointer, reflect.ValueOf(&krefTestNode{})))

	arena.licenceAlwaysShared = true
	require.False(t, krefMemberStore(arena, pointer), "member stores take the barrier in a shared VM")
	arena.licenceAlwaysShared = false

	chunk, _ := arena.krefFind(uintptr(pointer))
	revokeForeignSlab(licenceForeignKey{start: chunk.base, owner: arena.slabOwner}, handOffNativeArgRetains)
	require.False(t, krefMemberStore(arena, pointer), "a pending foreign revocation voids membership")
	require.True(t, arena.licenceRevokedBase(chunk.base))
	arena.Reset()

	arena.attachLicencePolicy(&VMLimits{})
	slot = krefTestSlot(t, arena, krefTestNodeType)
	pointer = slot.Addr().UnsafePointer()
	arena.revokeEpoch(epochOpaqueFunction, handOffNativeArgRetains)
	require.False(t, krefMemberStore(arena, pointer), "an epoch revocation (which every shared stop makes) voids membership")
	arena.Reset()

	heap := new(krefTestNode)
	require.False(t, krefMemberStore(arena, unsafe.Pointer(heap)), "a Go heap object is never a member")
}

func TestKRefMemberStoresVetoDataIntoEscapingTypes(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	arena.revokeHandOff(handOffNativeArgRetains, slot.Addr(), false, false)
	arena.Reset()
	arena.attachLicencePolicy(&VMLimits{})
	slot = krefTestSlot(t, arena, krefTestNodeType)
	pointer := slot.Addr().UnsafePointer()
	require.False(t, krefMemberStringStore(arena, pointer, arenaString(arena, "escapes")), "a type whose members escaped keeps the barrier for strings")
	require.True(t, krefMemberSlotStore(arena, pointer, reflect.ValueOf(&krefTestNode{})), "but not for pointers, which cost nothing an escape would not revoke")
	arena.Reset()
}

func TestKRefPointeesAreOnlyNonCompositePointerKinds(t *testing.T) {
	require.True(t, krefPointeeKind(reflect.TypeFor[string]()))
	require.True(t, krefPointeeKind(reflect.TypeFor[*krefTestNode]()))
	require.True(t, krefPointeeKind(reflect.TypeFor[any]()))
	require.True(t, krefPointeeKind(reflect.TypeFor[map[string]int]()))
	require.False(t, krefPointeeKind(reflect.TypeFor[int]()), "pointer-free pointees go to the byte slab")
	require.False(t, krefPointeeKind(krefTestNodeType), "struct pointees take the boundary-snapshot path")
	require.False(t, krefPointeeKind(reflect.TypeFor[[]string]()), "slice pointees take the slice-header slab")
	require.False(t, krefPointeeKind(reflect.TypeFor[[2]*int]()))
}

func TestKRefCoveredMemberStoreTakesTheBarrier(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	arena.revokeHandOff(handOffNativeArgRetains, slot.Addr(), false, false)
	require.False(t, krefMemberStringStore(arena, slot.Addr().UnsafePointer(), arenaString(arena, "covered")),
		"a store into a covered member takes the barrier")
	arena.Reset()
}

func TestKRefCopyNeedsBarrierForKRefSourcesAndReferences(t *testing.T) {
	require.False(t, krefCopyNeedsBarrier(nil, nil, nil, reflect.TypeFor[int]()), "no arena")
	plain := newArena(t)
	heapNode := new(krefTestNode)
	require.False(t, krefCopyNeedsBarrier(plain, unsafe.Pointer(heapNode), unsafe.Pointer(&heapNode.Next), reflect.TypeFor[*krefTestNode]()),
		"an arena with no KRef chunks needs no barrier")

	arena := newArena(t)
	defer arena.Reset()
	slot := krefTestSlot(t, arena, krefTestNodeType)
	member := slot.Addr().UnsafePointer()
	require.True(t, copySourceIsKRefMember(arena, member))
	require.True(t, krefCopyNeedsBarrier(arena, member, member, reflect.TypeFor[int]()), "a KRef source")

	holder := &struct {
		pointer   *krefTestNode
		boxed     any
		count     int
		reference struct{ next *int }
	}{pointer: (*krefTestNode)(member), boxed: (*krefTestNode)(member)}
	source := unsafe.Pointer(holder)
	require.True(t, krefCopyNeedsBarrier(arena, source, unsafe.Pointer(&holder.pointer), reflect.TypeFor[*krefTestNode]()), "a pointer into a KRef chunk")
	require.True(t, krefCopyNeedsBarrier(arena, source, unsafe.Pointer(&holder.boxed), reflect.TypeFor[any]()), "an interface holding one")
	require.False(t, krefCopyNeedsBarrier(arena, source, unsafe.Pointer(&holder.count), reflect.TypeFor[int]()), "a scalar")
	require.True(t, krefCopyNeedsBarrier(arena, source, unsafe.Pointer(&holder.reference), reflect.TypeFor[struct{ next *int }]()),
		"a composite field that holds pointers is not read")
	holder.pointer = heapNode
	require.False(t, krefCopyNeedsBarrier(arena, source, unsafe.Pointer(&holder.pointer), reflect.TypeFor[*krefTestNode]()), "a heap pointer")
}

func TestKRefCompositeOwnedAndCopied(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	require.False(t, krefCompositeOwned(arena, reflect.ValueOf(new(krefTestNode)).Elem()), "an arena with no KRef chunks")
	slot := krefTestSlot(t, arena, krefTestNodeType)
	slot.Set(reflect.ValueOf(krefTestNode{Name: "kept"}))
	require.True(t, krefCompositeOwned(arena, slot))
	require.False(t, krefCompositeOwned(arena, reflect.ValueOf(new(krefTestNode)).Elem()))

	copied := krefCopy(slot)
	require.Equal(t, "kept", requireAs[krefTestNode](t, copied.Interface()).Name)
	require.False(t, krefCompositeOwned(arena, copied), "the copy lives on the Go heap")
}

func TestVetoPointeeSiteVetoesOnce(t *testing.T) {
	vetoPointeeSite(pointeeSite{}, false)
	function := &program.CompiledFunction{}
	vetoes, epochVetoes := krefPlacementCounters.pointeeVetoes.Load(), krefPlacementCounters.pointeeEpochVetoes.Load()
	vetoPointeeSite(pointeeSite{function: function, programCounter: 3}, true)
	require.True(t, function.ArenaPlacementVetoed(3))
	vetoPointeeSite(pointeeSite{function: function, programCounter: 3}, true)
	require.Equal(t, vetoes+1, krefPlacementCounters.pointeeVetoes.Load(), "an already vetoed site is not counted again")
	require.Equal(t, epochVetoes+1, krefPlacementCounters.pointeeEpochVetoes.Load())
}

func TestSnapshotPlacementIsVetoedOnlyAfterAWalkBudgetEpoch(t *testing.T) {
	snapshotType := reflect.TypeFor[struct{ p *int }]()
	cases := []struct {
		name   string
		revoke func(a *RegisterArena)
		vetoed bool
	}{
		{"walk budget", func(a *RegisterArena) { a.revokeEpoch(epochWalkBudget, handOffUntracedContainer) }, true},
		{"probe exhausted", func(a *RegisterArena) { a.revokeEpoch(epochProbeExhausted, handOffUntracedContainer) }, false},
		{"shared container", func(a *RegisterArena) { a.revokeEpoch(epochSharedContainer, handOffUntracedContainer) }, false},
		{"opaque function", func(a *RegisterArena) { a.revokeEpoch(epochOpaqueFunction, handOffUntracedContainer) }, false},
		{"an epoch not revoked", func(*RegisterArena) {}, false},
	}
	for _, c := range cases {
		arena := newGoroutineArenaInTree(t, &ResourceTracker{})
		krefTestSlot(t, arena, snapshotType)
		class := arena.kref.classes[reflectValueABIType(snapshotType)]
		require.NotNil(t, class, "%s: test harness", c.name)
		class.snapshotPlaced = true
		c.revoke(arena)
		arena.settleSnapshotPlacements()
		require.Equal(t, c.vetoed, class.snapshotVetoed, c.name)
		require.False(t, class.snapshotPlaced, "%s: the epoch's placements are settled", c.name)
		arena.Reset()
	}
}

func TestGlobalStoreKeepsAKRefSlotOnlyInARevokedEpoch(t *testing.T) {
	arena := newArena(t)
	defer arena.Reset()
	slot := krefTestSlot(t, arena, krefTestNodeType)
	slot.Set(reflect.ValueOf(krefTestNode{Name: "kept"}))

	require.True(t, krefHolderMustCopy(arena), "a clean epoch reuses the chunk, so the holder copies")
	copied := escapeArenaValueForGlobal(arena, slot)
	require.NotEqual(t, slotAddress(slot), uintptr(copied.Addr().UnsafePointer()), "the global gets a heap copy")
	require.False(t, krefCompositeOwned(arena, copied))

	arena.revokeEpoch(epochWalkBudget, handOffUntracedContainer)
	require.False(t, krefHolderMustCopy(arena), "a revoked epoch leaves every KRef chunk to the collector")
	kept := escapeArenaValueForGlobal(arena, slot)
	require.Equal(t, slotAddress(slot), uintptr(kept.Addr().UnsafePointer()), "the global keeps the slot itself")
	require.Equal(t, "kept", requireAs[krefTestNode](t, kept.Interface()).Name)
}

func TestReuseHazardKRefSnapshotKeptByAGlobalInARevokedEpochSurvivesReuse(t *testing.T) {
	arena := newArena(t)
	slot := krefTestSlot(t, arena, krefTestNodeType)
	slot.Set(reflect.ValueOf(krefTestNode{Name: arenaString(arena, "kept by the global")}))
	arena.revokeEpoch(epochWalkBudget, handOffUntracedContainer)
	globals := NewGlobalStore()
	index := globals.AllocGeneral(reflect.Value{})
	escaped := escapeArenaValueForGlobal(arena, slot)
	noteBoxedHandOff(arena, handOffUntracedContainer, escaped, true)
	globals.SetGeneral(index, escaped)
	require.Equal(t, slotAddress(slot), uintptr(escaped.Addr().UnsafePointer()), "test harness: the global holds the KRef slot")

	for range 3 {
		arena.Reset()
		arena.attachLicencePolicy(&VMLimits{})
		for range 4096 {
			reused := krefTestSlot(t, arena, krefTestNodeType)
			reused.Set(reflect.ValueOf(krefTestNode{Name: arenaString(arena, "a later epoch's node")}))
		}
		require.False(t, arena.krefContains(escaped.Addr().UnsafePointer()), "the released chunk never returns to the arena")
	}
	arena.Reset()
	require.Equal(t, "kept by the global", requireAs[krefTestNode](t, globals.GetGeneral(index).Interface()).Name)
}
