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

func TestOnlyRolesThatNameNothingPerFunctionCopyAsTheyStand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role OperandRole
		want bool
	}{
		{name: "none", role: RoleNone, want: true},
		{name: "immediate", role: RoleImmediate, want: true},
		{name: "kind marker", role: RoleKindMarker, want: true},
		{name: "sub-opcode", role: roleSubOpcode, want: true},
		{name: "jump offset low byte", role: roleJumpOffsetLow, want: true},
		{name: "jump offset high byte", role: roleJumpOffsetHigh, want: true},
		{name: "int register", role: RoleRegInt, want: false},
		{name: "general register", role: RoleRegGeneral, want: false},
		{name: "byte slice register", role: roleRegSliceByte, want: false},
		{name: "dynamic register", role: RoleRegDynamic, want: false},
		{name: "constant index", role: roleConstIndex, want: false},
		{name: "field index", role: roleFieldIndex, want: false},
		{name: "type index", role: roleTypeIndex, want: false},
		{name: "call site low byte", role: roleCallSiteLow, want: false},
		{name: "call site high byte", role: roleCallSiteHigh, want: false},
		{name: "follows extension", role: roleFollowsExtension, want: false},
		{name: "unknown", role: roleUnknown, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, RoleMeansTheSameInEveryFunction(tt.role))
		})
	}
}
