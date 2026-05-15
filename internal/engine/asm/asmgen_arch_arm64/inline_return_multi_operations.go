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
	"fmt"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmarm64"
)

const (
	// labelRIMulti names the multi-value return copy loop entry of handlerReturnInline.
	labelRIMulti = "ri_multi"

	// labelRIMultiLoop names the multi-value return copy loop of handlerReturnInline.
	labelRIMultiLoop = "ri_multi_loop"

	// labelRIMultiNext names the multi-value return copy loop advance of
	// handlerReturnInline.
	labelRIMultiNext = "ri_multi_next"

	// returnCursorBytes is the byte span of the CTX_RET_CURSORS entries the multi-value
	// return loop zeroes: one 8-byte cursor per register kind up to uint (kind 5).
	returnCursorBytes = 48
)

// emitReturnInlineCopyMultipleReturns emits the arm64 multi-value return loop, copying
// two to four scalar results from the callee banks to the caller banks via the returnDest
// slice.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyMultipleReturns(e *asmgen.Emitter) {
	emitReturnInlineMultiPrologueARM64(e)
	emitReturnInlineMultiLoopHeadARM64(e)
	emitReturnInlineMultiDispatchARM64(e)
	emitReturnInlineMultiWordCopyARM64(e, "ri_multi_int", "CF_REGS_INTS_LEN", "CF_REGS_INTS_PTR")
	emitReturnInlineMultiWordCopyARM64(e, "ri_multi_float", "CF_REGS_FLOATS_LEN", "CF_REGS_FLOATS_PTR")
	emitReturnInlineMultiWordCopyARM64(e, "ri_multi_uint", "CF_REGS_UINTS_LEN", "CF_REGS_UINTS_PTR")
	emitReturnInlineMultiStringAndBoolCopyARM64(e)
}

// emitReturnInlineMultiPrologueARM64 rejects counts above the inline limit or the
// callee's result count, then seeds CTX_RET_COUNT, CTX_RET_INDEX and the per-kind
// cursors.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitReturnInlineMultiPrologueARM64(e *asmgen.Emitter) {
	e.Label(labelRIMulti)
	inst5(e, asmarm64.OperationCompare, "$4, R1")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R8), R3")
	inst5(e, asmarm64.OperationMove64Bits, "FN_RESULT_KINDS_LEN(R3), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R1")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_RET_COUNT(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CTX_RET_INDEX(R19)")
	for offset := 0; offset < returnCursorBytes; offset += 8 {
		inst5(e, asmarm64.OperationMove64Bits, fmt.Sprintf("ZR, (CTX_RET_CURSORS+%d)(R19)", offset))
	}
	e.Blank()
}

// emitReturnInlineMultiLoopHeadARM64 emits the multi-return loop head.
//
// It ends the loop when the index reaches the count or the destination length, decodes
// the destination descriptor into R7, validates it against the callee's result kind and
// loads the destination register into R7 and the callee source register (the advanced
// per-kind cursor) into R3, leaving the kind in R1.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitReturnInlineMultiLoopHeadARM64(e *asmgen.Emitter) {
	e.Label(labelRIMultiLoop)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_RET_INDEX(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_RET_COUNT(R19), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelRINoRetval)
	inst5(e, asmarm64.OperationMove64Bits, "CF_RETURNDEST_LEN(R8), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelRINoRetval)
	inst5(e, asmarm64.OperationMove64Bits, "$VL_SIZE, R6")
	inst5(e, asmarm64.OperationMultiply, "R5, R6, R7")
	inst5(e, asmarm64.OperationMove64Bits, "CF_RETURNDEST_PTR(R8), R6")
	inst5(e, asmarm64.OperationAdd, "R6, R7, R7")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "VL_IS_UPVALUE(R7), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R1, "+labelRIFallback)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "VL_KIND(R7), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R8), R3")
	inst5(e, asmarm64.OperationMove64Bits, "FN_RESULT_KINDS(R3), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3)(R5), R3")
	inst5(e, asmarm64.OperationCompare, "R3, R1")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelRIFallback)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "VL_REGISTER(R7), R7")
	inst5(e, asmarm64.OperationAdd, "$CTX_RET_CURSORS, R19, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R4)(R1<<3), R3")
	inst5(e, asmarm64.OperationAdd, "$1, R3, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R4)(R1<<3)")
	e.Blank()
}

// emitReturnInlineMultiDispatchARM64 branches on the kind in R1 to the per-kind copy arm,
// falling back for any kind the inline path does not copy.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitReturnInlineMultiDispatchARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationCompare, "$0, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_multi_int")
	inst5(e, asmarm64.OperationCompare, "$1, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_multi_float")
	inst5(e, asmarm64.OperationCompare, "$2, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_multi_string")
	inst5(e, asmarm64.OperationCompare, "$4, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_multi_bool")
	inst5(e, asmarm64.OperationCompare, "$5, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_multi_uint")
	inst5(e, asmarm64.OperationBranch, labelRIFallback)
	e.Blank()
}

// emitReturnInlineMultiWordCopyARM64 emits one 8-byte bank copy arm of the arm64
// multi-return loop: callee bank[R3] to caller bank[R7], falling back when R3 is beyond
// the callee bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes label (string) which names the arm.
// Takes lengthField (string) which is the callFrame define of the bank length.
// Takes pointerField (string) which is the callFrame define of the bank pointer.
func emitReturnInlineMultiWordCopyARM64(e *asmgen.Emitter, label, lengthField, pointerField string) {
	e.Label(label)
	inst5(e, asmarm64.OperationMove64Bits, lengthField+"(R8), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R3")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, pointerField+"(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1)(R3<<3), R1")
	inst5(e, asmarm64.OperationMove64Bits, pointerField+"(R22), R6")
	inst5(e, asmarm64.OperationMove64Bits, "R1, (R6)(R7<<3)")
	inst5(e, asmarm64.OperationBranch, labelRIMultiNext)
	e.Blank()
}

// emitReturnInlineMultiStringAndBoolCopyARM64 emits the 16-byte string copy arm, the
// 1-byte bool copy arm and the shared loop tail that advances CTX_RET_INDEX.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitReturnInlineMultiStringAndBoolCopyARM64(e *asmgen.Emitter) {
	e.Label("ri_multi_string")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_LEN(R8), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R3")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R8), R1")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R1, R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1), R4")
	inst5(e, asmarm64.OperationMove64Bits, "8(R1), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R22), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R7, R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R4, (R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R1, 8(R6)")
	inst5(e, asmarm64.OperationBranch, labelRIMultiNext)
	e.Blank()

	e.Label("ri_multi_bool")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_LEN(R8), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R3")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelRIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R1)(R3), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R22), R6")
	inst5(e, asmarm64.OperationMove8Bits, "R1, (R6)(R7)")
	e.Blank()

	e.Label(labelRIMultiNext)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_RET_INDEX(R19), R5")
	inst5(e, asmarm64.OperationAdd, "$1, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "R5, CTX_RET_INDEX(R19)")
	inst5(e, asmarm64.OperationBranch, labelRIMultiLoop)
	e.Blank()
}
