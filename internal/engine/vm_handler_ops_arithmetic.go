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
	"math"
	"reflect"
	"strings"
	"unsafe"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// float32BitWidth is the width of a float32, the shift that packs a complex64's imaginary
// part above its real part in one 64-bit word.
const float32BitWidth = 32

// runtimeClosureReflectType is the reflect type of a boxed interpreted closure.
var runtimeClosureReflectType = reflect.TypeFor[*RuntimeClosure]()

// PackTypedSliceStringToGeneral wraps an []string slice via the arena slice-header pool.
// See packTypedSliceToGeneral.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]string) which is the slice to wrap.
//
// Returns reflect.Value which wraps s with the cached string-slice type.
func PackTypedSliceStringToGeneral(arena *RegisterArena, s []string) reflect.Value {
	if arena == nil {
		return reflect.ValueOf(s)
	}
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), stringSliceReflectType)
}

// HandleTruncateNarrow truncates a narrow integer register to its declared width, using a
// zero-fill mask for unsigned and sign-extension for signed.
//
// Takes registers (*Registers) which provides the narrow integer banks.
// Takes instruction (instruction) which encodes the register index, the bit width, and
// the bank kind.
//
// Returns OpResult indicating the next execution step.
func HandleTruncateNarrow(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	bitWidth := uint(instruction.B)
	if isa.RegisterKind(instruction.C) == isa.RegisterUint {
		mask := uint64(1)<<bitWidth - 1
		registers.Uints[instruction.A] &= mask
		return opContinue
	}
	shift := registerBitWidth - bitWidth
	value := registers.Ints[instruction.A]
	registers.Ints[instruction.A] = (value << shift) >> shift
	return opContinue
}

// ReflectInterfaceEqual implements Go's == for two interface values, returning
// uncomparable when the shared dynamic type cannot be compared.
//
// Takes a (reflect.Value) which is the first operand.
// Takes b (reflect.Value) which is the second operand.
//
// Returns equal (bool) which is the == result when uncomparable is false.
// Returns uncomparable (bool) which is true when the types are uncomparable.
func ReflectInterfaceEqual(a, b reflect.Value) (equal bool, uncomparable bool) {
	equal, offending := interfaceValuesEqual(a, b, true)
	return equal, offending.IsValid()
}

// handleJump performs an unconditional branch by adding a signed offset to the program
// counter.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes instruction (instruction) which encodes the signed branch offset.
//
// Returns OpResult indicating the next execution step.
func handleJump(_ *VM, frame *CallFrame, _ *Registers, instruction isa.Instruction) OpResult {
	frame.ProgramCounter += int(instruction.SignedOffset())
	return opContinue
}

// handleJumpIfTrue performs a conditional branch when the integer condition register is
// non-zero.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the condition register and branch offset.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfTrue(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if registers.Ints[instruction.A] != 0 {
		frame.ProgramCounter += int(instruction.SignedOffset())
	}
	return opContinue
}

// handleJumpIfFalse performs a conditional branch when the integer condition register is
// zero.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the condition register and branch offset.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if registers.Ints[instruction.A] == 0 {
		frame.ProgramCounter += int(instruction.SignedOffset())
	}
	return opContinue
}

// handleUnpackInterface extracts a concrete value from an interface in a general register
// into a typed register.
//
// When the source value is invalid or nil, the destination register is set to its zero
// value.
//
// Takes registers (*Registers) which provides all typed register banks.
// Takes instruction (instruction) which encodes the source and destination register
// indices, and the target register kind.
//
// Returns OpResult indicating the next execution step.
func handleUnpackInterface(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.B]
	if v.IsValid() && v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if !v.IsValid() {
		unpackInterfaceZero(registers, instruction)
	} else {
		unpackInterfaceValue(registers, instruction, v)
	}
	return opContinue
}

// unpackInterfaceZero writes the zero value for the destination kind when the source
// reflect.Value is invalid (nil interface).
//
// Only the banks with a dedicated sub-op are listed. The default routes through the
// generic pack or unpack handler, which covers all thirteen banks.
//
// Takes registers (*Registers) which provides the register file to write the zero value
// into.
// Takes instruction (instruction) which encodes the operand slots and the target register
// kind.
func unpackInterfaceZero(registers *Registers, instruction isa.Instruction) {
	if unpackInterfaceSliceZero(registers, isa.RegisterKind(instruction.C), instruction.A) {
		return
	}
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		registers.Ints[instruction.A] = 0
	case isa.RegisterFloat:
		registers.Floats[instruction.A] = 0
	case isa.RegisterString:
		registers.Strings[instruction.A] = ""
	case isa.RegisterGeneral:
		registers.General[instruction.A] = reflect.Value{}
	case isa.RegisterBool:
		registers.Bools[instruction.A] = false
	case isa.RegisterUint:
		registers.Uints[instruction.A] = 0
	case isa.RegisterComplex:
		registers.Complex[instruction.A] = 0
	default:
	}
}

// unpackInterfaceValue extracts a concrete value from a valid reflect.Value into the
// destination register bank.
//
// Takes registers (*Registers) which provides the register file.
// Takes instruction (instruction) which encodes the operand slots and the target register
// kind.
// Takes value (reflect.Value) which is the concrete value to extract.
func unpackInterfaceValue(registers *Registers, instruction isa.Instruction, value reflect.Value) {
	if unpackInterfaceSlice(registers, isa.RegisterKind(instruction.C), instruction.A, value) {
		return
	}
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		unpackInterfaceInt(registers, instruction.A, value)
	case isa.RegisterFloat:
		registers.Floats[instruction.A] = value.Float()
	case isa.RegisterString:
		registers.Strings[instruction.A] = value.String()
	case isa.RegisterGeneral:
		registers.General[instruction.A] = value
	case isa.RegisterBool:
		registers.Bools[instruction.A] = value.Bool()
	case isa.RegisterUint:
		registers.Uints[instruction.A] = value.Uint()
	case isa.RegisterComplex:
		registers.Complex[instruction.A] = value.Complex()
	default:
	}
}

// unpackInterfaceInt handles the isa.RegisterInt case which requires checking multiple
// numeric kinds (signed, unsigned, bool).
//
// Takes registers (*Registers) which provides the register file to write the integer
// value into.
// Takes destination (uint8) which is the index of the target integer register.
// Takes value (reflect.Value) which is the value to extract the integer from.
func unpackInterfaceInt(registers *Registers, destination uint8, value reflect.Value) {
	if value.CanInt() {
		registers.Ints[destination] = value.Int()
	} else if value.CanUint() {
		registers.Ints[destination] = safeconv.Uint64ToInt64Reinterpret(value.Uint())
	} else if value.Kind() == reflect.Bool {
		registers.Ints[destination] = boolToInt64(value.Bool())
	}
}

// unpackInterfaceSliceZero clears a typed-slice register slot.
//
// The six typed-slice banks are not covered by the scalar switch in
// unpackInterfaceZero(), so a heap-promoted named typed-slice result read through
// OpUnpackInterface must reset its bank slot here.
//
// Takes registers (*Registers) which holds the register banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes destination (uint8) which is the slot index.
//
// Returns bool which is true when kind names a typed-slice bank.
func unpackInterfaceSliceZero(registers *Registers, kind isa.RegisterKind, destination uint8) bool {
	switch kind {
	case isa.RegisterSliceInt:
		registers.SlicesInt[destination] = nil
	case isa.RegisterSliceFloat:
		registers.slicesFloat[destination] = nil
	case isa.RegisterSliceString:
		registers.slicesString[destination] = nil
	case isa.RegisterSliceBool:
		registers.slicesBool[destination] = nil
	case isa.RegisterSliceUint:
		registers.slicesUint[destination] = nil
	case isa.RegisterSliceByte:
		registers.slicesByte[destination] = nil
	default:
		return false
	}
	return true
}

// unpackInterfaceSlice aliases a reflect slice into a typed-slice register bank.
//
// The backing array is shared so element writes through either view remain mutually
// visible. Exact-storage banks alias by direct type assertion. Width-identical
// []int/[]uint banks reinterpret the header in place. Narrow-int element slices whose
// declared width differs from the widened bank layout return false because they cannot
// alias safely.
//
// Takes registers (*Registers) which holds the register banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes destination (uint8) which is the slot index.
// Takes value (reflect.Value) which is the source slice to alias.
//
// Returns bool which is true when value was aliased into the bank.
func unpackInterfaceSlice(registers *Registers, kind isa.RegisterKind, destination uint8, value reflect.Value) bool {
	if !value.IsValid() || value.Kind() != reflect.Slice {
		return false
	}
	switch kind {
	case isa.RegisterSliceInt:
		if bankSlice, ok := reflectSliceToBankInt(value); ok {
			registers.SlicesInt[destination] = bankSlice
			return true
		}
	case isa.RegisterSliceUint:
		if bankSlice, ok := reflectSliceToBankUint(value); ok {
			registers.slicesUint[destination] = bankSlice
			return true
		}
	case isa.RegisterSliceFloat:
		if bankSlice, ok := reflect.TypeAssert[[]float64](value); ok {
			registers.slicesFloat[destination] = bankSlice
			return true
		}
	case isa.RegisterSliceString:
		if bankSlice, ok := reflect.TypeAssert[[]string](value); ok {
			registers.slicesString[destination] = bankSlice
			return true
		}
	case isa.RegisterSliceBool:
		if bankSlice, ok := reflect.TypeAssert[[]bool](value); ok {
			registers.slicesBool[destination] = bankSlice
			return true
		}
	case isa.RegisterSliceByte:
		if bankSlice, ok := reflect.TypeAssert[[]byte](value); ok {
			registers.slicesByte[destination] = bankSlice
			return true
		}
	default:
	}
	return false
}

// reflectSliceToBankInt aliases a reflect slice value into the []int64 bank storage.
// Width- identical []int slices are reinterpreted in place; narrow-element slices return
// false.
//
// Takes value (reflect.Value) which is the source slice.
//
// Returns the aliased []int64 and true, or nil and false when no alias applies.
func reflectSliceToBankInt(value reflect.Value) ([]int64, bool) {
	if bankSlice, ok := reflect.TypeAssert[[]int64](value); ok {
		return bankSlice, true
	}
	element := value.Type().Elem()
	if element.Kind() == reflect.Int && element.Size() == widthIdenticalSliceElementBytes {
		return unsafe.Slice((*int64)(value.UnsafePointer()), value.Cap())[:value.Len()], true
	}
	return nil, false
}

// reflectSliceToBankUint aliases a reflect slice value into the []uint64 storage of the
// isa.RegisterSliceUint bank, mirroring reflectSliceToBankInt for the unsigned banks.
//
// An exact []uint64 aliases directly; a width-identical []uint is reinterpreted in place.
// Narrow-uint element slices ([]uint16/[]uint32) return false so the caller keeps them on
// the loud reflect path.
//
// Takes value (reflect.Value) which is the source slice (already known to be a slice).
//
// Returns the aliased []uint64 and true, or nil and false when no width-identical alias
// applies.
func reflectSliceToBankUint(value reflect.Value) ([]uint64, bool) {
	if bankSlice, ok := reflect.TypeAssert[[]uint64](value); ok {
		return bankSlice, true
	}
	element := value.Type().Elem()
	if (element.Kind() == reflect.Uint || element.Kind() == reflect.Uintptr) && element.Size() == widthIdenticalSliceElementBytes {
		return unsafe.Slice((*uint64)(value.UnsafePointer()), value.Cap())[:value.Len()], true
	}
	return nil, false
}

// handlePackInterface wraps a typed register value into a reflect.Value and stores it in
// a general register. The packed value stays arena-backed: registers share the arena's
// lifetime, and the store barrier detaches the value if it later escapes to the heap.
//
// Takes vm (*VM) which provides the arena allocator for the box slabs.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which provides all typed register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handlePackInterface(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		registers.General[instruction.A] = boxInt64ToGeneral(vm.Arena, registers.Ints[instruction.B])
	case isa.RegisterFloat:
		registers.General[instruction.A] = boxFloat64ToGeneral(vm.Arena, registers.Floats[instruction.B])
	case isa.RegisterString:
		registers.General[instruction.A] = BoxStringToGeneral(vm.Arena, registers.Strings[instruction.B])
	case isa.RegisterGeneral:
		registers.General[instruction.A] = registers.General[instruction.B]
	case isa.RegisterBool:
		registers.General[instruction.A] = boxBoolToGeneral(registers.Bools[instruction.B])
	case isa.RegisterUint:
		registers.General[instruction.A] = boxUint64ToGeneral(vm.Arena, registers.Uints[instruction.B])
	case isa.RegisterComplex:
		registers.General[instruction.A] = boxComplex128ToGeneral(vm.Arena, registers.Complex[instruction.B])
	case isa.RegisterSliceInt:
		registers.General[instruction.A] = packTypedSliceToGeneral(vm.Arena, registers.SlicesInt[instruction.B], intSliceReflectType)
	case isa.RegisterSliceFloat:
		registers.General[instruction.A] = packTypedSliceFloatToGeneral(vm.Arena, registers.slicesFloat[instruction.B])
	case isa.RegisterSliceString:
		registers.General[instruction.A] = PackTypedSliceStringToGeneral(vm.Arena, registers.slicesString[instruction.B])
	case isa.RegisterSliceBool:
		registers.General[instruction.A] = packTypedSliceBoolToGeneral(vm.Arena, registers.slicesBool[instruction.B])
	case isa.RegisterSliceUint:
		registers.General[instruction.A] = packTypedSliceUintToGeneral(vm.Arena, registers.slicesUint[instruction.B])
	case isa.RegisterSliceByte:
		if vm.Arena != nil {
			registers.General[instruction.A] = arenaWrapByteSlice(vm.Arena, registers.slicesByte[instruction.B])
		} else {
			registers.General[instruction.A] = reflect.ValueOf(registers.slicesByte[instruction.B])
		}
	default:

		vMBoundsError(vm, frame, boundsTableRegisterKind, int(instruction.C), isa.NumRegisterKinds)
		return opPanicError
	}
	return opContinue
}

// packTypedSliceToGeneral wraps an []int64 slice into a reflect.Value through the arena's
// slice-header slab, avoiding per-call mallocgc.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]int64) which is the slice to wrap.
// Takes sliceType (reflect.Type) which is the cached reflect.Type for the slice.
//
// Returns the wrapped reflect.Value.
func packTypedSliceToGeneral(arena *RegisterArena, s []int64, sliceType reflect.Type) reflect.Value {
	if arena == nil {
		return reflect.ValueOf(s)
	}
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), sliceType)
}

// packTypedSliceFloatToGeneral wraps an []float64 slice via the arena slice-header pool.
// See packTypedSliceToGeneral.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]float64) which is the slice to wrap.
//
// Returns reflect.Value which wraps s with the cached float-slice type.
func packTypedSliceFloatToGeneral(arena *RegisterArena, s []float64) reflect.Value {
	if arena == nil {
		return reflect.ValueOf(s)
	}
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), floatSliceReflectType)
}

// packTypedSliceBoolToGeneral wraps an []bool slice via the arena slice-header pool. See
// packTypedSliceToGeneral.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]bool) which is the slice to wrap.
//
// Returns reflect.Value which wraps s with the cached bool-slice type.
func packTypedSliceBoolToGeneral(arena *RegisterArena, s []bool) reflect.Value {
	if arena == nil {
		return reflect.ValueOf(s)
	}
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), boolSliceReflectType)
}

// packTypedSliceUintToGeneral wraps an []uint64 slice via the arena slice-header pool.
// See packTypedSliceToGeneral.
//
// Takes arena (*RegisterArena) which provides the slice-header slab.
// Takes s ([]uint64) which is the slice to wrap.
//
// Returns reflect.Value which wraps s with the cached uint-slice type.
func packTypedSliceUintToGeneral(arena *RegisterArena, s []uint64) reflect.Value {
	if arena == nil {
		return reflect.ValueOf(s)
	}
	data := unsafe.Pointer(unsafe.SliceData(s))
	return arenaWrapTypedSlice(arena, data, len(s), cap(s), uintSliceReflectType)
}

// handlePackTyped boxes a typed-bank register value into the general bank while
// preserving its exact source-level reflect.Type from the TypeTable.
//
// Takes vm (*VM) which provides the arena allocator.
// Takes frame (*CallFrame) which provides the TypeTable and the extension word.
// Takes registers (*Registers) which provides the typed register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handlePackTyped(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	if reflectType == nil {
		registers.General[instruction.A] = packInterfaceFallback(vm, registers, instruction)
		return opContinue
	}
	registers.General[instruction.A] = boxTypedToGeneral(vm.Arena, reflectType, registers, instruction.B, isa.RegisterKind(instruction.C))
	return opContinue
}

// packInterfaceFallback boxes a register value using canonical representation.
//
// Used by handlePackTyped when the recorded type index resolves to a nil reflect.Type.
// Falls back to canonical boxing rather than panicking.
//
// Takes vm (*VM) which provides the arena.
// Takes registers (*Registers) which provides the source banks.
// Takes instruction (isa.Instruction) which encodes B=source, C=kind.
//
// Returns reflect.Value which is the boxed value.
func packInterfaceFallback(vm *VM, registers *Registers, instruction isa.Instruction) reflect.Value {
	switch isa.RegisterKind(instruction.C) {
	case isa.RegisterInt:
		return boxInt64ToGeneral(vm.Arena, registers.Ints[instruction.B])
	case isa.RegisterFloat:
		return boxFloat64ToGeneral(vm.Arena, registers.Floats[instruction.B])
	case isa.RegisterString:
		return BoxStringToGeneral(vm.Arena, registers.Strings[instruction.B])
	case isa.RegisterBool:
		return boxBoolToGeneral(registers.Bools[instruction.B])
	case isa.RegisterUint:
		return boxUint64ToGeneral(vm.Arena, registers.Uints[instruction.B])
	case isa.RegisterComplex:
		return reflect.ValueOf(registers.Complex[instruction.B])
	default:
		return registers.General[instruction.B]
	}
}

// boxTypedToGeneral reconstructs a register value as a reflect.Value of the given type.
//
// When arena is non-nil the fast path routes through arena box slabs with zero mallocgc
// per call. Sub-width integer widths exploit LE aliasing. Float32 and Complex64 cannot
// alias the float64/complex128 slabs so they fall through to reflect.New().
//
// Takes arena (*RegisterArena) which provides bump-allocated box slabs and may be nil in
// test contexts.
// Takes reflectType (reflect.Type) which is the precise type to clothe the value in.
// Takes registers (*Registers) which provides the typed banks.
// Takes sourceRegister (uint8) which is the source slot.
// Takes sourceKind (isa.RegisterKind) which selects the bank.
//
// Returns reflect.Value which carries reflectType identity.
func boxTypedToGeneral(arena *RegisterArena, reflectType reflect.Type, registers *Registers, sourceRegister uint8, sourceKind isa.RegisterKind) reflect.Value {
	kind := reflectType.Kind()
	if arena != nil {
		if value, ok := boxScalarToArenaBox(arena, reflectType, kind, registers, sourceRegister); ok {
			return value
		}
	}
	if kind == reflect.Slice {
		if boxed, ok := boxTypedSliceAs(arena, reflectType, registers, sourceRegister, sourceKind); ok {
			return boxed
		}
	}
	out := reflect.New(reflectType).Elem()
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out.SetInt(registers.Ints[sourceRegister])
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		out.SetUint(registers.Uints[sourceRegister])
	case reflect.Float32, reflect.Float64:
		out.SetFloat(registers.Floats[sourceRegister])
	case reflect.String:
		out.SetString(registers.Strings[sourceRegister])
	case reflect.Bool:
		out.SetBool(registers.Bools[sourceRegister])
	case reflect.Complex64, reflect.Complex128:
		out.SetComplex(registers.Complex[sourceRegister])
	default:
		switch sourceKind {
		case isa.RegisterInt:
			return boxInt64ToGeneral(nil, registers.Ints[sourceRegister])
		case isa.RegisterFloat:
			return boxFloat64ToGeneral(nil, registers.Floats[sourceRegister])
		case isa.RegisterString:
			return reflect.ValueOf(registers.Strings[sourceRegister])
		case isa.RegisterBool:
			return boxBoolToGeneral(registers.Bools[sourceRegister])
		case isa.RegisterUint:
			return boxUint64ToGeneral(nil, registers.Uints[sourceRegister])
		default:
			return registers.General[sourceRegister]
		}
	}
	return out
}

// boxTypedSliceAs boxes a typed-slice bank register as the exact slice type the source
// carried.
//
// Element types that share their bank's storage width reinterpret the header in place;
// narrower integer elements, whose storage is widened, are copied into a fresh slice of
// the exact type so the boxed value has Go's layout.
//
// Takes arena (*RegisterArena) which provides the slice-header slab; may be nil.
// Takes reflectType (reflect.Type) which is the exact slice type.
// Takes registers (*Registers) which provides the typed banks.
// Takes sourceRegister (uint8) which is the source slot.
// Takes sourceKind (isa.RegisterKind) which selects the bank.
//
// Returns the boxed slice and true, or false when the source is not a typed-slice bank.
func boxTypedSliceAs(arena *RegisterArena, reflectType reflect.Type, registers *Registers, sourceRegister uint8, sourceKind isa.RegisterKind) (reflect.Value, bool) {
	elemSize := reflectType.Elem().Size()
	switch sourceKind {
	case isa.RegisterSliceInt:
		s := registers.SlicesInt[sourceRegister]
		if elemSize == unsafe.Sizeof(int64(0)) {
			return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
		}
		return copyIntsAs(reflectType, s), true
	case isa.RegisterSliceUint:
		s := registers.slicesUint[sourceRegister]
		if elemSize == unsafe.Sizeof(uint64(0)) {
			return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
		}
		return copyUintsAs(reflectType, s), true
	case isa.RegisterSliceFloat:
		s := registers.slicesFloat[sourceRegister]
		return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
	case isa.RegisterSliceString:
		s := registers.slicesString[sourceRegister]
		return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
	case isa.RegisterSliceBool:
		s := registers.slicesBool[sourceRegister]
		return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
	case isa.RegisterSliceByte:
		s := registers.slicesByte[sourceRegister]
		return wrapSliceHeaderAs(arena, unsafe.Pointer(unsafe.SliceData(s)), len(s), cap(s), reflectType), true
	case isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterGeneral,
		isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex:
		return reflect.Value{}, false
	}
	return reflect.Value{}, false
}

// wrapSliceHeaderAs reinterprets a typed-bank slice header as sliceType, through the
// arena header slab when one is available.
//
// Takes arena (*RegisterArena) which may be nil.
// Takes data (unsafe.Pointer) which is the backing array; it passes through verbatim so a
// zero-capacity non-nil slice keeps its non-nil data word.
// Takes length (int) which is the slice length.
// Takes capacity (int) which is the slice capacity.
// Takes sliceType (reflect.Type) which is the exact slice type.
//
// Returns the wrapped reflect.Value.
func wrapSliceHeaderAs(arena *RegisterArena, data unsafe.Pointer, length, capacity int, sliceType reflect.Type) reflect.Value {
	if arena != nil {
		return arenaWrapTypedSlice(arena, data, length, capacity, sliceType)
	}
	out := reflect.New(sliceType).Elem()
	*(*snapshotSliceHeader)(unsafe.Pointer(out.UnsafeAddr())) = snapshotSliceHeader{Data: data, Len: length, Cap: capacity}
	return out
}

// copyIntsAs materialises a widened int-bank slice as a slice of a narrower signed
// element type.
//
// Takes sliceType (reflect.Type) which is the exact slice type.
// Takes s ([]int64) which is the widened storage.
//
// Returns the freshly allocated slice value.
func copyIntsAs(sliceType reflect.Type, s []int64) reflect.Value {
	if s == nil {
		return reflect.Zero(sliceType)
	}
	out := reflect.MakeSlice(sliceType, len(s), cap(s))
	for i, v := range s {
		out.Index(i).SetInt(v)
	}
	return out
}

// copyUintsAs is copyIntsAs for the uint bank.
//
// Takes sliceType (reflect.Type) which is the exact slice type.
// Takes s ([]uint64) which is the widened storage.
//
// Returns the freshly allocated slice value.
func copyUintsAs(sliceType reflect.Type, s []uint64) reflect.Value {
	if s == nil {
		return reflect.Zero(sliceType)
	}
	out := reflect.MakeSlice(sliceType, len(s), cap(s))
	for i, v := range s {
		out.Index(i).SetUint(v)
	}
	return out
}

// boxScalarToArenaBox is the arena-allocating scalar boxing path.
//
// Routes scalar kinds with a bump-allocated arena slot variant to their typed slab;
// compound/non-scalar kinds (Slice, Struct, Map, Pointer, Interface, Func, Chan, Array,
// UnsafePointer) plus the invalid/untyped slots fall through to the reflect.New path.
//
// Takes arena (*RegisterArena) which provides the bump-allocated per-kind box slabs.
// Takes reflectType (reflect.Type) which is the declared destination type used to mint
// the result reflect.Value's ABI token.
// Takes kind (reflect.Kind) which is reflectType.Kind() pre-computed by the caller.
// Takes registers (*Registers) which provides the typed banks.
// Takes sourceRegister (uint8) which is the source slot.
//
// Returns reflect.Value which is the boxed scalar on match, or the zero Value for
// compound kinds.
// Returns bool which is true on a scalar match and false for compound kinds.
func boxScalarToArenaBox(arena *RegisterArena, reflectType reflect.Type, kind reflect.Kind, registers *Registers, sourceRegister uint8) (reflect.Value, bool) {
	abiType := reflectValueABIType(reflectType)
	if boxed, ok := boxSignedToArenaBox(arena, abiType, kind, registers.Ints, sourceRegister); ok {
		return boxed, true
	}
	if boxed, ok := boxUnsignedToArenaBox(arena, abiType, kind, registers.Uints, sourceRegister); ok {
		return boxed, true
	}
	return boxOtherScalarToArenaBox(arena, abiType, reflectType, kind, registers, sourceRegister)
}

// boxSignedToArenaBox boxes an int-bank register as a signed integer of kind, truncating
// to the narrow width first so the box holds the value the static type carries.
//
// Takes arena (*RegisterArena) which allocates the box.
// Takes abiType (unsafe.Pointer) which is the box's runtime type.
// Takes kind (reflect.Kind) which is the static integer kind.
// Takes ints ([]int64) which is the int bank.
// Takes sourceRegister (uint8) which is the register slot.
//
// Returns the boxed value and true, or false when kind is not a signed integer kind.
func boxSignedToArenaBox(arena *RegisterArena, abiType unsafe.Pointer, kind reflect.Kind, ints []int64, sourceRegister uint8) (reflect.Value, bool) {
	switch kind {
	case reflect.Int, reflect.Int64, reflect.Int8, reflect.Int16, reflect.Int32:
	default:
		return reflect.Value{}, false
	}

	value := ints[sourceRegister]
	switch kind {
	case reflect.Int8:
		//nolint:gosec // matches SetInt truncation
		value = int64(int8(value))
	case reflect.Int16:
		//nolint:gosec // matches SetInt truncation
		value = int64(int16(value))
	case reflect.Int32:
		//nolint:gosec // matches SetInt truncation
		value = int64(int32(value))
	default:
	}
	slot := arena.allocIntBox(value)
	return unsafeNewAt(abiType, unsafe.Pointer(slot), kind), true
}

// boxUnsignedToArenaBox boxes a uint-bank register as an unsigned integer of kind,
// truncating to the narrow width first.
//
// Takes arena (*RegisterArena) which allocates the box.
// Takes abiType (unsafe.Pointer) which is the box's runtime type.
// Takes kind (reflect.Kind) which is the static integer kind.
// Takes uints ([]uint64) which is the uint bank.
// Takes sourceRegister (uint8) which is the register slot.
//
// Returns the boxed value and true, or false when kind is not an unsigned integer kind.
func boxUnsignedToArenaBox(arena *RegisterArena, abiType unsafe.Pointer, kind reflect.Kind, uints []uint64, sourceRegister uint8) (reflect.Value, bool) {
	switch kind {
	case reflect.Uint, reflect.Uint64, reflect.Uintptr, reflect.Uint8, reflect.Uint16, reflect.Uint32:
	default:
		return reflect.Value{}, false
	}
	value := uints[sourceRegister]
	switch kind {
	case reflect.Uint8:
		//nolint:gosec // matches SetUint truncation
		value = uint64(uint8(value))
	case reflect.Uint16:
		//nolint:gosec // matches SetUint truncation
		value = uint64(uint16(value))
	case reflect.Uint32:
		//nolint:gosec // matches SetUint truncation
		value = uint64(uint32(value))
	default:
	}
	slot := arena.allocUintBox(value)
	return unsafeNewAt(abiType, unsafe.Pointer(slot), kind), true
}

// boxOtherScalarToArenaBox boxes a float, string, bool or complex register. A float32 and
// a complex64 are stored as their bit patterns in a uint box; a bool takes the shared
// bool box and only for the plain bool type.
//
// Takes arena (*RegisterArena) which allocates the box.
// Takes abiType (unsafe.Pointer) which is the box's runtime type.
// Takes reflectType (reflect.Type) which is the static type.
// Takes kind (reflect.Kind) which is the static kind.
// Takes registers (*Registers) which is the register file.
// Takes sourceRegister (uint8) which is the register slot.
//
// Returns the boxed value and true, or false when kind is a compound kind (the caller
// falls back to reflect.New).
func boxOtherScalarToArenaBox(arena *RegisterArena, abiType unsafe.Pointer, reflectType reflect.Type, kind reflect.Kind, registers *Registers, sourceRegister uint8) (reflect.Value, bool) {
	switch kind {
	case reflect.Float32:
		slot := arena.allocUintBox(uint64(math.Float32bits(float32(registers.Floats[sourceRegister]))))
		return unsafeNewAt(abiType, unsafe.Pointer(slot), reflect.Float32), true
	case reflect.Float64:
		slot := arena.allocFloatBox(registers.Floats[sourceRegister])
		return unsafeNewAt(abiType, unsafe.Pointer(slot), reflect.Float64), true
	case reflect.String:
		slot := arena.allocStringBox(registers.Strings[sourceRegister])
		return unsafeNewAt(abiType, unsafe.Pointer(slot), reflect.String), true
	case reflect.Bool:
		if reflectType != plainBoolReflectType {
			return reflect.Value{}, false
		}
		return boxBoolToGeneral(registers.Bools[sourceRegister]), true
	case reflect.Complex64:
		value := registers.Complex[sourceRegister]
		bits := uint64(math.Float32bits(float32(real(value)))) | uint64(math.Float32bits(float32(imag(value))))<<float32BitWidth
		slot := arena.allocUintBox(bits)
		return unsafeNewAt(abiType, unsafe.Pointer(slot), reflect.Complex64), true
	case reflect.Complex128:
		slot := arena.allocComplexBox(registers.Complex[sourceRegister])
		return unsafeNewAt(abiType, unsafe.Pointer(slot), reflect.Complex128), true
	default:
		return reflect.Value{}, false
	}
}

// handleIntToFloat converts a signed integer register value to float64 and stores it in a
// float register.
//
// Takes registers (*Registers) which provides the integer and float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleIntToFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = float64(registers.Ints[instruction.B])
	return opContinue
}

// handleFloatToInt converts a floating-point register value to int64 and stores it in an
// integer register.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFloatToInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(registers.Floats[instruction.B])
	return opContinue
}

// handleRoundFloat32 rounds a float register value to float32 precision. float32 values
// share the float64 bank, so the compiler emits this after every operation whose static
// type is float32 to reproduce Go's single-precision rounding.
//
// Takes registers (*Registers) which provides the float register bank.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRoundFloat32(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = float64(float32(registers.Floats[instruction.B]))
	return opContinue
}

// handleRoundComplex64 rounds both parts of a complex register value to float32
// precision, the complex64 counterpart of handleRoundFloat32.
//
// Takes registers (*Registers) which provides the complex register bank.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRoundComplex64(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = complex128(complex64(registers.Complex[instruction.B]))
	return opContinue
}

// handleMoveBool copies a boolean value between virtual machine registers.
//
// Takes registers (*Registers) which provides the boolean register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveBool(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = registers.Bools[instruction.B]
	return opContinue
}

// handleLoadBoolConst loads a boolean constant from the function constant pool into a
// register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which provides the boolean register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadBoolConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.BoolConstants) {
		vMBoundsError(vm, frame, boundsTableBoolConstant, int(instruction.C), len(frame.Function.BoolConstants))
		return opPanicError
	}
	registers.Bools[instruction.B] = frame.Function.BoolConstants[instruction.C]
	return opContinue
}

// handleLoadComplexConst loads a complex number constant from the function constant pool
// into a register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which provides the complex register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadComplexConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.ComplexConstants) {
		vMBoundsError(vm, frame, boundsTableComplexConstant, int(index), len(frame.Function.ComplexConstants))
		return opPanicError
	}
	registers.Complex[instruction.A] = frame.Function.ComplexConstants[index]
	return opContinue
}

// handleAddComplex performs complex number addition of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the complex register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = registers.Complex[instruction.B] + registers.Complex[instruction.C]
	return opContinue
}

// handleSubComplex performs complex number subtraction of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the complex register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSubComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = registers.Complex[instruction.B] - registers.Complex[instruction.C]
	return opContinue
}

// handleMulComplex performs complex number multiplication of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the complex register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMulComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = registers.Complex[instruction.B] * registers.Complex[instruction.C]
	return opContinue
}

// handleDivComplex performs complex number division of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the complex register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleDivComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = registers.Complex[instruction.B] / registers.Complex[instruction.C]
	return opContinue
}

// handleEqComplex tests equality of two complex register values and stores the boolean
// result as an int.
//
// Takes registers (*Registers) which provides the complex and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Complex[instruction.B] == registers.Complex[instruction.C])
	return opContinue
}

// handleNeComplex tests inequality of two complex register values and stores the boolean
// result as an int.
//
// Takes registers (*Registers) which provides the complex and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Complex[instruction.B] != registers.Complex[instruction.C])
	return opContinue
}

// handleIntToUint converts a signed integer register value to uint64 and stores it in an
// unsigned register.
//
// Takes registers (*Registers) which provides the integer and unsigned integer register
// banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleIntToUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = safeconv.Int64ToUint64Reinterpret(registers.Ints[instruction.B])
	return opContinue
}

// handleUintToInt converts an unsigned integer register value to int64 and stores it in a
// signed register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleUintToInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = safeconv.Uint64ToInt64Reinterpret(registers.Uints[instruction.B])
	return opContinue
}

// handleUintToFloat converts an unsigned integer register value to float64 and stores it
// in a float register.
//
// Takes registers (*Registers) which provides the unsigned integer and float register
// banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleUintToFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = float64(registers.Uints[instruction.B])
	return opContinue
}

// handleFloatToUint converts a floating-point register value to uint64 and stores it in
// an unsigned register.
//
// Takes registers (*Registers) which provides the float and unsigned integer register
// banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFloatToUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Uints[instruction.A] = uint64(registers.Floats[instruction.B])
	return opContinue
}

// handleBoolToInt converts a boolean register value to an integer representation and
// stores it in an int register.
//
// Takes registers (*Registers) which provides the boolean and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBoolToInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Bools[instruction.B])
	return opContinue
}

// handleIntToBool converts a signed integer register value to a boolean and stores it in
// a bool register.
//
// Takes registers (*Registers) which provides the integer and boolean register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleIntToBool(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = registers.Ints[instruction.B] != 0
	return opContinue
}

// handleBuildComplex constructs a complex number from two float register values and
// stores it in a complex register.
//
// Takes registers (*Registers) which provides the float and complex register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBuildComplex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Complex[instruction.A] = complex(registers.Floats[instruction.B], registers.Floats[instruction.C])
	return opContinue
}

// handleIncInt increments a signed integer register value by one in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleIncInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A]++
	return opContinue
}

// handleDecInt decrements a signed integer register value by one in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target register index.
//
// Returns OpResult indicating the next execution step.
func handleDecInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A]--
	return opContinue
}

// handleNeFloat tests inequality of two floating-point register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] != registers.Floats[instruction.C])
	return opContinue
}

// handleGtFloat tests whether the first float register is greater than the second and
// stores the result as an int.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGtFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] > registers.Floats[instruction.C])
	return opContinue
}

// handleGeFloat tests whether the first float register is greater than or equal to the
// second and stores the result.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGeFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] >= registers.Floats[instruction.C])
	return opContinue
}

// handleNeString tests inequality of two string register values and stores the boolean
// result as an int.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] != registers.Strings[instruction.C])
	return opContinue
}

// handleGtString tests whether the first string register is lexicographically greater
// than the second.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGtString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] > registers.Strings[instruction.C])
	return opContinue
}

// handleGeString tests whether the first string register is lexicographically greater
// than or equal to the second.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGeString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] >= registers.Strings[instruction.C])
	return opContinue
}

// interfaceValuesEqual compares two general-bank values with Go's == rules, recursing
// through arrays and structs and reporting an uncomparable offender when one is found.
//
// Takes a (reflect.Value) which is the left operand.
// Takes b (reflect.Value) which is the right operand.
// Takes lenientNil (bool) which treats an invalid value as nil of any nilable type.
//
// Returns equal (bool) which is the comparison result when comparable.
// Returns offending (reflect.Value) which is the uncomparable dynamic value, or invalid
// when the comparison completed.
func interfaceValuesEqual(a, b reflect.Value, lenientNil bool) (equal bool, offending reflect.Value) {
	a, b = comparableOperand(a), comparableOperand(b)
	if !a.IsValid() || !b.IsValid() {
		return nilOperandsEqual(a, b, lenientNil), reflect.Value{}
	}
	if a.Type() != b.Type() {
		return false, reflect.Value{}
	}
	if a.Type() == runtimeClosureReflectType {
		return false, a
	}
	if equal, handled := pointerIdentityEqual(a, b); handled {
		return equal, reflect.Value{}
	}
	if equal, offending, handled := compositeValuesEqual(a, b, lenientNil); handled {
		return equal, offending
	}
	if !a.Comparable() {
		return false, a
	}
	return a.Equal(b), reflect.Value{}
}

// comparableOperand strips the interface and adapter layers off an equality operand: an
// interface yields its dynamic value (invalid when nil) and an adapter its underlying
// script value.
//
// Takes v (reflect.Value) which is the operand.
//
// Returns reflect.Value which is the value to compare.
func comparableOperand(v reflect.Value) reflect.Value {
	if v.IsValid() && v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	return unwrapAdapterUnderlying(v)
}

// nilOperandsEqual decides an equality where at least one operand is nil: both nil is
// equal; one nil equals a nil pointer, func, slice, map or channel only under the lenient
// rule (`ptr == nil`), never under strict interface comparison.
//
// Takes a (reflect.Value) which is the first operand, possibly invalid.
// Takes b (reflect.Value) which is the second operand, possibly invalid.
// Takes lenientNil (bool) which selects the lenient rule.
//
// Returns bool which is true when the operands compare equal.
func nilOperandsEqual(a, b reflect.Value, lenientNil bool) bool {
	if !a.IsValid() && !b.IsValid() {
		return true
	}
	if !lenientNil {
		return false
	}
	valid := a
	if !a.IsValid() {
		valid = b
	}
	return isNilableAndNil(valid)
}

// compositeValuesEqual compares arrays element by element and structs field by field
// (sentinel fields skipped), stopping at the first unequal or uncomparable pair.
//
// Takes a (reflect.Value) which is the first operand.
// Takes b (reflect.Value) which is the second operand, of the same type.
// Takes lenientNil (bool) which is passed to the element comparisons.
//
// Returns the result, the offending element when one is uncomparable, and whether the
// operands were composite at all.
func compositeValuesEqual(a, b reflect.Value, lenientNil bool) (equal bool, offending reflect.Value, handled bool) {
	switch a.Kind() {
	case reflect.Array:
		equal, offending = arrayValuesEqual(a, b, lenientNil)
		return equal, offending, true
	case reflect.Struct:
		equal, offending = structValuesEqual(a, b, lenientNil)
		return equal, offending, true
	default:
		return false, reflect.Value{}, false
	}
}

// arrayValuesEqual compares two arrays of one type element by element, stopping at the
// first unequal or uncomparable pair.
//
// Takes a (reflect.Value) which is the first array.
// Takes b (reflect.Value) which is the second array.
// Takes lenientNil (bool) which is passed to the element comparisons.
//
// Returns the result and the offending element when one is uncomparable.
func arrayValuesEqual(a, b reflect.Value, lenientNil bool) (equal bool, offending reflect.Value) {
	for i := range a.Len() {
		if equal, offending = interfaceValuesEqual(a.Index(i), b.Index(i), lenientNil); !equal || offending.IsValid() {
			return equal, offending
		}
	}
	return true, reflect.Value{}
}

// structValuesEqual compares two structs of one type field by field, skipping sentinel
// fields and stopping at the first unequal or uncomparable pair.
//
// Takes a (reflect.Value) which is the first struct.
// Takes b (reflect.Value) which is the second struct.
// Takes lenientNil (bool) which is passed to the field comparisons.
//
// Returns the result and the offending field when one is uncomparable.
func structValuesEqual(a, b reflect.Value, lenientNil bool) (equal bool, offending reflect.Value) {
	for i := range a.NumField() {
		if strings.HasPrefix(a.Type().Field(i).Name, pipitIDFieldPrefix) {
			continue
		}
		if equal, offending = interfaceValuesEqual(a.Field(i), b.Field(i), lenientNil); !equal || offending.IsValid() {
			return equal, offending
		}
	}
	return true, reflect.Value{}
}

// raiseUncomparablePanic raises Go's "comparing uncomparable type" runtime panic for an
// interface == when the dynamic type cannot be compared, matching gc's panic message and
// remaining catchable by interpreted recover.
//
// Takes vm (*VM) whose panic path is invoked.
// Takes operand (reflect.Value) whose dynamic type names the uncomparable value.
//
// Returns OpResult which signals the VM dispatch loop after the panic is raised.
func raiseUncomparablePanic(vm *VM, operand reflect.Value) OpResult {
	typeName := "uncomparable value"
	value := operand
	if value.IsValid() && value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if value.IsValid() {
		typeName = value.Type().String()
		if closure := closureFromValue(value); closure != nil {
			typeName = closureTypeString(closure)
		}
	}
	return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: comparing uncomparable type %s", typeName))
}

// handleNeGeneral tests inequality of two general register values using reflection and
// stores the result.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	equal, offending := interfaceValuesEqual(
		registers.General[instruction.B], registers.General[instruction.C], true)
	if offending.IsValid() {
		return raiseUncomparablePanic(vm, offending)
	}
	registers.Ints[instruction.A] = boolToInt64(!equal)
	return opContinue
}
