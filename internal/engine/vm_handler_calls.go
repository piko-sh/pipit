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
	"maps"
	"reflect"
	"sync"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

var (
	// reflectValueBufferPool pools reflect.Value scratch buffers for buildReflectArgs and
	// handleCallBoundMethodReflect.
	reflectValueBufferPool = sync.Pool{
		New: func() any {
			return new(make([]reflect.Value, 0, 8))
		},
	}
)

// runReturnDefers runs the returning frame's remaining deferred calls through the
// nested-dispatch path (native targets and closures that recover) and reports whether the
// return must stop early because a deferred call raised an error or unwound the frame.
//
// Takes frame (*CallFrame) which is the returning call frame.
//
// Returns the OpResult to report and true when the return must not continue, or
// opContinue and false when the handler should finish the return normally.
func (vm *VM) runReturnDefers(frame *CallFrame) (OpResult, bool) {
	expectedFp := vm.FramePointer
	if frame.simpleDefer != nil && frame.simpleDefer.active {
		vm.runFrameSimpleDefer(frame)
	}
	if len(vm.deferStack) > frame.deferBase {
		vm.runDefers()
	}
	if vm.evalError != nil {
		return opPanicError, true
	}
	if vm.FramePointer < expectedFp {
		if vm.FramePointer < vm.baseFramePointer {
			return opDone, true
		}
		return opFrameChanged, true
	}
	return opContinue, false
}

// repointClosureACIBase repoints the current frame's AsmCallInfo base at this VM's
// private clone of the function's table, when one exists.
//
// Frames pushed by the ASM inline-call path stamp the shared build-time base, which never
// carries runtime closure fills; without the repoint those frames would take the Go
// fallback for every closure call even though a filled entry exists. Also syncs the live
// dispatch context when it is sitting on the same frame, so the fast path engages at the
// next dispatch rather than after the next full rebuild.
//
// Takes function (*CompiledFunction) which is the frame's function.
func (vm *VM) repointClosureACIBase(function *program.CompiledFunction) {
	if vm.PrivateACITables == nil || !vm.PrivateACITables[function] {
		return
	}
	fp := vm.FramePointer
	if fp < 0 || fp >= len(vm.asmCallInfoBases) {
		return
	}
	table := vm.AsmCallInfoTables[function]
	if len(table) == 0 {
		return
	}
	base := uintptr(unsafe.Pointer(&table[0]))
	if vm.asmCallInfoBases[fp] == base {
		return
	}
	vm.asmCallInfoBases[fp] = base
	if vm.liveCtx != nil && int(vm.liveCtx.framePointer) == fp {
		vm.liveCtx.asmCallInfoBase = base
	}
}

// ensurePrivateACITable makes the caller's AsmCallInfo table private to this VM before
// the first runtime closure fill mutates it.
//
// Takes caller (*CompiledFunction) which owns the table entry being filled.
// Takes fp (int) which is the live frame pointer whose base is repointed on first clone.
// Takes table ([]AsmCallInfo) which is the caller's current (possibly shared) table.
//
// Returns the table this VM must fill into, the private clone on first fill, or the
// argument table unchanged.
func (vm *VM) ensurePrivateACITable(caller *program.CompiledFunction, fp int, table []AsmCallInfo) []AsmCallInfo {
	if vm.PrivateACITables[caller] {
		return table
	}
	if vm.PrivateACITables == nil {
		vm.PrivateACITables = make(map[*program.CompiledFunction]bool)
		privateMap := make(map[*program.CompiledFunction][]AsmCallInfo, len(vm.AsmCallInfoTables))
		maps.Copy(privateMap, vm.AsmCallInfoTables)
		vm.AsmCallInfoTables = privateMap
	}
	clone := make([]AsmCallInfo, len(table))
	copy(clone, table)
	vm.AsmCallInfoTables[caller] = clone
	vm.PrivateACITables[caller] = true
	vm.publishCallInfoBaseFor(caller, uintptr(unsafe.Pointer(&clone[0])))
	vm.asmCallInfoBases[fp] = uintptr(unsafe.Pointer(&clone[0]))
	if vm.liveCtx != nil && int(vm.liveCtx.framePointer) == fp {
		vm.liveCtx.asmCallInfoBase = vm.asmCallInfoBases[fp]
	}
	return clone
}

// tryPopulateClosureASMInfo fills the call site's AsmCallInfo entry for the closure the
// site just cached.
//
// Makes the site eligible for the ASM inline-call fast path with a closure-pointer guard.
// Entries are only populated when the callee passes the static eligibility gates
// (non-variadic, unperturbed scalar parameter layout, no complex registers), the closure
// shares the current root function (no cross-bundle snapshot machinery), and no
// goroutines exist (matching the Go-side cache's guard: the ASM re-reads the context's
// goroutine flag on every entry after Go execution, so a spawn invalidates the fast path
// at the next dispatch boundary). A different closure arriving at the same site fails the
// ASM pointer guard, falls back to this Go path, and re-fills the entry, matching the
// single-slot semantics of the site's Go closure cache.
//
// Takes caller (*program.CompiledFunction) which is the calling function.
// Takes siteIndex (uint16) which is the call-site index within the caller.
// Takes site (*CallSite) which owns the AsmCallInfo entry.
// Takes callee (*CompiledFunction) which is the closure's compiled body.
// Takes closurePointer (unsafe.Pointer) which is the closure's identity word.
// Takes closureCells ([]*UpvalueCell) which is the closure's upvalue cell slice.
// Takes closureRoot (*CompiledFunction) which is the closure's root function.
func (vm *VM) tryPopulateClosureASMInfo(
	caller *program.CompiledFunction,
	siteIndex uint16,
	site *program.CallSite,
	callee *program.CompiledFunction,
	closurePointer unsafe.Pointer,
	closureCells []*program.UpvalueCell,
	closureRoot *program.CompiledFunction,
) {
	if callee == nil || caller == nil || vm.AsmCallInfoTables == nil {
		return
	}
	fp := vm.FramePointer
	if fp < 0 || fp >= len(vm.asmCallInfoBases) {
		return
	}
	table := vm.AsmCallInfoTables[caller]
	if int(siteIndex) >= len(table) {
		return
	}
	table = vm.ensurePrivateACITable(caller, fp, table)
	info := &table[siteIndex]
	if closureRoot != nil && closureRoot != vm.rootFunction {
		info.closurePointer = 0
		return
	}
	if callee.IsVariadic || callee.NumRegisters[isa.RegisterComplex] > 0 || asmCalleeParameterLayoutPerturbed(callee) {
		info.closurePointer = 0
		return
	}
	if !mapASMArguments(info, site, callee) {
		info.closurePointer = 0
		return
	}
	if !ConfigureASMReturn(info, site, callee) {
		info.closurePointer = 0
		return
	}
	populateASMCalleeFields(info, site, callee)
	info.calleeFunctionIndex = vm.calleeBaseSlot(callee)
	info.closureValueRegister = int64(site.ClosureRegister)
	if len(closureCells) > 0 {
		info.closureUpvaluesPtr = uintptr(unsafe.Pointer(&closureCells[0]))
	} else {
		info.closureUpvaluesPtr = 0
	}
	info.closureUpvaluesLen = int64(len(closureCells))
	info.closureUpvaluesCap = int64(cap(closureCells))
	info.closurePointer = uintptr(closurePointer)
}

// acquireReflectValueBuffer returns a buffer with at least n slots, zero-padded for safe
// writes. The caller MUST pass the returned buffer through releaseReflectValueBuffer once
// the values are no longer needed (typically via defer immediately after acquisition).
//
// Takes n (int) which is the required length.
//
// Returns a buffer sized to n with zeroed entries.
func acquireReflectValueBuffer(n int) []reflect.Value {
	pointer, ok := reflectValueBufferPool.Get().(*[]reflect.Value)
	if !ok {
		buffer := make([]reflect.Value, n)
		return buffer
	}
	buffer := *pointer
	if cap(buffer) < n {
		buffer = make([]reflect.Value, n)
	} else {
		buffer = buffer[:n]
		clear(buffer)
	}
	*pointer = buffer
	return buffer
}

// releaseReflectValueBuffer returns a buffer to the pool.
//
// The caller must not retain references to any reflect.Value inside the buffer after
// release; subsequent acquirers may zero or overwrite the entries. Safe to call with a
// nil-or-zero-length buffer (no-op).
//
// Takes buffer ([]reflect.Value) which is the buffer to recycle.
func releaseReflectValueBuffer(buffer []reflect.Value) {
	if cap(buffer) == 0 {
		return
	}
	fullBuffer := buffer[:cap(buffer)]
	clear(fullBuffer)
	reflectValueBufferPool.Put(new(fullBuffer[:0]))
}

// registerToReflectValue reads a register value and returns it as a reflect.Value. Used
// for marshalling arguments to native calls.
//
// When arena is non-nil, primitive scalar kinds route through the arena box helpers (zero
// mallocgc per call); typed slice kinds use the arena slice-header pool. When arena is
// nil, falls back to the allocating reflect.ValueOf path (test contexts).
//
// Takes arena (*RegisterArena) which provides the bump arena; may be nil.
// Takes registers (*Registers) which holds the register banks.
// Takes kind (isa.RegisterKind) which selects the typed register bank.
// Takes register (uint8) which is the index within the selected bank.
//
// Returns reflect.Value wrapping the register value, or an invalid reflect.Value if the
// kind is unrecognised.
func registerToReflectValue(arena *RegisterArena, registers *Registers, kind isa.RegisterKind, register uint8) reflect.Value {
	switch kind {
	case isa.RegisterInt:
		return boxInt64ToGeneral(arena, registers.Ints[register])
	case isa.RegisterFloat:
		return boxFloat64ToGeneral(arena, registers.Floats[register])
	case isa.RegisterString:
		return BoxStringToGeneral(arena, registers.Strings[register])
	case isa.RegisterGeneral:
		return registers.General[register]
	case isa.RegisterBool:
		return boxBoolToGeneral(registers.Bools[register])
	case isa.RegisterUint:
		return boxUint64ToGeneral(arena, registers.Uints[register])
	case isa.RegisterComplex:
		return boxComplex128ToGeneral(arena, registers.Complex[register])
	case isa.RegisterSliceInt:
		return packTypedSliceToGeneral(arena, registers.SlicesInt[register], intSliceReflectType)
	case isa.RegisterSliceFloat:
		return packTypedSliceFloatToGeneral(arena, registers.slicesFloat[register])
	case isa.RegisterSliceString:
		return PackTypedSliceStringToGeneral(arena, registers.slicesString[register])
	case isa.RegisterSliceBool:
		return packTypedSliceBoolToGeneral(arena, registers.slicesBool[register])
	case isa.RegisterSliceUint:
		return packTypedSliceUintToGeneral(arena, registers.slicesUint[register])
	case isa.RegisterSliceByte:
		if arena == nil {
			return reflect.ValueOf(registers.slicesByte[register])
		}
		return arenaWrapByteSlice(arena, registers.slicesByte[register])
	default:
		return reflect.Value{}
	}
}

// unpackReflectArgs reads argumentCount extension words from the bytecode stream and
// returns them as a []reflect.Value slice. Each extension word encodes a source register
// (extensionWord.b) and its kind (extensionWord.c).
//
// Takes frame (*CallFrame) which provides the bytecode body and counter.
// Takes registers (*Registers) which holds the register banks.
// Takes argumentCount (int) which specifies how many extension words to consume.
//
// Returns []reflect.Value with length argumentCount containing the arguments.
func unpackReflectArgs(frame *CallFrame, registers *Registers, argumentCount int) []reflect.Value {
	arguments := make([]reflect.Value, argumentCount)
	for i := range argumentCount {
		extensionWord := readExtensionWord(frame)
		frame.ProgramCounter++
		arguments[i] = registerToReflectValue(nil, registers, isa.RegisterKind(extensionWord.C), extensionWord.B)
	}
	return arguments
}

// copyCallArgs copies arguments from caller registers to a new callee frame. Destination
// indices are per-kind (matching the Compiler's per-bank allocation) rather than the
// overall parameter index.
//
// Takes vm (*VM) which is the virtual machine.
// Takes arena (*RegisterArena) which is the allocation arena.
// Takes callerRegisters (*Registers) which holds the source values.
// Takes newFrame (*CallFrame) which is the destination frame to populate.
// Takes site (*CallSite) which describes argument locations in the caller.
// Takes callee (*CompiledFunction) which provides expected parameter kinds.
func copyCallArgs(vm *VM, arena *RegisterArena, callerRegisters *Registers, newFrame *CallFrame, site *program.CallSite, callee *program.CompiledFunction) {
	if site.ArgCopyProgram != nil {
		runArgCopyProgram(vm, arena, callerRegisters, &newFrame.Registers, site.ArgCopyProgram)
		return
	}
	var kindIndex [isa.NumRegisterKinds]int
	scattered := len(callee.ParameterRegisters) == len(callee.ParameterKinds)
	for i, argumentLocation := range site.Arguments {
		if i >= len(callee.ParameterKinds) {
			break
		}
		parameterKind := callee.ParameterKinds[i]
		dest := kindIndex[parameterKind]
		kindIndex[parameterKind]++
		if scattered {
			dest = int(callee.ParameterRegisters[i])
		}
		copyOneCallArgument(&newFrame.Registers, callerRegisters, parameterKind, argumentLocation.Kind, dest, argumentLocation.Register, arena)
	}
}

// runArgCopyProgram executes the per-site precomputed argument-copy program. Each entry
// maps a source register slot directly to a destination slot in the same bank, except for
// general-bank entries (which detect struct/array values and arena-copy them to defeat
// caller aliasing) and the boxing/unboxing fallback.
//
// Takes vm (*VM) which carries the per-call boundary helpers used for general-bank
// struct/array copies.
// Takes arena (*RegisterArena) which provides the arena copy helpers; may be nil.
// Takes callerRegisters (*Registers) which holds the source values.
// Takes destination (*Registers) which receives the copied values.
// Takes plan ([]CallArgCopy) which lists each per-entry copy op.
func runArgCopyProgram(vm *VM, arena *RegisterArena, callerRegisters *Registers, destination *Registers, plan []program.CallArgCopy) {
	for i := range plan {
		c := &plan[i]
		switch c.Op {
		case program.CopyIntToInt:
			destination.Ints[c.DestinationRegister] = callerRegisters.Ints[c.SourceRegister]
		case program.CopyFloatToFloat:
			destination.Floats[c.DestinationRegister] = callerRegisters.Floats[c.SourceRegister]
		case program.CopyStringToString:
			destination.Strings[c.DestinationRegister] = callerRegisters.Strings[c.SourceRegister]
		case program.CopyGeneralToGeneral:
			source := callerRegisters.General[c.SourceRegister]
			if source.Kind() == reflect.Struct || source.Kind() == reflect.Array {
				destination.General[c.DestinationRegister] = valueCopyForBoundaryArenaWithVM(arena, vm, source)
			} else {
				destination.General[c.DestinationRegister] = source
			}
		case program.CopyBoolToBool:
			destination.Bools[c.DestinationRegister] = callerRegisters.Bools[c.SourceRegister]
		case program.CopyUintToUint:
			destination.Uints[c.DestinationRegister] = callerRegisters.Uints[c.SourceRegister]
		case program.CopyComplexToComplex:
			destination.Complex[c.DestinationRegister] = callerRegisters.Complex[c.SourceRegister]
		case program.CopySliceIntToSliceInt:
			destination.SlicesInt[c.DestinationRegister] = callerRegisters.SlicesInt[c.SourceRegister]
		case program.CopySliceFloatToSliceFloat:
			destination.slicesFloat[c.DestinationRegister] = callerRegisters.slicesFloat[c.SourceRegister]
		case program.CopySliceStringToSliceString:
			destination.slicesString[c.DestinationRegister] = callerRegisters.slicesString[c.SourceRegister]
		case program.CopySliceBoolToSliceBool:
			destination.slicesBool[c.DestinationRegister] = callerRegisters.slicesBool[c.SourceRegister]
		case program.CopySliceUintToSliceUint:
			destination.slicesUint[c.DestinationRegister] = callerRegisters.slicesUint[c.SourceRegister]
		case program.CopySliceByteToSliceByte:
			destination.slicesByte[c.DestinationRegister] = callerRegisters.slicesByte[c.SourceRegister]
		case program.CopyBoxOrUnbox:
			sourceKind := isa.RegisterKind(c.KindByte & 0x0F)
			destinationKind := isa.RegisterKind(c.KindByte >> 4)
			copyOneCallArgument(destination, callerRegisters, destinationKind, sourceKind, int(c.DestinationRegister), c.SourceRegister, arena)
		}
	}
}

// copyOneCallArgument copies a single argument value from the source register bank to the
// destination register bank, handling same-kind copies, scalar-to-general boxing,
// general-to-scalar unboxing, and the int bank's 0 or 1 encoding of a comparison result
// passed to a bool parameter.
//
// Takes destination (*Registers) which is the destination register set.
// Takes source (*Registers) which is the source register set.
// Takes destinationKind (isa.RegisterKind) which is the expected kind in the callee.
// Takes sourceKind (isa.RegisterKind) which is the actual kind in the caller.
// Takes destinationRegister (int) which is the destination index in the typed bank.
// Takes sourceRegister (uint8) which is the source index in the typed bank.
// Takes arena (*RegisterArena) which receives bump-allocated narrow-int widening backings
// when unboxing crosses element widths; may be nil when no active VM context is
// available.
func copyOneCallArgument(destination, source *Registers, destinationKind, sourceKind isa.RegisterKind, destinationRegister int, sourceRegister uint8, arena *RegisterArena) {
	switch {
	case sourceKind == destinationKind:
		copySameKindArg(destination, source, destinationKind, destinationRegister, sourceRegister)
	case destinationKind == isa.RegisterGeneral:
		boxScalarToGeneral(destination, source, sourceKind, destinationRegister, sourceRegister, arena)
	case sourceKind == isa.RegisterGeneral:
		unboxGeneralToScalar(destination, source.General[sourceRegister], destinationKind, destinationRegister, arena)
	case destinationKind == isa.RegisterBool && sourceKind == isa.RegisterInt:
		destination.Bools[destinationRegister] = source.Ints[sourceRegister] != 0
	case destinationKind == isa.RegisterInt && sourceKind == isa.RegisterBool:
		destination.Ints[destinationRegister] = boolToInt64(source.Bools[sourceRegister])
	}
}

// copySameKindArg copies a register value when source and destination kinds match.
//
// Takes destination (*Registers) which is the destination register set.
// Takes source (*Registers) which is the source register set.
// Takes kind (isa.RegisterKind) which selects the typed bank to use.
// Takes destinationRegister (int) which is the destination index in the bank.
// Takes sourceRegister (uint8) which is the source index in the bank.
func copySameKindArg(destination, source *Registers, kind isa.RegisterKind, destinationRegister int, sourceRegister uint8) {
	switch kind {
	case isa.RegisterInt:
		destination.Ints[destinationRegister] = source.Ints[sourceRegister]
	case isa.RegisterFloat:
		destination.Floats[destinationRegister] = source.Floats[sourceRegister]
	case isa.RegisterString:
		destination.Strings[destinationRegister] = source.Strings[sourceRegister]
	case isa.RegisterGeneral:
		destination.General[destinationRegister] = ValueCopyForBoundary(source.General[sourceRegister])
	case isa.RegisterBool:
		destination.Bools[destinationRegister] = source.Bools[sourceRegister]
	case isa.RegisterUint:
		destination.Uints[destinationRegister] = source.Uints[sourceRegister]
	case isa.RegisterComplex:
		destination.Complex[destinationRegister] = source.Complex[sourceRegister]
	case isa.RegisterSliceInt:
		destination.SlicesInt[destinationRegister] = source.SlicesInt[sourceRegister]
	case isa.RegisterSliceFloat:
		destination.slicesFloat[destinationRegister] = source.slicesFloat[sourceRegister]
	case isa.RegisterSliceString:
		destination.slicesString[destinationRegister] = source.slicesString[sourceRegister]
	case isa.RegisterSliceBool:
		destination.slicesBool[destinationRegister] = source.slicesBool[sourceRegister]
	case isa.RegisterSliceUint:
		destination.slicesUint[destinationRegister] = source.slicesUint[sourceRegister]
	case isa.RegisterSliceByte:
		destination.slicesByte[destinationRegister] = source.slicesByte[sourceRegister]
	default:
	}
}

// boxScalarToGeneral wraps a typed register value into a reflect.Value stored in the
// general bank. Used when a scalar argument must be passed as interface{}.
//
// Scalar boxes are bump-allocated from arena when one is supplied, so a polymorphic call
// with an interface parameter does not allocate on the heap; the boxes live as long as
// the callee's registers and the escape barrier detaches them if they leave the arena.
// Only the banks with a dedicated sub-op are listed. The default routes through the
// generic pack or unpack handler, which covers all thirteen banks.
//
// Takes destination (*Registers) which is the destination register set.
// Takes source (*Registers) which is the source register set.
// Takes sourceKind (isa.RegisterKind) which selects the source typed bank.
// Takes destinationRegister (int) which is the general bank destination index.
// Takes sourceRegister (uint8) which is the source index in the typed bank.
// Takes arena (*RegisterArena) which receives the scalar boxes; may be nil.
func boxScalarToGeneral(destination, source *Registers, sourceKind isa.RegisterKind, destinationRegister int, sourceRegister uint8, arena *RegisterArena) {
	switch sourceKind {
	case isa.RegisterInt:
		destination.General[destinationRegister] = boxInt64ToGeneral(arena, source.Ints[sourceRegister])
	case isa.RegisterFloat:
		destination.General[destinationRegister] = boxFloat64ToGeneral(arena, source.Floats[sourceRegister])
	case isa.RegisterString:
		destination.General[destinationRegister] = BoxStringToGeneral(arena, source.Strings[sourceRegister])
	case isa.RegisterBool:
		destination.General[destinationRegister] = boxBoolToGeneral(source.Bools[sourceRegister])
	case isa.RegisterUint:
		destination.General[destinationRegister] = boxUint64ToGeneral(arena, source.Uints[sourceRegister])
	case isa.RegisterComplex:
		destination.General[destinationRegister] = boxComplex128ToGeneral(arena, source.Complex[sourceRegister])
	case isa.RegisterSliceInt:
		destination.General[destinationRegister] = reflect.ValueOf(source.SlicesInt[sourceRegister])
	case isa.RegisterSliceFloat:
		destination.General[destinationRegister] = reflect.ValueOf(source.slicesFloat[sourceRegister])
	case isa.RegisterSliceString:
		destination.General[destinationRegister] = reflect.ValueOf(source.slicesString[sourceRegister])
	case isa.RegisterSliceBool:
		destination.General[destinationRegister] = reflect.ValueOf(source.slicesBool[sourceRegister])
	case isa.RegisterSliceUint:
		destination.General[destinationRegister] = reflect.ValueOf(source.slicesUint[sourceRegister])
	case isa.RegisterSliceByte:
		destination.General[destinationRegister] = reflect.ValueOf(source.slicesByte[sourceRegister])
	default:
	}
}

// unboxGeneralToScalar extracts a concrete value from a reflect.Value and stores it in
// the appropriate typed register bank.
//
// For typed-slice destinations the fast path is a same-storage-type assertion ([]int64 /
// []uint64 / []float64 / []string / []bool / []byte). When the assertion fails (typically
// because the caller's slice was declared at a narrower integer width such as []int32 and
// the typed-bank storage uses the 64-bit sibling) the fallback widens each element
// through reflect into a freshly-allocated 64-bit-backed slice. The allocation is
// unavoidable: the user's narrower slice and the typed-bank's 64-bit slice have different
// element strides, so the backing arrays cannot be aliased. Only the banks with a
// dedicated sub-op are listed. The default routes through the generic pack or unpack
// handler, which covers all thirteen banks.
//
// Takes destination (*Registers) which is the destination register set.
// Takes value (reflect.Value) which is the value to unbox.
// Takes destinationKind (isa.RegisterKind) which selects the target typed bank.
// Takes destinationRegister (int) which is the destination index within that bank.
// Takes arena (*RegisterArena) which receives bump-allocated narrow-int widening
// backings; may be nil for test entry without an active VM.
func unboxGeneralToScalar(destination *Registers, value reflect.Value, destinationKind isa.RegisterKind, destinationRegister int, arena *RegisterArena) {
	switch destinationKind {
	case isa.RegisterInt:
		destination.Ints[destinationRegister] = value.Int()
	case isa.RegisterFloat:
		destination.Floats[destinationRegister] = value.Float()
	case isa.RegisterString:
		destination.Strings[destinationRegister] = value.String()
	case isa.RegisterBool:
		destination.Bools[destinationRegister] = value.Bool()
	case isa.RegisterUint:
		destination.Uints[destinationRegister] = value.Uint()
	case isa.RegisterComplex:
		destination.Complex[destinationRegister] = value.Complex()
	case isa.RegisterSliceInt:
		destination.SlicesInt[destinationRegister] = unboxToTypedIntSlice(value, arena)
	case isa.RegisterSliceFloat:
		if slice, ok := reflect.TypeAssert[[]float64](value); ok {
			destination.slicesFloat[destinationRegister] = slice
		}
	case isa.RegisterSliceString:
		if slice, ok := reflect.TypeAssert[[]string](value); ok {
			destination.slicesString[destinationRegister] = slice
		}
	case isa.RegisterSliceBool:
		if slice, ok := reflect.TypeAssert[[]bool](value); ok {
			destination.slicesBool[destinationRegister] = slice
		}
	case isa.RegisterSliceUint:
		destination.slicesUint[destinationRegister] = unboxToTypedUintSlice(value, arena)
	case isa.RegisterSliceByte:
		if slice, ok := reflect.TypeAssert[[]byte](value); ok {
			destination.slicesByte[destinationRegister] = slice
		}
	default:
	}
}

// matchesNarrowIntKind reports whether elemKind is a narrower signed- integer slice
// element that should widen into int64-backed storage.
//
// Takes elemKind (reflect.Kind) which is the slice element kind.
//
// Returns bool which is true when widening is required.
func matchesNarrowIntKind(elemKind reflect.Kind) bool {
	return elemKind == reflect.Int || elemKind == reflect.Int8 ||
		elemKind == reflect.Int16 || elemKind == reflect.Int32
}

// matchesNarrowUintKind reports whether elemKind is a narrower unsigned-integer slice
// element that should widen into uint64-backed storage.
//
// Takes elemKind (reflect.Kind) which is the slice element kind.
//
// Returns bool which is true when widening is required.
func matchesNarrowUintKind(elemKind reflect.Kind) bool {
	return elemKind == reflect.Uint || elemKind == reflect.Uint16 ||
		elemKind == reflect.Uint32 || elemKind == reflect.Uintptr
}

// widenIntSliceWithArena widens a narrow-int slice into []int64.
//
// Takes value (reflect.Value) which is the source slice.
// Takes arena (*RegisterArena) which provides the bump-allocator; may be nil for test
// entry.
//
// Returns the widened []int64 with one entry per source element; nil when the source
// slice is nil so the callee still sees a nil-comparing value.
func widenIntSliceWithArena(value reflect.Value, arena *RegisterArena) []int64 {
	if value.IsNil() {
		return nil
	}
	length := value.Len()
	var target []int64
	if arena != nil {
		target = arena.AllocIntBacking(length)
	}
	if target == nil {
		target = make([]int64, length)
	}
	for i := range length {
		target[i] = value.Index(i).Int()
	}
	return target
}

// widenUintSliceWithArena widens a narrow-uint slice into []uint64.
//
// Unsigned-int sibling of widenIntSliceWithArena. Uses arena.AllocUintBacking when arena
// is non-nil. See widenIntSliceWithArena for the aliasing rationale.
//
// Takes value (reflect.Value) which is the source slice.
// Takes arena (*RegisterArena) which provides the bump-allocator; may be nil for test
// entry.
//
// Returns the widened []uint64 with one entry per source element; nil when the source
// slice is nil so the callee still sees a nil-comparing value.
func widenUintSliceWithArena(value reflect.Value, arena *RegisterArena) []uint64 {
	if value.IsNil() {
		return nil
	}
	length := value.Len()
	var target []uint64
	if arena != nil {
		target = arena.AllocUintBacking(length)
	}
	if target == nil {
		target = make([]uint64, length)
	}
	for i := range length {
		target[i] = value.Index(i).Uint()
	}
	return target
}

// unboxToTypedIntSlice converts a signed-int slice into []int64.
//
// Converts a reflect.Value holding a signed integer slice (any width: int / int8 / int16
// / int32 / int64) into the int64-backed storage shared by isa.RegisterSliceInt. When the
// source is exactly []int64 the slice header is returned without copying; for narrower
// widths the elements are widened element-by-element via reflect.Value.Int(). The
// widening cost is the price paid for preserving the caller's declared element width
// across the call boundary; signed-int sign-extension is handled by reflect.
//
// Takes value (reflect.Value) which is the source slice value.
// Takes arena (*RegisterArena) which receives bump-allocated widened backings; may be nil
// for test/library entry without an active VM.
//
// Returns the int64-backed slice for the typed-slice bank, or nil when the source is not
// a recognised signed-int slice.
//
//nolint:dupl // hot-path twin
func unboxToTypedIntSlice(value reflect.Value, arena *RegisterArena) []int64 {
	if slice, ok := reflect.TypeAssert[[]int64](value); ok {
		return slice
	}
	if value.Kind() != reflect.Slice {
		return nil
	}
	elemKind := value.Type().Elem().Kind()

	if elemKind == reflect.Int {
		if slice, ok := reflect.TypeAssert[[]int](value); ok {
			return *(*[]int64)(unsafe.Pointer(&slice))
		}
	}
	if !matchesNarrowIntKind(elemKind) {
		return nil
	}
	return widenIntSliceWithArena(value, arena)
}

// unboxToTypedUintSlice is the unsigned-int companion of unboxToTypedIntSlice. Widens
// []uint / []uint16 / []uint32 elements into the uint64 storage shared by
// isa.RegisterSliceUint without re-aliasing the backing array.
//
// Takes value (reflect.Value) which is the source slice value.
// Takes arena (*RegisterArena) which receives bump-allocated widened backings; may be nil
// for test/library entry without an active VM.
//
// Returns the uint64-backed slice for the typed-slice bank, or nil when the source is not
// a recognised unsigned-int slice.
//
//nolint:dupl // hot-path twin
func unboxToTypedUintSlice(value reflect.Value, arena *RegisterArena) []uint64 {
	if slice, ok := reflect.TypeAssert[[]uint64](value); ok {
		return slice
	}
	if value.Kind() != reflect.Slice {
		return nil
	}
	elemKind := value.Type().Elem().Kind()

	if elemKind == reflect.Uint {
		if slice, ok := reflect.TypeAssert[[]uint](value); ok {
			return *(*[]uint64)(unsafe.Pointer(&slice))
		}
	}
	if !matchesNarrowUintKind(elemKind) {
		return nil
	}
	return widenUintSliceWithArena(value, arena)
}

// handleCall dispatches a compiled function call or closure invocation by pushing a new
// frame onto the call stack and copying arguments.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
//
//nolint:revive // hot path
func handleCall(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	var callee *program.CompiledFunction
	var closureCells []*program.UpvalueCell
	var closureRoot *program.CompiledFunction
	if !site.IsClosure {
		if site.CachedCallee != nil {
			callee = site.CachedCallee
			if site.ArgCopyProgram == nil && !vm.hasGoroutines {
				site.ArgCopyProgram = program.BuildCallArgCopyProgram(site.Arguments, callee.ParameterKinds, callee.ParameterRegisters)
			}
		} else {
			if int(site.FunctionIndex) >= len(vm.functions) {
				vMBoundsError(vm, frame, boundsTableFunction, int(site.FunctionIndex), len(vm.functions))
				return opPanicError
			}
			callee = vm.functions[site.FunctionIndex]
			if site.ArgCopyProgram == nil && !vm.hasGoroutines {
				site.ArgCopyProgram = program.BuildCallArgCopyProgram(site.Arguments, callee.ParameterKinds, callee.ParameterRegisters)
			}
		}
	} else {
		value := registers.General[site.ClosureRegister]
		if !value.IsValid() || (value.Kind() == reflect.Func && value.IsNil()) {
			return raiseNativePanicAsInterpreted(vm, nilDereferenceMessage)
		}
		rv := (*unsafeReflectValue)(unsafe.Pointer(&value))
		var closurePointer unsafe.Pointer
		if rv.flag&flagIndir != 0 {
			closurePointer = *(*unsafe.Pointer)(rv.ptr)
		} else {
			closurePointer = rv.ptr
		}
		if closurePointer != nil && !vm.hasGoroutines && closurePointer == site.CachedClosurePtr {
			callee = site.CachedClosureCallee
			closureCells = site.CachedClosureUpvalues
			closureRoot = site.CachedClosureRoot

			vm.repointClosureACIBase(frame.Function)
		} else {
			closure, ok := reflect.TypeAssert[*RuntimeClosure](value)
			if !ok {
				return handleCallNativeReflect(vm, registers, site, value)
			}
			callee = closure.Function
			closureCells = closure.upvalues
			closureRoot = closure.RootFunction

			if !vm.hasGoroutines {
				site.CachedClosurePtr = closurePointer
				site.CachedClosureCallee = callee
				site.CachedClosureUpvalues = closureCells
				site.CachedClosureRoot = closureRoot
				vm.tryPopulateClosureASMInfo(frame.Function, siteIndex, site, callee, closurePointer, closureCells, closureRoot)
			}
			if site.ArgCopyProgram == nil && !vm.hasGoroutines {
				site.ArgCopyProgram = program.BuildCallArgCopyProgram(site.Arguments, callee.ParameterKinds, callee.ParameterRegisters)
			}
		}
	}
	if !site.IsClosure && callee.TinyLeafShape != program.TinyLeafNone && tinyLeafSiteCompatible(site, callee) {
		return runTinyLeafInline(vm, registers, site, callee)
	}
	if vm.FramePointer >= vm.callDepthLimit() {
		return opStackOverflow
	}
	var snapshot *frameRootSnapshot
	if site.IsClosure {
		snapshot = vm.swapToClosureRoot(closureRoot)
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
	vm.recordFrameSnapshot(vm.FramePointer, snapshot)
	if closureCells != nil {
		f.initialiseUpvalues(closureCells, vm.Arena)
	}

	if site.IsClosure && callee.IsVariadic && !site.IsEllipsisSpread && site.RuntimeVariadicSliceType == nil && len(callee.ParameterKinds) > 0 && len(site.Arguments) >= len(callee.ParameterKinds)-1 {
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

// handleCallScalar dispatches a scalar-only interpreted call. Elides the closure,
// variadic and general-bank branches that handleCall carries.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
func handleCallScalar(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	var callee *program.CompiledFunction
	if site.CachedCallee != nil {
		callee = site.CachedCallee
	} else {
		if int(site.FunctionIndex) >= len(vm.functions) {
			vMBoundsError(vm, frame, boundsTableFunction, int(site.FunctionIndex), len(vm.functions))
			return opPanicError
		}
		callee = vm.functions[site.FunctionIndex]
		if site.ArgCopyProgram == nil && !vm.hasGoroutines {
			site.ArgCopyProgram = program.BuildCallArgCopyProgram(site.Arguments, callee.ParameterKinds, callee.ParameterRegisters)
		}
	}
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
	f.hasGeneralAlloc = false
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
	vm.recordFrameSnapshot(vm.FramePointer, nil)
	copyCallArgs(vm, vm.Arena, registers, f, site, callee)
	return opFrameChanged
}

// resolveDirectCallee returns the callee for a direct (non-closure) call site, mirroring
// the inline lookup inside handleCall. Kept as a helper because the tail-call path
// consumes the (callee, result) pair without needing the rest of the dispatch sequence.
//
// Takes vm (*VM) which is the executing VM.
// Takes site (*CallSite) which describes the callee to resolve.
//
// Returns the resolved callee function (or nil on failure) and the dispatch result
// (opContinue on success).
func resolveDirectCallee(vm *VM, site *program.CallSite) (*program.CompiledFunction, OpResult) {
	if site.CachedCallee != nil {
		return site.CachedCallee, opContinue
	}
	if int(site.FunctionIndex) >= len(vm.functions) {
		frame := &vm.CallStack[vm.FramePointer]
		vMBoundsError(vm, frame, boundsTableFunction, int(site.FunctionIndex), len(vm.functions))
		return nil, opPanicError
	}
	return vm.functions[site.FunctionIndex], opContinue
}

// handleCallIIFE handles an immediately-invoked function expression by pushing a new
// frame with upvalue cells snapshotted from the caller's registers.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
func handleCallIIFE(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	var callee *program.CompiledFunction
	if site.CachedCallee != nil {
		callee = site.CachedCallee
	} else {
		if int(site.FunctionIndex) >= len(vm.functions) {
			vMBoundsError(vm, frame, boundsTableFunction, int(site.FunctionIndex), len(vm.functions))
			return opPanicError
		}
		callee = vm.functions[site.FunctionIndex]
	}
	if vm.FramePointer >= vm.callDepthLimit() {
		return opStackOverflow
	}
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	f := &vm.CallStack[vm.FramePointer]
	descriptors := callee.UpvalueDescriptors
	n := len(descriptors)

	cellBatch := make([]program.UpvalueCell, n)
	var upvals []upvalue
	if vm.Arena != nil {
		vm.Arena.SaveInto(&f.arenaSave)
		upvals = vm.Arena.allocUpvalueRefs(n)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&f.Registers, callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
	} else {
		upvals = make([]upvalue, n)
		f.Registers = NewRegisters(callee.NumRegisters)
	}
	initialiseIIFEUpvalues(upvals, cellBatch, descriptors, registers, frame, vm.Arena)
	f.Function = callee
	f.ProgramCounter = 0
	f.returnDestination = site.Returns
	f.deferBase = len(vm.deferStack)
	if f.simpleDefer != nil {
		f.simpleDefer.active = false
	}
	f.upvalues = upvals
	f.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
	vm.recordFrameSnapshot(vm.FramePointer, nil)
	copyCallArgs(vm, vm.Arena, registers, f, site, callee)
	return opFrameChanged
}

// initialiseIIFEUpvalues populates the upvalue cells and references for an IIFE call,
// either inheriting from the parent frame or snapshotting register values into freshly
// allocated cells.
//
// Takes upvals ([]upvalue) which receives upvalue references for the frame.
// Takes cellBatch ([]UpvalueCell) which provides pre-allocated cells.
// Takes descriptors ([]UpvalueDescriptor) which describes each upvalue's source.
// Takes registers (*Registers) which holds the caller's current values.
// Takes frame (*CallFrame) which is the parent frame for non-local upvalues.
// Takes arena (*RegisterArena) which is the allocation arena.
func initialiseIIFEUpvalues(upvals []upvalue, cellBatch []program.UpvalueCell, descriptors []program.UpvalueDescriptor, registers *Registers, frame *CallFrame, arena *RegisterArena) {
	for i := range len(descriptors) {
		descriptor := descriptors[i]
		if !descriptor.IsLocal && frame.upvalues != nil {
			upvals[i].Value = frame.upvalues[descriptor.Index].Value
			continue
		}
		cellBatch[i].Kind = descriptor.Kind
		if descriptor.IsIndirect {
			cellBatch[i].IsIndirect = true
			cellBatch[i].OriginalKind = descriptor.OriginalKind
			cellBatch[i].GeneralValue = registers.General[descriptor.Index]
			cacheIndirectTarget(&cellBatch[i])
		} else {
			snapshotRegisterToCell(&cellBatch[i], registers, descriptor.Kind, descriptor.Index, arena)
		}
		upvals[i].Value = &cellBatch[i]
	}
}

// snapshotRegisterToCell copies the current register value into an upvalue cell. Used
// when creating closure captures for IIFE calls.
//
// Cells capture these seven banks directly; the typed-slice payloads are captured by the
// dedicated slice path in WriteRegisterToCell rather than here.
//
// Takes cell (*UpvalueCell) which is the destination upvalue cell.
// Takes registers (*Registers) which holds the source values.
// Takes kind (isa.RegisterKind) which selects the typed register bank.
// Takes index (uint8) which is the register index within that bank.
// Takes arena (*RegisterArena) which is the allocation arena.
func snapshotRegisterToCell(cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, index uint8, arena *RegisterArena) {
	switch kind {
	case isa.RegisterInt:
		cell.IntValue = registers.Ints[index]
	case isa.RegisterFloat:
		cell.FloatValue = registers.Floats[index]
	case isa.RegisterString:

		value := registers.Strings[index]
		if arena != nil {
			value = materialiseStringUnconditional(arena, value)
		}
		cell.StringValue = value
	case isa.RegisterGeneral:
		general := registers.General[index]
		if arena != nil {
			general = materialiseArenaValueUnconditional(arena, general)
		}
		cell.GeneralValue = general
	case isa.RegisterBool:
		cell.BoolValue = registers.Bools[index]
	case isa.RegisterUint:
		cell.UintValue = registers.Uints[index]
	case isa.RegisterComplex:
		cell.ComplexValue = registers.Complex[index]
	default:
	}
}

// handleReturn processes a function return by running deferred calls, copying return
// values to the caller's registers, and popping the frame. A deferred closure that cannot
// recover is pushed as an ordinary frame and the return re-executes after it, so deferred
// bodies do not nest a dispatch loop.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the returning call frame.
// Takes instruction (instruction) which encodes the return value count.
//
// Returns OpResult indicating whether execution is done or continuing.
func handleReturn(vm *VM, frame *CallFrame, _ *Registers, instruction isa.Instruction) OpResult {
	returnCount := int(instruction.A)
	if vm.startInlineDefer(frame) {
		return opFrameChanged
	}
	if result, interrupted := vm.runReturnDefers(frame); interrupted {
		return result
	}
	vm.syncNamedResults(frame)
	if vm.FramePointer == vm.baseFramePointer {
		if vm.inlineDispatchExpectUintResult && len(frame.Function.ResultKinds) > 0 && frame.Function.ResultKinds[0] == isa.RegisterUint {
			vm.inlineDispatchUintResult = frame.Registers.Uints[0]
		} else {
			vm.evalResult, _ = vm.extractResult(frame)
			vm.EvalAllResults = vm.extractAllResults(frame)
		}
		vm.popFrame()
		return opDone
	}
	returnDestination := frame.returnDestination
	var bankCounters [isa.NumRegisterKinds]uint8
	for i := 0; i < returnCount && i < len(returnDestination); i++ {
		dest := returnDestination[i]
		kind := frame.Function.ResultKinds[i]
		sourceRegister := bankCounters[kind]
		bankCounters[kind]++
		vm.copyReturnValueAt(frame, kind, sourceRegister, dest)
	}
	vm.popFrame()
	return opFrameChanged
}

// handleReturnVoid processes a void function return by running deferred calls and popping
// the frame without copying any return values.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the returning call frame.
//
// Returns OpResult indicating whether execution is done or continuing.
func handleReturnVoid(vm *VM, frame *CallFrame, _ *Registers, _ isa.Instruction) OpResult {
	if vm.startInlineDefer(frame) {
		return opFrameChanged
	}
	if result, interrupted := vm.runReturnDefers(frame); interrupted {
		return result
	}
	vm.syncNamedResults(frame)
	vm.popFrame()
	if vm.FramePointer < vm.baseFramePointer {
		return opDone
	}
	return opFrameChanged
}
