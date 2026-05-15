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

// EmitSetStructFieldGeneralT0 implements BytecodeArchPort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitSetStructFieldGeneralT0(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, sf_shim")
	emitStructFieldLayoutRowARM64(e)
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_FLAGS(R5), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R6, ss_flags")
	emitSetStructFieldGeneralPointerArmARM64(e)
	e.Label("ss_flags")
	emitSetStructFieldGeneralCycleBrokenArmARM64(e)
	e.Label("ss_done")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimSetStructFieldGeneralT0(SB)")
}

// emitSetStructFieldGeneralPointerArmARM64 emits the plain pointer-field store arm.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldGeneralPointerArmARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_KIND(R5), R6")
	inst5(e, asmarm64.OperationCompare, "$22, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	emitStructFieldReceiverBaseARM64(e, "$8", "ssp")
	emitSetStructFieldValueSlotARM64(e)
	inst5(e, asmarm64.OperationMove64Bits, "0(R8), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, ss_ptr_nil")
	emitSetStructFieldFunctionPointerARM64(e)
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "OFF_LAYOUT_FIELD_TYPE_INDEX(R5), R4")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE_LEN(R7), R3")
	inst5(e, asmarm64.OperationCompare, "R3, R4")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE(R7), R7")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R4, R4")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R9")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R8), R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R3, R4")
	inst5(e, asmarm64.OperationCompare, "$22, R4")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R8), R4")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R3, ss_ptr_store")
	inst5(e, asmarm64.OperationMove64Bits, "(R4), R4")
	e.Label("ss_ptr_store")
	inst5(e, asmarm64.OperationMove64Bits, "R4, (R6)")
	inst5(e, asmarm64.OperationBranch, "ss_done")
	e.Label("ss_ptr_nil")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R6)")
	inst5(e, asmarm64.OperationBranch, "ss_done")
}

// emitSetStructFieldGeneralCycleBrokenArmARM64 emits the cycle-broken `any` field store
// arm for ARM64.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldGeneralCycleBrokenArmARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationCompare, "$LAYOUT_FLAG_CYCLE_BROKEN, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "OFF_LAYOUT_KIND(R5), R6")
	inst5(e, asmarm64.OperationCompare, "$20, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R8")
	inst5(e, asmarm64.OperationAdd, "R4, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "16(R8), R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R3, R3")
	inst5(e, asmarm64.OperationCompare, "$22, R3")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "0(R8), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "ABI_TYPE_PTR_ELEM(R9), R9")
	emitSetStructFieldFunctionPointerARM64(e)
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "OFF_LAYOUT_TYPE_INDEX(R5), R4")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE_LEN(R7), R3")
	inst5(e, asmarm64.OperationCompare, "R3, R4")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "sf_shim")
	inst5(e, asmarm64.OperationMove64Bits, "FN_TYPE_TABLE(R7), R7")
	inst5(e, asmarm64.OperationLogicalShiftLeft, "$4, R4, R4")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "8(R7), R7")
	inst5(e, asmarm64.OperationCompare, "R7, R9")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	emitStructFieldReceiverBaseARM64(e, "$8", "ssi")
	emitSetStructFieldValueSlotARM64(e)
	inst5(e, asmarm64.OperationMove64Bits, "0(R8), R9")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, ss_iface_nil")
	inst5(e, asmarm64.OperationMove64Bits, "16(R8), R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0x1F, R3, R4")
	inst5(e, asmarm64.OperationCompare, "$18, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "ss_iface_data")
	inst5(e, asmarm64.OperationCompare, "$19, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "ss_iface_data")
	inst5(e, asmarm64.OperationCompare, "$21, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "ss_iface_data")
	inst5(e, asmarm64.OperationCompare, "$22, R4")
	inst5(e, asmarm64.OperationBranchIfEqual, "ss_iface_data")
	inst5(e, asmarm64.OperationCompare, "$26, R4")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "sf_shim")
	e.Label("ss_iface_data")
	inst5(e, asmarm64.OperationMove64Bits, "8(R8), R4")
	inst5(e, asmarm64.OperationTestBitAndBranchIfZero, "$7, R3, ss_iface_store")
	inst5(e, asmarm64.OperationMove64Bits, "(R4), R4")
	e.Label("ss_iface_store")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 0(R6)")
	inst5(e, asmarm64.OperationMove64Bits, "R4, 8(R6)")
	inst5(e, asmarm64.OperationBranch, "ss_done")
	e.Label("ss_iface_nil")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, R4")
	inst5(e, asmarm64.OperationBranch, "ss_iface_store")
}

// emitSetStructFieldValueSlotARM64 loads the address of general[B] into R8. R3 and R4 are
// scratch; R0, R5 and R6 are preserved.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldValueSlotARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R8")
	inst5(e, asmarm64.OperationAdd, "R4, R8, R8")
}

// emitSetStructFieldFunctionPointerARM64 loads the current frame's *CompiledFunction into
// R7. R4 is scratch.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldFunctionPointerARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_FRAME_POINTER(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "$CALLFRAME_SIZE, R7")
	inst5(e, asmarm64.OperationMultiply, "R7, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CSTACK_BASE(R19), R7")
	inst5(e, asmarm64.OperationAdd, "R4, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R7), R7")
}
