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
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// NativeParameterType resolves the declared type of the parameter that receives argument
// index, unfolding the variadic tail unless the call spreads a slice into it.
//
// Takes functionType (reflect.Type) which is the native function's type.
// Takes index (int) which is the argument position.
// Takes ellipsisSpread (bool) which reports whether the final argument is the whole
// variadic slice.
//
// Returns the parameter type and true, or nil and false when index is out of range.
func NativeParameterType(functionType reflect.Type, index int, ellipsisSpread bool) (reflect.Type, bool) {
	numIn := functionType.NumIn()
	if functionType.IsVariadic() && index >= numIn-1 {
		if ellipsisSpread {
			return functionType.In(numIn - 1), index == numIn-1
		}
		return functionType.In(numIn - 1).Elem(), true
	}
	if index >= numIn {
		return nil, false
	}
	return functionType.In(index), true
}

// describeInterpretedType renders the source-level name of an interpreted value for
// diagnostics, preferring the pipit type registry over the synthesised reflect name.
//
// Takes vm (*VM) which provides the type-name registry; may be nil.
// Takes value (reflect.Value) which is the value being described.
//
// Returns the type name to show the user.
func describeInterpretedType(vm *VM, value reflect.Value) string {
	if !value.IsValid() {
		return "<nil>"
	}
	if name, ok := pipitTypeName(vm, value); ok {
		if value.Kind() == reflect.Pointer {
			return "*" + name
		}
		return name
	}
	return typemodel.NamedScalarDisplayType(value.Type())
}

// checkNativeInterfaceArguments verifies that every argument bound for an interface-typed
// native parameter is assignable to it, so a pipit type whose method set no adapter
// covers fails with a named error rather than a reflect.Call panic.
//
// Takes vm (*VM) which provides the type-name registry for the message.
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the coerced arguments about to be passed.
// Takes ellipsisSpread (bool) which reports whether the final argument is the variadic
// slice itself rather than an element of it.
//
// Returns errUnsupportedInterfaceArgument naming the offending argument, or nil when
// every interface argument is assignable.
func checkNativeInterfaceArguments(vm *VM, reflectedFunction reflect.Value, arguments []reflect.Value, ellipsisSpread bool) error {
	functionType := reflectedFunction.Type()
	for index, argument := range arguments {
		expected, ok := NativeParameterType(functionType, index, ellipsisSpread)
		if !ok || expected.Kind() != reflect.Interface || expected.NumMethod() == 0 {
			continue
		}
		if !argument.IsValid() || argument.Type().AssignableTo(expected) {
			continue
		}
		return fmt.Errorf("%w: argument %d of %s has interpreted type %s which does not implement %s",
			fault.ErrUnsupportedInterfaceArgument, index, resolveNativeFunctionPath(reflectedFunction),
			describeInterpretedType(vm, argument), expected.String())
	}
	return nil
}

// checkInterfaceFieldValue verifies that a value about to be stored into an interface
// field is assignable to the field's type, so an interpreted type no adapter covers fails
// with a named error rather than a reflect.Set panic.
//
// Takes vm (*VM) which provides the type-name registry for the message.
// Takes value (reflect.Value) which is the (possibly adapter-wrapped) value to store.
// Takes fieldType (reflect.Type) which is the destination field's interface type.
//
// Returns errUnsupportedInterfaceField naming the value's type, or nil when assignable.
func checkInterfaceFieldValue(vm *VM, value reflect.Value, fieldType reflect.Type) error {
	if !value.IsValid() || fieldType.Kind() != reflect.Interface || fieldType.NumMethod() == 0 {
		return nil
	}
	if value.Type().AssignableTo(fieldType) {
		return nil
	}
	return fmt.Errorf("%w: interpreted type %s does not implement %s",
		fault.ErrUnsupportedInterfaceField, describeInterpretedType(vm, value), fieldType.String())
}
