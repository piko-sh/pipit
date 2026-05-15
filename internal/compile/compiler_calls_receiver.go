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
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// compileMethodReceiverWithPath emits the receiver for a method-call site, applying Go's
// implicit address-take when the callee is a pointer-receiver method.
//
// Takes receiverExpr (ast.Expr) which is the source-level receiver expression.
// Takes fieldPath ([]int) which is the embedded-field path with the method index removed.
// Takes callee (*CompiledFunction) which carries isPointerReceiver.
//
// Returns the location holding the final receiver value.
func (c *Compiler) compileMethodReceiverWithPath(ctx context.Context, receiverExpr ast.Expr, fieldPath []int, callee *program.CompiledFunction) (program.VarLocation, error) {
	finalType := finalReceiverTypeAfterFieldPath(c.Info, receiverExpr, fieldPath)
	_, finalIsPointer := pointerUnderlying(finalType)
	if callee.IsPointerReceiver && !finalIsPointer {
		return c.compileMethodReceiverAsPointer(ctx, receiverExpr, fieldPath)
	}

	receiverLocation, err := c.compileExpression(ctx, receiverExpr)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)
	for _, fieldIndex := range fieldPath {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetField, dest, receiverLocation.Register, safeconv.MustIntToUint8(fieldIndex))
		receiverLocation = program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
	}

	if !callee.IsPointerReceiver && finalIsPointer {
		derefRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDeref, derefRegister, receiverLocation.Register, 0)
		receiverLocation = program.VarLocation{Register: derefRegister, Kind: isa.RegisterGeneral}
	}
	return receiverLocation, nil
}

// compileMethodReceiverAsPointer compiles a pointer-receiver method's receiver expression
// into a *T pointer.
//
// Takes receiverExpr (ast.Expr) which is the source-level receiver.
// Takes fieldPath ([]int) which is the embedded-field traversal.
//
// Returns the *T pointer location and any compilation error.
func (c *Compiler) compileMethodReceiverAsPointer(ctx context.Context, receiverExpr ast.Expr, fieldPath []int) (program.VarLocation, error) {
	addrLoc, err := c.compileAddressOfReceiverExpr(ctx, receiverExpr)
	if err != nil {
		return program.VarLocation{}, err
	}
	return c.walkFieldPointerPath(addrLoc, receiverExpr, fieldPath), nil
}

// compileEmbeddedParentPointerReusing produces a pointer to the leaf field's parent
// struct without re-lowering the receiver, so the caller can index the live field with
// OpSetField/OpGetField.
//
// Takes receiverLocation (VarLocation) which holds the already-lowered receiver.
// Takes receiverExpr (ast.Expr) which supplies the static type chain for the walk.
// Takes fieldPath ([]int) which is the full selection index to the leaf field.
//
// Returns a general-banked pointer to the struct that directly contains the leaf field.
func (c *Compiler) compileEmbeddedParentPointerReusing(receiverLocation program.VarLocation, receiverExpr ast.Expr, fieldPath []int) program.VarLocation {
	baseRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, baseRegister, receiverLocation.Register, engine.AddrSourceStable)
	base := program.VarLocation{Register: baseRegister, Kind: isa.RegisterGeneral}
	return c.walkFieldPointerPath(base, receiverExpr, fieldPath[:len(fieldPath)-1])
}

// compileEmbeddedLeafPointerReusing produces the leaf field's address without re-lowering
// the receiver.
//
// Takes receiverLocation (VarLocation) which holds the already-lowered receiver.
// Takes receiverExpr (ast.Expr) which supplies the static type chain for the walk.
// Takes fieldPath ([]int) which is the selection index to the leaf field.
//
// Returns a general-banked pointer to the leaf field.
func (c *Compiler) compileEmbeddedLeafPointerReusing(receiverLocation program.VarLocation, receiverExpr ast.Expr, fieldPath []int) program.VarLocation {
	baseRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, baseRegister, receiverLocation.Register, engine.AddrSourceStable)
	base := program.VarLocation{Register: baseRegister, Kind: isa.RegisterGeneral}
	return c.walkFieldPointerPath(base, receiverExpr, fieldPath)
}

// walkFieldPointerPath walks fieldPath from a base pointer to the final field's location.
//
// Emits isa.OpDeref/isa.OpGetField per hop. A pointer-typed field is followed by its
// value so the next hop dereferences the pointee; a value-typed field is addressed in
// place with isa.OpAddr. With an empty fieldPath the base pointer is returned unchanged.
// baseAddr must be a pointer whose dereference yields the receiver, and receiverExpr
// supplies the static type chain so pointer intermediates are threaded correctly.
//
// Takes baseAddr (VarLocation) which is the receiver's address, a general-banked pointer.
// Takes receiverExpr (ast.Expr) which types the base of the walk.
// Takes fieldPath ([]int) which is the field-index chain to walk.
//
// Returns the final field pointer location.
func (c *Compiler) walkFieldPointerPath(baseAddr program.VarLocation, receiverExpr ast.Expr, fieldPath []int) program.VarLocation {
	addrLoc := baseAddr
	currentType := receiverExprType(c.Info, receiverExpr)
	for _, fieldIndex := range fieldPath {
		if element, ok := pointerUnderlying(currentType); ok {
			currentType = element
		}
		fieldType := structFieldType(currentType, fieldIndex)
		fieldReg := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDeref, fieldReg, addrLoc.Register, 0)
		program.Emit(c.Function, isa.OpGetField, fieldReg, fieldReg, safeconv.MustIntToUint8(fieldIndex))
		if _, fieldIsPointer := pointerUnderlying(fieldType); fieldIsPointer {
			addrLoc = program.VarLocation{Register: fieldReg, Kind: isa.RegisterGeneral}
		} else {
			addrReg := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
			program.Emit(c.Function, isa.OpAddr, addrReg, fieldReg, engine.AddrSourceStable)
			addrLoc = program.VarLocation{Register: addrReg, Kind: isa.RegisterGeneral}
		}
		currentType = fieldType
	}
	return addrLoc
}

// compileAddressOfReceiverExpr produces a *T pointer for a method receiver expression.
//
// Mirrors compileAddressOf's dispatch but takes a bare ast.Expr rather than the
// *ast.UnaryExpr wrapper that compileAddressOf uses for source-level `&x` syntax.
// Method-call address-takes are implicit (Go's auto-rewrite) and have no UnaryExpr in the
// AST. Falls back to compileExpression + isa.OpAddr when the receiver kind is not
// Ident/Selector/Index/StarExpr; isa.OpAddr's CanAddr branch handles already-addressable
// values (slice elements, addressable temporaries), and the non-addressable branch
// allocates fresh storage which is acceptable for read-only-receiver use cases (rare in
// valid Go since pointer-receiver methods on non-addressable values are forbidden).
//
// Takes expression (ast.Expr) which is the receiver expression.
//
// Returns the pointer location and any compilation error.
func (c *Compiler) compileAddressOfReceiverExpr(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	switch e := expression.(type) {
	case *ast.Ident:
		if loc, ok := c.compileAddressOfUpvalue(e); ok {
			return loc, nil
		}
		if loc, ok := c.compileAddressOfIdent(ctx, e); ok {
			return loc, nil
		}
	case *ast.SelectorExpr:
		return c.compileAddressOfSelector(ctx, e)
	case *ast.IndexExpr:
		return c.compileAddressOfIndex(ctx, e)
	case *ast.StarExpr:
		return c.compileExpression(ctx, e.X)
	}
	operand, err := c.compileExpression(ctx, expression)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &operand)
	destination := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, destination, operand.Register, 0)
	return program.VarLocation{Register: destination, Kind: isa.RegisterGeneral}, nil
}

// compileMethodExprDirectCall compiles a direct call to a method expression like
// Type.Method(receiver, arguments...).
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
// Takes selection (*types.Selection) which is the type-checker selection information for
// the method expression.
//
// Returns VarLocation holding the method call result and any compilation error.
func (c *Compiler) compileMethodExprDirectCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr, selection *types.Selection) (program.VarLocation, error) {
	functionIndex, ok := c.resolveMethodExprFunction(ctx, selectorExpression)
	if !ok {
		functionLocation, err := c.compileSelectorExpression(ctx, selectorExpression)
		if err != nil {
			return program.VarLocation{}, err
		}
		return c.compileNativeCallFromLocation(ctx, expression, functionLocation)
	}

	callee := c.RootFunction.Functions[functionIndex]

	if len(expression.Args) == 0 {
		return program.VarLocation{}, fault.ErrCompileMethodExprMissingReceiver
	}

	receiverLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)
	c.navigateFieldPath(ctx, selection.Index(), &receiverLocation)

	argumentLocations := make([]program.VarLocation, 0, len(expression.Args))
	argumentLocations = append(argumentLocations, receiverLocation)
	for _, argument := range expression.Args[1:] {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}
		argumentLocations = append(argumentLocations, location)
	}

	returnLocations := c.allocReturnRegisters(ctx, callee.ResultKinds)
	var resultLocation program.VarLocation
	if len(returnLocations) > 0 {
		resultLocation = returnLocations[0]
	}

	site := program.CallSite{FunctionIndex: functionIndex, Arguments: argumentLocations, Returns: returnLocations}
	if int(functionIndex) < len(c.RootFunction.Functions) {
		site.CachedCallee = c.RootFunction.Functions[functionIndex]
	}
	if site.CachedCallee != nil {
		site.ArgCopyProgram = program.BuildCallArgCopyProgram(site.Arguments, site.CachedCallee.ParameterKinds, site.CachedCallee.ParameterRegisters)
	}
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.emitCall(isa.SubOpCall, siteIndex)

	return c.unpackGenericResult(ctx, expression, resultLocation), nil
}

// resolveMethodExprFunction resolves a method expression selector to a functionTable
// index.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression to
// resolve.
//
// Returns the functionTable index and true if found, or zero and false otherwise.
func (c *Compiler) resolveMethodExprFunction(ctx context.Context, selectorExpression *ast.SelectorExpr) (uint16, bool) {
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

// navigateFieldPath emits isa.OpGetField instructions to traverse embedded struct field
// indices (all but the last index, which identifies the method itself).
//
// Takes index ([]int) which is the field index path from the type-checker selection.
// Takes receiverLocation (*VarLocation) which is the receiver location, updated in place.
func (c *Compiler) navigateFieldPath(_ context.Context, index []int, receiverLocation *program.VarLocation) {
	if len(index) <= 1 {
		return
	}
	for _, fieldIndex := range index[:len(index)-1] {
		destination := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetField, destination, receiverLocation.Register, safeconv.MustIntToUint8(fieldIndex))
		*receiverLocation = program.VarLocation{Register: destination, Kind: isa.RegisterGeneral}
	}
}

// receiverExprType returns the go/types type associated with a method receiver
// expression, or nil when the type-checker has no entry. Used to walk the embedded-field
// path.
//
// Takes info (*types.Info) which carries the type-checker output.
// Takes expression (ast.Expr) which is the receiver expression.
//
// Returns the receiver's type or nil.
func receiverExprType(info *types.Info, expression ast.Expr) types.Type {
	if info == nil || expression == nil {
		return nil
	}
	typeAndValue, ok := info.Types[expression]
	if !ok {
		return nil
	}
	return typeAndValue.Type
}

// pointerUnderlying reports whether t's underlying type is *T and returns the element
// type when so. Centralised so the receiver type-walk doesn't sprinkle its own type
// assertions.
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns the pointee type and true when t is a pointer; nil and false otherwise
// (including when t is nil).
func pointerUnderlying(t types.Type) (types.Type, bool) {
	if t == nil {
		return nil, false
	}
	pointer, ok := t.Underlying().(*types.Pointer)
	if !ok {
		return nil, false
	}
	return pointer.Elem(), true
}

// structFieldType returns the type of the given field index within t's underlying struct,
// or nil when t isn't a struct or the index is out of range. Used by the embedded-field
// walk.
//
// Takes t (types.Type) which is the parent type.
// Takes index (int) which is the field position.
//
// Returns the field's type or nil.
func structFieldType(t types.Type, index int) types.Type {
	if t == nil {
		return nil
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	if index < 0 || index >= st.NumFields() {
		return nil
	}
	return st.Field(index).Type()
}

// finalReceiverTypeAfterFieldPath walks selectorExpression.X's type through fieldPath to
// compute the type at the end of the traversal.
//
// Auto-derefs through pointer fields the way Go's selector rules do. Used by
// compileMethodReceiverWithPath to decide whether the receiver at the call site needs an
// implicit address-take to satisfy a pointer-receiver method.
//
// Takes info (*types.Info) which is the type-checker output.
// Takes receiverExpr (ast.Expr) which is the source-level receiver.
// Takes fieldPath ([]int) which is the embedded-field traversal.
//
// Returns the final type, or nil when type info is missing.
func finalReceiverTypeAfterFieldPath(info *types.Info, receiverExpr ast.Expr, fieldPath []int) types.Type {
	current := receiverExprType(info, receiverExpr)
	for _, index := range fieldPath {
		if element, ok := pointerUnderlying(current); ok {
			current = element
		}
		fieldType := structFieldType(current, index)
		if fieldType == nil {
			return current
		}
		current = fieldType
	}
	return current
}
