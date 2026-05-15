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

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"piko.sh/asmgen"

	"pipit.sh/pipit/internal/engine"
	engineasm "pipit.sh/pipit/internal/engine/asm"
	archamd64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_amd64"
	archarm64 "pipit.sh/pipit/internal/engine/asm/asmgen_arch_arm64"
)

const (
	// generatedFileHeader is the copyright, licence, and banner block appended below the
	// generated-code marker at the top of every generated .s file.
	generatedFileHeader = `// Copyright 2026 PolitePixels Limited
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
// strip others of their rights and dignity.`
)

// hostModulePaths lists the module declarations that may own the interpreter tree.
var hostModulePaths = []string{
	"module pipit.sh/pipit",
}

// headerOptions returns the asmgen options that reproduce pipit's generated-file header.
// Generation and validation must use the same options so validation compares against
// byte-identical output.
//
// Returns []asmgen.Option carrying the tool name and file header.
func headerOptions() []asmgen.Option {
	return []asmgen.Option{
		asmgen.WithGeneratedByTool("cmd/asmgen"),
		asmgen.WithFileHeader(generatedFileHeader),
	}
}

// main parses flags and runs either assembly generation or validation.
func main() {
	validate := flag.Bool("validate", false, "compare generated output against existing files instead of writing")
	root := flag.String("root", "", "module root to generate into; when empty, walk up from the working directory looking for a recognised go.mod")
	flag.Parse()

	if err := chdirToRepoRoot(*root); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if *validate {
		if err := runValidation(); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := runGeneration(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// chdirToRepoRoot chdirs to the module root the generated files are written relative to.
//
// Takes root (string) which is the explicit module root, or empty to auto-detect.
//
// Returns error when no go.mod marker is found before the filesystem root, or when a
// directory walk step fails.
func chdirToRepoRoot(root string) error {
	if root != "" {
		return os.Chdir(root)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	dir := cwd
	for {
		if matchesHostGoMod(dir) {
			if dir == cwd {
				return nil
			}
			return os.Chdir(dir)
		}
		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == "" || parent == dir {
			return fmt.Errorf("asmgen: no go.mod declaring one of %v found above %q; pass -root to set it explicitly", hostModulePaths, cwd)
		}
		dir = parent
	}
}

// matchesHostGoMod reports whether the directory holds a go.mod declaring one of the
// modules that may own the interpreter tree, matched on the first-line module
// declaration.
//
// The read is routed through a directory-rooted handle so the lookup cannot escape the
// candidate directory.
//
// Takes dir (string) which is the candidate directory holding go.mod.
//
// Returns bool which is true when dir/go.mod declares a recognised module.
func matchesHostGoMod(dir string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer func() { _ = root.Close() }()
	data, err := root.ReadFile("go.mod")
	if err != nil {
		return false
	}
	firstLine, _, _ := strings.Cut(string(data), "\n")
	trimmed := strings.TrimSpace(firstLine)
	for _, modulePath := range hostModulePaths {
		if strings.HasPrefix(trimmed, modulePath) {
			return true
		}
	}
	return false
}

// runGeneration writes Plan 9 assembly and header files for all architecture ports to
// their target directories on disk.
//
// Returns error when file generation or writing fails.
func runGeneration() error {
	writer := asmgen.NewDiskWriter()

	err := asmgen.GenerateFiles(
		writer,
		architecturePorts(),
		engineasm.FileGroups(),
		interpHeaderFiles(),
		engineasm.GoFiles(),
		headerOptions()...,
	)
	if err != nil {
		return fmt.Errorf("generating interp dispatch files: %w", err)
	}

	fmt.Println("generated all assembly files")
	return nil
}

// runValidation generates assembly files in memory and compares them against the existing
// files on disk, reporting any mismatches.
//
// Returns error when validation fails or mismatches are found.
func runValidation() error {
	interpMismatches, err := asmgen.GenerateAndValidate(
		architecturePorts(),
		engineasm.FileGroups(),
		interpHeaderFiles(),
		engineasm.GoFiles(),
		headerOptions()...,
	)
	if err != nil {
		return fmt.Errorf("validating interp files: %w", err)
	}

	allMismatches := interpMismatches
	if len(allMismatches) == 0 {
		fmt.Println("all generated files match existing files")
		return nil
	}

	for _, m := range allMismatches {
		fmt.Fprintf(os.Stderr, "mismatch in %s at line %d:\n  expected: %q\n  actual:   %q\n",
			m.File, m.Line, m.Expected, m.Actual)
	}

	return fmt.Errorf("%d file(s) have mismatches", len(allMismatches))
}

// architecturePorts builds the amd64 and arm64 ports with the handler jump table entries
// from the engine.
//
// Returns []engineasm.BytecodeArchitecturePort with one port per supported architecture.
func architecturePorts() []engineasm.BytecodeArchitecturePort {
	provider := engine.ProvideAsmHandlerJumpTableEntries()
	amd64Entries := make([]archamd64.JumpTableEntry, len(provider))
	arm64Entries := make([]archarm64.JumpTableEntry, len(provider))
	for i, entry := range provider {
		amd64Entries[i] = archamd64.JumpTableEntry(entry)
		arm64Entries[i] = archarm64.JumpTableEntry(entry)
	}

	return []engineasm.BytecodeArchitecturePort{
		archamd64.New(amd64Entries...),
		archarm64.New(arm64Entries...),
	}
}

// interpHeaderFiles assembles the engine header file generators with offsets derived from
// the live runtime structs via unsafe.Offsetof.
//
// Returns []asmgen.HeaderFile ready to pass to GenerateFiles or GenerateAndValidate.
func interpHeaderFiles() []asmgen.HeaderFile {
	return engineasm.HeaderFiles(
		engine.ProvideDispatchContextOffsets(),
		engine.ProvideCallFrameOffsets(),
		engine.ProvideASMCallInfoOffsets(),
		engine.ProvideVarLocationOffsets(),
	)
}
