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
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestReuseHazardTrimmedSlabLosesOwnership(t *testing.T) {
	observations := &hazardObservations{}
	limits := VMLimits{}
	abandon := limits.abandonsArenas()
	arena := acquireRegisterArena(abandon)
	header := arena.allocSliceHeader()
	backing := []int{7, 8, 9}
	*header = arenaSliceHeader{Data: unsafe.Pointer(&backing[0]), Len: 3, Cap: 3}
	cell := (*[2]int64)(arena.AllocBytes(16, 8))
	cell[0], cell[1] = 0x0C0FFEE, 0x0D15EA5E
	headerOwnedBefore := arena.OwnsSliceHeaderPointer(unsafe.Pointer(header))
	cellOwnedBefore := arena.ownsBytePointer(unsafe.Pointer(cell))

	for range maxRetainedOldSlabsPerType + 4 {
		arena.growSliceHeaderSlab()
		arena.growGenericBytesSlab(genericBytesSlabChunk)
	}
	headerOwnedAfter := arena.OwnsSliceHeaderPointer(unsafe.Pointer(header))
	cellOwnedAfter := arena.ownsBytePointer(unsafe.Pointer(cell))
	releaseRegisterArena(abandon, arena, false)

	next := acquireRegisterArena(abandon)
	for range 4 * sliceHeaderSlabChunk {
		slot := next.allocSliceHeader()
		*slot = arenaSliceHeader{Data: nil, Len: 99, Cap: 99}
	}
	for range genericBytesSlabChunk / 16 * 4 {
		filled := (*[2]int64)(next.AllocBytes(16, 8))
		filled[0], filled[1] = -1, -1
	}
	headerIntact := header.Len == 3 && header.Cap == 3 && header.Data == unsafe.Pointer(&backing[0])
	cellIntact := cell[0] == 0x0C0FFEE && cell[1] == 0x0D15EA5E
	releaseRegisterArena(abandon, next, false)

	observations.note("slice-header slot owned before/after trim=%t/%t", headerOwnedBefore, headerOwnedAfter)
	observations.note("generic-bytes cell owned before/after trim=%t/%t", cellOwnedBefore, cellOwnedAfter)
	reason := ""
	switch {
	case !headerIntact || !cellIntact:
		observations.fire("host-value-changed", "after release and a full refill: header intact=%t, cell intact=%t", headerIntact, cellIntact)
	case !headerOwnedBefore || !cellOwnedBefore:
		reason = "the slots were not arena-owned to begin with"
	case !headerOwnedAfter || !cellOwnedAfter:
		observations.note("ownership tracking is lost on trim, but the trimmed slab is never reused, so the loss does not corrupt the slot")
	}
	runtime.KeepAlive(backing)
	assertNoHazard(t, observations, reason)
}

func TestGrowPastBudgetDefersToTheSafepoint(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	setArenaBudget(t, arena, arenaTotalAllocatedBytes(arena)+1)
	arena.ChargeArenaAllocation(2, 0)
	require.NotZero(t, vm.checkpointFlags&checkpointFlagGCPending, "the grow must leave the collection to the safepoint")
	require.ErrorIs(t, recoveredError(func() { vm.runPendingCheckpoints() }), fault.ErrArenaBudgetExceeded,
		"the safepoint must enforce the budget once its collection has run")
}

func TestChargePastTheOvershootOnlyRaisesThePendingFlag(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaOwnerVM(t, arena, vm)
	setArenaBudget(t, arena, arenaTotalAllocatedBytes(arena)+1)
	require.NoError(t, recoveredError(func() { arena.ChargeArenaAllocation(2+budgetOvershootBytes, 0) }),
		"once a grow has changed the arena its charge never panics; the safepoint enforces the budget")
	require.NotZero(t, vm.checkpointFlags&checkpointFlagGCPending)
	require.False(t, arena.aborted)
}
