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

// ExtensionIntMask marks which bytes of one extension word name int registers.
type ExtensionIntMask = [NumInstructionOperands]bool

var (
	// extensionNone is a mask where no byte names an int register.
	extensionNone = ExtensionIntMask{}

	// extensionByteA is a mask where only byte A names an int register.
	extensionByteA = ExtensionIntMask{true, false, false}

	// extensionByteC is a mask where only byte C names an int register.
	extensionByteC = ExtensionIntMask{false, false, true}
)

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
	word := words[len(words)-1]
	switch owner.Op {
	case OpDrillTier1:
		return tier1ExtensionIntUse(owner, words)
	case OpGo, OpDefer:

		return ExtensionIntMask{false, RegisterKind(word.C) == RegisterInt, false}, extensionNone, true
	case OpSliceString:

		return ExtensionIntMask{owner.C&1 != 0, owner.C&2 != 0, false}, extensionNone, true
	case OpGetStructFieldIndexGeneral, OpSetStructFieldIndexGeneral, OpGetStructFieldSliceIndexScalar:

		if len(words) == 1 {
			return extensionByteC, extensionNone, true
		}
		return extensionNone, extensionNone, true
	case OpMapIndexOk, OpMapIndexOkIntInt, OpMapIndexOkStringInt, OpMapIndexOkStringString,
		OpMapIndexOkIntString, OpMapIndexOkIntGeneral, OpMapIndexOkStringGeneral:

		return extensionNone, extensionByteA, true
	case OpMakeSlice, OpPackTyped, OpAllocIndirect, OpConvert, OpTypeAssert, OpAppendStructFast,
		OpBindMethod, OpCopyStructFieldGeneralT0, OpStrReplaceAll,
		OpSliceIndexStructFieldInt, OpSliceIndexStructFieldUint, OpSliceIndexStructFieldFloat,
		OpSliceIndexStructFieldBool, OpSliceIndexStructFieldString:

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
// Returns reads, writes and ok as ExtensionIntUse() does.
func tier1ExtensionIntUse(owner Instruction, words []Instruction) (reads, writes ExtensionIntMask, ok bool) {
	if SubOpcode(owner.A) == SubOpDrillTier2 {
		return tier2ExtensionIntUse(owner)
	}
	first, word, ordinal := words[0], words[len(words)-1], len(words)-1
	switch SubOpcode(owner.A) {
	case SubOpMakeSliceInt, SubOpMakeSliceFloat, SubOpMakeSliceString, SubOpMakeSliceBool,
		SubOpMakeSliceUint, SubOpMakeSliceByte, SubOpMakeSliceHeap,
		SubOpSliceGetFloatDirect, SubOpSliceGetStringDirect, SubOpSliceGetBoolDirect,
		SubOpSliceGetUintDirect, SubOpSliceGetByteDirect,
		SubOpAppendSliceIntDirect, SubOpStrconvFormatInt:

		return extensionByteA, extensionNone, true
	case SubOpSliceOp, SubOpSliceByteSlice, SubOpSliceSliceIntDirect, SubOpSliceSliceFloatDirect,
		SubOpSliceSliceStringDirect, SubOpSliceSliceBoolDirect, SubOpSliceSliceUintDirect:
		return sliceBoundsExtensionIntReads(first, ordinal), extensionNone, true
	case SubOpCallBuiltin:

		return ExtensionIntMask{RegisterKind(word.B) == RegisterInt, false, false}, extensionNone, true
	case SubOpSelect:
		reads, writes = selectExtensionIntUse(words)
		return reads, writes, true
	case SubOpChannelReceive:

		return extensionNone, ExtensionIntMask{RegisterKind(word.B) == RegisterInt, false, false}, true
	case SubOpRangeNext:
		return extensionNone, rangeNextExtensionIntWrites(first, word, ordinal), true
	case SubOpCopySliceIntDirect, SubOpCopySliceFloatDirect, SubOpCopySliceStringDirect,
		SubOpCopySliceBoolDirect, SubOpCopySliceUintDirect, SubOpCopySliceByteDirect,
		SubOpEqInterfaceStrict, SubOpNeInterfaceStrict:

		return extensionNone, extensionByteA, true
	case SubOpSliceSetFloatDirect, SubOpSliceSetStringDirect, SubOpSliceSetBoolDirect,
		SubOpSliceSetUintDirect, SubOpSliceSetByteDirect,
		SubOpAppendSliceFloatDirect, SubOpAppendSliceStringDirect, SubOpAppendSliceBoolDirect,
		SubOpAppendSliceUintDirect, SubOpAppendSliceByteDirect, SubOpAppendUintInPlace,
		SubOpCallMethod, SubOpCallMethodInlineable, SubOpGetMethod, SubOpSpill, SubOpReload,
		SubOpGetGlobalWide, SubOpSetGlobalWide, SubOpMakeMethodExpr, SubOpMakeChannel, SubOpMathMod,
		SubOpGetStructFieldInt, SubOpGetStructFieldUint, SubOpGetStructFieldFloat,
		SubOpGetStructFieldBool, SubOpGetStructFieldString,
		SubOpSetStructFieldInt, SubOpSetStructFieldUint, SubOpSetStructFieldFloat,
		SubOpSetStructFieldBool, SubOpSetStructFieldString,
		SubOpGetStructFieldSliceInt, SubOpGetStructFieldSliceFloat, SubOpGetStructFieldSliceUint,
		SubOpGetStructFieldSliceString, SubOpGetStructFieldSliceBool, SubOpGetStructFieldSliceByte,
		SubOpSetStructFieldSliceInt, SubOpSetStructFieldSliceFloat, SubOpSetStructFieldSliceUint,
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
	case SubOpTier2MakeMap, SubOpTier2AllocStructLiteral:

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
		return ExtensionIntMask{RegisterKind(word.B) == RegisterInt, false, false}, extensionNone
	case SelectDirectionReceive:
		return extensionNone, ExtensionIntMask{RegisterKind(word.B) == RegisterInt, false, header.C != 0}
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
		return ExtensionIntMask{false, flags&1 != 0 && RegisterKind(word.C) == RegisterInt, false}
	case 1:
		return ExtensionIntMask{false, flags&2 != 0 && RegisterKind(word.C) == RegisterInt, false}
	default:
		return extensionNone
	}
}
