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

// deadlockError carries the Go runtime's deadlock message verbatim. It is a type rather
// than an errors.New value because the runtime's text ends in punctuation, which the
// sentinel constructors are linted against.
type deadlockError struct{}

var (
	// ErrDivisionByZero is returned when an integer division or remainder operation has a
	// zero divisor.
	ErrDivisionByZero = errors.New("division by zero")

	// ErrStackOverflow is returned when the call stack exceeds the maximum depth, indicating
	// infinite recursion or excessively deep call chains.
	ErrStackOverflow = errors.New("stack overflow")

	// ErrIndexOutOfRange is returned when a slice, array, or string index is outside the
	// valid range [0, len).
	ErrIndexOutOfRange = errors.New("index out of range")

	// ErrSliceOutOfRange is returned when slice bounds are outside the valid range or low
	// exceeds high.
	ErrSliceOutOfRange = errors.New("slice bounds out of range")

	// ErrAllocationLimit is returned when a single allocation (make slice, make chan,
	// unsafe.String, unsafe.Slice) exceeds the configured maximum size.
	ErrAllocationLimit = errors.New("allocation size limit exceeded")

	// ErrInterpreterInvariant is returned when an opcode handler finds state the compiler
	// promised could not occur. It is an interpreter defect, never a program error, so the
	// CLI reports it as an internal error rather than a panic.
	ErrInterpreterInvariant = errors.New("interpreter invariant violated")

	// ErrUncaughtPanic identifies the error an execution returns when a panic unwound every
	// frame without being recovered. Callers use this sentinel to distinguish a program
	// panic from other failures.
	ErrUncaughtPanic = errors.New("uncaught panic")

	// ErrDeadlock is returned when every goroutine of an execution is parked on an operation
	// only another goroutine of that execution could complete. Its text is the Go runtime's
	// own fatal message, exclamation mark included, so a script sees the failure it would
	// see natively.
	ErrDeadlock error = deadlockError{}

	// ErrGoroutineLimit is returned when the number of goroutines spawned by interpreted
	// code exceeds the configured limit.
	ErrGoroutineLimit = errors.New("goroutine limit exceeded")

	// ErrOutputLimit is returned when print/println output exceeds the configured maximum
	// size.
	ErrOutputLimit = errors.New("output size limit exceeded")

	// ErrCostBudgetExceeded is returned when the runtime cost of executing code exceeds the
	// budget set via WithCostBudget.
	ErrCostBudgetExceeded = errors.New("cost budget exceeded")

	// ErrGoroutineJoinTimeout is returned when goroutines spawned by an execution are still
	// running after the join grace period following the main function's return.
	ErrGoroutineJoinTimeout = errors.New("goroutines still running after main returned")

	// ErrUnsupportedInterfaceArgument is returned when an interpreted value is passed to a
	// native parameter of an interface type that no pipit adapter can satisfy, instead of
	// letting reflect.Call panic on the unassignable value.
	ErrUnsupportedInterfaceArgument = errors.New("unsupported interface argument")

	// ErrUnsupportedInterfaceField is returned when an interpreted value is stored into a
	// native struct field of an interface type that no pipit adapter can satisfy.
	ErrUnsupportedInterfaceField = errors.New("unsupported interface field value")

	// ErrTypeMismatch is returned when a value's runtime type disagrees with the type the
	// compiler chose an instruction for, such as a struct field read into a typed-slice bank
	// whose element width differs from the field's.
	ErrTypeMismatch = errors.New("type mismatch")

	// ErrGoexit signals that interpreted code invoked runtime.Goexit.
	//
	// The interpreter unwinds the VM's frame stack running defers, then surfaces this
	// sentinel. The real runtime.Goexit is never invoked.
	ErrGoexit = errors.New("runtime.Goexit")

	// ErrArenaBudgetExceeded is returned when a register-arena grow would push the
	// cumulative bytes allocated within a single Execute past the configured per-execution
	// budget.
	ErrArenaBudgetExceeded = errors.New("register arena byte budget exceeded")

	// ErrDebuggerStop is returned when the debugger requests execution to halt via
	// DebugActionStop.
	ErrDebuggerStop = errors.New("debugger: execution stopped")
)

// Error returns the Go runtime's own deadlock message.
//
// Returns string which is the verbatim fatal error text.
func (deadlockError) Error() string {
	return "fatal error: all goroutines are asleep - deadlock!"
}
