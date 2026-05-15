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
	"fmt"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmamd64"
)

const (
	// labelRIMulti names the entry point of the multi-value return copy loop of
	// handlerReturnInline.
	labelRIMulti = "ri_multi"

	// labelRIMultiLoop names the iteration label of the multi-value return copy loop of
	// handlerReturnInline.
	labelRIMultiLoop = "ri_multi_loop"

	// labelRIMultiNext names the next-element label of the multi-value return copy loop of
	// handlerReturnInline.
	labelRIMultiNext = "ri_multi_next"

	// returnCursorBytes is the byte span of the CTX_RET_CURSORS entries the multi-value
	// return loop zeroes: one 8-byte cursor per register kind up to uint (kind 5).
	returnCursorBytes = 48
)

// emitReturnInlineCopyMultipleReturns copies two to four scalar return values from the
// callee's register banks into the caller's, walking the returnDest slice with per-kind
// source cursors. Partial copies are idempotent, so a fallback to EXIT_RETURN redoes the
// whole copy safely.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyMultipleReturns(e *asmgen.Emitter) {
	emitReturnInlineMultiPrologue(e)
	emitReturnInlineMultiLoopHead(e)
	emitReturnInlineMultiDispatch(e)
	emitReturnInlineMultiWordCopy(e, "ri_multi_int", "CF_REGS_INTS_LEN", "CF_REGS_INTS_PTR")
	emitReturnInlineMultiWordCopy(e, "ri_multi_float", "CF_REGS_FLOATS_LEN", "CF_REGS_FLOATS_PTR")
	emitReturnInlineMultiWordCopy(e, "ri_multi_uint", "CF_REGS_UINTS_LEN", "CF_REGS_UINTS_PTR")
	emitReturnInlineMultiStringAndBoolCopy(e)
}

// emitReturnInlineMultiPrologue rejects counts above the inline limit or the callee's
// result count, then seeds the loop state: CTX_RET_COUNT, CTX_RET_INDEX and the per-kind
// cursors.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitReturnInlineMultiPrologue(e *asmgen.Emitter) {
	e.Label(labelRIMulti)
	inst(e, asmamd64.OperationCompare64Bits, "AX, $4")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(DI), SI")
	inst(e, asmamd64.OperationCompare64Bits, "AX, FN_RESULT_KINDS_LEN(SI)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_RET_COUNT(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CTX_RET_INDEX(R15)")
	for offset := 0; offset < returnCursorBytes; offset += 8 {
		inst(e, asmamd64.OperationMove64Bits, fmt.Sprintf("$0, (CTX_RET_CURSORS+%d)(R15)", offset))
	}
	e.Blank()
}

// emitReturnInlineMultiLoopHead emits the loop-head block of the multi-value return copy,
// validating each destination descriptor against the callee's result kind.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitReturnInlineMultiLoopHead(e *asmgen.Emitter) {
	e.Label(labelRIMultiLoop)
	inst(e, asmamd64.OperationMove64Bits, "CTX_RET_INDEX(R15), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_RET_COUNT(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelRINoRetval)
	inst(e, asmamd64.OperationCompare64Bits, "SI, CF_RETURNDEST_LEN(DI)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelRINoRetval)
	inst(e, asmamd64.OperationMove64Bits, "$VL_SIZE, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "SI, DX")
	inst(e, asmamd64.OperationAdd64Bits, "CF_RETURNDEST_PTR(DI), DX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_IS_UPVALUE(DX), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfNotZero, labelRIFallback)
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_KIND(DX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(DI), BX")
	inst(e, asmamd64.OperationMove64Bits, "FN_RESULT_KINDS(BX), BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(BX)(SI*1), BX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, BX")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelRIFallback)
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_REGISTER(DX), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "CTX_RET_CURSORS(R15), DX")
	inst(e, asmamd64.OperationMove64Bits, "(DX)(AX*8), BX")
	inst(e, asmamd64.OperationIncrement64Bits, "(DX)(AX*8)")
	e.Blank()
}

// emitReturnInlineMultiDispatch branches on the kind in AX to the per-kind copy arm,
// falling back for any kind the inline path does not copy.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitReturnInlineMultiDispatch(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare64Bits, "AX, $0")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_multi_int")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $1")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_multi_float")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $2")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_multi_string")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $4")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_multi_bool")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $5")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_multi_uint")
	inst(e, asmamd64.OperationJump, labelRIFallback)
	e.Blank()
}

// emitReturnInlineMultiWordCopy emits one 8-byte bank copy arm of the multi-return loop:
// callee bank[BX] to caller bank[CX], falling back when BX is beyond the callee bank.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes label (string) which names the arm.
// Takes lengthField (string) which is the callFrame define of the bank length.
// Takes pointerField (string) which is the callFrame define of the bank pointer.
func emitReturnInlineMultiWordCopy(e *asmgen.Emitter, label, lengthField, pointerField string) {
	e.Label(label)
	inst(e, asmamd64.OperationCompare64Bits, "BX, "+lengthField+"(DI)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, pointerField+"(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX)(BX*8), AX")
	inst(e, asmamd64.OperationMove64Bits, pointerField+"(R12), DX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (DX)(CX*8)")
	inst(e, asmamd64.OperationJump, labelRIMultiNext)
	e.Blank()
}

// emitReturnInlineMultiStringAndBoolCopy emits the 16-byte string copy arm, the 1-byte
// bool copy arm and the shared loop tail that advances CTX_RET_INDEX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitReturnInlineMultiStringAndBoolCopy(e *asmgen.Emitter) {
	e.Label("ri_multi_string")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CF_REGS_STRINGS_LEN(DI)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelRIFallback)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, CX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX)(BX*1), SI")
	inst(e, asmamd64.OperationMove64Bits, "8(AX)(BX*1), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R12), DX")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DX)(CX*1)")
	inst(e, asmamd64.OperationMove64Bits, "AX, 8(DX)(CX*1)")
	inst(e, asmamd64.OperationJump, labelRIMultiNext)
	e.Blank()

	e.Label("ri_multi_bool")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CF_REGS_BOOLS_LEN(DI)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(AX)(BX*1), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R12), DX")
	inst(e, asmamd64.OperationMove8Bits, "AX, (DX)(CX*1)")
	e.Blank()

	e.Label(labelRIMultiNext)
	inst(e, asmamd64.OperationIncrement64Bits, "CTX_RET_INDEX(R15)")
	inst(e, asmamd64.OperationJump, labelRIMultiLoop)
	e.Blank()
}
