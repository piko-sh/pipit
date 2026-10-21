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
	"strconv"

	"piko.sh/asmgen"
	"piko.sh/asmgen/asmamd64"
)

const (
	// operationMove8To64BitsSignExtended is the sign-extending one-byte load mnemonic.
	operationMove8To64BitsSignExtended = "MOVBQSX"

	// operationMove16To64BitsZeroExtended is the zero-extending two-byte load mnemonic.
	operationMove16To64BitsZeroExtended = "MOVWQZX"

	// elementWidthCount is the number of supported integer element widths.
	elementWidthCount = 4
)

// elementMoveAMD64 is the instruction that moves an element of one width.
type elementMoveAMD64 struct {
	// mnemonic is the move.
	mnemonic string

	// operands are its operands.
	operands string
}

var (
	// signedElementLoadsAMD64 are the 1-, 2-, 4- and 8-byte loads of a signed integer
	// element, sign-extending, of element CX of the data at SI into CX.
	signedElementLoadsAMD64 = [elementWidthCount]elementMoveAMD64{
		{mnemonic: operationMove8To64BitsSignExtended, operands: "(SI)(CX*1), CX"},
		{mnemonic: asmamd64.OperationMove16To64BitsSignExtended, operands: "(SI)(CX*2), CX"},
		{mnemonic: operationMove32To64BitsSignExtended, operands: "(SI)(CX*4), CX"},
		{mnemonic: asmamd64.OperationMove64Bits, operands: "(SI)(CX*8), CX"},
	}

	// unsignedElementLoadsAMD64 are the 1-, 2-, 4- and 8-byte loads of an unsigned integer
	// element, zero-extending, of element CX of the data at SI into CX.
	unsignedElementLoadsAMD64 = [elementWidthCount]elementMoveAMD64{
		{mnemonic: asmamd64.OperationMove8To64BitsZeroExtended, operands: "(SI)(CX*1), CX"},
		{mnemonic: operationMove16To64BitsZeroExtended, operands: "(SI)(CX*2), CX"},
		{mnemonic: asmamd64.OperationMove32Bits, operands: "(SI)(CX*4), CX"},
		{mnemonic: asmamd64.OperationMove64Bits, operands: "(SI)(CX*8), CX"},
	}

	// elementStoresAMD64 are the 1-, 2-, 4- and 8-byte stores of BX, truncated, into element
	// CX of the data at SI.
	elementStoresAMD64 = [elementWidthCount]elementMoveAMD64{
		{mnemonic: asmamd64.OperationMove8Bits, operands: "BL, (SI)(CX*1)"},
		{mnemonic: asmamd64.OperationMove16Bits, operands: "BX, (SI)(CX*2)"},
		{mnemonic: asmamd64.OperationMove32Bits, operands: "BX, (SI)(CX*4)"},
		{mnemonic: asmamd64.OperationMove64Bits, operands: "BX, (SI)(CX*8)"},
	}
)

// EmitSliceGetTypedElement emits an integer-element read.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which selects OpGetElementInt64 (any integer element into ints[A])
// over OpGetElementUint64 (an unsigned element into uints[A]).
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitSliceGetTypedElement(e *asmgen.Emitter, signed bool, shimSymbol string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	emitTypedElementShapeAMD64(e, signed)
	e.Blank()

	emitTypedElementDataAMD64(e, false)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(CX*8), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, AX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "element_shim")
	inst(e, asmamd64.OperationMove64Bits, "TES_ELEMENT_SIZE(BX), AX")
	if signed {
		inst(e, asmamd64.OperationCompare8Bits, "TES_ELEMENT_KIND(BX), $REFLECT_INT64")
		inst(e, asmamd64.OperationJumpIfAbove, "element_unsigned")
		emitElementMoveByWidthAMD64(e, "element_signed", "element_result", signedElementLoadsAMD64)
		inst(e, asmamd64.OperationJump, "element_result")
	}
	emitElementMoveByWidthAMD64(e, "element_unsigned", "element_result", unsignedElementLoadsAMD64)
	e.Label("element_result")
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	if signed {
		inst(e, asmamd64.OperationMove64Bits, "CX, (R8)(AX*8)")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove64Bits, "CX, (DI)(AX*8)")
	}
	e.Instruction(macroDispatchNext)
	e.Label("element_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// EmitSliceSetTypedElement emits an integer-element store.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which selects OpSetElementInt64 (ints[C] into any integer element)
// over OpSetElementUint64 (uints[C] into an unsigned element).
// Takes shimSymbol (string) which is the tier-2 shim for everything else.
func (*BytecodeAMD64Arch) EmitSliceSetTypedElement(e *asmgen.Emitter, signed bool, shimSymbol string) {
	inst(e, asmamd64.OperationMove64Bits, "DX, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$8, AX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "AL, AX")
	emitTypedElementShapeAMD64(e, signed)
	e.Blank()

	emitTypedElementDataAMD64(e, true)
	e.Blank()

	inst(e, asmamd64.OperationMove64Bits, "DX, CX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$16, CX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "CL, CX")
	inst(e, asmamd64.OperationMove64Bits, "(R8)(CX*8), CX")
	inst(e, asmamd64.OperationCompare64Bits, "CX, AX")
	inst(e, asmamd64.OperationJumpIfAboveOrEqual, "element_shim")
	inst(e, asmamd64.OperationMove64Bits, "TES_ELEMENT_SIZE(BX), AX")
	inst(e, asmamd64.OperationMove64Bits, "DX, BX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$24, BX")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "BL, BX")
	if signed {
		inst(e, asmamd64.OperationMove64Bits, "(R8)(BX*8), BX")
	} else {
		inst(e, asmamd64.OperationMove64Bits, "CTX_UINTS_BASE(R15), DI")
		inst(e, asmamd64.OperationMove64Bits, "(DI)(BX*8), BX")
	}
	emitElementMoveByWidthAMD64(e, "store", "store_done", elementStoresAMD64)
	e.Label("store_done")
	e.Instruction(macroDispatchNext)
	e.Label("element_shim")
	inst(e, asmamd64.OperationJump, "·"+shimSymbol+"(SB)")
}

// emitTypedElementShapeAMD64 resolves the element shape for the general register in AX.
//
// On success DI addresses the register's slot and BX holds its shape. Other cases jump to
// element_shim.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes signed (bool) which admits every integer element kind. Otherwise only the
// unsigned ones are admitted.
func emitTypedElementShapeAMD64(e *asmgen.Emitter, signed bool) {
	inst(e, asmamd64.OperationMove64Bits, "CTX_GENERALS_BASE(R15), DI")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(AX)(AX*2), AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "(DI)(AX*8), DI")
	inst(e, asmamd64.OperationMove64Bits, "0(DI), SI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	inst(e, asmamd64.OperationMove64Bits, "$TES_HASH_MULTIPLIER, AX")
	inst(e, asmamd64.OperationSignedMultiply64Bits, "SI, AX")
	inst(e, asmamd64.OperationShiftRight64Bits, "$TES_SLOT_SHIFT, AX")
	inst(e, asmamd64.OperationLoadEffectiveAddress64Bits, "·typedElementShapes(SB), BX")
	inst(e, asmamd64.OperationMove64Bits, "(BX)(AX*8), BX")
	inst(e, asmamd64.OperationTest64Bits, "BX, BX")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	inst(e, asmamd64.OperationCompare64Bits, "TES_COLLECTION_TYPE(BX), SI")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_shim")
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "TES_ELEMENT_KIND(BX), AX")
	if signed {
		inst(e, asmamd64.OperationSubtract64Bits, "$REFLECT_INT, AX")
		inst(e, asmamd64.OperationCompare64Bits, "AX, $(REFLECT_UINTPTR-REFLECT_INT)")
	} else {
		inst(e, asmamd64.OperationSubtract64Bits, "$REFLECT_UINT, AX")
		inst(e, asmamd64.OperationCompare64Bits, "AX, $(REFLECT_UINTPTR-REFLECT_UINT)")
	}
	inst(e, asmamd64.OperationJumpIfAbove, "element_shim")
}

// emitTypedElementDataAMD64 locates element data in SI and the collection length in AX.
//
// Unsupported storage jumps to element_shim. Writes also decline read-only values and
// arrays that cannot be addressed.
//
// On entry DI addresses the general slot and BX holds its shape.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes write (bool) which is true when the element is about to be written.
func emitTypedElementDataAMD64(e *asmgen.Emitter, write bool) {
	inst(e, asmamd64.OperationMove64Bits, "8(DI), SI")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	inst(e, asmamd64.OperationMove64Bits, "16(DI), DI")
	if write {
		inst(e, asmamd64.OperationTest64Bits, "$FLAG_RO, DI")
		inst(e, asmamd64.OperationJumpIfNotZero, "element_shim")
	}
	inst(e, asmamd64.OperationMove8To32BitsZeroExtended, "TES_CONTAINER(BX), AX")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $TES_CONTAINER_SLICE")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_not_slice")
	inst(e, asmamd64.OperationTest64Bits, "$FLAG_INDIR, DI")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	inst(e, asmamd64.OperationMove64Bits, "8(SI), AX")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), SI")
	inst(e, asmamd64.OperationJump, "element_data")
	e.Label("element_not_slice")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $TES_CONTAINER_ARRAY")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_not_array")
	inst(e, asmamd64.OperationTest64Bits, "$FLAG_INDIR, DI")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	if write {
		inst(e, asmamd64.OperationTest64Bits, "$FLAG_ADDR, DI")
		inst(e, asmamd64.OperationJumpIfZero, "element_shim")
	}
	inst(e, asmamd64.OperationJump, "element_array_length")
	e.Label("element_not_array")
	inst(e, asmamd64.OperationCompare64Bits, "AX, $TES_CONTAINER_ARRAY_POINTER")
	inst(e, asmamd64.OperationJumpIfNotEqual, "element_shim")
	inst(e, asmamd64.OperationTest64Bits, "$FLAG_INDIR, DI")
	inst(e, asmamd64.OperationJumpIfZero, "element_array_length")
	inst(e, asmamd64.OperationMove64Bits, "0(SI), SI")
	e.Label("element_array_length")
	inst(e, asmamd64.OperationMove64Bits, "TES_ARRAY_LENGTH(BX), AX")
	e.Label("element_data")
	inst(e, asmamd64.OperationTest64Bits, "SI, SI")
	inst(e, asmamd64.OperationJumpIfZero, "element_shim")
}

// emitElementMoveByWidthAMD64 makes one of four moves, chosen by the element size in AX
// (1, 2, 4 or 8 bytes).
//
// Every move but the 1-byte one jumps to done after it. The 1-byte move comes last and
// runs on into whatever is emitted next.
//
// Takes e (*asmgen.Emitter) which receives the emitted instructions.
// Takes label (string) which names the block and prefixes the labels inside it.
// Takes done (string) which is the label the wider moves jump to.
// Takes moves ([elementWidthCount]elementMoveAMD64) which are the 1-, 2-, 4- and 8-byte
// moves.
func emitElementMoveByWidthAMD64(e *asmgen.Emitter, label, done string, moves [elementWidthCount]elementMoveAMD64) {
	e.Label(label)
	for index := elementWidthCount - 1; index > 0; index-- {
		narrower := label + "_" + strconv.Itoa(1<<(index-1))
		inst(e, asmamd64.OperationCompare64Bits, "AX, $"+strconv.Itoa(1<<index))
		inst(e, asmamd64.OperationJumpIfNotEqual, narrower)
		inst(e, moves[index].mnemonic, moves[index].operands)
		inst(e, asmamd64.OperationJump, done)
		e.Label(narrower)
	}
	inst(e, moves[0].mnemonic, moves[0].operands)
}
