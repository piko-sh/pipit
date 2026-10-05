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

func arithmeticLoop(inner ...isa.Instruction) *program.CompiledFunction {
	body := make([]isa.Instruction, 0, len(inner)+5)
	body = append(body, mk(isa.OpLoadIntConst, 0, 0, 0), mk(isa.OpNop, 0, 0, 0))
	body = append(body, inner...)
	body = append(body, mk(isa.OpAddInt, 0, 0, 1), tier1Jump(0), isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
	program.SetJumpTarget(body, len(body)-2, 1)
	return &program.CompiledFunction{Body: body, NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 16, isa.RegisterUint: 8}}
}

func TestArithmeticHoistMovesInvariantChains(t *testing.T) {
	t.Parallel()
	cf := arithmeticLoop(
		mk(isa.OpSubInt, 10, 9, 2),
		mk(isa.OpMulInt, 11, 8, 10),
		mk(isa.OpTruncateNarrow, 11, 32, uint8(isa.RegisterInt)),
		mk(isa.OpAddInt, 12, 11, 0),
	)
	require.NoError(t, hoistLoopInvariantArithmetic(context.Background(), cf, nil))
	require.Equal(t, []isa.Opcode{isa.OpSubInt, isa.OpMulInt, isa.OpTruncateNarrow}, opcodes(cf.Body[1:4]), "the chain leaves the loop in order")
	require.Equal(t, isa.OpNop, cf.Body[4].Op, "the header follows the hoisted words")
	require.Equal(t, mk(isa.OpAddInt, 12, 11, 0), cf.Body[5], "the variant use stays")
}

func TestArithmeticHoistKeepsVariantOperations(t *testing.T) {
	t.Parallel()
	for name, inner := range map[string][]isa.Instruction{
		"reads the induction variable": {mk(isa.OpMulInt, 10, 0, 8)},
		"accumulates":                  {mk(isa.OpAddInt, 10, 10, 8)},
		"can fault":                    {mk(isa.OpDivInt, 10, 9, 8)},
		"destination read before":      {mk(isa.OpAddInt, 12, 10, 0), mk(isa.OpMulInt, 10, 9, 8)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cf := arithmeticLoop(inner...)
			original := slices.Clone(cf.Body)
			require.NoError(t, hoistLoopInvariantArithmetic(context.Background(), cf, nil))
			require.Equal(t, original, cf.Body)
		})
	}
}

func TestArithmeticHoistRenamesReusedTemporaries(t *testing.T) {
	t.Parallel()
	cf := arithmeticLoop(
		mk(isa.OpMulInt, 10, 9, 8),
		mk(isa.OpAddInt, 12, 10, 0),
		mk(isa.OpAddInt, 10, 0, 0),
		mk(isa.OpAddInt, 13, 10, 0),
	)
	require.NoError(t, hoistLoopInvariantArithmetic(context.Background(), cf, nil))
	require.Equal(t, mk(isa.OpMulInt, 16, 9, 8), cf.Body[1])
	require.Equal(t, mk(isa.OpAddInt, 12, 16, 0), cf.Body[3])
	require.Equal(t, mk(isa.OpAddInt, 13, 10, 0), cf.Body[5])
}
