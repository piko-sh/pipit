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

//go:build pipit_opcode_census

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/tests/bench/cross_language/harness"
)

const sumBenchmark = `package main

func sum(limit int) int {
	total := 0
	for index := 0; index < limit; index++ {
		total += index
	}
	return total
}

func RunInner(k int) (string, int64) {
	total := 0
	for range k {
		total = sum(100)
	}
	if total != 4950 {
		return "wrong", 0
	}
	return "4950", 0
}
`

func writeBenchmark(t *testing.T, root, name, spec string) {
	t.Helper()
	directory := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Join(directory, sourceDirectory), outputDirectoryPermissions))
	require.NoError(t, os.WriteFile(filepath.Join(directory, sourceDirectory, sourceFileName), []byte(sumBenchmark), outputFilePermissions))
	if spec != "" {
		require.NoError(t, os.WriteFile(filepath.Join(directory, specFileName), []byte(spec), outputFilePermissions))
	}
}

func TestRunMeasuresABenchmarkAndWritesEveryOutputAskedFor(t *testing.T) {
	root, outputs := t.TempDir(), t.TempDir()
	digest := harness.SHA256Hex(harness.NormaliseStdout([]byte("4950")))
	writeBenchmark(t, root, "01_checked", `{"expected_stdout_sha256":"`+digest+`"}`)
	writeBenchmark(t, root, "02_unchecked", "")
	before, err := os.Getwd()
	require.NoError(t, err)
	jsonPath, rankDirectory := filepath.Join(outputs, "report.json"), filepath.Join(outputs, "rank")

	var output bytes.Buffer
	err = run(t.Context(), []string{"-root", root, "-json", jsonPath, "-rank-out", rankDirectory, "-annotate", "sum", "-k", "2"}, &output)

	require.NoError(t, err)
	after, err := os.Getwd()
	require.NoError(t, err)
	require.Equal(t, before, after, "the working directory is put back after each benchmark")
	text := output.String()
	require.Contains(t, text, "## 01_checked:")
	require.Contains(t, text, "## 02_unchecked:")
	require.Equal(t, 1, bytes.Count(output.Bytes(), []byte("output not checked")), "only the benchmark with no spec is flagged")
	require.Contains(t, text, functionHeaderPrefix+"sum", "the named function's listing is printed")
	byName, err := readReports(jsonPath)
	require.NoError(t, err)
	require.Len(t, byName, 2)
	require.Positive(t, byName["01_checked"].Total)
	require.FileExists(t, filepath.Join(rankDirectory, "01_checked.json"))
	require.FileExists(t, filepath.Join(rankDirectory, "02_unchecked.json"))

	output.Reset()
	require.NoError(t, run(t.Context(), []string{"-root", root, "-filter", "checked$", "-against", jsonPath}, &output))
	require.Contains(t, output.String(), "(was ", "an earlier report is compared with")
}

func TestRunStopsAtABenchmarkThatReturnsTheWrongOutput(t *testing.T) {
	root := t.TempDir()
	writeBenchmark(t, root, "01_wrong", `{"expected_stdout_sha256":"00"}`)

	err := run(t.Context(), []string{"-root", root}, &bytes.Buffer{})

	require.ErrorIs(t, err, errResultMismatch)
	require.ErrorContains(t, err, "01_wrong")
}

func TestRunNamesTheBenchmarkThatDoesNotCompile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "01_broken", sourceDirectory)
	require.NoError(t, os.MkdirAll(directory, outputDirectoryPermissions))
	require.NoError(t, os.WriteFile(filepath.Join(directory, sourceFileName), []byte("package main\n\nfunc RunInner("), outputFilePermissions))

	err := run(t.Context(), []string{"-root", root}, &bytes.Buffer{})

	require.ErrorContains(t, err, "01_broken")
	require.ErrorContains(t, err, "compiling")
}
