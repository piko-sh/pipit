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

	// mapFastPathMaxElemBytes is Go's maximum inline map-element size (abi.MapMaxElemBytes).
	mapFastPathMaxElemBytes = 128
)

// mapAccessCacheEntry memoises the per-map-type info that typed map handlers would
// otherwise recompute on every invocation.
type mapAccessCacheEntry struct {
	// keyType is the cached m.Type().Key() result.
	keyType reflect.Type

	// elemType is the cached m.Type().Elem() result.
	elemType reflect.Type

	// zeroElement is elemType's zero value, which a lookup of an absent key yields.
	zeroElement reflect.Value

	// keyScratch is the VM's settable scratch value of keyType.
	keyScratch reflect.Value

	// fast64Eligible records whether keyType passes mapKeyIsFast64Eligible so the handler
	// can branch without recomputing the size+kind check.
	fast64Eligible bool

	// elemInline records whether elemType passes mapElemStoredInline, which every faststr or
	// fast64 slot access requires.
	elemInline bool
}

// mapElemStoredInline reports whether a map with element type elemType stores the element
// inline in its slot, the layout the faststr and fast64 fast paths read and write.
//
// Takes elemType (reflect.Type) which is the map's element type.
//
// Returns true when the element is at most mapFastPathMaxElemBytes.
func mapElemStoredInline(elemType reflect.Type) bool {
	return elemType.Size() <= mapFastPathMaxElemBytes
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

// resolveMapAccessCache returns the cached mapAccessCacheEntry for the given map's
// reflect.Type, recomputing on a type-pointer miss.
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
		zeroElement:    symtab.ZeroValueForType(elemType),
		keyScratch:     mapKeyScratchFor(vm, keyType),
		fast64Eligible: mapKeyIsFast64Eligible(keyType),
		elemInline:     mapElemStoredInline(elemType),
	}
	vm.mapAccessCacheLastType = mapType
	vm.mapAccessCacheLastEntry = entry
	return entry
}

// handleGetMapInt64Int64Ok handles isa.OpGetMapInt64Int64Ok, which reads ints[A] =
// map[int]int (or map[int]int64) in general[B] with key ints[C], plus the extension
// word's A field as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64Int64Ok(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64Int64Ok", registerRoleMap, instruction.B, instruction, frame, registers)
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
	keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
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

// handleGetMapStringInt64Ok handles isa.OpGetMapStringInt64Ok, which reads ints[A] =
// map[string]int (or map[string]int64) in general[B] with key strings[C], plus the
// extension word's A field as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringInt64Ok(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringInt64Ok", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleGetMapStringStringOk handles isa.OpGetMapStringStringOk, which reads strings[A] =
// map[string]string in general[B] with key strings[C], plus the extension word's A field
// as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringStringOk(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringStringOk", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleGetMapInt64StringOk handles isa.OpGetMapInt64StringOk, which reads strings[A] =
// map[int]string (or map[int64]string) in general[B] with key ints[C], plus the extension
// word's A field as the ok register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64StringOk(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64StringOk", registerRoleMap, instruction.B, instruction, frame, registers)
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
	keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
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

// mapKeyScratchFor returns reusable addressable storage for a map key type. Callers must
// set its contents before each use.
//
// Takes vm (*VM) which owns the per-VM scratch cache.
// Takes keyType (reflect.Type) which is the map's declared key type.
//
// Returns the reusable scratch reflect.Value.
func mapKeyScratchFor(vm *VM, keyType reflect.Type) reflect.Value {
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

// mapIndexStringKey reads key from a string-kinded-key map through reflect, for maps
// whose element the faststr fast path cannot address. The key goes into the per-VM key
// scratch, so a named string key type is honoured and no key box is allocated per call.
//
// Takes vm (*VM) which owns the key scratch.
// Takes m (reflect.Value) which is the map.
// Takes keyType (reflect.Type) which is the map's key type, of string kind.
// Takes key (string) which is the lookup key.
//
// Returns the element, or the zero reflect.Value when key is absent.
func mapIndexStringKey(vm *VM, m reflect.Value, keyType reflect.Type, key string) reflect.Value {
	keyScratch := mapKeyScratchFor(vm, keyType)
	keyScratch.SetString(key)
	return m.MapIndex(keyScratch)
}

// mapLookupStringKeyGeneral reads key from a string-kinded-key map whose element lives in
// the general bank.
//
// Takes vm (*VM) which owns the access cache and the key scratch.
// Takes m (reflect.Value) which is the map.
// Takes key (string) which is the lookup key.
//
// Returns the element, or the element type's zero value when key is absent.
// Returns true when key is present.
func mapLookupStringKeyGeneral(vm *VM, m reflect.Value, key string) (reflect.Value, bool) {
	entry := resolveMapAccessCache(vm, m.Type())
	if !useMapFastLinkname() || !entry.elemInline {
		if result := mapIndexStringKey(vm, m, entry.keyType, key); result.IsValid() {
			return result, true
		}
		return entry.zeroElement, false
	}
	resultPtr, present := mapAccessFastStrToGeneral(m, key)
	if !present {
		return entry.zeroElement, false
	}
	return wrapMapElemFast(vm, entry.elemType, resultPtr), true
}

// handleGetMapInt64General reads an integer-keyed map value into the general bank.
//
// Takes vm (*VM) which owns the per-VM key scratch cache.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64General(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64General", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.C]
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	keyScratch := cacheEntry.keyScratch
	keyScratch.SetInt(key)
	result := m.MapIndex(keyScratch)
	if result.IsValid() {
		registers.General[instruction.A] = result
	} else {
		registers.General[instruction.A] = cacheEntry.zeroElement
	}
	return opContinue
}

// handleGetMapInt64GeneralOk reads an integer-keyed map value and its presence flag.
//
// Takes vm (*VM) which owns the per-VM key scratch cache.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64GeneralOk(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64GeneralOk", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	key := registers.Ints[instruction.C]
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	elemType := cacheEntry.elemType
	if useMapFastLinkname() && cacheEntry.fast64Eligible && cacheEntry.elemInline {
		resultPtr, present := mapAccessFast64ToGeneral(m, key)
		if !present {
			registers.General[instruction.A] = cacheEntry.zeroElement
			registers.Ints[extensionWord.A] = 0
			return opContinue
		}
		registers.General[instruction.A] = wrapMapElemFast(vm, elemType, resultPtr)
		registers.Ints[extensionWord.A] = 1
		return opContinue
	}
	keyScratch := cacheEntry.keyScratch
	keyScratch.SetInt(key)
	result := m.MapIndex(keyScratch)
	if result.IsValid() {
		registers.General[instruction.A] = result
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.General[instruction.A] = cacheEntry.zeroElement
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

// handleGetMapInt64Int64 handles the isa.OpGetMapInt64Int64 instruction by reading an
// integer value from a map with an integer key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64Int64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64Int64", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.C]

	if concreteMap, ok := reflect.TypeAssert[map[int]int](m); ok {
		registers.Ints[instruction.A] = int64(concreteMap[int(key)])
		return opContinue
	}

	keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Ints[instruction.A] = result.Int()
	} else {
		registers.Ints[instruction.A] = 0
	}
	return opContinue
}

// handleSetMapInt64Int64 handles the isa.OpSetMapInt64Int64 instruction by writing an
// integer value to a map with an integer key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapInt64Int64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapInt64Int64", registerRoleMap, instruction.A, instruction, frame, registers)
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
		keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
		keyReflectValue.SetInt(key)
		valueReflectValue := reflect.New(m.Type().Elem()).Elem()
		valueReflectValue.SetInt(value)
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleGetMapStringInt64 handles isa.OpGetMapStringInt64, which reads ints[A] =
// map[string]int (or map[string]int64) in general[B] with string key strings[C].
// Type-asserts the typed map handle to dispatch directly, bypassing reflect.MapIndex on
// the hot path.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringInt64", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleSetMapStringInt64 handles isa.OpSetMapStringInt64, which writes
// general[A][strings[B]] = ints[C] for map[string]int (or map[string]int64).
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapStringInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapStringInt64", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	value := registers.Ints[instruction.C]
	if recovered, panicked := guardChannelOp(func() {
		if setMapStringInt64ProbeFirst(vm, m, rawKey, value) {
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

// handleGetMapStringString handles isa.OpGetMapStringString, which reads strings[A] =
// map[string]string in general[B] with key strings[C].
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringString", registerRoleMap, instruction.B, instruction, frame, registers)
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

// handleSetMapStringString handles isa.OpSetMapStringString, which writes
// general[A][strings[B]] = strings[C] for map[string]string.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapStringString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapStringString", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	rawValue := registers.Strings[instruction.C]
	if recovered, panicked := guardChannelOp(func() {
		if setMapStringStringProbeFirst(vm, m, rawKey, rawValue) {
			return
		}
		key := materialiseString(vm.Arena, rawKey)
		value := materialiseString(vm.Arena, rawValue)
		noteHandOffString(vm.Arena, handOffUntracedContainer, key)
		noteHandOffString(vm.Arena, handOffUntracedContainer, value)
		keyReflectValue := reflect.ValueOf(key).Convert(m.Type().Key())
		valueReflectValue := reflect.ValueOf(value).Convert(m.Type().Elem())
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleGetMapInt64String handles isa.OpGetMapInt64String, which reads strings[A] =
// map[int]string (or map[int64]string) in general[B] with key ints[C].
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapInt64String(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapInt64String", registerRoleMap, instruction.B, instruction, frame, registers)
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
	keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
	keyReflectValue.SetInt(key)
	result := m.MapIndex(keyReflectValue)
	if result.IsValid() {
		registers.Strings[instruction.A] = result.String()
	} else {
		registers.Strings[instruction.A] = ""
	}
	return opContinue
}

// handleSetMapInt64String handles isa.OpSetMapInt64String, which writes
// general[A][ints[B]] = strings[C] for map[int]string (or map[int64]string).
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapInt64String(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapInt64String", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	key := registers.Ints[instruction.B]
	value := materialiseString(vm.Arena, registers.Strings[instruction.C])
	noteHandOffString(vm.Arena, handOffUntracedContainer, value)
	if recovered, panicked := guardChannelOp(func() {
		if concrete, ok := reflect.TypeAssert[map[int]string](m); ok {
			concrete[int(key)] = value
			return
		}
		if concrete, ok := reflect.TypeAssert[map[int64]string](m); ok {
			concrete[key] = value
			return
		}
		keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
		keyReflectValue.SetInt(key)
		valueReflectValue := reflect.ValueOf(value).Convert(m.Type().Elem())
		m.SetMapIndex(keyReflectValue, valueReflectValue)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleAddToMapInt64Int64 adds to an integer entry in an integer-keyed map.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and delta.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddToMapInt64Int64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleAddToMapInt64Int64", registerRoleMap, instruction.A, instruction, frame, registers)
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
	keyReflectValue := resolveMapAccessCache(vm, m.Type()).keyScratch
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

// handleAddToMapStringInt64 handles isa.OpAddToMapStringInt64.
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
func handleAddToMapStringInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleAddToMapStringInt64", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	delta := registers.Ints[instruction.C]
	if addToMapStringInt64ProbeFirst(vm, m, rawKey, delta) {
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

// handleGetMapStringGeneral handles isa.OpGetMapStringGeneral.
//
// Reads general[A] = map[string]V in general[B] with key strings[C].
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringGeneral", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	registers.General[instruction.A], _ = mapLookupStringKeyGeneral(vm, m, registers.Strings[instruction.C])
	return opContinue
}

// handleGetMapStringGeneralOk handles isa.OpGetMapStringGeneralOk.
//
// Reads general[A] = map[string]V in general[B] with key strings[C], plus an extension
// word whose A field is the int register holding the ok flag.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which holds the map and key.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetMapStringGeneralOk(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleGetMapStringGeneralOk", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	value, ok := mapLookupStringKeyGeneral(vm, m, registers.Strings[instruction.C])
	registers.General[instruction.A] = value
	registers.Ints[extensionWord.A] = boolToInt64(ok)
	return opContinue
}

// handleSetMapInt64General handles isa.OpSetMapInt64General, which writes
// general[A][ints[B]] = general[C] for int-kinded-key maps whose element type lives in
// the general bank.
//
// Interface element types never take the typedmemmove path.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides panic context.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapInt64General(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapInt64General", registerRoleMap, instruction.A, instruction, frame, registers)
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
	keyScratch := cacheEntry.keyScratch
	keyScratch.SetInt(key)
	value := coerceValue(vm, registers.General[instruction.C], elemType)
	if !localContainerStore(vm.Arena, frame, m) {
		value = materialiseArenaValueIntoSlot(vm.Arena, value, elemType)
		noteBoxedHandOff(vm.Arena, handOffUntracedContainer, value, elemType.Kind() == reflect.Interface)
	}
	if cacheEntry.fast64Eligible && setMapInt64BoxedFast(m, elemType, key, value) {
		return opContinue
	}
	if recovered, panicked := guardChannelOp(func() {
		m.SetMapIndex(keyScratch, value)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleSetMapStringGeneral handles isa.OpSetMapStringGeneral, writing
// general[A][strings[B]] = general[C] via the fast-path runtime trampolines.
//
// Takes vm (*VM) which surfaces EvalError on nil-map writes.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetMapStringGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleSetMapStringGeneral", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	rawKey := registers.Strings[instruction.B]
	types := resolveMapAccessCache(vm, m.Type())
	elemType := types.elemType
	value := coerceValue(vm, registers.General[instruction.C], elemType)
	if !localContainerStore(vm.Arena, frame, m) && !mapSliceValueRevokedEpoch(vm.Arena, value, elemType) {
		value = materialiseArenaValueIntoSlot(vm.Arena, value, elemType)
		noteBoxedHandOff(vm.Arena, handOffUntracedContainer, value, elemType.Kind() == reflect.Interface)
	}
	if recovered, panicked := guardChannelOp(func() {
		if setMapStringGeneralProbeFirst(vm, m, types, rawKey, value) {
			return
		}
		keyReflectValue := reflect.ValueOf(materialiseString(vm.Arena, rawKey)).Convert(types.keyType)
		m.SetMapIndex(keyReflectValue, value)
	}); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}
