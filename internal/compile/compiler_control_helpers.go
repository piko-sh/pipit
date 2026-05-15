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
	"go/constant"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// incDecWrapMsg is the fmt.Errorf wrapper used when forwarding a sub-Compiler's
	// increment/decrement error so every dispatch branch returns a uniformly prefixed
	// diagnostic.
	incDecWrapMsg = "compiling increment/decrement: %w"
)

// multiReturnDeferredStore records a non-direct LHS target whose store instruction must
// be emitted after the call completes.
type multiReturnDeferredStore struct {
	// target specifies the LHS expression that needs a deferred store.
	target ast.Expr

	// sourceLocation specifies the source register location holding the value to store.
	sourceLocation program.VarLocation

	// prepared holds the target's operands when they were evaluated before the call (index,
	// selector and star targets), so the store happens in Go's order.
	prepared preparedTarget
}

// typedMapCommaOkRequest bundles the parameters of emitTypedMapCommaOk so the helper
// signature stays under the argument-count cap and the call site reads each field by
// name.
type typedMapCommaOkRequest struct {
	// valueIdentifier is the LHS identifier receiving the matched value.
	valueIdentifier *ast.Ident

	// okIdentifier is the LHS identifier receiving the presence bool.
	okIdentifier *ast.Ident

	// mapLocation is the compiled location of the source map register.
	mapLocation program.VarLocation

	// keyLocation is the compiled location of the lookup key.
	keyLocation program.VarLocation

	// op is the typed comma-ok opcode to Emit.
	op isa.Opcode

	// valueKind is the register kind for the value destination.
	valueKind isa.RegisterKind

	// isDefine reports whether the assignment uses := (declare) rather than = (assign).
	isDefine bool
}

// compileIncDec compiles an increment or decrement statement (x++ or x--).
//
// Takes statement (*ast.IncDecStmt) which is the increment or decrement statement AST
// node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileIncDec(ctx context.Context, statement *ast.IncDecStmt) (program.VarLocation, error) {
	if selectorExpression, ok := statement.X.(*ast.SelectorExpr); ok {
		return wrapIncDecResult(c.compileIncDecSelector(ctx, statement, selectorExpression))
	}

	if indexExpression, ok := statement.X.(*ast.IndexExpr); ok {
		return wrapIncDecResult(c.compileIncDecIndex(ctx, statement, indexExpression))
	}

	if starExpression, ok := statement.X.(*ast.StarExpr); ok {
		return wrapIncDecResult(c.compileIncDecStar(ctx, statement, starExpression))
	}

	identifier, ok := statement.X.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("unsupported inc/dec target: %T at %s", statement.X, c.positionString(statement.Pos()))
	}

	switch c.resolveIdentTarget(identifier) {
	case identTargetUpvalue:
		return wrapIncDecResult(c.compileIncDecUpvalue(ctx, statement, c.upvalueMap[identifier.Name]))
	case identTargetGlobal:
		return wrapIncDecResult(c.compileIncDecGlobal(ctx, statement, c.globalVariables[identifier.Name]))
	case identTargetLocal, identTargetUnknown:
	}

	return c.compileIncDecLocal(ctx, statement, identifier)
}

// compileIncDecStar compiles `*p++` or `*p--` by reading through the pointer, applying
// inc/dec, and writing the result back through the same pointer.
//
// Takes statement (*ast.IncDecStmt) which is the inc/dec statement.
// Takes target (*ast.StarExpr) which is the dereference expression.
//
// Returns the location of the post-write value and any compilation error.
func (c *Compiler) compileIncDecStar(ctx context.Context, statement *ast.IncDecStmt, target *ast.StarExpr) (program.VarLocation, error) {
	currentLocation, err := c.compileStarExpression(ctx, target)
	if err != nil {
		return program.VarLocation{}, err
	}
	if _, err := c.emitIncDec(ctx, statement.Tok, currentLocation); err != nil {
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(currentLocation, c.incDecStaticType(target))
	if err := c.compileStarAssign(ctx, target, currentLocation); err != nil {
		return program.VarLocation{}, err
	}
	return currentLocation, nil
}

// incDecStaticType returns the static Go type the type-checker recorded for an inc/dec
// target expression, or nil when none is available. Used by inc/dec call sites to detect
// narrow numeric types that need post-write truncation.
//
// Takes targetExpression (ast.Expr) which is the inc/dec target.
//
// Returns the recorded types.Type, or nil when no type is available.
func (c *Compiler) incDecStaticType(targetExpression ast.Expr) types.Type {
	if c.Info == nil {
		return nil
	}
	if tv, ok := c.Info.Types[targetExpression]; ok && tv.Type != nil {
		return tv.Type
	}
	if identifier, ok := targetExpression.(*ast.Ident); ok {
		if object := c.Info.Uses[identifier]; object != nil {
			return object.Type()
		}
		if object := c.Info.Defs[identifier]; object != nil {
			return object.Type()
		}
	}
	return nil
}

// compileIncDecLocal resolves the identifier against the current scope and emits the
// appropriate inc/dec sequence for a stack-local or spilled variable.
//
// Takes statement (*ast.IncDecStmt) which is the inc/dec statement.
// Takes identifier (*ast.Ident) which names the target variable.
//
// Returns the resulting location and any compilation error.
func (c *Compiler) compileIncDecLocal(
	ctx context.Context,
	statement *ast.IncDecStmt,
	identifier *ast.Ident,
) (program.VarLocation, error) {
	location, found := c.Scopes.LookupVar(identifier.Name)
	if !found {
		return program.VarLocation{}, fmt.Errorf("undefined variable: %s at %s", identifier.Name, c.positionString(identifier.Pos()))
	}

	staticType := c.incDecStaticType(identifier)

	if location.IsSpilled {
		scratch := c.emitReloadIfSpilled(ctx, location)
		if _, err := c.emitIncDec(ctx, statement.Tok, scratch); err != nil {
			return program.VarLocation{}, err
		}
		c.EmitNarrowIntegerTruncation(scratch, staticType)
		c.emitSpillStore(ctx, scratch.Register, location.Kind, location.SpillSlot)
		c.Scopes.Alloc.FreeTemp(location.Kind, scratch.Register)
		c.emitWriteSharedCellIfCaptured(ctx, location)
		return program.VarLocation{}, nil
	}

	if location.IsIndirect {
		scratch, err := c.EmitIndirectRead(ctx, location)
		if err != nil {
			return program.VarLocation{}, err
		}
		if _, err := c.emitIncDec(ctx, statement.Tok, scratch); err != nil {
			return program.VarLocation{}, err
		}
		c.EmitNarrowIntegerTruncation(scratch, staticType)
		c.emitIndirectWrite(ctx, location, scratch)
		c.Scopes.Alloc.FreeTemp(scratch.Kind, scratch.Register)
		c.emitWriteSharedCellIfCaptured(ctx, location)
		return program.VarLocation{}, nil
	}

	result, err := c.emitIncDec(ctx, statement.Tok, location)
	if err != nil {
		return result, err
	}
	c.EmitNarrowIntegerTruncation(location, staticType)
	c.emitWriteSharedCellIfCaptured(ctx, location)
	return result, nil
}

// compileIncDecIndex compiles m[k]++ and s[i]++ (and their decrement forms) by desugaring
// to m[k] += 1 / m[k] -= 1 and dispatching through the existing compound-assign-index
// path. This covers both map and slice/array targets because the compound path already
// handles both.
//
// Takes statement (*ast.IncDecStmt) which holds the ++/-- token.
// Takes indexExpression (*ast.IndexExpr) which is the target expression.
//
// Returns the compiled location (always zero value) and any error.
func (c *Compiler) compileIncDecIndex(ctx context.Context, statement *ast.IncDecStmt, indexExpression *ast.IndexExpr) (program.VarLocation, error) {
	one := &ast.BasicLit{
		ValuePos: statement.Pos(),
		Kind:     token.INT,
		Value:    "1",
	}
	operatorToken := token.ADD
	if statement.Tok == token.DEC {
		operatorToken = token.SUB
	}
	c.populateIncDecLiteralType(indexExpression, one)
	return c.compileCompoundAssignIndex(ctx, indexExpression, one, operatorToken)
}

// populateIncDecLiteralType records a TypeAndValue for the synthetic "1" literal used to
// desugar inc/dec into compound assignment. Without this the compound path reads an empty
// types.Type and mis-classifies the register kind on nested expressions.
//
// Takes indexExpression (*ast.IndexExpr) which identifies the element type.
// Takes literal (*ast.BasicLit) which is the synthetic "1" node.
func (c *Compiler) populateIncDecLiteralType(indexExpression *ast.IndexExpr, literal *ast.BasicLit) {
	collectionType, ok := c.Info.Types[indexExpression.X]
	if !ok || collectionType.Type == nil {
		return
	}

	underlying := collectionType.Type.Underlying()
	if pointer, isPointer := underlying.(*types.Pointer); isPointer {
		underlying = pointer.Elem().Underlying()
	}
	var elementType types.Type
	switch collection := underlying.(type) {
	case *types.Map:
		elementType = collection.Elem()
	case *types.Slice:
		elementType = collection.Elem()
	case *types.Array:
		elementType = collection.Elem()
	default:
		return
	}
	if c.Info.Types == nil {
		c.Info.Types = make(map[ast.Expr]types.TypeAndValue)
	}
	c.Info.Types[literal] = types.TypeAndValue{
		Type:  elementType,
		Value: constant.MakeInt64(1),
	}
}

// compileIncDecGlobal compiles an inc/dec on a global variable.
//
// Takes statement (*ast.IncDecStmt) which is the increment or decrement statement AST
// node.
// Takes gv (GlobalVariableInfo) which is the global variable information for the target.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileIncDecGlobal(ctx context.Context, statement *ast.IncDecStmt, gv program.GlobalVariableInfo) (program.VarLocation, error) {
	currentLocation := c.emitGetGlobal(ctx, gv)
	if _, err := c.emitIncDec(ctx, statement.Tok, currentLocation); err != nil {
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(currentLocation, c.incDecStaticType(statement.X))
	c.emitSetGlobal(ctx, gv, currentLocation)
	return program.VarLocation{}, nil
}

// compileIncDecSelector compiles s.Field++ or s.Field--.
//
// Takes statement (*ast.IncDecStmt) which is the increment or decrement statement AST
// node.
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the struct field.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileIncDecSelector(ctx context.Context, statement *ast.IncDecStmt, selectorExpression *ast.SelectorExpr) (program.VarLocation, error) {
	receiverLocation, err := c.compileExpression(ctx, selectorExpression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	selection := c.Info.Selections[selectorExpression]
	if selection == nil {
		return program.VarLocation{}, fmt.Errorf("unresolved selector: %s", selectorExpression.Sel.Name)
	}
	index := selection.Index()
	fieldIndex := safeconv.MustIntToUint8(index[len(index)-1])

	fieldKind := c.kindFor(selection.Type())
	if fieldKind != isa.RegisterInt && fieldKind != isa.RegisterFloat && fieldKind != isa.RegisterUint {
		return program.VarLocation{}, fault.ErrCompileIncDecSelectorNumeric
	}

	if c.tryEmitFusedIncDecStructField(ctx, statement.Tok, selection, fieldKind, receiverLocation) {
		return program.VarLocation{}, nil
	}

	if fieldKind == isa.RegisterInt && len(index) == 1 {
		return c.emitTypedIntFieldIncDec(ctx, statement.Tok, selection, receiverLocation, fieldIndex)
	}

	if len(index) > 1 {
		return c.emitEmbeddedFieldIncDec(ctx, statement.Tok, receiverLocation, selectorExpression, selection, index, fieldKind)
	}

	return c.emitBoxedFieldIncDec(ctx, statement.Tok, selection, receiverLocation, fieldIndex, fieldKind)
}

// emitEmbeddedFieldIncDec performs an increment or decrement on a promoted field by
// walking the full selection path to the leaf field's address.
//
// Takes operatorToken (token.Token) which is INC or DEC.
// Takes receiverLocation (VarLocation) which holds the already-lowered receiver.
// Takes selectorExpression (*ast.SelectorExpr) which is the promoted field selector.
// Takes selection (*types.Selection) which describes the field selection.
// Takes index ([]int) which is the full selection path to the leaf field.
// Takes fieldKind (isa.RegisterKind) which is the register bank of the leaf field.
//
// Returns the result location and any compilation error.
func (c *Compiler) emitEmbeddedFieldIncDec(
	ctx context.Context,
	operatorToken token.Token,
	receiverLocation program.VarLocation,
	selectorExpression *ast.SelectorExpr,
	selection *types.Selection,
	index []int,
	fieldKind isa.RegisterKind,
) (program.VarLocation, error) {
	pointerLocation := c.compileEmbeddedLeafPointerReusing(receiverLocation, selectorExpression.X, index)
	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, currentRegister, pointerLocation.Register, 0)

	valueRegister := c.Scopes.Alloc.AllocTemp(fieldKind)
	program.Emit(c.Function, isa.OpUnpackInterface, valueRegister, currentRegister, uint8(fieldKind))
	valueLocation := program.VarLocation{Register: valueRegister, Kind: fieldKind}
	if _, err := c.emitIncDec(ctx, operatorToken, valueLocation); err != nil {
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(valueLocation, selection.Type())
	genResult := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpPackInterface, genResult, valueRegister, uint8(fieldKind))
	program.Emit(c.Function, isa.OpSetField, pointerLocation.Register, isa.SentinelFieldDeref, genResult)

	return program.VarLocation{}, nil
}

// tryEmitFusedIncDecStructField emits the fused tier-1 isa.SubOpIncStructFieldInt/Uint
// (or Dec) super-instruction when the struct layout resolves at compile time AND the
// field is int/uint kind AND no narrow truncation is required AND the layout index fits
// in uint8.
//
// Saves the isa.OpGetFieldInt, tier-2 IncInt, isa.OpSetFieldInt three-trip and the int
// register temporary. Float is excluded because emitIncDec for floats already needs a
// constant pool entry and the gain wouldn't justify the second sub-op variant.
//
// Takes operatorToken (token.Token) which is the INC/DEC token.
// Takes selection (*types.Selection) which describes the field access.
// Takes fieldKind (isa.RegisterKind) which is the field's register kind.
// Takes receiverLocation (VarLocation) which is the boxed receiver.
//
// Returns true when the fused opcode was emitted.
func (c *Compiler) tryEmitFusedIncDecStructField(ctx context.Context, operatorToken token.Token, selection *types.Selection, fieldKind isa.RegisterKind, receiverLocation program.VarLocation) bool {
	if fieldKind != isa.RegisterInt && fieldKind != isa.RegisterUint {
		return false
	}
	if typemap.NarrowIntegerBitWidth(selection.Type()) != 0 {
		return false
	}
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok {
		return false
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if isa.RegisterKind(layout.RegisterKind) != fieldKind || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		return false
	}
	sub := isaselect.PickIncDecStructFieldSubOp(fieldKind, operatorToken)
	if sub == 0 {
		return false
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), receiverLocation.Register, safeconv.MustUintToUint8(uint(layoutIdx)))
	return true
}

// emitTypedIntFieldIncDec emits inc/dec on a direct int-kind field.
//
// Emits isa.OpGetFieldInt, the inc/dec opcode, an optional narrow-truncation, and
// isa.OpSetFieldInt.
//
// Takes operatorToken (token.Token) which is the INC or DEC token.
// Takes selection (*types.Selection) which describes the field.
// Takes receiverLocation (VarLocation) which is the boxed receiver.
// Takes fieldIndex (uint8) which is the direct field index.
//
// Returns VarLocation which is the empty value location yielded by the statement.
// Returns error when emitIncDec surfaces a sub-emission failure.
func (c *Compiler) emitTypedIntFieldIncDec(
	ctx context.Context,
	operatorToken token.Token,
	selection *types.Selection,
	receiverLocation program.VarLocation,
	fieldIndex uint8,
) (program.VarLocation, error) {
	valueRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	valueLocation := program.VarLocation{Register: valueRegister, Kind: isa.RegisterInt}
	c.EmitTyped(ctx, isa.OpGetFieldInt, valueLocation, receiverLocation, rawOperand(fieldIndex))
	if _, err := c.emitIncDec(ctx, operatorToken, valueLocation); err != nil {
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(valueLocation, selection.Type())
	c.EmitTyped(ctx, isa.OpSetFieldInt, receiverLocation, rawOperand(fieldIndex), valueLocation)
	return program.VarLocation{}, nil
}

// emitBoxedFieldIncDec emits inc/dec on a boxed field.
//
// Used for any field that cannot take the typed-int fast path: unpack the interface
// value, increment or decrement in the field-kind bank, then repack and store.
//
// Takes operatorToken (token.Token) which is the INC or DEC token.
// Takes selection (*types.Selection) which describes the field.
// Takes receiverLocation (VarLocation) which is the boxed receiver.
// Takes fieldIndex (uint8) which is the direct field index.
// Takes fieldKind (isa.RegisterKind) which is the field's register kind.
//
// Returns VarLocation which is the empty value location yielded by the statement.
// Returns error when emitIncDec surfaces a sub-emission failure.
func (c *Compiler) emitBoxedFieldIncDec(
	ctx context.Context,
	operatorToken token.Token,
	selection *types.Selection,
	receiverLocation program.VarLocation,
	fieldIndex uint8,
	fieldKind isa.RegisterKind,
) (program.VarLocation, error) {
	currentRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpGetField, currentRegister, receiverLocation.Register, fieldIndex)

	valueRegister := c.Scopes.Alloc.AllocTemp(fieldKind)
	program.Emit(c.Function, isa.OpUnpackInterface, valueRegister, currentRegister, uint8(fieldKind))
	valueLocation := program.VarLocation{Register: valueRegister, Kind: fieldKind}
	if _, err := c.emitIncDec(ctx, operatorToken, valueLocation); err != nil {
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(valueLocation, selection.Type())
	genResult := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpPackInterface, genResult, valueRegister, uint8(fieldKind))
	program.Emit(c.Function, isa.OpSetField, receiverLocation.Register, fieldIndex, genResult)

	return program.VarLocation{}, nil
}

// compileIncDecUpvalue compiles an inc/dec on a captured upvalue.
//
// Takes statement (*ast.IncDecStmt) which is the increment or decrement statement AST
// node.
// Takes reference (upvalueReference) which is the upvalue reference for the captured
// variable.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileIncDecUpvalue(ctx context.Context, statement *ast.IncDecStmt, reference upvalueReference) (program.VarLocation, error) {
	currentRegister := c.Scopes.Alloc.AllocTemp(reference.kind)
	program.Emit(c.Function, isa.OpGetUpvalue, currentRegister, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
	currentLocation := program.VarLocation{Register: currentRegister, Kind: reference.kind}

	if _, err := c.emitIncDec(ctx, statement.Tok, currentLocation); err != nil {
		c.Scopes.Alloc.FreeTemp(reference.kind, currentRegister)
		return program.VarLocation{}, err
	}
	c.EmitNarrowIntegerTruncation(currentLocation, c.incDecStaticType(statement.X))

	program.Emit(c.Function, isa.OpSetUpvalue, currentRegister, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
	c.Scopes.Alloc.FreeTemp(reference.kind, currentRegister)
	return program.VarLocation{}, nil
}

// emitIncDec emits the actual increment or decrement instruction for a numeric register.
//
// Takes operatorToken (token.Token) which indicates whether this is an increment
// (token.INC) or decrement (token.DEC).
// Takes location (VarLocation) which is the register location of the numeric value to
// modify.
//
// Returns the compiled location and any error encountered. Increment and decrement are
// defined on the numeric banks. Every other kind returns
// fault.ErrCompileIncDecRequiresNumeric from the default rather than falling through.
func (c *Compiler) emitIncDec(_ context.Context, operatorToken token.Token, location program.VarLocation) (program.VarLocation, error) {
	switch location.Kind {
	case isa.RegisterInt:
		if operatorToken == token.INC {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncInt), location.Register)
		} else {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecInt), location.Register)
		}
	case isa.RegisterFloat:
		oneIndex, err := program.AddFloatConstant(c.Function, 1.0)
		if err != nil {
			return program.VarLocation{}, err
		}
		temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterFloat)
		program.EmitWide(c.Function, isa.OpLoadFloatConst, temporaryRegister, oneIndex)
		if operatorToken == token.INC {
			program.Emit(c.Function, isa.OpAddFloat, location.Register, location.Register, temporaryRegister)
		} else {
			program.Emit(c.Function, isa.OpSubFloat, location.Register, location.Register, temporaryRegister)
		}
		c.Scopes.Alloc.FreeTemp(isa.RegisterFloat, temporaryRegister)
	case isa.RegisterUint:
		if operatorToken == token.INC {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2IncUint), location.Register)
		} else {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DecUint), location.Register)
		}
	default:
		return program.VarLocation{}, fault.ErrCompileIncDecRequiresNumeric
	}
	return program.VarLocation{}, nil
}

// callResultKinds determines the register kinds for each result of a call expression.
//
// Takes callExpression (*ast.CallExpr) which is the call expression to determine result
// kinds for.
//
// Returns the slice of register kinds for each result and any error encountered.
func (c *Compiler) callResultKinds(ctx context.Context, callExpression *ast.CallExpr) ([]isa.RegisterKind, error) {
	_, callee, found, err := c.directMultiReturnCallee(ctx, callExpression)
	if err != nil {
		return nil, err
	}
	if found {
		return callee.ResultKinds, nil
	}

	tv := c.Info.Types[callExpression.Fun]
	signature, ok := tv.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("cannot determine result types for call: %T", callExpression.Fun)
	}
	var kinds []isa.RegisterKind
	for v := range signature.Results().Variables() {
		kinds = append(kinds, c.kindFor(v.Type()))
	}
	return kinds, nil
}

// compileMultiReturnAssign compiles an assignment from a multi-return function call.
//
// Takes leftHandSideList ([]ast.Expr) which is the left-hand side expressions to assign
// results to.
// Takes callExpression (*ast.CallExpr) which is the multi-return call expression to
// compile.
//
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the first result location and any error encountered.
func (c *Compiler) compileMultiReturnAssign(ctx context.Context, leftHandSideList []ast.Expr, callExpression *ast.CallExpr, isDefine bool) (program.VarLocation, error) {
	resultKinds, err := c.callResultKinds(ctx, callExpression)
	if err != nil {
		return program.VarLocation{}, err
	}

	returnLocations := make([]program.VarLocation, len(leftHandSideList))
	var deferred []multiReturnDeferredStore

	for i, leftHandSide := range leftHandSideList {
		location, ds, err := c.resolveMultiReturnTarget(ctx, leftHandSide, resultKinds[i], isDefine)
		if err != nil {
			return program.VarLocation{}, err
		}
		returnLocations[i] = location
		if ds != nil {
			deferred = append(deferred, *ds)
		}
	}

	if err := c.emitMultiReturnCall(ctx, callExpression, returnLocations); err != nil {
		return program.VarLocation{}, err
	}

	c.recycleBlankMultiReturnTargets(leftHandSideList, returnLocations)

	for i := range deferred {
		ds := &deferred[i]
		if err := c.emitDeferredStore(ctx, *ds); err != nil {
			return program.VarLocation{}, err
		}
	}
	if isDefine {
		c.promoteDefinedTargets(ctx, leftHandSideList)
	}

	if len(returnLocations) > 0 {
		return returnLocations[0], nil
	}
	return program.VarLocation{}, nil
}

// recycleBlankMultiReturnTargets frees the registers that received results assigned to
// the blank identifier, which nothing reads.
//
// Takes targets ([]ast.Expr) which are the left-hand sides.
// Takes returnLocations ([]program.VarLocation) which received the results, in order.
func (c *Compiler) recycleBlankMultiReturnTargets(targets []ast.Expr, returnLocations []program.VarLocation) {
	for i, target := range targets {
		if identifier, ok := target.(*ast.Ident); ok && identifier.Name == typemap.BlankIdentName {
			c.Scopes.Alloc.RecycleRegister(returnLocations[i].Kind, returnLocations[i].Register)
		}
	}
}

// promoteDefinedTargets heap-promotes the variables a multi-value define declared, once
// their registers hold the values.
//
// Takes targets ([]ast.Expr) which are the left-hand sides of the define.
func (c *Compiler) promoteDefinedTargets(ctx context.Context, targets []ast.Expr) {
	for _, target := range targets {
		if identifier, ok := target.(*ast.Ident); ok {
			c.promoteDefinedIdent(ctx, identifier)
		}
	}
}

// promoteDefinedIdent heap-promotes one identifier a define declared, when the escape
// analysis marked it; blank and undeclared identifiers are left alone.
//
// Takes identifier (*ast.Ident) which may be nil.
func (c *Compiler) promoteDefinedIdent(ctx context.Context, identifier *ast.Ident) {
	if identifier == nil || identifier.Name == typemap.BlankIdentName || c.Info.Defs[identifier] == nil {
		return
	}
	c.tryHeapPromoteCapturedLocal(ctx, identifier.Name, identifier)
}

// resolveMultiReturnTarget resolves a single LHS target for a multi-return assignment.
//
// Takes leftHandSide (ast.Expr) which is the left-hand side expression to resolve.
// Takes kind (isa.RegisterKind) which is the expected register kind for the result.
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the target location, an optional deferred store, and any error.
func (c *Compiler) resolveMultiReturnTarget(ctx context.Context,
	leftHandSide ast.Expr,
	kind isa.RegisterKind,
	isDefine bool,
) (program.VarLocation, *multiReturnDeferredStore, error) {
	switch target := leftHandSide.(type) {
	case *ast.Ident:
		return c.resolveMultiReturnIdent(ctx, target, kind, isDefine)

	case *ast.IndexExpr, *ast.SelectorExpr, *ast.StarExpr:
		prepared, err := c.prepareAssignTarget(ctx, leftHandSide)
		if err != nil {
			return program.VarLocation{}, nil, err
		}
		register := c.Scopes.Alloc.Alloc(kind)
		location := program.VarLocation{Register: register, Kind: kind}
		ds := &multiReturnDeferredStore{prepared: prepared, sourceLocation: location, target: leftHandSide}
		return location, ds, nil

	default:
		return program.VarLocation{}, nil, fmt.Errorf("unsupported assignment target: %T at %s", leftHandSide, c.positionString(leftHandSide.Pos()))
	}
}

// resolveMultiReturnIdent resolves an identifier LHS target for a multi-return
// assignment.
//
// Takes target (*ast.Ident) which is the identifier to resolve.
// Takes kind (isa.RegisterKind) which is the expected register kind for the result.
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the target location, an optional deferred store, and any error.
func (c *Compiler) resolveMultiReturnIdent(ctx context.Context,
	target *ast.Ident,
	kind isa.RegisterKind,
	isDefine bool,
) (program.VarLocation, *multiReturnDeferredStore, error) {
	if target.Name == typemap.BlankIdentName {
		register := c.Scopes.Alloc.Alloc(kind)
		return program.VarLocation{Register: register, Kind: kind}, nil, nil
	}

	if isDefine {
		return c.resolveMultiReturnDefine(ctx, target, kind)
	}

	return c.resolveMultiReturnAssignIdent(ctx, target, kind)
}

// resolveMultiReturnDefine resolves a := target for a multi-return assignment.
//
// Takes target (*ast.Ident) which is the identifier to declare or look up.
// Takes kind (isa.RegisterKind) which is the register kind for the new variable.
//
// Returns the target location, an optional deferred store, and any error.
func (c *Compiler) resolveMultiReturnDefine(ctx context.Context,
	target *ast.Ident,
	kind isa.RegisterKind,
) (program.VarLocation, *multiReturnDeferredStore, error) {
	typeObject := c.Info.Defs[target]
	if typeObject != nil {
		location := c.Scopes.DeclareVar(target.Name, kind)
		return location, nil, nil
	}

	return c.resolveMultiReturnAssignIdent(ctx, target, kind)
}

// resolveMultiReturnAssignIdent resolves a plain = target for a multi-return assignment,
// and the reused name of a := define.
//
// The call writes straight into the variable's register only when that register is a
// plain local of the result's kind; an upvalue, a global, a spilled or indirect local, or
// a variable held in another bank receives the result through a deferred assignment.
//
// Takes target (*ast.Ident) which is the identifier to look up.
// Takes kind (isa.RegisterKind) which is the expected register kind for the result.
//
// Returns the target location, an optional deferred store, and any error.
func (c *Compiler) resolveMultiReturnAssignIdent(_ context.Context,
	target *ast.Ident,
	kind isa.RegisterKind,
) (program.VarLocation, *multiReturnDeferredStore, error) {
	switch c.resolveIdentTarget(target) {
	case identTargetUpvalue, identTargetGlobal:
		return c.deferredIdentStore(target, kind)
	case identTargetLocal, identTargetUnknown:
	}
	location, found := c.Scopes.LookupVar(target.Name)
	if !found || location.IsIndirect || location.IsSpilled || location.Kind != kind {
		return c.deferredIdentStore(target, kind)
	}
	return location, nil, nil
}

// deferredIdentStore gives a multi-return result a fresh register and a deferred store
// that assigns it to the identifier after the call, for targets the call cannot write
// directly (package variables, upvalues, heap cells, spill slots).
//
// Takes target (*ast.Ident) which is the assigned identifier.
// Takes kind (isa.RegisterKind) which is the result's bank.
//
// Returns the temporary location, the deferred store and a nil error.
func (c *Compiler) deferredIdentStore(target *ast.Ident, kind isa.RegisterKind) (program.VarLocation, *multiReturnDeferredStore, error) {
	register := c.Scopes.Alloc.Alloc(kind)
	location := program.VarLocation{Register: register, Kind: kind}
	unprepared := preparedTarget{expr: nil, base: program.VarLocation{}, index: program.VarLocation{}, kind: preparedNone}
	return location, &multiReturnDeferredStore{prepared: unprepared, sourceLocation: location, target: target}, nil
}

// emitMultiReturnCall compiles and emits the call instruction for a multi-return call.
//
// Takes callExpression (*ast.CallExpr) which is the call expression to compile and emit.
// Takes returnLocations ([]VarLocation) which is the pre-allocated return locations for
// each result.
//
// Returns any error encountered during compilation.
func (c *Compiler) emitMultiReturnCall(ctx context.Context, callExpression *ast.CallExpr, returnLocations []program.VarLocation) error {
	functionIndex, callee, found, err := c.directMultiReturnCallee(ctx, callExpression)
	if err != nil {
		return err
	}
	if found {
		return c.emitMultiReturnDirectCall(ctx, callExpression, functionIndex, callee, returnLocations)
	}
	callFun := c.unwrapGenericInstantiation(ctx, callExpression.Fun)
	switch fun := callFun.(type) {
	case *ast.Ident:
		if functionLocation, found := c.Scopes.LookupVar(fun.Name); found {
			return c.emitMultiReturnValueCall(ctx, callExpression, functionLocation, returnLocations)
		}
		return c.emitMultiReturnExpressionCall(ctx, callExpression, returnLocations)

	case *ast.SelectorExpr:
		return c.compileMultiReturnSelectorCall(ctx, fun, callExpression, returnLocations)

	default:
		return c.emitMultiReturnExpressionCall(ctx, callExpression, returnLocations)
	}
}

// directMultiReturnCallee resolves a multi-valued call whose callee names a compiled
// function, specialising a generic callee for this call site's type arguments first.
//
// Takes callExpression (*ast.CallExpr) which is the call.
//
// Returns uint16 which is the function index to call, after specialisation.
// Returns *program.CompiledFunction which is the callee at that index.
// Returns bool which is false when the callee is not a compiled function.
// Returns error when a required specialisation fails.
func (c *Compiler) directMultiReturnCallee(ctx context.Context, callExpression *ast.CallExpr) (uint16, *program.CompiledFunction, bool, error) {
	identifier, ok := c.unwrapGenericInstantiation(ctx, callExpression.Fun).(*ast.Ident)
	if !ok {
		return 0, nil, false, nil
	}
	functionIndex, found := c.functionTable[identifier.Name]
	if !found {
		return 0, nil, false, nil
	}
	specialised, err := c.maybeSpecialiseCallee(ctx, callExpression, functionIndex)
	if err != nil {
		return 0, nil, false, err
	}
	return specialised, c.RootFunction.Functions[specialised], true, nil
}

// emitMultiReturnDirectCall emits a multi-valued call to a compiled function.
//
// Takes callExpression (*ast.CallExpr) which is the call.
// Takes functionIndex (uint16) which is the callee's index in the function table.
// Takes callee (*program.CompiledFunction) which is the function at that index.
// Takes returnLocations ([]program.VarLocation) which receive the results.
//
// Returns error when the arguments fail to compile or the call-site table is full.
func (c *Compiler) emitMultiReturnDirectCall(ctx context.Context, callExpression *ast.CallExpr, functionIndex uint16, callee *program.CompiledFunction, returnLocations []program.VarLocation) error {
	argumentLocations, err := c.compileCallArguments(ctx, callExpression, callee)
	if err != nil {
		return err
	}
	site := program.CallSite{
		FunctionIndex: functionIndex,
		Arguments:     argumentLocations,
		Returns:       returnLocations,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return addErr
	}
	c.emitCall(isa.SubOpCall, siteIndex)
	return nil
}

// emitMultiReturnExpressionCall compiles a multi-valued call whose callee is a value
// rather than a named function: a captured function variable, a function literal called
// in place, or the result of another call.
//
// Takes callExpression (*ast.CallExpr) which is the call.
// Takes returnLocations ([]VarLocation) which receive the results.
//
// Returns any compilation error.
func (c *Compiler) emitMultiReturnExpressionCall(ctx context.Context, callExpression *ast.CallExpr, returnLocations []program.VarLocation) error {
	functionLocation, err := c.compileExpression(ctx, callExpression.Fun)
	if err != nil {
		return err
	}
	c.boxToGeneral(ctx, &functionLocation)
	return c.emitMultiReturnValueCall(ctx, callExpression, functionLocation, returnLocations)
}

// emitMultiReturnValueCall emits a multi-valued call through a function value held in a
// general register.
//
// Takes callExpression (*ast.CallExpr) which is the call.
// Takes functionLocation (VarLocation) which holds the callee value.
// Takes returnLocations ([]VarLocation) which receive the results.
//
// Returns any compilation error.
func (c *Compiler) emitMultiReturnValueCall(ctx context.Context, callExpression *ast.CallExpr, functionLocation program.VarLocation, returnLocations []program.VarLocation) error {
	argumentLocations, err := c.compileArgumentExpressions(ctx, callExpression)
	if err != nil {
		return err
	}
	argumentTypeNames, argumentTypeStrings := c.resolveArgumentStaticTypes(callExpression)
	site := program.CallSite{
		IsNative:                  true,
		NativeRegister:            functionLocation.Register,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		ArgumentStaticTypeNames:   argumentTypeNames,
		ArgumentStaticTypeStrings: argumentTypeStrings,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return addErr
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	return nil
}

// compileArgumentExpressions compiles all argument expressions for a native call,
// pre-boxing scalar arguments destined for an interface{} parameter so the callee
// receives the source-level type.
//
// Takes callExpression (*ast.CallExpr) which is the call expression.
//
// Returns the compiled argument locations and any error encountered.
func (c *Compiler) compileArgumentExpressions(ctx context.Context, callExpression *ast.CallExpr) ([]program.VarLocation, error) {
	nativeSignature := c.nativeCallSignature(callExpression)
	argumentLocations := make([]program.VarLocation, len(callExpression.Args))
	for i, argument := range callExpression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
		argumentLocations[i] = c.preboxNativeInterfaceArgument(ctx, nativeSignature, i, location)
	}
	return argumentLocations, nil
}

// compileMultiReturnSelectorCall dispatches a multi-return selector call to the
// appropriate code path.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method or function.
// Takes callExpression (*ast.CallExpr) which is the call expression to dispatch.
// Takes returnLocations ([]VarLocation) which is the pre-allocated return locations for
// each result.
//
// Returns any error encountered during compilation.
func (c *Compiler) compileMultiReturnSelectorCall(ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callExpression *ast.CallExpr,
	returnLocations []program.VarLocation,
) error {
	if functionIndex, ok := c.resolveMethodFunction(ctx, selectorExpression); ok {
		fieldPath := c.resolveEmbeddedFieldPath(ctx, selectorExpression)
		return c.emitMethodCallWithReturns(ctx, selectorExpression, callExpression, functionIndex, fieldPath, returnLocations)
	}

	if c.isInterfaceMethodCall(ctx, selectorExpression) {
		return c.emitDynamicMethodCallWithReturns(ctx, selectorExpression, callExpression, returnLocations)
	}

	if handled, err := c.emitLinkedCallWithReturns(ctx, selectorExpression, callExpression, returnLocations); handled || err != nil {
		return err
	}

	return c.emitNativeSelectorCallWithReturns(ctx, selectorExpression, callExpression, returnLocations)
}

// resolveMethodFunction resolves a selector expression to a compiled method function
// index.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression to
// resolve.
//
// Returns the function index and true if found, or zero and false otherwise.
func (c *Compiler) resolveMethodFunction(ctx context.Context, selectorExpression *ast.SelectorExpr) (uint16, bool) {
	tableName, ok := c.resolveMethodTableName(ctx, selectorExpression)
	if !ok {
		return 0, false
	}
	functionIndex, found := c.functionTable[tableName]
	if !found {
		return 0, false
	}

	specialised, err := c.maybeSpecialiseMethod(ctx, selectorExpression, functionIndex)
	if err != nil {
		c.recordStickyError(err)
		return 0, false
	}
	return specialised, true
}

// resolveEmbeddedFieldPath returns the field path for an embedded method receiver.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression to
// resolve the embedded path for.
//
// Returns the field index path excluding the final method index, or nil for direct
// methods.
func (c *Compiler) resolveEmbeddedFieldPath(_ context.Context, selectorExpression *ast.SelectorExpr) []int {
	selection, ok := c.Info.Selections[selectorExpression]
	if !ok {
		return nil
	}
	index := selection.Index()
	if len(index) <= 1 {
		return nil
	}
	return index[:len(index)-1]
}

// emitMethodCallWithReturns compiles an interpreted method call with pre-allocated return
// locations.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method.
// Takes callExpression (*ast.CallExpr) which is the call expression to compile.
// Takes functionIndex (uint16) which is the function index of the compiled method.
// Takes fieldPath ([]int) which is the embedded field path to the receiver, or nil.
// Takes returnLocations ([]VarLocation) which is the pre-allocated return locations for
// each result.
//
// Returns any error encountered during compilation.
func (c *Compiler) emitMethodCallWithReturns(ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callExpression *ast.CallExpr,
	functionIndex uint16,
	fieldPath []int,
	returnLocations []program.VarLocation,
) error {
	receiverLocation, err := c.compileExpression(ctx, selectorExpression.X)
	if err != nil {
		return err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	for _, fieldIndex := range fieldPath {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetField, dest, receiverLocation.Register, safeconv.MustIntToUint8(fieldIndex))
		receiverLocation = program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
	}

	callee := c.RootFunction.Functions[functionIndex]
	compiledArguments, err := c.compileCallArguments(ctx, callExpression, callee)
	if err != nil {
		return err
	}
	argumentLocations := make([]program.VarLocation, 0, 1+len(compiledArguments))
	argumentLocations = append(argumentLocations, receiverLocation)
	argumentLocations = append(argumentLocations, compiledArguments...)

	site := program.CallSite{
		FunctionIndex: functionIndex,
		Arguments:     argumentLocations,
		Returns:       returnLocations,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return addErr
	}
	c.emitCall(isa.SubOpCall, siteIndex)
	return nil
}

// emitDynamicMethodCallWithReturns compiles an interface method call with pre-allocated
// return locations.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the interface method.
// Takes callExpression (*ast.CallExpr) which is the call expression to compile.
// Takes returnLocations ([]VarLocation) which is the pre-allocated return locations for
// each result.
//
// Returns any error encountered during compilation.
func (c *Compiler) emitDynamicMethodCallWithReturns(ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callExpression *ast.CallExpr,
	returnLocations []program.VarLocation,
) error {
	receiverLocation, err := c.compileExpression(ctx, selectorExpression.X)
	if err != nil {
		return err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	argumentLocations := make([]program.VarLocation, 0, 1+len(callExpression.Args))
	argumentLocations = append(argumentLocations, receiverLocation)
	for _, argument := range callExpression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return err
		}
		if typemodel.IsNamedScalarPoolType(location.SourceType) {
			c.boxToGeneralTemp(ctx, &location)
		}
		argumentLocations = append(argumentLocations, location)
	}

	methodIndex, methodErr := program.AddStringConstant(c.Function, selectorExpression.Sel.Name)
	if methodErr != nil {
		return methodErr
	}

	site := program.CallSite{
		Arguments: argumentLocations,
		Returns:   returnLocations,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return addErr
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallMethod, siteIndex)
	program.EmitExtension(c.Function, methodIndex, 0)
	return nil
}

// emitNativeSelectorCallWithReturns compiles a native selector call with pre-allocated
// return locations.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the native function or method.
// Takes callExpression (*ast.CallExpr) which is the call expression to compile.
// Takes returnLocations ([]VarLocation) which is the pre-allocated return locations for
// each result.
//
// Returns any error encountered during compilation.
func (c *Compiler) emitNativeSelectorCallWithReturns(ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callExpression *ast.CallExpr,
	returnLocations []program.VarLocation,
) error {
	functionLocation, err := c.compileSelectorExpression(ctx, selectorExpression)
	if err != nil {
		return err
	}

	argumentLocations, err := c.compileArgumentExpressions(ctx, callExpression)
	if err != nil {
		return err
	}

	argumentTypeNames, argumentTypeStrings := c.resolveArgumentStaticTypes(callExpression)
	site := program.CallSite{
		IsNative:                  true,
		NativeRegister:            functionLocation.Register,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		ArgumentStaticTypeNames:   argumentTypeNames,
		ArgumentStaticTypeStrings: argumentTypeStrings,
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return addErr
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	return nil
}

// emitDeferredStore emits a store instruction for a multi-return LHS target that could
// not be written directly.
//
// Takes ds (multiReturnDeferredStore) which is the deferred store record containing the
// target and source location.
//
// Returns any error encountered during the store emission.
func (c *Compiler) emitDeferredStore(ctx context.Context, ds multiReturnDeferredStore) error {
	switch target := ds.target.(type) {
	case *ast.Ident:
		switch c.resolveIdentTarget(target) {
		case identTargetUpvalue:
			reference := c.upvalueMap[target.Name]
			program.Emit(c.Function, isa.OpSetUpvalue, ds.sourceLocation.Register, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
			return nil
		case identTargetGlobal:
			c.emitSetGlobal(ctx, c.globalVariables[target.Name], ds.sourceLocation)
			return nil
		case identTargetLocal, identTargetUnknown:
			if _, found := c.Scopes.LookupVar(target.Name); found {
				_, err := c.emitIdentAssign(ctx, target, ds.sourceLocation)
				return err
			}
		}
		return fmt.Errorf("deferred store: variable %s not found at %s", target.Name, c.positionString(target.Pos()))
	case *ast.IndexExpr, *ast.SelectorExpr, *ast.StarExpr:
		if ds.prepared.kind == preparedNone {
			_, err := c.emitAssignTarget(ctx, target, ds.sourceLocation)
			return err
		}
		_, err := c.emitPreparedStore(ctx, ds.prepared, ds.sourceLocation)
		return err
	default:
		return fmt.Errorf("unsupported deferred store target: %T at %s", ds.target, c.positionString(ds.target.Pos()))
	}
}

// declareCommaOkTargets declares or looks up the value and ok destination variables for
// comma-ok assignments.
//
// Takes valueIdentifier (*ast.Ident) which is the identifier for the value target.
// Takes okIdentifier (*ast.Ident) which is the identifier for the ok boolean target.
// Takes valueKind (isa.RegisterKind) which is the register kind for the value variable.
// Takes blankValueKind (isa.RegisterKind) which is the register kind to use when the
// value target is blank.
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the value destination location and the ok destination location.
func (c *Compiler) declareCommaOkTargets(
	ctx context.Context,
	valueIdentifier, okIdentifier *ast.Ident,
	valueKind isa.RegisterKind,
	blankValueKind isa.RegisterKind,
	isDefine bool,
) (valueDestination program.VarLocation, okDestination program.VarLocation) {
	if isDefine {
		valueDestination = c.declareCommaOkValue(ctx, valueIdentifier, valueKind, blankValueKind)
		okDestination = c.declareCommaOkBool(ctx, okIdentifier)
	} else {
		valueDestination = c.lookupCommaOkValue(ctx, valueIdentifier, blankValueKind)
		okDestination = c.lookupCommaOkBool(ctx, okIdentifier)
	}

	return valueDestination, okDestination
}

// declareCommaOkValue declares or resolves the value target for a comma-ok define. A
// value the typed-slice classifier admitted (a map lookup yielding an unnamed `[]T`) is
// declared on its typed-slice bank; the producing opcode's general-bank result is then
// adopted by the move that follows.
//
// Takes valueIdentifier (*ast.Ident) which is the identifier for the value target.
// Takes valueKind (isa.RegisterKind) which is the register kind for the value variable.
// Takes blankValueKind (isa.RegisterKind) which is the register kind to use when the
// target is blank.
//
// Returns the resolved value location.
func (c *Compiler) declareCommaOkValue(_ context.Context, valueIdentifier *ast.Ident, valueKind isa.RegisterKind, blankValueKind isa.RegisterKind) program.VarLocation {
	if valueIdentifier.Name != typemap.BlankIdentName {
		typeObject := c.Info.Defs[valueIdentifier]
		if typeObject != nil {
			return c.Scopes.DeclareVar(valueIdentifier.Name, c.typedSliceKindForLocal(valueIdentifier.Name, typeObject.Type(), valueKind))
		}
		location, _ := c.Scopes.LookupVar(valueIdentifier.Name)
		return location
	}
	return program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(blankValueKind), Kind: blankValueKind}
}

// declareCommaOkBool declares or resolves the ok target for a comma-ok define.
//
// Takes okIdentifier (*ast.Ident) which is the identifier for the ok boolean target.
//
// Returns the resolved ok location.
func (c *Compiler) declareCommaOkBool(_ context.Context, okIdentifier *ast.Ident) program.VarLocation {
	if okIdentifier.Name != typemap.BlankIdentName {
		typeObject := c.Info.Defs[okIdentifier]
		if typeObject != nil {
			return c.Scopes.DeclareVar(okIdentifier.Name, isa.RegisterInt)
		}
		location, _ := c.Scopes.LookupVar(okIdentifier.Name)
		return location
	}
	return program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(isa.RegisterInt), Kind: isa.RegisterInt}
}

// lookupCommaOkValue looks up the value target for a comma-ok assign.
//
// Takes valueIdentifier (*ast.Ident) which is the identifier for the value target.
// Takes blankValueKind (isa.RegisterKind) which is the register kind to use when the
// target is blank.
//
// Returns the resolved value location.
func (c *Compiler) lookupCommaOkValue(_ context.Context, valueIdentifier *ast.Ident, blankValueKind isa.RegisterKind) program.VarLocation {
	if valueIdentifier.Name != typemap.BlankIdentName {
		location, _ := c.Scopes.LookupVar(valueIdentifier.Name)
		return location
	}
	return program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(blankValueKind), Kind: blankValueKind}
}

// lookupCommaOkBool looks up the ok target for a comma-ok assign.
//
// Takes okIdentifier (*ast.Ident) which is the identifier for the ok boolean target.
//
// Returns the resolved ok location.
func (c *Compiler) lookupCommaOkBool(_ context.Context, okIdentifier *ast.Ident) program.VarLocation {
	if okIdentifier.Name != typemap.BlankIdentName {
		location, _ := c.Scopes.LookupVar(okIdentifier.Name)
		return location
	}
	return program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(isa.RegisterInt), Kind: isa.RegisterInt}
}

// compileMapCommaOk compiles v, ok := m[k] or v, ok = m[k].
//
// Takes leftHandSideList ([]ast.Expr) which is the left-hand side expressions for value
// and ok targets.
// Takes indexExpression (*ast.IndexExpr) which is the map index expression to evaluate.
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the value destination location and any error encountered.
func (c *Compiler) compileMapCommaOk(ctx context.Context, leftHandSideList []ast.Expr, indexExpression *ast.IndexExpr, isDefine bool) (program.VarLocation, error) {
	mapLocation, err := c.compileExpression(ctx, indexExpression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &mapLocation)
	keyLocation, err := c.compileExpression(ctx, indexExpression.Index)
	if err != nil {
		return program.VarLocation{}, err
	}
	valueIdentifier, okIdentifier, err := mapCommaOkTargetIdents(leftHandSideList)
	if err != nil {
		return program.VarLocation{}, err
	}
	valueKind, keyKind, elementType, err := c.mapCommaOkKinds(indexExpression, isDefine)
	if err != nil {
		return program.VarLocation{}, err
	}
	if op, ok := selectTypedMapIndexOkOpcode(keyKind, valueKind, keyLocation.Kind); ok {
		return c.emitTypedMapCommaOk(ctx, typedMapCommaOkRequest{
			valueIdentifier: valueIdentifier,
			okIdentifier:    okIdentifier,
			mapLocation:     mapLocation,
			keyLocation:     keyLocation,
			op:              op,
			valueKind:       valueKind,
			isDefine:        isDefine,
		}), nil
	}
	c.boxElementToGeneral(ctx, &keyLocation)
	valueDestination, okDestination := c.declareCommaOkTargets(ctx, valueIdentifier, okIdentifier, valueKind, isa.RegisterGeneral, isDefine)
	genDest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpMapIndexOk, genDest, mapLocation.Register, keyLocation.Register)
	program.Emit(c.Function, isa.OpExt, okRegister, 0, 0)
	c.storeCommaOkResult(ctx, valueDestination, genDest, elementType)
	c.emitMove(ctx, okDestination, program.VarLocation{Register: okRegister, Kind: isa.RegisterInt})
	if isDefine {
		c.promoteDefinedIdent(ctx, valueIdentifier)
	}
	return valueDestination, nil
}

// mapCommaOkKinds resolves the value kind, key kind, and value element type from a map
// index expression. For define-form (`v, ok := m[k]`) the source must be a map type; the
// assign-form falls back to general-bank kinds when the type checker hasn't recorded a
// map.
//
// Takes indexExpression (*ast.IndexExpr) which is the map index.
// Takes isDefine (bool) which differentiates `:=` from `=`.
//
// Returns the value kind, key kind, value element type, and any validation error.
func (c *Compiler) mapCommaOkKinds(indexExpression *ast.IndexExpr, isDefine bool) (valueKind isa.RegisterKind, keyKind isa.RegisterKind, elementType types.Type, err error) {
	mapType, ok := c.Info.Types[indexExpression.X].Type.Underlying().(*types.Map)
	if !ok {
		if isDefine {
			return isa.RegisterGeneral, isa.RegisterGeneral, nil, fault.ErrCompileMapCommaOkSourceNotMap
		}
		return isa.RegisterGeneral, isa.RegisterGeneral, nil, nil
	}
	elementType = mapType.Elem()
	return c.kindFor(elementType), c.kindFor(mapType.Key()), elementType, nil
}

// emitTypedMapCommaOk emits the typed comma-ok bytecode (typed key, typed value, ok flag)
// for the matched primitive (key, value) kinds. Allocates the typed destination, emits
// the op + extension word, and moves the result into the declared targets.
//
// Takes request (typedMapCommaOkRequest) which carries the matched opcode, identifiers,
// register locations, value kind, and the declare-vs-assign flag.
//
// Returns the value destination location.
func (c *Compiler) emitTypedMapCommaOk(ctx context.Context, request typedMapCommaOkRequest) program.VarLocation {
	valueDestination, okDestination := c.declareCommaOkTargets(ctx, request.valueIdentifier, request.okIdentifier, request.valueKind, isa.RegisterGeneral, request.isDefine)
	typedDest := c.Scopes.Alloc.Alloc(request.valueKind)
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, request.op, typedDest, request.mapLocation.Register, request.keyLocation.Register)
	program.Emit(c.Function, isa.OpExt, okRegister, 0, 0)
	if valueDestination.IsSpilled {
		c.emitSpillStore(ctx, typedDest, request.valueKind, valueDestination.SpillSlot)
	} else if valueDestination.Register != typedDest || valueDestination.Kind != request.valueKind {
		c.emitMove(ctx, valueDestination, program.VarLocation{Register: typedDest, Kind: request.valueKind})
	}
	c.emitMove(ctx, okDestination, program.VarLocation{Register: okRegister, Kind: isa.RegisterInt})
	if request.isDefine {
		c.promoteDefinedIdent(ctx, request.valueIdentifier)
	}
	return valueDestination
}

// storeCommaOkResult writes a comma-ok value into its declared target, unboxing from the
// general-bank scratch register as appropriate for the destination bank.
//
// Takes valueDestination (VarLocation) which is the destination for the comma-ok value.
// Takes genDest (uint8) which is the general register holding the boxed value.
// Takes elementType (types.Type) which is the static type of the value, or nil.
func (c *Compiler) storeCommaOkResult(ctx context.Context, valueDestination program.VarLocation, genDest uint8, elementType types.Type) {
	switch {
	case valueDestination.Kind == isa.RegisterGeneral, isa.IsTypedSliceKind(valueDestination.Kind):
		c.emitMoveTyped(ctx, valueDestination, program.VarLocation{Register: genDest, Kind: isa.RegisterGeneral}, elementType)
	case valueDestination.IsSpilled:
		scratch := c.Scopes.Alloc.AllocTemp(valueDestination.Kind)
		program.Emit(c.Function, isa.OpUnpackInterface, scratch, genDest, uint8(valueDestination.Kind))
		c.emitSpillStore(ctx, scratch, valueDestination.Kind, valueDestination.SpillSlot)
		c.Scopes.Alloc.FreeTemp(valueDestination.Kind, scratch)
	default:
		program.Emit(c.Function, isa.OpUnpackInterface, valueDestination.Register, genDest, uint8(valueDestination.Kind))
	}
}

// compileChannelReceiveCommaOk compiles v, ok := <-ch or v, ok = <-ch.
//
// Takes leftHandSideList ([]ast.Expr) which is the left-hand side expressions for value
// and ok targets.
// Takes unaryExpression (*ast.UnaryExpr) which is the channel receive expression to
// compile.
//
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the value destination location and any error encountered.
func (c *Compiler) compileChannelReceiveCommaOk(ctx context.Context, leftHandSideList []ast.Expr, unaryExpression *ast.UnaryExpr, isDefine bool) (program.VarLocation, error) {
	channelLocation, err := c.compileExpression(ctx, unaryExpression.X)
	if err != nil {
		return program.VarLocation{}, err
	}

	tv := c.Info.Types[unaryExpression.X]
	channelType, ok := tv.Type.Underlying().(*types.Chan)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileChanRecvCommaOkSourceNotChan
	}
	elementType := channelType.Elem()
	valueKind := c.kindFor(elementType)

	valueIdentifier, ok := leftHandSideList[0].(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileChanRecvCommaOkValueNotIdent
	}
	okIdentifier, ok := leftHandSideList[1].(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileChanRecvCommaOkOkNotIdent
	}

	valueDestination, okDestination := c.declareCommaOkTargets(ctx, valueIdentifier, okIdentifier, valueKind, valueKind, isDefine)

	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	destinationRegister := c.Scopes.Alloc.Alloc(valueKind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpChannelReceive), channelLocation.Register, okRegister)
	program.Emit(c.Function, isa.OpExt, destinationRegister, uint8(valueKind), 0)

	c.emitMoveTyped(ctx, valueDestination, program.VarLocation{Register: destinationRegister, Kind: valueKind}, elementType)
	c.emitMove(ctx, okDestination, program.VarLocation{Register: okRegister, Kind: isa.RegisterInt})

	if isDefine {
		c.promoteDefinedIdent(ctx, valueIdentifier)
	}
	return valueDestination, nil
}

// compileTypeAssertCommaOk compiles v, ok := x.(T) or v, ok = x.(T).
//
// Takes leftHandSideList ([]ast.Expr) which is the left-hand side expressions for value
// and ok targets.
// Takes assertExpr (*ast.TypeAssertExpr) which is the type assertion expression to
// compile.
//
// Takes isDefine (bool) which indicates whether this is a := define or = assign.
//
// Returns the value destination location and any error encountered.
func (c *Compiler) compileTypeAssertCommaOk(ctx context.Context, leftHandSideList []ast.Expr, assertExpr *ast.TypeAssertExpr, isDefine bool) (program.VarLocation, error) {
	sourceLocation, err := c.compileExpression(ctx, assertExpr.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &sourceLocation)

	targetType := c.Info.Types[assertExpr.Type].Type
	reflectType := c.typeAssertReflectType(ctx, targetType)
	methodNames := interfaceTargetMethodNames(c.substitutedType(targetType))
	typeIndex, typeErr := program.AddTypeRefWithMethods(c.Function, reflectType, methodNames)
	if typeErr != nil {
		return program.VarLocation{}, typeErr
	}
	valueKind := c.kindFor(targetType)

	valueIdentifier, ok := leftHandSideList[0].(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileTypeAssertCommaOkValueNotIdent
	}
	okIdentifier, ok := leftHandSideList[1].(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileTypeAssertCommaOkOkNotIdent
	}

	valueDestination, okDestination := c.declareCommaOkTargets(ctx, valueIdentifier, okIdentifier, valueKind, isa.RegisterGeneral, isDefine)

	genDest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	program.Emit(c.Function, isa.OpTypeAssert, genDest, sourceLocation.Register, okRegister)
	program.EmitExtension(c.Function, typeIndex, 0)

	c.storeCommaOkResult(ctx, valueDestination, genDest, targetType)
	c.emitMove(ctx, okDestination, program.VarLocation{Register: okRegister, Kind: isa.RegisterInt})

	if isDefine {
		c.promoteDefinedIdent(ctx, valueIdentifier)
	}
	return valueDestination, nil
}

// wrapIncDecResult forwards a sub-Compiler's result pair, wrapping any error with the
// shared incDecWrapMsg.
//
// Takes result (VarLocation) which is the sub-Compiler's location.
// Takes err (error) which is the sub-Compiler's error, possibly nil.
//
// Returns the result unchanged on success, or a zero location with the wrapped error.
func wrapIncDecResult(result program.VarLocation, err error) (program.VarLocation, error) {
	if err != nil {
		return program.VarLocation{}, fmt.Errorf(incDecWrapMsg, err)
	}
	return result, nil
}

// mapCommaOkTargetIdents extracts the value and ok identifier targets from a comma-ok
// left-hand side, returning a descriptive error when either target is not a bare
// identifier.
//
// Takes leftHandSideList ([]ast.Expr) which is the LHS of the comma-ok assignment.
//
// Returns the value identifier, the ok identifier, and any validation error.
func mapCommaOkTargetIdents(leftHandSideList []ast.Expr) (valueIdentifier *ast.Ident, okIdentifier *ast.Ident, err error) {
	value, ok := leftHandSideList[0].(*ast.Ident)
	if !ok {
		return nil, nil, fault.ErrCompileMapCommaOkValueNotIdent
	}
	okIdent, ok := leftHandSideList[1].(*ast.Ident)
	if !ok {
		return nil, nil, fault.ErrCompileMapCommaOkOkNotIdent
	}
	return value, okIdent, nil
}
