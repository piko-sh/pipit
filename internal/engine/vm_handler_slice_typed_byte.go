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

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

const (
	// sliceBoundsOutOfRangeFormat is the Go runtime's slice-bounds panic format, reused for
	// the three-argument slice form across every typed-slice handler.
	sliceBoundsOutOfRangeFormat = "runtime error: slice bounds out of range [%d:%d:%d] with capacity %d"
)

// handleSubOpMakeSliceByte creates a typed []byte slice.
//
// Performs slicesByte[B] = make([]byte, ints[C], ints[ext.A]). Routes the backing through
// vm.arena.AllocByteBacking - the same byte slab arenaMakeSliceBacking uses for the
// general-bank path - so byte allocation stays arena-resident even in the typed bank. The
// length>0 guard restores Go's zero-initialisation guarantee for the live prefix; the
// bump allocator returns whatever the slab last held.
//
// Takes vm (*VM) which provides the arena and reports panics.
// Takes frame (*CallFrame) which advances the PC over the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which carries B and C operands.
//
// Returns opContinue on success, opPanicError when length or capacity is invalid or
// exceeds the configured allocation limit.
func handleSubOpMakeSliceByte(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	length := registers.Ints[instruction.C]
	capacity := registers.Ints[extensionWord.A]
	if result, refused := refuseMakeSlice(vm, int(length), int(capacity), 1); refused {
		return result
	}
	if vm.Limits.MaxAllocSize > 0 && int(capacity) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(errMakeSliceLimitFmt,
			fault.ErrAllocationLimit, capacity, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	backing := vm.Arena.AllocByteBacking(int(capacity))
	if length > 0 {
		clear(backing[:length])
	}
	registers.slicesByte[instruction.B] = backing[:length:capacity]
	return opContinue
}

// handleSubOpBoxSliceByte boxes a typed []byte into the general bank.
//
// Writes general[B] = reflect.ValueOf(slicesByte[C]). Used at boundaries with
// reflect-bank consumers (native call, map value, interface conversion, container
// insert).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which carries B and C operands.
//
// Returns opContinue.
func handleSubOpBoxSliceByte(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.B] = reflect.ValueOf(registers.slicesByte[instruction.C])
	return opContinue
}

// handleSubOpSliceByteToString converts a typed []byte to a string.
//
// Writes strings[B] = string(slicesByte[C]) through the arena's byte slab via
// arenaBytesToString(), the same allocation path the general-bank
// handleFlatSubOpBytesToString() uses.
//
// Takes vm (*VM) which provides the arena.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which carries B and C operands.
//
// Returns opContinue.
func handleSubOpSliceByteToString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.B] = arenaBytesToString(vm.Arena, registers.slicesByte[instruction.C])
	return opContinue
}
