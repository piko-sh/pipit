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
	"sync"
	"sync/atomic"
	"unsafe"
	"weak"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

const (
	// DerefSnapshot is the isa.OpDereference operand-c sentinel requesting a freestanding
	// value copy whose backing storage is a fresh allocation, so mutations to the source
	// cannot leak into the copy. The default value 0 preserves live-view semantics.
	DerefSnapshot uint8 = 1

	// maxTypedHandleCacheEntries caps the per-VM typed channel handle cache. Beyond the cap,
	// dispatch is still correct but the typed handle is not memoised.
	maxTypedHandleCacheEntries = 1024

	// maxMethodCacheEntries caps the per-VM method-index cache. Beyond the cap, dispatch
	// repeats reflect.Type.MethodByName each call.
	maxMethodCacheEntries = 4096

	// initialTypedHandleCacheCapacity sizes the typedHandleCache map on first use. Eight
	// matches the typical channel/map count in early program startup and avoids two or three
	// growth rehashes for realistic workloads while keeping the cold-start memory cost low.
	initialTypedHandleCacheCapacity = 8

	// initialMapKeyScratchCapacity sizes the mapKeyScratch reuse cache on first insert. Most
	// programs use a handful of map key types (often just int and string) so a small initial
	// map fits the working set.
	initialMapKeyScratchCapacity = 4

	// typeAssertModePanic is the `x.(T)` mode where non-match panics with an interpreted
	// "interface conversion" message.
	typeAssertModePanic uint8 = 1

	// TypeAssertModeTypeSwitch is a type-switch case probe. On non-match the destination
	// register is left untouched so multi-type cases do not clobber a prior match.
	TypeAssertModeTypeSwitch uint8 = 2

	// AddrSourceStable is the operand-C marker for isa.OpAddress on heap-stable sources
	// where the escape-promote copy must be skipped because a copy would break aliasing with
	// the container element, while the default value 0 preserves escape-promote behaviour
	// for locals.
	AddrSourceStable uint8 = 1

	// typeIsPointerFreeCacheInitialHint is the starting capacity for the typeIsPointerFree
	// cache map. Sized for the typical interpreter session's distinct-type count; the map
	// grows automatically.
	typeIsPointerFreeCacheInitialHint = 64

	// typeIsPointerFreeFrontSlots is the size of typeIsPointerFree's per-type front, a power
	// of two.
	typeIsPointerFreeFrontSlots = 512
)

var (
	// unsafePointerType holds the reflect.Type for unsafe.Pointer, used to detect pointer
	// conversions.
	unsafePointerType = reflect.TypeFor[unsafe.Pointer]()

	// typeIsPointerFreeCache memoises typeIsPointerFree decisions per type. The map is
	// copy-on-write behind an atomic.Pointer so the read path is lock-free.
	typeIsPointerFreeCache atomic.Pointer[map[unsafe.Pointer]bool]

	// typeIsPointerFreeMu serialises copy-on-write updates to typeIsPointerFreeCache; the
	// read path is lock-free.
	typeIsPointerFreeMu sync.Mutex

	// typeIsPointerFreeFront is a direct-mapped, lock-free front for typeIsPointerFreeCache.
	//
	// Each slot holds a type's *abi.Type address, with bit 0 set when the type is pointer
	// free (types are word-aligned), or zero.
	//
	// A hit costs one atomic load and no map hash; a miss, or a slot another type took,
	// falls back to the map. A slot is written only after the map holds its type, which
	// keeps the address from naming another type.
	typeIsPointerFreeFront [typeIsPointerFreeFrontSlots]atomic.Uintptr
)

// selectCaseInfo tracks the destination register for a select receiver case.
type selectCaseInfo struct {
	// destinationRegister is the register index where the received value is stored.
	destinationRegister uint8

	// destinationKind identifies which typed register bank receives the value.
	destinationKind isa.RegisterKind

	// hasOk reports whether the recv case captures the comma-ok boolean.
	hasOk bool

	// okRegister is the int register that receives the comma-ok boolean (1 if value was
	// received from a channel, 0 if the channel was closed).
	okRegister uint8
}

// methodCacheKey identifies a (type, method name) pair for the per-VM method index cache
// used by handleBindMethodByNameBody.
type methodCacheKey struct {
	// typ is the reflect type of the receiver.
	typ reflect.Type

	// Name is the method name being looked up.
	Name string
}

// boundMethodVM captures state for running a bound method in a child VM. Capture happens
// on the owning goroutine because native callers may invoke the method concurrently.
type boundMethodVM struct {
	// vm is the VM the method was bound on; a failed call records its error there.
	vm *VM

	// link is the execution the method was bound in.
	link *executionLink

	// globals is the bound VM's shared global store.
	globals *GlobalStore

	// symbols is the bound VM's symbol registry.
	symbols *symtab.SymbolRegistry

	// rootFunction is the root the child VM executes under.
	rootFunction *program.CompiledFunction

	// callee is the compiled function to invoke as the method body.
	callee *program.CompiledFunction

	// rootFunctionOverride pins the rootFunction the child VM will execute under, overriding
	// the parent VM's. Set for the cross-package adapter path; nil for the in-package case.
	rootFunctionOverride *program.CompiledFunction

	// makerArena names the arena of the VM the method was bound on, for its count of active
	// callbacks (parked callers); a nil pointer in an arena without a licence.
	makerArena weak.Pointer[RegisterArena]

	// gates are the export gates each child VM's run holds, which are those the VM the
	// method was bound on had published when the method was bound (VM.exportedGates).
	gates exportGateSet

	// functions is the function table the child VM dispatches against.
	functions []*program.CompiledFunction

	// limits carries the resource limits inherited from the parent VM.
	limits VMLimits

	// goroutineID is the bound VM's goroutine identity, inherited by each child VM.
	goroutineID uint64
}

// rangeNextContext bundles the decoded extension-word parameters needed by the
// per-collection-type range-next helpers.
type rangeNextContext struct {
	// DoneDestination is the int register index that receives 1 when iterating or 0 when
	// exhausted.
	DoneDestination uint8

	// HasKey indicates whether the range loop binds a key variable.
	HasKey bool

	// HasValue indicates whether the range loop binds a value variable.
	HasValue bool

	// KeyInstruction encodes the destination register and kind for the key.
	KeyInstruction isa.Instruction

	// ValInstruction encodes the destination register and kind for the value.
	ValInstruction isa.Instruction
}

// newBoundMethodVM captures what invoking callee as a method of a value bound on vm
// needs. It must run on the goroutine that owns vm.
//
// Takes vm (*VM) which is the VM the method is bound on.
// Takes callee (*program.CompiledFunction) which is the method body.
// Takes rootFunctionOverride (*program.CompiledFunction) which pins the child VM's root
// for a method compiled in another package; nil for vm's own root.
//
// Returns the bound method.
func newBoundMethodVM(vm *VM, callee *program.CompiledFunction, rootFunctionOverride *program.CompiledFunction) *boundMethodVM {
	return new(makeBoundMethodVM(vm, callee, rootFunctionOverride))
}

// invoke runs a bound method in a child VM using bindCallbackRun's cancellation rules.
// Cancellation or timeout fails the live execution, while other failures are recorded on
// the bound VM.
//
// Takes receiver (reflect.Value) which is the method receiver value.
// Takes arguments ([]reflect.Value) which holds the method arguments.
// Takes extract (func(reflect.Value) reflect.Value) which converts each argument.
//
// Returns []reflect.Value which represents the method's return values, or zero-valued
// result slots when the child VM errors.
func (b *boundMethodVM) invoke(receiver reflect.Value, arguments []reflect.Value, extract func(reflect.Value) reflect.Value) []reflect.Value {
	results, err := b.invokeResult(receiver, arguments, extract)
	if err == nil {
		return results
	}
	if !failOnStop(b.link, err) && b.vm.evalError == nil {
		b.vm.evalError = fmt.Errorf("bound method invocation failed: %w", err)
	}
	return reflectResults(nil, b.callee.ResultKinds)
}

// invokeResult is invoke returning the child VM's failure to the caller instead of
// recording it on the parent, for adapters whose interface method can return an error.
//
// An interpreted panic from the child VM propagates through any active upstream native
// dispatch frame to reach an interpreted defer/recover.
//
// Takes receiver (reflect.Value) which is the method receiver value.
// Takes arguments ([]reflect.Value) which holds the method arguments.
// Takes extract (func(reflect.Value) reflect.Value) which converts each argument.
//
// Returns []reflect.Value which represents the method's return values.
// Returns error when the child VM failed.
func (b *boundMethodVM) invokeResult(receiver reflect.Value, arguments []reflect.Value, extract func(reflect.Value) reflect.Value) ([]reflect.Value, error) {
	callee, err := receiverMethodBody(b.rootFunction, receiver, b.callee)
	if err != nil {
		return nil, err
	}
	childVM := NewVM(b.link.detached, b.globals, b.symbols)
	recycleChild := false
	defer func() {
		if recycleChild {
			childVM.recycle()
		}
	}()
	childVM.bindCallbackRun(b.link, b.limits.CallbackTimeout)
	childVM.goroutineID = b.goroutineID
	childVM.reentrantInterpreterVM = true
	childVM.debugKind = DebugThreadBoundMethod
	childVM.debugParkDone = b.link.ctx.Done()

	childVM.hasGoroutines = true
	childVM.Limits = b.limits
	childVM.runGates = b.gates
	childVM.functions = b.functions
	childVM.rootFunction = b.rootFunction
	arena := b.acquireArena(childVM)
	childVM.Arena = arena
	if arena.licensed {
		arena.setLicenceVM(childVM)
	}
	childVM.CallStack = arena.frameStack()
	defer childVM.FinishWatcher()
	defer childVM.ReturnUnspentCostChunk()
	defer childVM.finishRun()
	completed := false
	defer func() {
		childVM.CallStack = nil
		childVM.Arena = nil
		if completed && b.keepsBoundArena(childVM, arena) && b.vm.parkBoundArena(arena) {
			return
		}
		releaseRegisterArena(b.limits.abandonsArenas(), arena, true)
	}()
	restoreUnwindSlice := childVM.adoptAbortUnwindSliceIfNested()
	defer restoreUnwindSlice()
	childVM.parkedMaker = b.makerArena
	if childVM.enterServedChain() {
		defer childVM.leaveServedChain()
	}
	childVM.pushBoundMethodFrame(callee, receiver, arguments, extract)
	result, err := childVM.runBoundMethodUnwinding()
	allResults := childVM.EvalAllResults
	childVM.EvalAllResults = nil
	childVM.evalSingle = false
	if err != nil {
		b.raiseStopThroughNative(childVM, err)
		return nil, err
	}
	result = b.handOffResults(childVM, result, allResults, receiver, arguments)
	results := reflectResultsMulti(result, allResults, callee.ResultKinds)
	recycleChild = true
	completed = true
	return results, nil
}

// pushBoundMethodFrame pushes the frame a bound method runs in, with its receiver and
// arguments in place.
//
// Takes callee (*program.CompiledFunction) which is the method body.
// Takes receiver (reflect.Value) which is the method receiver value.
// Takes arguments ([]reflect.Value) which holds the method arguments.
// Takes extract (func(reflect.Value) reflect.Value) which converts each argument.
func (vm *VM) pushBoundMethodFrame(callee *program.CompiledFunction, receiver reflect.Value, arguments []reflect.Value, extract func(reflect.Value) reflect.Value) {
	vm.PushFrame(callee)
	frame := vm.currentFrame()
	placeBoundMethodReceiver(&frame.Registers, callee, receiver)
	setMethodArgs(&frame.Registers, callee, arguments, extract)
}

// acquireArena returns the arena the bound method's VM runs on: the one the previous call
// kept, or a fresh one under the bound method's limits.
//
// Takes childVM (*VM) which is the VM the arena is for.
//
// Returns *RegisterArena which is the arena.
func (b *boundMethodVM) acquireArena(childVM *VM) *RegisterArena {
	if arena, kept := b.vm.takeBoundArena(); kept {
		return arena
	}
	arena := acquireRegisterArena(b.limits.abandonsArenas())
	arena.setRunBudget(b.limits.MaxArenaBytes)
	arena.MaxAllocSize = b.limits.MaxAllocSize
	arena.cloneUnownedStrings = true
	arena.AttachBudgetTracker(b.limits.Tracker)
	arena.attachLicencePolicy(&b.limits)
	arena.licenceAlwaysShared = arena.licensed
	if arenaAuditEnabled {
		arena.auditAcquired(childVM.debugKind.String())
	}
	return arena
}

// raiseStopThroughNative raises a bound-method VM's stop through its native caller when
// it must cross the native frame: a guest panic inside an active dispatch, or a hard
// stop.
//
// Takes childVM (*VM) which ran the method.
// Takes err (error) which is why it stopped.
//
// Panics with the method's panic or stop when it must reach the native caller.
func (b *boundMethodVM) raiseStopThroughNative(childVM *VM, err error) {
	if childVM.panicValue != nil && b.globals != nil && b.globals.dispatchDepth.Load() > 0 {
		panic(wrapCallbackPanic(childVM))
	}
	if crossesNativeFrame(err, b.globals) {
		panic(err)
	}
}

// handOffResults hands off a finished bound-method run's results, receiver and arguments,
// counting a run that needed no epoch revocation.
//
// Takes childVM (*VM) which ran the method.
// Takes result (any) which is the single result, or the first of several.
// Takes allResults ([]any) which holds every result of a multi-result method.
// Takes receiver (reflect.Value) which is the method's receiver.
// Takes arguments ([]reflect.Value) which are its arguments.
//
// Returns any which is the single result after its hand-off.
func (b *boundMethodVM) handOffResults(childVM *VM, result any, allResults []any, receiver reflect.Value, arguments []reflect.Value) any {
	handedOff := false
	if len(b.callee.ResultKinds) <= 1 {
		single := [1]any{result}
		handedOff = childVM.handOffReentrantRun(single[:], receiver, arguments)
		result = single[0]
	} else {
		handedOff = childVM.handOffReentrantRun(allResults, receiver, arguments)
	}
	if handedOff && licenceCounting {
		callbackReleaseCounters.boundRuns.Add(1)
	}
	return result
}

// runBoundMethodUnwinding runs a bound-method VM and its pending defers after a hard
// abort, cost stop or cancellation.
//
// A hard abort then goes on up as the same Go panic, joined with the unwind report when a
// defer was left blocked; a cost stop or a cancellation is returned. Once a stopped
// execution has abandoned a lock, it returns the fail-closed report instead of running
// (failedClosed).
//
// Returns any which is the run's result.
// Returns error which is the run's error.
//
// Panics again for a hard abort, joined with any unwind report, and for recovered values
// that are not handled execution stops.
func (vm *VM) runBoundMethodUnwinding() (result any, err error) {
	if refused := vm.failedClosed(); refused != nil {
		return nil, refused
	}
	defer vm.leaveCallbackRun(vm.enterCallbackRun())
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if abort, ok := recovered.(error); ok && unwindsAfter(abort) && !vm.abortUnwinding {
			if report := vm.unwindAfterAbort(); report != nil {
				panic(joinUnwindReport(abort, report))
			}
		}
		panic(recovered)
	}()
	vm.bindFinaliserPlacement()
	if vm.enterServedChain() {
		defer vm.leaveServedChain()
	}
	if vm.mayRunGated() && vm.beginGatedRun() {
		defer vm.endGatedRun()
	}
	result, err = vm.run(0)
	return result, vm.unwindAfterStoppedRun(0, err)
}

// cachedAddr returns v.Addr() using the VM's pointer-type cache.
//
// Takes v (reflect.Value) which the caller has already proved CanAddr.
//
// Returns a Pointer-kind Value equivalent to v.Addr().
func (vm *VM) cachedAddr(v reflect.Value) reflect.Value {
	return unsafePointerKindValue(reflectValueABIType(vm.pointerTypeOf(v.Type())), ReflectValuePtr(v))
}

// pointerTypeOf returns the pointer type to elementType from a small per-VM table,
// falling back to reflect.PointerTo on a miss and replacing the table's oldest entry.
//
// Takes elementType (reflect.Type) which is the element type.
//
// Returns reflect.Type which is the pointer type to elementType.
func (vm *VM) pointerTypeOf(elementType reflect.Type) reflect.Type {
	for index := range vm.pointerTypeCache {
		if vm.pointerTypeCache[index].element == elementType {
			return vm.pointerTypeCache[index].pointer
		}
	}
	slot := &vm.pointerTypeCache[vm.pointerTypeCacheNext]
	vm.pointerTypeCacheNext = (vm.pointerTypeCacheNext + 1) % pointerTypeCacheSlots
	slot.element, slot.pointer = elementType, reflect.PointerTo(elementType)
	return slot.pointer
}

// makeBoundMethodVM is newBoundMethodVM returning the bound method by value, for a call
// site that invokes it at once and keeps nothing, so it can live on the caller's stack.
// It pins vm all the same, as every holder of a bound method does.
//
// Takes vm (*VM) which is the VM the method is bound on.
// Takes callee (*program.CompiledFunction) which is the method body.
// Takes rootFunctionOverride (*program.CompiledFunction) which pins the child VM's root
// for a method compiled in another package, or is nil for vm's own root.
//
// Returns the bound method.
func makeBoundMethodVM(vm *VM, callee *program.CompiledFunction, rootFunctionOverride *program.CompiledFunction) boundMethodVM {
	vm.pinIdentity()
	bound := boundMethodVM{
		vm:                   vm,
		link:                 vm.currentLink(),
		globals:              vm.Globals,
		symbols:              vm.symbols,
		functions:            vm.functions,
		rootFunction:         vm.rootFunction,
		callee:               callee,
		rootFunctionOverride: rootFunctionOverride,
		limits:               vm.Limits,
		goroutineID:          vm.goroutineID,
		makerArena:           weak.Pointer[RegisterArena]{},
		gates:                vm.exportedGates(),
	}
	if rootFunctionOverride != nil {
		bound.functions = rootFunctionOverride.Functions
		bound.rootFunction = rootFunctionOverride
	}
	if vm.Arena != nil && vm.Arena.licensed {
		bound.makerArena = weak.Make(vm.Arena)
	}
	return bound
}

// reflectResultsMulti packages a bound method's return values.
//
// Prefers the multi-return slice (allResults) when the callee declared more than one
// result; falls back to the single result value otherwise. Without it, calls like a pipit
// Read body returning (int, error) only surface the int slot, so the adapter drops EOF
// and ErrCustom-style errors and io.ReadAll loops forever returning (0, nil).
//
// Takes result (any) which is the single return path's first value.
// Takes allResults ([]any) which holds every declared return value (nil when the callee
// declared zero or one).
// Takes resultKinds ([]isa.RegisterKind) which describes the slot kinds.
//
// Returns []reflect.Value matching resultKinds slot-by-slot.
func reflectResultsMulti(result any, allResults []any, resultKinds []isa.RegisterKind) []reflect.Value {
	if len(resultKinds) <= 1 {
		return reflectResults(result, resultKinds)
	}
	results := make([]reflect.Value, len(resultKinds))
	for i, kind := range resultKinds {
		var raw any
		if i < len(allResults) {
			raw = allResults[i]
		}
		if raw == nil {
			results[i] = symtab.ZeroValueForKind(kind)
			continue
		}
		results[i] = reflect.ValueOf(raw)
	}
	return results
}

// placeBoundMethodReceiver writes the receiver into general register 0. When callee is a
// value-receiver method but the incoming reflect.Value is a pointer, the pointer is
// dereferenced so the body sees the struct value rather than a zero.
//
// Takes registers (*Registers) which receives the receiver value.
// Takes callee (*CompiledFunction) which is the method body being prepared.
// Takes receiver (reflect.Value) which is the receiver value to install.
func placeBoundMethodReceiver(registers *Registers, callee *program.CompiledFunction, receiver reflect.Value) {
	if callee != nil && !callee.IsPointerReceiver &&
		receiver.IsValid() && receiver.Kind() == reflect.Pointer && !receiver.IsNil() &&
		receiver.Type() != runtimeClosureReflectType {
		receiver = receiver.Elem()
	}
	registers.General[0] = receiver
}

// reflectResults converts a VM execution result into a slice of reflect.Value matching
// the expected result kinds for return to native callers.
//
// Takes result (any) which is the raw VM execution result.
// Takes resultKinds ([]isa.RegisterKind) which describes the expected return types.
//
// Returns []reflect.Value matching the result kinds, or nil if none.
func reflectResults(result any, resultKinds []isa.RegisterKind) []reflect.Value {
	if len(resultKinds) == 0 {
		return nil
	}
	if result == nil {
		results := make([]reflect.Value, len(resultKinds))
		for i, k := range resultKinds {
			results[i] = symtab.ZeroValueForKind(k)
		}
		return results
	}
	return []reflect.Value{reflect.ValueOf(result)}
}

// handleAddress stores the address of the source value into the destination register.
// Arena-resident values are promoted to their own storage first so the resulting pointer
// cannot alias subsequent arena bytes, unless operand-C is AddrSourceStable.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source value and destination.
// Takes instruction (instruction) which encodes the source/destination register indices.
//
// Returns OpResult indicating the next execution step.
func handleAddress(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.B]
	if !v.IsValid() {
		vMPanicInvalidRegister("handleAddress", "source", instruction.B, instruction, frame, registers)
	}
	switch {
	case instruction.C == AddrSourceStable:
	case vm.Arena != nil && (v.Kind() == reflect.Struct || v.Kind() == reflect.Array) &&
		v.CanAddr() && vm.Arena.ownsBytePointer(ReflectValuePtr(v)):
		promoted := promoteAddressedValue(vm, frame, v)
		registers.General[instruction.B] = promoted
		v = promoted
	case v.CanAddr() && isScalarKind(v.Kind()):

		promoted := promoteAddressedValue(vm, frame, v)
		registers.General[instruction.B] = promoted
		v = promoted
	}
	if v.CanAddr() {
		registers.General[instruction.A] = vm.cachedAddr(v)
	} else {
		slot := vm.snapshotToBoundary(v)
		registers.General[instruction.A] = slot.Addr()
		registers.General[instruction.B] = slot
	}
	return opContinue
}

// handleDereference dereferences a pointer in the general register bank and stores the
// pointed-to value in the destination register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the pointer and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleDereference(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.B]
	if instruction.C == DerefSnapshot {
		if !v.IsValid() {
			registers.General[instruction.A] = v
			return opContinue
		}
		registers.General[instruction.A] = copyReflectValueArena(vm.Arena, v)
		return opContinue
	}
	if !v.IsValid() {
		return raiseNilDereference(vm)
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage))
		}
		element := v.Elem()
		if element.Kind() == reflect.Interface {
			element = element.Elem()
		}
		registers.General[instruction.A] = element
	case reflect.Interface:
		registers.General[instruction.A] = v.Elem()
	default:
		registers.General[instruction.A] = v
	}
	return opContinue
}

// isScalarKind reports whether kind is a scalar the typed banks box into arena slots:
// every integer width, both float widths, both complex widths, bool and string.
//
// Takes kind (reflect.Kind) which is the value's kind.
//
// Returns bool which is false for pointer-shaped and compound kinds.
func isScalarKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		return true
	default:
		return false
	}
}
