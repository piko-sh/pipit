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

	"pipit.sh/pipit/internal/isa"
)

// handleSubOpAdoptGeneralToSlicesInt extracts a []int64 from a reflect.Value held in
// general[C] and writes the slice header to slicesInt[B]. Sub-int-width sources widen
// element-by-element via unboxToTypedIntSlice, allocating a fresh int64 backing because
// element strides differ.
//
// Takes vm (*VM) which receives the interpreted panic on type assertion failure.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination slicesInt register B and
// the source general register C.
//
// Returns opContinue on success, opPanicError when the held value is not assignable to
// []int64 (or any narrower signed-int slice).
func handleSubOpAdoptGeneralToSlicesInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.SlicesInt[instruction.B] = nil
		return opContinue
	}
	typed := unboxToTypedIntSlice(value, vm.Arena)
	if typed == nil && value.Len() != 0 {
		vm.evalError = fmt.Errorf("cannot convert %s to []int64", value.Type())
		return opPanicError
	}
	registers.SlicesInt[instruction.B] = typed
	return opContinue
}

// handleSubOpAdoptGeneralToSlicesString mirrors handleSubOpAdoptGeneralToSlicesInt for
// the slicesString bank.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue or opPanicError.
func handleSubOpAdoptGeneralToSlicesString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.slicesString[instruction.B] = nil
		return opContinue
	}
	typed, ok := reflect.TypeAssert[[]string](value)
	if !ok {
		vm.evalError = fmt.Errorf("cannot convert %s to []string", value.Type())
		return opPanicError
	}
	registers.slicesString[instruction.B] = typed
	return opContinue
}

// handleSubOpAdoptGeneralToSlicesBool mirrors handleSubOpAdoptGeneralToSlicesInt for the
// slicesBool bank.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue or opPanicError.
func handleSubOpAdoptGeneralToSlicesBool(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.slicesBool[instruction.B] = nil
		return opContinue
	}
	typed, ok := reflect.TypeAssert[[]bool](value)
	if !ok {
		vm.evalError = fmt.Errorf("cannot convert %s to []bool", value.Type())
		return opPanicError
	}
	registers.slicesBool[instruction.B] = typed
	return opContinue
}

// handleSubOpAdoptGeneralToSlicesUint mirrors handleSubOpAdoptGeneralToSlicesInt for the
// slicesUint bank. Sub-int-width sources ([]uint / []uint16 / []uint32 / []uintptr) widen
// element-by-element via unboxToTypedUintSlice; see that helper for the storage-alignment
// rationale.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue or opPanicError.
func handleSubOpAdoptGeneralToSlicesUint(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.slicesUint[instruction.B] = nil
		return opContinue
	}
	typed := unboxToTypedUintSlice(value, vm.Arena)
	if typed == nil && value.Len() != 0 {
		vm.evalError = fmt.Errorf("cannot convert %s to []uint64", value.Type())
		return opPanicError
	}
	registers.slicesUint[instruction.B] = typed
	return opContinue
}

// handleSubOpAdoptGeneralToSlicesByte mirrors handleSubOpAdoptGeneralToSlicesInt for the
// slicesByte bank.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue or opPanicError.
func handleSubOpAdoptGeneralToSlicesByte(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.slicesByte[instruction.B] = nil
		return opContinue
	}
	typed, ok := reflect.TypeAssert[[]byte](value)
	if !ok {
		vm.evalError = fmt.Errorf("cannot convert %s to []byte", value.Type())
		return opPanicError
	}
	registers.slicesByte[instruction.B] = typed
	return opContinue
}

// handleSubOpBoxSliceFloat boxes a typed []float64 into the general bank: general[B] =
// reflect.ValueOf(slicesFloat[C]). Mirror of handleSubOpBoxSliceInt for the float bank.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination general register B and
// the source slicesFloat register C.
//
// Returns opContinue.
func handleSubOpBoxSliceFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = reflect.ValueOf(registers.slicesFloat[instruction.C])
	return opContinue
}

// handleSubOpBoxSliceString mirrors handleSubOpBoxSliceFloat for the slicesString bank.
//
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpBoxSliceString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = reflect.ValueOf(registers.slicesString[instruction.C])
	return opContinue
}

// handleSubOpBoxSliceBool mirrors handleSubOpBoxSliceFloat for the slicesBool bank.
//
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpBoxSliceBool(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = reflect.ValueOf(registers.slicesBool[instruction.C])
	return opContinue
}

// handleSubOpBoxSliceUint mirrors handleSubOpBoxSliceFloat for the slicesUint bank.
//
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpBoxSliceUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = reflect.ValueOf(registers.slicesUint[instruction.C])
	return opContinue
}
