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
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/safeconv"
)

// setFinalizerPointer identifies runtime.SetFinalizer among native callees.
var setFinalizerPointer = reflect.ValueOf(runtime.SetFinalizer).Pointer()

// argumentTypeContext carries the per-argument static-type metadata extracted from a
// CallSite into coerceReflectArgument so the adapter-selection path can identify
// named-primitive arguments that share an underlying reflect.Type (e.g. type Colour int
// and type Speed int both map to the int64 reflect.Type at runtime).
type argumentTypeContext struct {
	// staticTypeName is the named type's bare identifier (empty when the argument has no
	// named type).
	staticTypeName string

	// staticTypeString is the fully qualified types.Type.String() form used to disambiguate
	// same-name types from different packages.
	staticTypeString string

	// keepClosures leaves an interpreted closure as its *RuntimeClosure when the parameter
	// is an interface: fmt never calls a func argument, and the raw closure is what lets %T
	// name the closure's static type and %v print an address.
	keepClosures bool

	// skipInterfaceAdapter suppresses interface adapter wrapping. Set when the call target
	// is a pipit-side MakeFunc closure whose interface-typed parameters are an internal
	// type-erasure device, not a genuine interface{} sink.
	skipInterfaceAdapter bool
}

// nativeArgShaping carries what buildReflectArgs needs to coerce each operand of one
// native call: the callee's parameter types and the call-site classification (fmt
// printing or scanning, encoding marshaller, MakeFunc closure).
type nativeArgShaping struct {
	// parameterTypes lists the callee's declared parameter types.
	parameterTypes []reflect.Type

	// variadicElementType is the element type of a variadic parameter, or nil when the
	// callee is not variadic.
	variadicElementType reflect.Type

	// fmtVerbs holds the per-argument verb bytes extracted from the format string, or nil
	// when the call is not an fmt print.
	fmtVerbs []byte

	// parameterCount is len(parameterTypes) cached for index arithmetic.
	parameterCount int

	// fmtArguments is true when the callee is an fmt function that accepts a format string
	// followed by variadic arguments.
	fmtArguments bool

	// fmtPrinting is true for fmt printing (not scanning) calls.
	fmtPrinting bool

	// encodingArguments is true when the callee is an encoding marshaller whose arguments
	// carry struct tags.
	encodingArguments bool

	// skipInterfaceAdapter suppresses interface adapter wrapping for callees that accept
	// reflect-level values directly.
	skipInterfaceAdapter bool
}

// newNativeArgShaping reads the call site's cached classification.
//
// Takes registers (*Registers) which hold the operands (the format string, when any).
// Takes site (*program.CallSite) which describes the call.
// Takes reflectedFunction (reflect.Value) which is the native callee.
//
// Returns nativeArgShaping which shapes the operands.
func newNativeArgShaping(registers *Registers, site *program.CallSite, reflectedFunction reflect.Value) nativeArgShaping {
	parameterTypes := site.NativeParamTypes()
	shaping := nativeArgShaping{
		parameterTypes:       parameterTypes,
		variadicElementType:  variadicElementTypeForSite(site),
		fmtVerbs:             nil,
		parameterCount:       len(parameterTypes),
		fmtArguments:         false,
		fmtPrinting:          false,
		encodingArguments:    false,
		skipInterfaceAdapter: isPipitMakeFunctionClosure(reflectedFunction),
	}
	if cache := site.NativeParamCacheLoad(); cache != nil {
		shaping.fmtArguments = cache.FmtArguments
		shaping.fmtPrinting = cache.FmtArguments && !cache.FmtScan
		shaping.encodingArguments = cache.EncodingArguments
	}
	if shaping.fmtPrinting {
		shaping.fmtVerbs = fmtVerbsForCall(registers, site, parameterTypes)
	}
	return shaping
}

// shape coerces operand i for its parameter: a spread slice as the variadic slice, a
// variadic operand as the element type (then wrapped for fmt), a fixed operand as its
// parameter type (then shadowed for an encoder).
//
// Takes vm (*VM) which resolves adapters.
// Takes site (*program.CallSite) which describes the call.
// Takes i (int) which is the operand index.
// Takes raw (reflect.Value) which is the operand as read from its register.
//
// Returns reflect.Value which is the operand to pass.
func (shaping nativeArgShaping) shape(vm *VM, site *program.CallSite, i int, raw reflect.Value) reflect.Value {
	typeCtx := argumentTypeContextFromSite(site, i)
	typeCtx.skipInterfaceAdapter = shaping.skipInterfaceAdapter
	typeCtx.keepClosures = shaping.fmtArguments
	variadic := i >= shaping.parameterCount-1
	if shaping.fmtPrinting && variadic {
		typeCtx.skipInterfaceAdapter = true
	}
	switch {
	case site.IsEllipsisSpread && i == shaping.parameterCount-1 && shaping.parameterCount > 0 && shaping.parameterTypes[shaping.parameterCount-1].Kind() == reflect.Slice:
		return coerceVariadicSpreadSlice(vm, raw, shaping.parameterTypes[shaping.parameterCount-1])
	case shaping.variadicElementType != nil && variadic:
		argument := coerceReflectArgument(vm, raw, shaping.variadicElementType, typeCtx)
		if shaping.fmtPrinting {
			return wrapFmtArgumentForVerb(vm, argument, fmtVerbAt(shaping.fmtVerbs, i-(shaping.parameterCount-1)), typeCtx.staticTypeName)
		}
		if shaping.fmtArguments {
			return wrapFmtArgument(vm, argument, typeCtx.staticTypeName)
		}
		return argument
	case i < shaping.parameterCount:
		argument := coerceReflectArgument(vm, raw, shaping.parameterTypes[i], typeCtx)
		if shaping.encodingArguments && isEmptyInterfaceType(shaping.parameterTypes[i]) {
			return shadowForMarshal(vm, argument)
		}
		return argument
	default:
		return raw
	}
}

// buildNativeBackedErasurePointees scans every registered symbol for
// link.NativeBackedGenericType sentinels and collects the set of pointee types that
// delimit a genuine erasure boundary. The result is cached on the VM so the scan runs at
// most once.
//
// Takes the receiver vm (*VM) whose symbol registry is scanned.
//
// Returns the set of erased and erasure-argument pointee types; never nil so the caller's
// nil check memoises a completed scan.
func (vm *VM) buildNativeBackedErasurePointees() map[reflect.Type]struct{} {
	pointees := make(map[reflect.Type]struct{})
	if vm.symbols == nil {
		return pointees
	}
	for _, packagePath := range vm.symbols.AllPackages() {
		symbols, ok := vm.symbols.PackageSymbols(packagePath)
		if !ok {
			continue
		}
		for _, value := range symbols {
			collectErasurePointees(value, pointees)
		}
	}
	return pointees
}

// ownsInterpreterStorage reports whether p addresses memory the interpreter manages
// rather than a Go heap allocation: a register-arena cell, slab or slice backing, or a
// slot in a boundary snapshot chunk.
//
// Takes p (unsafe.Pointer) which is the address to classify.
//
// Returns bool which is true for interpreter-owned storage.
func (vm *VM) ownsInterpreterStorage(p unsafe.Pointer) bool {
	if vm.Arena != nil && (vm.Arena.ownsIndirectCell(p) || vm.Arena.OwnsSliceBacking(p) || vm.Arena.ownsScalarBox(p)) {
		return true
	}
	return vm.ownsBoundarySnapshot(p)
}

// loadNativeFastPath atomically reads a call site's published fast-path entry, or nil
// when the site has not been probed yet.
//
// Takes site (*CallSite) whose nativeFastPath pointer is read.
//
// Returns the published *nativeFastPathEntry, or nil.
func loadNativeFastPath(site *program.CallSite) *nativeFastPathEntry {
	return (*nativeFastPathEntry)(atomic.LoadPointer(&site.NativeFastPath))
}

// handleCallNative dispatches a call to a native Go function.
//
// The mechanisms are tried in this order, and the order is the contract:
//
//  1. The site's published fast path (loadNativeFastPath): a typed dispatcher chosen on
//     an earlier call, used only when no capability hook is installed and the site
//     carries no linked-generic type arguments.
//  2. Linked generics (handleCallLinkedReflect) when the site carries type arguments.
//  3. An interpreted closure stored in the function register (handleCallNativeClosure).
//  4. runtime.Goexit, matched by code pointer (handleRuntimeGoexit).
//  5. Fast-path classification (tryClassifyNativeFastPath) on the first call to a site
//     that has no published entry yet and no capability hook: it probes the function's
//     signature, publishes the dispatcher for later calls and runs it now.
//  6. Reflect invocation (handleCallNativeReflect): argument marshalling with the
//     interface adapters, the fmt intercept and the GIL handoff, then reflect.Value.Call.
//
// A capability hook disables 1 and 5 so every guarded call passes through the hook in 6.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes frame (*CallFrame) which is the current call frame.
// Takes registers (*Registers) which holds the current register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
func handleCallNative(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]

	if site.BlocksHostGoroutine && (vm.holdsInterpreterLock || vm.Limits.Debug != nil) {
		released := vm.releaseAroundBlock()
		defer vm.reacquireAfterBlock(released)
	}

	hookInstalled := vm.Limits.CapabilityHook != nil
	fastPath := loadNativeFastPath(site)
	if !hookInstalled && fastPath != nil && fastPath.fn != nativeFastPathNone && len(site.LinkedTypeArgs) == 0 {
		return dispatchCachedNativeFastPath(vm, site, registers, fastPath)
	}

	reflectedFunction := registers.General[site.NativeRegister]
	if !reflectedFunction.IsValid() {
		panicHandleCallNativeZeroValue(frame, registers, site, siteIndex)
	}

	if len(site.LinkedTypeArgs) > 0 {
		return handleCallLinkedReflect(vm, registers, site, reflectedFunction)
	}

	v := reflectedFunction.Interface()

	if closure, ok := v.(*RuntimeClosure); ok {
		return handleCallNativeClosure(vm, registers, site, closure)
	}

	if reflectedFunction.Pointer() == runtimeGoexitPointer {
		return handleRuntimeGoexit(vm)
	}

	if !hookInstalled && fastPath == nil {
		if rc, handled := tryClassifyNativeFastPath(vm, registers, site, v); handled {
			return rc
		}
	}

	return handleCallNativeReflect(vm, registers, site, reflectedFunction)
}

// tryClassifyNativeFastPath classifies a native call's fast path.
//
// On a hit, caches the function and tag on site, records the method receiver pointer, and
// routes any captured panic through the interpreted panic path. handled=false when the
// classifier did not match a known signature and the caller should fall back to
// reflect-driven dispatch.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes registers (*Registers) which holds the current register banks.
// Takes site (*CallSite) which describes the call site metadata.
// Takes v (any) which is the unwrapped native callable.
//
// Returns OpResult which is the dispatch outcome.
// Returns bool which is true when the fast path was taken.
func tryClassifyNativeFastPath(vm *VM, registers *Registers, site *program.CallSite, v any) (OpResult, bool) {
	if isCallStackIntrinsic(v) {
		atomic.StorePointer(&site.NativeFastPath, unsafe.Pointer(nativeFastPathNoneEntry))
		return opContinue, false
	}
	vm.Globals.dispatchDepth.Add(1)
	ok, tag, panicValue := tryNativeFastPath(vm, site, v, registers)
	vm.Globals.dispatchDepth.Add(-1)
	if !ok {
		return opContinue, false
	}
	entry := &nativeFastPathEntry{fn: v, tag: tag, receiverAddr: 0}
	if site.IsMethod {
		if receiver := registers.General[site.MethodReceiverRegister]; receiver.CanAddr() {
			entry.receiverAddr = receiver.Addr().Pointer()
		}
	}
	atomic.StorePointer(&site.NativeFastPath, unsafe.Pointer(entry))
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue), true
	}
	return opContinue, true
}

// panicHandleCallNativeZeroValue raises Go's nil-dereference panic when the function
// register a native call site names holds no value: the program called a nil func.
//
// Takes the frame, registers, site and site index for signature compatibility with the
// diagnostic form this replaced; they are not read.
//
// Panics with a recoverable runtime error.
func panicHandleCallNativeZeroValue(_ *CallFrame, _ *Registers, _ *program.CallSite, _ uint16) {
	panic(newRuntimePanicError(nilDereferenceMessage))
}

// dispatchCachedNativeFastPath handles the case where a native call site already has a
// cached fast-path function. For method calls it validates the receiver address and
// refreshes the cache when the receiver has moved.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes site (*CallSite) which provides the call site metadata.
// Takes registers (*Registers) which holds the current register banks.
// Takes entry (*nativeFastPathEntry) which is the published fast-path cache.
//
// Returns OpResult after dispatching the fast-path call.
func dispatchCachedNativeFastPath(vm *VM, site *program.CallSite, registers *Registers, entry *nativeFastPathEntry) OpResult {
	if !site.IsMethod {
		dispatchNativeFastPathTagged(vm, entry.tag, entry.fn, site, registers)
		return opContinue
	}
	receiver := registers.General[site.MethodReceiverRegister]
	if receiver.CanAddr() && receiver.Addr().Pointer() == entry.receiverAddr {
		dispatchNativeFastPathTagged(vm, entry.tag, entry.fn, site, registers)
		return opContinue
	}
	reflectedFunction := registers.General[site.NativeRegister]
	refreshed := &nativeFastPathEntry{fn: reflectedFunction.Interface(), tag: entry.tag, receiverAddr: 0}
	if receiver.CanAddr() {
		refreshed.receiverAddr = receiver.Addr().Pointer()
	}
	atomic.StorePointer(&site.NativeFastPath, unsafe.Pointer(refreshed))
	dispatchNativeFastPathTagged(vm, refreshed.tag, refreshed.fn, site, registers)
	return opContinue
}

// handleCallNativeClosure invokes a compiled closure that was resolved from a native call
// site by pushing a new frame and copying arguments.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes registers (*Registers) which holds the current register banks.
// Takes site (*CallSite) which describes argument and return locations.
// Takes closure (*RuntimeClosure) which is the closure to invoke.
//
// Returns OpResult indicating the next execution step.
func handleCallNativeClosure(vm *VM, registers *Registers, site *program.CallSite, closure *RuntimeClosure) OpResult {
	callee := closure.Function
	if vm.FramePointer >= vm.callDepthLimit() {
		return opStackOverflow
	}
	snapshot := vm.swapToClosureRoot(closure.RootFunction)
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
	vm.recordFrameSnapshot(vm.FramePointer, snapshot)
	if closure.upvalues != nil {
		f.initialiseUpvalues(closure.upvalues, vm.Arena)
	}

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
	return opFrameChanged
}

// copyCallArgsWithVariadicPacking copies fixed args and packs the remainder into the
// variadic slice.
//
// Used when a cross-package compiled callee is variadic and the source did not use
// ellipsis spread, since copyCallArgs alone would only transfer one register per
// parameter slot and leave the trailing values stranded.
//
// Takes callerRegisters (*Registers) which holds the caller's argument values.
// Takes newFrame (*CallFrame) which is the callee's freshly allocated frame.
// Takes site (*CallSite) which describes argument locations and carries the variadic
// slice type set during compilation.
// Takes callee (*CompiledFunction) which is the variadic callee whose parameter kinds
// determine destination registers.
// Takes arena (*RegisterArena) which is the arena for register allocation.
func copyCallArgsWithVariadicPacking(callerRegisters *Registers, newFrame *CallFrame, site *program.CallSite, callee *program.CompiledFunction, arena *RegisterArena) {
	fixedCount := max(min(int(site.RuntimeVariadicNumFixed), len(callee.ParameterKinds)-1), 0)

	var kindIndex [isa.NumRegisterKinds]int
	scattered := len(callee.ParameterRegisters) == len(callee.ParameterKinds)
	for i := 0; i < fixedCount && i < len(site.Arguments); i++ {
		parameterKind := callee.ParameterKinds[i]
		dest := kindIndex[parameterKind]
		kindIndex[parameterKind]++
		if scattered {
			dest = int(callee.ParameterRegisters[i])
		}
		argumentLocation := site.Arguments[i]
		copyOneCallArgument(&newFrame.Registers, callerRegisters, parameterKind, argumentLocation.Kind, dest, argumentLocation.Register, arena)
	}

	sliceType := site.RuntimeVariadicSliceType
	elementType := sliceType.Elem()
	variadicCount := max(len(site.Arguments)-fixedCount, 0)
	packed := reflect.MakeSlice(sliceType, variadicCount, variadicCount)
	for i := range variadicCount {
		argumentLocation := site.Arguments[fixedCount+i]
		value := registerToReflectValue(nil, callerRegisters, argumentLocation.Kind, argumentLocation.Register)
		if value.IsValid() && value.Type() != elementType && value.Type().ConvertibleTo(elementType) {
			value = value.Convert(elementType)
		}
		if value.IsValid() {
			packed.Index(i).Set(value)
		}
	}

	sliceParamIndex := len(callee.ParameterKinds) - 1
	if sliceParamIndex < 0 {
		return
	}
	sliceDestination := kindIndex[isa.RegisterGeneral]
	kindIndex[isa.RegisterGeneral]++
	if scattered {
		sliceDestination = int(callee.ParameterRegisters[sliceParamIndex])
	}
	newFrame.Registers.General[sliceDestination] = packed
}

// handleCallNativeReflect invokes a native function via reflect.Value.Call, building
// arguments from registers and storing results back.
//
// Takes vm (*VM) which is the virtual machine executing the instruction.
// Takes registers (*Registers) which holds the current register banks.
// Takes site (*CallSite) which describes argument and return locations.
// Takes reflectedFunction (reflect.Value) which is the native function to call.
//
// Returns OpResult indicating the next execution step.
//
// Panics if reflectedFunction is a zero reflect.Value.
func handleCallNativeReflect(vm *VM, registers *Registers, site *program.CallSite, reflectedFunction reflect.Value) OpResult {
	if !reflectedFunction.IsValid() {
		panic(newInvariantError(
			"handleCallNativeReflect - function register is zero reflect.Value; "+
				"site has %d arguments and %d returns",
			len(site.Arguments), len(site.Returns),
		))
	}
	if len(site.LinkedTypeArgs) > 0 {
		return handleCallLinkedReflect(vm, registers, site, reflectedFunction)
	}

	if result, taken := tryNativeFastPathDispatch(vm, registers, site, reflectedFunction); taken {
		return result
	}
	cacheParamTypes(site, reflectedFunction)

	var handoff *gilHandoff
	if vm.Limits.SafeMode && vm.holdsInterpreterLock {
		handoff = &gilHandoff{globals: vm.Globals, active: atomic.Bool{}, hasClosure: false}
	}
	previousHandoff := vm.pendingGilHandoff
	vm.pendingGilHandoff = handoff
	arguments := buildReflectArgs(vm, registers, site, reflectedFunction)
	vm.pendingGilHandoff = previousHandoff
	defer releaseReflectValueBuffer(arguments)
	if cache := site.NativeParamCacheLoad(); cache != nil && cache.Sleep {
		return vm.interruptibleSleep(time.Duration(arguments[0].Int()))
	}
	if dispatched, ok := dispatchPipitErrorsIntrinsic(vm, registers, site, reflectedFunction, arguments); ok {
		return dispatched
	}
	if dispatched, ok := dispatchSetFinalizerIntrinsic(vm, reflectedFunction, arguments); ok {
		return dispatched
	}
	if dispatched, ok := dispatchCallStackIntrinsic(vm, registers, site, reflectedFunction, arguments); ok {
		return dispatched
	}
	if denial := consultCapabilityHookForNativeCall(vm, site, reflectedFunction, arguments); denial != nil {
		vm.evalError = denial
		return opPanicError
	}
	unwrapPipitNamedTypeArguments(reflectedFunction, arguments)
	unwrapPipitAdapterArguments(reflectedFunction, arguments)
	shimReflectMakeFuncImpl(reflectedFunction, arguments)
	if err := checkNativeInterfaceArguments(vm, reflectedFunction, arguments, site.IsEllipsisSpread); err != nil {
		vm.evalError = err
		return opPanicError
	}
	results, panicValue, err := invokeNativeUnderHandoff(vm, site, reflectedFunction, arguments, handoff)
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue)
	}
	if err != nil {
		vm.evalError = err
		return opPanicError
	}
	results = applyPipitReflectTypeOfNaming(vm, reflectedFunction, site, results)
	storeReflectResults(registers, site.Returns, results)
	return opContinue
}

// safeReflectCallOrCallSlice dispatches via reflect.Value.CallSlice when the source call
// used the ellipsis spread (so the trailing slice becomes the variadic parameter as-is),
// or reflect.Value.Call otherwise.
//
// Takes function (reflect.Value) which is the function to invoke.
// Takes arguments ([]reflect.Value) which holds the prepared arguments.
// Takes ellipsisSpread (bool) which selects CallSlice vs Call.
//
// Returns the results, any recovered panic value, and the error (wrapped
// errNativeCallPanic when a panic was recovered).
func safeReflectCallOrCallSlice(function reflect.Value, arguments []reflect.Value, ellipsisSpread bool) (results []reflect.Value, panicValue any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results = nil
			panicValue = recovered
			err = fmt.Errorf("%w: %v", errNativeCallPanic, recovered)
		}
	}()
	if ellipsisSpread {
		results = function.CallSlice(arguments)
	} else {
		results = function.Call(arguments)
	}
	return results, nil, nil
}

// dispatchPipitErrorsIntrinsic routes errors.As / errors.Is to pipit's own
// implementations.
//
// The native stdlib functions consult reflect.Type.Implements which rejects
// pipit-synthesised concrete types (their reflect.Type carries an empty MethodSet because
// the methods live in pipit's MethodTable, not on the Go-side reflect.Type). The pipit
// replacements consult the MethodTable directly, so chain walks and second-argument
// matching behave correctly for user-declared error types. Detection is by symbol-pointer
// comparison against the cached errorsAsPointer and errorsIsPointer values at module
// init.
//
// Takes vm (*VM) which provides method registry access.
// Takes registers (*Registers) which holds caller register banks for result storage.
// Takes site (*CallSite) which describes argument/return locations.
// Takes reflectedFunction (reflect.Value) which is the resolved function being called.
// Takes arguments ([]reflect.Value) which are the prepared call arguments, already
// coerced.
//
// Returns the intended OpResult and true when the call was handled; returns opContinue
// and false when the call should fall through to safeReflectCall.
func dispatchPipitErrorsIntrinsic(vm *VM, registers *Registers, site *program.CallSite, reflectedFunction reflect.Value, arguments []reflect.Value) (OpResult, bool) {
	pointer := reflectedFunction.Pointer()
	if pointer == 0 {
		return opContinue, false
	}
	if pointer == errorsUnwrapPointer {
		if len(arguments) < 1 {
			return opContinue, false
		}
		result := pipitErrorsUnwrap(vm, arguments[0])
		storeReflectResults(registers, site.Returns, []reflect.Value{result})
		return opContinue, true
	}
	if pointer != errorsAsPointer && pointer != errorsIsPointer {
		return opContinue, false
	}
	if len(arguments) < 2 {
		return opContinue, false
	}
	matched := false
	if pointer == errorsAsPointer {
		matched = pipitErrorsAs(vm, arguments[0], arguments[1])
	} else {
		matched = pipitErrorsIs(vm, arguments[0], arguments[1])
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(matched)})
	return opContinue, true
}

// handleCallBoundMethodReflect invokes a native method obtained via
// reflect.Value.MethodByName. The method value is already bound to its receiver, so
// arguments[0] (the receiver) must be skipped to avoid passing the receiver twice.
//
// Takes vm (*VM) which provides context for closure coercion.
// Takes registers (*Registers) which holds the source values.
// Takes site (*CallSite) which describes argument and return locations.
// Takes boundMethod (reflect.Value) which is the receiver-bound method.
//
// Returns OpResult indicating the next execution step.
//
// Panics if boundMethod is a zero reflect.Value.
func handleCallBoundMethodReflect(vm *VM, registers *Registers, site *program.CallSite, boundMethod reflect.Value) OpResult {
	if !boundMethod.IsValid() {
		panic(newInvariantError(
			"handleCallBoundMethodReflect - bound method is zero reflect.Value; "+
				"site has %d arguments and %d returns",
			len(site.Arguments), len(site.Returns),
		))
	}
	methodArgs := site.Arguments[1:]
	methodType := boundMethod.Type()
	nArgs := len(methodArgs)
	arguments := acquireReflectValueBuffer(nArgs)
	defer releaseReflectValueBuffer(arguments)
	for i, argumentLocation := range methodArgs {
		arguments[i] = registerToReflectValue(vm.Arena, registers, argumentLocation.Kind, argumentLocation.Register)
		if i < methodType.NumIn() {
			arguments[i] = coerceReflectArgument(vm, arguments[i], methodType.In(i), argumentTypeContext{staticTypeName: "", staticTypeString: "", skipInterfaceAdapter: false, keepClosures: false})
		}
	}
	if denial := consultCapabilityHookForNativeCall(vm, site, boundMethod, arguments); denial != nil {
		vm.evalError = denial
		return opPanicError
	}
	if err := checkNativeInterfaceArguments(vm, boundMethod, arguments, false); err != nil {
		vm.evalError = err
		return opPanicError
	}
	vm.Globals.dispatchDepth.Add(1)
	results, panicValue, err := safeReflectCallWithPanic(boundMethod, arguments)
	vm.Globals.dispatchDepth.Add(-1)
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue)
	}
	if err != nil {
		vm.evalError = err
		return opPanicError
	}
	storeReflectResults(registers, site.Returns, results)
	return opContinue
}

// handleRuntimeGoexit intercepts runtime.Goexit calls from interpreted code. Unwinds the
// frame stack (running defers) and reports fault.ErrGoexit instead of terminating the
// host goroutine.
//
// Takes vm (*VM) which is the virtual machine executing the call.
//
// Returns opPanicError after the goexit unwind completes.
func handleRuntimeGoexit(vm *VM) OpResult {
	vm.unwindGoexit()
	vm.evalError = fault.ErrGoexit
	return opPanicError
}

// safeReflectCallWithPanic invokes function.Call under a recover guard and reports the
// recovered panic value separately from the error, so callers can route the panic through
// the interpreter's defer/recover machinery (raiseNativePanicAsInterpreted) instead of
// surfacing it as a fatal eval error.
//
// Takes function (reflect.Value) which is the function to invoke.
// Takes arguments ([]reflect.Value) which are the prepared arguments.
//
// Returns the result slice; the recovered panic value (if any); and any wrapped
// panic-as-error for callers that don't route panics interpreted.
func safeReflectCallWithPanic(function reflect.Value, arguments []reflect.Value) (results []reflect.Value, panicValue any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results = nil
			panicValue = recovered
			err = fmt.Errorf("%w: %v", errNativeCallPanic, recovered)
		}
	}()
	results = function.Call(arguments)
	return results, nil, nil
}

// cacheParamTypes lazily populates the call site's ParamTypes cache from the function's
// reflect.Type to avoid repeated reflect.Type.In(i) calls.
//
// Takes site (*CallSite) which is the call site to populate.
// Takes reflectedFunction (reflect.Value) which is the native function to inspect.
func cacheParamTypes(site *program.CallSite, reflectedFunction reflect.Value) {
	if site.NativeParamCacheLoad() != nil || len(site.Arguments) == 0 {
		return
	}
	functionType := reflectedFunction.Type()

	site.NativeParamCacheStore(&program.NativeParamCache{
		Types:             slices.Collect(functionType.Ins()),
		IsVariadic:        functionType.IsVariadic(),
		Sleep:             isWallClockSleep(reflectedFunction, functionType),
		FmtArguments:      isFmtVariadicAny(reflectedFunction, functionType),
		FmtScan:           isFmtScanFunction(reflectedFunction),
		EncodingArguments: isEncodingMarshaller(reflectedFunction),
	})
}

// isWallClockSleep reports whether the native is time.Sleep or the wall clock's Sleep the
// service installs for it, which the VM must run as an interruptible wait. A
// host-supplied Clock keeps its own Sleep so deterministic clocks stay deterministic.
//
// Takes reflectedFunction (reflect.Value) which is the native function.
// Takes functionType (reflect.Type) which is its signature.
//
// Returns true for the wall-clock sleep functions.
func isWallClockSleep(reflectedFunction reflect.Value, functionType reflect.Type) bool {
	if functionType.NumIn() != 1 || functionType.NumOut() != 0 || functionType.In(0) != reflect.TypeFor[time.Duration]() {
		return false
	}
	symbol := runtime.FuncForPC(reflectedFunction.Pointer())
	if symbol == nil {
		return false
	}
	name := symbol.Name()
	return name == "time.Sleep" || strings.HasSuffix(name, "internal/clock.wallClock.Sleep-fm")
}

// isEncodingMarshaller reports whether the native is one of the encoding/json or
// encoding/xml marshal entry points, whose `any` arguments get the marshal shadow walk.
//
// Takes reflectedFunction (reflect.Value) which is the native function.
//
// Returns true for json.Marshal, json.MarshalIndent, (*json.Encoder).Encode and the xml
// counterparts.
func isEncodingMarshaller(reflectedFunction reflect.Value) bool {
	symbol := runtime.FuncForPC(reflectedFunction.Pointer())
	if symbol == nil {
		return false
	}
	switch symbol.Name() {
	case "encoding/json.Marshal", "encoding/json.MarshalIndent", "encoding/json.(*Encoder).Encode",
		"encoding/xml.Marshal", "encoding/xml.MarshalIndent", "encoding/xml.(*Encoder).Encode":
		return true
	default:
		return false
	}
}

// isFmtScanFunction reports whether the native is one of package fmt's scanning functions
// (Scan, Scanln, Scanf and their S and F forms), whose variadic operands are pointers to
// fill rather than values to print.
//
// Takes reflectedFunction (reflect.Value) which is the native function value.
//
// Returns bool which is true for a fmt scanning function.
func isFmtScanFunction(reflectedFunction reflect.Value) bool {
	symbol := runtime.FuncForPC(reflectedFunction.Pointer())
	if symbol == nil {
		return false
	}
	name := symbol.Name()
	return strings.HasPrefix(name, "fmt.Scan") || strings.HasPrefix(name, "fmt.Sscan") || strings.HasPrefix(name, "fmt.Fscan")
}

// isFmtVariadicAny reports whether the native is a fmt printing function taking ...any
// (Println, Printf, Fprintln and the rest), whose operands the intercept must wrap.
//
// Takes reflectedFunction (reflect.Value) which is the native function.
// Takes functionType (reflect.Type) which is its signature.
//
// Returns true for fmt's variadic ...any functions.
func isFmtVariadicAny(reflectedFunction reflect.Value, functionType reflect.Type) bool {
	if !functionType.IsVariadic() || functionType.NumIn() == 0 {
		return false
	}
	last := functionType.In(functionType.NumIn() - 1)
	if last.Kind() != reflect.Slice || last.Elem().Kind() != reflect.Interface || last.Elem().NumMethod() != 0 {
		return false
	}
	symbol := runtime.FuncForPC(reflectedFunction.Pointer())
	return symbol != nil && strings.HasPrefix(symbol.Name(), "fmt.")
}

// buildReflectArgs marshals call-site arguments from registers into a []reflect.Value
// slice, coercing types where necessary to match the expected parameter types of the
// target native function.
//
// Takes vm (*VM) which provides context for closure coercion.
// Takes registers (*Registers) which holds the source values.
// Takes site (*CallSite) which describes argument locations and types.
// Takes reflectedFunction (reflect.Value) which is the resolved callee, used to detect
// pipit-side reflect.MakeFunc closures whose interface parameters must not trigger
// stdlib-interface adapter wrapping.
//
// Returns []reflect.Value ready for reflect.Value.Call.
func buildReflectArgs(vm *VM, registers *Registers, site *program.CallSite, reflectedFunction reflect.Value) []reflect.Value {
	arguments := acquireReflectValueBuffer(len(site.Arguments))
	shaping := newNativeArgShaping(registers, site, reflectedFunction)
	for i, argumentLocation := range site.Arguments {
		raw := registerToReflectValue(vm.Arena, registers, argumentLocation.Kind, argumentLocation.Register)
		arguments[i] = shaping.shape(vm, site, i, raw)
	}
	if shaping.fmtArguments {
		rewriteFmtTypeVerbs(site, arguments, shaping.parameterTypes)
	}
	return arguments
}

// rewriteFmtTypeVerbs rewrites %T verbs in the format string so that Printf, Fprintf and
// Errorf name script types correctly.
//
// fmt resolves %T without consulting Formatter, so rewriting the verb before the call is
// the only way to print a pool-backed named type.
//
// Takes site (*program.CallSite) which carries the static argument type strings.
// Takes arguments ([]reflect.Value) which are the coerced call arguments, edited in
// place.
// Takes parameterTypes ([]reflect.Type) which are the native's parameter types.
func rewriteFmtTypeVerbs(site *program.CallSite, arguments []reflect.Value, parameterTypes []reflect.Type) {
	parameterCount := len(parameterTypes)
	formatIndex := parameterCount - 2
	if formatIndex < 0 || parameterTypes[formatIndex].Kind() != reflect.String || formatIndex >= len(arguments) {
		return
	}
	format := arguments[formatIndex].String()
	if !strings.ContainsRune(format, 'T') {
		return
	}
	variadicStart := parameterCount - 1
	if site.IsEllipsisSpread {
		rewriteFmtTypeVerbsSpread(site, arguments, formatIndex, variadicStart, format)
		return
	}
	varArgs := make([]any, 0, len(arguments)-variadicStart)
	for _, argument := range arguments[variadicStart:] {
		varArgs = append(varArgs, interfaceOrNil(argument))
	}
	rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, variadicStart, format, varArgs)
	if !intercepted {
		return
	}
	arguments[formatIndex] = reflect.ValueOf(rewrittenFormat)
	for i, rewritten := range rewrittenArgs {
		if text, ok := rewritten.(string); ok {
			arguments[variadicStart+i] = reflect.ValueOf(text)
		}
	}
}

// rewriteFmtTypeVerbsSpread rewrites %T verbs for a call that spreads a slice into the
// variadic parameter (e.g. fmt.Sprintf(format, args...)).
//
// Elements come from the script's slice, so rewrites go into a fresh slice. No static
// type string describes an element, so every %T resolves from the element's runtime type.
//
// Takes site (*program.CallSite) which is the call site.
// Takes arguments ([]reflect.Value) which are the coerced call arguments, edited in
// place.
// Takes formatIndex (int) which is the format string's argument index.
// Takes sliceIndex (int) which is the spread slice's argument index.
// Takes format (string) which is the format string.
func rewriteFmtTypeVerbsSpread(site *program.CallSite, arguments []reflect.Value, formatIndex, sliceIndex int, format string) {
	if sliceIndex >= len(arguments) {
		return
	}
	slice := arguments[sliceIndex]
	if !slice.IsValid() || slice.Kind() != reflect.Slice {
		return
	}
	varArgs := make([]any, slice.Len())
	for i := range varArgs {
		varArgs[i] = interfaceOrNil(slice.Index(i))
	}
	rewrittenFormat, rewrittenArgs, intercepted := interceptFmtFormat(site, len(site.Arguments), format, varArgs)
	if !intercepted {
		return
	}
	arguments[formatIndex] = reflect.ValueOf(rewrittenFormat)
	fresh := reflect.MakeSlice(slice.Type(), len(rewrittenArgs), len(rewrittenArgs))
	for i, rewritten := range rewrittenArgs {
		if rewritten == nil {
			continue
		}
		fresh.Index(i).Set(reflect.ValueOf(rewritten))
	}
	arguments[sliceIndex] = fresh
}

// interfaceOrNil returns the Go value held by argument, or nil when the value cannot be
// read as an interface (invalid, or an unexported field).
//
// Takes argument (reflect.Value) which is the value to read.
//
// Returns any which is the value or nil.
func interfaceOrNil(argument reflect.Value) any {
	if !argument.IsValid() || !argument.CanInterface() {
		return nil
	}
	return argument.Interface()
}

// wrapFmtArgument routes a coerced `...any` argument through wrapPipitSynthesisedFmtArg
// so fmt sees adapters for synthesised types. Nil interfaces are left untouched.
//
// Takes vm (*VM) which provides the method registry for adapters.
// Takes argument (reflect.Value) which is the coerced argument.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns the argument, wrapped when its dynamic type is pipit-synthesised.
func wrapFmtArgument(vm *VM, argument reflect.Value, staticTypeName string) reflect.Value {
	return wrapFmtArgumentForVerb(vm, argument, 'v', staticTypeName)
}

// wrapFmtArgumentForVerb is wrapFmtArgument for an operand whose verb is known (see
// wrapPipitSynthesisedFmtArgForVerb).
//
// Takes vm (*VM) which resolves script method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes verb (byte) which is the operand's verb.
// Takes staticTypeName (string) which is the call site's recorded source-level type name.
//
// Returns reflect.Value which is the operand to hand to fmt.
func wrapFmtArgumentForVerb(vm *VM, argument reflect.Value, verb byte, staticTypeName string) reflect.Value {
	if !argument.IsValid() || !argument.CanInterface() || isNilInterfaceValue(argument) {
		return argument
	}
	wrapped := wrapPipitSynthesisedFmtArgForVerb(vm, argument.Interface(), verb, staticTypeName)
	if wrapped == nil {
		return argument
	}
	return reflect.ValueOf(wrapped)
}

// fmtVerbsForCall scans the format operand of a Printf-style native call (the parameter
// before the variadic one, when it is a string) and returns the verb of each variadic
// operand; nil when the call has no format string (Print, Println, Sprint).
//
// Takes registers (*Registers) which hold the operands.
// Takes site (*program.CallSite) which describes the call.
// Takes parameterTypes ([]reflect.Type) which are the callee's parameter types.
//
// Returns []byte which is nil or one verb per variadic operand.
func fmtVerbsForCall(registers *Registers, site *program.CallSite, parameterTypes []reflect.Type) []byte {
	formatIndex := len(parameterTypes) - 2
	if formatIndex < 0 || parameterTypes[formatIndex].Kind() != reflect.String || formatIndex >= len(site.Arguments) {
		return nil
	}
	formatLocation := site.Arguments[formatIndex]
	if formatLocation.Kind != isa.RegisterString {
		return nil
	}
	variadicCount := len(site.Arguments) - (formatIndex + 1)
	if variadicCount <= 0 {
		return nil
	}
	return fmtArgumentVerbs(registers.Strings[formatLocation.Register], variadicCount)
}

// fmtVerbAt returns the verb recorded for variadic operand index, or 'v' when the call
// has no format string or the index is out of range.
//
// Takes verbs ([]byte) which may be nil.
// Takes index (int) which is the variadic operand position.
//
// Returns byte which is the verb.
func fmtVerbAt(verbs []byte, index int) byte {
	if index < 0 || index >= len(verbs) {
		return 'v'
	}
	return verbs[index]
}

// isNilInterfaceValue reports whether value is an interface-kind Value holding nothing.
//
// Takes value (reflect.Value) which must be valid.
//
// Returns bool which is true for a nil interface value.
func isNilInterfaceValue(value reflect.Value) bool {
	return value.Kind() == reflect.Interface && value.IsNil()
}

// argumentTypeContextFromSite extracts the static-type context for one argument.
//
// When the call site has no static-type info (e.g. compiled-function targets), an empty
// context is returned.
//
// Takes site (*CallSite) which carries the per-argument static-type slices.
// Takes i (int) which is the zero-based argument position.
//
// Returns the argumentTypeContext populated from the site's recorded data for index i.
func argumentTypeContextFromSite(site *program.CallSite, i int) argumentTypeContext {
	var ctx argumentTypeContext
	if i < len(site.ArgumentStaticTypeNames) {
		ctx.staticTypeName = site.ArgumentStaticTypeNames[i]
	}
	if i < len(site.ArgumentStaticTypeStrings) {
		ctx.staticTypeString = site.ArgumentStaticTypeStrings[i]
	}
	return ctx
}

// variadicElementTypeForSite returns the element type of the trailing variadic parameter
// when the call site targets a variadic function called WITHOUT the ellipsis spread (so
// each trailing argument is a single variadic element rather than a pre-built slice).
//
// Takes site (*CallSite) which describes the parameter types and the ellipsis-spread
// flag.
//
// Returns the variadic element reflect.Type, or nil for non-variadic sites and
// ellipsis-spread calls.
func variadicElementTypeForSite(site *program.CallSite) reflect.Type {
	if site.IsEllipsisSpread {
		return nil
	}
	cache := site.NativeParamCacheLoad()
	if cache != nil && !cache.IsVariadic {
		return nil
	}
	var parameterTypes []reflect.Type
	if cache != nil {
		parameterTypes = cache.Types
	}
	parameterCount := len(parameterTypes)
	if parameterCount == 0 {
		return nil
	}
	last := parameterTypes[parameterCount-1]
	if last.Kind() != reflect.Slice {
		return nil
	}
	if len(site.Arguments) < parameterCount {
		return nil
	}
	return last.Elem()
}

// coerceVariadicSpreadSlice rebuilds a slice argument so its concrete type matches the
// variadic parameter's slice type.
//
// Without this, a source slice typed []interface{} cannot be CallSlice'd into a parameter
// typed []error (or any other concrete slice) - reflect rejects the type mismatch even
// when each element is convertible.
//
// Takes vm (*VM) which provides context for nested element coercion.
// Takes source (reflect.Value) which is the spread slice argument.
// Takes expectedSliceType (reflect.Type) which is the declared variadic slice type.
//
// Returns the original slice unchanged when types already match; a freshly allocated
// slice with coerced elements otherwise.
func coerceVariadicSpreadSlice(vm *VM, source reflect.Value, expectedSliceType reflect.Type) reflect.Value {
	if !source.IsValid() {
		return reflect.Zero(expectedSliceType)
	}
	if source.Type() == expectedSliceType {
		return source
	}
	if source.Kind() != reflect.Slice {
		return source
	}
	elementType := expectedSliceType.Elem()
	out := reflect.MakeSlice(expectedSliceType, source.Len(), source.Len())
	for j := range source.Len() {
		element := source.Index(j)
		if element.Kind() == reflect.Interface {
			if element.IsNil() {
				continue
			}
			element = element.Elem()
		}
		target := out.Index(j)
		switch {
		case element.Type().AssignableTo(elementType):
			target.Set(element)
		case element.Type().ConvertibleTo(elementType):
			target.Set(element.Convert(elementType))
		case elementType.Kind() == reflect.Interface:
			coerced := coerceReflectArgument(vm, element, elementType, argumentTypeContext{staticTypeName: "", staticTypeString: "", skipInterfaceAdapter: false, keepClosures: false})
			if coerced.IsValid() && coerced.Type().AssignableTo(elementType) {
				target.Set(coerced)
			}
		}
	}
	return out
}

// coerceReflectArgument adjusts a single argument value to match the expected parameter
// type. Handles closure-to-func wrapping, bool/int conversion, and general
// reflect.Convert coercion.
//
// Takes vm (*VM) which provides context for closure coercion.
// Takes argument (reflect.Value) which is the value to coerce.
// Takes expectedType (reflect.Type) which is the target parameter type.
// Takes typeCtx (argumentTypeContext) which carries the per-argument compile-time
// static-type metadata used by interface-adapter selection. Pass argumentTypeContext{}
// when no static info is available (e.g. native-callback paths from host code).
//
// Returns reflect.Value coerced to expectedType, or the original if none applies.
func coerceReflectArgument(vm *VM, argument reflect.Value, expectedType reflect.Type, typeCtx argumentTypeContext) reflect.Value {
	if widened, ok := conformSliceWidth(argument, expectedType); ok {
		return widened
	}
	if !argument.IsValid() {
		if expectedType != nil {
			return reflect.Zero(expectedType)
		}
		return argument
	}
	if argument.Type() == expectedType {
		return argument
	}
	if _, isClosure := reflect.TypeAssert[*RuntimeClosure](argument); isClosure {
		if typeCtx.keepClosures && expectedType.Kind() == reflect.Interface {
			return argument
		}
		return coerceClosureArgument(vm, argument, expectedType)
	}
	if adapted, ok := coerceForInterfaceSlot(vm, argument, expectedType, typeCtx); ok {
		return adapted
	}
	return coerceConcreteArgument(vm, argument, expectedType)
}

// coerceForInterfaceSlot adapts a script value bound for an interface parameter: an
// adapter carrying the value's compiled methods, or the named scalar the static type
// names. Skipped when the call site asked for raw values.
//
// Takes vm (*VM) which resolves script method tables.
// Takes argument (reflect.Value) which is the operand.
// Takes expectedType (reflect.Type) which is the parameter type.
// Takes typeCtx (argumentTypeContext) which carries the static type and the skip flag.
//
// Returns the adapted value and true, or false when nothing applies.
func coerceForInterfaceSlot(vm *VM, argument reflect.Value, expectedType reflect.Type, typeCtx argumentTypeContext) (reflect.Value, bool) {
	if expectedType.Kind() != reflect.Interface || typeCtx.skipInterfaceAdapter {
		return reflect.Value{}, false
	}
	if adapter := tryBuildInterfaceAdapter(vm, argument, expectedType, typeCtx); adapter.IsValid() {
		return adapter, true
	}
	return restoreNamedScalarForInterface(vm, argument, typeCtx)
}

// coerceConcreteArgument converts a scalar or pointer operand to the parameter type: an
// int flag to bool, a convertible value through reflect, or a reinterpreted pointer.
//
// Takes vm (*VM) which validates pointer reinterpretation.
// Takes argument (reflect.Value) which is the operand.
// Takes expectedType (reflect.Type) which is the parameter type.
//
// Returns reflect.Value which is the converted operand, or the operand itself.
func coerceConcreteArgument(vm *VM, argument reflect.Value, expectedType reflect.Type) reflect.Value {
	if expectedType.Kind() == reflect.Bool && argument.Kind() == reflect.Int64 {
		return reflect.ValueOf(argument.Int() != 0)
	}
	if argument.Type().ConvertibleTo(expectedType) {
		return argument.Convert(expectedType)
	}
	if coerced, ok := reinterpretPointerArgument(vm, argument, expectedType); ok {
		return coerced
	}
	return argument
}

// restoreNamedScalarForInterface re-clothes a scalar argument with its source-level named
// type when the parameter is an interface and the value would otherwise box as its bare
// underlying primitive.
//
// Takes vm (*VM) which provides the symbol registry.
// Takes argument (reflect.Value) which is the boxed scalar from a register.
// Takes typeCtx (argumentTypeContext) which carries the recorded static type string.
//
// Returns the restored named-type value and true on success, or a zero value and false.
func restoreNamedScalarForInterface(vm *VM, argument reflect.Value, typeCtx argumentTypeContext) (reflect.Value, bool) {
	if vm == nil || vm.symbols == nil || typeCtx.staticTypeString == "" {
		return reflect.Value{}, false
	}
	dotIndex := indexByteString(typeCtx.staticTypeString, '.')
	if dotIndex <= 0 || dotIndex >= len(typeCtx.staticTypeString)-1 {
		return reflect.Value{}, false
	}
	pkgQualifier := typeCtx.staticTypeString[:dotIndex]
	typeName := typeCtx.staticTypeString[dotIndex+1:]
	if strings.ContainsAny(pkgQualifier, "[]*") || strings.ContainsAny(typeName, "[]*") {
		return reflect.Value{}, false
	}
	namedType, ok := resolveRegisteredNamedType(vm.symbols, pkgQualifier, typeName)
	if !ok {
		return reflect.Value{}, false
	}
	if argument.Type() == namedType || !argument.Type().ConvertibleTo(namedType) {
		return reflect.Value{}, false
	}
	return argument.Convert(namedType), true
}

// reinterpretPointerArgument bridges native-backed generic erasure by reinterpreting a
// pointer via reflect.NewAt when the expected pointee is a registered erasure type.
// Mismatches outside the erasure boundary are left unchanged.
//
// Takes vm (*VM) which provides the symbol registry whose NativeBackedGenericType
// sentinels delimit the erasure boundary.
// Takes argument (reflect.Value) which is the supplied pointer value.
// Takes expectedType (reflect.Type) which is the declared parameter type.
//
// Returns the reinterpreted pointer and true, or a zero value and false when no
// reinterpretation applies.
func reinterpretPointerArgument(vm *VM, argument reflect.Value, expectedType reflect.Type) (reflect.Value, bool) {
	if expectedType == nil || expectedType.Kind() != reflect.Pointer || argument.Kind() != reflect.Pointer {
		return reflect.Value{}, false
	}
	if argument.Type() == expectedType || vm == nil {
		return reflect.Value{}, false
	}
	if vm.nativeBackedErasurePointees == nil {
		vm.nativeBackedErasurePointees = vm.buildNativeBackedErasurePointees()
	}
	if _, ok := vm.nativeBackedErasurePointees[expectedType.Elem()]; !ok {
		return reflect.Value{}, false
	}
	if argument.IsNil() {
		return reflect.Zero(expectedType), true
	}
	return reflect.NewAt(expectedType.Elem(), argument.UnsafePointer()), true
}

// collectErasurePointees adds the erased type and every erasure argument of a single
// symbol to pointees when that symbol is a NativeBackedGenericType sentinel; non-sentinel
// symbols are ignored.
//
// Takes value (reflect.Value) which is a registered symbol.
// Takes pointees (map[reflect.Type]struct{}) which accumulates the erasure-boundary
// pointee types.
func collectErasurePointees(value reflect.Value, pointees map[reflect.Type]struct{}) {
	if !value.IsValid() {
		return
	}
	native, ok := reflect.TypeAssert[link.NativeBackedGenericType](value)
	if !ok {
		return
	}
	if native.ErasedType != nil {
		pointees[native.ErasedType] = struct{}{}
	}
	for _, erasureArg := range native.ErasureArgs {
		if erasureArg != nil {
			pointees[erasureArg] = struct{}{}
		}
	}
}

// coerceClosureArgument wraps a runtime closure into a reflect.Func or callable interface
// value matching the expected parameter type.
//
// Takes vm (*VM) which provides context for closure wrapping.
// Takes argument (reflect.Value) which holds the runtime closure.
// Takes expectedType (reflect.Type) which is the target parameter type.
//
// Returns reflect.Value wrapping the closure as a func or interface.
func coerceClosureArgument(vm *VM, argument reflect.Value, expectedType reflect.Type) reflect.Value {
	switch expectedType.Kind() {
	case reflect.Func:
		return coerceClosureToFunction(vm, argument, expectedType)
	case reflect.Interface:
		return closureCallableValue(vm, argument)
	default:
		return argument
	}
}

// storeReflectResults unpacks reflect.Call results into the caller's register banks
// according to the return location descriptors.
//
// Takes registers (*Registers) which is the destination register set.
// Takes returns ([]VarLocation) which describes where to store each result.
// Takes results ([]reflect.Value) which holds the values from the call.
func storeReflectResults(registers *Registers, returns []program.VarLocation, results []reflect.Value) {
	limit := min(len(returns), len(results))
	for i, reflectValue := range results[:limit] {
		if reflectValue.Kind() == reflect.Interface && !reflectValue.IsNil() {
			reflectValue = reflectValue.Elem()
		}
		storeOneReflectResult(registers, returns[i], reflectValue)
	}
}

// storeOneReflectResult writes a single reflect.Value into the appropriate register bank.
// Special-cases bool-to-int64 for the int register bank.
//
// Named banks receive the reflect result directly; the default stores it into the general
// bank, which is the boxed form any other kind uses.
//
// Takes registers (*Registers) which is the destination register set.
// Takes retLocation (VarLocation) which describes the target bank and index.
// Takes value (reflect.Value) which is the value to store.
func storeOneReflectResult(registers *Registers, retLocation program.VarLocation, value reflect.Value) {
	switch retLocation.Kind {
	case isa.RegisterInt:
		if value.Kind() == reflect.Bool {
			registers.Ints[retLocation.Register] = boolToInt64(value.Bool())
		} else {
			registers.Ints[retLocation.Register] = value.Int()
		}
	case isa.RegisterFloat:
		registers.Floats[retLocation.Register] = value.Float()
	case isa.RegisterString:
		registers.Strings[retLocation.Register] = value.String()
	case isa.RegisterGeneral:
		registers.General[retLocation.Register] = value
	case isa.RegisterBool:
		registers.Bools[retLocation.Register] = value.Bool()
	case isa.RegisterUint:
		registers.Uints[retLocation.Register] = value.Uint()
	case isa.RegisterComplex:
		registers.Complex[retLocation.Register] = value.Complex()
	default:
	}
}

// tryNativeFastPathDispatch attempts the cached scalar fast path for a native call.
//
// Skipped when a capability hook is installed, because the fast path bypasses the
// marshalling the hook inspects, and when the site has already been classified as having
// no fast path.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes registers (*Registers) which holds the current register banks.
// Takes site (*CallSite) which carries the fast-path classification.
// Takes reflectedFunction (reflect.Value) which is the native callee.
//
// Returns the OpResult and true when the fast path handled the call, or false when the
// caller must fall through to the reflect path.
func tryNativeFastPathDispatch(vm *VM, registers *Registers, site *program.CallSite, reflectedFunction reflect.Value) (OpResult, bool) {
	entry := loadNativeFastPath(site)
	if vm.Limits.CapabilityHook != nil || (entry != nil && entry.fn == nativeFastPathNone) {
		return opContinue, false
	}
	vm.Globals.dispatchDepth.Add(1)
	ok, _, panicValue := tryNativeFastPath(vm, site, reflectedFunction.Interface(), registers)
	vm.Globals.dispatchDepth.Add(-1)
	if !ok {
		return opContinue, false
	}
	if panicValue != nil {
		return raiseNativePanicAsInterpreted(vm, panicValue), true
	}
	return opContinue, true
}

// invokeNativeUnderHandoff performs the reflect call, releasing the interpreter lock
// around it when the native captured an interpreted closure.
//
// Release-then-arm ordering guarantees a synchronous same-goroutine callback never Locks
// a mutex the goroutine still holds, and the callback body re-acquires the lock (via
// acquireForCallback) so it stays serialised; only the callback's own blocking waits run
// unlocked.
//
// Takes vm (*VM) which is the virtual machine executing the call.
// Takes site (*CallSite) which says whether the final argument is an ellipsis spread.
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the marshalled arguments.
// Takes handoff (*gilHandoff) which is armed only in safe mode, and nil otherwise.
//
// Returns the call results, any recovered panic value, and any call error.
func invokeNativeUnderHandoff(
	vm *VM,
	site *program.CallSite,
	reflectedFunction reflect.Value,
	arguments []reflect.Value,
	handoff *gilHandoff,
) ([]reflect.Value, any, error) {
	vm.Globals.dispatchDepth.Add(1)
	gilReleased := false
	if handoff != nil && handoff.hasClosure {
		gilReleased = vm.releaseAroundBlock()
		if gilReleased {
			handoff.active.Store(true)
		}
	}
	results, panicValue, err := safeReflectCallOrCallSlice(reflectedFunction, arguments, site.IsEllipsisSpread)
	if gilReleased {
		handoff.active.Store(false)
		vm.reacquireAfterBlock(gilReleased)
	}
	vm.Globals.dispatchDepth.Add(-1)
	return results, panicValue, err
}

// dispatchSetFinalizerIntrinsic guards runtime.SetFinalizer calls.
//
// The real function requires a pointer to the start of a Go allocation and terminates the
// process otherwise, so a registration on interpreter-owned storage is dropped. A pointer
// to Go heap memory goes to the real function.
//
// Takes vm (*VM) which owns the arena and snapshot pools.
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which are the coerced operands.
//
// Returns opContinue and true when the call was consumed.
func dispatchSetFinalizerIntrinsic(vm *VM, reflectedFunction reflect.Value, arguments []reflect.Value) (OpResult, bool) {
	if reflectedFunction.Pointer() != setFinalizerPointer || len(arguments) != 2 {
		return opContinue, false
	}
	object := arguments[0]
	if object.IsValid() && object.Kind() == reflect.Interface {
		object = object.Elem()
	}
	if !object.IsValid() || object.Kind() != reflect.Pointer || object.IsNil() {
		return opContinue, false
	}
	if vm.ownsInterpreterStorage(object.UnsafePointer()) {
		return opContinue, true
	}
	return opContinue, false
}
