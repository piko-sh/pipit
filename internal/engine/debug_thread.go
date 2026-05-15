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

const (
	// debugThreadMain is the VM a service execution starts with.
	debugThreadMain DebugThreadKind = iota

	// debugThreadGoroutine is a VM spawned by an interpreted `go` statement.
	debugThreadGoroutine

	// DebugThreadCallback is a VM running an interpreted closure called from host code.
	DebugThreadCallback

	// DebugThreadBoundMethod is a VM running an interpreted method called from host code
	// through a method value or an interface adapter.
	DebugThreadBoundMethod
)

const (
	// DebugRoleEntrypoint runs the program's entrypoint or a compiled expression.
	DebugRoleEntrypoint DebugRole = iota

	// DebugRoleVarInit runs the package-level variable initialisers.
	DebugRoleVarInit

	// DebugRoleInit runs one init function.
	DebugRoleInit

	// DebugRoleEval runs an evaluated expression body.
	DebugRoleEval

	// DebugRoleSession runs a REPL session submission.
	DebugRoleSession
)

const (
	// debugThreadRunning means the VM is executing instructions or inside a native call.
	debugThreadRunning DebugThreadState = iota

	// DebugThreadParked means the VM is stopped in the debugger; its frames and arena are
	// immutable until it is resumed.
	DebugThreadParked

	// DebugThreadBlocked means the VM is parked inside a blocking operation another VM must
	// complete (a channel operation, select, or a blocking native); it parks in the debugger
	// as soon as that operation returns while the world is stopped.
	DebugThreadBlocked
)

// DebugThreadKind says how a VM came to run: as the execution's own VM, as a `go`
// statement's child, as a host-callback VM or as a bound-method child.
type DebugThreadKind uint8

// String returns the kind's lower-case name.
//
// Returns string which is the name, or "unknown" outside the enumeration.
func (kind DebugThreadKind) String() string {
	switch kind {
	case debugThreadMain:
		return "main"
	case debugThreadGoroutine:
		return "goroutine"
	case DebugThreadCallback:
		return "callback"
	case DebugThreadBoundMethod:
		return "bound method"
	default:
		return "unknown"
	}
}

// DebugRole says which phase of a service execution a top-level VM runs, so the debugger
// can tell the entrypoint from the variable initialisers and init functions that precede
// it.
type DebugRole uint8

// DebugThreadState says what a debugged VM's goroutine is doing.
type DebugThreadState uint8

// String returns the state's lower-case name.
//
// Returns string which is the name, or "unknown" outside the enumeration.
func (state DebugThreadState) String() string {
	switch state {
	case debugThreadRunning:
		return "running"
	case DebugThreadParked:
		return "paused"
	case DebugThreadBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// DebugFrame describes one frame of a paused thread's call stack.
type DebugFrame struct {
	// Function is the compiled function executing in the frame; nil for the runtime.gopanic
	// pseudo frame shown between a deferred call and the panic that runs it.
	Function *program.CompiledFunction

	// FunctionName is the function's runtime name (main.run.func1), or its table name when
	// no runtime name was recorded.
	FunctionName string

	// File is the source file of the frame's current instruction, empty when unknown.
	File string

	// PC is the instruction index the frame is at: the instruction about to execute for the
	// paused frame, the call instruction for callers.
	PC int

	// Line and Column are the 1-based source position, 0 when unknown.
	Line int

	// Column is the 1-based source column, 0 when unknown.
	Column int

	// callStackIndex is the frame's index in the VM's call stack, or -1 for pseudo frames
	// and frames a panic has already unwound.
	callStackIndex int
}

// DebugPause records why and where a thread stopped.
type DebugPause struct {
	// thread is the thread that stopped.
	thread *DebugThread

	// Function is the compiled function executing when the thread stopped.
	Function *program.CompiledFunction

	// PanicValue is the value being panicked with for a DebugEventPanic pause, nil
	// otherwise.
	PanicValue any

	// File is the source file at the pause point, empty when unknown.
	File string

	// Message carries a diagnostic the client should show, such as a breakpoint condition
	// that failed to evaluate; empty otherwise.
	Message string

	// pc is the instruction index the thread stopped at.
	pc int

	// Line and Column are the 1-based source position, 0 when unknown.
	Line int

	// Column is the 1-based source column, 0 when unknown.
	Column int

	// framePointer is the paused thread's call stack depth.
	framePointer int

	// BreakpointID identifies the breakpoint that fired, 0 when none did.
	BreakpointID int

	// Event says why the thread stopped.
	Event program.DebugEvent
}

// DebugThreadInfo is the client-facing summary of one debugged VM.
type DebugThreadInfo struct {
	// functionName and file locate a paused or blocked thread; empty for a running one.
	functionName string

	// file is the source file at the thread's current position.
	file string

	// ID is the session-assigned thread id, unique for the life of the session.
	ID uint64

	// GoroutineID is the interpreted goroutine the thread belongs to; a callback shares its
	// parent's.
	GoroutineID uint64

	// line is the paused thread's 1-based source line, 0 when running or unknown.
	line int

	// Kind classifies how the thread's VM came to run.
	Kind DebugThreadKind

	// role is the execution phase the thread's VM runs.
	role DebugRole

	// State is the thread's current scheduling state.
	State DebugThreadState
}

// debugLineMark records the source line a frame last executed at, for breakpoint dedup.
type debugLineMark struct {
	// file is the source file of the last executed line.
	file string

	// line is the 1-based source line number.
	line int
}

// DebugThread is one debugged VM's registration with its DebugSession. It exists while
// its VM is inside at least one dispatch loop.
//
//exhaustruct:ignore
type DebugThread struct {
	// vm is the VM this thread tracks.
	vm *VM

	// session is the debug session the thread belongs to.
	session *DebugSession

	// wake is poked by Resume so a parked thread re-checks the world state.
	wake chan struct{}

	// lineMarks records, per call depth, the line the frame at that depth was last seen on;
	// reset when a new frame starts at that depth.
	lineMarks []debugLineMark

	// id is the session-assigned thread identifier.
	id uint64

	// goroutineID is the interpreted goroutine this thread runs under.
	goroutineID uint64

	// parkedAtGen is the stop generation this thread has parked for; a thread parks once per
	// generation.
	parkedAtGen uint64

	// pausedPC is the instruction index the thread paused at; exact when the pause happened
	// at the dispatch loop's safe point, otherwise the frame's last executed instruction.
	pausedPC int

	// pausedPCExact is true when pausedPC is the instruction about to execute.
	pausedPCExact bool

	// entered is set once the thread has passed its first safe point, for stop-on-entry.
	entered bool

	// kind says how the thread's VM came to run.
	kind DebugThreadKind

	// role is the execution phase the thread's VM runs.
	role DebugRole

	// state is what the thread's goroutine is doing.
	state DebugThreadState
}

// ID returns the session-assigned thread id.
//
// Returns uint64 which is the id.
func (t *DebugThread) ID() uint64 { return t.id }

// GoroutineID returns the interpreted goroutine id the thread runs under.
//
// Returns uint64 which is the goroutine id.
func (t *DebugThread) GoroutineID() uint64 { return t.goroutineID }

// Kind returns how the thread's VM came to run.
//
// Returns DebugThreadKind which is the kind.
func (t *DebugThread) Kind() DebugThreadKind { return t.kind }

// Role returns the execution phase the thread's VM runs.
//
// Returns DebugRole which is the role.
func (t *DebugThread) Role() DebugRole { return t.role }

// State returns what the thread's goroutine is doing.
//
// Returns DebugThreadState which is the state at the time of the call.
func (t *DebugThread) State() DebugThreadState {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	return t.state
}

// VM returns the thread's VM, for inspection while the thread is paused.
//
// Returns *VM which is the VM.
func (t *DebugThread) VM() *VM { return t.vm }

// Info summarises the thread for a client.
//
// Returns DebugThreadInfo which carries the position when the thread is paused or
// blocked.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (t *DebugThread) Info() DebugThreadInfo {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	return t.infoLocked()
}

// Frames returns the thread's call stack, innermost first, while the thread is paused or
// blocked. A running thread's stack is changing and is reported as empty.
//
// Returns []DebugFrame which is the stack, nil when the thread is running.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (t *DebugThread) Frames() []DebugFrame {
	t.session.mu.Lock()
	defer t.session.mu.Unlock()
	if t.state == debugThreadRunning {
		return nil
	}
	return t.vm.debugFrames(t.topFramePCLocked())
}

// CallFrame maps a frame index from Frames to the VM's call stack entry.
//
// Takes frameIndex (int) which counts from the innermost frame, 0 being the paused one.
//
// Returns *CallFrame which is the frame, or nil when not found.
// Returns bool which is false when the index is out of range, addresses a pseudo frame,
// or the thread is running.
func (t *DebugThread) CallFrame(frameIndex int) (*CallFrame, bool) {
	frames := t.Frames()
	if frameIndex < 0 || frameIndex >= len(frames) || frames[frameIndex].callStackIndex < 0 {
		return nil, false
	}
	return &t.vm.CallStack[frames[frameIndex].callStackIndex], true
}

// EvaluationFrame returns the innermost frame of a thread that is about to decide whether
// a breakpoint pauses it. It is valid only on the thread's own goroutine, from the
// session's condition evaluator, where the frame is stable without the world being
// stopped.
//
// Returns DebugFrame which describes the frame.
// Returns *CallFrame which is its call stack entry.
// Returns bool which is false when the thread has no frame.
func (t *DebugThread) EvaluationFrame() (DebugFrame, *CallFrame, bool) {
	vm := t.vm
	if vm.FramePointer < 0 {
		return DebugFrame{}, nil, false
	}
	frames := vm.debugFrames(t.pausedPC)
	if len(frames) == 0 || frames[0].callStackIndex < 0 {
		return DebugFrame{}, nil, false
	}
	return frames[0], &vm.CallStack[frames[0].callStackIndex], true
}

// lineMark returns the frame's line mark at a call depth, growing the table as needed.
//
// Takes framePointer (int) which is the call depth.
//
// Returns *debugLineMark which the caller updates in place.
func (t *DebugThread) lineMark(framePointer int) *debugLineMark {
	for len(t.lineMarks) <= framePointer {
		t.lineMarks = append(t.lineMarks, debugLineMark{file: "", line: 0})
	}
	return &t.lineMarks[framePointer]
}

// infoLocked builds the thread summary; the session mutex must be held.
//
// Returns DebugThreadInfo which is the summary.
func (t *DebugThread) infoLocked() DebugThreadInfo {
	info := DebugThreadInfo{
		functionName: "",
		file:         "",
		ID:           t.id,
		GoroutineID:  t.goroutineID,
		line:         0,
		Kind:         t.kind,
		role:         t.role,
		State:        t.state,
	}
	if t.state == debugThreadRunning || t.vm.FramePointer < 0 {
		return info
	}
	frames := t.vm.debugFrames(t.topFramePCLocked())
	if len(frames) == 0 {
		return info
	}
	info.functionName = frames[0].FunctionName
	info.file = frames[0].File
	info.line = frames[0].Line
	return info
}

// topFramePCLocked returns the instruction index to attribute the innermost frame to.
//
// Returns int which is the exact paused pc, or -1 to use the frame's last executed
// instruction.
func (t *DebugThread) topFramePCLocked() int {
	if t.pausedPCExact {
		return t.pausedPC
	}
	return -1
}
