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

package isa

import (
	"testing"
)

var operandBytesAtTier = [tierCount]int{3, 2, 1, 0}

var knownTierViolations = map[string]bool{}

func shapeOperandUse(shape OperandShape, tier Tier) (used int, overflows bool) {
	roles := [NumInstructionOperands]OperandRole{shape.A, shape.B, shape.C}
	available := operandBytesAtTier[tier]
	for index, role := range roles {
		if role == RoleNone {
			continue
		}
		used++

		if index < NumInstructionOperands-available {
			overflows = true
		}
	}
	return used, overflows
}

func TestEveryOperationHasADescribedShape(t *testing.T) {
	t.Parallel()

	for _, row := range AllSpecs() {

		if row.Has(specReserved) || row.Has(specMeta) {
			continue
		}
		shape := OperandShapeAt(row.Tier, row.Code)
		if shape.Flags&ShapeFlagDescribed == 0 {
			t.Errorf("%s has no operand shape; the verifier treats it as opaque, which means "+
				"no bank checking and no register-index bounds checking", row)
		}
	}
}

func TestOperationsSitAtTheTierTheirArityRequires(t *testing.T) {
	t.Parallel()

	for _, row := range AllSpecs() {
		if row.Has(specReserved) || row.Has(specMeta) {
			continue
		}
		shape := OperandShapeAt(row.Tier, row.Code)
		if shape.Flags&ShapeFlagDescribed == 0 {
			continue
		}
		used, overflows := shapeOperandUse(shape, row.Tier)

		if overflows {
			t.Errorf("%s carries an operand on a byte its tier reserves for a drill "+
				"discriminator, so the handler would read its own sub-opcode as an operand. It "+
				"needs %d operands and must sit at tier %d", row, used, tierForOperandCount(used))
			continue
		}

		wanted := tierForOperandCount(used)
		if row.Tier == wanted {
			if knownTierViolations[row.Name] {
				t.Errorf("%s is in knownTierViolations but now sits at the right tier; remove "+
					"it from the list", row)
			}
			continue
		}
		if knownTierViolations[row.Name] {
			continue
		}
		t.Errorf("%s uses %d operand byte(s) so it belongs at tier %d, not tier %d. Tier 0 is "+
			"capped at 256 slots, so an operation parked shallower than its arity requires "+
			"spends a slot another operation needs", row, used, wanted, row.Tier)
	}
}

func tierForOperandCount(count int) Tier {
	return Tier(NumInstructionOperands - count)
}
