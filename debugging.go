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

package pipit

import (
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine"
)

// Debugger drives breakpoints, stepping and inspection for every VM of an execution.
type Debugger = debug.Debugger

// DebugEvent is one debugger event: a pause, a thread starting or exiting, or the
// execution ending.
type DebugEvent = debug.Event

// DebugEventKind classifies debugger events.
type DebugEventKind = debug.EventKind

// StopReason says why a thread paused.
type StopReason = debug.StopReason

// Location is a source position with its function.
type Location = debug.Location

// PanicInfo describes the panic a pause stopped at.
type PanicInfo = debug.PanicInfo

// Thread summarises one debugged goroutine.
type Thread = debug.Thread

// ThreadState says what a thread is doing.
type ThreadState = debug.ThreadState

// StackFrame is one frame of a paused call stack.
type StackFrame = debug.StackFrame

// Scope describes one variable group of a frame.
type Scope = debug.Scope

// ScopeKind selects a variable group of a frame.
type ScopeKind = debug.ScopeKind

// VariableInfo describes one variable visible in a paused frame.
type VariableInfo = engine.VariableInfo

// Breakpoint describes a source breakpoint to set.
type Breakpoint = debug.Breakpoint

// FunctionBreakpoint describes a breakpoint on function entry.
type FunctionBreakpoint = debug.FunctionBreakpoint

// BreakpointResult reports how a breakpoint was installed.
type BreakpointResult = debug.BreakpointResult

// ExceptionFilter names a class of runtime events to pause on.
type ExceptionFilter = debug.ExceptionFilter

// EvalOptions tunes debugger expression evaluation.
type EvalOptions = debug.EvalOptions

// EvalResult is an evaluated expression's value and type.
type EvalResult = debug.EvalResult

const (
	// ExceptionFilterPanic pauses where an interpreted panic starts, before deferred
	// functions run.
	ExceptionFilterPanic = debug.ExceptionFilterPanic

	// ScopeLocals selects a frame's parameters and local variables.
	ScopeLocals = debug.ScopeLocals

	// ScopeClosure selects the variables a frame's function captured from enclosing
	// functions.
	ScopeClosure = debug.ScopeClosure

	// ScopeGlobals selects the package-level variables of a frame's package.
	ScopeGlobals = debug.ScopeGlobals

	// DebugEventPaused means a thread stopped and every thread is parked.
	DebugEventPaused = debug.EventPaused

	// DebugEventThreadStarted means an interpreted goroutine or callback began running.
	DebugEventThreadStarted = debug.EventThreadStarted

	// DebugEventThreadExited means a thread finished.
	DebugEventThreadExited = debug.EventThreadExited

	// DebugEventExited means the execution ended; the event's Err carries its error.
	DebugEventExited = debug.EventExited
)
