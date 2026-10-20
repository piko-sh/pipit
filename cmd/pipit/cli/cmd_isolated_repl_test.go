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

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/clitest"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

type replSessionFixture struct {
	done     chan struct{}
	sources  []string
	result   pipit.RestrictedResult
	err      error
	closeErr error
}

func (session *replSessionFixture) Submit(_ context.Context, source string) (pipit.RestrictedResult, error) {
	session.sources = append(session.sources, source)
	return session.result, session.err
}

func (session *replSessionFixture) Done() <-chan struct{} { return session.done }
func (session *replSessionFixture) Close() error          { return session.closeErr }

func replFixture() *replSessionFixture {
	var session replSessionFixture
	session.done = make(chan struct{})
	return &session
}

func TestIsolatedReplFraming(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		wantSources []string
		wantErr     bool
	}{
		{name: "statements then quit", input: "\nvar value = 40\nvalue+2\n:quit\nignored\n", wantSources: []string{"var value = 40", "value+2"}},
		{name: "a block between begin and end", input: ":begin\nfunc answer() int {\nreturn 42\n}\n:end\nanswer()\n", wantSources: []string{"func answer() int {\nreturn 42\n}\n", "answer()"}},
		{name: "help then quit", input: ":help\n:quit\n"},
		{name: "an unterminated block", input: ":begin\nunsubmitted\n", wantErr: true},
		{name: "reset is refused", input: ":reset\n", wantErr: true},
		{name: "load is refused", input: ":load /etc/passwd\n", wantErr: true},
		{name: "bytecode is refused", input: ":bytecode\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := replFixture()
			streams := output.IO{Stdin: strings.NewReader(tt.input), Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
			err := isolatedReplLoop(t.Context(), session, streams, true, false)
			require.Equal(t, tt.wantErr, err != nil, "error: %v", err)
			require.Equal(t, strings.Join(tt.wantSources, "|"), strings.Join(session.sources, "|"))
		})
	}
}

func TestIsolatedReplInputBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{name: "an overlong line", input: strings.Repeat("x", isolatedSourceLimit+1)},
		{name: "an overlong block", input: ":begin\n" + strings.Repeat("x", isolatedSourceLimit/2) + "\n" + strings.Repeat("x", isolatedSourceLimit/2) + "\n:end\n"},
		{name: "too many lines", input: strings.Repeat("\n", isolatedReplLineLimit+1)},
		{name: "too much input", input: strings.Repeat(strings.Repeat(" ", isolatedReplReadBuffer-1)+"\n", isolatedReplInputLimit/isolatedReplReadBuffer+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := replFixture()
			streams := output.IO{Stdin: strings.NewReader(tt.input), Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
			require.Error(t, isolatedReplLoop(t.Context(), session, streams, true, false), "oversized input never reaches the worker")
			require.Empty(t, session.sources)
		})
	}
}

func TestIsolatedReplFailureStopsAndSuppressesResults(t *testing.T) {
	t.Parallel()
	session := replFixture()
	session.err = errors.New("failure\x1b[2J")
	session.result.Value = []byte("42")
	var stdout, stderr bytes.Buffer
	streams := output.IO{Stdin: strings.NewReader("bad\nshouldNotRun\n"), Stdout: &stdout, Stderr: &stderr, ColourMode: output.ColourNever}
	err := isolatedReplLoop(t.Context(), session, streams, true, false)
	require.Error(t, err)
	require.Len(t, session.sources, 1, "a failed session stops reading input")
	require.Empty(t, stdout.String())
	require.NotContains(t, err.Error(), "\x1b", "diagnostics are escaped")
}

func TestIsolatedReplTerminalOutputEscapesControlSequences(t *testing.T) {
	t.Parallel()
	session := replFixture()
	session.result.Output = "\x1b]52;c;clipboard\a"
	var stdout bytes.Buffer
	streams := output.IO{Stdin: strings.NewReader("42\n"), Stdout: &stdout, Stderr: io.Discard, ColourMode: output.ColourNever}
	require.NoError(t, isolatedReplLoop(t.Context(), session, streams, true, true))
	require.False(t, strings.ContainsAny(stdout.String(), "\x1b\a"), "terminal control sequences are escaped: %q", stdout.String())
	require.Contains(t, stdout.String(), "\\x1b")
}

func TestIsolatedReplConflictingSourceRefused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		selection []string
	}{
		{name: "empty expression", selection: []string{"-e", ""}},
		{name: "empty entrypoint", selection: []string{"-entrypoint", ""}},
		{name: "check", selection: []string{"--check"}},
		{name: "stdin", selection: []string{"-"}},
		{name: "file", selection: []string{"file.go"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			args := make([]string, 0, 5+len(tt.selection))
			args = append(args, "--repl", "-worker", "/missing", "-worker-sha256", strings.Repeat("1", 64))
			args = append(args, "-watchdog", "/missing-watchdog", "-watchdog-sha256", strings.Repeat("2", 64))
			args = append(args, "-state-dir", "/missing-state")
			args = append(args, tt.selection...)
			result := clitest.Run(t.Context(), RunIsolated, args, "42")
			require.Equal(t, 1, result.Code, "a conflicting REPL invocation is refused")
			require.Empty(t, result.Stdout)
			require.Contains(t, result.Stderr, "cannot be combined")
		})
	}
}

func TestIsolatedReplWorkerExitAndCancellationInterruptInput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("Linux cancellable pipe input")
	}
	tests := []struct {
		name       string
		workerExit bool
	}{
		{name: "cancellation", workerExit: false},
		{name: "worker exit", workerExit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader, writer, err := os.Pipe()
			require.NoError(t, err)
			t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
			input, err := newIsolatedReplInput(reader)
			require.NoError(t, err)
			session := replFixture()
			ctx, cancel := context.WithCancel(t.Context())
			session.closeErr = errors.New("worker terminated unexpectedly")
			done := make(chan error, 1)
			streams := output.IO{Stdin: reader, Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
			go func() { done <- driveIsolatedRepl(ctx, session, input, streams, true) }()
			if tt.workerExit {
				close(session.done)
			} else {
				cancel()
			}
			select {
			case err := <-done:
				require.Error(t, err, "an ended session ends the input loop with an error")
			case <-time.After(time.Second):
				input.Cancel()
				assert.Fail(t, "blocked input observes session termination")
				<-done
			}
			cancel()
			require.NoError(t, input.Close())
			require.Empty(t, session.sources, "cancelled input submits no source")
			_, err = reader.Stat()
			require.NoError(t, err, "a borrowed input descriptor stays open")
		})
	}
}

type unsupportedReplInput struct{}

func (unsupportedReplInput) Read(_ []byte) (int, error) {
	panic("unsupported input reader must not be called")
}

func TestIsolatedReplInputRefusesUncancellableReaders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		reader io.Reader
	}{
		{name: "nil reader", reader: nil},
		{name: "plain reader", reader: unsupportedReplInput{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			input, err := newIsolatedReplInput(tt.reader)
			require.Error(t, err, "an uncancellable reader is refused")
			require.Nil(t, input)
		})
	}
}

func testIsolatedReplCancellationNative(t *testing.T, flags []string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	args := append(append([]string(nil), flags...), "--repl")
	result := clitest.RunWithReader(ctx, RunIsolated, args, reader)
	require.Equal(t, 1, result.Code, "cancellation ends a blocked prompt: %q", result.Stderr)
	require.Error(t, ctx.Err())
	require.Empty(t, result.Stdout)
	_, err = reader.Stat()
	require.NoError(t, err, "a borrowed input descriptor stays open")
}
