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
	"strings"
	"time"
)

const (
	// reportFileMode is the mode of latest.json and latest.md.
	reportFileMode = 0o600

	// missingValue fills a report cell that has no value.
	missingValue = "-"

	// kibPerMiB converts KiB to MiB.
	kibPerMiB = 1024

	// kibPerGiB converts KiB to GiB.
	kibPerGiB = kibPerMiB * 1024
)

// ReportEnvelope is the layout of latest.json.
type ReportEnvelope struct {
	// GeneratedAtUTC is when the report was written, in RFC 3339.
	GeneratedAtUTC string `json:"generated_at_utc"`

	// Host describes the machine and tool versions.
	Host HostInfo `json:"host"`

	// Runs holds every measured run.
	Runs []Result `json:"runs"`

	// Aggregates summarises the runs per benchmark, runner and mode.
	Aggregates []Aggregate `json:"aggregates"`

	// Config records the run settings.
	Config ReportConf `json:"config"`
}

// ReportConf is the run settings a report records.
type ReportConf struct {
	// Modes lists the modes measured.
	Modes []RunMode `json:"modes"`

	// RunsPerCombo is the number of measured runs per cell.
	RunsPerCombo int `json:"runs_per_combo"`

	// Warmup is the number of unmeasured runs before them.
	Warmup int `json:"warmup"`
}

// WriteJSONReport writes latest.json to the suite's output directory.
//
// Takes suite (*Suite) which supplies the host information, settings and directory.
// Takes runs ([]Result) which holds every measured run.
// Takes aggregates ([]Aggregate) which holds the summaries.
//
// Returns error when encoding or writing fails.
func WriteJSONReport(suite *Suite, runs []Result, aggregates []Aggregate) error {
	envelope := ReportEnvelope{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Host:           suite.hostInfo,
		Config: ReportConf{
			RunsPerCombo: suite.Config.Runs,
			Warmup:       suite.Config.Warmup,
			Modes:        suite.Config.Modes,
		},
		Runs:       runs,
		Aggregates: aggregates,
	}
	payload, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	path := filepath.Join(suite.OutputDirectory, "latest.json")
	return os.WriteFile(path, payload, reportFileMode)
}

// WriteMarkdownReport writes latest.md, the human-readable summary, to the suite's output
// directory.
//
// Takes suite (*Suite) which supplies the host information, settings and directory.
// Takes aggregates ([]Aggregate) which holds the summaries.
//
// Returns error when writing fails.
func WriteMarkdownReport(suite *Suite, aggregates []Aggregate) error {
	var builder strings.Builder
	writeMarkdownHeader(&builder, suite)
	writeMarkdownSummary(&builder, aggregates)
	writeMarkdownPerBenchmark(&builder, aggregates)
	path := filepath.Join(suite.OutputDirectory, "latest.md")
	return os.WriteFile(path, []byte(builder.String()), reportFileMode)
}

func writeMarkdownHeader(builder *strings.Builder, suite *Suite) {
	builder.WriteString("# Pipit cross-language benchmark results\n\n")
	fmt.Fprintf(builder, "_Generated %s_\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(builder, "Host: %s/%s, %d logical CPUs, %s.  ", suite.hostInfo.OS, suite.hostInfo.Arch, suite.hostInfo.CPUCountLogical, suite.hostInfo.GoVersion)
	if suite.hostInfo.CPythonImage != "" {
		fmt.Fprintf(builder, "CPython: %s.  ", suite.hostInfo.CPythonImage)
	}
	if suite.hostInfo.PyPyImage != "" {
		fmt.Fprintf(builder, "PyPy: %s.  ", suite.hostInfo.PyPyImage)
	}
	builder.WriteString("\n\n")
	fmt.Fprintf(builder, "Runs per combo: %d (warmup %d). Modes: ", suite.Config.Runs, suite.Config.Warmup)
	for index, mode := range suite.Config.Modes {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(string(mode))
	}
	builder.WriteString(".\n\n")
}

func writeMarkdownSummary(builder *strings.Builder, aggregates []Aggregate) {
	builder.WriteString("## Summary - cold-start mode (× vs pipit)\n\n")
	builder.WriteString("Cold-start = median compile + median runtime per single iteration. ")
	builder.WriteString("Container exec, subprocess startup, and other methodology overhead are excluded.\n\n")
	benchmarks := uniqueBenchmarks(aggregates)
	if len(benchmarks) == 0 {
		builder.WriteString("_No successful runs to report._\n\n")
		return
	}

	builder.WriteString("| Benchmark ")
	for _, runner := range AllRunners() {
		fmt.Fprintf(builder, "| %s ", runner)
	}
	builder.WriteString("|\n|---")
	for range AllRunners() {
		builder.WriteString("|---:")
	}
	builder.WriteString("|\n")
	for _, benchmark := range benchmarks {
		fmt.Fprintf(builder, "| %s ", benchmark)
		pipitCold := coldStartForCell(aggregates, benchmark, RunnerPipit, ModeInnerLoop)
		for _, runner := range AllRunners() {
			ratio := ratioColdVsPipit(aggregates, benchmark, runner, ModeInnerLoop, pipitCold)
			fmt.Fprintf(builder, "| %s ", ratio)
		}
		builder.WriteString("|\n")
	}
	builder.WriteString("\n")
}

func writeMarkdownPerBenchmark(builder *strings.Builder, aggregates []Aggregate) {
	benchmarks := uniqueBenchmarks(aggregates)
	for _, benchmark := range benchmarks {
		for _, mode := range AllModes() {
			cells := filterByBenchmarkMode(aggregates, benchmark, mode)
			if len(cells) == 0 {
				continue
			}
			fmt.Fprintf(builder, "## %s - %s\n\n", benchmark, mode)
			builder.WriteString("| Runner | Compile | Runtime | Cold-start | Stddev | P95 | Peak RSS | × vs pipit (cold) |\n")
			builder.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|\n")
			pipitCold := coldStartForCell(aggregates, benchmark, RunnerPipit, mode)
			for _, runner := range AllRunners() {
				aggregate, present := findAggregate(cells, runner)
				if !present {
					fmt.Fprintf(builder, "| %s | _missing_ | | | | | | |\n", runner)
					continue
				}
				ratio := ratioColdVsPipit(aggregates, benchmark, runner, mode, pipitCold)
				fmt.Fprintf(builder, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
					runner,
					formatNanos(aggregate.MedianCompileNanos),
					formatNanos(aggregate.MedianNanos),
					formatNanos(aggregate.ColdStartNanos),
					formatNanos(aggregate.StddevNanos),
					formatNanos(aggregate.P95Nanos),
					formatRSS(aggregate.PeakRSSKB),
					ratio,
				)
			}
			builder.WriteString("\n")
		}
	}
}

func uniqueBenchmarks(aggregates []Aggregate) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, aggregate := range aggregates {
		if _, ok := seen[aggregate.Benchmark]; ok {
			continue
		}
		seen[aggregate.Benchmark] = struct{}{}
		out = append(out, aggregate.Benchmark)
	}
	return out
}

func filterByBenchmarkMode(aggregates []Aggregate, benchmark string, mode RunMode) []Aggregate {
	out := []Aggregate{}
	for _, aggregate := range aggregates {
		if aggregate.Benchmark == benchmark && aggregate.Mode == mode {
			out = append(out, aggregate)
		}
	}
	return out
}

func findAggregate(cells []Aggregate, runner RunnerKind) (Aggregate, bool) {
	for _, aggregate := range cells {
		if aggregate.Runner == runner {
			return aggregate, true
		}
	}
	return Aggregate{}, false
}

func coldStartForCell(aggregates []Aggregate, benchmark string, runner RunnerKind, mode RunMode) int64 {
	for _, aggregate := range aggregates {
		if aggregate.Benchmark == benchmark && aggregate.Runner == runner && aggregate.Mode == mode {
			return aggregate.ColdStartNanos
		}
	}
	return 0
}

func ratioColdVsPipit(aggregates []Aggregate, benchmark string, runner RunnerKind, mode RunMode, pipitCold int64) string {
	if runner == RunnerPipit {
		return "1.00×"
	}
	if pipitCold == 0 {
		return missingValue
	}
	for _, aggregate := range aggregates {
		if aggregate.Benchmark == benchmark && aggregate.Runner == runner && aggregate.Mode == mode {
			if aggregate.ColdStartNanos == 0 {
				return missingValue
			}
			ratio := float64(aggregate.ColdStartNanos) / float64(pipitCold)
			return fmt.Sprintf("%.2f×", ratio)
		}
	}
	return "_miss_"
}

// formatNanos renders a duration in the largest unit below it.
//
// Takes nanos (int64) which is the duration.
//
// Returns string which is the duration with its unit, or missingValue when not positive.
func formatNanos(nanos int64) string {
	if nanos <= 0 {
		return missingValue
	}
	duration := time.Duration(nanos)
	switch {
	case duration < time.Microsecond:
		return fmt.Sprintf("%d ns", nanos)
	case duration < time.Millisecond:
		return fmt.Sprintf("%.1f µs", float64(nanos)/float64(time.Microsecond))
	case duration < time.Second:
		return fmt.Sprintf("%.2f ms", float64(nanos)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.2f s", duration.Seconds())
	}
}

// formatRSS renders a memory size in the largest binary unit below it.
//
// Takes kib (int64) which is the size in KiB.
//
// Returns string which is the size with its unit, or missingValue when not positive.
func formatRSS(kib int64) string {
	if kib <= 0 {
		return missingValue
	}
	switch {
	case kib < kibPerMiB:
		return fmt.Sprintf("%d KiB", kib)
	case kib < kibPerGiB:
		return fmt.Sprintf("%.1f MiB", float64(kib)/kibPerMiB)
	default:
		return fmt.Sprintf("%.2f GiB", float64(kib)/kibPerGiB)
	}
}
