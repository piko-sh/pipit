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
	core "piko.sh/asmgen/asmgen_arch_amd64"
)

const (
	// mnemonicColumnWidth is the standard column alignment for amd64 Plan 9 assembly
	// mnemonics.
	mnemonicColumnWidth = 8

	// elementStrideShiftByte is log2(2) - one-byte stride doubled to the 2-byte 16-bit
	// numeric register lane.
	elementStrideShiftByte = 1

	// elementStrideShiftQword is log2(8) - the int64/float64 stride.
	elementStrideShiftQword = 3

	// elementStrideShiftXmm is log2(16) - the complex128 stride.
	elementStrideShiftXmm = 4

	// labelBoundsFail names the shared bounds-check failure trampoline emitted at the foot
	// of the dispatch file.
	labelBoundsFail = "bounds_fail"
)

var (
	// low8Map maps 64-bit register names to their 8-bit low counterparts (e.g. "AX" -> "AL",
	// "BX" -> "BL", "CX" -> "CL").
	low8Map = map[string]string{
		"AX": "AL", "BX": "BL", "CX": "CL",
		"SI": "SI", "DI": "DIB",
	}
)

// JumpTableEntry pairs a handler symbol name with its byte offset into the dispatch
// table.
type JumpTableEntry struct {
	// Name is the Plan-9 ASM symbol name of the handler (without the leading middle dot).
	// Example: "handlerAddInt".
	Name string

	// TableSymbol identifies which dispatch table the handler is installed into.
	TableSymbol string

	// Offset is the byte offset into the target table where the handler address is written.
	Offset int
}

// BytecodeAMD64Arch extends the core AMD64Arch with bytecode dispatch-specific operations
// for the pipit interpreter.
type BytecodeAMD64Arch struct {
	core.AMD64Arch

	// jumpTableEntries lists every handler-offset pair to patch into the jump table,
	// populated at construction time so the offsets reflect current opcode iota values.
	jumpTableEntries []JumpTableEntry
}

// DispatchNext implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) DispatchNext(e *asmgen.Emitter) { e.Instruction(macroDispatchNext) }

// DivisionByZeroExit implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) DivisionByZeroExit(e *asmgen.Emitter) {
	e.Instruction(asmamd64.InstructionDivByZeroExitMacro)
}

// EmitTruncateNarrow implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeAMD64Arch) EmitTruncateNarrow(e *asmgen.Emitter) {
	e.IndentedComment("Extract A (register index) into AX.")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	e.Blank()

	e.IndentedComment("Extract B (bit width: 8/16/32) into BX.")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	e.Blank()

	e.IndentedComment("Extract C (registerKind: 0=int, 5=uint) into CX.")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	e.Blank()

	e.IndentedComment("Branch on registerKind == registerUint (5).")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $5")
	inst(e, asmamd64.OperationJumpIfNotEqual, "handler_truncate_narrow_int")
	e.Blank()

	e.IndentedComment("--- Uint path: uints[A] &= (1 << B) - 1 ---")
	inst(e, asmamd64.OperationMove64Bits, "$1, SI")
	inst(e, asmamd64.OperationMove8Bits, "BL, CL")
	inst(e, asmamd64.OperationShiftLeft64Bits, "CL, SI")
	inst(e, asmamd64.OperationSubtract64Bits, "$1, SI")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "SI, (DI)(AX*8)")
	inst(e, asmamd64.OperationJump, "handler_truncate_narrow_done")
	e.Blank()

	e.Label("handler_truncate_narrow_int")
	e.IndentedComment("--- Int path: ints[A] = (ints[A] << (64-B)) >> (64-B)  (arithmetic) ---")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "$64, CX")
	inst(e, asmamd64.OperationSubtract64Bits, "BX, CX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "CL, SI")
	inst(e, asmamd64.OperationShiftRightArithmetic64Bits, "CL, SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (R8)(AX*8)")
	e.Blank()

	e.Label("handler_truncate_narrow_done")
	e.Instruction(macroDispatchNext)
}

// EmitJumpTableBootstrap implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter.
// Takes destSymbol (string) which is the Plan-9 symbol of the destination table.
// Takes sourceSymbols ([]string) which are the source-table symbols in destination-layout
// order.
// Takes entriesPerTable (int) which is the number of 8-byte slots contributed by each
// source.
func (*BytecodeAMD64Arch) EmitJumpTableBootstrap(
	e *asmgen.Emitter,
	destSymbol string,
	sourceSymbols []string,
	entriesPerTable int,
) {
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, fmt.Sprintf("%s(SB), DI", destSymbol))
	for _, source := range sourceSymbols {
		e.Blank()
		inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, fmt.Sprintf("%s(SB), SI", source))
		inst(e, asmamd64.OperationMove64Bits, fmt.Sprintf("$%d, CX", entriesPerTable))
		inst(e, asmamd64.InstructionRepeatStringPrefixInline, asmamd64.InstructionMoveStringQuad)
	}
	e.Blank()
	inst(e, asmamd64.OperationReturn, "")
}

// ExtractA implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dest (string) which is the destination register name.
func (*BytecodeAMD64Arch) ExtractA(e *asmgen.Emitter, dest string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, "+dest)
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, "+dest)
	low := low8Map[dest]
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, low+", "+dest)
}

// ExtractB implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dest (string) which is the destination register name.
func (*BytecodeAMD64Arch) ExtractB(e *asmgen.Emitter, dest string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, "+dest)
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, "+dest)
	low := low8Map[dest]
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, low+", "+dest)
}

// ExtractC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dest (string) which is the destination register name.
func (*BytecodeAMD64Arch) ExtractC(e *asmgen.Emitter, dest string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, "+dest)
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, "+dest)
}

// ExtractWideBC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dest (string) which is the destination register name.
func (*BytecodeAMD64Arch) ExtractWideBC(e *asmgen.Emitter, dest string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, "+dest)
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, "+dest)
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, dest+", "+dest)
}

// ExtractSignedBC implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes dest (string) which is the destination register name.
func (*BytecodeAMD64Arch) ExtractSignedBC(e *asmgen.Emitter, dest string) {
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, "+dest)
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, dest+", "+dest)
}

// EmitInlineGoCallTwoOperandShim implements BytecodeArchPort. It emits a NOSPLIT NOFRAME
// body for the 2-operand shim shape.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes goSymbol (string) which is the Plan-9 ASM symbol of the Go trampoline to call.
func (*BytecodeAMD64Arch) EmitInlineGoCallTwoOperandShim(e *asmgen.Emitter, goSymbol string) {
	e.Instruction(asmamd64.InstructionNoLocalPointers)
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_SAVED_PC(R15)")
	inst(e, asmamd64.OperationAdjustStackPointer, "$32")
	inst(e, asmamd64.OperationMove64Bits, "R15, 0(SP)")
	inst(e, asmamd64.OperationMove64Bits, "BX, 8(SP)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(SP)")
	inst(e, asmamd64.OperationCall, goSymbol)
	inst(e, asmamd64.OperationMove64Bits, "24(SP), R15")
	inst(e, asmamd64.OperationAdjustStackPointer, "$-32")
	inst(e, asmamd64.OperationMove64Bits, "CTX_SAVED_PC(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_BASE(R15), R12")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_LEN(R15), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INTS_BASE(R15), R8")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLOATS_BASE(R15), R9")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R15), R11")
	inst(e, asmamd64.OperationMove64Bits, "CTX_JUMP_TABLE(R15), R10")
	e.Instruction(macroDispatchNext)
}

// EmitSubOpStrconvFormatBool implements BytecodeArchPort. It emits a NOFRAME body that
// converts a bool register to its "true" or "false" string header branchlessly, without
// heap allocation.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitSubOpStrconvFormatBool(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(DI)(CX*1), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "\xc2\xb7boolStringFalse(SB), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "\xc2\xb7boolStringTrue(SB), SI")
	inst(e, asmamd64.OperationTest8Bits, "AL, AL")
	inst(e, asmamd64.OperationConditionalMove64BitsIfNotEqual, "SI, DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), AX")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_STRINGS_BASE(R15), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationMove64Bits, "AX, 0(SI)(BX*1)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 8(SI)(BX*1)")
	e.Instruction(macroDispatchNext)
}

// EmitInlineGoCallThreeOperandShim implements BytecodeArchPort. It emits a NOSPLIT
// NOFRAME body for the 3-operand shape "B = goFn(C, ext.A)".
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes goSymbol (string) which is the Plan-9 ASM symbol of the Go trampoline to call.
func (*BytecodeAMD64Arch) EmitInlineGoCallThreeOperandShim(e *asmgen.Emitter, goSymbol string) {
	e.Instruction(asmamd64.InstructionNoLocalPointers)
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_SAVED_PC(R15)")
	inst(e, asmamd64.OperationAdjustStackPointer, "$40")
	inst(e, asmamd64.OperationMove64Bits, "R15, 0(SP)")
	inst(e, asmamd64.OperationMove64Bits, "BX, 8(SP)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(SP)")
	inst(e, asmamd64.OperationMove64Bits, "AX, 24(SP)")
	inst(e, asmamd64.OperationCall, goSymbol)
	inst(e, asmamd64.OperationMove64Bits, "32(SP), R15")
	inst(e, asmamd64.OperationAdjustStackPointer, "$-40")
	inst(e, asmamd64.OperationMove64Bits, "CTX_SAVED_PC(R15), R14")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_BASE(R15), R12")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CODE_LEN(R15), R13")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INTS_BASE(R15), R8")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FLOATS_BASE(R15), R9")
	inst(e, asmamd64.OperationMove64Bits, "CTX_INT_CONSTS_BASE(R15), R11")
	inst(e, asmamd64.OperationMove64Bits, "CTX_JUMP_TABLE(R15), R10")
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceFloatGet implements BytecodeArchPort. It emits a bounds-checked load for
// floats[B] = slicesFloat[C][ints[ext.A]], falling back to pathBFallback on out-of-range
// access.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesFloat bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceFloatGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueAMD64(e, contextOffset)
	inst(e, asmamd64.OperationMoveScalarDouble, "(DI)(BX*8), X0")
	inst(e, asmamd64.OperationMoveScalarDouble, "X0, (R9)(AX*8)")
	emitTypedSliceTailAMD64(e)
}

// EmitSliceIndexStructFieldScalar implements BytecodeArchPort. It emits a tier-0 fast
// path for dest[A] = slice(general[B])[ints[C]].field(layout[ext]), including an
// element-type identity check that guards the compile-time layout against the runtime
// element type.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes destIsFloat (bool) which selects the float bank destination over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
//
//nolint:revive // Reason above.
func (*BytecodeAMD64Arch) EmitSliceIndexStructFieldScalar(e *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string) {
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "BX, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_STRUCT_LAYOUT_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sif_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(AX), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "OFF_LAYOUT_KIND(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindA)
	if kindB != "" {
		inst(e, asmamd64.OperationJumpIfEqual, "sif_kind_ok")
		inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindB)
	}
	inst(e, asmamd64.OperationJumpIfNotEqual, "sif_shim")
	if kindB != "" {
		e.Label("sif_kind_ok")
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "sif_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $23")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sif_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationMove64Bits, "48(CX), CX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "OFF_LAYOUT_TYPE_INDEX(SI), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_TYPE_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sif_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_TYPE_TABLE(AX), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, AX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, 8(AX)")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sif_shim")
	inst(e, asmamd64.OperationMove64Bits, "0(CX), AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, 8(DI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sif_shim")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "BX, AX")
	inst(e, asmamd64.OperationMove64Bits, "(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "sif_shim")
	inst(e, asmamd64.OperationAdd64Bits, "AX, DI")
	inst(e, asmamd64.OperationMove32To64BitsZeroExtended, "OFF_LAYOUT_OFFSET(SI), CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	if destIsFloat {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R9)(AX*8)")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	}
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
	e.Label("sif_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitDerefSliceIntAccess implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpDerefSliceGetInt (isSet=false: ints[A] =
// (*general[B])[ints[C]]) and isa.OpDerefSliceSetInt (isSet=true: (*general[A])[ints[B]]
// = ints[C]). Resolves the pointer from its general slot (both indirection forms), reads
// the slice header through it, bounds-checks against the live length, and performs the
// element access. Int element stores carry no pointers, so no write barrier applies. Nil
// pointers, non-pointer receivers, and out-of-range indexes fall back to the Go shim,
// which raises the matching panics.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes isSet (bool) which selects the write-side operand roles and store.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeAMD64Arch) EmitDerefSliceIntAccess(e *asmgen.Emitter, isSet bool, shimSymbol string) {
	ptrShift, idxShift := "$16", "$24"
	if isSet {
		ptrShift, idxShift = "$8", "$16"
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, ptrShift+", AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ds_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $22")
	inst(e, asmamd64.OperationJumpIfNotEqual, "ds_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), SI")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ds_direct")
	inst(e, asmamd64.OperationMove64Bits, "(SI), SI")
	e.Label("ds_direct")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "ds_shim")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, idxShift+", BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, 8(SI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "ds_shim")
	inst(e, asmamd64.OperationMove64Bits, "(SI), SI")
	if isSet {
		inst(e, asmamd64.OperationMove64Bits, "DX, AX")
		inst(e, asmamd64.OperationShiftRight64Bits, "$24, AX")
		inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(BX*8)")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "DX, AX")
		inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
		inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	}
	e.Instruction(macroDispatchNext)
	e.Label("ds_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitGetStructFieldGeneralPointerT0 implements BytecodeArchPort, emitting a tier-0 fast
// path for reading receiver[B].field(layout[C]) into general[A] covering pointer-kind and
// direct-interface-kind fields. Gated on the runtime write barrier because pointer words
// are stored with raw moves.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
//
//nolint:revive // Reason above.
func (*BytecodeAMD64Arch) EmitGetStructFieldGeneralPointerT0(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	emitStructFieldCommonPrologueAMD64(e, "$22", "$20", "$16")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "OFF_LAYOUT_KIND(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $20")
	inst(e, asmamd64.OperationJumpIfNotEqual, "gp_ptr_field")
	inst(e, asmamd64.OperationMove64Bits, "(DI), SI")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "gp_store_zero")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "23(SI), CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $18")
	inst(e, asmamd64.OperationJumpIfEqual, "gp_iface_store")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $19")
	inst(e, asmamd64.OperationJumpIfEqual, "gp_iface_store")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $21")
	inst(e, asmamd64.OperationJumpIfEqual, "gp_iface_store")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $22")
	inst(e, asmamd64.OperationJumpIfEqual, "gp_iface_store")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $26")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	e.Label("gp_iface_store")
	inst(e, asmamd64.OperationMove64Bits, "SI, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "BX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(DI)")
	inst(e, asmamd64.OperationJump, "gp_done")
	e.Label("gp_store_zero")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(DI)")
	e.Label("gp_done")
	e.Instruction(macroDispatchNext)
	e.Label("gp_ptr_field")
	inst(e, asmamd64.OperationMove64Bits, "(DI), BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "OFF_LAYOUT_FIELD_TYPE_INDEX(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, FN_TYPE_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_TYPE_TABLE(AX), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, AX")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), SI")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "SI, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "BX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "$22, 16(DI)")
	inst(e, asmamd64.OperationJump, "gp_done")
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimGetStructFieldGeneralT0(SB)")
}

// EmitGetStructFieldRawPointerT0 implements BytecodeArchPort, emitting a tier-0 fast path
// that reads a cycle-broken eface field through receiver[B] and re-wraps it as a
// pointer-kind value in general[A]. Gated on the runtime write barrier.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
//
//nolint:revive // Reason above.
func (*BytecodeAMD64Arch) EmitGetStructFieldRawPointerT0(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "rp_shim")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_STRUCT_LAYOUT_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "rp_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(AX), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "rp_zero")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $22")
	inst(e, asmamd64.OperationJumpIfEqual, "rp_ptr_recv")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $25")
	inst(e, asmamd64.OperationJumpIfNotEqual, "rp_shim")
	inst(e, asmamd64.OperationTest64Bits, "$0x100, CX")
	inst(e, asmamd64.OperationJumpIfZero, "rp_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationJump, "rp_have_base")
	e.Label("rp_ptr_recv")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "rp_have_base")
	inst(e, asmamd64.OperationMove64Bits, "(DI), DI")
	e.Label("rp_have_base")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "rp_zero")
	inst(e, asmamd64.OperationMove32To64BitsZeroExtended, "OFF_LAYOUT_OFFSET(SI), CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, DI")
	inst(e, asmamd64.OperationMove64Bits, "(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), SI")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(BX*8), DI")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "rp_store_zero")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "SI, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "$22, 16(DI)")
	inst(e, asmamd64.OperationJump, "rp_done")
	e.Label("rp_zero")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(BX*8), DI")
	e.Label("rp_store_zero")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(DI)")
	e.Label("rp_done")
	e.Instruction(macroDispatchNext)
	e.Label("rp_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimGetStructFieldRawPointerT0(SB)")
}

// EmitEqGeneral implements BytecodeArchPort. It emits a tier-0 fast path for ints[A] =
// (general[B] == general[C]), handling the both-invalid, differing-type and
// pointer-identity-kind comparisons inline.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
//
//nolint:revive // Reason above.
func (*BytecodeAMD64Arch) EmitEqGeneral(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(AX*8), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(AX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), AX")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, "eqg_a_invalid")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "eqg_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $20")
	inst(e, asmamd64.OperationJumpIfEqual, "eqg_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $20")
	inst(e, asmamd64.OperationJumpIfEqual, "eqg_shim")
	inst(e, asmamd64.OperationCompare64Bits, "AX, 0(DI)")
	inst(e, asmamd64.OperationJumpIfNotEqual, "eqg_store_zero")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $18")
	inst(e, asmamd64.OperationJumpIfEqual, "eqg_ptr_cmp")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $22")
	inst(e, asmamd64.OperationJumpIfEqual, "eqg_ptr_cmp")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $26")
	inst(e, asmamd64.OperationJumpIfNotEqual, "eqg_shim")
	e.Label("eqg_ptr_cmp")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), AX")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "eqg_a_direct")
	inst(e, asmamd64.OperationMove64Bits, "(AX), AX")
	e.Label("eqg_a_direct")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), BX")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "eqg_b_direct")
	inst(e, asmamd64.OperationMove64Bits, "(BX), BX")
	e.Label("eqg_b_direct")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, BX")
	inst(e, asmamd64.OperationJumpIfNotEqual, "eqg_store")
	inst(e, asmamd64.OperationMove64Bits, "$1, CX")
	inst(e, asmamd64.OperationJump, "eqg_store")
	e.Label("eqg_a_invalid")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfNotEqual, "eqg_shim")
	inst(e, asmamd64.OperationMove64Bits, "$1, CX")
	inst(e, asmamd64.OperationJump, "eqg_store")
	e.Label("eqg_store_zero")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "CX, CX")
	e.Label("eqg_store")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
	e.Label("eqg_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimEqGeneral(SB)")
}

// EmitMoveGeneralAlias implements BytecodeArchPort.
//
// Tier-0 fast path for isa.OpMoveGeneral's alias mode (C==1): a raw 24-byte reflect.Value
// slot copy general[A] = general[B]. The copy stores pointer words with plain moves, so
// it runs only while the garbage collector's write barrier is disabled - the handler
// gates on the runtime's flag every execution and falls back to the Go shim during
// marking, and snapshot/boundary modes always take the shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeAMD64Arch) EmitMoveGeneralAlias(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $1")
	inst(e, asmamd64.OperationJumpIfNotEqual, "mg_shim")
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "mg_shim")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(DI)")
	e.Instruction(macroDispatchNext)
	e.Label("mg_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimMoveGeneral(SB)")
}

// EmitTestNilJump implements BytecodeArchPort.
//
// Tier-0 body for isa.OpTestNilJumpTrue/False: tests whether general[A] holds an invalid
// value or a nil value of a nilable kind (Chan, Func, Interface, Map, Pointer, Slice -the
// contiguous reflect.Kind range 18..23) and branches by the signed 16-bit offset in
// B|C<<8 according to the polarity. Pure reads with no general-bank stores, so no write
// barrier consideration applies; the handler is total and needs no shim fallback. The
// taken branch shares handlerJump's poll-budget back-edge exit.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes jumpWhenNil (bool) which selects the True (jump on nil) polarity.
func (*BytecodeAMD64Arch) EmitTestNilJump(e *asmgen.Emitter, jumpWhenNil bool) {
	nilLabel, notNilLabel := "tnj_take", "tnj_skip"
	if !jumpWhenNil {
		nilLabel, notNilLabel = "tnj_skip", "tnj_take"
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, nilLabel)
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $18")
	inst(e, asmamd64.OperationJumpIfBelow, notNilLabel)
	inst(e, asmamd64.OperationCompare64Bits, "BX, $23")
	inst(e, asmamd64.OperationJumpIfAbove, notNilLabel)
	inst(e, asmamd64.OperationTest64Bits, "$0x200, CX")
	inst(e, asmamd64.OperationJumpIfNotZero, notNilLabel)
	inst(e, asmamd64.OperationMove64Bits, "8(DI), SI")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $20")
	inst(e, asmamd64.OperationJumpIfEqual, "tnj_deref")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $23")
	inst(e, asmamd64.OperationJumpIfEqual, "tnj_deref")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "tnj_word")
	e.Label("tnj_deref")
	inst(e, asmamd64.OperationMove64Bits, "(SI), SI")
	e.Label("tnj_word")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, nilLabel)
	inst(e, asmamd64.OperationJump, notNilLabel)
	e.Label("tnj_take")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, DX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "DX, AX")
	inst(e, asmamd64.OperationMove16To64BitsSignExtended, "AX, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, R14")
	inst(e, asmamd64.OperationDecrement64Bits, "CTX_POLL_BUDGET(R15)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, "tnj_skip")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_POLL, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
	e.Label("tnj_skip")
	e.Instruction(macroDispatchNext)
}

// EmitStructFieldScalarGetT0 implements BytecodeArchPort. It emits a tier-0 fast path for
// dest[A] = *(receiverBase + layout[C].Offset) with full-width scalar fields and zero
// layout flags.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes kindA (string) which is the primary accepted layout Kind byte.
// Takes kindB (string) which is the secondary accepted Kind byte, or "" when the family
// has only one.
// Takes bank (string) which names the destination bank ("ints", "floats", "uints",
// "bools").
// Takes shimSymbol (string) which is the fallback shim, e.g.
// "handlerPathBShimGetStructFieldIntT0".
func (*BytecodeAMD64Arch) EmitStructFieldScalarGetT0(e *asmgen.Emitter, kindA, kindB, bank, shimSymbol string) {
	spec := structFieldBankAMD64(bank)
	emitStructFieldCommonPrologueAMD64(e, kindA, kindB, "$16")
	inst(e, spec.fieldLoadOp, "(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	emitStructFieldBankStoreAMD64(e, spec)
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitStructFieldScalarSetT0 implements BytecodeArchPort.
//
// Tier-0 fast path for the scalar struct-field SET family: *(receiverBase +
// layout[C].Offset) = source[B]. Receiver operand is A; scalar stores carry no pointers
// so no write barrier is required. Shape gates mirror EmitStructFieldScalarGetT0.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes kindA (string) which is the primary accepted layout Kind byte.
// Takes kindB (string) which is the secondary accepted layout Kind byte.
// Takes bank (string) which names the source bank as in EmitStructFieldScalarGetT0.
// Takes shimSymbol (string) which is the fallback shim symbol.
func (*BytecodeAMD64Arch) EmitStructFieldScalarSetT0(e *asmgen.Emitter, kindA, kindB, bank, shimSymbol string) {
	spec := structFieldBankAMD64(bank)
	emitStructFieldCommonPrologueAMD64(e, kindA, kindB, "$8")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	emitStructFieldBankLoadAMD64(e, spec)
	inst(e, spec.fieldStoreOp, spec.fieldStoreSrc+", (DI)")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitTypedSliceIntGetDirect implements BytecodeArchPort. It emits the tier-0 body for
// ints[A] = slicesInt[B][ints[C]] with bounds check.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceIntGetDirect(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderAMD64(e, contextOffset, "$16")
	emitTypedSliceIntDirectIndexAMD64(e, "$24")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, labelBoundsFail)
	inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	emitTypedSliceIntDirectTailAMD64(e)
}

// EmitTypedSliceIntSetDirect implements BytecodeArchPort.
//
// Emits the tier-0 body for slicesInt[A][ints[B]] = ints[C] with bounds check. See
// EmitTypedSliceIntGetDirect for the tier-0 conventions.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceIntSetDirect(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderAMD64(e, contextOffset, "$8")
	emitTypedSliceIntDirectIndexAMD64(e, "$16")
	inst(e, asmamd64.OperationCompare64Bits, "BX, CX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, labelBoundsFail)
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, AX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(BX*8)")
	emitTypedSliceIntDirectTailAMD64(e)
}

// EmitTypedSliceIntGetDirectUnchecked implements BytecodeArchPort.
//
// Bounds-elided variant of EmitTypedSliceIntGetDirect: the compiler's BCE pass only emits
// the unchecked opcode when a dominating comparison proves the index in range.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceIntGetDirectUnchecked(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderAMD64(e, contextOffset, "$16")
	emitTypedSliceIntDirectIndexAMD64(e, "$24")
	inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceIntSetDirectUnchecked implements BytecodeArchPort.
//
// Bounds-elided variant of EmitTypedSliceIntSetDirect for BCE-proven sites.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesInt bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceIntSetDirectUnchecked(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceIntDirectHeaderAMD64(e, contextOffset, "$8")
	emitTypedSliceIntDirectIndexAMD64(e, "$16")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, AX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(BX*8)")
	e.Instruction(macroDispatchNext)
}

// EmitLoadUintConst implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uintConstants[B|(C<<8)]. Pool indexes are compiler-generated
// so no bounds check is emitted, mirroring handlerLoadIntConst.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitLoadUintConst(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "BX, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(AX*8)")
	e.Instruction(macroDispatchNext)
}

// EmitLoadUintConstSmall implements BytecodeArchPort.
//
// Tier-1 body for uints[B] = uint64(C), the immediate small-constant loader.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitLoadUintConstSmall(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "BX, (SI)(AX*8)")
	e.Instruction(macroDispatchNext)
}

// EmitUintConstArith implements BytecodeArchPort.
//
// Tier-1 body for uints[B] = uints[C] op uintConstants[ext.A|(ext.B<<8)], consuming the
// extension word (PC is advanced past it before DISPATCH_NEXT).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which selects the instruction: "add", "sub", or "and".
func (*BytecodeAMD64Arch) EmitUintConstArith(e *asmgen.Emitter, operation string) {
	mnemonics := map[string]string{
		"add": asmamd64.OperationAdd64Bits,
		"sub": asmamd64.OperationSubtract64Bits,
		"and": asmamd64.OperationBitwiseAnd64Bits,
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, CX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "CX, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINT_CONSTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(CX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), DI")
	inst(e, mnemonics[operation], "CX, DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, (SI)(AX*8)")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
}

// EmitUintDivide implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uints[B] / uints[C]; a zero divisor exits through
// DIV_BY_ZERO_EXIT so the Go side raises the interpreted panic. The instruction word in
// DX is fully decoded before DIVQ clobbers DX; DISPATCH_NEXT reloads it.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitUintDivide(e *asmgen.Emitter) {
	emitUintDivRemPrologueAMD64(e)
	inst(e, asmamd64.OperationBitwiseExclusiveOr32Bits, "DX, DX")
	inst(e, asmamd64.OperationUnsignedDivide64Bits, "CX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (SI)(DI*8)")
	e.Instruction(macroDispatchNext)
	e.Label("dbz")
	e.Instruction("DIV_BY_ZERO_EXIT()")
}

// EmitUintRemainder implements BytecodeArchPort.
//
// Tier-0 body for uints[A] = uints[B] % uints[C]; shares EmitUintDivide's zero-divisor
// discipline but stores the remainder.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitUintRemainder(e *asmgen.Emitter) {
	emitUintDivRemPrologueAMD64(e)
	inst(e, asmamd64.OperationBitwiseExclusiveOr32Bits, "DX, DX")
	inst(e, asmamd64.OperationUnsignedDivide64Bits, "CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, (SI)(DI*8)")
	e.Instruction(macroDispatchNext)
	e.Label("dbz")
	e.Instruction("DIV_BY_ZERO_EXIT()")
}

// EmitTypedSliceFloatSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesFloat bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceFloatSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueAMD64(e, contextOffset)
	emitPeekExtensionWordAFieldAMD64(e, "AX")
	inst(e, asmamd64.OperationMoveScalarDouble, "(R9)(AX*8), X0")
	inst(e, asmamd64.OperationMoveScalarDouble, "X0, (DI)(BX*8)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceUintGet implements BytecodeArchPort. It emits a bounds-checked load for
// uints[B] = slicesUint[C][ints[ext.A]].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesUint bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceUintGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueAMD64(e, contextOffset)
	inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(AX*8)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceUintSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesUint bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceUintSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueAMD64(e, contextOffset)
	emitPeekExtensionWordAFieldAMD64(e, "AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(AX*8), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, (DI)(BX*8)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceBoolGet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesBool bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceBoolGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueAMD64(e, contextOffset)
	inst(e, asmamd64.OperationMove8Bits, "(DI)(BX*1), CL")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove8Bits, "CL, (SI)(AX*1)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceBoolSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesBool bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceBoolSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueAMD64(e, contextOffset)
	emitPeekExtensionWordAFieldAMD64(e, "AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove8Bits, "(SI)(AX*1), AL")
	inst(e, asmamd64.OperationMove8Bits, "AL, (DI)(BX*1)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceByteGet implements BytecodeArchPort. It emits a bounds-checked load for
// uints[B] = uint64(slicesByte[C][ints[ext.A]]).
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceByteGet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceGetPrologueAMD64(e, contextOffset)
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(DI)(BX*1), CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "CX, (SI)(AX*8)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceByteSet implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceByteSet(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceSetPrologueAMD64(e, contextOffset)
	emitPeekExtensionWordAFieldAMD64(e, "AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(AX*8), AX")
	inst(e, asmamd64.OperationMove8Bits, "AL, (DI)(BX*1)")
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceByteSlice implements BytecodeArchPort. It emits the body of slicesByte[B]
// = slicesByte[C][low:high], handling only the low+high flag combination in ASM and
// falling back to pathBFallback for other shapes.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the slicesByte bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceByteSlice(e *asmgen.Emitter, contextOffset string) {
	emitTypedSliceByteSliceExtractOperands(e)
	emitTypedSliceByteSliceLoadExtAndBounds(e, contextOffset)
	emitTypedSliceByteSliceWriteHeader(e, contextOffset)
	emitTypedSliceTailAMD64(e)
}

// EmitTypedSliceMove implements BytecodeArchPort. It emits the body of subOpMoveSlice for
// slicesX[B] = slicesX[C], copying the 24-byte slice header.
//
// R10 must not be used as scratch because DISPATCH_NEXT dereferences it as the jump-table
// base.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
func (*BytecodeAMD64Arch) EmitTypedSliceMove(e *asmgen.Emitter, contextOffset string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")

	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), DX")

	inst(e, asmamd64.OperationMove64Bits, contextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, SI")
	inst(e, asmamd64.OperationMove64Bits, "DI, 0(SI)")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(SI)")
	inst(e, asmamd64.OperationMove64Bits, "DX, 16(SI)")

	e.Instruction(macroDispatchNext)
}

// EmitTypedSliceSliceSlice implements BytecodeArchPort. It emits the body of slicesX[A] =
// slicesX[C][low:high] with a configurable element stride, handling only the low+high
// flag combination in ASM.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes contextOffset (string) which is the byte offset of the typed-slice bank base
// pointer within the dispatchContext.
// Takes elementSizeShift (uint8) which is the log2 of the element stride.
func (*BytecodeAMD64Arch) EmitTypedSliceSliceSlice(e *asmgen.Emitter, contextOffset string, elementSizeShift uint8) {
	emitTypedSliceByteSliceExtractOperands(e)
	emitTypedSliceByteSliceLoadExtAndBounds(e, contextOffset)
	emitTypedSliceSliceSliceWriteHeader(e, contextOffset, elementSizeShift)
	emitTypedSliceTailAMD64(e)
}

// structFieldBankSpecAMD64 carries the per-bank addressing and move forms used by the
// scalar struct-field handlers. base is either a pinned register ("R8"/"R9") or a
// CTX_*_BASE define that must first be loaded from the context.
type structFieldBankSpecAMD64 struct {
	// base is the pinned register name or CTX define for the bank base.
	base string

	// fieldLoadOp is the mnemonic used to load a field value.
	fieldLoadOp string

	// fieldStoreOp is the mnemonic used to store a value into a field.
	fieldStoreOp string

	// fieldStoreSrc is the source register name for field stores.
	fieldStoreSrc string

	// bankStoreOp is the mnemonic used to store a value into the bank.
	bankStoreOp string

	// bankLoadOp is the mnemonic used to load a value from the bank.
	bankLoadOp string

	// baseIsPinned is true when base names a pinned register.
	baseIsPinned bool

	// byteIndexed is true when the bank is byte-indexed.
	byteIndexed bool
}

// structFieldBankAMD64 maps a bank name from the arch-neutral handler definition to its
// amd64 addressing spec. Unknown names panic at generation time, which asmgen surfaces as
// a build failure rather than silently emitting wrong assembly.
//
// Takes bank (string) which is "ints", "floats", "uints" or "bools".
//
// Returns structFieldBankSpecAMD64 which is the resolved addressing spec.
func structFieldBankAMD64(bank string) structFieldBankSpecAMD64 {
	switch bank {
	case "ints":
		return structFieldBankSpecAMD64{
			base:          "R8",
			baseIsPinned:  true,
			fieldLoadOp:   asmamd64.OperationMove64Bits,
			fieldStoreOp:  asmamd64.OperationMove64Bits,
			fieldStoreSrc: "CX",
			bankStoreOp:   asmamd64.OperationMove64Bits,
			bankLoadOp:    asmamd64.OperationMove64Bits,
			byteIndexed:   false,
		}
	case "floats":
		return structFieldBankSpecAMD64{
			base:          "R9",
			baseIsPinned:  true,
			fieldLoadOp:   asmamd64.OperationMove64Bits,
			fieldStoreOp:  asmamd64.OperationMove64Bits,
			fieldStoreSrc: "CX",
			bankStoreOp:   asmamd64.OperationMove64Bits,
			bankLoadOp:    asmamd64.OperationMove64Bits,
			byteIndexed:   false,
		}
	case "uints":
		return structFieldBankSpecAMD64{
			base:          "CTX_UINTS_BASE",
			fieldLoadOp:   asmamd64.OperationMove64Bits,
			fieldStoreOp:  asmamd64.OperationMove64Bits,
			fieldStoreSrc: "CX",
			bankStoreOp:   asmamd64.OperationMove64Bits,
			bankLoadOp:    asmamd64.OperationMove64Bits,
			baseIsPinned:  false,
			byteIndexed:   false,
		}
	case "bools":
		return structFieldBankSpecAMD64{
			base:          "CTX_BOOLS_BASE",
			fieldLoadOp:   asmamd64.OperationMove8To32BitsZeroExtended,
			fieldStoreOp:  asmamd64.OperationMove8Bits,
			fieldStoreSrc: "CL",
			bankStoreOp:   asmamd64.OperationMove8Bits,
			bankLoadOp:    asmamd64.OperationMove8To32BitsZeroExtended,
			byteIndexed:   true,
			baseIsPinned:  false,
		}
	default:
		panic("structFieldBankAMD64: unknown bank " + bank)
	}
}

// emitStructFieldBankStoreAMD64 stores CX (CL for the byte-indexed bool bank) into the
// destination bank at index AX.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes spec (structFieldBankSpecAMD64) which is the resolved bank addressing spec.
func emitStructFieldBankStoreAMD64(e *asmgen.Emitter, spec structFieldBankSpecAMD64) {
	target := spec.base
	if !spec.baseIsPinned {
		inst(e, asmamd64.OperationMove64Bits, spec.base+"(R15), SI")
		target = "SI"
	}
	if spec.byteIndexed {
		inst(e, spec.bankStoreOp, "CL, ("+target+")(AX*1)")
		return
	}
	inst(e, spec.bankStoreOp, "CX, ("+target+")(AX*8)")
}

// emitStructFieldBankLoadAMD64 loads the source bank value at index AX into CX.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes spec (structFieldBankSpecAMD64) which is the resolved bank addressing spec.
func emitStructFieldBankLoadAMD64(e *asmgen.Emitter, spec structFieldBankSpecAMD64) {
	target := spec.base
	if !spec.baseIsPinned {
		inst(e, asmamd64.OperationMove64Bits, spec.base+"(R15), SI")
		target = "SI"
	}
	if spec.byteIndexed {
		inst(e, spec.bankLoadOp, "("+target+")(AX*1), CX")
		return
	}
	inst(e, spec.bankLoadOp, "("+target+")(AX*8), CX")
}

// emitTypedSliceIntDirectHeaderAMD64 extracts the slice-slot operand at slotShift from
// the instruction word and loads the slicesInt header: DI = data pointer, CX = length.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes sliceContextOffset (string) which is the slice bank base offset define.
// Takes slotShift (string) which is the bit shift selecting the slot operand ("$8" for
// the A field, "$16" for B).
func emitTypedSliceIntDirectHeaderAMD64(e *asmgen.Emitter, sliceContextOffset string, slotShift string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, slotShift+", BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, sliceContextOffset+"(R15), SI")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$24, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
}

// emitTypedSliceIntDirectIndexAMD64 loads the element index value into BX from the
// int-bank register named by the operand at indexShift.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes indexShift (string) which is the bit shift selecting the index-register operand.
func emitTypedSliceIntDirectIndexAMD64(e *asmgen.Emitter, indexShift string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, indexShift+", BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
}

// emitTypedSliceIntDirectTailAMD64 emits DISPATCH_NEXT plus the shared bounds-fail
// trampoline for the tier-0 slice-int handlers. Unlike emitTypedSliceTailAMD64 there is
// no PC increment: tier-0 opcodes consume no extension word.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitTypedSliceIntDirectTailAMD64(e *asmgen.Emitter) {
	e.Instruction(macroDispatchNext)
	e.Label(labelBoundsFail)
	inst(e, asmamd64.OperationJump, "·pathBFallback(SB)")
}

// emitUintDivRemPrologueAMD64 decodes the three uint div/rem operands and loads the
// dividend/divisor: on exit DI = destination uint register index, AX = dividend value, CX
// = divisor value (zero-checked), SI = uint bank base.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitUintDivRemPrologueAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, DI")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), SI")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(CX*8), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "dbz")
	inst(e, asmamd64.OperationMove64Bits, "(SI)(BX*8), AX")
}
