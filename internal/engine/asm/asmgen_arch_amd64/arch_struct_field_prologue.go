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

// emitStructFieldCommonPrologueAMD64 resolves layout[C] and the receiver into a field
// pointer in DI, branching to sf_shim on any unsupported shape.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate.
// Takes recvShift (string) which selects the receiver operand byte ("$8" A or "$16" B).
func emitStructFieldCommonPrologueAMD64(e *asmgen.Emitter, kindA, kindB, recvShift string) {
	emitStructFieldLayoutRowAMD64(e)
	emitStructFieldLayoutGateAMD64(e, kindA, kindB)
	emitStructFieldReceiverBaseAMD64(e, recvShift, "sf")
}

// emitStructFieldLayoutRowAMD64 loads the layout row named by operand C into SI,
// branching to sf_shim when the index is out of range.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitStructFieldLayoutRowAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_FRAME_POINTER(R15), AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "$CALLFRAME_SIZE, AX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_CSTACK_BASE(R15), AX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(AX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, FN_STRUCT_LAYOUT_TABLE_LEN(AX)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "FN_STRUCT_LAYOUT_TABLE(AX), SI")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$4, BX")
	inst(e, asmamd64.OperationAdd64Bits, "BX, SI")
}

// emitStructFieldLayoutGateAMD64 branches to sf_shim unless the layout row in SI has zero
// flags and one of the accepted kinds. CX is scratch; DX and SI are preserved.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kindA (string) which is the primary accepted layout Kind byte immediate.
// Takes kindB (string) which is the secondary accepted layout Kind byte immediate; may be
// empty.
func emitStructFieldLayoutGateAMD64(e *asmgen.Emitter, kindA, kindB string) {
	inst(e, asmamd64.OperationCompare8Bits, "OFF_LAYOUT_FLAGS(SI), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "OFF_LAYOUT_KIND(SI), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindA)
	if kindB != "" {
		inst(e, asmamd64.OperationJumpIfEqual, "sf_kind_ok")
		inst(e, asmamd64.OperationCompare64Bits, "CX, "+kindB)
	}
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	if kindB != "" {
		e.Label("sf_kind_ok")
	}
}

// emitStructFieldReceiverBaseAMD64 resolves the receiver at the given operand shift into
// a field pointer in DI, branching to sf_shim on nil or unsupported receiver shapes.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes recvShift (string) which selects the receiver operand byte ("$8" A or "$16" B).
// Takes labelPrefix (string) which prefixes the helper's local labels so each call site
// gets distinct labels.
func emitStructFieldReceiverBaseAMD64(e *asmgen.Emitter, recvShift, labelPrefix string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, recvShift+", AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), CX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(CX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), CX")
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, BX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$0x1F, BX")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $22")
	inst(e, asmamd64.OperationJumpIfEqual, labelPrefix+"_ptr_recv")
	inst(e, asmamd64.OperationCompare64Bits, "BX, $25")
	inst(e, asmamd64.OperationJumpIfNotEqual, "sf_shim")
	inst(e, asmamd64.OperationTest64Bits, "$0x100, CX")
	inst(e, asmamd64.OperationJumpIfZero, "sf_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationJump, labelPrefix+"_have_base")
	e.Label(labelPrefix + "_ptr_recv")
	inst(e, asmamd64.OperationMove64Bits, "8(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "$0x80, CX")
	inst(e, asmamd64.OperationJumpIfZero, labelPrefix+"_have_base")
	inst(e, asmamd64.OperationMove64Bits, "(DI), DI")
	e.Label(labelPrefix + "_have_base")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "sf_shim")
	inst(e, asmamd64.OperationMove32To64BitsZeroExtended, "OFF_LAYOUT_OFFSET(SI), CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, DI")
}
