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
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type fusedJumpCase struct {
	prepare     func(registers *Registers)
	name        string
	wantCounter int64
	instruction isa.Instruction
	wantJump    bool
}

const (
	fusedJumpCounter  = 1
	fusedJumpBound    = 2
	fusedJumpFlag     = 3
	fusedJumpWitness  = 5
	fusedJumpFellPast = 11
)

func runFusedJump(t *testing.T, testCase *fusedJumpCase, assembly bool) (jumped bool, counter int64) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, testCase.instruction, jumpExtension(1),
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, fusedJumpWitness, fusedJumpFellPast))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	testCase.prepare(registers)
	runOneInstruction(t, vm, frame, registers, testCase.instruction, assembly)
	require.NoError(t, vm.evalError)
	if assembly {
		return registers.Ints[fusedJumpWitness] != fusedJumpFellPast, registers.Ints[fusedJumpCounter]
	}
	return frame.ProgramCounter == len(builder.body)-1, registers.Ints[fusedJumpCounter]
}

func fusedJumpCases() []fusedJumpCase {
	unsigned := func(left, right uint64) func(*Registers) {
		return func(registers *Registers) {
			registers.Uints[fusedJumpCounter], registers.Uints[fusedJumpBound] = left, right
		}
	}
	counted := func(counter, bound int64) func(*Registers) {
		return func(registers *Registers) {
			registers.Ints[fusedJumpCounter], registers.Ints[fusedJumpBound] = counter, bound
		}
	}
	flagged := func(flag bool) func(*Registers) {
		return func(registers *Registers) {
			for register := range registers.Bools {
				registers.Bools[register] = !flag
			}
			registers.Bools[fusedJumpFlag] = flag
		}
	}
	equal := isa.NewTier1Instruction(isa.SubOpEqualUint64ThenJumpIfFalse, fusedJumpCounter, fusedJumpBound)
	different := isa.NewTier1Instruction(isa.SubOpNotEqualUint64ThenJumpIfFalse, fusedJumpCounter, fusedJumpBound)
	latch := isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessOrEqual, fusedJumpCounter, fusedJumpBound)
	ifFalse := isa.NewTier2Instruction(isa.SubOpTier2JumpIfFalseBool, fusedJumpFlag)
	ifTrue := isa.NewTier2Instruction(isa.SubOpTier2JumpIfTrueBool, fusedJumpFlag)

	fixed := []fusedJumpCase{
		{name: "a counter stepped below its inclusive bound", instruction: latch, prepare: counted(8, 10), wantJump: true, wantCounter: 9},
		{name: "a counter stepped onto its inclusive bound", instruction: latch, prepare: counted(9, 10), wantJump: true, wantCounter: 10},
		{name: "a counter stepped past its inclusive bound", instruction: latch, prepare: counted(10, 10), wantCounter: 11},
		{name: "a negative counter stepped onto its bound", instruction: latch, prepare: counted(-3, -2), wantJump: true, wantCounter: -2},
		{name: "a counter stepped onto the largest int", instruction: latch, prepare: counted(math.MaxInt64-1, math.MaxInt64),
			wantJump: true, wantCounter: math.MaxInt64},
		{name: "a counter stepped past the largest int wraps below any bound", instruction: latch, prepare: counted(math.MaxInt64, 10),
			wantJump: true, wantCounter: math.MinInt64},
		{name: "a false flag tested for false", instruction: ifFalse, prepare: flagged(false), wantJump: true},
		{name: "a true flag tested for false", instruction: ifFalse, prepare: flagged(true)},
		{name: "a true flag tested for true", instruction: ifTrue, prepare: flagged(true), wantJump: true},
		{name: "a false flag tested for true", instruction: ifTrue, prepare: flagged(false)},
	}
	pairs := []struct {
		left, right uint64
	}{{5, 5}, {5, 6}, {1 << 63, 1 << 63}, {math.MaxUint64, 0}, {0, 1 << 32}, {1 << 32, 1<<32 + 1}, {math.MaxUint64, math.MaxUint64}}
	cases := make([]fusedJumpCase, 0, len(fixed)+2*len(pairs))
	cases = append(cases, fixed...)
	for _, pair := range pairs {
		same := pair.left == pair.right
		cases = append(cases,
			fusedJumpCase{name: fmt.Sprintf("%d and %d tested for equality", pair.left, pair.right), instruction: equal,
				prepare: unsigned(pair.left, pair.right), wantJump: !same, wantCounter: 0},
			fusedJumpCase{name: fmt.Sprintf("%d and %d tested for difference", pair.left, pair.right), instruction: different,
				prepare: unsigned(pair.left, pair.right), wantJump: same, wantCounter: 0})
	}
	return cases
}

func TestFusedJumpsBranchAsTheirOperandsSay(t *testing.T) {
	for _, tt := range fusedJumpCases() {
		t.Run(tt.name, func(t *testing.T) {
			tier, code := isa.TierSub1, tt.instruction.A
			if isa.SubOpcode(tt.instruction.A) == isa.SubOpDrillTier2 {
				tier, code = isa.TierSub2, tt.instruction.B
			}
			spec, known := isa.SpecAt(tier, code)
			require.True(t, known)
			require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the operation has an assembly body")
			for _, assembly := range dispatchRoutes() {
				jumped, counter := runFusedJump(t, &tt, assembly)
				require.Equalf(t, tt.wantJump, jumped, "whether the jump is taken, %s", routeName(assembly))
				require.Equalf(t, tt.wantCounter, counter, "the counter register afterwards, %s", routeName(assembly))
			}
		})
	}
}
