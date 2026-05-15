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
		require.Contains(t, err.Error(), kind.String())
	}
}
