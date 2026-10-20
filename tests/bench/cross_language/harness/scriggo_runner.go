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
	"math"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/open2b/scriggo"
	"github.com/open2b/scriggo/native"
)

// ScriggoRunner executes benchmarks via the scriggo in-process Go interpreter
// (github.com/open2b/scriggo). Benchmarks using unsupported features (generics, non-empty
// interfaces, etc.) will fail at scriggo.Build.
type ScriggoRunner struct{}

// NewScriggoRunner returns a Runner that drives the scriggo library.
func NewScriggoRunner() *ScriggoRunner { return &ScriggoRunner{} }

// Kind reports the runner identity used in results.
func (*ScriggoRunner) Kind() RunnerKind { return RunnerScriggo }

// Available is always true; scriggo is statically linked.
func (*ScriggoRunner) Available(context.Context) (bool, string) {
	return true, ""
}

// Close is a no-op; scriggo holds no global resources.
func (*ScriggoRunner) Close(context.Context) error { return nil }

// Run compiles the benchmark's pipit_source.go together with a small generated wrapper
// that calls Run() / RunInner() and forwards the result through registered capture
// functions, then invokes scriggo. Scriggo's Run takes no context, so a run is not
// bounded by spec.TimeoutSeconds.
func (*ScriggoRunner) Run(_ context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	sourceBytes, err := readBenchmarkFile(benchmarkDir, "go", "pipit_source.go")
	if err != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "read pipit_source.go: "+err.Error()), nil
	}

	var capturedStdout string
	var capturedNanos int64
	imports := map[string]struct{}{`"__capture__"`: {}}
	body := stripPackageAndImports(string(sourceBytes), imports)
	files := scriggo.Files{
		"main.go": []byte(singleFileSource(imports, body, scriggoWrapperBody(mode, spec.KInner))),
	}
	packages := scriggoPackages(func(s string) { capturedStdout = s }, func(n int64) { capturedNanos = n })

	compileStart := time.Now()
	program, buildErr := scriggo.Build(files, &scriggo.BuildOptions{Packages: packages})
	compileNanos := time.Since(compileStart).Nanoseconds()
	if buildErr != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "scriggo build: "+buildErr.Error()), nil
	}
	wallStart := time.Now()
	runErr := runRecovering("scriggo", func() error { return program.Run(nil) })
	wallElapsed := time.Since(wallStart)
	if runErr != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "scriggo run: "+runErr.Error()), nil
	}
	return checkedResult(spec, RunnerScriggo, mode, []byte(capturedStdout), "", runTimings{
		wallNanos:    wallElapsed.Nanoseconds(),
		innerNanos:   capturedNanos,
		compileNanos: compileNanos,
		peakRSSKB:    pipitSelfPeakRSSKB(),
	}), nil
}

// scriggoPackages lists the native packages a benchmark may import under scriggo, and the
// __capture__ package the wrapper reports its result through.
//
// Takes captureStdout (func(string)) which receives the program's output.
// Takes captureNanos (func(int64)) which receives the inner loop's time.
//
// Returns native.Packages which is the table for scriggo.Build.
func scriggoPackages(captureStdout func(string), captureNanos func(int64)) native.Packages {
	return native.Packages{
		"__capture__": native.Package{
			Name: "__capture__",
			Declarations: native.Declarations{
				"Stdout": captureStdout,
				"Nanos":  captureNanos,
			},
		},
		"time": native.Package{
			Name: "time",
			Declarations: native.Declarations{
				"Now":  time.Now,
				"Time": reflect.TypeFor[time.Time](),
			},
		},
		"sync": native.Package{
			Name: "sync",
			Declarations: native.Declarations{
				"WaitGroup": reflect.TypeFor[sync.WaitGroup](),
				"Mutex":     reflect.TypeFor[sync.Mutex](),
			},
		},
		"os": native.Package{
			Name: "os",
			Declarations: native.Declarations{
				"ReadFile": os.ReadFile,
			},
		},
		"math": native.Package{
			Name: "math",
			Declarations: native.Declarations{
				"Sqrt":  math.Sqrt,
				"Floor": math.Floor,
				"Ceil":  math.Ceil,
				"Abs":   math.Abs,
				"Pow":   math.Pow,
				"Pi":    math.Pi,
			},
		},
		"crypto/sha256": native.Package{
			Name: "sha256",
			Declarations: native.Declarations{
				"Sum256": sha256.Sum256,
			},
		},
	}
}

// scriggoWrapperBody returns the body (no package/import lines) of a main() that calls
// Run() or RunInner(K) and forwards the result through the __capture__ native package
// back to the host.
func scriggoWrapperBody(mode RunMode, kInner int) string {
	if mode == ModeInnerLoop {
		return fmt.Sprintf(`
func main() {
	result, elapsed := RunInner(%d)
	__capture__.Stdout(result)
	__capture__.Nanos(elapsed)
}
`, kInner)
	}
	return `
func main() {
	__capture__.Stdout(Run())
}
`
}
