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

package link

import "reflect"

// MethodSymbolSeparator joins a receiver type name and a method name to form the symbol
// key a LinkedMethod is registered under. A Go identifier cannot contain it, so the key
// never collides with a package-level symbol.
const MethodSymbolSeparator = "."

// LinkedMethod marks a registered symbol as a generic method whose interpreter dispatch
// is delegated to a non-generic sibling function. Reflect cannot represent a generic
// method, so the host registers this sentinel and the interpreter calls the sibling with
// resolved type arguments prepended.
type LinkedMethod struct {
	// Target is a reflect.Value of the sibling function, whose parameters are TypeArgCount
	// reflect.Type values, then the receiver, then the method's own arguments.
	Target reflect.Value

	// Params captures the method's own argument shape in declaration order, excluding both
	// the type arguments and the receiver.
	Params []GenericFieldType

	// Results captures the method's return shape in declaration order.
	Results []GenericFieldType

	// Variadic mirrors reflect.Type.IsVariadic for the method's declared parameter list.
	Variadic bool

	// TypeArgCount is the number of type parameters the method itself declares, excluding
	// those inherited from the generic receiver.
	TypeArgCount int
}

// WrapMethod constructs a LinkedMethod for a sibling captured at registration time.
//
// Takes typeArgCount (int) which is the number of type parameters the generic method
// declares.
// Takes target (any) which is the sibling function value.
//
// Returns a LinkedMethod ready to be stored as a reflect.Value in the interpreter's
// symbol map, under the key MethodSymbolKey builds.
func WrapMethod(typeArgCount int, target any) LinkedMethod {
	return LinkedMethod{
		Target:       reflect.ValueOf(target),
		Params:       nil,
		Results:      nil,
		Variadic:     false,
		TypeArgCount: typeArgCount,
	}
}

// WrapMethodFunc constructs a LinkedMethod with explicit parameter and return
// descriptors, for a method whose signature mentions its type parameters outside the
// direct T position.
//
// Takes typeArgCount (int) which is the number of type parameters the generic method
// declares.
// Takes target (any) which is the sibling function value.
// Takes params ([]GenericFieldType) which describe the method's own arguments in
// declaration order.
// Takes results ([]GenericFieldType) which describe the method's return values in
// declaration order.
// Takes variadic (bool) which mirrors the method's IsVariadic flag.
//
// Returns a LinkedMethod ready to be stored as a reflect.Value in the interpreter's
// symbol map.
func WrapMethodFunc(typeArgCount int, target any, params, results []GenericFieldType, variadic bool) LinkedMethod {
	return LinkedMethod{
		Target:       reflect.ValueOf(target),
		Params:       params,
		Results:      results,
		Variadic:     variadic,
		TypeArgCount: typeArgCount,
	}
}

// MethodSymbolKey builds the symbol-map key a LinkedMethod is registered under.
//
// Takes receiverTypeName (string) which is the bare name of the receiver's named type, as
// declared in its package.
// Takes methodName (string) which is the method's name.
//
// Returns the key to store the LinkedMethod under in the package's symbol map.
func MethodSymbolKey(receiverTypeName, methodName string) string {
	return receiverTypeName + MethodSymbolSeparator + methodName
}
