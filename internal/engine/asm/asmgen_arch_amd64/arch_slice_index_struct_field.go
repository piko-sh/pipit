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

// EmitSliceIndexStructFieldScalar implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes destIsFloat (bool) which selects the float bank destination over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeAMD64Arch) EmitSliceIndexStructFieldScalar(e *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string) {
	emitSliceIndexStructFieldLayoutAMD64(e, kindA, kindB)
	emitSliceIndexStructFieldAddressAMD64(e)
	inst(e, asmamd64.OperationMove64Bits, "(DI), CX")
	emitSliceIndexStructFieldRegisterAMD64(e)
	if destIsFloat {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R9)(AX*8)")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	}
	emitSliceIndexStructFieldTailAMD64(e, shimSymbol)
}

// EmitSliceIndexStructFieldScalarStore implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes sourceIsFloat (bool) which selects the float bank source over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeAMD64Arch) EmitSliceIndexStructFieldScalarStore(e *asmgen.Emitter, kindA, kindB string, sourceIsFloat bool, shimSymbol string) {
	emitSliceIndexStructFieldLayoutAMD64(e, kindA, kindB)
	emitSliceIndexStructFieldAddressAMD64(e)
	emitSliceIndexStructFieldRegisterAMD64(e)
	if sourceIsFloat {
		inst(e, asmamd64.OperationMove64Bits, "(R9)(AX*8), CX")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
	}
	inst(e, asmamd64.OperationMove64Bits, "CX, (DI)")
	emitSliceIndexStructFieldTailAMD64(e, shimSymbol)
}

// emitSliceIndexStructFieldLayoutAMD64 finds the struct layout the extension word names
// and checks its kind, leaving the layout in SI and the function in AX, or jumps to
// element_field_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
func emitSliceIndexStructFieldLayoutAMD64(e *asmgen.Emitter, kindA, kindB string) {
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "BX, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_STRUCT_LAYOUT_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "element_field_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(AX), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "OFF_LAYOUT_KIND(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindA)
	if kindB != "" {
		inst(e, asmamd64.OperationJumpIfEqual, "element_field_kind_ok")
		inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindB)
	}
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_field_shim")
	if kindB != "" {
		e.Label("element_field_kind_ok")
	}
}

// emitSliceIndexStructFieldAddressAMD64 checks that general[B] is a slice of the layout's
// struct type and that ints[C] is inside it, leaving the address of the field in DI, or
// jumps to element_field_shim.
//
// On entry SI holds the layout and AX the function.
//
// A value of slice kind is always held indirectly, so the word after its type is read as
// the address of the slice header without a test of the flag.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSliceIndexStructFieldAddressAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "element_field_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$FLAG_KIND_MASK, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $REFLECT_SLICE")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_field_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationMove64Bits, "48(CX), CX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "OFF_LAYOUT_TYPE_INDEX(SI), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_TYPE_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "element_field_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_TYPE_TABLE(AX), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, AX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, 8(AX)")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_field_shim")
	inst(e, asmamd64.OperationMove64Bits, "0(CX), AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, 8(DI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "element_field_shim")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "BX, AX")
	inst(e, asmamd64.OperationMove64Bits, "(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "element_field_shim")
	inst(e, asmamd64.OperationAdd64Bits, "AX, DI")
	inst(e, asmamd64.OperationMove32To64BitsZeroExtended, "OFF_LAYOUT_OFFSET(SI), CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, DI")
}

// emitSliceIndexStructFieldRegisterAMD64 leaves operand A, the register read from or
// written to, in AX.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSliceIndexStructFieldRegisterAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
}

// emitSliceIndexStructFieldTailAMD64 steps over the extension word and dispatches, and
// emits the shim exit.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the fallback shim handler name.
func emitSliceIndexStructFieldTailAMD64(e *asmgen.Emitter, shimSymbol string) {
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
	e.Label("element_field_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}
