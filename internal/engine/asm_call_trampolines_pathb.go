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

	"pipit.sh/pipit/internal/isa"
)

// opResultToExitReason maps an OpResult to its ctx.exitReason code.
//
// Takes rc (OpResult) which is the handler's return code.
//
// Returns int64 which is the exit reason code to store in ctx.exitReason.
func opResultToExitReason(rc OpResult) int64 {
	switch rc {
	case opFrameChanged:
		return exitFrameChanged
	case opDone:
		return exitDonePathB
	case opDivByZero:
		return exitDivByZero
	case opStackOverflow:
		return exitStackOverflowPathB
	case opPanicError:
		return exitPanicErrorPathB
	default:
	}
	return exitPanicErrorPathB
}

// pathBTrampoline is the shared body of every per-opcode tier-2 ASM-call trampoline,
// syncing frame.ProgramCounter from ctx, calling the handler, and writing the result and
// cold-path exit metadata back to ctx.
//
// Takes ctx (*dispatchContext) which carries the back-pointer to vm.
// Takes instWord (uint32) which is the encoded bytecode instruction word.
// Takes handler (opcodeHandler) which is the Go handler the trampoline wraps.
//
//go:nosplit
func pathBTrampoline(ctx *dispatchContext, instWord uint32, handler opcodeHandler) {
	vm := ctx.vm
	startFramePointer := int(ctx.framePointer)

	if vm.FramePointer > startFramePointer {
		vm.applyPoppedFrameSnapshots(startFramePointer)
	}
	vm.FramePointer = startFramePointer
	frame := (*CallFrame)(unsafe.Pointer(ctx.currentFrame))
	registers := &frame.Registers
	frame.ProgramCounter = int(ctx.programCounter)
	inst := *(*isa.Instruction)(unsafe.Pointer(&instWord))
	rc := handler(vm, frame, registers, inst)
	if rc == opContinue && vm.FramePointer == startFramePointer {
		ctx.programCounter = int64(frame.ProgramCounter)
		ctx.handlerResult = 0
		return
	}
	if rc == opFrameChanged {
		vm.saveDispatchRegistersForFrame(ctx, startFramePointer)
		frame = &vm.CallStack[vm.FramePointer]
	}
	ctx.programCounter = int64(frame.ProgramCounter)
	ctx.framePointer = int64(vm.FramePointer)
	if fp := vm.FramePointer; fp >= 0 && fp < len(vm.CallStack) {
		ctx.currentFrame = uintptr(unsafe.Pointer(&vm.CallStack[fp]))
	}
	ctx.deferStackLength = int64(len(vm.deferStack))
	ctx.handlerResult = uint8(rc)
	if rc != opContinue {
		ctx.exitReason = opResultToExitReason(rc)
		ctx.exitProgramCounter = ctx.programCounter
	}
}

// pathBTrampolineNarrow is the slim variant of pathBTrampoline for handlers that neither
// read nor write frame.ProgramCounter. It omits the PC sync and hot-path re-check.
//
// Takes ctx (*dispatchContext) which carries the live VM state.
// Takes instWord (uint32) which is the encoded instruction word.
// Takes handler (opcodeHandler) which executes the operation.
//
//go:nosplit
func pathBTrampolineNarrow(ctx *dispatchContext, instWord uint32, handler opcodeHandler) {
	vm := ctx.vm
	startFramePointer := int(ctx.framePointer)
	frame := (*CallFrame)(unsafe.Pointer(ctx.currentFrame))
	registers := &frame.Registers
	inst := *(*isa.Instruction)(unsafe.Pointer(&instWord))

	frame.ProgramCounter = int(ctx.programCounter)
	rc := handler(vm, frame, registers, inst)
	ctx.handlerResult = uint8(rc)
	if rc == opContinue {
		return
	}
	if rc == opFrameChanged {
		vm.saveDispatchRegistersForFrame(ctx, startFramePointer)
		if fp := vm.FramePointer; fp >= 0 && fp < len(vm.CallStack) {
			ctx.programCounter = int64(vm.CallStack[fp].ProgramCounter)
		}
	}
	ctx.framePointer = int64(vm.FramePointer)
	if fp := vm.FramePointer; fp >= 0 && fp < len(vm.CallStack) {
		ctx.currentFrame = uintptr(unsafe.Pointer(&vm.CallStack[fp]))
	}
	ctx.deferStackLength = int64(len(vm.deferStack))
	ctx.exitReason = opResultToExitReason(rc)
	ctx.exitProgramCounter = ctx.programCounter
}
