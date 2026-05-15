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

	"pipit.sh/pipit/internal/isa"
)

const (
	// errMakeSliceLimitFmt formats the diagnostic raised when a typed slice make exceeds the
	// configured maxAllocSize limit.
	errMakeSliceLimitFmt = "%w: make slice length %d exceeds limit %d"

	// jumpOffsetByteB is the bit position of the second byte of a 24-bit signed jump offset
	// packed into the (a, b, c) operand triple of an extension word. The first byte is at
	// position 0, the third at 16.
	jumpOffsetByteB = 8

	// jumpOffsetByteC is the bit position of the third byte of a 24-bit signed jump offset
	// packed into the (a, b, c) operand triple of an extension word.
	jumpOffsetByteC = 16
)

// handleSliceGetIntDirect handles the isa.OpSliceGetIntDirect instruction by reading an
// int64 element from a slicesInt bank entry without crossing the reflect.Value boundary.
// This is the typed-storage replacement for handleSliceGetInt; the latter retains its
// place for general-bank slices whose element kind is int but whose storage form is
// reflect.Value.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the slicesInt collection and the int index.
// Takes instruction (instruction) which encodes the destination int register A, the
// source slicesInt register B, and the index int register C.
//
// Returns OpResult indicating the next execution step.
func handleSliceGetIntDirect(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.SlicesInt[instruction.B]
	index := registers.Ints[instruction.C]
	if uint64(index) >= uint64(len(slice)) { //nolint:gosec // unsigned bounds check
		return raiseIndexOutOfRange(vm, int(index), len(slice))
	}
	registers.Ints[instruction.A] = slice[index]
	return opContinue
}

// handleSliceSetIntDirect handles the isa.OpSliceSetIntDirect instruction by writing an
// int64 value to a slicesInt bank entry without crossing the reflect.Value boundary.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the slicesInt collection, the index, and the
// value.
// Takes instruction (instruction) which encodes the destination slicesInt register A, the
// index int register B, and the value int register C.
//
// Returns OpResult indicating the next execution step.
func handleSliceSetIntDirect(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.SlicesInt[instruction.A]
	index := registers.Ints[instruction.B]
	if uint64(index) >= uint64(len(slice)) { //nolint:gosec // unsigned bounds check
		return raiseIndexOutOfRange(vm, int(index), len(slice))
	}
	slice[index] = registers.Ints[instruction.C]
	return opContinue
}

// handleSubOpBoxSliceInt boxes a typed []int64 slice from slicesInt into a reflect.Value
// in the general bank: general[B] = reflect.ValueOf(slicesInt[C]). Used at boundaries
// where a typed slice meets a reflect-bank consumer (native function call, map value,
// interface conversion, container insert).
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which holds the source slicesInt and destination general
// banks.
// Takes instruction (instruction) which encodes the destination general register B and
// the source slicesInt register C.
//
// Returns OpResult indicating the next execution step.
func handleSubOpBoxSliceInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = boxTypedIntSlice(vm, registers.SlicesInt[instruction.C])
	return opContinue
}

// boxTypedIntSlice wraps a typed int-bank slice as a general-bank value of the
// script-level type []int, aliasing the same backing array.
//
// Sound only because the bank's element type and int have the same 64-bit layout.
//
// Takes vm (*VM) which supplies the arena's slice-header slab.
// Takes slice ([]int64) which is the typed-bank slice to box.
//
// Returns reflect.Value of type []int aliasing slice's backing array.
func boxTypedIntSlice(vm *VM, slice []int64) reflect.Value {
	if vm == nil {
		return reflect.ValueOf(slice)
	}
	return packTypedSliceToGeneral(vm.Arena, slice, intSliceReflectType)
}
