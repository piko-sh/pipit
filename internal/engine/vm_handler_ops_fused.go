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

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
)

// handleEqualInt64ConstantThenJumpIfFalse compares a register against an integer constant
// and branches when equality is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqualInt64ConstantThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] != constantValue)
}

// handleEqualInt64ConstantThenJumpIfTrue compares a register against an integer constant
// and branches when equality is true.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqualInt64ConstantThenJumpIfTrue(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] == constantValue)
}

// handleGreaterOrEqualInt64ConstantThenJumpIfFalse compares a register against an integer
// constant and branches when the greater-or-equal condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleGreaterOrEqualInt64ConstantThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] < constantValue)
}

// handleGreaterThanInt64ConstantThenJumpIfFalse compares a register against an integer
// constant and branches when the greater-than condition is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleGreaterThanInt64ConstantThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Ints[instruction.B] <= constantValue)
}

// handleAddInt64ConstantThenJump adds an integer constant to a register value and then
// unconditionally branches by the extension word offset.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleAddInt64ConstantThenJump(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] + frame.Function.IntConstants[instruction.C]
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
	frame.ProgramCounter += int(offset)
	return opContinue
}

// handleMultiplyInt64ConstantThenAdd multiplies a signed integer register by a constant
// and adds the register its extension word names.
//
// Both registers are read before the destination is written, so the destination may be
// either of them.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, the multiplied register
// and the constant pool index.
//
// Returns OpResult indicating the next execution step.
func handleMultiplyInt64ConstantThenAdd(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	registers.Ints[instruction.A] = registers.Ints[extensionWord.A] + registers.Ints[instruction.B]*frame.Function.IntConstants[instruction.C]
	return opContinue
}

// handleIncrementInt64ThenJumpIfLessThanBody increments a signed integer register and
// branches if the result is less than a comparison register.
//
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the target and comparison register
// indices.
//
// Returns OpResult indicating the next execution step.
func handleIncrementInt64ThenJumpIfLessThanBody(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A]++
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	if registers.Ints[instruction.A] < registers.Ints[instruction.B] {
		offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleGetSliceByteElementInt64 reads one byte of a byte slice into an int register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination register in B and the
// slice register in C.
//
// Returns OpResult indicating the next execution step.
func handleGetSliceByteElementInt64(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	slice := registers.slicesByte[instruction.C]
	index := registers.Ints[extensionWord.A]
	if index < 0 || index >= int64(len(slice)) {
		return raiseIndexOutOfRange(vm, int(index), len(slice))
	}
	registers.Ints[instruction.B] = int64(slice[index])
	return opContinue
}

// handleGetStructFieldUint64Run makes the snapshot move and the unsigned field reads of
// one isa.SubOpGetStructFieldUint64Run.
//
// The program counter is stepped to each read's word before the read is made, so a panic
// is reported at the read that raised it.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the struct's register in B and the
// register it is first moved from in C.
//
// Returns OpResult indicating the next execution step.
func handleGetStructFieldUint64Run(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	first := readExtensionWord(frame)
	base := frame.ProgramCounter
	words, reads := fieldRunWords(frame, first, 1)
	if words == nil {
		return raiseTruncatedRun(vm, frame, reads)
	}
	if instruction.B != instruction.C {
		move := isa.NewInstruction(isa.OpMoveGeneral, instruction.B, instruction.C, first.C>>isa.FieldRunModeShift)
		if result := handleMoveGeneral(vm, frame, registers, move); result != opContinue {
			return result
		}
	}
	return readUintFieldRun(vm, frame, registers, instruction.B, words, base)
}

// fieldRunWords returns the extension words of a field run, one for each read.
//
// Takes frame (*CallFrame) whose program counter is on the first of them.
// Takes first (isa.Instruction) which is that word, holding the number of reads.
// Takes fewest (int) which is the least number of reads the run is taken to make.
//
// Returns words ([]isa.Instruction) which are the run's words, or nil when the body ends
// before the last of them.
// Returns reads (int) which is the number of reads, kept within the run's bounds.
func fieldRunWords(frame *CallFrame, first isa.Instruction, fewest int) (words []isa.Instruction, reads int) {
	reads = min(max(int(first.C&isa.FieldRunCountMask), fewest), isa.FieldRunMaxReads)
	base := frame.ProgramCounter
	if base+reads > len(frame.Function.Body) {
		return nil, reads
	}
	return frame.Function.Body[base : base+reads], reads
}

// readUintFieldRun makes the unsigned field reads of a run from the struct in a general
// register, stepping the program counter to each read's word before making it.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which holds the layout table and the program counter.
// Takes registers (*Registers) which holds the struct and receives the fields.
// Takes receiver (uint8) which is the general register holding the struct.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each read.
// Takes base (int) which is the program counter of the first of them.
//
// Returns OpResult indicating the next execution step.
func readUintFieldRun(vm *VM, frame *CallFrame, registers *Registers, receiver uint8, words []isa.Instruction, base int) OpResult {
	if readUintFieldRunDirect(frame, registers, receiver, words, base) {
		return opContinue
	}
	if readFieldRunOfOneStruct(frame, registers, receiver, words) {
		frame.ProgramCounter = base + len(words)
		return opContinue
	}
	for ordinal, word := range words {
		frame.ProgramCounter = base + ordinal + 1
		read := isa.NewInstruction(isa.OpGetStructFieldUint64, word.A, receiver, word.B)
		if result := handleGetStructFieldUint64(vm, frame, registers, read); result != opContinue {
			return result
		}
	}
	return opContinue
}

// handleGetElementFieldUint64Run makes the element read and the unsigned field reads of
// one isa.SubOpGetElementFieldUint64Run.
//
// A debugger uses the separate element-read and field-run path so it can show the element
// register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the collection's register in B and the
// index's in C.
//
// Returns OpResult indicating the next execution step.
func handleGetElementFieldUint64Run(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	first := readExtensionWord(frame)
	base := frame.ProgramCounter
	words, reads := fieldRunWords(frame, first, isa.FieldRunMinReads)
	if words == nil {
		return raiseTruncatedRun(vm, frame, reads)
	}
	if vm.Limits.Debug == nil &&
		readStructElementUintFields(frame, registers, &registers.General[instruction.B], registers.Ints[instruction.C], words, base) {
		return opContinue
	}
	receiver := words[isa.ElementFieldRunScratchWord].C
	element := isa.NewInstruction(isa.OpGetElement, receiver, instruction.B, instruction.C)
	if result := handleGetElement(vm, frame, registers, element); result != opContinue {
		return result
	}
	if mode := first.C >> isa.FieldRunModeShift; mode != MoveGeneralModeAlias {
		if result := handleMoveGeneral(vm, frame, registers, isa.NewInstruction(isa.OpMoveGeneral, receiver, receiver, mode)); result != opContinue {
			return result
		}
	}
	return readUintFieldRun(vm, frame, registers, receiver, words, base)
}

// readFieldRunOfOneStruct reads fields after resolving the struct once through reflect,
// for a struct whose storage cannot be addressed directly.
//
// It writes nothing unless it makes every read.
//
// Takes frame (*CallFrame) which holds the layout table.
// Takes registers (*Registers) which holds the struct and receives the fields.
// Takes receiver (uint8) which is the general register holding the struct.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each read.
//
// Returns bool which is true when every read was made.
func readFieldRunOfOneStruct(frame *CallFrame, registers *Registers, receiver uint8, words []isa.Instruction) bool {
	value := registers.General[receiver]
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return false
	}
	fields, layouts := value.NumField(), frame.Function.StructLayoutTable
	for _, word := range words {
		if int(word.B) >= len(layouts) || layouts[word.B].PathLength != 1 || int(layouts[word.B].Path[0]) >= fields {
			return false
		}
	}
	for _, word := range words {
		registers.Uints[word.A] = value.Field(int(layouts[word.B].Path[0])).Uint()
	}
	return true
}

// handleSetSliceByteElementRun stores two to four bytes at consecutive positions of a
// byte slice.
//
// A run that fits is checked once and stored through a window of the slice. One that does
// not fit is stored a byte at a time (storeByteRunChecked()), so that the stores before
// the failing one are made.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the slice register in B and the index
// register in C.
//
// Returns OpResult indicating the next execution step.
func handleSetSliceByteElementRun(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	first := readExtensionWord(frame)
	stores := min(max(int(first.C), 1), isa.ByteRunMaxStores)
	base := frame.ProgramCounter
	if base+stores > len(frame.Function.Body) {
		return raiseTruncatedRun(vm, frame, stores)
	}
	words := frame.Function.Body[base : base+stores]
	frame.ProgramCounter += stores
	slice := registers.slicesByte[instruction.B]
	position := registers.Ints[instruction.C] + int64(first.B)
	if position < 0 || position > int64(len(slice))-int64(stores) {
		return storeByteRunChecked(vm, frame, registers, slice, position, words, base)
	}
	window := slice[position : position+int64(stores)]
	for store := range window {
		window[store] = safeconv.Uint64ToByteTruncate(registers.Uints[words[store].A])
	}
	return opContinue
}

// storeByteRunChecked makes the stores of a byte run one at a time, each bounds-checked
// on its own.
//
// The panic is reported at the failing store's word, whose source line is that store's.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the running frame.
// Takes registers (*Registers) which holds the values stored.
// Takes slice ([]byte) which is the slice stored into.
// Takes position (int64) which is the position of the first store.
// Takes words ([]isa.Instruction) which are the run's extension words, one for each
// store.
// Takes base (int) which is the program counter of the first word.
//
// Returns OpResult which is a panic when a store is out of range.
func storeByteRunChecked(vm *VM, frame *CallFrame, registers *Registers, slice []byte, position int64, words []isa.Instruction, base int) OpResult {
	for store, word := range words {
		index := position + int64(store)
		if index < 0 || index >= int64(len(slice)) {
			frame.ProgramCounter = base + store + 1
			return raiseIndexOutOfRange(vm, int(index), len(slice))
		}
		slice[index] = safeconv.Uint64ToByteTruncate(registers.Uints[word.A])
	}
	return opContinue
}

// handleIncrementInt64ThenJumpIfLessThanConstant increments a signed integer register and
// branches while the result is less than an integer constant.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the counter register in B and the
// constant pool index in C.
//
// Returns OpResult indicating the next execution step.
func handleIncrementInt64ThenJumpIfLessThanConstant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := intConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	registers.Ints[instruction.B]++
	return conditionalJump(frame, registers.Ints[instruction.B] < constantValue)
}

// handleLessThanLengthStringThenJumpIfFalseBody fuses an int < len(string) comparison
// with a conditional jump.
//
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which provides the integer and string register banks.
// Takes instruction (instruction) which encodes the counter and string register indices.
//
// Returns OpResult indicating the next execution step.
func handleLessThanLengthStringThenJumpIfFalseBody(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	if registers.Ints[instruction.A] >= int64(len(registers.Strings[instruction.B])) {
		offset := isa.JoinOffset(extensionWord.A, extensionWord.B)
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleMultiplyInt64Constant multiplies a register value by an integer constant from the
// function constant pool.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the function constant pool.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the destination, source, and constant
// pool index.
//
// Returns OpResult indicating the next execution step.
func handleMultiplyInt64Constant(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if int(instruction.C) >= len(frame.Function.IntConstants) {
		vMBoundsError(vm, frame, boundsTableIntConstant, int(instruction.C), len(frame.Function.IntConstants))
		return opPanicError
	}
	registers.Ints[instruction.A] = registers.Ints[instruction.B] * frame.Function.IntConstants[instruction.C]
	return opContinue
}

// handleEqualStringConstantThenJumpIfFalse compares a string register against a string
// constant and branches when equality is false.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides access to the bytecode body and program
// counter.
// Takes registers (*Registers) which provides the string register banks.
// Takes instruction (instruction) which encodes the source register and constant pool
// index.
//
// Returns OpResult indicating the next execution step.
func handleEqualStringConstantThenJumpIfFalse(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	constantValue, errResult, ok := stringConstBoundsCheck(vm, frame, instruction)
	if !ok {
		return errResult
	}
	return conditionalJump(frame, registers.Strings[instruction.B] != constantValue)
}

// handleBoxInt64Body boxes a signed integer register value into a reflect.Value in a
// general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the integer and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBoxInt64Body(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = boxInt64ToGeneral(vm.Arena, registers.Ints[instruction.B])
	return opContinue
}

// handleUnboxInt64Body unboxes an integer from a general register reflect.Value into a
// signed integer register.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleUnboxInt64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).Int()
	return opContinue
}

// handleBoxFloat64Body boxes a floating-point register value into a reflect.Value in a
// general register.
//
// Takes vm (*VM) which is the virtual machine.
// Takes registers (*Registers) which provides the float and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBoxFloat64Body(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = boxFloat64ToGeneral(vm.Arena, registers.Floats[instruction.B])
	return opContinue
}

// handleUnboxFloat64Body unboxes a float from a general register reflect.Value into a
// float register.
//
// Takes registers (*Registers) which provides the general and float register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleUnboxFloat64Body(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).Float()
	return opContinue
}

// handleBoxStringBody boxes a string register value into a reflect.Value in a general
// register.
//
// Takes vm (*VM) which provides the arena allocator for the string box slab. The packed
// string stays arena-backed until the store barrier detaches it on escape.
// Takes registers (*Registers) which provides the string and general register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleBoxStringBody(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = BoxStringToGeneral(vm.Arena, registers.Strings[instruction.B])
	return opContinue
}

// handleUnboxStringBody unboxes a string from a general register reflect.Value into a
// string register.
//
// Takes registers (*Registers) which provides the general and string register banks.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleUnboxStringBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = unwrapInterfaceElement(registers.General[instruction.B]).String()
	return opContinue
}

// handleJumpIfNil tests whether a general register holds nil and branches if the value is
// nil or invalid.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and branch offset
// operands.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfNil(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	offset := instruction.SignedOffset()
	if !v.IsValid() || isNilableAndNil(v) {
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// handleJumpIfNotNil tests whether a general register holds nil and branches if the value
// is non-nil and valid.
//
// Takes frame (*CallFrame) which provides access to the program counter.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the source register and branch offset
// operands.
//
// Returns OpResult indicating the next execution step.
func handleJumpIfNotNil(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	offset := instruction.SignedOffset()
	if v.IsValid() && !isNilableAndNil(v) {
		frame.ProgramCounter += int(offset)
	}
	return opContinue
}

// raiseTruncatedRun reports a run instruction whose extension words exceed the body.
//
// Takes vm (*VM) which receives the error.
// Takes frame (*CallFrame) which is the running frame.
// Takes words (int) which is the number of words the run counts.
//
// Returns OpResult which is opPanicError.
func raiseTruncatedRun(vm *VM, frame *CallFrame, words int) OpResult {
	vm.evalError = newInvariantError("run instruction at pc %d of %s counts %d words past the end of its body",
		frame.ProgramCounter-1, frame.Function.Name, words)
	return opPanicError
}
