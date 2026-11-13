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
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

var mutBytesVMs = []struct {
	name  string
	newVM func(t *testing.T) *VM
}{
	{"unshared", newLicensedMutBytesVM},
	{"shared", newGoroutineGCTestVM},
}

func TestReuseHazardCompactionRecyclesOnlyCleanChunks(t *testing.T) {
	observations := &hazardObservations{}
	arena := newArena(t)
	arena.recycleChunks = true
	setArenaBudget(t, arena, 1<<30)
	arena.GrowByteSlab(1)
	arena.GrowByteSlab(1)
	growSliceHeaderSlabToChunk(t, arena)
	arena.growSliceHeaderSlab()
	arena.growSliceHeaderSlab()
	for len(arena.genericBytesSlab) < genericBytesSlabChunk {
		arena.growGenericBytesSlab(1)
	}
	arena.growGenericBytesSlab(1)
	arena.growGenericBytesSlab(1)
	arena.GrowIntBackingSlab(1)
	arena.growFloatBackingSlab(1)
	arena.growStringBackingSlab(1)
	arena.growBoolBackingSlab(1)
	arena.growUintBackingSlab(1)

	dropped := map[uintptr]string{}
	collect := func(class string, bases []uintptr) {
		for _, base := range bases {
			dropped[base] = class
		}
	}
	collect("byte", basesOf(arena.oldByteSlabs))
	collect("int", basesOf(arena.oldIntBackings))
	collect("float", basesOf(arena.oldFloatBackings))
	collect("string", basesOf(arena.oldStringBackings))
	collect("bool", basesOf(arena.oldBoolBackings))
	collect("uint", basesOf(arena.oldUintBackings))
	chunks := map[uintptr]string{}
	for _, base := range basesOf(arena.oldGenericByteSlabs) {
		chunks[base] = "generic"
	}
	for _, base := range basesOf(arena.oldSliceHeaderSlabs) {
		chunks[base] = "header"
	}
	revoked := slabBase(arena.oldGenericByteSlabs[len(arena.oldGenericByteSlabs)-1])
	arena.revokeSlab(revoked, handOffIdentityKept)
	byteDead := basesOf(arena.oldByteSlabs)

	state := &gcMarkState{
		OldByteSlabLive: make([]bool, len(arena.oldByteSlabs)), OldGenericBytesLive: make([]bool, len(arena.oldGenericByteSlabs)),
		OldSliceHeaderLive: make([]bool, len(arena.oldSliceHeaderSlabs)), OldIntBackingLive: make([]bool, len(arena.oldIntBackings)),
		OldFloatBackingLive: make([]bool, len(arena.oldFloatBackings)), OldStringBackingLive: make([]bool, len(arena.oldStringBackings)),
		OldBoolBackingLive: make([]bool, len(arena.oldBoolBackings)), OldUintBackingLive: make([]bool, len(arena.oldUintBackings)),
	}
	arena.compactPhase(state)

	free := append(basesOf(arena.freeGenericByteChunks), basesOf(arena.freeSliceHeaderChunks)...)
	for _, base := range free {
		switch {
		case base == revoked:
			observations.fire("recycled-revoked-chunk", "the revoked generic chunk %#x reached a free list", base)
		case dropped[base] != "":
			observations.fire("recycled-"+dropped[base], "a dead %s slab %#x reached a free list", dropped[base], base)
		case chunks[base] == "":
			observations.fire("recycled-unknown", "free list holds %#x, not a dead chunk", base)
		}
	}
	for _, base := range arenaSlabBasesAll(arena) {
		if class := dropped[base]; class != "" {
			observations.fire("retained-"+class, "the dead %s slab %#x is still held by the arena", class, base)
		}
	}
	for _, base := range byteDead {
		if _, _, found := arenaSlabContaining(base); found {
			observations.fire("registry-kept", "the dead byte slab %#x is still registered", base)
		}
	}
	observations.note("dead byte/typed slabs=%d, dead chunks=%d, recycled chunks=%d, audit build=%v",
		len(dropped), len(chunks), len(free), arenaAuditEnabled)
	assertNoHazard(t, observations, "")
}

func basesOf[T any](list [][]T) []uintptr {
	bases := make([]uintptr, 0, len(list))
	for _, slab := range list {
		if base := slabBase(slab); base != 0 {
			bases = append(bases, base)
		}
	}
	return bases
}

func arenaSlabBasesAll(arena *RegisterArena) []uintptr {
	bases := []uintptr{slabBase(arena.byteSlab), slabBase(arena.genericBytesSlab), slabBase(arena.sliceHeaderSlab),
		slabBase(arena.intBackingSlab), slabBase(arena.floatBackingSlab), slabBase(arena.stringBackingSlab),
		slabBase(arena.boolBackingSlab), slabBase(arena.uintBackingSlab)}
	bases = append(bases, basesOf(arena.oldByteSlabs)...)
	bases = append(bases, basesOf(arena.oldGenericByteSlabs)...)
	bases = append(bases, basesOf(arena.freeGenericByteChunks)...)
	bases = append(bases, basesOf(arena.oldSliceHeaderSlabs)...)
	bases = append(bases, basesOf(arena.freeSliceHeaderChunks)...)
	bases = append(bases, basesOf(arena.oldIntBackings)...)
	bases = append(bases, basesOf(arena.oldFloatBackings)...)
	bases = append(bases, basesOf(arena.oldStringBackings)...)
	bases = append(bases, basesOf(arena.oldBoolBackings)...)
	return append(bases, basesOf(arena.oldUintBackings)...)
}

func TestCompactionNeverRecyclesARevokedDeadChunk(t *testing.T) {
	skipUnderReuseAudit(t)
	arena := newArena(t)
	arena.recycleChunks = true
	setArenaBudget(t, arena, 1<<30)
	growSliceHeaderSlabToChunk(t, arena)
	arena.growSliceHeaderSlab()
	arena.growSliceHeaderSlab()
	chunks := arena.oldSliceHeaderSlabs[len(arena.oldSliceHeaderSlabs)-2:]
	require.Equal(t, sliceHeaderSlabChunk, cap(chunks[0]))
	require.Equal(t, sliceHeaderSlabChunk, cap(chunks[1]))
	revoked := chunks[1]
	arena.revokeSlab(uintptr(unsafe.Pointer(unsafe.SliceData(revoked))), handOffIdentityKept)

	arena.compactPhase(deadSliceHeaderState(arena))
	for _, free := range arena.freeSliceHeaderChunks {
		require.NotEqual(t, uintptr(unsafe.Pointer(unsafe.SliceData(revoked))), uintptr(unsafe.Pointer(unsafe.SliceData(free))),
			"a revoked dead chunk must be dropped, not recycled")
	}
	require.NotEmpty(t, arena.freeSliceHeaderChunks, "the clean dead chunk is still recycled")
}

func newLicensedMutBytesVM(t *testing.T) *VM {
	t.Helper()
	vm := newLicensedMarkVM(t)
	vm.CallStack = []CallFrame{{}}
	return vm
}

func retireMutBytes(t *testing.T, arena *RegisterArena, n int, fill byte) []byte {
	t.Helper()
	require.True(t, arena.splitsMutableBytes(), "test harness: the arena must split its bytes")
	b := arena.AllocByteBacking(n)
	for i := range b {
		b[i] = fill + byte(i)
	}
	retired := len(arena.oldMutByteSlabs)
	arena.growMutByteSlab(1)
	require.Len(t, arena.oldMutByteSlabs, retired+1, "test harness: the slab holding the bytes must retire")
	require.True(t, slabsOwnPointer(nil, arena.oldMutByteSlabs, bytesStart(b)), "test harness: the bytes lie in a retired slab")
	return b
}

func requireBytesRead(t *testing.T, b []byte, fill byte) {
	t.Helper()
	for i := range b {
		require.Equal(t, fill+byte(i), b[i], "byte %d", i)
	}
}

func retiredMutBases(arena *RegisterArena) []uintptr {
	bases := make([]uintptr, 0, len(arena.oldMutByteSlabs))
	for _, slab := range arena.oldMutByteSlabs {
		bases = append(bases, slabBaseOf(slab))
	}
	return bases
}

func TestMinorGCDropsTheRetiredMutableByteSlabsNoValueReaches(t *testing.T) {
	for _, kind := range mutBytesVMs {
		t.Run(kind.name, func(t *testing.T) {
			vm := kind.newVM(t)
			arena := vm.Arena
			for round := range 4 {
				retireMutBytes(t, arena, 64, byte(round))
			}
			bases := retiredMutBases(arena)
			owner := arena.licenceOwnerID()
			for _, base := range bases {
				require.True(t, licencePublishes(owner, base), "test harness: a retired slab is published")
			}

			arena.MinorGC(vm)

			require.Empty(t, arena.oldMutByteSlabs, "every retired slab no value reaches is dropped")
			for _, base := range bases {
				require.False(t, licencePublishes(owner, base), "a dropped slab leaves the licence registry")
				require.False(t, arena.ownsMutBytes(base), "and the arena no longer owns its bytes")
				_, _, found := arenaSlabContaining(base)
				require.False(t, found, "and no byte-slab registry entry names it")
			}
		})
	}
}

func TestMinorGCKeepsTheRetiredMutableByteSlabALiveValueReaches(t *testing.T) {
	type pair struct {
		B []byte

		N int
	}

	holders := []struct {
		name        string
		hold        func(frame *CallFrame, b []byte)
		sharedReads bool
	}{
		{"a []byte register", func(frame *CallFrame, b []byte) { frame.Registers.slicesByte = [][]byte{b} }, true},
		{"a subslice of it", func(frame *CallFrame, b []byte) { frame.Registers.slicesByte = [][]byte{b[10:12]} }, true},
		{"a general register", func(frame *CallFrame, b []byte) { frame.Registers.General = []reflect.Value{reflect.ValueOf(b)} }, false},
		{"a string over its bytes", func(frame *CallFrame, b []byte) {
			frame.Registers.Strings = []string{unsafe.String(&b[4], 8)}
		}, true},
		{"a pointer to one byte", func(frame *CallFrame, b []byte) {
			frame.Registers.General = []reflect.Value{reflect.ValueOf(&b[5])}
		}, true},
		{"a pointer to an array over it", func(frame *CallFrame, b []byte) {
			frame.Registers.General = []reflect.Value{reflect.ValueOf((*[8]byte)(b))}
		}, true},
		{"an unsafe.Pointer", func(frame *CallFrame, b []byte) {
			frame.Registers.General = []reflect.Value{reflect.ValueOf(unsafe.Pointer(&b[3]))}
		}, true},
		{"a struct field", func(frame *CallFrame, b []byte) {
			frame.Registers.General = []reflect.Value{reflect.ValueOf(&pair{B: b, N: 1})}
		}, false},
		{"an interface", func(frame *CallFrame, b []byte) {
			frame.Registers.General = []reflect.Value{reflect.ValueOf([]any{b})}
		}, false},
	}
	for _, kind := range mutBytesVMs {
		for _, holder := range holders {
			t.Run(kind.name+"/"+holder.name, func(t *testing.T) {
				vm := kind.newVM(t)
				arena := vm.Arena
				live := retireMutBytes(t, arena, 64, 7)
				dead := retireMutBytes(t, arena, 64, 9)
				holder.hold(&vm.CallStack[0], live)

				arena.MinorGC(vm)

				require.True(t, slabsOwnPointer(nil, arena.oldMutByteSlabs, bytesStart(live)), "the slab a live value reaches stays")
				if kind.name == "shared" && !holder.sharedReads {
					require.True(t, slabsOwnPointer(nil, arena.oldMutByteSlabs, bytesStart(dead)), "a shared mark that fails closed drops nothing")
				} else {
					require.False(t, slabsOwnPointer(nil, arena.oldMutByteSlabs, bytesStart(dead)),
						"test harness: the collection reclaimed, dropping the slab nothing reaches")
				}
				require.True(t, arena.ownsMutBytes(bytesStart(live)))
				require.True(t, licencePublishes(arena.licenceOwnerID(), slabBaseOf(arena.oldMutByteSlabs[0])))
				requireBytesRead(t, live, 7)
				runtime.KeepAlive(dead)
			})
		}
	}
}

func TestDroppingARetiredMutableByteSlabNeverReusesIt(t *testing.T) {
	for _, kind := range mutBytesVMs {
		for _, revoked := range []bool{false, true} {
			name := kind.name + "/held where the mark cannot see"
			if revoked {
				name = kind.name + "/handed off"
			}
			t.Run(name, func(t *testing.T) {
				vm := kind.newVM(t)
				arena := vm.Arena
				kept := retireMutBytes(t, arena, 256, 3)
				start := slabBaseOf(arena.oldMutByteSlabs[0])
				end := start + uintptr(cap(arena.oldMutByteSlabs[0]))
				if revoked {
					noteHandOff(arena, handOffUntracedContainer, reflect.ValueOf(kept))
					require.True(t, licenceRevokedSlab(arena, arena.oldMutByteSlabs[0]), "test harness: the hand-off revoked the slab")
				}

				arena.MinorGC(vm)
				require.Empty(t, arena.oldMutByteSlabs, "the slab is dropped although a value outside the mark still holds it")
				runtime.GC()

				for round := range 64 {
					fresh := arena.AllocByteBacking(1024)
					for i := range fresh {
						fresh[i] = 0xFF
					}
					address := bytesStart(fresh)
					require.False(t, address+uintptr(len(fresh)) > start && address < end, "allocation %d landed in the dropped slab", round)
					if round%8 == 7 {
						arena.growMutByteSlab(1)
						arena.MinorGC(vm)
					}
				}
				runtime.GC()
				requireBytesRead(t, kept, 3)
			})
		}
	}
}

func TestDroppingARetiredMutableByteSlabHandsBackTheChargeItCarries(t *testing.T) {
	for _, kind := range mutBytesVMs {
		t.Run(kind.name+"/a slab whose charge went to its replacement", func(t *testing.T) {
			vm := kind.newVM(t)
			arena := vm.Arena
			for round := range 4 {
				retireMutBytes(t, arena, 64, byte(round))
			}
			held := arena.totalAllocatedBytes
			require.Equal(t, uint64(cap(arena.mutByteSlab)), held,
				"test harness: the current slab carries the charge its predecessors handed on")

			arena.MinorGC(vm)

			require.Empty(t, arena.oldMutByteSlabs)
			require.Equal(t, held, arena.totalAllocatedBytes, "dropping the retired slabs releases nothing, unshared or shared")
		})
		t.Run(kind.name+"/a slab charged on its own", func(t *testing.T) {
			vm := kind.newVM(t)
			arena := vm.Arena
			retireMutBytes(t, arena, 64, 1)
			retired := arena.oldMutByteSlabs[0]
			delete(arena.handedOnCharges, slabChargeKey(retired))
			arena.totalAllocatedBytes += uint64(cap(retired))
			held := arena.totalAllocatedBytes

			arena.MinorGC(vm)

			require.Empty(t, arena.oldMutByteSlabs)
			require.Equal(t, held-uint64(cap(retired)), arena.totalAllocatedBytes, "the dropped slab takes exactly its own charge with it")
			require.NotContains(t, arena.handedOnCharges, slabChargeKey(retired))
		})
	}
}

func TestTrimmingRetiredMutableByteSlabsKeepsTheChargeAndItsRecords(t *testing.T) {
	arena := newArena(t)
	setArenaBudget(t, arena, 1<<30)
	for range 2 * maxRetainedOldSlabsPerType {
		if len(arena.mutByteSlab) > initialMutByteSlabSize {
			arena.mutByteSlab = arena.mutByteSlab[:initialMutByteSlabSize]
		}
		arena.growMutByteSlab(1)
	}
	require.Len(t, arena.oldMutByteSlabs, maxRetainedOldSlabsPerType)
	require.Equal(t, uint64(cap(arena.mutByteSlab)), arena.totalAllocatedBytes,
		"each trimmed slab handed its charge on, so trimming it releases nothing")
	require.Len(t, arena.handedOnCharges, maxRetainedOldSlabsPerType, "a trimmed slab's record goes with it")
}

func TestCollectionsCountTheMutableByteSlabsTheyDrop(t *testing.T) {
	previous := SetLicenceCountingForTest(true)
	t.Cleanup(func() { SetLicenceCountingForTest(previous) })
	vm := newLicensedMutBytesVM(t)
	arena := vm.Arena
	for round := range 3 {
		retireMutBytes(t, arena, 64, byte(round))
	}
	capacity := 0
	for _, slab := range arena.oldMutByteSlabs {
		capacity += cap(slab)
	}
	before := LicenceDiagnostics()

	arena.MinorGC(vm)

	after := LicenceDiagnostics()
	require.Equal(t, int64(3), after.MutableByteSlabsDropped-before.MutableByteSlabsDropped)
	require.Equal(t, int64(capacity), after.MutableByteBytesDropped-before.MutableByteBytesDropped)
}
