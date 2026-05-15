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
	"slices"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/compile/inline"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

// yieldBodyParams bundles the parameters for compileYieldBody to stay within the
// argument-limit of 7.
type yieldBodyParams struct {
	// statement specifies the range statement AST node being compiled.
	statement *ast.RangeStmt

	// compiledFunction specifies the compiled function for the yield closure.
	compiledFunction *program.CompiledFunction

	// upvalueMap specifies the mapping from variable names to upvalue references.
	upvalueMap map[string]upvalueReference

	// returnKinds specifies the register kinds for each return value of the enclosing
	// function.
	returnKinds []isa.RegisterKind

	// stashUpvalueIndices specifies the upvalue indices for return value stash registers.
	stashUpvalueIndices []int

	// outerLabels specifies the labelled outer loop targets for cross-closure jumps.
	outerLabels []outerLabelTarget

	// numberOfYieldParameters specifies the number of parameters the yield callback accepts.
	numberOfYieldParameters int

	// stateFlagUpvalueIndex specifies the upvalue index for the state flag register.
	stateFlagUpvalueIndex int
}

// selectCase holds the pre-compiled information for a single select case.
type selectCase struct {
	// valueTarget is the expression a `case target = <-ch` assigns the value to, or nil for
	// `:=`, a blank target or a send case. Any assignable expression is allowed: a package
	// variable, a captured variable, an index, a field or a dereference.
	valueTarget ast.Expr

	// okTarget is the expression the comma-ok flag of a `case v, ok = <-ch` is assigned to,
	// or nil for `:=`, a blank target or no flag.
	okTarget ast.Expr

	// assignmentName specifies the variable name to declare in the case body for
	// recv-with-assignment cases.
	assignmentName string

	// okName specifies the variable name to declare for the comma-ok boolean in 'v, ok :=
	// <-ch' forms. Empty when there is no ok target.
	okName string

	// existingLocation holds the pre-existing variable location for the = form of
	// recv-assign. Only valid when isDefine is false.
	existingLocation program.VarLocation

	// valueKind specifies the register kind of the value being sent.
	valueKind isa.RegisterKind

	// channelRegister specifies the register holding the channel operand.
	channelRegister uint8

	// valueRegister specifies the register holding the value to send.
	valueRegister uint8

	// direction specifies the select case direction (recv, send, or default).
	direction uint8

	// destinationRegister specifies the register for the received value.
	destinationRegister uint8

	// destinationKind specifies the register kind of the receive destination.
	destinationKind isa.RegisterKind

	// okRegister specifies the int register for the comma-ok boolean.
	okRegister uint8

	// hasOk reports whether the case has a comma-ok destination.
	hasOk bool

	// assignmentKind specifies the register kind for the assigned receive variable.
	assignmentKind isa.RegisterKind

	// isDefine is true for := (short variable declaration) and false for = (assignment to
	// existing variable).
	isDefine bool
}

// EmitIndirectRead emits instructions to read through a heap-escaped pointer variable,
// returning the value in its original typed register.
//
// Takes location (VarLocation) which is the indirect variable location to dereference.
//
// Returns the dereferenced value location and any error.
func (c *Compiler) EmitIndirectRead(_ context.Context, location program.VarLocation) (program.VarLocation, error) {
	tempGen := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, tempGen, location.Register, 0)
	if location.OriginalKind == isa.RegisterGeneral {
		return program.VarLocation{Register: tempGen, Kind: isa.RegisterGeneral}, nil
	}
	dest := c.Scopes.Alloc.Alloc(location.OriginalKind)
	program.Emit(c.Function, isa.OpUnpackInterface, dest, tempGen, uint8(location.OriginalKind))
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, tempGen)
	return program.VarLocation{Register: dest, Kind: location.OriginalKind}, nil
}

// compileRangeOverFunction compiles a range-over-func loop (Go 1.23+).
//
// The loop body is wrapped in a yield callback closure that the iterator calls to deliver
// values. Outer scaffolding initialises a state flag, constructs the yield closure, calls
// the iterator with it, syncs upvalues, and dispatches stashed return values when the
// flag indicates a pending return. The yield body receives params as key/value, runs the
// body, then returns true to continue; break sets flag=1 and returns false, continue
// returns true, and return stashes values, sets flag=2, and returns false.
//
// Takes statement (*ast.RangeStmt) which is the range statement AST node to compile.
// Takes iterLocation (VarLocation) which is the register location of the iterator
// function.
// Takes iteratorSignature (*types.Signature) which is the type signature of the iterator
// function.
//
// Returns the result location and any compilation error.
func (c *Compiler) compileRangeOverFunction(ctx context.Context, statement *ast.RangeStmt, iterLocation program.VarLocation, iteratorSignature *types.Signature) (program.VarLocation, error) {
	yieldParameter := iteratorSignature.Params().At(0)
	yieldSignature, ok := yieldParameter.Type().Underlying().(*types.Signature)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("yield parameter underlying type is not a signature: %T", yieldParameter.Type().Underlying())
	}
	numYieldParams := yieldSignature.Params().Len()

	stateFlagReg := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	zeroIndex, err := program.AddIntConstant(c.Function, 0)
	if err != nil {
		return program.VarLocation{}, err
	}
	program.EmitWide(c.Function, isa.OpLoadIntConst, stateFlagReg, zeroIndex)

	stashRegs, returnKinds := c.allocReturnStash(ctx)

	freeVarNames := c.collectRangeBodyFreeVars(ctx, statement)

	compiledFunction, stateFlagUVIndex, stashUVIdxs, upvalueMap := c.buildYieldClosure(ctx,
		yieldSignature, stateFlagReg, stashRegs, freeVarNames,
	)

	root := c.RootFunction
	functionIndex := safeconv.MustIntToUint16(len(root.Functions))
	root.Functions = append(root.Functions, compiledFunction)

	outerLabels := c.collectOuterLabelTargets(ctx)

	if err := c.compileYieldBody(ctx, yieldBodyParams{
		statement:               statement,
		compiledFunction:        compiledFunction,
		numberOfYieldParameters: numYieldParams,
		upvalueMap:              upvalueMap,
		returnKinds:             returnKinds,
		stashUpvalueIndices:     stashUVIdxs,
		stateFlagUpvalueIndex:   stateFlagUVIndex,
		outerLabels:             outerLabels,
	}); err != nil {
		return program.VarLocation{}, err
	}

	yieldReg := c.emitIteratorCall(ctx, iterLocation, functionIndex)

	program.EmitTier2(c.Function, isa.SubOpTier2SyncClosureUpvalues, yieldReg)

	c.emitReturnStashDispatch(ctx, stateFlagReg, stashRegs, returnKinds, rangeBodyReturns(statement))

	c.emitOuterLabelDispatch(ctx, stateFlagReg, outerLabels)

	return program.VarLocation{}, nil
}

// allocReturnStash allocates registers for stashing return values when a return statement
// is found inside a range-over-func body.
//
// Returns the allocated stash register locations and the matching register kinds for each
// return value.
func (c *Compiler) allocReturnStash(_ context.Context) (stashRegs []program.VarLocation, returnKinds []isa.RegisterKind) {
	if len(c.Function.ResultKinds) == 0 {
		return nil, nil
	}
	returnKinds = c.Function.ResultKinds
	for _, kind := range returnKinds {
		register := c.Scopes.Alloc.Alloc(kind)
		stashRegs = append(stashRegs, program.VarLocation{Register: register, Kind: kind})
	}
	return stashRegs, returnKinds
}

// collectRangeBodyFreeVars finds free variables referenced in the range body and returns
// their names in sorted order.
//
// Takes statement (*ast.RangeStmt) which is the range statement whose body is analysed
// for free variable references.
//
// Returns the sorted list of free variable names found in the range body.
func (c *Compiler) collectRangeBodyFreeVars(ctx context.Context, statement *ast.RangeStmt) []string {
	localDefs := make(map[string]bool)
	if statement.Key != nil && !isBlankIdent(statement.Key) {
		if identifier, ok := statement.Key.(*ast.Ident); ok {
			localDefs[identifier.Name] = true
		}
	}
	if statement.Value != nil && !isBlankIdent(statement.Value) {
		if identifier, ok := statement.Value.(*ast.Ident); ok {
			localDefs[identifier.Name] = true
		}
	}
	escape.CollectLocalDefs(statement.Body, localDefs)

	free := make(map[string]bool)
	c.collectFreeIdents(ctx, statement.Body, localDefs, free)

	freeVarNames := make([]string, 0, len(free))
	for name := range free {
		freeVarNames = append(freeVarNames, name)
	}
	slices.Sort(freeVarNames)
	return freeVarNames
}

// buildYieldClosure constructs the compiledFunction for the yield callback, including
// parameter kinds, result kinds, and all upvalue descriptors (state flag, stash
// registers, free variables).
//
// Takes yieldSignature (*types.Signature) which is the yield function's type signature.
// Takes stateFlagReg (uint8) which is the register for the state flag.
// Takes stashRegs ([]VarLocation) which is the registers for return value stashing.
// Takes freeVarNames ([]string) which is the sorted list of free variable names.
//
// Returns the compiled function, state flag upvalue index, stash upvalue indices, and the
// upvalue reference map.
func (c *Compiler) buildYieldClosure(ctx context.Context,
	yieldSignature *types.Signature,
	stateFlagReg uint8,
	stashRegs []program.VarLocation,
	freeVarNames []string,
) (compiledFunction *program.CompiledFunction, stateFlagUVIndex int, stashUVIdxs []int, upvalueMap map[string]upvalueReference) {
	compiledFunction = &program.CompiledFunction{Name: "<yield>"}

	for v := range yieldSignature.Params().Variables() {
		compiledFunction.ParameterKinds = append(compiledFunction.ParameterKinds, c.kindForCallSlot(v.Type()))
		compiledFunction.ParameterIsGeneric = append(compiledFunction.ParameterIsGeneric, typemap.IsTypeParameter(v.Type()))
	}
	compiledFunction.ResultKinds = []isa.RegisterKind{isa.RegisterBool}

	upvalueMap = make(map[string]upvalueReference)
	uvIndex := 0

	compiledFunction.UpvalueDescriptors = append(compiledFunction.UpvalueDescriptors, program.UpvalueDescriptor{Index: stateFlagReg,
		Kind:    isa.RegisterInt,
		IsLocal: true, IsIndirect: false, OriginalKind: 0})
	stateFlagUVIndex = uvIndex
	uvIndex++

	stashUVIdxs = make([]int, len(stashRegs))
	for i, stash := range stashRegs {
		compiledFunction.UpvalueDescriptors = append(compiledFunction.UpvalueDescriptors, program.UpvalueDescriptor{Index: stash.Register,
			Kind:    stash.Kind,
			IsLocal: true, IsIndirect: false, OriginalKind: 0})
		stashUVIdxs[i] = uvIndex
		uvIndex++
	}

	c.buildFreeVarUpvalues(ctx, compiledFunction, freeVarNames, upvalueMap, uvIndex)

	return compiledFunction, stateFlagUVIndex, stashUVIdxs, upvalueMap
}

// isInsideLoop returns true if the Compiler is currently inside a loop body (for, range,
// etc.).
//
// Returns true when the Compiler is inside a loop body.
func (c *Compiler) isInsideLoop(_ context.Context) bool {
	for _, breakable := range slices.Backward(c.breakables) {
		if breakable.isLoop {
			return true
		}
	}
	return false
}

// collectOuterLabelTargets collects labelled outer loops for cross-closure break/continue
// from within a range-over-func body.
//
// Returns the list of outer label targets with assigned flag values.
func (c *Compiler) collectOuterLabelTargets(_ context.Context) []outerLabelTarget {
	var outerLabels []outerLabelTarget
	nextFlag := rangeOverFunctionFirstLabelFlag
	for i := range slices.Backward(c.breakables) {
		breakable := &c.breakables[i]
		if breakable.label == "" {
			continue
		}
		target := outerLabelTarget{label: breakable.label,
			breakFlag:      nextFlag,
			breakableIndex: i, continueFlag: 0}
		nextFlag++
		if breakable.isLoop {
			target.continueFlag = nextFlag
			nextFlag++
		}
		outerLabels = append(outerLabels, target)
	}
	return outerLabels
}

// compileYieldBody creates a sub-Compiler and compiles the range-over-func yield closure
// body, including parameter binding and implicit return true.
//
// Takes p (yieldBodyParams) which is the bundled parameters for the yield body
// compilation.
//
// Returns an error if compilation of the yield body fails.
func (c *Compiler) compileYieldBody(ctx context.Context, p yieldBodyParams) error {
	sub := newFunctionCompiler(ctx, c.programContext, p.compiledFunction, functionOptions{
		scopeName:         "<yield>",
		upvalues:          p.upvalueMap,
		substitutions:     c.typeSubstitutions,
		substitutionCache: c.typeSubstitutionsCache,
		rangeOverFunction: &rangeOverFunctionContext{
			stateFlagUpvalueIndex:     p.stateFlagUpvalueIndex,
			returnStashUpvalueIndices: p.stashUpvalueIndices,
			returnKinds:               p.returnKinds,
			outerLabels:               p.outerLabels,
		},
	})
	sub.Scopes.PushScope()

	if err := sub.declareYieldParams(ctx, p.statement, p.compiledFunction, p.numberOfYieldParameters); err != nil {
		return err
	}

	if _, err := sub.compileStmtList(ctx, p.statement.Body.List); err != nil {
		return fmt.Errorf("compiling range-over-func body: %w", err)
	}

	sub.emitYieldReturn(ctx, true)

	if err := sub.finishBody(ctx, p.compiledFunction, p.statement.Body, finishFlags{tailCall: false, finaliseDefer: false, classifyEscapes: false}); err != nil {
		return fmt.Errorf("compiling range-over-func body: %w", err)
	}
	return nil
}

// declareYieldParams declares yield parameters as key/value variables in the sub-Compiler
// scope, consuming register slots for unused params.
//
// Takes statement (*ast.RangeStmt) which is the range statement containing key/value
// identifiers.
// Takes compiledFunction (*compiledFunction) which is the compiled function whose
// parameter kinds are used.
// Takes numYieldParams (int) which is how many yield parameters to declare.
//
// Returns an error if a key or value expression is not an identifier.
func (c *Compiler) declareYieldParams(_ context.Context, statement *ast.RangeStmt, compiledFunction *program.CompiledFunction, numYieldParams int) error {
	hasKey := statement.Key != nil && !isBlankIdent(statement.Key)
	hasValue := statement.Value != nil && !isBlankIdent(statement.Value)

	if numYieldParams >= 1 {
		parameterKind := compiledFunction.ParameterKinds[0]
		if hasKey {
			keyIdent, ok := statement.Key.(*ast.Ident)
			if !ok {
				return fmt.Errorf("range key is not an identifier: %T", statement.Key)
			}
			c.Scopes.DeclareVar(keyIdent.Name, parameterKind)
		} else {
			c.Scopes.Alloc.Alloc(parameterKind)
		}
	}
	if numYieldParams >= 2 {
		parameterKind := compiledFunction.ParameterKinds[1]
		if hasValue {
			valueIdentifier, ok := statement.Value.(*ast.Ident)
			if !ok {
				return fmt.Errorf("range value is not an identifier: %T", statement.Value)
			}
			c.Scopes.DeclareVar(valueIdentifier.Name, parameterKind)
		} else {
			c.Scopes.Alloc.Alloc(parameterKind)
		}
	}
	return nil
}

// emitIteratorCall emits isa.OpMakeClosure for the yield callback and calls the iterator
// with it as the sole argument.
//
// Takes iterLocation (VarLocation) which is the register location of the iterator
// function.
// Takes functionIndex (uint16) which is the function table index of the yield closure.
//
// Returns the yield register for subsequent upvalue sync.
func (c *Compiler) emitIteratorCall(_ context.Context, iterLocation program.VarLocation, functionIndex uint16) uint8 {
	yieldReg := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpMakeClosure, yieldReg, functionIndex)

	site := program.CallSite{
		IsNative:       true,
		NativeRegister: iterLocation.Register,
		Arguments:      []program.VarLocation{{Register: yieldReg, Kind: isa.RegisterGeneral}},
	}
	siteIndex, err := program.AddCallSite(c.Function, &site)
	if err != nil {
		c.recordStickyError(err)
		return yieldReg
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	return yieldReg
}

// emitReturnStashDispatch checks the state flag for a pending return (flag == 2) and, if
// set, moves stashed values to return positions and emits opReturn.
//
// Takes stateFlagReg (uint8) which holds the state flag.
// Takes stashRegs ([]VarLocation) which hold the stashed return values.
// Takes returnKinds ([]isa.RegisterKind) which are the return-value banks.
// Takes bodyReturns (bool) which is true when the body returns from the enclosing
// function.
func (c *Compiler) emitReturnStashDispatch(ctx context.Context, stateFlagReg uint8, stashRegs []program.VarLocation, returnKinds []isa.RegisterKind, bodyReturns bool) {
	if len(returnKinds) == 0 && !bodyReturns {
		return
	}

	returnPendingIndex, err := program.AddIntConstant(c.Function, rangeOverFunctionReturnPendingFlag)
	if err != nil {
		c.recordStickyError(err)
		return
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, temporaryRegister, returnPendingIndex)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.Emit(c.Function, isa.OpEqInt, comparisonRegister, stateFlagReg, temporaryRegister)
	jumpSkip := program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, temporaryRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, comparisonRegister)

	var bankCounters [isa.NumRegisterKinds]uint8
	for i, kind := range returnKinds {
		destinationRegister := bankCounters[kind]
		bankCounters[kind]++
		dest := program.VarLocation{Register: destinationRegister, Kind: kind}
		c.emitMove(ctx, dest, stashRegs[i])
	}
	for k := range bankCounters {
		if bankCounters[k] > 0 {
			c.Scopes.Alloc.EnsureMin(isa.RegisterKind(k), uint32(bankCounters[k]))
		}
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), safeconv.MustIntToUint8(len(returnKinds)))

	program.PatchJump(c.Function, jumpSkip)
}

// rangeBodyReturns reports whether the range-over-func body returns from the enclosing
// function. Returns inside a nested function literal belong to that literal and do not
// count.
//
// Takes statement (*ast.RangeStmt) which is the range-over-func loop.
//
// Returns whether the body contains a return of its own.
func rangeBodyReturns(statement *ast.RangeStmt) bool {
	if statement.Body == nil {
		return false
	}
	found := false
	ast.Inspect(statement.Body, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		if _, isFunctionLiteral := node.(*ast.FuncLit); isFunctionLiteral {
			return false
		}
		if _, isReturn := node.(*ast.ReturnStmt); isReturn {
			found = true
			return false
		}
		return true
	})
	return found
}

// emitOuterLabelDispatch emits state flag checks and jumps for labelled break/continue
// targeting outer loops from within a range-over-func body.
//
// Takes stateFlagReg (uint8) which is the register holding the state flag.
// Takes outerLabels ([]outerLabelTarget) which is the labelled outer loop targets to
// dispatch to.
func (c *Compiler) emitOuterLabelDispatch(ctx context.Context, stateFlagReg uint8, outerLabels []outerLabelTarget) {
	for _, ol := range outerLabels {
		c.emitFlagBreakDispatch(ctx, stateFlagReg, ol)
		c.emitFlagContinueDispatch(ctx, stateFlagReg, ol)
	}
}

// emitFlagBreakDispatch emits a state flag check and break jump for a single labelled
// outer loop target.
//
// Takes stateFlagReg (uint8) which is the register holding the state flag.
// Takes ol (outerLabelTarget) which is the outer label target containing the break flag
// value and breakable index.
func (c *Compiler) emitFlagBreakDispatch(_ context.Context, stateFlagReg uint8, ol outerLabelTarget) {
	flagIndex, err := program.AddIntConstant(c.Function, ol.breakFlag)
	if err != nil {
		c.recordStickyError(err)
		return
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, temporaryRegister, flagIndex)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.Emit(c.Function, isa.OpEqInt, comparisonRegister, stateFlagReg, temporaryRegister)
	jumpSkip := program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, temporaryRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, comparisonRegister)

	jumpPC := program.EmitTier1Jump(c.Function)
	c.breakables[ol.breakableIndex].breakJumps = append(
		c.breakables[ol.breakableIndex].breakJumps, jumpPC)
	program.PatchJump(c.Function, jumpSkip)
}

// emitFlagContinueDispatch emits a state flag check and continue jump for a single
// labelled outer loop target, if it is a loop.
//
// Takes stateFlagReg (uint8) which is the register holding the state flag.
// Takes ol (outerLabelTarget) which is the outer label target containing the continue
// flag value and breakable index.
func (c *Compiler) emitFlagContinueDispatch(_ context.Context, stateFlagReg uint8, ol outerLabelTarget) {
	if ol.continueFlag == 0 {
		return
	}

	flagIdx2, err := program.AddIntConstant(c.Function, ol.continueFlag)
	if err != nil {
		c.recordStickyError(err)
		return
	}
	tmpReg2 := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, tmpReg2, flagIdx2)
	cmpReg2 := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.Emit(c.Function, isa.OpEqInt, cmpReg2, stateFlagReg, tmpReg2)
	jumpSkip2 := program.EmitJump(c.Function, isa.OpJumpIfFalse, cmpReg2)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, tmpReg2)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, cmpReg2)

	jumpPC2 := program.EmitTier1Jump(c.Function)
	c.breakables[ol.breakableIndex].continueJumps = append(
		c.breakables[ol.breakableIndex].continueJumps, jumpPC2)
	program.PatchJump(c.Function, jumpSkip2)
}

// emitYieldReturn emits instructions to return a boolean value from a range-over-func
// yield callback. Used for implicit end-of-body (true), break (false), continue (true),
// and return (false).
//
// Takes value (bool) which is the boolean to return: true continues iteration, false
// stops it.
func (c *Compiler) emitYieldReturn(ctx context.Context, value bool) {
	boolInt := uint8(0)
	if value {
		boolInt = 1
	}
	intRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadBool), intRegister, boolInt)
	source := program.VarLocation{Register: intRegister, Kind: isa.RegisterInt}
	dest := program.VarLocation{Register: 0, Kind: isa.RegisterBool}
	c.emitMove(ctx, dest, source)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, intRegister)
	c.Scopes.Alloc.EnsureMin(isa.RegisterBool, 1)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
}

// emitIndirectWrite emits instructions to write a value through a heap-escaped pointer
// variable.
//
// Takes dest (VarLocation) which is the indirect destination location.
// Takes source (VarLocation) which is the source value location to write.
func (c *Compiler) emitIndirectWrite(ctx context.Context, dest program.VarLocation, source program.VarLocation) {
	var generalSource program.VarLocation
	if source.Kind == isa.RegisterGeneral {
		generalSource = source
	} else {
		tempGen := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
		c.emitBoxToGeneral(ctx, tempGen, source)
		generalSource = program.VarLocation{Register: tempGen, Kind: isa.RegisterGeneral}
	}
	program.Emit(c.Function, isa.OpSetField, dest.Register, isa.SentinelFieldDeref, generalSource.Register)
	if generalSource.Register != source.Register {
		c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalSource.Register)
	}
}

// emitMoveToRegisterZero moves a value to register 0 in its bank, placing the result in
// the canonical return position.
//
// Takes location (VarLocation) which is the value to move to register zero.
func (c *Compiler) emitMoveToRegisterZero(_ context.Context, location program.VarLocation) {
	if location.Register == 0 {
		return
	}
	switch location.Kind {
	case isa.RegisterInt:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt), 0, location.Register)
	case isa.RegisterFloat:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveFloat), 0, location.Register)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveString), 0, location.Register)
	case isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpMoveGeneral, 0, location.Register, generalMoveModeFor(nil))
	case isa.RegisterBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveBool), 0, location.Register)
	case isa.RegisterUint:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveUint), 0, location.Register)
	case isa.RegisterComplex:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveComplex), 0, location.Register)
	default:
	}
}

// emitMove emits a move from source to dest with a nil source type. Prefer emitMoveTyped
// when the source's static type is available.
//
// Takes dest (VarLocation) which is the destination.
// Takes source (VarLocation) which is the source.
func (c *Compiler) emitMove(ctx context.Context, dest, source program.VarLocation) {
	c.emitMoveTyped(ctx, dest, source, nil)
}

// emitMoveTyped is emitMove with the source operand's static type threaded through, so
// general-bank moves can pick the alias-safe or always-snapshot mode at compile time and
// elide the runtime kind switch in handleMoveGeneral.
//
// Takes dest (VarLocation) which is the destination register location.
// Takes source (VarLocation) which is the source register location.
// Takes sourceType (types.Type) which is the source operand's static type. May be nil
// when not available; the resulting move falls back to the conservative
// dynamic-kind-switch path.
func (c *Compiler) emitMoveTyped(ctx context.Context, dest, source program.VarLocation, sourceType types.Type) {
	if dest.IsIndirect {
		c.emitIndirectWrite(ctx, dest, source)
		return
	}

	if dest.IsSpilled {
		if source.IsSpilled {
			source = c.emitReloadIfSpilled(ctx, source)
			defer c.Scopes.Alloc.FreeTemp(source.Kind, source.Register)
		}
		if source.Kind == dest.Kind {
			c.emitSpillStore(ctx, source.Register, dest.Kind, dest.SpillSlot)
		} else {
			scratch := c.Scopes.Alloc.AllocTemp(dest.Kind)
			nonSpilledDest := program.VarLocation{Register: scratch, Kind: dest.Kind}
			c.emitCrossBankMove(ctx, nonSpilledDest, source)
			c.emitSpillStore(ctx, scratch, dest.Kind, dest.SpillSlot)
			c.Scopes.Alloc.FreeTemp(dest.Kind, scratch)
		}
		return
	}

	if source.IsSpilled {
		source = c.emitReloadIfSpilled(ctx, source)
		defer c.Scopes.Alloc.FreeTemp(source.Kind, source.Register)
	}

	if dest.Kind == source.Kind && dest.Register == source.Register {
		return
	}

	if dest.Kind == source.Kind {
		c.emitSameKindMoveTyped(ctx, dest, source, sourceType)
		return
	}

	c.emitCrossBankMove(ctx, dest, source)
}

// emitWriteSharedCellIfCaptured emits isa.SubOpWriteSharedCell if the destination
// variable has been captured by a closure, keeping the upvalue cell in sync with the
// register. Suppressed inside for-loop post statements where per-iteration scoping causes
// the post to mutate a fresh per-iteration copy rather than the captured cell.
//
// Takes dest (VarLocation) which is the destination variable location to synchronise.
func (c *Compiler) emitWriteSharedCellIfCaptured(_ context.Context, dest program.VarLocation) {
	if !dest.IsCaptured {
		return
	}
	if dest.IsIndirect {
		return
	}
	if c.loopPost != nil {
		if _, declaredInInit := c.loopPost.initDecls[loopPostDeclKey{kind: dest.Kind, register: dest.Register}]; declaredInInit {
			return
		}
	}
	program.EmitTier1(c.Function, isa.SubOpWriteSharedCell, dest.Register, uint8(dest.Kind))
}

// emitSameKindMoveTyped emits a move instruction between registers of the same kind,
// threading the source operand's static type so general-bank moves can elide the runtime
// kind switch when the static type guarantees alias-safety or always-snapshot semantics.
//
// Takes dest (VarLocation) which is the destination register location.
// Takes source (VarLocation) which is the source register location of the same kind.
// Takes sourceType (types.Type) which is the source operand's static type. May be nil;
// falls through to the conservative dynamic mode.
func (c *Compiler) emitSameKindMoveTyped(_ context.Context, dest, source program.VarLocation, sourceType types.Type) {
	switch dest.Kind {
	case isa.RegisterInt:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt), dest.Register, source.Register)
	case isa.RegisterFloat:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveFloat), dest.Register, source.Register)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveString), dest.Register, source.Register)
	case isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpMoveGeneral, dest.Register, source.Register, generalMoveModeFor(sourceType))
	case isa.RegisterBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveBool), dest.Register, source.Register)
	case isa.RegisterUint:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveUint), dest.Register, source.Register)
	case isa.RegisterComplex:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveComplex), dest.Register, source.Register)
	case isa.RegisterSliceInt:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceInt), dest.Register, source.Register)
	case isa.RegisterSliceFloat:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceFloat), dest.Register, source.Register)
	case isa.RegisterSliceString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceString), dest.Register, source.Register)
	case isa.RegisterSliceBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceBool), dest.Register, source.Register)
	case isa.RegisterSliceUint:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceUint), dest.Register, source.Register)
	case isa.RegisterSliceByte:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceByte), dest.Register, source.Register)
	default:
	}
}

// emitCrossBankMove emits a conversion instruction between registers of different kinds
// (e.g. int to float, typed to general).
//
// Takes dest (VarLocation) which is the destination register location.
// Takes source (VarLocation) which is the source register location of a different kind.
func (c *Compiler) emitCrossBankMove(_ context.Context, dest, source program.VarLocation) {
	if subOp, ok := isaselect.ScalarCrossBankSubOp(source.Kind, dest.Kind); ok {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), dest.Register, source.Register)
		return
	}
	switch {
	case source.Kind == isa.RegisterGeneral && isa.IsTypedSliceKind(dest.Kind):
		adopt, _ := inline.CrossBankAdoptOrBoxSubOp(isa.RegisterGeneral, dest.Kind)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(adopt), dest.Register, source.Register)
	case dest.Kind == isa.RegisterGeneral && isa.IsTypedSliceKind(source.Kind):

		if c.boxNeedsPreciseType(source) && c.emitTypedBox(dest.Register, source) {
			return
		}
		box, _ := inline.CrossBankAdoptOrBoxSubOp(source.Kind, isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(box), dest.Register, source.Register)
	case dest.Kind == isa.RegisterGeneral:
		if !c.emitTypedBox(dest.Register, source) {
			program.Emit(c.Function, isa.OpPackInterface, dest.Register, source.Register, uint8(source.Kind))
		}
	case source.Kind == isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpUnpackInterface, dest.Register, source.Register, uint8(dest.Kind))
	}
}

// compileGo compiles a go statement (go func()...).
//
// Takes statement (*ast.GoStmt) which is the go statement AST node to compile.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileGo(ctx context.Context, statement *ast.GoStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureGoroutines, statement.Go); err != nil {
		return program.VarLocation{}, err
	}
	callExpression := statement.Call
	if builtin, isBuiltin := c.deferrableBuiltin(callExpression); isBuiltin {
		return c.compileBuiltinStatementCall(ctx, callExpression, builtin, isa.OpGo, engine.GoModeBuiltin)
	}

	functionLocation, err := c.compileExpression(ctx, callExpression.Fun)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &functionLocation)

	argumentCount := len(callExpression.Args)
	argumentLocations := make([]program.VarLocation, argumentCount)
	for i, argument := range callExpression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}
		argumentLocations[i] = location
	}

	program.Emit(c.Function, isa.OpGo, functionLocation.Register, safeconv.MustIntToUint8(argumentCount), 0)

	for _, location := range argumentLocations {
		program.Emit(c.Function, isa.OpExt, 0, location.Register, uint8(location.Kind))
	}

	return program.VarLocation{}, nil
}

// compileSelect compiles a select statement.
//
// Takes statement (*ast.SelectStmt) which is the select statement AST node to compile.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileSelect(ctx context.Context, statement *ast.SelectStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureChannels, statement.Select); err != nil {
		return program.VarLocation{}, err
	}
	clauses := statement.Body.List
	numCases := len(clauses)

	compiled, err := c.compileSelectCases(ctx, clauses)
	if err != nil {
		return program.VarLocation{}, err
	}

	return c.emitSelectDispatch(ctx, clauses, compiled, numCases)
}

// compileSelectCases pre-compiles channel and value expressions for all cases in a select
// statement.
//
// Takes clauses ([]ast.Stmt) which is the list of select case statements to pre-compile.
//
// Returns the compiled select cases and any compilation error.
func (c *Compiler) compileSelectCases(ctx context.Context, clauses []ast.Stmt) ([]selectCase, error) {
	compiled := make([]selectCase, len(clauses))

	for i, clause := range clauses {
		cc, ok := clause.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("select clause is not a CommClause: %T", clause)
		}
		if cc.Comm == nil {
			compiled[i] = selectCase{valueTarget: nil, okTarget: nil,
				direction:           isa.SelectDirectionDefault,
				assignmentName:      "",
				okName:              "",
				existingLocation:    program.VarLocation{},
				valueKind:           0,
				channelRegister:     0,
				valueRegister:       0,
				destinationRegister: 0,
				destinationKind:     0,
				okRegister:          0,
				hasOk:               false,
				assignmentKind:      0,
				isDefine:            false,
			}
			continue
		}

		sc, err := c.compileSelectComm(ctx, cc.Comm)
		if err != nil {
			return nil, err
		}
		compiled[i] = sc
	}
	return compiled, nil
}

// compileSelectComm compiles a single select communication clause (send, receive-discard,
// or receive-assign).
//
// Takes comm (ast.Stmt) which is the communication statement to compile.
//
// Returns the compiled select case and any compilation error.
func (c *Compiler) compileSelectComm(ctx context.Context, comm ast.Stmt) (selectCase, error) {
	switch comm := comm.(type) {
	case *ast.SendStmt:
		return c.compileSelectSend(ctx, comm)
	case *ast.ExprStmt:
		return c.compileSelectRecvDiscard(ctx, comm)
	case *ast.AssignStmt:
		return c.compileSelectRecvAssign(ctx, comm)
	default:
		return selectCase{}, fmt.Errorf("unsupported select comm type: %T at %s", comm, c.positionString(comm.Pos()))
	}
}

// compileSelectSend compiles a send case in a select statement (ch <- v).
//
// Takes comm (*ast.SendStmt) which is the send statement AST node to compile as a select
// send case.
//
// Returns the compiled select case and any compilation error.
func (c *Compiler) compileSelectSend(ctx context.Context, comm *ast.SendStmt) (selectCase, error) {
	channelLocation, err := c.compileExpression(ctx, comm.Chan)
	if err != nil {
		return selectCase{}, err
	}
	c.boxToGeneral(ctx, &channelLocation)
	valueLocation, err := c.compileExpression(ctx, comm.Value)
	if err != nil {
		return selectCase{}, err
	}

	if c.channelElementIsInterface(comm.Chan) {
		c.boxElementToGeneral(ctx, &valueLocation)
	}
	return selectCase{valueTarget: nil, okTarget: nil, direction: isa.SelectDirectionSend,
		channelRegister:     channelLocation.Register,
		valueRegister:       valueLocation.Register,
		valueKind:           valueLocation.Kind,
		assignmentName:      "",
		okName:              "",
		existingLocation:    program.VarLocation{},
		destinationRegister: 0,
		destinationKind:     0,
		okRegister:          0,
		hasOk:               false,
		assignmentKind:      0,
		isDefine:            false}, nil
}

// compileSelectRecvDiscard compiles a receive-and-discard case (<-ch).
//
// Takes comm (*ast.ExprStmt) which is the expression statement AST node containing the
// receive operation.
//
// Returns the compiled select case and any compilation error.
func (c *Compiler) compileSelectRecvDiscard(ctx context.Context, comm *ast.ExprStmt) (selectCase, error) {
	unary, ok := comm.X.(*ast.UnaryExpr)
	if !ok {
		return selectCase{}, fmt.Errorf("select recv expression is not a unary expression: %T", comm.X)
	}
	channelLocation, err := c.compileExpression(ctx, unary.X)
	if err != nil {
		return selectCase{}, err
	}
	c.boxToGeneral(ctx, &channelLocation)
	throwaway := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	return selectCase{valueTarget: nil, okTarget: nil, direction: isa.SelectDirectionReceive,
		channelRegister:     channelLocation.Register,
		destinationRegister: throwaway,
		destinationKind:     isa.RegisterGeneral,
		assignmentName:      "",
		okName:              "",
		existingLocation:    program.VarLocation{},
		valueKind:           0,
		valueRegister:       0,
		okRegister:          0,
		hasOk:               false,
		assignmentKind:      0,
		isDefine:            false}, nil
}

// compileSelectRecvAssign compiles a receive-with-assignment case (v := <-ch).
//
// Takes comm (*ast.AssignStmt) which is the assignment statement AST node containing the
// receive operation.
//
// Returns the compiled select case and any compilation error.
func (c *Compiler) compileSelectRecvAssign(ctx context.Context, comm *ast.AssignStmt) (selectCase, error) {
	unary, ok := ast.Unparen(comm.Rhs[0]).(*ast.UnaryExpr)
	if !ok {
		return selectCase{}, fmt.Errorf("select recv assign RHS is not a unary expression: %T", comm.Rhs[0])
	}
	channelLocation, err := c.compileExpression(ctx, unary.X)
	if err != nil {
		return selectCase{}, err
	}
	c.boxToGeneral(ctx, &channelLocation)

	receiverKind := isa.RegisterGeneral
	if tv, ok := c.Info.Types[unary]; ok {
		receiverKind = c.kindFor(tv.Type)
	}
	destinationRegister := c.Scopes.Alloc.Alloc(receiverKind)
	assignName := ""
	if identifier, ok := comm.Lhs[0].(*ast.Ident); ok {
		assignName = identifier.Name
	}
	sc := selectCase{valueTarget: nil, okTarget: nil, direction: isa.SelectDirectionReceive,
		channelRegister:     channelLocation.Register,
		destinationRegister: destinationRegister,
		destinationKind:     receiverKind,
		assignmentName:      assignName,
		assignmentKind:      receiverKind,
		isDefine:            comm.Tok == token.DEFINE, okName: "", existingLocation: program.VarLocation{}, valueKind: 0, valueRegister: 0, okRegister: 0, hasOk: false}
	if len(comm.Lhs) == 2 {
		sc.hasOk = true
		sc.okRegister = c.Scopes.Alloc.Alloc(isa.RegisterInt)
		okIdent, isIdent := comm.Lhs[1].(*ast.Ident)
		switch {
		case sc.isDefine && !isIdent:
			return selectCase{}, fmt.Errorf("select recv assign: second LHS is not an identifier at %s", c.positionString(comm.Pos()))
		case sc.isDefine:
			if okIdent.Name != typemap.BlankIdentName {
				sc.okName = okIdent.Name
			}
		case !isBlankIdent(comm.Lhs[1]):
			sc.okTarget = comm.Lhs[1]
		}
	}
	if sc.isDefine {
		if assignName == "" {
			return selectCase{}, fmt.Errorf("select recv define: LHS is not an identifier at %s", c.positionString(comm.Pos()))
		}
		return sc, nil
	}

	sc.assignmentName = ""
	if !isBlankIdent(comm.Lhs[0]) {
		sc.valueTarget = comm.Lhs[0]
	}
	return sc, nil
}

// emitSelectDispatch emits isa.SubOpSelect with extension words, then compiles dispatch
// jumps and case bodies.
//
// Takes clauses ([]ast.Stmt) which is the list of select case statements.
// Takes compiled ([]selectCase) which is the pre-compiled select cases.
// Takes numCases (int) which is the total number of cases.
//
// Returns an empty location and any compilation error.
func (c *Compiler) emitSelectDispatch(ctx context.Context, clauses []ast.Stmt, compiled []selectCase, numCases int) (program.VarLocation, error) {
	chosenRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.EmitTier1(c.Function, isa.SubOpSelect, safeconv.MustIntToUint8(numCases), chosenRegister)

	c.emitSelectExtWords(ctx, compiled)

	caseJumps := c.emitSelectCaseJumps(ctx, chosenRegister, numCases)

	endJumpIndex := len(c.Function.Body)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), 0, 0)

	c.breakables = append(c.breakables, breakableContext{
		isLoop: false,
		label:  c.consumePendingLabel(ctx),
	})
	bodyEnds, err := c.compileSelectBodies(ctx, clauses, compiled, caseJumps)
	if err != nil {
		return program.VarLocation{}, err
	}

	endOffset := safeconv.MustIntToUint16(len(c.Function.Body) - endJumpIndex - 1)
	lo, hi := isa.SplitWide(endOffset)
	c.Function.Body[endJumpIndex].B = lo
	c.Function.Body[endJumpIndex].C = hi

	for _, index := range bodyEnds {
		fwdOffset := safeconv.MustIntToUint16(len(c.Function.Body) - index - 1)
		bLo, bHi := isa.SplitWide(fwdOffset)
		c.Function.Body[index].B = bLo
		c.Function.Body[index].C = bHi
	}
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.breakJumps {
		program.PatchJump(c.Function, pc)
	}
	c.breakables = c.breakables[:len(c.breakables)-1]

	return program.VarLocation{}, nil
}

// emitSelectExtWords emits isa.OpExt extension words for each select case, encoding
// direction, channel, and value/dest registers.
//
// Takes compiled ([]selectCase) which is the pre-compiled select cases to emit extension
// words for.
func (c *Compiler) emitSelectExtWords(_ context.Context, compiled []selectCase) {
	for _, sc := range compiled {
		switch sc.direction {
		case isa.SelectDirectionReceive:
			okFlag := uint8(0)
			if sc.hasOk {
				okFlag = 1
			}
			program.Emit(c.Function, isa.OpExt, isa.SelectDirectionReceive, sc.channelRegister, okFlag)
			program.Emit(c.Function, isa.OpExt, sc.destinationRegister, uint8(sc.destinationKind), sc.okRegister)
		case isa.SelectDirectionSend:
			program.Emit(c.Function, isa.OpExt, isa.SelectDirectionSend, sc.channelRegister, 0)
			program.Emit(c.Function, isa.OpExt, sc.valueRegister, uint8(sc.valueKind), 0)
		case isa.SelectDirectionDefault:
			program.Emit(c.Function, isa.OpExt, isa.SelectDirectionDefault, 0, 0)
		}
	}
}

// emitSelectCaseJumps emits conditional jumps for dispatching to individual select case
// bodies based on the chosen index.
//
// Takes chosenRegister (uint8) which is the register holding the chosen case index.
// Takes numCases (int) which is the total number of cases.
//
// Returns the list of jump instruction PCs for patching case targets.
func (c *Compiler) emitSelectCaseJumps(_ context.Context, chosenRegister uint8, numCases int) []int {
	caseJumps := make([]int, numCases)
	temporaryRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	comparisonRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	for i := range numCases {
		index, err := program.AddIntConstant(c.Function, int64(i))
		if err != nil {
			c.recordStickyError(err)
			return caseJumps
		}
		program.EmitWide(c.Function, isa.OpLoadIntConst, temporaryRegister, index)
		program.Emit(c.Function, isa.OpEqInt, comparisonRegister, chosenRegister, temporaryRegister)
		caseJumps[i] = len(c.Function.Body)
		program.Emit(c.Function, isa.OpJumpIfTrue, comparisonRegister, 0, 0)
	}
	return caseJumps
}

// compileSelectBodies compiles each select case body, patches case jump targets, and
// returns the list of body-end jump PCs.
//
// Takes clauses ([]ast.Stmt) which is the list of select case statements.
// Takes compiled ([]selectCase) which is the pre-compiled select cases.
// Takes caseJumps ([]int) which is the jump instruction PCs to patch.
//
// Returns the list of body-end jump PCs and any compilation error.
func (c *Compiler) compileSelectBodies(ctx context.Context, clauses []ast.Stmt, compiled []selectCase, caseJumps []int) ([]int, error) {
	bodyEnds := make([]int, 0, len(clauses))
	for i, clause := range clauses {
		cc, ok := clause.(*ast.CommClause)
		if !ok {
			return nil, fmt.Errorf("select clause is not a CommClause: %T", clause)
		}
		program.PatchJump(c.Function, caseJumps[i])
		c.Scopes.PushScope()
		if err := c.bindSelectCaseVariables(ctx, compiled[i]); err != nil {
			return nil, err
		}
		if err := c.compileSelectCaseBody(ctx, cc.Body); err != nil {
			return nil, err
		}
		c.Scopes.PopScope()
		bodyEnds = append(bodyEnds, len(c.Function.Body))
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), 0, 0)
	}
	return bodyEnds, nil
}

// bindSelectCaseVariables binds the case's received value (when the direction is receive
// and the destination is not the blank identifier) and the optional ok flag into the
// current scope.
//
// Takes selected (selectCase) which is the select case whose receive bindings to install.
//
// Returns error when the target cannot be assigned.
func (c *Compiler) bindSelectCaseVariables(ctx context.Context, selected selectCase) error {
	if selected.direction != isa.SelectDirectionReceive {
		return nil
	}
	location := program.VarLocation{Register: selected.destinationRegister, Kind: selected.assignmentKind}
	if selected.isDefine && selected.assignmentName != "" && selected.assignmentName != typemap.BlankIdentName {
		c.Scopes.Scopes[len(c.Scopes.Scopes)-1].Vars[selected.assignmentName] = location
	}
	if selected.valueTarget != nil {
		if _, err := c.emitAssignTarget(ctx, selected.valueTarget, location); err != nil {
			return err
		}
	}
	if !selected.hasOk {
		return nil
	}

	okLocation := program.VarLocation{Register: c.Scopes.Alloc.Alloc(isa.RegisterBool), Kind: isa.RegisterBool}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), okLocation.Register, selected.okRegister)
	if selected.okName != "" {
		c.Scopes.Scopes[len(c.Scopes.Scopes)-1].Vars[selected.okName] = okLocation
	}
	if selected.okTarget != nil {
		if _, err := c.emitAssignTarget(ctx, selected.okTarget, okLocation); err != nil {
			return err
		}
	}
	return nil
}

// compileSelectCaseBody compiles each statement of a select case in sequence, surfacing
// the first compilation error encountered.
//
// Takes body ([]ast.Stmt) which are the statements forming the select case body.
//
// Returns the first compilation error encountered, or nil on success.
func (c *Compiler) compileSelectCaseBody(ctx context.Context, body []ast.Stmt) error {
	for _, bodyStmt := range body {
		if _, err := c.compileStmt(ctx, bodyStmt); err != nil {
			return err
		}
	}
	return nil
}

// compileSend compiles a channel send statement (ch <- v).
//
// Takes statement (*ast.SendStmt) which is the send statement AST node to compile.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileSend(ctx context.Context, statement *ast.SendStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureChannels, statement.Arrow); err != nil {
		return program.VarLocation{}, err
	}
	channelLocation, err := c.compileExpression(ctx, statement.Chan)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &channelLocation)

	valueLocation, err := c.compileExpression(ctx, statement.Value)
	if err != nil {
		return program.VarLocation{}, err
	}
	valueLocation = c.coerceEvalBoolResult(ctx, c.Info, statement.Value, valueLocation)

	if c.channelElementIsInterface(statement.Chan) {
		c.boxElementToGeneral(ctx, &valueLocation)
	}

	program.Emit(c.Function, isa.OpChannelSend, channelLocation.Register, valueLocation.Register, uint8(valueLocation.Kind))

	return program.VarLocation{}, nil
}

// channelElementIsInterface reports whether the channel expression's element type is an
// interface, so a scalar send must be boxed with its exact source type rather than left
// to the runtime's canonical bank conversion.
//
// Takes channelExpression (ast.Expr) which is the channel operand of the send.
//
// Returns true when the statically-known channel element type is an interface.
func (c *Compiler) channelElementIsInterface(channelExpression ast.Expr) bool {
	tv, ok := c.Info.Types[channelExpression]
	if !ok || tv.Type == nil {
		return false
	}
	channelType, isChannel := c.substitutedType(tv.Type).Underlying().(*types.Chan)
	if !isChannel {
		return false
	}
	_, isInterface := channelType.Elem().Underlying().(*types.Interface)
	return isInterface
}

// emitReloadIfSpilled ensures a VarLocation refers to a directly-addressable register.
//
// Takes location (VarLocation) which is the variable location to reload when it was
// spilled.
//
// Returns a VarLocation with a directly-addressable register.
func (c *Compiler) emitReloadIfSpilled(_ context.Context, location program.VarLocation) program.VarLocation {
	if !location.IsSpilled {
		return location
	}
	scratch := c.Scopes.Alloc.AllocTemp(location.Kind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpReload), scratch, uint8(location.Kind))
	program.EmitExtension(c.Function, location.SpillSlot, 0)
	return program.VarLocation{Register: scratch, Kind: location.Kind}
}

// emitSpillStore stores a value from a directly-addressable register into a spill slot.
//
// Takes sourceRegister (uint8) which is the source register to spill.
// Takes kind (isa.RegisterKind) which is the kind of the register.
// Takes slot (uint16) which is the spill slot index.
func (c *Compiler) emitSpillStore(_ context.Context, sourceRegister uint8, kind isa.RegisterKind, slot uint16) {
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpSpill), sourceRegister, uint8(kind))
	program.EmitExtension(c.Function, slot, 0)
}

// generalMoveModeFor maps the source operand's static type to the C operand for
// isa.OpMoveGeneral. Sites without static type info pass nil and get the conservative
// dynamic mode that preserves existing behaviour.
//
// Takes sourceType (types.Type) which is the source operand's static type, or nil.
//
// Returns the byte to encode in instruction.c for isa.OpMoveGeneral. snapshotDynamic is
// the conservative zero value the default returns, which is what a site without static
// type information asks for.
func generalMoveModeFor(sourceType types.Type) uint8 {
	switch snapshotModeFor(sourceType) {
	case snapshotNever:
		return engine.MoveGeneralModeAlias
	case snapshotAlways:
		return engine.MoveGeneralModeSnapshot
	default:
		return engine.MoveGeneralModeDynamic
	}
}

// isIntegerBasicKind returns true if the given basic type kind is an integer type
// (signed, unsigned, or untyped int/rune).
//
// Takes k (types.BasicKind) which is the basic type kind to check.
//
// Returns true if k is any integer kind, false otherwise.
func isIntegerBasicKind(k types.BasicKind) bool {
	switch k {
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.UntypedInt, types.UntypedRune,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
		return true
	default:
	}
	return false
}

// isBlankIdent returns true if the expression is the blank identifier _.
//
// Takes expression (ast.Expr) which is the expression to check.
//
// Returns true if the expression is an identifier named "_".
func isBlankIdent(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == typemap.BlankIdentName
}

// bodyContainsFunctionLit reports whether the given block statement contains any function
// literal (closure).
//
// Takes body (*ast.BlockStmt) which is the block statement to inspect for function
// literals.
//
// Returns true if a function literal is found in the block.
func bodyContainsFunctionLit(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			found = true
			return false
		}
		return !found
	})
	return found
}
