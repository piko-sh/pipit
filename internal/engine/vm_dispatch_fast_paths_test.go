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
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type fastPath func(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) bool

type fastPathCase struct {
	fast          fastPath
	handler       opcodeHandler
	prepare       func(registers *Registers)
	arrange       func(vm *VM, frame *CallFrame)
	name          string
	intConstants  []int64
	uintConstants []uint64
	instruction   isa.Instruction
	extension     isa.Instruction
	wantTaken     bool
}

type fastPathState struct {
	ints           []int64
	uints          []uint64
	floats         []float64
	bools          []bool
	bytes          [][]byte
	intSlices      [][]int64
	strings        []string
	generals       []any
	globalInts     []int64
	programCounter int
}

type fastPathNamedBytes []byte

func newFastPathFrame(t *testing.T, testCase *fastPathCase) (*VM, *CallFrame, *Registers) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.intConstants = testCase.intConstants
	builder.uintConstants = testCase.uintConstants
	builder.body = append(builder.body, testCase.instruction, testCase.extension)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	vm, frame, registers := newFramedVM(t, builder.build())
	frame.ProgramCounter = 1
	if testCase.prepare != nil {
		testCase.prepare(registers)
	}
	if testCase.arrange != nil {
		testCase.arrange(vm, frame)
	}
	return vm, frame, registers
}

func captureFastPathState(vm *VM, frame *CallFrame, registers *Registers) fastPathState {
	state := fastPathState{
		ints:           slices.Clone(registers.Ints),
		uints:          slices.Clone(registers.Uints),
		floats:         slices.Clone(registers.Floats),
		bools:          slices.Clone(registers.Bools),
		bytes:          make([][]byte, 0, len(registers.slicesByte)),
		intSlices:      make([][]int64, 0, len(registers.SlicesInt)),
		strings:        slices.Clone(registers.Strings),
		generals:       make([]any, 0, len(registers.General)),
		globalInts:     slices.Clone(vm.Globals.ints),
		programCounter: frame.ProgramCounter,
	}
	for _, slice := range registers.slicesByte {
		state.bytes = append(state.bytes, slices.Clone(slice))
	}
	for _, slice := range registers.SlicesInt {
		state.intSlices = append(state.intSlices, slices.Clone(slice))
	}
	for _, value := range registers.General {
		state.generals = append(state.generals, captureGeneral(value))
	}
	return state
}

func captureGeneral(value reflect.Value) any {
	switch {
	case !value.IsValid():
		return nil
	case !value.CanInterface():
		return fmt.Sprint(value)
	case value.Kind() == reflect.Slice:
		return reflect.AppendSlice(reflect.MakeSlice(value.Type(), 0, value.Len()), value).Interface()
	default:
		return value.Interface()
	}
}

func compareJumpCases(name string, subOpcode isa.SubOpcode, fast fastPath, handler opcodeHandler, jumpsOn, fallsOn int64) []fastPathCase {
	instruction := isa.NewTier1Instruction(subOpcode, 1, 0)
	withValue := func(value int64) func(*Registers) {
		return func(registers *Registers) { registers.Ints[1] = value }
	}
	return []fastPathCase{
		{name: name + " taking the jump", fast: fast, handler: handler, instruction: instruction, extension: jumpExtension(11),
			intConstants: []int64{10}, prepare: withValue(jumpsOn), wantTaken: true},
		{name: name + " taking a backward jump", fast: fast, handler: handler, instruction: instruction, extension: jumpExtension(-300),
			intConstants: []int64{10}, prepare: withValue(jumpsOn), wantTaken: true},
		{name: name + " falling through", fast: fast, handler: handler, instruction: instruction, extension: jumpExtension(11),
			intConstants: []int64{10}, prepare: withValue(fallsOn), wantTaken: true},
		{name: name + " with its constant outside the pool", fast: fast, handler: handler, instruction: isa.NewTier1Instruction(subOpcode, 1, 4),
			extension: jumpExtension(11), intConstants: []int64{10}, prepare: withValue(jumpsOn), wantTaken: false},
	}
}

func globalFastPathCases() []fastPathCase {
	store := func(vm *VM, _ *CallFrame) {
		vm.Globals.ints = make([]int64, 302)
		vm.Globals.ints[1], vm.Globals.ints[2], vm.Globals.ints[300] = 41, 43, 47
		vm.Globals.uints = []uint64{5, 6}
		vm.Globals.general = []reflect.Value{reflect.ValueOf("zero"), reflect.ValueOf("one")}
	}
	shared := func(vm *VM, frame *CallFrame) {
		store(vm, frame)
		vm.Globals.shared.Store(true)
	}
	bundled := func(vm *VM, frame *CallFrame) {
		store(vm, frame)
		frame.Function.GlobalBases = &program.SlotAllocation{isa.RegisterInt64: 1}
	}
	value := func(registers *Registers) { registers.Ints[2] = 99 }
	integer, general, unsigned := uint8(isa.RegisterInt64), uint8(isa.RegisterGeneral), uint8(isa.RegisterUint64)
	wide := func(index int) isa.Instruction {
		return isa.NewInstruction(isa.OpExtension, uint8(index&0xFF), uint8(index>>8), 0)
	}
	load := func(index, kind uint8) isa.Instruction { return isa.NewInstruction(isa.OpGetGlobal, 2, index, kind) }
	loadWide := func(kind uint8) isa.Instruction { return isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 2, kind) }
	save := func(index, kind uint8) isa.Instruction { return isa.NewInstruction(isa.OpSetGlobal, 2, index, kind) }
	saveWide := func(kind uint8) isa.Instruction { return isa.NewTier1Instruction(isa.SubOpSetGlobalWide, 2, kind) }

	return []fastPathCase{
		{name: "an integer global read", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(1, integer), arrange: store, wantTaken: true},
		{name: "a general global read", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(1, general), arrange: store, wantTaken: true},
		{name: "an unsigned global read", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(1, unsigned), arrange: store, wantTaken: false},
		{name: "a global read past its bank", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(2, general), arrange: store, wantTaken: false},
		{name: "a global read from a shared store", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(1, integer), arrange: shared, wantTaken: false},
		{name: "a global read by a bundled function", fast: fastGetGlobal, handler: handleGetGlobal, instruction: load(1, integer), arrange: bundled, wantTaken: false},
		{name: "an integer global read at a wide index", fast: fastGetGlobalWide, handler: handleGetGlobalWide,
			instruction: loadWide(integer), extension: wide(300), arrange: store, wantTaken: true},
		{name: "a general global read at a wide index", fast: fastGetGlobalWide, handler: handleGetGlobalWide,
			instruction: loadWide(general), extension: wide(1), arrange: store, wantTaken: false},
		{name: "a wide global read past its bank", fast: fastGetGlobalWide, handler: handleGetGlobalWide,
			instruction: loadWide(integer), extension: wide(302), arrange: store, wantTaken: false},
		{name: "a wide global read from a shared store", fast: fastGetGlobalWide, handler: handleGetGlobalWide,
			instruction: loadWide(integer), extension: wide(300), arrange: shared, wantTaken: false},
		{name: "a wide global read by a bundled function", fast: fastGetGlobalWide, handler: handleGetGlobalWide,
			instruction: loadWide(integer), extension: wide(300), arrange: bundled, wantTaken: false},
		{name: "an integer global written", fast: fastSetGlobal, handler: handleSetGlobal, instruction: save(1, integer), prepare: value, arrange: store, wantTaken: true},
		{name: "an unsigned global written", fast: fastSetGlobal, handler: handleSetGlobal, instruction: save(1, unsigned), prepare: value, arrange: store, wantTaken: false},
		{name: "a global written past its bank", fast: fastSetGlobal, handler: handleSetGlobal,
			instruction: isa.NewInstruction(isa.OpSetGlobal, 2, 255, integer), prepare: value, arrange: func(vm *VM, _ *CallFrame) {
				vm.Globals.ints = make([]int64, 4)
			}, wantTaken: false},
		{name: "a global written in a shared store", fast: fastSetGlobal, handler: handleSetGlobal, instruction: save(1, integer), prepare: value, arrange: shared, wantTaken: false},
		{name: "a global written by a bundled function", fast: fastSetGlobal, handler: handleSetGlobal, instruction: save(1, integer), prepare: value, arrange: bundled, wantTaken: false},
		{name: "an integer global written at a wide index", fast: fastSetGlobalWide, handler: handleSetGlobalWide,
			instruction: saveWide(integer), extension: wide(300), prepare: value, arrange: store, wantTaken: true},
		{name: "an unsigned global written at a wide index", fast: fastSetGlobalWide, handler: handleSetGlobalWide,
			instruction: saveWide(unsigned), extension: wide(1), prepare: value, arrange: store, wantTaken: false},
		{name: "a wide global written past its bank", fast: fastSetGlobalWide, handler: handleSetGlobalWide,
			instruction: saveWide(integer), extension: wide(302), prepare: value, arrange: store, wantTaken: false},
		{name: "a wide global written in a shared store", fast: fastSetGlobalWide, handler: handleSetGlobalWide,
			instruction: saveWide(integer), extension: wide(300), prepare: value, arrange: shared, wantTaken: false},
	}
}

func fastPathCases() []fastPathCase {
	byteSlice := func(register uint8, index int64) func(*Registers) {
		return func(registers *Registers) {
			registers.slicesByte[register] = []byte{10, 20, 30, 250}
			registers.Ints[3] = index
			registers.Uints[4] = 0x1FE
		}
	}
	byteRead := isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, 2, 1)
	byteReadUnsigned := isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, 2, 1)
	byteStore := isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 1, 3)
	indexExtension := isa.NewInstruction(isa.OpExtension, 3, 0, 0)
	multiplyThenAdd := isa.NewInstruction(isa.OpMultiplyInt64ConstantThenAdd, 0, 1, 1)
	maskWithConstant := isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, 2, 1)
	widePool := make([]uint64, 301)
	widePool[300] = 0xF0F0

	cases := []fastPathCase{
		{name: "a byte read as an integer", fast: fastGetSliceByteElementInt64, handler: handleGetSliceByteElementInt64,
			instruction: byteRead, extension: indexExtension, prepare: byteSlice(1, 3), wantTaken: true},
		{name: "a byte read as an integer one past the end", fast: fastGetSliceByteElementInt64, handler: handleGetSliceByteElementInt64,
			instruction: byteRead, extension: indexExtension, prepare: byteSlice(1, 4), wantTaken: false},
		{name: "a byte read as an integer at a negative index", fast: fastGetSliceByteElementInt64, handler: handleGetSliceByteElementInt64,
			instruction: byteRead, extension: indexExtension, prepare: byteSlice(1, -1), wantTaken: false},
		{name: "a byte read", fast: fastGetSliceByteElement, handler: handleGetSliceByteElement,
			instruction: byteReadUnsigned, extension: indexExtension, prepare: byteSlice(1, 0), wantTaken: true},
		{name: "a byte read from an empty slice", fast: fastGetSliceByteElement, handler: handleGetSliceByteElement,
			instruction: byteReadUnsigned, extension: indexExtension, prepare: func(registers *Registers) { registers.slicesByte[1] = nil }, wantTaken: false},
		{name: "a byte read at the largest index", fast: fastGetSliceByteElement, handler: handleGetSliceByteElement,
			instruction: byteReadUnsigned, extension: indexExtension, prepare: byteSlice(1, math.MaxInt64), wantTaken: false},
		{name: "a byte store that truncates its value", fast: fastSetSliceByteElement, handler: handleSetSliceByteElement,
			instruction: byteStore, extension: isa.NewInstruction(isa.OpExtension, 4, 0, 0), prepare: byteSlice(1, 2), wantTaken: true},
		{name: "a byte store at a displacement", fast: fastSetSliceByteElement, handler: handleSetSliceByteElement,
			instruction: byteStore, extension: isa.NewInstruction(isa.OpExtension, 4, 2, 0), prepare: byteSlice(1, 1), wantTaken: true},
		{name: "a byte store whose displacement leaves the slice", fast: fastSetSliceByteElement, handler: handleSetSliceByteElement,
			instruction: byteStore, extension: isa.NewInstruction(isa.OpExtension, 4, 2, 0), prepare: byteSlice(1, 2), wantTaken: false},
		{name: "a byte store at a negative index", fast: fastSetSliceByteElement, handler: handleSetSliceByteElement,
			instruction: byteStore, extension: isa.NewInstruction(isa.OpExtension, 4, 1, 0), prepare: byteSlice(1, -2), wantTaken: false},
		{name: "a multiply by a constant then add", fast: fastMultiplyInt64ConstantThenAdd, handler: handleMultiplyInt64ConstantThenAdd,
			instruction: multiplyThenAdd, extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0), intConstants: []int64{0, -7},
			prepare: func(registers *Registers) { registers.Ints[1], registers.Ints[2] = 6, 100 }, wantTaken: true},
		{name: "a multiply by a constant outside the pool", fast: fastMultiplyInt64ConstantThenAdd, handler: handleMultiplyInt64ConstantThenAdd,
			instruction: multiplyThenAdd, extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0), intConstants: []int64{3}, wantTaken: false},
		{name: "a mask with a constant", fast: fastBitAndUint64Constant, handler: handleBitAndUint64Constant,
			instruction: maskWithConstant, extension: isa.NewInstruction(isa.OpExtension, 1, 0, 0), uintConstants: []uint64{0, 0xFF00},
			prepare: func(registers *Registers) { registers.Uints[1] = 0xABCD }, wantTaken: true},
		{name: "a mask with a constant at a wide index", fast: fastBitAndUint64Constant, handler: handleBitAndUint64Constant,
			instruction: maskWithConstant, extension: isa.NewInstruction(isa.OpExtension, 300&0xFF, 300>>8, 0), uintConstants: widePool,
			prepare: func(registers *Registers) { registers.Uints[1] = 0xFFFF }, wantTaken: true},
		{name: "a mask with a constant outside the pool", fast: fastBitAndUint64Constant, handler: handleBitAndUint64Constant,
			instruction: maskWithConstant, extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0), uintConstants: []uint64{1, 2}, wantTaken: false},
		{name: "a counter stepped below its bound", fast: fastIncrementInt64ThenJumpIfLessThanConstant, handler: handleIncrementInt64ThenJumpIfLessThanConstant,
			instruction: isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, 1, 0), extension: jumpExtension(-3),
			intConstants: []int64{10}, prepare: func(registers *Registers) { registers.Ints[1] = 8 }, wantTaken: true},
		{name: "a counter stepped onto its bound", fast: fastIncrementInt64ThenJumpIfLessThanConstant, handler: handleIncrementInt64ThenJumpIfLessThanConstant,
			instruction: isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, 1, 0), extension: jumpExtension(-3),
			intConstants: []int64{10}, prepare: func(registers *Registers) { registers.Ints[1] = 9 }, wantTaken: true},
		{name: "a counter whose bound is outside the pool", fast: fastIncrementInt64ThenJumpIfLessThanConstant, handler: handleIncrementInt64ThenJumpIfLessThanConstant,
			instruction: isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, 1, 2), extension: jumpExtension(-3),
			intConstants: []int64{10}, prepare: func(registers *Registers) { registers.Ints[1] = 8 }, wantTaken: false},
	}
	for _, kind := range []isa.RegisterKind{isa.RegisterInt64, isa.RegisterUint64, isa.RegisterFloat64, isa.RegisterBool} {
		cases = append(cases, fastPathCase{name: "a zero loaded into the " + kind.String() + " bank", fast: fastLoadZero, handler: handleLoadZero,
			instruction: isa.NewTier1Instruction(isa.SubOpLoadZero, 2, uint8(kind)), prepare: func(registers *Registers) {
				registers.Ints[2], registers.Uints[2], registers.Floats[2], registers.Bools[2] = 7, 7, 7, true
			}, wantTaken: true})
	}
	for _, kind := range []isa.RegisterKind{isa.RegisterString, isa.RegisterGeneral, isa.RegisterComplex128} {
		cases = append(cases, fastPathCase{name: "a zero loaded into the " + kind.String() + " bank", fast: fastLoadZero, handler: handleLoadZero,
			instruction: isa.NewTier1Instruction(isa.SubOpLoadZero, 2, uint8(kind)), wantTaken: false})
	}
	cases = append(cases, compareJumpCases("a less-than test", isa.SubOpLessThanInt64ConstantThenJumpIfFalse,
		fastLessThanInt64ConstantThenJumpIfFalse, handleLessThanInt64ConstantThenJumpIfFalse, 10, 9)...)
	cases = append(cases, compareJumpCases("a greater-or-equal test", isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse,
		fastGreaterOrEqualInt64ConstantThenJumpIfFalse, handleGreaterOrEqualInt64ConstantThenJumpIfFalse, 9, 10)...)
	cases = append(cases, compareJumpCases("an equality test", isa.SubOpEqualInt64ConstantThenJumpIfFalse,
		fastEqualInt64ConstantThenJumpIfFalse, handleEqualInt64ConstantThenJumpIfFalse, 9, 10)...)
	cases = append(cases, compareJumpCases("an equality test jumping when true", isa.SubOpEqualInt64ConstantThenJumpIfTrue,
		fastEqualInt64ConstantThenJumpIfTrue, handleEqualInt64ConstantThenJumpIfTrue, 10, 9)...)
	cases = append(cases, globalFastPathCases()...)
	cases = append(cases, registerFastPathCases()...)
	return append(cases, byteElementFastPathCases()...)
}

func registerFastPathCases() []fastPathCase {
	counter := func(value int64) func(*Registers) {
		return func(registers *Registers) { registers.Ints[1], registers.Ints[2] = value, 10 }
	}
	step := isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThan, 1, 2)
	add := isa.NewTier1Instruction(isa.SubOpAddUint64Constant, 2, 1)
	subtract := isa.NewTier1Instruction(isa.SubOpSubtractUint64Constant, 2, 1)
	widePool := make([]uint64, 301)
	widePool[300] = 9
	operand := func(registers *Registers) { registers.Uints[1] = math.MaxUint64 - 2 }
	text := func(index int64) func(*Registers) {
		return func(registers *Registers) { registers.Strings[1], registers.Ints[3] = "h\u00e9llo", index }
	}
	integers := func(index int64) func(*Registers) {
		return func(registers *Registers) {
			registers.SlicesInt[1] = []int64{-4, 5, math.MinInt64}
			registers.Ints[3], registers.Ints[2] = index, 77
		}
	}
	byteOfText := isa.NewInstruction(isa.OpGetStringByte, 2, 1, 3)
	integerRead := isa.NewInstruction(isa.OpGetSliceInt64Element, 4, 1, 3)
	integerStore := isa.NewInstruction(isa.OpSetSliceInt64Element, 1, 3, 2)

	return []fastPathCase{
		{name: "a general register copied as it stands", fast: fastMoveGeneral, handler: handleMoveGeneral,
			instruction: isa.NewInstruction(isa.OpMoveGeneral, 2, 1, MoveGeneralModeAlias),
			prepare:     func(registers *Registers) { registers.General[1] = reflect.ValueOf("moved") }, wantTaken: true},
		{name: "a general move that may copy its value", fast: fastMoveGeneral, handler: handleMoveGeneral,
			instruction: isa.NewInstruction(isa.OpMoveGeneral, 2, 1, MoveGeneralModeDynamic),
			prepare:     func(registers *Registers) { registers.General[1] = reflect.ValueOf([2]int{1, 2}) }, wantTaken: false},
		{name: "a general move that snapshots its value", fast: fastMoveGeneral, handler: handleMoveGeneral,
			instruction: isa.NewInstruction(isa.OpMoveGeneral, 2, 1, MoveGeneralModeSnapshot),
			prepare:     func(registers *Registers) { registers.General[1] = reflect.ValueOf([2]int{1, 2}) }, wantTaken: false},
		{name: "a counter stepped below a register bound", fast: fastIncrementInt64ThenJumpIfLessThan, handler: handleIncrementInt64ThenJumpIfLessThan,
			instruction: step, extension: jumpExtension(-3), prepare: counter(8), wantTaken: true},
		{name: "a counter stepped onto a register bound", fast: fastIncrementInt64ThenJumpIfLessThan, handler: handleIncrementInt64ThenJumpIfLessThan,
			instruction: step, extension: jumpExtension(-3), prepare: counter(9), wantTaken: true},
		{name: "a counter stepped against itself", fast: fastIncrementInt64ThenJumpIfLessThan, handler: handleIncrementInt64ThenJumpIfLessThan,
			instruction: isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThan, 1, 1), extension: jumpExtension(300), prepare: counter(8), wantTaken: true},
		{name: "an unsigned constant added past the top", fast: fastAddUint64Constant, handler: handleAddUint64Constant,
			instruction: add, extension: isa.NewInstruction(isa.OpExtension, 1, 0, 0), uintConstants: []uint64{0, 5}, prepare: operand, wantTaken: true},
		{name: "an unsigned constant added from a wide index", fast: fastAddUint64Constant, handler: handleAddUint64Constant,
			instruction: add, extension: isa.NewInstruction(isa.OpExtension, 300&0xFF, 300>>8, 0), uintConstants: widePool, prepare: operand, wantTaken: true},
		{name: "an unsigned constant added from outside the pool", fast: fastAddUint64Constant, handler: handleAddUint64Constant,
			instruction: add, extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0), uintConstants: []uint64{0, 5}, prepare: operand, wantTaken: false},
		{name: "an unsigned constant subtracted past zero", fast: fastSubtractUint64Constant, handler: handleSubtractUint64Constant,
			instruction: subtract, extension: isa.NewInstruction(isa.OpExtension, 1, 0, 0), uintConstants: []uint64{0, 5},
			prepare: func(registers *Registers) { registers.Uints[1] = 3 }, wantTaken: true},
		{name: "an unsigned constant subtracted from a wide index", fast: fastSubtractUint64Constant, handler: handleSubtractUint64Constant,
			instruction: subtract, extension: isa.NewInstruction(isa.OpExtension, 300&0xFF, 300>>8, 0), uintConstants: widePool, prepare: operand, wantTaken: true},
		{name: "an unsigned constant subtracted from outside the pool", fast: fastSubtractUint64Constant, handler: handleSubtractUint64Constant,
			instruction: subtract, extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0), uintConstants: []uint64{0, 5}, prepare: operand, wantTaken: false},
		{name: "an unsigned constant added and kept to 32 bits", fast: fastAddUint32Constant, handler: handleAddUint32Constant,
			instruction: isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 2, 1), extension: isa.NewInstruction(isa.OpExtension, 1, 0, 0),
			uintConstants: []uint64{0, 5}, prepare: operand, wantTaken: true},
		{name: "a 32-bit sum with a constant at a wide index", fast: fastAddUint32Constant, handler: handleAddUint32Constant,
			instruction: isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 2, 1), extension: isa.NewInstruction(isa.OpExtension, 300&0xFF, 300>>8, 0),
			uintConstants: widePool, prepare: operand, wantTaken: true},
		{name: "a 32-bit sum with a constant outside the pool", fast: fastAddUint32Constant, handler: handleAddUint32Constant,
			instruction: isa.NewTier1Instruction(isa.SubOpAddUint32Constant, 2, 1), extension: isa.NewInstruction(isa.OpExtension, 2, 0, 0),
			uintConstants: []uint64{0, 5}, prepare: operand, wantTaken: false},
		{name: "a byte of a string", fast: fastGetStringByte, handler: handleGetStringByte, instruction: byteOfText, prepare: text(2), wantTaken: true},
		{name: "a byte one past the end of a string", fast: fastGetStringByte, handler: handleGetStringByte, instruction: byteOfText, prepare: text(6), wantTaken: false},
		{name: "a byte of a string at a negative index", fast: fastGetStringByte, handler: handleGetStringByte, instruction: byteOfText, prepare: text(-1), wantTaken: false},
		{name: "an integer read from an integer slice", fast: fastGetSliceInt64Element, handler: handleGetSliceInt64Element,
			instruction: integerRead, prepare: integers(2), wantTaken: true},
		{name: "an integer read one past the end", fast: fastGetSliceInt64Element, handler: handleGetSliceInt64Element,
			instruction: integerRead, prepare: integers(3), wantTaken: false},
		{name: "an integer read at a negative index", fast: fastGetSliceInt64Element, handler: handleGetSliceInt64Element,
			instruction: integerRead, prepare: integers(-1), wantTaken: false},
		{name: "an integer stored into an integer slice", fast: fastSetSliceInt64Element, handler: handleSetSliceInt64Element,
			instruction: integerStore, prepare: integers(0), wantTaken: true},
		{name: "an integer stored one past the end", fast: fastSetSliceInt64Element, handler: handleSetSliceInt64Element,
			instruction: integerStore, prepare: integers(3), wantTaken: false},
		{name: "an integer stored at the largest index", fast: fastSetSliceInt64Element, handler: handleSetSliceInt64Element,
			instruction: integerStore, prepare: integers(math.MaxInt64), wantTaken: false},
	}
}

func byteElementFastPathCases() []fastPathCase {
	probe := reflect.ValueOf([]byte{1})
	plain := byteSliceElements(&probe) != nil
	holding := func(collection func() reflect.Value, index int64) func(*Registers) {
		return func(registers *Registers) {
			registers.General[1] = collection()
			registers.Ints[3], registers.Uints[4] = index, 0x1FE
		}
	}
	bytes := func() reflect.Value { return reflect.ValueOf([]byte{10, 20, 30, 250}) }
	collections := []struct {
		build func() reflect.Value
		name  string
		index int64
		taken bool
	}{
		{name: "a byte slice", build: bytes, index: 3, taken: plain},
		{name: "a byte slice one past its end", build: bytes, index: 4},
		{name: "a byte slice at a negative index", build: bytes, index: -1},
		{name: "a nil byte slice", build: func() reflect.Value { return reflect.ValueOf([]byte(nil)) }},
		{name: "an empty register"},
		{name: "a named byte slice", build: func() reflect.Value { return reflect.ValueOf(fastPathNamedBytes{10, 20, 30}) }, index: 1},
		{name: "a byte array", build: func() reflect.Value { return reflect.ValueOf(&[4]byte{10, 20, 30, 40}).Elem() }, index: 1},
		{name: "a slice of wider elements", build: func() reflect.Value { return reflect.ValueOf([]uint16{10, 20, 30}) }, index: 1},
	}
	operations := []struct {
		fast        fastPath
		handler     opcodeHandler
		name        string
		instruction isa.Instruction
	}{
		{name: "an unsigned element read from ", fast: fastGetElementUint64, handler: handleGetElementUint64, instruction: isa.NewInstruction(isa.OpGetElementUint64, 2, 1, 3)},
		{name: "an integer element read from ", fast: fastGetElementInt64, handler: handleGetElementInt64, instruction: isa.NewInstruction(isa.OpGetElementInt64, 2, 1, 3)},
		{name: "an unsigned element stored into ", fast: fastSetElementUint64, handler: handleSetElementUint64, instruction: isa.NewInstruction(isa.OpSetElementUint64, 1, 3, 4)},
	}

	var cases []fastPathCase
	for _, operation := range operations {
		for _, collection := range collections {
			build := collection.build
			if build == nil {
				build = func() reflect.Value { return reflect.Value{} }
			}
			cases = append(cases, fastPathCase{name: operation.name + collection.name, fast: operation.fast, handler: operation.handler,
				instruction: operation.instruction, prepare: holding(build, collection.index), wantTaken: collection.taken})
		}
	}
	return append(cases, fastPathCase{name: "an unsigned element stored into a byte slice that may only be read",
		fast: fastSetElementUint64, handler: handleSetElementUint64, instruction: isa.NewInstruction(isa.OpSetElementUint64, 1, 3, 4),
		prepare: holding(func() reflect.Value {
			return reflect.ValueOf(struct{ hidden []byte }{hidden: []byte{10, 20, 30, 250}}).Field(0)
		}, 1), wantTaken: false})
}

func TestFastPathsAgreeWithTheirHandlers(t *testing.T) {
	t.Parallel()

	for _, tt := range fastPathCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handlerVM, handlerFrame, handlerRegisters := newFastPathFrame(t, &tt)
			fastVM, fastFrame, fastRegisters := newFastPathFrame(t, &tt)
			before := captureFastPathState(fastVM, fastFrame, fastRegisters)

			taken := tt.fast(fastVM, fastFrame, fastRegisters, tt.instruction)

			require.Equal(t, tt.wantTaken, taken)
			if !taken {
				require.Equal(t, before, captureFastPathState(fastVM, fastFrame, fastRegisters),
					"a fast path that declines must leave the state for the handler exactly as it found it")
				return
			}
			result := tt.handler(handlerVM, handlerFrame, handlerRegisters, tt.instruction)
			require.Equal(t, opContinue, result, "a fast path may only cover what the handler completes")
			require.Equal(t, captureFastPathState(handlerVM, handlerFrame, handlerRegisters), captureFastPathState(fastVM, fastFrame, fastRegisters),
				"a fast path that accepts must leave the state the handler leaves")
			require.NotEqual(t, before, captureFastPathState(fastVM, fastFrame, fastRegisters), "the case must be one the operation changes")
		})
	}
}

func TestEveryFastPathHasAContractCase(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "vm_dispatch_fast_paths.go", nil, parser.SkipObjectResolution)
	require.NoError(t, err)
	covered := map[string]bool{}
	for _, tt := range fastPathCases() {
		name := runtime.FuncForPC(reflect.ValueOf(tt.fast).Pointer()).Name()
		_, short, _ := strings.CutLast(name, ".")
		covered[short] = true
	}

	declared := 0
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || !strings.HasPrefix(function.Name.Name, "fast") {
			continue
		}
		declared++
		require.Truef(t, covered[function.Name.Name],
			"%s has no case in fastPathCases, so nothing holds it to the fast-path contract", function.Name.Name)
	}
	require.NotZero(t, declared)
}
