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

const (
	// labelASLFallback names the struct-literal allocation handler's exit to the Go path.
	labelASLFallback = "asl_fallback"

	// labelASLZeroWords names the word-zeroing loop of the struct-literal allocation
	// handler.
	labelASLZeroWords = "asl_zero_words"

	// labelASLZeroBytes names the byte-zeroing loop of the struct-literal allocation
	// handler.
	labelASLZeroBytes = "asl_zero_bytes"

	// labelASLStore names the general-bank store of the struct-literal allocation handler.
	labelASLStore = "asl_store"
)

// EmitAllocStructLiteral emits the arm64 body of handlerSubOpAllocStructLiteral,
// allocating a zeroed struct from the arena's generic-bytes slab.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeARM64Arch) EmitAllocStructLiteral(e *asmgen.Emitter) {
	emitAllocStructLiteralLookup(e)
	emitAllocStructLiteralBump(e)
	emitAllocStructLiteralZeroAndStore(e)
}

// emitAllocStructLiteralLookup decodes the destination register and type index, rejects
// an active write barrier, locates the struct-literal table entry through the current
// frame's function and rejects a non-qualifying type, leaving the entry and destination
// slot in registers for the allocation stage.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitAllocStructLiteralLookup(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R1")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R1, R1")
	inst5(e, asmarm64.OperationMove32BitsUnsigned, "(R22)(R20<<2), R2")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R2, R2")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFFFF, R2, R2")
	e.Blank()

	inst5(e, asmarm64.OperationMove64Bits, "$runtime·writeBarrier(SB), R3")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "(R3), R3")
	inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, "+labelASLFallback)
	inst5(e, asmarm64.OperationMove64Bits, "CTX_CURRENT_FRAME(R19), R3")
	inst5(e, asmarm64.OperationMove64Bits, "CF_FUNCTION(R3), R3")
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LITERAL_TABLE(R3), R4")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R4, "+labelASLFallback)
	inst5(e, asmarm64.OperationMove64Bits, "FN_STRUCT_LITERAL_TABLE_LEN(R3), R5")
	inst5(e, asmarm64.OperationCompare, "R5, R2")
	inst5(e, asmarm64.OperationBranchIfGreaterOrEqualSigned, labelASLFallback)
	inst5(e, asmarm64.OperationMove64Bits, "$SLE_SIZE, R5")
	inst5(e, asmarm64.OperationMultiply, "R2, R5, R5")
	inst5(e, asmarm64.OperationAdd, "R5, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "SLE_TYPE_WORD(R4), R6")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R6, "+labelASLFallback)
	e.Blank()
}

// emitAllocStructLiteralBump aligns and bumps the arena's generic-bytes cursor through
// the published cursor address, rejects an allocation beyond the slab length, records the
// bytes for GC pacing and marks the arena banks dirty.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitAllocStructLiteralBump(e *asmgen.Emitter) {
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERIC_IDX_PTR(R19), R10")
	inst5(e, asmarm64.OperationMove64Bits, "(R10), R7")
	inst5(e, asmarm64.OperationMove64Bits, "SLE_ALIGN_MASK(R4), R5")
	inst5(e, asmarm64.OperationAdd, "R5, R7, R7")
	inst5(e, asmarm64.OperationBitwiseAndNot, "R5, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "SLE_BYTES(R4), R9")
	inst5(e, asmarm64.OperationAdd, "R9, R7, R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERIC_HDR(R19), R11")
	inst5(e, asmarm64.OperationMove64Bits, "8(R11), R5")
	inst5(e, asmarm64.OperationCompare, "R5, R3")
	inst5(e, asmarm64.OperationBranchIfGreaterSigned, labelASLFallback)
	inst5(e, asmarm64.OperationMove64Bits, "R3, (R10)")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_ARENA_GENERIC_CHARGED(R19), R5")
	inst5(e, asmarm64.OperationAdd, "R9, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "R5, CTX_ARENA_GENERIC_CHARGED(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "$1, R5")
	inst5(e, asmarm64.OperationMove8Bits, "R5, CTX_ARENA_BANKS_DIRTY(R19)")
	inst5(e, asmarm64.OperationMove64Bits, "(R11), R5")
	inst5(e, asmarm64.OperationAdd, "R5, R7, R7")
	inst5(e, asmarm64.OperationMove64Bits, "R7, R3")
	e.Blank()
}

// emitAllocStructLiteralZeroAndStore zeroes the allocation eight bytes at a time with a
// byte tail, writes the reflect.Value triple into the destination slot, consumes the
// extension word and dispatches; the fallback exits to Go with the program counter
// rewound to the opcode word.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitAllocStructLiteralZeroAndStore(e *asmgen.Emitter) {
	e.Label(labelASLZeroWords)
	inst5(e, asmarm64.OperationCompare, "$8, R9")
	inst5(e, asmarm64.OperationBranchIfLessSigned, labelASLZeroBytes)
	inst5(e, asmarm64.OperationMove64Bits, "ZR, (R3)")
	inst5(e, asmarm64.OperationAdd, "$8, R3, R3")
	inst5(e, asmarm64.OperationSubtract, "$8, R9, R9")
	inst5(e, asmarm64.OperationBranch, labelASLZeroWords)
	e.Label(labelASLZeroBytes)
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, "+labelASLStore)
	inst5(e, asmarm64.OperationMove8Bits, "ZR, (R3)")
	inst5(e, asmarm64.OperationAdd, "$1, R3, R3")
	inst5(e, asmarm64.OperationSubtract, "$1, R9, R9")
	inst5(e, asmarm64.OperationBranch, labelASLZeroBytes)
	e.Blank()

	e.Label(labelASLStore)
	inst5(e, asmarm64.OperationMove64Bits, "$24, R5")
	inst5(e, asmarm64.OperationMultiply, "R1, R5, R5")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R3")
	inst5(e, asmarm64.OperationAdd, "R5, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "R6, (R3)")
	inst5(e, asmarm64.OperationMove64Bits, "R7, 8(R3)")
	inst5(e, asmarm64.OperationMove64Bits, "SLE_FLAG(R4), R5")
	inst5(e, asmarm64.OperationMove64Bits, "R5, 16(R3)")
	inst5(e, asmarm64.OperationAdd, "$1, R20, R20")
	e.Instruction(macroDispatchNext)
	e.Blank()

	e.Label(labelASLFallback)
	inst5(e, asmarm64.OperationBranch, "·pathBFallback(SB)")
}
