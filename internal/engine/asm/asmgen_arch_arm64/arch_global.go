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

// EmitGetGlobal implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitGetGlobal(e *asmgen.Emitter, shimSymbol string) {
	emitGetGlobalARM64(e, shimSymbol, false)
}

// EmitGetGlobalWide implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitGetGlobalWide(e *asmgen.Emitter, shimSymbol string) {
	emitGetGlobalARM64(e, shimSymbol, true)
}

// EmitSetGlobal implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitSetGlobal(e *asmgen.Emitter, shimSymbol string) {
	emitSetGlobalARM64(e, shimSymbol, false)
}

// EmitSetGlobalWide implements BytecodeArchitecturePort.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitSetGlobalWide(e *asmgen.Emitter, shimSymbol string) {
	emitSetGlobalARM64(e, shimSymbol, true)
}

// emitSetGlobalARM64 emits the scalar store body shared by EmitSetGlobal and
// EmitSetGlobalWide.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
// Takes wide (bool) which is true for the form whose index is in an extension word, which
// is stepped over once the store is made.
func emitSetGlobalARM64(e *asmgen.Emitter, shimSymbol string, wide bool) {
	emitGlobalSlotARM64(e, wide)
	emitGlobalScalarARM64(e, "REGKIND_INT", "GS_INTS", "global_not_int", func() {
		inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R8)(R7<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_UINT", "GS_UINTS", "global_not_uint", func() {
		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove64Bits, "(R4)(R3<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R8)(R7<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_FLOAT", "GS_FLOATS", "global_not_float", func() {
		inst5(e, asmarm64.OperationMove64Bits, "(R24)(R3<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R8)(R7<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_BOOL", "GS_BOOLS", "global_shim", func() {
		inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R4)(R3), R9")
		inst5(e, asmarm64.OperationMove8Bits, "R9, (R8)(R7)")
	})
	e.Label("global_done")
	if wide {
		inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	}
	e.Instruction(macroDispatchNext)
	e.Label("global_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// emitGetGlobalARM64 emits the read body shared by EmitGetGlobal and EmitGetGlobalWide.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
// Takes wide (bool) which is true for the form whose index is in an extension word, which
// is stepped over once the read is made.
func emitGetGlobalARM64(e *asmgen.Emitter, shimSymbol string, wide bool) {
	emitGlobalSlotARM64(e, wide)
	emitGlobalScalarARM64(e, "REGKIND_INT", "GS_INTS", "global_not_int", func() {
		inst5(e, asmarm64.OperationMove64Bits, "(R8)(R7<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R23)(R3<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_UINT", "GS_UINTS", "global_not_uint", func() {
		inst5(e, asmarm64.OperationMove64Bits, "(R8)(R7<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R4)(R3<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_FLOAT", "GS_FLOATS", "global_not_float", func() {
		inst5(e, asmarm64.OperationMove64Bits, "(R8)(R7<<3), R9")
		inst5(e, asmarm64.OperationMove64Bits, "R9, (R24)(R3<<3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_BOOL", "GS_BOOLS", "global_not_bool", func() {
		inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R8)(R7), R9")
		inst5(e, asmarm64.OperationMove64Bits, "CTX_BOOLS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove8Bits, "R9, (R4)(R3)")
	})
	emitGlobalScalarARM64(e, "REGKIND_GENERAL", "GS_GENERAL", "global_shim", func() { emitGlobalGeneralCopyARM64(e) })
	e.Label("global_done")
	if wide {
		inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	}
	e.Instruction(macroDispatchNext)
	e.Label("global_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// emitGlobalScalarARM64 emits a bank's kind test and bounds check for slot R7, leaving
// the bank's data in R8.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes kind (string) which is the REGKIND_* define of the bank.
// Takes bankOffset (string) which is the GS_* define of the bank's slice.
// Takes next (string) which is the label of the following arm.
// Takes load (func()) which emits the copy between the bank and the register.
func emitGlobalScalarARM64(e *asmgen.Emitter, kind, bankOffset, next string, load func()) {
	inst5(e, asmarm64.OperationCompare, "$"+kind+", R6")
	inst5(e, asmarm64.OperationBranchIfNotEqual, next)
	inst5(e, asmarm64.OperationMove64Bits, "("+bankOffset+"+8)(R5), R9")
	inst5(e, asmarm64.OperationCompare, "R9, R7")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "global_shim")
	inst5(e, asmarm64.OperationMove64Bits, bankOffset+"(R5), R8")
	load()
	inst5(e, asmarm64.OperationBranch, "global_done")
	if next != "global_shim" {
		e.Label(next)
	}
}

// emitGlobalSlotARM64 emits the prologue of a global read or store.
//
// It leaves the unshared store in R5, the kind in R6, the slot (the index plus the
// function's global base for the kind) in R7 and the register in R3, or branches to
// global_shim for a missing or shared store.
//
// The index is byte B of the instruction and the destination byte A, or for the wide form
// the low 16 bits of the extension word and byte B.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes wide (bool) which is true for the form whose index is in an extension word.
func emitGlobalSlotARM64(e *asmgen.Emitter, wide bool) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GLOBALS(R19), R5")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R5, global_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "GS_SHARED(R5), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R4, global_shim")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R6")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "ZR, R8")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CURRENT_FRAME(R19), R4")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R4), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R4, global_slot")
	inst5(e, asmarm64.OperationMove64Bits, "FN_GLOBAL_BASES(R4), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R4, global_slot")
	inst5(e, asmarm64.OperationCompare, "$REGKIND_UINT, R6")
	inst5(e, asmarm64.OperationBranchIfHigher, "global_shim")
	inst5(e, asmarm64.OperationMove16BitsUnsigned, "(R4)(R6<<1), R8")
	e.Label("global_slot")
	if wide {
		inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R7")
		inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R7, R7")
		inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R7, R7")
		inst5(e, asmarm64.OperationAdd, "R8, R7, R7")
		inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
		inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
		return
	}
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R7")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R7, R7")
	inst5(e, asmarm64.OperationAdd, "R8, R7, R7")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
}

// emitGlobalGeneralCopyARM64 copies general slot R7 of the bank at R8 into general
// register R3, three words of which two are pointers, while the write barrier is off.
// With it on, it branches to global_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
func emitGlobalGeneralCopyARM64(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R4")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R4), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R4, global_shim")
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R7, R7")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R3")
	inst5(e, asmarm64.OperationAdd, "R7, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R4")
	inst5(e, asmarm64.OperationAdd, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "0(R8), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 0(R4)")
	inst5(e, asmarm64.OperationMove64Bits, "8(R8), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 8(R4)")
	inst5(e, asmarm64.OperationMove64Bits, "16(R8), R9")
	inst5(e, asmarm64.OperationMove64Bits, "R9, 16(R4)")
}
