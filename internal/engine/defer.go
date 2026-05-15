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
)

// runDefers executes all deferred calls registered by the current frame.
//
// Called on normal function return in LIFO order. Each defer is popped from the stack
// BEFORE it runs, so if it panics the remaining (un-run) defers are still on the stack
// for the panic-unwind path (unwindFrame) to run with recover semantics.
func (vm *VM) runDefers() {
	frame := vm.currentFrame()
	base := frame.deferBase
	for len(vm.deferStack) > base {
		call := vm.deferStack[len(vm.deferStack)-1]
		vm.deferStack = vm.deferStack[:len(vm.deferStack)-1]
		if call.started {
			continue
		}
		vm.executeDeferredCall(call)
	}
}

// runFrameSimpleDefer executes the trivial-defer slot armed on this frame and clears it.
// Used by handleReturn / handleReturnVoid / unwindFrame on the fast path that avoids
// appending to the global deferStack.
//
// The classifier guarantees the deferred call body cannot call recover(), so the slot
// must run unconditionally, even during a panic unwind, where the panic state is
// preserved across the call.
//
// Takes frame (*CallFrame) which owns the trivial-defer slot.
func (vm *VM) runFrameSimpleDefer(frame *CallFrame) {
	record := frame.simpleDefer
	if record == nil || !record.active {
		return
	}
	record.active = false
	if record.consumed {
		record.consumed = false
		return
	}
	deferred := deferredCall{Function: record.target,
		nativeFunction: record.nativeFunction,
		arguments:      record.arguments,
		frameIndex:     vm.FramePointer, direct: nil, started: false, builtin: 0, spread: false}
	if record.direct {
		deferred.direct = record
	}
	record.target = nil
	record.nativeFunction = reflect.Value{}
	record.arguments = nil
	vm.executeDeferredCall(deferred)
}

// unwindGoexit handles a runtime.Goexit by running every armed defer.
//
// Walks frames from the current frame down to the base frame in LIFO order, popping each.
// recover() inside these deferred functions observes vm.panicking == false and returns
// nil, matching Go's "Because Goexit is not a panic, any recover calls in those deferred
// functions will return nil" semantics. The interpreter never calls the real
// runtime.Goexit; the host goroutine running the VM exits via normal return when vm.run
// finishes.
func (vm *VM) unwindGoexit() {
	for vm.FramePointer >= vm.baseFramePointer {
		frame := vm.currentFrame()
		if frame.simpleDefer != nil && frame.simpleDefer.active {
			vm.runFrameSimpleDefer(frame)
		}
		for i := len(vm.deferStack) - 1; i >= frame.deferBase; i-- {
			if vm.deferStack[i].started {
				continue
			}
			vm.executeDeferredCall(vm.deferStack[i])
		}
		vm.deferStack = vm.deferStack[:frame.deferBase]
		vm.popFrame()
	}
}

// unwindPanic handles panic unwinding by running deferred calls for each frame in LIFO
// order, catching panics when a deferred call contains a recover().
//
// Returns nil if the panic was recovered, or an error wrapping the panic value otherwise.
func (vm *VM) unwindPanic() error {
	for vm.FramePointer >= vm.baseFramePointer && vm.FramePointer >= 0 {
		if vm.unwindFrame() {
			return nil
		}
	}
	uncaught := newuncaughtPanicError(vm.panicValue)
	uncaught.stack = vm.panicStackWithCallback()
	return uncaught
}

// unwindFrame runs deferred calls for the current frame during unwinding, popping each
// defer before its body runs so a recover-then-re-panic never re-executes the same
// record, and pops the frame in either case.
//
// Returns true if a recover() was found and the panic was caught, or false otherwise.
func (vm *VM) unwindFrame() bool {
	enterFramePointer := vm.FramePointer
	frame := vm.currentFrame()
	if frame.simpleDefer != nil && frame.simpleDefer.active {
		vm.runFrameSimpleDefer(frame)
		if vm.FramePointer < enterFramePointer {
			return !vm.panicking
		}
		if !vm.panicking {
			vm.runRemainingDefers(frame.deferBase)
			vm.syncNamedResults(frame)
			vm.deliverRecoveredReturn(frame)
			vm.panicUnwound = nil
			vm.popFrame()
			return true
		}
	}
	for len(vm.deferStack) > frame.deferBase {
		call := vm.deferStack[len(vm.deferStack)-1]
		vm.deferStack = vm.deferStack[:len(vm.deferStack)-1]
		if call.started {
			continue
		}
		vm.executeDeferredCall(call)
		if vm.FramePointer < enterFramePointer {
			return !vm.panicking
		}
		if !vm.panicking {
			vm.runRemainingDefers(frame.deferBase)
			vm.syncNamedResults(frame)
			vm.deliverRecoveredReturn(frame)
			vm.panicUnwound = nil
			vm.popFrame()
			return true
		}
	}
	vm.recordUnwoundFrame(frame)
	vm.popFrame()
	return false
}

// recordUnwoundFrame remembers a frame the panic is about to pop, so a deferred function
// that inspects the stack still sees it beneath runtime.gopanic as it would in Go, where
// panicking frames stay on the stack until a recover completes.
//
// Takes frame (*CallFrame) which is the frame being popped.
func (vm *VM) recordUnwoundFrame(frame *CallFrame) {
	if frame.Function == nil {
		return
	}
	vm.panicUnwound = append(vm.panicUnwound, unwoundFrame{function: frame.Function, pc: frameInstructionIndex(frame)})
}

// runRemainingDefers drains every deferred call above base.
//
// Executes the calls in LIFO order with the same pop-before-execute discipline as
// unwindFrame so nested unwindPanic activity cannot re-run an entry. Called once a
// recover() has caught the panic and the frame is being finalised; the remaining defers
// run in normal (non-panicking) mode.
//
// Takes base (int) which specifies the lowest defer stack index to drain down to.
func (vm *VM) runRemainingDefers(base int) {
	for len(vm.deferStack) > base {
		call := vm.deferStack[len(vm.deferStack)-1]
		vm.deferStack = vm.deferStack[:len(vm.deferStack)-1]
		if call.started {
			continue
		}
		vm.executeDeferredCall(call)
	}
}

// executeDeferredCall runs a single deferred closure call by pushing a new call frame and
// executing it.
//
// Takes call (deferredCall) which specifies the deferred call record containing the
// closure and arguments.
func (vm *VM) executeDeferredCall(call deferredCall) {
	if call.builtin != 0 {
		vm.executeBuiltinDeferredCall(call)
		return
	}
	if call.Function == nil {
		vm.executeNativeDeferredCall(call)
		return
	}
	vm.pushDeferredFrame(call)

	priorRecoverEligible := vm.recoverEligibleFrame
	vm.recoverEligibleFrame = vm.FramePointer
	recorded := vm.panicking
	if recorded {
		vm.panicDeferFrames = append(vm.panicDeferFrames, panicDeferRecord{frameIndex: vm.FramePointer, unwound: append([]unwoundFrame(nil), vm.panicUnwound...)})
	}

	_, err := vm.RunDispatchedGuarded(vm.FramePointer)
	if recorded && len(vm.panicDeferFrames) > 0 {
		vm.panicDeferFrames = vm.panicDeferFrames[:len(vm.panicDeferFrames)-1]
	}
	vm.recoverEligibleFrame = priorRecoverEligible
	if err != nil {
		vm.evalError = err
	}
}

// pushDeferredFrame pushes a call frame for the deferred closure.
//
// Places captured arguments and refreshes the per-frame ASM call-info base exactly as
// every other interpreted-frame-push path does, so the callee's inline call fast path
// indexes its own call-site table. The caller decides whether the frame runs under a
// nested dispatch (executeDeferredCall()) or inside the returning frame's own dispatch
// loop (takeInlineDefer()).
//
// Takes call (deferredCall) which holds the compiled closure and its arguments.
func (vm *VM) pushDeferredFrame(call deferredCall) {
	callee := call.Function.Function
	deferSave, deferRegs := vm.allocateDeferredFrameRegisters(callee.NumRegisters)
	newFrame := CallFrame{Registers: deferRegs,
		Function:       callee,
		ProgramCounter: 0,
		deferBase:      len(vm.deferStack),
		arenaSave:      deferSave, sharedCells: nil, simpleDefer: nil, upvalues: nil, returnDestination: nil, hasGeneralAlloc: false, rootSwapped: false}
	if call.Function.upvalues != nil {
		newFrame.initialiseUpvalues(call.Function.upvalues, vm.Arena)
	}
	if call.direct != nil {
		placeDirectDeferArgs(&newFrame.Registers, call.direct, callee.ParameterKinds, vm.Arena)
		call.direct.ClearDirect()
	} else {
		placeReflectArgs(&newFrame.Registers, call.arguments, callee.ParameterKinds, vm.Arena)
	}
	snapshot := vm.swapToClosureRoot(call.Function.RootFunction)
	vm.guardCallDepth()
	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}

	releaseSharedCellMap(vm.CallStack[vm.FramePointer].sharedCells)
	vm.CallStack[vm.FramePointer] = newFrame
	vm.recordFrameSnapshot(vm.FramePointer, snapshot)
	vm.updateASMCallInfoBase()
}

// takeInlineDefer selects the next inline-eligible deferred call.
//
// Picks a compiled closure that cannot call recover() from the returning frame so the
// return handler can push it as an ordinary frame and re-execute the return once it has
// run. The frame record is marked consumed and a stack entry is marked started rather
// than removed, so the assembly return guard still sees a pending defer and routes the
// re-executed return through the Go handler. Deferred calls that target native functions,
// or closures that call recover(), are left for the nested-dispatch path, which preserves
// their recover semantics.
//
// Takes frame (*CallFrame) which is the returning frame.
//
// Returns deferredCall which is the taken call.
// Returns bool which is true when a call was taken.
func (vm *VM) takeInlineDefer(frame *CallFrame) (deferredCall, bool) {
	if record := frame.simpleDefer; record != nil && record.active {
		if record.consumed || record.target == nil || record.target.Function.HasRecover {
			return deferredCall{}, false
		}
		record.consumed = true
		call := deferredCall{Function: record.target, arguments: record.arguments, frameIndex: vm.FramePointer, direct: nil, nativeFunction: reflect.Value{}, started: false, builtin: 0, spread: false}
		if record.direct {
			call.direct = record
		}
		record.target = nil
		record.arguments = nil
		return call, true
	}
	top := len(vm.deferStack) - 1
	for top >= frame.deferBase && vm.deferStack[top].started {
		vm.deferStack = vm.deferStack[:top]
		top--
	}
	if top < frame.deferBase {
		return deferredCall{}, false
	}
	call := vm.deferStack[top]
	if call.Function == nil || call.Function.Function.HasRecover {
		return deferredCall{}, false
	}
	vm.deferStack[top].started = true
	return call, true
}

// startInlineDefer rewinds the frame and pushes the next inline-eligible deferred call.
//
// The deferred body runs in the current dispatch loop and the return re-executes with the
// record consumed. The program counter is rewound before the push because growing the
// call stack can move the frame.
//
// Takes frame (*CallFrame) which is the returning frame, with its program counter already
// advanced past the return instruction.
//
// Returns bool which is true when a deferred frame was pushed and the handler must report
// a frame change.
func (vm *VM) startInlineDefer(frame *CallFrame) bool {
	call, ok := vm.takeInlineDefer(frame)
	if !ok {
		return false
	}
	frame.ProgramCounter--
	vm.pushDeferredFrame(call)
	return true
}
