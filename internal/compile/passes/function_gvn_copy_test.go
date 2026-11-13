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
	copySource      = 3
	copyDestination = 4
)

func copyFunction(words ...isa.Instruction) *program.CompiledFunction {
	compiledFunction := &program.CompiledFunction{Body: words}
	compiledFunction.NumRegisters[isa.RegisterInt64] = 16
	return compiledFunction
}

func TestPropagateGvnCopyRemovesAMoveItsReadsCanDoWithout(t *testing.T) {
	t.Parallel()
	move := isa.NewTier1Instruction(isa.SubOpMoveInt64, copyDestination, copySource)
	selfMove := isa.NewTier1Instruction(isa.SubOpMoveInt64, copySource, copySource)
	read := mk(isa.OpAddInt64, 5, copyDestination, copyDestination)
	readOfSource := mk(isa.OpAddInt64, 5, copySource, copySource)
	reload := mk(isa.OpLoadInt64Constant, copySource, 0, 0)

	tests := []struct {
		name        string
		body        []isa.Instruction
		want        []isa.Instruction
		resultSlots int
	}{
		{
			name: "the reads take the copied register, whatever writes it after the last of them",
			body: []isa.Instruction{move, read, reload, returnVoid()},
			want: []isa.Instruction{nop(), readOfSource, reload, returnVoid()},
		},
		{
			name: "a read that also writes the copied register reads it first",
			body: []isa.Instruction{move, mk(isa.OpAddInt64, copySource, copyDestination, copyDestination), returnVoid()},
			want: []isa.Instruction{nop(), mk(isa.OpAddInt64, copySource, copySource, copySource), returnVoid()},
		},
		{
			name:        "a move of a register onto itself goes, even from a result slot",
			body:        []isa.Instruction{selfMove, readOfSource, returnVoid()},
			want:        []isa.Instruction{nop(), readOfSource, returnVoid()},
			resultSlots: copySource + 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := copyFunction(slices.Clone(tt.body)...)
			compiledFunction.ResultKinds = slices.Repeat([]isa.RegisterKind{isa.RegisterInt64}, tt.resultSlots)

			require.True(t, propagateGvnCopy(compiledFunction, newFunctionAnalysis(compiledFunction), 0))

			require.Equal(t, tt.want, compiledFunction.Body)
		})
	}
}

func TestPropagateGvnCopyKeepsAMoveItsReadsCannotDoWithout(t *testing.T) {
	t.Parallel()
	move := isa.NewTier1Instruction(isa.SubOpMoveInt64, copyDestination, copySource)
	read := mk(isa.OpAddInt64, 5, copyDestination, copyDestination)
	reload := mk(isa.OpLoadInt64Constant, copySource, 0, 0)
	branch := mk(isa.OpJumpIfTrue, 9, 0, 0)

	tests := []struct {
		name        string
		body        []isa.Instruction
		targets     map[int]int
		movePC      int
		resultSlots int
	}{
		{name: "the copied register is written before a read", body: []isa.Instruction{move, reload, read, returnVoid()}},
		{
			name:    "the copied register is written on one way to a read",
			body:    []isa.Instruction{move, branch, reload, read, returnVoid()},
			targets: map[int]int{1: 3},
		},
		{
			name:    "a read is reached by another definition too",
			body:    []isa.Instruction{branch, move, branch, mk(isa.OpLoadInt64Constant, copyDestination, 0, 0), read, returnVoid()},
			targets: map[int]int{0: 3, 2: 4},
			movePC:  1,
		},
		{name: "the register is a result slot", body: []isa.Instruction{move, read, returnVoid()}, resultSlots: copyDestination + 1},
		{name: "a word that is not a move", body: []isa.Instruction{move, read, returnVoid()}, movePC: 1},
		{
			name: "the function makes a closure, which may hold the register in a cell",
			body: []isa.Instruction{move, read, mk(isa.OpMakeClosure, 0, 0, 0), returnVoid()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := copyFunction(withJumpTargets(t, slices.Clone(tt.body), tt.targets)...)
			compiledFunction.ResultKinds = slices.Repeat([]isa.RegisterKind{isa.RegisterInt64}, tt.resultSlots)
			original := slices.Clone(compiledFunction.Body)

			require.False(t, propagateGvnCopy(compiledFunction, newFunctionAnalysis(compiledFunction), tt.movePC))

			require.Equal(t, original, compiledFunction.Body, "a move that stays leaves every word as it was")
		})
	}
}
