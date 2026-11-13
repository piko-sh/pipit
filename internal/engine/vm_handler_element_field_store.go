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

import "pipit.sh/pipit/internal/isa"

// handleSetElementFieldInt64 implements isa.OpSetElementFieldInt64 as the element read
// and the wide field store it stands for.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) whose program counter is on the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the value's register in A, the
// slice's in B and the index's in C.
//
// Returns OpResult indicating the next execution step.
func handleSetElementFieldInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return storeElementField(vm, frame, registers, instruction, isa.SubOpSetStructFieldInt64Wide, handleSetStructFieldInt64Wide)
}

// handleSetElementFieldFloat64 implements isa.OpSetElementFieldFloat64 as the element
// read and the wide field store it stands for.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) whose program counter is on the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the value's register in A, the
// slice's in B and the index's in C.
//
// Returns OpResult indicating the next execution step.
func handleSetElementFieldFloat64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	return storeElementField(vm, frame, registers, instruction, isa.SubOpSetStructFieldFloat64Wide, handleSetStructFieldFloat64Wide)
}

// storeElementField reads the element a fused field store writes into and then stores the
// field, through the two handlers the fused operation replaces.
//
// The element passes through the general register the extension word names in its byte C.
// That register is given back what it held once the store has succeeded, so the operation
// writes no register, which is what its shape says and what the assembly body does. A
// store that fails leaves the element there, in a frame that is then unwound. The
// extension word is left unread for the field store, which finds the layout index in it.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) whose program counter is on the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (isa.Instruction) which encodes the value's register in A, the
// slice's in B and the index's in C.
// Takes store (isa.SubOpcode) which is the wide field store of the value's bank.
// Takes storeField (opcodeHandler) which is that store's handler.
//
// Returns OpResult which is opContinue unless the read fails as isa.OpGetElement fails or
// the store fails as the wide store fails.
func storeElementField(
	vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, store isa.SubOpcode, storeField opcodeHandler,
) OpResult {
	element := readExtensionWord(frame).C
	held := registers.General[element]
	read := isa.NewInstruction(isa.OpGetElement, element, instruction.B, instruction.C)
	if result := handleGetElement(vm, frame, registers, read); result != opContinue {
		return result
	}
	result := storeField(vm, frame, registers, isa.NewTier1Instruction(store, element, instruction.A))
	if result == opContinue {
		registers.General[element] = held
	}
	return result
}
