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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// handleGetMethod resolves a method by name on a receiver value and stores the bound
// method in the destination register, using a per-VM cache.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the method name extension word.
// Takes registers (*Registers) which holds the receiver and destination.
// Takes instruction (instruction) which encodes the receiver register.
//
// Returns OpResult indicating the next execution step.
func handleGetMethod(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	getMethodPC := safeconv.IntToUint32Truncate(frame.ProgramCounter - 1)

	receiver := registers.General[instruction.B]
	if !receiver.IsValid() {
		return raiseNilDereference(vm)
	}

	ext := readExtensionWord(frame)
	frame.ProgramCounter++
	nameIndex := uint16(ext.A) | uint16(ext.B)<<isa.WideBitShift
	if int(nameIndex) >= len(frame.Function.StringConstants) {
		vMBoundsError(vm, frame, boundsTableStringConstant, int(nameIndex), len(frame.Function.StringConstants))
		return opPanicError
	}
	methodName := frame.Function.StringConstants[nameIndex]

	if receiver.Type() == reflectValueReflectType {
		if tryPipitReflectValueMethodGet(vm, registers, instruction.A, receiver, methodName) {
			return opContinue
		}
	}

	receiver = addressableMethodReceiver(receiver, registers, instruction.B)

	if cached := lookupCachedMethod(vm, receiver, methodName); cached.IsValid() {
		registers.General[instruction.A] = cached
		return opContinue
	}
	bound := resolveGetMethodBinding(vm, frame, receiver, methodName, getMethodPC)
	registers.General[instruction.A] = bound
	return opContinue
}

// resolveGetMethodBinding resolves a method name on a receiver.
//
// Falls back to embedded-method search and then to the cross-package method registry when
// reflect.Value.MethodByName returns zero. reflect's method-set is empty for
// pipit-synthesised receivers (reflect.StructOf produces no methods), so cross-batch
// dispatch needs the registry populated by bridgePackageExports plus a MakeFunc
// trampoline that invokes the owning rootFunction's bytecode.
//
// Takes vm (*VM) which provides the external method registry.
// Takes frame (*CallFrame) which carries the static receiver type-name table.
// Takes receiver (reflect.Value) which is the value the method binds to.
// Takes methodName (string) which is the method identifier.
// Takes getMethodPC (uint32) which is the program counter of the originating
// isa.SubOpGetMethod, used to look up the static receiver type name.
//
// Returns reflect.Value which is the bound method, or an invalid value when no matching
// method is found.
func resolveGetMethodBinding(vm *VM, frame *CallFrame, receiver reflect.Value, methodName string, getMethodPC uint32) reflect.Value {
	if bound, ok := boundRuntimeFuncMethod(vm, receiver, methodName); ok {
		return bound
	}
	bound := receiver.MethodByName(methodName)
	if !bound.IsValid() {
		bound = resolveNativeMethodOnEmbeds(receiver, methodName)
	}
	if bound.IsValid() {
		return bound
	}
	staticTypeName := frame.Function.GetMethodReceiverTypeNames[getMethodPC]
	if bound = resolveExternalPipitMethod(vm, receiver, methodName, staticTypeName); bound.IsValid() {
		return bound
	}
	return reflect.Value{}
}

// resolveExternalPipitMethod finds a method body in GlobalStore.externalMethods
// (populated for every cross-package methodTable entry by Service.bridgePackageExports)
// and returns a reflect.MakeFunc callable that dispatches into the foreign rootFunction.
//
// Used by handleGetMethod when the in-VM method set lookup fails: pipit-synthesised types
// built via reflect.StructOf have no Go-reflect methods, so any method resolved via the
// isa.SubOpGetMethod path needs this fallback in the cross-batch CompileProgram flow.
//
// Takes vm (*VM) which provides the type-name registry and external method table.
// Takes receiver (reflect.Value) which is the value the method is bound to.
// Takes methodName (string) which is the method identifier without type qualifier.
// Takes staticTypeName (string) which is the static type name of the receiver.
//
// Returns a callable reflect.Value on success, or an invalid value when the type name or
// method is not registered.
func resolveExternalPipitMethod(vm *VM, receiver reflect.Value, methodName, staticTypeName string) reflect.Value {
	if vm == nil {
		return reflect.Value{}
	}
	typeName, ok := pipitTypeName(vm, receiver)
	if !ok || typeName == "" {
		if receiver.IsValid() {
			if name := bareSentinelName(receiver.Type()); name != "" {
				typeName = name
			}
		}
	}
	if typeName == "" {
		typeName = staticTypeName
	}
	if typeName == "" {
		return reflect.Value{}
	}
	callable, ok := buildPipitBoundMethodCallable(vm, receiver, typeName, methodName)
	if !ok {
		return reflect.Value{}
	}
	return callable
}

// addressableMethodReceiver promotes a method-less struct receiver to a pointer so its
// pointer-receiver method set becomes visible. When the struct is not addressable a fresh
// heap copy is taken and the receiver register is rewritten to alias the same backing
// storage.
//
// Takes receiver (reflect.Value) which is the resolved receiver value.
// Takes registers (*Registers) which holds the receiver register bank.
// Takes receiverRegister (uint8) which is the receiver's register index.
//
// Returns the receiver promoted to a pointer when promotion applied, or the original
// receiver unchanged.
func addressableMethodReceiver(receiver reflect.Value, registers *Registers, receiverRegister uint8) reflect.Value {
	if receiver.NumMethod() != 0 || receiver.Kind() != reflect.Struct {
		return receiver
	}
	if receiver.CanAddr() {
		return receiver.Addr()
	}
	pointer := reflect.New(receiver.Type())
	pointer.Elem().Set(receiver)
	registers.General[receiverRegister] = pointer.Elem()
	return pointer
}

// lookupCachedMethod resolves a method by name through the per-VM method-index cache,
// populating the cache on a miss when capacity allows.
//
// Takes vm (*VM) which owns the method-index cache.
// Takes receiver (reflect.Value) which is the resolved receiver value.
// Takes methodName (string) which is the method to resolve.
//
// Returns the bound method reflect.Value, or an invalid value when the receiver type does
// not declare the method.
func lookupCachedMethod(vm *VM, receiver reflect.Value, methodName string) reflect.Value {
	if bound, ok := boundRuntimeFuncMethod(vm, receiver, methodName); ok {
		return bound
	}
	key := methodCacheKey{typ: receiver.Type(), Name: methodName}
	if index, ok := vm.methodCache[key]; ok {
		return receiver.Method(index)
	}
	m, ok := receiver.Type().MethodByName(methodName)
	if !ok {
		return reflect.Value{}
	}
	if vm.methodCache == nil {
		vm.methodCache = make(map[methodCacheKey]int)
	}
	if len(vm.methodCache) < maxMethodCacheEntries {
		vm.methodCache[key] = m.Index
	}
	return receiver.Method(m.Index)
}

// resolveNativeMethodOnEmbeds resolves a promoted method on embeds.
//
// Walks the embedded fields of a pipit-synthesised struct whose method-bearing embedded
// field was renamed non-anonymous (the `PipitEmbed_` marker, see embeddedFieldNeedsRename
// in compiler_types.go). Because pipit has to rename exported method-bearing embeds to
// satisfy reflect.StructOf, Go reflect does not promote the embed's methods onto the
// synth struct, so `event.Format(...)` where Event embeds time.Time finds nothing via
// MethodByName. This walks the renamed embed fields and resolves the method on the
// embedded value directly.
//
// Takes receiver (reflect.Value) which is the struct (or pointer to struct) the promoted
// method is invoked on.
// Takes methodName (string) which is the promoted method's name.
//
// Returns the bound method reflect.Value, or an invalid value when no embedded field
// carries the method.
func resolveNativeMethodOnEmbeds(receiver reflect.Value, methodName string) reflect.Value {
	structValue := receiver
	if structValue.Kind() == reflect.Pointer {
		if structValue.IsNil() {
			return reflect.Value{}
		}
		structValue = structValue.Elem()
	}
	if structValue.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	structType := structValue.Type()
	for index := range structType.NumField() {
		if !program.IsAnonymousField(structType.Field(index)) {
			continue
		}
		if bound := resolveMethodOnEmbeddedField(structValue.Field(index), methodName); bound.IsValid() {
			return bound
		}
	}
	return reflect.Value{}
}

// resolveMethodOnEmbeddedField resolves a method by name on a single embedded struct
// field, trying the value, its address, and the pointee when the field is a non-nil
// pointer.
//
// Takes fieldValue (reflect.Value) which is the embedded field value.
// Takes methodName (string) which is the promoted method's name.
//
// Returns the bound method reflect.Value, or an invalid value when the field does not
// carry the method.
func resolveMethodOnEmbeddedField(fieldValue reflect.Value, methodName string) reflect.Value {
	if bound := fieldValue.MethodByName(methodName); bound.IsValid() {
		return bound
	}
	if fieldValue.CanAddr() {
		if bound := fieldValue.Addr().MethodByName(methodName); bound.IsValid() {
			return bound
		}
	}
	if fieldValue.Kind() == reflect.Pointer && !fieldValue.IsNil() {
		if bound := fieldValue.Elem().MethodByName(methodName); bound.IsValid() {
			return bound
		}
	}
	return reflect.Value{}
}

// setMethodArgs copies reflect arguments into typed registers according to the callee's
// parameter kinds. General register 0 is reserved for the receiver.
//
// Takes registers (*Registers) which is the destination register set.
// Takes callee (*CompiledFunction) which provides the expected parameter kinds.
// Takes arguments ([]reflect.Value) which holds the values to copy.
// Takes extract (func(reflect.Value) reflect.Value) which converts each argument.
func setMethodArgs(registers *Registers, callee *program.CompiledFunction, arguments []reflect.Value, extract func(reflect.Value) reflect.Value) {
	var kindIndex [isa.NumRegisterKinds]int
	kindIndex[isa.RegisterGeneral] = 1
	scattered := len(callee.ParameterRegisters) == len(callee.ParameterKinds)
	for i, argument := range arguments {
		parameterKind := callee.ParameterKinds[i+1]
		dest := kindIndex[parameterKind]
		kindIndex[parameterKind]++
		if scattered {
			dest = int(callee.ParameterRegisters[i+1])
		}
		writeRegisterValue(registers, safeconv.MustIntToUint8(dest), parameterKind, extract(argument))
	}
}

// identityArg returns the argument unchanged (used by bound method invocations).
//
// Takes v (reflect.Value) which is the value to pass through.
//
// Returns reflect.Value unmodified.
func identityArg(v reflect.Value) reflect.Value { return v }

// elemArg unwraps an interface argument (used by method expression invocations). Concrete
// (non-interface) values are returned as-is.
//
// Takes v (reflect.Value) which is the value to unwrap.
//
// Returns reflect.Value with the interface unwrapped, or v itself.
func elemArg(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Interface {
		return v.Elem()
	}
	return v
}

// handleBindMethod creates a bound method value by capturing the receiver and wrapping
// the compiled callee in a reflect.MakeFunc closure.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the function index extension word.
// Takes registers (*Registers) which holds the receiver and destination.
// Takes instruction (instruction) which encodes the receiver and field count.
//
// Returns OpResult indicating the next execution step.
func handleBindMethod(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	receiver := registers.General[instruction.B]
	if !receiver.IsValid() {
		return raiseNilDereference(vm)
	}
	fieldCount := int(instruction.C)

	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	functionIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(functionIndex) >= len(vm.functions) {
		vMBoundsError(vm, frame, boundsTableFunction, int(functionIndex), len(vm.functions))
		return opPanicError
	}

	for range fieldCount {
		fieldExtension := readExtensionWord(frame)
		frame.ProgramCounter++
		receiver = receiver.Field(int(fieldExtension.A))
	}

	callee := vm.functions[functionIndex]
	signature, ok := callee.ReflectFuncType()
	if !ok {
		vm.evalError = errors.New("cannot create method value: no type info")
		return opPanicError
	}

	bound := &boundMethodVM{vm: vm, callee: callee, limits: vm.Limits, rootFunctionOverride: nil}

	boundReceiver := receiver
	switch {
	case receiver.Kind() == reflect.Struct:
		cp := reflect.New(receiver.Type()).Elem()
		cp.Set(receiver)
		boundReceiver = cp
	case vm.Arena != nil && receiver.CanAddr() && vm.Arena.ownsBytePointer(ReflectValuePtr(receiver)):

		boundReceiver = copyReflectValue(receiver)
	}
	registers.General[instruction.A] = reflect.MakeFunc(signature, func(arguments []reflect.Value) []reflect.Value {
		return conformReflectResults(bound.invoke(boundReceiver, arguments, identityArg), signature)
	})
	return opContinue
}

// ensureAddressableStructReceiver returns an addressable copy of receiver so
// pointer-receiver methods inside the callee can take Addr().
//
// Takes receiver (reflect.Value) which is the receiver argument.
//
// Returns the receiver unchanged when already addressable or non-struct, or an
// addressable copy otherwise.
func ensureAddressableStructReceiver(receiver reflect.Value) reflect.Value {
	if receiver.Kind() != reflect.Struct || receiver.CanAddr() {
		return receiver
	}
	addressable := reflect.New(receiver.Type()).Elem()
	addressable.Set(receiver)
	return addressable
}
