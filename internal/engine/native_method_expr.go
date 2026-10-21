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

	"pipit.sh/pipit/internal/engine/program"
)

// fastPathTypeByTag is the function type of each tag, the inverse of fastPathTagByType.
var fastPathTypeByTag = func() (types [fastPathTagBytesArr64 + 1]reflect.Type) {
	for functionType, tag := range fastPathTagByType {
		types[tag] = functionType
	}
	return types
}()

// dispatchNativeMethodValue calls a method value through its tag's dispatcher.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
// Takes entry (*nativeFastPathEntry) which is the site's published entry.
// Takes method (reflect.Value) which is the method value register.
//
// Returns bool which is false when the method value's type is not the tag's, leaving the
// call to the full dispatch path.
// Returns any which is the call's recovered panic, or nil.
func dispatchNativeMethodValue(vm *VM, site *program.CallSite, registers *Registers, entry *nativeFastPathEntry, method reflect.Value) (bool, any) {
	if method.Type() != fastPathTypeByTag[entry.tag] {
		return false, nil
	}
	bound := method.Interface()
	return true, dispatchNativeFastPathTagged(vm, entry.tag, bound, site, registers)
}

// dispatchCachedNativeMethod dispatches a method through its cached fast path.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes site (*CallSite) which provides the call site metadata.
// Takes registers (*Registers) which holds the current register banks.
// Takes entry (*nativeFastPathEntry) which is the site's published entry.
// Takes current (reflect.Value) which is the method value register, valid.
//
// Returns OpResult which is the dispatch result.
// Returns bool which is false when the method value's type is not the tag's, or an
// argument does not fit the native's parameter, leaving the call to the full dispatch
// path.
func dispatchCachedNativeMethod(vm *VM, site *program.CallSite, registers *Registers, entry *nativeFastPathEntry, current reflect.Value) (OpResult, bool) {
	if !fastPathArgumentsFit(entry.tag, site, registers) {
		return opContinue, false
	}
	if receiver, ok := entry.method.receiverWord(current); ok {
		return cachedNativeFastPathResult(vm, dispatchNativeMethodTagged(vm, entry.tag, entry.method.expression, receiver, site, registers))
	}
	if shape, ok := buildNativeMethodShape(current, entry.tag); ok {
		if receiver, matched := shape.receiverWord(current); matched {
			reshaped := &nativeFastPathEntry{tag: entry.tag, method: shape, fn: nil}
			publishNativeFastPath(site, reshaped)
			return cachedNativeFastPathResult(vm, dispatchNativeMethodTagged(vm, reshaped.tag, shape.expression, receiver, site, registers))
		}
	}
	dispatched, panicValue := dispatchNativeMethodValue(vm, site, registers, entry, current)
	if !dispatched {
		return opContinue, false
	}
	return cachedNativeFastPathResult(vm, panicValue)
}

// cachedNativeFastPathResult turns a recovered native panic into an interpreted panic.
//
// Takes vm (*VM) which made the call.
// Takes panicValue (any) which is the recovered panic, or nil.
//
// Returns OpResult which is opContinue or the interpreted panic's result.
// Returns bool which is true because the fast path handled the call.
func cachedNativeFastPathResult(vm *VM, panicValue any) (OpResult, bool) {
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue), true
	}
	return opContinue, true
}

// newNativeMethodEntry builds the entry published on a method site's first fast-path
// call.
//
// Takes tag (nativeFastPathTag) which is the classified tag.
// Takes method (reflect.Value) which is the method value register.
//
// Returns *nativeFastPathEntry which is the entry. Its shape is empty when the method
// value has no direct path.
func newNativeMethodEntry(tag nativeFastPathTag, method reflect.Value) *nativeFastPathEntry {
	entry := &nativeFastPathEntry{tag: tag, fn: nil, method: nativeMethodShape{}}
	if shape, ok := buildNativeMethodShape(method, tag); ok {
		entry.method = shape
	}
	return entry
}
