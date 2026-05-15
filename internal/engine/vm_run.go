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
	"context"
	"errors"
	"fmt"
	"runtime"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

const (
	// CostChunk is the number of cost units a VM draws from the shared tracker budget at a
	// time. Larger chunks touch the shared atomic less often; smaller chunks bound the
	// per-VM overshoot more tightly.
	CostChunk = 1024

	// UnmeteredCostSentinel is the local counter value of a VM whose execution has no cost
	// budget. It keeps the per-instruction check to a single signed compare: a metered VM's
	// counter is never left negative, because the instruction that drains it refills or
	// aborts before returning to the dispatch loop.
	UnmeteredCostSentinel = -1
)

// RunDispatchedGuarded wraps runDispatched with a recover() so that a native Go panic
// raised by an opcode handler (for example the invalid-register and not-a-struct
// diagnostics in vm_bounds_check.go, or any reflect operation that panics) is contained
// inside the interpreter instead of unwinding into and crashing the host.
//
// Takes baseFramePointer (int) which specifies the frame index at which this invocation
// should stop and return results.
//
// Returns the execution result and any error encountered during dispatch.
func (vm *VM) RunDispatchedGuarded(baseFramePointer int) (result any, err error) {
	if vm.lockInterpreter() {
		defer vm.unlockInterpreter()
	}
	defer func() { vm.handleRecoveredHandlerPanic(recover(), baseFramePointer, &result, &err) }()
	vm.DispatchEntries++
	return vm.runDispatched(baseFramePointer)
}

// RunGuarded wraps the pure-Go run loop with the same handler-panic recovery as
// RunDispatchedGuarded. Used by the variable-initialiser path, which dispatches via run
// directly rather than runDispatched.
//
// Takes baseFramePointer (int) which specifies the frame index at which this invocation
// should stop and return results.
//
// Returns the execution result and any error encountered during dispatch.
func (vm *VM) RunGuarded(baseFramePointer int) (result any, err error) {
	if vm.lockInterpreter() {
		defer vm.unlockInterpreter()
	}
	defer func() { vm.handleRecoveredHandlerPanic(recover(), baseFramePointer, &result, &err) }()
	return vm.run(baseFramePointer)
}

// AccountForInstructionCost charges the per-opcode cost against the VM's local cost
// chunk, refilling from the shared tracker budget when it runs out.
//
// Takes instruction (isa.Instruction) which identifies the cost-table entry to charge.
//
// Returns true when the shared budget is exhausted and the associated error.
func (vm *VM) AccountForInstructionCost(instruction isa.Instruction) (bool, error) {
	if vm.CostRemaining < 0 {
		return false, nil
	}
	return vm.chargeInstructionCost(instruction)
}

// RefillCostChunk moves up to CostChunk units from the shared tracker budget into the
// VM's local counter, absorbing any deficit already accrued.
//
// Returns true with errCostBudgetExceeded when the shared budget cannot cover the local
// deficit, and false with nil when metering is disabled or the refill succeeded.
func (vm *VM) RefillCostChunk() (bool, error) {
	if vm.Limits.CostBudget == 0 {
		vm.CostRemaining = UnmeteredCostSentinel
		return false, nil
	}
	shared := &vm.Limits.Tracker.CostRemaining
	for {
		available := shared.Load()
		if available <= 0 {
			return true, fault.ErrCostBudgetExceeded
		}
		take := min(available, CostChunk)
		if shared.CompareAndSwap(available, available-take) {
			vm.CostRemaining += take
			if vm.CostRemaining <= 0 {
				return true, fault.ErrCostBudgetExceeded
			}
			return false, nil
		}
	}
}

// SeedCostBudget resets the shared tracker budget to the configured total at the start of
// a top-level execution and empties the local counter so the first metered instruction
// refills from the tracker. When metering is disabled the local counter is parked at
// UnmeteredCostSentinel instead.
func (vm *VM) SeedCostBudget() {
	vm.CostRemaining = 0
	if vm.Limits.CostBudget == 0 || vm.Limits.Tracker == nil {
		vm.CostRemaining = UnmeteredCostSentinel
		return
	}
	vm.Limits.Tracker.CostRemaining.Store(vm.Limits.CostBudget)
}

// ReturnUnspentCostChunk hands the VM's unspent local chunk back to the shared tracker
// budget when a child VM exits, so work the child never performed remains available to
// its siblings and to the main goroutine.
func (vm *VM) ReturnUnspentCostChunk() {
	if vm.CostRemaining <= 0 || vm.Limits.CostBudget == 0 || vm.Limits.Tracker == nil {
		return
	}
	vm.Limits.Tracker.CostRemaining.Add(vm.CostRemaining)
	vm.CostRemaining = 0
}

// CostUsed reports the cost consumed by the execution as a whole: the configured budget
// less what remains in the shared tracker and in this VM's local chunk.
//
// Returns the consumed cost, or zero when metering is disabled.
func (vm *VM) CostUsed() int64 {
	if vm.Limits.CostBudget == 0 || vm.Limits.Tracker == nil {
		return 0
	}
	remaining := vm.Limits.Tracker.CostRemaining.Load() + max(vm.CostRemaining, 0)
	return vm.Limits.CostBudget - remaining
}

// handleRecoveredHandlerPanic forwards a recovered handler panic.
//
// The deferred body shared by RunDispatchedGuarded and RunGuarded. Receives the value
// already recovered by the enclosing deferred closure, converts a native Go panic raised
// by an opcode handler into an interpreted panic so interpreted defer/recover can observe
// it; an arena-budget panic is re-raised so execute's own recover continues to surface it
// as an error. The switch names the exit reasons that need work here. Every other
// isa.OpResult means "keep running the loop", which is what falling out of the switch
// does; that path is the overwhelming majority of dispatches.
//
// Takes recovered (any) which is the value returned by recover() in the deferred closure,
// or nil when no panic is pending.
// Takes baseFramePointer (int) which is the frame index at which dispatch should stop.
// Takes result (*any) which receives the recovered execution result.
// Takes err (*error) which receives the surfaced error, if any.
func (vm *VM) handleRecoveredHandlerPanic(recovered any, baseFramePointer int, result *any, err *error) {
	if recovered == nil {
		return
	}
	if budgetErr, ok := recovered.(error); ok && errors.Is(budgetErr, fault.ErrArenaBudgetExceeded) {
		panic(recovered)
	}
	if invariant, ok := recovered.(*invariantError); ok {
		vm.evalError = nil
		*result = nil
		*err = invariant
		return
	}

	if vm.liveCtx != nil && vm.Arena != nil && vm.liveCtx.arenaBanksDirty != 0 {
		syncArenaIndicesFromASM(vm.Arena, vm.liveCtx)
		vm.liveCtx.arenaBanksDirty = 0
	}

	savedBase := vm.baseFramePointer
	vm.baseFramePointer = baseFramePointer
	switch raiseNativePanicAsInterpreted(vm, nativeHandlerPanicValue(recovered)) {
	case opDone:
		vm.baseFramePointer = savedBase
		*result = vm.evalResult
		vm.evalResult = nil
		*err = nil
	case opFrameChanged:

		*result, *err = vm.RunDispatchedGuarded(baseFramePointer)
		vm.baseFramePointer = savedBase
	default:
		vm.baseFramePointer = savedBase
		*result = nil
		*err = vm.evalError
		vm.evalError = nil
	}
}

// run is the main execution loop, dispatching all opcodes via flatDispatchSwitch (Path-B)
// defined in vm_handler_flat_switch.go. The ASM dispatch loop (Path-A) covers most
// opcodes inline and only returns to this loop on exit reasons that need a Go-side
// handler.
//
// Takes baseFramePointer (int) which specifies the frame index at which this invocation
// should stop and return results.
//
// Returns the execution result and any error encountered during dispatch.
//
//revive:disable:cognitive-complexity // VM dispatch loops are inherently complex.
func (vm *VM) run(baseFramePointer int) (any, error) {
	savedBaseFp := vm.baseFramePointer
	vm.baseFramePointer = baseFramePointer
	vm.debugEnter()
	defer func() {
		vm.baseFramePointer = savedBaseFp
		vm.debugLeave()
	}()

	frame := &vm.CallStack[vm.FramePointer]
	registers := &frame.Registers

	var ops uint32
	for {
		ops++
		if ops&cancellationCheckMask == 0 {
			if done, result, err := vm.runPeriodicChecks(); done {
				return result, err
			}
			if vm.checkpointFlags != 0 || (vm.Arena != nil && vm.Arena.gcShouldRun()) {
				vm.runPendingCheckpoints()
				frame = &vm.CallStack[vm.FramePointer]
				registers = &frame.Registers
			}
			vm.yieldInterpreterLock()
		}
		if vm.debugStopRequested(frame) {
			return nil, vm.debugStopError()
		}
		if frame.ProgramCounter >= len(frame.Function.Body) {
			done, result, err := vm.handleEndOfBody(frame, baseFramePointer)
			if done {
				return result, err
			}
			frame = &vm.CallStack[vm.FramePointer]
			registers = &frame.Registers
			continue
		}

		instruction := frame.Function.Body[frame.ProgramCounter]
		frame.ProgramCounter++

		if exhausted, err := vm.AccountForInstructionCost(instruction); exhausted {
			return nil, err
		}
		vm.maybeYield()

		rc := flatDispatchSwitch(vm, frame, registers, instruction)
		if rc == opContinue {
			continue
		}
		result, terminal, err := vm.handleOpResult(rc)
		if terminal {
			return result, err
		}
		frame = &vm.CallStack[vm.FramePointer]
		registers = &frame.Registers
	}
}

// chargeInstructionCost is the metered half of AccountForInstructionCost, kept out of
// line so the unmetered check inlines into the dispatch loop. A zero counter refills
// before charging, which also parks a VM that was never seeded at the sentinel.
//
// Takes instruction (isa.Instruction) which identifies the cost-table entry to charge.
//
// Returns true when the shared budget is exhausted and the associated error to surface
// from run.
func (vm *VM) chargeInstructionCost(instruction isa.Instruction) (bool, error) {
	if vm.CostRemaining == 0 {
		if exhausted, err := vm.RefillCostChunk(); exhausted || vm.CostRemaining < 0 {
			return exhausted, err
		}
	}
	vm.CostRemaining -= vm.Limits.CostTable[isa.FlatIndexFor(instruction)]
	if vm.CostRemaining > 0 {
		return false, nil
	}
	return vm.RefillCostChunk()
}

// maybeYield optionally calls runtime.Gosched() when the yield interval is enabled and
// the per-VM counter aligns with it. The yield is a cooperative scheduling hint; it never
// blocks the VM.
func (vm *VM) maybeYield() {
	if vm.Limits.YieldInterval == 0 {
		return
	}
	vm.yieldCounter++
	if vm.yieldCounter&(vm.Limits.YieldInterval-1) == 0 {
		runtime.Gosched()
	}
}

// runPeriodicChecks inspects the cancellation and panic flags.
//
// When a terminal condition is observed, returns (true, result, err); the dispatch loop
// must then exit. Otherwise returns (false, nil, nil) to indicate execution may continue.
//
// Returns done (bool) which is true when the dispatch loop should exit immediately.
// Returns result (any) which is the propagated result value to surface from run.
// Returns err (error) which is any error encountered during the periodic checks.
func (vm *VM) runPeriodicChecks() (done bool, result any, err error) {
	if vm.Cancelled.Load() != 0 {
		return true, nil, cancellationError(vm.ctx)
	}
	if info := vm.Globals.goroutinePanic.Load(); info != nil {
		return true, nil, newGoroutinePanicError(info.value)
	}
	return false, nil, nil
}

// handleEndOfBody processes the end-of-body condition for the current frame by running
// defers, then either returning the result for the base frame or popping the frame and
// continuing.
//
// Takes frame (*CallFrame) which specifies the current call frame being completed.
// Takes baseFramePointer (int) which specifies the frame index that marks the bottom of
// this run invocation.
//
// Returns (true, result, err) when the caller should return, or (false, _, _) to continue
// the loop.
func (vm *VM) handleEndOfBody(frame *CallFrame, baseFramePointer int) (bool, any, error) {
	atBase, atRoot, result, err := vm.finaliseFrameAtEnd(frame, baseFramePointer)
	if atBase {
		return true, result, err
	}
	if atRoot {
		return true, nil, nil
	}
	return false, nil, nil
}

// finaliseFrameAtEnd runs deferred work and pops a returning frame.
//
// Takes frame (*CallFrame) which is the returning call frame.
// Takes baseFramePointer (int) which is the base frame pointer of the current dispatch
// invocation.
//
// Returns atBase, atRoot, the extracted result, and any error.
func (vm *VM) finaliseFrameAtEnd(frame *CallFrame, baseFramePointer int) (atBase bool, atRoot bool, result any, err error) {
	frame.ProgramCounter = len(frame.Function.Body) + 1
	if call, ok := vm.takeInlineDefer(frame); ok {
		vm.pushDeferredFrame(call)
		return false, false, nil, nil
	}
	if frame.simpleDefer != nil && frame.simpleDefer.active {
		vm.runFrameSimpleDefer(frame)
	}
	if len(vm.deferStack) > frame.deferBase {
		vm.runDefers()
	}
	if vm.evalError != nil {
		err = vm.evalError
		vm.evalError = nil
		return true, false, nil, err
	}
	if vm.FramePointer < baseFramePointer {
		return false, true, nil, nil
	}
	if vm.FramePointer == baseFramePointer {
		result, err = vm.extractResult(frame)
		vm.popFrame()
		return true, false, result, err
	}
	vm.popFrame()
	if vm.FramePointer < baseFramePointer {
		return false, true, nil, nil
	}
	return false, false, nil, nil
}

// handleOpResult translates an opcode handler return code into either a terminal result
// or a signal that execution continues (the common case, and also an interpreted panic
// that unwound to a recovering frame). The mapping is resolveTerminalResult's.
//
// Takes rc (OpResult) which specifies the opcode handler return code.
//
// Returns any which is the result value when terminal.
// Returns bool which is true when execution should stop.
// Returns error when a panic escaped the interpreter.
func (vm *VM) handleOpResult(rc OpResult) (result any, terminal bool, err error) {
	outcome, isTerminal := vm.resolveTerminalResult(rc)
	if !isTerminal || outcome.action == loopRebuild {
		return nil, false, nil
	}
	return outcome.result, true, outcome.err
}

// cancellationError reports why ctx ended as a fault.ErrExecutionCancelled error whose
// chain also reaches the context's cause and its own error (context.Canceled or
// context.DeadlineExceeded), whichever of those the caller wants to test for. A deadlock
// cause is returned as itself: it is a program failure, not a cancellation.
//
// Returns error which satisfies errors.Is against fault.ErrExecutionCancelled and against
// the context's own error.
func cancellationError(ctx context.Context) error {
	cause := context.Cause(ctx)
	ctxErr := ctx.Err()
	if cause == nil {
		cause = ctxErr
	}
	if errors.Is(cause, fault.ErrDeadlock) || errors.Is(cause, fault.ErrDebuggerStop) {
		return cause
	}
	var parts []error
	if !errors.Is(cause, fault.ErrExecutionCancelled) {
		parts = append(parts, fault.ErrExecutionCancelled)
	}
	parts = append(parts, cause)
	if ctxErr != nil && !errors.Is(cause, ctxErr) {
		parts = append(parts, ctxErr)
	}
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return fmt.Errorf(fault.ErrChainFmt, parts[0], parts[1])
	default:
		return fmt.Errorf("%w: %w: %w", parts[0], parts[1], parts[2])
	}
}

// nativeHandlerPanicValue normalises a recovered host panic into a value suitable for
// interpreted recover(). String panics raised by the VM diagnostics in vm_bounds_check.go
// are wrapped in a runtimePanicError so fmt formatting matches Go's runtime error
// surface; error and other values pass through unchanged.
//
// Takes recovered (any) which is the non-nil value returned by the host recover().
//
// Returns the value to hand to raiseNativePanicAsInterpreted.
func nativeHandlerPanicValue(recovered any) any {
	if message, ok := recovered.(string); ok {
		return &runtimePanicError{message: message}
	}
	return recovered
}
