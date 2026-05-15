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
	// SliceMaxBitFlag is the bit flag ORed into the flags byte of OpSliceOp when a
	// three-index slice expression has a max bound.
	SliceMaxBitFlag uint8 = 4

	// SliceLowBoundFlag is the bit flag indicating the low bound is present in a slice or
	// string-slice operation.
	SliceLowBoundFlag uint8 = 1

	// SliceHighBoundFlag is the bit flag indicating the high bound is present in a slice or
	// string-slice operation.
	SliceHighBoundFlag uint8 = 2

	// WideBitShift is the number of bits to shift when encoding or decoding wide (16-bit)
	// instruction operands from B|(C<<8).
	WideBitShift = 8

	// GeneralRegisterBankSize is the number of slots in a single register bank, used to size
	// the per-bank taint bitmap. The runtime addresses each bank with a single uint8
	// operand, so 256 entries cover the entire bank.
	GeneralRegisterBankSize = 256

	// BuiltinPrint identifies the print builtin function.
	BuiltinPrint uint8 = 1

	// BuiltinPrintln identifies the println builtin function.
	BuiltinPrintln uint8 = 2

	// BuiltinClear identifies the clear builtin function.
	BuiltinClear uint8 = 3

	// BuiltinClose identifies the close builtin in a defer or go statement.
	BuiltinClose uint8 = 4

	// BuiltinDelete identifies the delete builtin in a defer or go statement.
	BuiltinDelete uint8 = 5

	// BuiltinPanic identifies the panic builtin in a defer or go statement.
	BuiltinPanic uint8 = 6

	// BuiltinRecover identifies the recover builtin in a defer or go statement. Deferred
	// directly it never recovers: the spec's "called directly by a deferred function"
	// condition needs a function between the runtime and the call.
	BuiltinRecover uint8 = 7

	// BuiltinCopy identifies the copy builtin in a defer or go statement.
	BuiltinCopy uint8 = 8

	// SelectDirectionReceive indicates a receive operation in a select case.
	SelectDirectionReceive uint8 = 0

	// SelectDirectionSend indicates a send operation in a select case.
	SelectDirectionSend uint8 = 1

	// SelectDirectionDefault indicates the default case in a select statement.
	SelectDirectionDefault uint8 = 2

	// MaxTrivialDeferArgs caps the argument count for the simple-defer fast path.
	MaxTrivialDeferArgs = 4

	// MoveGeneralModeSnapshotRangeCandidate marks a read-only struct range value that
	// AliasReadOnlyRangeValues resolves to an alias or snapshot at compile time.
	MoveGeneralModeSnapshotRangeCandidate uint8 = 3

	// EmbeddedUnexportedPrefix is prepended to the synthesised reflect field name for
	// embedded unexported types because reflect.StructOf rejects anonymous fields with
	// PkgPath set. isAnonymousField and structFieldNameMatches treat such fields as
	// embedded.
	EmbeddedUnexportedPrefix = "PipitEmbed_"

	// SynthesisedIDFieldPrefix is prepended to the zero-width sentinel field that prevents
	// distinct source declarations sharing a layout from collapsing into one reflect.Type.
	// Changing this value is a bytecode format change.
	SynthesisedIDFieldPrefix = "_pipitID_"

	// CycleBrokenTagKey is the reflect.StructField.Tag key marking a field whose type was
	// substituted because reflect cannot build a self-referential type. Changing this value
	// is a bytecode format change.
	CycleBrokenTagKey = "pipit_cycle_broken"

	// CycleBrokenTagValue is the canonical value attached to a CycleBrokenTagKey marker.
	CycleBrokenTagValue = "1"

	// SentinelFieldDeref is the field-index sentinel used with OpSetField to indicate "set
	// via pointer dereference" rather than an actual struct field.
	SentinelFieldDeref uint8 = 255

	// AllocIndirectHeapCell is the OpExt C operand of OpAllocIndirect that requests a
	// heap-resident cell for cells that must outlive the allocating frame.
	AllocIndirectHeapCell uint8 = 1

	// StructFieldLayoutMaxPathDepth caps the embedded-field walk depth. 4 levels covers
	// virtually every real-world Go embedded pattern; paths deeper than this fall back to
	// the existing slow path.
	StructFieldLayoutMaxPathDepth = 4

	// SpillAreaOffset is the starting index in the register file where spill slots begin.
	// Spill slot N maps to register file index SpillAreaOffset + N.
	SpillAreaOffset = 256

	// MakeSliceExtHeapFlag is the heap-placement bit in the OpMakeSlice extension word's
	// operand C, placing the backing on the Go heap instead of the register arena.
	MakeSliceExtHeapFlag uint8 = 1
)

// InstrIsTier1SubOp reports whether instr is the tier-1 form {OpDrillTier1, subOp, *, *}.
//
// Takes instr (Instruction) which is the instruction to classify.
// Takes subOp (SubOpcode) which is the tier-1 sub-opcode to compare against.
//
// Returns true when instr carries the requested tier-1 sub-op.
func InstrIsTier1SubOp(instr Instruction, subOp SubOpcode) bool {
	return instr.Op == OpDrillTier1 && SubOpcode(instr.A) == subOp
}

// InstrIsTier2SubOp reports whether instr is the tier-2 form {OpDrillTier1,
// SubOpDrillTier2, subOp, *}.
//
// Takes instr (Instruction) which is the instruction to classify.
// Takes subOp (SubOpcodeTier2) which is the tier-2 sub-opcode to compare against.
//
// Returns true when instr carries the requested tier-2 sub-op.
func InstrIsTier2SubOp(instr Instruction, subOp SubOpcodeTier2) bool {
	return instr.Op == OpDrillTier1 &&
		SubOpcode(instr.A) == SubOpDrillTier2 &&
		SubOpcodeTier2(instr.B) == subOp
}

// InstrIsTier3SubOp reports whether instr is the tier-3 form {OpDrillTier1,
// SubOpDrillTier2, SubOpTier2DrillTier3, subOp}.
//
// Takes instr (Instruction) which is the instruction to classify.
// Takes subOp (SubOpcodeTier3) which is the tier-3 sub-opcode to compare against.
//
// Returns true when instr carries the requested tier-3 sub-op.
func InstrIsTier3SubOp(instr Instruction, subOp SubOpcodeTier3) bool {
	return instr.Op == OpDrillTier1 &&
		SubOpcode(instr.A) == SubOpDrillTier2 &&
		SubOpcodeTier2(instr.B) == SubOpTier2DrillTier3 &&
		SubOpcodeTier3(instr.C) == subOp
}
