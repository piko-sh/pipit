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

//go:build integration

package bytecode_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func mk(op isa.Opcode, a, b, c uint8) isa.Instruction {
	return isa.NewInstruction(op, a, b, c)
}

func jumpOffset(offset int16) (uint8, uint8) {
	u := uint16(offset)
	return uint8(u), uint8(u >> 8)
}

func TestPeepholeOptimise(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(5)

	tests := []struct {
		name     string
		consts   []int64
		strConst []string
		body     []isa.Instruction
		want     []isa.Opcode

		expectSubOp map[int]isa.SubOpcode
	}{
		{
			name:   "LoadInt64Constant + SubtractInt64 -> SubtractInt64Constant",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpSubtractInt64, 1, 2, 3),
			},
			want: []isa.Opcode{isa.OpSubtractInt64Constant, isa.OpNoOperation},
		},
		{
			name:   "LoadInt64Constant + AddInt64 -> AddInt64Constant",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpAddInt64, 1, 2, 3),
			},
			want: []isa.Opcode{isa.OpAddInt64Constant, isa.OpNoOperation},
		},
		{
			name:   "LoadInt64Constant + MultiplyInt64 -> MultiplyInt64Constant",
			consts: []int64{7},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpMultiplyInt64, 1, 2, 3),
			},
			want: []isa.Opcode{isa.OpMultiplyInt64Constant, isa.OpNoOperation},
		},
		{
			name:   "LoadInt64Constant + LessOrEqualInt64 + JumpIfFalse -> LeIntConstJumpFalse",
			consts: []int64{100},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpLessOrEqualInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse},
		},
		{
			name:   "LoadInt64Constant + LessThanInt64 + JumpIfFalse -> LtIntConstJumpFalse",
			consts: []int64{50},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpLessThanInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpLessThanInt64ConstantThenJumpIfFalse},
		},
		{
			name:   "LoadInt64Constant + EqualInt64 + JumpIfFalse -> EqIntConstJumpFalse",
			consts: []int64{3},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpEqualInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqualInt64ConstantThenJumpIfFalse},
		},
		{
			name:   "LoadInt64Constant + EqualInt64 + JumpIfTrue -> EqIntConstJumpTrue",
			consts: []int64{3},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpEqualInt64, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqualInt64ConstantThenJumpIfTrue},
		},
		{
			name:   "LoadInt64Constant + GreaterOrEqualInt64 + JumpIfFalse -> GeIntConstJumpFalse",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpGreaterOrEqualInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse},
		},
		{
			name:   "LoadInt64Constant + GreaterThanInt64 + JumpIfFalse -> GtIntConstJumpFalse",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpGreaterThanInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse},
		},
		{
			name:   "AddInt64Constant + Jump -> AddIntJump",
			consts: []int64{1},
			body: []isa.Instruction{
				mk(isa.OpAddInt64Constant, 0, 0, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi),
			},
			want: []isa.Opcode{isa.OpAddInt64ConstantThenJump, isa.OpExtension},
		},
		{
			name: "IncrementInt64 + LessThanInt64 + JumpIfTrue -> IncIntJumpLt",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncrementInt64), 0),
				mk(isa.OpLessThanInt64, 2, 0, 1),
				mk(isa.OpJumpIfTrue, 2, lo, hi),
			},
			want: []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
		},
		{
			name: "RuneToString + ConcatenateString -> ConcatRuneString",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertRuneToString), 3, 1),
				mk(isa.OpConcatenateString, 2, 0, 3),
			},
			want: []isa.Opcode{isa.OpConcatenateStringRune, isa.OpNoOperation},
		},
		{
			name:   "LoadInt64Constant small value -> LoadIntConstSmall",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 0, 0, 0),
			},
			want: []isa.Opcode{isa.OpDrillTier1},
		},
		{
			name:   "LoadInt64Constant large value stays unchanged",
			consts: []int64{1000},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 0, 0, 0),
			},
			want: []isa.Opcode{isa.OpLoadInt64Constant},
		},
		{
			name:   "LoadInt64Constant negative value stays unchanged",
			consts: []int64{-1},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 0, 0, 0),
			},
			want: []isa.Opcode{isa.OpLoadInt64Constant},
		},
		{
			name:     "LoadStringConstant + EqualString + JumpIfFalse -> EqStringConstJumpFalse",
			strConst: []string{"hello"},
			body: []isa.Instruction{
				mk(isa.OpLoadStringConstant, 3, 0, 0),
				mk(isa.OpEqualString, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqualStringConstantThenJumpIfFalse},
		},
		{
			name: "LoadNil + EqualGeneral + JumpIfTrue -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqualGeneral, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpJumpIfNil, isa.OpNoOperation, isa.OpNoOperation},
		},
		{
			name: "LoadNil + EqualGeneral + JumpIfFalse -> TestNilJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqualGeneral, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpJumpIfNotNil, isa.OpNoOperation, isa.OpNoOperation},
		},
		{
			name: "LoadNil + NotEqualGeneral + JumpIfTrue -> TestNilJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpNotEqualGeneral, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpJumpIfNotNil, isa.OpNoOperation, isa.OpNoOperation},
		},
		{
			name: "LoadNil + NotEqualGeneral + JumpIfFalse -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpNotEqualGeneral, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpJumpIfNil, isa.OpNoOperation, isa.OpNoOperation},
		},
		{
			name: "LoadNil in B position + EqualGeneral + JumpIfTrue -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqualGeneral, 4, 3, 1),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpJumpIfNil, isa.OpNoOperation, isa.OpNoOperation},
		},
		{
			name: "fusion still fires when jump targets first of pair",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), 0, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertRuneToString), 3, 1),
				mk(isa.OpConcatenateString, 2, 0, 3),
			},
			want: []isa.Opcode{isa.OpDrillTier1, isa.OpConcatenateStringRune, isa.OpNoOperation},
		},
		{
			name: "no fusion when jump targets second of pair",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), 1, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertRuneToString), 3, 1),
				mk(isa.OpConcatenateString, 2, 0, 3),
			},
			want: []isa.Opcode{isa.OpDrillTier1, isa.OpDrillTier1, isa.OpConcatenateString},
		},
		{
			name:   "no fusion when registers don't match",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpAddInt64, 1, 2, 5),
			},
			want: []isa.Opcode{isa.OpDrillTier1, isa.OpAddInt64},
		},
		{
			name: "StringIndex + UintToInt -> StringIndexToInt",
			body: []isa.Instruction{
				mk(isa.OpGetStringByte, 3, 0, 1),
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), 2, 3),
			},
			want: []isa.Opcode{isa.OpGetStringByteAsInt64, isa.OpNoOperation},
		},
		{
			name: "no StringIndexToInt when registers don't match",
			body: []isa.Instruction{
				mk(isa.OpGetStringByte, 3, 0, 1),
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), 2, 5),
			},
			want: []isa.Opcode{isa.OpGetStringByte, isa.OpDrillTier1},
		},
		{
			name: "LengthString + LessThanInt64 + JumpIfFalse -> LenStringLtJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLengthString), 3, 0),
				mk(isa.OpLessThanInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			want: []isa.Opcode{isa.OpDrillTier1, isa.OpExtension, isa.OpNoOperation},
		},
		{
			name: "no LenStringLtJumpFalse when len reg doesn't match",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLengthString), 3, 0),
				mk(isa.OpLessThanInt64, 4, 1, 5),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},

			want:        []isa.Opcode{isa.OpDrillTier1, isa.OpDrillTier1, isa.OpExtension},
			expectSubOp: map[int]isa.SubOpcode{1: isa.SubOpLessThanInt64ThenJumpIfFalse},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body:            make([]isa.Instruction, len(tt.body)),
				IntConstants:    tt.consts,
				StringConstants: tt.strConst,
			}
			copy(compiledFunction.Body, tt.body)
			_ = passes.Optimise(t.Context(), passes.DefaultOptions(), compiledFunction)

			got := make([]isa.Opcode, len(compiledFunction.Body))
			for i, instr := range compiledFunction.Body {
				got[i] = instr.Op
			}
			assert.Equal(t, tt.want, got, "opcode sequence mismatch")
			for index, subOp := range tt.expectSubOp {
				assert.Truef(t, isa.InstrIsTier1SubOp(compiledFunction.Body[index], subOp),
					"instruction %d should be tier-1 %v, got %v",
					index, subOp, isa.InstructionDisplayName(compiledFunction.Body[index]))
			}
		})
	}
}

func TestPeepholePreservesSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		consts []int64
		body   []isa.Instruction
		checkA uint8
		checkB uint8
		checkC uint8
	}{
		{
			name:   "SubtractInt64Constant preserves operand registers",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 5, 0, 0),
				mk(isa.OpSubtractInt64, 1, 2, 5),
			},
			checkA: 1,
			checkB: 2,
			checkC: 0,
		},
		{
			name:   "AddInt64Constant preserves operand registers",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 5, 0, 0),
				mk(isa.OpAddInt64, 3, 4, 5),
			},
			checkA: 3,
			checkB: 4,
			checkC: 0,
		},
		{
			name: "ConcatRuneString preserves operand registers",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertRuneToString), 7, 3),
				mk(isa.OpConcatenateString, 5, 2, 7),
			},
			checkA: 5,
			checkB: 2,
			checkC: 3,
		},
		{
			name: "StringIndexToInt preserves operand registers",
			body: []isa.Instruction{
				mk(isa.OpGetStringByte, 7, 3, 2),
				mk(isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), 5, 7),
			},
			checkA: 5,
			checkB: 3,
			checkC: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body:         make([]isa.Instruction, len(tt.body)),
				IntConstants: tt.consts,
			}
			copy(compiledFunction.Body, tt.body)
			_ = passes.Optimise(t.Context(), passes.DefaultOptions(), compiledFunction)

			fused := compiledFunction.Body[0]
			assert.Equal(t, tt.checkA, fused.A, "A register")
			assert.Equal(t, tt.checkB, fused.B, "B register")
			assert.Equal(t, tt.checkC, fused.C, "C register")
		})
	}
}

func TestPeepholeJumpOffsetAdjustment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		consts       []int64
		body         []isa.Instruction
		fusedOp      isa.Opcode
		expectOffset int16
	}{
		{
			name:   "3-instr fusion adjusts offset by +1",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 3, 0, 0),
				mk(isa.OpLessThanInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, 5, 0),
			},
			fusedOp:      isa.OpDrillTier1,
			expectOffset: 6,
		},
		{
			name: "IncIntJumpLt adjusts offset by +1",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncrementInt64), 0),
				mk(isa.OpLessThanInt64, 2, 0, 1),
				mk(isa.OpJumpIfTrue, 2, 253, 255),
			},
			fusedOp:      isa.OpDrillTier1,
			expectOffset: -2,
		},
		{
			name: "LenStringLtJumpFalse adjusts offset by +1",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLengthString), 3, 0),
				mk(isa.OpLessThanInt64, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, 5, 0),
			},
			fusedOp:      isa.OpDrillTier1,
			expectOffset: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body:         make([]isa.Instruction, len(tt.body)),
				IntConstants: tt.consts,
			}
			copy(compiledFunction.Body, tt.body)
			_ = passes.Optimise(t.Context(), passes.DefaultOptions(), compiledFunction)

			assert.Equal(t, tt.fusedOp, compiledFunction.Body[0].Op)

			extensionWord := compiledFunction.Body[1]
			assert.Equal(t, isa.OpExtension, extensionWord.Op)
			gotOffset := int16(uint16(extensionWord.A) | uint16(extensionWord.B)<<8)
			assert.Equal(t, tt.expectOffset, gotOffset, "adjusted jump offset")
		})
	}
}

func TestPeepholeNilJumpOffset(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
			mk(isa.OpEqualGeneral, 4, 1, 3),
			mk(isa.OpJumpIfTrue, 4, 5, 0),
		},
	}
	_ = passes.Optimise(t.Context(), passes.DefaultOptions(), compiledFunction)

	assert.Equal(t, isa.OpJumpIfNil, compiledFunction.Body[0].Op)
	gotOffset := int16(uint16(compiledFunction.Body[0].B) | uint16(compiledFunction.Body[0].C)<<8)
	assert.Equal(t, int16(7), gotOffset, "nil jump offset adjusts by +2")
}

func TestPeepholeRecursive(t *testing.T) {
	t.Parallel()
	child := &program.CompiledFunction{
		IntConstants: []int64{1},
		Body: []isa.Instruction{
			mk(isa.OpLoadInt64Constant, 3, 0, 0),
			mk(isa.OpAddInt64, 1, 2, 3),
		},
	}
	parent := &program.CompiledFunction{
		Functions: []*program.CompiledFunction{child},
		Body:      []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)},
	}
	_ = passes.Optimise(t.Context(), passes.DefaultOptions(), parent)

	assert.Equal(t, isa.OpAddInt64Constant, child.Body[0].Op)
	assert.Equal(t, isa.OpNoOperation, child.Body[1].Op)
}
