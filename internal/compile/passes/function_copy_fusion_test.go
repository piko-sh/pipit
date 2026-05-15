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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	copyFusionTemp        = 3
	copyFusionSource      = 1
	copyFusionDestination = 2
)

func copyFusionFunction(tail ...isa.Instruction) *program.CompiledFunction {
	body := make([]isa.Instruction, 0, 2+len(tail))
	body = append(body,
		mk(isa.OpGetStructFieldGeneral, copyFusionTemp, copyFusionSource, 0),
		mk(isa.OpSetStructFieldGeneral, copyFusionDestination, copyFusionTemp, 0))
	body = append(body, tail...)
	return &program.CompiledFunction{
		Body:              body,
		StructLayoutTable: []program.StructFieldLayout{{Kind: uint8(reflect.Pointer)}},
	}
}

func fuseCopyAtStart(compiledFunction *program.CompiledFunction) bool {
	body := compiledFunction.Body
	return FuseCopyStructFieldGeneralT0(compiledFunction, body, 0, len(body), BuildAllJumpTargets(body))
}

func TestFuseCopyStructFieldFollowsFusedBranches(t *testing.T) {
	t.Parallel()

	writeTemp := mk(isa.OpLoadGeneralConst, copyFusionTemp, 0, 0)
	readTemp := mk(isa.OpMoveGeneral, 9, copyFusionTemp, 0)
	ret := isa.NewTier2Instruction(isa.SubOpTier2Return, 0)

	cases := []struct {
		name  string
		tail  []isa.Instruction
		fused bool
	}{
		{
			name:  "both edges of a fused compare write the temporary",
			tail:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1), fusedExt(1), writeTemp, ret},
			fused: true,
		},
		{
			name:  "the taken edge of a fused compare reads the temporary",
			tail:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1), fusedExt(1), writeTemp, readTemp, ret},
			fused: false,
		},
		{
			name:  "the fall-through of a fused compare reads the temporary",
			tail:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 0, 1), fusedExt(1), readTemp, writeTemp, ret},
			fused: false,
		},
		{
			name:  "the type-switch dispatch has targets the scan cannot follow",
			tail:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpTypeSwitchJump, 5, 6), wordJumpRow(1), isa.NewTier1Instruction(isa.SubOpJump, 0, 0), writeTemp, ret},
			fused: false,
		},
		{
			name: "a uint fusion's padding is stepped over before the fall-through, and its target reads",
			tail: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 7), fusedExt(1), mk(isa.OpNop, 0, 0, 0),
				writeTemp,
				readTemp,
				ret,
			},
			fused: false,
		},
		{
			name: "a uint fusion's padding is stepped over before the fall-through, and its target writes",
			tail: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 7), fusedExt(1), mk(isa.OpNop, 0, 0, 0),
				writeTemp,
				writeTemp,
				ret,
			},
			fused: true,
		},
		{
			name:  "a tier-2 panic reads the temporary",
			tail:  []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2Panic, copyFusionTemp), ret},
			fused: false,
		},
		{
			name:  "a return ends the scan with a non-result temporary dead",
			tail:  []isa.Instruction{ret},
			fused: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := copyFusionFunction(testCase.tail...)
			require.Equal(t, testCase.fused, fuseCopyAtStart(compiledFunction))
			if testCase.fused {
				require.Equal(t, isa.OpCopyStructFieldGeneralT0, compiledFunction.Body[0].Op)
				require.Equal(t, isa.OpExt, compiledFunction.Body[1].Op)
			} else {
				require.Equal(t, isa.OpGetStructFieldGeneral, compiledFunction.Body[0].Op, "a refused fusion leaves the pair alone")
			}
		})
	}
}

func TestFuseCopyStructFieldRefusesWhenTemporaryIsAResultSlot(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Body: []isa.Instruction{
			mk(isa.OpGetStructFieldGeneral, 0, copyFusionSource, 0),
			mk(isa.OpSetStructFieldGeneral, copyFusionDestination, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		StructLayoutTable: []program.StructFieldLayout{{Kind: uint8(reflect.Pointer)}},
		ResultKinds:       []isa.RegisterKind{isa.RegisterGeneral},
	}

	require.False(t, fuseCopyAtStart(compiledFunction))
}

func wordJumpRow(offset int16) isa.Instruction {
	lo, hi := jumpOffset(offset)
	return mk(isa.OpTypeSwitchCase, 0, lo, hi)
}
