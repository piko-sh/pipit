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
)

// runTinyLeafIncIntField implements the inlined body for the void `recv.fieldX++` shape
// on an int-kind field: the field is incremented in place through the receiver's struct
// base pointer, and receivers the unsafe path cannot crack fall back to
// tinyLeafMutateFallback.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries the receiver argument slot.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue after the increment, or the fallback's result.
func runTinyLeafIncIntField(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	recv := registers.General[site.Arguments[0].Register]
	if base, ok := structFieldUnsafeBaseFromValue(recv); ok {
		if incrementIntFieldInPlace(unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset)), reflect.Kind(callee.TinyLeafLayout.Kind)) {
			return opContinue
		}
	}
	return tinyLeafMutateFallback(vm, registers, site, callee, recv)
}

// runTinyLeafIncUintField is the uint-field sibling of runTinyLeafIncIntField.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries the receiver argument slot.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue after the increment, or the fallback's result.
func runTinyLeafIncUintField(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	recv := registers.General[site.Arguments[0].Register]
	if base, ok := structFieldUnsafeBaseFromValue(recv); ok {
		if incrementUintFieldInPlace(unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset)), reflect.Kind(callee.TinyLeafLayout.Kind)) {
			return opContinue
		}
	}
	return tinyLeafMutateFallback(vm, registers, site, callee, recv)
}

// runTinyLeafSetScalarFieldFromArg implements the inlined body for the void `recv.fieldX
// = argument` shape: the second call-site argument is read from the caller's scalar bank
// and stored through the receiver's struct base pointer with the width the field layout
// names.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries the receiver and value argument slots.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue after the store, or the fallback's result.
func runTinyLeafSetScalarFieldFromArg(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	recv := registers.General[site.Arguments[0].Register]
	kind := reflect.Kind(callee.TinyLeafLayout.Kind)
	argument, readable := tinyLeafArgumentValue(registers, site, kind)
	if !readable {
		return pushCompiledFrame(vm, registers, site, callee)
	}
	if base, ok := structFieldUnsafeBaseFromValue(recv); ok {
		if writeScalarFieldAt(unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset)), kind, argument) {
			return opContinue
		}
	}
	return tinyLeafMutateFallback(vm, registers, site, callee, recv)
}

// writeScalarFieldAt stores a scalar of kind at p, narrowing exactly as the tier-0 field
// set handlers do.
//
// Takes p (unsafe.Pointer) which addresses the field bytes.
// Takes kind (reflect.Kind) which selects the store width.
// Takes argument (tinyLeafScalarArgument) whose field matching kind is stored.
//
// Returns true when kind is a supported scalar kind and the store happened.
func writeScalarFieldAt(p unsafe.Pointer, kind reflect.Kind, argument tinyLeafScalarArgument) bool {
	intValue, uintValue, floatValue, boolValue := argument.intValue, argument.uintValue, argument.floatValue, argument.boolValue
	switch kind {
	case reflect.Int:
		*(*int)(p) = int(intValue)
	case reflect.Int8:
		*(*int8)(p) = int8(intValue) //nolint:gosec // matches SetInt truncation
	case reflect.Int16:
		*(*int16)(p) = int16(intValue) //nolint:gosec // matches SetInt truncation
	case reflect.Int32:
		*(*int32)(p) = int32(intValue) //nolint:gosec // matches SetInt truncation
	case reflect.Int64:
		*(*int64)(p) = intValue
	case reflect.Uint:
		*(*uint)(p) = uint(uintValue)
	case reflect.Uint8:
		*(*uint8)(p) = uint8(uintValue) //nolint:gosec // matches SetUint truncation
	case reflect.Uint16:
		*(*uint16)(p) = uint16(uintValue) //nolint:gosec // matches SetUint truncation
	case reflect.Uint32:
		*(*uint32)(p) = uint32(uintValue) //nolint:gosec // matches SetUint truncation
	case reflect.Uint64:
		*(*uint64)(p) = uintValue
	case reflect.Uintptr:
		*(*uintptr)(p) = uintptr(uintValue)
	case reflect.Float32:
		*(*float32)(p) = float32(floatValue)
	case reflect.Float64:
		*(*float64)(p) = floatValue
	case reflect.Bool:
		*(*bool)(p) = boolValue
	default:
		return false
	}
	return true
}
