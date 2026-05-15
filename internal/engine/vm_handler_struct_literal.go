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

// handleSubOpAllocStructLiteral allocates a zeroed struct literal into general[C].
//
// Reads the struct type from the TypeTable index carried by the extension word. On the
// first execution in a function it also builds the struct-literal table so later calls
// stay in assembly.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the extension word and type table.
// Takes registers (*Registers) which holds the destination general bank.
// Takes instruction (instruction) which carries the destination register in C.
//
// Returns OpResult indicating whether execution continues or an error was raised.
func handleSubOpAllocStructLiteral(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	if reflectType.Kind() != reflect.Struct {
		vm.evalError = errStructLiteralTypeNotStruct
		return opPanicError
	}
	EnsureStructLiteralTable(frame.Function)
	vm.StructLiteralGoAllocations++
	registers.General[instruction.C] = allocateStructLiteralValue(vm, reflectType)
	return opContinue
}
