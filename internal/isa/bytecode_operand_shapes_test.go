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
		{opcode: OpAddInt, aRole: RoleRegInt, writesA: true},
		{opcode: OpSubInt, aRole: RoleRegInt, writesA: true},
		{opcode: OpMulInt, aRole: RoleRegInt, writesA: true},
		{opcode: OpSliceGetInt, aRole: RoleRegInt, writesA: true},
		{opcode: OpSliceGetFloat, aRole: RoleRegFloat, writesA: true},
		{opcode: OpSliceGetString, aRole: RoleRegString, writesA: true},
		{opcode: OpSliceGetBool, aRole: RoleRegBool, writesA: true},
		{opcode: OpSliceGetUint, aRole: RoleRegUint, writesA: true},
		{opcode: OpGetFieldInt, aRole: RoleRegInt, writesA: true},
		{opcode: OpSetFieldInt, aRole: RoleRegGeneral, writesA: false},
		{opcode: OpPackInterface, aRole: RoleRegGeneral, writesA: true},
		{opcode: OpUnpackInterface, aRole: RoleRegDynamic, writesA: true},
		{opcode: OpLoadIntConst, aRole: RoleRegInt, writesA: true},
		{opcode: OpLoadFloatConst, aRole: RoleRegFloat, writesA: true},
		{opcode: OpLoadStringConst, aRole: RoleRegString, writesA: true},
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
		{name: "uint add-constant carries its pool index", instr: NewTier1Instruction(SubOpAddUintConst, 1, 2)},
		{name: "uint subtract-constant carries its pool index", instr: NewTier1Instruction(SubOpSubUintConst, 1, 2)},
		{name: "uint and-constant carries its pool index", instr: NewTier1Instruction(SubOpBitAndUintConst, 1, 2)},
		{name: "interface method expression carries type and name indices", instr: NewTier2Instruction(SubOpTier2MakeInterfaceMethodExpr, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.NotZero(t, ShapeForInstruction(tt.instr).Flags&ShapeFlagFollowsExtension)
		})
	}
}

func TestMapIndexOkBranchesAreControlFlow(t *testing.T) {
	t.Parallel()

	for _, op := range []Opcode{
		OpMapIndexOkJumpIfFalseIntInt, OpMapIndexOkJumpIfFalseStringInt, OpMapIndexOkJumpIfFalseStringString,
		OpMapIndexOkJumpIfFalseIntString, OpMapIndexOkJumpIfFalseIntGeneral, OpMapIndexOkJumpIfFalseStringGeneral,
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

	for _, op := range []Opcode{OpSliceGetInt, OpSliceGetFloat, OpSliceGetString, OpSliceGetBool, OpSliceGetUint} {
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

func TestKindForRoleRoundTrip(t *testing.T) {
	t.Parallel()

	for kindIndex := range NumRegisterKinds {
		kind := RegisterKind(kindIndex)
		t.Run(kind.String(), func(t *testing.T) {
			t.Parallel()
			role := RoleForKind(kind)
			got, ok := KindForRole(role)
			require.Truef(t, ok, "KindForRole(%v) is register-shaped", role)
			require.Equalf(t, kind, got,
				"RoleForKind gave %v, which maps back to another bank. A bank with no role of its own "+
					"falls through RoleForKind's default and silently coerces to the general bank.", role)
		})
	}
}

func TestKindForRoleNonRegisterRolesAreOpaque(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role OperandRole
	}{
		{name: "none", role: RoleNone},
		{name: "constant index", role: roleConstIndex},
		{name: "field index", role: roleFieldIndex},
		{name: "immediate", role: RoleImmediate},
		{name: "type index", role: roleTypeIndex},
		{name: "kind marker", role: RoleKindMarker},
		{name: "jump offset low byte", role: roleJumpOffsetLow},
		{name: "jump offset high byte", role: roleJumpOffsetHigh},
		{name: "call site low byte", role: roleCallSiteLow},
		{name: "call site high byte", role: roleCallSiteHigh},
		{name: "follows extension", role: roleFollowsExtension},
		{name: "unknown", role: roleUnknown},
		{name: "dynamic register", role: RoleRegDynamic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := KindForRole(tt.role)
			require.False(t, ok, "the role is not register-shaped")
		})
	}
}

func TestIncrementShapesReadTheirRegister(t *testing.T) {
	t.Parallel()
	for _, op := range []SubOpcodeTier2{SubOpTier2IncInt, SubOpTier2DecInt, SubOpTier2IncUint, SubOpTier2DecUint} {
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
	for _, op := range []SubOpcode{SubOpSimdDotProductFloat64, SubOpSimdSumSliceFloat64} {
		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()
			shape := ShapeForInstruction(NewTier1Instruction(op, 4, 5))
			require.True(t, shape.Reads[1], "the kernel adds into the register it names in B")
			require.True(t, shape.Writes[1], "the kernel writes the register it names in B")
		})
	}
}
