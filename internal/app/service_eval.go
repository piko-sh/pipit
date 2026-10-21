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
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"

	"pipit.sh/pipit/internal/buildfilter"
	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/frontend"

	"pipit.sh/pipit/internal/fault"
)

const (
	// errFmtEvaluatingFile is the format string for wrapping errors during file evaluation.
	errFmtEvaluatingFile = "evaluating file: %w"

	// errFmtCompilingFile is the format string for wrapping errors during file compilation.
	errFmtCompilingFile = "compiling file: %w"
)

var (
	// statementForbiddenIdents names predeclared identifiers whose CallExpr form yields a
	// value that go/types rejects as an unused bare statement.
	statementForbiddenIdents = map[string]struct{}{
		"append":     {},
		"cap":        {},
		"complex":    {},
		"imag":       {},
		"len":        {},
		"make":       {},
		"max":        {},
		"min":        {},
		"new":        {},
		"real":       {},
		"int":        {},
		"int8":       {},
		"int16":      {},
		"int32":      {},
		"int64":      {},
		"uint":       {},
		"uint8":      {},
		"uint16":     {},
		"uint32":     {},
		"uint64":     {},
		"uintptr":    {},
		"byte":       {},
		"rune":       {},
		"float32":    {},
		"float64":    {},
		"complex64":  {},
		"complex128": {},
		"bool":       {},
		"string":     {},
		"error":      {},
		"any":        {},
		"comparable": {},
	}
)

// orderedPackages is what compiling a program's packages in dependency order produced.
type orderedPackages struct {
	// mainFunctionTable is the main package's function table, nil without a main package.
	mainFunctionTable map[string]uint16

	// lastFunctionTable is the function table of the last package compiled.
	lastFunctionTable map[string]uint16

	// initFunctions lists every package's init functions in dependency order.
	initFunctions []uint16

	// bindings describe each package for the debugger.
	bindings []debug.PackageBinding
}

// processEnvironment is the environment interpreted code sees: an overlay on the host
// process environment, or a sealed environment of its own.
type processEnvironment struct {
	// values are the variables the overlay (or sealed environment) holds.
	values map[string]string

	// removed names host variables the program unset, so they stay hidden until set again.
	removed map[string]struct{}

	// mu guards every field; interpreted goroutines read and write concurrently.
	mu sync.RWMutex

	// sealed reports that the host environment is never consulted.
	sealed bool

	// hostCleared reports that the program called Clearenv, which hides the whole host
	// environment from then on.
	hostCleared bool
}

// newProcessEnvironment builds the environment view over the configured variables.
//
// Takes values (map[string]string) which the service owns already.
// Takes sealed (bool) which selects a sealed environment over an overlay.
//
// Returns *processEnvironment ready to install.
func newProcessEnvironment(values map[string]string, sealed bool) *processEnvironment {
	return &processEnvironment{mu: sync.RWMutex{}, values: values, removed: map[string]struct{}{}, sealed: sealed, hostCleared: false}
}

// hostVisible reports whether host variables are consulted at all.
//
// Returns bool which is false for a sealed or cleared environment.
func (e *processEnvironment) hostVisible() bool {
	return !e.sealed && !e.hostCleared
}

// lookup finds key in the overlay, then in the host environment when visible.
//
// Takes key (string) which is the variable name.
//
// Returns the value and whether it is set.
//
// Safe for concurrent use; acquires a read lock.
func (e *processEnvironment) lookup(key string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if value, ok := e.values[key]; ok {
		return value, true
	}
	if _, gone := e.removed[key]; gone || !e.hostVisible() {
		return "", false
	}
	return os.LookupEnv(key)
}

// getenv is os.Getenv over the view.
//
// Takes key (string) which is the variable name.
//
// Returns string which is the value, or empty when unset.
func (e *processEnvironment) getenv(key string) string {
	value, _ := e.lookup(key)
	return value
}

// environ is os.Environ over the view, sorted.
//
// Returns []string of key=value entries.
//
// Safe for concurrent use; acquires a read lock.
func (e *processEnvironment) environ() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	merged := make(map[string]string, len(e.values))
	if e.hostVisible() {
		for _, entry := range os.Environ() {
			key, value, _ := strings.Cut(entry, "=")
			if _, gone := e.removed[key]; !gone {
				merged[key] = value
			}
		}
	}
	maps.Copy(merged, e.values)
	result := make([]string, 0, len(merged))
	for key, value := range merged {
		result = append(result, key+"="+value)
	}
	slices.Sort(result)
	return result
}

// setenv is os.Setenv over the view; the host process is never changed.
//
// Takes key (string) which is the variable name.
// Takes value (string) which is the variable value.
//
// Returns error which is always nil.
//
// Safe for concurrent use; acquires a write lock.
func (e *processEnvironment) setenv(key, value string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.values[key] = value
	delete(e.removed, key)
	return nil
}

// unsetenv is os.Unsetenv over the view; a host variable stays hidden until set again.
//
// Takes key (string) which is the variable name.
//
// Returns error which is always nil.
//
// Safe for concurrent use; acquires a write lock.
func (e *processEnvironment) unsetenv(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.values, key)
	e.removed[key] = struct{}{}
	return nil
}

// clearenv is os.Clearenv over the view.
//
// Safe for concurrent use; acquires a write lock.
func (e *processEnvironment) clearenv() {
	e.mu.Lock()
	defer e.mu.Unlock()
	clear(e.values)
	e.hostCleared = true
}

// compileOrderedPackages iterates over topologically sorted packages, compiling each one
// and collecting init functions and func tables.
//
// Takes order ([]string) which is the topologically sorted list of package import paths.
// Takes set (packageSet) which holds the parsed packages and the import paths of the
// packages that already failed to parse.
// Takes rootFunction (*CompiledFunction) which is the root compiled function for the
// program.
// Takes crossPackageMethods (map[string]uint16) which tracks method indices shared across
// packages.
//
// Returns orderedPackages which is what the packages produced.
// Returns error which joins the problems of every package that failed.
func (s *Service) compileOrderedPackages(
	ctx context.Context,
	order []string,
	set packageSet,
	rootFunction *program.CompiledFunction,
	crossPackageMethods map[string]uint16,
) (orderedPackages, error) {
	interpretedPaths := make(map[string]bool, len(set.parsed)+len(set.failed))
	for importPath := range set.parsed {
		interpretedPaths[importPath] = true
	}
	failed := maps.Clone(set.failed)
	if failed == nil {
		failed = make(map[string]struct{})
	}
	for importPath := range failed {
		interpretedPaths[importPath] = true
	}

	var compiled orderedPackages
	var problems []error
	for _, importPath := range order {
		if err := cancellationError(ctx); err != nil {
			problems = append(problems, err)
			break
		}
		pkg := set.parsed[importPath]
		if importsAnyOf(pkg, failed) {
			failed[importPath] = struct{}{}
			continue
		}
		result, compileErr := s.compileSinglePackage(ctx, pkg, rootFunction, crossPackageMethods, interpretedPaths)
		if compileErr != nil {
			problems = append(problems, compileErr)
			failed[importPath] = struct{}{}
			if stopsCompilation(ctx, compileErr) {
				break
			}
			continue
		}
		if recordErr := s.recordCompiledPackage(pkg, result, &compiled, crossPackageMethods); recordErr != nil {
			problems = append(problems, recordErr)
			failed[importPath] = struct{}{}
		}
	}

	return compiled, fault.JoinProblems(problems)
}

// recordCompiledPackage adds what a package compiled to the program: its debugger
// binding, init functions and methods, and its exports for the packages that import it,
// or, for package main, its function table and external methods.
//
// Takes pkg (*parsedPackage) which is the package.
// Takes result (*packageCompileResult) which is what it compiled to.
// Takes compiled (*orderedPackages) which collects the program.
// Takes crossPackageMethods (map[string]uint16) which collects the methods.
//
// Returns error when the registry rejects a non-main package's exported types.
func (s *Service) recordCompiledPackage(pkg *parsedPackage, result *packageCompileResult, compiled *orderedPackages, crossPackageMethods map[string]uint16) error {
	compiled.bindings = append(compiled.bindings, debug.PackageBinding{
		Package:         result.typesPackage,
		FunctionTable:   result.functionTable,
		GlobalVariables: result.globalVariables,
		Path:            pkg.importPath,
	})
	compiled.initFunctions = append(compiled.initFunctions, result.initFunctionIndices...)
	collectCrossPackageMethods(result.functionTable, crossPackageMethods)
	compiled.lastFunctionTable = result.functionTable

	if pkg.packageName != mainPackageName {
		return s.bridgePackageExports(pkg.importPath, result)
	}
	publishExternalMethods(s.globals, result.rootFunction)
	compiled.mainFunctionTable = make(map[string]uint16, len(result.functionTable))
	maps.Copy(compiled.mainFunctionTable, result.functionTable)
	return nil
}

// parseAndFilterFiles parses the source files the interpreter compiles, in name order. A
// //go:embed directive is rejected only in a file that survives the build filter, so a
// file guarded by //go:build !interp may embed.
//
// Takes sources (map[string]string) which maps filenames to source code strings.
//
// Returns []*ast.File which are the parsed and filtered AST files.
// Returns error when parsing fails, no files pass the filter, or ctx ends.
func (s *Service) parseAndFilterFiles(ctx context.Context, sources map[string]string) ([]*ast.File, error) {
	files, err := s.parseSelectedFiles(ctx, sources)
	if errors.Is(err, fault.ErrExecutionCancelled) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrParse, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: all files excluded by build constraints", fault.ErrParse)
	}
	if err := rejectGoEmbed(s.fileSet, "", files); err != nil {
		return nil, err
	}
	return files, nil
}

// parseSelectedFiles parses matching files in name order, collecting failures up to
// maxTypeErrorsPerPackage.
//
// Takes sources (map[string]string) which maps filenames to source code strings.
//
// Returns []*ast.File which are the included files that parsed.
// Returns error which joins one error per file whose build constraints are malformed or
// which does not parse, each naming its file, for the caller to wrap with fault.ErrParse;
// or which wraps fault.ErrExecutionCancelled when ctx ends.
func (s *Service) parseSelectedFiles(ctx context.Context, sources map[string]string) ([]*ast.File, error) {
	matcher := s.fileMatcher()
	files := make([]*ast.File, 0, len(sources))
	problems := newCappedErrors(maxTypeErrorsPerPackage)
	for _, name := range slices.Sorted(maps.Keys(sources)) {
		if err := cancellationError(ctx); err != nil {
			return nil, err
		}
		included, err := matcher.MatchString(name, sources[name])
		if err != nil {
			problems.add(err)
			continue
		}
		if !included {
			continue
		}
		file, err := parser.ParseFile(s.fileSet, name, sources[name], parser.ParseComments)
		if err != nil {
			problems.add(err)
			continue
		}
		files = append(files, file)
	}
	return files, problems.join(errTooManyFileErrors)
}

// fileMatcher returns a matcher for the host platform with the interp tag and configured
// build tags.
//
// Returns *buildfilter.Matcher which decides each file.
func (s *Service) fileMatcher() *buildfilter.Matcher {
	if s.config == nil || s.config.fileMatcher == nil {
		return buildfilter.New(nil)
	}
	return s.config.fileMatcher
}

// MatchFile reports whether a file matches the host platform and configured build tags,
// excluding package documentation and cgo files.
//
// Takes name (string) which is the file's name; only the base name counts.
// Takes src ([]byte) which is the file's content.
//
// Returns bool which is true when the file is compiled.
// Returns error which wraps fault.ErrParse when the build constraints are malformed, the
// file's header cannot be read, or a test file uses cgo.
func (s *Service) MatchFile(name string, src []byte) (bool, error) {
	matched, err := s.fileMatcher().Match(name, src)
	if err != nil {
		return false, fmt.Errorf(fault.ErrChainFmt, fault.ErrParse, err)
	}
	return matched, nil
}

// newFileSetCompiler creates a Compiler for a file-set or eval compilation that needs
// global variable tracking.
//
// Takes rootFunction (*CompiledFunction) which is the root compiled function for the file
// set.
// Takes info (*types.Info) which holds the type-checking information.
//
// Returns *Compiler configured with global variable tracking.
func (s *Service) newFileSetCompiler(ctx context.Context, rootFunction *program.CompiledFunction, info *types.Info) *compile.Compiler {
	s.applyResourceLimits(rootFunction)
	return compile.NewCompiler(ctx, compile.CompilerConfig{
		FileSet:            s.fileSet,
		Info:               info,
		Function:           rootFunction,
		RootFunction:       rootFunction,
		ScopeName:          "<root>",
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            s.symbols,
		Globals:            s.globals,
		Features:           s.features,
		Passes:             s.optimisations(),
		Patterns:           nil,
		MaxLiteralElements: s.maxLiteralElements(),
		MaxExpressionDepth: s.maxExpressionDepth(),
		DebugEnabled:       s.config != nil && s.config.debugInfo,
	})
}

// applyResourceLimits stamps the service-configured per-function caps (constant pool,
// specialisations, methods) onto rootFunction so that every CompiledFunction propagating
// from it observes the configured limits.
//
// Takes rootFunction (*CompiledFunction) which receives the cap values.
func (s *Service) applyResourceLimits(rootFunction *program.CompiledFunction) {
	if s.config == nil || rootFunction == nil {
		return
	}
	rootFunction.ApplyResourceLimits(
		s.config.maxConstantPoolSize, s.config.maxSpecialisations, s.config.maxMethods)
}

// maxExpressionDepth returns the configured expression-depth ceiling, or 0 to keep the
// package default in force.
//
// Returns int which is the configured ceiling, or 0 when unset.
func (s *Service) maxExpressionDepth() int {
	if s.config != nil {
		return s.config.maxExpressionDepth
	}
	return 0
}

// maxLiteralElements returns the configured max literal element count, or 0 if not set.
//
// Returns int which is the configured limit, or 0 for unlimited.
func (s *Service) maxLiteralElements() int {
	if s.config != nil {
		return s.config.maxLiteralElements
	}
	return 0
}

// evalExpressionConfig builds the compiler configuration for a single-expression
// evaluation.
//
// Takes info (*types.Info) which is the type information for the expression.
//
// Returns the configuration to hand to compile.CompileEvalExpression.
func (s *Service) evalExpressionConfig(info *types.Info) compile.CompilerConfig {
	return compile.CompilerConfig{
		FileSet:            s.fileSet,
		Info:               info,
		Function:           nil,
		RootFunction:       nil,
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            s.symbols,
		Globals:            nil,
		ScopeName:          "<eval>",
		Features:           s.features,
		Passes:             s.optimisations(),
		Patterns:           nil,
		DebugEnabled:       false,
		MaxLiteralElements: s.maxLiteralElements(),
		MaxExpressionDepth: 0,
	}
}

// optimisations returns the configured optimisation selection, or every optimisation when
// the service has no config.
//
// Returns the passes.Options handed to each compiler.
func (s *Service) optimisations() passes.Options {
	if s.config != nil {
		return s.config.optimisations
	}
	return passes.DefaultOptions()
}

// optimisationsFor selects optimisations for one compilation attempt.
//
// Returns the passes.Options for the compile's program pipeline.
func (s *Service) optimisationsFor(ctx context.Context) passes.Options {
	options := s.optimisations()
	if arenaPromotionOff(ctx) {
		options.ArenaPromotion = false
	}
	return options
}

// executeInitFunction runs a single init function in its own VM, ensuring the arena is
// released on init function exit rather than when the caller returns.
//
// Takes rootFunction (*CompiledFunction) which provides the function table for
// cross-function calls.
// Takes initFunction (*CompiledFunction) which is the init function to execute.
//
// Returns error when the init function execution fails.
func (s *Service) executeInitFunction(ctx context.Context, rootFunction *program.CompiledFunction, initFunction *program.CompiledFunction) (err error) {
	defer engine.RecoverArenaBudgetError(&err)
	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleInit)
	defer vm.Recycle()
	defer cancelExecution(fault.ErrMainReturned)
	defer s.recordCost(vm)
	vm.BindFileSet(program.ExportFunctions(rootFunction), rootFunction)
	vm.EnsureOwnedCallStack(initFunction)
	defer vm.ReleaseArena()
	defer vm.FinishWatcher()
	vm.PushFrame(initFunction)
	if _, runErr := vm.RunDispatchedGuarded(0); runErr != nil {
		return fmt.Errorf("init: %w", runErr)
	}
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil {
		return fmt.Errorf("init: %w", joinErr)
	}
	if info := vm.TakeGoroutinePanic(); info != nil {
		return fmt.Errorf("init: %w", engine.NewGoroutinePanicError(info.Value()))
	}
	vm.MaterialiseGlobalStrings()
	return nil
}

// evalExpr evaluates a single expression.
//
// Takes code (string) which contains the Go expression source.
//
// Returns any which is the result of evaluating the expression.
// Returns error when parsing, type-checking, or execution fails.
func (s *Service) evalExpr(ctx context.Context, code string) (any, error) {
	wrapped := "package main\nvar _ = " + code
	if expressionAST, parseErr := parser.ParseExprFrom(s.fileSet, evalFileName, code, 0); parseErr == nil {
		if ident, ok := expressionAST.(*ast.Ident); ok && ident.Name == "nil" {
			wrapped = "package main\nvar _ any = " + code
		}
	}
	file, err := parser.ParseFile(s.fileSet, evalFileName, wrapped, 0)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrParse, err)
	}

	info := frontend.NewTypesInfo()
	if _, err := s.checkPackageTypes(ctx, mainPackageName, []*ast.File{file}, info, typeCheckOptions{interpretedPaths: nil, allowUnusedImports: false}); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err)
	}

	declaration, ok := file.Decls[0].(*ast.GenDecl)
	if !ok {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, fmt.Errorf("expected GenDecl, got %T", file.Decls[0]))
	}
	spec, ok := declaration.Specs[0].(*ast.ValueSpec)
	if !ok {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, fmt.Errorf("expected ValueSpec, got %T", declaration.Specs[0]))
	}
	typedExpr := spec.Values[0]

	compiledFunction, err := compile.CompileEvalExpression(ctx, s.evalExpressionConfig(info), typedExpr)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}

	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleEval)
	defer cancelExecution(fault.ErrMainReturned)
	result, execErr := vm.Execute(compiledFunction)
	s.recordCost(vm)
	return result, execErr
}

// doEvalFile evaluates a parsed file containing statements.
//
// Takes file (*ast.File) which is the parsed AST file to evaluate.
//
// Returns any which is the result of evaluating the last expression.
// Returns error when type-checking, compilation, or execution fails.
func (s *Service) doEvalFile(ctx context.Context, file *ast.File) (any, error) {
	lastExpr, hasResult := s.rewriteLastExprStmt(file)

	info := frontend.NewTypesInfo()
	if _, err := s.checkPackageTypes(ctx, mainPackageName, []*ast.File{file}, info, typeCheckOptions{interpretedPaths: nil, allowUnusedImports: false}); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err)
	}

	if hasResult && !expressionYieldsValue(info, lastExpr) {
		hasResult = false
	}

	evalFunction := program.NewNamedFunction("<eval>")
	c := s.newFileSetCompiler(ctx, evalFunction, info)

	c.RegisterPackageLevelVarsFromDecls(ctx, file.Decls)

	if err := s.compileAndRunVarInits(ctx, c, []*ast.File{file}); err != nil {
		return nil, fmt.Errorf(errFmtEvaluatingFile, err)
	}

	if err := c.CompileNonEvalFuncDecls(ctx, file.Decls); err != nil {
		return nil, fmt.Errorf(errFmtEvaluatingFile, err)
	}

	for _, initIndex := range c.InitFunctionIndices() {
		if err := s.executeInitFunction(ctx, evalFunction, program.ExportFunctions(evalFunction)[initIndex]); err != nil {
			return nil, fmt.Errorf(errFmtEvaluatingFile, err)
		}
	}

	result, evalError := compileAndRunEvalBody(ctx, c, s, file, info, lastExpr, hasResult)
	if evalError != nil {
		return nil, fmt.Errorf(errFmtEvaluatingFile, evalError)
	}
	return result, nil
}

// compileAndRunVarInits compiles and immediately runs package-level variable
// initialisers. No-op when no global variables are registered.
//
// Takes c (*Compiler) which holds the registered global variables.
// Takes files ([]*ast.File) which are the parsed AST files with variable declarations.
//
// Returns error when compilation or execution of variable initialisers fails.
func (s *Service) compileAndRunVarInits(ctx context.Context, c *compile.Compiler, files []*ast.File) (err error) {
	variableInitialisationFunction, err := c.CompileVariableInitFunction(ctx, files)
	if err != nil {
		return fmt.Errorf("compiling variable initialisers: %w", err)
	}
	if variableInitialisationFunction == nil {
		return nil
	}
	defer engine.RecoverArenaBudgetError(&err)

	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleVarInit)
	defer cancelExecution(fault.ErrMainReturned)
	vm.EnsureOwnedCallStack(variableInitialisationFunction)
	defer vm.ReleaseArena()
	defer vm.FinishWatcher()
	vm.PushFrame(variableInitialisationFunction)
	if _, runErr := vm.RunDispatchedGuarded(0); runErr != nil {
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

// newTypesConfig creates a types.Config for type checking.
//
// Returns *types.Config which uses the service's symbol importer to synthesise packages
// under ctx.
func (s *Service) newTypesConfig(ctx context.Context) *types.Config {
	if s.symbols == nil {
		return frontend.NewTypesConfig(nil)
	}
	return frontend.NewTypesConfig(s.symbols.Importer(ctx))
}

// evalMixed handles code that mixes declarations (func, var, type, const) with executable
// statements, separating declarations from statements and placing them at the appropriate
// scope levels.
//
// Takes code (string) which is the mixed Go source code to evaluate.
//
// Returns any which is the result of evaluating the statements.
// Returns error when parsing, type-checking, or execution fails.
func (s *Service) evalMixed(ctx context.Context, code string) (any, error) {
	file, err := s.parseMixedSource(code)
	if err != nil {
		return nil, fmt.Errorf("evaluating mixed source: %w", err)
	}
	if err := s.checkFileImports(nil, file); err != nil {
		return nil, fmt.Errorf("evaluating mixed source: %w", err)
	}
	result, evalError := s.doEvalFile(ctx, file)
	if evalError != nil {
		return nil, fmt.Errorf("evaluating mixed source: %w", evalError)
	}
	return result, nil
}

// parseMixedSource classifies mixed code into imports, declarations, and statements, then
// reconstructs and parses a valid Go source file.
//
// Takes code (string) which is the mixed Go source code to parse.
//
// Returns *ast.File which is the reconstructed and parsed AST file.
// Returns error when the reconstructed source fails to parse.
func (s *Service) parseMixedSource(code string) (*ast.File, error) {
	cl := classifyLines(strings.Split(code, newlineSep))
	source := buildMixedSource(cl)

	file, err := parser.ParseFile(s.fileSet, evalFileName, source, 0)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrParse, err)
	}
	if err := rejectGoEmbedSource(evalFileName, source); err != nil {
		return nil, err
	}
	return file, nil
}

// rewriteLastExprStmt rewrites the trailing eval statement for go/types.
//
// Wraps the trailing expression as `_ = expr` so the synthetic _eval_ function passes
// go/types. Bare `nil` is wrapped as `_ = any(nil)` to give it a concrete type. Void
// function calls are left as bare ExprStmt nodes because wrapping them would fail with
// "(no value) used as value".
//
// Takes file (*ast.File) which contains the _eval_ function.
//
// Returns ast.Expr which is the original expression for type lookup; nil when no rewrite
// was performed.
// Returns bool which is true when the last statement was an expression statement we
// recognised.
func (*Service) rewriteLastExprStmt(file *ast.File) (ast.Expr, bool) {
	for _, declaration := range file.Decls {
		functionDeclaration, ok := declaration.(*ast.FuncDecl)
		if !ok || functionDeclaration.Name.Name != compile.EvalFunctionName || functionDeclaration.Body == nil {
			continue
		}
		return rewriteEvalBodyTail(functionDeclaration.Body)
	}
	return nil, false
}

// compileExpression compiles a single expression.
//
// Wraps code as `package main\nvar _ = <code>`. For bare `nil`, the assignee type is set
// to `any` so the literal type-checks.
//
// Takes code (string) which contains the Go expression source.
//
// Returns *CompiledFunction which is the compiled expression.
// Returns error when parsing, type-checking, or compilation fails.
func (s *Service) compileExpression(ctx context.Context, code string) (*program.CompiledFunction, error) {
	wrapped := "package main\nvar _ = " + code
	if expressionAST, parseErr := parser.ParseExprFrom(s.fileSet, evalFileName, code, 0); parseErr == nil {
		if ident, ok := expressionAST.(*ast.Ident); ok && ident.Name == "nil" {
			wrapped = "package main\nvar _ any = " + code
		}
	}
	file, err := parser.ParseFile(s.fileSet, evalFileName, wrapped, 0)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrParse, err)
	}

	info := frontend.NewTypesInfo()
	if _, err := s.checkPackageTypes(ctx, mainPackageName, []*ast.File{file}, info, typeCheckOptions{interpretedPaths: nil, allowUnusedImports: false}); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err)
	}

	declaration, ok := file.Decls[0].(*ast.GenDecl)
	if !ok {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, fmt.Errorf("expected GenDecl, got %T", file.Decls[0]))
	}
	spec, ok := declaration.Specs[0].(*ast.ValueSpec)
	if !ok {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, fmt.Errorf("expected ValueSpec, got %T", declaration.Specs[0]))
	}
	typedExpr := spec.Values[0]

	compiled, compileErr := compile.CompileEvalExpression(ctx, s.evalExpressionConfig(info), typedExpr)
	if compileErr != nil {
		return nil, fmt.Errorf("compiling expression: %w", compileErr)
	}
	return compiled, nil
}

// compileFile compiles a parsed file containing statements.
//
// Takes file (*ast.File) which is the parsed AST file to compile.
//
// Returns *CompiledFunction which is the compiled file.
// Returns error when type-checking or compilation fails.
func (s *Service) compileFile(ctx context.Context, file *ast.File) (*program.CompiledFunction, error) {
	lastExpr, hasResult := s.rewriteLastExprStmt(file)

	info := frontend.NewTypesInfo()
	if _, err := s.checkPackageTypes(ctx, mainPackageName, []*ast.File{file}, info, typeCheckOptions{interpretedPaths: nil, allowUnusedImports: false}); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err)
	}

	if hasResult && !expressionYieldsValue(info, lastExpr) {
		hasResult = false
	}

	evalFunction := program.NewNamedFunction("<eval>")
	c := s.newFileSetCompiler(ctx, evalFunction, info)

	c.RegisterPackageLevelVarsFromDecls(ctx, file.Decls)

	variableInitialisationFunction, err := c.CompileVariableInitFunction(ctx, []*ast.File{file})
	if err != nil {
		return nil, fmt.Errorf(errFmtCompilingFile, err)
	}
	if variableInitialisationFunction != nil {
		evalFunction.SetVariableInitFunction(variableInitialisationFunction)
	}

	if err := c.CompileNonEvalFuncDecls(ctx, file.Decls); err != nil {
		return nil, fmt.Errorf(errFmtCompilingFile, err)
	}

	compiled, compileErr := c.CompileEvalFunction(ctx, file, info, lastExpr, hasResult, evalFunction)
	if compileErr != nil {
		return nil, fmt.Errorf(errFmtCompilingFile, compileErr)
	}
	return compiled, nil
}

// compileMixed compiles code mixing declarations with statements.
//
// Takes code (string) which is the mixed Go source code to compile.
//
// Returns *CompiledFunction which is the compiled mixed code.
// Returns error when parsing or compilation fails.
func (s *Service) compileMixed(ctx context.Context, code string) (*program.CompiledFunction, error) {
	file, err := s.parseMixedSource(code)
	if err != nil {
		return nil, fmt.Errorf("compiling mixed source: %w", err)
	}
	if err := s.checkFileImports(nil, file); err != nil {
		return nil, fmt.Errorf("compiling mixed source: %w", err)
	}
	compiled, compileErr := s.compileFile(ctx, file)
	if compileErr != nil {
		return nil, fmt.Errorf("compiling mixed source: %w", compileErr)
	}
	return compiled, nil
}

// applyEnvOverrides patches the symbol registry's "os" package (and "flag" when an
// argument vector is configured) so that environment access, os.Args and flag parsing
// operate on the configured process view instead of the host process.
func (s *Service) applyEnvOverrides() {
	if s.config == nil || !s.config.processOverridesConfigured() || s.config.envOnce == nil {
		return
	}
	s.config.envOnce.Do(s.installEnvOverrides)
}

// installEnvOverrides performs the registry patch for applyEnvOverrides.
func (s *Service) installEnvOverrides() {
	existing, ok := s.symbols.PackageSymbols("os")
	if !ok {
		return
	}
	patched := make(map[string]reflect.Value, len(existing)+1)
	maps.Copy(patched, existing)
	if s.config.envConfigured {
		installEnvironmentSymbols(patched, newProcessEnvironment(s.config.env, s.config.envSealed))
	}
	if s.config.args != nil {
		args := slices.Clone(s.config.args)
		patched["Args"] = reflect.ValueOf(&args).Elem()
		s.installFlagOverrides(args)
	}
	s.symbols.RegisterPackage("os", patched)
}

// installFlagOverrides points the flag package's command line at the configured argument
// vector.
//
// Takes args ([]string) which is the argument vector, args[0] being the program name.
func (s *Service) installFlagOverrides(args []string) {
	existing, ok := s.symbols.PackageSymbols("flag")
	if !ok {
		return
	}
	set := flag.NewFlagSet(args[0], flag.ExitOnError)
	setValue := reflect.ValueOf(set)
	patched := make(map[string]reflect.Value, len(existing))
	for name, symbol := range existing {
		patched[name] = symbol
		if symbol.Kind() != reflect.Func {
			continue
		}
		if method := setValue.MethodByName(name); method.IsValid() && method.Type() == symbol.Type() {
			patched[name] = method
		}
	}
	patched["CommandLine"] = reflect.ValueOf(&set).Elem()
	patched["Parse"] = reflect.ValueOf(func() { _ = set.Parse(args[1:]) })
	s.symbols.RegisterPackage("flag", patched)
}

// installEnvironmentSymbols replaces the os package's environment accessors in symbols
// with the view's.
//
// Takes symbols (map[string]reflect.Value) which is the patched os package.
// Takes env (*processEnvironment) which is the view.
func installEnvironmentSymbols(symbols map[string]reflect.Value, env *processEnvironment) {
	symbols["Getenv"] = reflect.ValueOf(env.getenv)
	symbols["LookupEnv"] = reflect.ValueOf(env.lookup)
	symbols["Environ"] = reflect.ValueOf(env.environ)
	symbols["Setenv"] = reflect.ValueOf(env.setenv)
	symbols["Unsetenv"] = reflect.ValueOf(env.unsetenv)
	symbols["Clearenv"] = reflect.ValueOf(env.clearenv)
	symbols["ExpandEnv"] = reflect.ValueOf(func(text string) string { return os.Expand(text, env.getenv) })
}

// rewriteEvalBodyTail inspects the last statement of a pipit-eval function body and
// rewrites it so the eval result is observable via a synthetic `_ = expr` assignment when
// the statement is a value- returning expression. Returns the original expression and a
// bool indicating whether the rewrite succeeded.
//
// Takes body (*ast.BlockStmt) which is the eval function's body.
//
// Returns the trailing expression (or nil) and true when the body ended in an expression
// statement pipit can observe.
func rewriteEvalBodyTail(body *ast.BlockStmt) (ast.Expr, bool) {
	statements := body.List
	if len(statements) == 0 {
		return nil, false
	}
	last, ok := statements[len(statements)-1].(*ast.ExprStmt)
	if !ok {
		return nil, false
	}
	if call, isCall := last.X.(*ast.CallExpr); isCall {
		if callRequiresResultBinding(call) {
			body.List[len(statements)-1] = makeBindToBlankAssign(last.X)
		}
		return last.X, true
	}
	if ident, isIdent := last.X.(*ast.Ident); isIdent && ident.Name == "nil" {
		body.List[len(statements)-1] = makeBindToBlankAssign(&ast.CallExpr{
			Fun:  ast.NewIdent("any"),
			Args: []ast.Expr{last.X},
		})
		return last.X, true
	}

	body.List[len(statements)-1] = makeBindToBlankAssign(last.X)
	return last.X, true
}

// makeBindToBlankAssign produces the AST for `_ = expr` assignment statement used by
// rewriteEvalBodyTail to observe a trailing expression's value during pipit's eval
// transform.
//
// Takes expression (ast.Expr) which is the right-hand-side expression.
//
// Returns the synthetic assignment statement.
func makeBindToBlankAssign(expression ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{
		Lhs: []ast.Expr{ast.NewIdent("_")},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{expression},
	}
}

// callRequiresResultBinding reports whether a call needs `_ = call(...)` wrapping.
//
// Covers predeclared identifiers in statementForbiddenIdents (builtins like len/cap/make
// and named types like int/string) and type conversions with a composite-type literal
// callee ([]byte(s), (*T)(p), etc.). Regular function/method calls are valid statements
// and are left alone.
//
// Takes call (*ast.CallExpr) which is the trailing call expression.
//
// Returns bool, true when the call must be wrapped to typecheck.
func callRequiresResultBinding(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		_, ok := statementForbiddenIdents[fun.Name]
		return ok
	case *ast.ArrayType,
		*ast.MapType,
		*ast.ChanType,
		*ast.StructType,
		*ast.InterfaceType,
		*ast.FuncType,
		*ast.StarExpr,
		*ast.ParenExpr:

		return true
	default:
		return false
	}
}

// expressionYieldsValue reports whether the expression yields a value.
//
// Void function calls (zero-length result tuple) and expressions with no recorded type
// are treated as not yielding a value.
//
// Takes info (*types.Info) populated by a successful Check pass.
// Takes expression (ast.Expr) which is the trailing expression of the _eval_ function.
//
// Returns bool, true when the expression yields a value.
func expressionYieldsValue(info *types.Info, expression ast.Expr) bool {
	if expression == nil {
		return false
	}
	typeAndValue, ok := info.Types[expression]
	if !ok {
		return false
	}
	if typeAndValue.Type == nil {
		return false
	}
	if tuple, isTuple := typeAndValue.Type.(*types.Tuple); isTuple {
		return tuple.Len() > 0
	}
	return true
}

// stopsCompilation reports whether cancellation or a compiler panic prevents further
// compilation.
//
// Takes err (error) which is the package's failure.
//
// Returns bool which is true when no further package is compiled.
func stopsCompilation(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, fault.ErrCompilePanic)
}

// importsAnyOf reports whether any file of pkg imports one of the given paths.
//
// Takes pkg (*parsedPackage) which is the package to inspect.
// Takes paths (map[string]struct{}) which are the import paths to look for.
//
// Returns bool which is true when the package imports at least one of them.
func importsAnyOf(pkg *parsedPackage, paths map[string]struct{}) bool {
	if len(paths) == 0 {
		return false
	}
	for _, file := range pkg.files {
		for _, spec := range file.Imports {
			importPath, err := importPathOf(spec)
			if err != nil {
				continue
			}
			if _, found := paths[importPath]; found {
				return true
			}
		}
	}
	return false
}
