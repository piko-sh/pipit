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
	"reflect"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/mem"
)

func newGCTestVM() *VM {
	return &VM{
		Globals:   &GlobalStore{},
		Arena:     NewRegisterArena(),
		CallStack: []CallFrame{},
	}
}

func TestMinorGC_DropsFullyDeadOldByteSlabs(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena

	for range 5 {
		arena.GrowByteSlab(InitialByteSlabSize)
	}
	require.NotEmpty(t, arena.oldByteSlabs, "expected oldByteSlabs to be populated after growth")
	beforeCount := len(arena.oldByteSlabs)

	arena.MinorGC(vm)

	assert.Emptyf(t, arena.oldByteSlabs, "expected oldByteSlabs to be drained (no live refs), started with %d", beforeCount)
}

func TestMinorGC_PreservesLiveOldByteSlabs(t *testing.T) {
	if !arenaUsesUnsafeSlabs {
		t.Skip("safe build: MinorGC slab-retention semantics require the unsafe arena")
	}
	vm := newGCTestVM()
	arena := vm.Arena

	buffer := arena.AllocStringBytes(64)
	copy(buffer, strings.Repeat("a", 64))
	liveStr := mem.String(buffer)

	arena.GrowByteSlab(InitialByteSlabSize)
	require.Lenf(t, arena.oldByteSlabs, 1, "expected 1 old byteSlab after growth, got %d", len(arena.oldByteSlabs))

	vm.Globals.Strings = []string{liveStr}

	arena.MinorGC(vm)

	assert.Lenf(t, arena.oldByteSlabs, 1, "expected live old byteSlab to survive GC, got %d remaining", len(arena.oldByteSlabs))
}

func TestMinorGC_DropsDeadOldIntBackings(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena

	for range 3 {
		arena.GrowIntBackingSlab(InitialIntBackingSize)
	}
	require.NotEmpty(t, arena.oldIntBackings, "expected oldIntBackings to be populated after growth")

	arena.MinorGC(vm)

	assert.Emptyf(t, arena.oldIntBackings, "expected oldIntBackings to be drained (no live refs), got %d", len(arena.oldIntBackings))
}

func TestMinorGC_PreservesLiveOldIntBackings(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena

	liveSlice := arena.AllocIntBacking(16)
	for i := range liveSlice {
		liveSlice[i] = int64(i)
	}

	arena.GrowIntBackingSlab(InitialIntBackingSize)
	require.Lenf(t, arena.oldIntBackings, 1, "expected 1 old int backing after growth, got %d", len(arena.oldIntBackings))

	vm.CallStack = []CallFrame{{
		Registers: Registers{
			SlicesInt: [][]int64{liveSlice},
		},
	}}
	vm.FramePointer = 0

	arena.MinorGC(vm)

	assert.Lenf(t, arena.oldIntBackings, 1, "expected live old int backing to survive GC, got %d remaining", len(arena.oldIntBackings))
}

func TestMinorGC_UpdatesGCCount(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	assert.Equalf(t, uint32(0), arena.gcCount, "fresh arena should have gcCount=0, got %d", arena.gcCount)
	arena.MinorGC(vm)
	arena.MinorGC(vm)
	assert.Equalf(t, uint32(2), arena.gcCount, "expected gcCount=2 after two MinorGCs, got %d", arena.gcCount)
}

func TestMinorGC_ResetsBytesAllocated(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena

	arena.AllocStringBytes(1024)
	arena.AllocIntBacking(128)
	require.NotZerof(t, arena.bytesAllocated, "expected bytesAllocated > 0 after allocations")

	arena.MinorGC(vm)

	assert.GreaterOrEqualf(t, arena.bytesAllocated, int64(0), "bytesAllocated should not go negative, got %d", arena.bytesAllocated)
}

func TestMinorGC_AdvancesThreshold(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena
	arena.bytesAllocated = gcInitialThreshold + 1
	arena.MinorGC(vm)
	assert.Positivef(t, arena.nextGCAt, "expected nextGCAt to be set after MinorGC, got %d", arena.nextGCAt)
	assert.GreaterOrEqualf(t, arena.nextGCAt, arena.bytesAllocated+gcMinThresholdDelta,
		"expected nextGCAt >= bytesAllocated+gcMinThresholdDelta, got nextGCAt=%d bytesAllocated=%d",
		arena.nextGCAt, arena.bytesAllocated)
}

func TestMinorGC_PacingUsesPreviousBytesAtLastGC(t *testing.T) {
	vm := newGCTestVM()
	arena := vm.Arena

	const preGCBytes = int64(4 << 20)
	const previousLastGC = preGCBytes - (1 << 19)
	arena.bytesAllocated = preGCBytes
	arena.bytesAtLastGC = previousLastGC

	arena.MinorGC(vm)

	growth := preGCBytes - previousLastGC
	wantDelta := min(max(growth*gcMostlyLiveGrowthMultiplier, gcMinThresholdDelta), gcMaxThresholdDelta)
	wantNextGCAt := preGCBytes + wantDelta
	assert.Equalf(t, wantNextGCAt, arena.nextGCAt,
		"MinorGC must pace on the allocation since the previous GC (preGC - bytesAtLastGC), not this cycle's reclaimed bytes")

	buggyDelta := min(max(gcInitialThreshold*gcMostlyLiveGrowthMultiplier, gcMinThresholdDelta), gcMaxThresholdDelta)
	assert.NotEqualf(t, preGCBytes+buggyDelta, arena.nextGCAt,
		"nextGCAt matches the pre-fix ordering that overwrote bytesAtLastGC before the pacing calculation read it")

	assert.Equalf(t, preGCBytes, arena.bytesAtLastGC,
		"bytesAtLastGC must be updated to the post-GC byte count for the next cycle")
}

func TestUpdateNextGCAt_MostlyLiveBacksOff(t *testing.T) {
	arena := NewRegisterArena()
	arena.bytesAtLastGC = 0
	preGC := int64(10 * 1024 * 1024)
	postGC := int64(9 * 1024 * 1024)
	arena.updateNextGCAt(preGC, postGC)
	delta := arena.nextGCAt - postGC
	assert.GreaterOrEqualf(t, delta, gcMinThresholdDelta, "mostly-live should produce large delta (>= min), got %d", delta)
}

func TestUpdateNextGCAt_MostlyGarbageKeepsTight(t *testing.T) {
	arena := NewRegisterArena()
	arena.bytesAtLastGC = 0
	preGC := int64(10 * 1024 * 1024)
	postGC := int64(1 * 1024 * 1024)
	arena.updateNextGCAt(preGC, postGC)
	delta := arena.nextGCAt - postGC
	assert.LessOrEqualf(t, delta, gcMaxThresholdDelta, "mostly-garbage should produce bounded delta (<= max), got %d", delta)
}

func TestUpdateNextGCAt_RespectsFloorAndCeiling(t *testing.T) {
	arena := NewRegisterArena()
	arena.bytesAtLastGC = 0
	preGC := int64(100)
	postGC := int64(50)
	arena.updateNextGCAt(preGC, postGC)
	assert.GreaterOrEqualf(t, arena.nextGCAt-postGC, gcMinThresholdDelta,
		"threshold delta below floor, got %d expected >= %d",
		arena.nextGCAt-postGC, gcMinThresholdDelta)
}

func TestGCShouldRun_HonoursThreshold(t *testing.T) {
	arena := NewRegisterArena()
	assert.False(t, arena.gcShouldRun(), "fresh arena should not trigger GC")
	arena.bytesAllocated = gcInitialThreshold - 1
	assert.False(t, arena.gcShouldRun(), "arena below threshold should not trigger GC")
	arena.bytesAllocated = gcInitialThreshold + 1
	assert.True(t, arena.gcShouldRun(), "arena above threshold should trigger GC")
}

type gcSharedHolder struct {
	pair *gcTracePair
}

func newGoroutineGCTestVM(t *testing.T) *VM {
	t.Helper()
	vm := newGCTestVM()
	vm.debugKind = debugThreadGoroutine
	vm.hasGoroutines = true
	vm.CallStack = []CallFrame{{}}
	arena := vm.Arena
	arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, arena, 1<<30)
	arena.licenceAlwaysShared = true
	arena.setLicenceVM(vm)
	require.True(t, arena.licenceShared(0), "test harness: the arena's memory must be shared")
	return vm
}

func retirePair(t *testing.T, arena *RegisterArena) unsafe.Pointer {
	t.Helper()
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, _ := retireGenericBytesAllocation(t, arena, pairType.Size(), uintptr(pairType.Align()))
	return pointer
}

func retainsGenericSlabHolding(arena *RegisterArena, pointer unsafe.Pointer) bool {
	for _, slab := range arena.oldGenericByteSlabs {
		if len(slab) > 0 && uintptr(pointer) >= uintptr(unsafe.Pointer(&slab[0])) && uintptr(pointer) < uintptr(unsafe.Pointer(&slab[0]))+uintptr(len(slab)) {
			return true
		}
	}
	return false
}

func TestMinorGCOfAGoroutineArenaReclaimsWhileItsMemoryIsShared(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	live := retirePair(t, arena)
	retirePair(t, arena)
	vm.CallStack[0].Registers.General = []reflect.Value{reflect.NewAt(reflect.TypeFor[gcTracePair](), live)}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 1, "the slab no root reaches is reclaimed although another goroutine shares the VM's memory")
	require.True(t, retainsGenericSlabHolding(arena, live), "the slab a register reaches is kept")
}

func TestMinorGCOfAGoroutineArenaFailsClosedWhileShared(t *testing.T) {
	pairType := reflect.TypeFor[gcTracePair]()
	for _, testCase := range []struct {
		name  string
		setUp func(vm *VM, live unsafe.Pointer)
	}{
		{"on a cell its VM made that a walk handed off", func(vm *VM, live unsafe.Pointer) {
			cell := newCellFor(vm)
			cell.Kind = isa.RegisterGeneral
			cell.GeneralValue = reflect.NewAt(pairType, live)
			cell.HandedOff.Store(1)
			vm.CallStack[0].upvalues = []upvalue{{Value: cell}}
		}},
		{"on a root that reaches memory outside its own storage", func(vm *VM, live unsafe.Pointer) {
			vm.CallStack[0].Registers.General = []reflect.Value{reflect.ValueOf(&gcSharedHolder{pair: (*gcTracePair)(live)})}
		}},
		{"for the VM that started the goroutines", func(vm *VM, live unsafe.Pointer) {
			vm.CallStack[0].Registers.General = []reflect.Value{reflect.NewAt(pairType, live)}
			vm.debugKind = 0
			vm.spawnedGoroutines = true
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			vm := newGoroutineGCTestVM(t)
			arena := vm.Arena
			live := retirePair(t, arena)
			retirePair(t, arena)
			testCase.setUp(vm, live)

			arena.MinorGC(vm)

			require.Len(t, arena.oldGenericByteSlabs, 2, "a mark that cannot vouch for every reference reclaims nothing")
		})
	}
}

func TestMinorGCOfAGoroutineArenaReadsNeitherCellsItDidNotMakeNorGlobals(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	cell := &program.UpvalueCell{Kind: isa.RegisterGeneral}
	vm.CallStack[0].upvalues = []upvalue{{Value: cell}}
	vm.Globals.general = make([]reflect.Value, 1)
	vm.Globals.Strings = make([]string, 1)
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Go(func() {
		for round := 0; ; round++ {
			select {
			case <-stop:
				return
			default:
			}
			cell.GeneralValue = reflect.ValueOf(&gcTracePair{A: int64(round)})
			vm.Globals.general[0] = reflect.ValueOf(&gcTracePair{B: int64(round)})
			vm.Globals.Strings[0] = strings.Repeat("g", round%8)
		}
	})
	defer writer.Wait()
	defer close(stop)

	for range 64 {
		retirePair(t, arena)
		arena.MinorGC(vm)
		require.Empty(t, arena.oldGenericByteSlabs, "every collection reclaims while another goroutine writes the cell and the globals")
	}
}

func TestSharedMinorGCReleasesOnlyTheChargeADroppedSlabCarries(t *testing.T) {
	t.Run("a slab whose charge went to its replacement", func(t *testing.T) {
		vm := newGoroutineGCTestVM(t)
		arena := vm.Arena
		for range 4 {
			arena.GrowIntBackingSlab(1)
		}
		require.NotEmpty(t, arena.oldIntBackings)
		held := arena.totalAllocatedBytes

		arena.MinorGC(vm)

		require.Empty(t, arena.oldIntBackings, "test harness: nothing reaches the retired backings")
		require.Equal(t, held, arena.totalAllocatedBytes,
			"the current backing holds the charge its predecessors handed on, so dropping them releases nothing")
	})
	t.Run("a full chunk", func(t *testing.T) {
		vm := newGoroutineGCTestVM(t)
		arena := vm.Arena
		arena.recycleChunks = false
		for len(arena.genericBytesSlab) < genericBytesSlabChunk {
			arena.growGenericBytesSlab(1)
		}
		arena.growGenericBytesSlab(1)
		held := arena.totalAllocatedBytes

		arena.MinorGC(vm)

		require.Empty(t, arena.oldGenericByteSlabs, "test harness: nothing reaches the retired slabs")
		require.Equal(t, held-genericBytesSlabChunk, arena.totalAllocatedBytes,
			"the dropped chunk was charged on its own and takes its charge with it; the smaller slabs before it took none")
	})
}

func TestMinorGCOfAnUnlicensedArenaMarksAfterAGoroutineExportedGuestCode(t *testing.T) {
	vm := newGCTestVM()
	vm.CallStack = []CallFrame{{}}
	vm.Limits.Tracker = &ResourceTracker{}
	arena := vm.Arena
	setArenaBudget(t, arena, 1<<30)
	arena.setOwnerVM(vm)
	require.False(t, arena.licensed, "test harness: the abandon fallback leaves the arena unlicensed")
	vm.gatesForChildGoroutines()
	require.NotNil(t, vm.exportGate, "test harness: starting a goroutine gives the VM a gate")
	vm.exportGate.exportedBySpawned.Store(true)
	vm.hasGoroutines, vm.spawnedGoroutines = true, true
	vm.Limits.Tracker.GoroutineCount.Store(1)
	live := retirePair(t, arena)
	retirePair(t, arena)
	vm.CallStack[0].Registers.General = []reflect.Value{reflect.NewAt(reflect.TypeFor[gcTracePair](), live)}

	arena.MinorGC(vm)

	require.True(t, retainsGenericSlabHolding(arena, live), "an unlicensed collection always marks, so the slab a register reaches is kept")
	require.Len(t, arena.oldGenericByteSlabs, 1, "and the slab nothing reaches is reclaimed")
	require.False(t, vm.exportShares, "an unlicensed collection starts no owner section")
	require.False(t, vm.exportGate.ownerWaiting())
}

func TestCompactionReclaimsNothingAfterASkippedMarkWhateverTheLicence(t *testing.T) {
	vm := newGCTestVM()
	vm.CallStack = []CallFrame{{}}
	arena := vm.Arena
	setArenaBudget(t, arena, 1<<30)
	require.False(t, arena.licensed)
	retirePair(t, arena)
	state := acquireGCMarkState(arena)
	defer releaseGCMarkState(state)
	state.failClose(failClosedShared)

	reclaimed, recycled := arena.compactPhase(state)

	require.Zero(t, reclaimed)
	require.Zero(t, recycled)
	require.Len(t, arena.oldGenericByteSlabs, 1, "a mark that did not run vouches for no slab, so none is dropped")
}
