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
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/muesli/cancelreader"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

const (
	// isolatedReplInputLimit is the maximum total bytes the REPL reads from its input
	// source.
	isolatedReplInputLimit = 4 << 20

	// isolatedReplReadBuffer is the initial scanner buffer size for REPL line reads.
	isolatedReplReadBuffer = 4096

	// isolatedReplLineLimit is the maximum number of input lines the REPL processes in one
	// session.
	isolatedReplLineLimit = 16 << 10
)

// isolatedReplSession is the persistent evaluation surface a native worker session
// exposes.
type isolatedReplSession interface {
	// Submit evaluates bounded source in the session.
	//
	// Takes source (string) which is the input text.
	//
	// Returns pipit.RestrictedResult which is the session output.
	// Returns error when execution fails.
	Submit(context.Context, string) (pipit.RestrictedResult, error)

	// Done returns a channel that closes when the session terminates.
	//
	// Returns <-chan struct{} which signals termination.
	Done() <-chan struct{}

	// Close releases the native session resources.
	//
	// Returns error when cleanup fails.
	Close() error
}

// isolatedReplSubmitter wraps a session so submissions use the caller's context instead
// of the input loop's.
type isolatedReplSubmitter struct {
	// isolatedReplSession is the underlying session.
	isolatedReplSession

	// ctx is the caller's context, independent of input cancellation.
	ctx context.Context
}

// Submit preserves the caller's context independently of input cancellation. Worker exit
// can stop input without discarding a final already-reaped response.
//
// Takes source (string) which is the bounded input text.
//
// Returns pipit.RestrictedResult which is the session output.
// Returns error when execution or caller cancellation fails.
func (submitter isolatedReplSubmitter) Submit(_ context.Context, source string) (pipit.RestrictedResult, error) {
	return submitter.isolatedReplSession.Submit(submitter.ctx, source)
}

// isolatedReplState tracks the line framing state for the REPL input loop.
type isolatedReplState struct {
	// block accumulates multiline source between :begin and :end markers.
	block strings.Builder

	// multiline is true while inside a :begin block.
	multiline bool
}

// finish validates that EOF did not interrupt an explicitly opened source block.
//
// Returns an error for an unfinished block, otherwise nil.
func (state *isolatedReplState) finish() error {
	if state.multiline {
		return errors.New("unfinished isolated REPL block; expected :end")
	}
	return nil
}

// prompt writes trusted terminal chrome without including script-controlled bytes.
//
// Takes writer (io.Writer) which receives the prompt text.
// Takes interactive (bool) which enables terminal prompts.
//
// Returns any output error.
func (state *isolatedReplState) prompt(writer io.Writer, interactive bool) error {
	if !interactive {
		return nil
	}
	prompt := "isolated> "
	if state.multiline {
		prompt = "... "
	}
	_, err := io.WriteString(writer, prompt)
	return err
}

// accept recognises only explicit framing and exit commands, never Go syntax.
//
// Takes line (string) which is the raw input text.
// Takes writer (io.Writer) which receives trusted help output.
//
// Returns string which is the accumulated source text.
// Returns bool which is true when the user quits.
// Returns error which reports a framing or source-size fault.
func (state *isolatedReplState) accept(line string, writer io.Writer) (string, bool, error) {
	if state.multiline {
		if line != ":end" {
			return "", false, appendIsolatedReplLine(&state.block, line)
		}
		state.multiline = false
		source := state.block.String()
		state.block.Reset()
		return source, false, nil
	}
	switch line {
	case ":quit":
		return "", true, nil
	case ":begin":
		state.multiline = true
	case ":help":
		_, err := io.WriteString(writer, "Enter Go source, :begin/:end for multiple lines, or :quit. Any failure ends this isolated session.\n")
		return "", false, err
	default:
		if strings.HasPrefix(line, ":") {
			return "", false, errors.New("unsupported isolated REPL command")
		}
		return line, false, nil
	}
	return "", false, nil
}

// isolatedReplSelection refuses source modes and authority changes before reading stdin.
//
// Takes options (isolatedOptions) which describes the explicit command selection.
// Takes flags (*flag.FlagSet) which records the parsed command-line flags.
//
// Returns error for conflicting source selection or a probe request.
func isolatedReplSelection(options isolatedOptions, flags *flag.FlagSet) error {
	conflict := *options.check || len(flags.Args()) != 0
	flags.Visit(func(option *flag.Flag) {
		if option.Name == "e" || option.Name == "entrypoint" {
			conflict = true
		}
	})
	if conflict {
		return errors.New("isolated --repl cannot be combined with --check, source or an entrypoint")
	}
	return nil
}

// runIsolatedRepl starts a dedicated worker without importing trusted REPL hooks.
//
// Takes config (pipit.IsolatedConfig) which is the approved worker configuration.
// Takes streams (output.IO) which supplies source input and bounded result destinations.
// Takes printResult (bool) which enables JSON scalar output.
//
// Returns zero only after the input loop and native cleanup both succeed.
func runIsolatedRepl(ctx context.Context, config pipit.IsolatedConfig, streams output.IO, printResult bool) (exitCode int) {
	input, err := newIsolatedReplInput(streams.Stdin)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	defer func() {
		if err := input.Close(); err != nil {
			fmt.Fprintf(streams.Stderr, errorFormat, err)
			exitCode = 1
		}
	}()
	session, err := pipit.NewIsolatedSession(ctx, config)
	if session != nil {
		defer func() {
			if err := session.Close(); err != nil {
				fmt.Fprintf(streams.Stderr, errorFormat, err)
				exitCode = 1
			}
		}()
	}
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if err := driveIsolatedRepl(ctx, session, input, streams, printResult); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	return 0
}

// driveIsolatedRepl interrupts host input when the worker or caller stops. It joins
// cancellation callbacks before the input owner closes their descriptors.
//
// Takes session (isolatedReplSession) which is the dedicated worker session.
// Takes input (cancelreader.CancelReader) which owns cancellation resources.
// Takes streams (output.IO) which supplies the standard input and output.
// Takes printResult (bool) which enables JSON scalar output.
//
// Returns the first input, submission or cancellation error.
//
// Concurrency: spawns a goroutine to monitor session termination; the goroutine joins
// before the call completes.
func driveIsolatedRepl(ctx context.Context, session isolatedReplSession, input cancelreader.CancelReader, streams output.IO, printResult bool) error {
	loopContext, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		select {
		case <-session.Done():
			cancel()
		case <-loopContext.Done():
		}
	}()
	cancelDone := make(chan struct{})
	stopInput := context.AfterFunc(loopContext, func() { input.Cancel(); close(cancelDone) })
	defer func() {
		cancel()
		<-monitorDone
		if !stopInput() {
			<-cancelDone
		}
	}()
	interactive := streams.IsStdinTTY()
	streams.Stdin = input
	submitter := isolatedReplSubmitter{isolatedReplSession: session, ctx: ctx}
	err := isolatedReplLoop(loopContext, submitter, streams, printResult, interactive)
	if errors.Is(err, context.Canceled) && ctx.Err() == nil {
		return session.Close()
	}
	return err
}

// isolatedReplLoop reads bounded lines and explicit multiline blocks. It never parses,
// compiles, loads files, changes grants or exposes interpreter state in the host.
//
// Takes session (isolatedReplSession) which is the dedicated worker session.
// Takes streams (output.IO) which supplies input and output writers.
// Takes printResult (bool) which enables JSON scalar output.
// Takes interactive (bool) which enables terminal prompts.
//
// Returns the first input, submission or output error. Failures are not recoverable.
func isolatedReplLoop(ctx context.Context, session isolatedReplSession, streams output.IO, printResult, interactive bool) error {
	input := &io.LimitedReader{R: streams.Stdin, N: isolatedReplInputLimit + 1}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, isolatedReplReadBuffer), isolatedSourceLimit+1)
	var state isolatedReplState
	remainingLines := isolatedReplLineLimit
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := state.prompt(streams.Stderr, interactive); err != nil {
			return err
		}
		if !scanner.Scan() {
			break
		}
		if input.N == 0 || remainingLines == 0 {
			return errors.New("isolated REPL input limit exceeded")
		}
		remainingLines--
		source, quit, err := state.accept(scanner.Text(), streams.Stderr)
		if err != nil || quit {
			return err
		}
		if err := submitIsolatedRepl(ctx, session, source, streams, printResult, interactive); err != nil {
			return err
		}
	}
	return errors.Join(ctx.Err(), scanner.Err(), state.finish())
}

// appendIsolatedReplLine bounds a multiline block before allocating retained source.
//
// Takes block (*strings.Builder) which accumulates the multiline source.
// Takes line (string) which is the next input line.
//
// Returns error before the source byte limit can be exceeded.
func appendIsolatedReplLine(block *strings.Builder, line string) error {
	if len(line) >= isolatedSourceLimit-block.Len() {
		return errors.New("isolated REPL source limit exceeded")
	}
	block.WriteString(line)
	block.WriteByte('\n')
	return nil
}

// submitIsolatedRepl submits non-blank source and publishes bounded scalar results.
// Terminal output is quoted so script control sequences cannot operate the terminal.
//
// Takes session (isolatedReplSession) which is the dedicated worker session.
// Takes source (string) which is the bounded input text.
// Takes streams (output.IO) which supplies output writers.
// Takes printResult (bool) which enables JSON scalar output.
// Takes interactive (bool) which enables terminal quoting.
//
// Returns error for evaluation or output failure without continuing the session.
func submitIsolatedRepl(
	ctx context.Context, session isolatedReplSession, source string, streams output.IO,
	printResult, interactive bool,
) error {
	if strings.TrimSpace(source) == "" {
		return nil
	}
	if len(source) > isolatedSourceLimit {
		return errors.New("isolated REPL source limit exceeded")
	}
	result, err := session.Submit(ctx, source)
	if err != nil {
		return fmt.Errorf("isolated submission failed: %q", err.Error())
	}
	if interactive && result.Output != "" {
		result.Output = strconv.Quote(result.Output) + "\n"
	}
	if writeIsolatedResult(result, nil, printResult, streams) != 0 {
		return errors.New("writing isolated REPL result failed")
	}
	return nil
}
