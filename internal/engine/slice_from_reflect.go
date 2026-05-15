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
)

// sliceFromReflectField extracts a typed slice from a reflect.Value.
//
// Unlike reflect.TypeAssert it accepts any element type whose memory layout matches T's,
// and avoids Interface so it tolerates flagRO values from unexported fields.
//
// Takes field (reflect.Value) which is the slice value to extract.
//
// Returns the typed slice and true on success, or (nil, false) when the element layout
// does not match T or the value is not a slice.
func sliceFromReflectField[T any](field reflect.Value) ([]T, bool) {
	if field.Kind() != reflect.Slice {
		return nil, false
	}
	elemType := field.Type().Elem()
	var zero T
	zeroType := reflect.TypeOf(zero)
	if elemType != zeroType && elemType.Size() != zeroType.Size() {
		return nil, false
	}
	length := field.Len()
	capacity := field.Cap()
	if capacity == 0 {
		return nil, true
	}

	data := (*T)(field.UnsafePointer())
	return unsafe.Slice(data, capacity)[:length:capacity], true
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
