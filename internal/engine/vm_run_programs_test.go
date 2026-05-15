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

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func emitTier1Jump(builder *bytecodeBuilder, offset int16) {
	low, high := isa.SplitOffset(offset)
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpJump, low, high))
}

func countingLoop(limit int64) *program.CompiledFunction {
	const (
		total     = 0
		counter   = 1
		limitReg  = 2
		condition = 3
	)

	builder := newBytecodeBuilder()
	builder.addIntConst(0)
	builder.addIntConst(limit)
	builder.intRegisters(4).returnInt()

	builder.Emit(isa.OpLoadIntConst, total, 0, 0)
	builder.Emit(isa.OpLoadIntConst, counter, 0, 0)
	builder.Emit(isa.OpLoadIntConst, limitReg, 1, 0)

	loopTop := builder.CurrentPC()
	builder.Emit(isa.OpLtInt, condition, counter, limitReg)
	exitJump := builder.CurrentPC()
	builder.EmitJump(isa.OpJumpIfFalse, condition, 0)
	builder.Emit(isa.OpAddInt, total, total, counter)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), counter)
	emitTier1Jump(builder, int16(loopTop-builder.CurrentPC()-1))

	exitTarget := builder.CurrentPC()
	builder.body[exitJump] = isa.NewInstruction(isa.OpJumpIfFalse, condition, 0, 0)
	low, high := isa.SplitOffset(int16(exitTarget - exitJump - 1))
	builder.body[exitJump].B = low
	builder.body[exitJump].C = high

	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return builder.build()
}

func TestALoopingProgramRunsToCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit int64
		want  any
	}{
		{name: "a loop that never runs", limit: 0, want: 0},
		{name: "a single iteration", limit: 1, want: 0},
		{name: "a handful of iterations", limit: 5, want: 10},
		{name: "many iterations", limit: 100, want: 4950},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := executeTestBytecode(t, countingLoop(tt.limit))

			require.NoError(t, err)
			require.Equal(t, tt.want, result,
				"the dispatch loop must follow the backward jump until the condition fails")
		})
	}
}

func TestAForwardJumpSkipsTheBranchItGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		condition int64
		want      any
	}{
		{name: "a true condition takes the branch", condition: 1, want: 1},
		{name: "a false condition skips it", condition: 0, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			builder := newBytecodeBuilder()
			builder.addIntConst(tt.condition)
			builder.addIntConst(1)
			builder.addIntConst(2)
			builder.intRegisters(3).returnInt()

			builder.Emit(isa.OpLoadIntConst, 1, 0, 0)

			builder.EmitJump(isa.OpJumpIfFalse, 1, 2)
			builder.Emit(isa.OpLoadIntConst, 0, 1, 0)
			emitTier1Jump(builder, 1)
			builder.Emit(isa.OpLoadIntConst, 0, 2, 0)
			builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

			result, err := executeTestBytecode(t, builder.build())

			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestAProgramOfEveryScalarBankReturnsItsOwnKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() *program.CompiledFunction
		want  any
		name  string
	}{
		{
			name: "the integer bank",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addIntConst(42)
				b.intRegisters(1).returnInt()
				b.Emit(isa.OpLoadIntConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 42,
		},
		{
			name: "the float bank",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addFloatConst(2.5)
				b.floatRegisters(1).returnFloat()
				b.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: 2.5,
		},
		{
			name: "the string bank",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addStringConst("pipit")
				b.stringRegisters(1).returnString()
				b.Emit(isa.OpLoadStringConst, 0, 0, 0)
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: "pipit",
		},
		{
			name: "the boolean bank",
			build: func() *program.CompiledFunction {
				b := newBytecodeBuilder()
				b.addBoolConst(true)
				b.boolRegisters(1).returnBool()
				b.body = append(b.body, isa.NewTier1Instruction(isa.SubOpLoadBoolConst, 0, 0))
				b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return b.build()
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := executeTestBytecode(t, tt.build())

			require.NoError(t, err)
			require.Equal(t, tt.want, result)
		})
	}
}

func TestALongProgramExercisesTheDispatchLoopRepeatedly(t *testing.T) {
	t.Parallel()

	const steps = 200

	builder := newBytecodeBuilder()
	zero := builder.addIntConst(0)
	one := builder.addIntConst(1)
	builder.intRegisters(3).returnInt()

	builder.Emit(isa.OpLoadIntConst, 0, zero, 0)
	builder.Emit(isa.OpLoadIntConst, 1, one, 0)
	for range steps {
		builder.Emit(isa.OpAddInt, 0, 0, 1)
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	result, err := executeTestBytecode(t, builder.build())

	require.NoError(t, err)
	require.Equal(t, steps, result, "every instruction in a long body must run exactly once")
}
