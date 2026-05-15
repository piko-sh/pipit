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
	"sync/atomic"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

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
// an already-resolved call site. Extracted from handleCallMethod so paths that arrive at
// a *CallSite by other means (handleCallMethodInlineable, inline-binop inner-call
// dispatch) reuse the same dispatch flow without re-reading from the bytecode stream.
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
	hit := lookupMethodInlineCache(site, typeWord)
	if hit != nil {
		vm.MethodCallGoDispatches++

		valueMethodThroughPointer := !hit.IsPointerReceiver && receiver.IsValid() && receiver.Kind() == reflect.Pointer
		if !valueMethodThroughPointer {
			vm.tryPopulateMethodASMInfo(frame.Function, site, hit, typeWord)
			if hit.TinyLeafShape != program.TinyLeafNone {
				return runTinyLeafInline(vm, registers, site, hit)
			}
		}
		classifyFusedEvalShape(hit)
		if hit.EvalShape > program.FusedEvalNone {
			if result, fused := runFusedEvalShape(registers, site, hit); fused {
				return result
			}
		}
		return pushCompiledFrame(vm, registers, site, hit)
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
		if closure.namedType != "" {
			typeName = closure.namedType
		}
	}
	tableName := typeName + "." + methodName
	functionIndex, ok := vm.rootFunction.MethodTable()[tableName]
	wasPromoted := false
	receiverValue := receiver.value
	if !ok {
		functionIndex, receiverValue, ok = resolvePromotedMethod(vm, receiverValue, methodName)
		wasPromoted = ok
	}
	if !ok {
		receiver.value = receiverValue
		return resolveMethodCallExternalFallback(vm, registers, site, receiver, methodName, tableName, wasPromoted)
	}
	if int(functionIndex) >= len(vm.functions) {
		vMBoundsError(vm, frame, boundsTableFunction, int(functionIndex), len(vm.functions))
		return opPanicError
	}
	callee := vm.functions[functionIndex]

	updateMethodIC(site, receiver.typ, functionIndex, callee, wasPromoted || closureReceiver)
	if !wasPromoted {
		return pushCompiledFrame(vm, registers, site, callee)
	}

	original := registers.General[receiver.register]
	registers.General[receiver.register] = receiverValue
	result := pushCompiledFrame(vm, registers, site, callee)
	registers.General[receiver.register] = original
	return result
}

// resolveMethodCallExternalFallback handles the slow-path fallbacks.
//
// Used by resolveMethodCallSlow when the local MethodTable and the promoted-method lookup
// both miss. Split out so the parent stays within the cognitive-complexity and
// function-length budgets; this slow path is only taken on IC misses for methods that the
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
// Takes methodName (string) which is the unqualified method name.
// Takes tableName (string) which is the "TypeName.MethodName" key.
// Takes wasPromoted (bool) which signals promotion through embedded fields.
//
// Returns the dispatch OpResult.
func resolveMethodCallExternalFallback(
	vm *VM,
	registers *Registers,
	site *program.CallSite,
	receiver methodCallReceiver,
	methodName string,
	tableName string,
	wasPromoted bool,
) OpResult {
	if result, dispatched := tryExternalMethodTable(vm, registers, site, receiver, tableName, wasPromoted); dispatched {
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
	if lookupErr != nil {
		vm.evalError = newInvariantError("undefined method: %s", tableName)
		return opPanicError
	}
	if nativeMethod.IsValid() {
		return handleCallBoundMethodReflect(vm, registers, site, nativeMethod)
	}
	vm.evalError = newInvariantError("undefined method: %s", tableName)
	return opPanicError
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
// Takes tableName (string) which is the "TypeName.MethodName" key.
// Takes wasPromoted (bool) which is the IC-cache hint forwarded to updateMethodIC.
//
// Returns the dispatch OpResult and true when the lookup hit and the call was queued;
// otherwise (_, false) so the caller continues with the reflect-fallback chain.
func tryExternalMethodTable(
	vm *VM,
	registers *Registers,
	site *program.CallSite,
	receiver methodCallReceiver,
	tableName string,
	wasPromoted bool,
) (OpResult, bool) {
	if vm.Globals == nil {
		return opContinue, false
	}
	entry, ok := vm.Globals.lookupExternalMethod(tableName)
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

// pushCompiledFrame pushes a new call frame for a compiled function.
//
// Copies arguments from the caller's registers to the callee's frame.
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
		vm.Arena.allocRegistersIntoCached(&f.Registers, callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
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
	return resolvePromotedMethodAtDepth(vm, receiver, methodName, 0)
}

// resolvePromotedMethodAtDepth is the depth-bounded implementation.
//
// Provides the recursive body for resolvePromotedMethod.
//
// Takes vm (*VM) which provides the method table.
// Takes receiver (reflect.Value) which is the value whose embedded fields are searched.
// Takes methodName (string) which is the method name to locate.
// Takes depth (int) which tracks the current recursion depth.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the embedded receiver value.
// Returns bool which is true when the method was found.
func resolvePromotedMethodAtDepth(vm *VM, receiver reflect.Value, methodName string, depth int) (uint16, reflect.Value, bool) {
	if depth >= maxPromotedMethodDepth {
		return 0, receiver, false
	}
	value := receiver
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() == reflect.Interface {
		return resolvePromotedMethodOnInterface(vm, value, receiver, methodName, depth)
	}
	if value.Kind() != reflect.Struct {
		return 0, receiver, false
	}
	for ft, field := range value.Fields() {
		if !program.IsAnonymousField(ft) {
			continue
		}

		if functionIndex, resolved, ok := resolvePromotedMethodOnEmbeddedField(vm, ft, launderReadOnlyFieldValue(field), methodName, depth); ok {
			return functionIndex, resolved, true
		}
	}
	return 0, receiver, false
}

// resolvePromotedMethodOnInterface unwraps an interface receiver.
//
// Resolves the method against the concrete value, recursing for further promotion via
// embedded fields.
//
// Takes vm (*VM) which provides the method table.
// Takes value (reflect.Value) which is the unwrapped value at the current depth.
// Takes receiver (reflect.Value) which is the original receiver to return on failure.
// Takes methodName (string) which is the method name to locate.
// Takes depth (int) which tracks the current recursion depth.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the resolved concrete receiver.
// Returns bool which is true when the method was found.
func resolvePromotedMethodOnInterface(vm *VM, value, receiver reflect.Value, methodName string, depth int) (uint16, reflect.Value, bool) {
	if value.IsNil() {
		return 0, receiver, false
	}
	concrete := value.Elem()
	if functionIndex, ok := lookupMethodTableForReceiver(vm, concrete, methodName); ok {
		return functionIndex, concrete, true
	}
	return resolvePromotedMethodAtDepth(vm, concrete, methodName, depth+1)
}

// resolvePromotedMethodOnEmbeddedField walks an anonymous field.
//
// Handles both interface-typed and concrete-typed embeddings and recurses into the
// field's own embedded chain on miss.
//
// Takes vm (*VM) which provides the method table.
// Takes ft (reflect.StructField) which is the field metadata.
// Takes field (reflect.Value) which is the field value.
// Takes methodName (string) which is the method name to locate.
// Takes depth (int) which tracks the current recursion depth.
//
// Returns uint16 which is the function index when found.
// Returns reflect.Value which is the resolved receiver.
// Returns bool which is true when the field produced a match.
func resolvePromotedMethodOnEmbeddedField(vm *VM, ft reflect.StructField, field reflect.Value, methodName string, depth int) (uint16, reflect.Value, bool) {
	fieldType := ft.Type
	if fieldType.Kind() == reflect.Pointer {
		fieldType = fieldType.Elem()
		field = field.Elem()
	}
	if fieldType.Kind() == reflect.Interface {
		if !field.IsValid() || field.IsNil() {
			return 0, field, false
		}
		concrete := field.Elem()
		if functionIndex, ok := lookupMethodTableForReceiver(vm, concrete, methodName); ok {
			return functionIndex, concrete, true
		}
		if functionIndex, embedded, ok := resolvePromotedMethodAtDepth(vm, concrete, methodName, depth+1); ok {
			return functionIndex, embedded, true
		}
		return 0, field, false
	}
	if !field.IsValid() {
		return 0, field, false
	}
	if functionIndex, ok := lookupMethodTableForReceiver(vm, field, methodName); ok {
		return functionIndex, field, true
	}

	if functionIndex, ok := vm.rootFunction.MethodTable()[ft.Name+"."+methodName]; ok {
		return functionIndex, field, true
	}
	if functionIndex, embedded, ok := resolvePromotedMethodAtDepth(vm, field, methodName, depth+1); ok {
		return functionIndex, embedded, true
	}
	return 0, field, false
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
	if info, ok := typemodel.LookupNamedScalarPoolInfo(receiverType); ok {
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

// receiverDispatchTypeWord returns the type word the method inline cache keys on.
//
// A pointer receiver is keyed on its element type, so *T and T reach the same cache
// entry: the method set the call site resolved against is the same either way.
//
// Takes receiver (*reflect.Value) which is the receiver register's value, taken by
// pointer so the raw header can be read without copying it.
//
// Returns the type word, or zero when the receiver carries no type.
func receiverDispatchTypeWord(receiver *reflect.Value) uintptr {
	raw := (*unsafeReflectValue)(unsafe.Pointer(receiver))
	typeWord := uintptr(raw.typ)
	if typeWord == 0 {
		return 0
	}
	if reflect.Kind(*(*uint8)(unsafe.Add(raw.typ, program.AbiTypeKindByteOffset))&uint8(flagKindMask)) == reflect.Pointer {
		typeWord = *(*uintptr)(unsafe.Add(raw.typ, program.AbiTypePointerElemOffset))
	}
	return typeWord
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
