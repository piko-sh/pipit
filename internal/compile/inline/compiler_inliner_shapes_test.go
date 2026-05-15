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

package inline

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type allowListedOperation struct {
	name      string
	shape     isa.OperandShape
	poolShape inlinePoolShape
}

func allowListedOperations(t *testing.T) []allowListedOperation {
	t.Helper()
	var operations []allowListedOperation
	for opIndex, allowed := range phase2OpcodeAllowList {
		if !allowed {
			continue
		}
		op := isa.Opcode(opIndex)
		operations = append(operations, allowListedOperation{
			name:      op.String(),
			shape:     isa.OperandShapeFor(op),
			poolShape: inlinePoolShapes[isa.FlatIndexOf(op)],
		})
	}
	for _, sub := range phase2AllowedTier1SubOps {
		if sub == isa.SubOpDrillTier2 {
			continue
		}
		operations = append(operations, allowListedOperation{
			name:      fmt.Sprintf("tier-1 %s", isa.InstructionDisplayName(isa.NewTier1Instruction(sub, 0, 0))),
			shape:     isa.OperandShapeAt(isa.TierSub1, uint8(sub)),
			poolShape: inlinePoolShapes[isa.FlatIndexOfSub1(sub)],
		})
	}
	return operations
}

func TestExtensionWordAgreesWithOperandShapes(t *testing.T) {
	t.Parallel()

	var disagreements []string
	for _, operation := range allowListedOperations(t) {
		if operation.shape.Flags&isa.ShapeFlagDescribed == 0 {
			continue
		}
		inlinerSaysExtension := operation.poolShape.hasExtensionWord
		isaSaysExtension := operation.shape.Flags&isa.ShapeFlagFollowsExtension != 0
		if inlinerSaysExtension == isaSaysExtension {
			continue
		}
		disagreements = append(disagreements,
			fmt.Sprintf("%s (inliner=%v isa=%v)", operation.name, inlinerSaysExtension, isaSaysExtension))
	}

	require.Emptyf(t, disagreements,
		"inlinePoolShapes and isa.OperandShapes disagree about which operations carry a trailing\n"+
			"isa.OpExt word; fix the wrong table. disagreements: %v", disagreements)
}

func TestInlinePoolShapesDescribeEveryIndexedOperand(t *testing.T) {
	t.Parallel()

	for _, operation := range allowListedOperations(t) {
		if operation.shape.Flags&isa.ShapeFlagDescribed == 0 {
			continue
		}
		poolShape := operation.poolShape
		described := poolShape.bKindByte != 0 || poolShape.cKindByte != 0 ||
			poolShape.bcWide16 != 0 || poolShape.extAWide16 != 0 || poolShape.hasExtensionWord
		roles := [isa.NumInstructionOperands]isa.OperandRole{operation.shape.A, operation.shape.B, operation.shape.C}
		for position, role := range roles {
			if !isa.RoleIndexesPerFunctionTable(role) {
				continue
			}
			require.Truef(t, described,
				"%s operand %d has role %v, which indexes a pool or table, but inlinePoolShapes has no descriptor",
				operation.name, position, role)
		}
	}
}

func TestRemapAcceptsEveryAllowListedTier1SubOp(t *testing.T) {
	t.Parallel()

	for _, sub := range phase2AllowedTier1SubOps {
		if sub == isa.SubOpDrillTier2 {
			continue
		}
		t.Run(isa.InstructionDisplayName(isa.NewTier1Instruction(sub, 0, 0)), func(t *testing.T) {
			t.Parallel()
			ctx := allMappingContext()
			_, ok := remapOperands(isa.NewTier1Instruction(sub, 0, 0), ctx)
			require.True(t, ok, "allow-listed sub-op is refused by remapOperands")
		})
	}
}

func allMappingContext() *inlineContext {
	callee := &program.CompiledFunction{
		IntConstants:    []int64{0},
		FloatConstants:  []float64{0},
		StringConstants: []string{""},
		BoolConstants:   []bool{false},
		UintConstants:   []uint64{0},
		CallSites:       []program.CallSite{{}},
	}
	ctx := &inlineContext{caller: &program.CompiledFunction{}, callee: callee}
	ctx.resetRegisterRemap()
	for bank := range ctx.remap {
		for slot := range ctx.remap[bank] {
			ctx.remap[bank][slot] = int16(slot)
		}
	}
	return ctx
}
