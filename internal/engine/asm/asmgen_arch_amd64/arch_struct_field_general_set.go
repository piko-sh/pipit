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

// EmitSetStructFieldGeneralT0 emits the tier-0 fast path for writing general[B] into a
// struct field covering pointer-kind and cycle-broken any fields.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeAMD64Arch) EmitSetStructFieldGeneralT0(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	emitStructFieldLayoutRowAMD64(e)
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "OFF_LAYOUT_FLAGS(SI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfNotZero, "ss_flags")
	emitSetStructFieldGeneralPointerArmAMD64(e)
	e.Label("ss_flags")
	emitSetStructFieldGeneralCycleBrokenArmAMD64(e)
	e.Label("ss_done")
	e.Instruction(macroDispatchNext)
	e.Label("sf_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimSetStructFieldGeneralT0(SB)")
}

// emitSetStructFieldGeneralPointerArmAMD64 emits the plain pointer-field store arm,
// comparing the value's type word against the function type table entry before storing
// the data word.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldGeneralPointerArmAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "OFF_LAYOUT_KIND(SI), $22")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	emitStructFieldReceiverBaseAMD64(e, "$8", "ssp")
	emitSetStructFieldValueSlotAMD64(e)
	inst(e, asmamd64.OperationMove64Bits, "0(BX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ss_ptr_nil")
	emitSetStructFieldFunctionPointerAMD64(e)
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "OFF_LAYOUT_FIELD_TYPE_INDEX(SI), SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, FN_TYPE_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_TYPE_TABLE(AX), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, SI")
	inst(e, asmamd64.OperationAdd64Bits, "SI, AX")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, AX")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, AX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $22")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(BX), AX")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ss_ptr_store")
	inst(e, asmamd64.OperationMove64Bits, "(AX), AX")
	e.Label("ss_ptr_store")
	inst(e, asmamd64.OperationMove64Bits, "AX, (DI)")
	inst(e, asmamd64.OperationJump, "ss_done")
	e.Label("ss_ptr_nil")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "AX, AX")
	inst(e, asmamd64.OperationJump, "ss_ptr_store")
}

// emitSetStructFieldGeneralCycleBrokenArmAMD64 emits the cycle-broken `any` field store
// arm for AMD64.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldGeneralCycleBrokenArmAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare64Bits, "CX, $LAYOUT_FLAG_CYCLE_BROKEN")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationCompare8Bits, "OFF_LAYOUT_KIND(SI), $20")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(BX*8), BX")
	inst(e, asmamd64.OperationMove64Bits, "16(BX), CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $22")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "0(BX), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "ABI_TYPE_PTR_ELEM(CX), CX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "OFF_LAYOUT_TYPE_INDEX(SI), BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_TYPE_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_TYPE_TABLE(AX), AX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, AX")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, CX")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	emitStructFieldReceiverBaseAMD64(e, "$8", "ssi")
	emitSetStructFieldValueSlotAMD64(e)
	inst(e, asmamd64.OperationMove64Bits, "0(BX), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, "ss_iface_nil")
	inst(e, asmamd64.OperationMove64Bits, "16(BX), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, SI")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, SI")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $18")
	inst(e, asmamd64.OperationJumpIfEqual, "ss_iface_data")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $19")
	inst(e, asmamd64.OperationJumpIfEqual, "ss_iface_data")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $21")
	inst(e, asmamd64.OperationJumpIfEqual, "ss_iface_data")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $22")
	inst(e, asmamd64.OperationJumpIfEqual, "ss_iface_data")
	inst(e, asmamd64.OperationCompare64Bits, "SI, $26")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	e.Label("ss_iface_data")
	inst(e, asmamd64.OperationMove64Bits, "8(BX), BX")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, "ss_iface_store")
	inst(e, asmamd64.OperationMove64Bits, "(BX), BX")
	e.Label("ss_iface_store")
	inst(e, asmamd64.OperationMove64Bits, "AX, (DI)")
	inst(e, asmamd64.OperationMove64Bits, "BX, 8(DI)")
	inst(e, asmamd64.OperationJump, "ss_done")
	e.Label("ss_iface_nil")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "BX, BX")
	inst(e, asmamd64.OperationJump, "ss_iface_store")
}

// emitSetStructFieldValueSlotAMD64 loads the address of general[B] into BX. AX and CX are
// scratch; DX, SI and DI are preserved.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldValueSlotAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(CX*8), BX")
}

// emitSetStructFieldFunctionPointerAMD64 loads the current frame's *CompiledFunction into
// AX. Only AX is written.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitSetStructFieldFunctionPointerAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
}
