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

package escape

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestAliasFreeAtInstructionSubTier(t *testing.T) {
	t.Parallel()

	const slot = 5
	general := uint8(isa.RegisterGeneral)
	integer := uint8(isa.RegisterInt)
	compiledFunction := &program.CompiledFunction{}

	cases := []struct {
		name      string
		instr     isa.Instruction
		aliasFree bool
	}{
		{name: "write shared cell of the slot as a general register", instr: isa.NewTier1Instruction(isa.SubOpWriteSharedCell, slot, general), aliasFree: false},
		{name: "write shared cell of the same index in the int bank", instr: isa.NewTier1Instruction(isa.SubOpWriteSharedCell, slot, integer), aliasFree: true},
		{name: "set global wide of the slot as a general register", instr: isa.NewTier1Instruction(isa.SubOpSetGlobalWide, slot, general), aliasFree: false},
		{name: "set global wide of the same index in the int bank", instr: isa.NewTier1Instruction(isa.SubOpSetGlobalWide, slot, integer), aliasFree: true},
		{name: "range init reads the collection in C", instr: isa.NewTier1Instruction(isa.SubOpRangeInit, 7, slot), aliasFree: false},
		{name: "range next reads the iterator in B", instr: isa.NewTier1Instruction(isa.SubOpRangeNext, slot, 0), aliasFree: false},
		{name: "nil test inspects without copying", instr: isa.NewTier1Instruction(isa.SubOpEqInterfaceNil, 1, slot), aliasFree: true},
		{name: "len inspects without copying", instr: isa.NewTier1Instruction(isa.SubOpLen, 1, slot), aliasFree: true},
		{name: "tier-2 make map only writes the slot", instr: isa.NewTier2Instruction(isa.SubOpTier2MakeMap, slot), aliasFree: true},
		{name: "tier-2 panic reads the slot", instr: isa.NewTier2Instruction(isa.SubOpTier2Panic, slot), aliasFree: false},
		{name: "an int move whose operand happens to equal the slot", instr: isa.NewTier1Instruction(isa.SubOpMoveInt, slot, slot), aliasFree: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.aliasFree, aliasFreeAtInstruction(compiledFunction, testCase.instr, slot))
		})
	}
}

func TestTier1SubOpEscapesIsShapeDriven(t *testing.T) {
	t.Parallel()

	const taintedSlot = 3
	var tainted [isa.GeneralRegisterBankSize]bool
	tainted[taintedSlot] = true
	general := uint8(isa.RegisterGeneral)
	integer := uint8(isa.RegisterInt)

	cases := []struct {
		name    string
		instr   isa.Instruction
		escapes bool
	}{
		{name: "load bool const with a pool index equal to the tainted slot", instr: isa.NewTier1Instruction(isa.SubOpLoadBoolConst, taintedSlot, taintedSlot), escapes: false},
		{name: "string const compare-and-branch with int register and pool index equal to the slot", instr: isa.NewTier1Instruction(isa.SubOpEqStringConstJumpFalse, taintedSlot, taintedSlot), escapes: false},
		{name: "int compare-and-branch on registers equal to the slot", instr: isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, taintedSlot, taintedSlot), escapes: false},
		{name: "set global wide stores the tainted general register", instr: isa.NewTier1Instruction(isa.SubOpSetGlobalWide, taintedSlot, general), escapes: true},
		{name: "set global wide of an int register with the same index", instr: isa.NewTier1Instruction(isa.SubOpSetGlobalWide, taintedSlot, integer), escapes: false},
		{name: "write shared cell stores the tainted general register", instr: isa.NewTier1Instruction(isa.SubOpWriteSharedCell, taintedSlot, general), escapes: true},
		{name: "write shared cell of an int register with the same index", instr: isa.NewTier1Instruction(isa.SubOpWriteSharedCell, taintedSlot, integer), escapes: false},
		{name: "map delete reads the tainted map register", instr: isa.NewTier1Instruction(isa.SubOpMapDelete, taintedSlot, 1), escapes: true},
		{name: "get global wide only writes its destination", instr: isa.NewTier1Instruction(isa.SubOpGetGlobalWide, taintedSlot, general), escapes: false},
		{name: "nil test inspects the tainted interface without publishing it", instr: isa.NewTier1Instruction(isa.SubOpEqInterfaceNil, 1, taintedSlot), escapes: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.escapes, tier1SubOpEscapes(isa.SubOpcode(testCase.instr.A), testCase.instr, &tainted))
		})
	}
}

func TestDeferPublishesTaintedArguments(t *testing.T) {
	t.Parallel()

	const (
		callee   = 1
		argument = 5
	)
	var tainted [isa.GeneralRegisterBankSize]bool
	tainted[argument] = true

	deferred := func(kind isa.RegisterKind) *program.CompiledFunction {
		return &program.CompiledFunction{Body: []isa.Instruction{
			isa.NewInstruction(isa.OpDefer, callee, 1, 0),
			isa.NewInstruction(isa.OpExt, 0, argument, uint8(kind)),
		}}
	}

	require.True(t, escapesAtInstruction(deferred(isa.RegisterGeneral), 0, &tainted),
		"a tainted general argument reaches the deferred call")
	require.False(t, escapesAtInstruction(deferred(isa.RegisterInt), 0, &tainted),
		"an int argument with the same register number is not the tainted pointer")

	var taintedCallee [isa.GeneralRegisterBankSize]bool
	taintedCallee[callee] = true
	require.True(t, escapesAtInstruction(deferred(isa.RegisterInt), 0, &taintedCallee),
		"the deferred callee value itself outlives the frame")
}
