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

	"pipit.sh/pipit/internal/isa"
)

// closureResults are the results of a closure run, either a single result held by value
// or every result as a slice.
type closureResults struct {
	// one is the result when single.
	one any

	// all is every result when not single.
	all []any

	// single is true when the run returned exactly one result.
	single bool
}

// slice returns every result, a single one in buffer.
//
// Takes buffer (*[1]any) which holds a single result.
//
// Returns []any which is every result.
func (r closureResults) slice(buffer *[1]any) []any {
	if r.single {
		buffer[0] = r.one
		return buffer[:]
	}
	return r.all
}

// setBaseFrameResults extracts the base frame's results once.
//
// Takes frame (*CallFrame) which is the returning base frame.
func (vm *VM) setBaseFrameResults(frame *CallFrame) {
	if len(frame.Function.ResultKinds) == 1 {
		vm.EvalAllResults = nil
		vm.evalResult = vm.extractResultSlot(frame, 0, 0)
		vm.evalSingle = true
		return
	}
	vm.evalSingle = false
	vm.EvalAllResults = vm.extractAllResults(frame)
	vm.evalResult = nil
	if len(vm.EvalAllResults) > 0 {
		vm.evalResult = vm.EvalAllResults[0]
	}
}

// extractAllResults extracts all return values from the final frame.
//
// Takes frame (*CallFrame) which specifies the call frame to extract results from.
//
// Returns a slice of all result values.
func (vm *VM) extractAllResults(frame *CallFrame) []any {
	resultKinds := frame.Function.ResultKinds
	if len(resultKinds) == 0 {
		return nil
	}

	results := make([]any, len(resultKinds))
	var bankCounters [isa.NumRegisterKinds]uint8

	for i, kind := range resultKinds {
		sourceRegister := int(bankCounters[kind])
		bankCounters[kind]++
		results[i] = vm.extractResultSlot(frame, i, sourceRegister)
	}

	return results
}

// extractResultSlot extracts result i of the final frame from its bank register.
//
// Takes frame (*CallFrame) which is the returning frame.
// Takes i (int) which is the result's index.
// Takes sourceRegister (int) which is the result's register within its bank.
//
// Returns any which is the result.
func (vm *VM) extractResultSlot(frame *CallFrame, i, sourceRegister int) any {
	kind := frame.Function.ResultKinds[i]
	registers := &frame.Registers
	if hint := resultReflectTypeHint(frame.Function, i); hint != nil {
		if value, ok := extractRegisterValueAs(vm.Arena, registers, kind, sourceRegister, hint); ok {
			return value
		}
	}
	return vm.extractRegisterValue(registers, kind, sourceRegister)
}

// takeBaseFrameResults takes the results setBaseFrameResults left. A single result is the
// run's result, which the dispatch loop moved out of evalResult.
//
// Takes result (any) which is the run's result.
// Takes completed (bool) which is false when the run failed and left no results.
//
// Returns closureResults which are the results.
func (vm *VM) takeBaseFrameResults(result any, completed bool) closureResults {
	results := closureResults{one: nil, all: vm.EvalAllResults, single: false}
	if completed && vm.evalSingle {
		results = closureResults{one: result, all: nil, single: true}
	}
	vm.EvalAllResults = nil
	vm.evalSingle = false
	return results
}

// callClosureRaw executes a compiled closure for a callback, bound or top-level run.
//
// Takes closure (*RuntimeClosure) which specifies the closure to call.
// Takes arguments ([]reflect.Value) which provides the reflect.Value arguments to pass.
//
// Returns closureResults which are the results of the run.
func (vm *VM) callClosureRaw(closure *RuntimeClosure, arguments []reflect.Value) closureResults {
	defer vm.leaveCallbackRun(vm.enterCallbackRun())
	if vm.Cancelled.Load() != 0 || vm.pollContext() {
		vm.evalError = vm.stopError()
		return closureResults{one: nil, all: nil, single: false}
	}
	if vm.enterServedChain() {
		defer vm.leaveServedChain()
	}
	if storeCensusBuilt {
		censusNotePublished(arguments)
	}
	if vm.Arena != nil {
		vm.Arena.reentrantHandedOff = false
	}
	callee := closure.Function
	closureFramePointer := vm.pushClosureFrame(closure)
	frame := &vm.CallStack[closureFramePointer]

	assignReflectParams(&frame.Registers, callee.ParameterKinds, callee.ParameterRegisters, arguments)

	vm.evalSingle = false
	runResult, err := vm.RunDispatchedGuarded(closureFramePointer)
	if err != nil {
		vm.evalError = err
	}

	results := vm.takeBaseFrameResults(runResult, err == nil)
	if err == nil {
		var handedOff bool
		if results.single {
			single := [1]any{results.one}
			handedOff = vm.handOffReentrantRun(single[:], reflect.Value{}, arguments)
			results.one = single[0]
		} else {
			handedOff = vm.handOffReentrantRun(results.all, reflect.Value{}, arguments)
		}
		if handedOff && !vm.topLevelRun && licenceCounting {
			callbackReleaseCounters.callbackRuns.Add(1)
		}
	}
	return results
}

// pushClosureFrame pushes the frame closure runs in, with its registers allocated and its
// upvalues bound, swapping to the closure's program root.
//
// Takes closure (*RuntimeClosure) which is about to run.
//
// Returns int which is the new frame's index.
func (vm *VM) pushClosureFrame(closure *RuntimeClosure) int {
	callee := closure.Function
	snapshot := vm.swapToClosureRoot(closure.RootFunction)

	vm.guardCallDepth()
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	closureFramePointer := vm.FramePointer
	frame := &vm.CallStack[vm.FramePointer]
	if vm.Arena != nil {
		vm.Arena.SaveInto(&frame.arenaSave)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&frame.Registers, &callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
	} else {
		frame.Registers = NewRegisters(callee.NumRegisters)
	}
	frame.Function = callee
	frame.ProgramCounter = 0
	frame.returnDestination = nil
	frame.deferBase = len(vm.deferStack)
	if frame.simpleDefer != nil {
		frame.simpleDefer.active = false
	}
	frame.upvalues = nil
	frame.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(frame.sharedCells)
	frame.sharedCells = nil
	vm.recordFrameSnapshot(closureFramePointer, snapshot)
	if closure.upvalues != nil {
		frame.initialiseUpvalues(closure.upvalues, vm.Arena)
	}
	vm.updateASMCallInfoBase()
	return closureFramePointer
}
