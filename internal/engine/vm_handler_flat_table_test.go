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
	"math/bits"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestFlatDispatchIndexPartitionsTheTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instruction isa.Instruction
		name        string
		want        uint
	}{
		{name: "a main-tier opcode indexes by its opcode", instruction: isa.NewInstruction(isa.OpAddInt, 1, 2, 3), want: uint(isa.OpAddInt)},
		{
			name:        "a first-tier sub-opcode indexes above the first base",
			instruction: isa.NewTier1Instruction(isa.SubOpNegInt, 1, 2),
			want:        flatHandlerTier1Base + uint(isa.SubOpNegInt),
		},
		{
			name:        "a second-tier sub-opcode indexes above the second base",
			instruction: isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
			want:        flatHandlerTier2Base + uint(isa.SubOpTier2Return),
		},
		{
			name:        "a third-tier sub-opcode indexes above the third base",
			instruction: isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
			want:        flatHandlerTier3Base + uint(isa.SubOpTier3ReturnVoid),
		},
		{
			name:        "the all-zero word lands on the third base",
			instruction: isa.Instruction{},
			want:        flatHandlerTier3Base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, flatDispatchIndex(tt.instruction))
		})
	}
}

func TestFlatDispatchIndexAgreesWithItsBitTwiddlingVariant(t *testing.T) {
	t.Parallel()

	t.Run("across every instruction in the specification", func(t *testing.T) {
		t.Parallel()
		for _, row := range isa.AllSpecs() {
			var instruction isa.Instruction
			switch row.Tier {
			case isa.TierMain:
				instruction = isa.NewInstruction(isa.Opcode(row.Code), 1, 2, 3)
			case isa.TierSub1:
				instruction = isa.NewTier1Instruction(isa.SubOpcode(row.Code), 2, 3)
			case isa.TierSub2:
				instruction = isa.NewTier2Instruction(isa.SubOpcodeTier2(row.Code), 3)
			case isa.TierSub3:
				instruction = isa.NewTier3Instruction(isa.SubOpcodeTier3(row.Code))
			}
			require.Equalf(t, flatDispatchIndex(instruction), flatDispatchIndexTZCNT(instruction),
				"the two dispatch-index forms must agree for %s", row.Name)
		}
	})

	t.Run("the two forms agree on the all-zero word", func(t *testing.T) {
		t.Parallel()
		zero := isa.Instruction{}

		require.Equal(t, uint(flatHandlerTier3Base), flatDispatchIndex(zero),
			"the all-zero word is classified as a third-tier sub-op, where an invalid-sub-op handler reports it")
		require.Equal(t, flatDispatchIndex(zero), flatDispatchIndexTZCNT(zero),
			"the benchmark variant must decode the all-zero word the same way, so promoting it cannot change how a padding word dispatches")
	})
}

func TestFlatDispatchRoutesToTheHandlerTheOpcodeNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		build func() isa.Instruction
		seed  func(registers *Registers)
		check func(t *testing.T, registers *Registers)
		name  string
	}{
		{
			name:  "integer addition",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpAddInt, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Ints[1], registers.Ints[2] = 10, 32 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(42), registers.Ints[0]) },
		},
		{
			name:  "unsigned addition",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpAddUint, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Uints[1], registers.Uints[2] = 10, 32 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(42), registers.Uints[0]) },
		},
		{
			name:  "float multiplication",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpMulFloat, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Floats[1], registers.Floats[2] = 1.5, 4 },
			check: func(t *testing.T, registers *Registers) { require.InDelta(t, 6, registers.Floats[0], 0) },
		},
		{
			name:  "string equality",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpEqString, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Strings[1], registers.Strings[2] = "a", "a" },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(1), registers.Ints[0]) },
		},
		{
			name:  "integer comparison",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpLtInt, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Ints[1], registers.Ints[2] = 1, 2 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(1), registers.Ints[0]) },
		},
		{
			name:  "bitwise exclusive or",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpBitXor, 0, 1, 2) },
			seed:  func(registers *Registers) { registers.Ints[1], registers.Ints[2] = 0b1100, 0b1010 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(0b0110), registers.Ints[0]) },
		},
		{
			name:  "a first-tier integer negation",
			build: func() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpNegInt, 0, 1) },
			seed:  func(registers *Registers) { registers.Ints[1] = 42 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(-42), registers.Ints[0]) },
		},
		{
			name:  "a first-tier unsigned move",
			build: func() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpMoveUint, 0, 1) },
			seed:  func(registers *Registers) { registers.Uints[1] = 42 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, uint64(42), registers.Uints[0]) },
		},
		{
			name:  "a first-tier bitwise complement",
			build: func() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpBitNotUint, 0, 1) },
			seed:  func(registers *Registers) { registers.Uints[1] = 0 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, ^uint64(0), registers.Uints[0]) },
		},
		{
			name:  "a first-tier string length",
			build: func() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpLenString, 0, 1) },
			seed:  func(registers *Registers) { registers.Strings[1] = "pipit" },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(5), registers.Ints[0]) },
		},
		{
			name:  "a first-tier logical negation",
			build: func() isa.Instruction { return isa.NewTier1Instruction(isa.SubOpNot, 0, 1) },
			seed:  func(registers *Registers) { registers.Ints[1] = 0 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(1), registers.Ints[0]) },
		},
		{
			name:  "an extension word is inert",
			build: func() isa.Instruction { return isa.NewInstruction(isa.OpExt, 1, 2, 3) },
			seed:  func(registers *Registers) { registers.Ints[0] = 7 },
			check: func(t *testing.T, registers *Registers) { require.Equal(t, int64(7), registers.Ints[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			vm, frame, registers := newStandardVM(t)
			tt.seed(registers)

			got := flatDispatchSwitch(vm, frame, registers, tt.build())

			require.Equal(t, opContinue, got, "the dispatcher must reach a handler that continues")
			tt.check(t, registers)
		})
	}
}

func TestFlatDispatchRejectsAnUnknownOpcode(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newStandardVM(t)

	got := flatDispatchSwitch(vm, frame, registers, isa.Instruction{Op: isa.Opcode(254)})

	require.NotEqual(t, opContinue, got, "an opcode with no handler must not be silently ignored")
	require.Error(t, vm.evalError)
}

const (
	bitsPerByteShift  = 3
	flatTierByteShift = 8
)

func flatDispatchIndexTZCNT(instr isa.Instruction) uint {
	word := *(*uint32)(unsafe.Pointer(&instr))
	if word == 0 {
		return flatHandlerTier3Base
	}
	tzcnt := bits.TrailingZeros32(word)
	tier := uint(tzcnt) >> bitsPerByteShift
	subop := uint(byte(word >> (tier << bitsPerByteShift)))
	return tier<<flatTierByteShift | subop
}
