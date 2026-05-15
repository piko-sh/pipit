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

package compile

import (
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/engine/program"
)

// functionDeclEntry pairs a function declaration with its compiled shell, used between
// the register and compile passes.
type functionDeclEntry struct {
	// declaration is the parsed function declaration AST node.
	declaration *ast.FuncDecl

	// compiledFunction is the compiled function shell for this declaration.
	compiledFunction *program.CompiledFunction
}

// findEvalFunctionDeclaration locates the _eval_ function declaration in a file.
//
// Takes file (*ast.File) which is the parsed AST file to search.
//
// Returns *ast.FuncDecl which is the _eval_ function declaration, or nil when not found.
func findEvalFunctionDeclaration(file *ast.File) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		functionDeclaration, ok := declaration.(*ast.FuncDecl)
		if ok && functionDeclaration.Name.Name == EvalFunctionName {
			return functionDeclaration
		}
	}
	return nil
}

// indexValueSpecsByPosition indexes every ast.ValueSpec from a declaration list by the
// source position of each of its names. types.Initializer.Lhs uses *types.Var (which
// carries a Pos), so this index lets us recover the originating spec from any LHS var.
//
// Takes files ([]*ast.File) which are the parsed AST files to walk.
//
// Returns map[token.Pos]*ast.ValueSpec keyed by every declared name's position.
func indexValueSpecsByPosition(files []*ast.File) map[token.Pos]*ast.ValueSpec {
	out := make(map[token.Pos]*ast.ValueSpec)
	for _, file := range files {
		for _, declaration := range file.Decls {
			indexValueSpecsFromDecl(declaration, out)
		}
	}
	return out
}

// indexValueSpecsFromDecl indexes value specs from a var declaration.
//
// Records every name position from a single top-level declaration into out, when the
// declaration is a `var` block. No-op for any other declaration kind.
//
// Takes declaration (ast.Decl) which is the top-level declaration to inspect.
// Takes out (map[token.Pos]*ast.ValueSpec) which receives the position-keyed spec
// entries.
func indexValueSpecsFromDecl(declaration ast.Decl, out map[token.Pos]*ast.ValueSpec) {
	genDecl, ok := declaration.(*ast.GenDecl)
	if !ok || genDecl.Tok != token.VAR {
		return
	}
	for _, spec := range genDecl.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, name := range vs.Names {
			out[name.Pos()] = vs
		}
	}
}
