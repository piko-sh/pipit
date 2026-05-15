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

//go:build integration

package selfhost_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/engine"
)

const repositoryDepth = "../../.."

const reportPath = "docs/selfhost-report.md"

func harnessArgs() []string {
	return strings.Fields(os.Getenv("PIPIT_SELFHOST_ARGS"))
}

func reportFor(args []string) string {
	for index, arg := range args {
		switch {
		case arg == "-report" && index+1 < len(args):
			return args[index+1]
		case strings.HasPrefix(arg, "-report="):
			return strings.TrimPrefix(arg, "-report=")
		}
	}
	return reportPath
}

func TestSelfhost(t *testing.T) {
	root, err := filepath.Abs(repositoryDepth)
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}

	t.Chdir(root)

	args := harnessArgs()
	var stdout, stderr bytes.Buffer
	status := run(args, &stdout, &stderr)

	if stdout.Len() > 0 {
		t.Logf("selfhost summary:\n%s", stdout.String())
	}
	if stderr.Len() > 0 {
		t.Logf("selfhost diagnostics:\n%s", stderr.String())
	}

	report := reportFor(args)
	info, err := os.Stat(report)
	if err != nil {
		t.Fatalf("the harness wrote no report at %s: %v", report, err)
	}
	if info.Size() == 0 {
		t.Fatalf("the harness wrote an empty report at %s", report)
	}

	if status == 2 {
		t.Fatalf("the harness could not start: %s", stderr.String())
	}
	if status != 0 {
		t.Logf("programs remain that do not compile identically; see %s", report)
	}
}

func TestSelfhostAssemblyDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("the assembly-dispatcher lane is skipped under -short")
	}
	root, err := filepath.Abs(repositoryDepth)
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	t.Chdir(root)

	args := []string{"-go-dispatch=false", "-limit", "40", "-report", filepath.Join(t.TempDir(), "report.md")}
	var stdout, stderr bytes.Buffer
	status, results := runWithResults(args, &stdout, &stderr)
	if status == 2 || results == nil {
		t.Fatalf("the harness could not start on the assembly dispatcher: %s", stderr.String())
	}
	if stdout.Len() > 0 {
		t.Logf("assembly-dispatcher summary:\n%s", stdout.String())
	}
	if status != 0 {
		t.Logf("programs remain that do not compile identically on the assembly dispatcher")
	}
}

func TestSelfhostIsDeterministic(t *testing.T) {
	runs, err := strconv.Atoi(os.Getenv("PIPIT_SELFHOST_DETERMINISM"))
	if err != nil || runs < 2 {
		t.Skip("set PIPIT_SELFHOST_DETERMINISM=N (N >= 2) to compare N runs of the lane")
	}
	root, err := filepath.Abs(repositoryDepth)
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	t.Chdir(root)

	var reference map[string]string
	for iteration := range runs {
		args := append(harnessArgs(), "-report", filepath.Join(t.TempDir(), "report.md"))
		var stdout, stderr bytes.Buffer
		started := time.Now()
		status, results := runWithResults(args, &stdout, &stderr)
		if status == 2 || results == nil {
			t.Fatalf("run %d could not start: %s", iteration, stderr.String())
		}
		t.Logf("run %d: status %d, %d programs in %s, heap after GC %d MiB", iteration, status, len(results), time.Since(started).Round(time.Millisecond), heapInUseMiB())
		writeHeapProfile(t, iteration)
		outcomes := outcomesByName(results)
		if reference == nil {
			reference = outcomes
			continue
		}
		for name, outcome := range outcomes {
			if previous, ok := reference[name]; ok && previous != outcome {
				t.Errorf("%s: run 0 %s, run %d %s", name, previous, iteration, outcome)
			}
		}
	}
}

func outcomesByName(results []caseResult) map[string]string {
	outcomes := make(map[string]string, len(results))
	for _, result := range results {
		detail, _, _ := strings.Cut(result.detail, "\n")
		outcomes[result.name] = string(result.outcome) + ": " + detail
	}
	return outcomes
}

func heapInUseMiB() uint64 {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapInuse >> 20
}

func writeHeapProfile(t *testing.T, iteration int) {
	prefix := os.Getenv("PIPIT_SELFHOST_HEAPPROFILE")
	if prefix == "" {
		return
	}
	file, err := os.Create(prefix + "-" + strconv.Itoa(iteration) + ".pprof")
	if err != nil {
		t.Fatalf("creating the heap profile: %v", err)
	}
	defer func() { _ = file.Close() }()
	if err := pprof.WriteHeapProfile(file); err != nil {
		t.Fatalf("writing the heap profile: %v", err)
	}
}

func TestSelfhostCompilesItself(t *testing.T) {
	if testing.Short() {
		t.Skip("the self-compile takes about a minute; skipped under -short")
	}
	root, err := filepath.Abs(repositoryDepth)
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	t.Chdir(root)
	ctx := context.Background()

	h, err := newHost(ctx, hostOptions{compilerDir: "internal/compile", goDispatch: true})
	if err != nil {
		t.Fatalf("loading the compiler under the interpreter: %v", err)
	}
	sources, err := packageSources("internal/compile")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	names := slices.Sorted(maps.Keys(sources))
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		file, err := parser.ParseFile(fset, name, sources[name], parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files = append(files, file)
	}
	info := newTypesInfo()
	config := &types.Config{Sizes: types.SizesFor("gc", "amd64"), GoVersion: engine.InterpretedGoVersion, Importer: h.symbols}
	if _, err := config.Check(compileImportPath, fset, files, info); err != nil {
		t.Fatalf("type-checking the compiler: %v", err)
	}

	nativeStart := time.Now()
	native, err := h.compileNatively(ctx, fset, info, files, compileImportPath)
	if err != nil {
		t.Fatalf("native compile of the compiler: %v", err)
	}
	nativeTime := time.Since(nativeStart)
	interpretedStart := time.Now()
	interpreted, disassembly, err := h.compileInterpreted(ctx, fset, info, files, compileImportPath)
	if err != nil {
		t.Fatalf("interpreted compile of the compiler: %v\n%s", err, pipit.UncaughtPanicStack(err))
	}
	interpretedTime := time.Since(interpretedStart)
	t.Logf("compiler source: %d files; native %s, interpreted %s, both including the pack and listing of the %d-byte program",
		len(files), nativeTime.Round(time.Millisecond), interpretedTime.Round(time.Millisecond), len(native.bytes))
	if !bytes.Equal(native.bytes, interpreted) {
		t.Fatalf("the interpreted compiler compiles itself differently from the native compiler:\n%s",
			firstDifference(native.disassembly, disassembly))
	}
}
