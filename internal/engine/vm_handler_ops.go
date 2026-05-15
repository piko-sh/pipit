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

	"pipit.sh/pipit/internal/isa"
)

const (
	// registerBitWidth is the underlying width of the int and uint register banks. Used as
	// the high anchor when sign-extending narrow integers in isa.OpTruncateNarrow: the value
	// is shifted up by (registerBitWidth - declaredWidth), then arithmetic-shifted back down
	// so the sign bit of the declared width propagates correctly.
	registerBitWidth uint = 64

	// smallIntBoxLow is the inclusive lower bound of the pre-allocated reflect.Value box
	// cache for int64 values; mirrors CPython's small-int cache extended down to -128 to
	// cover common sentinels.
	smallIntBoxLow = -128

	// smallIntBoxHigh is the inclusive upper bound of the pre-allocated reflect.Value box
	// cache; 256 keeps the full byte range and small loop counters in cache.
	smallIntBoxHigh = 256

	// smallIntBoxSize is the entry count for smallIntBoxCache (inclusive of both bounds).
	smallIntBoxSize = smallIntBoxHigh - smallIntBoxLow + 1

	// smallUintBoxCacheSize covers byte-range uints (0..255), which is the common output of
	// string and []byte indexing. Index == value.
	smallUintBoxCacheSize = 256

	// extensionWideIndexHighByteShift is the left shift placing the extension word's B byte
	// into the high half of a uint16 wide index.
	extensionWideIndexHighByteShift = 8

	// widthIdenticalSliceElementBytes is the element width at which a slice header can be
	// aliased rather than copied between int and int64 banks.
	widthIdenticalSliceElementBytes = 8

	// integerDivideByZeroMessage is the panic message raised when an integer division or
	// remainder operation has a zero divisor. It mirrors the Go runtime's own wording so
	// interpreted programs observe identical panic text.
	integerDivideByZeroMessage = "runtime error: integer divide by zero"

	// MoveGeneralModeDynamic dispatches to valueCopyForBoundaryArena's runtime kind switch.
	MoveGeneralModeDynamic uint8 = 0

	// MoveGeneralModeAlias performs a direct reflect.Value header copy. Emitted when the
	// source's static type is alias-safe.
	MoveGeneralModeAlias uint8 = 1

	// MoveGeneralModeSnapshot unconditionally invokes the snapshot helper. Emitted when the
	// source's static type is struct or array.
	MoveGeneralModeSnapshot uint8 = 2
)

var (
	// smallIntBoxCache holds pre-allocated reflect.Value boxes for int64 values in
	// [smallIntBoxLow, smallIntBoxHigh]. Index = v - smallIntBoxLow.
	smallIntBoxCache [smallIntBoxSize]reflect.Value

	// smallUintBoxCache holds pre-allocated reflect.Value boxes for uint64 values in [0,
	// smallUintBoxCacheSize-1].
	smallUintBoxCache [smallUintBoxCacheSize]reflect.Value

	// boolBoxCache holds the two pre-allocated reflect.Value boxes for bool values. Index 0
	// is false, index 1 is true.
	boolBoxCache [2]reflect.Value

	// emptyStringBox is the pre-allocated reflect.Value for the empty string, which appears
	// frequently as a zero-value initialiser and loop sentinel.
	emptyStringBox = reflect.ValueOf("")

	// stringTypeABIType caches the *abi.Type for `string` so the arena- backed box
	// constructor can skip the reflect.Type to *abi.Type extract on the hot path. Populated
	// in init() once per process.
	stringTypeABIType = reflectValueABIType(reflect.TypeFor[string]())

	// intTypeABIType is the cached *abi.Type pointer for int, used by the arena-backed
	// boxInt64ToGeneral fast path.
	intTypeABIType = reflectValueABIType(reflect.TypeFor[int]())

	// float64TypeABIType is the cached *abi.Type pointer for float64, used by the
	// arena-backed boxFloat64ToGeneral fast path.
	float64TypeABIType = reflectValueABIType(reflect.TypeFor[float64]())

	// uint64TypeABIType is the cached *abi.Type pointer for uint64, used by the arena-backed
	// boxUint64ToGeneral fast path.
	uint64TypeABIType = reflectValueABIType(reflect.TypeFor[uint64]())

	// complex128TypeABIType is the cached *abi.Type pointer for complex128, used by the
	// arena-backed boxComplex128ToGeneral fast path to replace the per-call reflect.ValueOf
	// mallocgc.
	complex128TypeABIType = reflectValueABIType(reflect.TypeFor[complex128]())
)

var (
	// plainBoolReflectType is the predeclared bool type; boxScalarToArenaBox compares
	// against it so named bool types never leak through the plain-typed true/false cache.
	plainBoolReflectType = reflect.TypeFor[bool]()
)

// BoxStringToGeneral returns a reflect.Value wrapping s, using the arena's string-box
// slab when available.
//
// Takes arena (*RegisterArena) which is the per-VM bump arena; may be nil.
// Takes s (string) which is the value to box.
//
// Returns a reflect.Value of dynamic type string wrapping s.
func BoxStringToGeneral(arena *RegisterArena, s string) reflect.Value {
	if len(s) == 0 {
		return emptyStringBox
	}
	if arena != nil {
		slot := arena.allocStringBox(s)

		return unsafeNewAt(stringTypeABIType, unsafe.Pointer(slot), reflect.String)
	}
	return reflect.ValueOf(s)
}

func init() {
	for i := range smallIntBoxCache {
		smallIntBoxCache[i] = reflect.ValueOf(i + smallIntBoxLow)
	}
}

// boxInt64ToGeneral returns a reflect.Value wrapping v, using the static cache for small
// values and the arena's int-box slab otherwise.
//
// Takes arena (*RegisterArena) which is the per-VM bump arena; may be nil.
// Takes v (int64) which is the value to box.
//
// Returns a reflect.Value of dynamic type int wrapping v.
func boxInt64ToGeneral(arena *RegisterArena, v int64) reflect.Value {
	if v >= smallIntBoxLow && v <= smallIntBoxHigh {
		return smallIntBoxCache[v-smallIntBoxLow]
	}
	if arena != nil {
		slot := arena.allocIntBox(v)
		return unsafeNewAt(intTypeABIType, unsafe.Pointer(slot), reflect.Int)
	}
	return reflect.ValueOf(int(v))
}

func init() {
	for i := range smallUintBoxCache {
		smallUintBoxCache[i] = reflect.ValueOf(uint64(i))
	}
	boolBoxCache[0] = reflect.ValueOf(false)
	boolBoxCache[1] = reflect.ValueOf(true)
}

// boxUint64ToGeneral returns a reflect.Value wrapping v.
//
// For values in [0, smallUintBoxCacheSize) uses the static cache. For larger values, when
// arena is supplied, bump-allocates from arena.uintBoxSlab; when arena is nil, falls back
// to reflect.ValueOf.
//
// Takes arena (*RegisterArena) which is the per-VM bump arena; may be nil.
// Takes v (uint64) which is the value to box.
//
// Returns a reflect.Value of dynamic type uint64 wrapping v.
func boxUint64ToGeneral(arena *RegisterArena, v uint64) reflect.Value {
	if v < smallUintBoxCacheSize {
		return smallUintBoxCache[v]
	}
	if arena != nil {
		slot := arena.allocUintBox(v)
		return unsafeNewAt(uint64TypeABIType, unsafe.Pointer(slot), reflect.Uint64)
	}
	return reflect.ValueOf(v)
}

// boxFloat64ToGeneral returns a reflect.Value wrapping v.
//
// No static cache (floats don't have a clean small-value enumeration); arena-allocates
// when supplied, else reflect.ValueOf.
//
// Takes arena (*RegisterArena) which is the per-VM bump arena; may be nil.
// Takes v (float64) which is the value to box.
//
// Returns a reflect.Value of dynamic type float64 wrapping v.
func boxFloat64ToGeneral(arena *RegisterArena, v float64) reflect.Value {
	if arena != nil {
		slot := arena.allocFloatBox(v)
		return unsafeNewAt(float64TypeABIType, unsafe.Pointer(slot), reflect.Float64)
	}
	return reflect.ValueOf(v)
}

// boxComplex128ToGeneral returns a reflect.Value wrapping v.
//
// Sibling of boxInt64ToGeneral / boxFloat64ToGeneral. No static cache (complex128 doesn't
// have a clean small-value enumeration). When an arena is supplied, bump-allocates from
// arena.complexBoxSlab; when arena is nil, falls back to reflect.ValueOf.
//
// Takes arena (*RegisterArena) which is the per-VM bump arena; may be nil.
// Takes v (complex128) which is the value to box.
//
// Returns a reflect.Value of dynamic type complex128 wrapping v.
func boxComplex128ToGeneral(arena *RegisterArena, v complex128) reflect.Value {
	if arena != nil {
		slot := arena.allocComplexBox(v)
		return unsafeNewAt(complex128TypeABIType, unsafe.Pointer(slot), reflect.Complex128)
	}
	return reflect.ValueOf(v)
}

// boxBoolToGeneral returns a pre-cached reflect.Value wrapping v.
//
// Cost is one branch and one slice load - no allocation.
//
// Takes v (bool) which is the value to box.
//
// Returns a reflect.Value of dynamic type bool wrapping v.
func boxBoolToGeneral(v bool) reflect.Value {
	if v {
		return boolBoxCache[1]
	}
	return boolBoxCache[0]
}

// handleExt is the handler for an extension opcode slot; it is a no-op that continues
// dispatch.
//
// Returns OpResult indicating the next execution step.
func handleExt(_ *VM, _ *CallFrame, _ *Registers, _ isa.Instruction) OpResult { return opContinue }

// conditionalJump reads the extension word from the bytecode stream and advances the
// program counter by the encoded offset when shouldJump is true. This is a small helper
// shared by all const-compare-and-branch handlers to eliminate duplicated jump logic.
//
// Takes frame (*CallFrame) which provides the bytecode body and program counter.
// Takes shouldJump (bool) which indicates whether the branch should be taken.
//
// Returns OpResult indicating the next execution step.
func conditionalJump(frame *CallFrame, shouldJump bool) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	if shouldJump {
		offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// intConstBoundsCheck validates that instruction.c is within the integer constant pool
// and returns the constant value.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the integer constant pool.
// Takes instruction (instruction) which encodes the constant pool index in field c.
//
// Returns constantValue (int64) which is the constant value when ok is true.
// Returns errResult (OpResult) which is the error result when ok is false.
// Returns ok (bool) which indicates whether the bounds check passed.
func intConstBoundsCheck(vm *VM, frame *CallFrame, instruction isa.Instruction) (int64, OpResult, bool) {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return 0, opPanicError, false
	}
	return frame.Function.IntConstants[instruction.C], opContinue, true
}

// stringConstBoundsCheck validates that instruction.c is within the string constant pool
// and returns the constant value. When the index is out of bounds it triggers a VM bounds
// error and returns ok=false.
//
// Takes vm (*VM) which provides bounds-error reporting.
// Takes frame (*CallFrame) which provides the string constant pool.
// Takes instruction (instruction) which encodes the constant pool index in field c.
//
// Returns constantValue (string) which is the constant value when ok is true.
// Returns errResult (OpResult) which is the error result when ok is false.
// Returns ok (bool) which indicates whether the bounds check passed.
func stringConstBoundsCheck(vm *VM, frame *CallFrame, instruction isa.Instruction) (string, OpResult, bool) {
	if int(instruction.C) >= len(frame.Function.StringConstants) {
		vMBoundsError(vm, frame, boundsTableStringConstant, int(instruction.C), len(frame.Function.StringConstants))
		return "", opPanicError, false
	}
	return frame.Function.StringConstants[instruction.C], opContinue, true
}

// handleMoveInt copies a signed integer value between virtual machine registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes source and destination register indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B]
	return opContinue
}

// handleMoveFloat copies a floating-point value between virtual machine registers.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes source and destination register indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = registers.Floats[instruction.B]
	return opContinue
}

// handleMoveString copies a string value between virtual machine registers.
//
// Takes registers (*Registers) which provides the string register banks.
// Takes instruction (instruction) which encodes source and destination register indices.
//
// Returns OpResult indicating the next execution step.
func handleMoveString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = registers.Strings[instruction.B]
	return opContinue
}

// handleMoveGeneral copies a general-purpose reflect.Value between registers.
//
// The instruction's C operand encodes a snapshot mode chosen by the Compiler from the
// source operand's static type (see moveGeneralMode constants). Mode zero (dynamic)
// preserves the pre-existing ValueCopyForBoundary path so existing serialised bytecode
// continues to behave identically.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes source and destination register indices
// and the snapshot mode in operand C.
//
// Returns OpResult indicating the next execution step.
func handleMoveGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	source := registers.General[instruction.B]
	switch instruction.C {
	case MoveGeneralModeAlias:
		registers.General[instruction.A] = source
	case MoveGeneralModeSnapshot, isa.MoveGeneralModeSnapshotRangeCandidate:
		if !source.IsValid() {
			registers.General[instruction.A] = source
		} else {
			registers.General[instruction.A] = copyReflectValueArena(vm.Arena, source)
		}
	default:
		registers.General[instruction.A] = valueCopyForBoundaryArena(vm.Arena, source)
	}
	return opContinue
}

// handleMoveSliceInt copies a SlicesInt slice header.
//
// Used by the bytecode inliner during splice register remapping. Same-bank only;
// cross-bank conversions route through CopyOneCallArgument or the dedicated adoption
// opcodes (isa.SubOpAdoptGeneralToSlicesFloat etc.).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes destination index in operand A and source
// index in operand B.
//
// Returns OpResult signalling the VM dispatch loop to continue.
func handleMoveSliceInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.SlicesInt[instruction.A] = registers.SlicesInt[instruction.B]
	return opContinue
}

// handleMoveSliceFloat copies a slicesFloat slice header between typed-slice registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction).
//
// Returns OpResult indicating the next execution step.
func handleMoveSliceFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.slicesFloat[instruction.A] = registers.slicesFloat[instruction.B]
	return opContinue
}

// handleMoveSliceString copies a slicesString slice header between typed-slice registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction).
//
// Returns OpResult indicating the next execution step.
func handleMoveSliceString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.slicesString[instruction.A] = registers.slicesString[instruction.B]
	return opContinue
}

// handleMoveSliceBool copies a slicesBool slice header between typed-slice registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction).
//
// Returns OpResult indicating the next execution step.
func handleMoveSliceBool(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.slicesBool[instruction.A] = registers.slicesBool[instruction.B]
	return opContinue
}

// handleMoveSliceUint copies a slicesUint slice header between typed-slice registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction).
//
// Returns OpResult indicating the next execution step.
func handleMoveSliceUint(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.slicesUint[instruction.A] = registers.slicesUint[instruction.B]
	return opContinue
}

// handleMoveSliceByte copies a slicesByte slice header between typed-slice registers.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction).
//
// Returns OpResult indicating the next execution step.
func handleMoveSliceByte(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.slicesByte[instruction.A] = registers.slicesByte[instruction.B]
	return opContinue
}

// handleLoadIntConst loads a signed integer constant from the function constant pool into
// a register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadIntConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(index), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = frame.Function.IntConstants[index]
	return opContinue
}

// handleLoadFloatConst loads a floating-point constant from the function constant pool
// into a register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadFloatConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.FloatConstants) {
		vMBoundsError(vm, frame, boundsTableFloatConstant, int(index), len(frame.Function.FloatConstants))
		return opPanicError
	}
	registers.Floats[instruction.A] = frame.Function.FloatConstants[index]
	return opContinue
}

// handleLoadStringConst loads a string constant from the function constant pool into a
// register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which provides the string register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadStringConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.StringConstants) {
		vMBoundsError(vm, frame, boundsTableStringConstant, int(index), len(frame.Function.StringConstants))
		return opPanicError
	}
	registers.Strings[instruction.A] = frame.Function.StringConstants[index]
	return opContinue
}

// handleLoadGeneralConst loads a general constant from the function constant pool into a
// register.
//
// When the constant is a struct, a fresh addressable copy is created so that each
// invocation gets its own mutable value and pointer-receiver methods can be called.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand slots and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLoadGeneralConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.GeneralConstants) {
		vMBoundsError(vm, frame, boundsTableGeneralConstant, int(index), len(frame.Function.GeneralConstants))
		return opPanicError
	}
	v := frame.Function.GeneralConstants[index]

	if v.Kind() == reflect.Struct || v.Kind() == reflect.Array {
		v = copyReflectValueArenaWithVM(vm.Arena, vm, v)
	}
	registers.General[instruction.A] = v
	return opContinue
}

// handleLoadCompositeZeroReuse loads a composite-zero constant, reusing the destination
// register's storage when it already holds a same-typed addressable value.
//
// Takes vm (*VM) which supplies the arena for the fallback copy.
// Takes frame (*CallFrame) which carries the constant pool.
// Takes registers (*Registers) which holds the destination.
// Takes instruction (instruction) which encodes the destination and constant index.
//
// Returns OpResult indicating the next execution step.
func handleLoadCompositeZeroReuse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := instruction.WideIndex()
	if int(index) >= len(frame.Function.GeneralConstants) {
		vMBoundsError(vm, frame, boundsTableGeneralConstant, int(index), len(frame.Function.GeneralConstants))
		return opPanicError
	}
	constant := frame.Function.GeneralConstants[index]
	if existing := registers.General[instruction.A]; existing.IsValid() && existing.CanSet() &&
		existing.Type() == constant.Type() {
		existing.SetZero()
		return opContinue
	}
	registers.General[instruction.A] = copyReflectValueArenaWithVM(vm.Arena, vm, constant)
	return opContinue
}

// handleLoadNil loads an invalid reflect.Value representing nil into a general register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLoadNil(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = reflect.Value{}
	return opContinue
}

// handleLoadZero stores the zero value for the register kind specified by instruction.b
// into the destination register.
//
// Takes registers (*Registers) which provides all typed register banks.
// Takes instruction (instruction) which encodes the operand slots and the register kind
// in operand B.
//
// Returns OpResult indicating the next execution step.
func handleLoadZero(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	switch isa.RegisterKind(instruction.B) {
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
	case isa.RegisterSliceInt:
		registers.SlicesInt[instruction.A] = nil
	case isa.RegisterSliceFloat:
		registers.slicesFloat[instruction.A] = nil
	case isa.RegisterSliceString:
		registers.slicesString[instruction.A] = nil
	case isa.RegisterSliceBool:
		registers.slicesBool[instruction.A] = nil
	case isa.RegisterSliceUint:
		registers.slicesUint[instruction.A] = nil
	case isa.RegisterSliceByte:
		registers.slicesByte[instruction.A] = nil
	default:
	}
	return opContinue
}

// handleAddInt performs signed integer addition of two register operands in the virtual
// machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] + registers.Ints[instruction.C]
	return opContinue
}

// handleSubInt performs signed integer subtraction of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSubInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] - registers.Ints[instruction.C]
	return opContinue
}

// handleMulInt performs signed integer multiplication of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMulInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] * registers.Ints[instruction.C]
	return opContinue
}

// handleDivInt performs signed integer division of two register operands in the virtual
// machine.
//
// When the divisor is zero, raises an interpreted integer-divide-by-zero panic instead of
// continuing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleDivInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Ints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] / divisor
	return opContinue
}

// handleRemInt computes the signed integer remainder of two register operands in the
// virtual machine.
//
// When the divisor is zero, raises an interpreted integer-divide-by-zero panic instead of
// continuing.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult which signals the VM dispatch loop to continue, or the result of
// raising an interpreted divide-by-zero panic when the divisor register holds zero.
func handleRemInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	divisor := registers.Ints[instruction.C]
	if divisor == 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(integerDivideByZeroMessage))
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] % divisor
	return opContinue
}

// handleNegInt negates a signed integer register value in the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNegInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = -registers.Ints[instruction.B]
	return opContinue
}

// handleBitAnd performs a bitwise AND of two signed integer register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAnd(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] & registers.Ints[instruction.C]
	return opContinue
}

// handleBitOr performs a bitwise OR of two signed integer register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitOr(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] | registers.Ints[instruction.C]
	return opContinue
}

// handleBitXor performs a bitwise XOR of two signed integer register operands in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitXor(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] ^ registers.Ints[instruction.C]
	return opContinue
}

// handleBitAndNot performs a bitwise AND NOT of two signed integer register operands in
// the virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitAndNot(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] &^ registers.Ints[instruction.C]
	return opContinue
}

// handleBitNot performs a bitwise complement of a signed integer register value in the
// virtual machine.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBitNot(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = ^registers.Ints[instruction.B]
	return opContinue
}

// handleShiftLeft performs a left bit shift of a signed integer register by the amount in
// another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftLeft(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] << uint(registers.Ints[instruction.C]) //nolint:gosec // register shift
	return opContinue
}

// handleShiftRight performs a right bit shift of a signed integer register by the amount
// in another register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, value, and shift-amount
// register indices.
//
// Returns OpResult indicating the next execution step.
func handleShiftRight(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.Ints[instruction.B] >> uint(registers.Ints[instruction.C]) //nolint:gosec // register shift
	return opContinue
}

// handleSubIntConst subtracts a constant pool integer from a register value in the
// virtual machine.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleSubIntConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] - frame.Function.IntConstants[instruction.C]
	return opContinue
}

// handleAddIntConst adds a constant pool integer to a register value in the virtual
// machine.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleAddIntConst(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] + frame.Function.IntConstants[instruction.C]
	return opContinue
}

// handleLeIntConstJumpFalse compares a register against an integer constant and branches
// when the less-or-equal condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLeIntConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] > constantValue)
}

// handleLtIntConstJumpFalse compares a register against an integer constant and branches
// when the less-than condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleLtIntConstJumpFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] >= constantValue)
}

// handleLtIntJumpFalse compares two register operands and branches by the extension-word
// offset when the less-than condition is false. Fuses isa.OpLtInt + isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (instruction) which encodes the operand registers in B and C.
//
// Returns OpResult indicating the next execution step.
func handleLtIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] >= registers.Ints[instruction.C])
}

// handleLeIntJumpFalse compares two register operands and branches by the extension-word
// offset when the less-or-equal condition is false. Fuses isa.OpLeInt +
// isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the integer operands.
// Takes instruction (instruction) which encodes the operand register indices in B and C.
//
// Returns OpResult indicating the next execution step.
func handleLeIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] > registers.Ints[instruction.C])
}

// handleGtIntJumpFalse compares two register operands and branches by the extension-word
// offset when the greater-than condition is false. Fuses isa.OpGtInt + isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the integer operands.
// Takes instruction (instruction) which encodes the operand register indices in B and C.
//
// Returns OpResult indicating the next execution step.
func handleGtIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] <= registers.Ints[instruction.C])
}

// handleGeIntJumpFalse compares two register operands and branches by the extension-word
// offset when the greater-or-equal condition is false. Fuses isa.OpGeInt +
// isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the integer operands.
// Takes instruction (instruction) which encodes the operand register indices in B and C.
//
// Returns OpResult indicating the next execution step.
func handleGeIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] < registers.Ints[instruction.C])
}

// handleEqIntJumpFalse compares two register operands and branches by the extension-word
// offset when the equality condition is false. Fuses isa.OpEqInt + isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the integer operands.
// Takes instruction (instruction) which encodes the operand register indices in B and C.
//
// Returns OpResult indicating the next execution step.
func handleEqIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] != registers.Ints[instruction.C])
}

// handleNeIntJumpFalse compares two register operands and branches by the extension-word
// offset when the inequality condition is false. Fuses isa.OpNeInt + isa.OpJumpIfFalse.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
// Takes registers (*Registers) which holds the integer operands.
// Takes instruction (instruction) which encodes the operand register indices in B and C.
//
// Returns OpResult indicating the next execution step.
func handleNeIntJumpFalse(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return conditionalJump(frame, registers.Ints[instruction.B] == registers.Ints[instruction.C])
}

// handleAddFloat performs floating-point addition of two register operands in the virtual
// machine.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAddFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = registers.Floats[instruction.B] + registers.Floats[instruction.C]
	return opContinue
}

// handleSubFloat performs floating-point subtraction of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSubFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = registers.Floats[instruction.B] - registers.Floats[instruction.C]
	return opContinue
}

// handleMulFloat performs floating-point multiplication of two register operands in the
// virtual machine.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMulFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = registers.Floats[instruction.B] * registers.Floats[instruction.C]
	return opContinue
}

// handleDivFloat performs floating-point division of two register operands in the virtual
// machine.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleDivFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = registers.Floats[instruction.B] / registers.Floats[instruction.C]
	return opContinue
}

// handleNegFloat negates a floating-point register value in the virtual machine.
//
// Takes registers (*Registers) which provides the float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNegFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = -registers.Floats[instruction.B]
	return opContinue
}

// handleConcatString concatenates two string register values using the arena allocator.
//
// Takes vm (*VM) which provides access to the arena allocator.
// Takes registers (*Registers) which provides the string register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleConcatString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a := registers.Strings[instruction.B]
	b := registers.Strings[instruction.C]
	if vm.Limits.MaxStringSize > 0 && len(a)+len(b) > vm.Limits.MaxStringSize {
		vm.evalError = fmt.Errorf("%w: concat result %d bytes exceeds limit %d",
			errStringLimit, len(a)+len(b), vm.Limits.MaxStringSize)
		return opPanicError
	}
	registers.Strings[instruction.A] = ArenaConcatString(vm.Arena, a, b)
	return opContinue
}

// handleLenString computes the byte length of a string register value and stores it as an
// integer.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLenString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(len(registers.Strings[instruction.B]))
	return opContinue
}

// handleAdd performs addition on two general register operands using reflection-based
// type dispatch.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleAdd(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a, b := registers.General[instruction.B], registers.General[instruction.C]
	registers.General[instruction.A] = ReflectBinaryOp(a, b, func(x, y int64) int64 { return x + y },
		func(x, y float64) float64 { return x + y }, func(x, y string) string { return x + y }, nil)
	return opContinue
}

// handleSub performs subtraction on two general register operands using reflection-based
// type dispatch.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSub(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a, b := registers.General[instruction.B], registers.General[instruction.C]
	registers.General[instruction.A] = ReflectBinaryOp(a, b, func(x, y int64) int64 { return x - y },
		func(x, y float64) float64 { return x - y }, nil, nil)
	return opContinue
}

// handleMul performs multiplication on two general register operands using
// reflection-based type dispatch.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMul(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a, b := registers.General[instruction.B], registers.General[instruction.C]
	registers.General[instruction.A] = ReflectBinaryOp(a, b, func(x, y int64) int64 { return x * y },
		func(x, y float64) float64 { return x * y }, nil, nil)
	return opContinue
}

// handleDiv performs division on two general register operands using reflection-based
// type dispatch.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleDiv(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a, b := registers.General[instruction.B], registers.General[instruction.C]
	registers.General[instruction.A] = ReflectBinaryOp(a, b, func(x, y int64) int64 { return x / y },
		func(x, y float64) float64 { return x / y }, nil, func(x, y uint64) uint64 { return x / y })
	return opContinue
}

// handleRem computes the remainder of two general register operands using
// reflection-based type dispatch.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRem(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	a, b := registers.General[instruction.B], registers.General[instruction.C]
	registers.General[instruction.A] = ReflectBinaryOp(a, b, func(x, y int64) int64 { return x % y }, nil, nil, func(x, y uint64) uint64 { return x % y })
	return opContinue
}

// handleEqInt tests equality of two signed integer register values and stores the boolean
// result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] == registers.Ints[instruction.C])
	return opContinue
}

// handleNeInt tests inequality of two signed integer register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNeInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] != registers.Ints[instruction.C])
	return opContinue
}

// handleLtInt tests whether the first signed integer register is less than the second and
// stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLtInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] < registers.Ints[instruction.C])
	return opContinue
}

// handleLeInt tests whether the first signed integer register is less than or equal to
// the second and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLeInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] <= registers.Ints[instruction.C])
	return opContinue
}

// handleGtInt tests whether the first signed integer register is greater than the second
// and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGtInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] > registers.Ints[instruction.C])
	return opContinue
}

// handleGeInt tests whether the first signed integer register is greater than or equal to
// the second and stores the result.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGeInt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] >= registers.Ints[instruction.C])
	return opContinue
}

// handleEqFloat tests equality of two floating-point register values and stores the
// boolean result as an int.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] == registers.Floats[instruction.C])
	return opContinue
}

// handleLtFloat tests whether the first float register is less than the second and stores
// the result as an int.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLtFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] < registers.Floats[instruction.C])
	return opContinue
}

// handleLeFloat tests whether the first float register is less than or equal to the
// second and stores the result.
//
// Takes registers (*Registers) which provides the float and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLeFloat(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Floats[instruction.B] <= registers.Floats[instruction.C])
	return opContinue
}

// handleEqString tests equality of two string register values and stores the boolean
// result as an int.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] == registers.Strings[instruction.C])
	return opContinue
}

// handleLtString tests whether the first string register is lexicographically less than
// the second and stores the result.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLtString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] < registers.Strings[instruction.C])
	return opContinue
}

// handleLeString tests whether the first string register is lexicographically less than
// or equal to the second.
//
// Takes registers (*Registers) which provides the string and integer register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLeString(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Strings[instruction.B] <= registers.Strings[instruction.C])
	return opContinue
}

// handleEqInterfaceNil sets ints[A] = 1 when general[B] is the zero-value interface (no
// dynamic type, no dynamic value), and 0 otherwise. This matches Go's "interface holding
// typed nil != nil" rule that would otherwise be lost in reflectEqual's typed-nil fast
// path.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination int register in operand A
// and the source general register in operand B.
//
// Returns OpResult indicating the next execution step.
func handleEqInterfaceNil(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.Ints[instruction.B] = 1
		return opContinue
	}
	if value.Kind() == reflect.Interface && value.IsNil() {
		registers.Ints[instruction.B] = 1
		return opContinue
	}
	registers.Ints[instruction.B] = 0
	return opContinue
}

// handleNeInterfaceNil is the != mirror of handleEqInterfaceNil.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination int register in operand A
// and the source general register in operand B.
//
// Returns OpResult indicating the next execution step.
func handleNeInterfaceNil(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	value := registers.General[instruction.C]
	if !value.IsValid() {
		registers.Ints[instruction.B] = 0
		return opContinue
	}
	if value.Kind() == reflect.Interface && value.IsNil() {
		registers.Ints[instruction.B] = 0
		return opContinue
	}
	registers.Ints[instruction.B] = 1
	return opContinue
}

// handleEqGeneral compares two general-bank values for equality under Go's == semantics.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the operands and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleEqGeneral(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	equal, offending := interfaceValuesEqual(
		registers.General[instruction.B], registers.General[instruction.C], true)
	if offending.IsValid() {
		return raiseUncomparablePanic(vm, offending)
	}
	registers.Ints[instruction.A] = boolToInt64(equal)
	return opContinue
}

// handleSubOpEqInterfaceStrict implements isa.SubOpEqInterfaceStrict: general[B] ==
// general[C] with strict interface equality, where a nil interface equals only a nil
// interface. The destination int register travels in the trailing isa.OpExt word.
//
// Takes vm (*VM) which raises the uncomparable panic.
// Takes frame (*CallFrame) which supplies and advances past the extension word.
// Takes registers (*Registers) which holds the operands and the int result.
// Takes instruction (instruction) which encodes the two general operand registers.
//
// Returns OpResult which continues, or the panic result for uncomparable operands.
func handleSubOpEqInterfaceStrict(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	equal, offending := interfaceValuesEqual(
		registers.General[instruction.B], registers.General[instruction.C], false)
	if offending.IsValid() {
		return raiseUncomparablePanic(vm, offending)
	}
	registers.Ints[extensionWord.A] = boolToInt64(equal)
	return opContinue
}

// handleSubOpNeInterfaceStrict is the != mirror of handleSubOpEqInterfaceStrict.
//
// Takes vm (*VM) which raises the uncomparable panic.
// Takes frame (*CallFrame) which supplies and advances past the extension word.
// Takes registers (*Registers) which holds the operands and the int result.
// Takes instruction (instruction) which encodes the two general operand registers.
//
// Returns OpResult which continues, or the panic result for uncomparable operands.
func handleSubOpNeInterfaceStrict(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	equal, offending := interfaceValuesEqual(
		registers.General[instruction.B], registers.General[instruction.C], false)
	if offending.IsValid() {
		return raiseUncomparablePanic(vm, offending)
	}
	registers.Ints[extensionWord.A] = boolToInt64(!equal)
	return opContinue
}

// isNilableAndNil reports whether v is a nil-able kind (func, pointer, interface, slice,
// map, channel) and currently holds a nil value.
//
// Takes v (reflect.Value) which is the value to inspect for nil-ability and nil state.
//
// Returns true if v is a nil-able kind and currently nil, false otherwise.
func isNilableAndNil(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Func, reflect.Pointer, reflect.Interface,
		reflect.Slice, reflect.Map, reflect.Chan:
		return v.IsNil()
	default:
	}
	return false
}

// handleLtGeneral tests whether the first general register is less than the second using
// reflection comparison.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLtGeneral(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(reflectCompare(registers.General[instruction.B], registers.General[instruction.C]) < 0)
	return opContinue
}

// handleLeGeneral tests whether the first general register is less than or equal to the
// second using reflection.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleLeGeneral(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(reflectCompare(registers.General[instruction.B], registers.General[instruction.C]) <= 0)
	return opContinue
}

// handleGtGeneral tests whether the first general register is greater than the second
// using reflection comparison.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGtGeneral(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(reflectCompare(registers.General[instruction.B], registers.General[instruction.C]) > 0)
	return opContinue
}

// handleGeGeneral tests whether the first general register is greater than or equal to
// the second using reflection.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleGeGeneral(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(reflectCompare(registers.General[instruction.B], registers.General[instruction.C]) >= 0)
	return opContinue
}

// handleNot performs a logical NOT on an integer register, storing 1 if the value is zero
// and 0 otherwise.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleNot(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = boolToInt64(registers.Ints[instruction.B] == 0)
	return opContinue
}
