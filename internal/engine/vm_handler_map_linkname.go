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

	"pipit.sh/pipit/internal/safeconv"
)

// runtimeMapaccess2Fast64 is a direct linkname to runtime.mapaccess2_fast64, bypassing
// reflect's generic mapaccess path for int64-keyed maps. The signature must match the
// runtime's exactly.
//
// Takes t (unsafe.Pointer) which is the *abi.MapType describing the map.
// Takes m (unsafe.Pointer) which is the *runtime.Map.
// Takes key (uint64) which is the int64 / uint64 key.
//
// Returns the value slot pointer and the present flag.
//
//go:linkname runtimeMapaccess2Fast64 runtime.mapaccess2_fast64
//go:noescape
func runtimeMapaccess2Fast64(t unsafe.Pointer, m unsafe.Pointer, key uint64) (unsafe.Pointer, bool)

// runtimeMapassignFast64 looks up or allocates a slot for a uint64 key.
//
// Caller must initialise the returned slot via runtimeTypedmemmove with the map's value
// type.
//
// Takes t (unsafe.Pointer) which is the *abi.MapType describing the map.
// Takes m (unsafe.Pointer) which is the *runtime.Map.
// Takes key (uint64) which is the int64 / uint64 key.
//
// Returns the pointer to the value slot.
//
//go:linkname runtimeMapassignFast64 runtime.mapassign_fast64
//go:noescape
func runtimeMapassignFast64(t unsafe.Pointer, m unsafe.Pointer, key uint64) unsafe.Pointer

// runtimeTypedmemmove copies a value of type t to destination from source, emitting GC
// write barriers.
//
// Takes t (unsafe.Pointer) which is the *abi.Type describing the value.
// Takes destination (unsafe.Pointer) which is the destination slot.
// Takes source (unsafe.Pointer) which is the source slot.
//
//go:linkname runtimeTypedmemmove runtime.typedmemmove
//go:noescape
func runtimeTypedmemmove(t unsafe.Pointer, destination unsafe.Pointer, source unsafe.Pointer)

// runtimeMapaccess2Faststr looks up a string-keyed map entry.
//
// Same signature shape as runtimeMapaccess2Fast64 but specialised for the
// runtime.aeshashstr / strhash hash path that string keys use, avoiding
// reflect.Value.MapIndex's key-boxing allocation entirely.
//
// Takes t (unsafe.Pointer) which is the *abi.MapType describing the map.
// Takes m (unsafe.Pointer) which is the *runtime.Map.
// Takes key (string) which is the string lookup key.
//
// Returns the value slot pointer and the present flag.
//
//go:linkname runtimeMapaccess2Faststr runtime.mapaccess2_faststr
//go:noescape
func runtimeMapaccess2Faststr(t unsafe.Pointer, m unsafe.Pointer, key string) (unsafe.Pointer, bool)

// runtimeMapassignFaststr looks up or allocates a slot for a string key.
//
// Caller must initialise the returned slot via runtimeTypedmemmove with the map's value
// type.
//
// Takes t (unsafe.Pointer) which is the *abi.MapType describing the map.
// Takes m (unsafe.Pointer) which is the *runtime.Map.
// Takes key (string) which is the string key.
//
// Returns the pointer to the value slot.
//
//go:linkname runtimeMapassignFaststr runtime.mapassign_faststr
//go:noescape
func runtimeMapassignFaststr(t unsafe.Pointer, m unsafe.Pointer, key string) unsafe.Pointer

// mapAccessFast64ToGeneral does a direct runtime.mapaccess2_fast64 against an int64-keyed
// map, resolving the underlying *Map pointer from the reflect.Value.
//
// Takes mapValue (reflect.Value) which must be a Map kind value whose key type is exactly
// int64 / uint64 / int (8-byte int).
// Takes key (int64) which is the lookup key.
//
// Returns the value slot pointer and the present flag, or (nil, false) when the caller
// should fall back to the reflect path.
func mapAccessFast64ToGeneral(mapValue reflect.Value, key int64) (unsafe.Pointer, bool) {
	if !mapValue.IsValid() {
		return nil, false
	}
	rv := (*unsafeReflectValue)(unsafe.Pointer(&mapValue))
	if rv.typ == nil {
		return nil, false
	}
	var mapPtr unsafe.Pointer
	if rv.flag&flagIndir != 0 {
		mapPtr = *(*unsafe.Pointer)(rv.ptr)
	} else {
		mapPtr = rv.ptr
	}
	if mapPtr == nil {
		return nil, false
	}
	resultPtr, ok := runtimeMapaccess2Fast64(rv.typ, mapPtr, safeconv.Int64ToUint64Reinterpret(key))
	return resultPtr, ok
}

// mapAccessFastStrToGeneral reads a string-keyed map via the runtime fast path.
//
// Mirrors mapAccessFast64ToGeneral exactly; only the linkname symbol and the key type
// differ. Used by handleMapGetStringGeneral and handleMapIndexOkStringGeneral. Returns
// (nil, false) when caller should fall back to the reflect path (map is nil, etc.).
//
// Takes mapValue (reflect.Value) which must be a Map kind value with string keys.
// Takes key (string) which is the lookup key.
//
// Returns the value slot pointer and the present flag.
func mapAccessFastStrToGeneral(mapValue reflect.Value, key string) (unsafe.Pointer, bool) {
	if !mapValue.IsValid() {
		return nil, false
	}
	rv := (*unsafeReflectValue)(unsafe.Pointer(&mapValue))
	if rv.typ == nil {
		return nil, false
	}
	var mapPtr unsafe.Pointer
	if rv.flag&flagIndir != 0 {
		mapPtr = *(*unsafe.Pointer)(rv.ptr)
	} else {
		mapPtr = rv.ptr
	}
	if mapPtr == nil {
		return nil, false
	}
	resultPtr, ok := runtimeMapaccess2Faststr(rv.typ, mapPtr, key)
	return resultPtr, ok
}

// mapAssignFastStr returns the value slot pointer for a string key.
//
// Allocates the slot if absent. Caller must initialise the slot via runtimeTypedmemmove
// with the map's value-type abi pointer. Mirrors the read-side helpers; used by
// handleMapSetStringGeneral.
//
// Takes mapValue (reflect.Value) which must be a Map kind value with string keys.
// Takes key (string) which is the key to assign.
//
// Returns the value slot pointer, or nil when the map is invalid or nil.
func mapAssignFastStr(mapValue reflect.Value, key string) unsafe.Pointer {
	if !mapValue.IsValid() {
		return nil
	}
	rv := (*unsafeReflectValue)(unsafe.Pointer(&mapValue))
	if rv.typ == nil {
		return nil
	}
	var mapPtr unsafe.Pointer
	if rv.flag&flagIndir != 0 {
		mapPtr = *(*unsafe.Pointer)(rv.ptr)
	} else {
		mapPtr = rv.ptr
	}
	if mapPtr == nil {
		return nil
	}
	return runtimeMapassignFaststr(rv.typ, mapPtr, key)
}

// useMapFastLinkname reports whether the linkname-backed map fast paths are available.
//
// Returns true on the unsafe build.
func useMapFastLinkname() bool {
	return true
}

// mapSetStringProbeFirst writes an element into a string-keyed map, probing first to
// avoid planting an arena pointer as the stored key. Only a miss materialises the key.
//
// Takes vm (*VM) which owns the arena the key may live in.
// Takes m (reflect.Value) which must be a string-keyed map.
// Takes key (string) which is the raw key from the string bank.
// Takes write (func(unsafe.Pointer)) which stores the element into the slot.
//
// Returns true when the element was written, false when the caller must take the reflect
// fallback.
func mapSetStringProbeFirst(vm *VM, m reflect.Value, key string, write func(slot unsafe.Pointer)) bool {
	if slot, ok := mapAccessFastStrToGeneral(m, key); ok {
		write(slot)
		return true
	}
	slot := mapAssignFastStr(m, materialiseString(vm.Arena, key))
	if slot == nil {
		return false
	}
	write(slot)
	return true
}

// mapHasStringKeyAndWordIntElem reports whether m is a string-keyed map whose element is
// an 8-byte integer (int or int64, named or not), so the element can be written as an
// int64 word through the slot pointer.
//
// Takes mapType (reflect.Type) which is the map's type.
//
// Returns true when the word-int writers apply.
func mapHasStringKeyAndWordIntElem(mapType reflect.Type) bool {
	if mapType.Key().Kind() != reflect.String {
		return false
	}
	elem := mapType.Elem()
	return (elem.Kind() == reflect.Int || elem.Kind() == reflect.Int64) && elem.Size() == uintptr(int64BoxBytes)
}

// mapSetStringIntProbeFirst stores an int64 element under a string key via
// mapSetStringProbeFirst.
//
// Takes vm (*VM) which owns the arena.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the raw key.
// Takes value (int64) which is the element to store.
//
// Returns true when written, false when the reflect fallback must run.
func mapSetStringIntProbeFirst(vm *VM, m reflect.Value, key string, value int64) bool {
	if !mapHasStringKeyAndWordIntElem(m.Type()) {
		return false
	}
	return mapSetStringProbeFirst(vm, m, key, func(slot unsafe.Pointer) {
		*(*int64)(slot) = value
	})
}

// mapAddStringIntProbeFirst adds delta to the int64 element under a string key via
// mapSetStringProbeFirst; a fresh slot from mapassign is zeroed, so the same writer
// serves both hit and miss.
//
// Takes vm (*VM) which owns the arena.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the raw key.
// Takes delta (int64) which is added to the element.
//
// Returns true when written, false when the reflect fallback must run.
func mapAddStringIntProbeFirst(vm *VM, m reflect.Value, key string, delta int64) bool {
	if !mapHasStringKeyAndWordIntElem(m.Type()) {
		return false
	}
	return mapSetStringProbeFirst(vm, m, key, func(slot unsafe.Pointer) {
		*(*int64)(slot) += delta
	})
}

// mapSetStringStringProbeFirst stores a string element under a string key via
// mapSetStringProbeFirst. The element is materialised because the map is heap-owned.
//
// Takes vm (*VM) which owns the arena.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the raw key.
// Takes value (string) which is the raw element.
//
// Returns true when written, false when the reflect fallback must run.
func mapSetStringStringProbeFirst(vm *VM, m reflect.Value, key, value string) bool {
	mapType := m.Type()
	if mapType.Key().Kind() != reflect.String || mapType.Elem().Kind() != reflect.String {
		return false
	}
	materialised := materialiseString(vm.Arena, value)
	return mapSetStringProbeFirst(vm, m, key, func(slot unsafe.Pointer) {
		*(*string)(slot) = materialised
	})
}

// mapSetStringGeneralProbeFirst stores a coerced element under a string key.
//
// Copies via runtime.typedmemmove so GC write barriers are emitted. The element must
// carry the map's exact element type because the raw copy cannot box a concrete value
// into an interface slot.
//
// Takes vm (*VM) which owns the arena.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the raw key.
// Takes value (reflect.Value) which is the element; invalid stores the zero element.
//
// Returns true when written, false when the reflect fallback must run.
func mapSetStringGeneralProbeFirst(vm *VM, m reflect.Value, key string, value reflect.Value) bool {
	mapType := m.Type()
	elemType := mapType.Elem()
	if mapType.Key().Kind() != reflect.String || (value.IsValid() && value.Type() != elemType) {
		return false
	}
	elemABIType := reflectValueABIType(elemType)
	return mapSetStringProbeFirst(vm, m, key, func(slot unsafe.Pointer) {
		writeReflectValueToSlot(elemABIType, slot, value)
	})
}

// writeReflectValueToSlot copies value into slot with runtime.typedmemmove, or zeroes the
// slot when value is invalid.
//
// Takes elemABIType (unsafe.Pointer) which is the *abi.Type of the slot's element.
// Takes slot (unsafe.Pointer) which is the destination element slot.
// Takes value (reflect.Value) which is the element to copy.
func writeReflectValueToSlot(elemABIType, slot unsafe.Pointer, value reflect.Value) {
	if !value.IsValid() {
		var zero [32]byte
		runtimeTypedmemmove(elemABIType, slot, unsafe.Pointer(&zero[0]))
		return
	}
	valueRaw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if valueRaw.flag&flagIndir != 0 {
		runtimeTypedmemmove(elemABIType, slot, valueRaw.ptr)
		return
	}
	runtimeTypedmemmove(elemABIType, slot, unsafe.Pointer(&valueRaw.ptr))
}
