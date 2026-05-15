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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// structLayoutAtWide looks up the StructLayoutTable entry at the given wide (uint16)
// index. The index is range-checked against the function's StructLayoutTable; an
// out-of-range index panics with a diagnostic message, which the dispatch loop's recover
// converts into an interpreted panic rather than crashing the host.
//
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes index (uint16) which is the wide structLayoutTable index.
//
// Returns the StructFieldLayout entry.
func structLayoutAtWide(frame *CallFrame, index uint16) program.StructFieldLayout {
	if int(index) >= len(frame.Function.StructLayoutTable) {
		panic(newInvariantError(
			"struct field layout index %d out of range (table size %d); pc=%d funcName=%s",
			index, len(frame.Function.StructLayoutTable),
			frame.ProgramCounter, frame.Function.Name,
		))
	}
	return frame.Function.StructLayoutTable[index]
}

// Reflect-based helpers shared between the unsafe and safe build variants of the
// struct-field fast path. The unsafe build uses these only as a fallback when the
// receiver struct is not addressable (e.g. global var literal, function-return value);
// the safe build uses them for every access.
//
// Walking via reflect.Value.Field at each level is slower than the unsafe pointer
// arithmetic but avoids the per-access struct rehome that would otherwise dominate
// hot-loop cost on non-addressable receivers without loop-invariant code motion.

// structFieldReflectRead walks the layout's path through a reflect.Value of the receiver
// struct and returns the leaf field as a reflect.Value. Auto-derefs pointers and unwraps
// interfaces.
//
// Takes registers (*Registers) which provides the general bank.
// Takes frame (*CallFrame) which is the active call frame.
// Takes generalRegister (uint8) which is the general register holding the source struct.
// Takes layout (StructFieldLayout) which carries the field path.
//
// Returns the leaf reflect.Value and true on success; (zero Value, false) when the source
// is invalid or not a struct.
func structFieldReflectRead(registers *Registers, frame *CallFrame, generalRegister uint8, layout program.StructFieldLayout) (reflect.Value, bool) {
	value := registers.General[generalRegister]
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	value = unwrapToStruct(value)
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}

	if int(layout.Path[0]) >= value.NumField() {
		rebuilt, ok := rebuildStructFromLayout(value, frame, layout)
		if !ok {
			return reflect.Value{}, false
		}
		value = rebuilt
	}
	for level := uint8(0); level < layout.PathLength; level++ {
		index := int(layout.Path[level])
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		if index >= value.NumField() {
			return reflect.Value{}, false
		}
		value = value.Field(index)
	}
	return value, true
}

// rebuildStructFromLayout constructs an addressable reflect.Value.
//
// Produces a value of the layout's declared struct type pointed at the same storage as
// value. Used when value's reflect.Type was erased by a native call (Go generic-method
// dispatch can return a *T whose reflect.Type reads back as `struct {}` because the type
// parameter was lost in the runtime type wrapper).
//
// Takes value (reflect.Value) whose UnsafePointer() locates the struct storage.
// Takes frame (*CallFrame) which carries the TypeTable lookup.
// Takes layout (StructFieldLayout) whose TypeIndex names the declared struct type.
//
// Returns the rebuilt reflect.Value and true on success; (zero Value, false) when the
// layout cannot be resolved.
func rebuildStructFromLayout(value reflect.Value, frame *CallFrame, layout program.StructFieldLayout) (reflect.Value, bool) {
	if frame == nil {
		return reflect.Value{}, false
	}
	if int(layout.TypeIndex) >= len(frame.Function.TypeTable) {
		return reflect.Value{}, false
	}
	structType := frame.Function.TypeTable[layout.TypeIndex]
	if structType == nil || structType.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	dataPtr := ReflectValuePtr(value)
	if dataPtr == nil {
		return reflect.Value{}, false
	}
	return reflect.NewAt(structType, dataPtr).Elem(), true
}

// unwrapToStruct strips Interface and Pointer wrappers in any order until value reaches a
// Struct or runs out of unwrap opportunities.
// Returns the zero Value when a nil pointer is encountered mid-walk.
//
// Map values typed `interface{}` arrive as Interface-wrapped Pointer (cycle-broken
// recursive structs become `map[string]interface{}` holding `*Self`); a flat one-pass
// through Pointer-then-Interface would miss that shape.
//
// Takes value (reflect.Value) which is the candidate to unwrap.
//
// Returns the innermost non-Interface/non-Pointer value, or zero Value if a nil pointer
// was traversed.
func unwrapToStruct(value reflect.Value) reflect.Value {
	for value.IsValid() {
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				return reflect.Value{}
			}
			value = value.Elem()
		case reflect.Interface:
			value = value.Elem()
		default:
			return value
		}
	}
	return value
}

// structFieldReceiverIsNilPointer reports whether the receiver is a nil pointer.
//
// Checks directly and through the same pointer/interface nesting unwrapToStruct()
// traverses. Only this shape must raise a nil-dereference panic.
//
// Takes value (reflect.Value) which is the receiver register's current value.
//
// Returns true when a field access through value would dereference a nil pointer.
func structFieldReceiverIsNilPointer(value reflect.Value) bool {
	for value.IsValid() {
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				return true
			}
			value = value.Elem()
		case reflect.Interface:
			value = value.Elem()
		default:
			return false
		}
	}
	return false
}

// raiseNilPointerDereference raises Go's nil-pointer dereference runtime panic as an
// interpreted panic that deferred recover() can observe, matching what native Go raises
// when a struct field is read or written through a nil receiver. Mirrors the message and
// unwind path handleDeref uses for explicit pointer dereferences.
//
// Takes vm (*VM) which carries the panic machinery.
//
// Returns OpResult which propagates the raised panic to the dispatch loop.
func raiseNilPointerDereference(vm *VM) OpResult {
	return raiseNativePanicAsInterpreted(vm, newRuntimePanicError(nilDereferenceMessage))
}

// alignFramePointerToFrame realigns vm.FramePointer with the frame the dispatcher is
// actually executing, before a struct-field handler raises an interpreted panic. The ASM
// trampoline hot path can leave FramePointer stale.
//
// Takes vm (*VM) whose FramePointer mirror is realigned.
// Takes frame (*CallFrame) which is the frame the raising handler was dispatched with.
func alignFramePointerToFrame(vm *VM, frame *CallFrame) {
	if vm.FramePointer >= 0 && vm.FramePointer < len(vm.CallStack) && &vm.CallStack[vm.FramePointer] == frame {
		return
	}
	for index := range vm.CallStack {
		if &vm.CallStack[index] == frame {
			vm.FramePointer = index
			return
		}
	}
}

// structFieldFallbackMiss converts a struct-field handler fallback failure into the
// correct outcome.
//
// A nil-pointer receiver raises the interpreted nil-dereference panic while every other
// unsupported shape keeps the historical silent skip. Called only after both the unsafe
// base resolution and the reflect walk have declined.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes frame (*CallFrame) which is the executing frame, used to realign the
// vm.FramePointer mirror before unwinding (see alignFramePointerToFrame).
// Takes registers (*Registers) which provides the general bank.
// Takes receiverRegister (uint8) which is the general register holding the receiver.
//
// Returns OpResult which is the raised panic for nil-pointer receivers, opContinue
// otherwise.
func structFieldFallbackMiss(vm *VM, frame *CallFrame, registers *Registers, receiverRegister uint8) OpResult {
	if structFieldReceiverIsNilPointer(registers.General[receiverRegister]) {
		alignFramePointerToFrame(vm, frame)
		return raiseNilPointerDereference(vm)
	}
	return opContinue
}

// structFieldReflectWrite walks the layout's path to the destination field, rehydrating
// the register from the TypeTable when it holds an invalid value.
//
// Takes registers (*Registers) which provides the general bank.
// Takes frame (*CallFrame) which carries the TypeTable.
// Takes generalRegister (uint8) which is the general register holding the struct.
// Takes layout (StructFieldLayout) which carries the field path and TypeTable index.
//
// Returns the addressable leaf reflect.Value and true on success; (zero Value, false)
// when the source is invalid or not a struct.
func structFieldReflectWrite(registers *Registers, frame *CallFrame, generalRegister uint8, layout program.StructFieldLayout) (reflect.Value, bool) {
	value, ok := ensureStructFieldRegisterValue(registers, frame, generalRegister, layout)
	if !ok {
		return reflect.Value{}, false
	}
	value, ok = makeStructAddressable(registers, generalRegister, value)
	if !ok {
		return reflect.Value{}, false
	}
	for level := uint8(0); level < layout.PathLength; level++ {
		index := int(layout.Path[level])
		if value.Kind() != reflect.Struct || index >= value.NumField() {
			return reflect.Value{}, false
		}
		value = value.Field(index)
	}

	if !value.CanSet() && value.CanAddr() {
		value = unsafeNewAt(reflectValueABIType(value.Type()), ReflectValuePtr(value), value.Kind())
	}
	return value, true
}

// ensureStructFieldRegisterValue returns the receiver register value.
//
// Lazily allocates a zero struct of the declared type when the slot is invalid. Returns
// false when the slot has no usable struct backing.
//
// Takes registers (*Registers) which provides the general bank.
// Takes frame (*CallFrame) which carries the TypeTable used to rehydrate invalid
// receivers.
// Takes generalRegister (uint8) which is the register slot index.
// Takes layout (StructFieldLayout) which carries the declared TypeTable index.
//
// Returns reflect.Value which is the receiver struct value.
// Returns bool which is true when the slot is usable.
func ensureStructFieldRegisterValue(registers *Registers, frame *CallFrame, generalRegister uint8, layout program.StructFieldLayout) (reflect.Value, bool) {
	value := registers.General[generalRegister]
	if value.IsValid() {
		return value, true
	}
	if frame == nil || int(layout.TypeIndex) >= len(frame.Function.TypeTable) {
		return reflect.Value{}, false
	}
	structType := frame.Function.TypeTable[layout.TypeIndex]
	if structType == nil {
		return reflect.Value{}, false
	}
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	value = reflect.New(structType).Elem()
	registers.General[generalRegister] = value
	return value, true
}

// makeStructAddressable promotes value to an addressable struct.
//
// Peels pointers and interfaces off value and updates the register slot so subsequent
// field traversal can use CanAddr. Returns false when value cannot be normalised to an
// addressable struct.
//
// Takes registers (*Registers) which provides the general bank.
// Takes generalRegister (uint8) which is the register slot index.
// Takes value (reflect.Value) which is the receiver to normalise.
//
// Returns reflect.Value which is the addressable struct value.
// Returns bool which is true when normalisation succeeded.
func makeStructAddressable(registers *Registers, generalRegister uint8, value reflect.Value) (reflect.Value, bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	if !value.CanAddr() {
		addressable := reflect.New(value.Type()).Elem()
		addressable.Set(value)
		value = addressable
		registers.General[generalRegister] = value
	}
	return value, true
}

// readFusedArrayFieldExtensions consumes the two isa.OpExt words that follow the fused
// array-field index opcodes.
//
// Takes frame (*CallFrame) which provides the bytecode body and PC.
//
// Returns the index register, array length, element size, and element kind.
func readFusedArrayFieldExtensions(frame *CallFrame) (uint8, int, uintptr, reflect.Kind) {
	body := frame.Function.Body
	ext1 := body[frame.ProgramCounter]
	ext2 := body[frame.ProgramCounter+1]
	frame.ProgramCounter += 2
	arrayLength := int(uint16(ext1.A) | uint16(ext1.B)<<extensionWideIndexHighByteShift)
	elementSize := uintptr(uint16(ext2.A) | uint16(ext2.B)<<extensionWideIndexHighByteShift)
	return ext1.C, arrayLength, elementSize, reflect.Kind(ext2.C)
}

// readFusedSliceFieldExtensions decodes the two extension words of
// isa.OpGetStructFieldSliceIndexScalar and advances the frame's program counter past
// them.
//
// Takes frame (*CallFrame) which provides the bytecode body and program counter.
//
// Returns the index int-register, element byte size, sub-field byte offset, and the
// destination isa.RegisterKind.
func readFusedSliceFieldExtensions(frame *CallFrame) (indexRegister uint8, elementSize uintptr, subFieldOffset uintptr, destinationKind isa.RegisterKind) {
	body := frame.Function.Body
	ext1 := body[frame.ProgramCounter]
	ext2 := body[frame.ProgramCounter+1]
	frame.ProgramCounter += 2
	elementSize = uintptr(uint16(ext1.A) | uint16(ext1.B)<<extensionWideIndexHighByteShift)
	subFieldOffset = uintptr(uint16(ext2.A) | uint16(ext2.B)<<extensionWideIndexHighByteShift)
	return ext1.C, elementSize, subFieldOffset, isa.RegisterKind(ext2.C)
}

// storeScalarSubFieldFromReflect reads the scalar sub-field at the given byte offset of a
// struct element reflect.Value and stores it into the destination register bank. Used by
// the reflect fallback arms of isa.OpGetStructFieldSliceIndexScalar, resolving the
// sub-field by matching its byte offset against the element type's fields.
//
// Defined on the scalar banks a struct sub-field can occupy. The default leaves the
// register alone and the caller falls back to the generic field read.
//
// Takes registers (*Registers) which provides the destination banks.
// Takes element (reflect.Value) which is the struct element to read.
// Takes destination (uint8) which is the destination register index.
// Takes subFieldOffset (uintptr) which is the sub-field's byte offset within the element.
// Takes subKind (isa.RegisterKind) which names the destination bank.
func storeScalarSubFieldFromReflect(registers *Registers, element reflect.Value, destination uint8, subFieldOffset uintptr, subKind isa.RegisterKind) {
	elementType := element.Type()
	for i := range elementType.NumField() {
		if elementType.Field(i).Offset != subFieldOffset {
			continue
		}
		field := element.Field(i)
		switch subKind {
		case isa.RegisterInt:
			registers.Ints[destination] = field.Int()
		case isa.RegisterFloat:
			registers.Floats[destination] = field.Float()
		case isa.RegisterUint:
			registers.Uints[destination] = field.Uint()
		case isa.RegisterBool:
			registers.Bools[destination] = field.Bool()
		default:
		}
		return
	}
}
