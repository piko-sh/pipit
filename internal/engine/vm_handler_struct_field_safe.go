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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/isa"
)

// handleGetStructFieldInt64Wide is the safe-build fallback for the int-kind struct-field
// read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldInt64Wide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Ints[instruction.B] = field.Int()
	return opContinue
}

// handleGetStructFieldUint64Wide is the safe-build fallback for the uint-kind
// struct-field read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUint64Wide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Uints[instruction.B] = field.Uint()
	return opContinue
}

// handleGetStructFieldBoolWide is the safe-build fallback for the bool-kind struct-field
// read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldBoolWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Bools[instruction.B] = field.Bool()
	return opContinue
}

// handleGetStructFieldStringWide is the safe-build fallback for the string-kind
// struct-field read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldStringWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Strings[instruction.B] = field.String()
	return opContinue
}

// handleGetStructFieldSliceInt64 reads a []int64 struct field via reflect.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.SlicesInt[instruction.B])
}

// handleGetStructFieldSliceFloat64 reads a []float64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceFloat64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesFloat[instruction.B])
}

// handleGetStructFieldSliceUint64 reads a []uint64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceUint64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesUint[instruction.B])
}

// handleGetStructFieldSliceString reads a []string struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesString[instruction.B])
}

// handleGetStructFieldSliceBool reads a []bool struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesBool[instruction.B])
}

// handleGetStructFieldSliceByte reads a []byte struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	if field.Kind() == reflect.Slice && field.Type().Elem().Kind() == reflect.Uint8 {
		registers.slicesByte[instruction.B] = field.Bytes()
		return opContinue
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesByte[instruction.B])
}

// handleSetStructFieldInt64Wide is the safe-build fallback for the int-kind struct-field
// write.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source scalar
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldInt64Wide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	field.SetInt(registers.Ints[instruction.C])
	return opContinue
}

// handleSetStructFieldUint64Wide is the safe-build fallback for the uint-kind
// struct-field write.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source scalar
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUint64Wide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	field.SetUint(registers.Uints[instruction.C])
	return opContinue
}

// handleSetStructFieldSliceInt64 writes a []int64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.SlicesInt[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldSliceFloat64 writes a []float64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceFloat64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesFloat[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldSliceUint64 writes a []uint64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceUint64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesUint[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// writeSliceFieldSameLayout writes a slice header into an addressable field by copying
// the (Data, Len, Cap) bytes directly, bypassing reflect.Value.Set's type check when the
// field's element type has the same layout as the bank's canonical type. Callers pass the
// slice through storeSliceFieldBarrier first.
//
// Takes field (reflect.Value) which is the destination slice field.
// Takes data (unsafe.Pointer) which is the slice's backing array pointer.
// Takes length (int) which is len(source).
// Takes capacity (int) which is cap(source).
func writeSliceFieldSameLayout(field reflect.Value, data unsafe.Pointer, length, capacity int) {
	if !field.CanAddr() {
		return
	}
	header := (*struct {
		Data unsafe.Pointer
		Len  int
		Cap  int
	})(unsafe.Pointer(field.UnsafeAddr()))
	header.Data = data
	header.Len = length
	header.Cap = capacity
}

// handleSetStructFieldSliceString writes a []string struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesString[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldSliceBool writes a []bool struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesBool[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldSliceByte writes a []byte struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesByte[instruction.C]
	source = storeSliceFieldBarrier(vm.Arena, field, source)
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}
