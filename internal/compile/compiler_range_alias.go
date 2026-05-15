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
	"go/types"
)

// rangeValueMayAlias reports whether a range loop's value variable can share the
// element's storage.
//
// The loop must declare the value, range over a plain identifier, contain no closure,
// goroutine or defer, never assign to, address or take a method value of the value
// variable, and never assign to or address the ranged identifier. Heap purity of the body
// is decided later by AliasReadOnlyRangeValues().
//
// Takes statement (*ast.RangeStmt) which is the range loop.
// Takes valueIdentifier (*ast.Ident) which is the loop's value variable.
//
// Returns true when the syntactic conditions hold.
func (c *Compiler) rangeValueMayAlias(statement *ast.RangeStmt, valueIdentifier *ast.Ident) bool {
	if statement.Tok != token.DEFINE || statement.Body == nil {
		return false
	}
	rangedIdentifier, ok := statement.X.(*ast.Ident)
	if !ok || bodyContainsFunctionLit(statement.Body) {
		return false
	}
	protected := map[string]bool{valueIdentifier.Name: true, rangedIdentifier.Name: true}
	safe := true
	ast.Inspect(statement.Body, func(node ast.Node) bool {
		if !safe {
			return false
		}
		safe = c.rangeBodyNodeKeepsAlias(node, protected, valueIdentifier.Name)
		return safe
	})
	return safe
}

// rangeBodyNodeKeepsAlias reports whether one node of a range body is compatible with
// aliasing the value variable.
//
// Takes node (ast.Node) which is the node under inspection.
// Takes protected (map[string]bool) which names the value and ranged identifiers.
// Takes valueName (string) which is the value variable's name.
//
// Returns false when the node writes, addresses or binds a method on a protected name, or
// starts a goroutine or defer.
func (c *Compiler) rangeBodyNodeKeepsAlias(node ast.Node, protected map[string]bool, valueName string) bool {
	switch n := node.(type) {
	case *ast.AssignStmt:
		for _, lhs := range n.Lhs {
			if protected[rootIdentifierName(lhs)] {
				return false
			}
		}
	case *ast.IncDecStmt:
		return !protected[rootIdentifierName(n.X)]
	case *ast.UnaryExpr:
		return n.Op != token.AND || !protected[rootIdentifierName(n.X)]
	case *ast.RangeStmt:
		if n.Tok == token.ASSIGN {
			return !protected[rootIdentifierName(n.Key)] && !protected[rootIdentifierName(n.Value)]
		}
	case *ast.SelectorExpr:
		if rootIdentifierName(n.X) != valueName {
			return true
		}
		selection, ok := c.Info.Selections[n]
		return !ok || selection.Kind() != types.MethodVal
	case *ast.GoStmt, *ast.DeferStmt:
		return false
	default:
	}
	return true
}

// rootIdentifierName returns the identifier at the root of a selector, index, star or
// parenthesised expression chain, or the empty string when the chain does not start at an
// identifier.
//
// Takes expr (ast.Expr) which is the expression whose root is sought.
//
// Returns the root identifier's name or "".
func rootIdentifierName(expr ast.Expr) string {
	for expr != nil {
		switch e := expr.(type) {
		case *ast.Ident:
			return e.Name
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.IndexExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		case *ast.SliceExpr:
			expr = e.X
		default:
			return ""
		}
	}
	return ""
}
