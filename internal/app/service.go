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
	"context"
	"errors"
	"fmt"
	"go/token"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"pipit.sh/pipit/internal/buildfilter"
	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/debug"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
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

// errServiceClosed is returned by every compile and execution on a closed service.
var errServiceClosed = fmt.Errorf("%w: the interpreter service is closed", policy.ErrClosed)

// ErrInvalidOption reports a service option given a value it does not accept, such as a
// limit that is not positive.
var ErrInvalidOption = fmt.Errorf("invalid service option: %w", policy.ErrInvalidConfig)

// Option configures the interpreter service.
type Option func(*serviceConfig)

// Service is the public entry point for the bytecode interpreter. It parses, type-checks,
// compiles, and executes Go source code.
//
// Safe for concurrent reads of the symbol registry after setup. All other state requires
// a distinct Service per concurrent evaluation, typically via Clone.
type Service struct {
	// constructionLog keeps what construction and UseSymbolProviders log where no caller's
	// context exists, until the next call that has one replays it (bindLogger).
	constructionLog *logging.Deferred

	// constructionErr holds the symbol providers the options named that were rejected at
	// construction, until the first call that binds a context reports and returns it.
	constructionErr error

	// stderrWriter overrides the writer for print/println output; when nil, VMs fall back to
	// os.Stderr, and REPL and notebook hosts set this to capture output for display in their
	// own UI.
	stderrWriter io.Writer

	// initsRun records, per file set ExecuteInits has initialised, whether the following
	// ExecuteEntrypoint must skip the initialisers. Keys are weak so the file set can be
	// collected; a cleanup on it removes its entry, and Close drops them all.
	initsRun map[weak.Pointer[program.CompiledFileSet]]bool

	// symbols holds pre-registered native symbols available to interpreted code via import.
	symbols *symtab.SymbolRegistry

	// namedCompositeQuota is charged for every new named composite type the service's
	// programs add, in every registry the service builds. Clones share it with the symbols.
	namedCompositeQuota *typemodel.NamedCompositeQuota

	// globals holds package-level variables shared between successive evaluations.
	globals *engine.GlobalStore

	// config holds optional service configuration assembled from Option functions passed to
	// NewService.
	config *serviceConfig

	// fileSet is the go/token position table reused across every evaluation handled by this
	// Service.
	fileSet *token.FileSet

	// borrowDeclarations holds the host's declared borrow classes, frozen by the first
	// evaluation; shared with clones, which share the symbols.
	borrowDeclarations *engine.BorrowDeclarations

	// pendingVarBridges accumulates one entry per non-main interpreted package compiled by
	// CompileProgram, resolved into the symbol registry once ExecuteInits has run.
	pendingVarBridges []pendingVarBridge

	// varInitReuse holds the global reuse state, including each file set's initialiser image
	// and watch.
	varInitReuse varInitReuseRegistry

	// loadedGlobals tracks the slots programs loaded from bytecode hold, the regions their
	// unloads freed, and the interface names loads registered.
	loadedGlobals loadedGlobalRegistry

	// limits holds resource constraints threaded into each VM.
	limits engine.VMLimits

	// lastCostUsed holds the total computation cost consumed by the most recent execution,
	// read via LastCostUsed.
	lastCostUsed atomic.Int64

	// initsMu guards initsRun.
	initsMu sync.Mutex

	// constructionMu guards constructionErr.
	constructionMu sync.Mutex

	// features controls which Go language constructs are allowed during compilation.
	features policy.InterpFeature

	// closed is set by Close; every later compile and execution returns errServiceClosed.
	closed atomic.Bool
}

// namedCompositeConfig holds the caps on named array, slice, map, channel and function
// types a service's options set.
type namedCompositeConfig struct {
	// maxTypes caps the new types the service's programs may add
	// (WithMaxNamedCompositeTypes). Zero selects typemodel.DefaultNamedCompositeQuota.
	maxTypes int

	// processLimit is the process-wide cap WithNamedCompositeProcessLimit sets when the
	// service is built. Zero leaves the cap as it is.
	processLimit int
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

	// invalidOptions joins an ErrInvalidOption error for every option given a value it does
	// not accept.
	invalidOptions error

	// envOnce guards one-time application of environment overrides.
	envOnce *sync.Once

	// fileMatcher selects source files using the configured build tags.
	fileMatcher *buildfilter.Matcher

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

	// maxLoadedNamedInterfaces caps the distinct interface names loaded bytecode may
	// register. Zero selects defaultMaxLoadedNamedInterfaces.
	maxLoadedNamedInterfaces int

	// namedComposites holds the caps on named composite types the options set.
	namedComposites namedCompositeConfig

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

	// callbackTimeout bounds each run of an interpreted closure native code calls; zero
	// leaves callbacks unbounded beyond the wrapping execution's context.
	callbackTimeout time.Duration

	// finaliserTimeout bounds each run of a guest finaliser or cleanup
	// (WithFinaliserTimeout). Zero selects the engine's default.
	finaliserTimeout time.Duration

	// abortUnwindCost is the total cost a stopped VM's pending defers may spend
	// (WithAbortUnwindCost); zero selects the engine's default.
	abortUnwindCost int64

	// abortUnwindWall is the total wall time a stopped VM's pending defers may spend
	// (WithAbortUnwindWall); zero selects the engine's default.
	abortUnwindWall time.Duration

	// abortUnwindDeferWallMax caps one deferred call's wall time during an unwinding
	// (WithAbortUnwindDeferWallMax); zero selects the engine's default.
	abortUnwindDeferWallMax time.Duration

	// abortUnwindNativeGrace is how long a deferred call may stay in native code past its
	// deadline (WithAbortUnwindNativeGrace); zero selects the engine's default.
	abortUnwindNativeGrace time.Duration

	// goroutineJoinUnwindCap bounds the join's wait for unwinding VMs
	// (WithGoroutineJoinUnwindCap); zero selects the engine's default.
	goroutineJoinUnwindCap time.Duration

	// leakedUnwinderCap is how many unwindings left blocked in native code the service
	// allows (WithLeakedUnwinderCap); zero selects the engine's default.
	leakedUnwinderCap int64

	// lockWalkMaxValues caps the values the abandoned-lock check visits
	// (WithLockWalkMaxValues); zero selects the engine's default.
	lockWalkMaxValues int

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

	// licenceCheckerStrict makes a licence checker defect fail the compile instead of
	// running the program abandoned (withLicenceCheckerStrict).
	licenceCheckerStrict bool
}

// NewService creates a new interpreter service.
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
	config.fileMatcher = buildfilter.New(config.buildTags)

	if config.namedComposites.processLimit > 0 {
		typemodel.SetNamedCompositeProcessLimit(config.namedComposites.processLimit)
	}
	namedCompositeQuota := typemodel.NewNamedCompositeQuota(config.namedComposites.maxTypes)
	symbols := symtab.NewSymbolRegistry(nil)
	symbols.SetNamedCompositeQuota(namedCompositeQuota)
	symbols.ProtectPackage(policy.PkgUnsafe)

	features := config.features
	if features == 0 {
		features = policy.InterpFeaturesAll
	}

	service := &Service{
		constructionLog:     logging.NewDeferred(),
		constructionErr:     nil,
		constructionMu:      sync.Mutex{},
		stderrWriter:        config.stderrWriter,
		initsRun:            nil,
		symbols:             symbols,
		namedCompositeQuota: namedCompositeQuota,
		globals:             engine.NewGlobalStore(),
		config:              config,
		fileSet:             token.NewFileSet(),
		borrowDeclarations:  engine.NewBorrowDeclarations(),
		pendingVarBridges:   nil,
		varInitReuse:        varInitReuseRegistry{},
		loadedGlobals:       loadedGlobalRegistry{},
		limits:              engine.VMLimits{},
		lastCostUsed:        atomic.Int64{},
		initsMu:             sync.Mutex{},
		features:            features,
		closed:              atomic.Bool{},
	}
	service.useConfiguredSymbolProviders()
	service.limits = service.buildLimits()
	runtime.AddCleanup(service, engine.StopGlobalReuseWatch, service.globals)
	return service
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
//
// Programs loaded from bytecode lose their package-variable slots, so running one
// afterwards returns ErrProgramUnloaded. Load it again instead.
func (s *Service) Reset() {
	s.fileSet = token.NewFileSet()
	s.forgetVariableInitImages()
	s.invalidateLoadedPrograms()
	s.globals.Reset()
}

// Close releases what the service holds beyond its own memory.
//
// It stops initialiser watches, drops file set reuse state and ExecuteInits records, and
// forgets loaded programs' slots. The finalisers and cleanups its programs registered
// stop as well.
//
// Returns error which is always nil.
func (s *Service) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.dropAllVariableInitStates()
	s.forgetInitsRun()
	s.invalidateLoadedPrograms()
	s.globals.CloseFinalisers()
	return nil
}

// Clone creates a copy of the service sharing symbols but with fresh execution state.
//
// Returns *Service which is a new service with shared symbols and independent state.
func (s *Service) Clone() *Service {
	cloned := &Service{fileSet: token.NewFileSet(), constructionLog: logging.NewDeferred(), constructionErr: nil, constructionMu: sync.Mutex{},
		symbols:             s.symbols,
		namedCompositeQuota: s.namedCompositeQuota,
		globals:             engine.NewGlobalStore(),
		config:              s.config,
		borrowDeclarations:  s.borrowDeclarations,
		features:            s.features, stderrWriter: nil, pendingVarBridges: nil, initsRun: nil, initsMu: sync.Mutex{}, limits: engine.VMLimits{
			CapabilityHook:           nil,
			Tracker:                  nil,
			Debug:                    nil,
			DisableMinorGC:           false,
			AbandonArenasOnRelease:   false,
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
			CallbackTimeout:          0,
			AbortUnwindCost:          0,
			AbortUnwindWall:          0,
			AbortUnwindDeferWallMax:  0,
			AbortUnwindNativeGrace:   0,
			GoroutineJoinUnwindCap:   0,
			LeakedUnwinderCap:        0,
			LockWalkMaxValues:        0,
			ForceGoDispatch:          false,
			FinaliserTimeout:         0,
			BorrowDeclarations:       nil,
			SafeMode:                 false}, lastCostUsed: atomic.Int64{},
		varInitReuse:  varInitReuseRegistry{},
		loadedGlobals: loadedGlobalRegistry{},
		closed:        atomic.Bool{},
	}
	cloned.limits = cloned.buildLimits()
	runtime.AddCleanup(cloned, engine.StopGlobalReuseWatch, cloned.globals)
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

// AbandonedLocks reports locks left held by the service's stopped executions.
//
// Returns the cumulative count of abandoned locks.
func (s *Service) AbandonedLocks() int64 {
	if s.limits.Tracker == nil {
		return 0
	}
	return s.limits.Tracker.AbandonedLocks.Load()
}

// newServiceConfig returns a serviceConfig with its one-time guards initialised.
//
// Returns *serviceConfig which has its envOnce guard ready for use.
func newServiceConfig() *serviceConfig {
	return &serviceConfig{
		bytecodeStore:                nil,
		maxLoadedNamedInterfaces:     0,
		namedComposites:              namedCompositeConfig{maxTypes: 0, processLimit: 0},
		finaliserTimeout:             0,
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
		invalidOptions:               nil,
		envOnce:                      new(sync.Once),
		buildTags:                    nil,
		fileMatcher:                  nil,
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
		callbackTimeout:              0,
		abortUnwindCost:              0,
		abortUnwindWall:              0,
		abortUnwindDeferWallMax:      0,
		abortUnwindNativeGrace:       0,
		goroutineJoinUnwindCap:       0,
		leakedUnwinderCap:            0,
		lockWalkMaxValues:            0,
		features:                     0,
		optimisations:                passes.DefaultOptions(),
		bytecodeVerificationDisabled: false,
		allowUnpinnedModules:         false,
		debugInfo:                    false,
		forceGoDispatch:              false,
		disableMinorGC:               false,
		safeMode:                     false,
		envConfigured:                false,
		licenceCheckerStrict:         licenceCheckerStrictDefault.Load(),
	}
}

// processOverridesConfigured reports whether interpreted code sees an environment or
// argument vector other than the host process's, so the os and flag packages need
// patching.
//
// Returns bool which is true when WithEnv, WithSealedEnv or WithArgs was applied.
func (c *serviceConfig) processOverridesConfigured() bool {
	return c.envConfigured || c.args != nil
}

// useConfiguredSymbolProviders registers the symbol providers the options named. The
// types packages it rejected are recorded, and the first call that binds a context
// reports them through that context's logger and returns them (bindLogger).
//
// Concurrent access is guarded by a mutex. The service mutex guards the construction
// error.
func (s *Service) useConfiguredSymbolProviders() {
	if len(s.config.symbolProviders) == 0 {
		return
	}
	if err := s.UseSymbolProviders(s.config.symbolProviders...); err != nil {
		s.constructionMu.Lock()
		s.constructionErr = errors.Join(s.constructionErr, err)
		s.constructionMu.Unlock()
	}
}

// bindLogger binds the service's logger into ctx as the fallback for a ctx that carries
// none, replays what was logged without a context (constructionLog), and takes the
// construction error, logging it once.
//
// Returns context.Context which carries the logger.
// Returns error which is the rejected symbol providers on the first call after
// construction, or nil.
//
// Concurrent access is guarded by a mutex. The service mutex guards replay and removal of
// construction diagnostics.
func (s *Service) bindLogger(ctx context.Context) (context.Context, error) {
	ctx, logger := logging.From(logging.WithFallbackLogger(ctx, s.config.logger))
	if s.constructionLog != nil {
		s.constructionLog.Replay(ctx)
	}
	s.constructionMu.Lock()
	err := s.constructionErr
	s.constructionErr = nil
	s.constructionMu.Unlock()
	if err != nil {
		logger.WarnContext(ctx, "A symbol provider's types packages could not all be registered; importing those fails", logging.ErrAttr(err))
		return ctx, fmt.Errorf("app: registering symbol providers: %w", err)
	}
	return ctx, nil
}
