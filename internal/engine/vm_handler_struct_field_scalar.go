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

	"pipit.sh/pipit/internal/isa"
)

var (
	// structFieldUnsafeStringType is the cached reflect.Type for `string`. Used by the
	// string-write fast path to construct a reflect.Value pointing at the destination field
	// via reflect.NewAt; that route uses typedmemmove internally and so inserts the GC write
	// barrier required when overwriting a pointer-bearing slot in a heap object.
	structFieldUnsafeStringType = reflect.TypeFor[string]()

	// structFieldUnsafeStringABIType caches the *abi.Type pointer of
	// reflect.TypeFor[string]() so the typed-string-set hot path
	// (handleSetStructFieldUnsafeString below) skips reflect.NewAt's internal ptrTo()
	// lookup. Constant for the program's lifetime; initialised at package init.
	structFieldUnsafeStringABIType = reflectValueABIType(structFieldUnsafeStringType)
)

// handleGetStructFieldUnsafeFloat reads a float-kind struct field.
//
// Stores the float64 value into floats[B]. Uses unsafe.Pointer arithmetic via the
// pre-resolved structLayoutTable entry; falls back to a reflect walk on non-addressable
// receivers.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldUnsafeFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, instruction.C)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.C, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.C)
		}
		registers.Floats[instruction.B] = field.Float()
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Float32:
		registers.Floats[instruction.B] = float64(*(*float32)(fieldPointer))
	case reflect.Float64:
		registers.Floats[instruction.B] = *(*float64)(fieldPointer)
	default:
	}
	return opContinue
}

// handleSetStructFieldUnsafeFloat writes a float-kind struct field.
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
func handleSetStructFieldUnsafeFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		field.SetFloat(registers.Floats[instruction.C])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	source := registers.Floats[instruction.C]
	switch reflect.Kind(layout.Kind) {
	case reflect.Float32:
		*(*float32)(fieldPointer) = float32(source)
	case reflect.Float64:
		*(*float64)(fieldPointer) = source
	default:
	}
	return opContinue
}

// handleSetStructFieldUnsafeBool writes a bool-kind struct field.
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
func handleSetStructFieldUnsafeBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		field.SetBool(registers.Bools[instruction.C])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	*(*bool)(fieldPointer) = registers.Bools[instruction.C]
	return opContinue
}

// handleSetStructFieldUnsafeString writes a string-kind struct field.
//
// Goes through reflect.NewAt + SetString so the runtime inserts the GC write barrier for
// the string header's data pointer. Falls back to a full reflect walk + SetString on
// non-addressable receivers.
//
// Skips reflect.NewAt's internal ptrTo() lookup by reusing the cached *abi.Type for
// string (structFieldUnsafeStringABIType above), which is initialised once at package
// init.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldUnsafeString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layoutIndex := readExtensionWideIndex(frame)
	layout := structLayoutAtWide(frame, layoutIndex)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		field.SetString(materialiseString(vm.Arena, registers.Strings[instruction.C]))
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	unsafeNewAt(structFieldUnsafeStringABIType, fieldPointer, reflect.String).
		SetString(MaterialiseStringForFieldStore(vm.Arena, base, registers.Strings[instruction.C]))
	return opContinue
}
