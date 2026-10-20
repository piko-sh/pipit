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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mvm-sh/mvm/interp"
	"github.com/mvm-sh/mvm/lang/golang"
	"github.com/mvm-sh/mvm/stdlib"

	// Registers every standard library package with mvm's stdlib.Values.
	_ "github.com/mvm-sh/mvm/stdlib/all"
)

// MvmRunner executes benchmarks via the mvm in-process Go interpreter
// (github.com/mvm-sh/mvm).
type MvmRunner struct{}

// NewMvmRunner returns a Runner that drives the mvm library.
func NewMvmRunner() *MvmRunner { return &MvmRunner{} }

// Kind reports the runner identity used in results.
func (*MvmRunner) Kind() RunnerKind { return RunnerMvm }

// Available is always true; mvm is statically linked.
func (*MvmRunner) Available(context.Context) (bool, string) {
	return true, ""
}

// Close is a no-op; mvm holds no global resources.
func (*MvmRunner) Close(context.Context) error { return nil }

// Run compiles the benchmark's pipit_source.go together with a small generated main()
// that calls Run() / RunInner(K) and writes the result to stdout and (for inner-loop
// mode) the elapsed nanos to stderr. Mvm's Eval takes no context, so a run is not bounded
// by spec.TimeoutSeconds.
func (*MvmRunner) Run(_ context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	sourceBytes, err := readBenchmarkFile(benchmarkDir, "go", "pipit_source.go")
	if err != nil {
		return failedResult(spec.Name, RunnerMvm, mode, "read pipit_source.go: "+err.Error()), nil
	}
	imports := map[string]struct{}{`"fmt"`: {}, `"os"`: {}}
	body := stripPackageAndImports(string(sourceBytes), imports)
	mergedSource := singleFileSource(imports, body, mvmWrapperBody(mode, spec.KInner))

	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	interpreter := interp.NewInterpreter(golang.GoSpec)
	interpreter.ImportPackageValues(stdlib.Values)
	interpreter.SetIO(os.Stdin, &stdoutBuffer, &stderrBuffer)

	wallStart := time.Now()
	runErr := runRecovering("mvm", func() error {
		_, evalErr := interpreter.Eval(filepath.Join(benchmarkDir, "go", "merged.go"), mergedSource)
		return evalErr
	})
	wallElapsed := time.Since(wallStart)
	if runErr != nil {
		return failedResult(spec.Name, RunnerMvm, mode, "mvm eval: "+runErr.Error()), nil
	}

	innerNanos := int64(0)
	if mode == ModeInnerLoop {
		innerNanos = parseMvmInnerElapsedNanos(stderrBuffer.String())
	}
	return checkedResult(spec, RunnerMvm, mode, stdoutBuffer.Bytes(), "", runTimings{
		wallNanos:    wallElapsed.Nanoseconds(),
		innerNanos:   innerNanos,
		compileNanos: interpreter.Stats.CompileTime.Nanoseconds(),
		peakRSSKB:    pipitSelfPeakRSSKB(),
	}), nil
}

// mvmWrapperBody returns the body (no package / import lines) of a `main()` that calls
// `Run()` or `RunInner(K)` and writes the result to stdout (and elapsed nanos to stderr)
// the same way native_main.go does. Mvm's bundled stdlib provides fmt and os, so the
// wrapper looks like a real Go program.
func mvmWrapperBody(mode RunMode, kInner int) string {
	if mode == ModeInnerLoop {
		return fmt.Sprintf(`
func main() {
	result, elapsed := RunInner(%d)
	fmt.Println(result)
	fmt.Fprintf(os.Stderr, "INNER_ELAPSED_NS=%%d\n", elapsed)
}
`, kInner)
	}
	return `
func main() {
	fmt.Println(Run())
}
`
}

// parseMvmInnerElapsedNanos extracts the `INNER_ELAPSED_NS=<int>` value the wrapper
// main() writes to stderr in inner-loop mode. Returns zero if the line is absent or
// malformed, matching the subprocess runner behaviour.
func parseMvmInnerElapsedNanos(stderrContent string) int64 {
	for line := range strings.SplitSeq(stderrContent, "\n") {
		trimmed := strings.TrimSpace(line)
		const prefix = "INNER_ELAPSED_NS="
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value, err := strconv.ParseInt(trimmed[len(prefix):], 10, 64)
		if err == nil {
			return value
		}
	}
	return 0
}
