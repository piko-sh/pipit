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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// globalBankSize returns the length of the global bank addressed by the given register
// kind, used for bounds-checking a global index before the typed accessors index into the
// underlying slice.
//
// GlobalStore has exactly these seven banks and no typed-slice banks, so a package-level
// slice is classified general before it reaches here. Adding slice arms would name fields
// that do not exist.
//
// Takes globals (*GlobalStore) which provides the global variable banks.
// Takes kind (isa.RegisterKind) which selects the bank to measure.
//
// Returns the number of slots in the selected bank.
func globalBankSize(globals *GlobalStore, kind isa.RegisterKind) int {
	switch kind {
	case isa.RegisterInt:
		return len(globals.ints)
	case isa.RegisterFloat:
		return len(globals.floats)
	case isa.RegisterString:
		return len(globals.Strings)
	case isa.RegisterGeneral:
		return len(globals.general)
	case isa.RegisterBool:
		return len(globals.bools)
	case isa.RegisterUint:
		return len(globals.uints)
	case isa.RegisterComplex:
		return len(globals.complexes)
	default:
		return 0
	}
}

// checkGlobalIndex verifies that a global variable index falls within the bank selected
// by instruction.c, setting an interpreted bounds error on the VM when it does not.
//
// Takes vm (*VM) which receives the bounds error.
// Takes frame (*CallFrame) which provides program-counter context.
// Takes index (int) which is the global slot index to validate.
// Takes kind (isa.RegisterKind) which selects the bank to bounds-check against.
//
// Returns true when the index is in range, false otherwise.
func checkGlobalIndex(vm *VM, frame *CallFrame, index int, kind isa.RegisterKind) bool {
	size := globalBankSize(vm.Globals, kind)
	if index < 0 || index >= size {
		vMBoundsError(vm, frame, "global", index, size)
		return false
	}
	return true
}

// globalGetByKind loads the global variable at the given index into the destination
// register instruction.a, dispatching by the register kind encoded in instruction.c.
//
// GlobalStore has exactly these seven banks and no typed-slice banks, so a package-level
// slice is classified general before it reaches here. Adding slice arms would name fields
// that do not exist.
//
// Takes registers (*Registers) which provides the typed register banks.
// Takes globals (*GlobalStore) which provides the global variable store.
// Takes index (int) which is the global variable slot index.
// Takes instruction (instruction) which encodes the operand slots and register kind.
func globalGetByKind(registers *Registers, globals *GlobalStore, index int, instruction isa.Instruction) {
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		registers.Ints[instruction.A] = globals.GetInt(index)
	case isa.RegisterFloat:
		registers.Floats[instruction.A] = globals.GetFloat(index)
	case isa.RegisterString:
		registers.Strings[instruction.A] = globals.GetString(index)
	case isa.RegisterGeneral:
		registers.General[instruction.A] = globals.GetGeneral(index)
	case isa.RegisterBool:
		registers.Bools[instruction.A] = globals.GetBool(index)
	case isa.RegisterUint:
		registers.Uints[instruction.A] = globals.GetUint(index)
	case isa.RegisterComplex:
		registers.Complex[instruction.A] = globals.getComplex(index)
	default:
	}
}

// globalSetByKind stores the source register instruction.a into the global variable at
// the given index, dispatching by the register kind encoded in instruction.c. Strings are
// materialised when goroutines are active.
//
// GlobalStore has exactly these seven banks and no typed-slice banks, so a package-level
// slice is classified general before it reaches here. Adding slice arms would name fields
// that do not exist.
//
// Takes registers (*Registers) which provides the typed register banks.
// Takes globals (*GlobalStore) which provides the global variable store.
// Takes index (int) which is the global variable slot index.
// Takes instruction (instruction) which encodes the source register and register kind.
// Takes hasGoroutines (bool) which indicates whether string materialisation is required
// for goroutine safety.
// Takes arena (*RegisterArena) which provides the string arena for materialisation.
func globalSetByKind(registers *Registers, globals *GlobalStore, index int, instruction isa.Instruction, hasGoroutines bool, arena *RegisterArena) {
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		globals.SetInt(index, registers.Ints[instruction.A])
	case isa.RegisterFloat:
		globals.SetFloat(index, registers.Floats[instruction.A])
	case isa.RegisterString:
		s := registers.Strings[instruction.A]
		if hasGoroutines {
			s = materialiseStringUnconditional(arena, s)
		}
		globals.SetString(index, s)
	case isa.RegisterGeneral:
		globals.SetGeneral(index, escapeArenaValueForGlobal(arena, registers.General[instruction.A]))
	case isa.RegisterBool:
		globals.SetBool(index, registers.Bools[instruction.A])
	case isa.RegisterUint:
		globals.SetUint(index, registers.Uints[instruction.A])
	case isa.RegisterComplex:
		globals.setComplex(index, registers.Complex[instruction.A])
	default:
	}
}

// escapeArenaValueForGlobal escapes a value for the global store.
//
// Composite-kind branches reuse MaterialiseArenaValueUnconditional; the primitive branch
// handles addressable reflect.Values built over arena scalar slabs (AllocIntBox /
// AllocFloatBox / AllocUintBox / AllocComplexBox) that the arena recycles each run.
// Without the heap copy, globals.general[i] would point into recycled arena memory and
// read as the zero value on the next Eval.
//
// Takes arena (*RegisterArena) which owns the candidate slabs.
// Takes v (reflect.Value) which is about to be persisted.
//
// Returns reflect.Value which is a heap-resident equivalent for arena-backed primitives,
// the composite-materialised value for struct/array/slice/string, and v unchanged
// otherwise.
func escapeArenaValueForGlobal(arena *RegisterArena, v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.Bool:
		if !v.CanAddr() {
			return v
		}
		return copyReflectValue(v)
	case reflect.Slice:
		if !arenaUsesUnsafeSlabs {
			return safeBuildEscapeSliceHeader(v)
		}
		return materialiseArenaValueUnconditional(arena, v)
	default:
		return materialiseArenaValueUnconditional(arena, v)
	}
}

// safeBuildEscapeSliceHeader rewraps v on the heap, snapshotting (Data, Len, Cap) into a
// fresh arenaSliceHeader. Used by the safe build's global-escape path; the unsafe build
// never calls this (ASM-tier paths and OwnsSliceHeaderPointer precisely detect arena
// residency).
//
// Takes v (reflect.Value) which is the slice value about to be stored in a global slot.
//
// Returns a reflect.Value with the same dynamic type as v, pointing at a heap-allocated
// header.
func safeBuildEscapeSliceHeader(v reflect.Value) reflect.Value {
	header := &arenaSliceHeader{
		Data: unsafe.Pointer(v.Pointer()),
		Len:  v.Len(),
		Cap:  v.Cap(),
	}
	return unsafeNewAt(reflectValueABIType(v.Type()), unsafe.Pointer(header), reflect.Slice)
}

// handleGetGlobal implements isa.OpGetGlobal. It loads a package-level variable at index
// instruction.b into register instruction.a of the bank indicated by instruction.c.
//
// Takes vm (*VM) which provides access to the global variable store.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which provides the typed register banks.
// Takes instruction (instruction) which encodes the operand slots, global index, and
// register kind.
//
// Returns OpResult indicating the next execution step.
func handleGetGlobal(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	kind := isa.RegisterKind(instruction.C)
	index := int(instruction.B) + bundleSlotBase(frame.Function, kind)
	if !checkGlobalIndex(vm, frame, index, kind) {
		return opPanicError
	}
	globalGetByKind(registers, vm.Globals, index, instruction)
	return opContinue
}

// handleSetGlobal implements isa.OpSetGlobal. It stores register instruction.a of the
// bank indicated by instruction.c into the package-level variable at index instruction.b.
//
// Takes vm (*VM) which provides access to the global variable store and goroutine-safety
// state for string materialisation.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which provides the typed register banks.
// Takes instruction (instruction) which encodes the source register, global index, and
// register kind.
//
// Returns OpResult indicating the next execution step.
func handleSetGlobal(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	kind := isa.RegisterKind(instruction.C)
	index := int(instruction.B) + bundleSlotBase(frame.Function, kind)
	if !checkGlobalIndex(vm, frame, index, kind) {
		return opPanicError
	}
	globalSetByKind(registers, vm.Globals, index, instruction, vm.hasGoroutines, vm.Arena)
	return opContinue
}

// handleGetGlobalWide implements isa.SubOpGetGlobalWide for globals whose index exceeds
// 255.
//
// Takes vm (*VM) which provides access to the global variable store.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which provides the typed register banks.
// Takes instruction (instruction) which encodes the operand slots and register kind.
//
// Returns OpResult indicating the next execution step.
func handleGetGlobalWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	kind := isa.RegisterKind(instruction.C)
	index := int(uint16(extensionWord.A)|uint16(extensionWord.B)<<isa.WideBitShift) + bundleSlotBase(frame.Function, kind)
	if !checkGlobalIndex(vm, frame, index, kind) {
		return opPanicError
	}
	globalGetByKind(registers, vm.Globals, index, isa.Instruction{Op: isa.OpDrillTier1, A: instruction.B, B: 0, C: instruction.C})
	return opContinue
}

// handleSetGlobalWide implements isa.SubOpSetGlobalWide for globals whose index exceeds
// 255.
//
// Takes vm (*VM) which provides access to the global variable store and goroutine-safety
// state for string materialisation.
// Takes frame (*CallFrame) which provides the extension word.
// Takes registers (*Registers) which provides the typed register banks.
// Takes instruction (instruction) which encodes the source register and register kind.
//
// Returns OpResult indicating the next execution step.
func handleSetGlobalWide(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	kind := isa.RegisterKind(instruction.C)
	index := int(uint16(extensionWord.A)|uint16(extensionWord.B)<<isa.WideBitShift) + bundleSlotBase(frame.Function, kind)
	if !checkGlobalIndex(vm, frame, index, kind) {
		return opPanicError
	}
	globalSetByKind(registers, vm.Globals, index, isa.Instruction{Op: isa.OpDrillTier1, A: instruction.B, B: 0, C: instruction.C}, vm.hasGoroutines, vm.Arena)
	return opContinue
}

// bundleSlotBase returns the load-time base offset to add to a global-access operand for
// fn's bundle, or 0 when the function is source-compiled (globalBases is nil). Inlined by
// the Go Compiler; one indirection, one load, one return.
//
// Takes fn (*CompiledFunction) which is the active function.
// Takes kind (isa.RegisterKind) which selects the global bank.
//
// Returns int which is the slot base offset for the kind.
func bundleSlotBase(fn *program.CompiledFunction, kind isa.RegisterKind) int {
	if fn == nil || fn.GlobalBases == nil {
		return 0
	}
	if int(kind) >= program.NumGlobalRegisterKinds {
		return 0
	}
	return int(fn.GlobalBases[kind])
}
