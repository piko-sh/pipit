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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// readTier0StructFieldLayout looks up the structLayoutTable entry at the given uint8
// index. The index is range-checked against the function's StructLayoutTable; an
// out-of-range index panics with a diagnostic message, which the dispatch loop's recover
// converts into an interpreted panic rather than crashing the host.
//
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes index (uint8) which is the structLayoutTable index.
//
// Returns the StructFieldLayout entry.
func readTier0StructFieldLayout(frame *CallFrame, index uint8) program.StructFieldLayout {
	if int(index) >= len(frame.Function.StructLayoutTable) {
		panicTier0LayoutIndexOutOfRange(frame, index)
	}
	return frame.Function.StructLayoutTable[index]
}

// panicTier0LayoutIndexOutOfRange raises the diagnostic panic for an out-of-range tier-0
// layout index. It is split out and marked noinline so the fmt.Sprintf cost stays off
// readTier0StructFieldLayout's hot path, keeping that lookup small enough for the
// Compiler to inline it into every tier-0 struct-field handler.
//
// Takes frame (*CallFrame) which provides the layout table and the diagnostic context.
// Takes index (uint8) which is the offending structLayoutTable index.
//
//go:noinline
func panicTier0LayoutIndexOutOfRange(frame *CallFrame, index uint8) {
	panic(newInvariantError(
		"tier-0 struct field layout index %d out of range (table size %d); pc=%d funcName=%s",
		index, len(frame.Function.StructLayoutTable),
		frame.ProgramCounter, frame.Function.Name,
	))
}

// panicTier0UnexpectedFieldKind raises a diagnostic panic when a tier-0 struct-field
// handler reaches a field whose kind its opcode is never emitted for (safe build).
// Failing loudly keeps such a Compiler contract violation from silently leaving a
// destination undefined.
//
// Takes frame (*CallFrame) which provides the diagnostic context.
// Takes op (string) which names the offending opcode.
// Takes got (reflect.Kind) which is the field kind actually encountered.
//
//go:noinline
func panicTier0UnexpectedFieldKind(frame *CallFrame, op string, got reflect.Kind) {
	panic(newInvariantError(
		"tier-0 %s reached a %s field (compiler contract violation); pc=%d funcName=%s",
		op, got, frame.ProgramCounter, frame.Function.Name,
	))
}

// handleGetStructFieldGeneralT0 reads a pointer or interface struct field.
//
// Safe-build implementation walks via reflect.Field, matching the unsafe-build's
// reflect-walk fallback semantics. The interface-leaf unwrap is shared via
// unwrapInterfaceLeaf so both builds produce identical observable behaviour.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldGeneralT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	field, ok := structFieldReflectRead(registers, frame, instruction.B, layout)
	if !ok {
		if result := structFieldFallbackMiss(vm, frame, registers, instruction.B); result != opContinue {
			return result
		}
		registers.General[instruction.A] = reflect.Value{}
		return opContinue
	}

	if !field.CanInterface() && field.CanAddr() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	if field.Kind() == reflect.Slice && field.CanAddr() {
		buffer := vm.acquireSliceSnapshot()
		*buffer = *(*snapshotSliceHeader)(ReflectValuePtr(field))
		registers.General[instruction.A] = unsafeReadOnlyValue(reflectValueABIType(field.Type()), unsafe.Pointer(buffer), reflect.Slice)
		return opContinue
	}
	if field.CanAddr() {
		switch field.Kind() {
		case reflect.Map, reflect.Chan, reflect.Func:
			value := *(*unsafe.Pointer)(ReflectValuePtr(field))
			registers.General[instruction.A] = unsafeDirectIfaceKindValue(reflectValueABIType(field.Type()), value, field.Kind())
			return opContinue
		}
	}
	registers.General[instruction.A] = launderReadOnlyFieldValue(snapshotPointerLeaf(unwrapInterfaceLeaf(field)))
	return opContinue
}

// handleGetStructFieldRawPointerT0 is the safe-build counterpart that delegates to the
// general path because the safe build has no access to the eface header.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldRawPointerT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return handleGetStructFieldGeneralT0(vm, frame, registers, instruction)
}

// snapshotPointerLeaf detaches a pointer-typed reflect.Value from any live storage
// backing it (safe-build mirror of the unsafe-build helper). See
// vm_handler_struct_field_tier0_unsafe.go for the full rationale; without it `removed :=
// cache.tail` retains a live reference into cache.tail's storage and subsequent writes to
// cache.tail silently mutate `removed`.
//
// Takes field (reflect.Value) which is the leaf value just read.
//
// Returns a detached snapshot for pointer-kind values.
// Returns the original value unchanged for non-pointer kinds.
func snapshotPointerLeaf(field reflect.Value) reflect.Value {
	if !field.IsValid() {
		return field
	}
	if field.Kind() != reflect.Pointer {
		return field
	}
	if !field.CanAddr() {

		pointer := field.UnsafePointer()
		if pointer == nil {
			return field
		}
		return reflect.NewAt(field.Type().Elem(), pointer)
	}
	pointer := field.UnsafePointer()
	return reflect.NewAt(field.Type().Elem(), pointer)
}

// unwrapInterfaceLeaf normalises an interface leaf to its held value.
//
// Mirrors the cycle-broken interface unwrap branch in handleGetField so subsequent field
// accesses see the user's pointer/struct directly. Returns the zero Value for a nil-held
// interface so downstream nil checks in opTestNil keep working.
//
// Takes field (reflect.Value) which is the leaf value just read.
//
// Returns the unwrapped value when field is a non-nil interface.
// Returns field unchanged otherwise.
func unwrapInterfaceLeaf(field reflect.Value) reflect.Value {
	if field.Kind() == reflect.Interface {
		if field.IsNil() {
			return reflect.Value{}
		}
		return field.Elem()
	}
	return field
}

// handleSetStructFieldGeneralT0 writes a pointer or interface struct field.
//
// Operand A=general structReg, B=general valueReg, C=structLayoutTable index. Safe-build
// implementation matches the unsafe-build's slow-path semantics (reflect.Field walk) for
// parity testing.
//
// Takes vm (*VM) which supports coerceValue's closure coercion.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleSetStructFieldGeneralT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	value := registers.General[instruction.B]
	field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
	if !walkOK {
		return structFieldFallbackMiss(vm, frame, registers, instruction.A)
	}
	if !field.CanSet() && field.CanAddr() {
		field = reflect.NewAt(field.Type(), field.Addr().UnsafePointer()).Elem()
	}
	if !value.IsValid() {
		field.SetZero()
		return opContinue
	}
	coerced := coerceValue(vm, value, field.Type())

	if field.Type().Kind() == reflect.Interface && field.Type().NumMethod() > 0 &&
		coerced.IsValid() && !coerced.Type().Implements(field.Type()) {
		if adapted := tryBuildInterfaceAdapter(vm, coerced, field.Type(), argumentTypeContext{}); adapted.IsValid() {
			coerced = adapted
		}
	}
	if err := checkInterfaceFieldValue(vm, coerced, field.Type()); err != nil {
		vm.evalError = err
		return opPanicError
	}
	field.Set(coerced)
	return opContinue
}

// handleCopyStructFieldGeneralT0 is the safe-build counterpart of the fused general-bank
// field-to-field copy.
//
// The unsafe build does this with one runtime.typedmemmove between the two field
// pointers; the safe build cannot poke runtime internals, so it walks the source via
// reflect and writes to the destination via the same reflect path the regular SET handler
// uses. Slower than the unsafe variant but observably identical, keeping the build-tag
// parity tests happy.
//
// Operand A=sourceRecv, B=destinationRecv, C=sourceLayoutIndex. The following isa.OpExt
// word carries A=destinationLayoutIndex.
//
// Takes vm (*VM) which supports coerceValue's closure coercion.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue once the source field is copied into the destination field.
func handleCopyStructFieldGeneralT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sourceLayout := readTier0StructFieldLayout(frame, instruction.C)
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	destinationLayout := readTier0StructFieldLayout(frame, extension.A)
	sourceInstr := instruction
	sourceInstr.B = instruction.A
	sourceField, sourceOk := structFieldReflectWrite(registers, frame, sourceInstr.B, sourceLayout)
	if !sourceOk {
		return structFieldFallbackMiss(vm, frame, registers, sourceInstr.B)
	}
	sourceValue := sourceField
	if sourceValue.Kind() == reflect.Pointer && sourceValue.CanAddr() {
		sourceValue = snapshotPointerLeaf(sourceValue)
	}
	destinationField, destinationOk := structFieldReflectWrite(registers, frame, instruction.B, destinationLayout)
	if !destinationOk {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	if !destinationField.CanSet() && destinationField.CanAddr() {
		destinationField = reflect.NewAt(destinationField.Type(), destinationField.Addr().UnsafePointer()).Elem()
	}
	if !sourceValue.IsValid() {
		destinationField.SetZero()
		return opContinue
	}
	coerced := coerceValue(vm, sourceValue, destinationField.Type())
	destinationField.Set(coerced)
	return opContinue
}

// handleGetStructFieldIndexGeneral reads one element of an array-typed struct field (safe
// build): general[A] = receiver[B].field(layout[C]).elem[ints[ext1.C]].
//
// Dispatches via structFieldReflectRead instead of unsafe.Pointer arithmetic, then
// mirrors the INDEX handler's bounds check and non-nil interface unwrap.
//
// Takes vm (*VM) which supplies panic plumbing for the bounds check.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the receiver, index, and destination.
// Takes instruction (instruction) which carries A=dest, B=receiver, C=layout index.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldIndexGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	indexRegister, _, _, _ := readFusedArrayFieldExtensions(frame)
	layout := readTier0StructFieldLayout(frame, instruction.C)
	field, ok := structFieldReflectRead(registers, frame, instruction.B, layout)
	if !ok {
		return structFieldFallbackMiss(vm, frame, registers, instruction.B)
	}
	index := int(registers.Ints[indexRegister])
	if boundsResult, inRange := checkSliceBounds(vm, field, index); !inRange {
		return boundsResult
	}
	element := field.Index(index)
	if element.Kind() == reflect.Interface && !element.IsNil() {
		element = element.Elem()
	}
	registers.General[instruction.A] = launderReadOnlyFieldValue(element)
	return opContinue
}
