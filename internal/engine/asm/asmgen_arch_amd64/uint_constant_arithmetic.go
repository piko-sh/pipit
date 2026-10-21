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

// EmitUintConstArith emits uint arithmetic with an extension-word constant.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
// Takes operation (string) which selects the uint arithmetic operation.
func (*BytecodeAMD64Arch) EmitUintConstArith(e *asmgen.Emitter, operation string) {
	mnemonic, narrows := uintConstantOperationAMD64(operation)
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
	inst(e, mnemonic, "CX, DI")
	if narrows {
		inst(e, asmamd64.OperationMove32Bits, "DI, DI")
	}
	inst(e, asmamd64.OperationMove64Bits, "DI, (SI)(AX*8)")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
}

// uintConstantOperationAMD64 resolves an operation of EmitUintConstArith.
//
// Takes operation (string) which is "add", "sub" or "and", or "add32" or "sub32" for the
// forms that keep the low 32 bits of the result.
//
// Returns mnemonic (string) which is the arithmetic instruction.
// Returns narrows (bool) which is true when the result is kept to 32 bits.
//
// Panics when the operation is any other.
func uintConstantOperationAMD64(operation string) (mnemonic string, narrows bool) {
	switch operation {
	case "add":
		return asmamd64.OperationAdd64Bits, false
	case "sub":
		return asmamd64.OperationSubtract64Bits, false
	case "and":
		return asmamd64.OperationBitwiseAnd64Bits, false
	case "add32":
		return asmamd64.OperationAdd64Bits, true
	case "sub32":
		return asmamd64.OperationSubtract64Bits, true
	default:
		panic("EmitUintConstArith: unknown operation " + operation)
	}
}
