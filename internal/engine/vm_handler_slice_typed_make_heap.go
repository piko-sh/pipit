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
	"unsafe"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// handleSubOpMakeSliceHeap builds a typed-bank slice on the Go heap.
//
// The Compiler selects this for locals that leave the frame, so the store barrier finds a
// heap-owned backing and never has to copy an arena slab.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the capacity and kind extension word.
// Takes registers (*Registers) which holds the length register and the destination bank.
// Takes instruction (instruction) which encodes the destination slot and length register.
//
// Returns opContinue on success, or opPanicError with vm.evalError set when the length or
// capacity is invalid, the allocation limit is exceeded, or the kind is not a typed bank.
func handleSubOpMakeSliceHeap(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	length := registers.Ints[instruction.C]
	capacity := registers.Ints[extensionWord.A]
	if result, refused := refuseMakeSlice(vm, int(length), int(capacity), typedSliceElementSize(isa.RegisterKind(extensionWord.B))); refused {
		return result
	}
	if vm.Limits.MaxAllocSize > 0 && int(capacity) > vm.Limits.MaxAllocSize {
		vm.evalError = fmt.Errorf(errMakeSliceLimitFmt,
			fault.ErrAllocationLimit, capacity, vm.Limits.MaxAllocSize)
		return opPanicError
	}
	if !storeHeapTypedSlice(registers, isa.RegisterKind(extensionWord.B), instruction.B, int(length), int(capacity)) {
		vm.evalError = fmt.Errorf("%w: MAKE_SLICE_HEAP kind %d is not a typed-slice bank", errInvalidOpcode, extensionWord.B)
		return opPanicError
	}
	return opContinue
}

// storeHeapTypedSlice allocates a heap slice of the bank's element type and stores its
// header in the destination slot.
//
// Defined on the six typed-slice banks only, which is what the false return states. A
// caller asking to store a heap slice into a scalar bank is a bug in the caller, not a
// missing arm.
//
// Takes registers (*Registers) which holds the typed-slice banks.
// Takes kind (isa.RegisterKind) which selects the bank.
// Takes slot (uint8) which is the destination register.
// Takes length (int) which sizes the new slice.
// Takes capacity (int) which sizes the new slice backing.
//
// Returns bool which is false when kind is not a typed-slice bank.
func storeHeapTypedSlice(registers *Registers, kind isa.RegisterKind, slot uint8, length, capacity int) bool {
	switch kind {
	case isa.RegisterSliceInt:
		registers.SlicesInt[slot] = make([]int64, length, capacity)
	case isa.RegisterSliceFloat:
		registers.slicesFloat[slot] = make([]float64, length, capacity)
	case isa.RegisterSliceString:
		registers.slicesString[slot] = make([]string, length, capacity)
	case isa.RegisterSliceBool:
		registers.slicesBool[slot] = make([]bool, length, capacity)
	case isa.RegisterSliceUint:
		registers.slicesUint[slot] = make([]uint64, length, capacity)
	case isa.RegisterSliceByte:
		registers.slicesByte[slot] = make([]byte, length, capacity)
	default:
		return false
	}
	return true
}

// typedSliceElementSize returns the element size in bytes of a typed-slice bank, so the
// Go-parity makeslice checks can size the request the way the runtime would.
//
// Takes kind (isa.RegisterKind) which names the typed-slice bank.
//
// Returns uintptr which is the element size; eight bytes for every bank that is not a
// typed slice, so the limit check stays conservative.
func typedSliceElementSize(kind isa.RegisterKind) uintptr {
	if kind == isa.RegisterSliceString {
		return unsafe.Sizeof("")
	}
	if kind == isa.RegisterSliceBool || kind == isa.RegisterSliceByte {
		return 1
	}
	return unsafe.Sizeof(int64(0))
}
