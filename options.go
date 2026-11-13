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
	"context"
	"io"
	"log/slog"
	"time"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/logging"
)

// WithSymbolProvider registers native symbol tables interpreted code may import. Calls
// accumulate; a later provider wins on duplicate symbols.
//
// Takes providers (SymbolProviderPort variadic) in increasing order of precedence.
//
// Returns Option which appends the providers.
func WithSymbolProvider(providers ...SymbolProviderPort) Option {
	return app.WithSymbolProvider(providers...)
}

// WithLogger installs the fallback logger for interpreter diagnostics and, for the
// isolated tiers, the boundary's audit events.
//
// A logger already in a call's context wins. Nil leaves the process default as the
// fallback.
//
// Takes logger (*slog.Logger) which receives diagnostics and audit events.
//
// Returns Option which applies the logger override.
func WithLogger(logger *slog.Logger) Option {
	return app.WithLogger(logger)
}

// ContextWithLogger returns a copy of ctx carrying logger.
//
// Passing nil returns ctx unchanged.
//
// Takes logger (*slog.Logger) which receives the diagnostics of calls made with the
// returned context.
//
// Returns context.Context which carries logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return logging.WithLogger(ctx, logger)
}

// WithSafeMode disables the unsafe fast paths, keeping only the memory-safe interpreter.
//
// Returns Option applying the setting.
func WithSafeMode() Option { return app.WithSafeMode() }

// WithStderr routes print and println output to writer instead of os.Stderr.
//
// Takes writer (io.Writer) which receives the interpreted program's stderr output.
//
// Returns Option applying the setting.
func WithStderr(writer io.Writer) Option { return app.WithStderr(writer) }

// WithBytecodeVerification enables or disables the bytecode verifier before execution.
//
// Takes enabled (bool).
//
// Returns Option applying the setting.
func WithBytecodeVerification(enabled bool) Option { return app.WithBytecodeVerification(enabled) }

// WithMaxExpressionDepth bounds the nesting depth the compiler accepts.
//
// Takes n (int).
//
// Returns Option applying the setting.
func WithMaxExpressionDepth(n int) Option { return app.WithMaxExpressionDepth(n) }

// WithMaxConstantPoolSize bounds a compiled function's constant pool.
//
// Takes n (int).
//
// Returns Option applying the setting.
func WithMaxConstantPoolSize(n int) Option { return app.WithMaxConstantPoolSize(n) }

// WithMaxSpecialisations bounds the generic specialisations one compilation may create.
//
// Takes n (int).
//
// Returns Option applying the setting.
func WithMaxSpecialisations(n int) Option { return app.WithMaxSpecialisations(n) }

// WithMaxMethods bounds the method-set size one compilation may synthesise.
//
// Takes n (int).
//
// Returns Option applying the setting.
func WithMaxMethods(n int) Option { return app.WithMaxMethods(n) }

// WithMaxArenaSizeBytes bounds the register arena, not all Go memory.
//
// Takes n (uint64).
//
// Returns Option applying the setting.
func WithMaxArenaSizeBytes(n uint64) Option { return app.WithMaxArenaSizeBytes(n) }

// WithVerifierIterationLimit bounds the bytecode verifier's fixed-point iterations.
//
// Takes n (int).
//
// Returns Option applying the setting.
func WithVerifierIterationLimit(n int) Option { return app.WithVerifierIterationLimit(n) }

// WithMaxExecutionTime caps total wall-clock time for one evaluation.
//
// Takes maximum (time.Duration) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxExecutionTime(maximum time.Duration) Option {
	return app.WithMaxExecutionTime(maximum)
}

// WithMaxAllocSize caps the element count of a single allocation.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxAllocSize(maximum int) Option {
	return app.WithMaxAllocSize(maximum)
}

// WithMaxGoroutines caps concurrent goroutines started by interpreted code.
//
// Takes maximum (int32) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxGoroutines(maximum int32) Option {
	return app.WithMaxGoroutines(maximum)
}

// WithMaxCallDepth caps call stack depth before an overflow is reported.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxCallDepth(maximum int) Option {
	return app.WithMaxCallDepth(maximum)
}

// WithMaxOutputSize caps the bytes print and println may write.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxOutputSize(maximum int) Option {
	return app.WithMaxOutputSize(maximum)
}

// WithMaxSourceSize caps the total source size accepted for compilation.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxSourceSize(maximum int) Option {
	return app.WithMaxSourceSize(maximum)
}

// WithMaxStringSize caps the length a string concatenation may produce.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxStringSize(maximum int) Option {
	return app.WithMaxStringSize(maximum)
}

// WithMaxLiteralElements caps the elements in one composite literal.
//
// Takes maximum (int) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithMaxLiteralElements(maximum int) Option {
	return app.WithMaxLiteralElements(maximum)
}

// WithCostBudget caps the total computation cost of one execution.
//
// Takes budget (int64) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithCostBudget(budget int64) Option {
	return app.WithCostBudget(budget)
}

// WithYieldInterval sets how often the dispatch loop checks for cancellation.
//
// Takes interval (uint32) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithYieldInterval(interval uint32) Option {
	return app.WithYieldInterval(interval)
}

// WithCallbackTimeout bounds each run of an interpreted closure that native code calls,
// including time spent in channel operations, select or sleep. A run past the bound fails
// with ErrCallbackTimeout, which also matches ErrExecutionCancelled; zero or negative
// disables the bound.
//
// Takes timeout (time.Duration) which is the bound.
//
// Returns Option which applies the bound.
func WithCallbackTimeout(timeout time.Duration) Option {
	return app.WithCallbackTimeout(timeout)
}

// WithFinaliserTimeout bounds each run of a finaliser or cleanup a program registered
// with runtime.SetFinalizer or runtime.AddCleanup.
//
// A run past the bound is stopped and the next finaliser runs. The default is 30 seconds.
// A shorter WithCallbackTimeout applies instead.
//
// Takes timeout (time.Duration) which is the bound. It must be positive.
//
// Returns Option which applies the bound.
func WithFinaliserTimeout(timeout time.Duration) Option {
	return app.WithFinaliserTimeout(timeout)
}

// WithDeadlockGrace sets the grace period before parked goroutines trigger ErrDeadlock.
// The default is one second; a negative value disables the check.
//
// Takes grace (time.Duration) which is the grace period.
//
// Returns Option which applies the grace period.
func WithDeadlockGrace(grace time.Duration) Option {
	return app.WithDeadlockGrace(grace)
}

// WithAbortUnwindCost sets the total cost, in cost-table units, that the pending defers
// of a stopped evaluation may spend between them while it unwinds. The default is
// 100,000.
//
// Takes cost (int64) which is the allowance; a value that is not positive makes every
// compile and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the allowance.
func WithAbortUnwindCost(cost int64) Option {
	return app.WithAbortUnwindCost(cost)
}

// WithAbortUnwindWall sets the total wall time that the pending defers of a stopped
// evaluation may spend waiting while it unwinds. The default is one second.
//
// Takes wall (time.Duration) which is the allowance; a value that is not positive makes
// every compile and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the allowance.
func WithAbortUnwindWall(wall time.Duration) Option {
	return app.WithAbortUnwindWall(wall)
}

// WithAbortUnwindDeferWallMax caps the wall time one deferred call may spend while a
// stopped evaluation unwinds. The default is 500 ms.
//
// Takes wall (time.Duration) which is the cap; a value that is not positive makes every
// compile and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the cap.
func WithAbortUnwindDeferWallMax(wall time.Duration) Option {
	return app.WithAbortUnwindDeferWallMax(wall)
}

// WithAbortUnwindNativeGrace sets how long past its wall deadline a deferred call of an
// unwinding may stay inside a native call before the evaluation returns ErrUnwindBlocked
// and leaves the call's goroutine and memory behind. The default is 200 ms.
//
// Takes grace (time.Duration) which is the grace; a value that is not positive makes
// every compile and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the grace.
func WithAbortUnwindNativeGrace(grace time.Duration) Option {
	return app.WithAbortUnwindNativeGrace(grace)
}

// WithGoroutineJoinUnwindCap bounds how long, from its start, the wait for an
// evaluation's goroutines keeps going while some of them run their deferred calls after a
// stop. The default is ten seconds.
//
// Takes bound (time.Duration) which is the bound; a value that is not positive makes
// every compile and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the bound.
func WithGoroutineJoinUnwindCap(bound time.Duration) Option {
	return app.WithGoroutineJoinUnwindCap(bound)
}

// WithLeakedUnwinderCap sets how many deferred calls left blocked in native code (each
// reported with ErrUnwindBlocked) the interpreter allows before every later evaluation
// fails with ErrUnwindLeakLimit. The default is 64.
//
// Takes limit (int64) which is the cap; a value that is not positive makes every compile
// and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the cap.
func WithLeakedUnwinderCap(limit int64) Option {
	return app.WithLeakedUnwinderCap(limit)
}

// WithLockWalkMaxValues caps the values the abandoned-lock check visits from the package
// variables after a stopped evaluation.
//
// Takes values (int) which is the cap; a value that is not positive makes every compile
// and evaluation fail with ErrInvalidOption.
//
// Returns Option which applies the cap.
func WithLockWalkMaxValues(values int) Option {
	return app.WithLockWalkMaxValues(values)
}

// WithBuildTags adds build tags for constraint evaluation.
//
// Takes tags (...string) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithBuildTags(tags ...string) Option {
	return app.WithBuildTags(tags...)
}

// WithEnv overlays env on the host environment for interpreted code. The map is copied;
// use WithSealedEnv to hide the host environment entirely.
//
// Takes env (map[string]string) which maps variable names to values.
//
// Returns Option which applies the environment overlay.
func WithEnv(env map[string]string) Option {
	return app.WithEnv(env)
}

// WithSealedEnv replaces the environment for interpreted code with exactly env; the host
// process's variables are never visible, and the program's own writes stay private. The
// restricted and isolated tiers always seal the environment.
//
// Takes env (map[string]string) which is the whole environment; it is copied.
//
// Returns Option which applies the sealed environment.
func WithSealedEnv(env map[string]string) Option {
	return app.WithSealedEnv(env)
}

// WithArgs sets the argument vector interpreted code observes through os.Args and the
// flag package, args[0] being the program name, so a script's flag.Parse sees the
// script's own arguments. The slice is copied.
//
// Takes args ([]string) which is the argument vector.
//
// Returns Option which applies the argument vector.
func WithArgs(args []string) Option {
	return app.WithArgs(args)
}

// WithForceGoDispatch selects the portable Go dispatch loop over the assembly one.
//
// Returns Option applying the setting.
func WithForceGoDispatch() Option {
	return app.WithForceGoDispatch()
}

// WithDebugInfo retains source maps and variable tables for debugging.
//
// Returns Option applying the setting.
func WithDebugInfo() Option {
	return app.WithDebugInfo()
}

// WithDebugger attaches a debugger to every VM the interpreter creates.
//
// Takes debugger (*Debugger) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithDebugger(debugger *Debugger) Option {
	return app.WithDebugger(debugger)
}

// WithBytecodeStore installs the store used by SaveCompiled and LoadCompiled.
//
// Takes store (BytecodeStorePort) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithBytecodeStore(store BytecodeStorePort) Option {
	return app.WithBytecodeStore(store)
}

// WithMaxLoadedNamedInterfaces caps how many distinct interface names loaded bytecode may
// add to the interpreter.
//
// A load that would pass the cap fails with ErrNamedInterfaceLimit. The default is
// 65,536.
//
// Takes count (int) which must be positive.
//
// Returns Option applying the setting.
func WithMaxLoadedNamedInterfaces(count int) Option {
	return app.WithMaxLoadedNamedInterfaces(count)
}

// WithMaxNamedCompositeTypes caps how many new named array, slice, map, channel and
// function types the programs one interpreter compiles or loads may add.
//
// Identical declarations share one type and are charged once. A program that would pass
// the cap fails with ErrNamedCompositeQuota. The default is 4,096.
//
// Takes count (int) which must be positive.
//
// Returns Option applying the setting.
func WithMaxNamedCompositeTypes(count int) Option {
	return app.WithMaxNamedCompositeTypes(count)
}

// WithNamedCompositeProcessLimit sets the cap on named array, slice, map, channel and
// function types across the whole process.
//
// The last interpreter built with this option sets it for all of them. Types already
// built stay, and a cap below their count refuses every new one. The default is 65,536.
//
// Takes count (int) which must be positive.
//
// Returns Option applying the setting.
func WithNamedCompositeProcessLimit(count int) Option {
	return app.WithNamedCompositeProcessLimit(count)
}

// WithCapabilityHook installs the hook consulted before gated native operations.
//
// Takes hook (CapabilityHook) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithCapabilityHook(hook CapabilityHook) Option {
	return app.WithCapabilityHook(hook)
}

// WithClock replaces the time source interpreted code observes.
//
// Takes source (Clock) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithClock(source Clock) Option {
	return app.WithClock(source)
}

// WithAllowUnpinnedModules permits loading module refs that carry no pin.
//
// Takes allow (bool) which is applied to the interpreter.
//
// Returns Option applying the setting.
func WithAllowUnpinnedModules(allow bool) Option {
	return app.WithAllowUnpinnedModules(allow)
}

// WithImportAllowlist restricts interpreted code to importing only the given paths.
//
// An empty allowlist imposes no restriction. Combine with WithDeniedImports to carve a
// hole in an otherwise permitted set.
//
// Takes paths (...string) which are the permitted import paths.
//
// Returns Option applying the allowlist.
func WithImportAllowlist(paths ...string) Option {
	return app.WithImportAllowlist(paths...)
}

// WithDeniedImports forbids interpreted code from importing the given paths.
//
// A denial wins over WithImportAllowlist, so a path in both is denied.
//
// Takes paths (...string) which are the forbidden import paths.
//
// Returns Option applying the denials.
func WithDeniedImports(paths ...string) Option {
	return app.WithDeniedImports(paths...)
}
