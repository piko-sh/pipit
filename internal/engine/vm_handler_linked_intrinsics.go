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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/stdlink"
)

// addCleanupArguments is the number of arguments runtime.AddCleanup takes, including the
// pointer, the cleanup and the cleanup's argument.
const addCleanupArguments = 3

var (
	// errorsAsTypeSiblingPointer identifies stdlink.ErrorsAsType among linked siblings.
	errorsAsTypeSiblingPointer = reflect.ValueOf(stdlink.ErrorsAsType).Pointer()

	// reflectTypeAssertSiblingPointer identifies stdlink.ReflectTypeAssert.
	reflectTypeAssertSiblingPointer = reflect.ValueOf(stdlink.ReflectTypeAssert).Pointer()

	// reflectTypeForSiblingPointer identifies stdlink.ReflectTypeFor.
	reflectTypeForSiblingPointer = reflect.ValueOf(stdlink.ReflectTypeFor).Pointer()

	// addCleanupSiblingPointer identifies stdlink.RuntimeAddCleanup, which also marks a
	// program as one whose values get Go allocations (programSetsFinalisers).
	addCleanupSiblingPointer = reflect.ValueOf(stdlink.RuntimeAddCleanup).Pointer()

	// cleanupFunctionType is the type a runtime.AddCleanup cleanup is converted to.
	cleanupFunctionType = reflect.TypeFor[func(any)]()

	// cleanupArgumentType is the type of a cleanup's argument as the cleanup receives it.
	cleanupArgumentType = reflect.TypeFor[any]()

	// linkedIntrinsics maps each intrinsic sibling to the handler that runs it. It is filled
	// by init, because the handlers reach code that reaches this table.
	linkedIntrinsics map[uintptr]linkedIntrinsic
)

// linkedIntrinsic is one linked sibling the interpreter runs itself.
type linkedIntrinsic struct {
	// run runs the sibling.
	run func(vm *VM, typeArguments []reflect.Type, arguments []reflect.Value) []reflect.Value

	// typeArguments is the number of type arguments the call site must carry.
	typeArguments int

	// arguments is the number of arguments the call must pass.
	arguments int
}

func init() {
	linkedIntrinsics = map[uintptr]linkedIntrinsic{
		errorsAsTypeSiblingPointer:      {typeArguments: 1, arguments: 1, run: runLinkedErrorsAsType},
		reflectTypeAssertSiblingPointer: {typeArguments: 1, arguments: 1, run: runLinkedReflectTypeAssert},
		reflectTypeForSiblingPointer:    {typeArguments: 1, arguments: 0, run: runLinkedReflectTypeFor},
		addCleanupSiblingPointer:        {typeArguments: 2, arguments: addCleanupArguments, run: linkedAddCleanup},
	}
}

// dispatchLinkedIntrinsic runs the linked siblings whose meaning depends on script types
// or interpreter storage in place of their plain Go versions.
//
// Takes vm (*VM) which owns the method tables and storage.
// Takes site (*program.CallSite) which carries the type arguments.
// Takes sibling (reflect.Value) which is the linked sibling.
// Takes arguments ([]reflect.Value) which are the call's own arguments, prepared.
//
// Returns the results, any panic to raise in the program, and false when sibling is not
// an intrinsic. A call site with too few type arguments or arguments, which only
// malformed bytecode produces, is handled with a panic value wrapping
// errLinkedIntrinsicShape.
func dispatchLinkedIntrinsic(vm *VM, site *program.CallSite, sibling reflect.Value, arguments []reflect.Value) (results []reflect.Value, panicValue any, handled bool) {
	intrinsic, ok := linkedIntrinsics[sibling.Pointer()]
	if !ok {
		return nil, nil, false
	}
	if len(site.LinkedTypeArgs) < intrinsic.typeArguments || len(arguments) < intrinsic.arguments {
		return nil, fmt.Errorf("%w: %d type arguments and %d arguments", errLinkedIntrinsicShape, len(site.LinkedTypeArgs), len(arguments)), true
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			results, panicValue, handled = nil, recovered, true
		}
	}()
	return intrinsic.run(vm, site.LinkedTypeArgs, arguments), nil, true
}

// runLinkedErrorsAsType runs errors.AsType[E](err).
//
// Takes vm (*VM) which owns the method tables.
// Takes typeArguments ([]reflect.Type) which holds E.
// Takes arguments ([]reflect.Value) which holds err.
//
// Returns []reflect.Value which are E and the found flag.
func runLinkedErrorsAsType(vm *VM, typeArguments []reflect.Type, arguments []reflect.Value) []reflect.Value {
	return linkedErrorsAsType(vm, typeArguments[0], arguments[0])
}

// runLinkedReflectTypeAssert runs reflect.TypeAssert[T](v).
//
// Takes vm (*VM) which owns the method tables.
// Takes typeArguments ([]reflect.Type) which holds T.
// Takes arguments ([]reflect.Value) which holds v.
//
// Returns []reflect.Value which are the T and the ok flag.
func runLinkedReflectTypeAssert(vm *VM, typeArguments []reflect.Type, arguments []reflect.Value) []reflect.Value {
	return linkedReflectTypeAssert(vm, typeArguments[0], arguments[0])
}

// runLinkedReflectTypeFor runs reflect.TypeFor[T]().
//
// Takes typeArguments ([]reflect.Type) which holds T.
//
// Returns []reflect.Value which holds the reflect.Type.
func runLinkedReflectTypeFor(_ *VM, typeArguments []reflect.Type, _ []reflect.Value) []reflect.Value {
	return []reflect.Value{reflect.ValueOf(namedScalarDisplayReflectType(typeArguments[0]))}
}

// linkedErrorsAsType is errors.AsType[E](err) over the program's method tables.
//
// Takes vm (*VM) which owns the method tables.
// Takes errorType (reflect.Type) which is E.
// Takes err (reflect.Value) which is the error searched.
//
// Returns the E found (or E's zero value) and whether one was.
func linkedErrorsAsType(vm *VM, errorType reflect.Type, err reflect.Value) []reflect.Value {
	target := reflect.New(errorType)
	found := pipitErrorsAs(vm, err, target)
	return []reflect.Value{target.Elem(), reflect.ValueOf(found)}
}

// linkedReflectTypeAssert is reflect.TypeAssert[T](v), that is v.Interface().(T), with a
// script value satisfying an interface T through its script methods as a type assertion
// does.
//
// Takes vm (*VM) which owns the method tables.
// Takes assertedType (reflect.Type) which is T.
// Takes argument (reflect.Value) which holds v.
//
// Returns the value as a T (T's zero value on failure) and whether the assertion held.
//
// Panics as v.Interface() does for the zero Value.
func linkedReflectTypeAssert(vm *VM, assertedType reflect.Type, argument reflect.Value) []reflect.Value {
	value, ok := reflect.TypeAssert[reflect.Value](argument)
	if !ok {
		panic(fmt.Sprintf("reflect.TypeAssert: argument is %s, not reflect.Value", argument.Type()))
	}
	result := reflect.New(assertedType).Elem()
	dynamic := reflect.ValueOf(value.Interface())
	matched := false
	if dynamic.IsValid() {
		if assertedType.Kind() == reflect.Interface {
			switch {
			case dynamic.Type().Implements(assertedType):
				matched = true
			case sourceImplementsAllMethods(vm, dynamic, reflectInterfaceRequirements(assertedType)):
				dynamic = coerceReflectArgument(vm, dynamic, assertedType, argumentTypeContext{staticTypeName: "", staticTypeString: "", keepClosures: false, skipInterfaceAdapter: false})
				matched = dynamic.IsValid() && dynamic.Type().AssignableTo(assertedType)
			}
		} else {
			dynamic, matched = matchTypeAssertion(dynamic, assertedType)
		}
	}
	if matched {
		result.Set(dynamic)
	}
	return []reflect.Value{result, reflect.ValueOf(matched)}
}

// reflectInterfaceRequirements lists an interface's methods as encoded requirements, by
// name and arity, for sourceImplementsAllMethods.
//
// Takes interfaceType (reflect.Type) which is a native interface type.
//
// Returns []string which is one requirement per method.
func reflectInterfaceRequirements(interfaceType reflect.Type) []string {
	required := make([]string, 0, interfaceType.NumMethod())
	for method := range interfaceType.Methods() {
		required = append(required, program.EncodeInterfaceMethodRequirementCounts(method.Name, method.Type.NumIn(), method.Type.NumOut()))
	}
	return required
}

// linkedAddCleanup is runtime.AddCleanup[T, S](ptr, cleanup, arg) for script code.
//
// Values of a program that calls AddCleanup are Go allocations (programSetsFinalisers),
// so a pointer into interpreter storage can only come from a value placed before the
// program was known to attach cleanups. The arena may reuse that storage, so no cleanup
// can be attached to it, and the call panics rather than dropping the cleanup. The
// program is recorded as attaching cleanups, so its later values are Go allocations.
//
// The runtime calls the cleanup on its own cleanup goroutines. It is given a trampoline
// (cleanupTrampoline) that queues the call for the finaliser worker, which runs a script
// cleanup under the finaliser deadline with its panics recovered.
//
// Takes vm (*VM) which owns the storage and runs the closure.
// Takes typeArguments ([]reflect.Type) which are T and S.
// Takes arguments ([]reflect.Value) which are ptr, cleanup and arg.
//
// Returns the runtime.Cleanup.
//
// Panics for a pointer into interpreter storage, for a cleanup that is not a function,
// and as runtime.AddCleanup does.
func linkedAddCleanup(vm *VM, typeArguments []reflect.Type, arguments []reflect.Value) []reflect.Value {
	if root := vm.rootFunction; root != nil {
		root.RecordFinaliserUse(true, len(root.Functions))
		vm.bindFinaliserPlacement()
	}
	pointer := arguments[0]
	if pointer.IsValid() && pointer.Kind() == reflect.Interface {
		pointer = pointer.Elem()
	}
	if pointer.IsValid() && pointer.Kind() == reflect.Pointer && !pointer.IsNil() && vm.ownsInterpreterStorage(pointer.UnsafePointer()) {
		panic(fmt.Errorf("%w: runtime.AddCleanup on a *%s", errCleanupOnInterpreterStorage, typeArguments[0]))
	}
	cleanup := coerceReflectArgument(vm, arguments[1], cleanupFunctionType, argumentTypeContext{staticTypeName: "", staticTypeString: "", keepClosures: false, skipInterfaceAdapter: false})
	if cleanup.IsValid() && cleanup.Kind() == reflect.Interface {
		cleanup = cleanup.Elem()
	}
	if !cleanup.IsValid() || cleanup.Type() != cleanupFunctionType || cleanup.IsNil() {
		panic(fmt.Errorf("%w: runtime.AddCleanup cleanup of type %s", errCleanupNotFunction, describeValueType(arguments[1])))
	}
	var argument any
	if arguments[2].IsValid() {
		argument = arguments[2].Interface()
	}
	var pointerValue any
	if pointer.IsValid() {
		pointerValue = pointer.Interface()
	}
	trampoline := cleanupTrampoline(vm, vm.boundedFinaliserFunction(cleanup))
	handle := stdlink.RuntimeAddCleanup(typeArguments[0], typeArguments[1], pointerValue, trampoline, argument)
	return []reflect.Value{reflect.ValueOf(handle)}
}

// describeValueType names a value's type for a message, or "nil" for the zero Value.
//
// Takes value (reflect.Value) which is the value.
//
// Returns string which is the type's name.
func describeValueType(value reflect.Value) string {
	if !value.IsValid() {
		return "nil"
	}
	return value.Type().String()
}
