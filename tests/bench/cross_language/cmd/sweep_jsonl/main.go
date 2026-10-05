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

package main

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
)

type report struct {
	GeneratedAtUTC string `json:"generated_at_utc"`

	Host struct {
		OS              string `json:"os"`
		Arch            string `json:"arch"`
		GoVersion       string `json:"go_version"`
		CPUCountLogical int    `json:"cpu_count_logical"`
	} `json:"host"`

	Runs []struct {
		Benchmark string `json:"benchmark"`
		Runner    string `json:"runner"`
		Mode      string `json:"mode"`
		Status    string `json:"status"`
		Note      string `json:"note"`
	} `json:"runs"`

	Aggregates []aggregate `json:"aggregates"`
}

type aggregate struct {
	Benchmark string `json:"benchmark"`

	Runner string `json:"runner"`

	Mode string `json:"mode"`

	Runs int `json:"runs"`

	MedianNanos int64 `json:"median_nanos"`

	MeanNanos int64 `json:"mean_nanos"`

	StddevNanos int64 `json:"stddev_nanos"`

	MinNanos int64 `json:"min_nanos"`

	P95Nanos int64 `json:"p95_nanos"`

	PeakRSSKB int64 `json:"peak_rss_kb_median"`

	MedianCompileNanos int64 `json:"median_compile_nanos"`

	ColdStartNanos int64 `json:"cold_start_nanos"`
}

const minArgs = 3

var runnerOrder = []string{"pipit", "go", "cpython", "pypy", "yaegi", "scriggo", "tengo", "mvm"}

type rowKey struct {
	runner string

	benchmark string

	mode string
}

type row struct { //nolint:govet // field order is the JSON order
	Lang string `json:"lang"`

	Bench string `json:"bench"`

	Mode string `json:"mode"`

	Status string `json:"status"`

	Reason string `json:"reason"`

	Runs int `json:"runs"`

	CompileNs int64 `json:"compile_ns"`

	RuntimeNs int64 `json:"runtime_ns"`

	ColdStartNs int64 `json:"cold_start_ns"`

	MeanNs int64 `json:"mean_ns"`

	StddevNs int64 `json:"stddev_ns"`

	MinNs int64 `json:"min_ns"`

	P95Ns int64 `json:"p95_ns"`

	PeakRSSKB int64 `json:"peak_rss_kb"`

	Timestamp string `json:"timestamp"`

	HostOS string `json:"host_os"`

	HostArch string `json:"host_arch"`

	GoVersion string `json:"go_version"`

	CPUCountLogical int `json:"cpu_count_logical"`
}

func main() {
	if len(os.Args) < minArgs {
		fmt.Fprintln(os.Stderr, "usage: sweep_jsonl OUT.jsonl REPORT.json [OVERRIDE.json ...]")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "sweep_jsonl:", err)
		os.Exit(1)
	}
}

func run(outPath string, inputs []string) error {
	reports := make([]report, 0, len(inputs))
	for _, path := range inputs {
		data, err := os.ReadFile(path) //nolint:gosec // paths are command-line arguments
		if err != nil {
			return err
		}
		var decoded report
		if err := json.Unmarshal(data, &decoded); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		reports = append(reports, decoded)
	}
	file, err := os.Create(outPath) //nolint:gosec // path is a command-line argument
	if err != nil {
		return err
	}
	rows := convert(reports)
	if err := write(file, rows); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fmt.Printf("wrote %d rows to %s\n", len(rows), outPath)
	return nil
}

func convert(reports []report) []row {
	rows := map[rowKey]row{}
	for index := range reports {
		merge(rows, &reports[index])
	}
	sorted := make([]row, 0, len(rows))
	for key := range rows {
		sorted = append(sorted, rows[key])
	}
	slices.SortFunc(sorted, func(a, b row) int {
		return cmp.Or(
			cmp.Compare(runnerRank(a.Lang), runnerRank(b.Lang)),
			cmp.Compare(a.Bench, b.Bench),
			cmp.Compare(a.Mode, b.Mode),
		)
	})
	return sorted
}

func merge(rows map[rowKey]row, current *report) {
	runners := map[string]bool{}
	reasons := map[rowKey]string{}
	failed := map[rowKey]bool{}
	for _, result := range current.Runs {
		runners[result.Runner] = true
		key := rowKey{result.Runner, result.Benchmark, result.Mode}
		if result.Status != "ok" {
			failed[key] = true
			reasons[key] = cmp.Or(reasons[key], result.Note)
		}
	}
	maps.DeleteFunc(rows, func(key rowKey, _ row) bool { return runners[key.runner] })
	aggregates := map[rowKey]*aggregate{}
	for index := range current.Aggregates {
		value := &current.Aggregates[index]
		aggregates[rowKey{value.Runner, value.Benchmark, value.Mode}] = value
	}
	for _, result := range current.Runs {
		key := rowKey{result.Runner, result.Benchmark, result.Mode}
		value, ok := aggregates[key]
		if failed[key] || !ok {
			value = &aggregate{}
		}
		rows[key] = newRow(key, current, value, cmp.Or(reasons[key], "failed"), failed[key] || !ok)
	}
}

func newRow(key rowKey, current *report, value *aggregate, reason string, failed bool) row {
	status := "ok"
	if failed {
		status = "failed"
	} else {
		reason = ""
	}
	return row{
		Lang: key.runner, Bench: key.benchmark, Mode: key.mode, Status: status, Reason: reason,
		Runs: value.Runs, CompileNs: value.MedianCompileNanos, RuntimeNs: value.MedianNanos,
		ColdStartNs: value.ColdStartNanos, MeanNs: value.MeanNanos, StddevNs: value.StddevNanos,
		MinNs: value.MinNanos, P95Ns: value.P95Nanos, PeakRSSKB: value.PeakRSSKB,
		Timestamp: current.GeneratedAtUTC, HostOS: current.Host.OS, HostArch: current.Host.Arch,
		GoVersion: current.Host.GoVersion, CPUCountLogical: current.Host.CPUCountLogical,
	}
}

func runnerRank(runner string) int {
	if index := slices.Index(runnerOrder, runner); index >= 0 {
		return index
	}
	return len(runnerOrder)
}

func write(writer io.Writer, rows []row) error {
	buffered := bufio.NewWriter(writer)
	encoder := json.NewEncoder(buffered)
	encoder.SetEscapeHTML(false)
	for index := range rows {
		if err := encoder.Encode(&rows[index]); err != nil {
			return err
		}
	}
	return buffered.Flush()
}
