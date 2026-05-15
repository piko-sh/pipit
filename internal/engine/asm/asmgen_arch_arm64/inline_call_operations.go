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

const (
	// inlineCallLRRestoreOperand is the LR-restore source/dest pair, emitted before any JMP
	// exit of a FRAMEd inline-call handler. Must precede the SP teardown so PCSP tracking
	// stays consistent.
	inlineCallLRRestoreOperand = "0(RSP), R30"

	// inlineCallFrameTeardownOperand is the manual SP-adjustment that reverses the 48-byte
	// frame allocation. Paired with inlineCallLRRestoreOperand.
	inlineCallFrameTeardownOperand = "$48, RSP"

	// inlineCallDispatchExitSymbol is the symbolic JMP target the FRAMEd inline-call
	// handlers use instead of a literal RET. See handlerDispatchExit in
	// handlers_initialisation.go for why the RET is owned by a separate NOFRAME function.
	inlineCallDispatchExitSymbol = "\xc2\xb7dispatchExit(SB)"

	// labelCIFallback is the label for the call-inline fallback exit path.
	labelCIFallback = "ci_fallback"

	// labelRIFallback is the label for the return-inline fallback exit path.
	labelRIFallback = "ri_fallback"

	// labelRINoRetval is the label for the return-inline no-return-value path.
	labelRINoRetval = "ri_no_retval"

	// labelCIFallbackPostFPInc names the call-inline fallback exit path branch reached after
	// the frame-pointer increment.
	labelCIFallbackPostFPInc = "ci_fallback_post_fp_inc"
)

// arm64InlineCallOps implements InlineCallOperationsPort for ARM 64-bit Plan 9 assembly.
// Each method emits the complete handler body for inline call, return, and void-return
// handlers.
type arm64InlineCallOps struct{}

var (
	_ asmgen.InlineCallOperationsPort = (*arm64InlineCallOps)(nil)
)

// EmitTailCallInline emits the arm64 top-level body of handlerTailCallInline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) EmitTailCallInline(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)

	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationBranchAndLink, "·handlerTailCallInlineSubroutine(SB)")

	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	e.Instruction(macroDispatchNext)
}

// EmitTailCallInlineSubroutine emits the arm64 body of the sub-routine that
// handlerTailCallInline BLs into for the actual tail-call work.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) EmitTailCallInlineSubroutine(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)

	inst5(e, asmarm64.OperationMove64Bits, "R19, 8(RSP)")
	inst5(e, asmarm64.OperationBranchAndLink, "·asmTailCallExecute(SB)")
	inst5(e, asmarm64.OperationMove64Bits, "16(RSP), R19")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_PC(R19), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_BASE(R19), R22")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_LEN(R19), R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INTS_BASE(R19), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLOATS_BASE(R19), R24")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R19), R26")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_JUMP_TABLE(R19), R25")
	e.Instruction(asmarm64.OperationReturn)
}

// EmitCallInline emits the full handlerCallInline function body, attempting ASM-inlined
// call for fast-path eligible sites and falling back to Go otherwise.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) EmitCallInline(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	o.emitCallInlineLookupCallInfo(e)
	o.emitCallInlineGuardChecks(e)
	o.emitCallInlineSaveCallerState(e)
	o.emitCallInlineAllocateCalleeFrame(e)
	o.emitCallInlineAllocateRegisters(e)
	o.emitCallInlinePopulateFrameFields(e)
	o.emitCallInlineCopyArguments(e)
	o.emitCallInlineMaybeSetupGeneralBank(e)
	o.emitCallInlineReloadDispatchState(e)
	o.emitCallInlineExitPaths(e)
}

// EmitCallInlineScalar emits the lean isa.SubOpCallScalar handler body.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (o *arm64InlineCallOps) EmitCallInlineScalar(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	o.emitCallInlineLookupCallInfo(e)
	o.emitCallInlineCoreGuardChecks(e)
	o.emitCallInlineSaveCallerState(e)
	o.emitCallInlineAllocateCalleeFrame(e)
	o.emitCallInlineAllocateRegisters(e)
	o.emitCallInlinePopulateFrameFields(e)
	o.emitCallInlineCopyArguments(e)
	o.emitCallInlineReloadDispatchState(e)
	o.emitCallInlineExitPaths(e)
}

// EmitReturnInline emits the full handlerReturnInline function body, attempting
// ASM-inlined return for single-value fast-path cases.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) EmitReturnInline(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	o.emitReturnInlineGuardChecks(e)
	o.emitReturnInlineCopyReturnValue(e)
	o.emitReturnInlineClearStringArena(e)
	o.emitReturnInlineMaybeClearGeneralBank(e, "ri")
	o.emitReturnInlineRestoreCallerState(e)
	o.emitReturnInlineExitPath(e)
}

// EmitReturnVoidInline emits the full handlerReturnVoidInline function body, skipping
// return value copy and proceeding directly to arena cleanup.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) EmitReturnVoidInline(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	o.emitReturnVoidInlineGuardChecks(e)
	o.emitReturnVoidInlineClearStringArena(e)
	o.emitReturnInlineMaybeClearGeneralBank(e, "rvi")
	o.emitReturnVoidInlineRestoreCallerState(e)
	o.emitReturnVoidInlineExitPath(e)
}

// EmitCallInlineSetupGeneralBank emits the arm64 body of the trampoline that finishes
// general-bank setup when isFastPath == 3.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (*arm64InlineCallOps) EmitCallInlineSetupGeneralBank(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)

	inst5(e, asmarm64.OperationMove64Bits, "R2, 32(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_SAVED_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R19, 8(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R2, 16(RSP)")
	inst5(e, asmarm64.OperationBranchAndLink, "·asmCallSetupGeneralBank(SB)")
	inst5(e, asmarm64.OperationMove64Bits, "24(RSP), R19")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_SAVED_PC(R19), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_BASE(R19), R22")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_LEN(R19), R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INTS_BASE(R19), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLOATS_BASE(R19), R24")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R19), R26")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_JUMP_TABLE(R19), R25")
	inst5(e, asmarm64.OperationMove64Bits, "32(RSP), R2")
	e.Instruction(asmarm64.OperationReturn)
}

// EmitCallInlineClearGeneralBank emits the arm64 body of the return-side trampoline that
// clears the GC-visible general slab range occupied by the popped callee frame.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (*arm64InlineCallOps) EmitCallInlineClearGeneralBank(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)

	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_SAVED_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R19, 8(RSP)")
	inst5(e, asmarm64.OperationBranchAndLink, "·asmReturnClearGeneralBank(SB)")
	inst5(e, asmarm64.OperationMove64Bits, "16(RSP), R19")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_SAVED_PC(R19), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_BASE(R19), R22")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_LEN(R19), R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INTS_BASE(R19), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLOATS_BASE(R19), R24")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R19), R26")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_JUMP_TABLE(R19), R25")
	e.Instruction(asmarm64.OperationReturn)
}

// emitReturnInlineMaybeClearGeneralBank emits a conditional BL to clear general-bank
// slots allocated by the returning callee.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes prefix (string) which selects the label namespace.
func (*arm64InlineCallOps) emitReturnInlineMaybeClearGeneralBank(e *asmgen.Emitter, prefix string) {
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CF_HAS_GENERAL_ALLOC(R8), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, "+prefix+"_gen_clear_done")

	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, "+prefix+"_gen_clear_slow")
	inst5(e, asmarm64.OperationMove64Bits, "CF_ARENA_SAVE_GENERAL_IDX(R8), R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R19), R6")
	inst5(e, asmarm64.OperationSubtract, "R3, R6, R6")
	inst5(e, asmarm64.OperationCompare, "$0, R6")
	inst5(e, asmarm64.OperationBranchIfLessOrEqualSigned, prefix+"_gen_clear_restore")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R3, R9, R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERAL_SLAB(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R9, R7, R7")
	e.Label(prefix + "_gen_clear_zero")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R7)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 8(R7)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 16(R7)")
	inst5(e, asmarm64.OperationAdd, "$24, R7, R7")
	inst5(e, asmarm64.OperationSubtract, "$1, R6, R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R6, "+prefix+"_gen_clear_zero")
	e.Label(prefix + "_gen_clear_restore")
	inst5(e, asmarm64.OperationMove64Bits, "R3, CTX_ARENA_GEN_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R6")
	inst5(e, asmarm64.OperationMove8Bits, "R6, CTX_ARENA_BANKS_DIRTY(R19)")
	inst5(e, asmarm64.OperationBranch, prefix+"_gen_clear_done")

	e.Label(prefix + "_gen_clear_slow")
	inst5(e, asmarm64.OperationBranchAndLink, "·handlerCallInlineClearGeneralBank(SB)")

	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R3")
	inst5(e, asmarm64.OperationSubtract, "$1, R3, R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R21, R7, R9")
	inst5(e, asmarm64.OperationAdd, "R6, R9, R22")
	e.Label(prefix + "_gen_clear_done")
	e.Blank()
}

// emitCallInlineLookupCallInfo extracts the call site index from the instruction word and
// loads the corresponding app.AsmCallInfo entry.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineLookupCallInfo(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight32Bits, "$16, R0, R1")
	e.Blank()
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ASM_CALL_INFO_BASE(R19), R2")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R2, "+labelCIFallback)
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$ACI_SIZE_SHIFT, R1, R3")
	inst5(e, asmarm64.OperationAdd, "R2, R3, R2")
	e.Blank()
}

// emitCallInlineGuardChecks emits the fast-path eligibility and capacity guard checks for
// inline calls.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) emitCallInlineGuardChecks(e *asmgen.Emitter) {
	o.emitCallInlineFastPathDiscriminator(e)
	o.emitCallInlineCoreGuardChecks(e)
}

// emitCallInlineFastPathDiscriminator emits the ACI_IS_FAST_PATH == 0 fallback exit,
// split from EmitCallInlineScalar which skips it because CalleeUsesScalarBanksOnly
// guarantees fast-path eligibility.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineFastPathDiscriminator(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_IS_FAST_PATH(R2), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, "+labelCIFallback)
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "ACI_CLOSURE_PTR(R2), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, ci_static_entry")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CTX_HAS_GOROUTINES(R19), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R4, "+labelCIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CLOSURE_REG(R2), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "16(R5), R4")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R5")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R4, ci_closure_direct")
	inst5(e, asmarm64.OperationMove64Bits, "(R5), R5")
	e.Label("ci_closure_direct")
	inst5(e, asmarm64.OperationCompare, "R3, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelCIFallback)
	e.Label("ci_static_entry")
	e.Blank()
}

// emitCallInlineCoreGuardChecks emits the guard chain shared by EmitCallInline and
// EmitCallInlineScalar, checking frame-depth limit, call-stack capacity and arena
// capacity.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineCoreGuardChecks(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationAdd, "$1, R4, R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_DEPTH_LIMIT(R19), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, "ci_overflow")
	e.Blank()
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_LEN(R19), R6")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelCIFallback)
	e.Blank()
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_INTS(R2), R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_INT_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R6")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallback)
	e.Blank()
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_FLOATS(R2), R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_FLT_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R6")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallback)
	e.Blank()
}

// emitCallInlineSaveCallerState saves the caller's dispatch registers and program counter
// so they can be restored on return.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineSaveCallerState(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R19), R6")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$DS_SIZE_SHIFT, R4, R7")
	inst5(e, asmarm64.OperationAdd, "R6, R7, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R22, DS_CODE_BASE(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R21, DS_CODE_LEN(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R26, DS_INT_CONSTS(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, DS_FLOAT_CONSTS(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STR_CONSTS_BASE(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, DS_STRING_CONSTS(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOL_CONSTS_BASE(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, DS_BOOL_CONSTS(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, DS_UINT_CONSTS(R6)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R8")
	inst5(e, asmarm64.OperationAdd, "R6, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CF_PROGRAM_COUNTER(R8)")
	e.Blank()
}

// emitCallInlineAllocateCalleeFrame computes the callee frame address, updates the frame
// pointer, and saves arena indices for later restoration.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineAllocateCalleeFrame(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R5, R7, R9")
	inst5(e, asmarm64.OperationAdd, "R6, R9, R9")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "R5, CTX_FRAME_POINTER(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R9, CTX_CURRENT_FRAME(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_SHARED_CELLS(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "$0xFFFF, R7")
	inst5(e, asmarm64.OperationMove16Bits, "R7, CF_REGS_LAST_MASK(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_UPVALS_PTR(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CF_UPVALUES_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_UPVALS_LEN(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_UPVALUES_PTR+8)(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_UPVALS_CAP(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (CF_UPVALUES_PTR+16)(R9)")
	e.Blank()

	emitCallInlineSaveArenaIndicesARM64(e)

	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_INT_SLAB(R19), R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R7, R11")
	inst5(e, asmarm64.OperationAdd, "R11, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_INTS(R2), R10")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_INTS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_INTS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_INTS_CAP(R9)")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_ARENA_INT_IDX(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_FLT_SLAB(R19), R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R7, R11")
	inst5(e, asmarm64.OperationAdd, "R11, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_FLOATS(R2), R10")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_FLOATS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_FLOATS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_FLOATS_CAP(R9)")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_ARENA_FLT_IDX(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R7")
	inst5(e, asmarm64.OperationMove8Bits, "R7, CTX_ARENA_BANKS_DIRTY(R19)")
	e.Blank()
}

// emitCallInlineAllocateRegisters allocates register banks for string, bool, and uint
// types, or zeroes them for the fast path.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) emitCallInlineAllocateRegisters(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_IS_FAST_PATH(R2), R10")
	inst5(e, asmarm64.OperationCompare, "$2, R10")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "ci_full_register_alloc")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_CAP(R9)")
	inst5(e, asmarm64.OperationBranch, "ci_register_alloc_done")
	e.Blank()

	e.Label("ci_full_register_alloc")
	o.emitCallInlineAllocateStringRegisters(e)
	o.emitCallInlineAllocateBooleanRegisters(e)
	o.emitCallInlineAllocateUnsignedIntegerRegisters(e)
	o.emitCallInlineAllocateByteSliceRegisters(e)
	o.emitCallInlineRegisterAllocationDone(e)
}

// emitCallInlineAllocateStringRegisters allocates the callee's string register bank from
// the string arena slab, or zeroes the frame fields if the callee requires no string
// registers.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineAllocateStringRegisters(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_STRINGS(R2), R10")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R10, ci_zero_strings")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R11")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R11")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_STR_SLAB(R19), R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R7, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_STRINGS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_STRINGS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_STRINGS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R11, CTX_ARENA_STR_IDX(R19)")
	inst5(e, asmarm64.OperationBranch, "ci_strings_done")
	e.Blank()

	e.Label("ci_zero_strings")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_STRINGS_CAP(R9)")
	e.Blank()

	e.Label("ci_strings_done")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_GENERAL_CAP(R9)")
	e.Blank()
}

// emitCallInlineAllocateBooleanRegisters allocates the callee's boolean register bank
// from the boolean arena slab, or zeroes the frame fields if the callee requires no
// boolean registers.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineAllocateBooleanRegisters(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_BOOLS(R2), R10")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R10, ci_zero_bools")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_BOOL_IDX(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R11")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_BOOL_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R11")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_BOOL_SLAB(R19), R8")
	inst5(e, asmarm64.OperationAdd, "R7, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_BOOLS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_BOOLS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_BOOLS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R11, CTX_ARENA_BOOL_IDX(R19)")
	inst5(e, asmarm64.OperationBranch, "ci_bools_done")
	e.Blank()

	e.Label("ci_zero_bools")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_BOOLS_CAP(R9)")
	e.Blank()

	e.Label("ci_bools_done")
}

// emitCallInlineAllocateUnsignedIntegerRegisters allocates the callee's unsigned integer
// register bank from the uint arena slab, or zeroes the frame fields if the callee
// requires no unsigned integer registers.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineAllocateUnsignedIntegerRegisters(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_UINTS(R2), R10")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R10, ci_zero_uints")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_UINT_IDX(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R11")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_UINT_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R11")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_UINT_SLAB(R19), R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R7, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_UINTS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_UINTS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_UINTS_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R11, CTX_ARENA_UINT_IDX(R19)")
	inst5(e, asmarm64.OperationBranch, "ci_uints_done")
	e.Blank()

	e.Label("ci_zero_uints")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_UINTS_CAP(R9)")
	e.Blank()

	e.Label("ci_uints_done")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_COMPLEX_CAP(R9)")
	e.Blank()
}

// emitCallInlineAllocateByteSliceRegisters allocates the callee's slicesByte register
// bank from the typed-byte arena slab, or zeroes the frame fields if the callee requires
// none.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineAllocateByteSliceRegisters(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_NUM_SLICEBYTE(R2), R10")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R10, ci_zero_slicebyte")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_IDX(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R10, R7, R11")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_CAP(R19), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R11")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_SLAB(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_REGS_SLICEBYTE_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_SLICEBYTE_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R10, CF_REGS_SLICEBYTE_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R11, CTX_ARENA_SLICEBYTE_IDX(R19)")
	inst5(e, asmarm64.OperationBranch, "ci_slicebyte_done")
	e.Blank()

	e.Label("ci_zero_slicebyte")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICEBYTE_CAP(R9)")
	e.Blank()

	e.Label("ci_slicebyte_done")
}

// emitCallInlineRegisterAllocationDone emits the common exit point after all register
// banks have been allocated or zeroed.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineRegisterAllocationDone(e *asmgen.Emitter) {
	e.Label("ci_register_alloc_done")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICESINT_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICESINT_CAP(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICESFLOAT_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICESFLOAT_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, CF_REGS_SLICESSTRING_CAP(R9)")
	e.Blank()
}

// emitCallInlinePopulateFrameFields writes the remaining callee frame fields: function
// pointer, return destination slice, and defer base.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlinePopulateFrameFields(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_FUNCTION(R2), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_FUNCTION(R9)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "ACI_RET_DEST_PTR(R2), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_RETURNDEST_PTR(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_RET_DEST_LEN(R2), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_RETURNDEST_LEN(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_RETURNDEST_CAP(R9)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "CTX_DEFER_STACK_LEN(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CF_DEFERBASE(R9)")
	e.Blank()

	inst5(e, asmarm64.OperationMove8Bits, "ZR, CF_HAS_GENERAL_ALLOC(R9)")
	e.Blank()
}

// emitCallInlineCopyArguments emits the argument copy loops for all five register bank
// types: int, float, string, bool, and uint.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineCopyArguments(e *asmgen.Emitter) {
	emitCallInlineCopyIntegerArguments(e)
	emitCallInlineCopyFloatArguments(e)
	emitCallInlineCopyStringArguments(e)
	emitCallInlineCopyBooleanArguments(e)
	emitCallInlineCopyUnsignedIntegerArguments(e)
	emitCallInlineCopyByteSliceArguments(e)
}

// emitCallInlineReloadDispatchState updates the asmCIBases array and reloads all dispatch
// registers for the callee before issuing DISPATCH_NEXT.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineReloadDispatchState(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ASM_CI_PTRS(R19), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R5")
	emitCalleeCallInfoBase(e)
	inst5(e, asmarm64.OperationMove64Bits, "R8, (R7)(R5<<3)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, CTX_ASM_CALL_INFO_BASE(R19)")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_BODY(R2), R22")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_BODY_LEN(R2), R21")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_INT_CONSTS(R2), R26")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R20")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_INTS_PTR(R9), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R9), R24")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "R22, CTX_CODE_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R21, CTX_CODE_LEN(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R23, CTX_INTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R24, CTX_FLOATS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R26, CTX_INT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_FLT_CONSTS(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_FLT_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_STRINGS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_UINTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_BOOLS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_GENERALS_BASE(R19)")

	inst5(e, asmarm64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_SLICES_BYTE_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_STR_CONSTS(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_STR_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_BOOL_CONSTS(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_BOOL_CONSTS_BASE(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_UINT_CONSTS(R2), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_UINT_CONSTS_BASE(R19)")

	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	e.Instruction(macroDispatchNext)
	e.Blank()
}

// emitCallInlineExitPaths emits the fallback and overflow exit labels for the inline call
// handler. ci_fallback_post_fp_inc rolls back the already-incremented frame pointer so
// the Go-side processExitCall does not index into an uninitialised CallStack slot.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallInlineExitPaths(e *asmgen.Emitter) {
	e.Label(labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R7")
	inst5(e, asmarm64.OperationSubtract, "$1, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_FRAME_POINTER(R19)")

	e.Blank()

	e.Label(labelCIFallback)
	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_CALL, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")

	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
	e.Blank()

	e.Label("ci_overflow")
	inst5(e, asmarm64.OperationSubtract, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_CALL_OVERFLOW, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
}

// emitCalleeCallInfoBase resolves the callee's AsmCallInfo table base into R8, using the
// per-function array for same-root calls and the entry's own ACI_CALLEE_CALL_INFO for
// cross-root calls where the array index is not valid.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitCalleeCallInfoBase(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CTX_CI_BASES_ROOT_MATCH(R19), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, ci_callee_own_call_info")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CI_BASES_BY_FUNC(R19), R8")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_FUNC_INDEX(R2), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R8)(R6<<3), R8")
	inst5(e, asmarm64.OperationBranch, "ci_callee_call_info_resolved")
	e.Blank()

	e.Label("ci_callee_own_call_info")
	inst5(e, asmarm64.OperationMove64Bits, "ACI_CALLEE_CALL_INFO(R2), R8")
	e.Blank()

	e.Label("ci_callee_call_info_resolved")
}
