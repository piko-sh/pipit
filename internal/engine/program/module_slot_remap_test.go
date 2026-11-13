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

package program

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func TestGlobalOperandExtentSpansTheSlotsEveryFunctionNames(t *testing.T) {
	t.Parallel()

	nested := &CompiledFunction{Name: "nested", Body: []isa.Instruction{
		{Op: isa.OpGetGlobal, A: 0, B: 9, C: uint8(isa.RegisterInt64)},
		isa.NewTier1Instruction(isa.SubOpSetGlobalWide, 0, uint8(isa.RegisterString)),
		{Op: 0, A: 0x2c, B: 0x01, C: 0},
	}}
	root := &CompiledFunction{Name: "root", Functions: []*CompiledFunction{nested, nested}, Body: []isa.Instruction{
		{Op: isa.OpSetGlobal, A: 0, B: 7, C: uint8(isa.RegisterInt64)},
		{Op: isa.OpSetGlobal, A: 0, B: 4, C: uint8(isa.RegisterGeneral)},
	}}
	initialiser := &CompiledFunction{Name: "<varinit>", Body: []isa.Instruction{
		{Op: isa.OpSetGlobal, A: 0, B: 12, C: uint8(isa.RegisterInt64)},
	}}

	low, count, err := GlobalOperandExtent(root, initialiser, nil)
	require.NoError(t, err)
	require.Equal(t, uint16(7), low[isa.RegisterInt64])
	require.Equal(t, uint16(6), count[isa.RegisterInt64], "slots 7 to 12")
	require.Equal(t, uint16(0x12c), low[isa.RegisterString], "a wide operand's slot comes from its extension word")
	require.Equal(t, uint16(1), count[isa.RegisterString])
	require.Equal(t, uint16(4), low[isa.RegisterGeneral])
	require.Equal(t, uint16(1), count[isa.RegisterGeneral])
	require.Zero(t, count[isa.RegisterFloat64], "a kind with no operand has no extent")
}

func TestGlobalOperandExtentRefusesMalformedOperands(t *testing.T) {
	t.Parallel()

	_, _, err := GlobalOperandExtent(&CompiledFunction{Name: "kind", Body: []isa.Instruction{
		{Op: isa.OpGetGlobal, A: 0, B: 1, C: uint8(NumGlobalRegisterKinds)},
	}})
	require.ErrorContains(t, err, "out of range")

	_, _, err = GlobalOperandExtent(&CompiledFunction{Name: "wide", Body: []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 0, uint8(isa.RegisterInt64)),
	}})
	require.ErrorContains(t, err, "missing extension word")
}

func TestGlobalOperandExtentRefusesAnExtentWiderThanASlotCount(t *testing.T) {
	t.Parallel()

	_, _, err := GlobalOperandExtent(&CompiledFunction{Name: "full", Body: []isa.Instruction{
		{Op: isa.OpGetGlobal, A: 0, B: 0, C: uint8(isa.RegisterInt64)},
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 0, uint8(isa.RegisterInt64)),
		{A: 0xFF, B: 0xFF},
	}})
	require.ErrorIs(t, err, fault.ErrGlobalSlotsExhausted, "slots 0 to 65535 are 65536 slots, which a uint16 count cannot hold")
	require.ErrorIs(t, err, fault.ErrBytecodeVerification)

	_, _, err = GlobalOperandExtent(&CompiledFunction{Name: "last", Body: []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 0, uint8(isa.RegisterString)),
		{A: 0xFF, B: 0xFF},
	}})
	require.ErrorIs(t, err, fault.ErrBytecodeVerification, "a bank holding slot 65535 would need 65536 slots")

	low, count, err := GlobalOperandExtent(&CompiledFunction{Name: "widest", Body: []isa.Instruction{
		{Op: isa.OpGetGlobal, A: 0, B: 0, C: uint8(isa.RegisterInt64)},
		isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 0, uint8(isa.RegisterInt64)),
		{A: 0xFE, B: 0xFF},
	}})
	require.NoError(t, err)
	require.Equal(t, uint16(0), low[isa.RegisterInt64])
	require.Equal(t, uint16(math.MaxUint16), count[isa.RegisterInt64], "the widest extent a count holds")
}
