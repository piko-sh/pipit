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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundaryChunkSlots(t *testing.T) {
	t.Parallel()
	const kib = 1024
	tests := []struct {
		name        string
		elementSize uintptr
		previous    int
		want        int
	}{
		{name: "one-byte first chunk fills the initial budget", elementSize: 1, previous: 0, want: 4096},
		{name: "one-byte later chunk stays at the slot cap", elementSize: 1, previous: 4096, want: 4096},
		{name: "24-byte first chunk is one page", elementSize: 24, previous: 0, want: 170},
		{name: "24-byte second chunk doubles", elementSize: 24, previous: 170, want: 340},
		{name: "24-byte chunk growth stops at the slot cap", elementSize: 24, previous: 3000, want: 4096},
		{name: "2 KiB first chunk holds two", elementSize: 2 * kib, previous: 0, want: 2},
		{name: "2 KiB second chunk holds four", elementSize: 2 * kib, previous: 2, want: 4},
		{name: "2 KiB chunk growth stops at the byte cap", elementSize: 2 * kib, previous: 512, want: 512},
		{name: "1 MiB first chunk holds one", elementSize: kib * kib, previous: 0, want: 1},
		{name: "1 MiB later chunk still holds one", elementSize: kib * kib, previous: 1, want: 1},
		{name: "oversized element still gets one slot", elementSize: 8 * kib * kib, previous: 0, want: 1},
		{name: "zero-size element takes the slot cap", elementSize: 0, previous: 0, want: 4096},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, boundaryChunkSlots(tc.elementSize, tc.previous))
		})
	}
}

type wideBoundaryStruct struct {
	Name string
	Pad  [2032]byte
}

func TestAcquireBoundarySnapshotGrowsSmallChunks(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	elementType := reflect.TypeFor[wideBoundaryStruct]()
	require.EqualValues(t, 2048, elementType.Size())

	first := vm.acquireBoundarySnapshot(elementType)
	chunk := vm.boundarySnapshotChunks[reflectValueABIType(elementType)]
	require.LessOrEqual(t, uintptr(chunk.slab.Len())*elementType.Size(), uintptr(8192), "a 2 KiB struct's first chunk is at most 8 KiB")
	firstChunkLen := chunk.slab.Len()

	slots := make([]reflect.Value, 0, firstChunkLen+3)
	slots = append(slots, first)
	for range firstChunkLen + 2 {
		slots = append(slots, vm.acquireBoundarySnapshot(elementType))
	}
	grown := vm.boundarySnapshotChunks[reflectValueABIType(elementType)]
	require.NotSame(t, chunk, grown, "the type overflowed into a second chunk")
	require.Equal(t, firstChunkLen*2, grown.slab.Len(), "the second chunk doubles")
	require.Len(t, grown.retired, 1)

	seen := make(map[uintptr]bool, len(slots))
	for i, slot := range slots {
		require.True(t, slot.CanAddr(), "slot %d is addressable", i)
		address := slot.Addr().Pointer()
		require.False(t, seen[address], "slot %d shares an address", i)
		seen[address] = true
		slot.Field(0).SetString("kept")
	}
	require.True(t, vm.ownsBoundarySnapshot(first.Addr().UnsafePointer()), "a slot from the retired chunk is still recognised")
	require.True(t, vm.ownsBoundarySnapshot(slots[len(slots)-1].Addr().UnsafePointer()))
	require.False(t, vm.ownsBoundarySnapshot(reflect.ValueOf(new(wideBoundaryStruct)).UnsafePointer()), "a heap value is not")
	require.Equal(t, "kept", first.Field(0).String(), "retiring a chunk leaves its slots intact")
}

func TestAcquireBoundarySnapshotForgetsOldestRetiredChunk(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	elementType := reflect.TypeFor[wideBoundaryStruct]()
	oldest := vm.acquireBoundarySnapshot(elementType)
	retiredChunks := 0
	for retiredChunks <= maxRetainedOldSlabsPerType {
		chunk := vm.boundarySnapshotChunks[reflectValueABIType(elementType)]
		for chunk.nextIdx < chunk.slab.Len() {
			vm.acquireBoundarySnapshot(elementType)
		}
		vm.acquireBoundarySnapshot(elementType)
		retiredChunks++
	}
	current := vm.boundarySnapshotChunks[reflectValueABIType(elementType)]
	require.Len(t, current.retired, maxRetainedOldSlabsPerType)
	require.False(t, vm.ownsBoundarySnapshot(oldest.Addr().UnsafePointer()), "the retained list is bounded")
	require.True(t, vm.ownsBoundarySnapshot(current.retired[0].Index(0).Addr().UnsafePointer()))
}
