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

const (
	joinSource          = 3
	joinDestination     = 1
	joinFlag            = 9
	joinMoveClosingJump = 2
)

func joinMoveBody(t *testing.T, missLine, hitLine []isa.Instruction, tail ...isa.Instruction) (body []isa.Instruction, move int) {
	t.Helper()
	body = make([]isa.Instruction, 0, len(missLine)+len(hitLine)+len(tail)+4)
	body = append(body, mk(isa.OpJumpIfTrue, joinFlag, 0, 0))
	body = append(body, missLine...)
	jump := len(body)
	body = append(body, tier1Jump(0))
	hit := len(body)
	body = append(body, hitLine...)
	move = len(body)
	body = append(body, isa.NewTier1Instruction(isa.SubOpMoveUint64, joinDestination, joinSource))
	body = append(body, tail...)
	body = append(body, returnVoid())
	require.True(t, program.SetJumpTarget(body, 0, hit))
	require.True(t, program.SetJumpTarget(body, jump, move))
	return body, move
}

func joinMoveFunction(body []isa.Instruction) *program.CompiledFunction {
	compiledFunction := &program.CompiledFunction{Body: body}
	compiledFunction.NumRegisters[isa.RegisterInt64] = 16
	compiledFunction.NumRegisters[isa.RegisterUint64] = 16
	return compiledFunction
}

func TestCoalesceMovesRenamesEveryDefinitionThatReachesAMoveAtAJoin(t *testing.T) {
	t.Parallel()
	loadZero := func(register uint8) isa.Instruction {
		return isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, register, 0)
	}
	sum := func(register uint8) isa.Instruction { return mk(isa.OpAddUint64, register, 5, 6) }
	use := mk(isa.OpAddUint64, 7, joinDestination, joinDestination)

	body, move := joinMoveBody(t, []isa.Instruction{loadZero(joinSource)}, []isa.Instruction{sum(joinSource)}, use)
	compiledFunction := joinMoveFunction(body)

	require.True(t, coalesceMoves(compiledFunction, jumpTargetsFor(nil, body)))

	require.Equal(t, loadZero(joinDestination), compiledFunction.Body[1], "the line that jumps to the move writes the destination")
	require.Equal(t, sum(joinDestination), compiledFunction.Body[3], "the line that falls into the move writes the destination")
	require.Equal(t, nop(), compiledFunction.Body[move])
	require.Equal(t, use, compiledFunction.Body[move+1])
}

func TestCoalesceMovesKeepsAMoveAtAJoinItCannotAccountFor(t *testing.T) {
	t.Parallel()
	loadZero := isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, joinSource, 0)
	sum := mk(isa.OpAddUint64, joinSource, 5, 6)
	readsSource := mk(isa.OpAddUint64, 7, joinSource, joinSource)
	readsDestination := mk(isa.OpAddUint64, 7, joinDestination, 5)

	tests := []struct {
		name  string
		build func(t *testing.T) []isa.Instruction
	}{
		{
			name: "one way in does not define the source",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := joinMoveBody(t, []isa.Instruction{mk(isa.OpAddUint64, 8, 5, 6)}, []isa.Instruction{sum})
				return body
			},
		},
		{
			name: "the source is read after the move",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum}, readsSource)
				return body
			},
		},
		{
			name: "one line reads the destination before the move",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum, readsDestination})
				return body
			},
		},
		{
			name: "a conditional jump lands on the move",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, move := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum})
				body = append(body, loadZero, mk(isa.OpJumpIfTrue, joinFlag, 0, 0), returnVoid())
				require.True(t, program.SetJumpTarget(body, len(body)-2, move))
				return body
			},
		},
		{
			name: "a jump lands inside one of the lines",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, move := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum, mk(isa.OpAddUint64, 8, 5, 6)})
				body = append(body, tier1Jump(0))
				require.True(t, program.SetJumpTarget(body, len(body)-1, move-1))
				return body
			},
		},
		{
			name: "a conditional jump lands on the jump that closes a line",
			build: func(t *testing.T) []isa.Instruction {
				t.Helper()
				body, _ := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum})
				body = append(body, mk(isa.OpJumpIfTrue, joinFlag, 0, 0), returnVoid())
				require.True(t, program.SetJumpTarget(body, len(body)-2, joinMoveClosingJump))
				return body
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := tt.build(t)
			compiledFunction := joinMoveFunction(slices.Clone(body))

			coalesceMoves(compiledFunction, jumpTargetsFor(nil, body))

			require.Equal(t, body, compiledFunction.Body)
		})
	}
}

func TestCoalesceMovesRenamesTheDefinitionsOfTwoLinesThatJumpToAMove(t *testing.T) {
	t.Parallel()
	loadZero := func(register uint8) isa.Instruction {
		return isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, register, 0)
	}
	sum := func(register uint8) isa.Instruction { return mk(isa.OpAddUint64, register, 5, 6) }
	use := mk(isa.OpAddUint64, 7, joinDestination, joinDestination)
	const second, move = 3, 6
	body := []isa.Instruction{
		mk(isa.OpJumpIfTrue, joinFlag, 0, 0),
		loadZero(joinSource), tier1Jump(0),
		sum(joinSource), tier1Jump(0),
		returnVoid(),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, joinDestination, joinSource),
		use, returnVoid(),
	}
	require.True(t, program.SetJumpTarget(body, 0, second))
	require.True(t, program.SetJumpTarget(body, 2, move))
	require.True(t, program.SetJumpTarget(body, 4, move))
	compiledFunction := joinMoveFunction(body)

	require.True(t, coalesceMoves(compiledFunction, jumpTargetsFor(nil, body)))

	require.Equal(t, loadZero(joinDestination), compiledFunction.Body[1])
	require.Equal(t, sum(joinDestination), compiledFunction.Body[second])
	require.Equal(t, nop(), compiledFunction.Body[move], "the return before the move is no way into it")
}

func TestCoalesceMovesFollowsAJumpThatLandsOnTheJumpClosingAnotherLine(t *testing.T) {
	t.Parallel()
	loadZero := func(register uint8) isa.Instruction {
		return isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, register, 0)
	}
	sum := func(register uint8) isa.Instruction { return mk(isa.OpAddUint64, register, 5, 6) }
	product := func(register uint8) isa.Instruction { return mk(isa.OpMultiplyUint64, register, 5, 6) }
	use := mk(isa.OpAddUint64, 7, joinDestination, joinDestination)
	const innerOther, closing, other, move = 4, 5, 6, 7
	body := []isa.Instruction{
		mk(isa.OpJumpIfTrue, joinFlag, 0, 0),
		mk(isa.OpJumpIfTrue, joinFlag, 0, 0),
		loadZero(joinSource), tier1Jump(0),
		sum(joinSource),
		tier1Jump(0),
		product(joinSource),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, joinDestination, joinSource),
		use, returnVoid(),
	}
	require.True(t, program.SetJumpTarget(body, 0, other))
	require.True(t, program.SetJumpTarget(body, 1, innerOther))
	require.True(t, program.SetJumpTarget(body, 3, closing))
	require.True(t, program.SetJumpTarget(body, closing, move))
	compiledFunction := joinMoveFunction(body)

	require.True(t, coalesceMoves(compiledFunction, jumpTargetsFor(nil, body)))

	require.Equal(t, loadZero(joinDestination), compiledFunction.Body[2], "the line that jumps onto the closing jump")
	require.Equal(t, sum(joinDestination), compiledFunction.Body[innerOther], "the line that falls into the closing jump")
	require.Equal(t, product(joinDestination), compiledFunction.Body[other], "the line that falls into the move")
	require.Equal(t, nop(), compiledFunction.Body[move])
}

func TestCoalesceMovesKeepsAMoveAtAJoinWhoseSourceADebuggerShows(t *testing.T) {
	t.Parallel()
	loadZero := isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, joinSource, 0)
	sum := mk(isa.OpAddUint64, joinSource, 5, 6)
	forward, _ := joinMoveBody(t, []isa.Instruction{loadZero}, []isa.Instruction{sum})
	backward := []isa.Instruction{
		tier1Jump(0),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, joinDestination, joinSource),
		returnVoid(),
		sum, tier1Jump(0),
	}
	require.True(t, program.SetJumpTarget(backward, 0, 3))
	require.True(t, program.SetJumpTarget(backward, 4, 1))

	tests := []struct {
		name     string
		body     []isa.Instruction
		startPC  int
		endPC    int
		wantKept bool
	}{
		{name: "the variable is shown in a line that jumps forward to the move", body: forward, startPC: 1, endPC: 2, wantKept: true},
		{name: "the variable is shown only in a line that jumps back to the move", body: backward, startPC: 3, endPC: 4, wantKept: true},
		{name: "the variable is shown only after the lines and the move", body: backward, startPC: 5, endPC: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := joinMoveFunction(slices.Clone(tt.body))
			compiledFunction.DebugVarTable = &program.DebugVarTable{Entries: []program.DebugVarEntry{{
				Name: "shown", Location: program.VarLocation{Kind: isa.RegisterUint64, Register: joinSource}, StartPC: tt.startPC, EndPC: tt.endPC,
			}}}

			removed := coalesceMoves(compiledFunction, jumpTargetsFor(nil, tt.body))

			require.Equal(t, !tt.wantKept, removed)
			if tt.wantKept {
				require.Equal(t, tt.body, compiledFunction.Body)
			}
		})
	}
}

func TestFallsThroughIntoLooksPastPaddingAndExtensionWords(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpAddUint64, 1, 2, 3),
		isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, 3, 2), mk(isa.OpExtension, 10, 0, 0),
		nop(),
		mk(isa.OpAddUint64, 1, 2, 3),
		tier1Jump(0), nop(),
		mk(isa.OpAddUint64, 1, 2, 3),
		returnVoid(),
		mk(isa.OpAddUint64, 1, 2, 3),
	}

	require.False(t, fallsThroughInto(body, 0), "nothing comes before the first word")
	require.True(t, fallsThroughInto(body, 1))
	require.True(t, fallsThroughInto(body, 4), "an instruction, its extension word and padding fall through")
	require.False(t, fallsThroughInto(body, 7), "a jump does not, whatever padding follows it")
	require.False(t, fallsThroughInto(body, 9), "nor does a return")
}
