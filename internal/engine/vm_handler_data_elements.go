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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// nilDereferenceMessage is the Go runtime's text for a nil pointer dereference.
const nilDereferenceMessage = "runtime error: invalid memory address or nil pointer dereference"

// checkSliceBounds validates that index is within [0, collection.Len()). On failure,
// raises an interpreted-side runtime panic whose message matches Go's runtime ("runtime
// error: index out of range [N] with length M") so interpreted defer/recover() observes
// parity.
//
// Takes vm (*VM) which is used to raise the interpreted panic on failure.
// Takes collection (reflect.Value) which is the slice or array to check.
// Takes index (int) which is the index to validate.
//
// Returns the OpResult to propagate from the caller (opContinue when the index is in
// range; the raise result otherwise) and a boolean flagging in-range / out-of-range so
// callers can branch.
func checkSliceBounds(vm *VM, collection reflect.Value, index int) (OpResult, bool) {
	length := collection.Len()
	if index < 0 || index >= length {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: index out of range [%d] with length %d", index, length)), false
	}
	return opContinue, true
}

// resolveIndexCollection normalises a collection for index operations, auto-dereferencing
// a pointer-to-array so that `(*[N]T)[i]` matches Go's index semantics. Returns the
// original value unchanged for slices, strings, and maps.
//
// Takes vm (*VM) which receives the error on nil-pointer dereference.
// Takes collection (reflect.Value) which is the indexed value.
//
// Returns the normalised collection and true on success, or the original collection and
// false when a nil pointer was encountered.
func resolveIndexCollection(vm *VM, collection reflect.Value) (reflect.Value, OpResult, bool) {
	if !collection.IsValid() {
		return collection, raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage)), false
	}
	if collection.Kind() != reflect.Pointer {
		return collection, opContinue, true
	}
	if collection.IsNil() {
		return collection, raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage)), false
	}
	if elem := collection.Elem(); elem.Kind() == reflect.Array {
		return elem, opContinue, true
	}
	return collection, opContinue, true
}

// handleIndex handles the isa.OpIndex instruction by reading a general element from a
// slice or array at the given integer index.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleIndex(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	elem := collection.Index(index)
	if elem.Kind() == reflect.Interface && !elem.IsNil() {
		elem = elem.Elem()
	}
	registers.General[instruction.A] = elem
	return opContinue
}

// handleIndexSet handles the isa.OpIndexSet instruction by writing a general value to a
// slice or array at the given integer index.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleIndexSet(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	return storeIndexedElement(vm, frame, registers, collection.Index(index), instruction.C, instruction)
}

// storeIndexedElement writes general[valueRegister] into the addressable element value,
// preserving isa.OpIndexSet's coercion order: arena materialisation, the direct any-slot
// fast path, closure-to-func coercion, convertibility widening, and the final
// assignability check before reflect.Set (which keeps GC write barriers).
//
// Takes vm (*VM) which supplies the arena and panic plumbing.
// Takes frame (*CallFrame) which is used for panic diagnostics.
// Takes registers (*Registers) which holds the source value.
// Takes target (reflect.Value) which is the addressable element to assign.
// Takes valueRegister (uint8) which is the general-bank register holding the value.
// Takes instruction (instruction) which is included in panic diagnostics.
//
// Returns OpResult indicating the next execution step.
func storeIndexedElement(vm *VM, frame *CallFrame, registers *Registers, target reflect.Value, valueRegister uint8, instruction isa.Instruction) OpResult {
	value := MaterialiseArenaValue(vm.Arena, coerceValue(vm, registers.General[valueRegister], target.Type()))
	if writeAnyInterfaceSlotFast(target, value) {
		return opContinue
	}
	value = coerceClosureToFunction(vm, value, target.Type())
	if value.Type() != target.Type() && value.Type().ConvertibleTo(target.Type()) {
		if converted, ok := fastIntKindConvert(vm, value, target.Type()); ok {
			value = converted
		} else {
			value = value.Convert(target.Type())
		}
	}
	if value.IsValid() && !value.Type().AssignableTo(target.Type()) {
		vMPanicTypeMismatch("handleIndexSet", target.Type(), value.Type(), instruction, frame, registers)
	}
	target.Set(value)
	return opContinue
}

// writeAnyInterfaceSlotFast attempts the direct-iface fast path for writing a single-word
// value (pointer, map, chan, func, unsafe. Pointer) into a `[]any` (empty-interface)
// slot.
//
// reflect.Value.Convert(any) goes through cvtT2I which heap-allocates a fresh eface;
// reflect.Value.Set on an interface target is the same dance. Writing the (type, data)
// eface pair directly into the slot via runtime.typedmemmove mirrors the pattern in
// handleSetStructFieldGeneralT0.
//
// Allocation-free int-kind cross-conversion (int64 to/from int, uint64 to/from uintptr,
// etc.) is handled separately by fastIntKindConvert in the caller; this helper covers the
// orthogonal eface-target case.
//
// Takes target (reflect.Value) which is the destination interface slot.
// Takes value (reflect.Value) which is the source value to install.
//
// Returns true when the fast path completed the write, false when the caller should fall
// through to the generic Convert/Set path.
func writeAnyInterfaceSlotFast(target, value reflect.Value) bool {
	if !useMapFastLinkname() {
		return false
	}
	if target.Kind() != reflect.Interface || target.Type().NumMethod() != 0 {
		return false
	}
	valueRaw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if valueRaw.typ == nil {
		zeroEface := [2]unsafe.Pointer{}
		targetRaw := (*unsafeReflectValue)(unsafe.Pointer(&target))
		runtimeTypedmemmove(reflectValueABIType(target.Type()), targetRaw.ptr, unsafe.Pointer(&zeroEface[0]))
		return true
	}
	if !program.HeldKindIsDirectPointer(reflect.Kind(valueRaw.flag & flagKindMask)) {
		return false
	}
	var dataPtr unsafe.Pointer
	if valueRaw.flag&flagIndir != 0 {
		dataPtr = *(*unsafe.Pointer)(valueRaw.ptr)
	} else {
		dataPtr = valueRaw.ptr
	}
	eface := [2]unsafe.Pointer{valueRaw.typ, dataPtr}
	targetRaw := (*unsafeReflectValue)(unsafe.Pointer(&target))
	runtimeTypedmemmove(reflectValueABIType(target.Type()), targetRaw.ptr, unsafe.Pointer(&eface[0]))
	return true
}

// convertMapKey converts a map key to the map's key type when it differs, using an
// allocation-free integer reinterpret when possible and falling back to reflect.Convert.
//
// Takes vm (*VM) which owns the per-vm scratch cache.
// Takes key (reflect.Value) which is the key value to convert.
// Takes keyType (reflect.Type) which is the target key type.
//
// Returns reflect.Value holding the key converted to keyType if needed.
func convertMapKey(vm *VM, key reflect.Value, keyType reflect.Type) reflect.Value {
	srcType := key.Type()
	if srcType == keyType {
		return key
	}
	if converted, ok := fastIntKindConvert(vm, key, keyType); ok {
		return converted
	}
	if srcType.ConvertibleTo(keyType) {
		return key.Convert(keyType)
	}
	return key
}

// fastIntKindConvert attempts an allocation-free integer kind reinterpret matching the
// semantics of `reflect.Value.Convert` for integer-to-integer casts.
//
// Uses the per-vm intMapKeyScratch as the result holder, caller-safe because the scratch
// is consumed immediately by the caller (map ops capture the value, slice/array Set ops
// copy out). It covers signed-to-signed, unsigned-to-unsigned and same-width
// cross-signedness reinterprets (two's complement is representation- preserving), and
// both width-narrowing and width-widening because SetInt / SetUint on a narrower scratch
// truncates and on a wider scratch sign- or zero-extends identically to reflect.Convert.
//
// Takes vm (*VM) which owns the per-vm scratch cache.
// Takes value (reflect.Value) which is the source integer value.
// Takes dstType (reflect.Type) which is the target integer type.
//
// Returns (scratch, true) on a hit; (zero, false) otherwise.
func fastIntKindConvert(vm *VM, value reflect.Value, dstType reflect.Type) (reflect.Value, bool) {
	sourceKind := value.Type().Kind()
	destinationKind := dstType.Kind()
	sourceIsSigned := isSignedIntKind(sourceKind)
	sourceIsUnsigned := isUnsignedIntKind(sourceKind)
	destinationIsSigned := isSignedIntKind(destinationKind)
	destinationIsUnsigned := isUnsignedIntKind(destinationKind)
	sourceIsIntegral := sourceIsSigned || sourceIsUnsigned
	destinationIsIntegral := destinationIsSigned || destinationIsUnsigned
	if !sourceIsIntegral || !destinationIsIntegral {
		return reflect.Value{}, false
	}
	scratch := intMapKeyScratch(vm, dstType)
	switch {
	case destinationIsSigned && sourceIsSigned:
		scratch.SetInt(value.Int())
	case destinationIsUnsigned && sourceIsUnsigned:
		scratch.SetUint(value.Uint())
	case destinationIsSigned && sourceIsUnsigned:
		scratch.SetInt(safeconv.Uint64ToInt64Reinterpret(value.Uint()))
	case destinationIsUnsigned && sourceIsSigned:
		scratch.SetUint(safeconv.Int64ToUint64Reinterpret(value.Int()))
	}
	return scratch, true
}

// isSignedIntKind reports whether kind is one of the signed integer reflect.Kind values.
//
// Takes kind (reflect.Kind) which is the kind to test.
//
// Returns true for Int, Int8, Int16, Int32, Int64; false otherwise.
func isSignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

// isUnsignedIntKind reports whether kind is one of the unsigned integer reflect.Kind
// values (excluding UnsafePointer; uintptr is included as it is the integer alias for
// pointer-width unsigned).
//
// Takes kind (reflect.Kind) which is the kind to test.
//
// Returns true for Uint, Uint8, Uint16, Uint32, Uint64, Uintptr; false otherwise.
func isUnsignedIntKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	default:
		return false
	}
}

// handleMapIndex handles the isa.OpMapIndex instruction by reading a value from a map
// using a general register key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndex(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndex", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if m.Kind() != reflect.Map {
		vm.evalError = newInvariantError("map index on non-map value (%s)", m.Kind())
		return opPanicError
	}
	key := convertMapKey(vm, registers.General[instruction.C], m.Type().Key())
	result := m.MapIndex(key)
	if result.IsValid() {
		registers.General[instruction.A] = result
	} else {
		registers.General[instruction.A] = reflect.Zero(m.Type().Elem())
	}
	return opContinue
}

// handleMapIndexOk handles the isa.OpMapIndexOk instruction by reading a map value and
// setting an ok flag indicating whether the key was found.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the ok register extension word.
// Takes registers (*Registers) which holds the map and key values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapIndexOk(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.B]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapIndexOk", registerRoleMap, instruction.B, instruction, frame, registers)
	}
	if m.Kind() != reflect.Map {
		vm.evalError = newInvariantError("map index on non-map value (%s)", m.Kind())
		return opPanicError
	}
	key := convertMapKey(vm, registers.General[instruction.C], m.Type().Key())
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	result := m.MapIndex(key)
	if result.IsValid() {
		registers.General[instruction.A] = result
		registers.Ints[extensionWord.A] = 1
	} else {
		registers.General[instruction.A] = reflect.Zero(m.Type().Elem())
		registers.Ints[extensionWord.A] = 0
	}
	return opContinue
}

// handleMapSet handles the isa.OpMapSet instruction by writing a value to a map at the
// given key with closure and type coercion.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map, key, and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
//
// Fast path: 8-byte int-keyed map with a concrete (non-interface) element type, where the
// value's runtime type already matches elemType. Covers the LRU `cache.lookup[key] =
// node` pattern (map[int64]*lruNode, *lruNode source) but bails for interface{} element
// maps where the slot needs full eface boxing that the generic reflect.Value.SetMapIndex
// handles correctly.
func handleMapSet(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapSet", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if !mapKindError(vm, m) {
		return opPanicError
	}
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	keyType := cacheEntry.keyType
	elemType := cacheEntry.elemType
	if useMapFastLinkname() && cacheEntry.fast64Eligible && elemType.Kind() != reflect.Interface {
		if mapSetFast64Pointer(vm, m, elemType, registers, instruction) {
			return opContinue
		}
	}
	return mapSetReflectSlow(vm, m, keyType, elemType, registers, instruction)
}

// mapSetFast64Pointer is the 8-byte-keyed pointer-value fast path for handleMapSet.
//
// Takes vm (*VM) which provides arena materialise.
// Takes m (reflect.Value) which is the map register.
// Takes elemType (reflect.Type) which is the map's element type.
// Takes registers (*Registers) which holds the key and value.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns true when the fast path completed, false to fall through.
func mapSetFast64Pointer(vm *VM, m reflect.Value, elemType reflect.Type, registers *Registers, instr isa.Instruction) bool {
	key, ok := mapKeyAsInt64(registers.General[instr.B])
	if !ok {
		return false
	}
	return mapSetFast64PointerKeyed(vm, m, elemType, key, registers.General[instr.C])
}

// mapSetFast64PointerKeyed is mapSetFast64Pointer with the key already extracted, shared
// by the generic handler (general-bank keys) and the typed isa.OpMapSetIntGeneral handler
// (int-bank keys).
//
// Takes vm (*VM) which owns the arena for value materialisation.
// Takes m (reflect.Value) which is the destination map.
// Takes elemType (reflect.Type) which is the map's element type.
// Takes key (int64) which is the 8-byte-kinded key value.
// Takes rawValue (reflect.Value) which is the value to store.
//
// Returns true when the fast path stored the value, false to fall back.
func mapSetFast64PointerKeyed(vm *VM, m reflect.Value, elemType reflect.Type, key int64, rawValue reflect.Value) bool {
	mapRaw := (*unsafeReflectValue)(unsafe.Pointer(&m))
	mapPtr := dereferenceIndirectPointer(mapRaw)
	if mapPtr == nil {
		return false
	}
	valueValue := coerceValue(vm, rawValue, elemType)
	if !valueValue.IsValid() || valueValue.Kind() != reflect.Pointer {
		return false
	}
	valueValue = MaterialiseArenaValue(vm.Arena, valueValue)
	valueRaw := (*unsafeReflectValue)(unsafe.Pointer(&valueValue))
	ptrValue := dereferenceIndirectPointer(valueRaw)
	slotPtr := runtimeMapassignFast64(mapRaw.typ, mapPtr, safeconv.Int64ToUint64Reinterpret(key))
	if slotPtr == nil {
		return true
	}
	runtimeTypedmemmove(reflectValueABIType(elemType), slotPtr, unsafe.Pointer(new(ptrValue)))
	return true
}

// mapKeyAsInt64 extracts an int64-wide key value from any integer- kind reflect.Value,
// mirroring Go's implicit width conversion for integer map keys.
//
// Takes keyValue (reflect.Value) which is the source key.
//
// Returns the int64 representation and true on success, or (0, false) when keyValue isn't
// an integer kind.
func mapKeyAsInt64(keyValue reflect.Value) (int64, bool) {
	switch keyValue.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return keyValue.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return safeconv.Uint64ToInt64Reinterpret(keyValue.Uint()), true
	default:
	}
	return 0, false
}

// dereferenceIndirectPointer reads through a punned reflect.Value's flagIndir bit,
// returning either the indirected pointer or the raw pointer slot.
//
// Takes raw (*unsafeReflectValue) which is the punned reflect.Value internals.
//
// Returns the resolved unsafe.Pointer.
func dereferenceIndirectPointer(raw *unsafeReflectValue) unsafe.Pointer {
	if raw.flag&flagIndir != 0 {
		return *(*unsafe.Pointer)(raw.ptr)
	}
	return raw.ptr
}

// mapSetReflectSlow is the reflect-driven map-set fallback used when the fast64 path
// bails (non-int key kind, nil map pointer, non- pointer element value, or
// interface-typed elements). A dedicated function so handleMapSet's bail conditions are
// expressed as early returns rather than branching to a shared tail.
//
// Takes vm (*VM) which is the virtual machine.
// Takes m (reflect.Value) which is the map value.
// Takes keyType (reflect.Type) which is the map key type.
// Takes elemType (reflect.Type) which is the map element type.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns OpResult indicating the next execution step.
func mapSetReflectSlow(vm *VM, m reflect.Value, keyType, elemType reflect.Type, registers *Registers, instruction isa.Instruction) OpResult {
	key := MaterialiseArenaValue(vm.Arena, convertMapKey(vm, registers.General[instruction.B], keyType))
	value := MaterialiseArenaValue(vm.Arena, coerceValue(vm, registers.General[instruction.C], elemType))
	if recovered, panicked := guardChannelOp(func() { m.SetMapIndex(key, value) }); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// handleMapDelete handles the isa.SubOpMapDelete instruction by deleting an entry from a
// map using the given key.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the map and key values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMapDelete(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	m := registers.General[instruction.A]
	if !m.IsValid() {
		vMPanicInvalidRegister("handleMapDelete", registerRoleMap, instruction.A, instruction, frame, registers)
	}
	if m.Kind() != reflect.Map {
		vm.evalError = newInvariantError("map delete on non-map value (%s)", m.Kind())
		return opPanicError
	}
	cacheEntry := resolveMapAccessCache(vm, m.Type())
	if cacheEntry.fast64Eligible {
		if key, ok := mapKeyAsInt64(registers.General[instruction.B]); ok {
			keyScratch := intMapKeyScratch(vm, cacheEntry.keyType)
			keyScratch.SetInt(key)
			m.SetMapIndex(keyScratch, reflect.Value{})
			return opContinue
		}
	}
	key := convertMapKey(vm, registers.General[instruction.B], m.Type().Key())
	m.SetMapIndex(key, reflect.Value{})
	return opContinue
}

// handleGetField handles the isa.OpGetField instruction by reading a struct field into a
// general register, dereferencing pointers as needed.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the struct and destination.
// Takes instruction (instruction) which encodes the register and field index.
//
// Returns OpResult indicating the next execution step.
//
// Unwraps interface values that hold a concrete struct or pointer to struct. This is
// needed when a previous step pulled the value out of a container whose element type was
// cycle-broken to `any` (see convertFieldBreakingCycles): map[K]*Self lookup or [N]*Self
// index returns an interface that the user expects to be a struct or *struct for the next
// field access.
//
//nolint:revive // hot path
func handleGetField(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.General[instruction.B]
	if !s.IsValid() {

		return raiseNilDereference(vm)
	}
	if s.Kind() == reflect.Interface && !s.IsNil() {
		s = s.Elem()
	}
	if s.Kind() == reflect.Pointer {
		if s.IsNil() {
			return raiseNilDereference(vm)
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		vMPanicNotStruct("handleGetField", instruction.B, s.Kind(), instruction, frame, registers)
	}
	if int(instruction.C) >= s.NumField() {
		vMPanicFieldIndex("handleGetField", s.Type(), instruction.C, instruction, frame, registers)
	}
	field := s.Field(int(instruction.C))
	if !field.CanInterface() {
		if !s.CanAddr() {
			addressable := reflect.New(s.Type()).Elem()
			addressable.Set(s)
			s = addressable
			field = s.Field(int(instruction.C))
			registers.General[instruction.B] = s
		}
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	if field.Kind() == reflect.Interface {
		if field.IsNil() {
			field = reflect.Value{}
		} else {
			field = field.Elem()
		}
	} else if field.CanAddr() {
		switch field.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func:
			value := *(*unsafe.Pointer)(ReflectValuePtr(field))
			field = unsafeDirectIfaceKindValue(reflectValueABIType(field.Type()), value, field.Kind())
		case reflect.Slice:
			buffer := vm.acquireSliceSnapshot()
			*buffer = *(*snapshotSliceHeader)(ReflectValuePtr(field))
			field = unsafeReadOnlyValue(reflectValueABIType(field.Type()), unsafe.Pointer(buffer), reflect.Slice)
		default:
		}
	}
	registers.General[instruction.A] = field
	return opContinue
}

// handleSetField handles the isa.OpSetField instruction by writing a value to a struct
// field with closure coercion and type conversion.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the struct and value registers.
// Takes instruction (instruction) which encodes the struct and field index.
//
// Returns OpResult indicating the next execution step.
//
// Note: aborts via vmPanicXxx helpers when the struct register is invalid, the deref
// target is not a pointer or interface, the deref target is nil, or the value type is not
// assignable to the field type.
func handleSetField(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.General[instruction.A]
	if !s.IsValid() {
		return raiseNilDereference(vm)
	}
	if instruction.B == isa.SentinelFieldDeref {
		return handleSetFieldDeref(vm, frame, registers, instruction, s)
	}
	if s.Kind() == reflect.Pointer {
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		vMPanicNotStruct("handleSetField", instruction.A, s.Kind(), instruction, frame, registers)
	}
	if int(instruction.B) >= s.NumField() {
		vMPanicFieldIndex("handleSetField", s.Type(), instruction.B, instruction, frame, registers)
	}
	field := s.Field(int(instruction.B))
	if source := registers.General[instruction.C]; source.IsValid() && field.CanSet() {
		if writeIntegerFieldFast(field, source) {
			return opContinue
		}
	}
	value := coerceValue(vm, registers.General[instruction.C], field.Type())
	if !frame.Function.FieldStoreArenaSafePCs[frame.ProgramCounter-1] {
		value = MaterialiseArenaValue(vm.Arena, value)
	}
	if !field.CanSet() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	if value.IsValid() && field.Type() != value.Type() && !value.Type().AssignableTo(field.Type()) {
		panicSetFieldTypeMismatch(s, field, value, instruction)
	}
	field.Set(value)
	return opContinue
}

// handleSetFieldDeref dispatches the `*p = value` form of isa.OpSetField, materialising
// arena-resident sources so the heap pointee does not end up holding a slab pointer.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
// Takes receiver (reflect.Value) which is the pointer or interface receiver.
//
// Returns opContinue on success.
//
// Panics with an invariant error when receiver is neither a pointer nor an interface, or
// when receiver is nil.
func handleSetFieldDeref(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, receiver reflect.Value) OpResult {
	if receiver.Kind() != reflect.Pointer && receiver.Kind() != reflect.Interface {
		panic(newInvariantError(
			"handleSetField deref - general[%d] is %v, expected pointer or interface; "+
				"pc=%d funcName=%s; registers: a=%d b=%d c=%d\n%s",
			instruction.A, receiver.Kind(),
			frame.ProgramCounter, frame.Function.Name,
			instruction.A, instruction.B, instruction.C,
			vMDiagnosticContext(frame, registers, int(instruction.A)),
		))
	}
	if receiver.IsNil() {
		return raiseNilDereference(vm)
	}
	value := registers.General[instruction.C]
	if receiver.Kind() == reflect.Pointer {
		if vm.Arena != nil && !vm.Arena.ownsIndirectCell(receiver.UnsafePointer()) {
			value = escapeArenaValueForGlobal(vm.Arena, value)
		}
		if writeSliceHeaderThroughPointer(receiver, value) {
			return opContinue
		}
	}
	element := receiver.Elem()
	element.Set(MaterialiseArenaValue(vm.Arena, coerceValue(vm, value, element.Type())))
	return opContinue
}

// writeSliceHeaderThroughPointer attempts to write a slice-header value directly into the
// pointee of receiver. Returns true once the write is applied; false when the receiver
// does not point at a matching slice type.
//
// Takes receiver (reflect.Value) which is the pointer holding the destination slice
// storage.
// Takes value (reflect.Value) which is the candidate slice value.
//
// Returns true once the write completed via typedmemmove, false when the caller should
// fall back to the generic Elem().Set path.
func writeSliceHeaderThroughPointer(receiver, value reflect.Value) bool {
	if !useMapFastLinkname() {
		return false
	}
	elemType := receiver.Type().Elem()
	if elemType.Kind() != reflect.Slice {
		return false
	}
	if !value.IsValid() || value.Kind() != reflect.Slice || value.Type() != elemType {
		return false
	}
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&receiver))
	var elemPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		elemPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		elemPtr = recvShape.ptr
	}
	valueShape := (*unsafeReflectValue)(unsafe.Pointer(&value))
	var srcPtr unsafe.Pointer
	if valueShape.flag&flagIndir != 0 {
		srcPtr = valueShape.ptr
	} else {
		srcPtr = unsafe.Pointer(&valueShape.ptr)
	}
	runtimeTypedmemmove(reflectValueABIType(elemType), elemPtr, srcPtr)
	return true
}

// panicSetFieldTypeMismatch raises the diagnostic panic emitted by handleSetField when a
// value is not assignable to the resolved struct field.
//
// Takes parent (reflect.Value) which is the struct holding the field.
// Takes field (reflect.Value) which is the destination field.
// Takes value (reflect.Value) which is the source value rejected as incompatible.
// Takes instr (instruction) which is forwarded into the diagnostic.
//
// Panics with a formatted message describing the offending types.
func panicSetFieldTypeMismatch(parent, field, value reflect.Value, instr isa.Instruction) {
	fieldName := parent.Type().Field(int(instr.B)).Name
	panic(newInvariantError(
		"handleSetField type mismatch - struct %v field [%d] %q (type %v) cannot accept value of type %v; "+
			"registers: a=%d b=%d c=%d; struct has %d fields",
		parent.Type(), instr.B, fieldName, field.Type(), value.Type(),
		instr.A, instr.B, instr.C, parent.NumField(),
	))
}

// handleGetFieldInt handles the isa.OpGetFieldInt instruction by reading an integer
// struct field directly into an int register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the struct and destination.
// Takes instruction (instruction) which encodes the register and field index.
//
// Returns OpResult indicating the next execution step.
func handleGetFieldInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.General[instruction.B]
	if !s.IsValid() {
		return raiseNilDereference(vm)
	}
	if s.Kind() == reflect.Pointer {
		if s.IsNil() {
			vmPanicNilPointerReceiver()
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		vMPanicNotStruct("handleGetFieldInt", instruction.B, s.Kind(), instruction, frame, registers)
	}
	if int(instruction.C) >= s.NumField() {
		vMPanicFieldIndex("handleGetFieldInt", s.Type(), instruction.C, instruction, frame, registers)
	}
	field := s.Field(int(instruction.C))
	if !field.CanInterface() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	field = unwrapInterfaceElement(field)
	if field.Kind() == reflect.Bool {
		if field.Bool() {
			registers.Ints[instruction.A] = 1
		} else {
			registers.Ints[instruction.A] = 0
		}
	} else {
		registers.Ints[instruction.A] = field.Int()
	}
	return opContinue
}

// handleSetFieldInt handles the isa.OpSetFieldInt instruction by writing an int register
// value directly to an integer struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the struct and source value.
// Takes instruction (instruction) which encodes the struct and field index.
//
// Returns OpResult indicating the next execution step.
func handleSetFieldInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.General[instruction.A]
	if !s.IsValid() {
		return raiseNilDereference(vm)
	}
	if s.Kind() == reflect.Pointer {
		if s.IsNil() {
			vmPanicNilPointerReceiver()
		}
		s = s.Elem()
	}
	if s.Kind() != reflect.Struct {
		vMPanicNotStruct("handleSetFieldInt", instruction.A, s.Kind(), instruction, frame, registers)
	}
	if int(instruction.B) >= s.NumField() {
		vMPanicFieldIndex("handleSetFieldInt", s.Type(), instruction.B, instruction, frame, registers)
	}
	field := s.Field(int(instruction.B))
	if !field.CanSet() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	v := registers.Ints[instruction.C]
	if field.Kind() == reflect.Bool {
		field.SetBool(v != 0)
	} else {
		field.SetInt(v)
	}
	return opContinue
}

// derefSliceHeader resolves the []int64 header a *[]T-kinded general register points at.
//
// Mirrors handleDeref's Pointer arm for the nil check (same panic text) and
// resolvePointerBase for the flagIndir forms; the pointee address is the slice header
// itself. Non-pointer receivers report unsupported so callers raise the same diagnostics
// the composed DEREF path would.
//
// Takes vm (*VM) which receives the nil-dereference panic.
// Takes registers (*Registers) which holds the pointer value.
// Takes pointerRegister (uint8) which is the general-bank register holding the *[]T.
//
// Returns the slice-header pointer, the panic OpResult when raised, and ok.
func derefSliceHeader(vm *VM, registers *Registers, pointerRegister uint8) (*snapshotSliceHeader, OpResult, bool) {
	value := registers.General[pointerRegister]
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if raw.typ == nil || reflect.Kind(raw.flag&flagKindMask) != reflect.Pointer {
		return nil, opContinue, false
	}
	base := raw.ptr
	if raw.flag&flagIndir != 0 {
		base = *(*unsafe.Pointer)(base)
	}
	if base == nil {
		return nil, raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage)), false
	}
	return (*snapshotSliceHeader)(base), opContinue, true
}

// handleDerefSliceGetInt handles isa.OpDerefSliceGetInt: ints[A] = (*general[B])[ints[C]]
// for pointers to int-kinded slices.
//
// Reads the slice header through the pointer on every access, so aliasing and
// append/reslice-through-pointer effects are always visible. Unsupported receiver shapes
// fall back to the composed semantics via reflect.
//
// Takes vm (*VM) which supplies panic plumbing.
// Takes frame (*CallFrame) which is used for fallback diagnostics.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (instruction) which encodes A=dest, B=pointer, C=index.
//
// Returns OpResult indicating the next execution step.
func handleDerefSliceGetInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	header, panicResult, ok := derefSliceHeader(vm, registers, instruction.B)
	if !ok {
		if panicResult != opContinue {
			return panicResult
		}
		return derefSliceFallbackGet(vm, frame, registers, instruction)
	}
	index := registers.Ints[instruction.C]
	//nolint:gosec // unsigned bounds check
	if uint64(index) >= uint64(header.Len) {
		return raiseIndexOutOfRange(vm, int(index), header.Len)
	}
	//nolint:gosec // index checked above
	registers.Ints[instruction.A] = *(*int64)(unsafe.Add(header.Data, uintptr(index)*8))
	return opContinue
}

// derefSliceFallbackGet routes unsupported isa.OpDerefSliceGetInt receivers through
// reflect, matching what the composed DEREF+INDEX pair produced.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns OpResult indicating the next execution step.
func derefSliceFallbackGet(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.B]
	if !value.IsValid() {
		return raiseNilDereference(vm)
	}
	element := value.Elem()
	index := int(registers.Ints[instruction.C])
	if boundsResult, inRange := checkSliceBounds(vm, element, index); !inRange {
		return boundsResult
	}
	registers.Ints[instruction.A] = element.Index(index).Int()
	return opContinue
}

// handleDerefSliceSetInt handles isa.OpDerefSliceSetInt: (*general[A])[ints[B]] = ints[C]
// for pointers to int-kinded slices. Element stores into int slices carry no pointers, so
// no write barrier applies.
//
// Takes vm (*VM) which supplies panic plumbing.
// Takes registers (*Registers) which holds the operands.
// Takes instruction (isa.Instruction) which encodes A=pointer, B=index, C=source.
//
// Returns OpResult indicating the next execution step.
func handleDerefSliceSetInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	header, panicResult, ok := derefSliceHeader(vm, registers, instruction.A)
	if !ok {
		if panicResult != opContinue {
			return panicResult
		}
		value := registers.General[instruction.A]
		if !value.IsValid() {
			return raiseNilDereference(vm)
		}
		element := value.Elem()
		index := int(registers.Ints[instruction.B])
		if boundsResult, inRange := checkSliceBounds(vm, element, index); !inRange {
			return boundsResult
		}
		element.Index(index).SetInt(registers.Ints[instruction.C])
		return opContinue
	}
	index := registers.Ints[instruction.B]
	//nolint:gosec // unsigned bounds check
	if uint64(index) >= uint64(header.Len) {
		return raiseIndexOutOfRange(vm, int(index), header.Len)
	}
	//nolint:gosec // index checked above
	*(*int64)(unsafe.Add(header.Data, uintptr(index)*8)) = registers.Ints[instruction.C]
	return opContinue
}

// raiseNilDereference raises Go's nil-pointer runtime error as an interpreted panic, so a
// deferred recover() in the script observes the same value it would under Go.
//
// Takes vm (*VM) which is the executing VM.
//
// Returns OpResult after attempting unwind to a recover handler.
func raiseNilDereference(vm *VM) OpResult {
	return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage))
}
