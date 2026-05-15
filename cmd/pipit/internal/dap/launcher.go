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

package dap

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pipitmodules "pipit.sh/pipit/sdk/module"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/modloader"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
	"pipit.sh/pipit/internal/debug"
)

const (
	// outputScanInitialBuffer is the starting capacity of the script-output scan buffer.
	outputScanInitialBuffer = 4096

	// outputScanMaxBuffer caps a single line of captured script output.
	outputScanMaxBuffer = 1 << 20
)

// launchArguments is the launch-request body the IDE sends. The field set deliberately
// mirrors `pipit run` flags so operators can translate one to the other without
// translation tables.
type launchArguments struct {
	// Program is the absolute path to the script to debug (required). May be a single .go
	// file or a directory.
	Program string `json:"program"`

	// GoproxyURL overrides the default GOPROXY base, same as `pipit run --goproxy`.
	GoproxyURL string `json:"goproxy,omitempty"`

	// Cache mirrors `pipit run --cache` (off | on | home | gopath | <path>). Default: "off".
	Cache string `json:"cache,omitempty"`

	// Entrypoint names the function to invoke once the program is compiled. Default: "main".
	Entrypoint string `json:"entrypoint,omitempty"`

	// Args are forwarded as positional script arguments, mirroring `pipit run -- <args>`
	// semantics.
	Args []string `json:"args,omitempty"`

	// AllowNetwork mirrors `pipit run --allow-network`.
	AllowNetwork bool `json:"allowNetwork,omitempty"`

	// StopOnEntry pauses at the first instruction of the entrypoint when true. The IDE
	// typically wires this to a "Pause on entry" checkbox in the Run/Debug config.
	StopOnEntry bool `json:"stopOnEntry,omitempty"`
}

// launcher owns the goroutines that compile + run the script under [pipit.Debugger]
// control. Lifetime spans from the DAP `configurationDone` request (which kicks off the
// run) to either the script's natural completion, an unrecoverable error, or a
// `disconnect` request from the client.
type launcher struct {
	// debugger is the session's [pipit.Debugger], attached to the interpreter the launcher
	// builds.
	debugger *pipit.Debugger

	// done is closed when the run goroutine returns (with or without an error). Used by stop
	// to wait for clean teardown.
	done chan struct{}

	// runCancel cancels the context the script runs under; nil until start.
	runCancel context.CancelFunc

	// args holds the parsed launch arguments. Preserved so future restart support can rerun
	// with the same config.
	args launchArguments

	// stopOnce ensures the stop sequence runs at most once.
	stopOnce sync.Once
}

// start compiles the script and assembles the interpreter synchronously, then spawns the
// run and pause goroutines.
//
// Takes s (*server) which owns the DAP session and breakpoint store.
//
// Returns error when reading, parsing, or compiling the program fails.
//
// Concurrency: on success it launches the run and pause goroutines, which outlive the
// call and terminate on stop, ctx cancellation, or program exit.
func (l *launcher) start(ctx context.Context, s *server) error {
	source, err := os.ReadFile(l.args.Program)
	if err != nil {
		return fmt.Errorf("read program: %w", err)
	}

	cacheSpec, err := modloader.ParseCacheSpec(l.args.Cache)
	if err != nil {
		return fmt.Errorf("parse cache spec: %w", err)
	}

	debugger := l.debugger

	limits := runner.Limits{
		Symbols:       s.opts.Symbols,
		BytecodeStore: nil, Logger: nil, Timeout: 0, MaxAlloc: 0, MaxCallDepth: 0, MaxOutputSize: 0, CostBudget: 0, MaxGoroutines: 0}
	extras := []pipit.Option{pipit.WithDebugger(debugger)}
	if len(l.args.Args) > 0 {
		extras = append(extras, pipit.WithEnv(buildScriptEnv(l.args.Args)))
	}
	interpreter := runner.New(limits, extras...)

	external, err := scriptHasExternalImports(l.args.Program, source)
	if err != nil {
		return fmt.Errorf("parse imports: %w", err)
	}

	var compiled *pipit.CompiledFileSet
	if external {
		compiled, err = l.prepareModuleProgram(ctx, interpreter, cacheSpec)
		if err != nil {
			return fmt.Errorf("prepare modules: %w", err)
		}
	} else {
		mainPath := l.args.Program
		compiled, err = interpreter.CompileProgram(ctx, mainPath, map[string]map[string]string{
			"": {mainPath: string(source)},
		})
		if err != nil {
			return fmt.Errorf("compile program: %w", err)
		}
	}

	debugger.SetPauseOnEntry(l.args.StopOnEntry)

	runCtx, runCancel := context.WithCancel(ctx)
	eventsCtx, eventsCancel := context.WithCancel(ctx)
	l.runCancel = runCancel
	go l.runLoop(runCtx, s, interpreter, compiled, eventsCancel)
	go l.eventLoop(eventsCtx, s)
	return nil
}

// prepareModuleProgram resolves third-party imports and compiles the program against the
// populated symbol registry.
//
// Takes interpreter (*pipit.Interpreter) which receives the loaded modules.
// Takes cacheSpec (modloader.CacheSpec) which selects the bytecode cache root.
//
// Returns *CompiledFileSet which is the compiled main program.
// Returns error when resolution, packaging, loading, or compilation fails.
func (l *launcher) prepareModuleProgram(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	cacheSpec modloader.CacheSpec,
) (*pipit.CompiledFileSet, error) {
	cacheRoot, appliedCacheMode := modloader.ResolveCacheSpec(cacheSpec, l.args.Program, io.Discard)

	resolution, err := modloader.ResolveScript(ctx, l.args.Program, modloader.ScriptResolverOptions{
		AllowNetwork:     l.args.AllowNetwork,
		GoproxyURL:       l.args.GoproxyURL,
		StdlibPackages:   modloader.StdlibPackageSet(),
		CacheRoot:        cacheRoot,
		AppliedCacheMode: appliedCacheMode,
		HTTPClient:       nil,
		RequiredVersions: nil,
		Snapshot:         nil,
	})
	if err != nil {
		return nil, err
	}

	bytecodeRoot := ""
	if cacheRoot != "" {
		bytecodeRoot = filepath.Join(cacheRoot, "bytecode")
	}
	bytecodeCache := modloader.NewBytecodeCache(bytecodeRoot).WithIdentity(modloader.ResolvedCacheIdentity(resolution.Modules))

	for _, module := range resolution.Modules {
		for _, relPath := range module.PackageOrder {
			if err := loadModulePackage(ctx, interpreter, bytecodeCache, module, relPath); err != nil {
				return nil, err
			}
		}
	}

	mainPackages := map[string]map[string]string{"": resolution.MainSources}
	compiled, err := interpreter.CompileProgram(ctx, resolution.MainPackage, mainPackages)
	if err != nil {
		return nil, err
	}
	return compiled, nil
}

// runLoop drives the interpreter on its own goroutine and reports the exit.
//
// Takes s (*server) which receives output and lifecycle events.
// Takes interpreter (*pipit.Interpreter) which executes the compiled program.
// Takes compiled (*pipit.CompiledFileSet) which is the program to execute.
// Takes eventsCancel (context.CancelFunc) which ends the event loop once the run is over.
func (l *launcher) runLoop(ctx context.Context, s *server, interpreter *pipit.Interpreter, compiled *pipit.CompiledFileSet, eventsCancel context.CancelFunc) {
	defer close(l.done)
	defer eventsCancel()

	restore, pumpsDone := captureProcessStdio(s)
	defer func() {
		restore()
		<-pumpsDone
	}()

	exitCode := 0
	err := interpreter.ExecuteInits(ctx, compiled)
	if err == nil {
		_, err = interpreter.ExecuteEntrypoint(ctx, compiled, l.args.Entrypoint)
	}
	if err != nil && !errors.Is(err, pipit.ErrDebuggerStop) && !errors.Is(err, context.Canceled) {
		exitCode = 1
		s.writeOutput("stderr", "pipit: "+err.Error()+"\n")
	}
	s.clearStop()
	s.emitTerminated(exitCode)
}

// eventLoop relays debugger events to the DAP client as stopped or thread events.
//
// Takes s (*server) which stores the stop state and emits events.
func (l *launcher) eventLoop(ctx context.Context, s *server) {
	for {
		event, err := l.debugger.WaitForEvent(ctx)
		if err != nil {
			return
		}
		switch event.Kind {
		case debug.EventPaused:
			if event.Reason == debug.StopReasonOtherThread {
				continue
			}
			s.setStop(newStopState(event))
			s.emitStopped(event)
		case debug.EventThreadStarted:
			s.emitThread("started", event.ThreadID)
		case debug.EventThreadExited:
			s.emitThread("exited", event.ThreadID)
		default:

			continue
		}
	}
}

// stop ends the script and waits for the run goroutine.
//
// Safe to call multiple times; only the first call runs the stop sequence, which cancels
// the run context and asks the debugger to stop, so a paused program wakes at once and a
// running one stops at its next instruction.
func (l *launcher) stop() {
	if l == nil {
		return
	}
	l.stopOnce.Do(func() {
		if l.runCancel != nil {
			l.runCancel()
		}
		l.debugger.Stop()
	})
	if l.done != nil && l.runCancel != nil {
		<-l.done
	}
}

// loadModulePackage compiles one resolved sub-package and registers it with the
// interpreter, so the debugged program's imports resolve against it.
//
// Sub-packages with no sources are skipped; those directories hold only test files.
//
// Takes interpreter (*pipit.Interpreter) which receives the loaded package.
// Takes cache (*BytecodeCache) which serves and stores the compiled bundle.
// Takes module (ResolvedModule) which is the module the sub-package belongs to.
// Takes relPath (string) which is the sub-package path, empty for the module root.
//
// Returns error when the package fails to compile or load.
func loadModulePackage(
	ctx context.Context,
	interpreter *pipit.Interpreter,
	cache *modloader.BytecodeCache,
	module modloader.ResolvedModule,
	relPath string,
) error {
	sources := module.Packages[relPath]
	if len(sources) == 0 {
		return nil
	}
	importPath := module.Path
	if relPath != "" {
		importPath = module.Path + "/" + relPath
	}
	bundle, _, err := modloader.LoadOrCompilePackage(ctx, interpreter, cache, importPath, module.Version, sources, warnCacheWrite)
	if err != nil {
		return fmt.Errorf("prepare package %s: %w", importPath, err)
	}
	reference := pipitmodules.Ref{
		Path:    importPath,
		Version: module.Version,
		Pin:     bundle.Descriptor.Ref.Pin,
	}
	if _, err := interpreter.LoadModule(ctx, bundle, reference, nil, pipit.LoadCompiledFromBytes); err != nil {
		return fmt.Errorf("load package %s: %w", importPath, err)
	}
	return nil
}

// captureProcessStdio redirects os.Stdout and os.Stderr into DAP output events.
//
// The pipes pump every line into an output event. The returned restore function
// reinstates the original streams, and the returned channel closes once both pumps have
// drained. The DAP server's own writes go through opts.Stdout, which holds the original
// *os.File, so replacing os.Stdout only redirects code that reads it lazily.
//
// Takes s (*server) which receives the captured output events.
//
// Returns func() which restores os.Stdout and os.Stderr and shuts the pipes.
// Returns <-chan struct{} which closes once both pumps have drained.
//
// Concurrency: starts background pump goroutines; run restore before the process reads
// the original streams again.
func captureProcessStdio(s *server) (restore func(), done <-chan struct{}) {
	origStdout := os.Stdout
	origStderr := os.Stderr

	outReader, outWriter, errOut := os.Pipe()
	if errOut != nil {
		fmt.Fprintf(s.opts.Stderr, "pipit dap: cannot create stdout pipe: %v\n", errOut)
		closed := make(chan struct{})
		close(closed)
		return func() {}, closed
	}
	errReader, errWriter, errErr := os.Pipe()
	if errErr != nil {
		fmt.Fprintf(s.opts.Stderr, "pipit dap: cannot create stderr pipe: %v\n", errErr)
		_ = outReader.Close()
		_ = outWriter.Close()
		closed := make(chan struct{})
		close(closed)
		return func() {}, closed
	}

	os.Stdout = outWriter
	os.Stderr = errWriter

	doneCh := make(chan struct{})
	var pumpsWg sync.WaitGroup
	pumpsWg.Add(2)
	go pumpStdio(s, "stdout", outReader, &pumpsWg)
	go pumpStdio(s, "stderr", errReader, &pumpsWg)
	go func() {
		pumpsWg.Wait()
		close(doneCh)
	}()

	restore = func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		_ = outWriter.Close()
		_ = errWriter.Close()
	}
	return restore, doneCh
}

// pumpStdio reads lines from r and emits one DAP output event per line.
//
// It closes wg when the reader hits EOF, which happens once the pipe writer is closed by
// the restore function from captureProcessStdio.
//
// Takes s (*server) which receives the output events.
// Takes category (string) which labels the output stream ("stdout"/"stderr").
// Takes r (io.ReadCloser) which supplies the raw output to scan.
// Takes wg (*sync.WaitGroup) which is marked done when scanning ends.
func pumpStdio(s *server, category string, r io.ReadCloser, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { _ = r.Close() }()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, outputScanInitialBuffer), outputScanMaxBuffer)
	for scanner.Scan() {
		s.writeOutput(category, scanner.Text()+"\n")
	}
}

// buildScriptEnv mirrors cmd_run.go's helper of the same name.
//
// It is duplicated here rather than exported from the cli package to avoid a cli <->
// internal/dap import cycle.
//
// Takes scriptArgs ([]string) which are the positional script arguments.
//
// Returns map[string]string which holds the PIPIT_ARGC and PIPIT_ARG_N vars.
func buildScriptEnv(scriptArgs []string) map[string]string {
	env := make(map[string]string, len(scriptArgs)+1)
	env["PIPIT_ARGC"] = fmt.Sprintf("%d", len(scriptArgs))
	for index, value := range scriptArgs {
		env[fmt.Sprintf("PIPIT_ARG_%d", index)] = value
	}
	return env
}

// scriptHasExternalImports mirrors cmd_run.go's helper of the same name.
//
// Takes path (string) which is the script path used for import extraction.
// Takes source ([]byte) which holds the script's raw source bytes.
//
// Returns bool which is true when a non-stdlib import is present.
// Returns error when the imports cannot be extracted.
func scriptHasExternalImports(path string, source []byte) (bool, error) {
	imports, err := modloader.ExtractImports(path, source)
	if err != nil {
		return false, err
	}
	stdlib := modloader.StdlibPackageSet()
	for _, importPath := range imports {
		if _, ok := stdlib[importPath]; ok {
			continue
		}
		if strings.Contains(importPath, ".") {
			return true, nil
		}
	}
	return false, nil
}

// warnCacheWrite reports a failed bytecode cache write; the write is best effort, so the
// launch continues.
//
// Takes err (error) which describes the failed write.
func warnCacheWrite(err error) {
	fmt.Fprintf(os.Stderr, "pipit dap: warning: %v\n", err)
}
