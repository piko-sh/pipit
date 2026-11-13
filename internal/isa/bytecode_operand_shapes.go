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
	// holding a typed []int64 slice header.
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
	// operand or a following extension word.
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
	// OpBoxAsInterface and OpUnboxFromInterface where C names the bank for B or A).
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

	// roleFollowsExtension marks an operand whose meaning is supplied by the following
	// OpExtension word. The verifier treats both bytes as opaque.
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

	// ShapeFlagFollowsExtension marks an opcode that may consume OpExtension words after it.
	//
	// The number of words varies, and a defer or go with no arguments carries none, so a
	// consumer such as the verifier skips every consecutive OpExtension word rather than a
	// fixed count, and never reads those words as standalone instructions.
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

// ReadsExtensionWord reports whether an instruction requires at least one extension word.
//
// A defer or a go statement carries one word per argument, and none when it has none.
// Every other instruction marked as followed by extension words reads the first one
// unconditionally, for its jump offset, an operand or a count.
//
// Takes instruction (Instruction) which is the instruction word.
//
// Returns bool which is true when the instruction needs an extension word after it.
func ReadsExtensionWord(instruction Instruction) bool {
	if ShapeForInstruction(instruction).Flags&ShapeFlagFollowsExtension == 0 {
		return false
	}
	return instruction.Op != OpDefer && instruction.Op != OpGo
}

// ScalarConstantOperand returns the constant-pool index a described instruction carries
// and the register kind whose pool it indexes.
//
// The index is wide, B|(C<<8), when both operands are constant indices, and the pool is
// that of the register A is loaded into. Otherwise it is C alone, and the pool is that of
// the register in B. Only the pools of scalar kinds are named. A constant index whose
// register is general may index a table other than the general constants, as a closure's
// does.
//
// Takes instruction (Instruction) which is the instruction word.
//
// Returns int which is the index.
// Returns RegisterKind which names the pool.
// Returns bool which is false when the instruction carries no scalar constant index.
func ScalarConstantOperand(instruction Instruction) (int, RegisterKind, bool) {
	shape := ShapeForInstruction(instruction)
	if shape.Flags&ShapeFlagDescribed == 0 || shape.C != roleConstIndex {
		return 0, 0, false
	}
	index, register := int(instruction.C), shape.B
	if shape.B == roleConstIndex {
		index, register = int(instruction.WideIndex()), shape.A
	}
	kind, ok := KindForRole(register)
	if !ok {
		return 0, 0, false
	}
	switch kind {
	case RegisterInt64, RegisterUint64, RegisterFloat64, RegisterString, RegisterBool, RegisterComplex128:
		return index, kind, true
	default:
		return 0, 0, false
	}
}

// CallSiteIndex returns the call-site index a call instruction carries in B|(C<<8), as
// the shape table marks it.
//
// Takes instr (Instruction) which is the instruction word.
//
// Returns the index, and false when the instruction carries no call-site index.
func CallSiteIndex(instr Instruction) (uint16, bool) {
	shape := ShapeForInstruction(instr)
	if shape.B != roleCallSiteLow || shape.C != roleCallSiteHigh {
		return 0, false
	}
	return instr.WideIndex(), true
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
// travels in the following OpExtension word.
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

// describeSub1ConstJump records a tier-1 fused compare-against-constant-and-branch with
// "reads B, C is the constant index, OpExtension follows for the jump offset".
//
// Takes op (SubOpcode) which is the sub-opcode being described.
// Takes source (OperandRole) which is the role for the test register.
func describeSub1ConstJump(op SubOpcode, source OperandRole) {
	describedSub1(op, source, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagControlFlow|ShapeFlagFollowsExtension)
}

// describeSub1RegRegJump records a tier-1 fused compare-and-branch with "reads B and C as
// registers of role source, OpExtension follows for the jump offset". Used by the fused
// reg-reg-compare-and-jump sub-ops such as SubOpLessThanInt64ThenJumpIfFalse.
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
	describeDstSrcSrc(OpAddInt64, RoleRegInt)
	describeDstSrcSrc(OpAddInt32, RoleRegInt)
	describeDstSrcSrc(OpSubtractInt32, RoleRegInt)
	describeDstSrcSrc(OpMultiplyInt32, RoleRegInt)
	describeArithConst(OpAddInt32Constant, RoleRegInt)
	describeArithConst(OpSubtractInt32Constant, RoleRegInt)
	describeDstSrcSrc(OpAddUint32, RoleRegUint)
	describeDstSrcSrc(OpSubtractUint32, RoleRegUint)
	describeDstSrcSrc(OpMultiplyUint32, RoleRegUint)
	describeDstSrcSrc(OpSubtractInt64, RoleRegInt)
	describeDstSrcSrc(OpMultiplyInt64, RoleRegInt)
	describeDstSrcSrc(OpDivideInt64, RoleRegInt)
	describeDstSrcSrc(OpRemainderInt64, RoleRegInt)
	describeDstSrcSrc(OpBitAndInt64, RoleRegInt)
	describeDstSrcSrc(OpBitOrInt64, RoleRegInt)
	describeDstSrcSrc(OpBitXorInt64, RoleRegInt)
	describeDstSrcSrc(OpBitAndNotInt64, RoleRegInt)
	describeDstSrcSrc(OpShiftLeftInt64, RoleRegInt)
	describeDstSrcSrc(OpShiftRightInt64, RoleRegInt)
	describeDstSrcSrc(OpAddFloat64, RoleRegFloat)
	describeDstSrcSrc(OpSubtractFloat64, RoleRegFloat)
	describeDstSrcSrc(OpMultiplyFloat64, RoleRegFloat)
	describeDstSrcSrc(OpDivideFloat64, RoleRegFloat)
	describeDstSrcSrc(OpAddUint64, RoleRegUint)
	describeDstSrcSrc(OpSubtractUint64, RoleRegUint)
	describeDstSrcSrc(OpMultiplyUint64, RoleRegUint)
	describeDstSrcSrc(OpDivideUint64, RoleRegUint)
	describeDstSrcSrc(OpRemainderUint64, RoleRegUint)
	describeDstSrcSrc(OpBitAndUint64, RoleRegUint)
	describeDstSrcSrc(OpBitOrUint64, RoleRegUint)
	describeDstSrcSrc(OpBitXorUint64, RoleRegUint)
	describeDstSrcSrc(OpBitAndNotUint64, RoleRegUint)
	describeDstSrcSrc(OpShiftLeftUint64, RoleRegUint)
	describeDstSrcSrc(OpShiftRightUint64, RoleRegUint)
	describeDstSrcSrc(OpAddComplex128, RoleRegComplex)
	describeDstSrcSrc(OpSubtractComplex128, RoleRegComplex)
	describeDstSrcSrc(OpMultiplyComplex128, RoleRegComplex)
	describeDstSrcSrc(OpDivideComplex128, RoleRegComplex)
}

// populateGenericArithShapes describes the reflect-based arithmetic opcodes that operate
// on the general register bank.
func populateGenericArithShapes() {
	describeDstSrcSrc(OpAddGeneral, RoleRegGeneral)
	describeDstSrcSrc(OpSubtractGeneral, RoleRegGeneral)
	describeDstSrcSrc(OpMultiplyGeneral, RoleRegGeneral)
	describeDstSrcSrc(OpDivideGeneral, RoleRegGeneral)
	describeDstSrcSrc(OpRemainderGeneral, RoleRegGeneral)
}

// populateComparisonShapes describes the comparison opcodes that produce an int
// (boolean-as-int) result from typed-bank reads.
func populateComparisonShapes() {
	describeDstSrcSrcMixed(OpEqualInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpNotEqualInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpLessThanInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpLessOrEqualInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpGreaterThanInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpGreaterOrEqualInt64, RoleRegInt, RoleRegInt)
	describeDstSrcSrcMixed(OpEqualFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpNotEqualFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpLessThanFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpLessOrEqualFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpGreaterThanFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpGreaterOrEqualFloat64, RoleRegInt, RoleRegFloat)
	describeDstSrcSrcMixed(OpEqualString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpNotEqualString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpLessThanString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpLessOrEqualString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpGreaterThanString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpGreaterOrEqualString, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpEqualUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpNotEqualUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpLessThanUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpLessOrEqualUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpGreaterThanUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpGreaterOrEqualUint64, RoleRegInt, RoleRegUint)
	describeDstSrcSrcMixed(OpEqualComplex128, RoleRegInt, RoleRegComplex)
	describeDstSrcSrcMixed(OpNotEqualComplex128, RoleRegInt, RoleRegComplex)
	describeDstSrcSrcMixed(OpEqualGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpNotEqualGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpLessThanGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpLessOrEqualGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpGreaterThanGeneral, RoleRegInt, RoleRegGeneral)
	describeDstSrcSrcMixed(OpGreaterOrEqualGeneral, RoleRegInt, RoleRegGeneral)
}

// populateConversionMoveShapes describes type-converting moves between scalar register
// banks plus same-bank moves and the cross-bank pack/unpack opcodes.
func populateConversionMoveShapes() {
	described3(OpTruncateToWidth, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)

	described3(OpMoveGeneral, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpBoxAsInterface, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpBoxAsType, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpUnboxFromInterface, RoleRegDynamic, RoleRegGeneral, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateConstantLoadShapes describes the opcodes that materialise constants into typed
// register banks.
func populateConstantLoadShapes() {
	describeLoadConst(OpLoadInt64Constant, RoleRegInt)
	describeLoadConst(OpLoadFloat64Constant, RoleRegFloat)
	describeLoadConst(OpLoadStringConstant, RoleRegString)
	describeLoadConst(OpLoadGeneralConstant, RoleRegGeneral)
	describeLoadConst(OpLoadCompositeZeroInPlace, RoleRegGeneral)
	describeLoadConst(OpLoadUint64Constant, RoleRegUint)
	describeLoadConst(OpLoadComplex128Constant, RoleRegComplex)
}

// populateJumpAndFusedShapes describes unconditional jumps, conditional branches, and
// fused arithmetic-and-jump opcodes.
func populateJumpAndFusedShapes() {
	describeArithConst(OpSubtractInt64Constant, RoleRegInt)
	describeArithConst(OpAddInt64Constant, RoleRegInt)
	describeArithConst(OpMultiplyInt64Constant, RoleRegInt)
	described3(OpAddInt64ConstantThenJump, RoleRegInt, RoleRegInt, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	described3(OpMultiplyInt64ConstantThenAdd, RoleRegInt, RoleRegInt, roleConstIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	describeJumpCondition(OpJumpIfTrue, RoleRegInt)
	describeJumpCondition(OpJumpIfFalse, RoleRegInt)
	describeJumpCondition(OpJumpIfNil, RoleRegGeneral)
	describeJumpCondition(OpJumpIfNotNil, RoleRegGeneral)
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
	described3(OpGetStringByte, RoleRegUint, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetStringByteAsInt64, RoleRegInt, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrc(OpConcatenateString, RoleRegString)
	described3(OpConcatenateStringRune, RoleRegString, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateStringIntrinsicShapes describes the strings package intrinsic opcodes
// (Contains, ToUpper, etc.).
func populateStringIntrinsicShapes() {
	described3(OpStringsContainsRune, RoleRegBool, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrcMixed(OpStringsContains, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStringsHasPrefix, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStringsHasSuffix, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStringsEqualFold, RoleRegBool, RoleRegString)
	describeDstSrcSrcMixed(OpStringsIndex, RoleRegInt, RoleRegString)
	describeDstSrcSrcMixed(OpStringsCount, RoleRegInt, RoleRegString)
	describeDstSrcSrc(OpStringsTrimPrefix, RoleRegString)
	describeDstSrcSrc(OpStringsTrimSuffix, RoleRegString)
	describeDstSrcSrc(OpStringsTrim, RoleRegString)
	described3(OpStringsIndexRune, RoleRegInt, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStringsRepeat, RoleRegString, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	describeDstSrcSrcMixed(OpStringsLastIndex, RoleRegInt, RoleRegString)
	described3(OpStringsJoin, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStringsSplit, RoleRegGeneral, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpStringsReplaceAll, RoleRegString, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateComplexAndFusedStringShapes describes complex constructors, accessors, and
// fused string-and-jump opcodes.
func populateComplexAndFusedStringShapes() {
	describeDstSrcSrcMixed(OpMakeComplex128, RoleRegComplex, RoleRegFloat)
	described3(OpSliceString, RoleRegString, RoleRegString, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
}

// populateSliceAndAppendShapes describes typed slice get/set and typed append opcodes.
func populateSliceAndAppendShapes() {
	describeTypedSliceGet(OpGetElementInt64, RoleRegInt)
	describeTypedSliceGet(OpGetElementFloat64, RoleRegFloat)
	describeTypedSliceGet(OpGetElementString, RoleRegString)
	describeTypedSliceGet(OpGetElementBool, RoleRegBool)
	describeTypedSliceGet(OpGetElementUint64, RoleRegUint)
	describeTypedSliceGet(OpGetElementInt64Unchecked, RoleRegInt)
	describeTypedSliceSet(OpSetElementInt64, RoleRegInt)
	describeTypedSliceSet(OpSetElementFloat64, RoleRegFloat)
	describeTypedSliceSet(OpSetElementString, RoleRegString)
	describeTypedSliceSet(OpSetElementBool, RoleRegBool)
	describeTypedSliceSet(OpSetElementUint64, RoleRegUint)
	describeTypedSliceSet(OpSetElementInt64Unchecked, RoleRegInt)
}

// populateTypedSliceDirectShapes describes typed direct-storage slice opcodes that
// operate against the app.SlicesInt bank without reflect.
func populateTypedSliceDirectShapes() {
	described3(OpGetSliceInt64Element, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetSliceInt64Element, RoleRegSliceInt, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpRangeNextSliceInt64, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	described3(OpRangeNextSliceByte, RoleRegInt, roleRegSliceByte, RoleRegUint,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension|ShapeFlagControlFlow)
	described3(OpGetSliceInt64ElementUnchecked, RoleRegInt, RoleRegSliceInt, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetSliceInt64ElementUnchecked, RoleRegSliceInt, RoleRegInt, RoleRegInt,
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
	described3(OpGetMapInt64Int64, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetMapInt64Int64, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetMapStringInt64, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetMapStringInt64, RoleRegGeneral, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)

	described3(OpSetMapInt64General, RoleRegGeneral, RoleRegInt, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpSetMapStringGeneral, RoleRegGeneral, RoleRegString, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpAddToMapInt64Int64, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpAddToMapStringInt64, RoleRegGeneral, RoleRegString, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpAppendStringToMapString, RoleRegGeneral, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetMapStringString, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetMapStringString, RoleRegGeneral, RoleRegString, RoleRegString,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetMapInt64String, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetMapInt64String, RoleRegGeneral, RoleRegInt, RoleRegString,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateGenericFieldShapes describes the generic typed-int field accessors and the
// dynamic get/set field opcodes.
func populateGenericFieldShapes() {
	described3(OpGetFieldInt64, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetFieldInt64, RoleRegGeneral, roleFieldIndex, RoleRegInt,
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
	described3(OpGetStructFieldInt64, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldInt64, RoleRegGeneral, RoleRegInt, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldUint64, RoleRegUint, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldUint64, RoleRegGeneral, RoleRegUint, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetStructFieldFloat64, RoleRegFloat, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldFloat64, RoleRegGeneral, RoleRegFloat, roleFieldIndex,
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
	described3(OpGetStructFieldRawPointer, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetStructFieldGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
	populateFusedStructFieldCopyShape()
	described3(OpSwapStructFieldsGeneral, RoleRegGeneral, roleFieldIndex, roleFieldIndex,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateSliceIndexStructFieldShapes describes the typed fused
// slice-index-of-struct-field super-instructions for every register family.
func populateSliceIndexStructFieldShapes() {
	described3(OpGetElementFieldInt64, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpGetElementFieldUint64, RoleRegUint, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpGetElementFieldFloat64, RoleRegFloat, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpGetElementFieldBool, RoleRegBool, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpGetElementFieldString, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSetElementFieldInt64, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSetElementFieldFloat64, RoleRegFloat, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
}

// populateUpvalueAndConvertShapes describes upvalue access and the remaining conversion
// opcodes that touch the general bank.
func populateUpvalueAndConvertShapes() {
	described3(OpGetCapturedVariable, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{false, false, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetCapturedVariable, RoleRegDynamic, RoleImmediate, RoleKindMarker,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateTerminatorShapes describes returns, panics, and the trivial nop and ext
// opcodes.
func populateTerminatorShapes() {
	described3(OpExtension, RoleImmediate, RoleImmediate, RoleImmediate,
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

	described3(OpGetElement, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetElementInt64ViaPointer, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetElementInt64ViaPointer, RoleRegGeneral, RoleRegInt, RoleRegInt,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)

	described3(OpGetStructFieldElementGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSetStructFieldElementGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
	described3(OpSetElement, RoleRegGeneral, RoleRegInt, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetMap, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpSetMap, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{true, true, true},
		[NumInstructionOperands]bool{false, false, false},
		0)
	described3(OpGetMapOk, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	populateTypedGetMapOkShapes()
	populateAppendShapes()
	describeDstSrcSrcMixed(OpCopyGeneral, RoleRegInt, RoleRegGeneral)
	described3(OpSendToChannel, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		0)
}

// populateAppendShapes describes the append opcode family: the generic append, the byte
// and typed fast paths, and the in-place variants that reuse the destination header.
func populateAppendShapes() {
	described3(OpAppend, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendByte, RoleRegGeneral, RoleRegGeneral, RoleRegUint,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	populateFastAppendShapes()
	described3(OpAppendByteInPlace, RoleRegGeneral, RoleRegGeneral, RoleRegUint,
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
		0)
}

// populateFusedStructFieldCopyShape describes the fused get/set pair produced by
// FuseCopyStructFieldGeneral.
func populateFusedStructFieldCopyShape() {
	described3(OpCopyStructFieldGeneral, RoleRegGeneral, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{true, true, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension)
}

// populateTypedGetMapOkShapes describes the typed map-comma-ok opcodes used by the
// compiler when both the key kind and value kind match a primitive register bank,
// eliminating the boxing that the general-bank OpGetMapOk path otherwise pays.
func populateTypedGetMapOkShapes() {
	described3(OpGetMapInt64Int64Ok, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpGetMapStringInt64Ok, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpGetMapStringStringOk, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpGetMapInt64StringOk, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpGetMapInt64General, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetMapInt64GeneralOk, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	populateStringKeyedGeneralMapShapes()
	described3(OpGetMapInt64Int64OkThenJumpIfFalse, RoleRegInt, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
	described3(OpGetMapStringInt64OkThenJumpIfFalse, RoleRegInt, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
	described3(OpGetMapStringStringOkThenJumpIfFalse, RoleRegString, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
	described3(OpGetMapInt64StringOkThenJumpIfFalse, RoleRegString, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
	described3(OpGetMapInt64GeneralOkThenJumpIfFalse, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
	described3(OpGetMapStringGeneralOkThenJumpIfFalse, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites|ShapeFlagControlFlow)
}

// populateAddressTypeShapes describes address-of, dereference, type-assertion, and
// reflect-driven indirection opcodes.
func populateAddressTypeShapes() {
	described3(OpAddress, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, true, false},
		0)

	described3(OpDereference, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAllocateIndirect, RoleRegGeneral, RoleRegDynamic, RoleKindMarker,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpTypeAssert, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, true},
		ShapeFlagFollowsExtension)
	described3(OpConvertGeneral, RoleRegGeneral, RoleRegGeneral, RoleImmediate,
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
	described3(OpAppendStruct, RoleRegGeneral, RoleRegGeneral, RoleRegGeneral,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		ShapeFlagFollowsExtension)
	described3(OpAppendInt64, RoleRegGeneral, RoleRegGeneral, RoleRegInt,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendFloat64, RoleRegGeneral, RoleRegGeneral, RoleRegFloat,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpAppendString, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
}

// populateStringKeyedGeneralMapShapes describes the string-keyed general-valued map reads
// and comma-ok variants.
func populateStringKeyedGeneralMapShapes() {
	described3(OpGetMapStringGeneral, RoleRegGeneral, RoleRegGeneral, RoleRegString,
		[NumInstructionOperands]bool{false, true, true},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetMapStringGeneralOk, RoleRegGeneral, RoleRegGeneral, RoleRegString,
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
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)
	described3(OpGo, RoleRegGeneral, RoleImmediate, RoleImmediate,
		[NumInstructionOperands]bool{true, false, false},
		[NumInstructionOperands]bool{false, false, false},
		ShapeFlagFollowsExtension|ShapeFlagOpaqueWrites)

	described3(OpLengthStructField, RoleRegInt, RoleRegGeneral, roleFieldIndex,
		[NumInstructionOperands]bool{false, true, false},
		[NumInstructionOperands]bool{true, false, false},
		0)
	described3(OpGetStructFieldElementField, RoleRegDynamic, RoleRegGeneral, roleFieldIndex,
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
