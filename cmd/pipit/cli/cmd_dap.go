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
	"os"

	"pipit.sh/pipit/cmd/pipit/internal/dap"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// RunDap handles `pipit dap`.
//
// Takes args ([]string) which holds the positional command arguments.
// Takes streams (output.IO) which supplies the stdio streams.
//
// Returns int which is the exit code: 0 on clean disconnect, 1 on internal error.
func RunDap(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit dap", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)

	logPath := flags.String("debug-log", "", "Write every DAP message (in + out) to this file; useful when developing the adapter")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit dap [--debug-log <path>]")
		fmt.Fprintln(streams.Stderr, "")
		fmt.Fprintln(streams.Stderr, "Speaks the Debug Adapter Protocol over stdio. The script and its")
		fmt.Fprintln(streams.Stderr, "options are supplied by the client's `launch` request, not the")
		fmt.Fprintln(streams.Stderr, "command line - see docs/dap.md for the launch-args schema.")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 1
	}

	var logFile *os.File
	if *logPath != "" {
		file, err := os.Create(*logPath)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "pipit dap: cannot open log file %q: %v\n", *logPath, err)
			return 1
		}
		defer func() { _ = file.Close() }()
		logFile = file
	}

	return dap.Run(ctx, dap.ServerOptions{
		Stdin:   streams.Stdin,
		Stdout:  streams.Stdout,
		Stderr:  streams.Stderr,
		Log:     logFile,
		Symbols: ExtraSymbols(ctx),
	})
}
