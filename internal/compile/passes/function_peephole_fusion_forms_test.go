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

func fuseAtStart(
	fuser func(*program.CompiledFunction, []isa.Instruction, int, int, map[int]bool) bool,
	compiledFunction *program.CompiledFunction,
) bool {
	body := compiledFunction.Body
	return fuser(compiledFunction, body, 0, len(body), BuildAllJumpTargets(body))
}

func TestFusingAnAdditionWithTheJumpThatFollowsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   []isa.Instruction
		want   bool
		offset int16
	}{
		{
			name:   "a forward jump",
			body:   []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3), tier1Jump(4), returnVoid()},
			want:   true,
			offset: 5,
		},
		{
			name:   "a backward jump closing a loop",
			body:   []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3), tier1Jump(-6), returnVoid()},
			want:   true,
			offset: -5,
		},
		{
			name: "an addition with no jump after it",
			body: []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3), returnVoid()},
			want: false,
		},
		{
			name: "an addition followed by something other than a jump",
			body: []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3), mk(isa.OpAddInt, 1, 2, 3), returnVoid()},
			want: false,
		},
		{
			name: "an instruction that is not the constant addition",
			body: []isa.Instruction{mk(isa.OpAddInt, 1, 2, 3), tier1Jump(4), returnVoid()},
			want: false,
		},
		{
			name: "an addition at the very end of the body",
			body: []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}
			before := append([]isa.Instruction(nil), tt.body...)

			got := FuseAddIntJump(compiledFunction, compiledFunction.Body, 0, len(compiledFunction.Body), map[int]bool{})

			require.Equal(t, tt.want, got)
			if !tt.want {
				require.Equal(t, before, compiledFunction.Body,
					"a window that does not match must be left exactly as it was")
				return
			}
			require.Equal(t, isa.OpAddIntJump, compiledFunction.Body[0].Op,
				"the pair becomes one instruction that adds and branches")
			require.Equal(t, isa.OpExt, compiledFunction.Body[1].Op,
				"the branch offset moves into an extension word")
			require.Equal(t, tt.offset, isa.JoinOffset(compiledFunction.Body[1].A, compiledFunction.Body[1].B),
				"the offset is measured from the word after the fused pair, so it gains one")
		})
	}
}

func TestFusingAJumpIntoAnAdditionIsRefusedWhenTheJumpIsATarget(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{mk(isa.OpAddIntConst, 1, 2, 3), tier1Jump(4), returnVoid()},
	}
	before := append([]isa.Instruction(nil), compiledFunction.Body...)

	got := FuseAddIntJump(compiledFunction, compiledFunction.Body, 0, len(compiledFunction.Body), map[int]bool{1: true})

	require.False(t, got,
		"something else branches to the jump, so it cannot be folded into the word before it")
	require.Equal(t, before, compiledFunction.Body)
}

func TestFusingARuneConversionIntoTheConcatenationThatConsumesIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want bool
	}{
		{
			name: "the concatenation reads the converted rune",
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpRuneToString, 4, 5), mk(isa.OpConcatString, 1, 2, 4), returnVoid()},
			want: true,
		},
		{
			name: "the concatenation reads a different register",
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpRuneToString, 4, 5), mk(isa.OpConcatString, 1, 2, 9), returnVoid()},
			want: false,
		},
		{
			name: "the conversion is followed by something else",
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpRuneToString, 4, 5), returnVoid()},
			want: false,
		},
		{
			name: "the first word is not a rune conversion",
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveString, 4, 5), mk(isa.OpConcatString, 1, 2, 4), returnVoid()},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}

			got := fuseAtStart(FuseConcatRune, compiledFunction)

			require.Equal(t, tt.want, got)
			if !tt.want {
				return
			}
			require.Equal(t, isa.OpConcatRuneString, compiledFunction.Body[0].Op,
				"the rune is concatenated in place, with no intermediate string register")
			require.Equal(t, uint8(1), compiledFunction.Body[0].A, "the destination is the concatenation's")
			require.Equal(t, uint8(2), compiledFunction.Body[0].B, "the left operand is the concatenation's")
			require.Equal(t, uint8(5), compiledFunction.Body[0].C, "the right operand is the rune the conversion read")
			require.Equal(t, isa.OpNop, compiledFunction.Body[1].Op,
				"the consumed word is left as a no-op rather than removed, so jump offsets still hold")
		})
	}
}

func TestFusingAStringIndexIntoTheConversionThatFollowsIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want bool
	}{
		{
			name: "the conversion reads the indexed byte",
			body: []isa.Instruction{mk(isa.OpStringIndex, 4, 2, 3), isa.NewTier1Instruction(isa.SubOpUintToInt, 7, 4), returnVoid()},
			want: true,
		},
		{
			name: "the conversion reads a different register",
			body: []isa.Instruction{mk(isa.OpStringIndex, 4, 2, 3), isa.NewTier1Instruction(isa.SubOpUintToInt, 7, 9), returnVoid()},
			want: false,
		},
		{
			name: "the index is followed by something else",
			body: []isa.Instruction{mk(isa.OpStringIndex, 4, 2, 3), returnVoid()},
			want: false,
		},
		{
			name: "the first word is not a string index",
			body: []isa.Instruction{mk(isa.OpSliceGetUint, 4, 2, 3), isa.NewTier1Instruction(isa.SubOpUintToInt, 7, 4), returnVoid()},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}

			got := fuseAtStart(FuseStringIndexToInt, compiledFunction)

			require.Equal(t, tt.want, got)
			if !tt.want {
				return
			}
			require.Equal(t, isa.OpStringIndexToInt, compiledFunction.Body[0].Op,
				"the byte lands in the integer bank directly, with no unsigned register in between")
			require.Equal(t, uint8(7), compiledFunction.Body[0].A)
			require.Equal(t, uint8(2), compiledFunction.Body[0].B)
			require.Equal(t, uint8(3), compiledFunction.Body[0].C)
			require.Equal(t, isa.OpNop, compiledFunction.Body[1].Op)
		})
	}
}

func TestFusingAnAppendWithTheMoveThatWritesItBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     []isa.Instruction
		want     bool
		wantDest uint8
	}{
		{
			name:     "a boxed append written back over its source",
			body:     []isa.Instruction{mk(isa.OpAppend, 6, 2, 3), mk(isa.OpMoveGeneral, 2, 6, 0), returnVoid()},
			want:     true,
			wantDest: 2,
		},
		{
			name: "a boxed append written somewhere else",
			body: []isa.Instruction{mk(isa.OpAppend, 6, 2, 3), mk(isa.OpMoveGeneral, 9, 6, 0), returnVoid()},
			want: false,
		},
		{
			name: "a boxed append whose result is not moved at all",
			body: []isa.Instruction{mk(isa.OpAppend, 6, 2, 3), returnVoid()},
			want: false,
		},
		{
			name: "a word that is not an append",
			body: []isa.Instruction{mk(isa.OpMoveGeneral, 6, 2, 0), mk(isa.OpMoveGeneral, 2, 6, 0), returnVoid()},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}

			got := fuseAtStart(FuseAppendMove, compiledFunction)

			require.Equal(t, tt.want, got)
			if !tt.want {
				return
			}
			require.Equal(t, tt.wantDest, compiledFunction.Body[0].A,
				"the append writes straight back into the slice it grew, which is what lets it grow in place")
			require.Equal(t, isa.OpNop, compiledFunction.Body[1].Op,
				"the write-back move has nothing left to do")
		})
	}
}

func TestNarrowingAConstantLoadToItsSmallForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		constants []int64
		body      isa.Instruction
		wantSmall bool
		wantValue uint8
	}{
		{name: "a small positive constant", constants: []int64{7}, body: mk(isa.OpLoadIntConst, 1, 0, 0), wantSmall: true, wantValue: 7},
		{name: "zero", constants: []int64{0}, body: mk(isa.OpLoadIntConst, 1, 0, 0), wantSmall: true, wantValue: 0},
		{name: "the largest value the small form holds", constants: []int64{program.MaxSmallConstant}, body: mk(isa.OpLoadIntConst, 1, 0, 0), wantSmall: true, wantValue: uint8(program.MaxSmallConstant)},
		{name: "one past the largest value", constants: []int64{program.MaxSmallConstant + 1}, body: mk(isa.OpLoadIntConst, 1, 0, 0), wantSmall: false},
		{name: "a negative constant", constants: []int64{-1}, body: mk(isa.OpLoadIntConst, 1, 0, 0), wantSmall: false},
		{name: "an index past the constant pool", constants: []int64{7}, body: mk(isa.OpLoadIntConst, 1, 9, 0), wantSmall: false},
		{name: "a wide index that uses the high byte", constants: []int64{7}, body: mk(isa.OpLoadIntConst, 1, 0, 1), wantSmall: false},
		{name: "an instruction that is not a constant load", constants: []int64{7}, body: isa.NewTier1Instruction(isa.SubOpMoveInt, 1, 0), wantSmall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{tt.body}, IntConstants: tt.constants}

			OptimiseLoadIntConst(compiledFunction, compiledFunction.Body, 0)

			if !tt.wantSmall {
				require.Equal(t, tt.body, compiledFunction.Body[0],
					"a constant the inline slot cannot hold keeps its pool lookup")
				return
			}
			require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[0], isa.SubOpLoadIntConstSmall),
				"a small constant is carried in the instruction itself, with no pool read at run time")
			require.Equal(t, tt.wantValue, compiledFunction.Body[0].C)
			require.Equal(t, uint8(1), compiledFunction.Body[0].B, "the destination register is preserved")
		})
	}
}

func TestNarrowingAnUnsignedConstantLoadToItsSmallForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		constants []uint64
		body      isa.Instruction
		wantSmall bool
		wantValue uint8
	}{
		{name: "a small constant", constants: []uint64{7}, body: mk(isa.OpLoadUintConst, 1, 0, 0), wantSmall: true, wantValue: 7},
		{name: "zero", constants: []uint64{0}, body: mk(isa.OpLoadUintConst, 1, 0, 0), wantSmall: true, wantValue: 0},
		{name: "the largest value the small form holds", constants: []uint64{uint64(program.MaxSmallConstant)}, body: mk(isa.OpLoadUintConst, 1, 0, 0), wantSmall: true, wantValue: uint8(program.MaxSmallConstant)},
		{name: "one past the largest value", constants: []uint64{uint64(program.MaxSmallConstant) + 1}, body: mk(isa.OpLoadUintConst, 1, 0, 0), wantSmall: false},
		{name: "an index past the constant pool", constants: []uint64{7}, body: mk(isa.OpLoadUintConst, 1, 9, 0), wantSmall: false},
		{name: "an instruction that is not a constant load", constants: []uint64{7}, body: isa.NewTier1Instruction(isa.SubOpMoveUint, 1, 0), wantSmall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{tt.body}, UintConstants: tt.constants}

			OptimiseLoadUintConst(compiledFunction, compiledFunction.Body, 0)

			if !tt.wantSmall {
				require.Equal(t, tt.body, compiledFunction.Body[0])
				return
			}
			require.True(t, isa.InstrIsTier1SubOp(compiledFunction.Body[0], isa.SubOpLoadUintConstSmall))
			require.Equal(t, tt.wantValue, compiledFunction.Body[0].C)
		})
	}
}

func TestSynchronisingTheSourceMapClearsThePositionsOfRetiredWords(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpMoveInt, 1, 2),
			mk(isa.OpNop, 0, 0, 0),
			mk(isa.OpAddInt, 1, 2, 3),
		},
		DebugSourceMap: &program.SourceMap{Positions: []program.SourcePosition{
			{Line: 10, Column: 1},
			{Line: 11, Column: 1},
			{Line: 12, Column: 1},
		}},
	}

	SyncSourceMapAfterOptimise(compiledFunction, compiledFunction.Body)

	require.Equal(t, 10, int(compiledFunction.DebugSourceMap.Positions[0].Line),
		"a word that survived optimisation keeps the line it came from")
	require.Zero(t, compiledFunction.DebugSourceMap.Positions[1].Line,
		"a retired word has no source line left to report, and claiming one would misplace a breakpoint")
	require.Equal(t, 12, int(compiledFunction.DebugSourceMap.Positions[2].Line))
}

func TestSynchronisingTheSourceMapOfAFunctionWithoutOneDoesNothing(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{mk(isa.OpNop, 0, 0, 0)}}

	require.NotPanics(t, func() { SyncSourceMapAfterOptimise(compiledFunction, compiledFunction.Body) },
		"a build with no debug information has no positions to clear")
}
