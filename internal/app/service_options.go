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

package app

import (
	"io"
	"log/slog"
	"maps"
	"slices"
	"time"

	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

const (
	// importAllowlistSentinel is a never-matched key that marks an import allowlist as
	// configured even when it holds no real paths, so "deny all external imports" is
	// distinguishable from "no allowlist".
	importAllowlistSentinel = "\x00allowlist-configured"
)

// WithBuildTags sets custom build tags for //go:build constraint evaluation in
// CompileFileSet and CompileProgram. The current GOOS, GOARCH, and Go version are always
// included as default tags.
//
// Takes tags (string variadic) which are additional build tags to activate.
//
// Returns Option which applies the build tags to the service config.
func WithBuildTags(tags ...string) Option {
	return func(c *serviceConfig) {
		c.buildTags = append(c.buildTags, tags...)
	}
}

// WithSymbolProvider registers native symbol tables interpreted code may import. Calls
// accumulate; a later provider wins on duplicate symbols.
//
// Takes providers (symtab.SymbolProviderPort variadic) in increasing order of precedence.
//
// Returns Option which appends the providers to the service config.
func WithSymbolProvider(providers ...symtab.SymbolProviderPort) Option {
	return func(c *serviceConfig) {
		c.symbolProviders = append(c.symbolProviders, providers...)
	}
}

// WithEnv overlays env on the host environment for interpreted code. The map is copied;
// use WithSealedEnv to hide the host environment entirely.
//
// Takes env (map[string]string) which maps variable names to values; nil or empty
// overlays nothing but still keeps the program's own writes away from the host.
//
// Returns Option which applies the environment overlay to the service config.
func WithEnv(env map[string]string) Option {
	return func(c *serviceConfig) {
		c.env = cloneEnv(env)
		c.envSealed = false
		c.envConfigured = true
	}
}

// WithSealedEnv replaces the environment for interpreted code: os.Getenv and its
// relatives see exactly env (an empty map is an empty environment) and never the host
// process's variables, and the program's own writes stay in that private environment. The
// restricted and isolated tiers always seal the environment.
//
// Takes env (map[string]string) which is the whole environment; it is copied.
//
// Returns Option which applies the sealed environment to the service config.
func WithSealedEnv(env map[string]string) Option {
	return func(c *serviceConfig) {
		c.env = cloneEnv(env)
		c.envSealed = true
		c.envConfigured = true
	}
}

// WithArgs sets the argument vector interpreted code observes via os.Args. The slice is
// copied.
//
// Takes args ([]string) which is the argument vector; an empty slice is padded with an
// empty program name.
//
// Returns Option which applies the argument vector to the service config.
func WithArgs(args []string) Option {
	return func(c *serviceConfig) {
		copied := slices.Clone(args)
		if len(copied) == 0 {
			copied = []string{""}
		}
		c.args = copied
	}
}

// WithMaxExecutionTime sets the maximum duration for any single evaluation. Each public
// method wraps the caller's context with this deadline and the shorter limit wins.
//
// Takes d (time.Duration) which is the maximum execution time.
//
// Returns Option which applies the execution time limit.
func WithMaxExecutionTime(d time.Duration) Option {
	return func(c *serviceConfig) {
		c.maxExecutionTime = d
	}
}

// WithMaxAllocSize sets the maximum number of elements permitted in a single allocation
// (make slice, make chan, unsafe.String, unsafe.Slice). Zero means unlimited.
//
// Takes n (int) which is the maximum element count.
//
// Returns Option which applies the allocation limit.
func WithMaxAllocSize(n int) Option {
	return func(c *serviceConfig) {
		c.maxAllocSize = n
	}
}

// WithMaxGoroutines sets the maximum number of concurrent goroutines that interpreted
// code may spawn via go statements. Zero means unlimited.
//
// Takes n (int32) which is the goroutine limit.
//
// Returns Option which applies the goroutine limit.
func WithMaxGoroutines(n int32) Option {
	return func(c *serviceConfig) {
		c.maxGoroutines = n
	}
}

// WithMaxCallDepth sets the maximum call stack depth before a stack overflow error is
// raised. Zero uses the default (10000).
//
// Takes n (int) which is the call depth limit.
//
// Returns Option which applies the call depth limit.
func WithMaxCallDepth(n int) Option {
	return func(c *serviceConfig) {
		c.maxCallDepth = n
	}
}

// WithMaxOutputSize sets the maximum number of bytes that print and println may write
// before returning an error. Zero means unlimited.
//
// Takes n (int) which is the output byte limit.
//
// Returns Option which applies the output size limit.
func WithMaxOutputSize(n int) Option {
	return func(c *serviceConfig) {
		c.maxOutputSize = n
	}
}

// WithForceGoDispatch forces the VM to use the pure Go dispatch loop even on
// architectures with ASM threaded dispatch (amd64, arm64). Useful for testing dispatch
// parity across both paths.
//
// Returns Option which enables pure Go dispatch.
func WithForceGoDispatch() Option {
	return func(c *serviceConfig) {
		c.forceGoDispatch = true
	}
}

// WithMinorGCDisabled keeps the register arena's minor collection from running for the
// service's executions. Memory then grows monotonically until the arena's byte budget is
// hit, so this is a diagnostic knob: when a program misbehaves only with collection on,
// the collector's mark or compaction phase is the suspect.
//
// Returns Option which disables the minor collection.
func WithMinorGCDisabled() Option {
	return func(c *serviceConfig) {
		c.disableMinorGC = true
	}
}

// WithSafeMode enables additional per-instruction runtime guards. Safe mode forces the
// pure Go dispatch loop.
//
// Returns Option which enables the runtime guard mode.
func WithSafeMode() Option {
	return func(c *serviceConfig) {
		c.safeMode = true
	}
}

// WithDebugInfo enables debug information generation during compilation. When enabled,
// the Compiler records source positions for each bytecode instruction and variable
// liveness ranges.
//
// Returns Option which enables debug info generation.
func WithDebugInfo() Option {
	return func(c *serviceConfig) {
		c.debugInfo = true
	}
}

// WithBytecodeVerification toggles the post-compilation bytecode verifier. Verification
// is on by default; pass false to disable it (for example in production builds where the
// cost is unwanted).
//
// Takes enabled (bool) which selects whether the verifier runs.
//
// Returns Option which sets the verifier opt-out flag.
func WithBytecodeVerification(enabled bool) Option {
	return func(c *serviceConfig) {
		c.bytecodeVerificationDisabled = !enabled
	}
}

// WithOptimisations selects which compiler optimisations run.
//
// Takes opts (passes.Options) which replaces the whole selection.
//
// Returns Option which installs the selection.
func WithOptimisations(opts passes.Options) Option {
	return func(c *serviceConfig) {
		c.optimisations = opts
	}
}

// WithAllowUnpinnedModules controls whether LoadModule() accepts a reference without a
// pin. Pins are required by default so every loaded bundle is checked against an
// operator-declared fingerprint; pass true for interactive or development hosts that
// build bundles on the fly and have nothing to pin them against.
//
// Takes allow (bool) which selects whether unpinned refs are accepted.
//
// Returns Option which sets the unpinned-module policy.
func WithAllowUnpinnedModules(allow bool) Option {
	return func(c *serviceConfig) {
		c.allowUnpinnedModules = allow
	}
}

// WithDeniedImports rejects the named import paths at compile time. A script that imports
// any of them fails to compile with a clear error.
//
// Takes paths (...string) which are the import paths to deny.
//
// Returns Option which records the denied import set.
func WithDeniedImports(paths ...string) Option {
	return func(c *serviceConfig) {
		if c.deniedImports == nil {
			c.deniedImports = make(map[string]struct{}, len(paths))
		}
		for _, path := range paths {
			c.deniedImports[path] = struct{}{}
		}
	}
}

// WithImportAllowlist restricts which external packages a script may import.
//
// Takes paths (...string) which are the permitted external import paths.
//
// Returns Option which records the allowlist.
func WithImportAllowlist(paths ...string) Option {
	return func(c *serviceConfig) {
		if c.allowedImports == nil {
			c.allowedImports = make(map[string]struct{}, len(paths)+1)
		}
		c.allowedImports[importAllowlistSentinel] = struct{}{}
		for _, path := range paths {
			c.allowedImports[path] = struct{}{}
		}
	}
}

// WithDebugger attaches a Debugger to the interpreter service. This implies
// WithDebugInfo() and WithForceGoDispatch(): debug info is needed for breakpoints and
// source mapping, and Go dispatch is required because the ASM loop does not support the
// debug hook.
//
// Takes debugger (*Debugger) which is the debugger to attach.
//
// Returns Option which attaches the debugger.
func WithDebugger(debugger *debug.Debugger) Option {
	return func(c *serviceConfig) {
		c.debugInfo = true
		c.forceGoDispatch = true
		c.debugger = debugger
	}
}

// WithArenaFactory sets a custom factory for register arena allocation.
//
// When nil, the global sync.Pool is used. Each call to the factory should return a fresh
// RegisterArena for test isolation.
//
// Takes factory (func() *RegisterArena) which is the arena constructor.
//
// Returns Option which applies the arena factory.
func WithArenaFactory(factory func() *engine.RegisterArena) Option {
	return func(c *serviceConfig) {
		c.arenaFactory = factory
	}
}

// withCompilationSnapshot registers a callback that receives a snapshot of the compiled
// output at the end of CompileProgram, regardless of whether compilation succeeded or
// failed partway through. The snapshot contains all functions from packages that compiled
// successfully before the failure.
//
// Takes callback (func(*CompiledFileSet)) which receives the partial or complete
// compilation snapshot.
//
// Returns Option which registers the snapshot callback.
func withCompilationSnapshot(callback func(*program.CompiledFileSet)) Option {
	return func(c *serviceConfig) {
		c.compilationSnapshotCallback = callback
	}
}

// WithFeatures sets the allowed language features for compilation.
//
// Features not present in the bitmask cause a compile-time error when used. The default
// is InterpFeaturesAll.
//
// Takes features (InterpFeature) which is the bitmask of allowed features.
//
// Returns Option which applies the feature restrictions.
func WithFeatures(features policy.InterpFeature) Option {
	return func(c *serviceConfig) {
		c.features = features
	}
}

// WithCostBudget sets the maximum total computation cost for a single execution, with
// zero disabling cost metering. Cost metering forces Go dispatch.
//
// Takes budget (int64) which is the total cost budget.
//
// Returns Option which enables cost metering.
func WithCostBudget(budget int64) Option {
	return func(c *serviceConfig) {
		c.costBudget = budget
	}
}

// WithCostTable sets a custom per-opcode cost table for cost metering.
//
// If not set, the default cost table is used. Use DefaultCostTable to get a copy of the
// default table and modify individual entries.
//
// Takes table (CostTable) which is the custom cost table.
//
// Returns Option which applies the custom cost table.
func WithCostTable(table *isa.CostTable) Option {
	return func(c *serviceConfig) {
		c.costTable = table
	}
}

// WithMaxSourceSize sets the maximum total source code size in bytes accepted for
// compilation. Zero (the default) means no limit.
//
// Takes n (int) which is the maximum source size in bytes.
//
// Returns Option which enables source size limiting.
func WithMaxSourceSize(n int) Option {
	return func(c *serviceConfig) {
		c.maxSourceSize = n
	}
}

// WithMaxStringSize sets the maximum string length in bytes that a concatenation may
// produce at runtime. Zero (the default) means no limit.
//
// Takes n (int) which is the maximum string size in bytes.
//
// Returns Option which enables string size limiting.
func WithMaxStringSize(n int) Option {
	return func(c *serviceConfig) {
		c.maxStringSize = n
	}
}

// WithMaxLiteralElements sets the maximum number of elements allowed in a single
// composite literal (slice, array, map) at compile time. Zero (the default) means no
// limit.
//
// Takes n (int) which is the maximum element count.
//
// Returns Option which enables literal element limiting.
func WithMaxLiteralElements(n int) Option {
	return func(c *serviceConfig) {
		c.maxLiteralElements = n
	}
}

// WithYieldInterval sets the number of instructions between runtime.Gosched() calls for
// cooperative scheduling. Requires a power-of-two value; zero disables.
//
// Takes n (uint32) which is the instruction interval between yields.
//
// Returns Option which enables cooperative yielding.
func WithYieldInterval(n uint32) Option {
	return func(c *serviceConfig) {
		c.yieldInterval = n
	}
}

// WithDeadlockGrace sets how long every goroutine of an execution may stay parked on
// channels the program made before the execution fails with fault.ErrDeadlock, the
// interpreter's equivalent of Go's "all goroutines are asleep" fatal error. Goroutines
// parked in native calls or on host channels (time.After, a context's Done) never count.
//
// Takes grace (time.Duration) which replaces the one-second default; a negative value
// disables the check.
//
// Returns Option which applies the grace period.
func WithDeadlockGrace(grace time.Duration) Option {
	return func(c *serviceConfig) {
		if grace < 0 {
			grace = 0
		}
		c.deadlockGrace = grace
	}
}

// WithMaxConstantPoolSize sets the maximum number of entries permitted in each
// per-function constant pool (int, float, string, bool, uint, complex, general, type,
// call-site). Zero leaves the package default (defaultMaxConstantPoolSize, 65535) in
// force.
//
// Takes n (int) which is the maximum entry count.
//
// Returns Option which applies the constant-pool ceiling.
func WithMaxConstantPoolSize(n int) Option {
	return func(c *serviceConfig) {
		c.maxConstantPoolSize = n
	}
}

// WithMaxSpecialisations sets the maximum number of generic-function specialisations any
// single generic callee may accumulate. Zero leaves the package default
// (defaultMaxSpecialisations, 1000) in force.
//
// Takes n (int) which is the maximum specialisation count.
//
// Returns Option which applies the specialisation ceiling.
func WithMaxSpecialisations(n int) Option {
	return func(c *serviceConfig) {
		c.maxSpecialisations = n
	}
}

// WithMaxMethods sets the maximum number of entries permitted in the root function's
// method table. Zero leaves the package default (defaultMaxMethods, 10000) in force.
//
// Takes n (int) which is the maximum method count.
//
// Returns Option which applies the method-table ceiling.
func WithMaxMethods(n int) Option {
	return func(c *serviceConfig) {
		c.maxMethods = n
	}
}

// WithMaxExpressionDepth sets the maximum recursion depth permitted when compiling nested
// expressions. Zero leaves the package default (defaultMaxExpressionDepth, 1024) in
// force.
//
// Takes n (int) which is the maximum expression nesting depth.
//
// Returns Option which applies the expression-depth ceiling.
func WithMaxExpressionDepth(n int) Option {
	return func(c *serviceConfig) {
		c.maxExpressionDepth = n
	}
}

// WithMaxArenaSizeBytes sets the arena working-set byte budget. Zero leaves the
// host-aware default in force.
//
// Takes n (uint64) which is the maximum arena working-set size in bytes.
//
// Returns Option which applies the arena-size ceiling.
func WithMaxArenaSizeBytes(n uint64) Option {
	return func(c *serviceConfig) {
		c.maxArenaSizeBytes = n
		c.maxArenaBytes = n
	}
}

// WithVerifierIterationLimit sets the maximum number of dataflow iterations the bytecode
// verifier may perform per function. Zero leaves the verifier default (100000) in force.
//
// Takes n (int) which is the maximum iteration count.
//
// Returns Option which applies the verifier iteration ceiling.
func WithVerifierIterationLimit(n int) Option {
	return func(c *serviceConfig) {
		c.verifierIterationLimit = n
	}
}

// WithCapabilityHook installs a CapabilityHook consulted before every gated native
// operation. Nil preserves the ungated default.
//
// Takes hook (CapabilityHook) which is consulted at gated dispatch sites.
//
// Returns Option which applies the hook to the service config.
func WithCapabilityHook(hook policy.CapabilityHook) Option {
	return func(c *serviceConfig) {
		c.capabilityHook = hook
	}
}

// WithClock installs a Clock replacing stdlib time access in interpreted code. Nil
// selects WallClock.
//
// Takes source (clock.Clock) which becomes the interpreted time source.
//
// Returns Option which applies the clock override.
func WithClock(source clock.Clock) Option {
	return func(c *serviceConfig) {
		c.clock = source
	}
}

// WithLogger installs the logger for the interpreter's warnings and errors. Nil selects
// the process default.
//
// Takes logger (*slog.Logger) which receives the interpreter's diagnostics.
//
// Returns Option which applies the logger override.
func WithLogger(logger *slog.Logger) Option {
	return func(c *serviceConfig) {
		c.logger = logger
	}
}

// WithStderr routes print and println output to writer instead of os.Stderr. The
// runtime-mutable SetStderr remains for hosts that swap the writer between evaluations.
//
// Takes writer (io.Writer) which receives the interpreted program's stderr output.
//
// Returns Option which sets the writer at construction.
func WithStderr(writer io.Writer) Option {
	return func(config *serviceConfig) {
		config.stderrWriter = writer
	}
}

// cloneEnv copies env into a fresh, never-nil map.
//
// Takes env (map[string]string) which may be nil.
//
// Returns map[string]string which the service owns.
func cloneEnv(env map[string]string) map[string]string {
	copied := make(map[string]string, len(env))
	maps.Copy(copied, env)
	return copied
}
