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
	"fmt"
	"os"
	"path/filepath"
)

// runTimings are the measurements a runner took for one completed run.
type runTimings struct {
	// wallNanos is the run's wall-clock time.
	wallNanos int64

	// innerNanos is the inner loop's time, as the program reported it.
	innerNanos int64

	// compileNanos is the compilation time, where the runner measures it.
	compileNanos int64

	// peakRSSKB is the peak resident set size in KiB.
	peakRSSKB int64
}

// checkedResult builds the result of a run that completed. It hashes the output and marks
// a digest that differs from the spec's as a mismatch.
//
// Takes spec (BenchSpec) which holds the name and the expected digest.
// Takes runner (RunnerKind) which ran the benchmark.
// Takes mode (RunMode) which is what the timings cover.
// Takes stdout ([]byte) which is the run's output.
// Takes stderr (string) which is the run's standard error with the timing markers
// removed.
// Takes timings (runTimings) which are the run's measurements.
//
// Returns Result which is the run's record.
func checkedResult(spec BenchSpec, runner RunnerKind, mode RunMode, stdout []byte, stderr string, timings runTimings) Result {
	stdoutSHA := SHA256Hex(NormaliseStdout(stdout))
	status := StatusOK
	note := ""
	if stdoutSHA != spec.ExpectedStdoutSHA {
		status = StatusMismatch
		note = fmt.Sprintf("stdout SHA %s does not match expected %s", stdoutSHA, spec.ExpectedStdoutSHA)
	}
	return Result{
		Benchmark:    spec.Name,
		Runner:       runner,
		Mode:         mode,
		StdoutSHA:    stdoutSHA,
		Status:       status,
		Note:         note,
		StderrTail:   tailString(stderr, stderrTailBytes),
		RunIndex:     0,
		WallNanos:    timings.wallNanos,
		InnerNanos:   timings.innerNanos,
		CompileNanos: timings.compileNanos,
		PeakRSSKB:    timings.peakRSSKB,
	}
}

// failedResult builds the result of a run that did not complete.
//
// Takes benchmark (string) which is the benchmark's name.
// Takes runner (RunnerKind) which ran it.
// Takes mode (RunMode) which was being measured.
// Takes note (string) which says what went wrong.
//
// Returns Result which has StatusFailed.
func failedResult(benchmark string, runner RunnerKind, mode RunMode, note string) Result {
	return unmeasuredResult(benchmark, runner, mode, StatusFailed, note)
}

// skippedResult builds the result of a runner that did not run the benchmark.
//
// Takes benchmark (string) which is the benchmark's name.
// Takes runner (RunnerKind) which was skipped.
// Takes mode (RunMode) which would have been measured.
// Takes note (string) which says why.
//
// Returns Result which has StatusSkipped.
func skippedResult(benchmark string, runner RunnerKind, mode RunMode, note string) Result {
	return unmeasuredResult(benchmark, runner, mode, StatusSkipped, note)
}

// unmeasuredResult builds a result that carries a status and a note but no measurements.
//
// Takes benchmark (string) which is the benchmark's name.
// Takes runner (RunnerKind) which is the runner.
// Takes mode (RunMode) which is the mode.
// Takes status (string) which is one of the Status constants.
// Takes note (string) which explains the status.
//
// Returns Result which has zero timings.
func unmeasuredResult(benchmark string, runner RunnerKind, mode RunMode, status, note string) Result {
	return Result{
		Benchmark:    benchmark,
		Runner:       runner,
		Mode:         mode,
		StdoutSHA:    "",
		Status:       status,
		Note:         note,
		StderrTail:   "",
		RunIndex:     0,
		WallNanos:    0,
		InnerNanos:   0,
		CompileNanos: 0,
		PeakRSSKB:    0,
	}
}

// tailString returns the last limit bytes of text.
//
// Takes text (string) which is the text to cut.
// Takes limit (int) which is the most bytes to keep.
//
// Returns string which is text, or its tail when longer than limit.
func tailString(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}

// readBenchmarkFile reads a file inside a benchmark directory.
//
// Takes benchmarkDir (string) which is the benchmark's directory.
// Takes elements (...string) which are the path elements below it.
//
// Returns []byte which is the file's contents.
// Returns error when the file cannot be read.
func readBenchmarkFile(benchmarkDir string, elements ...string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(filepath.Join(append([]string{benchmarkDir}, elements...)...)))
}
