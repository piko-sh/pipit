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
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
)

// RunCompile handles `pipit compile <file|dir> -o out.pbc`.
//
// Takes args ([]string) which holds the command-line arguments to parse.
// Takes streams (output.IO) which supplies the standard I/O streams.
//
// Returns int which is the process exit code; 0 on success, 1 on failure.
func RunCompile(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit compile", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)

	out := flags.String("o", "", "Output file path (required)")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit compile -o <out.pbc> <file|directory>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() < 1 || *out == "" {
		flags.Usage()
		return 1
	}

	target := flags.Arg(0)
	if err := compileTarget(ctx, target, *out, streams); err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	fmt.Fprintf(streams.Stdout, "wrote %s\n", *out)
	return 0
}

// compileTarget compiles the target file or directory and writes the bytecode output to
// out.
//
// Takes target (string) which is the source file or directory path.
// Takes out (string) which is the destination bytecode file path.
//
// Returns error when sources cannot be read, the output directory cannot be prepared, or
// compilation fails.
func compileTarget(ctx context.Context, target, out string, _ output.IO) error {
	sources, err := collectSources(target)
	if err != nil {
		return err
	}

	directory := filepath.Dir(out)
	store, err := pipit.NewDirectoryBytecodeStore(directory)
	if err != nil {
		return fmt.Errorf("cannot prepare output directory: %w", err)
	}
	interpreter := runner.New(runner.Limits{
		BytecodeStore: store, Symbols: ExtraSymbols(ctx), Logger: loggerFromContext(ctx),
		Timeout: 0, MaxAlloc: 0, MaxCallDepth: 0, MaxOutputSize: 0, CostBudget: 0, MaxGoroutines: 0})

	compiled, err := interpreter.CompileFileSet(ctx, sources)
	if err != nil {
		return err
	}
	return pipit.SaveCompiledToFile(ctx, interpreter, out, compiled)
}

// collectSources reads a single .go file or every .go file in a directory, keyed by
// basename. Skips _test.go files.
//
// Takes target (string) which is the file or directory path to read.
//
// Returns map[string]string which maps each basename to its source.
// Returns error when the target cannot be read or has no .go files.
func collectSources(target string) (map[string]string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("cannot stat %q: %w", target, err)
	}
	if !info.IsDir() {
		body, err := os.ReadFile(target) //nolint:gosec // operator-supplied script path
		if err != nil {
			return nil, fmt.Errorf("cannot read %q: %w", target, err)
		}
		return map[string]string{filepath.Base(target): string(body)}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %q: %w", target, err)
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(target, name)) //nolint:gosec // operator-supplied script directory
		if err != nil {
			return nil, fmt.Errorf("cannot read %q: %w", name, err)
		}
		sources[name] = string(body)
	}
	if len(sources) == 0 {
		return nil, errors.New("no .go files found")
	}
	return sources, nil
}
