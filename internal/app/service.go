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
	"go/token"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

const (
	// evalFileName is the synthetic filename used for parsed eval snippets.
	evalFileName = "eval.go"

	// mainPackageName is the package name used for eval compilations.
	mainPackageName = "main"

	// newlineSep is the newline separator used when joining source lines.
	newlineSep = "\n"

	// errVarinitFmt is the format string for variable initialiser errors.
	errVarinitFmt = "varinit: %w"

	// safeMessageEvaluationFailed is the user-safe summary returned to LSP / REPL callers
	// when Eval fails. The underlying go/types or Compiler diagnostic stays in the wrapped
	// cause for developer builds; production surfaces only this generic line.
	safeMessageEvaluationFailed = "evaluation failed"

	// safeMessageEvaluationCancelled is the user-safe summary returned when Eval is
	// cancelled or its deadline elapses.
	safeMessageEvaluationCancelled = "evaluation cancelled"

	// safeMessageCompilationFailed is the user-safe summary returned when Compile fails.
	// Mirrors safeMessageEvaluationFailed for the compile-only entrypoint.
	safeMessageCompilationFailed = "compilation failed"

	// safeMessageCompilationCancelled is the user-safe summary returned when Compile is
	// cancelled or its deadline elapses.
	safeMessageCompilationCancelled = "compilation cancelled"

	// safeMessageSubmissionFailed is the user-safe summary returned by Session.Submit when
	// any pipeline step (preprocess, classify, re-check, compile, execute) fails.
	safeMessageSubmissionFailed = "session submission failed"

	// defaultMaxExecutionTime is the maximum duration for any single evaluation when no
	// explicit limit is configured. Prevents untrusted code from running indefinitely.
	defaultMaxExecutionTime = 15 * time.Minute

	// defaultMaxAllocSize is the maximum element count for a single allocation when no
	// explicit limit is configured.
	defaultMaxAllocSize = 1 << 30

	// defaultMaxOutputSize is the maximum bytes print/println may write when no explicit
	// limit is configured.
	defaultMaxOutputSize = 256 << 20

	// errFmtCompilingFileSet is the format string for wrapping errors during file set
	// compilation.
	errFmtCompilingFileSet = "compiling file set: %w"

	// errFmtCompilingProgram is the format string for wrapping errors during program
	// compilation.
	errFmtCompilingProgram = "compiling program: %w"

	// defaultDeadlockGrace is the default grace period before parked goroutines trigger
	// ErrDeadlock.
	defaultDeadlockGrace = time.Second
)

// Option configures the interpreter service.
type Option func(*serviceConfig)

// Service is the public entry point for the bytecode interpreter. It parses, type-checks,
// compiles, and executes Go source code.
//
// Safe for concurrent reads of the symbol registry after setup. All other state requires
// a distinct Service per concurrent evaluation, typically via Clone.
type Service struct {
	// stderrWriter overrides the writer for print/println output; when nil, VMs fall back to
	// os.Stderr, and REPL and notebook hosts set this to capture output for display in their
	// own UI.
	stderrWriter io.Writer

	// fileSet is the go/token position table reused across every evaluation handled by this
	// Service.
	fileSet *token.FileSet

	// symbols holds pre-registered native symbols available to interpreted code via import.
	symbols *symtab.SymbolRegistry

	// globals holds package-level variables shared between successive evaluations.
	globals *engine.GlobalStore

	// config holds optional service configuration assembled from Option functions passed to
	// NewService.
	config *serviceConfig

	// pendingVarBridges accumulates one entry per non-main interpreted package compiled by
	// CompileProgram, resolved into the symbol registry once ExecuteInits has run.
	pendingVarBridges []pendingVarBridge

	// initsRun records the file sets ExecuteInits has initialised, so the following
	// ExecuteEntrypoint on the same file set does not run the initialisers a second time.
	initsRun map[*program.CompiledFileSet]struct{}

	// limits holds resource constraints threaded into each VM.
	limits engine.VMLimits

	// initsMu guards initsRun.
	initsMu sync.Mutex

	// lastCostUsed holds the total computation cost consumed by the most recent execution,
	// read via LastCostUsed.
	lastCostUsed atomic.Int64

	// features controls which Go language constructs are allowed during compilation.
	features policy.InterpFeature
}

// NewService creates a new interpreter service.
//
// The symbol registry starts empty: without WithSymbolProvider the service runs the Go
// language in full but resolves no import.
//
// Takes opts (Option variadic) which configure symbol providers, build tags, environment
// variables, and other interpreter behaviour.
//
// Returns *Service which is ready to evaluate code.
func NewService(opts ...Option) *Service {
	config := newServiceConfig()
	for _, opt := range opts {
		opt(config)
	}

	symbols := symtab.NewSymbolRegistry(nil)
	symbols.ProtectPackage(policy.PkgUnsafe)

	features := config.features
	if features == 0 {
		features = policy.InterpFeaturesAll
	}

	s := &Service{fileSet: token.NewFileSet(),
		globals:  engine.NewGlobalStore(),
		symbols:  symbols,
		config:   config,
		features: features, stderrWriter: config.stderrWriter, pendingVarBridges: nil, initsRun: nil, initsMu: sync.Mutex{}, limits: engine.VMLimits{
			CapabilityHook:           nil,
			Tracker:                  nil,
			Debug:                    nil,
			DisableMinorGC:           false,
			CallbackArenaRetireBytes: 0,
			ArenaFactory:             nil,
			CostTable:                nil,
			Diagnostics:              nil,
			MaxCallDepth:             0,
			MaxStringSize:            0,
			MaxOutputSize:            0,
			CostBudget:               0,
			MaxArenaBytes:            0,
			MaxAllocSize:             0,
			MaxGoroutines:            0,
			YieldInterval:            0,
			DeadlockGrace:            0,
			ForceGoDispatch:          false,
			SafeMode:                 false}, lastCostUsed: atomic.Int64{},
	}
	if len(config.symbolProviders) > 0 {
		s.UseSymbolProviders(config.symbolProviders...)
	}
	s.limits = s.buildLimits()
	return s
}

// SetStderr overrides the writer for print/println output. Pass nil to restore the
// default (os.Stderr).
//
// Takes writer (io.Writer) which receives print/println output, or nil to reset.
func (s *Service) SetStderr(writer io.Writer) {
	s.stderrWriter = writer
}

// Reset clears the interpreter state for reuse. The symbol registry and configuration are
// preserved.
func (s *Service) Reset() {
	s.fileSet = token.NewFileSet()
	s.globals.Reset()
}

// Clone creates a copy of the service sharing symbols but with fresh execution state.
//
// Returns *Service which is a new service with shared symbols and independent state.
func (s *Service) Clone() *Service {
	cloned := &Service{fileSet: token.NewFileSet(),
		symbols:  s.symbols,
		globals:  engine.NewGlobalStore(),
		config:   s.config,
		features: s.features, stderrWriter: nil, pendingVarBridges: nil, initsRun: nil, initsMu: sync.Mutex{}, limits: engine.VMLimits{
			CapabilityHook:           nil,
			Tracker:                  nil,
			Debug:                    nil,
			DisableMinorGC:           false,
			CallbackArenaRetireBytes: 0,
			ArenaFactory:             nil,
			CostTable:                nil,
			Diagnostics:              nil,
			MaxCallDepth:             0,
			MaxStringSize:            0,
			MaxOutputSize:            0,
			CostBudget:               0,
			MaxArenaBytes:            0,
			MaxAllocSize:             0,
			MaxGoroutines:            0,
			YieldInterval:            0,
			DeadlockGrace:            0,
			ForceGoDispatch:          false,
			SafeMode:                 false}, lastCostUsed: atomic.Int64{},
	}
	cloned.limits = cloned.buildLimits()
	return cloned
}

// SetCapabilityHook installs or replaces the CapabilityHook on this Service, accepting
// nil to revert to the permissive default. In-flight executions keep the hook they
// started with.
//
// Takes hook (CapabilityHook) which is the new hook, or nil to clear.
func (s *Service) SetCapabilityHook(hook policy.CapabilityHook) {
	if s.config == nil {
		s.config = newServiceConfig()
	}
	s.config.capabilityHook = hook
	s.limits.CapabilityHook = hook
}

// CapabilityHook returns the currently installed CapabilityHook, or nil when no hook has
// been configured. The returned value reflects the configured hook, not the permissive
// fallback used at dispatch time.
//
// Returns CapabilityHook which is the configured hook, or nil.
func (s *Service) CapabilityHook() policy.CapabilityHook {
	if s.config == nil {
		return nil
	}
	return s.config.capabilityHook
}

// LastCostUsed returns the total computation cost consumed by the most recent execution
// when cost metering is enabled.
//
// Returns int64 which is the cost consumed, or 0 when cost metering is disabled.
func (s *Service) LastCostUsed() int64 {
	return s.lastCostUsed.Load()
}

// LeakedGoroutines reports how many goroutines spawned by this Service's executions were
// still running when their execution gave up joining them. A non-zero count means the
// Service may still have interpreted code running in the background and must not be
// reused or pooled.
//
// Returns the cumulative count of goroutines that outlived their execution.
func (s *Service) LeakedGoroutines() int64 {
	if s.limits.Tracker == nil {
		return 0
	}
	return s.limits.Tracker.LeakedGoroutines.Load()
}

// serviceConfig holds optional configuration for the interpreter service.
type serviceConfig struct {
	// bytecodeStore is an optional port for persisting compiled bytecode.
	bytecodeStore program.BytecodeStorePort

	// symbolProviders holds the symbol tables interpreted code may import, in the order
	// WithSymbolProvider supplied them. Empty registers nothing.
	symbolProviders []symtab.SymbolProviderPort

	// stderrWriter is the writer WithStderr selected for print and println output; nil means
	// os.Stderr.
	stderrWriter io.Writer

	// clock is the time source interpreted code observes. Nil selects WallClock.
	clock clock.Clock

	// capabilityHook is consulted before every gated native operation. Nil means no gating.
	capabilityHook policy.CapabilityHook

	// arenaFactory is an optional factory for creating register arenas.
	arenaFactory func() *engine.RegisterArena

	// compilationSnapshotCallback is called at the end of CompileProgram with the compiled
	// output so far, regardless of whether compilation succeeded or failed partway through.
	// This enables bytecode emission for debugging even when a later package fails to
	// compile.
	compilationSnapshotCallback func(*program.CompiledFileSet)

	// env holds environment variable overrides for interpreted code: an overlay on the host
	// environment, or the whole environment when envSealed is set.
	env map[string]string

	// args is the argument vector interpreted code observes through os.Args and the flag
	// package; nil leaves the host process's own.
	args []string

	// deniedImports holds import paths a script may never import.
	deniedImports map[string]struct{}

	// allowedImports restricts which external packages a script may import.
	allowedImports map[string]struct{}

	// debugger is an optional debugger to attach to each VM.
	debugger *debug.Debugger

	// costTable is the per-opcode cost table for cost metering. Nil means use the default
	// cost table.
	costTable *isa.CostTable

	// logger receives the interpreter's warnings and errors. Nil means the process default
	// logger is resolved at each call site, so a host that calls slog.SetDefault after the
	// service is built is still honoured.
	logger *slog.Logger

	// envOnce guards one-time application of environment overrides.
	envOnce *sync.Once

	// buildTags holds additional build tags for constraint evaluation.
	buildTags []string

	// maxLiteralElements is the maximum number of elements in a single composite literal
	// (slice, array, map). Zero means no limit.
	maxLiteralElements int

	// maxExecutionTime is the maximum duration for any single evaluation.
	maxExecutionTime time.Duration

	// maxOutputSize is the maximum bytes print and println may write.
	maxOutputSize int

	// maxSourceSize is the maximum total source code size in bytes accepted for compilation.
	// Zero means no limit.
	maxSourceSize int

	// maxStringSize is the maximum string length in bytes that a concatenation may produce.
	// Zero means no limit.
	maxStringSize int

	// maxAllocSize is the maximum element count for a single allocation.
	maxAllocSize int

	// maxCallDepth is the maximum call stack depth before overflow.
	maxCallDepth int

	// maxSpecialisations caps the number of generic-function specialisations registered per
	// generic callee. Zero selects defaultMaxSpecialisations (1000).
	maxSpecialisations int

	// costBudget is the maximum total computation cost for a single execution. Zero means
	// cost metering is disabled.
	costBudget int64

	// verifierIterationLimit caps the number of dataflow iterations the bytecode verifier
	// may perform per function. Zero selects the verifier's built-in default.
	verifierIterationLimit int

	// maxArenaSizeBytes mirrors maxArenaBytes under the WithMaxArenaSizeBytes alias for
	// explicit limit naming. Zero preserves the existing arena budget setting.
	maxArenaSizeBytes uint64

	// maxExpressionDepth caps the recursion depth when compiling nested expressions. Zero
	// selects defaultMaxExpressionDepth (1024).
	maxExpressionDepth int

	// maxMethods caps the size of CompiledFunction.methodTable on the root function. Zero
	// selects defaultMaxMethods (10000).
	maxMethods int

	// maxArenaBytes caps the cumulative arena bytes per Execute. Zero selects the default.
	maxArenaBytes uint64

	// maxConstantPoolSize caps each per-function constant pool (int, float, string, bool,
	// uint, complex, general, type, call-site). Zero selects defaultMaxConstantPoolSize
	// (65535).
	maxConstantPoolSize int

	// maxGoroutines is the maximum concurrent goroutines for interpreted code.
	maxGoroutines int32

	// yieldInterval is the number of instructions between runtime.Gosched() calls for
	// cooperative scheduling.
	yieldInterval uint32

	// deadlockGrace is how long every goroutine of an execution may stay parked on program
	// channels before the execution fails with fault.ErrDeadlock; zero disables the check.
	// See engine/deadlock.go.
	deadlockGrace time.Duration

	// features controls which Go language constructs are allowed during compilation. Zero
	// value means InterpFeaturesAll.
	features policy.InterpFeature

	// optimisations selects which compiler optimisations run. newServiceConfig starts it at
	// passes.DefaultOptions (everything on); WithOptimisations replaces it.
	optimisations passes.Options

	// bytecodeVerificationDisabled opts out of the post-compilation bytecode verifier.
	// Verification is on by default; set this via WithBytecodeVerification(false) to skip
	// it.
	bytecodeVerificationDisabled bool

	// allowUnpinnedModules lets LoadModule accept a reference whose Pin is empty. Off by
	// default so a production host cannot load a bundle without an integrity check;
	// interactive and development hosts opt in via WithAllowUnpinnedModules.
	allowUnpinnedModules bool

	// debugInfo enables debug information generation during compilation.
	debugInfo bool

	// forceGoDispatch forces the pure Go dispatch loop on all architectures.
	forceGoDispatch bool

	// disableMinorGC keeps the register arena's minor collection from running (the
	// WithMinorGCDisabled option), a diagnostic knob for bisecting collector faults.
	disableMinorGC bool

	// safeMode enables per-instruction runtime guards for untrusted code by routing
	// execution onto the pure Go dispatch loop instead of the ASM fast path.
	safeMode bool

	// envConfigured records whether WithEnv or WithSealedEnv configured the environment at
	// construction. It is immutable after construction, so the per-eval install gate can
	// consult it without racing the interpreted Setenv/Unsetenv writes that mutate the env
	// map under its own lock.
	envConfigured bool

	// envSealed makes env the entire environment instead of an overlay on the host's.
	envSealed bool
}

// newServiceConfig returns a serviceConfig with its one-time guards initialised.
//
// Returns *serviceConfig which has its envOnce guard ready for use.
func newServiceConfig() *serviceConfig {
	config := &serviceConfig{
		bytecodeStore:                nil,
		symbolProviders:              nil,
		stderrWriter:                 nil,
		clock:                        nil,
		capabilityHook:               nil,
		arenaFactory:                 nil,
		compilationSnapshotCallback:  nil,
		env:                          nil,
		envSealed:                    false,
		args:                         nil,
		deniedImports:                nil,
		allowedImports:               nil,
		debugger:                     nil,
		costTable:                    nil,
		logger:                       nil,
		envOnce:                      nil,
		buildTags:                    nil,
		maxLiteralElements:           0,
		maxExecutionTime:             0,
		maxOutputSize:                0,
		maxSourceSize:                0,
		maxStringSize:                0,
		maxAllocSize:                 0,
		maxCallDepth:                 0,
		maxSpecialisations:           0,
		costBudget:                   0,
		verifierIterationLimit:       0,
		maxArenaSizeBytes:            0,
		maxExpressionDepth:           0,
		maxMethods:                   0,
		maxArenaBytes:                0,
		maxConstantPoolSize:          0,
		maxGoroutines:                0,
		yieldInterval:                0,
		deadlockGrace:                defaultDeadlockGrace,
		features:                     0,
		optimisations:                passes.DefaultOptions(),
		bytecodeVerificationDisabled: false,
		allowUnpinnedModules:         false,
		debugInfo:                    false,
		forceGoDispatch:              false,
		disableMinorGC:               false,
		safeMode:                     false,
		envConfigured:                false,
	}
	config.envOnce = new(sync.Once)
	return config
}

// processOverridesConfigured reports whether interpreted code sees an environment or
// argument vector other than the host process's, so the os and flag packages need
// patching.
//
// Returns bool which is true when WithEnv, WithSealedEnv or WithArgs was applied.
func (c *serviceConfig) processOverridesConfigured() bool {
	return c.envConfigured || c.args != nil
}
