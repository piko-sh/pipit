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

// handleSubOpCopySliceIntDirect implements `copy(destination, source)` between two typed
// slicesInt headers without crossing through reflect.Copy.
//
// Takes frame (*CallFrame) whose programCounter is advanced past the extension word.
// Takes registers (*Registers) which provides the slicesInt banks.
// Takes instr (instruction) which encodes the destination slicesInt register B and the
// source slicesInt register C.
//
// Returns OpResult indicating the next execution step.
func handleSubOpCopySliceIntDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.SlicesInt[instr.B], registers.SlicesInt[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleSubOpCopySliceFloatDirect mirrors handleSubOpCopySliceIntDirect for slicesFloat.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpCopySliceFloatDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesFloat[instr.B], registers.slicesFloat[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleSubOpCopySliceStringDirect mirrors handleSubOpCopySliceIntDirect for
// slicesString.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpCopySliceStringDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesString[instr.B], registers.slicesString[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleSubOpCopySliceBoolDirect mirrors handleSubOpCopySliceIntDirect for slicesBool.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpCopySliceBoolDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesBool[instr.B], registers.slicesBool[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleSubOpCopySliceUintDirect mirrors handleSubOpCopySliceIntDirect for slicesUint.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpCopySliceUintDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesUint[instr.B], registers.slicesUint[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}

// handleSubOpCopySliceByteDirect mirrors handleSubOpCopySliceIntDirect for slicesByte.
//
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
// Takes instr (isa.Instruction) which is the current instruction.
//
// Returns opContinue.
func handleSubOpCopySliceByteDirect(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	count := copy(registers.slicesByte[instr.B], registers.slicesByte[instr.C])
	registers.Ints[extensionWord.A] = int64(count)
	return opContinue
}
