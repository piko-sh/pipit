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
	// appendArgCount is the number of arguments in the single-element append form `append(s,
	// v)` that the lowering handles; a spread append has the same shape.
	appendArgCount = 2
)

// MatchShape reports whether `lhs = rhs` has the `x = append(y, v)` shape: an identifier
// on the left and a two-argument call of the append builtin on the right.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
// Takes rightHandSide (ast.Expr) which is the assigned expression.
//
// Returns *ast.Ident which is the target.
// Returns *ast.CallExpr which is the append call.
// Returns bool which is false when the shape does not match.
func MatchShape(leftHandSide, rightHandSide ast.Expr) (*ast.Ident, *ast.CallExpr, bool) {
	lhsIdent, ok := leftHandSide.(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	callRHS, ok := rightHandSide.(*ast.CallExpr)
	if !ok {
		return nil, nil, false
	}
	if len(callRHS.Args) != appendArgCount {
		return nil, nil, false
	}
	funIdent, ok := callRHS.Fun.(*ast.Ident)
	if !ok || funIdent.Name != "append" {
		return nil, nil, false
	}
	return lhsIdent, callRHS, true
}

// SameSlice reports whether the append's first argument names the same object as the
// assignment target, which is what makes the append a candidate for in-place growth.
//
// Takes info (*types.Info) which resolves identifiers to objects.
// Takes lhsIdent (*ast.Ident) which is the assignment target.
// Takes callRHS (*ast.CallExpr) which is the append call.
//
// Returns bool which is true when both identifiers denote one object.
func SameSlice(info *types.Info, lhsIdent *ast.Ident, callRHS *ast.CallExpr) bool {
	rhsIdent, ok := callRHS.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	lhsObject := info.ObjectOf(lhsIdent)
	if lhsObject == nil {
		return false
	}
	return lhsObject == info.ObjectOf(rhsIdent)
}

// ElementKind returns the underlying element type name of the slice being appended to and
// whether the append spreads its second argument.
//
// Takes info (*types.Info) which resolves the slice type.
// Takes callRHS (*ast.CallExpr) which is the append call.
//
// Returns kindName (string) which is the element's underlying type string.
// Returns spread (bool) which is true for `append(s, t...)`.
// Returns ok (bool) which is false when the first argument is not a slice.
func ElementKind(info *types.Info, callRHS *ast.CallExpr) (kindName string, spread bool, ok bool) {
	sliceTypeInfo, found := info.Types[callRHS.Args[0]]
	if !found || sliceTypeInfo.Type == nil {
		return "", false, false
	}
	sliceType, sliceOk := sliceTypeInfo.Type.Underlying().(*types.Slice)
	if !sliceOk {
		return "", false, false
	}
	elementName := sliceType.Elem().Underlying().String()
	spread = callRHS.Ellipsis.IsValid()
	return elementName, spread, true
}
