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

// Command dispatch_report runs benchmarks under the opcode census and reports instruction
// counts and dispatch paths.
//
// Build with the census tag, from the tests/bench module.
//
//	go run -tags pipit_opcode_census ./cross_language/cmd/dispatch_report
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/sdk/stdlib"
	"pipit.sh/pipit/tests/bench/cross_language/harness"
)

const (
	// maxCallDepth is the call-stack ceiling, high enough for the recursive benchmarks.
	maxCallDepth = 200_000

	// entrypointName is the wrapper function each benchmark is entered through.
	entrypointName = "EntrypointRun"

	// sourceFileName is the benchmark source within a benchmark's go directory.
	sourceFileName = "pipit_source.go"

	// specFileName is the file in a benchmark's directory that records its expected output.
	specFileName = "spec.json"

	// percent scales a fraction for printing.
	percent = 100

	// outputFilePermissions are the permissions of the files the command writes.
	outputFilePermissions = 0o600

	// outputDirectoryPermissions are the permissions of the directories it creates.
	outputDirectoryPermissions = 0o750

	// functionHeaderPrefix starts the line that introduces a function in a listing.
	functionHeaderPrefix = "; function "

	// defaultListLength is how many operations and functions a section lists by default.
	defaultListLength = 12

	// defaultRoot is the benchmark directory, from the module the command is run in.
	defaultRoot = "cross_language/benchmarks"

	// sourceDirectory is the directory of a benchmark that holds its Go source.
	sourceDirectory = "go"

	// inputFileLimit is the largest source, spec or earlier report the command reads.
	inputFileLimit = 64 << 20

	// markerAssembly marks an operation the assembly loop runs in an assembly body.
	markerAssembly = "A"
)

// pathMarkers gives each dispatch path the letter a listing marks it with.
var pathMarkers = [...]string{
	isa.DispatchNone: markerAssembly,
	isa.DispatchAsm:  markerAssembly,
	isa.DispatchShim: "S",
	isa.DispatchExit: "X",
	isa.DispatchGo:   "G",
}

var (
	// errResultMismatch reports a benchmark that returned something other than what its spec
	// records as correct.
	errResultMismatch = errors.New("result mismatch")

	// errInvalidFlag reports a flag whose value the command cannot use.
	errInvalidFlag = errors.New("invalid flag value")

	// errUnexpectedArguments reports arguments the command does not take.
	errUnexpectedArguments = errors.New("unexpected arguments")

	// errFileTooLarge reports an input file past inputFileLimit.
	errFileTooLarge = errors.New("file is larger than the command reads")

	// errNoBenchmark reports a root and filter that select nothing.
	errNoBenchmark = errors.New("no benchmark matches")

	// errCensusNotBuilt reports a binary built without the census tag.
	errCensusNotBuilt = errors.New("dispatch_report needs the opcode census: rebuild with -tags pipit_opcode_census")
)

// options holds the command-line settings.
type options struct {
	// root is the directory holding one directory per benchmark.
	root string

	// filter selects benchmarks by name. Nil selects all.
	filter *regexp.Regexp

	// jsonPath names the JSON report to write, or is empty.
	jsonPath string

	// againstPath names an earlier JSON report to compare with, or is empty.
	againstPath string

	// rankDirectory names the directory the raw censuses are written to, or is empty.
	rankDirectory string

	// annotate names a function whose listing is printed with counts, or is empty.
	annotate string

	// innerRuns is the inner run count passed to each benchmark's RunInner.
	innerRuns int

	// top is how many operations and functions each section lists.
	top int
}

// countedFunction is one function's dispatch counts joined with its body.
type countedFunction struct {
	// function is the compiled function.
	function *program.CompiledFunction

	// counts holds the dispatch count of each body word.
	counts []uint64
}

// censusCapture is what one benchmark run recorded.
type censusCapture struct {
	// raw is the census report in its JSON form.
	raw []byte

	// functions holds the per-function counts.
	functions []countedFunction

	// total is the number of instructions dispatched.
	total uint64
}

// pathCounts splits a dispatch count by how the assembly loop runs each instruction.
type pathCounts struct {
	// Assembly counts instructions run in an assembly body.
	Assembly uint64 `json:"assembly"`

	// Shim counts instructions that call their Go handler from inside the loop.
	Shim uint64 `json:"shim"`

	// Exit counts instructions that leave the loop through a dedicated exit.
	Exit uint64 `json:"exit"`

	// Generic counts instructions that leave the loop through the generic exit.
	Generic uint64 `json:"generic"`
}

// operationCount is one operation's share of a benchmark's dispatches.
type operationCount struct {
	// Name is the operation's display name, with its tier.
	Name string `json:"name"`

	// Path is the dispatch marker A, S, X or G.
	Path string `json:"path"`

	// Count is the number of dispatches.
	Count uint64 `json:"count"`
}

// functionCount is one function's share of a benchmark's dispatches.
type functionCount struct {
	// Name is the function's name.
	Name string `json:"name"`

	// Count is the number of dispatches inside the function.
	Count uint64 `json:"count"`

	// Length is the number of words in the function body.
	Length int `json:"length"`
}

// programReport is the report for one benchmark.
type programReport struct {
	// Name is the benchmark's directory name.
	Name string `json:"name"`

	// Operations lists every dispatched operation, most frequent first.
	Operations []operationCount `json:"operations"`

	// Functions lists every counted function, most frequent first.
	Functions []functionCount `json:"functions"`

	// Paths splits Counted by dispatch path.
	Paths pathCounts `json:"paths"`

	// Total is the number of instructions dispatched.
	Total uint64 `json:"total"`

	// Counted is the number of dispatches inside the functions the census listed.
	Counted uint64 `json:"counted"`
}

// main measures the selected benchmarks and prints the report.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dispatch_report:", err)
		os.Exit(1)
	}
}

// run parses the arguments, measures every selected benchmark and writes the report.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes output (io.Writer) which receives the text report.
//
// Returns error when an argument is invalid or a benchmark fails.
func run(ctx context.Context, arguments []string, output io.Writer) error {
	settings, err := parseOptions(arguments, output)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if !censusBuilt {
		return errCensusNotBuilt
	}
	names, err := benchmarkNames(settings)
	if err != nil {
		return err
	}
	earlier, err := readReports(settings.againstPath)
	if err != nil {
		return err
	}
	if settings.rankDirectory != "" {
		if err = os.MkdirAll(settings.rankDirectory, outputDirectoryPermissions); err != nil {
			return fmt.Errorf("creating %s: %w", settings.rankDirectory, err)
		}
	}
	reports := make([]programReport, 0, len(names))
	for _, name := range names {
		capture, checked, measureErr := measure(ctx, settings, name)
		if measureErr != nil {
			return fmt.Errorf("%s: %w", name, measureErr)
		}
		report := buildReport(name, capture)
		reports = append(reports, report)
		writeProgram(output, settings, report, earlier[name])
		if !checked {
			fmt.Fprintln(output, "   output not checked: the benchmark records no expected digest")
		}
		writeAnnotation(output, settings, capture)
		if rankErr := writeRankInput(settings, name, capture); rankErr != nil {
			return rankErr
		}
	}
	return writeReports(settings.jsonPath, reports)
}

// parseOptions reads the command-line flags.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes output (io.Writer) which receives the usage text when it is asked for.
//
// Returns options which holds the settings.
// Returns error which is flag.ErrHelp after printing the usage, or names the flag that is
// malformed.
func parseOptions(arguments []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("dispatch_report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", defaultRoot, "directory holding one directory per benchmark")
	filter := flags.String("filter", "", "regular expression selecting benchmarks by name")
	jsonPath := flags.String("json", "", "write the report as JSON to this file")
	againstPath := flags.String("against", "", "compare with an earlier -json report")
	rankDirectory := flags.String("rank-out", "", "write each benchmark's raw census to this directory")
	annotate := flags.String("annotate", "", "print this function's listing with a count on every word")
	innerRuns := flags.Int("k", 1, "inner run count passed to RunInner")
	top := flags.Int("top", defaultListLength, "operations and functions listed per section")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.Usage()
			return options{}, err
		}
		return options{}, fmt.Errorf("parsing flags: %w", err)
	}
	settings := options{
		root:          *root,
		filter:        nil,
		jsonPath:      *jsonPath,
		againstPath:   *againstPath,
		rankDirectory: *rankDirectory,
		annotate:      *annotate,
		innerRuns:     *innerRuns,
		top:           *top,
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("%w: %s", errUnexpectedArguments, strings.Join(flags.Args(), " "))
	}
	if settings.top < 0 {
		return options{}, fmt.Errorf("%w: -top %d is negative", errInvalidFlag, settings.top)
	}
	if settings.innerRuns < 1 {
		return options{}, fmt.Errorf("%w: -k %d is less than one", errInvalidFlag, settings.innerRuns)
	}
	if *filter != "" {
		compiled, err := regexp.Compile(*filter)
		if err != nil {
			return options{}, fmt.Errorf("%w: -filter: %w", errInvalidFlag, err)
		}
		settings.filter = compiled
	}
	return settings, nil
}

// benchmarkNames lists the selected benchmark directories in name order.
//
// Takes settings (options) which holds the root and the filter.
//
// Returns []string which holds the directory names.
// Returns error when the root or a benchmark's source cannot be read, or nothing is
// selected.
func benchmarkNames(settings options) ([]string, error) {
	entries, err := os.ReadDir(settings.root)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", settings.root, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || (settings.filter != nil && !settings.filter.MatchString(entry.Name())) {
			continue
		}
		_, statErr := os.Stat(filepath.Join(settings.root, entry.Name(), sourceDirectory, sourceFileName))
		switch {
		case statErr == nil:
			names = append(names, entry.Name())
		case errors.Is(statErr, fs.ErrNotExist):
		default:
			return nil, fmt.Errorf("checking %s: %w", entry.Name(), statErr)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%w under %s", errNoBenchmark, settings.root)
	}
	return names, nil
}

// measure compiles one benchmark and runs it once under the census.
//
// The benchmark runs with its own directory as the working directory, as the harness runs
// it, because some read their input by a relative path.
//
// Takes settings (options) which holds the root and the inner run count.
// Takes name (string) which is the benchmark's directory name.
//
// Returns capture (censusCapture) which holds what the run dispatched.
// Returns checked (bool) which is false when the benchmark records no digest to check its
// output against.
// Returns err (error) when the benchmark cannot be compiled or run, or returns the wrong
// output.
func measure(ctx context.Context, settings options, name string) (capture censusCapture, checked bool, err error) {
	directory, err := filepath.Abs(filepath.Join(settings.root, name))
	if err != nil {
		return censusCapture{}, false, fmt.Errorf("resolving benchmark directory: %w", err)
	}
	source, err := readBounded(filepath.Join(directory, sourceDirectory, sourceFileName), inputFileLimit)
	if err != nil {
		return censusCapture{}, false, err
	}
	previous, err := os.Getwd()
	if err != nil {
		return censusCapture{}, false, fmt.Errorf("reading working directory: %w", err)
	}
	if err = os.Chdir(directory); err != nil {
		return censusCapture{}, false, fmt.Errorf("entering benchmark directory: %w", err)
	}
	defer func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			err = errors.Join(err, fmt.Errorf("leaving benchmark directory: %w", chdirErr))
		}
	}()
	capture, result, err := runUnderCensus(ctx, string(source), settings)
	if err != nil {
		return censusCapture{}, false, err
	}
	checked, err = checkResult(directory, result)
	return capture, checked, err
}

// readBounded reads a whole input file, refusing one past a limit.
//
// Takes path (string) which names the file.
// Takes limit (int) which is the largest size read, in bytes.
//
// Returns content ([]byte) which is the file's content.
// Returns err (error) when the file cannot be read or is too large.
func readBounded(path string, limit int) (content []byte, err error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", path, closeErr))
		}
	}()
	content, err = io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(content) > limit {
		return nil, fmt.Errorf("%s: %w", path, errFileTooLarge)
	}
	return content, nil
}

// checkResult compares the benchmark's output digest with its spec, using the harness's
// canonical form of the output.
//
// Takes directory (string) which is the benchmark's directory.
// Takes result (any) which is what the entry point returned.
//
// Returns bool which is false when the benchmark has no spec, or a spec with no digest,
// so nothing was checked.
// Returns error when the spec cannot be read or the result does not match it.
func checkResult(directory string, result any) (bool, error) {
	raw, err := readBounded(filepath.Join(directory, specFileName), inputFileLimit)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var spec struct {
		Expected string `json:"expected_stdout_sha256"`
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		return false, fmt.Errorf("decoding spec: %w", err)
	}
	if spec.Expected == "" {
		return false, nil
	}
	text, ok := result.(string)
	if !ok {
		return false, fmt.Errorf("%w: the entry point returned %T, not a string", errResultMismatch, result)
	}
	if got := harness.SHA256Hex(harness.NormaliseStdout([]byte(text))); got != spec.Expected {
		return false, fmt.Errorf("%w: digest %s, the spec expects %s", errResultMismatch, got, spec.Expected)
	}
	return true, nil
}

// runUnderCensus compiles a benchmark source and executes its entry point once, on an
// interpreter built as the benchmark harness builds its own.
//
// Takes source (string) which is the benchmark source.
// Takes settings (options) which holds the inner run count baked into the wrapper and
// says whether the raw census is wanted.
//
// Returns capture (censusCapture) which holds what the run dispatched.
// Returns result (any) which is what the entry point returned.
// Returns err (error) when compilation or execution fails, or the interpreter cannot be
// closed.
func runUnderCensus(ctx context.Context, source string, settings options) (capture censusCapture, result any, err error) {
	wrapper := fmt.Sprintf("package main\n\nfunc %s() (string, int64) {\n\treturn RunInner(%d)\n}\n", entrypointName, settings.innerRuns)
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxCallDepth(maxCallDepth))
	defer func() {
		if closeErr := interpreter.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing the interpreter: %w", closeErr))
		}
	}()
	compiled, err := interpreter.CompileFileSet(ctx, map[string]string{"main.go": source, "entrypoint.go": wrapper})
	if err != nil {
		return censusCapture{}, nil, fmt.Errorf("compiling: %w", err)
	}
	resetCensus()
	result, err = interpreter.ExecuteEntrypoint(ctx, compiled, entrypointName)
	if err != nil {
		return censusCapture{}, nil, fmt.Errorf("executing: %w", err)
	}
	capture, err = captureCensus(settings.rankDirectory != "")
	return capture, result, err
}

// buildReport joins a capture's counts with what each counted word is.
//
// Takes name (string) which is the benchmark's name.
// Takes capture (censusCapture) which holds the per-function counts.
//
// Returns programReport which holds the totals, split by path, operation and function.
func buildReport(name string, capture censusCapture) programReport {
	report := programReport{
		Name:       name,
		Operations: nil,
		Functions:  make([]functionCount, 0, len(capture.functions)),
		Paths:      pathCounts{Assembly: 0, Shim: 0, Exit: 0, Generic: 0},
		Total:      capture.total,
		Counted:    0,
	}
	operations := map[string]*operationCount{}
	for _, counted := range capture.functions {
		inFunction := uint64(0)
		for programCounter, count := range counted.counts {
			if count == 0 || programCounter >= len(counted.function.Body) {
				continue
			}
			inFunction += count
			tally(&report.Paths, operations, counted.function.Body[programCounter], count)
		}
		report.Counted += inFunction
		report.Functions = append(report.Functions,
			functionCount{Name: counted.function.Name, Count: inFunction, Length: len(counted.function.Body)})
	}
	for _, operation := range operations {
		report.Operations = append(report.Operations, *operation)
	}
	slices.SortFunc(report.Operations, func(left, right operationCount) int {
		return compareCounts(left.Count, right.Count, left.Name, right.Name)
	})
	slices.SortFunc(report.Functions, func(left, right functionCount) int {
		return compareCounts(left.Count, right.Count, left.Name, right.Name)
	})
	return report
}

// tally adds one word's dispatches to the path split and to its operation.
//
// Takes paths (*pathCounts) which receives the count under the word's dispatch path.
// Takes operations (map[string]*operationCount) which receives it under the word's name.
// Takes word (isa.Instruction) which is the dispatched word.
// Takes count (uint64) which is how often it was dispatched.
func tally(paths *pathCounts, operations map[string]*operationCount, word isa.Instruction, count uint64) {
	path := isa.DispatchForInstruction(word)
	switch path {
	case isa.DispatchShim:
		paths.Shim += count
	case isa.DispatchExit:
		paths.Exit += count
	case isa.DispatchGo:
		paths.Generic += count
	case isa.DispatchAsm, isa.DispatchNone:
		paths.Assembly += count
	}
	name := isa.InstructionDisplayName(word)
	operation, seen := operations[name]
	if !seen {
		operation = &operationCount{Name: name, Path: pathMarkers[path], Count: 0}
		operations[name] = operation
	}
	operation.Count += count
}

// compareCounts orders larger counts first and breaks ties by name.
//
// Takes leftCount (uint64) which is the left entry's count.
// Takes rightCount (uint64) which is the right entry's count.
// Takes leftName (string) which is the left entry's name.
// Takes rightName (string) which is the right entry's name.
//
// Returns int which is negative when the left entry sorts first.
func compareCounts(leftCount, rightCount uint64, leftName, rightName string) int {
	return cmp.Or(cmp.Compare(rightCount, leftCount), cmp.Compare(leftName, rightName))
}

// share returns part as a percentage of whole.
//
// Takes part (uint64) which is the numerator.
// Takes whole (uint64) which is the denominator.
//
// Returns float64 which is zero when whole is zero.
func share(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return percent * float64(part) / float64(whole)
}

// writeProgram prints one benchmark's section of the text report.
//
// Takes output (io.Writer) which receives the text.
// Takes settings (options) which holds the list length.
// Takes report (programReport) which is the benchmark's report.
// Takes earlier (*programReport) which is the same benchmark in an earlier report, or
// nil.
func writeProgram(output io.Writer, settings options, report programReport, earlier *programReport) {
	fmt.Fprintf(output, "\n## %s: %d dispatches", report.Name, report.Total)
	if earlier != nil && earlier.Total != 0 {
		fmt.Fprintf(output, " (was %d, %+.2f%%)", earlier.Total, share(report.Total, earlier.Total)-percent)
	}
	fmt.Fprintf(output, "\n   assembly %.2f%%  shim %.2f%%  exit %.2f%%  generic exit %.2f%%\n",
		share(report.Paths.Assembly, report.Counted), share(report.Paths.Shim, report.Counted),
		share(report.Paths.Exit, report.Counted), share(report.Paths.Generic, report.Counted))
	if earlier != nil {
		fmt.Fprintf(output, "   was      %.2f%%  shim %.2f%%  exit %.2f%%  generic exit %.2f%%\n",
			share(earlier.Paths.Assembly, earlier.Counted), share(earlier.Paths.Shim, earlier.Counted),
			share(earlier.Paths.Exit, earlier.Counted), share(earlier.Paths.Generic, earlier.Counted))
	}
	fmt.Fprintln(output, "   not in assembly:")
	listed := 0
	for _, operation := range report.Operations {
		if operation.Path == markerAssembly || listed == settings.top {
			continue
		}
		listed++
		fmt.Fprintf(output, "     %s %-46s %12d  %6.2f%%\n", operation.Path, operation.Name, operation.Count, share(operation.Count, report.Counted))
	}
	fmt.Fprintln(output, "   functions:")
	for _, function := range report.Functions[:min(len(report.Functions), settings.top)] {
		fmt.Fprintf(output, "     %6.2f%%  %4d words  %s\n", share(function.Count, report.Counted), function.Length, function.Name)
	}
}

// writeAnnotation prints the listing of the function named by -annotate with each word's
// share of the benchmark's dispatches, for every counted function of that name.
//
// Takes output (io.Writer) which receives the listing.
// Takes settings (options) which names the function.
// Takes capture (censusCapture) which holds the counted functions.
func writeAnnotation(output io.Writer, settings options, capture censusCapture) {
	if settings.annotate == "" {
		return
	}
	found := false
	for _, counted := range capture.functions {
		if counted.function.Name != settings.annotate {
			continue
		}
		found = true
		for _, line := range ownListing(debug.DisassembleFunctionAssembly(counted.function)) {
			programCounter, isWord := listingProgramCounter(line)
			if !isWord || programCounter >= len(counted.counts) {
				fmt.Fprintf(output, "         %s\n", line)
				continue
			}
			fmt.Fprintf(output, "%7.3f%% %s\n", share(counted.counts[programCounter], capture.total), line)
		}
	}
	if !found {
		fmt.Fprintf(output, "   -annotate: no counted function is named %s\n", settings.annotate)
	}
}

// ownListing returns the lines of a function's own section of a listing, leaving out the
// sections of its nested functions.
//
// Takes listing (string) which is the function's listing followed by its children's.
//
// Returns []string which holds the lines up to the second function header.
func ownListing(listing string) []string {
	var lines []string
	headers := 0
	for line := range strings.SplitSeq(strings.TrimRight(listing, "\n"), "\n") {
		if strings.HasPrefix(line, functionHeaderPrefix) {
			headers++
		}
		if headers > 1 {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

// listingProgramCounter reads the program counter that starts an instruction line,
// however many digits it has.
//
// Takes line (string) which is one line of a listing.
//
// Returns int which is the program counter.
// Returns bool which is false for a line that is not an instruction.
func listingProgramCounter(line string) (int, bool) {
	field, _, found := strings.Cut(line, " ")
	if !found || field == "" || strings.TrimLeft(field, "0123456789") != "" {
		return 0, false
	}
	programCounter, err := strconv.Atoi(field)
	return programCounter, err == nil
}

// writeRankInput writes one benchmark's raw census into the -rank-out directory.
//
// Takes settings (options) which names the directory.
// Takes name (string) which is the benchmark's name.
// Takes capture (censusCapture) which holds the raw census.
//
// Returns error when the file cannot be written.
func writeRankInput(settings options, name string, capture censusCapture) error {
	if settings.rankDirectory == "" {
		return nil
	}
	path := filepath.Join(settings.rankDirectory, name+".json")
	if err := os.WriteFile(path, capture.raw, outputFilePermissions); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// writeReports writes every benchmark's report as one JSON document.
//
// Takes path (string) which names the file, or is empty to skip writing.
// Takes reports ([]programReport) which are the reports.
//
// Returns error when the file cannot be written.
func writeReports(path string, reports []programReport) error {
	if path == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(reports, "", " ")
	if err != nil {
		return fmt.Errorf("encoding the report: %w", err)
	}
	if err = os.WriteFile(path, encoded, outputFilePermissions); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// readReports reads an earlier JSON report, keyed by benchmark name.
//
// Takes path (string) which names the file, or is empty for no earlier report.
//
// Returns map[string]*programReport which is empty when path is empty.
// Returns error when the file cannot be read or decoded.
func readReports(path string) (map[string]*programReport, error) {
	byName := map[string]*programReport{}
	if path == "" {
		return byName, nil
	}
	encoded, err := readBounded(path, inputFileLimit)
	if err != nil {
		return nil, err
	}
	var reports []programReport
	if err = json.Unmarshal(encoded, &reports); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	for index := range reports {
		byName[reports[index].Name] = &reports[index]
	}
	return byName, nil
}
