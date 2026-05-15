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
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// errWrapNameAtPosition is the wrap format the generic-specialisation errors share: the
	// sentinel, the declaration's name, and the source position of the site that could not
	// be monomorphised.
	errWrapNameAtPosition = "%w: %s at %s"

	// maxFunctionIndex caps compiled functions per program (uint16 encoding, a few slots
	// reserved).
	maxFunctionIndex = 65530
)

// errSpecialisationPendingTypeParam reports that a specialisation slot could not be bound
// because the type argument is still a type parameter of the declaration currently being
// compiled. It never escapes the compile package: the caller either propagates the
// requirement outward or emits a trap.
var errSpecialisationPendingTypeParam = errors.New("specialisation type argument is still a type parameter")

// compileCallExpression compiles a function call expression into bytecode.
//
// Takes expression (*ast.CallExpr) which is the AST call expression node to compile into
// bytecode.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileCallExpression(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	savedParen := c.callParen
	c.callParen = expression.Lparen
	defer func() { c.callParen = savedParen }()
	if tv, ok := c.Info.Types[expression.Fun]; ok && tv.IsType() {
		return c.compileTypeConversion(ctx, expression)
	}

	callFun := c.unwrapGenericInstantiation(ctx, expression.Fun)

	if selectorExpression, ok := callFun.(*ast.SelectorExpr); ok {
		return c.compileSelectorCallExpression(ctx, selectorExpression, expression)
	}

	if lit, ok := callFun.(*ast.FuncLit); ok {
		return c.compileIIFE(ctx, lit, expression)
	}

	identifier, ok := callFun.(*ast.Ident)
	if !ok {
		return c.compileIndirectCall(ctx, expression)
	}

	if typeObject, ok := c.Info.Uses[identifier]; ok {
		if _, isBuiltin := typeObject.(*types.Builtin); isBuiltin {
			return c.compileBuiltinCall(ctx, identifier.Name, expression)
		}
		if _, isFunc := typeObject.(*types.Func); !isFunc {
			return c.resolveIndirectIdent(ctx, identifier, expression)
		}
	}

	functionIndex, found := c.functionTable[identifier.Name]
	if !found {
		return c.resolveIndirectIdent(ctx, identifier, expression)
	}

	return c.compileDirectCall(ctx, expression, functionIndex)
}

// unwrapGenericInstantiation strips generic instantiation wrappers (IndexExpr,
// IndexListExpr) from a call target when they represent type parameter instantiation
// rather than indexing.
//
// Takes fun (ast.Expr) which is the expression to unwrap.
//
// Returns ast.Expr with generic instantiation removed.
func (c *Compiler) unwrapGenericInstantiation(_ context.Context, fun ast.Expr) ast.Expr {
	if index, ok := fun.(*ast.IndexExpr); ok {
		unwrap := false
		switch x := index.X.(type) {
		case *ast.Ident:
			_, unwrap = c.Info.Instances[x]
		case *ast.SelectorExpr:
			_, unwrap = c.Info.Instances[x.Sel]
		}
		if unwrap {
			fun = index.X
		}
	}
	if index, ok := fun.(*ast.IndexListExpr); ok {
		fun = index.X
	}
	return fun
}

// compileIndirectCall compiles a call to a non-identifier expression (e.g. a function
// stored in a variable or returned from another call).
//
// Takes expression (*ast.CallExpr) which is the AST call expression to compile via its
// evaluated target.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileIndirectCall(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	functionLocation, err := c.compileExpression(ctx, expression.Fun)
	if err != nil {
		return program.VarLocation{}, fmt.Errorf("compiling call target %T at %s: %w", expression.Fun, c.positionString(expression.Fun.Pos()), err)
	}
	if functionLocation.Kind == isa.RegisterGeneral {
		return c.compileNativeCallFromLocation(ctx, expression, functionLocation)
	}
	return program.VarLocation{}, fmt.Errorf("unsupported call target: %T at %s", expression.Fun, c.positionString(expression.Fun.Pos()))
}

// resolveIndirectIdent resolves an identifier that is not in the functionTable - it may
// be a closure variable or a captured upvalue.
//
// Takes identifier (*ast.Ident) which is the identifier to resolve.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation of the call result and any resolution error.
func (c *Compiler) resolveIndirectIdent(ctx context.Context, identifier *ast.Ident, expression *ast.CallExpr) (program.VarLocation, error) {
	location, varFound := c.Scopes.LookupVar(identifier.Name)
	if varFound && location.Kind == isa.RegisterGeneral {
		return c.compileClosureCall(ctx, identifier, expression, location)
	}
	if reference, ok := c.upvalueMap[identifier.Name]; ok {
		dest := c.Scopes.Alloc.Alloc(reference.kind)
		program.Emit(c.Function, isa.OpGetUpvalue, dest, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
		upvalLocation := program.VarLocation{Register: dest, Kind: reference.kind}
		return c.compileClosureCall(ctx, identifier, expression, upvalLocation)
	}
	if globalVar, isGlobal := c.globalVariables[identifier.Name]; isGlobal && globalVar.Kind == isa.RegisterGeneral {
		globalLocation := c.emitGetGlobal(ctx, globalVar)
		return c.compileClosureCall(ctx, identifier, expression, globalLocation)
	}
	if symbolLocation, resolved := c.compilePackageSymbolIdent(identifier); resolved {
		return c.compileNativeCallFromLocation(ctx, expression, symbolLocation)
	}
	return program.VarLocation{}, fmt.Errorf("undefined function: %s at %s", identifier.Name, c.positionString(identifier.Pos()))
}

// compileDirectCall compiles a direct call to a compiled function.
//
// Resolves the callee via functionTable index. When the callee is a generic function and
// the type-args at this call site can be resolved from c.Info.Instances, triggers full
// body specialisation: a fresh CompiledFunction with typed-bank parameter and result
// kinds is compiled (or reused from the generic's specialisation cache) and the call
// dispatches to it directly. The boxing/unboxing dance that the type-erased path would
// otherwise Emit is skipped.
//
// Takes expression (*ast.CallExpr) which is the AST call expression node.
// Takes functionIndex (uint16) which is the index of the target function in the
// functionTable.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileDirectCall(ctx context.Context, expression *ast.CallExpr, functionIndex uint16) (program.VarLocation, error) {
	specialised, err := c.maybeSpecialiseCallee(ctx, expression, functionIndex)
	if err != nil {
		return program.VarLocation{}, err
	}
	if specialised != functionIndex {
		functionIndex = specialised
	}
	callee := c.RootFunction.Functions[functionIndex]

	argumentLocations, err := c.compileCallArguments(ctx, expression, callee)
	if err != nil {
		return program.VarLocation{}, err
	}

	returnLocations := c.allocReturnRegisters(ctx, callee.ResultKinds)
	var resultLocation program.VarLocation
	if len(returnLocations) > 0 {
		resultLocation = returnLocations[0]
	}

	site := program.CallSite{
		FunctionIndex: functionIndex,
		Arguments:     argumentLocations,
		Returns:       returnLocations,
	}
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
	c.emitCall(selectDirectCallOpcode(&site, callee), siteIndex)

	return c.unpackGenericResult(ctx, expression, resultLocation), nil
}

// maybeSpecialiseCallee returns a specialised functionIndex when the callee is generic
// and this call site's type-args can be resolved from c.Info.Instances; returns
// functionIndex unchanged otherwise.
//
// Triggered by compileDirectCall (and parallel method paths). Compiles the specialisation
// lazily on first encounter, registers the new functionIndex on the generic callee's
// specialisations map BEFORE emitting the body so recursive calls inside resolve to the
// reserved entry. Caps specialisations per generic at MaxSpecialisationsPerFunction;
// beyond that, falls back to the type-erased path.
//
// Takes expression (*ast.CallExpr) which is the call expression (used to find the
// unwrapped ident for c.Info.Instances lookup).
// Takes functionIndex (uint16) which is the original (generic) function index.
//
// Returns the specialised functionIndex (or original if no specialisation fired) and any
// compilation error.
func (c *Compiler) maybeSpecialiseCallee(
	ctx context.Context,
	expression *ast.CallExpr,
	functionIndex uint16,
) (uint16, error) {
	callee, ok := c.specialisationCandidate(functionIndex)
	if !ok {
		return functionIndex, nil
	}
	instance, ok := c.specialisationInstanceFor(ctx, expression, callee)
	if !ok {
		return functionIndex, nil
	}
	subs, key, ok := c.buildSpecialisationKey(ctx, callee, instance)
	if !ok {
		return functionIndex, c.refuseErasedGenericFallback(callee, expression)
	}
	specFunctionIndex, ok, err := c.specialiseInto(ctx, callee, subs, &key)
	if err != nil {
		return functionIndex, err
	}
	if !ok {
		return functionIndex, c.refuseErasedGenericFallback(callee, expression)
	}
	return specFunctionIndex, nil
}

// refuseErasedGenericFallback returns an error when the callee requires specialisation
// and its erased body carries a trap.
//
// Takes callee (*CompiledFunction) which is the generic function.
// Takes expression (*ast.CallExpr) which is the call site for positioning.
//
// Returns nil when the erased body is correct, or an error otherwise.
func (c *Compiler) refuseErasedGenericFallback(callee *program.CompiledFunction, expression *ast.CallExpr) error {
	if !callee.RequiresSpecialisation {
		return nil
	}
	return fmt.Errorf(errWrapNameAtPosition, fault.ErrCompileGenericCallerRequiresSpecialisation,
		callee.Name, c.positionString(expression.Pos()))
}

// emitRequiresSpecialisationTrap emits an unconditional panic ahead of a generic-method
// call whose type arguments are unsubstituted type parameters.
//
// Takes methodName (string) which is the generic method being called, named in the panic.
// Takes position (token.Pos) which is the call site, named in the panic.
func (c *Compiler) emitRequiresSpecialisationTrap(ctx context.Context, methodName string, position token.Pos) {
	message := fmt.Sprintf("pipit: erased body of a generic caller reached: %s at %s",
		methodName, c.positionString(position))
	index, err := program.AddStringConstant(c.Function, message)
	if err != nil {
		return
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterString)
	program.EmitWide(c.Function, isa.OpLoadStringConst, register, index)
	location := program.VarLocation{Register: register, Kind: isa.RegisterString}
	c.boxElementToGeneral(ctx, &location)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2),
		uint8(isa.SubOpTier2Panic), location.Register)
}

// specialiseInto reserves a function slot for a specialisation and emits its body.
//
// Shared by the call path and the value path so both observe the same caps and the same
// registration ordering. That ordering is load-bearing: the specialisation is registered
// before the body is compiled, so a recursive generic that calls itself at the same type
// arguments resolves to the slot already reserved rather than recursing forever.
//
// Takes callee (*CompiledFunction) which is the generic origin.
// Takes subs (map[*types.TypeParam]types.Type) which is the substitution map.
// Takes key (SpecialisationKey) which identifies the instantiation.
//
// Returns the specialised function index and true when one was produced or reused; false
// with no error when a cap declined it.
func (c *Compiler) specialiseInto(
	ctx context.Context,
	callee *program.CompiledFunction,
	subs map[*types.TypeParam]types.Type,
	key *program.SpecialisationKey,
) (uint16, bool, error) {
	if existing, ok := program.LookupSpecialisation(callee, key); ok {
		return existing, true, nil
	}
	if len(callee.Specialisations) >= program.MaxSpecialisationsPerFunction {
		return 0, false, nil
	}
	if len(callee.Specialisations) >= program.SpecialisationsCap(callee) {
		return 0, false, nil
	}
	root := c.RootFunction
	if len(root.Functions) >= maxFunctionIndex {
		return 0, false, nil
	}
	specFunctionIndex := safeconv.MustIntToUint16(len(root.Functions))
	specCF := &program.CompiledFunction{}
	root.Functions = append(root.Functions, specCF)
	if regErr := program.RegisterSpecialisation(callee, key, specFunctionIndex); regErr != nil {
		root.Functions = root.Functions[:len(root.Functions)-1]
		return 0, false, nil
	}
	if err := c.compileSpecialisedBody(ctx, callee, subs, specFunctionIndex); err != nil {
		return 0, false, err
	}
	return specFunctionIndex, true, nil
}

// specialiseGenericValue resolves an instantiated generic used as a value to the index of
// its monomorphic body.
//
// The call path can fall back to the erased body when specialisation declines, because
// the call site supplies concrete arguments. A value cannot: binding the erased body
// hands out a closure whose type parameters are never substituted, which produces wrong
// results rather than slow ones. So this reports failure and the caller raises it.
//
// Takes identifier (*ast.Ident) which is the instantiated name.
// Takes functionIndex (uint16) which is the generic's index.
//
// Returns the index to bind, and false when no specialisation could be produced.
func (c *Compiler) specialiseGenericValue(ctx context.Context, identifier *ast.Ident, functionIndex uint16) (uint16, bool, error) {
	callee, ok := c.specialisationCandidate(functionIndex)
	if !ok {
		return functionIndex, true, nil
	}
	instance, ok := c.Info.Instances[identifier]
	if !ok || instance.TypeArgs == nil || instance.TypeArgs.Len() != callee.GenericTypeParams.Len() {
		return 0, false, nil
	}
	subs, key, ok := c.buildSpecialisationKey(ctx, callee, instance)
	if !ok {
		return 0, false, nil
	}
	return c.specialiseInto(ctx, callee, subs, &key)
}

// maybeSpecialiseMethod resolves a method call to a monomorphic body when the method
// declares its own type parameters (Go 1.27 generic methods).
//
// Unlike a generic function, a generic method has no correct erased lowering: `var x P`
// inside the body has no type to zero and %T would print the type parameter's name rather
// than the argument's. So where maybeSpecialiseCallee falls back, this reports an error.
//
// Takes selectorExpression (*ast.SelectorExpr) which names the method.
// Takes functionIndex (uint16) which is the method's index in the function table.
//
// Returns the index to call, or an error when a generic method cannot be monomorphised.
func (c *Compiler) maybeSpecialiseMethod(ctx context.Context, selectorExpression *ast.SelectorExpr, functionIndex uint16) (uint16, error) {
	callee, ok := c.specialisationCandidate(functionIndex)
	if !ok || !callee.HasReceiver {
		return functionIndex, nil
	}

	subs, key, err := c.buildMethodSpecialisationKey(ctx, selectorExpression, callee)
	if errors.Is(err, errSpecialisationPendingTypeParam) {
		c.emitRequiresSpecialisationTrap(ctx, callee.Name, selectorExpression.Pos())
		return functionIndex, nil
	}
	if err != nil {
		return functionIndex, fmt.Errorf(errWrapNameAtPosition, err,
			callee.Name, c.positionString(selectorExpression.Pos()))
	}

	specFunctionIndex, specialised, err := c.specialiseInto(ctx, callee, subs, &key)
	if err != nil {
		return functionIndex, err
	}
	if !specialised {
		return functionIndex, fmt.Errorf(errWrapNameAtPosition, fault.ErrCompileGenericMethodSpecialisationLimit,
			callee.Name, c.positionString(selectorExpression.Pos()))
	}
	return specFunctionIndex, nil
}

// buildMethodSpecialisationKey combines the receiver's type arguments with the method's
// own.
//
// Receiver arguments occupy the leading slots and the method's follow, matching source
// order and making a receiver-only key a prefix of a generic-method key. Both lists are
// keyed on type parameters taken from the declaration, never from an instantiated
// signature: go/types freshens a signature's own type parameters during substitution, so
// the objects on an instance are clones the body does not resolve against.
//
// Takes selectorExpression (*ast.SelectorExpr) which names the method.
// Takes callee (*CompiledFunction) which is the generic method.
//
// Returns the substitution map, the key, and false when the instantiation is incomplete.
func (c *Compiler) buildMethodSpecialisationKey(
	ctx context.Context,
	selectorExpression *ast.SelectorExpr,
	callee *program.CompiledFunction,
) (map[*types.TypeParam]types.Type, program.SpecialisationKey, error) {
	var key program.SpecialisationKey

	receiverArgs, ok := c.resolveReceiverTypeArgs(selectorExpression, callee)
	if !ok {
		return nil, key, fault.ErrCompileGenericMethodNotSpecialisable
	}
	instance, ok := c.Info.Instances[selectorExpression.Sel]
	if !ok || instance.TypeArgs == nil || instance.TypeArgs.Len() != callee.GenericTypeParams.Len() {
		return nil, key, fault.ErrCompileGenericMethodNotSpecialisable
	}

	total := len(receiverArgs) + instance.TypeArgs.Len()
	if total > program.MaxSpecialisationTypeArgs {
		return nil, key, fault.ErrCompileGenericMethodTypeArgLimit
	}

	subs := make(map[*types.TypeParam]types.Type, total)
	slot := 0
	for i, receiverArg := range receiverArgs {
		if err := c.assignSpecialisationSlot(ctx, &key, slot, callee.GenericRecvTypeParams.At(i), receiverArg, subs); err != nil {
			return nil, key, err
		}
		slot++
	}
	for i := range instance.TypeArgs.Len() {
		if err := c.assignSpecialisationSlot(ctx, &key, slot, callee.GenericTypeParams.At(i), instance.TypeArgs.At(i), subs); err != nil {
			return nil, key, err
		}
		slot++
	}

	if len(subs) != total {
		return nil, key, fault.ErrCompileGenericMethodNotSpecialisable
	}
	return subs, key, nil
}

// assignSpecialisationSlot substitutes one type argument and records it in the key.
//
// Takes key (*SpecialisationKey) which receives the reflect type and disambiguated name.
// Takes slot (int) which is the key position.
// Takes typeParam (*types.TypeParam) which is the parameter being bound.
// Takes typeArg (types.Type) which is the argument to bind.
// Takes subs (map) which receives the binding.
//
// Returns false when the argument is not concrete or has no reflect representation.
func (c *Compiler) assignSpecialisationSlot(
	ctx context.Context,
	key *program.SpecialisationKey,
	slot int,
	typeParam *types.TypeParam,
	typeArg types.Type,
	subs map[*types.TypeParam]types.Type,
) error {
	substituted := typemap.CanonicalType(c.substitutedType(typeArg))
	if substituted == nil {
		return fault.ErrCompileGenericMethodNotSpecialisable
	}

	if typemap.ContainsTypeParameter(substituted) {
		return errSpecialisationPendingTypeParam
	}
	reflectType := c.TypeToReflect(ctx, substituted)
	if reflectType == nil {
		return fault.ErrCompileGenericMethodNotSpecialisable
	}
	subs[typeParam] = substituted
	key.Types[slot] = reflectType
	key.Names[slot] = c.specialisationTypeName(substituted)
	return nil
}

// resolveReceiverTypeArgs reads the type arguments the receiver expression supplies.
//
// Takes selectorExpression (*ast.SelectorExpr) whose X is the receiver.
// Takes callee (*CompiledFunction) which is the generic method.
//
// Returns the receiver's type arguments, empty when the receiver's base type is not
// generic, and false when the receiver is generic but its arguments cannot be read.
func (c *Compiler) resolveReceiverTypeArgs(selectorExpression *ast.SelectorExpr, callee *program.CompiledFunction) ([]types.Type, bool) {
	if callee.GenericRecvTypeParams == nil || callee.GenericRecvTypeParams.Len() == 0 {
		return nil, true
	}
	receiverType := c.substitutedType(c.staticTypeOf(selectorExpression.X))
	if pointer, isPointer := types.Unalias(receiverType).(*types.Pointer); isPointer {
		receiverType = pointer.Elem()
	}
	named, isNamed := types.Unalias(receiverType).(*types.Named)
	if !isNamed || named.TypeArgs() == nil || named.TypeArgs().Len() != callee.GenericRecvTypeParams.Len() {
		return nil, false
	}
	return slices.Collect(named.TypeArgs().Types()), true
}

// specialisationCandidate validates that functionIndex points at a generic callee whose
// type parameters are populated and therefore eligible for body specialisation.
//
// Takes functionIndex (uint16) which is the candidate callee's index.
//
// Returns the callee CompiledFunction and a bool indicating whether it is a
// specialisation candidate.
func (c *Compiler) specialisationCandidate(functionIndex uint16) (*program.CompiledFunction, bool) {
	if int(functionIndex) >= len(c.RootFunction.Functions) {
		return nil, false
	}
	callee := c.RootFunction.Functions[functionIndex]
	if !callee.IsGenericFunction || callee.GenericTypeParams == nil {
		return nil, false
	}
	return callee, true
}

// specialisationInstanceFor looks up the types.Instance at the call site and returns it
// when the type-args match the type-parameter arity of the callee.
//
// Takes expression (*ast.CallExpr) which is the call expression.
// Takes callee (*CompiledFunction) which is the generic callee.
//
// Returns the matched types.Instance and a bool indicating whether the instance is usable
// for specialisation.
func (c *Compiler) specialisationInstanceFor(ctx context.Context, expression *ast.CallExpr, callee *program.CompiledFunction) (types.Instance, bool) {
	identifier := unwrapInstanceIdent(c.unwrapGenericInstantiation(ctx, expression.Fun))
	if identifier == nil {
		return types.Instance{}, false
	}
	instance, ok := c.Info.Instances[identifier]
	if !ok || instance.TypeArgs == nil {
		return types.Instance{}, false
	}
	arity := instance.TypeArgs.Len()
	if arity == 0 || arity > program.MaxSpecialisationTypeArgs || arity != callee.GenericTypeParams.Len() {
		return types.Instance{}, false
	}
	return instance, true
}

// buildSpecialisationKey resolves each type-argument through the active substitution map
// and packs the resulting reflect.Type values into a SpecialisationKey suitable for
// lookup or registration. Returns false when any type-argument cannot be reduced to a
// concrete type.
//
// Takes callee (*CompiledFunction) which provides the type-parameter list ordering.
// Takes instance (types.Instance) which carries the type-args.
//
// Returns the substitution map, the cache key, and a bool indicating whether the key was
// successfully built.
func (c *Compiler) buildSpecialisationKey(ctx context.Context, callee *program.CompiledFunction, instance types.Instance) (map[*types.TypeParam]types.Type, program.SpecialisationKey, bool) {
	arity := instance.TypeArgs.Len()
	subs := make(map[*types.TypeParam]types.Type, arity)
	var key program.SpecialisationKey
	for i := range arity {
		typeArg := typemap.CanonicalType(c.substitutedType(instance.TypeArgs.At(i)))

		if typeArg == nil || typemap.ContainsTypeParameter(typeArg) {
			return nil, key, false
		}
		reflectType := c.TypeToReflect(ctx, typeArg)
		if reflectType == nil {
			return nil, key, false
		}
		subs[callee.GenericTypeParams.At(i)] = typeArg
		key.Types[i] = reflectType
		key.Names[i] = c.specialisationTypeName(typeArg)
	}
	return subs, key, true
}

// specialisationTypeName renders a type argument for SpecialisationKey.Names, appending
// the declaring position to disambiguate function-local types.
//
// Takes typeArg (types.Type) which is the concrete type argument.
//
// Returns the disambiguated type name.
func (c *Compiler) specialisationTypeName(typeArg types.Type) string {
	name := types.TypeString(typeArg, nil)
	named, ok := types.Unalias(typeArg).(*types.Named)
	if !ok {
		return name
	}
	object := named.Obj()
	if object == nil || object.Pkg() == nil || object.Parent() == object.Pkg().Scope() {
		return name
	}
	if c.FileSet == nil {
		return name
	}
	return name + "@" + c.FileSet.Position(object.Pos()).String()
}

// allocReturnRegisters allocates registers for function return values.
//
// Takes resultKinds ([]isa.RegisterKind) which are the register kinds for each return
// value.
//
// Returns []VarLocation corresponding to the allocated return registers.
func (c *Compiler) allocReturnRegisters(_ context.Context, resultKinds []isa.RegisterKind) []program.VarLocation {
	if len(resultKinds) == 0 {
		return nil
	}
	locs := make([]program.VarLocation, len(resultKinds))
	for i, kind := range resultKinds {
		register := c.Scopes.Alloc.Alloc(kind)
		locs[i] = program.VarLocation{Register: register, Kind: kind}
	}
	return locs
}

// unpackGenericResult unboxes a generic call result into a concrete scalar register when
// needed.
//
// When the location is isa.RegisterGeneral but the call expression type maps to a scalar
// kind, emits an isa.OpUnpackInterface to unbox the value.
//
// Takes expression (*ast.CallExpr) which is the call expression used to determine the
// concrete type.
// Takes location (VarLocation) which is the current VarLocation of the call result.
//
// Returns VarLocation which is the original or unboxed location.
func (c *Compiler) unpackGenericResult(_ context.Context, expression *ast.CallExpr, location program.VarLocation) program.VarLocation {
	if location.Kind != isa.RegisterGeneral {
		return location
	}
	tv, ok := c.Info.Types[expression]
	if !ok {
		return location
	}
	expressionKind := c.kindFor(tv.Type)
	if expressionKind == isa.RegisterGeneral {
		return location
	}
	scalarRegister := c.Scopes.Alloc.Alloc(expressionKind)
	program.Emit(c.Function, isa.OpUnpackInterface, scalarRegister, location.Register, uint8(expressionKind))
	resultLocation := program.VarLocation{Register: scalarRegister, Kind: expressionKind}
	c.EmitNarrowIntegerTruncation(resultLocation, tv.Type)
	return resultLocation
}

// compileSelectorCallExpression compiles a method or package-level function call via a
// selector expression.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method or function.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileSelectorCallExpression(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, error) {
	if selection, ok := c.Info.Selections[selectorExpression]; ok && selection.Kind() == types.MethodExpr {
		return c.compileMethodExprDirectCall(ctx, selectorExpression, expression, selection)
	}

	if location, ok, err := c.tryCompiledMethodCall(ctx, selectorExpression, expression); ok || err != nil {
		return location, err
	}

	if c.isInterfaceMethodCall(ctx, selectorExpression) {
		return c.compileDynamicMethodCall(ctx, selectorExpression, expression)
	}

	if location, ok, err := c.tryUnsafeBuiltinCall(ctx, selectorExpression, expression); ok || err != nil {
		return location, err
	}

	if location, ok, err := c.tryCompileIntrinsic(ctx, selectorExpression, expression); ok || err != nil {
		return location, err
	}

	if location, ok, err := c.tryCompileLinkedCall(ctx, selectorExpression, expression); ok || err != nil {
		return location, err
	}

	if location, ok, err := c.tryCompileLinkedMethodCall(ctx, selectorExpression, expression); ok || err != nil {
		return location, err
	}

	return c.compileSelectorNativeCall(ctx, selectorExpression, expression)
}

// tryCompiledMethodCall attempts to compile a call to a user-defined method found in the
// functionTable.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation, a bool indicating success, and any error.
func (c *Compiler) tryCompiledMethodCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, bool, error) {
	tableName, ok := c.resolveMethodTableName(ctx, selectorExpression)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	functionIndex, found := c.functionTable[tableName]
	if !found {
		return program.VarLocation{}, false, nil
	}
	functionIndex, err := c.maybeSpecialiseMethod(ctx, selectorExpression, functionIndex)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	var fieldPath []int
	if selection, ok := c.Info.Selections[selectorExpression]; ok {
		if index := selection.Index(); len(index) > 1 {
			fieldPath = index[:len(index)-1]
		}
	}
	location, err := c.compileMethodCall(ctx, selectorExpression, expression, functionIndex, fieldPath)
	return location, true, err
}

// tryUnsafeBuiltinCall checks if the selector targets an unsafe package builtin and
// compiles it.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression to check.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation, a bool indicating a match was found, and any error.
func (c *Compiler) tryUnsafeBuiltinCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, bool, error) {
	typeObject, ok := c.Info.Uses[selectorExpression.Sel]
	if !ok {
		return program.VarLocation{}, false, nil
	}
	if _, isBuiltin := typeObject.(*types.Builtin); !isBuiltin {
		return program.VarLocation{}, false, nil
	}
	if typeObject.Pkg() == nil || typeObject.Pkg().Path() != policy.PkgUnsafe {
		return program.VarLocation{}, false, nil
	}
	location, err := c.compileUnsafeBuiltinCall(ctx, selectorExpression.Sel.Name, expression)
	return location, true, err
}

// compileSelectorNativeCall falls back to compiling a selector call as a native function
// invocation.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression for the
// native function.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation holding the native call result and any compilation error.
func (c *Compiler) compileSelectorNativeCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, error) {
	selector, err := c.compileSelectorExpressionDetailed(ctx, selectorExpression)
	if err != nil {
		return program.VarLocation{}, err
	}
	if selector.hasMethodValueHint {
		return c.compileNativeCallFromLocation(ctx, expression, selector.location, selector.methodValueHint)
	}
	return c.compileNativeCallFromLocation(ctx, expression, selector.location)
}

// resolveArgumentStaticTypes computes per-argument static-type metadata for a native
// call.
//
// Both slices are indexed alongside the call's argument list. Used to populate CallSite's
// argumentStaticTypeNames and argumentStaticTypeStrings - see those fields for the
// consumers (interface-adapter selection and the %T fmt interceptor). names[i] is the
// source-level named type for argument i ("Colour", "Bomb"), unwrapping one pointer
// layer; empty when not named. typeStrings[i] is the Go-syntax type representation
// ("int", "[]int", "*main.Bomb"); empty when no static type info is recorded.
//
// Takes expression (*ast.CallExpr) which carries the per-argument AST nodes used to look
// up static types in c.Info.Types.
//
// Returns names, the bare named-type identifiers per argument.
// Returns typeStrings, the qualified Go-syntax type renderings per argument.
func (c *Compiler) resolveArgumentStaticTypes(expression *ast.CallExpr) (names, typeStrings []string) {
	names = make([]string, len(expression.Args))
	typeStrings = make([]string, len(expression.Args))
	if c.Info == nil {
		return names, typeStrings
	}
	for i, argument := range expression.Args {
		staticType, ok := c.Info.Types[argument]
		if !ok || staticType.Type == nil {
			continue
		}

		argumentType := typemap.CanonicalType(c.substitutedType(staticType.Type))
		if isInterfaceType(argumentType) {
			continue
		}
		typeStrings[i] = typemap.RuntimeTypeString(argumentType)
		names[i] = bareNamedTypeName(argumentType)
	}
	return names, typeStrings
}

// isInterfaceMethodCall returns true when selectorExpression resolves to a method call
// whose receiver type is an interface (so dispatch must go through the runtime method
// table rather than a direct call).
//
// Takes selectorExpression (*ast.SelectorExpr) the selector to inspect.
//
// Returns bool which is true if the receiver type is an interface.
func (c *Compiler) isInterfaceMethodCall(_ context.Context, selectorExpression *ast.SelectorExpr) bool {
	selection, ok := c.Info.Selections[selectorExpression]
	if !ok || selection.Kind() != types.MethodVal {
		return false
	}
	if methodFunction, ok := selection.Obj().(*types.Func); ok {
		if signature, ok := methodFunction.Type().(*types.Signature); ok && signature.Recv() != nil {
			definitionType := signature.Recv().Type()
			if pointer, ok := definitionType.(*types.Pointer); ok {
				definitionType = pointer.Elem()
			}
			if _, isInterface := definitionType.Underlying().(*types.Interface); isInterface {
				return true
			}
		}
	}
	receiverType := selection.Recv()
	if pointer, ok := receiverType.(*types.Pointer); ok {
		receiverType = pointer.Elem()
	}
	_, isInterface := receiverType.Underlying().(*types.Interface)
	return isInterface
}

// compileDynamicMethodCall compiles a method call on an interface receiver using runtime
// dispatch via the method table.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
//
// Returns VarLocation holding the dispatch result and any compilation error.
func (c *Compiler) compileDynamicMethodCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr) (program.VarLocation, error) {
	receiverLocation, err := c.compileExpression(ctx, selectorExpression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	argumentLocations, err := c.compileDynamicMethodArgs(ctx, receiverLocation, expression.Args)
	if err != nil {
		return program.VarLocation{}, err
	}

	signature, err := c.resolveDynamicMethodSignature(expression.Fun)
	if err != nil {
		return program.VarLocation{}, err
	}

	returnLocations, resultLocation := c.allocDynamicMethodReturns(signature)

	methodIndex, err := program.AddStringConstant(c.Function, selectorExpression.Sel.Name)
	if err != nil {
		return program.VarLocation{}, err
	}

	staticTypeNames, staticTypeStrings := c.resolveMethodCallStaticTypes(selectorExpression, expression)
	site := program.CallSite{
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		ArgumentStaticTypeNames:   staticTypeNames,
		ArgumentStaticTypeStrings: staticTypeStrings,
		IsEllipsisSpread:          expression.Ellipsis.IsValid(),
	}
	c.configureDynamicMethodVariadic(ctx, &site, signature, expression)
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallMethod, siteIndex)
	program.EmitExtension(c.Function, methodIndex, 0)

	return resultLocation, nil
}

// compileDynamicMethodArgs compiles the receiver and call arguments into a single
// argumentLocations slice that callMethod's site layout expects.
//
// Takes receiverLocation (VarLocation) which is the boxed receiver.
// Takes args ([]ast.Expr) which are the call's positional arguments.
//
// Returns the assembled []VarLocation slice and any compilation error.
func (c *Compiler) compileDynamicMethodArgs(ctx context.Context, receiverLocation program.VarLocation, args []ast.Expr) ([]program.VarLocation, error) {
	argumentLocations := make([]program.VarLocation, 0, 1+len(args))
	argumentLocations = append(argumentLocations, receiverLocation)
	for _, argument := range args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		if typemodel.IsNamedScalarPoolType(location.SourceType) {
			c.boxToGeneralTemp(ctx, &location)
		}
		argumentLocations = append(argumentLocations, location)
	}
	return argumentLocations, nil
}

// resolveDynamicMethodSignature extracts the *types.Signature for a method-call selector
// expression.
//
// Takes functionExpression (ast.Expr) which is the call's Fun field (the SelectorExpr).
//
// Returns the resolved signature and any compilation error.
func (c *Compiler) resolveDynamicMethodSignature(functionExpression ast.Expr) (*types.Signature, error) {
	typeAndValue, ok := c.Info.Types[functionExpression]
	if !ok || typeAndValue.Type == nil {
		return nil, fmt.Errorf("%w: missing type information for method call at %s", fault.ErrCompilation, c.positionString(functionExpression.Pos()))
	}
	signature, ok := typeAndValue.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("expected *types.Signature, got %T", typeAndValue.Type.Underlying())
	}
	return signature, nil
}

// allocDynamicMethodReturns allocates one register per result slot in signature and
// packages them as varLocations.
//
// Takes signature (*types.Signature) which describes the method's return slots.
//
// Returns the slice of return locations and the first one (or zero when no returns) so
// the caller can name the call's result.
func (c *Compiler) allocDynamicMethodReturns(signature *types.Signature) ([]program.VarLocation, program.VarLocation) {
	var returnLocations []program.VarLocation
	for r := range signature.Results().Variables() {
		kind := c.kindFor(r.Type())
		register := c.Scopes.Alloc.Alloc(kind)
		returnLocations = append(returnLocations, program.VarLocation{Register: register, Kind: kind})
	}
	if len(returnLocations) == 0 {
		return returnLocations, program.VarLocation{}
	}
	return returnLocations, returnLocations[0]
}

// configureDynamicMethodVariadic fills in the variadic-packing fields on site when the
// method is variadic and the call does not use spread syntax.
//
// Takes site (*CallSite) which is the call site being assembled.
// Takes signature (*types.Signature) which describes the method.
// Takes expression (*ast.CallExpr) which is the call expression.
func (c *Compiler) configureDynamicMethodVariadic(ctx context.Context, site *program.CallSite, signature *types.Signature, expression *ast.CallExpr) {
	if !signature.Variadic() || expression.Ellipsis.IsValid() {
		return
	}
	lastParameter := signature.Params().At(signature.Params().Len() - 1)
	site.RuntimeVariadicSliceType = c.TypeToReflect(ctx, lastParameter.Type())
	site.RuntimeVariadicNumFixed = safeconv.MustIntToUint8(1 + signature.Params().Len() - 1)
}

// resolveMethodCallStaticTypes builds per-argument static-type metadata so the runtime
// can recover source-level type names even when typed-bank storage has collapsed them.
//
// Takes selectorExpression (*ast.SelectorExpr) which carries the receiver in `.X`.
// Takes callExpression (*ast.CallExpr) which carries the method arguments.
//
// Returns names ([]string) which are per-argument named-type identifiers, receiver in
// slot 0.
// Returns typeStrings ([]string) which are per-argument Go-syntax type strings, receiver
// in slot 0.
func (c *Compiler) resolveMethodCallStaticTypes(selectorExpression *ast.SelectorExpr, callExpression *ast.CallExpr) (names, typeStrings []string) {
	argumentNames, argumentTypeStrings := c.resolveArgumentStaticTypes(callExpression)
	receiverName, receiverTypeString := c.resolveReceiverStaticType(selectorExpression.X)
	names = make([]string, 0, 1+len(argumentNames))
	typeStrings = make([]string, 0, 1+len(argumentTypeStrings))
	names = append(names, receiverName)
	names = append(names, argumentNames...)
	typeStrings = append(typeStrings, receiverTypeString)
	typeStrings = append(typeStrings, argumentTypeStrings...)
	return names, typeStrings
}

// resolveReceiverStaticType returns the source-level named type and Go-syntax type string
// of a method receiver expression, substituting generic type parameters via
// `c.substitutedType` so receivers typed as `T` inside a generic body resolve to the
// concrete instantiation at the specialised call site.
//
// Takes receiverExpression (ast.Expr) which is the receiver subtree (the `X` of a
// `*ast.SelectorExpr`).
//
// Returns name (string) - bare named-type identifier ("Tag"), empty when no named type is
// involved (e.g. interface receiver).
// Returns typeString (string) - Go-syntax type rendering for downstream consumers like
// `argumentStaticTypeStrings`.
func (c *Compiler) resolveReceiverStaticType(receiverExpression ast.Expr) (name, typeString string) {
	if c.Info == nil {
		return "", ""
	}
	staticType := c.staticTypeOf(receiverExpression)
	if staticType == nil {
		return "", ""
	}
	substituted := typemap.CanonicalType(c.substitutedType(staticType))
	if substituted == nil {
		return "", ""
	}
	underlying := substituted
	if pointer, ok := underlying.(*types.Pointer); ok {
		underlying = pointer.Elem()
	}
	if _, isInterface := underlying.Underlying().(*types.Interface); isInterface {
		return "", ""
	}
	return bareNamedTypeName(substituted), typemap.RuntimeTypeString(substituted)
}

// resolveMethodTableName returns the functionTable key for a selector call if the
// selector refers to a method defined in interpreted source code.
//
// When the method is promoted via struct embedding, this returns the defining type's name
// rather than the receiver type's name.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression to
// resolve.
//
// Returns the functionTable key string and true if found, or empty string and false
// otherwise.
func (c *Compiler) resolveMethodTableName(_ context.Context, selectorExpression *ast.SelectorExpr) (string, bool) {
	selection, ok := c.Info.Selections[selectorExpression]
	if !ok || (selection.Kind() != types.MethodVal && selection.Kind() != types.MethodExpr) {
		return "", false
	}

	typeFunction, ok := selection.Obj().(*types.Func)
	if !ok {
		return "", false
	}
	signature, ok := typeFunction.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return "", false
	}
	defType := signature.Recv().Type()
	if pointer, ok := defType.(*types.Pointer); ok {
		defType = pointer.Elem()
	}
	named, ok := defType.(*types.Named)
	if !ok {
		return "", false
	}
	if named.Obj().Pkg() == nil {
		return "", false
	}
	return named.Obj().Name() + "." + selectorExpression.Sel.Name, true
}

// compileMethodCall compiles a call to a user-defined method, passing the receiver as the
// first argument.
//
// Takes selectorExpression (*ast.SelectorExpr) which is the selector expression
// identifying the method.
// Takes expression (*ast.CallExpr) which is the enclosing call expression.
// Takes functionIndex (uint16) which is the index of the target method in the
// functionTable.
// Takes fieldPath ([]int) which contains embedding field indices for promoted methods, or
// nil for direct methods.
//
// Returns VarLocation holding the method call result and any compilation error.
func (c *Compiler) compileMethodCall(ctx context.Context, selectorExpression *ast.SelectorExpr, expression *ast.CallExpr, functionIndex uint16, fieldPath []int) (program.VarLocation, error) {
	callee := c.RootFunction.Functions[functionIndex]

	receiverLocation, err := c.compileMethodReceiverWithPath(ctx, selectorExpression.X, fieldPath, callee)
	if err != nil {
		return program.VarLocation{}, err
	}

	nonReceiverArguments, err := c.compileCallArguments(ctx, expression, callee)
	if err != nil {
		return program.VarLocation{}, err
	}
	argumentLocations := make([]program.VarLocation, 0, 1+len(nonReceiverArguments))
	argumentLocations = append(argumentLocations, receiverLocation)
	argumentLocations = append(argumentLocations, nonReceiverArguments...)

	returnLocations := c.allocReturnRegisters(ctx, callee.ResultKinds)
	var resultLocation program.VarLocation
	if len(returnLocations) > 0 {
		resultLocation = returnLocations[0]
	}

	staticTypeNames, staticTypeStrings := c.resolveMethodCallStaticTypes(selectorExpression, expression)
	site := program.CallSite{
		FunctionIndex:             functionIndex,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		ArgumentStaticTypeNames:   staticTypeNames,
		ArgumentStaticTypeStrings: staticTypeStrings,
	}
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

// bindFunctionValue emits the closure binding for a top-level function used as a value,
// specialising the generic body when the type checker has inferred an instantiation.
//
// Takes identifier (*ast.Ident) which names the function being bound.
// Takes functionIndex (uint16) which is the function table entry.
//
// Returns VarLocation which is the bound closure in the general bank.
// Returns error when an instantiation could not be monomorphised.
func (c *Compiler) bindFunctionValue(ctx context.Context, identifier *ast.Ident, functionIndex uint16) (program.VarLocation, error) {
	bound := functionIndex
	if callee, ok := c.specialisationCandidate(functionIndex); ok && c.hasCompleteInstance(identifier, callee) {
		specIndex, specialised, err := c.specialiseGenericValue(ctx, identifier, functionIndex)
		if err != nil {
			return program.VarLocation{}, err
		}
		if !specialised {
			return program.VarLocation{}, fmt.Errorf(errWrapNameAtPosition,
				fault.ErrCompileGenericValueNotSpecialisable, identifier.Name,
				c.positionString(identifier.Pos()))
		}
		bound = specIndex
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpMakeClosure, dest, bound)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// hasCompleteInstance reports whether the type checker recorded a full set of type
// arguments for identifier against callee's declared type parameters.
//
// An incomplete or absent entry means the identifier sits inside a generic body that has
// not been monomorphised itself, where binding the erased function is the correct thing
// to do.
//
// Takes identifier (*ast.Ident) which names the generic function.
// Takes callee (*program.CompiledFunction) which is the generic declaration.
//
// Returns true when every declared type parameter has a recorded type argument.
func (c *Compiler) hasCompleteInstance(identifier *ast.Ident, callee *program.CompiledFunction) bool {
	if c.Info == nil {
		return false
	}
	instance, ok := c.Info.Instances[identifier]
	return ok && instance.TypeArgs != nil && instance.TypeArgs.Len() == callee.GenericTypeParams.Len()
}

// CalleeUsesScalarBanksOnly reports whether every parameter and result of callee maps to
// a scalar typed register kind (int, uint, float, bool, string, complex), which gates the
// lean SubOpCallScalar dispatcher.
//
// Takes callee (*CompiledFunction) which carries the compiled parameter and result kinds.
//
// Returns true when every entry of parameterKinds and resultKinds is a scalar typed kind.
func CalleeUsesScalarBanksOnly(callee *program.CompiledFunction) bool {
	for _, kind := range callee.ParameterKinds {
		if kind == isa.RegisterGeneral || isa.IsTypedSliceKind(kind) {
			return false
		}
	}
	for _, kind := range callee.ResultKinds {
		if kind == isa.RegisterGeneral || isa.IsTypedSliceKind(kind) {
			return false
		}
	}
	return true
}

// selectDirectCallOpcode picks between SubOpCall and SubOpCallScalar for a direct call.
//
// Takes site (*CallSite) which is the populated call site.
// Takes callee (*CompiledFunction) which is the resolved callee.
//
// Returns the sub-opcode to emit.
func selectDirectCallOpcode(site *program.CallSite, callee *program.CompiledFunction) isa.SubOpcode {
	if callee == nil || callee.IsVariadic || len(site.LinkedTypeArgs) > 0 {
		return isa.SubOpCall
	}
	if !CalleeUsesScalarBanksOnly(callee) {
		return isa.SubOpCall
	}
	return isa.SubOpCallScalar
}

// unwrapInstanceIdent returns the *ast.Ident from a function expression for
// c.Info.Instances lookup. Handles bare identifiers, selector expressions (returns the
// selector's Sel), and returns nil for unsupported shapes.
//
// Takes fun (ast.Expr) which is the unwrapped (post unwrapGenericInstantiation) function
// expression.
//
// Returns the *ast.Ident or nil.
func unwrapInstanceIdent(fun ast.Expr) *ast.Ident {
	switch x := fun.(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	}
	return nil
}

// bareNamedTypeName returns the source-level identifier of a named type, unwrapping a
// single pointer layer (so `*Bomb` resolves to "Bomb").
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns the bare name, or "" when t is not a named type or has no source object.
func bareNamedTypeName(t types.Type) string {
	if pointer, ok := types.Unalias(t).(*types.Pointer); ok {
		t = pointer.Elem()
	}

	named, ok := types.Unalias(t).(*types.Named)
	if !ok || named.Obj() == nil {
		return ""
	}
	return named.Obj().Name()
}
