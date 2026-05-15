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

	"pipit.sh/pipit/internal/fault"
)

func TestArenaAllocatorsHandOutDistinctBuffers(t *testing.T) {
	t.Parallel()

	t.Run("byte backing is not shared between allocations", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		first := arena.AllocByteBacking(4)
		second := arena.AllocByteBacking(4)
		first[0] = 1
		second[0] = 2

		require.Len(t, first, 4)
		require.Len(t, second, 4)
		require.Equal(t, byte(1), first[0], "a later allocation must not overwrite an earlier one")
	})

	t.Run("byte backing is capped to its own length", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		buffer := arena.AllocByteBacking(4)

		require.Equal(t, 4, cap(buffer),
			"a full three-index slice keeps an append from scribbling over the next allocation")
	})

	t.Run("a zero-length request yields an empty buffer", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		require.Empty(t, arena.AllocByteBacking(0))
		require.Empty(t, arena.AllocByteBacking(-1))
	})

	t.Run("every typed backing allocator returns the requested length", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		require.Len(t, arena.AllocIntBacking(3), 3)
		require.Len(t, arena.AllocFloatBacking(3), 3)
		require.Len(t, arena.AllocStringBacking(3), 3)
		require.Len(t, arena.AllocBoolBacking(3), 3)
		require.Len(t, arena.AllocUintBacking(3), 3)
		require.Len(t, arena.AllocStringBytes(3), 3)
	})

	t.Run("a large request grows the slab rather than failing", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		buffer := arena.AllocByteBacking(1 << 16)

		require.Len(t, buffer, 1<<16)
	})

	t.Run("raw allocation honours the requested alignment", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		for _, align := range []uintptr{1, 2, 4, 8} {
			pointer := arena.AllocBytes(16, align)

			require.NotNil(t, pointer)
			require.Zerof(t, uintptr(pointer)&(align-1), "an allocation must satisfy its %d-byte alignment", align)
		}
	})

	t.Run("a zero-size raw allocation yields the shared sentinel", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)

		require.Equal(t, arena.AllocBytes(0, 8), arena.AllocBytes(0, 8),
			"zero-size allocations share one address rather than consuming the slab")
	})
}

func TestArenaSavePointRestoresEveryBankIndex(t *testing.T) {
	t.Parallel()

	arena := newTestArena(t)
	arena.AllocByteBacking(32)
	arena.AllocIntBacking(4)

	var save ArenaSavePoint
	arena.SaveInto(&save)
	before := arenaTotalAllocatedBytes(arena)

	arena.AllocByteBacking(64)
	arena.AllocIntBacking(8)
	arena.Restore(save)

	var after ArenaSavePoint
	arena.SaveInto(&after)

	require.Equal(t, save, after, "restoring a save point must rewind every bank index")
	require.GreaterOrEqual(t, arenaTotalAllocatedBytes(arena), before,
		"the running allocation total records what was handed out, not what is live")
}

func TestArenaResetRewindsToTheStart(t *testing.T) {
	t.Parallel()

	arena := newTestArena(t)
	arena.AllocByteBacking(32)
	arena.AllocIntBacking(4)

	arena.Reset()

	var after ArenaSavePoint
	arena.SaveInto(&after)

	require.Equal(t, ArenaSavePoint{}, after, "a reset arena starts from the beginning of every slab")
}

func TestArenaRefusesASingleAllocationPastItsLimit(t *testing.T) {
	t.Parallel()

	t.Run("an allocation within the limit succeeds", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxAllocSize = 16

		require.Len(t, arena.AllocByteBacking(16), 16)
	})

	t.Run("an allocation past the limit panics with the allocation sentinel", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxAllocSize = 16

		defer func() {
			recovered := recover()
			require.NotNil(t, recovered, "an over-large allocation must be refused")
			err, ok := recovered.(error)
			require.True(t, ok, "the arena panics with an error value")
			require.ErrorIs(t, err, fault.ErrAllocationLimit)
		}()

		arena.AllocByteBacking(17)
	})

	t.Run("no limit admits any size", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxAllocSize = 0

		require.Len(t, arena.AllocByteBacking(1<<14), 1<<14)
	})
}

func TestArenaBudgetLimitFallsBackToTheDefault(t *testing.T) {
	t.Parallel()

	t.Run("an unset budget uses the package default", func(t *testing.T) {
		t.Parallel()
		arena := newTestArena(t)
		arena.MaxArenaBytes = 0

		require.Equal(t, defaultMaxArenaBytes, arena.arenaBudgetLimit())
	})

	t.Run("a configured budget is used as given", func(t *testing.T) {
		t.Parallel()
		arena := newBudgetedArena(t, 4096)

		require.Equal(t, uint64(4096), arena.arenaBudgetLimit())
	})
}

func TestChargeArenaAllocationEnforcesTheBudget(t *testing.T) {
	t.Parallel()

	t.Run("a charge within the budget is recorded", func(t *testing.T) {
		t.Parallel()
		arena := newBudgetedArena(t, 1<<20)
		before := arenaTotalAllocatedBytes(arena)

		arena.ChargeArenaAllocation(1024, 0)

		require.Equal(t, before+1024, arenaTotalAllocatedBytes(arena))
	})

	t.Run("a shrinking charge releases the difference", func(t *testing.T) {
		t.Parallel()
		arena := newBudgetedArena(t, 1<<20)
		arena.ChargeArenaAllocation(2048, 0)
		before := arenaTotalAllocatedBytes(arena)

		arena.ChargeArenaAllocation(512, 1024)

		require.Less(t, arenaTotalAllocatedBytes(arena), before,
			"shrinking a buffer must give the bytes back to the budget")
	})

	t.Run("a charge past the budget panics with the budget sentinel", func(t *testing.T) {
		t.Parallel()
		arena := newBudgetedArena(t, 1024)

		require.Panics(t, func() { arena.ChargeArenaAllocation(1<<20, 0) },
			"a charge past the budget must not be allowed to proceed")
	})

	t.Run("the budget sentinel names the arena limit", func(t *testing.T) {
		t.Parallel()
		arena := newBudgetedArena(t, 1024)

		defer func() {
			recovered := recover()
			require.NotNil(t, recovered)
			err, ok := recovered.(error)
			require.True(t, ok, "the arena panics with an error value")
			require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
		}()

		arena.ChargeArenaAllocation(1<<20, 0)
	})
}

func TestReleaseArenaBytesNeverUnderflows(t *testing.T) {
	t.Parallel()

	arena := newBudgetedArena(t, 1<<20)
	arena.ChargeArenaAllocation(512, 0)

	arena.releaseArenaBytes(1 << 30)

	require.LessOrEqual(t, arenaTotalAllocatedBytes(arena), uint64(512),
		"releasing more than was charged must not wrap the running total")
}
