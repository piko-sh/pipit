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

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// compoundSliceAccessArgs bundles the per-call inputs to emitCompoundSliceAccess so the
// helper stays under the per-function argument cap.
type compoundSliceAccessArgs struct {
	// valueLocation is the scratch element register for a get or the binary-op result
	// register for a set.
	valueLocation program.VarLocation

	// collectionLocation is the slice or array being read or written.
	collectionLocation program.VarLocation

	// indexLocation is the element index for the access.
	indexLocation program.VarLocation

	// elementRegisterKind is the register kind of the slice element.
	elementRegisterKind isa.RegisterKind

	// useDirectInt selects the tier-0 int direct opcode when both the slice and element are
	// int-banked.
	useDirectInt bool

	// useDirectTier1 selects the typed-direct tier-1 sub-op path.
	useDirectTier1 bool
}

// tryRewriteIndexRMWAssign detects the pattern m[k] = m[k] + delta (or the symmetric
// delta + m[k]) and rewrites it as a synthetic compound assignment so the downstream
// compound-assign path can emit the fused opMapAdd*Int single-probe opcode.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment.
//
// Returns the rewritten statement and true when the pattern matches, the original
// statement and false otherwise.
func (*Compiler) tryRewriteIndexRMWAssign(statement *ast.AssignStmt) (*ast.AssignStmt, bool) {
	if statement.Tok != token.ASSIGN || len(statement.Lhs) != 1 || len(statement.Rhs) != 1 {
		return statement, false
	}
	indexLHS, ok := statement.Lhs[0].(*ast.IndexExpr)
	if !ok {
		return statement, false
	}
	binary, ok := statement.Rhs[0].(*ast.BinaryExpr)
	if !ok || binary.Op != token.ADD {
		return statement, false
	}
	var delta ast.Expr
	switch {
	case exprStructurallyEqual(binary.X, indexLHS):
		delta = binary.Y
	case exprStructurallyEqual(binary.Y, indexLHS):
		delta = binary.X
	default:
		return statement, false
	}
	rewritten := &ast.AssignStmt{
		Lhs:    statement.Lhs,
		TokPos: statement.TokPos,
		Tok:    token.ADD_ASSIGN,
		Rhs:    []ast.Expr{delta},
	}
	return rewritten, true
}

// compileCompoundAssign compiles a compound assignment (e.g. x += 5).
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement with a compound
// operator.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssign(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	binaryOperation := compoundToOp(statement.Tok)
	leftHandSide := statement.Lhs[0]

	switch target := leftHandSide.(type) {
	case *ast.Ident:
		return c.compileCompoundAssignIdent(ctx, target, statement.Rhs[0], binaryOperation)
	case *ast.IndexExpr:
		return c.compileCompoundAssignIndex(ctx, target, statement.Rhs[0], binaryOperation)
	case *ast.SelectorExpr:
		return c.compileCompoundAssignSelector(ctx, target, statement.Rhs[0], binaryOperation)
	case *ast.StarExpr:
		return c.compileCompoundAssignStar(ctx, target, statement.Rhs[0], binaryOperation)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported compound assignment target: %T at %s", leftHandSide, c.positionString(leftHandSide.Pos()))
	}
}

// compileCompoundAssignStar compiles `*p OP= rhs` by reading the value through the
// dereferenced pointer, applying the binary operator, and writing the result back through
// the same pointer. Mirrors the Ident path but uses compileStarExpression /
// compileStarAssign for the load and store sides.
//
// Takes target (*ast.StarExpr) which is the dereference expression on the left-hand side.
// Takes rightHandSide (ast.Expr) which is the right-hand operand.
// Takes binaryOperation (token.Token) which is the underlying binary operator.
//
// Returns the location of the computed result and any compilation error.
func (c *Compiler) compileCompoundAssignStar(ctx context.Context, target *ast.StarExpr, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	pointerLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	pointerLocation = c.pinOperand(ctx, pointerLocation, c.staticTypeOf(target.X))
	currentLocation, err := c.compileStarExpressionFrom(ctx, target, pointerLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	if err := c.compileStarAssignFrom(ctx, pointerLocation, resultLocation); err != nil {
		return program.VarLocation{}, err
	}
	return resultLocation, nil
}

// compileCompoundAssignIdent compiles identifier += v for upvalues, globals, and local
// variables.
//
// Takes target (*ast.Ident) which is the AST identifier being assigned to.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignIdent(ctx context.Context, target *ast.Ident, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	if target.Name == typemap.BlankIdentName {
		_, err := c.compileExpression(ctx, rightHandSide)
		return program.VarLocation{}, err
	}

	switch c.resolveIdentTarget(target) {
	case identTargetUpvalue:
		return c.compileCompoundAssignUpvalue(ctx, c.upvalueMap[target.Name], rightHandSide, binaryOperation)
	case identTargetGlobal:
		return c.compileCompoundAssignGlobal(ctx, c.globalVariables[target.Name], rightHandSide, binaryOperation)
	case identTargetLocal, identTargetUnknown:
	}

	destLocation, found := c.Scopes.LookupVar(target.Name)
	if !found {
		return program.VarLocation{}, fmt.Errorf("undefined variable: %s at %s", target.Name, c.positionString(target.Pos()))
	}
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}

	opLocation := destLocation
	switch {
	case destLocation.IsIndirect:
		readLocation, err := c.EmitIndirectRead(ctx, destLocation)
		if err != nil {
			return program.VarLocation{}, err
		}
		opLocation = readLocation
	case destLocation.IsSpilled:
		opLocation = c.emitReloadIfSpilled(ctx, destLocation)
	}

	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, opLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	var resultType types.Type
	if targetObject := c.Info.ObjectOf(target); targetObject != nil {
		c.EmitNarrowIntegerTruncation(resultLocation, targetObject.Type())
		resultType = targetObject.Type()
	}
	c.emitMoveTyped(ctx, destLocation, resultLocation, resultType)
	c.emitWriteSharedCellIfCaptured(ctx, destLocation)
	return destLocation, nil
}

// compileCompoundAssignUpvalue compiles compound assignment to a captured variable.
//
// Takes reference (upvalueReference) which is the upvalue reference identifying the
// captured variable.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignUpvalue(ctx context.Context, reference upvalueReference, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	currentRegister := c.Scopes.Alloc.AllocTemp(reference.kind)
	program.Emit(c.Function, isa.OpGetUpvalue, currentRegister, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
	currentLocation := program.VarLocation{Register: currentRegister, Kind: reference.kind}

	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	program.Emit(c.Function, isa.OpSetUpvalue, resultLocation.Register, safeconv.MustIntToUint8(reference.index), uint8(resultLocation.Kind))
	c.Scopes.Alloc.FreeTemp(reference.kind, currentRegister)
	return resultLocation, nil
}

// compileCompoundAssignGlobal compiles compound assignment to a package-level variable.
//
// Takes gv (GlobalVariableInfo) which holds the global store location for the target
// variable.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignGlobal(ctx context.Context, gv program.GlobalVariableInfo, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	currentLocation := c.emitGetGlobal(ctx, gv)
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.emitSetGlobal(ctx, gv, resultLocation)
	return resultLocation, nil
}

// compileCompoundAssignIndex compiles a[i] += v for maps and slices/arrays.
//
// Takes target (*ast.IndexExpr) which is the AST index expression representing the
// element being assigned.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignIndex(ctx context.Context, target *ast.IndexExpr, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	collectionLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	indexLocation, err := c.compileExpression(ctx, target.Index)
	if err != nil {
		return program.VarLocation{}, err
	}

	collectionType := c.Info.Types[target.X].Type.Underlying()
	if mapType, isMap := collectionType.(*types.Map); isMap {
		return c.compileCompoundAssignMap(ctx, mapType, collectionLocation, indexLocation, rightHandSide, binaryOperation)
	}

	c.ensureIntRegister(ctx, &indexLocation)
	return c.compileCompoundAssignSlice(ctx, collectionType, collectionLocation, indexLocation, rightHandSide, binaryOperation)
}

// compileCompoundAssignMap compiles m[k] += v for maps.
//
// Takes mapType (*types.Map) which is the go/types map type for selecting the fast path.
// Takes collectionLocation (VarLocation) which is the register location of the map
// collection.
// Takes indexLocation (VarLocation) which is the register location of the map key.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignMap(
	ctx context.Context,
	mapType *types.Map,
	collectionLocation, indexLocation program.VarLocation,
	rightHandSide ast.Expr,
	binaryOperation token.Token,
) (program.VarLocation, error) {
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}

	keyKind := c.kindFor(mapType.Key())
	valueKind := c.kindFor(mapType.Elem())
	if valueKind == isa.RegisterInt && rhsLocation.Kind == isa.RegisterInt && indexLocation.Kind == keyKind &&
		binaryOperation == token.ADD &&
		(keyKind == isa.RegisterInt || keyKind == isa.RegisterString) {
		fusedOp := isa.OpMapAddIntInt
		if keyKind == isa.RegisterString {
			fusedOp = isa.OpMapAddStringInt
		}
		program.Emit(c.Function, fusedOp, collectionLocation.Register, indexLocation.Register, rhsLocation.Register)
		return program.VarLocation{}, nil
	}
	if keyKind == isa.RegisterInt && valueKind == isa.RegisterInt && indexLocation.Kind == isa.RegisterInt && rhsLocation.Kind == isa.RegisterInt {
		currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		currentLocation := program.VarLocation{Register: currentRegister, Kind: isa.RegisterInt}
		c.EmitTyped(ctx, isa.OpMapGetIntInt, currentLocation, collectionLocation, indexLocation)
		resultLocation, binaryError := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
		if binaryError != nil {
			return program.VarLocation{}, binaryError
		}
		c.EmitTyped(ctx, isa.OpMapSetIntInt, collectionLocation, indexLocation, resultLocation)
		return program.VarLocation{}, nil
	}

	c.boxElementToGeneralTemp(ctx, &indexLocation)
	c.boxToGeneralTemp(ctx, &collectionLocation)
	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpMapIndex, currentRegister, collectionLocation.Register, indexLocation.Register)
	currentLocation := c.unboxForCompound(ctx, currentRegister, rhsLocation.Kind)

	resultLocation, binaryError := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if binaryError != nil {
		return program.VarLocation{}, binaryError
	}
	c.boxElementToGeneralTemp(ctx, &resultLocation)
	program.Emit(c.Function, isa.OpMapSet, collectionLocation.Register, indexLocation.Register, resultLocation.Register)
	return program.VarLocation{}, nil
}

// compileCompoundAssignSlice compiles a[i] += v for slices and arrays.
//
// Takes collectionType (types.Type) which is the go/types type of the slice or array
// collection.
// Takes collectionLocation (VarLocation) which is the register location of the slice or
// array.
// Takes indexLocation (VarLocation) which is the register location of the element index.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignSlice(
	ctx context.Context,
	collectionType types.Type,
	collectionLocation, indexLocation program.VarLocation,
	rightHandSide ast.Expr,
	binaryOperation token.Token,
) (program.VarLocation, error) {
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}

	if elementRegisterKind, ok := c.sliceElemRegisterKind(collectionType); ok && rhsLocation.Kind == elementRegisterKind && indexLocation.Kind == isa.RegisterInt {
		var elementType types.Type
		switch collection := collectionType.(type) {
		case *types.Slice:
			elementType = collection.Elem()
		case *types.Array:
			elementType = collection.Elem()
		}
		return c.compileCompoundAssignSliceTyped(ctx, collectionLocation, indexLocation, rhsLocation, elementRegisterKind, binaryOperation, elementType)
	}

	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpIndex, currentRegister, collectionLocation.Register, indexLocation.Register)
	currentLocation := c.unboxForCompound(ctx, currentRegister, rhsLocation.Kind)

	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneralTemp(ctx, &resultLocation)
	program.Emit(c.Function, isa.OpIndexSet, collectionLocation.Register, indexLocation.Register, resultLocation.Register)
	return program.VarLocation{}, nil
}

// compileCompoundAssignSliceTyped emits a typed slice compound assignment (int or float
// fast path).
//
// Takes collectionLocation (VarLocation) which is the register location of the slice or
// array.
// Takes indexLocation (VarLocation) which is the register location of the element index.
// Takes rhsLocation (VarLocation) which is the register location of the right-hand side
// value.
// Takes elementRegisterKind (isa.RegisterKind) which is the register kind of the slice
// element type.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
// Takes elementType (types.Type) which is the Go type of the slice element.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignSliceTyped(
	ctx context.Context,
	collectionLocation, indexLocation, rhsLocation program.VarLocation,
	elementRegisterKind isa.RegisterKind,
	binaryOperation token.Token,
	elementType types.Type,
) (program.VarLocation, error) {
	currentRegister := c.Scopes.Alloc.AllocTemp(elementRegisterKind)
	currentLocation := program.VarLocation{Register: currentRegister, Kind: elementRegisterKind}
	useDirectInt := collectionLocation.Kind == isa.RegisterSliceInt && elementRegisterKind == isa.RegisterInt
	useDirectTier1 := !useDirectInt &&
		isa.ElementKindForTypedSlice(collectionLocation.Kind) == elementRegisterKind &&
		isa.IsTypedSliceKind(collectionLocation.Kind)
	c.emitCompoundSliceGet(ctx, currentLocation, collectionLocation, indexLocation, elementRegisterKind, useDirectInt, useDirectTier1)
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}

	if elementType != nil {
		c.EmitNarrowIntegerTruncation(resultLocation, elementType)
	}
	c.emitCompoundSliceSet(ctx, collectionLocation, indexLocation, resultLocation, elementRegisterKind, useDirectInt, useDirectTier1)
	return program.VarLocation{}, nil
}

// emitCompoundSliceGet emits the read half of a compound slice assignment, dispatching to
// the direct-int, typed-tier1, or generic per-element-kind path based on the slice and
// element register kinds.
//
// Takes currentLocation (VarLocation) which receives the loaded element value.
// Takes collectionLocation (VarLocation) which is the slice or array being read.
// Takes indexLocation (VarLocation) which is the element index.
// Takes elementRegisterKind (isa.RegisterKind) which is the slice's element register
// kind.
// Takes useDirectInt (bool) which selects the tier-0 int direct path.
// Takes useDirectTier1 (bool) which selects the typed-tier1 path.
func (c *Compiler) emitCompoundSliceGet(
	ctx context.Context,
	currentLocation,
	collectionLocation,
	indexLocation program.VarLocation,
	elementRegisterKind isa.RegisterKind,
	useDirectInt,
	useDirectTier1 bool,
) {
	c.emitCompoundSliceAccess(ctx, isaselect.CompoundSliceGetOps, compoundSliceAccessArgs{
		valueLocation:       currentLocation,
		collectionLocation:  collectionLocation,
		indexLocation:       indexLocation,
		elementRegisterKind: elementRegisterKind,
		useDirectInt:        useDirectInt,
		useDirectTier1:      useDirectTier1,
	})
}

// emitCompoundSliceSet emits the write half of a compound slice assignment, mirroring
// emitCompoundSliceGet's dispatch strategy.
//
// Takes collectionLocation (VarLocation) which is the slice or array being written.
// Takes indexLocation (VarLocation) which is the element index.
// Takes resultLocation (VarLocation) which holds the value to store.
// Takes elementRegisterKind (isa.RegisterKind) which is the slice's element register
// kind.
// Takes useDirectInt (bool) which selects the tier-0 int direct path.
// Takes useDirectTier1 (bool) which selects the typed-tier1 path.
func (c *Compiler) emitCompoundSliceSet(
	ctx context.Context,
	collectionLocation,
	indexLocation,
	resultLocation program.VarLocation,
	elementRegisterKind isa.RegisterKind,
	useDirectInt,
	useDirectTier1 bool,
) {
	c.emitCompoundSliceAccess(ctx, isaselect.CompoundSliceSetOps, compoundSliceAccessArgs{
		valueLocation:       resultLocation,
		collectionLocation:  collectionLocation,
		indexLocation:       indexLocation,
		elementRegisterKind: elementRegisterKind,
		useDirectInt:        useDirectInt,
		useDirectTier1:      useDirectTier1,
	})
}

// emitCompoundSliceAccess is the shared dispatcher for the get/set halves of compound
// typed-slice assignment. valueLocation is the scratch element register for a get or the
// result-of-binary-op register for a set; the per-direction opcodes are supplied via ops.
//
// Takes ops (compoundSliceOps) which carries the per-direction opcode dispatch table.
// Takes args (compoundSliceAccessArgs) which carries the per-call inputs.
func (c *Compiler) emitCompoundSliceAccess(ctx context.Context, ops isaselect.CompoundSliceOps, args compoundSliceAccessArgs) {
	isGet := ops.DirectIntOpcode == isa.OpSliceGetIntDirect
	switch {
	case args.useDirectInt:
		if isGet {
			c.EmitTyped(ctx, ops.DirectIntOpcode, args.valueLocation, args.collectionLocation, args.indexLocation)
		} else {
			c.EmitTyped(ctx, ops.DirectIntOpcode, args.collectionLocation, args.indexLocation, args.valueLocation)
		}
		return
	case args.useDirectTier1:
		directSubOp, _ := ops.Tier1SubOp(args.collectionLocation.Kind)
		if isGet {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(directSubOp), args.valueLocation.Register, args.collectionLocation.Register)
			program.Emit(c.Function, isa.OpExt, args.indexLocation.Register, 0, 0)
		} else {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(directSubOp), args.collectionLocation.Register, args.indexLocation.Register)
			program.Emit(c.Function, isa.OpExt, args.valueLocation.Register, 0, 0)
		}
		return
	}
	op := ops.PerKind[args.elementRegisterKind]
	if op == 0 {
		return
	}
	if isGet {
		c.EmitTyped(ctx, op, args.valueLocation, args.collectionLocation, args.indexLocation)
	} else {
		c.EmitTyped(ctx, op, args.collectionLocation, args.indexLocation, args.valueLocation)
	}
}

// unboxForCompound optionally unboxes a general register for compound assignment when the
// RHS is a typed register.
//
// Takes generalRegister (uint8) which is the general register holding the boxed value.
// Takes rightHandSideKind (isa.RegisterKind) which is the register kind of the right-hand
// side operand.
//
// Returns a VarLocation with the unboxed value in a typed register, or the original
// general register location if the RHS is also general.
func (c *Compiler) unboxForCompound(_ context.Context, generalRegister uint8, rightHandSideKind isa.RegisterKind) program.VarLocation {
	if rightHandSideKind == isa.RegisterGeneral {
		return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
	}
	unboxed := c.Scopes.Alloc.AllocTemp(rightHandSideKind)
	program.Emit(c.Function, isa.OpUnpackInterface, unboxed, generalRegister, uint8(rightHandSideKind))
	return program.VarLocation{Register: unboxed, Kind: rightHandSideKind}
}

// compileCompoundAssignSelector compiles s.Field += v.
//
// Takes target (*ast.SelectorExpr) which is the AST selector expression identifying the
// struct field.
// Takes rightHandSide (ast.Expr) which is the AST expression on the right-hand side of
// the compound operator.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignSelector(ctx context.Context, target *ast.SelectorExpr, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	if c.isPackageQualifiedSelector(target) {
		return c.compileCompoundAssignPackageVar(ctx, target, rightHandSide, binaryOperation)
	}
	receiverLocation, err := c.compileExpression(ctx, target.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	selection := c.Info.Selections[target]
	if selection == nil {
		return program.VarLocation{}, fmt.Errorf("unresolved selector: %s", target.Sel.Name)
	}
	index := selection.Index()
	fieldIndex := safeconv.MustIntToUint8(index[len(index)-1])

	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}

	if location, ok, fastErr := c.tryCompileCompoundAssignSelectorFastPath(ctx, selection, receiverLocation, rhsLocation, binaryOperation); ok {
		return location, fastErr
	}

	if rhsLocation.Kind == isa.RegisterInt && len(index) == 1 {
		return c.compileCompoundAssignFieldInt(ctx, receiverLocation, rhsLocation, fieldIndex, binaryOperation)
	}

	if len(index) > 1 {
		return c.compileEmbeddedCompoundAssign(ctx, receiverLocation, target, index, rhsLocation, binaryOperation)
	}

	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpGetField, currentRegister, receiverLocation.Register, fieldIndex)
	currentLocation := c.unboxForCompound(ctx, currentRegister, rhsLocation.Kind)

	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneralTemp(ctx, &resultLocation)
	program.Emit(c.Function, isa.OpSetField, receiverLocation.Register, fieldIndex, resultLocation.Register)
	return program.VarLocation{}, nil
}

// compileEmbeddedCompoundAssign performs a compound assignment to a promoted field,
// walking the selection path to the leaf.
//
// Takes receiverLocation (VarLocation) which holds the lowered receiver.
// Takes target (*ast.SelectorExpr) which is the promoted field selector.
// Takes index ([]int) which is the selection path to the leaf field.
// Takes rhsLocation (VarLocation) which holds the right-hand value.
// Takes binaryOperation (token.Token) which is the compound operator.
//
// Returns the result location and any compilation error.
func (c *Compiler) compileEmbeddedCompoundAssign(
	ctx context.Context,
	receiverLocation program.VarLocation,
	target *ast.SelectorExpr,
	index []int,
	rhsLocation program.VarLocation,
	binaryOperation token.Token,
) (program.VarLocation, error) {
	pointerLocation := c.compileEmbeddedLeafPointerReusing(receiverLocation, target.X, index)
	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, currentRegister, pointerLocation.Register, 0)
	currentLocation := c.unboxForCompound(ctx, currentRegister, rhsLocation.Kind)

	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneralTemp(ctx, &resultLocation)
	program.Emit(c.Function, isa.OpSetField, pointerLocation.Register, isa.SentinelFieldDeref, resultLocation.Register)
	return program.VarLocation{}, nil
}

// tryCompileCompoundAssignSelectorFastPath attempts to emit the direct-unsafe fast-path
// compound-assign sequence for s.Field op= v. On success returns (location, true, err);
// otherwise returns (zero, false, nil) so the caller proceeds to the existing slow paths.
//
// The fast path requires both READ and WRITE eligibility for the matching register kind.
// The rhsLocation's kind must equal the field's register kind (otherwise a coercion would
// be needed and we route to the slow path which handles that via boxToGeneralTemp +
// isa.OpSetField).
//
// Takes selection (*types.Selection) which is the field selection.
// Takes receiverLocation (VarLocation) which is the general-bank register holding the
// struct.
// Takes rhsLocation (VarLocation) which is the value-side of the compound op.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location and true on success.
// Returns (zero, false, nil) when the fast path is not eligible.
func (c *Compiler) tryCompileCompoundAssignSelectorFastPath(
	ctx context.Context,
	selection *types.Selection,
	receiverLocation, rhsLocation program.VarLocation,
	binaryOperation token.Token,
) (program.VarLocation, bool, error) {
	if !isaselect.StructFieldFastPathKindEnabled(rhsLocation.Kind) || !isaselect.StructFieldFastPathWriteKindEnabled(rhsLocation.Kind) {
		return program.VarLocation{}, false, nil
	}
	if rhsLocation.Kind == isa.RegisterString {
		return program.VarLocation{}, false, nil
	}
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if isa.RegisterKind(layout.RegisterKind) != rhsLocation.Kind {
		return program.VarLocation{}, false, nil
	}
	useTier0 := fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx)
	getOp, getOpOk := isaselect.PickGetStructFieldTier0Op(rhsLocation.Kind)
	setOp, setOpOk := isaselect.PickSetStructFieldTier0Op(rhsLocation.Kind)
	getSub, getOk := isaselect.PickGetStructFieldUnsafeSubOp(rhsLocation.Kind)
	setSub, setOk := isaselect.PickSetStructFieldUnsafeSubOp(rhsLocation.Kind)
	if !getOk || !setOk {
		return program.VarLocation{}, false, nil
	}

	currentRegister := c.Scopes.Alloc.AllocTemp(rhsLocation.Kind)
	if useTier0 && getOpOk {
		program.Emit(c.Function, getOp, currentRegister, receiverLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
	} else {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(getSub), currentRegister, receiverLocation.Register)
		c.emitStructFieldLayoutExtension(layoutIdx)
	}

	currentLocation := program.VarLocation{Register: currentRegister, Kind: rhsLocation.Kind}
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if resultLocation.Kind != rhsLocation.Kind {
		return program.VarLocation{}, false, nil
	}
	if useTier0 && setOpOk {
		program.Emit(c.Function, setOp, receiverLocation.Register, resultLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
	} else {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(setSub), receiverLocation.Register, resultLocation.Register)
		c.emitStructFieldLayoutExtension(layoutIdx)
	}
	return program.VarLocation{}, true, nil
}

// compileCompoundAssignFieldInt emits the int fast path for s.Field += v where the field
// is a direct (non-embedded) int.
//
// Takes receiverLocation (VarLocation) which is the register location of the struct
// receiver.
// Takes rhsLocation (VarLocation) which is the register location of the right-hand side
// value.
// Takes fieldIndex (uint8) which is the bytecode field index within the struct.
// Takes binaryOperation (token.Token) which is the binary operator corresponding to the
// compound assignment.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileCompoundAssignFieldInt(ctx context.Context, receiverLocation, rhsLocation program.VarLocation, fieldIndex uint8, binaryOperation token.Token) (program.VarLocation, error) {
	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	currentLocation := program.VarLocation{Register: currentRegister, Kind: isa.RegisterInt}
	c.EmitTyped(ctx, isa.OpGetFieldInt, currentLocation, receiverLocation, rawOperand(fieldIndex))
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.EmitTyped(ctx, isa.OpSetFieldInt, receiverLocation, rawOperand(fieldIndex), resultLocation)
	return program.VarLocation{}, nil
}

// compileCompoundAssignPackageVar lowers `pkg.Var op= v` for a settable package variable
// of a registered Go package: the current value is read as the package symbol, unpacked
// into its static bank, combined with the operand, and written back through the
// variable's address (compileNativePackageVarAssign).
//
// Takes target (*ast.SelectorExpr) which is the package-qualified variable.
// Takes rightHandSide (ast.Expr) which is the operand.
// Takes binaryOperation (token.Token) which is the arithmetic operator.
//
// Returns the location holding the stored result.
func (c *Compiler) compileCompoundAssignPackageVar(ctx context.Context, target *ast.SelectorExpr, rightHandSide ast.Expr, binaryOperation token.Token) (program.VarLocation, error) {
	currentLocation, err := c.compileExpression(ctx, target)
	if err != nil {
		return program.VarLocation{}, err
	}
	staticType := c.staticTypeOf(target)
	if kind := c.kindFor(staticType); kind != isa.RegisterGeneral && currentLocation.Kind == isa.RegisterGeneral {
		currentLocation = c.unpackConversionArgument(currentLocation, kind)
		currentLocation.SourceType = c.exactReflectTypeForBoxing(staticType)
	}
	rhsLocation, err := c.compileExpression(ctx, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	resultLocation, err := c.emitBinaryOp(ctx, binaryOperation, currentLocation, rhsLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	handled, err := c.compileNativePackageVarAssign(ctx, target, resultLocation)
	if err != nil {
		return program.VarLocation{}, err
	}
	if !handled {
		return program.VarLocation{}, fmt.Errorf("unresolved selector: %s", target.Sel.Name)
	}
	return resultLocation, nil
}

// exprStructurallyEqual reports whether two ast.Expr nodes match structurally.
//
// Compares identifiers, literal values, selector names, and index expressions.
// Deliberately covers only the safe pure-read shapes (Ident, BasicLit, IndexExpr,
// SelectorExpr, ParenExpr); anything more complex returns false. Used to recognise m[k] =
// m[k] + v for the map-add fusion.
//
// Takes a (ast.Expr) which is the first expression to compare.
// Takes b (ast.Expr) which is the second expression to compare.
//
// Returns bool indicating whether the expressions are structurally identical for the
// supported subset.
func exprStructurallyEqual(a, b ast.Expr) bool {
	if a == nil || b == nil {
		return a == b
	}
	switch ax := a.(type) {
	case *ast.Ident:
		bx, ok := b.(*ast.Ident)
		return ok && ax.Name == bx.Name
	case *ast.BasicLit:
		bx, ok := b.(*ast.BasicLit)
		return ok && ax.Kind == bx.Kind && ax.Value == bx.Value
	case *ast.IndexExpr:
		bx, ok := b.(*ast.IndexExpr)
		return ok && exprStructurallyEqual(ax.X, bx.X) && exprStructurallyEqual(ax.Index, bx.Index)
	case *ast.SelectorExpr:
		bx, ok := b.(*ast.SelectorExpr)
		return ok && exprStructurallyEqual(ax.X, bx.X) && ax.Sel.Name == bx.Sel.Name
	case *ast.ParenExpr:
		bx, ok := b.(*ast.ParenExpr)
		return ok && exprStructurallyEqual(ax.X, bx.X)
	}
	return false
}
