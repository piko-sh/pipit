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
	for _, test := range []struct {
		input   string
		sources []string
		fails   bool
	}{
		{input: "\nvar value = 40\nvalue+2\n:quit\nignored\n", sources: []string{"var value = 40", "value+2"}, fails: false},
		{input: ":begin\nfunc answer() int {\nreturn 42\n}\n:end\nanswer()\n", sources: []string{"func answer() int {\nreturn 42\n}\n", "answer()"}, fails: false},
		{input: ":help\n:quit\n", sources: nil, fails: false},
		{input: ":begin\nunsubmitted\n", sources: nil, fails: true},
		{input: ":reset\n", sources: nil, fails: true},
		{input: ":load /etc/passwd\n", sources: nil, fails: true},
		{input: ":bytecode\n", sources: nil, fails: true},
	} {
		session := replFixture()
		streams := output.IO{Stdin: strings.NewReader(test.input), Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
		err := isolatedReplLoop(context.Background(), session, streams, true, false)
		if (err != nil) != test.fails || strings.Join(session.sources, "|") != strings.Join(test.sources, "|") {
			t.Fatalf("input=%q sources=%q error=%v", test.input, session.sources, err)
		}
	}
}

func TestIsolatedReplInputBounds(t *testing.T) {
	t.Parallel()
	for _, input := range []string{
		strings.Repeat("x", isolatedSourceLimit+1),
		":begin\n" + strings.Repeat("x", isolatedSourceLimit/2) + "\n" + strings.Repeat("x", isolatedSourceLimit/2) + "\n:end\n",
		strings.Repeat("\n", isolatedReplLineLimit+1),
		strings.Repeat(strings.Repeat(" ", isolatedReplReadBuffer-1)+"\n", isolatedReplInputLimit/isolatedReplReadBuffer+1),
	} {
		session := replFixture()
		streams := output.IO{Stdin: strings.NewReader(input), Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
		if err := isolatedReplLoop(context.Background(), session, streams, true, false); err == nil || len(session.sources) != 0 {
			t.Fatalf("oversized input reached worker: calls=%d error=%v", len(session.sources), err)
		}
	}
}

func TestIsolatedReplFailureStopsAndSuppressesResults(t *testing.T) {
	t.Parallel()
	session := replFixture()
	session.err = errors.New("failure\x1b[2J")
	session.result.Value = []byte("42")
	var stdout, stderr bytes.Buffer
	streams := output.IO{Stdin: strings.NewReader("bad\nshouldNotRun\n"), Stdout: &stdout, Stderr: &stderr, ColourMode: output.ColourNever}
	err := isolatedReplLoop(context.Background(), session, streams, true, false)
	if err == nil || len(session.sources) != 1 || stdout.Len() != 0 || strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("failed session continued or emitted unsafe diagnostics: calls=%d output=%q error=%v", len(session.sources), stdout.String(), err)
	}
}

func TestIsolatedReplTerminalOutputEscapesControlSequences(t *testing.T) {
	t.Parallel()
	session := replFixture()
	session.result.Output = "\x1b]52;c;clipboard\a"
	var stdout bytes.Buffer
	streams := output.IO{Stdin: strings.NewReader("42\n"), Stdout: &stdout, Stderr: io.Discard, ColourMode: output.ColourNever}
	if err := isolatedReplLoop(context.Background(), session, streams, true, true); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(stdout.String(), "\x1b\a") || !strings.Contains(stdout.String(), "\\x1b") {
		t.Fatalf("terminal control sequence escaped: %q", stdout.String())
	}
}

func TestIsolatedReplConflictingSourceRefused(t *testing.T) {
	t.Parallel()
	for _, selection := range [][]string{{"-e", ""}, {"-entrypoint", ""}, {"--check"}, {"-"}, {"file.go"}} {
		args := make([]string, 0, 5+len(selection))
		args = append(args, "--repl", "-worker", "/missing", "-worker-sha256", strings.Repeat("1", 64))
		args = append(args, "-watchdog", "/missing-watchdog", "-watchdog-sha256", strings.Repeat("2", 64))
		args = append(args, "-state-dir", "/missing-state")
		args = append(args, selection...)
		result := clitest.Run(context.Background(), RunIsolated, args, "42")
		if result.Code != 1 || result.Stdout != "" || !strings.Contains(result.Stderr, "cannot be combined") {
			t.Fatalf("conflicting REPL invocation accepted: %+v", result)
		}
	}
}

func TestIsolatedReplWorkerExitAndCancellationInterruptInput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux cancellable pipe input")
	}
	t.Parallel()
	for _, workerExit := range []bool{false, true} {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
		input, err := newIsolatedReplInput(reader)
		if err != nil {
			t.Fatal(err)
		}
		session := replFixture()
		ctx, cancel := context.WithCancel(context.Background())
		session.closeErr = errors.New("worker terminated unexpectedly")
		done := make(chan error, 1)
		streams := output.IO{Stdin: reader, Stdout: io.Discard, Stderr: io.Discard, ColourMode: output.ColourNever}
		go func() { done <- driveIsolatedRepl(ctx, session, input, streams, true) }()
		if workerExit {
			close(session.done)
		} else {
			cancel()
		}
		select {
		case err := <-done:
			if err == nil {
				t.Error("ended session left successful input loop")
			}
		case <-time.After(time.Second):
			input.Cancel()
			t.Error("blocked input did not observe session termination")
			<-done
		}
		cancel()
		if err := input.Close(); err != nil {
			t.Fatal(err)
		}
		if len(session.sources) != 0 {
			t.Fatal("cancelled input submitted source")
		}
		if _, err := reader.Stat(); err != nil {
			t.Fatalf("borrowed input descriptor closed: %v", err)
		}
	}
}

type unsupportedReplInput struct{}

func (unsupportedReplInput) Read(_ []byte) (int, error) {
	panic("unsupported input reader must not be called")
}

func TestIsolatedReplInputRefusesUncancellableReaders(t *testing.T) {
	t.Parallel()
	for _, reader := range []io.Reader{nil, unsupportedReplInput{}} {
		if input, err := newIsolatedReplInput(reader); err == nil || input != nil {
			t.Fatal("unsupported reader accepted")
		}
	}
}

func testIsolatedReplCancellationNative(t *testing.T, flags []string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	args := append(append([]string(nil), flags...), "--repl")
	result := clitest.RunWithReader(ctx, RunIsolated, args, reader)
	if result.Code != 1 || ctx.Err() == nil || result.Stdout != "" {
		t.Fatalf("blocked prompt survived cancellation: code=%d output=%q error=%q", result.Code, result.Stdout, result.Stderr)
	}
	if _, err := reader.Stat(); err != nil {
		t.Fatalf("borrowed input descriptor closed: %v", err)
	}
}
