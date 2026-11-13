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

// EmitGetGlobal implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitGetGlobal(e *asmgen.Emitter, shimSymbol string) {
	emitGetGlobalAMD64(e, shimSymbol, false)
}

// EmitGetGlobalWide implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitGetGlobalWide(e *asmgen.Emitter, shimSymbol string) {
	emitGetGlobalAMD64(e, shimSymbol, true)
}

// EmitSetGlobal implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitSetGlobal(e *asmgen.Emitter, shimSymbol string) {
	emitSetGlobalAMD64(e, shimSymbol, false)
}

// EmitSetGlobalWide implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitSetGlobalWide(e *asmgen.Emitter, shimSymbol string) {
	emitSetGlobalAMD64(e, shimSymbol, true)
}

// emitSetGlobalAMD64 emits the scalar store body shared by EmitSetGlobal and
// EmitSetGlobalWide.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
// Takes wide (bool) which is true for the form whose index is in an extension word, which
// is stepped over once the store is made.
func emitSetGlobalAMD64(e *asmgen.Emitter, shimSymbol string, wide bool) {
	emitGlobalSlotAMD64(e, wide)
	emitGlobalScalarAMD64(e, "REGKIND_INT", "GS_INTS", "global_not_int", func() {
		inst(e, asmamd64.OperationMove64Bits, "(R8)(AX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(BX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_UINT", "GS_UINTS", "global_not_uint", func() {
		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), CX")
		inst(e, asmamd64.OperationMove64Bits, "(CX)(AX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(BX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_FLOAT", "GS_FLOATS", "global_not_float", func() {
		inst(e, asmamd64.OperationMove64Bits, "(R9)(AX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(BX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_BOOL", "GS_BOOLS", "global_shim", func() {
		inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), CX")
		inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(CX)(AX*1), CX")
		inst(e, asmamd64.OperationMove8Bits, "CL, (DI)(BX*1)")
	})
	e.Label("global_done")
	if wide {
		inst(e, asmamd64.OperationIncrement64Bits, "R14")
	}
	e.Instruction(macroDispatchNext)
	e.Label("global_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// emitGetGlobalAMD64 emits the read body shared by EmitGetGlobal and EmitGetGlobalWide.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
// Takes wide (bool) which is true for the form whose index is in an extension word, which
// is stepped over once the read is made.
func emitGetGlobalAMD64(e *asmgen.Emitter, shimSymbol string, wide bool) {
	emitGlobalSlotAMD64(e, wide)
	emitGlobalScalarAMD64(e, "REGKIND_INT", "GS_INTS", "global_not_int", func() {
		inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_UINT", "GS_UINTS", "global_not_uint", func() {
		inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(AX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_FLOAT", "GS_FLOATS", "global_not_float", func() {
		inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), CX")
		inst(e, asmamd64.OperationMove64Bits, "CX, (R9)(AX*8)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_BOOL", "GS_BOOLS", "global_not_bool", func() {
		inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "(DI)(BX*1), CX")
		inst(e, asmamd64.OperationMove64Bits, "CTX_BOOLS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove8Bits, "CL, (DI)(AX*1)")
	})
	emitGlobalScalarAMD64(e, "REGKIND_GENERAL", "GS_GENERAL", "global_shim", func() { emitGlobalGeneralCopyAMD64(e) })
	e.Label("global_done")
	if wide {
		inst(e, asmamd64.OperationIncrement64Bits, "R14")
	}
	e.Instruction(macroDispatchNext)
	e.Label("global_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// emitGlobalScalarAMD64 emits a bank's kind test and bounds check for slot BX, leaving
// the bank's data in DI.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kind (string) which is the REGKIND_* define of the bank.
// Takes bankOffset (string) which is the GS_* define of the bank's slice.
// Takes next (string) which is the label of the following arm.
// Takes load (func()) which emits the copy between the bank and the register.
func emitGlobalScalarAMD64(e *asmgen.Emitter, kind, bankOffset, next string, load func()) {
	inst(e, asmamd64.OperationCompare64Bits, "CX, $"+kind)
	inst(e, asmamd64.OperationJumpIfNotEqual, next)
	inst(e, asmamd64.OperationCompare64Bits, "BX, ("+bankOffset+"+8)(SI)")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "global_shim")
	inst(e, asmamd64.OperationMove64Bits, bankOffset+"(SI), DI")
	load()
	inst(e, asmamd64.OperationJump, "global_done")
	if next != "global_shim" {
		e.Label(next)
	}
}

// emitGlobalSlotAMD64 emits the prologue of a global read or store.
//
// It leaves the unshared store in SI, the kind in CX, the slot (the index plus the
// function's global base for the kind) in BX and the register in AX, or jumps to
// global_shim for a missing or shared store.
//
// The index is byte B of the instruction and the destination byte A, or for the wide form
// the low 16 bits of the extension word and byte B.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes wide (bool) which is true for the form whose index is in an extension word.
func emitGlobalSlotAMD64(e *asmgen.Emitter, wide bool) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_GLOBALS(R15), SI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "global_shim")
	inst(e, asmamd64.OperationCompare8Bits, "GS_SHARED(SI), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "global_shim")
	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationBitwiseXor64Bits, "AX, AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_CURRENT_FRAME(R15), DI")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "global_slot")
	inst(e, asmamd64.OperationMove64Bits, "FN_GLOBAL_BASES(DI), DI")
	inst(e, asmamd64.OperationTest64Bits, "DI, DI")
	inst(e, asmamd64.OperationJumpIfZero, "global_slot")
	inst(e, asmamd64.OperationCompare64Bits, "CX, $REGKIND_UINT")
	inst(e, asmamd64.OperationJumpIfAbove, "global_shim")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "(DI)(CX*2), AX")
	e.Label("global_slot")
	if wide {
		inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), BX")
		inst(e, asmamd64.OperationShiftRight64Bits, "$8, BX")
		inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "BX, BX")
		inst(e, asmamd64.OperationAdd64Bits, "AX, BX")
		inst(e, asmamd64.OperationMove64Bits, "DX, AX")
		inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
		inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
		return
	}
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, BX")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
}

// emitGlobalGeneralCopyAMD64 copies general slot BX of the bank at DI into general
// register AX, three words of which two are pointers, while the write barrier is off.
// With it on, it jumps to global_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitGlobalGeneralCopyAMD64(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, "global_shim")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), BX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(BX*8), SI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(AX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 0(DI)")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 8(DI)")
	inst(e, asmamd64.OperationMove64Bits, "16(SI), CX")
	inst(e, asmamd64.OperationMove64Bits, "CX, 16(DI)")
}
