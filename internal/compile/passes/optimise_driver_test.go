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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func bodyHasTier1SubOp(body []isa.Instruction, subOp isa.SubOpcode) bool {
	for _, instruction := range body {
		if isa.InstrIsTier1SubOp(instruction, subOp) {
			return true
		}
	}
	return false
}

func bodyHasOpcode(body []isa.Instruction, op isa.Opcode) bool {
	for _, instruction := range body {
		if instruction.Op == op {
			return true
		}
	}
	return false
}

func TestOptimisingAFunctionNarrowsItsSmallConstantLoads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		constants []int64
		wantSmall bool
	}{
		{name: "a constant the inline slot holds", constants: []int64{7}, wantSmall: true},
		{name: "a constant too large for the inline slot", constants: []int64{program.MaxSmallConstant + 1}, wantSmall: false},
		{name: "a negative constant", constants: []int64{-1}, wantSmall: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body: []isa.Instruction{
					mk(isa.OpLoadIntConst, 0, 0, 0),
					isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
				},
				IntConstants: tt.constants,
				NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
			}

			require.NoError(t, Optimise(context.Background(), DefaultOptions(), compiledFunction))

			require.Equal(t, tt.wantSmall, bodyHasTier1SubOp(compiledFunction.Body, isa.SubOpLoadIntConstSmall),
				"a constant small enough to travel inside the instruction should not cost a pool read")
			require.Equal(t, !tt.wantSmall, bodyHasOpcode(compiledFunction.Body, isa.OpLoadIntConst),
				"a constant that was narrowed leaves no pool load behind, and one that was not keeps it")
		})
	}
}

func TestOptimisingAFunctionFusesACompareWithItsBranch(t *testing.T) {
	t.Parallel()

	lo, hi := jumpOffset(1)
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLtInt, 3, 1, 2),
			mk(isa.OpJumpIfFalse, 3, lo, hi),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 0),
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 4},
	}

	require.NoError(t, Optimise(context.Background(), DefaultOptions(), compiledFunction))

	require.True(t, bodyHasTier1SubOp(compiledFunction.Body, isa.SubOpLtIntJumpFalse),
		"a comparison whose result only feeds the branch beside it becomes one compare-and-branch word")
	require.False(t, bodyHasOpcode(compiledFunction.Body, isa.OpLtInt),
		"the separate comparison has nothing left to compute")
}

func TestOptimisingAFunctionRecursesIntoItsNestedFunctions(t *testing.T) {
	t.Parallel()

	child := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		IntConstants: []int64{3},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
	}
	parent := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		IntConstants: []int64{4},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
		Functions:    []*program.CompiledFunction{child},
	}

	require.NoError(t, Optimise(context.Background(), DefaultOptions(), parent))

	require.True(t, bodyHasTier1SubOp(parent.Body, isa.SubOpLoadIntConstSmall))
	require.True(t, bodyHasTier1SubOp(child.Body, isa.SubOpLoadIntConstSmall),
		"a nested function is compiled as part of its parent, so it has to be optimised with it")
}

func TestOptimisingOneFunctionLeavesItsNestedFunctionsAlone(t *testing.T) {
	t.Parallel()

	child := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		IntConstants: []int64{3},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
	}
	parent := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		IntConstants: []int64{4},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
		Functions:    []*program.CompiledFunction{child},
	}

	require.NoError(t, OptimiseFunction(context.Background(), DefaultOptions(), parent))

	require.True(t, bodyHasTier1SubOp(parent.Body, isa.SubOpLoadIntConstSmall))
	require.True(t, bodyHasOpcode(child.Body, isa.OpLoadIntConst),
		"the single-function entry point is what the compiler calls as each body finishes, so it must not reach ahead")
}

func TestOptimisingWithEveryOptionalPassOffStillNarrowsConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options Options
	}{
		{name: "every optional pass on", options: DefaultOptions()},
		{name: "every optional pass off", options: Options{}},
		{name: "only bounds-check elimination", options: Options{BCE: true}},
		{name: "only common-subexpression elimination", options: Options{CSE: true}},
		{name: "only global value numbering", options: Options{GVN: true}},
		{name: "only loop-invariant code motion", options: Options{LICM: true}},
		{name: "only arena promotion", options: Options{ArenaPromotion: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{
				Body: []isa.Instruction{
					mk(isa.OpLoadIntConst, 0, 0, 0),
					isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
				},
				IntConstants: []int64{7},
				NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
			}

			require.NoError(t, Optimise(context.Background(), tt.options, compiledFunction))

			require.True(t, bodyHasTier1SubOp(compiledFunction.Body, isa.SubOpLoadIntConstSmall),
				"the peephole rewrites are not optional, so they run whichever analyses are switched on")
		})
	}
}

func TestOptimisingAFunctionStopsWhenTheCallerCancels(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpLoadIntConst, 0, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		IntConstants: []int64{7},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 2},
	}

	require.Error(t, Optimise(ctx, DefaultOptions(), compiledFunction),
		"optimisation runs inside the caller's compile, so a cancelled compile must not keep rewriting bodies")
}

func TestOptimisingAnEmptyBodyIsHarmless(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{}

	require.NoError(t, Optimise(context.Background(), DefaultOptions(), compiledFunction),
		"a declaration with no body has nothing to rewrite, and refusing it would fail a valid compile")
	require.Empty(t, compiledFunction.Body)
}
