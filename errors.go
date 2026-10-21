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
	"errors"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/sdk/module"
)

var (
	// ErrCapabilityDenied is the sentinel DenyCapabilityHook returns for every gate. Hosts
	// building a hook on DenyCapabilityHook match it with errors.Is to recognise a policy
	// denial.
	ErrCapabilityDenied = policy.ErrCapabilityDenied

	// ErrInvalidConfig reports an invalid configuration. Tier variants such as
	// ErrInvalidIsolatedConfig, ErrInvalidRestrictedConfig and ErrInvalidOption wrap it.
	ErrInvalidConfig = policy.ErrInvalidConfig

	// ErrInvalidOption is returned by every compile and evaluation of an interpreter built
	// with an option given a value it does not accept, such as a limit that is not positive.
	// It wraps ErrInvalidConfig.
	ErrInvalidOption = app.ErrInvalidOption

	// ErrUnavailable reports a capability the current platform cannot provide.
	// ErrIsolatedUnavailable wraps it.
	ErrUnavailable = policy.ErrUnavailable

	// ErrBusy reports a resource that is occupied. The session-busy variant wraps it.
	ErrBusy = policy.ErrBusy

	// ErrClosed reports a resource that is closed or already consumed. ErrIsolatedClosed and
	// the session-closed variant wrap it.
	ErrClosed = policy.ErrClosed

	// ErrLimit reports an exceeded resource limit. The session-limit variant wraps it.
	ErrLimit = policy.ErrLimit

	// ErrUnpinnedModuleRef is returned by Interpreter.LoadModule() when the reference
	// carries no pin and the interpreter was not built with WithAllowUnpinnedModules().
	// Match it with errors.Is().
	ErrUnpinnedModuleRef = module.ErrUnpinnedRef

	// ErrEmptyBytecodeDirectory is returned by NewDirectoryBytecodeStore for an empty path.
	ErrEmptyBytecodeDirectory = errors.New("pipit: bytecode store directory must not be empty")

	// ErrParse is returned when go/parser rejects the source code.
	ErrParse = fault.ErrParse

	// ErrTypeCheck is returned when go/types rejects the source code.
	ErrTypeCheck = fault.ErrTypeCheck

	// ErrCompilation is returned when the compiler rejects type-checked source.
	ErrCompilation = fault.ErrCompilation

	// ErrCyclicImport is returned when the import graph contains a cycle.
	ErrCyclicImport = fault.ErrCyclicImport

	// ErrEmbedUnsupported is returned when a file the interpreter compiles carries a
	// //go:embed directive. Guard such a file with //go:build !interp and give the
	// interpreter a //go:build interp alternative that reads the files at run time.
	ErrEmbedUnsupported = fault.ErrCompileEmbedUnsupported

	// ErrRegisterOverflow is returned when a function needs more live registers in one bank
	// than the bytecode can address, even after spilling its locals. The error names the
	// function and, when known, the statement where it ran out.
	ErrRegisterOverflow = fault.ErrRegisterOverflow

	// ErrSourceSizeLimit is returned when the source exceeds the configured maximum size.
	ErrSourceSizeLimit = fault.ErrSourceSizeLimit

	// ErrEntrypointNotFound is returned when the requested entrypoint does not exist.
	ErrEntrypointNotFound = fault.ErrEntrypointNotFound

	// ErrExecutionCancelled is returned when the context ends an execution early.
	ErrExecutionCancelled = fault.ErrExecutionCancelled

	// ErrCallbackTimeout is returned when an interpreted closure called from native code
	// runs past the timeout set by WithCallbackTimeout. An error carrying it also matches
	// ErrExecutionCancelled.
	ErrCallbackTimeout = fault.ErrCallbackTimeout

	// ErrFeatureNotAllowed is returned when the source uses a language feature the
	// configured feature set forbids.
	ErrFeatureNotAllowed = fault.ErrFeatureNotAllowed

	// ErrPackageNotInRegistry is returned when an import names a package no symbol provider
	// registered.
	ErrPackageNotInRegistry = fault.ErrPackageNotInRegistry

	// ErrNoBytecodeStore is returned when bytecode save or load runs without a store.
	ErrNoBytecodeStore = fault.ErrNoBytecodeStore

	// ErrGlobalSlotsExhausted is returned by LoadCompiled when the interpreter has no room
	// left for the loaded program's package variables. Unloading programs with
	// Interpreter.UnloadCompiled frees room.
	ErrGlobalSlotsExhausted = fault.ErrGlobalSlotsExhausted

	// ErrProgramUnloaded is returned by a run of a file set after Interpreter.UnloadCompiled
	// released it, or after Interpreter.Reset. It wraps ErrClosed.
	ErrProgramUnloaded = app.ErrProgramUnloaded

	// ErrProgramNotLoaded is returned by Interpreter.UnloadCompiled for a file set this
	// interpreter did not load from bytecode.
	ErrProgramNotLoaded = app.ErrProgramNotLoaded

	// ErrNamedInterfaceLimit is returned when loading bytecode would register more distinct
	// interface names than WithMaxLoadedNamedInterfaces allows.
	ErrNamedInterfaceLimit = app.ErrNamedInterfaceLimit

	// ErrNamedCompositeQuota is returned when a program would add more named array, slice,
	// map, channel and function types than WithMaxNamedCompositeTypes allows.
	ErrNamedCompositeQuota = fault.ErrNamedCompositeQuota

	// ErrNamedCompositeLimit is returned when a program would take the process past the cap
	// WithNamedCompositeProcessLimit sets on named array, slice, map, channel and function
	// types.
	ErrNamedCompositeLimit = fault.ErrNamedCompositeLimit

	// ErrBytecodeVerification is returned when the bytecode verifier rejects a program.
	ErrBytecodeVerification = fault.ErrBytecodeVerification

	// ErrUncaughtPanic identifies an execution that ended in a panic no recover caught, in
	// the main goroutine or a spawned one. Use UncaughtPanicValue to read the value.
	ErrUncaughtPanic = fault.ErrUncaughtPanic

	// ErrInterpreterInvariant identifies an interpreter defect (state the compiler promised
	// could not occur) rather than a program error; it is never recoverable by interpreted
	// code.
	ErrInterpreterInvariant = fault.ErrInterpreterInvariant

	// ErrDeadlock is returned when every goroutine of an execution is parked on a channel
	// the program made and nothing can wake them, Go's "all goroutines are asleep" fatal
	// error. Tune or disable the check with WithDeadlockGrace.
	ErrDeadlock = fault.ErrDeadlock

	// ErrCostBudgetExceeded is returned when execution exhausts the cost budget.
	ErrCostBudgetExceeded = fault.ErrCostBudgetExceeded

	// ErrAllocationLimit is returned when one allocation exceeds the configured maximum.
	ErrAllocationLimit = fault.ErrAllocationLimit

	// ErrOutputLimit is returned when print output exceeds the configured maximum.
	ErrOutputLimit = fault.ErrOutputLimit

	// ErrGoroutineLimit is returned when interpreted code spawns too many goroutines.
	ErrGoroutineLimit = fault.ErrGoroutineLimit

	// ErrGoroutineJoinTimeout is returned when goroutines the program spawned are still
	// running after its entrypoint returned and the join grace period. A whole-program
	// runner may treat it as a normal exit, as Go does when main returns.
	ErrGoroutineJoinTimeout = fault.ErrGoroutineJoinTimeout

	// ErrUnwindBlocked is joined to the error of a stopped execution (a budget abort, the
	// cost budget, the context's cancellation or deadline) when one of the deferred calls
	// its stopped goroutines ran was still blocked in native code at the unwinding's
	// deadline.
	ErrUnwindBlocked = fault.ErrUnwindBlocked

	// ErrLockAbandoned is returned by every evaluation once an earlier one was stopped and
	// left a sync.Mutex or sync.RWMutex it had locked still held after its deferred calls
	// ran.
	ErrLockAbandoned = fault.ErrLockAbandoned

	// ErrUnwindLeakLimit is returned by every evaluation once the interpreter's stopped
	// evaluations have left as many deferred calls blocked in native code (each reported
	// with ErrUnwindBlocked) as WithLeakedUnwinderCap allows, 64 by default, since each one
	// keeps a goroutine and its memory alive.
	ErrUnwindLeakLimit = fault.ErrUnwindLeakLimit

	// ErrStackOverflow is returned when the call stack exceeds the maximum depth.
	ErrStackOverflow = fault.ErrStackOverflow

	// ErrArenaBudgetExceeded is returned when register-arena growth exceeds its budget.
	ErrArenaBudgetExceeded = fault.ErrArenaBudgetExceeded

	// ErrDivisionByZero is returned when an integer division has a zero divisor and no
	// deferred recover catches the panic.
	ErrDivisionByZero = fault.ErrDivisionByZero

	// ErrIndexOutOfRange is returned when an index is outside [0, len) and no deferred
	// recover catches the panic.
	ErrIndexOutOfRange = fault.ErrIndexOutOfRange

	// ErrNoSymbolProvider reports that a restricted interpreter was built without any symbol
	// provider, so it has no imports to review.
	ErrNoSymbolProvider = fault.ErrNoSymbolProvider
)

// EvaluationError reports that interpreted script code failed, as opposed to a
// configuration, transport or resource error. A host distinguishes the two with
// errors.As.
type EvaluationError = fault.EvaluationError

// ProblemList reports several independent problems that one CompileProgram call found,
// such as type errors in two packages and a //go:embed directive in a third. errors.Is
// matches the sentinel of any problem; CompileProblems lists them.
type ProblemList = fault.ProblemList

var (
	// ErrDebuggerStop is the error an execution returns when the debugger stopped it.
	ErrDebuggerStop = fault.ErrDebuggerStop

	// ErrDebugNotPaused is returned by debugger inspection and resume operations when no
	// thread is paused.
	ErrDebugNotPaused = fault.ErrDebugNotPaused

	// ErrDebugNotRunning is returned by a pause request when no execution is running.
	ErrDebugNotRunning = fault.ErrDebugNotRunning

	// ErrDebugAlreadyPaused is returned by a pause request while already paused.
	ErrDebugAlreadyPaused = fault.ErrDebugAlreadyPaused

	// ErrDebugUnknownThread is returned when a debugger operation names no live thread.
	ErrDebugUnknownThread = fault.ErrDebugUnknownThread

	// ErrDebugFrameOutOfRange is returned when a frame index addresses no frame.
	ErrDebugFrameOutOfRange = fault.ErrDebugFrameOutOfRange

	// ErrDebugExited is returned by a wait for a pause when the execution ended instead.
	ErrDebugExited = fault.ErrDebugExited

	// ErrNoProgramBinding is returned by expression evaluation when the program was compiled
	// without the information the evaluator needs.
	ErrNoProgramBinding = fault.ErrNoProgramBinding

	// ErrVariableUnavailable is returned when an expression names a variable the debugger
	// does not track at the paused position.
	ErrVariableUnavailable = fault.ErrVariableUnavailable

	// ErrVariableNotWritable is returned by an unsupported variable write.
	ErrVariableNotWritable = fault.ErrVariableNotWritable
)

// CompileProblems splits a compile error for display, returning err itself when it has no
// ProblemList. Problems are grouped by package, with each group containing its type
// errors or register overflows.
//
// Takes err (error) which is the error from a compile call.
//
// Returns []error which is nil for a nil err.
func CompileProblems(err error) []error {
	return fault.Problems(err)
}

// UncaughtPanicValue returns the value of the panic behind an ErrUncaughtPanic error.
//
// Takes err (error) which is an execution error.
//
// Returns the panic value and true, or nil and false when err is not an uncaught panic.
func UncaughtPanicValue(err error) (any, bool) {
	return engine.UncaughtPanicValue(err)
}

// UncaughtPanicStack returns the interpreted call stack recorded when the panic behind an
// ErrUncaughtPanic error started, rendered the way Go's traceback renders frames (a name
// line and an indented file:line line per frame), innermost first.
//
// Takes err (error) which may wrap an uncaught panic.
//
// Returns string which is empty when err is not an uncaught panic or no stack was
// recorded.
func UncaughtPanicStack(err error) string {
	return engine.UncaughtPanicStack(err)
}
