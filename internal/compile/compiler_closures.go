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
	"os"
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

var (
	// checkCaptureEnabled turns on the capture assertion (PIPIT_CHECK_CAPTURE=1): a closure
	// that captures a variable by value which the enclosing function assigns is a compile
	// error instead of a silent snapshot. Off by default; the escape collector's
	// capture-by-reference rule is what keeps it from firing.
	checkCaptureEnabled = os.Getenv("PIPIT_CHECK_CAPTURE") != ""

	// scalarConversions is a table of specialised cross-bank conversion sub-opcodes looked
	// up by (srcKind, destinationKind).
	scalarConversions = map[scalarConversionKey]scalarConversionEntry{
		{Source: isa.RegisterInt, destination: isa.RegisterFloat}:  {subOp: isa.SubOpIntToFloat, destinationKind: isa.RegisterFloat},
		{Source: isa.RegisterFloat, destination: isa.RegisterInt}:  {subOp: isa.SubOpFloatToInt, destinationKind: isa.RegisterInt},
		{Source: isa.RegisterInt, destination: isa.RegisterUint}:   {subOp: isa.SubOpIntToUint, destinationKind: isa.RegisterUint},
		{Source: isa.RegisterUint, destination: isa.RegisterInt}:   {subOp: isa.SubOpUintToInt, destinationKind: isa.RegisterInt},
		{Source: isa.RegisterUint, destination: isa.RegisterFloat}: {subOp: isa.SubOpUintToFloat, destinationKind: isa.RegisterFloat},
		{Source: isa.RegisterFloat, destination: isa.RegisterUint}: {subOp: isa.SubOpFloatToUint, destinationKind: isa.RegisterUint},
		{Source: isa.RegisterBool, destination: isa.RegisterInt}:   {subOp: isa.SubOpBoolToInt, destinationKind: isa.RegisterInt},
		{Source: isa.RegisterInt, destination: isa.RegisterBool}:   {subOp: isa.SubOpIntToBool, destinationKind: isa.RegisterBool},
	}
)

// scalarConversionKey identifies a source/destination register kind pair used to look up
// cross-bank scalar conversion sub-opcodes.
type scalarConversionKey struct {
	// Source is the source register kind for the conversion.
	Source isa.RegisterKind

	// destination is the destination register kind for the conversion.
	destination isa.RegisterKind
}

// scalarConversionEntry maps a (source, destination) kind pair to the tier-1 sub-op tag
// and destination register kind used to perform the conversion. Each cross-bank
// conversion is emitted as {isa.OpDrillTier1, subOp, destination, source}.
type scalarConversionEntry struct {
	// subOp is the tier-1 sub-opcode emitted for this conversion.
	subOp isa.SubOpcode

	// destinationKind is the register kind of the conversion result.
	destinationKind isa.RegisterKind
}

// compileFunctionLit compiles a function literal (closure) and emits isa.OpMakeClosure
// into a fresh general register.
//
// Takes lit (*ast.FuncLit) which is the function literal AST node.
//
// Returns the closure variable location and any compilation error.
func (c *Compiler) compileFunctionLit(ctx context.Context, lit *ast.FuncLit) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureClosures, lit.Type.Func); err != nil {
		return program.VarLocation{}, err
	}
	functionIndex, _, err := c.compileClosureBody(ctx, lit)
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpMakeClosure, dest, functionIndex)

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileClosureBody compiles a function literal body and registers the resulting
// CompiledFunction in the root function's nested functions table.
//
// Takes lit (*ast.FuncLit) which is the function literal AST node.
//
// Returns the function index, the sorted free variable names, and any compilation error.
func (c *Compiler) compileClosureBody(ctx context.Context, lit *ast.FuncLit) (uint16, []string, error) {
	freeVars := c.findFreeVars(ctx, lit)

	compiledFunction := &program.CompiledFunction{Name: "<closure>"}
	c.closureCount++
	compiledFunction.RuntimeName = c.closureRuntimeName(c.closureCount)

	tv := c.Info.Types[lit]
	signature, ok := tv.Type.(*types.Signature)
	if !ok {
		return 0, nil, fmt.Errorf("expected *types.Signature, got %T", tv.Type)
	}
	compiledFunction.IsVariadic = signature.Variadic()
	compiledFunction.VariadicSliceType = c.variadicSliceReflectType(ctx, signature)

	c.populateClosureParameterAndResultKinds(ctx, compiledFunction, lit, signature)
	upvalueMap := make(map[string]upvalueReference)
	c.buildFreeVarUpvalues(ctx, compiledFunction, freeVars, upvalueMap, 0)
	functionIndex := safeconv.MustIntToUint16(len(c.RootFunction.Functions))
	c.RootFunction.Functions = append(c.RootFunction.Functions, compiledFunction)

	sub := newFunctionCompiler(ctx, c.programContext, compiledFunction, functionOptions{
		scopeName:         "<closure>",
		upvalues:          upvalueMap,
		substitutions:     c.typeSubstitutions,
		substitutionCache: c.typeSubstitutionsCache,
		rangeOverFunction: nil,
	})
	sub.recordUpvalueDebugEntries(upvalueMap)
	sub.prepareBody(bodySpec{body: lit.Body, params: lit.Type.Params, resultTypes: resultTypesOf(signature)})
	sub.declareClosureParams(ctx, lit)
	sub.declareNamedResults(ctx, lit.Type.Results, sub.Function)

	if _, err := sub.compileStmtList(ctx, lit.Body.List); err != nil {
		return 0, nil, fmt.Errorf("compiling closure: %w", err)
	}

	if err := sub.finishBody(ctx, compiledFunction, lit.Body, finishFlags{tailCall: true, finaliseDefer: true, classifyEscapes: true}); err != nil {
		return 0, nil, fmt.Errorf("compiling closure: %w", err)
	}

	return functionIndex, freeVars, nil
}

// populateClosureParameterAndResultKinds fills closure kind metadata.
//
// Fills compiledFunction.parameterKinds, compiledFunction.ParameterIsGeneric, and
// compiledFunction.resultKinds for the closure literal lit using the closure's go/types
// signature. Closure-specific kind selection applies: heap-promoted captured params,
// type-parameter-bearing params, and typed-slice survivors each override the default
// call-slot kind.
//
// Takes compiledFunction (*CompiledFunction) which receives the kind metadata.
// Takes lit (*ast.FuncLit) which is the source function literal.
// Takes signature (*types.Signature) which is the closure's typed signature.
func (c *Compiler) populateClosureParameterAndResultKinds(ctx context.Context, compiledFunction *program.CompiledFunction, lit *ast.FuncLit, signature *types.Signature) {
	parameterCount := signature.Params().Len()
	parameterIndex := 0
	closureHeapPromoted := collectHeapPromotedParamNames(c, lit.Type, lit.Body)
	closureTypedSliceParams := classifyTypedSliceParameters(c, lit.Body, signature)
	for p := range signature.Params().Variables() {
		kind := c.parameterSlotKind(signature, p.Type(), parameterIndex, parameterCount)
		if closureHeapPromoted[p.Name()] {
			kind = c.kindFor(p.Type())
		}
		if typemap.IsTypeParameter(p.Type()) || typemap.ContainsTypeParameter(p.Type()) {
			kind = c.kindFor(p.Type())
		}
		if isa.IsTypedSliceKind(kind) {
			if survivorKind, ok := closureTypedSliceParams[p.Name()]; ok {
				kind = survivorKind
			} else {
				kind = c.kindFor(p.Type())
			}
		}
		compiledFunction.ParameterKinds = append(compiledFunction.ParameterKinds, kind)
		compiledFunction.ParameterIsGeneric = append(compiledFunction.ParameterIsGeneric, typemap.IsTypeParameter(p.Type()))
		parameterIndex++
	}
	for r := range signature.Results().Variables() {
		compiledFunction.ResultKinds = append(compiledFunction.ResultKinds, c.kindForCallSlot(r.Type()))
		compiledFunction.ResultReflectTypes = append(compiledFunction.ResultReflectTypes, c.exactReflectTypeForBoxing(r.Type()))
	}
	compiledFunction.SignatureReflectType = c.signatureReflectType(ctx, signature)
}

// signatureReflectType converts a function's signature, without its receiver, to the
// reflect func type a value of that function carries at run time.
//
// Takes signature (*types.Signature) which is the function's signature.
//
// Returns reflect.Type which is the func type, or nil when the signature mentions a type
// parameter (an erased generic body has no single static type).
func (c *Compiler) signatureReflectType(ctx context.Context, signature *types.Signature) reflect.Type {
	if signature == nil {
		return nil
	}
	shape := types.NewSignatureType(nil, nil, nil, signature.Params(), signature.Results(), signature.Variadic())
	if typemap.ContainsTypeParameter(shape) {
		return nil
	}
	reflectType := c.TypeToReflect(ctx, shape)
	if reflectType == nil || reflectType.Kind() != reflect.Func {
		return nil
	}
	return reflectType
}

// declareClosureParams declares the closure's parameter variables in the sub-Compiler's
// scope and applies heap promotion to each.
//
// Takes lit (*ast.FuncLit) which is the function literal AST node.
func (c *Compiler) declareClosureParams(ctx context.Context, lit *ast.FuncLit) {
	if lit.Type.Params == nil {
		return
	}
	recordParam := func(location program.VarLocation) {
		if c.Function != nil {
			c.Function.ParameterRegisters = append(c.Function.ParameterRegisters, location.Register)
		}
	}
	parameterPosition := 0
	for _, field := range lit.Type.Params.List {
		if len(field.Names) == 0 {
			parameterPosition = c.declareAnonymousParameter(c, field.Type, parameterPosition, recordParam)
			continue
		}
		for _, name := range field.Names {
			parameterPosition = c.declareClosureParam(ctx, name, parameterPosition, recordParam)
		}
	}
}

// declareClosureParam declares one named closure parameter in the closure's scope, taking
// the bank the closure's signature assigned to that position.
//
// Takes name (*ast.Ident) which is the parameter identifier.
// Takes parameterPosition (int) which is the parameter's index.
// Takes recordParam (func(VarLocation)) which records the register.
//
// Returns the next parameter position.
func (c *Compiler) declareClosureParam(ctx context.Context, name *ast.Ident, parameterPosition int, recordParam func(program.VarLocation)) int {
	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return parameterPosition + 1
	}
	kind := c.kindFor(typeObject.Type())
	if c.Function != nil && parameterPosition < len(c.Function.ParameterKinds) {
		kind = c.Function.ParameterKinds[parameterPosition]
	}
	location := c.Scopes.DeclareVar(name.Name, kind)
	recordParam(location)
	c.tryHeapPromoteCapturedLocal(ctx, name.Name, name)
	return parameterPosition + 1
}

// buildFreeVarUpvalues appends upvalue descriptors for freeVars to compiledFunction and
// populates upvalueMap with the matching references. Sources each free variable from
// either the enclosing scope (isLocal=true descriptor) or the enclosing function's
// upvalueMap (isLocal=false).
//
// Takes compiledFunction (*CompiledFunction) which receives appended descriptors.
// Takes freeVars ([]string) which are the captured variable names.
// Takes upvalueMap (map[string]upvalueReference) which receives the per-name upvalue
// reference.
// Takes startIndex (int) which is the first upvalue index to assign.
func (c *Compiler) buildFreeVarUpvalues(ctx context.Context, compiledFunction *program.CompiledFunction, freeVars []string, upvalueMap map[string]upvalueReference, startIndex int) {
	uvIndex := startIndex
	for _, name := range freeVars {
		if outerLocation, ok := c.Scopes.LookupVar(name); ok {
			c.captureLocalUpvalue(ctx, compiledFunction, name, outerLocation, upvalueMap, uvIndex)
			uvIndex++
			continue
		}
		if parentRef, found := c.upvalueMap[name]; found {
			captureParentUpvalue(compiledFunction, name, parentRef, upvalueMap, uvIndex)
			uvIndex++
		}
	}
}

// captureLocalUpvalue records the capture of a variable of the enclosing function: a
// spilled variable is reloaded first, an indirect one is captured as its cell, and a
// by-value capture of a variable the enclosing function assigns is reported when the
// capture check is enabled.
//
// Takes compiledFunction (*program.CompiledFunction) which is the closure being built.
// Takes name (string) which is the captured variable.
// Takes outerLocation (program.VarLocation) which is the variable's location.
// Takes upvalueMap (map[string]upvalueReference) which receives the closure's reference.
// Takes uvIndex (int) which is the upvalue's index in the closure.
func (c *Compiler) captureLocalUpvalue(
	ctx context.Context,
	compiledFunction *program.CompiledFunction,
	name string,
	outerLocation program.VarLocation,
	upvalueMap map[string]upvalueReference,
	uvIndex int,
) {
	if outerLocation.IsSpilled {
		scratch := c.emitReloadIfSpilled(ctx, outerLocation)
		outerLocation = program.VarLocation{Register: scratch.Register, Kind: outerLocation.Kind, IsIndirect: outerLocation.IsIndirect, OriginalKind: outerLocation.OriginalKind}
		c.Scopes.UpdateVar(name, outerLocation)
	}
	descriptorKind := outerLocation.Kind
	referenceKind := outerLocation.Kind
	if outerLocation.IsIndirect {
		descriptorKind = isa.RegisterGeneral
		referenceKind = outerLocation.OriginalKind
	} else if checkCaptureEnabled && c.captureCheckNames[name] {
		c.recordStickyError(fmt.Errorf("capture check: %q is captured by value by %s but assigned in the enclosing function", name, compiledFunction.Name))
	}
	compiledFunction.UpvalueDescriptors = append(compiledFunction.UpvalueDescriptors, program.UpvalueDescriptor{
		Index:        outerLocation.Register,
		Kind:         descriptorKind,
		IsLocal:      true,
		IsIndirect:   outerLocation.IsIndirect,
		OriginalKind: outerLocation.OriginalKind,
	})
	upvalueMap[name] = upvalueReference{
		index:        uvIndex,
		kind:         referenceKind,
		isIndirect:   outerLocation.IsIndirect,
		originalKind: outerLocation.OriginalKind,
	}
	c.Scopes.MarkCaptured(name)
}

// compileIIFE compiles an immediately invoked function expression. Emits
// isa.SubOpCallIIFE followed by isa.SubOpTier3SyncIIFEUpvalues when the literal captures
// any free variable; otherwise emits a plain isa.SubOpCall.
//
// Takes lit (*ast.FuncLit) which is the function literal AST node.
// Takes expression (*ast.CallExpr) which is the call expression containing the arguments.
//
// Returns the first result location and any compilation error.
func (c *Compiler) compileIIFE(ctx context.Context, lit *ast.FuncLit, expression *ast.CallExpr) (program.VarLocation, error) {
	functionIndex, freeVars, err := c.compileClosureBody(ctx, lit)
	if err != nil {
		return program.VarLocation{}, err
	}

	argumentLocations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}
		argumentLocations[i] = location
	}

	tv := c.Info.Types[lit]
	signature, ok := tv.Type.(*types.Signature)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("expected *types.Signature, got %T", tv.Type)
	}

	var returnLocations []program.VarLocation
	var resultLocation program.VarLocation
	for r := range signature.Results().Variables() {
		kind := c.kindFor(r.Type())
		register := c.Scopes.Alloc.Alloc(kind)
		returnLocations = append(returnLocations, program.VarLocation{Register: register, Kind: kind})
	}
	if len(returnLocations) > 0 {
		resultLocation = returnLocations[0]
	}

	site := program.CallSite{
		Arguments:     argumentLocations,
		Returns:       returnLocations,
		FunctionIndex: functionIndex,
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

	if len(freeVars) > 0 {
		program.EmitTier1Wide(c.Function, isa.SubOpCallIIFE, siteIndex)
		program.EmitTier3(c.Function, isa.SubOpTier3SyncIIFEUpvalues)
	} else {
		c.emitCall(isa.SubOpCall, siteIndex)
	}

	return resultLocation, nil
}

// findFreeVars returns the variables referenced inside lit that are declared in an
// enclosing scope, including transitive captures from nested function literals.
//
// Takes lit (*ast.FuncLit) which is the function literal AST node.
//
// Returns a sorted list of captured variable names.
func (c *Compiler) findFreeVars(ctx context.Context, lit *ast.FuncLit) []string {
	localDefs := make(map[string]bool)
	if lit.Type.Params != nil {
		for _, field := range lit.Type.Params.List {
			for _, name := range field.Names {
				localDefs[name.Name] = true
			}
		}
	}

	escape.CollectLocalDefs(lit.Body, localDefs)

	free := make(map[string]bool)
	c.collectFreeIdents(ctx, lit.Body, localDefs, free)

	result := make([]string, 0, len(free))
	for name := range free {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// collectFreeIdents walks body collecting identifiers that refer to variables from the
// enclosing scope. Nested function literals are descended into via
// collectNestedLitFreeIdents to capture transitively-referenced variables.
//
// Takes body (*ast.BlockStmt) which is the block statement to walk.
// Takes localDefs (map[string]bool) which holds the locally-defined variable names to
// exclude.
// Takes free (map[string]bool) which accumulates the free variable names.
func (c *Compiler) collectFreeIdents(ctx context.Context, body *ast.BlockStmt, localDefs map[string]bool, free map[string]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		if nestedLit, ok := n.(*ast.FuncLit); ok {
			c.collectNestedLitFreeIdents(ctx, nestedLit, localDefs, free)
			return false
		}

		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if localDefs[id.Name] && !c.identDeclaredOutside(id, body) {
			return true
		}

		c.markIdentFreeIfCaptured(ctx, id, free)
		return true
	})
}

// identDeclaredOutside reports whether id refers to a variable declared outside body,
// which a same-named local declared later inside body (`x := f(x)`) does not change: the
// new x is only in scope after its declaration, so this use is the outer x.
//
// Takes id (*ast.Ident) which is the use.
// Takes body (*ast.BlockStmt) which is the closure body.
//
// Returns true when the identifier's object lies outside the body.
func (c *Compiler) identDeclaredOutside(id *ast.Ident, body *ast.BlockStmt) bool {
	object, ok := c.Info.Uses[id].(*types.Var)
	if !ok || !object.Pos().IsValid() {
		return false
	}
	return object.Pos() < body.Pos() || object.Pos() >= body.End()
}

// collectNestedLitFreeIdents recursively collects free identifiers from a nested function
// literal, promoting transitive captures that also need to be captured by the enclosing
// function.
//
// Takes nestedLit (*ast.FuncLit) which is the nested function literal.
// Takes localDefs (map[string]bool) which holds the enclosing function's local
// declarations.
// Takes free (map[string]bool) which accumulates the free variable names.
func (c *Compiler) collectNestedLitFreeIdents(ctx context.Context, nestedLit *ast.FuncLit, localDefs map[string]bool, free map[string]bool) {
	nestedDefs := make(map[string]bool)
	if nestedLit.Type.Params != nil {
		for _, field := range nestedLit.Type.Params.List {
			for _, name := range field.Names {
				nestedDefs[name.Name] = true
			}
		}
	}
	escape.CollectLocalDefs(nestedLit.Body, nestedDefs)

	nestedFree := make(map[string]bool)
	c.collectFreeIdents(ctx, nestedLit.Body, nestedDefs, nestedFree)

	for name := range nestedFree {
		if localDefs[name] {
			continue
		}
		c.markNameFreeIfCaptured(ctx, name, free)
	}
}

// markIdentFreeIfCaptured marks id as free when it resolves to a *types.Var defined in
// the enclosing scope or upvalue map.
//
// Takes id (*ast.Ident) which is the identifier to check.
// Takes free (map[string]bool) which accumulates the free variable names.
func (c *Compiler) markIdentFreeIfCaptured(ctx context.Context, id *ast.Ident, free map[string]bool) {
	typeObject, ok := c.Info.Uses[id]
	if !ok {
		return
	}
	if _, isVar := typeObject.(*types.Var); !isVar {
		return
	}
	c.markNameFreeIfCaptured(ctx, id.Name, free)
}

// markNameFreeIfCaptured marks name as free when it resolves to a local in the current
// scope or to an existing upvalue reference.
//
// Takes name (string) which is the variable name to check.
// Takes free (map[string]bool) which accumulates the free variable names.
func (c *Compiler) markNameFreeIfCaptured(_ context.Context, name string, free map[string]bool) {
	if _, found := c.Scopes.LookupVar(name); found {
		free[name] = true
	} else if _, found := c.upvalueMap[name]; found {
		free[name] = true
	}
}

// closureCallSignature resolves the *types.Signature of a closure variable referenced by
// identifier.
//
// Takes identifier (*ast.Ident) which names the closure variable.
//
// Returns the resolved signature, or an error when the variable is not callable.
func (c *Compiler) closureCallSignature(identifier *ast.Ident) (*types.Signature, error) {
	typeObject := c.Info.Uses[identifier]
	if typeObject != nil && typeObject.Type() != nil {
		if asSignature, ok := c.substitutedType(typeObject.Type()).Underlying().(*types.Signature); ok {
			return asSignature, nil
		}
	}
	return nil, fmt.Errorf("variable %s is not callable", identifier.Name)
}

// compileClosureCall compiles a call through a closure value held in a local variable.
// Emits isa.SubOpCall followed by isa.SubOpTier2SyncClosureUpvalues so the callee's
// writes through upvalue cells are mirrored back into the caller's snapshot.
//
// Takes identifier (*ast.Ident) which is the identifier of the closure variable.
// Takes expression (*ast.CallExpr) which is the call expression supplying the arguments.
// Takes closureLocation (VarLocation) which is the register holding the closure value.
//
// Returns the first result location and any compilation error.
func (c *Compiler) compileClosureCall(ctx context.Context, identifier *ast.Ident, expression *ast.CallExpr, closureLocation program.VarLocation) (program.VarLocation, error) {
	signature, err := c.closureCallSignature(identifier)
	if err != nil {
		return program.VarLocation{}, err
	}

	if closureLocation.IsIndirect {
		dereferenced, derefErr := c.EmitIndirectRead(ctx, closureLocation)
		if derefErr != nil {
			return program.VarLocation{}, derefErr
		}
		closureLocation = dereferenced
	}

	argumentLocations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		location, argErr := c.compileExpression(ctx, argument)
		if argErr != nil {
			return program.VarLocation{}, argErr
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
		argumentLocations[i] = c.preboxNativeInterfaceArgument(ctx, signature, i, location)
	}

	returnLocations, resultLocation := c.allocateNativeReturns(signature)

	site := program.CallSite{
		Arguments:        argumentLocations,
		Returns:          returnLocations,
		IsClosure:        true,
		ClosureRegister:  closureLocation.Register,
		IsEllipsisSpread: expression.Ellipsis.IsValid(),
	}
	if signature.Variadic() && !expression.Ellipsis.IsValid() {
		lastParameter := signature.Params().At(signature.Params().Len() - 1)
		site.RuntimeVariadicSliceType = c.TypeToReflect(ctx, lastParameter.Type())
		site.RuntimeVariadicNumFixed = safeconv.MustIntToUint8(signature.Params().Len() - 1)
	}
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.emitCall(isa.SubOpCall, siteIndex)
	program.EmitTier2(c.Function, isa.SubOpTier2SyncClosureUpvalues, closureLocation.Register)

	return resultLocation, nil
}

// compileTypeConversion compiles a type conversion expression such as int(x), string(x),
// or []byte(s). Dispatches through the scalarConversions table, the byte/string fast
// path, the same-kind short circuit, or a generic reflect-based fallback.
//
// Takes expression (*ast.CallExpr) which is the conversion call.
//
// Returns the converted location and any compilation error.
func (c *Compiler) compileTypeConversion(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileTypeConversionArgCount
	}

	dstType := c.Info.Types[expression].Type
	if location, handled, nilErr := c.compileTypedNilOrExpression(ctx, expression.Args[0], dstType); handled {
		return location, nilErr
	}

	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	srcType := c.Info.Types[expression.Args[0]].Type
	srcKind := c.kindFor(srcType)
	destinationKind := c.kindFor(dstType)

	argumentLocation = c.unpackConversionArgument(argumentLocation, srcKind)

	if location, ok, scalarErr := c.compileScalarConversion(ctx, argumentLocation, srcKind, destinationKind, srcType, dstType); ok {
		return location, scalarErr
	}

	if location, ok := c.compileByteStringConversion(ctx, argumentLocation, srcKind, destinationKind, srcType, dstType); ok {
		return location, nil
	}

	if location, ok := c.compileInterfaceConversion(ctx, argumentLocation, dstType); ok {
		return location, nil
	}

	if location, ok := c.compileSameKindConversion(ctx, argumentLocation, srcKind, destinationKind, srcType, dstType); ok {
		return location, nil
	}

	result, err := c.compileReflectConversion(ctx, argumentLocation, dstType, destinationKind)
	if err != nil {
		return result, err
	}
	c.EmitNarrowIntegerTruncation(result, dstType)
	return result, nil
}

// unpackConversionArgument unpacks a general-register conversion argument into its typed
// source register when the source type is a scalar kind.
//
// Takes argumentLocation (VarLocation) which is the compiled argument.
// Takes srcKind (isa.RegisterKind) which is the source register kind.
//
// Returns the possibly-unpacked argument location.
func (c *Compiler) unpackConversionArgument(argumentLocation program.VarLocation, srcKind isa.RegisterKind) program.VarLocation {
	if argumentLocation.Kind != isa.RegisterGeneral || srcKind == isa.RegisterGeneral {
		return argumentLocation
	}
	unpacked := c.Scopes.Alloc.AllocTemp(srcKind)
	program.Emit(c.Function, isa.OpUnpackInterface, unpacked, argumentLocation.Register, uint8(srcKind))
	return program.VarLocation{Register: unpacked, Kind: srcKind}
}

// compileScalarConversion handles cross-bank scalar conversions, including the
// float-to-narrow-integer reflect path and the scalarConversions sub-opcode table.
//
// Takes argumentLocation (VarLocation) which is the compiled argument.
// Takes srcKind (isa.RegisterKind) which is the source register kind.
// Takes destinationKind (isa.RegisterKind) which is the destination kind.
// Takes dstType (types.Type) which is the destination Go type.
//
// Returns (location, true, err) when handled, or (_, false, nil) when not applicable.
func (c *Compiler) compileScalarConversion(
	ctx context.Context,
	argumentLocation program.VarLocation,
	srcKind,
	destinationKind isa.RegisterKind,
	_,
	dstType types.Type,
) (program.VarLocation, bool, error) {
	if srcKind == isa.RegisterFloat && (destinationKind == isa.RegisterInt || destinationKind == isa.RegisterUint) && typemap.NarrowIntegerBitWidth(dstType) != 0 {
		result, err := c.compileReflectConversion(ctx, argumentLocation, dstType, destinationKind)
		if err != nil {
			return result, true, err
		}
		c.EmitNarrowIntegerTruncation(result, dstType)
		return result, true, nil
	}
	if entry, ok := scalarConversions[scalarConversionKey{Source: srcKind, destination: destinationKind}]; ok {
		dest := c.Scopes.Alloc.Alloc(entry.destinationKind)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(entry.subOp), dest, argumentLocation.Register)
		result := program.VarLocation{Register: dest, Kind: entry.destinationKind}
		c.EmitNarrowIntegerTruncation(result, dstType)
		return result, true, nil
	}
	return program.VarLocation{}, false, nil
}

// compileInterfaceConversion boxes a typed argument into a general register when the
// destination type is an interface.
//
// Takes argumentLocation (VarLocation) which is the compiled argument.
// Takes dstType (types.Type) which is the destination Go type.
//
// Returns (location, true) when handled, or (_, false) when the destination is not an
// interface or the argument is already general.
func (c *Compiler) compileInterfaceConversion(ctx context.Context, argumentLocation program.VarLocation, dstType types.Type) (program.VarLocation, bool) {
	if _, dstIsInterface := dstType.Underlying().(*types.Interface); !dstIsInterface || argumentLocation.Kind == isa.RegisterGeneral {
		return program.VarLocation{}, false
	}
	generalRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	if c.emitTypedBox(generalRegister, argumentLocation) {
		return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}, true
	}
	c.boxToGeneral(ctx, &argumentLocation)
	return argumentLocation, true
}

// compileSameKindConversion handles conversions where source and destination occupy the
// same register bank, emitting only a narrowing truncation when the integer bit widths
// differ.
//
// Takes argumentLocation (VarLocation) which is the compiled argument.
// Takes srcKind (isa.RegisterKind) which is the source register kind.
// Takes destinationKind (isa.RegisterKind) which is the destination kind.
// Takes srcType (types.Type) which is the source Go type.
// Takes dstType (types.Type) which is the destination Go type.
//
// Returns (location, true) when handled, or (_, false) when a reflect conversion is still
// required.
func (c *Compiler) compileSameKindConversion(
	ctx context.Context,
	argumentLocation program.VarLocation,
	srcKind,
	destinationKind isa.RegisterKind,
	srcType,
	dstType types.Type,
) (program.VarLocation, bool) {
	if srcKind != destinationKind || needsReflectSameKind(srcKind, srcType, dstType) {
		return program.VarLocation{}, false
	}
	if typemap.NarrowIntegerBitWidth(dstType) != 0 && typemap.NarrowIntegerBitWidth(srcType) != typemap.NarrowIntegerBitWidth(dstType) {
		dest := c.Scopes.Alloc.Alloc(destinationKind)
		result := program.VarLocation{Register: dest, Kind: destinationKind}
		c.emitMove(ctx, result, argumentLocation)
		c.EmitNarrowIntegerTruncation(result, dstType)
		return result, true
	}

	argumentLocation.SourceType = c.exactReflectTypeForBoxing(dstType)
	return argumentLocation, true
}

// compileByteStringConversion handles string-to-[]byte, []byte-to-string, and
// int-to-string (rune) conversions via tier-1 sub-opcodes.
//
// Takes argumentLocation (VarLocation) which is the compiled argument location.
// Takes srcKind (isa.RegisterKind) which is the source register kind.
// Takes destinationKind (isa.RegisterKind) which is the destination register kind.
// Takes srcType (types.Type) which is the source Go type.
// Takes dstType (types.Type) which is the destination Go type.
//
// Returns (location, true) when handled, or (_, false) when not applicable.
func (c *Compiler) compileByteStringConversion(
	_ context.Context,
	argumentLocation program.VarLocation,
	srcKind,
	destinationKind isa.RegisterKind,
	srcType,
	dstType types.Type,
) (program.VarLocation, bool) {
	if srcKind == isa.RegisterString && isSliceOfByte(dstType) {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpStringToBytes), dest, argumentLocation.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
	}

	if destinationKind == isa.RegisterString && isSliceOfByte(srcType) {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterString)
		if argumentLocation.Kind == isa.RegisterSliceByte {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpSliceByteToString), dest, argumentLocation.Register)
		} else {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpBytesToString), dest, argumentLocation.Register)
		}
		return program.VarLocation{Register: dest, Kind: isa.RegisterString}, true
	}

	if srcKind == isa.RegisterInt && destinationKind == isa.RegisterString {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterString)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpRuneToString), dest, argumentLocation.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterString}, true
	}

	return program.VarLocation{}, false
}

// compileReflectConversion emits a generic reflect-based type conversion via
// isa.OpConvert. Unboxes the result back into a typed bank when destinationKind is not
// isa.RegisterGeneral.
//
// Takes argumentLocation (VarLocation) which is the compiled argument location.
// Takes dstType (types.Type) which is the target Go type.
// Takes destinationKind (isa.RegisterKind) which is the destination register kind.
//
// Returns the converted variable location and any compilation error.
func (c *Compiler) compileReflectConversion(ctx context.Context, argumentLocation program.VarLocation, dstType types.Type, destinationKind isa.RegisterKind) (program.VarLocation, error) {
	c.boxToGeneral(ctx, &argumentLocation)

	reflectType := c.TypeToReflect(ctx, dstType)
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpConvert, dest, argumentLocation.Register, 0)
	program.EmitExtension(c.Function, typeIndex, 0)

	if destinationKind != isa.RegisterGeneral {
		return c.emitUnboxFromGeneral(dest, destinationKind), nil
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// closureRuntimeName returns the runtime name for the ordinal-th function literal,
// following Go's naming convention.
//
// Takes ordinal (int) which is the literal's position among the function's literals.
//
// Returns string which is the qualified name.
func (c *Compiler) closureRuntimeName(ordinal int) string {
	parent := c.Function.RuntimeName
	if parent == "" {
		qualifier := c.runtimePackageName
		if qualifier == "" {
			qualifier = "main"
		}
		parent = qualifier + ".init"
	}
	if c.Function.Name == "<closure>" {
		return fmt.Sprintf("%s.%d", parent, ordinal)
	}
	return fmt.Sprintf("%s.func%d", parent, ordinal)
}

// captureParentUpvalue records the capture of a variable the enclosing closure itself
// captured, forwarding the parent's upvalue.
//
// Takes compiledFunction (*program.CompiledFunction) which is the closure being built.
// Takes name (string) which is the captured variable.
// Takes parentRef (upvalueReference) which is the parent's reference.
// Takes upvalueMap (map[string]upvalueReference) which receives the closure's reference.
// Takes uvIndex (int) which is the upvalue's index in the closure.
func captureParentUpvalue(compiledFunction *program.CompiledFunction, name string, parentRef upvalueReference, upvalueMap map[string]upvalueReference, uvIndex int) {
	descriptorKind := parentRef.kind
	if parentRef.isIndirect {
		descriptorKind = isa.RegisterGeneral
	}
	compiledFunction.UpvalueDescriptors = append(compiledFunction.UpvalueDescriptors, program.UpvalueDescriptor{
		Index:        safeconv.MustIntToUint8(parentRef.index),
		Kind:         descriptorKind,
		IsLocal:      false,
		IsIndirect:   parentRef.isIndirect,
		OriginalKind: parentRef.originalKind,
	})
	upvalueMap[name] = upvalueReference{
		index:        uvIndex,
		kind:         parentRef.kind,
		isIndirect:   parentRef.isIndirect,
		originalKind: parentRef.originalKind,
	}
}
