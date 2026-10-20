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
	"go/constant"
	"go/parser"
	"go/types"
	"log/slog"
	"maps"
	"path"
	"reflect"
	runtimedebug "runtime/debug"
	"slices"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/frontend"
	"pipit.sh/pipit/internal/stdlibindex"
	"pipit.sh/pipit/internal/symtab"
)

const (
	// undefinedPrefix is the prefix that go/types uses for "undefined" error messages (e.g.
	// "undefined: content_domain.AnnotatedField").
	undefinedPrefix = "undefined: "

	// couldNotImportPrefix is the prefix that go/types uses when the Importer returns an
	// error for a requested import. It is a stable part of the go/types public error format;
	// a change in upstream Go silently disables enrichment of these messages until the
	// prefix is updated here.
	couldNotImportPrefix = "could not import "

	// maxTypeErrorsPerPackage caps the type errors reported for one package.
	maxTypeErrorsPerPackage = 10

	// notRegisteredMarker is the surface text of fault.ErrPackageNotInRegistry as it appears
	// inside the Importer's wrapped error message. The marker stays in sync with the
	// fmt.Errorf call at symbol_registry.go:(*SymbolRegistry).Import; enrichTypeCheckError
	// falls back to the un-enriched error when the marker no longer matches.
	notRegisteredMarker = "not registered with interpreter"
)

var (
	// errTooManyTypeErrors ends a package's type errors when more than
	// maxTypeErrorsPerPackage were found.
	errTooManyTypeErrors = errors.New("too many type errors")

	// errTooManyFileErrors ends a package's parse errors when more than
	// maxTypeErrorsPerPackage of its files failed.
	errTooManyFileErrors = errors.New("too many files with errors")
)

// parsedPackage holds the parsed and filtered files for a single package within a
// multi-package compilation.
type parsedPackage struct {
	// importPath is the fully qualified import path for the package.
	importPath string

	// relPath is the relative path within the module.
	relPath string

	// packageName is the declared package name from the source files.
	packageName string

	// files holds the parsed and build-tag-filtered AST files.
	files []*ast.File
}

// packageSet holds parsed packages and collects failures from the remaining packages.
type packageSet struct {
	// parsed maps import paths to the packages that parsed cleanly.
	parsed map[string]*parsedPackage

	// failed holds the import paths of the packages that did not.
	failed map[string]struct{}

	// problems lists the failed packages' problems in import-path order.
	problems []error
}

// packageCompileResult holds the output of compiling a single package within a
// multi-package compilation.
type packageCompileResult struct {
	// functionTable maps function names to their indices in the root function.
	functionTable map[string]uint16

	// globalVariables maps package-level variable names to their store slots.
	globalVariables map[string]program.GlobalVariableInfo

	// info holds the type-checking results for the package.
	info *types.Info

	// typesPackage is the types.Package produced by type-checking.
	typesPackage *types.Package

	// rootFunction is the root compiled function containing all sub-functions.
	rootFunction *program.CompiledFunction

	// initFunctionIndices holds indices of init functions in source order.
	initFunctionIndices []uint16

	// exportedVars lists exported package-level vars paired with the global-store slots
	// their values occupy, resolved into the symbol registry after ExecuteInits runs.
	exportedVars []packageVarExport
}

// packageVarExport pairs an exported var's name with the global-store slot the Compiler
// allocated for it, plus the var's Go type and the settable reflect.Value the symbol
// registry advertises.
type packageVarExport struct {
	// reflectType is the Go reflect.Type the bridged value should carry, used to wrap
	// primitive-bank values (int, uint, ...) in the var's declared Go type rather than the
	// bank's raw int64.
	reflectType reflect.Type

	// storage is the settable reflect.Value advertised in the symbol registry. The post-init
	// finaliser writes the current global-store value into this address; any compiled
	// bytecode that captured this Value reads through the same backing pointer.
	storage reflect.Value

	// name is the exported identifier (without package qualifier).
	name string

	// slot is the global-store coordinates the var's value lives at.
	slot program.GlobalVariableInfo
}

// pendingVarBridge ties one package's exported-var slot list to its import path. The
// Service accumulates these during compilation and resolves them after
// Service.ExecuteInits runs the package's variable-init function.
type pendingVarBridge struct {
	// importPath identifies the symbol-registry package the bridged vars belong to.
	importPath string

	// vars carries the slot+type entries recorded for each exported var by
	// compileSinglePackage.
	vars []packageVarExport
}

// typeCheckOptions adjusts checkPackageTypes for its caller.
type typeCheckOptions struct {
	// interpretedPaths holds the import paths compiled from source in this batch, which get
	// no symbol-table hint, or nil.
	interpretedPaths map[string]bool

	// allowUnusedImports accepts imports nothing uses, as a session does for the imports it
	// keeps from earlier submissions.
	allowUnusedImports bool
}

// Compile parses, type-checks, and compiles Go source code into a CompiledFunction
// without executing it. The returned function can be executed multiple times via Execute.
//
// Takes code (string) which contains the Go source code to compile.
//
// Returns *CompiledFunction which is the compiled representation.
// Returns error when parsing, type-checking, or compilation fails.
func (s *Service) Compile(ctx context.Context, code string) (*program.CompiledFunction, error) {
	if err := ctx.Err(); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationCancelled, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err))
	}
	if err := s.checkSourceSize(len(code)); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}
	if err := rejectGoEmbedSource(evalFileName, code); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	ctx = logging.ContextWithLogger(ctx, s.config.logger)

	_, err := parser.ParseExprFrom(s.fileSet, evalFileName, code, 0)
	if err == nil {
		result, compileErr := s.compileExpression(ctx, code)
		if compileErr != nil {
			return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf("compiling source: %w", compileErr))
		}
		return result, nil
	}

	wrappedCode := "package main\nfunc _eval_() {\n" + code + "\n}"
	file, parseErr := parser.ParseFile(s.fileSet, evalFileName, wrappedCode, 0)
	if parseErr == nil {
		result, compileErr := s.compileFile(ctx, file)
		if compileErr == nil {
			return result, nil
		}

		if mixedResult, mixedErr := s.compileMixed(ctx, code); mixedErr == nil {
			return mixedResult, nil
		}
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf("compiling source: %w", compileErr))
	}

	result, compileErr := s.compileMixed(ctx, code)
	if compileErr != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf("compiling source: %w", compileErr))
	}
	return result, nil
}

// CompileFileSet parses and type-checks multiple Go source files as a single package,
// returning a CompiledFileSet that can be executed many times via ExecuteEntrypoint.
//
// Takes sources (map[string]string) where keys are filenames (used for error reporting
// and deterministic ordering) and values are source code strings.
//
// Returns *CompiledFileSet which holds all compiled functions.
// Returns error when parsing, type-checking, or compilation fails; a compiler panic is
// reported as fault.ErrCompilePanic, as for CompileProgram.
func (s *Service) CompileFileSet(ctx context.Context, sources map[string]string) (compiled *program.CompiledFileSet, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logCompilePanic(ctx, recovered)
			compiled = nil
			err = engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf("%w: %v", fault.ErrCompilePanic, recovered))
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationCancelled, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err))
	}
	if err := s.checkSourceSize(fileSetSourceBytes(sources)); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	ctx = logging.ContextWithLogger(ctx, s.config.logger)

	s.applyEnvOverrides()

	files, err := s.parseAndValidateFileSet(ctx, sources)
	if err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf(errFmtCompilingFileSet, err))
	}

	info := frontend.NewTypesInfo()
	typesPackage, err := s.checkPackageTypes(mainPackageName, files, info, typeCheckOptions{interpretedPaths: nil, allowUnusedImports: false})
	if err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err))
	}

	rootFunction := program.NewNamedFunction("<fileset>")
	c := s.newFileSetCompiler(ctx, rootFunction, info)

	c.RegisterPackageLevelVarsFromFiles(ctx, files)

	if err := c.TwoPassCompileFuncs(ctx, files, ""); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf(errFmtCompilingFileSet, err))
	}

	variableInitialisationFunction, err := c.CompileVariableInitFunction(ctx, files)
	if err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf(errFmtCompilingFileSet, err))
	}

	if err := s.runPostCompilationChecks(ctx, rootFunction, errFmtCompilingFileSet); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}

	entrypoints := make(map[string]uint16, len(c.FunctionTable()))
	maps.Copy(entrypoints, c.FunctionTable())

	pretouchCompileTimeDerivedState(rootFunction)

	cfs := program.NewCompiledFileSet(
		rootFunction, entrypoints, c.InitFunctionIndices(), variableInitialisationFunction)
	s.bindDebugProgram(cfs, []debug.PackageBinding{mainPackageBinding(typesPackage, c)})
	return cfs, nil
}

// mainPackageBinding describes a single-package program for the debugger.
//
// Takes typesPackage (*types.Package) which is the type-checked package.
// Takes c (*compile.Compiler) which compiled it.
//
// Returns debug.PackageBinding for the main package.
func mainPackageBinding(typesPackage *types.Package, c *compile.Compiler) debug.PackageBinding {
	return debug.PackageBinding{
		Package:         typesPackage,
		FunctionTable:   c.FunctionTable(),
		GlobalVariables: c.GlobalVariables(),
		Path:            "",
	}
}

// bindDebugProgram hands a freshly compiled program's type and slot information to the
// attached debugger, so it can evaluate expressions in paused frames and verify
// breakpoint lines. A no-op without a debugger.
//
// Takes cfs (*program.CompiledFileSet) which is the compiled program.
// Takes packages ([]debug.PackageBinding) which describe its packages.
func (s *Service) bindDebugProgram(cfs *program.CompiledFileSet, packages []debug.PackageBinding) {
	if s.config == nil || s.config.debugger == nil {
		return
	}
	s.config.debugger.BindProgram(&debug.ProgramBinding{
		FileSet:  s.fileSet,
		Compiled: cfs,
		Symbols:  s.symbols,
		Globals:  s.globals,
		Packages: packages,
		Compiler: s.packageCompilerConfig(cfs.Root(), nil),
		Limits:   s.limits,
	})
}

// CompileProgram compiles multiple packages from source, resolving import dependencies
// and wiring cross-package calls via the symbol registry.
//
// Takes modulePath (string) which is the module path (e.g. "testpkg").
// Takes packages (map[string]map[string]string) which maps relative package paths to
// filename-to-source maps.
//
// Returns *CompiledFileSet which contains all compiled functions.
// Returns error when parsing, type-checking, or compilation fails.
func (s *Service) CompileProgram(ctx context.Context, modulePath string, packages map[string]map[string]string) (*program.CompiledFileSet, error) {
	if err := ctx.Err(); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationCancelled, fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, err))
	}
	if err := s.checkSourceSize(countSourceBytes(packages)); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}
	ctx, cancel := s.applyMaxExecutionTime(ctx)
	defer cancel()
	ctx = logging.ContextWithLogger(ctx, s.config.logger)

	s.applyEnvOverrides()

	set, err := s.parseAndValidateImports(ctx, modulePath, packages)
	if err != nil {
		return nil, programFailure(set.problems, err)
	}
	parsed := set.parsed

	order, err := topoSort(buildDependencyGraph(parsed))
	if err != nil {
		return nil, programFailure(set.problems, err)
	}

	rootFunction := program.NewNamedFunction("<program>")
	crossPackageMethods := make(map[string]uint16)
	var allInitFunctions []uint16
	var mainFunctionTable map[string]uint16
	var lastFunctionTable map[string]uint16

	if s.config != nil && s.config.compilationSnapshotCallback != nil {
		defer func() {
			entrypoints := resolveEntrypoints(mainFunctionTable, lastFunctionTable)
			s.config.compilationSnapshotCallback(
				program.NewCompiledFileSet(rootFunction, entrypoints, allInitFunctions, nil))
		}()
	}

	compiled, err := s.compileOrderedPackages(ctx, order, set, rootFunction, crossPackageMethods)
	allInitFunctions, mainFunctionTable, lastFunctionTable = compiled.initFunctions, compiled.mainFunctionTable, compiled.lastFunctionTable
	if failure := programFailure(set.problems, err); failure != nil {
		return nil, failure
	}

	if err := s.runPostCompilationChecks(ctx, rootFunction, errFmtCompilingProgram); err != nil {
		return nil, engine.NewSafeError(safeMessageCompilationFailed, err)
	}

	pretouchCompileTimeDerivedState(rootFunction)

	cfs := program.NewCompiledFileSet(
		rootFunction, resolveEntrypoints(mainFunctionTable, lastFunctionTable), allInitFunctions, nil)
	s.bindDebugProgram(cfs, compiled.bindings)
	return cfs, nil
}

// parseAllPackages parses and filters files for every package in a multi-package
// compilation. A package that fails does not stop the others.
//
// Takes modulePath (string) which is the module path prefix.
// Takes packages (map[string]map[string]string) which maps relative package paths to
// filename-to-source maps.
//
// Returns packageSet which holds the parsed packages and the problems of the failed ones.
// Returns error which wraps fault.ErrExecutionCancelled when ctx ends.
func (s *Service) parseAllPackages(ctx context.Context, modulePath string, packages map[string]map[string]string) (packageSet, error) {
	set := packageSet{
		parsed:   make(map[string]*parsedPackage, len(packages)),
		failed:   make(map[string]struct{}),
		problems: nil,
	}
	for _, relPath := range slices.Sorted(maps.Keys(packages)) {
		importPath := modulePath
		if relPath != "" {
			importPath = modulePath + "/" + relPath
		}

		pkg, err := s.parseSinglePackage(ctx, importPath, relPath, packages[relPath])
		if errors.Is(err, fault.ErrExecutionCancelled) {
			return set, err
		}
		if err != nil {
			set.failed[importPath] = struct{}{}
			set.problems = append(set.problems, err)
			continue
		}
		if pkg != nil {
			set.parsed[importPath] = pkg
		}
	}
	return set, nil
}

// parseSinglePackage parses the source files of one package that the interpreter
// compiles. A //go:embed directive is rejected only in a file that survives the build
// filter, so a file guarded by //go:build !interp may embed.
//
// Takes importPath (string) which is the fully qualified import path.
// Takes relPath (string) which is the relative path within the module.
// Takes sources (map[string]string) which maps filenames to source.
//
// Returns *parsedPackage which holds the filtered AST files, or nil when all files are
// excluded by build constraints.
// Returns error which names the package, when a file's build constraints are malformed, a
// file fails to parse, or a file embeds; or which wraps fault.ErrExecutionCancelled when
// ctx ends.
func (s *Service) parseSinglePackage(ctx context.Context, importPath, relPath string, sources map[string]string) (*parsedPackage, error) {
	files, err := s.parseSelectedFiles(ctx, sources)
	if errors.Is(err, fault.ErrExecutionCancelled) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrParse, importPath, err)
	}
	if err := rejectGoEmbed(s.fileSet, importPath, files); err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, nil
	}

	return &parsedPackage{
		importPath:  importPath,
		relPath:     relPath,
		packageName: files[0].Name.Name,
		files:       files,
	}, nil
}

// compileSinglePackage type-checks and compiles a single package, sharing the provided
// root function and seeding with cross-package method entries from previously compiled
// packages.
//
// Takes pkg (*parsedPackage) which is the parsed package to compile.
// Takes rootFunction (*CompiledFunction) which is the shared root function.
// Takes crossPackageMethods (map[string]uint16) which holds method entries from
// previously compiled packages.
// Takes interpretedPaths (map[string]bool) which holds import paths of packages compiled
// from source in this batch, used to distinguish them from native packages when enriching
// type-check errors.
//
// Returns *packageCompileResult which holds the compiled output.
// Returns error when type-checking or compilation fails.
func (s *Service) compileSinglePackage(
	ctx context.Context,
	pkg *parsedPackage,
	rootFunction *program.CompiledFunction,
	crossPackageMethods map[string]uint16,
	interpretedPaths map[string]bool,
) (result *packageCompileResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logCompilePanic(ctx, recovered)
			result = nil
			err = fmt.Errorf("%w: %s: %v", fault.ErrCompilePanic, pkg.importPath, recovered)
		}
	}()
	info := frontend.NewTypesInfo()
	typesPackage, err := s.checkPackageTypes(pkg.importPath, pkg.files, info, typeCheckOptions{interpretedPaths: interpretedPaths, allowUnusedImports: false})
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrTypeCheck, pkg.importPath, err)
	}

	s.applyResourceLimits(rootFunction)
	compilation, err := compile.CompileParsedPackage(ctx, s.packageCompilerConfig(rootFunction, info), pkg.files, pkg.importPath, crossPackageMethods)
	if err != nil {
		return nil, err
	}
	c := compilation.Compiler
	initIndices := compilation.InitFunctionIndices

	exportedVars := collectExportedVars(ctx, c, typesPackage)

	return &packageCompileResult{
		functionTable:       c.FunctionTable(),
		globalVariables:     c.GlobalVariables(),
		initFunctionIndices: initIndices,
		info:                info,
		typesPackage:        typesPackage,
		rootFunction:        rootFunction,
		exportedVars:        exportedVars,
	}, nil
}

// checkPackageTypes type-checks a package or snippet, collecting up to
// maxTypeErrorsPerPackage errors with diagnostic hints and rejecting oversize function
// signatures.
//
// Takes packagePath (string) which is the package path handed to go/types.
// Takes files ([]*ast.File) which are the package's files.
// Takes info (*types.Info) which receives the type information.
// Takes options (typeCheckOptions) which adjust the check.
//
// Returns *types.Package which is the checked package.
// Returns error which joins the package's type errors, the first one first, or nil.
func (s *Service) checkPackageTypes(packagePath string, files []*ast.File, info *types.Info, options typeCheckOptions) (*types.Package, error) {
	typeErrors := newCappedErrors(maxTypeErrorsPerPackage)
	typesConfig := s.newTypesConfig()
	typesConfig.DisableUnusedImportCheck = options.allowUnusedImports
	typesConfig.Error = func(err error) {
		if !typeErrors.full() {
			err = s.enrichTypeCheckError(err, files, options.interpretedPaths)
		}
		typeErrors.add(err)
	}
	typesPackage, err := typesConfig.Check(packagePath, s.fileSet, files, info)
	sizeErr := rejectOversizeSignatures(s.fileSet, files)
	if err == nil {
		return typesPackage, sizeErr
	}
	if typeErrors.empty() {
		typeErrors.add(s.enrichTypeCheckError(err, files, options.interpretedPaths))
	}
	return typesPackage, errors.Join(typeErrors.join(errTooManyTypeErrors), sizeErr)
}

// packageCompilerConfig builds the compiler configuration compileSinglePackage hands to
// compile.CompileParsedPackage: the service's file set, symbol registry, globals, feature
// set, optimisation options and limits, with the package's type information and root
// function.
//
// Takes rootFunction (*CompiledFunction) which is the package root.
// Takes info (*types.Info) which carries the go/types data.
//
// Returns compile.CompilerConfig which is ready for compile.CompileParsedPackage.
func (s *Service) packageCompilerConfig(rootFunction *program.CompiledFunction, info *types.Info) compile.CompilerConfig {
	return compile.CompilerConfig{
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
	}
}

// bridgePackageExports registers exported functions and constants from a compiled
// non-main package into the symbol registry so that subsequent packages can import them.
//
// Takes importPath (string) which is the package's import path.
// Takes result (*packageCompileResult) which holds the compiled package output.
func (s *Service) bridgePackageExports(importPath string, result *packageCompileResult) {
	exports := collectPackageExports(result)

	s.symbols.RegisterPackage(importPath, exports)
	s.symbols.RegisterTypesPackage(importPath, result.typesPackage)

	if len(result.exportedVars) > 0 {
		s.pendingVarBridges = append(s.pendingVarBridges, pendingVarBridge{
			importPath: importPath,
			vars:       result.exportedVars,
		})
	}

	publishExternalMethods(s.globals, result.rootFunction)
}

// finalisePendingVarBridges reads the post-init value of every recorded exported var from
// GlobalStore and merges them into the symbol registry under each package's import path.
//
// Called from Service.ExecuteInits after all init functions (including each package's
// synthesised <varinit>) have executed, so the values are guaranteed to reflect the
// user's initialisers rather than zero values.
func (s *Service) finalisePendingVarBridges() {
	if len(s.pendingVarBridges) == 0 {
		return
	}
	for _, pending := range s.pendingVarBridges {
		for _, varExport := range pending.vars {
			value, ok := s.globals.SnapshotVarAs(varExport.slot, varExport.reflectType)
			if !ok {
				continue
			}
			if !varExport.storage.IsValid() || !varExport.storage.CanSet() {
				continue
			}
			varExport.storage.Set(value)
		}
	}
	s.pendingVarBridges = nil
}

// enrichTypeCheckError inspects a type-check error for "undefined: X.Y" patterns where X
// is a package alias that resolves to a native package registered in the symbol registry.
// When the package is registered but the symbol Y is not, the error is enriched with a
// hint suggesting re-extraction via "pipit extract".
//
// Takes err (error) which is the original error from go/types.
// Takes files ([]*ast.File) which are the AST files being type-checked.
// Takes interpretedPaths (map[string]bool) which holds import paths of packages compiled
// from source in this batch. These are excluded from the hint because they are not
// managed by the extract command.
//
// Returns the original error unchanged when no enrichment applies, or a wrapped error
// with a diagnostic hint appended.
func (s *Service) enrichTypeCheckError(err error, files []*ast.File, interpretedPaths map[string]bool) error {
	if s.symbols == nil {
		return err
	}

	var typesErr types.Error
	if !errors.As(err, &typesErr) {
		return err
	}

	message := typesErr.Msg

	if strings.HasPrefix(message, couldNotImportPrefix) && strings.Contains(message, notRegisteredMarker) {
		return enrichMissingPackageError(err, message)
	}

	if !strings.HasPrefix(message, undefinedPrefix) {
		return err
	}

	identifier := strings.TrimPrefix(message, undefinedPrefix)
	packageAlias, symbolName, hasDot := strings.Cut(identifier, ".")
	if !hasDot {
		return s.enrichRenamedPackageError(err, identifier, files)
	}

	aliases := buildImportAliasMap(files, s.symbols.PackageName)
	fullPath, found := aliases[packageAlias]
	if !found {
		return err
	}

	if interpretedPaths[fullPath] {
		return err
	}

	if !s.symbols.HasPackage(fullPath) {
		return err
	}

	if _, registered := s.symbols.Lookup(fullPath, symbolName); registered {
		return err
	}

	return fmt.Errorf(
		"%w - symbol %q is not registered in the symbol registry for package %q; "+
			"you may need to re-run \"pipit extract generate\" (or \"piko extract generate\") "+
			"to update symbol exports",
		err, symbolName, fullPath,
	)
}

// enrichRenamedPackageError explains an "undefined: X" error where X is the last element
// of an unaliased import whose registered package has another name, which happens when a
// symbol table records no name and the package declares one other than the name goimports
// assumes.
//
// Takes err (error) which is the original error from go/types.
// Takes identifier (string) which is the undefined name.
// Takes files ([]*ast.File) which are the AST files being type-checked.
//
// Returns the original error unchanged when no import explains it, or a wrapped error
// with a hint appended.
func (s *Service) enrichRenamedPackageError(err error, identifier string, files []*ast.File) error {
	for _, file := range files {
		for _, importSpec := range file.Imports {
			if importSpec.Name != nil {
				continue
			}
			importPath, pathErr := importPathOf(importSpec)
			if pathErr != nil || path.Base(importPath) != identifier || !s.symbols.HasPackage(importPath) {
				continue
			}
			name := s.symbols.PackageName(importPath)
			if name == identifier {
				continue
			}
			return fmt.Errorf(
				"%w - package %q is known as %q, not %q; regenerate its symbol table with "+
					"\"pipit extract generate\" (or \"piko extract generate\"), which records the name a "+
					"package declares, add a PackageNameSymbol entry to a hand-written table, or "+
					"import it with an explicit name: import %s %q",
				err, importPath, name, identifier, identifier, importPath,
			)
		}
	}
	return err
}

// parseAndValidateImports parses every package and enforces the import policy. Imports
// within the source batch are treated as local, including packages that failed to
// parse.
//
// Takes modulePath (string) which is the module's root import path.
// Takes packages (map[string]map[string]string) which maps each package's relative path
// to its filename-to-source map.
//
// Returns packageSet which holds the parsed packages and the parse problems.
// Returns error which holds a problem for each package with imports the policy refuses,
// or which wraps fault.ErrExecutionCancelled when ctx ends.
func (s *Service) parseAndValidateImports(ctx context.Context, modulePath string, packages map[string]map[string]string) (packageSet, error) {
	set, err := s.parseAllPackages(ctx, modulePath, packages)
	if err != nil {
		return set, err
	}
	if err := s.checkImports(set); err != nil {
		return set, err
	}
	return set, nil
}

// parseAndValidateFileSet parses, filters and import-checks a single-package file set.
// Its imports are all external for policy checks.
//
// Takes sources (map[string]string) which maps each filename to its source code.
//
// Returns []*ast.File which are the parsed, build-tag-filtered files.
// Returns error when parsing fails, an import violates the policy, or ctx ends.
func (s *Service) parseAndValidateFileSet(ctx context.Context, sources map[string]string) ([]*ast.File, error) {
	files, err := s.parseAndFilterFiles(ctx, sources)
	if err != nil {
		return nil, err
	}
	var refused []string
	for _, file := range files {
		refused = append(refused, s.refusedImports(nil, file)...)
	}
	if err := errImportNotPermitted("", refused); err != nil {
		return nil, err
	}
	return files, nil
}

// checkImports rejects denied paths and external imports absent from an active
// allowlist.
//
// Takes set (packageSet) which holds the parsed packages and the import paths of those
// that failed, all of which count as local.
//
// Returns error which holds one problem for each package with refused imports, naming
// every refused import, or nil.
func (s *Service) checkImports(set packageSet) error {
	if len(s.config.deniedImports) == 0 && len(s.config.allowedImports) == 0 {
		return nil
	}
	local := make(map[string]*parsedPackage, len(set.parsed)+len(set.failed))
	maps.Copy(local, set.parsed)
	for importPath := range set.failed {
		local[importPath] = nil
	}
	var problems []error
	for _, importPath := range slices.Sorted(maps.Keys(set.parsed)) {
		var refused []string
		for _, file := range set.parsed[importPath].files {
			refused = append(refused, s.refusedImports(local, file)...)
		}
		if err := errImportNotPermitted(importPath, refused); err != nil {
			problems = append(problems, err)
		}
	}
	return fault.JoinProblems(problems)
}

// checkFileImports applies the import policy to one file's imports.
//
// Takes parsed (map[string]*parsedPackage) which holds the local packages compiled in
// this batch, or nil when the file has no local siblings.
// Takes file (*ast.File) which is the parsed source file to check.
//
// Returns error naming every import that violates the denylist or allowlist.
func (s *Service) checkFileImports(parsed map[string]*parsedPackage, file *ast.File) error {
	return errImportNotPermitted("", s.refusedImports(parsed, file))
}

// refusedImports returns the imports the policy rejects. Denied paths remain forbidden
// even when a local package has the same name.
//
// Takes parsed (map[string]*parsedPackage) which holds the local packages compiled in
// this batch, or nil when the file has no local siblings.
// Takes file (*ast.File) which is the parsed source file to check.
//
// Returns []string which holds the refused import paths in source order. A path literal
// that does not unquote is refused as written, so a policy never lets it through.
func (s *Service) refusedImports(parsed map[string]*parsedPackage, file *ast.File) []string {
	if len(s.config.deniedImports) == 0 && len(s.config.allowedImports) == 0 {
		return nil
	}
	var refused []string
	for _, importSpec := range file.Imports {
		importPath, err := importPathOf(importSpec)
		if err != nil {
			refused = append(refused, importSpec.Path.Value)
			continue
		}
		if _, denied := s.config.deniedImports[importPath]; denied {
			refused = append(refused, importPath)
			continue
		}
		if len(s.config.allowedImports) == 0 {
			continue
		}
		if _, isLocal := parsed[importPath]; isLocal {
			continue
		}
		if _, allowed := s.config.allowedImports[importPath]; !allowed {
			refused = append(refused, importPath)
		}
	}
	return refused
}

// checkSourceSize returns an error if the total source code size exceeds the configured
// maximum.
//
// Takes totalBytes (int) which is the total source code size in bytes to validate.
//
// Returns error when the size exceeds the configured limit, or nil.
func (s *Service) checkSourceSize(totalBytes int) error {
	if s.config != nil && s.config.maxSourceSize > 0 && totalBytes > s.config.maxSourceSize {
		return fmt.Errorf("%w: %d bytes exceeds limit %d", fault.ErrSourceSizeLimit, totalBytes, s.config.maxSourceSize)
	}
	return nil
}

// collectExportedVars walks the package scope and records every exported package-level
// var's name, global-store slot, and Go type. The values are not yet initialised;
// finalisePendingVarBridges writes them into the symbol registry after ExecuteInits.
//
// Takes c (*Compiler) which provides the globalVariables map and the types.Type to
// reflect.Type translator.
// Takes typesPackage (*types.Package) which supplies the scope walked for exported
// identifiers.
//
// Returns the export entries in deterministic name order.
func collectExportedVars(ctx context.Context, c *compile.Compiler, typesPackage *types.Package) []packageVarExport {
	if c == nil || typesPackage == nil || typesPackage.Scope() == nil {
		return nil
	}
	scope := typesPackage.Scope()
	names := scope.Names()
	slices.Sort(names)

	exports := make([]packageVarExport, 0, len(names))
	for _, name := range names {
		object := scope.Lookup(name)
		varObject, ok := object.(*types.Var)
		if !ok || !varObject.Exported() {
			continue
		}
		slot, ok := c.GlobalVariables()[name]
		if !ok {
			continue
		}
		reflectType := c.TypeToReflect(ctx, varObject.Type())
		if reflectType == nil {
			continue
		}

		storage := reflect.New(reflectType).Elem()
		exports = append(exports, packageVarExport{
			name:        name,
			slot:        slot,
			reflectType: reflectType,
			storage:     storage,
		})
	}
	return exports
}

// collectPackageExports gathers a package's exported symbols.
//
// Gathers exported function closures, constants, and var storages into a single
// reflect.Value-keyed map suitable for SymbolRegistry.RegisterPackage. Var storages are
// advertised even before their initialisers have run so a downstream package can capture
// the storage's reflect.Value during type-check; finalisePendingVarBridges rewrites them
// post-init.
//
// Takes result (*packageCompileResult) which holds the compiled package output.
//
// Returns map[string]reflect.Value which keys exports by symbol name.
func collectPackageExports(result *packageCompileResult) map[string]reflect.Value {
	exports := make(map[string]reflect.Value)
	for name, index := range result.functionTable {
		if !ast.IsExported(name) || strings.Contains(name, ".") {
			continue
		}
		closure := engine.NewRuntimeClosure(program.ExportFunctions(result.rootFunction)[index], result.rootFunction)
		exports[name] = reflect.ValueOf(closure)
	}
	for _, typeObject := range result.info.Defs {
		if typeObject == nil || !typeObject.Exported() {
			continue
		}
		constantDecl, ok := typeObject.(*types.Const)
		if !ok {
			continue
		}
		if _, already := exports[constantDecl.Name()]; already {
			continue
		}
		exports[constantDecl.Name()] = constantToReflectValue(constantDecl.Val())
	}
	for _, varExport := range result.exportedVars {
		exports[varExport.name] = varExport.storage
	}
	return exports
}

// publishExternalMethods copies each "TypeName.MethodName" entry on rootFunction's
// methodTable into the Service-shared external method registry so a later CompileProgram
// for a different package can resolve cross-package adapter dispatch. No-op when there is
// nothing to publish or the globals slot is absent.
//
// Takes globals (*GlobalStore) which holds the cross-package method registry.
// Takes rootFunction (*CompiledFunction) which carries the local methodTable.
func publishExternalMethods(globals *engine.GlobalStore, rootFunction *program.CompiledFunction) {
	if rootFunction == nil || globals == nil || len(rootFunction.MethodTable()) == 0 {
		return
	}
	for key, methodIndex := range rootFunction.MethodTable() {
		globals.RegisterExternalMethod(key, rootFunction, methodIndex)
	}
}

// importPathOf returns the canonical import path of an import spec.
//
// Takes spec (*ast.ImportSpec) which is the import declaration to decode.
//
// Returns string which is the unquoted import path.
// Returns error when the path literal is malformed.
func importPathOf(spec *ast.ImportSpec) (string, error) {
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return "", fmt.Errorf("%w: malformed import path %s", fault.ErrParse, spec.Path.Value)
	}
	return importPath, nil
}

// errImportNotPermitted reports the imports the denylist or allowlist rejected.
//
// Takes packagePath (string) which names the importing package; empty for a file set or a
// snippet.
// Takes importPaths ([]string) which are the rejected import paths, in any order.
//
// Returns error which wraps fault.ErrParse and names every rejected path once, or nil
// when importPaths is empty.
func errImportNotPermitted(packagePath string, importPaths []string) error {
	if len(importPaths) == 0 {
		return nil
	}
	paths := slices.Compact(slices.Sorted(slices.Values(importPaths)))
	quoted := make([]string, len(paths))
	for index, importPath := range paths {
		quoted[index] = strconv.Quote(importPath)
	}
	subject := "import " + quoted[0] + " is"
	if len(quoted) > 1 {
		subject = "imports " + strings.Join(quoted, ", ") + " are"
	}
	if packagePath == "" {
		return fmt.Errorf("%w: %s not permitted", fault.ErrParse, subject)
	}
	return fmt.Errorf("%w: %s: %s not permitted", fault.ErrParse, packagePath, subject)
}

// buildDependencyGraph returns an import graph mapping each package to its local
// (source-defined) dependencies.
//
// Takes parsed (map[string]*parsedPackage) which maps import paths to their parsed
// package data.
//
// Returns map[string][]string which maps each import path to its local dependency import
// paths.
func buildDependencyGraph(parsed map[string]*parsedPackage) map[string][]string {
	deps := make(map[string][]string, len(parsed))
	for importPath, pkg := range parsed {
		var localDeps []string
		for _, file := range pkg.files {
			for _, imp := range file.Imports {
				impPath, err := importPathOf(imp)
				if err != nil {
					continue
				}
				if _, isLocal := parsed[impPath]; isLocal {
					localDeps = append(localDeps, impPath)
				}
			}
		}
		deps[importPath] = localDeps
	}
	return deps
}

// collectCrossPackageMethods copies method entries (names containing ".") from a
// package's functionTable into the cross-package map.
//
// Takes functionTable (map[string]uint16) which is the compiled package's function table.
// Takes crossPackageMethods (map[string]uint16) which is the shared cross-package method
// accumulator.
func collectCrossPackageMethods(functionTable, crossPackageMethods map[string]uint16) {
	for name, index := range functionTable {
		if strings.Contains(name, ".") {
			crossPackageMethods[name] = index
		}
	}
}

// topoSort performs a topological sort of the dependency graph, returning packages in
// dependency order (dependencies before dependents).
//
// Takes deps (map[string][]string) which maps each package import path to its dependency
// import paths.
//
// Returns []string which is the sorted import order.
// Returns error when a dependency cycle is detected; wraps fault.ErrCyclicImport.
func topoSort(deps map[string][]string) ([]string, error) {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2

		maxImportGraphDepth = 1024
	)

	state := make(map[string]int, len(deps))
	var order []string

	var visit func(string, int) error
	visit = func(node string, depth int) error {
		if depth > maxImportGraphDepth {
			return fmt.Errorf("%w: import graph depth at %s exceeded %d", fault.ErrCompileDepthLimit, node, maxImportGraphDepth)
		}
		switch state[node] {
		case visiting:
			return fmt.Errorf("%w: %s", fault.ErrCyclicImport, node)
		case visited:
			return nil
		}

		state[node] = visiting
		for _, dependency := range deps[node] {
			if err := visit(dependency, depth+1); err != nil {
				return err
			}
		}
		state[node] = visited
		order = append(order, node)
		return nil
	}

	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		if err := visit(k, 0); err != nil {
			return nil, err
		}
	}

	return order, nil
}

// resolveEntrypoints determines the entrypoint function table from the main package,
// falling back to the last compiled package when no explicit main package exists.
//
// Takes mainFunctionTable (map[string]uint16) which is the main package's function table,
// or nil if no main package was compiled.
// Takes lastFunctionTable (map[string]uint16) which is the last compiled package's
// function table.
//
// Returns map[string]uint16 which maps entrypoint names to function indices, or nil when
// no entrypoints are available.
func resolveEntrypoints(mainFunctionTable, lastFunctionTable map[string]uint16) map[string]uint16 {
	if mainFunctionTable == nil && lastFunctionTable != nil {
		mainFunctionTable = make(map[string]uint16, len(lastFunctionTable))
		maps.Copy(mainFunctionTable, lastFunctionTable)
	}
	if mainFunctionTable == nil {
		return nil
	}
	entrypoints := make(map[string]uint16, len(mainFunctionTable))
	maps.Copy(entrypoints, mainFunctionTable)
	return entrypoints
}

// constantToReflectValue converts a go/constant.Value to a reflect.Value using the
// constant's kind to select the appropriate Go type.
//
// Takes value (constant.Value) which is the constant to convert.
//
// Returns reflect.Value which holds the converted value, or reflect.ValueOf(nil) for
// unhandled constant kinds.
func constantToReflectValue(value constant.Value) reflect.Value {
	switch value.Kind() {
	case constant.Int:
		if v, ok := constant.Int64Val(value); ok {
			return reflect.ValueOf(int(v))
		}
		return reflect.ValueOf(0)
	case constant.Float:
		if v, ok := constant.Float64Val(value); ok {
			return reflect.ValueOf(v)
		}
		return reflect.ValueOf(0.0)
	case constant.String:
		return reflect.ValueOf(constant.StringVal(value))
	case constant.Bool:
		return reflect.ValueOf(constant.BoolVal(value))
	default:
		return reflect.ValueOf(nil)
	}
}

// buildImportAliasMap maps import bindings to full paths. Unaliased imports use the
// declared package name, which may differ from the final path element.
//
// Takes files ([]*ast.File) which are the parsed source files to scan.
// Takes packageName (func(string) string) which names an unaliased import; nil selects
// symtab.AssumedPackageName.
//
// Returns map[string]string mapping each local alias to its fully qualified import
// path. Blank imports ("_"), dot imports (".") and imports with no usable name are
// excluded.
func buildImportAliasMap(files []*ast.File, packageName func(string) string) map[string]string {
	if packageName == nil {
		packageName = symtab.AssumedPackageName
	}
	aliases := make(map[string]string)
	for _, file := range files {
		for _, importSpec := range file.Imports {
			if name, importPath, ok := importBinding(importSpec, packageName); ok {
				aliases[name] = importPath
			}
		}
	}
	return aliases
}

// importBinding returns the name an import binds in its file and the path it imports.
//
// Takes importSpec (*ast.ImportSpec) which is the import.
// Takes packageName (func(string) string) which names an unaliased import.
//
// Returns name (string) which is the bound name.
// Returns importPath (string) which is the import path.
// Returns ok (bool) which is false for a blank or dot import, an invalid path, or an
// import with no usable name.
func importBinding(importSpec *ast.ImportSpec, packageName func(string) string) (name, importPath string, ok bool) {
	importPath, err := importPathOf(importSpec)
	if err != nil {
		return "", "", false
	}
	if importSpec.Name != nil {
		name = importSpec.Name.Name
	} else {
		name = packageName(importPath)
	}
	if name == "" || name == "_" || name == "." {
		return "", "", false
	}
	return name, importPath, true
}

// enrichMissingPackageError appends an actionable hint to the original type-check error
// when the underlying cause is a package missing from the symbol registry.
//
// Standard-library paths hint at the WithStandardLibrary option; other paths hint at the
// extract command.
//
// Takes err (error) which is the unwrapped types.Error returned by go/types.
// Takes message (string) which is the types.Error.Msg, already matched against
// couldNotImportPrefix and notRegisteredMarker.
//
// Returns the original error wrapped with a trailing hint block.
func enrichMissingPackageError(err error, message string) error {
	importPath := extractMissingPackagePath(message)
	if importPath == "" {
		return errors.Join(fault.ErrPackageNotInRegistry, fmt.Errorf(
			"%w\n\nhint: an interpreter registers no packages until a host asks; pass "+
				"stdlib.WithStandardLibrary() to NewInterpreter for the Go standard library",
			err,
		))
	}
	if stdlibindex.Has(importPath) {
		return errors.Join(fault.ErrPackageNotInRegistry, fmt.Errorf(
			"%w\n\nhint: %q is part of the Go standard library, which is opt-in. Pass "+
				"stdlib.WithStandardLibrary() to NewInterpreter, or the bundle that carries it "+
				"(pipit.sh/pipit/sdk/stdlib)",
			err, importPath,
		))
	}
	return errors.Join(fault.ErrPackageNotInRegistry, fmt.Errorf(
		"%w\n\nhint: add %q to the symbol manifest and run \"pipit extract generate\" "+
			"(or \"piko extract generate\"), or run \"pipit extract discover\" to find all "+
			"missing registrations",
		err, importPath,
	))
}

// extractMissingPackagePath pulls the import path out of the "could not import X (package
// \"X\" not registered with interpreter)" message shape emitted by go/types when the
// Importer returns fault.ErrPackageNotInRegistry. Returns empty string when the message
// does not match the expected shape.
//
// Takes message (string) which is the types.Error.Msg text.
//
// Returns the extracted import path or an empty string.
func extractMissingPackagePath(message string) string {
	if !strings.HasPrefix(message, couldNotImportPrefix) {
		return ""
	}
	rest := message[len(couldNotImportPrefix):]
	end := strings.IndexByte(rest, ' ')
	if end <= 0 {
		return ""
	}
	return rest[:end]
}

// pretouchCompileTimeDerivedState eagerly builds per-function structures (ASM call-info
// tables and precomputed alloc counts) at compile time, so the first execution does not
// pay to build them lazily.
//
// Takes rootFunction (*CompiledFunction) which is the fileset root produced by the
// Compiler.
func pretouchCompileTimeDerivedState(rootFunction *program.CompiledFunction) {
	if rootFunction == nil {
		return
	}
	engine.EnsureASMCallInfoTables(rootFunction)
	rootFunction.EnsurePrecomputedAllocCounts()
	for _, child := range program.ExportFunctions(rootFunction) {
		if child == nil {
			continue
		}
		child.EnsurePrecomputedAllocCounts()
	}
}

// programFailure is the error CompileProgram returns for the problems found so far and a
// later failure, so the later failure never hides the earlier problems.
//
// Takes problems ([]error) which are the problems found while parsing; never modified.
// Takes err (error) which is the later failure, or nil.
//
// Returns error which is nil when there is nothing to report.
func programFailure(problems []error, err error) error {
	joined := fault.JoinProblems(slices.Concat(problems, []error{err}))
	if joined == nil {
		return nil
	}
	return engine.NewSafeError(safeMessageCompilationFailed, fmt.Errorf(errFmtCompilingProgram, joined))
}

// fileSetSourceBytes totals the byte length of the source files of one package.
//
// Takes sources (map[string]string) which maps file names to source.
//
// Returns int which is the total byte count.
func fileSetSourceBytes(sources map[string]string) int {
	total := 0
	for _, source := range sources {
		total += len(source)
	}
	return total
}

// countSourceBytes totals the byte length across all source files in a multi-package
// compilation input.
//
// Takes packages (map[string]map[string]string) which maps package paths to
// filename-to-source maps.
//
// Returns int which is the total byte count across all source files.
func countSourceBytes(packages map[string]map[string]string) int {
	total := 0
	for _, files := range packages {
		total += fileSetSourceBytes(files)
	}
	return total
}

// logCompilePanic logs a recovered compiler panic with its stack, once, so the error
// returned for it can stay short.
//
// Takes recovered (any) which is the value recover returned.
func logCompilePanic(ctx context.Context, recovered any) {
	logging.LoggerFrom(ctx).Warn("Compiler panicked",
		slog.String("panic", fmt.Sprint(recovered)),
		slog.String("stack", string(runtimedebug.Stack())))
}

// cancellationError returns the error compilation reports when ctx has ended, or nil
// while it is live.
//
// Returns error which wraps fault.ErrExecutionCancelled and the reason ctx ended.
func cancellationError(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	cause := context.Cause(ctx)
	if errors.Is(cause, fault.ErrExecutionCancelled) {
		return cause
	}
	return fmt.Errorf(fault.ErrChainFmt, fault.ErrExecutionCancelled, cause)
}
