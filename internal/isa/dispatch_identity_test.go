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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlatIndexGivesEveryTierItsOwnBand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		index int
		lower int
		upper int
	}{
		{name: "a tier-0 opcode", index: FlatIndexOf(OpAddInt), lower: 0, upper: SlotsPerTier},
		{name: "a tier-1 sub-op", index: FlatIndexOfSub1(SubOpCall), lower: SlotsPerTier, upper: 2 * SlotsPerTier},
		{name: "a tier-2 sub-op", index: FlatIndexOfSub2(SubOpTier2Return), lower: 2 * SlotsPerTier, upper: 3 * SlotsPerTier},
		{name: "a tier-3 sub-op", index: FlatIndexOfSub3(SubOpTier3Nop), lower: 3 * SlotsPerTier, upper: FlatDispatchSlots},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.GreaterOrEqual(t, tt.index, tt.lower)
			require.Less(t, tt.index, tt.upper)
		})
	}
}

func TestFlatIndexForFollowsTheDrillCascade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		want        int
	}{
		{name: "a tier-0 opcode", instruction: NewInstruction(OpAddInt, 0, 0, 0), want: FlatIndexOf(OpAddInt)},
		{name: "a tier-1 sub-op", instruction: NewTier1Instruction(SubOpCall, 0, 0), want: FlatIndexOfSub1(SubOpCall)},
		{
			name: "a tier-2 sub-op", instruction: NewTier2Instruction(SubOpTier2Return, 0),
			want: FlatIndexOfSub2(SubOpTier2Return),
		},
		{
			name: "a tier-3 sub-op", instruction: NewTier3Instruction(SubOpTier3Nop),
			want: FlatIndexOfSub3(SubOpTier3Nop),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, FlatIndexFor(tt.instruction))
			require.Less(t, FlatIndexFor(tt.instruction), FlatDispatchSlots)
		})
	}
}

func TestSharedDefaultCostTableIsTheSameTableEveryTime(t *testing.T) {
	t.Parallel()

	first := SharedDefaultCostTable()
	require.NotNil(t, first)
	require.Same(t, first, SharedDefaultCostTable(), "callers key off one shared table, so it must not be rebuilt")
}

func TestInstrIsTier3SubOpMatchesOnlyTheFullCascade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		want        bool
	}{
		{name: "the matching tier-3 sub-op", instruction: NewTier3Instruction(SubOpTier3Nop), want: true},
		{name: "a tier-0 opcode", instruction: NewInstruction(OpAddInt, 0, 0, 0)},
		{name: "a tier-1 sub-op", instruction: NewTier1Instruction(SubOpCall, 0, 0)},
		{name: "a tier-2 sub-op", instruction: NewTier2Instruction(SubOpTier2Return, 0)},
		{name: "another tier-3 sub-op", instruction: NewTier3Instruction(SubOpTier3ReturnVoid)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, InstrIsTier3SubOp(tt.instruction, SubOpTier3Nop))
		})
	}
}

func TestInstructionDisplayNameLabelsTheTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		wantPrefix  string
	}{
		{name: "a tier-0 opcode", instruction: NewInstruction(OpAddInt, 0, 0, 0), wantPrefix: "0:"},
		{name: "a tier-1 sub-op", instruction: NewTier1Instruction(SubOpCall, 0, 0), wantPrefix: "1:"},
		{name: "a tier-2 sub-op", instruction: NewTier2Instruction(SubOpTier2Return, 0), wantPrefix: "2:"},
		{name: "a tier-3 sub-op", instruction: NewTier3Instruction(SubOpTier3Nop), wantPrefix: "3:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			label := InstructionDisplayName(tt.instruction)
			require.True(t, strings.HasPrefix(label, tt.wantPrefix), "label %q must name its tier", label)
			require.Greater(t, len(label), len(tt.wantPrefix), "the label must name the operation too")
		})
	}
}

func TestSpecForResolvesTierZeroRows(t *testing.T) {
	t.Parallel()

	row, ok := SpecFor(OpAddInt)
	require.True(t, ok)
	require.Equal(t, TierMain, row.Tier)
	require.Equal(t, uint8(OpAddInt), row.Code)

	_, ok = SpecAt(Tier(99), 0)
	require.False(t, ok, "a tier past the encoding has no rows")
}

func TestOpSpecRendersItsOwnIdentity(t *testing.T) {
	t.Parallel()

	row, ok := SpecFor(OpAddInt)
	require.True(t, ok)

	rendered := row.String()
	require.Contains(t, rendered, row.Name)
	require.Contains(t, rendered, "tier0")

	require.Equal(t, OperandShapeAt(row.Tier, row.Code), row.Shape(),
		"a row describes its own operands rather than borrowing the drill wildcard")
}

func TestRoleIndexesPerFunctionTableNamesTheRenumberableRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role OperandRole
		want bool
	}{
		{name: "a constant-pool index", role: roleConstIndex, want: true},
		{name: "a type-table index", role: roleTypeIndex, want: true},
		{name: "an int register", role: RoleRegInt, want: false},
		{name: "an operand that names no role", role: RoleNone, want: false},
		{name: "a field index", role: roleFieldIndex, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, RoleIndexesPerFunctionTable(tt.role))
		})
	}
}

func TestRegisterKindNamesEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind RegisterKind
		want string
	}{
		{name: "the int bank", kind: RegisterInt, want: "int"},
		{name: "the float bank", kind: RegisterFloat, want: "float"},
		{name: "the string bank", kind: RegisterString, want: "string"},
		{name: "the general bank", kind: RegisterGeneral, want: "general"},
		{name: "the bool bank", kind: RegisterBool, want: "bool"},
		{name: "the uint bank", kind: RegisterUint, want: "uint"},
		{name: "the complex bank", kind: RegisterComplex, want: "complex"},
		{name: "the int-slice bank", kind: RegisterSliceInt, want: "sliceInt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, tt.kind.String())
		})
	}

	t.Run("every bank names itself distinctly", func(t *testing.T) {
		t.Parallel()

		seen := map[string]bool{}
		for kind := range RegisterKind(NumRegisterKinds) {
			name := kind.String()
			require.NotEmpty(t, name)
			require.Falsef(t, seen[name], "two banks both render as %q", name)
			seen[name] = true
		}
	})
}

func TestElementKindForTypedSliceNamesTheElementBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind RegisterKind
		want RegisterKind
	}{
		{name: "an int slice", kind: RegisterSliceInt, want: RegisterInt},
		{name: "a float slice", kind: RegisterSliceFloat, want: RegisterFloat},
		{name: "a string slice", kind: RegisterSliceString, want: RegisterString},
		{name: "a bool slice", kind: RegisterSliceBool, want: RegisterBool},
		{name: "a uint slice", kind: RegisterSliceUint, want: RegisterUint},
		{name: "a byte slice", kind: RegisterSliceByte, want: RegisterUint},
		{name: "a bank that is not a typed slice", kind: RegisterInt, want: RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, ElementKindForTypedSlice(tt.kind))
		})
	}
}
