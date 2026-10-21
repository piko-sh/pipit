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

package passes

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func arithmeticLoop(inner ...isa.Instruction) *program.CompiledFunction {
	body := make([]isa.Instruction, 0, len(inner)+5)
	body = append(body, mk(isa.OpLoadInt64Constant, 0, 0, 0), mk(isa.OpNoOperation, 0, 0, 0))
	body = append(body, inner...)
	body = append(body, mk(isa.OpAddInt64, 0, 0, 1), tier1Jump(0), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
	program.SetJumpTarget(body, len(body)-2, 1)
	return &program.CompiledFunction{Body: body, NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 16, isa.RegisterUint64: 8}}
}

func TestArithmeticHoistMovesInvariantChains(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpSubtractInt64, 10, 9, 2),
		mk(isa.OpMultiplyInt64, 11, 8, 10),
		mk(isa.OpTruncateToWidth, 11, 32, uint8(isa.RegisterInt64)),
		mk(isa.OpAddInt64, 12, 11, 0),
	)
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, []isa.Opcode{isa.OpSubtractInt64, isa.OpMultiplyInt64, isa.OpTruncateToWidth}, opcodes(compiledFunction.Body[1:4]), "the chain leaves the loop in order")
	require.Equal(t, isa.OpNoOperation, compiledFunction.Body[4].Op, "the header follows the hoisted words")
	require.Equal(t, mk(isa.OpAddInt64, 12, 11, 0), compiledFunction.Body[5], "the variant use stays")
}

func TestArithmeticHoistKeepsVariantOperations(t *testing.T) {
	t.Parallel()
	for name, inner := range map[string][]isa.Instruction{
		"reads the induction variable": {mk(isa.OpMultiplyInt64, 10, 0, 8)},
		"accumulates":                  {mk(isa.OpAddInt64, 10, 10, 8)},
		"can fault":                    {mk(isa.OpDivideInt64, 10, 9, 8)},
		"destination read before":      {mk(isa.OpAddInt64, 12, 10, 0), mk(isa.OpMultiplyInt64, 10, 9, 8)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := arithmeticLoop(inner...)
			original := slices.Clone(compiledFunction.Body)
			require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
			require.Equal(t, original, compiledFunction.Body)
		})
	}
}

func TestArithmeticHoistRenamesReusedTemporaries(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpMultiplyInt64, 10, 9, 8),
		mk(isa.OpAddInt64, 12, 10, 0),
		mk(isa.OpAddInt64, 10, 0, 0),
		mk(isa.OpAddInt64, 13, 10, 0),
	)
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, mk(isa.OpMultiplyInt64, 16, 9, 8), compiledFunction.Body[1])
	require.Equal(t, mk(isa.OpAddInt64, 12, 16, 0), compiledFunction.Body[3])
	require.Equal(t, mk(isa.OpAddInt64, 13, 10, 0), compiledFunction.Body[5])
}

func TestArithmeticHoistRenamesATemporaryItsUseRedefines(t *testing.T) {
	t.Parallel()
	compiledFunction := arithmeticLoop(
		mk(isa.OpSubtractInt64, 10, 9, 8),
		mk(isa.OpSubtractInt64Constant, 10, 10, 5),
		mk(isa.OpMultiplyInt64, 11, 7, 10),
		mk(isa.OpMultiplyInt64Constant, 10, 0, 6),
		mk(isa.OpAddInt64, 11, 11, 10),
	)
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, []isa.Instruction{
		mk(isa.OpSubtractInt64, 16, 9, 8),
		mk(isa.OpSubtractInt64Constant, 17, 16, 5),
		mk(isa.OpMultiplyInt64, 18, 7, 17),
	}, compiledFunction.Body[1:4], "each step of the chain takes a register of its own and leaves the loop")
	require.Equal(t, isa.OpNoOperation, compiledFunction.Body[4].Op, "the header follows the hoisted words")
	require.Equal(t, mk(isa.OpMultiplyInt64Constant, 10, 0, 6), compiledFunction.Body[5], "the variant product stays and keeps its register")
	require.Equal(t, mk(isa.OpAddInt64, 11, 18, 10), compiledFunction.Body[6], "the variant sum reads the hoisted product and still writes its own register")
}

func TestRenamableUseTellsAReadFromARedefinition(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		instruction isa.Instruction
		renamable   bool
		redefines   bool
	}{
		"a plain read":                      {instruction: mk(isa.OpAddInt64, 12, 10, 3), renamable: true, redefines: false},
		"a read the destination overwrites": {instruction: mk(isa.OpSubtractInt64Constant, 10, 10, 5), renamable: true, redefines: true},
		"both sources and the destination":  {instruction: mk(isa.OpAddInt64, 10, 10, 10), renamable: true, redefines: true},
		"a write alone":                     {instruction: mk(isa.OpAddInt64, 10, 3, 4), renamable: false, redefines: true},
		"an operand read and written":       {instruction: mk(isa.OpTruncateToWidth, 10, 32, uint8(isa.RegisterInt64)), renamable: false, redefines: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			renamable, redefines := renamableUse(test.instruction, isa.RegisterInt64, 10)
			require.Equal(t, test.renamable, renamable)
			require.Equal(t, test.redefines, redefines)
		})
	}
}

func TestRenameRegisterReadsLeavesTheWrittenOperand(t *testing.T) {
	t.Parallel()
	require.Equal(t, mk(isa.OpSubtractInt64Constant, 10, 16, 5),
		renameRegisterReads(mk(isa.OpSubtractInt64Constant, 10, 10, 5), isa.RegisterInt64, 10, 16))
	require.Equal(t, mk(isa.OpAddInt64, 10, 16, 16),
		renameRegisterReads(mk(isa.OpAddInt64, 10, 10, 10), isa.RegisterInt64, 10, 16))
	require.Equal(t, mk(isa.OpAddInt64, 12, 16, 3),
		renameRegisterReads(mk(isa.OpAddInt64, 12, 10, 3), isa.RegisterInt64, 10, 16))
}

func TestArithmeticHoistLiftsTheLengthOfAnUnchangingStringOrTypedSlice(t *testing.T) {
	t.Parallel()
	const length, collection, other = 10, 3, 6
	tests := []struct {
		name   string
		length isa.SubOpcode
		move   isa.SubOpcode
	}{
		{name: "a string", length: isa.SubOpLengthString, move: isa.SubOpMoveString},
		{name: "a slice of int", length: isa.SubOpLengthSliceInt64, move: isa.SubOpMoveSliceInt64},
		{name: "a slice of float", length: isa.SubOpLengthSliceFloat64, move: isa.SubOpMoveSliceFloat64},
		{name: "a slice of string", length: isa.SubOpLengthSliceString, move: isa.SubOpMoveSliceString},
		{name: "a slice of bool", length: isa.SubOpLengthSliceBool, move: isa.SubOpMoveSliceBool},
		{name: "a slice of uint", length: isa.SubOpLengthSliceUint64, move: isa.SubOpMoveSliceUint64},
		{name: "a slice of byte", length: isa.SubOpLengthSliceByte, move: isa.SubOpMoveSliceByte},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read := isa.NewTier1Instruction(tt.length, length, collection)
			use := mk(isa.OpAddInt64, 12, length, 0)

			compiledFunction := arithmeticLoop(read, use)
			require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
			require.Equal(t, read, compiledFunction.Body[1], "the length is read once, ahead of the loop")
			require.Equal(t, isa.OpNoOperation, compiledFunction.Body[2].Op, "the header follows the hoisted read")
			require.Equal(t, use, compiledFunction.Body[3], "the use stays in the loop")

			compiledFunction = arithmeticLoop(read, use, isa.NewTier1Instruction(tt.move, collection, other))
			original := slices.Clone(compiledFunction.Body)
			require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
			require.Equal(t, original, compiledFunction.Body, "a register the loop assigns has a length to read each time round")
		})
	}
}

func TestArithmeticHoistLiftsTheLengthOfAGeneralSliceTheLoopIndexesAndCannotChange(t *testing.T) {
	t.Parallel()
	const length, collection, index = 10, 3, 0
	read := isa.NewTier1Instruction(isa.SubOpLengthGeneral, length, collection)
	layout := mk(isa.OpExtension, 0, 0, 0)
	tests := []struct {
		name    string
		element []isa.Instruction
	}{
		{name: "a general element", element: []isa.Instruction{mk(isa.OpGetElement, 5, collection, index)}},
		{name: "an integer element", element: []isa.Instruction{mk(isa.OpGetElementInt64, 12, collection, index)}},
		{name: "an unsigned element", element: []isa.Instruction{mk(isa.OpGetElementUint64, 2, collection, index)}},
		{name: "a float element", element: []isa.Instruction{mk(isa.OpGetElementFloat64, 2, collection, index)}},
		{name: "a bool element", element: []isa.Instruction{mk(isa.OpGetElementBool, 2, collection, index)}},
		{name: "a string element", element: []isa.Instruction{mk(isa.OpGetElementString, 2, collection, index)}},
		{name: "an integer field of an element", element: []isa.Instruction{mk(isa.OpGetElementFieldInt64, 12, collection, index), layout}},
		{name: "an unsigned field of an element", element: []isa.Instruction{mk(isa.OpGetElementFieldUint64, 2, collection, index), layout}},
		{name: "a float field of an element", element: []isa.Instruction{mk(isa.OpGetElementFieldFloat64, 2, collection, index), layout}},
		{name: "a bool field of an element", element: []isa.Instruction{mk(isa.OpGetElementFieldBool, 2, collection, index), layout}},
		{name: "a string field of an element", element: []isa.Instruction{mk(isa.OpGetElementFieldString, 2, collection, index), layout}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := arithmeticLoop(append(slices.Clone(tt.element), read)...)
			compiledFunction.NumRegisters[isa.RegisterGeneral] = 8

			require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))

			require.Equal(t, read, compiledFunction.Body[1], "the length is read once, ahead of the loop")
			require.Equal(t, isa.OpNoOperation, compiledFunction.Body[2].Op, "the header follows the hoisted read")
			require.Equal(t, tt.element, compiledFunction.Body[3:3+len(tt.element)], "the element read stays in the loop")
		})
	}
}

func TestArithmeticHoistKeepsTheLengthOfAGeneralRegisterTheLoopCouldChange(t *testing.T) {
	t.Parallel()
	const length, collection, other, index = 10, 3, 6, 0
	read := isa.NewTier1Instruction(isa.SubOpLengthGeneral, length, collection)
	element := mk(isa.OpGetElementUint64, 2, collection, index)
	tests := []struct {
		name  string
		inner []isa.Instruction
	}{
		{name: "nothing in the loop indexes the register, which could then hold a channel", inner: []isa.Instruction{read}},
		{name: "only another register is indexed", inner: []isa.Instruction{mk(isa.OpGetElementUint64, 2, other, index), read}},
		{name: "the register is assigned", inner: []isa.Instruction{element, read, mk(isa.OpMoveGeneral, collection, other, 0)}},
		{name: "an element is stored", inner: []isa.Instruction{element, read, mk(isa.OpSetElementUint64, collection, index, 2)}},
		{name: "a slice variable is stored", inner: []isa.Instruction{element, read, mk(isa.OpSetGlobal, other, 0, uint8(isa.RegisterGeneral))}},
		{name: "the register is appended to in place", inner: []isa.Instruction{element, read, mk(isa.OpAppendInPlace, collection, collection, 5)}},
		{name: "a function of unknown effect is called", inner: []isa.Instruction{element, read, isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}},
		{name: "a method is called", inner: []isa.Instruction{element, read, isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0), mk(isa.OpExtension, 0, 0, 0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := arithmeticLoop(tt.inner...)
			compiledFunction.NumRegisters[isa.RegisterGeneral] = 8
			compiledFunction.CallSites = []program.CallSite{{}}
			original := slices.Clone(compiledFunction.Body)

			require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))

			require.Equal(t, original, compiledFunction.Body)
		})
	}
}

func TestLoopKeepsSliceLengthOnlyWhileItMeetsNoOtherGoroutine(t *testing.T) {
	t.Parallel()
	const length, collection, index = 10, 3, 0
	read := isa.NewTier1Instruction(isa.SubOpLengthGeneral, length, collection)
	element := mk(isa.OpGetElementUint64, 2, collection, index)
	tests := []struct {
		name  string
		other isa.Instruction
		want  bool
	}{
		{name: "arithmetic", other: mk(isa.OpAddInt64, 12, 12, 12), want: true},
		{name: "a select", other: isa.NewTier1Instruction(isa.SubOpSelect, 0, 0)},
		{name: "a step of a range, which may be over a channel", other: isa.NewTier1Instruction(isa.SubOpRangeNext, 0, 0)},
		{name: "a go statement", other: mk(isa.OpGo, 0, 0, 0)},
		{name: "a receive", other: isa.NewTier1Instruction(isa.SubOpReceiveFromChannel, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{element, read, tt.other}}

			require.Equal(t, tt.want, loopKeepsSliceLength(compiledFunction, []int{0, 1, 2}, 1))
		})
	}
}

func TestArithmeticHoistLiftsAGeneralLengthPastACallThatStoresNothing(t *testing.T) {
	t.Parallel()
	const length, collection, index = 10, 3, 0
	read := isa.NewTier1Instruction(isa.SubOpLengthGeneral, length, collection)
	callee := &program.CompiledFunction{Name: "pure", HeapMutationClass: program.HeapPureCallee}
	compiledFunction := arithmeticLoop(mk(isa.OpGetElementUint64, 2, collection, index), isa.NewTier1Instruction(isa.SubOpCall, 0, 0), read)
	compiledFunction.NumRegisters[isa.RegisterGeneral] = 8
	compiledFunction.Functions = []*program.CompiledFunction{callee}
	compiledFunction.CallSites = []program.CallSite{{CachedCallee: callee}}

	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))

	require.Equal(t, read, compiledFunction.Body[1], "a callee that stores nothing leaves the length as it was")
}

func TestArithmeticHoistLiftsAByteViewOfAnUnchangingCollection(t *testing.T) {
	t.Parallel()
	const view, collection = 2, 5
	unbox := isa.NewTier1Instruction(isa.SubOpUnboxSliceByte, view, collection)
	read := isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, 3, view)

	compiledFunction := arithmeticLoop(unbox, read, mk(isa.OpExtension, 0, 0, 0))
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, unbox, compiledFunction.Body[1], "the view is taken once, ahead of the loop")
	require.Equal(t, read, compiledFunction.Body[3], "the read stays in the loop")

	compiledFunction = arithmeticLoop(unbox, read, mk(isa.OpExtension, 0, 0, 0), mk(isa.OpMoveGeneral, collection, 6, 0))
	original := slices.Clone(compiledFunction.Body)
	require.NoError(t, hoistLoopInvariantArithmetic(t.Context(), compiledFunction, nil))
	require.Equal(t, original, compiledFunction.Body, "a collection reassigned in the loop needs a fresh view each time")
}
