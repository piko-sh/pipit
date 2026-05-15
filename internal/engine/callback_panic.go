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

import "fmt"

// callbackBoundaryMarker separates a callback VM's frames from the parent frames that
// invoked the native code the callback came back through.
const callbackBoundaryMarker = "--- callback boundary: the frames above ran in a callback VM invoked by native code called from the frame below ---"

// callbackPanic carries a callback VM's panic value together with its rendered
// interpreted stack across the Go frames between the callback and the parent VM. Without
// the wrapper the callback's frames would be lost when the parent's recovery reports the
// panic.
type callbackPanic struct {
	// value is the panic value as the callback raised it.
	value any

	// stack is the callback VM's rendered interpreted stack, "" when it recorded none.
	stack string
}

// Error renders the wrapped value so a Go-level recover that only formats the panic still
// prints the program's message.
//
// Returns string which is the wrapped value's text.
func (p *callbackPanic) Error() string {
	return fmt.Sprint(p.value)
}

// wrapCallbackPanic packages a failed callback VM's panic for re-raising in the parent.
//
// Takes child (*VM) which ran the callback and holds the panic value and the error that
// carries its stack.
//
// Returns any which is the value to panic with; the child's own panic value when the
// child recorded no stack.
func wrapCallbackPanic(child *VM) any {
	stack := UncaughtPanicStack(child.evalError)
	if stack == "" {
		return child.panicValue
	}
	return &callbackPanic{value: child.panicValue, stack: stack}
}

// unwrapCallbackPanic separates a recovered Go panic into the program's panic value and
// the callback stack it may carry.
//
// Takes recovered (any) which is the value Go's recover() returned.
//
// Returns any which is the program's panic value.
// Returns string which is the callback VM's stack, "" when the panic did not cross a
// callback boundary.
func unwrapCallbackPanic(recovered any) (any, string) {
	if nested, ok := recovered.(*callbackPanic); ok {
		return nested.value, nested.stack
	}
	return recovered, ""
}

// recordRecoveredPanic stores a recovered Go panic as the VM's current panic value,
// keeping the callback stack it carries for the uncaught-panic report.
//
// Takes recovered (any) which is the value Go's recover() returned; nil becomes a
// runtime.PanicNilError by the callers that need it.
func (vm *VM) recordRecoveredPanic(recovered any) {
	value, callbackStack := unwrapCallbackPanic(recovered)
	vm.panicValue = MaterialiseAnyForArena(vm.Arena, value)
	vm.callbackPanicStack = callbackStack
}

// panicStackWithCallback renders the current panic's frames, preceded by the frames of
// the callback VM the panic came from when it crossed a callback boundary.
//
// Returns string which is the combined stack, innermost frames first.
func (vm *VM) panicStackWithCallback() string {
	stack := vm.panicStack()
	if vm.callbackPanicStack == "" {
		return stack
	}
	return vm.callbackPanicStack + "\n" + callbackBoundaryMarker + "\n" + stack
}
