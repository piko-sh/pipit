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

package scope

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestScalarBanksSpillPastThreshold(t *testing.T) {
	t.Parallel()

	for _, kind := range []isa.RegisterKind{
		isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterGeneral,
		isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex,
	} {
		stack := NewScopeStack("run")
		stack.PushScope()
		var spilled bool
		for index := range int(spillThreshold) + 4 {
			location := stack.DeclareVar("v"+string(rune('a'+index%26))+string(rune('a'+index/26)), kind)
			if location.IsSpilled {
				spilled = true
			}
		}
		require.True(t, spilled, "%s should spill past the threshold", kind)
		require.NoError(t, stack.OverflowError(), "%s spilling must not be refused", kind)
	}
}

func TestTypedSliceBanksRefuseToSpill(t *testing.T) {
	t.Parallel()

	for _, kind := range []isa.RegisterKind{
		isa.RegisterSliceInt, isa.RegisterSliceFloat, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint, isa.RegisterSliceByte,
	} {
		stack := NewScopeStack("run")
		stack.PushScope()
		for index := range int(spillThreshold) + 4 {
			stack.DeclareVar("v"+string(rune('a'+index%26))+string(rune('a'+index/26)), kind)
		}
		err := stack.OverflowError()
		require.Error(t, err, "%s must refuse to spill", kind)
		require.ErrorIs(t, err, fault.ErrSpillUnsupportedBank)
		require.ErrorIs(t, err, fault.ErrRegisterOverflow, "a refused spill is a register overflow")
		require.True(t, stack.Alloc.RefusedSpill())
		require.Contains(t, err.Error(), kind.String())
	}
}

func TestStatementWindowsRecordPeakAndReads(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	kept := stack.DeclareVar("kept", isa.RegisterInt)
	outer := stack.BeginStatement()
	inner := stack.BeginStatement()
	_, _ = stack.LookupVar("kept")
	stack.Alloc.Alloc(isa.RegisterInt)
	stack.Alloc.Alloc(isa.RegisterInt)
	innerUsage := stack.EndStatement(&inner)
	require.Equal(t, uint32(1), innerUsage.Start[isa.RegisterInt])
	require.Equal(t, uint32(3), innerUsage.Peak[isa.RegisterInt])
	require.True(t, innerUsage.Reads.Contains(isa.RegisterInt, uint32(kept.Register)))

	outerUsage := stack.EndStatement(&outer)
	require.Equal(t, uint32(3), outerUsage.Peak[isa.RegisterInt], "an inner peak counts for the enclosing statement")
	require.True(t, outerUsage.Reads.Contains(isa.RegisterInt, uint32(kept.Register)))
}

func TestSpillLimitsApplyPerSpillableBank(t *testing.T) {
	t.Parallel()

	allocator := &RegisterAllocator{}
	var limits [isa.NumRegisterKinds]LoweredLimit
	limits[isa.RegisterInt] = LoweredLimit{Register: 100, Active: true}
	limits[isa.RegisterSliceInt] = LoweredLimit{Register: 10, Active: true}
	limits[isa.RegisterFloat] = LoweredLimit{Register: 0, Active: true}
	allocator.SetSpillLimits(limits)
	require.Zero(t, allocator.SpillLimit(isa.RegisterFloat), "a limit of register 0 spills every local")
	require.Equal(t, min(uint32(100), allocator.SpillLimit(isa.RegisterInt)), allocator.SpillLimit(isa.RegisterInt))
	require.LessOrEqual(t, allocator.SpillLimit(isa.RegisterInt), uint32(100))
	require.Equal(t, allocator.SpillLimit(isa.RegisterString), min(spillThreshold, allocator.SpillLimit(isa.RegisterString)))
	require.Equal(t, spillThreshold, allocator.SpillLimit(isa.RegisterSliceInt), "typed-slice banks keep the default")
}

func TestKeepLocationKeepsARegisterAcrossARestore(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	watermark := stack.Alloc.Snapshot()
	first := stack.Alloc.Alloc(isa.RegisterInt)
	second := stack.Alloc.Alloc(isa.RegisterInt)
	stack.RestoreWatermark(watermark)
	require.Equal(t, first, stack.Alloc.Alloc(isa.RegisterInt), "a restore frees temporaries")

	stack.RestoreWatermark(watermark)
	stack.KeepLocation(program.VarLocation{Register: second, Kind: isa.RegisterInt})
	require.Equal(t, second+1, stack.Alloc.Alloc(isa.RegisterInt), "a kept register stays allocated")

	stack.KeepLocation(program.VarLocation{IsSpilled: true, SpillSlot: 3, Kind: isa.RegisterInt})
	require.NoError(t, stack.OverflowError())
}

func TestDeclareParameterNeverSpills(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	for index := range maxRegisterIndex + 1 {
		location := stack.DeclareParameter("p"+string(rune('a'+index%26))+string(rune('a'+index/26)), isa.RegisterInt)
		require.False(t, location.IsSpilled, "parameter %d must stay in a register", index)
		require.Equal(t, uint8(index), location.Register)
	}
	require.NoError(t, stack.OverflowError())

	stack.DeclareParameter("overflowing", isa.RegisterInt)
	require.ErrorIs(t, stack.OverflowError(), fault.ErrRegisterOverflow)
}

func TestParseForcedSpillLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, uint32(16), parseForcedSpillLimit("16"))
	require.Zero(t, parseForcedSpillLimit(""))
	require.Zero(t, parseForcedSpillLimit("many"))
	require.Zero(t, parseForcedSpillLimit("246"), "a limit at or above the threshold changes nothing")
}

func TestRestoreWatermarkKeepsTheVariablesAfterTheyMove(t *testing.T) {
	t.Parallel()

	stack := NewScopeStack("run")
	stack.PushScope()
	watermark := stack.Alloc.Snapshot()
	first := stack.DeclareVar("first", isa.RegisterInt)
	second := stack.DeclareVar("second", isa.RegisterInt)
	stack.Alloc.Alloc(isa.RegisterInt)

	stack.RestoreWatermark(watermark)
	require.Equal(t, uint32(second.Register)+1, stack.Alloc.Snapshot()[isa.RegisterInt], "both variables stay allocated")

	require.True(t, stack.UpdateVar("second", program.VarLocation{Register: first.Register, Kind: isa.RegisterInt}))
	stack.RestoreWatermark(watermark)
	require.Equal(t, uint32(first.Register)+1, stack.Alloc.Snapshot()[isa.RegisterInt],
		"a variable that left the top register no longer holds it")

	stack.BindVar("third", program.VarLocation{Register: 9, Kind: isa.RegisterInt})
	stack.RestoreWatermark(watermark)
	require.Equal(t, uint32(10), stack.Alloc.Snapshot()[isa.RegisterInt], "a bound variable is kept too")
}
