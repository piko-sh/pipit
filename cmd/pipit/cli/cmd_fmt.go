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
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
)

// fallbackFileMode is the permission bits used when the original file's mode cannot be
// read back before rewriting it.
const fallbackFileMode = 0o600

// RunFmt handles `pipit fmt [-w] [-d] <file|dir...>`. Stdin form: `pipit fmt -`.
//
// Takes args ([]string) which holds the command arguments and targets.
// Takes streams (output.IO) which supplies stdin, stdout and stderr.
//
// Returns int which is the exit code: 0 on success, 1 on parse error, 2 when -d is set
// and differences were found.
func RunFmt(_ context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit fmt", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	write := flags.Bool("w", false, "Write result to (source) file instead of stdout")
	check := flags.Bool("d", false, "Print diffs / exit 2 when files differ")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit fmt [-w] [-d] <file|dir...>")
		fmt.Fprintln(streams.Stderr, "       pipit fmt -")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() < 1 {
		return formatStream(streams.Stdin, streams.Stdout, streams.Stderr)
	}

	exit := 0
	for _, target := range flags.Args() {
		if target == "-" {
			if status := formatStream(streams.Stdin, streams.Stdout, streams.Stderr); status > exit {
				exit = status
			}
			continue
		}
		if status := formatPath(target, *write, *check, streams); status > exit {
			exit = status
		}
	}
	return exit
}

// formatStream reads Go source from stdin, formats it, and writes the result to stdout.
//
// Takes stdin (io.Reader) which supplies the Go source to format.
// Takes stdout (io.Writer) which receives the formatted output.
// Takes stderr (io.Writer) which receives error diagnostics.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func formatStream(stdin io.Reader, stdout, stderr io.Writer) int {
	source, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "pipit fmt: reading stdin: %v\n", err)
		return 1
	}
	formatted, err := pipit.FormatGoSource(source)
	if err != nil {
		fmt.Fprintf(stderr, "pipit fmt: %v\n", err)
		return 1
	}
	_, _ = stdout.Write(formatted)
	return 0
}

// formatPath formats the Go file at path, or every .go file beneath it when path is a
// directory.
//
// Takes path (string) which is the file or directory to format.
// Takes write (bool) which rewrites files in place when true.
// Takes check (bool) which reports differing files and exits 2 when true.
// Takes streams (output.IO) which supplies stdout and stderr.
//
// Returns int which is the highest exit code from the formatted files.
func formatPath(path string, write, check bool, streams output.IO) int {
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "pipit fmt: cannot stat %q: %v\n", path, err)
		return 1
	}
	if info.IsDir() {
		exit := 0
		_ = filepath.WalkDir(path, func(child string, dirEntry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if dirEntry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(child, ".go") {
				return nil
			}
			if status := formatFile(child, write, check, streams); status > exit {
				exit = status
			}
			return nil
		})
		return exit
	}
	return formatFile(path, write, check, streams)
}

// formatFile formats the single Go file at path according to the write and check flags.
//
// Takes path (string) which is the Go file to format.
// Takes write (bool) which rewrites the file in place when true.
// Takes check (bool) which prints the path and exits 2 when set.
// Takes streams (output.IO) which supplies stdout and stderr.
//
// Returns int which is the exit code: 0 ok, 1 on error, 2 when differing.
func formatFile(path string, write, check bool, streams output.IO) int {
	source, err := os.ReadFile(path) //nolint:gosec // operator-supplied source path
	if err != nil {
		fmt.Fprintf(streams.Stderr, "pipit fmt: %v\n", err)
		return 1
	}
	formatted, err := pipit.FormatGoSource(source)
	if err != nil {
		fmt.Fprintf(streams.Stderr, "pipit fmt: %s: %v\n", path, err)
		return 1
	}
	if check && !bytes.Equal(source, formatted) {
		fmt.Fprintf(streams.Stdout, "%s differs\n", path)
		return 2
	}
	if write {
		mode := os.FileMode(fallbackFileMode)
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(path, formatted, mode); err != nil { //nolint:gosec // operator-supplied path
			fmt.Fprintf(streams.Stderr, "pipit fmt: %v\n", err)
			return 1
		}
		return 0
	}
	_, _ = streams.Stdout.Write(formatted)
	return 0
}
