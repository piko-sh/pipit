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
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
)

const (
	// DerefSnapshot is the isa.OpDeref operand-c sentinel requesting a freestanding value
	// copy whose backing storage is a fresh allocation, so mutations to the source cannot
	// leak into the copy. The default value 0 preserves live-view semantics.
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

	// AddrSourceStable is the operand-C marker for isa.OpAddr on heap-stable sources where
	// the escape-promote copy must be skipped because a copy would break aliasing with the
	// container element, while the default value 0 preserves escape-promote behaviour for
	// locals.
	AddrSourceStable uint8 = 1

	// typeIsPointerFreeCacheInitialHint is the starting capacity for the typeIsPointerFree
	// cache map. Sized for the typical interpreter session's distinct-type count; the map
	// grows automatically.
	typeIsPointerFreeCacheInitialHint = 64
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
// used by handleGetMethod.
type methodCacheKey struct {
	// typ is the reflect type of the receiver.
	typ reflect.Type

	// Name is the method name being looked up.
	Name string
}

// boundMethodVM holds the captured state for invoking a bound method or method expression
// in a fresh child VM.
type boundMethodVM struct {
	// vm is the parent VM providing shared context and function table.
	vm *VM

	// callee is the compiled function to invoke as the method body.
	callee *program.CompiledFunction

	// rootFunctionOverride pins the rootFunction the child VM will execute under, overriding
	// the parent VM's. Set for the cross-package adapter path; nil for the in-package case.
	rootFunctionOverride *program.CompiledFunction

	// limits carries the resource limits inherited from the parent VM.
	limits VMLimits
}

// invoke sets up a child VM, copies the receiver and arguments into registers, runs the
// callee, and returns the reflect results. The child runs under context.WithoutCancel
// because a bound value can outlive the execution that created it.
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
	if b.vm.evalError == nil {
		b.vm.evalError = fmt.Errorf("bound method invocation failed: %w", err)
	}
	return reflectResults(nil, b.callee.ResultKinds)
}

// invokeResult is invoke returning the child VM's failure to the caller instead of
// recording it on the parent, for adapters whose interface method can return an error.
//
// Takes receiver (reflect.Value) which is the method receiver value.
// Takes arguments ([]reflect.Value) which holds the method arguments.
// Takes extract (func(reflect.Value) reflect.Value) which converts each argument.
//
// Returns []reflect.Value which represents the method's return values.
// Returns error when the child VM failed.
//
// Panics when the child VM raises an interpreted panic and an upstream native dispatch
// frame is still active, so the panic reaches an interpreted defer/recover.
func (b *boundMethodVM) invokeResult(receiver reflect.Value, arguments []reflect.Value, extract func(reflect.Value) reflect.Value) ([]reflect.Value, error) {
	childVM := NewVM(context.WithoutCancel(b.vm.ctx), b.vm.Globals, b.vm.symbols)
	childVM.goroutineID = b.vm.goroutineID
	childVM.reentrantInterpreterVM = true
	childVM.debugKind = DebugThreadBoundMethod
	childVM.debugParkDone = b.vm.ctx.Done()

	childVM.hasGoroutines = true
	childVM.Limits = b.limits
	if b.rootFunctionOverride != nil {
		childVM.functions = b.rootFunctionOverride.Functions
		childVM.rootFunction = b.rootFunctionOverride
	} else {
		childVM.functions = b.vm.functions
		childVM.rootFunction = b.vm.rootFunction
	}
	arena := GetRegisterArena()

	arena.MaxArenaBytes = b.limits.MaxArenaBytes
	arena.MaxAllocSize = b.limits.MaxAllocSize
	arena.disableMinorGC = b.limits.DisableMinorGC
	arena.AttachBudgetTracker(b.limits.Tracker)
	childVM.Arena = arena
	childVM.CallStack = arena.frameStack()
	defer childVM.FinishWatcher()
	defer childVM.ReturnUnspentCostChunk()
	defer func() {
		childVM.CallStack = nil
		putRegisterArenaAbandoningEscapes(arena)
	}()
	childVM.PushFrame(b.callee)
	f := childVM.currentFrame()
	placeBoundMethodReceiver(&f.Registers, b.callee, receiver)
	setMethodArgs(&f.Registers, b.callee, arguments, extract)
	result, err := childVM.run(0)
	allResults := childVM.EvalAllResults
	childVM.EvalAllResults = nil
	if err != nil {
		if childVM.panicValue != nil && b.vm.Globals != nil && b.vm.Globals.dispatchDepth.Load() > 0 {
			panic(wrapCallbackPanic(childVM))
		}
		return nil, err
	}
	return reflectResultsMulti(result, allResults, b.callee.ResultKinds), nil
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

// cachedAddr returns v.Addr() but bypasses reflect.PointerTo's sync.Map lookup via a
// one-slot per-VM cache. Hot for the `&Struct{...}` ADDR opcode chain in tight
// constructors.
//
// Takes v (reflect.Value) which the caller has already proved CanAddr.
//
// Returns a Pointer-kind Value equivalent to v.Addr().
func (vm *VM) cachedAddr(v reflect.Value) reflect.Value {
	elemType := v.Type()
	ptrType := vm.ptrTypeCacheValue
	if vm.ptrTypeCacheKey != elemType {
		ptrType = reflect.PointerTo(elemType)
		vm.ptrTypeCacheKey = elemType
		vm.ptrTypeCacheValue = ptrType
	}
	return unsafePointerKindValue(reflectValueABIType(ptrType), ReflectValuePtr(v))
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
		receiver.IsValid() && receiver.Kind() == reflect.Pointer && !receiver.IsNil() {
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

// handleAddr stores the address of the source value into the destination register.
// Arena-resident values are promoted to their own storage first so the resulting pointer
// cannot alias subsequent arena bytes, unless operand-C is AddrSourceStable.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source value and destination.
// Takes instruction (instruction) which encodes the source/destination register indices.
//
// Returns OpResult indicating the next execution step.
func handleAddr(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.B]
	if !v.IsValid() {
		vMPanicInvalidRegister("handleAddr", "source", instruction.B, instruction, frame, registers)
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

// handleDeref dereferences a pointer in the general register bank and stores the
// pointed-to value in the destination register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the pointer and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleDeref(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
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
