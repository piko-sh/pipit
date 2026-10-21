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

	"pipit.sh/pipit/internal/engine/program"
)

func TestSharedCellMapPoolKeepsNoMoreThanItsBoundAfterABurst(t *testing.T) {
	bound := stablepoolRetentionBound(sharedCellMapPool.Cap(), sharedCellMapPool.MaxCap())
	burst(2*bound, acquireSharedCellMap, releaseSharedCellMap)
	kept := sharedCellMapPool.Drain()
	t.Cleanup(func() {
		for _, cells := range kept {
			sharedCellMapPool.Put(cells)
		}
	})
	require.LessOrEqual(t, len(kept), bound, "after a burst of %d shared-cell maps the pool keeps at most its bound", 2*bound)
}

func TestSharedCellMapKeepsEveryKeyPastItsInlineStorage(t *testing.T) {
	cells := acquireSharedCellMap()
	t.Cleanup(func() { releaseSharedCellMap(cells) })
	count := 3 * inlineSharedCellsCapacity
	made := make([]*program.UpvalueCell, count)
	for index := range made {
		made[index] = &program.UpvalueCell{}
		cells.set(uint16(index), made[index])
	}
	require.NotEmpty(t, cells.overflow, "keys past the inline slots spill into overflow")
	for index, cell := range made {
		found, ok := cells.get(uint16(index))
		require.True(t, ok)
		require.Same(t, cell, found, "key %d", index)
		require.True(t, cells.has(uint16(index)))
	}
	replacement := &program.UpvalueCell{}
	cells.set(uint16(count-1), replacement)
	cells.set(0, replacement)
	found, _ := cells.get(uint16(count - 1))
	require.Same(t, replacement, found, "setting an overflow key again overwrites it")
	found, _ = cells.get(0)
	require.Same(t, replacement, found, "setting an inline key again overwrites it")

	cells.remove(0)
	cells.remove(uint16(count - 1))
	cells.remove(uint16(count))
	require.False(t, cells.has(0))
	require.False(t, cells.has(uint16(count-1)))
	_, ok := cells.get(uint16(count - 1))
	require.False(t, ok)
	for index := 1; index < count-1; index++ {
		require.True(t, cells.has(uint16(index)), "removing one key keeps key %d", index)
	}
	require.False(t, cells.has(uint16(count+1)))
}

func TestReleasingASharedCellMapForgetsItsCells(t *testing.T) {
	releaseSharedCellMap(nil)
	cells := acquireSharedCellMap()
	for index := range 2 * inlineSharedCellsCapacity {
		cells.set(uint16(index), &program.UpvalueCell{})
	}
	releaseSharedCellMap(cells)
	again := acquireSharedCellMap()
	t.Cleanup(func() { releaseSharedCellMap(again) })
	require.False(t, again.has(0), "a pooled map comes back empty")
	require.Empty(t, again.overflow)
}
