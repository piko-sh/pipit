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
	"fmt"
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

// handleUnsafeString implements isa.OpUnsafeString. It constructs a string from a pointer
// in general[B] and a length in ints[C], copying the bytes into a heap-backed buffer for
// safety.
//
// Takes vm (*VM) which provides allocation limits and error reporting.
// Takes registers (*Registers) which provides the general, int, and string register
// banks.
// Takes instruction (instruction) which encodes the destination string register, source
// pointer register, and length register.
//
// Returns OpResult which signals continuation or a panic on allocation limit violation.
func handleUnsafeString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	pointer := registers.General[instruction.B]
	length := registers.Ints[instruction.C]

	if !pointer.IsValid() || !isPointerKind(pointer.Kind()) {
		vm.evalError = newInvariantError("unsafe.String called on non-pointer value")
		return opPanicError
	}
	if length < 0 || allocationTooLarge(int(length), 1) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: unsafe.String: len out of range"))
	}
	if pointer.IsNil() && length > 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: unsafe.String: ptr is nil and len is not zero"))
	}
	if length == 0 {
		registers.Strings[instruction.A] = ""
		return opContinue
	}

	if vm.Limits.MaxAllocSize > 0 && int(length) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf("%w: unsafe.String length %d exceeds limit %d",
			fault.ErrAllocationLimit, length, vm.Limits.MaxAllocSize)
		return opPanicError
	}

	if err := vm.checkUnsafeSpan("unsafe.String", pointer.Pointer(), uintptr(length)); err != nil {
		vm.evalError = err
		return opPanicError
	}

	base := pointer.UnsafePointer()
	source := unsafe.Slice((*byte)(base), length)
	buffer := make([]byte, length)
	copy(buffer, source)
	registers.Strings[instruction.A] = string(buffer)

	return opContinue
}

// handleUnsafeStringData implements isa.SubOpUnsafeStringData by copying strings[B] into
// a fresh byte buffer and storing a pointer to its first byte in general[A].
//
// Empty source strings yield a typed nil *byte. The copy decouples the returned pointer
// from the immutable string backing store so callers cannot mutate string memory.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the string and general register banks.
// Takes instruction (instruction) which encodes the destination general register and
// source string register.
//
// Returns OpResult indicating the next execution step.
func handleUnsafeStringData(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.Strings[instruction.B]

	if len(s) == 0 {
		registers.General[instruction.A] = symtab.ZeroValueForType(reflect.PointerTo(reflect.TypeFor[byte]()))
		return opContinue
	}

	buffer := []byte(s)
	result := reflect.ValueOf(&buffer[0])
	registers.General[instruction.A] = result

	if vm.Limits.SafeMode {
		base := result.Pointer()
		vm.recordPointerProvenance(result, pointerBound{base: base, limit: base + uintptr(len(buffer)), elemSize: 1})
	}

	return opContinue
}

// handleUnsafeSlice implements isa.OpUnsafeSlice. It creates a slice of the element type
// pointed to by general[B] with length ints[C], copying each element via reflect for
// safety.
//
// Takes vm (*VM) which provides allocation limits and error reporting.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which provides the general and int register banks.
// Takes instruction (instruction) which encodes the destination general register, source
// pointer register, and length register.
//
// Returns OpResult which signals continuation or a panic on allocation limit violation.
func handleUnsafeSlice(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	pointer := registers.General[instruction.B]
	length := registers.Ints[instruction.C]

	if !pointer.IsValid() {
		vMPanicInvalidRegister("handleUnsafeSlice", "pointer", instruction.B, instruction, frame, registers)
	}
	if pointer.Kind() != reflect.Pointer {
		vm.evalError = newInvariantError("unsafe.Slice called on non-pointer value")
		return opPanicError
	}
	elementType := pointer.Type().Elem()

	if length < 0 || allocationTooLarge(int(length), elementType.Size()) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: unsafe.Slice: len out of range"))
	}
	if pointer.IsNil() && length > 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: unsafe.Slice: ptr is nil and len is not zero"))
	}
	if length == 0 {
		registers.General[instruction.A] = reflect.MakeSlice(reflect.SliceOf(elementType), 0, 0)
		return opContinue
	}

	if vm.Limits.MaxAllocSize > 0 && int(length) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf("%w: unsafe.Slice length %d exceeds limit %d",
			fault.ErrAllocationLimit, length, vm.Limits.MaxAllocSize)
		return opPanicError
	}

	elementSize := elementType.Size()

	if err := vm.checkUnsafeSpan("unsafe.Slice", pointer.Pointer(), uintptr(length)*elementSize); err != nil {
		vm.evalError = err
		return opPanicError
	}

	slice := reflect.MakeSlice(reflect.SliceOf(elementType), int(length), int(length))
	base := pointer.UnsafePointer()

	for i := range length {
		source := reflect.NewAt(elementType, unsafe.Add(base, uintptr(i)*elementSize))
		slice.Index(int(i)).Set(source.Elem())
	}

	registers.General[instruction.A] = slice

	return opContinue
}

// handleUnsafeSliceData implements isa.SubOpUnsafeSliceData. It stores the address of the
// first element of general[B] in general[A], or a nil pointer when the slice is empty or
// invalid.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which provides the general register bank.
// Takes instruction (instruction) which encodes the destination general register and
// source slice register.
//
// Returns OpResult indicating the next execution step.
func handleUnsafeSliceData(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.General[instruction.B]

	if !s.IsValid() {
		vMPanicInvalidRegister("handleUnsafeSliceData", "slice", instruction.B, instruction, frame, registers)
	}
	if s.Len() == 0 {
		elementType := s.Type().Elem()
		registers.General[instruction.A] = symtab.ZeroValueForType(reflect.PointerTo(elementType))
		return opContinue
	}

	result := s.Index(0).Addr()
	registers.General[instruction.A] = result

	if vm.Limits.SafeMode {
		elementSize := s.Type().Elem().Size()
		base := result.Pointer()
		vm.recordPointerProvenance(result, pointerBound{base: base, limit: base + uintptr(s.Cap())*elementSize, elemSize: elementSize})
	}

	return opContinue
}

// handleUnsafeAdd implements isa.OpUnsafeAdd. It advances the pointer in general[B] by
// ints[C] bytes using unsafe.Add and stores the result in general[A].
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the general and int register banks.
// Takes instruction (instruction) which encodes the destination general register, source
// pointer register, and byte offset register.
//
// Returns OpResult indicating the next execution step.
func handleUnsafeAdd(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	pointer := registers.General[instruction.B]
	offset := registers.Ints[instruction.C]

	if !pointer.IsValid() || !isUnsafePointerKind(pointer.Kind()) {
		vm.evalError = newInvariantError("unsafe.Add called on non-pointer value")
		return opPanicError
	}
	if pointer.IsNil() {
		registers.General[instruction.A] = reflect.ValueOf(unsafe.Pointer(nil))
		return opContinue
	}

	base := pointer.Pointer()
	var origin pointerBound
	if vm.Limits.SafeMode {
		recorded, found := vm.lookupPointerProvenance(base)
		if !found {
			vm.evalError = newRuntimePanicError("safe mode: unsafe.Add: %v (pointer has no tracked origin)", errUnsafeBounds)
			return opPanicError
		}
		newAddr := base + uintptr(offset) //nolint:gosec // bounds-checked below
		if newAddr < recorded.base || newAddr > recorded.limit {
			vm.evalError = newRuntimePanicError("safe mode: unsafe.Add: %v", errUnsafeBounds)
			return opPanicError
		}
		origin = recorded
	}

	result := unsafe.Add(pointer.UnsafePointer(), int(offset))
	resultValue := reflect.ValueOf(result)
	registers.General[instruction.A] = resultValue

	if vm.Limits.SafeMode {
		vm.recordPointerProvenance(resultValue, origin)
	}

	return opContinue
}

// isPointerKind reports whether kind is a typed or untyped pointer kind, used by the
// unsafe handlers to confirm a register holds a pointer before calling
// reflect.Value.Pointer or .IsNil, both of which panic for unrelated kinds.
//
// Takes kind (reflect.Kind) which is the register value's kind.
//
// Returns true for reflect.Pointer and reflect.UnsafePointer.
func isPointerKind(kind reflect.Kind) bool {
	return kind == reflect.Pointer || kind == reflect.UnsafePointer
}

// isUnsafePointerKind reports whether kind is a pointer kind suitable for unsafe.Add
// pointer arithmetic. It accepts the same kinds as isPointerKind; the distinct name
// documents intent at the call site.
//
// Takes kind (reflect.Kind) which is the register value's kind.
//
// Returns true for reflect.Pointer and reflect.UnsafePointer.
func isUnsafePointerKind(kind reflect.Kind) bool {
	return kind == reflect.Pointer || kind == reflect.UnsafePointer
}
