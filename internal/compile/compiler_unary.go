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

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

// compileUnaryExpression compiles a unary expression.
//
// Takes expression (*ast.UnaryExpr) which is the unary expression AST node to compile.
//
// Returns the compiled variable location and any compilation error.
func (c *Compiler) compileUnaryExpression(ctx context.Context, expression *ast.UnaryExpr) (program.VarLocation, error) {
	if location, handled, err := c.compileAddressOfWithoutReading(ctx, expression); handled {
		return location, err
	}
	if expression.Op == token.AND {
		return c.compileAddressOf(ctx, expression)
	}

	operand, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	if expression.Op == token.SUB || expression.Op == token.ADD || expression.Op == token.XOR {
		if operand, err = c.unboxScalarOperand(ctx, operand, c.staticTypeOf(expression.X)); err != nil {
			return program.VarLocation{}, err
		}
	}

	switch expression.Op {
	case token.SUB:
		return c.compileNarrowedUnary(ctx, expression, operand, c.compileUnarySub)
	case token.ADD:
		return operand, nil
	case token.NOT:
		return c.compileUnaryNot(ctx, operand)
	case token.XOR:
		return c.compileNarrowedUnary(ctx, expression, operand, c.compileUnaryXor)
	case token.ARROW:
		return c.compileUnaryArrow(ctx, expression, operand)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported unary operator: %s at %s", expression.Op, c.positionString(expression.Pos()))
	}
}

// compileUnarySub compiles the unary negation operator (-x).
//
// Takes operand (VarLocation) which is the compiled operand to negate.
//
// Returns the negated variable location and any compilation error. Unary minus is defined
// on the numeric banks. Every other kind returns fault.ErrCompileUnaryMinusUnsupported
// from the default.
func (c *Compiler) compileUnarySub(_ context.Context, operand program.VarLocation) (program.VarLocation, error) {
	switch operand.Kind {
	case isa.RegisterInt:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNegInt), dest, operand.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
	case isa.RegisterFloat:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterFloat)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNegFloat), dest, operand.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterFloat}, nil
	case isa.RegisterUint:
		zeroReg := c.Scopes.Alloc.AllocTemp(isa.RegisterUint)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), zeroReg, uint8(isa.RegisterUint))
		dest := c.Scopes.Alloc.Alloc(isa.RegisterUint)
		program.Emit(c.Function, isa.OpSubUint, dest, zeroReg, operand.Register)
		c.Scopes.Alloc.FreeTemp(isa.RegisterUint, zeroReg)
		return program.VarLocation{Register: dest, Kind: isa.RegisterUint}, nil
	case isa.RegisterComplex:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterComplex)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNegComplex), dest, operand.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterComplex}, nil
	default:
		return program.VarLocation{}, fault.ErrCompileUnaryMinusUnsupported
	}
}

// compileUnaryNot compiles the logical NOT operator (!x).
//
// Takes operand (VarLocation) which is the compiled operand to logically negate.
//
// Returns the negated variable location and any compilation error.
func (c *Compiler) compileUnaryNot(_ context.Context, operand program.VarLocation) (program.VarLocation, error) {
	if operand.Kind == isa.RegisterBool {
		intRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBoolToInt), intRegister, operand.Register)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNot), intRegister, intRegister)
		dest := c.Scopes.Alloc.Alloc(isa.RegisterBool)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), dest, intRegister)
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt, intRegister)
		return program.VarLocation{Register: dest, Kind: isa.RegisterBool}, nil
	}
	if operand.Kind == isa.RegisterGeneral {
		booleanRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterBool)
		program.Emit(c.Function, isa.OpUnpackInterface, booleanRegister, operand.Register, uint8(isa.RegisterBool))
		intRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBoolToInt), intRegister, booleanRegister)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNot), intRegister, intRegister)
		dest := c.Scopes.Alloc.Alloc(isa.RegisterBool)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), dest, intRegister)
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt, intRegister)
		c.Scopes.Alloc.FreeTemp(isa.RegisterBool, booleanRegister)
		return program.VarLocation{Register: dest, Kind: isa.RegisterBool}, nil
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpNot), dest, operand.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
}

// compileUnaryXor compiles the bitwise complement operator (^x).
//
// Takes operand (VarLocation) which is the compiled operand to complement.
//
// Returns the complemented variable location and any compilation error. Unary xor is
// defined on the integer banks. Every other kind returns
// fault.ErrCompileUnaryXorRequiresInteger from the default.
func (c *Compiler) compileUnaryXor(_ context.Context, operand program.VarLocation) (program.VarLocation, error) {
	switch operand.Kind {
	case isa.RegisterInt:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBitNot), dest, operand.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
	case isa.RegisterUint:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterUint)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBitNotUint), dest, operand.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterUint}, nil
	default:
		return program.VarLocation{}, fault.ErrCompileUnaryXorRequiresInteger
	}
}

// compileUnaryArrow compiles the channel receive operator (<-ch).
//
// Takes expression (*ast.UnaryExpr) which is the unary expression AST node containing the
// channel receive.
// Takes operand (VarLocation) which is the compiled channel operand.
//
// Returns the received value location and any compilation error.
func (c *Compiler) compileUnaryArrow(_ context.Context, expression *ast.UnaryExpr, operand program.VarLocation) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureChannels, expression.OpPos); err != nil {
		return program.VarLocation{}, err
	}
	if operand.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, fault.ErrCompileChannelReceiveRequiresGeneral
	}
	tv, ok := c.Info.Types[expression.X]
	if !ok || tv.Type == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for channel receive operand at %s", fault.ErrCompilation, c.positionString(expression.X.Pos()))
	}
	channelType, isChan := tv.Type.Underlying().(*types.Chan)
	if !isChan {
		return program.VarLocation{}, fmt.Errorf("%w: channel receive operand is not a channel type at %s", fault.ErrCompilation, c.positionString(expression.X.Pos()))
	}
	elementType := channelType.Elem()
	resultKind := c.kindFor(elementType)
	destinationRegister := c.Scopes.Alloc.Alloc(resultKind)
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpChannelReceive), operand.Register, okRegister)
	program.Emit(c.Function, isa.OpExt, destinationRegister, uint8(resultKind), 0)
	return program.VarLocation{Register: destinationRegister, Kind: resultKind}, nil
}

// compileAddressOf compiles the address-of operator (&x), dispatching to specialised
// handlers for identifiers and selectors.
//
// Takes expression (*ast.UnaryExpr) which is the unary expression AST node.
//
// Returns the pointer variable location and any compilation error.
func (c *Compiler) compileAddressOf(ctx context.Context, expression *ast.UnaryExpr) (program.VarLocation, error) {
	if identifier, ok := expression.X.(*ast.Ident); ok {
		if location, ok := c.compileAddressOfUpvalue(identifier); ok {
			return location, nil
		}
		if location, ok := c.compileAddressOfIdent(ctx, identifier); ok {
			return location, nil
		}
	}

	if selectorExpression, ok := expression.X.(*ast.SelectorExpr); ok {
		return c.compileAddressOfSelector(ctx, selectorExpression)
	}

	operand, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &operand)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, dest, operand.Register, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileAddressOfUpvalue handles &identifier for a captured upvalue that was
// heap-promoted, loading the cell's *T pointer via OpGetUpvalue so the returned address
// aliases the shared cell.
//
// Takes identifier (*ast.Ident) which is the captured-name expression.
//
// Returns (location, true) when the name resolves to an indirect upvalue and the cell
// pointer was loaded; (_, false) otherwise.
func (c *Compiler) compileAddressOfUpvalue(identifier *ast.Ident) (program.VarLocation, bool) {
	reference, ok := c.upvalueMap[identifier.Name]
	if !ok || !reference.isIndirect {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpGetUpvalue, dest, safeconv.MustIntToUint8(reference.index), uint8(program.UpvalueKindAsPointer))
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// compileAddressOfIdent handles &identifier for a local variable that is not already in a
// general register.
//
// Takes identifier (*ast.Ident) which is the identifier whose address is taken.
//
// Returns (location, true) when the address was resolved, or (_, false) to fall through.
func (c *Compiler) compileAddressOfIdent(ctx context.Context, identifier *ast.Ident) (program.VarLocation, bool) {
	location, found := c.Scopes.LookupVar(identifier.Name)
	if !found {
		return c.compileAddressOfGlobal(identifier)
	}
	if location.IsIndirect {
		return location, true
	}

	tv := c.Info.Types[identifier]
	reflectType := c.TypeToReflect(ctx, tv.Type)
	promoted, ok := c.promoteToIndirect(ctx, identifier.Name, reflectType)
	if !ok {
		return program.VarLocation{}, false
	}
	c.refreshNamedResultLocation(identifier.Name, promoted)
	return program.VarLocation{Register: promoted.Register, Kind: isa.RegisterGeneral}, true
}

// promoteToIndirect heap-promotes the named local variable into a fresh heap-allocated
// cell and updates the scope entry to point at the *T pointer so all subsequent reads and
// writes go through the indirect path automatically.
//
// Takes name (string) which is the variable to promote.
// Takes reflectType (reflect.Type) which is the static type of the variable, used to seed
// the heap cell's element type.
//
// Returns the new indirect location and true on success, or zero and false when the name
// is not found in scope.
func (c *Compiler) promoteToIndirect(ctx context.Context, name string, reflectType reflect.Type) (program.VarLocation, bool) {
	location, found := c.Scopes.LookupVar(name)
	if !found {
		return program.VarLocation{}, false
	}
	if location.IsIndirect {
		return location, true
	}

	sourceLocation := location
	if location.IsSpilled {
		sourceLocation = c.emitReloadIfSpilled(ctx, location)
	}

	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false
	}
	pointerRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	allocSitePC := len(c.Function.Body)
	program.Emit(c.Function, isa.OpAllocIndirect, pointerRegister, sourceLocation.Register, uint8(sourceLocation.Kind))
	program.EmitExtension(c.Function, typeIndex, 0)
	if c.escapeAllocSitePCs == nil {
		c.escapeAllocSitePCs = make(map[string]int)
	}
	c.escapeAllocSitePCs[name] = allocSitePC

	if location.IsSpilled {
		c.Scopes.Alloc.FreeTemp(sourceLocation.Kind, sourceLocation.Register)
	}
	promoted := program.VarLocation{
		Register:     pointerRegister,
		Kind:         isa.RegisterGeneral,
		IsIndirect:   true,
		OriginalKind: location.Kind,
	}
	c.Scopes.UpdateVar(name, promoted)
	return promoted, true
}

// compileAddressOfSelector handles &recv.Field, promoting the receiver to indirect if
// needed and taking the address of the field.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression AST node.
//
// Returns the field pointer location and any compilation error.
func (c *Compiler) compileAddressOfSelector(ctx context.Context, selectorExpression *ast.SelectorExpr) (program.VarLocation, error) {
	if recvIdent, ok := selectorExpression.X.(*ast.Ident); ok {
		if location, ok := c.tryAddressOfKnownSelector(ctx, selectorExpression, recvIdent); ok {
			return location, nil
		}
	}

	if selection := c.Info.Selections[selectorExpression]; selection != nil && selection.Kind() == types.FieldVal {
		return c.compileMethodReceiverAsPointer(ctx, selectorExpression.X, selection.Index())
	}

	receiverLocation, err := c.compileExpression(ctx, selectorExpression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, dest, receiverLocation.Register, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// tryAddressOfKnownSelector attempts to resolve &identifier.Field when the receiver
// identifier is a known local variable, promoting it to indirect storage if necessary.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression AST node.
// Takes recvIdent (*ast.Ident) which is the receiver identifier.
//
// Returns (location, true) on success, or (_, false) to fall through to the generic path.
func (c *Compiler) tryAddressOfKnownSelector(ctx context.Context, selectorExpression *ast.SelectorExpr, recvIdent *ast.Ident) (program.VarLocation, bool) {
	receiverLocation, found := c.Scopes.LookupVar(recvIdent.Name)
	if !found {
		return program.VarLocation{}, false
	}

	if !receiverLocation.IsIndirect && receiverLocation.Kind == isa.RegisterGeneral {
		receiverLocation = c.promoteReceiverToIndirect(ctx, selectorExpression.X, recvIdent.Name, receiverLocation)
	}

	if !receiverLocation.IsIndirect {
		return program.VarLocation{}, false
	}

	_, indices, _ := types.LookupFieldOrMethod(c.Info.Types[selectorExpression.X].Type, true, nil, selectorExpression.Sel.Name)
	if len(indices) != 1 {
		return program.VarLocation{}, false
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	dereferenceRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, dereferenceRegister, receiverLocation.Register, 0)
	fieldRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpGetField, fieldRegister, dereferenceRegister, safeconv.MustIntToUint8(indices[0]))
	program.Emit(c.Function, isa.OpAddr, dest, fieldRegister, engine.AddrSourceStable)
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, fieldRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, dereferenceRegister)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// promoteReceiverToIndirect upgrades a non-indirect general-register variable (typically
// the receiver of a selector expression) to indirect storage in place: the same register
// now holds the *T pointer rather than the struct value, and isa.OpAllocIndirect copies
// the value to a fresh heap cell.
//
// Used by tryAddressOfKnownSelector for &recv.Field where recv lives in a general
// register.
//
// Takes xExpr (ast.Expr) which is the expression used to resolve the type.
// Takes name (string) which is the variable name in scope.
// Takes receiverLocation (VarLocation) which is the current variable location.
//
// Returns the promoted variable location with indirect storage.
func (c *Compiler) promoteReceiverToIndirect(ctx context.Context, xExpr ast.Expr, name string, receiverLocation program.VarLocation) program.VarLocation {
	tv := c.Info.Types[xExpr]
	reflectType := c.TypeToReflect(ctx, tv.Type)
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		c.recordStickyError(err)
		return receiverLocation
	}
	program.Emit(c.Function, isa.OpAllocIndirect, receiverLocation.Register, receiverLocation.Register, uint8(isa.RegisterGeneral))
	program.EmitExtension(c.Function, typeIndex, 0)
	promoted := program.VarLocation{
		Register:     receiverLocation.Register,
		Kind:         isa.RegisterGeneral,
		IsIndirect:   true,
		OriginalKind: isa.RegisterGeneral,
	}
	c.Scopes.UpdateVar(name, promoted)
	return promoted
}

// compileAddressOfIndex compiles &collection[index], keeping the indexed element as an
// addressable reflect.Value so the resulting pointer refers to the element within the
// original backing store rather than to a copy.
//
// Takes expression (*ast.IndexExpr) which is the index expression AST node.
//
// Returns the element pointer location and any compilation error.
func (c *Compiler) compileAddressOfIndex(ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, error) {
	collectionLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}

	indexLocation, err := c.compileExpression(ctx, expression.Index)
	if err != nil {
		return program.VarLocation{}, err
	}

	if indexLocation.Kind != isa.RegisterInt {
		c.ensureIntRegister(ctx, &indexLocation)
	}

	c.boxToGeneral(ctx, &collectionLocation)

	elementRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpIndex, elementRegister, collectionLocation.Register, indexLocation.Register)

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, dest, elementRegister, engine.AddrSourceStable)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileAddressOfWithoutReading handles address-of forms that must not compile their
// operand as a value first. The general path would emit a dead read of a captured cell,
// which is a data race when other goroutines hold the same cell.
//
// Takes expression (*ast.UnaryExpr) which is the unary expression being compiled.
//
// Returns the compiled location, true when this path handled the expression, and any
// compilation error.
func (c *Compiler) compileAddressOfWithoutReading(ctx context.Context, expression *ast.UnaryExpr) (program.VarLocation, bool, error) {
	if expression.Op != token.AND {
		return program.VarLocation{}, false, nil
	}
	if indexExpression, ok := expression.X.(*ast.IndexExpr); ok {
		location, err := c.compileAddressOfIndex(ctx, indexExpression)
		return location, true, err
	}
	if identifier, ok := expression.X.(*ast.Ident); ok {
		if location, resolved := c.compileAddressOfUpvalue(identifier); resolved {
			return location, true, nil
		}
	}
	return program.VarLocation{}, false, nil
}

// compileNarrowedUnary applies a unary operator and truncates the result to the
// expression's static width.
//
// Shared by - and ^, which both produce a value that can exceed the declared integer
// width and so must be narrowed before anything reads it.
//
// Takes expression (*ast.UnaryExpr) whose static type gives the target width.
// Takes operand (VarLocation) which is the already-compiled operand.
// Takes apply (func) which emits the operator itself.
//
// Returns the narrowed result location and any compilation error.
func (c *Compiler) compileNarrowedUnary(
	ctx context.Context,
	expression *ast.UnaryExpr,
	operand program.VarLocation,
	apply func(context.Context, program.VarLocation) (program.VarLocation, error),
) (program.VarLocation, error) {
	result, err := apply(ctx, operand)
	if err != nil {
		return result, err
	}
	if staticType := c.staticTypeOf(expression); staticType != nil {
		c.EmitNarrowIntegerTruncation(result, staticType)
	}
	return result, nil
}
