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

package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// discoverFlags holds the parsed flags for the discover subcommand.
type discoverFlags struct {
	// root is the project root directory.
	root string

	// outputFormat controls how the discovered packages are rendered: "list", "yaml", or
	// "json".
	outputFormat string

	// ignore is the comma-separated list of additional import paths to exclude from the
	// result.
	ignore string
}

// runDiscover walks the project, filters out the packages the host already provides, and
// prints the remaining required packages.
//
// Takes arguments ([]string) which are the subcommand arguments.
// Takes stdout (io.Writer) which receives the discovered list.
// Takes stderr (io.Writer) which receives warnings and diagnostics.
// Takes config (Config) which supplies the scanners and provided set.
//
// Returns int which is the exit code: 0 on success, 1 on error.
func runDiscover(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int {
	flags, status := parseDiscoverArgs(arguments, stderr, config)
	if status != parseOK {
		if status == parseHelp {
			discoverUsage(stdout, config)
			return 0
		}
		return 1
	}

	result, err := Discover(ctx, config.discoverOptions(flags.root, splitIgnoreList(flags.ignore)))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}

	if err := renderDiscoverResult(stdout, stderr, flags.outputFormat, result, config); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}
	return 0
}

// parseDiscoverArgs parses the command-line flags for discover.
//
// Takes arguments ([]string) which is the raw argument list.
// Takes stderr (io.Writer) which receives usage and diagnostics.
// Takes config (Config) which names the tool in the usage text.
//
// Returns the parsed flags and a parseStatus describing the outcome.
func parseDiscoverArgs(arguments []string, stderr io.Writer, config Config) (discoverFlags, parseStatus) {
	flags := discoverFlags{
		root:         ".",
		outputFormat: "list",
		ignore:       "",
	}

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		if !strings.HasPrefix(argument, "-") {
			_, _ = fmt.Fprintf(stderr, "Unexpected argument: %s\n\n", argument)
			discoverUsage(stderr, config)
			return flags, parseError
		}

		var ok bool
		switch argument {
		case "-h", "--help":
			return flags, parseHelp
		case "--root":
			flags.root, ok = flagValue(arguments, &i, stderr)
		case "--output":
			flags.outputFormat, ok = flagValue(arguments, &i, stderr)
		case "--ignore":
			flags.ignore, ok = flagValue(arguments, &i, stderr)
		default:
			_, _ = fmt.Fprintf(stderr, "Unknown flag: %s\n\n", argument)
			discoverUsage(stderr, config)
			return flags, parseError
		}
		if !ok {
			return flags, parseError
		}
	}

	switch flags.outputFormat {
	case "list", "yaml", "json":
	default:
		_, _ = fmt.Fprintf(stderr, "Unknown output format: %s (expected list, yaml, or json)\n", flags.outputFormat)
		return flags, parseError
	}

	return flags, parseOK
}

// splitIgnoreList turns a comma-separated ignore list into a trimmed non-empty slice.
//
// Takes raw (string) which is the comma-separated input.
//
// Returns []string with whitespace trimmed and empty entries removed.
func splitIgnoreList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			trimmed = append(trimmed, part)
		}
	}
	return trimmed
}

// renderDiscoverResult prints the discovered result in the requested format. Warnings
// (cgo, generics) always go to stderr.
//
// Takes stdout (io.Writer) which receives the primary output.
// Takes stderr (io.Writer) which receives warnings.
// Takes format (string) which selects "list", "yaml", or "json".
// Takes result (DiscoverResult) which is the discovery output.
// Takes config (Config) which names the manifest in warnings.
//
// Returns error when rendering fails.
func renderDiscoverResult(stdout, stderr io.Writer, format string, result DiscoverResult, config Config) error {
	switch format {
	case "list":
		for _, path := range result.RequiredImports {
			_, _ = fmt.Fprintln(stdout, path)
		}
	case "yaml":
		_, _ = fmt.Fprintln(stdout, "packages:")
		for _, path := range result.RequiredImports {
			_, _ = fmt.Fprintf(stdout, "  - %s\n", path)
		}
	case "json":
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding json: %w", err)
		}
		if _, err := stdout.Write(encoded); err != nil {
			return fmt.Errorf("writing json: %w", err)
		}
		_, _ = fmt.Fprintln(stdout)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}

	for _, path := range result.SkippedCgo {
		_, _ = fmt.Fprintf(stderr, "warning: %s uses cgo and cannot be interpreted\n", path)
	}
	for _, path := range result.GenericCandidates {
		_, _ = fmt.Fprintf(stderr, "warning: %s exports generic types; add a manual generic: block in %s\n", path, config.DefaultManifest)
	}
	return nil
}

// discoverUsage writes the usage information for the discover subcommand.
//
// Takes w (io.Writer) which receives the usage text.
// Takes config (Config) which names the tool.
func discoverUsage(w io.Writer, config Config) {
	_, _ = fmt.Fprintf(w, `Usage: %s discover [flags]

Walk the project's sources, collect the packages they import, and
print the packages that need registering with the interpreter.

Packages the interpreter already provides are filtered out
automatically.

Flags:
  --root <dir>         Project root directory (default: .)
  --output <format>    Output format: list, yaml, or json (default: list)
  --ignore <paths>     Comma-separated import paths to exclude
  -h, --help           Show this help message

`, config.ToolName)
}
