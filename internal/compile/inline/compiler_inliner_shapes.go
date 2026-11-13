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

package inline

import "pipit.sh/pipit/internal/isa"

// inlinePool identifies which per-function table an opcode operand references.
type inlinePool uint8

const (
	// poolNone marks an opcode operand that does not reference any per-function pool.
	poolNone inlinePool = iota

	// poolIntConsts references the function's integer constants pool.
	poolIntConsts

	// poolFloatConsts references the function's float constants pool.
	poolFloatConsts

	// poolStringConsts references the function's string constants pool.
	poolStringConsts

	// poolBoolConsts references the function's bool constants pool.
	poolBoolConsts

	// poolUintConsts references the function's uint constants pool.
	poolUintConsts

	// poolComplexConsts references the function's complex constants pool.
	poolComplexConsts

	// poolGeneralConsts references the function's general (reflect.Value) constants pool.
	poolGeneralConsts

	// poolTypeTable references the function's type table (for MakeSlice, Convert,
	// TypeAssert, AllocateIndirect).
	poolTypeTable

	// poolStructLayoutTable references the function's struct-layout table (used by the
	// tier-0 struct-field ops).
	poolStructLayoutTable

	// poolCallSites references the function's call-site table.
	poolCallSites

	// poolFunctions references the program-wide function table.
	poolFunctions

	// poolGlobals marks an index into the program's global store. The index is kept as it
	// stands, which holds only between two functions with the same global bases.
	poolGlobals
)

// inlinePoolShape records the per-opcode pool descriptor used by the inliner. Only
// opcodes with a non-register, non-jump-offset operand need an entry.
//
//exhaustruct:ignore
type inlinePoolShape struct {
	// bKindByte names the pool whose uint8 index lives in operand B alone when nonzero (e.g.
	// opLoadBoolConstant).
	bKindByte inlinePool

	// cKindByte names the pool whose uint8 index lives in operand C of the main instruction
	// when nonzero (e.g. tier-0 struct field T0 ops).
	cKindByte inlinePool

	// bcWide16 names the pool whose uint16 little-endian index is formed by operands B and C
	// together (b|c<<8) when nonzero (e.g. opLoadInt64Constant).
	bcWide16 inlinePool

	// extAWide16 names the pool whose uint16 index lives in operands A and B of the
	// following opExtension word when nonzero.
	extAWide16 inlinePool

	// hasExtensionWord is true when this opcode consumes one opExtension instruction
	// following it in the bytecode stream.
	hasExtensionWord bool

	// extARegBank names the bank for an ext-word operand A register reference (used by
	// opGetMapOk* variants whose extension A is `okRegister`, an int register, and
	// registerInt = 0 conflicts with the zero default so extARegSet acts as the
	// discriminator).
	extARegBank isa.RegisterKind

	// extARegSet is true when extARegBank carries a meaningful value (needed because
	// registerInt = 0).
	extARegSet bool

	// describedExtension is true when the operation's extension words, however many follow
	// it, are copied by reading isa.ExtensionRegisterUse for the registers each names.
	// extAWide16 still names a table index in the first word.
	describedExtension bool

	// relativeJump is true when the operation branches by an offset relative to itself and
	// to nothing else, so a copy of the whole callee body keeps it right.
	relativeJump bool
}

var (
	// inlinePoolShapes maps each operation to its pool descriptor, keyed by the flat (tier,
	// code) slot so an operation that moves between tiers keeps its descriptor.
	inlinePoolShapes [isa.FlatDispatchSlots]inlinePoolShape

	// describedRegistersOnly is the descriptor of an operation whose extension words name
	// registers and no table index.
	describedRegistersOnly = inlinePoolShape{hasExtensionWord: true, describedExtension: true}
)

func init() {
	registerInlineLoadConstShapes()
	registerInlineConstFusedArithShapes()
	registerInlineStructFieldShapes()
	registerInlineExtensionTypeTableShapes()
	registerInlineGetMapOkShapes()
	registerInlineFusedJumpShapes()
	registerInlineCallShape()
	registerInlineDescribedShapes()
}

// registerInlineLoadConstShapes populates wide-index constant load shapes (b|c<<8 ->
// respective constant pool) and the single-byte bool-constant pool index. The bool pool
// has at most 2 entries (true/false) but the callee's index can differ from the caller's
// after merge so the byte must be remapped via bKindByte.
func registerInlineLoadConstShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadInt64Constant)] = inlinePoolShape{bcWide16: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadFloat64Constant)] = inlinePoolShape{bcWide16: poolFloatConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadStringConstant)] = inlinePoolShape{bcWide16: poolStringConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadUint64Constant)] = inlinePoolShape{bcWide16: poolUintConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadComplex128Constant)] = inlinePoolShape{bcWide16: poolComplexConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadGeneralConstant)] = inlinePoolShape{bcWide16: poolGeneralConsts}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpLoadBoolConstant)] = inlinePoolShape{cKindByte: poolBoolConsts}
}

// registerInlineConstFusedArithShapes records the constant-pool operand of arithmetic
// ops.
//
// Compare-and-jump operations are omitted because their extension-word encoding varies
// per opcode.
func registerInlineConstFusedArithShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAddInt64Constant)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAddInt32Constant)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSubtractInt32Constant)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSubtractInt64Constant)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMultiplyInt64Constant)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMultiplyInt64ConstantThenAdd)] = inlinePoolShape{
		cKindByte: poolIntConsts, hasExtensionWord: true, extARegBank: isa.RegisterInt64, extARegSet: true,
	}
}

// registerInlineStructFieldShapes populates tier-0 struct field access ops; operand C is
// a uint8 structLayoutTable index for each.
func registerInlineStructFieldShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLengthStructField)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpGetStructFieldSliceByte)] = inlinePoolShape{extAWide16: poolStructLayoutTable, hasExtensionWord: true}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpGetSliceByteElement)] = inlinePoolShape{hasExtensionWord: true, extARegBank: isa.RegisterInt64, extARegSet: true}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldInt64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldInt64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldUint64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldUint64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldFloat64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldFloat64)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldBool)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldBool)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldGeneral)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldGeneral)] = inlinePoolShape{cKindByte: poolStructLayoutTable}

	inlinePoolShapes[isa.FlatIndexOf(isa.OpCopyStructFieldGeneral)] = inlinePoolShape{
		cKindByte:        poolStructLayoutTable,
		extAWide16:       poolStructLayoutTable,
		hasExtensionWord: true,
	}
}

// registerInlineExtensionTypeTableShapes populates the ops whose extension word carries a
// typeTable index in ext A|B<<8.
func registerInlineExtensionTypeTableShapes() {
	typeTableShape := inlinePoolShape{
		extAWide16:       poolTypeTable,
		hasExtensionWord: true,
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMakeSlice)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpConvertGeneral)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpTypeAssert)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAllocateIndirect)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpBoxAsType)] = typeTableShape
}

// registerInlineGetMapOkShapes populates the isa.OpGetMapOk variants. The main word is
// pure-register.
func registerInlineGetMapOkShapes() {
	mapIndexShape := inlinePoolShape{
		hasExtensionWord: true,
		extARegBank:      isa.RegisterInt64,
		extARegSet:       true,
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapOk)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapInt64Int64Ok)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapInt64StringOk)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapInt64GeneralOk)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapStringInt64Ok)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapStringStringOk)] = mapIndexShape
}

// registerInlineFusedJumpShapes populates the tier-1 compare-and-branch fusions and the
// fused loop latches.
func registerInlineFusedJumpShapes() {
	intConstJump := inlinePoolShape{cKindByte: poolIntConsts, hasExtensionWord: true}
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse, isa.SubOpLessThanInt64ConstantThenJumpIfFalse, isa.SubOpEqualInt64ConstantThenJumpIfFalse,
		isa.SubOpEqualInt64ConstantThenJumpIfTrue, isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse,
		isa.SubOpIncrementInt64ThenJumpIfLessThanConstant,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = intConstJump
	}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpEqualStringConstantThenJumpIfFalse)] = inlinePoolShape{
		cKindByte:        poolStringConsts,
		hasExtensionWord: true,
	}
	registerJump := inlinePoolShape{hasExtensionWord: true}
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpLessThanInt64ThenJumpIfFalse, isa.SubOpLessOrEqualInt64ThenJumpIfFalse, isa.SubOpGreaterThanInt64ThenJumpIfFalse,
		isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, isa.SubOpEqualInt64ThenJumpIfFalse, isa.SubOpNotEqualInt64ThenJumpIfFalse,
		isa.SubOpIncrementInt64ThenJumpIfLessThan,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = registerJump
	}
}

// registerInlineCallShape populates the tier-1 calls whose site index lives in operand
// B|C<<8. isa.SubOpTailCall and isa.SubOpCallScalar share the encoding so the same remap
// path services all three call variants.
func registerInlineCallShape() {
	callSiteShape := inlinePoolShape{bcWide16: poolCallSites}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpCall)] = callSiteShape
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpTailCall)] = callSiteShape
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpCallScalar)] = callSiteShape
}

// inlinePoolShapeFor returns the inlinePoolShape entry for an instruction.
//
// Sparse table; zero-value (poolNone everywhere) means the operation has no pool-index
// operands and the inliner walks only its register operands.
//
// Takes instr (instruction) which is the instruction whose shape to fetch.
//
// Returns the inlinePoolShape entry, zero-value when the operation has no pool operands.
func inlinePoolShapeFor(instr isa.Instruction) inlinePoolShape {
	return inlinePoolShapes[isa.FlatIndexFor(instr)]
}

// registerInlineDescribedShapes records pool mappings for operations spliced by
// description.
func registerInlineDescribedShapes() {
	registerInlineDescribedFieldShapes()
	registerInlineDescribedElementShapes()
	registerInlineDescribedGlobalShapes()

	uintConstant := inlinePoolShape{extAWide16: poolUintConsts, hasExtensionWord: true, describedExtension: true}
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpAddUint64Constant, isa.SubOpSubtractUint64Constant, isa.SubOpBitAndUint64Constant,
		isa.SubOpAddUint32Constant, isa.SubOpSubtractUint32Constant,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = uintConstant
	}

	branch := inlinePoolShape{hasExtensionWord: true, describedExtension: true, relativeJump: true}
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpLessThanLengthStringThenJumpIfFalse, isa.SubOpEqualUint64ImmediateThenJumpIfFalse,
		isa.SubOpEqualUint64ThenJumpIfFalse, isa.SubOpNotEqualUint64ThenJumpIfFalse, isa.SubOpIncrementInt64ThenJumpIfLessOrEqual,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = branch
	}

	for _, opcode := range []isa.Opcode{
		isa.OpGetMapInt64Int64OkThenJumpIfFalse, isa.OpGetMapStringInt64OkThenJumpIfFalse,
		isa.OpGetMapStringStringOkThenJumpIfFalse, isa.OpGetMapInt64StringOkThenJumpIfFalse,
		isa.OpGetMapInt64GeneralOkThenJumpIfFalse, isa.OpGetMapStringGeneralOkThenJumpIfFalse,
	} {
		inlinePoolShapes[isa.FlatIndexOf(opcode)] = branch
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetMapStringGeneralOk)] = describedRegistersOnly

	typeInFirstWord := inlinePoolShape{extAWide16: poolTypeTable, hasExtensionWord: true, describedExtension: true}
	inlinePoolShapes[isa.FlatIndexOfSub2(isa.SubOpTier2MakeMap)] = typeInFirstWord
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAppendStruct)] = typeInFirstWord
}

// registerInlineDescribedFieldShapes records mappings for described field operations.
func registerInlineDescribedFieldShapes() {
	layoutInC := inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldRawPointer)] = layoutInC
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpIncrementStructFieldInt64, isa.SubOpDecrementStructFieldInt64,
		isa.SubOpIncrementStructFieldUint64, isa.SubOpDecrementStructFieldUint64,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = layoutInC
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSwapStructFieldsGeneral)] = inlinePoolShape{
		bKindByte: poolStructLayoutTable, cKindByte: poolStructLayoutTable,
	}

	layoutInCWithWords := inlinePoolShape{cKindByte: poolStructLayoutTable, hasExtensionWord: true, describedExtension: true}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldElementGeneral)] = layoutInCWithWords
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldElementGeneral)] = layoutInCWithWords

	layoutInFirstWord := inlinePoolShape{extAWide16: poolStructLayoutTable, hasExtensionWord: true, describedExtension: true}
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpGetStructFieldInt64Wide, isa.SubOpGetStructFieldUint64Wide, isa.SubOpGetStructFieldFloat64Wide,
		isa.SubOpGetStructFieldBoolWide, isa.SubOpGetStructFieldStringWide,
		isa.SubOpSetStructFieldInt64Wide, isa.SubOpSetStructFieldUint64Wide, isa.SubOpSetStructFieldFloat64Wide,
		isa.SubOpSetStructFieldBoolWide, isa.SubOpSetStructFieldStringWide,
		isa.SubOpGetStructFieldSliceInt64, isa.SubOpGetStructFieldSliceFloat64, isa.SubOpGetStructFieldSliceUint64,
		isa.SubOpGetStructFieldSliceString, isa.SubOpGetStructFieldSliceBool,
		isa.SubOpSetStructFieldSliceInt64, isa.SubOpSetStructFieldSliceFloat64, isa.SubOpSetStructFieldSliceUint64,
		isa.SubOpSetStructFieldSliceString, isa.SubOpSetStructFieldSliceBool, isa.SubOpSetStructFieldSliceByte,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = layoutInFirstWord
	}
	for _, opcode := range []isa.Opcode{
		isa.OpGetElementFieldInt64, isa.OpGetElementFieldUint64, isa.OpGetElementFieldFloat64,
		isa.OpGetElementFieldBool, isa.OpGetElementFieldString,
		isa.OpSetElementFieldInt64, isa.OpSetElementFieldFloat64,
	} {
		inlinePoolShapes[isa.FlatIndexOf(opcode)] = layoutInFirstWord
	}
}

// registerInlineDescribedElementShapes records mappings for described element and slicing
// operations.
func registerInlineDescribedElementShapes() {
	for _, subOpcode := range []isa.SubOpcode{
		isa.SubOpGetSliceFloat64Element, isa.SubOpGetSliceStringElement, isa.SubOpGetSliceBoolElement,
		isa.SubOpGetSliceUint64Element, isa.SubOpGetSliceByteElementInt64,
		isa.SubOpSetSliceFloat64Element, isa.SubOpSetSliceStringElement, isa.SubOpSetSliceBoolElement,
		isa.SubOpSetSliceUint64Element, isa.SubOpSetSliceByteElement,
		isa.SubOpSliceGeneral, isa.SubOpSliceSliceByte, isa.SubOpSliceSliceInt64, isa.SubOpSliceSliceFloat64,
		isa.SubOpSliceSliceString, isa.SubOpSliceSliceBool, isa.SubOpSliceSliceUint64,
		isa.SubOpSetSliceByteElementRun, isa.SubOpAddressField,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(subOpcode)] = describedRegistersOnly
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSliceString)] = describedRegistersOnly
}

// registerInlineDescribedGlobalShapes populates the global reads and writes.
func registerInlineDescribedGlobalShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetGlobal)] = inlinePoolShape{bKindByte: poolGlobals}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetGlobal)] = inlinePoolShape{bKindByte: poolGlobals}
	wide := inlinePoolShape{extAWide16: poolGlobals, hasExtensionWord: true, describedExtension: true}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpGetGlobalWide)] = wide
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpSetGlobalWide)] = wide
}
