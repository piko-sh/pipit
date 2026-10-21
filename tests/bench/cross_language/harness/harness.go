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
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runner is the per-language driver interface. Every implementation knows how to probe
// its environment, execute one benchmark in a given mode, and release any held resources.
type Runner interface {
	Kind() RunnerKind

	Available(ctx context.Context) (bool, string)

	Run(ctx context.Context, spec BenchSpec, mode RunMode, benchmarkDir string) (Result, error)

	Close(ctx context.Context) error
}

// Suite is the top-level orchestration object. Load() builds one; Run(t) drives it
// through every (benchmark, runner, mode) combination configured by the environment,
// writes the JSON+Markdown reports, and fails the test if a required runner is
// unavailable or if the regen-hashes flow detected drift.
type Suite struct {
	// BenchmarksDirectory holds one directory per benchmark.
	BenchmarksDirectory string

	// OutputDirectory receives latest.json and latest.md.
	OutputDirectory string

	// Specs are the benchmarks to run, sorted by name.
	Specs []BenchSpec

	// Runners are the enabled runners.
	Runners []Runner

	// hostInfo describes the machine for the reports.
	hostInfo HostInfo

	// Config is the suite's configuration.
	Config SuiteConfig
}

// outputDirectoryMode is the mode of a created results directory.
const outputDirectoryMode = 0o750

// suiteOutcome collects what a suite run measured.
type suiteOutcome struct {
	// results holds every measured run and every skipped runner.
	results []Result

	// aggregates summarises the cells that had successful runs.
	aggregates []Aggregate
}

// Run executes the full suite and writes reports. Each (benchmark, runner, mode) cell
// yields Config.Runs Result records plus Config.Warmup discarded records. Subtests are
// created per benchmark and per runner so partial failures show up clearly in `go test
// -v` output.
func (suite *Suite) Run(testingHandle *testing.T) {
	testingHandle.Helper()
	ctx := testingHandle.Context()

	suite.checkRequiredRunners(ctx, testingHandle)

	if suite.Config.Regenerate {
		require.NoError(testingHandle, suite.regenerateHashes(ctx), "regenerate hashes")
		return
	}

	outcome := &suiteOutcome{results: []Result{}, aggregates: []Aggregate{}}
	for index := range suite.Specs {
		spec := suite.Specs[index]
		testingHandle.Run(spec.Name, func(benchmarkTestHandle *testing.T) {
			suite.runBenchmark(ctx, benchmarkTestHandle, spec, outcome)
		})
	}

	sortAggregates(outcome.aggregates)
	assert.NoError(testingHandle, WriteJSONReport(suite, outcome.results, outcome.aggregates), "write JSON report")
	assert.NoError(testingHandle, WriteMarkdownReport(suite, outcome.aggregates), "write Markdown report")
}

// runBenchmark checks a benchmark's sources, then runs it on every runner that supports
// it, recording an unavailable runner as skipped.
//
// Takes testingHandle (*testing.T) which is the benchmark's subtest.
// Takes spec (BenchSpec) which is the benchmark.
// Takes outcome (*suiteOutcome) which receives the results.
func (suite *Suite) runBenchmark(ctx context.Context, testingHandle *testing.T, spec BenchSpec, outcome *suiteOutcome) {
	benchmarkDirectory := filepath.Join(suite.BenchmarksDirectory, spec.Name)
	require.NoError(testingHandle, VerifySourceSHAs(spec, benchmarkDirectory), "source SHAs of %s match the spec", spec.Name)

	for _, runner := range suite.Runners {
		if isSkippedRunner(spec, runner.Kind()) {
			continue
		}
		if available, message := runner.Available(ctx); !available {
			testingHandle.Logf("runner %s skipped: %s", runner.Kind(), message)
			outcome.results = append(outcome.results, skippedResult(spec.Name, runner.Kind(), ModeEndToEnd, message))
			continue
		}
		testingHandle.Run(string(runner.Kind()), func(runnerTestHandle *testing.T) {
			suite.runModes(ctx, runnerTestHandle, runner, spec, benchmarkDirectory, outcome)
		})
	}
}

// runModes measures one benchmark on one runner in every configured mode.
//
// Takes testingHandle (*testing.T) which is the runner's subtest.
// Takes runner (Runner) which runs the benchmark.
// Takes spec (BenchSpec) which is the benchmark.
// Takes benchmarkDirectory (string) which holds its sources.
// Takes outcome (*suiteOutcome) which receives the results.
func (suite *Suite) runModes(ctx context.Context, testingHandle *testing.T, runner Runner, spec BenchSpec, benchmarkDirectory string, outcome *suiteOutcome) {
	for _, mode := range suite.Config.Modes {
		testingHandle.Run(string(mode), func(modeTestHandle *testing.T) {
			runResults := suite.executeCell(ctx, modeTestHandle, runner, spec, mode, benchmarkDirectory)
			outcome.results = append(outcome.results, runResults...)
			if aggregate, ok := aggregateOK(runResults, spec.KInner); ok {
				outcome.aggregates = append(outcome.aggregates, aggregate)
			}
		})
	}
}

// executeCell runs warmup + measured iterations for a single (benchmark, runner, mode)
// tuple. Warmup results are logged but not appended to the global results slice.
func (suite *Suite) executeCell(
	parent context.Context,
	testingHandle *testing.T,
	runner Runner,
	spec BenchSpec,
	mode RunMode,
	benchmarkDirectory string,
) []Result {
	results := make([]Result, 0, suite.Config.Runs)
	for warmupIndex := 0; warmupIndex < suite.Config.Warmup; warmupIndex++ {
		_, err := runner.Run(parent, spec, mode, benchmarkDirectory)
		if err != nil {
			testingHandle.Logf("warmup %d framework error: %v", warmupIndex, err)
		}
	}
	for runIndex := 0; runIndex < suite.Config.Runs; runIndex++ {
		result, err := runner.Run(parent, spec, mode, benchmarkDirectory)
		require.NoError(testingHandle, err, "run %d has no framework error", runIndex)
		result.RunIndex = runIndex
		switch result.Status {
		case StatusMismatch:
			testingHandle.Logf("mismatch on run %d: %s", runIndex, result.Note)
		case StatusFailed:
			testingHandle.Logf("failed run %d: %s", runIndex, result.Note)
		}
		results = append(results, result)
	}
	return results
}

// regenerateHashes runs the native Go variant of every discovered benchmark and rewrites
// each spec.json with the resulting stdout SHA plus the current source-file SHAs. Used
// when adding a new benchmark or after editing one. Native Go is the canonical truth for
// the expected output.
func (suite *Suite) regenerateHashes(ctx context.Context) error {
	goRunner := NewGoRunner(filepath.Join(suite.BenchmarksDirectory, ".gocache"))
	if available, message := goRunner.Available(ctx); !available {
		return fmt.Errorf("regenerate requires go: %s", message)
	}
	for index := range suite.Specs {
		spec := suite.Specs[index]
		benchmarkDirectory := filepath.Join(suite.BenchmarksDirectory, spec.Name)
		regenSpec := spec
		regenSpec.ExpectedStdoutSHA = ""
		regenSpec.SourceSHAs = nil
		result, runError := goRunner.Run(ctx, regenSpec, ModeEndToEnd, benchmarkDirectory)
		if runError != nil {
			return fmt.Errorf("%s: %w", spec.Name, runError)
		}
		if result.Status == StatusFailed {
			return fmt.Errorf("%s: native go run failed: %s", spec.Name, result.Note)
		}
		updatedSpec := spec
		updatedSpec.ExpectedStdoutSHA = result.StdoutSHA
		updatedSpec.SourceSHAs = collectSourceSHAs(benchmarkDirectory)
		if err := saveSpec(SpecPath(benchmarkDirectory), updatedSpec); err != nil {
			return fmt.Errorf("%s: %w", spec.Name, err)
		}
		fmt.Printf("regenerated %s -> %s\n", spec.Name, result.StdoutSHA)
		suite.Specs[index] = updatedSpec
	}
	return nil
}

// checkRequiredRunners fails the test early if any runner marked required via
// CROSS_LANG_REQUIRE is unavailable.
func (suite *Suite) checkRequiredRunners(ctx context.Context, testingHandle *testing.T) {
	for _, runner := range suite.Runners {
		if !suite.Config.RunnerRequired(runner.Kind()) {
			continue
		}
		available, message := runner.Available(ctx)
		if !available {
			require.Failf(testingHandle, "required runner unavailable", "runner %s is required (CROSS_LANG_REQUIRE) but unavailable: %s", runner.Kind(), message)
		}
	}
}

// resolveOutputDirectory places the results directory. A relative CROSS_LANG_OUTPUT_DIR,
// including the default "results", sits beside the benchmarks directory, and an absolute
// one is used as given.
//
// Takes benchmarksDirectory (string) which is the suite's benchmarks directory.
// Takes configured (string) which is the configured output directory, relative or
// absolute.
//
// Returns string which is the directory the run writes latest.json and latest.md into.
func resolveOutputDirectory(benchmarksDirectory, configured string) string {
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}
	return filepath.Join(filepath.Dir(benchmarksDirectory), configured)
}

// Load discovers benchmarks under benchmarksDirectory, parses every spec.json, and
// constructs the runner registry per SuiteConfig. Filter/runner-set/mode env overrides
// are applied here.
func Load(testingHandle *testing.T, benchmarksDirectory string) *Suite {
	testingHandle.Helper()
	config := LoadSuiteConfig()

	specs, err := discoverSpecs(benchmarksDirectory, config)
	require.NoError(testingHandle, err, "load benchmarks")

	outputDirectory := resolveOutputDirectory(benchmarksDirectory, config.OutputDirectory)
	require.NoError(testingHandle, os.MkdirAll(outputDirectory, outputDirectoryMode), "create output directory %s", outputDirectory)

	runners := buildRunners(config, benchmarksDirectory)
	return &Suite{
		BenchmarksDirectory: benchmarksDirectory,
		OutputDirectory:     outputDirectory,
		Config:              config,
		Specs:               specs,
		Runners:             runners,
		hostInfo:            collectHostInfo(testingHandle.Context(), config),
	}
}

// collectSourceSHAs walks the canonical source-file paths under a benchmark directory and
// returns the per-file SHA map. Missing files are silently omitted so a benchmark that
// legitimately lacks (say) a PyPy variant does not crash regeneration. Any files under
// testdata/ are also hashed and recorded under their relative path so committed corpora,
// fixtures, etc. participate in source-SHA verification.
func collectSourceSHAs(benchmarkDirectory string) map[string]string {
	relativePaths := []string{
		"go/pipit_source.go",
		"go/native_main.go",
		"py/cpython.py",
		"py/pypy.py",
	}
	out := map[string]string{}
	for _, relativePath := range relativePaths {
		hash, err := HashSourceFile(filepath.Join(benchmarkDirectory, relativePath))
		if err != nil {
			continue
		}
		out[relativePath] = hash
	}
	collectTestdataSHAs(benchmarkDirectory, out)
	return out
}

// collectTestdataSHAs walks any testdata/ subdirectory under a benchmark directory and
// adds every regular file's SHA to the map keyed by its relative path (e.g.
// "testdata/corpus.txt"). If testdata/ is missing the map is left unchanged.
func collectTestdataSHAs(benchmarkDirectory string, out map[string]string) {
	testdataRoot := filepath.Join(benchmarkDirectory, "testdata")
	info, err := os.Stat(testdataRoot)
	if err != nil || !info.IsDir() {
		return
	}
	_ = filepath.Walk(testdataRoot, func(path string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil || fileInfo == nil || fileInfo.IsDir() {
			return nil
		}
		hash, hashErr := HashSourceFile(path)
		if hashErr != nil {
			return nil
		}
		relativePath, relErr := filepath.Rel(benchmarkDirectory, path)
		if relErr != nil {
			return nil
		}
		out[filepath.ToSlash(relativePath)] = hash
		return nil
	})
}

// discoverSpecs loads the spec of every benchmark directory the filter keeps.
//
// Takes benchmarksDirectory (string) which holds one directory per benchmark.
// Takes config (SuiteConfig) which supplies the filter.
//
// Returns []BenchSpec which is the specs sorted by name.
// Returns error when the directory cannot be read or a spec is invalid.
func discoverSpecs(benchmarksDirectory string, config SuiteConfig) ([]BenchSpec, error) {
	entries, err := os.ReadDir(benchmarksDirectory)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", benchmarksDirectory, err)
	}
	specs := make([]BenchSpec, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		if !config.MatchesFilter(entry.Name()) {
			continue
		}
		specPath := SpecPath(filepath.Join(benchmarksDirectory, entry.Name()))
		spec, err := LoadSpec(specPath)
		if err != nil {
			return nil, err
		}
		if spec.Name != entry.Name() {
			return nil, fmt.Errorf("spec %s: name %q does not match directory %q", specPath, spec.Name, entry.Name())
		}
		specs = append(specs, spec)
	}
	slices.SortFunc(specs, func(left, right BenchSpec) int { return cmp.Compare(left.Name, right.Name) })
	return specs, nil
}

// buildRunners creates the enabled runners, choosing host or container Python.
//
// Takes config (SuiteConfig) which says which runners are enabled.
// Takes benchmarksDirectory (string) which the Go cache and Python containers use.
//
// Returns []Runner which is the runners in report order.
func buildRunners(config SuiteConfig, benchmarksDirectory string) []Runner {
	registered := []Runner{}
	enabled := func(kind RunnerKind) bool { return config.RunnerEnabled(kind) }

	if enabled(RunnerPipit) {
		registered = append(registered, NewPipitRunner())
	}
	if enabled(RunnerGo) {
		registered = append(registered, NewGoRunner(filepath.Join(benchmarksDirectory, ".gocache")))
	}
	if enabled(RunnerCPython) {
		if config.UseHostPython {
			registered = append(registered, NewHostCPythonRunner())
		} else {
			registered = append(registered, NewContainerCPythonRunner(config.CPythonImage, benchmarksDirectory))
		}
	}
	if enabled(RunnerPyPy) {
		if config.UseHostPython {
			registered = append(registered, NewHostPyPyRunner())
		} else {
			registered = append(registered, NewContainerPyPyRunner(config.PyPyImage, benchmarksDirectory))
		}
	}
	if enabled(RunnerYaegi) {
		registered = append(registered, NewYaegiRunner())
	}
	if enabled(RunnerScriggo) {
		registered = append(registered, NewScriggoRunner())
	}
	if enabled(RunnerTengo) {
		registered = append(registered, NewTengoRunner())
	}
	if enabled(RunnerMvm) {
		registered = append(registered, NewMvmRunner())
	}
	return registered
}

// collectHostInfo records the machine and the Python images or versions for the reports.
//
// Takes config (SuiteConfig) which says whether Python runs on the host.
//
// Returns HostInfo which describes the host.
func collectHostInfo(ctx context.Context, config SuiteConfig) HostInfo {
	info := HostInfo{
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
		CPUModel:         "",
		GoVersion:        runtime.Version(),
		PipitCommit:      "",
		CPythonImage:     "",
		CPythonVersion:   "",
		PyPyImage:        "",
		PyPyVersion:      "",
		ContainerRuntime: "",
		CPUCountLogical:  runtime.NumCPU(),
	}
	if config.UseHostPython {
		info.CPythonImage = "host"
		info.CPythonVersion = PythonVersionString(ctx, "python3")
		info.PyPyImage = "host"
		info.PyPyVersion = PythonVersionString(ctx, "pypy3")
	} else {
		info.CPythonImage = config.CPythonImage
		info.PyPyImage = config.PyPyImage
	}
	return info
}

// aggregateOK summarises the successful runs of one cell.
//
// Takes results ([]Result) which are the cell's runs.
// Takes kInner (int) which is the inner-loop iteration count.
//
// Returns Aggregate which is the summary.
// Returns bool which is false when no run succeeded.
func aggregateOK(results []Result, kInner int) (Aggregate, bool) {
	if len(results) == 0 {
		return Aggregate{}, false
	}
	successful := []int64{}
	rssSamples := []int64{}
	compileSamples := []int64{}
	for index := range results {
		result := &results[index]
		if result.Status != StatusOK {
			continue
		}
		sample := result.WallNanos
		if result.Mode == ModeInnerLoop && result.InnerNanos > 0 {
			sample = result.InnerNanos
		}
		successful = append(successful, sample)
		if result.PeakRSSKB > 0 {
			rssSamples = append(rssSamples, result.PeakRSSKB)
		}
		if result.CompileNanos > 0 {
			compileSamples = append(compileSamples, result.CompileNanos)
		}
	}
	if len(successful) == 0 {
		return Aggregate{}, false
	}
	return Summarise(results[0].Benchmark, results[0].Runner, results[0].Mode, successful, rssSamples, compileSamples, kInner), true
}

// isSkippedRunner reports whether the spec opts out of the runner.
//
// Takes spec (BenchSpec) which lists the skipped runners.
// Takes kind (RunnerKind) which is the runner.
//
// Returns bool which is true when the runner is listed.
func isSkippedRunner(spec BenchSpec, kind RunnerKind) bool {
	return slices.Contains(spec.SkipRunners, kind)
}

// sortAggregates orders aggregates by benchmark, then mode, then runner.
//
// Takes aggregates ([]Aggregate) which is sorted in place.
func sortAggregates(aggregates []Aggregate) {
	slices.SortFunc(aggregates, func(left, right Aggregate) int {
		return cmp.Or(
			cmp.Compare(left.Benchmark, right.Benchmark),
			cmp.Compare(left.Mode, right.Mode),
			cmp.Compare(left.Runner, right.Runner),
		)
	})
}
