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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestInstructionWritesRegisterInBankSeesSubTierWriters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		instr  isa.Instruction
		bank   isa.OperandRole
		reg    uint8
		writes bool
	}{
		{name: "get method writes the general destination in B", instr: isa.NewTier1Instruction(isa.SubOpBindMethodByName, 1, 2), bank: isa.RoleRegGeneral, reg: 1, writes: true},
		{name: "get method writes the receiver back through C", instr: isa.NewTier1Instruction(isa.SubOpBindMethodByName, 1, 2), bank: isa.RoleRegGeneral, reg: 2, writes: true},
		{name: "channel receive writes opaquely so any register may be written", instr: isa.NewTier1Instruction(isa.SubOpReceiveFromChannel, 1, 2), bank: isa.RoleRegFloat, reg: 9, writes: true},
		{name: "range next writes opaquely so any register may be written", instr: isa.NewTier1Instruction(isa.SubOpRangeNext, 1, 2), bank: isa.RoleRegString, reg: 4, writes: true},
		{name: "tier-2 inc int writes its int operand", instr: isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 5), bank: isa.RoleRegInt, reg: 5, writes: true},
		{name: "tier-2 inc int leaves other int registers alone", instr: isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 5), bank: isa.RoleRegInt, reg: 4, writes: false},
		{name: "tier-2 inc int does not write the general bank", instr: isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 5), bank: isa.RoleRegGeneral, reg: 5, writes: false},
		{name: "tier-2 make map writes its general destination", instr: isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 6), bank: isa.RoleRegGeneral, reg: 6, writes: true},
		{name: "tier-2 alloc struct literal writes its general destination", instr: isa.NewTier2Instruction(isa.SubOpTier2AllocateStructLiteral, 6), bank: isa.RoleRegGeneral, reg: 6, writes: true},
		{name: "tier-2 load nil writes its general destination", instr: isa.NewTier2Instruction(isa.SubOpTier2LoadNil, 6), bank: isa.RoleRegGeneral, reg: 6, writes: true},
		{name: "tier-2 set zero reads rather than writes", instr: isa.NewTier2Instruction(isa.SubOpTier2SetZeroGeneral, 6), bank: isa.RoleRegGeneral, reg: 6, writes: false},
		{name: "load zero resolves its bank through the kind marker", instr: isa.NewTier1Instruction(isa.SubOpLoadZero, 3, uint8(isa.RegisterFloat64)), bank: isa.RoleRegFloat, reg: 3, writes: true},
		{name: "load zero does not write the bank the marker excludes", instr: isa.NewTier1Instruction(isa.SubOpLoadZero, 3, uint8(isa.RegisterFloat64)), bank: isa.RoleRegInt, reg: 3, writes: false},
		{name: "move int writes its int destination", instr: isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 1), bank: isa.RoleRegInt, reg: 3, writes: true},
		{name: "move int does not write the float bank", instr: isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 1), bank: isa.RoleRegFloat, reg: 3, writes: false},
		{name: "tier-0 add int writes its destination", instr: mk(isa.OpAddInt64, 7, 1, 2), bank: isa.RoleRegInt, reg: 7, writes: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.writes, instructionWritesRegisterInBank(testCase.instr, testCase.bank, testCase.reg))
		})
	}
}

func TestBceInvalidatesLengthFactOnTier1OpaqueWriter(t *testing.T) {
	t.Parallel()

	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		isa.NewTier1Instruction(isa.SubOpReceiveFromChannel, 5, 2),
		mk(isa.OpExtension, 2, uint8(isa.RegisterInt64), 0),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}

	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)

	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[6].Op,
		"a channel receive may write the index register through its extension word, so the fact is dropped")
}

func TestBceInvalidatesLengthFactOnTier2IncrementInt64(t *testing.T) {
	t.Parallel()

	lo, hi := jumpOffset(5)
	body := []isa.Instruction{
		mk(isa.OpDrillTier1, uint8(isa.SubOpLengthSliceInt64), 1, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpLessThanInt64, 3, 2, 1),
		mk(isa.OpJumpIfFalse, 3, lo, hi),
		isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 2),
		mk(isa.OpGetSliceInt64Element, 4, 0, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}

	ElideRedundantBoundsChecks(compiledFunction, compiledFunction.Body)

	require.Equal(t, isa.OpGetSliceInt64Element, compiledFunction.Body[5].Op,
		"the index register changed after the proof, so the access stays checked")
}

func TestGvnResetsLastDefOnTier2Writer(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpLoadInt64Constant, 2, 0, 0),
		mk(isa.OpAddInt64, 3, 1, 2),
		isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 1),
		mk(isa.OpAddInt64, 4, 1, 2),
	}
	compiledFunction := &program.CompiledFunction{Body: body}

	_ = RunFunctionGvn(t.Context(), compiledFunction)

	require.Equal(t, isa.OpAddInt64, compiledFunction.Body[4].Op,
		"the operand was incremented between the two sums, so the second must be recomputed")
}

func TestLicmRefusesHoistWhenTier2MakeMapWritesReceiver(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpLoadInt64Constant, 1, 0, 0),
		mk(isa.OpGetStructFieldGeneral, 5, 4, 2),
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 4),
		mk(isa.OpExtension, 0, 0, 0),
		tier1Jump(-4),
	}
	originalLen := len(body)
	compiledFunction := &program.CompiledFunction{Body: body}

	_ = HoistLoopInvariantStructFieldReads(t.Context(), compiledFunction)

	require.Len(t, compiledFunction.Body, originalLen,
		"the receiver is rewritten inside the loop, so the read is not invariant")
}
