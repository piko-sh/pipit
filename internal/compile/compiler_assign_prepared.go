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
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// preparedTargetKind says which shape of assignment target was prepared.
type preparedTargetKind uint8

const (
	// preparedNone means the target's operands were not evaluated ahead of time; the store
	// goes through emitAssignTarget as usual (identifiers, blank).
	preparedNone preparedTargetKind = iota

	// preparedIndex is an index target with its collection and index already in temps.
	preparedIndex

	// preparedSelector is a field target with its receiver already in a temp.
	preparedSelector

	// preparedStar is a pointer target with the pointer already in a temp.
	preparedStar
)

// preparedTarget is an assignment target whose LHS operands were evaluated before the RHS
// to match Go's evaluation order.
type preparedTarget struct {
	// expr is the target expression.
	expr ast.Expr

	// base holds the collection, receiver or pointer for the prepared kinds.
	base program.VarLocation

	// index holds the index or key for preparedIndex.
	index program.VarLocation

	// kind says which operands were prepared.
	kind preparedTargetKind
}

// assignmentNeedsOrdering reports whether an assignment must evaluate its target's
// operands before its value to match Go.
//
// Takes leftHandSide (ast.Expr) which is the target.
// Takes rightHandSide (ast.Expr) which is the value.
//
// Returns true when the target is not a plain identifier and one side contains a call or
// a channel receive.
func (c *Compiler) assignmentNeedsOrdering(leftHandSide, rightHandSide ast.Expr) bool {
	switch leftHandSide.(type) {
	case *ast.IndexExpr, *ast.SelectorExpr, *ast.StarExpr:
	default:
		return false
	}
	return c.exprHasCallOrReceive(leftHandSide) || c.exprHasCallOrReceive(rightHandSide)
}

// exprHasCallOrReceive reports whether expr contains a function call (not a type
// conversion) or a channel receive.
//
// Takes expr (ast.Expr) which is the expression to scan.
//
// Returns true when evaluating expr can have observable order.
func (c *Compiler) exprHasCallOrReceive(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found {
			return false
		}
		switch n := node.(type) {
		case *ast.CallExpr:
			if c.Info != nil {
				if tv, ok := c.Info.Types[n.Fun]; ok && tv.IsType() {
					return true
				}
			}
			found = true
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				found = true
			}
		case *ast.FuncLit:
			return false
		}
		return !found
	})
	return found
}

// prepareAssignTarget evaluates the operands of an assignment target into temporaries.
//
// Takes leftHandSide (ast.Expr) which is the target.
//
// Returns the prepared target, or an error when an operand fails to compile.
func (c *Compiler) prepareAssignTarget(ctx context.Context, leftHandSide ast.Expr) (preparedTarget, error) {
	switch target := leftHandSide.(type) {
	case *ast.IndexExpr:
		collection, err := c.prepareContainerBase(ctx, target.X)
		if err != nil {
			return preparedTarget{}, err
		}
		index, err := c.compileExpression(ctx, target.Index)
		if err != nil {
			return preparedTarget{}, err
		}
		index = c.pinOperand(ctx, index, c.staticTypeOf(target.Index))
		return preparedTarget{expr: target, base: collection, index: index, kind: preparedIndex}, nil
	case *ast.SelectorExpr:
		receiver, err := c.prepareContainerBase(ctx, target.X)
		if err != nil {
			return preparedTarget{}, err
		}
		c.boxToGeneral(ctx, &receiver)
		return preparedTarget{expr: target, base: receiver, index: program.VarLocation{}, kind: preparedSelector}, nil
	case *ast.StarExpr:
		pointer, err := c.compileExpression(ctx, target.X)
		if err != nil {
			return preparedTarget{}, err
		}
		return preparedTarget{expr: target, base: c.pinOperand(ctx, pointer, c.staticTypeOf(target.X)), index: program.VarLocation{}, kind: preparedStar}, nil
	default:
		return preparedTarget{expr: leftHandSide, base: program.VarLocation{}, index: program.VarLocation{}, kind: preparedNone}, nil
	}
}

// pinOperand makes a prepared operand safe to hold across the evaluation of the value and
// the other stores of a tuple.
//
// Scalars and reference-like values are copied into a fresh register. Structs and arrays
// are held as-is because a copy would break addressable stores. Indirect or spilled
// variables are read first.
//
// Takes location (program.VarLocation) which is the operand.
// Takes staticType (types.Type) which is the operand's static type, possibly nil.
//
// Returns program.VarLocation which is the location to store through.
func (c *Compiler) pinOperand(ctx context.Context, location program.VarLocation, staticType types.Type) program.VarLocation {
	if location.IsIndirect {
		read, err := c.EmitIndirectRead(ctx, location)
		if err != nil {
			c.recordStickyError(err)
			return location
		}
		location = read
	}
	if location.IsSpilled {
		location = c.emitReloadIfSpilled(ctx, location)
	}
	if location.Kind == isa.RegisterGeneral && !isReferenceLikeType(staticType) {
		return location
	}
	copied := program.VarLocation{Register: c.Scopes.Alloc.Alloc(location.Kind), Kind: location.Kind, SourceType: location.SourceType}
	c.emitMove(ctx, copied, location)
	return copied
}

// prepareContainerBase evaluates the collection or receiver of an assignment target ahead
// of the value.
//
// Reference-like bases are compiled and copied into a fresh register. Struct or array
// bases are compiled as their address instead, because a value copy would break the
// store.
//
// Takes base (ast.Expr) which is the target's collection or receiver expression.
//
// Returns the location to store through, or an error when it fails to compile.
func (c *Compiler) prepareContainerBase(ctx context.Context, base ast.Expr) (program.VarLocation, error) {
	staticType := c.staticTypeOf(base)
	if staticType != nil && !isReferenceLikeType(staticType) {
		switch staticType.Underlying().(type) {
		case *types.Struct, *types.Array:
			return c.addressOfAssignBase(ctx, base)
		default:
		}
	}
	location, err := c.compileExpression(ctx, base)
	if err != nil {
		return program.VarLocation{}, err
	}
	return c.pinOperand(ctx, location, staticType), nil
}

// addressOfAssignBase produces a pointer to the storage of a struct- or array-typed
// assignment base.
//
// Unlike `&x` this never promotes a local to a heap cell, so the variable's later reads
// and register layout are untouched.
//
// Takes base (ast.Expr) which is the struct- or array-typed base expression.
//
// Returns the pointer location, or an error when the base fails to compile.
func (c *Compiler) addressOfAssignBase(ctx context.Context, base ast.Expr) (program.VarLocation, error) {
	switch expr := ast.Unparen(base).(type) {
	case *ast.Ident:
		if location, ok := c.addressOfAssignIdent(ctx, expr); ok {
			return location, nil
		}
	case *ast.StarExpr:
		pointer, err := c.compileExpression(ctx, expr.X)
		if err != nil {
			return program.VarLocation{}, err
		}
		return c.pinOperand(ctx, pointer, c.staticTypeOf(expr.X)), nil
	case *ast.SelectorExpr:
		if selection := c.Info.Selections[expr]; selection != nil && selection.Kind() == types.FieldVal {
			return c.addressOfAssignField(ctx, expr, selection)
		}
	case *ast.IndexExpr:
		return c.compileAddressOfIndex(ctx, expr)
	default:
	}
	location, err := c.compileExpression(ctx, base)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &location)
	return c.emitStableAddress(ctx, location), nil
}

// addressOfAssignIdent yields the stable address of a named variable: the cell pointer of
// a captured or indirect variable, the raw slot of an indirect package variable, and an
// in-register address for any other local or package variable.
//
// Takes expr (*ast.Ident) which names the variable.
//
// Returns the address location and true, or false when the name is not a variable.
func (c *Compiler) addressOfAssignIdent(ctx context.Context, expr *ast.Ident) (program.VarLocation, bool) {
	if location, ok := c.compileAddressOfUpvalue(expr); ok {
		return location, true
	}
	if location, found := c.Scopes.LookupVar(expr.Name); found {
		if location.IsIndirect {
			return program.VarLocation{Register: location.Register, Kind: isa.RegisterGeneral}, true
		}
		if location.IsSpilled {
			location = c.emitReloadIfSpilled(ctx, location)
		}
		return c.emitStableAddress(ctx, location), true
	}
	if gv, ok := c.globalVariables[expr.Name]; ok {
		if gv.IsIndirect {
			return c.emitGetGlobalSlot(gv), true
		}
		return c.emitStableAddress(ctx, c.emitGetGlobal(ctx, gv)), true
	}
	return program.VarLocation{}, false
}

// addressOfAssignField yields the address of a struct field: the parent is pinned when it
// is reference-like (a pointer or interface) and addressed otherwise, then the field path
// is walked as pointers.
//
// Takes expr (*ast.SelectorExpr) which is the field selection.
// Takes selection (*types.Selection) which is its resolved field path.
//
// Returns the field's address location.
func (c *Compiler) addressOfAssignField(ctx context.Context, expr *ast.SelectorExpr, selection *types.Selection) (program.VarLocation, error) {
	var parent program.VarLocation
	var err error
	if isReferenceLikeType(c.staticTypeOf(expr.X)) {
		parent, err = c.compileExpression(ctx, expr.X)
		if err == nil {
			parent = c.pinOperand(ctx, parent, c.staticTypeOf(expr.X))
		}
	} else {
		parent, err = c.addressOfAssignBase(ctx, expr.X)
	}
	if err != nil {
		return program.VarLocation{}, err
	}
	return c.walkFieldPointerPath(parent, expr.X, selection.Index()), nil
}

// emitStableAddress emits a pointer to the value held in a general-bank register, telling
// the runtime the source is stable so it is not copied to the heap.
//
// Takes location (program.VarLocation) which holds the value.
//
// Returns program.VarLocation which is the pointer.
func (c *Compiler) emitStableAddress(ctx context.Context, location program.VarLocation) program.VarLocation {
	c.boxToGeneral(ctx, &location)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, dest, location.Register, engine.AddrSourceStable)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
}

// emitPreparedStore stores value into a prepared target.
//
// Takes target (preparedTarget) which is the prepared target.
// Takes value (program.VarLocation) which is the value to store.
//
// Returns the location the store produced (for statement results), or an error.
func (c *Compiler) emitPreparedStore(ctx context.Context, target preparedTarget, value program.VarLocation) (program.VarLocation, error) {
	var err error
	switch target.kind {
	case preparedIndex:
		err = c.compileIndexAssignFrom(ctx, target.expr.(*ast.IndexExpr), target.base, target.index, value)
	case preparedSelector:
		err = c.compileSelectorAssignFrom(ctx, target.expr.(*ast.SelectorExpr), target.base, value)
	case preparedStar:
		err = c.compileStarAssignFrom(ctx, target.base, value)
	case preparedNone:
		return c.emitAssignTarget(ctx, target.expr, value)
	}
	if err != nil {
		return program.VarLocation{}, err
	}
	return value, nil
}

// isReferenceLikeType reports whether copying a value of t copies only a reference or
// header, so the copy still reaches the same backing store.
//
// Takes t (types.Type) which is the static type, possibly nil.
//
// Returns true for slices, maps, pointers, channels, funcs and interfaces.
func isReferenceLikeType(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Slice, *types.Map, *types.Pointer, *types.Chan, *types.Signature, *types.Interface:
		return true
	default:
		return false
	}
}

// tupleNeedsPreparedTargets reports whether a tuple assignment has a target whose
// operands must be evaluated before any store: Go evaluates every index and pointer on
// the left before assigning, so `i, xs[i] = 1, 2` indexes with the old i.
//
// Takes targets ([]ast.Expr) which are the left-hand sides.
//
// Returns true when at least one target is an index, selector or star expression.
func tupleNeedsPreparedTargets(targets []ast.Expr) bool {
	for _, target := range targets {
		switch target.(type) {
		case *ast.IndexExpr, *ast.SelectorExpr, *ast.StarExpr:
			return true
		default:
		}
	}
	return false
}
