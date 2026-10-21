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
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func truncate(reg uint8, kind isa.RegisterKind) isa.Instruction {
	return mk(isa.OpTruncateToWidth, reg, 32, uint8(kind))
}

func rangedRegisters() [isa.NumRegisterKinds]uint32 {
	var counts [isa.NumRegisterKinds]uint32
	counts[isa.RegisterInt64], counts[isa.RegisterUint64] = 16, 16
	return counts
}

func TestRangedTruncateElisionRemovesProvenNoOps(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 8, 127),
		mk(isa.OpBitAndInt64, 9, 7, 8),
		truncate(9, isa.RegisterInt64),
		truncate(7, isa.RegisterInt64),
		mk(isa.OpShiftRightInt64, 10, 7, 8),
		truncate(10, isa.RegisterInt64),
		mk(isa.OpAddInt64, 11, 7, 7),
		truncate(11, isa.RegisterInt64),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), NumRegisters: rangedRegisters()}
	require.True(t, elideRanged(t, compiledFunction))
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[2]), "a value masked to 127 fits")
	require.Equal(t, body[3], compiledFunction.Body[3], "an unknown value needs its truncation")
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[5]), "a right shift of a truncated value fits")
	require.Equal(t, body[7], compiledFunction.Body[7], "the sum of two int32 values may not fit")
}

func TestRangedTruncateElisionFollowsLoops(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		truncate(0, isa.RegisterUint64),
		mk(isa.OpLoadInt64Constant, 9, 0, 0),
		mk(isa.OpJumpIfFalse, 9, 0, 0),
		mk(isa.OpShiftRightUint64, 5, 0, 4),
		truncate(5, isa.RegisterUint64),
		mk(isa.OpAddUint64, 6, 0, 1),
		truncate(6, isa.RegisterUint64),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 6),
		tier1Jump(0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 2, 9))
	require.True(t, program.SetJumpTarget(body, 8, 1))
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), IntConstants: []int64{1}, NumRegisters: rangedRegisters()}
	require.True(t, elideRanged(t, compiledFunction))
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[4]), "position stays 32-bit around the loop")
	require.Equal(t, body[6], compiledFunction.Body[6], "the addition can carry past 32 bits")
}

func TestRangedTruncateElisionForgetsAcrossCalls(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 8, 127),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		truncate(8, isa.RegisterInt64),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), NumRegisters: rangedRegisters()}
	require.False(t, elideRanged(t, compiledFunction))
	require.Equal(t, body, compiledFunction.Body)
}

func TestRangedTruncateElisionUsesShiftCounts(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		truncate(0, isa.RegisterUint64),
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 3, uint8(isa.RegisterGeneral)), mk(isa.OpExtension, 1, 1, 0),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 12, 26),
		mk(isa.OpShiftRightUint64, 2, 0, 12),
		isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, 1, 2),
		truncate(1, isa.RegisterInt64),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), NumRegisters: rangedRegisters()}
	require.True(t, elideRanged(t, compiledFunction))
	require.True(t, isCanonicalNoOperation(compiledFunction.Body[6]), "a 32-bit value shifted right by 26 fits int32, across a wide global load")
}

func TestRangedTruncateElisionLeavesUncountedRegistersUnknown(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 8, 127),
		truncate(8, isa.RegisterInt64),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	counted := &program.CompiledFunction{Body: slices.Clone(body), NumRegisters: rangedRegisters()}
	require.True(t, elideRanged(t, counted), "a register within the function's count has its range tracked")
	uncounted := &program.CompiledFunction{Body: slices.Clone(body)}
	require.False(t, elideRanged(t, uncounted), "a register past the count is never taken to fit")
	require.Equal(t, body, uncounted.Body)
}

func elideRanged(t *testing.T, compiledFunction *program.CompiledFunction) bool {
	t.Helper()
	removed, err := elideRangedTruncates(t.Context(), compiledFunction)
	require.NoError(t, err)
	return removed
}

func TestElideRangedTruncatesStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpBitAndInt64, 9, 7, 7),
			truncate(9, isa.RegisterInt64),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
		NumRegisters: rangedRegisters(),
	}
	removed, err := elideRangedTruncates(ctx, compiledFunction)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, removed)
}

func TestRangedTruncateElisionModelsUnsignedArithmetic(t *testing.T) {
	t.Parallel()
	const value, small, result = 3, 4, 5
	loadSmall := isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, small, 26)
	withConstant := func(subOp isa.SubOpcode) []isa.Instruction {
		return []isa.Instruction{isa.NewTier1Instruction(subOp, result, value), mk(isa.OpExtension, 0, 0, 0)}
	}

	tests := []struct {
		name      string
		operation []isa.Instruction
		constants []uint64
		fits      bool
	}{
		{name: "a byte of a string fits", operation: []isa.Instruction{mk(isa.OpGetStringByte, result, 0, 0)}, fits: true},
		{name: "a remainder by a small divisor fits", operation: []isa.Instruction{loadSmall, mk(isa.OpRemainderUint64, result, 6, small)}, fits: true},
		{name: "a remainder of a 32-bit value fits", operation: []isa.Instruction{mk(isa.OpRemainderUint64, result, value, 6)}, fits: true},
		{name: "a remainder of two unknown values may not fit", operation: []isa.Instruction{mk(isa.OpRemainderUint64, result, 6, 7)}, fits: false},
		{name: "a product of two small values fits", operation: []isa.Instruction{loadSmall, mk(isa.OpMultiplyUint64, result, small, small)}, fits: true},
		{name: "a product of 32-bit values may not fit", operation: []isa.Instruction{mk(isa.OpMultiplyUint64, result, value, value)}, fits: false},
		{name: "a difference that cannot wrap fits", operation: []isa.Instruction{
			loadSmall, isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 6, 9), mk(isa.OpSubtractUint64, result, small, 6)}, fits: true},
		{name: "a difference that may wrap may not fit", operation: []isa.Instruction{loadSmall, mk(isa.OpSubtractUint64, result, small, value)}, fits: false},
		{name: "a small constant added to a small value fits", operation: slices.Concat(
			[]isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, value, 26)}, withConstant(isa.SubOpAddUint64Constant)),
			constants: []uint64{1000}, fits: true},
		{name: "a constant added to a 32-bit value may not fit", operation: withConstant(isa.SubOpAddUint64Constant), constants: []uint64{1}, fits: false},
		{name: "a constant subtracted from a value known to exceed it fits", operation: slices.Concat(
			[]isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, value, 26)}, withConstant(isa.SubOpSubtractUint64Constant)),
			constants: []uint64{7}, fits: true},
		{name: "a constant subtracted from a 32-bit value may wrap", operation: withConstant(isa.SubOpSubtractUint64Constant), constants: []uint64{7}, fits: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := slices.Concat([]isa.Instruction{truncate(value, isa.RegisterUint64)}, tt.operation,
				[]isa.Instruction{truncate(result, isa.RegisterUint64), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)})
			checked := len(body) - 2
			compiledFunction := &program.CompiledFunction{Body: slices.Clone(body), NumRegisters: rangedRegisters(), UintConstants: tt.constants}

			elideRanged(t, compiledFunction)

			require.Equal(t, tt.fits, isCanonicalNoOperation(compiledFunction.Body[checked]))
		})
	}
}
