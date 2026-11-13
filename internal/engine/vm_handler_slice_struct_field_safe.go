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

//go:build safe

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// readSliceStructFieldLayout decodes the layout-index extension word.
//
// Takes frame (*CallFrame) which supplies the extension word and the owning function's
// structLayoutTable.
//
// Returns the decoded StructFieldLayout entry.
// Returns true when the encoded index is in range; false otherwise.
func readSliceStructFieldLayout(frame *CallFrame) (program.StructFieldLayout, bool) {
	layoutIndex := readExtensionWideIndex(frame)
	if int(layoutIndex) >= len(frame.Function.StructLayoutTable) {
		return program.StructFieldLayout{}, false
	}
	return frame.Function.StructLayoutTable[layoutIndex], true
}

// sliceStructFieldReflectLeaf walks a slice[i] field path via reflect.
//
// Takes vm (*VM) which is consulted for bounds-check error reporting.
// Takes slice (reflect.Value) which is the slice (or interface-wrapped slice) being
// indexed.
// Takes index (int) which is the element offset.
// Takes layout (StructFieldLayout) which encodes the field path inside the element.
//
// Returns reflect.Value which is the leaf selected by the path.
// Returns OpResult which is what the handler returns on failure. It is the result of
// raising the bounds or nil-pointer panic, which a recover may have caught, and
// opPanicError for a shape the walk cannot follow.
// Returns bool which is true on success and false when a bounds, kind or field-index
// check fails.
func sliceStructFieldReflectLeaf(vm *VM, slice reflect.Value, index int, layout program.StructFieldLayout) (reflect.Value, OpResult, bool) {
	collection, indexResult, ok := resolveIndexCollection(vm, slice)
	if !ok {
		return reflect.Value{}, indexResult, false
	}
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return reflect.Value{}, boundsResult, false
	}
	element := unwrapInterfaceElement(collection.Index(index))
	for level := uint8(0); level < layout.PathLength; level++ {
		if element.Kind() != reflect.Struct {
			return reflect.Value{}, opPanicError, false
		}
		fieldIndex := int(layout.Path[level])
		if fieldIndex >= element.NumField() {
			return reflect.Value{}, opPanicError, false
		}
		element = element.Field(fieldIndex)
	}
	return element, opContinue, true
}

// handleGetElementFieldInt64 writes ints[a] = (slice[c]).field.int.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult which is opContinue on success, opPanicError on bounds / shape
// failure.
func handleGetElementFieldInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, failure, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return failure
	}
	registers.Ints[instruction.A] = field.Int()
	return opContinue
}

// handleGetElementFieldUint64 writes uints[a] = (slice[c]).field.uint.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult which is opContinue on success, opPanicError on failure.
func handleGetElementFieldUint64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, failure, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return failure
	}
	registers.Uints[instruction.A] = field.Uint()
	return opContinue
}

// handleGetElementFieldFloat64 writes floats[a] = (slice[c]).field.float.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult which is opContinue on success, opPanicError on failure.
func handleGetElementFieldFloat64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, failure, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return failure
	}
	registers.Floats[instruction.A] = field.Float()
	return opContinue
}

// handleGetElementFieldBool writes bools[a] = (slice[c]).field.bool.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult which is opContinue on success, opPanicError on failure.
func handleGetElementFieldBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, failure, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return failure
	}
	registers.Bools[instruction.A] = field.Bool()
	return opContinue
}

// handleGetElementFieldString writes strings[a] = (slice[c]).field.string.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult which is opContinue on success, opPanicError on failure.
func handleGetElementFieldString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, failure, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return failure
	}
	registers.Strings[instruction.A] = field.String()
	return opContinue
}
