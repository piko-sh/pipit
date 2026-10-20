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

package sliceshare

import (
	"go/ast"
	"go/types"
)

// ConversionOperand returns the operand of call when the conversion preserves its
// slice header.
//
// Takes info (*types.Info) which recognises conversions; nil recognises none.
// Takes call (*ast.CallExpr) which is the candidate conversion.
//
// Returns ast.Expr which is the operand.
// Returns bool which is true for a conversion that shares its operand's backing.
func ConversionOperand(info *types.Info, call *ast.CallExpr) (ast.Expr, bool) {
	if info == nil || len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return nil, false
	}
	target, found := info.Types[call.Fun]
	if !found || !target.IsType() || target.Type == nil {
		return nil, false
	}
	switch target.Type.Underlying().(type) {
	case *types.Basic, *types.Array:
		return nil, false
	}
	operand := call.Args[0]
	if operandType := info.TypeOf(operand); operandType != nil {
		if _, basic := operandType.Underlying().(*types.Basic); basic {
			return nil, false
		}
	}
	return operand, true
}

// StripSharing removes the parentheses and sharing conversions around expression, leaving
// the expression whose header the whole expression passes on.
//
// Takes info (*types.Info) which recognises conversions; nil removes parentheses only.
// Takes expression (ast.Expr) which is the expression to strip.
//
// Returns ast.Expr which is the innermost expression that is not a parenthesis or a
// sharing conversion.
func StripSharing(info *types.Info, expression ast.Expr) ast.Expr {
	for {
		expression = ast.Unparen(expression)
		call, ok := expression.(*ast.CallExpr)
		if !ok {
			return expression
		}
		operand, sharing := ConversionOperand(info, call)
		if !sharing {
			return expression
		}
		expression = operand
	}
}

// BuiltinName returns the name of the predeclared function call invokes. Without type
// information the callee is matched by name, so a shadowed builtin counts as one.
//
// Takes info (*types.Info) which resolves the callee; may be nil.
// Takes call (*ast.CallExpr) which is the call.
//
// Returns string which is the builtin's name.
// Returns bool which is true when call invokes a builtin.
func BuiltinName(info *types.Info, call *ast.CallExpr) (string, bool) {
	ident, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return "", false
	}
	if info != nil {
		if object, found := info.Uses[ident]; found {
			_, builtin := object.(*types.Builtin)
			return ident.Name, builtin
		}
	}
	_, builtin := types.Universe.Lookup(ident.Name).(*types.Builtin)
	return ident.Name, builtin
}
