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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJumpLayoutOfClassifiesEveryLayout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		instr        Instruction
		layout       JumpLayout
		trailingNops int
	}{
		{name: "jump if false", instr: NewInstruction(OpJumpIfFalse, 1, 2, 0), layout: JumpLayoutWord},
		{name: "test nil jump true", instr: NewInstruction(OpTestNilJumpTrue, 1, 2, 0), layout: JumpLayoutWord},
		{name: "type switch case row", instr: NewInstruction(OpTypeSwitchCase, 1, 2, 0), layout: JumpLayoutWord},
		{name: "unconditional jump", instr: NewTier1Instruction(SubOpJump, 2, 0), layout: JumpLayoutWord},
		{name: "add int jump", instr: NewInstruction(OpAddIntJump, 1, 2, 3), layout: JumpLayoutExtension},
		{name: "lt int jump false", instr: NewTier1Instruction(SubOpLtIntJumpFalse, 1, 2), layout: JumpLayoutExtension},
		{name: "le int const jump false", instr: NewTier1Instruction(SubOpLeIntConstJumpFalse, 1, 2), layout: JumpLayoutExtension},
		{name: "eq string const jump false", instr: NewTier1Instruction(SubOpEqStringConstJumpFalse, 1, 2), layout: JumpLayoutExtension},
		{name: "inc int jump lt", instr: NewTier1Instruction(SubOpIncIntJumpLt, 1, 2), layout: JumpLayoutExtension},
		{name: "len string lt jump false", instr: NewTier1Instruction(SubOpLenStringLtJumpFalse, 1, 2), layout: JumpLayoutExtension},
		{name: "eq uint const jump false pads one nop", instr: NewTier1Instruction(SubOpEqUintConstJumpFalse, 1, 2), layout: JumpLayoutExtension, trailingNops: EqUintConstJumpFalseNopCount},
		{name: "range check uint reads the second extension", instr: NewTier2Instruction(SubOpTier2RangeCheckUintJumpFalse, 1), layout: JumpLayoutSecondExtension, trailingNops: RangeCheckUintFusionNopCount},
		{name: "range next slice int", instr: NewInstruction(OpRangeNextSliceInt, 1, 2, 3), layout: JumpLayoutExtension24},
		{name: "range next slice byte", instr: NewInstruction(OpRangeNextSliceByte, 1, 2, 3), layout: JumpLayoutExtension24},
		{name: "type switch dispatch", instr: NewTier1Instruction(SubOpTypeSwitchJump, 1, 2), layout: JumpLayoutMultiWay},
		{name: "make slice int carries an extension but no offset", instr: NewTier1Instruction(SubOpMakeSliceInt, 1, 2), layout: JumpLayoutNone},
		{name: "call method carries an extension but no offset", instr: NewTier1Instruction(SubOpCallMethod, 1, 2), layout: JumpLayoutNone},
		{name: "tier-2 make map carries an extension but no offset", instr: NewTier2Instruction(SubOpTier2MakeMap, 1), layout: JumpLayoutNone},
		{name: "extension word", instr: NewInstruction(OpExt, 1, 2, 3), layout: JumpLayoutNone},
		{name: "tail call transfers control without an offset", instr: NewTier1Instruction(SubOpTailCall, 1, 2), layout: JumpLayoutNone},
		{name: "return transfers control without an offset", instr: NewTier2Instruction(SubOpTier2Return, 1), layout: JumpLayoutNone},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			layout, trailingNops := JumpLayoutOf(testCase.instr)
			require.Equal(t, testCase.layout, layout)
			require.Equal(t, testCase.trailingNops, trailingNops)
		})
	}
}

func TestJumpLayoutAgreesWithSpecAndShape(t *testing.T) {
	t.Parallel()

	offsetlessControlFlow := map[string]bool{
		"TAIL_CALL":         true,
		"TIER2_RETURN":      true,
		"TIER3_RETURN_VOID": true,
	}

	for _, row := range AllSpecs() {
		if row.Has(specReserved) || row.Has(specMeta) {
			continue
		}
		instr := instructionForSpecRow(row)
		layout, _ := JumpLayoutOf(instr)
		require.Equalf(t, row.Has(SpecJump), layout != JumpLayoutNone,
			"%s: SpecJump and JumpLayoutOf disagree", row)

		shape := OperandShapeAt(row.Tier, row.Code)
		if shape.Flags&ShapeFlagControlFlow != 0 && !offsetlessControlFlow[row.Name] {
			require.Truef(t, row.Has(SpecJump),
				"%s has ShapeFlagControlFlow but no SpecJump; every branch with a target must be decodable", row)
		}
		switch layout {
		case JumpLayoutExtension, JumpLayoutSecondExtension, JumpLayoutExtension24:
			require.NotZerof(t, shape.Flags&ShapeFlagFollowsExtension,
				"%s keeps its offset in an extension word but its shape does not declare one", row)
		case JumpLayoutWord:
			require.Zerof(t, shape.Flags&ShapeFlagFollowsExtension,
				"%s keeps its offset in the word but its shape declares an extension", row)
		default:
		}
	}
}

func instructionForSpecRow(row OpSpec) Instruction {
	switch row.Tier {
	case TierMain:
		return NewInstruction(Opcode(row.Code), 1, 2, 3)
	case TierSub1:
		return NewTier1Instruction(SubOpcode(row.Code), 2, 3)
	case TierSub2:
		return NewTier2Instruction(SubOpcodeTier2(row.Code), 3)
	default:
		return NewTier3Instruction(SubOpcodeTier3(row.Code))
	}
}

func TestJumpLayoutWordCount(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, JumpLayoutNone.WordCount())
	require.Equal(t, 1, JumpLayoutWord.WordCount())
	require.Equal(t, 1, JumpLayoutMultiWay.WordCount())
	require.Equal(t, 2, JumpLayoutExtension.WordCount())
	require.Equal(t, 2, JumpLayoutExtension24.WordCount())
	require.Equal(t, 3, JumpLayoutSecondExtension.WordCount())
}
