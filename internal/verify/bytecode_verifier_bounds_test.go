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

func TestVerifyRegisterOperandBoundsExtensionWords(t *testing.T) {
	t.Parallel()

	const (
		intCount     = 4
		generalCount = 4
		outOfRange   = 9
	)
	deferWith := func(arguments ...isa.Instruction) []isa.Instruction {
		return append([]isa.Instruction{isa.NewInstruction(isa.OpDefer, 0, uint8(len(arguments)), 0)}, arguments...)
	}
	argument := func(register uint8, kind isa.RegisterKind) isa.Instruction {
		return isa.NewInstruction(isa.OpExt, 0, register, uint8(kind))
	}
	badInt := isa.NewInstruction(isa.OpAddInt, outOfRange, 0, 1)
	cases := []struct {
		name  string
		body  []isa.Instruction
		valid bool
	}{
		{
			name:  "a defer with no arguments does not hide the next instruction",
			body:  append(deferWith(), badInt),
			valid: false,
		},
		{
			name:  "a defer skips every argument word",
			body:  append(deferWith(argument(1, isa.RegisterInt), argument(2, isa.RegisterString)), isa.NewInstruction(isa.OpAddInt, 3, 0, 1)),
			valid: true,
		},
		{
			name:  "a deferred int argument beyond the frame is rejected",
			body:  deferWith(argument(1, isa.RegisterInt), argument(outOfRange, isa.RegisterInt)),
			valid: false,
		},
		{
			name:  "a deferred string argument is not checked against the int count",
			body:  deferWith(argument(outOfRange, isa.RegisterString)),
			valid: true,
		},
		{
			name:  "the instruction after an append is checked",
			body:  []isa.Instruction{isa.NewInstruction(isa.OpAppend, 1, 2, 3), badInt},
			valid: false,
		},
		{
			name:  "an append element beyond the frame is rejected",
			body:  []isa.Instruction{isa.NewInstruction(isa.OpAppend, 1, 2, outOfRange)},
			valid: false,
		},
		{
			name:  "a typed slice read with its int index beyond the frame is rejected",
			body:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceGetFloatDirect, 0, 0), isa.NewInstruction(isa.OpExt, outOfRange, 0, 0)},
			valid: false,
		},
		{
			name:  "a struct field layout index is not a register",
			body:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetStructFieldInt, 0, 1), isa.NewInstruction(isa.OpExt, outOfRange, outOfRange, 0)},
			valid: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Name: "f", Body: testCase.body}
			compiledFunction.NumRegisters[isa.RegisterInt] = intCount
			compiledFunction.NumRegisters[isa.RegisterGeneral] = generalCount
			compiledFunction.NumRegisters[isa.RegisterString] = 1
			compiledFunction.NumRegisters[isa.RegisterFloat] = 1
			compiledFunction.NumRegisters[isa.RegisterSliceFloat] = 1

			err := VerifyRegisterOperandBounds(compiledFunction)
			if testCase.valid {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrRegisterOperandOutOfRange)
		})
	}
}
