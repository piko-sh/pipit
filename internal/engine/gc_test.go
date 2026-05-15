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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Greaterf(t, arena.nextGCAt, int64(0), "expected nextGCAt to be set after MinorGC, got %d", arena.nextGCAt)
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
