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
	"fmt"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

const (
	// EventPaused means a thread stopped and the world is parked.
	EventPaused EventKind = iota + 1

	// EventThreadStarted means an interpreted goroutine or callback VM began running.
	EventThreadStarted

	// EventThreadExited means a thread finished.
	EventThreadExited

	// EventExited means the execution ended; Err carries its error, nil on success.
	EventExited
)

const (
	// stopReasonNone is the zero value for events that are not pauses.
	stopReasonNone StopReason = iota

	// StopReasonEntry is the stop-on-entry pause at the entrypoint's first instruction.
	StopReasonEntry

	// StopReasonBreakpoint is a source breakpoint.
	StopReasonBreakpoint

	// StopReasonStep is the end of a step.
	StopReasonStep

	// StopReasonPause is a client pause request.
	StopReasonPause

	// StopReasonPanic is an interpreted panic, paused before deferred functions run.
	StopReasonPanic

	// StopReasonFunctionBreakpoint is a breakpoint on function entry.
	StopReasonFunctionBreakpoint

	// StopReasonOtherThread means this thread parked because another thread stopped.
	StopReasonOtherThread
)

// EventKind classifies debugger events.
type EventKind uint8

// String returns the kind's lower-case name.
//
// Returns string which is the name.
func (kind EventKind) String() string {
	switch kind {
	case EventPaused:
		return "paused"
	case EventThreadStarted:
		return "thread started"
	case EventThreadExited:
		return "thread exited"
	case EventExited:
		return "exited"
	default:
		return "unknown"
	}
}

// StopReason says why a thread paused.
type StopReason uint8

// String returns the reason as debugger clients spell it.
//
// Returns string which is the reason.
func (reason StopReason) String() string {
	switch reason {
	case StopReasonEntry:
		return "entry"
	case StopReasonBreakpoint:
		return "breakpoint"
	case StopReasonStep:
		return "step"
	case StopReasonPanic:
		return "exception"
	case StopReasonFunctionBreakpoint:
		return "function breakpoint"
	case StopReasonOtherThread:
		return "paused by another thread"
	case stopReasonNone:
		return ""
	default:

		return "pause"
	}
}

// Location is a source position with the function it belongs to.
type Location struct {
	// Function is the function's runtime name (main.run.func1), or its table name when no
	// runtime name was recorded.
	Function string

	// File is the source file, empty when unknown.
	File string

	// Line and Column are 1-based, 0 when unknown.
	Line int

	// Column is the 1-based column, 0 when unknown.
	Column int
}

// PanicInfo describes the panic a StopReasonPanic pause stopped at.
type PanicInfo struct {
	// Value is the panic value.
	Value any

	// Text is the value formatted as Go prints it.
	Text string

	// Type is the value's Go type.
	Type string
}

// Event is one debugger event.
type Event struct {
	// Err is the execution error for EventExited, nil on success.
	Err error

	// Panic is set for EventPaused with StopReasonPanic.
	Panic *PanicInfo

	// HitBreakpointIDs lists the breakpoints that fired for a breakpoint pause.
	HitBreakpointIDs []int

	// ThreadName is the thread's display name for thread events.
	ThreadName string

	// Message carries a diagnostic to show the user, such as a breakpoint condition that
	// failed to evaluate.
	Message string

	// Location is the stopping thread's innermost frame for EventPaused.
	Location Location

	// ThreadID and GoroutineID identify the thread the event concerns.
	ThreadID uint64

	// GoroutineID is the interpreted goroutine the event concerns.
	GoroutineID uint64

	// Kind says what happened.
	Kind EventKind

	// Reason says why a thread paused, stopReasonNone for other kinds.
	Reason StopReason
}

// stopReasonFor maps an engine pause event to a client stop reason.
//
// Takes event (program.DebugEvent) which is the engine's reason.
//
// Returns StopReason which is the client's.
func stopReasonFor(event program.DebugEvent) StopReason {
	switch event {
	case program.DebugEventBreakpoint:
		return StopReasonBreakpoint
	case program.DebugEventStep, program.DebugEventExit:
		return StopReasonStep
	case program.DebugEventEntry:
		return StopReasonEntry
	case program.DebugEventPanic:
		return StopReasonPanic
	case program.DebugEventFunctionBreakpoint:
		return StopReasonFunctionBreakpoint
	case program.DebugEventThreadPaused:
		return StopReasonOtherThread
	default:

		return StopReasonPause
	}
}

// threadName renders a thread's display name.
//
// Takes info (engine.DebugThreadInfo) which describes the thread.
//
// Returns string which is the name, e.g. "goroutine 1" or "goroutine 1 (callback)".
func threadName(info engine.DebugThreadInfo) string {
	name := fmt.Sprintf("goroutine %d", info.GoroutineID)
	switch info.Kind {
	case engine.DebugThreadCallback:
		return name + " (callback)"
	case engine.DebugThreadBoundMethod:
		return name + " (method)"
	default:
		return name
	}
}

// eventFromSession converts an engine session event into a client event.
//
// Takes sessionEvent (engine.DebugSessionEvent) which is the engine's record.
//
// Returns Event which is the client's view of it.
func eventFromSession(sessionEvent engine.DebugSessionEvent) Event {
	info := sessionEvent.Thread
	event := Event{
		Err:              nil,
		Panic:            nil,
		HitBreakpointIDs: nil,
		ThreadName:       threadName(info),
		Message:          "",
		Location:         Location{Function: "", File: "", Line: 0, Column: 0},
		ThreadID:         info.ID,
		GoroutineID:      info.GoroutineID,
		Kind:             0,
		Reason:           stopReasonNone,
	}
	switch sessionEvent.Kind {
	case engine.DebugSessionEventThreadStarted:
		event.Kind = EventThreadStarted
	case engine.DebugSessionEventThreadExited:
		event.Kind = EventThreadExited
	case engine.DebugSessionEventPaused:
		event.Kind = EventPaused
		fillPause(&event, sessionEvent.Pause)
	default:
		event.Kind = EventPaused
	}
	return event
}

// fillPause copies a pause record's details into a client event.
//
// Takes event (*Event) which receives the details.
// Takes pause (*engine.DebugPause) which is the engine's record; nil leaves event as is.
func fillPause(event *Event, pause *engine.DebugPause) {
	if pause == nil {
		return
	}
	event.Reason = stopReasonFor(pause.Event)
	event.Message = pause.Message
	event.Location = Location{Function: functionDisplayName(pause.Function), File: pause.File, Line: pause.Line, Column: pause.Column}
	if pause.BreakpointID != 0 {
		event.HitBreakpointIDs = []int{pause.BreakpointID}
	}
	if pause.Event == program.DebugEventPanic {
		event.Panic = &PanicInfo{Value: pause.PanicValue, Text: fmt.Sprint(pause.PanicValue), Type: fmt.Sprintf("%T", pause.PanicValue)}
	}
}

// functionDisplayName returns a function's runtime name, falling back to its table name.
//
// Takes function (*program.CompiledFunction) which may be nil.
//
// Returns string which is the name, empty for nil.
func functionDisplayName(function *program.CompiledFunction) string {
	if function == nil {
		return ""
	}
	if function.RuntimeName != "" {
		return function.RuntimeName
	}
	return function.Name
}
