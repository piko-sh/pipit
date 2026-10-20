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
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// parseStatus discriminates between successful parse, help request, and error so each
// subcommand can map them to the right process exit code.
type parseStatus int

const (
	// parseOK means parsing completed successfully and the command should run.
	parseOK parseStatus = iota

	// parseHelp means the user asked for help and the command should exit with status 0
	// without running.
	parseHelp

	// parseError means parsing failed; the caller should exit with a non-zero status.
	parseError
)

const (
	// errFlagRequiresValueFmt is the format string used when a required flag value is
	// missing from the command line.
	errFlagRequiresValueFmt = "%s requires a value\n"

	// defaultManifestName is the manifest file used when Config.DefaultManifest is empty.
	defaultManifestName = "pipit-symbols.yaml"

	// defaultInitPackage is the package name init records when Config.InitPackage is empty.
	defaultInitPackage = "symbols"

	// defaultInitDirectory is the output directory init records when Config.InitDirectory is
	// empty.
	defaultInitDirectory = "symbols"

	// includeProjectFlag includes the project's packages as required imports for one
	// run.
	includeProjectFlag = "--include-project"
)

// Config adapts RunCommand to the command-line tool that embeds it.
type Config struct {
	// AlreadyProvided returns the import paths the host already has symbol tables for, so
	// discovery does not report them. Nil provides nothing.
	AlreadyProvided func() []string

	// ToolName is how the command is invoked, for example "pipit extract". It prefixes the
	// help text and generated-file headers; empty selects DefaultTool.
	ToolName string

	// DefaultManifest is the manifest path used when --manifest is not given. Empty selects
	// "pipit-symbols.yaml".
	DefaultManifest string

	// InitPackage is the package name init records when --package is not given. Empty
	// selects "symbols".
	InitPackage string

	// InitDirectory is the output directory init records when --dir is not given. Empty
	// selects "symbols".
	InitDirectory string

	// SourceDirs are the project directories discovery scans. Empty scans the whole project
	// root.
	SourceDirs []string

	// FileFilter decides which Go files the interpreter compiles, as
	// DiscoverOptions.FileFilter does. Nil selects DefaultFileFilter.
	FileFilter FileFilter

	// Scanners read the imports of project source files. Empty selects GoFileScanner.
	Scanners []SourceScanner

	// IgnoreProjectModule drops imports of the project's own module from discovery, for a
	// host that compiles those packages from source instead of registering them.
	IgnoreProjectModule bool

	// FollowProjectImports discovers imports used by the project's own packages. See
	// DiscoverOptions.FollowProjectImports for traversal rules.
	FollowProjectImports bool
}

// extractSubcommandHandler is the contract implemented by every extract subcommand: parse
// its own flags, do its job, and return a process exit code.
type extractSubcommandHandler func(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int

var (
	// extractSubcommands maps the subcommand name to its handler.
	extractSubcommands = map[string]extractSubcommandHandler{
		"generate": runGenerate,
		"discover": runDiscover,
		"init":     runInit,
		"check":    runCheck,
	}
)

// RunCommand runs the extract command, dispatching to the named subcommand. With no
// arguments it prints the help and exits 0 so users can discover the subcommands.
//
// Takes arguments ([]string) which are the command-line arguments after "extract".
// Takes stdout (io.Writer) which receives normal output.
// Takes stderr (io.Writer) which receives errors and warnings.
// Takes config (Config) which adapts the command to the embedding tool.
//
// Returns int which is the exit code: 0 on success, 1 on error.
func RunCommand(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int {
	config = config.withDefaults()
	if len(arguments) == 0 || arguments[0] == "-h" || arguments[0] == "--help" {
		commandUsage(stdout, config)
		return 0
	}

	handler, ok := extractSubcommands[arguments[0]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "Unknown subcommand: %s\n\n", arguments[0])
		commandUsage(stderr, config)
		return 1
	}
	return handler(ctx, arguments[1:], stdout, stderr, config)
}

// withDefaults fills the empty fields of the configuration.
//
// Returns Config which has every defaultable field set.
func (config Config) withDefaults() Config {
	if config.ToolName == "" {
		config.ToolName = DefaultTool
	}
	if config.DefaultManifest == "" {
		config.DefaultManifest = defaultManifestName
	}
	if config.InitPackage == "" {
		config.InitPackage = defaultInitPackage
	}
	if config.InitDirectory == "" {
		config.InitDirectory = defaultInitDirectory
	}
	return config
}

// discoverOptions builds the discovery options for a project root.
//
// Takes root (string) which is the project root.
// Takes extraIgnored ([]string) which are user-supplied exclusions.
//
// Returns DiscoverOptions which applies the configured directories, scanners and provided
// set.
// Returns error when the project module must be ignored but go.mod cannot be read. A
// missing go.mod, or one without a module path, leaves nothing to ignore.
func (config Config) discoverOptions(root string, extraIgnored []string) (DiscoverOptions, error) {
	options := DiscoverOptions{
		Root:                 root,
		SourceDirs:           config.SourceDirs,
		Scanners:             config.Scanners,
		ExtraIgnored:         extraIgnored,
		IgnoredPrefixes:      nil,
		AlreadyProvided:      nil,
		FileFilter:           config.FileFilter,
		BuildTags:            nil,
		FollowProjectImports: config.FollowProjectImports,
	}
	if config.AlreadyProvided != nil {
		options.AlreadyProvided = config.AlreadyProvided()
	}
	if !config.IgnoreProjectModule {
		return options, nil
	}
	modulePath, err := projectModulePath(root)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNoModulePath):
		return options, nil
	case err != nil:
		return DiscoverOptions{}, err
	}
	options.IgnoredPrefixes = []string{modulePath}
	return options, nil
}

// includingProject returns the configuration for a run that reports the project's own
// packages as required and does not follow their imports, as --include-project asks.
//
// Returns Config which neither ignores nor follows the project module.
func (config Config) includingProject() Config {
	config.IgnoreProjectModule = false
	config.FollowProjectImports = false
	return config
}

// projectModulePath returns the module path declared by root/go.mod.
//
// Takes root (string) which is the project root.
//
// Returns string which is the module path.
// Returns error when go.mod cannot be read, is too large, or declares no module path.
func projectModulePath(root string) (modulePath string, err error) {
	projectRoot, err := os.OpenRoot(root)
	if err != nil {
		return "", fmt.Errorf("opening project root %s: %w", root, err)
	}
	defer func() {
		if closeErr := projectRoot.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	data, err := readBoundedFile(projectRoot, goModFileName, maxGoModSizeBytes)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filepath.Join(root, goModFileName), err)
	}
	modulePath = modfile.ModulePath(data)
	if modulePath == "" {
		return "", fmt.Errorf("%w: %s", errNoModulePath, filepath.Join(root, goModFileName))
	}
	return modulePath, nil
}

// commandUsage writes the usage information for the extract command, listing the
// subcommands in a stable order.
//
// Takes w (io.Writer) which receives the usage text.
// Takes config (Config) which names the tool and its manifest.
func commandUsage(w io.Writer, config Config) {
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, `Usage: %s <subcommand> [flags]

Work with the symbol tables the interpreter uses to import native Go
packages.

Subcommands:
`, config.ToolName)

	names := make([]string, 0, len(extractSubcommands))
	for name := range extractSubcommands {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		_, _ = fmt.Fprintf(&builder, "  %-10s %s\n", name, subcommandSummary(name, config))
	}

	_, _ = fmt.Fprintf(&builder, `
Run "%s <subcommand> --help" for details on any subcommand.
`, config.ToolName)

	_, _ = fmt.Fprint(w, builder.String())
}

// subcommandSummary returns a one-line summary for the parent help output.
//
// Takes name (string) which is the subcommand name.
// Takes config (Config) which names the manifest.
//
// Returns string describing the subcommand briefly.
func subcommandSummary(name string, config Config) string {
	switch name {
	case "generate":
		return "Generate reflect-based symbol files from " + config.DefaultManifest
	case "discover":
		return "Walk the project and report packages that need registering"
	case "init":
		return "Create " + config.DefaultManifest + " by discovering project imports"
	case "check":
		return "Verify " + config.DefaultManifest + " against the current project imports"
	default:
		return ""
	}
}

// flagValue reads the value that follows a flag.
//
// Takes arguments ([]string) which is the argument list.
// Takes index (*int) which points at the flag and is advanced past its value.
// Takes stderr (io.Writer) which receives the error when the value is missing.
//
// Returns string which is the value.
// Returns bool which is false when the value is missing.
func flagValue(arguments []string, index *int, stderr io.Writer) (string, bool) {
	if *index+1 >= len(arguments) {
		_, _ = fmt.Fprintf(stderr, errFlagRequiresValueFmt, arguments[*index])
		return "", false
	}
	*index++
	return arguments[*index], true
}

// includeProjectUsage returns the help line for --include-project, which depends on how
// the configuration treats the project module, or nothing when it neither ignores nor
// follows it, so the flag changes nothing.
//
// Takes config (Config) which says how the project module is treated.
//
// Returns string which is the help line with its trailing newline, or empty.
func includeProjectUsage(config Config) string {
	switch {
	case config.FollowProjectImports:
		return "  " + includeProjectFlag + "    Report the project's own packages as required instead of\n" +
			"                       compiling them from source and following their imports\n"
	case config.IgnoreProjectModule:
		return "  " + includeProjectFlag + "    Report the project's own packages as required instead of\n" +
			"                       leaving them out because they are compiled from source\n"
	default:
		return ""
	}
}

// reportDiscoverWarnings writes the unresolved imports and general warnings of a
// discovery to stderr.
//
// Takes stderr (io.Writer) which receives the warnings.
// Takes result (DiscoverResult) which carries them.
func reportDiscoverWarnings(stderr io.Writer, result DiscoverResult) {
	for _, unresolved := range result.Unresolved {
		_, _ = fmt.Fprintf(stderr, "warning: %s cannot be extracted: %s\n", unresolved.ImportPath, unresolved.Reason)
	}
	for _, warning := range result.Warnings {
		_, _ = fmt.Fprintf(stderr, "warning: %s\n", warning)
	}
}
