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

package isa

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtensionIntUseLayouts(t *testing.T) {
	t.Parallel()
	const reg = 9
	word := func(a, b, c uint8) Instruction { return NewInstruction(OpExtension, a, b, c) }
	none := [NumInstructionOperands]bool{}
	onlyA := [NumInstructionOperands]bool{true, false, false}
	onlyB := [NumInstructionOperands]bool{false, true, false}

	tests := []struct {
		name  string
		owner Instruction

		prefix []Instruction
		word   Instruction
		mask   [NumInstructionOperands]bool
		writes [NumInstructionOperands]bool
		ok     bool
	}{
		{name: "typed make reads the capacity in A", owner: NewTier1Instruction(SubOpMakeSliceInt64, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "heap make reads the capacity in A", owner: NewTier1Instruction(SubOpMakeSliceHeap, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "typed get reads the index in A", owner: NewTier1Instruction(SubOpGetSliceFloat64Element, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "typed set stores a register of the element bank", owner: NewTier1Instruction(SubOpSetSliceFloat64Element, 1, 2), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "int append reads the element in A", owner: NewTier1Instruction(SubOpAppendSliceInt64, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "float append reads a float", owner: NewTier1Instruction(SubOpAppendSliceFloat64, 1, 2), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "slice bounds word with both bounds", owner: NewTier1Instruction(SubOpSliceGeneral, 1, 2), word: word(SliceLowBoundFlag|SliceHighBoundFlag, 4, 5), mask: [NumInstructionOperands]bool{false, true, true}, ok: true},
		{name: "slice bounds word with only a high bound", owner: NewTier1Instruction(SubOpSliceSliceInt64, 1, 2), word: word(SliceHighBoundFlag, 0, 5), mask: [NumInstructionOperands]bool{false, false, true}, ok: true},
		{name: "slice max word when the flag is set", owner: NewTier1Instruction(SubOpSliceGeneral, 1, 2), prefix: []Instruction{word(SliceMaxBitFlag, 0, 0)}, word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "slice second word without the max flag", owner: NewTier1Instruction(SubOpSliceGeneral, 1, 2), prefix: []Instruction{word(0, 0, 0)}, word: word(reg, 0, 0), mask: none, ok: true},
		{name: "string slice with a low bound", owner: NewInstruction(OpSliceString, 1, 2, 1), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "string slice with a high bound", owner: NewInstruction(OpSliceString, 1, 2, 2), word: word(0, reg, 0), mask: onlyB, ok: true},
		{name: "builtin int argument", owner: NewTier1Instruction(SubOpCallBuiltin, 3, 1), word: word(reg, uint8(RegisterInt64), 0), mask: onlyA, ok: true},
		{name: "builtin general argument with the same number", owner: NewTier1Instruction(SubOpCallBuiltin, 3, 1), word: word(reg, uint8(RegisterGeneral), 0), mask: none, ok: true},
		{name: "deferred int argument", owner: NewInstruction(OpDefer, 1, 1, 0), word: word(0, reg, uint8(RegisterInt64)), mask: onlyB, ok: true},
		{name: "spawned string argument", owner: NewInstruction(OpGo, 1, 1, 0), word: word(0, reg, uint8(RegisterString)), mask: none, ok: true},
		{name: "strconv base in A", owner: NewTier1Instruction(SubOpStrconvFormatInt, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "pack typed carries a type index", owner: NewInstruction(OpBoxAsType, 1, 2, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "method call carries a name index", owner: NewTier1Instruction(SubOpCallMethod, 0, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "struct field read carries a layout index", owner: NewTier1Instruction(SubOpGetStructFieldStringWide, 1, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "struct field increment has no extension word", owner: NewTier1Instruction(SubOpIncrementStructFieldInt64, 1, 0), word: word(reg, 0, 0), ok: false},
		{name: "spill carries a spill index", owner: NewTier1Instruction(SubOpSpill, reg, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "select case header names a channel", owner: NewTier1Instruction(SubOpSelect, 2, 0), word: word(SelectDirectionReceive, reg, 1), mask: none, ok: true},
		{name: "select receive destination and ok flag are writes", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 1)}, word: word(reg, uint8(RegisterInt64), reg), mask: none, writes: [NumInstructionOperands]bool{true, false, true}, ok: true},
		{name: "select receive without ok writes only the destination", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 0)}, word: word(reg, uint8(RegisterString), reg), mask: none, writes: none, ok: true},
		{name: "channel receive into an int register is a write", owner: NewTier1Instruction(SubOpReceiveFromChannel, 1, 2), word: word(reg, uint8(RegisterInt64), 0), mask: none, writes: onlyA, ok: true},
		{name: "channel receive into a string register", owner: NewTier1Instruction(SubOpReceiveFromChannel, 1, 2), word: word(reg, uint8(RegisterString), 0), mask: none, writes: none, ok: true},
		{name: "range next writes an int key", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), word: word(1, reg, uint8(RegisterInt64)), mask: none, writes: onlyB, ok: true},
		{name: "range next with the key disabled writes nothing", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), word: word(2, reg, uint8(RegisterInt64)), mask: none, writes: none, ok: true},
		{name: "range next writes an int value in the second word", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), prefix: []Instruction{word(3, 4, uint8(RegisterGeneral))}, word: word(0, reg, uint8(RegisterInt64)), mask: none, writes: onlyB, ok: true},
		{name: "range next string value is not an int write", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), prefix: []Instruction{word(3, 4, uint8(RegisterInt64))}, word: word(0, reg, uint8(RegisterString)), mask: none, writes: none, ok: true},
		{name: "map index ok writes the flag", owner: NewInstruction(OpGetMapInt64Int64Ok, 1, 2, 3), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "interface comparison writes its result", owner: NewTier1Instruction(SubOpEqualInterfaceStrict, 1, 2), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "typed copy writes the count", owner: NewTier1Instruction(SubOpCopySliceInt64, 1, 2), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "fused array field index reads the index in the first word", owner: NewInstruction(OpSetStructFieldElementGeneral, 1, 2, 0), word: word(4, 0, reg), mask: [NumInstructionOperands]bool{false, false, true}, ok: true},
		{name: "fused array field second word is size and kind", owner: NewInstruction(OpGetStructFieldElementGeneral, 1, 2, 0), prefix: []Instruction{word(4, 0, 3)}, word: word(reg, 0, uint8(RegisterInt64)), mask: none, ok: true},
		{name: "slice field index carries a layout index", owner: NewInstruction(OpGetElementFieldInt64, 1, 2, 3), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "select send of an int register", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 0), word(0, 0, 0), word(SelectDirectionSend, 2, 0)}, word: word(reg, uint8(RegisterInt64), 0), mask: onlyA, ok: true},
		{name: "select send of a string register", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionSend, 2, 0)}, word: word(reg, uint8(RegisterString), 0), mask: none, ok: true},
		{name: "select header after a default case", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionDefault, 0, 0)}, word: word(SelectDirectionSend, reg, 0), mask: none, ok: true},
		{name: "tier-2 make map carries a type index and a size hint", owner: NewTier2Instruction(SubOpTier2MakeMap, 1), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "tier-2 struct literal carries a type index", owner: NewTier2Instruction(SubOpTier2AllocateStructLiteral, 1), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "tier-2 increment has no extension word", owner: NewTier2Instruction(SubOpTier2IncrementInt64, 1), word: word(reg, 0, 0), ok: false},
		{name: "make channel carries a type index", owner: NewTier1Instruction(SubOpMakeChannel, 1, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "generic append is not recorded", owner: NewInstruction(OpAppend, 1, 2, 3), word: word(reg, 0, 0), ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			words := append(append([]Instruction(nil), tt.prefix...), tt.word)
			reads, writes, ok := ExtensionIntUse(tt.owner, words)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.mask, reads)
			require.Equal(t, tt.writes, writes)
		})
	}
}

func TestExtensionIntUseOnlyDescribesExtensionCarriers(t *testing.T) {
	t.Parallel()
	blank := []Instruction{NewInstruction(OpExtension, 0, 0, 0)}
	for code := range math.MaxUint8 + 1 {
		op := Opcode(code)
		if op == OpDrillTier1 {
			continue
		}
		instruction := NewInstruction(op, 0, 0, 0)
		if _, _, ok := ExtensionIntUse(instruction, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instruction).Flags&ShapeFlagFollowsExtension,
			"%s has a recorded extension layout but its shape declares no extension word", op)
	}
	for code := range math.MaxUint8 + 1 {
		instruction := NewTier1Instruction(SubOpcode(code), 0, 0)
		if _, _, ok := ExtensionIntUse(instruction, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instruction).Flags&ShapeFlagFollowsExtension,
			"tier-1 subOp-op %d has a recorded extension layout but its shape declares no extension word", code)
	}
	for code := range math.MaxUint8 + 1 {
		instruction := NewTier2Instruction(SubOpcodeTier2(code), 0)
		if _, _, ok := ExtensionIntUse(instruction, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instruction).Flags&ShapeFlagFollowsExtension,
			"tier-2 subOp-op %d has a recorded extension layout but its shape declares no extension word", code)
	}
}

func TestExtensionNamesOnlyIntRegisters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		owner Instruction
		want  bool
	}{
		{name: "array field index read", owner: NewInstruction(OpGetStructFieldElementGeneral, 1, 2, 3), want: true},
		{name: "array field index write", owner: NewInstruction(OpSetStructFieldElementGeneral, 1, 2, 3), want: true},
		{name: "type assertion", owner: NewInstruction(OpTypeAssert, 1, 2, 3), want: true},
		{name: "typed pack", owner: NewInstruction(OpBoxAsType, 1, 2, 3), want: true},
		{name: "indirect allocation", owner: NewInstruction(OpAllocateIndirect, 1, 2, 3), want: true},
		{name: "struct fast append carries a type index", owner: NewInstruction(OpAppendStruct, 1, 2, 3), want: true},
		{name: "string slice names its int bounds", owner: NewInstruction(OpSliceString, 1, 2, 3), want: true},
		{name: "general slice names its int bounds and flags", owner: NewTier1Instruction(SubOpSliceGeneral, 1, 2), want: true},
		{name: "typed slice expression names its int bounds and flags", owner: NewTier1Instruction(SubOpSliceSliceString, 1, 2), want: true},
		{name: "map construction", owner: NewTier2Instruction(SubOpTier2MakeMap, 1), want: true},
		{name: "struct literal construction", owner: NewTier2Instruction(SubOpTier2AllocateStructLiteral, 1), want: true},
		{name: "typed slice read names its int index", owner: NewTier1Instruction(SubOpGetSliceByteElement, 1, 2), want: true},
		{name: "typed string slice read names its int index", owner: NewTier1Instruction(SubOpGetSliceStringElement, 1, 2), want: true},
		{name: "struct field read carries a layout index", owner: NewTier1Instruction(SubOpGetStructFieldStringWide, 1, 2), want: true},
		{name: "struct field write carries a layout index", owner: NewTier1Instruction(SubOpSetStructFieldUint64Wide, 1, 2), want: true},
		{name: "struct slice field read carries a layout index", owner: NewTier1Instruction(SubOpGetStructFieldSliceInt64, 1, 2), want: true},
		{name: "struct slice field write carries a layout index", owner: NewTier1Instruction(SubOpSetStructFieldSliceByte, 1, 2), want: true},
		{name: "a typed slice store names the stored value in its element bank", owner: NewTier1Instruction(SubOpSetSliceStringElement, 1, 2), want: false},
		{name: "a byte slice store names the stored value as a uint", owner: NewTier1Instruction(SubOpSetSliceByteElement, 1, 2), want: false},
		{name: "a deferred call names registers of any bank", owner: NewInstruction(OpDefer, 1, 2, 0), want: false},
		{name: "a builtin call names registers of any bank", owner: NewTier1Instruction(SubOpCallBuiltin, 0, 1), want: false},
		{name: "another tier-2 operation", owner: NewTier2Instruction(SubOpTier2IncrementInt64, 1), want: false},
		{name: "an unlisted operation", owner: NewInstruction(OpAppend, 1, 2, 3), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ExtensionNamesOnlyIntRegisters(tt.owner))
		})
	}
}

func TestExtensionNamesOnlyIntRegistersImpliesARecordedIntLayout(t *testing.T) {
	t.Parallel()

	blank := []Instruction{NewInstruction(OpExtension, 0, 0, 0)}
	owners := make([]Instruction, 0, 3*(math.MaxUint8+1))
	for code := range math.MaxUint8 + 1 {
		owners = append(owners,
			NewInstruction(Opcode(code), 0, 0, 0),
			NewTier1Instruction(SubOpcode(code), 0, 0),
			NewTier2Instruction(SubOpcodeTier2(code), 0))
	}
	for _, owner := range owners {
		if !ExtensionNamesOnlyIntRegisters(owner) {
			continue
		}
		_, _, ok := ExtensionIntUse(owner, blank)
		require.Truef(t, ok, "%s is cleared as naming only int registers but has no recorded int layout",
			InstructionDisplayName(owner))
	}
}

func TestExtensionRegisterUseSeparatesScalarStoreBanks(t *testing.T) {
	t.Parallel()
	_, _, ok := ExtensionRegisterUse(NewInstruction(OpDefer, 0, 0, 0), []Instruction{NewInstruction(OpExtension, 3, 0, 0)}, RegisterGeneral)
	require.False(t, ok, "a deferred call has no recorded register layout")
	tests := []struct {
		subOpcode SubOpcode
		source    RegisterKind
	}{
		{subOpcode: SubOpSetSliceFloat64Element, source: RegisterFloat64},
		{subOpcode: SubOpSetSliceStringElement, source: RegisterString},
		{subOpcode: SubOpSetSliceBoolElement, source: RegisterBool},
		{subOpcode: SubOpSetSliceUint64Element, source: RegisterUint64},
		{subOpcode: SubOpSetSliceByteElement, source: RegisterUint64},
	}
	for _, tt := range tests {
		t.Run(tt.subOpcode.String(), func(t *testing.T) {
			t.Parallel()
			for _, bank := range []RegisterKind{RegisterInt64, RegisterGeneral, RegisterFloat64, RegisterString, RegisterBool, RegisterUint64, RegisterSliceByte} {
				reads, writes, ok := ExtensionRegisterUse(NewTier1Instruction(tt.subOpcode, 0, 1), []Instruction{NewInstruction(OpExtension, 3, 0, 0)}, bank)
				require.True(t, ok, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{bank == tt.source, false, false}, reads, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{}, writes, "bank %s", bank)
			}
		})
	}
}

func TestExtensionRegisterUseRecordsWordsThatNameNoGeneralRegister(t *testing.T) {
	t.Parallel()
	word := []Instruction{NewInstruction(OpExtension, 3, 4, 5)}
	owners := []Instruction{
		NewInstruction(OpMakeSlice, 0, 1, 2),
		NewTier1Instruction(SubOpLessThanInt64ThenJumpIfFalse, 1, 2),
		NewTier1Instruction(SubOpMakeSliceInt64, 1, 2),
		NewTier1Instruction(SubOpMathMod, 1, 2),
		NewTier1Instruction(SubOpReload, 1, uint8(RegisterGeneral)),
		NewTier1Instruction(SubOpGetGlobalWide, 1, uint8(RegisterGeneral)),
		NewTier2Instruction(SubOpTier2InRangeUint64ThenJumpIfFalse, 1),
	}
	for _, owner := range owners {
		reads, writes, ok := ExtensionRegisterUse(owner, word, RegisterGeneral)
		require.True(t, ok, InstructionDisplayName(owner))
		require.Equal(t, ExtensionIntMask{}, reads, InstructionDisplayName(owner))
		require.Equal(t, ExtensionIntMask{}, writes, InstructionDisplayName(owner))
	}
	_, _, ok := ExtensionRegisterUse(NewTier1Instruction(SubOpMathMod, 1, 2), word, RegisterFloat64)
	require.False(t, ok, "the record covers the general bank only, as MATH_MOD's word names a float register")
}

func TestExtensionRegisterUseRecordsWordsThatNameNoRegisterOfAnyBank(t *testing.T) {
	t.Parallel()
	word := []Instruction{NewInstruction(OpExtension, 0, 0, 0)}
	owners := []SubOpcode{
		SubOpGetGlobalWide, SubOpSetGlobalWide, SubOpCallMethod, SubOpCallMethodInlineable,
		SubOpAddUint64Constant, SubOpSubtractUint64Constant, SubOpBitAndUint64Constant,
		SubOpAddUint32Constant, SubOpSubtractUint32Constant,
		SubOpIncrementInt64ThenJumpIfLessThan, SubOpLessThanLengthStringThenJumpIfFalse,
		SubOpIncrementInt64ThenJumpIfLessOrEqual, SubOpEqualUint64ThenJumpIfFalse, SubOpNotEqualUint64ThenJumpIfFalse,
		SubOpLessThanInt64ThenJumpIfFalse, SubOpLessOrEqualInt64ThenJumpIfFalse, SubOpGreaterThanInt64ThenJumpIfFalse,
		SubOpGreaterOrEqualInt64ThenJumpIfFalse, SubOpEqualInt64ThenJumpIfFalse, SubOpNotEqualInt64ThenJumpIfFalse,
		SubOpLessOrEqualInt64ConstantThenJumpIfFalse, SubOpLessThanInt64ConstantThenJumpIfFalse,
		SubOpEqualInt64ConstantThenJumpIfFalse, SubOpEqualInt64ConstantThenJumpIfTrue,
		SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, SubOpGreaterThanInt64ConstantThenJumpIfFalse,
		SubOpEqualStringConstantThenJumpIfFalse, SubOpEqualUint64ImmediateThenJumpIfFalse,
	}
	banks := []RegisterKind{
		RegisterInt64, RegisterFloat64, RegisterString, RegisterGeneral, RegisterBool, RegisterUint64, RegisterComplex128,
		RegisterSliceInt64, RegisterSliceByte,
	}
	for _, subOpcode := range owners {
		owner := NewTier1Instruction(subOpcode, 1, 2)
		require.True(t, ShapeForInstruction(owner).Flags&ShapeFlagFollowsExtension != 0, "%s carries an extension word", subOpcode)
		for _, bank := range banks {
			reads, writes, ok := ExtensionRegisterUse(owner, word, bank)
			require.True(t, ok, "%s in bank %s", subOpcode, bank)
			require.Equal(t, ExtensionIntMask{}, reads, "%s in bank %s: a zero byte is not register zero", subOpcode, bank)
			require.Equal(t, ExtensionIntMask{}, writes, "%s in bank %s", subOpcode, bank)
		}
	}
	for _, subOpcode := range []SubOpcodeTier2{SubOpTier2JumpIfFalseBool, SubOpTier2JumpIfTrueBool, SubOpTier2InRangeUint64ThenJumpIfFalse} {
		owner := NewTier2Instruction(subOpcode, 1)
		require.True(t, ShapeForInstruction(owner).Flags&ShapeFlagFollowsExtension != 0, "%s carries an extension word", subOpcode)
		for _, bank := range banks {
			reads, writes, ok := ExtensionRegisterUse(owner, word, bank)
			require.True(t, ok, "%s in bank %s", subOpcode, bank)
			require.Equal(t, ExtensionIntMask{}, reads, "%s in bank %s: an immediate or a jump offset is not a register", subOpcode, bank)
			require.Equal(t, ExtensionIntMask{}, writes, "%s in bank %s", subOpcode, bank)
		}
	}
	tableIndexOwners := []Opcode{
		OpGetElementFieldInt64, OpGetElementFieldUint64, OpGetElementFieldFloat64, OpGetElementFieldBool, OpGetElementFieldString,
		OpCopyStructFieldGeneral, OpMakeSlice, OpConvertGeneral,
	}
	for _, opcode := range tableIndexOwners {
		owner := NewInstruction(opcode, 1, 2, 3)
		require.True(t, ShapeForInstruction(owner).Flags&ShapeFlagFollowsExtension != 0, "%s carries an extension word", opcode)
		for _, bank := range banks {
			reads, writes, ok := ExtensionRegisterUse(owner, word, bank)
			require.True(t, ok, "%s in bank %s", opcode, bank)
			require.Equal(t, ExtensionIntMask{}, reads, "%s in bank %s: a layout or type index is not a register", opcode, bank)
			require.Equal(t, ExtensionIntMask{}, writes, "%s in bank %s", opcode, bank)
		}
	}
	_, _, ok := ExtensionRegisterUse(NewTier1Instruction(SubOpMathMod, 1, 2), word, RegisterUint64)
	require.False(t, ok, "an operation outside the list keeps its unknown layout")
}

func TestExtensionRegisterUseOfMultiplyThenAddNamesItsAddedRegister(t *testing.T) {
	t.Parallel()
	owner := NewInstruction(OpMultiplyInt64ConstantThenAdd, 1, 2, 3)
	word := []Instruction{NewInstruction(OpExtension, 5, 0, 0)}
	require.True(t, ShapeForInstruction(owner).Flags&ShapeFlagFollowsExtension != 0)

	reads, writes, ok := ExtensionRegisterUse(owner, word, RegisterInt64)
	require.True(t, ok)
	require.Equal(t, ExtensionIntMask{true, false, false}, reads, "byte A names the added register")
	require.Equal(t, ExtensionIntMask{}, writes)

	for _, bank := range []RegisterKind{RegisterUint64, RegisterFloat64, RegisterString, RegisterGeneral, RegisterBool} {
		reads, writes, ok = ExtensionRegisterUse(owner, word, bank)
		require.True(t, ok, "bank %s", bank)
		require.Equal(t, ExtensionIntMask{}, reads, "bank %s: the word names an int register only", bank)
		require.Equal(t, ExtensionIntMask{}, writes, "bank %s", bank)
	}
}

func TestExtensionRegisterUseOfCommaOkLookupsNamesOnlyTheOkFlag(t *testing.T) {
	t.Parallel()
	word := []Instruction{NewInstruction(OpExtension, 5, 5, 5)}
	lookups := []Opcode{
		OpGetMapOk, OpGetMapInt64Int64Ok, OpGetMapStringInt64Ok, OpGetMapStringStringOk,
		OpGetMapInt64StringOk, OpGetMapInt64GeneralOk, OpGetMapStringGeneralOk,
		OpGetMapInt64Int64OkThenJumpIfFalse, OpGetMapStringInt64OkThenJumpIfFalse,
		OpGetMapStringStringOkThenJumpIfFalse, OpGetMapInt64StringOkThenJumpIfFalse,
		OpGetMapInt64GeneralOkThenJumpIfFalse, OpGetMapStringGeneralOkThenJumpIfFalse,
	}
	for _, opcode := range lookups {
		owner := NewInstruction(opcode, 1, 2, 3)
		reads, writes, ok := ExtensionRegisterUse(owner, word, RegisterInt64)
		require.True(t, ok, "%s", opcode)
		require.Equal(t, ExtensionIntMask{}, reads, "%s reads no int register through its extension word", opcode)
		require.Equal(t, ExtensionIntMask{true, false, false}, writes, "%s writes its ok flag to the int register in byte A", opcode)
		for _, bank := range []RegisterKind{RegisterGeneral, RegisterString, RegisterUint64, RegisterFloat64} {
			reads, writes, ok = ExtensionRegisterUse(owner, word, bank)
			require.True(t, ok, "%s in bank %s", opcode, bank)
			require.Equal(t, ExtensionIntMask{}, reads, "%s in bank %s", opcode, bank)
			require.Equal(t, ExtensionIntMask{}, writes, "%s in bank %s", opcode, bank)
		}
	}
}

func TestExtensionRegisterUseOfAByteStoreRunNamesOnlyTheValuesItStores(t *testing.T) {
	t.Parallel()
	owner := NewTier1Instruction(SubOpSetSliceByteElementRun, 1, 2)
	for count := uint8(ByteRunMinStores); count <= ByteRunMaxStores; count++ {
		words := []Instruction{NewInstruction(OpExtension, 5, 9, count)}
		for len(words) < int(count) {
			words = append(words, NewInstruction(OpExtension, 6, 6, 6))
		}
		for last := 1; last <= len(words); last++ {
			reads, writes, ok := ExtensionRegisterUse(owner, words[:last], RegisterUint64)
			require.True(t, ok)
			require.Equal(t, ExtensionIntMask{true, false, false}, reads, "only byte A of each word is a register")
			require.Equal(t, ExtensionIntMask{}, writes)
			for _, bank := range []RegisterKind{RegisterInt64, RegisterGeneral, RegisterFloat64, RegisterString, RegisterBool, RegisterSliceByte} {
				reads, writes, ok = ExtensionRegisterUse(owner, words[:last], bank)
				require.True(t, ok, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{}, reads, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{}, writes, "bank %s", bank)
			}
		}
	}
}

func TestExtensionRegisterUseOfAnElementFieldStoreNamesOnlyTheRegisterItMayLeaveTheElementIn(t *testing.T) {
	t.Parallel()
	words := []Instruction{NewInstruction(OpExtension, 4, 5, 6)}
	for _, opcode := range []Opcode{OpSetElementFieldInt64, OpSetElementFieldFloat64} {
		owner := NewInstruction(opcode, 1, 2, 3)
		require.True(t, IsElementFieldStore(owner))
		for kind := range RegisterKind(NumRegisterKinds) {
			reads, writes, ok := ExtensionRegisterUse(owner, words, kind)
			require.True(t, ok)
			require.Equal(t, ExtensionIntMask{}, reads)
			want := ExtensionIntMask{}
			if kind == RegisterGeneral {
				want = ExtensionIntMask{false, false, true}
			}
			require.Equal(t, want, writes, "only byte C names a register, and it is a general one")
		}
	}
	require.False(t, IsElementFieldStore(NewInstruction(OpGetElementFieldInt64, 1, 2, 3)))
	require.False(t, IsElementFieldStore(NewInstruction(OpSetStructFieldInt64, 1, 2, 3)))
	require.False(t, IsElementFieldStore(words[0]))
}

func TestExtensionRegisterUseOfAnElementFieldRunNamesItsDestinationsAndItsReceiver(t *testing.T) {
	t.Parallel()
	owner := NewTier1Instruction(SubOpGetElementFieldUint64Run, 1, 2)
	for count := uint8(FieldRunMinReads); count <= FieldRunMaxReads; count++ {
		words := []Instruction{NewInstruction(OpExtension, 5, 9, count|4<<FieldRunModeShift), NewInstruction(OpExtension, 6, 6, 3)}
		for len(words) < int(count) {
			words = append(words, NewInstruction(OpExtension, 7, 7, 0))
		}
		for last := 1; last <= len(words); last++ {
			reads, writes, ok := ExtensionRegisterUse(owner, words[:last], RegisterUint64)
			require.True(t, ok)
			require.Equal(t, ExtensionIntMask{}, reads)
			require.Equal(t, ExtensionIntMask{true, false, false}, writes, "byte A of each word is a uint destination")

			reads, writes, ok = ExtensionRegisterUse(owner, words[:last], RegisterGeneral)
			require.True(t, ok)
			require.Equal(t, ExtensionIntMask{}, reads)
			require.Equal(t, ExtensionIntMask{false, false, last == ElementFieldRunScratchWord+1}, writes,
				"byte C of the second word, and of no other, is the general register a debugger is shown the element in")

			for _, bank := range []RegisterKind{RegisterInt64, RegisterFloat64, RegisterString, RegisterBool, RegisterSliceByte} {
				reads, writes, ok = ExtensionRegisterUse(owner, words[:last], bank)
				require.True(t, ok)
				require.Equal(t, ExtensionIntMask{}, reads)
				require.Equal(t, ExtensionIntMask{}, writes)
			}
		}
	}
	parts, least, most, ok := RunParts(owner, NewInstruction(OpExtension, 0, 0, 3|0x40))
	require.True(t, ok)
	require.Equal(t, 3, parts)
	require.Equal(t, FieldRunMinReads, least)
	require.Equal(t, FieldRunMaxReads, most)
}

func TestExtensionRegisterUseOfAFieldReadRunNamesOnlyTheRegistersItWrites(t *testing.T) {
	t.Parallel()
	owner := NewTier1Instruction(SubOpGetStructFieldUint64Run, 1, 2)
	for count := uint8(FieldRunMinReads); count <= FieldRunMaxReads; count++ {
		words := []Instruction{NewInstruction(OpExtension, 5, 9, count|4<<FieldRunModeShift)}
		for len(words) < int(count) {
			words = append(words, NewInstruction(OpExtension, 6, 6, 0))
		}
		for last := 1; last <= len(words); last++ {
			reads, writes, ok := ExtensionRegisterUse(owner, words[:last], RegisterUint64)
			require.True(t, ok)
			require.Equal(t, ExtensionIntMask{}, reads)
			require.Equal(t, ExtensionIntMask{true, false, false}, writes, "only byte A of each word is a register")
			for _, bank := range []RegisterKind{RegisterInt64, RegisterGeneral, RegisterFloat64, RegisterString, RegisterBool, RegisterSliceByte} {
				reads, writes, ok = ExtensionRegisterUse(owner, words[:last], bank)
				require.True(t, ok, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{}, reads, "bank %s", bank)
				require.Equal(t, ExtensionIntMask{}, writes, "bank %s", bank)
			}
		}
	}
	parts, least, most, ok := RunParts(owner, NewInstruction(OpExtension, 5, 9, 3|4<<FieldRunModeShift))
	require.True(t, ok)
	require.Equal(t, []int{3, FieldRunMinReads, FieldRunMaxReads}, []int{parts, least, most})
	_, _, _, ok = RunParts(NewInstruction(OpAddInt64, 0, 0, 0), NewInstruction(OpExtension, 0, 0, 0))
	require.False(t, ok)
}

func TestRunPartsReadsTheCountOfEachRun(t *testing.T) {
	t.Parallel()

	parts, least, most, ok := RunParts(NewTier1Instruction(SubOpSetSliceByteElementRun, 0, 0), NewInstruction(OpExtension, 0, 0, 3))
	require.True(t, ok)
	require.Equal(t, 3, parts)
	require.Equal(t, ByteRunMinStores, least)
	require.Equal(t, ByteRunMaxStores, most)

	parts, least, most, ok = RunParts(NewTier1Instruction(SubOpGetStructFieldUint64Run, 0, 0), NewInstruction(OpExtension, 0, 0, 2|0x30))
	require.True(t, ok)
	require.Equal(t, 2, parts, "the count is the masked part of the byte")
	require.Equal(t, FieldRunMinReads, least)
	require.Equal(t, FieldRunMaxReads, most)

	_, _, _, ok = RunParts(NewInstruction(OpAddInt64, 1, 2, 3), NewInstruction(OpExtension, 0, 0, 3))
	require.False(t, ok, "an instruction that is no run counts no parts")
}

func TestExtensionConstantIndexNamesTheUintConstantOfTheConstantArithmeticForms(t *testing.T) {
	t.Parallel()

	word := NewInstruction(OpExtension, 0x34, 0x12, 0xFF)
	for _, subOpcode := range []SubOpcode{
		SubOpAddUint64Constant, SubOpSubtractUint64Constant, SubOpBitAndUint64Constant,
		SubOpAddUint32Constant, SubOpSubtractUint32Constant,
	} {
		index, kind, ok := ExtensionConstantIndex(NewTier1Instruction(subOpcode, 1, 2), word)
		require.True(t, ok, "%s", subOpcode)
		require.Equal(t, 0x1234, index, "%s: bytes A and B hold the index, low byte first", subOpcode)
		require.Equal(t, RegisterUint64, kind, "%s", subOpcode)
	}
	others := []Instruction{
		NewTier1Instruction(SubOpEqualUint64ThenJumpIfFalse, 1, 2),
		NewTier1Instruction(SubOpGetGlobalWide, 1, 2),
		NewInstruction(OpMultiplyInt64ConstantThenAdd, 1, 2, 3),
		NewTier2Instruction(SubOpTier2JumpIfFalseBool, 1),
	}
	for _, owner := range others {
		_, _, ok := ExtensionConstantIndex(owner, word)
		require.False(t, ok, "%s keeps no constant index in its extension word", InstructionDisplayName(owner))
	}
}
