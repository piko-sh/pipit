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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
)

// nativeMethodReceiver says how a method value's receiver becomes the expression's
// receiver word.
type nativeMethodReceiver uint8

const (
	// nativeMethodReceiverNone marks a shape without a direct path.
	nativeMethodReceiverNone nativeMethodReceiver = iota

	// nativeMethodReceiverPointer identifies a pointer receiver whose word holds the
	// receiver pointer.
	nativeMethodReceiverPointer

	// nativeMethodReceiverValue identifies a value receiver held indirectly.
	nativeMethodReceiverValue

	// nativeMethodReceiverInterface identifies an interface receiver.
	nativeMethodReceiverInterface
)

// nativeMethodShape is the receiver-free cache of one native method call site.
type nativeMethodShape struct {
	// valueType is the *abi.Type of the receiver the method value names.
	valueType unsafe.Pointer

	// dynamicType is the *abi.Type of an interface receiver's dynamic value; nil for other
	// receivers.
	dynamicType unsafe.Pointer

	// expression is the method expression's func value word; nil when the shape has no
	// direct path.
	expression unsafe.Pointer

	// index is the method index the method value names.
	index int

	// receiver says how the receiver word is read.
	receiver nativeMethodReceiver
}

// fastPathMethodDispatcher is the signature used by a method-expression dispatcher.
//
// It matches the tag's fastPathDispatcher, with expression called using receiver as the
// first argument.
type fastPathMethodDispatcher func(vm *VM, expression, receiver unsafe.Pointer, site *program.CallSite, registers *Registers)

// reflectTypeOfABIType returns the reflect.Type of an *abi.Type pointer.
//
// Takes abiType (unsafe.Pointer) which is the type.
//
// Returns reflect.Type which is that type.
func reflectTypeOfABIType(abiType unsafe.Pointer) reflect.Type {
	var probe any
	(*emptyInterface)(unsafe.Pointer(&probe)).typ = abiType
	return reflect.TypeOf(probe)
}

// interfaceReceiverElem returns the dynamic value of an interface receiver a method value
// names, from the method value's header.
//
// Takes header (*unsafeReflectValue) which is the method value's header; its type is an
// interface type.
//
// Returns reflect.Value which is the dynamic value, invalid for a nil interface.
func interfaceReceiverElem(header *unsafeReflectValue) reflect.Value {
	if header.flag&flagIndir == 0 || header.ptr == nil {
		return reflect.Value{}
	}
	receiver := unsafeReflectValue{typ: header.typ, ptr: header.ptr, flag: uintptr(reflect.Interface) | flagIndir}
	return (*reflect.Value)(unsafe.Pointer(&receiver)).Elem()
}

// receiverWord reads the receiver word for shape from a method value, or reports that the
// method value is not of the shape.
//
// Takes method (reflect.Value) which is the method value register.
//
// Returns unsafe.Pointer which is the receiver word.
// Returns bool which is false when the method value names another receiver type, method
// or dynamic type, or a receiver the shape cannot pass.
func (shape *nativeMethodShape) receiverWord(method reflect.Value) (unsafe.Pointer, bool) {
	header := (*unsafeReflectValue)(unsafe.Pointer(&method))
	if shape.expression == nil || header.flag&flagMethodBit == 0 || header.typ != shape.valueType ||
		int(header.flag>>flagMethodShift) != shape.index {
		return nil, false
	}
	indirect := header.flag&flagIndir != 0
	switch shape.receiver {
	case nativeMethodReceiverPointer:
		if indirect {
			return *(*unsafe.Pointer)(header.ptr), true
		}
		return header.ptr, true
	case nativeMethodReceiverValue:
		return header.ptr, indirect
	case nativeMethodReceiverInterface:
		element := interfaceReceiverElem(header)
		elemHeader := (*unsafeReflectValue)(unsafe.Pointer(&element))
		if elemHeader.typ != shape.dynamicType {
			return nil, false
		}
		return elemHeader.ptr, true
	default:
		return nil, false
	}
}

// buildNativeMethodShape derives the receiver-free shape of a method value for a site
// whose calls take tag's dispatcher.
//
// Takes method (reflect.Value) which is the method value register.
// Takes tag (nativeFastPathTag) which is the site's fast-path tag.
//
// Returns nativeMethodShape which is the shape.
// Returns bool which is false when the value is not a reflected method, the tag has no
// method-expression dispatcher, a pointer-shaped value receiver is held directly, or the
// signature does not match the tag.
func buildNativeMethodShape(method reflect.Value, tag nativeFastPathTag) (nativeMethodShape, bool) {
	header := (*unsafeReflectValue)(unsafe.Pointer(&method))
	if header.flag&flagMethodBit == 0 || int(tag) >= len(fastPathMethodDispatchTable) || fastPathMethodDispatchTable[tag] == nil {
		return nativeMethodShape{}, false
	}
	bound := method.Type()
	if fastPathTagByType[bound] != tag {
		return nativeMethodShape{}, false
	}
	shape := nativeMethodShape{valueType: header.typ, index: int(header.flag >> flagMethodShift), dynamicType: nil, expression: nil, receiver: 0}
	receiverType := reflectTypeOfABIType(header.typ)
	if shape.index >= receiverType.NumMethod() {
		return nativeMethodShape{}, false
	}
	var expression reflect.Method
	found := false
	switch {
	case receiverType.Kind() == reflect.Interface:
		element := interfaceReceiverElem(header)
		if !element.IsValid() {
			return nativeMethodShape{}, false
		}
		elemHeader := (*unsafeReflectValue)(unsafe.Pointer(&element))
		name := receiverType.Method(shape.index).Name
		switch {
		case element.Kind() == reflect.Pointer:
			expression, found = element.Type().MethodByName(name)
		case elemHeader.flag&flagIndir != 0:
			expression, found = reflect.PointerTo(element.Type()).MethodByName(name)
		}
		shape.dynamicType = elemHeader.typ
		shape.receiver = nativeMethodReceiverInterface
	case receiverType.Kind() == reflect.Pointer:
		expression, found = receiverType.Method(shape.index), true
		shape.receiver = nativeMethodReceiverPointer
	case header.flag&flagIndir != 0:
		expression, found = reflect.PointerTo(receiverType).MethodByName(receiverType.Method(shape.index).Name)
		shape.receiver = nativeMethodReceiverValue
	}
	if !found || !expression.Func.IsValid() || !methodExpressionMatches(expression.Func.Type(), bound) {
		return nativeMethodShape{}, false
	}
	function := expression.Func.Interface()
	shape.expression = (*emptyInterface)(unsafe.Pointer(&function)).data
	return shape, true
}

// methodExpressionMatches reports whether a method expression's type is the bound method
// type with one pointer receiver first, so the receiver passes as one pointer word.
//
// Takes expression (reflect.Type) which is the method expression's type.
// Takes bound (reflect.Type) which is the method value's type.
//
// Returns bool which is true when they agree.
func methodExpressionMatches(expression, bound reflect.Type) bool {
	if expression.Kind() != reflect.Func || expression.NumIn() != bound.NumIn()+1 || expression.NumOut() != bound.NumOut() ||
		expression.IsVariadic() != bound.IsVariadic() || expression.In(0).Kind() != reflect.Pointer {
		return false
	}
	for index := range bound.NumIn() {
		if expression.In(index+1) != bound.In(index) {
			return false
		}
	}
	for index := range bound.NumOut() {
		if expression.Out(index) != bound.Out(index) {
			return false
		}
	}
	return true
}

func init() {
	fastPathMethodDispatchTable[fastPathTagStringIntError] = fpMethodDispatchStringIntError
	fastPathMethodDispatchTable[fastPathTagRetError] = fpMethodDispatchRetError
	fastPathMethodDispatchTable[fastPathTagStringError] = fpMethodDispatchStringError
}

// dispatchNativeMethodTagged calls a method expression with its receiver through the
// tag's method-expression dispatcher, inside the recover boundary
// dispatchNativeFastPathTagged gives a cached function.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes tag (nativeFastPathTag) which selects the dispatcher.
// Takes expression (unsafe.Pointer) which is the method expression's func value word.
// Takes receiver (unsafe.Pointer) which is the receiver word.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
//
// Returns the recovered panic value, or nil.
func dispatchNativeMethodTagged(vm *VM, tag nativeFastPathTag, expression, receiver unsafe.Pointer, site *program.CallSite, registers *Registers) (panicValue any) {
	defer func() {
		if r := recover(); r != nil {
			panicValue = r
		}
	}()
	fastPathMethodDispatchTable[tag](vm, expression, receiver, site, registers)
	return nil
}

// fpMethodDispatchStringIntError is the method-expression form of
// fpDispatchStringIntError.
//
// Takes expression (unsafe.Pointer) which is the method expression.
// Takes receiver (unsafe.Pointer) which is the receiver word.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpMethodDispatchStringIntError(_ *VM, expression, receiver unsafe.Pointer, site *program.CallSite, registers *Registers) {
	method := *(*func(unsafe.Pointer, string) (int, error))(unsafe.Pointer(&expression))
	value, err := method(receiver, readStringArg(registers, site.Arguments[0]))
	registers.Ints[site.Returns[0].Register] = int64(value)
	if len(site.Returns) > 1 {
		storeErrorResult(registers, site.Returns[1].Register, err)
	}
}

// fpMethodDispatchRetError is the method-expression form of fpDispatchRetError.
//
// Takes expression (unsafe.Pointer) which is the method expression.
// Takes receiver (unsafe.Pointer) which is the receiver word.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpMethodDispatchRetError(_ *VM, expression, receiver unsafe.Pointer, site *program.CallSite, registers *Registers) {
	f := *(*func(unsafe.Pointer) error)(unsafe.Pointer(&expression))
	storeErrorResult(registers, site.Returns[0].Register, f(receiver))
}

// fpMethodDispatchStringError is the method-expression form of fpDispatchStringError.
//
// Takes expression (unsafe.Pointer) which is the method expression.
// Takes receiver (unsafe.Pointer) which is the receiver word.
// Takes site (*CallSite) which is the call site metadata.
// Takes registers (*Registers) which is the VM register file.
func fpMethodDispatchStringError(_ *VM, expression, receiver unsafe.Pointer, site *program.CallSite, registers *Registers) {
	method := *(*func(unsafe.Pointer, string) error)(unsafe.Pointer(&expression))
	result := method(receiver, readStringArg(registers, site.Arguments[0]))
	if len(site.Returns) > 0 {
		storeErrorResult(registers, site.Returns[0].Register, result)
	}
}

// storeErrorResult stores an error result in a general register, the zero Value for nil.
//
// Takes registers (*Registers) which is the VM register file.
// Takes register (uint8) which is the destination.
// Takes err (error) which is the result.
func storeErrorResult(registers *Registers, register uint8, err error) {
	if err != nil {
		registers.General[register] = reflect.ValueOf(err)
	} else {
		registers.General[register] = reflect.Value{}
	}
}
