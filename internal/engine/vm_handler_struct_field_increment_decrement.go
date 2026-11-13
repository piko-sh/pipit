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
)

// handleIncrementStructFieldInt64 fuses `s.field++` for int-kind struct fields into a
// single dispatch.
//
// Takes vm (*VM) which carries the fallback context.
// Takes frame (*CallFrame) which provides the structLayoutTable lookup.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (isa.Instruction) which encodes (a, b, c) operand indices.
//
// Returns opContinue after the in-place increment, or the fallback's result.
func handleIncrementStructFieldInt64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instr.C)
	base, ok := structFieldUnsafeBase(registers, instr.B)
	if !ok {
		return incrementOrDecrementStructFieldInt64Fallback(vm, frame, registers, instr, layout, +1)
	}
	if !incrementIntFieldInPlace(unsafe.Add(base, uintptr(layout.Offset)), reflect.Kind(layout.Kind)) {
		return incrementOrDecrementStructFieldInt64Fallback(vm, frame, registers, instr, layout, +1)
	}
	return opContinue
}

// handleDecrementStructFieldInt64 is the decrement sibling of
// handleIncrementStructFieldInt64.
//
// Takes vm (*VM) which carries the fallback context.
// Takes frame (*CallFrame) which provides the structLayoutTable lookup.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) which encodes (a, b, c) operand indices.
//
// Returns opContinue after the in-place decrement, or the fallback's result when the
// receiver is not unsafe-addressable.
func handleDecrementStructFieldInt64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instr.C)
	base, ok := structFieldUnsafeBase(registers, instr.B)
	if !ok {
		return incrementOrDecrementStructFieldInt64Fallback(vm, frame, registers, instr, layout, -1)
	}
	if !decrementIntFieldInPlace(unsafe.Add(base, uintptr(layout.Offset)), reflect.Kind(layout.Kind)) {
		return incrementOrDecrementStructFieldInt64Fallback(vm, frame, registers, instr, layout, -1)
	}
	return opContinue
}

// handleIncrementStructFieldUint64 is the uint sibling of
// handleIncrementStructFieldInt64. Same shape, uint widths.
//
// Takes vm (*VM) which carries the fallback context.
// Takes frame (*CallFrame) which provides the structLayoutTable lookup.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) which encodes (a, b, c) operand indices.
//
// Returns opContinue after the in-place increment, or the fallback's result when the
// receiver is not unsafe-addressable.
func handleIncrementStructFieldUint64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instr.C)
	base, ok := structFieldUnsafeBase(registers, instr.B)
	if !ok {
		return incrementOrDecrementStructFieldUint64Fallback(vm, frame, registers, instr, layout, +1)
	}
	if !incrementUintFieldInPlace(unsafe.Add(base, uintptr(layout.Offset)), reflect.Kind(layout.Kind)) {
		return incrementOrDecrementStructFieldUint64Fallback(vm, frame, registers, instr, layout, +1)
	}
	return opContinue
}

// handleDecrementStructFieldUint64 is the decrement sibling of
// handleIncrementStructFieldUint64.
//
// Takes vm (*VM) which carries the fallback context.
// Takes frame (*CallFrame) which provides the structLayoutTable lookup.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) which encodes (a, b, c) operand indices.
//
// Returns opContinue after the in-place decrement, or the fallback's result when the
// receiver is not unsafe-addressable.
func handleDecrementStructFieldUint64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instr.C)
	base, ok := structFieldUnsafeBase(registers, instr.B)
	if !ok {
		return incrementOrDecrementStructFieldUint64Fallback(vm, frame, registers, instr, layout, -1)
	}
	if !decrementUintFieldInPlace(unsafe.Add(base, uintptr(layout.Offset)), reflect.Kind(layout.Kind)) {
		return incrementOrDecrementStructFieldUint64Fallback(vm, frame, registers, instr, layout, -1)
	}
	return opContinue
}

// incrementIntFieldInPlace applies the native `++` operation to the signed-integer kind
// referenced by fieldPointer.
//
// Takes fieldPointer (unsafe.Pointer) which is the unsafe address of the field.
// Takes kind (reflect.Kind) which is the resolved signed-integer kind.
//
// Returns true when the kind was handled, false when the kind is not a recognised
// signed-integer width.
func incrementIntFieldInPlace(fieldPointer unsafe.Pointer, kind reflect.Kind) bool {
	switch kind {
	case reflect.Int:
		*(*int)(fieldPointer)++
	case reflect.Int8:
		*(*int8)(fieldPointer)++
	case reflect.Int16:
		*(*int16)(fieldPointer)++
	case reflect.Int32:
		*(*int32)(fieldPointer)++
	case reflect.Int64:
		*(*int64)(fieldPointer)++
	default:
		return false
	}
	return true
}

// decrementIntFieldInPlace applies the native `--` operation to the signed-integer kind
// referenced by fieldPointer.
//
// Takes fieldPointer (unsafe.Pointer) which is the unsafe address of the field.
// Takes kind (reflect.Kind) which is the resolved signed-integer kind.
//
// Returns true when the kind was handled, false when the kind is not a recognised
// signed-integer width.
func decrementIntFieldInPlace(fieldPointer unsafe.Pointer, kind reflect.Kind) bool {
	switch kind {
	case reflect.Int:
		*(*int)(fieldPointer)--
	case reflect.Int8:
		*(*int8)(fieldPointer)--
	case reflect.Int16:
		*(*int16)(fieldPointer)--
	case reflect.Int32:
		*(*int32)(fieldPointer)--
	case reflect.Int64:
		*(*int64)(fieldPointer)--
	default:
		return false
	}
	return true
}

// incrementUintFieldInPlace applies the native `++` operation to the unsigned-integer
// kind referenced by fieldPointer.
//
// Takes fieldPointer (unsafe.Pointer) which is the unsafe address of the field.
// Takes kind (reflect.Kind) which is the resolved unsigned-integer kind.
//
// Returns true when the kind was handled, false when the kind is not a recognised
// unsigned-integer width.
func incrementUintFieldInPlace(fieldPointer unsafe.Pointer, kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint:
		*(*uint)(fieldPointer)++
	case reflect.Uint8:
		*(*uint8)(fieldPointer)++
	case reflect.Uint16:
		*(*uint16)(fieldPointer)++
	case reflect.Uint32:
		*(*uint32)(fieldPointer)++
	case reflect.Uint64:
		*(*uint64)(fieldPointer)++
	case reflect.Uintptr:
		*(*uintptr)(fieldPointer)++
	default:
		return false
	}
	return true
}

// decrementUintFieldInPlace applies the native `--` operation to the unsigned-integer
// kind referenced by fieldPointer.
//
// Takes fieldPointer (unsafe.Pointer) which is the unsafe address of the field.
// Takes kind (reflect.Kind) which is the resolved unsigned-integer kind.
//
// Returns true when the kind was handled, false when the kind is not a recognised
// unsigned-integer width.
func decrementUintFieldInPlace(fieldPointer unsafe.Pointer, kind reflect.Kind) bool {
	switch kind {
	case reflect.Uint:
		*(*uint)(fieldPointer)--
	case reflect.Uint8:
		*(*uint8)(fieldPointer)--
	case reflect.Uint16:
		*(*uint16)(fieldPointer)--
	case reflect.Uint32:
		*(*uint32)(fieldPointer)--
	case reflect.Uint64:
		*(*uint64)(fieldPointer)--
	case reflect.Uintptr:
		*(*uintptr)(fieldPointer)--
	default:
		return false
	}
	return true
}

// incrementOrDecrementStructFieldInt64Fallback walks the field via reflect when the
// receiver isn't unsafe-addressable (e.g., embedded interface holding a non-pointer
// struct). Honours the layout's Kind for the typed SetInt.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) which encodes (a, b, c) operand indices.
// Takes layout (StructFieldLayout) which is the resolved field descriptor.
// Takes delta (int64) which is +1 for increment, -1 for decrement.
//
// Returns opContinue after applying the typed delta via reflect, or the raised
// interpreted panic when the receiver is a nil pointer.
func incrementOrDecrementStructFieldInt64Fallback(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction, layout program.StructFieldLayout, delta int64) OpResult {
	if !incrementOrDecrementStructFieldInt64Apply(frame, registers, instr.B, layout, delta) {
		return structFieldFallbackMiss(vm, frame, registers, instr.B)
	}
	return opContinue
}

// incrementOrDecrementStructFieldInt64Apply performs the reflect-walk int increment or
// decrement without raising on failure.
//
// Takes frame (*CallFrame) which provides the current PC.
// Takes registers (*Registers) which holds the receiver.
// Takes receiverRegister (uint8) which is the receiver slot.
// Takes layout (program.StructFieldLayout) which describes the field path.
// Takes delta (int64) which is the increment or decrement.
//
// Returns bool which is true when the write applied.
func incrementOrDecrementStructFieldInt64Apply(frame *CallFrame, registers *Registers, receiverRegister uint8, layout program.StructFieldLayout, delta int64) bool {
	field, ok := structFieldReflectWrite(registers, frame, receiverRegister, layout)
	if !ok {
		return false
	}
	field.SetInt(field.Int() + delta)
	return true
}

// incrementOrDecrementStructFieldUint64Fallback is the uint sibling of
// incrementOrDecrementStructFieldInt64Fallback.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) which encodes (a, b, c) operand indices.
// Takes layout (StructFieldLayout) which is the resolved field descriptor.
// Takes delta (int64) which is +1 for increment, -1 for decrement.
//
// Returns opContinue after applying the typed delta via reflect, or the raised
// interpreted panic when the receiver is a nil pointer.
func incrementOrDecrementStructFieldUint64Fallback(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction, layout program.StructFieldLayout, delta int64) OpResult {
	if !incrementOrDecrementStructFieldUint64Apply(frame, registers, instr.B, layout, delta) {
		return structFieldFallbackMiss(vm, frame, registers, instr.B)
	}
	return opContinue
}

// incrementOrDecrementStructFieldUint64Apply is the uint sibling of
// incrementOrDecrementStructFieldInt64Apply. The settability note there applies here too.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the receiver.
// Takes receiverRegister (uint8) which is the receiver slot.
// Takes layout (StructFieldLayout) which describes the field path.
// Takes delta (int64) which is the increment or decrement.
//
// Returns true when the write applied, false when the receiver could not be walked.
func incrementOrDecrementStructFieldUint64Apply(frame *CallFrame, registers *Registers, receiverRegister uint8, layout program.StructFieldLayout, delta int64) bool {
	field, ok := structFieldReflectWrite(registers, frame, receiverRegister, layout)
	if !ok {
		return false
	}
	current := field.Uint()
	if delta >= 0 {
		field.SetUint(current + 1)
	} else {
		field.SetUint(current - 1)
	}
	return true
}
