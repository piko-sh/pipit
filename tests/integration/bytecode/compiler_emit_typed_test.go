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

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestCoerceForOperandPassesThroughOnMatch(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	bodyLenBefore := len(c.Function.Body)
	loc := program.VarLocation{Register: 5, Kind: isa.RegisterInt}

	out := c.CoerceForOperand(context.Background(), isa.RoleRegInt, true, loc)
	require.Equal(t, loc, out, "matching kind should pass through unchanged")
	require.Equal(t, bodyLenBefore, len(c.Function.Body),
		"no instructions should be emitted on match")
}

func TestCoerceForOperandNonReadDoesNothing(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	bodyLenBefore := len(c.Function.Body)
	loc := program.VarLocation{Register: 3, Kind: isa.RegisterGeneral}

	out := c.CoerceForOperand(context.Background(), isa.RoleRegInt, false, loc)
	require.Equal(t, loc, out, "non-read operands pass through")
	require.Equal(t, bodyLenBefore, len(c.Function.Body))
}

func TestCoerceForOperandInsertsUnpackForGeneralToInt(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	source := program.VarLocation{Register: 7, Kind: isa.RegisterGeneral}

	out := c.CoerceForOperand(context.Background(), isa.RoleRegInt, true, source)
	require.Equal(t, isa.RegisterInt, out.Kind, "result should be in int bank")
	require.NotEmpty(t, c.Function.Body, "an instruction should have been emitted")
	last := c.Function.Body[len(c.Function.Body)-1]
	require.True(t, isa.InstrIsTier1SubOp(last, isa.SubOpMoveGeneralToInt),
		"a general->int move should have been inserted; got %v", last)
}

func TestEmitTypedFunnelInjectsCoercion(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	destination := program.VarLocation{Register: 0, Kind: isa.RegisterInt}
	srcA := program.VarLocation{Register: 4, Kind: isa.RegisterGeneral}
	srcB := program.VarLocation{Register: 1, Kind: isa.RegisterInt}

	c.EmitTyped(context.Background(), isa.OpAddInt, destination, srcA, srcB)

	require.GreaterOrEqual(t, len(c.Function.Body), 2,
		"funnel should emit at least one coerce + the opAddInt")
	final := c.Function.Body[len(c.Function.Body)-1]
	require.Equal(t, isa.OpAddInt, final.Op, "final emitted op should be opAddInt")
	prev := c.Function.Body[len(c.Function.Body)-2]
	require.True(t, isa.InstrIsTier1SubOp(prev, isa.SubOpMoveGeneralToInt),
		"a general->int coercion should precede the opAddInt; got %v", prev)
}

func newTestCompiler(t *testing.T) *compile.Compiler {
	t.Helper()
	compiledFunction := &program.CompiledFunction{Name: "<test>"}
	return compile.NewCompiler(context.Background(), compile.CompilerConfig{
		Function:     compiledFunction,
		RootFunction: compiledFunction,
		ScopeName:    "<test>",
		Passes:       passes.DefaultOptions(),
	})
}
