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
	"slices"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// runMakeInterfaceMethodExpr builds a method-expression function value.
//
// Handles isa.SubOpTier2MakeInterfaceMethodExpr. general[C] becomes a function that takes
// the receiver as its first argument and resolves the method by name on that receiver's
// dynamic type when called, through the same lookup GET_METHOD uses.
//
// Takes vm (*VM) which owns the method tables.
// Takes frame (*CallFrame) which supplies the two extension words and the static receiver
// type recorded for the site.
// Takes registers (*Registers) which receives the function value.
// Takes instruction (isa.Instruction) which names the destination in operand C.
//
// Returns OpResult which is opContinue or opPanicError.
//
// Panics if the receiver is missing, nil, or has no such method.
func runMakeInterfaceMethodExpr(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sitePC := safeconv.IntToUint32Truncate(frame.ProgramCounter - 1)
	signature, methodName, ok := readInterfaceMethodExprWords(vm, frame)
	if !ok {
		return opPanicError
	}
	outTypes := slices.Collect(signature.Outs())
	staticTypeName := frame.Function.GetMethodReceiverTypeNames[sitePC]
	registers.General[instruction.C] = reflect.MakeFunc(signature, func(arguments []reflect.Value) []reflect.Value {
		if len(arguments) == 0 || !arguments[0].IsValid() {
			panic(newInvariantError("method expression invoked without a receiver"))
		}
		return invokeInterfaceMethodExpr(vm, arguments[0], methodName, staticTypeName, arguments[1:], outTypes)
	})
	return opContinue
}

// readInterfaceMethodExprWords reads the two extension words of the instruction: the type
// table index of the function type and the string constant index of the method.
//
// Takes vm (*VM) which receives a bounds error on a malformed word.
// Takes frame (*CallFrame) which supplies the words and tables.
//
// Returns the function type, the method name and false when a word is out of range.
func readInterfaceMethodExprWords(vm *VM, frame *CallFrame) (reflect.Type, string, bool) {
	typeWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(typeWord.A) | uint16(typeWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return nil, "", false
	}
	nameWord := readExtensionWord(frame)
	frame.ProgramCounter++
	nameIndex := uint16(nameWord.A) | uint16(nameWord.B)<<isa.WideBitShift
	if int(nameIndex) >= len(frame.Function.StringConstants) {
		vMBoundsError(vm, frame, boundsTableStringConstant, int(nameIndex), len(frame.Function.StringConstants))
		return nil, "", false
	}
	return frame.Function.TypeTable[typeIndex], frame.Function.StringConstants[nameIndex], true
}

// invokeInterfaceMethodExpr dispatches methodName on receiver's dynamic type: reflect
// methods first, then methods reachable through embedded natives, pipit-declared methods
// by type name, and finally methods promoted through an embedded field.
//
// Takes vm (*VM) which owns the method tables.
// Takes receiver (reflect.Value) which is the first argument of the expression.
// Takes methodName (string) which is the method to call.
// Takes staticTypeName (string) which is the interface's recorded type name.
// Takes arguments ([]reflect.Value) which are the remaining arguments.
// Takes outTypes ([]reflect.Type) which are the expression's result types.
//
// Returns []reflect.Value which holds the results shaped to outTypes.
//
// Panics with an interpreted runtime error when the receiver is nil or the method does
// not exist.
func invokeInterfaceMethodExpr(vm *VM, receiver reflect.Value, methodName, staticTypeName string, arguments []reflect.Value, outTypes []reflect.Type) []reflect.Value {
	if receiver.Kind() == reflect.Interface {
		receiver = receiver.Elem()
	}
	if !receiver.IsValid() {
		panic(newRuntimePanicError(nilDereferenceMessage))
	}
	if results, handled := invokeReflectTypeMethodExpr(vm, receiver, methodName, arguments, outTypes); handled {
		return results
	}
	if funcIndex, ok := lookupMethodTableForReceiver(vm, receiver, methodName); ok && int(funcIndex) < len(vm.functions) {
		own := &boundMethodVM{vm: vm, callee: vm.functions[funcIndex], limits: vm.Limits, rootFunctionOverride: nil}
		return shapeBoundMethodResults(own.invoke(receiver, arguments, identityArg), outTypes)
	}
	if bound := boundNativeMethodExpr(vm, receiver, methodName, staticTypeName); bound.IsValid() {
		return shapeBoundMethodResults(bound.Call(adaptBoundArguments(bound.Type(), arguments)), outTypes)
	}
	funcIndex, promotedReceiver, ok := resolvePromotedMethod(vm, receiver, methodName)
	if !ok || int(funcIndex) >= len(vm.functions) {
		panic(newInvariantError("undefined method %s.%s", receiver.Type(), methodName))
	}
	promoted := &boundMethodVM{vm: vm, callee: vm.functions[funcIndex], limits: vm.Limits, rootFunctionOverride: nil}
	return shapeBoundMethodResults(promoted.invoke(promotedReceiver, arguments, identityArg), outTypes)
}

// invokeReflectTypeMethodExpr runs a reflect.Type method on a script type through the
// reflect intercepts, raising the intercept's panic message as an interpreted panic.
//
// Takes vm (*VM) which owns the method tables.
// Takes receiver (reflect.Value) which is the receiver value.
// Takes methodName (string) which is the method name.
// Takes arguments ([]reflect.Value) which are the operands.
// Takes outTypes ([]reflect.Type) which shape the results.
//
// Returns the results and true when the intercept handled the call.
func invokeReflectTypeMethodExpr(vm *VM, receiver reflect.Value, methodName string, arguments []reflect.Value, outTypes []reflect.Type) ([]reflect.Value, bool) {
	if !receiver.CanInterface() || !receiver.Type().Implements(reflectTypeReflectType) {
		return nil, false
	}
	rt, ok := reflect.TypeAssert[reflect.Type](receiver)
	if !ok {
		return nil, false
	}
	results, panicMessage, handled := pipitReflectTypeMethodCall(vm, rt, methodName, arguments)
	if !handled {
		return nil, false
	}
	if panicMessage != "" {
		panic(newRuntimePanicError("%s", panicMessage))
	}
	return shapeBoundMethodResults(results, outTypes), true
}

// boundNativeMethodExpr resolves methodName on a native receiver: its own method set,
// then methods promoted from embedded native fields, then an externally registered script
// method.
//
// Takes vm (*VM) which owns the external method registry.
// Takes receiver (reflect.Value) which is the receiver value.
// Takes methodName (string) which is the method name.
// Takes staticTypeName (string) which is the receiver's static type name.
//
// Returns reflect.Value which is the bound method, invalid when none resolves.
func boundNativeMethodExpr(vm *VM, receiver reflect.Value, methodName, staticTypeName string) reflect.Value {
	bound := receiver.MethodByName(methodName)
	if !bound.IsValid() {
		bound = resolveNativeMethodOnEmbeds(receiver, methodName)
	}
	if !bound.IsValid() {
		bound = resolveExternalPipitMethod(vm, receiver, methodName, staticTypeName)
	}
	return bound
}

// adaptBoundArguments converts call arguments to the bound method's parameter types where
// the two differ only in representation (the caller's int against the interpreted
// method's int64), so reflect.Call accepts them.
//
// Takes bound (reflect.Type) which is the bound method's function type.
// Takes arguments ([]reflect.Value) which are the caller's arguments.
//
// Returns the arguments, converted where needed.
func adaptBoundArguments(bound reflect.Type, arguments []reflect.Value) []reflect.Value {
	fixed := bound.NumIn()
	if bound.IsVariadic() {
		fixed--
	}
	adapted := make([]reflect.Value, len(arguments))
	for i, argument := range arguments {
		adapted[i] = argument
		if i >= fixed || !argument.IsValid() {
			continue
		}
		want := bound.In(i)
		if argument.Type() != want && argument.Type().ConvertibleTo(want) && want.Kind() != reflect.Interface {
			adapted[i] = argument.Convert(want)
		}
	}
	return adapted
}
