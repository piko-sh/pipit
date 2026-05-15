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
	"context"
	"testing"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/assert"
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
		expect   []isa.Opcode

		expectSubOp map[int]isa.SubOpcode
	}{
		{
			name:   "LoadIntConst + SubInt -> SubIntConst",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpSubInt, 1, 2, 3),
			},
			expect: []isa.Opcode{isa.OpSubIntConst, isa.OpNop},
		},
		{
			name:   "LoadIntConst + AddInt -> AddIntConst",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpAddInt, 1, 2, 3),
			},
			expect: []isa.Opcode{isa.OpAddIntConst, isa.OpNop},
		},
		{
			name:   "LoadIntConst + MulInt -> MulIntConst",
			consts: []int64{7},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpMulInt, 1, 2, 3),
			},
			expect: []isa.Opcode{isa.OpMulIntConst, isa.OpNop},
		},
		{
			name:   "LoadIntConst + LeInt + JumpIfFalse -> LeIntConstJumpFalse",
			consts: []int64{100},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpLeInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpLeIntConstJumpFalse},
		},
		{
			name:   "LoadIntConst + LtInt + JumpIfFalse -> LtIntConstJumpFalse",
			consts: []int64{50},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpLtInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpLtIntConstJumpFalse},
		},
		{
			name:   "LoadIntConst + EqInt + JumpIfFalse -> EqIntConstJumpFalse",
			consts: []int64{3},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpEqInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqIntConstJumpFalse},
		},
		{
			name:   "LoadIntConst + EqInt + JumpIfTrue -> EqIntConstJumpTrue",
			consts: []int64{3},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpEqInt, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqIntConstJumpTrue},
		},
		{
			name:   "LoadIntConst + GeInt + JumpIfFalse -> GeIntConstJumpFalse",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpGeInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpGeIntConstJumpFalse},
		},
		{
			name:   "LoadIntConst + GtInt + JumpIfFalse -> GtIntConstJumpFalse",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpGtInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpGtIntConstJumpFalse},
		},
		{
			name:   "AddIntConst + Jump -> AddIntJump",
			consts: []int64{1},
			body: []isa.Instruction{
				mk(isa.OpAddIntConst, 0, 0, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi),
			},
			expect: []isa.Opcode{isa.OpAddIntJump, isa.OpExt},
		},
		{
			name: "IncInt + LtInt + JumpIfTrue -> IncIntJumpLt",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), 0),
				mk(isa.OpLtInt, 2, 0, 1),
				mk(isa.OpJumpIfTrue, 2, lo, hi),
			},
			expect: []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
		},
		{
			name: "RuneToString + ConcatString -> ConcatRuneString",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpRuneToString), 3, 1),
				mk(isa.OpConcatString, 2, 0, 3),
			},
			expect: []isa.Opcode{isa.OpConcatRuneString, isa.OpNop},
		},
		{
			name:   "LoadIntConst small value -> LoadIntConstSmall",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 0, 0, 0),
			},
			expect: []isa.Opcode{isa.OpDrillTier1},
		},
		{
			name:   "LoadIntConst large value stays unchanged",
			consts: []int64{1000},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 0, 0, 0),
			},
			expect: []isa.Opcode{isa.OpLoadIntConst},
		},
		{
			name:   "LoadIntConst negative value stays unchanged",
			consts: []int64{-1},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 0, 0, 0),
			},
			expect: []isa.Opcode{isa.OpLoadIntConst},
		},
		{
			name:     "LoadStringConst + EqString + JumpIfFalse -> EqStringConstJumpFalse",
			strConst: []string{"hello"},
			body: []isa.Instruction{
				mk(isa.OpLoadStringConst, 3, 0, 0),
				mk(isa.OpEqString, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
			expectSubOp: map[int]isa.SubOpcode{0: isa.SubOpEqStringConstJumpFalse},
		},
		{
			name: "LoadNil + EqGeneral + JumpIfTrue -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqGeneral, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpTestNilJumpTrue, isa.OpNop, isa.OpNop},
		},
		{
			name: "LoadNil + EqGeneral + JumpIfFalse -> TestNilJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqGeneral, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpTestNilJumpFalse, isa.OpNop, isa.OpNop},
		},
		{
			name: "LoadNil + NeGeneral + JumpIfTrue -> TestNilJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpNeGeneral, 4, 1, 3),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpTestNilJumpFalse, isa.OpNop, isa.OpNop},
		},
		{
			name: "LoadNil + NeGeneral + JumpIfFalse -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpNeGeneral, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpTestNilJumpTrue, isa.OpNop, isa.OpNop},
		},
		{
			name: "LoadNil in B position + EqGeneral + JumpIfTrue -> TestNilJumpTrue",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), 3),
				mk(isa.OpEqGeneral, 4, 3, 1),
				mk(isa.OpJumpIfTrue, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpTestNilJumpTrue, isa.OpNop, isa.OpNop},
		},
		{
			name: "fusion still fires when jump targets first of pair",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), 0, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpRuneToString), 3, 1),
				mk(isa.OpConcatString, 2, 0, 3),
			},
			expect: []isa.Opcode{isa.OpDrillTier1, isa.OpConcatRuneString, isa.OpNop},
		},
		{
			name: "no fusion when jump targets second of pair",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpJump), 1, 0),
				mk(isa.OpDrillTier1, uint8(isa.SubOpRuneToString), 3, 1),
				mk(isa.OpConcatString, 2, 0, 3),
			},
			expect: []isa.Opcode{isa.OpDrillTier1, isa.OpDrillTier1, isa.OpConcatString},
		},
		{
			name:   "no fusion when registers don't match",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpAddInt, 1, 2, 5),
			},
			expect: []isa.Opcode{isa.OpDrillTier1, isa.OpAddInt},
		},
		{
			name: "StringIndex + UintToInt -> StringIndexToInt",
			body: []isa.Instruction{
				mk(isa.OpStringIndex, 3, 0, 1),
				mk(isa.OpDrillTier1, uint8(isa.SubOpUintToInt), 2, 3),
			},
			expect: []isa.Opcode{isa.OpStringIndexToInt, isa.OpNop},
		},
		{
			name: "no StringIndexToInt when registers don't match",
			body: []isa.Instruction{
				mk(isa.OpStringIndex, 3, 0, 1),
				mk(isa.OpDrillTier1, uint8(isa.SubOpUintToInt), 2, 5),
			},
			expect: []isa.Opcode{isa.OpStringIndex, isa.OpDrillTier1},
		},
		{
			name: "LenString + LtInt + JumpIfFalse -> LenStringLtJumpFalse",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLenString), 3, 0),
				mk(isa.OpLtInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},
			expect: []isa.Opcode{isa.OpDrillTier1, isa.OpExt, isa.OpNop},
		},
		{
			name: "no LenStringLtJumpFalse when len reg doesn't match",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLenString), 3, 0),
				mk(isa.OpLtInt, 4, 1, 5),
				mk(isa.OpJumpIfFalse, 4, lo, hi),
			},

			expect:      []isa.Opcode{isa.OpDrillTier1, isa.OpDrillTier1, isa.OpExt},
			expectSubOp: map[int]isa.SubOpcode{1: isa.SubOpLtIntJumpFalse},
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
			_ = passes.Optimise(context.Background(), passes.DefaultOptions(), compiledFunction)

			got := make([]isa.Opcode, len(compiledFunction.Body))
			for i, instr := range compiledFunction.Body {
				got[i] = instr.Op
			}
			assert.Equal(t, tt.expect, got, "opcode sequence mismatch")
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
			name:   "SubIntConst preserves operand registers",
			consts: []int64{42},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 5, 0, 0),
				mk(isa.OpSubInt, 1, 2, 5),
			},
			checkA: 1,
			checkB: 2,
			checkC: 0,
		},
		{
			name:   "AddIntConst preserves operand registers",
			consts: []int64{10},
			body: []isa.Instruction{
				mk(isa.OpLoadIntConst, 5, 0, 0),
				mk(isa.OpAddInt, 3, 4, 5),
			},
			checkA: 3,
			checkB: 4,
			checkC: 0,
		},
		{
			name: "ConcatRuneString preserves operand registers",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpRuneToString), 7, 3),
				mk(isa.OpConcatString, 5, 2, 7),
			},
			checkA: 5,
			checkB: 2,
			checkC: 3,
		},
		{
			name: "StringIndexToInt preserves operand registers",
			body: []isa.Instruction{
				mk(isa.OpStringIndex, 7, 3, 2),
				mk(isa.OpDrillTier1, uint8(isa.SubOpUintToInt), 5, 7),
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
			_ = passes.Optimise(context.Background(), passes.DefaultOptions(), compiledFunction)

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
				mk(isa.OpLoadIntConst, 3, 0, 0),
				mk(isa.OpLtInt, 4, 1, 3),
				mk(isa.OpJumpIfFalse, 4, 5, 0),
			},
			fusedOp:      isa.OpDrillTier1,
			expectOffset: 6,
		},
		{
			name: "IncIntJumpLt adjusts offset by +1",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), 0),
				mk(isa.OpLtInt, 2, 0, 1),
				mk(isa.OpJumpIfTrue, 2, 252, 255),
			},
			fusedOp:      isa.OpDrillTier1,
			expectOffset: -3,
		},
		{
			name: "LenStringLtJumpFalse adjusts offset by +1",
			body: []isa.Instruction{
				mk(isa.OpDrillTier1, uint8(isa.SubOpLenString), 3, 0),
				mk(isa.OpLtInt, 4, 1, 3),
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
			_ = passes.Optimise(context.Background(), passes.DefaultOptions(), compiledFunction)

			assert.Equal(t, tt.fusedOp, compiledFunction.Body[0].Op)

			extensionWord := compiledFunction.Body[1]
			assert.Equal(t, isa.OpExt, extensionWord.Op)
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
			mk(isa.OpEqGeneral, 4, 1, 3),
			mk(isa.OpJumpIfTrue, 4, 5, 0),
		},
	}
	_ = passes.Optimise(context.Background(), passes.DefaultOptions(), compiledFunction)

	assert.Equal(t, isa.OpTestNilJumpTrue, compiledFunction.Body[0].Op)
	gotOffset := int16(uint16(compiledFunction.Body[0].B) | uint16(compiledFunction.Body[0].C)<<8)
	assert.Equal(t, int16(7), gotOffset, "nil jump offset adjusts by +2")
}

func TestPeepholeRecursive(t *testing.T) {
	t.Parallel()
	child := &program.CompiledFunction{
		IntConstants: []int64{1},
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 3, 0, 0),
			mk(isa.OpAddInt, 1, 2, 3),
		},
	}
	parent := &program.CompiledFunction{
		Functions: []*program.CompiledFunction{child},
		Body:      []isa.Instruction{mk(isa.OpNop, 0, 0, 0)},
	}
	_ = passes.Optimise(context.Background(), passes.DefaultOptions(), parent)

	assert.Equal(t, isa.OpAddIntConst, child.Body[0].Op)
	assert.Equal(t, isa.OpNop, child.Body[1].Op)
}
