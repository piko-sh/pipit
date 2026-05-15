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

//go:build !pipit_bce_paranoid

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// handleSliceGetIntDirectUnchecked is the bounds-elided variant of
// handleSliceGetIntDirect. Reads ints[A] = slicesInt[B][ints[C]] without the
// `uint64(index) >= uint64(len(slice))` check.
//
// Takes registers (*Registers) which holds the slicesInt collection and the int index.
// Takes instruction (instruction) which encodes the destination int register A, the
// source slicesInt register B, and the index int register C.
//
// Returns opContinue on success. The Go runtime's slice access will panic if the
// compile-time proof was wrong.
func handleSliceGetIntDirectUnchecked(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = registers.SlicesInt[instruction.B][registers.Ints[instruction.C]]
	return opContinue
}

// handleSliceSetIntDirectUnchecked is the bounds-elided variant of
// handleSliceSetIntDirect. Writes slicesInt[A][ints[B]] = ints[C] without the runtime
// bounds check.
//
// Takes registers (*Registers) which holds the slicesInt collection, the index, and the
// value.
// Takes instruction (instruction) which encodes the destination slicesInt register A, the
// index int register B, and the value int register C.
//
// Returns opContinue on success.
func handleSliceSetIntDirectUnchecked(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.SlicesInt[instruction.A][registers.Ints[instruction.B]] = registers.Ints[instruction.C]
	return opContinue
}

// handleSliceGetIntUnchecked is the bounds-elided reflect-bank variant of
// handleSliceGetInt for ints[A] = general[B].Index(ints[C]).Int().
//
// Operates on the reflect (general) register bank without the runtime range check.
//
// Soundness: the BCE pass MUST prove the index is in [0, len(general[B])) before emitting
// this opcode. An out-of-range access lets the underlying reflect.Value.Index call panic
// instead of routing the panic through vm.evalError.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection and index.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue on success or opPanicError if the collection is not a valid
// indexable value.
func handleSliceGetIntUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.B])
	if !ok {
		return indexPanicResult
	}
	element := unwrapInterfaceElement(collection.Index(int(registers.Ints[instruction.C])))
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		registers.Ints[instruction.A] = element.Int()
	default:
		registers.Ints[instruction.A] = safeconv.Uint64ToInt64Reinterpret(element.Uint())
	}
	return opContinue
}

// handleSliceSetIntUnchecked is the bounds-elided reflect-bank variant of
// handleSliceSetInt for general[A].Index(ints[B]).SetInt(ints[C]).
//
// Operates on the reflect (general) register bank without the runtime range check.
//
// Soundness: same proof obligation as handleSliceGetIntUnchecked.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the collection, index and value.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns opContinue on success or opPanicError if the collection is not a valid
// indexable value.
func handleSliceSetIntUnchecked(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection, indexPanicResult, ok := resolveIndexCollection(vm, registers.General[instruction.A])
	if !ok {
		return indexPanicResult
	}
	element := collection.Index(int(registers.Ints[instruction.B]))
	switch element.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		element.SetInt(registers.Ints[instruction.C])
	default:
		element.SetUint(safeconv.Int64ToUint64Reinterpret(registers.Ints[instruction.C]))
	}
	return opContinue
}
