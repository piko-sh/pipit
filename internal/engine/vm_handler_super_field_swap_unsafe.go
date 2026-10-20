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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// swapStructFieldsGeneralUnsafe swaps two same-typed general-bank fields of the struct
// the receiver register points to, with runtime.typedmemmove so the GC barriers are kept.
//
// Takes vm (*VM) which provides the reflect fallback path.
// Takes frame (*CallFrame) which provides the layout and type tables.
// Takes registers (*Registers) which provides the struct register.
// Takes instruction (instruction) which encodes the operand indices.
// Takes layoutA (program.StructFieldLayout) which is the first field.
// Takes layoutB (program.StructFieldLayout) which is the second field.
// Takes fieldType (reflect.Type) which is the type both fields share.
//
// Returns opContinue, or the fallback's result when the receiver or kind does not fit.
func swapStructFieldsGeneralUnsafe(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, layoutA, layoutB program.StructFieldLayout, fieldType reflect.Type) OpResult {
	recvRaw := (*unsafeReflectValue)(unsafe.Pointer(new(registers.General[instruction.A])))
	if recvRaw.typ == nil || reflect.Kind(recvRaw.flag&flagKindMask) != reflect.Pointer {
		return swapStructFieldsGeneralReflectFallback(vm, frame, registers, instruction, layoutA, layoutB)
	}
	var base unsafe.Pointer
	if recvRaw.flag&flagIndir != 0 {
		base = *(*unsafe.Pointer)(recvRaw.ptr)
	} else {
		base = recvRaw.ptr
	}
	if base == nil {
		return swapStructFieldsGeneralReflectFallback(vm, frame, registers, instruction, layoutA, layoutB)
	}
	fieldPtrA := unsafe.Add(base, uintptr(layoutA.Offset))
	fieldPtrB := unsafe.Add(base, uintptr(layoutB.Offset))
	typeABI := reflectValueABIType(fieldType)
	switch reflect.Kind(layoutA.Kind) {
	case reflect.Interface:
		var temp [2]unsafe.Pointer
		temp = *(*[2]unsafe.Pointer)(fieldPtrA)
		runtimeTypedmemmove(typeABI, fieldPtrA, fieldPtrB)
		runtimeTypedmemmove(typeABI, fieldPtrB, unsafe.Pointer(&temp[0]))
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		var temp unsafe.Pointer
		temp = *(*unsafe.Pointer)(fieldPtrA)
		runtimeTypedmemmove(typeABI, fieldPtrA, fieldPtrB)
		runtimeTypedmemmove(typeABI, fieldPtrB, unsafe.Pointer(&temp))
	default:
		return swapStructFieldsGeneralReflectFallback(vm, frame, registers, instruction, layoutA, layoutB)
	}
	return opContinue
}
