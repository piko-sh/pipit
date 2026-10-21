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

package asmgen_arch_arm64

import (
	"piko.sh/asmgen"
	"piko.sh/asmgen/asmarm64"
)

// UintCompareAndBranch emits an equality comparison of two uint registers and a
// conditional branch.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes condition (string) which is the condition, EQ or NE.
// Takes leftIndex (string) which is the left operand's register index.
// Takes rightIndex (string) which is the right operand's register index.
// Takes label (string) which is the branch target label.
func (*BytecodeARM64Arch) UintCompareAndBranch(e *asmgen.Emitter, condition string, leftIndex, rightIndex, label string) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+leftIndex+"<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)("+rightIndex+"<<3), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	inst5(e, uintEqualityBranchARM64(condition), label)
}

// uintEqualityBranchARM64 returns the branch that follows a comparison of two uint
// registers.
//
// Takes condition (string) which is the condition, EQ or NE.
//
// Returns string which is the branch mnemonic.
//
// Panics when the condition is any other, as an ordered one needs an unsigned branch this
// emitter has no use for yet.
func uintEqualityBranchARM64(condition string) string {
	switch condition {
	case "EQ":
		return asmarm64.OperationBranchIfEqual
	case "NE":
		return asmarm64.OperationBranchIfNotEqual
	default:
		panic("UintCompareAndBranch: unknown condition " + condition)
	}
}
