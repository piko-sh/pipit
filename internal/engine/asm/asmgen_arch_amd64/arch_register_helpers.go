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

import "piko.sh/asmgen"

// ScratchRegisters returns the general-purpose scratch registers for amd64.
//
// Returns []string which lists AX, BX, CX, SI, DI.
func (*BytecodeAMD64Arch) ScratchRegisters() []string {
	return []string{"AX", "BX", "CX", "SI", "DI"}
}

// FloatScratchRegisters returns the floating-point scratch registers for amd64.
//
// Returns []string which lists X0, X1.
func (*BytecodeAMD64Arch) FloatScratchRegisters() []string { return []string{"X0", "X1"} }

// DataTemporary returns the data temporary register for amd64.
//
// Returns string which is always "SI" regardless of offset.
func (*BytecodeAMD64Arch) DataTemporary(_ int) string { return "SI" }

// BranchOnCondition emits a conditional branch (J<condition>) to the given label.
//
// Takes e (*asmgen.Emitter) which is the output buffer.
// Takes condition (string) which is the branch condition code.
// Takes label (string) which is the target label.
func (*BytecodeAMD64Arch) BranchOnCondition(e *asmgen.Emitter, condition string, label string) {
	inst(e, "J"+condition, label)
}

// TestAndBranch emits a TESTQ followed by a conditional branch.
//
// Takes e (*asmgen.Emitter) which is the output buffer.
// Takes register (string) which is the register to test.
// Takes condition (string) which is "ZERO" or "NONZERO".
// Takes label (string) which is the target label.
func (*BytecodeAMD64Arch) TestAndBranch(e *asmgen.Emitter, register, condition, label string) {
	inst(e, "TESTQ", register+", "+register)
	switch condition {
	case "ZERO":
		inst(e, "JZ", label)
	case "NONZERO":
		inst(e, "JNZ", label)
	}
}
