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

// EmitLoadZero implements BytecodeArchitecturePort.
//
// DX holds the instruction word, whose byte 2 is the register and byte 3 the bank.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for the banks not zeroed here.
func (*BytecodeAMD64Arch) EmitLoadZero(e *asmgen.Emitter, shimSymbol string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_INT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "zero_not_int")
	inst(e, asmamd64.OperationMove64Bits, "$0, (R8)(AX*8)")
	inst(e, asmamd64.OperationJump, "zero_done")
	e.Label("zero_not_int")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_UINT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "zero_not_uint")
	inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "$0, (DI)(AX*8)")
	inst(e, asmamd64.OperationJump, "zero_done")
	e.Label("zero_not_uint")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_FLOAT")
	inst(e, asmamd64.OperationJumpIfNotEqual, "zero_not_float")
	inst(e, asmamd64.OperationMove64Bits, "$0, (R9)(AX*8)")
	inst(e, asmamd64.OperationJump, "zero_done")
	e.Label("zero_not_float")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_BOOL")
	inst(e, asmamd64.OperationJumpIfNotEqual, "zero_shim")
	inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), DI")
	inst(e, asmamd64.OperationMove8Bits, "$0, (DI)(AX*1)")
	e.Label("zero_done")
	e.Instruction(macroDispatchNext)
	e.Label("zero_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}
