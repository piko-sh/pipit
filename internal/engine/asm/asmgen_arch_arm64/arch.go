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
	core "piko.sh/asmgen/asmgen_arch_arm64"
)

// eqUintConstJumpFalseSkipWords is the number of words after the primary word of
// isa.SubOpEqUintConstJumpFalse (the extension word plus the trailing no-ops), as an
// immediate for the program-counter adjustment.
const eqUintConstJumpFalseSkipWords = "2"

const (
	// macroDivByZeroExit is the DIV_BY_ZERO_EXIT() dispatch macro invocation that records
	// the exit reason and tail-calls dispatchExit.
	macroDivByZeroExit = "DIV_BY_ZERO_EXIT()"

	// elementStrideShiftByte is log2(2) - one-byte stride lifted into the 2-byte numeric
	// lane.
	elementStrideShiftByte = 1

	// elementStrideShiftQword is log2(8) - the int64/float64 stride.
	elementStrideShiftQword = 3

	// elementStrideShiftXmm is log2(16) - the complex128 stride.
	elementStrideShiftXmm = 4

	// mnemonicColumnWidth is the padding width used by most instructions in the bytecode
	// dispatch handlers.
	mnemonicColumnWidth = 6

	// defaultColumnWidth is the padding width used by the inst5 helper.
	defaultColumnWidth = 5

	// roundingColumnWidth is the padding width used by rounding instructions whose mnemonics
	// are longer.
	roundingColumnWidth = 8

	// conversionColumnWidth is the padding width used by the FCVTZSD instruction whose
	// mnemonic is 7 characters.
	conversionColumnWidth = 7

	// shim2OperandFrameClose is the 2-operand shim frame-teardown operand, reversing the
	// 48-byte allocation from the assembler's align(32+8, 16) prologue.
	shim2OperandFrameClose = "$48, RSP"

	// shim2OperandLRRestoreBeforeJMP is the LR-restore operand paired with
	// shim2OperandFrameClose; reloads R30 from SP+0 before the frame-teardown ADD so
	// dispatchExit's RET pops the correct return address.
	shim2OperandLRRestoreBeforeJMP = "0(RSP), R30"

	// shim3OperandFrameClose is the manual frame-teardown operand for the 3-operand
	// BL-bearing shim emitted by EmitInlineGoCallThreeOperandShim (framesize $48-0;
	// assembler allocates align(48+8, 16)=64 bytes).
	shim3OperandFrameClose = "$64, RSP"

	// shim3OperandLRRestoreBeforeJMP is the LR-restore operand paired with
	// shim3OperandFrameClose; reloads R30 from SP+0 before the frame-teardown ADD.
	shim3OperandLRRestoreBeforeJMP = "0(RSP), R30"

	// conditionEQ names the equal condition code used by conditional branches.
	conditionEQ = "EQ"

	// conditionNE names the not-equal condition code used by conditional branches.
	conditionNE = "NE"

	// labelBoundsFail names the shared bounds-check failure trampoline emitted at the foot
	// of the dispatch file.
	labelBoundsFail = "bounds_fail"
)

// JumpTableEntry pairs a handler symbol name with its byte offset into the dispatch
// table. Defined locally to avoid an import cycle through the parent asm package.
type JumpTableEntry struct {
	// Name is the Plan-9 ASM symbol name of the handler (without the leading middle dot).
	Name string

	// TableSymbol identifies which dispatch table the handler is installed into.
	TableSymbol string

	// Offset is the byte offset into the target table where the handler address is written.
	Offset int
}

// BytecodeARM64Arch extends the core ARM64Arch with methods specific to the pipit
// bytecode dispatch loop.
type BytecodeARM64Arch struct {
	core.ARM64Arch

	// jumpTableEntries lists every (handler, offset) pair the EmitInitJumpTable body should
	// patch into asmJumpTable.
	jumpTableEntries []JumpTableEntry
}

// ExtractA implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dest (string) which is the destination register name.
func (*BytecodeARM64Arch) ExtractA(e *asmgen.Emitter, dest string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, "+dest)
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, "+dest+", "+dest)
}

// ExtractB implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dest (string) which is the destination register name.
func (*BytecodeARM64Arch) ExtractB(e *asmgen.Emitter, dest string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, "+dest)
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, "+dest+", "+dest)
}

// ExtractC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dest (string) which is the destination register name.
func (*BytecodeARM64Arch) ExtractC(e *asmgen.Emitter, dest string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, "+dest)
}

// ExtractWideBC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dest (string) which is the destination register name.
func (*BytecodeARM64Arch) ExtractWideBC(e *asmgen.Emitter, dest string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, "+dest)
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, "+dest+", "+dest)
}

// ExtractSignedBC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes dest (string) which is the destination register name.
func (*BytecodeARM64Arch) ExtractSignedBC(e *asmgen.Emitter, dest string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, "+dest)
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$48, "+dest+", "+dest)
	inst5(e, asmarm64.OperationArithmeticShiftRight, "$48, "+dest+", "+dest)
}

// EmitInlineGoCallTwoOperandShim implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes goSymbol (string) which is the Plan-9 symbol of the Go trampoline to BL into.
func (*BytecodeARM64Arch) EmitInlineGoCallTwoOperandShim(e *asmgen.Emitter, goSymbol string) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R2")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_SAVED_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R19, 8(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R1, 16(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R2, 24(RSP)")
	inst5(e, asmarm64.OperationBranchAndLink, goSymbol)
	inst5(e, asmarm64.OperationMove64Bits, "32(RSP), R19")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_SAVED_PC(R19), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_BASE(R19), R22")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_LEN(R19), R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INTS_BASE(R19), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLOATS_BASE(R19), R24")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R19), R26")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_JUMP_TABLE(R19), R25")
	inst5(e, asmarm64.OperationMove64Bits, shim2OperandLRRestoreBeforeJMP)
	inst5(e, asmarm64.OperationAdd, shim2OperandFrameClose)
	e.Instruction(macroDispatchNext)
}

// EmitSubOpStrconvFormatBool implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitSubOpStrconvFormatBool(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R2")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3)(R2), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$\xc2\xb7boolStringFalse(SB), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$\xc2\xb7boolStringTrue(SB), R6")
	inst5(e, asmarm64.OperationCompare, "$0, R4")
	inst5(e, asmarm64.OperationConditionalSelect, "EQ, R5, R6, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R3")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R1, R1")
	inst5(e, asmarm64.OperationAdd, "R1, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R6, 0(R3)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R3)")
	e.Instruction(macroDispatchNext)
}

// EmitInlineGoCallThreeOperandShim implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes goSymbol (string) which is the Plan-9 symbol of the Go trampoline to BL into.
func (*BytecodeARM64Arch) EmitInlineGoCallThreeOperandShim(e *asmgen.Emitter, goSymbol string) {
	e.Instruction(asmarm64.InstructionNoLocalPointers)
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R2")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R3, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_SAVED_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R19, 8(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R1, 16(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R2, 24(RSP)")
	inst5(e, asmarm64.OperationMove64Bits, "R3, 32(RSP)")
	inst5(e, asmarm64.OperationBranchAndLink, goSymbol)
	inst5(e, asmarm64.OperationMove64Bits, "40(RSP), R19")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_SAVED_PC(R19), R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_BASE(R19), R22")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CODE_LEN(R19), R21")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INTS_BASE(R19), R23")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FLOATS_BASE(R19), R24")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R19), R26")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_JUMP_TABLE(R19), R25")
	inst5(e, asmarm64.OperationMove64Bits, shim3OperandLRRestoreBeforeJMP)
	inst5(e, asmarm64.OperationAdd, shim3OperandFrameClose)
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceFloatGet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesFloat bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceFloatGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueARM64(e, contextOffset)
	inst5(e, asmarm64.OperationFloatMove64Bits, "(R6)(R8<<3), F0")
	inst5(e, asmarm64.OperationFloatMove64Bits, "F0, (R24)(R3<<3)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceIntGetDirect implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceIntGetDirect(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderARM64(e, contextOffset, "$16")
	emitTypedSliceIntDirectIndexARM64(e, "$24")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, labelBoundsFail)
	inst5(e, asmarm64.OperationMove64Bits, "(R6)(R8<<3), R9")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	emitTypedSliceIntDirectTailARM64(e)
}

// EmitTypedSliceIntSetDirect implements BytecodeArchPort.
//
// Emits the tier-0 body for slicesInt[A][ints[B]] = ints[C] with bounds check. See
// EmitTypedSliceIntGetDirect for the tier-0 conventions.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceIntSetDirect(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderARM64(e, contextOffset, "$8")
	emitTypedSliceIntDirectIndexARM64(e, "$16")
	inst5(e, asmarm64.OperationCompare, "R7, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, labelBoundsFail)
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R6)(R8<<3)")
	emitTypedSliceIntDirectTailARM64(e)
}

// EmitTypedSliceIntGetDirectUnchecked implements BytecodeArchPort.
//
// Bounds-elided variant of EmitTypedSliceIntGetDirect: the compiler's BCE pass only emits
// the unchecked opcode when a dominating comparison proves the index in range.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceIntGetDirectUnchecked(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderARM64(e, contextOffset, "$16")
	emitTypedSliceIntDirectIndexARM64(e, "$24")
	inst5(e, asmarm64.OperationMove64Bits, "(R6)(R8<<3), R9")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceIntSetDirectUnchecked implements BytecodeArchPort.
//
// Bounds-elided variant of EmitTypedSliceIntSetDirect for BCE-proven sites.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceIntSetDirectUnchecked(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderARM64(e, contextOffset, "$8")
	emitTypedSliceIntDirectIndexARM64(e, "$16")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, (R6)(R8<<3)")
	e.Instruction(macroDispatchNext)
}

// EmitSliceIndexStructFieldScalar implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes destIsFloat (bool) which selects the float bank destination over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
//
//nolint:revive // Reason above.
func (*BytecodeARM64Arch) EmitSliceIndexStructFieldScalar(e *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string) {
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R3, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R5), R10")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE_LEN(R10), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sif_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(R10), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R5")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_KIND(R5), R6")
	inst5(e, asmarm64.OperationCompare, kindA+", R6")
	if kindB != "" {
		inst5(e, asmarm64.OperationBranchIfEqual, "sif_kind_ok")
		inst5(e, asmarm64.OperationCompare, kindB+", R6")
	}
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sif_shim")
	if kindB != "" {
		e.Label("sif_kind_ok")
	}
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "0(R6), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, sif_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R6), R8")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R8, R8")
	inst5(e, asmarm64.OperationCompare, "$23, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sif_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R6")
	inst5(e, asmarm64.OperationMove64Bits, "48(R7), R7")
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "OFF_LAYOUT_TYPE_INDEX(R5), R3")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE_LEN(R10), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sif_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE(R10), R4")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "8(R4), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sif_shim")
	inst5(e, asmarm64.OperationMove64Bits, "0(R7), R4")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sif_shim")
	inst5(e, asmarm64.OperationMultiply, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, sif_shim")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "OFF_LAYOUT_OFFSET(R5), R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R7")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	if destIsFloat {
		inst5(e, asmarm64.OperationMove64Bits, "R7, (R24)(R3<<3)")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "R7, (R23)(R3<<3)")
	}
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
	e.Label("sif_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitDerefSliceIntAccess implements BytecodeArchPort.
//
// arm64 twin of the amd64 implementation: pointer-resolved int-slice element access with
// live bounds check; unsupported shapes and out-of-range indexes fall back to the shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes isSet (bool) which selects the write-side operand roles and store.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeARM64Arch) EmitDerefSliceIntAccess(e *asmgen.Emitter, isSet bool, shimSymbol string) {
	ptrShift, idxShift := "$16", "$24"
	if isSet {
		ptrShift, idxShift = "$8", "$16"
	}
	inst5(e, asmarm64.OperationLogicalShiftRight, ptrShift+", R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, ds_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R5), R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R7, R8")
	inst5(e, asmarm64.OperationCompare, "$22, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "ds_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R6")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R7, ds_direct")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R6")
	e.Label("ds_direct")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, ds_shim")
	inst5(e, asmarm64.OperationLogicalShiftRight, idxShift+", R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "ds_shim")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R6")
	if isSet {
		inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
		inst5(e, asmarm64.OperationMove64Bits, "(R23)(R4<<3), R4")
		inst5(e, asmarm64.OperationMove64Bits, "R4, (R6)(R3<<3)")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "(R6)(R3<<3), R4")
		inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
		inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
		inst5(e, asmarm64.OperationMove64Bits, "R4, (R23)(R3<<3)")
	}
	e.Instruction(macroDispatchNext)
	e.Label("ds_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitGetStructFieldGeneralPointerT0 implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
//
//nolint:revive // straight-line listing must not be split.
func (*BytecodeARM64Arch) EmitGetStructFieldGeneralPointerT0(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, sf_shim")
	emitStructFieldCommonPrologueARM64(e, "$22", "$20", "$16")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_KIND(R5), R4")
	inst5(e, asmarm64.OperationCompare, "$20, R4")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "gp_ptr_field")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R8")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, gp_store_zero")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "23(R7), R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R4, R4")
	inst5(e, asmarm64.OperationCompare, "$18, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "gp_iface_store")
	inst5(e, asmarm64.OperationCompare, "$19, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "gp_iface_store")
	inst5(e, asmarm64.OperationCompare, "$21, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "gp_iface_store")
	inst5(e, asmarm64.OperationCompare, "$22, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "gp_iface_store")
	inst5(e, asmarm64.OperationCompare, "$26, R4")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	e.Label("gp_iface_store")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R4, 16(R6)")
	inst5(e, asmarm64.OperationBranch, "gp_done")
	e.Label("gp_store_zero")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 16(R6)")
	e.Label("gp_done")
	e.Instruction(macroDispatchNext)
	e.Label("gp_ptr_field")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R7, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R7), R7")
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "OFF_LAYOUT_FIELD_TYPE_INDEX(R5), R4")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE_LEN(R7), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R4")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE(R7), R7")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R4, R4")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R7")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "$22, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 16(R6)")
	inst5(e, asmarm64.OperationBranch, "gp_done")
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimGetStructFieldGeneralT0(SB)")
}

// EmitGetStructFieldRawPointerT0 implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
//
//nolint:revive // Reason above.
func (*BytecodeARM64Arch) EmitGetStructFieldRawPointerT0(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, rp_shim")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R5), R5")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE_LEN(R5), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "rp_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(R5), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R5")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "0(R6), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, rp_zero")
	inst5(e, asmarm64.OperationMove64Bits, "16(R6), R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R7, R8")
	inst5(e, asmarm64.OperationCompare, "$22, R8")
	inst5(e, asmarm64.OperationBranchIfEqual, "rp_ptr_recv")
	inst5(e, asmarm64.OperationCompare, "$25, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "rp_shim")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$8, R7, rp_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R6")
	inst5(e, asmarm64.OperationBranch, "rp_have_base")
	e.Label("rp_ptr_recv")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R6")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R7, rp_have_base")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R6")
	e.Label("rp_have_base")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, rp_zero")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "OFF_LAYOUT_OFFSET(R5), R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R8")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, rp_store_zero")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "$22, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 16(R6)")
	inst5(e, asmarm64.OperationBranch, "rp_done")
	e.Label("rp_zero")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	e.Label("rp_store_zero")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, 16(R6)")
	e.Label("rp_done")
	e.Instruction(macroDispatchNext)
	e.Label("rp_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimGetStructFieldRawPointerT0(SB)")
}

// EmitEqGeneral implements BytecodeArchPort.
//
// arm64 twin of the amd64 implementation: fast general-equality arms (both invalid,
// differing type words, pointer-identity kinds) with everything else falling back to the
// Go shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitEqGeneral(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R3, R3")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R6")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R7")
	inst5(e, asmarm64.OperationMove64Bits, "0(R6), R8")
	inst5(e, asmarm64.OperationMove64Bits, "0(R7), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R8, eqg_a_invalid")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, eqg_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R6), R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R3, R4")
	inst5(e, asmarm64.OperationCompare, "$20, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "eqg_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R7), R5")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R5, R10")
	inst5(e, asmarm64.OperationCompare, "$20, R10")
	inst5(e, asmarm64.OperationBranchIfEqual, "eqg_shim")
	inst5(e, asmarm64.OperationCompare, "R8, R9")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "eqg_store_zero")
	inst5(e, asmarm64.OperationCompare, "$18, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "eqg_ptr_cmp")
	inst5(e, asmarm64.OperationCompare, "$22, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "eqg_ptr_cmp")
	inst5(e, asmarm64.OperationCompare, "$26, R4")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "eqg_shim")
	e.Label("eqg_ptr_cmp")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R8")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R3, eqg_a_direct")
	inst5(e, asmarm64.OperationMove64Bits, "(R8), R8")
	e.Label("eqg_a_direct")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R9")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R5, eqg_b_direct")
	inst5(e, asmarm64.OperationMove64Bits, "(R9), R9")
	e.Label("eqg_b_direct")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R4")
	inst5(e, asmarm64.OperationCompare, "R8, R9")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "eqg_store")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R4")
	inst5(e, asmarm64.OperationBranch, "eqg_store")
	e.Label("eqg_a_invalid")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R9, eqg_shim")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R4")
	inst5(e, asmarm64.OperationBranch, "eqg_store")
	e.Label("eqg_store_zero")
	inst5(e, asmarm64.OperationMove64Bits, "$0, R4")
	e.Label("eqg_store")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R4, (R23)(R3<<3)")
	e.Instruction(macroDispatchNext)
	e.Label("eqg_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimEqGeneral(SB)")
}

// EmitMoveGeneralAlias implements BytecodeArchPort.
//
// arm64 twin of the amd64 implementation: alias-mode 24-byte slot copy gated on the
// runtime write-barrier flag, with every other mode and the marking phase falling back to
// the Go shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitMoveGeneralAlias(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationCompare, "$1, R3")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "mg_shim")
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R4")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R4), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R4, mg_shim")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R5")
	inst5(e, asmarm64.OperationMultiply, "R5, R3, R3")
	inst5(e, asmarm64.OperationMultiply, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R6")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R7")
	inst5(e, asmarm64.OperationMove64Bits, "0(R7), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 8(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "16(R7), R8")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 16(R6)")
	e.Instruction(macroDispatchNext)
	e.Label("mg_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimMoveGeneral(SB)")
}

// EmitTestNilJump implements BytecodeArchPort.
//
// arm64 twin of the amd64 implementation: nil-tests general[A] and branches by the signed
// 16-bit offset in B|C<<8 with the shared poll-budget back-edge exit.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes jumpWhenNil (bool) which selects the True (jump on nil) polarity.
func (*BytecodeARM64Arch) EmitTestNilJump(e *asmgen.Emitter, jumpWhenNil bool) {
	nilLabel, notNilLabel := "tnj_take", "tnj_skip"
	if !jumpWhenNil {
		nilLabel, notNilLabel = "tnj_skip", "tnj_take"
	}
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, "+nilLabel)
	inst5(e, asmarm64.OperationMove64Bits, "16(R5), R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R7, R8")
	inst5(e, asmarm64.OperationCompare, "$18, R8")
	inst5(e, asmarm64.OperationBranchIfLower, notNilLabel)
	inst5(e, asmarm64.OperationCompare, "$23, R8")
	inst5(e, asmarm64.OperationBranchIfHigher, notNilLabel)
	inst5(e, asmarm64.OperationTestBitAndBranchIfNotZero, "$9, R7, "+notNilLabel)
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R9")
	inst5(e, asmarm64.OperationCompare, "$20, R8")
	inst5(e, asmarm64.OperationBranchIfEqual, "tnj_deref")
	inst5(e, asmarm64.OperationCompare, "$23, R8")
	inst5(e, asmarm64.OperationBranchIfEqual, "tnj_deref")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R7, tnj_word")
	e.Label("tnj_deref")
	inst5(e, asmarm64.OperationMove64Bits, "(R9), R9")
	e.Label("tnj_word")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, "+nilLabel)
	inst5(e, asmarm64.OperationBranch, notNilLabel)
	e.Label("tnj_take")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$48, R3, R3")
	inst5(e, asmarm64.OperationArithmeticShiftRight, "$48, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R20, R20")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_POLL_BUDGET(R19), R0")
	inst5(e, asmarm64.OperationSubtract, "$1, R0, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_POLL_BUDGET(R19)")
	inst5(e, asmarm64.OperationCompare, "$0, R0")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, "tnj_skip")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_PC(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$EXIT_POLL, R0")
	inst5(e, asmarm64.OperationMove64Bits, "R0, CTX_EXIT_REASON(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "R20, CTX_EXIT_PC(R19)")
	inst5(e, asmarm64.OperationReturn, "")
	e.Label("tnj_skip")
	e.Instruction(macroDispatchNext)
}

// EmitStructFieldScalarGetT0 implements BytecodeArchPort.
//
// Tier-0 fast path for the scalar struct-field GET family: dest[A] = *(receiverBase +
// layout[C].Offset). Shape gates and fallback semantics mirror the amd64 implementation;
// unsupported shapes branch to the wide tier-2 shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte (e.g. "$6" Int64).
// Takes kindB (string) which is the secondary accepted Kind byte, or "" when the family
// has only one.
// Takes bank (string) which names the destination bank ("ints", "floats", "uints",
// "bools"); the mapping to pinned registers / context bases and width-correct move forms
// lives in structFieldBankARM64.
// Takes shimSymbol (string) which is the fallback shim, e.g.
// "handlerPathBShimGetStructFieldIntT0".
func (*BytecodeARM64Arch) EmitStructFieldScalarGetT0(e *asmgen.Emitter, kindA, kindB, bank, shimSymbol string) {
	spec := structFieldBankARM64(bank)
	emitStructFieldCommonPrologueARM64(e, kindA, kindB, "$16")
	inst5(e, spec.fieldLoadOp, "(R6), R9")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	emitStructFieldBankStoreARM64(e, spec)
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldScalarSetT0 implements BytecodeArchPort.
//
// Tier-0 fast path for the scalar struct-field SET family: *(receiverBase +
// layout[C].Offset) = source[B]. Scalar stores carry no pointers so no write barrier is
// required. Shape gates mirror EmitStructFieldScalarGetT0.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte.
// Takes kindB (string) which is the secondary accepted layout Kind byte.
// Takes bank (string) which names the source bank as in EmitStructFieldScalarGetT0.
// Takes shimSymbol (string) which is the fallback shim symbol.
func (*BytecodeARM64Arch) EmitStructFieldScalarSetT0(e *asmgen.Emitter, kindA, kindB, bank, shimSymbol string) {
	spec := structFieldBankARM64(bank)
	emitStructFieldCommonPrologueARM64(e, kindA, kindB, "$8")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	emitStructFieldBankLoadARM64(e, spec)
	inst5(e, spec.fieldStoreOp, "R9, (R6)")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitLoadUintConst implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uintConstants[B|(C<<8)]. Pool indexes are compiler-generated
// so no bounds check is emitted, mirroring the int-const loader.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitLoadUintConst(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R4<<3), R6")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R5)(R3<<3)")
	e.Instruction(macroDispatchNext)
}

// EmitLoadUintConstSmall implements BytecodeArchPort.
//
// Tier-1 body for uints[B] = uint64(C), the immediate small-constant loader.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitLoadUintConstSmall(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "R4, (R5)(R3<<3)")
	e.Instruction(macroDispatchNext)
}

// EmitUintConstArith implements BytecodeArchPort.
//
// Tier-1 body for uints[B] = uints[C] op uintConstants[ext.A|(ext.B<<8)], consuming the
// extension word (PC is advanced past it before DISPATCH_NEXT).
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which selects the instruction: "add", "sub", or "and".
func (*BytecodeARM64Arch) EmitUintConstArith(e *asmgen.Emitter, operation string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R5")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R5, R5")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R6)(R5<<3), R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R6")
	inst5(e, asmarm64.OperationMove64Bits, "(R6)(R4<<3), R7")
	switch operation {
	case "add":
		inst5(e, asmarm64.OperationAdd, "R5, R7, R7")
	case "sub":
		inst5(e, asmarm64.OperationSubtract, "R5, R7, R7")
	case "and":
		inst5(e, asmarm64.OperationBitwiseAnd, "R5, R7, R7")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R6)(R3<<3)")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
}

// EmitUintDivide implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uints[B] / uints[C]; a zero divisor exits through
// DIV_BY_ZERO_EXIT so the Go side raises the interpreted panic.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitUintDivide(e *asmgen.Emitter) {
	emitUintDivRemPrologueARM64(e)
	inst5(e, asmarm64.OperationUnsignedDivide, "R7, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R5)(R3<<3)")
	e.Instruction(macroDispatchNext)
	e.Label("dbz")
	e.Instruction("DIV_BY_ZERO_EXIT()")
}

// EmitUintRemainder implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uints[B] % uints[C]: remainder = dividend - quotient *
// divisor, sharing EmitUintDivide's zero-divisor discipline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitUintRemainder(e *asmgen.Emitter) {
	emitUintDivRemPrologueARM64(e)
	inst5(e, asmarm64.OperationUnsignedDivide, "R7, R6, R8")
	inst5(e, asmarm64.OperationMultiply, "R7, R8, R8")
	inst5(e, asmarm64.OperationSubtract, "R8, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R5)(R3<<3)")
	e.Instruction(macroDispatchNext)
	e.Label("dbz")
	e.Instruction("DIV_BY_ZERO_EXIT()")
}

// EmitTypedSliceFloatSet implements BytecodeArchPort.
//
// Emits the bounds-checked element-store body for a tier-1 umbrella sub-op of the form
// slicesFloat[B][ints[C]] = floats[ext.A].
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesFloat bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceFloatSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueARM64(e, contextOffset)
	emitPeekExtensionWordAFieldARM64(e, "R3")
	inst5(e, asmarm64.OperationFloatMove64Bits, "(R24)(R3<<3), F0")
	inst5(e, asmarm64.OperationFloatMove64Bits, "F0, (R6)(R8<<3)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceUintGet implements BytecodeArchPort.
//
// Emits the bounds-checked element-load body for a tier-1 umbrella sub-op of the form
// uints[B] = slicesUint[C][ints[ext.A]]. The destination uint bank base is loaded from
// CTX_UINTS_BASE (uint bank does not occupy a pinned dispatch register).
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesUint bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceUintGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueARM64(e, contextOffset)
	inst5(e, asmarm64.OperationMove64Bits, "(R6)(R8<<3), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R5)(R3<<3)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceUintSet implements BytecodeArchPort.
//
// Emits the bounds-checked element-store body for a tier-1 umbrella sub-op of the form
// slicesUint[B][ints[C]] = uints[ext.A].
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesUint bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceUintSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueARM64(e, contextOffset)
	emitPeekExtensionWordAFieldARM64(e, "R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R3<<3), R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R6)(R8<<3)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceBoolGet implements BytecodeArchPort.
//
// Emits the bounds-checked element-load body for a tier-1 umbrella sub-op of the form
// bools[B] = slicesBool[C][ints[ext.A]]. Bool elements are 1 byte; the load uses MOVBU
// (zero-extending byte load) and the store uses MOVB.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesBool bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceBoolGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueARM64(e, contextOffset)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R6)(R8), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove8Bits, "R7, (R5)(R3)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceBoolSet implements BytecodeArchPort.
//
// Emits the bounds-checked element-store body for a tier-1 umbrella sub-op of the form
// slicesBool[B][ints[C]] = bools[ext.A].
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesBool bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceBoolSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueARM64(e, contextOffset)
	emitPeekExtensionWordAFieldARM64(e, "R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R5)(R3), R7")
	inst5(e, asmarm64.OperationMove8Bits, "R7, (R6)(R8)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceByteGet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceByteGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueARM64(e, contextOffset)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R6)(R8), R7")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R10")
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R10)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceByteSet implements BytecodeArchPort.
//
// Emits the bounds-checked element-store body for a tier-1 umbrella sub-op of the form
// slicesByte[B][ints[C]] = byte(uints[ext.A]). The source uint64 is loaded with MOVD and
// only the low byte is stored via MOVB.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceByteSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueARM64(e, contextOffset)
	emitPeekExtensionWordAFieldARM64(e, "R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R10")
	inst5(e, asmarm64.OperationMove64Bits, "(R10), R7")
	inst5(e, asmarm64.OperationMove8Bits, "R7, (R6)(R8)")
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceByteSlice implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceByteSlice(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceByteSliceExtractARM64(e)
	emitTypedSliceByteSliceLoadAndBoundsARM64(e, contextOffset)
	emitTypedSliceByteSliceWriteHeaderARM64(e, contextOffset)
	emitTypedSliceTailARM64(e)
}

// EmitTypedSliceMove implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceMove(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")

	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMultiply, "R9, R4, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R7")
	inst5(e, asmarm64.OperationMove64Bits, "16(R5), R8")

	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMultiply, "R9, R3, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "R6, 0(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R5)")
	inst5(e, asmarm64.OperationMove64Bits, "R8, 16(R5)")

	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceSliceSlice implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
// Takes elementSizeShift (uint8) which is the log2 of the element stride.
func (*BytecodeARM64Arch) EmitTypedSliceSliceSlice(e *asmgen.Emitter, contextOffset string, elementSizeShift uint8) {
	emitTypedSliceByteSliceExtractARM64(e)
	emitTypedSliceByteSliceLoadAndBoundsARM64(e, contextOffset)
	emitTypedSliceSliceSliceWriteHeaderARM64(e, contextOffset, elementSizeShift)
	emitTypedSliceTailARM64(e)
}

// EmitTypedRangeNextByte implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedRangeNextByte(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")

	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")

	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R7, R7")

	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R6")
	inst5(e, asmarm64.OperationAdd, "$1, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R23)(R3<<3)")

	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R9, R4, R10")
	inst5(e, asmarm64.OperationAdd, "R10, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R8")

	inst5(e, asmarm64.OperationCompare, "R8, R6")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, "rangeByteEnd")

	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R5")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R5)(R6), R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$3, R7, R7")
	inst5(e, asmarm64.OperationAdd, "R7, R5, R10")
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R10)")

	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	inst5(e, asmarm64.OperationBranch, "rangeByteDispatch")

	e.Label("rangeByteEnd")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R3, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFFFF, R3, R3")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	inst5(e, asmarm64.OperationAdd, "R3, R20, R20")
	emitBackEdgePoll(e)

	e.Label("rangeByteDispatch")
	e.Instruction(macroDispatchNext)
}

// EmitRangeCheckUintJumpFalse implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitRangeCheckUintJumpFalse(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")

	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R4)(R3<<3), R5")

	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R0")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")

	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R6, R6")

	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R7, R7")

	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfLower, "rangeCheckUintTakeJump")

	inst5(e, asmarm64.OperationCompare, "R7, R5")
	inst5(e, asmarm64.OperationBranchIfHigher, "rangeCheckUintTakeJump")

	inst5(e, asmarm64.OperationAdd, "$6, R20, R20")
	inst5(e, asmarm64.OperationBranch, "rangeCheckUintDispatch")

	e.Label("rangeCheckUintTakeJump")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R8")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R8, R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$48, R8, R8")
	inst5(e, asmarm64.OperationArithmeticShiftRight, "$48, R8, R8")
	inst5(e, asmarm64.OperationAdd, "$5, R20, R20")
	inst5(e, asmarm64.OperationAdd, "R8, R20, R20")
	emitBackEdgePoll(e)

	e.Label("rangeCheckUintDispatch")
	e.Instruction(macroDispatchNext)
}

// EmitEqUintConstJumpFalse implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeARM64Arch) EmitEqUintConstJumpFalse(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R6, R6")

	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R4)(R3<<3), R5")
	inst5(e, asmarm64.OperationCompare, "R6, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "eqUintConstTakeJump")

	inst5(e, asmarm64.OperationAdd, "$"+eqUintConstJumpFalseSkipWords+", R20, R20")
	inst5(e, asmarm64.OperationBranch, "eqUintConstDispatch")

	e.Label("eqUintConstTakeJump")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R8")
	inst5(e, asmarm64.OperationAdd, "$"+eqUintConstJumpFalseSkipWords+", R20, R20")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R8, R8")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$48, R8, R8")
	inst5(e, asmarm64.OperationArithmeticShiftRight, "$48, R8, R8")
	inst5(e, asmarm64.OperationAdd, "R8, R20, R20")
	emitBackEdgePoll(e)

	e.Label("eqUintConstDispatch")
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceStringGet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the slicesString bank base
// pointer within the dispatchContext.
func (*BytecodeARM64Arch) EmitTypedSliceStringGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueARM64(e, contextOffset)
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R8, R8")
	inst5(e, asmarm64.OperationAdd, "R8, R6, R10")
	inst5(e, asmarm64.OperationMove64Bits, "0(R10), R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R10), R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_STRINGS_BASE(R19), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R10")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 0(R10)")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 8(R10)")
	emitTypedSliceTailARM64(e)
}

// EmitComplexCopy implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
func (*BytecodeARM64Arch) EmitComplexCopy(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R8")
	inst5(e, asmarm64.OperationMove64Bits, "0(R8), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R8), R7")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R8")
	inst5(e, asmarm64.OperationMove64Bits, "R6, 0(R8)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R8)")
	e.Instruction(macroDispatchNext)
}

// EmitComplexNegate implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes contextOffset (string) which is the byte offset of the complex bank base pointer
// within the dispatchContext.
func (*BytecodeARM64Arch) EmitComplexNegate(e *asmgen.Emitter, contextOffset string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R4")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, contextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R4, R5, R9")
	inst5(e, asmarm64.OperationMove64Bits, "0(R9), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R9), R7")
	inst5(e, asmarm64.OperationMove64Bits, "$0x8000000000000000, R8")
	inst5(e, asmarm64.OperationExclusiveOr, "R8, R6, R6")
	inst5(e, asmarm64.OperationExclusiveOr, "R8, R7, R7")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R9")
	inst5(e, asmarm64.OperationMove64Bits, "R6, 0(R9)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R9)")
	e.Instruction(macroDispatchNext)
}

// structFieldBankSpecARM64 carries the per-bank addressing and move forms used by the
// scalar struct-field handlers. base is either a pinned register ("R23"/"R24") or a
// CTX_*_BASE define that must first be loaded from the context.
type structFieldBankSpecARM64 struct {
	// base holds the register name or CTX define for the bank base address.
	base string

	// fieldLoadOp holds the instruction mnemonic for loading a struct field value.
	fieldLoadOp string

	// fieldStoreOp holds the instruction mnemonic for storing a struct field value.
	fieldStoreOp string

	// bankStoreOp holds the instruction mnemonic for storing into a register bank slot.
	bankStoreOp string

	// bankLoadOp holds the instruction mnemonic for loading from a register bank slot.
	bankLoadOp string

	// baseIsPinned is true when base names a pinned register rather than a CTX define.
	baseIsPinned bool

	// byteIndexed is true when the bank uses byte indexing instead of word indexing.
	byteIndexed bool
}

// emitTypedSliceIntDirectHeaderARM64 extracts the slice-slot operand at slotShift from
// the instruction word and loads the slicesInt header: R6 = data pointer, R7 = length.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the slice bank base offset define.
// Takes slotShift (string) which is the bit shift selecting the slot operand ("$8" for
// the A field, "$16" for B).
func emitTypedSliceIntDirectHeaderARM64(e *asmgen.Emitter, sliceContextOffset string, slotShift string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, slotShift+", R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, sliceContextOffset+"(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R9")
	inst5(e, asmarm64.OperationMultiply, "R9, R3, R7")
	inst5(e, asmarm64.OperationAdd, "R7, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R7")
}

// emitTypedSliceIntDirectIndexARM64 loads the element index value into R8 from the
// int-bank register named by the operand at indexShift.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes indexShift (string) which is the bit shift selecting the index-register operand.
func emitTypedSliceIntDirectIndexARM64(e *asmgen.Emitter, indexShift string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, indexShift+", R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R4<<3), R8")
}

// emitTypedSliceIntDirectTailARM64 emits DISPATCH_NEXT plus the shared bounds-fail
// trampoline for the tier-0 slice-int handlers. Unlike emitTypedSliceTailARM64 there is
// no PC increment: tier-0 opcodes consume no extension word.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitTypedSliceIntDirectTailARM64(e *asmgen.Emitter) {
	e.Instruction(macroDispatchNext)
	e.Label(labelBoundsFail)
	inst5(e, asmarm64.OperationBranch, "·pathBFallback(SB)")
}

// structFieldBankARM64 maps a bank name from the arch-neutral handler definition to its
// arm64 addressing spec. Unknown names panic at generation time, which asmgen surfaces as
// a build failure rather than silently emitting wrong assembly.
//
// Takes bank (string) which is "ints", "floats", "uints" or "bools".
//
// Returns structFieldBankSpecARM64 which is the resolved addressing spec.
func structFieldBankARM64(bank string) structFieldBankSpecARM64 {
	switch bank {
	case "ints":
		return structFieldBankSpecARM64{
			base:         "R23",
			baseIsPinned: true,
			fieldLoadOp:  asmarm64.OperationMove64Bits,
			fieldStoreOp: asmarm64.OperationMove64Bits,
			bankStoreOp:  asmarm64.OperationMove64Bits,
			bankLoadOp:   asmarm64.OperationMove64Bits,
			byteIndexed:  false,
		}
	case "floats":
		return structFieldBankSpecARM64{
			base:         "R24",
			baseIsPinned: true,
			fieldLoadOp:  asmarm64.OperationMove64Bits,
			fieldStoreOp: asmarm64.OperationMove64Bits,
			bankStoreOp:  asmarm64.OperationMove64Bits,
			bankLoadOp:   asmarm64.OperationMove64Bits,
			byteIndexed:  false,
		}
	case "uints":
		return structFieldBankSpecARM64{
			base:         "CTX_UINTS_BASE",
			fieldLoadOp:  asmarm64.OperationMove64Bits,
			fieldStoreOp: asmarm64.OperationMove64Bits,
			bankStoreOp:  asmarm64.OperationMove64Bits,
			bankLoadOp:   asmarm64.OperationMove64Bits,
			baseIsPinned: false,
			byteIndexed:  false,
		}
	case "bools":
		return structFieldBankSpecARM64{
			base:         "CTX_BOOLS_BASE",
			fieldLoadOp:  asmarm64.OperationMove8BitsUnsigned,
			fieldStoreOp: asmarm64.OperationMove8Bits,
			bankStoreOp:  asmarm64.OperationMove8Bits,
			bankLoadOp:   asmarm64.OperationMove8BitsUnsigned,
			byteIndexed:  true,
			baseIsPinned: false,
		}
	default:
		panic("structFieldBankARM64: unknown bank " + bank)
	}
}

// emitStructFieldBankStoreARM64 stores R9 into the destination bank at index R3.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes spec (structFieldBankSpecARM64) which is the resolved bank addressing spec.
func emitStructFieldBankStoreARM64(e *asmgen.Emitter, spec structFieldBankSpecARM64) {
	target := spec.base
	if !spec.baseIsPinned {
		inst5(e, asmarm64.OperationMove64Bits, spec.base+"(R19), R5")
		target = "R5"
	}
	if spec.byteIndexed {
		inst5(e, spec.bankStoreOp, "R9, ("+target+")(R3)")
		return
	}
	inst5(e, spec.bankStoreOp, "R9, ("+target+")(R3<<3)")
}

// emitStructFieldBankLoadARM64 loads the source bank value at index R3 into R9.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes spec (structFieldBankSpecARM64) which is the resolved bank addressing spec.
func emitStructFieldBankLoadARM64(e *asmgen.Emitter, spec structFieldBankSpecARM64) {
	target := spec.base
	if !spec.baseIsPinned {
		inst5(e, asmarm64.OperationMove64Bits, spec.base+"(R19), R5")
		target = "R5"
	}
	if spec.byteIndexed {
		inst5(e, spec.bankLoadOp, "("+target+")(R3), R9")
		return
	}
	inst5(e, spec.bankLoadOp, "("+target+")(R3<<3), R9")
}

// emitUintDivRemPrologueARM64 decodes the three uint div/rem operands: on exit R3 =
// destination uint register index, R6 = dividend value, R7 = divisor value
// (zero-checked), R5 = uint bank base.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitUintDivRemPrologueARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R4")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R4, R4")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R9")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R5")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R9<<3), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, dbz")
	inst5(e, asmarm64.OperationMove64Bits, "(R5)(R4<<3), R6")
}
