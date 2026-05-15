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

var explicitlyClassifiedOpcodes = map[isa.Opcode]string{
	isa.OpAllocIndirect: "B is the source general register",
	isa.OpAddr:          "B is the source general register",
	isa.OpGetField:      "reads through the receiver to a new value that does not alias it",
	isa.OpDeref:         "reads through the receiver to a new value that does not alias it",
	isa.OpMoveGeneral:   "the walk propagates taint rather than classifying an escape",
	isa.OpSetField:      "C is the stored value",
	isa.OpMakeClosure:   "makeClosureCaptures inspects the nested function's upvalues",
	isa.OpDrillTier1:    "tier1OrDrilledEscapes routes the call, builtin and select sub-ops to readers of the call site and extension words, and drills the rest",
	isa.OpGo:            "spawnedArgumentsEscape reads the argument extension words",
	isa.OpDefer:         "spawnedArgumentsEscape reads the argument extension words; the deferred call runs after the frame's body completes",
}

func TestEveryOpcodeIsClassified(t *testing.T) {
	t.Parallel()

	var unclassified []string
	for opIndex := range isa.OpcodeCount {
		op := isa.Opcode(opIndex)
		if op.String() == "UNKNOWN" {
			continue
		}
		if isa.OperandShapeFor(op).Flags&isa.ShapeFlagDescribed != 0 {
			continue
		}
		if _, handled := explicitlyClassifiedOpcodes[op]; handled {
			continue
		}
		if _, cannotPublish := opcodeCannotPublishGeneral[isa.FlatIndexOf(op)]; cannotPublish {
			continue
		}
		if _, publishes := opcodePublishesGeneral[isa.FlatIndexOf(op)]; publishes {
			continue
		}
		var noTaint [isa.GeneralRegisterBankSize]bool
		if _, classified := structFieldOpEscapes(isa.Instruction{Op: op}, &noTaint); classified {
			continue
		}
		unclassified = append(unclassified, op.String())
	}

	require.Emptyf(t, unclassified,
		"these opcodes reach escapesAtUnclassifiedOp with no recorded classification.\n"+
			"Each now fails closed, which is safe but gives up arena promotion wherever it\n"+
			"appears. Resolve each one by describing it in isa.OperandShapes (preferred, it\n"+
			"also feeds the verifier), classifying it in escapesAtInstruction, or adding it to\n"+
			"opcodeCannotPublishGeneral or opcodePublishesGeneral with the reason.\n"+
			"unclassified: %v", unclassified)
}

func TestUndescribedEscapeClassificationsHold(t *testing.T) {
	t.Parallel()

	var allTainted [isa.GeneralRegisterBankSize]bool
	for slot := range allTainted {
		allTainted[slot] = true
	}
	for slot, reason := range opcodeCannotPublishGeneral {
		instr := instructionForFlatIndex(slot)
		require.Falsef(t, escapesAtUnclassifiedOp(instr, &allTainted),
			"%s is listed as unable to publish a general register (%s) but the pass says it escapes",
			isa.InstructionDisplayName(instr), reason)
	}
	for slot, reason := range opcodePublishesGeneral {
		instr := instructionForFlatIndex(slot)
		require.Truef(t, escapesAtUnclassifiedOp(instr, &allTainted),
			"%s is listed as publishing a general register (%s) but the pass says it is safe",
			isa.InstructionDisplayName(instr), reason)
	}
}

func TestClassificationEntriesAreReachable(t *testing.T) {
	t.Parallel()

	for name, list := range map[string]map[int]string{
		"opcodeCannotPublishGeneral": opcodeCannotPublishGeneral,
		"opcodePublishesGeneral":     opcodePublishesGeneral,
	} {
		for slot, reason := range list {
			instr := instructionForFlatIndex(slot)
			label := isa.InstructionDisplayName(instr)
			require.NotEmptyf(t, reason, "%s: %s must carry its reason", name, label)
			_, hasOwnArm := explicitlyClassifiedOpcodes[instr.Op]
			require.Falsef(t, hasOwnArm && instr.Op != isa.OpDrillTier1,
				"%s: %s is also handled by its own arm in escapesAtInstruction, which runs\n"+
					"first, so this entry is dead. Delete the entry.", name, label)
			var noTaint [isa.GeneralRegisterBankSize]bool
			_, classifiedElsewhere := structFieldOpEscapes(instr, &noTaint)
			require.Falsef(t, classifiedElsewhere,
				"%s: %s is already classified by structFieldOpEscapes, which runs first, so\n"+
					"this entry is dead. Delete the entry.", name, label)
		}
	}
}

func TestRangeInitPropagatesTaintToIterator(t *testing.T) {
	t.Parallel()

	const (
		collectionSlot = 0
		iteratorSlot   = 1
	)

	publishIterator := isa.NewTier1Instruction(isa.SubOpWriteSharedCell, iteratorSlot, 3)

	withRangeInit := &program.CompiledFunction{Body: []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpRangeInit, iteratorSlot, collectionSlot),
		publishIterator,
	}}
	require.True(t, analyseGeneralRegisterEscape(withRangeInit, collectionSlot, 0),
		"publishing an iterator built over the tainted collection must count as an escape")

	withoutRangeInit := &program.CompiledFunction{Body: []isa.Instruction{publishIterator}}
	require.False(t, analyseGeneralRegisterEscape(withoutRangeInit, collectionSlot, 0),
		"publishing an unrelated register must not be attributed to the collection")
}

func instructionForFlatIndex(index int) isa.Instruction {
	tier := isa.Tier(index / isa.SlotsPerTier)
	code := uint8(index % isa.SlotsPerTier)
	switch tier {
	case isa.TierMain:
		return isa.NewInstruction(isa.Opcode(code), 1, 2, 3)
	case isa.TierSub1:
		return isa.NewTier1Instruction(isa.SubOpcode(code), 2, 3)
	case isa.TierSub2:
		return isa.NewTier2Instruction(isa.SubOpcodeTier2(code), 3)
	default:
		return isa.NewTier3Instruction(isa.SubOpcodeTier3(code))
	}
}
