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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// nativeArgumentProbeDepth bounds the nesting abandonIfArgumentReferencesArena follows;
	// a probe that stops early answers that the argument references the arena.
	nativeArgumentProbeDepth = 3

	// nativeArgumentProbeBudget bounds the values that probe visits.
	nativeArgumentProbeBudget = 64
)

// runtimeClosurePointerType is the type of a guest closure value.
var runtimeClosurePointerType = reflect.TypeFor[*RuntimeClosure]()

// nativeMayKeepArguments reports whether a native callee may keep a reference to an
// argument beyond the call, by the arena's borrow registry.
//
// Takes arena (*RegisterArena) which supplies the registry.
// Takes function (reflect.Value) which is the native callee.
//
// Returns bool which is true for unknown and retaining callees.
func nativeMayKeepArguments(arena *RegisterArena, function reflect.Value) bool {
	switch arena.nativeBorrowClass(function) {
	case BorrowBorrows, BorrowResultAliases, BorrowReadsOnly:
		return false
	default:
		return true
	}
}

// undo puts back each detached register that still holds its clone (a native result
// written into the same register is left alone).
//
// Takes registers (*Registers) which holds the registers.
func (r *nativeArgumentRestore) undo(registers *Registers) {
	for _, entry := range r.strings[:r.nStrings] {
		current := registers.Strings[entry.register]
		if len(current) == len(entry.clone) && unsafe.StringData(current) == unsafe.StringData(entry.clone) {
			registers.Strings[entry.register] = entry.original
		}
	}
	for _, entry := range r.generals[:r.nGeneral] {
		if registers.General[entry.register] == entry.clone {
			registers.General[entry.register] = entry.original
		}
	}
	r.reset()
}

// detachNativeStringArguments detaches, in the registers, the arguments a native call is
// about to pass to a callee that may keep them.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes registers (*Registers) which holds the arguments.
// Takes site (*program.CallSite) which locates them.
// Takes function (reflect.Value) which is the native callee.
// Takes vm (*VM) whose scratch record (restoreRecord) takes the rewrites, made at the
// VM's first rewrite (the record is about 600 bytes, so returning it by value would zero
// and copy it on every native call).
//
// Returns *nativeArgumentRestore which holds the rewrites to undo, or nil when there are
// none.
func detachNativeStringArguments(arena *RegisterArena, registers *Registers, site *program.CallSite, function reflect.Value, vm *VM) *nativeArgumentRestore {
	var restore *nativeArgumentRestore
	if !function.IsValid() || function.Kind() != reflect.Func || !nativeMayKeepArguments(arena, function) {
		return nil
	}
	for _, location := range site.Arguments {
		switch location.Kind {
		case isa.RegisterString:
			if int(location.Register) < len(registers.Strings) {
				restore = detachNativeStringArgument(arena, registers, location.Register, vm, restore)
			}
		case isa.RegisterGeneral:
			if int(location.Register) < len(registers.General) {
				restore = detachNativeGeneralArgument(arena, registers, location.Register, vm, restore)
			}
		default:
			if backing, ok := typedSliceBacking(registers, location.Kind, location.Register); ok && arena.ownsAnyStorage(backing) {
				arena.unpooled = true
			}
		}
	}
	return restore
}

// detachNativeStringArgument clones one arena string argument in place and records the
// rewrite.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes registers (*Registers) which holds the argument.
// Takes register (uint8) which is the argument's string register, in range.
// Takes vm (*VM) whose scratch record takes the rewrite.
// Takes restore (*nativeArgumentRestore) which is the record so far, or nil.
//
// Returns *nativeArgumentRestore which is the record, made on the first rewrite.
func detachNativeStringArgument(arena *RegisterArena, registers *Registers, register uint8, vm *VM, restore *nativeArgumentRestore) *nativeArgumentRestore {
	original := registers.Strings[register]
	clone := cloneArenaString(arena, original)
	if unsafe.StringData(clone) == unsafe.StringData(original) {
		return restore
	}
	registers.Strings[register] = clone
	if restore == nil {
		restore = vm.restoreRecord(false)
	}
	if restore.nStrings < nativeArgumentRestoreSlots {
		restore.strings[restore.nStrings] = nativeStringRestore{original: original, clone: clone, register: register}
		restore.nStrings++
	}
	return restore
}

// detachNativeGeneralArgument detaches one general argument in place and records the
// rewrite.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes registers (*Registers) which holds the argument.
// Takes register (uint8) which is the argument's general register, in range.
// Takes vm (*VM) whose scratch record takes the rewrite.
// Takes restore (*nativeArgumentRestore) which is the record so far, or nil.
//
// Returns *nativeArgumentRestore which is the record, made on the first rewrite.
func detachNativeGeneralArgument(arena *RegisterArena, registers *Registers, register uint8, vm *VM, restore *nativeArgumentRestore) *nativeArgumentRestore {
	original := registers.General[register]
	detached := detachNativeArgument(arena, original)
	if detached == original {
		return restore
	}
	registers.General[register] = detached
	if restore == nil {
		restore = vm.restoreRecord(false)
	}
	if restore.nGeneral < nativeArgumentRestoreSlots {
		restore.generals[restore.nGeneral] = nativeGeneralRestore{original: original, clone: detached, register: register}
		restore.nGeneral++
	}
	return restore
}

// abandonIfArgumentReferencesArena marks arena to be abandoned at release, as the abandon
// fallback does, when a composite argument handed to a native that may keep it references
// arena storage.
//
// A slice must keep its backing, which the native may write and the guest read back, so
// it cannot be copied; the arena is therefore not reused, and the storage the native
// keeps stays valid for as long as it is reachable.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the argument.
func abandonIfArgumentReferencesArena(arena *RegisterArena, value reflect.Value) {
	if arena.unpooled || !value.IsValid() || value.Kind() == reflect.String || value.Kind() == reflect.Func {
		return
	}
	budget := nativeArgumentProbeBudget
	if isRuntimeClosureValue(value) {
		if !arena.abandonAtRelease && closureReferencesArena(arena, (*RuntimeClosure)(value.UnsafePointer()), nativeArgumentProbeDepth, &budget) {
			arena.abandonAtRelease = true
		}
		return
	}
	if references, _ := arena.referencesArenaStorage(value, false, nativeArgumentProbeDepth, &budget, false); references {
		arena.unpooled = true
	}
}

// closureReferencesArena reports whether a guest closure's cells reach arena storage.
//
// It checks indirect cells whose variables or values reach the arena, direct cells
// holding arena strings or typed-slice backings, general values referencing arena
// storage, and nested closures.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes closure (*RuntimeClosure) which is the closure; nil holds nothing.
// Takes depth (int) which bounds the nesting followed.
// Takes budget (*int) which bounds the values visited.
//
// Returns bool which is true when the closure may reach arena storage.
func closureReferencesArena(arena *RegisterArena, closure *RuntimeClosure, depth int, budget *int) bool {
	if closure == nil {
		return false
	}
	if depth <= 0 || *budget <= 0 {
		return true
	}
	*budget--
	if arena.ownsAnyStorage(unsafe.Pointer(closure)) {
		return true
	}
	for _, cell := range closure.upvalues {
		if cell == nil {
			continue
		}
		if cellReferencesArena(arena, cell, depth-1, budget) {
			return true
		}
	}
	return false
}

// cellReferencesArena is closureReferencesArena for one upvalue cell.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes cell (*program.UpvalueCell) which is the cell.
// Takes depth (int) which bounds the nesting followed.
// Takes budget (*int) which bounds the values visited.
//
// Returns bool which is true when the cell may reach arena storage.
func cellReferencesArena(arena *RegisterArena, cell *program.UpvalueCell, depth int, budget *int) bool {
	if arena.ownsAnyStorage(unsafe.Pointer(cell)) {
		return true
	}
	if cell.IsIndirect {
		return generalReferencesArena(arena, cell.GeneralValue, depth, budget)
	}
	switch cell.Kind {
	case isa.RegisterString:
		return arena.referencesArenaString(cell.StringValue)
	case isa.RegisterGeneral:
		return generalReferencesArena(arena, cell.GeneralValue, depth, budget)
	case isa.RegisterSliceInt64:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceIntValue)))
	case isa.RegisterSliceFloat64:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceFloatValue)))
	case isa.RegisterSliceString:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceStringValue))) ||
			generalReferencesArena(arena, reflect.ValueOf(cell.SliceStringValue), depth, budget)
	case isa.RegisterSliceBool:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceBoolValue)))
	case isa.RegisterSliceUint64:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceUintValue)))
	case isa.RegisterSliceByte:
		return arena.ownsAnyStorage(unsafe.Pointer(unsafe.SliceData(cell.SliceByteValue)))
	default:
		return false
	}
}

// generalReferencesArena reports whether a general value reaches arena storage, following
// a nested guest closure through its cells.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the value; an invalid one holds nothing.
// Takes depth (int) which bounds the nesting followed.
// Takes budget (*int) which bounds the values visited.
//
// Returns bool which is true when value may reach arena storage.
func generalReferencesArena(arena *RegisterArena, value reflect.Value, depth int, budget *int) bool {
	if !value.IsValid() {
		return false
	}
	if isRuntimeClosureValue(value) {
		return closureReferencesArena(arena, (*RuntimeClosure)(value.UnsafePointer()), depth, budget)
	}
	if value.Kind() == reflect.Pointer && !value.IsNil() && arena.ownsAnyStorage(value.UnsafePointer()) {
		return true
	}
	references, _ := arena.referencesArenaStorage(value, false, depth, budget, false)
	return references
}

// isRuntimeClosureValue reports whether v is a guest closure.
//
// Takes v (reflect.Value) which is the argument.
//
// Returns bool which is true for a *RuntimeClosure.
func isRuntimeClosureValue(v reflect.Value) bool {
	return v.Kind() == reflect.Pointer && v.Type() == runtimeClosurePointerType
}

// typedSliceBacking returns the backing address of a typed-slice register.
//
// Takes registers (*Registers) which holds the banks.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes register (uint8) which is the register.
//
// Returns unsafe.Pointer which is the backing.
// Returns bool which is false for other banks.
func typedSliceBacking(registers *Registers, kind isa.RegisterKind, register uint8) (unsafe.Pointer, bool) {
	switch kind {
	case isa.RegisterSliceInt64:
		return bankBacking(registers.SlicesInt, register)
	case isa.RegisterSliceFloat64:
		return bankBacking(registers.slicesFloat, register)
	case isa.RegisterSliceString:
		return bankBacking(registers.slicesString, register)
	case isa.RegisterSliceBool:
		return bankBacking(registers.slicesBool, register)
	case isa.RegisterSliceUint64:
		return bankBacking(registers.slicesUint, register)
	case isa.RegisterSliceByte:
		return bankBacking(registers.slicesByte, register)
	default:
		return nil, false
	}
}

// bankBacking returns the backing address of bank[register].
//
// Takes bank ([][]T) which is the typed-slice bank.
// Takes register (uint8) which is the register.
//
// Returns unsafe.Pointer which is the backing, nil for an empty slice.
// Returns bool which is false when register is out of range.
func bankBacking[T any](bank [][]T, register uint8) (unsafe.Pointer, bool) {
	if int(register) >= len(bank) || cap(bank[register]) == 0 {
		return nil, false
	}
	return unsafe.Pointer(unsafe.SliceData(bank[register])), true
}

// detachNativeMethodStringArguments is detachNativeStringArguments for a bound native
// method whose arguments are already built.
//
// Takes arena (*RegisterArena) which owns the candidate strings.
// Takes method (reflect.Value) which is the bound method.
// Takes arguments ([]reflect.Value) which are rewritten in place.
func detachNativeMethodStringArguments(arena *RegisterArena, method reflect.Value, arguments []reflect.Value) {
	if !method.IsValid() || method.Kind() != reflect.Func || !nativeMayKeepArguments(arena, method) {
		return
	}
	for index, argument := range arguments {
		arguments[index] = detachNativeArgument(arena, argument)
	}
}

// detachNativeArgument detaches one argument a native that may keep it is about to
// receive.
//
// Takes arena (*RegisterArena) which owns the candidate storage.
// Takes value (reflect.Value) which is the argument.
//
// Returns reflect.Value which is the argument to pass.
func detachNativeArgument(arena *RegisterArena, value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Kind() == reflect.String {
		text := value.String()
		clone := cloneArenaString(arena, text)
		if unsafe.StringData(clone) == unsafe.StringData(text) && !arena.OwnsStringBox(ReflectValuePtr(value)) {
			return value
		}
		return reflect.ValueOf(clone).Convert(value.Type())
	}
	if arena.interfaceSharesArenaStorage(value) {
		value = copyReflectValue(value)
	}
	abandonIfArgumentReferencesArena(arena, value)
	return value
}
