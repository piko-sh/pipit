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

//go:build integration

package selfhost_test

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/selfhost"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	compileImportPath    = "pipit.sh/pipit/internal/compile"
	corpusPackagePath    = "main"
	caseTimeout          = 3 * time.Minute
	interpretedCallDepth = 100000
)

const driverSource = `package main

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

func CompilePackage(
	ctx context.Context, fset *token.FileSet, info *types.Info, root *program.CompiledFunction,
	symbols *symtab.SymbolRegistry, globals *engine.GlobalStore, options passes.Options,
	features policy.InterpFeature, files []*ast.File, importPath string, sink map[string]any,
) error {
	compilation, err := compile.CompileParsedPackage(ctx, compile.CompilerConfig{
		FileSet:            fset,
		Info:               info,
		Function:           root,
		RootFunction:       root,
		ScopeName:          "<root>",
		FunctionTable:      nil,
		GlobalVariables:    nil,
		Symbols:            symbols,
		Globals:            globals,
		Features:           features,
		Passes:             options,
		Patterns:           nil,
		MaxLiteralElements: 0,
		MaxExpressionDepth: 0,
		DebugEnabled:       false,
	}, files, importPath, nil)
	if err != nil {
		return err
	}
	sink["inits"] = compilation.InitFunctionIndices
	sink["table"] = compilation.Compiler.FunctionTable()
	return nil
}
`

type options struct {
	corpus           string
	compiler         string
	report           string
	dump             string
	disasm           string
	filter           string
	traceLowerings   string
	limit            int
	verbose          bool
	goDispatch       bool
	noPasses         bool
	noArenaPromotion bool
	noGC             bool
}

type hostOptions struct {
	compilerDir      string
	traceLogger      *slog.Logger
	goDispatch       bool
	noPasses         bool
	noArenaPromotion bool
	noGC             bool
}

type caseOutcome string

const (
	outcomeIdentical    caseOutcome = "identical"
	outcomeMismatch     caseOutcome = "mismatch"
	outcomeInterpreted  caseOutcome = "interp-error"
	outcomeNativeFailed caseOutcome = "native-error"
	outcomeUnparsed     caseOutcome = "unparsed"
)

const dumpDirectoryMode = 0o750

const dumpFileMode = 0o600

func parseOptions(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("selfhost", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var opts options
	flags.StringVar(&opts.corpus, "corpus", "tests/integration/snippets/testdata", "directory whose */eval.go files are compiled")
	flags.StringVar(&opts.compiler, "compiler", "internal/compile", "directory holding the compiler's sources")
	flags.StringVar(&opts.report, "report", "docs/selfhost-report.md", "markdown report to write")
	flags.StringVar(&opts.filter, "filter", "", "comma-separated fragments; only programs whose directory name contains one are compiled")
	flags.IntVar(&opts.limit, "limit", 0, "compile only the first N programs (0 = all)")
	flags.BoolVar(&opts.verbose, "v", false, "print one line per program")
	flags.BoolVar(&opts.goDispatch, "go-dispatch", true, "run the interpreted compiler on the pure Go dispatcher rather than the assembly one")
	flags.StringVar(&opts.dump, "dump", "", "directory that receives both listings and both packed programs of each mismatch, and the panic stack plus innermost listing of each interpreted failure")
	flags.StringVar(&opts.disasm, "disasm", "", "print the interpreted compiler's listing of the named function, with source lines, and exit")
	flags.StringVar(&opts.traceLowerings, "trace-lowerings", "", "file that receives both compilers' lowering decisions (needs PIPIT_TRACE_LOWERING=1 in the environment)")
	flags.BoolVar(&opts.noPasses, "no-passes", false, "compile the compiler itself without optimisation passes (isolates optimiser faults from interpretation faults)")
	flags.BoolVar(&opts.noArenaPromotion, "no-arena-promotion", false, "compile the compiler itself without arena annotations, so every allocation takes the heap path")
	flags.BoolVar(&opts.noGC, "no-gc", false, "never run the register arena's minor collection while the interpreted compiler runs")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	status, _ := runWithResults(args, stdout, stderr)
	return status
}

func runWithResults(args []string, stdout, stderr io.Writer) (int, []caseResult) {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		return 2, nil
	}

	started := time.Now()
	ctx := context.Background()
	traceLogger, closeTrace, err := openTraceLogger(opts.traceLowerings, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "selfhost: %v\n", err)
		return 1, nil
	}
	defer closeTrace()
	host, err := newHost(ctx, hostOptions{
		compilerDir: opts.compiler, traceLogger: traceLogger, goDispatch: opts.goDispatch,
		noPasses: opts.noPasses, noArenaPromotion: opts.noArenaPromotion, noGC: opts.noGC,
	})
	if err != nil {
		fmt.Fprintf(stderr, "selfhost: %v\n", err)
		return 1, nil
	}
	setup := time.Since(started)
	if opts.disasm != "" {
		if err := host.printListing(stdout, opts.disasm); err != nil {
			fmt.Fprintf(stderr, "selfhost: %v\n", err)
			return 1, nil
		}
		return 0, nil
	}

	cases, err := corpusFiles(opts.corpus, splitList(opts.filter), opts.limit)
	if err != nil {
		fmt.Fprintf(stderr, "selfhost: %v\n", err)
		return 1, nil
	}
	results := make([]caseResult, 0, len(cases))
	host.dump = opts.dump
	for _, path := range cases {
		result := host.runCase(ctx, path)
		results = append(results, result)
		if opts.verbose {
			fmt.Fprintf(stdout, "%-12s %s\n", result.outcome, result.name)
		}
	}
	summary := summarise(results, setup, time.Since(started))
	if err := os.WriteFile(opts.report, []byte(renderReport(summary, results)), dumpFileMode); err != nil {
		fmt.Fprintf(stderr, "selfhost: writing report: %v\n", err)
		return 1, results
	}
	_, _ = fmt.Fprint(stdout, summary.String())
	fmt.Fprintf(stdout, "report: %s\n", opts.report)
	if summary.identical != summary.compared {
		return 1, results
	}
	return 0, results
}

func openTraceLogger(path string, stderr io.Writer) (*slog.Logger, func(), error) {
	if path == "" {
		return nil, func() {}, nil
	}
	if os.Getenv("PIPIT_TRACE_LOWERING") == "" {
		fmt.Fprintln(stderr, "selfhost: -trace-lowerings needs PIPIT_TRACE_LOWERING=1 in the environment; the trace will be empty")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, dumpFileMode)
	if err != nil {
		return nil, func() {}, fmt.Errorf("creating the lowering trace: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{AddSource: false, Level: slog.LevelDebug, ReplaceAttr: nil}))
	return logger, func() { _ = file.Close() }, nil
}

func splitList(raw string) []string {
	var parts []string
	for part := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func matchesFilter(name string, fragments []string) bool {
	if len(fragments) == 0 {
		return true
	}
	for _, fragment := range fragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

type host struct {
	interpreter       *app.Service
	compiled          *pipit.CompiledFileSet
	driver            *pipit.CompiledFileSet
	symbols           *symtab.SymbolRegistry
	nativeLogger      *slog.Logger
	interpretedLogger *slog.Logger
	dump              string
}

func newHost(ctx context.Context, hostOpts hostOptions) (*host, error) {
	providers := append(stdlib.Providers(), selfhost.NewProvider())
	interpreter := app.NewService(interpreterOptionsFor(hostOpts)...)
	interpreter.UseSymbolProviders(providers...)

	var nativeLogger, interpretedLogger *slog.Logger
	if hostOpts.traceLogger != nil {
		nativeLogger = hostOpts.traceLogger.With("side", "native")
		interpretedLogger = hostOpts.traceLogger.With("side", "interpreted")
	}

	sources, err := packageSources(hostOpts.compilerDir)
	if err != nil {
		return nil, err
	}

	compiled, err := interpreter.CompileProgram(ctx, compileImportPath, map[string]map[string]string{"": sources})
	if err != nil {
		return nil, fmt.Errorf("compiling %s under the interpreter: %w", compileImportPath, err)
	}
	if err := interpreter.ExecuteInits(ctx, compiled); err != nil {
		return nil, fmt.Errorf("initialising %s: %w", compileImportPath, err)
	}

	driver, err := interpreter.CompileProgram(ctx, corpusPackagePath, map[string]map[string]string{"": {"driver.go": driverSource}})
	if err != nil {
		return nil, fmt.Errorf("compiling the driver: %w", err)
	}
	if err := interpreter.ExecuteInits(ctx, driver); err != nil {
		return nil, fmt.Errorf("initialising the driver: %w", err)
	}
	return &host{
		interpreter: interpreter, compiled: compiled, driver: driver, symbols: newRegistry(providers),
		nativeLogger: nativeLogger, interpretedLogger: interpretedLogger, dump: "",
	}, nil
}

func interpreterOptionsFor(hostOpts hostOptions) []pipit.Option {
	interpreterOptions := []pipit.Option{pipit.WithMaxCallDepth(interpretedCallDepth)}
	if hostOpts.goDispatch {
		interpreterOptions = append(interpreterOptions, pipit.WithForceGoDispatch())
	}
	if hostOpts.noGC {
		interpreterOptions = append(interpreterOptions, app.WithMinorGCDisabled())
	}
	if hostOpts.noPasses || hostOpts.noArenaPromotion {
		optimisations := passes.DefaultOptions()
		if hostOpts.noPasses {
			optimisations = passes.Options{
				BCE: false, CSE: false, GVN: false, LICM: false, UnrollSelfRecursive: false, SIMDKernels: false, LoopUnroll: false,
				ArenaPromotion: true,
			}
		}
		optimisations.ArenaPromotion = !hostOpts.noArenaPromotion
		interpreterOptions = append(interpreterOptions, app.WithOptimisations(optimisations))
	}
	return interpreterOptions
}

func newRegistry(providers []symtab.SymbolProviderPort) *symtab.SymbolRegistry {
	exports := symtab.NewCompositeSymbolProvider(providers...).Exports()
	delete(exports, policy.PkgUnsafe)
	registry := symtab.NewSymbolRegistry(exports)
	registry.ProtectPackage(policy.PkgUnsafe)
	for _, provider := range providers {
		if typed, ok := provider.(program.TypesPackageProviderPort); ok {
			for path, pkg := range typed.TypesPackages() {
				registry.RegisterTypesPackage(path, pkg)
			}
		}
	}
	registry.SynthesiseAll()
	return registry
}

func packageSources(directory string) (map[string]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		matched, err := build.Default.MatchFile(directory, name)
		if err != nil || !matched {
			continue
		}
		body, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		sources[name] = string(body)
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no Go files in %s", directory)
	}
	return sources, nil
}

func corpusFiles(directory string, filter []string, limit int) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(directory, "*", "eval.go"))
	if err != nil {
		return nil, err
	}
	slices.Sort(matches)
	matches = slices.DeleteFunc(matches, func(path string) bool {
		return !matchesFilter(filepath.Base(filepath.Dir(path)), filter)
	})
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no */eval.go programs under %s", directory)
	}
	return matches, nil
}

type caseResult struct {
	name                        string
	outcome                     caseOutcome
	detail                      string
	nativeTime, interpretedTime time.Duration
}

func (h *host) runCase(ctx context.Context, path string) caseResult {
	name := filepath.Base(filepath.Dir(path))
	fset := token.NewFileSet()
	source, err := os.ReadFile(path)
	if err != nil {
		return caseResult{name: name, outcome: outcomeUnparsed, detail: err.Error(), nativeTime: 0, interpretedTime: 0}
	}
	file, err := parser.ParseFile(fset, "main.go", source, parser.ParseComments)
	if err != nil {
		return caseResult{name: name, outcome: outcomeUnparsed, detail: err.Error(), nativeTime: 0, interpretedTime: 0}
	}
	info := newTypesInfo()
	config := &types.Config{Sizes: types.SizesFor("gc", "amd64"), GoVersion: engine.InterpretedGoVersion, Importer: h.symbols}
	if _, err := config.Check(corpusPackagePath, fset, []*ast.File{file}, info); err != nil {
		return caseResult{name: name, outcome: outcomeNativeFailed, detail: "type check: " + err.Error(), nativeTime: 0, interpretedTime: 0}
	}

	nativeStart := time.Now()
	nativePacked, err := h.compileNatively(ctx, fset, info, []*ast.File{file}, corpusPackagePath)
	nativeTime := time.Since(nativeStart)
	if err != nil {
		return caseResult{name: name, outcome: outcomeNativeFailed, detail: err.Error(), nativeTime: nativeTime, interpretedTime: 0}
	}

	interpretedStart := time.Now()
	interpretedPacked, disassembly, err := h.compileInterpreted(ctx, fset, info, []*ast.File{file}, corpusPackagePath)
	interpretedTime := time.Since(interpretedStart)
	if err != nil {
		detail := err.Error()
		stack := pipit.UncaughtPanicStack(err)
		if stack != "" {
			detail += "\n\ninterpreted stack:\n" + stack
		}
		h.dumpFailure(name, detail, stack)
		return caseResult{name: name, outcome: outcomeInterpreted, detail: detail, nativeTime: nativeTime, interpretedTime: interpretedTime}
	}
	if bytes.Equal(nativePacked.bytes, interpretedPacked) {
		return caseResult{name: name, outcome: outcomeIdentical, detail: "", nativeTime: nativeTime, interpretedTime: interpretedTime}
	}
	h.dumpListings(name, nativePacked.disassembly, disassembly)
	h.dumpBytes(name, nativePacked.bytes, interpretedPacked)
	return caseResult{name: name, outcome: outcomeMismatch, detail: firstDifference(nativePacked.disassembly, disassembly), nativeTime: nativeTime, interpretedTime: interpretedTime}
}

func newTypesInfo() *types.Info {
	return &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Instances:    make(map[*ast.Ident]types.Instance),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    make(map[ast.Node]types.Object),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:       make(map[ast.Node]*types.Scope),
		InitOrder:    nil,
		FileVersions: make(map[*ast.File]string),
	}
}

type packedProgram struct {
	disassembly string
	bytes       []byte
}

func (h *host) compileNatively(ctx context.Context, fset *token.FileSet, info *types.Info, files []*ast.File, packagePath string) (packedProgram, error) {
	if h.nativeLogger != nil {
		ctx = logging.ContextWithLogger(ctx, h.nativeLogger)
	}
	root := program.NewNamedFunction("<program>")
	config := compile.CompilerConfig{
		FileSet: fset, Info: info, Function: root, RootFunction: root, ScopeName: "<root>",
		FunctionTable: nil, GlobalVariables: nil, Symbols: h.symbols, Globals: engine.NewGlobalStore(),
		Features: policy.InterpFeaturesAll, Passes: passes.DefaultOptions(), Patterns: nil,
		MaxLiteralElements: 0, MaxExpressionDepth: 0, DebugEnabled: false,
	}
	compilation, err := compile.CompileParsedPackage(ctx, config, files, packagePath, nil)
	if err != nil {
		return packedProgram{bytes: nil, disassembly: ""}, err
	}
	return pack(root, compilation.Compiler.FunctionTable(), compilation.InitFunctionIndices), nil
}

func (h *host) compileInterpreted(ctx context.Context, fset *token.FileSet, info *types.Info, files []*ast.File, packagePath string) ([]byte, string, error) {
	ctx, cancel := context.WithTimeout(ctx, caseTimeout)
	defer cancel()
	if h.interpretedLogger != nil {
		ctx = logging.ContextWithLogger(ctx, h.interpretedLogger)
	}
	root := program.NewNamedFunction("<program>")
	sink := make(map[string]any, 2)
	result, err := h.interpreter.CallFunction(ctx, h.driver, "CompilePackage",
		ctx, fset, info, root, h.symbols, engine.NewGlobalStore(), passes.DefaultOptions(), policy.InterpFeaturesAll,
		files, packagePath, sink)
	if err != nil {
		return nil, "", err
	}
	if failure, ok := result.(error); ok && failure != nil {
		return nil, "", failure
	}
	inits, initsOK := sink["inits"].([]uint16)
	if !initsOK && sink["inits"] != nil {
		return nil, "", fmt.Errorf("the interpreted compiler reported an init list of type %T", sink["inits"])
	}
	table, ok := sink["table"].(map[string]uint16)
	if !ok {
		return nil, "", errors.New("the interpreted compiler reported no function table")
	}
	packed := pack(root, table, inits)
	return packed.bytes, packed.disassembly, nil
}

func pack(root *program.CompiledFunction, table map[string]uint16, inits []uint16) packedProgram {
	if len(inits) == 0 {
		inits = nil
	}
	entrypoints := make(map[string]uint16, len(table))
	maps.Copy(entrypoints, table)
	fileSet := program.NewCompiledFileSet(root, entrypoints, inits, nil)
	return packedProgram{bytes: pipit.PackCompiledFileSetToBytes(fileSet), disassembly: disassemble(fileSet)}
}

func (h *host) dumpListings(name, native, interpreted string) {
	if h.dump == "" {
		return
	}
	_ = os.MkdirAll(h.dump, dumpDirectoryMode)
	_ = os.WriteFile(filepath.Join(h.dump, name+".native.pkasm"), []byte(native), dumpFileMode)
	_ = os.WriteFile(filepath.Join(h.dump, name+".interpreted.pkasm"), []byte(interpreted), dumpFileMode)
}

func (h *host) dumpBytes(name string, native, interpreted []byte) {
	if h.dump == "" {
		return
	}
	_ = os.MkdirAll(h.dump, dumpDirectoryMode)
	_ = os.WriteFile(filepath.Join(h.dump, name+".native.pbc"), native, dumpFileMode)
	_ = os.WriteFile(filepath.Join(h.dump, name+".interpreted.pbc"), interpreted, dumpFileMode)
}

func (h *host) dumpFailure(name, detail, stack string) {
	if h.dump == "" {
		return
	}
	_ = os.MkdirAll(h.dump, dumpDirectoryMode)
	var out strings.Builder
	out.WriteString(detail)
	if function := innermostCompilerFunction(stack); function != "" {
		out.WriteString("\n\ninnermost compiler function: " + function + "\n\n")
		if err := h.printListing(&out, function); err != nil {
			out.WriteString(err.Error() + "\n")
		}
	}
	_ = os.WriteFile(filepath.Join(h.dump, name+".interpreted.panic.txt"), []byte(out.String()), dumpFileMode)
}

func innermostCompilerFunction(stack string) string {
	for line := range strings.SplitSeq(stack, "\n") {
		frame := strings.TrimSpace(line)
		if !strings.HasPrefix(frame, compileImportPath+".") {
			continue
		}
		frame = strings.TrimSuffix(frame, "(...)")
		if index := strings.LastIndexByte(frame, '.'); index >= 0 {
			frame = frame[index+1:]
		}
		if frame != "" {
			return frame
		}
	}
	return ""
}
