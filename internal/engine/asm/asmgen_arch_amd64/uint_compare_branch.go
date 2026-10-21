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

package asmgen_arch_amd64

import (
	"piko.sh/asmgen"
	"piko.sh/asmgen/asmamd64"
)

// UintCompareAndBranch emits an equality comparison of two uint registers and a
// conditional branch.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes condition (string) which is the condition, EQ or NE.
// Takes leftIndex (string) which is the left operand's register index.
// Takes rightIndex (string) which is the right operand's register index.
// Takes label (string) which is the branch target label.
func (*BytecodeAMD64Arch) UintCompareAndBranch(e *asmgen.Emitter, condition string, leftIndex, rightIndex, label string) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI)("+leftIndex+"*8), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, (DI)("+rightIndex+"*8)")
	inst(e, uintEqualityJumpAMD64(condition), label)
}

// uintEqualityJumpAMD64 returns the jump that follows a comparison of two uint registers.
//
// Takes condition (string) which is the condition, EQ or NE.
//
// Returns string which is the jump mnemonic.
//
// Panics when the condition is any other, as an ordered one needs an unsigned jump this
// emitter has no use for yet.
func uintEqualityJumpAMD64(condition string) string {
	switch condition {
	case "EQ":
		return asmamd64.OperationJumpIfEqual
	case "NE":
		return asmamd64.OperationJumpIfNotEqual
	default:
		panic("UintCompareAndBranch: unknown condition " + condition)
	}
}
