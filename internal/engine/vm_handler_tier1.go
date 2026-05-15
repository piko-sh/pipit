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
	"errors"
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

const (
	// minStrconvBase is the smallest radix accepted by strconv.FormatInt (and the tier-1
	// FormatInt sub-op).
	minStrconvBase = 2

	// maxStrconvBase is the largest radix accepted by strconv.FormatInt (and the tier-1
	// FormatInt sub-op): digits 0-9 plus letters a-z.
	maxStrconvBase = 36
)

// runMakeMethodExpr is the body of the MakeMethodExpr sub-op.
//
// Kept as a separate function rather than inlined into the switch because it performs
// multi-instruction extension-word reading and reflect.MakeFunc closure construction.
// Operand layout: instruction.b = destination general register, instruction.c = embedded
// field path length. The function index is in the next extension word; per-field
// traversal indices follow.
//
// Takes vm (*VM) which provides the function table.
// Takes frame (*CallFrame) which carries extension words.
// Takes registers (*Registers) which receives the closure.
// Takes instruction (instruction) carrying destination (B) and fieldCount (C).
//
// Returns OpResult indicating the next execution step.
//
// Panics when the generated method-expression closure is invoked without a receiver or
// with an out-of-range embedded field index.
func runMakeMethodExpr(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	fieldCount := int(instruction.C)
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	functionIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(functionIndex) >= len(vm.functions) {
		vMBoundsError(vm, frame, boundsTableFunction, int(functionIndex), len(vm.functions))
		return opPanicError
	}
	var fieldPathBuffer [4]int
	fieldPath := fieldPathBuffer[:0]
	for range fieldCount {
		fieldExtension := readExtensionWord(frame)
		frame.ProgramCounter++
		fieldPath = append(fieldPath, int(fieldExtension.A))
	}
	callee := vm.functions[functionIndex]
	signature, ok := callee.ReflectMethodExprType()
	if !ok {
		vm.evalError = errors.New("cannot create method expression: no type info")
		return opPanicError
	}
	if fieldCount > 0 {
		signature = eraseReceiverParameter(signature)
	}
	bound := &boundMethodVM{vm: vm, callee: callee, limits: vm.Limits, rootFunctionOverride: nil}
	boundFieldPath := fieldPath
	registers.General[instruction.B] = reflect.MakeFunc(signature, func(arguments []reflect.Value) []reflect.Value {
		if len(arguments) == 0 || !arguments[0].IsValid() {
			panic(newInvariantError("method expression invoked without a receiver"))
		}
		receiver := resolveMethodExprReceiver(arguments[0], boundFieldPath, callee.IsPointerReceiver)
		return conformReflectResults(bound.invoke(receiver, arguments[1:], elemArg), signature)
	})
	return opContinue
}

// eraseReceiverParameter rebuilds a method-expression signature with its first parameter,
// the receiver, widened to the empty interface.
//
// Takes signature (reflect.Type) which is a func type with at least one parameter.
//
// Returns reflect.Type which accepts any receiver value.
func eraseReceiverParameter(signature reflect.Type) reflect.Type {
	if signature.Kind() != reflect.Func || signature.NumIn() == 0 {
		return signature
	}
	inTypes := make([]reflect.Type, signature.NumIn())
	inTypes[0] = reflect.TypeFor[any]()
	for i := 1; i < signature.NumIn(); i++ {
		inTypes[i] = signature.In(i)
	}
	outTypes := make([]reflect.Type, signature.NumOut())
	for i := range outTypes {
		outTypes[i] = signature.Out(i)
	}
	return reflect.FuncOf(inTypes, outTypes, signature.IsVariadic())
}

// resolveMethodExprReceiver derives the method-expression receiver.
//
// Unwraps the supplied receiver and walks the embedded-field path. Each step panics with
// an interpreted runtime error when the value is not a struct or the index is out of
// range, so a malformed call surfaces to interpreted recover() rather than crashing the
// host.
//
// Takes receiver (reflect.Value) which is the first argument supplied to the
// method-expression closure.
// Takes fieldPath ([]int) which lists the embedded-field indices to traverse from the
// unwrapped receiver.
// Takes pointerReceiver (bool) which is true when the method body expects the pointer to
// the resolved struct rather than the struct itself.
//
// Returns reflect.Value which is the receiver to hand to the method body: the addressable
// struct, or its address for a pointer receiver.
//
// Panics when the field path traverses a non-struct value or when an embedded-field index
// is out of range; the panic surfaces to interpreted recover().
func resolveMethodExprReceiver(receiver reflect.Value, fieldPath []int, pointerReceiver bool) reflect.Value {
	receiver = derefMethodExprStep(receiver)
	for _, index := range fieldPath {
		if receiver.Kind() != reflect.Struct {
			panic(newInvariantError("method expression field path traverses non-struct value (%s)", receiver.Kind()))
		}
		if index < 0 || index >= receiver.NumField() {
			panic(newInvariantError("method expression field index %d out of range", index))
		}
		receiver = derefMethodExprStep(receiver.Field(index))
	}
	receiver = ensureAddressableStructReceiver(receiver)

	if pointerReceiver && receiver.Kind() == reflect.Struct && receiver.CanAddr() {
		return receiver.Addr()
	}
	return receiver
}

// derefMethodExprStep strips every pointer or interface layer from one step of a method
// expression's field path (`Outer{*Inner}` reaches Inner through the pointer), raising
// Go's nil-dereference panic when a layer is nil.
//
// Takes value (reflect.Value) which is the value at the current path step.
//
// Returns reflect.Value which is the underlying struct or leaf value.
func derefMethodExprStep(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface) {
		if value.IsNil() {
			panic(newRuntimePanicError(nilDereferenceMessage))
		}
		value = value.Elem()
	}
	return value
}
