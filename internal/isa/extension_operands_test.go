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
	word := func(a, b, c uint8) Instruction { return NewInstruction(OpExt, a, b, c) }
	none := [NumInstructionOperands]bool{}
	onlyA := [NumInstructionOperands]bool{true, false, false}
	onlyB := [NumInstructionOperands]bool{false, true, false}

	cases := []struct {
		name  string
		owner Instruction

		prefix []Instruction
		word   Instruction
		mask   [NumInstructionOperands]bool
		writes [NumInstructionOperands]bool
		ok     bool
	}{
		{name: "typed make reads the capacity in A", owner: NewTier1Instruction(SubOpMakeSliceInt, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "heap make reads the capacity in A", owner: NewTier1Instruction(SubOpMakeSliceHeap, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "typed get reads the index in A", owner: NewTier1Instruction(SubOpSliceGetFloatDirect, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "typed set stores a register of the element bank", owner: NewTier1Instruction(SubOpSliceSetFloatDirect, 1, 2), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "int append reads the element in A", owner: NewTier1Instruction(SubOpAppendSliceIntDirect, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "float append reads a float", owner: NewTier1Instruction(SubOpAppendSliceFloatDirect, 1, 2), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "slice bounds word with both bounds", owner: NewTier1Instruction(SubOpSliceOp, 1, 2), word: word(SliceLowBoundFlag|SliceHighBoundFlag, 4, 5), mask: [NumInstructionOperands]bool{false, true, true}, ok: true},
		{name: "slice bounds word with only a high bound", owner: NewTier1Instruction(SubOpSliceSliceIntDirect, 1, 2), word: word(SliceHighBoundFlag, 0, 5), mask: [NumInstructionOperands]bool{false, false, true}, ok: true},
		{name: "slice max word when the flag is set", owner: NewTier1Instruction(SubOpSliceOp, 1, 2), prefix: []Instruction{word(SliceMaxBitFlag, 0, 0)}, word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "slice second word without the max flag", owner: NewTier1Instruction(SubOpSliceOp, 1, 2), prefix: []Instruction{word(0, 0, 0)}, word: word(reg, 0, 0), mask: none, ok: true},
		{name: "string slice with a low bound", owner: NewInstruction(OpSliceString, 1, 2, 1), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "string slice with a high bound", owner: NewInstruction(OpSliceString, 1, 2, 2), word: word(0, reg, 0), mask: onlyB, ok: true},
		{name: "builtin int argument", owner: NewTier1Instruction(SubOpCallBuiltin, 3, 1), word: word(reg, uint8(RegisterInt), 0), mask: onlyA, ok: true},
		{name: "builtin general argument with the same number", owner: NewTier1Instruction(SubOpCallBuiltin, 3, 1), word: word(reg, uint8(RegisterGeneral), 0), mask: none, ok: true},
		{name: "deferred int argument", owner: NewInstruction(OpDefer, 1, 1, 0), word: word(0, reg, uint8(RegisterInt)), mask: onlyB, ok: true},
		{name: "spawned string argument", owner: NewInstruction(OpGo, 1, 1, 0), word: word(0, reg, uint8(RegisterString)), mask: none, ok: true},
		{name: "strconv base in A", owner: NewTier1Instruction(SubOpStrconvFormatInt, 1, 2), word: word(reg, 0, 0), mask: onlyA, ok: true},
		{name: "pack typed carries a type index", owner: NewInstruction(OpPackTyped, 1, 2, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "method call carries a name index", owner: NewTier1Instruction(SubOpCallMethod, 0, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "struct field read carries a layout index", owner: NewTier1Instruction(SubOpGetStructFieldString, 1, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "struct field increment has no extension word", owner: NewTier1Instruction(SubOpIncStructFieldInt, 1, 0), word: word(reg, 0, 0), ok: false},
		{name: "spill carries a spill index", owner: NewTier1Instruction(SubOpSpill, reg, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "select case header names a channel", owner: NewTier1Instruction(SubOpSelect, 2, 0), word: word(SelectDirectionReceive, reg, 1), mask: none, ok: true},
		{name: "select receive destination and ok flag are writes", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 1)}, word: word(reg, uint8(RegisterInt), reg), mask: none, writes: [NumInstructionOperands]bool{true, false, true}, ok: true},
		{name: "select receive without ok writes only the destination", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 0)}, word: word(reg, uint8(RegisterString), reg), mask: none, writes: none, ok: true},
		{name: "channel receive into an int register is a write", owner: NewTier1Instruction(SubOpChannelReceive, 1, 2), word: word(reg, uint8(RegisterInt), 0), mask: none, writes: onlyA, ok: true},
		{name: "channel receive into a string register", owner: NewTier1Instruction(SubOpChannelReceive, 1, 2), word: word(reg, uint8(RegisterString), 0), mask: none, writes: none, ok: true},
		{name: "range next writes an int key", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), word: word(1, reg, uint8(RegisterInt)), mask: none, writes: onlyB, ok: true},
		{name: "range next with the key disabled writes nothing", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), word: word(2, reg, uint8(RegisterInt)), mask: none, writes: none, ok: true},
		{name: "range next writes an int value in the second word", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), prefix: []Instruction{word(3, 4, uint8(RegisterGeneral))}, word: word(0, reg, uint8(RegisterInt)), mask: none, writes: onlyB, ok: true},
		{name: "range next string value is not an int write", owner: NewTier1Instruction(SubOpRangeNext, 1, 2), prefix: []Instruction{word(3, 4, uint8(RegisterInt))}, word: word(0, reg, uint8(RegisterString)), mask: none, writes: none, ok: true},
		{name: "map index ok writes the flag", owner: NewInstruction(OpMapIndexOkIntInt, 1, 2, 3), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "interface comparison writes its result", owner: NewTier1Instruction(SubOpEqInterfaceStrict, 1, 2), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "typed copy writes the count", owner: NewTier1Instruction(SubOpCopySliceIntDirect, 1, 2), word: word(reg, 0, 0), mask: none, writes: onlyA, ok: true},
		{name: "fused array field index reads the index in the first word", owner: NewInstruction(OpSetStructFieldIndexGeneral, 1, 2, 0), word: word(4, 0, reg), mask: [NumInstructionOperands]bool{false, false, true}, ok: true},
		{name: "fused array field second word is size and kind", owner: NewInstruction(OpGetStructFieldIndexGeneral, 1, 2, 0), prefix: []Instruction{word(4, 0, 3)}, word: word(reg, 0, uint8(RegisterInt)), mask: none, ok: true},
		{name: "slice field index carries a layout index", owner: NewInstruction(OpSliceIndexStructFieldInt, 1, 2, 3), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "select send of an int register", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionReceive, 1, 0), word(0, 0, 0), word(SelectDirectionSend, 2, 0)}, word: word(reg, uint8(RegisterInt), 0), mask: onlyA, ok: true},
		{name: "select send of a string register", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionSend, 2, 0)}, word: word(reg, uint8(RegisterString), 0), mask: none, ok: true},
		{name: "select header after a default case", owner: NewTier1Instruction(SubOpSelect, 2, 0), prefix: []Instruction{word(SelectDirectionDefault, 0, 0)}, word: word(SelectDirectionSend, reg, 0), mask: none, ok: true},
		{name: "tier-2 make map carries a type index and a size hint", owner: NewTier2Instruction(SubOpTier2MakeMap, 1), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "tier-2 struct literal carries a type index", owner: NewTier2Instruction(SubOpTier2AllocStructLiteral, 1), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "tier-2 increment has no extension word", owner: NewTier2Instruction(SubOpTier2IncInt, 1), word: word(reg, 0, 0), ok: false},
		{name: "make channel carries a type index", owner: NewTier1Instruction(SubOpMakeChannel, 1, 0), word: word(reg, 0, 0), mask: none, ok: true},
		{name: "generic append is not recorded", owner: NewInstruction(OpAppend, 1, 2, 3), word: word(reg, 0, 0), ok: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			words := append(append([]Instruction(nil), testCase.prefix...), testCase.word)
			reads, writes, ok := ExtensionIntUse(testCase.owner, words)
			require.Equal(t, testCase.ok, ok)
			require.Equal(t, testCase.mask, reads)
			require.Equal(t, testCase.writes, writes)
		})
	}
}

func TestExtensionIntUseOnlyDescribesExtensionCarriers(t *testing.T) {
	t.Parallel()
	blank := []Instruction{NewInstruction(OpExt, 0, 0, 0)}
	for code := range math.MaxUint8 + 1 {
		op := Opcode(code)
		if op == OpDrillTier1 || op == OpGo || op == OpDefer {
			continue
		}
		instr := NewInstruction(op, 0, 0, 0)
		if _, _, ok := ExtensionIntUse(instr, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instr).Flags&ShapeFlagFollowsExtension,
			"%s has a recorded extension layout but its shape declares no extension word", op)
	}
	for code := range math.MaxUint8 + 1 {
		instr := NewTier1Instruction(SubOpcode(code), 0, 0)
		if _, _, ok := ExtensionIntUse(instr, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instr).Flags&ShapeFlagFollowsExtension,
			"tier-1 sub-op %d has a recorded extension layout but its shape declares no extension word", code)
	}
	for code := range math.MaxUint8 + 1 {
		instr := NewTier2Instruction(SubOpcodeTier2(code), 0)
		if _, _, ok := ExtensionIntUse(instr, blank); !ok {
			continue
		}
		require.NotZero(t, ShapeForInstruction(instr).Flags&ShapeFlagFollowsExtension,
			"tier-2 sub-op %d has a recorded extension layout but its shape declares no extension word", code)
	}
}
