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

package verify

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestVerifyRegisterOperandBoundsAcceptsIIFESyncWithNoGeneralRegisters(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Name: "f",
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpLoadIntConstSmall, 0, 1),
			isa.NewTier1Instruction(isa.SubOpCallIIFE, 0, 0),
			isa.NewTier3Instruction(isa.SubOpTier3SyncIIFEUpvalues),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
	}
	compiledFunction.NumRegisters[isa.RegisterInt] = 2

	require.NoError(t, VerifyRegisterOperandBounds(compiledFunction))
}

func TestVerifyRegisterOperandBoundsChecksClosureSyncRegister(t *testing.T) {
	t.Parallel()

	compiledFunction := &program.CompiledFunction{
		Name: "f",
		Body: []isa.Instruction{
			isa.NewTier2Instruction(isa.SubOpTier2SyncClosureUpvalues, 3),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
	}
	compiledFunction.NumRegisters[isa.RegisterGeneral] = 2

	require.ErrorIs(t, VerifyRegisterOperandBounds(compiledFunction), ErrRegisterOperandOutOfRange)
}
