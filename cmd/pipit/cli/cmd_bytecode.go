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
	"path/filepath"

	"charm.land/lipgloss/v2"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/disasm"
	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/runner"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
)

// RunBytecode dispatches `pipit bytecode <inspect|disasm|run> ...`.
//
// Takes args ([]string) which holds the subcommand and its arguments.
// Takes streams (output.IO) which supplies the standard IO streams.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func RunBytecode(ctx context.Context, args []string, streams output.IO) int {
	if len(args) == 0 {
		printBytecodeUsage(streams.Stderr)
		return 1
	}
	subcommand := args[0]
	rest := args[1:]
	switch subcommand {
	case "inspect":
		return runBytecodeInspect(ctx, rest, streams)
	case "disasm", "disassemble":
		return runBytecodeDisasm(ctx, rest, streams)
	case "run":
		return runBytecodeRun(ctx, rest, streams)
	case "-h", "--help", "help":
		printBytecodeUsage(streams.Stdout)
		return 0
	default:
		fmt.Fprintf(streams.Stderr, "pipit bytecode: unknown subcommand %q\n", subcommand)
		printBytecodeUsage(streams.Stderr)
		return 1
	}
}

// printBytecodeUsage writes the bytecode subcommand usage text to writer.
//
// Takes writer (interface{ Write(p []byte) (int, error) }) which receives the rendered
// usage text.
func printBytecodeUsage(writer interface{ Write(p []byte) (int, error) }) {
	const usage = `Usage:
  pipit bytecode inspect  <file.pbc>
  pipit bytecode disasm   <file.pbc> [--func <name>] [--no-colour]
  pipit bytecode run      <file.pbc> [--entrypoint <name>]
`
	_, _ = writer.Write([]byte(usage))
}

// runBytecodeInspect prints metadata about a compiled bytecode artefact.
//
// Takes args ([]string) which holds the file path and any extra arguments.
// Takes streams (output.IO) which supplies the standard IO streams.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func runBytecodeInspect(ctx context.Context, args []string, streams output.IO) int {
	if len(args) < 1 {
		fmt.Fprintln(streams.Stderr, "Usage: pipit bytecode inspect <file.pbc>")
		return 1
	}
	path := args[0]
	_, compiled, err := loadBytecodeFile(ctx, path)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}

	theme := styles.For(streams.ShouldColour())
	info, statErr := os.Stat(path)
	size := int64(-1)
	if statErr == nil {
		size = info.Size()
	}
	fmt.Fprintln(streams.Stdout, theme.Title.Render("Bytecode artefact"))
	fmt.Fprintf(streams.Stdout, "  %s %s\n", theme.Subtle.Render("path:"), path)
	if size >= 0 {
		fmt.Fprintf(streams.Stdout, "  %s %d bytes\n", theme.Subtle.Render("size:"), size)
	}
	fmt.Fprintf(streams.Stdout, "  %s %s\n", theme.Subtle.Render("schema:"), pipit.BytecodeVersion)
	functions := compiledFunctions(compiled)
	fmt.Fprintf(streams.Stdout, "  %s %d\n", theme.Subtle.Render("functions:"), len(functions))
	for _, name := range functions {
		fmt.Fprintf(streams.Stdout, "    %s\n", name)
	}
	return 0
}

// runBytecodeDisasm renders the disassembly of a compiled bytecode file.
//
// Takes args ([]string) which holds the file path and flag arguments.
// Takes streams (output.IO) which supplies the standard IO streams.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func runBytecodeDisasm(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit bytecode disasm", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	function := flags.String("func", "", "Only disassemble the named function (omit for all)")
	noColour := flags.Bool("no-colour", false, "Disable ANSI styling")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit bytecode disasm [--func <name>] [--no-colour] <file.pbc>")
		flags.PrintDefaults()
	}
	path, ok := parseBytecodePath(flags, args)
	if !ok {
		return 1
	}
	_, compiled, err := loadBytecodeFile(ctx, path)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}

	useColour := !*noColour && streams.ShouldColour()
	theme := styles.For(useColour)

	matched := false
	for _, item := range eachFunction(compiled) {
		if *function != "" && item.name != *function {
			continue
		}
		matched = true
		header := theme.Title.Render(item.name)
		fmt.Fprintln(streams.Stdout, header)
		fmt.Fprintln(streams.Stdout, lipgloss.NewStyle().Render(disasm.Render(item.compiled, theme)))
	}
	if !matched {
		fmt.Fprintln(streams.Stderr, "no functions found")
		return 1
	}
	return 0
}

// runBytecodeRun executes an entrypoint in a compiled bytecode file.
//
// Takes args ([]string) which holds the file path and flag arguments.
// Takes streams (output.IO) which supplies the standard IO streams.
//
// Returns int which is the exit code: 0 on success, 1 on failure.
func runBytecodeRun(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit bytecode run", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	entrypoint := flags.String("entrypoint", "main", "Function name to invoke")
	flags.Usage = func() {
		fmt.Fprintln(streams.Stderr, "Usage: pipit bytecode run [--entrypoint <name>] <file.pbc>")
		flags.PrintDefaults()
	}
	path, ok := parseBytecodePath(flags, args)
	if !ok {
		return 1
	}
	interpreter, compiled, err := loadBytecodeFile(ctx, path)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	result, err := interpreter.ExecuteEntrypoint(ctx, compiled, *entrypoint)
	if err != nil {
		fmt.Fprintf(streams.Stderr, errorFormat, err)
		return 1
	}
	if result != nil {
		fmt.Fprintln(streams.Stdout, formatResult(result))
	}
	return 0
}

// parseBytecodePath parses args with flags and returns the single positional file path.
//
// Takes flags (*flag.FlagSet) which holds the subcommand's flag definitions.
// Takes args ([]string) which holds the raw subcommand arguments.
//
// Returns string which is the bytecode file path.
// Returns bool which is false when parsing failed or no path was given.
func parseBytecodePath(flags *flag.FlagSet, args []string) (string, bool) {
	if err := flags.Parse(args); err != nil {
		return "", false
	}
	if flags.NArg() < 1 {
		flags.Usage()
		return "", false
	}
	return flags.Arg(0), true
}

// loadBytecodeFile builds an interpreter backed by the file's directory and loads the
// compiled bytecode at path.
//
// Takes path (string) which is the bytecode file to load.
//
// Returns *pipit.Interpreter which is the interpreter the file was loaded into.
// Returns *pipit.CompiledFileSet which holds the loaded bytecode.
// Returns error when the store cannot be opened or the file cannot be loaded.
func loadBytecodeFile(ctx context.Context, path string) (*pipit.Interpreter, *pipit.CompiledFileSet, error) {
	store, err := pipit.NewDirectoryBytecodeStore(filepath.Dir(path))
	if err != nil {
		return nil, nil, err
	}
	interpreter := runner.New(runner.Limits{
		BytecodeStore: store, Symbols: ExtraSymbols(ctx), Logger: loggerFromContext(ctx),
		Timeout: 0, MaxAlloc: 0, MaxCallDepth: 0, MaxOutputSize: 0, CostBudget: 0, MaxGoroutines: 0})
	compiled, err := pipit.LoadCompiledFromFile(ctx, interpreter, path)
	if err != nil {
		return nil, nil, err
	}
	return interpreter, compiled, nil
}
