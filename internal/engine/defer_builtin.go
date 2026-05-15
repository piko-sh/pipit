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
	"runtime"

	"pipit.sh/pipit/internal/isa"
)

// executeBuiltinDeferredCall runs a deferred (or `go`) builtin. A Go panic raised by the
// builtin (closing a closed channel, copying mismatched slices) becomes an interpreted
// panic exactly as a native deferred call's would.
//
// Takes call (deferredCall) which carries the builtin identifier and its arguments.
func (vm *VM) executeBuiltinDeferredCall(call deferredCall) {
	defer func() {
		if recovered := recover(); recovered != nil {
			vm.recordRecoveredPanic(recovered)
			vm.panicUnwound = nil
			vm.panicking = true
		}
	}()
	switch call.builtin {
	case isa.BuiltinPrint:
		execBuiltinPrint(vm, reflectArgsToAny(call.arguments), false)
	case isa.BuiltinPrintln:
		execBuiltinPrint(vm, reflectArgsToAny(call.arguments), true)
	case isa.BuiltinClose:
		if len(call.arguments) == 1 && call.arguments[0].IsValid() {
			call.arguments[0].Close()
		}
	case isa.BuiltinDelete:
		deferredMapDelete(vm, call.arguments)
	case isa.BuiltinPanic:
		vm.panicValue = deferredPanicValue(vm, call.arguments)
		vm.panicUnwound = nil
		vm.panicking = true
	case isa.BuiltinCopy:
		if len(call.arguments) == 2 && call.arguments[0].IsValid() && call.arguments[1].IsValid() {
			source := coerceValue(vm, call.arguments[1], call.arguments[0].Type())
			reflect.Copy(call.arguments[0], source)
		}
	case isa.BuiltinClear:
		if len(call.arguments) == 1 && call.arguments[0].IsValid() && !call.arguments[0].IsNil() {
			call.arguments[0].Clear()
		}
	default:
	}
}

// registerBuiltinDefer pushes a deferred builtin call: `defer close(ch)`, `defer
// println(x)` and the other builtins the spec allows in a defer statement. The arguments
// are evaluated now, as for any defer, and the builtin itself runs when the frame returns
// or unwinds.
//
// Takes vm (*VM) which owns the defer stack.
// Takes frame (*CallFrame) which supplies the argument extension words.
// Takes registers (*Registers) which holds the argument values.
// Takes builtin (uint8) which is the isa.Builtin* identifier.
// Takes argumentCount (int) which is the number of extension words to read.
//
// Returns opContinue, or opPanicError when the defer stack is exhausted.
func registerBuiltinDefer(vm *VM, frame *CallFrame, registers *Registers, builtin uint8, argumentCount int) OpResult {
	if len(vm.deferStack) >= maxDeferStackSize {
		vm.evalError = fmt.Errorf("%w: %d entries", errDeferStackExhausted, maxDeferStackSize)
		return opPanicError
	}
	arguments := unpackReflectArgs(frame, registers, argumentCount)
	materialiseReflectStringArgs(vm.Arena, arguments)
	materialiseReflectDeferArgs(vm.Arena, arguments)
	vm.deferStack = append(vm.deferStack, deferredCall{Function: nil, direct: nil, nativeFunction: reflect.Value{},
		arguments: arguments, frameIndex: vm.FramePointer, started: false, builtin: builtin, spread: false})
	return opContinue
}

// reflectArgsToAny unwraps deferred argument values for the print builtins.
//
// Takes arguments ([]reflect.Value) which are the evaluated arguments.
//
// Returns the same values as interfaces, with invalid values as nil.
func reflectArgsToAny(arguments []reflect.Value) []any {
	out := make([]any, len(arguments))
	for i, argument := range arguments {
		if argument.IsValid() {
			out[i] = argument.Interface()
		}
	}
	return out
}

// deferredMapDelete performs `delete(m, key)` for a deferred call; deleting from a nil
// map is a no-op, as in Go.
//
// Takes vm (*VM) which supplies the key conversion context.
// Takes arguments ([]reflect.Value) which hold the map and the key.
func deferredMapDelete(vm *VM, arguments []reflect.Value) {
	if len(arguments) != 2 {
		return
	}
	m := arguments[0]
	if !m.IsValid() || m.Kind() != reflect.Map || m.IsNil() {
		return
	}
	m.SetMapIndex(convertMapKey(vm, arguments[1], m.Type().Key()), reflect.Value{})
}

// deferredPanicValue materialises the value of a deferred `panic(v)`; `panic(nil)` yields
// *runtime.PanicNilError as in Go.
//
// Takes vm (*VM) which owns the arena the value may live in.
// Takes arguments ([]reflect.Value) which hold the panic value.
//
// Returns the value to panic with.
func deferredPanicValue(vm *VM, arguments []reflect.Value) any {
	if len(arguments) != 1 || !arguments[0].IsValid() {
		return new(runtime.PanicNilError)
	}
	return MaterialiseArenaValue(vm.Arena, arguments[0]).Interface()
}
