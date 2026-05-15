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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// initFlags holds the parsed flags for the init subcommand.
type initFlags struct {
	// root is the project root directory (default ".").
	root string

	// output is the manifest file path to write.
	output string

	// packageName is the generated-symbols package name to embed in the manifest.
	packageName string

	// generatedDir is the output directory for generate, recorded in the manifest.
	generatedDir string

	// force overwrites an existing manifest when true.
	force bool
}

// runInit discovers the project's required packages and writes them to a new manifest.
//
// Takes arguments ([]string) which are the subcommand arguments.
// Takes stdout (io.Writer) which receives progress messages.
// Takes stderr (io.Writer) which receives warnings and errors.
// Takes config (Config) which supplies the defaults, scanners and provided set.
//
// Returns int which is the exit code: 0 on success, 1 on error.
func runInit(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int {
	flags, status := parseInitArgs(arguments, stderr, config)
	if status != parseOK {
		if status == parseHelp {
			initUsage(stdout, config)
			return 0
		}
		return 1
	}

	directory, filename := manifestLocation(flags.output)
	root, err := os.OpenRoot(directory)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: opening %s: %s\n", directory, err)
		return 1
	}
	defer func() { _ = root.Close() }()

	result, err := Discover(ctx, config.discoverOptions(flags.root, nil))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %s\n", err)
		return 1
	}

	return writeInitManifest(stdout, stderr, root, filename, flags, result, config)
}

// manifestLocation splits the manifest path into its directory and file name.
//
// Takes outputPath (string) which is the destination manifest path.
//
// Returns directory which is the cleaned directory, "." for a bare file name.
// Returns filename which is the file name.
func manifestLocation(outputPath string) (directory, filename string) {
	directory, filename = filepath.Split(outputPath)
	if directory == "" {
		directory = "."
	}
	return filepath.Clean(directory), filename
}

// writeInitManifest performs the I/O and reporting half of init, separated from the
// orchestration so it can be tested with a synthesised DiscoverResult.
//
// Takes stdout (io.Writer) which receives the summary line.
// Takes stderr (io.Writer) which receives warnings and errors.
// Takes root (*os.Root) which confines the write to the manifest's directory.
// Takes filename (string) which is the manifest file name inside root.
// Takes flags (initFlags) which supply the manifest header fields and the --force policy.
// Takes result (DiscoverResult) which supplies the packages to write and any warnings.
// Takes config (Config) which names the tool in the manifest comment.
//
// Returns int which is the exit code: 0 on success, 1 on error.
func writeInitManifest(stdout, stderr io.Writer, root *os.Root, filename string, flags initFlags, result DiscoverResult, config Config) int {
	if !flags.force {
		if _, statErr := root.Stat(filename); statErr == nil {
			_, _ = fmt.Fprintf(stderr, "Error: %s already exists; use --force to overwrite\n", flags.output)
			return 1
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(stderr, "Error: %s\n", statErr)
			return 1
		}
	}

	contents := renderInitManifest(flags, result.RequiredImports, config)
	if err := root.WriteFile(filename, []byte(contents), extractFilePerms); err != nil {
		_, _ = fmt.Fprintf(stderr, "Error writing %s: %s\n", flags.output, err)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "Wrote %s with %d package(s).\n", flags.output, len(result.RequiredImports))
	for _, path := range result.SkippedCgo {
		_, _ = fmt.Fprintf(stderr, "warning: %s uses cgo and cannot be interpreted (not written to manifest)\n", path)
	}
	for _, path := range result.GenericCandidates {
		_, _ = fmt.Fprintf(stderr, "warning: %s exports generic types; edit the manifest to add a generic: block\n", path)
	}
	return 0
}

// parseInitArgs parses the command-line flags for init.
//
// Takes arguments ([]string) which is the raw argument list.
// Takes stderr (io.Writer) which receives usage and diagnostics.
// Takes config (Config) which supplies the defaults.
//
// Returns the parsed flags and a parseStatus describing the outcome.
func parseInitArgs(arguments []string, stderr io.Writer, config Config) (initFlags, parseStatus) {
	flags := initFlags{
		root:         ".",
		output:       config.DefaultManifest,
		packageName:  config.InitPackage,
		generatedDir: config.InitDirectory,
		force:        false,
	}

	valueTargets := map[string]*string{
		"--root":    &flags.root,
		"--output":  &flags.output,
		"--package": &flags.packageName,
		"--dir":     &flags.generatedDir,
	}

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		if !strings.HasPrefix(argument, "-") {
			_, _ = fmt.Fprintf(stderr, "Unexpected argument: %s\n\n", argument)
			initUsage(stderr, config)
			return flags, parseError
		}

		switch argument {
		case "-h", "--help":
			return flags, parseHelp
		case "--force":
			flags.force = true
		default:
			target, known := valueTargets[argument]
			if !known {
				_, _ = fmt.Fprintf(stderr, "Unknown flag: %s\n\n", argument)
				initUsage(stderr, config)
				return flags, parseError
			}
			value, ok := flagValue(arguments, &i, stderr)
			if !ok {
				return flags, parseError
			}
			*target = value
		}
	}

	return flags, parseOK
}

// renderInitManifest produces the YAML for a new manifest with the discovered package
// list, in the format LoadManifest accepts.
//
// Takes flags (initFlags) which supplies the header fields.
// Takes packages ([]string) which is the sorted discovered list.
// Takes config (Config) which names the tool in the leading comment.
//
// Returns string with trailing newline.
func renderInitManifest(flags initFlags, packages []string, config Config) string {
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "# Generated by `%s init`. Edit freely.\n", config.ToolName)
	_, _ = fmt.Fprintf(&builder, "package: %s\n", flags.packageName)
	_, _ = fmt.Fprintf(&builder, "output: %s\n", flags.generatedDir)
	builder.WriteString("packages:\n")
	for _, path := range packages {
		_, _ = fmt.Fprintf(&builder, "  - %s\n", path)
	}
	return builder.String()
}

// initUsage writes the usage information for the init subcommand.
//
// Takes w (io.Writer) which receives the usage text.
// Takes config (Config) which names the tool and its defaults.
func initUsage(w io.Writer, config Config) {
	_, _ = fmt.Fprintf(w, `Usage: %[1]s init [flags]

Discover the project's required imports and write them to a new
%[2]s manifest. Refuses to overwrite an existing file
unless --force is given.

After writing the manifest, run "%[1]s generate" to produce
the reflect-based symbol files.

Flags:
  --root <dir>         Project root directory (default: .)
  --output <path>      Manifest path to write (default: %[2]s)
  --package <name>     Generated-symbols package name
                       (default: %[3]s)
  --dir <path>         Generated-symbols output directory recorded in
                       the manifest (default: %[4]s)
  --force              Overwrite an existing manifest
  -h, --help           Show this help message

`, config.ToolName, config.DefaultManifest, config.InitPackage, config.InitDirectory)
}
