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
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const (
	uint32Left        = 1
	uint32Right       = 2
	uint32Destination = 3
	uint32Untouched   = 4
)

type uint32Operation struct {
	want        func(left, right uint64) uint64
	name        string
	instruction isa.Instruction
	constant    bool
}

func uint32Operations() []uint32Operation {
	narrow := func(value uint64) uint64 { return value & math.MaxUint32 }
	return []uint32Operation{
		{name: "subtract", instruction: isa.NewInstruction(isa.OpSubtractUint32, uint32Destination, uint32Left, uint32Right),
			want: func(left, right uint64) uint64 { return narrow(left - right) }},
		{name: "multiply", instruction: isa.NewInstruction(isa.OpMultiplyUint32, uint32Destination, uint32Left, uint32Right),
			want: func(left, right uint64) uint64 { return narrow(left * right) }},
		{name: "add a constant", instruction: isa.NewTier1Instruction(isa.SubOpAddUint32Constant, uint32Destination, uint32Left), constant: true,
			want: func(left, right uint64) uint64 { return narrow(left + right) }},
		{name: "subtract a constant", instruction: isa.NewTier1Instruction(isa.SubOpSubtractUint32Constant, uint32Destination, uint32Left), constant: true,
			want: func(left, right uint64) uint64 { return narrow(left - right) }},
	}
}

func runUint32Operation(t *testing.T, operation uint32Operation, left, right uint64, assembly bool) uint64 {
	t.Helper()
	const constantIndex = 300
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.body = append(builder.body, operation.instruction)
	if operation.constant {
		builder.uintConstants = make([]uint64, constantIndex+1)
		builder.uintConstants[constantIndex] = right
		builder.body = append(builder.body, isa.NewInstruction(isa.OpExtension, constantIndex&0xFF, constantIndex>>8, 0))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	vm, frame, registers := newFramedVM(t, builder.build())
	for register := range registers.Uints {
		registers.Uints[register] = math.MaxUint64 - 7
	}
	registers.Uints[uint32Left], registers.Uints[uint32Right] = left, right
	runOneInstruction(t, vm, frame, registers, operation.instruction, assembly)
	require.NoError(t, vm.evalError)
	require.Equal(t, uint64(math.MaxUint64-7), registers.Uints[uint32Untouched], "a register the operation does not name is left alone")
	return registers.Uints[uint32Destination]
}

func TestUnsigned32BitArithmeticKeepsTheLow32Bits(t *testing.T) {
	values := []uint64{0, 1, 2, 0xFFFF, 16777619, math.MaxUint32, math.MaxUint32 + 1, 1 << 40, 0x1234_5678_9ABC_DEF0, math.MaxUint64}
	for _, operation := range uint32Operations() {
		t.Run(operation.name, func(t *testing.T) {
			tier, code := isa.TierMain, uint8(operation.instruction.Op)
			if operation.constant {
				tier, code = isa.TierSub1, operation.instruction.A
			}
			spec, known := isa.SpecAt(tier, code)
			require.True(t, known)
			require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the operation has an assembly body")
			for _, left := range values {
				for _, right := range values {
					for _, assembly := range dispatchRoutes() {
						require.Equalf(t, operation.want(left, right), runUint32Operation(t, operation, left, right, assembly),
							"%s, %d and %d", routeName(assembly), left, right)
					}
				}
			}
		})
	}
}
