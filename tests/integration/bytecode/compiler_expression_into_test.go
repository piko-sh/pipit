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

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func countTier1SubOp(compiledFunction *program.CompiledFunction, sub isa.SubOpcode) int {
	count := 0
	for _, instr := range compiledFunction.Body {
		if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == sub {
			count++
		}
	}
	return count
}

func TestAssignBinaryOpEmitsNoMove(t *testing.T) {
	t.Parallel()
	cfs := compileFileSource(t, `package main

func run(a int, b int) int {
	x := 7
	for i := 0; i < 3; i++ {
		x = x*a - b
	}
	return x
}
`)
	run := program.ExportFunctions(cfs.Root())[cfs.Entrypoints()["run"]]
	subPC := findOpcode(run, isa.OpSubInt)
	require.GreaterOrEqual(t, subPC, 0, "expected the subtraction in the body:\n%s", run.Disassemble())
	mulPC := findOpcode(run, isa.OpMulInt)
	require.GreaterOrEqual(t, mulPC, 0)
	require.Equal(t, run.Body[subPC].A, run.Body[mulPC].B, "the subtraction writes x's register, the one the multiplication read x from")
	require.False(t, isa.InstrIsTier1SubOp(run.Body[subPC+1], isa.SubOpMoveInt), "no move follows the assignment's arithmetic:\n%s", run.Disassemble())
	require.Equal(t, 1, countTier1SubOp(run, isa.SubOpMoveInt), "only the return slot move remains:\n%s", run.Disassemble())
}

func TestReturnBinaryOpEmitsIntoReturnSlot(t *testing.T) {
	t.Parallel()
	cfs := compileFileSource(t, `package main

func run(a int, b int) int {
	return a*b + a
}
`)
	run := program.ExportFunctions(cfs.Root())[cfs.Entrypoints()["run"]]
	require.Zero(t, countTier1SubOp(run, isa.SubOpMoveInt), "the return must not move the arithmetic result:\n%s", run.Disassemble())
	addPC := findOpcode(run, isa.OpAddInt)
	require.GreaterOrEqual(t, addPC, 0, "expected the addition in the body:\n%s", run.Disassemble())
	require.Zero(t, run.Body[addPC].A, "the addition writes return slot zero directly")
}

func TestDestinationDrivenArithmeticKeepsOperandOrder(t *testing.T) {
	t.Parallel()
	cases := map[string]any{
		`x := 10; y := 3; x = y - x; x`:                           int64(-7),
		`x := 10; y := 3; x = x - y; x`:                           int64(7),
		`x := 10; y := 4; x = x / y; y = x % y; x + y`:            int64(2 + 2),
		`s := "a"; t := "b"; s = t + s; s`:                        "ba",
		`f := 1.5; g := 2.0; f = g * f; f`:                        float64(3),
		`var u uint = 9; var v uint = 4; u = u - v; int(u)`:       int64(5),
		`x := 2; x = (x * x) + (x - 1); x`:                        int64(5),
		`var b int8 = 100; var c int8 = 100; b = b + c; int(b)`:   int64(-56),
		`x := 1; f := func() int { x = x + 1; return x }; f(); x`: int64(2),
		`x := 2; y := x * x; x + y`:                               int64(6),
		`x := 2; z := (x * x) + (x - 1); z`:                       int64(5),
		`var x uint = 3; var y uint = x * x; int(x + y)`:          int64(12),
	}
	for source, expected := range cases {
		result, err := app.NewService().Eval(context.Background(), source)
		require.NoError(t, err, source)
		require.EqualValues(t, expected, result, source)
	}
}

func TestArithConstFusionKeepsLiveLoad(t *testing.T) {
	t.Parallel()
	compiledFunction := compileExpression(t, `x := 2; y := x * x; x + y`)
	requireContainsOpcode(t, compiledFunction, isa.OpMulInt)
	requireNoOpcode(t, compiledFunction, isa.OpMulIntConst)
	result, err := app.NewService().Eval(context.Background(), `x := 2; y := x * x; x + y`)
	require.NoError(t, err)
	require.EqualValues(t, 6, result)
}
