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

const (
	// refusedClosure is the reason for operations that reach the callee's closure cells.
	refusedClosure = "reads or writes the closure cells of the frame it runs in"

	// refusedFrame is the reason for operations whose meaning depends on which frame runs
	// them.
	refusedFrame = "its effect depends on the frame it runs in: a panic, a recover, a defer, a goroutine start or a spill slot"

	// refusedChannel is the reason for the channel operations.
	refusedChannel = "a channel operation can park the goroutine, which resumes by frame"

	// refusedCall is the reason for the call forms the splice does not carry.
	refusedCall = "a native, built-in, method or closure call: the call site is not one the splice merges"

	// refusedMethodTable is the reason for operations that read the function's method
	// tables.
	refusedMethodTable = "binds a method through a table of the function it was compiled in"

	// refusedInPlace is the reason for operations chosen by a proof about the callee's own
	// registers.
	refusedInPlace = "reuses its destination's storage under a proof made for the callee's registers alone"

	// refusedArena is the reason for the typed slice allocations.
	refusedArena = "allocates from the frame's arena: spliced into a loop, each pass would hold its allocation until the caller returns"

	// refusedExtension is the reason for operations whose extension words are not fully
	// described.
	refusedExtension = "its extension words name registers that isa.ExtensionRegisterUse does not describe for every bank"

	// refusedExtensionBank is the reason for operations whose register bank is named by an
	// extension word.
	refusedExtensionBank = "an extension word names the bank of one of its registers"

	// refusedExtensionTable is the reason for operations whose extension words carry table
	// indices the splice does not merge.
	refusedExtensionTable = "its extension words carry indices into a table of the callee that the splice does not merge"

	// refusedRange is the reason for the range loop forms.
	refusedRange = "a range loop form: its iterator state and loop latch are not carried by the splice"

	// refusedTypeSwitch is the reason for the type-switch table.
	refusedTypeSwitch = "a type-switch table: its rows carry type-table indices and must stay contiguous"

	// refusedGeneralAppend is the reason for the two general append forms.
	refusedGeneralAppend = "a general append: whether it may reuse its slice header is recorded by program counter for the function it was compiled in"

	// refusedUnsafe is the reason for the unsafe-package operations.
	refusedUnsafe = "an unsafe-package operation: its value is handed to the licence checker by site"
)

// inlineRefusedOperations holds, by flat dispatch index, the reason each operation
// excluded from splicing is kept out, and the empty string for every other operation.
var inlineRefusedOperations = buildInlineRefusedOperations()

// refusalTable is the refusal table while it is being built.
type refusalTable [isa.FlatDispatchSlots]string

// tier0 records a reason against tier-0 operations.
//
// Takes reason (string) which is why the operations are refused.
// Takes operations (...isa.Opcode) which are the operations.
func (table *refusalTable) tier0(reason string, operations ...isa.Opcode) {
	for _, operation := range operations {
		table[isa.FlatIndexOf(operation)] = reason
	}
}

// tier1 records a reason against tier-1 operations.
//
// Takes reason (string) which is why the operations are refused.
// Takes operations (...isa.SubOpcode) which are the operations.
func (table *refusalTable) tier1(reason string, operations ...isa.SubOpcode) {
	for _, operation := range operations {
		table[isa.FlatIndexOfSub1(operation)] = reason
	}
}

// tier2 records a reason against tier-2 operations.
//
// Takes reason (string) which is why the operations are refused.
// Takes operations (...isa.SubOpcodeTier2) which are the operations.
func (table *refusalTable) tier2(reason string, operations ...isa.SubOpcodeTier2) {
	for _, operation := range operations {
		table[isa.FlatIndexOfSub2(operation)] = reason
	}
}

// buildInlineRefusedOperations assembles the refusal table from its groups.
//
// Returns refusalTable which holds each refused operation's reason at its flat index.
func buildInlineRefusedOperations() refusalTable {
	var table refusalTable
	refuseFrameBoundOperations(&table)
	refuseOperationsTheSpliceCannotCarry(&table)
	return table
}

// refuseFrameBoundOperations records the operations whose meaning belongs to the frame,
// the function or the proof they were compiled under.
//
// Takes table (*refusalTable) which is the table being built.
func refuseFrameBoundOperations(table *refusalTable) {
	table.tier0(refusedClosure, isa.OpMakeClosure, isa.OpGetCapturedVariable, isa.OpSetCapturedVariable)
	table.tier1(refusedClosure, isa.SubOpResetSharedCell, isa.SubOpSetSharedCell)
	table.tier2(refusedClosure, isa.SubOpTier2SynchroniseClosureCapturedVariables)

	table.tier0(refusedFrame, isa.OpDefer, isa.OpGo)
	table.tier1(refusedFrame, isa.SubOpSpill, isa.SubOpReload)
	table.tier2(refusedFrame, isa.SubOpTier2Panic, isa.SubOpTier2Recover, isa.SubOpTier2PanicRangeFunctionContinuedInt64)

	table.tier0(refusedChannel, isa.OpSendToChannel)
	table.tier1(refusedChannel, isa.SubOpMakeChannel, isa.SubOpReceiveFromChannel, isa.SubOpSelect)
	table.tier2(refusedChannel, isa.SubOpTier2CloseChannel)

	table.tier1(refusedCall,
		isa.SubOpCallNative, isa.SubOpCallBuiltin, isa.SubOpCallMethod, isa.SubOpCallIIFE,
		isa.SubOpCallMethodInlineable)

	table.tier0(refusedMethodTable, isa.OpBindMethod)
	table.tier1(refusedMethodTable, isa.SubOpMakeMethodExpression, isa.SubOpBindMethodByName)
	table.tier2(refusedMethodTable, isa.SubOpTier2MakeInterfaceMethodExpression)

	table.tier0(refusedInPlace,
		isa.OpLoadCompositeZeroInPlace, isa.OpAppendByteInPlace, isa.OpAppendInPlace, isa.OpAppendSpreadInPlace)
	table.tier1(refusedInPlace, isa.SubOpAppendUint64WideInPlace, isa.SubOpAppendSpreadByteInPlace)

	table.tier1(refusedArena,
		isa.SubOpMakeSliceInt64, isa.SubOpMakeSliceFloat64, isa.SubOpMakeSliceString,
		isa.SubOpMakeSliceBool, isa.SubOpMakeSliceUint64, isa.SubOpMakeSliceByte)

	table.tier0(refusedGeneralAppend, isa.OpAppend, isa.OpAppendSpread)

	table.tier0(refusedUnsafe, isa.OpUnsafeString, isa.OpUnsafeSlice, isa.OpUnsafeAdd)
	table.tier1(refusedUnsafe, isa.SubOpUnsafeStringData, isa.SubOpUnsafeSliceData)
}

// refuseOperationsTheSpliceCannotCarry records the operations whose extension words,
// tables or control flow the splice does not rewrite.
//
// Takes table (*refusalTable) which is the table being built.
func refuseOperationsTheSpliceCannotCarry(table *refusalTable) {
	table.tier0(refusedExtension, isa.OpAddInt64ConstantThenJump, isa.OpStringsReplaceAll)
	table.tier1(refusedExtension,
		isa.SubOpMathMod, isa.SubOpStrconvFormatInt,
		isa.SubOpAppendUint64Wide, isa.SubOpAppendInt64Wide, isa.SubOpAppendStringWide,
		isa.SubOpAppendFloat64Wide, isa.SubOpAppendBoolWide,
		isa.SubOpSimdDotProductFloat64, isa.SubOpSimdSumFloat64, isa.SubOpSimdAddFloat64, isa.SubOpSimdScaleFloat64,
		isa.SubOpAppendSliceInt64, isa.SubOpAppendSliceFloat64, isa.SubOpAppendSliceString,
		isa.SubOpAppendSliceBool, isa.SubOpAppendSliceUint64, isa.SubOpAppendSliceByte,
		isa.SubOpCopySliceInt64, isa.SubOpCopySliceFloat64, isa.SubOpCopySliceString,
		isa.SubOpCopySliceBool, isa.SubOpCopySliceUint64, isa.SubOpCopySliceByte,
		isa.SubOpEqualInterfaceStrict, isa.SubOpNotEqualInterfaceStrict)

	table.tier0(refusedExtensionBank, isa.OpGetStructFieldElementField)
	table.tier1(refusedExtensionBank, isa.SubOpMakeSliceHeap)

	table.tier1(refusedExtensionTable, isa.SubOpGetStructFieldUint64Run, isa.SubOpGetElementFieldUint64Run)
	table.tier2(refusedExtensionTable, isa.SubOpTier2AllocateStructLiteral)

	table.tier0(refusedRange, isa.OpRangeNextSliceByte, isa.OpRangeNextSliceInt64)
	table.tier1(refusedRange, isa.SubOpRangeInitialise, isa.SubOpRangeNext)

	table.tier0(refusedTypeSwitch, isa.OpTypeSwitchCase)
	table.tier1(refusedTypeSwitch, isa.SubOpTypeSwitchJump)
}

// inlineRefusalReason returns why an operation is kept out of spliced bodies although its
// description would admit it, or the empty string when nothing keeps it out.
//
// Takes instruction (isa.Instruction) which is the callee instruction.
//
// Returns string which is the reason, empty when the operation is not refused.
func inlineRefusalReason(instruction isa.Instruction) string {
	return inlineRefusedOperations[isa.FlatIndexFor(instruction)]
}
