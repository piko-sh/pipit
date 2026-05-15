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

package fault

import "errors"

var (
	// ErrDebugNotPaused is returned by debugger inspection and resume operations when no VM
	// of the execution is paused, so there is nothing to inspect or resume.
	ErrDebugNotPaused = errors.New("debugger: no thread is paused")

	// ErrDebugNotRunning is returned by a pause request when no execution is running.
	ErrDebugNotRunning = errors.New("debugger: no execution is running")

	// ErrDebugAlreadyPaused is returned by a pause request while the world is already
	// stopped.
	ErrDebugAlreadyPaused = errors.New("debugger: execution is already paused")

	// ErrDebugUnknownThread is returned when a debugger operation names a thread id that no
	// live VM of the execution carries.
	ErrDebugUnknownThread = errors.New("debugger: unknown thread")

	// ErrDebugFrameOutOfRange is returned when a frame index does not address a frame of the
	// paused thread's call stack.
	ErrDebugFrameOutOfRange = errors.New("debugger: frame index out of range")

	// ErrDebugExited is returned by a wait for a pause when the execution ended instead.
	ErrDebugExited = errors.New("debugger: execution exited")

	// ErrNoProgramBinding is returned by expression evaluation when the paused program was
	// compiled without the source and type information the evaluator needs.
	ErrNoProgramBinding = errors.New("debugger: no program binding for expression evaluation")

	// ErrVariableUnavailable is returned when an expression names a variable the debug var
	// table does not track at the paused position.
	ErrVariableUnavailable = errors.New("debugger: variable is not available at this position")

	// ErrVariableNotWritable is returned by a variable write the debugger does not support,
	// such as a typed-slice or heap-promoted local.
	ErrVariableNotWritable = errors.New("debugger: variable cannot be written")

	// ErrConditionNotBool is returned when a breakpoint condition does not evaluate to a
	// boolean.
	ErrConditionNotBool = errors.New("debugger: breakpoint condition is not a boolean expression")

	// ErrCallsNotAllowed is returned when an expression calls a function in a context that
	// forbids side effects, such as a hover evaluation.
	ErrCallsNotAllowed = errors.New("debugger: function calls are not allowed in this expression")
)
