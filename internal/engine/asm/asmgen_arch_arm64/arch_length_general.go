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

// EmitLengthGeneral emits a length read from a general-bank slice.
//
// R0 holds the instruction word, whose byte 3 is the general source register and byte 2
// the int destination. A reflect.Value of slice kind always holds a pointer to its slice
// header, with the indirection flag set. Anything else leaves by the shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func (*BytecodeARM64Arch) EmitLengthGeneral(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R4")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R6")
	inst5(e, asmarm64.OperationAdd, "R4, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "0(R6), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, length_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R6), R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_KIND_MASK, R7, R8")
	inst5(e, asmarm64.OperationCompare, "$REFLECT_SLICE, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "length_shim")
	inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_INDIR, R7, R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, length_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, length_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R6), R5")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R5, (R23)(R3<<3)")
	e.Instruction(macroDispatchNext)
	e.Label("length_shim")
	inst5(e, asmarm64.OperationBranch, "·handlerPathBShimLengthGeneral(SB)")
}
