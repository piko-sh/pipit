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

// Package cli is the importable shell of the pipit CLI.
//
// The stock pipit binary is a one-line wrapper around [Main]. External users who want a
// customised pipit binary (for example with extra host symbols such as net/http
// registered) can depend on cli, register the symbols they need via [WithSymbols], and
// call [Main] themselves.
package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pipit.sh/pipit/cmd/pipit/internal/output"
)

const (
	// errorFormat is the shared diagnostic template written to stderr for CLI errors.
	errorFormat = "pipit: %v\n"

	// exitPanic is the status for an uncaught panic.
	exitPanic = 2

	// exitInternalError is the status for pipit's own defects.
	exitInternalError = 3

	// exitTimeout is the status for a deadline expiry.
	exitTimeout = 124

	// forceExitCode is the exit code for termination by SIGINT, which is 128 + signum with
	// SIGINT = 2.
	forceExitCode = 130

	// watchdogGrace is how far past its deadline an execution may run before the process is
	// ended outright.
	watchdogGrace = 2 * time.Second
)

// Main is the canonical CLI entry point.
//
// Takes args ([]string) which holds the argv after the program name.
// Takes opts (...Option) which configure the CLI, such as host symbols.
//
// Returns int which is the exit code the process should terminate with.
//
// Concurrency: spawns a goroutine that watches for signals and may call os.Exit on a
// second interrupt.
func Main(args []string, opts ...Option) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	go func() {
		select {
		case <-signals:
			cancel()
			fmt.Fprintln(os.Stderr, "\npipit: interrupt received; cancelling. Press Ctrl-C again to force-exit the process.")
		case <-ctx.Done():
			return
		}
		<-signals
		fmt.Fprintln(os.Stderr, "pipit: forcing exit.")
		os.Exit(forceExitCode)
	}()

	return MainContext(ctx, args, opts...)
}

// MainContext is the context-aware sibling of Main.
//
// Use when the caller already owns a cancellation context (for example tests, or an
// embedding application that drives shutdown explicitly).
//
// Takes args ([]string) which holds the argv after the program name.
// Takes opts (...Option) which configure the CLI, such as host symbols.
//
// Returns int which is the exit code the process should terminate with.
func MainContext(ctx context.Context, args []string, opts ...Option) int {
	streams := output.Real()
	level, rest, err := splitGlobalFlags(args)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	config := newConfig(opts)
	if config.logger == nil {
		config.logger = newStderrLogger(level, streams.Stderr)
	}
	return dispatch(withConfig(ctx, config), rest, streams)
}

// armExecutionWatchdog ends the process with the timeout status when an execution
// overruns its deadline by more than watchdogGrace. A non-positive timeout arms nothing.
//
// Takes timeout (time.Duration) which is the execution's wall-clock limit.
// Takes stderr (io.Writer) which receives the explanation before the process ends.
//
// Returns func() which disarms the watchdog; call it once the execution has returned.
func armExecutionWatchdog(timeout time.Duration, stderr io.Writer) func() {
	if timeout <= 0 {
		return func() {}
	}
	timer := time.AfterFunc(timeout+watchdogGrace, func() {
		fmt.Fprintf(stderr, "pipit: execution cancelled: the %s timeout passed and a native call could not be interrupted\n", timeout)
		os.Exit(exitTimeout)
	})
	return func() { timer.Stop() }
}

// splitGlobalFlags peels the top-level flags off the front of argv.
//
// Only -log-level is global, so the scan stops at the first argument that is not one;
// everything after it belongs to the subcommand. Both `-log-level=debug` and `-log-level
// debug` are accepted, with one or two leading dashes.
//
// Takes args ([]string) which holds the argv after the program name.
//
// Returns Level which is the requested level, or the default when the flag is absent.
// Returns []string which is argv with the global flags removed.
// Returns error when the flag carries no value or names an unknown level.
func splitGlobalFlags(args []string) (slog.Level, []string, error) {
	level := defaultLogLevel
	index := 0
	for index < len(args) {
		name, value, hasValue := splitFlagArg(args[index])
		if name != logLevelFlagName {
			break
		}
		if !hasValue {
			if index+1 >= len(args) {
				return level, nil, fmt.Errorf("-%s needs a value (debug, info, warn, or error)", logLevelFlagName)
			}
			index++
			value = args[index]
		}
		parsed, err := parseLogLevel(value)
		if err != nil {
			return level, nil, err
		}
		level = parsed
		index++
	}
	return level, args[index:], nil
}

// splitFlagArg splits one argv entry into a flag name and an inline value.
//
// Takes arg (string) which is a single argv entry.
//
// Returns string which is the flag name without its dashes, empty when arg is no flag.
// Returns string which is the value from a `name=value` entry, empty otherwise.
// Returns bool which is true when the entry carried an inline value.
func splitFlagArg(arg string) (name, value string, hasValue bool) {
	trimmed := strings.TrimLeft(arg, "-")
	if trimmed == "" || trimmed == arg {
		return "", "", false
	}
	flagName, flagValue, found := strings.Cut(trimmed, "=")
	if found {
		return flagName, flagValue, true
	}
	return trimmed, "", false
}

// dispatch routes the top-level argv to the right subcommand.
//
// Takes args ([]string) which holds the argv after the program name.
// Takes streams (output.IO) which supplies the stdout and stderr writers.
//
// Returns int which is the exit code for the chosen subcommand.
func dispatch(ctx context.Context, args []string, streams output.IO) int {
	if len(args) == 0 {
		printTopLevelHelp(streams.Stdout)
		return 0
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "-h", "--help", "help":
		printTopLevelHelp(streams.Stdout)
		return 0
	case "-v", "--version", "version":
		return RunVersion(ctx, rest, streams)
	case "run":
		return RunRun(ctx, rest, streams)
	case "eval":
		return RunEval(ctx, rest, streams)
	case "isolated":
		return RunIsolated(ctx, rest, streams)
	case "repl":
		return RunRepl(ctx, rest, streams)
	case "test":
		return RunTest(ctx, rest, streams)
	case "compile":
		return RunCompile(ctx, rest, streams)
	case "bytecode":
		return RunBytecode(ctx, rest, streams)
	case "symbols":
		return RunSymbols(ctx, rest, streams)
	case "extract":
		return RunExtract(ctx, rest, streams)
	case "doc":
		return RunDoc(ctx, rest, streams)
	case "fmt":
		return RunFmt(ctx, rest, streams)
	case "debug":
		return RunDebug(ctx, rest, streams)
	case "dap":
		return RunDap(ctx, rest, streams)
	case "module":
		return RunModule(ctx, rest, streams)
	default:
		fmt.Fprintf(streams.Stderr, "pipit: unknown command %q (try `pipit help`)\n", command)
		return 1
	}
}

// printTopLevelHelp writes the top-level command overview to writer.
//
// Takes writer (io.Writer) which receives the formatted help text.
func printTopLevelHelp(writer io.Writer) {
	const help = `pipit - a Go scripting runtime

Usage:
  pipit [global flags] <command> [flags] [args]

Global flags:
  -log-level <level>  Lowest level of pipit's own log records to print on stderr:
                      debug | info | warn | error (default warn)

Commands:
  run        Run a Go source file or directory
  test       Run the Test functions of a package's _test.go files
  eval       Evaluate a Go expression / statement
  isolated   Experimental native execution and isolated REPL
  repl       Interactive Read-Eval-Print Loop (TUI)
  compile    Compile to portable bytecode
  bytecode   Inspect, disassemble, or run compiled bytecode
  symbols    List or describe registered host symbols
  extract    Generate symbol tables for native Go packages
  doc        Look up the signature of a registered symbol
  fmt        Format Go source (gofmt-compatible)
  debug      Step debugger (TUI)
  dap        Debug Adapter Protocol server (for GoLand, VS Code, etc.)
  module     Inspect the module lockfile and cache
  version    Print version information

Global flags go before the command name, as in "pipit -log-level=debug run x.go".
Use "pipit <command> --help" for command-specific flags.
`
	_, _ = writer.Write([]byte(help))
}
