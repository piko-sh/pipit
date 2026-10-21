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
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestHardLimitRefusesAGrowBeforeItRetiresTheSlab(t *testing.T) {
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, arenaTotalAllocatedBytes(arena)+1024)
	slab := unsafe.SliceData(arena.intBackingSlab)
	retired, total := len(arena.oldIntBackings), arenaTotalAllocatedBytes(arena)
	require.ErrorIs(t, recoveredError(func() { arena.AllocIntBacking(1 << 24) }), fault.ErrArenaBudgetExceeded)
	require.Equal(t, slab, unsafe.SliceData(arena.intBackingSlab), "the current slab must not be replaced")
	require.Len(t, arena.oldIntBackings, retired, "nothing may be retired")
	require.Equal(t, total, arenaTotalAllocatedBytes(arena), "nothing may be charged")
	require.True(t, arena.aborted)
}

func TestArenaWithoutAnOwnerDefersToItsLicenceVM(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	arena.setLicenceVM(vm)
	setArenaBudget(t, arena, arenaTotalAllocatedBytes(arena)+1)
	require.NoError(t, recoveredError(func() { arena.GrowIntBackingSlab(1024) }))
	require.NotZero(t, vm.checkpointFlags&checkpointFlagGCPending, "the VM running the arena services its budget at a safepoint")
	require.ErrorIs(t, recoveredError(arena.enforceBudgetAtSafepoint), fault.ErrArenaBudgetExceeded)
	require.True(t, arena.aborted)
}

func TestAbortedArenaIsReleasedButNeverPooled(t *testing.T) {
	aborted := NewRegisterArena()
	aborted.attachLicencePolicy(&VMLimits{})
	kept := arenaString(aborted, "published by an interrupted handler")
	aborted.markAborted()
	require.True(t, aborted.licenceState.epochRevoked)
	require.Equal(t, epochAborted, aborted.licenceState.epochReason)
	PutRegisterArena(aborted)
	pooled := registerArenaPool.Drain()
	for _, arena := range pooled {
		registerArenaPool.Put(arena)
	}
	require.NotContains(t, pooled, aborted, "an aborted arena is never pooled")
	require.Equal(t, "published by an interrupted handler", kept, "the revoked epoch detached the slab before Reset cleared the arena")
}

func TestInterruptedMinorCollectionIsNotResumable(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	interrupt := func() { panic("interrupted part-way through the mark") }
	restore := SetMinorGCSiteHook(interrupt)
	defer restore()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		arena.MinorGC(vm)
	}()
	_, invariant := recovered.(*invariantError)
	require.True(t, invariant, "an interrupted collection must end the run, not reach a guest recover(): %v", recovered)
	require.True(t, arena.aborted)
}

func TestAbortOfAGoroutineArenaStaysLocal(t *testing.T) {
	tree := &ResourceTracker{}
	sibling := newArenaInTree(t, tree)
	goroutine := newGoroutineArenaInTree(t, tree)
	goroutine.markAborted()
	require.True(t, goroutine.licenceState.epochRevoked && goroutine.licenceState.banksRevoked)
	require.False(t, sibling.readBlindHandOffs().all)
	goroutine.aborted = false
	sibling.Reset()
	goroutine.Reset()

	caller := newArenaInTree(t, &ResourceTracker{})
	callback := newCallbackArenaInTree(t, &ResourceTracker{})
	callback.markAborted()
	require.True(t, caller.readBlindHandOffs().all, "a callback arena's abort reaches the caller in another tree")
	callback.aborted = false
	caller.Reset()
	callback.Reset()
}

func TestGrowPastTheOvershootIsRefusedBeforeItCharges(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	setArenaBudget(t, arena, arenaTotalAllocatedBytes(arena)+1)
	before := arenaTotalAllocatedBytes(arena)
	require.ErrorIs(t, recoveredError(func() { arena.admitGrow(2+budgetOvershootBytes, 0) }), fault.ErrArenaBudgetExceeded)
	require.Equal(t, before, arenaTotalAllocatedBytes(arena), "the hard limit fires before the grow charges anything")
	require.True(t, arena.aborted, "an arena an abort interrupted is never pooled again")
}

func TestAGrowPastTheOvershootIsLoweredToFitWhenASmallerSlabServes(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	const budget = 3 << 20
	setArenaBudget(t, arena, budget)
	arena.intBackingSlab = make([]int64, (2<<20)/arenaSizeInt64)
	arena.intBackingIndex = len(arena.intBackingSlab)
	kept := uint64(6144)
	arena.totalAllocatedBytes = 2<<20 + kept
	retired := len(arena.intBackingSlab)

	require.NoError(t, recoveredError(func() { arena.AllocIntBacking(4) }), "a smaller grow lets the run reach its safepoint")
	require.False(t, arena.aborted)
	require.Greater(t, len(arena.intBackingSlab), retired, "the grow still makes room")
	require.LessOrEqual(t, arenaTotalAllocatedBytes(arena), budget+budgetOvershootBytes, "the grow stays within the hard limit")
	require.NotZero(t, vm.checkpointFlags&checkpointFlagGCPending, "the arena is over budget, so the next safepoint collects")
}

func TestAGrowAfterACollectionIsLoweredToFitAroundKeptMemory(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	const budget = 3 << 20
	setArenaBudget(t, arena, budget)
	arena.intBackingSlab = make([]int64, (4<<20)/arenaSizeInt64)
	arena.intBackingIndex = len(arena.intBackingSlab)
	arena.totalAllocatedBytes = 6144

	require.NoError(t, recoveredError(func() { arena.AllocIntBacking(4) }), "kept memory must not turn a grow the budget would admit into a stop")
	require.LessOrEqual(t, arenaTotalAllocatedBytes(arena), budget+budgetOvershootBytes)
}

func TestAGrowIsNotLoweredBelowWhatTheRequestNeeds(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	setArenaBudget(t, arena, 3<<20)
	arena.totalAllocatedBytes = 3 << 20
	before := arenaTotalAllocatedBytes(arena)

	require.ErrorIs(t, recoveredError(func() { arena.AllocIntBacking((2 << 20) / int(arenaSizeInt64)) }), fault.ErrArenaBudgetExceeded)
	require.Equal(t, before, arenaTotalAllocatedBytes(arena), "a request the limit cannot hold is still refused before it charges")
}

func TestAByteSlabGrowIsLoweredToFit(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	const budget = 3 << 20
	setArenaBudget(t, arena, budget)
	arena.replaceByteSlab(make([]byte, 2<<20), true)
	arena.byteIndex = len(arena.byteSlab)
	arena.totalAllocatedBytes = 2<<20 + 6144

	require.NoError(t, recoveredError(func() { arena.GrowByteSlab(16) }))
	require.Greater(t, len(arena.byteSlab), 16)
	require.LessOrEqual(t, arenaTotalAllocatedBytes(arena), budget+budgetOvershootBytes)
}
