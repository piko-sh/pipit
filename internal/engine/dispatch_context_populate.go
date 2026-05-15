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

import (
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
)

// buildDispatchContext populates a dispatchContext from the current VM frame state. The
// context is only valid for the lifetime of the current frame; after isa.SubOpCall or
// opReturn it must be rebuilt.
//
// Takes ctx (*dispatchContext) which is the dispatch context struct to populate with
// current frame state.
// Takes jumpTable (*[opcodeTableSize]uintptr) which is the opcode dispatch table mapping
// opcodes to handler addresses.
func (vm *VM) buildDispatchContext(ctx *dispatchContext, jumpTable *[opcodeTableSize]uintptr) {
	frame := &vm.CallStack[vm.FramePointer]
	body := frame.Function.Body
	registers := &frame.Registers

	if len(body) > 0 {
		ctx.codeBase = uintptr(unsafe.Pointer(&body[0]))
	}
	ctx.codeLength = int64(len(body))
	ctx.programCounter = int64(frame.ProgramCounter)

	populateRegisterBases(ctx, registers)
	populateConstantTableBases(ctx, frame.Function)
	populateAuxiliaryTableBases(ctx, frame.Function)

	if jumpTable != nil {
		ctx.jumpTable = uintptr(unsafe.Pointer(&jumpTable[0]))
	}

	ctx.exitReason = 0
	ctx.exitProgramCounter = 0

	ctx.callStackBase = uintptr(unsafe.Pointer(&vm.CallStack[0]))
	ctx.callStackLength = int64(len(vm.CallStack))
	ctx.framePointer = int64(vm.FramePointer)
	if fp := vm.FramePointer; fp >= 0 && fp < len(vm.CallStack) {
		ctx.currentFrame = uintptr(unsafe.Pointer(&vm.CallStack[fp]))
	}
	ctx.baseFramePointer = int64(vm.baseFramePointer)
	ctx.callDepthLimit = int64(vm.callDepthLimit())
	ctx.deferStackLength = int64(len(vm.deferStack))

	vm.populateArenaContext(ctx)
	vm.populateExtendedBases(ctx, registers)
}

// populateExtendedBases writes string, uint, and bool register base pointers plus ASM
// metadata pointers into the dispatch context.
//
// Takes ctx (*dispatchContext) which is the dispatch context to populate with extended
// register base pointers.
// Takes registers (*Registers) which provides the string, uint, and bool register slices.
func (vm *VM) populateExtendedBases(ctx *dispatchContext, registers *Registers) {
	if len(registers.Strings) > 0 {
		ctx.stringsBase = uintptr(unsafe.Pointer(&registers.Strings[0]))
	}
	if len(registers.Uints) > 0 {
		ctx.uintsBase = uintptr(unsafe.Pointer(&registers.Uints[0]))
	}
	if len(registers.General) > 0 {
		ctx.generalsBase = uintptr(unsafe.Pointer(&registers.General[0]))
	}
	if len(registers.Bools) > 0 {
		ctx.boolsBase = uintptr(unsafe.Pointer(&registers.Bools[0]))
	}
	if len(registers.SlicesInt) > 0 {
		ctx.slicesIntBase = uintptr(unsafe.Pointer(&registers.SlicesInt[0]))
	}
	if len(registers.slicesFloat) > 0 {
		ctx.slicesFloatBase = uintptr(unsafe.Pointer(&registers.slicesFloat[0]))
	}
	if len(registers.slicesString) > 0 {
		ctx.slicesStringBase = uintptr(unsafe.Pointer(&registers.slicesString[0]))
	}
	if len(registers.slicesBool) > 0 {
		ctx.slicesBoolBase = uintptr(unsafe.Pointer(&registers.slicesBool[0]))
	}
	if len(registers.slicesUint) > 0 {
		ctx.slicesUintBase = uintptr(unsafe.Pointer(&registers.slicesUint[0]))
	}
	if len(registers.slicesByte) > 0 {
		ctx.slicesByteBase = uintptr(unsafe.Pointer(&registers.slicesByte[0]))
	}
	if len(registers.Complex) > 0 {
		ctx.complexBase = uintptr(unsafe.Pointer(&registers.Complex[0]))
	}
	ctx.vm = vm
	vm.liveCtx = ctx

	if len(vm.asmCallInfoBases) > 0 {
		ctx.asmCallInfoBasesPointer = uintptr(unsafe.Pointer(&vm.asmCallInfoBases[0]))
		ctx.asmCallInfoBase = vm.asmCallInfoBases[vm.FramePointer]
	}
	if len(vm.asmCallInfoBasesByFunction) > 0 {
		ctx.callInfoBasesByFunction = uintptr(unsafe.Pointer(&vm.asmCallInfoBasesByFunction[0]))
	}
	if vm.rootFunction == vm.asmCallInfoRoot {
		ctx.callInfoBasesRootMatch = 1
	} else {
		ctx.callInfoBasesRootMatch = 0
	}
	if len(vm.asmDispatchSaves) > 0 {
		ctx.dispatchSavesPointer = uintptr(unsafe.Pointer(&vm.asmDispatchSaves[0]))
	}
}

// populateArenaContext writes the arena slab pointers and indices into the dispatch
// context so that ASM can allocate registers inline.
//
// Takes ctx (*dispatchContext) which is the dispatch context to populate with arena
// state.
func (vm *VM) populateArenaContext(ctx *dispatchContext) {
	if vm.Arena == nil {
		return
	}
	if len(vm.Arena.intSlab) > 0 {
		ctx.arenaIntSlab = uintptr(unsafe.Pointer(&vm.Arena.intSlab[0]))
	}
	ctx.arenaIntCapacity = int64(len(vm.Arena.intSlab))
	ctx.arenaIntIndex = int64(vm.Arena.IntIndex)
	if len(vm.Arena.floatSlab) > 0 {
		ctx.arenaFloatSlab = uintptr(unsafe.Pointer(&vm.Arena.floatSlab[0]))
	}
	ctx.arenaFloatCapacity = int64(len(vm.Arena.floatSlab))
	ctx.arenaFloatIndex = int64(vm.Arena.FloatIndex)
	ctx.arenaStringIndex = int64(vm.Arena.StringIndex)
	ctx.arenaGeneralIndex = int64(vm.Arena.GeneralIndex)
	ctx.arenaBoolIndex = int64(vm.Arena.BoolIndex)
	ctx.arenaUintIndex = int64(vm.Arena.UintIndex)
	populateGenericBytesContext(ctx, vm.Arena)
	ctx.arenaComplexIndex = int64(vm.Arena.ComplexIndex)
	if len(vm.Arena.stringSlab) > 0 {
		ctx.arenaStringSlab = uintptr(unsafe.Pointer(&vm.Arena.stringSlab[0]))
	}
	ctx.arenaStringCapacity = int64(len(vm.Arena.stringSlab))
	if len(vm.Arena.boolSlab) > 0 {
		ctx.arenaBoolSlab = uintptr(unsafe.Pointer(&vm.Arena.boolSlab[0]))
	}
	ctx.arenaBoolCapacity = int64(len(vm.Arena.boolSlab))
	if len(vm.Arena.uintSlab) > 0 {
		ctx.arenaUintSlab = uintptr(unsafe.Pointer(&vm.Arena.uintSlab[0]))
	}
	ctx.arenaUintCapacity = int64(len(vm.Arena.uintSlab))
	if len(vm.Arena.generalSlab) > 0 {
		ctx.arenaGeneralSlab = uintptr(unsafe.Pointer(&vm.Arena.generalSlab[0]))
	}
	ctx.arenaGeneralSlabCap = int64(len(vm.Arena.generalSlab))
	ctx.arenaSliceByteIndex = int64(vm.Arena.SlicesByteIndex)
	if len(vm.Arena.slicesByteSlab) > 0 {
		ctx.arenaSliceByteSlab = uintptr(unsafe.Pointer(&vm.Arena.slicesByteSlab[0]))
	}
	ctx.arenaSliceByteCapacity = int64(len(vm.Arena.slicesByteSlab))
	ctx.arenaSliceIntIndex = int64(vm.Arena.SlicesIntIndex)
	ctx.arenaSliceFloatIndex = int64(vm.Arena.SlicesFloatIndex)
	ctx.arenaSliceStringIndex = int64(vm.Arena.SlicesStringIndex)
	ctx.arenaSliceBoolIndex = int64(vm.Arena.SlicesBoolIndex)
	ctx.arenaSliceUintIndex = int64(vm.Arena.SlicesUintIndex)
	ctx.arenaBanksDirty = 0
	ctx.arenaSlabGenerationSeen = vm.Arena.slabGeneration
}

// syncCallContextFromASM updates VM state from the dispatchContext after the ASM loop
// returns. Arena cursor write-backs are gated on ctx.arenaBanksDirty.
//
// Takes ctx (*dispatchContext) which is the dispatch context containing the updated ASM
// state.
func (vm *VM) syncCallContextFromASM(ctx *dispatchContext) {
	newFp := int(ctx.framePointer)
	vm.applyPoppedFrameSnapshots(newFp)
	vm.FramePointer = newFp
	vm.publishCallInfoRoot(ctx)
	if vm.Arena != nil && ctx.arenaBanksDirty != 0 {
		syncArenaIndicesFromASM(vm.Arena, ctx)
		ctx.arenaBanksDirty = 0
	}
}

// applyPoppedFrameSnapshots restores popped-frame dispatch state.
//
// ASM-side inline return handlers (handlerReturnInline / handlerReturnVoidInline) update
// CTX_FRAME_POINTER and arena indices without going through Go's vm.popFrame; the Go-side
// vm.RootFunction / vm.functions tables and the parallel rootSnapshots slice are
// untouched. When a popped frame had a non-nil snapshot (recorded because a cross-bundle
// closure or external-method swap happened during its push), the caller's dispatch tables
// would otherwise remain pointing at the callee's bundle. Walk the popped range
// oldest-to-newest so restoration matches an equivalent series of vm.popFrame calls.
//
// Takes newFp (int) which is the new frame-pointer value after the ASM-side pop.
func (vm *VM) applyPoppedFrameSnapshots(newFp int) {
	if vm.FramePointer <= newFp || newFp < -1 {
		return
	}
	for fp := newFp + 1; fp <= vm.FramePointer && fp < len(vm.rootSnapshots); fp++ {
		snapshot := vm.rootSnapshots[fp]
		if snapshot == nil {
			continue
		}
		vm.restoreRootSnapshot(snapshot)
		vm.rootSnapshots[fp] = nil
		if fp < len(vm.CallStack) {
			vm.CallStack[fp].rootSwapped = false
		}
	}
}

// publishCallInfoRoot republishes to ctx whether the VM's current root is the one the
// per-function call-info base array was built for, together with that array, after a
// Go-side step may have swapped or restored the root.
//
// Takes ctx (*dispatchContext) which is the live dispatch context.
func (vm *VM) publishCallInfoRoot(ctx *dispatchContext) {
	if len(vm.asmCallInfoBasesByFunction) > 0 {
		ctx.callInfoBasesByFunction = uintptr(unsafe.Pointer(&vm.asmCallInfoBasesByFunction[0]))
	}
	if vm.rootFunction == vm.asmCallInfoRoot {
		ctx.callInfoBasesRootMatch = 1
	} else {
		ctx.callInfoBasesRootMatch = 0
	}
}

// refreshCallContext updates the call-related fields in the dispatchContext after a
// Go-side frame change (push/pop).
//
// Takes ctx (*dispatchContext) which is the dispatch context to refresh with current call
// stack state.
func (vm *VM) refreshCallContext(ctx *dispatchContext) {
	ctx.callStackBase = uintptr(unsafe.Pointer(&vm.CallStack[0]))
	ctx.callStackLength = int64(len(vm.CallStack))
	ctx.framePointer = int64(vm.FramePointer)
	if fp := vm.FramePointer; fp >= 0 && fp < len(vm.CallStack) {
		ctx.currentFrame = uintptr(unsafe.Pointer(&vm.CallStack[fp]))
	}
	ctx.deferStackLength = int64(len(vm.deferStack))
	if vm.hasGoroutines {
		ctx.hasGoroutinesFlag = 1
	} else {
		ctx.hasGoroutinesFlag = 0
	}
	vm.publishCallInfoRoot(ctx)
	if vm.Arena != nil {
		vm.refreshArenaSlabs(ctx)
	}
	if len(vm.asmCallInfoBases) > 0 {
		if vm.FramePointer >= 0 && vm.FramePointer < len(vm.asmCallInfoBases) {
			ctx.asmCallInfoBase = vm.asmCallInfoBases[vm.FramePointer]
		}
		ctx.asmCallInfoBasesPointer = uintptr(unsafe.Pointer(&vm.asmCallInfoBases[0]))
	}
	if len(vm.asmDispatchSaves) > 0 {
		ctx.dispatchSavesPointer = uintptr(unsafe.Pointer(&vm.asmDispatchSaves[0]))
	}
}

// refreshArenaSlabs updates the arena slab pointers, capacities, and indices in the
// dispatch context from the current arena state.
//
// Takes ctx (*dispatchContext) which is the dispatch context to refresh with current
// arena slab state.
func (vm *VM) refreshArenaSlabs(ctx *dispatchContext) {
	arena := vm.Arena
	ctx.arenaIntIndex = int64(arena.IntIndex)
	ctx.arenaFloatIndex = int64(arena.FloatIndex)
	ctx.arenaStringIndex = int64(arena.StringIndex)
	ctx.arenaGeneralIndex = int64(arena.GeneralIndex)
	ctx.arenaBoolIndex = int64(arena.BoolIndex)
	ctx.arenaUintIndex = int64(arena.UintIndex)
	ctx.arenaComplexIndex = int64(arena.ComplexIndex)
	ctx.arenaSliceByteIndex = int64(arena.SlicesByteIndex)
	ctx.arenaSliceIntIndex = int64(arena.SlicesIntIndex)
	ctx.arenaSliceFloatIndex = int64(arena.SlicesFloatIndex)
	ctx.arenaSliceStringIndex = int64(arena.SlicesStringIndex)
	ctx.arenaSliceBoolIndex = int64(arena.SlicesBoolIndex)
	ctx.arenaSliceUintIndex = int64(arena.SlicesUintIndex)
	populateGenericBytesContext(ctx, arena)
	ctx.arenaBanksDirty = 0
	if ctx.arenaSlabGenerationSeen == arena.slabGeneration {
		return
	}
	ctx.arenaSlabGenerationSeen = arena.slabGeneration
	if len(arena.intSlab) > 0 {
		ctx.arenaIntSlab = uintptr(unsafe.Pointer(&arena.intSlab[0]))
	}
	ctx.arenaIntCapacity = int64(len(arena.intSlab))
	if len(arena.floatSlab) > 0 {
		ctx.arenaFloatSlab = uintptr(unsafe.Pointer(&arena.floatSlab[0]))
	}
	ctx.arenaFloatCapacity = int64(len(arena.floatSlab))
	if len(arena.stringSlab) > 0 {
		ctx.arenaStringSlab = uintptr(unsafe.Pointer(&arena.stringSlab[0]))
	}
	ctx.arenaStringCapacity = int64(len(arena.stringSlab))
	if len(arena.boolSlab) > 0 {
		ctx.arenaBoolSlab = uintptr(unsafe.Pointer(&arena.boolSlab[0]))
	}
	ctx.arenaBoolCapacity = int64(len(arena.boolSlab))
	if len(arena.uintSlab) > 0 {
		ctx.arenaUintSlab = uintptr(unsafe.Pointer(&arena.uintSlab[0]))
	}
	ctx.arenaUintCapacity = int64(len(arena.uintSlab))
	if len(arena.slicesByteSlab) > 0 {
		ctx.arenaSliceByteSlab = uintptr(unsafe.Pointer(&arena.slicesByteSlab[0]))
	}
	ctx.arenaSliceByteCapacity = int64(len(arena.slicesByteSlab))
	if len(arena.generalSlab) > 0 {
		ctx.arenaGeneralSlab = uintptr(unsafe.Pointer(&arena.generalSlab[0]))
	}
	ctx.arenaGeneralSlabCap = int64(len(arena.generalSlab))
}

// saveCurrentDispatchRegisters writes the current frame's dispatch register values into
// asmDispatchSaves[vm.FramePointer]. Every live frame must have its slot written before
// anything can be pushed above it, because the inline return handler restores the
// caller's code and constant bases from the slot.
//
// Takes ctx (*dispatchContext) which is the dispatch context containing the current
// register values to save.
func (vm *VM) saveCurrentDispatchRegisters(ctx *dispatchContext) {
	vm.saveDispatchRegistersForFrame(ctx, vm.FramePointer)
}

// saveDispatchRegistersForFrame writes the dispatch register values held in ctx into the
// save slot of the frame at framePointer.
//
// Out-of-range frame pointers and a nil save table are ignored, so callers on the exit
// path need no guard of their own.
//
// Takes ctx (*dispatchContext) which holds the code and constant bases to save.
// Takes framePointer (int) which is the frame whose slot receives them.
//
//go:nosplit
func (vm *VM) saveDispatchRegistersForFrame(ctx *dispatchContext, framePointer int) {
	if vm.asmDispatchSaves == nil || framePointer < 0 || framePointer >= len(vm.asmDispatchSaves) {
		return
	}
	save := &vm.asmDispatchSaves[framePointer]
	save.codeBase = ctx.codeBase
	save.codeLength = ctx.codeLength
	save.intConstantsBase = ctx.intConstantsBase
	save.uintConstantsBase = ctx.uintConstantsBase
	save.floatConstantsBase = ctx.floatConstantsBase
	save.stringConstantsBase = ctx.stringConstantsBase
	save.boolConstantsBase = ctx.boolConstantsBase
}

// populateRegisterBases writes the int and float register base pointers and their lengths
// into the dispatch context.
//
// Takes ctx (*dispatchContext) which receives the base addresses.
// Takes registers (*Registers) which is the current frame's register set.
func populateRegisterBases(ctx *dispatchContext, registers *Registers) {
	if len(registers.Ints) > 0 {
		ctx.intsBase = uintptr(unsafe.Pointer(&registers.Ints[0]))
	}
	ctx.intsLength = int64(len(registers.Ints))
	if len(registers.Floats) > 0 {
		ctx.floatsBase = uintptr(unsafe.Pointer(&registers.Floats[0]))
	}
	ctx.floatsLength = int64(len(registers.Floats))
}

// populateConstantTableBases writes the int, float, string, and bool constant-table base
// pointers and lengths into the dispatch context.
//
// Takes ctx (*dispatchContext) which receives the base addresses.
// Takes function (*CompiledFunction) which holds the constant tables.
func populateConstantTableBases(ctx *dispatchContext, function *program.CompiledFunction) {
	if len(function.IntConstants) > 0 {
		ctx.intConstantsBase = uintptr(unsafe.Pointer(&function.IntConstants[0]))
	}
	ctx.intConstantsLength = int64(len(function.IntConstants))
	if len(function.FloatConstants) > 0 {
		ctx.floatConstantsBase = uintptr(unsafe.Pointer(&function.FloatConstants[0]))
	}
	ctx.floatConstantsLength = int64(len(function.FloatConstants))
	if len(function.StringConstants) > 0 {
		ctx.stringConstantsBase = uintptr(unsafe.Pointer(&function.StringConstants[0]))
	}
	ctx.stringConstantsLength = int64(len(function.StringConstants))
	if len(function.UintConstants) > 0 {
		ctx.uintConstantsBase = uintptr(unsafe.Pointer(&function.UintConstants[0]))
	}
	ctx.uintConstantsLength = int64(len(function.UintConstants))
	if len(function.BoolConstants) > 0 {
		ctx.boolConstantsBase = uintptr(unsafe.Pointer(&function.BoolConstants[0]))
	}
	ctx.boolConstantsLength = int64(len(function.BoolConstants))
}

// populateAuxiliaryTableBases writes the struct-layout and type-table base pointers and
// lengths into the dispatch context. These fields are shared with rebuildDispatchPointers
// and must be initialised on the first dispatch entry so trampolines reading
// ctx.structLayoutTableBase do not dereference a bogus pointer.
//
// Takes ctx (*dispatchContext) which receives the base addresses.
// Takes function (*CompiledFunction) which holds the layout tables.
func populateAuxiliaryTableBases(ctx *dispatchContext, function *program.CompiledFunction) {
	if len(function.StructLayoutTable) > 0 {
		ctx.structLayoutTableBase = uintptr(unsafe.Pointer(&function.StructLayoutTable[0]))
	}
	ctx.structLayoutTableLength = int64(len(function.StructLayoutTable))
	if len(function.TypeTable) > 0 {
		ctx.typeTableBase = uintptr(unsafe.Pointer(&function.TypeTable[0]))
	}
	ctx.typeTableLength = int64(len(function.TypeTable))
}
