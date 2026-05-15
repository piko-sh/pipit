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

// emitCallInlineMaybeSetupGeneralBank emits the general-bank setup stage of the inline
// call for callees whose isFastPath is 3. All checks pass before any write so the slow
// arm always starts from the state the Go trampoline expects.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineMaybeSetupGeneralBank(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare64Bits, "ACI_IS_FAST_PATH(AX), $3")
	inst(e, asmamd64.OperationJumpIfNotEqual, "ci_no_general_bank")
	emitCallInlineGeneralBankFastArmAMD64(e)
	e.Label("ci_general_bank_slow")
	inst(e, asmamd64.OperationCall, "·handlerCallInlineSetupGeneralBank(SB)")

	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "DI, DX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CSTACK_BASE(R15), BX")
	inst(e, asmamd64.OperationAdd64Bits, "DX, BX")
	e.Label("ci_no_general_bank")
	e.Blank()
}

// emitCallInlineGeneralBankFastArmAMD64 emits the fast arm's guard checks and writes,
// jumping to ci_no_general_bank on success or ci_general_bank_slow on any refusal.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCallInlineGeneralBankFastArmAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "ci_general_bank_slow")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_GENERAL(AX), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_general_bank_slow")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CTX_ARENA_GENERAL_SLAB_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, "ci_general_bank_slow")
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_GENERAL_ARGS(AX), SI")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), R14")
	emitCallInlineGeneralBankArgumentPreCheckAMD64(e)
	emitCallInlineGeneralBankAllocateAMD64(e)
	emitCallInlineGeneralBankCopyArgumentsAMD64(e)
	emitCallInlineGeneralBankSaveCalleeStateAMD64(e)
	inst(e, asmamd64.OperationJump, "ci_no_general_bank")
}

// emitCallInlineGeneralBankArgumentPreCheckAMD64 walks the general argument sources and
// jumps to the slow path when one holds a struct or array value.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCallInlineGeneralBankArgumentPreCheckAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_general_bank_alloc")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "R12, R12")
	e.Label("ci_general_bank_check_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_GENERAL_ARG_SRCS)(AX)(R12*8), R13")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(R13)(R13*2), R13")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(R14)(R13*8), R13")
	inst(e, asmamd64.OperationMove64Bits, "0(R13), DX")
	inst(e, asmamd64.OperationTest64Bits, "DX, DX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_general_bank_check_next")
	inst(e, asmamd64.OperationMove64Bits, "16(R13), DX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, $25")
	inst(e, asmamd64.OperationJumpIfEqual, "ci_general_bank_slow")
	inst(e, asmamd64.OperationCompare64Bits, "DX, $17")
	inst(e, asmamd64.OperationJumpIfEqual, "ci_general_bank_slow")
	e.Label("ci_general_bank_check_next")
	inst(e, asmamd64.OperationIncrement64Bits, "R12")
	inst(e, asmamd64.OperationCompare64Bits, "R12, SI")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_general_bank_check_loop")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), DX")
	e.Label("ci_general_bank_alloc")
}

// emitCallInlineGeneralBankAllocateAMD64 installs the callee's general bank from the
// arena slab and marks the arena banks dirty.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCallInlineGeneralBankAllocateAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), CX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$3, CX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_ARENA_GENERAL_SLAB(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_GENERAL_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_GENERAL_CAP(BX)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CF_HAS_GENERAL_ALLOC(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DX, CTX_ARENA_GEN_IDX(R15)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CTX_ARENA_BANKS_DIRTY(R15)")
}

// emitCallInlineGeneralBankCopyArgumentsAMD64 copies each general argument's three words
// from the caller's slot named by the call info into callee general[i].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCallInlineGeneralBankCopyArgumentsAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_general_bank_save")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "R12, R12")
	e.Label("ci_general_bank_copy_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_GENERAL_ARG_SRCS)(AX)(R12*8), R13")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(R13)(R13*2), R13")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(R14)(R13*8), R13")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(R12)(R12*2), DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DX*8), DX")
	inst(e, asmamd64.OperationMove64Bits, "0(R13), DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, 0(DX)")
	inst(e, asmamd64.OperationMove64Bits, "8(R13), DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, 8(DX)")
	inst(e, asmamd64.OperationMove64Bits, "16(R13), DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, 16(DX)")
	inst(e, asmamd64.OperationIncrement64Bits, "R12")
	inst(e, asmamd64.OperationCompare64Bits, "R12, SI")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_general_bank_copy_loop")
	e.Label("ci_general_bank_save")
}

// emitCallInlineGeneralBankSaveCalleeStateAMD64 writes the callee's dispatch-save slot
// with the same layout emitCallInlineSaveCallerState uses for the caller.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCallInlineGeneralBankSaveCalleeStateAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R15), DX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), DI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$DS_SIZE_SHIFT, DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(DI*1), DX")
	for _, word := range [][2]string{
		{"ACI_CALLEE_BODY", "DS_CODE_BASE"}, {"ACI_CALLEE_BODY_LEN", "DS_CODE_LEN"}, {"ACI_CALLEE_INT_CONSTS", "DS_INT_CONSTS"},
		{"ACI_CALLEE_FLT_CONSTS", "DS_FLOAT_CONSTS"}, {"ACI_CALLEE_STR_CONSTS", "DS_STRING_CONSTS"}, {"ACI_CALLEE_BOOL_CONSTS", "DS_BOOL_CONSTS"},
		{"ACI_CALLEE_UINT_CONSTS", "DS_UINT_CONSTS"},
	} {
		inst(e, asmamd64.OperationMove64Bits, word[0]+"(AX), DI")
		inst(e, asmamd64.OperationMove64Bits, "DI, "+word[1]+"(DX)")
	}
}
