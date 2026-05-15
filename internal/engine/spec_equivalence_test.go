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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/asm"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func tableSymbolOf(tier isa.Tier) string {
	switch tier {
	case isa.TierSub1:
		return tier1JumpTableSymbol
	case isa.TierSub2:
		return tier2JumpTableSymbol
	case isa.TierSub3:
		return tier3JumpTableSymbol
	default:
		return ""
	}
}

func TestGeneratedHandlerTableMatchesSpec(t *testing.T) {
	t.Parallel()
	names := handlerNamesByTier()
	for _, row := range isa.AllSpecs() {
		require.Equalf(t, names[row.Tier][row.Code], row.Handler, "handler of %s", row)
	}
	for tier := range names {
		for code, name := range names[tier] {
			if name == "" {
				continue
			}
			row, ok := isa.SpecAt(isa.Tier(tier), uint8(code))
			require.Truef(t, ok, "registered handler %s at tier %d code %d has no spec row", name, tier, code)
			require.Equal(t, name, row.Handler)
		}
	}
}

func TestGeneratedJumpTableMatchesSpec(t *testing.T) {
	t.Parallel()
	want := map[asm.AsmHandlerJumpTableEntry]bool{}
	for _, entry := range buildStaticJumpTableEntries() {
		want[entry] = true
	}
	got := map[asm.AsmHandlerJumpTableEntry]bool{}
	for _, row := range isa.AllSpecs() {
		offset := int(row.Code) * bytesPerJumpTableSlot
		if row.AsmBody != "" {
			got[asm.AsmHandlerJumpTableEntry{Name: row.AsmBody, TableSymbol: row.AsmTable, Offset: offset}] = true
		}
		if row.ExitStub != "" && row.Tier != isa.TierMain {
			got[asm.AsmHandlerJumpTableEntry{Name: row.ExitStub, TableSymbol: tableSymbolOf(row.Tier), Offset: offset}] = true
		}
	}
	for entry := range want {
		require.Truef(t, got[entry], "hand-written jump-table entry %+v is not derived from the spec", entry)
	}
	for entry := range got {
		require.Truef(t, want[entry], "spec-derived jump-table entry %+v is not in the hand-written set", entry)
	}
}

func TestGeneratedShimRegistryMatchesSpec(t *testing.T) {
	t.Parallel()
	want := map[shimRegistration]bool{}
	for _, shim := range pathBShimRegistry {
		want[shim] = true
	}
	got := map[shimRegistration]bool{}
	for _, row := range isa.AllSpecs() {
		if row.Shim == "" {
			continue
		}
		got[shimRegistration{HandlerName: row.ShimHandler, ShimSuffix: row.Shim, Tier: row.Tier, Code: row.Code, Narrow: row.Has(isa.SpecShimNarrow), InstallSuppressed: row.Has(isa.SpecShimSuppressed)}] = true
	}
	require.Equal(t, want, got)
}

func TestGeneratedDirectExitRegistryMatchesSpec(t *testing.T) {
	t.Parallel()
	exits, reasons := directExitsByOpcode(t)
	for op, exit := range exits {
		row, ok := isa.SpecFor(op)
		require.Truef(t, ok, "direct exit for %s has no spec row", op)
		require.Equalf(t, exit[0], row.ExitStub, "exit stub of %s", row)
		require.Equalf(t, exit[1], row.ExitReason, "exit reason of %s", row)
	}
	for _, row := range isa.AllSpecs() {
		if row.ExitStub == "" {
			continue
		}
		require.Equalf(t, reasons[row.ExitStub], row.ExitReason, "registry reason of %s", row)
		if row.Tier == isa.TierMain {
			require.Containsf(t, exits, isa.Opcode(row.Code), "spec exit stub %s is not installed by installPerOpDirectExits", row)
		}
	}
}

func TestSpecJumpFlagMatchesPredicate(t *testing.T) {
	t.Parallel()
	for _, row := range isa.AllSpecs() {
		var instr isa.Instruction
		switch row.Tier {
		case isa.TierMain:
			instr = isa.NewInstruction(isa.Opcode(row.Code), 0, 0, 0)
		case isa.TierSub1:
			instr = isa.NewTier1Instruction(isa.SubOpcode(row.Code), 0, 0)
		case isa.TierSub2:
			instr = isa.NewTier2Instruction(isa.SubOpcodeTier2(row.Code), 0)
		case isa.TierSub3:
			instr = isa.NewTier3Instruction(isa.SubOpcodeTier3(row.Code))
		default:
			continue
		}
		layout, _ := isa.JumpLayoutOf(instr)
		require.Equalf(t, layout != isa.JumpLayoutNone, row.Has(isa.SpecJump), "jump flag of %s", row)

		if layout == isa.JumpLayoutNone || layout == isa.JumpLayoutMultiWay {
			continue
		}
		body := []isa.Instruction{instr, isa.NewInstruction(isa.OpExt, 0, 0, 0), isa.NewInstruction(isa.OpExt, 0, 0, 0)}
		_, ok := program.JumpTargetAt(body, 0)
		require.Truef(t, ok, "spec row %s is flagged as a jump but JumpTargetAt does not decode it", row)
	}
}
