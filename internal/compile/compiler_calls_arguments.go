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
	"go/types"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// compileCallArguments compiles function call arguments, handling variadic packing when
// the callee is variadic and the call does not use spread.
//
// Takes expression (*ast.CallExpr) which is the AST call expression containing the
// arguments.
// Takes callee (*CompiledFunction) which is the compiled function being called.
//
// Returns []VarLocation of the compiled arguments and any compilation error.
func (c *Compiler) compileCallArguments(ctx context.Context, expression *ast.CallExpr, callee *program.CompiledFunction) ([]program.VarLocation, error) {
	if !callee.IsVariadic || expression.Ellipsis.IsValid() {
		return c.compileFixedCallArguments(ctx, expression, callee)
	}

	signature, err := c.signatureForCall(expression)
	if err != nil {
		return nil, err
	}
	return c.compileVariadicPackedArgs(ctx, expression, signature)
}

// compileFixedCallArguments compiles the arguments of a call whose callee is
// non-variadic, or whose call site uses an ellipsis spread, producing one compiled
// location per source argument.
//
// Takes expression (*ast.CallExpr) which is the call expression whose arguments are
// compiled.
// Takes callee (*CompiledFunction) which is the compiled function being called, used to
// consult expected parameter kinds.
//
// Returns the compiled argument locations and any compilation error.
func (c *Compiler) compileFixedCallArguments(ctx context.Context, expression *ast.CallExpr, callee *program.CompiledFunction) ([]program.VarLocation, error) {
	if locations, ok, err := c.tryCompileFixedFromMultiReturn(ctx, expression, callee); ok {
		return locations, err
	}
	signature, sigErr := c.signatureForCall(expression)
	flows := c.lookupSliceFlow(expression).Arguments
	argumentLocations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		if i < len(flows) && flows[i] {
			location, placed, err := c.compileHeapPlacedSliceAllocation(ctx, argument)
			if err != nil {
				return nil, err
			}
			if placed {
				argumentLocations[i] = c.boxFixedArgumentForGeneralParam(callee, i, location)
				continue
			}
		}
		mark := c.Scopes.Alloc.Snapshot()
		location, err := c.compileFixedCallArgument(ctx, callee, signature, sigErr, i, argument)
		if err != nil {
			return nil, err
		}
		argumentLocations[i] = c.compactArgument(ctx, mark, location, c.staticTypeOf(argument))
	}
	return argumentLocations, nil
}

// tryCompileFixedFromMultiReturn handles `sum(pair())` and `receiver.method(pair())`
// where the only source argument is a multi-return call whose values spread across a
// non-variadic callee's fixed parameters. Returns (locations, true, nil) when the spread
// path applied; (nil, false, nil) when the call is not a single-multi-return-argument
// spread and the ordinary per-argument path should run.
//
// Takes expression (*ast.CallExpr) which is the outer call whose sole argument is the
// multi-return inner call.
// Takes callee (*CompiledFunction) whose variadic flag gates the spread off for variadic
// callees.
//
// Returns the per-result argument locations, a boolean flag indicating whether the spread
// path fired, and any compilation error from the inner call.
func (c *Compiler) tryCompileFixedFromMultiReturn(ctx context.Context, expression *ast.CallExpr, callee *program.CompiledFunction) ([]program.VarLocation, bool, error) {
	if len(expression.Args) != 1 || expression.Ellipsis.IsValid() || callee.IsVariadic || c.Info == nil {
		return nil, false, nil
	}
	innerCall, ok := expression.Args[0].(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	innerType, hasType := c.Info.Types[innerCall.Fun]
	if !hasType {
		return nil, false, nil
	}
	innerSig, ok := innerType.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, false, nil
	}
	resultCount := innerSig.Results().Len()
	signature, sigErr := c.signatureForCall(expression)
	if resultCount < 2 || sigErr != nil || signature == nil || signature.Variadic() || signature.Params().Len() != resultCount {
		return nil, false, nil
	}
	returnLocations := make([]program.VarLocation, resultCount)
	for i := range resultCount {
		resultType := innerSig.Results().At(i).Type()
		kind := c.kindFor(resultType)
		returnLocations[i] = program.VarLocation{
			Register:   c.Scopes.Alloc.Alloc(kind),
			Kind:       kind,
			SourceType: c.TypeToReflect(ctx, resultType),
		}
	}
	if err := c.emitMultiReturnCall(ctx, innerCall, returnLocations); err != nil {
		return nil, true, err
	}

	argumentLocations := make([]program.VarLocation, resultCount)
	for i := range returnLocations {
		argumentLocations[i] = c.boxFixedArgumentForGeneralParam(callee, i, returnLocations[i])
	}
	return argumentLocations, true, nil
}

// compileFixedCallArgument compiles a single fixed-position call argument, applying
// typed-nil handling, eval-bool coercion, and typed boxing when the destination parameter
// expects a general register.
//
// Takes callee (*CompiledFunction) which is the compiled callee.
// Takes signature (*types.Signature) which is the resolved callee signature, or nil when
// it could not be resolved.
// Takes sigErr (error) which records any signature resolution failure.
// Takes index (int) which is the zero-based argument position.
// Takes argument (ast.Expr) which is the argument expression.
//
// Returns the compiled argument location and any compilation error.
func (c *Compiler) compileFixedCallArgument(
	ctx context.Context,
	callee *program.CompiledFunction,
	signature *types.Signature,
	sigErr error,
	index int,
	argument ast.Expr,
) (program.VarLocation, error) {
	var expectedType types.Type
	if sigErr == nil && signature != nil && signature.Params() != nil && index < signature.Params().Len() {
		expectedType = signature.Params().At(index).Type()
	}
	location, handled, herr := c.compileTypedNilOrExpression(ctx, argument, expectedType)
	if herr != nil {
		return program.VarLocation{}, herr
	}
	if !handled {
		var err error
		location, err = c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	location = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
	c.detachIndexedRead(argument, location)
	return c.boxFixedArgumentForGeneralParam(callee, index, location), nil
}

// boxFixedArgumentForGeneralParam boxes a fixed-position argument into a general register
// when the destination parameter expects a general register but the argument was compiled
// into a typed register.
//
// Takes callee (*CompiledFunction) whose parameterKinds describe the expected register
// banks.
// Takes index (int) which is the zero-based argument position.
// Takes location (VarLocation) which is the compiled argument location.
//
// Returns the possibly-boxed argument location.
func (c *Compiler) boxFixedArgumentForGeneralParam(callee *program.CompiledFunction, index int, location program.VarLocation) program.VarLocation {
	slot := index
	if callee.HasReceiver {
		slot++
	}
	if slot >= len(callee.ParameterKinds) || callee.ParameterKinds[slot] != isa.RegisterGeneral {
		return location
	}
	if location.Kind == isa.RegisterGeneral || location.SourceType == nil {
		return location
	}
	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	if c.emitTypedBox(generalRegister, location) {
		return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
	}
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
	return location
}

// signatureForCall resolves the callee's *types.Signature for a call expression. Works
// for both *ast.Ident (direct calls) and *ast.SelectorExpr (cross-package or method
// calls).
//
// Takes expression (*ast.CallExpr) which is the call expression whose callee signature is
// needed.
//
// Returns the resolved *types.Signature and an error if the callee's type cannot be
// interpreted as a function signature.
func (c *Compiler) signatureForCall(expression *ast.CallExpr) (*types.Signature, error) {
	tv, ok := c.Info.Types[expression.Fun]
	if !ok {
		return nil, fmt.Errorf("missing type info for call expression at %s", c.positionString(expression.Pos()))
	}
	signature, ok := tv.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("expected *types.Signature, got %T", tv.Type.Underlying())
	}
	return signature, nil
}

// compileVariadicPackedArgs compiles arguments and packs trailing values into a slice.
// The call must be variadic without an ellipsis spread.
//
// Takes expression (*ast.CallExpr) which is the call expression whose arguments need
// packing.
// Takes signature (*types.Signature) which is the callee's signature.
//
// Returns []VarLocation aligned 1:1 with the signature's parameters, and any compilation
// error from sub-expressions.
func (c *Compiler) compileVariadicPackedArgs(ctx context.Context, expression *ast.CallExpr, signature *types.Signature) ([]program.VarLocation, error) {
	fixedCount := signature.Params().Len() - 1
	if locations, ok, err := c.tryCompileVariadicFromMultiReturn(ctx, expression, signature); ok {
		return locations, err
	}
	argumentLocations := make([]program.VarLocation, signature.Params().Len())

	for i := 0; i < fixedCount && i < len(expression.Args); i++ {
		mark := c.Scopes.Alloc.Snapshot()
		location, err := c.compileExpression(ctx, expression.Args[i])
		if err != nil {
			return nil, err
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, expression.Args[i], location)
		argumentLocations[i] = c.compactArgument(ctx, mark, location, c.staticTypeOf(expression.Args[i]))
	}

	lastParameter := signature.Params().At(signature.Params().Len() - 1)
	sliceType := c.TypeToReflect(ctx, lastParameter.Type())
	typeIndex, err := program.AddTypeRef(c.Function, sliceType)
	if err != nil {
		return nil, err
	}

	variadicCount := max(len(expression.Args)-fixedCount, 0)
	lengthRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	lengthIndex, err := program.AddIntConstant(c.Function, int64(variadicCount))
	if err != nil {
		return nil, err
	}
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, lengthRegister, lengthIndex)

	sliceDestination := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpMakeSlice, sliceDestination, lengthRegister, lengthRegister)
	program.EmitExtension(c.Function, typeIndex, 0)

	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, lengthRegister)

	for i := fixedCount; i < len(expression.Args); i++ {
		watermark := c.Scopes.Alloc.Snapshot()
		location, exprErr := c.compileExpression(ctx, expression.Args[i])
		if exprErr != nil {
			return nil, exprErr
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, expression.Args[i], location)
		c.boxToGeneralTemp(ctx, &location)
		indexRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
		idxConst, idxErr := program.AddIntConstant(c.Function, int64(i-fixedCount))
		if idxErr != nil {
			return nil, idxErr
		}
		program.EmitWide(c.Function, isa.OpLoadInt64Constant, indexRegister, idxConst)
		program.Emit(c.Function, isa.OpSetElement, sliceDestination, indexRegister, location.Register)
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, indexRegister)
		c.Scopes.RestoreWatermark(watermark)
	}

	argumentLocations[fixedCount] = program.VarLocation{Register: sliceDestination, Kind: isa.RegisterGeneral}
	return argumentLocations, nil
}

// tryCompileVariadicFromMultiReturn handles `formatAll(three())` where the only argument
// is a multi-return call whose values must spread across the callee's parameters. Returns
// (locations, true, nil) when the spread path applied; (nil, false, nil) when the call
// site does not match the pattern; (nil, true, err) on Emit failure.
//
// Takes expression (*ast.CallExpr) which is the outer call carrying the variadic
// parameter.
// Takes signature (*types.Signature) which is the callee's signature.
//
// Returns the assembled argument locations (one entry per parameter, trailing slice
// last), a boolean flag indicating whether the spread path fired, and any compilation
// error from the inner call.
func (c *Compiler) tryCompileVariadicFromMultiReturn(ctx context.Context, expression *ast.CallExpr, signature *types.Signature) ([]program.VarLocation, bool, error) {
	if len(expression.Args) != 1 {
		return nil, false, nil
	}
	innerCall, ok := expression.Args[0].(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	if c.Info == nil {
		return nil, false, nil
	}
	innerType, hasType := c.Info.Types[innerCall.Fun]
	if !hasType {
		return nil, false, nil
	}
	innerSig, ok := innerType.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, false, nil
	}
	resultCount := innerSig.Results().Len()
	if resultCount < 2 {
		return nil, false, nil
	}
	parameterCount := signature.Params().Len()
	fixedCount := parameterCount - 1
	if fixedCount != 0 {
		return nil, false, nil
	}
	resultKinds := make([]isa.RegisterKind, resultCount)
	for i := range resultCount {
		resultKinds[i] = c.kindFor(innerSig.Results().At(i).Type())
	}
	returnLocations := make([]program.VarLocation, resultCount)
	for i, kind := range resultKinds {
		register := c.Scopes.Alloc.Alloc(kind)
		returnLocations[i] = program.VarLocation{Register: register, Kind: kind}
	}
	if err := c.emitMultiReturnCall(ctx, innerCall, returnLocations); err != nil {
		return nil, true, err
	}
	lastParameter := signature.Params().At(parameterCount - 1)
	sliceDestination, err := c.packLocationsIntoVariadicSlice(ctx, lastParameter.Type(), returnLocations)
	if err != nil {
		return nil, true, err
	}
	argumentLocations := make([]program.VarLocation, parameterCount)
	argumentLocations[parameterCount-1] = program.VarLocation{Register: sliceDestination, Kind: isa.RegisterGeneral}
	return argumentLocations, true, nil
}

// packLocationsIntoVariadicSlice builds a variadic slice register holding the given value
// locations, boxing each entry to a general register and storing it at its index.
//
// Takes sliceTypeExpr (types.Type) which is the variadic parameter's slice type, used to
// resolve the runtime slice type.
// Takes locations ([]VarLocation) which are the value locations to store into the slice
// in order.
//
// Returns the register holding the populated slice and any compilation error from
// constant or type registration.
func (c *Compiler) packLocationsIntoVariadicSlice(ctx context.Context, sliceTypeExpr types.Type, locations []program.VarLocation) (uint8, error) {
	sliceType := c.TypeToReflect(ctx, sliceTypeExpr)
	typeIndex, err := program.AddTypeRef(c.Function, sliceType)
	if err != nil {
		return 0, err
	}
	lengthRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	lengthIndex, err := program.AddIntConstant(c.Function, int64(len(locations)))
	if err != nil {
		return 0, err
	}
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, lengthRegister, lengthIndex)
	sliceDestination := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpMakeSlice, sliceDestination, lengthRegister, lengthRegister)
	program.EmitExtension(c.Function, typeIndex, 0)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, lengthRegister)
	for i, location := range locations {
		watermark := c.Scopes.Alloc.Snapshot()
		valueLocation := location
		c.boxToGeneralTemp(ctx, &valueLocation)
		indexRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
		idxConst, idxErr := program.AddIntConstant(c.Function, int64(i))
		if idxErr != nil {
			return 0, idxErr
		}
		program.EmitWide(c.Function, isa.OpLoadInt64Constant, indexRegister, idxConst)
		program.Emit(c.Function, isa.OpSetElement, sliceDestination, indexRegister, valueLocation.Register)
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, indexRegister)
		c.Scopes.RestoreWatermark(watermark)
	}
	return sliceDestination, nil
}

// tryCompileNativeCallFromMultiReturn handles native calls whose single argument is a
// multi-return call, every result of which spreads across the callee's parameters (Go's
// f(g()) form).
//
// Takes expression (*ast.CallExpr) which is the outer native call.
//
// Returns the spread argument locations (one per result), a boolean flag indicating
// whether the spread path fired, and any compilation error from the inner call.
func (c *Compiler) tryCompileNativeCallFromMultiReturn(ctx context.Context, expression *ast.CallExpr) ([]program.VarLocation, bool, error) {
	if len(expression.Args) != 1 || expression.Ellipsis.IsValid() {
		return nil, false, nil
	}
	if c.Info == nil {
		return nil, false, nil
	}
	innerCall, ok := expression.Args[0].(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	innerType, hasType := c.Info.Types[innerCall.Fun]
	if !hasType {
		return nil, false, nil
	}
	innerSig, ok := innerType.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, false, nil
	}
	resultCount := innerSig.Results().Len()
	if resultCount < 2 {
		return nil, false, nil
	}
	outerType, hasOuter := c.Info.Types[expression.Fun]
	if !hasOuter {
		return nil, false, nil
	}
	outerSig, ok := outerType.Type.Underlying().(*types.Signature)
	if !ok {
		return nil, false, nil
	}
	if !multiReturnFillsParameters(outerSig, resultCount) {
		return nil, false, nil
	}
	resultKinds := make([]isa.RegisterKind, resultCount)
	for i := range resultCount {
		resultKinds[i] = c.kindFor(innerSig.Results().At(i).Type())
	}
	returnLocations := make([]program.VarLocation, resultCount)
	for i, kind := range resultKinds {
		register := c.Scopes.Alloc.Alloc(kind)
		returnLocations[i] = program.VarLocation{Register: register, Kind: kind}
	}
	if err := c.emitMultiReturnCall(ctx, innerCall, returnLocations); err != nil {
		return nil, true, err
	}
	return returnLocations, true, nil
}

// multiReturnFillsParameters reports whether resultCount values, the results of one call,
// can be a native callee's whole argument list.
//
// Takes signature (*types.Signature) which is the callee's signature.
// Takes resultCount (int) which is the number of values.
//
// Returns bool which is true when the values fill the parameters.
func multiReturnFillsParameters(signature *types.Signature, resultCount int) bool {
	parameters := signature.Params().Len()
	if signature.Variadic() {
		return resultCount >= parameters-1
	}
	return resultCount == parameters
}

// nativeCallSignature resolves the *types.Signature of a native call expression, or nil
// when the callee type is not a signature.
//
// Takes expression (*ast.CallExpr) which is the call to inspect.
//
// Returns the resolved signature, or nil.
func (c *Compiler) nativeCallSignature(expression *ast.CallExpr) *types.Signature {
	typeAndValue, ok := c.Info.Types[expression.Fun]
	if !ok || typeAndValue.Type == nil {
		return nil
	}
	signature, isSignature := typeAndValue.Type.Underlying().(*types.Signature)
	if !isSignature {
		return nil
	}
	return signature
}

// preboxNativeInterfaceArgument boxes a scalar argument as interface when the native
// callee's fixed parameter at argumentIndex has interface type, preserving the argument's
// precise source-level type.
//
// Takes signature (*types.Signature) which is the native callee's signature; nil leaves
// the location unchanged.
// Takes argumentIndex (int) which is the positional argument index.
// Takes location (VarLocation) which is the compiled argument.
//
// Returns VarLocation which is the (possibly re-boxed) argument location.
func (c *Compiler) preboxNativeInterfaceArgument(ctx context.Context, signature *types.Signature, argumentIndex int, location program.VarLocation) program.VarLocation {
	if signature == nil || location.Kind == isa.RegisterGeneral || location.SourceType == nil {
		return location
	}
	parameters := signature.Params()
	if parameters == nil {
		return location
	}
	fixedCount := parameters.Len()
	if signature.Variadic() {
		fixedCount--
	}
	if argumentIndex < 0 {
		return location
	}
	if argumentIndex >= fixedCount {
		return c.preboxVariadicInterfaceArgument(ctx, signature, location)
	}
	if _, isInterface := parameters.At(argumentIndex).Type().Underlying().(*types.Interface); !isInterface {
		return location
	}
	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	if !c.emitTypedBox(generalRegister, location) {
		c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
		return location
	}
	return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// preboxVariadicInterfaceArgument boxes a value bound for a native's `...any` slot when
// its static type is not the bank's canonical type.
//
// Takes signature (*types.Signature) which is the native callee's signature.
// Takes location (program.VarLocation) which holds the argument.
//
// Returns the general-bank location holding the typed box, or location unchanged when no
// boxing is needed.
func (c *Compiler) preboxVariadicInterfaceArgument(ctx context.Context, signature *types.Signature, location program.VarLocation) program.VarLocation {
	if !signature.Variadic() || !c.boxNeedsPreciseType(location) {
		return location
	}
	last := signature.Params().At(signature.Params().Len() - 1)
	slice, ok := last.Type().Underlying().(*types.Slice)
	if !ok {
		return location
	}
	if _, isInterface := slice.Elem().Underlying().(*types.Interface); !isInterface {
		return location
	}
	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	c.emitBoxToGeneral(ctx, generalRegister, location)
	return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
}

// compileNativeArguments compiles the arguments of a native call, applying multi-return
// spread, eval-bool coercion, and interface pre-boxing for fixed interface parameters.
//
// Takes expression (*ast.CallExpr) which is the native call whose arguments are compiled.
//
// Returns the compiled argument locations and any compilation error.
func (c *Compiler) compileNativeArguments(ctx context.Context, expression *ast.CallExpr) ([]program.VarLocation, error) {
	if spreadLocations, ok, err := c.tryCompileNativeCallFromMultiReturn(ctx, expression); err != nil {
		return nil, err
	} else if ok {
		return spreadLocations, nil
	}
	nativeSignature := c.nativeCallSignature(expression)
	byteViews := c.takeByteViewCandidates(expression, nativeSignature)
	argumentLocations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		if byteViews&(1<<i) != 0 {
			continue
		}
		mark := c.Scopes.Alloc.Snapshot()
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
		location = c.preboxNativeInterfaceArgument(ctx, nativeSignature, i, location)
		argumentLocations[i] = c.compactArgument(ctx, mark, location, c.staticTypeOf(argument))
	}
	if byteViews != 0 {
		if err := c.compileByteViewArguments(ctx, expression, byteViews, argumentLocations); err != nil {
			return nil, err
		}
	}
	return argumentLocations, nil
}

// allocateNativeReturns allocates result registers for a native call according to the
// callee's signature.
//
// Takes signature (*types.Signature) which is the native callee's signature; nil yields
// no result locations.
//
// Returns the allocated return locations and the primary result location (the first
// return, or the zero value when there are none).
func (c *Compiler) allocateNativeReturns(signature *types.Signature) ([]program.VarLocation, program.VarLocation) {
	if signature == nil {
		return nil, program.VarLocation{}
	}
	var returnLocations []program.VarLocation
	for v := range signature.Results().Variables() {
		kind := c.kindFor(v.Type())
		register := c.Scopes.Alloc.Alloc(kind)
		returnLocations = append(returnLocations, program.VarLocation{Register: register, Kind: kind})
	}
	if len(returnLocations) > 0 {
		return returnLocations, returnLocations[0]
	}
	return returnLocations, program.VarLocation{}
}

// nativeCallBlocksHostGoroutine reports whether the native call target parks the host
// goroutine.
//
// Takes expression (*ast.CallExpr) which is the native call expression to classify.
//
// Returns bool which is true when the call parks the host goroutine.
func (c *Compiler) nativeCallBlocksHostGoroutine(expression *ast.CallExpr) bool {
	selector, ok := expression.Fun.(*ast.SelectorExpr)
	if !ok || c.Info == nil {
		return false
	}
	function, ok := c.Info.ObjectOf(selector.Sel).(*types.Func)
	if !ok || function.Pkg() == nil {
		return false
	}
	if selection, isSelection := c.Info.Selections[selector]; isSelection && selection.Kind() == types.MethodVal {
		return isBlockingStdlibMethod(methodReceiverTypeName(function), function.Name())
	}
	return function.Pkg().Path() == "time" && function.Name() == "Sleep"
}

// compileNativeCallFromLocation compiles a call to a function stored in a general
// register.
//
// Takes expression (*ast.CallExpr) which is the AST call expression containing the
// arguments.
// Takes functionLocation (VarLocation) which is the VarLocation holding the function
// reference.
// Takes methodReceiverRegister (...uint8) which optionally specifies a general register
// holding the method receiver.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileNativeCallFromLocation(ctx context.Context, expression *ast.CallExpr, functionLocation program.VarLocation, methodReceiverRegister ...uint8) (program.VarLocation, error) {
	c.byteViewCall = expression
	argumentLocations, err := c.compileNativeArguments(ctx, expression)
	byteViews := c.byteViewMarked
	c.byteViewCall, c.byteViewMarked = nil, 0
	if err != nil {
		return program.VarLocation{}, err
	}

	returnLocations, resultLocation := c.allocateNativeReturns(c.nativeCallSignature(expression))

	site := c.nativeCallSite(ctx, expression, functionLocation, argumentLocations, returnLocations)
	site.ByteViewArguments = byteViews
	if len(methodReceiverRegister) > 0 {
		site.IsMethod = true
		site.MethodReceiverRegister = methodReceiverRegister[0]
	}
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	if err := c.retypeErasedPointerResults(ctx, expression, returnLocations); err != nil {
		return program.VarLocation{}, err
	}

	return resultLocation, nil
}

// retypeErasedPointerResults gives the pointer results of a method of a native-backed
// generic type (sync/atomic.Pointer) the type the program instantiated it at.
//
// Every instantiation of such a type runs on one erased instantiation (atomic.Pointer
// over struct{}), which stores the pointer it is given unchanged. A pointer it returns
// carries the erased element type and is retyped here, keeping its address, so it
// compares equal to the pointer stored and reads the value it points at.
//
// Takes expression (*ast.CallExpr) which is the call.
// Takes returnLocations ([]VarLocation) which hold the results.
//
// Returns any error adding the type to the type table.
func (c *Compiler) retypeErasedPointerResults(ctx context.Context, expression *ast.CallExpr, returnLocations []program.VarLocation) error {
	signature, ok := c.erasedGenericMethodSignature(expression)
	if !ok {
		return nil
	}
	for index, location := range returnLocations {
		if index >= signature.Results().Len() || location.Kind != isa.RegisterGeneral {
			continue
		}
		resultType := signature.Results().At(index).Type()
		if _, isPointer := resultType.Underlying().(*types.Pointer); !isPointer {
			continue
		}
		reflectType := c.TypeToReflect(ctx, resultType)
		if reflectType == nil {
			return fmt.Errorf("%w: result %d of %s", fault.ErrCompileNamedTypeIdentity, index, signature)
		}
		typeIndex, err := program.AddTypeRef(c.Function, reflectType)
		if err != nil {
			return err
		}
		program.Emit(c.Function, isa.OpConvertGeneral, location.Register, location.Register, 0)
		program.EmitExtension(c.Function, typeIndex, 0)
	}
	return nil
}

// erasedGenericMethodSignature returns the signature of the method a call names when its
// receiver is an instantiation of a native-backed generic type.
//
// Takes expression (*ast.CallExpr) which is the call.
//
// Returns the method's instantiated signature and true, or false for any other call.
func (c *Compiler) erasedGenericMethodSignature(expression *ast.CallExpr) (*types.Signature, bool) {
	if c.symbols == nil || c.Info == nil {
		return nil, false
	}
	selector, ok := ast.Unparen(expression.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	selection, ok := c.Info.Selections[selector]
	if !ok || selection.Kind() != types.MethodVal {
		return nil, false
	}
	receiver := types.Unalias(selection.Recv())
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		receiver = types.Unalias(pointer.Elem())
	}
	named, ok := receiver.(*types.Named)
	if !ok || named.TypeArgs().Len() == 0 || named.Obj().Pkg() == nil {
		return nil, false
	}
	if _, erased := c.symbols.NativeBackedReflectType(named.Obj().Pkg().Path(), named.Obj().Name()); !erased {
		return nil, false
	}
	signature, ok := selection.Type().(*types.Signature)
	return signature, ok
}

// nativeCallSite builds the packing, interface and blocking metadata for a call through a
// function value.
//
// Takes expression (*ast.CallExpr) which is the call.
// Takes functionLocation (VarLocation) which holds the callee value.
// Takes argumentLocations ([]VarLocation) which hold the compiled arguments.
// Takes returnLocations ([]VarLocation) which receive the results.
//
// Returns program.CallSite which is the site, not yet added to the function.
func (c *Compiler) nativeCallSite(ctx context.Context, expression *ast.CallExpr, functionLocation program.VarLocation, argumentLocations, returnLocations []program.VarLocation) program.CallSite {
	signature := c.nativeCallSignature(expression)
	argumentTypeNames, argumentTypeStrings := c.resolveArgumentStaticTypes(expression)
	site := program.CallSite{
		IsNative:                  true,
		NativeRegister:            functionLocation.Register,
		Arguments:                 argumentLocations,
		Returns:                   returnLocations,
		IsEllipsisSpread:          expression.Ellipsis.IsValid(),
		ArgumentStaticTypeNames:   argumentTypeNames,
		ArgumentStaticTypeStrings: argumentTypeStrings,
		ParameterInterfaceFlags:   collectParameterInterfaceFlags(signature),
		BlocksHostGoroutine:       c.nativeCallBlocksHostGoroutine(expression),
	}
	if signature != nil && signature.Variadic() && !expression.Ellipsis.IsValid() {
		lastParameter := signature.Params().At(signature.Params().Len() - 1)
		site.RuntimeVariadicSliceType = c.TypeToReflect(ctx, lastParameter.Type())
		site.RuntimeVariadicNumFixed = safeconv.MustIntToUint8(signature.Params().Len() - 1)
	}
	return site
}

// methodReceiverTypeName returns the "pkgpath.TypeName" of the type declaring the method.
//
// Takes function (*types.Func) which is the resolved method.
//
// Returns the declaring receiver's qualified name, or "" when it is not a package-scoped
// named type.
func methodReceiverTypeName(function *types.Func) string {
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return ""
	}
	return receiverTypeName(signature.Recv().Type())
}

// isBlockingStdlibMethod matches the sync-package methods that block the calling
// goroutine without running interpreted code, so the interpreter lock can be released
// around the whole native call in safe mode.
//
// Takes receiverType (string) which is the "pkgpath.TypeName" of the receiver.
// Takes method (string) which is the method name.
//
// Returns bool which is true when the method blocks the calling goroutine.
func isBlockingStdlibMethod(receiverType, method string) bool {
	switch receiverType {
	case "sync.WaitGroup", "sync.Cond":
		return method == "Wait"
	case "sync.Mutex":
		return method == "Lock"
	case "sync.RWMutex":
		return method == "Lock" || method == "RLock"
	default:
		return false
	}
}

// receiverTypeName returns the "pkgpath.TypeName" of a method receiver, stripping a
// pointer layer, or "" when the receiver is not a package-scoped named type.
//
// Takes receiver (types.Type) which is the method receiver type.
//
// Returns string which is the "pkgpath.TypeName", or "" when the receiver is not a
// package-scoped named type.
func receiverTypeName(receiver types.Type) string {
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	named, ok := receiver.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name()
}

// collectParameterInterfaceFlags records which fixed parameters have interface type.
//
// Takes signature (*types.Signature) which is the native callee's signature.
//
// Returns []bool which is nil when the callee has no fixed interface parameters,
// otherwise a per-slot flag slice.
func collectParameterInterfaceFlags(signature *types.Signature) []bool {
	if signature == nil {
		return nil
	}
	params := signature.Params()
	if params == nil || params.Len() == 0 {
		return nil
	}
	count := params.Len()
	if signature.Variadic() {
		count--
	}
	if count <= 0 {
		return nil
	}
	flags := make([]bool, count)
	hasInterface := false
	for i := range count {
		_, isInterface := params.At(i).Type().Underlying().(*types.Interface)
		flags[i] = isInterface
		hasInterface = hasInterface || isInterface
	}
	if !hasInterface {
		return nil
	}
	return flags
}
