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
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestHeapDataArenaClaimsNoDataAndNeverReusesIt(t *testing.T) {
	arena := acquireHeapDataArena(false)
	require.True(t, arena.heapData)
	require.False(t, arena.recycleChunks)

	bytes := arena.AllocStringBytes(5)
	copy(bytes, "hello")
	kept := unsafe.String(&bytes[0], len(bytes))
	ints := arena.AllocIntBacking(4)
	ints[0] = 42
	header := arena.allocSliceHeader()
	generic := arena.AllocBytes(16, 8)
	strs := arena.AllocStringBacking(2)
	strs[0] = kept

	require.False(t, arena.OwnsSliceBacking(unsafe.Pointer(&ints[0])), "a heap-data arena claims none of its backings")
	require.False(t, arena.OwnsSliceBacking(unsafe.Pointer(&bytes[0])))
	require.False(t, arena.ownsStringBacking(unsafe.Pointer(&strs[0])))
	require.False(t, arena.ownsBytePointer(generic))
	if arenaUsesUnsafeSlabs {
		require.False(t, arena.OwnsString(kept), "a heap-data arena claims none of its string bytes")
		require.False(t, arena.OwnsSliceHeaderPointer(unsafe.Pointer(header)))
		box := arena.allocStringBox("boxed")
		require.True(t, arena.OwnsStringBox(unsafe.Pointer(box)), "boxes stay arena storage and are re-boxed on escape")
	}

	releaseRegisterArena(false, arena, false)
	require.Nil(t, arena.byteSlab)
	require.Nil(t, arena.oldByteSlabs)
	require.Nil(t, arena.intBackingSlab)
	require.Nil(t, arena.genericBytesSlab)
	require.Nil(t, arena.sliceHeaderSlab)

	for range 8 {
		next := acquireHeapDataArena(false)
		overwrite := next.AllocStringBytes(5)
		copy(overwrite, "XXXXX")
		backing := next.AllocIntBacking(4)
		backing[0] = -1
		releaseRegisterArena(false, next, false)
	}
	require.Equal(t, "hello", kept, "released heap data is never handed out again")
	require.EqualValues(t, 42, ints[0])
	require.Equal(t, "hello", strs[0])
}

func TestHeapDataArenaChargesEveryAllocation(t *testing.T) {
	arena := acquireHeapDataArena(true)
	before := arena.totalAllocatedBytes
	for range 10 {
		arena.AllocIntBacking(512)
	}
	require.GreaterOrEqual(t, arena.totalAllocatedBytes-before, uint64(10*512*8))
	releaseRegisterArena(true, arena, false)
}

func drainHeapDataArenaPool() []*RegisterArena {
	var drained []*RegisterArena
	for {
		arena, ok := heapDataArenaPool.Get().(*RegisterArena)
		if !ok || arena == nil {
			return drained
		}
		drained = append(drained, arena)
	}
}

func TestTypeReachesOnlyHeapData(t *testing.T) {
	type row struct {
		name   string
		scores []int
		grid   [2]float64
	}
	type withPointer struct {
		name string
		next *withPointer
	}
	type tree struct{ children []tree }
	cases := []struct {
		valueType reflect.Type
		name      string
		want      bool
	}{
		{name: "scalars and strings", valueType: reflect.TypeFor[row](), want: true},
		{name: "a slice of rows", valueType: reflect.TypeFor[[]row](), want: true},
		{name: "a type containing itself through a slice", valueType: reflect.TypeFor[tree](), want: true},
		{name: "a pointer field", valueType: reflect.TypeFor[withPointer](), want: false},
		{name: "a map", valueType: reflect.TypeFor[map[string]int](), want: false},
		{name: "an interface", valueType: reflect.TypeFor[[]any](), want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, typeReachesOnlyHeapData(testCase.valueType))
			require.Equal(t, testCase.want, typeReachesOnlyHeapData(testCase.valueType), "the cached answer agrees")
		})
	}
}

func TestEnsureVarInitCallStackUsesAHeapDataArena(t *testing.T) {
	vm := newTestVM(t)
	vm.Limits.MaxArenaBytes = 1 << 20
	vm.EnsureVarInitCallStack()
	defer vm.ReleaseArena()
	require.NotNil(t, vm.Arena)
	require.True(t, vm.Arena.heapData, "a variable initialiser's data stays on the heap")
	require.True(t, vm.Arena.disableMinorGC)
	require.EqualValues(t, 1<<20, vm.Arena.MaxArenaBytes)
	require.NotNil(t, vm.CallStack)

	safe := newTestVM(t)
	safe.Limits.SafeMode = true
	safe.EnsureVarInitCallStack()
	defer safe.ReleaseArena()
	if safe.Arena != nil {
		require.False(t, safe.Arena.heapData, "safe mode keeps the ordinary arena")
	}
}
