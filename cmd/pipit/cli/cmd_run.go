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

package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	pipitmodules "pipit.sh/pipit/sdk/module"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
	"pipit.sh/pipit/internal/safeconv"
)

// defaultRunCallDepth is the call-depth limit pipit run applies unless -max-call-depth
// says otherwise.
const defaultRunCallDepth = 1000000

// runFlags holds pointers to every value the `pipit run` flag set parses into.
type runFlags struct {
	// timeout caps wall-clock time per evaluation.
	timeout *time.Duration

	// maxAlloc caps the element count of a single allocation.
	maxAlloc *int

	// maxGoroutines caps concurrent script goroutines.
	maxGoroutines *int

	// maxOutput caps the bytes print and println may write.
	maxOutput *int

	// costBudget is the total instruction-cost budget, 0 for unlimited.
	costBudget *int64

	// maxCallDepth caps the interpreted call stack; scripts recurse as deep as Go programs
	// do, so the default is far above the library's conservative embedded default.
	maxCallDepth *int

	// entrypoint names the function to invoke for a single file.
	entrypoint *string

	// emit is the directory compiled bytecode is written to before running.
	emit *string

	// allowNetwork permits on-demand GOPROXY fetches.
	allowNetwork *bool

	// goproxyURL overrides the GOPROXY base URL.
	goproxyURL *string

	// cacheSpec is the raw --cache value.
	cacheSpec *string

	// gate is the raw --gate capability spec.
	gate *string

	// autoDeny denies unapproved capabilities without prompting.
	autoDeny *bool

	// lockfilePath overrides the capability lockfile location.
	lockfilePath *string
}

// moduleRunOptions carries the third-party-module flags through to runTarget without
// enlarging the dispatch surface.
type moduleRunOptions struct {
	// Snapshot pins gated local inputs; nil preserves the trusted direct-read path.
	Snapshot *modloader.SourceSnapshot

	// Stderr receives diagnostic messages about module resolution and caching.
	Stderr io.Writer

	// CacheSpec selects the bytecode cache mode and optional path.
	CacheSpec modloader.CacheSpec

	// GoproxyURL overrides the Go module proxy URL for dependency fetches.
	GoproxyURL string

	// AllowNetwork permits network access for module downloads.
	AllowNetwork bool
}

// RunRun handles `pipit run <file|dir>`.
//
// Takes args ([]string) which holds the command-line arguments.
// Takes streams (output.IO) which supplies stdin, stdout, and stderr.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func RunRun(ctx context.Context, args []string, streams output.IO) (exitCode int) {
	flags := flag.NewFlagSet("pipit run", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)

	options := registerRunFlags(flags)
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit run [flags] <file|directory> [-- script-argument ...]")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() < 1 {
		flags.Usage()
		return 1
	}

	target, scriptArgs := splitRunTarget(flags)

	limits, err := runLimits(ctx, options)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}

	extras := []pipit.Option{}
	extras = append(extras,
		pipit.WithArgs(append([]string{target}, scriptArgs...)),
		pipit.WithEnv(buildScriptEnv(target, scriptArgs)))

	gateOptions, saveLockfile, snapshot, err := capabilityGateOptions(options, target, scriptArgs, streams)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	extras = append(extras, gateOptions...)
	defer saveRunApprovals(saveLockfile, streams.Stderr, &exitCode)

	cache, err := modloader.ParseCacheSpec(*options.cacheSpec)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}

	moduleOptions := moduleRunOptions{
		Snapshot:     snapshot,
		AllowNetwork: *options.allowNetwork,
		GoproxyURL:   *options.goproxyURL,
		CacheSpec:    cache,
		Stderr:       streams.Stderr,
	}
	stopWatchdog := armExecutionWatchdog(*options.timeout, streams.Stderr)
	defer stopWatchdog()
	if err := runTarget(ctx, runner.New(limits, extras...), target, *options.entrypoint, streams.Stdout, moduleOptions); err != nil {
		return reportExecutionError(streams.Stderr, err)
	}
	return 0
}

// splitRunTarget separates the run target from the script's own arguments. Go's flag
// package stops at the first positional (the target), so a following `--` separator is
// still in Args; it belongs to pipit, not the script.
//
// Takes flags (*flag.FlagSet) which has been parsed.
//
// Returns the target and the script arguments.
func splitRunTarget(flags *flag.FlagSet) (string, []string) {
	scriptArgs := flags.Args()[1:]
	if len(scriptArgs) > 0 && scriptArgs[0] == "--" {
		scriptArgs = scriptArgs[1:]
	}
	return flags.Arg(0), scriptArgs
}

// reportExecutionError prints an execution failure and maps it to the process exit
// status.
//
// Takes stderr (io.Writer) which receives the report.
// Takes err (error) which is the execution error.
//
// Returns int which is the exit status.
func reportExecutionError(stderr io.Writer, err error) int {
	switch {
	case errors.Is(err, pipit.ErrGoroutineJoinTimeout):
		return 0
	case errors.Is(err, pipit.ErrInterpreterInvariant):
		fmt.Fprintf(stderr, "pipit: internal error: %v\n", err)
		return exitInternalError
	case errors.Is(err, pipit.ErrStackOverflow):
		fmt.Fprintf(stderr, "fatal error: %v\n\ngoroutine 1 [running]:\n\t[interpreted by pipit]\n", err)
		return exitPanic
	case errors.Is(err, pipit.ErrDeadlock):
		fmt.Fprintf(stderr, "%v\n\ngoroutine 1 [chan receive]:\n\t[interpreted by pipit]\n", pipit.ErrDeadlock)
		return exitPanic
	case errors.Is(err, pipit.ErrExecutionCancelled):
		fmt.Fprintf(stderr, errorFormat, err)
		return exitTimeout
	case errors.Is(err, pipit.ErrUncaughtPanic):
		if value, ok := pipit.UncaughtPanicValue(err); ok {
			stack := pipit.UncaughtPanicStack(err)
			if stack == "" {
				stack = "\t[interpreted by pipit]\n"
			}
			fmt.Fprintf(stderr, "panic: %v\n\ngoroutine 1 [running]:\n%s", value, stack)
		} else {
			fmt.Fprintf(stderr, errorFormat, err)
		}
		return exitPanic
	default:
		fmt.Fprintf(stderr, errorFormat, err)
		return 1
	}
}

// saveRunApprovals reports approval persistence failures in the command exit status.
//
// Takes save (func() error) which persists approvals, or nil for an ungated run.
// Takes stderr (io.Writer) which receives a diagnostic on failure.
// Takes exitCode (*int) which receives a failing status if persistence fails.
func saveRunApprovals(save func() error, stderr io.Writer, exitCode *int) {
	if save == nil {
		return
	}
	if err := save(); err != nil {
		fmt.Fprintf(stderr, errorFormat, err)
		*exitCode = 1
	}
}

// runLimits builds the interpreter sandbox limits from the parsed flags.
//
// Takes options (*runFlags) which supplies the limit flags and the emit directory.
//
// Returns Limits which holds the configured sandbox limits.
// Returns error when the bytecode emission directory cannot be prepared.
func runLimits(ctx context.Context, options *runFlags) (runner.Limits, error) {
	limits := runner.Limits{
		Timeout:       *options.timeout,
		MaxAlloc:      *options.maxAlloc,
		MaxGoroutines: safeconv.IntToInt32(*options.maxGoroutines),
		MaxOutputSize: *options.maxOutput,
		CostBudget:    *options.costBudget,
		Symbols:       ExtraSymbols(ctx),
		Logger:        loggerFromContext(ctx),
		BytecodeStore: nil,
		MaxCallDepth:  *options.maxCallDepth,
	}
	if *options.emit == "" {
		return limits, nil
	}
	store, err := pipit.NewDirectoryBytecodeStore(*options.emit)
	if err != nil {
		return limits, fmt.Errorf("cannot prepare emission directory: %w", err)
	}
	limits.BytecodeStore = store
	return limits, nil
}

// registerRunFlags declares every `pipit run` flag on flags.
//
// Takes flags (*flag.FlagSet) which receives the flag declarations.
//
// Returns *runFlags which holds the pointers the flags parse into.
func registerRunFlags(flags *flag.FlagSet) *runFlags {
	options := new(runFlags)
	options.timeout = flags.Duration("timeout", 15*time.Minute, "Maximum wall-clock time per evaluation")
	options.maxAlloc = flags.Int("max-alloc", 0, "Maximum element count for a single allocation (0 = default)")
	options.maxGoroutines = flags.Int("max-goroutines", 0, "Maximum concurrent goroutines (0 = default)")
	options.maxOutput = flags.Int("max-output", 0, "Maximum bytes print/println may write (0 = default)")
	options.costBudget = flags.Int64("cost-budget", 0, "Total instruction cost budget (0 = unlimited)")
	options.maxCallDepth = flags.Int("max-call-depth", defaultRunCallDepth, "Maximum interpreted call depth")
	options.entrypoint = flags.String("entrypoint", "main", "Function to invoke when running a single file")
	options.emit = flags.String("emit-bytecode", "", "Directory to write compiled bytecode to before running")
	options.allowNetwork = flags.Bool("allow-network", false, "Allow fetching third-party modules from GOPROXY on demand")
	options.goproxyURL = flags.String("goproxy", "", "Override GOPROXY base URL (default https://proxy.golang.org)")
	options.cacheSpec = flags.String("cache", "off", "Module cache: off | on | home | gopath | <filesystem path (./foo, /abs, ~/foo, C:\\foo)>")
	options.gate = flags.String("gate", "", "Gate capabilities and consult on use: all | network | disk | exec | env (comma-separated); empty leaves everything ungated")
	options.autoDeny = flags.Bool("autodeny", false, "With --gate, deny unapproved capabilities without prompting, even on a terminal")
	options.lockfilePath = flags.String("lockfile", "", "Capability approval path (default user configuration directory/pipit/approvals)")
	return options
}

// capabilityGateOptions builds the capability-hook interpreter options for --gate.
//
// Without --gate it returns no options and no saver, leaving the run ungated. Interactive
// mode (a TTY without --autodeny) additionally permits unpinned module loads, since the
// operator approves each module as it is met; frozen runs keep the pin requirement so a
// lockfile-driven rerun cannot load a bundle whose integrity was never checked.
//
// Takes options (*runFlags) which supplies the gate, autodeny, and lockfile flags.
// Takes target (string) which is the script being run.
// Takes scriptArgs ([]string) which binds saved approval to the invocation arguments.
// Takes streams (output.IO) which supplies the stdin TTY check.
//
// Returns []Option which carries the capability hook and, in interactive mode, the
// unpinned-module opt-in; nil when ungated.
// Returns func() error which persists approvals after the run, nil when ungated.
// Returns *modloader.SourceSnapshot which retains approved local source, nil when
// ungated.
// Returns error when the gate spec is invalid or the lockfile cannot be read.
func capabilityGateOptions(options *runFlags, target string, scriptArgs []string, streams output.IO) ([]pipit.Option, func() error, *modloader.SourceSnapshot, error) {
	gatedAxes, err := modloader.ParseGateSpec(*options.gate)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(gatedAxes) == 0 {
		return nil, nil, nil, nil
	}

	snapshot, err := modloader.CaptureInvocation(target, gatedAxes, *options.entrypoint, scriptArgs)
	if err != nil {
		return nil, nil, nil, err
	}
	lockPath := *options.lockfilePath
	if lockPath == "" {
		lockPath, err = modloader.DefaultApprovalPath(snapshot.Path())
		if err != nil {
			return nil, nil, nil, err
		}
	}
	store := modloader.NewStore(lockPath)
	if err := store.Load(); err != nil {
		return nil, nil, nil, err
	}
	store.SetScript(snapshot.Path(), snapshot.Identity())

	mode := modloader.HookModeFrozen
	var prompter modloader.Prompter
	if streams.IsStdinTTY() && !*options.autoDeny {
		mode = modloader.HookModeInteractive
		prompter = modloader.StdinPrompter{}
	}
	hook := modloader.NewHook(mode, store, prompter)
	hook.Gate(gatedAxes...)
	hook.SetScriptLabel(filepath.Base(target))
	gateOptions := []pipit.Option{pipit.WithCapabilityHook(hook)}
	if mode == modloader.HookModeInteractive {
		gateOptions = append(gateOptions, pipit.WithAllowUnpinnedModules(true))
	}
	return gateOptions, store.Save, snapshot, nil
}

// cacheLabel formats the cache-hit log line so the user sees which kind of cache served
// them. Keyword modes print the mode name; explicit paths print the custom path.
//
// Takes mode (modloader.CacheMode) which selects the cache kind.
// Takes root (string) which is the custom cache filesystem path.
//
// Returns string which labels the cache for the log line.
func cacheLabel(mode modloader.CacheMode, root string) string {
	switch mode {
	case modloader.CacheCustom:
		return root
	default:
		return string(mode)
	}
}

// runTarget chooses single-file or multi-package execution based on whether target is a
// directory.
//
// Takes interpreter (*pipit.Interpreter) which runs the compiled code.
// Takes target (string) which is the file or directory path.
// Takes entrypoint (string) which names the function to invoke.
// Takes stdout (io.Writer) which receives the run result.
// Takes modules (moduleRunOptions) which carries module-fetch options.
//
// Returns error when stat fails or execution returns an error.
func runTarget(ctx context.Context, interpreter *pipit.Interpreter, target, entrypoint string, stdout io.Writer, modules moduleRunOptions) error {
	if modules.Snapshot != nil {
		if modules.Snapshot.IsDirectory() {
			return runDirectorySources(ctx, interpreter, modules.Snapshot.Path(), modules.Snapshot.DirectorySources(), entrypoint, stdout, modules)
		}
		return runSingleFile(ctx, interpreter, modules.Snapshot.Path(), entrypoint, stdout, modules)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("cannot stat %q: %w", target, err)
	}

	if info.IsDir() {
		return runDirectory(ctx, interpreter, target, entrypoint, stdout, modules)
	}
	return runSingleFile(ctx, interpreter, target, entrypoint, stdout, modules)
}

// runSingleFile loads a single .go file and executes the chosen entrypoint.
//
// When the script imports third-party modules, the modloader fetches them via GOPROXY
// first and routes the whole program through CompileProgram. Stdlib-only scripts use the
// fast path.
//
// Takes interpreter (*pipit.Interpreter) which runs the compiled code.
// Takes path (string) which is the .go file path.
// Takes entrypoint (string) which names the function to invoke.
// Takes stdout (io.Writer) which receives the run result.
// Takes modules (moduleRunOptions) which carries module-fetch options.
//
// Returns error when the file cannot be read, compiled, or executed.
func runSingleFile(ctx context.Context, interpreter *pipit.Interpreter, path, entrypoint string, stdout io.Writer, modules moduleRunOptions) error {
	readSource := os.ReadFile
	if modules.Snapshot != nil {
		readSource = modules.Snapshot.ReadFile
	}
	source, err := readSource(path)
	if err != nil {
		return fmt.Errorf("cannot read %q: %w", path, err)
	}

	needsResolution, err := scriptNeedsResolution(filepath.Dir(path), map[string]string{filepath.Base(path): string(source)}, modules.Snapshot)
	if err != nil {
		return fmt.Errorf("parsing imports: %w", err)
	}
	if !needsResolution {
		compiled, err := interpreter.CompileFileSet(ctx, map[string]string{absolutePath(path): string(source)})
		if err != nil {
			return err
		}
		result, err := interpreter.ExecuteEntrypoint(ctx, compiled, entrypoint)
		if err != nil {
			return err
		}
		printResult(stdout, result)
		return nil
	}

	options, cacheRoot := resolverOptions(path, modules)
	resolution, err := modloader.ResolveScript(ctx, path, options)
	if err != nil {
		return err
	}
	return runResolution(ctx, interpreter, resolution, cacheRoot, entrypoint, stdout, modules)
}

// resolverOptions builds the resolver options for a run and resolves the cache root the
// bytecode cache hangs off.
//
// Takes anchor (string) which is a file path whose directory anchors the on-mode cache.
// Takes modules (moduleRunOptions) which carries the module-fetch flags.
//
// Returns modloader.ScriptResolverOptions which configures the resolve.
// Returns string which is the resolved cache root, empty when caching is off.
func resolverOptions(anchor string, modules moduleRunOptions) (modloader.ScriptResolverOptions, string) {
	cacheRoot, appliedCacheMode := modloader.ResolveCacheSpec(modules.CacheSpec, anchor, modules.Stderr)
	return modloader.ScriptResolverOptions{
		Snapshot:         modules.Snapshot,
		AllowNetwork:     modules.AllowNetwork,
		GoproxyURL:       modules.GoproxyURL,
		StdlibPackages:   modloader.StdlibPackageSet(),
		CacheRoot:        cacheRoot,
		AppliedCacheMode: appliedCacheMode,
		HTTPClient:       nil, RequiredVersions: nil}, cacheRoot
}

// compileResolution loads the resolved modules and compiles the program's own package
// against them.
//
// Takes interpreter (*pipit.Interpreter) which receives the modules and compiles the
// program.
// Takes resolution (*modloader.ScriptResolution) which holds the modules and sources.
// Takes cacheRoot (string) which locates the bytecode cache, empty when caching is off.
// Takes modules (moduleRunOptions) which carries the diagnostics writer.
//
// Returns *pipit.CompiledFileSet which is the compiled program.
// Returns error when a module or the program fails to compile.
func compileResolution(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	resolution *modloader.ScriptResolution,
	cacheRoot string,
	modules moduleRunOptions,
) (*pipit.CompiledFileSet, error) {
	reportResolution(modules.Stderr, resolution)

	bytecodeRoot := ""
	if cacheRoot != "" {
		bytecodeRoot = filepath.Join(cacheRoot, "bytecode")
	}
	if err := loadResolvedModules(ctx, interpreter, modloader.NewBytecodeCache(bytecodeRoot), resolution.Modules, modules.Stderr); err != nil {
		return nil, err
	}

	mainPackages := map[string]map[string]string{"": resolution.MainSources}
	return interpreter.CompileProgram(ctx, resolution.MainPackage, mainPackages)
}

// runResolution compiles the resolved program and runs the entrypoint.
//
// Takes interpreter (*pipit.Interpreter) which runs the compiled code.
// Takes resolution (*modloader.ScriptResolution) which holds the modules and sources.
// Takes cacheRoot (string) which locates the bytecode cache, empty when caching is off.
// Takes entrypoint (string) which names the function to invoke.
// Takes stdout (io.Writer) which receives the run result.
// Takes modules (moduleRunOptions) which carries the diagnostics writer.
//
// Returns error when a module or the program fails to compile or execute.
func runResolution(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	resolution *modloader.ScriptResolution,
	cacheRoot, entrypoint string,
	stdout io.Writer,
	modules moduleRunOptions,
) error {
	compiled, err := compileResolution(ctx, interpreter, resolution, cacheRoot, modules)
	if err != nil {
		return err
	}
	if err := interpreter.ExecuteInits(ctx, compiled); err != nil {
		return err
	}
	result, err := interpreter.ExecuteEntrypoint(ctx, compiled, entrypoint)
	if err != nil {
		return err
	}
	printResult(stdout, result)
	return nil
}

// printResult writes the entrypoint's return value when it produced one.
//
// Takes stdout (io.Writer) which receives the formatted value.
// Takes result (any) which is the entrypoint's return value, nil when there was none.
func printResult(stdout io.Writer, result any) {
	if result != nil {
		fmt.Fprintln(stdout, formatResult(result))
	}
}

// reportResolution logs what the module resolve fetched and what it served from cache.
//
// Takes stderr (io.Writer) which receives the summary, nil to stay silent.
// Takes resolution (*ScriptResolution) which holds the fetch and cache-hit lists.
func reportResolution(stderr io.Writer, resolution *modloader.ScriptResolution) {
	if stderr == nil {
		return
	}
	if len(resolution.LocalModules) > 0 {
		fmt.Fprintf(stderr, "pipit: loaded %d local module(s): %s\n",
			len(resolution.LocalModules), strings.Join(resolution.LocalModules, ", "))
	}
	if len(resolution.Fetched) > 0 {
		fmt.Fprintf(stderr, "pipit: fetched %d module(s) from GOPROXY: %s\n",
			len(resolution.Fetched), strings.Join(resolution.Fetched, ", "))
	}
	if len(resolution.CacheHits) > 0 {
		fmt.Fprintf(stderr, "pipit: served %d module(s) from cache (%s): %s\n",
			len(resolution.CacheHits),
			cacheLabel(resolution.AppliedCacheMode, resolution.CacheRoot),
			strings.Join(resolution.CacheHits, ", "))
	}
}

// loadResolvedModules compiles and registers every resolved module package, in dependency
// order, so the main program's imports resolve against them.
//
// Takes interpreter (*pipit.Interpreter) which receives the loaded packages.
// Takes cache (*BytecodeCache) which serves and stores compiled bundles.
// Takes resolved ([]ResolvedModule) which lists the modules in dependency order.
// Takes stderr (io.Writer) which receives cache-hit diagnostics, nil to stay silent.
//
// Returns error when a package fails to compile or load.
func loadResolvedModules(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	cache *modloader.BytecodeCache,
	resolved []modloader.ResolvedModule,
	stderr io.Writer,
) error {
	cache = cache.WithIdentity(modloader.ResolvedCacheIdentity(resolved))
	for _, mod := range resolved {
		for _, relPath := range mod.PackageOrder {
			if err := loadModulePackage(ctx, interpreter, cache, mod, relPath, stderr); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadModulePackage compiles one module sub-package and registers it with the
// interpreter's symbol registry.
//
// Packages with no sources are skipped, which happens for directories that only hold test
// files.
//
// Takes interpreter (*pipit.Interpreter) which receives the loaded package.
// Takes cache (*BytecodeCache) which serves and stores the compiled bundle.
// Takes mod (ResolvedModule) which is the module the sub-package belongs to.
// Takes relPath (string) which is the sub-package path, empty for the module root.
// Takes stderr (io.Writer) which receives cache-hit diagnostics, nil to stay silent.
//
// Returns error when the package fails to compile or load.
func loadModulePackage(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	cache *modloader.BytecodeCache,
	mod modloader.ResolvedModule,
	relPath string,
	stderr io.Writer,
) error {
	sources := mod.Packages[relPath]
	if len(sources) == 0 {
		return nil
	}
	importPath := mod.Path
	if relPath != "" {
		importPath = mod.Path + "/" + relPath
	}
	bundle, hit, err := modloader.LoadOrCompilePackage(ctx, interpreter, cache, importPath, mod.Version, sources, warnCacheWrite)
	if err != nil {
		return fmt.Errorf("preparing package %s: %w", importPath, err)
	}
	reference := pipitmodules.Ref{
		Path:    importPath,
		Version: mod.Version,
		Pin:     bundle.Descriptor.Ref.Pin,
	}
	if _, err := interpreter.LoadModule(ctx, bundle, reference, nil, pipit.LoadCompiledFromBytes); err != nil {
		return fmt.Errorf("loading package %s: %w", importPath, err)
	}
	if hit && stderr != nil {
		fmt.Fprintf(stderr, "pipit: bytecode cache hit %s@%s\n", importPath, mod.Version)
	}
	return nil
}

// scriptNeedsResolution reports whether a program imports anything the module resolver
// must provide: a package of a local module named by the nearest go.mod, or a remote
// module fetched from GOPROXY. Host-provided packages never need it.
//
// Takes directory (string) which is the program's directory, used to find go.mod.
// Takes sources (map[string]string) which maps file name to source.
// Takes snapshot (*modloader.SourceSnapshot) which is nil for the trusted direct-read
// path.
//
// Returns bool which is true when the resolver must run before compilation.
// Returns error when a source cannot be parsed.
func scriptNeedsResolution(directory string, sources map[string]string, snapshot *modloader.SourceSnapshot) (bool, error) {
	var imports []string
	for name, source := range sources {
		fileImports, err := modloader.ExtractImports(name, []byte(source))
		if err != nil {
			return false, err
		}
		imports = append(imports, fileImports...)
	}
	return modloader.ImportsNeedResolution(directory, imports, modloader.StdlibPackageSet(), snapshot), nil
}

// runDirectory loads every .go file in a directory as a single package and executes the
// chosen entrypoint.
//
// Takes interpreter (*pipit.Interpreter) which runs the compiled code.
// Takes directory (string) which is the directory path to load.
// Takes entrypoint (string) which names the function to invoke.
// Takes stdout (io.Writer) which receives the run result.
// Takes modules (moduleRunOptions) which carries module-fetch options.
//
// Returns error when the directory cannot be read or execution fails.
func runDirectory(ctx context.Context, interpreter *pipit.Interpreter, directory, entrypoint string, stdout io.Writer, modules moduleRunOptions) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("cannot read directory %q: %w", directory, err)
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(directory, name)) //nolint:gosec // operator-supplied script directory
		if err != nil {
			return fmt.Errorf("cannot read %q: %w", name, err)
		}
		sources[absolutePath(filepath.Join(directory, name))] = string(content)
	}
	return runDirectorySources(ctx, interpreter, directory, sources, entrypoint, stdout, modules)
}

// absolutePath returns the absolute form of a path, or the path itself when it cannot be
// resolved, so compiled files carry the names Go's runtime would report.
//
// Takes path (string) which is the file path as given.
//
// Returns string which is the absolute path.
func absolutePath(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

// runDirectorySources compiles already acquired source without reopening paths. A package
// that imports a local or remote module goes through the resolver like a single file
// does; otherwise the files compile as one file set.
//
// Takes interpreter (*pipit.Interpreter) which compiles and executes the package.
// Takes directory (string) which is the package directory the sources came from.
// Takes sources (map[string]string) which contains immutable source strings.
// Takes entrypoint (string) which selects execution.
// Takes stdout (io.Writer) which receives the result.
// Takes modules (moduleRunOptions) which carries module-fetch options.
//
// Returns error when compilation or execution fails.
func runDirectorySources(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	directory string,
	sources map[string]string,
	entrypoint string,
	stdout io.Writer,
	modules moduleRunOptions,
) error {
	if len(sources) == 0 {
		return errors.New("no .go files in directory")
	}
	needsResolution, err := scriptNeedsResolution(directory, sources, modules.Snapshot)
	if err != nil {
		return fmt.Errorf("parsing imports: %w", err)
	}
	if needsResolution {
		options, cacheRoot := resolverOptions(filepath.Join(directory, "package"), modules)
		resolution, err := modloader.ResolveDirectory(ctx, directory, sources, options)
		if err != nil {
			return err
		}
		return runResolution(ctx, interpreter, resolution, cacheRoot, entrypoint, stdout, modules)
	}

	compiled, err := interpreter.CompileFileSet(ctx, sources)
	if err != nil {
		return err
	}
	result, err := interpreter.ExecuteEntrypoint(ctx, compiled, entrypoint)
	if err != nil {
		return err
	}
	if result != nil {
		fmt.Fprintln(stdout, formatResult(result))
	}
	return nil
}

// formatResult turns an interpreter return into a printable string.
//
// Mirrors fmt.Sprintf("%v", v) but trims trailing newlines so the caller's Fprintln stays
// consistent.
//
// Takes value (any) which is the interpreter return value.
//
// Returns string which is the printable form with trailing newlines trimmed.
func formatResult(value any) string {
	var builder bytes.Buffer
	fmt.Fprintf(&builder, "%v", value)
	return strings.TrimRight(builder.String(), "\n")
}

// buildScriptEnv builds the variables pipit run overlays on the environment: the
// positional arguments as PIPIT_ARGC and PIPIT_ARG_<n> (the same arguments reach os.Args
// through WithArgs) and the script's directory as PIPIT_SCRIPT_DIR, so a script can find
// files next to itself the way go:embed would have let it.
//
// Takes target (string) which is the file or directory being run.
// Takes scriptArgs ([]string) which holds the positional arguments.
//
// Returns map[string]string which is the environment overlay.
func buildScriptEnv(target string, scriptArgs []string) map[string]string {
	env := make(map[string]string, len(scriptArgs)+2)
	env["PIPIT_ARGC"] = fmt.Sprintf("%d", len(scriptArgs))
	for index, value := range scriptArgs {
		env[fmt.Sprintf("PIPIT_ARG_%d", index)] = value
	}
	env["PIPIT_SCRIPT_DIR"] = scriptDirectory(target)
	return env
}

// scriptDirectory returns the absolute directory a run target lives in: the target itself
// when it is a directory, otherwise its parent. When the path cannot be resolved the
// cleaned relative form is returned so the variable is always set.
//
// Takes target (string) which is the file or directory being run.
//
// Returns string which is the directory.
func scriptDirectory(target string) string {
	directory := target
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		directory = filepath.Dir(target)
	}
	if absolute, err := filepath.Abs(directory); err == nil {
		return absolute
	}
	return filepath.Clean(directory)
}

// warnCacheWrite reports a failed bytecode cache write; the write is best effort, so the
// run continues.
//
// Takes err (error) which describes the failed write.
func warnCacheWrite(err error) {
	fmt.Fprintf(os.Stderr, "pipit: warning: %v\n", err)
}
