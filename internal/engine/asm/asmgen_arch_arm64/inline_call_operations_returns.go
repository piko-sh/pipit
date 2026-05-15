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

// emitReturnInlineGuardChecks emits the guard checks that determine whether the return
// can be handled inline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineGuardChecks(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BASE_FRAME_POINTER(R19), R5")
	inst5(e, asmarm64.OperationCompare, "R5, R4")
	inst5(e, asmarm64.OperationBranchIfLessOrEqualSigned, labelRIFallback)
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R8")
	inst5(e, asmarm64.OperationAdd, "R6, R8, R8")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_DEFERBASE(R8), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_DEFER_STACK_LEN(R19), R9")
	inst5(e, asmarm64.OperationCompare, "R9, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelRIFallback)
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_SIMPLEDEFER(R8), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ri_no_simple_defer")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "SDR_ACTIVE(R7), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R9, "+labelRIFallback)
	e.Label("ri_no_simple_defer")
	e.Blank()
	emitReturnInlineFrameOwnershipGuardsARM64(e, "R8", labelRIFallback)

	inst5(e, asmarm64.OperationLogicalShiftRight32Bits, "$24, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	e.Blank()

	inst5(e, asmarm64.OperationSubtract, "$1, R4, R21")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R21, R7, R9")
	inst5(e, asmarm64.OperationAdd, "R6, R9, R22")
	e.Blank()

	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, "+labelRINoRetval)
	inst5(e, asmarm64.OperationCompare, "$1, R1")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelRIMulti)
	e.Blank()
}

// emitReturnInlineCopyReturnValue emits the return value copy logic, dispatching on the
// return value type to copy a single value from the callee's register bank to the
// caller's register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) emitReturnInlineCopyReturnValue(e *asmgen.Emitter) {
	o.emitReturnInlineDispatchReturnType(e)
	o.emitReturnInlineCopyIntegerReturn(e)
	o.emitReturnInlineCopyFloatReturn(e)
	o.emitReturnInlineCopyStringReturn(e)
	o.emitReturnInlineCopyBooleanReturn(e)
	o.emitReturnInlineCopyUnsignedIntegerReturn(e)
	emitReturnInlineCopyGeneralReturnARM64(e)
	o.emitReturnInlineCopyMultipleReturns(e)
}

// emitReturnInlineDispatchReturnType loads the return destination descriptor and
// dispatches to the appropriate type-specific copy handler.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineDispatchReturnType(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CF_RETURNDEST_PTR(R8), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, "+labelRIFallback)
	e.Blank()

	inst(e, asmarm64.OperationMove8BitsUnsigned, "VL_IS_UPVALUE(R7), R1", mnemonicColumnWidth)
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R1, ri_fallback")
	e.Blank()

	inst(e, asmarm64.OperationMove8BitsUnsigned, "VL_KIND(R7), R1", mnemonicColumnWidth)
	inst(e, asmarm64.OperationMove8BitsUnsigned, "VL_REGISTER(R7), R7", mnemonicColumnWidth)
	e.Blank()

	inst5(e, asmarm64.OperationCompare, "$0, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_int")
	inst5(e, asmarm64.OperationCompare, "$1, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_float")
	inst5(e, asmarm64.OperationCompare, "$2, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_string")
	inst5(e, asmarm64.OperationCompare, "$4, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_bool")
	inst5(e, asmarm64.OperationCompare, "$5, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_uint")
	inst5(e, asmarm64.OperationCompare, "$3, R1")
	inst5(e, asmarm64.OperationBranchIfEqual, "ri_check_general")
	inst5(e, asmarm64.OperationBranch, labelRIFallback)
	e.Blank()
}

// emitReturnInlineCopyIntegerReturn copies a single integer return value from the
// callee's first integer register to the caller's integer register bank at the
// destination index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyIntegerReturn(e *asmgen.Emitter) {
	e.Label("ri_check_int")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, ri_fallback")
	e.Blank()

	e.Label("ri_copy_int")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R22), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R1, (R3)(R7<<3)")
	inst5(e, asmarm64.OperationBranch, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyFloatReturn copies a single float return value from the callee's
// first float register to the caller's float register bank at the destination index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyFloatReturn(e *asmgen.Emitter) {
	e.Label("ri_check_float")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, ri_fallback")
	e.Blank()

	e.Label("ri_copy_float")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R22), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R1, (R3)(R7<<3)")
	inst5(e, asmarm64.OperationBranch, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyStringReturn copies a single 16-byte string return value from the
// callee's first string register to the caller's string register bank at the destination
// index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyStringReturn(e *asmgen.Emitter) {
	e.Label("ri_check_string")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, ri_fallback")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1), R3")
	inst5(e, asmarm64.OperationMove64Bits, "8(R1), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R22), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R7, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R5)(R6)")
	inst5(e, asmarm64.OperationAdd, "$8, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R1, (R5)(R6)")
	inst5(e, asmarm64.OperationBranch, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyBooleanReturn copies a single boolean return value from the
// callee's first boolean register to the caller's boolean register bank at the
// destination index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyBooleanReturn(e *asmgen.Emitter) {
	e.Label("ri_check_bool")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, ri_fallback")
	inst(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R8), R1", mnemonicColumnWidth)
	inst(e, asmarm64.OperationMove8BitsUnsigned, "(R1), R1", mnemonicColumnWidth)
	inst(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R22), R3", mnemonicColumnWidth)
	inst(e, asmarm64.OperationMove8Bits, "R1, (R3)(R7)", mnemonicColumnWidth)
	inst(e, asmarm64.OperationBranch, labelRINoRetval, mnemonicColumnWidth)
	e.Blank()
}

// emitReturnInlineCopyUnsignedIntegerReturn copies a single unsigned integer return value
// from the callee's first uint register to the caller's uint register bank at the
// destination index.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineCopyUnsignedIntegerReturn(e *asmgen.Emitter) {
	e.Label("ri_check_uint")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_LEN(R8), R1")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R1, ri_fallback")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1), R1")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R22), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R1, (R3)(R7<<3)")
	inst5(e, asmarm64.OperationBranch, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineClearStringArena zeroes the callee's string arena entries for GC
// safety, then restores the saved arena indices.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineClearStringArena(e *asmgen.Emitter) {
	e.Label(labelRINoRetval)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R19), R3")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(R8), R5")
	inst5(e, asmarm64.OperationCompare, "R3, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, "ri_str_clear_done")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_SLAB(R19), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R5, R1")
	inst5(e, asmarm64.OperationAdd, "R1, R6, R1")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R6, R3, R3")
	e.Blank()

	e.Label("ri_str_clear_loop")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R1)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 8(R1)")
	inst5(e, asmarm64.OperationAdd, "$16, R1, R1")
	inst5(e, asmarm64.OperationCompare, "R3, R1")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ri_str_clear_loop")
	e.Blank()

	e.Label("ri_str_clear_done")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+0)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_INT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+8)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_FLT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_STR_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+32)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_BOOL_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+40)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_UINT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+96)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_SLICEBYTE_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R1")
	inst5(e, asmarm64.OperationMove8Bits, "R1, CTX_ARENA_BANKS_DIRTY(R19)")
	e.Blank()
}

// emitReturnInlineRestoreCallerState pops the frame and restores all caller dispatch
// state, then issues DISPATCH_NEXT.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineRestoreCallerState(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "R21, CTX_FRAME_POINTER(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R22, CTX_CURRENT_FRAME(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_ASM_CI_PTRS(R19), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1)(R21<<3), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ASM_CALL_INFO_BASE(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_PROGRAM_COUNTER(R22), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R22), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R22), R24")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_STRINGS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_UINTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_BOOLS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_GENERALS_BASE(R19)")

	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_SLICES_BYTE_BASE(R19)")
	emitARM64RestoreTypedSliceBanks(e)
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R19), R1")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$DS_SIZE_SHIFT, R21, R7")
	inst5(e, asmarm64.OperationAdd, "R1, R7, R1")
	inst5(e, asmarm64.OperationMove64Bits, "DS_CODE_BASE(R1), R22")
	inst5(e, asmarm64.OperationMove64Bits, "DS_CODE_LEN(R1), R21")
	inst5(e, asmarm64.OperationMove64Bits, "DS_INT_CONSTS(R1), R26")
	inst5(e, asmarm64.OperationMove64Bits, "DS_FLOAT_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_FLT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_STRING_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_STR_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_BOOL_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_BOOL_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_UINT_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_UINT_CONSTS_BASE(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "R22, CTX_CODE_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R21, CTX_CODE_LEN(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R23, CTX_INTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R24, CTX_FLOATS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R26, CTX_INT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	e.Instruction(macroDispatchNext)
	e.Blank()
}

// emitReturnInlineFrameOwnershipGuardsARM64 falls back to the Go return when the callee
// frame cannot pop inline.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes frameRegister (string) which names the register holding the callee frame pointer.
// Takes fallback (string) which is the label of the Go return path.
func emitReturnInlineFrameOwnershipGuardsARM64(e *asmgen.Emitter, frameRegister string, fallback string) {
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CF_ROOT_SWAPPED("+frameRegister+"), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R7, "+fallback)
	inst5(e, asmarm64.OperationMove64Bits, "CF_SHARED_CELLS("+frameRegister+"), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R7, "+fallback)
	e.Blank()
}

// emitReturnInlineExitPath emits the ri_fallback label for cases where the return cannot
// be handled inline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnInlineExitPath(e *asmgen.Emitter) {
	e.Label(labelRIFallback)
	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_RETURN, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
}

// emitReturnVoidInlineGuardChecks emits the guard checks for the void-return fast path.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnVoidInlineGuardChecks(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BASE_FRAME_POINTER(R19), R5")
	inst5(e, asmarm64.OperationCompare, "R5, R4")
	inst5(e, asmarm64.OperationBranchIfLessOrEqualSigned, "rvi_fallback")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R8")
	inst5(e, asmarm64.OperationAdd, "R6, R8, R8")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_DEFERBASE(R8), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_DEFER_STACK_LEN(R19), R9")
	inst5(e, asmarm64.OperationCompare, "R9, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "rvi_fallback")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_SIMPLEDEFER(R8), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, rvi_no_simple_defer")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "SDR_ACTIVE(R7), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R9, rvi_fallback")
	e.Label("rvi_no_simple_defer")
	e.Blank()
	emitReturnInlineFrameOwnershipGuardsARM64(e, "R8", "rvi_fallback")

	inst5(e, asmarm64.OperationSubtract, "$1, R4, R21")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R21, R7, R9")
	inst5(e, asmarm64.OperationAdd, "R6, R9, R22")
	e.Blank()
}

// emitReturnVoidInlineClearStringArena zeroes the callee's string arena entries for GC
// safety and restores the saved arena indices.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnVoidInlineClearStringArena(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R19), R3")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(R8), R5")
	inst5(e, asmarm64.OperationCompare, "R3, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, "rvi_str_clear_done")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_SLAB(R19), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R5, R1")
	inst5(e, asmarm64.OperationAdd, "R1, R6, R1")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R6, R3, R3")
	e.Blank()

	e.Label("rvi_str_clear_loop")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R1)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 8(R1)")
	inst5(e, asmarm64.OperationAdd, "$16, R1, R1")
	inst5(e, asmarm64.OperationCompare, "R3, R1")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "rvi_str_clear_loop")
	e.Blank()

	e.Label("rvi_str_clear_done")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+0)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_INT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+8)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_FLT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_STR_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+32)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_BOOL_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+40)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_UINT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(CF_ARENA_SAVE+96)(R8), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ARENA_SLICEBYTE_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R1")
	inst5(e, asmarm64.OperationMove8Bits, "R1, CTX_ARENA_BANKS_DIRTY(R19)")
	e.Blank()
}

// emitReturnVoidInlineRestoreCallerState pops the frame and restores all caller dispatch
// state, then issues DISPATCH_NEXT.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnVoidInlineRestoreCallerState(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "R21, CTX_FRAME_POINTER(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R22, CTX_CURRENT_FRAME(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_ASM_CI_PTRS(R19), R1")
	inst5(e, asmarm64.OperationMove64Bits, "(R1)(R21<<3), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_ASM_CALL_INFO_BASE(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_PROGRAM_COUNTER(R22), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R22), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R22), R24")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_STRINGS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_UINTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_BOOLS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_GENERALS_BASE(R19)")

	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(R22), R1")
	inst5(e, asmarm64.OperationMove64Bits, "R1, CTX_SLICES_BYTE_BASE(R19)")
	emitARM64RestoreTypedSliceBanks(e)
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R19), R1")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$DS_SIZE_SHIFT, R21, R7")
	inst5(e, asmarm64.OperationAdd, "R1, R7, R1")
	inst5(e, asmarm64.OperationMove64Bits, "DS_CODE_BASE(R1), R22")
	inst5(e, asmarm64.OperationMove64Bits, "DS_CODE_LEN(R1), R21")
	inst5(e, asmarm64.OperationMove64Bits, "DS_INT_CONSTS(R1), R26")
	inst5(e, asmarm64.OperationMove64Bits, "DS_FLOAT_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_FLT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_STRING_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_STR_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_BOOL_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_BOOL_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "DS_UINT_CONSTS(R1), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_UINT_CONSTS_BASE(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "R22, CTX_CODE_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R21, CTX_CODE_LEN(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R23, CTX_INTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R24, CTX_FLOATS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R26, CTX_INT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	e.Instruction(macroDispatchNext)
	e.Blank()
}

// emitReturnVoidInlineExitPath emits the rvi_fallback label for cases where the void
// return cannot be handled inline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitReturnVoidInlineExitPath(e *asmgen.Emitter) {
	e.Label("rvi_fallback")
	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_RETURN_VOID, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
}

// emitCallInlineSaveArenaIndicesARM64 spills the seven scalar arena indices
// (int/float/string/general/bool/uint/complex) plus the slicesByte index into the callee
// frame's arenaSave slot. Each save is the same MOVD-load + MOVD-store pair; the helper
// keeps the parent emitter under the function-length limit without obscuring the pattern.
//
// Takes e (*asmgen.Emitter) which receives the spill sequence.
func emitCallInlineSaveArenaIndicesARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+0)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+8)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+16)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+24)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_BOOL_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+32)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_UINT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+40)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_CPLX_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+48)(R9)")

	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEINT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+56)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEFLT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+64)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICESTR_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+72)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEBOOL_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+80)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEUINT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+88)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_ARENA_SAVE+96)(R9)")
	e.Blank()
}

// emitCallInlineCopyIntegerArguments emits the integer argument copy loop, transferring
// each integer argument from the caller's register bank to the callee's register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyIntegerArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_INT_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_int_args")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_int_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R11")
	inst5(e, asmarm64.OperationAdd, "$ACI_INT_ARG_SRCS, R11, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R10)(R8<<3)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_int_loop")
	e.Blank()

	e.Label("ci_no_int_args")
	e.Blank()
}

// emitCallInlineCopyFloatArguments emits the float argument copy loop, transferring each
// float argument from the caller's register bank to the callee's register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyFloatArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_FLOAT_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_float_args")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_float_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R11")
	inst5(e, asmarm64.OperationAdd, "$ACI_FLOAT_ARG_SRCS, R11, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R24)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R10)(R8<<3)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_float_loop")
	e.Blank()

	e.Label("ci_no_float_args")
	e.Blank()
}

// emitCallInlineCopyStringArguments emits the string argument copy loop, transferring
// each 16-byte string header from the caller's register bank to the callee's register
// bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyStringArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_STRING_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_string_args")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R11")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_string_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_STRING_ARG_SRCS, R3, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R11)(R3), R5")
	inst5(e, asmarm64.OperationAdd, "$8, R3, R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R11)(R6), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R8, R12")
	inst5(e, asmarm64.OperationMove64Bits, "R5, (R10)(R12)")
	inst5(e, asmarm64.OperationAdd, "$8, R12, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R10)(R3)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_string_loop")
	e.Blank()

	e.Label("ci_no_string_args")
	e.Blank()
}

// emitCallInlineCopyBooleanArguments emits the boolean argument copy loop, transferring
// each single-byte boolean argument from the caller's register bank to the callee's
// register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyBooleanArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_BOOL_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_bool_args")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R11")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_bool_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_BOOL_ARG_SRCS, R3, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")
	inst(e, asmarm64.OperationMove8BitsUnsigned, "(R11)(R3), R3", mnemonicColumnWidth)
	inst5(e, asmarm64.OperationMove8Bits, "R3, (R10)(R8)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_bool_loop")
	e.Blank()

	e.Label("ci_no_bool_args")
	e.Blank()
}

// emitCallInlineCopyUnsignedIntegerArguments emits the unsigned integer argument copy
// loop, transferring each uint argument from the caller's register bank to the callee's
// register bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyUnsignedIntegerArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_UINT_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_uint_args")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R11")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_uint_loop")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_UINT_ARG_SRCS, R3, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R11)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R10)(R8<<3)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_uint_loop")
	e.Blank()

	e.Label("ci_no_uint_args")
	e.Blank()
}

// emitCallInlineCopyByteSliceArguments copies the caller's []byte argument values
// (24-byte slice headers) into the callee's slicesByte register slab, mirroring the amd64
// body.
//
// Each header is copied as three 8-byte words. Since arm64 lacks a power-of-two shift for
// 24, the source/destination byte offsets are computed via MUL with R4=24.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCallInlineCopyByteSliceArguments(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_NUM_SLICEBYTE_ARGS(R2), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, ci_no_slicebyte_args")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_SLICES_BYTE_BASE(R19), R11")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(R9), R10")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R8")
	e.Blank()

	e.Label("ci_slicebyte_loop")

	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R8, R3")
	inst5(e, asmarm64.OperationAdd, "$ACI_SLICEBYTE_ARG_SRCS, R3, R12")
	inst5(e, asmarm64.OperationAdd, "R2, R12, R12")
	inst5(e, asmarm64.OperationMove64Bits, "(R12), R3")

	inst5(e, asmarm64.OperationMultiply, "R4, R3, R5")

	inst5(e, asmarm64.OperationMultiply, "R4, R8, R6")

	inst5(e, asmarm64.OperationAdd, "R5, R11, R12")
	inst5(e, asmarm64.OperationMove64Bits, "0(R12), R3")
	inst5(e, asmarm64.OperationAdd, "R6, R10, R5")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 0(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "8(R12), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 8(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "16(R12), R3")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 16(R5)")
	inst5(e, asmarm64.OperationAdd, "$1, R8, R8")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfLessSigned, "ci_slicebyte_loop")
	e.Blank()

	e.Label("ci_no_slicebyte_args")
	e.Blank()
}
