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
	// TypeAssert, AllocIndirect).
	poolTypeTable

	// poolStructLayoutTable references the function's struct-layout table (used by the
	// tier-0 struct-field ops).
	poolStructLayoutTable

	// poolCallSites references the function's call-site table.
	poolCallSites

	// poolFunctions references the program-wide function table.
	poolFunctions
)

// inlinePoolShape records the per-opcode pool descriptor used by the inliner. Only
// opcodes with a non-register, non-jump-offset operand need an entry.
//
//exhaustruct:ignore
type inlinePoolShape struct {
	// bKindByte names the pool whose uint8 index lives in operand B alone when nonzero (e.g.
	// opLoadBoolConst).
	bKindByte inlinePool

	// cKindByte names the pool whose uint8 index lives in operand C of the main instruction
	// when nonzero (e.g. tier-0 struct field T0 ops).
	cKindByte inlinePool

	// bcWide16 names the pool whose uint16 little-endian index is formed by operands B and C
	// together (b|c<<8) when nonzero (e.g. opLoadIntConst).
	bcWide16 inlinePool

	// extAWide16 names the pool whose uint16 index lives in operands A and B of the
	// following opExt word when nonzero. Used by ops like opTypeAssert that follow with
	// `opExt typeIndexLow typeIndexHigh _`.
	extAWide16 inlinePool

	// hasExtensionWord is true when this opcode consumes one opExt instruction following it
	// in the bytecode stream. The inliner must walk and (when applicable) remap that
	// extension when iterating the body.
	hasExtensionWord bool

	// extARegBank names the bank for an ext-word operand A register reference (used by
	// opMapIndexOk* variants whose extension A is `okRegister`, an int register; registerInt
	// = 0 conflicts with the zero default so extARegSet acts as the discriminator).
	extARegBank isa.RegisterKind

	// extARegSet is true when extARegBank carries a meaningful value (needed because
	// registerInt = 0).
	extARegSet bool
}

var (
	// inlinePoolShapes maps each operation to its pool descriptor, keyed by the flat (tier,
	// code) slot so an operation that moves between tiers keeps its descriptor.
	inlinePoolShapes [isa.FlatDispatchSlots]inlinePoolShape
)

func init() {
	registerInlineLoadConstShapes()
	registerInlineConstFusedArithShapes()
	registerInlineStructFieldShapes()
	registerInlineExtensionTypeTableShapes()
	registerInlineMapIndexOkShapes()
	registerInlineExtensionPassthroughShapes()
	registerInlineFusedJumpShapes()
	registerInlineCallShape()
}

// registerInlineLoadConstShapes populates wide-index constant load shapes (b|c<<8 ->
// respective constant pool) and the single-byte bool-constant pool index. The bool pool
// has at most 2 entries (true/false) but the callee's index can differ from the caller's
// after merge so the byte must be remapped via bKindByte.
func registerInlineLoadConstShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadIntConst)] = inlinePoolShape{bcWide16: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadFloatConst)] = inlinePoolShape{bcWide16: poolFloatConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadStringConst)] = inlinePoolShape{bcWide16: poolStringConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadUintConst)] = inlinePoolShape{bcWide16: poolUintConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadComplexConst)] = inlinePoolShape{bcWide16: poolComplexConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpLoadGeneralConst)] = inlinePoolShape{bcWide16: poolGeneralConsts}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpLoadBoolConst)] = inlinePoolShape{cKindByte: poolBoolConsts}
}

// registerInlineConstFusedArithShapes populates the const-fused arithmetic ops with the
// int-const pool index in operand C. The const-fused compare-and-jump ops are
// intentionally omitted: their extension-word encoding varies per opcode and is refused
// by the inliner.
func registerInlineConstFusedArithShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAddIntConst)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSubIntConst)] = inlinePoolShape{cKindByte: poolIntConsts}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMulIntConst)] = inlinePoolShape{cKindByte: poolIntConsts}
}

// registerInlineStructFieldShapes populates tier-0 struct field access ops; operand C is
// a uint8 structLayoutTable index for each.
func registerInlineStructFieldShapes() {
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldIntT0)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldIntT0)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldUint)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldUint)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldFloat)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldFloat)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldBool)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldBool)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpGetStructFieldGeneral)] = inlinePoolShape{cKindByte: poolStructLayoutTable}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpSetStructFieldGeneral)] = inlinePoolShape{cKindByte: poolStructLayoutTable}

	inlinePoolShapes[isa.FlatIndexOf(isa.OpCopyStructFieldGeneralT0)] = inlinePoolShape{
		cKindByte:        poolStructLayoutTable,
		extAWide16:       poolStructLayoutTable,
		hasExtensionWord: true,
	}
}

// registerInlineExtensionTypeTableShapes populates the ops whose extension word carries a
// typeTable index in ext A|B<<8. Each emits Emit(op, ...) then EmitExtension(typeIndex,
// hint).
func registerInlineExtensionTypeTableShapes() {
	typeTableShape := inlinePoolShape{
		extAWide16:       poolTypeTable,
		hasExtensionWord: true,
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMakeSlice)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpConvert)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpTypeAssert)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAllocIndirect)] = typeTableShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpPackTyped)] = typeTableShape
}

// registerInlineMapIndexOkShapes populates the isa.OpMapIndexOk variants. The main word
// is pure-register; the extension holds the okRegister (isa.RegisterInt) in operand A.
func registerInlineMapIndexOkShapes() {
	mapIndexShape := inlinePoolShape{
		hasExtensionWord: true,
		extARegBank:      isa.RegisterInt,
		extARegSet:       true,
	}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOk)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOkIntInt)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOkIntString)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOkIntGeneral)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOkStringInt)] = mapIndexShape
	inlinePoolShapes[isa.FlatIndexOf(isa.OpMapIndexOkStringString)] = mapIndexShape
}

// registerInlineExtensionPassthroughShapes populates ops whose extension word carries
// data that needs no remapping (append immediate length, global-wide high bits).
// hasExtensionWord is set so the body walker advances past the ext without
// misinterpreting it.
func registerInlineExtensionPassthroughShapes() {
	passthrough := inlinePoolShape{hasExtensionWord: true}
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAppend)] = passthrough
	inlinePoolShapes[isa.FlatIndexOf(isa.OpAppendSpread)] = passthrough
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpGetGlobalWide)] = passthrough
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpSetGlobalWide)] = passthrough
}

// registerInlineFusedJumpShapes populates the tier-1 compare-and-branch fusions. Each
// carries its jump offset in the following isa.OpExt word, which is copied verbatim, and
// the compare-against-constant forms also carry a byte-wide constant-pool index in C that
// the merge renumbers.
func registerInlineFusedJumpShapes() {
	intConstJump := inlinePoolShape{cKindByte: poolIntConsts, hasExtensionWord: true}
	for _, sub := range []isa.SubOpcode{
		isa.SubOpLeIntConstJumpFalse, isa.SubOpLtIntConstJumpFalse, isa.SubOpEqIntConstJumpFalse,
		isa.SubOpEqIntConstJumpTrue, isa.SubOpGeIntConstJumpFalse, isa.SubOpGtIntConstJumpFalse,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(sub)] = intConstJump
	}
	inlinePoolShapes[isa.FlatIndexOfSub1(isa.SubOpEqStringConstJumpFalse)] = inlinePoolShape{
		cKindByte:        poolStringConsts,
		hasExtensionWord: true,
	}
	registerJump := inlinePoolShape{hasExtensionWord: true}
	for _, sub := range []isa.SubOpcode{
		isa.SubOpLtIntJumpFalse, isa.SubOpLeIntJumpFalse, isa.SubOpGtIntJumpFalse,
		isa.SubOpGeIntJumpFalse, isa.SubOpEqIntJumpFalse, isa.SubOpNeIntJumpFalse,
	} {
		inlinePoolShapes[isa.FlatIndexOfSub1(sub)] = registerJump
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
