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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

// SAFETY: every trampoline in this file is called from the assembly dispatcher with the
// live dispatchContext and reads or writes register banks through the base pointers the
// context carries. The unsafe arithmetic is sound because (1) the bases were populated by
// buildDispatchContext or rebuildDispatchPointers from the current frame's slices after
// the last Go-side change, (2) register indices come from instruction operands the
// compiler bounds to the function's register counts, and (3) the backing slabs are kept
// alive by the frame's Registers and the arena's save points until the frame pops.

import (
	"math"
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/safeconv"
)

// asmCallMathSin is the trampoline for handlerSubOpMathSin.
//
// Reads floats[sourceIndex] from the float bank exposed via ctx.floatsBase, computes
// math.Sin, writes to floats[destinationIndex], and returns ctx so the ASM caller can
// Restore R15.
//
// Takes ctx (*dispatchContext) which carries the live float bank base pointer.
// Takes destinationIndex (int64) which is the float register receiving the result.
// Takes sourceIndex (int64) which is the float register holding the operand.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMathSin(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	base := unsafe.Pointer(ctx.floatsBase)
	source := (*float64)(unsafe.Add(base, sourceIndex*8))
	destination := (*float64)(unsafe.Add(base, destinationIndex*8))
	*destination = math.Sin(*source)
	return ctx
}

// asmCallMathCos is the trampoline for handlerSubOpMathCos.
//
// Reads floats[sourceIndex], computes math.Cos, writes to floats[destinationIndex], and
// returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the float bank base pointer.
// Takes destinationIndex (int64) which is the float register receiving the result.
// Takes sourceIndex (int64) which is the float register holding the operand.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMathCos(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	base := unsafe.Pointer(ctx.floatsBase)
	source := (*float64)(unsafe.Add(base, sourceIndex*8))
	destination := (*float64)(unsafe.Add(base, destinationIndex*8))
	*destination = math.Cos(*source)
	return ctx
}

// asmCallMathExp is the trampoline for handlerSubOpMathExp.
//
// Reads floats[sourceIndex], computes math.Exp, writes to floats[destinationIndex], and
// returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the float bank base pointer.
// Takes destinationIndex (int64) which is the float register receiving the result.
// Takes sourceIndex (int64) which is the float register holding the operand.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMathExp(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	base := unsafe.Pointer(ctx.floatsBase)
	source := (*float64)(unsafe.Add(base, sourceIndex*8))
	destination := (*float64)(unsafe.Add(base, destinationIndex*8))
	*destination = math.Exp(*source)
	return ctx
}

// asmCallMathTan is the trampoline for handlerSubOpMathTan.
//
// Reads floats[sourceIndex], computes math.Tan, writes to floats[destinationIndex], and
// returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the float bank base pointer.
// Takes destinationIndex (int64) which is the float register receiving the result.
// Takes sourceIndex (int64) which is the float register holding the operand.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMathTan(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	base := unsafe.Pointer(ctx.floatsBase)
	source := (*float64)(unsafe.Add(base, sourceIndex*8))
	destination := (*float64)(unsafe.Add(base, destinationIndex*8))
	*destination = math.Tan(*source)
	return ctx
}

// asmCallMathMod is the trampoline for handlerSubOpMathMod.
//
// Takes ctx (*dispatchContext) which carries the float bank base pointer.
// Takes destinationIndex (int64) which is the float register receiving the result.
// Takes source1Index (int64) which is the float register holding the dividend.
// Takes source2Index (int64) which is the float register holding the divisor.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMathMod(ctx *dispatchContext, destinationIndex, source1Index, source2Index int64) *dispatchContext {
	base := unsafe.Pointer(ctx.floatsBase)
	source1 := (*float64)(unsafe.Add(base, source1Index*8))
	source2 := (*float64)(unsafe.Add(base, source2Index*8))
	destination := (*float64)(unsafe.Add(base, destinationIndex*8))
	*destination = math.Mod(*source1, *source2)
	return ctx
}

// asmCallStrconvItoa is the trampoline for handlerSubOpStrconvItoa. The result is
// arena-backed to avoid a Go heap allocation.
//
// Takes ctx (*dispatchContext) which carries the int and string bank base pointers.
// Takes destinationIndex (int64) which is the string register receiving the result.
// Takes sourceIndex (int64) which is the int register holding the value to format.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallStrconvItoa(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	intsBase := unsafe.Pointer(ctx.intsBase)
	source := *(*int64)(unsafe.Add(intsBase, sourceIndex*8))

	stringsBase := unsafe.Pointer(ctx.stringsBase)
	destination := (*string)(unsafe.Add(stringsBase, destinationIndex*16))
	*destination = arenaItoaString(ctx.vm.Arena, source)
	return ctx
}

// asmCallStrconvFormatInt is the trampoline for handlerSubOpStrconvFormatInt. The result
// is arena-backed to avoid a Go heap allocation.
//
// Takes ctx (*dispatchContext) which carries the int and string bank base pointers.
// Takes destinationIndex (int64) which is the string register receiving the result.
// Takes source1Index (int64) which is the int register holding the value to format.
// Takes source2Index (int64) which is the int register holding the numeric base.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallStrconvFormatInt(ctx *dispatchContext, destinationIndex, source1Index, source2Index int64) *dispatchContext {
	intsBase := unsafe.Pointer(ctx.intsBase)
	source1 := *(*int64)(unsafe.Add(intsBase, source1Index*8))
	source2 := *(*int64)(unsafe.Add(intsBase, source2Index*8))

	stringsBase := unsafe.Pointer(ctx.stringsBase)
	destination := (*string)(unsafe.Add(stringsBase, destinationIndex*16))
	*destination = arenaFormatIntString(ctx.vm.Arena, source1, int(source2))
	return ctx
}

// vmRegistersForCtx returns the *Registers for the frame the VM is dispatching. It reads
// ctx.FramePointer rather than vm.FramePointer because the Go-side mirror lags during ASM
// inline calls.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer.
//
// Returns the *Registers for the frame at ctx.FramePointer in vm.CallStack.
//
//go:nosplit
func vmRegistersForCtx(ctx *dispatchContext) *Registers {
	frame := &ctx.vm.CallStack[ctx.framePointer]
	return &frame.Registers
}

// asmCallCap is the trampoline for handlerSubOpCap.
//
// Writes ints[destinationIndex] = collectionLengthOrCap(general[sourceIndex],
// reflect.Value.Cap). Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer used to locate the
// general bank.
// Takes destinationIndex (int64) which is the int register receiving the capacity.
// Takes sourceIndex (int64) which is the general register holding the collection value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallCap(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	registers.Ints[destinationIndex] = collectionLengthOrCap(registers.General[sourceIndex], reflect.Value.Cap)
	return ctx
}

// asmCallBytesToString is the trampoline for handlerSubOpBytesToString.
//
// Writes strings[destinationIndex] = arenaBytesToString(vm.arena,
// general[sourceIndex].Bytes()). Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer used to locate the
// general bank and the arena.
// Takes destinationIndex (int64) which is the string register receiving the converted
// text.
// Takes sourceIndex (int64) which is the general register holding the byte slice.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallBytesToString(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	registers.Strings[destinationIndex] = arenaBytesToString(ctx.vm.Arena, registers.General[sourceIndex].Bytes())
	return ctx
}

// asmCallBoxSliceInt is the trampoline for handlerSubOpBoxSliceInt.
//
// Writes general[destinationIndex] = reflect.ValueOf(slicesInt[sourceIndex]), boxing a
// typed-bank int slice into the general (reflect.Value) bank. Returns ctx so the ASM
// caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer used to locate both
// banks.
// Takes destinationIndex (int64) which is the general register receiving the boxed slice.
// Takes sourceIndex (int64) which is the slicesInt register holding the source slice.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallBoxSliceInt(ctx *dispatchContext, destinationIndex, sourceIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	registers.General[destinationIndex] = boxTypedIntSlice(ctx.vm, registers.SlicesInt[sourceIndex])
	return ctx
}

// asmCallMakeSliceInt is the trampoline for handlerSubOpMakeSliceInt. The backing array
// is bump-allocated from the arena's intBacking slab.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesInt register receiving the new slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceInt(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocIntBacking(int(capacity))
	clear(backing)
	registers.SlicesInt[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallMakeSliceFloat is the trampoline for handlerSubOpMakeSliceFloat.
//
// Writes slicesFloat[destinationIndex] = arena-backed make([]float64, ints[lengthIndex],
// ints[capacityIndex]). Three-operand sub-op (tier 0 arity) backed by the arena's
// floatBacking slab. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesFloat register receiving the new
// slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceFloat(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocFloatBacking(int(capacity))
	clear(backing)
	registers.slicesFloat[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallMakeSliceString is the trampoline for handlerSubOpMakeSliceString.
//
// Writes slicesString[destinationIndex] = arena-backed make([]string, ints[lengthIndex],
// ints[capacityIndex]). Three-operand sub-op (tier 0 arity) backed by the arena's
// stringBacking slab. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesString register receiving the new
// slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceString(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocStringBacking(int(capacity))
	clear(backing)
	registers.slicesString[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallMakeSliceBool is the trampoline for handlerSubOpMakeSliceBool.
//
// Writes slicesBool[destinationIndex] = arena-backed make([]bool, ints[lengthIndex],
// ints[capacityIndex]). Three-operand sub-op (tier 0 arity) backed by the arena's
// boolBacking slab. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesBool register receiving the new
// slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceBool(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocBoolBacking(int(capacity))
	clear(backing)
	registers.slicesBool[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallMakeSliceUint is the trampoline for handlerSubOpMakeSliceUint.
//
// Writes slicesUint[destinationIndex] = arena-backed make([]uint64, ints[lengthIndex],
// ints[capacityIndex]). Three-operand sub-op (tier 0 arity) backed by the arena's
// uintBacking slab. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesUint register receiving the new
// slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceUint(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocUintBacking(int(capacity))
	clear(backing)
	registers.slicesUint[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallMakeSliceByte is the trampoline for handlerSubOpMakeSliceByte. The backing comes
// from the arena's shared byteSlab.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesByte register receiving the new
// slice.
// Takes lengthIndex (int64) which is the int register holding the requested length.
// Takes capacityIndex (int64) which is the int register holding the requested capacity.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallMakeSliceByte(ctx *dispatchContext, destinationIndex, lengthIndex, capacityIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	length := registers.Ints[lengthIndex]
	capacity := registers.Ints[capacityIndex]
	backing := ctx.vm.Arena.AllocByteBacking(int(capacity))
	if length > 0 {
		clear(backing[:length])
	}
	registers.slicesByte[destinationIndex] = backing[:length:capacity]
	return ctx
}

// asmCallAppendSliceIntDirect is the trampoline for handlerSubOpAppendSliceIntDirect.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesInt register receiving the appended
// slice.
// Takes sourceIndex (int64) which is the slicesInt register holding the source slice.
// Takes elementIndex (int64) which is the int register holding the element value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceIntDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.SlicesInt[sourceIndex]
	element := registers.Ints[elementIndex]
	registers.SlicesInt[destinationIndex] = arenaAppendInt(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallAppendSliceFloatDirect is the trampoline for handlerSubOpAppendSliceFloatDirect.
//
// Same shape as asmCallAppendSliceIntDirect but for []float64 with the element read from
// the float bank.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesFloat register receiving the appended
// slice.
// Takes sourceIndex (int64) which is the slicesFloat register holding the source slice.
// Takes elementIndex (int64) which is the float register holding the element value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceFloatDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.slicesFloat[sourceIndex]
	element := registers.Floats[elementIndex]
	registers.slicesFloat[destinationIndex] = arenaAppendFloat(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallAppendSliceStringDirect is the trampoline for
// handlerSubOpAppendSliceStringDirect.
//
// Reads slicesString[sourceIndex] and strings[elementIndex], routes the element through
// materialiseString (so an arena-borrowed header gets a proper backing if needed before
// being held by the destination slice), appends, and writes to
// slicesString[destinationIndex].
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesString register receiving the
// appended slice.
// Takes sourceIndex (int64) which is the slicesString register holding the source slice.
// Takes elementIndex (int64) which is the string register holding the element value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceStringDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.slicesString[sourceIndex]
	element := MaterialiseStringForArenaAppend(ctx.vm.Arena, source, registers.Strings[elementIndex])
	registers.slicesString[destinationIndex] = arenaAppendString(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallAppendSliceBoolDirect is the trampoline for handlerSubOpAppendSliceBoolDirect.
//
// Same shape as asmCallAppendSliceIntDirect but for []bool with the element read from the
// bool bank.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesBool register receiving the appended
// slice.
// Takes sourceIndex (int64) which is the slicesBool register holding the source slice.
// Takes elementIndex (int64) which is the bool register holding the element value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceBoolDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.slicesBool[sourceIndex]
	element := registers.Bools[elementIndex]
	registers.slicesBool[destinationIndex] = arenaAppendBool(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallAppendSliceUintDirect is the trampoline for handlerSubOpAppendSliceUintDirect.
//
// Same shape as asmCallAppendSliceIntDirect but for []uint64 with the element read from
// the uint bank.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesUint register receiving the appended
// slice.
// Takes sourceIndex (int64) which is the slicesUint register holding the source slice.
// Takes elementIndex (int64) which is the uint register holding the element value.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceUintDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.slicesUint[sourceIndex]
	element := registers.Uints[elementIndex]
	registers.slicesUint[destinationIndex] = arenaAppendUint(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallAppendSliceByteDirect is the trampoline for handlerSubOpAppendSliceByteDirect.
//
// Reads slicesByte[sourceIndex] and uints[elementIndex], truncates the uint64 element to
// a byte (matching handleSubOpAppendSliceByteDirect), appends, and writes to
// slicesByte[destinationIndex].
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes destinationIndex (int64) which is the slicesByte register receiving the appended
// slice.
// Takes sourceIndex (int64) which is the slicesByte register holding the source slice.
// Takes elementIndex (int64) which is the uint register whose low byte is appended.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//go:nosplit
func asmCallAppendSliceByteDirect(ctx *dispatchContext, destinationIndex, sourceIndex, elementIndex int64) *dispatchContext {
	registers := vmRegistersForCtx(ctx)
	source := registers.slicesByte[sourceIndex]
	element := safeconv.Uint64ToByteTruncate(registers.Uints[elementIndex])
	registers.slicesByte[destinationIndex] = arenaAppendByte(ctx.vm.Arena, source, element)
	return ctx
}

// asmCallSetupGeneralBank finishes an inline call's general-bank setup when the ASM fast
// arm declined. It allocates the callee's general bank from the arena slab, copies
// arguments with Go's typed assignment so GC write barriers fire, and saves the callee's
// dispatch state.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and arena handle.
// Takes callInfo (*AsmCallInfo) which holds the callee's general count and argument
// sources.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:unused // called from ASM
func asmCallSetupGeneralBank(ctx *dispatchContext, callInfo *AsmCallInfo) *dispatchContext {
	fp := int(ctx.framePointer)
	ctx.vm.CallStack[fp].hasGeneralAlloc = true
	arena := ctx.vm.Arena
	asmSetupCalleeGeneralBank(ctx, callInfo, arena, fp)
	asmCopyGeneralArguments(ctx, callInfo, arena, fp)
	asmSaveCalleeDispatchState(ctx, callInfo, fp)
	return ctx
}

// asmSetupCalleeGeneralBank slices the callee's general-bank registers out of the arena's
// general slab, growing the slab when the callee's register count would not fit.
//
// Takes ctx (*dispatchContext) which owns the call stack.
// Takes callInfo (*AsmCallInfo) which describes the callee's register layout.
// Takes arena (*RegisterArena) which provides the general slab storage.
// Takes fp (int) which is the frame pointer index of the callee on the call stack.
//
//nolint:unused // called from ASM path
//go:nosplit
func asmSetupCalleeGeneralBank(ctx *dispatchContext, callInfo *AsmCallInfo, arena *RegisterArena, fp int) {
	count := callInfo.calleeGeneralCount
	if count == 0 {
		return
	}

	start := int(ctx.arenaGeneralIndex)
	end := start + int(count)
	if end > len(arena.generalSlab) {
		arena.growGeneralSlab(end)
	}
	ctx.vm.CallStack[fp].Registers.General = arena.generalSlab[start:end]
	arena.GeneralIndex = end
	ctx.arenaGeneralIndex = int64(end)

	ctx.generalsBase = uintptr(unsafe.Pointer(&arena.generalSlab[start]))

	ctx.arenaGeneralSlab = uintptr(unsafe.Pointer(&arena.generalSlab[0]))
	ctx.arenaGeneralSlabCap = int64(len(arena.generalSlab))
}

// asmCopyGeneralArguments copies the general-bank arguments from the caller frame (at
// fp-1) into the callee frame using Go's typed assignment so GC write barriers fire
// correctly for the reflect.Value payload. The boundary-copy guard for Struct/Array kinds
// is inlined so the 99% recursive *node case stays branch-light.
//
// Takes ctx (*dispatchContext) which owns the call stack.
// Takes callInfo (*AsmCallInfo) which describes argument source slots.
// Takes arena (*RegisterArena) which is used for boundary-copy slab growth.
// Takes fp (int) which is the frame pointer index of the callee on the call stack.
//
//nolint:unused // called from ASM path
//go:nosplit
func asmCopyGeneralArguments(ctx *dispatchContext, callInfo *AsmCallInfo, arena *RegisterArena, fp int) {
	argumentCount := callInfo.generalArgumentCount
	if argumentCount == 0 {
		return
	}
	callerGen := ctx.vm.CallStack[fp-1].Registers.General
	calleeGen := ctx.vm.CallStack[fp].Registers.General
	n := int(argumentCount)
	for i := range n {
		sourceIndex := int(callInfo.generalArgumentSources[i])
		source := callerGen[sourceIndex]
		if !source.IsValid() {
			calleeGen[i] = source
			continue
		}
		k := source.Kind()
		if k != reflect.Struct && k != reflect.Array {
			calleeGen[i] = source
			continue
		}
		calleeGen[i] = valueCopyForBoundaryArenaWithVM(arena, ctx.vm, source)
	}
}

// asmSaveCalleeDispatchState stashes the callee's freshly entered dispatch state into
// asmDispatchSaves[callee_fp] so any deeper Go-side push can later restore the callee
// correctly.
//
// Takes ctx (*dispatchContext) which owns asmDispatchSaves.
// Takes callInfo (*AsmCallInfo) which describes the callee's code and constants.
// Takes fp (int) which is the frame pointer index of the callee on the call stack.
//
//nolint:unused // called from ASM path
//go:nosplit
func asmSaveCalleeDispatchState(ctx *dispatchContext, callInfo *AsmCallInfo, fp int) {
	if ctx.vm.asmDispatchSaves == nil || fp < 0 || fp >= len(ctx.vm.asmDispatchSaves) {
		return
	}
	save := &ctx.vm.asmDispatchSaves[fp]
	save.codeBase = callInfo.calleeBody
	save.codeLength = callInfo.calleeBodyLength
	save.intConstantsBase = callInfo.calleeIntConstants
	save.uintConstantsBase = callInfo.calleeUintConstants
	save.floatConstantsBase = callInfo.calleeFloatConstants
	save.stringConstantsBase = callInfo.calleeStringConstants
	save.boolConstantsBase = callInfo.calleeBoolConstants
}

// asmReturnClearGeneralBank releases an inline callee's general-bank allocation. It
// clears the slab range so the GC no longer scans stale reflect.Value entries and
// restores arena.GeneralIndex from the callee's arenaSave.
//
// Takes ctx (*dispatchContext) which carries the live callee frame pointer and arena
// handle.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:unused // called from ASM
//go:nosplit
func asmReturnClearGeneralBank(ctx *dispatchContext) *dispatchContext {
	fp := int(ctx.framePointer)
	callee := &ctx.vm.CallStack[fp]
	savedIdx := callee.arenaSave.GeneralIndex
	arena := ctx.vm.Arena

	live := int(ctx.arenaGeneralIndex)
	if savedIdx < live {
		clear(arena.generalSlab[savedIdx:live])
	}
	arena.GeneralIndex = savedIdx
	ctx.arenaGeneralIndex = int64(savedIdx)
	return ctx
}

// asmTailCallExecute performs a tail call from the ASM dispatch loop without exiting
// through the Go-side exit-reason switch. The argument copy runs in Go to keep
// correctness for alias-bearing arguments.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and un-advanced PC.
//
// Returns the same ctx so the ASM caller can reload R15.
//
//nolint:unused // called from ASM
func asmTailCallExecute(ctx *dispatchContext) *dispatchContext {
	vm := ctx.vm
	vm.syncCallContextFromASM(ctx)
	fp := int(ctx.framePointer)
	frame := &vm.CallStack[fp]
	registers := &frame.Registers
	frame.ProgramCounter = int(ctx.programCounter)
	instruction := frame.Function.Body[frame.ProgramCounter]
	frame.ProgramCounter++
	handleTailCall(vm, frame, registers, instruction)
	vm.updateASMCallInfoBase()
	vm.refreshCallContext(ctx)
	vm.rebuildDispatchPointers(ctx, frame, registers)
	return ctx
}

// frameForCtx returns the *CallFrame the ASM dispatcher is currently executing. It reads
// ctx.FramePointer rather than vm.FramePointer because the Go-side mirror lags during ASM
// inline calls.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer.
//
// Returns the *CallFrame at vm.CallStack[ctx.FramePointer].
//
//go:nosplit
func frameForCtx(ctx *dispatchContext) *CallFrame {
	return &ctx.vm.CallStack[ctx.framePointer]
}

// trampolinePointerBase extracts the unsafe base for the Pointer-receiver fast path.
//
// Reads general[recvReg] without taking the address of a stack-resident reflect.Value,
// which Go's escape analysis pessimises when the caller is reached via an ASM trampoline;
// every call would heap-allocate the 24-byte reflect.Value. The body reads the {typ, ptr,
// flag} triplet directly from the general bank's backing memory via the slice header's
// data pointer.
//
// Takes registers (*Registers) which owns the general bank slice header.
// Takes recvReg (uint8) which is the general register index of the receiver.
//
// Returns the receiver's base pointer when the value's kind is reflect.Pointer.
// Returns false when the slot is empty, the kind is not Pointer, or the indirected
// pointer is nil; the caller should route to the reflect-walk slow path.
//
//go:nosplit
func trampolinePointerBase(registers *Registers, recvReg uint8) (unsafe.Pointer, bool) {
	slot := (*unsafeReflectValue)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(registers.General)), uintptr(recvReg)*24))
	if slot.typ == nil {
		return nil, false
	}
	if reflect.Kind(slot.flag&flagKindMask) != reflect.Pointer {
		return nil, false
	}
	if slot.flag&flagIndir != 0 {
		inner := *(*unsafe.Pointer)(slot.ptr)
		if inner == nil {
			return nil, false
		}
		return inner, true
	}
	if slot.ptr == nil {
		return nil, false
	}
	return slot.ptr, true
}

// asmCallIncStructFieldInt increments a signed-int struct field via the fast path.
//
// Tier-1 sub-op (op=isa.OpDrillTier1, A=sub-op id, B=recvReg, C= layoutIdx low byte)
// invoked through EmitInlineGoCallTwoOperandShim so the ASM caller passes (ctx, recvReg,
// layoutIdx). Resolves the field's unsafe address via trampolinePointerBase and INC's the
// appropriately sized integer in place; on receiver-shape mismatch or non-int Kind the
// body routes through incStructFieldIntSlowPath which calls the existing reflect-walk
// fallback. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and structLayout
// table.
// Takes recvReg (int64) which is the general register holding the pointer receiver.
// Takes layoutIdx (int64) which is the index into the function's structLayoutTable.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:dupl // hot-path twin
//go:nosplit
func asmCallIncStructFieldInt(ctx *dispatchContext, recvReg, layoutIdx int64) *dispatchContext {
	frame := frameForCtx(ctx)
	layout := frame.Function.StructLayoutTable[layoutIdx]
	registers := &frame.Registers
	base, ok := trampolinePointerBase(registers, safeconv.Int64ToUint8(recvReg))
	if !ok {
		incStructFieldIntSlowPath(ctx, frame, registers, recvReg, layout, +1)
		return ctx
	}

	fieldPtr := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Int:
		*(*int)(fieldPtr)++
	case reflect.Int8:
		*(*int8)(fieldPtr)++
	case reflect.Int16:
		*(*int16)(fieldPtr)++
	case reflect.Int32:
		*(*int32)(fieldPtr)++
	case reflect.Int64:
		*(*int64)(fieldPtr)++
	default:
		incStructFieldIntSlowPath(ctx, frame, registers, recvReg, layout, +1)
	}
	return ctx
}

// asmCallDecStructFieldInt decrements a signed-int struct field via the fast path.
//
// Sign-flipped twin of asmCallIncStructFieldInt kept separate so the hot-path INC and DEC
// bodies both remain inlinable. See that function for the dispatch shape and operand
// layout. Slow path routes through incStructFieldIntSlowPath with delta=-1.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and structLayout
// table.
// Takes recvReg (int64) which is the general register holding the pointer receiver.
// Takes layoutIdx (int64) which is the index into the function's structLayoutTable.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:dupl // hot-path twin
//go:nosplit
func asmCallDecStructFieldInt(ctx *dispatchContext, recvReg, layoutIdx int64) *dispatchContext {
	frame := frameForCtx(ctx)
	layout := frame.Function.StructLayoutTable[layoutIdx]
	registers := &frame.Registers
	base, ok := trampolinePointerBase(registers, safeconv.Int64ToUint8(recvReg))
	if !ok {
		incStructFieldIntSlowPath(ctx, frame, registers, recvReg, layout, -1)
		return ctx
	}

	fieldPtr := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Int:
		*(*int)(fieldPtr)--
	case reflect.Int8:
		*(*int8)(fieldPtr)--
	case reflect.Int16:
		*(*int16)(fieldPtr)--
	case reflect.Int32:
		*(*int32)(fieldPtr)--
	case reflect.Int64:
		*(*int64)(fieldPtr)--
	default:
		incStructFieldIntSlowPath(ctx, frame, registers, recvReg, layout, -1)
	}
	return ctx
}

// asmCallIncStructFieldUint increments an unsigned-int struct field via the fast path.
//
// Unsigned counterpart of asmCallIncStructFieldInt, dispatched the same way
// (op=isa.OpDrillTier1, A=sub-op id, B=recvReg, C=layoutIdx low byte) through
// EmitInlineGoCallTwoOperandShim. Resolves the field's unsafe address via
// trampolinePointerBase and INC's the appropriately sized unsigned integer in place; on
// receiver-shape mismatch or non-uint Kind the body routes through
// incStructFieldUintSlowPath. Returns ctx so the ASM caller can restore R15.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and structLayout
// table.
// Takes recvReg (int64) which is the general register holding the pointer receiver.
// Takes layoutIdx (int64) which is the index into the function's structLayoutTable.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:dupl // hot-path twin
//go:nosplit
func asmCallIncStructFieldUint(ctx *dispatchContext, recvReg, layoutIdx int64) *dispatchContext {
	frame := frameForCtx(ctx)
	layout := frame.Function.StructLayoutTable[layoutIdx]
	registers := &frame.Registers
	base, ok := trampolinePointerBase(registers, safeconv.Int64ToUint8(recvReg))
	if !ok {
		incStructFieldUintSlowPath(ctx, frame, registers, recvReg, layout, +1)
		return ctx
	}

	fieldPtr := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Uint:
		*(*uint)(fieldPtr)++
	case reflect.Uint8:
		*(*uint8)(fieldPtr)++
	case reflect.Uint16:
		*(*uint16)(fieldPtr)++
	case reflect.Uint32:
		*(*uint32)(fieldPtr)++
	case reflect.Uint64:
		*(*uint64)(fieldPtr)++
	case reflect.Uintptr:
		*(*uintptr)(fieldPtr)++
	default:
		incStructFieldUintSlowPath(ctx, frame, registers, recvReg, layout, +1)
	}
	return ctx
}

// asmCallDecStructFieldUint decrements an unsigned-int struct field via the fast path.
//
// Sign-flipped twin of asmCallIncStructFieldUint kept separate so the hot-path INC and
// DEC bodies both remain inlinable. See that function for the dispatch shape and operand
// layout. Slow path routes through incStructFieldUintSlowPath with delta=-1.
//
// Takes ctx (*dispatchContext) which carries the live frame pointer and structLayout
// table.
// Takes recvReg (int64) which is the general register holding the pointer receiver.
// Takes layoutIdx (int64) which is the index into the function's structLayoutTable.
//
// Returns the same ctx so the ASM caller can reload R15 from AX.
//
//nolint:dupl // hot-path twin
//go:nosplit
func asmCallDecStructFieldUint(ctx *dispatchContext, recvReg, layoutIdx int64) *dispatchContext {
	frame := frameForCtx(ctx)
	layout := frame.Function.StructLayoutTable[layoutIdx]
	registers := &frame.Registers
	base, ok := trampolinePointerBase(registers, safeconv.Int64ToUint8(recvReg))
	if !ok {
		incStructFieldUintSlowPath(ctx, frame, registers, recvReg, layout, -1)
		return ctx
	}

	fieldPtr := unsafe.Add(base, uintptr(layout.Offset))
	switch reflect.Kind(layout.Kind) {
	case reflect.Uint:
		*(*uint)(fieldPtr)--
	case reflect.Uint8:
		*(*uint8)(fieldPtr)--
	case reflect.Uint16:
		*(*uint16)(fieldPtr)--
	case reflect.Uint32:
		*(*uint32)(fieldPtr)--
	case reflect.Uint64:
		*(*uint64)(fieldPtr)--
	case reflect.Uintptr:
		*(*uintptr)(fieldPtr)--
	default:
		incStructFieldUintSlowPath(ctx, frame, registers, recvReg, layout, -1)
	}
	return ctx
}

// incStructFieldIntSlowPath routes a failed Inc/Dec to the reflect-walk fallback and
// raises on a nil pointer receiver. The nil case uses a Go panic rather than an OpResult
// because the ASM shim cannot observe a returned OpResult.
//
// Takes frame (*CallFrame) which is the dispatching frame.
// Takes registers (*Registers) which is the frame's register bundle.
// Takes recvReg (int64) which is the general register holding the receiver.
// Takes layout (StructFieldLayout) which describes the target field.
// Takes delta (int64) which is +1 for Inc and -1 for Dec.
//
//go:nosplit
func incStructFieldIntSlowPath(_ *dispatchContext, frame *CallFrame, registers *Registers, recvReg int64, layout program.StructFieldLayout, delta int64) {
	if incDecStructFieldIntApply(frame, registers, safeconv.Int64ToUint8(recvReg), layout, delta) {
		return
	}
	if structFieldReceiverIsNilPointer(registers.General[safeconv.Int64ToUint8(recvReg)]) {
		vmPanicNilPointerReceiver()
	}
}

// incStructFieldUintSlowPath routes a failed Inc/Dec to the reflect-walk fallback and
// raises on a nil pointer receiver.
//
// Unsigned counterpart of incStructFieldIntSlowPath, called from the uint Inc/Dec
// trampolines when the fast path's shape check fails. Raises the nil-dereference host
// panic through the same host-stack unwind documented on incStructFieldIntSlowPath.
//
// Takes frame (*CallFrame) which is the dispatching frame.
// Takes registers (*Registers) which is the frame's register bundle.
// Takes recvReg (int64) which is the general register holding the receiver.
// Takes layout (StructFieldLayout) which describes the target field.
// Takes delta (int64) which is +1 for Inc and -1 for Dec.
//
//go:nosplit
func incStructFieldUintSlowPath(_ *dispatchContext, frame *CallFrame, registers *Registers, recvReg int64, layout program.StructFieldLayout, delta int64) {
	if incDecStructFieldUintApply(frame, registers, safeconv.Int64ToUint8(recvReg), layout, delta) {
		return
	}
	if structFieldReceiverIsNilPointer(registers.General[safeconv.Int64ToUint8(recvReg)]) {
		vmPanicNilPointerReceiver()
	}
}
