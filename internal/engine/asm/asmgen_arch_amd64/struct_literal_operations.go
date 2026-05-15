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

// EmitAllocStructLiteral emits the body of handlerSubOpAllocStructLiteral, carving a
// zeroed struct from the arena's generic-bytes slab and writing the result into
// general[C].
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func (*BytecodeAMD64Arch) EmitAllocStructLiteral(e *asmgen.Emitter) {
	emitAllocStructLiteralLookup(e)
	emitAllocStructLiteralBump(e)
	emitAllocStructLiteralZeroAndStore(e)
}

// emitAllocStructLiteralLookup decodes the destination register and type index, rejects
// an active write barrier and locates the struct-literal table entry.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitAllocStructLiteralLookup(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	inst(e, asmamd64.OperationMove32Bits, "(R12)(R14*4), AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove16To32BitsZeroExtended, "AX, AX")
	e.Blank()

	inst(e, asmamd64.OperationCompare8Bits, "runtime·writeBarrier(SB), $0")
	inst(e, asmamd64.OperationJumpIfNotEqual, labelASLFallback)
	inst(e, asmamd64.OperationMove64Bits, "CTX_CURRENT_FRAME(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "CF_FUNCTION(CX), CX")
	inst(e, asmamd64.OperationMove64Bits, "FN_STRUCT_LITERAL_TABLE(CX), DX")
	inst(e, asmamd64.OperationTest64Bits, "DX, DX")
	inst(e, asmamd64.OperationJumpIfZero, labelASLFallback)
	inst(e, asmamd64.OperationCompare64Bits, "AX, FN_STRUCT_LITERAL_TABLE_LEN(CX)")
	inst(e, asmamd64.OperationJumpIfGreaterOrEqualSigned, labelASLFallback)
	inst(e, asmamd64.OperationMove64Bits, "$SLE_SIZE, CX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "AX, CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, DX")
	inst(e, asmamd64.OperationMove64Bits, "SLE_TYPE_WORD(DX), SI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, labelASLFallback)
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(BX)(BX*2), BX")
	inst(e, asmamd64.OperationShiftLeft64Bits, "$3, BX")
	inst(e, asmamd64.OperationAdd64Bits, "CTX_GENERALS_BASE(R15), BX")
	e.Blank()
}

// emitAllocStructLiteralBump aligns and bumps the arena's generic-bytes cursor through
// the published cursor address, rejects an allocation beyond the slab length, records the
// bytes for GC pacing and marks the arena banks dirty.
//
// Takes e (*asmgen.Emitter) which is the assembly emitter to write to.
func emitAllocStructLiteralBump(e *asmgen.Emitter) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_GENERIC_IDX_PTR(R15), CX")
	inst(e, asmamd64.OperationMove64Bits, "(CX), DI")
	inst(e, asmamd64.OperationMove64Bits, "SLE_ALIGN_MASK(DX), AX")
	inst(e, asmamd64.OperationAdd64Bits, "AX, DI")
	inst(e, asmamd64.OperationBitwiseNot64Bits, "AX")
	inst(e, asmamd64.OperationBitwiseAnd64Bits, "AX, DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, AX")
	inst(e, asmamd64.OperationAdd64Bits, "SLE_BYTES(DX), AX")
	inst(e, asmamd64.OperationMove64Bits, "CTX_ARENA_GENERIC_HDR(R15), SI")
	inst(e, asmamd64.OperationCompare64Bits, "AX, 8(SI)")
	inst(e, asmamd64.OperationJumpIfGreaterSigned, labelASLFallback)
	inst(e, asmamd64.OperationMove64Bits, "AX, (CX)")
	inst(e, asmamd64.OperationMove64Bits, "SLE_BYTES(DX), CX")
	inst(e, asmamd64.OperationAdd64Bits, "CX, CTX_ARENA_GENERIC_CHARGED(R15)")
	inst(e, asmamd64.OperationMove8Bits, "$1, CTX_ARENA_BANKS_DIRTY(R15)")
	inst(e, asmamd64.OperationAdd64Bits, "(SI), DI")
	inst(e, asmamd64.OperationMove64Bits, "DI, AX")
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
	inst(e, asmamd64.OperationCompare64Bits, "CX, $8")
	inst(e, asmamd64.OperationJumpIfLessSigned, labelASLZeroBytes)
	inst(e, asmamd64.OperationMove64Bits, "$0, (AX)")
	inst(e, asmamd64.OperationAdd64Bits, "$8, AX")
	inst(e, asmamd64.OperationSubtract64Bits, "$8, CX")
	inst(e, asmamd64.OperationJump, labelASLZeroWords)
	e.Label(labelASLZeroBytes)
	inst(e, asmamd64.OperationTest64Bits, "CX, CX")
	inst(e, asmamd64.OperationJumpIfZero, labelASLStore)
	inst(e, asmamd64.OperationMove8Bits, "$0, (AX)")
	inst(e, asmamd64.OperationIncrement64Bits, "AX")
	inst(e, asmamd64.OperationDecrement64Bits, "CX")
	inst(e, asmamd64.OperationJump, labelASLZeroBytes)
	e.Blank()

	e.Label(labelASLStore)
	inst(e, asmamd64.OperationMove64Bits, "SLE_TYPE_WORD(DX), SI")
	inst(e, asmamd64.OperationMove64Bits, "SI, (BX)")
	inst(e, asmamd64.OperationMove64Bits, "DI, 8(BX)")
	inst(e, asmamd64.OperationMove64Bits, "SLE_FLAG(DX), AX")
	inst(e, asmamd64.OperationMove64Bits, "AX, 16(BX)")
	inst(e, asmamd64.OperationIncrement64Bits, "R14")
	e.Instruction(macroDispatchNext)
	e.Blank()

	e.Label(labelASLFallback)
	inst(e, asmamd64.OperationDecrement64Bits, "R14")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_PC(R15)")
	inst(e, asmamd64.OperationMove64Bits, "$EXIT_TIER2, CTX_EXIT_REASON(R15)")
	inst(e, asmamd64.OperationMove64Bits, "R14, CTX_EXIT_PC(R15)")
	inst(e, asmamd64.OperationReturn, "")
}
