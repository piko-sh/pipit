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

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// compileMultiAssign compiles a multi-value assignment (non-:= form). Handles both tuple
// swap (a, b = b, a) and multi-return (a, b = f()).
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement with multiple
// left-hand side targets.
//
// Returns the result location of the compiled assignment and any compilation error
// encountered.
func (c *Compiler) compileMultiAssign(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	if len(statement.Rhs) == 1 && c.commaOkTargetsNeedTemps(statement) {
		return c.compileCommaOkAssignTargets(ctx, statement)
	}
	if len(statement.Rhs) == 1 {
		if location, ok, err := c.tryMultiAssignSingleRHS(ctx, statement); ok || err != nil {
			return location, err
		}
	}
	return c.compileTupleAssign(ctx, statement)
}

// tryMultiAssignSingleRHS handles multi-value assignment when there is exactly one RHS
// expression: multi-return calls, map comma-ok, type assertion comma-ok, and channel
// receive comma-ok.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement with a single
// right-hand side expression.
//
// Returns the result location, a bool indicating whether the assignment was handled, and
// any compilation error encountered.
func (c *Compiler) tryMultiAssignSingleRHS(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	rightHandSide := statement.Rhs[0]

	if callExpression, ok := rightHandSide.(*ast.CallExpr); ok {
		location, err := c.compileMultiReturnAssign(ctx, statement.Lhs, callExpression, false)
		return location, true, err
	}

	if indexExpression, ok := rightHandSide.(*ast.IndexExpr); ok && len(statement.Lhs) == commaOkResultCount {
		if tv, has := c.Info.Types[indexExpression.X]; has {
			if _, isMap := tv.Type.Underlying().(*types.Map); isMap {
				location, err := c.compileMapCommaOk(ctx, statement.Lhs, indexExpression, false)
				return location, true, err
			}
		}
	}

	if assertExpr, ok := rightHandSide.(*ast.TypeAssertExpr); ok && len(statement.Lhs) == commaOkResultCount {
		location, err := c.compileTypeAssertCommaOk(ctx, statement.Lhs, assertExpr, false)
		return location, true, err
	}

	if unaryExpression, ok := rightHandSide.(*ast.UnaryExpr); ok && unaryExpression.Op == token.ARROW && len(statement.Lhs) == commaOkResultCount {
		location, err := c.compileChannelReceiveCommaOk(ctx, statement.Lhs, unaryExpression, false)
		return location, true, err
	}

	return program.VarLocation{}, false, nil
}

// tryCompileSwapStructFields detects `t.x, t.y = t.y, t.x` and emits an
// OpSwapStructFieldsGeneralT0 super-op instead of the generic expansion.
//
// Takes statement (*ast.AssignStmt) which is the candidate swap.
//
// Returns (handled, location, err). handled is true when the super-op was emitted and the
// caller must skip the generic lowering path.
func (c *Compiler) tryCompileSwapStructFields(
	ctx context.Context,
	statement *ast.AssignStmt,
) (bool, program.VarLocation, error) {
	lhs0, lhs1, recognised := recogniseSwapSelectorShape(statement)
	if !recognised {
		return false, program.VarLocation{}, nil
	}
	layoutAIdx, layoutBIdx, layoutsOK := c.resolveSwapLayouts(ctx, lhs0, lhs1)
	if !layoutsOK {
		return false, program.VarLocation{}, nil
	}
	receiverLocation, err := c.compileExpression(ctx, lhs0.X)
	if err != nil {
		return false, program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)
	program.Emit(c.Function,
		isa.OpSwapStructFieldsGeneralT0,
		receiverLocation.Register,
		safeconv.MustIntToUint8(int(layoutAIdx)),
		safeconv.MustIntToUint8(int(layoutBIdx)),
	)
	return true, program.VarLocation{}, nil
}

// resolveSwapLayouts resolves the two struct-field layouts for a swap candidate.
//
// Takes lhs0 (*ast.SelectorExpr) which is the first left-hand selector to resolve.
// Takes lhs1 (*ast.SelectorExpr) which is the second left-hand selector to resolve.
//
// Returns firstLayoutIndex (uint16) which is the layout index for lhs0.
// Returns secondLayoutIndex (uint16) which is the layout index for lhs1.
// Returns resolved (bool) which is true when both layouts are addressable as general-bank
// tier-0 layouts with matching Kind and FieldTypeIndex.
func (c *Compiler) resolveSwapLayouts(ctx context.Context, lhs0 *ast.SelectorExpr, lhs1 *ast.SelectorExpr) (firstLayoutIndex uint16, secondLayoutIndex uint16, resolved bool) {
	selectionA := c.Info.Selections[lhs0]
	selectionB := c.Info.Selections[lhs1]
	if selectionA == nil || selectionB == nil {
		return 0, 0, false
	}
	layoutAIdx, okA := c.tryResolveStructFieldLayout(ctx, selectionA)
	layoutBIdx, okB := c.tryResolveStructFieldLayout(ctx, selectionB)
	if !okA || !okB {
		return 0, 0, false
	}
	if !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutAIdx) || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutBIdx) {
		return 0, 0, false
	}
	layoutA := c.Function.StructLayoutTable[layoutAIdx]
	layoutB := c.Function.StructLayoutTable[layoutBIdx]
	if isa.RegisterKind(layoutA.RegisterKind) != isa.RegisterGeneral ||
		isa.RegisterKind(layoutB.RegisterKind) != isa.RegisterGeneral {
		return 0, 0, false
	}
	if layoutA.Kind != layoutB.Kind || layoutA.FieldTypeIndex != layoutB.FieldTypeIndex {
		return 0, 0, false
	}
	return layoutAIdx, layoutBIdx, true
}

// compileTupleAssign compiles a parallel multi-target assignment such as `a, b = b, a` by
// evaluating every right-hand side into a temporary before writing any target. Detects
// the cross-paired struct-field swap fast path before falling back to the generic
// temp-then-store walk.
//
// Takes statement (*ast.AssignStmt) which has matched LHS and RHS counts.
//
// Returns the last assigned target's location and any compilation error.
func (c *Compiler) compileTupleAssign(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	if len(statement.Rhs) != len(statement.Lhs) {
		return program.VarLocation{}, fmt.Errorf("assignment count mismatch: %d = %d", len(statement.Lhs), len(statement.Rhs))
	}
	if handled, location, err := c.tryCompileSwapStructFields(ctx, statement); err != nil {
		return program.VarLocation{}, err
	} else if handled {
		return location, nil
	}
	prepared, err := c.prepareTupleTargets(ctx, statement.Lhs)
	if err != nil {
		return program.VarLocation{}, err
	}
	temps, err := c.compileTupleValues(ctx, statement.Rhs)
	if err != nil {
		return program.VarLocation{}, err
	}
	lastLocation, err := c.storeTupleValues(ctx, statement.Lhs, prepared, temps)
	for _, temp := range temps {
		c.Scopes.Alloc.FreeTemp(temp.Kind, temp.Register)
	}
	return lastLocation, err
}

// prepareTupleTargets evaluates the operands of every target of a tuple assignment before
// any value when at least one target is not a plain identifier, so the evaluation order
// matches Go's. Plain identifier lists need nothing and yield nil.
//
// Takes targets ([]ast.Expr) which are the left-hand sides.
//
// Returns []preparedTarget which is nil or one entry per target.
// Returns error when an operand fails to compile.
func (c *Compiler) prepareTupleTargets(ctx context.Context, targets []ast.Expr) ([]preparedTarget, error) {
	if !tupleNeedsPreparedTargets(targets) {
		return nil, nil
	}
	prepared := make([]preparedTarget, len(targets))
	for i, target := range targets {
		entry, err := c.prepareAssignTarget(ctx, target)
		if err != nil {
			return nil, err
		}
		prepared[i] = entry
	}
	return prepared, nil
}

// compileTupleValues evaluates every right-hand side of a tuple assignment into its own
// temporary, so a later store cannot disturb an earlier value.
//
// Takes values ([]ast.Expr) which are the right-hand sides.
//
// Returns []program.VarLocation which holds one temporary per value.
// Returns error when a right-hand expression fails to compile.
func (c *Compiler) compileTupleValues(ctx context.Context, values []ast.Expr) ([]program.VarLocation, error) {
	temps := make([]program.VarLocation, len(values))
	for i, value := range values {
		location, err := c.compileExpression(ctx, value)
		if err != nil {
			return nil, err
		}
		c.snapshotIndirectLocalRead(value, location)
		tempRegister := c.Scopes.Alloc.AllocTemp(location.Kind)
		tempLocation := program.VarLocation{Register: tempRegister, Kind: location.Kind}
		c.emitMoveTyped(ctx, tempLocation, location, c.staticTypeOf(value))
		temps[i] = tempLocation
	}
	return temps, nil
}

// storeTupleValues stores each temporary into its target, through the prepared operands
// when the targets were prepared.
//
// Takes targets ([]ast.Expr) which are the left-hand sides.
// Takes prepared ([]preparedTarget) which is nil or one entry per target.
// Takes temps ([]program.VarLocation) which hold the values.
//
// Returns program.VarLocation which is the last stored location.
// Returns error when a store fails to compile.
func (c *Compiler) storeTupleValues(ctx context.Context, targets []ast.Expr, prepared []preparedTarget, temps []program.VarLocation) (program.VarLocation, error) {
	var lastLocation program.VarLocation
	for i, target := range targets {
		var location program.VarLocation
		var err error
		if prepared != nil {
			location, err = c.emitPreparedStore(ctx, prepared[i], temps[i])
		} else {
			location, err = c.emitAssignTarget(ctx, target, temps[i])
		}
		if err != nil {
			return program.VarLocation{}, err
		}
		if location.Kind != 0 || location.Register != 0 {
			lastLocation = location
		}
	}
	return lastLocation, nil
}

// compileShortVarDecl compiles a short variable declaration (:=).
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement with
// token.DEFINE as the operator.
//
// Returns the result location of the compiled declaration and any compilation error
// encountered.
func (c *Compiler) compileShortVarDecl(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	if location, handled, err := c.tryShortVarDeclLowerings(ctx, statement); handled {
		return location, err
	}
	return c.compileSequentialShortVar(ctx, statement)
}

// tryMultiReturnShortVar detects a multi-return call in := context (e.g. a, b := f()) and
// compiles it.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement to check for a
// multi-return call.
//
// Returns the result location, a bool indicating whether the assignment was handled as a
// multi-return call, and any compilation error encountered.
func (c *Compiler) tryMultiReturnShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	callExpression, ok := statement.Rhs[0].(*ast.CallExpr)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	location, err := c.compileMultiReturnAssign(ctx, statement.Lhs, callExpression, true)
	return location, true, err
}

// tryMapCommaOkShortVar detects a map index comma-ok in := context (e.g. v, ok := m[k])
// and compiles it.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement to check for a
// map comma-ok pattern.
//
// Returns the result location, a bool indicating whether the assignment was handled as a
// map comma-ok, and any compilation error encountered.
func (c *Compiler) tryMapCommaOkShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	if len(statement.Lhs) != 2 {
		return program.VarLocation{}, false, nil
	}
	indexExpression, ok := statement.Rhs[0].(*ast.IndexExpr)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	tv, has := c.Info.Types[indexExpression.X]
	if !has {
		return program.VarLocation{}, false, nil
	}
	if _, isMap := tv.Type.Underlying().(*types.Map); !isMap {
		return program.VarLocation{}, false, nil
	}
	location, err := c.compileMapCommaOk(ctx, statement.Lhs, indexExpression, true)
	return location, true, err
}

// tryTypeAssertCommaOkShortVar detects a type assertion comma-ok in := context (e.g. v,
// ok := x.(T)) and compiles it.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement to check for a
// type assertion comma-ok pattern.
//
// Returns the result location, a bool indicating whether the assignment was handled as a
// type assertion comma-ok, and any compilation error encountered.
func (c *Compiler) tryTypeAssertCommaOkShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	if len(statement.Lhs) != 2 {
		return program.VarLocation{}, false, nil
	}
	assertExpr, ok := statement.Rhs[0].(*ast.TypeAssertExpr)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	location, err := c.compileTypeAssertCommaOk(ctx, statement.Lhs, assertExpr, true)
	return location, true, err
}

// tryChannelReceiveCommaOkShortVar detects a channel receive comma-ok in := context (e.g.
// v, ok := <-ch) and compiles it.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement to check for a
// channel receive comma-ok pattern.
//
// Returns the result location, a bool indicating whether the assignment was handled as a
// channel receive comma-ok, and any compilation error encountered.
func (c *Compiler) tryChannelReceiveCommaOkShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	if len(statement.Lhs) != 2 {
		return program.VarLocation{}, false, nil
	}
	unaryExpression, ok := statement.Rhs[0].(*ast.UnaryExpr)
	if !ok || unaryExpression.Op != token.ARROW {
		return program.VarLocation{}, false, nil
	}
	location, err := c.compileChannelReceiveCommaOk(ctx, statement.Lhs, unaryExpression, true)
	return location, true, err
}

// compileSequentialShortVar compiles the sequential := case where each LHS identifier is
// declared (or redeclared) and assigned from the corresponding RHS expression.
//
// Takes statement (*ast.AssignStmt) which is the AST assignment statement with matched
// LHS and RHS pairs.
//
// Returns the result location of the last declared variable and any compilation error
// encountered.
func (c *Compiler) compileSequentialShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	if c.shortVarNeedsTwoPhase(statement) {
		return c.compileSequentialShortVarTwoPhase(ctx, statement)
	}
	var lastLocation program.VarLocation
	for i, leftHandSide := range statement.Lhs {
		identifier, ok := leftHandSide.(*ast.Ident)
		if !ok || identifier.Name == typemap.BlankIdentName {
			continue
		}
		location, err := c.compileShortVarIdent(ctx, identifier, statement.Rhs, i)
		if err != nil {
			return program.VarLocation{}, err
		}
		if location.Kind != 0 || location.Register != 0 {
			lastLocation = location
		}
	}
	return lastLocation, nil
}

// shortVarNeedsTwoPhase reports whether a short declaration is self-referential.
//
// Reports whether any right-hand expression references a name declared or redeclared on
// the left, which makes the sequential declare-then-assign lowering observably wrong: Go
// evaluates every right-hand side before any assignment, so `x, y := 2, x` must bind y to
// the pre-existing x, not the freshly assigned 2. Only these self-referential
// multi-assignments take the two-phase path; the common independent case keeps the
// cheaper sequential lowering unchanged.
//
// Takes statement (*ast.AssignStmt) which is the short variable declaration to inspect.
//
// Returns bool which is true when the two-phase lowering is required.
func (*Compiler) shortVarNeedsTwoPhase(statement *ast.AssignStmt) bool {
	if len(statement.Lhs) < 2 || len(statement.Rhs) != len(statement.Lhs) {
		return false
	}
	names := shortVarLeftHandNames(statement)
	if len(names) == 0 {
		return false
	}
	for _, rightHandSide := range statement.Rhs {
		if exprReferencesAnyName(rightHandSide, names) {
			return true
		}
	}
	return false
}

// compileSequentialShortVarTwoPhase lowers a self-referential `a, b := ...` in two
// phases.
//
// Every right-hand side is evaluated into a persistent temporary first, then each
// variable is declared and assigned from its temporary. This matches Go's
// evaluate-all-then-assign order. Blank left-hand sides still have their right-hand side
// evaluated for side effects.
//
// Takes statement (*ast.AssignStmt) which is the short variable declaration to lower.
//
// Returns the last declared location (for a trailing-expression result) and any error.
func (c *Compiler) compileSequentialShortVarTwoPhase(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	temps := make([]program.VarLocation, len(statement.Lhs))
	hasValue := make([]bool, len(statement.Lhs))
	for i, rightHandSide := range statement.Rhs {
		valueLocation, err := c.compileExpression(ctx, rightHandSide)
		if err != nil {
			return program.VarLocation{}, err
		}
		valueLocation = c.coerceEvalBoolResult(ctx, c.Info, rightHandSide, valueLocation)
		c.snapshotIndirectLocalRead(rightHandSide, valueLocation)
		identifier, ok := statement.Lhs[i].(*ast.Ident)
		if !ok || identifier.Name == typemap.BlankIdentName {
			continue
		}
		persistent := c.Scopes.Alloc.AllocTemp(valueLocation.Kind)
		temps[i] = program.VarLocation{Register: persistent, Kind: valueLocation.Kind}
		c.emitMoveTyped(ctx, temps[i], valueLocation, c.staticTypeOf(rightHandSide))
		hasValue[i] = true
	}

	return c.declareTwoPhaseShortVars(ctx, statement, temps, hasValue)
}

// declareTwoPhaseShortVars is the declare phase of compileSequentialShortVarTwoPhase:
// each non-blank left-hand identifier whose right-hand side produced a value
// (hasValue[i]) is declared and assigned from its persistent temporary.
//
// Takes statement (*ast.AssignStmt) which is the short variable declaration.
// Takes temps ([]VarLocation) which holds each right-hand value's persistent register.
// Takes hasValue ([]bool) which marks the indices that produced an assignable value.
//
// Returns the last declared location (for a trailing-expression result) and any error.
func (c *Compiler) declareTwoPhaseShortVars(ctx context.Context, statement *ast.AssignStmt, temps []program.VarLocation, hasValue []bool) (program.VarLocation, error) {
	var lastLocation program.VarLocation
	for i, leftHandSide := range statement.Lhs {
		if !hasValue[i] {
			continue
		}
		identifier, ok := leftHandSide.(*ast.Ident)
		if !ok {
			continue
		}
		location, err := c.declareShortVarFromValue(ctx, identifier, temps[i], statement.Rhs[i])
		if err != nil {
			return program.VarLocation{}, err
		}
		if location.Kind != 0 || location.Register != 0 {
			lastLocation = location
		}
	}
	return lastLocation, nil
}

// declareShortVarFromValue declares or redeclares identifier and assigns it from an
// already-evaluated value, sharing the new-declaration and redeclaration handling of
// compileShortVarIdent but skipping right-hand-side evaluation (the two-phase caller has
// already lowered every right-hand side into a persistent temporary).
//
// Takes identifier (*ast.Ident) which is the variable being declared or redeclared.
// Takes value (VarLocation) which is the already-evaluated value to assign.
// Takes rhsExpr (ast.Expr) which is the source right-hand expression for its static type.
//
// Returns the declared location and any error.
func (c *Compiler) declareShortVarFromValue(ctx context.Context, identifier *ast.Ident, value program.VarLocation, rhsExpr ast.Expr) (program.VarLocation, error) {
	typeObject := c.Info.Defs[identifier]
	if typeObject == nil {
		existing, found := c.Scopes.LookupVar(identifier.Name)
		if !found {
			return program.VarLocation{}, nil
		}
		c.emitMoveTyped(ctx, existing, value, c.staticTypeOf(rhsExpr))
		return existing, nil
	}

	kind := c.kindFor(typeObject.Type())
	location := c.Scopes.DeclareVar(identifier.Name, kind)
	if c.isInsideLoop(ctx) && !location.IsSpilled && c.closureCapturedNames[identifier.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, location.Register, uint8(location.Kind))
	}
	c.emitValueCopyForLocalAssignment(rhsExpr, value, identifier.Name)
	c.emitMoveTyped(ctx, location, value, c.staticTypeOf(rhsExpr))
	c.tryHeapPromoteCapturedLocal(ctx, identifier.Name, identifier)
	if promoted, ok := c.Scopes.LookupVar(identifier.Name); ok {
		location = promoted
	}
	return location, nil
}

// compileShortVarIdent compiles a single identifier in a short variable declaration,
// either declaring a new variable or redeclaring an existing one.
//
// Takes identifier (*ast.Ident) which is the AST identifier being declared or redeclared.
// Takes rightHandSideExprs ([]ast.Expr) which is the slice of right-hand side AST
// expressions.
// Takes i (int) which is the index of this identifier within the declaration.
//
// Returns the location of the declared or redeclared variable and any compilation error
// encountered.
func (c *Compiler) compileShortVarIdent(ctx context.Context, identifier *ast.Ident, rightHandSideExprs []ast.Expr, i int) (program.VarLocation, error) {
	typeObject := c.Info.Defs[identifier]
	if typeObject == nil {
		return c.compileShortVarRedecl(ctx, identifier, rightHandSideExprs, i)
	}
	kind := c.typedSliceKindForLocal(identifier.Name, typeObject.Type(), c.kindFor(typeObject.Type()))

	if isa.IsTypedSliceKind(kind) {
		return c.compileShortVarIdentTypedSlice(ctx, identifier, rightHandSideExprs, i, kind)
	}

	var valueLocation program.VarLocation
	var hasValue bool
	var rhsExpr ast.Expr
	if i < len(rightHandSideExprs) {
		watermark := c.Scopes.Alloc.Snapshot()
		var err error
		valueLocation, err = c.compileLocalInitialiser(ctx, identifier.Name, rightHandSideExprs[i])
		if err != nil {
			return program.VarLocation{}, err
		}
		valueLocation = c.coerceEvalBoolResult(ctx, c.Info, rightHandSideExprs[i], valueLocation)
		c.snapshotIndirectLocalRead(rightHandSideExprs[i], valueLocation)
		c.Scopes.RestoreWatermark(watermark)
		hasValue = true
		rhsExpr = rightHandSideExprs[i]
	}

	location := c.Scopes.DeclareVar(identifier.Name, kind)
	if c.isInsideLoop(ctx) && !location.IsSpilled && c.closureCapturedNames[identifier.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, location.Register, uint8(location.Kind))
	}

	if hasValue {
		c.emitValueCopyForLocalAssignment(rhsExpr, valueLocation, identifier.Name)
		c.emitMoveTyped(ctx, location, valueLocation, c.staticTypeOf(rhsExpr))
	}
	c.tryHeapPromoteCapturedLocal(ctx, identifier.Name, identifier)
	if promoted, ok := c.Scopes.LookupVar(identifier.Name); ok {
		location = promoted
	}

	return location, nil
}

// typedSliceKindForLocal returns the typed-slice bank the classifier assigned to a local,
// or kind unchanged when the local is not a general-bank slice, was not classified, or
// the classification does not match its declared type.
//
// Takes name (string) which is the local's name.
// Takes declared (types.Type) which is the local's declared type.
// Takes kind (isa.RegisterKind) which is the bank kindFor() chose for that type.
//
// Returns the register kind the local is declared with.
func (c *Compiler) typedSliceKindForLocal(name string, declared types.Type, kind isa.RegisterKind) isa.RegisterKind {
	if kind != isa.RegisterGeneral {
		return kind
	}
	classified, ok := c.typedSliceLocals[name]
	if !ok || !isa.IsTypedSliceKind(classified) || classified != typemap.KindForTypedSlice(declared) {
		return kind
	}
	return classified
}

// compileShortVarIdentTypedSlice compiles a typed-slice short-var declaration.
//
// Takes identifier (*ast.Ident) which is the AST identifier being declared.
// Takes rightHandSideExprs ([]ast.Expr) of which only index i is consulted.
// Takes i (int) which is the position within the declaration.
// Takes kind (isa.RegisterKind) which selects the typed-slice bank.
//
// Returns the location of the declared variable and any compilation error.
func (c *Compiler) compileShortVarIdentTypedSlice(ctx context.Context, identifier *ast.Ident, rightHandSideExprs []ast.Expr, i int, kind isa.RegisterKind) (program.VarLocation, error) {
	location := c.Scopes.DeclareVar(identifier.Name, kind)
	if c.isInsideLoop(ctx) && !location.IsSpilled && c.closureCapturedNames[identifier.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, location.Register, uint8(location.Kind))
	}

	if i < len(rightHandSideExprs) {
		if err := c.emitTypedSliceInitialiser(ctx, identifier, rightHandSideExprs[i], location, kind); err != nil {
			return program.VarLocation{}, err
		}
	}
	c.tryHeapPromoteCapturedLocal(ctx, identifier.Name, identifier)
	if promoted, ok := c.Scopes.LookupVar(identifier.Name); ok {
		location = promoted
	}
	return location, nil
}

// emitTypedSliceInitialiser writes a typed-slice local's initialiser into its register: a
// make call through the typed make sub-op, anything else (a map lookup) through a
// general-bank compile followed by the adopt move.
//
// Takes identifier (*ast.Ident) which is the local being declared.
// Takes value (ast.Expr) which is the initialiser expression.
// Takes location (VarLocation) which is the declared typed-slice register.
// Takes kind (isa.RegisterKind) which is the local's typed-slice bank.
//
// Returns the first compilation error encountered.
func (c *Compiler) emitTypedSliceInitialiser(ctx context.Context, identifier *ast.Ident, value ast.Expr, location program.VarLocation, kind isa.RegisterKind) error {
	if callExpression, ok := c.makeSliceCall(value); ok {
		return c.emitTypedMakeSliceInto(ctx, callExpression, location.Register, kind, c.heapTypedSliceLocals[identifier.Name])
	}
	watermark := c.Scopes.Alloc.Snapshot()
	valueLocation, err := c.compileExpression(ctx, value)
	if err != nil {
		return err
	}
	c.emitMoveTyped(ctx, location, valueLocation, c.staticTypeOf(value))
	c.Scopes.RestoreWatermark(watermark)
	return nil
}

// emitTypedMakeSliceInto emits the umbrella subOpMakeSlice<Kind> instruction matching
// kind, compiling the length and capacity arguments into the int bank and threading them
// through the standard isa.OpDrillTier1 + isa.OpExt extension pair.
//
// Takes expression (*ast.CallExpr) which must be a make([]T, ...) call validated by the
// typed-slice classifier.
// Takes destinationRegister (uint8) which is the target typed-slice register receiving
// the new slice header.
// Takes kind (isa.RegisterKind) which selects the matching subOpMakeSlice variant.
// Takes heapPlaced (bool) which selects isa.SubOpMakeSliceHeap, allocating the backing on
// the Go heap because the local escapes the frame (see classifyHeapTypedSliceLocals).
//
// Returns nil on success, or an error when compiling the length or capacity expression
// fails or when kind is not a typed-slice bank.
func (c *Compiler) emitTypedMakeSliceInto(ctx context.Context, expression *ast.CallExpr, destinationRegister uint8, kind isa.RegisterKind, heapPlaced bool) error {
	subOp, ok := isaselect.TypedMakeSliceSubOp(kind)
	if !ok {
		return fmt.Errorf("emitTypedMakeSliceInto: unsupported typed-slice kind %d", kind)
	}
	var lengthLocation program.VarLocation
	if len(expression.Args) >= 2 {
		var err error
		lengthLocation, err = c.compileExpression(ctx, expression.Args[1])
		if err != nil {
			return err
		}
	}
	capacityLocation := lengthLocation
	if len(expression.Args) >= makeSliceMinCapArgs {
		var err error
		capacityLocation, err = c.compileExpression(ctx, expression.Args[2])
		if err != nil {
			return err
		}
	}
	if heapPlaced {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMakeSliceHeap), destinationRegister, lengthLocation.Register)
		program.Emit(c.Function, isa.OpExt, capacityLocation.Register, uint8(kind), 0)
		return nil
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), destinationRegister, lengthLocation.Register)
	program.Emit(c.Function, isa.OpExt, capacityLocation.Register, 0, 0)
	return nil
}

// emitValueCopyForLocalAssignment emits an in-place snapshot for struct or array values
// so the destination does not alias the source's heap memory. Reference kinds pass
// through unchanged, and the snapshot is skipped when localName is provably read-only.
//
// Takes rhsExpr (ast.Expr) whose static type drives the kind decision.
// Takes valueLocation (VarLocation) which is the source register.
// Takes localName (string) which is the destination's bound name, or empty/"_" when
// unavailable.
func (c *Compiler) emitValueCopyForLocalAssignment(rhsExpr ast.Expr, valueLocation program.VarLocation, localName string) {
	if valueLocation.Kind != isa.RegisterGeneral {
		return
	}
	if c.Info == nil || rhsExpr == nil {
		return
	}
	tv, ok := c.Info.Types[rhsExpr]
	if !ok || tv.Type == nil {
		return
	}
	if c.rhsReadsIndirectLocal(rhsExpr) {
		return
	}
	if !typeIsStructOrArray(tv.Type) {
		if c.rhsAliasesIndexedBackingStore(rhsExpr) && !typeIsInterface(tv.Type) {
			program.Emit(c.Function, isa.OpDeref, valueLocation.Register, valueLocation.Register, engine.DerefSnapshot)
		}
		return
	}
	if localName != "" && localName != "_" &&
		!c.writtenLocalNames[localName] &&
		!c.closureCapturedNames[localName] {
		return
	}
	program.Emit(c.Function, isa.OpDeref, valueLocation.Register, valueLocation.Register, engine.DerefSnapshot)
}

// rhsAliasesIndexedBackingStore reports whether rhsExpr is a slice or array index
// expression whose evaluated value aliases the collection's backing storage.
//
// handleIndex hands back collection.Index(i), an addressable reflect.Value sharing the
// backing array, so a reference-kind element bound directly into a local would follow
// later in-place mutations of that backing rather than holding an independent copy. Map
// and string indexes already yield detached copies and so are excluded.
//
// Takes rhsExpr (ast.Expr) which is the bind's right-hand-side expression.
//
// Returns true when the index reads from a slice or array backing store.
func (c *Compiler) rhsAliasesIndexedBackingStore(rhsExpr ast.Expr) bool {
	indexExpr, ok := rhsExpr.(*ast.IndexExpr)
	if !ok {
		return false
	}
	if c.Info == nil {
		return false
	}
	collectionTypeAndValue, ok := c.Info.Types[indexExpr.X]
	if !ok || collectionTypeAndValue.Type == nil {
		return false
	}
	switch collectionTypeAndValue.Type.Underlying().(type) {
	case *types.Slice, *types.Array:
		return true
	default:
		return false
	}
}

// rhsReadsIndirectLocal reports whether rhsExpr is a bare identifier naming a
// heap-promoted general-bank local, whose read (EmitIndirectRead) hands back cell.Elem():
// an addressable reflect.Value whose header points at the heap cell instead of carrying
// the value.
//
// Takes rhsExpr (ast.Expr) which is the candidate expression.
//
// Returns true when the compiled value aliases a promoted local's cell.
func (c *Compiler) rhsReadsIndirectLocal(rhsExpr ast.Expr) bool {
	identifier, ok := ast.Unparen(rhsExpr).(*ast.Ident)
	if !ok {
		return false
	}
	source, found := c.Scopes.LookupVar(identifier.Name)
	return found && source.IsIndirect && source.OriginalKind == isa.RegisterGeneral
}

// snapshotIndirectLocalRead detaches value from the heap cell it was read out of when
// rhsExpr names a heap-promoted general-bank local. Called before declaring the LHS so `x
// := x` shadowing resolves to the outer local.
//
// Takes rhsExpr (ast.Expr) which is the expression value was compiled from.
// Takes value (VarLocation) which holds the compiled value.
//
// Returns true when a snapshot was emitted.
func (c *Compiler) snapshotIndirectLocalRead(rhsExpr ast.Expr, value program.VarLocation) bool {
	if value.Kind != isa.RegisterGeneral || value.IsSpilled || value.IsIndirect {
		return false
	}
	if !c.rhsReadsIndirectLocal(rhsExpr) {
		return false
	}
	program.Emit(c.Function, isa.OpDeref, value.Register, value.Register, engine.DerefSnapshot)
	return true
}

// compileShortVarRedecl handles a redeclared identifier in := by looking up the existing
// variable and assigning the RHS value.
//
// Takes identifier (*ast.Ident) which is the AST identifier being redeclared.
// Takes rightHandSideExprs ([]ast.Expr) which is the slice of right-hand side AST
// expressions.
// Takes i (int) which is the index of this identifier within the declaration.
//
// Returns the location of the redeclared variable and any compilation error encountered.
func (c *Compiler) compileShortVarRedecl(ctx context.Context, identifier *ast.Ident, rightHandSideExprs []ast.Expr, i int) (program.VarLocation, error) {
	location, found := c.Scopes.LookupVar(identifier.Name)
	if !found || i >= len(rightHandSideExprs) {
		return program.VarLocation{}, nil
	}

	watermark := c.Scopes.Alloc.Snapshot()
	valueLocation, err := c.compileExpression(ctx, rightHandSideExprs[i])
	if err != nil {
		return program.VarLocation{}, err
	}
	valueLocation = c.coerceEvalBoolResult(ctx, c.Info, rightHandSideExprs[i], valueLocation)
	c.emitMoveTyped(ctx, location, valueLocation, c.staticTypeOf(rightHandSideExprs[i]))
	c.Scopes.RestoreWatermark(watermark)

	return location, nil
}

// compileMultiValueShortVar dispatches a short variable declaration whose single
// right-hand expression yields more than one value.
//
// The four shapes are a multi-return call, the comma-ok forms of a map index and a type
// assertion, and a channel receive. Each reports whether it recognised the statement, so
// they are tried in turn and the first match wins.
//
// Takes statement (*ast.AssignStmt) which has at least two names and exactly one
// expression.
//
// Returns the compiled location, true when one of the shapes handled the statement, and
// any compilation error.
func (c *Compiler) compileMultiValueShortVar(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	shapes := []func(context.Context, *ast.AssignStmt) (program.VarLocation, bool, error){
		c.tryMultiReturnShortVar,
		c.tryMapCommaOkShortVar,
		c.tryTypeAssertCommaOkShortVar,
		c.tryChannelReceiveCommaOkShortVar,
	}
	for _, shape := range shapes {
		if location, ok, err := shape(ctx, statement); ok || err != nil {
			return location, true, err
		}
	}
	return program.VarLocation{}, false, nil
}

// shortVarLeftHandNames collects the non-blank identifiers declared on the left of a
// short variable declaration into a set, for self-reference detection.
//
// Takes statement (*ast.AssignStmt) whose Lhs identifiers are gathered.
//
// Returns a set of the declared names.
func shortVarLeftHandNames(statement *ast.AssignStmt) map[string]struct{} {
	names := make(map[string]struct{}, len(statement.Lhs))
	for _, leftHandSide := range statement.Lhs {
		if identifier, ok := leftHandSide.(*ast.Ident); ok && identifier.Name != typemap.BlankIdentName {
			names[identifier.Name] = struct{}{}
		}
	}
	return names
}

// exprReferencesAnyName reports whether expr mentions any identifier in names.
//
// Takes expr (ast.Expr) which is the right-hand expression to scan.
// Takes names (map[string]struct{}) which is the set of left-hand declared names.
//
// Returns true on the first referenced name found.
func exprReferencesAnyName(expr ast.Expr, names map[string]struct{}) bool {
	referenced := false
	ast.Inspect(expr, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if _, isName := names[identifier.Name]; isName {
			referenced = true
			return false
		}
		return true
	})
	return referenced
}

// typeIsInterface reports whether t's underlying type is an interface.
//
// Takes t (types.Type) which is the type to classify; nil returns false.
//
// Returns true when t reduces to an interface type.
func typeIsInterface(t types.Type) bool {
	if t == nil {
		return false
	}
	_, isInterface := t.Underlying().(*types.Interface)
	return isInterface
}

// recogniseSwapSelectorShape inspects statement for the cross-paired selector-swap shape:
// `recv.fieldA, recv.fieldB = recv.fieldB, recv.fieldA`.
//
// Takes statement (*ast.AssignStmt) which is the assignment to inspect for the swap
// shape.
//
// Returns lhs0Selector (*ast.SelectorExpr) which is the first left-hand selector when
// matched.
// Returns lhs1Selector (*ast.SelectorExpr) which is the second left-hand selector when
// matched.
// Returns matched (bool) which is true when the shape matches.
func recogniseSwapSelectorShape(statement *ast.AssignStmt) (lhs0Selector *ast.SelectorExpr, lhs1Selector *ast.SelectorExpr, matched bool) {
	if statement.Tok != token.ASSIGN || len(statement.Lhs) != 2 || len(statement.Rhs) != 2 {
		return nil, nil, false
	}
	lhs0, ok := statement.Lhs[0].(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	lhs1, ok := statement.Lhs[1].(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	rhs0, ok := statement.Rhs[0].(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	rhs1, ok := statement.Rhs[1].(*ast.SelectorExpr)
	if !ok {
		return nil, nil, false
	}
	recvIdent, ok := lhs0.X.(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	if !sameSelectorReceiver(recvIdent, lhs1.X) ||
		!sameSelectorReceiver(recvIdent, rhs0.X) ||
		!sameSelectorReceiver(recvIdent, rhs1.X) {
		return nil, nil, false
	}
	if lhs0.Sel.Name == lhs1.Sel.Name {
		return nil, nil, false
	}
	if lhs0.Sel.Name != rhs1.Sel.Name || lhs1.Sel.Name != rhs0.Sel.Name {
		return nil, nil, false
	}
	return lhs0, lhs1, true
}

// sameSelectorReceiver reports whether expression is an identifier with the same Name as
// recvIdent.
//
// Takes recvIdent (*ast.Ident) which is the receiver identifier to compare against.
// Takes expression (ast.Expr) which is the expression to test.
//
// Returns true when expression is an identifier sharing recvIdent's name.
func sameSelectorReceiver(recvIdent *ast.Ident, expression ast.Expr) bool {
	candidate, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	return candidate.Name == recvIdent.Name
}
