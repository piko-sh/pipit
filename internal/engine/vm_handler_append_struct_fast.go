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
	"unsafe"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// structAppendResult describes the slice produced by a struct-fast append.
type structAppendResult struct {
	// data is the backing's first element.
	data unsafe.Pointer

	// length is the result length.
	length int

	// capacity is the result capacity.
	capacity int
}

// publishStructAppend installs the appended slice's header: in place on the source header
// at an alias-free same-register site, or through a fresh arena header otherwise.
//
// Takes frame (*CallFrame) which identifies the executing function.
// Takes registers (*Registers) which holds the general bank.
// Takes instruction (instruction) which encodes the operand indices.
// Takes sitePC (int) which is the PC of the append instruction word.
// Takes header (*arenaSliceHeader) which is the source header.
// Takes result (structAppendResult) which describes the appended slice.
//
// Returns opContinue.
func (vm *VM) publishStructAppend(frame *CallFrame, registers *Registers, instruction isa.Instruction, sitePC int, header *arenaSliceHeader, result structAppendResult) OpResult {
	if instruction.A == instruction.B && frame.Function.InPlaceHeaderReusePCs[sitePC] {
		header.Data = result.data
		header.Len = result.length
		header.Cap = result.capacity
		return opContinue
	}
	slot := vm.Arena.allocSliceHeader()
	slot.Data = result.data
	slot.Len = result.length
	slot.Cap = result.capacity
	sliceType := registers.General[instruction.B].Type()
	registers.General[instruction.A] = unsafeNewAt(reflectValueABIType(sliceType), unsafe.Pointer(slot), reflect.Slice)
	return opContinue
}

// handleAppendStructFast appends a pointer-free struct or array element to a general-bank
// slice with raw header arithmetic.
//
// Encoding: A=dest general, B=source general, C=element general, followed by an extension
// word whose wide field indexes the element type in the type table. When the source
// header is arena-owned and the element matches the recorded type, the element bytes are
// copied into spare capacity, or into a doubled arena backing when the slice is full and
// arena-born. The result header is reused in place at sites the escape pass proved
// alias-free and allocated fresh otherwise. Heap-backed slices, mismatched elements and
// every safe-build case fall back to handleAppend.
//
// Takes vm (*VM) which provides the arena and allocation limits.
// Takes frame (*CallFrame) whose programCounter is advanced past the extension word.
// Takes registers (*Registers) which holds the general bank.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAppendStructFast(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sitePC := frame.ProgramCounter - 1
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	sliceValue := registers.General[instruction.B]
	element := registers.General[instruction.C]
	typeIndex := int(extensionWord.WideIndex())
	header := arenaOwnedSliceHeader(vm.Arena, sliceValue)
	if header == nil || typeIndex >= len(frame.Function.TypeTable) || !element.IsValid() || !element.CanAddr() {
		return handleAppend(vm, frame, registers, instruction)
	}
	elementType := frame.Function.TypeTable[typeIndex]
	if element.Type() != elementType {
		return handleAppend(vm, frame, registers, instruction)
	}
	if rc := checkAppendLimit(vm, sliceValue); rc != opContinue {
		return rc
	}
	size := elementType.Size()
	if header.Len < header.Cap {
		copyStructElementInto(header.Data, header.Len, size, element)
		result := structAppendResult{data: header.Data, length: header.Len + 1, capacity: header.Cap}
		return vm.publishStructAppend(frame, registers, instruction, sitePC, header, result)
	}
	backing, newCap, ok := growStructBackingInArena(vm, header, size, uintptr(elementType.Align()))
	if !ok {
		return handleAppend(vm, frame, registers, instruction)
	}
	copyStructElementInto(backing, header.Len, size, element)
	result := structAppendResult{data: backing, length: header.Len + 1, capacity: newCap}
	return vm.publishStructAppend(frame, registers, instruction, sitePC, header, result)
}

// copyStructElementInto copies a pointer-free element's bytes into slot index of a
// backing whose elements are size bytes each.
//
// Takes data (unsafe.Pointer) which is the backing's first element.
// Takes index (int) which is the destination element index.
// Takes size (uintptr) which is the element byte size.
// Takes element (reflect.Value) which is the addressable source element.
func copyStructElementInto(data unsafe.Pointer, index int, size uintptr, element reflect.Value) {
	destination := unsafe.Slice((*byte)(unsafe.Add(data, size*safeconv.IntToUintptr(index))), size)
	source := unsafe.Slice((*byte)(ReflectValuePtr(element)), size)
	copy(destination, source)
}
