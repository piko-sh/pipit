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

func returnIntProgram(value int64) *program.CompiledFunction {
	builder := newBytecodeBuilder()
	builder.addIntConst(value)
	builder.intRegisters(2).returnInt()
	builder.Emit(isa.OpLoadIntConst, 0, 0, 0)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	return builder.build()
}

func TestExecuteReturnsTheValueTheProgramLeavesInRegisterZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() *program.CompiledFunction
		want  any
		name  string
	}{
		{name: "an integer constant", build: func() *program.CompiledFunction { return returnIntProgram(42) }, want: 42},
		{name: "a negative integer constant", build: func() *program.CompiledFunction { return returnIntProgram(-42) }, want: -42},
		{name: "a zero constant", build: func() *program.CompiledFunction { return returnIntProgram(0) }, want: 0},
		{
			name: "a float constant",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.addFloatConst(2.5)
				builder.floatRegisters(2).returnFloat()
				builder.Emit(isa.OpLoadFloatConst, 0, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			want: 2.5,
		},
		{
			name: "a string constant",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.addStringConst("pipit")
				builder.stringRegisters(2).returnString()
				builder.Emit(isa.OpLoadStringConst, 0, 0, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			want: "pipit",
		},
		{
			name: "a computed sum",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.addIntConst(10)
				builder.addIntConst(32)
				builder.intRegisters(3).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, 1, 0)
				builder.Emit(isa.OpAddInt, 0, 1, 2)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
			want: 42,
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

func TestExecuteSurfacesARuntimeFaultAsAnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() *program.CompiledFunction
		name  string
	}{
		{
			name: "an integer division by zero",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.addIntConst(1)
				builder.addIntConst(0)
				builder.intRegisters(3).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, 1, 0)
				builder.Emit(isa.OpDivInt, 0, 1, 2)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
		},
		{
			name: "an integer remainder by zero",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.addIntConst(1)
				builder.addIntConst(0)
				builder.intRegisters(3).returnInt()
				builder.Emit(isa.OpLoadIntConst, 1, 0, 0)
				builder.Emit(isa.OpLoadIntConst, 2, 1, 0)
				builder.Emit(isa.OpRemInt, 0, 1, 2)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
		},
		{
			name: "a constant index past the pool",
			build: func() *program.CompiledFunction {
				builder := newBytecodeBuilder()
				builder.intRegisters(2).returnInt()
				builder.Emit(isa.OpLoadIntConst, 0, 9, 0)
				builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
				return builder.build()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := executeTestBytecode(t, tt.build())

			require.Error(t, err, "a runtime fault must reach the host as an error")
			require.Nil(t, result)
		})
	}
}

func TestExecuteRunsTheSameProgramRepeatedly(t *testing.T) {
	t.Parallel()

	compiledFunction := returnIntProgram(42)

	for range 5 {
		result, err := executeTestBytecode(t, compiledFunction)

		require.NoError(t, err)
		require.Equal(t, 42, result, "a compiled function must be re-runnable without residue")
	}
}

func TestExecuteLeavesTheVirtualMachineReusable(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	t.Cleanup(vm.ReleaseArena)

	first, err := vm.Execute(returnIntProgram(1))
	require.NoError(t, err)
	require.Equal(t, 1, first)

	second, err := vm.Execute(returnIntProgram(2))
	require.NoError(t, err)
	require.Equal(t, 2, second, "a virtual machine must run a second program after the first returns")
}

func TestExecuteUnwindsTheFrameStackAfterAFault(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	t.Cleanup(vm.ReleaseArena)

	builder := newBytecodeBuilder()
	builder.addIntConst(1)
	builder.addIntConst(0)
	builder.intRegisters(3).returnInt()
	builder.Emit(isa.OpLoadIntConst, 1, 0, 0)
	builder.Emit(isa.OpLoadIntConst, 2, 1, 0)
	builder.Emit(isa.OpDivInt, 0, 1, 2)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	_, err := vm.Execute(builder.build())
	require.Error(t, err)

	result, err := vm.Execute(returnIntProgram(7))

	require.NoError(t, err, "a fault must not leave the machine unusable")
	require.Equal(t, 7, result)
}
