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
	"pipit.sh/pipit/internal/engine/program"
)

// debugStepMode is the session's single-stepping state.
type debugStepMode uint8

const (
	// debugStepNone means no step is active.
	debugStepNone debugStepMode = iota

	// debugStepIn pauses at the next new source line, entering calls and host callbacks.
	debugStepIn

	// debugStepOver pauses at the next new source line at the same or a shallower depth.
	debugStepOver

	// debugStepOut pauses once the stepping frame has returned.
	debugStepOut

	// debugStepReturnToParent pauses the goroutine's next VM at its next source line, after
	// the VM that was stepping (a callback or bound method) has returned to host code.
	debugStepReturnToParent
)

// debugStepState is the in-flight step: which VM and goroutine it belongs to and where it
// started. A step belongs to one goroutine; other goroutines run free until the stepping
// goroutine pauses again, and a breakpoint on any thread cancels the step.
type debugStepState struct {
	// vm is the VM the step was issued on; nil in debugStepReturnToParent.
	vm *VM

	// file and line are the source position the step started from.
	file string

	// goroutineID is the goroutine the step belongs to.
	goroutineID uint64

	// framePointer is the call depth the step started from.
	framePointer int

	// line is the 1-based source line the step started from.
	line int

	// mode is the active step kind.
	mode debugStepMode
}

// applyActionLocked installs the step state a resume action asks for; the mutex must be
// held.
//
// Takes thread (*DebugThread) which is the thread the action applies to.
// Takes action (program.DebugAction) which is Continue or a step.
func (s *DebugSession) applyActionLocked(thread *DebugThread, action program.DebugAction) {
	mode := debugStepNone
	switch action {
	case program.DebugActionStepIn:
		mode = debugStepIn
	case program.DebugActionStepOver:
		mode = debugStepOver
	case program.DebugActionStepOut:
		mode = debugStepOut
	default:

		mode = debugStepNone
	}
	if mode == debugStepNone {
		s.step = debugStepState{vm: nil, file: "", goroutineID: 0, framePointer: 0, line: 0, mode: debugStepNone}
		return
	}
	file, line := "", 0
	if thread.vm.FramePointer >= 0 {
		frame := &thread.vm.CallStack[thread.vm.FramePointer]
		file, line, _ = debugSourcePosition(frame.Function, thread.pausedPC)
	}
	s.step = debugStepState{
		vm:           thread.vm,
		file:         file,
		goroutineID:  thread.goroutineID,
		framePointer: thread.vm.FramePointer,
		line:         line,
		mode:         mode,
	}
}

// stepShouldPauseLocked decides whether the in-flight step pauses thread at the given
// position; the mutex must be held.
//
// Takes thread (*DebugThread) which reached a safe point.
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes framePointer (int) which is the thread's call depth.
//
// Returns bool which is true when the thread must pause with DebugEventStep.
func (s *DebugSession) stepShouldPauseLocked(thread *DebugThread, function *program.CompiledFunction, pc int, framePointer int) bool {
	step := &s.step
	if step.mode == debugStepNone || step.goroutineID != thread.goroutineID {
		return false
	}
	if step.vm != thread.vm {
		return s.stepAcrossVMLocked(function, pc)
	}
	if step.mode == debugStepOut {
		return framePointer < step.framePointer
	}
	file, line, _ := debugSourcePosition(function, pc)
	if line == 0 {
		return false
	}
	newLine := file != step.file || line != step.line
	switch step.mode {
	case debugStepIn:
		return newLine
	case debugStepOver:
		return framePointer <= step.framePointer && newLine
	default:
		return false
	}
}

// stepAcrossVMLocked applies the step to a VM of the stepping goroutine other than the
// one the step was issued on: a callback the stepped code called into, or the parent a
// stepped callback returned to. The mutex must be held.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
//
// Returns bool which is true when the VM must pause.
func (s *DebugSession) stepAcrossVMLocked(function *program.CompiledFunction, pc int) bool {
	switch s.step.mode {
	case debugStepIn, debugStepReturnToParent:
		_, line, _ := debugSourcePosition(function, pc)
		return line > 0
	default:
		return false
	}
}

// onThreadExitLocked adjusts the step when the VM it was issued on leaves: a callback or
// bound-method VM hands the step back to its goroutine's parent VM, a main or goroutine
// VM ends it. The mutex must be held.
//
// Takes thread (*DebugThread) which is leaving the session.
func (s *DebugSession) onThreadExitLocked(thread *DebugThread) {
	if s.step.mode == debugStepNone || s.step.vm != thread.vm {
		return
	}
	switch thread.kind {
	case DebugThreadCallback, DebugThreadBoundMethod:
		s.step.vm = nil
		s.step.mode = debugStepReturnToParent
	default:
		s.step = debugStepState{vm: nil, file: "", goroutineID: 0, framePointer: 0, line: 0, mode: debugStepNone}
	}
	s.recomputeActiveLocked()
}
