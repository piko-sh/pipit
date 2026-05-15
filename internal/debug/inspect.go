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

package debug

import (
	"context"
	"reflect"
	"time"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

// ThreadState says what a thread is doing.
type ThreadState uint8

const (
	// threadRunning means the thread is executing or inside a native call.
	threadRunning ThreadState = iota + 1

	// ThreadPaused means the thread is stopped in the debugger and can be inspected.
	ThreadPaused

	// threadBlocked means the thread is inside a blocking operation; it parks as soon as
	// that operation returns while the world is stopped.
	threadBlocked
)

// Thread summarises one debugged VM.
type Thread struct {
	// Name is the display name, e.g. "goroutine 2" or "goroutine 1 (callback)".
	Name string

	// ID is the session-assigned thread id; GoroutineID is the interpreted goroutine, shared
	// by a callback with its parent.
	ID uint64

	// GoroutineID is the interpreted goroutine, shared by a callback with its parent.
	GoroutineID uint64

	// Kind says how the VM came to run.
	Kind engine.DebugThreadKind

	// State says what the thread is doing.
	State ThreadState
}

// StackFrame is one frame of a paused thread's call stack.
type StackFrame struct {
	// Function is the runtime name (main.run.func1), or the table name when none was
	// recorded.
	Function string

	// File is the source file, empty when unknown.
	File string

	// Line and Column are 1-based, 0 when unknown.
	Line int

	// Column is the 1-based column, 0 when unknown.
	Column int

	// HasVariables is true when the frame's function carries a debug var table.
	HasVariables bool
}

// ScopeKind selects a group of variables in a frame.
type ScopeKind uint8

const (
	// ScopeLocals is the frame's parameters and local variables.
	ScopeLocals ScopeKind = iota + 1

	// ScopeClosure is the variables the frame's function captured from enclosing functions.
	ScopeClosure

	// ScopeGlobals is the package-level variables of the frame's package.
	ScopeGlobals
)

// Scope describes one variable group of a frame.
type Scope struct {
	// Name is the display name.
	Name string

	// Count is the number of variables in the group.
	Count int

	// Kind selects the group for Variables.
	Kind ScopeKind

	// Expensive marks groups a client should expand only on request.
	Expensive bool
}

// VariableInfo is one variable's name, value, register kind and Go type.
type VariableInfo = engine.VariableInfo

// EvalOptions tunes expression evaluation.
type EvalOptions struct {
	// Timeout bounds the evaluation; zero selects two seconds.
	Timeout time.Duration

	// AllowCalls permits calls to interpreted and host functions; when false any call other
	// than a builtin is refused with fault.ErrCallsNotAllowed.
	AllowCalls bool
}

// EvalResult is an evaluated expression's value and type.
type EvalResult struct {
	// Value is the result, copied out of the interpreter's arena.
	Value any

	// Type is the Go type as source text.
	Type string
}

// Threads lists the live threads ordered by id.
//
// Returns []Thread which is empty when no execution is running.
func (d *Debugger) Threads() []Thread {
	infos := d.session.Threads()
	threads := make([]Thread, 0, len(infos))
	for _, info := range infos {
		threads = append(threads, Thread{Name: threadName(info), ID: info.ID, GoroutineID: info.GoroutineID, Kind: info.Kind, State: threadStateFor(info.State)})
	}
	return threads
}

// StackTrace returns a paused thread's call stack, innermost first.
//
// Takes threadID (uint64) which selects the thread.
//
// Returns []StackFrame which is the stack.
// Returns error which is fault.ErrDebugNotPaused when the world is running or the thread
// is not paused, or fault.ErrDebugUnknownThread.
func (d *Debugger) StackTrace(threadID uint64) ([]StackFrame, error) {
	thread, frames, err := d.pausedFrames(threadID)
	if err != nil {
		return nil, err
	}
	_ = thread
	stack := make([]StackFrame, 0, len(frames))
	for _, frame := range frames {
		stack = append(stack, StackFrame{
			Function:     frame.FunctionName,
			File:         frame.File,
			Line:         frame.Line,
			Column:       frame.Column,
			HasVariables: frame.Function != nil && frame.Function.DebugVarTable != nil,
		})
	}
	return stack, nil
}

// Scopes lists the variable groups of one frame of a paused thread.
//
// Takes threadID (uint64) which selects the thread.
// Takes frameIndex (int) which selects the frame, 0 being innermost.
//
// Returns []Scope which lists locals, the closure scope when the function captured
// anything, and globals when the program's bindings are known.
// Returns error which is as for StackTrace, or fault.ErrDebugFrameOutOfRange.
func (d *Debugger) Scopes(threadID uint64, frameIndex int) ([]Scope, error) {
	thread, frames, err := d.pausedFrames(threadID)
	if err != nil {
		return nil, err
	}
	frame, callFrame, err := selectFrame(thread, frames, frameIndex)
	if err != nil {
		return nil, err
	}
	locals, captured := liveEntries(callFrame, frame.PC)
	scopes := []Scope{{Name: "Locals", Kind: ScopeLocals, Count: len(locals), Expensive: false}}
	if len(captured) > 0 {
		scopes = append(scopes, Scope{Name: "Closure", Kind: ScopeClosure, Count: len(captured), Expensive: false})
	}
	if binding := d.currentBinding(); binding != nil {
		if pkg := binding.packageAt(frame); pkg != nil {
			scopes = append(scopes, Scope{Name: "Globals", Kind: ScopeGlobals, Count: len(pkg.GlobalVariables), Expensive: true})
		}
	}
	return scopes, nil
}

// Variables reads one variable group of one frame of a paused thread.
//
// Takes threadID (uint64) which selects the thread.
// Takes frameIndex (int) which selects the frame.
// Takes scope (ScopeKind) which selects the group.
//
// Returns []VariableInfo which is the group's variables in declaration order.
// Returns error which is as for Scopes, or fault.ErrNoProgramBinding for globals without
// a binding.
func (d *Debugger) Variables(threadID uint64, frameIndex int, scope ScopeKind) ([]VariableInfo, error) {
	thread, frames, err := d.pausedFrames(threadID)
	if err != nil {
		return nil, err
	}
	frame, callFrame, err := selectFrame(thread, frames, frameIndex)
	if err != nil {
		return nil, err
	}
	if scope == ScopeGlobals {
		return d.globalVariables(thread, frame)
	}
	locals, captured := liveEntries(callFrame, frame.PC)
	entries := locals
	if scope == ScopeClosure {
		entries = captured
	}
	variables := make([]VariableInfo, 0, len(entries))
	for _, entry := range entries {
		value := thread.VM().ReadFrameVariable(callFrame, entry)
		var boxed any
		if value.IsValid() {
			boxed = value.Interface()
		}
		variables = append(variables, VariableInfo{Name: entry.Name, Value: boxed, Kind: entry.Location.Kind.String(), Type: d.typeNameFor(frame, entry.Name, value)})
	}
	return variables, nil
}

// SetVariable writes a scalar or string local of a paused frame.
//
// Takes threadID (uint64) which selects the thread.
// Takes frameIndex (int) which selects the frame.
// Takes name (string) which is the variable's source name.
// Takes value (any) which is the new value, converted to the variable's type exactly.
//
// Returns VariableInfo which is the variable after the write.
// Returns error which is as for Scopes, fault.ErrVariableUnavailable when no live
// variable has that name, or fault.ErrVariableNotWritable.
func (d *Debugger) SetVariable(threadID uint64, frameIndex int, name string, value any) (VariableInfo, error) {
	thread, frames, err := d.pausedFrames(threadID)
	if err != nil {
		return VariableInfo{}, err
	}
	frame, callFrame, err := selectFrame(thread, frames, frameIndex)
	if err != nil {
		return VariableInfo{}, err
	}
	entry, ok := lookupEntry(callFrame, frame.PC, name)
	if !ok {
		return VariableInfo{}, fault.ErrVariableUnavailable
	}
	if err := thread.VM().WriteFrameVariable(callFrame, entry, reflect.ValueOf(value)); err != nil {
		return VariableInfo{}, err
	}
	stored := thread.VM().ReadFrameVariable(callFrame, entry)
	var boxed any
	if stored.IsValid() {
		boxed = stored.Interface()
	}
	return VariableInfo{Name: name, Value: boxed, Kind: entry.Location.Kind.String(), Type: d.typeNameFor(frame, name, stored)}, nil
}

// Evaluate evaluates a Go expression in one frame of a paused thread, with the frame's
// locals, captured variables, package globals and imports in scope.
//
// Takes threadID (uint64) which selects the thread.
// Takes frameIndex (int) which selects the frame.
// Takes expression (string) which is the Go expression.
// Takes opts (EvalOptions) which tune the evaluation.
//
// Returns EvalResult which is the value and its type.
// Returns error which is as for Scopes, fault.ErrNoProgramBinding when the program was
// compiled without the information the evaluator needs, or the expression's own error.
func (d *Debugger) Evaluate(ctx context.Context, threadID uint64, frameIndex int, expression string, opts EvalOptions) (EvalResult, error) {
	thread, frames, err := d.pausedFrames(threadID)
	if err != nil {
		return EvalResult{Value: nil, Type: ""}, err
	}
	frame, callFrame, err := selectFrame(thread, frames, frameIndex)
	if err != nil {
		return EvalResult{Value: nil, Type: ""}, err
	}
	binding := d.currentBinding()
	if binding == nil {
		return EvalResult{Value: nil, Type: ""}, fault.ErrNoProgramBinding
	}
	return binding.evaluate(ctx, thread.VM(), frame, callFrame, expression, opts)
}

// pausedFrames resolves a thread and its frames, requiring the world to be stopped.
//
// Takes threadID (uint64) which selects the thread.
//
// Returns *engine.DebugThread, []engine.DebugFrame and error as for StackTrace.
func (d *Debugger) pausedFrames(threadID uint64) (*engine.DebugThread, []engine.DebugFrame, error) {
	if !d.session.Stopped() {
		return nil, nil, fault.ErrDebugNotPaused
	}
	thread, ok := d.session.Thread(threadID)
	if !ok {
		return nil, nil, fault.ErrDebugUnknownThread
	}
	frames := thread.Frames()
	if frames == nil {
		return nil, nil, fault.ErrDebugNotPaused
	}
	return thread, frames, nil
}

// selectFrame maps a frame index to its call stack entry.
//
// Takes thread (*engine.DebugThread) which is the paused thread.
// Takes frames ([]engine.DebugFrame) which is the thread's call stack.
// Takes frameIndex (int) which selects the frame.
//
// Returns engine.DebugFrame which is the frame's snapshot.
// Returns *engine.CallFrame which is the live call frame.
// Returns error which is fault.ErrDebugFrameOutOfRange for an index outside the stack or
// on a pseudo frame.
func selectFrame(thread *engine.DebugThread, frames []engine.DebugFrame, frameIndex int) (engine.DebugFrame, *engine.CallFrame, error) {
	callFrame, ok := thread.CallFrame(frameIndex)
	if !ok {
		return engine.DebugFrame{}, nil, fault.ErrDebugFrameOutOfRange
	}
	return frames[frameIndex], callFrame, nil
}

// liveEntries splits the var-table entries live at pc into locals and captured variables.
//
// Takes callFrame (*engine.CallFrame) which is the live call frame.
// Takes pc (int) which is the program counter.
//
// Returns locals ([]program.DebugVarEntry) which is the frame's own variables.
// Returns captured ([]program.DebugVarEntry) which is the closure's upvalues.
func liveEntries(callFrame *engine.CallFrame, pc int) (locals, captured []program.DebugVarEntry) {
	if callFrame.Function == nil || callFrame.Function.DebugVarTable == nil {
		return nil, nil
	}
	for _, entry := range callFrame.Function.DebugVarTable.LiveVariables(pc) {
		if entry.Location.IsUpvalue {
			captured = append(captured, entry)
			continue
		}
		locals = append(locals, entry)
	}
	return locals, captured
}

// lookupEntry finds the innermost live variable with the given name.
//
// Takes callFrame (*engine.CallFrame) which is the live call frame.
// Takes pc (int) which is the program counter.
// Takes name (string) which is the variable name.
//
// Returns program.DebugVarEntry which is the variable's table entry.
// Returns bool which is false when none is live.
func lookupEntry(callFrame *engine.CallFrame, pc int, name string) (program.DebugVarEntry, bool) {
	locals, captured := liveEntries(callFrame, pc)
	var found program.DebugVarEntry
	ok := false
	for _, entry := range append(locals, captured...) {
		if entry.Name != name {
			continue
		}
		if !ok || entry.StartPC >= found.StartPC {
			found = entry
			ok = true
		}
	}
	return found, ok
}

// threadStateFor maps an engine thread state to the client's.
//
// Takes state (engine.DebugThreadState) which is the engine's.
//
// Returns ThreadState which is the client's.
func threadStateFor(state engine.DebugThreadState) ThreadState {
	switch state {
	case engine.DebugThreadParked:
		return ThreadPaused
	case engine.DebugThreadBlocked:
		return threadBlocked
	default:
		return threadRunning
	}
}
