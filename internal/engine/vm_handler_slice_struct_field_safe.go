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
// Returns the leaf reflect.Value selected by the path.
// Returns true on success; false when bounds, kind, or field-index checks fail.
func sliceStructFieldReflectLeaf(vm *VM, slice reflect.Value, index int, layout program.StructFieldLayout) (reflect.Value, bool) {
	collection, _, ok := resolveIndexCollection(vm, slice)
	if !ok {
		return reflect.Value{}, false
	}
	if _, ok := checkSliceBounds(vm, collection, index); !ok {
		return reflect.Value{}, false
	}
	element := unwrapInterfaceElement(collection.Index(index))
	for level := uint8(0); level < layout.PathLength; level++ {
		if element.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		fieldIndex := int(layout.Path[level])
		if fieldIndex >= element.NumField() {
			return reflect.Value{}, false
		}
		element = element.Field(fieldIndex)
	}
	return element, true
}

// handleSliceIndexStructFieldInt writes ints[a] = (slice[c]).field.int.
//
// Safe-build mirror of OpSliceIndexStructFieldInt. Reads the layout extension word, walks
// the field path via reflect, and stores the leaf Int() value.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult: opContinue on success, opPanicError on bounds / shape failure.
func handleSliceIndexStructFieldInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return opPanicError
	}
	registers.Ints[instruction.A] = field.Int()
	return opContinue
}

// handleSliceIndexStructFieldUint writes uints[a] = (slice[c]).field.uint.
//
// Safe-build mirror of isa.OpSliceIndexStructFieldUint.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult: opContinue on success, opPanicError on failure.
func handleSliceIndexStructFieldUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return opPanicError
	}
	registers.Uints[instruction.A] = field.Uint()
	return opContinue
}

// handleSliceIndexStructFieldFloat writes floats[a] = (slice[c]).field.float.
//
// Safe-build mirror of isa.OpSliceIndexStructFieldFloat.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult: opContinue on success, opPanicError on failure.
func handleSliceIndexStructFieldFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return opPanicError
	}
	registers.Floats[instruction.A] = field.Float()
	return opContinue
}

// handleSliceIndexStructFieldBool writes bools[a] = (slice[c]).field.bool.
//
// Safe-build mirror of isa.OpSliceIndexStructFieldBool.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult: opContinue on success, opPanicError on failure.
func handleSliceIndexStructFieldBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return opPanicError
	}
	registers.Bools[instruction.A] = field.Bool()
	return opContinue
}

// handleSliceIndexStructFieldString writes strings[a] = (slice[c]).field.string.
//
// Safe-build mirror of isa.OpSliceIndexStructFieldString.
//
// Takes vm (*VM) which provides bounds-check error context.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which is the typed register file.
// Takes instruction (instruction) which encodes dest A, slice B, and index C.
//
// Returns OpResult: opContinue on success, opPanicError on failure.
func handleSliceIndexStructFieldString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	field, ok := sliceStructFieldReflectLeaf(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout)
	if !ok {
		return opPanicError
	}
	registers.Strings[instruction.A] = field.String()
	return opContinue
}
