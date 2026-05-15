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

// handleGetStructFieldUnsafeInt is the safe-build fallback for the int-kind struct-field
// read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Ints[instruction.B] = field.Int()
	return opContinue
}

// handleGetStructFieldUnsafeUint is the safe-build fallback for the uint-kind
// struct-field read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Uints[instruction.B] = field.Uint()
	return opContinue
}

// handleGetStructFieldUnsafeBool is the safe-build fallback for the bool-kind
// struct-field read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Bools[instruction.B] = field.Bool()
	return opContinue
}

// handleGetStructFieldUnsafeString is the safe-build fallback for the string-kind
// struct-field read.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	registers.Strings[instruction.B] = field.String()
	return opContinue
}

// handleGetStructFieldUnsafeSliceInt reads a []int64 struct field via reflect.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.SlicesInt[instruction.B])
}

// handleGetStructFieldUnsafeSliceFloat reads a []float64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesFloat[instruction.B])
}

// handleGetStructFieldUnsafeSliceUint reads a []uint64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesUint[instruction.B])
}

// handleGetStructFieldUnsafeSliceString reads a []string struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesString[instruction.B])
}

// handleGetStructFieldUnsafeSliceBool reads a []bool struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesBool[instruction.B])
}

// handleGetStructFieldUnsafeSliceByte reads a []byte struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the source value and receives the read result.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUnsafeSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectRead(registers, frame, instruction.C, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.C)
	}
	return readTypedSliceFieldFallback(vm, frame, registers, instruction.C, field, &registers.slicesByte[instruction.B])
}

// handleSetStructFieldUnsafeInt is the safe-build fallback for the int-kind struct-field
// write.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source scalar
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	field.SetInt(registers.Ints[instruction.C])
	return opContinue
}

// handleSetStructFieldUnsafeUint is the safe-build fallback for the uint-kind
// struct-field write.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source scalar
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	field.SetUint(registers.Uints[instruction.C])
	return opContinue
}

// handleSetStructFieldUnsafeSliceInt writes a []int64 struct field.
//
// reflect.Value.Set inserts the GC write barrier automatically.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.SlicesInt[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldUnsafeSliceFloat writes a []float64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesFloat[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldUnsafeSliceUint writes a []uint64 struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesUint[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// writeSliceFieldSameLayout writes a slice header into an addressable field by copying
// the (Data, Len, Cap) bytes directly, bypassing reflect.Value.Set's type check when the
// field's element type has the same layout as the bank's canonical type.
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

// handleSetStructFieldUnsafeSliceString writes a []string struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesString[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldUnsafeSliceBool writes a []bool struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesBool[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}

// handleSetStructFieldUnsafeSliceByte writes a []byte struct field.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the extension wide index and structLayoutTable.
// Takes registers (*Registers) which holds the destination struct and the source slice
// value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldUnsafeSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	field, ok := structFieldReflectWrite(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	source := registers.slicesByte[instruction.C]
	writeSliceFieldSameLayout(field, unsafe.Pointer(unsafe.SliceData(source)), len(source), cap(source))
	return opContinue
}
