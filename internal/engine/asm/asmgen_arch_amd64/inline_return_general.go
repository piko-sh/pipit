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

// emitReturnInlineCopyGeneralReturnAMD64 emits the inline return arm that copies a single
// general result from the callee into the caller's bank.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitReturnInlineCopyGeneralReturnAMD64(e *asmgen.Emitter) {
	e.Label("ri_check_general")
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelRIFallback)
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_GENERAL_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R12), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), CX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$3, CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, BX")
	inst(e, asmamd64.OperationMove64Bits, "0(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(BX)")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(BX)")
	inst(e, asmamd64.OperationMove64Bits, "16(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(BX)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}
