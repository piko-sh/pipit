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
	"path/filepath"
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
func (runner *ScriggoRunner) Kind() RunnerKind { return RunnerScriggo }

// Available is always true; scriggo is statically linked.
func (runner *ScriggoRunner) Available(ctx context.Context) (bool, string) {
	_ = ctx
	return true, ""
}

// Close is a no-op; scriggo holds no global resources.
func (runner *ScriggoRunner) Close(ctx context.Context) error { _ = ctx; return nil }

// Run compiles the benchmark's pipit_source.go together with a small generated wrapper
// that calls Run() / RunInner() and forwards the result through registered capture
// functions, then invokes scriggo.
func (runner *ScriggoRunner) Run(parent context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error) {
	sourcePath := filepath.Join(benchmarkDir, "go", "pipit_source.go")
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "read pipit_source.go: "+err.Error()), nil
	}

	ctx, cancel := context.WithTimeoutCause(
		parent,
		time.Duration(spec.TimeoutSeconds)*time.Second,
		fmt.Errorf("scriggo runner: %s timed out", spec.Name),
	)
	defer cancel()
	_ = ctx

	var capturedStdout string
	var capturedNanos int64
	capture := func(s string) { capturedStdout = s }
	captureNanos := func(n int64) { capturedNanos = n }

	mergedSource, mergeErr := mergeScriggoSource(string(sourceBytes), scriggoWrapperBody(mode, spec.KInner))
	if mergeErr != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "merge source: "+mergeErr.Error()), nil
	}
	files := scriggo.Files{
		"main.go": []byte(mergedSource),
	}

	packages := native.Packages{
		"__capture__": native.Package{
			Name: "__capture__",
			Declarations: native.Declarations{
				"Stdout": capture,
				"Nanos":  captureNanos,
			},
		},
		"time": native.Package{
			Name: "time",
			Declarations: native.Declarations{
				"Now":  time.Now,
				"Time": reflect.TypeOf((*time.Time)(nil)).Elem(),
			},
		},
		"sync": native.Package{
			Name: "sync",
			Declarations: native.Declarations{
				"WaitGroup": reflect.TypeOf((*sync.WaitGroup)(nil)).Elem(),
				"Mutex":     reflect.TypeOf((*sync.Mutex)(nil)).Elem(),
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

	compileStart := time.Now()
	program, buildErr := scriggo.Build(files, &scriggo.BuildOptions{Packages: packages})
	compileNanos := time.Since(compileStart).Nanoseconds()
	if buildErr != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "scriggo build: "+buildErr.Error()), nil
	}
	wallStart := time.Now()
	var runErr error
	var runPanic any
	func() {
		defer func() { runPanic = recover() }()
		runErr = program.Run(nil)
	}()
	wallElapsed := time.Since(wallStart)
	if runPanic != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, fmt.Sprintf("scriggo VM panic: %v", runPanic)), nil
	}
	if runErr != nil {
		return failedResult(spec.Name, RunnerScriggo, mode, "scriggo run: "+runErr.Error()), nil
	}

	normalised := NormaliseStdout([]byte(capturedStdout))
	stdoutSHA := SHA256Hex(normalised)

	status := StatusOK
	note := ""
	if stdoutSHA != spec.ExpectedStdoutSHA {
		status = StatusMismatch
		note = fmt.Sprintf("stdout SHA %s does not match expected %s", stdoutSHA, spec.ExpectedStdoutSHA)
	}

	return Result{
		Benchmark:    spec.Name,
		Runner:       RunnerScriggo,
		Mode:         mode,
		WallNanos:    wallElapsed.Nanoseconds(),
		InnerNanos:   capturedNanos,
		CompileNanos: compileNanos,
		PeakRSSKB:    pipitSelfPeakRSSKB(),
		StdoutSHA:    stdoutSHA,
		Status:       status,
		Note:         note,
	}, nil
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

// mergeScriggoSource glues pipit_source.go and the wrapper main() into a single Go source
// file with one package declaration and a unified import block including "__capture__".
//
// pipit_source.go shape: starts with optional comments, then `package main`, then
// optional imports, then declarations. It never declares main() (that lives in
// native_main.go). We strip its package + imports, capture the imports, then concatenate.
func mergeScriggoSource(pipitSource string, wrapperBody string) (string, error) {
	imports := map[string]struct{}{}
	imports[`"__capture__"`] = struct{}{}
	body := stripPackageAndImports(pipitSource, imports)
	importList := make([]string, 0, len(imports))
	for imp := range imports {
		importList = append(importList, imp)
	}

	for i := 1; i < len(importList); i++ {
		for j := i; j > 0 && importList[j-1] > importList[j]; j-- {
			importList[j-1], importList[j] = importList[j], importList[j-1]
		}
	}
	merged := "package main\n\nimport (\n"
	for _, imp := range importList {
		merged += "\t" + imp + "\n"
	}
	merged += ")\n" + body + "\n" + wrapperBody
	return merged, nil
}
