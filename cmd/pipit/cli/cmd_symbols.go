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
	"slices"
	"strings"

	"pipit.sh/pipit/cmd/pipit/internal/output"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
)

// RunSymbols dispatches `pipit symbols <list|show> ...`.
//
// Takes args ([]string) which holds the subcommand and its arguments.
// Takes streams (output.IO) which carries the standard I/O streams.
//
// Returns int which is the exit code: 0 on success, 1 on any failure.
func RunSymbols(ctx context.Context, args []string, streams output.IO) int {
	if len(args) == 0 {
		return runSymbolsList(ctx, nil, streams)
	}
	switch args[0] {
	case "list":
		return runSymbolsList(ctx, args[1:], streams)
	case "show":
		return runSymbolsShow(ctx, args[1:], streams)
	case "-h", "--help", "help":
		fmt.Fprintln(streams.Stdout, "Usage: pipit symbols [list|show <package>]")
		return 0
	default:
		fmt.Fprintf(streams.Stderr, "pipit symbols: unknown subcommand %q\n", args[0])
		return 1
	}
}

// runSymbolsList prints the registered packages and their export counts.
//
// Takes args ([]string) which holds the list subcommand flag arguments.
// Takes streams (output.IO) which carries the standard I/O streams.
//
// Returns int which is the exit code: 0 on success, 1 on any failure.
func runSymbolsList(ctx context.Context, args []string, streams output.IO) int {
	flags := flag.NewFlagSet("pipit symbols list", flag.ContinueOnError)
	flags.SetOutput(streams.Stderr)
	filter := flags.String("filter", "", "Substring filter on package path")
	if err := flags.Parse(args); err != nil {
		return 1
	}

	theme := styles.For(streams.ShouldColour())
	exports := allExports(ctx)
	packages := make([]string, 0, len(exports))
	for path := range exports {
		if *filter != "" && !strings.Contains(strings.ToLower(path), strings.ToLower(*filter)) {
			continue
		}
		packages = append(packages, path)
	}
	slices.Sort(packages)

	fmt.Fprintf(streams.Stdout, "%s %d package(s) registered\n", theme.Title.Render("symbols:"), len(packages))
	for _, path := range packages {
		count := len(exports[path])
		fmt.Fprintf(streams.Stdout, "  %s  %s\n",
			theme.Subtle.Render(fmt.Sprintf("%4d", count)),
			theme.Strong.Render(path),
		)
	}
	return 0
}

// runSymbolsShow prints the exported symbols for one registered package.
//
// Takes args ([]string) which holds the show subcommand arguments.
// Takes streams (output.IO) which carries the standard I/O streams.
//
// Returns int which is the exit code: 0 on success, 1 on any failure.
func runSymbolsShow(ctx context.Context, args []string, streams output.IO) int {
	if len(args) < 1 {
		fmt.Fprintln(streams.Stderr, "Usage: pipit symbols show <package>")
		return 1
	}
	target := args[0]
	exports := allExports(ctx)
	symbols, ok := exports[target]
	if !ok {
		fmt.Fprintf(streams.Stderr, "pipit: package %q is not registered\n", target)
		return 1
	}

	theme := styles.For(streams.ShouldColour())
	names := make([]string, 0, len(symbols))
	for name := range symbols {
		names = append(names, name)
	}
	slices.Sort(names)
	fmt.Fprintf(streams.Stdout, "%s %s (%d symbols)\n",
		theme.Title.Render("package"),
		theme.Strong.Render(target),
		len(names),
	)
	for _, name := range names {
		value := symbols[name]
		fmt.Fprintf(streams.Stdout, "  %s %s\n",
			theme.Strong.Render(name),
			theme.Subtle.Render(value.Type().String()),
		)
	}
	return 0
}
