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

//go:build crosslang

package harness

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// YaegiRunner drives the yaegi Go interpreter (github.com/traefik/yaegi) as an in-process
// library. Compile is timed separately from execute via Interpreter.Compile / Execute.
// The benchmark's pipit_source.go and native_main.go are merged into a single source
// string before passing to yaegi (its public API does not accept multi-file packages).
type YaegiRunner struct{}

// NewYaegiRunner returns a Runner that drives the yaegi library in-process.
func NewYaegiRunner() *YaegiRunner { return &YaegiRunner{} }

// Kind reports the runner identity used in results.
func (*YaegiRunner) Kind() RunnerKind { return RunnerYaegi }

// Available is always true; the yaegi library is statically linked.
func (*YaegiRunner) Available(context.Context) (bool, string) {
	return true, ""
}

// Close is a no-op.
func (*YaegiRunner) Close(context.Context) error { return nil }

// Run reads the benchmark's pipit_source.go + native_main.go, merges them, compiles via
// yaegi.Interpreter.Compile (timed), then executes (timed). Stdout/stderr from the
// yaegi'd code are captured via pipes.
func (*YaegiRunner) Run(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	srcBytes, err := readBenchmarkFile(benchmarkDir, "go", "pipit_source.go")
	if err != nil {
		return failedResult(spec.Name, RunnerYaegi, mode, "read pipit_source.go: "+err.Error()), nil
	}
	mainBytes, err := readBenchmarkFile(benchmarkDir, "go", "native_main.go")
	if err != nil {
		return failedResult(spec.Name, RunnerYaegi, mode, "read native_main.go: "+err.Error()), nil
	}
	imports := map[string]struct{}{}
	merged := singleFileSource(imports, stripPackageAndImports(string(srcBytes), imports), stripPackageAndImports(string(mainBytes), imports))

	ctx, cancel := context.WithTimeoutCause(parent, time.Duration(spec.TimeoutSeconds)*time.Second,
		fmt.Errorf("yaegi runner %s/%s timed out", spec.Name, mode))
	defer cancel()

	run, runErr := runYaegiSource(ctx, merged, benchmarkDir, mode, spec)
	if runErr != nil {
		return failedResult(spec.Name, RunnerYaegi, mode, runErr.Error()), nil
	}
	innerNanos, _, stderrRemainder := ParseTimingMarkers(run.stderr)
	return checkedResult(spec, RunnerYaegi, mode, run.stdout, stderrRemainder, runTimings{
		wallNanos:    run.wallNanos,
		innerNanos:   innerNanos,
		compileNanos: run.compileNanos,
		peakRSSKB:    pipitSelfPeakRSSKB(),
	}), nil
}

// runYaegiSource captures the process's standard streams, swaps os.Args so the running Go
// code sees `--mode/--k`, changes into the benchmark directory, then compiles and runs
// source with the yaegi library.
//
// Takes source (string) which is the merged program.
// Takes benchmarkDir (string) which is the run's working directory.
// Takes mode (RunMode) which selects the program's mode flags.
// Takes spec (BenchSpec) which supplies the iteration count and arguments.
//
// Returns interpretedRun which holds the captured output and timings.
// Returns error when capturing, compiling or running fails.
func runYaegiSource(ctx context.Context, source, benchmarkDir string, mode RunMode, spec BenchSpec) (interpretedRun, error) {
	capture, err := startProcessCapture(buildYaegiArgv(mode, spec), benchmarkDir)
	if err != nil {
		return interpretedRun{stdout: nil, stderr: nil, compileNanos: 0, wallNanos: 0}, err
	}
	run, runErr := compileAndRunYaegi(ctx, source, capture)
	run.stdout, run.stderr = capture.finish()
	return run, runErr
}

// compileAndRunYaegi compiles and runs source with yaegi, writing to the capture's pipes.
//
// Takes source (string) which is the merged program.
// Takes capture (*processCapture) which supplies the output pipes.
//
// Returns interpretedRun which holds the timings; the caller fills in the output.
// Returns error when the standard library, compilation or the run fails.
func compileAndRunYaegi(ctx context.Context, source string, capture *processCapture) (interpretedRun, error) {
	var run interpretedRun
	interpreter := interp.New(interp.Options{
		Stdin:  os.Stdin,
		Stdout: capture.stdoutWriter,
		Stderr: capture.stderrWriter,
	})
	if err := interpreter.Use(stdlib.Symbols); err != nil {
		return run, fmt.Errorf("yaegi stdlib: %w", err)
	}
	compileStart := time.Now()
	program, err := interpreter.Compile(source)
	run.compileNanos = time.Since(compileStart).Nanoseconds()
	if err != nil {
		return run, fmt.Errorf("yaegi compile: %w", err)
	}
	runStart := time.Now()
	err = runRecovering("yaegi", func() error {
		_, executeErr := interpreter.ExecuteWithContext(ctx, program)
		return executeErr
	})
	run.wallNanos = time.Since(runStart).Nanoseconds()
	if err != nil {
		return run, fmt.Errorf("yaegi run: %w", err)
	}
	return run, nil
}

// buildYaegiArgv shapes argv so os.Args inside the interpreted Go code matches what
// `yaegi run` would have produced via subprocess.
func buildYaegiArgv(mode RunMode, spec BenchSpec) []string {
	args := []string{"yaegi-bench"}
	if mode == ModeInnerLoop {
		args = append(args, "--mode=inner", "--k="+strconv.Itoa(spec.KInner))
	} else {
		args = append(args, "--mode=endtoend")
	}
	args = append(args, spec.Args...)
	return args
}
