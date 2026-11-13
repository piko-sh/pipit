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
	"go/ast"
	"strings"

	"piko.sh/asmgen"

	"pipit.sh/pipit/internal/gosource"
)

const (
	// dispatchGoOutputDir is the destination directory for generated Go files that pair with
	// the asmgen-emitted .s files. Same root as the .s output (engine), so the generated
	// trampolines can reference unexported handle*/pathBTrampoline symbols without exporting
	// them.
	dispatchGoOutputDir = "internal/engine"
)

// GoFiles returns the list of asmgen-managed Go source files.
//
// Returns []asmgen.GoFile which is the generated file list.
func GoFiles() []asmgen.GoFile {
	dispatch := dispatchGoFiles()
	files := make([]asmgen.GoFile, 0, 1+len(dispatch))
	files = append(files, asmgen.GoFile{
		Name: "asm_call_trampolines_pathb_generated.go",
		Dir:  dispatchGoOutputDir,
		Emit: func(_ []asmgen.ArchitecturePort) string {
			return emitPathBTrampolineFile(PathBShims())
		},
	})
	return append(files, dispatch...)
}

// emitPathBTrampolineFile returns the source text of the generated Go file containing one
// trampoline per tier-2 shim spec.
//
// Takes specs ([]PathBShimSpec) which lists the shim specifications to emit.
//
// Returns string which is the generated Go source text.
func emitPathBTrampolineFile(specs []PathBShimSpec) string {
	declarations := make([]gosource.Declaration, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSuffix(strings.TrimPrefix(spec.TrampolineSymbol, "·"), "(SB)")
		dispatcher := "pathBTrampoline"
		if spec.IsNarrow {
			dispatcher = "pathBTrampolineNarrow"
		}
		declarations = append(declarations, gosource.Declaration{
			Doc:        nil,
			Directives: []string{"go:nosplit"},
			Node: &ast.FuncDecl{
				Name: ast.NewIdent(name),
				Type: &ast.FuncType{
					Params: &ast.FieldList{List: []*ast.Field{
						{Names: []*ast.Ident{ast.NewIdent("ctx")}, Type: &ast.StarExpr{X: ast.NewIdent("dispatchContext")}},
						{Names: []*ast.Ident{ast.NewIdent("instWord")}, Type: ast.NewIdent("uint32")},
					}},
					Results: &ast.FieldList{List: []*ast.Field{{Type: &ast.StarExpr{X: ast.NewIdent("dispatchContext")}}}},
				},
				Body: &ast.BlockStmt{List: []ast.Stmt{
					&ast.ExprStmt{X: &ast.CallExpr{
						Fun: ast.NewIdent(dispatcher),
						Args: []ast.Expr{
							ast.NewIdent("ctx"),
							ast.NewIdent("instWord"),
							ast.NewIdent(pathBHandlerForTrampoline(name)),
						},
					}},
					&ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("ctx")}},
				}},
			},
		})
	}
	return renderGoFile(&gosource.File{
		Header:                    generatedGoNotice + "\n\n" + licenceHeaderText,
		BuildConstraint:           sharedDispatchBuildConstraint,
		Package:                   "engine",
		ImportGroups:              nil,
		LoneImportUnparenthesised: false,
		Declarations:              declarations,
	})
}

// pathBHandlerForTrampoline derives the Go handler function name from the trampoline name
// by stripping the "asmCall" prefix and lowercasing the first letter.
//
// Takes trampolineName (string) which is the trampoline symbol name.
//
// Returns string which is the derived handler function name.
func pathBHandlerForTrampoline(trampolineName string) string {
	rest := strings.TrimPrefix(trampolineName, "asmCall")
	if rest == "" {
		return trampolineName
	}
	return strings.ToLower(rest[:1]) + rest[1:]
}
