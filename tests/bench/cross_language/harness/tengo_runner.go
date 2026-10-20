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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"
)

// TengoRunner executes a benchmark via the tengo Go library (github.com/d5/tengo/v2).
// This is an in-process runner: compile is timed separately from execute via tengo's
// Script.Compile/Compiled.Run API split. Benchmarks without a `tengo/script.tengo` file
// return StatusSkipped; benchmarks tengo cannot express (no goroutines, no uint64) opt
// out via spec.SkipRunners.
type TengoRunner struct{}

// NewTengoRunner returns a Runner that drives the tengo library in-process.
func NewTengoRunner() *TengoRunner { return &TengoRunner{} }

// Kind reports the runner identity used in results.
func (*TengoRunner) Kind() RunnerKind { return RunnerTengo }

// Available is always true; the tengo library is statically linked.
func (*TengoRunner) Available(context.Context) (bool, string) {
	return true, ""
}

// Close is a no-op.
func (*TengoRunner) Close(context.Context) error { return nil }

// Run reads the benchmark's tengo/script.tengo, compiles it via tengo.NewScript/Compile
// (timed as compile_nanos), then runs the compiled program (timed as wall_nanos). The
// tengo script's own timing of its inner loop is parsed off stderr.
func (*TengoRunner) Run(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	scriptPath := filepath.Join(benchmarkDir, "tengo", "script.tengo")
	if _, statErr := os.Stat(scriptPath); statErr != nil {
		return skippedResult(spec.Name, RunnerTengo, mode, "no tengo/script.tengo for this benchmark"), nil
	}

	source, err := os.ReadFile(filepath.Clean(scriptPath))
	if err != nil {
		return failedResult(spec.Name, RunnerTengo, mode, "read script: "+err.Error()), nil
	}

	timeout := time.Duration(spec.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeoutCause(parent, timeout,
		fmt.Errorf("tengo runner %s/%s timed out", spec.Name, mode))
	defer cancel()

	run, runErr := runTengoScript(ctx, source, scriptPath, mode, spec)
	if runErr != nil {
		return failedResult(spec.Name, RunnerTengo, mode, runErr.Error()), nil
	}
	innerNanos, _, stderrRemainder := ParseTimingMarkers(run.stderr)
	return checkedResult(spec, RunnerTengo, mode, run.stdout, stderrRemainder, runTimings{
		wallNanos:    run.wallNanos,
		innerNanos:   innerNanos,
		compileNanos: run.compileNanos,
		peakRSSKB:    pipitSelfPeakRSSKB(),
	}), nil
}

// runTengoScript captures the process's standard streams, swaps os.Args so the tengo
// script's `os.args()` sees our --mode/--k flags, then compiles and runs the script.
//
// Takes ctx (context.Context) which bounds the run.
// Takes source ([]byte) which is the script.
// Takes scriptPath (string) which becomes the script's argv[1].
// Takes mode (RunMode) which selects the script's mode flags.
// Takes spec (BenchSpec) which supplies the iteration count and arguments.
//
// Returns interpretedRun which holds the captured output and timings.
// Returns error when capturing, compiling or running fails.
func runTengoScript(ctx context.Context, source []byte, scriptPath string, mode RunMode, spec BenchSpec) (interpretedRun, error) {
	capture, err := startProcessCapture(buildTengoArgv(scriptPath, mode, spec), "")
	if err != nil {
		return interpretedRun{stdout: nil, stderr: nil, compileNanos: 0, wallNanos: 0}, err
	}
	run, runErr := compileAndRunTengo(ctx, source)
	run.stdout, run.stderr = capture.finish()
	return run, runErr
}

// compileAndRunTengo compiles and runs a tengo script; its output goes to os.Stdout and
// os.Stderr, which the caller has captured.
//
// Takes ctx (context.Context) which bounds the run.
// Takes source ([]byte) which is the script.
//
// Returns interpretedRun which holds the timings; the caller fills in the output.
// Returns error when registering the builtin, compilation or the run fails.
func compileAndRunTengo(ctx context.Context, source []byte) (interpretedRun, error) {
	var run interpretedRun
	script := tengo.NewScript(source)
	script.SetImports(stdlib.GetModuleMap(stdlib.AllModuleNames()...))
	if err := script.Add("sha256_sum", &tengo.UserFunction{Name: "sha256_sum", Value: tengoSha256Sum}); err != nil {
		return run, fmt.Errorf("tengo register sha256_sum: %w", err)
	}
	compileStart := time.Now()
	compiled, err := script.Compile()
	run.compileNanos = time.Since(compileStart).Nanoseconds()
	if err != nil {
		return run, fmt.Errorf("tengo compile: %w", err)
	}
	runStart := time.Now()
	err = runRecovering("tengo", func() error { return compiled.RunContext(ctx) })
	run.wallNanos = time.Since(runStart).Nanoseconds()
	if err != nil {
		return run, fmt.Errorf("tengo run: %w", err)
	}
	return run, nil
}

// buildTengoArgv shapes the os.Args slice that the tengo script's `os.args()` call
// returns. We mirror what the standalone `tengo` binary would see: argv[0] is the binary
// name, argv[1] is the script path, then the mode/k flags.
func buildTengoArgv(scriptPath string, mode RunMode, spec BenchSpec) []string {
	args := []string{"tengo-bench", scriptPath}
	if mode == ModeInnerLoop {
		args = append(args, "--mode", "inner", "--k", strconv.Itoa(spec.KInner))
	} else {
		args = append(args, "--mode", "endtoend")
	}
	args = append(args, spec.Args...)
	return args
}

// tengoSha256Sum is the host implementation behind the `sha256_sum` builtin exposed to
// tengo scripts. It accepts a single byte-slice argument and returns the 32-byte SHA-256
// digest as a tengo Bytes value. Mirrors crypto/sha256.Sum256 on the Go-family runners
// and hashlib.sha256(b).digest() on the Python runners; we register it as a script global
// because tengo's stdlib does not bundle SHA-256.
func tengoSha256Sum(args ...tengo.Object) (tengo.Object, error) {
	if len(args) != 1 {
		return nil, tengo.ErrWrongNumArguments
	}
	byteSlice, ok := args[0].(*tengo.Bytes)
	if !ok {
		return nil, tengo.ErrInvalidArgumentType{Name: "input", Expected: "bytes", Found: args[0].TypeName()}
	}
	digest := sha256.Sum256(byteSlice.Value)
	out := make([]byte, len(digest))
	copy(out, digest[:])
	return &tengo.Bytes{Value: out}, nil
}
