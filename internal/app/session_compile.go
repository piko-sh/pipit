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

package app

import (
	"bytes"
	"context"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine/program"
)

// sessionImport is a single import entry with its optional alias ("" for none, "." for a
// dot import, "_" for a blank import), so aliases and dot/blank imports survive across
// session submissions rather than being reconstructed as a bare `import "path"`.
type sessionImport struct {
	// path is the import path.
	path string

	// alias is the optional import alias: "" for none, "." for a dot import, "_" for a blank
	// import.
	alias string
}

// newSessionCompiler constructs a Compiler that emits the current Submit's eval body into
// a fresh transient evalFunction.
//
// Takes root (*CompiledFunction) which holds the persistent function table.
// Takes evalFunction (*CompiledFunction) which is the per-Submit compilation target.
// Takes info (*types.Info) which is the type-check info for the current Submit.
// Takes globalVariables (map[string]GlobalVariableInfo) which maps names to global slots.
// Takes functionTable (map[string]uint16) which maps function names to root indices.
//
// Returns *Compiler configured for session-mode emission.
func (s *Service) newSessionCompiler(
	ctx context.Context,
	root *program.CompiledFunction,
	evalFunction *program.CompiledFunction,
	info *types.Info,
	globalVariables map[string]program.GlobalVariableInfo,
	functionTable map[string]uint16,
) *compile.Compiler {
	s.applyResourceLimits(root)
	return compile.NewCompiler(ctx, compile.CompilerConfig{
		FileSet:            s.fileSet,
		Info:               info,
		Function:           evalFunction,
		RootFunction:       root,
		ScopeName:          "<session>",
		FunctionTable:      functionTable,
		GlobalVariables:    globalVariables,
		Symbols:            s.symbols,
		Globals:            s.globals,
		Features:           s.features,
		Passes:             s.optimisations(),
		Patterns:           nil,
		MaxLiteralElements: s.maxLiteralElements(),
		MaxExpressionDepth: s.maxExpressionDepth(),
		DebugEnabled:       s.config != nil && s.config.debugInfo,
	})
}

// declNamesIn enumerates every session-scope name introduced by a declaration, supporting
// redeclaration detection on grouped declarations such as `var (x int; y string)`.
//
// Takes declaration (ast.Decl) which is the declaration to walk.
//
// Returns []SessionDecl listing each (name, kind) introduced, in source order. Returns an
// empty slice for declarations with no session-scope effect.
func declNamesIn(declaration ast.Decl) []SessionDecl {
	switch typed := declaration.(type) {
	case *ast.FuncDecl:
		return functionDeclNames(typed)
	case *ast.GenDecl:
		return genDeclNames(typed)
	}
	return nil
}

// functionDeclNames returns the SessionDecl introduced by a function declaration,
// excluding the special init function which is run rather than registered as a named
// declaration.
//
// Takes functionDeclaration (*ast.FuncDecl) which is the function declaration to inspect.
//
// Returns []SessionDecl with one entry for a non-init named function, or nil for init or
// anonymous declarations.
func functionDeclNames(functionDeclaration *ast.FuncDecl) []SessionDecl {
	if functionDeclaration.Name == nil {
		return nil
	}

	if functionDeclaration.Name.Name == compile.InitFunctionName && functionDeclaration.Recv == nil {
		return nil
	}
	return []SessionDecl{{Name: functionDeclaration.Name.Name, Kind: SessionDeclFunc}}
}

// genDeclNames returns the SessionDecls introduced by a generic declaration block
// (type/var/const). Imports are excluded because importSpecsIn handles them.
//
// Takes genericDeclaration (*ast.GenDecl) which is the declaration block.
//
// Returns []SessionDecl in source order.
func genDeclNames(genericDeclaration *ast.GenDecl) []SessionDecl {
	var out []SessionDecl
	for _, spec := range genericDeclaration.Specs {
		switch typed := spec.(type) {
		case *ast.TypeSpec:
			if typed.Name != nil && typed.Name.Name != "_" {
				out = append(out, SessionDecl{Name: typed.Name.Name, Kind: SessionDeclType})
			}
		case *ast.ValueSpec:
			out = append(out, valueSpecNames(genericDeclaration.Tok, typed)...)
		}
	}
	return out
}

// valueSpecNames returns the SessionDecls introduced by a value spec, classified as const
// or var based on the parent GenDecl's keyword. Blank identifiers ("_") and nil names are
// skipped.
//
// Takes declarationToken (token.Token) which is the parent GenDecl's keyword token.
// Takes spec (*ast.ValueSpec) which lists the names.
//
// Returns []SessionDecl in source order.
func valueSpecNames(declarationToken token.Token, spec *ast.ValueSpec) []SessionDecl {
	kind := SessionDeclVar
	if declarationToken == token.CONST {
		kind = SessionDeclConst
	}
	out := make([]SessionDecl, 0, len(spec.Names))
	for _, name := range spec.Names {
		if name == nil || name.Name == "_" {
			continue
		}
		out = append(out, SessionDecl{Name: name.Name, Kind: kind})
	}
	return out
}

// isInitFunctionDecl reports whether the declaration is a top-level init.
//
// True for `func init() { ... }` declarations. Methods named init (any receiver) are not
// package init functions and yield false.
//
// Takes declaration (ast.Decl) which is the declaration to test.
//
// Returns bool which is true for init function declarations.
func isInitFunctionDecl(declaration ast.Decl) bool {
	function, ok := declaration.(*ast.FuncDecl)
	return ok && function.Name != nil && function.Name.Name == compile.InitFunctionName && function.Recv == nil
}

// initDeclSignature returns a stable string fingerprint for an init function declaration
// so the session can recognise an init it has already compiled and avoid re-registering /
// re-running it on subsequent Submits. Uses go/printer with token.NewFileSet so the
// output is independent of source positions.
//
// Takes declaration (ast.Decl) which is expected to be an init function.
//
// Returns string which is the printer-rendered representation, or "" if rendering fails
// or declaration is not an init.
func initDeclSignature(declaration ast.Decl) string {
	if !isInitFunctionDecl(declaration) {
		return ""
	}
	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, token.NewFileSet(), declaration); err != nil {
		return ""
	}
	return buffer.String()
}

// importSpecsIn extracts the imports from an import declaration, preserving each path and
// its optional alias, and returning nil for non-import GenDecls.
//
// Takes declaration (ast.Decl) which may be an import declaration.
//
// Returns []sessionImport with one entry per imported spec.
func importSpecsIn(declaration ast.Decl) []sessionImport {
	generic, ok := declaration.(*ast.GenDecl)
	if !ok || generic.Tok != token.IMPORT {
		return nil
	}
	out := make([]sessionImport, 0, len(generic.Specs))
	for _, spec := range generic.Specs {
		importSpec, ok := spec.(*ast.ImportSpec)
		if !ok || importSpec.Path == nil {
			continue
		}
		alias := ""
		if importSpec.Name != nil {
			alias = importSpec.Name.Name
		}
		out = append(out, sessionImport{path: strings.Trim(importSpec.Path.Value, "\"`"), alias: alias})
	}
	return out
}
