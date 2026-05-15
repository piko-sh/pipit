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
	"pipit.sh/pipit/internal/safeconv"
)

// sliceStructFieldResolution captures the outcome of trying to resolve
// `slice[index].field` to either a direct field pointer (fast path) or a leaf
// reflect.Value (slow path). At most one of fieldPointer or reflectLeaf is valid; ok is
// false on bounds / nil errors (with vm.evalError already set).
type sliceStructFieldResolution struct {
	// fieldPointer is the direct unsafe pointer to the leaf field when useUnsafe is true;
	// nil otherwise.
	fieldPointer unsafe.Pointer

	// reflectLeaf is the leaf reflect.Value when useUnsafe is false; zero Value otherwise.
	reflectLeaf reflect.Value

	// useUnsafe selects between the unsafe-pointer fast path and the reflect.Field walk
	// fallback.
	useUnsafe bool

	// ok reports whether resolution succeeded; false signals a bounds or nil-pointer error
	// and panicResult carries the resulting OpResult that callers should propagate (a
	// recover may have caught the panic, in which case panicResult will be opFrameChanged
	// rather than opPanicError).
	ok bool

	// panicResult carries the OpResult produced by the inner raiseNativePanicAsInterpreted
	// call when resolution fails so callers can propagate it instead of always returning
	// opPanicError. Meaningless when ok is true.
	panicResult OpResult
}

// readSliceStructFieldLayout fetches the StructLayoutTable entry referenced by the
// trailing isa.OpExt word and advances the program counter past the extension.
//
// Takes frame (*CallFrame) which provides the layout table and the isa.OpExt word at the
// current program counter.
//
// Returns the resolved layout and true on success; the zero layout and false when the
// layout index is out of range.
func readSliceStructFieldLayout(frame *CallFrame) (program.StructFieldLayout, bool) {
	layoutIndex := readExtensionWideIndex(frame)
	if int(layoutIndex) >= len(frame.Function.StructLayoutTable) {
		return program.StructFieldLayout{}, false
	}
	return frame.Function.StructLayoutTable[layoutIndex], true
}

// sliceStructFieldResolve attempts unsafe-pointer resolution first, falling back to a
// reflect.Field walk when the slice's element reflect.Type doesn't match the expected
// struct type (cycle-broken []any-storage slices, generic instantiations where storage
// layout differs from the static type). Bounds and nil-pointer errors short-circuit with
// ok=false and vm.evalError set so the caller returns opPanicError.
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes slice (reflect.Value) which holds the source slice value.
// Takes index (int) which selects the element to read.
// Takes layout (StructFieldLayout) which describes the field path.
// Takes frame (*CallFrame) which provides the TypeTable lookup.
//
// Returns the resolution capturing either the unsafe pointer or the reflect leaf, with ok
// set on success.
func sliceStructFieldResolve(vm *VM, slice reflect.Value, index int, layout program.StructFieldLayout, frame *CallFrame) sliceStructFieldResolution {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, slice)
	if !ok {
		return sliceStructFieldResolution{panicResult: indexPanicResult, fieldPointer: nil, reflectLeaf: reflect.Value{}, useUnsafe: false, ok: false}
	}
	if boundsResult, ok := checkSliceBounds(vm, collection, index); !ok {
		return sliceStructFieldResolution{panicResult: boundsResult, fieldPointer: nil, reflectLeaf: reflect.Value{}, useUnsafe: false, ok: false}
	}
	if int(layout.TypeIndex) < len(frame.Function.TypeTable) {
		elementType := frame.Function.TypeTable[layout.TypeIndex]
		if elementType != nil &&
			collection.Type().Elem() == elementType &&
			collection.UnsafePointer() != nil {
			elementSize := elementType.Size()
			elementPtr := unsafe.Add(collection.UnsafePointer(), safeconv.IntToUintptr(index)*elementSize)
			return sliceStructFieldResolution{fieldPointer: unsafe.Add(elementPtr, uintptr(layout.Offset)),
				useUnsafe: true,
				ok:        true, reflectLeaf: reflect.Value{}, panicResult: 0}
		}
	}
	element := unwrapInterfaceElement(collection.Index(index))
	for level := uint8(0); level < layout.PathLength; level++ {
		if element.Kind() != reflect.Struct {
			return sliceStructFieldResolution{}
		}
		fieldIndex := int(layout.Path[level])
		if fieldIndex >= element.NumField() {
			return sliceStructFieldResolution{}
		}
		element = element.Field(fieldIndex)
	}
	return sliceStructFieldResolution{reflectLeaf: element,
		ok: true, fieldPointer: nil, useUnsafe: false, panicResult: 0}
}

// handleSliceIndexStructFieldInt reads slice[index].field where the leaf is an int-kind
// value, writing the result to ints[A]. The extension word carries the StructLayoutTable
// index.
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source slice and destination int register.
// Takes instruction (instruction) which encodes A/B/C operands.
//
// Returns opPanicError on bounds / nil failure or opContinue on success.
func handleSliceIndexStructFieldInt(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	r := sliceStructFieldResolve(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout, frame)
	if !r.ok {
		return opPanicError
	}
	if r.useUnsafe {
		switch reflect.Kind(layout.Kind) {
		case reflect.Int:
			registers.Ints[instruction.A] = int64(*(*int)(r.fieldPointer))
		case reflect.Int8:
			registers.Ints[instruction.A] = int64(*(*int8)(r.fieldPointer))
		case reflect.Int16:
			registers.Ints[instruction.A] = int64(*(*int16)(r.fieldPointer))
		case reflect.Int32:
			registers.Ints[instruction.A] = int64(*(*int32)(r.fieldPointer))
		case reflect.Int64:
			registers.Ints[instruction.A] = *(*int64)(r.fieldPointer)
		default:
		}
		return opContinue
	}
	registers.Ints[instruction.A] = r.reflectLeaf.Int()
	return opContinue
}

// handleSliceIndexStructFieldUint reads slice[index].field for uint leaves into uints[A].
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source slice and destination uint
// register.
// Takes instruction (instruction) which encodes A/B/C operands.
//
// Returns opPanicError on failure or opContinue on success.
func handleSliceIndexStructFieldUint(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	r := sliceStructFieldResolve(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout, frame)
	if !r.ok {
		return opPanicError
	}
	if r.useUnsafe {
		switch reflect.Kind(layout.Kind) {
		case reflect.Uint:
			registers.Uints[instruction.A] = uint64(*(*uint)(r.fieldPointer))
		case reflect.Uint8:
			registers.Uints[instruction.A] = uint64(*(*uint8)(r.fieldPointer))
		case reflect.Uint16:
			registers.Uints[instruction.A] = uint64(*(*uint16)(r.fieldPointer))
		case reflect.Uint32:
			registers.Uints[instruction.A] = uint64(*(*uint32)(r.fieldPointer))
		case reflect.Uint64:
			registers.Uints[instruction.A] = *(*uint64)(r.fieldPointer)
		case reflect.Uintptr:
			registers.Uints[instruction.A] = uint64(*(*uintptr)(r.fieldPointer))
		default:
		}
		return opContinue
	}
	registers.Uints[instruction.A] = r.reflectLeaf.Uint()
	return opContinue
}

// handleSliceIndexStructFieldFloat reads slice[index].field for float leaves into
// floats[A].
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source slice and destination float
// register.
// Takes instruction (instruction) which encodes A/B/C operands.
//
// Returns opPanicError on failure or opContinue on success.
func handleSliceIndexStructFieldFloat(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	r := sliceStructFieldResolve(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout, frame)
	if !r.ok {
		return opPanicError
	}
	if r.useUnsafe {
		switch reflect.Kind(layout.Kind) {
		case reflect.Float32:
			registers.Floats[instruction.A] = float64(*(*float32)(r.fieldPointer))
		case reflect.Float64:
			registers.Floats[instruction.A] = *(*float64)(r.fieldPointer)
		default:
		}
		return opContinue
	}
	registers.Floats[instruction.A] = r.reflectLeaf.Float()
	return opContinue
}

// handleSliceIndexStructFieldBool reads slice[index].field for bool leaves into bools[A].
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source slice and destination bool
// register.
// Takes instruction (instruction) which encodes A/B/C operands.
//
// Returns opPanicError on failure or opContinue on success.
//
//nolint:dupl // hot-path twin
func handleSliceIndexStructFieldBool(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	r := sliceStructFieldResolve(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout, frame)
	if !r.ok {
		return opPanicError
	}
	if r.useUnsafe {
		registers.Bools[instruction.A] = *(*bool)(r.fieldPointer)
		return opContinue
	}
	registers.Bools[instruction.A] = r.reflectLeaf.Bool()
	return opContinue
}

// handleSliceIndexStructFieldString reads slice[index].field for string leaves into
// strings[A]. The 16-byte Go string header (data pointer + length) is copied directly
// without allocating a fresh Go string - the destination shares the underlying byte data
// with the source until the string register is overwritten.
//
// Takes vm (*VM) which receives the EvalError on failure.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the source slice and destination string
// register.
// Takes instruction (instruction) which encodes A/B/C operands.
//
// Returns opPanicError on failure or opContinue on success.
//
//nolint:dupl // hot-path twin
func handleSliceIndexStructFieldString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	layout, ok := readSliceStructFieldLayout(frame)
	if !ok {
		return opPanicError
	}
	r := sliceStructFieldResolve(vm, registers.General[instruction.B], int(registers.Ints[instruction.C]), layout, frame)
	if !r.ok {
		return opPanicError
	}
	if r.useUnsafe {
		registers.Strings[instruction.A] = *(*string)(r.fieldPointer)
		return opContinue
	}
	registers.Strings[instruction.A] = r.reflectLeaf.String()
	return opContinue
}
