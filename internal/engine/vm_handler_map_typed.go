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

	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

const (
	// mapKeyFast64SizeBytes is the byte size of an 8-byte key type recognised by
	// runtimeMapaccess2_fast64. Keys that are not exactly this wide cannot use the fast
	// specialisation.
	mapKeyFast64SizeBytes = 8
)

// mapAccessCacheEntry memoises the per-map-type info that typed map handlers would
// otherwise recompute on every invocation.
type mapAccessCacheEntry struct {
	// keyType is the cached m.Type().Key() result.
	keyType reflect.Type

	// elemType is the cached m.Type().Elem() result.
	elemType reflect.Type

	// fast64Eligible records whether keyType passes mapKeyIsFast64Eligible so the handler
	// can branch without recomputing the size+kind check.
	fast64Eligible bool
}

// mapKindError reports whether the supplied value is a map, raising an interpreted
// runtime panic on the VM when it is not. Typed map handlers call this after the IsValid
// check so a non-map register surfaces as an interpreted panic instead of a host crash
// inside reflect.
//
// Takes vm (*VM) which receives the interpreted panic error.
// Takes m (reflect.Value) which is the value expected to be a map.
//
// Returns true when m is a map kind, false otherwise.
func mapKindError(vm *VM, m reflect.Value) bool {
	if m.Kind() == reflect.Map {
		return true
	}
	return mapKindErrorSlow(vm, m)
}

// mapKindErrorSlow records the non-map diagnostic. It is split out and marked noinline so
// the newRuntimePanicError formatting stays off mapKindError's hot path, letting that
// guard collapse to a single Kind comparison that inlines into every typed-map handler.
//
// Takes vm (*VM) which receives the evaluation error.
// Takes m (reflect.Value) which is the non-map value.
//
// Returns false, so callers can return mapKindErrorSlow(...) directly.
//
//go:noinline
func mapKindErrorSlow(vm *VM, m reflect.Value) bool {
	vm.evalError = newInvariantError("map operation on non-map value (%s)", m.Kind())
	return false
}

// resolveMapAccessCache returns the cached (keyType, elemType, fast64Eligible) tuple for
// the given map's reflect.Type, recomputing on a type-pointer miss.
//
// Takes vm (*VM) which owns the single-slot cache.
// Takes mapType (reflect.Type) which is the map's static type.
//
// Returns the cached entry, recomputed on miss.
func resolveMapAccessCache(vm *VM, mapType reflect.Type) mapAccessCacheEntry {
	if vm.mapAccessCacheLastType == mapType {
		return vm.mapAccessCacheLastEntry
	}
	keyType := mapType.Key()
	elemType := mapType.Elem()
	entry := mapAccessCacheEntry{
		keyType:        keyType,
		elemType:       elemType,
		fast64Eligible: mapKeyIsFast64Eligible(keyType),
	}
	vm.mapAccessCacheLastType = mapType
	vm.mapAccessCacheLastEntry = entry
	return entry
}

// handleMapIndexOkIntInt handles isa.OpMapIndexOkIntInt: reads ints[A] = map[int]int (or
// map[int]int64) in general[B] with key ints[C], plus the extension word's A field as the
// ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkIntInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkIntInt", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[int]int](m); ok {
		value, present := concrete[int(key)]
		registers.Ints[instruction.A] = int64(value)
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[int64]int64](m); ok {
		value, present := concrete[key]
		registers.Ints[instruction.A] = value
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Ints[instruction.A] = result.Int()
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.Ints[instruction.A] = 0
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// handleMapIndexOkStringInt handles isa.OpMapIndexOkStringInt: reads ints[A] =
// map[string]int (or map[string]int64) in general[B] with key strings[C], plus the
// extension word's A field as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkStringInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkStringInt", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[string]int](m); ok {
		value, present := concrete[key]
		registers.Ints[instruction.A] = int64(value)
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[string]int64](m); ok {
		value, present := concrete[key]
		registers.Ints[instruction.A] = value
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Ints[instruction.A] = result.Int()
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.Ints[instruction.A] = 0
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// handleMapIndexOkStringString handles isa.OpMapIndexOkStringString: reads strings[A] =
// map[string]string in general[B] with key strings[C], plus the extension word's A field
// as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkStringString", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[string]string](m); ok {
		value, present := concrete[key]
		registers.Strings[instruction.A] = value
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Strings[instruction.A] = result.String()
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.Strings[instruction.A] = ""
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// handleMapIndexOkIntString handles isa.OpMapIndexOkIntString: reads strings[A] =
// map[int]string (or map[int64]string) in general[B] with key ints[C], plus the extension
// word's A field as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkIntString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkIntString", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[int]string](m); ok {
		value, present := concrete[int(key)]
		registers.Strings[instruction.A] = value
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[int64]string](m); ok {
		value, present := concrete[key]
		registers.Strings[instruction.A] = value
		registers.Ints[extensionWord.A] = boolToInt64(present)
		return opContinue
	}
	keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Strings[instruction.A] = result.String()
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.Strings[instruction.A] = ""
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// intMapKeyScratch returns a reusable, addressable reflect.Value for the given
// integer-kind map key type.
//
// The first call per key type pays a reflect.New allocation; subsequent calls return the
// cached scratch. Callers must SetInt before passing to MapIndex - the scratch is shared
// so previous contents are arbitrary.
//
// Takes vm (*VM) which owns the per-VM scratch cache.
// Takes keyType (reflect.Type) which is the map's declared key type.
//
// Returns the reusable scratch reflect.Value.
func intMapKeyScratch(vm *VM, keyType reflect.Type) reflect.Value {
	if cached, ok := vm.mapKeyScratch[keyType]; ok {
		return cached
	}
	scratch := reflect.New(keyType).Elem()
	if vm.mapKeyScratch == nil {
		vm.mapKeyScratch = make(map[reflect.Type]reflect.Value, initialMapKeyScratchCapacity)
	}
	vm.mapKeyScratch[keyType] = scratch
	return scratch
}

// handleMapGetIntGeneral handles isa.OpMapGetIntGeneral.
//
// Reads general[A] = map[int]V (or map[int64]V) in general[B] with key ints[C], where V
// is any value type whose register kind is isa.RegisterGeneral (pointers, interfaces,
// slices, maps of nested types, user structs). Avoids boxing the int key into a fresh
// reflect.Value per call by reusing a per-keyType scratch (vm.mapKeyScratch). The result
// allocation for the value is unavoidable without unsafe map access.
//
// Takes vm (*VM) which owns the per-VM key scratch cache.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetIntGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetIntGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.C]
	keyScratch := intMapKeyScratch(vm, m.Type().Key())
	keyScratch.SetInt(key)
	result := m.MapIndex(keyScratch)
	if result.IsValid() {
		registers.General[instruction.A] = result
	} else {
		registers.General[instruction.A] = symtab.ZeroValueForType(m.Type().Elem())
	}
	return opContinue
}

// handleMapIndexOkIntGeneral handles isa.OpMapIndexOkIntGeneral.
//
// Reads general[A] = map[int]V (or map[int64]V) in general[B] with key ints[C], plus the
// extension word's A field as the int register holding the ok flag. Mirrors
// handleMapGetIntGeneral but sets instruction.a to the zero value of the map's element
// type when the key is absent (matching Go's `v, ok := m[k]` semantics).
//
// Takes vm (*VM) which owns the per-VM key scratch cache.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkIntGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkIntGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	keyType := cacheEntry.keyType
	elemType := cacheEntry.elemType
	if useMapFastLinkname() && cacheEntry.fast64Eligible {
		resultPtr, present := mapAccessFast64ToGeneral(m, key)
		if !present {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
			registers.Ints[extensionWord.A] = 0
			return opContinue
		}
		registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
		registers.Ints[extensionWord.A] = 1
		return opContinue
	}
	keyScratch := intMapKeyScratch(vm, keyType)
	keyScratch.SetInt(key)
	result := m.MapIndex(keyScratch)
	if result.IsValid() {
		registers.General[instruction.A] = result
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// mapKeyIsFast64Eligible reports whether keyType is eligible for the 8-byte
// mapaccess2_fast64 specialisation.
//
// Restricted to plain int/int64/uint/uint64/uintptr - the strictest subset that always
// passes runtimeMapaccess2Fast64's internal type checks regardless of Go runtime version.
//
// Takes keyType (reflect.Type) which is the candidate map key type.
//
// Returns true when keyType qualifies, false otherwise.
func mapKeyIsFast64Eligible(keyType reflect.Type) bool {
	if keyType.Size() != mapKeyFast64SizeBytes {
		return false
	}
	switch keyType.Kind() {
	case reflect.Int, reflect.Int64,
		reflect.Uint, reflect.Uint64,
		reflect.Uintptr:
		return true
	default:
	}
	return false
}

// wrapMapElemFast wraps a fast-path map slot pointer into a reflect.Value of type
// elemType. The result must not alias the map's internal storage, so pointer kinds read
// the word directly, slice and string kinds copy only the header into an arena slot, and
// all others take a heap copy.
//
// Takes vm (*VM) which supplies the register arena for header copy-outs.
// Takes elemType (reflect.Type) which is the map's element type.
// Takes slotPtr (unsafe.Pointer) which is the pointer into the map's internal slot.
//
// Returns a reflect.Value holding an independent copy of the element.
func wrapMapElemFast(vm *VM, elemType reflect.Type, slotPtr unsafe.Pointer) reflect.Value {
	abiType := reflectValueABIType(elemType)
	switch kind := elemType.Kind(); kind {
	case reflect.Pointer:
		return unsafePointerKindValue(abiType, *(*unsafe.Pointer)(slotPtr))
	case reflect.UnsafePointer, reflect.Chan, reflect.Func, reflect.Map:

		return unsafeDirectIfaceKindValue(abiType, *(*unsafe.Pointer)(slotPtr), kind)
	case reflect.Slice:
		buffer := vm.acquireSliceSnapshot()
		*buffer = *(*snapshotSliceHeader)(slotPtr)
		return unsafeReadOnlyValue(abiType, unsafe.Pointer(buffer), reflect.Slice)
	case reflect.String:
		if vm.Arena != nil {
			slot := vm.Arena.allocStringBox(*(*string)(slotPtr))
			return unsafeReadOnlyValue(abiType, unsafe.Pointer(slot), reflect.String)
		}
	default:
	}
	return copyReflectValue(unsafeNewAt(abiType, slotPtr, elemType.Kind()))
}

// handleMapGetIntInt handles the isa.OpMapGetIntInt instruction by reading an integer
// value from a map with an integer key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetIntInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetIntInt", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.C]

	if concreteMap, ok := reflect.TypeAssert[map[int]int](m); ok {
		registers.Ints[instruction.A] = int64(concreteMap[int(key)])
		return opContinue
	}

	keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Ints[instruction.A] = result.Int()
	} else {
		registers.Ints[instruction.A] = 0
	}
	return opContinue
}

// handleMapSetIntInt handles the isa.OpMapSetIntInt instruction by writing an integer
// value to a map with an integer key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetIntInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetIntInt", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.B]
	value := registers.Ints[instruction.C]

	if recovered, panicked := guardChannelOp(func() {
		if concreteMap, ok := reflect.TypeAssert[map[int]int](m); ok {
			concreteMap[int(key)] = int(value)
			return
		}
		keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
		keyReflectValue.SetInt(key)
		valueReflectValue := reflect.New(m.Type().Elem()).Elem()
		valueReflectValue.SetInt(value)
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapGetStringInt handles isa.OpMapGetStringInt: reads ints[A] = map[string]int (or
// map[string]int64) in general[B] with string key strings[C]. Type-asserts the typed map
// handle to dispatch directly, bypassing reflect.MapIndex on the hot path.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetStringInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetStringInt", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Strings[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[string]int](m); ok {
		registers.Ints[instruction.A] = int64(concrete[key])
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[string]int64](m); ok {
		registers.Ints[instruction.A] = concrete[key]
		return opContinue
	}
	keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Ints[instruction.A] = result.Int()
	} else {
		registers.Ints[instruction.A] = 0
	}
	return opContinue
}

// handleMapSetStringInt handles isa.OpMapSetStringInt: writes general[A][strings[B]] =
// ints[C] for map[string]int (or map[string]int64).
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetStringInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetStringInt", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	value := registers.Ints[instruction.C]
	if recovered, panicked := guardChannelOp(func() {
		if mapSetStringIntProbeFirst(vm, m, rawKey, value) {
			return
		}
		key := materialiseString(vm.Arena, rawKey)
		if concrete, ok := reflect.TypeAssert[map[string]int](m); ok {
			concrete[key] = int(value)
			return
		}
		if concrete, ok := reflect.TypeAssert[map[string]int64](m); ok {
			concrete[key] = value
			return
		}
		keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
		valueReflectValue := reflect.New(m.Type().Elem()).Elem()
		valueReflectValue.SetInt(value)
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapGetStringString handles isa.OpMapGetStringString: reads strings[A] =
// map[string]string in general[B] with key strings[C].
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetStringString", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Strings[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[string]string](m); ok {
		registers.Strings[instruction.A] = concrete[key]
		return opContinue
	}
	keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Strings[instruction.A] = result.String()
	} else {
		registers.Strings[instruction.A] = ""
	}
	return opContinue
}

// handleMapSetStringString handles isa.OpMapSetStringString: writes
// general[A][strings[B]] = strings[C] for map[string]string.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetStringString", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	rawValue := registers.Strings[instruction.C]
	if recovered, panicked := guardChannelOp(func() {
		if mapSetStringStringProbeFirst(vm, m, rawKey, rawValue) {
			return
		}
		key := materialiseString(vm.Arena, rawKey)
		value := materialiseString(vm.Arena, rawValue)
		keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
		valueReflectValue := reflect.ValueOf(value).Convert(m.Type().Elem())
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapGetIntString handles isa.OpMapGetIntString: reads strings[A] = map[int]string
// (or map[int64]string) in general[B] with key ints[C].
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetIntString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetIntString", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[int]string](m); ok {
		registers.Strings[instruction.A] = concrete[int(key)]
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[int64]string](m); ok {
		registers.Strings[instruction.A] = concrete[key]
		return opContinue
	}
	keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Strings[instruction.A] = result.String()
	} else {
		registers.Strings[instruction.A] = ""
	}
	return opContinue
}

// handleMapSetIntString handles isa.OpMapSetIntString: writes general[A][ints[B]] =
// strings[C] for map[int]string (or map[int64]string).
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetIntString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetIntString", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.B]
	value := materialiseString(vm.Arena, registers.Strings[instruction.C])
	if recovered, panicked := guardChannelOp(func() {
		if concrete, ok := reflect.TypeAssert[map[int]string](m); ok {
			concrete[int(key)] = value
			return
		}
		if concrete, ok := reflect.TypeAssert[map[int64]string](m); ok {
			concrete[key] = value
			return
		}
		keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
		keyReflectValue.SetInt(key)
		valueReflectValue := reflect.ValueOf(value).Convert(m.Type().Elem())
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapAddIntInt handles isa.OpMapAddIntInt.
//
// Performs general[A][ints[B]] += ints[C] for map[int]int (or map[int64]int64). Absent
// keys are treated as 0. Fuses get+add+set into one dispatch with a single map probe,
// eliminating the redundant hash and bucket walk that the unfused get/add/set sequence
// pays.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and delta.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapAddIntInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapAddIntInt", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.B]
	delta := registers.Ints[instruction.C]
	if concrete, ok := reflect.TypeAssert[map[int]int](m); ok {
		concrete[int(key)] += int(delta)
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[int64]int64](m); ok {
		concrete[key] += delta
		return opContinue
	}
	keyReflectValue := intMapKeyScratch(vm, m.Type().Key())
	keyReflectValue.SetInt(key)
	current := int64(0)
	if existing := m.MapIndex(keyReflectValue); existing.IsValid() {
		current = existing.Int()
	}
	valueReflectValue := reflect.New(m.Type().Elem()).Elem()
	valueReflectValue.SetInt(current + delta)
	m.SetMapIndex(keyReflectValue, valueReflectValue)
	return opContinue
}

// handleMapAddStringInt handles isa.OpMapAddStringInt.
//
// Performs general[A][strings[B]] += ints[C] for map[string]int (or map[string]int64).
// Absent keys are treated as 0. Fuses get+add+set into one dispatch with one hash of the
// key, vs the two probes the unfused sequence performs.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and delta.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapAddStringInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapAddStringInt", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	delta := registers.Ints[instruction.C]
	if mapAddStringIntProbeFirst(vm, m, rawKey, delta) {
		return opContinue
	}
	key := materialiseString(vm.Arena, rawKey)
	if concrete, ok := reflect.TypeAssert[map[string]int](m); ok {
		concrete[key] += int(delta)
		return opContinue
	}
	if concrete, ok := reflect.TypeAssert[map[string]int64](m); ok {
		concrete[key] += delta
		return opContinue
	}
	keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
	current := int64(0)
	if existing := m.MapIndex(keyReflectValue); existing.IsValid() {
		current = existing.Int()
	}
	valueReflectValue := reflect.New(m.Type().Elem()).Elem()
	valueReflectValue.SetInt(current + delta)
	m.SetMapIndex(keyReflectValue, valueReflectValue)
	return opContinue
}

// handleMapGetStringGeneral handles isa.OpMapGetStringGeneral.
//
// Reads general[A] = map[string]V in general[B] with key strings[C]. Mirrors
// handleMapGetIntGeneral for string keys: routes through runtime.mapaccess2_faststr (via
// mapAccessFastStrToGeneral) to skip the per-call key-boxing allocation that
// reflect.Value.MapIndex performs.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapGetStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapGetStringGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Strings[instruction.C]
	elemType := m.Type().Elem()
	if !useMapFastLinkname() {
		result := m.MapIndex(reflect.ValueOf(key))
		if !result.IsValid() {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		} else {
			registers.General[instruction.A] = result
		}
		return opContinue
	}
	resultPtr, present := mapAccessFastStrToGeneral(m, key)
	if !present {
		registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		return opContinue
	}
	registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
	return opContinue
}

// handleMapIndexOkStringGeneral handles isa.OpMapIndexOkStringGeneral.
//
// Reads general[A] = map[string]V in general[B] with key strings[C], plus an extension
// word whose A field is the int register holding the ok flag. Mirrors
// handleMapIndexOkIntGeneral for string keys.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOkStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOkStringGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Strings[instruction.C]
	elemType := m.Type().Elem()
	if !useMapFastLinkname() {
		result := m.MapIndex(reflect.ValueOf(key))
		if !result.IsValid() {
			registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
			registers.Ints[extensionWord.A] = 0
		} else {
			registers.General[instruction.A] = result
			registers.Ints[extensionWord.A] = 1
		}
		return opContinue
	}
	resultPtr, present := mapAccessFastStrToGeneral(m, key)
	if !present {
		registers.General[instruction.A] = symtab.ZeroValueForType(elemType)
		registers.Ints[extensionWord.A] = 0
		return opContinue
	}
	registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
	registers.Ints[extensionWord.A] = 1
	return opContinue
}

// handleMapSetIntGeneral handles isa.OpMapSetIntGeneral: writes general[A][ints[B]] =
// general[C] for int-kinded-key maps whose element type lives in the general bank.
//
// Pointer-kind elements with 8-byte int-kinded keys route through the
// runtime.mapassign_fast64 fast path shared with the generic handler; everything else
// takes the reflect fallback. Interface element types never take the typedmemmove path.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides panic context.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetIntGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetIntGeneral", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	elemType := cacheEntry.elemType
	key := registers.Ints[instruction.B]
	if useMapFastLinkname() && cacheEntry.fast64Eligible && elemType.Kind() != reflect.Interface {
		if mapSetFast64PointerKeyed(vm, m, elemType, key, registers.General[instruction.C]) {
			return opContinue
		}
	}
	keyScratch := intMapKeyScratch(vm, cacheEntry.keyType)
	keyScratch.SetInt(key)
	value := MaterialiseArenaValue(vm.Arena, coerceValue(vm, registers.General[instruction.C], elemType))
	if recovered, panicked := guardChannelOp(func() {
		m.SetMapIndex(keyScratch, value)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapSetStringGeneral handles isa.OpMapSetStringGeneral, writing
// general[A][strings[B]] = general[C] via the fast-path runtime trampolines.
//
// Takes vm (*VM) which surfaces EvalError on nil-map writes.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapSetStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSetStringGeneral", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	value := MaterialiseArenaValue(vm.Arena, coerceValue(vm, registers.General[instruction.C], m.Type().Elem()))
	if recovered, panicked := guardChannelOp(func() {
		if mapSetStringGeneralProbeFirst(vm, m, rawKey, value) {
			return
		}
		keyReflectValue := reflect.ValueOf(materialiseString(vm.Arena, rawKey)).Convert(m.Type().Key())
		m.SetMapIndex(keyReflectValue, value)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}
