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

package program

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func offsetExt(offset int16) isa.Instruction {
	lo, hi := isa.SplitOffset(offset)
	return isa.NewInstruction(isa.OpExt, lo, hi, 0)
}

func wordJump(op isa.Opcode, a uint8, offset int16) isa.Instruction {
	lo, hi := isa.SplitOffset(offset)
	return isa.NewInstruction(op, a, lo, hi)
}

func nop() isa.Instruction { return isa.NewInstruction(isa.OpNop, 0, 0, 0) }

func TestJumpTargetAtMatchesHandlerArithmetic(t *testing.T) {
	t.Parallel()

	const offset = 3
	cases := []struct {
		name   string
		body   []isa.Instruction
		target int
	}{
		{
			name:   "jump if false lands at pc+1+offset",
			body:   []isa.Instruction{wordJump(isa.OpJumpIfFalse, 0, offset)},
			target: 1 + offset,
		},
		{
			name:   "test nil jump lands at pc+1+offset",
			body:   []isa.Instruction{wordJump(isa.OpTestNilJumpTrue, 0, offset)},
			target: 1 + offset,
		},
		{
			name:   "type switch row lands at pc+1+offset",
			body:   []isa.Instruction{wordJump(isa.OpTypeSwitchCase, 0, offset)},
			target: 1 + offset,
		},
		{
			name: "unconditional tier-1 jump lands at pc+1+offset",
			body: func() []isa.Instruction {
				lo, hi := isa.SplitOffset(offset)
				return []isa.Instruction{isa.NewTier1Instruction(isa.SubOpJump, lo, hi)}
			}(),
			target: 1 + offset,
		},
		{
			name:   "add int jump lands at pc+2+offset",
			body:   []isa.Instruction{isa.NewInstruction(isa.OpAddIntJump, 0, 1, 2), offsetExt(offset)},
			target: 2 + offset,
		},
		{
			name:   "lt int jump false lands at pc+2+offset",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1), offsetExt(offset)},
			target: 2 + offset,
		},
		{
			name:   "eq int const jump true lands at pc+2+offset",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpEqIntConstJumpTrue, 0, 1), offsetExt(offset)},
			target: 2 + offset,
		},
		{
			name:   "eq string const jump false lands at pc+2+offset",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpEqStringConstJumpFalse, 0, 1), offsetExt(offset)},
			target: 2 + offset,
		},
		{
			name:   "inc int jump lt lands at pc+2+offset and its nop is a real word",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncIntJumpLt, 0, 1), offsetExt(offset), nop()},
			target: 2 + offset,
		},
		{
			name:   "len string lt jump false lands at pc+2+offset",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLenStringLtJumpFalse, 0, 1), offsetExt(offset), nop()},
			target: 2 + offset,
		},
		{
			name:   "eq uint const jump false skips its nop before adding the offset",
			body:   []isa.Instruction{isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 7), offsetExt(offset), nop()},
			target: 2 + isa.EqUintConstJumpFalseNopCount + offset,
		},
		{
			name: "range check uint reads the second extension and skips five nops",
			body: []isa.Instruction{
				isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, 0),
				isa.NewInstruction(isa.OpExt, 1, 10, 0),
				offsetExt(offset),
				nop(), nop(), nop(), nop(), nop(),
			},
			target: 3 + isa.RangeCheckUintFusionNopCount + offset,
		},
		{
			name:   "range next slice int lands at pc+2 plus a 24-bit forward offset",
			body:   []isa.Instruction{isa.NewInstruction(isa.OpRangeNextSliceInt, 0, 1, 2), isa.NewInstruction(isa.OpExt, 0x34, 0x12, 0x01)},
			target: 2 + 0x011234,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			target, ok := JumpTargetAt(testCase.body, 0)
			require.True(t, ok)
			require.Equal(t, testCase.target, target)
		})
	}
}

func TestJumpTargetAtRefusesNonJumps(t *testing.T) {
	t.Parallel()

	misleading := isa.NewInstruction(isa.OpExt, 0xFD, 0xFF, 0)
	cases := []struct {
		name string
		body []isa.Instruction
	}{
		{name: "type switch dispatch has no single target", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 1, 0)}},
		{name: "extension word", body: []isa.Instruction{misleading}},
		{name: "make slice int with extension", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMakeSliceInt, 0, 1), misleading}},
		{name: "call method with extension", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 1), misleading}},
		{name: "tier-2 make map with extension", body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0), misleading}},
		{name: "fused jump missing its extension word", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1)}},
		{name: "tail call", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpTailCall, 0, 0)}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, ok := JumpTargetAt(testCase.body, 0)
			require.False(t, ok)
		})
	}
	_, ok := JumpTargetAt([]isa.Instruction{nop()}, 5)
	require.False(t, ok, "out-of-range pc is not a jump")
}

func TestSetJumpTargetRoundTripsEveryLayout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body []isa.Instruction
	}{
		{name: "word layout", body: []isa.Instruction{wordJump(isa.OpJumpIfFalse, 0, 1), nop(), nop(), nop()}},
		{name: "extension layout", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGeIntJumpFalse, 4, 5), offsetExt(1), nop(), nop()}},
		{name: "extension layout with nop padding", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 4, 5), offsetExt(1), nop(), nop(), nop()}},
		{name: "second extension layout", body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, 4), isa.NewInstruction(isa.OpExt, 1, 2, 0), offsetExt(1), nop(), nop(), nop(), nop(), nop(), nop(), nop()}},
		{name: "24-bit extension layout", body: []isa.Instruction{isa.NewInstruction(isa.OpRangeNextSliceByte, 0, 1, 2), isa.NewInstruction(isa.OpExt, 1, 0, 0), nop(), nop()}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := append([]isa.Instruction(nil), testCase.body...)
			operands := body[0]
			target := len(body) - 1
			require.True(t, SetJumpTarget(body, 0, target))
			decoded, ok := JumpTargetAt(body, 0)
			require.True(t, ok)
			require.Equal(t, target, decoded)

			if layout, _ := isa.JumpLayoutOf(body[0]); layout != isa.JumpLayoutWord {
				require.Equal(t, operands, body[0], "operand bytes must survive a repoint")
			}
		})
	}
}

func TestSetJumpTargetRefusesWhatItCannotEncode(t *testing.T) {
	t.Parallel()

	multiWay := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 1, 0), nop()}
	require.False(t, SetJumpTarget(multiWay, 0, 1), "the type-switch dispatch has no single target to set")

	notAJump := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMakeSliceInt, 0, 1), isa.NewInstruction(isa.OpExt, 9, 9, 9)}
	require.False(t, SetJumpTarget(notAJump, 0, 0))
	require.Equal(t, isa.NewInstruction(isa.OpExt, 9, 9, 9), notAJump[1], "a refused repoint leaves the body untouched")

	backward24 := []isa.Instruction{nop(), isa.NewInstruction(isa.OpRangeNextSliceInt, 0, 1, 2), isa.NewInstruction(isa.OpExt, 1, 0, 0)}
	require.False(t, SetJumpTarget(backward24, 1, 0), "the 24-bit layout is forward only")

	missingExt := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1)}
	require.False(t, SetJumpTarget(missingExt, 0, 0))
}

func TestJumpTargetsOfCollectsEveryLayout(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		wordJump(isa.OpJumpIfFalse, 0, 6),
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1),
		offsetExt(3),
		isa.NewTier1Instruction(isa.SubOpMakeSliceInt, 0, 1),
		isa.NewInstruction(isa.OpExt, 0xFD, 0xFF, 0),
		wordJump(isa.OpTypeSwitchCase, 0, 2),
		nop(),
		nop(),
		isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
	}
	targets := JumpTargetsOf(body)
	require.Equal(t, map[int]bool{6: true, 7: true, 8: true}, targets)
	require.Equal(t, targets, (*CompiledFunction)(nil).BuildJumpTargets(body), "the method form delegates")
}

func TestJumpWordCountAndFootprint(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, JumpWordCount(wordJump(isa.OpJumpIfFalse, 0, 1)))
	require.Equal(t, 2, JumpWordCount(isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1)))
	require.Equal(t, 3, JumpWordCount(isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, 0)))
	require.Equal(t, 1, JumpWordCount(nop()))

	require.Equal(t, 2+isa.EqUintConstJumpFalseNopCount, JumpFootprint(isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 1)))
	require.Equal(t, 3+isa.RangeCheckUintFusionNopCount, JumpFootprint(isa.NewTier2Instruction(isa.SubOpTier2RangeCheckUintJumpFalse, 0)))
	require.Equal(t, 1, JumpFootprint(nop()))
}
