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
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/tests/bench/cross_language/harness"
)

func TestParseOptionsValidatesEveryFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr   error
		name      string
		arguments []string
	}{
		{name: "the defaults"},
		{name: "every flag given", arguments: []string{"-root", "r", "-filter", "^0", "-json", "j", "-against", "a", "-rank-out", "o", "-annotate", "f", "-k", "3", "-top", "0"}},
		{name: "a negative list length", arguments: []string{"-top", "-1"}, wantErr: errInvalidFlag},
		{name: "no inner run", arguments: []string{"-k", "0"}, wantErr: errInvalidFlag},
		{name: "a filter that is not a pattern", arguments: []string{"-filter", "("}, wantErr: errInvalidFlag},
		{name: "a stray argument", arguments: []string{"benchmarks"}, wantErr: errUnexpectedArguments},
		{name: "help", arguments: []string{"-h"}, wantErr: flag.ErrHelp},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings, err := parseOptions(tt.arguments, &bytes.Buffer{})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.GreaterOrEqual(t, settings.innerRuns, 1)
		})
	}

	t.Run("every flag reaches the setting it names", func(t *testing.T) {
		t.Parallel()

		settings, err := parseOptions(
			[]string{"-root", "r", "-filter", "^0", "-json", "j", "-against", "a", "-rank-out", "o", "-annotate", "f", "-k", "3", "-top", "0"},
			&bytes.Buffer{})

		require.NoError(t, err)
		require.NotNil(t, settings.filter)
		pattern := settings.filter.String()
		settings.filter = nil
		require.Equal(t, "^0", pattern)
		require.Equal(t, options{root: "r", jsonPath: "j", againstPath: "a", rankDirectory: "o", annotate: "f", innerRuns: 3, top: 0}, settings)
	})

	t.Run("a flag that does not exist is reported once, by the error", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer

		_, err := parseOptions([]string{"-nonesuch"}, &output)

		require.ErrorContains(t, err, "nonesuch")
		require.Empty(t, output.String(), "nothing is written where the report goes")
	})

	t.Run("help prints the usage where it was asked for", func(t *testing.T) {
		t.Parallel()
		var output bytes.Buffer

		_, err := parseOptions([]string{"-h"}, &output)

		require.ErrorIs(t, err, flag.ErrHelp)
		require.Contains(t, output.String(), "-filter")
	})

	t.Run("the defaults select the suite from the module the command runs in", func(t *testing.T) {
		t.Parallel()

		settings, err := parseOptions(nil, &bytes.Buffer{})

		require.NoError(t, err)
		require.Equal(t, defaultRoot, settings.root)
		require.Equal(t, defaultListLength, settings.top)
		require.Equal(t, 1, settings.innerRuns)
		require.Nil(t, settings.filter)
	})

	t.Run("an unknown flag", func(t *testing.T) {
		t.Parallel()

		_, err := parseOptions([]string{"-nope"}, &bytes.Buffer{})

		require.ErrorContains(t, err, "parsing flags")
	})
}

func TestRunPrintsItsUsageWithoutFailing(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer

	require.NoError(t, run(t.Context(), []string{"-h"}, &output))
	require.Contains(t, output.String(), "-rank-out")
}

func TestBenchmarkNamesListsDirectoriesThatHoldASource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"02_second", "01_first", "03_no_source"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, sourceDirectory), outputDirectoryPermissions))
	}
	for _, name := range []string{"02_second", "01_first"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name, sourceDirectory, sourceFileName), []byte("package main\n"), outputFilePermissions))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "a_file"), nil, outputFilePermissions))

	all, err := benchmarkNames(options{root: root})
	require.NoError(t, err)
	require.Equal(t, []string{"01_first", "02_second"}, all, "in name order, without the directory that has no source")

	settings, err := parseOptions([]string{"-root", root, "-filter", "second"}, &bytes.Buffer{})
	require.NoError(t, err)
	filtered, err := benchmarkNames(settings)
	require.NoError(t, err)
	require.Equal(t, []string{"02_second"}, filtered)

	settings, err = parseOptions([]string{"-root", root, "-filter", "nothing"}, &bytes.Buffer{})
	require.NoError(t, err)
	_, err = benchmarkNames(settings)
	require.ErrorIs(t, err, errNoBenchmark)

	_, err = benchmarkNames(options{root: filepath.Join(root, "missing")})
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestBenchmarkNamesReportsABenchmarkItCannotLookInto(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("the superuser can look into any directory")
	}

	root := t.TempDir()
	closed := filepath.Join(root, "01_closed")
	require.NoError(t, os.MkdirAll(filepath.Join(closed, sourceDirectory), outputDirectoryPermissions))
	require.NoError(t, os.Chmod(closed, 0))
	t.Cleanup(func() { require.NoError(t, os.Chmod(closed, outputDirectoryPermissions)) })

	_, err := benchmarkNames(options{root: root})

	require.ErrorIs(t, err, fs.ErrPermission, "a benchmark that cannot be read is an error, not a benchmark that is absent")
	require.ErrorContains(t, err, "01_closed")
}

func TestCheckResultComparesTheOutputWithTheRecordedDigest(t *testing.T) {
	t.Parallel()

	const output = "42  \r\nline two\n"
	digest := harness.SHA256Hex(harness.NormaliseStdout([]byte(output)))
	tests := []struct {
		wantErr     error
		result      any
		name        string
		spec        string
		wantChecked bool
	}{
		{name: "the recorded output", spec: `{"expected_stdout_sha256":"` + digest + `"}`, result: output, wantChecked: true},
		{name: "the same output with other line ends", spec: `{"expected_stdout_sha256":"` + digest + `"}`, result: "42\rline two", wantChecked: true},
		{name: "another output", spec: `{"expected_stdout_sha256":"` + digest + `"}`, result: "43\n", wantErr: errResultMismatch},
		{name: "a result that is not text", spec: `{"expected_stdout_sha256":"` + digest + `"}`, result: 42, wantErr: errResultMismatch},
		{name: "a spec with no digest", spec: `{"name":"x"}`, result: output},
		{name: "no spec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if tt.spec != "" {
				require.NoError(t, os.WriteFile(filepath.Join(directory, specFileName), []byte(tt.spec), outputFilePermissions))
			}

			checked, err := checkResult(directory, tt.result)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantChecked, checked)
		})
	}

	t.Run("a spec that is not JSON", func(t *testing.T) {
		t.Parallel()
		directory := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(directory, specFileName), []byte("{"), outputFilePermissions))

		_, err := checkResult(directory, output)

		require.ErrorContains(t, err, "decoding spec")
	})
}

func countedBody() censusCapture {
	hot := &program.CompiledFunction{Name: "hot", Body: []isa.Instruction{
		isa.NewInstruction(isa.OpAddInt64, 0, 1, 2),
		isa.NewInstruction(isa.OpMakeClosure, 0, 0, 0),
		isa.NewInstruction(isa.OpGetMap, 0, 1, 2),
		isa.NewTier1Instruction(isa.SubOpCallNative, 0, 0),
	}}
	cold := &program.CompiledFunction{Name: "cold", Body: []isa.Instruction{isa.NewInstruction(isa.OpAddInt64, 0, 1, 2)}}
	return censusCapture{
		functions: []countedFunction{
			{function: cold, counts: []uint64{5}},
			{function: hot, counts: []uint64{60, 20, 10, 5, 99}},
		},
		total: 100,
	}
}

func TestBuildReportSplitsTheCountsByPathOperationAndFunction(t *testing.T) {
	t.Parallel()

	report := buildReport("example", countedBody())

	require.Equal(t, "example", report.Name)
	require.Equal(t, uint64(100), report.Total)
	require.Equal(t, uint64(100), report.Counted, "a count past the end of a body is left out")
	require.Equal(t, pathCounts{Assembly: 65, Shim: 20, Exit: 10, Generic: 5}, report.Paths)
	require.Equal(t, []functionCount{{Name: "hot", Count: 95, Length: 4}, {Name: "cold", Count: 5, Length: 1}}, report.Functions)
	require.Len(t, report.Operations, 4)
	require.Equal(t, uint64(65), report.Operations[0].Count, "the two functions' additions are one operation, listed first")
	require.Equal(t, markerAssembly, report.Operations[0].Path)
	paths := map[string]string{}
	for _, operation := range report.Operations {
		paths[operation.Path] = operation.Name
	}
	require.Len(t, paths, 4, "one operation on each dispatch path")
}

func TestCompareCountsPutsLargerCountsFirstAndThenNamesInOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		leftName, rightName   string
		leftCount, rightCount uint64
		want                  int
	}{
		{name: "the larger count sorts first", leftCount: 9, rightCount: 3, leftName: "b", rightName: "a", want: -1},
		{name: "the smaller count sorts last", leftCount: 3, rightCount: 9, leftName: "a", rightName: "b", want: 1},
		{name: "equal counts fall back to the name", leftCount: 3, rightCount: 3, leftName: "a", rightName: "b", want: -1},
		{name: "equal entries", leftCount: 3, rightCount: 3, leftName: "a", rightName: "a", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, compareCounts(tt.leftCount, tt.rightCount, tt.leftName, tt.rightName))
		})
	}
}

func TestShareIsAPercentageAndZeroOfNothing(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 25.0, share(1, 4), 1e-9)
	require.InDelta(t, 150.0, share(3, 2), 1e-9)
	require.Zero(t, share(3, 0))
}

func TestOwnListingStopsAtTheFirstNestedFunction(t *testing.T) {
	t.Parallel()

	listing := functionHeaderPrefix + "outer\n0000 A ADD\n0001 A RETURN\n\n" + functionHeaderPrefix + "outer.func1\n0000 A RETURN\n"

	require.Equal(t, []string{functionHeaderPrefix + "outer", "0000 A ADD", "0001 A RETURN", ""}, ownListing(listing))
	require.Equal(t, []string{"0000 A ADD"}, ownListing("0000 A ADD\n"), "a listing with no header is all the function's own")
}

func TestListingProgramCounterReadsTheNumberThatStartsAnInstructionLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line   string
		want   int
		wantOK bool
	}{
		{line: "0000 A 0:ADD_INT64", want: 0, wantOK: true},
		{line: "0042 G 1:CALL_NATIVE", want: 42, wantOK: true},
		{line: "12345 A 0:ADD_INT64", want: 12345, wantOK: true},
		{line: functionHeaderPrefix + "f"},
		{line: ""},
		{line: "0042"},
		{line: " 0042 A"},
		{line: "00x2 A 0:ADD_INT64"},
		{line: "-042 A 0:ADD_INT64"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			got, ok := listingProgramCounter(tt.line)

			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestWriteProgramListsWhatIsNotInAssemblyAndTheBusiestFunctions(t *testing.T) {
	t.Parallel()

	report := buildReport("example", countedBody())
	earlier := report
	earlier.Total = 200

	var output bytes.Buffer
	writeProgram(&output, options{top: 2}, report, &earlier)

	text := output.String()
	require.Contains(t, text, "## example: 100 dispatches (was 200, -50.00%)")
	require.Contains(t, text, "assembly 65.00%  shim 20.00%  exit 10.00%  generic exit 5.00%")
	require.Contains(t, text, "S ")
	require.Contains(t, text, "X ")
	require.NotContains(t, text, "G ", "the list stops at the length asked for")
	require.Contains(t, text, "hot")
	require.Contains(t, text, "cold")

	output.Reset()
	writeProgram(&output, options{top: 0}, report, nil)
	require.NotContains(t, output.String(), "was")
	require.NotContains(t, output.String(), "hot", "a length of zero lists nothing")
}

func TestWriteAnnotationPrintsEachWordsShareOrSaysNothingMatched(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	writeAnnotation(&output, options{}, countedBody())
	require.Empty(t, output.String(), "nothing is annotated unless a function is named")

	writeAnnotation(&output, options{annotate: "hot"}, countedBody())
	require.Contains(t, output.String(), " 60.000% 0000")
	require.Contains(t, output.String(), functionHeaderPrefix+"hot")

	output.Reset()
	writeAnnotation(&output, options{annotate: "nosuch"}, countedBody())
	require.Contains(t, output.String(), "no counted function is named nosuch")
}

func TestReportsAreReadBackAsTheyWereWritten(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "report.json")
	reports := []programReport{buildReport("one", countedBody()), buildReport("two", censusCapture{total: 7})}

	require.NoError(t, writeReports(path, reports))
	byName, err := readReports(path)

	require.NoError(t, err)
	require.Len(t, byName, 2)
	require.Equal(t, reports[0].Paths, byName["one"].Paths)
	require.Equal(t, uint64(7), byName["two"].Total)
}

func TestReportsAreOptional(t *testing.T) {
	t.Parallel()

	require.NoError(t, writeReports("", nil), "no path means no file")
	byName, err := readReports("")
	require.NoError(t, err)
	require.Empty(t, byName)

	_, err = readReports(filepath.Join(t.TempDir(), "missing.json"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	garbled := filepath.Join(t.TempDir(), "garbled.json")
	require.NoError(t, os.WriteFile(garbled, []byte("{"), outputFilePermissions))
	_, err = readReports(garbled)
	require.ErrorContains(t, err, "decoding")

	require.Error(t, writeReports(filepath.Join(t.TempDir(), "missing", "report.json"), nil))
}

func TestWriteRankInputWritesTheRawCensusUnderTheBenchmarksName(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	capture := censusCapture{raw: []byte(`{"total":1}`)}

	require.NoError(t, writeRankInput(options{}, "example", capture), "nothing is written unless a directory is named")
	require.NoError(t, writeRankInput(options{rankDirectory: directory}, "example", capture))

	written, err := os.ReadFile(filepath.Join(directory, "example.json"))
	require.NoError(t, err)
	require.JSONEq(t, `{"total":1}`, string(written))

	require.Error(t, writeRankInput(options{rankDirectory: filepath.Join(directory, "missing")}, "example", capture))
}

func TestReadBoundedRefusesAFilePastItsLimit(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	small, large := filepath.Join(directory, "small"), filepath.Join(directory, "large")
	require.NoError(t, os.WriteFile(small, []byte("content"), outputFilePermissions))
	require.NoError(t, os.WriteFile(large, []byte("contents"), outputFilePermissions))
	const limit = len("content")

	content, err := readBounded(small, limit)
	require.NoError(t, err)
	require.Equal(t, "content", string(content), "a file of exactly the limit is read whole")

	_, err = readBounded(large, limit)
	require.ErrorIs(t, err, errFileTooLarge, "one byte more is refused")

	_, err = readBounded(filepath.Join(directory, "missing"), limit)
	require.ErrorIs(t, err, fs.ErrNotExist)
}
