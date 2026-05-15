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

//go:build pipit_bce_paranoid

// Paranoid (safety-harness) variants of the bounds-check-elimination
// handlers. The default fast variants live in
// vm_handler_bce_unchecked.go and skip the bounds check based on a
// compile-time proof emitted by the BCE pass. A defective proof there
// is undefined behaviour at runtime.
//
// Building with -tags pipit_bce_paranoid swaps in this file: every
// "unchecked" handler regains an explicit bounds check and routes
// out-of-range accesses through vm.evalError + opPanicError, mirroring
// the checked variants exactly. CI enables this tag for the
// adversarial-bytecode fuzz runs so a bad proof surfaces as a clean
// verifier-style failure rather than a Go runtime panic or memory
// corruption. Production builds leave the tag off.

package engine

import (
	"fmt"
	"reflect"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// errIdxOutOfRangeFmt is the format string the paranoid bounds-check handlers use to
	// report an index-out-of-range access ("<sentinel>: <index> with length <len>"). It
	// lives here rather than in the default build because only these pipit_bce_paranoid
	// handlers format the message directly; the production handlers route through
	// raiseIndexOutOfRange.
	errIdxOutOfRangeFmt = "%w: %d with length %d"
)

// handleSliceGetIntDirectUnchecked is the paranoid variant of handleSliceGetIntDirect. It
// reinstates the runtime bounds check dropped by the fast variant so an incorrect
// compile-time BCE proof is reported as a regular interpreter error.
//
// Takes vm (*VM) which receives the bounds-check error when triggered.
// Takes registers (*Registers) which holds the SlicesInt collection and the int index.
// Takes instruction (instruction) which encodes the destination int register A, the
// source SlicesInt register B, and the index int register C.
//
// Returns opContinue on success or opPanicError when the index is outside [0,
// len(slice)).
func handleSliceGetIntDirectUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.SlicesInt[instruction.B]
	index := registers.Ints[instruction.C]
	if uint64(index) >= uint64(len(slice)) { //nolint:gosec // unsigned bounds check
		vm.evalError = fmt.Errorf(errIdxOutOfRangeFmt, fault.ErrIndexOutOfRange, index, len(slice))
		return opPanicError
	}
	registers.Ints[instruction.A] = slice[index]
	return opContinue
}

// handleSliceSetIntDirectUnchecked is the paranoid variant of handleSliceSetIntDirect.
// The runtime bounds check is restored to surface bad BCE proofs as interpreter errors.
//
// Takes vm (*VM) which receives the bounds-check error when triggered.
// Takes registers (*Registers) which holds the SlicesInt collection, the index, and the
// value.
// Takes instruction (instruction) which encodes the destination SlicesInt register A, the
// index int register B, and the value int register C.
//
// Returns opContinue on success or opPanicError when the index is outside [0,
// len(slice)).
func handleSliceSetIntDirectUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.SlicesInt[instruction.A]
	index := registers.Ints[instruction.B]
	if uint64(index) >= uint64(len(slice)) { //nolint:gosec // unsigned bounds check
		vm.evalError = fmt.Errorf(errIdxOutOfRangeFmt, fault.ErrIndexOutOfRange, index, len(slice))
		return opPanicError
	}
	slice[index] = registers.Ints[instruction.C]
	return opContinue
}

// handleSliceGetIntUnchecked is the paranoid variant of the reflect-bank bounds-elided
// get. The runtime range check is restored so a defective BCE proof on a general-bank
// slice surfaces through vm.evalError instead of letting reflect.Value.Index panic.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue on success or opPanicError when the collection is not indexable or
// the index falls outside [0, len).
func handleSliceGetIntUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.C])
	if index < 0 || index >= collection.Len() {
		vm.evalError = fmt.Errorf(errIdxOutOfRangeFmt, fault.ErrIndexOutOfRange, index, collection.Len())
		return opPanicError
	}
	element := unwrapInterfaceElement(collection.Index(index))
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		registers.Ints[instruction.A] = element.Int()
	default:
		registers.Ints[instruction.A] = safeconv.Uint64ToInt64Reinterpret(element.Uint())
	}
	return opContinue
}

// handleSliceSetIntUnchecked is the paranoid variant of the reflect-bank bounds-elided
// set. The runtime range check is restored.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue on success or opPanicError when the collection is not indexable or
// the index falls outside [0, len).
func handleSliceSetIntUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	index := int(registers.Ints[instruction.B])
	if index < 0 || index >= collection.Len() {
		vm.evalError = fmt.Errorf(errIdxOutOfRangeFmt, fault.ErrIndexOutOfRange, index, collection.Len())
		return opPanicError
	}
	element := collection.Index(index)
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		element.SetInt(registers.Ints[instruction.C])
	default:
		element.SetUint(safeconv.Int64ToUint64Reinterpret(registers.Ints[instruction.C]))
	}
	return opContinue
}
