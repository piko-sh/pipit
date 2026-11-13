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

package isa

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperandShapesDescribeEveryCriticalOpcode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		opcode  Opcode
		aRole   OperandRole
		writesA bool
	}{
		{opcode: OpAddInt64, aRole: RoleRegInt, writesA: true},
		{opcode: OpSubtractInt64, aRole: RoleRegInt, writesA: true},
		{opcode: OpMultiplyInt64, aRole: RoleRegInt, writesA: true},
		{opcode: OpGetElementInt64, aRole: RoleRegInt, writesA: true},
		{opcode: OpGetElementFloat64, aRole: RoleRegFloat, writesA: true},
		{opcode: OpGetElementString, aRole: RoleRegString, writesA: true},
		{opcode: OpGetElementBool, aRole: RoleRegBool, writesA: true},
		{opcode: OpGetElementUint64, aRole: RoleRegUint, writesA: true},
		{opcode: OpGetFieldInt64, aRole: RoleRegInt, writesA: true},
		{opcode: OpSetFieldInt64, aRole: RoleRegGeneral, writesA: false},
		{opcode: OpBoxAsInterface, aRole: RoleRegGeneral, writesA: true},
		{opcode: OpUnboxFromInterface, aRole: RoleRegDynamic, writesA: true},
		{opcode: OpLoadInt64Constant, aRole: RoleRegInt, writesA: true},
		{opcode: OpLoadFloat64Constant, aRole: RoleRegFloat, writesA: true},
		{opcode: OpLoadStringConstant, aRole: RoleRegString, writesA: true},
	}
	for _, tt := range tests {
		t.Run(tt.opcode.String(), func(t *testing.T) {
			t.Parallel()
			shape := OperandShapeFor(tt.opcode)
			require.NotZero(t, shape.Flags&ShapeFlagDescribed, "the shape is described")
			require.Equal(t, tt.aRole, shape.A, "role of operand A")
			require.Equal(t, tt.writesA, shape.Writes[0], "whether operand A is written")
		})
	}
}

func TestAppendReadsItsElementAndCarriesNoExtensionWord(t *testing.T) {
	t.Parallel()

	appendShape := OperandShapeFor(OpAppend)
	require.Equal(t, RoleRegGeneral, appendShape.C, "the handler reads general[C] as the element")
	require.Equal(t, [NumInstructionOperands]bool{false, true, true}, appendShape.Reads)
	require.Zero(t, appendShape.Flags&ShapeFlagFollowsExtension, "append is a single word")

	spreadShape := OperandShapeFor(OpAppendSpread)
	require.Zero(t, spreadShape.Flags&ShapeFlagFollowsExtension, "append-spread is a single word")
}

func TestOperationsWithExtensionWordsDeclareThem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		instr Instruction
	}{
		{name: "defer carries one word per argument", instr: NewInstruction(OpDefer, 1, 2, 0)},
		{name: "go carries one word per argument", instr: NewInstruction(OpGo, 1, 2, 0)},
		{name: "uint add-constant carries its pool index", instr: NewTier1Instruction(SubOpAddUint64Constant, 1, 2)},
		{name: "uint subtract-constant carries its pool index", instr: NewTier1Instruction(SubOpSubtractUint64Constant, 1, 2)},
		{name: "uint and-constant carries its pool index", instr: NewTier1Instruction(SubOpBitAndUint64Constant, 1, 2)},
		{name: "interface method expression carries type and name indices", instr: NewTier2Instruction(SubOpTier2MakeInterfaceMethodExpression, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotZero(t, ShapeForInstruction(tt.instr).Flags&ShapeFlagFollowsExtension)
		})
	}
}

func TestGetMapOkBranchesAreControlFlow(t *testing.T) {
	t.Parallel()

	for _, op := range []Opcode{
		OpGetMapInt64Int64OkThenJumpIfFalse, OpGetMapStringInt64OkThenJumpIfFalse, OpGetMapStringStringOkThenJumpIfFalse,
		OpGetMapInt64StringOkThenJumpIfFalse, OpGetMapInt64GeneralOkThenJumpIfFalse, OpGetMapStringGeneralOkThenJumpIfFalse,
	} {
		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()
			shape := OperandShapeFor(op)
			require.NotZero(t, shape.Flags&ShapeFlagControlFlow, "the operation branches on a missing key")
			require.NotZero(t, shape.Flags&ShapeFlagOpaqueWrites, "the operation writes the ok register its extension word names")
		})
	}
}

func TestOperandShapeReadKindsForTypedSliceGet(t *testing.T) {
	t.Parallel()

	for _, op := range []Opcode{OpGetElementInt64, OpGetElementFloat64, OpGetElementString, OpGetElementBool, OpGetElementUint64} {
		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()
			shape := OperandShapeFor(op)
			require.Equal(t, RoleRegGeneral, shape.B, "operand B holds the slice")
			require.True(t, shape.Reads[1], "operand B is read")
			require.Equal(t, RoleRegInt, shape.C, "operand C holds the index")
			require.True(t, shape.Reads[2], "operand C is read")
		})
	}
}

func TestIncrementShapesReadTheirRegister(t *testing.T) {
	t.Parallel()
	for _, op := range []SubOpcodeTier2{SubOpTier2IncrementInt64, SubOpTier2DecrementInt64, SubOpTier2IncrementUint64, SubOpTier2DecrementUint64} {
		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()
			shape := ShapeForInstruction(NewTier2Instruction(op, 4))
			require.True(t, shape.Reads[2], "the operation reads the register it updates")
			require.True(t, shape.Writes[2], "the operation writes the register it updates")
		})
	}
}

func TestAccumulatingKernelsReadTheirDestination(t *testing.T) {
	t.Parallel()
	for _, op := range []SubOpcode{SubOpSimdDotProductFloat64, SubOpSimdSumFloat64} {
		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()
			shape := ShapeForInstruction(NewTier1Instruction(op, 4, 5))
			require.True(t, shape.Reads[1], "the kernel adds into the register it names in B")
			require.True(t, shape.Writes[1], "the kernel writes the register it names in B")
		})
	}
}

func TestScalarConstantOperandNamesTheIndexAndItsPool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		instruction Instruction
		index       int
		kind        RegisterKind
		carries     bool
	}{
		{
			name:        "a wide index loaded into its register's pool",
			instruction: NewInstruction(OpLoadStringConstant, 3, 0x34, 0x12),
			index:       0x1234,
			kind:        RegisterString,
			carries:     true,
		},
		{
			name:        "a narrow index in the pool of the register it is combined with",
			instruction: NewInstruction(OpAddInt64Constant, 1, 2, 9),
			index:       9,
			kind:        RegisterInt64,
			carries:     true,
		},
		{
			name:        "no constant index",
			instruction: NewInstruction(OpAddInt64, 1, 2, 3),
		},
		{
			name:        "a closure's index names no scalar pool",
			instruction: NewInstruction(OpMakeClosure, 1, 2, 3),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			index, kind, carries := ScalarConstantOperand(test.instruction)
			require.Equal(t, test.carries, carries)
			require.Equal(t, test.index, index)
			require.Equal(t, test.kind, kind)
		})
	}
}

func TestReadsExtensionWordExceptsOnlyDeferAndGo(t *testing.T) {
	t.Parallel()

	require.True(t, ReadsExtensionWord(NewTier2Instruction(SubOpTier2MakeMap, 0)),
		"a map's construction reads its type from the word after it")
	require.False(t, ReadsExtensionWord(NewInstruction(OpAddInt64, 1, 2, 3)),
		"an instruction without extension words reads none")
	for _, op := range []Opcode{OpDefer, OpGo} {
		instruction := NewInstruction(op, 0, 0, 0)
		require.NotZero(t, ShapeForInstruction(instruction).Flags&ShapeFlagFollowsExtension)
		require.False(t, ReadsExtensionWord(instruction), "%s carries one word per argument, and may have none", op)
	}
}
