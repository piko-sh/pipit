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

// Package liveness is the last-use analysis over a statement list: the pre-scan that lets
// the register allocator recycle a local's register after its final read.
package liveness

import (
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/compile/typemap"
)

// ComputeLastUseIndices pre-scans statements to find the index of the last reference to
// each locally declared name.
//
// Takes statements ([]ast.Stmt) which is the block of statements to scan.
//
// Returns a map from declared name to last-use index, or nil when no declarations exist
// or goto/label statements invalidate the forward-only liveness assumption.
func ComputeLastUseIndices(statements []ast.Stmt) map[string]int {
	declared, hasGotoOrLabel := collectDeclaredNamesAndLabels(statements)
	if len(declared) == 0 || hasGotoOrLabel {
		return nil
	}
	return scanLastUsePerVariable(statements, declared)
}

// ExtractDeclaredNames returns the variable names introduced by a declaring statement (:=
// or var/const), or nil for non-declaring statements. The blank identifier is filtered
// out.
//
// Takes statement (ast.Stmt) which is the AST statement to inspect.
//
// Returns the slice of declared non-blank variable names, or nil when none.
func ExtractDeclaredNames(statement ast.Stmt) []string {
	switch s := statement.(type) {
	case *ast.AssignStmt:
		return extractShortVarDeclNames(s)
	case *ast.DeclStmt:
		return extractDeclStmtNames(s)
	case *ast.LabeledStmt:
		return ExtractDeclaredNames(s.Stmt)
	default:
		return nil
	}
}

// collectDeclaredNamesAndLabels scans statements and returns the set of locally declared
// names together with a flag that is true when any goto or labelled statement appears.
//
// Takes statements ([]ast.Stmt) which is the block of statements to scan.
//
// Returns the set of locally declared names and a boolean that is true when any goto or
// labelled statement appears in the block.
func collectDeclaredNamesAndLabels(statements []ast.Stmt) (map[string]struct{}, bool) {
	declared := make(map[string]struct{})
	hasGotoOrLabel := false
	for _, statement := range statements {
		for _, name := range ExtractDeclaredNames(statement) {
			declared[name] = struct{}{}
		}
		if !hasGotoOrLabel {
			hasGotoOrLabel = statementHasGotoOrLabel(statement)
		}
	}
	return declared, hasGotoOrLabel
}

// statementHasGotoOrLabel reports whether statement is a goto branch or a labelled
// statement.
//
// Takes statement (ast.Stmt) which is the AST statement to inspect.
//
// Returns true when the statement is a goto branch or labelled statement.
func statementHasGotoOrLabel(statement ast.Stmt) bool {
	switch s := statement.(type) {
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.LabeledStmt:
		_ = s
		return true
	default:
		return false
	}
}

// scanLastUsePerVariable walks each statement and records the index of the last statement
// that references each name in declared.
//
// Takes statements ([]ast.Stmt) which is the block of statements to scan.
// Takes declared (map[string]struct{}) which is the set of locally declared names to
// track.
//
// Returns a map from declared name to the index of its last use.
func scanLastUsePerVariable(statements []ast.Stmt, declared map[string]struct{}) map[string]int {
	lastUse := make(map[string]int, len(declared))
	for i, statement := range statements {
		ast.Inspect(statement, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.Ident); ok {
				if _, isDeclared := declared[identifier.Name]; isDeclared {
					lastUse[identifier.Name] = i
				}
			}
			return true
		})
	}
	return lastUse
}

// extractDeclStmtNames returns the non-blank names introduced by a var or const
// declaration statement, or nil when statement contains no value specs.
//
// Takes statement (*ast.DeclStmt) which is the AST declaration statement to inspect.
//
// Returns the slice of non-blank declared names, or nil when no value specs.
func extractDeclStmtNames(statement *ast.DeclStmt) []string {
	generalDeclaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok {
		return nil
	}
	var names []string
	for _, spec := range generalDeclaration.Specs {
		if valueSpec, ok := spec.(*ast.ValueSpec); ok {
			for _, name := range valueSpec.Names {
				if name.Name != typemap.BlankIdentName {
					names = append(names, name.Name)
				}
			}
		}
	}
	return names
}

// extractShortVarDeclNames returns the non-blank identifiers introduced by a short
// variable declaration (:=), or nil when statement is not a := assignment.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement to inspect.
//
// Returns the slice of non-blank declared names, or nil when not a :=.
func extractShortVarDeclNames(statement *ast.AssignStmt) []string {
	if statement.Tok != token.DEFINE {
		return nil
	}
	var names []string
	for _, leftHandSide := range statement.Lhs {
		if identifier, ok := leftHandSide.(*ast.Ident); ok && identifier.Name != typemap.BlankIdentName {
			names = append(names, identifier.Name)
		}
	}
	return names
}
