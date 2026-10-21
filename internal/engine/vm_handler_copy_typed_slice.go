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

// handleCopySliceInt64 implements `copy(destination, source)` between two typed slicesInt
// headers without crossing through reflect.Copy.
//
// Takes frame (*CallFrame) whose programCounter is advanced past the extension word.
// Takes registers (*Registers) which provides the slicesInt banks.
// Takes instr (instruction) which encodes the destination slicesInt register B and the
// source slicesInt register C.
//
// Returns OpResult indicating the next execution step.
func handleCopySliceInt64(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.SlicesInt[instr.B], registers.SlicesInt[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleCopySliceFloat64 mirrors handleCopySliceInt64 for slicesFloat.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleCopySliceFloat64(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesFloat[instr.B], registers.slicesFloat[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleCopySliceString mirrors handleCopySliceInt64 for slicesString.
//
// Takes vm (*VM) which is the VM executing the instruction.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleCopySliceString(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesString[instr.B], registers.slicesString[instr.C])
	if vm.Arena.countsHandOffs() {
		noteStringCopy(vm.Arena, registers.slicesString[instr.B][:count])
	}
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleCopySliceBool mirrors handleCopySliceInt64 for slicesBool.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleCopySliceBool(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesBool[instr.B], registers.slicesBool[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleCopySliceUint64 mirrors handleCopySliceInt64 for slicesUint.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleCopySliceUint64(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesUint[instr.B], registers.slicesUint[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleCopySliceByte mirrors handleCopySliceInt64 for slicesByte.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleCopySliceByte(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesByte[instr.B], registers.slicesByte[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}
