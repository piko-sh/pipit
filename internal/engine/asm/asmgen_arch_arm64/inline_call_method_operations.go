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
	// labelCIMethodScan names the receiver-type table scan entry of the inline method-call
	// handler.
	labelCIMethodScan = "ci_method_scan"

	// labelCIMethodLoop names the receiver-type table scan loop of the inline method-call
	// handler.
	labelCIMethodLoop = "ci_method_loop"

	// labelCIMethodHit names the receiver-type table scan hit of the inline method-call
	// handler.
	labelCIMethodHit = "ci_method_hit"
)

// EmitCallMethodInline emits the body of handlerCallMethodInline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (o *arm64InlineCallOps) EmitCallMethodInline(e *asmgen.Emitter) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	o.emitCallInlineLookupCallInfo(e)
	o.emitCallMethodInlineSelectEntry(e)
	o.emitCallInlineGuardChecks(e)
	o.emitCallInlineSaveCallerState(e)
	o.emitCallInlineAllocateCalleeFrame(e)
	o.emitCallInlineAllocateRegisters(e)
	o.emitCallInlinePopulateFrameFields(e)
	o.emitCallInlineCopyArguments(e)
	o.emitCallInlineMaybeSetupGeneralBank(e)
	o.emitCallInlineReloadDispatchState(e)
	o.emitCallMethodInlineExitPaths(e)
}

// emitCallMethodInlineSelectEntry retargets R2 from the method call site's AsmCallInfo to
// the per-receiver-type entry matching the receiver's type word. Pointer-kind receivers
// are dereferenced through ABI_TYPE_PTR_ELEM for the inline-cache comparison.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallMethodInlineSelectEntry(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "ACI_METHOD_ENTRY_COUNT(R2), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, "+labelCIFallback)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "CTX_HAS_GOROUTINES(R19), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R4, "+labelCIFallback)
	inst5(e, asmarm64.OperationMove64Bits, "ACI_RECEIVER_REG(R2), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R4, "+labelCIFallback)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "ABI_TYPE_KIND_BYTE(R4), R5")
	inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_KIND_MASK, R5, R5")
	inst5(e, asmarm64.OperationCompare, "$REFLECT_INTERFACE, R5")
	inst5(e, asmarm64.OperationBranchIfEqual, labelCIFallback)
	inst5(e, asmarm64.OperationCompare, "$REFLECT_POINTER, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, labelCIMethodScan)
	inst5(e, asmarm64.OperationMove64Bits, "ABI_TYPE_PTR_ELEM(R4), R4")
	e.Label(labelCIMethodScan)
	inst5(e, asmarm64.OperationMove64Bits, "$0, R5")
	inst5(e, asmarm64.OperationAdd, "$ACI_METHOD_TYPE_WORDS, R2, R6")
	e.Label(labelCIMethodLoop)
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R7")
	inst5(e, asmarm64.OperationCompare, "R4, R7")
	inst5(e, asmarm64.OperationBranchIfEqual, labelCIMethodHit)
	inst5(e, asmarm64.OperationAdd, "$8, R6, R6")
	inst5(e, asmarm64.OperationAdd, "$1, R5, R5")
	inst5(e, asmarm64.OperationCompare, "R3, R5")
	inst5(e, asmarm64.OperationBranchIfLessSigned, labelCIMethodLoop)
	inst5(e, asmarm64.OperationBranch, labelCIFallback)
	e.Label(labelCIMethodHit)
	inst5(e, asmarm64.OperationMove64Bits, "ACI_METHOD_ENTRIES_PTR(R2), R2")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$ACI_SIZE_SHIFT, R5, R5")
	inst5(e, asmarm64.OperationAdd, "R5, R2, R2")
	e.Blank()
}

// emitCallMethodInlineExitPaths emits the exit paths for the inline method-call handler,
// rewinding the PC by two words and exiting with EXIT_TIER2 so the Go side re-executes
// the method opcode through its own handler.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*arm64InlineCallOps) emitCallMethodInlineExitPaths(e *asmgen.Emitter) {
	e.Label(labelCIFallbackPostFPInc)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R7")
	inst5(e, asmarm64.OperationSubtract, "$1, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, CTX_FRAME_POINTER(R19)")
	e.Blank()

	e.Label(labelCIFallback)
	inst5(e, asmarm64.OperationSubtract, "$2, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_TIER2, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
	e.Blank()

	e.Label("ci_overflow")
	inst5(e, asmarm64.OperationSubtract, "$2, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_CALL_OVERFLOW, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, inlineCallLRRestoreOperand)
	inst5(e, asmarm64.OperationAdd, inlineCallFrameTeardownOperand)
	inst5(e, asmarm64.OperationJump, inlineCallDispatchExitSymbol)
}
