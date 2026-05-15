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
	"fmt"
	"io"
	"strings"
)

// checkFlags holds the parsed flags for the check subcommand.
type checkFlags struct {
	// manifest is the manifest path to validate.
	manifest string

	// root is the project root directory to discover from.
	root string
}

// runCheck verifies that the manifest covers every package the project needs. Missing
// entries produce exit code 1 so the command can gate CI.
//
// Takes arguments ([]string) which are the subcommand arguments.
// Takes stdout (io.Writer) which receives summary output.
// Takes stderr (io.Writer) which receives drift reports.
// Takes config (Config) which supplies the default manifest, scanners and provided set.
//
// Returns int which is the exit code: 0 when aligned, 1 on drift or error.
func runCheck(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int {
	flags, status := parseCheckArgs(arguments, stderr, config)
	if status != parseOK {
		if status == parseHelp {
			checkUsage(stdout, config)
			return 0
		}
		return 1
	}

	manifest, err := LoadManifest(flags.manifest)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error loading manifest: %s\n", err)
		return 1
	}

	result, err := Discover(ctx, config.discoverOptions(flags.root, nil))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error discovering project: %s\n", err)
		return 1
	}

	return reportCheckResult(stdout, stderr, flags.manifest, manifest, result, config)
}

// reportCheckResult computes the manifest-versus-project diff, reports warnings, and
// returns the exit code. It is separate from runCheck so it can be tested with
// synthesised manifests and results.
//
// Takes stdout (io.Writer) which receives the up-to-date summary on success.
// Takes stderr (io.Writer) which receives warnings and the missing package list on drift.
// Takes manifestPath (string) which is the path shown in messages.
// Takes manifest (*Manifest) which is the declared set.
// Takes result (DiscoverResult) which is the discovered required set.
// Takes config (Config) which names the tool in the hint.
//
// Returns int which is the exit code: 0 when the manifest covers every required package,
// 1 when packages are missing.
func reportCheckResult(stdout, stderr io.Writer, manifestPath string, manifest *Manifest, result DiscoverResult, config Config) int {
	diff := Diff(manifest, result)

	for _, path := range result.SkippedCgo {
		_, _ = fmt.Fprintf(stderr, "warning: %s uses cgo and cannot be interpreted\n", path)
	}
	for _, path := range result.GenericCandidates {
		_, _ = fmt.Fprintf(stderr, "warning: %s exports generic types; a manual generic: block is required\n", path)
	}
	for _, path := range diff.Unused {
		_, _ = fmt.Fprintf(stderr, "warning: %s is declared in %s but not used by the project\n", path, manifestPath)
	}

	if len(diff.Missing) == 0 {
		_, _ = fmt.Fprintf(stdout, "%s is up-to-date (%d package(s)).\n", manifestPath, len(manifest.Packages))
		return 0
	}

	_, _ = fmt.Fprintf(stderr, "\n%s is missing %d package(s):\n", manifestPath, len(diff.Missing))
	for _, path := range diff.Missing {
		_, _ = fmt.Fprintf(stderr, "  - %s\n", path)
	}
	_, _ = fmt.Fprintf(stderr, "\nhint: add the missing paths to %s and run \"%s generate\"\n", manifestPath, config.ToolName)
	return 1
}

// parseCheckArgs parses the command-line flags for check.
//
// Takes arguments ([]string) which is the raw argument list.
// Takes stderr (io.Writer) which receives usage and diagnostics.
// Takes config (Config) which supplies the default manifest.
//
// Returns the parsed flags and a parseStatus describing the outcome.
func parseCheckArgs(arguments []string, stderr io.Writer, config Config) (checkFlags, parseStatus) {
	flags := checkFlags{
		manifest: config.DefaultManifest,
		root:     ".",
	}

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		if !strings.HasPrefix(argument, "-") {
			_, _ = fmt.Fprintf(stderr, "Unexpected argument: %s\n\n", argument)
			checkUsage(stderr, config)
			return flags, parseError
		}

		var ok bool
		switch argument {
		case "-h", "--help":
			return flags, parseHelp
		case "--manifest":
			flags.manifest, ok = flagValue(arguments, &i, stderr)
		case "--root":
			flags.root, ok = flagValue(arguments, &i, stderr)
		default:
			_, _ = fmt.Fprintf(stderr, "Unknown flag: %s\n\n", argument)
			checkUsage(stderr, config)
			return flags, parseError
		}
		if !ok {
			return flags, parseError
		}
	}

	return flags, parseOK
}

// checkUsage writes the usage information for the check subcommand.
//
// Takes w (io.Writer) which receives the usage text.
// Takes config (Config) which names the tool and its default manifest.
func checkUsage(w io.Writer, config Config) {
	_, _ = fmt.Fprintf(w, `Usage: %[1]s check [flags]

Compare %[2]s against the current project and exit non-zero
when the manifest is missing required packages.

Intended for CI: a green check means "%[1]s generate" will
produce a registry that matches the project sources. Manifest
entries that aren't required by the project are reported as
warnings but do not fail the check.

Flags:
  --manifest <path>    Path to YAML manifest (default: %[2]s)
  --root <dir>         Project root directory (default: .)
  -h, --help           Show this help message

`, config.ToolName, config.DefaultManifest)
}
