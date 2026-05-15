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

// Package frontend turns Go source into the type-checked form the compiler consumes.
//
// Holding the parse-and-type-check step here keeps it below both internal/app and
// internal/compile in the dependency order, so the compiler and its tests can reach it
// without inverting the layering depguard enforces.
//
// The package deliberately carries no service policy: build-constraint filtering, import
// allowlists and the //go:embed refusal stay in internal/app, where the configuration
// they read lives.
package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"

	"pipit.sh/pipit/internal/engine/goversion"
	"pipit.sh/pipit/internal/fault"
)

// hostArchitecture is the architecture the size model describes. The interpreter lays
// values out to one model regardless of the host, so the checker is pinned to it.
const hostArchitecture = "amd64"

// hostCompiler names the size model's compiler, matching the layout the runtime assumes.
const hostCompiler = "gc"

// Result carries everything a compilation needs from the front end.
type Result struct {
	// FileSet resolves AST positions to files and lines.
	FileSet *token.FileSet

	// Info holds the go/types results for the parsed files.
	Info *types.Info

	// Package is the type-checked package the files declare.
	Package *types.Package

	// Files are the parsed files, in the deterministic order they were checked.
	Files []*ast.File
}

// NewTypesInfo creates the types.Info the compiler reads after type checking. Every
// consulted map is allocated because a nil map makes go/types skip recording, which
// surfaces as a missing type later.
//
// Returns *types.Info with each consulted map ready to receive results.
func NewTypesInfo() *types.Info {
	return &types.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Instances:  make(map[*ast.Ident]types.Instance),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
}

// NewTypesConfig creates the types.Config the interpreter type-checks against.
//
// The size model is pinned rather than taken from the host so a script sees one value
// layout everywhere, and the language version is the one the interpreter implements.
//
// Takes importer (types.Importer) which resolves imported packages, or nil to refuse
// every import.
//
// Returns *types.Config configured for interpreted source.
func NewTypesConfig(importer types.Importer) *types.Config {
	config := &types.Config{
		Sizes:     types.SizesFor(hostCompiler, hostArchitecture),
		GoVersion: goversion.InterpretedGoVersion,
	}
	if importer != nil {
		config.Importer = importer
	}
	return config
}

// ParseSources parses every source file in a deterministic order.
//
// Files are parsed by sorted filename so a map's iteration order cannot change the
// positions recorded in the file set, which would otherwise make diagnostics and source
// maps differ between runs of the same input.
//
// Takes fileSet (*token.FileSet) which records the positions of the parsed files.
// Takes sources (map[string]string) which maps filename to source text.
//
// Returns []*ast.File in sorted filename order.
// Returns error wrapping fault.ErrParse when any file fails to parse.
func ParseSources(fileSet *token.FileSet, sources map[string]string) ([]*ast.File, error) {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	slices.Sort(names)

	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		file, err := parser.ParseFile(fileSet, name, sources[name], parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrParse, name, err)
		}
		files = append(files, file)
	}
	return files, nil
}

// Typecheck parses and type-checks a set of sources as one package.
//
// Takes packageName (string) which names the package being checked.
// Takes sources (map[string]string) which maps filename to source text.
// Takes importer (types.Importer) which resolves imports, or nil to refuse every import.
//
// Returns *Result holding the file set, parsed files and type information.
// Returns error wrapping fault.ErrParse or fault.ErrTypeCheck on failure.
func Typecheck(packageName string, sources map[string]string, importer types.Importer) (*Result, error) {
	fileSet := token.NewFileSet()
	files, err := ParseSources(fileSet, sources)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%w: no source files to check", fault.ErrParse)
	}

	info := NewTypesInfo()
	checked, err := NewTypesConfig(importer).Check(packageName, fileSet, files, info)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrTypeCheck, err)
	}
	return &Result{FileSet: fileSet, Info: info, Package: checked, Files: files}, nil
}
