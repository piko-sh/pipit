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
	"unsafe"
)

// OwnsSliceBacking reports whether p falls inside any arena backing slab.
//
// Covers the string byte slab, the generic byte slab and every typed element slab,
// current or retired. A slice whose backing is arena-owned dies with the arena and must
// be copied to the heap before a heap container may keep it.
//
// Takes p (unsafe.Pointer) which is the slice's data pointer to test.
//
// Returns true when p is in any arena backing slab.
func (a *RegisterArena) OwnsSliceBacking(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	return slabsOwnPointer(a.byteSlab, a.oldByteSlabs, pointer) ||
		slabsOwnPointer(a.genericBytesSlab, a.oldGenericByteSlabs, pointer) ||
		slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, pointer) ||
		slabsOwnPointer(a.intBackingSlab, a.oldIntBackings, pointer) ||
		slabsOwnPointer(a.floatBackingSlab, a.oldFloatBackings, pointer) ||
		slabsOwnPointer(a.boolBackingSlab, a.oldBoolBackings, pointer) ||
		slabsOwnPointer(a.uintBackingSlab, a.oldUintBackings, pointer)
}

// ownsStringBacking reports whether p falls inside an arena string-backing slab.
//
// Takes p (unsafe.Pointer) which is the slice's data pointer to test.
//
// Returns true when p is in the arena's string-backing slabs.
func (a *RegisterArena) ownsStringBacking(p unsafe.Pointer) bool {
	if p == nil {
		return false
	}
	return slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, uintptr(p))
}

// ownsSliceBackingOf is the element-kind-aware form of OwnsSliceBacking().
//
// Only []string can live in the string-backing slab, a scalar element type can only live
// in its own typed slab or the generic byte slab, and a slice of reference kinds can
// never be arena-backed, so most stores answer after one or two probes.
//
// Takes v (reflect.Value) which is the slice whose backing is tested.
//
// Returns true when v's backing lies in an arena slab that could hold its element type.
func (a *RegisterArena) ownsSliceBackingOf(v reflect.Value) bool {
	p := v.UnsafePointer()
	if p == nil {
		return false
	}
	pointer := uintptr(p)
	switch v.Type().Elem().Kind() {
	case reflect.String:
		return slabsOwnPointer(a.stringBackingSlab, a.oldStringBackings, pointer)
	case reflect.Uint8:
		return slabsOwnPointer(a.byteSlab, a.oldByteSlabs, pointer) || a.ownsBytePointer(p)
	case reflect.Int, reflect.Int64:
		return slabsOwnPointer(a.intBackingSlab, a.oldIntBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Float64:
		return slabsOwnPointer(a.floatBackingSlab, a.oldFloatBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Bool:
		return slabsOwnPointer(a.boolBackingSlab, a.oldBoolBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return slabsOwnPointer(a.uintBackingSlab, a.oldUintBackings, pointer) || a.ownsBytePointer(p)
	case reflect.Pointer, reflect.Map, reflect.Interface, reflect.Slice, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return false
	default:
		return a.ownsBytePointer(p)
	}
}

// slabOwnsPointer reports whether pointer falls inside the element storage of slab.
//
// Takes slab ([]T) which is the slab to test against.
// Takes pointer (uintptr) which is the address to test.
//
// Returns true when pointer lies within slab's elements.
func slabOwnsPointer[T any](slab []T, pointer uintptr) bool {
	if len(slab) == 0 {
		return false
	}
	var zero T

	base := uintptr(unsafe.Pointer(&slab[0]))
	return pointer >= base && pointer < base+uintptr(len(slab))*unsafe.Sizeof(zero)
}

// slabsOwnPointer reports whether pointer falls inside the current slab or any retired
// slab kept alive after a grow.
//
// Takes current ([]T) which is the live slab.
// Takes retired ([][]T) which are the slabs replaced by growth since the last reset.
// Takes pointer (uintptr) which is the address to test.
//
// Returns true when any of the slabs owns pointer.
func slabsOwnPointer[T any](current []T, retired [][]T, pointer uintptr) bool {
	if slabOwnsPointer(current, pointer) {
		return true
	}
	for _, slab := range retired {
		if slabOwnsPointer(slab, pointer) {
			return true
		}
	}
	return false
}
