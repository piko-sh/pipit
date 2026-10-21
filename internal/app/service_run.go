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
	"go/ast"
	"go/parser"
	"reflect"
	"runtime"
	"sync/atomic"
	"weak"

	"pipit.sh/pipit/internal/clock"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

// debugExecutionKey marks a context as belonging to an execution the debugger was told
// about.
type debugExecutionKey struct{}

// Eval evaluates Go source code and returns the result.
//
// The source can be a single expression, a statement, or a complete Go source file. For
// expressions, the expression's value is returned. For statements, nil is returned.
//
// Takes code (string) which contains the Go source code to evaluate.
//
// Returns any which is the result of evaluating the code.
// Returns error when parsing, type-checking, compilation, or execution fails.
func (s *Service) Eval(ctx context.Context, code string) (any, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if err := s.checkEvalSource(ctx, code); err != nil {
		return nil, err
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return nil, bindErr
	}

	s.applyEnvOverrides()

	if expression, err := parser.ParseExprFrom(s.fileSet, evalFileName, code, 0); err == nil {
		if result, done, evalErr := s.evalAsExpression(ctx, code, expression); done {
			return result, evalErr
		}
	}

	wrappedCode := "package main\nfunc _eval_() {\n" + code + "\n}"
	file, parseErr := parser.ParseFile(s.fileSet, evalFileName, wrappedCode, 0)
	if parseErr == nil {
		result, evalErr := s.doEvalFile(ctx, file)
		if evalErr == nil {
			return result, nil
		}

		if errors.Is(evalErr, fault.ErrTypeCheck) {
			if mixedResult, mixedErr := s.evalMixed(ctx, code); mixedErr == nil {
				return mixedResult, nil
			}
		}
		return nil, engine.NewSafeError(safeMessageEvaluationFailed, fmt.Errorf("evaluating expression: %w", evalErr))
	}

	result, evalErr := s.evalMixed(ctx, code)
	if evalErr != nil {
		return nil, engine.NewSafeError(safeMessageEvaluationFailed, fmt.Errorf("evaluating expression: %w", evalErr))
	}
	return result, nil
}

// Execute runs a pre-compiled function and returns its result.
//
// Takes compiledFunction (*CompiledFunction) which is the compiled function to run.
//
// Returns any which is the result of executing the function.
// Returns error when execution fails.
func (s *Service) Execute(ctx context.Context, compiledFunction *program.CompiledFunction) (any, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return nil, bindErr
	}

	ctx, endDebug := s.beginDebugExecution(ctx)

	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleEntrypoint)
	defer vm.Recycle()
	defer cancelExecution(fault.ErrMainReturned)
	result, err := vm.Execute(compiledFunction)
	s.recordCost(vm)
	endDebug(err)
	return result, err
}

// EvalFile parses a complete Go source file, compiles all declarations, and runs the
// named entrypoint function. This wraps CompileFileSet + ExecuteEntrypoint for
// single-file use.
//
// The source must be a valid Go file with a package clause. The entrypoint must name a
// function declared in the file.
//
// Takes source (string) which is the complete Go source file.
// Takes entrypoint (string) which is the function name to execute.
//
// Returns any which is the entrypoint function's return value.
// Returns error when parsing, type-checking, compilation, or execution fails.
func (s *Service) EvalFile(ctx context.Context, source string, entrypoint string) (any, error) {
	if err := s.checkOpen(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()

	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return nil, bindErr
	}

	cfs, err := s.CompileFileSet(ctx, map[string]string{"main.go": source})
	if err != nil {
		return nil, fmt.Errorf("evaluating file: %w", err)
	}
	result, entryErr := s.ExecuteEntrypoint(ctx, cfs, entrypoint)
	if entryErr != nil {
		return nil, fmt.Errorf("evaluating file: %w", entryErr)
	}
	return result, nil
}

// ExecuteEntrypoint runs a named function from a pre-compiled file set, executing init
// functions first (in source order) before the entrypoint.
//
// Takes cfs (*CompiledFileSet) which is the compiled file set.
// Takes entrypoint (string) which is the function name to execute.
//
// Returns any which is the entrypoint function's return value.
// Returns error when the entrypoint is not found or execution fails.
func (s *Service) ExecuteEntrypoint(ctx context.Context, cfs *program.CompiledFileSet, entrypoint string) (result any, err error) {
	ctx, openErr := s.openForCall(ctx)
	if openErr != nil {
		return nil, openErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, ctxErr)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	ctx, endDebug := s.beginDebugExecution(ctx)
	defer func() { endDebug(err) }()

	index, ok := cfs.Entrypoints()[entrypoint]
	if !ok {
		return nil, fmt.Errorf("%w: %q", fault.ErrEntrypointNotFound, entrypoint)
	}
	endRun, err := s.beginLoadedRun(cfs)
	if err != nil {
		return nil, err
	}
	defer endRun()
	endExecution := s.beginExecution()
	defer endExecution()

	if !s.takeInitsRun(cfs) {
		if !s.skipVariableInits(cfs) {
			initErr := s.runVariableInits(ctx, cfs)
			s.noteVariableInitsRan(cfs, initErr == nil)
			if initErr != nil {
				return nil, initErr
			}
		}

		for _, initIndex := range cfs.InitFunctions() {
			if initErr := s.executeInitFunction(ctx, cfs.Root(), program.ExportFunctions(cfs.Root())[initIndex]); initErr != nil {
				return nil, fmt.Errorf("executing entrypoint: %w", initErr)
			}
		}
	}

	s.finalisePendingVarBridges()
	return s.runEntrypointFunction(ctx, cfs, program.ExportFunctions(cfs.Root())[index], nil)
}

// CallFunction runs a named top-level function of a pre-compiled file set with
// host-supplied arguments.
//
// Takes cfs (*CompiledFileSet) which is the compiled file set.
// Takes name (string) which is the function name to execute.
// Takes arguments (...any) which supply one value per declared parameter; a nil argument
// becomes the zero interface value.
//
// Returns the function's first return value and any execution error.
func (s *Service) CallFunction(ctx context.Context, cfs *program.CompiledFileSet, name string, arguments ...any) (result any, err error) {
	if closedErr := s.checkOpen(); closedErr != nil {
		return nil, closedErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, ctxErr)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return nil, bindErr
	}
	ctx, endDebug := s.beginDebugExecution(ctx)
	defer func() { endDebug(err) }()

	index, ok := cfs.Entrypoints()[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", fault.ErrEntrypointNotFound, name)
	}
	endRun, err := s.beginLoadedRun(cfs)
	if err != nil {
		return nil, err
	}
	defer endRun()
	endExecution := s.beginExecution()
	defer endExecution()
	values := make([]reflect.Value, len(arguments))
	for i, argument := range arguments {
		if argument == nil {
			values[i] = reflect.Zero(reflect.TypeFor[any]())
			continue
		}
		values[i] = reflect.ValueOf(argument)
	}
	s.finalisePendingVarBridges()
	return s.runEntrypointFunction(ctx, cfs, program.ExportFunctions(cfs.Root())[index], values)
}

// DeclareTopLevelCalls declares that the host calls function values received from cfs's
// program only at top level, including values registered by its init functions.
//
// Takes cfs (*CompiledFileSet) which is the compiled program.
func (*Service) DeclareTopLevelCalls(cfs *program.CompiledFileSet) {
	if cfs != nil {
		cfs.Root().DeclareHostTopLevelCalls()
	}
}

// ExecuteInits runs variable initialisers and init functions from a pre-compiled file set
// without requiring a named entrypoint. This is useful when the compiled code only needs
// its init side-effects (such as registering functions into a global registry).
//
// Takes cfs (*CompiledFileSet) which is the compiled file set whose init functions are to
// execute.
//
// Returns error when a variable initialiser or init function fails.
func (s *Service) ExecuteInits(ctx context.Context, cfs *program.CompiledFileSet) (err error) {
	if closedErr := s.checkOpen(); closedErr != nil {
		return closedErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, ctxErr)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	var bindErr error
	if ctx, bindErr = s.bindLogger(ctx); bindErr != nil {
		return bindErr
	}
	ctx, endDebug := s.beginDebugExecution(ctx)
	defer func() { endDebug(err) }()
	defer engine.RecoverArenaBudgetError(&err)
	defer func() {
		if err == nil {
			s.markInitsRun(cfs)
		}
	}()

	endRun, err := s.beginLoadedRun(cfs)
	if err != nil {
		return err
	}
	defer endRun()
	endExecution := s.beginExecution()
	defer endExecution()
	initErr := s.runVariableInits(ctx, cfs)
	s.noteVariableInitsRan(cfs, initErr == nil)
	if initErr != nil {
		return initErr
	}

	for _, initIndex := range cfs.InitFunctions() {
		function := program.ExportFunctions(cfs.Root())[initIndex]
		if err := s.executeInitFunction(ctx, cfs.Root(), function); err != nil {
			return fmt.Errorf("executing init functions: %w", err)
		}
	}

	s.finalisePendingVarBridges()
	return nil
}

// openForCall is checkOpen followed by bindLogger, for an entry point that takes a
// context.
//
// Returns context.Context which carries the logger.
// Returns error which is checkOpen's or bindLogger's.
func (s *Service) openForCall(ctx context.Context) (context.Context, error) {
	if err := s.checkOpen(); err != nil {
		return ctx, err
	}
	return s.bindLogger(ctx)
}

// checkOpen reports whether the service may still compile and execute.
//
// Returns error which is errServiceClosed once Close has run, the ErrInvalidOption errors
// of the options the service was built with that were given values they do not accept, or
// nil.
func (s *Service) checkOpen() error {
	if s.closed.Load() {
		return errServiceClosed
	}
	if s.config != nil && s.config.invalidOptions != nil {
		return s.config.invalidOptions
	}
	return nil
}

// applyClockOverrides replaces the stdlib time package's Now, Since, Until, Sleep,
// NewTimer, and NewTicker symbols with clock-dispatched versions when a non-default Clock
// has been configured. No-op when the configured clock is nil or WallClock.
func (s *Service) applyClockOverrides() {
	if s.config == nil || s.config.clock == nil {
		return
	}
	if s.config.clock == clock.WallClock {
		return
	}
	overrides := clock.ClockOverrideSymbols(s.config.clock)
	if s.symbols == nil {
		s.symbols = symtab.NewSymbolRegistry(symtab.SymbolExports{"time": overrides})
		s.symbols.SetNamedCompositeQuota(s.namedCompositeQuota)
		s.symbols.ProtectPackage(policy.PkgUnsafe)
		return
	}
	s.symbols.OverlayPackage("time", overrides)
}

// applyVMOutputs propagates Service-level output settings to a freshly constructed VM.
//
// Takes vm (*VM) which is the VM to configure.
func (s *Service) applyVMOutputs(vm *engine.VM) {
	if s.stderrWriter != nil {
		vm.SetStderrWriter(s.stderrWriter)
	}
}

// runVariableInits executes the variable-initialisation function of a compiled file set
// when present, on a heap-data arena (engine.VM.EnsureVarInitCallStack) whose data
// allocations come from the Go heap and are never reused, then copies any strings the
// globals still hold in arena memory out before it is released.
//
// No-op when the file set has no package-level variables.
//
// Takes cfs (*CompiledFileSet) which provides root functions and the optional variable
// initialiser.
//
// Returns error when the initialiser fails to execute.
func (s *Service) runVariableInits(ctx context.Context, cfs *program.CompiledFileSet) (err error) {
	if cfs.VariableInitFunction() == nil {
		return nil
	}
	defer engine.RecoverArenaBudgetError(&err)
	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleVarInit)
	defer vm.Recycle()
	defer cancelExecution(fault.ErrMainReturned)
	vm.BindFileSet(program.ExportFunctions(cfs.Root()), cfs.Root())
	vm.EnsureVarInitCallStack()
	defer vm.ReleaseArena()
	defer vm.FinishWatcher()
	vm.PushFrame(cfs.VariableInitFunction())
	if _, runErr := vm.RunGuarded(0); runErr != nil {
		return fmt.Errorf(errVarinitFmt, runErr)
	}
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil {
		return fmt.Errorf(errVarinitFmt, joinErr)
	}
	if info := vm.TakeGoroutinePanic(); info != nil {
		return fmt.Errorf(errVarinitFmt, engine.NewGoroutinePanicError(info.Value()))
	}
	vm.MaterialiseGlobalStrings()
	return nil
}

// runEntrypointFunction executes the resolved entrypoint via the dispatched runner,
// configuring the VM with the register arena and ASM call-info tables. A recovered
// arena-budget panic is converted to an error; any other recovered panic is allowed to
// propagate.
//
// Takes cfs (*CompiledFileSet) which provides root functions.
// Takes entrypointFunction (*CompiledFunction) which is the function to run.
// Takes arguments ([]reflect.Value) which are bound to the function's parameters; nil for
// a function that takes none.
//
// Returns any which is the entrypoint's return value.
// Returns error when execution fails or a goroutine panic surfaces.
func (s *Service) runEntrypointFunction(
	ctx context.Context,
	cfs *program.CompiledFileSet,
	entrypointFunction *program.CompiledFunction,
	arguments []reflect.Value,
) (result any, err error) {
	defer func() {
		if budgetErr := engine.ClassifyRecoveredPanic(recover()); budgetErr != nil {
			result = nil
			err = budgetErr
		}
	}()
	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleEntrypoint)
	defer vm.Recycle()
	defer cancelExecution(fault.ErrMainReturned)
	defer vm.FinishWatcher()
	arena := vm.PrepareForFileSet(cfs, entrypointFunction)
	defer func() {
		_ = vm.JoinSpawnedGoroutines()
		vm.ReleaseFileSetArena(arena)
	}()
	vm.PushFrame(entrypointFunction)
	if len(arguments) > 0 || len(entrypointFunction.ParameterKinds) > 0 {
		if bindErr := vm.AssignFrameArguments(arguments); bindErr != nil {
			return nil, bindErr
		}
	}
	result, err = vm.RunDispatchedGuarded(0)
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil && err == nil {
		err = joinErr
	}
	s.recordCost(vm)
	result = engine.MaterialiseAnyForArena(arena, result)
	engine.MaterialiseHostArguments(arena, arguments)

	vm.EvalAllResults = nil
	if info := vm.TakeGoroutinePanic(); info != nil && err == nil {
		err = engine.NewGoroutinePanicError(info.Value())
		result = nil
	}
	return result, err
}

// newExecutionVM creates a VM for a top-level execution, from a recycled shell when one
// is parked.
//
// Takes role (engine.DebugRole) which labels the execution phase for the debugger.
//
// Returns the configured VM and the cancel function the caller must defer. A caller that
// owns the VM to the end defers vm.Recycle() first, so it runs after every other use.
func (s *Service) newExecutionVM(ctx context.Context, role engine.DebugRole) (*engine.VM, context.CancelCauseFunc) {
	executionCtx, cancel := engine.NewExecutionContext(ctx)
	s.borrowDeclarations.Freeze()
	vm := engine.NewVM(executionCtx, s.globals, s.symbols)
	vm.Limits = s.limits
	vm.SetDebugRole(role)
	vm.SetExecutionCancel(cancel)
	s.applyVMOutputs(vm)
	vm.SeedExecutionBudgets()
	return vm, cancel
}

// debugSession returns the attached debugger's engine session, or nil.
//
// Returns *engine.DebugSession which every VM the service creates registers with.
func (s *Service) debugSession() *engine.DebugSession {
	if s.config == nil || s.config.debugger == nil {
		return nil
	}
	return s.config.debugger.Session()
}

// beginDebugExecution marks the start of a top-level execution for the attached debugger,
// so it can report when the execution ends. Nested executions started under the same
// context (an entrypoint's variable initialisers and init functions) share the outer
// mark, and a service without a debugger pays nothing.
//
// Returns context.Context which carries the mark.
// Returns func(error) which the caller invokes with the execution's error when it ends.
func (s *Service) beginDebugExecution(ctx context.Context) (context.Context, func(error)) {
	if s.config == nil || s.config.debugger == nil || ctx.Value(debugExecutionKey{}) != nil {
		return ctx, func(error) {}
	}
	end := s.config.debugger.BeginExecution()
	return context.WithValue(ctx, debugExecutionKey{}, struct{}{}), end
}

// markInitsRun records that ExecuteInits ran the file set's variable initialisers and
// init functions, so the next ExecuteEntrypoint on it does not run them again.
//
// The record does not keep the file set alive: the first record for a file set registers
// a cleanup that removes it once the file set is collected. A closed service records
// nothing.
//
// Takes cfs (*program.CompiledFileSet) which was initialised.
//
// Concurrency: safe for concurrent use; acquires s.initsMu.
func (s *Service) markInitsRun(cfs *program.CompiledFileSet) {
	key := weak.Make(cfs)
	s.initsMu.Lock()
	defer s.initsMu.Unlock()
	if s.closed.Load() {
		return
	}
	if s.initsRun == nil {
		s.initsRun = make(map[weak.Pointer[program.CompiledFileSet]]bool)
	}
	if _, known := s.initsRun[key]; !known {
		runtime.AddCleanup(cfs, dropInitsRun, fileSetRecordOwner{service: weak.Make(s), fileSet: key})
	}
	s.initsRun[key] = true
}

// takeInitsRun reports whether ExecuteInits already initialised the file set, clearing
// the record so a later ExecuteEntrypoint runs the initialisers again as before.
//
// Takes cfs (*program.CompiledFileSet) which is about to run.
//
// Returns bool which is true when the initialisers must be skipped.
//
// Concurrency: safe for concurrent use; acquires s.initsMu.
func (s *Service) takeInitsRun(cfs *program.CompiledFileSet) bool {
	key := weak.Make(cfs)
	s.initsMu.Lock()
	defer s.initsMu.Unlock()
	if !s.initsRun[key] {
		return false
	}
	s.initsRun[key] = false
	return true
}

// forgetInitsRun drops every ExecuteInits record; Close calls it.
//
// Concurrency: safe for concurrent use; acquires s.initsMu.
func (s *Service) forgetInitsRun() {
	s.initsMu.Lock()
	defer s.initsMu.Unlock()
	s.initsRun = nil
}

// dropInitsRun is the cleanup that removes a collected file set's ExecuteInits record.
//
// Takes owner (fileSetRecordOwner) which names the service and the file set.
//
// Concurrency: Runs on the runtime's cleanup goroutine and takes the service's initsMu.
func dropInitsRun(owner fileSetRecordOwner) {
	service := owner.service.Value()
	if service == nil {
		return
	}
	service.initsMu.Lock()
	defer service.initsMu.Unlock()
	delete(service.initsRun, owner.fileSet)
}

// buildLimits constructs VMLimits from the service configuration. Each call creates a
// fresh ResourceTracker so that cloned services and separate evaluations do not share
// counters.
//
// Returns VMLimits configured from the service settings.
func (s *Service) buildLimits() engine.VMLimits {
	limits := engine.VMLimits{MaxAllocSize: defaultMaxAllocSize,
		Debug:                    s.debugSession(),
		MaxGoroutines:            policy.DefaultMaxGoroutines,
		MaxOutputSize:            defaultMaxOutputSize,
		DisableMinorGC:           s.config.disableMinorGC,
		AbandonArenasOnRelease:   false,
		CallbackArenaRetireBytes: 0,
		CapabilityHook:           nil,
		Tracker:                  nil,
		ArenaFactory:             nil,
		CostTable:                nil,
		Diagnostics:              nil,
		MaxCallDepth:             0,
		MaxStringSize:            0,
		CostBudget:               0,
		MaxArenaBytes:            0,
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
		SafeMode:                 false}
	if s.config != nil {
		s.applyConfigLimits(&limits)
	}
	limits.Tracker = &engine.ResourceTracker{
		GoroutineCount:        atomic.Int32{},
		GoroutinesUnscheduled: atomic.Int32{},
		CostRemaining:         atomic.Int64{},
		ArenaBytes:            atomic.Int64{},
		LeakedGoroutines:      atomic.Int64{},
	}
	limits.Diagnostics = &engine.FastPathDiagnostics{}
	limits.BorrowDeclarations = s.borrowDeclarations
	return limits
}

// applyConfigLimits copies non-zero config values into the given VMLimits, enabling cost
// accounting and yield when configured.
//
// Takes limits (*VMLimits) which is the limits struct to populate from the service
// config.
func (s *Service) applyConfigLimits(limits *engine.VMLimits) {
	limits.ArenaFactory = s.config.arenaFactory
	limits.MaxCallDepth = s.config.maxCallDepth
	limits.DeadlockGrace = s.config.deadlockGrace
	limits.CallbackTimeout = s.config.callbackTimeout
	limits.FinaliserTimeout = s.config.finaliserTimeout
	limits.AbortUnwindCost = s.config.abortUnwindCost
	limits.AbortUnwindWall = s.config.abortUnwindWall
	limits.AbortUnwindDeferWallMax = s.config.abortUnwindDeferWallMax
	limits.AbortUnwindNativeGrace = s.config.abortUnwindNativeGrace
	limits.GoroutineJoinUnwindCap = s.config.goroutineJoinUnwindCap
	limits.LeakedUnwinderCap = s.config.leakedUnwinderCap
	limits.LockWalkMaxValues = s.config.lockWalkMaxValues
	limits.ForceGoDispatch = s.config.forceGoDispatch
	limits.SafeMode = s.config.safeMode
	limits.DisableMinorGC = s.config.disableMinorGC
	if s.config.safeMode {
		limits.ForceGoDispatch = true
	}
	if s.config.maxAllocSize > 0 {
		limits.MaxAllocSize = s.config.maxAllocSize
	}
	if s.config.maxGoroutines > 0 {
		limits.MaxGoroutines = s.config.maxGoroutines
	}
	if s.config.maxOutputSize > 0 {
		limits.MaxOutputSize = s.config.maxOutputSize
	}
	if s.config.costBudget > 0 {
		limits.CostBudget = s.config.costBudget
		limits.ForceGoDispatch = true
		if s.config.costTable != nil {
			limits.CostTable = s.config.costTable
		} else {
			limits.CostTable = isa.SharedDefaultCostTable()
		}
	}
	if s.config.maxStringSize > 0 {
		limits.MaxStringSize = s.config.maxStringSize
	}
	if s.config.yieldInterval > 0 {
		limits.YieldInterval = s.config.yieldInterval
		limits.ForceGoDispatch = true
	}
	if s.config.maxArenaBytes > 0 {
		limits.MaxArenaBytes = s.config.maxArenaBytes
	}
	if s.config.capabilityHook != nil {
		limits.CapabilityHook = s.config.capabilityHook
	}
}

// applyMaxExecutionTime wraps ctx with the configured maximum execution time, if set. The
// cancel function must be deferred by the caller.
//
// Returns context.Context with the deadline applied and context.CancelFunc that must be
// deferred.
func (s *Service) applyMaxExecutionTime(ctx context.Context) (context.Context, context.CancelFunc) {
	limit := defaultMaxExecutionTime
	if s.config != nil && s.config.maxExecutionTime > 0 {
		limit = s.config.maxExecutionTime
	} else if s.config != nil && s.config.debugger != nil {
		return context.WithCancel(ctx)
	}
	return context.WithTimeoutCause(ctx, limit, fault.ErrExecutionCancelled)
}

// recordCost stores the cost consumed by a VM execution into the service for later
// retrieval via LastCostUsed.
//
// Takes vm (*VM) which is the virtual machine whose cost to record.
func (s *Service) recordCost(vm *engine.VM) {
	if vm.Limits.CostBudget > 0 {
		s.lastCostUsed.Store(vm.CostUsed())
	}
}

// checkEvalSource refuses an evaluation whose context has ended, whose source is too
// large, or whose source uses go:embed.
//
// Takes code (string) which is the Go source code to evaluate.
//
// Returns error which is the refusal as a safe error, or nil.
func (s *Service) checkEvalSource(ctx context.Context, code string) error {
	if err := ctx.Err(); err != nil {
		return engine.NewSafeError(safeMessageEvaluationCancelled, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err))
	}
	if err := s.checkSourceSize(len(code)); err != nil {
		return engine.NewSafeError(safeMessageEvaluationFailed, err)
	}
	if err := rejectGoEmbedSource(evalFileName, code); err != nil {
		return engine.NewSafeError(safeMessageEvaluationFailed, err)
	}
	return nil
}

// evalAsExpression evaluates source that parsed as an expression.
//
// Takes code (string) which is the source.
// Takes expression (ast.Expr) which is its parse.
//
// Returns any which is the expression's value.
// Returns bool which is false when the expression is a call with no value, which Eval
// then runs as a statement instead.
// Returns error when evaluation fails.
func (s *Service) evalAsExpression(ctx context.Context, code string, expression ast.Expr) (any, bool, error) {
	result, evalErr := s.evalExpr(ctx, code)
	if evalErr == nil {
		return result, true, nil
	}
	if callMayBeAStatement(expression, evalErr) {
		return nil, false, nil
	}
	return nil, true, engine.NewSafeError(safeMessageEvaluationFailed, fmt.Errorf("evaluating expression: %w", evalErr))
}

// callMayBeAStatement reports whether an expression Eval could not type check as a value
// may still run as a statement.
//
// Takes expression (ast.Expr) which is the parsed source.
// Takes evalErr (error) which is the error evaluating it as a value gave.
//
// Returns bool which is true for a call that failed type checking.
func callMayBeAStatement(expression ast.Expr, evalErr error) bool {
	_, isCall := ast.Unparen(expression).(*ast.CallExpr)
	return isCall && errors.Is(evalErr, fault.ErrTypeCheck)
}
