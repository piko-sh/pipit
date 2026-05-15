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
	"path/filepath"
	"runtime"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

// benchmarkCallDepthLimit is the call-depth ceiling the in-process pipit runner uses,
// well above the interpreter's default. Benchmarks run recursive-descent parsers and tree
// walks on much larger inputs than a typical embedded user would, and the default is
// intentionally constrained for safety; here we lift it so a benchmark's wall time
// reflects the cost of the algorithm, not pipit's safety bound.
const benchmarkCallDepthLimit = 200000

// PipitRunner executes a benchmark in-process via the pipit interpreter. It loads the
// canonical Go source from `<benchmark>/go/pipit_source.go` and calls either `Run`
// (end-to-end) or `RunInner` (inner-loop) by name.
type PipitRunner struct{}

// NewPipitRunner returns a Runner that drives pipit in-process. No setup is required
// ahead of Available/Run.
func NewPipitRunner() *PipitRunner { return &PipitRunner{} }

// Kind reports the runner identity used in results.
func (runner *PipitRunner) Kind() RunnerKind { return RunnerPipit }

// Available always returns true; pipit is linked into the test binary.
func (runner *PipitRunner) Available(ctx context.Context) (bool, string) {
	_ = ctx
	return true, ""
}

// Close is a no-op for the in-process runner.
func (runner *PipitRunner) Close(ctx context.Context) error {
	_ = ctx
	return nil
}

// Run compiles `<dir>/go/pipit_source.go` together with a generated wrapper that selects
// the entrypoint and (for ModeInnerLoop) hard-codes the K iteration count, then invokes
// the wrapper. ExecuteEntrypoint is parameterless so the wrapper is how we thread spec
// data into the in-process invocation. The first return value is the per-run Result. The
// second return value is non-nil only on framework-level failures that should abort the
// suite (e.g. source file missing).
func (runner *PipitRunner) Run(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	sourcePath := filepath.Join(benchmarkDir, "go", "pipit_source.go")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return failedResult(spec.Name, RunnerPipit, mode, "read pipit_source.go: "+err.Error()), nil
	}
	wrapperEntrypoint := "EntrypointRun"
	wrapperSource := generateWrapper(mode, spec)

	ctx, cancel := context.WithTimeoutCause(
		parent,
		time.Duration(spec.TimeoutSeconds)*time.Second,
		fmt.Errorf("pipit runner: %s/%s timed out", spec.Name, wrapperEntrypoint),
	)
	defer cancel()

	service := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxCallDepth(benchmarkCallDepthLimit))

	sourceMap := map[string]string{
		"main.go":       string(sourceBytes),
		"entrypoint.go": wrapperSource,
	}
	compileStart := time.Now()
	compiledFileSet, compileError := service.CompileFileSet(ctx, sourceMap)
	compileNanos := time.Since(compileStart).Nanoseconds()
	if compileError != nil {
		return failedResult(spec.Name, RunnerPipit, mode, "compile: "+compileError.Error()), nil
	}

	wallStart := time.Now()
	resultValue, executeError := service.ExecuteEntrypoint(ctx, compiledFileSet, wrapperEntrypoint)
	wallElapsed := time.Since(wallStart)

	if executeError != nil {
		return failedResult(spec.Name, RunnerPipit, mode, "execute: "+executeError.Error()), nil
	}

	canonicalOutput, innerNanos := decodePipitResult(resultValue, mode)
	normalised := NormaliseStdout([]byte(canonicalOutput))
	stdoutSHA := SHA256Hex(normalised)

	status := StatusOK
	note := ""
	if stdoutSHA != spec.ExpectedStdoutSHA {
		status = StatusMismatch
		note = fmt.Sprintf("stdout SHA %s does not match expected %s", stdoutSHA, spec.ExpectedStdoutSHA)
	}

	return Result{
		Benchmark:    spec.Name,
		Runner:       RunnerPipit,
		Mode:         mode,
		WallNanos:    wallElapsed.Nanoseconds(),
		InnerNanos:   innerNanos,
		CompileNanos: compileNanos,
		PeakRSSKB:    pipitSelfPeakRSSKB(),
		StdoutSHA:    stdoutSHA,
		Status:       status,
		Note:         note,
	}, nil
}

// generateWrapper synthesises a tiny `entrypoint.go` file that pipit compiles alongside
// the benchmark source. The wrapper defines a single parameterless function
// `EntrypointRun` that calls either `Run()` (for ModeEndToEnd) or `RunInner(K)` with K
// hard-coded (for ModeInnerLoop). This keeps the pipit entry-point shape parameterless
// while still letting the harness vary spec.KInner per run.
func generateWrapper(mode RunMode, spec BenchSpec) string {
	if mode == ModeInnerLoop {
		return fmt.Sprintf(`package main

func EntrypointRun() (string, int64) {
	return RunInner(%d)
}
`, spec.KInner)
	}
	return `package main

func EntrypointRun() string {
	return Run()
}
`
}

// decodePipitResult normalises whatever the entrypoint returned into the
// (canonical-string, inner-nanos) tuple the harness expects.
//
// For ModeEndToEnd: pipit's Run() returns a string; the inner-nanos field is always zero.
//
// For ModeInnerLoop: pipit's RunInner(k) returns two values which the interpreter exposes
// to native callers as a `[]any` (or similar). We inspect the shape defensively rather
// than relying on an exact API contract so a future tweak to the runtime's multi-return
// surface does not silently break the harness.
func decodePipitResult(raw any, mode RunMode) (string, int64) {
	if mode == ModeEndToEnd {
		if asString, ok := raw.(string); ok {
			return asString, 0
		}
		return fmt.Sprint(raw), 0
	}
	switch typed := raw.(type) {
	case []any:
		canonical := ""
		var nanos int64
		if len(typed) > 0 {
			canonical = anyToString(typed[0])
		}
		if len(typed) > 1 {
			nanos = anyToInt64(typed[1])
		}
		return canonical, nanos
	default:
		return fmt.Sprint(raw), 0
	}
}

func anyToString(value any) string {
	if asString, ok := value.(string); ok {
		return asString
	}
	return fmt.Sprint(value)
}

func anyToInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		return int64(typed)
	default:
		return 0
	}
}

// pipitSelfPeakRSSKB samples the current process's peak resident-set-size in KiB. Because
// pipit is in-process, the captured RSS is the test binary's RSS, which is dominated by
// Go runtime and unrelated test infrastructure; it is not directly comparable to
// per-process RSS for the subprocess runners. The Markdown report footnotes this.
func pipitSelfPeakRSSKB() int64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return int64(memStats.Sys / 1024)
}

func failedResult(benchmark string, runner RunnerKind, mode RunMode, note string) Result {
	return Result{
		Benchmark: benchmark,
		Runner:    runner,
		Mode:      mode,
		Status:    StatusFailed,
		Note:      note,
	}
}
