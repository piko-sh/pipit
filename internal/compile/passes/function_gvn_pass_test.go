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

func TestGvnRewritesIdenticalAddInt(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpAddInt, 3, 1, 2),
		mk(isa.OpAddInt, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op,
		"second opAddInt rewritten as tier-1 MOVE_INT")
	require.Equal(t, uint8(isa.SubOpMoveInt), compiledFunction.Body[3].A)
	require.Equal(t, uint8(4), compiledFunction.Body[3].B)
	require.Equal(t, uint8(3), compiledFunction.Body[3].C)
}

func TestGvnRewritesCommutativeAddInt(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpAddInt, 3, 1, 2),
		mk(isa.OpAddInt, 4, 2, 1),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op,
		"a+b and b+a have the same canonicalised key")
	require.Equal(t, uint8(isa.SubOpMoveInt), compiledFunction.Body[3].A)
}

func TestGvnRefusesNonCommutativeReorder(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpSubInt, 3, 1, 2),
		mk(isa.OpSubInt, 4, 2, 1),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpSubInt, compiledFunction.Body[3].Op,
		"a-b and b-a are different values; no rewrite")
}

func TestGvnRewritesAddFloat(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadFloatConst, 1, 0, 0),
		mk(isa.OpLoadFloatConst, 2, 0, 0),
		mk(isa.OpAddFloat, 3, 1, 2),
		mk(isa.OpAddFloat, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[3].Op)
	require.Equal(t, uint8(isa.SubOpMoveFloat), compiledFunction.Body[3].A)
}

func TestGvnRewritesAddIntConst(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpAddIntConst, 2, 1, 5),
		mk(isa.OpAddIntConst, 3, 1, 5),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpDrillTier1, compiledFunction.Body[2].Op,
		"same operand + same constant index = same value")
}

func TestGvnRefusesDifferentConstants(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpAddIntConst, 2, 1, 5),
		mk(isa.OpAddIntConst, 3, 1, 6),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpAddIntConst, compiledFunction.Body[2].Op,
		"different const indices → different values")
}

func TestGvnRefusesAfterIntermediateWrite(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpAddInt, 3, 1, 2),
		mk(isa.OpLoadIntConst, 3, 0, 0),
		mk(isa.OpAddInt, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpAddInt, compiledFunction.Body[4].Op,
		"register 3 (candidate's dest) was overwritten; no rewrite")
}

func TestGvnRefusesAfterCall(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpLoadIntConst, 1, 0, 0),
		mk(isa.OpLoadIntConst, 2, 0, 0),
		mk(isa.OpAddInt, 3, 1, 2),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		mk(isa.OpAddInt, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Equal(t, isa.OpAddInt, compiledFunction.Body[4].Op,
		"call clears value table; no rewrite")
}

func TestGvnRefusesEmptyFunction(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{}
	_ = RunFunctionGvn(context.Background(), compiledFunction)
	require.Empty(t, compiledFunction.Body)
}
