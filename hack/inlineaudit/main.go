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

// Command inlineaudit reports which ranked js/wasm operations run without a Go call.
//
// Run from the repository root.
//
//	go run ./hack/inlineaudit
//	go run ./hack/inlineaudit -update
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"pipit.sh/pipit/internal/isa"
)

const (
	// engineDirectory is the engine package, relative to the repository root.
	engineDirectory = "internal/engine"

	// hotOperationsFile lists the ranked operations and their shares.
	hotOperationsFile = "dispatch_hot_operations.txt"

	// wrappersFile holds the generated handlers that forward to a body function.
	wrappersFile = "vm_handler_flat_wrappers_generated.go"

	// fastPathsFile holds the hand-written fast paths the loop tries before a handler.
	fastPathsFile = "vm_dispatch_fast_paths.go"

	// fastPathPrefix starts every fast path's name.
	fastPathPrefix = "fast"

	// spanFile holds the js/wasm dispatch loop.
	spanFile = "vm_dispatch_span_ranked_generated.go"

	// recordedFile lists the ranked operations the loop is expected to inline.
	recordedFile = "dispatch_inlined_operations.txt"

	// recordedHeader starts the recorded file.
	recordedHeader = "# The ranked operations the js/wasm dispatch loop inlines, in rank order.\n" +
		"#\n" +
		"# Regenerate with\n" +
		"#\n" +
		"#\tgo run ./hack/inlineaudit -update\n" +
		"#\n" +
		"# The audit fails when an operation listed here is no longer inlined.\n\n"

	// bodySuffix is what a wrapper's body function adds to the wrapper's name.
	bodySuffix = "Body"

	// handlerPrefix starts every handler's name.
	handlerPrefix = "handle"

	// compilerLineLimit is the longest line of compiler output the audit accepts.
	compilerLineLimit = 1 << 20

	// keptCompilerLines is how many lines of compiler output that are not inlining decisions
	// are kept to show when the build fails.
	keptCompilerLines = 50

	// inputFileLimit is the largest input file the audit reads.
	inputFileLimit = 16 << 20

	// buildTimeout bounds the js/wasm build of the engine.
	buildTimeout = 10 * time.Minute

	// stoppedBuildWaitDelay bounds the wait for a stopped build to give up its output.
	stoppedBuildWaitDelay = 5 * time.Second

	// recordedFilePermissions are the permissions of the recorded file.
	recordedFilePermissions = 0o644

	// verdictInlined marks an operation the loop reaches without a Go call.
	verdictInlined = "inlined"

	// verdictCall marks an operation the loop calls.
	verdictCall = "CALL"

	// percent turns a share into a fraction.
	percent = 100
)

// constantPrefixes lists operation prefixes from longest to shortest.
var constantPrefixes = [...]string{"SubOpTier3", "SubOpTier2", "SubOp", "Op"}

var (
	// sharePattern matches the share recorded in the note after an operation.
	sharePattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)%`)

	// wrapperPattern matches a generated wrapper's declaration.
	wrapperPattern = regexp.MustCompile(`(?m)^func (handle\w+)\(`)

	// fastPathPattern matches a fast path's declaration and captures its handler's stem.
	fastPathPattern = regexp.MustCompile(`(?m)^func fast([A-Z]\w*)\(`)

	// canInlinePattern matches the compiler reporting that a function can be inlined.
	canInlinePattern = regexp.MustCompile(`: can inline (\w+)$`)

	// inlinedCallPattern matches the compiler reporting a call it inlined.
	inlinedCallPattern = regexp.MustCompile(`^(\S+?):\d+:\d+: inlining call to (\w+)$`)

	// errDrift reports recorded operations that are no longer inlined.
	errDrift = errors.New("ranked operations stopped being inlined into the js/wasm dispatch loop")

	// errNoHandler reports a ranked operation the instruction set has no handler for.
	errNoHandler = errors.New("ranked operation has no handler")

	// errListedTwice reports an operation the ranked list holds twice.
	errListedTwice = errors.New("operation is ranked twice")

	// errNoOperations reports a ranked list that holds no operation.
	errNoOperations = errors.New("the ranked list holds no operation")

	// errFileTooLarge reports an input file past inputFileLimit.
	errFileTooLarge = errors.New("file is larger than the audit reads")

	// errUnexpectedArguments reports arguments the command does not take.
	errUnexpectedArguments = errors.New("unexpected arguments")

	// errNothingInlined reports a build whose output named no inlined call in the loop.
	errNothingInlined = errors.New("the compiler reported no inlining into the dispatch loop")

	// errBuildTimedOut is the cause given to a build that outlives buildTimeout.
	errBuildTimedOut = errors.New("the js/wasm build of the engine timed out")
)

// rankedOperation is one line of the ranked list.
type rankedOperation struct {
	// constant is the operation's isa constant.
	constant string

	// share is the operation's recorded share of dispatches, in percent.
	share float64
}

// inliningFacts is what the compiler reported about the engine package.
type inliningFacts struct {
	// inlinable holds every function the compiler can inline.
	inlinable map[string]bool

	// inlinedIntoSpan holds every function inlined at a call in the dispatch loop's file.
	inlinedIntoSpan map[string]bool
}

// auditInputs is what the audit reads from the engine's sources.
type auditInputs struct {
	// wrappers names the handlers that forward to a body function.
	wrappers map[string]bool

	// fastPaths names the handlers that have a fast path.
	fastPaths map[string]bool

	// operations holds the ranked operations in rank order.
	operations []rankedOperation
}

// auditResult is the verdict on each ranked operation.
type auditResult struct {
	// ranked holds every ranked operation.
	ranked map[string]bool

	// inlined holds the operations the loop reaches without a Go call, in rank order.
	inlined []string

	// rankedShare is the share of dispatches the ranked list covers, in percent.
	rankedShare float64

	// inlinedShare is the share of dispatches the inlined operations cover, in percent.
	inlinedShare float64
}

// record adds one line of compiler output if it reports an inlining decision.
//
// Takes line (string) which is one line of compiler output.
//
// Returns bool which is true when the line was an inlining decision.
func (facts *inliningFacts) record(line string) bool {
	if match := canInlinePattern.FindStringSubmatch(line); match != nil {
		facts.inlinable[match[1]] = true
		return true
	}
	match := inlinedCallPattern.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	if filepath.Base(match[1]) == spanFile {
		facts.inlinedIntoSpan[match[2]] = true
	}
	return true
}

// main compares or updates the recorded js/wasm inlining decisions.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "inlineaudit:", err)
		os.Exit(1)
	}
}

// run audits the dispatch loop and compares or records the inlined set.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes output (io.Writer) which receives the report.
//
// Returns error when an argument is wrong, the engine cannot be built, a file cannot be
// read, or a recorded operation is no longer inlined.
func run(ctx context.Context, arguments []string, output io.Writer) error {
	update, err := parseArguments(arguments, output)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	inputs, err := readInputs(engineDirectory)
	if err != nil {
		return err
	}
	buildContext, cancel := context.WithTimeoutCause(ctx, buildTimeout, errBuildTimedOut)
	defer cancel()
	facts, err := compileEngine(buildContext)
	if err != nil {
		return err
	}
	result, err := audit(inputs, facts, output)
	if err != nil {
		return err
	}
	recordedPath := filepath.Join(engineDirectory, recordedFile)
	if update {
		return writeRecorded(recordedPath, result.inlined)
	}
	return compareWithRecorded(recordedPath, result, output)
}

// parseArguments reads the command-line arguments.
//
// Takes arguments ([]string) which are the command-line arguments.
// Takes output (io.Writer) which receives the usage text when it is asked for.
//
// Returns bool which is true when the inlined set is to be recorded, not compared.
// Returns error which is flag.ErrHelp after printing the usage, or names what is wrong.
func parseArguments(arguments []string, output io.Writer) (bool, error) {
	flags := flag.NewFlagSet("inlineaudit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	update := flags.Bool("update", false, "record the current inlined set instead of comparing with it")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.Usage()
			return false, err
		}
		return false, fmt.Errorf("parsing arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return false, fmt.Errorf("%w: %s", errUnexpectedArguments, strings.Join(flags.Args(), " "))
	}
	return *update, nil
}

// readInputs reads the ranked list and the handlers declared around the loop.
//
// Takes directory (string) which holds the engine package.
//
// Returns auditInputs which holds what was read.
// Returns error when a file cannot be read or the ranked list is malformed.
func readInputs(directory string) (auditInputs, error) {
	var inputs auditInputs
	content, err := readBounded(filepath.Join(directory, hotOperationsFile), inputFileLimit)
	if err != nil {
		return inputs, err
	}
	if inputs.operations, err = rankedOperations(content); err != nil {
		return inputs, fmt.Errorf("%s: %w", hotOperationsFile, err)
	}
	if inputs.wrappers, err = readDeclared(filepath.Join(directory, wrappersFile), wrapperPattern, ""); err != nil {
		return inputs, err
	}
	inputs.fastPaths, err = readDeclared(filepath.Join(directory, fastPathsFile), fastPathPattern, handlerPrefix)
	return inputs, err
}

// readBounded reads a whole input file, refusing one past a limit.
//
// Takes path (string) which names the file.
// Takes limit (int) which is the largest size read, in bytes.
//
// Returns string which is the file's content.
// Returns error when the file cannot be read or is too large.
func readBounded(path string, limit int) (content string, err error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing %s: %w", path, closeErr))
		}
	}()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	if len(data) > limit {
		return "", fmt.Errorf("%s: %w", path, errFileTooLarge)
	}
	return string(data), nil
}

// rankedOperations parses the ranked list.
//
// Takes content (string) which is the list.
//
// Returns []rankedOperation which holds the operations in rank order.
// Returns error when an operation is listed twice or none is listed.
func rankedOperations(content string) ([]rankedOperation, error) {
	var operations []rankedOperation
	seen := map[string]bool{}
	for number, line := range strings.Split(content, "\n") {
		constant, note, _ := strings.Cut(line, "#")
		constant = strings.TrimSpace(constant)
		if constant == "" {
			continue
		}
		if seen[constant] {
			return nil, fmt.Errorf("line %d: %w: %s", number+1, errListedTwice, constant)
		}
		seen[constant] = true
		share := 0.0
		if match := sharePattern.FindStringSubmatch(note); match != nil {
			parsed, err := strconv.ParseFloat(match[1], 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: reading the share of %s: %w", number+1, constant, err)
			}
			share = parsed
		}
		operations = append(operations, rankedOperation{constant: constant, share: share})
	}
	if len(operations) == 0 {
		return nil, errNoOperations
	}
	return operations, nil
}

// readDeclared reads the functions a file declares that match a pattern.
//
// Takes path (string) which names the file.
// Takes pattern (*regexp.Regexp) which captures part of each wanted function's name.
// Takes prefix (string) which is prepended to each captured name.
//
// Returns map[string]bool which is keyed by prefix and captured name.
// Returns error when the file cannot be read.
func readDeclared(path string, pattern *regexp.Regexp, prefix string) (map[string]bool, error) {
	content, err := readBounded(path, inputFileLimit)
	if err != nil {
		return nil, err
	}
	declared := map[string]bool{}
	for _, match := range pattern.FindAllStringSubmatch(content, -1) {
		declared[prefix+match[1]] = true
	}
	return declared, nil
}

// audit gives each ranked operation its verdict and prints the report.
//
// Takes inputs (auditInputs) which holds the ranked list and the declared handlers.
// Takes facts (inliningFacts) which holds the compiler's decisions.
// Takes output (io.Writer) which receives the report.
//
// Returns auditResult which holds the verdicts.
// Returns error when a ranked operation has no handler in the instruction set, which a
// verdict of "CALL" would hide.
func audit(inputs auditInputs, facts inliningFacts, output io.Writer) (auditResult, error) {
	result := auditResult{ranked: make(map[string]bool, len(inputs.operations)), inlined: nil, rankedShare: 0, inlinedShare: 0}
	handlers := specHandlers()
	for _, operation := range inputs.operations {
		handler := handlerOf(operation.constant)
		if !handlers[handler] {
			return result, fmt.Errorf("%w: %s (looked for %s)", errNoHandler, operation.constant, handler)
		}
		result.ranked[operation.constant] = true
		result.rankedShare += operation.share
		verdict := verdictCall
		if runsFastPathInline(facts, inputs.fastPaths, handler) || reachesHandlerWithoutCall(facts, inputs.wrappers, handler) {
			verdict = verdictInlined
			result.inlined = append(result.inlined, operation.constant)
			result.inlinedShare += operation.share
		}
		fmt.Fprintf(output, "%6.2f%%  %-8s %s\n", operation.share, verdict, operation.constant)
	}
	fmt.Fprintf(output, "\n%d of %d ranked operations inlined: %.1f%% of dispatches out of the %.1f%% the ranked list covers\n",
		len(result.inlined), len(inputs.operations), result.inlinedShare, result.rankedShare)
	fmt.Fprintf(output, "Go calls from the loop per ranked dispatch: at least %.3f\n",
		(result.rankedShare-result.inlinedShare)/cmp.Or(result.rankedShare, percent))
	return result, nil
}

// specHandlers lists the handlers the instruction set names.
//
// Returns map[string]bool which holds each handler's name.
func specHandlers() map[string]bool {
	handlers := map[string]bool{}
	specs := isa.AllSpecs()
	for index := range specs {
		if handler := specs[index].Handler; handler != "" {
			handlers[handler] = true
		}
	}
	return handlers
}

// handlerOf returns the name of the handler for an operation constant.
//
// Takes constant (string) which is the isa constant, for example SubOpMoveInt64.
//
// Returns string which is the handler's name, for example handleMoveInt64.
func handlerOf(constant string) string {
	for _, prefix := range constantPrefixes {
		if stem, found := strings.CutPrefix(constant, prefix); found {
			return handlerPrefix + stem
		}
	}
	return handlerPrefix + constant
}

// runsFastPathInline reports whether the loop inlines an operation's fast path.
//
// Takes facts (inliningFacts) which holds the compiler's decisions.
// Takes fastPaths (map[string]bool) which names the handlers that have a fast path.
// Takes handler (string) which is the handler's name.
//
// Returns bool which is true when the fast path is inlined into the loop.
func runsFastPathInline(facts inliningFacts, fastPaths map[string]bool, handler string) bool {
	return fastPaths[handler] && facts.inlinedIntoSpan[fastPathPrefix+strings.TrimPrefix(handler, handlerPrefix)]
}

// reachesHandlerWithoutCall reports whether the loop inlines a handler and its wrapper
// body.
//
// Takes facts (inliningFacts) which holds the compiler's decisions.
// Takes wrappers (map[string]bool) which names the handlers that forward to a body.
// Takes handler (string) which is the handler's name.
//
// Returns bool which is true when no call remains on the way to the handler's code.
func reachesHandlerWithoutCall(facts inliningFacts, wrappers map[string]bool, handler string) bool {
	if !facts.inlinedIntoSpan[handler] {
		return false
	}
	return !wrappers[handler] || facts.inlinable[handler+bodySuffix]
}

// compileEngine builds the engine for js/wasm and collects the compiler's inlining
// decisions for that package.
//
// Takes ctx (context.Context) which stops the build when it is cancelled or times out.
//
// Returns inliningFacts which holds the decisions.
// Returns error when the build cannot be started, its output cannot be read, or it fails.
func compileEngine(ctx context.Context) (inliningFacts, error) {
	command := exec.CommandContext(ctx, "go", "build", "-gcflags=pipit.sh/pipit/internal/engine=-m", "./"+engineDirectory+"/")
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	stopWithItsChildren(command)
	diagnostics, err := command.StderrPipe()
	if err != nil {
		return inliningFacts{}, fmt.Errorf("attaching to the compiler: %w", err)
	}
	if err = command.Start(); err != nil {
		return inliningFacts{}, fmt.Errorf("starting the compiler: %w", err)
	}
	facts, others, readErr := readDecisions(diagnostics)
	if readErr != nil {
		killErr := command.Cancel()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		return facts, errors.Join(fmt.Errorf("reading the compiler's output: %w", readErr), killErr, command.Wait())
	}
	if err = command.Wait(); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return facts, fmt.Errorf("building the engine for js/wasm: %w", errors.Join(err, cause))
		}
		if len(others) == 0 {
			return facts, fmt.Errorf("building the engine for js/wasm: %w", err)
		}
		return facts, fmt.Errorf("building the engine for js/wasm: %w\n%s", err, strings.Join(others, "\n"))
	}
	if len(facts.inlinedIntoSpan) == 0 {
		return facts, fmt.Errorf("%w (%s)", errNothingInlined, spanFile)
	}
	return facts, nil
}

// readDecisions reads the compiler's output to its end.
//
// Takes diagnostics (io.Reader) which is the compiler's output.
//
// Returns facts (inliningFacts) which holds the inlining decisions.
// Returns others ([]string) which holds the last keptCompilerLines lines that were not
// inlining decisions, where a failed build's errors are.
// Returns err (error) when the output cannot be read or holds a line past
// compilerLineLimit.
func readDecisions(diagnostics io.Reader) (facts inliningFacts, others []string, err error) {
	facts = inliningFacts{inlinable: map[string]bool{}, inlinedIntoSpan: map[string]bool{}}
	scanner := bufio.NewScanner(diagnostics)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), compilerLineLimit)
	for scanner.Scan() {
		line := scanner.Text()
		if facts.record(line) {
			continue
		}
		if len(others) == keptCompilerLines {
			others = slices.Delete(others, 0, 1)
		}
		others = append(others, line)
	}
	return facts, others, scanner.Err()
}

// writeRecorded records the inlined set.
//
// Takes path (string) which names the recorded file.
// Takes inlined ([]string) which holds the inlined operations in rank order.
//
// Returns error when the file cannot be written.
func writeRecorded(path string, inlined []string) error {
	content := recordedHeader + strings.Join(inlined, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), recordedFilePermissions); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// compareWithRecorded reports the recorded operations that are no longer inlined.
//
// Takes path (string) which names the recorded file.
// Takes result (auditResult) which holds the operations ranked and inlined now.
// Takes output (io.Writer) which receives the operations inlined now but not recorded,
// and the recorded ones that are no longer ranked.
//
// Returns error when the recorded file cannot be read or a recorded operation that is
// still ranked is no longer inlined.
func compareWithRecorded(path string, result auditResult, output io.Writer) error {
	content, err := readBounded(path, inputFileLimit)
	if err != nil {
		return fmt.Errorf("%w (record it with -update)", err)
	}
	inlined := make(map[string]bool, len(result.inlined))
	for _, constant := range result.inlined {
		inlined[constant] = true
	}
	var lost []string
	recorded := map[string]bool{}
	for line := range strings.SplitSeq(content, "\n") {
		constant := strings.TrimSpace(line)
		if constant == "" || strings.HasPrefix(constant, "#") {
			continue
		}
		recorded[constant] = true
		switch {
		case inlined[constant]:
		case !result.ranked[constant]:
			fmt.Fprintf(output, "recorded but no longer ranked, rerun with -update: %s\n", constant)
		default:
			lost = append(lost, constant)
		}
	}
	for _, constant := range result.inlined {
		if !recorded[constant] {
			fmt.Fprintf(output, "newly inlined, not yet recorded: %s\n", constant)
		}
	}
	if len(lost) != 0 {
		return fmt.Errorf("%w: %s", errDrift, strings.Join(lost, ", "))
	}
	return nil
}
