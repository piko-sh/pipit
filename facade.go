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

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/sdk/module"
)

// BorrowClass is what a native function does with the arguments it is handed, as a host
// declares it with Interpreter.DeclareNativeBorrowClass.
type BorrowClass = engine.BorrowClass

const (
	// BorrowUnknown declares nothing.
	//
	// An arena argument revokes.
	BorrowUnknown = engine.BorrowUnknown

	// BorrowBorrows declares that the native keeps no reference to an argument once the call
	// returns.
	BorrowBorrows = engine.BorrowBorrows

	// BorrowResultAliases declares that the native may return a result aliasing an argument,
	// but keeps no other reference.
	BorrowResultAliases = engine.BorrowResultAliases

	// BorrowRetains declares that the native may keep a reference to an argument.
	BorrowRetains = engine.BorrowRetains

	// BorrowReadsOnly declares that the native borrows, returns no alias of an argument and
	// never writes through one, so a []byte(s) argument may be the string's own bytes.
	BorrowReadsOnly = engine.BorrowReadsOnly
)

// Interpreter is the vetted public interpreter surface: a thin wrapper over the internal
// service that exposes evaluation, compilation, module and session operations. It
// deliberately does not expose the advanced registry-mutation methods (UseSymbols,
// UseSymbolProviders, RegisterPackage); register host packages through
// NewInterpreterWithSymbols instead.
type Interpreter struct {
	// service is the internal interpreter that backs all public operations.
	service *app.Service
}

// Session is the vetted public session surface. It does not expose Service or Restore.
type Session struct {
	// session is the internal session that backs all public operations.
	session *app.Session
}

var (
	// ErrBorrowDeclarationsFrozen is returned by a declaration after the first evaluation.
	ErrBorrowDeclarationsFrozen = engine.ErrBorrowDeclarationsFrozen

	// ErrBorrowDeclarationRefused is returned by a declaration its checks refuse.
	ErrBorrowDeclarationRefused = engine.ErrBorrowDeclarationRefused
)

// Compile compiles one expression or statement block.
//
// Takes code (string) which is the source to compile.
//
// Returns *CompiledFunction and any compilation error.
func (i *Interpreter) Compile(ctx context.Context, code string) (*CompiledFunction, error) {
	return i.service.Compile(ctx, code)
}

// CompileFileSet compiles a package from its named source files.
//
// Takes sources (map[string]string) mapping filenames to source text.
//
// Returns *CompiledFileSet and any compilation error.
func (i *Interpreter) CompileFileSet(ctx context.Context, sources map[string]string) (*CompiledFileSet, error) {
	return i.service.CompileFileSet(ctx, sources)
}

// CompileProgram compiles a multi-package program. When several packages fail, the error
// is a ProblemList with one problem for each; CompileProblems splits it.
//
// Takes modulePath (string) which is the root module import path.
// Takes packages (map[string]map[string]string) mapping package paths to their named
// source files.
//
// Returns *CompiledFileSet and any compilation error.
func (i *Interpreter) CompileProgram(ctx context.Context, modulePath string, packages map[string]map[string]string) (*CompiledFileSet, error) {
	return i.service.CompileProgram(ctx, modulePath, packages)
}

// Eval evaluates one expression or statement block.
//
// Takes code (string) which is the source to evaluate.
//
// Returns the evaluation result and any error.
func (i *Interpreter) Eval(ctx context.Context, code string) (any, error) {
	return i.service.Eval(ctx, code)
}

// Execute runs a previously compiled function.
//
// Takes compiledFunction (*CompiledFunction) which is the compiled unit to run.
//
// Returns the execution result and any error.
func (i *Interpreter) Execute(ctx context.Context, compiledFunction *CompiledFunction) (any, error) {
	return i.service.Execute(ctx, compiledFunction)
}

// EvalFile evaluates a complete source file and calls its entrypoint.
//
// Takes source (string) which is the file's source text.
// Takes entrypoint (string) which is the function name to invoke.
//
// Returns the entrypoint's result and any error.
func (i *Interpreter) EvalFile(ctx context.Context, source, entrypoint string) (any, error) {
	return i.service.EvalFile(ctx, source, entrypoint)
}

// ExecuteEntrypoint runs the named entrypoint of a compiled file set.
//
// Takes cfs (*CompiledFileSet) which holds the compiled package.
// Takes entrypoint (string) which is the function name to invoke.
//
// Returns the entrypoint's result and any error.
func (i *Interpreter) ExecuteEntrypoint(ctx context.Context, cfs *CompiledFileSet, entrypoint string) (any, error) {
	return i.service.ExecuteEntrypoint(ctx, cfs, entrypoint)
}

// CallFunction runs a named function with host-supplied arguments.
//
// Takes cfs (*CompiledFileSet) which holds the compiled package.
// Takes name (string) which is the function to call.
// Takes arguments ([]any) which are the host values passed in.
//
// Returns the function's result and any error.
func (i *Interpreter) CallFunction(ctx context.Context, cfs *CompiledFileSet, name string, arguments ...any) (any, error) {
	return i.service.CallFunction(ctx, cfs, name, arguments...)
}

// ExecuteInits runs the package initialisers of a compiled file set once.
//
// Takes cfs (*CompiledFileSet) whose initialisers are executed.
//
// Returns error when an initialiser fails.
func (i *Interpreter) ExecuteInits(ctx context.Context, cfs *CompiledFileSet) error {
	return i.service.ExecuteInits(ctx, cfs)
}

// DeclareTopLevelCalls declares that the host calls function values received from the
// program only at top level.
//
// Such calls must never occur inside a native function called by an interpreted program.
// Their arguments must not reach values belonging to a running program.
//
// Takes cfs (*CompiledFileSet) which is the compiled program.
func (i *Interpreter) DeclareTopLevelCalls(cfs *CompiledFileSet) {
	i.service.DeclareTopLevelCalls(cfs)
}

// SaveCompiled persists a compiled file set under key.
//
// Takes key (string) which identifies the stored entry.
// Takes cfs (*CompiledFileSet) which is the compiled unit to save.
//
// Returns error when serialisation or storage fails.
func (i *Interpreter) SaveCompiled(ctx context.Context, key string, cfs *CompiledFileSet) error {
	return i.service.SaveCompiled(ctx, key, cfs)
}

// LoadCompiled loads a compiled file set previously saved under key.
//
// Takes key (string) which identifies the stored entry.
//
// Returns *CompiledFileSet and any deserialisation error.
func (i *Interpreter) LoadCompiled(ctx context.Context, key string) (*CompiledFileSet, error) {
	return i.service.LoadCompiled(ctx, key)
}

// UnloadCompiled releases the package variables of a file set LoadCompiled returned.
//
// The variables' storage returns to the interpreter once no run of the program is in
// progress and no other load of the same bytecode still uses it, and is cleared before
// another program reuses it. A later run of the file set returns ErrProgramUnloaded.
// Function values the program handed the host must not be called after it is unloaded.
// Unloading a file set twice does nothing.
//
// Takes cfs (*CompiledFileSet) which is a file set this interpreter loaded.
//
// Returns error which wraps ErrProgramNotLoaded when cfs was not loaded by this
// interpreter.
func (i *Interpreter) UnloadCompiled(cfs *CompiledFileSet) error {
	return i.service.UnloadCompiled(cfs)
}

// HasRegisteredPackage reports whether importPath is registered.
//
// Takes importPath (string) which is the package import path to check.
//
// Returns true when the path has been registered.
func (i *Interpreter) HasRegisteredPackage(importPath string) bool {
	return i.service.HasRegisteredPackage(importPath)
}

// PackageModule packages a module bundle from its sources.
//
// Takes moduleDescriptor (module.Descriptor) which describes the module.
// Takes modulePath (string) which is the root module import path.
// Takes packages (map[string]map[string]string) mapping package paths to their named
// source files.
// Takes bytecodePacker (func(context.Context, *CompiledFileSet) ([]byte, error)) which
// serialises the compiled output, or reports why it cannot.
//
// Returns *module.Bundle which is the packaged module.
// Returns error which reports a compilation or packaging failure.
func (i *Interpreter) PackageModule(
	ctx context.Context,
	moduleDescriptor module.Descriptor,
	modulePath string,
	packages map[string]map[string]string,
	bytecodePacker func(context.Context, *CompiledFileSet) ([]byte, error),
) (*module.Bundle, error) {
	return i.service.PackageModule(ctx, moduleDescriptor, modulePath, packages, bytecodePacker)
}

// LoadModule loads a module bundle.
//
// Takes bundle (*module.Bundle) which holds the packaged module.
// Takes reference (module.Ref) which identifies the module version.
// Takes provider (module.Provider) which resolves transitive deps.
// Takes bytecodeUnpacker (func) which deserialises the compiled output.
//
// Returns *module.Loaded and any loading error.
func (i *Interpreter) LoadModule(
	ctx context.Context,
	bundle *module.Bundle,
	reference module.Ref,
	provider module.Provider,
	bytecodeUnpacker func(context.Context, []byte, *SymbolRegistry) (*CompiledFileSet, error),
) (*module.Loaded, error) {
	return i.service.LoadModule(ctx, bundle, reference, provider, bytecodeUnpacker)
}

// SetStderr routes interpreted stderr to writer.
//
// Takes writer (io.Writer) which receives stderr output.
func (i *Interpreter) SetStderr(writer io.Writer) {
	i.service.SetStderr(writer)
}

// Reset clears accumulated per-evaluation state.
func (i *Interpreter) Reset() {
	i.service.Reset()
}

// Close stops the interpreter's background bookkeeping, the watches that let unchanged
// package variables skip their initialisers, and drops the state kept per compiled file
// set. Call it when the interpreter is no longer needed; a clone has its own.
//
// Returns error which is always nil.
func (i *Interpreter) Close() error {
	return i.service.Close()
}

// Clone derives an independent interpreter sharing immutable configuration.
//
// Returns *Interpreter backed by a fresh internal service.
func (i *Interpreter) Clone() *Interpreter {
	return wrapInterpreter(i.service.Clone())
}

// SetCapabilityHook installs the capability hook consulted before gated calls.
//
// Takes hook (CapabilityHook) which is the hook to install.
func (i *Interpreter) SetCapabilityHook(hook CapabilityHook) {
	i.service.SetCapabilityHook(hook)
}

// CapabilityHook returns the installed capability hook.
//
// Returns CapabilityHook currently in effect.
func (i *Interpreter) CapabilityHook() CapabilityHook {
	return i.service.CapabilityHook()
}

// LastCostUsed reports the metered cost of the last evaluation.
//
// Returns int64 counting the cost units consumed.
func (i *Interpreter) LastCostUsed() int64 {
	return i.service.LastCostUsed()
}

// LeakedGoroutines reports goroutines left running after the last evaluation.
//
// Returns int64 counting the leaked goroutines.
func (i *Interpreter) LeakedGoroutines() int64 {
	return i.service.LeakedGoroutines()
}

// AbandonedLocks reports locks a stopped evaluation left held. Once it is non-zero every
// later evaluation fails at once with ErrLockAbandoned.
//
// Returns int64 counting the abandoned locks.
func (i *Interpreter) AbandonedLocks() int64 {
	return i.service.AbandonedLocks()
}

// FastPathStats returns the interpreter's fast-path counters.
//
// Returns FastPathStats with the accumulated counters.
func (i *Interpreter) FastPathStats() FastPathStats {
	return i.service.FastPathStats()
}

// NewSession starts a stateful session over this interpreter.
//
// Takes opts ([]SessionOption) which configure the session.
//
// Returns *Session backed by the interpreter's service.
func (i *Interpreter) NewSession(opts ...SessionOption) *Session {
	return wrapSession(i.service.NewSession(opts...))
}

// SetStderr routes the session's interpreted stderr to writer.
//
// Takes writer (io.Writer) which receives stderr output.
func (s *Session) SetStderr(writer io.Writer) {
	s.session.SetStderr(writer)
}

// Submit evaluates one submission against retained session state.
//
// Takes code (string) which is the source to evaluate.
//
// Returns the evaluation result and any error.
func (s *Session) Submit(ctx context.Context, code string) (any, error) {
	return s.session.Submit(ctx, code)
}

// Reset clears the session's accumulated declarations.
func (s *Session) Reset() {
	s.session.Reset()
}

// CompiledFunctions returns the session's accumulated compiled functions.
//
// Returns []*CompiledFunction from all prior submissions.
func (s *Session) CompiledFunctions() []*CompiledFunction {
	return s.session.CompiledFunctions()
}

// Inspect returns the session's accumulated declarations.
//
// Returns SessionState describing the declared names.
func (s *Session) Inspect() SessionState {
	return s.session.Inspect()
}

// wrapInterpreter wraps a service, returning nil for a nil service.
//
// Takes service (*app.Service) which is the internal interpreter to wrap.
//
// Returns *Interpreter backed by the given service, or nil.
func wrapInterpreter(service *app.Service) *Interpreter {
	if service == nil {
		return nil
	}
	return &Interpreter{service: service}
}

// wrapSession wraps an app session, returning nil for a nil session.
//
// Takes session (*app.Session) which is the internal session to wrap.
//
// Returns *Session backed by the given session, or nil.
func wrapSession(session *app.Session) *Session {
	if session == nil {
		return nil
	}
	return &Session{session: session}
}

// DeclareNativeBorrowClass declares the borrow class of a native function the host
// registers.
//
// Takes function (any) which is the native function.
// Takes class (BorrowClass) which is the declared class.
//
// Returns error which wraps ErrBorrowDeclarationsFrozen or ErrBorrowDeclarationRefused
// when the declaration is not recorded.
func (i *Interpreter) DeclareNativeBorrowClass(function any, class BorrowClass) error {
	return i.service.DeclareNativeBorrowClass(function, class)
}
