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

// EmitLengthGeneral emits a length read from a general-bank slice.
//
// DX holds the instruction word, whose byte 3 is the general source register and byte 2
// the int destination. A reflect.Value of slice kind always holds a pointer to its slice
// header, with the indirection flag set. Anything else leaves by the shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeAMD64Arch) EmitLengthGeneral(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(CX)(CX*2), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(AX*8), SI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, "length_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), BX")
	inst(e, asmamd64.OperationMove64Bits, "BX, CX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "$FLAG_KIND_MASK, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REFLECT_SLICE")
	inst(e, asmamd64.OperationJumpIfNotEqual, "length_shim")
	inst(e, asmamd64.OperationTest64Bits, "$FLAG_INDIR, BX")
	inst(e, asmamd64.OperationJumpIfZero, "length_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), AX")
	inst(e, asmamd64.OperationTest64Bits, "AX, AX")
	inst(e, asmamd64.OperationJumpIfZero, "length_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(AX), CX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	e.Instruction(macroDispatchNext)
	e.Label("length_shim")
	inst(e, asmamd64.OperationJump, "·handlerPathBShimLengthGeneral(SB)")
}
