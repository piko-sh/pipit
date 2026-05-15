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
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// starAppendArgCount is the required argument count for an append(*p, value) expression
	// that matches the fused star-append-byte super-instruction.
	starAppendArgCount = 2
)

// tryCompileStructIntoCollection detects the pattern collection[index] =
// StructType{fields...} and compiles it as an assign-through, writing fields directly
// into the addressable slice or array element. This avoids allocating a temporary struct
// via reflect.New.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
// Takes rightHandSide (ast.Expr) which is the right-hand side expression.
//
// Returns the destination VarLocation, whether the optimisation was applied, and any
// compilation error.
func (c *Compiler) tryCompileStructIntoCollection(ctx context.Context, leftHandSide ast.Expr, rightHandSide ast.Expr) (program.VarLocation, bool, error) {
	indexExpression, ok := leftHandSide.(*ast.IndexExpr)
	if !ok {
		return program.VarLocation{}, false, nil
	}

	compositeLiteral, ok := rightHandSide.(*ast.CompositeLit)
	if !ok {
		return program.VarLocation{}, false, nil
	}

	reflectType, ok := c.structIntoCollectionShape(ctx, indexExpression, compositeLiteral)
	if !ok {
		return program.VarLocation{}, false, nil
	}

	collectionLocation, err := c.compileExpression(ctx, indexExpression.X)
	if err != nil {
		return program.VarLocation{}, true, err
	}

	indexLocation, err := c.compileExpression(ctx, indexExpression.Index)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if indexLocation.Kind != isa.RegisterInt {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringStructIntoCollection, "index-kind", "kind", indexLocation.Kind.String())
		return program.VarLocation{}, false, nil
	}

	destination := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	c.setDebugPosition(ctx, indexExpression.Lbrack)
	program.Emit(c.Function, isa.OpIndex, destination, collectionLocation.Register, indexLocation.Register)

	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2SetZero), destination)

	for i, element := range compositeLiteral.Elts {
		if err := c.compileStructField(ctx, destination, i, element, compositeLiteral, reflectType); err != nil {
			return program.VarLocation{}, true, err
		}
	}

	return program.VarLocation{Register: destination, Kind: isa.RegisterGeneral}, true, nil
}

// structIntoCollectionShape checks that the literal is a struct and the collection a
// slice or array, noting the failed gate on the lowering trace.
//
// Takes indexExpression (*ast.IndexExpr) which is the assignment target.
// Takes compositeLiteral (*ast.CompositeLit) which is the assigned literal.
//
// Returns reflect.Type which is the struct type.
// Returns bool which is false on refusal.
func (c *Compiler) structIntoCollectionShape(ctx context.Context, indexExpression *ast.IndexExpr, compositeLiteral *ast.CompositeLit) (reflect.Type, bool) {
	literalTypeInfo, ok := c.Info.Types[compositeLiteral]
	if !ok {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringStructIntoCollection, "literal-type")
		return nil, false
	}
	reflectType := c.TypeToReflect(ctx, literalTypeInfo.Type)
	if reflectType.Kind() != reflect.Struct {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringStructIntoCollection, "literal-kind", "kind", reflectType.Kind().String())
		return nil, false
	}
	collectionTypeInfo, ok := c.Info.Types[indexExpression.X]
	if !ok {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringStructIntoCollection, "collection-type")
		return nil, false
	}
	collectionType := collectionTypeInfo.Type.Underlying()
	switch collectionType.(type) {
	case *types.Slice, *types.Array:
		return reflectType, true
	default:
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringStructIntoCollection, "collection-kind", "type", fmt.Sprint(collectionType))
		return nil, false
	}
}

// tryCompileStarAppendByteFast detects `*p = append(*p, b)` where p is a `*[]byte` and
// emits the fused isa.SubOpStarAppendByteFast opcode.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
// Takes rightHandSide (ast.Expr) which is the value expression.
//
// Returns the destination location and true when the fusion fired, or (zero, false, nil)
// for non-matching shapes.
func (c *Compiler) tryCompileStarAppendByteFast(ctx context.Context, leftHandSide ast.Expr, rightHandSide ast.Expr) (program.VarLocation, bool, error) {
	starLHS, callRHS, ok := matchStarAppendByteShape(leftHandSide, rightHandSide)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	if !c.matchStarAppendByteIdentities(starLHS, callRHS) {
		return program.VarLocation{}, false, nil
	}
	if !c.checkStarAppendByteSliceType(starLHS) {
		return program.VarLocation{}, false, nil
	}
	return c.emitStarAppendByteFast(ctx, starLHS, callRHS)
}

// matchStarAppendByteIdentities verifies that the first append argument is *p with the
// same pointer identifier as the LHS so the rewrite is SSA-equivalent without effect
// analysis.
//
// Takes starLHS (*ast.StarExpr) which is the LHS star expression of the assignment.
// Takes callRHS (*ast.CallExpr) which is the RHS append call expression.
//
// Returns true when both identifiers reference the same pointer object.
func (c *Compiler) matchStarAppendByteIdentities(starLHS *ast.StarExpr, callRHS *ast.CallExpr) bool {
	starArg0, ok := callRHS.Args[0].(*ast.StarExpr)
	if !ok {
		return false
	}
	lhsIdent, ok := starLHS.X.(*ast.Ident)
	if !ok {
		return false
	}
	rhsIdent, ok := starArg0.X.(*ast.Ident)
	if !ok {
		return false
	}
	return c.Info.ObjectOf(lhsIdent) != nil && c.Info.ObjectOf(lhsIdent) == c.Info.ObjectOf(rhsIdent)
}

// checkStarAppendByteSliceType confirms that the LHS pointer's element type is []byte (or
// []uint8 alias).
//
// Takes starLHS (*ast.StarExpr) which is the LHS star expression whose underlying
// pointer-to-slice element type is inspected.
//
// Returns true when the element type is byte or uint8.
func (c *Compiler) checkStarAppendByteSliceType(starLHS *ast.StarExpr) bool {
	pointerTypeInfo, ok := c.Info.Types[starLHS.X]
	if !ok || pointerTypeInfo.Type == nil {
		return false
	}
	pointerType, ok := pointerTypeInfo.Type.Underlying().(*types.Pointer)
	if !ok {
		return false
	}
	sliceType, ok := pointerType.Elem().Underlying().(*types.Slice)
	if !ok {
		return false
	}
	elementName := sliceType.Elem().Underlying().String()
	return elementName == "byte" || elementName == "uint8"
}

// emitStarAppendByteFast compiles the operands and emits the fused star-append-byte
// super-instruction.
//
// Takes starLHS (*ast.StarExpr) which is the LHS star expression of the assignment.
// Takes callRHS (*ast.CallExpr) which is the RHS append call expression.
//
// Returns the pointer location holding the appended slice and true when the fast path was
// emitted, or an error when sub-expression compilation fails.
func (c *Compiler) emitStarAppendByteFast(ctx context.Context, starLHS *ast.StarExpr, callRHS *ast.CallExpr) (program.VarLocation, bool, error) {
	pointerLocation, err := c.compileExpression(ctx, starLHS.X)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if pointerLocation.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, false, nil
	}
	valueLocation, err := c.compileExpression(ctx, callRHS.Args[1])
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if callRHS.Ellipsis != token.NoPos {
		return c.emitStarAppendByteSpread(ctx, callRHS, pointerLocation, valueLocation)
	}
	if valueLocation.Kind != isa.RegisterUint {
		return program.VarLocation{}, false, nil
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpStarAppendByteFast), pointerLocation.Register, valueLocation.Register)
	return pointerLocation, true, nil
}

// emitStarAppendByteSpread emits the spread (`append(*p, source...)`) variant, validating
// the source slice's element type is byte.
//
// Takes callRHS (*ast.CallExpr) which is the RHS append call expression.
// Takes pointerLocation (VarLocation) which holds the destination pointer.
// Takes valueLocation (VarLocation) which holds the source slice.
//
// Returns the pointer location after the appended store, true when the spread fast path
// was emitted, or an error when value boxing fails.
func (c *Compiler) emitStarAppendByteSpread(ctx context.Context, callRHS *ast.CallExpr, pointerLocation program.VarLocation, valueLocation program.VarLocation) (program.VarLocation, bool, error) {
	valueType, ok := c.Info.Types[callRHS.Args[1]]
	if !ok || valueType.Type == nil {
		return program.VarLocation{}, false, nil
	}
	sourceSlice, ok := valueType.Type.Underlying().(*types.Slice)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	elementName := sourceSlice.Elem().Underlying().String()
	if elementName != "byte" && elementName != "uint8" {
		return program.VarLocation{}, false, nil
	}
	c.boxToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpStarAppendByteSpread), pointerLocation.Register, valueLocation.Register)
	return pointerLocation, true, nil
}

// matchStarAppendByteShape pattern-matches `*p = append(..., ...)` at the AST level.
//
// Takes leftHandSide (ast.Expr) which is the LHS expression of the assignment.
// Takes rightHandSide (ast.Expr) which is the RHS expression of the assignment.
//
// Returns the LHS star expression and the RHS append call expression when the shape
// matches (otherwise nil values), and a bool that is true when the shape matches.
func matchStarAppendByteShape(leftHandSide, rightHandSide ast.Expr) (*ast.StarExpr, *ast.CallExpr, bool) {
	starLHS, ok := leftHandSide.(*ast.StarExpr)
	if !ok {
		return nil, nil, false
	}
	callRHS, ok := rightHandSide.(*ast.CallExpr)
	if !ok {
		return nil, nil, false
	}
	if len(callRHS.Args) != starAppendArgCount {
		return nil, nil, false
	}
	funIdent, ok := callRHS.Fun.(*ast.Ident)
	if !ok || funIdent.Name != "append" {
		return nil, nil, false
	}
	return starLHS, callRHS, true
}
