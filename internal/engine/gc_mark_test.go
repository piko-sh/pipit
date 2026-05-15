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
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestEnsureBoolSlice_GrowsCapacityWhenInsufficient(t *testing.T) {
	t.Parallel()
	got := ensureBoolSlice(nil, 4)
	require.Len(t, got, 4)
	for _, value := range got {
		require.False(t, value, "fresh allocation must be zeroed")
	}
}

func TestEnsureBoolSlice_ReusesBackingWhenCapacitySufficient(t *testing.T) {
	t.Parallel()
	source := make([]bool, 0, 8)
	source = append(source, true, true, true)
	got := ensureBoolSlice(source, 4)
	require.Len(t, got, 4)
	for _, value := range got {
		require.False(t, value, "reused backing must be cleared to zero")
	}
}

func TestEnsureBoolSlice_TruncatesAndClears(t *testing.T) {
	t.Parallel()
	source := []bool{true, true, true, true}
	got := ensureBoolSlice(source, 2)
	require.Len(t, got, 2)
	require.False(t, got[0])
	require.False(t, got[1])
}

var (
	gcMarkPointerTestSinkMutex sync.Mutex
	gcMarkPointerTestSink      any
)

func heapPinned[T any](backing []T) []T {
	gcMarkPointerTestSinkMutex.Lock()
	gcMarkPointerTestSink = backing
	gcMarkPointerTestSinkMutex.Unlock()
	return backing
}

func TestPointerInIntSlice(t *testing.T) {
	t.Parallel()
	backing := heapPinned([]int64{1, 2, 3, 4, 5})
	stride := uintptr(unsafe.Sizeof(int64(0)))
	base := uintptr(unsafe.Pointer(&backing[0]))

	require.True(t, pointerInIntSlice(base, backing, stride))
	require.True(t, pointerInIntSlice(base+stride*2, backing, stride))
	require.True(t, pointerInIntSlice(base+stride*4, backing, stride),
		"last element should be in-range")
	require.False(t, pointerInIntSlice(base+stride*uintptr(len(backing)), backing, stride),
		"one past the end should be out-of-range")
	require.False(t, pointerInIntSlice(base-stride, backing, stride),
		"one before the start should be out-of-range")
	require.False(t, pointerInIntSlice(base, nil, stride),
		"empty backing should always return false")
}

func TestPointerInFloatSlice(t *testing.T) {
	t.Parallel()
	backing := heapPinned([]float64{1.0, 2.0, 3.0})
	stride := uintptr(unsafe.Sizeof(float64(0)))
	base := uintptr(unsafe.Pointer(&backing[0]))

	require.True(t, pointerInFloatSlice(base, backing, stride))
	require.True(t, pointerInFloatSlice(base+stride, backing, stride))
	require.False(t, pointerInFloatSlice(base+stride*uintptr(len(backing)), backing, stride))
	require.False(t, pointerInFloatSlice(base, nil, stride))
}

func TestPointerInStringSlice(t *testing.T) {
	t.Parallel()
	backing := heapPinned([]string{"a", "b", "c"})
	headerSize := uintptr(unsafe.Sizeof(""))
	base := uintptr(unsafe.Pointer(&backing[0]))

	require.True(t, pointerInStringSlice(base, backing, headerSize))
	require.True(t, pointerInStringSlice(base+headerSize*2, backing, headerSize))
	require.False(t, pointerInStringSlice(base+headerSize*uintptr(len(backing)), backing, headerSize))
	require.False(t, pointerInStringSlice(base, nil, headerSize))
}

func TestPointerInBoolSlice(t *testing.T) {
	t.Parallel()
	backing := heapPinned([]bool{false, true, false, true, false})
	stride := uintptr(unsafe.Sizeof(false))
	base := uintptr(unsafe.Pointer(&backing[0]))

	require.True(t, pointerInBoolSlice(base, backing, stride))
	require.True(t, pointerInBoolSlice(base+stride*3, backing, stride))
	require.False(t, pointerInBoolSlice(base+stride*uintptr(len(backing)), backing, stride))
	require.False(t, pointerInBoolSlice(base, nil, stride))
}

func TestPointerInUintSlice(t *testing.T) {
	t.Parallel()
	backing := heapPinned([]uint64{10, 20, 30, 40})
	stride := uintptr(unsafe.Sizeof(uint64(0)))
	base := uintptr(unsafe.Pointer(&backing[0]))

	require.True(t, pointerInUintSlice(base, backing, stride))
	require.True(t, pointerInUintSlice(base+stride*2, backing, stride))
	require.False(t, pointerInUintSlice(base+stride*uintptr(len(backing)), backing, stride))
	require.False(t, pointerInUintSlice(base, nil, stride))
}

func TestGCMarkStateReset_ClearsLiveCounters(t *testing.T) {
	t.Parallel()
	state := &gcMarkState{
		CurrentLiveBytes:        128,
		CurrentLiveGenericBytes: 64,
		OldByteSlabLive:         []bool{true, false},
		OldGenericBytesLive:     []bool{true},
		OldSliceHeaderLive:      []bool{true},
		OldIntBackingLive:       []bool{true},
		OldFloatBackingLive:     []bool{true},
		OldStringBackingLive:    []bool{true},
		OldBoolBackingLive:      []bool{true},
		OldUintBackingLive:      []bool{true},
	}

	state.Reset()

	require.Zero(t, state.CurrentLiveBytes)
	require.Zero(t, state.CurrentLiveGenericBytes)
	require.Empty(t, state.OldByteSlabLive)
	require.Empty(t, state.OldGenericBytesLive)
	require.Empty(t, state.OldSliceHeaderLive)
	require.Empty(t, state.OldIntBackingLive)
	require.Empty(t, state.OldFloatBackingLive)
	require.Empty(t, state.OldStringBackingLive)
	require.Empty(t, state.OldBoolBackingLive)
	require.Empty(t, state.OldUintBackingLive)
}

func TestAcquireReleaseGCMarkState(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	state := acquireGCMarkState(arena)
	require.NotNil(t, state)

	releaseGCMarkState(state)
}

func TestMarkByteSlice_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	state := &gcMarkState{}
	markByteSlice(arena, state, nil)
	require.Empty(t, state.OldByteSlabLive,
		"empty slice must not touch state")
}

func TestMarkIntSlice_PointerInCurrentSlab(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	state := &gcMarkState{}

	current := arena.intBackingSlab[:4]
	require.NotEmpty(t, current, "test precondition: arena should have backing")

	markIntSlice(arena, state, current)
	require.Empty(t, state.OldIntBackingLive,
		"current-slab pointers should not advance the retention bitmap")
}

func TestMarkIntSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()

	retained := make([]int64, 8)
	arena.oldIntBackings = append(arena.oldIntBackings, retained)

	state := &gcMarkState{OldIntBackingLive: make([]bool, 1)}

	markIntSlice(arena, state, retained[:4])

	require.True(t, state.OldIntBackingLive[0],
		"a slice pointing into the retained slab must mark that retention entry live")
}

func TestMarkFloatSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	retained := make([]float64, 8)
	arena.oldFloatBackings = append(arena.oldFloatBackings, retained)
	state := &gcMarkState{OldFloatBackingLive: make([]bool, 1)}

	markFloatSlice(arena, state, retained[:4])
	require.True(t, state.OldFloatBackingLive[0])
}

func TestMarkStringSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	retained := make([]string, 4)
	for index := range retained {
		retained[index] = "hello"
	}
	arena.oldStringBackings = append(arena.oldStringBackings, retained)
	state := &gcMarkState{OldStringBackingLive: make([]bool, 1)}

	markStringSlice(arena, state, retained[:2])
	require.True(t, state.OldStringBackingLive[0])
}

func TestMarkBoolSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	retained := make([]bool, 8)
	arena.oldBoolBackings = append(arena.oldBoolBackings, retained)
	state := &gcMarkState{OldBoolBackingLive: make([]bool, 1)}

	markBoolSlice(arena, state, retained[:4])
	require.True(t, state.OldBoolBackingLive[0])
}

func TestMarkUintSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	retained := make([]uint64, 8)
	arena.oldUintBackings = append(arena.oldUintBackings, retained)
	state := &gcMarkState{OldUintBackingLive: make([]bool, 1)}

	markUintSlice(arena, state, retained[:4])
	require.True(t, state.OldUintBackingLive[0])
}

func TestMarkByteSlice_PointerInRetainedSlabMarksIt(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	retained := make([]byte, 32)
	arena.oldByteSlabs = append(arena.oldByteSlabs, retained)
	state := &gcMarkState{OldByteSlabLive: make([]bool, 1)}

	markByteSlice(arena, state, retained[:16])
	require.True(t, state.OldByteSlabLive[0])
}

func TestMarkString_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	state := &gcMarkState{}
	markString(arena, state, "")
	require.Empty(t, state.OldByteSlabLive)
}

func TestMarkUpvalueCell_NilIsNoOp(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	state := &gcMarkState{}
	markUpvalueCell(arena, state, nil)
}

func TestMarkRuntimeClosure_NilIsNoOp(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	state := &gcMarkState{}
	markRuntimeClosure(arena, state, nil)
}

func TestMarkPhase_NoOpWithNilArena(t *testing.T) {
	t.Parallel()
	vm := newTestVM(t)
	vm.Arena = nil
	state := &gcMarkState{}

	vm.markPhase(state)

	require.Zero(t, state.CurrentLiveBytes, "nil arena should leave state untouched")
}
