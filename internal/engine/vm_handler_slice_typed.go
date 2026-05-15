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

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// handleSliceGetInt handles the isa.OpSliceGetInt instruction by reading an integer
// element from a slice or array without reflect boxing.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	element := unwrapInterfaceElement(collection.Index(index))
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		registers.Ints[instruction.A] = element.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		registers.Ints[instruction.A] = safeconv.Uint64ToInt64Reinterpret(element.Uint())
	default:
		vm.evalError = newInvariantError("integer slice read on non-integer element (%s)", element.Kind())
		return opPanicError
	}
	return opContinue
}

// unwrapInterfaceElement returns the concrete value inside an interface-typed slice
// element, leaving non-interface values untouched.
//
// Takes element (reflect.Value) which is the slice element.
//
// Returns the unwrapped concrete value or the original element.
func unwrapInterfaceElement(element reflect.Value) reflect.Value {
	if element.Kind() == reflect.Interface && !element.IsNil() {
		return element.Elem()
	}
	return element
}

// handleSliceSetInt handles the isa.OpSliceSetInt instruction by writing an integer value
// to a slice or array element without reflect boxing.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	element := collection.Index(index)
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		element.SetInt(registers.Ints[instruction.C])
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		element.SetUint(safeconv.Int64ToUint64Reinterpret(registers.Ints[instruction.C]))
	default:
		vm.evalError = newInvariantError("integer slice write on non-integer element (%s)", element.Kind())
		return opPanicError
	}
	return opContinue
}

// handleSliceGetFloat handles the isa.OpSliceGetFloat instruction by reading a float
// element from a slice or array without reflect boxing.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetFloat(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	registers.Floats[instruction.A] = unwrapInterfaceElement(collection.Index(index)).Float()
	return opContinue
}

// handleSliceSetFloat handles the isa.OpSliceSetFloat instruction by writing a float
// value to a slice or array element without reflect boxing.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetFloat(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	collection.Index(index).SetFloat(registers.Floats[instruction.C])
	return opContinue
}

// handleSliceGetString handles the isa.OpSliceGetString instruction by reading a string
// element from a slice or array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	registers.Strings[instruction.A] = unwrapInterfaceElement(collection.Index(index)).String()
	return opContinue
}

// handleSliceSetString handles the isa.OpSliceSetString instruction by writing a string
// value to a slice or array element.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	collection.Index(index).SetString(materialiseStringForIndexedStore(vm.Arena, collection, registers.Strings[instruction.C]))
	return opContinue
}

// handleSliceGetBool handles the isa.OpSliceGetBool instruction by reading a bool element
// from a slice or array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	registers.Bools[instruction.A] = unwrapInterfaceElement(collection.Index(index)).Bool()
	return opContinue
}

// handleSliceSetBool handles the isa.OpSliceSetBool instruction by writing a bool value
// to a slice or array element.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	collection.Index(index).SetBool(registers.Bools[instruction.C])
	return opContinue
}

// handleSliceGetUint handles the isa.OpSliceGetUint instruction by reading a uint element
// from a slice or array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetUint(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	registers.Uints[instruction.A] = unwrapInterfaceElement(collection.Index(index)).Uint()
	return opContinue
}

// handleSliceSetUint handles the isa.OpSliceSetUint instruction by writing a uint value
// to a slice or array element.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetUint(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	collection.Index(index).SetUint(registers.Uints[instruction.C])
	return opContinue
}
