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
	"pipit.sh/pipit/internal/fault"
)

// VariableInfo describes a variable visible at a debug pause point.
type VariableInfo struct {
	// Name is the source-level variable name.
	Name string

	// Value is the variable's current runtime value, copied out of the arena so it stays
	// valid after the VM resumes.
	Value any

	// Kind describes the register bank the variable lives in (e.g. "int", "string").
	Kind string

	// Type is the Go type as source text (e.g. "[]int", "*main.T"), filled in by the debug
	// layer when the program's type information is available; empty otherwise.
	Type string
}

// SetDebugRole records which phase of a service execution this VM runs, so the debugger's
// stop-on-entry fires on the entrypoint rather than on a variable initialiser.
//
// Takes role (DebugRole) which is the phase.
func (vm *VM) SetDebugRole(role DebugRole) {
	vm.debugRole = role
}

// debugStopRequested is the per-instruction debugger check. It costs one nil test and one
// atomic load until a pause condition exists.
//
// Takes frame (*CallFrame) which is the current call frame.
//
// Returns bool which is true when the debugger ended the execution.
func (vm *VM) debugStopRequested(frame *CallFrame) bool {
	session := vm.Limits.Debug
	if session == nil || session.active.Load() == 0 {
		return false
	}
	return vm.debugCheck(frame) == program.DebugActionStop
}

// debugStopError is the error run() returns when the debugger check asked it to stop: the
// execution's cancellation error when a parked VM was woken by cancellation, and
// fault.ErrDebuggerStop when the debugger ended the execution.
//
// Returns error which the dispatch loop returns.
func (vm *VM) debugStopError() error {
	if !vm.debugWokenByCancel {
		return fault.ErrDebuggerStop
	}
	vm.debugWokenByCancel = false
	if vm.ctx.Err() != nil {
		return cancellationError(vm.ctx)
	}
	return fault.ErrExecutionCancelled
}

// debugEnter registers the VM with its session when it enters its outermost dispatch
// loop.
func (vm *VM) debugEnter() {
	session := vm.Limits.Debug
	if session == nil {
		return
	}
	vm.debugRunDepth++
	if vm.debugRunDepth != 1 {
		return
	}
	vm.debugThread = session.register(vm)
}

// debugLeave deregisters the VM when it leaves its outermost dispatch loop.
func (vm *VM) debugLeave() {
	thread := vm.debugThread
	if thread == nil {
		return
	}
	vm.debugRunDepth--
	if vm.debugRunDepth != 0 {
		return
	}
	thread.session.deregister(thread)
	vm.debugThread = nil
}

// debugCheck decides at a safe point whether the VM pauses, and parks it when it does.
//
// Takes frame (*CallFrame) which is the current call frame.
//
// Returns program.DebugAction which is Stop when the debugger ended the execution.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (vm *VM) debugCheck(frame *CallFrame) program.DebugAction {
	thread := vm.debugThread
	if thread == nil {
		return program.DebugActionContinue
	}
	s := thread.session
	pc := frame.ProgramCounter
	s.mu.Lock()
	if s.terminated {
		s.mu.Unlock()
		return program.DebugActionStop
	}
	if s.stopped && thread.parkedAtGen != s.stopGen {
		return thread.parkForWorldLocked(frame.Function, pc, true)
	}
	if s.pauseRequested {
		s.pauseRequested = false
		return thread.pauseHereLocked(vm.debugPauseAt(frame.Function, pc, program.DebugEventPause), true)
	}
	entering := !thread.entered
	thread.entered = true
	if entering && s.stopOnEntry && thread.kind == debugThreadMain && thread.role == DebugRoleEntrypoint {
		s.stopOnEntry = false
		return thread.pauseHereLocked(vm.debugPauseAt(frame.Function, pc, program.DebugEventEntry), true)
	}
	if pc < len(frame.Function.Body) {
		if breakpoint, ok := s.sourceBreakpointLocked(thread, frame.Function, pc, vm.FramePointer); ok {
			return thread.fireBreakpoint(breakpoint, frame.Function, pc, program.DebugEventBreakpoint)
		}
		if breakpoint, ok := s.functionBreakpointLocked(frame.Function, pc); ok {
			return thread.fireBreakpoint(breakpoint, frame.Function, pc, program.DebugEventFunctionBreakpoint)
		}
	}
	if s.stepShouldPauseLocked(thread, frame.Function, pc, vm.FramePointer) {
		return thread.pauseHereLocked(vm.debugPauseAt(frame.Function, pc, program.DebugEventStep), true)
	}
	s.mu.Unlock()
	return program.DebugActionContinue
}

// debugPanicPoint is the safe point at the start of an interpreted panic, before any
// deferred function runs. It pauses when the client asked to pause on panics.
//
// Takes value (any) which is the panic value.
//
// Returns bool which is true when the debugger ended the execution.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (vm *VM) debugPanicPoint(value any) bool {
	thread := vm.debugThread
	session := vm.Limits.Debug
	if thread == nil || session == nil || session.active.Load() == 0 || vm.FramePointer < 0 {
		return false
	}
	session.mu.Lock()
	if session.terminated {
		session.mu.Unlock()
		return true
	}
	if !session.pauseOnPanic {
		session.mu.Unlock()
		return false
	}
	frame := &vm.CallStack[vm.FramePointer]
	pause := vm.debugPauseAt(frame.Function, frameInstructionIndex(frame), program.DebugEventPanic)
	pause.PanicValue = value
	return thread.pauseHereLocked(pause, false) == program.DebugActionStop
}

// sourceBreakpointLocked finds a source breakpoint at the position, applying per-frame
// dedup so a line fires once per entry. The mutex must be held.
//
// Takes thread (*DebugThread) which is the executing thread.
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes framePointer (int) which is the thread's call depth.
//
// Returns *DebugBreakpoint which fired, or nil when none did.
// Returns bool which is false when no breakpoint matched.
func (s *DebugSession) sourceBreakpointLocked(thread *DebugThread, function *program.CompiledFunction, pc int, framePointer int) (*DebugBreakpoint, bool) {
	if function.DebugSourceMap == nil || len(s.breakpoints) == 0 {
		return nil, false
	}
	mark := thread.lineMark(framePointer)
	if pc == 0 {
		*mark = debugLineMark{file: "", line: 0}
	}
	file, line, _ := function.DebugSourceMap.SourcePosition(pc)
	if line == 0 {
		return nil, false
	}
	entered := mark.file != file || mark.line != line
	breakpoint, ok := s.breakpoints[debugBreakpointKey{file: file, line: line}]
	fire := ok && entered
	if fire || !function.DebugSourceMap.Inlined(pc) {
		*mark = debugLineMark{file: file, line: line}
	}
	if !fire {
		return nil, false
	}
	return breakpoint, true
}

// functionBreakpointLocked finds a function breakpoint on entry to function; the mutex
// must be held.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
//
// Returns *DebugBreakpoint which fired, or nil when none did.
// Returns bool which is false when no breakpoint matched.
func (s *DebugSession) functionBreakpointLocked(function *program.CompiledFunction, pc int) (*DebugBreakpoint, bool) {
	if pc != 0 || len(s.functionBreakpoints) == 0 {
		return nil, false
	}
	if breakpoint, ok := s.functionBreakpoints[function.RuntimeName]; ok {
		return breakpoint, true
	}
	breakpoint, ok := s.functionBreakpoints[function.Name]
	return breakpoint, ok
}

// fireBreakpoint evaluates a breakpoint's condition and hit condition and pauses when
// they hold. The mutex must be held on entry; it is released around the condition
// evaluation and not held on return.
//
// Takes breakpoint (*DebugBreakpoint) which fired.
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes event (program.DebugEvent) which is the pause reason.
//
// Returns program.DebugAction which is Stop when the debugger ended the execution.
func (t *DebugThread) fireBreakpoint(breakpoint *DebugBreakpoint, function *program.CompiledFunction, pc int, event program.DebugEvent) program.DebugAction {
	s := t.session
	message := ""
	if breakpoint.Condition != "" {
		evaluator := s.conditionEvaluator
		t.pausedPC = pc
		t.pausedPCExact = true
		s.mu.Unlock()
		holds, err := evaluateBreakpointCondition(evaluator, t, breakpoint)
		s.mu.Lock()
		if err != nil {
			message = err.Error()
		} else if !holds {
			s.mu.Unlock()
			return program.DebugActionContinue
		}
	}
	if message == "" {
		breakpoint.HitCount++
		if !breakpoint.HitCondition.Matches(breakpoint.HitCount) {
			s.mu.Unlock()
			return program.DebugActionContinue
		}
	}
	pause := t.vm.debugPauseAt(function, pc, event)
	pause.BreakpointID = breakpoint.ID
	pause.Message = message
	return t.pauseHereLocked(pause, true)
}

// evaluateBreakpointCondition runs a breakpoint condition through the session's
// evaluator.
//
// Takes evaluator (debugConditionEvaluator) which may be nil.
// Takes thread (*DebugThread) which is the paused thread.
// Takes breakpoint (*DebugBreakpoint) which is the breakpoint to evaluate.
//
// Returns bool which is the condition's value.
// Returns error when the condition could not be evaluated.
func evaluateBreakpointCondition(evaluator debugConditionEvaluator, thread *DebugThread, breakpoint *DebugBreakpoint) (bool, error) {
	if evaluator == nil {
		return false, fault.ErrNoProgramBinding
	}
	return evaluator(thread, breakpoint)
}

// debugPauseAt describes a pause of this VM at an instruction.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes event (program.DebugEvent) which is the reason.
//
// Returns DebugPause with the source position filled in and Thread left nil.
func (vm *VM) debugPauseAt(function *program.CompiledFunction, pc int, event program.DebugEvent) DebugPause {
	file, line, column := debugSourcePosition(function, pc)
	return DebugPause{
		thread:       nil,
		Function:     function,
		PanicValue:   nil,
		File:         file,
		Message:      "",
		pc:           pc,
		Line:         line,
		Column:       column,
		framePointer: vm.FramePointer,
		BreakpointID: 0,
		Event:        event,
	}
}

// debugSourcePosition returns the source position of an instruction, falling back to the
// closest earlier instruction that has one, as the runtime stack formatter does.
//
// Takes function (*program.CompiledFunction) which owns the source map; may be nil.
// Takes pc (int) which is the instruction index, len(Body) for the epilogue.
//
// Returns file (string) which is the source file path, empty when unknown.
// Returns line (int) which is the 1-based line, 0 when unknown.
// Returns column (int) which is the 1-based column, 0 when unknown.
func debugSourcePosition(function *program.CompiledFunction, pc int) (file string, line, column int) {
	if function == nil || function.DebugSourceMap == nil {
		return "", 0, 0
	}
	if pc >= len(function.Body) {
		if file, line := function.DebugSourceMap.EpiloguePosition(); line > 0 {
			return file, line, 0
		}
		pc = len(function.Body) - 1
	}
	for candidate := pc; candidate >= 0; candidate-- {
		file, line, column := function.DebugSourceMap.SourcePosition(candidate)
		if line > 0 {
			return file, line, column
		}
	}
	return "", 0, 0
}

// debugFrames builds the VM's call stack for the debugger, innermost first, with the
// runtime.gopanic pseudo frame and unwound frames spliced in as the runtime stack does.
//
// Takes topPC (int) which is the instruction index to attribute the innermost frame to,
// or -1 to use the frame's last executed instruction.
//
// Returns []DebugFrame which is the stack.
func (vm *VM) debugFrames(topPC int) []DebugFrame {
	frames := make([]DebugFrame, 0, vm.FramePointer+1)
	for index := vm.FramePointer; index >= 0; index-- {
		frame := &vm.CallStack[index]
		if frame.Function == nil {
			continue
		}
		pc := frameInstructionIndex(frame)
		if index == vm.FramePointer && topPC >= 0 {
			pc = topPC
		}
		frames = append(frames, debugFrameFor(frame.Function, pc, index))
		if record, ok := vm.panicDeferRecord(index); ok {
			frames = append(frames, DebugFrame{Function: nil, FunctionName: pseudoFrameGopanic, File: "", PC: 0, Line: 0, Column: 0, callStackIndex: -1})
			for _, unwound := range record.unwound {
				frames = append(frames, debugFrameFor(unwound.function, unwound.pc, -1))
			}
		}
	}
	return frames
}

// debugFrameFor describes one executing instruction of a function.
//
// Takes function (*program.CompiledFunction) which owns the instruction.
// Takes pc (int) which is the instruction index.
// Takes callStackIndex (int) which is the frame's call stack slot, or -1.
//
// Returns DebugFrame which carries the runtime name and source position.
func debugFrameFor(function *program.CompiledFunction, pc int, callStackIndex int) DebugFrame {
	file, line, column := debugSourcePosition(function, pc)
	name := function.RuntimeName
	if name == "" {
		name = function.Name
	}
	return DebugFrame{Function: function, FunctionName: name, File: file, PC: pc, Line: line, Column: column, callStackIndex: callStackIndex}
}
