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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func factsOf(inlinable, inlinedIntoSpan []string) inliningFacts {
	facts := inliningFacts{inlinable: map[string]bool{}, inlinedIntoSpan: map[string]bool{}}
	for _, name := range inlinable {
		facts.inlinable[name] = true
	}
	for _, name := range inlinedIntoSpan {
		facts.inlinedIntoSpan[name] = true
	}
	return facts
}

func TestHandlerOfStripsTheLongestTierPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		constant string
		want     string
	}{
		{constant: "OpAddInt64", want: "handleAddInt64"},
		{constant: "SubOpMoveInt64", want: "handleMoveInt64"},
		{constant: "SubOpTier2Return", want: "handleReturn"},
		{constant: "SubOpTier3ReturnVoid", want: "handleReturnVoid"},
		{constant: "Unprefixed", want: "handleUnprefixed"},
	}
	for _, tt := range tests {
		t.Run(tt.constant, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, handlerOf(tt.constant))
		})
	}
}

func TestRankedOperationsReadsTheListAsItsGeneratorDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		content string
		want    []rankedOperation
	}{
		{
			name:    "operations with and without a share",
			content: "# header\n\nOpAddInt64  # 12.50% of dispatches\nSubOpMoveInt64\n  SubOpJump # 3%\n",
			want: []rankedOperation{
				{constant: "OpAddInt64", share: 12.5}, {constant: "SubOpMoveInt64", share: 0}, {constant: "SubOpJump", share: 3},
			},
		},
		{name: "a note with no share", content: "OpAddInt64 # hot in loops\n", want: []rankedOperation{{constant: "OpAddInt64", share: 0}}},
		{name: "an operation listed twice", content: "OpAddInt64\nOpAddInt64 # 1%\n", wantErr: errListedTwice},
		{name: "only comments", content: "# nothing\n\n", wantErr: errNoOperations},
		{name: "an empty list", content: "", wantErr: errNoOperations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := rankedOperations(tt.content)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRecordKeepsOnlyDecisionsAboutWholeFunctionsOfTheEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		line            string
		wantInlinable   []string
		wantInlinedSpan []string
		wantDecision    bool
	}{
		{name: "a function that can be inlined", line: "./vm_handler_ops.go:10:6: can inline handleAddInt64", wantDecision: true, wantInlinable: []string{"handleAddInt64"}},
		{name: "a closure of a function is not the function", line: "./vm_handler_ops.go:12:9: can inline handleAddInt64.func1"},
		{
			name: "a call inlined into the loop", line: "./" + spanFile + ":40:21: inlining call to handleMoveInt64",
			wantDecision: true, wantInlinedSpan: []string{"handleMoveInt64"},
		},
		{name: "a call inlined somewhere else", line: "./vm.go:40:21: inlining call to handleMoveInt64", wantDecision: true},
		{name: "a closure inlined into the loop", line: "./" + spanFile + ":40:21: inlining call to handleMoveInt64.func1"},
		{name: "a function of another package inlined into the loop", line: "./" + spanFile + ":40:21: inlining call to isa.JoinWide"},
		{name: "an escape analysis line", line: "./vm.go:9:2: moved to heap: frame"},
		{name: "a compile error", line: "./vm.go:9:2: undefined: frame"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			facts := factsOf(nil, nil)

			decision := facts.record(tt.line)

			require.Equal(t, tt.wantDecision, decision)
			require.Equal(t, factsOf(tt.wantInlinable, tt.wantInlinedSpan), facts)
		})
	}
}

func TestReadDecisionsKeepsTheLastLinesThatAreNotDecisions(t *testing.T) {
	t.Parallel()

	var output strings.Builder
	output.WriteString("./a.go:1:6: can inline handleAddInt64\n")
	for range keptCompilerLines + 5 {
		output.WriteString("./a.go:2:2: moved to heap: x\n")
	}
	output.WriteString("./a.go:3:1: undefined: y\n")

	facts, others, err := readDecisions(strings.NewReader(output.String()))

	require.NoError(t, err)
	require.True(t, facts.inlinable["handleAddInt64"])
	require.Len(t, others, keptCompilerLines, "the lines kept for a failed build are bounded")
	require.Equal(t, "./a.go:3:1: undefined: y", others[len(others)-1], "the compiler's last words are the ones kept")
}

func TestReadDecisionsReportsALineItCannotHold(t *testing.T) {
	t.Parallel()

	_, _, err := readDecisions(strings.NewReader(strings.Repeat("x", compilerLineLimit+1)))

	require.ErrorIs(t, err, bufio.ErrTooLong)
}

func TestAnOperationIsInlinedThroughItsFastPathOrItsHandler(t *testing.T) {
	t.Parallel()

	wrappers := map[string]bool{"handleWrapped": true}
	fastPaths := map[string]bool{"handleQuick": true}
	tests := []struct {
		name    string
		handler string
		facts   inliningFacts
		want    bool
	}{
		{name: "a fast path inlined into the loop", handler: "handleQuick", facts: factsOf(nil, []string{"fastQuick"}), want: true},
		{name: "a fast path the loop calls", handler: "handleQuick", facts: factsOf(nil, nil)},
		{name: "a fast path of another handler", handler: "handlePlain", facts: factsOf(nil, []string{"fastPlain"})},
		{name: "a handler inlined into the loop", handler: "handlePlain", facts: factsOf(nil, []string{"handlePlain"}), want: true},
		{name: "a wrapper whose body is called", handler: "handleWrapped", facts: factsOf(nil, []string{"handleWrapped"})},
		{
			name: "a wrapper whose body is inlined too", handler: "handleWrapped",
			facts: factsOf([]string{"handleWrappedBody"}, []string{"handleWrapped"}), want: true,
		},
		{name: "a handler the loop calls", handler: "handlePlain", facts: factsOf([]string{"handlePlain"}, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := runsFastPathInline(tt.facts, fastPaths, tt.handler) || reachesHandlerWithoutCall(tt.facts, wrappers, tt.handler)

			require.Equal(t, tt.want, got)
		})
	}
}

func TestAuditGivesEachRankedOperationAVerdict(t *testing.T) {
	t.Parallel()

	inputs := auditInputs{
		wrappers:  map[string]bool{},
		fastPaths: map[string]bool{},
		operations: []rankedOperation{
			{constant: "OpAddInt64", share: 60}, {constant: "OpSubtractInt64", share: 30},
		},
	}
	var output bytes.Buffer

	result, err := audit(inputs, factsOf(nil, []string{"handleAddInt64"}), &output)

	require.NoError(t, err)
	require.Equal(t, []string{"OpAddInt64"}, result.inlined)
	require.InDelta(t, 90.0, result.rankedShare, 1e-9)
	require.InDelta(t, 60.0, result.inlinedShare, 1e-9)
	require.Contains(t, output.String(), "inlined  OpAddInt64")
	require.Contains(t, output.String(), "CALL     OpSubtractInt64")
	require.Contains(t, output.String(), "1 of 2 ranked operations inlined")
	require.Contains(t, output.String(), "at least 0.333")
}

func TestAuditRefusesARankedOperationTheInstructionSetHasNoHandlerFor(t *testing.T) {
	t.Parallel()

	inputs := auditInputs{operations: []rankedOperation{{constant: "OpNoSuchOperation", share: 1}}}

	_, err := audit(inputs, factsOf(nil, nil), &bytes.Buffer{})

	require.ErrorIs(t, err, errNoHandler, "a verdict of CALL would hide a name the audit cannot follow")
}

func TestCompareWithRecordedTellsLostInliningFromAListThatMoved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr    error
		name       string
		recorded   string
		wantOutput string
		result     auditResult
	}{
		{
			name:     "every recorded operation still inlined",
			recorded: "# header\nOpAddInt64\nOpSubtractInt64\n",
			result:   auditResult{ranked: map[string]bool{"OpAddInt64": true, "OpSubtractInt64": true}, inlined: []string{"OpAddInt64", "OpSubtractInt64"}},
		},
		{
			name:     "a recorded operation the loop now calls",
			recorded: "OpAddInt64\nOpSubtractInt64\n",
			result:   auditResult{ranked: map[string]bool{"OpAddInt64": true, "OpSubtractInt64": true}, inlined: []string{"OpAddInt64"}},
			wantErr:  errDrift,
		},
		{
			name:       "a recorded operation that left the ranked list",
			recorded:   "OpAddInt64\nOpSubtractInt64\n",
			result:     auditResult{ranked: map[string]bool{"OpAddInt64": true}, inlined: []string{"OpAddInt64"}},
			wantOutput: "recorded but no longer ranked, rerun with -update: OpSubtractInt64",
		},
		{
			name:       "an operation inlined but not yet recorded",
			recorded:   "OpAddInt64\n",
			result:     auditResult{ranked: map[string]bool{"OpAddInt64": true, "OpSubtractInt64": true}, inlined: []string{"OpAddInt64", "OpSubtractInt64"}},
			wantOutput: "newly inlined, not yet recorded: OpSubtractInt64",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), recordedFile)
			require.NoError(t, os.WriteFile(path, []byte(tt.recorded), recordedFilePermissions))
			var output bytes.Buffer

			err := compareWithRecorded(path, tt.result, &output)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Contains(t, output.String(), tt.wantOutput)
		})
	}
}

func TestCompareWithRecordedNeedsARecordedFile(t *testing.T) {
	t.Parallel()

	err := compareWithRecorded(filepath.Join(t.TempDir(), "missing"), auditResult{}, &bytes.Buffer{})

	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, "-update")
}

func TestWriteRecordedWritesWhatCompareWithRecordedReads(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), recordedFile)
	result := auditResult{ranked: map[string]bool{"OpAddInt64": true, "SubOpJump": true}, inlined: []string{"OpAddInt64", "SubOpJump"}}

	require.NoError(t, writeRecorded(path, result.inlined))

	var output bytes.Buffer
	require.NoError(t, compareWithRecorded(path, result, &output))
	require.Empty(t, output.String(), "a set just recorded has nothing new and nothing lost")
}

func TestReadBoundedRefusesAFilePastItsLimit(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	small, large := filepath.Join(directory, "small"), filepath.Join(directory, "large")
	require.NoError(t, os.WriteFile(small, []byte("content"), recordedFilePermissions))
	require.NoError(t, os.WriteFile(large, []byte("contents"), recordedFilePermissions))
	const limit = len("content")

	content, err := readBounded(small, limit)
	require.NoError(t, err)
	require.Equal(t, "content", content, "a file of exactly the limit is read whole")

	_, err = readBounded(large, limit)
	require.ErrorIs(t, err, errFileTooLarge, "one byte more is refused")

	_, err = readBounded(filepath.Join(directory, "missing"), limit)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadInputsReadsTheRankedListAndTheDeclaredHandlers(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(content), recordedFilePermissions))
	}
	write(hotOperationsFile, "OpAddInt64 # 50%\nSubOpMoveInt64 # 25%\n")
	write(wrappersFile, "package engine\n\nfunc handleAddInt64(a int) int {\n\treturn handleAddInt64Body(a)\n}\n")
	write(fastPathsFile, "package engine\n\nfunc fastMoveInt64() bool { return true }\n\nfunc stepOverBranch() {}\n")

	inputs, err := readInputs(directory)

	require.NoError(t, err)
	require.Len(t, inputs.operations, 2)
	require.Equal(t, map[string]bool{"handleAddInt64": true}, inputs.wrappers)
	require.Equal(t, map[string]bool{"handleMoveInt64": true}, inputs.fastPaths, "a helper that is not a fast path is left out")
}

func TestReadInputsNamesTheFileItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := readInputs(t.TempDir())

	require.ErrorIs(t, err, os.ErrNotExist)
	require.ErrorContains(t, err, hotOperationsFile)
}

func TestRunChecksItsArgumentsBeforeItBuildsAnything(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr    error
		name       string
		wantOutput string
		arguments  []string
	}{
		{name: "help is printed and is not a failure", arguments: []string{"-h"}, wantOutput: "-update"},
		{name: "a stray argument", arguments: []string{"update"}, wantErr: errUnexpectedArguments},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer

			err := run(t.Context(), tt.arguments, &output)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Contains(t, output.String(), tt.wantOutput)
		})
	}

	t.Run("an unknown flag", func(t *testing.T) {
		t.Parallel()

		err := run(t.Context(), []string{"-nope"}, &bytes.Buffer{})

		require.ErrorContains(t, err, "parsing arguments")
	})
}

func putOnPath(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in compiler is a shell script")
	}
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go"), []byte(script), 0o700))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func useFakeCompiler(t *testing.T, status int, lines ...string) {
	t.Helper()
	putOnPath(t, "#!/bin/sh\ncat >&2 <<'DIAGNOSTICS'\n"+strings.Join(lines, "\n")+"\nDIAGNOSTICS\nexit "+strconv.Itoa(status)+"\n")
}

func TestCompileEngineCollectsTheCompilersDecisions(t *testing.T) {
	useFakeCompiler(t, 0,
		"./vm.go:3:6: can inline handleAddInt64Body",
		"./"+spanFile+":9:14: inlining call to handleAddInt64",
		"./vm.go:5:2: moved to heap: frame",
	)

	facts, err := compileEngine(t.Context())

	require.NoError(t, err)
	require.Equal(t, factsOf([]string{"handleAddInt64Body"}, []string{"handleAddInt64"}), facts)
}

func TestCompileEngineShowsWhatTheCompilerSaidWhenTheBuildFails(t *testing.T) {
	useFakeCompiler(t, 1, "./vm.go:9:2: undefined: frame")

	_, err := compileEngine(t.Context())

	require.ErrorContains(t, err, "building the engine for js/wasm")
	require.ErrorContains(t, err, "undefined: frame", "the compiler's own error is what the reader needs")
}

func TestCompileEngineRefusesABuildThatInlinedNothingIntoTheLoop(t *testing.T) {
	useFakeCompiler(t, 0, "./vm.go:3:6: can inline handleAddInt64Body")

	_, err := compileEngine(t.Context())

	require.ErrorIs(t, err, errNothingInlined)
}

func TestCompileEngineReportsACompilerItCannotStart(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := compileEngine(t.Context())

	require.ErrorContains(t, err, "starting the compiler")
}

func TestCompileEngineStopsABuildThatOutlivesItsContext(t *testing.T) {
	putOnPath(t, "#!/bin/sh\necho './vm.go:3:6: can inline handleAddInt64Body' >&2\nsleep 600 &\nwait\n")
	ctx, cancel := context.WithTimeoutCause(t.Context(), 200*time.Millisecond, errBuildTimedOut)
	defer cancel()
	started := time.Now()

	_, err := compileEngine(ctx)

	require.ErrorIs(t, err, errBuildTimedOut, "the cause of the stop is what the reader is told")
	require.NotContains(t, err.Error(), "can inline", "the compiler's last lines do not explain a build that was stopped")
	require.Less(t, time.Since(started), time.Minute, "the build and the child it started are killed, not waited for")
}

func TestCompileEngineStopsABuildWhoseOutputItCannotRead(t *testing.T) {
	putOnPath(t, "#!/bin/sh\nhead -c "+strconv.Itoa(compilerLineLimit+1)+" /dev/zero | tr '\\0' x >&2\nsleep 600 &\nwait\n")
	started := time.Now()

	_, err := compileEngine(t.Context())

	require.ErrorIs(t, err, bufio.ErrTooLong)
	require.ErrorContains(t, err, "reading the compiler's output")
	require.Less(t, time.Since(started), time.Minute, "a build that cannot be read is killed, with the child it started")
}

func TestRunRecordsAndThenComparesTheInlinedSet(t *testing.T) {
	directory := t.TempDir()
	engine := filepath.Join(directory, engineDirectory)
	require.NoError(t, os.MkdirAll(engine, 0o700))
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(engine, name), []byte(content), recordedFilePermissions))
	}
	write(hotOperationsFile, "OpAddInt64 # 75%\nOpSubtractInt64 # 25%\n")
	write(wrappersFile, "package engine\n")
	write(fastPathsFile, "package engine\n")
	t.Chdir(directory)
	useFakeCompiler(t, 0, "./"+spanFile+":9:14: inlining call to handleAddInt64")

	var output bytes.Buffer
	err := run(t.Context(), nil, &output)
	require.ErrorIs(t, err, os.ErrNotExist, "there is nothing to compare with until the set is recorded")

	require.NoError(t, run(t.Context(), []string{"-update"}, &output))
	recorded, err := os.ReadFile(filepath.Join(engine, recordedFile))
	require.NoError(t, err)
	require.Equal(t, recordedHeader+"OpAddInt64\n", string(recorded))

	output.Reset()
	require.NoError(t, run(t.Context(), nil, &output))
	require.Contains(t, output.String(), "1 of 2 ranked operations inlined")

	useFakeCompiler(t, 0, "./"+spanFile+":9:14: inlining call to handleSubtractInt64")
	require.ErrorIs(t, run(t.Context(), nil, &output), errDrift, "the recorded operation is now called")
}
