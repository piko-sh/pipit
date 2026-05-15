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
	"pipit.sh/pipit/internal/isa"
)

// structFieldReflectFallback abstracts over structFieldReflectRead /
// structFieldReflectWrite so the wide-encoded resolver helpers can share their skeleton.
type structFieldReflectFallback func(registers *Registers, frame *CallFrame, generalRegister uint8, layout program.StructFieldLayout) (reflect.Value, bool)

// structFieldUnsafeBase resolves a general-bank reflect.Value to the underlying
// unsafe.Pointer of the struct it represents (auto-deref for pointer-to-struct, returns
// (nil, false) for non-addressable receivers so the caller falls back to the
// reflect-based walk).
//
// Takes registers (*Registers) which provides the general bank.
// Takes generalRegister (uint8) which is the general register index holding the struct
// reflect.Value.
//
// Returns the base unsafe.Pointer of the underlying struct, plus true on success.
// Returns (nil, false) when the value is invalid, a nil pointer, or otherwise unreachable
// as a stable base pointer.
//
// Bypasses reflect.Value method calls for the dominant Pointer case by reading the
// value's internal {typ,ptr,flag} layout directly. The inline form is a kind extract +
// nil check + pointer fetch, with no function call boundary.
func structFieldUnsafeBase(registers *Registers, generalRegister uint8) (unsafe.Pointer, bool) {
	value := registers.General[generalRegister]
	raw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if raw.typ == nil {
		return nil, false
	}
	switch reflect.Kind(raw.flag & flagKindMask) {
	case reflect.Pointer:
		return resolvePointerBase(raw)
	case reflect.Interface:
		return resolveInterfaceBase(value)
	case reflect.Struct:
		return resolveStructBase(raw)
	default:
	}
	return nil, false
}

// resolvePointerBase extracts the underlying unsafe.Pointer when the reflect.Value holds
// a Pointer kind.
//
// When flagIndir is set, raw.ptr addresses a *T storage cell that must be dereferenced.
// When unset, raw.ptr IS the pointer value already (snapshotted from
// snapshotPointerLeaf).
//
// Takes raw (*unsafeReflectValue) which is the punned reflect.Value internals.
//
// Returns the dereferenced pointer plus true on success; (nil, false) when the pointer is
// nil.
func resolvePointerBase(raw *unsafeReflectValue) (unsafe.Pointer, bool) {
	if raw.flag&flagIndir != 0 {
		inner := *(*unsafe.Pointer)(raw.ptr)
		if inner == nil {
			return nil, false
		}
		return inner, true
	}
	if raw.ptr == nil {
		return nil, false
	}
	return raw.ptr, true
}

// resolveInterfaceBase extracts the underlying unsafe.Pointer when the reflect.Value
// holds an Interface kind.
//
// For an addressable Value (flagAddr | flagIndir set) the internal .ptr field IS the
// storage address - same as what inner.Addr(). UnsafePointer() returns, but without the
// ptrTo() / sync.Map.Load round-trip Addr() does to build the *T wrapper Value.
//
// Takes value (reflect.Value) which is the interface-holding value.
//
// Returns the dereferenced pointer plus true on success; (nil, false) when the value is a
// nil pointer or otherwise unreachable.
func resolveInterfaceBase(value reflect.Value) (unsafe.Pointer, bool) {
	inner := value.Elem()
	if inner.Kind() == reflect.Pointer {
		if inner.IsNil() {
			return nil, false
		}
		return inner.UnsafePointer(), true
	}
	if inner.CanAddr() {
		return ReflectValuePtr(inner), true
	}
	return nil, false
}

// resolveStructBase extracts the unsafe.Pointer when the reflect.Value holds an
// addressable Struct kind. flagAddr set means raw.ptr is the storage.
//
// Takes raw (*unsafeReflectValue) which is the punned reflect.Value internals.
//
// Returns the storage pointer plus true when addressable; (nil, false) otherwise.
func resolveStructBase(raw *unsafeReflectValue) (unsafe.Pointer, bool) {
	if raw.flag&flagAddr != 0 {
		return raw.ptr, true
	}
	return nil, false
}

// handleGetStructFieldUnsafeInt reads an int-kind struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next dispatch step.
func handleGetStructFieldUnsafeInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField, layoutKind := resolveReadFieldPointerWideKind(frame, registers, instruction.C)
	if !ok {
		if fallbackField.IsValid() {
			registers.Ints[instruction.B] = fallbackField.Int()
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Ints[instruction.B] = loadIntKindFromUnsafe(fieldPointer, layoutKind)
	return opContinue
}

// handleGetStructFieldUnsafeUint reads a uint-kind struct field.
//
// Stores the uint64 value into uints[B]. Uses unsafe.Pointer arithmetic via the
// pre-resolved structLayoutTable entry; falls back to a reflect walk on non-addressable
// receivers.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldUnsafeUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField, layoutKind := resolveReadFieldPointerWideKind(frame, registers, instruction.C)
	if !ok {
		if fallbackField.IsValid() {
			registers.Uints[instruction.B] = fallbackField.Uint()
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Uints[instruction.B] = readUintAt(fieldPointer, layoutKind)
	return opContinue
}

// handleGetStructFieldUnsafeBool reads a bool-kind struct field.
//
// Stores the bool value into bools[B]. Uses unsafe.Pointer arithmetic via the
// pre-resolved structLayoutTable entry; falls back to a reflect walk on non-addressable
// receivers.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldUnsafeBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		if fallbackField.IsValid() {
			registers.Bools[instruction.B] = fallbackField.Bool()
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Bools[instruction.B] = *(*bool)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeString reads a string-kind struct field.
//
// Stores the 16-byte string header into strings[B]. Reads are barrier-free. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry; falls back to a
// reflect walk on non-addressable receivers.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldUnsafeString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		if fallbackField.IsValid() {
			registers.Strings[instruction.B] = fallbackField.String()
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Strings[instruction.B] = *(*string)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceInt reads a []int64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.SlicesInt[instruction.B])
	}
	registers.SlicesInt[instruction.B] = *(*[]int64)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceFloat is the float64 counterpart of
// handleGetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.slicesFloat[instruction.B])
	}
	registers.slicesFloat[instruction.B] = *(*[]float64)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceUint is the uint64 counterpart of
// handleGetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.slicesUint[instruction.B])
	}
	registers.slicesUint[instruction.B] = *(*[]uint64)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceString is the string counterpart of
// handleGetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.slicesString[instruction.B])
	}
	registers.slicesString[instruction.B] = *(*[]string)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceBool is the bool counterpart of
// handleGetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.slicesBool[instruction.B])
	}
	registers.slicesBool[instruction.B] = *(*[]bool)(fieldPointer)
	return opContinue
}

// handleGetStructFieldUnsafeSliceByte is the byte (uint8) counterpart of
// handleGetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField := resolveReadFieldPointerWide(frame, registers, instruction.C)
	if !ok {
		return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, fallbackField, &registers.slicesByte[instruction.B])
	}
	registers.slicesByte[instruction.B] = *(*[]byte)(fieldPointer)
	return opContinue
}

// handleSetStructFieldUnsafeInt writes an int-kind struct field.
//
// Operand layout: B=destination general register holding the struct, C=source int
// register. Uses unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry;
// falls back to a reflect write on non-addressable receivers (with rehome - writes to
// non-addressable values are otherwise a Go compile error).
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldUnsafeInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField, layoutKind := resolveWriteFieldPointerWideKind(frame, registers, instruction.B)
	if !ok {
		if fallbackField.IsValid() {
			fallbackField.SetInt(registers.Ints[instruction.C])
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	storeIntKindAtUnsafe(fieldPointer, layoutKind, registers.Ints[instruction.C])
	return opContinue
}

// handleSetStructFieldUnsafeUint writes a uint-kind struct field.
//
// Uses unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry; falls back
// to a reflect write on non-addressable receivers.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldUnsafeUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldPointer, ok, fallbackField, layoutKind := resolveWriteFieldPointerWideKind(frame, registers, instruction.B)
	if !ok {
		if fallbackField.IsValid() {
			fallbackField.SetUint(registers.Uints[instruction.C])
			return opContinue
		}
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	storeUintKindAtUnsafe(fieldPointer, layoutKind, registers.Uints[instruction.C])
	return opContinue
}

// setStructFieldUnsafeSliceImpl is the shared body for typed-slice struct-field SET
// handlers.
//
// The hot path writes the slice header directly into the receiver via
// runtime.typedmemmove (GC write barrier preserved). The cold fallback walks the field
// via reflect.Value.Set when structFieldUnsafeBase declines (non-addressable receiver,
// dereferenced interface, etc.). Encoding (handlers): B=receiver general register,
// C=source typed-slice register, EXT word=uint16 layout index. The compile-side picker
// pickSetStructFieldSliceSubOp gates emission on canonical 64-bit element widths plus
// string/bool/byte.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which provides the receiver bank.
// Takes instruction (instruction) which encodes the operand indices.
// Takes source (T) which is the typed-slice header to write - passed by value so the
// helper can take its address for typedmemmove.
//
// Returns opContinue, or the raised interpreted panic when the receiver is a nil pointer.
func setStructFieldUnsafeSliceImpl[T any](vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, source T) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		field.Set(reflect.ValueOf(source))
		return opContinue
	}
	if int(layout.FieldTypeIndex) >= len(frame.Function.TypeTable) {
		return opContinue
	}
	fieldType := frame.Function.TypeTable[layout.FieldTypeIndex]
	if fieldType == nil {
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	runtimeTypedmemmove(reflectValueABIType(fieldType), fieldPointer, unsafe.Pointer(&source))
	return opContinue
}

// handleSetStructFieldUnsafeSliceInt writes a []int64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.SlicesInt[instruction.C])
}

// handleSetStructFieldUnsafeSliceFloat is the float64 counterpart of
// handleSetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.slicesFloat[instruction.C])
}

// handleSetStructFieldUnsafeSliceUint is the uint64 counterpart of
// handleSetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.slicesUint[instruction.C])
}

// handleSetStructFieldUnsafeSliceString is the []string counterpart of
// handleSetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.slicesString[instruction.C])
}

// handleSetStructFieldUnsafeSliceBool is the []bool counterpart of
// handleSetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.slicesBool[instruction.C])
}

// handleSetStructFieldUnsafeSliceByte is the []byte counterpart of
// handleSetStructFieldUnsafeSliceInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return setStructFieldUnsafeSliceImpl(vm, frame, registers, instruction, registers.slicesByte[instruction.C])
}

// resolveReadFieldPointerWide resolves the wide-encoded structLayoutTable index into a
// field unsafe.Pointer when the receiver is addressable, or surfaces a reflect.Value
// walker for the fallback path.
//
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the receiver.
// Takes generalRegister (uint8) which is the general-bank index of the receiver.
//
// Returns the field unsafe.Pointer plus true when the unsafe path is available, an
// invalid reflect.Value when the receiver is unreachable, or a valid reflect.Value for
// the fallback to consume otherwise.
func resolveReadFieldPointerWide(frame *CallFrame, registers *Registers, generalRegister uint8) (unsafe.Pointer, bool, reflect.Value) {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, generalRegister)
	if ok {
		return unsafe.Add(base, uintptr(layout.Offset)), true, reflect.Value{}
	}
	field, walkOK := structFieldReflectRead(registers, frame, generalRegister, layout)
	if !walkOK {
		return nil, false, reflect.Value{}
	}
	return nil, false, field
}

// resolveTypedFieldPointer is the shared body of resolveReadFieldPointerWideKind and
// resolveWriteFieldPointerWideKind.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes generalRegister (uint8) which is the general-bank index of the receiver.
// Takes fallback (structFieldReflectFallback) which selects the read or write reflect
// walker.
//
// Returns the field unsafe.Pointer plus true when the unsafe path is available, a valid
// reflect.Value for the fallback otherwise, and the layout's reflect.Kind.
func resolveTypedFieldPointer(frame *CallFrame, registers *Registers, generalRegister uint8, fallback structFieldReflectFallback) (unsafe.Pointer, bool, reflect.Value, reflect.Kind) {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	kind := reflect.Kind(layout.Kind)
	base, ok := structFieldUnsafeBase(registers, generalRegister)
	if ok {
		return unsafe.Add(base, uintptr(layout.Offset)), true, reflect.Value{}, kind
	}
	field, walkOK := fallback(registers, frame, generalRegister, layout)
	if !walkOK {
		return nil, false, reflect.Value{}, kind
	}
	return nil, false, field, kind
}

// resolveReadFieldPointerWideKind is the read-path variant of resolveTypedFieldPointer.
//
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the receiver.
// Takes generalRegister (uint8) which is the general-bank index of the receiver.
//
// Returns the field unsafe.Pointer plus true when the unsafe path is available, a valid
// reflect.Value for the fallback otherwise, and the layout's reflect.Kind.
func resolveReadFieldPointerWideKind(frame *CallFrame, registers *Registers, generalRegister uint8) (unsafe.Pointer, bool, reflect.Value, reflect.Kind) {
	return resolveTypedFieldPointer(frame, registers, generalRegister, structFieldReflectRead)
}

// resolveWriteFieldPointerWideKind is the write-path variant of resolveTypedFieldPointer.
//
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the receiver.
// Takes generalRegister (uint8) which is the general-bank index of the receiver.
//
// Returns the field unsafe.Pointer plus true when the unsafe path is available, a valid
// addressable reflect.Value for the fallback otherwise, and the layout's reflect.Kind.
func resolveWriteFieldPointerWideKind(frame *CallFrame, registers *Registers, generalRegister uint8) (unsafe.Pointer, bool, reflect.Value, reflect.Kind) {
	return resolveTypedFieldPointer(frame, registers, generalRegister, structFieldReflectWrite)
}

// loadIntKindFromUnsafe widens a typed signed-integer value at fieldPointer to int64
// according to the resolved kind.
//
// Takes fieldPointer (unsafe.Pointer) which is the address of the field.
// Takes kind (reflect.Kind) which identifies the signed-integer width.
//
// Returns the widened int64 value; zero when the kind is unknown.
func loadIntKindFromUnsafe(fieldPointer unsafe.Pointer, kind reflect.Kind) int64 {
	switch kind {
	case reflect.Int:
		return int64(*(*int)(fieldPointer))
	case reflect.Int8:
		return int64(*(*int8)(fieldPointer))
	case reflect.Int16:
		return int64(*(*int16)(fieldPointer))
	case reflect.Int32:
		return int64(*(*int32)(fieldPointer))
	case reflect.Int64:
		return *(*int64)(fieldPointer)
	default:
	}
	return 0
}

// storeIntKindAtUnsafe writes a narrowed signed-integer value at fieldPointer according
// to the resolved kind. The narrowing follows Go's standard modular semantics for typed
// conversions, mirroring what the source program would observe if compiled directly.
//
// Takes fieldPointer (unsafe.Pointer) which is the address of the field.
// Takes kind (reflect.Kind) which identifies the signed-integer width.
// Takes source (int64) which is the value to narrow and store.
func storeIntKindAtUnsafe(fieldPointer unsafe.Pointer, kind reflect.Kind, source int64) {
	switch kind {
	case reflect.Int:
		*(*int)(fieldPointer) = int(source)
	case reflect.Int8:
		*(*int8)(fieldPointer) = int8(source) //nolint:gosec // deliberate truncation
	case reflect.Int16:
		*(*int16)(fieldPointer) = int16(source) //nolint:gosec // deliberate truncation
	case reflect.Int32:
		*(*int32)(fieldPointer) = int32(source) //nolint:gosec // deliberate truncation
	case reflect.Int64:
		*(*int64)(fieldPointer) = source
	default:
	}
}

// storeUintKindAtUnsafe writes a narrowed unsigned-integer value at fieldPointer
// according to the resolved kind. The narrowing follows Go's standard modular semantics
// for typed conversions.
//
// Takes fieldPointer (unsafe.Pointer) which is the address of the field.
// Takes kind (reflect.Kind) which identifies the unsigned-integer width.
// Takes source (uint64) which is the value to narrow and store.
func storeUintKindAtUnsafe(fieldPointer unsafe.Pointer, kind reflect.Kind, source uint64) {
	switch kind {
	case reflect.Uint:
		*(*uint)(fieldPointer) = uint(source)
	case reflect.Uint8:
		*(*uint8)(fieldPointer) = uint8(source) //nolint:gosec // deliberate truncation
	case reflect.Uint16:
		*(*uint16)(fieldPointer) = uint16(source) //nolint:gosec // deliberate truncation
	case reflect.Uint32:
		*(*uint32)(fieldPointer) = uint32(source) //nolint:gosec // deliberate truncation
	case reflect.Uint64:
		*(*uint64)(fieldPointer) = source
	case reflect.Uintptr:
		*(*uintptr)(fieldPointer) = uintptr(source)
	default:
	}
}
