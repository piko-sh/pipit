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

func doublingCallee() *program.CompiledFunction {
	builder := newBytecodeBuilder()
	builder.intRegisters(3).returnInt()
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterInt}
	builder.Emit(isa.OpAddInt, 0, 1, 1)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	callee := builder.build()
	callee.Name = "double"
	callee.ParameterRegisters = []uint8{1}
	return callee
}

func voidCallee() *program.CompiledFunction {
	builder := newBytecodeBuilder()
	builder.intRegisters(1)
	builder.body = append(builder.body, isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))

	callee := builder.build()
	callee.Name = "void"
	return callee
}

func callingProgram(callee *program.CompiledFunction, argument int64, arguments, returns []program.VarLocation) *program.CompiledFunction {
	builder := newBytecodeBuilder()
	builder.addIntConst(argument)
	builder.intRegisters(4).returnInt()

	calleeIndex := builder.addSubFunction(callee)
	siteIndex := builder.AddCallSite(&program.CallSite{
		FunctionIndex: calleeIndex,
		Arguments:     arguments,
		Returns:       returns,
	})

	builder.Emit(isa.OpLoadIntConst, 2, 0, 0)
	builder.body = append(builder.body, makeOpCallSlot(siteIndex))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	root := builder.build()
	root.Name = "root"
	return root
}

func intSlot(register uint8) program.VarLocation {
	return program.VarLocation{Kind: isa.RegisterInt, Register: register}
}

func TestCallingACompiledFunctionReturnsItsResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		argument int64
		want     any
	}{
		{name: "a positive argument", argument: 21, want: 42},
		{name: "a zero argument", argument: 0, want: 0},
		{name: "a negative argument", argument: -5, want: -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := callingProgram(doublingCallee(), tt.argument,
				[]program.VarLocation{intSlot(2)},
				[]program.VarLocation{intSlot(0)},
			)

			result, err := executeTestBytecode(t, root)

			require.NoError(t, err)
			require.Equal(t, tt.want, result,
				"the call must push a frame, copy the argument in, and copy the result back")
		})
	}
}

func TestCallingAVoidFunctionLeavesTheCallerIntact(t *testing.T) {
	t.Parallel()

	root := callingProgram(voidCallee(), 7, nil, nil)

	_, err := executeTestBytecode(t, root)

	require.NoError(t, err,
		"a call that returns nothing must still push and pop its frame cleanly")
}

func TestCallingTheSameSiteRepeatedlyReusesItsCache(t *testing.T) {
	t.Parallel()

	root := callingProgram(doublingCallee(), 21,
		[]program.VarLocation{intSlot(2)},
		[]program.VarLocation{intSlot(0)},
	)

	for range 5 {
		result, err := executeTestBytecode(t, root)

		require.NoError(t, err)
		require.Equal(t, 42, result,
			"a warm call site must keep returning the same answer, not drift as its caches fill")
	}
}

func TestCallSiteIndexPastTheTableIsRefused(t *testing.T) {
	t.Parallel()

	builder := newBytecodeBuilder()
	builder.intRegisters(2).returnInt()
	builder.body = append(builder.body, makeOpCallSlot(99))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	_, err := executeTestBytecode(t, builder.build())

	require.Error(t, err, "a call site index past the table must surface as an error")
}

func TestCallToAFunctionIndexPastTheTableIsRefused(t *testing.T) {
	t.Parallel()

	builder := newBytecodeBuilder()
	builder.intRegisters(2).returnInt()
	siteIndex := builder.AddCallSite(&program.CallSite{FunctionIndex: 99})
	builder.body = append(builder.body, makeOpCallSlot(siteIndex))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)

	_, err := executeTestBytecode(t, builder.build())

	require.Error(t, err, "a callee index past the function table must surface as an error")
}

func TestNestedCallsUnwindInOrder(t *testing.T) {
	t.Parallel()

	const outerIndex, innerIndex = 0, 1

	inner := doublingCallee()

	outerBuilder := newBytecodeBuilder()
	outerBuilder.intRegisters(4).returnInt()
	outerBuilder.parameterKinds = []isa.RegisterKind{isa.RegisterInt}
	innerSite := outerBuilder.AddCallSite(&program.CallSite{
		FunctionIndex: innerIndex,
		Arguments:     []program.VarLocation{intSlot(1)},
		Returns:       []program.VarLocation{intSlot(2)},
	})
	outerBuilder.body = append(outerBuilder.body, makeOpCallSlot(innerSite))
	outerBuilder.Emit(isa.OpAddInt, 0, 2, 2)
	outerBuilder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	outer := outerBuilder.build()
	outer.Name = "outer"
	outer.ParameterRegisters = []uint8{1}

	rootBuilder := newBytecodeBuilder()
	rootBuilder.addIntConst(5)
	rootBuilder.intRegisters(4).returnInt()
	rootBuilder.functions = []*program.CompiledFunction{outer, inner}
	outerSite := rootBuilder.AddCallSite(&program.CallSite{
		FunctionIndex: outerIndex,
		Arguments:     []program.VarLocation{intSlot(2)},
		Returns:       []program.VarLocation{intSlot(0)},
	})
	rootBuilder.Emit(isa.OpLoadIntConst, 2, 0, 0)
	rootBuilder.body = append(rootBuilder.body, makeOpCallSlot(outerSite))
	rootBuilder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
	root := rootBuilder.build()
	root.Name = "root"

	result, err := executeTestBytecode(t, root)

	require.NoError(t, err)
	require.Equal(t, 20, result,
		"inner doubles to ten and outer doubles again, so both frames must unwind in order")
}
