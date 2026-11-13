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

package program

import "reflect"

// ReceiverMethodKey names one method of a generic type called on a receiver of one
// runtime type.
type ReceiverMethodKey struct {
	// Receiver is the receiver's runtime type with any pointer removed, the type of an
	// instantiation such as List[string, int].
	Receiver reflect.Type

	// Method is the function index of the method's shared body, the one the method table
	// names.
	Method uint16
}

// receiverBodyKey is ReceiverMethodKey with the shared body itself in place of its index.
type receiverBodyKey struct {
	// receiver is the receiver's runtime type with any pointer removed.
	receiver reflect.Type

	// method is the method's shared body.
	method *CompiledFunction
}

// RegisterReceiverMethod records that a call of method on a receiver of runtime type
// receiver runs the body at target.
//
// Both indices must name functions already in root's function table.
//
// Takes root (*CompiledFunction) which owns the table and the functions.
// Takes receiver (reflect.Type) which is the receiver's runtime type, pointer removed.
// Takes method (uint16) which is the method's shared body.
// Takes target (uint16) which is the body to run.
func RegisterReceiverMethod(root *CompiledFunction, receiver reflect.Type, method, target uint16) {
	if int(method) >= len(root.Functions) || int(target) >= len(root.Functions) {
		return
	}
	if root.receiverMethods == nil {
		root.receiverMethods = make(map[ReceiverMethodKey]uint16)
		root.receiverBodies = make(map[receiverBodyKey]*CompiledFunction)
		root.receiverMethodOwners = make(map[*CompiledFunction]struct{})
	}
	shared := root.Functions[method]
	root.receiverMethods[ReceiverMethodKey{Receiver: receiver, Method: method}] = target
	root.receiverBodies[receiverBodyKey{receiver: receiver, method: shared}] = root.Functions[target]
	root.receiverMethodOwners[shared] = struct{}{}
}

// ReceiverMethodBody returns the body to run for method on a receiver of runtime type
// receiver.
//
// Takes receiver (reflect.Type) which is the receiver's runtime type. A pointer is
// removed.
// Takes method (*CompiledFunction) which is the body the method table named.
//
// Returns *CompiledFunction which is the body to run, method itself when the table has
// nothing to say about it.
// Returns bool which is false when method is a generic type's method that has bodies for
// other receivers but none for this one. running the shared body would build values of
// the wrong types, so the caller must refuse the call.
func (compiledFunction *CompiledFunction) ReceiverMethodBody(receiver reflect.Type, method *CompiledFunction) (*CompiledFunction, bool) {
	if !compiledFunction.HasReceiverBodies(method) {
		return method, true
	}
	if receiver != nil && receiver.Kind() == reflect.Pointer {
		receiver = receiver.Elem()
	}
	if body, ok := compiledFunction.receiverBodies[receiverBodyKey{receiver: receiver, method: method}]; ok {
		return body, true
	}
	return method, false
}

// HasReceiverBodies reports whether the receiver method table has entries for method, so
// a caller can skip working out the receiver's type for every other method.
//
// Takes method (*CompiledFunction) which is the body the method table named.
//
// Returns bool which is true when method is a generic type's method with entries.
func (compiledFunction *CompiledFunction) HasReceiverBodies(method *CompiledFunction) bool {
	_, owned := compiledFunction.receiverMethodOwners[method]
	return owned
}

// ReceiverMethods returns the receiver method table for serialisation.
//
// Returns map[ReceiverMethodKey]uint16 which is the live table. Callers must not mutate
// it.
func (compiledFunction *CompiledFunction) ReceiverMethods() map[ReceiverMethodKey]uint16 {
	return compiledFunction.receiverMethods
}

// SetReceiverMethods installs a receiver method table rebuilt from serialised bytecode.
// The function table must already be in place.
//
// Takes table (map[ReceiverMethodKey]uint16) which becomes the table.
func (compiledFunction *CompiledFunction) SetReceiverMethods(table map[ReceiverMethodKey]uint16) {
	compiledFunction.receiverMethods = nil
	compiledFunction.receiverBodies = nil
	compiledFunction.receiverMethodOwners = nil
	for key, target := range table {
		RegisterReceiverMethod(compiledFunction, key.Receiver, key.Method, target)
	}
}
