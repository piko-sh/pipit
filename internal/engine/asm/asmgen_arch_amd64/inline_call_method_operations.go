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
	// labelCIMethodScan names the entry point for the receiver-type table scan of the inline
	// method-call handler.
	labelCIMethodScan = "ci_method_scan"

	// labelCIMethodLoop names the iteration label for the receiver-type table scan of the
	// inline method-call handler.
	labelCIMethodLoop = "ci_method_loop"

	// labelCIMethodHit names the match label for the receiver-type table scan of the inline
	// method-call handler.
	labelCIMethodHit = "ci_method_hit"
)

// EmitCallMethodInline emits the body of handlerCallMethodInline, selecting the call
// entry by receiver type and sharing every frame-push stage with EmitCallInline.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (o *amd64InlineCallOps) EmitCallMethodInline(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	o.emitCallInlineLookup(e)
	o.emitCallMethodInlineSelectEntry(e)
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
	o.emitCallMethodInlineFallbackPaths(e)
}

// emitCallMethodInlineSelectEntry retargets AX at the per-receiver-type entry that
// matches the receiver's type word, jumping to ci_fallback on a nil, interface-kind or
// missing type.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallMethodInlineSelectEntry(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "ACI_METHOD_ENTRY_COUNT(AX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, labelCIFallback)
	inst(e, asmamd64.OperationCompare8Bits, "CTX_HAS_GOROUTINES(R15), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelCIFallback)
	inst(e, asmamd64.OperationMove64Bits, "ACI_RECEIVER_REG(AX), SI")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(SI)(SI*2), SI")
	inst(e, asmamd64.OperationMove64Bits, "(DI)(SI*8), SI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, labelCIFallback)
	inst(e, asmamd64.OperationMove8To64BitsZeroExtended, "ABI_TYPE_KIND_BYTE(SI), DX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$FLAG_KIND_MASK, DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, $REFLECT_INTERFACE")
	inst(e, asmamd64.OperationJumpIfEqual, labelCIFallback)
	inst(e, asmamd64.OperationCompare64Bits, "DX, $REFLECT_POINTER")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelCIMethodScan)
	inst(e, asmamd64.OperationMove64Bits, "ABI_TYPE_PTR_ELEM(SI), SI")
	e.Label(labelCIMethodScan)
	inst(e, asmamd64.OperationBitwiseXor64Bits, "DX, DX")
	e.Label(labelCIMethodLoop)
	inst(e, asmamd64.OperationCompare64Bits, "(ACI_METHOD_TYPE_WORDS)(AX)(DX*8), SI")
	inst(e, asmamd64.OperationJumpIfEqual, labelCIMethodHit)
	inst(e, asmamd64.OperationIncrement64Bits, "DX")
	inst(e, asmamd64.OperationCompare64Bits, "DX, CX")
	inst(e, asmamd64.OperationJumpIfLessSigned, labelCIMethodLoop)
	inst(e, asmamd64.OperationJump, labelCIFallback)
	e.Label(labelCIMethodHit)
	inst(e, asmamd64.OperationShiftLeft64Bits, "$ACI_SIZE_SHIFT, DX")
	inst(e, asmamd64.OperationMove64Bits, "ACI_METHOD_ENTRIES_PTR(AX), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(DX*1), AX")
	e.Blank()
}

// emitCallMethodInlineFallbackPaths emits the exit paths for the inline method-call
// handler. Mirrors emitCallInlineFallbackPaths but rewinds pc by two words and exits with
// EXIT_TIER2 so the Go side re-executes the method opcode.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64InlineCallOps) emitCallMethodInlineFallbackPaths(e *asmgen.Emitter) {
	e.Label(labelCIFallbackPostFPInc)
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), CX")
	inst(e, asmamd64.OperationDecrement64Bits, "CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, CTX_FRAME_POINTER(R15)")
	e.Blank()

	e.Label(labelCIFallback)
	inst(e, asmamd64.OperationSubtract64Bits, "$2, R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_TIER2, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
	e.Blank()

	e.Label("ci_overflow")
	inst(e, asmamd64.OperationSubtract64Bits, "$2, R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_CALL_OVERFLOW, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}
