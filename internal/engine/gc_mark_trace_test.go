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

type gcTracePair struct {
	A int64
	B int64
}

func retireGenericBytesAllocation(t *testing.T, a *RegisterArena, size, align uintptr) (unsafe.Pointer, int) {
	t.Helper()
	pointer := a.AllocBytes(size, align)
	a.growGenericBytesSlab(1)
	index := len(a.oldGenericByteSlabs) - 1
	require.GreaterOrEqual(t, index, 0)
	return pointer, index
}

func retireSliceHeader(t *testing.T, a *RegisterArena) (*arenaSliceHeader, int) {
	t.Helper()
	header := a.allocSliceHeader()
	a.growSliceHeaderSlab()
	index := len(a.oldSliceHeaderSlabs) - 1
	require.GreaterOrEqual(t, index, 0)
	return header, index
}

func TestMarkReflectValueTracesPointerIntoRetiredGenericChunk(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, index := retireGenericBytesAllocation(t, a, pairType.Size(), uintptr(pairType.Align()))
	root := reflect.NewAt(pairType, pointer)
	require.Equal(t, reflect.Pointer, root.Kind())

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	require.True(t, state.OldGenericBytesLive[index], "a pointer root keeps its pointee's chunk alive")
}

func TestMarkReflectValueTracesStructSliceBacking(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	pairType := reflect.TypeFor[gcTracePair]()
	header := a.allocSliceHeader()
	backing, index := retireGenericBytesAllocation(t, a, pairType.Size()*4, uintptr(pairType.Align()))
	header.Data = backing
	header.Len = 0
	header.Cap = 4
	root := reflect.NewAt(reflect.SliceOf(pairType), unsafe.Pointer(header)).Elem()
	require.Equal(t, reflect.Slice, root.Kind())

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	require.True(t, state.OldGenericBytesLive[index], "spare capacity of a struct-element slice keeps its backing chunk alive")
}

func TestMarkReflectValueTracesNarrowScalarSliceBacking(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	header := a.allocSliceHeader()
	backing, index := retireGenericBytesAllocation(t, a, 4*8, 4)
	header.Data = backing
	header.Len = 8
	header.Cap = 8
	root := reflect.NewAt(reflect.TypeFor[[]int32](), unsafe.Pointer(header)).Elem()

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	require.True(t, state.OldGenericBytesLive[index], "an int32 slice backing lives in the generic bytes slab")
}

func TestMarkReflectValueTracesPointerToArenaSliceHeader(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	backing, genericIndex := retireGenericBytesAllocation(t, a, 4*8, 4)
	header, headerIndex := retireSliceHeader(t, a)
	header.Data = backing
	header.Len = 8
	header.Cap = 8
	root := reflect.NewAt(reflect.TypeFor[[]int32](), unsafe.Pointer(header))
	require.Equal(t, reflect.Pointer, root.Kind())

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	require.True(t, state.OldSliceHeaderLive[headerIndex], "a pointer to a slice keeps the header chunk alive")
	require.True(t, state.OldGenericBytesLive[genericIndex], "and the header's backing chunk")
}

func TestMarkReflectValueTracesThroughInterfaceRoot(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, index := retireGenericBytesAllocation(t, a, pairType.Size(), uintptr(pairType.Align()))
	var boxed any = reflect.NewAt(pairType, pointer).Interface()
	root := reflect.ValueOf(&boxed).Elem()
	require.Equal(t, reflect.Interface, root.Kind())

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	require.True(t, state.OldGenericBytesLive[index], "an interface root is unwrapped before tracing")
}

func TestMarkReflectValueLeavesUnreferencedChunkDead(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	setArenaBudget(t, a, 1<<30)
	pairType := reflect.TypeFor[gcTracePair]()
	_, index := retireGenericBytesAllocation(t, a, pairType.Size(), uintptr(pairType.Align()))
	other := reflect.New(pairType)

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, other)
	require.False(t, state.OldGenericBytesLive[index], "a heap pointer marks nothing")
}

func TestCompactPhaseKeepsChunkReachedThroughPointerRoot(t *testing.T) {
	t.Parallel()
	a := NewRegisterArena()
	a.recycleChunks = true
	setArenaBudget(t, a, 1<<30)
	for len(a.genericBytesSlab) < genericBytesSlabChunk {
		a.growGenericBytesSlab(1)
	}
	pairType := reflect.TypeFor[gcTracePair]()
	pointer := a.AllocBytes(pairType.Size(), uintptr(pairType.Align()))
	*(*gcTracePair)(pointer) = gcTracePair{A: 3, B: 4}
	a.growGenericBytesSlab(1)
	retired := a.oldGenericByteSlabs[len(a.oldGenericByteSlabs)-1]
	require.Equal(t, genericBytesSlabChunk, cap(retired))
	root := reflect.NewAt(pairType, pointer)

	state := acquireGCMarkState(a)
	defer releaseGCMarkState(state)
	markReflectValue(a, state, root)
	_, recycled := a.compactPhase(state)
	require.Zero(t, recycled, "a chunk reachable only through a pointer root is not recycled")
	require.Empty(t, a.freeGenericByteChunks)
	require.Equal(t, gcTracePair{A: 3, B: 4}, *(*gcTracePair)(pointer))

	dead := acquireGCMarkState(a)
	defer releaseGCMarkState(dead)
	_, recycled = a.compactPhase(dead)
	require.Equal(t, int64(genericBytesSlabChunk), recycled, "the same chunk is recycled once nothing references it")
	require.Equal(t, gcTracePair{}, *(*gcTracePair)(pointer), "a recycled chunk is zeroed")
}
