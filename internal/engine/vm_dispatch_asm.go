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

import (
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// runDispatched executes bytecode starting from baseFramePointer using the ASM dispatch
// loop, falling back to Go for tier-2 opcodes via a trampoline pattern.
//
// Takes baseFramePointer (int) which specifies the call stack frame to return from when
// execution completes.
//
// Returns the execution result and any error encountered.
func (vm *VM) runDispatched(baseFramePointer int) (any, error) {
	return vm.runDispatchedLoop(baseFramePointer)
}

// recordDispatchExit re-reads the current frame after the assembly loop exits and copies
// the program counter the assembly left in ctx into it.
//
// Takes ctx (*dispatchContext) which mirrors the frame that was running.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which is the active register file.
//
// Returns the refreshed frame and registers, unchanged when no frame is active.
func (vm *VM) recordDispatchExit(ctx *dispatchContext, frame *CallFrame, registers *Registers) (*CallFrame, *Registers) {
	if vm.FramePointer < 0 {
		return frame, registers
	}
	frame = &vm.CallStack[vm.FramePointer]
	registers = &frame.Registers
	frame.ProgramCounter = int(ctx.programCounter)
	return frame, registers
}

// saveExitedFrameIfPushed writes the dispatch state ctx carries into the exited frame's
// save slot when the Go-side exit handling pushed frames on top of it.
//
// Takes ctx (*dispatchContext) which holds the exited frame's dispatch state.
// Takes exitedFramePointer (int) which is the frame pointer at the exit.
func (vm *VM) saveExitedFrameIfPushed(ctx *dispatchContext, exitedFramePointer int) {
	if vm.FramePointer > exitedFramePointer {
		vm.saveDispatchRegistersForFrame(ctx, exitedFramePointer)
	}
}

// runDispatchedLoop is the body of runDispatched().
//
// Takes baseFramePointer (int) which is the frame the run returns to.
//
// Returns the result and error of the run.
func (vm *VM) runDispatchedLoop(baseFramePointer int) (any, error) {
	if vm.Limits.ForceGoDispatch || vm.Limits.SafeMode || vm.Limits.Debug != nil {
		return vm.run(baseFramePointer)
	}

	savedBaseFp := vm.baseFramePointer
	vm.baseFramePointer = baseFramePointer
	defer func() { vm.baseFramePointer = savedBaseFp }()

	frame := &vm.CallStack[vm.FramePointer]
	registers := &frame.Registers

	var ctx dispatchContext

	vm.buildDispatchContext(&ctx, &asmJumpTable)
	vm.saveCurrentDispatchRegisters(&ctx)

	for {
		if vm.dispatchCheckpointDue() {
			frame, registers = vm.runDispatchCheckpoint(&ctx)
		}
		ctx.pollBudget = dispatchPollBudget
		dispatchLoop(&ctx)

		vm.syncCallContextFromASM(&ctx)
		exitedFramePointer := vm.FramePointer
		frame, registers = vm.recordDispatchExit(&ctx, frame, registers)

		if vm.Cancelled.Load() != 0 {
			return nil, cancellationError(vm.ctx)
		}

		result, action, err := vm.handleDispatchExit(
			&ctx, frame, registers, baseFramePointer,
		)
		if action == loopReturn {
			return result, err
		}
		if action == loopContinue {
			continue
		}

		vm.saveExitedFrameIfPushed(&ctx, exitedFramePointer)
		frame = &vm.CallStack[vm.FramePointer]
		registers = &frame.Registers
		vm.rebuildDispatchPointers(&ctx, frame, registers)
	}
}

// handleDispatchExit routes an ASM dispatch exit to the appropriate handler and returns
// the action the main loop should take.
//
// Takes ctx (*dispatchContext) which provides the dispatch context.
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes baseFramePointer (int) which specifies the base frame for return detection.
//
// Returns the result, the dispatch action, and any error.
//
//nolint:revive // hot path
func (vm *VM) handleDispatchExit(
	ctx *dispatchContext,
	frame *CallFrame,
	registers *Registers,
	baseFramePointer int,
) (any, dispatchAction, error) {
	switch ctx.exitReason {
	case exitPoll:
		return nil, loopContinue, nil
	case exitEndOfCode:
		return vm.processEndOfCode(frame, baseFramePointer)
	case exitCall:
		return vm.processExitCall(ctx, frame, registers)
	case exitReturn:
		return vm.processExitReturn(frame, registers)
	case exitReturnVoid:
		return vm.processExitReturnVoid(frame, registers)
	case exitTailCall:
		vm.processExitTailCall(frame, registers)
		return nil, loopRebuild, nil
	case exitPathB:
		return vm.processExitPathB(frame, registers, ctx)
	case exitSetField:
		return vm.processExitSetField(frame, registers, ctx)
	case exitGetField:
		return vm.processExitGetField(frame, registers, ctx)
	case exitMapIndex:
		return vm.processExitMapIndex(frame, registers, ctx)
	case exitAppend:
		return vm.processExitAppend(frame, registers, ctx)
	case exitAppendByteFast:
		return vm.processExitAppendByteFast(frame, registers, ctx)
	case exitSliceSetStringDirect:
		return vm.processExitSliceSetStringDirect(frame, registers, ctx)
	case exitAppendStructFast:
		return vm.processExitThroughHandler(frame, registers, ctx, handleAppendStructFast)
	case exitAppendIntFast:
		return vm.processExitThroughHandler(frame, registers, ctx, handleAppendInt)
	case exitAppendFloatFast:
		return vm.processExitThroughHandler(frame, registers, ctx, handleAppendFloat)
	case exitAppendStringFast:
		return vm.processExitThroughHandler(frame, registers, ctx, handleAppendString)
	case exitTestNilJumpFalse:
		return vm.processExitTestNilJumpFalse(frame, registers, ctx)
	case exitTestNilJumpTrue:
		return vm.processExitTestNilJumpTrue(frame, registers, ctx)
	case exitFrameChanged:
		vm.updateASMCallInfoBase()
		return nil, loopRebuild, nil
	case exitGetStructFieldIntT0:
		return vm.processExitGetStructFieldIntT0(frame, registers, ctx)
	case exitGetStructFieldUintT0:
		return vm.processExitGetStructFieldUintT0(frame, registers, ctx)
	case exitGetStructFieldFloatT0:
		return vm.processExitGetStructFieldFloatT0(frame, registers, ctx)
	case exitGetStructFieldBoolT0:
		return vm.processExitGetStructFieldBoolT0(frame, registers, ctx)
	case exitSetStructFieldIntT0:
		return vm.processExitSetStructFieldIntT0(frame, registers, ctx)
	case exitSetStructFieldUintT0:
		return vm.processExitSetStructFieldUintT0(frame, registers, ctx)
	case exitSetStructFieldFloatT0:
		return vm.processExitSetStructFieldFloatT0(frame, registers, ctx)
	case exitSetStructFieldBoolT0:
		return vm.processExitSetStructFieldBoolT0(frame, registers, ctx)
	case exitGetStructFieldGeneralT0:
		return vm.processExitGetStructFieldGeneralT0(frame, registers, ctx)
	case exitSetStructFieldGeneralT0:
		return vm.processExitSetStructFieldGeneralT0(frame, registers, ctx)
	case exitDivByZero:
		return vm.raiseDivByZeroForDispatch()
	case exitCallOverflow, exitStackOverflowPathB:
		return nil, loopReturn, vm.stackOverflowError()
	case exitPanicErrorPathB:
		err := vm.evalError
		vm.evalError = nil
		return nil, loopReturn, err
	case exitDonePathB:
		result := vm.evalResult
		vm.evalResult = nil
		return result, loopReturn, nil
	default:
		return nil, loopRebuild, nil
	}
}

// processExitSetField is the direct-exit Go handler for isa.OpSetField.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetField(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetField(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitGetField is the direct-exit Go handler for isa.OpGetField.
//
// Mirrors processExitSetField: skips handlerTable[op] on the first op, then continues
// batching for subsequent trampolining ops.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetField(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetField(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitMapIndex is the direct-exit Go handler for isa.OpMapIndex.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitMapIndex(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleMapIndex(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// dispatchCheckpointDue reports whether the arena GC checkpoint must run before the
// assembly dispatcher is re-entered.
//
// Returns true when a checkpoint flag is pending or the arena has crossed its GC
// threshold, and the VM still has a frame to service it from.
func (vm *VM) dispatchCheckpointDue() bool {
	if vm.FramePointer < 0 {
		return false
	}
	return vm.checkpointFlags != 0 || (vm.Arena != nil && vm.Arena.gcShouldRun())
}

// runDispatchCheckpoint services the pending checkpoints and rebuilds the dispatch
// context's cached pointers, because MinorGC may have retired slabs it pointed at.
//
// Takes ctx (*dispatchContext) which is the context about to re-enter assembly.
//
// Returns the current frame and its register file.
func (vm *VM) runDispatchCheckpoint(ctx *dispatchContext) (*CallFrame, *Registers) {
	vm.runPendingCheckpoints()
	frame := &vm.CallStack[vm.FramePointer]
	registers := &frame.Registers
	vm.rebuildDispatchPointers(ctx, frame, registers)
	return frame, registers
}

// processExitThroughHandler is the shared body of the direct exits that run one Go
// handler for the instruction at the exit PC and resume through the batched tier-2 loop.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
// Takes handler (opcodeHandler) which is the Go handler for the exited opcode.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitThroughHandler(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
	handler func(*VM, *CallFrame, *Registers, isa.Instruction) OpResult,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handler(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitAppend is the direct-exit Go handler for isa.OpAppend.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitAppend(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleAppend(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitAppendByteFast is the direct-exit Go handler for the specialised
// byte-builder opcode.
//
// Skips both the handlerTable[op] indirect call and the generic tier-2 processor.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitAppendByteFast(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleAppendByteFast(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitSliceSetStringDirect is the direct-exit Go handler for the tier-1 []string
// element store, which leaves assembly so the stored string passes the arena string
// barrier before it can reach a heap-owned backing.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSliceSetStringDirect(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSubOpSliceSetStringDirect(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// Tier-0 struct-field READER direct-exit Go handlers follow. Each one mirrors
// processExitGetField's shape: read the current instruction, advance PC, dispatch
// directly to the matching tier-0 Go handler (skipping the handlerTable[op] indirect
// lookup), then run the shared post-first-op batching loop. Subsequent trampolining ops
// in the same Go entry still go through the generic indirect-dispatch loop, so the saving
// is the single handlerTable[op] miss the trampoline's entry op would otherwise pay.

// processExitGetStructFieldIntT0 is the direct-exit Go handler for
// isa.OpGetStructFieldIntT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetStructFieldIntT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetStructFieldIntT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitGetStructFieldUintT0 is the direct-exit Go handler for
// opGetStructFieldUintT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetStructFieldUintT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetStructFieldUintT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitGetStructFieldFloatT0 is the direct-exit Go handler for
// opGetStructFieldFloatT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetStructFieldFloatT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetStructFieldFloatT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitGetStructFieldBoolT0 is the direct-exit Go handler for
// opGetStructFieldBoolT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetStructFieldBoolT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetStructFieldBoolT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// Tier-0 struct-field WRITER direct-exit Go handlers follow (primitives only; the
// General-bank Set stays on the generic processExitPathB path because it needs
// runtimeTypedmemmove).

// processExitSetStructFieldIntT0 is the direct-exit Go handler for
// isa.OpSetStructFieldIntT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetStructFieldIntT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetStructFieldIntT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitSetStructFieldUintT0 is the direct-exit Go handler for
// opSetStructFieldUintT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetStructFieldUintT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetStructFieldUintT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitSetStructFieldFloatT0 is the direct-exit Go handler for
// opSetStructFieldFloatT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetStructFieldFloatT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetStructFieldFloatT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitSetStructFieldBoolT0 is the direct-exit Go handler for
// opSetStructFieldBoolT0.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetStructFieldBoolT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetStructFieldBoolT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitGetStructFieldGeneralT0 is the direct-exit Go handler for
// isa.OpGetStructFieldGeneral.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitGetStructFieldGeneralT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleGetStructFieldGeneralT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitSetStructFieldGeneralT0 is the direct-exit Go handler for
// isa.OpSetStructFieldGeneral. Same pattern as the reader; the handler still uses
// runtime_typedmemmove internally for the pointer/interface store - the direct exit only
// saves the handlerTable[op] indirect.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitSetStructFieldGeneralT0(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleSetStructFieldGeneralT0(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitTestNilJumpFalse is the direct-exit Go handler for isa.OpTestNilJumpFalse.
//
// The handler body runs in Go; the direct exit saves only the handlerTable[op] indirect
// on the trampoline's first op and fires once per recursive step in any *node-style
// pointer walker.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitTestNilJumpFalse(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleTestNilJumpFalse(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// processExitTestNilJumpTrue mirrors processExitTestNilJumpFalse for the inverted-sense
// nil test.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitTestNilJumpTrue(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	codeLength := int(ctx.codeLength)
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := handleTestNilJumpTrue(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, codeLength, body, rc)
}

// runPathBBatch is the path-B batching loop for consecutive Go-fallback ops.
//
// The per-op direct-exit handlers and processExitPathB() enter it with the result of the
// op they already ran, and it keeps dispatching consecutive Go-fallback ops
// (instructionWouldTrampoline) for up to pathBBatchLimit ops, saving one ASM/Go
// round-trip per batched op. On reaching the cap it hands control back to ASM so
// dispatchLoop() runs its cancellation check. Terminal results resolve through
// resolveTerminalResult(); any other non-continue result means the frame changed and
// dispatch must rebuild.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
// Takes codeLength (int) which is the cached length of body.
// Takes body ([]isa.Instruction) which is the bytecode body of the current function.
// Takes rc (OpResult) which is the return code from the op already run.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) runPathBBatch(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
	codeLength int,
	body []isa.Instruction,
	rc OpResult,
) (any, dispatchAction, error) {
	for range pathBBatchLimit {
		if rc != opContinue {
			if outcome, terminal := vm.resolveTerminalResult(rc); terminal {
				return outcome.result, outcome.action, outcome.err
			}
			vm.updateASMCallInfoBase()
			return nil, loopRebuild, nil
		}
		if frame.ProgramCounter >= codeLength || !instructionWouldTrampoline(body[frame.ProgramCounter]) {
			ctx.programCounter = int64(frame.ProgramCounter)
			ctx.deferStackLength = int64(len(vm.deferStack))
			return nil, loopContinue, nil
		}
		instruction := body[frame.ProgramCounter]
		frame.ProgramCounter++
		rc = flatDispatchSwitch(vm, frame, registers, instruction)
	}
	ctx.programCounter = int64(frame.ProgramCounter)
	ctx.deferStackLength = int64(len(vm.deferStack))
	return nil, loopContinue, nil
}

// processEndOfCode handles the end-of-code exit by running pending defers, popping the
// frame, and returning the result at the base frame.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes baseFramePointer (int) which specifies the base frame for return detection.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processEndOfCode(
	frame *CallFrame,
	baseFramePointer int,
) (any, dispatchAction, error) {
	atBase, atRoot, result, err := vm.finaliseFrameAtEnd(frame, baseFramePointer)
	switch {
	case atBase:
		return result, loopReturn, err
	case atRoot:
		return nil, loopReturn, nil
	default:
		return nil, loopRebuild, nil
	}
}

// processExitCall handles a compiled function call exit from the ASM dispatch loop.
//
// The switch names the exit reasons that need work here. Every other isa.OpResult means
// "keep running the loop", which is what falling out of the switch does; that path is the
// overwhelming majority of dispatches. The caller's dispatch-save slot has already been
// written by runDispatched(), which does so for every exit.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
//
// Returns any which is the result value on opDone.
// Returns dispatchAction which selects the next loop action.
// Returns error when the call caused a stack overflow or error.
func (vm *VM) processExitCall(
	_ *dispatchContext,
	frame *CallFrame,
	registers *Registers,
) (any, dispatchAction, error) {
	instruction := frame.Function.Body[frame.ProgramCounter]
	frame.ProgramCounter++
	switch handleCall(vm, frame, registers, instruction) {
	case opStackOverflow:
		return nil, loopReturn, vm.stackOverflowError()
	case opDone:
		result := vm.evalResult
		vm.evalResult = nil
		return result, loopReturn, nil
	case opPanicError:
		err := vm.evalError
		vm.evalError = nil
		return nil, loopReturn, err
	default:
	}
	vm.updateASMCallInfoBase()
	return nil, loopRebuild, nil
}

// processExitReturn handles a return instruction exit from the ASM dispatch loop.
//
// The switch names the exit reasons that need work here. Every other isa.OpResult means
// "keep running the loop", which is what falling out of the switch does; that path is the
// overwhelming majority of dispatches.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitReturn(
	frame *CallFrame,
	registers *Registers,
) (any, dispatchAction, error) {
	tier2Instruction := frame.Function.Body[frame.ProgramCounter]
	frame.ProgramCounter++
	vm.ReturnGoDispatches++
	syntheticReturn := isa.Instruction{Op: isa.OpNop, A: tier2Instruction.C, B: 0, C: 0}
	switch handleReturn(vm, frame, registers, syntheticReturn) {
	case opDone:
		result := vm.evalResult
		vm.evalResult = nil
		return result, loopReturn, nil
	case opPanicError:
		err := vm.evalError
		vm.evalError = nil
		return nil, loopReturn, err
	default:
	}
	vm.updateASMCallInfoBase()
	return nil, loopRebuild, nil
}

// processExitReturnVoid handles a void return exit from the ASM dispatch loop.
//
// The switch names the exit reasons that need work here. Every other isa.OpResult means
// "keep running the loop", which is what falling out of the switch does; that path is the
// overwhelming majority of dispatches.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitReturnVoid(
	frame *CallFrame,
	registers *Registers,
) (any, dispatchAction, error) {
	frame.ProgramCounter++
	switch handleReturnVoid(vm, frame, registers, isa.Instruction{Op: isa.OpNop, A: 0, B: 0, C: 0}) {
	case opDone:
		return nil, loopReturn, nil
	case opPanicError:
		err := vm.evalError
		vm.evalError = nil
		return nil, loopReturn, err
	default:
	}
	vm.updateASMCallInfoBase()
	return nil, loopRebuild, nil
}

// processExitTailCall handles a tail call exit from the ASM dispatch loop.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
func (vm *VM) processExitTailCall(frame *CallFrame, registers *Registers) {
	instruction := frame.Function.Body[frame.ProgramCounter]
	frame.ProgramCounter++
	handleTailCall(vm, frame, registers, instruction)
	vm.updateASMCallInfoBase()
}

// processExitPathB handles a tier-2 opcode exit by dispatching the exiting op through the
// Go handler table and then continuing in runPathBBatch.
//
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
// Takes ctx (*dispatchContext) which provides the dispatch context.
//
// Returns the result, the dispatch action, and any error.
func (vm *VM) processExitPathB(
	frame *CallFrame,
	registers *Registers,
	ctx *dispatchContext,
) (any, dispatchAction, error) {
	body := frame.Function.Body
	instruction := body[frame.ProgramCounter]
	frame.ProgramCounter++
	rc := flatDispatchSwitch(vm, frame, registers, instruction)
	return vm.runPathBBatch(frame, registers, ctx, int(ctx.codeLength), body, rc)
}

// rebuildDispatchPointers updates the ASM dispatch context pointers after a frame change
// (call, return, tier-2 handler, etc.).
//
// Takes ctx (*dispatchContext) which provides the dispatch context to update.
// Takes frame (*CallFrame) which specifies the current call frame.
// Takes registers (*Registers) which provides the register file.
func (vm *VM) rebuildDispatchPointers(
	ctx *dispatchContext,
	frame *CallFrame,
	registers *Registers,
) {
	function := frame.Function

	function.EnsurePrecomputedAllocCounts()
	body := function.Body

	var codeBase uintptr
	if len(body) > 0 {
		codeBase = uintptr(unsafe.Pointer(&body[0]))
	}
	sameFunction := codeBase != 0 && ctx.codeBase == codeBase
	ctx.codeBase = codeBase
	ctx.codeLength = int64(len(body))
	ctx.programCounter = int64(frame.ProgramCounter)
	rebuildRegisterBaseScalars(ctx, registers, function.NonZeroBankMask)
	rebuildRegisterBaseSlices(ctx, registers, function.NonZeroBankMask)
	vm.repairRegisterBasesFromCallers(ctx, function.NonZeroBankMask)

	if !sameFunction {
		rebuildConstantBases(ctx, function)
		rebuildLayoutAndTypeTableBases(ctx, function)
	}
	vm.refreshCallContext(ctx)
	vm.saveCurrentDispatchRegisters(ctx)
}

// repairRegisterBasesFromCallers restores bank base pointers that the current frame did
// not refresh by walking up the call stack to find the nearest ancestor frame whose bank
// is populated. Prevents ctx from pointing at a deallocated callee bank after a return
// lands in a frame whose nonZeroBankMask has the bit clear.
//
// Takes ctx (*dispatchContext) which holds the base pointers being repaired.
// Takes currentMask (uint16) which is the current frame's nonZeroBankMask; bits already
// set here are already correct.
func (vm *VM) repairRegisterBasesFromCallers(ctx *dispatchContext, currentMask uint16) {
	if !vm.usesTypedSliceBanks {
		return
	}
	missing := typedSliceBankMask & ^currentMask
	if missing == 0 {
		return
	}
	for fp := vm.FramePointer - 1; fp >= 0 && missing != 0; fp-- {
		ancestor := &vm.CallStack[fp]
		if ancestor.Function == nil {
			continue
		}
		bits := ancestor.Function.NonZeroBankMask & missing
		if bits == 0 {
			continue
		}
		refreshCtxBasesFromAncestor(ctx, &ancestor.Registers, bits)
		missing &^= bits
	}
}

// refreshCtxBasesFromAncestor updates ctx's bank-base pointers to the banks present in
// ancestorRegs whose bit is set in bits. The seven per-bank refresh checks (slicesInt,
// slicesFloat, slicesString, slicesBool, slicesUint, slicesByte, complex) are factored
// out of repairRegisterBasesFromCallers so the parent's cognitive complexity stays inside
// the linter limit.
//
// Each `len(regs.X) > 0` guard protects against the empty-bank case where indexing
// &regs.X[0] would panic; ancestor frames with the nonZeroBankMask bit set should always
// have a non-empty bank, but the guard makes the helper safe to call from non-strict
// callers.
//
// Takes ctx (*dispatchContext) whose base fields are refreshed.
// Takes ancestorRegs (*Registers) which holds the ancestor frame's banks.
// Takes bits (uint16) which is the subset of bank masks to refresh.
func refreshCtxBasesFromAncestor(ctx *dispatchContext, ancestorRegs *Registers, bits uint16) {
	if bits&allocMaskSliceInt != 0 && len(ancestorRegs.SlicesInt) > 0 {
		ctx.slicesIntBase = uintptr(unsafe.Pointer(&ancestorRegs.SlicesInt[0]))
	}
	if bits&allocMaskSliceFloat != 0 && len(ancestorRegs.slicesFloat) > 0 {
		ctx.slicesFloatBase = uintptr(unsafe.Pointer(&ancestorRegs.slicesFloat[0]))
	}
	if bits&allocMaskSliceString != 0 && len(ancestorRegs.slicesString) > 0 {
		ctx.slicesStringBase = uintptr(unsafe.Pointer(&ancestorRegs.slicesString[0]))
	}
	if bits&allocMaskSliceBool != 0 && len(ancestorRegs.slicesBool) > 0 {
		ctx.slicesBoolBase = uintptr(unsafe.Pointer(&ancestorRegs.slicesBool[0]))
	}
	if bits&allocMaskSliceUint != 0 && len(ancestorRegs.slicesUint) > 0 {
		ctx.slicesUintBase = uintptr(unsafe.Pointer(&ancestorRegs.slicesUint[0]))
	}
	if bits&allocMaskSliceByte != 0 && len(ancestorRegs.slicesByte) > 0 {
		ctx.slicesByteBase = uintptr(unsafe.Pointer(&ancestorRegs.slicesByte[0]))
	}
	if bits&allocMaskComplex != 0 && len(ancestorRegs.Complex) > 0 {
		ctx.complexBase = uintptr(unsafe.Pointer(&ancestorRegs.Complex[0]))
	}
}

// rebuildRegisterBaseScalars refreshes the scalar bank base pointers
// (int/float/string/uint/bool) on ctx for the active frame.
//
// Banks whose bit is clear in bankMask are left at their existing value so inline-return
// ASM that does not refresh every base never observes a stale-NULL.
//
// Takes ctx (*dispatchContext) which is the dispatch context whose base fields are
// refreshed.
// Takes registers (*Registers) which are the active frame's register banks.
// Takes bankMask (uint16) which is the bitmap of banks that must be refreshed for this
// frame.
func rebuildRegisterBaseScalars(ctx *dispatchContext, registers *Registers, bankMask uint16) {
	if bankMask&allocMaskInt != 0 {
		ctx.intsBase = uintptr(unsafe.Pointer(&registers.Ints[0]))
	}
	if bankMask&allocMaskFloat != 0 {
		ctx.floatsBase = uintptr(unsafe.Pointer(&registers.Floats[0]))
	}
	if bankMask&allocMaskString != 0 {
		ctx.stringsBase = uintptr(unsafe.Pointer(&registers.Strings[0]))
	}
	if bankMask&allocMaskUint != 0 {
		ctx.uintsBase = uintptr(unsafe.Pointer(&registers.Uints[0]))
	}
	if bankMask&allocMaskGeneral != 0 {
		ctx.generalsBase = uintptr(unsafe.Pointer(&registers.General[0]))
	}
	if bankMask&allocMaskBool != 0 {
		ctx.boolsBase = uintptr(unsafe.Pointer(&registers.Bools[0]))
	}
}

// rebuildRegisterBaseSlices refreshes typed-slice and complex bank base pointers on ctx
// for the active frame.
//
// Banks whose bit is clear in bankMask are intentionally not zeroed because the
// inline-return ASM does not refresh them; leaving the caller's still-valid base in place
// is correct because the Compiler only emits typed-slice ASM ops when the current frame
// has matching slots.
//
// Takes ctx (*dispatchContext) which is the dispatch context whose base fields are
// refreshed.
// Takes registers (*Registers) which are the active frame's register banks.
// Takes bankMask (uint16) which is the bitmap of banks that must be refreshed for this
// frame.
func rebuildRegisterBaseSlices(ctx *dispatchContext, registers *Registers, bankMask uint16) {
	if bankMask&allocMaskSliceInt != 0 {
		ctx.slicesIntBase = uintptr(unsafe.Pointer(&registers.SlicesInt[0]))
	}
	if bankMask&allocMaskSliceFloat != 0 {
		ctx.slicesFloatBase = uintptr(unsafe.Pointer(&registers.slicesFloat[0]))
	}
	if bankMask&allocMaskSliceString != 0 {
		ctx.slicesStringBase = uintptr(unsafe.Pointer(&registers.slicesString[0]))
	}
	if bankMask&allocMaskSliceBool != 0 {
		ctx.slicesBoolBase = uintptr(unsafe.Pointer(&registers.slicesBool[0]))
	}
	if bankMask&allocMaskSliceUint != 0 {
		ctx.slicesUintBase = uintptr(unsafe.Pointer(&registers.slicesUint[0]))
	}
	if bankMask&allocMaskSliceByte != 0 {
		ctx.slicesByteBase = uintptr(unsafe.Pointer(&registers.slicesByte[0]))
	}
	if bankMask&allocMaskComplex != 0 {
		ctx.complexBase = uintptr(unsafe.Pointer(&registers.Complex[0]))
	}
}

// rebuildConstantBases refreshes the per-function constant-pool base pointers
// (int/float/string/bool).
//
// String and bool pools also reset their length fields and explicitly zero the base when
// empty so a cross-frame stale pointer never leaks into ASM.
//
// Takes ctx (*dispatchContext) which is the dispatch context whose constant-pool fields
// are refreshed.
// Takes function (*CompiledFunction) which is the active compiled function whose
// constants supply the bases.
func rebuildConstantBases(ctx *dispatchContext, function *program.CompiledFunction) {
	constMask := function.NonEmptyConstantMask
	if constMask&program.ConstMaskInt != 0 {
		ctx.intConstantsBase = uintptr(unsafe.Pointer(&function.IntConstants[0]))
	}
	if constMask&program.ConstMaskFloat != 0 {
		ctx.floatConstantsBase = uintptr(unsafe.Pointer(&function.FloatConstants[0]))
	}
	if constMask&program.ConstMaskString != 0 {
		ctx.stringConstantsBase = uintptr(unsafe.Pointer(&function.StringConstants[0]))
	} else {
		ctx.stringConstantsBase = 0
	}
	ctx.stringConstantsLength = int64(len(function.StringConstants))
	if constMask&program.ConstMaskBool != 0 {
		ctx.boolConstantsBase = uintptr(unsafe.Pointer(&function.BoolConstants[0]))
	} else {
		ctx.boolConstantsBase = 0
	}
	ctx.boolConstantsLength = int64(len(function.BoolConstants))
	if constMask&program.ConstMaskUint != 0 {
		ctx.uintConstantsBase = uintptr(unsafe.Pointer(&function.UintConstants[0]))
	} else {
		ctx.uintConstantsBase = 0
	}
	ctx.uintConstantsLength = int64(len(function.UintConstants))
}

// rebuildLayoutAndTypeTableBases refreshes the structLayoutTable and typeTable base
// pointers on ctx, explicitly zeroing them on empty tables so stale cross-frame leaks
// cannot reach ASM tier-1 struct-field handlers.
//
// Takes ctx (*dispatchContext) which is the dispatch context whose table base fields are
// refreshed.
// Takes function (*CompiledFunction) which is the active compiled function whose tables
// supply the bases.
func rebuildLayoutAndTypeTableBases(ctx *dispatchContext, function *program.CompiledFunction) {
	constMask := function.NonEmptyConstantMask
	if constMask&program.ConstMaskStructLayoutTable != 0 {
		ctx.structLayoutTableBase = uintptr(unsafe.Pointer(&function.StructLayoutTable[0]))
	} else {
		ctx.structLayoutTableBase = 0
	}
	ctx.structLayoutTableLength = int64(len(function.StructLayoutTable))
	if constMask&program.ConstMaskTypeTable != 0 {
		ctx.typeTableBase = uintptr(unsafe.Pointer(&function.TypeTable[0]))
	} else {
		ctx.typeTableBase = 0
	}
	ctx.typeTableLength = int64(len(function.TypeTable))
}
