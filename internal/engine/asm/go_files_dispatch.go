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

package asm

import (
	"fmt"
	"slices"
	"strings"

	"piko.sh/asmgen"
)

const (
	// sharedDispatchBuildConstraint is the build tag for the file both architectures build.
	sharedDispatchBuildConstraint = "//go:build !safe && !(js && wasm) && (amd64 || arm64)"
)

// licenceHeaderText is the licence and project-stance banner every source file in the
// tree carries, reproduced here so the generated dispatch files match the hand-written
// ones.
const licenceHeaderText = `// Copyright 2026 PolitePixels Limited
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
`

// dispatchGoFiles returns the generated Go files that pair with the dispatch assembly.
//
// asmgen.GoFile carries one Name and one Emit, with no per-architecture fan-out, so the
// architecture split is expressed as three entries whose Emit closures filter for their
// own architecture rather than by extending the upstream type.
//
// Returns []asmgen.GoFile which is the generated file list.
func dispatchGoFiles() []asmgen.GoFile {
	architectures := []asmgen.Architecture{asmgen.ArchitectureAMD64, asmgen.ArchitectureARM64}
	files := make([]asmgen.GoFile, 0, 1+len(architectures))
	files = append(files, asmgen.GoFile{
		Name: "vm_dispatch_shared_generated.go",
		Dir:  dispatchGoOutputDir,
		Emit: func(_ []asmgen.ArchitecturePort) string {
			return emitSharedDispatchFile()
		},
	})

	for _, architecture := range architectures {
		files = append(files, asmgen.GoFile{
			Name: fmt.Sprintf("vm_dispatch_%s_generated.go", architecture),
			Dir:  dispatchGoOutputDir,
			Emit: func(_ []asmgen.ArchitecturePort) string {
				return emitArchitectureDispatchFile(architecture)
			},
		})
	}

	return files
}

// handlerSynopsisIndex maps every handler name to its assembly comment.
//
// The comment is the single source of the symbol's one-line description: emitting the Go
// doc comment from it is what keeps the prototype and the TEXT block from describing the
// same handler differently.
//
// Returns map[string]string which maps handler name to its synopsis.
func handlerSynopsisIndex() map[string]string {
	synopses := make(map[string]string)
	for _, group := range FileGroups() {
		for _, handler := range group.Handlers {
			synopses[handler.Name] = handler.Comment
		}
	}
	return synopses
}

// declarationsFor selects the declarations belonging to one output file.
//
// Takes architecture (asmgen.Architecture) which filters the declarations, or empty for
// the shared file.
//
// Returns []goDeclaration which is the matching declarations.
func declarationsFor(architecture asmgen.Architecture) []goDeclaration {
	var selected []goDeclaration
	for _, declaration := range goDeclarations() {
		if architecture == "" {
			if declaration.Architectures == nil {
				selected = append(selected, declaration)
			}
			continue
		}
		if declarationTargets(declaration.Architectures, architecture) && declaration.Architectures != nil {
			selected = append(selected, declaration)
		}
	}
	return selected
}

// declarationTargets reports whether a restriction list covers an architecture.
//
// Takes architectures ([]asmgen.Architecture) which is the restriction list, or nil for
// unrestricted.
// Takes architecture (asmgen.Architecture) which is the architecture to test.
//
// Returns bool which is true when the architecture is covered.
func declarationTargets(architectures []asmgen.Architecture, architecture asmgen.Architecture) bool {
	if architectures == nil {
		return true
	}
	return slices.Contains(architectures, architecture)
}

// emitSharedDispatchFile returns the source of the architecture-independent dispatch
// file.
//
// Returns string which is the generated Go source.
func emitSharedDispatchFile() string {
	var b strings.Builder
	writeGeneratedGoHeader(&b, sharedDispatchBuildConstraint, nil)

	b.WriteString(`var (
	asmJumpTable [opcodeTableSize]uintptr
)

`)

	synopses := handlerSynopsisIndex()
	for _, declaration := range declarationsFor("") {
		writeGoDeclaration(&b, declaration, synopses)
	}

	writeInstallDispatchTables(&b)
	return b.String()
}

// emitArchitectureDispatchFile returns the source of one architecture's dispatch file.
//
// Takes architecture (asmgen.Architecture) which selects the target architecture.
//
// Returns string which is the generated Go source.
func emitArchitectureDispatchFile(architecture asmgen.Architecture) string {
	steps := initStepsFor(architecture)

	var imports []string
	for _, step := range steps {
		if step.Shared {
			continue
		}
		if step.GuardImport != "" {
			imports = append(imports, step.GuardImport)
		}
	}
	slices.Sort(imports)
	imports = dedupeStrings(imports)

	var b strings.Builder
	constraint := fmt.Sprintf("//go:build !safe && !(js && wasm) && %s", architecture)
	writeGeneratedGoHeader(&b, constraint, imports)

	synopses := handlerSynopsisIndex()
	for _, declaration := range declarationsFor(architecture) {
		writeGoDeclaration(&b, declaration, synopses)
	}

	b.WriteString("func init() {\n")
	for _, step := range steps {
		if step.Shared {
			continue
		}
		if step.Guard == "" {
			fmt.Fprintf(&b, "\t%s\n", step.Call)
			continue
		}
		fmt.Fprintf(&b, "\tif %s {\n\t\t%s\n\t}\n", step.Guard, step.Call)
	}
	b.WriteString("\tinstallDispatchTables()\n")
	b.WriteString("}\n")
	return b.String()
}

// initStepsFor selects the init sequence entries that run on one architecture.
//
// Takes architecture (asmgen.Architecture) which selects the target architecture.
//
// Returns []initStep which is the filtered init steps.
func initStepsFor(architecture asmgen.Architecture) []initStep {
	var selected []initStep
	for _, step := range initSequence() {
		if declarationTargets(step.Architectures, architecture) {
			selected = append(selected, step)
		}
	}
	return selected
}

// dedupeStrings removes adjacent duplicates from a sorted slice.
//
// Takes values ([]string) which is the sorted input.
//
// Returns []string which is the deduplicated result.
func dedupeStrings(values []string) []string {
	var unique []string
	for i, value := range values {
		if i > 0 && value == values[i-1] {
			continue
		}
		unique = append(unique, value)
	}
	return unique
}

// writeGeneratedGoHeader writes the generated-file banner, build tag, package clause and
// optional import block.
//
// Takes b (*strings.Builder) which is the output buffer.
// Takes buildConstraint (string) which is the go:build line.
// Takes imports ([]string) which is the optional import paths.
func writeGeneratedGoHeader(b *strings.Builder, buildConstraint string, imports []string) {
	b.WriteString("// Code generated by cmd/asmgen; DO NOT EDIT.\n\n")
	b.WriteString(licenceHeaderText)
	b.WriteString("\n")
	b.WriteString(buildConstraint)
	b.WriteString("\n\npackage engine\n\n")

	if len(imports) == 0 {
		return
	}
	b.WriteString("import (\n")
	for _, path := range imports {
		fmt.Fprintf(b, "\t%q\n", path)
	}
	b.WriteString(")\n\n")
}

// writeGoDeclaration writes one forward declaration with its doc comment.
//
// Takes b (*strings.Builder) which receives the emitted source.
// Takes declaration (goDeclaration) which is the declaration to write.
// Takes synopses (map[string]string) which maps handler names to their doc comments.
//
// Panics when declaration.Name has no matching handler synopsis.
func writeGoDeclaration(b *strings.Builder, declaration goDeclaration, synopses map[string]string) {
	_, ok := synopses[declaration.Name]
	if !ok {
		panic(fmt.Sprintf("asm: goDeclarations names %q, which no handler definition defines", declaration.Name))
	}

	b.WriteString("//go:noescape\nfunc ")
	b.WriteString(declaration.Name)
	b.WriteString("(")
	b.WriteString(declaration.Parameters)
	b.WriteString(")")

	b.WriteString("\n\n")
}

// writeInstallDispatchTables writes the shared installer that both architectures call.
//
// Takes b (*strings.Builder) which receives the emitted source.
func writeInstallDispatchTables(b *strings.Builder) {
	b.WriteString("func installDispatchTables() {\n")
	for _, step := range initSequence() {
		if !step.Shared {
			continue
		}
		fmt.Fprintf(b, "\t%s\n", step.Call)
	}
	b.WriteString("}\n")
}
