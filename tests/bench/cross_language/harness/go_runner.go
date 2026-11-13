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
	"os/exec"
	"strconv"
	"time"
)

// GoRunner executes a benchmark by shelling out to `go run` on the `<benchmark>/go/`
// directory. The native Go variant produces the canonical stdout hash for the rest of the
// suite to verify against.
type GoRunner struct {
	goBinary string

	goCache string
}

// NewGoRunner returns a Runner that drives `go run`. The shared GOCACHE path lets the
// warmup runs absorb cold-build cost so the timed runs see only execution time. Pass an
// empty cachePath for a fresh per-run cache (slower but useful for honestly measuring
// first-run cost).
func NewGoRunner(cachePath string) *GoRunner {
	return &GoRunner{goBinary: "go", goCache: cachePath}
}

// Kind reports the runner identity used in results.
func (*GoRunner) Kind() RunnerKind { return RunnerGo }

// Available probes that `go` is on PATH. We do not require any particular version: the
// suite reports whatever the host go-toolchain provides and records it in the host info
// block.
func (runner *GoRunner) Available(context.Context) (bool, string) {
	if _, err := exec.LookPath(runner.goBinary); err != nil {
		return false, "go binary not found on PATH: " + err.Error()
	}
	return true, ""
}

// Close is a no-op; the shared GOCACHE survives suite invocations.
func (*GoRunner) Close(context.Context) error {
	return nil
}

// Run builds the benchmark to a temp binary (timed as compile) then invokes the binary
// (timed as runtime). Splitting compile from runtime lets the report show go's compile
// cost separately from its execution cost; the shared GOCACHE means warmup runs absorb
// the cold-build cost of the standard library, and the per-call `go build` is just our
// package plus link.
func (runner *GoRunner) Run(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	binaryFile, err := os.CreateTemp("", "crosslang-go-bench-")
	if err != nil {
		return failedResult(spec.Name, RunnerGo, mode, "create temp bin: "+err.Error()), nil
	}
	_ = binaryFile.Close()
	binaryPath := binaryFile.Name()
	defer func() { _ = os.Remove(binaryPath) }()

	timeout := time.Duration(spec.TimeoutSeconds) * time.Second
	buildResponse, failure := runner.build(parent, spec, mode, benchmarkDir, binaryPath, timeout)
	if failure != nil {
		return *failure, nil
	}

	runResponse, err := RunSubprocess(parent, SubprocessRequest{
		Name:       binaryPath,
		Args:       buildRunnerArgs(mode, spec),
		Env:        nil,
		WorkingDir: benchmarkDir,
		Timeout:    timeout,
	})
	if err != nil {
		return failedResult(spec.Name, RunnerGo, mode, "framework (run): "+err.Error()), nil
	}
	if runResponse.Err != nil {
		return failedResult(spec.Name, RunnerGo, mode, fmt.Sprintf("go binary exited %d: %s; stderr=%s",
			runResponse.ExitCode, runResponse.Err.Error(), tailString(string(runResponse.Stderr), stderrTailBytes))), nil
	}

	innerNanos, stderrRemainder := ParseInnerElapsedNanos(runResponse.Stderr)
	return checkedResult(spec, RunnerGo, mode, runResponse.Stdout, stderrRemainder, runTimings{
		wallNanos:    buildResponse.WallNanos + runResponse.WallNanos,
		innerNanos:   innerNanos,
		compileNanos: buildResponse.WallNanos,
		peakRSSKB:    runResponse.PeakRSSKB,
	}), nil
}

// build compiles the benchmark's go directory to binaryPath with the shared GOCACHE.
//
// Takes spec (BenchSpec) which names the benchmark.
// Takes mode (RunMode) which is recorded in a failed result.
// Takes benchmarkDir (string) which holds the go directory.
// Takes binaryPath (string) which receives the binary.
// Takes timeout (time.Duration) which bounds the build.
//
// Returns SubprocessResponse which is the build's response.
// Returns *Result which is the failed result when the build did not succeed, nil
// otherwise.
func (runner *GoRunner) build(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir, binaryPath string, timeout time.Duration) (SubprocessResponse, *Result) {
	var environment []string
	if runner.goCache != "" {
		environment = append(environment, "GOCACHE="+runner.goCache)
	}
	response, err := RunSubprocess(parent, SubprocessRequest{
		Name:       runner.goBinary,
		Args:       []string{"build", "-o", binaryPath, "./go"},
		Env:        environment,
		WorkingDir: benchmarkDir,
		Timeout:    timeout,
	})
	if err != nil {
		failure := failedResult(spec.Name, RunnerGo, mode, "framework (build): "+err.Error())
		return response, &failure
	}
	if response.Err != nil {
		failure := failedResult(spec.Name, RunnerGo, mode, fmt.Sprintf("go build exited %d: %s; stderr=%s",
			response.ExitCode, response.Err.Error(), tailString(string(response.Stderr), stderrTailBytes)))
		return response, &failure
	}
	return response, nil
}

// buildRunnerArgs assembles the flag + positional argv passed to subprocess runners.
// Inner-loop mode prepends `--mode=inner --k=<K>`; end-to-end mode passes positional args
// directly. The spec's positional args follow.
func buildRunnerArgs(mode RunMode, spec BenchSpec) []string {
	args := []string{}
	if mode == ModeInnerLoop {
		args = append(args, "--mode=inner", "--k="+strconv.Itoa(spec.KInner))
	} else {
		args = append(args, "--mode=endtoend")
	}
	args = append(args, spec.Args...)
	return args
}
