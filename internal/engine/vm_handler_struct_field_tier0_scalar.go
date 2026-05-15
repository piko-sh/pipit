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

// handleGetStructFieldIntT0 reads an int-kind struct field via the tier-0 path.
//
// Operand A=int destination, B=general source, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldIntT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		registers.Ints[instruction.A] = field.Int()
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Int:
		registers.Ints[instruction.A] = int64(*(*int)(fieldPointer))
	case reflect.Int8:
		registers.Ints[instruction.A] = int64(*(*int8)(fieldPointer))
	case reflect.Int16:
		registers.Ints[instruction.A] = int64(*(*int16)(fieldPointer))
	case reflect.Int32:
		registers.Ints[instruction.A] = int64(*(*int32)(fieldPointer))
	case reflect.Int64:
		registers.Ints[instruction.A] = *(*int64)(fieldPointer)
	default:
	}
	return opContinue
}

// handleGetStructFieldUintT0 reads a uint-kind struct field via the tier-0 path.
//
// Operand A=uint destination, B=general source, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldUintT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		registers.Uints[instruction.A] = field.Uint()
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Uint:
		registers.Uints[instruction.A] = uint64(*(*uint)(fieldPointer))
	case reflect.Uint8:
		registers.Uints[instruction.A] = uint64(*(*uint8)(fieldPointer))
	case reflect.Uint16:
		registers.Uints[instruction.A] = uint64(*(*uint16)(fieldPointer))
	case reflect.Uint32:
		registers.Uints[instruction.A] = uint64(*(*uint32)(fieldPointer))
	case reflect.Uint64:
		registers.Uints[instruction.A] = *(*uint64)(fieldPointer)
	case reflect.Uintptr:
		registers.Uints[instruction.A] = uint64(*(*uintptr)(fieldPointer))
	default:
	}
	return opContinue
}

// handleGetStructFieldFloatT0 reads a float-kind struct field via tier-0.
//
// Operand A=float destination, B=general source, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldFloatT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		registers.Floats[instruction.A] = field.Float()
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Float32:
		registers.Floats[instruction.A] = float64(*(*float32)(fieldPointer))
	case reflect.Float64:
		registers.Floats[instruction.A] = *(*float64)(fieldPointer)
	default:
	}
	return opContinue
}

// handleGetStructFieldBoolT0 reads a bool-kind struct field via tier-0.
//
// Operand A=bool destination, B=general source, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldBoolT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		registers.Bools[instruction.A] = field.Bool()
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	registers.Bools[instruction.A] = *(*bool)(fieldPointer)
	return opContinue
}

// handleSetStructFieldIntT0 writes an int-kind struct field via tier-0.
//
// Operand A=general structReg, B=int valueReg, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldIntT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.A)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.A)
		}
		field.SetInt(registers.Ints[instruction.B])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	source := registers.Ints[instruction.B]
	switch reflect.Kind(layout.Kind) {
	case reflect.Int:
		*(*int)(fieldPointer) = int(source)
	case reflect.Int8:
		*(*int8)(fieldPointer) = int8(source) //nolint:gosec // matches SetInt truncation
	case reflect.Int16:
		*(*int16)(fieldPointer) = int16(source) //nolint:gosec // matches SetInt truncation
	case reflect.Int32:
		*(*int32)(fieldPointer) = int32(source) //nolint:gosec // matches SetInt truncation
	case reflect.Int64:
		*(*int64)(fieldPointer) = source
	default:
	}
	return opContinue
}

// handleSetStructFieldUintT0 writes a uint-kind struct field via tier-0.
//
// Operand A=general structReg, B=uint valueReg, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldUintT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.A)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.A)
		}
		field.SetUint(registers.Uints[instruction.B])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	source := registers.Uints[instruction.B]
	switch reflect.Kind(layout.Kind) {
	case reflect.Uint:
		*(*uint)(fieldPointer) = uint(source)
	case reflect.Uint8:
		*(*uint8)(fieldPointer) = uint8(source) //nolint:gosec // matches SetUint truncation
	case reflect.Uint16:
		*(*uint16)(fieldPointer) = uint16(source) //nolint:gosec // matches SetUint truncation
	case reflect.Uint32:
		*(*uint32)(fieldPointer) = uint32(source) //nolint:gosec // matches SetUint truncation
	case reflect.Uint64:
		*(*uint64)(fieldPointer) = source
	case reflect.Uintptr:
		*(*uintptr)(fieldPointer) = uintptr(source)
	default:
	}
	return opContinue
}

// handleSetStructFieldFloatT0 writes a float-kind struct field via tier-0.
//
// Operand A=general structReg, B=float valueReg, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldFloatT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.A)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.A)
		}
		field.SetFloat(registers.Floats[instruction.B])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	source := registers.Floats[instruction.B]
	switch reflect.Kind(layout.Kind) {
	case reflect.Float32:
		*(*float32)(fieldPointer) = float32(source)
	case reflect.Float64:
		*(*float64)(fieldPointer) = source
	default:
	}
	return opContinue
}

// handleSetStructFieldBoolT0 writes a bool-kind struct field via tier-0.
//
// Operand A=general structReg, B=bool valueReg, C=structLayoutTable index. Uses
// unsafe.Pointer arithmetic via the pre-resolved structLayoutTable entry, falling back to
// a reflect.Field walk when the unsafe base is unavailable.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldBoolT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	base, ok := structFieldUnsafeBase(registers, instruction.A)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.A)
		}
		field.SetBool(registers.Bools[instruction.B])
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	*(*bool)(fieldPointer) = registers.Bools[instruction.B]
	return opContinue
}

// handleGetStructFieldSliceLen reads the length of a slice-kind struct field without
// materialising a slice header value.
//
// Operand A=int destination, B=general source (receiver), C=structLayoutTable index.
// Reads the header's length word in place via unsafe.Pointer arithmetic, falling back to
// a reflect.Field walk when the unsafe base is unavailable. No allocation on either path.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldSliceLen(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	if reflect.Kind(layout.Kind) != reflect.Slice {
		panicTier0UnexpectedFieldKind(frame, "GET_STRUCT_FIELD_SLICE_LEN", reflect.Kind(layout.Kind))
	}
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		registers.Ints[instruction.A] = int64(field.Len())
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	registers.Ints[instruction.A] = int64(*(*int)(unsafe.Add(fieldPointer, 8)))
	return opContinue
}

// handleGetStructFieldSliceIndexScalar reads one full-width scalar sub-field of one
// element of a slice-typed struct field: bank(ext2.C)[A] =
// receiver[B].field(layout[C])[ints[ext1.C]].subfield(ext2.wide).
//
// The slice header is read in place through the receiver on every access -aliasing and
// reslice-through-receiver semantics stay automatic, no header value is materialised, and
// no element copy is boxed. Out-of-range indexes raise the interpreted panic. Names the
// scalar element banks the fused slice-index read can service; any other element kind
// declines through the default and takes the generic path.
//
// Takes vm (*VM) which supplies panic plumbing for the bounds check.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the receiver, index, and destination.
// Takes instruction (instruction) which carries A=dest, B=receiver, C=layout index.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldSliceIndexScalar(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	indexRegister, elementSize, subFieldOffset, subKind := readFusedSliceFieldExtensions(frame)
	layout := readTier0StructFieldLayout(frame, instruction.C)
	index := int(registers.Ints[indexRegister])
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		if boundsResult, inRange := checkSliceBounds(vm, field, index); !inRange {
			return boundsResult
		}
		storeScalarSubFieldFromReflect(registers, field.Index(index), instruction.A, subFieldOffset, subKind)
		return opContinue
	}
	header := (*snapshotSliceHeader)(unsafe.Add(base, uintptr(layout.Offset)))
	if index < 0 || index >= header.Len {
		return raiseIndexOutOfRange(vm, index, header.Len)
	}
	scalarPointer := unsafe.Add(header.Data, uintptr(index)*elementSize+subFieldOffset)
	switch subKind {
	case isa.RegisterInt:
		registers.Ints[instruction.A] = *(*int64)(scalarPointer)
	case isa.RegisterFloat:
		registers.Floats[instruction.A] = *(*float64)(scalarPointer)
	case isa.RegisterUint:
		registers.Uints[instruction.A] = *(*uint64)(scalarPointer)
	case isa.RegisterBool:
		registers.Bools[instruction.A] = *(*bool)(scalarPointer)
	default:
	}
	return opContinue
}

// handleSetStructFieldIndexGeneral writes one element of an array-typed struct field:
// receiver[A].field(layout[C]).elem[ints[ext1.C]] = general[B].
//
// The write goes through an addressable reflect view of the element so every arm keeps GC
// write barriers, sharing isa.OpIndexSet's coercion pipeline via storeIndexedElement
// (including its direct any-slot fast path for eface elements).
//
// Takes vm (*VM) which supplies the arena and panic plumbing.
// Takes frame (*CallFrame) which provides the layout table, type table, and PC.
// Takes registers (*Registers) which holds the receiver, index, and source value.
// Takes instruction (instruction) which carries A=receiver, B=source, C=layout index.
//
// Returns OpResult indicating the next execution step.
func handleSetStructFieldIndexGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	indexRegister, arrayLength, elementSize, _ := readFusedArrayFieldExtensions(frame)
	layout := readTier0StructFieldLayout(frame, instruction.C)
	index := int(registers.Ints[indexRegister])
	base, ok := structFieldUnsafeBase(registers, instruction.A)
	if !ok {
		field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.A)
		}
		if boundsResult, inRange := checkSliceBounds(vm, field, index); !inRange {
			return boundsResult
		}
		return storeIndexedElement(vm, frame, registers, field.Index(index), instruction.B, instruction)
	}
	if index < 0 || index >= arrayLength {
		return raiseIndexOutOfRange(vm, index, arrayLength)
	}
	elementPointer := unsafe.Add(base, uintptr(layout.Offset)+uintptr(index)*elementSize)
	fieldType := frame.Function.TypeTable[layout.FieldTypeIndex]
	elementType := fieldType.Elem()
	target := unsafeNewAt(reflectValueABIType(elementType), elementPointer, elementType.Kind())
	return storeIndexedElement(vm, frame, registers, target, instruction.B, instruction)
}
