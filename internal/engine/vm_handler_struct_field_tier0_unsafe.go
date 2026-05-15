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
	// abiTypeKindOffset is the byte offset of abi.Type.Kind_ within the runtime's abi.Type
	// header (Size_=8 + PtrBytes=8 + Hash=4 + TFlag=1 + Align_=1 + FieldAlign_=1 = 23). The
	// low 5 bits of the byte encode reflect.Kind.
	abiTypeKindOffset = program.AbiTypeKindByteOffset

	// efaceDataPointerOffsetMoved documents that the constant now lives in function.go
	// beside the other neutral abi offsets so safe builds compile. efaceDataPointerOffset is
	// the byte offset of the data pointer within Go's eface header (one uintptr-sized type
	// pointer precedes the data pointer).
)

// readTier0StructFieldLayout looks up the structLayoutTable entry at instruction.c (uint8
// index). The index is range-checked against the function's structLayoutTable; an
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
// handler reaches a field whose kind its opcode is never emitted for, which can only
// happen through a Compiler contract violation. Failing loudly keeps such a regression
// from silently leaving a destination register undefined.
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
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldGeneralT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	if int(layout.FieldTypeIndex) < len(frame.Function.TypeTable) {
		if result, handled := tryGetStructFieldUnsafe(vm, frame, registers, instruction, layout); handled {
			return result
		}
	}
	return getStructFieldGeneralFallback(vm, frame, registers, instruction, layout)
}

// tryGetStructFieldUnsafe attempts the unsafe Pointer-kind hot path for
// opGetStructFieldGeneralT0.
//
// Interface-kind layouts are safe via the unsafe path because tryResolveStructFieldLayout
// rejects generic-erased receiver types at compile time; every Interface layout reaching
// this handler has a runtime struct whose field really is a 16-byte eface at the cached
// offset (cycle-broken *Self or genuine interface field).
//
// Takes vm (*VM) which provides arena access for slice snapshots.
// Takes frame (*CallFrame) which supplies the type table.
// Takes registers (*Registers) which holds the receiver and destination banks.
// Takes instruction (instruction) whose operands encode A=dest, B=receiver, C=layout
// index.
// Takes layout (StructFieldLayout) which is the pre-resolved layout entry.
//
// Returns OpResult and handled=true when the unsafe path completed (success or empty
// receiver); handled=false routes through the reflect-walk fallback.
func tryGetStructFieldUnsafe(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, layout program.StructFieldLayout) (OpResult, bool) {
	base, ok := structFieldReceiverBase(registers, instruction.B)
	fieldType := frame.Function.TypeTable[layout.FieldTypeIndex]
	if !ok || fieldType == nil {
		return opContinue, false
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	kind := reflect.Kind(layout.Kind)
	switch kind {
	case reflect.Pointer:
		rawPtr := *(*unsafe.Pointer)(fieldPointer)
		registers.General[instruction.A] = unsafePointerKindValue(reflectValueABIType(fieldType), rawPtr)
		return opContinue, true
	case reflect.Interface:
		if !layoutStructMatchesReceiver(frame, registers, instruction.B, layout) {
			return opContinue, false
		}
		if result, handled := tryGetStructInterfaceField(registers, instruction.A, fieldPointer); handled {
			return result, true
		}
	case reflect.Slice:
		buffer := vm.acquireSliceSnapshot()
		*buffer = *(*snapshotSliceHeader)(fieldPointer)
		registers.General[instruction.A] = unsafeReadOnlyValue(reflectValueABIType(fieldType), unsafe.Pointer(buffer), reflect.Slice)
		return opContinue, true
	case reflect.Map, reflect.Chan, reflect.Func:
		value := *(*unsafe.Pointer)(fieldPointer)
		registers.General[instruction.A] = unsafeDirectIfaceKindValue(reflectValueABIType(fieldType), value, kind)
		return opContinue, true
	default:
	}
	field := unsafeNewAt(reflectValueABIType(fieldType), fieldPointer, fieldType.Kind())
	registers.General[instruction.A] = snapshotPointerLeaf(unwrapInterfaceLeaf(field))
	return opContinue, true
}

// layoutStructMatchesReceiver reports whether the receiver holds the struct type the
// layout was built for.
//
// Guards the unsafe interface-field fast paths against treating a host struct through a
// layout built from the Compiler's converted struct type. Costs a kind test and at most
// two pointer compares.
//
// Takes frame (*CallFrame) which supplies the type table.
// Takes registers (*Registers) which holds the receiver.
// Takes receiverRegister (uint8) which is the general-bank register of the receiver.
// Takes layout (StructFieldLayout) which names the struct type the layout was built for.
//
// Returns true when the unsafe interface-field fast paths may trust the layout.
func layoutStructMatchesReceiver(frame *CallFrame, registers *Registers, receiverRegister uint8, layout program.StructFieldLayout) bool {
	receiver := registers.General[receiverRegister]
	if receiver.Kind() == reflect.Interface {
		receiver = receiver.Elem()
		if !receiver.IsValid() {
			return false
		}
	}
	words := layoutTypeWordTable(frame.Function)
	if int(layout.TypeIndex) >= len(words) {
		return false
	}
	typeWord := (*unsafeReflectValue)(unsafe.Pointer(&receiver)).typ
	pair := words[layout.TypeIndex]
	return typeWord == pair.Value || typeWord == pair.Pointer
}

// structFieldReceiverBase resolves the receiver's base pointer for tier-0 struct-field
// access. Inlines structFieldUnsafeBase's Pointer-kind hot path; non-Pointer-kind
// receivers fall through to the shared helper which handles Interface /
// addressable-Struct.
//
// Takes registers (*Registers) which contains the receiver value.
// Takes receiverRegister (uint8) which is the general-bank register holding the receiver.
//
// Returns the base pointer for unsafe field arithmetic and ok=true when usable; otherwise
// ok=false signals the reflect fallback.
func structFieldReceiverBase(registers *Registers, receiverRegister uint8) (unsafe.Pointer, bool) {
	recvRaw := (*unsafeReflectValue)(unsafe.Pointer(new(registers.General[receiverRegister])))
	if recvRaw.typ != nil && reflect.Kind(recvRaw.flag&flagKindMask) == reflect.Pointer {
		var base unsafe.Pointer
		if recvRaw.flag&flagIndir != 0 {
			base = *(*unsafe.Pointer)(recvRaw.ptr)
		} else {
			base = recvRaw.ptr
		}
		return base, base != nil
	}
	return structFieldUnsafeBase(registers, receiverRegister)
}

// tryGetStructInterfaceField is the Interface-kind GET fast path, reading the eface
// header directly. Only direct-pointer held kinds take the fast path.
//
// Takes registers (*Registers) which contains the destination slot.
// Takes destinationRegister (uint8) which is the general-bank register.
// Takes fieldPointer (unsafe.Pointer) which is the address of the eface field.
//
// Returns (opContinue, true) on the fast path; (_, false) for non-direct holds.
func tryGetStructInterfaceField(registers *Registers, destinationRegister uint8, fieldPointer unsafe.Pointer) (OpResult, bool) {
	heldTyp := *(*unsafe.Pointer)(fieldPointer)
	if heldTyp == nil {
		registers.General[destinationRegister] = reflect.Value{}
		return opContinue, true
	}
	heldKindByte := *(*uint8)(unsafe.Add(heldTyp, abiTypeKindOffset))
	heldKind := reflect.Kind(heldKindByte & uint8(flagKindMask))
	if !program.HeldKindIsDirectPointer(heldKind) {
		return opContinue, false
	}
	heldData := *(*unsafe.Pointer)(unsafe.Add(fieldPointer, program.EfaceDataPointerOffset))

	registers.General[destinationRegister] = unsafeDirectIfaceKindValue(heldTyp, heldData, heldKind)
	return opContinue, true
}

// getStructFieldGeneralFallback walks the struct via reflect.Field and rebinds through
// unsafeNewAt to drop any flagRO set by reading an unexported field. Without the rebind,
// downstream SET on the returned value would panic with "reflect.Value.Set using value
// obtained using unexported field"; this matches the equivalent branch in handleGetField.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the receiver and destination banks.
// Takes instruction (instruction) whose operands encode A=dest and B=receiver.
// Takes layout (StructFieldLayout) which is the pre-resolved layout entry.
//
// Returns opContinue once the destination register has been written, or the raised
// interpreted panic when the receiver is a nil pointer.
func getStructFieldGeneralFallback(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, layout program.StructFieldLayout) OpResult {
	field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
	if !walkOK {
		if result := structFieldFallbackMiss(vm, frame, registers, instruction.B); result != opContinue {
			return result
		}
		registers.General[instruction.A] = reflect.Value{}
		return opContinue
	}
	if field.CanAddr() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	registers.General[instruction.A] = snapshotPointerLeaf(unwrapInterfaceLeaf(field))
	return opContinue
}

// handleGetStructFieldRawPointerT0 specialises handleGetStructFieldGeneralT0 for
// cycle-broken interface{} fields whose held value the Compiler has proved is always a
// pointer of a statically-known type (the cycle-broken *Self to any substitution in
// convertFieldBreakingCycles).
//
// The Compiler emits this only for cycle-broken Interface layouts whose held value is
// always a pointer.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which supplies the type table and layout table.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue.
func handleGetStructFieldRawPointerT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout := readTier0StructFieldLayout(frame, instruction.C)
	recvRaw := (*unsafeReflectValue)(unsafe.Pointer(new(registers.General[instruction.B])))
	var base unsafe.Pointer
	var ok bool
	if recvRaw.typ != nil && reflect.Kind(recvRaw.flag&flagKindMask) == reflect.Pointer {
		if recvRaw.flag&flagIndir != 0 {
			base = *(*unsafe.Pointer)(recvRaw.ptr)
		} else {
			base = recvRaw.ptr
		}
		ok = base != nil
	} else {
		base, ok = structFieldUnsafeBase(registers, instruction.B)
	}
	if !ok {
		if result := structFieldFallbackMiss(vm, frame, registers, instruction.B); result != opContinue {
			return result
		}
		registers.General[instruction.A] = reflect.Value{}
		return opContinue
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	heldTyp := *(*unsafe.Pointer)(fieldPointer)
	if heldTyp == nil {
		registers.General[instruction.A] = reflect.Value{}
		return opContinue
	}
	heldData := *(*unsafe.Pointer)(unsafe.Add(fieldPointer, program.EfaceDataPointerOffset))
	registers.General[instruction.A] = unsafePointerKindValue(heldTyp, heldData)
	return opContinue
}

// snapshotPointerLeaf detaches a pointer Value from its storage so a subsequent write to
// the same struct field does not silently mutate the previously-read value.
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
		return field
	}
	rawPtr := field.UnsafePointer()
	return unsafePointerKindValue(reflectValueABIType(field.Type()), rawPtr)
}

// unwrapInterfaceLeaf normalises an interface leaf to its held value.
//
// Mirrors the cycle-broken interface unwrap branch in handleGetField so subsequent field
// accesses see the user's pointer/struct directly.
// Returns the zero Value for nil interface holds so downstream nil checks in opTestNil
// keep working.
//
// Takes field (reflect.Value) which is the leaf value just read from the struct.
//
// Returns the unwrapped value when field is a non-nil interface, otherwise field
// unchanged.
func unwrapInterfaceLeaf(field reflect.Value) reflect.Value {
	if !field.IsValid() {
		return field
	}
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
// Operand A=general structReg, B=general valueReg, C=structLayoutTable index. Reflects on
// the field address via reflect.NewAt and uses Set to install the value through Go's GC
// write barrier. Skipping the barrier (e.g. by direct *(*unsafe.Pointer)(fieldPointer) =
// uintptr store) would corrupt the GC tricolour invariant for pointer-bearing fields. The
// Set call handles the interface-wrapping step when the field type is interface{} and the
// value type is the concrete held type - matching handleSetField's coercion.
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
	if int(layout.FieldTypeIndex) >= len(frame.Function.TypeTable) {
		return setStructFieldGeneralReflectFallback(vm, frame, registers, instruction, layout, value)
	}
	fieldType := frame.Function.TypeTable[layout.FieldTypeIndex]
	base, ok := structFieldReceiverBase(registers, instruction.A)
	if !ok || fieldType == nil {
		return setStructFieldGeneralReflectFallback(vm, frame, registers, instruction, layout, value)
	}
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	if reflect.Kind(layout.Kind) == reflect.Interface {
		if !layoutStructMatchesReceiver(frame, registers, instruction.A, layout) {
			return setStructFieldGeneralReflectFallback(vm, frame, registers, instruction, layout, value)
		}
		return setStructInterfaceField(vm, value, fieldPointer, fieldType)
	}
	field := unsafeNewAt(reflectValueABIType(fieldType), fieldPointer, fieldType.Kind())
	if !value.IsValid() {
		field.SetZero()
		return opContinue
	}
	coerced := coerceValue(vm, value, fieldType)
	if !frame.Function.FieldStoreArenaSafePCs[frame.ProgramCounter-1] {
		coerced = MaterialiseArenaValue(vm.Arena, coerced)
	}
	field.Set(coerced)
	return opContinue
}

// handleCopyStructFieldGeneralT0 fuses a field-to-field copy for general-bank struct
// fields.
//
// Replaces the GET_STRUCT_FIELD_GENERAL_T0 + SET_STRUCT_FIELD_GENERAL_T0 pair the
// peephole pass emits when the intermediate destination register is read exactly once (by
// the SET) and written exactly once (by the GET). The fused op skips the intermediate
// reflect.Value materialisation in the GET (no register write) and the matching
// extraction in the SET, going directly from `*(sourceBase+srcOffset)` to
// `*(destinationBase+dstOffset)` via runtime.typedmemmove (which preserves the
// destination's write barriers).
//
// Takes vm (*VM) which is carried for the fallback path.
// Takes frame (*CallFrame) which supplies the type and layout tables.
// Takes registers (*Registers) which contains the source and destination receivers.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue after the copy or fallback completes.
func handleCopyStructFieldGeneralT0(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	sourceLayout := readTier0StructFieldLayout(frame, instruction.C)
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	destinationLayout := readTier0StructFieldLayout(frame, extension.A)
	if int(sourceLayout.FieldTypeIndex) >= len(frame.Function.TypeTable) {
		return copyStructFieldGeneralFallback(vm, frame, registers, instruction, sourceLayout, destinationLayout)
	}
	if int(destinationLayout.FieldTypeIndex) >= len(frame.Function.TypeTable) {
		return copyStructFieldGeneralFallback(vm, frame, registers, instruction, sourceLayout, destinationLayout)
	}
	sourceBase, ok := structFieldReceiverBase(registers, instruction.A)
	if !ok {
		return copyStructFieldGeneralFallback(vm, frame, registers, instruction, sourceLayout, destinationLayout)
	}
	destinationBase, ok := structFieldReceiverBase(registers, instruction.B)
	if !ok {
		return copyStructFieldGeneralFallback(vm, frame, registers, instruction, sourceLayout, destinationLayout)
	}
	destinationFieldType := frame.Function.TypeTable[destinationLayout.FieldTypeIndex]
	if destinationFieldType == nil {
		return copyStructFieldGeneralFallback(vm, frame, registers, instruction, sourceLayout, destinationLayout)
	}
	sourceFieldPointer := unsafe.Add(sourceBase, uintptr(sourceLayout.Offset))
	destinationFieldPointer := unsafe.Add(destinationBase, uintptr(destinationLayout.Offset))
	runtimeTypedmemmove(reflectValueABIType(destinationFieldType), destinationFieldPointer, sourceFieldPointer)
	return opContinue
}

// copyStructFieldGeneralFallback runs the slow GET-then-SET sequence when the fast path
// in handleCopyStructFieldGeneralT0 cannot apply.
//
// Takes vm (*VM) which is threaded into the reflect fallback.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the source and destination receivers.
// Takes instr (instruction) which encodes the operand indices.
// Takes sourceLayout (StructFieldLayout) which is the source field layout.
// Takes destinationLayout (StructFieldLayout) which is the destination field layout.
//
// Returns opContinue after the fallback completes.
func copyStructFieldGeneralFallback(
	vm *VM,
	frame *CallFrame,
	registers *Registers,
	instr isa.Instruction,
	sourceLayout, destinationLayout program.StructFieldLayout,
) OpResult {
	getProxy := isa.Instruction{Op: instr.Op, A: instr.A, B: instr.A, C: 0}
	fetched := getStructFieldGeneralReflectRead(registers, frame, getProxy, sourceLayout)
	if !fetched.IsValid() {
		if result := structFieldFallbackMiss(vm, frame, registers, instr.A); result != opContinue {
			return result
		}
	}
	setProxy := isa.Instruction{Op: instr.Op, A: instr.B, B: 0, C: 0}
	return setStructFieldGeneralReflectFallback(vm, frame, registers, setProxy, destinationLayout, fetched)
}

// getStructFieldGeneralReflectRead reads a struct field via the reflect-walk path the SET
// fallback also uses.
//
// Takes registers (*Registers) which holds the receiver.
// Takes instr (instruction) whose operands carry the receiver register.
// Takes layout (StructFieldLayout) which describes the field path.
//
// Returns the leaf reflect.Value, or the zero Value when the walk fails.
func getStructFieldGeneralReflectRead(registers *Registers, _ *CallFrame, instr isa.Instruction, layout program.StructFieldLayout) reflect.Value {
	receiver := registers.General[instr.B]
	if !receiver.IsValid() {
		return reflect.Value{}
	}
	if receiver.Kind() == reflect.Pointer {
		if receiver.IsNil() {
			return reflect.Value{}
		}
		receiver = receiver.Elem()
	}
	if receiver.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	field := walkStructFieldByLayout(receiver, layout)
	return field
}

// walkStructFieldByLayout follows the (possibly embedded) field path recorded in
// layout.Path to the leaf reflect.Value. Mirrors the walk that handleGetField performs
// for non-T0 paths.
//
// Takes receiver (reflect.Value) which is the starting struct value.
// Takes layout (StructFieldLayout) which provides the field path.
//
// Returns the leaf reflect.Value, or the zero Value when any step is invalid.
func walkStructFieldByLayout(receiver reflect.Value, layout program.StructFieldLayout) reflect.Value {
	current := receiver
	for step := range int(layout.PathLength) {
		if current.Kind() != reflect.Struct {
			return reflect.Value{}
		}
		fieldIdx := int(layout.Path[step])
		if fieldIdx >= current.NumField() {
			return reflect.Value{}
		}
		current = current.Field(fieldIdx)
	}
	return current
}

// setStructInterfaceField writes an interface-typed struct field.
//
// Uses the direct iface-write fast path: the `node.previous = X` pattern dominates this
// handler and value is usually a Pointer-kind reflect.Value (`*lruNode` from
// snapshotPointerLeaf). Skips reflect.Value.Set's full dispatch by building the eface
// header on stack and copying it through runtime.typedmemmove (which emits the same write
// barriers Set would).
//
// The eface shortcut is only valid for empty interface fields (NumMethod()==0); non-empty
// interfaces need itab synthesis via reflect.Value.Set. Non-pointer-kind sources also
// fall through to the reflect path.
//
// Takes vm (*VM) which is the virtual machine.
// Takes value (reflect.Value) which is the source value being installed.
// Takes fieldPointer (unsafe.Pointer) which is the address of the interface field.
// Takes fieldType (reflect.Type) which is the type of the interface field.
//
// Returns opContinue once the write completes.
func setStructInterfaceField(vm *VM, value reflect.Value, fieldPointer unsafe.Pointer, fieldType reflect.Type) OpResult {
	valueRaw := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if valueRaw.typ == nil {
		zeroEface := [2]unsafe.Pointer{}
		runtimeTypedmemmove(reflectValueABIType(fieldType), fieldPointer, unsafe.Pointer(&zeroEface[0]))
		return opContinue
	}
	valueKind := reflect.Kind(valueRaw.flag & flagKindMask)
	if program.HeldKindIsDirectPointer(valueKind) && fieldType.NumMethod() == 0 {
		dataPtr := efaceDataPointerFromValue(valueRaw)
		eface := [2]unsafe.Pointer{valueRaw.typ, dataPtr}
		runtimeTypedmemmove(reflectValueABIType(fieldType), fieldPointer, unsafe.Pointer(&eface[0]))
		return opContinue
	}

	wrapped := wrapForInterfaceField(vm, value, fieldType)
	if err := checkInterfaceFieldValue(vm, wrapped, fieldType); err != nil {
		vm.evalError = err
		return opPanicError
	}
	field := unsafeNewAt(reflectValueABIType(fieldType), fieldPointer, reflect.Interface)
	field.Set(wrapped)
	return opContinue
}

// wrapForInterfaceField returns value wrapped in an adapter.
//
// Wraps when the destination interface field has methods that value's Go-side reflect
// method set does not satisfy. Returns value unchanged when no adaptation is needed
// (empty interface, or value already implements the destination via its native Go method
// set, or the value is not a pipit-synthesised type with adapter coverage).
//
// Takes vm (*VM) which provides the method registry used by tryBuildInterfaceAdapter.
// Takes value (reflect.Value) which is the source value about to be stored.
// Takes fieldType (reflect.Type) which is the destination interface field's declared
// type.
//
// Returns reflect.Value which is the (possibly wrapped) value ready for
// reflect.Value.Set.
func wrapForInterfaceField(vm *VM, value reflect.Value, fieldType reflect.Type) reflect.Value {
	if vm == nil || !value.IsValid() {
		return value
	}
	if fieldType.Kind() != reflect.Interface || fieldType.NumMethod() == 0 {
		return value
	}
	if value.Type().Implements(fieldType) {
		return value
	}
	adapted := tryBuildInterfaceAdapter(vm, value, fieldType, argumentTypeContext{staticTypeName: "", staticTypeString: "", skipInterfaceAdapter: false, keepClosures: false})
	if !adapted.IsValid() {
		return value
	}
	return adapted
}

// efaceDataPointerFromValue extracts the eface data pointer from a Pointer-kind
// reflect.Value. When flagIndir is unset the storage pointer is the pointer value itself
// (snapshotPointerLeaf produces this shape); when flagIndir is set the storage pointer
// must be dereferenced once to fetch the actual pointer.
//
// Takes valueRaw (*unsafeReflectValue) which is the header of the source value.
//
// Returns the eface data pointer ready for packing into an interface slot.
func efaceDataPointerFromValue(valueRaw *unsafeReflectValue) unsafe.Pointer {
	if valueRaw.flag&flagIndir != 0 {
		return *(*unsafe.Pointer)(valueRaw.ptr)
	}
	return valueRaw.ptr
}

// setStructFieldGeneralReflectFallback writes a general-bank value to a struct field via
// the reflect.Field walk.
//
// Used when the unsafe-pointer base is unavailable (non-addressable receiver) or when the
// field's storage layout requires reflect's full ABI handling (interface-kind leaves).
// The CanSet/CanAddr rebind strips the flagRO bit that reflect.Value.Field sets on
// unexported fields, without which Set/SetZero would panic.
//
// Takes vm (*VM) for closure-coercion support in coerceValue.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instruction (isa.Instruction) which encodes the operand indices.
// Takes layout (StructFieldLayout) which describes the field path.
// Takes value (reflect.Value) which is the right-hand-side value to install at the
// field's storage.
//
// Returns opContinue once the write completes or is skipped, or the raised interpreted
// panic when the receiver is a nil pointer.
func setStructFieldGeneralReflectFallback(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, layout program.StructFieldLayout, value reflect.Value) OpResult {
	field, walkOK := structFieldReflectWrite(registers, frame, instruction.A, layout)
	if !walkOK {
		return structFieldFallbackMiss(vm, frame, registers, instruction.A)
	}
	if !field.CanSet() && field.CanAddr() {
		field = unsafeNewAt(reflectValueABIType(field.Type()), ReflectValuePtr(field), field.Kind())
	}
	if !value.IsValid() {
		field.SetZero()
		return opContinue
	}
	coerced := wrapForInterfaceField(vm, coerceValue(vm, value, field.Type()), field.Type())
	if err := checkInterfaceFieldValue(vm, coerced, field.Type()); err != nil {
		vm.evalError = err
		return opPanicError
	}
	field.Set(coerced)
	return opContinue
}

// handleGetStructFieldIndexGeneral reads one element of an array-typed struct field:
// general[A] = receiver[B].field(layout[C]).elem[ints[ext1.C]].
//
// Fuses the GET_STRUCT_FIELD_GENERAL_T0 + INDEX pair the Compiler previously emitted for
// expressions like node.children[slot]. Pointer-kind elements load the pointer word
// directly; interface-kind elements reuse the direct-iface eface read; every other
// element shape goes through an addressable reflect view with the same non-nil interface
// unwrap the INDEX handler applies.
//
// Takes vm (*VM) which supplies panic plumbing for the bounds check.
// Takes frame (*CallFrame) which provides the layout table, type table, and PC.
// Takes registers (*Registers) which holds the receiver, index, and destination.
// Takes instruction (instruction) which carries A=dest, B=receiver, C=layout index.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldIndexGeneral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	indexRegister, arrayLength, elementSize, elementKind := readFusedArrayFieldExtensions(frame)
	layout := readTier0StructFieldLayout(frame, instruction.C)
	index := int(registers.Ints[indexRegister])
	base, ok := structFieldUnsafeBase(registers, instruction.B)
	if !ok {
		field, walkOK := structFieldReflectRead(registers, frame, instruction.B, layout)
		if !walkOK {
			return structFieldFallbackMiss(vm, frame, registers, instruction.B)
		}
		if boundsResult, inRange := checkSliceBounds(vm, field, index); !inRange {
			return boundsResult
		}
		element := field.Index(index)

		if element.CanAddr() {
			element = unsafeNewAt(reflectValueABIType(element.Type()), ReflectValuePtr(element), element.Kind())
		}
		registers.General[instruction.A] = snapshotPointerLeaf(unwrapInterfaceLeaf(element))
		return opContinue
	}
	if index < 0 || index >= arrayLength {
		return raiseIndexOutOfRange(vm, index, arrayLength)
	}
	elementPointer := unsafe.Add(base, uintptr(layout.Offset)+uintptr(index)*elementSize)
	if elementKind == reflect.Interface {
		if result, handled := tryGetStructInterfaceField(registers, instruction.A, elementPointer); handled {
			return result
		}
	}
	fieldType := frame.Function.TypeTable[layout.FieldTypeIndex]
	elementType := fieldType.Elem()
	if elementKind == reflect.Pointer {
		rawPointer := *(*unsafe.Pointer)(elementPointer)
		registers.General[instruction.A] = unsafePointerKindValue(reflectValueABIType(elementType), rawPointer)
		return opContinue
	}
	element := unsafeNewAt(reflectValueABIType(elementType), elementPointer, elementType.Kind())
	if element.Kind() == reflect.Interface && !element.IsNil() {
		element = element.Elem()
	}
	registers.General[instruction.A] = element
	return opContinue
}
