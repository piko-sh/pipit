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

const (
	// NumInstructionOperands is the fixed number of single-byte operands (a, b, c) carried
	// by every instruction in the four-byte instruction encoding. Tables that span operand
	// positions have this length.
	NumInstructionOperands = 3
)

const (
	// RoleNone marks an unused operand byte.
	RoleNone OperandRole = iota

	// RoleRegInt designates the operand as an int-bank register index.
	RoleRegInt

	// RoleRegFloat designates the operand as a float-bank register index.
	RoleRegFloat

	// RoleRegString designates the operand as a string-bank register index.
	RoleRegString

	// RoleRegBool designates the operand as a bool-bank register index.
	RoleRegBool

	// RoleRegUint designates the operand as a uint-bank register index.
	RoleRegUint

	// RoleRegComplex designates the operand as a complex-bank register index.
	RoleRegComplex

	// RoleRegGeneral designates the operand as a general-bank register index.
	RoleRegGeneral

	// RoleRegSliceInt designates the operand as an app.SlicesInt-bank register index,
	// holding a typed []int64 slice header. Used by the typed slice opcodes
	// (isa.SubOpMakeSliceInt, OpSliceGetIntDirect, OpSliceSetIntDirect,
	// isa.SubOpLenSliceIntDirect, OpRangeNextSliceInt) that bypass reflect.Value entirely.
	RoleRegSliceInt

	// roleRegSliceFloat designates the operand as a slicesFloat-bank register index, holding
	// a typed []float64 slice header.
	roleRegSliceFloat

	// roleRegSliceString designates the operand as a slicesString-bank register index,
	// holding a typed []string slice header.
	roleRegSliceString

	// roleRegSliceBool designates the operand as a slicesBool-bank register index, holding a
	// typed []bool slice header.
	roleRegSliceBool

	// roleRegSliceUint designates the operand as a slicesUint-bank register index, holding a
	// typed []uint64 slice header.
	roleRegSliceUint

	// roleRegSliceByte designates the operand as a slicesByte-bank register index, holding a
	// typed []byte slice header.
	roleRegSliceByte

	// RoleRegDynamic marks a register operand whose bank is selected at runtime by another
	// operand or a following extension word. Used by ops such as OpGetUpvalue,
	// isa.SubOpLoadZero and OpAddr; the funnel treats these as opaque and the verifier
	// widens them to "any bank".
	RoleRegDynamic

	// roleConstIndex marks an operand that indexes into a constant pool.
	roleConstIndex

	// roleFieldIndex marks an operand that selects a struct field by position.
	roleFieldIndex

	// RoleImmediate marks a small inline value (e.g. byte literals, flag bits).
	RoleImmediate

	// roleTypeIndex marks an operand that indexes into the function's type table.
	roleTypeIndex

	// RoleKindMarker marks an operand that names a RegisterKind value (used by ops like
	// OpPackInterface and OpUnpackInterface where C names the bank for B or A).
	RoleKindMarker

	// roleSubOpcode marks an operand that names a SubOpcode value dispatched by
	// OpDrillTier1. The verifier treats this as an immediate-style classifier rather than a
	// register reference.
	roleSubOpcode

	// roleJumpOffsetLow marks the low byte of a 16-bit signed jump offset.
	roleJumpOffsetLow

	// roleJumpOffsetHigh marks the high byte of a 16-bit signed jump offset paired with
	// roleJumpOffsetLow.
	roleJumpOffsetHigh

	// roleCallSiteLow marks the low byte of a call-site index.
	roleCallSiteLow

	// roleCallSiteHigh marks the high byte of a call-site index paired with roleCallSiteLow.
	roleCallSiteHigh

	// roleFollowsExtension marks an operand whose meaning is supplied by the following OpExt
	// word; the verifier treats both bytes as opaque.
	roleFollowsExtension

	// roleUnknown marks an opcode the table does not describe; treated as opaque (no
	// constraints, no writes recorded) so undescribed opcodes do not block verification of
	// the rest of the function.
	roleUnknown
)

const (
	// ShapeFlagDescribed marks the descriptor as authoritative. Without it, the verifier
	// treats the opcode as opaque (matching roleUnknown per-operand entries).
	ShapeFlagDescribed shapeFlag = 1 << iota

	// ShapeFlagFollowsExtension marks an opcode that consumes one or more OpExt words after
	// it. The verifier skips those words rather than treating them as standalone
	// instructions.
	ShapeFlagFollowsExtension

	// ShapeFlagControlFlow marks ops that change the program counter in non-fallthrough ways
	// (jumps, calls, returns, panics). The verifier uses this to build the CFG.
	ShapeFlagControlFlow

	// ShapeFlagTerminator marks ops that end a basic block by always transferring control
	// elsewhere (return, panic, jump).
	ShapeFlagTerminator

	// ShapeFlagOpaqueWrites marks an operation that writes registers its operand bytes do
	// not name, so any analysis must treat the operation as potentially writing any
	// register.
	ShapeFlagOpaqueWrites
)

// OperandRole classifies what an instruction operand byte means.
type OperandRole uint8

// OperandShape describes the per-operand role for one opcode.
type OperandShape struct {
	// A is the role for operand byte A.
	A OperandRole

	// B is the role for operand byte B.
	B OperandRole

	// C is the role for operand byte C.
	C OperandRole

	// Reads marks each operand that reads its named register.
	Reads [NumInstructionOperands]bool

	// Writes marks each operand that writes its named register.
	Writes [NumInstructionOperands]bool

	// Flags holds whole-instruction shape flags (control flow, extension, terminator, etc.).
	Flags shapeFlag
}

var (
	// operandShapes maps each operation to its operand-shape descriptor, indexed by encoding
	// tier and then by the operation's code within that tier.
	operandShapes [tierCount][SlotsPerTier]OperandShape
)

// shapeFlag bitmasks describe whole-instruction properties that individual operand roles
// cannot express.
type shapeFlag uint8

// OperandShapeAt returns the operand-shape descriptor for an operation at a tier, giving
// back the opaque zero shape for an undescribed slot rather than panicking.
//
// Takes tier (Tier) which is the encoding depth the operation lives at.
// Takes code (uint8) which is the operation's value within that tier.
//
// Returns OperandShape which is the descriptor, or the opaque zero shape.
func OperandShapeAt(tier Tier, code uint8) OperandShape {
	if int(tier) >= len(operandShapes) {
		return OperandShape{}
	}
	return operandShapes[tier][code]
}

// OperandShapeFor returns the operand-shape descriptor for a tier-0 opcode.
//
// Takes op (Opcode) which is the opcode byte to describe.
//
// Returns OperandShape which is the descriptor, or the opaque zero shape for undescribed
// opcodes.
func OperandShapeFor(op Opcode) OperandShape {
	return OperandShapeAt(TierMain, uint8(op))
}

// ShapeForInstruction resolves the drill cascade and returns the shape of the operation
// the instruction actually executes.
//
// Takes instr (Instruction) which is the encoded instruction word.
//
// Returns OperandShape which is the descriptor, or the opaque zero shape.
func ShapeForInstruction(instr Instruction) OperandShape {
	if instr.Op != OpDrillTier1 {
		return OperandShapeAt(TierMain, uint8(instr.Op))
	}
	if SubOpcode(instr.A) != SubOpDrillTier2 {
		return OperandShapeAt(TierSub1, instr.A)
	}
	if SubOpcodeTier2(instr.B) != SubOpTier2DrillTier3 {
		return OperandShapeAt(TierSub2, instr.B)
	}
	return OperandShapeAt(TierSub3, instr.C)
}

// KindForRole returns the RegisterKind expected by a register-shaped operand role. The
// boolean is false for non-register roles.
//
// Takes role (OperandRole) which is the operand's classification.
//
// Returns the corresponding register bank kind and true when the role is register-shaped,
// otherwise zero and false.
func KindForRole(role OperandRole) (RegisterKind, bool) {
	switch role {
	case RoleRegInt:
		return RegisterInt, true
	case RoleRegFloat:
		return RegisterFloat, true
	case RoleRegString:
		return RegisterString, true
	case RoleRegBool:
		return RegisterBool, true
	case RoleRegUint:
		return RegisterUint, true
	case RoleRegComplex:
		return RegisterComplex, true
	case RoleRegGeneral:
		return RegisterGeneral, true
	case RoleRegSliceInt:
		return RegisterSliceInt, true
	case roleRegSliceFloat:
		return RegisterSliceFloat, true
	case roleRegSliceString:
		return RegisterSliceString, true
	case roleRegSliceBool:
		return RegisterSliceBool, true
	case roleRegSliceUint:
		return RegisterSliceUint, true
	case roleRegSliceByte:
		return RegisterSliceByte, true
	default:
	}
	return 0, false
}

// RoleIndexesPerFunctionTable reports whether an operand role names an index into a
// per-function table that must be renumbered when splicing bytecode across function
// boundaries.
//
// Takes role (OperandRole) which is the operand's classification.
//
// Returns true for the constant-pool and type-table index roles.
func RoleIndexesPerFunctionTable(role OperandRole) bool {
	switch role {
	case roleConstIndex, roleTypeIndex:
		return true
	default:
		return false
	}
}

// RoleForKind returns the OperandRole that names a register operand of the given bank.
//
// Takes kind (RegisterKind) which is the bank to convert.
//
// Returns the matching OperandRole.
func RoleForKind(kind RegisterKind) OperandRole {
	switch kind {
	case RegisterInt:
		return RoleRegInt
	case RegisterFloat:
		return RoleRegFloat
	case RegisterString:
		return RoleRegString
	case RegisterBool:
		return RoleRegBool
	case RegisterUint:
		return RoleRegUint
	case RegisterComplex:
		return RoleRegComplex
	case RegisterSliceInt:
		return RoleRegSliceInt
	case RegisterSliceFloat:
		return roleRegSliceFloat
	case RegisterSliceString:
		return roleRegSliceString
	case RegisterSliceBool:
		return roleRegSliceBool
	case RegisterSliceUint:
		return roleRegSliceUint
	case RegisterSliceByte:
		return roleRegSliceByte
	default:

		return RoleRegGeneral
	}
}

// described3 records a three-operand opcode with separate roles per position.
// ShapeFlagDescribed is added automatically.
//
// Takes op (Opcode) which is the opcode being described.
// Takes ra (OperandRole) which is the role for operand A.
// Takes rb (OperandRole) which is the role for operand B.
// Takes rc (OperandRole) which is the role for operand C.
// Takes reads ([NumInstructionOperands]bool) which marks register read accesses per
// position.
// Takes writes ([NumInstructionOperands]bool) which marks register write accesses per
// position.
// Takes flags (shapeFlag) which adds extra whole-instruction flags.
func described3(op Opcode, ra, rb, rc OperandRole, reads, writes [NumInstructionOperands]bool, flags shapeFlag) {
	operandShapes[TierMain][uint8(op)] = OperandShape{
		A: ra, B: rb, C: rc,
		Reads:  reads,
		Writes: writes,
		Flags:  flags | ShapeFlagDescribed,
	}
}

// describedSub1 records a tier-1 operation. Operand A holds the sub-opcode, so only B and
// C carry operands and the A slot is always RoleNone with neither read nor write set.
//
// Takes op (SubOpcode) which is the operation being described.
// Takes rb (OperandRole) which is the role for operand B.
// Takes rc (OperandRole) which is the role for operand C.
// Takes reads ([NumInstructionOperands]bool) which marks register read accesses per
// position; index 0 must stay false.
// Takes writes ([NumInstructionOperands]bool) which marks register write accesses per
// position; index 0 must stay false.
// Takes flags (shapeFlag) which adds extra whole-instruction flags.
func describedSub1(op SubOpcode, rb, rc OperandRole, reads, writes [NumInstructionOperands]bool, flags shapeFlag) {
	operandShapes[TierSub1][uint8(op)] = OperandShape{
		A: RoleNone, B: rb, C: rc,
		Reads:  reads,
		Writes: writes,
		Flags:  flags | ShapeFlagDescribed,
	}
}

// describedSub2 records a tier-2 operation. Operands A and B hold drill markers, so only
// C carries an operand.
//
// Takes op (SubOpcodeTier2) which is the operation being described.
// Takes rc (OperandRole) which is the role for operand C.
// Takes readsC (bool) which marks whether operand C is read.
// Takes writesC (bool) which marks whether operand C is written.
// Takes flags (shapeFlag) which adds extra whole-instruction flags.
func describedSub2(op SubOpcodeTier2, rc OperandRole, readsC, writesC bool, flags shapeFlag) {
	operandShapes[TierSub2][uint8(op)] = OperandShape{
		A: RoleNone, B: RoleNone, C: rc,
		Reads:  [NumInstructionOperands]bool{false, false, readsC},
		Writes: [NumInstructionOperands]bool{false, false, writesC},
		Flags:  flags | ShapeFlagDescribed,
	}
}

// describedSub3 records a tier-3 operation. Every byte is a discriminator, so no operand
// role is carried.
//
// Takes op (SubOpcodeTier3) which is the operation being described.
// Takes flags (shapeFlag) which adds extra whole-instruction flags.
func describedSub3(op SubOpcodeTier3, flags shapeFlag) {
	operandShapes[TierSub3][uint8(op)] = OperandShape{
		A: RoleNone, B: RoleNone, C: RoleNone,
		Reads:  [NumInstructionOperands]bool{false, false, false},
		Writes: [NumInstructionOperands]bool{false, false, false},
		Flags:  flags | ShapeFlagDescribed,
	}
}

// describeDstSrcSrc records the canonical "writes A, reads B and C" pattern where all
// three operands share the same register kind.
//
// Takes op (Opcode) which is the opcode being described.
// Takes kind (OperandRole) which is the shared register role.
func describeDstSrcSrc(op Opcode, kind OperandRole) {
	described3(op, kind, kind, kind,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// describeDstSrcSrcMixed records "writes A in destinationKind, reads B and C in
// sourceKind" (e.g. comparisons that return int but read float).
//
// Takes op (Opcode) which is the opcode being described.
// Takes destination (OperandRole) which is the destination role.
// Takes source (OperandRole) which is the shared source role.
func describeDstSrcSrcMixed(op Opcode, destination, source OperandRole) {
	described3(op, destination, source, source,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// describeLoadConst records "writes A from a wide constant index in B|(C<<8)".
//
// Takes op (Opcode) which is the opcode being described.
// Takes destination (OperandRole) which is the destination register role.
func describeLoadConst(op Opcode, destination OperandRole) {
	described3(op, destination, roleConstIndex, roleConstIndex,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// describeArithConst records "writes A, reads B, takes constant index in C".
//
// Takes op (Opcode) which is the opcode being described.
// Takes kind (OperandRole) which is the register role for both A and B.
func describeArithConst(op Opcode, kind OperandRole) {
	described3(op, kind, kind, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// describeJumpCondition records "reads A, jump offset in B|(C<<8)".
//
// Takes op (Opcode) which is the opcode being described.
// Takes source (OperandRole) which is the role for the test register.
func describeJumpCondition(op Opcode, source OperandRole) {
	described3(op, source, roleJumpOffsetLow, roleJumpOffsetHigh,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagControlFlow)
}

// describeSub1DstSrc records the tier-1 "writes B, reads C" pattern.
//
// Takes op (SubOpcode) which is the sub-opcode being described.
// Takes destination (OperandRole) which is the destination role.
// Takes source (OperandRole) which is the source role.
func describeSub1DstSrc(op SubOpcode, destination, source OperandRole) {
	describedSub1(op, destination, source,
		[NumInstructionOperands]bool{false, false, true},
		[NumInstructionOperands]bool{false, true, false},
		0)
}

// describeSub1DstSrcExtension records a tier-1 "B = f(C)" operation whose layout or index
// travels in the following OpExt word.
//
// Takes op (SubOpcode) which is the sub-opcode being described.
// Takes destination (OperandRole) which is the role of the B operand written.
// Takes source (OperandRole) which is the role of the C operand read.
func describeSub1DstSrcExtension(op SubOpcode, destination, source OperandRole) {
	describedSub1(op, destination, source,
		[NumInstructionOperands]bool{false, false, true},
		[NumInstructionOperands]bool{false, true, false},
		ShapeFlagFollowsExtension)
}

// describeSub1ConstJump records a tier-1 fused compare-against-constant-and-branch:
// "reads B, C is the constant index, OpExt follows for the jump offset".
//
// Takes op (SubOpcode) which is the sub-opcode being described.
// Takes source (OperandRole) which is the role for the test register.
func describeSub1ConstJump(op SubOpcode, source OperandRole) {
	describedSub1(op, source, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagControlFlow|ShapeFlagFollowsExtension)
}

// describeSub1RegRegJump records a tier-1 fused compare-and-branch: "reads B and C as
// registers of role source, OpExt follows for the jump offset". Used by the fused
// reg-reg-compare-and-jump sub-ops such as SubOpLtIntJumpFalse.
//
// Takes op (SubOpcode) which is the sub-opcode being described.
// Takes source (OperandRole) which is the role for both operand registers.
func describeSub1RegRegJump(op SubOpcode, source OperandRole) {
	describedSub1(op, source, source,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagControlFlow|ShapeFlagFollowsExtension)
}

// describeTypedSliceGet records "writes A as destinationKind, reads B as general (the
// slice), reads C as int (the index)".
//
// Takes op (Opcode) which is the opcode being described.
// Takes destination (OperandRole) which is the destination register role.
func describeTypedSliceGet(op Opcode, destination OperandRole) {
	described3(op, destination, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// describeTypedSliceSet records "reads A as general (the slice), reads B as int (index),
// reads C as srcKind (the value)".
//
// Takes op (Opcode) which is the opcode being described.
// Takes source (OperandRole) which is the value register role.
func describeTypedSliceSet(op Opcode, source OperandRole) {
	described3(op, RoleRegGeneral, RoleRegInt, source,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

func init() {
	populateScalarArithShapes()
	populateGenericArithShapes()
	populateComparisonShapes()
	populateConversionMoveShapes()
	populateConstantLoadShapes()
	populateJumpAndFusedShapes()
	populateMathShapes()
	populateCollectionShapes()
	populateTypeSwitchJumpShapes()
	populateAddressTypeShapes()
	populateGlobalUnsafeShapes()
	populateStringShapes()
	populateStringIntrinsicShapes()
	populateComplexAndFusedStringShapes()
	populateSliceAndAppendShapes()
	populateTypedSliceDirectShapes()
	populateMapAndFieldShapes()
	populateUpvalueAndConvertShapes()
	populateTerminatorShapes()
	populateCallAndFrameShapes()
	populateSubTierShapes()
}

// populateScalarArithShapes describes integer, float, uint, and complex arithmetic and
// bitwise opcodes that operate on a single register bank.
func populateScalarArithShapes() {
	describeDstSrcSrc(OpAddInt, RoleRegInt)
	describeDstSrcSrc(OpSubInt, RoleRegInt)
	describeDstSrcSrc(OpMulInt, RoleRegInt)
	describeDstSrcSrc(OpDivInt, RoleRegInt)
	describeDstSrcSrc(OpRemInt, RoleRegInt)
	describeDstSrcSrc(OpBitAnd, RoleRegInt)
	describeDstSrcSrc(OpBitOr, RoleRegInt)
	describeDstSrcSrc(OpBitXor, RoleRegInt)
	describeDstSrcSrc(OpBitAndNot, RoleRegInt)
	describeDstSrcSrc(OpShiftLeft, RoleRegInt)
	describeDstSrcSrc(OpShiftRight, RoleRegInt)
	describeDstSrcSrc(OpAddFloat, RoleRegFloat)
	describeDstSrcSrc(OpSubFloat, RoleRegFloat)
	describeDstSrcSrc(OpMulFloat, RoleRegFloat)
	describeDstSrcSrc(OpDivFloat, RoleRegFloat)
	describeDstSrcSrc(OpAddUint, RoleRegUint)
	describeDstSrcSrc(OpSubUint, RoleRegUint)
	describeDstSrcSrc(OpMulUint, RoleRegUint)
	describeDstSrcSrc(OpDivUint, RoleRegUint)
	describeDstSrcSrc(OpRemUint, RoleRegUint)
	describeDstSrcSrc(OpBitAndUint, RoleRegUint)
	describeDstSrcSrc(OpBitOrUint, RoleRegUint)
	describeDstSrcSrc(OpBitXorUint, RoleRegUint)
	describeDstSrcSrc(OpBitAndNotUint, RoleRegUint)
	describeDstSrcSrc(OpShiftLeftUint, RoleRegUint)
	describeDstSrcSrc(OpShiftRightUint, RoleRegUint)
	describeDstSrcSrc(OpAddComplex, RoleRegComplex)
	describeDstSrcSrc(OpSubComplex, RoleRegComplex)
	describeDstSrcSrc(OpMulComplex, RoleRegComplex)
	describeDstSrcSrc(OpDivComplex, RoleRegComplex)
}

// populateGenericArithShapes describes the reflect-based arithmetic opcodes that operate
// on the general register bank.
func populateGenericArithShapes() {
	describeDstSrcSrc(OpAdd, RoleRegGeneral)
	describeDstSrcSrc(OpSub, RoleRegGeneral)
	describeDstSrcSrc(OpMul, RoleRegGeneral)
	describeDstSrcSrc(OpDiv, RoleRegGeneral)
	describeDstSrcSrc(OpRem, RoleRegGeneral)
}

// populateComparisonShapes describes the comparison opcodes that produce an int
// (boolean-as-int) result from typed-bank reads.
func populateComparisonShapes() {
	describeDstSrcSrcMixed(OpEqInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpNeInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpLtInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpLeInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpGtInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpGeInt, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpEqFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpNeFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpLtFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpLeFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpGtFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpGeFloat, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpEqString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpNeString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpLtString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpLeString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpGtString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpGeString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpEqUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpNeUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpLtUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpLeUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpGtUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpGeUint, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpEqComplex, RoleRegInt, RoleRegComplex)
	describeDstSrcSrcMixed(OpNeComplex, RoleRegInt, RoleRegComplex)
	describeDstSrcSrcMixed(OpEqGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpNeGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpLtGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpLeGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpGtGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpGeGeneral, RoleRegInt, RoleRegGeneral)
}

// populateConversionMoveShapes describes type-converting moves between scalar register
// banks plus same-bank moves and the cross-bank pack/unpack opcodes.
func populateConversionMoveShapes() {
	described3(OpTruncateNarrow, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)

	described3(OpMoveGeneral, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpPackInterface, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpPackTyped, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpUnpackInterface, RoleRegDynamic, RoleRegGeneral, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateConstantLoadShapes describes the opcodes that materialise constants into typed
// register banks.
func populateConstantLoadShapes() {
	describeLoadConst(OpLoadIntConst, RoleRegInt)
	describeLoadConst(OpLoadFloatConst, RoleRegFloat)
	describeLoadConst(OpLoadStringConst, RoleRegString)
	describeLoadConst(OpLoadGeneralConst, RoleRegGeneral)
	describeLoadConst(OpLoadCompositeZeroReuse, RoleRegGeneral)
	describeLoadConst(OpLoadUintConst, RoleRegUint)
	describeLoadConst(OpLoadComplexConst, RoleRegComplex)
}

// populateJumpAndFusedShapes describes unconditional jumps, conditional branches, and
// fused arithmetic-and-jump opcodes.
func populateJumpAndFusedShapes() {
	describeArithConst(OpSubIntConst, RoleRegInt)
	describeArithConst(OpAddIntConst, RoleRegInt)
	describeArithConst(OpMulIntConst, RoleRegInt)
	described3(OpAddIntJump, RoleRegInt, RoleRegInt, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	describeJumpCondition(OpJumpIfTrue, RoleRegInt)
	describeJumpCondition(OpJumpIfFalse, RoleRegInt)
	describeJumpCondition(OpTestNilJumpTrue, RoleRegGeneral)
	describeJumpCondition(OpTestNilJumpFalse, RoleRegGeneral)
}

// populateMathShapes describes the math intrinsic opcodes that operate on float
// registers, plus the umbrella opcode that dispatches to many cold-path handlers via a
// sub-op discriminator.
func populateMathShapes() {
	describeDstSrcSrc(OpMathPow, RoleRegFloat)

	described3(OpDrillTier1, roleSubOpcode, RoleRegDynamic, RoleRegDynamic,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{false, true, false},
		0)
}

// populateStringShapes describes basic string operations and strconv formatters that
// produce string-bank values from typed-bank inputs.
func populateStringShapes() {
	described3(OpStringIndex, RoleRegUint, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStringIndexToInt, RoleRegInt, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrc(OpConcatString, RoleRegString)
	described3(OpConcatRuneString, RoleRegString, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateStringIntrinsicShapes describes the strings package intrinsic opcodes
// (Contains, ToUpper, etc.).
func populateStringIntrinsicShapes() {
	described3(OpStrContainsRune, RoleRegBool, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrcMixed(OpStrContains, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStrHasPrefix, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStrHasSuffix, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStrEqualFold, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStrIndex, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpStrCount, RoleRegInt, RoleRegString)
	describeDstSrcSrc(OpStrTrimPrefix, RoleRegString)
	describeDstSrcSrc(OpStrTrimSuffix, RoleRegString)
	describeDstSrcSrc(OpStrTrim, RoleRegString)
	described3(OpStrIndexRune, RoleRegInt, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStrRepeat, RoleRegString, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrcMixed(OpStrLastIndex, RoleRegInt, RoleRegString)
	described3(OpStrJoin, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStrSplit, RoleRegGeneral, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStrReplaceAll, RoleRegString, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateComplexAndFusedStringShapes describes complex constructors, accessors, and
// fused string-and-jump opcodes.
func populateComplexAndFusedStringShapes() {
	describeDstSrcSrcMixed(OpBuildComplex, RoleRegComplex, RoleRegFloat)
	described3(OpSliceString, RoleRegString, RoleRegString, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateSliceAndAppendShapes describes typed slice get/set and typed append opcodes.
func populateSliceAndAppendShapes() {
	describeTypedSliceGet(OpSliceGetInt, RoleRegInt)
	describeTypedSliceGet(OpSliceGetFloat, RoleRegFloat)
	describeTypedSliceGet(OpSliceGetString, RoleRegString)
	describeTypedSliceGet(OpSliceGetBool, RoleRegBool)
	describeTypedSliceGet(OpSliceGetUint, RoleRegUint)
	describeTypedSliceGet(OpSliceGetIntUnchecked, RoleRegInt)
	describeTypedSliceSet(OpSliceSetInt, RoleRegInt)
	describeTypedSliceSet(OpSliceSetFloat, RoleRegFloat)
	describeTypedSliceSet(OpSliceSetString, RoleRegString)
	describeTypedSliceSet(OpSliceSetBool, RoleRegBool)
	describeTypedSliceSet(OpSliceSetUint, RoleRegUint)
	describeTypedSliceSet(OpSliceSetIntUnchecked, RoleRegInt)
}

// populateTypedSliceDirectShapes describes typed direct-storage slice opcodes that
// operate against the app.SlicesInt bank without reflect.
func populateTypedSliceDirectShapes() {
	described3(OpSliceGetIntDirect, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSliceSetIntDirect, RoleRegSliceInt, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpRangeNextSliceInt, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	described3(OpRangeNextSliceByte, RoleRegInt, roleRegSliceByte, RoleRegUint,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	described3(OpSliceGetIntDirectUnchecked, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSliceSetIntDirectUnchecked, RoleRegSliceInt, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateMapAndFieldShapes describes typed map opcodes and typed field accesses.
func populateMapAndFieldShapes() {
	populateTypedMapShapes()
	populateGenericFieldShapes()
	populateStructFieldShapes()
	populateSliceIndexStructFieldShapes()
}

// populateTypedMapShapes describes opcodes that access maps keyed and valued by typed
// registers (int, string).
func populateTypedMapShapes() {
	described3(OpMapGetIntInt, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapSetIntInt, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapGetStringInt, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapSetStringInt, RoleRegGeneral, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)

	described3(OpMapSetIntGeneral, RoleRegGeneral, RoleRegInt, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapSetStringGeneral, RoleRegGeneral, RoleRegString, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapAddIntInt, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapAddStringInt, RoleRegGeneral, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapGetStringString, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapSetStringString, RoleRegGeneral, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapGetIntString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapSetIntString, RoleRegGeneral, RoleRegInt, RoleRegString,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateGenericFieldShapes describes the generic typed-int field accessors and the
// dynamic get/set field opcodes.
func populateGenericFieldShapes() {
	described3(OpGetFieldInt, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetFieldInt, RoleRegGeneral, roleFieldIndex, RoleRegInt,
		[NumInstructionOperands]bool{true, false, true},
		[NumInstructionOperands]bool{false, false, false},
		0)

	described3(OpGetField, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetField, RoleRegGeneral, roleFieldIndex, RoleRegGeneral,
		[NumInstructionOperands]bool{true, false, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateStructFieldShapes describes the typed tier-0 struct-field get and set opcodes
// plus the in-place swap.
func populateStructFieldShapes() {
	described3(OpGetStructFieldIntT0, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldIntT0, RoleRegGeneral, RoleRegInt, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldUint, RoleRegUint, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldUint, RoleRegGeneral, RoleRegUint, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldFloat, RoleRegFloat, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldFloat, RoleRegGeneral, RoleRegFloat, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldBool, RoleRegBool, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldBool, RoleRegGeneral, RoleRegBool, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetStructFieldRawPointerT0, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	populateFusedStructFieldCopyShape()
	described3(OpSwapStructFieldsGeneralT0, RoleRegGeneral, roleFieldIndex, roleFieldIndex,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateSliceIndexStructFieldShapes describes the typed fused
// slice-index-of-struct-field super-instructions for every register family.
func populateSliceIndexStructFieldShapes() {
	described3(OpSliceIndexStructFieldInt, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSliceIndexStructFieldUint, RoleRegUint, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSliceIndexStructFieldFloat, RoleRegFloat, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSliceIndexStructFieldBool, RoleRegBool, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSliceIndexStructFieldString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateUpvalueAndConvertShapes describes upvalue access and the remaining conversion
// opcodes that touch the general bank.
func populateUpvalueAndConvertShapes() {
	described3(OpGetUpvalue, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetUpvalue, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateTerminatorShapes describes returns, panics, and the trivial nop and ext
// opcodes.
func populateTerminatorShapes() {
	described3(OpExt, RoleImmediate, RoleImmediate, RoleImmediate,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateCollectionShapes describes the make/index/append/len/cap opcodes that operate
// on slice/map/channel general-bank values.
func populateCollectionShapes() {
	described3(OpMakeSlice, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)

	described3(OpIndex, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpDerefSliceGetInt, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpDerefSliceSetInt, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)

	described3(OpGetStructFieldIndexGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSetStructFieldIndexGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
	described3(OpIndexSet, RoleRegGeneral, RoleRegInt, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapIndex, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapSet, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpMapIndexOk, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	populateTypedMapIndexOkShapes()
	populateAppendShapes()
	describeDstSrcSrcMixed(OpCopy, RoleRegInt, RoleRegGeneral)
	described3(OpChannelSend, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateAppendShapes describes the append opcode family: the generic append, the byte
// and typed fast paths, and the in-place variants that reuse the destination header.
func populateAppendShapes() {
	described3(OpAppend, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpAppendByteFast, RoleRegGeneral, RoleRegGeneral, RoleRegUint,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	populateFastAppendShapes()
	described3(OpAppendByteFastInPlace, RoleRegGeneral, RoleRegGeneral, RoleRegUint,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendInPlace, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendSpreadInPlace, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendSpread, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateFusedStructFieldCopyShape describes the fused get/set pair produced by
// FuseCopyStructFieldGeneralT0.
func populateFusedStructFieldCopyShape() {
	described3(OpCopyStructFieldGeneralT0, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
}

// populateTypedMapIndexOkShapes describes the typed map-comma-ok opcodes used by the
// compiler when both the key kind and value kind match a primitive register bank,
// eliminating the boxing that the general-bank OpMapIndexOk path otherwise pays.
func populateTypedMapIndexOkShapes() {
	described3(OpMapIndexOkIntInt, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkStringInt, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkStringString, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkIntString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapGetIntGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapIndexOkIntGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	populateStringKeyedGeneralMapShapes()
	described3(OpMapIndexOkJumpIfFalseIntInt, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkJumpIfFalseStringInt, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkJumpIfFalseStringString, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkJumpIfFalseIntString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkJumpIfFalseIntGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpMapIndexOkJumpIfFalseStringGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
}

// populateAddressTypeShapes describes address-of, dereference, type-assertion, and
// reflect-driven indirection opcodes.
func populateAddressTypeShapes() {
	described3(OpAddr, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, true, false},
		0)

	described3(OpDeref, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAllocIndirect, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpTypeAssert, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension)
	described3(OpConvert, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpBindMethod, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpMakeClosure, RoleRegGeneral, roleConstIndex, roleConstIndex,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateGlobalUnsafeShapes describes package-level variable access and the
// unsafe-package intrinsic opcodes.
func populateGlobalUnsafeShapes() {
	described3(OpGetGlobal, RoleRegDynamic, roleConstIndex, RoleKindMarker,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetGlobal, RoleRegDynamic, roleConstIndex, RoleKindMarker,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpUnsafeString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpUnsafeSlice, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpUnsafeAdd, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateFastAppendShapes describes the direct-exit append opcodes: the struct form
// carries its element type in an extension word, the scalar forms read their element from
// the matching typed bank.
func populateFastAppendShapes() {
	described3(OpAppendStructFast, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpAppendIntFast, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendFloatFast, RoleRegGeneral, RoleRegGeneral, RoleRegFloat,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendStringFast, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateStringKeyedGeneralMapShapes describes the string-keyed general-valued map reads
// and comma-ok variants.
func populateStringKeyedGeneralMapShapes() {
	described3(OpMapGetStringGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpMapIndexOkStringGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
}

// populateCallAndFrameShapes describes the call, defer/go, shared-cell, range and select
// operations.
func populateCallAndFrameShapes() {
	described3(OpDefer, RoleRegGeneral, RoleImmediate, RoleImmediate,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0|ShapeFlagOpaqueWrites)
	described3(OpGo, RoleRegGeneral, RoleImmediate, RoleImmediate,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0|ShapeFlagOpaqueWrites)

	described3(OpGetStructFieldSliceLen, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetStructFieldSliceIndexScalar, RoleRegDynamic, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// describeCallSite records a tier-1 call sub-op whose whole payload is the 16-bit
// call-site index in B|(C<<8).
//
// Takes op (SubOpcode) which is the call sub-opcode being described.
// Takes flags (shapeFlag) which adds extra whole-instruction flags.
func describeCallSite(op SubOpcode, flags shapeFlag) {
	describedSub1(op, roleCallSiteLow, roleCallSiteHigh,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{false, false, false},
		flags|ShapeFlagOpaqueWrites)
}
