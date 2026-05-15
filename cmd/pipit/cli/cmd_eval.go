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
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
)

// RunEval handles `pipit eval -e "<code>"` or `pipit eval -`.
//
// Takes args ([]string) which holds the command-line arguments to parse.
// Takes streams (output.IO) which supplies the stdin, stdout and stderr.
//
// Returns int which is the exit code; 0 on success, 1 on failure.
func RunEval(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit eval", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)

	expression := flags.String("e", "", "Expression or statement to evaluate (use `-` after to read stdin)")
	timeout := flags.Duration("timeout", 1*time.Minute, "Maximum wall-clock time per evaluation")
	printResult := flags.Bool("print", true, "Print the resulting value to stdout")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit eval -e \"<code>\"")
		fmt.Fprintln(streams.Stderr, "       pipit eval -")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return 1
	}

	source, err := resolveEvalSource(*expression, flags.Args(), streams.Stdin)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if source == "" {
		flags.Usage()
		return 1
	}

	interpreter := runner.New(runner.Limits{
		Timeout: *timeout, Symbols: ExtraSymbols(ctx), Logger: loggerFromContext(ctx),
		BytecodeStore: nil, MaxAlloc: 0, MaxCallDepth: 0, MaxOutputSize: 0, CostBudget: 0, MaxGoroutines: 0})
	stopWatchdog := armExecutionWatchdog(*timeout, streams.Stderr)
	defer stopWatchdog()
	result, err := interpreter.Eval(ctx, source)
	if err != nil {
		return reportExecutionError(streams.Stderr, err)
	}
	if *printResult && result != nil {
		fmt.Fprintln(streams.Stdout, formatResult(result))
	}
	return 0
}

// resolveEvalSource picks a source string from -e, positional "-", or from remaining
// positional args joined with newlines.
//
// Takes expression (string) which holds the -e flag value, if any.
// Takes positional ([]string) which holds the remaining arguments.
// Takes stdin (io.Reader) which is read when the first argument is "-".
//
// Returns string which is the resolved source to evaluate.
// Returns error when stdin cannot be read.
func resolveEvalSource(expression string, positional []string, stdin io.Reader) (string, error) {
	if expression != "" {
		return expression, nil
	}
	if len(positional) > 0 && positional[0] == "-" {
		body, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(body), nil
	}
	if len(positional) > 0 {
		var joined strings.Builder
		for index, fragment := range positional {
			if index > 0 {
				joined.WriteString("\n")
			}
			joined.WriteString(fragment)
		}
		return joined.String(), nil
	}
	return "", nil
}
