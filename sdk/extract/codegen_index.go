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
	"fmt"
	"go/ast"
	"go/token"
	"slices"
	"strconv"

	"pipit.sh/pipit/internal/gosource"
)

// IndexFileName is the file the import-path index is written to.
const IndexFileName = "gen_paths.go"

// GenerateIndexFile calls Generator.GenerateIndexFile with the default tool name.
//
// Takes paths ([]string) which are the import paths to list.
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when formatting fails.
func GenerateIndexFile(paths []string, outputPackage string) ([]byte, error) {
	return defaultGenerator.GenerateIndexFile(paths, outputPackage)
}

// GenerateIndexFile produces the Go source listing the extracted import paths.
//
// Takes paths ([]string) which are the import paths to list.
// Takes outputPackage (string) which is the Go package name for the generated file.
//
// Returns []byte which is the formatted Go source.
// Returns error when rendering fails.
func (g Generator) GenerateIndexFile(paths []string, outputPackage string) ([]byte, error) {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	listed := gosource.Lines(&ast.CompositeLit{Type: &ast.ArrayType{Elt: ast.NewIdent("string")}})
	for _, path := range sorted {
		listed.Elts = append(listed.Elts, &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)})
	}
	file := gosource.File{
		Header:          g.marker(),
		BuildConstraint: "",
		Package:         outputPackage,
		ImportGroups:    nil,
		Declarations: []gosource.Declaration{{
			Doc: []string{
				"ImportPaths lists every standard-library package the symbol tables register,",
				"sorted by import path.",
			},
			Directives: nil,
			Node: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{
				Names:  []*ast.Ident{ast.NewIdent("ImportPaths")},
				Values: []ast.Expr{listed},
			}}},
		}},
		LoneImportUnparenthesised: false,
	}
	formatted, err := file.Render()
	if err != nil {
		return nil, fmt.Errorf("rendering %s: %w", IndexFileName, err)
	}
	return formatted, nil
}
