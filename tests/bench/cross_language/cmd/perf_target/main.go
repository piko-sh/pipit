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

// Command perf_target compiles a benchmark pipit_source.go once and executes RunInner(k)
// N times, printing ns/op and heap alloc deltas. Intended as a stable target binary for
// perf record / pprof against the interpreter inner loop: compile and warmup happen
// before the measured window, so profile samples and MemStats deltas cover steady-state
// execution only.
//
// Usage:
//
//	go run ./tests/bench/cmd/perf_target \
//	    [-cpuprofile f.pb.gz] [-memprofile f.pb.gz] <pipit_source.go> <k_inner> [iterations]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"time"

	"pipit.sh/pipit"
)

const (
	// perfTargetMaxCallDepth is the call-stack ceiling passed to the interpreter. Generous
	// enough that deeply recursive benchmarks do not hit a ceiling that only exists inside
	// perf_target.
	perfTargetMaxCallDepth = 200_000

	// perfTargetMemProfileRate records one allocation profile sample per this many bytes,
	// fine-grained enough to attribute the interpreter's small allocations.
	perfTargetMemProfileRate = 4096

	// perfTargetDefaultIterations is the measured-loop repeat count used when the caller
	// supplies no explicit iteration argument.
	perfTargetDefaultIterations = 5
)

func main() {
	cpuProfile := flag.String("cpuprofile", "", "write CPU profile of the measured loop to file")
	memProfile := flag.String("memprofile", "", "write allocation profile of the measured loop to file")
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: perf_target [-cpuprofile f] [-memprofile f] <pipit_source.go> <k_inner> [iterations]")
		os.Exit(2)
	}
	if *memProfile != "" {
		runtime.MemProfileRate = perfTargetMemProfileRate
	}
	kInner, _ := strconv.Atoi(flag.Arg(1))
	iterations := perfTargetDefaultIterations
	if flag.NArg() > 2 {
		iterations, _ = strconv.Atoi(flag.Arg(2))
	}
	ctx := context.Background()
	service, compiled := compileAndWarmup(ctx, flag.Arg(0), kInner)
	elapsed, allocs, bytes := measureLoop(ctx, service, compiled, iterations, *cpuProfile, *memProfile)
	printReport(iterations, kInner, elapsed, allocs, bytes)
}

// compileAndWarmup compiles the benchmark source together with a generated entry-point
// wrapper and executes it once, so the measured loop starts against a warm interpreter.
//
// Takes sourcePath (string) which is the pipit_source.go to compile.
// Takes kInner (int) which is the inner loop count baked into the wrapper.
//
// Returns the configured interpreter service and the compiled file set.
func compileAndWarmup(ctx context.Context, sourcePath string, kInner int) (*pipit.Interpreter, *pipit.CompiledFileSet) {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		panic(err)
	}
	wrapper := fmt.Sprintf("package main\n\nfunc EntrypointRun() (string, int64) {\n\treturn RunInner(%d)\n}\n", kInner)
	service := pipit.NewInterpreter(pipit.WithMaxCallDepth(perfTargetMaxCallDepth))
	sources := map[string]string{"main.go": string(source), "entrypoint.go": wrapper}
	compiled, err := service.CompileFileSet(ctx, sources)
	if err != nil {
		panic(err)
	}
	if _, err := service.ExecuteEntrypoint(ctx, compiled, "EntrypointRun"); err != nil {
		panic(err)
	}
	return service, compiled
}

// measureLoop runs the entry-point the requested number of times with profiling active
// and samples the heap on either side of the loop.
//
// Takes service (*pipit.Interpreter) which is the warmed interpreter service.
// Takes compiled (*pipit.CompiledFileSet) which is the already-compiled program.
// Takes iterations (int) which is the number of measured executions.
// Takes cpuProfilePath (string) which names the CPU profile file, or is empty.
// Takes memProfilePath (string) which names the allocation profile file, or is empty.
//
// Returns elapsed (Duration) which is the wall time of the measured loop.
// Returns allocs (uint64) which is the allocation count over the loop.
// Returns bytes (uint64) which is the allocated byte total over the loop.
func measureLoop(
	ctx context.Context,
	service *pipit.Interpreter,
	compiled *pipit.CompiledFileSet,
	iterations int,
	cpuProfilePath string,
	memProfilePath string,
) (elapsed time.Duration, allocs, bytes uint64) {

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	stopCPUProfile := startCPUProfile(cpuProfilePath)
	defer stopCPUProfile()
	start := time.Now()
	for range iterations {
		if _, err := service.ExecuteEntrypoint(ctx, compiled, "EntrypointRun"); err != nil {
			panic(err)
		}
	}
	elapsed = time.Since(start)
	writeMemProfile(memProfilePath)
	runtime.ReadMemStats(&after)
	return elapsed, after.Mallocs - before.Mallocs, after.TotalAlloc - before.TotalAlloc
}

// startCPUProfile begins CPU profiling into path. An empty path disables profiling and
// yields a stop function that does nothing.
//
// Takes path (string) which names the CPU profile file.
//
// Returns the stop function, which ends profiling and closes the profile file.
func startCPUProfile(path string) func() {
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		panic(err)
	}
	return func() {
		pprof.StopCPUProfile()
		if err := file.Close(); err != nil {
			panic(err)
		}
	}
}

// writeMemProfile writes the allocation profile to path, doing nothing when path is
// empty.
//
// Takes path (string) which names the allocation profile file.
func writeMemProfile(path string) {
	if path == "" {
		return
	}

	runtime.GC()
	file, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			panic(err)
		}
	}()
	if err := pprof.Lookup("allocs").WriteTo(file, 0); err != nil {
		panic(err)
	}
}

// printReport writes the one-line per-run summary to stdout.
//
// Takes iterations (int) which is the measured loop count.
// Takes kInner (int) which is the inner loop count each iteration ran.
// Takes elapsed (time.Duration) which is the wall time of the measured loop.
// Takes allocs (uint64) which is the allocation count over the loop.
// Takes bytes (uint64) which is the allocated byte total over the loop.
func printReport(iterations, kInner int, elapsed time.Duration, allocs, bytes uint64) {
	totalRuns := int64(iterations) * int64(kInner)
	fmt.Printf("iterations=%d k_inner=%d wall=%v ns/op=%d allocs/op=%d bytes/op=%d\n",
		iterations, kInner, elapsed,
		elapsed.Nanoseconds()/totalRuns,
		int64(allocs)/totalRuns,
		int64(bytes)/totalRuns)
}
