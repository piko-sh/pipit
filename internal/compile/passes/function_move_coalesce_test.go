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

func runCoalesce(t *testing.T, compiledFunction *program.CompiledFunction) {
	t.Helper()
	require.NoError(t, (coalesceMovesPass{}).Run(t.Context(), &PassContext{Analysis: newFunctionAnalysis(compiledFunction)}, compiledFunction))
}

func TestCoalesceRenamesDefinitionThroughInPlaceUpdates(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpBitAndUint64, 7, 5, 6),
		mk(isa.OpTruncateToWidth, 7, 32, uint8(isa.RegisterUint64)),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 3, 7),
		mk(isa.OpAddUint64, 4, 3, 3),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	runCoalesce(t, compiledFunction)
	require.Equal(t, []isa.Instruction{
		mk(isa.OpBitAndUint64, 3, 5, 6),
		mk(isa.OpTruncateToWidth, 3, 32, uint8(isa.RegisterUint64)),
		nop(),
		mk(isa.OpAddUint64, 4, 3, 3),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}, compiledFunction.Body)
}

func TestCoalesceRenamesDefinitionThatReadsTheDestination(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpMultiplyInt64, 7, 3, 5),
		mk(isa.OpAddInt64, 7, 7, 6),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	runCoalesce(t, compiledFunction)
	require.Equal(t, []isa.Instruction{
		mk(isa.OpMultiplyInt64, 3, 3, 5),
		mk(isa.OpAddInt64, 3, 3, 6),
		nop(),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}, compiledFunction.Body)
}

func TestCoalesceForwardsSourceIntoExtensionWord(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 1, 2),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 8), mk(isa.OpExtension, 1, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 1, 3),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 9), mk(isa.OpExtension, 1, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	runCoalesce(t, compiledFunction)
	require.Equal(t, []isa.Instruction{
		nop(),
		body[1], mk(isa.OpExtension, 2, 0, 0),
		nop(),
		body[4], mk(isa.OpExtension, 3, 0, 0),
		body[6],
	}, compiledFunction.Body)
}

func TestCoalesceRefusesUnsafeRewrites(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		body []isa.Instruction
		vars []program.DebugVarEntry
	}{
		"source read after move":        {body: []isa.Instruction{mk(isa.OpAddInt64, 7, 5, 6), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), mk(isa.OpAddInt64, 4, 7, 3), returnVoid()}},
		"destination read between":      {body: []isa.Instruction{mk(isa.OpAddInt64, 7, 5, 6), mk(isa.OpAddInt64, 4, 3, 3), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), returnVoid()}},
		"destination written between":   {body: []isa.Instruction{mk(isa.OpAddInt64, 7, 5, 6), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 3), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), returnVoid()}},
		"update reads destination":      {body: []isa.Instruction{mk(isa.OpAddInt64, 7, 5, 6), mk(isa.OpAddInt64, 7, 7, 3), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), returnVoid()}},
		"increment is not a definition": {body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 7), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), returnVoid()}},
		"named variable": {
			body: []isa.Instruction{mk(isa.OpAddInt64, 7, 5, 6), isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7), returnVoid()},
			vars: []program.DebugVarEntry{{Name: "v", Location: program.VarLocation{Kind: isa.RegisterInt64, Register: 7}, StartPC: 0, EndPC: 2}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: slices.Clone(tc.body)}
			if tc.vars != nil {
				compiledFunction.DebugVarTable = &program.DebugVarTable{Entries: tc.vars}
			}
			runCoalesce(t, compiledFunction)
			require.Equal(t, tc.body, compiledFunction.Body)
		})
	}
}

func TestCoalesceForwardsAcrossUnrelatedInstructions(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 1, 3),
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 9, 1),
		mk(isa.OpAddInt64, 10, 8, 9),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, 0, 10), mk(isa.OpExtension, 1, 0, 0),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 1, 255),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body)}
	runCoalesce(t, compiledFunction)
	want := slices.Clone(body)
	want[0] = nop()
	want[4] = mk(isa.OpExtension, 3, 0, 0)
	require.Equal(t, want, compiledFunction.Body)

	blocked := slices.Clone(body)
	blocked[1] = isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 3, 7)
	compiledFunction = &program.CompiledFunction{Body: slices.Clone(blocked)}
	runCoalesce(t, compiledFunction)
	require.Equal(t, blocked, compiledFunction.Body, "the source is overwritten before the store")
}

func TestCoalesceRenamesADefinitionThatOwnsAnExtensionWord(t *testing.T) {
	t.Parallel()
	masked := func(destination, source uint8) []isa.Instruction {
		return []isa.Instruction{isa.NewTier1Instruction(isa.SubOpBitAndUint64Constant, destination, source), mk(isa.OpExtension, 1, 0, 0)}
	}
	move := []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMoveUint64, 3, 7)}
	tail := []isa.Instruction{mk(isa.OpAddUint64, 4, 3, 3), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)}
	intoExtensionRegister := []isa.Instruction{
		mk(isa.OpMultiplyInt64ConstantThenAdd, 7, 5, 0), mk(isa.OpExtension, 6, 0, 0),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7),
		mk(isa.OpAddInt64, 4, 3, 3),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	ontoExtensionWord := slices.Concat([]isa.Instruction{tier1Jump(0)}, masked(7, 5), move, tail)
	require.True(t, program.SetJumpTarget(ontoExtensionWord, 0, 2))

	tests := []struct {
		name   string
		reason string
		body   []isa.Instruction
		want   []isa.Instruction
	}{
		{
			name:   "the definition itself",
			reason: "the constant index in the extension word names no register, so only the head word is renamed",
			body:   slices.Concat(masked(7, 5), move, tail),
			want:   slices.Concat(masked(3, 5), []isa.Instruction{nop()}, tail),
		},
		{
			name:   "an in-place update after the definition",
			reason: "the update is renamed with the definition it follows",
			body:   slices.Concat([]isa.Instruction{mk(isa.OpShiftRightUint64, 7, 5, 6)}, masked(7, 7), move, tail),
			want:   slices.Concat([]isa.Instruction{mk(isa.OpShiftRightUint64, 3, 5, 6)}, masked(3, 3), []isa.Instruction{nop()}, tail),
		},
		{
			name:   "an extension word that names a register of the bank is left alone",
			reason: "renaming the head word alone would leave the extension word naming the old register",
			body:   intoExtensionRegister,
			want:   intoExtensionRegister,
		},
		{
			name:   "a jump onto the extension word is left alone",
			reason: "a word a jump lands on is not part of a straight line",
			body:   ontoExtensionWord,
			want:   ontoExtensionWord,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: slices.Clone(tt.body), IntConstants: []int64{4}}

			runCoalesce(t, compiledFunction)

			require.Equal(t, tt.want, compiledFunction.Body, tt.reason)
		})
	}
}

func TestCoalesceLeavesMovesThatEstablishOrReadAResult(t *testing.T) {
	t.Parallel()
	returnOne := isa.NewTier2Instruction(isa.SubOpTier2Return, 1)
	results := []isa.RegisterKind{isa.RegisterUint64}
	placed := []isa.Instruction{
		mk(isa.OpMultiplyUint64, 2, 0, 4), mk(isa.OpTruncateToWidth, 2, 32, uint8(isa.RegisterUint64)),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 2), returnOne,
	}
	for name, function := range map[string]*program.CompiledFunction{
		"a value placed in the result slot of a function that defers": {
			Body: slices.Concat([]isa.Instruction{mk(isa.OpDefer, 0, 0, 0)}, placed),
		},
		"a named result placed in its result slot": {
			Body:                 placed,
			NamedResultLocations: []program.VarLocation{{Kind: isa.RegisterUint64, Register: 2}},
		},
		"a result slot copied to a temporary": {
			Body: []isa.Instruction{
				mk(isa.OpMultiplyUint64, 0, 3, 4), isa.NewTier1Instruction(isa.SubOpMoveUint64, 5, 0), mk(isa.OpAddUint64, 6, 5, 5), returnOne,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := slices.Clone(function.Body)
			function.ResultKinds = results
			runCoalesce(t, function)
			require.Equal(t, body, function.Body, "the move is what establishes or reads the result")
		})
	}
}

func TestCoalesceComputesIntoAResultSlotOnlyAReturnReads(t *testing.T) {
	t.Parallel()
	returnOne := isa.NewTier2Instruction(isa.SubOpTier2Return, 1)
	compiledFunction := &program.CompiledFunction{ResultKinds: []isa.RegisterKind{isa.RegisterUint64}, Body: []isa.Instruction{
		mk(isa.OpMultiplyUint64, 2, 0, 4), mk(isa.OpTruncateToWidth, 2, 32, uint8(isa.RegisterUint64)),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 2), returnOne,
	}}
	runCoalesce(t, compiledFunction)
	require.Equal(t, []isa.Instruction{
		mk(isa.OpMultiplyUint64, 0, 0, 4), mk(isa.OpTruncateToWidth, 0, 32, uint8(isa.RegisterUint64)), nop(), returnOne,
	}, compiledFunction.Body, "nothing is deferred, so the slot is seen only by the return and the product may be made in it")
}
