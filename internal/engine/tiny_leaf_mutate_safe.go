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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// runTinyLeafIncIntField implements the inlined body for the void `recv.fieldX++` shape
// on an int-kind field through reflect; receivers whose field cannot be set fall back to
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
	if field, ok := tinyLeafSettableField(recv, callee.TinyLeafLayout); ok {
		field.SetInt(field.Int() + 1)
		return opContinue
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
	if field, ok := tinyLeafSettableField(recv, callee.TinyLeafLayout); ok {
		field.SetUint(field.Uint() + 1)
		return opContinue
	}
	return tinyLeafMutateFallback(vm, registers, site, callee, recv)
}

// runTinyLeafSetScalarFieldFromArg implements the inlined body for the void `recv.fieldX
// = argument` shape through reflect.
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
	field, ok := tinyLeafSettableField(recv, callee.TinyLeafLayout)
	if !ok {
		return tinyLeafMutateFallback(vm, registers, site, callee, recv)
	}
	switch tinyLeafScalarFieldBank(kind) {
	case isa.RegisterInt:
		field.SetInt(argument.intValue)
	case isa.RegisterUint:
		field.SetUint(argument.uintValue)
	case isa.RegisterFloat:
		field.SetFloat(argument.floatValue)
	default:
		field.SetBool(argument.boolValue)
	}
	return opContinue
}

// tinyLeafSettableField walks the receiver to the leaf field and reports whether it can
// be written through reflect.
//
// Takes recv (reflect.Value) which is the receiver value.
// Takes layout (StructFieldLayout) which carries the field path.
//
// Returns the settable field and true, or false when the field cannot be written.
func tinyLeafSettableField(recv reflect.Value, layout program.StructFieldLayout) (reflect.Value, bool) {
	field, ok := tinyLeafReflectField(recv, layout)
	if !ok || !field.CanSet() {
		return reflect.Value{}, false
	}
	return field, true
}
