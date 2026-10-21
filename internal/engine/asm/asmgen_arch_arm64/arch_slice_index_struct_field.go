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

// EmitSliceIndexStructFieldScalar implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes destIsFloat (bool) which selects the float bank destination over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeARM64Arch) EmitSliceIndexStructFieldScalar(e *asmgen.Emitter, kindA, kindB string, destIsFloat bool, shimSymbol string) {
	emitSliceIndexStructFieldLayoutARM64(e, kindA, kindB)
	emitSliceIndexStructFieldAddressARM64(e)
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R7")
	emitSliceIndexStructFieldRegisterARM64(e)
	if destIsFloat {
		inst5(e, asmarm64.OperationMove64Bits, "R7, (R24)(R3<<3)")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "R7, (R23)(R3<<3)")
	}
	emitSliceIndexStructFieldTailARM64(e, shimSymbol)
}

// EmitSliceIndexStructFieldScalarStore implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes sourceIsFloat (bool) which selects the float bank source over ints.
// Takes shimSymbol (string) which is the fallback shim handler name.
func (*BytecodeARM64Arch) EmitSliceIndexStructFieldScalarStore(e *asmgen.Emitter, kindA, kindB string, sourceIsFloat bool, shimSymbol string) {
	emitSliceIndexStructFieldLayoutARM64(e, kindA, kindB)
	emitSliceIndexStructFieldAddressARM64(e)
	emitSliceIndexStructFieldRegisterARM64(e)
	if sourceIsFloat {
		inst5(e, asmarm64.OperationMove64Bits, "(R24)(R3<<3), R7")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R7")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R6)")
	emitSliceIndexStructFieldTailARM64(e, shimSymbol)
}

// emitSliceIndexStructFieldLayoutARM64 finds the struct layout the extension word names
// and checks its kind, leaving the layout in R5 and the function in R10, or branches to
// element_field_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
func emitSliceIndexStructFieldLayoutARM64(e *asmgen.Emitter, kindA, kindB string) {
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
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "element_field_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(R10), R5")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R5, R5")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_KIND(R5), R6")
	inst5(e, asmarm64.OperationCompare, kindA+", R6")
	if kindB != "" {
		inst5(e, asmarm64.OperationBranchIfEqual, "element_field_kind_ok")
		inst5(e, asmarm64.OperationCompare, kindB+", R6")
	}
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_field_shim")
	if kindB != "" {
		e.Label("element_field_kind_ok")
	}
}

// emitSliceIndexStructFieldAddressARM64 checks that general[B] is a slice of the layout's
// struct type and that ints[C] is inside it, leaving the address of the field in R6, or
// branches to element_field_shim.
//
// On entry R5 holds the layout and R10 the function.
//
// A value of slice kind is always held indirectly, so the word after its type is read as
// the address of the slice header without a test of the flag.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSliceIndexStructFieldAddressARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "0(R6), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, element_field_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R6), R8")
	inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_KIND_MASK, R8, R8")
	inst5(e, asmarm64.OperationCompare, "$REFLECT_SLICE, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_field_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R6")
	inst5(e, asmarm64.OperationMove64Bits, "48(R7), R7")
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "OFF_LAYOUT_TYPE_INDEX(R5), R3")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE_LEN(R10), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "element_field_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE(R10), R4")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "8(R4), R4")
	inst5(e, asmarm64.OperationCompare, "R4, R7")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_field_shim")
	inst5(e, asmarm64.OperationMove64Bits, "0(R7), R4")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R3")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "element_field_shim")
	inst5(e, asmarm64.OperationMultiply, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "(R6), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, element_field_shim")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "OFF_LAYOUT_OFFSET(R5), R7")
	inst5(e, asmarm64.OperationAdd, "R7, R6, R6")
}

// emitSliceIndexStructFieldRegisterARM64 leaves operand A, the register read from or
// written to, in R3.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSliceIndexStructFieldRegisterARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
}

// emitSliceIndexStructFieldTailARM64 steps over the extension word and dispatches, and
// emits the shim exit.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the fallback shim handler name.
func emitSliceIndexStructFieldTailARM64(e *asmgen.Emitter, shimSymbol string) {
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
	e.Label("element_field_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}
