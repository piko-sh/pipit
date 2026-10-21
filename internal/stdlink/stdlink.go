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

package stdlink

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"

	"pipit.sh/pipit/internal/fault"
)

// ReflectTypeFor is the sibling of reflect.TypeFor[T]().
//
// Takes typeArgument (reflect.Type) which is T.
//
// Returns reflect.Type which is T itself.
func ReflectTypeFor(typeArgument reflect.Type) reflect.Type {
	return typeArgument
}

// ReflectTypeAssert is the sibling of reflect.TypeAssert[T](v), which is
// v.Interface().(T) without the intermediate interface value.
//
// Takes typeArgument (reflect.Type) which is T.
// Takes value (reflect.Value) which is v.
//
// Returns reflect.Value which holds the value as a T (T's zero value on failure).
// Returns bool which reports whether the assertion held.
func ReflectTypeAssert(typeArgument reflect.Type, value reflect.Value) (reflect.Value, bool) {
	dynamic := reflect.ValueOf(value.Interface())
	result := reflect.New(typeArgument).Elem()
	if !dynamic.IsValid() {
		return result, false
	}
	if typeArgument.Kind() == reflect.Interface {
		if !dynamic.Type().Implements(typeArgument) {
			return result, false
		}
		result.Set(dynamic)
		return result, true
	}
	if dynamic.Type() != typeArgument {
		return result, false
	}
	result.Set(dynamic)
	return result, true
}

// ErrorsAsType is the sibling of errors.AsType[E](err).
//
// Takes typeArgument (reflect.Type) which is E.
// Takes err (error) which is the error whose chain is searched.
//
// Returns reflect.Value which holds the first error in the chain that is an E (E's zero
// value when none is).
// Returns bool which reports whether one was found.
func ErrorsAsType(typeArgument reflect.Type, err error) (reflect.Value, bool) {
	target := reflect.New(typeArgument)
	if err == nil {
		return target.Elem(), false
	}
	found := errors.As(err, target.Interface())
	return target.Elem(), found
}

// RuntimeAddCleanup is the sibling of runtime.AddCleanup[T, S](ptr, cleanup, arg).
//
// The cleanup is attached to the object ptr points into, which is what AddCleanup does
// for any T. Cleanups may be attached to interior pointers, so the object is found from
// the address alone.
//
// Takes pointerType (reflect.Type) which is T.
// Takes argumentType (reflect.Type) which is S.
// Takes pointer (any) which is ptr.
// Takes cleanup (func(any)) which is the cleanup function.
// Takes argument (any) which is arg.
//
// Returns runtime.Cleanup which can stop the cleanup.
//
// Panics when ptr is not a non-nil pointer or arg equals ptr, as AddCleanup does.
func RuntimeAddCleanup(pointerType, argumentType reflect.Type, pointer any, cleanup func(any), argument any) runtime.Cleanup {
	address := cleanupAddress(pointerType, pointer)
	if argumentType.Kind() == reflect.Pointer {
		if argumentValue := reflect.ValueOf(argument); argumentValue.IsValid() && argumentValue.Kind() == reflect.Pointer && (*byte)(argumentValue.UnsafePointer()) == address {
			panic(fault.ErrCleanupArgumentIsPointer)
		}
	}
	return runtime.AddCleanup(address, cleanup, argument)
}

// RandN is the sibling of math/rand/v2's generic N[Int](n), drawing from the top-level
// source.
//
// The result is converted back to typeArgument rather than returned as the basic type it
// was drawn at, because N's constraint is written with ~ and so admits named types.
//
// Takes typeArgument (reflect.Type) which is Int.
// Takes bound (any) which is n, the exclusive upper bound.
//
// Returns any which is the pseudo-random value, typed as typeArgument.
//
// Panics when bound is not positive, as N does.
func RandN(typeArgument reflect.Type, bound any) any {
	value := reflect.ValueOf(bound)
	if !value.IsValid() {
		panic(fmt.Errorf("%w: math/rand/v2.N called with an invalid bound", fault.ErrTypeArgument))
	}
	var drawn reflect.Value
	switch typeArgument.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		drawn = reflect.ValueOf(rand.N(value.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		drawn = reflect.ValueOf(rand.N(value.Uint()))
	default:
		panic(fmt.Errorf("%w: math/rand/v2.N instantiated at %s", fault.ErrTypeArgument, typeArgument))
	}
	return drawn.Convert(typeArgument).Interface()
}

// cleanupAddress returns the address a runtime.AddCleanup ptr holds.
//
// Takes pointerType (reflect.Type) which is T, for the message.
// Takes pointer (any) which is ptr.
//
// Returns *byte which points at the address, so that AddCleanup finds the object.
//
// Panics when ptr is not a non-nil pointer.
func cleanupAddress(pointerType reflect.Type, pointer any) *byte {
	value := reflect.ValueOf(pointer)
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		panic(fmt.Errorf("%w: *%s", fault.ErrCleanupPointer, pointerType))
	}
	return (*byte)(value.UnsafePointer())
}
