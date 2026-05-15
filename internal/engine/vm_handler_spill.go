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

// decodeSpillIndex reads the isa.OpExt instruction following a spill/reload opcode and
// returns the register file index for the spill slot. It advances the program counter
// past the extension.
//
// Takes frame (*CallFrame) which provides access to the instruction stream and program
// counter.
//
// Returns the register file index for the spill slot.
func decodeSpillIndex(frame *CallFrame) int {
	ext := readExtensionWord(frame)
	frame.ProgramCounter++
	return isa.SpillAreaOffset + isa.DecodeExtension24(ext)
}

// spillBankSize returns the length of the register bank addressed by the given register
// kind.
//
// Takes registers (*Registers) which holds the register banks.
// Takes kind (isa.RegisterKind) which selects the bank to measure.
//
// Returns the number of slots in the selected bank.
func spillBankSize(registers *Registers, kind isa.RegisterKind) int {
	switch kind {
	case isa.RegisterInt:
		return len(registers.Ints)
	case isa.RegisterFloat:
		return len(registers.Floats)
	case isa.RegisterString:
		return len(registers.Strings)
	case isa.RegisterGeneral:
		return len(registers.General)
	case isa.RegisterBool:
		return len(registers.Bools)
	case isa.RegisterUint:
		return len(registers.Uints)
	case isa.RegisterComplex:
		return len(registers.Complex)
	default:
		return 0
	}
}

// checkSpillIndex verifies that a decoded spill slot index falls within the register bank
// selected by instruction.b, setting an interpreted bounds error on the VM when it does
// not.
//
// Takes vm (*VM) which receives the bounds error.
// Takes frame (*CallFrame) which provides program-counter context.
// Takes registers (*Registers) which holds the register banks.
// Takes index (int) which is the decoded spill slot index.
// Takes kind (isa.RegisterKind) which selects the bank to bounds-check.
//
// Returns true when the index is in range, false otherwise.
func checkSpillIndex(vm *VM, frame *CallFrame, registers *Registers, index int, kind isa.RegisterKind) bool {
	size := spillBankSize(registers, kind)
	if index < 0 || index >= size {
		vMBoundsError(vm, frame, "spill slot", index, size)
		return false
	}
	return true
}

// handleSpill handles the isa.SubOpSpill instruction by copying a register value into the
// spill area of the register file.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the instruction stream.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and bank kind.
//
// Returns OpResult indicating the next execution step.
func handleSpill(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := decodeSpillIndex(frame)

	if !checkSpillIndex(vm, frame, registers, index, isa.RegisterKind(instruction.B)) {
		return opPanicError
	}
	switch isa.RegisterKind(instruction.B) {
	case isa.RegisterInt:
		registers.Ints[index] = registers.Ints[instruction.A]
	case isa.RegisterFloat:
		registers.Floats[index] = registers.Floats[instruction.A]
	case isa.RegisterString:
		registers.Strings[index] = registers.Strings[instruction.A]
	case isa.RegisterGeneral:
		registers.General[index] = registers.General[instruction.A]
	case isa.RegisterBool:
		registers.Bools[index] = registers.Bools[instruction.A]
	case isa.RegisterUint:
		registers.Uints[index] = registers.Uints[instruction.A]
	case isa.RegisterComplex:
		registers.Complex[index] = registers.Complex[instruction.A]
	default:
	}
	return opContinue
}

// handleReload handles the isa.SubOpReload instruction by copying a value from the spill
// area back into a directly-addressable register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the instruction stream.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand slots and bank kind.
//
// Returns OpResult indicating the next execution step.
func handleReload(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	index := decodeSpillIndex(frame)

	if !checkSpillIndex(vm, frame, registers, index, isa.RegisterKind(instruction.B)) {
		return opPanicError
	}
	switch isa.RegisterKind(instruction.B) {
	case isa.RegisterInt:
		registers.Ints[instruction.A] = registers.Ints[index]
	case isa.RegisterFloat:
		registers.Floats[instruction.A] = registers.Floats[index]
	case isa.RegisterString:
		registers.Strings[instruction.A] = registers.Strings[index]
	case isa.RegisterGeneral:
		registers.General[instruction.A] = registers.General[index]
	case isa.RegisterBool:
		registers.Bools[instruction.A] = registers.Bools[index]
	case isa.RegisterUint:
		registers.Uints[instruction.A] = registers.Uints[index]
	case isa.RegisterComplex:
		registers.Complex[instruction.A] = registers.Complex[index]
	default:
	}
	return opContinue
}
