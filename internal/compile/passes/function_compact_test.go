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

func nop() isa.Instruction { return mk(isa.OpNoOperation, 0, 0, 0) }

func requireJumpTarget(t *testing.T, body []isa.Instruction, programCounter, want int) {
	t.Helper()
	target, ok := program.JumpTargetAt(body, programCounter)
	require.True(t, ok, "program counter %d is not a jump", programCounter)
	require.Equal(t, want, target, "jump at program counter %d", programCounter)
}

func TestCompactDeletesNopsAndRetargetsJumps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		nop(),
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		nop(),
		mk(isa.OpAddInt64, 1, 1, 0),
		nop(),
		tier1Jump(0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 2, 5))
	require.True(t, program.SetJumpTarget(body, 6, 1))
	compiledFunction := &program.CompiledFunction{Body: body}
	require.True(t, compacted(t, compiledFunction))
	require.Len(t, compiledFunction.Body, 5)
	require.Equal(t, isa.OpAddInt64, compiledFunction.Body[2].Op)
	requireJumpTarget(t, compiledFunction.Body, 1, 3)
	requireJumpTarget(t, compiledFunction.Body, 3, 1)
}

func TestCompactDeletesJumpsToTheNextInstruction(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		tier1Jump(2),
		nop(),
		tier1Jump(0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.True(t, compacted(t, compiledFunction))
	require.Equal(t, []isa.Instruction{body[0], body[5]}, compiledFunction.Body)
}

func TestCompactKeepsJumpPaddingAndExtensionWords(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, 0, 7), fusedExt(0), nop(),
		isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceByte, 0, 1), mk(isa.OpExtension, 0, 0, 0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, 6))
	compiledFunction := &program.CompiledFunction{Body: body}
	require.True(t, compacted(t, compiledFunction))
	want := slices.Concat(body[:5], body[6:])
	require.True(t, program.SetJumpTarget(want, 0, 5))
	require.Equal(t, want, compiledFunction.Body)
}

func typeSwitchHead() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 5, 6) }

func withJumpTargets(t *testing.T, body []isa.Instruction, targets map[int]int) []isa.Instruction {
	t.Helper()
	for programCounter, target := range targets {
		require.True(t, program.SetJumpTarget(body, programCounter, target), "program counter %d cannot reach %d", programCounter, target)
	}
	return body
}

func requireWholeTypeSwitchTables(t *testing.T, body []isa.Instruction) {
	t.Helper()
	for programCounter := range body {
		if !isa.InstrIsTier1SubOp(body[programCounter], isa.SubOpTypeSwitchJump) {
			continue
		}
		row := programCounter + 1
		for row < len(body) && body[row].Op == isa.OpTypeSwitchCase {
			target, ok := program.JumpTargetAt(body, row)
			require.True(t, ok)
			require.Greater(t, target, row, "the row at program counter %d must jump forward", row)
			row++
		}
		require.Less(t, row, len(body), "the table at program counter %d has no default jump", programCounter)
		require.True(t, isa.InstrIsTier1SubOp(body[row], isa.SubOpJump), "the rows at program counter %d must be followed by the default jump", programCounter)
	}
}

func TestCompactLeavesBodiesItCannotImprove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reason  string
		body    []isa.Instruction
		targets map[int]int
	}{
		{
			name:   "a body with nothing to delete",
			reason: "there is no dead word, no out-of-line block and no jump to thread",
			body:   []isa.Instruction{mk(isa.OpLoadInt64Constant, 0, 0, 0), returnVoid()},
		},
		{
			name:   "a block only a type switch's default jump enters",
			reason: "the default jump enters the out-of-line block, and replacing it would break the table",
			body: []isa.Instruction{
				typeSwitchHead(),
				wordJumpRow(0),
				tier1Jump(0),
				mk(isa.OpAddInt64, 1, 1, 0),
				returnVoid(),
				mk(isa.OpLoadInt64Constant, 0, 0, 0),
				tier1Jump(0),
			},
			targets: map[int]int{1: 3, 2: 5, 6: 3},
		},
		{
			name:   "table targets that lead to a backward jump",
			reason: "threading the row or the default through the backward jump would make it a backward edge",
			body: []isa.Instruction{
				mk(isa.OpAddInt64, 1, 1, 0),
				typeSwitchHead(),
				wordJumpRow(0),
				tier1Jump(0),
				tier1Jump(0),
				returnVoid(),
			},
			targets: map[int]int{2: 4, 3: 4, 4: 0},
		},
		{
			name:   "a type switch table with no default jump",
			reason: "a table that is not laid out as the runtime reads it is not touched, dead words included",
			body: []isa.Instruction{
				typeSwitchHead(),
				wordJumpRow(0),
				nop(),
				returnVoid(),
			},
			targets: map[int]int{1: 3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := withJumpTargets(t, tt.body, tt.targets)
			compiledFunction := &program.CompiledFunction{Body: slices.Clone(body)}

			require.False(t, compacted(t, compiledFunction), tt.reason)
			require.Equal(t, body, compiledFunction.Body)
		})
	}
}

func TestCompactDeletesDeadWordsAroundATypeSwitchTable(t *testing.T) {
	t.Parallel()
	body := withJumpTargets(t, []isa.Instruction{
		nop(),
		typeSwitchHead(),
		wordJumpRow(0),
		wordJumpRow(0),
		tier1Jump(0),
		nop(),
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		nop(),
		mk(isa.OpAddInt64, 1, 1, 0),
		nop(),
		returnVoid(),
	}, map[int]int{2: 8, 3: 9, 4: 5})
	compiledFunction := &program.CompiledFunction{Body: body}

	require.True(t, compacted(t, compiledFunction))

	require.Len(t, compiledFunction.Body, 7, "the four no-ops are gone and every table word stays")
	require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[0], isa.SubOpTypeSwitchJump))
	requireWholeTypeSwitchTables(t, compiledFunction.Body)
	requireJumpTarget(t, compiledFunction.Body, 1, 5)
	requireJumpTarget(t, compiledFunction.Body, 2, 6)
	requireJumpTarget(t, compiledFunction.Body, 3, 4)
	require.Equal(t, isa.OpLoadInt64Constant, compiledFunction.Body[4].Op, "the default jump now lands on the next word and is still kept")
}

func TestCompactMovesABlockHoldingAWholeTypeSwitchTable(t *testing.T) {
	t.Parallel()
	body := withJumpTargets(t, []isa.Instruction{
		tier1Jump(0),
		mk(isa.OpAddInt64, 1, 1, 0),
		returnVoid(),
		returnVoid(),
		typeSwitchHead(),
		wordJumpRow(0),
		tier1Jump(0),
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		tier1Jump(0),
	}, map[int]int{0: 4, 5: 7, 6: 1, 8: 1})
	compiledFunction := &program.CompiledFunction{Body: body}

	require.True(t, compacted(t, compiledFunction))

	require.Len(t, compiledFunction.Body, 7, "the entering jump and the block's closing jump are gone")
	require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[0], isa.SubOpTypeSwitchJump))
	requireWholeTypeSwitchTables(t, compiledFunction.Body)
	requireJumpTarget(t, compiledFunction.Body, 1, 3)
	requireJumpTarget(t, compiledFunction.Body, 2, 4)
	require.Equal(t, []isa.Opcode{isa.OpLoadInt64Constant, isa.OpAddInt64}, opcodes(compiledFunction.Body[3:5]),
		"the default jump is not taken for the block's closing jump, so the block runs on into the code after it")
}

func TestCompactKeepsABlockAJumpEntersThroughADeadWord(t *testing.T) {
	t.Parallel()
	body := withJumpTargets(t, []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		tier1Jump(0),
		mk(isa.OpAddInt64, 1, 1, 0),
		returnVoid(),
		returnVoid(),
		nop(),
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
		tier1Jump(0),
	}, map[int]int{0: 5, 1: 6, 7: 2})
	compiledFunction := &program.CompiledFunction{Body: body}

	require.True(t, compacted(t, compiledFunction))

	target, ok := program.JumpTargetAt(compiledFunction.Body, 0)
	require.True(t, ok)
	require.Equal(t, isa.OpLoadInt64Constant, compiledFunction.Body[target].Op,
		"the branch reached the block by falling through the no-op, so it must still reach the block")
}

func TestCompactKeepsALineOnlyWordOnlyInADebugBuild(t *testing.T) {
	t.Parallel()
	files := []string{"main.go"}
	build := func(variables *program.DebugVarTable) *program.CompiledFunction {
		return &program.CompiledFunction{
			Body:           []isa.Instruction{mk(isa.OpLoadInt64Constant, 0, 0, 0), nop(), returnVoid()},
			DebugSourceMap: &program.SourceMap{Files: &files, Positions: []program.SourcePosition{{Line: 1}, {Line: 2}, {Line: 3}}},
			DebugVarTable:  variables,
		}
	}

	plain := build(nil)
	require.True(t, compacted(t, plain))
	require.Len(t, plain.Body, 2, "outside a debug build the line simply has no word")
	require.Equal(t, []program.SourcePosition{{Line: 1}, {Line: 3}}, plain.DebugSourceMap.Positions)

	debugged := build(&program.DebugVarTable{Entries: nil})
	require.False(t, compacted(t, debugged), "a breakpoint on line 2 needs the word in a debug build")
	require.Len(t, debugged.Body, 3)
}

func TestCompactMovesMetadataWithSurvivingWords(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		nop(),
		nop(),
		mk(isa.OpAddInt64, 1, 1, 0),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, len(body)))
	files := []string{"main.go"}
	positions := []program.SourcePosition{{Line: 1}, {Line: 0}, {Line: 9}, {Line: 2}, {Line: 2}, {Line: 3}}
	compiledFunction := &program.CompiledFunction{
		Body:                              body,
		DebugSourceMap:                    &program.SourceMap{Files: &files, Positions: positions},
		DebugVarTable:                     &program.DebugVarTable{Entries: []program.DebugVarEntry{{Name: "x", StartPC: 3, EndPC: 5}, {Name: "y", StartPC: 1, EndPC: 0}}},
		ArenaSafeAllocPCs:                 map[int]bool{3: true},
		BindMethodByNameReceiverTypeNames: map[uint32]string{5: "T"},
		PeepholeProvenance:                map[int]program.PeepholeAnnotation{4: {Kind: peepholeRewriteCseTier0, Origin: 3}, 3: {Kind: peepholeRewriteCseTier0, Origin: 1}},
	}
	require.True(t, compacted(t, compiledFunction))
	want := []isa.Instruction{body[0], body[2], body[3], body[5]}
	require.True(t, program.SetJumpTarget(want, 0, len(want)))
	require.Equal(t, want, compiledFunction.Body, "the NO_OPERATION carrying line 9 alone survives")
	require.Equal(t, []program.SourcePosition{{Line: 1}, {Line: 9}, {Line: 2}, {Line: 3}}, compiledFunction.DebugSourceMap.Positions)
	require.Equal(t, []program.DebugVarEntry{{Name: "x", StartPC: 2, EndPC: 3}, {Name: "y", StartPC: 1, EndPC: 0}}, compiledFunction.DebugVarTable.Entries)
	require.Equal(t, map[int]bool{2: true}, compiledFunction.ArenaSafeAllocPCs)
	require.Equal(t, map[uint32]string{3: "T"}, compiledFunction.BindMethodByNameReceiverTypeNames)
	require.Equal(t, map[int]program.PeepholeAnnotation{2: {Kind: peepholeRewriteCseTier0, Origin: 1}}, compiledFunction.PeepholeProvenance)
}

func TestCompactStraightensOutOfLineBlocks(t *testing.T) {
	t.Parallel()
	loopTail := mk(isa.OpAddInt64, 1, 1, 0)
	inlined := []isa.Instruction{mk(isa.OpGetStructFieldInt64, 2, 3, 0), mk(isa.OpJumpIfFalse, 2, 0, 0), mk(isa.OpAddInt64, 2, 2, 2)}
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 0, 0, 0),
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
	compiledFunction := &program.CompiledFunction{Body: body}
	require.True(t, compacted(t, compiledFunction))
	require.Len(t, compiledFunction.Body, 7)
	require.Equal(t, []isa.Opcode{isa.OpLoadInt64Constant, isa.OpGetStructFieldInt64, isa.OpJumpIfFalse, isa.OpAddInt64, isa.OpAddInt64}, opcodes(compiledFunction.Body[:5]))
	requireJumpTarget(t, compiledFunction.Body, 2, 4)
	requireJumpTarget(t, compiledFunction.Body, 5, 1)
}

func TestCompactKeepsBlocksWithOtherEntries(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		tier1Jump(0),
		mk(isa.OpAddInt64, 1, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		mk(isa.OpAddInt64, 2, 2, 2),
		tier1Jump(0),
	}
	require.True(t, program.SetJumpTarget(body, 0, 4))
	require.True(t, program.SetJumpTarget(body, 1, 4))
	require.True(t, program.SetJumpTarget(body, 5, 2))
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body)}
	require.False(t, compacted(t, compiledFunction))
	require.Equal(t, body, compiledFunction.Body)
}

func TestCompactThreadsJumpsThroughJumps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpJumpIfFalse, 0, 0, 0),
		mk(isa.OpAddInt64, 1, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		tier1Jump(0),
		mk(isa.OpAddInt64, 2, 2, 2),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 0, 3))
	require.True(t, program.SetJumpTarget(body, 3, 5))
	compiledFunction := &program.CompiledFunction{Body: body}
	require.True(t, compacted(t, compiledFunction))
	requireJumpTarget(t, compiledFunction.Body, 0, 5)
}

func opcodes(body []isa.Instruction) []isa.Opcode {
	ops := make([]isa.Opcode, len(body))
	for i, inst := range body {
		ops[i] = inst.Op
	}
	return ops
}

func TestThreadBranchesOnOneRegisterSkipsTheSecondTestOfAShortCircuit(t *testing.T) {
	t.Parallel()
	const condition = 13
	body := []isa.Instruction{
		mk(isa.OpGreaterOrEqualInt64, condition, 10, 11),
		condJump(isa.OpJumpIfFalse, condition, 1),
		mk(isa.OpLessThanInt64, condition, 10, 16),
		condJump(isa.OpJumpIfFalse, condition, 1),
		returnVoid(),
		mk(isa.OpAddInt64, 1, 1, 1),
		returnVoid(),
	}
	require.True(t, threadBranchesOnOneRegister(body))
	target, ok := program.JumpTargetAt(body, 1)
	require.True(t, ok)
	require.Equal(t, 5, target, "a false first operand makes the whole condition false, so it takes the second branch's target")

	body = []isa.Instruction{
		mk(isa.OpGreaterOrEqualInt64, condition, 10, 11),
		condJump(isa.OpJumpIfTrue, condition, 1),
		mk(isa.OpLessThanInt64, condition, 10, 16),
		condJump(isa.OpJumpIfFalse, condition, 1),
		returnVoid(),
		mk(isa.OpAddInt64, 1, 1, 1),
		returnVoid(),
	}
	require.True(t, threadBranchesOnOneRegister(body))
	target, ok = program.JumpTargetAt(body, 1)
	require.True(t, ok)
	require.Equal(t, 4, target, "a true first operand of `||` falls through the branch that leaves on false")

	body = []isa.Instruction{
		condJump(isa.OpJumpIfFalse, condition, 1),
		returnVoid(),
		condJump(isa.OpJumpIfFalse, condition+1, 1),
		returnVoid(),
		returnVoid(),
	}
	original := slices.Clone(body)
	require.False(t, threadBranchesOnOneRegister(body), "a branch on another register tests something else")
	require.Equal(t, original, body)
}

func compacted(t *testing.T, compiledFunction *program.CompiledFunction) bool {
	t.Helper()
	changed, err := compactBody(t.Context(), compiledFunction)
	require.NoError(t, err)
	return changed
}
