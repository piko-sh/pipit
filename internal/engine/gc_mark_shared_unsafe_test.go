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
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
)

func TestSharedMarkReadsAHeaderInItsOwnUncoveredStorage(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	backing := retirePair(t, arena)
	retirePair(t, arena)
	header := arena.allocSliceHeader()
	*header = arenaSliceHeader{Data: backing, Len: 1, Cap: 1}
	slice := unsafeNewAt(reflectValueABIType(reflect.TypeFor[[]gcTracePair]()), unsafe.Pointer(header), reflect.Slice)
	vm.CallStack[0].Registers.General = []reflect.Value{slice}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 1, "the dead slab is reclaimed")
	require.True(t, retainsGenericSlabHolding(arena, backing), "the backing a header in the arena's own storage names is kept")
}

func TestSharedMarkFailsClosedOnAHeaderOutsideItsOwnStorage(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	backing := retirePair(t, arena)
	retirePair(t, arena)
	header := &arenaSliceHeader{Data: backing, Len: 1, Cap: 1}
	slice := unsafeNewAt(reflectValueABIType(reflect.TypeFor[[]gcTracePair]()), unsafe.Pointer(header), reflect.Slice)
	vm.CallStack[0].Registers.General = []reflect.Value{slice}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 2, "a header another goroutine could be writing is not read, so nothing is reclaimed")
}

func TestSharedMarkReadsItsOwnKRefSlots(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	live := retirePair(t, arena)
	retirePair(t, arena)
	slot := krefTestSlot(t, arena, reflect.TypeFor[*gcTracePair]())
	slot.Set(reflect.NewAt(reflect.TypeFor[gcTracePair](), live))
	vm.CallStack[0].Registers.General = []reflect.Value{slot.Addr()}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 1, "the dead slab is reclaimed")
	require.True(t, retainsGenericSlabHolding(arena, live), "the slab a KRef slot of the arena reaches is kept")
}

func TestSharedMarkReadsNoKRefSlotACoverReaches(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	slot := krefTestSlot(t, arena, reflect.TypeFor[*gcTracePair]())
	chunk, found := arena.krefFind(slotAddress(slot))
	require.True(t, found)
	arena.revokeSlab(chunk.base, handOffSharedStore)
	vm.CallStack[0].Registers.General = []reflect.Value{slot.Addr()}
	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Go(func() {
		for round := 0; ; round++ {
			select {
			case <-stop:
				return
			default:
			}
			slot.Set(reflect.ValueOf(&gcTracePair{A: int64(round)}))
		}
	})
	defer writer.Wait()
	defer close(stop)

	for round := range 12 {
		retirePair(t, arena)
		arena.MinorGC(vm)
		require.Len(t, arena.oldGenericByteSlabs, round+1, "the mark fails closed rather than read a slot another goroutine may write")
	}
}

func TestSharedMarkReadsReflectsZeroValues(t *testing.T) {
	vm := newGoroutineGCTestVM(t)
	arena := vm.Arena
	live := retirePair(t, arena)
	retirePair(t, arena)
	vm.CallStack[0].Registers.General = []reflect.Value{
		reflect.Zero(reflect.TypeFor[[]byte]()),
		reflect.Zero(reflect.TypeFor[gcSharedHolder]()),
		reflect.Zero(reflect.TypeFor[[4]any]()),
		reflect.NewAt(reflect.TypeFor[gcTracePair](), live),
	}

	arena.MinorGC(vm)

	require.Len(t, arena.oldGenericByteSlabs, 1, "a register holding a zero value from reflect.Zero does not fail the mark closed")
	require.True(t, retainsGenericSlabHolding(arena, live))
}

func TestReflectZeroValuesSpanTheBufferReflectServesThemFrom(t *testing.T) {
	require.Equal(t, uintptr(zeroValueBufferBytes), reflectZeroValues.end-reflectZeroValues.start, "reflect serves a %d-byte zero value from the buffer's start", zeroValueBufferBytes)
	for _, reflectType := range []reflect.Type{reflect.TypeFor[[]byte](), reflect.TypeFor[markedRecord](), reflect.TypeFor[[zeroValueBufferBytes]byte]()} {
		require.True(t, reflectZeroValues.holds(uintptr(ReflectValuePtr(reflect.Zero(reflectType)))), "%v", reflectType)
	}
	require.False(t, reflectZeroValues.holds(uintptr(ReflectValuePtr(reflect.Zero(reflect.TypeFor[[zeroValueBufferBytes + 1]byte]())))), "a larger zero value is allocated, outside the buffer")
}

func TestReflectZeroValuesClaimNothingUnlessTwoSizesShareTheBuffer(t *testing.T) {
	require.Equal(t, zeroValueSpan{start: 4096, end: 4096 + zeroValueBufferBytes}, zeroValueSpanFrom(4096, 4096))
	require.Equal(t, zeroValueSpan{}, zeroValueSpanFrom(4096, 8192),
		"a reflect that allocates each zero value afresh proves no shared buffer, so a shared mark fails closed on one")
	require.Equal(t, zeroValueSpan{}, zeroValueSpanFrom(0, 0), "a reflect that returned no pointer claims nothing")
}

func TestSharedMarkReadsACellItsVMMadeThroughAPointer(t *testing.T) {
	for _, testCase := range []struct {
		name string
		root func(cell *program.UpvalueCell) reflect.Value
	}{
		{"a closure in a register", func(cell *program.UpvalueCell) reflect.Value {
			return reflect.ValueOf(&RuntimeClosure{upvalues: []*program.UpvalueCell{cell}})
		}},
		{"a cell pointer in a register", func(cell *program.UpvalueCell) reflect.Value {
			return reflect.ValueOf(cell)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			vm := newGoroutineGCTestVM(t)
			arena := vm.Arena
			live := retirePair(t, arena)
			retirePair(t, arena)
			vm.CallStack[0].Registers.General = []reflect.Value{testCase.root(cellHolding(vm, live))}

			arena.MinorGC(vm)

			require.Len(t, arena.oldGenericByteSlabs, 1, "the slab nothing reaches is reclaimed")
			require.True(t, retainsGenericSlabHolding(arena, live), "the slab a cell the VM made reaches is kept")
		})
	}
}
