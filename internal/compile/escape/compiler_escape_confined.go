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
)

// maxConfinementDepth bounds the callee chain the confinement analysis follows before it
// gives up and reports an escape.
const maxConfinementDepth = 8

// walkWithParents visits every node under root with the chain of its ancestors, nearest
// last. Returning false from visit skips the node's children.
//
// Takes root (ast.Node) which is the subtree to walk.
// Takes visit (func) which receives each node and its ancestors.
func walkWithParents(root ast.Node, visit func(node ast.Node, parents []ast.Node) bool) {
	var parents []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			parents = parents[:len(parents)-1]
			return true
		}
		descend := visit(node, parents)
		parents = append(parents, node)
		return descend
	})
}

// addressArgumentConfined reports whether the address passed as argument index of call
// stays inside the callee and does not outlive it.
//
// Takes call (*ast.CallExpr) which is the call receiving the address.
// Takes index (int) which is the argument's position.
// Takes body (*ast.BlockStmt) which is the caller's body, checked for a local shadowing
// the callee's name.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
//
// Returns true when the address provably does not outlive the call.
func addressArgumentConfined(call *ast.CallExpr, index int, body *ast.BlockStmt, declarations map[string]*ast.FuncDecl) bool {
	callee, ok := call.Fun.(*ast.Ident)
	if !ok || call.Ellipsis != token.NoPos {
		return false
	}
	declaration, found := declarations[callee.Name]
	if !found || declaration.Recv != nil || declaration.Body == nil {
		return false
	}
	if nameDeclaredAtLeastOnce(body, callee.Name) {
		return false
	}
	return parameterIsConfined(declaration, index, declarations, map[*ast.FuncDecl]bool{}, 0)
}

// parameterIsConfined reports whether parameter index of declaration is used only in ways
// that keep the pointer inside the callee's frame.
//
// A declaration already being analysed higher in the chain is assumed confined: if any
// use in the cycle escapes, the frame that found it reports the escape on its own, so the
// assumption is only ever confirmed, never relied on to hide an escape.
//
// Takes declaration (*ast.FuncDecl) which is the callee.
// Takes index (int) which is the parameter's position.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
// Takes visiting (map[*ast.FuncDecl]bool) which holds the declarations on the current
// chain.
// Takes depth (int) which is the current chain length.
//
// Returns true when the parameter is confined.
func parameterIsConfined(declaration *ast.FuncDecl, index int, declarations map[string]*ast.FuncDecl, visiting map[*ast.FuncDecl]bool, depth int) bool {
	if depth > maxConfinementDepth {
		return false
	}
	if visiting[declaration] {
		return true
	}
	name, ok := parameterNameAt(declaration, index)
	if !ok || name == "_" {
		return false
	}
	if nameDeclaredAtLeastOnce(declaration.Body, name) {
		return false
	}
	visiting[declaration] = true
	defer delete(visiting, declaration)
	confined := true
	walkWithParents(declaration.Body, func(node ast.Node, parents []ast.Node) bool {
		if !confined {
			return false
		}
		ident, isIdent := node.(*ast.Ident)
		if !isIdent || ident.Name != name || isSelectorField(ident, parents) {
			return true
		}
		if !pointerUseConfined(ident, parents, declaration.Body, declarations, visiting, depth) {
			confined = false
		}
		return true
	})
	return confined
}

// parameterNameAt returns the name of the parameter at position index, expanding grouped
// declarations such as `a, b *T`.
//
// Takes declaration (*ast.FuncDecl) which is the function.
// Takes index (int) which is the parameter position.
//
// Returns the name and true, or false when the position does not exist or is variadic.
func parameterNameAt(declaration *ast.FuncDecl, index int) (string, bool) {
	if declaration.Type == nil || declaration.Type.Params == nil {
		return "", false
	}
	position := 0
	for _, field := range declaration.Type.Params.List {
		if _, variadic := field.Type.(*ast.Ellipsis); variadic {
			return "", false
		}
		count := max(len(field.Names), 1)
		if index < position+count {
			if len(field.Names) == 0 {
				return "", false
			}
			return field.Names[index-position].Name, true
		}
		position += count
	}
	return "", false
}

// pointerUseConfined judges one occurrence of a pointer parameter by its ancestors,
// returning true when the use keeps the pointer within the frame.
//
// Takes ident (*ast.Ident) which is the occurrence.
// Takes parents ([]ast.Node) which are the occurrence's ancestors, nearest last.
// Takes body (*ast.BlockStmt) which is the enclosing function body.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
// Takes visiting (map[*ast.FuncDecl]bool) which holds the declarations on the chain.
// Takes depth (int) which is the current chain length.
//
// Returns true when the occurrence keeps the pointer confined.
func pointerUseConfined(ident *ast.Ident, parents []ast.Node, body *ast.BlockStmt, declarations map[string]*ast.FuncDecl, visiting map[*ast.FuncDecl]bool, depth int) bool {
	if len(parents) == 0 || enclosedByFunctionLiteral(parents) {
		return false
	}
	switch parent := parents[len(parents)-1].(type) {
	case *ast.StarExpr:
		return sliceValueUseAllowed(parent, parents[:len(parents)-1])
	case *ast.BinaryExpr:
		return isNilComparison(parent)
	case *ast.CallExpr:
		index := argumentIndex(parent, ident)
		if index < 0 {
			return false
		}
		callee, ok := parent.Fun.(*ast.Ident)
		if !ok || parent.Ellipsis != token.NoPos {
			return false
		}
		declaration, found := declarations[callee.Name]
		if !found || declaration.Recv != nil || declaration.Body == nil || nameDeclaredAtLeastOnce(body, callee.Name) {
			return false
		}
		return parameterIsConfined(declaration, index, declarations, visiting, depth+1)
	default:
		return false
	}
}

// sliceValueUseAllowed judges a use of a slice value by its ancestors, returning true
// when the use keeps the backing store private to the frame.
//
// Takes value (ast.Node) which is the slice-valued node.
// Takes parents ([]ast.Node) which are its ancestors, nearest last.
//
// Returns true when the use keeps the backing store private.
func sliceValueUseAllowed(value ast.Node, parents []ast.Node) bool {
	for len(parents) > 0 {
		parent := parents[len(parents)-1]
		if _, isParen := parent.(*ast.ParenExpr); !isParen {
			break
		}
		value = parent
		parents = parents[:len(parents)-1]
	}
	if len(parents) == 0 {
		return false
	}
	switch parent := parents[len(parents)-1].(type) {
	case *ast.CallExpr:
		return sliceValueCallAllowed(parent, value)
	case *ast.IndexExpr:
		return parent.X == value
	case *ast.RangeStmt:
		return parent.X == value
	case *ast.AssignStmt:
		return containsExpr(parent.Lhs, value)
	case *ast.ValueSpec:
		return containsIdent(parent.Names, value)
	case *ast.BinaryExpr:
		return isNilComparison(parent)
	default:
		return false
	}
}

// sliceValueCallAllowed judges a slice value passed to a call: only the builtins that
// copy or measure it, appending to it, or spreading it into an append keep the backing
// private.
//
// Takes call (*ast.CallExpr) which is the call.
// Takes value (ast.Node) which is the argument.
//
// Returns true when the call cannot retain the backing.
func sliceValueCallAllowed(call *ast.CallExpr, value ast.Node) bool {
	callee, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	index := argumentIndex(call, value)
	if index < 0 {
		return false
	}
	switch callee.Name {
	case "len", "cap", "string", "copy":
		return true
	case "append":
		return index == 0 || (index == len(call.Args)-1 && call.Ellipsis != token.NoPos)
	default:
		return false
	}
}

// isNilComparison reports whether the binary expression compares against the nil literal.
//
// Takes expression (*ast.BinaryExpr) which is the comparison.
//
// Returns true for `x == nil` and `x != nil`.
func isNilComparison(expression *ast.BinaryExpr) bool {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return false
	}
	return isNilIdent(expression.X) || isNilIdent(expression.Y)
}

// isNilIdent reports whether expression is the nil identifier.
//
// Takes expression (ast.Expr) which is the expression to test.
//
// Returns bool which is true when expression is the nil literal.
func isNilIdent(expression ast.Expr) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == "nil"
}

// isSelectorField reports whether ident is the field half of a selector, `x.ident`, which
// names a field rather than the variable.
//
// Takes ident (*ast.Ident) which is the identifier.
// Takes parents ([]ast.Node) which are its ancestors.
//
// Returns true when ident is a selected field or method.
func isSelectorField(ident *ast.Ident, parents []ast.Node) bool {
	if len(parents) == 0 {
		return false
	}
	selector, ok := parents[len(parents)-1].(*ast.SelectorExpr)
	return ok && selector.Sel == ident
}

// argumentIndex returns the position of node among call's arguments, or -1 when node is
// the callee or not a direct argument.
//
// Takes call (*ast.CallExpr) which is the call.
// Takes node (ast.Node) which is the candidate argument.
//
// Returns the argument index or -1.
func argumentIndex(call *ast.CallExpr, node ast.Node) int {
	for i, argument := range call.Args {
		if argument == node {
			return i
		}
	}
	return -1
}

// enclosedByFunctionLiteral reports whether any ancestor is a function literal, which may
// outlive the frame and so lets anything it mentions out.
//
// Takes parents ([]ast.Node) which are the ancestors.
//
// Returns true when a function literal encloses the occurrence.
func enclosedByFunctionLiteral(parents []ast.Node) bool {
	for _, parent := range parents {
		if _, ok := parent.(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

// containsExpr reports whether value is one of the expressions.
//
// Takes expressions ([]ast.Expr) which is the list to search.
// Takes value (ast.Node) which is the node to find.
//
// Returns bool which is true when value appears in the list.
func containsExpr(expressions []ast.Expr, value ast.Node) bool {
	for _, expression := range expressions {
		if expression == value {
			return true
		}
	}
	return false
}

// containsIdent reports whether value is one of the identifiers.
//
// Takes identifiers ([]*ast.Ident) which is the list to search.
// Takes value (ast.Node) which is the node to find.
//
// Returns bool which is true when value appears in the list.
func containsIdent(identifiers []*ast.Ident, value ast.Node) bool {
	for _, ident := range identifiers {
		if ident == value {
			return true
		}
	}
	return false
}

// nameDeclaredAtLeastOnce reports whether body declares a local named name.
//
// Takes body (*ast.BlockStmt) which is searched.
// Takes name (string) which is the identifier.
//
// Returns true when any declaration site introduces name.
func nameDeclaredAtLeastOnce(body *ast.BlockStmt, name string) bool {
	found := false
	tally := func(ident *ast.Ident) bool {
		if ident == nil || ident.Name != name {
			return false
		}
		found = true
		return true
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		return !countDeclarationSite(node, tally)
	})
	return found
}

// sliceBackingEscapes reports whether the slice backing store of the named local can be
// reached from outside the frame.
//
// Takes name (string) which is the local.
// Takes body (*ast.BlockStmt) which is the function body.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
//
// Returns true when some use may let the backing store outlive the frame.
func sliceBackingEscapes(name string, body *ast.BlockStmt, declarations map[string]*ast.FuncDecl) bool {
	escapes := false
	walkWithParents(body, func(node ast.Node, parents []ast.Node) bool {
		if escapes {
			return false
		}
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Name != name || isSelectorField(ident, parents) {
			return true
		}
		if !localSliceUseAllowed(ident, parents, body, declarations) {
			escapes = true
		}
		return true
	})
	return escapes
}

// localSliceUseAllowed judges one occurrence of a slice-valued local: an address taken of
// it must go straight to a confined callee parameter; any other use must keep the backing
// private under sliceValueUseAllowed().
//
// Takes ident (*ast.Ident) which is the occurrence.
// Takes parents ([]ast.Node) which are its ancestors, nearest last.
// Takes body (*ast.BlockStmt) which is the enclosing function body.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
//
// Returns true when the use keeps the backing private.
func localSliceUseAllowed(ident *ast.Ident, parents []ast.Node, body *ast.BlockStmt, declarations map[string]*ast.FuncDecl) bool {
	if len(parents) == 0 || enclosedByFunctionLiteral(parents) {
		return false
	}
	unary, ok := parents[len(parents)-1].(*ast.UnaryExpr)
	if !ok {
		return sliceValueUseAllowed(ident, parents)
	}
	if unary.Op != token.AND || len(parents) < 2 {
		return false
	}
	call, isCall := parents[len(parents)-2].(*ast.CallExpr)
	if !isCall {
		return false
	}
	index := argumentIndex(call, unary)
	return index >= 0 && addressArgumentConfined(call, index, body, declarations)
}

// isCopyingStringConversion reports whether expression is `string(name)`, a conversion
// that copies the bytes it reads.
//
// Takes expression (ast.Expr) which is the candidate.
// Takes name (string) which is the local.
//
// Returns true for a string conversion applied directly to the local.
func isCopyingStringConversion(expression ast.Expr, name string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Ellipsis != token.NoPos {
		return false
	}
	callee, isIdent := call.Fun.(*ast.Ident)
	argument, argumentIsIdent := call.Args[0].(*ast.Ident)
	return isIdent && callee.Name == "string" && argumentIsIdent && argument.Name == name
}
