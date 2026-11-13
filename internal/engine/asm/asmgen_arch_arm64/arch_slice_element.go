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
	"strconv"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmarm64"
)

// elementWidthCount is the number of supported integer element widths.
const elementWidthCount = 4

// elementMoveARM64 is the instruction that moves an element of one width.
type elementMoveARM64 struct {
	// mnemonic is the move.
	mnemonic string

	// operands are its operands.
	operands string
}

var (
	// signedElementLoadsARM64 are the 1-, 2-, 4- and 8-byte loads of a signed integer
	// element, sign-extending, of element R8 of the data at R5 into R8.
	signedElementLoadsARM64 = [elementWidthCount]elementMoveARM64{
		{mnemonic: asmarm64.OperationMove8Bits, operands: "(R5)(R8), R8"},
		{mnemonic: asmarm64.OperationMove16Bits, operands: "(R5)(R8<<1), R8"},
		{mnemonic: asmarm64.OperationMove32Bits, operands: "(R5)(R8<<2), R8"},
		{mnemonic: asmarm64.OperationMove64Bits, operands: "(R5)(R8<<3), R8"},
	}

	// unsignedElementLoadsARM64 are the 1-, 2-, 4- and 8-byte loads of an unsigned integer
	// element, zero-extending, of element R8 of the data at R5 into R8.
	unsignedElementLoadsARM64 = [elementWidthCount]elementMoveARM64{
		{mnemonic: asmarm64.OperationMove8BitsUnsigned, operands: "(R5)(R8), R8"},
		{mnemonic: asmarm64.OperationMove16BitsUnsigned, operands: "(R5)(R8<<1), R8"},
		{mnemonic: asmarm64.OperationMove32BitsUnsigned, operands: "(R5)(R8<<2), R8"},
		{mnemonic: asmarm64.OperationMove64Bits, operands: "(R5)(R8<<3), R8"},
	}

	// elementStoresARM64 are the 1-, 2-, 4- and 8-byte stores of R3, truncated, into element
	// R8 of the data at R5.
	elementStoresARM64 = [elementWidthCount]elementMoveARM64{
		{mnemonic: asmarm64.OperationMove8Bits, operands: "R3, (R5)(R8)"},
		{mnemonic: asmarm64.OperationMove16Bits, operands: "R3, (R5)(R8<<1)"},
		{mnemonic: asmarm64.OperationMove32Bits, operands: "R3, (R5)(R8<<2)"},
		{mnemonic: asmarm64.OperationMove64Bits, operands: "R3, (R5)(R8<<3)"},
	}
)

// EmitSliceGetTypedElement emits an integer-element read.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which selects OpGetElementInt64 (any integer element into ints[A])
// over OpGetElementUint64 (an unsigned element into uints[A]).
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitSliceGetTypedElement(e *asmgen.Emitter, signed bool, shimSymbol string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	emitTypedElementShapeARM64(e, signed)
	e.Blank()

	emitTypedElementDataARM64(e, false)
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R8")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R8<<3), R8")
	inst5(e, asmarm64.OperationCompare, "R6, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "TES_ELEMENT_SIZE(R7), R6")
	if signed {
		inst5(e, asmarm64.OperationMove8BitsUnsigned, "TES_ELEMENT_KIND(R7), R9")
		inst5(e, asmarm64.OperationCompare, "$REFLECT_INT64, R9")
		inst5(e, asmarm64.OperationBranchIfHigher, "element_unsigned")
		emitElementMoveByWidthARM64(e, "element_signed", "element_result", signedElementLoadsARM64)
		inst5(e, asmarm64.OperationBranch, "element_result")
	}
	emitElementMoveByWidthARM64(e, "element_unsigned", "element_result", unsignedElementLoadsARM64)
	e.Label("element_result")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	if signed {
		inst5(e, asmarm64.OperationMove64Bits, "R8, (R23)(R3<<3)")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove64Bits, "R8, (R4)(R3<<3)")
	}
	e.Instruction(macroDispatchNext)
	e.Label("element_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// EmitSliceSetTypedElement emits an integer-element store.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which selects OpSetElementInt64 (ints[C] into any integer element)
// over OpSetElementUint64 (uints[C] into an unsigned element).
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeARM64Arch) EmitSliceSetTypedElement(e *asmgen.Emitter, signed bool, shimSymbol string) {
	inst5(e, asmarm64.OperationLogicalShiftRight, "$8, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	emitTypedElementShapeARM64(e, signed)
	e.Blank()

	emitTypedElementDataARM64(e, true)
	e.Blank()

	inst5(e, asmarm64.OperationLogicalShiftRight, "$16, R0, R8")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R8, R8")
	inst5(e, asmarm64.OperationMove64Bits, "(R23)(R8<<3), R8")
	inst5(e, asmarm64.OperationCompare, "R6, R8")
	inst5(e, asmarm64.OperationBranchIfHigherOrSame, "element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "TES_ELEMENT_SIZE(R7), R6")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$24, R0, R3")
	inst5(e, asmarm64.OperationBitwiseAnd, "$0xFF, R3, R3")
	if signed {
		inst5(e, asmarm64.OperationMove64Bits, "(R23)(R3<<3), R3")
	} else {
		inst5(e, asmarm64.OperationMove64Bits, "CTX_UINTS_BASE(R19), R4")
		inst5(e, asmarm64.OperationMove64Bits, "(R4)(R3<<3), R3")
	}
	emitElementMoveByWidthARM64(e, "store", "store_done", elementStoresARM64)
	e.Label("store_done")
	e.Instruction(macroDispatchNext)
	e.Label("element_shim")
	inst5(e, asmarm64.OperationBranch, "·"+shimSymbol+"(SB)")
}

// emitTypedElementShapeARM64 resolves the element shape for the general register in R3.
//
// On success R4 addresses the register's slot and R7 holds its shape. Other cases branch
// to element_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which admits every integer element kind. Otherwise only the
// unsigned ones are admitted.
func emitTypedElementShapeARM64(e *asmgen.Emitter, signed bool) {
	inst5(e, asmarm64.OperationMove64Bits, "$24, R4")
	inst5(e, asmarm64.OperationMultiply, "R4, R3, R3")
	inst5(e, asmarm64.OperationMove64Bits, "CTX_GENERALS_BASE(R19), R4")
	inst5(e, asmarm64.OperationAdd, "R3, R4, R4")
	inst5(e, asmarm64.OperationMove64Bits, "0(R4), R5")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R5, element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "$TES_HASH_MULTIPLIER, R6")
	inst5(e, asmarm64.OperationMultiply, "R6, R5, R6")
	inst5(e, asmarm64.OperationLogicalShiftRight, "$TES_SLOT_SHIFT, R6, R6")
	inst5(e, asmarm64.OperationMove64Bits, "$·typedElementShapes(SB), R7")
	inst5(e, asmarm64.OperationMove64Bits, "(R7)(R6<<3), R7")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R7, element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "TES_COLLECTION_TYPE(R7), R8")
	inst5(e, asmarm64.OperationCompare, "R8, R5")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_shim")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "TES_ELEMENT_KIND(R7), R8")
	if signed {
		inst5(e, asmarm64.OperationSubtract, "$REFLECT_INT, R8, R8")
		inst5(e, asmarm64.OperationCompare, "$(REFLECT_UINTPTR-REFLECT_INT), R8")
	} else {
		inst5(e, asmarm64.OperationSubtract, "$REFLECT_UINT, R8, R8")
		inst5(e, asmarm64.OperationCompare, "$(REFLECT_UINTPTR-REFLECT_UINT), R8")
	}
	inst5(e, asmarm64.OperationBranchIfHigher, "element_shim")
}

// emitTypedElementDataARM64 locates element data in R5 and the collection length in R6.
//
// Unsupported storage branches to element_shim. Writes also decline read-only values and
// arrays that cannot be addressed.
//
// On entry R4 addresses the general slot and R7 holds its shape. R3 is used for a write.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes write (bool) which is true when the element is about to be written.
func emitTypedElementDataARM64(e *asmgen.Emitter, write bool) {
	inst5(e, asmarm64.OperationMove64Bits, "8(R4), R5")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R5, element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "16(R4), R9")
	if write {
		inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_RO, R9, R3")
		inst5(e, asmarm64.OperationCompareAndBranchIfNotZero, "R3, element_shim")
		inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_ADDR, R9, R3")
	}
	inst5(e, asmarm64.OperationBitwiseAnd, "$FLAG_INDIR, R9, R9")
	inst5(e, asmarm64.OperationMove8BitsUnsigned, "TES_CONTAINER(R7), R8")
	inst5(e, asmarm64.OperationCompare, "$TES_CONTAINER_SLICE, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_not_slice")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, element_shim")
	inst5(e, asmarm64.OperationMove64Bits, "8(R5), R6")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R5")
	inst5(e, asmarm64.OperationBranch, "element_data")
	e.Label("element_not_slice")
	inst5(e, asmarm64.OperationCompare, "$TES_CONTAINER_ARRAY, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_not_array")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, element_shim")
	if write {
		inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R3, element_shim")
	}
	inst5(e, asmarm64.OperationBranch, "element_array_length")
	e.Label("element_not_array")
	inst5(e, asmarm64.OperationCompare, "$TES_CONTAINER_ARRAY_POINTER, R8")
	inst5(e, asmarm64.OperationBranchIfNotEqual, "element_shim")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R9, element_array_length")
	inst5(e, asmarm64.OperationMove64Bits, "0(R5), R5")
	e.Label("element_array_length")
	inst5(e, asmarm64.OperationMove64Bits, "TES_ARRAY_LENGTH(R7), R6")
	e.Label("element_data")
	inst5(e, asmarm64.OperationCompareAndBranchIfZero, "R5, element_shim")
}

// emitElementMoveByWidthARM64 makes one of four moves, chosen by the element size in R6
// (1, 2, 4 or 8 bytes).
//
// Every move but the 1-byte one branches to done after it. The 1-byte move comes last and
// runs on into whatever is emitted next.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes label (string) which names the block and prefixes the labels inside it.
// Takes done (string) which is the label the wider moves branch to.
// Takes moves ([elementWidthCount]elementMoveARM64) which are the 1-, 2-, 4- and 8-byte
// moves.
func emitElementMoveByWidthARM64(e *asmgen.Emitter, label, done string, moves [elementWidthCount]elementMoveARM64) {
	e.Label(label)
	for index := elementWidthCount - 1; index > 0; index-- {
		narrower := label + "_" + strconv.Itoa(1<<(index-1))
		inst5(e, asmarm64.OperationCompare, "$"+strconv.Itoa(1<<index)+", R6")
		inst5(e, asmarm64.OperationBranchIfNotEqual, narrower)
		inst5(e, moves[index].mnemonic, moves[index].operands)
		inst5(e, asmarm64.OperationBranch, done)
		e.Label(narrower)
	}
	inst5(e, moves[0].mnemonic, moves[0].operands)
}
