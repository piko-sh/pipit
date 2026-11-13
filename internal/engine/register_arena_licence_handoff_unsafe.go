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
)

// licenceNoteKeyOf returns v's identity, for matching a second note to the first.
//
// Takes v (reflect.Value) which is the noted value.
// Takes boxed (bool) which is the note's boxed argument.
//
// Returns licenceNoteKey which is the identity.
func licenceNoteKeyOf(v reflect.Value, boxed bool) licenceNoteKey {
	raw := (*unsafeReflectValue)(unsafe.Pointer(&v))
	return licenceNoteKey{typ: uintptr(raw.typ), ptr: uintptr(raw.ptr), flag: raw.flag, boxed: boxed}
}

// ownsAnyStorage reports whether pointer addresses an arena backing, slice header, scalar
// or string box, or upvalue cell.
//
// Takes pointer (unsafe.Pointer) which is the address to test.
//
// Returns bool which is true for arena-owned storage.
func (a *RegisterArena) ownsAnyStorage(pointer unsafe.Pointer) bool {
	if pointer == nil {
		return false
	}
	return a.OwnsSliceBacking(pointer) || a.OwnsSliceHeaderPointer(pointer) || a.ownsScalarBox(pointer) ||
		a.OwnsStringBox(pointer) || slabOwnsPointer(a.upvalueCellSlab, uintptr(pointer))
}

// referencesArenaString reports whether text's bytes lie in this arena or in any
// registered arena byte slab.
//
// Takes text (string) which is the candidate.
//
// Returns bool which is true for arena-backed bytes.
func (a *RegisterArena) referencesArenaString(text string) bool {
	if len(text) == 0 {
		return false
	}
	if a.OwnsString(text) {
		return true
	}
	_, _, found := arenaSlabContaining(uintptr(unsafe.Pointer(unsafe.StringData(text))))
	return found
}

// interfaceSharesArenaStorage reports whether packing value into an interface would leave
// the interface's data word pointing into the arena.
//
// Takes value (reflect.Value) which is the candidate.
//
// Returns bool which is true when the interface would share arena storage.
func (a *RegisterArena) interfaceSharesArenaStorage(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Interface:
		return false
	default:
	}
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	return raw.flag&flagIndir != 0 && raw.flag&flagAddr == 0 && (a.ownsAnyStorage(raw.ptr) || a.krefContains(raw.ptr))
}

// referencesArenaStorage reports whether value, or anything value reaches within the
// probe's bounds, lives in arena storage.
//
// Takes value (reflect.Value) which is the value to probe.
// Takes boxed (bool) which is true when value is reached through, or converted to, an
// interface.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns references (bool) which is true when arena storage is reachable, or the probe
// ran out of budget, or would have had to read shared memory, first.
// Returns truncated (bool) which is true when the answer came from such a stop.
func (a *RegisterArena) referencesArenaStorage(value reflect.Value, boxed bool, depth int, budget *int, shared bool) (references bool, truncated bool) {
	if !value.IsValid() {
		return false, false
	}
	if depth <= 0 || *budget <= 0 {
		return true, true
	}
	if shared && (value.CanAddr() || value.Kind() == reflect.Map && !value.IsNil()) {
		return true, true
	}
	*budget--
	if boxed && a.interfaceSharesArenaStorage(value) {
		return true, false
	}
	return a.kindReferencesArena(value, depth, budget, shared)
}

// kindReferencesArena is referencesArenaStorage's probe of value by its kind, once value
// is admitted and counted.
//
// Takes value (reflect.Value) which is the value to probe.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns references (bool) which is true when the probe finds arena storage.
// Returns truncated (bool) which is true when the answer came from a stop.
func (a *RegisterArena) kindReferencesArena(value reflect.Value, depth int, budget *int, shared bool) (references bool, truncated bool) {
	switch value.Kind() {
	case reflect.String:
		return a.referencesArenaString(value.String()), false
	case reflect.Slice:
		if a.holdsArenaPointer(value.UnsafePointer()) {
			return true, false
		}
		return a.elementsReferenceArena(value, depth, budget, shared)
	case reflect.Array:
		return a.elementsReferenceArena(value, depth, budget, shared)
	case reflect.Struct:
		return a.fieldsReferenceArena(value, depth, budget, shared)
	case reflect.Pointer:
		if value.IsNil() {
			return false, false
		}
		if a.holdsArenaPointer(value.UnsafePointer()) {
			return true, false
		}
		return a.referencesArenaStorage(value.Elem(), false, depth-1, budget, shared)
	case reflect.Interface:
		if value.IsNil() {
			return false, false
		}
		return a.referencesArenaStorage(value.Elem(), true, depth-1, budget, shared)
	case reflect.Map:
		return a.mapReferencesArena(value, depth, budget, shared)
	case reflect.UnsafePointer:
		return a.holdsArenaPointer(value.UnsafePointer()), false
	default:
		return false, false
	}
}

// holdsArenaPointer reports whether p points into this arena's storage or its KRef
// chunks.
//
// Takes p (unsafe.Pointer) which is the pointer.
//
// Returns bool which is true when it does.
func (a *RegisterArena) holdsArenaPointer(p unsafe.Pointer) bool {
	return a.ownsAnyStorage(p) || a.krefContains(p)
}

// fieldsReferenceArena probes each field of a struct that may hold pointers.
//
// Takes value (reflect.Value) which is the struct.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns references (bool) which is true when the probe finds arena storage.
// Returns truncated (bool) which is true when the answer came from a stop.
func (a *RegisterArena) fieldsReferenceArena(value reflect.Value, depth int, budget *int, shared bool) (references bool, truncated bool) {
	if typeIsPointerFree(value.Type()) {
		return false, false
	}
	for index := range value.NumField() {
		if references, truncated := a.referencesArenaStorage(structFieldValue(value, index), false, depth-1, budget, shared); references {
			return true, truncated
		}
	}
	return false, false
}

// elementsReferenceArena probes each element of a slice or array whose elements may hold
// pointers.
//
// Takes value (reflect.Value) which is the slice or array.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns references (bool) which is true when the probe finds arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func (a *RegisterArena) elementsReferenceArena(value reflect.Value, depth int, budget *int, shared bool) (references bool, truncated bool) {
	if typeIsPointerFree(value.Type().Elem()) {
		return false, false
	}
	for index := range value.Len() {
		if references, truncated := a.referencesArenaStorage(value.Index(index), false, depth-1, budget, shared); references {
			return true, truncated
		}
	}
	return false, false
}

// mapReferencesArena probes each key and value of a map.
//
// Takes value (reflect.Value) which is the map.
// Takes depth (int) which is the remaining nesting allowance.
// Takes budget (*int) which is the remaining leaf allowance.
// Takes shared (bool) which is true when another goroutine may be writing what value
// reaches.
//
// Returns references (bool) which is true when the probe finds arena storage.
// Returns truncated (bool) which is true when the probe cannot finish within its bounds
// or safely inspect shared storage.
func (a *RegisterArena) mapReferencesArena(value reflect.Value, depth int, budget *int, shared bool) (references bool, truncated bool) {
	if value.IsNil() {
		return false, false
	}
	iterator := value.MapRange()
	for iterator.Next() {
		if references, truncated := a.referencesArenaStorage(iterator.Key(), false, depth-1, budget, shared); references {
			return true, truncated
		}
		if references, truncated := a.referencesArenaStorage(iterator.Value(), false, depth-1, budget, shared); references {
			return true, truncated
		}
	}
	return false, false
}
