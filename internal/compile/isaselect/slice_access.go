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

package isaselect

import "pipit.sh/pipit/internal/isa"

// SliceAccessPlan is the instruction chosen for one typed-slice element read or write:
// either a tier-0 opcode or a tier-1 sub-op under isa.OpDrillTier1 (the direct typed-bank
// forms carry their third operand in an extension word).
type SliceAccessPlan struct {
	// Op is the tier-0 opcode when UseTier1 is false.
	Op isa.Opcode

	// Tier1 is the sub-op when UseTier1 is true.
	Tier1 isa.SubOpcode

	// UseTier1 selects the tier-1 direct form.
	UseTier1 bool
}

// PlanSliceGet picks the read instruction for a slice element access.
//
// Takes collectionKind (isa.RegisterKind) which holds the slice header.
// Takes elementKind (isa.RegisterKind) which receives the element.
// Takes boundsSafe (bool) which is true when the index has been proven in range.
//
// Returns SliceAccessPlan which is the instruction.
// Returns bool which is false when elementKind has no typed read.
func PlanSliceGet(collectionKind, elementKind isa.RegisterKind, boundsSafe bool) (SliceAccessPlan, bool) {
	if collectionKind == isa.RegisterSliceInt && elementKind == isa.RegisterInt {
		if boundsSafe {
			return SliceAccessPlan{Op: isa.OpSliceGetIntDirectUnchecked, Tier1: 0, UseTier1: false}, true
		}
		return SliceAccessPlan{Op: isa.OpSliceGetIntDirect, Tier1: 0, UseTier1: false}, true
	}
	if subOp, ok := typedSliceDirectGetTier1SubOp(collectionKind); ok && isa.ElementKindForTypedSlice(collectionKind) == elementKind {
		return SliceAccessPlan{Op: 0, Tier1: subOp, UseTier1: true}, true
	}
	op, ok := sliceGetOpForElement(elementKind)
	return SliceAccessPlan{Op: op, Tier1: 0, UseTier1: false}, ok
}

// PlanSliceSet picks the write instruction for a slice whose header lives in
// collectionKind and whose elements come from elementKind, mirroring PlanSliceGet.
//
// Takes collectionKind (isa.RegisterKind) which holds the slice header.
// Takes elementKind (isa.RegisterKind) which supplies the element.
//
// Returns SliceAccessPlan which is the instruction.
// Returns bool which is false when elementKind has no typed write.
func PlanSliceSet(collectionKind, elementKind isa.RegisterKind) (SliceAccessPlan, bool) {
	if collectionKind == isa.RegisterSliceInt && elementKind == isa.RegisterInt {
		return SliceAccessPlan{Op: isa.OpSliceSetIntDirect, Tier1: 0, UseTier1: false}, true
	}
	if subOp, ok := typedSliceDirectSetTier1SubOp(collectionKind); ok && isa.ElementKindForTypedSlice(collectionKind) == elementKind {
		return SliceAccessPlan{Op: 0, Tier1: subOp, UseTier1: true}, true
	}
	op, ok := sliceSetOpForElement(elementKind)
	return SliceAccessPlan{Op: op, Tier1: 0, UseTier1: false}, ok
}

// sliceGetOpForElement maps an element kind to the general-bank slice read opcode.
//
// Takes elementKind (isa.RegisterKind) which receives the element.
//
// Returns isa.Opcode which is the read.
// Returns bool which is false for other kinds.
func sliceGetOpForElement(elementKind isa.RegisterKind) (isa.Opcode, bool) {
	switch elementKind {
	case isa.RegisterInt:
		return isa.OpSliceGetInt, true
	case isa.RegisterFloat:
		return isa.OpSliceGetFloat, true
	case isa.RegisterString:
		return isa.OpSliceGetString, true
	case isa.RegisterBool:
		return isa.OpSliceGetBool, true
	case isa.RegisterUint:
		return isa.OpSliceGetUint, true
	default:
		return 0, false
	}
}

// sliceSetOpForElement maps an element kind to the general-bank slice write opcode.
//
// Takes elementKind (isa.RegisterKind) which supplies the element.
//
// Returns isa.Opcode which is the write.
// Returns bool which is false for other kinds.
func sliceSetOpForElement(elementKind isa.RegisterKind) (isa.Opcode, bool) {
	switch elementKind {
	case isa.RegisterInt:
		return isa.OpSliceSetInt, true
	case isa.RegisterFloat:
		return isa.OpSliceSetFloat, true
	case isa.RegisterString:
		return isa.OpSliceSetString, true
	case isa.RegisterBool:
		return isa.OpSliceSetBool, true
	case isa.RegisterUint:
		return isa.OpSliceSetUint, true
	default:
		return 0, false
	}
}
