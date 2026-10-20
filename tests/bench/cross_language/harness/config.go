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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	// StatusOK marks a run whose output matched the expected digest.
	StatusOK = "ok"

	// StatusSkipped marks a runner that was not available or not asked for.
	StatusSkipped = "skipped"

	// StatusFailed marks a run that did not complete.
	StatusFailed = "failed"

	// StatusMismatch marks a run whose output differed from the expected digest.
	StatusMismatch = "mismatch"

	// defaultTimeoutSeconds bounds one run when a spec sets no timeout.
	defaultTimeoutSeconds = 120

	// defaultRuns is the number of measured runs per cell.
	defaultRuns = 7

	// defaultWarmup is the number of unmeasured runs before the measured ones.
	defaultWarmup = 2

	// envEnabled is the value that turns an on/off environment variable on.
	envEnabled = "1"

	// specFileMode is the mode of a regenerated spec.json, which is committed.
	specFileMode = 0o644
)

// RunMode says how much of a program a measurement covers.
type RunMode string

const (
	// ModeEndToEnd times the whole process, start-up and compilation included.
	ModeEndToEnd RunMode = "endtoend"

	// ModeInnerLoop times only the benchmark's own inner loop.
	ModeInnerLoop RunMode = "innerloop"
)

// RunnerKind names an implementation the suite runs each benchmark on.
type RunnerKind string

const (
	// RunnerPipit runs the Go source under pipit.
	RunnerPipit RunnerKind = "pipit"

	// RunnerGo runs the Go source compiled natively.
	RunnerGo RunnerKind = "go"

	// RunnerCPython runs the Python port under CPython.
	RunnerCPython RunnerKind = "cpython"

	// RunnerPyPy runs the Python port under PyPy.
	RunnerPyPy RunnerKind = "pypy"

	// RunnerYaegi runs the Go source under the yaegi interpreter.
	RunnerYaegi RunnerKind = "yaegi"

	// RunnerScriggo runs the Go source under the scriggo interpreter.
	RunnerScriggo RunnerKind = "scriggo"

	// RunnerTengo runs the Tengo port.
	RunnerTengo RunnerKind = "tengo"

	// RunnerMvm runs the Go source under the mvm interpreter.
	RunnerMvm RunnerKind = "mvm"
)

// BenchSpec describes what a benchmark runs and its expected output in spec.json.
type BenchSpec struct {
	// SourceSHAs maps each source file to its SHA-256, so a stale expected digest is
	// noticed.
	SourceSHAs map[string]string `json:"source_sha256"`

	// Name is the benchmark's directory name.
	Name string `json:"name"`

	// Description says what the benchmark computes.
	Description string `json:"description"`

	// ExpectedStdoutSHA is the SHA-256 of the correct output.
	ExpectedStdoutSHA string `json:"expected_stdout_sha256"`

	// Args are the command-line arguments every runner passes.
	Args []string `json:"args"`

	// Inputs lists the data files the benchmark reads.
	Inputs []string `json:"inputs,omitempty"`

	// SkipRunners lists the runners this benchmark does not support.
	SkipRunners []RunnerKind `json:"skip_runners,omitempty"`

	// KInner is the number of inner-loop iterations an inner-loop run performs.
	KInner int `json:"k_inner"`

	// TimeoutSeconds bounds one run.
	TimeoutSeconds int `json:"timeout_seconds"`
}

// Result is one run of one benchmark on one runner in one mode.
type Result struct {
	// Benchmark is the benchmark's name.
	Benchmark string `json:"benchmark"`

	// Runner is the implementation that ran it.
	Runner RunnerKind `json:"runner"`

	// Mode is what the timing covers.
	Mode RunMode `json:"mode"`

	// StdoutSHA is the SHA-256 of the run's output.
	StdoutSHA string `json:"stdout_sha256"`

	// Status is one of the Status constants.
	Status string `json:"status"`

	// Note explains a skip or a failure.
	Note string `json:"note,omitempty"`

	// StderrTail is the end of the run's standard error, kept for failures.
	StderrTail string `json:"stderr_tail,omitempty"`

	// RunIndex numbers the run within its cell; warm-up runs are not recorded.
	RunIndex int `json:"run_index"`

	// WallNanos is the run's wall-clock time.
	WallNanos int64 `json:"wall_nanos"`

	// InnerNanos is the inner loop's time, as the program reported it.
	InnerNanos int64 `json:"inner_nanos,omitempty"`

	// CompileNanos is the compilation time, where the runner reports it.
	CompileNanos int64 `json:"compile_nanos,omitempty"`

	// PeakRSSKB is the process's peak resident set size in KiB.
	PeakRSSKB int64 `json:"peak_rss_kb"`
}

// Aggregate summarises the runs of one benchmark on one runner in one mode.
type Aggregate struct {
	// Benchmark is the benchmark's name.
	Benchmark string `json:"benchmark"`

	// Runner is the implementation that ran it.
	Runner RunnerKind `json:"runner"`

	// Mode is what the timings cover.
	Mode RunMode `json:"mode"`

	// Runs is the number of measured runs that succeeded.
	Runs int `json:"runs"`

	// MedianNanos is the median time.
	MedianNanos int64 `json:"median_nanos"`

	// MeanNanos is the mean time.
	MeanNanos int64 `json:"mean_nanos"`

	// StddevNanos is the sample standard deviation.
	StddevNanos int64 `json:"stddev_nanos"`

	// MinNanos is the fastest run.
	MinNanos int64 `json:"min_nanos"`

	// P95Nanos is the 95th percentile.
	P95Nanos int64 `json:"p95_nanos"`

	// PeakRSSKB is the median peak resident set size in KiB.
	PeakRSSKB int64 `json:"peak_rss_kb_median"`

	// MedianCompileNanos is the median compilation time, where reported.
	MedianCompileNanos int64 `json:"median_compile_nanos,omitempty"`

	// ColdStartNanos estimates compilation plus one iteration using median timings.
	ColdStartNanos int64 `json:"cold_start_nanos,omitempty"`
}

// HostInfo records the machine and tool versions a report was produced on.
type HostInfo struct {
	// OS is runtime.GOOS.
	OS string `json:"os"`

	// Arch is runtime.GOARCH.
	Arch string `json:"arch"`

	// CPUModel is the processor's model name, where known.
	CPUModel string `json:"cpu_model,omitempty"`

	// GoVersion is the Go toolchain version.
	GoVersion string `json:"go_version,omitempty"`

	// PipitCommit is the pipit revision under test, where known.
	PipitCommit string `json:"pipit_commit,omitempty"`

	// CPythonImage is the container image CPython ran in.
	CPythonImage string `json:"cpython_image,omitempty"`

	// CPythonVersion is the CPython version reported by that image.
	CPythonVersion string `json:"cpython_version,omitempty"`

	// PyPyImage is the container image PyPy ran in.
	PyPyImage string `json:"pypy_image,omitempty"`

	// PyPyVersion is the PyPy version reported by that image.
	PyPyVersion string `json:"pypy_version,omitempty"`

	// ContainerRuntime is the container engine used, such as docker or podman.
	ContainerRuntime string `json:"container_runtime,omitempty"`

	// CPUCountLogical is the number of logical CPUs.
	CPUCountLogical int `json:"cpu_count_logical"`
}

// SuiteConfig is the suite's configuration, read from CROSS_LANG_* environment variables.
type SuiteConfig struct {
	// CPythonImage is the CPython container image.
	CPythonImage string

	// OutputDirectory is where reports are written.
	OutputDirectory string

	// PyPyImage is the PyPy container image.
	PyPyImage string

	// Runners lists the runners to use.
	Runners []RunnerKind

	// Modes lists the modes to measure.
	Modes []RunMode

	// RequireRunners lists the runners whose absence fails the suite instead of being
	// skipped.
	RequireRunners []RunnerKind

	// Filter keeps only benchmarks whose name contains one of these fragments; empty keeps
	// all.
	Filter []string

	// Runs is the number of measured runs per cell.
	Runs int

	// Warmup is the number of unmeasured runs before them.
	Warmup int

	// UseHostPython runs Python on the host instead of in a container.
	UseHostPython bool

	// Regenerate rewrites each spec's expected output and source digests from a native Go
	// run instead of measuring.
	Regenerate bool
}

// MatchesFilter reports whether the benchmark should run under the configured filter.
//
// Takes benchmarkName (string) which is the benchmark's directory name.
//
// Returns bool which is true when no filter is set or a fragment matches.
func (config SuiteConfig) MatchesFilter(benchmarkName string) bool {
	if len(config.Filter) == 0 {
		return true
	}
	return slices.ContainsFunc(config.Filter, func(fragment string) bool {
		return strings.Contains(benchmarkName, fragment)
	})
}

// RunnerEnabled reports whether the runner is configured to run.
//
// Takes kind (RunnerKind) which is the runner.
//
// Returns bool which is true when it is in Runners.
func (config SuiteConfig) RunnerEnabled(kind RunnerKind) bool {
	return slices.Contains(config.Runners, kind)
}

// RunnerRequired reports whether the runner must be available.
//
// Takes kind (RunnerKind) which is the runner.
//
// Returns bool which is true when it is in RequireRunners.
func (config SuiteConfig) RunnerRequired(kind RunnerKind) bool {
	return slices.Contains(config.RequireRunners, kind)
}

// AllModes lists every run mode.
//
// Returns []RunMode which is the modes in report order.
func AllModes() []RunMode {
	return []RunMode{ModeEndToEnd, ModeInnerLoop}
}

// AllRunners lists every runner.
//
// Returns []RunnerKind which is the runners in report order.
func AllRunners() []RunnerKind {
	return []RunnerKind{RunnerPipit, RunnerGo, RunnerCPython, RunnerPyPy, RunnerYaegi, RunnerScriggo, RunnerTengo, RunnerMvm}
}

// LoadSpec reads and checks a benchmark's spec.json.
//
// Takes path (string) which is the spec file.
//
// Returns BenchSpec which is the spec, with the default timeout filled in.
// Returns error when the file cannot be read or parsed, or a required field is missing.
func LoadSpec(path string) (BenchSpec, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return BenchSpec{}, fmt.Errorf("read spec %s: %w", path, err)
	}
	var spec BenchSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return BenchSpec{}, fmt.Errorf("parse spec %s: %w", path, err)
	}
	if spec.Name == "" {
		return BenchSpec{}, fmt.Errorf("spec %s: missing name", path)
	}
	if spec.KInner <= 0 {
		return BenchSpec{}, fmt.Errorf("spec %s: k_inner must be > 0", path)
	}
	if spec.TimeoutSeconds <= 0 {
		spec.TimeoutSeconds = defaultTimeoutSeconds
	}
	return spec, nil
}

// LoadSuiteConfig reads the suite's configuration from the environment.
//
// Returns SuiteConfig which is the configuration, with at least one measured run.
func LoadSuiteConfig() SuiteConfig {
	config := SuiteConfig{
		Runs:            envIntDefault("CROSS_LANG_RUNS", defaultRuns),
		Warmup:          envIntDefault("CROSS_LANG_WARMUP", defaultWarmup),
		Filter:          envSplit("CROSS_LANG_FILTER"),
		Runners:         envRunners("CROSS_LANG_RUNNERS", AllRunners()),
		Modes:           envModes("CROSS_LANG_MODE", AllModes()),
		UseHostPython:   envFlag("CROSS_LANG_USE_HOST_PYTHON"),
		RequireRunners:  envRunners("CROSS_LANG_REQUIRE", nil),
		Regenerate:      envFlag("CROSS_LANG_REGEN"),
		CPythonImage:    envStringDefault("CROSS_LANG_CPYTHON_IMAGE", "python:3.14-slim"),
		PyPyImage:       envStringDefault("CROSS_LANG_PYPY_IMAGE", "pypy:3.11-slim"),
		OutputDirectory: envStringDefault("CROSS_LANG_OUTPUT_DIR", "results"),
	}
	if config.Runs < 1 {
		config.Runs = 1
	}
	if config.Warmup < 0 {
		config.Warmup = 0
	}
	return config
}

// SpecPath returns the spec file of a benchmark directory.
//
// Takes benchmarkDir (string) which is the benchmark's directory.
//
// Returns string which is its spec.json path.
func SpecPath(benchmarkDir string) string {
	return filepath.Join(benchmarkDir, "spec.json")
}

// saveSpec writes spec to path as indented JSON.
//
// Takes path (string) which is the spec file.
// Takes spec (BenchSpec) which is the spec to write.
//
// Returns error when encoding or writing fails.
func saveSpec(path string, spec BenchSpec) error {
	payload, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spec %s: %w", path, err)
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, specFileMode)
}

// envFlag reports whether an on/off environment variable is on.
//
// Takes name (string) which is the variable.
//
// Returns bool which is true when it is set to envEnabled.
func envFlag(name string) bool {
	return os.Getenv(name) == envEnabled
}

// envIntDefault reads an integer environment variable.
//
// Takes name (string) which is the variable.
// Takes fallback (int) which is used when it is unset or not a number.
//
// Returns int which is the value.
func envIntDefault(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

// envStringDefault reads a string environment variable.
//
// Takes name (string) which is the variable.
// Takes fallback (string) which is used when it is unset or empty.
//
// Returns string which is the value.
func envStringDefault(name, fallback string) string {
	if raw := os.Getenv(name); raw != "" {
		return raw
	}
	return fallback
}

// envSplit reads a comma-separated environment variable.
//
// Takes name (string) which is the variable.
//
// Returns []string which holds the trimmed, non-empty parts, or nil when it is unset.
func envSplit(name string) []string {
	raw := os.Getenv(name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// envRunners reads a comma-separated list of runners.
//
// Takes name (string) which is the variable.
// Takes fallback ([]RunnerKind) which is used when it is unset.
//
// Returns []RunnerKind which is the lower-cased runners.
func envRunners(name string, fallback []RunnerKind) []RunnerKind {
	parts := envSplit(name)
	if len(parts) == 0 {
		return fallback
	}
	out := make([]RunnerKind, 0, len(parts))
	for _, part := range parts {
		out = append(out, RunnerKind(strings.ToLower(part)))
	}
	return out
}

// envModes reads the run mode selection.
//
// Takes name (string) which is the variable.
// Takes fallback ([]RunMode) which is used when it is unset or not recognised.
//
// Returns []RunMode which is the selected modes.
func envModes(name string, fallback []RunMode) []RunMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "both", "all":
		return AllModes()
	case "endtoend":
		return []RunMode{ModeEndToEnd}
	case "innerloop":
		return []RunMode{ModeInnerLoop}
	default:
		return fallback
	}
}
