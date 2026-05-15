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
	// macroDispatchNext represents the DISPATCH_NEXT() macro invocation.
	macroDispatchNext = "DISPATCH_NEXT()"

	// labelSliceBoundsFail is the label for the slice bounds failure exit path.
	labelSliceBoundsFail = "sl_bounds_fail"
)

// amd64StringOps implements StringOperationsPort for x86-64, where each method emits the
// complete handler body for a string operation.
type amd64StringOps struct{}

var (
	_ asmgen.StringOperationsPort = (*amd64StringOps)(nil)
)

// EmitLenString emits the body for handlerLenString.
//
// Sets ints[A] = len(strings[B]) by loading the string header length field (offset +8
// from the 16-byte header) and storing it into the integer bank. The caller appends
// DISPATCH_NEXT.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitLenString(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
}

// EmitStringIndex emits the body for handlerStringIndex, performing uints[A] =
// uint64(strings[B][ints[C]]) with bounds checking and a tier-2 fallback exit.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitStringIndex(e *asmgen.Emitter) {
	emitStringIndexExtractAndLoad(e)
	emitStringIndexBoundsCheckAndStore(e, "si_fallback", "CTX_UINTS_BASE(R15)")
}

// EmitEqualString emits the body for handlerEqString, performing ints[A] = (strings[B] ==
// strings[C]) ? 1 : 0.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitEqualString(e *asmgen.Emitter) {
	emitStringCompareExtractHeaders(e)
	emitStringCompareLengthFastPath(e, "eqs_ne", "eqs_eq")
	emitStringCompareByteByByte(e, "eqs_ne")
	emitStringCompareResultLabels(e, "eqs_eq", "eqs_ne", "eqs_done", true)
}

// EmitNotEqualString emits the body for handlerNeString, performing ints[A] = (strings[B]
// != strings[C]) ? 1 : 0 with inverted result polarity.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitNotEqualString(e *asmgen.Emitter) {
	emitStringCompareExtractHeaders(e)
	emitStringCompareLengthFastPath(e, "nes_ne", "nes_eq")
	emitStringCompareByteByByte(e, "nes_ne")
	emitStringCompareResultLabels(e, "nes_eq", "nes_ne", "nes_done", false)
}

// EmitSliceString emits the body for handlerSliceString, performing strings[A] =
// strings[B][low:high] with bounds checking and a fallback exit.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitSliceString(e *asmgen.Emitter) {
	emitSliceStringExtractAndLoadHeader(e)
	emitSliceStringLoadExtensionWord(e)
	emitSliceStringComputeLowBound(e)
	emitSliceStringComputeHighBound(e)
	emitSliceStringValidateAndStore(e)
	emitSliceStringBoundsFail(e)
}

// EmitStringIndexToInt emits the body for handlerStringIndexToInt, performing ints[A] =
// int64(strings[B][ints[C]]) with bounds checking and a tier-2 fallback exit.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitStringIndexToInt(e *asmgen.Emitter) {
	emitStringIndexExtractAndLoad(e)
	emitStringIndexToIntBoundsCheckAndStore(e)
}

// EmitLenStringLtJumpFalse emits the body for handlerLenStringLtJumpFalse, which jumps by
// a signed 16-bit offset when ints[A] >= len(strings[B]).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*amd64StringOps) EmitLenStringLtJumpFalse(e *asmgen.Emitter) {
	emitLenStringLtLoadAndCompare(e)
	emitLenStringLtJumpOffsetAndDispatch(e)
}

// emitStringIndexExtractAndLoad emits the operand extraction and string header loading
// sequence shared by EmitStringIndex and EmitStringIndexToInt.
//
// After this sequence: AX = operand A index, DI = string data pointer, SI = string
// length, CX = index value from ints[C].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitStringIndexExtractAndLoad(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*1), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), SI")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(CX*8), CX")
}

// emitStringIndexBoundsCheckAndStore emits the bounds check, byte load, store,
// DISPATCH_NEXT, and fallback exit for string indexing operations.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes fallbackLabel (string) which is the unique label for the fallback branch target.
// Takes destBase (string) which is the context field macro for the destination bank.
func emitStringIndexBoundsCheckAndStore(e *asmgen.Emitter, fallbackLabel string, destBase string) {
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfSign, fallbackLabel)
	inst(e, asmamd64.OperationCompare64Bits, "CX, SI")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, fallbackLabel)
	inst(e, asmamd64.OperationMove8To64BitsZeroExtended, "(DI)(CX*1), CX")
	if destBase == "CTX_UINTS_BASE(R15)" {
		inst(e, asmamd64.OperationMove64Bits, destBase+", SI")
		inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(AX*8)")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	}
	e.Instruction(macroDispatchNext)
	e.Blank()
	e.Label(fallbackLabel)
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_TIER2, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// emitStringCompareExtractHeaders emits the operand extraction and string header loading
// sequence shared by EmitEqualString and EmitNotEqualString.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitStringCompareExtractHeaders(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(CX*1), DX")
}

// emitStringCompareLengthFastPath emits the length comparison early exit and the
// pointer-equality check that skips the byte loop for identical or empty strings.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes neLabel (string) which is the not-equal branch label.
// Takes eqLabel (string) which is the equal branch label.
func emitStringCompareLengthFastPath(e *asmgen.Emitter, neLabel, eqLabel string) {
	inst(e, asmamd64.OperationCompare64Bits, "DI, DX")
	inst(e, asmamd64.OperationJumpIfNotEqual, neLabel)
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*1), BX")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(CX*1), CX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CX")
	inst(e, asmamd64.OperationJumpIfEqual, eqLabel)
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, eqLabel)
}

// emitStringCompareByteByByte emits the REP CMPSB byte-by-byte comparison and branches to
// neLabel if any byte differs.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes neLabel (string) which is the not-equal branch label.
func emitStringCompareByteByByte(e *asmgen.Emitter, neLabel string) {
	inst(e, asmamd64.OperationMove64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, DI")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	e.Instruction(asmamd64.InstructionRepeatStringPrefix)
	e.Instruction(asmamd64.InstructionCompareStringByte)
	inst(e, asmamd64.OperationJumpIfNotEqual, neLabel)
}

// emitStringCompareResultLabels emits the equal, not-equal, and done labels together with
// the result store and DISPATCH_NEXT.
//
// When equalResult is true (used by EmitEqualString), the eqLabel sets BX = 1 and the
// neLabel sets BX = 0. When equalResult is false (used by EmitNotEqualString), the values
// are inverted. The doneLabel stores BX into ints[AX] and dispatches.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes eqLabel (string) which is the equal branch label name.
// Takes neLabel (string) which is the not-equal branch label name.
// Takes doneLabel (string) which is the final store and dispatch label name.
// Takes equalResult (bool) which controls the result polarity.
func emitStringCompareResultLabels(e *asmgen.Emitter, eqLabel, neLabel, doneLabel string, equalResult bool) {
	e.Blank()
	e.Label(eqLabel)
	if equalResult {
		inst(e, asmamd64.OperationMove64Bits, "$1, BX")
	} else {
		inst(e, asmamd64.OperationBitwiseXor64Bits, "BX, BX")
	}
	inst(e, asmamd64.OperationJump, doneLabel)
	e.Blank()
	e.Label(neLabel)
	if equalResult {
		inst(e, asmamd64.OperationBitwiseXor64Bits, "BX, BX")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "$1, BX")
	}
	e.Blank()
	e.Label(doneLabel)
	inst(e, asmamd64.OperationMove64Bits, "BX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
}

// emitSliceStringExtractAndLoadHeader emits the operand extraction and loads the source
// string header for EmitSliceString.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringExtractAndLoadHeader(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*1), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), BX")
}

// emitSliceStringLoadExtensionWord loads the second instruction word (which carries the
// low and high bound register indices) and advances the program counter past it.
//
// After this sequence: DX = extension word value, R14 incremented by 1.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringLoadExtensionWord(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
}

// emitSliceStringComputeLowBound computes the low bound for the string slice, defaulting
// to 0 when flag bit 0 is clear.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringComputeLowBound(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationBitwiseXor64Bits, "SI, SI")
	inst(e, asmamd64.OperationTest8Bits, "$1, CL")
	inst(e, asmamd64.OperationJumpIfZero, "sl_no_low")
	inst(e, asmamd64.OperationMove64Bits, "DX, SI")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, SI")
	inst(e, asmamd64.OperationBitwiseAnd32Bits, "$0xFF, SI")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(SI*8), SI")
	e.Blank()
	e.Label("sl_no_low")
}

// emitSliceStringComputeHighBound computes the high bound for the string slice,
// defaulting to the original string length when flag bit 1 is clear.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringComputeHighBound(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationTest8Bits, "$2, CL")
	inst(e, asmamd64.OperationJumpIfZero, "sl_default_high")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, CX")
	inst(e, asmamd64.OperationBitwiseAnd32Bits, "$0xFF, CX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(CX*8), CX")
	inst(e, asmamd64.OperationJump, "sl_got_high")
	e.Blank()
	e.Label("sl_default_high")
	inst(e, asmamd64.OperationMove64Bits, "BX, CX")
	e.Blank()
	e.Label("sl_got_high")
}

// emitSliceStringValidateAndStore validates 0 <= low <= high <= len, stores the new
// string header on success, and jumps to sl_bounds_fail on violation.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringValidateAndStore(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfSign, labelSliceBoundsFail)
	inst(e, asmamd64.OperationCompare64Bits, "CX, SI")
	inst(e, asmamd64.OperationJumpIfLessSigned, labelSliceBoundsFail)
	inst(e, asmamd64.OperationCompare64Bits, "CX, BX")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelSliceBoundsFail)
	inst(e, asmamd64.OperationAdd64Bits, "SI, DI")
	inst(e, asmamd64.OperationSubtract64Bits, "SI, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, AX")
	inst(e, asmamd64.OperationMove64Bits, "DI, (SI)(AX*1)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(SI)(AX*1)")
	e.Instruction(macroDispatchNext)
}

// emitSliceStringBoundsFail emits the bounds-failure exit for EmitSliceString, rewinding
// pc by two and returning to Go with EXIT_TIER2.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitSliceStringBoundsFail(e *asmgen.Emitter) {
	e.Blank()
	e.Label(labelSliceBoundsFail)
	inst(e, asmamd64.OperationSubtract64Bits, "$2, R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_TIER2, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// emitStringIndexToIntBoundsCheckAndStore emits the bounds check, byte load, store to the
// integer bank via the pinned R8 register, DISPATCH_NEXT, and the fallback exit.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitStringIndexToIntBoundsCheckAndStore(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfSign, "sit_fallback")
	inst(e, asmamd64.OperationCompare64Bits, "CX, SI")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, "sit_fallback")
	inst(e, asmamd64.OperationMove8To64BitsZeroExtended, "(DI)(CX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
	e.Blank()
	e.Label("sit_fallback")
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_TIER2, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}

// emitLenStringLtLoadAndCompare emits operand extraction, string length loading, and the
// int-vs-length comparison for EmitLenStringLtJumpFalse.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitLenStringLtLoadAndCompare(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI)(BX*1), SI")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, SI")
	inst(e, asmamd64.OperationJumpIfLessSigned, "lsj_taken")
}

// emitLenStringLtJumpOffsetAndDispatch emits the branch-not-taken offset load and the
// branch-taken pc advance, both ending in DISPATCH_NEXT.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitLenStringLtJumpOffsetAndDispatch(e *asmgen.Emitter) {
	e.Blank()
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), DX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, DX")
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, "DX, DX")
	inst(e, asmamd64.OperationAdd64Bits, "DX, R14")
	emitBackEdgePoll(e)
	inst(e, asmamd64.OperationJump, "lsj_dispatch")
	e.Blank()
	e.Label("lsj_taken")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Blank()
	e.Label("lsj_dispatch")
	e.Instruction(macroDispatchNext)
}
