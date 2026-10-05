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

func nop() isa.Instruction { return mk(isa.OpNop, 0, 0, 0) }

func requireJumpTarget(t *testing.T, body []isa.Instruction, pc, want int) {
	t.Helper()
	target, ok := program.JumpTargetAt(body, pc)
	require.True(t, ok, "pc %d is not a jump", pc)
	require.Equal(t, want, target, "jump at pc %d", pc)
}

func TestCompactDeletesNopsAndRetargetsJumps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 0, 0, 0),
		nop(),
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		nop(),
		mk(isa.OpAddInt, 1, 1, 0),
		nop(),
		tier1Jump(0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 2, 5))
	require.True(t, program.SetJumpTarget(body, 6, 1))
	cf := &program.CompiledFunction{Body: body}
	require.True(t, compactBody(cf))
	require.Len(t, cf.Body, 5)
	require.Equal(t, isa.OpAddInt, cf.Body[2].Op)
	requireJumpTarget(t, cf.Body, 1, 3)
	requireJumpTarget(t, cf.Body, 3, 1)
}

func TestCompactDeletesJumpsToTheNextInstruction(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 0, 0, 0),
		tier1Jump(2),
		nop(),
		tier1Jump(0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	cf := &program.CompiledFunction{Body: body}
	require.True(t, compactBody(cf))
	require.Equal(t, []isa.Instruction{body[0], body[5]}, cf.Body)
}

func TestCompactKeepsJumpPaddingAndExtensionWords(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 7), fusedExt(0), nop(),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 1), mk(isa.OpExt, 0, 0, 0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, 6))
	cf := &program.CompiledFunction{Body: body}
	require.True(t, compactBody(cf))
	want := slices.Concat(body[:5], body[6:])
	require.True(t, program.SetJumpTarget(want, 0, 5))
	require.Equal(t, want, cf.Body)
}

func TestCompactLeavesTypeSwitchesAndCleanBodies(t *testing.T) {
	t.Parallel()
	for name, body := range map[string][]isa.Instruction{
		"type switch": {isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 5, 6), wordJumpRow(1), tier1Jump(0), nop(), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)},
		"clean":       {mk(isa.OpLoadIntConst, 0, 0, 0), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cf := &program.CompiledFunction{Body: slices.Clone(body)}
			require.False(t, compactBody(cf))
			require.Equal(t, body, cf.Body)
		})
	}
}

func TestCompactMovesMetadataWithSurvivingWords(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		nop(),
		nop(),
		mk(isa.OpAddInt, 1, 1, 0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, len(body)))
	files := []string{"main.go"}
	positions := []program.SourcePosition{{Line: 1}, {Line: 0}, {Line: 9}, {Line: 2}, {Line: 2}, {Line: 3}}
	cf := &program.CompiledFunction{
		Body:                       body,
		DebugSourceMap:             &program.SourceMap{Files: &files, Positions: positions},
		DebugVarTable:              &program.DebugVarTable{Entries: []program.DebugVarEntry{{Name: "x", StartPC: 3, EndPC: 5}, {Name: "y", StartPC: 1, EndPC: 0}}},
		ArenaSafeAllocPCs:          map[int]bool{3: true},
		GetMethodReceiverTypeNames: map[uint32]string{5: "T"},
		PeepholeProvenance:         map[int]program.PeepholeAnnotation{4: {Kind: peepholeRewriteCseTier0, Origin: 3}, 3: {Kind: peepholeRewriteCseTier0, Origin: 1}},
	}
	require.True(t, compactBody(cf))
	want := []isa.Instruction{body[0], body[2], body[3], body[5]}
	require.True(t, program.SetJumpTarget(want, 0, len(want)))
	require.Equal(t, want, cf.Body, "the NOP carrying line 9 alone survives")
	require.Equal(t, []program.SourcePosition{{Line: 1}, {Line: 9}, {Line: 2}, {Line: 3}}, cf.DebugSourceMap.Positions)
	require.Equal(t, []program.DebugVarEntry{{Name: "x", StartPC: 2, EndPC: 3}, {Name: "y", StartPC: 1, EndPC: 0}}, cf.DebugVarTable.Entries)
	require.Equal(t, map[int]bool{2: true}, cf.ArenaSafeAllocPCs)
	require.Equal(t, map[uint32]string{3: "T"}, cf.GetMethodReceiverTypeNames)
	require.Equal(t, map[int]program.PeepholeAnnotation{2: {Kind: peepholeRewriteCseTier0, Origin: 1}}, cf.PeepholeProvenance)
}

func TestCompactStraightensOutOfLineBlocks(t *testing.T) {
	t.Parallel()
	loopTail := mk(isa.OpAddInt, 1, 1, 0)
	inlined := []isa.Instruction{mk(isa.OpGetStructFieldIntT0, 2, 3, 0), mk(isa.OpJumpIfFalse, 2, 0, 0), mk(isa.OpAddInt, 2, 2, 2)}
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 0, 0, 0),
		tier1Jump(0),
		loopTail,
		tier1Jump(0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		inlined[0], inlined[1], inlined[2],
		tier1Jump(0),
	}
	require.True(t, program.SetJumpTarget(body, 1, 5))
	require.True(t, program.SetJumpTarget(body, 3, 1))
	require.True(t, program.SetJumpTarget(body, 6, 8))
	require.True(t, program.SetJumpTarget(body, 8, 2))
	cf := &program.CompiledFunction{Body: body}
	require.True(t, compactBody(cf))
	require.Len(t, cf.Body, 7)
	require.Equal(t, []isa.Opcode{isa.OpLoadIntConst, isa.OpGetStructFieldIntT0, isa.OpJumpIfFalse, isa.OpAddInt, isa.OpAddInt}, opcodes(cf.Body[:5]))
	requireJumpTarget(t, cf.Body, 2, 4)
	requireJumpTarget(t, cf.Body, 5, 1)
}

func TestCompactKeepsBlocksWithOtherEntries(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		tier1Jump(0),
		mk(isa.OpAddInt, 1, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		mk(isa.OpAddInt, 2, 2, 2),
		tier1Jump(0),
	}
	require.True(t, program.SetJumpTarget(body, 0, 4))
	require.True(t, program.SetJumpTarget(body, 1, 4))
	require.True(t, program.SetJumpTarget(body, 5, 2))
	cf := &program.CompiledFunction{Body: slices.Clone(body)}
	require.False(t, compactBody(cf))
	require.Equal(t, body, cf.Body)
}

func TestCompactThreadsJumpsThroughJumps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		mk(isa.OpAddInt, 1, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		tier1Jump(0),
		mk(isa.OpAddInt, 2, 2, 2),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, 3))
	require.True(t, program.SetJumpTarget(body, 3, 5))
	cf := &program.CompiledFunction{Body: body}
	require.True(t, compactBody(cf))
	requireJumpTarget(t, cf.Body, 0, 5)
}

func opcodes(body []isa.Instruction) []isa.Opcode {
	ops := make([]isa.Opcode, len(body))
	for i, inst := range body {
		ops[i] = inst.Op
	}
	return ops
}
