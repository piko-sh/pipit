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

	"pipit.sh/pipit/internal/isa"
)

func TestGeneratedDispatchReportsAnOpcodeWithNoSpecRow(t *testing.T) {
	t.Parallel()

	claimed := make(map[uint8]bool, len(isa.AllSpecs()))
	for _, row := range isa.AllSpecs() {
		if row.Tier == isa.TierMain {
			claimed[row.Code] = true
		}
	}

	unclaimed := -1
	for code := 255; code > 0; code-- {
		if !claimed[uint8(code)] {
			unclaimed = code
			break
		}
	}
	if unclaimed < 0 {
		t.Skip("the main tier is fully populated, so there is no unclaimed opcode to probe")
	}

	vm, frame, registers := newStandardVM(t)

	got := flatDispatchSwitch(vm, frame, registers, isa.Instruction{Op: isa.Opcode(unclaimed)})

	require.Equal(t, opPanicError, got)
	require.ErrorIs(t, vm.evalError, errInvalidOpcode,
		"an opcode with no specification row must be reported, not silently ignored")
}

func TestGeneratedDispatchReportsAnUnknownSubOpcodeAtEachTier(t *testing.T) {
	t.Parallel()

	claimed := map[isa.Tier]map[uint8]bool{
		isa.TierSub1: {}, isa.TierSub2: {}, isa.TierSub3: {},
	}
	for _, row := range isa.AllSpecs() {
		if codes, tracked := claimed[row.Tier]; tracked {
			codes[row.Code] = true
		}
	}

	tests := []struct {
		build func(code uint8) isa.Instruction
		name  string
		tier  isa.Tier
	}{
		{
			name: "the first tier", tier: isa.TierSub1,
			build: func(code uint8) isa.Instruction { return isa.NewTier1Instruction(isa.SubOpcode(code), 0, 0) },
		},
		{
			name: "the second tier", tier: isa.TierSub2,
			build: func(code uint8) isa.Instruction { return isa.NewTier2Instruction(isa.SubOpcodeTier2(code), 0) },
		},
		{
			name: "the third tier", tier: isa.TierSub3,
			build: func(code uint8) isa.Instruction { return isa.NewTier3Instruction(isa.SubOpcodeTier3(code)) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			unclaimed := -1
			for code := 255; code > 0; code-- {
				if !claimed[tt.tier][uint8(code)] {
					unclaimed = code
					break
				}
			}
			if unclaimed < 0 {
				t.Skipf("%s is fully populated, so there is no unclaimed sub-opcode to probe", tt.name)
			}

			vm, frame, registers := newStandardVM(t)

			got := flatDispatchSwitch(vm, frame, registers, tt.build(uint8(unclaimed)))

			require.Equal(t, opPanicError, got)
			require.Error(t, vm.evalError,
				"an unregistered sub-opcode must surface an encoding error rather than fall through")
		})
	}
}
