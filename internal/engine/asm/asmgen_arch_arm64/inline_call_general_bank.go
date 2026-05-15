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

// emitCallInlineMaybeSetupGeneralBank emits the general-bank setup stage of an inline
// call whose isFastPath is 3 on ARM64.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (*arm64InlineCallOps) emitCallInlineMaybeSetupGeneralBank(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_IS_FAST_PATH(R2), R3")
	inst5(e, asmarm64.OperationCompare, "$3, R3")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "ci_no_general_bank")
	emitCallInlineGeneralBankFastArmARM64(e)
	e.Label("ci_general_bank_slow")
	inst5(e, asmarm64.OperationBranchAndLink, "·handlerCallInlineSetupGeneralBank(SB)")

	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R3")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R4")
	inst5(e, asmarm64.OperationMultiply, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R9")
	inst5(e, asmarm64.OperationAdd, "R4, R9, R9")
	e.Label("ci_no_general_bank")
	e.Blank()
}

// emitCallInlineGeneralBankFastArmARM64 emits the fast-arm general-bank setup, branching
// to ci_general_bank_slow when any check fails.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func emitCallInlineGeneralBankFastArmARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, ci_general_bank_slow")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_GENERAL(R2), R5")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R5, ci_general_bank_slow")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R19), R4")
	inst5(e, asmarm64.OperationAdd, "R5, R4, R6")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERAL_SLAB_CAP(R19), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R6")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, "ci_general_bank_slow")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_GENERAL_ARGS(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R12")
	emitCallInlineGeneralBankArgumentPreCheckARM64(e)
	emitCallInlineGeneralBankAllocateARM64(e)
	emitCallInlineGeneralBankCopyArgumentsARM64(e)
	emitCallInlineGeneralBankSaveCalleeStateARM64(e)
	inst5(e, asmarm64.OperationBranch, "ci_no_general_bank")
}

// emitCallInlineGeneralBankArgumentPreCheckARM64 walks general argument sources and
// branches to the slow path when one holds a struct or array.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func emitCallInlineGeneralBankArgumentPreCheckARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_general_bank_alloc")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Label("ci_general_bank_check_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_GENERAL_ARG_SRCS, R3, R11")
	inst5(e, asmarm64.OperationAdd, "R2, R11, R11")
	inst5(e, asmarm64.OperationMove64Bits, "(R11), R3")
	inst5(e, asmarm64.OperationMultiply, "R12, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R10, R3, R11")
	inst5(e, asmarm64.OperationMove64Bits, "0(R11), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, ci_general_bank_check_next")
	inst5(e, asmarm64.OperationMove64Bits, "16(R11), R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R3, R3")
	inst5(e, asmarm64.OperationCompare, "$25, R3")
	inst5(e, asmarm64.OperationBranchIfEqual, "ci_general_bank_slow")
	inst5(e, asmarm64.OperationCompare, "$17, R3")
	inst5(e, asmarm64.OperationBranchIfEqual, "ci_general_bank_slow")
	e.Label("ci_general_bank_check_next")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_general_bank_check_loop")
	e.Label("ci_general_bank_alloc")
}

// emitCallInlineGeneralBankAllocateARM64 installs the callee's general bank on the frame,
// sets hasGeneralAlloc, advances the cursor and marks the arena banks dirty.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func emitCallInlineGeneralBankAllocateARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMultiply, "R12, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERAL_SLAB(R19), R3")
	inst5(e, asmarm64.OperationAdd, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "R4, CF_REGS_GENERAL_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R5, CF_REGS_GENERAL_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R5, CF_REGS_GENERAL_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R3")
	inst5(e, asmarm64.OperationMove8Bits, "R3, CF_HAS_GENERAL_ALLOC(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R6, CTX_ARENA_GEN_IDX(R19)")
	inst5(e, asmarm64.OperationMove8Bits, "R3, CTX_ARENA_BANKS_DIRTY(R19)")
}

// emitCallInlineGeneralBankCopyArgumentsARM64 copies each general argument's three words
// from the caller's slot named by the call info into callee general[i].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func emitCallInlineGeneralBankCopyArgumentsARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_general_bank_save")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Label("ci_general_bank_copy_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_GENERAL_ARG_SRCS, R3, R11")
	inst5(e, asmarm64.OperationAdd, "R2, R11, R11")
	inst5(e, asmarm64.OperationMove64Bits, "(R11), R3")
	inst5(e, asmarm64.OperationMultiply, "R12, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R10, R3, R11")
	inst5(e, asmarm64.OperationMultiply, "R12, R8, R13")
	inst5(e, asmarm64.OperationAdd, "R4, R13, R13")
	inst5(e, asmarm64.OperationMove64Bits, "0(R11), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 0(R13)")
	inst5(e, asmarm64.OperationMove64Bits, "8(R11), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 8(R13)")
	inst5(e, asmarm64.OperationMove64Bits, "16(R11), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 16(R13)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_general_bank_copy_loop")
	e.Label("ci_general_bank_save")
}

// emitCallInlineGeneralBankSaveCalleeStateARM64 writes the callee's dispatch-save slot
// with the same seven-word layout emitCallInlineSaveCallerState uses for the caller.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func emitCallInlineGeneralBankSaveCalleeStateARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R19), R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R11")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$DS_SIZE_SHIFT, R11, R11")
	inst5(e, asmarm64.OperationAdd, "R11, R3, R3")
	for _, word := range [][2]string{
		{"ACI_CALLEE_BODY", "DS_CODE_BASE"}, {"ACI_CALLEE_BODY_LEN", "DS_CODE_LEN"}, {"ACI_CALLEE_INT_CONSTS", "DS_INT_CONSTS"},
		{"ACI_CALLEE_FLT_CONSTS", "DS_FLOAT_CONSTS"}, {"ACI_CALLEE_STR_CONSTS", "DS_STRING_CONSTS"}, {"ACI_CALLEE_BOOL_CONSTS", "DS_BOOL_CONSTS"},
		{"ACI_CALLEE_UINT_CONSTS", "DS_UINT_CONSTS"},
	} {
		inst5(e, asmarm64.OperationMove64Bits, word[0]+"(R2), R11")
		inst5(e, asmarm64.OperationMove64Bits, "R11, "+word[1]+"(R3)")
	}
}
