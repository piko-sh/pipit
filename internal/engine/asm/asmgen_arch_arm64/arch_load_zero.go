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

// EmitLoadZero implements BytecodeArchitecturePort.
//
// R0 holds the instruction word, whose byte 2 is the register and byte 3 the bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for the banks not zeroed here.
func (*BytecodeARM64Arch) EmitLoadZero(e *asmgen.Emitter, shimSymbol string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R6, R6")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_INT, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "zero_not_int")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R23)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "zero_done")
	e.Label("zero_not_int")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_UINT, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "zero_not_uint")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R4)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "zero_done")
	e.Label("zero_not_uint")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_FLOAT, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "zero_not_float")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R24)(R3<<3)")
	inst5(e, asmarm64.OperationBranch, "zero_done")
	e.Label("zero_not_float")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_BOOL, R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "zero_shim")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R4")
	inst5(e, asmarm64.OperationMove8Bits, "ZR, (R4)(R3)")
	e.Label("zero_done")
	e.Instruction(macroDispatchNext)
	e.Label("zero_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}
