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
	"math"
	"reflect"
	"strconv"

	"piko.sh/vectormaths"
	"pipit.sh/pipit/internal/isa"
)

// handleFlatSubOpMathSin sets floats[B] = math.Sin(floats[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMathSin(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = math.Sin(registers.Floats[instr.C])
	return opContinue
}

// handleFlatSubOpMathCos sets floats[B] = math.Cos(floats[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMathCos(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = math.Cos(registers.Floats[instr.C])
	return opContinue
}

// handleFlatSubOpMathExp sets floats[B] = math.Exp(floats[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMathExp(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = math.Exp(registers.Floats[instr.C])
	return opContinue
}

// handleFlatSubOpMathTan sets floats[B] = math.Tan(floats[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMathTan(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = math.Tan(registers.Floats[instr.C])
	return opContinue
}

// handleFlatSubOpMathMod sets floats[B] = math.Mod(floats[C], floats[ext.a]).
//
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMathMod(_ *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	registers.Floats[instr.B] = math.Mod(registers.Floats[instr.C], registers.Floats[extension.A])
	return opContinue
}

// handleFlatSubOpStrconvFormatBool sets strings[B] = strconv.FormatBool(bools[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpStrconvFormatBool(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Strings[instr.B] = strconv.FormatBool(registers.Bools[instr.C])
	return opContinue
}

// handleFlatSubOpStrconvFormatInt formats ints[C] with the supplied base.
//
// Takes vm (*VM) which owns the arena and panic state.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpStrconvFormatInt(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	base := int(registers.Ints[extension.A])
	if base < minStrconvBase || base > maxStrconvBase {
		vm.evalError = newRuntimePanicError("strconv: illegal AppendInt/FormatInt base %d", base)
		return opPanicError
	}
	registers.Strings[instr.B] = arenaFormatIntString(vm.Arena, registers.Ints[instr.C], base)
	return opContinue
}

// handleFlatSubOpStrconvItoa sets strings[B] = strconv.Itoa(ints[C]).
//
// Takes vm (*VM) which owns the arena allocator.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpStrconvItoa(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Strings[instr.B] = arenaItoaString(vm.Arena, registers.Ints[instr.C])
	return opContinue
}

// handleFlatSubOpRealComplex sets floats[B] = real(complex[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpRealComplex(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = real(registers.Complex[instr.C])
	return opContinue
}

// handleFlatSubOpImagComplex sets floats[B] = imag(complex[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpImagComplex(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Floats[instr.B] = imag(registers.Complex[instr.C])
	return opContinue
}

// handleFlatSubOpBytesToString sets strings[B] = string(general[C].Bytes()).
//
// Takes vm (*VM) which owns the arena allocator.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpBytesToString(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Strings[instr.B] = arenaBytesToString(vm.Arena, registers.General[instr.C].Bytes())
	return opContinue
}

// handleFlatSubOpCap sets ints[B] = cap(general[C]).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpCap(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Ints[instr.B] = collectionLengthOrCap(registers.General[instr.C], reflect.Value.Cap)
	return opContinue
}

// handleFlatSubOpNegComplex sets complex[B] = -complex[C].
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpNegComplex(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Complex[instr.B] = -registers.Complex[instr.C]
	return opContinue
}

// handleFlatSubOpMoveComplex sets complex[B] = complex[C].
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpMoveComplex(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Complex[instr.B] = registers.Complex[instr.C]
	return opContinue
}

// handleFlatSubOpLoadIntConstSmall sets ints[B] = int64(C).
//
// Shared with isa.SubOpLoadBool which also writes a 0/1 int.
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpLoadIntConstSmall(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Ints[instr.B] = int64(instr.C)
	return opContinue
}

// handleFlatSubOpLoadUintConstSmall sets uints[B] = uint64(C).
//
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleFlatSubOpLoadUintConstSmall(_ *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	registers.Uints[instr.B] = uint64(instr.C)
	return opContinue
}

// SIMD sub-op handlers run their kernels and return OpResult directly; reaching the
// handler implies the dispatch matched.

// handleFlatSubOpSimdDotProductFloat64 accumulates the bounded dot product.
//
// floats[B] += dotProduct(slicesFloat[C], slicesFloat[ext.a])[:ints[ext.b]].
//
// Takes vm (*VM) which owns panic state.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpSimdDotProductFloat64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	a, b, count, ok := simdReadDualSliceWithCount(vm, registers, instr.C, extension.A, extension.B)
	if !ok {
		return opPanicError
	}
	registers.Floats[instr.B] += simdDotProductFloat64(a[:count], b[:count])
	return opContinue
}

// handleFlatSubOpSimdSumSliceFloat64 accumulates the bounded slice sum.
//
// floats[B] += sum(slicesFloat[C])[:ints[ext.a]].
//
// Takes vm (*VM) which owns panic state.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpSimdSumSliceFloat64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	slice, count, ok := simdReadSliceWithCount(vm, registers, instr.C, extension.A)
	if !ok {
		return opPanicError
	}
	registers.Floats[instr.B] += simdSumFloat64(slice[:count])
	return opContinue
}

// handleFlatSubOpSimdAddSliceFloat64 vectorises destination[i] = a[i] + b[i].
//
// Takes vm (*VM) which owns panic state.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpSimdAddSliceFloat64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	plan := simdReadElementwiseFloat64(vm, registers, instr.B, instr.C, extension.A, extension.B)
	if !plan.ok {
		return opPanicError
	}
	destination := plan.destination[:plan.count]
	a := plan.a[:plan.count]
	b := plan.b[:plan.count]

	if float64SlicesOverlap(destination, a) || float64SlicesOverlap(destination, b) {
		addF64ScalarForward(destination, a, b)
		return opContinue
	}
	vectormaths.AddF64(destination, a, b)
	return opContinue
}

// handleFlatSubOpSimdScaleSliceFloat64 vectorises s[i] *= alpha.
//
// Takes vm (*VM) which owns panic state.
// Takes frame (*CallFrame) which supplies the extension word.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpSimdScaleSliceFloat64(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	extension := readExtensionWord(frame)
	frame.ProgramCounter++
	slice, count, ok := simdReadSliceWithCount(vm, registers, instr.B, extension.A)
	if !ok {
		return opPanicError
	}
	simdScaleFloat64(slice[:count], registers.Floats[instr.C])
	return opContinue
}

// handleFlatSubOpAdoptGeneralToSlicesFloat unboxes a []float64 into the typed bank.
//
// Takes vm (*VM) which owns panic state.
// Takes registers (*Registers) which holds the register banks.
// Takes instr (instruction) which encodes the operand indices.
//
// Returns OpResult which signals continued dispatch or a panic.
func handleFlatSubOpAdoptGeneralToSlicesFloat(vm *VM, _ *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	value := registers.General[instr.C]
	if !value.IsValid() {
		raiseNativePanicAsInterpreted(vm, newRuntimePanicError("simd adopt: source general register holds an invalid reflect.Value"))
		return opPanicError
	}
	slice, ok := reflect.TypeAssert[[]float64](value)
	if !ok {
		raiseNativePanicAsInterpreted(vm, newRuntimePanicError("simd adopt: expected []float64, got %s", value.Type()))
		return opPanicError
	}
	registers.slicesFloat[instr.B] = slice
	return opContinue
}
