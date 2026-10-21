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
	"sync"

	"pipit.sh/pipit/internal/engine/program"
)

// heapDataArenaPool holds the register files of released heap-data arenas.
var heapDataArenaPool sync.Pool

// heapDataReachCache memoises typeReachesOnlyHeapData by reflect.Type.
var heapDataReachCache typedCache[reflect.Type, bool]

// newHeapDataArena creates an arena with register banks at the pool's initial sizes and
// no allocated data slabs.
//
// Returns *RegisterArena which is the new arena.
func newHeapDataArena() *RegisterArena {
	arena := new(RegisterArena)
	initHeapDataRegisterFile(arena)
	arena.heapData = true
	arena.recycleChunks = false
	return arena
}

// initHeapDataRegisterFile populates a fresh heap-data arena's register file (register
// banks, frames, upvalue slabs) at the pool's initial sizes, as initRegisterArenaInPlace
// does, and leaves every data slab (the byte slab, the typed slice backings and the
// slice-header slab) unallocated; ensureCapacity does not pre-size them from the
// program's hints.
//
// Takes arena (*RegisterArena) which is the arena to initialise; must be non-nil.
func initHeapDataRegisterFile(arena *RegisterArena) {
	arena.recycleChunks = ArenaChunkRecyclingEnabled
	arena.intSlab = make([]int64, initialIntSlabs)
	arena.floatSlab = make([]float64, initialFloatSlabs)
	arena.stringSlab = make([]string, initialStringSlabs)
	arena.generalSlab = make([]reflect.Value, initialGeneralSlabs)
	arena.boolSlab = make([]bool, initialBoolSlabs)
	arena.uintSlab = make([]uint64, initialUintSlabs)
	arena.complexSlab = make([]complex128, initialComplexSlabs)
	arena.slicesIntSlab = make([][]int64, initialSlicesIntSlabs)
	arena.slicesFloatSlab = make([][]float64, initialSlicesFloatSlabs)
	arena.slicesStringSlab = make([][]string, initialSlicesStringSlabs)
	arena.slicesBoolSlab = make([][]bool, initialSlicesBoolSlabs)
	arena.slicesUintSlab = make([][]uint64, initialSlicesUintSlabs)
	arena.slicesByteSlab = make([][]byte, initialSlicesByteSlabs)
	arena.slabGeneration++
	arena.frameSlab = make([]CallFrame, initialFrameSlabs)
	arena.callInfoBasesSlab = make([]uintptr, initialFrameSlabs)
	arena.dispatchSavesSlab = make([]asmDispatchSave, initialFrameSlabs)
	arena.upvalueCellSlab = make([]program.UpvalueCell, initialUpvalueCellSlabs)
	arena.upvalueReferenceSlab = make([]upvalue, initialUpvalueRefSlabs)
}

// dataSlabCapacity returns the capacity for a growing data slab.
//
// Takes ordinary (int) which is the capacity the ordinary growth rule chose.
// Takes request (int) which is the allocation that triggered the growth.
//
// Returns int which is the slab's new capacity.
func (a *RegisterArena) dataSlabCapacity(ordinary, request int) int {
	if !a.heapData {
		return ordinary
	}
	return max(request, 1)
}

// acquireHeapDataArena returns a heap-data arena for a variable initialiser.
//
// Takes abandon (bool) which is VMLimits.abandonsArenas for the acquiring VM.
//
// Returns *RegisterArena which is ready for use.
func acquireHeapDataArena(abandon bool) *RegisterArena {
	if !abandon {
		if pooled, ok := heapDataArenaPool.Get().(*RegisterArena); ok && pooled != nil {
			return pooled
		}
	}
	return newHeapDataArena()
}

// releaseHeapDataArena ends a heap-data arena's run.
//
// Its data slabs are dropped first, before anything that would clear, poison, audit or
// recycle them. The package-level variables still hold that memory. The register file is
// then audited (in the reuse audit build), reset and pooled, unless it must not be
// reused, when it is dropped. The arena was aborted, the VM must not reuse arena storage,
// or an argument to a native that may keep it reached the arena's boxes or cells
// (unpooled, abandonAtRelease).
//
// Takes abandon (bool) which is VMLimits.abandonsArenas for the releasing VM.
// Takes arena (*RegisterArena) which is a heap-data arena no goroutine still uses.
func releaseHeapDataArena(abandon bool, arena *RegisterArena) {
	arena.dropHeapData()
	if abandon || arena.aborted || arena.unpooled || arena.abandonAtRelease {
		if arenaAuditEnabled {
			arena.auditRunEnded()
		}
		arena.detachBudgetTracker()
		return
	}
	if arenaAuditEnabled {
		arena.auditRelease()
	}
	arena.detachBudgetTracker()
	arena.Reset()
	arena.disableMinorGC = true
	arena.recycleChunks = false
	heapDataArenaPool.Put(arena)
}

// dropHeapData forgets every data slab of a heap-data arena, current, retired and
// recycled, and the allocation positions within them.
//
// The box slabs are not data slabs here. They stay, and are cleared by Reset, as in any
// arena.
func (a *RegisterArena) dropHeapData() {
	a.replaceByteSlab(nil, true)
	a.oldByteSlabs, a.byteIndex = nil, 0
	a.dropMutByteSlabs()
	a.krefDropAll()
	a.genericBytesSlab, a.oldGenericByteSlabs, a.freeGenericByteChunks, a.genericBytesIndex = nil, nil, nil, 0
	a.sliceHeaderSlab, a.oldSliceHeaderSlabs, a.freeSliceHeaderChunks, a.sliceHeaderIndex = nil, nil, nil, 0
	a.intBackingSlab, a.oldIntBackings, a.intBackingIndex = nil, nil, 0
	a.floatBackingSlab, a.oldFloatBackings, a.floatBackingIndex = nil, nil, 0
	a.stringBackingSlab, a.oldStringBackings, a.stringBackingIndex = nil, nil, 0
	a.boolBackingSlab, a.oldBoolBackings, a.boolBackingIndex = nil, nil, 0
	a.uintBackingSlab, a.oldUintBackings, a.uintBackingIndex = nil, nil, 0
	a.slabGeneration++
}

// EnsureVarInitCallStack is EnsureCallStack for a variable-initialiser run.
//
// The arena it acquires is a heap-data arena (see acquireHeapDataArena), unless the VM
// uses a custom arena factory or safe mode, when it is EnsureCallStack's arena. The
// caller must defer vm.ReleaseArena().
func (vm *VM) EnsureVarInitCallStack() {
	if vm.Arena == nil && vm.Limits.ArenaFactory == nil && !vm.Limits.SafeMode {
		arena := acquireHeapDataArena(vm.Limits.abandonsArenas())
		arena.setRunBudget(vm.Limits.MaxArenaBytes)
		arena.MaxAllocSize = vm.Limits.MaxAllocSize
		arena.disableMinorGC = true
		arena.recycleChunks = false
		arena.cloneUnownedStrings = vm.reentrantInterpreterVM
		arena.AttachBudgetTracker(vm.Limits.Tracker)
		if arenaAuditEnabled {
			arena.auditAcquired(vm.debugKind.String())
		}
		vm.Arena = arena
	}
	vm.EnsureCallStack()
}

// typeReachesOnlyHeapData reports whether a value of type valueType can reach memory only
// through string bytes and slice backings.
//
// Takes valueType (reflect.Type) which is the type to inspect.
//
// Returns bool which is true when valueType reaches only heap data.
func typeReachesOnlyHeapData(valueType reflect.Type) bool {
	if reaches, ok := heapDataReachCache.load(valueType); ok {
		return reaches
	}
	reaches := computeTypeReachesOnlyHeapData(valueType, map[reflect.Type]bool{})
	heapDataReachCache.store(valueType, reaches)
	return reaches
}

// computeTypeReachesOnlyHeapData is typeReachesOnlyHeapData without the memo.
//
// Takes valueType (reflect.Type) which is the type to inspect.
// Takes visiting (map[reflect.Type]bool) which holds the types on the current path, so a
// type that contains itself through a slice terminates.
//
// Returns bool which is true when valueType reaches only heap data.
func computeTypeReachesOnlyHeapData(valueType reflect.Type, visiting map[reflect.Type]bool) bool {
	if visiting[valueType] {
		return true
	}
	switch valueType.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		return true
	case reflect.Array, reflect.Slice:
		visiting[valueType] = true
		defer delete(visiting, valueType)
		return computeTypeReachesOnlyHeapData(valueType.Elem(), visiting)
	case reflect.Struct:
		visiting[valueType] = true
		defer delete(visiting, valueType)
		for index := range valueType.NumField() {
			if !computeTypeReachesOnlyHeapData(structFieldType(valueType, index).Type, visiting) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
