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

	"pipit.sh/pipit/internal/fault"
)

// sliceFromReflectField extracts a typed slice from a reflect.Value.
//
// Unlike reflect.TypeAssert it accepts any element type that a typed bank stores as T,
// one of the same width and the same scalar class (a []int field read into the int64
// bank, or a named float type into the float64 bank), and it avoids Interface so it
// tolerates flagRO values from unexported fields. An element of another kind, such as a
// pointer read into an integer bank or a two-word struct read as a string header, is
// refused, so the bank never holds words of another type.
//
// Takes field (reflect.Value) which is the slice value to extract.
//
// Returns the typed slice and true on success, or (nil, false) when the element layout
// does not match T or the value is not a slice.
func sliceFromReflectField[T any](field reflect.Value) ([]T, bool) {
	if !field.IsValid() || field.Kind() != reflect.Slice || !storesAsBankElement(field.Type().Elem(), reflect.TypeFor[T]()) {
		return nil, false
	}
	if field.Cap() == 0 {
		return nil, true
	}
	return aliasSliceElements[T](field)
}

// storesAsBankElement reports whether a typed bank of element type bank can hold the
// elements of a slice of element type element in place. It accepts the same type, or the
// same width and the same scalar class.
//
// Takes element (reflect.Type) which is the slice's element type.
// Takes bank (reflect.Type) which is the bank's element type.
//
// Returns bool which is true when the elements can be shared without conversion.
func storesAsBankElement(element, bank reflect.Type) bool {
	if element == bank {
		return true
	}
	return element.Size() == bank.Size() && bankScalarClass(element.Kind()) == bankScalarClass(bank.Kind())
}

// readTypedSliceFieldFallback finishes a typed-slice struct field read when the unsafe
// pointer path declined the receiver. Raises an interpreted panic for an unconvertible
// field, because a width mismatch here is a broken compiler invariant.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the current register file.
// Takes receiver (uint8) which is the general register holding the struct.
// Takes field (reflect.Value) which is the reflect-walked field.
// Takes destination (*[]T) which is the bank register that receives the slice.
//
// Returns OpResult which is opContinue on success, or the fallback/panic result.
func readTypedSliceFieldFallback[T any](vm *VM, frame *CallFrame, registers *Registers, receiver uint8, field reflect.Value, destination *[]T) OpResult {
	if !field.IsValid() {
		return structFieldFallbackMiss(vm, frame, registers, receiver)
	}
	slice, ok := sliceFromReflectField[T](field)
	if !ok {
		var zero T
		return raiseNativePanicAsInterpreted(vm, fmt.Errorf("%w: struct field of type %s read as a []%T bank slice", fault.ErrTypeMismatch, field.Type(), zero))
	}
	*destination = slice
	return opContinue
}

// typedSliceOf returns value as a []T sharing its backing array.
//
// It accepts an exact []T or a slice of another type whose element type is T, such as a
// named slice type declared over []T, which a typed-slice bank holds as the same []T. Nil
// stays nil and an empty slice stays empty. A read-only value from an unexported field is
// read without Interface, which would panic on it.
//
// Takes value (reflect.Value) which is the candidate slice. It may be invalid.
//
// Returns []T which aliases value's elements.
// Returns bool which is false when value is not a slice of T.
func typedSliceOf[T any](value reflect.Value) ([]T, bool) {
	if !value.IsValid() || value.Kind() != reflect.Slice || value.Type().Elem() != reflect.TypeFor[T]() {
		return nil, false
	}
	if value.CanInterface() {
		if typed, ok := reflect.TypeAssert[[]T](value); ok {
			return typed, true
		}
	}
	if value.IsNil() {
		return nil, true
	}
	return aliasSliceElements[T](value)
}
