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
	"sync/atomic"
	"unsafe"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// methodKeyBufferSize is the size of the stack buffer a "TypeName.MethodName" lookup key
// is built in. A longer key is built on the heap.
const methodKeyBufferSize = 96

// methodCallReceiver bundles the receiver-side state passed into resolveMethodCallSlow so
// the function signature stays within the argument-limit.
type methodCallReceiver struct {
	// typ is the reflect.Type of the receiver.
	typ reflect.Type

	// value is the receiver's runtime reflect.Value.
	value reflect.Value

	// register is the general-bank register holding the receiver.
	register uint8
}

// promotionSearch is one search for a method promoted through embedded fields.
//
// Go reaches a method promoted through an embedded interface by a wrapper that calls the
// interface's method on the field's current value, so each embedded interface the search
// crosses starts a fresh promotion from the concrete value it holds. The static depth
// bound (maxPromotedMethodDepth) applies between crossings, and the crossings are bounded
// by the call depth limit, as Go's wrapper frames are by the stack.
type promotionSearch struct {
	// vm provides the method table and the call depth limit.
	vm *VM

	// methodName is the unqualified method being searched for.
	methodName string

	// interfaceHops counts the embedded interfaces crossed so far.
	interfaceHops int

	// reachedNil records that the search met a nil embedded interface or a nil pointer,
	// which in Go dereferences nil when the method is reached through it.
	reachedNil bool
}

// at searches receiver and its embedded fields.
//
// Takes receiver (reflect.Value) which is the value whose embedded fields are searched.
// Takes depth (int) which is the static embedding depth since the last interface.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the receiver the method binds to.
// Returns bool which is true when the method was found.
func (s *promotionSearch) at(receiver reflect.Value, depth int) (uint16, reflect.Value, bool) {
	if depth >= maxPromotedMethodDepth || !receiver.IsValid() {
		return 0, receiver, false
	}
	if closure := closureFromValue(receiver); closure != nil {
		return s.closureMethod(closure, receiver)
	}
	value := receiver
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			s.reachedNil = true
			return 0, receiver, false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Interface {
		return s.throughInterface(value, receiver)
	}
	if value.Kind() != reflect.Struct {
		return 0, receiver, false
	}
	structType := value.Type()
	for index := range structType.NumField() {
		ft := structType.Field(index)
		if !program.IsAnonymousField(ft) {
			continue
		}
		field := launderReadOnlyFieldValue(value.Field(index))
		if functionIndex, resolved, ok := s.throughEmbeddedField(ft, field, depth); ok {
			return functionIndex, resolved, true
		}
	}
	return 0, receiver, false
}

// closureMethod finds the method of the named func type a closure reached through an
// interface was converted to. A closure has no fields to promote methods from.
//
// Takes closure (*RuntimeClosure) which is the closure.
// Takes receiver (reflect.Value) which holds it and is returned as the receiver.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the receiver the method binds to.
// Returns bool which is true when the closure's named type declares the method.
func (s *promotionSearch) closureMethod(closure *RuntimeClosure, receiver reflect.Value) (uint16, reflect.Value, bool) {
	name := closureNamedTypeName(closure)
	if name == "" {
		return 0, receiver, false
	}
	functionIndex, ok := s.vm.rootFunction.MethodTable()[name+"."+s.methodName]
	return functionIndex, receiver, ok
}

// throughInterface continues the search from the concrete value an interface holds.
//
// Takes value (reflect.Value) which is the interface value.
// Takes fallback (reflect.Value) which is returned when nothing is found.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the receiver the method binds to.
// Returns bool which is true when the method was found.
func (s *promotionSearch) throughInterface(value, fallback reflect.Value) (uint16, reflect.Value, bool) {
	if value.IsNil() {
		s.reachedNil = true
		return 0, fallback, false
	}
	s.interfaceHops++
	if s.interfaceHops > s.vm.callDepthLimit() {
		return 0, fallback, false
	}
	concrete := value.Elem()
	if functionIndex, ok := lookupMethodTableForReceiver(s.vm, concrete, s.methodName); ok {
		return functionIndex, concrete, true
	}
	if functionIndex, embedded, ok := s.at(concrete, 0); ok {
		return functionIndex, embedded, true
	}
	return 0, fallback, false
}

// throughEmbeddedField searches one anonymous field, the method set of its own type first
// and then its embedded fields.
//
// Takes ft (reflect.StructField) which is the field metadata.
// Takes field (reflect.Value) which is the field value.
// Takes depth (int) which is the static embedding depth of the field's parent.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the receiver the method binds to.
// Returns bool which is true when the field produced a match.
func (s *promotionSearch) throughEmbeddedField(ft reflect.StructField, field reflect.Value, depth int) (uint16, reflect.Value, bool) {
	if ft.Type.Kind() == reflect.Interface {
		return s.throughInterface(field, field)
	}
	if ft.Type.Kind() == reflect.Pointer {
		if field.IsNil() {
			s.reachedNil = true
			return 0, field, false
		}
		field = field.Elem()
		if field.Kind() == reflect.Interface {
			return s.throughInterface(field, field)
		}
	}
	if functionIndex, ok := lookupMethodTableForReceiver(s.vm, field, s.methodName); ok {
		return functionIndex, field, true
	}
	if functionIndex, ok := s.vm.rootFunction.MethodTable()[ft.Name+"."+s.methodName]; ok {
		return functionIndex, field, true
	}
	return s.at(field, depth+1)
}

// handleCallMethod dispatches a compiled method call by resolving the method from the
// type's method table and pushing a new frame for the callee.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
func handleCallMethod(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	return dispatchMethodCallSite(vm, frame, registers, site, extensionWord)
}

// dispatchMethodCallSite performs the receiver lookup, IC walk, and callee invocation for
// an already-resolved call site.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the current call frame.
// Takes registers (*Registers) which holds the active register banks.
// Takes site (*CallSite) which is the already-resolved call descriptor.
// Takes extensionWord (instruction) which carries the call's mode and info bits.
//
// Returns OpResult indicating the next execution step.
func dispatchMethodCallSite(vm *VM, frame *CallFrame, registers *Registers, site *program.CallSite, extensionWord isa.Instruction) OpResult {
	if len(site.Arguments) == 0 {
		vm.evalError = newInvariantError("method call site has no receiver argument")
		return opPanicError
	}
	receiverLocation := site.Arguments[0]
	receiver := registers.General[receiverLocation.Register]
	typeWord := receiverDispatchTypeWord(&receiver)
	if hit := lookupMethodInlineCache(site, typeWord); hit != nil {
		vm.MethodCallGoDispatches++
		valueMethodThroughPointer := !hit.IsPointerReceiver && receiver.IsValid() && receiver.Kind() == reflect.Pointer
		if !valueMethodThroughPointer {
			vm.tryPopulateMethodASMInfo(frame.Function, site, hit, typeWord)
			if result, ran := runCachedLeaf(vm, registers, site, hit); ran {
				return result
			}
		}
		return runFusedOrFramed(vm, registers, site, hit)
	}
	if isNilInterfaceReceiver(receiver) {
		return raiseNilPointerDereference(vm)
	}
	recvType := receiver.Type()
	if recvType.Kind() == reflect.Pointer {
		recvType = recvType.Elem()
	}
	return resolveMethodCallSlow(vm, frame, registers, site, methodCallReceiver{value: receiver, typ: recvType, register: receiverLocation.Register}, extensionWord)
}

// resolveMethodCallSlow handles the slow path for handleCallMethod.
//
// Used when neither the specialisation slot nor the IC produced a callee. Decodes the
// method name from the extension word, looks up the callee via the method table (with
// promoted-method fallback and reflect-based native dispatch as last resort), and updates
// the IC once a target is found.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes site (*CallSite) which is the call descriptor.
// Takes receiver (methodCallReceiver) which bundles the receiver value/type/register.
// Takes extensionWord (Instruction) which carries the call's mode and info bits.
//
// Returns the dispatch OpResult.
func resolveMethodCallSlow(vm *VM, frame *CallFrame, registers *Registers, site *program.CallSite, receiver methodCallReceiver, extensionWord isa.Instruction) OpResult {
	nameIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(nameIndex) >= len(frame.Function.StringConstants) {
		vMBoundsError(vm, frame, boundsTableStringConstant, int(nameIndex), len(frame.Function.StringConstants))
		return opPanicError
	}
	methodName := frame.Function.StringConstants[nameIndex]
	typeName := resolveReceiverTypeName(vm, site, receiver.typ)
	closureReceiver := false
	if closure := closureFromValue(receiver.value); closure != nil {
		closureReceiver = true
		if name := closureNamedTypeName(closure); name != "" {
			typeName = name
		}
	}
	functionIndex, ok := lookupMethodKey(vm.rootFunction.MethodTable(), typeName, methodName)
	wasPromoted := false
	receiverValue := receiver.value
	search := promotionSearch{vm: vm, methodName: methodName, interfaceHops: 0, reachedNil: false}
	if !ok {
		functionIndex, receiverValue, ok = search.at(receiverValue, 0)
		wasPromoted = ok
	}
	if !ok {
		receiver.value = receiverValue
		return resolveMethodCallExternalFallback(vm, registers, site, receiver, typeName, methodName, &search)
	}
	if int(functionIndex) >= len(vm.functions) {
		vMBoundsError(vm, frame, boundsTableFunction, int(functionIndex), len(vm.functions))
		return opPanicError
	}
	callee, err := receiverMethodBody(vm.rootFunction, receiverValue, vm.functions[functionIndex])
	if err != nil {
		vm.evalError = err
		return opPanicError
	}

	updateMethodIC(site, receiver.typ, functionIndex, callee, wasPromoted || closureReceiver)
	if !wasPromoted {
		return pushCompiledFrame(vm, registers, site, callee)
	}

	original := registers.General[receiver.register]
	registers.General[receiver.register] = receiverValueFor(callee, receiverValue)
	result := pushCompiledFrame(vm, registers, site, callee)
	registers.General[receiver.register] = original
	return result
}

// resolveMethodCallExternalFallback handles the slow-path fallbacks.
//
// Used by resolveMethodCallSlow when the local MethodTable and the promoted-method lookup
// both miss. Split out so the parent stays within the cognitive-complexity and
// function-length budgets. This slow path is only taken on IC misses for methods that the
// local RootFunction does not own, so the function-call overhead is irrelevant.
//
// It tries GlobalStore.externalMethods first (cross-package dispatch such as
// testify/assert.Fail invoking a main-package method via *main.localT), then the pipit
// reflect overlays (tryInterceptPipitReflectTypeMethod and
// tryInterceptPipitReflectValueMethod), and finally safeMethodByName for genuinely native
// receivers. It surfaces an "undefined method: T.M" error when every lookup misses.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes site (*CallSite) which is the call descriptor.
// Takes receiver (methodCallReceiver) whose .value holds the (possibly promoted)
// receiver.
// Takes typeName (string) which is the receiver's type name in the method tables.
// Takes methodName (string) which is the unqualified method name.
// Takes search (*promotionSearch) which is the promotion search that missed, so the
// receiver is not a promoted one. When every fallback misses too, it decides the panic
// (promotedMethodFailure).
//
// Returns the dispatch OpResult.
func resolveMethodCallExternalFallback(
	vm *VM,
	registers *Registers,
	site *program.CallSite,
	receiver methodCallReceiver,
	typeName string,
	methodName string,
	search *promotionSearch,
) OpResult {
	if result, dispatched := tryExternalMethodTable(vm, registers, site, receiver, typeName, methodName, false); dispatched {
		return result
	}
	if result, intercepted := tryInterceptPipitReflectTypeMethod(vm, registers, site, receiver.value, methodName); intercepted {
		return result
	}
	if result, intercepted := tryInterceptPipitReflectValueMethod(vm, registers, site, receiver.value, methodName); intercepted {
		return result
	}
	if result, intercepted := tryInterceptRuntimeFuncMethod(vm, registers, site, receiver.value, methodName); intercepted {
		return result
	}
	nativeMethod, lookupErr := safeMethodByName(receiver.value, methodName)
	if lookupErr == nil && nativeMethod.IsValid() {
		receiverType := receiver.value.Type()
		return handleCallBoundMethodReflect(vm, registers, site, nativeMethod, nativeMethodAnySink(receiverType, methodName),
			nativeCalleeName{receiverType: receiverType, methodName: methodName, description: ""})
	}
	result := promotedMethodFailure(vm, search, typeName+"."+methodName)
	if lookupErr != nil && vm.evalError != nil {
		vm.evalError = errors.Join(vm.evalError, lookupErr)
	}
	return result
}

// lookupMethodKey looks up the "TypeName.MethodName" key in a method table without
// building the key as a string. The key is assembled in a stack buffer and indexed with
// m[string(bytes)], which Go does not allocate for.
//
// Takes table (map[string]V) which is the method table.
// Takes typeName (string) which is the receiver's type name.
// Takes methodName (string) which is the method name.
//
// Returns V which is the entry.
// Returns bool which is true when the key is present.
func lookupMethodKey[V any](table map[string]V, typeName, methodName string) (V, bool) {
	var buffer [methodKeyBufferSize]byte
	key := append(buffer[:0], typeName...)
	key = append(key, '.')
	key = append(key, methodName...)
	entry, ok := table[string(key)]
	return entry, ok
}

// tryExternalMethodTable consults GlobalStore.externalMethods.
//
// Drives cross-package method dispatch. Cross-package callers (e.g. testify dispatching
// `t.Errorf` where `t` is a *main.localT) register their methods here so a caller whose
// RootFunction does NOT own the receiver's methods can still find them.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes site (*CallSite) which is the call descriptor.
// Takes receiver (methodCallReceiver) which bundles the receiver value/type/register.
// Takes typeName (string) which is the receiver's type name in the method tables.
// Takes methodName (string) which is the method name.
// Takes wasPromoted (bool) which is the IC-cache hint forwarded to updateMethodIC.
//
// Returns the dispatch OpResult and true when the lookup hit and the call was queued.
// Otherwise it returns (_, false) so the caller continues with the reflect-fallback
// chain.
func tryExternalMethodTable(
	vm *VM,
	registers *Registers,
	site *program.CallSite,
	receiver methodCallReceiver,
	typeName string,
	methodName string,
	wasPromoted bool,
) (OpResult, bool) {
	if vm.Globals == nil {
		return opContinue, false
	}
	entry, ok := vm.Globals.lookupExternalMethodOf(typeName, methodName)
	if !ok || entry.rootFunction == nil {
		return opContinue, false
	}
	if int(entry.methodIndex) >= len(entry.rootFunction.Functions) {
		return opContinue, false
	}
	callee := entry.rootFunction.Functions[entry.methodIndex]
	if entry.rootFunction == vm.rootFunction {
		updateMethodIC(site, receiver.typ, entry.methodIndex, callee, wasPromoted)
	}

	snapshot := vm.swapToClosureRoot(entry.rootFunction)

	if snapshot != nil && callee.IsVariadic && site.RuntimeVariadicSliceType == nil && !site.IsEllipsisSpread && len(callee.ParameterKinds) > 0 && len(site.Arguments) >= len(callee.ParameterKinds)-1 {
		lastKind := callee.ParameterKinds[len(callee.ParameterKinds)-1]
		elementType := program.KindDefaultReflectType(lastKind)
		site.RuntimeVariadicSliceType = reflect.SliceOf(elementType)
		site.RuntimeVariadicNumFixed = safeconv.MustIntToUint8(len(callee.ParameterKinds) - 1)
	}
	result := pushCompiledFrame(vm, registers, site, callee)
	vm.recordFrameSnapshot(vm.FramePointer, snapshot)
	return result, true
}

// updateMethodIC publishes a resolved entry to the inline cache with round-robin
// eviction. Promoted-method resolutions are skipped because the cache cannot replay the
// field-walk.
//
// Takes site (*program.CallSite) which is the call site to update.
// Takes recvType (reflect.Type) which is the concrete receiver type.
// Takes functionIndex (uint16) which is the resolved function index.
// Takes callee (*program.CompiledFunction) which is the resolved function.
// Takes wasPromoted (bool) which skips the update when true.
func updateMethodIC(site *program.CallSite, recvType reflect.Type, functionIndex uint16, callee *program.CompiledFunction, wasPromoted bool) {
	if wasPromoted {
		return
	}
	for i := range site.MethodICSlots {
		pointer := atomic.LoadPointer(&site.MethodICSlots[i])
		if pointer == nil {
			continue
		}
		entry := (*program.MonomorphicCacheEntry)(pointer)
		if entry.ReceiverType == recvType {
			return
		}
	}
	victim := atomic.AddUint32(&site.MethodICVictim, 1) & program.MethodICVictimMask
	entry := &program.MonomorphicCacheEntry{
		ReceiverType:     recvType,
		ReceiverTypeWord: uintptr(reflectValueABIType(recvType)),
		FunctionIndex:    functionIndex,
		Callee:           callee,
	}
	atomic.StorePointer(&site.MethodICSlots[victim], unsafe.Pointer(entry))
}

// runCachedLeaf runs the tiny-leaf method named by an inline cache in place.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes registers (*Registers) which holds the caller's register banks.
// Takes site (*CallSite) which describes argument and return locations.
// Takes callee (*CompiledFunction) which is the cached method.
//
// Returns OpResult which indicates the next execution step when the leaf ran.
// Returns bool which is false when the method is no tiny leaf or could not run in place.
func runCachedLeaf(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) (OpResult, bool) {
	if callee.TinyLeafShape == program.TinyLeafNone {
		return opContinue, false
	}
	return runTinyLeafInline(vm, registers, site, callee)
}

// runFusedOrFramed calls the method an inline cache named through the fused evaluator
// when its body is one of that evaluator's shapes, and in a frame otherwise.
//
// Takes vm (*VM) which provides the call stack.
// Takes registers (*Registers) which holds the caller's register banks.
// Takes site (*CallSite) which describes argument and return locations.
// Takes callee (*CompiledFunction) which is the cached method.
//
// Returns OpResult indicating the next execution step.
func runFusedOrFramed(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	classifyFusedEvalShape(callee)
	if callee.EvalShape > program.FusedEvalNone {
		if result, fused := runFusedEvalShape(registers, site, callee); fused {
			return result
		}
	}
	return pushCompiledFrame(vm, registers, site, callee)
}

// pushCompiledFrame pushes a compiled-function frame and copies its arguments.
//
// Takes vm (*VM) which provides the call stack.
// Takes registers (*Registers) which holds the caller's register banks.
// Takes site (*CallSite) which describes argument and return locations.
// Takes callee (*CompiledFunction) which is the function to call.
//
// Returns OpResult indicating the next execution step.
func pushCompiledFrame(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	if vm.FramePointer >= vm.callDepthLimit() {
		return opStackOverflow
	}
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	f := &vm.CallStack[vm.FramePointer]
	if vm.Arena != nil {
		vm.Arena.SaveInto(&f.arenaSave)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&f.Registers, &callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
	} else {
		f.Registers = NewRegisters(callee.NumRegisters)
	}
	f.Function = callee
	f.ProgramCounter = 0
	f.returnDestination = site.Returns
	f.deferBase = len(vm.deferStack)
	if f.simpleDefer != nil {
		f.simpleDefer.active = false
	}
	f.upvalues = nil
	f.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
	vm.recordFrameSnapshot(vm.FramePointer, nil)

	if callee.IsVariadic && site.RuntimeVariadicSliceType == nil && !site.IsEllipsisSpread && len(callee.ParameterKinds) > 0 && len(site.Arguments) >= len(callee.ParameterKinds)-1 {
		lastKind := callee.ParameterKinds[len(callee.ParameterKinds)-1]
		elementType := program.KindDefaultReflectType(lastKind)
		site.RuntimeVariadicSliceType = reflect.SliceOf(elementType)
		site.RuntimeVariadicNumFixed = safeconv.MustIntToUint8(len(callee.ParameterKinds) - 1)
	}
	if site.RuntimeVariadicSliceType != nil && callee.IsVariadic {
		copyCallArgsWithVariadicPacking(registers, f, site, callee, vm.Arena)
	} else {
		copyCallArgs(vm, vm.Arena, registers, f, site, callee)
	}
	copyValueReceiverThroughPointer(vm, f, callee)
	return opFrameChanged
}

// copyValueReceiverThroughPointer gives a value-receiver method the copy Go makes when it
// is reached through a pointer.
//
// An interface holding *S dispatches S.Put with a copy of *S, so writes inside Put never
// reach the caller's value. Static call sites dereference at compile time; this covers
// dynamic dispatch.
//
// Takes vm (*VM) which owns the arena the copy is placed in.
// Takes frame (*CallFrame) which is the callee frame whose receiver slot is patched.
// Takes callee (*program.CompiledFunction) which is the method being entered.
func copyValueReceiverThroughPointer(vm *VM, frame *CallFrame, callee *program.CompiledFunction) {
	if !callee.HasReceiver || callee.IsPointerReceiver || len(callee.ParameterKinds) == 0 ||
		callee.ParameterKinds[0] != isa.RegisterGeneral || len(frame.Registers.General) == 0 {
		return
	}
	receiver := frame.Registers.General[0]
	if !receiver.IsValid() || receiver.Kind() != reflect.Pointer || receiver.IsNil() || receiver.Type() == runtimeClosureReflectType {
		return
	}
	frame.Registers.General[0] = valueCopyForBoundaryArenaWithVM(vm.Arena, vm, receiver.Elem())
}

// resolvePromotedMethod searches embedded fields for a method.
//
// Used when direct method table lookup fails because the method is promoted from an
// embedded type.
//
// Takes vm (*VM) which provides access to the root function's method table.
// Takes receiver (reflect.Value) which is the value whose fields are searched.
// Takes methodName (string) which is the method name to locate.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the embedded receiver value.
// Returns bool which is true when the method was found.
func resolvePromotedMethod(vm *VM, receiver reflect.Value, methodName string) (uint16, reflect.Value, bool) {
	search := promotionSearch{vm: vm, methodName: methodName, interfaceHops: 0, reachedNil: false}
	return search.at(receiver, 0)
}

// promotedMethodFailure raises a panic when a promoted method cannot be reached.
//
// As in Go, it raises a nil pointer dereference when the path ran into a nil embedded
// interface or pointer, or a stack overflow when it crossed more embedded interfaces than
// the call depth limit allows (each crossing is a wrapper frame in Go). Otherwise it
// raises an internal error, since the type checker accepted the selector.
//
// Takes vm (*VM) which receives the error.
// Takes search (*promotionSearch) which is the finished search.
// Takes tableName (string) which names the method in the internal error.
//
// Returns OpResult which propagates the raised panic.
func promotedMethodFailure(vm *VM, search *promotionSearch, tableName string) OpResult {
	switch {
	case search.reachedNil:
		return raiseNilPointerDereference(vm)
	case search.interfaceHops > vm.callDepthLimit():
		vm.evalError = vm.stackOverflowError()
	default:
		vm.evalError = newInvariantError("undefined method: %s", tableName)
	}
	return opPanicError
}

// lookupMethodTableForReceiver returns the method-table func index.
//
// Resolves methodName on the receiver's concrete type, deriving the pipit type name via
// reflect.Type.Name() or the typeNames fallback.
//
// Takes vm (*VM) which provides the RootFunction MethodTable.
// Takes receiver (reflect.Value) which carries the concrete receiver value.
// Takes methodName (string) which is the unqualified method name.
//
// Returns uint16 which is the function index on hit.
// Returns bool which is true on hit.
func lookupMethodTableForReceiver(vm *VM, receiver reflect.Value, methodName string) (uint16, bool) {
	receiverType := receiver.Type()
	if receiverType.Kind() == reflect.Pointer {
		receiverType = receiverType.Elem()
	}
	typeName := receiverType.Name()
	if info, ok := typemodel.LookupNamedTypeInfo(receiverType); ok {
		typeName = info.BareName
	}
	if typeName == "" && vm.rootFunction.TypeNames != nil {
		typeName = vm.rootFunction.TypeNames[receiverType]
	}
	if typeName == "" {
		typeName = bareSentinelName(receiverType)
	}
	if typeName == "" {
		return 0, false
	}
	functionIndex, ok := vm.rootFunction.MethodTable()[typeName+"."+methodName]
	return functionIndex, ok
}

// lookupMethodInlineCache resolves a receiver type word against the call site's cache.
//
// The single-entry lastReceiver pair is checked before the slot array: a monomorphic
// site, which is the overwhelming majority, then costs one comparison. A slot hit
// promotes itself into that pair so the next dispatch takes the short path.
//
// Takes site (*CallSite) which owns the cache.
// Takes typeWord (uintptr) which is the receiver's dispatch key; zero never matches.
//
// Returns the cached callee, or nil when the site has not seen this receiver type.
func lookupMethodInlineCache(site *program.CallSite, typeWord uintptr) *program.CompiledFunction {
	if typeWord == 0 {
		return nil
	}
	if typeWord == site.LastReceiverTypeWord {
		return site.LastReceiverCallee
	}
	for slotIndex := range site.MethodICSlots {
		pointer := atomic.LoadPointer(&site.MethodICSlots[slotIndex])
		if pointer == nil {
			continue
		}
		entry := (*program.MonomorphicCacheEntry)(pointer)
		if entry.ReceiverTypeWord == typeWord {
			site.LastReceiverTypeWord = typeWord
			site.LastReceiverCallee = entry.Callee
			return entry.Callee
		}
	}
	return nil
}

// isNilInterfaceReceiver reports whether a method receiver is a nil interface value,
// which makes the call Go's nil dereference, recoverable by the program's defers.
//
// Takes receiver (reflect.Value) which is the receiver register's value.
//
// Returns bool which is true for a zero or nil-interface value.
func isNilInterfaceReceiver(receiver reflect.Value) bool {
	return !receiver.IsValid() || (receiver.Kind() == reflect.Interface && receiver.IsNil())
}
