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

// Package link exposes the sentinel types used to bridge Go generic functions into
// pipit's interpreter.
//
// Go monomorphises generics at compile time, so the machine code for GetData[Post] only
// exists if the compiler saw that instantiation. When the interpreter executes source
// calling a generic function with a user-defined type, the specialised code is absent. A
// //piko:link directive declares a non-generic sibling accepting the instantiated types
// as prepended reflect.Type arguments; the extract tool wraps the sibling in a
// [LinkedFunction] during symbol generation, and the interpreter dispatches through it.
//
// Every declaration here aliases pipit.sh/pipit/internal/link, which owns them. Aliases
// are the same type, so the reflect.Type identity the interpreter matches on is preserved
// whichever path a caller imports.
package link

import (
	"reflect"

	"pipit.sh/pipit/internal/link"
)

const (
	// FieldKindBasic is a field of a predeclared Go type.
	FieldKindBasic = link.FieldKindBasic

	// FieldKindTypeArg is a field whose type is one of the generic's type parameters.
	FieldKindTypeArg = link.FieldKindTypeArg

	// FieldKindSlice is a slice field.
	FieldKindSlice = link.FieldKindSlice

	// FieldKindArray is a fixed-length array field.
	FieldKindArray = link.FieldKindArray

	// FieldKindMap is a map field.
	FieldKindMap = link.FieldKindMap

	// FieldKindPointer is a pointer field.
	FieldKindPointer = link.FieldKindPointer

	// FieldKindChan is a channel field.
	FieldKindChan = link.FieldKindChan

	// FieldKindInterface is an interface field.
	FieldKindInterface = link.FieldKindInterface

	// FieldKindNamed is a field of a named non-generic type.
	FieldKindNamed = link.FieldKindNamed

	// FieldKindNamedGeneric is a field of a named generic type.
	FieldKindNamedGeneric = link.FieldKindNamedGeneric

	// FieldKindError is a field of the builtin error type.
	FieldKindError = link.FieldKindError

	// MethodSymbolSeparator joins a receiver type name to a method name in a symbol key.
	MethodSymbolSeparator = link.MethodSymbolSeparator
)

// GenericFieldKind classifies one field of a linked generic type.
type GenericFieldKind = link.GenericFieldKind

// GenericFieldType describes the type of one field of a linked generic type.
type GenericFieldType = link.GenericFieldType

// GenericField is one named field of a linked generic type.
type GenericField = link.GenericField

// LinkedGenericType describes a generic type whose shape the interpreter rebuilds.
type LinkedGenericType = link.LinkedGenericType

// NativeBackedGenericType describes a generic type erased to a native implementation.
type NativeBackedGenericType = link.NativeBackedGenericType

// LinkedFunction pairs a non-generic sibling with the generic shape it stands in for.
type LinkedFunction = link.LinkedFunction

// LinkedMethod pairs a non-generic sibling with the generic method it stands in for.
type LinkedMethod = link.LinkedMethod

// Wrap constructs a LinkedFunction for the sibling captured at codegen time.
//
// Takes typeArgCount (int) which is the number of type parameters on the generic source.
// Takes target (any) which is the sibling function value.
//
// Returns LinkedFunction ready to be stored in the interpreter's symbol map.
func Wrap(typeArgCount int, target any) LinkedFunction { return link.Wrap(typeArgCount, target) }

// WrapFunc constructs a LinkedFunction with explicit parameter and return descriptors.
//
// Takes typeArgCount (int) which is the number of type parameters on the generic source.
// Takes target (any) which is the sibling function value.
// Takes params ([]GenericFieldType) which describe the generic's arguments in order.
// Takes results ([]GenericFieldType) which describe the generic's returns in order.
// Takes variadic (bool) which mirrors the generic's IsVariadic flag.
//
// Returns LinkedFunction ready to be stored in the interpreter's symbol map.
func WrapFunc(typeArgCount int, target any, params, results []GenericFieldType, variadic bool) LinkedFunction {
	return link.WrapFunc(typeArgCount, target, params, results, variadic)
}

// WrapMethod constructs a LinkedMethod for a sibling captured at registration time.
//
// Takes typeArgCount (int) which is the number of type parameters the method declares.
// Takes target (any) which is the sibling function value.
//
// Returns LinkedMethod ready to be stored under the key MethodSymbolKey builds.
func WrapMethod(typeArgCount int, target any) LinkedMethod {
	return link.WrapMethod(typeArgCount, target)
}

// WrapMethodFunc constructs a LinkedMethod with explicit parameter and return
// descriptors.
//
// Takes typeArgCount (int) which is the number of type parameters the method declares.
// Takes target (any) which is the sibling function value.
// Takes params ([]GenericFieldType) which describe the method's arguments in order.
// Takes results ([]GenericFieldType) which describe the method's returns in order.
// Takes variadic (bool) which mirrors the method's IsVariadic flag.
//
// Returns LinkedMethod ready to be stored in the interpreter's symbol map.
func WrapMethodFunc(typeArgCount int, target any, params, results []GenericFieldType, variadic bool) LinkedMethod {
	return link.WrapMethodFunc(typeArgCount, target, params, results, variadic)
}

// WrapType describes a generic type's field shape for the interpreter.
//
// Takes name (string) which is the generic type's name.
// Takes typeArgCount (int) which is the number of type parameters it declares.
// Takes fields ([]GenericField) which describe its fields in declaration order.
//
// Returns LinkedGenericType the interpreter instantiates against user types.
func WrapType(name string, typeArgCount int, fields []GenericField) LinkedGenericType {
	return link.WrapType(name, typeArgCount, fields)
}

// WrapNativeGenericType describes a generic type erased to a native implementation.
//
// Takes name (string) which is the generic type's name.
// Takes typeArgCount (int) which is the number of type parameters it declares.
// Takes erased (reflect.Type) which is the native type standing in for it.
// Takes erasureArgs (...reflect.Type) which are the erased type arguments.
//
// Returns NativeBackedGenericType for the interpreter's native dispatch.
func WrapNativeGenericType(name string, typeArgCount int, erased reflect.Type, erasureArgs ...reflect.Type) NativeBackedGenericType {
	return link.WrapNativeGenericType(name, typeArgCount, erased, erasureArgs...)
}

// MethodSymbolKey builds the symbol-map key for a linked method.
//
// Takes receiverTypeName (string) which names the receiver type.
// Takes methodName (string) which names the method.
//
// Returns string which is the joined key.
func MethodSymbolKey(receiverTypeName, methodName string) string {
	return link.MethodSymbolKey(receiverTypeName, methodName)
}
