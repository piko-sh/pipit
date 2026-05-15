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
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

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

	// minBuildSuffixParts is the threshold for considering a filename to have both GOOS and
	// GOARCH suffix components (e.g. "foo_linux_amd64").
	minBuildSuffixParts = 3
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

	// knownGOOSTags enumerates the GOOS values Go recognises as filename-suffix build
	// constraints. Keep in sync with go/build/syslist.go; this list rarely changes between
	// Go releases.
	knownGOOSTags = map[string]bool{
		"aix":       true,
		"android":   true,
		"darwin":    true,
		"dragonfly": true,
		"freebsd":   true,
		"hurd":      true,
		"illumos":   true,
		"ios":       true,
		"js":        true,
		"linux":     true,
		"nacl":      true,
		"netbsd":    true,
		"openbsd":   true,
		"plan9":     true,
		"solaris":   true,
		"wasip1":    true,
		"windows":   true,
		"zos":       true,
	}

	// knownGOARCHTags enumerates the GOARCH values Go recognises as filename-suffix build
	// constraints. Keep in sync with go/build/syslist.go.
	knownGOARCHTags = map[string]bool{
		"386":         true,
		"amd64":       true,
		"amd64p32":    true,
		"arm":         true,
		"arm64":       true,
		"arm64be":     true,
		"armbe":       true,
		"loong64":     true,
		"mips":        true,
		"mips64":      true,
		"mips64le":    true,
		"mips64p32":   true,
		"mips64p32le": true,
		"mipsle":      true,
		"ppc":         true,
		"ppc64":       true,
		"ppc64le":     true,
		"riscv":       true,
		"riscv64":     true,
		"s390":        true,
		"s390x":       true,
		"sparc":       true,
		"sparc64":     true,
		"wasm":        true,
	}

	// unixGOOS is the set of GOOS values for which Go implies the "unix" build tag.
	unixGOOS = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true,
		"freebsd": true, "hurd": true, "illumos": true, "ios": true,
		"linux": true, "netbsd": true, "openbsd": true, "solaris": true,
	}
)

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
// Takes parsed (map[string]*parsedPackage) which maps import paths to their parsed
// package data.
// Takes rootFunction (*CompiledFunction) which is the root compiled function for the
// program.
// Takes crossPackageMethods (map[string]uint16) which tracks method indices shared across
// packages.
//
// Returns orderedPackages which is what the packages produced.
// Returns error when any package fails to compile.
func (s *Service) compileOrderedPackages(
	ctx context.Context,
	order []string,
	parsed map[string]*parsedPackage,
	rootFunction *program.CompiledFunction,
	crossPackageMethods map[string]uint16,
) (orderedPackages, error) {
	interpretedPaths := make(map[string]bool, len(parsed))
	for importPath := range parsed {
		interpretedPaths[importPath] = true
	}

	var compiled orderedPackages
	for _, importPath := range order {
		pkg := parsed[importPath]
		result, compileErr := s.compileSinglePackage(ctx, pkg, rootFunction, crossPackageMethods, interpretedPaths)
		if compileErr != nil {
			return compiled, compileErr
		}
		compiled.bindings = append(compiled.bindings, debug.PackageBinding{
			Package:         result.typesPackage,
			FunctionTable:   result.functionTable,
			GlobalVariables: result.globalVariables,
			Path:            importPath,
		})

		compiled.initFunctions = append(compiled.initFunctions, result.initFunctionIndices...)
		collectCrossPackageMethods(result.functionTable, crossPackageMethods)
		compiled.lastFunctionTable = result.functionTable

		if pkg.packageName != "main" {
			s.bridgePackageExports(importPath, result)
		} else {
			publishExternalMethods(s.globals, result.rootFunction)
		}

		if pkg.packageName == "main" {
			compiled.mainFunctionTable = make(map[string]uint16, len(result.functionTable))
			maps.Copy(compiled.mainFunctionTable, result.functionTable)
		}
	}

	return compiled, nil
}

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

// parseAndFilterFiles parses source files in deterministic order and filters them by
// //go:build constraints.
//
// Takes sources (map[string]string) which maps filenames to source code strings.
//
// Returns []*ast.File which are the parsed and filtered AST files.
// Returns error when parsing fails or no files pass the filter.
func (s *Service) parseAndFilterFiles(sources map[string]string) ([]*ast.File, error) {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)

	allFiles := make([]*ast.File, 0, len(sources))
	for _, name := range names {
		file, err := parser.ParseFile(s.fileSet, name, sources[name], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrParse, name, err)
		}
		allFiles = append(allFiles, file)
	}
	if err := rejectGoEmbed(s.fileSet, allFiles); err != nil {
		return nil, err
	}

	files := make([]*ast.File, 0, len(allFiles))
	for _, file := range allFiles {
		if s.shouldIncludeFile(file) {
			files = append(files, file)
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("%w: all files excluded by build constraints", fault.ErrParse)
	}
	return files, nil
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
	defer cancelExecution(fault.ErrMainReturned)
	defer s.recordCost(vm)
	vm.BindFileSet(program.ExportFunctions(rootFunction), rootFunction)
	vm.EnsureCallStack()
	defer vm.ReleaseArena()
	defer vm.FinishWatcher()
	vm.PushFrame(initFunction)
	if _, runErr := vm.RunGuarded(0); runErr != nil {
		return fmt.Errorf("init: %w", runErr)
	}
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil {
		return fmt.Errorf("init: %w", joinErr)
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
	conf := s.newTypesConfig()

	_, err = conf.Check(mainPackageName, s.fileSet, []*ast.File{file}, info)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, s.enrichTypeCheckError(err, []*ast.File{file}, nil))
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
	conf := s.newTypesConfig()

	if _, err := conf.Check(mainPackageName, s.fileSet, []*ast.File{file}, info); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, s.enrichTypeCheckError(err, []*ast.File{file}, nil))
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
	vm.EnsureCallStack()
	defer vm.ReleaseArena()
	defer vm.FinishWatcher()
	vm.PushFrame(variableInitialisationFunction)
	if _, runErr := vm.RunGuarded(0); runErr != nil {
		return fmt.Errorf(errVarinitFmt, runErr)
	}
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil {
		return fmt.Errorf(errVarinitFmt, joinErr)
	}
	vm.MaterialiseGlobalStrings()
	return nil
}

// newTypesConfig creates a types.Config for type checking.
//
// The nil check matters: s.symbols is a concrete pointer, so handing it straight to an
// importer interface would produce a non-nil interface wrapping a nil pointer, and the
// checker would call through it.
//
// Returns *types.Config configured with the service's symbol importer.
func (s *Service) newTypesConfig() *types.Config {
	if s.symbols == nil {
		return frontend.NewTypesConfig(nil)
	}
	return frontend.NewTypesConfig(s.symbols)
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
	conf := s.newTypesConfig()

	_, err = conf.Check(mainPackageName, s.fileSet, []*ast.File{file}, info)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, s.enrichTypeCheckError(err, []*ast.File{file}, nil))
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
	conf := s.newTypesConfig()

	if _, err := conf.Check(mainPackageName, s.fileSet, []*ast.File{file}, info); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, s.enrichTypeCheckError(err, []*ast.File{file}, nil))
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

// shouldIncludeFile reports whether a file participates in the current compilation by
// evaluating //go:build directives, legacy +build comments, and the filename's
// GOOS/GOARCH suffix convention.
//
// Takes file (*ast.File) which is the parsed Go source file.
//
// Returns bool which is true when every constraint matches the active tag set.
func (s *Service) shouldIncludeFile(file *ast.File) bool {
	matcher := s.buildTagMatcher()
	if !buildConstraintCommentsAccept(file, matcher) {
		return false
	}
	filename := s.fileSet.Position(file.Package).Filename
	if filename == "" {
		filename = file.Name.Name
	}
	return filenameMatchesBuildSuffix(filename, matcher)
}

// buildTagMatcher returns a predicate that reports whether a build tag is active. The
// default set includes the current GOOS, GOARCH, the Go version, the synthetic `interp`
// tag, and any user-provided tags from WithBuildTags.
//
// The `interp` tag lets module authors gate files that require features pipit does not
// implement (unsafe.Pointer, cgo, inline assembly). A `!interp` constraint excludes the
// file; the author can pair it with an `interp`-only pure-Go alternative.
//
// Returns func(string) bool which reports whether a given tag is active.
func (s *Service) buildTagMatcher() func(string) bool {
	tags := make(map[string]bool)
	tags[runtime.GOOS] = true
	tags[runtime.GOARCH] = true
	tags["interp"] = true

	tags["gc"] = true

	for _, tag := range build.Default.ReleaseTags {
		tags[tag] = true
	}

	if unixGOOS[runtime.GOOS] {
		tags["unix"] = true
	}

	if s.config != nil {
		for _, t := range s.config.buildTags {
			tags[t] = true
		}
	}

	return func(tag string) bool {
		return tags[tag]
	}
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

// buildConstraintCommentsAccept evaluates every //go:build (or +build) comment appearing
// before the package clause against matcher. Returns false as soon as any constraint
// rejects the configured build tag set.
//
// Takes file (*ast.File) which provides the comment groups to inspect.
// Takes matcher (func(string) bool) which reports whether a tag is active.
//
// Returns bool which is true when every constraint accepts the tag set.
func buildConstraintCommentsAccept(file *ast.File, matcher func(string) bool) bool {
	for _, cg := range file.Comments {
		if cg.Pos() >= file.Package {
			break
		}
		for _, c := range cg.List {
			if !constraint.IsGoBuild(c.Text) && !constraint.IsPlusBuild(c.Text) {
				continue
			}
			expression, err := constraint.Parse(c.Text)
			if err != nil {
				continue
			}
			if !expression.Eval(matcher) {
				return false
			}
		}
	}
	return true
}

// filenameMatchesBuildSuffix evaluates the GOOS/GOARCH suffix on a filename.
//
// The function decodes the implicit build constraint encoded in a Go file's basename.
// Names like foo.go always match; foo_linux.go matches linux only; foo_amd64.go matches
// amd64 only; foo_linux_amd64.go requires both linux and amd64. The `_test` suffix is
// stripped before constraint matching. A filename whose final underscore-separated token
// is not a known GOOS or GOARCH name is treated as unconstrained.
//
// Takes filename (string) which is the file's basename or full path.
// Takes matcher (func(string) bool) which reports whether a tag is active.
//
// Returns bool which is true when the encoded constraint accepts the active tag set.
func filenameMatchesBuildSuffix(filename string, matcher func(string) bool) bool {
	base := filename
	if index := strings.LastIndexAny(base, "/\\"); index >= 0 {
		base = base[index+1:]
	}
	base = strings.TrimSuffix(base, ".go")
	base = strings.TrimSuffix(base, "_test")

	parts := strings.Split(base, "_")
	if len(parts) < 2 {
		return true
	}
	last := parts[len(parts)-1]
	var prev string
	if len(parts) >= minBuildSuffixParts {
		prev = parts[len(parts)-2]
	}

	if prev != "" && knownGOOSTags[prev] && knownGOARCHTags[last] {
		return matcher(prev) && matcher(last)
	}
	if knownGOOSTags[last] || knownGOARCHTags[last] {
		return matcher(last)
	}
	return true
}
