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
	"unsafe"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// handleAppendUint64WideInPlace is the in-place sibling of handleAppendUint64Wide for the
// uint-width slice types. Falls back to handleAppendUint64Wide on non-arena slots, on
// []byte, or on non-uint-width slice kinds.
//
// Takes vm (*VM) which owns the executing program state.
// Takes frame (*CallFrame) which carries the extension-word program counter.
// Takes registers (*Registers) which holds the active register banks.
// Takes instr (instruction) which carries operands B (destination/source) and C (source).
//
// Returns OpResult which signals continuation or panic.
func handleAppendUint64WideInPlace(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	sliceValue := registers.General[instr.C]
	element := registers.Uints[extensionWord.A]

	if !sliceValue.IsValid() {
		return handleAppendUint64WideFallbackInvalid(registers, instr, element)
	}
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}

	sourceShape := (*unsafeReflectValue)(unsafe.Pointer(&sliceValue))
	headerPtr := (*arenaSliceHeader)(sourceShape.ptr)
	if headerPtr == nil || !vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(headerPtr)) {
		return handleAppendUint64WideGrowFallback(registers, instr, sliceValue, element)
	}

	if rc, ok := appendUint64WideInPlaceTypedDispatch(vm, sliceValue, headerPtr, element); ok {
		return rc
	}

	return handleAppendUint64WideGrowFallback(registers, instr, sliceValue, element)
}

// appendUint64WideInPlaceTypedDispatch handles each concrete uint-width slice shape
// recognised by the in-place append fast path.
//
// Takes vm (*VM) which provides the arena.
// Takes sliceValue (reflect.Value) which holds the source slice.
// Takes headerPtr (*arenaSliceHeader) which is the arena slot to update.
// Takes element (uint64) which is the value to append.
//
// Returns OpResult which propagates continuation or panic.
// Returns bool which is true when the slice matched a known shape, false when the caller
// must fall back to the reflect-driven grow path.
func appendUint64WideInPlaceTypedDispatch(vm *VM, sliceValue reflect.Value, headerPtr *arenaSliceHeader, element uint64) (OpResult, bool) {
	if s, ok := reflect.TypeAssert[[]byte](sliceValue); ok {
		grown := arenaAppendByte(vm.Arena, s, safeconv.Uint64ToUint8(element))
		headerPtr.Data = unsafe.Pointer(unsafe.SliceData(grown))
		headerPtr.Len = len(grown)
		headerPtr.Cap = cap(grown)
		return opContinue, true
	}
	if s, ok := reflect.TypeAssert[[]uint64](sliceValue); ok {
		grown := arenaAppendUint64(vm.Arena, s, element)
		headerPtr.Data = sliceDataPtrUint64(grown)
		headerPtr.Len = len(grown)
		headerPtr.Cap = cap(grown)
		return opContinue, true
	}
	if s, ok := reflect.TypeAssert[[]uint](sliceValue); ok {
		s = append(s, uint(element))
		headerPtr.Data = sliceDataPtrUint(s)
		headerPtr.Len = len(s)
		headerPtr.Cap = cap(s)
		return opContinue, true
	}
	if s, ok := reflect.TypeAssert[[]uint32](sliceValue); ok {
		s = append(s, safeconv.Uint64ToUint32(element))
		headerPtr.Data = sliceDataPtrUint32(s)
		headerPtr.Len = len(s)
		headerPtr.Cap = cap(s)
		return opContinue, true
	}
	if s, ok := reflect.TypeAssert[[]uint16](sliceValue); ok {
		s = append(s, safeconv.Uint64ToUint16(element))
		headerPtr.Data = sliceDataPtrUint16(s)
		headerPtr.Len = len(s)
		headerPtr.Cap = cap(s)
		return opContinue, true
	}
	if s, ok := reflect.TypeAssert[[]uintptr](sliceValue); ok {
		s = append(s, uintptr(element))
		headerPtr.Data = sliceDataPtrUintptr(s)
		headerPtr.Len = len(s)
		headerPtr.Cap = cap(s)
		return opContinue, true
	}
	return opContinue, false
}

// handleAppendUint64WideFallbackInvalid handles the nil-source case for the in-place uint
// append, matching the original handleAppendUint64Wide behaviour, which produces a
// single-element []byte slice in the destination register.
//
// Takes registers (*Registers) which holds the destination general bank.
// Takes instr (instruction) whose B operand selects the destination.
// Takes element (uint64) which is the single byte value to write.
//
// Returns OpResult indicating the next execution step.
func handleAppendUint64WideFallbackInvalid(registers *Registers, instr isa.Instruction, element uint64) OpResult {
	registers.General[instr.B] = reflect.ValueOf([]byte{safeconv.Uint64ToUint8(element)})
	return opContinue
}

// handleAppendUint64WideGrowFallback runs the reflect.Append slow path for cases the
// in-place fast paths above didn't cover. Writes the resulting reflect.Value to the
// destination register (which equals the source register).
//
// Takes registers (*Registers) which holds the destination general bank.
// Takes instr (instruction) whose B operand selects the destination.
// Takes sliceValue (reflect.Value) which is the source slice.
// Takes element (uint64) which is the value to append.
//
// Returns OpResult indicating the next execution step.
func handleAppendUint64WideGrowFallback(registers *Registers, instr isa.Instruction, sliceValue reflect.Value, element uint64) OpResult {
	registers.General[instr.B] = reflect.Append(sliceValue, reflect.ValueOf(element).Convert(sliceValue.Type().Elem()))
	return opContinue
}

// handleAppendSpreadByteInPlace is the unified in-place byte-spread append. Handles both
// the slice form and the pointer form, mirroring handleAppendByteInPlace.
//
// Takes vm (*VM) which owns the executing program state.
// Takes frame (*CallFrame) which is forwarded to the fallback path.
// Takes registers (*Registers) which holds the destination and source banks.
// Takes instr (instruction) which carries B (destination) and C (source).
//
// Returns OpResult which signals continuation or panic.
func handleAppendSpreadByteInPlace(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	destinationValue := registers.General[instr.B]
	sourceValue := registers.General[instr.C]
	if !destinationValue.IsValid() || !sourceValue.IsValid() {
		return handleAppendSpreadByteInPlaceFallback(vm, frame, registers, instr)
	}
	sourceSlice, ok := reflect.TypeAssert[[]byte](sourceValue)
	if !ok {
		return handleAppendSpreadByteInPlaceFallback(vm, frame, registers, instr)
	}

	switch destinationValue.Kind() {
	case reflect.Slice:
		return appendSpreadByteInPlaceSlice(vm, frame, registers, instr, destinationValue, sourceSlice)
	case reflect.Pointer:
		if destinationValue.IsNil() {
			return handleAppendSpreadByteInPlaceFallback(vm, frame, registers, instr)
		}
		return appendSpreadByteInPlacePointer(vm, registers, instr, destinationValue, sourceSlice)
	default:
		return handleAppendSpreadByteInPlaceFallback(vm, frame, registers, instr)
	}
}

// appendSpreadByteInPlaceSlice handles the slice byte-spread in-place op.
//
// Covers the x = append(x, source...) shape. When the destination's slot is not
// arena-owned, falls back to an inline spread that allocates a fresh arenaSliceHeader
// (the standard arenaWrapByteSpreadResult path) - we never call back into the tier-0
// handleAppendSpread because its operand encoding includes an extension-word read that
// would corrupt dispatch when invoked from a tier-1 sub-op site.
//
// Takes vm (*VM) which provides the arena and allocation limits.
// Takes registers (*Registers) which holds the destination general bank.
// Takes instr (instruction) whose B operand selects the destination slot.
// Takes destinationValue (reflect.Value) which is the destination slice.
// Takes sourceSlice ([]byte) which holds the bytes to append.
//
// Returns OpResult which signals continuation or panic.
func appendSpreadByteInPlaceSlice(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction, destinationValue reflect.Value, sourceSlice []byte) OpResult {
	destinationSlice, sliceOk := reflect.TypeAssert[[]byte](destinationValue)
	if !sliceOk {
		return opContinue
	}
	if vm.Limits.MaxAllocSize > 0 && len(destinationSlice)+len(sourceSlice) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(destinationSlice)+len(sourceSlice), vm.Limits.MaxAllocSize)
		return opPanicError
	}
	destinationShape := (*unsafeReflectValue)(unsafe.Pointer(&destinationValue))
	headerPtr := (*arenaSliceHeader)(destinationShape.ptr)
	if headerPtr == nil || !vm.Arena.OwnsSliceHeaderPointer(unsafe.Pointer(headerPtr)) {
		result := arenaAppendByteSpread(vm.Arena, destinationSlice, sourceSlice)
		registers.General[instr.B] = arenaWrapByteSpreadResult(vm.Arena, destinationValue, result)
		return opContinue
	}
	result := arenaAppendByteSpread(vm.Arena, destinationSlice, sourceSlice)
	headerPtr.Data = unsafe.Pointer(unsafe.SliceData(result))
	headerPtr.Len = len(result)
	headerPtr.Cap = cap(result)
	return opContinue
}

// appendSpreadByteInPlacePointer handles the *p = append(*p, source...) shape for the
// byte-spread in-place opcode.
//
// Takes vm (*VM) which provides the arena and allocation limits.
// Takes registers (*Registers) which is unused after the early return.
// Takes pointerValue (reflect.Value) which wraps the destination pointer.
// Takes sourceSlice ([]byte) which holds the bytes to append.
//
// Returns OpResult which signals continuation or panic.
func appendSpreadByteInPlacePointer(vm *VM, registers *Registers, _ isa.Instruction, pointerValue reflect.Value, sourceSlice []byte) OpResult {
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&pointerValue))
	var headerPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		headerPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		headerPtr = recvShape.ptr
	}
	if headerPtr == nil {
		_ = registers
		return opContinue
	}

	current := *(*[]byte)(headerPtr)
	if vm.Limits.MaxAllocSize > 0 && len(current)+len(sourceSlice) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(current)+len(sourceSlice), vm.Limits.MaxAllocSize)
		return opPanicError
	}
	extended := appendSpreadByteViaPointer(vm.Arena, headerPtr, current, sourceSlice)
	if !useMapFastLinkname() {
		*(*[]byte)(headerPtr) = extended
		return opContinue
	}
	elemType := pointerValue.Type().Elem()
	runtimeTypedmemmove(reflectValueABIType(elemType), headerPtr, unsafe.Pointer(&extended))
	return opContinue
}

// handleAppendSpreadByteInPlaceFallback handles spread-append edge cases.
//
// Covers invalid operands, non-[]byte source, and non-arena slots. The Compiler emits
// isa.SubOpAppendSpreadByteInPlace only when the static types confirm []byte on both
// sides AND the safety predicate clears the destination from aliasing. The fallback
// exists for defensive in-place demotion cases. Inlines the spread logic via
// arenaAppendByteSpread + reflect.Value.SetBytes to avoid disturbing the dispatch state
// that handleAppendSpread (a tier-0 handler with a different operand encoding and
// extension-word expectation) would modify.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns OpResult indicating the next execution step.
func handleAppendSpreadByteInPlaceFallback(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	destinationValue := registers.General[instr.B]
	sourceValue := registers.General[instr.C]
	if !destinationValue.IsValid() || !sourceValue.IsValid() {
		return opContinue
	}
	if destinationValue.Kind() != reflect.Slice {
		return opContinue
	}
	destinationSlice, destOk := reflect.TypeAssert[[]byte](destinationValue)
	sourceSlice, srcOk := reflect.TypeAssert[[]byte](sourceValue)
	if !destOk || !srcOk {
		return opContinue
	}
	if vm.Limits.MaxAllocSize > 0 && len(destinationSlice)+len(sourceSlice) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(destinationSlice)+len(sourceSlice), vm.Limits.MaxAllocSize)
		return opPanicError
	}
	result := arenaAppendByteSpread(vm.Arena, destinationSlice, sourceSlice)
	registers.General[instr.B] = arenaWrapByteSpreadResult(vm.Arena, destinationValue, result)
	return opContinue
}

// appendByteInPlacePointer handles the *p = append(*p, b) shape for
// handleAppendByteInPlace.
//
// Mirrors handleAppendByteViaPointer's machinery. It extracts the pointee header via
// flagIndir, runs arenaAppendByte, and writes back the new []byte header via
// runtimeTypedmemmove (preserves the GC write barrier for the Data pointer field of any
// heap-resident header).
//
// Takes vm (*VM) which provides the arena and allocation limits.
// Takes registers (*Registers) which holds the byte-value uint bank.
// Takes instr (instruction) whose C operand selects the byte register.
// Takes pointerValue (reflect.Value) which wraps the destination pointer.
//
// Returns OpResult indicating the next execution step.
func appendByteInPlacePointer(vm *VM, registers *Registers, instr isa.Instruction, pointerValue reflect.Value) OpResult {
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&pointerValue))

	var headerPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		headerPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		headerPtr = recvShape.ptr
	}
	if headerPtr == nil {
		return opContinue
	}

	current := *(*[]byte)(headerPtr)
	if vm.Limits.MaxAllocSize > 0 && len(current)+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(current)+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}

	extended := starAppendByte(vm.Arena, headerPtr, current, safeconv.Uint64ToUint8(registers.Uints[instr.C]))
	if !useMapFastLinkname() {
		*(*[]byte)(headerPtr) = extended
		return opContinue
	}
	elemType := pointerValue.Type().Elem()
	runtimeTypedmemmove(reflectValueABIType(elemType), headerPtr, unsafe.Pointer(&extended))
	return opContinue
}

// handleAppendByteViaPointer is the fused `*p = append(*p, b)` op.
//
// Takes vm (*VM) which provides arena, evalError, and allocation limits.
// Takes frame (*CallFrame) which is forwarded to the fallback path on invalid operands.
// Takes registers (*Registers) which holds the pointer and byte banks.
// Takes instr (instruction) which encodes the pointer register and byte-value register
// indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendByteViaPointer(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	if !useMapFastLinkname() {
		return handleSubOpStarAppendByteSafe(vm, registers, instr)
	}
	pointerValue := registers.General[instr.B]
	if !pointerValue.IsValid() || pointerValue.Kind() != reflect.Pointer || pointerValue.IsNil() {
		return handleAppendByteViaPointerFallback(vm, frame, registers, instr)
	}
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&pointerValue))

	var headerPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		headerPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		headerPtr = recvShape.ptr
	}
	if headerPtr == nil {
		return handleAppendByteViaPointerFallback(vm, frame, registers, instr)
	}

	current := *(*[]byte)(headerPtr)
	if vm.Limits.MaxAllocSize > 0 && len(current)+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(current)+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	extended := starAppendByte(vm.Arena, headerPtr, current, safeconv.Uint64ToUint8(registers.Uints[instr.C]))
	elemType := pointerValue.Type().Elem()
	runtimeTypedmemmove(reflectValueABIType(elemType), headerPtr, unsafe.Pointer(&extended))
	return opContinue
}

// handleSubOpStarAppendByteSafe implements `*p = append(*p, byte)` via reflect.
//
// Routes here from handleAppendByteViaPointer under safe build where the layout cast and
// runtime.typedmemmove are unavailable. It resolves *p as an addressable reflect.Value of
// kind Slice, appends the supplied byte to *p's []byte view, then sets *p back to the
// extended slice via target.SetBytes so the pointer holder observes the update.
//
// Takes vm (*VM) which carries allocation limits.
// Takes registers (*Registers) which holds the pointer general bank and the byte-value
// uint bank.
// Takes instr (instruction) where b=pointer register, c=byte-value register.
//
// Returns opPanicError when the pointer register is invalid, nil, or holds a non-pointer
// Kind. It returns opContinue on success.
func handleSubOpStarAppendByteSafe(vm *VM, registers *Registers, instr isa.Instruction) OpResult {
	pointerValue := registers.General[instr.B]
	if !pointerValue.IsValid() || pointerValue.Kind() != reflect.Pointer || pointerValue.IsNil() {
		vm.evalError = newInvariantError("handleAppendByteViaPointer: invalid pointer in general[%d]", instr.B)
		return opPanicError
	}
	target := pointerValue.Elem()
	if !target.CanSet() {
		vm.evalError = newInvariantError("handleAppendByteViaPointer: target *p not settable")
		return opPanicError
	}
	current := target.Bytes()
	headerPtr := unsafe.Pointer(target.UnsafeAddr())
	if vm.Limits.MaxAllocSize > 0 && len(current)+1 > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(current)+1, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	extended := starAppendByte(vm.Arena, headerPtr, current, safeconv.Uint64ToUint8(registers.Uints[instr.C]))
	target.SetBytes(extended)
	return opContinue
}

// handleAppendByteViaPointerFallback handles edge cases for the star-append op.
//
// Covers the invalid / nil pointer cases that the Compiler shouldn't be emitting this op
// for, by routing through the regular handler chain.
//
// Takes vm (*VM) which provides the panic-raise path.
// Takes registers (*Registers) which holds the pointer general bank.
// Takes instr (isa.Instruction) which encodes the pointer register index.
//
// Returns OpResult indicating opContinue when the operands pass the defensive checks.
//
// Panics with a diagnostic message when the pointer register holds an invalid value, a
// non-pointer Kind, or a nil pointer.
func handleAppendByteViaPointerFallback(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	pointerValue := registers.General[instr.B]
	if !pointerValue.IsValid() {
		return raiseNilDereference(vm)
	}
	if pointerValue.Kind() != reflect.Pointer {
		panic(newInvariantError(
			"handleAppendByteViaPointer: general[%d] is %v, expected *[]byte",
			instr.B, pointerValue.Kind(),
		))
	}
	if pointerValue.IsNil() {
		panic(newInvariantError(
			"handleAppendByteViaPointer: general[%d] is nil %v",
			instr.B, pointerValue.Type(),
		))
	}
	return opContinue
}

// handleAppendSpreadByteViaPointer is the spread sibling of handleAppendByteViaPointer.
// Implements `*p = append(*p, source...)` where both `*p` and `source` are `[]byte`.
//
// Takes vm (*VM) which provides arena, evalError, and allocation limits.
// Takes frame (*CallFrame) which is forwarded to the fallback path on invalid operands.
// Takes registers (*Registers) which holds the pointer and source-slice banks.
// Takes instr (instruction) which encodes the pointer register and the source slice
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendSpreadByteViaPointer(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	pointerValue := registers.General[instr.B]
	if !pointerValue.IsValid() || pointerValue.Kind() != reflect.Pointer || pointerValue.IsNil() {
		return handleAppendByteViaPointerFallback(vm, frame, registers, instr)
	}
	srcValue := registers.General[instr.C]
	source, ok := reflect.TypeAssert[[]byte](srcValue)
	if !ok {
		return handleAppendByteViaPointerFallback(vm, frame, registers, instr)
	}
	recvShape := (*unsafeReflectValue)(unsafe.Pointer(&pointerValue))
	var headerPtr unsafe.Pointer
	if recvShape.flag&flagIndir != 0 {
		headerPtr = *(*unsafe.Pointer)(recvShape.ptr)
	} else {
		headerPtr = recvShape.ptr
	}
	if headerPtr == nil {
		return handleAppendByteViaPointerFallback(vm, frame, registers, instr)
	}
	current := *(*[]byte)(headerPtr)
	if vm.Limits.MaxAllocSize > 0 && len(current)+len(source) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(appendLimitExceededFormat,
			fault.ErrAllocationLimit, len(current)+len(source), vm.Limits.MaxAllocSize)
		return opPanicError
	}
	extended := appendSpreadByteViaPointer(vm.Arena, headerPtr, current, source)
	if !useMapFastLinkname() {
		*(*[]byte)(headerPtr) = extended
		return opContinue
	}
	elemType := pointerValue.Type().Elem()
	runtimeTypedmemmove(reflectValueABIType(elemType), headerPtr, unsafe.Pointer(&extended))
	return opContinue
}

// starAppendByte is the append step of the `*p = append(*p, b)` forms.
//
// Takes arena (*RegisterArena) which supplies the byte slab; may be nil.
// Takes header (unsafe.Pointer) which is p, the address of the slice header written.
// Takes current ([]byte) which is *p.
// Takes b (byte) which is appended.
//
// Returns []byte which is the extended slice to write to *p.
func starAppendByte(arena *RegisterArena, header unsafe.Pointer, current []byte, b byte) []byte {
	if len(current) < cap(current) || starAppendHomeIsArena(arena, header) {
		return arenaAppendByte(arena, current, b)
	}
	return append(current, b)
}

// appendSpreadByteViaPointer is starAppendByte for `*p = append(*p, source...)`.
//
// Takes arena (*RegisterArena) which supplies the byte slab. It may be nil.
// Takes header (unsafe.Pointer) which is p.
// Takes current ([]byte) which is *p.
// Takes source ([]byte) which is appended.
//
// Returns []byte which is the extended slice to write to *p.
func appendSpreadByteViaPointer(arena *RegisterArena, header unsafe.Pointer, current, source []byte) []byte {
	if len(current)+len(source) <= cap(current) || starAppendHomeIsArena(arena, header) {
		return arenaAppendByteSpread(arena, current, source)
	}
	return append(current, source...)
}

// starAppendHomeIsArena reports whether a star-append's destination header lives in
// storage this arena owns.
//
// The slice-header slab, where `p := &s` puts a local's header, is tested first so the
// common case answers in one range check. It runs only when the slice grows.
//
// Takes arena (*RegisterArena) which may be nil, in which case nothing is arena-owned and
// the answer is true (no barrier applies).
// Takes header (unsafe.Pointer) which is the header's address.
//
// Returns bool which is true when the header is arena storage.
func starAppendHomeIsArena(arena *RegisterArena, header unsafe.Pointer) bool {
	if arena == nil || sliceHeaderSlabOwns(arena.sliceHeaderSlab, uintptr(header)) {
		return true
	}
	return arena.ownsPointee(header)
}
