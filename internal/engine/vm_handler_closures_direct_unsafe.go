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

// cacheIndirectTarget records the pointee address, runtime type word and kinds of an
// indirect cell's heap box.
//
// Closure writes can then store straight through the pointer. Runs once at cell creation,
// before the cell is shared, so the cache needs no synchronisation. Cells whose pointer
// is invalid or nil stay uncached and take the reflect path.
//
// Takes cell (*UpvalueCell) which is the indirect cell to cache.
func cacheIndirectTarget(cell *program.UpvalueCell) {
	pointer := cell.GeneralValue
	if !pointer.IsValid() || pointer.Kind() != reflect.Pointer || pointer.IsNil() {
		return
	}
	pointee := pointer.Type().Elem()
	cell.IndirectKind = pointee.Kind()
	if pointee.Kind() == reflect.Slice {
		cell.IndirectElemKind = pointee.Elem().Kind()
	}
	cell.IndirectType = reflectValueABIType(pointee)
	cell.IndirectTarget = pointer.UnsafePointer()
}

// writeIndirectCellDirect stores a register straight into an indirect cell's heap box
// when the box's type matches the register bank's layout.
//
// Scalars are written through the cached pointer, strings and typed-slice headers are
// copied with runtimeTypedmemmove so the write barrier is preserved. Arena-backed slices
// and mismatched or unpopulated cells return false so the caller takes the reflect path,
// which materialises and coerces as before. Named banks get a direct unsafe write; the
// default delegates to writeIndirectSliceHeaderDirect(), which services the typed-slice
// banks.
//
// Takes arena (*RegisterArena) which owns slabs a slice backing may still live in.
// Takes cell (*UpvalueCell) which is the indirect cell being written.
// Takes registers (*Registers) which holds the source banks.
// Takes kind (isa.RegisterKind) which selects the source bank.
// Takes index (byte) which is the register index within that bank.
//
// Returns true when the value was stored directly.
func writeIndirectCellDirect(arena *RegisterArena, cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, index byte) bool {
	target := cell.IndirectTarget
	if target == nil {
		return false
	}
	switch kind {
	case isa.RegisterInt:
		return storeIndirectScalar(target, cell.IndirectKind, reflect.Int, reflect.Int64, registers.Ints[index])
	case isa.RegisterUint:
		return storeIndirectScalar(target, cell.IndirectKind, reflect.Uint, reflect.Uint64, registers.Uints[index])
	case isa.RegisterFloat:
		return storeIndirectScalar(target, cell.IndirectKind, reflect.Float64, reflect.Float64, registers.Floats[index])
	case isa.RegisterBool:
		return storeIndirectScalar(target, cell.IndirectKind, reflect.Bool, reflect.Bool, registers.Bools[index])
	case isa.RegisterComplex:
		return storeIndirectScalar(target, cell.IndirectKind, reflect.Complex128, reflect.Complex128, registers.Complex[index])
	case isa.RegisterString:
		if cell.IndirectKind != reflect.String {
			return false
		}
		value := materialiseStringUnconditional(arena, registers.Strings[index])
		runtimeTypedmemmove(cell.IndirectType, target, unsafe.Pointer(&value))
		return true
	default:
		return writeIndirectSliceHeaderDirect(arena, cell, registers, kind, index)
	}
}

// storeIndirectScalar writes value through target when the pointee kind is one of the two
// kinds that share the bank's width and representation.
//
// Takes target (unsafe.Pointer) which is the pointee address.
// Takes pointeeKind (reflect.Kind) which is the cached pointee kind.
// Takes first (reflect.Kind) which is one eligible pointee kind.
// Takes second (reflect.Kind) which is the other eligible pointee kind.
// Takes value (T) which is the bank value.
//
// Returns true when the store happened.
func storeIndirectScalar[T int64 | uint64 | float64 | bool | complex128](target unsafe.Pointer, pointeeKind, first, second reflect.Kind, value T) bool {
	if pointeeKind != first && pointeeKind != second && (pointeeKind != reflect.Uintptr || first != reflect.Uint) {
		return false
	}
	*(*T)(target) = value
	return true
}

// writeIndirectSliceHeaderDirect copies a typed-bank slice header over a slice pointee
// whose elements share the bank's element layout, unless the backing still lives in an
// arena slab (the reflect path copies it to the heap).
//
// Takes arena (*RegisterArena) which may own the backing.
// Takes cell (*UpvalueCell) which is the indirect cell.
// Takes registers (*Registers) which holds the typed-slice banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (byte) which is the register index.
//
// Returns true when the header was copied.
func writeIndirectSliceHeaderDirect(arena *RegisterArena, cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, index byte) bool {
	if cell.IndirectKind != reflect.Slice {
		return false
	}
	header, backing, ok := typedSliceBankHeader(registers, kind, index, cell.IndirectElemKind)
	if !ok || (arena != nil && arena.OwnsSliceBacking(backing)) {
		return false
	}
	runtimeTypedmemmove(cell.IndirectType, cell.IndirectTarget, header)
	return true
}

// typedSliceBankHeader returns the address of the bank slot's slice header and its
// backing pointer when the pointee element kind matches the bank's element layout.
//
// Membership test for the six typed-slice banks. The default is the "not a typed slice"
// answer, and every caller uses the false return to take the non-slice path.
//
// Takes registers (*Registers) which holds the typed-slice banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes index (byte) which is the register index.
// Takes elemKind (reflect.Kind) which is the pointee's element kind.
//
// Returns the header address, the backing pointer and true on a layout match.
func typedSliceBankHeader(registers *Registers, kind isa.RegisterKind, index byte, elemKind reflect.Kind) (header, backing unsafe.Pointer, ok bool) {
	switch kind {
	case isa.RegisterSliceInt:
		if elemKind == reflect.Int || elemKind == reflect.Int64 {
			slot := &registers.SlicesInt[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	case isa.RegisterSliceUint:
		if elemKind == reflect.Uint || elemKind == reflect.Uint64 || elemKind == reflect.Uintptr {
			slot := &registers.slicesUint[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	case isa.RegisterSliceFloat:
		if elemKind == reflect.Float64 {
			slot := &registers.slicesFloat[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	case isa.RegisterSliceString:
		if elemKind == reflect.String {
			slot := &registers.slicesString[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	case isa.RegisterSliceBool:
		if elemKind == reflect.Bool {
			slot := &registers.slicesBool[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	case isa.RegisterSliceByte:
		if elemKind == reflect.Uint8 {
			slot := &registers.slicesByte[index]
			return unsafe.Pointer(slot), unsafe.Pointer(unsafe.SliceData(*slot)), true
		}
	default:
	}
	return nil, nil, false
}
