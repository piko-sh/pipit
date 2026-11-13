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
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
)

const (
	// pointeeMaxSites bounds the pointee placements an epoch records; a site beyond it takes
	// the heap.
	pointeeMaxSites = 64
)

// pointeeSite is one reference-holding pointee placement of an epoch.
type pointeeSite struct {
	// function holds the site.
	function *program.CompiledFunction

	// programCounter is the site's program counter.
	programCounter int

	// base is the start of the chunk the pointee went to.
	base uintptr
}

// krefPlacesFor reports whether vm's arena may place values in KRef chunks.
//
// KRef placement applies to the arena (krefPlaces), no debugger is attached and the
// program does not call runtime.SetFinalizer. A KRef chunk keeps what its slots reference
// reachable until the chunk is recycled, and its slots are never Go allocations of their
// own (allocatesForFinalisers).
//
// Takes vm (*VM) which is the allocating VM.
//
// Returns bool which is true when KRef placement applies to vm.
func krefPlacesFor(vm *VM) bool {
	return vm.Limits.Debug == nil && vm.Arena.krefPlaces() && !vm.allocatesForFinalisers()
}

// krefSnapshot returns a KRef slot for a boundary snapshot of type valueType.
//
// Takes valueType (reflect.Type) which is the snapshot's type.
//
// Returns reflect.Value which is the zeroed, addressable slot.
// Returns bool which is false when the snapshot is not placed in KRef.
func (vm *VM) krefSnapshot(valueType reflect.Type) (reflect.Value, bool) {
	if !krefPlacesFor(vm) {
		return reflect.Value{}, false
	}
	arena := vm.Arena
	key := reflectValueABIType(valueType)
	class := arena.kref.classes[key]
	if class != nil && class.snapshotVetoed {
		class.used = true
		return reflect.Value{}, false
	}
	slot, ok := arena.krefSlot(valueType)
	if ok {
		if class == nil {
			class = arena.kref.classes[key]
		}
		if class != nil {
			class.snapshotPlaced = true
		}
		if licenceCounting {
			krefPlacementCounters.snapshotSlots.Add(1)
		}
	}
	return slot, ok
}

// settleSnapshotPlacements is the cost guard of snapshot placement at a licensed release.
func (a *RegisterArena) settleSnapshotPlacements() {
	if len(a.kref.classes) == 0 {
		return
	}
	budget := a.licenceState.epochRevoked && a.licenceState.epochReason == epochWalkBudget
	for _, class := range a.kref.classes {
		if budget && class.snapshotPlaced && !class.snapshotVetoed {
			class.snapshotVetoed = true
			if licenceCounting {
				krefPlacementCounters.snapshotVetoes.Add(1)
			}
		}
		class.snapshotPlaced = false
	}
}

// krefMemberStore reports whether a store into p is a member store.
//
// Takes a (*RegisterArena) which may be nil.
// Takes p (unsafe.Pointer) which is the destination; nil answers false.
//
// Returns bool which is true when the store needs no barrier.
func krefMemberStore(a *RegisterArena, p unsafe.Pointer) bool {
	return krefMemberStoreOf(a, p, false)
}

// krefMemberStoreOf applies krefMemberStore to a value that references arena data, as
// determined by referencesData.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes pointer (unsafe.Pointer) which is the destination; nil answers false.
// Takes referencesData (bool) which is true for a data-referencing value.
//
// Returns bool which is true when the store needs no barrier.
func krefMemberStoreOf(arena *RegisterArena, pointer unsafe.Pointer, referencesData bool) bool {
	if arena == nil || pointer == nil || len(arena.kref.sorted) == 0 {
		return false
	}
	chunk, found := arena.krefFind(uintptr(pointer))
	if !found || !arena.krefMembersOwned() || !licenceRegisteredPage(arena, uintptr(pointer)) {
		return false
	}
	if referencesData && arena.krefVetoed(chunk) {
		return false
	}
	return arena.licenceCoverEmpty() || !arena.licenceCoveredSlab(chunk.base)
}

// krefMemberSlotStore is krefMemberStore for a store of v.
//
// Takes a (*RegisterArena) which may be nil.
// Takes p (unsafe.Pointer) which is the destination slot.
// Takes v (reflect.Value) which is the value stored.
//
// Returns bool which is true when the store needs no barrier.
func krefMemberSlotStore(a *RegisterArena, p unsafe.Pointer, v reflect.Value) bool {
	if !krefMemberStoreOf(a, p, krefMemberReferencesData(v)) {
		return false
	}
	return !v.IsValid() || v.Kind() != reflect.String || !a.mayHoldForeignString(v.String()) || a.keepsForeignStrings()
}

// krefMemberStringStore is krefMemberStore for a string s stored into p.
//
// Takes a (*RegisterArena) which may be nil.
// Takes p (unsafe.Pointer) which is the destination.
// Takes s (string) which is the string stored.
//
// Returns bool which is true when the store needs no barrier.
func krefMemberStringStore(a *RegisterArena, p unsafe.Pointer, s string) bool {
	return krefMemberStoreOf(a, p, len(s) != 0) && (!a.mayHoldForeignString(s) || a.keepsForeignStrings())
}

// copySourceIsKRefMember reports whether a barrier-free field copy (the fused
// COPY_STRUCT_FIELD_GENERAL) reads from a KRef slot, whose general fields member stores
// with no barrier may have left referencing arena storage that no hand-off covers.
//
// Takes a (*RegisterArena) which may be nil.
// Takes source (unsafe.Pointer) which is the source struct's address.
//
// Returns bool which is true when the copy must take the barrier.
func copySourceIsKRefMember(a *RegisterArena, source unsafe.Pointer) bool {
	return a != nil && len(a.kref.sorted) != 0 && a.krefContains(source)
}

// krefCopyNeedsBarrier reports whether a fused field copy needs its barriered fallback.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes source (unsafe.Pointer) which is the source struct's address.
// Takes field (unsafe.Pointer) which is the source field's address.
// Takes fieldType (reflect.Type) which is the field's type.
//
// Returns bool which is true when the copy must take the barrier.
func krefCopyNeedsBarrier(arena *RegisterArena, source, field unsafe.Pointer, fieldType reflect.Type) bool {
	if arena == nil || len(arena.kref.sorted) == 0 {
		return false
	}
	if copySourceIsKRefMember(arena, source) {
		return true
	}
	var word unsafe.Pointer
	switch fieldType.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.UnsafePointer, reflect.Chan, reflect.Func:
		word = *(*unsafe.Pointer)(field)
	case reflect.Interface:
		word = (*[2]unsafe.Pointer)(field)[1]
	default:
		return !typeIsPointerFree(fieldType)
	}
	return arena.krefContains(word)
}

// krefMemberReferencesData reports whether storing value may make the destination
// reference arena data.
//
// Takes value (reflect.Value) which is the value stored.
//
// Returns bool which is true for a data-referencing value.
func krefMemberReferencesData(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Func, reflect.Chan:
		return false
	case reflect.String:
		return value.Len() != 0
	default:
		return !typeIsPointerFree(value.Type())
	}
}

// krefVetoed reports whether chunk's type is vetoed for data-referencing member stores.
//
// Takes chunk (*krefChunk) which is one of the arena's chunks.
//
// Returns bool which is true when the type's members escaped.
func (*RegisterArena) krefVetoed(chunk *krefChunk) bool {
	return chunk.class != nil && chunk.class.memberVetoed
}

// krefMakeBacking builds make([]T, length, capacity) for a T that holds pointers in a
// KRef run, its header in the slice-header slab.
//
// Takes vm (*VM) which is the allocating VM.
// Takes sliceType (reflect.Type) which is the slice type.
// Takes length (int) which is the length.
// Takes capacity (int) which is the capacity.
//
// Returns reflect.Value which is the slice.
// Returns bool which is false when the backing is not placed in KRef.
func krefMakeBacking(vm *VM, sliceType reflect.Type, length, capacity int) (reflect.Value, bool) {
	if capacity <= 0 || sliceType.Kind() != reflect.Slice || typeIsPointerFree(sliceType.Elem()) || !krefPlacesFor(vm) {
		return reflect.Value{}, false
	}
	if class := vm.Arena.kref.classes[reflectValueABIType(sliceType.Elem())]; class != nil && class.memberVetoed {
		class.used = true
		return reflect.Value{}, false
	}
	data, ok := vm.Arena.krefRun(sliceType.Elem(), capacity)
	if !ok {
		return reflect.Value{}, false
	}
	return arenaWrapTypedSlice(vm.Arena, data, length, capacity, sliceType), true
}

// krefPointee places the pointee of a non-granted new(T) or heap-promoted local in a KRef
// slot when T holds pointers and is not a struct, array or slice, the site is not a
// global cell, and no earlier epoch vetoed it.
//
// Takes vm (*VM) which is the allocating VM.
// Takes function (*program.CompiledFunction) which holds the site.
// Takes programCounter (int) which is the site's program counter.
// Takes valueType (reflect.Type) which is the pointee type.
// Takes heapCell (bool) which is true for a global cell (AllocateIndirectHeapCell).
//
// Returns reflect.Value which is the pointer to the slot.
// Returns bool which is false when the pointee is not placed in KRef.
func krefPointee(vm *VM, function *program.CompiledFunction, programCounter int, valueType reflect.Type, heapCell bool) (reflect.Value, bool) {
	if function == nil || !krefPointeeKind(valueType) || !krefPlacesFor(vm) {
		return reflect.Value{}, false
	}
	if heapCell {
		return reflect.Value{}, false
	}
	arena := vm.Arena
	if len(arena.kref.pointeeSites) >= pointeeMaxSites || function.ArenaPlacementVetoed(programCounter) {
		return reflect.Value{}, false
	}
	slot, ok := arena.krefSlot(valueType)
	if !ok {
		return reflect.Value{}, false
	}
	pointer := slot.Addr()
	if chunk, found := arena.krefFind(uintptr(pointer.UnsafePointer())); found {
		arena.kref.pointeeSites = append(arena.kref.pointeeSites, pointeeSite{function: function, programCounter: programCounter, base: chunk.base})
	}
	if licenceCounting {
		krefPlacementCounters.pointeePlaced.Add(1)
	}
	return pointer, true
}

// krefPointeeKind reports whether a pointee of type valueType can occupy a KRef slot.
//
// Takes valueType (reflect.Type) which is the pointee type.
//
// Returns bool which is true for a reference-holding pointee type.
func krefPointeeKind(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.Struct, reflect.Array, reflect.Slice:
		return false
	default:
		return valueType.Size() != 0 && !typeIsPointerFree(valueType)
	}
}

// settlePointeePlacements counts, at a licensed release, each pointee placement of the
// ending epoch as clean or revoked, vetoes the site of every revoked one, and forgets
// them.
func (a *RegisterArena) settlePointeePlacements() {
	sites := a.kref.pointeeSites
	if len(sites) == 0 {
		return
	}
	epoch := a.licenceState.epochRevoked
	for _, site := range sites {
		if epoch || a.pointeeSiteRevoked(site) {
			vetoPointeeSite(site, epoch)
		}
	}
	clear(a.kref.pointeeSites)
	a.kref.pointeeSites = a.kref.pointeeSites[:0]
}

// pointeeSiteRevoked reports, and counts, whether a pointee placement's chunk was revoked
// in an epoch that was not.
//
// Takes site (pointeeSite) which is the placement.
//
// Returns bool which is true when its chunk was revoked.
func (a *RegisterArena) pointeeSiteRevoked(site pointeeSite) bool {
	_, revoked := a.licenceState.revoked[site.base]
	if licenceCounting {
		if revoked {
			krefPlacementCounters.pointeeRevoked.Add(1)
		} else {
			krefPlacementCounters.pointeeClean.Add(1)
		}
	}
	return revoked
}

// vetoPointeeSite vetoes a placement's site, unless it has no function or is already
// vetoed, and counts the veto.
//
// Takes site (pointeeSite) which is the placement.
// Takes epoch (bool) which is true when the epoch was revoked.
func vetoPointeeSite(site pointeeSite, epoch bool) {
	if site.function == nil || site.function.ArenaPlacementVetoed(site.programCounter) {
		return
	}
	site.function.VetoArenaPlacement(site.programCounter)
	if !licenceCounting {
		return
	}
	krefPlacementCounters.pointeeVetoes.Add(1)
	if epoch {
		krefPlacementCounters.pointeeEpochVetoes.Add(1)
	}
}

// krefMembersOwned reports whether the arena's KRef slots are members of its private set.
//
// KRef placement applies (krefPlaces), and no other goroutine shares the VM.
//
// Returns bool which is true when stores into uncovered KRef slots are own-storage
// stores.
func (a *RegisterArena) krefMembersOwned() bool {
	return a.krefPlaces() && !a.licenceShared(0)
}
