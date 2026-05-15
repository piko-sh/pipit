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

package asm

import (
	"fmt"
	"strings"

	"piko.sh/asmgen"
)

const (
	// callFrameSizeComment heads the CALLFRAME_SIZE define in the dispatch header.
	callFrameSizeComment = `// callFrame size in bytes, pinned by TestCallFrameOffsets.
`
)

// DispatchContextOffsets carries dispatchContext field offsets baked into the generated
// asm_dispatch_offsets.h.
//
//exhaustruct:ignore
type DispatchContextOffsets struct {
	// CodeBase is the byte offset of the CodeBase field (pointer to the first instruction in
	// the active body).
	CodeBase uintptr

	// CodeLength is the byte offset of the CodeLength field (number of instructions in the
	// body).
	CodeLength uintptr

	// ProgramCounter is the byte offset of the ProgramCounter field (current instruction
	// index).
	ProgramCounter uintptr

	// IntsBase is the byte offset of the IntsBase field (pointer to the int64 register
	// bank).
	IntsBase uintptr

	// IntsLength is the byte offset of the IntsLength field (number of int64 registers
	// allocated).
	IntsLength uintptr

	// FloatsBase is the byte offset of the FloatsBase field (pointer to the float64 register
	// bank).
	FloatsBase uintptr

	// FloatsLength is the byte offset of the FloatsLength field (number of float64 registers
	// allocated).
	FloatsLength uintptr

	// IntConstantsBase is the byte offset of the IntConstantsBase field (pointer to the
	// int64 constant pool).
	IntConstantsBase uintptr

	// IntConstantsLength is the byte offset of the IntConstantsLength field.
	IntConstantsLength uintptr

	// FloatConstantsBase is the byte offset of the FloatConstantsBase field (pointer to the
	// float64 constant pool).
	FloatConstantsBase uintptr

	// FloatConstantsLength is the byte offset of the FloatConstantsLength field.
	FloatConstantsLength uintptr

	// JumpTable is the byte offset of the JumpTable field (pointer to the flat dispatch
	// table).
	JumpTable uintptr

	// ExitReason is the byte offset of the ExitReason field, written by ASM before returning
	// to Go to indicate why dispatch exited.
	ExitReason uintptr

	// ExitProgramCounter is the byte offset of the ExitProgramCounter field, holding the PC
	// at which dispatch exited.
	ExitProgramCounter uintptr

	// AsmCallInfoBase is the byte offset of the AsmCallInfoBase field (active function's
	// asmCallInfo table base pointer).
	AsmCallInfoBase uintptr

	// CallStackBase is the byte offset of the CallStackBase field (pointer to
	// vm.callStack[0]).
	CallStackBase uintptr

	// CallStackLength is the byte offset of the CallStackLength field.
	CallStackLength uintptr

	// FramePointer is the byte offset of the FramePointer field (current frame index within
	// the call stack).
	FramePointer uintptr

	// BaseFramePointer is the byte offset of the BaseFramePointer field (frame index of the
	// outermost dispatched frame).
	BaseFramePointer uintptr

	// CallDepthLimit is the byte offset of the CallDepthLimit field.
	CallDepthLimit uintptr

	// ArenaIntSlab is the byte offset of the ArenaIntSlab field (pointer to the int register
	// arena slab base).
	ArenaIntSlab uintptr

	// ArenaIntCapacity is the byte offset of the ArenaIntCapacity field (total slot count of
	// the int slab).
	ArenaIntCapacity uintptr

	// ArenaIntIndex is the byte offset of the ArenaIntIndex field (current bump position;
	// read-write by ASM).
	ArenaIntIndex uintptr

	// ArenaFloatSlab is the byte offset of the ArenaFloatSlab field (pointer to the float
	// register arena slab base).
	ArenaFloatSlab uintptr

	// ArenaFloatCapacity is the byte offset of the ArenaFloatCapacity field.
	ArenaFloatCapacity uintptr

	// ArenaFloatIndex is the byte offset of the ArenaFloatIndex field (current bump
	// position; read-write by ASM).
	ArenaFloatIndex uintptr

	// ArenaStringIndex is the byte offset of the ArenaStringIndex field (current bump
	// position; read-write by ASM).
	ArenaStringIndex uintptr

	// ArenaGeneralIndex is the byte offset of the ArenaGeneralIndex field (read-only by
	// ASM).
	ArenaGeneralIndex uintptr

	// ArenaBoolIndex is the byte offset of the ArenaBoolIndex field (read-write by ASM).
	ArenaBoolIndex uintptr

	// ArenaUintIndex is the byte offset of the ArenaUintIndex field (read-write by ASM).
	ArenaUintIndex uintptr

	// ArenaComplexIndex is the byte offset of the ArenaComplexIndex field (read-only by
	// ASM).
	ArenaComplexIndex uintptr

	// DeferStackLength is the byte offset of the DeferStackLength field (vm.deferStack entry
	// count).
	DeferStackLength uintptr

	// HasGoroutinesFlag is the byte offset of the HasGoroutinesFlag field, read by the ASM
	// closure-call guard.
	HasGoroutinesFlag uintptr

	// CallInfoBasesRootMatch is the byte offset of the CallInfoBasesRootMatch field, read by
	// the inline call to decide whether the per-function call-info base array belongs to the
	// root the current frame was compiled in.
	CallInfoBasesRootMatch uintptr

	// CallInfoBasesByFunction is the byte offset of the CallInfoBasesByFunction field: the
	// pointer to the per-function asmCallInfo table base array the inline call handlers
	// index with ACI_CALLEE_FUNC_INDEX.
	CallInfoBasesByFunction uintptr

	// ReturnBankCursors is the byte offset of the ReturnBankCursors array: the per-kind
	// callee source cursors the inline multi-return handler advances as it copies results.
	ReturnBankCursors uintptr

	// ReturnIndex is the byte offset of the ReturnIndex field: the destination position the
	// inline multi-return handler is copying.
	ReturnIndex uintptr

	// ReturnCount is the byte offset of the ReturnCount field: the number of results the
	// inline multi-return handler copies.
	ReturnCount uintptr

	// ArenaGenericBytesSlabHeader is the byte offset of the ArenaGenericBytesSlabHeader
	// field (address of the RegisterArena.genericBytesSlab slice header).
	ArenaGenericBytesSlabHeader uintptr

	// ArenaGenericBytesIndexPointer is the byte offset of the ArenaGenericBytesIndexPointer
	// field (address of RegisterArena.genericBytesIndex).
	ArenaGenericBytesIndexPointer uintptr

	// ArenaGenericBytesCharged is the byte offset of the ArenaGenericBytesCharged field
	// (bytes the struct-literal allocation handler carved since the last write-back).
	ArenaGenericBytesCharged uintptr

	// CurrentFrame is the byte offset of the CurrentFrame field: the cached address of the
	// active callFrame, maintained by the inline call/return ASM and the Go frame-change
	// refresh sites so shim trampolines resolve the frame with one load.
	CurrentFrame uintptr

	// PollBudget is the byte offset of the PollBudget field, decremented by the ASM
	// back-edge poll so the dispatch loop periodically returns to Go to check cancellation.
	PollBudget uintptr

	// AsmCallInfoBasesPtr is the byte offset of the asmCallInfoBasesPointer field (pointer
	// to the asmCallInfoBases slice base).
	AsmCallInfoBasesPtr uintptr

	// DispatchSavesPtr is the byte offset of the dispatchSavesPointer field (pointer to the
	// asmDispatchSaves slice base).
	DispatchSavesPtr uintptr

	// StringsBase is the byte offset of the StringsBase field (pointer to the string
	// register bank, 16-byte headers).
	StringsBase uintptr

	// UintsBase is the byte offset of the UintsBase field (pointer to the uint64 register
	// bank).
	UintsBase uintptr

	// BoolsBase is the byte offset of the BoolsBase field (pointer to the bool register
	// bank).
	BoolsBase uintptr

	// GeneralsBase is the byte offset of the GeneralsBase field (pointer to the current
	// frame's general reflect.Value bank; read-only from ASM). Emitted as CTX_GENERALS_BASE.
	GeneralsBase uintptr

	// ArenaStringSlab is the byte offset of the ArenaStringSlab field (pointer to the string
	// register arena slab base).
	ArenaStringSlab uintptr

	// ArenaStringCapacity is the byte offset of the ArenaStringCapacity field.
	ArenaStringCapacity uintptr

	// ArenaBoolSlab is the byte offset of the ArenaBoolSlab field (pointer to the bool
	// register arena slab base).
	ArenaBoolSlab uintptr

	// ArenaBoolCapacity is the byte offset of the ArenaBoolCapacity field.
	ArenaBoolCapacity uintptr

	// ArenaUintSlab is the byte offset of the ArenaUintSlab field (pointer to the uint
	// register arena slab base).
	ArenaUintSlab uintptr

	// ArenaUintCapacity is the byte offset of the ArenaUintCapacity field.
	ArenaUintCapacity uintptr

	// SlicesIntBase is the byte offset of the SlicesIntBase field (pointer to the first
	// []int64 slice header in the registers.slicesInt bank; each slot is 24 bytes).
	SlicesIntBase uintptr

	// SlicesFloatBase is the byte offset of the SlicesFloatBase field (pointer to the first
	// []float64 slice header).
	SlicesFloatBase uintptr

	// SlicesStringBase is the byte offset of the SlicesStringBase field (pointer to the
	// first []string slice header).
	SlicesStringBase uintptr

	// SlicesBoolBase is the byte offset of the SlicesBoolBase field (pointer to the first
	// []bool slice header).
	SlicesBoolBase uintptr

	// SlicesUintBase is the byte offset of the SlicesUintBase field (pointer to the first
	// []uint64 slice header).
	SlicesUintBase uintptr

	// ComplexBase is the byte offset of the ComplexBase field (pointer to the first
	// complex128 element; 16 bytes per slot).
	ComplexBase uintptr

	// StringConstantsBase is the byte offset of the StringConstantsBase field (pointer to
	// the string constant pool; 16-byte headers).
	StringConstantsBase uintptr

	// StringConstantsLength is the byte offset of the StringConstantsLength field.
	StringConstantsLength uintptr

	// BoolConstantsBase is the byte offset of the BoolConstantsBase field (pointer to the
	// bool constant pool; 1-byte entries).
	BoolConstantsBase uintptr

	// UintConstantsBase is the byte offset of the UintConstantsBase field (active function's
	// uint constant pool base). Emitted as CTX_UINT_CONSTS_BASE.
	UintConstantsBase uintptr

	// UintConstantsLength is the informational length companion, emitted as
	// CTX_UINT_CONSTS_LEN.
	UintConstantsLength uintptr

	// BoolConstantsLength is the byte offset of the BoolConstantsLength field.
	BoolConstantsLength uintptr

	// SavedPC is the byte offset of the spill slot used by tier-1 inline-call shims to stash
	// R14 (pipit PC) across a Go-trampoline CALL.
	//
	// Spilled to ctx rather than the handler's local frame so the frame can declare
	// NO_LOCAL_POINTERS truthfully. See the asmgen emitters EmitInlineGoCallTwoOperandShim
	// and EmitInlineGoCallThreeOperandShim for the spill discipline.
	SavedPC uintptr

	// HandlerResult is the byte offset of the single-byte opResult slot that tier-2 ASM-call
	// shims read after CALLing their Go trampoline.
	HandlerResult uintptr

	// ArenaBanksDirty is the byte offset of the single-byte flag (CTX_ARENA_BANKS_DIRTY)
	// that the inline call/tail-call/return handlers set after mutating any CTX_ARENA_*_IDX
	// cursor, letting Go's exit sync skip the eleven cursor write-backs while zero.
	ArenaBanksDirty uintptr

	// StructLayoutTableBase is the byte offset of the StructLayoutTableBase field (active
	// function's structLayoutTable base pointer).
	StructLayoutTableBase uintptr

	// StructLayoutTableLength is the byte offset of the StructLayoutTableLength field;
	// informational only at runtime.
	StructLayoutTableLength uintptr

	// TypeTableBase is the byte offset of the TypeTableBase field (active function's
	// typeTable base pointer).
	TypeTableBase uintptr

	// TypeTableLength is the byte offset of the TypeTableLength field; informational only at
	// runtime.
	TypeTableLength uintptr

	// SlicesByteBase is the byte offset of the SlicesByteBase field (pointer to
	// registers.slicesByte[0]).
	SlicesByteBase uintptr

	// ArenaGeneralSlab is the byte offset of the general-slab base-pointer mirror.
	ArenaGeneralSlab uintptr

	// ArenaGeneralSlabCap is the byte offset of the general-slab capacity mirror.
	ArenaGeneralSlabCap uintptr

	// ArenaSliceByteSlab is the byte offset of the ArenaSliceByteSlab field (pointer to the
	// typed []byte arena slab base).
	ArenaSliceByteSlab uintptr

	// ArenaSliceByteCapacity is the byte offset of the ArenaSliceByteCapacity field (total
	// slot count of the typed []byte slab).
	ArenaSliceByteCapacity uintptr

	// ArenaSliceByteIndex is the byte offset of the ArenaSliceByteIndex field (current bump
	// position of the typed []byte slab; read-write by ASM).
	ArenaSliceByteIndex uintptr

	// ArenaSliceIntIndex is the byte offset of the slicesInt bump-index mirror field.
	//
	// Read by the inline-call frame-push shim so the new frame's ArenaSave block carries the
	// correct save-point values for every typed-slice bank (not just slicesByte). ASM does
	// not bump these indices itself; they exist solely so the frame-push shim's ArenaSave
	// write covers all 13 ArenaSavePoint slots.
	ArenaSliceIntIndex uintptr

	// ArenaSliceFloatIndex is the byte offset of the slicesFloat bump-index mirror field.
	ArenaSliceFloatIndex uintptr

	// ArenaSliceStringIndex is the byte offset of the slicesString bump-index mirror field.
	ArenaSliceStringIndex uintptr

	// ArenaSliceBoolIndex is the byte offset of the slicesBool bump-index mirror field.
	ArenaSliceBoolIndex uintptr

	// ArenaSliceUintIndex is the byte offset of the slicesUint bump-index mirror field.
	ArenaSliceUintIndex uintptr
}

// CallFrameOffsets carries the size of the runtime callFrame struct and byte offsets of
// every callFrame field referenced by the inline call and return ASM handlers.
//
//exhaustruct:ignore
type CallFrameOffsets struct {
	// Size is the total byte size of the callFrame struct (CALLFRAME_SIZE in the generated
	// header).
	Size uintptr

	// RegsIntsPtr is the byte offset of the registers.ints slice pointer within the frame.
	RegsIntsPtr uintptr

	// RegsIntsLen is the byte offset of the registers.ints slice length.
	RegsIntsLen uintptr

	// RegsIntsCap is the byte offset of the registers.ints slice capacity.
	RegsIntsCap uintptr

	// RegsFloatsPtr is the byte offset of the registers.floats slice pointer.
	RegsFloatsPtr uintptr

	// RegsFloatsLen is the byte offset of the registers.floats slice length.
	RegsFloatsLen uintptr

	// RegsFloatsCap is the byte offset of the registers.floats slice capacity.
	RegsFloatsCap uintptr

	// RegsStringsPtr is the byte offset of the registers.strings slice pointer.
	RegsStringsPtr uintptr

	// RegsStringsLen is the byte offset of the registers.strings slice length.
	RegsStringsLen uintptr

	// RegsStringsCap is the byte offset of the registers.strings slice capacity.
	RegsStringsCap uintptr

	// RegsGeneralPtr is the byte offset of the registers.general slice pointer. Not
	// allocated on the inline-call fast path so the ASM only zeros these fields; included as
	// macros so the callFrame struct stays free to reorder.
	RegsGeneralPtr uintptr

	// RegsGeneralLen is the byte offset of the registers.general slice length.
	RegsGeneralLen uintptr

	// RegsGeneralCap is the byte offset of the registers.general slice capacity.
	RegsGeneralCap uintptr

	// RegsBoolsPtr is the byte offset of the registers.bools slice pointer.
	RegsBoolsPtr uintptr

	// RegsBoolsLen is the byte offset of the registers.bools slice length.
	RegsBoolsLen uintptr

	// RegsBoolsCap is the byte offset of the registers.bools slice capacity.
	RegsBoolsCap uintptr

	// RegsUintsPtr is the byte offset of the registers.uints slice pointer.
	RegsUintsPtr uintptr

	// RegsUintsLen is the byte offset of the registers.uints slice length.
	RegsUintsLen uintptr

	// RegsUintsCap is the byte offset of the registers.uints slice capacity.
	RegsUintsCap uintptr

	// RegsComplexPtr is the byte offset of the registers.complex slice pointer.
	RegsComplexPtr uintptr

	// RegsComplexLen is the byte offset of the registers.complex slice length.
	RegsComplexLen uintptr

	// RegsComplexCap is the byte offset of the registers.complex slice capacity.
	RegsComplexCap uintptr

	// RegsSlicesIntPtr is the byte offset of the registers.slicesInt slice header pointer.
	RegsSlicesIntPtr uintptr

	// RegsSlicesIntLen is the byte offset of the registers.slicesInt slice length.
	RegsSlicesIntLen uintptr

	// RegsSlicesIntCap is the byte offset of the registers.slicesInt slice capacity.
	RegsSlicesIntCap uintptr

	// RegsSlicesFloatPtr is the byte offset of the registers.slicesFloat slice header
	// pointer.
	RegsSlicesFloatPtr uintptr

	// RegsSlicesFloatLen is the byte offset of the registers.slicesFloat slice length.
	RegsSlicesFloatLen uintptr

	// RegsSlicesFloatCap is the byte offset of the registers.slicesFloat slice capacity.
	RegsSlicesFloatCap uintptr

	// RegsSlicesStringPtr is the byte offset of the registers.slicesString slice header
	// pointer.
	RegsSlicesStringPtr uintptr

	// RegsSlicesStringLen is the byte offset of the registers.slicesString slice length.
	RegsSlicesStringLen uintptr

	// RegsSlicesStringCap is the byte offset of the registers.slicesString slice capacity.
	RegsSlicesStringCap uintptr

	// RegsSlicesBoolPtr is the byte offset of the registers.slicesBool slice header pointer.
	RegsSlicesBoolPtr uintptr

	// RegsSlicesBoolLen is the byte offset of the registers.slicesBool slice length.
	RegsSlicesBoolLen uintptr

	// RegsSlicesBoolCap is the byte offset of the registers.slicesBool slice capacity.
	RegsSlicesBoolCap uintptr

	// RegsSlicesUintPtr is the byte offset of the registers.slicesUint slice header pointer.
	RegsSlicesUintPtr uintptr

	// RegsSlicesUintLen is the byte offset of the registers.slicesUint slice length.
	RegsSlicesUintLen uintptr

	// RegsSlicesUintCap is the byte offset of the registers.slicesUint slice capacity.
	RegsSlicesUintCap uintptr

	// RegsSliceBytePtr is the byte offset of the registers.slicesByte slice header pointer.
	RegsSliceBytePtr uintptr

	// RegsSliceByteLen is the byte offset of the registers.slicesByte slice length.
	RegsSliceByteLen uintptr

	// RegsSliceByteCap is the byte offset of the registers.slicesByte slice capacity.
	RegsSliceByteCap uintptr

	// RegsLastAllocMask is the byte offset of registers.lastAllocMask within the frame
	// (CF_REGS_LAST_MASK). The inline-call installer stamps the all-banks sentinel here so
	// the Go-path diff-clearing allocator treats ASM-installed frames as fully populated.
	RegsLastAllocMask uintptr

	// Function is the byte offset of the function field (pointer to the compiled function
	// being executed).
	Function uintptr

	// FunctionStructLayoutTable is the byte offset of the structLayoutTable slice data
	// pointer within CompiledFunction (FN_STRUCT_LAYOUT_TABLE).
	//
	// Tier-0 struct-field handlers resolve the ACTIVE function's layout table through the
	// frame instead of a context-cached copy, which would go stale across inline calls (the
	// inline call/return ASM does not refresh per-function table bases).
	FunctionStructLayoutTable uintptr

	// FunctionStructLayoutTableLen is the byte offset of the structLayoutTable slice length
	// within CompiledFunction (FN_STRUCT_LAYOUT_TABLE_LEN).
	FunctionStructLayoutTableLen uintptr

	// FunctionTypeTable is the byte offset of the typeTable slice data pointer within
	// CompiledFunction (FN_TYPE_TABLE). Entries are 16-byte reflect.Type interface values;
	// the data word at entry offset 8 is the *abi.Type the unsafe value constructors take.
	FunctionTypeTable uintptr

	// FunctionResultKinds is the byte offset of the resultKinds slice data pointer within
	// CompiledFunction, read by the inline multi-return handler to validate each
	// destination's kind against the callee's result kind.
	FunctionResultKinds uintptr

	// FunctionResultKindsLen is the byte offset of the resultKinds slice length within
	// CompiledFunction.
	FunctionResultKindsLen uintptr

	// FunctionStructLiteralTableBase is the byte offset of the structLiteralTableBase field
	// within CompiledFunction: the published pointer to the first structLiteralEntry.
	FunctionStructLiteralTableBase uintptr

	// FunctionStructLiteralTableLen is the byte offset of the structLiteralTableLength field
	// within CompiledFunction: the published entry count.
	FunctionStructLiteralTableLen uintptr

	// StructLiteralEntrySize is the byte size of one structLiteralEntry.
	StructLiteralEntrySize uintptr

	// StructLiteralEntryTypeWord is the byte offset of the typeWord field within
	// structLiteralEntry (zero for a type the assembly handler must not allocate).
	StructLiteralEntryTypeWord uintptr

	// StructLiteralEntryBytes is the byte offset of the size field within
	// structLiteralEntry.
	StructLiteralEntryBytes uintptr

	// StructLiteralEntryAlignMask is the byte offset of the alignMask field within
	// structLiteralEntry (alignment minus one).
	StructLiteralEntryAlignMask uintptr

	// StructLiteralEntryFlag is the byte offset of the flag field within structLiteralEntry
	// (the reflect.Value flag word of an addressable struct view).
	StructLiteralEntryFlag uintptr

	// FunctionTypeTableLen is the byte offset of the typeTable slice length within
	// CompiledFunction (FN_TYPE_TABLE_LEN).
	FunctionTypeTableLen uintptr

	// SharedCells is the byte offset of the SharedCells field (closure-cell dedup map;
	// lazily allocated).
	SharedCells uintptr

	// Upvalues is the byte offset of the upvalues slice header.
	Upvalues uintptr

	// ReturnDestPtr is the byte offset of the returnDestination slice pointer.
	ReturnDestPtr uintptr

	// ReturnDestLen is the byte offset of the returnDestination slice length.
	ReturnDestLen uintptr

	// ReturnDestCap is the byte offset of the returnDestination slice capacity.
	ReturnDestCap uintptr

	// ProgramCounter is the byte offset of the ProgramCounter field (the per-frame PC,
	// distinct from the dispatch context's PC).
	ProgramCounter uintptr

	// DeferBase is the byte offset of the DeferBase field (the vm.deferStack index at which
	// this frame's defers start).
	DeferBase uintptr

	// SimpleDefer is the byte offset of the *simpleDeferRecord trivial-defer fast-path slot
	// (CF_SIMPLEDEFER), which inline return handlers check to fall back to Go when the
	// record is non-nil and active because the trivial path registers no deferStack entry
	// for the DeferBase guard to catch.
	SimpleDefer uintptr

	// SimpleDeferActive is the byte offset of simpleDeferRecord.active within the record
	// pointed to by the frame's SimpleDefer field. Emitted as SDR_ACTIVE.
	SimpleDeferActive uintptr

	// ArenaSave is the byte offset of the ArenaSave field (snapshot of arena state taken at
	// frame push and restored on pop).
	ArenaSave uintptr

	// ArenaSaveGeneralIndex is the byte offset of the arena save point's general-bank
	// allocation index within the callFrame (CF_ARENA_SAVE plus the nested index offset).
	// The return-inline fast arm reads it to compute the callee's general-slab clear range
	// without calling into Go.
	ArenaSaveGeneralIndex uintptr

	// HasGeneralAlloc is the byte offset of the HasGeneralAlloc byte (non-zero when this
	// frame allocated general-bank slots, telling the return handler whether to run the
	// clear+restore trampoline).
	HasGeneralAlloc uintptr

	// RootSwapped is the byte offset of the rootSwapped byte (non-zero when entering the
	// frame swapped the VM's root to another bundle, so the frame must pop through Go).
	RootSwapped uintptr
}

// ASMCallInfoOffsets carries asmCallInfo field offsets walked by the inline call
// dispatcher.
type ASMCallInfoOffsets struct {
	// CalleeFunction is the byte offset of the callee's CompiledFunction pointer.
	CalleeFunction uintptr

	// ClosurePointer is the byte offset of the closure pointer field (ACI_CLOSURE_PTR).
	ClosurePointer uintptr

	// ClosureValueRegister is the byte offset of the closure value register field
	// (ACI_CLOSURE_REG).
	ClosureValueRegister uintptr

	// ClosureUpvaluesPtr is the byte offset of the closure upvalues slice pointer
	// (ACI_UPVALS_PTR).
	ClosureUpvaluesPtr uintptr

	// ClosureUpvaluesLen is the byte offset of the closure upvalues slice length
	// (ACI_UPVALS_LEN).
	ClosureUpvaluesLen uintptr

	// ClosureUpvaluesCap is the byte offset of the closure upvalues slice capacity
	// (ACI_UPVALS_CAP).
	ClosureUpvaluesCap uintptr

	// CalleeBody is the byte offset of the callee body pointer (first instruction).
	CalleeBody uintptr

	// CalleeBodyLen is the byte offset of the callee body instruction count.
	CalleeBodyLen uintptr

	// CalleeIntConsts is the byte offset of the callee's int constant table pointer.
	CalleeIntConsts uintptr

	// CalleeFltConsts is the byte offset of the callee's float constant table pointer.
	CalleeFltConsts uintptr

	// CalleeNumInts is the byte offset of the callee's int register count.
	CalleeNumInts uintptr

	// CalleeNumFloats is the byte offset of the callee's float register count.
	CalleeNumFloats uintptr

	// NumIntArgs is the byte offset of the int argument count.
	NumIntArgs uintptr

	// IntArgSrcs is the byte offset of the int argument source array (caller register
	// indices, 8 slots).
	IntArgSrcs uintptr

	// NumFloatArgs is the byte offset of the float argument count.
	NumFloatArgs uintptr

	// FloatArgSrcs is the byte offset of the float argument source array (caller register
	// indices, 8 slots).
	FloatArgSrcs uintptr

	// NumReturns is the byte offset of the return value count (0 or 1).
	NumReturns uintptr

	// RetDestKind is the byte offset of the return-destination register kind.
	RetDestKind uintptr

	// RetDestReg is the byte offset of the return-destination register index in the caller.
	RetDestReg uintptr

	// RetDestPtr is the byte offset of the return-destination descriptor pointer.
	RetDestPtr uintptr

	// RetDestLen is the byte offset of the return-destination descriptor length.
	RetDestLen uintptr

	// CalleeCallInfo is the byte offset of the callee's asmCallInfo table base pointer.
	CalleeCallInfo uintptr

	// IsFastPath is the byte offset of the inline-dispatch mode tag, which is 0 when not
	// eligible, 1 when eligible (string/bool/uint), 2 for lean (int/float only), and 3 for
	// general-bank setup through a trampoline.
	IsFastPath uintptr

	// CalleeNumStrings is the byte offset of the callee's string register count.
	CalleeNumStrings uintptr

	// CalleeNumBools is the byte offset of the callee's bool register count.
	CalleeNumBools uintptr

	// CalleeNumUints is the byte offset of the callee's uint register count.
	CalleeNumUints uintptr

	// NumStringArgs is the byte offset of the string argument count.
	NumStringArgs uintptr

	// StringArgSrcs is the byte offset of the string argument source array (8 slots).
	StringArgSrcs uintptr

	// NumBoolArgs is the byte offset of the bool argument count.
	NumBoolArgs uintptr

	// BoolArgSrcs is the byte offset of the bool argument source array (8 slots).
	BoolArgSrcs uintptr

	// NumUintArgs is the byte offset of the uint argument count.
	NumUintArgs uintptr

	// UintArgSrcs is the byte offset of the uint argument source array (8 slots).
	UintArgSrcs uintptr

	// CalleeUintConsts is the byte offset of the callee's uint constant table pointer within
	// asmCallInfo. Emitted as ACI_CALLEE_UINT_CONSTS.
	CalleeUintConsts uintptr

	// CalleeStrConsts is the byte offset of the callee's string constant table pointer
	// (16-byte headers).
	CalleeStrConsts uintptr

	// CalleeBoolConsts is the byte offset of the callee's bool constant table pointer
	// (1-byte entries).
	CalleeBoolConsts uintptr

	// CalleeNumGeneral is the byte offset of the callee's general (reflect.Value) register
	// count.
	CalleeNumGeneral uintptr

	// NumGeneralArgs is the byte offset of the general argument count.
	NumGeneralArgs uintptr

	// GeneralArgSrcs is the byte offset of the general argument source array (8 slots).
	GeneralArgSrcs uintptr

	// CalleeNumSliceByte is the byte offset of the callee's slicesByte register count.
	CalleeNumSliceByte uintptr

	// NumSliceByteArgs is the byte offset of the []byte argument count.
	NumSliceByteArgs uintptr

	// SliceByteArgSrcs is the byte offset of the []byte argument source array (8 slots).
	SliceByteArgSrcs uintptr

	// ReceiverValueRegister is the byte offset of the method call site's receiver
	// general-bank register index (ACI_RECEIVER_REG).
	ReceiverValueRegister uintptr

	// MethodEntryCount is the byte offset of the populated receiver-type count
	// (ACI_METHOD_ENTRY_COUNT).
	MethodEntryCount uintptr

	// MethodTypeWords is the byte offset of the receiver type-word table
	// (ACI_METHOD_TYPE_WORDS, eight 8-byte slots).
	MethodTypeWords uintptr

	// MethodEntriesPtr is the byte offset of the pointer to the per-receiver-type entry
	// array (ACI_METHOD_ENTRIES_PTR).
	MethodEntriesPtr uintptr

	// CalleeFunctionIndex is the byte offset of the callee's function index
	// (ACI_CALLEE_FUNC_INDEX), used to index CTX_CI_BASES_BY_FUNC.
	CalleeFunctionIndex uintptr

	// SizeShift is the log2 of sizeof(asmCallInfo) so the dispatcher can index per-site
	// entries as (siteIndex << SizeShift).
	SizeShift int
}

// VarLocationOffsets carries varLocation field offsets read by the return handler to
// materialise the caller's destination register kind.
type VarLocationOffsets struct {
	// UpvalueIndex is the byte offset of the UpvalueIndex field.
	UpvalueIndex uintptr

	// Register is the byte offset of the register index field.
	Register uintptr

	// Kind is the byte offset of the register-bank kind field.
	Kind uintptr

	// IsUpvalue is the byte offset of the IsUpvalue flag.
	IsUpvalue uintptr

	// Size is the byte size of one varLocation, the stride the inline multi-return handler
	// walks the return destination slice with.
	Size uintptr
}

// offsetDefineEntry is a single #define name/value pair.
type offsetDefineEntry struct {
	// name is the macro identifier emitted after "#define ".
	name string

	// value is the integer value the macro expands to.
	value uintptr
}

// offsetDefineList is an ordered slice of offset #define entries used to format aligned
// #define columns in the generated header.
type offsetDefineList []offsetDefineEntry

// add appends a #define entry to the list.
//
// Takes name (string) which is the #define identifier.
//
// Takes value (uintptr) which is the offset value.
func (list *offsetDefineList) add(name string, value uintptr) {
	*list = append(*list, offsetDefineEntry{name: name, value: value})
}

// format returns the #define block with names padded so value columns align.
//
// Returns string which is the formatted block.
func (list offsetDefineList) format() string {
	width := 0
	for _, entry := range list {
		if len(entry.name) > width {
			width = len(entry.name)
		}
	}
	var builder strings.Builder
	for _, entry := range list {
		builder.WriteString(formatDefineLine(entry.name, entry.value, width))
		builder.WriteString("\n")
	}
	return builder.String()
}

// formatExcept returns the #define block excluding any entry whose name matches
// excludeName.
//
// Takes excludeName (string) which is the entry name to omit.
//
// Returns string which is the formatted block.
func (list offsetDefineList) formatExcept(excludeName string) string {
	width := 0
	for _, entry := range list {
		if entry.name == excludeName {
			continue
		}
		if len(entry.name) > width {
			width = len(entry.name)
		}
	}
	var builder strings.Builder
	for _, entry := range list {
		if entry.name == excludeName {
			continue
		}
		builder.WriteString(formatDefineLine(entry.name, entry.value, width))
		builder.WriteString("\n")
	}
	return builder.String()
}

// lineFor returns a single formatted #define line for the named entry, or "" when no
// entry matches.
//
// Takes name (string) which is the entry name to look up.
//
// Returns string which is the formatted line, or "" when no entry matches.
func (list offsetDefineList) lineFor(name string) string {
	for _, entry := range list {
		if entry.name == name {
			return formatDefineLine(entry.name, entry.value, len(entry.name))
		}
	}
	return ""
}

// HeaderFiles returns the HeaderFile definitions for the engine dispatch headers. The
// offsets are supplied by the caller so the emitted asm_dispatch_offsets.h is always
// derived from the current Go struct layout rather than hardcoded literals.
//
// Takes contextOffsets (*DispatchContextOffsets) which supplies core and extended
// dispatchContext field positions.
// Takes frameOffsets (*CallFrameOffsets) which supplies callFrame size and field
// positions.
// Takes callInfoOffsets (*ASMCallInfoOffsets) which supplies asmCallInfo field positions.
// Takes varLocationOffsets (VarLocationOffsets) which supplies varLocation field
// positions.
//
// Returns []asmgen.HeaderFile containing the offset and macro header definitions.
func HeaderFiles(
	contextOffsets *DispatchContextOffsets,
	frameOffsets *CallFrameOffsets,
	callInfoOffsets *ASMCallInfoOffsets,
	varLocationOffsets VarLocationOffsets,
) []asmgen.HeaderFile {
	return []asmgen.HeaderFile{
		{
			Name: "asm_dispatch_offsets.h",
			Dir:  dispatchOutputDir,
			Emit: func(_ []asmgen.ArchitecturePort) string {
				return emitDispatchOffsetsHeader(
					contextOffsets,
					frameOffsets,
					callInfoOffsets,
					varLocationOffsets,
				)
			},
		},
		{
			Name: "asm_dispatch_amd64.h",
			Dir:  dispatchOutputDir,
			Emit: func(archs []asmgen.ArchitecturePort) string {
				return dispatchMacrosForArchitecture(archs, asmgen.ArchitectureAMD64)
			},
		},
		{
			Name: "asm_dispatch_arm64.h",
			Dir:  dispatchOutputDir,
			Emit: func(archs []asmgen.ArchitecturePort) string {
				return dispatchMacrosForArchitecture(archs, asmgen.ArchitectureARM64)
			},
		},
	}
}

// dispatchMacrosForArchitecture finds the BytecodeArchitecturePort matching target and
// returns its dispatch macros, or "" when no match is found.
//
// Takes archs ([]asmgen.ArchitecturePort) which is the set of registered architecture
// ports.
// Takes target (asmgen.Architecture) which identifies the architecture to match.
//
// Returns string containing the dispatch macros for the matched architecture.
func dispatchMacrosForArchitecture(archs []asmgen.ArchitecturePort, target asmgen.Architecture) string {
	for _, arch := range archs {
		if arch.Arch() != target {
			continue
		}
		if bytecodeArch, ok := arch.(BytecodeArchitecturePort); ok {
			return bytecodeArch.DispatchMacros()
		}
	}
	return ""
}

// emitDispatchOffsetsHeader returns the full dispatch offsets header content.
//
// Takes contextOffsets (*DispatchContextOffsets) which supplies dispatchContext field
// positions.
// Takes frameOffsets (*CallFrameOffsets) which supplies callFrame size and field
// positions.
// Takes callInfoOffsets (*ASMCallInfoOffsets) which supplies asmCallInfo field positions.
// Takes varLocationOffsets (VarLocationOffsets) which supplies varLocation field
// positions.
//
// Returns string containing the assembled header.
func emitDispatchOffsetsHeader(
	contextOffsets *DispatchContextOffsets,
	frameOffsets *CallFrameOffsets,
	callInfoOffsets *ASMCallInfoOffsets,
	varLocationOffsets VarLocationOffsets,
) string {
	return emitOffsetsLicenceAndPreamble() +
		emitOffsetsExitReasons() +
		emitOffsetsCallFrame(frameOffsets) +
		emitOffsetsDispatchContextCore(contextOffsets) +
		emitOffsetsDispatchContextExtended(contextOffsets) +
		emitOffsetsASMCallInfo(callInfoOffsets) +
		emitOffsetsVarLocation(varLocationOffsets)
}

// emitOffsetsDispatchContextCore returns the CTX_* #define block for the core
// dispatchContext field offsets (CodeBase, ProgramCounter, constant-pool pointers,
// JumpTable, exit reason/PC).
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns string containing the formatted #define block.
func emitOffsetsDispatchContextCore(offsets *DispatchContextOffsets) string {
	defines := offsetDefineList{}
	defines.add("CTX_CODE_BASE", offsets.CodeBase)
	defines.add("CTX_CODE_LEN", offsets.CodeLength)
	defines.add("CTX_PC", offsets.ProgramCounter)
	defines.add("CTX_INTS_BASE", offsets.IntsBase)
	defines.add("CTX_INTS_LEN", offsets.IntsLength)
	defines.add("CTX_FLOATS_BASE", offsets.FloatsBase)
	defines.add("CTX_FLOATS_LEN", offsets.FloatsLength)
	defines.add("CTX_INT_CONSTS_BASE", offsets.IntConstantsBase)
	defines.add("CTX_INT_CONSTS_LEN", offsets.IntConstantsLength)
	defines.add("CTX_FLT_CONSTS_BASE", offsets.FloatConstantsBase)
	defines.add("CTX_FLT_CONSTS_LEN", offsets.FloatConstantsLength)
	defines.add("CTX_JUMP_TABLE", offsets.JumpTable)
	defines.add("CTX_EXIT_REASON", offsets.ExitReason)
	defines.add("CTX_EXIT_PC", offsets.ExitProgramCounter)

	return "// DispatchContext core field offsets, used by DISPATCH_NEXT and the handlers.\n" +
		defines.format()
}

// emitOffsetsLicenceAndPreamble returns the Apache 2.0 licence header and instruction
// encoding comment that appear at the top of the generated offsets header file.
//
// Returns string containing the licence block and encoding preamble.
func emitOffsetsLicenceAndPreamble() string {
	return `// Code generated by cmd/asmgen; DO NOT EDIT.

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

// Architecture-independent constants shared by the amd64 and arm64 dispatch assembly.
// Offsets come from unsafe.Offsetof at generation time. TestABIOffsetsGolden pins the Go
// layouts, and make generate-asmgen-validate catches a stale header.

// Each instruction is 4 bytes, {Op, A, B, C} as uint8.

`
}

// emitOffsetsExitReasons returns the exit reason #define block.
//
// Returns string containing the EXIT_* #define block.
func emitOffsetsExitReasons() string {
	return `// Exit reasons.
#define EXIT_END_OF_CODE    0
#define EXIT_TIER2          1
#define EXIT_DIV_BY_ZERO    2
#define EXIT_CALL           3
#define EXIT_RETURN         4
#define EXIT_RETURN_VOID    5
#define EXIT_TAIL_CALL      6
#define EXIT_CALL_OVERFLOW  7
#define EXIT_POLL           29

`
}

// addFunctionTableDefines appends the FN_* and SLE_* defines that locate the per-function
// tables assembly reads through CF_FUNCTION.
//
// Takes defines (*offsetDefineList) which receives the new entries.
// Takes offsets (*CallFrameOffsets) which supplies the field positions.
func addFunctionTableDefines(defines *offsetDefineList, offsets *CallFrameOffsets) {
	defines.add("FN_STRUCT_LAYOUT_TABLE", offsets.FunctionStructLayoutTable)
	defines.add("FN_STRUCT_LAYOUT_TABLE_LEN", offsets.FunctionStructLayoutTableLen)
	defines.add("FN_TYPE_TABLE", offsets.FunctionTypeTable)
	defines.add("FN_TYPE_TABLE_LEN", offsets.FunctionTypeTableLen)
	defines.add("FN_RESULT_KINDS", offsets.FunctionResultKinds)
	defines.add("FN_RESULT_KINDS_LEN", offsets.FunctionResultKindsLen)
	defines.add("FN_STRUCT_LITERAL_TABLE", offsets.FunctionStructLiteralTableBase)
	defines.add("FN_STRUCT_LITERAL_TABLE_LEN", offsets.FunctionStructLiteralTableLen)
	defines.add("SLE_SIZE", offsets.StructLiteralEntrySize)
	defines.add("SLE_TYPE_WORD", offsets.StructLiteralEntryTypeWord)
	defines.add("SLE_BYTES", offsets.StructLiteralEntryBytes)
	defines.add("SLE_ALIGN_MASK", offsets.StructLiteralEntryAlignMask)
	defines.add("SLE_FLAG", offsets.StructLiteralEntryFlag)
}

// addCallFrameRegisterBankDefines appends the CF_REGS_* pointer, length and capacity
// defines for every register bank of the callFrame.
//
// Takes defines (*offsetDefineList) which receives the new entries.
// Takes offsets (*CallFrameOffsets) which supplies the field positions.
func addCallFrameRegisterBankDefines(defines *offsetDefineList, offsets *CallFrameOffsets) {
	defines.add("CF_REGS_INTS_PTR", offsets.RegsIntsPtr)
	defines.add("CF_REGS_INTS_LEN", offsets.RegsIntsLen)
	defines.add("CF_REGS_INTS_CAP", offsets.RegsIntsCap)
	defines.add("CF_REGS_FLOATS_PTR", offsets.RegsFloatsPtr)
	defines.add("CF_REGS_FLOATS_LEN", offsets.RegsFloatsLen)
	defines.add("CF_REGS_FLOATS_CAP", offsets.RegsFloatsCap)
	defines.add("CF_REGS_STRINGS_PTR", offsets.RegsStringsPtr)
	defines.add("CF_REGS_STRINGS_LEN", offsets.RegsStringsLen)
	defines.add("CF_REGS_STRINGS_CAP", offsets.RegsStringsCap)
	defines.add("CF_REGS_GENERAL_PTR", offsets.RegsGeneralPtr)
	defines.add("CF_REGS_GENERAL_LEN", offsets.RegsGeneralLen)
	defines.add("CF_REGS_GENERAL_CAP", offsets.RegsGeneralCap)
	defines.add("CF_REGS_BOOLS_PTR", offsets.RegsBoolsPtr)
	defines.add("CF_REGS_BOOLS_LEN", offsets.RegsBoolsLen)
	defines.add("CF_REGS_BOOLS_CAP", offsets.RegsBoolsCap)
	defines.add("CF_REGS_UINTS_PTR", offsets.RegsUintsPtr)
	defines.add("CF_REGS_UINTS_LEN", offsets.RegsUintsLen)
	defines.add("CF_REGS_UINTS_CAP", offsets.RegsUintsCap)
	defines.add("CF_REGS_COMPLEX_PTR", offsets.RegsComplexPtr)
	defines.add("CF_REGS_COMPLEX_LEN", offsets.RegsComplexLen)
	defines.add("CF_REGS_COMPLEX_CAP", offsets.RegsComplexCap)
	defines.add("CF_REGS_SLICESINT_PTR", offsets.RegsSlicesIntPtr)
	defines.add("CF_REGS_SLICESINT_LEN", offsets.RegsSlicesIntLen)
	defines.add("CF_REGS_SLICESINT_CAP", offsets.RegsSlicesIntCap)
	defines.add("CF_REGS_SLICESFLOAT_PTR", offsets.RegsSlicesFloatPtr)
	defines.add("CF_REGS_SLICESFLOAT_LEN", offsets.RegsSlicesFloatLen)
	defines.add("CF_REGS_SLICESFLOAT_CAP", offsets.RegsSlicesFloatCap)
	defines.add("CF_REGS_SLICESSTRING_PTR", offsets.RegsSlicesStringPtr)
	defines.add("CF_REGS_SLICESSTRING_LEN", offsets.RegsSlicesStringLen)
	defines.add("CF_REGS_SLICESSTRING_CAP", offsets.RegsSlicesStringCap)
	defines.add("CF_REGS_SLICESBOOL_PTR", offsets.RegsSlicesBoolPtr)
	defines.add("CF_REGS_SLICESBOOL_LEN", offsets.RegsSlicesBoolLen)
	defines.add("CF_REGS_SLICESBOOL_CAP", offsets.RegsSlicesBoolCap)
	defines.add("CF_REGS_SLICESUINT_PTR", offsets.RegsSlicesUintPtr)
	defines.add("CF_REGS_SLICESUINT_LEN", offsets.RegsSlicesUintLen)
	defines.add("CF_REGS_SLICESUINT_CAP", offsets.RegsSlicesUintCap)
	defines.add("CF_REGS_SLICEBYTE_PTR", offsets.RegsSliceBytePtr)
	defines.add("CF_REGS_SLICEBYTE_LEN", offsets.RegsSliceByteLen)
	defines.add("CF_REGS_SLICEBYTE_CAP", offsets.RegsSliceByteCap)
}

// emitOffsetsCallFrame returns the CF_* #define block for callFrame size and field
// offsets, with CALLFRAME_SIZE emitted ahead of the field block.
//
// Takes offsets (*CallFrameOffsets) which supplies the frame size and field positions.
//
// Returns string containing the formatted CALLFRAME_SIZE and CF_* #define block.
func emitOffsetsCallFrame(offsets *CallFrameOffsets) string {
	defines := offsetDefineList{
		{name: "CALLFRAME_SIZE", value: offsets.Size},
	}
	addCallFrameRegisterBankDefines(&defines, offsets)
	defines.add("CF_REGS_LAST_MASK", offsets.RegsLastAllocMask)
	defines.add("CF_FUNCTION", offsets.Function)
	addFunctionTableDefines(&defines, offsets)
	defines.add("CF_SHARED_CELLS", offsets.SharedCells)
	defines.add("CF_UPVALUES_PTR", offsets.Upvalues)
	defines.add("CF_RETURNDEST_PTR", offsets.ReturnDestPtr)
	defines.add("CF_RETURNDEST_LEN", offsets.ReturnDestLen)
	defines.add("CF_RETURNDEST_CAP", offsets.ReturnDestCap)
	defines.add("CF_PROGRAM_COUNTER", offsets.ProgramCounter)
	defines.add("CF_DEFERBASE", offsets.DeferBase)
	defines.add("CF_SIMPLEDEFER", offsets.SimpleDefer)
	defines.add("SDR_ACTIVE", offsets.SimpleDeferActive)
	defines.add("CF_ARENA_SAVE", offsets.ArenaSave)
	defines.add("CF_ARENA_SAVE_GENERAL_IDX", offsets.ArenaSaveGeneralIndex)
	defines.add("CF_HAS_GENERAL_ALLOC", offsets.HasGeneralAlloc)
	defines.add("CF_ROOT_SWAPPED", offsets.RootSwapped)

	preamble := callFrameSizeComment + defines.lineFor("CALLFRAME_SIZE") + "\n" +
		"\n// callFrame field offsets.\n"
	return preamble + defines.formatExcept("CALLFRAME_SIZE") + "\n"
}

// emitOffsetsDispatchContextExtended returns the extended CTX_* #define block for
// dispatchContext fields beyond the core dispatch set.
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns string containing the extended CTX_* defines, the struct-field table, []byte
// bank and []byte arena blocks, layout constants and upvalue cell offsets.
func emitOffsetsDispatchContextExtended(offsets *DispatchContextOffsets) string {
	preamble := "// DispatchContext field offsets for inline call and return.\n"
	body := buildExtendedDispatchContextDefines(offsets).format()
	structFieldTables := emitStructFieldTableDefines(offsets)
	sliceByteBank := emitSliceByteBankDefines(offsets)
	sliceByteArena := emitSliceByteArenaDefines(offsets)
	return preamble + body + structFieldTables + sliceByteBank + sliceByteArena + "\n" + emitOffsetsStructFieldLayout() + "\n" + emitOffsetsUpvalueCell()
}

// buildExtendedDispatchContextDefines assembles the main CTX_* define block emitted at
// the top of emitOffsetsDispatchContextExtended.
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns offsetDefineList containing the extended CTX_* entries.
func buildExtendedDispatchContextDefines(offsets *DispatchContextOffsets) offsetDefineList {
	defines := offsetDefineList{}
	defines.add("CTX_ASM_CALL_INFO_BASE", offsets.AsmCallInfoBase)
	defines.add("CTX_CSTACK_BASE", offsets.CallStackBase)
	defines.add("CTX_CSTACK_LEN", offsets.CallStackLength)
	defines.add("CTX_FRAME_POINTER", offsets.FramePointer)
	defines.add("CTX_BASE_FRAME_POINTER", offsets.BaseFramePointer)
	defines.add("CTX_DEPTH_LIMIT", offsets.CallDepthLimit)
	defines.add("CTX_ARENA_INT_SLAB", offsets.ArenaIntSlab)
	defines.add("CTX_ARENA_INT_CAP", offsets.ArenaIntCapacity)
	defines.add("CTX_ARENA_INT_IDX", offsets.ArenaIntIndex)
	defines.add("CTX_ARENA_FLT_SLAB", offsets.ArenaFloatSlab)
	defines.add("CTX_ARENA_FLT_CAP", offsets.ArenaFloatCapacity)
	defines.add("CTX_ARENA_FLT_IDX", offsets.ArenaFloatIndex)
	defines.add("CTX_ARENA_STR_IDX", offsets.ArenaStringIndex)
	defines.add("CTX_ARENA_GEN_IDX", offsets.ArenaGeneralIndex)
	defines.add("CTX_ARENA_BOOL_IDX", offsets.ArenaBoolIndex)
	defines.add("CTX_ARENA_UINT_IDX", offsets.ArenaUintIndex)
	defines.add("CTX_ARENA_CPLX_IDX", offsets.ArenaComplexIndex)
	defines.add("CTX_DEFER_STACK_LEN", offsets.DeferStackLength)
	defines.add("CTX_POLL_BUDGET", offsets.PollBudget)
	defines.add("CTX_CURRENT_FRAME", offsets.CurrentFrame)
	defines.add("CTX_HAS_GOROUTINES", offsets.HasGoroutinesFlag)
	defines.add("CTX_CI_BASES_ROOT_MATCH", offsets.CallInfoBasesRootMatch)
	defines.add("CTX_CI_BASES_BY_FUNC", offsets.CallInfoBasesByFunction)
	defines.add("CTX_RET_CURSORS", offsets.ReturnBankCursors)
	defines.add("CTX_RET_INDEX", offsets.ReturnIndex)
	defines.add("CTX_RET_COUNT", offsets.ReturnCount)
	defines.add("CTX_ARENA_GENERIC_HDR", offsets.ArenaGenericBytesSlabHeader)
	defines.add("CTX_ARENA_GENERIC_IDX_PTR", offsets.ArenaGenericBytesIndexPointer)
	defines.add("CTX_ARENA_GENERIC_CHARGED", offsets.ArenaGenericBytesCharged)
	defines.add("CTX_ASM_CI_PTRS", offsets.AsmCallInfoBasesPtr)
	defines.add("CTX_DISPATCH_SAVES", offsets.DispatchSavesPtr)
	defines.add("CTX_STRINGS_BASE", offsets.StringsBase)
	defines.add("CTX_UINTS_BASE", offsets.UintsBase)
	defines.add("CTX_GENERALS_BASE", offsets.GeneralsBase)
	defines.add("CTX_BOOLS_BASE", offsets.BoolsBase)
	defines.add("CTX_ARENA_STR_SLAB", offsets.ArenaStringSlab)
	defines.add("CTX_ARENA_STR_CAP", offsets.ArenaStringCapacity)
	defines.add("CTX_ARENA_BOOL_SLAB", offsets.ArenaBoolSlab)
	defines.add("CTX_ARENA_BOOL_CAP", offsets.ArenaBoolCapacity)
	defines.add("CTX_ARENA_UINT_SLAB", offsets.ArenaUintSlab)
	defines.add("CTX_ARENA_UINT_CAP", offsets.ArenaUintCapacity)
	defines.add("CTX_SLICES_INT_BASE", offsets.SlicesIntBase)
	defines.add("CTX_SLICES_FLOAT_BASE", offsets.SlicesFloatBase)
	defines.add("CTX_SLICES_STRING_BASE", offsets.SlicesStringBase)
	defines.add("CTX_SLICES_BOOL_BASE", offsets.SlicesBoolBase)
	defines.add("CTX_SLICES_UINT_BASE", offsets.SlicesUintBase)
	defines.add("CTX_COMPLEX_BASE", offsets.ComplexBase)
	defines.add("CTX_STR_CONSTS_BASE", offsets.StringConstantsBase)
	defines.add("CTX_STR_CONSTS_LEN", offsets.StringConstantsLength)
	defines.add("CTX_BOOL_CONSTS_BASE", offsets.BoolConstantsBase)
	defines.add("CTX_BOOL_CONSTS_LEN", offsets.BoolConstantsLength)
	defines.add("CTX_UINT_CONSTS_BASE", offsets.UintConstantsBase)
	defines.add("CTX_UINT_CONSTS_LEN", offsets.UintConstantsLength)
	defines.add("CTX_SAVED_PC", offsets.SavedPC)
	defines.add("CTX_HANDLER_RESULT", offsets.HandlerResult)
	defines.add("CTX_ARENA_BANKS_DIRTY", offsets.ArenaBanksDirty)
	return defines
}

// emitStructFieldTableDefines emits the preamble and define block for the struct-field
// ASM handler table bases.
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns string containing the struct-field table #define block.
func emitStructFieldTableDefines(offsets *DispatchContextOffsets) string {
	preamble := "\n// Struct-field handler tables. structLayoutTable has one row per field-access site,\n" +
		"// indexed by body[pc].c, and typeTable holds reflect.Type slots.\n"
	defines := offsetDefineList{}
	defines.add("CTX_STRUCT_LAYOUT_TABLE_BASE", offsets.StructLayoutTableBase)
	defines.add("CTX_STRUCT_LAYOUT_TABLE_LEN", offsets.StructLayoutTableLength)
	defines.add("CTX_TYPE_TABLE_BASE", offsets.TypeTableBase)
	defines.add("CTX_TYPE_TABLE_LEN", offsets.TypeTableLength)
	return preamble + defines.format()
}

// emitSliceByteBankDefines emits the preamble and define for the typed []byte register
// bank base.
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns string containing the []byte bank #define block.
func emitSliceByteBankDefines(offsets *DispatchContextOffsets) string {
	preamble := "\n// Typed []byte register bank base. Each slot is a 24-byte slice header.\n"
	defines := offsetDefineList{}
	defines.add("CTX_SLICES_BYTE_BASE", offsets.SlicesByteBase)
	return preamble + defines.format()
}

// emitSliceByteArenaDefines emits the preamble and defines for the typed []byte arena
// slab used by the inline-call allocator.
//
// Takes offsets (*DispatchContextOffsets) which supplies the field positions.
//
// Returns string containing the []byte arena #define block.
func emitSliceByteArenaDefines(offsets *DispatchContextOffsets) string {
	preamble := "\n// Typed []byte arena slab, from which the inline-call allocator bump-allocates the\n" +
		"// callee's slicesByte bank.\n"
	defines := offsetDefineList{}
	defines.add("CTX_ARENA_GENERAL_SLAB", offsets.ArenaGeneralSlab)
	defines.add("CTX_ARENA_GENERAL_SLAB_CAP", offsets.ArenaGeneralSlabCap)
	defines.add("CTX_ARENA_SLICEBYTE_SLAB", offsets.ArenaSliceByteSlab)
	defines.add("CTX_ARENA_SLICEBYTE_CAP", offsets.ArenaSliceByteCapacity)
	defines.add("CTX_ARENA_SLICEBYTE_IDX", offsets.ArenaSliceByteIndex)
	defines.add("CTX_ARENA_SLICEINT_IDX", offsets.ArenaSliceIntIndex)
	defines.add("CTX_ARENA_SLICEFLT_IDX", offsets.ArenaSliceFloatIndex)
	defines.add("CTX_ARENA_SLICESTR_IDX", offsets.ArenaSliceStringIndex)
	defines.add("CTX_ARENA_SLICEBOOL_IDX", offsets.ArenaSliceBoolIndex)
	defines.add("CTX_ARENA_SLICEUINT_IDX", offsets.ArenaSliceUintIndex)
	return preamble + defines.format()
}

// emitOffsetsStructFieldLayout returns the static #define block covering
// structFieldLayout field offsets and reflect.Kind / reflect.Value flag constants.
//
// Returns string containing the LAYOUT_*, REFLECT_*, FLAG_* and ABI_TYPE_* #define
// blocks.
func emitOffsetsStructFieldLayout() string {
	return `// structFieldLayout is 16 bytes, pinned by TestStructFieldLayoutSize, so row i is at
// base + (i << LAYOUT_SIZE_SHIFT).
#define LAYOUT_SIZE_SHIFT 4

// structFieldLayout field offsets read by the handlers, pinned by
// TestStructFieldLayoutOffsets.
#define OFF_LAYOUT_OFFSET           0
#define OFF_LAYOUT_TYPE_INDEX       4
#define OFF_LAYOUT_KIND             11
#define OFF_LAYOUT_REGISTER_KIND    12
#define OFF_LAYOUT_FLAGS            13
#define OFF_LAYOUT_FIELD_TYPE_INDEX 14

// structFieldLayout.Flags bits, pinned by TestStructFieldLayoutFlagConstants.
#define LAYOUT_FLAG_EMBEDDED     1
#define LAYOUT_FLAG_CYCLE_BROKEN 2

// reflect.Kind values, pinned by TestReflectKindConstants.
#define REFLECT_INVALID 0
#define REFLECT_BOOL    1
#define REFLECT_INT     2
#define REFLECT_INT8    3
#define REFLECT_INT16   4
#define REFLECT_INT32   5
#define REFLECT_INT64   6
#define REFLECT_UINT    7
#define REFLECT_UINT8   8
#define REFLECT_UINT16  9
#define REFLECT_UINT32  10
#define REFLECT_UINT64  11
#define REFLECT_UINTPTR 12
#define REFLECT_FLOAT32 13
#define REFLECT_FLOAT64 14
#define REFLECT_INTERFACE 20
#define REFLECT_POINTER 22

// reflect.Value flag bits, pinned by TestReflectValueFlagConstants.
#define FLAG_KIND_MASK 0x1F
#define FLAG_INDIR     0x80
#define FLAG_ADDR      0x100

` + fmt.Sprintf(`// abi.Type field offsets, pinned by TestABITypeOffsetsMatchASMDefines.
#define ABI_TYPE_KIND_BYTE %d
#define ABI_TYPE_PTR_ELEM  %d

`, ABITypeKindByteOffset, ABITypePointerElemOffset)
}

// emitOffsetsUpvalueCell returns the static #define block covering upvalueCell field
// offsets and isa.RegisterKind enum values the tier-0 GET_UPVALUE handler reads.
//
// Returns string containing the CELL_*, REGKIND_* and DS_* #define blocks.
func emitOffsetsUpvalueCell() string {
	return `// upvalueCell field offsets, pinned by TestUpvalueCellOffsets. Indirect cells and
// non-scalar kinds take the tier-2 shim instead.
#define CELL_INT_VALUE   248
#define CELL_FLOAT_VALUE 240
#define CELL_UINT_VALUE  216
#define CELL_BOOL_VALUE  256
#define CELL_IS_INDIRECT 258

// registerKind values for the GET_UPVALUE destination bank, pinned by
// TestRegisterKindConstants.
#define REGKIND_INT   0
#define REGKIND_FLOAT 1
#define REGKIND_BOOL  4
#define REGKIND_UINT  5

// asmDispatchSave field offsets, pinned by TestASMDispatchSaveOffsets.
#define DS_CODE_BASE     0
#define DS_CODE_LEN      8
#define DS_INT_CONSTS    16
#define DS_FLOAT_CONSTS  24
#define DS_STRING_CONSTS 32
#define DS_BOOL_CONSTS   40
#define DS_UINT_CONSTS   48
#define DS_SIZE_SHIFT    6

`
}

// emitOffsetsASMCallInfo returns the ACI_* #define block for asmCallInfo field offsets
// plus ACI_SIZE_SHIFT.
//
// Takes offsets (*ASMCallInfoOffsets) which supplies the field positions.
//
// Returns string containing the ACI_* #define block.
func emitOffsetsASMCallInfo(offsets *ASMCallInfoOffsets) string {
	defines := offsetDefineList{}
	defines.add("ACI_CALLEE_FUNCTION", offsets.CalleeFunction)
	defines.add("ACI_CLOSURE_PTR", offsets.ClosurePointer)
	defines.add("ACI_CLOSURE_REG", offsets.ClosureValueRegister)
	defines.add("ACI_UPVALS_PTR", offsets.ClosureUpvaluesPtr)
	defines.add("ACI_UPVALS_LEN", offsets.ClosureUpvaluesLen)
	defines.add("ACI_UPVALS_CAP", offsets.ClosureUpvaluesCap)
	defines.add("ACI_CALLEE_BODY", offsets.CalleeBody)
	defines.add("ACI_CALLEE_BODY_LEN", offsets.CalleeBodyLen)
	defines.add("ACI_CALLEE_INT_CONSTS", offsets.CalleeIntConsts)
	defines.add("ACI_CALLEE_FLT_CONSTS", offsets.CalleeFltConsts)
	defines.add("ACI_CALLEE_NUM_INTS", offsets.CalleeNumInts)
	defines.add("ACI_CALLEE_NUM_FLOATS", offsets.CalleeNumFloats)
	defines.add("ACI_NUM_INT_ARGS", offsets.NumIntArgs)
	defines.add("ACI_INT_ARG_SRCS", offsets.IntArgSrcs)
	defines.add("ACI_NUM_FLOAT_ARGS", offsets.NumFloatArgs)
	defines.add("ACI_FLOAT_ARG_SRCS", offsets.FloatArgSrcs)
	defines.add("ACI_NUM_RETURNS", offsets.NumReturns)
	defines.add("ACI_RET_DEST_KIND", offsets.RetDestKind)
	defines.add("ACI_RET_DEST_REG", offsets.RetDestReg)
	defines.add("ACI_RET_DEST_PTR", offsets.RetDestPtr)
	defines.add("ACI_RET_DEST_LEN", offsets.RetDestLen)
	defines.add("ACI_CALLEE_CALL_INFO", offsets.CalleeCallInfo)
	defines.add("ACI_IS_FAST_PATH", offsets.IsFastPath)
	defines.add("ACI_CALLEE_NUM_STRINGS", offsets.CalleeNumStrings)
	defines.add("ACI_CALLEE_NUM_BOOLS", offsets.CalleeNumBools)
	defines.add("ACI_CALLEE_NUM_UINTS", offsets.CalleeNumUints)
	defines.add("ACI_NUM_STRING_ARGS", offsets.NumStringArgs)
	defines.add("ACI_STRING_ARG_SRCS", offsets.StringArgSrcs)
	defines.add("ACI_NUM_BOOL_ARGS", offsets.NumBoolArgs)
	defines.add("ACI_BOOL_ARG_SRCS", offsets.BoolArgSrcs)
	defines.add("ACI_NUM_UINT_ARGS", offsets.NumUintArgs)
	defines.add("ACI_UINT_ARG_SRCS", offsets.UintArgSrcs)
	defines.add("ACI_CALLEE_STR_CONSTS", offsets.CalleeStrConsts)
	defines.add("ACI_CALLEE_UINT_CONSTS", offsets.CalleeUintConsts)
	defines.add("ACI_CALLEE_BOOL_CONSTS", offsets.CalleeBoolConsts)
	defines.add("ACI_CALLEE_NUM_GENERAL", offsets.CalleeNumGeneral)
	defines.add("ACI_NUM_GENERAL_ARGS", offsets.NumGeneralArgs)
	defines.add("ACI_GENERAL_ARG_SRCS", offsets.GeneralArgSrcs)
	defines.add("ACI_CALLEE_NUM_SLICEBYTE", offsets.CalleeNumSliceByte)
	defines.add("ACI_NUM_SLICEBYTE_ARGS", offsets.NumSliceByteArgs)
	defines.add("ACI_SLICEBYTE_ARG_SRCS", offsets.SliceByteArgSrcs)
	addASMCallInfoMethodDefines(&defines, offsets)

	return "// asmCallInfo field offsets, pinned by TestASMCallInfoOffsets.\n" +
		defines.format() + "\n" +
		fmt.Sprintf("// asmCallInfo is %d bytes.\n", 1<<offsets.SizeShift) +
		fmt.Sprintf("#define ACI_SIZE_SHIFT %d\n\n", offsets.SizeShift)
}

// addASMCallInfoMethodDefines appends the asmCallInfo defines used by the inline
// method-call handler and the per-function table-base lookup.
//
// Takes defines (*offsetDefineList) which receives the new entries.
// Takes offsets (*ASMCallInfoOffsets) which supplies the field positions.
func addASMCallInfoMethodDefines(defines *offsetDefineList, offsets *ASMCallInfoOffsets) {
	defines.add("ACI_RECEIVER_REG", offsets.ReceiverValueRegister)
	defines.add("ACI_METHOD_ENTRY_COUNT", offsets.MethodEntryCount)
	defines.add("ACI_METHOD_TYPE_WORDS", offsets.MethodTypeWords)
	defines.add("ACI_METHOD_ENTRIES_PTR", offsets.MethodEntriesPtr)
	defines.add("ACI_CALLEE_FUNC_INDEX", offsets.CalleeFunctionIndex)
}

// emitOffsetsVarLocation returns the VL_* #define block for varLocation field offsets.
//
// Takes offsets (VarLocationOffsets) which supplies the field positions.
//
// Returns string containing the VL_* #define block.
func emitOffsetsVarLocation(offsets VarLocationOffsets) string {
	defines := offsetDefineList{}
	defines.add("VL_UPVALUE_INDEX", offsets.UpvalueIndex)
	defines.add("VL_REGISTER", offsets.Register)
	defines.add("VL_KIND", offsets.Kind)
	defines.add("VL_IS_UPVALUE", offsets.IsUpvalue)
	defines.add("VL_SIZE", offsets.Size)

	return "// varLocation field offsets.\n" + defines.format()
}

// formatDefineLine formats one "#define NAME value" line with column-aligned padding.
//
// Takes name (string) which is the macro identifier.
// Takes value (uintptr) which is the integer the macro expands to.
// Takes nameWidth (int) which is the column width to pad the name to.
//
// Returns string containing the formatted #define line.
func formatDefineLine(name string, value uintptr, nameWidth int) string {
	padding := max(nameWidth-len(name), 0)
	return "#define " + name + strings.Repeat(" ", padding+1) + fmt.Sprintf("%d", value)
}
