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

package isa

import "math"

// ExtensionIntMask marks which bytes of one extension word name int registers.
type ExtensionIntMask = [NumInstructionOperands]bool

const (
	// ByteRunMinStores is the fewest stores one SubOpSetSliceByteElementRun makes.
	ByteRunMinStores = 2

	// ByteRunMaxStores is the most stores one SubOpSetSliceByteElementRun makes.
	ByteRunMaxStores = 4

	// FieldRunMinReads is the fewest reads one SubOpGetStructFieldUint64Run or
	// SubOpGetElementFieldUint64Run makes.
	FieldRunMinReads = 2

	// FieldRunMaxReads is the most reads one SubOpGetStructFieldUint64Run or
	// SubOpGetElementFieldUint64Run makes.
	FieldRunMaxReads = 4

	// FieldRunCountMask selects the number of reads from byte C of the first extension word
	// of a SubOpGetStructFieldUint64Run or a SubOpGetElementFieldUint64Run.
	FieldRunCountMask = 0x0F

	// FieldRunModeShift moves the OpMoveGeneral mode out of byte C of the first extension
	// word of a SubOpGetStructFieldUint64Run or a SubOpGetElementFieldUint64Run.
	FieldRunModeShift = 4

	// FieldRunMaxMode is the largest move mode the high half of a field run's first word can
	// hold.
	FieldRunMaxMode = math.MaxUint8 >> FieldRunModeShift

	// ElementFieldRunScratchWord is the position, among the extension words of a
	// SubOpGetElementFieldUint64Run, of the word whose byte C names the register of the
	// general bank that the operation writes only for a debugger.
	ElementFieldRunScratchWord = 1

	_ uint = FieldRunMinReads - ElementFieldRunScratchWord - 1
)

var (
	// extensionNone is a mask where no byte names an int register.
	extensionNone = ExtensionIntMask{}

	// extensionByteA is a mask where only byte A names an int register.
	extensionByteA = ExtensionIntMask{true, false, false}

	// extensionByteC is a mask where only byte C names an int register.
	extensionByteC = ExtensionIntMask{false, false, true}
)

// ExtensionNamesOnlyIntRegisters reports whether every byte of the owner's extension
// words is an int register or an immediate, so no byte names a register of any other
// bank.
//
// Takes owner (Instruction) which is the operation the extension words belong to.
//
// Returns true when the owner's extension words name no register outside the int bank.
func ExtensionNamesOnlyIntRegisters(owner Instruction) bool {
	switch owner.Op {
	case OpGetStructFieldElementGeneral, OpSetStructFieldElementGeneral,
		OpTypeAssert, OpBoxAsType, OpAllocateIndirect, OpAppendStruct, OpSliceString,
		OpMultiplyInt64ConstantThenAdd,
		OpGetMapOk, OpGetMapInt64Int64Ok, OpGetMapStringInt64Ok, OpGetMapStringStringOk,
		OpGetMapInt64StringOk, OpGetMapInt64GeneralOk, OpGetMapStringGeneralOk:
		return true
	case OpDrillTier1:
		return tier1ExtensionNamesOnlyIntRegisters(owner)
	default:
		return isGetMapOkThenJumpIfFalse(owner.Op)
	}
}

// ExtensionRegisterUse reports extension-word register effects in one bank.
//
// Takes owner (Instruction) which owns the extension words.
// Takes words ([]Instruction) which ends with the extension word being classified.
// Takes kind (RegisterKind) which selects the register bank.
//
// Returns reads (ExtensionIntMask) which marks bytes that name registers read in kind.
// Returns writes (ExtensionIntMask) which marks bytes that name registers written in
// kind.
// Returns ok (bool) which is false when the layout is unknown or words is empty.
func ExtensionRegisterUse(owner Instruction, words []Instruction, kind RegisterKind) (reads, writes ExtensionIntMask, ok bool) {
	if len(words) == 0 {
		return extensionNone, extensionNone, false
	}
	if kind == RegisterInt64 {
		return ExtensionIntUse(owner, words)
	}
	if IsElementFieldStore(owner) {
		return extensionNone, elementFieldStoreExtensionWrites(words, kind), true
	}
	if ExtensionNamesOnlyIntRegisters(owner) || extensionNamesNoRegister(owner) ||
		kind == RegisterGeneral && extensionNamesNoGeneralRegister(owner) {
		return extensionNone, extensionNone, true
	}
	if InstrIsTier1SubOp(owner, SubOpSetSliceByteElementRun) {
		return byteRunExtensionUintReads(words, kind), extensionNone, len(words) <= ByteRunMaxStores
	}
	if InstrIsTier1SubOp(owner, SubOpGetStructFieldUint64Run) {
		return extensionNone, fieldRunExtensionUintWrites(words, kind), len(words) <= FieldRunMaxReads
	}
	if InstrIsTier1SubOp(owner, SubOpGetElementFieldUint64Run) {
		return extensionNone, elementFieldRunExtensionWrites(words, kind), len(words) <= FieldRunMaxReads
	}
	if owner.Op != OpDrillTier1 || len(words) != 1 {
		return extensionNone, extensionNone, false
	}
	var source RegisterKind
	switch SubOpcode(owner.A) {
	case SubOpSetSliceFloat64Element:
		source = RegisterFloat64
	case SubOpSetSliceStringElement:
		source = RegisterString
	case SubOpSetSliceBoolElement:
		source = RegisterBool
	case SubOpSetSliceUint64Element, SubOpSetSliceByteElement:
		source = RegisterUint64
	default:
		return extensionNone, extensionNone, false
	}
	return ExtensionIntMask{kind == source, false, false}, extensionNone, true
}

// byteRunExtensionUintReads reads a byte-store extension word's uint register from byte
// A.
//
// Takes words ([]Instruction) which are the extension words up to the one classified.
// Takes kind (RegisterKind) which is the bank asked about.
//
// Returns ExtensionIntMask which is the read mask of the last word, empty for any bank
// but the uint one.
func byteRunExtensionUintReads(words []Instruction, kind RegisterKind) ExtensionIntMask {
	if kind != RegisterUint64 || len(words) > int(words[0].C) {
		return extensionNone
	}
	return extensionByteA
}

// fieldRunExtensionUintWrites reads a field-read extension word's uint destination from
// byte A.
//
// Takes words ([]Instruction) which are the extension words up to the one classified.
// Takes kind (RegisterKind) which is the bank asked about.
//
// Returns ExtensionIntMask which is the write mask of the last word, empty for any bank
// but the uint one.
func fieldRunExtensionUintWrites(words []Instruction, kind RegisterKind) ExtensionIntMask {
	if kind != RegisterUint64 || len(words) > int(words[0].C&FieldRunCountMask) {
		return extensionNone
	}
	return extensionByteA
}

// elementFieldRunExtensionWrites is fieldRunExtensionUintWrites for a
// SubOpGetElementFieldUint64Run, whose second word also names the general scratch
// register in byte C.
//
// Takes words ([]Instruction) which are the extension words up to the one classified.
// Takes kind (RegisterKind) which is the bank asked about.
//
// Returns ExtensionIntMask which is the write mask of the last word.
func elementFieldRunExtensionWrites(words []Instruction, kind RegisterKind) ExtensionIntMask {
	if len(words) > int(words[0].C&FieldRunCountMask) {
		return extensionNone
	}
	switch {
	case kind == RegisterUint64:
		return extensionByteA
	case kind == RegisterGeneral && len(words) == ElementFieldRunScratchWord+1:
		return extensionByteC
	default:
		return extensionNone
	}
}

// elementFieldStoreExtensionWrites reads the general register an element field store may
// leave its element in from byte C of the store's one extension word.
//
// Takes words ([]Instruction) which are the extension words up to the one classified.
// Takes kind (RegisterKind) which is the bank asked about.
//
// Returns ExtensionIntMask which is the write mask of the last word, empty for any bank
// but the general one.
func elementFieldStoreExtensionWrites(words []Instruction, kind RegisterKind) ExtensionIntMask {
	if kind != RegisterGeneral || len(words) != 1 {
		return extensionNone
	}
	return extensionByteC
}

// RunParts reports how many extension words a run instruction says follow it, one for
// each of its stores or reads, and the bounds the compiler keeps that number within.
//
// Takes owner (Instruction) which is the instruction asked about.
// Takes first (Instruction) which is the word after it.
//
// Returns parts (int) which is the number of words the run counts.
// Returns least (int) which is the fewest a well-formed run counts.
// Returns most (int) which is the most a well-formed run counts.
// Returns ok (bool) which is false when owner is not a run.
func RunParts(owner, first Instruction) (parts, least, most int, ok bool) {
	switch {
	case InstrIsTier1SubOp(owner, SubOpSetSliceByteElementRun):
		return int(first.C), ByteRunMinStores, ByteRunMaxStores, true
	case InstrIsTier1SubOp(owner, SubOpGetStructFieldUint64Run), InstrIsTier1SubOp(owner, SubOpGetElementFieldUint64Run):
		return int(first.C & FieldRunCountMask), FieldRunMinReads, FieldRunMaxReads, true
	default:
		return 0, 0, 0, false
	}
}

// IsElementFieldStore reports whether an instruction stores into a field of a slice
// element, whose extension word names a general scratch register in its byte C.
//
// The register is recorded as written, which keeps a pass from giving the byte a register
// whose value is still wanted, although a store that succeeds restores it.
//
// Takes instruction (Instruction) which is the instruction.
//
// Returns bool which is true for OpSetElementFieldInt64 and OpSetElementFieldFloat64.
func IsElementFieldStore(instruction Instruction) bool {
	return instruction.Op == OpSetElementFieldInt64 || instruction.Op == OpSetElementFieldFloat64
}

// ExtensionConstantIndex reports the uint constant a constant arithmetic form names in
// its extension word instead of in its own operands.
//
// Takes owner (Instruction) which is the instruction that owns the word.
// Takes extension (Instruction) which is the word.
//
// Returns index (int) which is the constant's index in its pool.
// Returns kind (RegisterKind) which names the pool.
// Returns ok (bool) which is false when the owner is not one of those forms.
func ExtensionConstantIndex(owner, extension Instruction) (index int, kind RegisterKind, ok bool) {
	if owner.Op != OpDrillTier1 {
		return 0, 0, false
	}
	switch SubOpcode(owner.A) {
	case SubOpAddUint64Constant, SubOpSubtractUint64Constant, SubOpBitAndUint64Constant,
		SubOpAddUint32Constant, SubOpSubtractUint32Constant:
		return int(JoinWide(extension.A, extension.B)), RegisterUint64, true
	default:
		return 0, 0, false
	}
}

// extensionNamesNoRegister reports whether the owner's extension word names no register.
//
// Takes owner (Instruction) which is the operation the extension word belongs to.
//
// Returns bool which is true when the word names no register.
func extensionNamesNoRegister(owner Instruction) bool {
	switch owner.Op {
	case OpGetElementFieldInt64, OpGetElementFieldUint64, OpGetElementFieldFloat64,
		OpGetElementFieldBool, OpGetElementFieldString,
		OpCopyStructFieldGeneral, OpMakeSlice, OpConvertGeneral:
		return true
	case OpDrillTier1:
	default:
		return false
	}
	switch SubOpcode(owner.A) {
	case SubOpDrillTier2:
		return tier2ExtensionNamesNoRegister(SubOpcodeTier2(owner.B))
	case SubOpGetGlobalWide, SubOpSetGlobalWide, SubOpCallMethod, SubOpCallMethodInlineable,
		SubOpAddUint64Constant, SubOpSubtractUint64Constant, SubOpBitAndUint64Constant,
		SubOpAddUint32Constant, SubOpSubtractUint32Constant,
		SubOpIncrementInt64ThenJumpIfLessThan, SubOpIncrementInt64ThenJumpIfLessThanConstant,
		SubOpIncrementInt64ThenJumpIfLessOrEqual, SubOpEqualUint64ThenJumpIfFalse, SubOpNotEqualUint64ThenJumpIfFalse,
		SubOpLessThanLengthStringThenJumpIfFalse,
		SubOpLessThanInt64ThenJumpIfFalse, SubOpLessOrEqualInt64ThenJumpIfFalse, SubOpGreaterThanInt64ThenJumpIfFalse,
		SubOpGreaterOrEqualInt64ThenJumpIfFalse, SubOpEqualInt64ThenJumpIfFalse, SubOpNotEqualInt64ThenJumpIfFalse,
		SubOpLessOrEqualInt64ConstantThenJumpIfFalse, SubOpLessThanInt64ConstantThenJumpIfFalse,
		SubOpEqualInt64ConstantThenJumpIfFalse, SubOpEqualInt64ConstantThenJumpIfTrue,
		SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, SubOpGreaterThanInt64ConstantThenJumpIfFalse,
		SubOpEqualStringConstantThenJumpIfFalse, SubOpEqualUint64ImmediateThenJumpIfFalse:
		return true
	default:
		return false
	}
}

// tier2ExtensionNamesNoRegister answers extensionNamesNoRegister() for the tier-2
// operations.
//
// Takes operation (SubOpcodeTier2) which is the tier-2 operation.
//
// Returns bool which is true for the bool jumps, whose word is a jump offset, and for the
// range check, whose words are two immediate bounds and a jump offset.
func tier2ExtensionNamesNoRegister(operation SubOpcodeTier2) bool {
	switch operation {
	case SubOpTier2JumpIfFalseBool, SubOpTier2JumpIfTrueBool, SubOpTier2InRangeUint64ThenJumpIfFalse:
		return true
	default:
		return false
	}
}

// extensionNamesNoGeneralRegister reports whether extension words name no general
// register, for an owner whose words name a register of some other bank.
//
// Takes owner (Instruction) which is the operation the extension words belong to.
//
// Returns true when the owner's extension words name no general register.
func extensionNamesNoGeneralRegister(owner Instruction) bool {
	switch owner.Op {
	case OpAddInt64ConstantThenJump, OpRangeNextSliceByte, OpRangeNextSliceInt64, OpBindMethod,
		OpStringsReplaceAll, OpGetStructFieldElementField:
		return true
	case OpDrillTier1:
		return tier1ExtensionNamesNoGeneralRegister(owner)
	default:
		return false
	}
}

// tier1ExtensionNamesNoGeneralRegister answers extensionNamesNoGeneralRegister() for the
// tier-1 and tier-2 operations.
//
// Takes owner (Instruction) which is the drilled instruction.
//
// Returns true when the owner's extension words name no general register.
func tier1ExtensionNamesNoGeneralRegister(owner Instruction) bool {
	switch SubOpcode(owner.A) {
	case SubOpDrillTier2:
		return SubOpcodeTier2(owner.B) == SubOpTier2MakeInterfaceMethodExpression
	case SubOpMathMod, SubOpStrconvFormatInt, SubOpMakeMethodExpression, SubOpMakeChannel, SubOpBindMethodByName,
		SubOpReload,
		SubOpMakeSliceInt64, SubOpMakeSliceFloat64, SubOpMakeSliceString, SubOpMakeSliceBool, SubOpMakeSliceUint64,
		SubOpMakeSliceByte, SubOpMakeSliceHeap,
		SubOpAppendUint64Wide, SubOpAppendInt64Wide, SubOpAppendStringWide, SubOpAppendFloat64Wide, SubOpAppendBoolWide,
		SubOpAppendUint64WideInPlace,
		SubOpAppendSliceInt64, SubOpAppendSliceFloat64, SubOpAppendSliceString, SubOpAppendSliceBool,
		SubOpAppendSliceUint64, SubOpAppendSliceByte,
		SubOpSimdDotProductFloat64, SubOpSimdSumFloat64, SubOpSimdAddFloat64, SubOpSimdScaleFloat64:
		return true
	default:
		return false
	}
}

// tier1ExtensionNamesOnlyIntRegisters answers ExtensionNamesOnlyIntRegisters() for the
// tier-1 and tier-2 operations.
//
// Takes owner (Instruction) which is the drilled instruction.
//
// Returns true when the owner's extension words name no register outside the int bank.
func tier1ExtensionNamesOnlyIntRegisters(owner Instruction) bool {
	switch SubOpcode(owner.A) {
	case SubOpDrillTier2:
		switch SubOpcodeTier2(owner.B) {
		case SubOpTier2MakeMap, SubOpTier2AllocateStructLiteral:
			return true
		default:
			return false
		}
	case SubOpGetSliceFloat64Element, SubOpGetSliceStringElement, SubOpGetSliceBoolElement,
		SubOpGetSliceUint64Element, SubOpGetSliceByteElement, SubOpGetSliceByteElementInt64,
		SubOpSliceGeneral, SubOpSliceSliceByte, SubOpSliceSliceInt64, SubOpSliceSliceFloat64,
		SubOpSliceSliceString, SubOpSliceSliceBool, SubOpSliceSliceUint64,
		SubOpGetStructFieldInt64Wide, SubOpGetStructFieldUint64Wide, SubOpGetStructFieldFloat64Wide,
		SubOpGetStructFieldBoolWide, SubOpGetStructFieldStringWide, SubOpAddressField,
		SubOpSetStructFieldInt64Wide, SubOpSetStructFieldUint64Wide, SubOpSetStructFieldFloat64Wide,
		SubOpSetStructFieldBoolWide, SubOpSetStructFieldStringWide,
		SubOpGetStructFieldSliceInt64, SubOpGetStructFieldSliceFloat64, SubOpGetStructFieldSliceUint64,
		SubOpGetStructFieldSliceString, SubOpGetStructFieldSliceBool, SubOpGetStructFieldSliceByte,
		SubOpSetStructFieldSliceInt64, SubOpSetStructFieldSliceFloat64, SubOpSetStructFieldSliceUint64,
		SubOpSetStructFieldSliceString, SubOpSetStructFieldSliceBool, SubOpSetStructFieldSliceByte:
		return true
	default:
		return false
	}
}

// ExtensionIntUse reports which bytes of an extension word name int registers that the
// owning operation reads, and which name int registers it writes. When the layout is not
// recorded, ok is false and the caller must assume any byte may name an int register.
//
// Takes owner (Instruction) which is the operation the extension words belong to.
// Takes words ([]Instruction) which are the extension words up to and including the one
// being classified.
//
// Returns reads (ExtensionIntMask) which marks bytes that name int registers read.
// Returns writes (ExtensionIntMask) which marks bytes that name int registers written.
// Returns ok (bool) which is false when the layout is not recorded or words is empty.
func ExtensionIntUse(owner Instruction, words []Instruction) (reads, writes ExtensionIntMask, ok bool) {
	if len(words) == 0 {
		return extensionNone, extensionNone, false
	}
	if extensionNamesNoRegister(owner) {
		return extensionNone, extensionNone, true
	}
	word := words[len(words)-1]
	switch owner.Op {
	case OpDrillTier1:
		return tier1ExtensionIntUse(owner, words)
	case OpGo, OpDefer:

		return ExtensionIntMask{false, RegisterKind(word.C) == RegisterInt64, false}, extensionNone, true
	case OpSliceString:

		return ExtensionIntMask{owner.C&1 != 0, owner.C&2 != 0, false}, extensionNone, true
	case OpGetStructFieldElementGeneral, OpSetStructFieldElementGeneral, OpGetStructFieldElementField:

		if len(words) == 1 {
			return extensionByteC, extensionNone, true
		}
		return extensionNone, extensionNone, true
	case OpGetMapOk, OpGetMapInt64Int64Ok, OpGetMapStringInt64Ok, OpGetMapStringStringOk,
		OpGetMapInt64StringOk, OpGetMapInt64GeneralOk, OpGetMapStringGeneralOk,
		OpGetMapInt64Int64OkThenJumpIfFalse, OpGetMapStringInt64OkThenJumpIfFalse,
		OpGetMapStringStringOkThenJumpIfFalse, OpGetMapInt64StringOkThenJumpIfFalse,
		OpGetMapInt64GeneralOkThenJumpIfFalse, OpGetMapStringGeneralOkThenJumpIfFalse:

		return extensionNone, extensionByteA, true
	case OpMultiplyInt64ConstantThenAdd:

		return extensionByteA, extensionNone, true
	case OpMakeSlice, OpBoxAsType, OpAllocateIndirect, OpConvertGeneral, OpTypeAssert, OpAppendStruct,
		OpBindMethod, OpCopyStructFieldGeneral, OpStringsReplaceAll,
		OpGetElementFieldInt64, OpGetElementFieldUint64, OpGetElementFieldFloat64,
		OpGetElementFieldBool, OpGetElementFieldString,
		OpSetElementFieldInt64, OpSetElementFieldFloat64:

		return extensionNone, extensionNone, true
	default:
		return extensionNone, extensionNone, false
	}
}

// tier1ExtensionIntUse answers ExtensionIntUse() for the tier-1 and deeper operations.
//
// Takes owner (Instruction) which is the drilled instruction.
// Takes words ([]Instruction) which are its extension words up to the one classified.
//
// Returns reads (ExtensionIntMask) which marks the int registers read.
// Returns writes (ExtensionIntMask) which marks the int registers written.
// Returns ok (bool) which is true when the instruction is classified.
func tier1ExtensionIntUse(owner Instruction, words []Instruction) (reads, writes ExtensionIntMask, ok bool) {
	if SubOpcode(owner.A) == SubOpDrillTier2 {
		return tier2ExtensionIntUse(owner)
	}
	first, word, ordinal := words[0], words[len(words)-1], len(words)-1
	switch SubOpcode(owner.A) {
	case SubOpMakeSliceInt64, SubOpMakeSliceFloat64, SubOpMakeSliceString, SubOpMakeSliceBool,
		SubOpMakeSliceUint64, SubOpMakeSliceByte, SubOpMakeSliceHeap,
		SubOpGetSliceFloat64Element, SubOpGetSliceStringElement, SubOpGetSliceBoolElement,
		SubOpGetSliceUint64Element, SubOpGetSliceByteElement, SubOpGetSliceByteElementInt64,
		SubOpAppendSliceInt64, SubOpStrconvFormatInt:

		return extensionByteA, extensionNone, true
	case SubOpSliceGeneral, SubOpSliceSliceByte, SubOpSliceSliceInt64, SubOpSliceSliceFloat64,
		SubOpSliceSliceString, SubOpSliceSliceBool, SubOpSliceSliceUint64:
		return sliceBoundsExtensionIntReads(first, ordinal), extensionNone, true
	case SubOpCallBuiltin:

		return ExtensionIntMask{RegisterKind(word.B) == RegisterInt64, false, false}, extensionNone, true
	case SubOpSelect:
		reads, writes = selectExtensionIntUse(words)
		return reads, writes, true
	case SubOpReceiveFromChannel:

		return extensionNone, ExtensionIntMask{RegisterKind(word.B) == RegisterInt64, false, false}, true
	case SubOpRangeNext:
		return extensionNone, rangeNextExtensionIntWrites(first, word, ordinal), true
	case SubOpCopySliceInt64, SubOpCopySliceFloat64, SubOpCopySliceString,
		SubOpCopySliceBool, SubOpCopySliceUint64, SubOpCopySliceByte,
		SubOpEqualInterfaceStrict, SubOpNotEqualInterfaceStrict:

		return extensionNone, extensionByteA, true
	case SubOpSetSliceFloat64Element, SubOpSetSliceStringElement, SubOpSetSliceBoolElement,
		SubOpSetSliceUint64Element, SubOpSetSliceByteElement, SubOpSetSliceByteElementRun,
		SubOpGetStructFieldUint64Run, SubOpGetElementFieldUint64Run,
		SubOpAppendSliceFloat64, SubOpAppendSliceString, SubOpAppendSliceBool,
		SubOpAppendSliceUint64, SubOpAppendSliceByte, SubOpAppendUint64WideInPlace,
		SubOpCallMethod, SubOpCallMethodInlineable, SubOpBindMethodByName, SubOpSpill, SubOpReload,
		SubOpGetGlobalWide, SubOpSetGlobalWide, SubOpMakeMethodExpression, SubOpMakeChannel, SubOpMathMod,
		SubOpGetStructFieldInt64Wide, SubOpGetStructFieldUint64Wide, SubOpGetStructFieldFloat64Wide,
		SubOpGetStructFieldBoolWide, SubOpGetStructFieldStringWide, SubOpAddressField,
		SubOpSetStructFieldInt64Wide, SubOpSetStructFieldUint64Wide, SubOpSetStructFieldFloat64Wide,
		SubOpSetStructFieldBoolWide, SubOpSetStructFieldStringWide,
		SubOpGetStructFieldSliceInt64, SubOpGetStructFieldSliceFloat64, SubOpGetStructFieldSliceUint64,
		SubOpGetStructFieldSliceString, SubOpGetStructFieldSliceBool, SubOpGetStructFieldSliceByte,
		SubOpSetStructFieldSliceInt64, SubOpSetStructFieldSliceFloat64, SubOpSetStructFieldSliceUint64,
		SubOpSetStructFieldSliceString, SubOpSetStructFieldSliceBool, SubOpSetStructFieldSliceByte:

		return extensionNone, extensionNone, true
	default:
		return extensionNone, extensionNone, false
	}
}

// sliceBoundsExtensionIntReads decodes the bounds words shared by the slice expressions:
// the first word is {flags, lowRegister, highRegister} and, when the flags carry
// SliceMaxBitFlag, a second word {maxRegister, 0, 0} follows. A bound register is read
// only when its flag is set.
//
// Takes first (Instruction) which is the flags word.
// Takes ordinal (int) which is the position of the word being classified.
//
// Returns the int-read mask for that word.
func sliceBoundsExtensionIntReads(first Instruction, ordinal int) ExtensionIntMask {
	flags := first.A
	switch ordinal {
	case 0:
		return ExtensionIntMask{false, flags&SliceLowBoundFlag != 0, flags&SliceHighBoundFlag != 0}
	case 1:
		return ExtensionIntMask{flags&SliceMaxBitFlag != 0, false, false}
	default:
		return extensionNone
	}
}

// tier2ExtensionIntUse answers ExtensionIntUse() for the tier-2 operations.
//
// Takes owner (Instruction) which is the tier-2 instruction.
//
// Returns reads, writes and ok as ExtensionIntUse() does.
func tier2ExtensionIntUse(owner Instruction) (reads, writes ExtensionIntMask, ok bool) {
	switch SubOpcodeTier2(owner.B) {
	case SubOpTier2MakeMap, SubOpTier2AllocateStructLiteral:

		return extensionNone, extensionNone, true
	default:
		return extensionNone, extensionNone, false
	}
}

// selectExtensionIntUse decodes the per-case extension words of a SELECT instruction and
// returns the int-register read/write masks for the last word.
//
// Takes words ([]Instruction) which are the case words up to the one classified.
//
// Returns reads and writes for the last word.
func selectExtensionIntUse(words []Instruction) (reads, writes ExtensionIntMask) {
	last := len(words) - 1
	position := 0
	for position < last {
		switch words[position].A {
		case SelectDirectionReceive, SelectDirectionSend:
			position += 2
		default:
			position++
		}
	}
	if position == last {
		return extensionNone, extensionNone
	}
	header, word := words[last-1], words[last]
	switch header.A {
	case SelectDirectionSend:
		return ExtensionIntMask{RegisterKind(word.B) == RegisterInt64, false, false}, extensionNone
	case SelectDirectionReceive:
		return extensionNone, ExtensionIntMask{RegisterKind(word.B) == RegisterInt64, false, header.C != 0}
	default:
		return extensionNone, extensionNone
	}
}

// rangeNextExtensionIntWrites decodes the two destination words of RANGE_NEXT: the first
// is {flags, keyRegister, keyKind} and the second {0, valueRegister, valueKind}; bit 0 of
// the flags enables the key write and bit 1 the value write.
//
// Takes first (Instruction) which is the flags word.
// Takes word (Instruction) which is the word being classified.
// Takes ordinal (int) which is its position after the instruction.
//
// Returns the int-write mask for that word.
func rangeNextExtensionIntWrites(first, word Instruction, ordinal int) ExtensionIntMask {
	flags := first.A
	switch ordinal {
	case 0:
		return ExtensionIntMask{false, flags&1 != 0 && RegisterKind(word.C) == RegisterInt64, false}
	case 1:
		return ExtensionIntMask{false, flags&2 != 0 && RegisterKind(word.C) == RegisterInt64, false}
	default:
		return extensionNone
	}
}
