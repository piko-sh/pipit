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
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/logging"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// arenaMaxAlignment is the maximum element alignment the byte slab is guaranteed to
	// satisfy. Types requiring stricter alignment fall back to reflect.MakeSlice /
	// reflect.MakeMapWithSize.
	arenaMaxAlignment uintptr = 8

	// makeMapHintMaxLog2 caps the log2 size hint extension carried by isa.SubOpTier2MakeMap.
	// Higher exponents would yield map sizes outside the representable range of int.
	makeMapHintMaxLog2 uint8 = 31

	// goMaxAllocBytes mirrors the Go runtime's maxAlloc: the largest allocation makeslice
	// accepts before raising its own "out of range" runtime error. 1<<48 on 64-bit targets
	// (48-bit heap addressing) and 1<<32 on 32-bit ones.
	goMaxAllocBytes = uint64(1) << (32 + 16*(^uint(0)>>63))
)

var (
	// errorInterfaceType is the reflect.Type of the built-in error interface, used to detect
	// whether a wrapped closure's signature has a trailing error return slot we can thread a
	// failure into.
	errorInterfaceType = reflect.TypeFor[error]()
)

// sliceOpBounds carries the decoded bounds metadata for isa.SubOpSliceOp.
type sliceOpBounds struct {
	// flags carries the sliceLowBoundFlag, sliceHighBoundFlag and sliceMaxBitFlag bits set
	// by the Compiler.
	flags uint8

	// lowReg holds the register index supplying the low bound when its flag bit is set.
	lowReg uint8

	// highReg holds the register index supplying the high bound when its flag bit is set.
	highReg uint8

	// maxReg holds the register index supplying the max bound when the three-index form is
	// in use.
	maxReg uint8

	// hasMax records whether the instruction encodes a three-index slice.
	hasMax bool
}

// adoptClosureRoot points a callback VM at the closure's own program root and gives it
// the assembly call tables and the shared per-function base array built once for that
// root, so a callback VM never rebuilds either.
//
// Takes closure (*RuntimeClosure) which is about to run on vm.
// Takes wrapperRoot (*program.CompiledFunction) which is the wrapping VM's root, used
// when the closure records none.
func (vm *VM) adoptClosureRoot(closure *RuntimeClosure, wrapperRoot *program.CompiledFunction) {
	root := closure.RootFunction
	if root == nil {
		root = wrapperRoot
	}
	if root == nil {
		return
	}
	vm.functions = root.Functions
	vm.rootFunction = root
	vm.usesTypedSliceBanks = bundleUsesTypedSliceBanks(root)
	vm.AsmCallInfoTables = EnsureASMCallInfoTables(root)
	vm.buildCallInfoBasesByFunction(root)
}

// coerceClosureToFunction converts a RuntimeClosure value to a reflect.Func matching the
// target type. The wrapper captures persistent state rather than the VM itself, so the
// wrapped function remains callable after the original VM is released.
//
// Takes vm (*VM) which provides context for the closure invocation.
// Takes value (reflect.Value) which holds the RuntimeClosure to convert.
// Takes targetType (reflect.Type) which is the desired func type.
//
// Returns reflect.Value wrapping the closure as the target func type.
func coerceClosureToFunction(vm *VM, value reflect.Value, targetType reflect.Type) reflect.Value {
	if targetType.Kind() != reflect.Func {
		return value
	}
	closure, ok := reflect.TypeAssert[*RuntimeClosure](value)
	if !ok {
		return value
	}
	return makeClosureWrapperFunc(vm, closure, targetType)
}

// closureCallableValue wraps a RuntimeClosure in a reflect.Func with a signature derived
// from its compiled function's parameter and result kinds.
//
// When the wrapped closure fails at call time, the failure is threaded into the
// signature's trailing error return (if present) and non-error slots are filled with zero
// values; signatures without an error slot log the failure and return all zero values.
//
// Takes vm (*VM) which provides context for the closure invocation.
// Takes value (reflect.Value) which holds the RuntimeClosure to wrap.
//
// Returns reflect.Value holding a reflect.Func with the derived signature.
func closureCallableValue(vm *VM, value reflect.Value) reflect.Value {
	closure, ok := reflect.TypeAssert[*RuntimeClosure](value)
	if !ok {
		return value
	}
	compiledFunction := closure.Function
	if compiledFunction.SignatureReflectType != nil {
		return makeClosureWrapperFunc(vm, closure, compiledFunction.SignatureReflectType)
	}
	inTypes := make([]reflect.Type, len(compiledFunction.ParameterKinds))
	lastIndex := len(inTypes) - 1
	for i := range compiledFunction.ParameterKinds {
		if compiledFunction.IsVariadic && i == lastIndex {
			inTypes[i] = reflect.TypeFor[[]any]()
			continue
		}
		inTypes[i] = reflect.TypeFor[any]()
	}
	outTypes := make([]reflect.Type, len(compiledFunction.ResultKinds))
	for i, k := range compiledFunction.ResultKinds {
		outTypes[i] = program.KindDefaultReflectType(k)
	}
	functionType := reflect.FuncOf(compiledFunction.VariadicSafeInTypes(inTypes), outTypes, compiledFunction.IsVariadic)
	return makeClosureWrapperFunc(vm, closure, functionType)
}

// makeClosureWrapperFunc builds a reflect.MakeFunc wrapper that runs closure on the
// parent's cached callback VM or a fresh one.
//
// Takes vm (*VM) which provides the persistent state to capture.
// Takes closure (*RuntimeClosure) which is the interpreter closure to invoke on each
// call.
// Takes functionType (reflect.Type) which is the wrapper's reflect.Func signature.
//
// Returns the wrapper reflect.Value of kind Func.
//
// Panics with the closure's panicValue when the inner call raised a Go panic from within
// an active dispatch (re-raised so the host sees the original value).
func makeClosureWrapperFunc(vm *VM, closure *RuntimeClosure, functionType reflect.Type) reflect.Value {
	spec := newCallbackVMSpec(vm)

	handoff := vm.pendingGilHandoff
	if handoff != nil {
		handoff.hasClosure = true
	}

	return reflect.MakeFunc(functionType, func(arguments []reflect.Value) []reflect.Value {
		callbackVM := spec.acquire(closure)
		defer spec.finish(callbackVM)

		acquiredGil := handoff.acquireForCallback()
		if acquiredGil {
			callbackVM.holdsInterpreterLock = true
		}
		defer func() {
			if acquiredGil {
				callbackVM.holdsInterpreterLock = false
				handoff.releaseAfterCallback(true)
			}
		}()
		result := callbackVM.callClosureReflect(closure, arguments, functionType)
		if callbackVM.evalError != nil {
			if callbackVM.panicValue != nil && spec.globals.dispatchDepth.Load() > 0 {
				panic(wrapCallbackPanic(callbackVM))
			}
			return buildClosureErrorReturns(spec.ctx, functionType, callbackVM.evalError)
		}
		return result
	})
}

// buildClosureErrorReturns builds the zero-value return slots a failed reflect.MakeFunc
// wrapper must hand back to its caller.
//
// Takes targetType (reflect.Type) which is the wrapped function type whose return slots
// are being built.
// Takes err (error) which is the interpreter-side failure.
//
// Returns []reflect.Value matching the target signature's outputs.
func buildClosureErrorReturns(ctx context.Context, targetType reflect.Type, err error) []reflect.Value {
	l := logging.LoggerFrom(ctx)
	numOut := targetType.NumOut()
	if numOut == 0 {
		l.Warn("wrapped closure failed with no error return slot", logging.ErrAttr(err))
		return nil
	}
	returns := make([]reflect.Value, numOut)
	lastIsError := targetType.Out(numOut-1) == errorInterfaceType
	for i := range numOut - 1 {
		returns[i] = reflect.Zero(targetType.Out(i))
	}
	if lastIsError {
		returns[numOut-1] = reflect.ValueOf(err)
		return returns
	}
	l.Warn("wrapped closure failed with no error return slot", logging.ErrAttr(err))
	returns[numOut-1] = reflect.Zero(targetType.Out(numOut - 1))
	return returns
}

// handleMakeSlice handles the isa.OpMakeSlice instruction by creating a new slice of the
// specified type, length, and capacity.
//
// For element kinds with matching arena backing slabs
// (byte/int64/float64/bool/uint64/string), the slice's backing array is bump-allocated
// from the arena instead of reflect.MakeSlice triggering mallocgc. Other element kinds
// (struct types, []int on 32-bit, custom interfaces, etc.) still go through
// reflect.MakeSlice, as does any make whose extension word carries
// isa.MakeSliceExtHeapFlag: the Compiler sets it for heap-promoted locals so the closure
// cell's escape barrier never has to copy the backing (which would split aliases and
// shrink the capacity).
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the length and capacity values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMakeSlice(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	length := int(registers.Ints[instruction.B])
	capacity := int(registers.Ints[instruction.C])

	if result, refused := refuseMakeSlice(vm, length, capacity, reflectType.Elem().Size()); refused {
		return result
	}
	if vm.Limits.MaxAllocSize > 0 && length > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf("%w: make slice length %d exceeds limit %d",
			fault.ErrAllocationLimit, length, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	if vm.Limits.MaxAllocSize > 0 && capacity > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf("%w: make slice capacity %d exceeds limit %d",
			fault.ErrAllocationLimit, capacity, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	if extensionWord.C&isa.MakeSliceExtHeapFlag == 0 {
		if backing, ok := arenaMakeSliceBacking(vm, reflectType, length, capacity); ok {
			registers.General[instruction.A] = backing
			return opContinue
		}
	}
	registers.General[instruction.A] = reflect.MakeSlice(reflectType, length, capacity)
	return opContinue
}

// arenaMakeSliceBacking tries to allocate the slice backing from the arena. It returns
// (reflect.Value, true) when the element kind has a typed slab or is pointer-free, and
// (zero, false) otherwise so the caller falls through to reflect.MakeSlice.
//
// Only exact-kind matches are used because Go's []int and []int64 are distinct types even
// when their element sizes match. []byte backing is eagerly cleared to honour Go's zero
// contract. Other typed-slab kinds leave backing unzeroed because the compiler guarantees
// every slot is written before it is read.
//
// Takes vm (*VM) which provides the arena.
// Takes reflectType (reflect.Type) which is the requested slice type.
// Takes length (int) which is the make() length argument.
// Takes capacity (int) which is the make() capacity argument.
//
// Returns reflect.Value wrapping the arena-backed slice when ok, and bool indicating
// whether the arena path was taken.
//
// revive:disable-next-line:cognitive-complexity // single switch on element kind
func arenaMakeSliceBacking(vm *VM, reflectType reflect.Type, length, capacity int) (reflect.Value, bool) {
	if reflectType.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	elem := reflectType.Elem()
	switch elem.Kind() {
	case reflect.Uint8:
		backing := vm.Arena.AllocByteBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.Int64:
		backing := vm.Arena.AllocIntBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.Float64:
		backing := vm.Arena.AllocFloatBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.Bool:
		backing := vm.Arena.AllocBoolBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.Uint64:
		backing := vm.Arena.AllocUintBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.String:
		backing := vm.Arena.AllocStringBacking(capacity)
		clear(backing)
		return arenaWrapMakeBacking(vm.Arena, backing, length, capacity, reflectType), true
	case reflect.Struct, reflect.Array:
		if !typeIsPointerFree(elem) {
			return reflect.Value{}, false
		}
		return arenaMakeStructSliceBacking(vm, reflectType, length, capacity), true
	case reflect.Int32, reflect.Uint32, reflect.Float32, reflect.Int16, reflect.Uint16:
		return arenaMakeStructSliceBacking(vm, reflectType, length, capacity), true
	default:
	}
	return reflect.Value{}, false
}

// arenaMakeStructSliceBacking bump-allocates an arena-backed slice.
//
// The region is sized to hold `capacity` elements of reflectType.Elem() and aligned to
// the element type. The returned reflect.Value is a slice of reflectType with len/cap as
// given. Caller must have verified reflectType.Elem() is pointer-free.
//
// Takes vm (*VM) which provides the arena.
// Takes reflectType (reflect.Type) which is the slice type to build.
// Takes length (int) which is the initial slice length.
// Takes capacity (int) which is the slice capacity.
//
// Returns a reflect.Value of type reflectType with len/cap as given.
func arenaMakeStructSliceBacking(vm *VM, reflectType reflect.Type, length, capacity int) reflect.Value {
	if capacity == 0 {
		return reflect.MakeSlice(reflectType, length, capacity)
	}
	elem := reflectType.Elem()
	elemSize := elem.Size()
	align := safeconv.IntToUintptr(elem.Align())
	if align == 0 {
		align = 1
	}
	if align > arenaMaxAlignment {
		return reflect.MakeSlice(reflectType, length, capacity)
	}
	backingBytes := elemSize * safeconv.IntToUintptr(capacity)
	dataPtr := vm.Arena.AllocBytes(backingBytes, align)

	clear(unsafe.Slice((*byte)(dataPtr), backingBytes))
	slot := vm.Arena.allocSliceHeader()
	slot.Data = dataPtr
	slot.Len = length
	slot.Cap = capacity
	return unsafeNewAt(reflectValueABIType(reflectType), unsafe.Pointer(slot), reflect.Slice)
}

// handleMakeMap handles the isa.SubOpTier2MakeMap instruction by creating a new map or
// struct value of the type specified in the type table.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the destination general bank.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMakeMap(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	if reflectType.Kind() == reflect.Struct {
		registers.General[instruction.A] = allocateStructLiteralValue(vm, reflectType)
		return opContinue
	}
	if result, refused := refuseMapSizeHint(vm, extensionWord.C); refused {
		return result
	}
	registers.General[instruction.A] = makeMapWithHint(reflectType, extensionWord.C)
	return opContinue
}

// refuseMapSizeHint applies the host's single-allocation cap to a map size hint.
//
// make([]T, n), make(chan T, n) and append all consult MaxAllocSize before they reserve
// space; the map hint is the one reservation that did not, so a constant hint could
// preallocate far past the budget the host configured. The hint is a log2 exponent, so
// the comparison is done on the entry count it expands to.
//
// Takes vm (*VM) which carries the configured limits.
// Takes hintLog (uint8) which is the log2 size hint carried by the extension word.
//
// Returns the OpResult of the raised fault and true when the hint exceeds the cap.
func refuseMapSizeHint(vm *VM, hintLog uint8) (OpResult, bool) {
	if vm.Limits.MaxAllocSize <= 0 || hintLog == 0 {
		return opContinue, false
	}
	if hintLog > makeMapHintMaxLog2 {
		hintLog = makeMapHintMaxLog2
	}
	entries := uint64(1) << hintLog
	if entries <= uint64(vm.Limits.MaxAllocSize) {
		return opContinue, false
	}
	vm.evalError = fmt.Errorf("%w: make map size hint %d exceeds limit %d",
		fault.ErrAllocationLimit, entries, vm.Limits.MaxAllocSize)
	return opPanicError, true
}

// allocateStructLiteralValue allocates a struct-typed reflect.Value for a struct-literal
// site. Pointer-free structs use the arena byte slab and pointer-containing structs route
// through the boundary-snapshot chunk slab so the GC can scan pointer fields.
//
// The arena slab slot must be explicitly zeroed because the byte slab is pool-reused and
// may carry stale data from a previous allocation.
//
// Takes vm (*VM) which provides the arena.
// Takes reflectType (reflect.Type) which is the struct type being allocated.
//
// Returns the struct-typed reflect.Value.
func allocateStructLiteralValue(vm *VM, reflectType reflect.Type) reflect.Value {
	if typeIsPointerFree(reflectType) {
		align := safeconv.IntToUintptr(reflectType.Align())
		if align == 0 {
			align = 1
		}
		if align <= arenaMaxAlignment {
			size := reflectType.Size()
			ptr := vm.Arena.AllocBytes(size, align)
			if size > 0 {
				clear(unsafe.Slice((*byte)(ptr), size))
			}
			return unsafeNewAt(reflectValueABIType(reflectType), ptr, reflect.Struct)
		}
	}
	return vm.acquireBoundarySnapshot(reflectType)
}

// makeMapWithHint constructs a map of reflectType, sized from the isa.SubOpTier2MakeMap
// log2 hint when present.
//
// hintLog encodes log2(size hint); a zero value means no hint and routes to the unsized
// reflect.MakeMap. Values above makeMapHintMaxLog2 are clamped so the shift cannot
// overflow.
//
// Takes reflectType (reflect.Type) which is the map type to build.
// Takes hintLog (uint8) which is the encoded size hint.
//
// Returns the map-kind reflect.Value.
func makeMapWithHint(reflectType reflect.Type, hintLog uint8) reflect.Value {
	if hintLog == 0 {
		return reflect.MakeMap(reflectType)
	}
	if hintLog > makeMapHintMaxLog2 {
		hintLog = makeMapHintMaxLog2
	}
	return reflect.MakeMapWithSize(reflectType, 1<<hintLog)
}

// handleSetZero zeroes the composite value in general[A], setting all fields to their
// zero values. Used by the assign-through optimisation to clear a slice/array element
// before writing individual fields.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the destination value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSetZero(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	if !v.IsValid() {
		vMPanicInvalidRegister("handleSetZero", "target", instruction.A, instruction, frame, registers)
	}
	v.SetZero()
	return opContinue
}

// handleMakeChannel handles the isa.SubOpMakeChannel instruction by creating a new
// channel of the specified type and buffer size.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the buffer size and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMakeChannel(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	bufSize := int(registers.Ints[instruction.B])
	if bufSize < 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: makechan: size out of range"))
	}
	if vm.Limits.MaxAllocSize > 0 && bufSize > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf("%w: make chan buffer %d exceeds limit %d",
			fault.ErrAllocationLimit, bufSize, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	channel := reflect.MakeChan(reflectType, bufSize)
	vm.Globals.noteInterpretedChannel(channel)
	registers.General[instruction.A] = channel
	return opContinue
}

// handleLen handles the isa.SubOpLen instruction by computing the length of a general
// register value and storing the result in an int register.
//
// Takes registers (*Registers) which holds the source and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLen(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = collectionLengthOrCap(registers.General[instruction.B], reflect.Value.Len)
	return opContinue
}

// collectionLengthOrCap returns len(v) or cap(v) while honouring Go's rule that len/cap
// on a *[N]T returns N even when the pointer is nil or would otherwise panic under
// reflect.
//
// Takes v (reflect.Value) which is the collection value.
// Takes measure (func(reflect.Value) int) which is either reflect.Value.Len or
// reflect.Value.Cap.
//
// Returns the computed length as int64.
func collectionLengthOrCap(v reflect.Value, measure func(reflect.Value) int) int64 {
	if !v.IsValid() {
		return 0
	}
	if v.Kind() == reflect.Pointer && v.Type().Elem().Kind() == reflect.Array {
		return int64(v.Type().Elem().Len())
	}
	return int64(measure(v))
}

// handleSliceOp handles the isa.SubOpSliceOp instruction by performing a slice operation
// with optional low, high, and max bounds.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the bounds extension words.
// Takes registers (*Registers) which holds the collection and bounds.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
//
// For Slice-kind collections, bypasses reflect.Value.Slice/.Slice3 (each allocates a
// fresh 24-byte heap slice header) by constructing the result Value via unsafe with the
// new header in a slab-allocated slot. Array-kind collections (the `arr[:]` pattern) get
// the same treatment with cap derived from the array's length.
func handleSliceOp(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	bounds := readSliceBoundFlags(frame)
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.C])
	if !ok {
		return indexPanicResult
	}
	low := 0
	if bounds.flags&isa.SliceLowBoundFlag != 0 {
		low = int(registers.Ints[bounds.lowReg])
	}
	high := collection.Len()
	if bounds.flags&isa.SliceHighBoundFlag != 0 {
		high = int(registers.Ints[bounds.highReg])
	}
	maxBound := collection.Cap()
	if bounds.hasMax {
		maxBound = int(registers.Ints[bounds.maxReg])
	}
	capacity := collection.Cap()
	if collection.Kind() == reflect.Array {
		capacity = collection.Len()
	}
	if result, ok := checkSliceOpBounds(vm, low, high, maxBound, capacity, bounds.hasMax); !ok {
		return result
	}
	if collection.Kind() == reflect.Slice {
		registers.General[instruction.B] = sliceFromSliceFast(vm, collection, low, high, maxBound, bounds.hasMax)
		return opContinue
	}
	if collection.Kind() == reflect.Array {
		if result, sliced := sliceFromArrayFast(vm, collection, low, high, maxBound); sliced {
			registers.General[instruction.B] = result
			return opContinue
		}
	}
	if bounds.hasMax {
		registers.General[instruction.B] = collection.Slice3(low, high, maxBound)
	} else {
		registers.General[instruction.B] = collection.Slice(low, high)
	}
	return opContinue
}

// checkSliceOpBounds validates slice-expression bounds against Go runtime rules.
//
// The check covers [low:high] / [low:high:max] forms for slice and array operations,
// mirroring Go's runtime error messages so interpreted defer/recover() observes parity.
// On failure, raises an interpreted-side runtime panic and returns the propagated
// OpResult. Go's runtime emits a different message depending on which bound is violated
// (low < 0 yields "[low:]", high < low yields "[low:high]", a high or max overshoot
// includes the capacity suffix), so each branch builds the matching diagnostic.
//
// Takes vm (*VM) which is used to raise the interpreted panic.
// Takes low (int) which is the requested lower bound.
// Takes high (int) which is the requested upper bound.
// Takes maxBound (int) which is the requested capacity bound (only considered when hasMax
// is true).
// Takes capacity (int) which is the underlying storage capacity used for the diagnostic
// message.
// Takes hasMax (bool) which is true when the slice expression included an explicit `:max`
// bound (Slice3 form).
//
// Returns the OpResult to propagate and a boolean flagging valid / invalid bounds.
func checkSliceOpBounds(vm *VM, low, high, maxBound, capacity int, hasMax bool) (OpResult, bool) {
	switch {
	case low < 0:
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: slice bounds out of range [%d:]", low)), false
	case high < low:
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: slice bounds out of range [%d:%d]", low, high)), false
	case hasMax && maxBound < high:
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: slice bounds out of range [:%d:%d]", high, maxBound)), false
	case hasMax && maxBound > capacity:
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: slice bounds out of range [:%d:%d] with capacity %d", high, maxBound, capacity)), false
	case high > capacity:
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: slice bounds out of range [:%d] with capacity %d", high, capacity)), false
	}
	return opContinue, true
}

// readSliceBoundFlags reads the bounds extension words for isa.SubOpSliceOp.
//
// Takes frame (*CallFrame) which provides the extension words.
//
// Returns the decoded sliceOpBounds.
func readSliceBoundFlags(frame *CallFrame) sliceOpBounds {
	ext1 := readExtensionWord(frame)
	frame.ProgramCounter++
	result := sliceOpBounds{flags: ext1.A,
		lowReg:  ext1.B,
		highReg: ext1.C,
		hasMax:  ext1.A&isa.SliceMaxBitFlag != 0, maxReg: 0}
	if result.hasMax {
		ext2 := readExtensionWord(frame)
		frame.ProgramCounter++
		result.maxReg = ext2.A
	}
	return result
}

// sliceFromSliceFast builds the result reflect.Value for the slice- kind branch of
// handleSliceOp, falling back to reflect.Slice/Slice3 on bounds violation so users see
// the canonical panic message.
//
// Takes vm (*VM) which provides the slice-header slab.
// Takes collection (reflect.Value) which is the source slice.
// Takes low (int) which is the lower slice bound.
// Takes high (int) which is the upper slice bound.
// Takes maxBound (int) which is the maximum capacity bound.
// Takes hasMax (bool) which indicates the three-index form.
//
// Returns the result reflect.Value.
func sliceFromSliceFast(vm *VM, collection reflect.Value, low, high, maxBound int, hasMax bool) reflect.Value {
	sourceHeaderPtr := ReflectValuePtr(collection)
	if sourceHeaderPtr == nil {
		if hasMax {
			return collection.Slice3(low, high, maxBound)
		}
		return collection.Slice(low, high)
	}
	sourceHeader := (*snapshotSliceHeader)(sourceHeaderPtr)
	if low < 0 || low > high || high > sourceHeader.Cap || maxBound < high || maxBound > sourceHeader.Cap {
		if hasMax {
			return collection.Slice3(low, high, maxBound)
		}
		return collection.Slice(low, high)
	}
	elemSize := uintptr(0)
	if sourceHeader.Cap > 0 {
		elemSize = collection.Type().Elem().Size()
	}
	destination := vm.acquireSliceSnapshot()
	destinationCap := maxBound - low
	offsetBytes := uintptr(low) * elemSize

	switch {
	case sourceHeader.Data == nil:
		destination.Data = nil
	case offsetBytes == 0:
		destination.Data = sourceHeader.Data
	case low == sourceHeader.Cap:
		destination.Data = zeroSizeAllocPtr
	default:
		destination.Data = unsafe.Add(sourceHeader.Data, offsetBytes)
	}
	destination.Len = high - low
	destination.Cap = destinationCap
	return unsafeReadOnlyValue(reflectValueABIType(collection.Type()), unsafe.Pointer(destination), reflect.Slice)
}

// sliceFromArrayFast builds a slice header over an array's backing storage without
// requiring collection.CanAddr(), because the reflect.Value's ptr field is stable until
// the register is overwritten.
//
// Takes vm (*VM) which provides the slice-header slab.
// Takes collection (reflect.Value) which is the source array.
// Takes low (int) which is the lower slice bound.
// Takes high (int) which is the upper slice bound.
// Takes maxBound (int) which is the maximum capacity bound.
//
// Returns the result reflect.Value and true on success, or zero and false when bounds are
// invalid so the caller falls back to reflect.
func sliceFromArrayFast(vm *VM, collection reflect.Value, low, high, maxBound int) (reflect.Value, bool) {
	arrLen := collection.Type().Len()
	if low < 0 || low > high || high > arrLen || maxBound < high || maxBound > arrLen {
		return reflect.Value{}, false
	}
	elemType := collection.Type().Elem()
	arrPtr := ReflectValuePtr(collection)
	if arrPtr == nil {
		return reflect.Value{}, false
	}
	destination := vm.acquireSliceSnapshot()

	if low > 0 && low == arrLen {
		destination.Data = zeroSizeAllocPtr
	} else {
		destination.Data = unsafe.Add(arrPtr, uintptr(low)*elemType.Size())
	}
	destination.Len = high - low
	destination.Cap = maxBound - low
	sliceType := reflect.SliceOf(elemType)
	return unsafeReadOnlyValue(reflectValueABIType(sliceType), unsafe.Pointer(destination), reflect.Slice), true
}

// handleCopy handles the isa.OpCopy instruction by copying elements between slices and
// storing the number of elements copied.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the destination and source slices.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleCopy(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	destination := registers.General[instruction.B]
	source := registers.General[instruction.C]
	if !destination.IsValid() || destination.Kind() != reflect.Slice {
		vm.evalError = newInvariantError("copy destination is not a slice")
		return opPanicError
	}
	if !source.IsValid() {
		vm.evalError = newInvariantError("copy source is invalid")
		return opPanicError
	}
	if source.Kind() != reflect.Slice && source.Kind() != reflect.Array && source.Kind() != reflect.String {
		vm.evalError = newInvariantError("copy source is not a slice, array, or string")
		return opPanicError
	}
	registers.Ints[instruction.A] = int64(reflect.Copy(destination, source))
	return opContinue
}

// refuseMakeSlice applies the Go runtime's makeslice checks and raises the matching
// recoverable runtime error: "len out of range" when the length alone is negative or too
// large for the heap, otherwise "cap out of range" when the capacity is negative, below
// the length or too large.
//
// Takes vm (*VM) which is the executing VM.
// Takes length (int) which is the requested length.
// Takes capacity (int) which is the requested capacity.
// Takes elementSize (uintptr) which is the element size in bytes.
//
// Returns the OpResult of the raised panic and true when Go would refuse the request.
func refuseMakeSlice(vm *VM, length, capacity int, elementSize uintptr) (OpResult, bool) {
	if length < 0 || allocationTooLarge(length, elementSize) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: makeslice: len out of range")), true
	}
	if capacity < 0 || capacity < length || allocationTooLarge(capacity, elementSize) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: makeslice: cap out of range")), true
	}
	return opContinue, false
}

// allocationTooLarge reports whether count elements of elementSize bytes exceed the Go
// runtime's maximum allocation, treating multiplication overflow as too large.
//
// Takes count (int) which is a non-negative element count.
// Takes elementSize (uintptr) which is the element size in bytes.
//
// Returns bool which is true when Go's makeslice would refuse the allocation.
func allocationTooLarge(count int, elementSize uintptr) bool {
	if elementSize == 0 || count == 0 {
		return false
	}
	elements := safeconv.IntToUint64(count)
	size := uint64(elementSize)
	bytes := elements * size
	return bytes/size != elements || bytes > goMaxAllocBytes
}
