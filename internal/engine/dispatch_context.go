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

package engine

// dispatchContext is the per-goroutine VM dispatch state shared with ASM, laid out for
// cache locality so reordering fields requires regenerating asm_dispatch_offsets.h.
//
//nolint:govet // Reason above.
type dispatchContext struct {
	// vm is a back-pointer to the executing VM, used by Go-side trampolines. Not read by
	// ASM.
	vm *VM

	// codeBase is the pointer to the first instruction in the compiled function body
	// (unsafe.Pointer to &body[0]).
	codeBase uintptr

	// codeLength is the number of instructions in the body.
	codeLength int64

	// programCounter is the current program counter (instruction index). Updated by ASM
	// before returning to Go.
	programCounter int64

	// intsBase is the pointer to the int64 register bank (unsafe.Pointer to
	// &registers.Ints[0]).
	intsBase uintptr

	// intsLength is the number of int64 registers allocated.
	intsLength int64

	// floatsBase is the pointer to the float64 register bank (unsafe.Pointer to
	// &registers.floats[0]).
	floatsBase uintptr

	// floatsLength is the number of float64 registers allocated.
	floatsLength int64

	// intConstantsBase is the pointer to the int64 constant table (unsafe.Pointer to
	// &fn.intConstants[0]).
	intConstantsBase uintptr

	// intConstantsLength is the number of int64 constants.
	intConstantsLength int64

	// floatConstantsBase is the pointer to the float64 constant table (unsafe.Pointer to
	// &fn.floatConstants[0]).
	floatConstantsBase uintptr

	// floatConstantsLength is the number of float64 constants.
	floatConstantsLength int64

	// jumpTable is the pointer to the 256-entry dispatch table (unsafe.Pointer to
	// &jumpTable[0]). Each entry is a uintptr holding the absolute address of the handler
	// for that opcode.
	jumpTable uintptr

	// exitReason is written by ASM before returning to indicate why the dispatch loop
	// exited: 0 for end of code (pc >= codeLength), 1 for a tier 2 opcode that needs Go
	// handling, and 2 for a division-by-zero error.
	exitReason int64

	// exitProgramCounter is the program counter at which the dispatch loop exited. For tier
	// 2 exits, this is the PC of the instruction that needs Go handling.
	exitProgramCounter int64

	// asmCallInfoBase is the current function's AsmCallInfo table base pointer.
	asmCallInfoBase uintptr

	// callStackBase is the pointer to the first element of the VM call stack.
	callStackBase uintptr

	// callStackLength is the number of entries in the VM call stack.
	callStackLength int64

	// framePointer is the current frame pointer index within the call stack.
	framePointer int64

	// baseFramePointer is the base frame pointer established by runDispatched.
	baseFramePointer int64

	// callDepthLimit is the maximum call depth allowed before overflow.
	callDepthLimit int64

	// arenaIntSlab is the pointer to the first element of the int register arena slab.
	arenaIntSlab uintptr

	// arenaIntCapacity is the total capacity of the int register arena slab.
	arenaIntCapacity int64

	// arenaIntIndex is the current allocation index into the int arena slab, read-write by
	// ASM.
	arenaIntIndex int64

	// arenaFloatSlab is the pointer to the first element of the float register arena slab.
	arenaFloatSlab uintptr

	// arenaFloatCapacity is the total capacity of the float register arena slab.
	arenaFloatCapacity int64

	// arenaFloatIndex is the current allocation index into the float arena slab, read-write
	// by ASM.
	arenaFloatIndex int64

	// arenaStringIndex is the current string arena allocation index, read-write by ASM.
	arenaStringIndex int64

	// arenaGeneralIndex is the current general arena allocation index, read-write by ASM:
	// the inline call's general-bank fast arm advances it and the inline return's clear
	// restores it, both marking arenaBanksDirty so syncCallContextFromASM writes it back.
	arenaGeneralIndex int64

	// arenaBoolIndex is the current bool arena allocation index, read-write by ASM.
	arenaBoolIndex int64

	// arenaUintIndex is the current uint arena allocation index, read-write by ASM.
	arenaUintIndex int64

	// arenaComplexIndex is the current complex arena allocation index, read-only by ASM.
	arenaComplexIndex int64

	// deferStackLength is the number of entries in the VM defer stack.
	deferStackLength int64

	// asmCallInfoBasesPointer is the pointer to the first element of the asmCallInfoBases
	// slice.
	asmCallInfoBasesPointer uintptr

	// dispatchSavesPointer is the pointer to the first element of the asmDispatchSaves
	// slice.
	dispatchSavesPointer uintptr

	// stringsBase is the pointer to the string register bank (unsafe.Pointer to
	// &registers.Strings[0]). Each string is 16 bytes: {Data uintptr, Len int}.
	stringsBase uintptr

	// uintsBase is the pointer to the uint64 register bank (unsafe.Pointer to
	// &registers.uints[0]).
	uintsBase uintptr

	// generalsBase is the pointer to the current frame's general register bank (24-byte
	// reflect.Value slots). ASM writes general slots only with raw three-word moves under
	// the runtime write-barrier guard.
	generalsBase uintptr

	// boolsBase is the pointer to the bool register bank (unsafe.Pointer to
	// &registers.bools[0]).
	boolsBase uintptr

	// arenaStringSlab is the pointer to the first element of the string register arena slab.
	arenaStringSlab uintptr

	// arenaStringCapacity is the total capacity of the string register arena slab.
	arenaStringCapacity int64

	// arenaBoolSlab is the pointer to the first element of the bool register arena slab.
	arenaBoolSlab uintptr

	// arenaBoolCapacity is the total capacity of the bool register arena slab.
	arenaBoolCapacity int64

	// arenaUintSlab is the pointer to the first element of the uint register arena slab.
	arenaUintSlab uintptr

	// arenaUintCapacity is the total capacity of the uint register arena slab.
	arenaUintCapacity int64

	// slicesIntBase is the pointer to the first []int64 slice header in the typed slicesInt
	// register bank (24-byte slice headers).
	slicesIntBase uintptr

	// slicesFloatBase is the pointer to the first []float64 slice header in the typed
	// slicesFloat register bank. Same 24-byte slot layout as slicesIntBase.
	slicesFloatBase uintptr

	// slicesStringBase is the pointer to the first []string slice header in the typed
	// slicesString register bank.
	slicesStringBase uintptr

	// slicesBoolBase is the pointer to the first []bool slice header in the typed slicesBool
	// register bank.
	slicesBoolBase uintptr

	// slicesUintBase is the pointer to the first []uint64 slice header in the typed
	// slicesUint register bank.
	slicesUintBase uintptr

	// complexBase is the pointer to the first complex128 element in the complex register
	// bank (16-byte slots).
	complexBase uintptr

	// stringConstantsBase is the pointer to the first 16-byte string header in the active
	// function's string constant table.
	stringConstantsBase uintptr

	// stringConstantsLength is the number of entries in the string constant table. Kept for
	// verifier panics; the runtime does not bounds-check the const-load fast path because
	// the Compiler only emits indices in range.
	stringConstantsLength int64

	// boolConstantsBase is the pointer to the first 1-byte entry in the active function's
	// bool constant table. Read by handlerLoadBoolConst.
	boolConstantsBase uintptr

	// boolConstantsLength is the number of entries in the bool constant table.
	boolConstantsLength int64

	// uintConstantsBase is the pointer to the active function's uint64 constant table. Zero
	// when the function has no uint constants.
	uintConstantsBase uintptr

	// uintConstantsLength is the informational length companion of uintConstantsBase.
	uintConstantsLength int64

	// savedPC is the spill slot for the bytecode PC across a CALL into a Go trampoline,
	// typed uintptr so the GC skips it and the handler can declare NO_LOCAL_POINTERS.
	savedPC uintptr

	// handlerResult is the OpResult byte from the most recent tier-2 trampoline. Stored on
	// ctx rather than as a Go multi-return so the shim recovers R15 with a single load.
	handlerResult uint8

	// arenaBanksDirty signals that ASM advanced the arena cursors, so syncCallContextFromASM
	// writes the cursors back. Cleared after republishing.
	arenaBanksDirty uint8

	// structLayoutTableBase is the pointer to the first structFieldLayout entry (16-byte
	// entries). Zero when the active function has no resolved struct-field layouts.
	structLayoutTableBase uintptr

	// structLayoutTableLength is the number of entries in the active function's
	// structLayoutTable, exposed as the informational length companion via the
	// CTX_STRUCT_LAYOUT_TABLE_LEN .h define (the Compiler-Emit invariant guarantees ASM-side
	// reads are in bounds).
	structLayoutTableLength int64

	// typeTableBase is the pointer to the first reflect.Type slot (16-byte interface). Zero
	// when the active function has no recorded types.
	typeTableBase uintptr

	// typeTableLength is the number of entries in the active function's typeTable. Length
	// companion to typeTableBase; same informational semantics as structLayoutTableLength.
	typeTableLength int64

	// slicesByteBase is the pointer to the first []byte slice header in the typed slicesByte
	// register bank (24-byte slice headers).
	slicesByteBase uintptr

	// arenaGeneralSlab is the pointer to RegisterArena.GeneralSlab[0].
	arenaGeneralSlab uintptr

	// arenaGeneralSlabCap is the total slot count of RegisterArena.GeneralSlab. The
	// inline-call general-bank fast arm checks (current index + requested) against this cap
	// and falls back to the Go helper when a grow would be needed.
	arenaGeneralSlabCap int64

	// arenaSliceByteSlab is the pointer to the first slot of RegisterArena.slicesByteSlab.
	arenaSliceByteSlab uintptr

	// arenaSliceByteCapacity is the total slot count of RegisterArena.slicesByteSlab. The
	// inline-call allocator checks (current index + requested) against this cap before
	// bumping.
	arenaSliceByteCapacity int64

	// arenaSliceByteIndex is the current bump position of the typed byte-slice arena slab.
	// ASM updates this in place after a successful allocation; the Go-side restore path
	// reads it to know how far the arena has advanced.
	arenaSliceByteIndex int64

	// arenaSliceIntIndex mirrors RegisterArena.SlicesIntIndex so the ASM inline-call shim
	// writes the correct save-point value into the new frame's arenaSave block.
	arenaSliceIntIndex int64

	// arenaSliceFloatIndex mirrors RegisterArena.SlicesFloatIndex.
	arenaSliceFloatIndex int64

	// arenaSliceStringIndex mirrors RegisterArena.SlicesStringIndex.
	arenaSliceStringIndex int64

	// arenaSliceBoolIndex mirrors RegisterArena.SlicesBoolIndex.
	arenaSliceBoolIndex int64

	// arenaSliceUintIndex mirrors RegisterArena.SlicesUintIndex.
	arenaSliceUintIndex int64

	// pollBudget is decremented by the ASM dispatch loop on each tier-1 back edge. When it
	// reaches zero the loop exits with exitPoll.
	pollBudget int64

	// currentFrame is the address of the active callFrame in vm.CallStack, cached so the
	// tier-2 shim trampolines resolve the frame with a single load.
	currentFrame uintptr

	// hasGoroutinesFlag mirrors vm.hasGoroutines for the ASM closure-call guard. The inline
	// call falls back to Go when set because cached upvalue cells must not be shared.
	hasGoroutinesFlag uint8

	// callInfoBasesRootMatch is 1 while callInfoBasesByFunction is the array built for the
	// VM's current root, and 0 after a root swap. When 0 the inline call uses the callee's
	// own base instead of the array, because the array's indices belong to a different root.
	callInfoBasesRootMatch uint8

	// arenaSlabGenerationSeen is the last-mirrored RegisterArena.SlabGeneration value.
	// refreshArenaSlabs() skips the slab stores while unchanged.
	arenaSlabGenerationSeen uint64

	// callInfoBasesByFunction is the pointer to the first slot of
	// VM.asmCallInfoBasesByFunction. The inline call handlers read the callee's current
	// AsmCallInfo table base from it, indexed by AsmCallInfo.calleeFunctionIndex.
	callInfoBasesByFunction uintptr

	// returnBankCursors holds, per register kind, the next callee source register the inline
	// multi-return handler copies from, matching the per-kind counters handleReturn keeps.
	// The handler mirrors them here because it has no spare registers for five counters;
	// single-value returns never touch them.
	returnBankCursors [8]int64

	// returnIndex is the return destination position the inline multi-return handler is
	// copying.
	returnIndex int64

	// returnCount is the number of results the inline multi-return handler copies.
	returnCount int64

	// arenaGenericBytesSlabHeader is the address of the RegisterArena.genericBytesSlab slice
	// header. The struct-literal allocation handler reads the slab base and length through
	// it on every allocation, so Go-side growth needs no mirror refresh.
	arenaGenericBytesSlabHeader uintptr

	// arenaGenericBytesIndexPointer is the address of RegisterArena.genericBytesIndex. The
	// struct-literal allocation handler bumps the arena's own cursor through it, which keeps
	// the arena authoritative for the Go trampolines that allocate generic bytes while the
	// assembly loop is still running (value-receiver copies, boundary copies).
	arenaGenericBytesIndexPointer uintptr

	// arenaGenericBytesCharged accumulates the bytes the struct-literal allocation handler
	// carved from the generic-bytes slab since the last write-back, so
	// syncArenaIndicesFromASM can feed RegisterArena.noteAlloc and GC pacing stays accurate.
	arenaGenericBytesCharged int64
}

// asmDispatchSave stores the dispatch register values preserved across inline
// call/return. The assembly addresses it through the DS_* defines, which
// TestASMDispatchSaveOffsets pins to these field offsets.
type asmDispatchSave struct {
	// codeBase is the pointer to the first instruction of the saved function body.
	codeBase uintptr

	// codeLength is the number of instructions in the saved function body.
	codeLength int64

	// intConstantsBase is the pointer to the saved function's int constant table.
	intConstantsBase uintptr

	// floatConstantsBase is the pointer to the saved function's float constant table.
	floatConstantsBase uintptr

	// stringConstantsBase is the pointer to the saved function's string constant table. Each
	// entry is a 16-byte Go string header.
	stringConstantsBase uintptr

	// boolConstantsBase is the pointer to the saved function's bool constant table. Each
	// entry is a single byte.
	boolConstantsBase uintptr

	// uintConstantsBase is the pointer to the saved function's uint constant table. Keeps
	// the record at 64 bytes so the ASM save/restore blocks can index records with a fixed
	// shift.
	uintConstantsBase uintptr

	// _reserved1 is reserved padding, keeping the struct size stable so the ASM offsets
	// baked into asm_dispatch_offsets.h do not shift when a new field is added.
	_reserved1 uintptr
}
