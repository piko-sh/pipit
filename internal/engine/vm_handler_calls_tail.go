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
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// handleTailCall performs a tail call optimisation by reusing the current frame,
// snapshotting arguments before reclaiming the arena region.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the current call frame to reuse.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the call site index.
//
// Returns OpResult indicating the next execution step.
func handleTailCall(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	siteIndex := instruction.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	callee, result := resolveDirectCallee(vm, site)
	if result != opContinue {
		return result
	}
	transferTailCallArguments(vm, frame, registers, site, callee)
	frame.Function = callee
	frame.ProgramCounter = 0
	frame.upvalues = nil
	frame.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(frame.sharedCells)
	frame.sharedCells = nil
	return opFrameChanged
}

// transferTailCallArguments moves the caller's argument values into the callee's
// parameter registers, either via the direct in-place copy fast path or via snapshot +
// arena re-allocation when the layouts differ or arguments alias destinations.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the call frame being repurposed for the tail call.
// Takes registers (*Registers) which holds the caller's argument values.
// Takes site (*CallSite) which describes the source layout and argument program.
// Takes callee (*CompiledFunction) which is the function whose parameter registers
// receive the values.
func transferTailCallArguments(vm *VM, frame *CallFrame, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) {
	if site.TailReuseFrameInPlace && !site.TailArgsAlias && site.ArgCopyProgram != nil {
		copyCallArgs(vm, vm.Arena, registers, frame, site, callee)
		return
	}
	arguments := snapshotTailCallArgs(vm, site, registers, callee)
	if !site.TailReuseFrameInPlace {
		reallocateTailCallFrame(vm, frame, callee)
	}
	placeTailCallArgs(&frame.Registers, arguments, callee.ParameterKinds, callee.ParameterRegisters, vm.Arena)
}

// reallocateTailCallFrame restores the caller's arena windows then re-saves and
// re-allocates at the callee's per-bank sizes. Used only when tailReuseFrameInPlace is
// false (layouts differ).
//
// Takes vm (*VM) for execution state; its arena is updated.
// Takes frame (*CallFrame) whose arena save point and registers are replaced for the
// callee's layout.
// Takes callee (*CompiledFunction) whose per-bank sizes drive reallocation.
func reallocateTailCallFrame(vm *VM, frame *CallFrame, callee *program.CompiledFunction) {
	if vm.Arena != nil {
		vm.Arena.Restore(frame.arenaSave)
		vm.Arena.SaveInto(&frame.arenaSave)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&frame.Registers, callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
		return
	}
	frame.arenaSave = ArenaSavePoint{}
	frame.Registers = NewRegisters(callee.NumRegisters)
}

// snapshotTailCallArgs captures all argument values from the current registers into a
// buffer of tailCallArgument values. This snapshot is taken before the current frame's
// arena region is reclaimed, preserving arguments that may overlap with the callee's
// registers.
//
// Reuses vm.tailCallArgBuffer to avoid per-call allocation.
//
// Takes vm (*VM) which owns the per-VM tail-call buffer.
// Takes site (*CallSite) which describes argument locations and the buffer.
// Takes registers (*Registers) which holds the current values to snapshot.
// Takes callee (*CompiledFunction) which provides parameter kinds.
//
// Returns []tailCallArgument containing the snapshotted argument values.
func snapshotTailCallArgs(vm *VM, site *program.CallSite, registers *Registers, callee *program.CompiledFunction) []tailCallArgument {
	argumentCount := len(site.Arguments)
	if cap(vm.tailCallArgBuffer) < argumentCount {
		vm.tailCallArgBuffer = make([]tailCallArgument, argumentCount)
	} else {
		vm.tailCallArgBuffer = vm.tailCallArgBuffer[:argumentCount]
		clear(vm.tailCallArgBuffer)
	}
	arguments := vm.tailCallArgBuffer
	for i, argumentLocation := range site.Arguments {
		if i >= len(callee.ParameterKinds) {
			break
		}
		arguments[i] = snapshotOneTailArgument(registers, argumentLocation)
	}
	return arguments
}

// snapshotOneTailArgument reads a single argument from the caller's registers and returns
// it as a tailCallArgument tagged with the source register kind.
//
// Takes registers (*Registers) which holds the source values.
// Takes argumentLocation (VarLocation) which identifies the register bank and index.
//
// Returns tailCallArgument containing the copied value and its kind.
func snapshotOneTailArgument(registers *Registers, argumentLocation program.VarLocation) tailCallArgument {
	switch argumentLocation.Kind {
	case isa.RegisterInt:
		return tailCallArgument{IntValue: registers.Ints[argumentLocation.Register], Kind: isa.RegisterInt}
	case isa.RegisterFloat:
		return tailCallArgument{FloatValue: registers.Floats[argumentLocation.Register], Kind: isa.RegisterFloat}
	case isa.RegisterString:
		return tailCallArgument{StringValue: registers.Strings[argumentLocation.Register], Kind: isa.RegisterString}
	case isa.RegisterGeneral:
		return tailCallArgument{GeneralValue: registers.General[argumentLocation.Register], Kind: isa.RegisterGeneral}
	case isa.RegisterBool:
		return tailCallArgument{BoolValue: registers.Bools[argumentLocation.Register], Kind: isa.RegisterBool}
	case isa.RegisterUint:
		return tailCallArgument{UintValue: registers.Uints[argumentLocation.Register], Kind: isa.RegisterUint}
	case isa.RegisterComplex:
		return tailCallArgument{ComplexValue: registers.Complex[argumentLocation.Register], Kind: isa.RegisterComplex}
	case isa.RegisterSliceInt:
		return tailCallArgument{sliceIntValue: registers.SlicesInt[argumentLocation.Register], Kind: isa.RegisterSliceInt}
	case isa.RegisterSliceFloat:
		return tailCallArgument{sliceFloatValue: registers.slicesFloat[argumentLocation.Register], Kind: isa.RegisterSliceFloat}
	case isa.RegisterSliceString:
		return tailCallArgument{sliceStringValue: registers.slicesString[argumentLocation.Register], Kind: isa.RegisterSliceString}
	case isa.RegisterSliceBool:
		return tailCallArgument{sliceBoolValue: registers.slicesBool[argumentLocation.Register], Kind: isa.RegisterSliceBool}
	case isa.RegisterSliceUint:
		return tailCallArgument{sliceUintValue: registers.slicesUint[argumentLocation.Register], Kind: isa.RegisterSliceUint}
	case isa.RegisterSliceByte:
		return tailCallArgument{sliceByteValue: registers.slicesByte[argumentLocation.Register], Kind: isa.RegisterSliceByte}
	default:
		return tailCallArgument{}
	}
}

// placeTailCallArgs writes snapshotted tail call arguments into the new callee registers,
// handling same-kind placement, scalar-to-general boxing, and general-to-scalar unboxing.
//
// Takes calleeRegisters (*Registers) which is the destination register set.
// Takes arguments ([]tailCallArgument) which holds the snapshotted argument values.
// Takes parameterKinds ([]isa.RegisterKind) which is the expected kind per param.
// Takes parameterRegisters ([]uint8) which is the callee's per-parameter slot index in
// its kind-bank, or empty when no callee parameter layout is recorded.
// Takes arena (*RegisterArena) which receives bump-allocated narrow-int widening backings
// during cross-kind unboxing; may be nil.
func placeTailCallArgs(calleeRegisters *Registers, arguments []tailCallArgument, parameterKinds []isa.RegisterKind, parameterRegisters []uint8, arena *RegisterArena) {
	var kindIndex [isa.NumRegisterKinds]int
	for i := range arguments {
		if i >= len(parameterKinds) {
			break
		}
		parameterKind := parameterKinds[i]
		var dest int
		if i < len(parameterRegisters) {
			dest = int(parameterRegisters[i])
			kindIndex[parameterKind] = dest + 1
		} else {
			dest = kindIndex[parameterKind]
			kindIndex[parameterKind]++
		}
		placeOneTailArgument(calleeRegisters, &arguments[i], parameterKind, dest, arena)
	}
}

// placeOneTailArgument writes a single snapshotted argument into the destination
// register, converting between kinds as needed.
//
// Takes regs (*Registers) which is the destination register set.
// Takes argument (*tailCallArgument) which holds the snapshotted value.
// Takes parameterKind (isa.RegisterKind) which is the expected destination kind.
// Takes dest (int) which is the destination index within the typed bank.
// Takes arena (*RegisterArena) which receives bump-allocated narrow-int widening backings
// during cross-kind unboxing; may be nil.
func placeOneTailArgument(regs *Registers, argument *tailCallArgument, parameterKind isa.RegisterKind, dest int, arena *RegisterArena) {
	if argument.Kind == parameterKind {
		placeTailArgSameKind(regs, argument, parameterKind, dest)
	} else if parameterKind == isa.RegisterGeneral {
		boxTailArgumentToGeneral(regs, argument, dest)
	} else if argument.Kind == isa.RegisterGeneral {
		unboxGeneralToScalar(regs, argument.GeneralValue, parameterKind, dest, arena)
	}
}

// placeTailArgSameKind handles the common case where source and destination kinds match,
// performing a direct value copy with no conversion.
//
// Takes regs (*Registers) which is the destination register set.
// Takes argument (*tailCallArgument) which holds the snapshotted value.
// Takes kind (isa.RegisterKind) which selects the typed bank.
// Takes dest (int) which is the destination index in the bank.
func placeTailArgSameKind(regs *Registers, argument *tailCallArgument, kind isa.RegisterKind, dest int) {
	switch kind {
	case isa.RegisterInt:
		regs.Ints[dest] = argument.IntValue
	case isa.RegisterFloat:
		regs.Floats[dest] = argument.FloatValue
	case isa.RegisterString:
		regs.Strings[dest] = argument.StringValue
	case isa.RegisterGeneral:
		regs.General[dest] = argument.GeneralValue
	case isa.RegisterBool:
		regs.Bools[dest] = argument.BoolValue
	case isa.RegisterUint:
		regs.Uints[dest] = argument.UintValue
	case isa.RegisterComplex:
		regs.Complex[dest] = argument.ComplexValue
	case isa.RegisterSliceInt:
		regs.SlicesInt[dest] = argument.sliceIntValue
	case isa.RegisterSliceFloat:
		regs.slicesFloat[dest] = argument.sliceFloatValue
	case isa.RegisterSliceString:
		regs.slicesString[dest] = argument.sliceStringValue
	case isa.RegisterSliceBool:
		regs.slicesBool[dest] = argument.sliceBoolValue
	case isa.RegisterSliceUint:
		regs.slicesUint[dest] = argument.sliceUintValue
	case isa.RegisterSliceByte:
		regs.slicesByte[dest] = argument.sliceByteValue
	default:
	}
}

// boxTailArgumentToGeneral wraps a typed tail-call argument into a reflect.Value and
// stores it in the general register bank.
//
// Only the banks with a dedicated sub-op are listed. The default routes through the
// generic pack or unpack handler, which covers all thirteen banks.
//
// Takes regs (*Registers) which is the destination register set.
// Takes argument (*tailCallArgument) which holds the snapshotted value to box.
// Takes dest (int) which is the index in the general register bank.
func boxTailArgumentToGeneral(regs *Registers, argument *tailCallArgument, dest int) {
	switch argument.Kind {
	case isa.RegisterInt:
		regs.General[dest] = reflect.ValueOf(argument.IntValue)
	case isa.RegisterFloat:
		regs.General[dest] = reflect.ValueOf(argument.FloatValue)
	case isa.RegisterString:
		regs.General[dest] = reflect.ValueOf(argument.StringValue)
	case isa.RegisterBool:
		regs.General[dest] = reflect.ValueOf(argument.BoolValue)
	case isa.RegisterUint:
		regs.General[dest] = reflect.ValueOf(argument.UintValue)
	case isa.RegisterComplex:
		regs.General[dest] = reflect.ValueOf(argument.ComplexValue)
	case isa.RegisterSliceInt:
		regs.General[dest] = reflect.ValueOf(argument.sliceIntValue)
	case isa.RegisterSliceFloat:
		regs.General[dest] = reflect.ValueOf(argument.sliceFloatValue)
	case isa.RegisterSliceString:
		regs.General[dest] = reflect.ValueOf(argument.sliceStringValue)
	case isa.RegisterSliceBool:
		regs.General[dest] = reflect.ValueOf(argument.sliceBoolValue)
	case isa.RegisterSliceUint:
		regs.General[dest] = reflect.ValueOf(argument.sliceUintValue)
	case isa.RegisterSliceByte:
		regs.General[dest] = reflect.ValueOf(argument.sliceByteValue)
	default:
	}
}
