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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestGvnRewritesIdenticalAddInt64(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpAddInt64, 3, 1, 2),
		mk(isa.OpAddInt64, 4, 1, 2),
		mk(isa.OpLoadInt64Constant, 3, 0, 0), mk(isa.OpAddInt64, 5, 4, 4), returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op,
		"second opAddInt64 rewritten as tier-1 MOVE_INT64")
	require.Equal(t, uint8(isa.SubOpMoveInt64), compiledFunction.Body[3].A)
	require.Equal(t, uint8(4), compiledFunction.Body[3].B)
	require.Equal(t, uint8(3), compiledFunction.Body[3].C)
	require.Equal(t, mk(isa.OpAddInt64, 5, 4, 4), compiledFunction.Body[5],
		"the first sum's register is set again before the read, so the read keeps the copy")
}

func TestGvnRewritesCommutativeAddInt64(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpAddInt64, 3, 1, 2),
		mk(isa.OpAddInt64, 4, 2, 1),
		mk(isa.OpLoadInt64Constant, 3, 0, 0), mk(isa.OpAddInt64, 5, 4, 4), returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op,
		"a+b and b+a have the same canonicalised key")
	require.Equal(t, uint8(isa.SubOpMoveInt64), compiledFunction.Body[3].A)
}

func TestGvnRefusesNonCommutativeReorder(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpSubtractInt64, 3, 1, 2),
		mk(isa.OpSubtractInt64, 4, 2, 1),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpSubtractInt64, compiledFunction.Body[3].Op,
		"a-b and b-a are different values; no rewrite")
}

func TestGvnRewritesAddFloat64(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadFloat64Constant, 1, 0, 0),
		mk(isa.OpLoadFloat64Constant, 2, 0, 0),
		mk(isa.OpAddFloat64, 3, 1, 2),
		mk(isa.OpAddFloat64, 4, 1, 2),
		mk(isa.OpLoadFloat64Constant, 3, 0, 0), mk(isa.OpAddFloat64, 5, 4, 4), returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op)
	require.Equal(t, uint8(isa.SubOpMoveFloat64), compiledFunction.Body[3].A)
}

func TestGvnRewritesAddInt64Constant(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpAddInt64Constant, 2, 1, 5),
		mk(isa.OpAddInt64Constant, 3, 1, 5),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[2].Op,
		"same operand + same constant index = same value")
}

func TestGvnRefusesDifferentConstants(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpAddInt64Constant, 2, 1, 5),
		mk(isa.OpAddInt64Constant, 3, 1, 6),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpAddInt64Constant, compiledFunction.Body[2].Op,
		"different const indices → different values")
}

func TestGvnRefusesAfterIntermediateWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpAddInt64, 3, 1, 2),
		mk(isa.OpLoadInt64Constant, 3, 0, 0),
		mk(isa.OpAddInt64, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpAddInt64, compiledFunction.Body[4].Op,
		"register 3 (candidate's dest) was overwritten; no rewrite")
}

func TestGvnRefusesAfterCall(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpAddInt64, 3, 1, 2),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		mk(isa.OpAddInt64, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Equal(t, isa.OpAddInt64, compiledFunction.Body[4].Op,
		"call clears value table; no rewrite")
}

func TestGvnRefusesEmptyFunction(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{}
	require.NoError(t, RunFunctionGvn(t.Context(), compiledFunction))
	require.Empty(t, compiledFunction.Body)
}

func TestGvnNamesTheValueOfARegisterItSawNoDefinitionOf(t *testing.T) {
	t.Parallel()
	const counter, first, second, element = 10, 14, 15, 2
	oneLess := func(destination uint8) isa.Instruction {
		return mk(isa.OpSubtractInt64Constant, destination, counter, 1)
	}
	read := func(index uint8) isa.Instruction { return mk(isa.OpGetSliceInt64Element, element, 0, index) }

	tests := []struct {
		name    string
		between []isa.Instruction
		want    isa.Instruction
		wantUse isa.Instruction
	}{
		{name: "nothing between the two", want: nop(), wantUse: read(first)},
		{name: "another register is written between", between: []isa.Instruction{mk(isa.OpAddInt64Constant, 3, 3, 1)}, want: nop(), wantUse: read(first)},
		{
			name:    "the operand is moved onto itself",
			between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveInt64, counter, counter)},
			want:    nop(), wantUse: read(first),
		},
		{
			name:    "the operand is written between",
			between: []isa.Instruction{mk(isa.OpAddInt64Constant, counter, counter, 1)},
			want:    oneLess(second), wantUse: read(second),
		},
		{
			name:    "the operand is written by an operation the pass does not model",
			between: []isa.Instruction{mk(isa.OpGetSliceInt64Element, counter, 0, 3)},
			want:    oneLess(second), wantUse: read(second),
		},
		{
			name:    "a call comes between",
			between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)},
			want:    oneLess(second), wantUse: read(second),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := []isa.Instruction{oneLess(first)}
			body = append(body, tt.between...)
			body = append(body, oneLess(second), read(second), returnVoid())
			function := &program.CompiledFunction{Body: body, CallSites: []program.CallSite{{}}}
			function.NumRegisters[isa.RegisterInt64] = 16
			function.NumRegisters[isa.RegisterSliceInt64] = 1

			require.NoError(t, RunFunctionGvn(t.Context(), function))

			require.Equal(t, tt.want, function.Body[len(body)-3])
			require.Equal(t, tt.wantUse, function.Body[len(body)-2])
		})
	}
}

func TestGvnFollowsAMoveToTheValueItCopied(t *testing.T) {
	t.Parallel()
	const original, copied = 4, 5
	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, original, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, copied, original),
		mk(isa.OpAddInt64Constant, 6, original, 1),
		mk(isa.OpAddInt64Constant, 7, copied, 1),
		mk(isa.OpAddInt64, 8, 7, 7),
		returnVoid(),
	}
	function := &program.CompiledFunction{Body: body}
	function.NumRegisters[isa.RegisterInt64] = 16

	require.NoError(t, RunFunctionGvn(t.Context(), function))

	require.Equal(t, nop(), function.Body[3], "a sum of the copy is the sum of the original")
	require.Equal(t, mk(isa.OpAddInt64, 8, 6, 6), function.Body[4])
}

func TestGvnReusesAValueOnlyWhereTheScanSawEverythingThatRan(t *testing.T) {
	t.Parallel()

	const useless = 9
	load := func(register uint8) isa.Instruction { return mk(isa.OpLoadInt64Constant, register, 0, 0) }
	addFive := func(destination, source uint8) isa.Instruction {
		return mk(isa.OpAddInt64Constant, destination, source, 5)
	}
	move := func(destination, source uint8) isa.Instruction {
		return isa.NewTier1Instruction(isa.SubOpMoveInt64, destination, source)
	}
	jump := isa.NewTier1Instruction(isa.SubOpJump, 0, 0)
	branch := mk(isa.OpJumpIfTrue, useless, 0, 0)

	tests := []struct {
		name      string
		body      []isa.Instruction
		targets   map[int]int
		use       int
		wantReuse bool
	}{
		{
			name: "two bodies appended after the function, with the operand set again between them",
			body: []isa.Instruction{
				load(1), jump,
				load(1), jump,
				returnVoid(),
				addFive(5, 1), jump,
				addFive(6, 1), jump,
			},
			targets: map[int]int{1: 5, 6: 2, 3: 7, 8: 4},
			use:     7,
		},
		{
			name: "a loop that changes the operand after the use",
			body: []isa.Instruction{
				load(1), addFive(2, 1),
				addFive(3, 1), mk(isa.OpAddInt64Constant, 1, 1, 6), branch,
				returnVoid(),
			},
			targets: map[int]int{4: 2},
			use:     2,
		},
		{
			name: "a branch over words that leave the operand alone",
			body: []isa.Instruction{
				load(1), addFive(2, 1),
				branch, load(7),
				addFive(3, 1),
				returnVoid(),
			},
			targets:   map[int]int{2: 4},
			use:       4,
			wantReuse: true,
		},
		{
			name: "a branch over words that set the operand again",
			body: []isa.Instruction{
				load(1), addFive(2, 1),
				branch, load(1),
				addFive(3, 1),
				returnVoid(),
			},
			targets: map[int]int{2: 4},
			use:     4,
		},
		{
			name: "a value computed after the branch that skips it",
			body: []isa.Instruction{
				load(1), branch,
				addFive(2, 1), jump,
				addFive(3, 1),
				returnVoid(),
			},
			targets: map[int]int{1: 4, 3: 4},
			use:     4,
		},
		{
			name: "a branch over a move that would make two operands the same value",
			body: []isa.Instruction{
				branch, move(1, 4),
				addFive(2, 1), addFive(3, 4),
				returnVoid(),
			},
			targets: map[int]int{0: 2},
			use:     3,
		},
		{
			name: "a branch over a move that gives the operand back an earlier value",
			body: []isa.Instruction{
				load(4), move(1, 4), addFive(2, 1),
				load(1), branch, move(1, 4),
				addFive(3, 1),
				returnVoid(),
			},
			targets: map[int]int{4: 6},
			use:     6,
		},
		{
			name: "a branch over a repeat that gives the operand an earlier value",
			body: []isa.Instruction{
				load(1), addFive(2, 1), addFive(6, 2),
				load(3), branch, addFive(3, 1),
				addFive(7, 3),
				returnVoid(),
			},
			targets: map[int]int{4: 6},
			use:     6,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function := &program.CompiledFunction{Body: withJumpTargets(t, tt.body, tt.targets)}
			original := function.Body[tt.use]

			require.NoError(t, RunFunctionGvn(t.Context(), function))

			if tt.wantReuse {
				require.Equal(t, nop(), function.Body[tt.use], "the earlier value is still the value, and nothing reads a copy of it")
				return
			}
			require.Equal(t, original, function.Body[tt.use], "the value is computed afresh")
		})
	}
}
