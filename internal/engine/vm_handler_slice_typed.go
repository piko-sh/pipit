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

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
)

// handleGetElementInt64 handles the isa.OpGetElementInt64 instruction by reading an
// integer element from a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetElementInt64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if element, plain := getElementInt64WithoutReflect(registers.General[instruction.B], registers.Ints[instruction.C]); plain {
		registers.Ints[instruction.A] = element
		return opContinue
	}
	return sliceGetViaReflect(vm, registers, instruction, isa.RegisterInt64)
}

// handleSetElementInt64 handles the isa.OpSetElementInt64 instruction by writing an
// integer value to an element of a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetElementInt64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if setElementInt64WithoutReflect(vm.Arena, registers.General[instruction.A], registers.Ints[instruction.B], registers.Ints[instruction.C]) {
		return opContinue
	}
	return sliceSetViaReflect(vm, registers, instruction, isa.RegisterInt64)
}

// handleGetElementUint64 handles the isa.OpGetElementUint64 instruction by reading a uint
// element from a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetElementUint64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if element, plain := getElementUint64WithoutReflect(registers.General[instruction.B], registers.Ints[instruction.C]); plain {
		registers.Uints[instruction.A] = element
		return opContinue
	}
	return sliceGetViaReflect(vm, registers, instruction, isa.RegisterUint64)
}

// handleSetElementUint64 handles the isa.OpSetElementUint64 instruction by writing a uint
// value to an element of a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetElementUint64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if setElementUint64WithoutReflect(vm.Arena, registers.General[instruction.A], registers.Ints[instruction.B], registers.Uints[instruction.C]) {
		return opContinue
	}
	return sliceSetViaReflect(vm, registers, instruction, isa.RegisterUint64)
}

// handleGetElementFloat64 handles the isa.OpGetElementFloat64 instruction by reading a
// float element from a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetElementFloat64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if element, plain := getElementFloat64WithoutReflect(registers.General[instruction.B], registers.Ints[instruction.C]); plain {
		registers.Floats[instruction.A] = element
		return opContinue
	}
	return sliceGetViaReflect(vm, registers, instruction, isa.RegisterFloat64)
}

// handleSetElementFloat64 handles the isa.OpSetElementFloat64 instruction by writing a
// float value to an element of a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetElementFloat64(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if setElementFloat64WithoutReflect(vm.Arena, registers.General[instruction.A], registers.Ints[instruction.B], registers.Floats[instruction.C]) {
		return opContinue
	}
	return sliceSetViaReflect(vm, registers, instruction, isa.RegisterFloat64)
}

// handleGetElementString handles the isa.OpGetElementString instruction by reading a
// string element from a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetElementString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if element, plain := getElementStringWithoutReflect(registers.General[instruction.B], registers.Ints[instruction.C]); plain {
		registers.Strings[instruction.A] = element
		return opContinue
	}
	return sliceGetViaReflect(vm, registers, instruction, isa.RegisterString)
}

// handleSetElementString handles the isa.OpSetElementString instruction by writing a
// string value to an element of a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetElementString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if setElementStringWithoutReflect(vm.Arena, registers.General[instruction.A], registers.Ints[instruction.B], registers.Strings[instruction.C]) {
		return opContinue
	}
	return setElementStringViaReflect(vm, registers, instruction)
}

// handleGetElementBool handles the isa.OpGetElementBool instruction by reading a bool
// element from a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGetElementBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if element, plain := getElementBoolWithoutReflect(registers.General[instruction.B], registers.Ints[instruction.C]); plain {
		registers.Bools[instruction.A] = element
		return opContinue
	}
	return sliceGetViaReflect(vm, registers, instruction, isa.RegisterBool)
}

// handleSetElementBool handles the isa.OpSetElementBool instruction by writing a bool
// value to an element of a slice, an array or a pointer to an array.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetElementBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if setElementBoolWithoutReflect(vm.Arena, registers.General[instruction.A], registers.Ints[instruction.B], registers.Bools[instruction.C]) {
		return opContinue
	}
	return sliceSetViaReflect(vm, registers, instruction, isa.RegisterBool)
}

// sliceGetViaReflect is the reflect route every typed slice read shares, for an element
// its kind's reflection-free route declined: registers.<bank>[A] = collection[B][ints[C]]
// through resolveIndexCollection, the bounds check, Index and the reflect getter of the
// bank's kind.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and destination.
// Takes instruction (instruction) which encodes the operand indices.
// Takes bank (isa.RegisterKind) which is the destination: int, uint, float, string or
// bool.
//
// Returns OpResult indicating the next execution step.
func sliceGetViaReflect(vm *VM, registers *Registers, instruction isa.Instruction, bank isa.RegisterKind) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	element := unwrapInterfaceElement(collection.Index(index))
	switch bank {
	case isa.RegisterInt64:
		value, ok := readIntElement(vm, element)
		if !ok {
			return opPanicError
		}
		registers.Ints[instruction.A] = value
	case isa.RegisterUint64:
		registers.Uints[instruction.A] = element.Uint()
	case isa.RegisterFloat64:
		registers.Floats[instruction.A] = element.Float()
	case isa.RegisterString:
		registers.Strings[instruction.A] = element.String()
	case isa.RegisterBool:
		registers.Bools[instruction.A] = element.Bool()
	default:
		vm.evalError = newInvariantError("typed slice read into register bank %d", bank)
		return opPanicError
	}
	return opContinue
}

// sliceSetViaReflect writes a scalar typed slice element through reflection.
//
// It handles elements their kind's reflection-free route declined, computing
// collection[A][ints[B]] = registers.<bank>[C] through resolveIndexCollection, the bounds
// check, Index and the reflect setter of bank's kind. A scalar holds no reference, so no
// barrier applies. A string, which does, takes setElementStringViaReflect, so no scalar
// write reaches a string barrier.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
// Takes bank (isa.RegisterKind) which is the source, either int, uint, float or bool.
//
// Returns OpResult indicating the next execution step.
func sliceSetViaReflect(vm *VM, registers *Registers, instruction isa.Instruction, bank isa.RegisterKind) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return boundsResult
	}
	element := collection.Index(index)
	switch bank {
	case isa.RegisterInt64:
		if !writeIntElement(vm, element, registers.Ints[instruction.C]) {
			return opPanicError
		}
	case isa.RegisterUint64:
		element.SetUint(registers.Uints[instruction.C])
	case isa.RegisterFloat64:
		element.SetFloat(registers.Floats[instruction.C])
	case isa.RegisterBool:
		element.SetBool(registers.Bools[instruction.C])
	default:
		vm.evalError = newInvariantError("typed slice write from register bank %d", bank)
		return opPanicError
	}
	return opContinue
}

// setElementStringViaReflect is OpSetElementString's reflect route, for an element the
// reflection-free route declined, computing collection[A][ints[B]] = strings[C] through
// resolveIndexCollection, the bounds check, Index and SetString of the string the
// barrier, materialiseStringForIndexedStore, decides.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func setElementStringViaReflect(vm *VM, registers *Registers, instruction isa.Instruction) OpResult {
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

// readIntElement reads an integer element through reflect: a signed one as it is, an
// unsigned one reinterpreted.
//
// Takes vm (*VM) which records the invariant error for any other kind.
// Takes element (reflect.Value) which is the element.
//
// Returns int64 which is the element.
// Returns bool which is false for an element that is not an integer.
func readIntElement(vm *VM, element reflect.Value) (int64, bool) {
	switch {
	case isSignedIntKind(element.Kind()):
		return element.Int(), true
	case isUnsignedIntKind(element.Kind()):
		return safeconv.Uint64ToInt64Reinterpret(element.Uint()), true
	default:
		vm.evalError = newInvariantError("integer slice read on non-integer element (%s)", element.Kind())
		return 0, false
	}
}

// writeIntElement writes an integer element through reflect: SetInt for a signed one,
// SetUint of the reinterpreted value for an unsigned one.
//
// Takes vm (*VM) which records the invariant error for any other kind.
// Takes element (reflect.Value) which is the element.
// Takes value (int64) which is the value stored.
//
// Returns bool which is false for an element that is not an integer.
func writeIntElement(vm *VM, element reflect.Value, value int64) bool {
	switch {
	case isSignedIntKind(element.Kind()):
		element.SetInt(value)
	case isUnsignedIntKind(element.Kind()):
		element.SetUint(safeconv.Int64ToUint64Reinterpret(value))
	default:
		vm.evalError = newInvariantError("integer slice write on non-integer element (%s)", element.Kind())
		return false
	}
	return true
}
