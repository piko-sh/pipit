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
)

// unparenthesiseTargets removes the parentheses around every assignment, inc/dec and
// range target, and around every operand of &, under each node, in place, together with
// those around the operands the target's selectors and indexes reach into.
//
// Takes nodes ([]N) which are the files or declarations to rewrite.
func unparenthesiseTargets[N ast.Node](nodes []N) {
	for _, node := range nodes {
		ast.Inspect(node, unparenthesiseTarget)
	}
}

// unparenthesiseTarget rewrites the targets of one statement or the operand of one &, as
// an ast.Inspect visitor.
//
// Takes node (ast.Node) which is the node being visited.
//
// Returns bool which is always true, so the walk reaches nested function literals.
func unparenthesiseTarget(node ast.Node) bool {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		if typed.Tok == token.DEFINE {
			return true
		}
		for i, target := range typed.Lhs {
			typed.Lhs[i] = unparenthesisePath(target)
		}
	case *ast.IncDecStmt:
		typed.X = unparenthesisePath(typed.X)
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			typed.X = unparenthesisePath(typed.X)
		}
	case *ast.RangeStmt:
		if typed.Tok != token.ASSIGN {
			return true
		}
		if typed.Key != nil {
			typed.Key = unparenthesisePath(typed.Key)
		}
		if typed.Value != nil {
			typed.Value = unparenthesisePath(typed.Value)
		}
	}
	return true
}

// unparenthesisePath removes the parentheses around a target and around each operand its
// selectors and indexes reach into, so (t).n and (s)[i] become t.n and s[i] and their
// root variable is found. The operand of a dereference is left as written, since *p
// reaches memory through p's value, never p itself.
//
// Takes target (ast.Expr) which is the target expression.
//
// Returns ast.Expr which is the target without its parentheses.
func unparenthesisePath(target ast.Expr) ast.Expr {
	target = ast.Unparen(target)
	for current := target; ; {
		switch typed := current.(type) {
		case *ast.SelectorExpr:
			typed.X = ast.Unparen(typed.X)
			current = typed.X
		case *ast.IndexExpr:
			typed.X = ast.Unparen(typed.X)
			current = typed.X
		case *ast.IndexListExpr:
			typed.X = ast.Unparen(typed.X)
			current = typed.X
		default:
			return target
		}
	}
}
