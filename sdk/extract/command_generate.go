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
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// extractFilePerms is the file permission used when writing generated files.
	extractFilePerms = 0o644

	// extractDirectoryPerms is the permission used when creating generated directories.
	extractDirectoryPerms = 0o755

	// emitAll writes the reflect tables and the gcexportdata unit.
	emitAll = "all"

	// emitSymbols writes only the reflect tables.
	emitSymbols = "symbols"

	// emitTypes writes only the gcexportdata unit.
	emitTypes = "types"

	// emitIndex writes only the import-path index.
	emitIndex = "index"

	// registerFileName is the file declaring the Symbols map the other files populate.
	registerFileName = "gen_register.go"

	// typesDescriptorFileName is the JSON file listing the extracted import paths.
	typesDescriptorFileName = "gen_types_descriptor.json"

	// errorLineFmt is the format for a one-line error report on stderr.
	errorLineFmt = "Error: %s\n"
)

// generateFlags holds the parsed flags for the generate subcommand.
type generateFlags struct {
	// manifest is the path to the YAML manifest file.
	manifest string

	// output overrides the output directory from the manifest.
	output string

	// packageName overrides the package name from the manifest.
	packageName string

	// emit selects which artefacts to write: "symbols" for the reflect tables, "types" for
	// the gcexportdata unit, "index" for the import-path list, "all" for symbols and types.
	emit string

	// selectPrefixes restricts the manifest to packages matching one of these import-path
	// prefixes, so one manifest can drive several output packages. Empty keeps them all.
	selectPrefixes []string

	// list prints the selected import paths and writes nothing, so a caller can check a
	// selection without paying for the type-check of every package in it.
	list bool

	// dryRun prints what would be generated without writing files.
	dryRun bool
}

// emitContext bundles the parameters shared by the file emission helpers.
type emitContext struct {
	// packageConfigs maps import paths to their package-level configuration: generic
	// instantiations and the optional build constraint. Plain entries map to the zero
	// config.
	packageConfigs map[string]PackageConfig

	// manifest describes which packages to extract and where to write output.
	manifest *Manifest

	// output is the output directory, nil in dry-run mode.
	output *os.Root

	// stdout receives normal progress messages.
	stdout io.Writer

	// stderr receives error and diagnostic messages.
	stderr io.Writer

	// generator stamps each generated file with the tool name.
	generator Generator

	// flags holds the parsed command-line flags.
	flags generateFlags
}

// generatedOutput is one file produced by the types loader generation step.
type generatedOutput struct {
	// label names the file in progress output.
	label string

	// name is the file path relative to the output directory.
	name string

	// data is the file content.
	data []byte
}

// runGenerate reads a manifest and writes the reflect-based Go symbol files the
// interpreter uses to import native packages.
//
// Takes arguments ([]string) which are the subcommand arguments.
// Takes stdout (io.Writer) which receives progress messages.
// Takes stderr (io.Writer) which receives errors.
// Takes config (Config) which names the tool and its default manifest.
//
// Returns int which is the exit code: 0 on success, 1 on error.
func runGenerate(ctx context.Context, arguments []string, stdout, stderr io.Writer, config Config) int {
	flags, status := parseGenerateArgs(arguments, stderr, config)
	if status != parseOK {
		if status == parseHelp {
			generateUsage(stdout, config)
			return 0
		}
		return 1
	}

	manifest, err := loadAndApplyManifest(flags)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, errorLineFmt, err)
		return 1
	}

	if flags.list {
		for _, path := range manifest.ImportPaths() {
			_, _ = fmt.Fprintln(stdout, path)
		}
		return 0
	}

	_, _ = fmt.Fprintf(stdout, "Extracting %d packages...\n", len(manifest.Packages))

	packages, err := ExtractContext(ctx, manifest.ImportPaths(), manifest.GenericConfigs())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error extracting packages: %s\n", err)
		return 1
	}

	ec := emitContext{
		manifest:       manifest,
		packageConfigs: manifest.PackageConfigs(),
		flags:          flags,
		stdout:         stdout,
		stderr:         stderr,
		generator:      Generator{Tool: config.ToolName},
		output:         nil,
	}
	if !flags.dryRun {
		output, err := openOutputRoot(manifest.Output)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "Error opening %s: %s\n", manifest.Output, err)
			return 1
		}
		defer func() { _ = output.Close() }()
		ec.output = output
	}

	if err := emitPackageFiles(packages, ec); err != nil {
		return 1
	}

	if flags.dryRun {
		_, _ = fmt.Fprint(stdout, "\nDry run complete. No files were written.\n")
	} else {
		_, _ = fmt.Fprint(stdout, "\nDone.\n")
	}
	return 0
}

// openOutputRoot creates the output directory when needed and opens it as an os.Root, so
// every write stays inside it.
//
// Takes directory (string) which is the output directory.
//
// Returns *os.Root which confines writes to the directory.
// Returns error when the directory cannot be created or opened.
func openOutputRoot(directory string) (*os.Root, error) {
	if err := os.MkdirAll(directory, extractDirectoryPerms); err != nil {
		return nil, err
	}
	return os.OpenRoot(directory)
}

// parseGenerateArgs parses the generate flags.
//
// Takes arguments ([]string) which are the raw command-line arguments.
// Takes stderr (io.Writer) which receives error and usage output.
// Takes config (Config) which supplies the default manifest.
//
// Returns generateFlags which holds the parsed flag values.
// Returns parseStatus which discriminates success, help, and error outcomes.
func parseGenerateArgs(arguments []string, stderr io.Writer, config Config) (generateFlags, parseStatus) {
	flags := generateFlags{
		manifest:       config.DefaultManifest,
		emit:           emitAll,
		output:         "",
		packageName:    "",
		selectPrefixes: nil,
		list:           false,
		dryRun:         false,
	}

	for i := 0; i < len(arguments); i++ {
		argument := arguments[i]
		if !strings.HasPrefix(argument, "-") {
			_, _ = fmt.Fprintf(stderr, "Unexpected argument: %s\n\n", argument)
			generateUsage(stderr, config)
			return flags, parseError
		}

		var ok bool
		switch argument {
		case "-h", "--help":
			return flags, parseHelp
		case "--manifest", "-manifest":
			flags.manifest, ok = flagValue(arguments, &i, stderr)
		case "--output", "-output":
			flags.output, ok = flagValue(arguments, &i, stderr)
		case "--package", "-package":
			flags.packageName, ok = flagValue(arguments, &i, stderr)
		case "--select", "-select":
			var value string
			value, ok = flagValue(arguments, &i, stderr)
			flags.selectPrefixes = append(flags.selectPrefixes, strings.Split(value, ",")...)
		case "--emit", "-emit":
			flags.emit, ok = flagValue(arguments, &i, stderr)
			if ok && !validEmit(flags.emit) {
				_, _ = fmt.Fprintf(stderr, "Unknown --emit value: %s (expected %s, %s, %s or %s)\n\n",
					flags.emit, emitAll, emitSymbols, emitTypes, emitIndex)
				return flags, parseError
			}
		case "--list":
			flags.list, ok = true, true
		case "--dry-run":
			flags.dryRun, ok = true, true
		default:
			_, _ = fmt.Fprintf(stderr, "Unknown flag: %s\n\n", argument)
			generateUsage(stderr, config)
			return flags, parseError
		}
		if !ok {
			return flags, parseError
		}
	}

	return flags, parseOK
}

// validEmit reports whether value is a recognised --emit value.
//
// Takes value (string) which is the flag value.
//
// Returns bool which is true for all, symbols, types and index.
func validEmit(value string) bool {
	switch value {
	case emitAll, emitSymbols, emitTypes, emitIndex:
		return true
	default:
		return false
	}
}

// loadAndApplyManifest loads the manifest and applies the flag overrides.
//
// Takes flags (generateFlags) which holds the manifest path and optional overrides.
//
// Returns *Manifest which is the loaded manifest with overrides applied.
// Returns error when the manifest cannot be loaded or the selection matches nothing.
func loadAndApplyManifest(flags generateFlags) (*Manifest, error) {
	manifest, err := LoadManifest(flags.manifest)
	if err != nil {
		return nil, err
	}

	if flags.output != "" {
		manifest.Output = flags.output
	}
	if flags.packageName != "" {
		manifest.Package = flags.packageName
	}
	if len(flags.selectPrefixes) > 0 {
		manifest.Packages = selectPackages(manifest.Packages, flags.selectPrefixes)
		if len(manifest.Packages) == 0 {
			return nil, fmt.Errorf("--select %s matched no package in %s",
				strings.Join(flags.selectPrefixes, ","), flags.manifest)
		}
	}
	return manifest, nil
}

// selectPackages keeps the configs whose import path equals a prefix or sits below it, so
// "crypto" takes crypto and crypto/aes but not cryptobyte.
//
// Takes configs ([]PackageConfig) which is the manifest's package list.
// Takes prefixes ([]string) which are the import-path prefixes to keep.
//
// Returns the configs that matched, in manifest order.
func selectPackages(configs []PackageConfig, prefixes []string) []PackageConfig {
	kept := make([]PackageConfig, 0, len(configs))
	for i := range configs {
		if hasPathPrefix(configs[i].ImportPath, prefixes) {
			kept = append(kept, configs[i])
		}
	}
	return kept
}

// emitPackageFiles writes the files the --emit selection asks for.
//
// Takes packages ([]ExtractedPackage) which are the extracted packages.
// Takes ec (emitContext) which bundles manifest, configs, flags, output and writers.
//
// Returns error when any generation or write step fails.
func emitPackageFiles(packages []ExtractedPackage, ec emitContext) error {
	if ec.flags.emit == emitIndex {
		return writeIndexFile(packages, ec)
	}

	if ec.flags.emit != emitTypes {
		if err := writeRegisterFile(ec); err != nil {
			return err
		}
		for i := range packages {
			if err := emitSinglePackage(&packages[i], ec); err != nil {
				return err
			}
		}
	}

	importPaths := collectAllImportPaths(packages)
	if ec.flags.emit != emitSymbols && len(importPaths) > 0 {
		if err := writeTypesLoader(packages, ec); err != nil {
			_, _ = fmt.Fprintf(ec.stderr, errorLineFmt, err)
			return err
		}
		if err := writeTypesDescriptor(importPaths, ec); err != nil {
			_, _ = fmt.Fprintf(ec.stderr, errorLineFmt, err)
			return err
		}
	}
	return nil
}

// writeIndexFile emits the import-path index, which names the extracted packages without
// referencing them.
//
// Takes packages ([]ExtractedPackage) which are the extracted packages.
// Takes ec (emitContext) which bundles manifest, flags, output and writers.
//
// Returns error when generation or writing fails.
func writeIndexFile(packages []ExtractedPackage, ec emitContext) error {
	source, err := ec.generator.GenerateIndexFile(collectAllImportPaths(packages), ec.manifest.Package)
	if err != nil {
		_, _ = fmt.Fprintf(ec.stderr, "Error generating index file: %s\n", err)
		return err
	}
	if err := writeGeneratedOutput(generatedOutput{label: "index", name: IndexFileName, data: source}, ec); err != nil {
		_, _ = fmt.Fprintf(ec.stderr, errorLineFmt, err)
		return err
	}
	return nil
}

// writeRegisterFile emits gen_register.go, which declares the package-level Symbols map
// that every other gen_*.go file populates in its init(). Without this file the generated
// package fails to compile.
//
// Takes ec (emitContext) which bundles manifest, flags, output and writers.
//
// Returns error when generation or writing fails.
func writeRegisterFile(ec emitContext) error {
	source, err := ec.generator.GenerateRegisterFile(ec.manifest.Package)
	if err != nil {
		_, _ = fmt.Fprintf(ec.stderr, "Error generating register file: %s\n", err)
		return err
	}
	if err := writeGeneratedOutput(generatedOutput{label: "register", name: registerFileName, data: source}, ec); err != nil {
		_, _ = fmt.Fprintf(ec.stderr, errorLineFmt, err)
		return err
	}
	return nil
}

// emitSinglePackage generates and writes, or dry-run prints, the source file for one
// extracted package.
//
// Takes extractedPackage (*ExtractedPackage) which is the package to process.
// Takes ec (emitContext) which bundles manifest, configs, flags, output and writers.
//
// Returns error when generation or writing fails.
func emitSinglePackage(extractedPackage *ExtractedPackage, ec emitContext) error {
	generatorConfig := ec.packageConfigs[extractedPackage.ImportPath]
	generatorConfig.LinkImportPath = ec.manifest.LinkImportPath
	source, err := ec.generator.GenerateFile(*extractedPackage, ec.manifest.Package, generatorConfig)
	if err != nil {
		_, _ = fmt.Fprintf(ec.stderr, "Error generating %s: %s\n", extractedPackage.ImportPath, err)
		return err
	}

	if source == nil {
		_, _ = fmt.Fprintf(ec.stdout, "  %s: no extractable symbols, skipping\n", extractedPackage.ImportPath)
		return nil
	}

	file := generatedOutput{label: extractedPackage.ImportPath, name: OutputFileName(extractedPackage.ImportPath), data: source}
	if err := writeGeneratedOutput(file, ec); err != nil {
		_, _ = fmt.Fprintf(ec.stderr, errorLineFmt, err)
		return err
	}
	return nil
}

// collectAllImportPaths returns the import paths of all extracted packages, which the
// types descriptor lists so build tooling can load the same package set.
//
// Takes packages ([]ExtractedPackage) which are the extracted packages.
//
// Returns []string which lists all package import paths.
func collectAllImportPaths(packages []ExtractedPackage) []string {
	paths := make([]string, 0, len(packages))
	for i := range packages {
		paths = append(paths, packages[i].ImportPath)
	}
	return paths
}

// writeTypesLoader writes the types loader sources, the types manifest and one
// gzip-compressed gcexportdata blob per extracted package. Together they let the
// generated package rebuild complete types.Package values at run time without a Go
// toolchain.
//
// Takes packages ([]ExtractedPackage) which carry the loaded types packages.
// Takes ec (emitContext) which bundles manifest, flags, output and writers.
//
// Returns error when generation or writing of any loader file fails.
func writeTypesLoader(packages []ExtractedPackage, ec emitContext) error {
	blobs, err := GenerateTypesExportBlobs(packages)
	if err != nil {
		return fmt.Errorf("encoding types export data: %w", err)
	}
	loaderSource, err := ec.generator.GenerateTypesLoaderFile(ec.manifest.Package)
	if err != nil {
		return fmt.Errorf("generating types_loader: %w", err)
	}
	wasmSource, err := ec.generator.GenerateTypesLoaderWASMFile(ec.manifest.Package)
	if err != nil {
		return fmt.Errorf("generating types_loader wasm stub: %w", err)
	}
	manifestSource, err := ec.generator.GenerateTypesManifestFile(blobs, ec.manifest.Package, runtime.Version())
	if err != nil {
		return fmt.Errorf("generating types manifest: %w", err)
	}
	files := []generatedOutput{
		{label: "types_loader", name: TypesLoaderFileName, data: loaderSource},
		{label: "types_loader_wasm", name: TypesLoaderWASMFileName, data: wasmSource},
		{label: "types_manifest", name: TypesManifestFileName, data: manifestSource},
	}
	for _, blob := range blobs {
		files = append(files, generatedOutput{label: "types_export " + blob.ImportPath, name: blob.FileName, data: blob.Data})
	}
	if ec.output != nil {
		if err := resetTypesExportDirectory(ec.output); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err := writeGeneratedOutput(file, ec); err != nil {
			return err
		}
	}
	return nil
}

// resetTypesExportDirectory removes stale export blobs and recreates the blob directory
// so packages dropped from the manifest do not leave orphaned files behind.
//
// Takes output (*os.Root) which confines the file operations to the output directory.
//
// Returns error when the directory cannot be reset.
func resetTypesExportDirectory(output *os.Root) error {
	if err := output.RemoveAll(TypesExportDirectory); err != nil {
		return fmt.Errorf("clearing %s: %w", TypesExportDirectory, err)
	}
	if err := output.MkdirAll(TypesExportDirectory, extractDirectoryPerms); err != nil {
		return fmt.Errorf("creating %s: %w", TypesExportDirectory, err)
	}
	return nil
}

// writeTypesDescriptor writes the types descriptor JSON file, which lists every extracted
// import path so a bytecode consumer can load the matching types.Package values.
//
// Takes importPaths ([]string) which lists the import paths to include.
// Takes ec (emitContext) which bundles manifest, flags, output and writers.
//
// Returns error when generation or writing of the descriptor fails.
func writeTypesDescriptor(importPaths []string, ec emitContext) error {
	descriptor, err := GenerateTypesDescriptorFile(importPaths)
	if err != nil {
		return fmt.Errorf("generating types descriptor: %w", err)
	}
	return writeGeneratedOutput(generatedOutput{label: "types_descriptor", name: typesDescriptorFileName, data: descriptor}, ec)
}

// writeGeneratedOutput writes one generated file, or prints what would be written in
// dry-run mode.
//
// Takes file (generatedOutput) which is the file to write.
// Takes ec (emitContext) which supplies the output directory and writers.
//
// Returns error when the write fails.
func writeGeneratedOutput(file generatedOutput, ec emitContext) error {
	outPath := filepath.Join(ec.manifest.Output, file.name)
	if ec.output == nil {
		_, _ = fmt.Fprintf(ec.stdout, "  %s -> %s (%d bytes)\n", file.label, outPath, len(file.data))
		return nil
	}
	if err := ec.output.WriteFile(file.name, file.data, extractFilePerms); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}
	_, _ = fmt.Fprintf(ec.stdout, "  %s -> %s\n", file.label, outPath)
	return nil
}

// generateUsage writes the usage information for the generate subcommand.
//
// Takes w (io.Writer) which receives the usage text.
// Takes config (Config) which names the tool and its default manifest.
func generateUsage(w io.Writer, config Config) {
	_, _ = fmt.Fprintf(w, `Usage: %[1]s generate [flags]

Generate reflect-based Go symbol files from a symbol manifest.

Reads a YAML manifest specifying which packages to extract and writes
Go source files containing reflect.Value symbol tables that the
interpreter loads at runtime.

Flags:
  --manifest <path>    Path to YAML manifest file (default: %[2]s)
  --output <directory> Override output directory from manifest
  --package <name>     Override output package name from manifest
  --select <prefixes>  Comma-separated import-path prefixes to keep (default: all)
  --list               Print the selected import paths and exit
  --emit <what>        Which artefacts to write: all (default), symbols, types, index
  --dry-run            Print what would be generated without writing files
  -h, --help           Show this help message

Manifest format (YAML):

  package: symbols
  output: symbols
  packages:
    - fmt
    - strings
    - encoding/json

`, config.ToolName, config.DefaultManifest)
}
