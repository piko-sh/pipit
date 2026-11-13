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
	"unsafe"
)

const (
	// structAppendMinimumCapacity is the smallest backing an arena-grown struct slice is
	// given, so the first few appends do not each trigger a fresh backing.
	structAppendMinimumCapacity = 4
)

// growStructBackingInArena allocates a doubled arena backing for a full struct slice and
// copies the existing elements into it.
//
// Backing ownership is sticky, as for the typed appends: an empty or arena-backed slice
// grows in the arena, a heap-backed one is left to Go's append so it stays on the heap. A
// capacity that would breach the single-allocation limit is also left to the fallback.
//
// Takes vm (*VM) which provides the arena and allocation limits.
// Takes header (*arenaSliceHeader) which is the full source header.
// Takes size (uintptr) which is the element byte size.
// Takes align (uintptr) which is the element alignment.
//
// Returns the new backing, its capacity, and true; or nil, zero and false when the slice
// must grow through the fallback path.
func growStructBackingInArena(vm *VM, header *arenaSliceHeader, size, align uintptr) (unsafe.Pointer, int, bool) {
	if header.Cap > 0 && !vm.Arena.OwnsSliceBacking(header.Data) {
		return nil, 0, false
	}
	newCap := max(header.Cap*2, structAppendMinimumCapacity)
	if vm.Limits.MaxAllocSize > 0 && newCap > vm.Limits.MaxAllocSize {
		return nil, 0, false
	}
	backing := vm.Arena.AllocBytes(size*uintptr(newCap), align)
	if header.Len > 0 {
		copy(unsafe.Slice((*byte)(backing), size*uintptr(header.Len)), unsafe.Slice((*byte)(header.Data), size*uintptr(header.Len)))
	}
	return backing, newCap, true
}
