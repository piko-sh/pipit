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

package appendlower

import (
	"go/ast"
	"go/types"
)

const (
	// blankIdentifier is the Go blank identifier, which never names a slice worth tracking.
	blankIdentifier = "_"
)

// CollectAliases returns the names of slice values in body whose header may be shared
// with another handle, so an append through them must not be lowered to the in-place
// form.
//
// Takes info (*types.Info) which resolves parameter types; nil falls back to the syntax.
// Takes parameters (*ast.FieldList) which is the function's parameter list; nil for none.
// Takes body (*ast.BlockStmt) which is the function body; nil for none.
//
// Returns map[string]bool which is the aliased name set, or nil when nothing is aliased.
func CollectAliases(info *types.Info, parameters *ast.FieldList, body *ast.BlockStmt) map[string]bool {
	aliased := make(map[string]bool)
	collectAliasesFromParameters(info, parameters, aliased)
	if body != nil {
		ast.Inspect(body, func(node ast.Node) bool {
			collectAliasesFromNode(node, aliased)
			return true
		})
	}
	if len(aliased) == 0 {
		return nil
	}
	return aliased
}

// collectAliasesFromParameters records every slice-typed parameter name.
//
// Takes info (*types.Info) which resolves the parameter types.
// Takes parameters (*ast.FieldList) which is the parameter list.
// Takes aliased (map[string]bool) which receives the names.
func collectAliasesFromParameters(info *types.Info, parameters *ast.FieldList, aliased map[string]bool) {
	if parameters == nil {
		return
	}
	for _, field := range parameters.List {
		if !fieldIsSlice(info, field) {
			continue
		}
		for _, name := range field.Names {
			if name == nil || name.Name == blankIdentifier {
				continue
			}
			aliased[name.Name] = true
		}
	}
}

// fieldIsSlice reports whether a parameter field has slice type, by go/types when info
// covers the field and by syntax otherwise.
//
// Takes info (*types.Info) which resolves the field type; may be nil.
// Takes field (*ast.Field) which is the parameter field.
//
// Returns bool which is true for a slice-typed field.
func fieldIsSlice(info *types.Info, field *ast.Field) bool {
	if info != nil && field.Type != nil {
		if typeInfo, ok := info.Types[field.Type]; ok && typeInfo.Type != nil {
			_, isSlice := typeInfo.Type.Underlying().(*types.Slice)
			return isSlice
		}
	}
	arrayType, ok := field.Type.(*ast.ArrayType)
	if !ok {
		return false
	}
	return arrayType.Len == nil
}

// collectAliasesFromNode records the aliasing forms one AST node can introduce.
//
// Takes node (ast.Node) which is the node under inspection.
// Takes aliased (map[string]bool) which receives the names.
func collectAliasesFromNode(node ast.Node, aliased map[string]bool) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		recordAssignRHS(typed, aliased)
	case *ast.UnaryExpr:
		recordAddressOf(typed, aliased)
	case *ast.ValueSpec:
		recordVarDeclValues(typed, aliased)
	}
}

// recordAssignRHS records every identifier on the right-hand side of an assignment: its
// header is now shared with the left-hand side.
//
// Takes statement (*ast.AssignStmt) which is the assignment.
// Takes aliased (map[string]bool) which receives the names.
func recordAssignRHS(statement *ast.AssignStmt, aliased map[string]bool) {
	for _, expression := range statement.Rhs {
		recordIdent(expression, aliased)
	}
}

// recordAddressOf records the operand of a unary & expression.
//
// Takes expression (*ast.UnaryExpr) which is the unary expression.
// Takes aliased (map[string]bool) which receives the name.
func recordAddressOf(expression *ast.UnaryExpr, aliased map[string]bool) {
	if expression.Op.String() != "&" {
		return
	}
	recordIdent(expression.X, aliased)
}

// recordVarDeclValues records every identifier used as a var declaration's value.
//
// Takes spec (*ast.ValueSpec) which is the declaration.
// Takes aliased (map[string]bool) which receives the names.
func recordVarDeclValues(spec *ast.ValueSpec, aliased map[string]bool) {
	for _, value := range spec.Values {
		recordIdent(value, aliased)
	}
}

// recordIdent records expression when it is a named, non-blank identifier.
//
// Takes expression (ast.Expr) which is the candidate.
// Takes aliased (map[string]bool) which receives the name.
func recordIdent(expression ast.Expr, aliased map[string]bool) {
	ident, ok := expression.(*ast.Ident)
	if !ok {
		return
	}
	if ident.Name == "" || ident.Name == blankIdentifier {
		return
	}
	aliased[ident.Name] = true
}
