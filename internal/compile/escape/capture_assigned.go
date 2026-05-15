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

package escape

import (
	"go/ast"
	"go/token"
	"go/types"
)

// CollectDirectlyAssignedNames returns the names assigned as a whole anywhere in body.
//
// Covers `x = v`, `x op= v`, `x++`, `for x = range`, and a `:=` that reuses an existing
// x. Writes through an index or field path are excluded because they mutate shared or
// already-promoted storage.
//
// Takes info (*types.Info) which resolves `:=` redeclarations; may be nil.
// Takes body (*ast.BlockStmt) which is scanned in full, nested literals included.
//
// Returns map[string]bool which is nil when nothing is assigned.
func CollectDirectlyAssignedNames(info *types.Info, body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return nil
	}
	loopDeclared := loopInitDeclaredNames(body)
	var assigned map[string]bool
	mark := func(expression ast.Expr) {
		identifier, ok := ast.Unparen(expression).(*ast.Ident)
		if !ok || identifier.Name == "_" || loopDeclared[identifier.Name] {
			return
		}
		if assigned == nil {
			assigned = make(map[string]bool)
		}
		assigned[identifier.Name] = true
	}
	ast.Inspect(body, func(node ast.Node) bool {
		recordDirectAssignment(info, node, mark)
		return true
	})
	return assigned
}

// recordDirectAssignment passes every identifier that node assigns as a whole to mark:
// the left-hand operands of an assignment (a redeclaring `:=` included), the operand of
// an increment or decrement, and the key and value of an assignment-form range clause.
//
// Takes info (*types.Info) which resolves `:=` redeclarations; may be nil.
// Takes node (ast.Node) which is the visited node.
// Takes mark (func) which records an assigned operand.
func recordDirectAssignment(info *types.Info, node ast.Node, mark func(ast.Expr)) {
	switch statement := node.(type) {
	case *ast.AssignStmt:
		for _, lhs := range statement.Lhs {
			if statement.Tok == token.DEFINE && !redeclares(info, lhs) {
				continue
			}
			mark(lhs)
		}
	case *ast.IncDecStmt:
		mark(statement.X)
	case *ast.RangeStmt:
		if statement.Tok == token.ASSIGN {
			mark(statement.Key)
			mark(statement.Value)
		}
	}
}

// redeclares reports whether lhs in a `:=` statement names an existing variable rather
// than declaring a new one. Without type information every `:=` operand is treated as a
// declaration.
//
// Takes info (*types.Info) which may be nil.
// Takes lhs (ast.Expr) which is one left-hand operand of the statement.
//
// Returns bool which is true when the operand is an assignment to an existing variable.
func redeclares(info *types.Info, lhs ast.Expr) bool {
	identifier, ok := lhs.(*ast.Ident)
	if !ok || info == nil || identifier.Name == "_" {
		return false
	}
	_, declared := info.Defs[identifier]
	return declared && info.Defs[identifier] == nil
}

// loopInitDeclaredNames returns the names declared with `:=` by the init statement of any
// three-clause loop in body.
//
// Takes body (*ast.BlockStmt) which is scanned in full.
//
// Returns map[string]bool which is nil when no such loop exists.
func loopInitDeclaredNames(body *ast.BlockStmt) map[string]bool {
	var result map[string]bool
	ast.Inspect(body, func(node ast.Node) bool {
		loop, ok := node.(*ast.ForStmt)
		if !ok {
			return true
		}
		initAssign, ok := loop.Init.(*ast.AssignStmt)
		if !ok || initAssign.Tok != token.DEFINE {
			return true
		}
		for _, lhs := range initAssign.Lhs {
			identifier, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if result == nil {
				result = make(map[string]bool)
			}
			result[identifier.Name] = true
		}
		return true
	})
	return result
}
