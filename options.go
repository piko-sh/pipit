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
	"io"
	"log/slog"
	"time"

	"pipit.sh/pipit/internal/app"
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

// WithLogger installs the logger that receives interpreter diagnostics and, for the
// isolated tiers, the boundary's audit events. Passing nil resolves the process default
// logger at each call site.
//
// Takes logger (*slog.Logger) which receives diagnostics and audit events.
//
// Returns Option which applies the logger override.
func WithLogger(logger *slog.Logger) Option {
	return app.WithLogger(logger)
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

// WithDeadlockGrace sets the grace period before parked goroutines trigger ErrDeadlock.
// The default is one second; a negative value disables the check.
//
// Takes grace (time.Duration) which is the grace period.
//
// Returns Option which applies the grace period.
func WithDeadlockGrace(grace time.Duration) Option {
	return app.WithDeadlockGrace(grace)
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
