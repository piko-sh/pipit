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

// EmitUintConstArith emits uint arithmetic with an extension-word constant.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes operation (string) which selects the uint arithmetic operation.
func (*BytecodeARM64Arch) EmitUintConstArith(e *asmgen.Emitter, operation string) {
	mnemonic, narrows := uintConstantOperationARM64(operation)
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
	inst5(e, mnemonic, "R5, R7, R7")
	if narrows {
		inst5(e, asmarm64.OperationMove32BitsUnsigned, "R7, R7")
	}
	inst5(e, asmarm64.OperationMove64Bits, "R7, (R6)(R3<<3)")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
}

// uintConstantOperationARM64 resolves an operation of EmitUintConstArith.
//
// Takes operation (string) which is "add", "sub" or "and", or "add32" or "sub32" for the
// forms that keep the low 32 bits of the result.
//
// Returns mnemonic (string) which is the arithmetic instruction.
// Returns narrows (bool) which is true when the result is kept to 32 bits.
//
// Panics when the operation is any other.
func uintConstantOperationARM64(operation string) (mnemonic string, narrows bool) {
	switch operation {
	case "add":
		return asmarm64.OperationAdd, false
	case "sub":
		return asmarm64.OperationSubtract, false
	case "and":
		return asmarm64.OperationBitwiseAnd, false
	case "add32":
		return asmarm64.OperationAdd, true
	case "sub32":
		return asmarm64.OperationSubtract, true
	default:
		panic("EmitUintConstArith: unknown operation " + operation)
	}
}
