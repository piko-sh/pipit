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

// KindForRole returns the RegisterKind expected by a register-shaped operand role.
//
// Takes role (OperandRole) which is the operand's classification.
//
// Returns RegisterKind which is the register bank kind, or zero.
// Returns bool which is true when the role is register-shaped.
func KindForRole(role OperandRole) (RegisterKind, bool) {
	switch role {
	case RoleRegInt:
		return RegisterInt64, true
	case RoleRegFloat:
		return RegisterFloat64, true
	case RoleRegString:
		return RegisterString, true
	case RoleRegBool:
		return RegisterBool, true
	case RoleRegUint:
		return RegisterUint64, true
	case RoleRegComplex:
		return RegisterComplex128, true
	case RoleRegGeneral:
		return RegisterGeneral, true
	case RoleRegSliceInt:
		return RegisterSliceInt64, true
	case roleRegSliceFloat:
		return RegisterSliceFloat64, true
	case roleRegSliceString:
		return RegisterSliceString, true
	case roleRegSliceBool:
		return RegisterSliceBool, true
	case roleRegSliceUint:
		return RegisterSliceUint64, true
	case roleRegSliceByte:
		return RegisterSliceByte, true
	default:
	}
	return 0, false
}

// RoleIndexesPerFunctionTable reports whether an operand role names an index into a
// per-function table that must be renumbered when splicing bytecode across function
// boundaries.
//
// Takes role (OperandRole) which is the operand's classification.
//
// Returns true for the constant-pool and type-table index roles.
func RoleIndexesPerFunctionTable(role OperandRole) bool {
	switch role {
	case roleConstIndex, roleTypeIndex:
		return true
	default:
		return false
	}
}

// RoleMeansTheSameInEveryFunction reports whether an operand role needs no translation
// between functions.
//
// Takes role (OperandRole) which is the operand's classification.
//
// Returns true when the operand byte can be copied as it stands.
func RoleMeansTheSameInEveryFunction(role OperandRole) bool {
	switch role {
	case RoleNone, RoleImmediate, RoleKindMarker, roleSubOpcode, roleJumpOffsetLow, roleJumpOffsetHigh:
		return true
	default:
		return false
	}
}

// RoleForKind returns the OperandRole that names a register operand of the given bank.
//
// Takes kind (RegisterKind) which is the bank to convert.
//
// Returns the matching OperandRole.
func RoleForKind(kind RegisterKind) OperandRole {
	switch kind {
	case RegisterInt64:
		return RoleRegInt
	case RegisterFloat64:
		return RoleRegFloat
	case RegisterString:
		return RoleRegString
	case RegisterBool:
		return RoleRegBool
	case RegisterUint64:
		return RoleRegUint
	case RegisterComplex128:
		return RoleRegComplex
	case RegisterSliceInt64:
		return RoleRegSliceInt
	case RegisterSliceFloat64:
		return roleRegSliceFloat
	case RegisterSliceString:
		return roleRegSliceString
	case RegisterSliceBool:
		return roleRegSliceBool
	case RegisterSliceUint64:
		return roleRegSliceUint
	case RegisterSliceByte:
		return roleRegSliceByte
	default:

		return RoleRegGeneral
	}
}
