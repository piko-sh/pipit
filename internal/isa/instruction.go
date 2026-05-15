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

import (
	"fmt"

	"pipit.sh/pipit/internal/safeconv"
)

// Instruction is a compact 4-byte bytecode instruction in Op[A][B][C] format.
//
// exhaustruct:ignore
type Instruction struct {
	// Op is the opcode identifying the operation to perform.
	Op Opcode

	// A is the first operand, typically the destination register index.
	A uint8

	// B is the second operand, typically a source register index or the low byte of a wide
	// immediate.
	B uint8

	// C is the third operand, typically a source register index or the high byte of a wide
	// immediate.
	C uint8
}

// NewInstruction creates an instruction from its components.
//
// Takes op (Opcode) which is the operation to encode.
// Takes a (uint8) which is the first operand.
// Takes b (uint8) which is the second operand.
// Takes c (uint8) which is the third operand.
//
// Returns the assembled instruction.
func NewInstruction(op Opcode, a, b, c uint8) Instruction {
	return Instruction{Op: op, A: a, B: b, C: c}
}

// NewTier1Instruction creates a tier-1 instruction {OpDrillTier1, subOp, b, c}.
//
// Takes subOp (SubOpcode) which selects the tier-1 operation.
// Takes b (uint8) which is the first operand.
// Takes c (uint8) which is the second operand.
//
// Returns the assembled instruction.
func NewTier1Instruction(subOp SubOpcode, b, c uint8) Instruction {
	return Instruction{Op: OpDrillTier1, A: uint8(subOp), B: b, C: c}
}

// NewTier2Instruction creates a tier-2 instruction {OpDrillTier1, SubOpDrillTier2, subOp,
// c}.
//
// Tier-2 operations carry their single operand in C; A and B are drill discriminators.
//
// Takes subOp (SubOpcodeTier2) which selects the tier-2 operation.
// Takes c (uint8) which is the sole operand.
//
// Returns the assembled instruction.
func NewTier2Instruction(subOp SubOpcodeTier2, c uint8) Instruction {
	return Instruction{
		Op: OpDrillTier1,
		A:  uint8(SubOpDrillTier2),
		B:  uint8(subOp),
		C:  c,
	}
}

// NewTier3Instruction creates a tier-3 instruction {OpDrillTier1, SubOpDrillTier2,
// SubOpTier2DrillTier3, subOp}.
//
// Tier-3 operations take no operands; all three bytes are discriminators.
//
// Takes subOp (SubOpcodeTier3) which selects the tier-3 operation.
//
// Returns the assembled instruction.
func NewTier3Instruction(subOp SubOpcodeTier3) Instruction {
	return Instruction{
		Op: OpDrillTier1,
		A:  uint8(SubOpDrillTier2),
		B:  uint8(SubOpTier2DrillTier3),
		C:  uint8(subOp),
	}
}

// String returns a human-readable representation of the instruction for debugging and
// disassembly.
//
// Returns a formatted string showing the opcode and operand values.
func (i Instruction) String() string {
	return fmt.Sprintf("%-18s %3d %3d %3d", i.Op, i.A, i.B, i.C)
}

// SignedOffset extracts a signed 16-bit jump offset from B|(C<<8).
//
// Returns the offset as an int16.
func (i Instruction) SignedOffset() int16 {
	return JoinOffset(i.B, i.C)
}

// WideIndex extracts an unsigned 16-bit index from B|(C<<8).
//
// Returns the index as a uint16.
func (i Instruction) WideIndex() uint16 {
	return JoinWide(i.B, i.C)
}

// SplitWide splits a uint16 into low and high bytes for bytecode B|C encoding.
//
// Takes value (uint16) which is the wide operand to split.
//
// Returns lo (uint8) which is the low byte (stored in B).
// Returns hi (uint8) which is the high byte (stored in C).
func SplitWide(value uint16) (lo, hi uint8) {
	return uint8(value), uint8(value >> 8) //nolint:gosec // byte split
}

// JoinWide reconstructs a uint16 from low and high bytes.
//
// Takes lo (uint8) which is the low byte (from B).
// Takes hi (uint8) which is the high byte (from C).
//
// Returns the reconstructed uint16.
func JoinWide(lo, hi uint8) uint16 {
	return uint16(lo) | uint16(hi)<<WideBitShift
}

// DecodeExtension24 reconstructs a 24-bit unsigned integer from the three fields of an
// OpExt instruction (a=low, b=mid, c=high byte).
//
// Takes ext (Instruction) which is the extension instruction.
//
// Returns the decoded 24-bit value as an int.
func DecodeExtension24(ext Instruction) int {
	return int(ext.A) | int(ext.B)<<WideBitShift | int(ext.C)<<(2*WideBitShift)
}

// SplitOffset splits a signed 16-bit jump offset into low and high bytes, preserving
// two's complement representation.
//
// Takes offset (int16) which is the signed offset to split.
//
// Returns lo (uint8) which is the low byte.
// Returns hi (uint8) which is the high byte.
func SplitOffset(offset int16) (lo, hi uint8) {
	return SplitWide(safeconv.Int16ToUint16(offset))
}

// JoinOffset reconstructs a signed 16-bit jump offset from low and high bytes.
//
// Takes lo (uint8) which is the low byte.
// Takes hi (uint8) which is the high byte.
//
// Returns the reconstructed int16 offset.
func JoinOffset(lo, hi uint8) int16 {
	return safeconv.Uint16ToInt16(JoinWide(lo, hi))
}
