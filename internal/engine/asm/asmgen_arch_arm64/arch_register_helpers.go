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

import "piko.sh/asmgen"

// scratchRegisters holds the general-purpose scratch registers for arm64.
var scratchRegisters = []string{"R3", "R4", "R5", "R6", "R7", "R8", "R9", "R10"}

// ScratchRegisters returns the general-purpose scratch registers for arm64.
//
// Returns []string which lists R3 through R10.
func (*BytecodeARM64Arch) ScratchRegisters() []string {
	return scratchRegisters
}

// FloatScratchRegisters returns the floating-point scratch registers for arm64.
//
// Returns []string which lists F0 through F3.
func (*BytecodeARM64Arch) FloatScratchRegisters() []string {
	return []string{"F0", "F1", "F2", "F3"}
}

// DataTemporary returns the data temporary register for arm64, offset past the given
// number of operand registers.
//
// Takes afterOperands (int) which is the number of operand registers to skip.
//
// Returns string which is the selected scratch register.
func (*BytecodeARM64Arch) DataTemporary(afterOperands int) string {
	return scratchRegisters[afterOperands]
}

// BranchOnCondition emits a conditional branch (B<condition>) to the given label.
//
// Takes e (*asmgen.Emitter) which is the output buffer.
// Takes condition (string) which is the branch condition code.
// Takes label (string) which is the target label.
func (*BytecodeARM64Arch) BranchOnCondition(e *asmgen.Emitter, condition string, label string) {
	inst5(e, "B"+condition, label)
}

// TestAndBranch emits a compare-and-branch instruction for arm64.
//
// Takes e (*asmgen.Emitter) which is the output buffer.
// Takes register (string) which is the register to test.
// Takes condition (string) which is "ZERO" or "NONZERO".
// Takes label (string) which is the target label.
func (*BytecodeARM64Arch) TestAndBranch(e *asmgen.Emitter, register, condition, label string) {
	switch condition {
	case "ZERO":
		inst5(e, "CBZ", register+", "+label)
	case "NONZERO":
		inst5(e, "CBNZ", register+", "+label)
	}
}
