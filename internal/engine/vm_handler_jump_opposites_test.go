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
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const (
	oppositeJumpLeft     = 1
	oppositeJumpRight    = 2
	oppositeJumpFlag     = 3
	oppositeJumpWitness  = 5
	oppositeJumpFellPast = 11
)

func runConditionalJump(t *testing.T, instruction isa.Instruction, left, right int64, assembly bool) (jumped bool) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.intConstants = []int64{right, right, right}
	layout, _ := isa.JumpLayoutOf(instruction)
	switch layout {
	case isa.JumpLayoutWord:
		instruction.B, instruction.C = isa.SplitOffset(1)
		builder.body = append(builder.body, instruction)
	case isa.JumpLayoutExtension:
		builder.body = append(builder.body, instruction, jumpExtension(1))
	default:
		require.Failf(t, "unexpected layout", "%s keeps its offset in layout %d", instruction.Op, layout)
	}
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, oppositeJumpWitness, oppositeJumpFellPast))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	registers.Ints[oppositeJumpLeft], registers.Ints[oppositeJumpRight] = left, right
	registers.Uints[oppositeJumpLeft], registers.Uints[oppositeJumpRight] = uint64(left), uint64(right)
	registers.Bools[oppositeJumpFlag] = left < right
	if left != right {
		registers.General[oppositeJumpLeft] = reflect.ValueOf(new(left))
	}
	runOneInstruction(t, vm, frame, registers, instruction, assembly)
	require.NoError(t, vm.evalError)
	if assembly {
		return registers.Ints[oppositeJumpWitness] != oppositeJumpFellPast
	}
	return frame.ProgramCounter == len(builder.body)-1
}

func TestAConditionalJumpAndItsOppositeNeverGoTheSameWay(t *testing.T) {
	t.Parallel()

	operands := []struct {
		left, right int64
	}{
		{0, 0}, {5, 5}, {5, 6}, {6, 5}, {-1, 0}, {0, -1}, {0, 1}, {1, 0},
		{math.MinInt64, math.MaxInt64}, {math.MaxInt64, math.MinInt64}, {math.MinInt64, math.MinInt64},
	}
	paired := 0
	for code := range isa.SlotsPerTier {
		for _, instruction := range []isa.Instruction{
			isa.NewInstruction(isa.Opcode(code), oppositeJumpLeft, 0, 0),
			isa.NewTier1Instruction(isa.SubOpcode(code), oppositeJumpLeft, oppositeJumpRight),
			isa.NewTier2Instruction(isa.SubOpcodeTier2(code), oppositeJumpFlag),
		} {
			opposite, ok := isa.OppositeConditionalJump(instruction)
			if !ok {
				continue
			}
			paired++
			for _, assembly := range dispatchRoutes() {
				taken := 0
				for _, pair := range operands {
					jumped := runConditionalJump(t, instruction, pair.left, pair.right, assembly)
					oppositeJumped := runConditionalJump(t, opposite, pair.left, pair.right, assembly)
					require.NotEqualf(t, jumped, oppositeJumped, "%s and its opposite with operands %d and %d by the %s",
						instruction, pair.left, pair.right, routeName(assembly))
					if jumped {
						taken++
					}
				}
				require.Positivef(t, taken, "%s is taken for some operands", instruction)
				require.Lessf(t, taken, len(operands), "%s is not taken for some operands", instruction)
			}
		}
	}
	require.Equal(t, 20, paired)
}
