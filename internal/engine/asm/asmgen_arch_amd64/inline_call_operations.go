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

const (
	// labelCIFallback is the label for the call-inline fallback exit path.
	labelCIFallback = "ci_fallback"

	// labelCICalleeOwnCallInfo is the label for taking the callee's own call-info base from
	// the call-info entry, used when the per-function base array belongs to another root.
	labelCICalleeOwnCallInfo = "ci_callee_own_call_info"

	// labelCICalleeCallInfoResolved is the label where both call-info base routes rejoin.
	labelCICalleeCallInfoResolved = "ci_callee_call_info_resolved"

	// labelRIFallback is the label for the return-inline fallback exit path.
	labelRIFallback = "ri_fallback"

	// labelRINoRetval is the label for the return-inline no-return-value path.
	labelRINoRetval = "ri_no_retval"

	// labelCIFallbackPostFPInc names the call-inline fallback exit path branch reached after
	// the frame-pointer increment.
	labelCIFallbackPostFPInc = "ci_fallback_post_fp_inc"

	// returnInlineLabelPrefix is the prefix shared by every return-inline label name used in
	// the dispatch tables.
	returnInlineLabelPrefix = "ri"

	// returnValueInlineLabelPrefix is the prefix shared by every return-value-inline label
	// name used in the dispatch tables.
	returnValueInlineLabelPrefix = "rvi"
)

// amd64InlineCallOps implements InlineCallOperationsPort for x86-64, where each method
// emits the complete handler body for an inline call or return operation.
type amd64InlineCallOps struct{}

var (
	_ asmgen.InlineCallOperationsPort = (*amd64InlineCallOps)(nil)
)

// EmitTailCallInline emits the top-level body of handlerTailCallInline. The handler is
// NOSPLIT NOFRAME so DISPATCH_NEXT does not leak a leftover prologue frame.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) EmitTailCallInline(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationCall, "\xc2\xb7handlerTailCallInlineSubroutine(SB)")
	e.Instruction(macroDispatchNext)
}

// EmitTailCallInlineSubroutine emits the body of the sub-routine that
// handlerTailCallInline CALLs to perform the actual tail-call work.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) EmitTailCallInlineSubroutine(e *asmgen.Emitter) {
	e.Instruction(asmamd64.InstructionNoLocalPointers)
	inst(e, asmamd64.OperationMove64Bits, "R15, 0(SP)")
	inst(e, asmamd64.OperationCall, "\xc2\xb7asmTailCallExecute(SB)")
	inst(e, asmamd64.OperationMove64Bits, "8(SP), R15")
	inst(e, asmamd64.OperationMove64Bits, "CTX_PC(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_BASE(R15), R12")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_LEN(R15), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INTS_BASE(R15), R8")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLOATS_BASE(R15), R9")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R15), R11")
	inst(e, asmamd64.OperationMove64Bits, "CTX_JUMP_TABLE(R15), R10")
	e.Instruction(asmamd64.OperationReturn)
}

// EmitCallInline emits the body of handlerCallInline.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) EmitCallInline(e *asmgen.Emitter) {
	o.emitCallInlineLookup(e)
	o.emitCallInlineGuardChecks(e)
	o.emitCallInlineSaveCallerState(e)
	o.emitCallInlineComputeCalleeFrame(e)
	o.emitCallInlineAllocateIntFloatRegisters(e)
	o.emitCallInlineAllocateExtendedRegisters(e)
	o.emitCallInlinePopulateFrameFields(e)
	o.emitCallInlineCopyIntegerArguments(e)
	o.emitCallInlineCopyFloatArguments(e)
	o.emitCallInlineCopyStringArguments(e)
	o.emitCallInlineCopyBooleanArguments(e)
	o.emitCallInlineCopyUnsignedIntegerArguments(e)
	o.emitCallInlineCopyByteSliceArguments(e)
	o.emitCallInlineMaybeSetupGeneralBank(e)
	o.emitCallInlineReloadDispatch(e)
	o.emitCallInlineFallbackPaths(e)
}

// EmitCallInlineScalar emits the lean isa.SubOpCallScalar handler body.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) EmitCallInlineScalar(e *asmgen.Emitter) {
	o.emitCallInlineLookup(e)
	o.emitCallInlineCoreGuardChecks(e)
	o.emitCallInlineSaveCallerState(e)
	o.emitCallInlineComputeCalleeFrame(e)
	o.emitCallInlineAllocateIntFloatRegisters(e)
	o.emitCallInlineAllocateExtendedRegisters(e)
	o.emitCallInlinePopulateFrameFields(e)
	o.emitCallInlineCopyIntegerArguments(e)
	o.emitCallInlineCopyFloatArguments(e)
	o.emitCallInlineCopyStringArguments(e)
	o.emitCallInlineCopyBooleanArguments(e)
	o.emitCallInlineCopyUnsignedIntegerArguments(e)
	o.emitCallInlineCopyByteSliceArguments(e)
	o.emitCallInlineReloadDispatch(e)
	o.emitCallInlineFallbackPaths(e)
}

// EmitReturnInline emits the body of handlerReturnInline.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) EmitReturnInline(e *asmgen.Emitter) {
	o.emitReturnInlineGuardChecks(e)
	o.emitReturnInlineDispatchReturnType(e)
	o.emitReturnInlineCopyIntegerReturn(e)
	o.emitReturnInlineCopyFloatReturn(e)
	o.emitReturnInlineCopyStringReturn(e)
	o.emitReturnInlineCopyBooleanReturn(e)
	o.emitReturnInlineCopyUnsignedIntegerReturn(e)
	emitReturnInlineCopyGeneralReturnAMD64(e)
	o.emitReturnInlineCopyMultipleReturns(e)
	o.emitReturnInlineClearStringArena(e, returnInlineLabelPrefix, true)
	o.emitReturnInlineMaybeClearGeneralBank(e, returnInlineLabelPrefix)
	o.emitReturnInlineRestoreCallerState(e, returnInlineLabelPrefix)
	o.emitReturnInlineFallbackPath(e, returnInlineLabelPrefix, "EXIT_RETURN")
}

// EmitReturnVoidInline emits the body of handlerReturnVoidInline. Same as ReturnInline
// but skips the return value copy entirely.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) EmitReturnVoidInline(e *asmgen.Emitter) {
	o.emitReturnVoidInlineGuardChecks(e)
	o.emitReturnInlineClearStringArena(e, returnValueInlineLabelPrefix, false)
	o.emitReturnInlineMaybeClearGeneralBank(e, returnValueInlineLabelPrefix)
	o.emitReturnInlineRestoreCallerState(e, returnValueInlineLabelPrefix)
	o.emitReturnInlineFallbackPath(e, returnValueInlineLabelPrefix, "EXIT_RETURN_VOID")
}

// EmitCallInlineSetupGeneralBank emits the body of the trampoline that finishes
// general-bank setup for an inline call when the callee's isFastPath == 3.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (*amd64InlineCallOps) EmitCallInlineSetupGeneralBank(e *asmgen.Emitter) {
	e.Instruction(asmamd64.InstructionNoLocalPointers)
	inst(e, asmamd64.OperationMove64Bits, "AX, 24(SP)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_SAVED_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R15, 0(SP)")
	inst(e, asmamd64.OperationMove64Bits, "AX, 8(SP)")
	inst(e, asmamd64.OperationCall, "·asmCallSetupGeneralBank(SB)")
	inst(e, asmamd64.OperationMove64Bits, "16(SP), R15")
	inst(e, asmamd64.OperationMove64Bits, "CTX_SAVED_PC(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_BASE(R15), R12")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_LEN(R15), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INTS_BASE(R15), R8")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLOATS_BASE(R15), R9")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R15), R11")
	inst(e, asmamd64.OperationMove64Bits, "CTX_JUMP_TABLE(R15), R10")
	inst(e, asmamd64.OperationMove64Bits, "24(SP), AX")
	e.Instruction(asmamd64.OperationReturn)
}

// EmitCallInlineClearGeneralBank emits the return-side trampoline that zeroes the
// GC-visible general slab range of the popped callee.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
func (*amd64InlineCallOps) EmitCallInlineClearGeneralBank(e *asmgen.Emitter) {
	e.Instruction(asmamd64.InstructionNoLocalPointers)
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_SAVED_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R15, 0(SP)")
	inst(e, asmamd64.OperationCall, "·asmReturnClearGeneralBank(SB)")
	inst(e, asmamd64.OperationMove64Bits, "8(SP), R15")
	inst(e, asmamd64.OperationMove64Bits, "CTX_SAVED_PC(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_BASE(R15), R12")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_LEN(R15), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INTS_BASE(R15), R8")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLOATS_BASE(R15), R9")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R15), R11")
	inst(e, asmamd64.OperationMove64Bits, "CTX_JUMP_TABLE(R15), R10")
	e.Instruction(asmamd64.OperationReturn)
}

// emitReturnInlineMaybeClearGeneralBank emits a conditional CALL to
// handlerCallInlineClearGeneralBank when the returning callee allocated general-bank
// slots.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes prefix (string) which selects the label namespace.
func (*amd64InlineCallOps) emitReturnInlineMaybeClearGeneralBank(e *asmgen.Emitter, prefix string) {
	inst(e, asmamd64.OperationCompare8Bits, "CF_HAS_GENERAL_ALLOC(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, prefix+"_gen_clear_done")

	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, prefix+"_gen_clear_slow")
	inst(e, asmamd64.OperationMove64Bits, "CF_ARENA_SAVE_GENERAL_IDX(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R15), DX")
	inst(e, asmamd64.OperationSubtract64Bits, "CX, DX")
	inst(e, asmamd64.OperationJumpIfLessOrEqualSigned, prefix+"_gen_clear_restore")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$3, BX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_ARENA_GENERAL_SLAB(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "BX, SI")
	e.Label(prefix + "_gen_clear_zero")
	inst(e, asmamd64.OperationMove64Bits, "$0, (SI)")
	inst(e, asmamd64.OperationMove64Bits, "$0, 8(SI)")
	inst(e, asmamd64.OperationMove64Bits, "$0, 16(SI)")
	inst(e, asmamd64.OperationAdd64Bits, "$24, SI")
	inst(e, asmamd64.OperationDecrement64Bits, "DX")
	inst(e, asmamd64.OperationJumpIfNotZero, prefix+"_gen_clear_zero")
	e.Label(prefix + "_gen_clear_restore")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_ARENA_GEN_IDX(R15)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CTX_ARENA_BANKS_DIRTY(R15)")
	inst(e, asmamd64.OperationJump, prefix+"_gen_clear_done")

	e.Label(prefix + "_gen_clear_slow")
	inst(e, asmamd64.OperationCall, "·handlerCallInlineClearGeneralBank(SB)")

	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "-1(SI), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CSTACK_BASE(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationMove64Bits, "R13, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DX*1), R12")
	e.Label(prefix + "_gen_clear_done")
	e.Blank()
}

// emitCallInlineLookup extracts the call site index from the operand word and resolves
// the corresponding asmCallInfo entry into AX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineLookup(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, CX")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ASM_CALL_INFO_BASE(R15), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, labelCIFallback)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$ACI_SIZE_SHIFT, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(CX*1), AX")
	e.Blank()
}

// emitCallInlineGuardChecks emits the fast-path eligibility checks that determine whether
// the call can be handled entirely in assembly.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) emitCallInlineGuardChecks(e *asmgen.Emitter) {
	o.emitCallInlineFastPathDiscriminator(e)
	o.emitCallInlineCoreGuardChecks(e)
}

// emitCallInlineFastPathDiscriminator emits the isFastPath == 0 fallback exit. Split out
// so EmitCallInlineScalar can skip it.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineFastPathDiscriminator(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare64Bits, "ACI_IS_FAST_PATH(AX), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelCIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "ACI_CLOSURE_PTR(AX), BX")
	inst(e, asmamd64.OperationTest64Bits, "BX, BX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_static_entry")
	inst(e, asmamd64.OperationCompare8Bits, "CTX_HAS_GOROUTINES(R15), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelCIFallback)
	inst(e, asmamd64.OperationMove64Bits, "ACI_CLOSURE_REG(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(SI)(CX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), SI")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_closure_direct")
	inst(e, asmamd64.OperationMove64Bits, "(SI), SI")
	e.Label("ci_closure_direct")
	inst(e, asmamd64.OperationCompare64Bits, "SI, BX")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelCIFallback)
	e.Label("ci_static_entry")
	e.Blank()
}

// emitCallInlineCoreGuardChecks emits the runtime guards shared by EmitCallInline and
// EmitCallInlineScalar, covering frame-depth limit, call-stack capacity and int/float
// arena capacity.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCoreGuardChecks(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "1(SI), DI")
	inst(e, asmamd64.OperationCompare64Bits, "DI, CTX_DEPTH_LIMIT(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, "ci_overflow")
	e.Blank()

	inst(e, asmamd64.OperationCompare64Bits, "DI, CTX_CSTACK_LEN(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelCIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R15), BX")
	inst(e, asmamd64.OperationAdd64Bits, "ACI_CALLEE_NUM_INTS(AX), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CTX_ARENA_INT_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R15), BX")
	inst(e, asmamd64.OperationAdd64Bits, "ACI_CALLEE_NUM_FLOATS(AX), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CTX_ARENA_FLT_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallback)
	e.Blank()
}

// emitCallInlineSaveCallerState saves the caller's dispatch registers and program counter
// into the per-frame dispatch save slot so they can be restored when the callee returns.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineSaveCallerState(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "SI, CX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$DS_SIZE_SHIFT, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(CX*1), BX")
	inst(e, asmamd64.OperationMove64Bits, "R12, DS_CODE_BASE(BX)")
	inst(e, asmamd64.OperationMove64Bits, "R13, DS_CODE_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "R11, DS_INT_CONSTS(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLT_CONSTS_BASE(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, DS_FLOAT_CONSTS(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STR_CONSTS_BASE(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, DS_STRING_CONSTS(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOL_CONSTS_BASE(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, DS_BOOL_CONSTS(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, DS_UINT_CONSTS(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_CSTACK_BASE(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "SI, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(CX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "R14, CF_PROGRAM_COUNTER(CX)")
	e.Blank()
}

// emitCallInlineComputeCalleeFrame computes the callee's call frame pointer and snapshots
// all arena indices into the callee frame's arenaSave block.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineComputeCalleeFrame(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "DI, DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DX*1), BX")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "DI, CTX_FRAME_POINTER(R15)")
	inst(e, asmamd64.OperationMove64Bits, "BX, CTX_CURRENT_FRAME(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "$0, CF_SHARED_CELLS(BX)")
	inst(e, asmamd64.OperationMove16Bits, "$0xFFFF, CF_REGS_LAST_MASK(BX)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_UPVALS_PTR(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_UPVALUES_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_UPVALS_LEN(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_UPVALUES_PTR+8)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_UPVALS_CAP(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_UPVALUES_PTR+16)(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+0)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+8)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+16)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_GEN_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+24)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_BOOL_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+32)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_UINT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+40)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_CPLX_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+48)(BX)")

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEINT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+56)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEFLT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+64)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICESTR_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+72)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEBOOL_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+80)(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEUINT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+88)(BX)")

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (CF_ARENA_SAVE+96)(BX)")
	e.Blank()
}

// emitCallInlineAllocateIntFloatRegisters allocates the integer and float register bank
// slabs from their respective arenas.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineAllocateIntFloatRegisters(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_INT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_INT_SLAB(R15), DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*8), DX")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_INTS(AX), DI")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_INTS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_INTS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_INTS_CAP(BX)")
	inst(e, asmamd64.OperationAdd64Bits, "DI, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_ARENA_INT_IDX(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_FLT_IDX(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_FLT_SLAB(R15), DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*8), DX")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_FLOATS(AX), DI")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_FLOATS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_FLOATS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_FLOATS_CAP(BX)")
	inst(e, asmamd64.OperationAdd64Bits, "DI, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_ARENA_FLT_IDX(R15)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CTX_ARENA_BANKS_DIRTY(R15)")
	e.Blank()
}

// emitCallInlineAllocateExtendedRegisters allocates the string, bool, uint and byte-slice
// register bank slabs, or zeroes them when isFastPath == 2.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) emitCallInlineAllocateExtendedRegisters(e *asmgen.Emitter) {
	o.emitCallInlineFastPathZeroAllBanks(e)
	e.Label("ci_full_register_alloc")
	o.emitCallInlineAllocateStringRegisters(e)
	o.emitCallInlineAllocateBooleanRegisters(e)
	o.emitCallInlineAllocateUnsignedIntegerRegisters(e)
	o.emitCallInlineAllocateByteSliceRegisters(e)
}

// emitCallInlineFastPathZeroAllBanks zeroes all non-int/float register slices when
// isFastPath == 2, bypassing per-bank allocation.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineFastPathZeroAllBanks(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare64Bits, "ACI_IS_FAST_PATH(AX), $2")
	inst(e, asmamd64.OperationJumpIfNotEqual, "ci_full_register_alloc")
	e.Blank()
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_STRINGS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_STRINGS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_STRINGS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_BOOLS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_BOOLS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_BOOLS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_UINTS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_UINTS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_UINTS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICEBYTE_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICEBYTE_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICEBYTE_CAP(BX)")
	inst(e, asmamd64.OperationJump, "ci_register_alloc_done")
	e.Blank()
}

// emitCallInlineAllocateStringRegisters allocates the callee's string register bank from
// the string arena slab. Also zeroes the general register slice since generics are never
// allocated on the ASM fast path.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineAllocateStringRegisters(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_STRINGS(AX), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_zero_strings")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_ARENA_STR_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_STR_SLAB(R15), DX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_STRINGS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_STRINGS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_STRINGS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "SI, CTX_ARENA_STR_IDX(R15)")
	inst(e, asmamd64.OperationJump, "ci_strings_done")
	e.Blank()
	e.Label("ci_zero_strings")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_STRINGS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_STRINGS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_STRINGS_CAP(BX)")
	e.Blank()
	e.Label("ci_strings_done")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_GENERAL_CAP(BX)")
	e.Blank()
}

// emitCallInlineAllocateBooleanRegisters allocates the callee's boolean register bank
// from the boolean arena slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineAllocateBooleanRegisters(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_BOOLS(AX), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_zero_bools")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_BOOL_IDX(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_ARENA_BOOL_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_BOOL_SLAB(R15), DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_BOOLS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_BOOLS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_BOOLS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "SI, CTX_ARENA_BOOL_IDX(R15)")
	inst(e, asmamd64.OperationJump, "ci_bools_done")
	e.Blank()
	e.Label("ci_zero_bools")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_BOOLS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_BOOLS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_BOOLS_CAP(BX)")
	e.Blank()
}

// emitCallInlineAllocateUnsignedIntegerRegisters allocates the callee's uint register
// bank from the uint arena slab. Also zeroes the complex register slice since complex
// registers are never allocated on the ASM fast path.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineAllocateUnsignedIntegerRegisters(e *asmgen.Emitter) {
	e.Label("ci_bools_done")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_UINTS(AX), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_zero_uints")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_UINT_IDX(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_ARENA_UINT_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_UINT_SLAB(R15), DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*8), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_UINTS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_UINTS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_UINTS_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "SI, CTX_ARENA_UINT_IDX(R15)")
	inst(e, asmamd64.OperationJump, "ci_uints_done")
	e.Blank()
	e.Label("ci_zero_uints")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_UINTS_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_UINTS_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_UINTS_CAP(BX)")
	e.Blank()
	e.Label("ci_uints_done")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_COMPLEX_CAP(BX)")
	e.Blank()
}

// emitCallInlineAllocateByteSliceRegisters allocates the callee's slicesByte register
// bank from the typed-byte arena slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineAllocateByteSliceRegisters(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_NUM_SLICEBYTE(AX), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "ci_zero_slicebyte")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_IDX(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(DI*1), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_ARENA_SLICEBYTE_CAP(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_SLICEBYTE_SLAB(R15), DX")

	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(CX*1), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_REGS_SLICEBYTE_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_SLICEBYTE_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, CF_REGS_SLICEBYTE_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "SI, CTX_ARENA_SLICEBYTE_IDX(R15)")
	inst(e, asmamd64.OperationJump, "ci_slicebyte_done")
	e.Blank()
	e.Label("ci_zero_slicebyte")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_SLICEBYTE_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_SLICEBYTE_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, CF_REGS_SLICEBYTE_CAP(BX)")
	e.Blank()
	e.Label("ci_slicebyte_done")

	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
}

// emitCallInlinePopulateFrameFields writes the remaining callee call frame fields not
// covered by register bank allocation.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlinePopulateFrameFields(e *asmgen.Emitter) {
	e.Label("ci_register_alloc_done")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICESINT_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICESINT_CAP(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICESFLOAT_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICESFLOAT_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "CX, CF_REGS_SLICESSTRING_CAP(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove8Bits, "$0, CF_HAS_GENERAL_ALLOC(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_FUNCTION(AX), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_FUNCTION(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "ACI_RET_DEST_PTR(AX), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_RETURNDEST_PTR(BX)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_RET_DEST_LEN(AX), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_RETURNDEST_LEN(BX)")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_RETURNDEST_CAP(BX)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_DEFER_STACK_LEN(R15), DX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CF_DEFERBASE(BX)")
	e.Blank()
}

// emitCallInlineCopyIntegerArguments copies the caller's integer argument values into the
// callee's integer register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyIntegerArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_INT_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_int_args")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_INTS_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_int_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_INT_ARG_SRCS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(SI*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DI)(DX*8)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_int_loop")
	e.Blank()

	e.Label("ci_no_int_args")
	e.Blank()
}

// emitCallInlineCopyFloatArguments copies the caller's float argument values into the
// callee's float register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyFloatArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_FLOAT_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_float_args")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_float_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_FLOAT_ARG_SRCS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "(R9)(SI*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DI)(DX*8)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_float_loop")
	e.Blank()

	e.Label("ci_no_float_args")
	e.Blank()
}

// emitCallInlineCopyStringArguments copies the caller's string argument values (16-byte
// headers) into the callee's string register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyStringArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_STRING_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_string_args")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_string_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_STRING_ARG_SRCS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, SI")
	inst(e, asmamd64.OperationMove64Bits, "(R14)(SI*1), R13")
	inst(e, asmamd64.OperationMove64Bits, "8(R14)(SI*1), SI")
	inst(e, asmamd64.OperationMove64Bits, "DX, R12")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, R12")
	inst(e, asmamd64.OperationMove64Bits, "R13, (DI)(R12*1)")
	inst(e, asmamd64.OperationMove64Bits, "SI, 8(DI)(R12*1)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_string_loop")
	e.Blank()

	e.Label("ci_no_string_args")
	e.Blank()
}

// emitCallInlineCopyBooleanArguments copies the caller's boolean argument values (1-byte
// each) into the callee's boolean register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyBooleanArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_BOOL_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_bool_args")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_bool_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_BOOL_ARG_SRCS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(R14)(SI*1), SI")
	inst(e, asmamd64.OperationMove8Bits, "SI, (DI)(DX*1)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_bool_loop")
	e.Blank()

	e.Label("ci_no_bool_args")
	e.Blank()
}

// emitCallInlineCopyUnsignedIntegerArguments copies the caller's unsigned integer
// argument values into the callee's uint register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyUnsignedIntegerArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_UINT_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_uint_args")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_UINTS_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_uint_loop")
	inst(e, asmamd64.OperationMove64Bits, "(ACI_UINT_ARG_SRCS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "(R14)(SI*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (DI)(DX*8)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_uint_loop")
	e.Blank()

	e.Label("ci_no_uint_args")
	e.Blank()
}

// emitCallInlineCopyByteSliceArguments copies the caller's []byte argument values
// (24-byte slice headers) into the callee's slicesByte register slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineCopyByteSliceArguments(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_NUM_SLICEBYTE_ARGS(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ci_no_slicebyte_args")
	inst(e, asmamd64.OperationMove64Bits, "CTX_SLICES_BYTE_BASE(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(BX), DI")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Blank()

	e.Label("ci_slicebyte_loop")

	inst(e, asmamd64.OperationMove64Bits, "(ACI_SLICEBYTE_ARG_SRCS)(AX)(DX*8), SI")

	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, SI")

	inst(e, asmamd64.OperationMove64Bits, "DX, R12")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, R12")

	inst(e, asmamd64.OperationMove64Bits, "0(R14)(SI*1), R13")
	inst(e, asmamd64.OperationMove64Bits, "R13, 0(DI)(R12*1)")
	inst(e, asmamd64.OperationMove64Bits, "8(R14)(SI*1), R13")
	inst(e, asmamd64.OperationMove64Bits, "R13, 8(DI)(R12*1)")
	inst(e, asmamd64.OperationMove64Bits, "16(R14)(SI*1), R13")
	inst(e, asmamd64.OperationMove64Bits, "R13, 16(DI)(R12*1)")
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, "ci_slicebyte_loop")
	e.Blank()

	e.Label("ci_no_slicebyte_args")
	e.Blank()
}

// emitCallInlineReloadDispatch reloads all dispatch registers and cached base pointers
// for the callee frame and emits DISPATCH_NEXT to begin executing the callee's first
// instruction.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineReloadDispatch(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_ASM_CI_PTRS(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), DI")

	emitCalleeCallInfoBase(e)
	inst(e, asmamd64.OperationMove64Bits, "DX, (CX)(DI*8)")
	inst(e, asmamd64.OperationMove64Bits, "DX, CTX_ASM_CALL_INFO_BASE(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_BODY(AX), R12")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_BODY_LEN(AX), R13")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_INT_CONSTS(AX), R11")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "R14, R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_INTS_PTR(BX), R8")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(BX), R9")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "R12, CTX_CODE_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R13, CTX_CODE_LEN(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R8, CTX_INTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R9, CTX_FLOATS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R11, CTX_INT_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_FLT_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_FLT_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_STRINGS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_UINTS_PTR(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_UINTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_BOOLS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_GENERALS_BASE(R15)")

	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_SLICES_BYTE_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_STR_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_STR_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_BOOL_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_BOOL_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_UINT_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_UINT_CONSTS_BASE(R15)")
	e.Instruction(macroDispatchNext)
	e.Blank()
}

// emitCallInlineFallbackPaths emits the exit paths for when the inline call cannot
// proceed. ci_fallback_post_fp_inc rolls back ctx.framePointer first to avoid indexing an
// uninitialised callStack slot.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallInlineFallbackPaths(e *asmgen.Emitter) {
	e.Label(labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), CX")
	inst(e, asmamd64.OperationDecrement64Bits, "CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_FRAME_POINTER(R15)")

	e.Blank()

	e.Label(labelCIFallback)
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_CALL, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
	e.Blank()

	e.Label("ci_overflow")
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_CALL_OVERFLOW, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// emitReturnInlineGuardChecks emits the eligibility checks for the inline return path,
// verifying that the frame is not the base frame, no defers have been pushed, and the
// return count is zero or one.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineGuardChecks(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_BASE_FRAME_POINTER(R15)")
	inst(e, asmamd64.OperationJumpIfLessOrEqualSigned, labelRIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_CSTACK_BASE(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationMove64Bits, "SI, DI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DI*1), DI")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_DEFERBASE(DI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, CTX_DEFER_STACK_LEN(R15)")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelRIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_SIMPLEDEFER(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ri_no_simple_defer")
	inst(e, asmamd64.OperationCompare8Bits, "SDR_ACTIVE(CX), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelRIFallback)
	e.Label("ri_no_simple_defer")
	e.Blank()
	emitReturnInlineFrameOwnershipGuards(e, "DI", labelRIFallback)

	inst(e, asmamd64.OperationMove32Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight32Bits, "$24, AX")
	inst(e, asmamd64.OperationBitwiseAnd32Bits, "$0xFF, AX")
	e.Blank()

	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "-1(SI), R13")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationMove64Bits, "R13, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DX*1), R12")
	e.Blank()

	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, labelRINoRetval)
	inst(e, asmamd64.OperationCompare64Bits, "AX, $1")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelRIMulti)
	e.Blank()
}

// emitReturnInlineFrameOwnershipGuards falls back to Go when the callee frame cannot pop
// inline because a root swap or closure allocation requires Go-side cleanup.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes frameRegister (string) which names the register holding the callee frame pointer.
// Takes fallback (string) which is the label of the Go return path.
func emitReturnInlineFrameOwnershipGuards(e *asmgen.Emitter, frameRegister string, fallback string) {
	inst(e, asmamd64.OperationCompare8Bits, "CF_ROOT_SWAPPED("+frameRegister+"), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, fallback)
	inst(e, asmamd64.OperationCompare64Bits, "CF_SHARED_CELLS("+frameRegister+"), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, fallback)
	e.Blank()
}

// emitReturnInlineDispatchReturnType loads the return destination descriptor and
// dispatches to the appropriate type-specific copy path based on the kind byte. Upvalue
// destinations and unsupported kinds fall back to the Go handler.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineDispatchReturnType(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CF_RETURNDEST_PTR(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, labelRIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_IS_UPVALUE(CX), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfNotZero, labelRIFallback)
	e.Blank()

	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_KIND(CX), AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "VL_REGISTER(CX), CX")
	e.Blank()

	inst(e, asmamd64.OperationCompare64Bits, "AX, $0")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_int")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $1")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_float")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $2")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_string")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $4")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_bool")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $5")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_uint")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $3")
	inst(e, asmamd64.OperationJumpIfEqual, "ri_check_general")
	inst(e, asmamd64.OperationJump, labelRIFallback)
	e.Blank()
}

// emitReturnInlineCopyIntegerReturn copies a single integer return value from the
// callee's first int register into the caller's int register bank at destination CX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyIntegerReturn(e *asmgen.Emitter) {
	e.Label("ri_check_int")
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_INTS_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	e.Blank()

	e.Label("ri_copy_int")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_INTS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_INTS_PTR(R12), BX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (BX)(CX*8)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyFloatReturn copies a single float return value from the callee's
// first float register into the caller's float register bank at destination CX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyFloatReturn(e *asmgen.Emitter) {
	e.Label("ri_check_float")
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_FLOATS_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	e.Blank()

	e.Label("ri_copy_float")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R12), BX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (BX)(CX*8)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyStringReturn copies a single string return value (a 16-byte
// pointer+length pair) from the callee's first string register into the caller's string
// register bank at destination CX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyStringReturn(e *asmgen.Emitter) {
	e.Label("ri_check_string")
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_STRINGS_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX), SI")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R12), BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, CX")
	inst(e, asmamd64.OperationMove64Bits, "SI, (BX)(CX*1)")
	inst(e, asmamd64.OperationMove64Bits, "AX, 8(BX)(CX*1)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyBooleanReturn copies a single boolean return value from the
// callee's first bool register into the caller's bool register bank at destination CX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyBooleanReturn(e *asmgen.Emitter) {
	e.Label("ri_check_bool")
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_BOOLS_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(AX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R12), BX")
	inst(e, asmamd64.OperationMove8Bits, "AX, (BX)(CX*1)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineCopyUnsignedIntegerReturn copies a single unsigned integer return value
// from the callee's first uint register into the caller's uint register bank at
// destination CX.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineCopyUnsignedIntegerReturn(e *asmgen.Emitter) {
	e.Label("ri_check_uint")
	inst(e, asmamd64.OperationCompare64Bits, "CF_REGS_UINTS_LEN(DI), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelRIFallback)
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_UINTS_PTR(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R12), BX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (BX)(CX*8)")
	inst(e, asmamd64.OperationJump, labelRINoRetval)
	e.Blank()
}

// emitReturnInlineClearStringArena zeroes out string arena entries allocated by the
// callee so the GC does not see stale string pointers, then restores all arena indices
// from the callee frame's arenaSave block.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes prefix (string) which selects the label namespace.
// Takes emitNoRetvalLabel (bool) which controls whether a no-retval label is emitted.
func (*amd64InlineCallOps) emitReturnInlineClearStringArena(e *asmgen.Emitter, prefix string, emitNoRetvalLabel bool) {
	if emitNoRetvalLabel {
		e.Label(prefix + "_no_retval")
	}
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_STR_IDX(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(DI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, SI")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, prefix+"_str_clear_done")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_STR_SLAB(R15), DX")
	inst(e, asmamd64.OperationMove64Bits, "CX, AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(AX*1), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DX)(SI*1), SI")
	e.Blank()

	e.Label(prefix + "_str_clear_loop")
	inst(e, asmamd64.OperationMove64Bits, "$0, (AX)")
	inst(e, asmamd64.OperationMove64Bits, "$0, 8(AX)")
	inst(e, asmamd64.OperationAdd64Bits, "$16, AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, SI")
	inst(e, asmamd64.OperationJumpIfLessSigned, prefix+"_str_clear_loop")
	e.Blank()

	e.Label(prefix + "_str_clear_done")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+0)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_INT_IDX(R15)")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+8)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_FLT_IDX(R15)")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+16)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_STR_IDX(R15)")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+32)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_BOOL_IDX(R15)")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+40)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_UINT_IDX(R15)")
	inst(e, asmamd64.OperationMove64Bits, "(CF_ARENA_SAVE+96)(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ARENA_SLICEBYTE_IDX(R15)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CTX_ARENA_BANKS_DIRTY(R15)")
	e.Blank()
}

// emitReturnInlineRestoreCallerState pops the callee frame and restores the caller's
// complete dispatch state, including all pinned registers and cached base pointers.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnInlineRestoreCallerState(e *asmgen.Emitter, _ string) {
	inst(e, asmamd64.OperationMove64Bits, "R13, CTX_FRAME_POINTER(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R12, CTX_CURRENT_FRAME(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_ASM_CI_PTRS(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "(AX)(R13*8), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_ASM_CALL_INFO_BASE(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_PROGRAM_COUNTER(R12), R14")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_INTS_PTR(R12), R8")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_FLOATS_PTR(R12), R9")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_STRINGS_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_STRINGS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_UINTS_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_UINTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_BOOLS_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_BOOLS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_GENERAL_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_GENERALS_BASE(R15)")

	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICEBYTE_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_BYTE_BASE(R15)")
	emitAMD64RestoreTypedSliceBanks(e)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_DISPATCH_SAVES(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "R13, CX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$DS_SIZE_SHIFT, CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(CX*1), AX")
	inst(e, asmamd64.OperationMove64Bits, "DS_CODE_BASE(AX), R12")
	inst(e, asmamd64.OperationMove64Bits, "DS_CODE_LEN(AX), R13")
	inst(e, asmamd64.OperationMove64Bits, "DS_INT_CONSTS(AX), R11")
	inst(e, asmamd64.OperationMove64Bits, "DS_FLOAT_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_FLT_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "DS_STRING_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_STR_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "DS_BOOL_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_BOOL_CONSTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "DS_UINT_CONSTS(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_UINT_CONSTS_BASE(R15)")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "R12, CTX_CODE_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R13, CTX_CODE_LEN(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R8, CTX_INTS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R9, CTX_FLOATS_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R11, CTX_INT_CONSTS_BASE(R15)")
	e.Instruction(macroDispatchNext)
	e.Blank()
}

// emitReturnInlineFallbackPath emits the exit path for when the inline return cannot
// proceed, storing the given exit reason and returning to Go.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes prefix (string) which selects the label namespace.
// Takes exitReason (string) which is the assembly exit constant name.
func (*amd64InlineCallOps) emitReturnInlineFallbackPath(e *asmgen.Emitter, prefix string, exitReason string) {
	e.Label(prefix + "_fallback")
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$"+exitReason+", CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// emitReturnVoidInlineGuardChecks emits the eligibility checks for the inline void return
// path, verifying the frame is not the base frame and no defers have been pushed.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitReturnVoidInlineGuardChecks(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, CTX_BASE_FRAME_POINTER(R15)")
	inst(e, asmamd64.OperationJumpIfLessOrEqualSigned, "rvi_fallback")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CTX_CSTACK_BASE(R15), BX")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationMove64Bits, "SI, DI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DI*1), DI")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_DEFERBASE(DI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, CTX_DEFER_STACK_LEN(R15)")
	inst(e, asmamd64.OperationJumpIfNotEqual, "rvi_fallback")
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "CF_SIMPLEDEFER(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "rvi_no_simple_defer")
	inst(e, asmamd64.OperationCompare8Bits, "SDR_ACTIVE(CX), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "rvi_fallback")
	e.Label("rvi_no_simple_defer")
	e.Blank()
	emitReturnInlineFrameOwnershipGuards(e, "DI", "rvi_fallback")

	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "-1(SI), R13")
	inst(e, asmamd64.OperationMove64Bits, "$CALLFRAME_SIZE, CX")
	inst(e, asmamd64.OperationMove64Bits, "R13, DX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "CX, DX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(DX*1), R12")
	e.Blank()
}

// emitCalleeCallInfoBase leaves the callee's AsmCallInfo table base in DX.
//
// The per-function base array is only valid while the VM runs in the root the callee was
// built for. After a root swap the entry's own ACI_CALLEE_CALL_INFO is used instead,
// because the array index would name an unrelated function in the new bundle.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitCalleeCallInfoBase(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "CTX_CI_BASES_ROOT_MATCH(R15), $0")
	inst(e, asmamd64.OperationJumpIfEqual, labelCICalleeOwnCallInfo)
	inst(e, asmamd64.OperationMove64Bits, "CTX_CI_BASES_BY_FUNC(R15), DX")
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_FUNC_INDEX(AX), SI")
	inst(e, asmamd64.OperationMove64Bits, "(DX)(SI*8), DX")
	inst(e, asmamd64.OperationJump, labelCICalleeCallInfoResolved)
	e.Blank()

	e.Label(labelCICalleeOwnCallInfo)
	inst(e, asmamd64.OperationMove64Bits, "ACI_CALLEE_CALL_INFO(AX), DX")
	e.Blank()

	e.Label(labelCICalleeCallInfoResolved)
}

// emitAMD64RestoreTypedSliceBanks restores typed-slice and complex bank bases from the
// caller's callFrame. Without this, an inline return can leave ctx bank bases pointing at
// the callee's deallocated banks when the caller's refresh mask does not cover them.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitAMD64RestoreTypedSliceBanks(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICESINT_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_INT_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICESFLOAT_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_FLOAT_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICESSTRING_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_STRING_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICESBOOL_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_BOOL_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_SLICESUINT_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_SLICES_UINT_BASE(R15)")
	inst(e, asmamd64.OperationMove64Bits, "CF_REGS_COMPLEX_PTR(R12), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, CTX_COMPLEX_BASE(R15)")
}
