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

	type op struct {
		Opcode
		aRole   OperandRole
		writesA bool
	}
	critical := []op{
		{Opcode: OpAddInt, aRole: RoleRegInt, writesA: true},
		{Opcode: OpSubInt, aRole: RoleRegInt, writesA: true},
		{Opcode: OpMulInt, aRole: RoleRegInt, writesA: true},
		{Opcode: OpSliceGetInt, aRole: RoleRegInt, writesA: true},
		{Opcode: OpSliceGetFloat, aRole: RoleRegFloat, writesA: true},
		{Opcode: OpSliceGetString, aRole: RoleRegString, writesA: true},
		{Opcode: OpSliceGetBool, aRole: RoleRegBool, writesA: true},
		{Opcode: OpSliceGetUint, aRole: RoleRegUint, writesA: true},
		{Opcode: OpGetFieldInt, aRole: RoleRegInt, writesA: true},
		{Opcode: OpSetFieldInt, aRole: RoleRegGeneral, writesA: false},
		{Opcode: OpPackInterface, aRole: RoleRegGeneral, writesA: true},
		{Opcode: OpUnpackInterface, aRole: RoleRegDynamic, writesA: true},
		{Opcode: OpLoadIntConst, aRole: RoleRegInt, writesA: true},
		{Opcode: OpLoadFloatConst, aRole: RoleRegFloat, writesA: true},
		{Opcode: OpLoadStringConst, aRole: RoleRegString, writesA: true},
	}

	for _, tc := range critical {
		shape := OperandShapeFor(tc.Opcode)
		require.NotZero(t, shape.Flags&ShapeFlagDescribed,
			"opcode %s lacks shapeFlagDescribed", tc.Opcode)
		require.Equal(t, tc.aRole, shape.A,
			"opcode %s expected role A %v, got %v", tc.Opcode, tc.aRole, shape.A)
		require.Equal(t, tc.writesA, shape.Writes[0],
			"opcode %s writes-A flag mismatch", tc.Opcode)
	}
}

func TestOperandShapeReadKindsForTypedSliceGet(t *testing.T) {
	t.Parallel()

	cases := []Opcode{
		OpSliceGetInt,
		OpSliceGetFloat,
		OpSliceGetString,
		OpSliceGetBool,
		OpSliceGetUint,
	}
	for _, op := range cases {
		shape := OperandShapeFor(op)
		require.Equal(t, RoleRegGeneral, shape.B, "opcode %s operand B", op)
		require.True(t, shape.Reads[1], "opcode %s reads operand B", op)
		require.Equal(t, RoleRegInt, shape.C, "opcode %s operand C", op)
		require.True(t, shape.Reads[2], "opcode %s reads operand C", op)
	}
}

func TestKindForRoleRoundTrip(t *testing.T) {
	t.Parallel()

	for kindIndex := range NumRegisterKinds {
		kind := RegisterKind(kindIndex)
		role := RoleForKind(kind)
		got, ok := KindForRole(role)
		require.Truef(t, ok, "KindForRole(%v) should be register-shaped", role)
		require.Equalf(t, kind, got,
			"round-trip mismatch for %v: RoleForKind gave %v, which maps back to %v.\n"+
				"A bank with no role of its own falls through RoleForKind's default and\n"+
				"silently coerces to the general bank.", kind, role, got)
	}
}

func TestKindForRoleNonRegisterRolesAreOpaque(t *testing.T) {
	t.Parallel()

	nonRegister := []OperandRole{
		RoleNone, roleConstIndex, roleFieldIndex, RoleImmediate,
		roleTypeIndex, RoleKindMarker, roleJumpOffsetLow, roleJumpOffsetHigh,
		roleCallSiteLow, roleCallSiteHigh, roleFollowsExtension, roleUnknown,
		RoleRegDynamic,
	}
	for _, role := range nonRegister {
		_, ok := KindForRole(role)
		require.False(t, ok, "role %v should not be register-shaped", role)
	}
}
