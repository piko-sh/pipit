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

// emitReturnInlineCopyGeneralReturnARM64 emits the inline return arm that copies a single
// general result from the callee into the caller's bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitReturnInlineCopyGeneralReturnARM64(e *asmgen.Emitter) {
	e.Label("ri_check_general")
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R1")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R1), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R1, "+labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, "+labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R22), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R6")
	inst5(e, asmarm64.OperationMultiply, "R6, R7, R6")
	inst5(e, asmarm64.OperationAdd, "R6, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R1), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 0(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "8(R1), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 8(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "16(R1), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 16(R5)")
	inst5(e, asmarm64.OperationBranch, labelRINoRetval)
	e.Blank()
}
