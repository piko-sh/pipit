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
	"runtime"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
)

const (
	// finaliserTinySize is the Go runtime's tiny-allocation size (maxTinySize). An object
	// without pointers and smaller than this may share a block, so runtime.SetFinalizer
	// accepts a pointer to it at any address in an allocation.
	finaliserTinySize = 16
)

var (
	// setFinaliserPointer identifies runtime.SetFinalizer among native callees.
	setFinaliserPointer = reflect.ValueOf(runtime.SetFinalizer).Pointer()

	// finaliserDropLogged is set once a finaliser registration on interpreter storage has
	// been logged, so the warning appears once per process.
	finaliserDropLogged atomic.Bool

	// finaliserDrops counts finaliser registrations dropped because their object was
	// interpreter storage, for tests and diagnostics.
	finaliserDrops atomic.Int64
)

// allocatesForFinalisers reports whether vm runs a program that calls
// runtime.SetFinalizer, in which every value whose address the program can take gets its
// own Go allocation.
//
// Takes vm (*VM) which is the allocating VM.
//
// Returns bool which is true when the program calls runtime.SetFinalizer.
func (vm *VM) allocatesForFinalisers() bool {
	return programSetsFinalisers(vm.rootFunction)
}

// bindFinaliserPlacement tells vm's arena, at the entry of a run, whether the running
// program calls runtime.SetFinalizer, for the arena routes that see no VM
// (heapSliceHeaders).
func (vm *VM) bindFinaliserPlacement() {
	if arena := vm.Arena; arena != nil {
		arena.heapSliceHeaders = vm.allocatesForFinalisers()
	}
}

// programSetsFinalisers reports whether root's program calls runtime.SetFinalizer or
// runtime.AddCleanup. It does when one is among the function constants of root or of a
// function in its table, or a call reached the finaliser intrinsic.
//
// The answer is recorded on root, so each function's constants are scanned once. When a
// session adds functions to the table, only root and the added functions are scanned.
//
// Takes root (*program.CompiledFunction) which is the program's root. Nil answers false.
//
// Returns bool which is true when the program calls it.
func programSetsFinalisers(root *program.CompiledFunction) bool {
	if root == nil {
		return false
	}
	functions := root.Functions
	sets, scanned := root.FinaliserScan()
	if sets {
		return true
	}
	if scanned == len(functions) {
		return false
	}
	start := 0
	if scanned > 0 && scanned < len(functions) {
		start = scanned
	}
	sets = constantsCallSetFinalizer(root)
	for _, function := range functions[start:] {
		sets = sets || constantsCallSetFinalizer(function)
	}
	root.RecordFinaliserUse(sets, len(functions))
	return sets
}

// constantsCallSetFinalizer reports whether runtime.SetFinalizer is among a function's
// general constants.
//
// Takes function (*program.CompiledFunction) which may be nil.
//
// Returns bool which is true when it is.
func constantsCallSetFinalizer(function *program.CompiledFunction) bool {
	if function == nil {
		return false
	}
	for _, constant := range function.GeneralConstants {
		if constant.IsValid() && constant.Kind() == reflect.Func && !constant.IsNil() {
			if pointer := constant.Pointer(); pointer == setFinaliserPointer || pointer == addCleanupSiblingPointer {
				return true
			}
		}
	}
	return false
}

// dispatchSetFinalizerIntrinsic runs runtime.SetFinalizer calls.
//
// A registration on interpreter storage cannot be passed on. Such a registration is
// dropped, the drop is counted and logged once per process, and the program is recorded
// as setting finalisers, so its later values are Go allocations. A clearing call (a nil
// finaliser) on such a value is consumed without a warning, since no finaliser is
// registered on it.
//
// Every other call is checked as runtime.SetFinalizer checks it (registerFinaliser). The
// real function ends the process for a call it refuses. The program gets a panic with the
// same message instead.
//
// Takes vm (*VM) which owns the arena and snapshot pools.
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the coerced operands.
//
// Returns the dispatch result and true when the callee is runtime.SetFinalizer.
func dispatchSetFinalizerIntrinsic(vm *VM, reflectedFunction reflect.Value, arguments []reflect.Value) (OpResult, bool) {
	if reflectedFunction.Pointer() != setFinaliserPointer || len(arguments) != 2 {
		return opContinue, false
	}
	if err := vm.registerFinaliser(arguments[0], arguments[1]); err != nil {
		return raiseNativePanicAsInterpreted(vm, err), true
	}
	return opContinue, true
}

// registerFinaliser performs runtime.SetFinalizer(object, finaliser) for the program.
//
// The checks follow runtime.SetFinalizer's, in its order. A nil finaliser clears the one
// set. The runtime is given a trampoline (finaliserTrampoline) that queues the finaliser
// for the finaliser worker, never the guest function itself.
//
// Takes object (reflect.Value) which is the first operand.
// Takes finaliser (reflect.Value) which is the second operand.
//
// Returns error which wraps errFinaliserMisuse with Go's message for a call Go refuses.
func (vm *VM) registerFinaliser(object reflect.Value, finaliser reflect.Value) error {
	if object.IsValid() && object.Kind() == reflect.Interface {
		object = object.Elem()
	}
	if err := checkFinaliserObject(object); err != nil {
		return err
	}
	if vm.ownsInterpreterStorage(object.UnsafePointer()) {
		vm.dropInterpreterStorageFinaliser(object, finaliser)
		return nil
	}
	if object.Type().Elem().Size() == 0 {
		return nil
	}
	clears := finaliserClears(finaliser)
	if !clears {
		if finaliser.Kind() == reflect.Interface {
			finaliser = finaliser.Elem()
		}
		if err := checkFinaliserFunction(object.Type(), finaliser.Type()); err != nil {
			return err
		}
	}
	return finaliserAllocations.update(object, func(state finaliserAllocationState) (bool, error) {
		return vm.applyFinaliser(state, object, finaliser, clears)
	})
}

// applyFinaliser makes a checked registration on object, whose record is in state, with
// the registry's lock held.
//
// Takes state (finaliserAllocationState) which is what the registry knows of object.
// Takes object (reflect.Value) which is the pointer.
// Takes finaliser (reflect.Value) which is the checked finaliser.
// Takes clears (bool) which is true when the call clears the finaliser.
//
// Returns bool which is true when a finaliser is now set on object.
// Returns error which wraps errFinaliserMisuse with Go's message for a call Go refuses.
func (vm *VM) applyFinaliser(state finaliserAllocationState, object, finaliser reflect.Value, clears bool) (bool, error) {
	switch {
	case state == finaliserAllocationUnknown && !finaliserTinyEligible(object.Type().Elem()):
		return false, finaliserMisuse("runtime.SetFinalizer: pointer not at beginning of allocated block")
	case clears:
		if state == finaliserAllocationSet {
			runtime.SetFinalizer(object.Interface(), nil)
		}
		return false, nil
	case state == finaliserAllocationSet:
		return true, finaliserMisuse("runtime.SetFinalizer: finalizer already set")
	default:
		trampoline := finaliserTrampoline(vm, vm.boundedFinaliserFunction(finaliser))
		runtime.SetFinalizer(object.Interface(), trampoline.Interface())
		return true, nil
	}
}

// finaliserTinyEligible reports whether runtime.SetFinalizer accepts a pointer to element
// anywhere in an allocation, not only at its start. The element holds no pointers and is
// smaller than the runtime's tiny-allocation size, so the object may share a block with
// others.
//
// Takes element (reflect.Type) which is the pointer's element type.
//
// Returns bool which is true for such an element.
func finaliserTinyEligible(element reflect.Type) bool {
	return element.Size() < finaliserTinySize && typeIsPointerFree(element)
}

// dropInterpreterStorageFinaliser drops a registration on interpreter storage, recording
// the program as one that sets finalisers.
//
// Takes object (reflect.Value) which points into interpreter storage.
// Takes finaliser (reflect.Value) which is the second operand.
func (vm *VM) dropInterpreterStorageFinaliser(object reflect.Value, finaliser reflect.Value) {
	if root := vm.rootFunction; root != nil {
		root.RecordFinaliserUse(true, len(root.Functions))
		vm.bindFinaliserPlacement()
	}
	if finaliserClears(finaliser) {
		return
	}
	finaliserDrops.Add(1)
	if finaliserDropLogged.CompareAndSwap(false, true) {
		_, logger := logging.From(vm.executionContext())
		logger.Warn(
			"runtime.SetFinalizer on interpreter storage registers no finaliser; values allocated after this call are Go allocations",
			"type", object.Type().String())
	}
}

// checkFinaliserObject checks runtime.SetFinalizer's first operand as the runtime does.
//
// Takes object (reflect.Value) which is the operand, out of its interface.
//
// Returns error which wraps errFinaliserMisuse unless object is a non-nil pointer.
func checkFinaliserObject(object reflect.Value) error {
	switch {
	case !object.IsValid():
		return finaliserMisuse("runtime.SetFinalizer: first argument is nil")
	case object.Kind() != reflect.Pointer:
		return finaliserMisuse("runtime.SetFinalizer: first argument is " + scriptTypeString(object.Type()) + ", not pointer")
	case object.IsNil():
		return finaliserMisuse("runtime.SetFinalizer: pointer not in allocated block")
	}
	return nil
}

// checkFinaliserFunction checks a runtime.SetFinalizer finaliser's type as the runtime
// does.
//
// The parameter must be the object's type, an empty interface, an interface the object's
// type implements, or a pointer type with the same element type when one of the two
// pointer types has no uncommon type. The last case is accepted only where it is certain,
// when the shared element type is unnamed, so that its unnamed pointer type has no
// methods. A case the runtime might accept but the check cannot confirm is refused, which
// raises a panic rather than ending the process.
//
// Takes objectType (reflect.Type) which is the object's pointer type.
// Takes finaliserType (reflect.Type) which is the finaliser's type.
//
// Returns error which wraps errFinaliserMisuse with Go's message when the runtime would
// refuse the pair. Types are named as the program names them (scriptTypeString).
func checkFinaliserFunction(objectType, finaliserType reflect.Type) error {
	if finaliserType.Kind() != reflect.Func {
		return finaliserMisuse("runtime.SetFinalizer: second argument is " + scriptTypeString(finaliserType) + ", not a function")
	}
	refused := "runtime.SetFinalizer: cannot pass " + scriptTypeString(objectType) + " to finalizer " + scriptTypeString(finaliserType)
	if finaliserType.IsVariadic() {
		return finaliserMisuse(refused + " because dotdotdot")
	}
	if finaliserType.NumIn() != 1 {
		return finaliserMisuse(refused)
	}
	parameter := finaliserType.In(0)
	switch {
	case parameter == objectType:
		return nil
	case parameter.Kind() == reflect.Pointer:
		if parameter.Elem() == objectType.Elem() && objectType.Elem().Name() == "" && (parameter.Name() == "" || objectType.Name() == "") {
			return nil
		}
	case parameter.Kind() == reflect.Interface:
		if parameter.NumMethod() == 0 || objectType.Implements(parameter) {
			return nil
		}
	}
	return finaliserMisuse(refused)
}

// finaliserMisuseError is the panic value of a runtime.SetFinalizer call Go refuses. Its
// message is Go's own.
type finaliserMisuseError struct {
	// message is the runtime's message.
	message string
}

// Error returns the runtime's message.
//
// Returns string which is the message.
func (e *finaliserMisuseError) Error() string {
	return e.message
}

// Unwrap returns errFinaliserMisuse.
//
// Returns error which is the sentinel.
func (*finaliserMisuseError) Unwrap() error {
	return errFinaliserMisuse
}

// finaliserMisuse returns the panic value for a refused runtime.SetFinalizer call.
//
// Takes message (string) which is the runtime's message.
//
// Returns error which wraps errFinaliserMisuse.
func finaliserMisuse(message string) error {
	return &finaliserMisuseError{message: message}
}

// finaliserClears reports whether a runtime.SetFinalizer finaliser argument is nil, which
// clears an object's finaliser.
//
// Takes finaliser (reflect.Value) which is the coerced second operand.
//
// Returns bool which is true for a nil or absent finaliser.
func finaliserClears(finaliser reflect.Value) bool {
	if finaliser.IsValid() && finaliser.Kind() == reflect.Interface {
		finaliser = finaliser.Elem()
	}
	return !finaliser.IsValid() || (finaliser.Kind() == reflect.Func && finaliser.IsNil())
}
