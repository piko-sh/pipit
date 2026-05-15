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

package program

const (
	// DebugEventStep fires when the VM reaches a new source line during single-stepping.
	DebugEventStep DebugEvent = iota

	// DebugEventBreakpoint fires when the VM hits a registered breakpoint.
	DebugEventBreakpoint

	// DebugEventEntry fires when the VM enters a new function.
	DebugEventEntry

	// DebugEventExit fires when the VM is about to exit a function.
	DebugEventExit

	// DebugEventPause fires when a client asked the running program to pause and this VM was
	// the first to reach a safe point.
	DebugEventPause

	// DebugEventPanic fires where an interpreted panic starts, before any deferred function
	// runs and before recover can observe the value.
	DebugEventPanic

	// DebugEventFunctionBreakpoint fires on entry to a function named by a function
	// breakpoint.
	DebugEventFunctionBreakpoint

	// DebugEventThreadPaused fires on a VM that parked because another VM of the same
	// execution stopped the world; it is not the VM the client asked about.
	DebugEventThreadPaused
)

const (
	// DebugActionContinue resumes execution until the next breakpoint or program
	// termination.
	DebugActionContinue DebugAction = iota

	// DebugActionStepIn advances to the next source line, entering function calls.
	DebugActionStepIn

	// DebugActionStepOver advances to the next source line at the same or shallower call
	// depth, skipping into function calls.
	DebugActionStepOver

	// DebugActionStepOut runs until the current function returns, then pauses in the caller.
	DebugActionStepOut

	// DebugActionStop terminates execution immediately.
	DebugActionStop
)

// DebugEvent identifies why the debugger hook was invoked.
type DebugEvent uint8

// String returns the event's lower-case name, as debugger clients report it.
//
// Returns string which is the name, or "unknown" for a value outside the enumeration.
func (event DebugEvent) String() string {
	switch event {
	case DebugEventStep:
		return "step"
	case DebugEventBreakpoint:
		return "breakpoint"
	case DebugEventEntry:
		return "entry"
	case DebugEventExit:
		return "exit"
	case DebugEventPause:
		return "pause"
	case DebugEventPanic:
		return "panic"
	case DebugEventFunctionBreakpoint:
		return "function breakpoint"
	case DebugEventThreadPaused:
		return "thread paused"
	default:
		return "unknown"
	}
}

// DebugAction tells the VM what to do after the debug hook returns.
type DebugAction uint8
