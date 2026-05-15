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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var testConfig = Config{ToolName: "tool extract", DefaultManifest: "tool-symbols.yaml"}.withDefaults()

func runTestCommand(arguments ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := RunCommand(context.Background(), arguments, &stdout, &stderr, testConfig)
	return code, stdout.String(), stderr.String()
}

func TestRunCommandNoSubcommandPrintsHelp(t *testing.T) {
	t.Parallel()

	code, stdout, stderr := runTestCommand()

	require.Equal(t, 0, code, "help output should exit 0")
	require.Empty(t, stderr, "no error output when showing help")
	require.Contains(t, stdout, "Usage: tool extract <subcommand>")
	for _, name := range []string{"generate", "discover", "init", "check"} {
		require.Containsf(t, stdout, name, "help must list subcommand %s", name)
	}
}

func TestRunCommandHelpFlagPrintsHelp(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-h", "--help"} {
		t.Run(strings.TrimLeft(flag, "-"), func(t *testing.T) {
			t.Parallel()
			code, stdout, _ := runTestCommand(flag)
			require.Equal(t, 0, code)
			require.Contains(t, stdout, "Subcommands:")
		})
	}
}

func TestRunCommandUnknownSubcommandFails(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTestCommand("bogus")

	require.Equal(t, 1, code)
	require.Contains(t, stderr, "Unknown subcommand: bogus")
	require.Contains(t, stderr, "Subcommands:")
}

func TestRunCommandSubcommandHelpNamesTheTool(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"generate", "discover", "init", "check"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, stdout, _ := runTestCommand(name, "--help")
			require.Equal(t, 0, code)
			require.Contains(t, stdout, "Usage: tool extract "+name)
		})
	}
}

func TestRunCommandDefaultsNameTheTool(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := RunCommand(context.Background(), []string{"generate", "--help"}, &stdout, &stderr, Config{})

	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "Usage: "+DefaultTool+" generate")
	require.Contains(t, stdout.String(), "default: pipit-symbols.yaml")
}

func TestRunGenerateMissingManifestFails(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTestCommand("generate", "--manifest", "/nonexistent/tool-symbols.yaml")

	require.Equal(t, 1, code)
	require.Contains(t, stderr, "Error")
}

func TestRunGenerateRejectsUnknownEmit(t *testing.T) {
	t.Parallel()

	code, _, stderr := runTestCommand("generate", "--emit", "everything")

	require.Equal(t, 1, code)
	require.Contains(t, stderr, "Unknown --emit value: everything")
}

func writeTestManifest(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool-symbols.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o644))
	return path
}

func TestRunGenerateListPrintsTheSelection(t *testing.T) {
	t.Parallel()

	manifest := writeTestManifest(t, "package: symbols\noutput: symbols\npackages:\n  - crypto\n  - crypto/aes\n  - cryptobyte\n  - strings\n")

	code, stdout, stderr := runTestCommand("generate", "--manifest", manifest, "--select", "crypto", "--list")

	require.Equal(t, 0, code, stderr)
	require.Equal(t, "crypto\ncrypto/aes\n", stdout, "--select keeps a prefix and the paths below it, not a longer name")
}

func TestRunGenerateSelectMatchingNothingFails(t *testing.T) {
	t.Parallel()

	manifest := writeTestManifest(t, "package: symbols\noutput: symbols\npackages:\n  - strings\n")

	code, _, stderr := runTestCommand("generate", "--manifest", manifest, "--select", "net", "--list")

	require.Equal(t, 1, code)
	require.Contains(t, stderr, "matched no package")
}

func TestRunGenerateWritesStampedFiles(t *testing.T) {
	t.Parallel()

	output := filepath.Join(t.TempDir(), "out", "symbols")
	manifest := writeTestManifest(t, "package: symbols\noutput: "+output+"\npackages:\n  - strings\n")

	code, stdout, stderr := runTestCommand("generate", "--manifest", manifest)
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stdout, "Done.")

	for _, name := range []string{registerFileName, "gen_strings.go", TypesLoaderFileName, TypesLoaderWASMFileName, TypesManifestFileName, typesDescriptorFileName} {
		data, err := os.ReadFile(filepath.Join(output, name))
		require.NoErrorf(t, err, "generate must write %s", name)
		if strings.HasSuffix(name, ".go") {
			require.Truef(t, strings.HasPrefix(string(data), "// Code generated by 'tool extract'. DO NOT EDIT."),
				"%s must name the tool that generated it", name)
		}
	}
	_, err := os.Stat(filepath.Join(output, TypesExportDirectory, "strings"+TypesExportBlobSuffix))
	require.NoError(t, err, "generate must write the export blob")
}

func TestRunGenerateIndexOnlyWritesTheIndex(t *testing.T) {
	t.Parallel()

	output := filepath.Join(t.TempDir(), "index")
	manifest := writeTestManifest(t, "package: index\noutput: "+output+"\npackages:\n  - strings\n  - bytes\n")

	code, _, stderr := runTestCommand("generate", "--manifest", manifest, "--emit", "index")
	require.Equal(t, 0, code, stderr)

	entries, err := os.ReadDir(output)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, IndexFileName, entries[0].Name())
}

func TestRunGenerateDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	output := filepath.Join(t.TempDir(), "dry")
	manifest := writeTestManifest(t, "package: symbols\noutput: "+output+"\npackages:\n  - strings\n")

	code, stdout, stderr := runTestCommand("generate", "--manifest", manifest, "--dry-run")
	require.Equal(t, 0, code, stderr)
	require.Contains(t, stdout, "Dry run complete")

	_, err := os.Stat(output)
	require.True(t, os.IsNotExist(err), "a dry run must not create the output directory")
}

func TestRunDiscoverIgnoresTheProjectModule(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":                 fixtureModule,
		"pkg/helpers/helpers.go": "package helpers\n\nfunc Double(x int) int { return x * 2 }\n",
		"main.go":                "package main\n\nimport (\n\t\"expvar\"\n\t\"example.com/fixture/pkg/helpers\"\n)\n\nvar _ = expvar.Do\nvar _ = helpers.Double\n",
	})

	config := testConfig
	config.IgnoreProjectModule = true
	var stdout, stderr bytes.Buffer
	code := RunCommand(context.Background(), []string{"discover", "--root", root}, &stdout, &stderr, config)

	require.Equal(t, 0, code, stderr.String())
	require.Equal(t, "expvar\n", stdout.String())
}

func TestRunCheckUsesTheProvidedSet(t *testing.T) {
	t.Parallel()

	root := writeFixtureProject(t, fixtureFiles{
		"go.mod":  fixtureModule,
		"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n",
	})
	manifest := writeTestManifest(t, "package: symbols\noutput: symbols\npackages:\n  - strings\n")

	config := testConfig
	config.AlreadyProvided = func() []string { return []string{"fmt"} }
	var stdout, stderr bytes.Buffer
	code := RunCommand(context.Background(), []string{"check", "--root", root, "--manifest", manifest}, &stdout, &stderr, config)

	require.Equal(t, 0, code, "a provided package is not missing from the manifest: %s", stderr.String())
}

func TestReportCheckResultGreenPath(t *testing.T) {
	t.Parallel()

	manifest := &Manifest{
		Packages: []PackageConfig{
			{ImportPath: "github.com/a/one"},
			{ImportPath: "github.com/b/two"},
		},
	}
	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/b/two"},
	}

	var stdout, stderr bytes.Buffer
	code := reportCheckResult(&stdout, &stderr, "tool-symbols.yaml", manifest, result, testConfig)

	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "tool-symbols.yaml is up-to-date (2 package(s)).")
	require.Empty(t, stderr.String())
}

func TestReportCheckResultExitsOneOnMissing(t *testing.T) {
	t.Parallel()

	manifest := &Manifest{
		Packages: []PackageConfig{
			{ImportPath: "github.com/a/one"},
		},
	}
	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/new/gap", "github.com/extra/missing"},
	}

	var stdout, stderr bytes.Buffer
	code := reportCheckResult(&stdout, &stderr, "tool-symbols.yaml", manifest, result, testConfig)

	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "is missing 2 package(s)")
	require.Contains(t, stderr.String(), "  - github.com/extra/missing")
	require.Contains(t, stderr.String(), "  - github.com/new/gap")
	require.Contains(t, stderr.String(), `run "tool extract generate"`)
	require.Empty(t, stdout.String())
}

func TestReportCheckResultWarnsOnUnusedButExitsZero(t *testing.T) {
	t.Parallel()

	manifest := &Manifest{
		Packages: []PackageConfig{
			{ImportPath: "github.com/a/one"},
			{ImportPath: "github.com/stale/unused"},
		},
	}
	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one"},
	}

	var stdout, stderr bytes.Buffer
	code := reportCheckResult(&stdout, &stderr, "tool-symbols.yaml", manifest, result, testConfig)

	require.Equal(t, 0, code, "unused entries must not fail the check")
	require.Contains(t, stdout.String(), "is up-to-date")
	require.Contains(t, stderr.String(), "github.com/stale/unused")
	require.Contains(t, stderr.String(), "not used by the project")
}

func TestReportCheckResultWarnsOnCgoAndGeneric(t *testing.T) {
	t.Parallel()

	manifest := &Manifest{
		Packages: []PackageConfig{
			{ImportPath: "github.com/a/one"},
		},
	}
	result := DiscoverResult{
		RequiredImports:   []string{"github.com/a/one"},
		SkippedCgo:        []string{"github.com/example/cgo"},
		GenericCandidates: []string{"github.com/example/generic"},
	}

	var stdout, stderr bytes.Buffer
	code := reportCheckResult(&stdout, &stderr, "tool-symbols.yaml", manifest, result, testConfig)

	require.Equal(t, 0, code)
	require.Contains(t, stderr.String(), "github.com/example/cgo")
	require.Contains(t, stderr.String(), "uses cgo")
	require.Contains(t, stderr.String(), "github.com/example/generic")
	require.Contains(t, stderr.String(), "exports generic types")
}

func TestRenderDiscoverResultListFormat(t *testing.T) {
	t.Parallel()

	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/b/two"},
	}

	var stdout, stderr bytes.Buffer
	require.NoError(t, renderDiscoverResult(&stdout, &stderr, "list", result, testConfig))

	require.Equal(t, "github.com/a/one\ngithub.com/b/two\n", stdout.String())
	require.Empty(t, stderr.String())
}

func TestRenderDiscoverResultYAMLFormat(t *testing.T) {
	t.Parallel()

	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/b/two"},
	}

	var stdout, stderr bytes.Buffer
	require.NoError(t, renderDiscoverResult(&stdout, &stderr, "yaml", result, testConfig))

	out := stdout.String()
	require.True(t, strings.HasPrefix(out, "packages:\n"), "yaml output must start with packages: header")
	require.Contains(t, out, "  - github.com/a/one")
	require.Contains(t, out, "  - github.com/b/two")
}

func TestRenderDiscoverResultJSONFormat(t *testing.T) {
	t.Parallel()

	result := DiscoverResult{
		RequiredImports:   []string{"github.com/a/one"},
		SkippedCgo:        []string{"github.com/example/cgo"},
		GenericCandidates: []string{"github.com/example/generic"},
	}

	var stdout, stderr bytes.Buffer
	require.NoError(t, renderDiscoverResult(&stdout, &stderr, "json", result, testConfig))

	out := stdout.String()
	require.Contains(t, out, `"RequiredImports"`)
	require.Contains(t, out, `"SkippedCgo"`)
	require.Contains(t, out, `"GenericCandidates"`)
	require.Contains(t, out, "github.com/a/one")
}

func TestRenderDiscoverResultRoutesWarningsToStderr(t *testing.T) {
	t.Parallel()

	result := DiscoverResult{
		RequiredImports:   []string{"github.com/a/one"},
		SkippedCgo:        []string{"github.com/example/cgo"},
		GenericCandidates: []string{"github.com/example/generic"},
	}

	var stdout, stderr bytes.Buffer
	require.NoError(t, renderDiscoverResult(&stdout, &stderr, "list", result, testConfig))

	require.Contains(t, stdout.String(), "github.com/a/one")
	require.NotContains(t, stdout.String(), "cgo")

	require.Contains(t, stderr.String(), "github.com/example/cgo")
	require.Contains(t, stderr.String(), "uses cgo")
	require.Contains(t, stderr.String(), "github.com/example/generic")
	require.Contains(t, stderr.String(), "generic types; add a manual generic: block in tool-symbols.yaml")
}

func TestRenderDiscoverResultUnknownFormatErrors(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := renderDiscoverResult(&stdout, &stderr, "xml", DiscoverResult{}, testConfig)

	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported format")
}

func TestSplitIgnoreList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "github.com/a/one", want: []string{"github.com/a/one"}},
		{name: "multiple", in: "github.com/a/one,github.com/b/two", want: []string{"github.com/a/one", "github.com/b/two"}},
		{name: "whitespace trimmed", in: "  github.com/a/one  , github.com/b/two  ", want: []string{"github.com/a/one", "github.com/b/two"}},
		{name: "empty entries dropped", in: "a,,b,", want: []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, splitIgnoreList(tt.in))
		})
	}
}

func openTestRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	directory := t.TempDir()
	root, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	return root, directory
}

func TestWriteInitManifestWritesFreshFile(t *testing.T) {
	t.Parallel()

	root, _ := openTestRoot(t)
	flags := initFlags{
		output:       "tool-symbols.yaml",
		packageName:  "project_symbols",
		generatedDir: "internal/symbols",
	}
	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/b/two"},
	}

	var stdout, stderr bytes.Buffer
	code := writeInitManifest(&stdout, &stderr, root, "tool-symbols.yaml", flags, result, testConfig)

	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "Wrote tool-symbols.yaml with 2 package(s).")
	require.Empty(t, stderr.String())

	written, err := root.ReadFile("tool-symbols.yaml")
	require.NoError(t, err)
	require.Contains(t, string(written), "# Generated by `tool extract init`.")
	require.Contains(t, string(written), "package: project_symbols")
	require.Contains(t, string(written), "output: internal/symbols")
	require.Contains(t, string(written), "  - github.com/a/one")
	require.Contains(t, string(written), "  - github.com/b/two")
}

func TestWriteInitManifestRefusesExistingFileWithoutForce(t *testing.T) {
	t.Parallel()

	root, _ := openTestRoot(t)
	require.NoError(t, root.WriteFile("tool-symbols.yaml", []byte("existing"), 0o644))

	flags := initFlags{output: "tool-symbols.yaml"}
	result := DiscoverResult{RequiredImports: []string{"github.com/example/pkg"}}

	var stdout, stderr bytes.Buffer
	code := writeInitManifest(&stdout, &stderr, root, "tool-symbols.yaml", flags, result, testConfig)

	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "already exists; use --force to overwrite")
	require.Empty(t, stdout.String())

	kept, err := root.ReadFile("tool-symbols.yaml")
	require.NoError(t, err)
	require.Equal(t, "existing", string(kept), "existing manifest must not be overwritten")
}

func TestWriteInitManifestForceOverwrites(t *testing.T) {
	t.Parallel()

	root, _ := openTestRoot(t)
	require.NoError(t, root.WriteFile("tool-symbols.yaml", []byte("old content"), 0o644))

	flags := initFlags{
		output:       "tool-symbols.yaml",
		packageName:  "project_symbols",
		generatedDir: "internal/symbols",
		force:        true,
	}
	result := DiscoverResult{RequiredImports: []string{"github.com/example/pkg"}}

	var stdout, stderr bytes.Buffer
	code := writeInitManifest(&stdout, &stderr, root, "tool-symbols.yaml", flags, result, testConfig)

	require.Equal(t, 0, code)

	written, err := root.ReadFile("tool-symbols.yaml")
	require.NoError(t, err)
	require.NotEqual(t, "old content", string(written))
	require.Contains(t, string(written), "github.com/example/pkg")
}

func TestWriteInitManifestReportsCgoAndGenericWarnings(t *testing.T) {
	t.Parallel()

	root, _ := openTestRoot(t)
	flags := initFlags{
		output:       "tool-symbols.yaml",
		packageName:  "project_symbols",
		generatedDir: "internal/symbols",
	}
	result := DiscoverResult{
		RequiredImports:   []string{"github.com/example/ok"},
		SkippedCgo:        []string{"github.com/example/cgo"},
		GenericCandidates: []string{"github.com/example/generic"},
	}

	var stdout, stderr bytes.Buffer
	code := writeInitManifest(&stdout, &stderr, root, "tool-symbols.yaml", flags, result, testConfig)

	require.Equal(t, 0, code)
	require.Contains(t, stderr.String(), "github.com/example/cgo")
	require.Contains(t, stderr.String(), "uses cgo")
	require.Contains(t, stderr.String(), "github.com/example/generic")
	require.Contains(t, stderr.String(), "generic types")
}

func TestRenderInitManifestRoundTripsThroughLoadManifest(t *testing.T) {
	t.Parallel()

	root, directory := openTestRoot(t)
	flags := initFlags{
		output:       "tool-symbols.yaml",
		packageName:  "project_symbols",
		generatedDir: "internal/symbols",
	}
	result := DiscoverResult{
		RequiredImports: []string{"github.com/a/one", "github.com/b/two", "example.com/c/three"},
	}

	var stdout, stderr bytes.Buffer
	code := writeInitManifest(&stdout, &stderr, root, "tool-symbols.yaml", flags, result, testConfig)
	require.Equal(t, 0, code)

	loaded, err := LoadManifest(filepath.Join(directory, "tool-symbols.yaml"))
	require.NoError(t, err, "rendered YAML must parse back through LoadManifest")
	require.Equal(t, flags.packageName, loaded.Package)
	require.Equal(t, flags.generatedDir, loaded.Output)
	require.Len(t, loaded.Packages, len(result.RequiredImports))
}
