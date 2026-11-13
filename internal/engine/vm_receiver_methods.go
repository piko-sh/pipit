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
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// receiverMethodType returns the runtime type a receiver's methods are keyed on in the
// receiver method table. That is the value's type with any pointer or interface removed,
// or, for a closure converted to a named func type, that named type.
//
// Takes receiver (reflect.Value) which is the receiver value.
//
// Returns reflect.Type which is nil for an invalid receiver.
func receiverMethodType(receiver reflect.Value) reflect.Type {
	if !receiver.IsValid() {
		return nil
	}
	if receiver.Kind() == reflect.Interface && !receiver.IsNil() {
		receiver = receiver.Elem()
	}
	if closure := closureFromValue(receiver); closure != nil {
		return typemodel.NamedCompositeTypeByID(closure.namedType)
	}
	receiverType := receiver.Type()
	if receiverType.Kind() == reflect.Pointer {
		receiverType = receiverType.Elem()
	}
	return receiverType
}

// receiverMethodBody returns the body to run for a method found by name on receiver.
//
// Takes root (*program.CompiledFunction) which owns the method and the table.
// Takes receiver (reflect.Value) which is the receiver the method is called on.
// Takes method (*program.CompiledFunction) which is the body the method table named.
//
// Returns *program.CompiledFunction which is the body to run.
// Returns error which wraps fault.ErrNoMethodBodyForReceiver when no body was compiled
// for the receiver's type arguments.
func receiverMethodBody(root *program.CompiledFunction, receiver reflect.Value, method *program.CompiledFunction) (*program.CompiledFunction, error) {
	if root == nil || method == nil || !root.HasReceiverBodies(method) {
		return method, nil
	}
	receiverType := receiverMethodType(receiver)
	body, ok := root.ReceiverMethodBody(receiverType, method)
	if !ok {
		typeName := "<nil>"
		if receiverType != nil {
			typeName = scriptTypeString(receiverType)
		}
		return method, fmt.Errorf("%w: %s on %s", fault.ErrNoMethodBodyForReceiver, method.Name, typeName)
	}
	return body, nil
}

// receiverTypeMethodBody is receiverMethodBody for a receiver known only by its type, as
// reflect's method descriptors and Implements see it. An unknown type keeps the method
// the table named.
//
// Takes root (*program.CompiledFunction) which owns the method and the table.
// Takes receiverType (reflect.Type) which is the receiver's method table key, or nil.
// Takes method (*program.CompiledFunction) which is the body the method table named.
//
// Returns *program.CompiledFunction which is the body to run.
// Returns error which wraps fault.ErrNoMethodBodyForReceiver when no body was compiled
// for the receiver's type arguments.
func receiverTypeMethodBody(root *program.CompiledFunction, receiverType reflect.Type, method *program.CompiledFunction) (*program.CompiledFunction, error) {
	if root == nil || method == nil || receiverType == nil || !root.HasReceiverBodies(method) {
		return method, nil
	}
	body, ok := root.ReceiverMethodBody(receiverType, method)
	if !ok || body == nil {
		return method, fmt.Errorf("%w: %s on %s", fault.ErrNoMethodBodyForReceiver, method.Name, scriptTypeString(receiverType))
	}
	return body, nil
}
