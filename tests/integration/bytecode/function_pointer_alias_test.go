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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestAliasAnalysisAllocsAreDistinct(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
			mk(isa.OpAllocIndirect, 1, 0, 0),
			mk(isa.OpMoveGeneral, 2, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.NotNil(t, compiledFunction.AliasInfo)
	require.False(t, compiledFunction.AliasInfo.MayAlias(2, 0, 1),
		"two distinct opAllocIndirect produce different alias classes")
}

func TestAliasAnalysisSameRegisterAlwaysAliases(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.True(t, compiledFunction.AliasInfo.MayAlias(0, 0, 0),
		"a register always aliases itself")
}

func TestAliasAnalysisWildOnCall(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
			mk(isa.OpAllocIndirect, 1, 0, 0),
			isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
			mk(isa.OpMoveGeneral, 2, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.True(t, compiledFunction.AliasInfo.MayAlias(3, 0, 1),
		"call wildens all general registers; everything may-aliases everything")
}

func TestAliasAnalysisMovePropagatesClass(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
			mk(isa.OpMoveGeneral, 1, 0, 0),
			mk(isa.OpAllocIndirect, 2, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.True(t, compiledFunction.AliasInfo.MayAlias(2, 0, 1),
		"reg 1 received reg 0 via move; same class; may-aliases")
	require.False(t, compiledFunction.AliasInfo.MayAlias(2, 0, 2),
		"reg 2 is a separate allocation; distinct class")
}

func TestAliasAnalysisParametersAreDistinct(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		ParameterKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral},
		Body: []isa.Instruction{
			mk(isa.OpMoveGeneral, 2, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.False(t, compiledFunction.AliasInfo.MayAlias(0, 0, 1),
		"two general-bank parameters seeded with distinct classes")
}

func TestAliasAnalysisMergeAtJumpTargetWidensToWild(t *testing.T) {
	t.Parallel()
	lo, hi := jumpOffset(2)
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
			mk(isa.OpJumpIfFalse, 0, lo, hi),
			mk(isa.OpAllocIndirect, 1, 0, 0),
			mk(isa.OpMoveGeneral, 0, 1, 0),
			mk(isa.OpMoveGeneral, 2, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.True(t, compiledFunction.AliasInfo.MayAlias(4, 0, 1),
		"reg 0 has different classes on the two paths reaching PC 4; merge widens to wild")
}

func TestAliasAnalysisEmptyFunctionHandled(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.Nil(t, compiledFunction.AliasInfo, "empty function does not allocate alias info")
}

func TestMayAliasNilInfoReturnsTrue(t *testing.T) {
	t.Parallel()
	var info *program.PointerAliasInfo
	require.True(t, info.MayAlias(0, 1, 2),
		"nil info: conservative may-alias")
}

func TestMayAliasOutOfRangePCReturnsTrue(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpAllocIndirect, 0, 0, 0),
		},
	}
	_ = passes.RunPointerAliasAnalysis(context.Background(), compiledFunction)
	require.True(t, compiledFunction.AliasInfo.MayAlias(999, 0, 1),
		"PC outside body: conservative may-alias")
}
