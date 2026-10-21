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
	"slices"

	"pipit.sh/pipit/internal/compile/appendlower"
	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
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

	// gotoExits receives the gotos that leave the body.
	gotoExits *rangeOverFunctionGotoExits

	// ownLabel is the range statement's label, or "".
	ownLabel string

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

// compileRangeOverFunction compiles a range-over-func loop (Go 1.23+).
//
// The loop body is wrapped in a yield callback closure that the iterator calls to deliver
// values. Outer scaffolding initialises a state flag, constructs the yield closure, calls
// the iterator with it, syncs upvalues, and dispatches stashed return values when the
// flag indicates a pending return. The yield body receives params as key/value, runs the
// body, then returns true to continue. break sets flag=1 and returns false, continue
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
	ownLabel := c.consumePendingLabel(ctx)
	yieldSignature, err := c.rangeYieldSignature(statement, iteratorSignature)
	if err != nil {
		return program.VarLocation{}, err
	}
	numYieldParams := yieldSignature.Params().Len()

	stateFlagReg := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	zeroIndex, err := program.AddIntConstant(c.Function, 0)
	if err != nil {
		return program.VarLocation{}, err
	}
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, stateFlagReg, zeroIndex)

	stashRegs, returnKinds := c.allocReturnStash(ctx)
	freeVarNames := c.collectRangeBodyFreeVars(ctx, statement)

	compiledFunction, stateFlagUVIndex, stashUVIdxs, upvalueMap := c.buildYieldClosure(ctx,
		yieldSignature, stateFlagReg, stashRegs, freeVarNames,
	)
	if err := c.checkYieldClosureOperands(statement, compiledFunction, returnKinds); err != nil {
		return program.VarLocation{}, err
	}

	functionIndex, err := c.registerCreatedFunction(compiledFunction)
	if err != nil {
		return program.VarLocation{}, err
	}

	outerLabels, nextFlag := c.collectOuterLabelTargets(ctx)
	gotoExits := &rangeOverFunctionGotoExits{targets: nil, firstFlag: nextFlag}

	if err := c.compileYieldBody(ctx, yieldBodyParams{
		statement:               statement,
		compiledFunction:        compiledFunction,
		numberOfYieldParameters: numYieldParams,
		upvalueMap:              upvalueMap,
		returnKinds:             returnKinds,
		stashUpvalueIndices:     stashUVIdxs,
		stateFlagUpvalueIndex:   stateFlagUVIndex,
		outerLabels:             outerLabels,
		gotoExits:               gotoExits,
		ownLabel:                ownLabel,
	}); err != nil {
		return program.VarLocation{}, err
	}

	if err := c.emitRangeOverFunctionCall(ctx, iterLocation, compiledFunction, functionIndex, stateFlagReg); err != nil {
		return program.VarLocation{}, err
	}

	c.emitReturnStashDispatch(ctx, stateFlagReg, stashRegs, returnKinds, rangeBodyReturns(statement))

	c.emitOuterLabelDispatch(ctx, stateFlagReg, outerLabels)

	if err := c.emitGotoExitDispatch(ctx, stateFlagReg, gotoExits); err != nil {
		return program.VarLocation{}, err
	}

	return program.VarLocation{}, nil
}

// rangeYieldSignature returns the signature of the yield function an iterator takes.
//
// Takes statement (*ast.RangeStmt) which is the range statement.
// Takes iteratorSignature (*types.Signature) which is the iterator's signature.
//
// Returns *types.Signature which is the yield function's signature.
// Returns error which wraps fault.ErrCompilation when the parameter is no function.
func (c *Compiler) rangeYieldSignature(statement *ast.RangeStmt, iteratorSignature *types.Signature) (*types.Signature, error) {
	yieldType := iteratorSignature.Params().At(0).Type().Underlying()
	yieldSignature, ok := yieldType.(*types.Signature)
	if !ok {
		return nil, fmt.Errorf("%w: yield parameter underlying type is not a signature: %T at %s",
			fault.ErrCompilation, yieldType, c.positionString(statement.Pos()))
	}
	return yieldSignature, nil
}

// checkYieldClosureOperands checks that the yield closure's captures and the stashed
// results each fit an instruction's 8-bit count operand.
//
// Takes statement (*ast.RangeStmt) which positions the error.
// Takes compiledFunction (*program.CompiledFunction) which is the yield closure.
// Takes returnKinds ([]isa.RegisterKind) which are the enclosing function's results.
//
// Returns error when either count does not fit.
func (c *Compiler) checkYieldClosureOperands(statement *ast.RangeStmt, compiledFunction *program.CompiledFunction, returnKinds []isa.RegisterKind) error {
	if _, err := c.uint8Operand(len(compiledFunction.UpvalueDescriptors), "yield closure capture count", statement.Pos()); err != nil {
		return err
	}
	_, err := c.uint8Operand(len(returnKinds), "result count", statement.Pos())
	return err
}

// emitRangeOverFunctionCall makes the yield closure with a fresh state flag cell, calls
// the iterator with it, copies the closure's cells back into this frame's registers and
// marks the cell exited (emitRangeFunctionExited).
//
// Takes iterLocation (VarLocation) which holds the iterator function.
// Takes compiledFunction (*program.CompiledFunction) which is the yield closure.
// Takes functionIndex (uint16) which is the closure's function table index.
// Takes stateFlagReg (uint8) which holds the state flag.
//
// Returns error when the exited marker's constant cannot be added.
func (c *Compiler) emitRangeOverFunctionCall(ctx context.Context, iterLocation program.VarLocation, compiledFunction *program.CompiledFunction, functionIndex uint16, stateFlagReg uint8) error {
	program.EmitTier1(c.Function, isa.SubOpResetSharedCell, stateFlagReg, uint8(isa.RegisterInt64))
	c.refreshYieldCaptures(compiledFunction)
	yieldReg := c.emitIteratorCall(ctx, iterLocation, functionIndex)
	program.EmitTier2(c.Function, isa.SubOpTier2SynchroniseClosureCapturedVariables, yieldReg)
	if err := c.emitRangeFunctionExited(stateFlagReg); err != nil {
		return err
	}
	return c.emitRangeFunctionRecoveredCheck(stateFlagReg)
}

// emitRangeFunctionRecoveredCheck raises Go's runtime error after the iterator returns
// when the state flag still marks a body call that panicked
// (isa.RangeFunctionStatePanicked). The iterator recovered the body's panic and returned
// instead of letting it continue.
//
// Takes stateFlagReg (uint8) which holds the flag the body left.
//
// Returns error when the constant cannot be added.
func (c *Compiler) emitRangeFunctionRecoveredCheck(stateFlagReg uint8) error {
	jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, isa.RangeFunctionStatePanicked)
	if !ok {
		return fmt.Errorf("%w: range-over-func recovered-panic check", fault.ErrCompilation)
	}
	recoveredIndex, err := program.AddIntConstant(c.Function, isa.RangeFunctionStateRecovered)
	if err != nil {
		return err
	}
	recoveredRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, recoveredRegister, recoveredIndex)
	program.EmitTier2(c.Function, isa.SubOpTier2PanicRangeFunctionContinuedInt64, recoveredRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, recoveredRegister)
	program.PatchJump(c.Function, jumpSkip)
	return nil
}

// emitRangeFunctionExited marks the yield closure's state flag cell exited once the
// iterator has returned, so a yield function the iterator kept and calls later panics as
// in Go.
//
// Takes stateFlagReg (uint8) which holds the state flag.
//
// Returns error when the constant cannot be added.
func (c *Compiler) emitRangeFunctionExited(stateFlagReg uint8) error {
	exitedIndex, err := program.AddIntConstant(c.Function, isa.RangeFunctionStateExited)
	if err != nil {
		return err
	}
	savedRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), savedRegister, stateFlagReg)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, stateFlagReg, exitedIndex)
	program.EmitTier1(c.Function, isa.SubOpSetSharedCell, stateFlagReg, uint8(isa.RegisterInt64))
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), stateFlagReg, savedRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, savedRegister)
	return nil
}

// emitRangeFunctionContinuedCheck starts a range-over-func body with a check of its
// loop's state flag.
//
// The check panics as Go does when the iterator calls the body after the body returned
// false, after a call of the body panicked, or after the range statement finished.
func (c *Compiler) emitRangeFunctionContinuedCheck() {
	flagRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpGetCapturedVariable, flagRegister, safeconv.MustIntToUint8(c.rangeOverFunction.stateFlagUpvalueIndex), uint8(isa.RegisterInt64))
	program.EmitTier2(c.Function, isa.SubOpTier2PanicRangeFunctionContinuedInt64, flagRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, flagRegister)
	c.emitSetEnclosingStateFlag(isa.RangeFunctionStatePanicked)
}

// refreshYieldCaptures copies current register values into shared cells before creating a
// yield closure. This prevents a loop iteration from capturing stale values left by an
// earlier iteration.
//
// Takes compiledFunction (*program.CompiledFunction) which is the yield closure.
func (c *Compiler) refreshYieldCaptures(compiledFunction *program.CompiledFunction) {
	for _, descriptor := range compiledFunction.UpvalueDescriptors {
		if descriptor.IsLocal && !descriptor.IsIndirect {
			program.EmitTier1(c.Function, isa.SubOpSetSharedCell, descriptor.Index, uint8(descriptor.Kind))
		}
	}
}

// allocReturnStash allocates registers for stashing return values when a return statement
// is found inside a range-over-func body.
//
// Returns the allocated stash register locations and the matching register kinds for each
// return value.
func (c *Compiler) allocReturnStash(_ context.Context) (stashRegs []program.VarLocation, returnKinds []isa.RegisterKind) {
	returnKinds = c.Function.ResultKinds
	if c.rangeOverFunction != nil {
		returnKinds = c.rangeOverFunction.returnKinds
	}
	if len(returnKinds) == 0 {
		return nil, nil
	}
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
		Kind:    isa.RegisterInt64,
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

// collectOuterLabelTargets collects labelled outer loops for cross-closure break/continue
// from within a range-over-func body.
//
// Inside another range-over-func body, the labels outside that body are collected too, as
// forwarded targets, and a jump to one is passed on to the enclosing body's dispatch.
//
// Returns the list of outer label targets with assigned flag values, and the first flag
// value left unassigned.
func (c *Compiler) collectOuterLabelTargets(_ context.Context) ([]outerLabelTarget, int64) {
	var outerLabels []outerLabelTarget
	nextFlag := rangeOverFunctionFirstLabelFlag
	for i := range slices.Backward(c.breakables) {
		breakable := &c.breakables[i]
		if breakable.label == "" {
			continue
		}
		target := outerLabelTarget{label: breakable.label,
			breakFlag:      nextFlag,
			breakableIndex: i, continueFlag: 0,
			forwardBreakFlag: 0, forwardContinueFlag: 0, forwarded: false, continuesEnclosingBody: false}
		nextFlag++
		if breakable.isLoop {
			target.continueFlag = nextFlag
			nextFlag++
		}
		outerLabels = append(outerLabels, target)
	}
	if c.rangeOverFunction != nil {
		for _, enclosing := range c.rangeOverFunction.outerLabels {
			target := outerLabelTarget{label: enclosing.label, breakFlag: nextFlag, breakableIndex: -1,
				continueFlag: 0, forwardBreakFlag: enclosing.breakFlag, forwardContinueFlag: enclosing.continueFlag, forwarded: true,
				continuesEnclosingBody: false}
			nextFlag++
			if enclosing.continueFlag > 0 {
				target.continueFlag = nextFlag
				nextFlag++
			}
			outerLabels = append(outerLabels, target)
		}
		if label := c.rangeOverFunction.ownLabel; label != "" {
			outerLabels = append(outerLabels, outerLabelTarget{label: label, breakFlag: nextFlag, continueFlag: nextFlag + 1,
				breakableIndex: -1, forwardBreakFlag: 1, forwardContinueFlag: 0, forwarded: true, continuesEnclosingBody: true})
			nextFlag += 2
		}
	}
	return outerLabels, nextFlag
}

// compileYieldBody creates a sub-Compiler and compiles the range-over-func yield closure
// body, including parameter binding and implicit return true.
//
// Takes p (yieldBodyParams) which is the bundled parameters for the yield body
// compilation.
//
// Returns an error if compilation of the yield body fails.
func (c *Compiler) compileYieldBody(ctx context.Context, p yieldBodyParams) error {
	attempt := func(settings registerSettings) (*Compiler, error) {
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
				resultTypes:               c.sourceResultTypes(),
				bodyLabels:                labelsDeclaredIn(p.statement.Body),
				gotoExits:                 p.gotoExits,
				ownLabel:                  p.ownLabel,
			},
		})
		p.gotoExits.targets = nil
		sub.Scopes.PushScope()
		sub.closureCapturedNames = escape.CollectClosureCapturedNamesAll(p.statement.Body)
		sub.writtenLocalNames = escape.CollectWrittenLocalNames(p.statement.Body)
		sub.Scopes.SetUnspillableNames(sub.unspillableNames(p.statement.Body, sub.closureCapturedNames))
		sub.inPlaceAppendAliases = appendlower.CollectAliases(sub.Info, appendlower.Body{
			Receiver:   nil,
			Parameters: nil,
			Entry:      []ast.Expr{p.statement.Key, p.statement.Value},
			Block:      p.statement.Body,
			Repeats:    true,
		})

		if err := sub.declareYieldParams(ctx, p.statement, p.compiledFunction, p.numberOfYieldParameters); err != nil {
			return sub, err
		}
		sub.applyRegisterSettings(settings)
		sub.emitRangeFunctionContinuedCheck()

		if _, err := sub.compileStmtList(ctx, p.statement.Body.List); err != nil {
			return sub, err
		}
		sub.emitYieldReturn(ctx, true)
		return sub, nil
	}
	finish := func(sub *Compiler) error {
		return sub.finishBody(ctx, p.compiledFunction, p.statement.Body, finishFlags{tailCall: false, finaliseDefer: false, classifyEscapes: false, returnAtEnd: false})
	}
	unit := retryUnit{function: p.compiledFunction, node: p.statement, substitutions: c.typeSubstitutions}
	if err := c.compileNestedBodyWithRetry(ctx, unit, attempt, finish); err != nil {
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
				return fmt.Errorf("%w: range key is not an identifier: %T at %s", fault.ErrCompilation, statement.Key, c.positionString(statement.Key.Pos()))
			}
			c.Scopes.DeclareParameter(keyIdent.Name, parameterKind)
		} else {
			c.Scopes.Alloc.Alloc(parameterKind)
		}
	}
	if numYieldParams >= 2 {
		parameterKind := compiledFunction.ParameterKinds[1]
		if hasValue {
			valueIdentifier, ok := statement.Value.(*ast.Ident)
			if !ok {
				return fmt.Errorf("%w: range value is not an identifier: %T at %s", fault.ErrCompilation, statement.Value, c.positionString(statement.Value.Pos()))
			}
			c.Scopes.DeclareParameter(valueIdentifier.Name, parameterKind)
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

	siteIndex, err := program.AddCallSite(c.Function, new(program.CallSite{
		IsNative:       true,
		NativeRegister: iterLocation.Register,
		Arguments:      []program.VarLocation{{Register: yieldReg, Kind: isa.RegisterGeneral}},
	}))
	if err != nil {
		c.recordStickyError(err)
		return yieldReg
	}
	c.markCallPosition()
	program.EmitTier1Wide(c.Function, isa.SubOpCallNative, siteIndex)
	return yieldReg
}

// emitReturnStashDispatch checks the state flag for a pending return and carries it out.
//
// An explicit return (flag 2) stashed its values, which become the function's results,
// stored into the named results (real, or the synthetic ones a function with defers has)
// and returned through the named-result protocol, so deferred calls see and may change
// them, or moved to the return slots of a function with neither. A bare return (flag -1)
// returns the named results as they stand. Inside another range-over-func body the return
// is passed on. The values go to the enclosing body's stash, its flag is set and its
// yield closure returns false.
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
	jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, rangeOverFunctionReturnPendingFlag)
	if !ok {
		return
	}
	c.emitPendingReturn(ctx, stashRegs, returnKinds)
	program.PatchJump(c.Function, jumpSkip)

	if !bodyReturns || len(returnKinds) == 0 {
		return
	}
	jumpSkipBare, ok := c.emitStateFlagTest(stateFlagReg, rangeOverFunctionBareReturnFlag)
	if !ok {
		return
	}
	if c.rangeOverFunction != nil {
		c.emitSetEnclosingStateFlag(rangeOverFunctionBareReturnFlag)
		c.emitYieldReturn(ctx, false)
	} else {
		c.emitNamedResultReturn(ctx)
	}
	program.PatchJump(c.Function, jumpSkipBare)
}

// emitPendingReturn carries out an explicit return a range-over-func body stashed. It
// passes the values to the enclosing body's stash, stores them into the named results and
// returns through the named-result protocol, or moves them to the return slots.
//
// Takes stashRegs ([]VarLocation) which hold the stashed return values.
// Takes returnKinds ([]isa.RegisterKind) which are the return-value banks.
func (c *Compiler) emitPendingReturn(ctx context.Context, stashRegs []program.VarLocation, returnKinds []isa.RegisterKind) {
	switch {
	case c.rangeOverFunction != nil:
		rangeContext := c.rangeOverFunction
		for i, stash := range stashRegs {
			program.Emit(c.Function, isa.OpSetCapturedVariable, stash.Register, safeconv.MustIntToUint8(rangeContext.returnStashUpvalueIndices[i]), uint8(stash.Kind))
		}
		c.emitSetEnclosingStateFlag(rangeOverFunctionReturnPendingFlag)
		c.emitYieldReturn(ctx, false)
	case len(c.Function.NamedResultLocations) > 0:
		for i := range c.Function.NamedResultLocations {
			var staticType types.Type
			if i < len(c.currentResultTypes) {
				staticType = c.currentResultTypes[i]
			}
			c.emitNamedResultStore(ctx, i, stashRegs[i], staticType)
		}
		c.emitNamedResultReturn(ctx)
	default:
		var bankCounters [isa.NumRegisterKinds]uint8
		for i, kind := range returnKinds {
			destinationRegister := bankCounters[kind]
			bankCounters[kind]++
			c.emitMove(ctx, program.VarLocation{Register: destinationRegister, Kind: kind}, stashRegs[i])
		}
		for k := range bankCounters {
			if bankCounters[k] > 0 {
				c.Scopes.Alloc.EnsureMin(isa.RegisterKind(k), uint32(bankCounters[k]))
			}
		}
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), safeconv.MustIntToUint8(len(returnKinds)))
	}
}

// emitStateFlagTest emits a jump past the code that follows unless the state flag holds
// value.
//
// Takes stateFlagReg (uint8) which holds the state flag.
// Takes value (int64) which is the flag value tested for.
//
// Returns the jump to patch at the end of the guarded code, and false when the constant
// could not be added (the error is recorded).
func (c *Compiler) emitStateFlagTest(stateFlagReg uint8, value int64) (int, bool) {
	valueIndex, err := program.AddIntConstant(c.Function, value)
	if err != nil {
		c.recordStickyError(err)
		return 0, false
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, valueIndex)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpEqualInt64, comparisonRegister, stateFlagReg, temporaryRegister)
	jumpSkip := program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, temporaryRegister)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, comparisonRegister)
	return jumpSkip, true
}

// emitSetEnclosingStateFlag sets the state flag of the range-over-func body being
// compiled, which its loop's dispatch reads once the iterator returns.
//
// Takes value (int64) which is the flag value.
func (c *Compiler) emitSetEnclosingStateFlag(value int64) {
	valueIndex, err := program.AddIntConstant(c.Function, value)
	if err != nil {
		c.recordStickyError(err)
		return
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, valueIndex)
	program.Emit(c.Function, isa.OpSetCapturedVariable, temporaryRegister, safeconv.MustIntToUint8(c.rangeOverFunction.stateFlagUpvalueIndex), uint8(isa.RegisterInt64))
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, temporaryRegister)
}

// emitGotoExitDispatch emits, for each label the body left by goto, a test of the state
// flag and the goto itself, compiled in the enclosing body (which passes it on again when
// the label lies outside it too).
//
// Takes stateFlagReg (uint8) which holds the state flag.
// Takes exits (*rangeOverFunctionGotoExits) which lists the labels.
//
// Returns any error compiling a goto.
func (c *Compiler) emitGotoExitDispatch(ctx context.Context, stateFlagReg uint8, exits *rangeOverFunctionGotoExits) error {
	for _, exit := range exits.targets {
		jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, exit.flag)
		if !ok {
			return nil
		}
		if _, err := c.compileBranchGoto(ctx, exit.statement); err != nil {
			return err
		}
		program.PatchJump(c.Function, jumpSkip)
	}
	return nil
}

// labelsDeclaredIn returns the labels declared in body, outside any function literal.
//
// Takes body (*ast.BlockStmt) which is the block searched.
//
// Returns the set of label names, nil when there are none.
func labelsDeclaredIn(body *ast.BlockStmt) map[string]bool {
	var labels map[string]bool
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.LabeledStmt:
			if labels == nil {
				labels = make(map[string]bool)
			}
			labels[typed.Label.Name] = true
		}
		return true
	})
	return labels
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
	for _, target := range outerLabels {
		c.emitFlagBreakDispatch(ctx, stateFlagReg, target)
		c.emitFlagContinueDispatch(ctx, stateFlagReg, target)
	}
}

// emitFlagBreakDispatch emits a state flag check and break jump for a single labelled
// outer loop target.
//
// Takes stateFlagReg (uint8) which is the register holding the state flag.
// Takes target (outerLabelTarget) which is the outer label target containing the break
// flag value and breakable index.
func (c *Compiler) emitFlagBreakDispatch(ctx context.Context, stateFlagReg uint8, target outerLabelTarget) {
	if target.forwarded {
		c.emitForwardedLabelDispatch(ctx, stateFlagReg, target.breakFlag, target.forwardBreakFlag)
		return
	}
	jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, target.breakFlag)
	if !ok {
		return
	}
	jumpPC := program.EmitTier1Jump(c.Function)
	c.breakables[target.breakableIndex].breakJumps = append(c.breakables[target.breakableIndex].breakJumps, jumpPC)
	program.PatchJump(c.Function, jumpSkip)
}

// emitFlagContinueDispatch emits a state flag check and continue jump for a single
// labelled outer loop target, if it is a loop.
//
// Takes stateFlagReg (uint8) which is the register holding the state flag.
// Takes target (outerLabelTarget) which is the outer label target containing the continue
// flag value and breakable index.
func (c *Compiler) emitFlagContinueDispatch(ctx context.Context, stateFlagReg uint8, target outerLabelTarget) {
	if target.continueFlag == 0 {
		return
	}
	if target.forwarded && !target.continuesEnclosingBody {
		c.emitForwardedLabelDispatch(ctx, stateFlagReg, target.continueFlag, target.forwardContinueFlag)
		return
	}
	jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, target.continueFlag)
	if !ok {
		return
	}
	if target.continuesEnclosingBody {
		c.emitYieldReturn(ctx, true)
		program.PatchJump(c.Function, jumpSkip)
		return
	}
	jumpPC := program.EmitTier1Jump(c.Function)
	c.breakables[target.breakableIndex].continueJumps = append(c.breakables[target.breakableIndex].continueJumps, jumpPC)
	program.PatchJump(c.Function, jumpSkip)
}

// emitForwardedLabelDispatch passes a labelled break or continue for a label outside the
// enclosing range-over-func body on to that body. When the state flag holds flag, it sets
// the enclosing body's flag to forwardFlag and returns false from its yield closure.
//
// Takes stateFlagReg (uint8) which holds the state flag.
// Takes flag (int64) which is this loop's flag value for the jump.
// Takes forwardFlag (int64) which is the enclosing body's flag value for it.
func (c *Compiler) emitForwardedLabelDispatch(ctx context.Context, stateFlagReg uint8, flag, forwardFlag int64) {
	jumpSkip, ok := c.emitStateFlagTest(stateFlagReg, flag)
	if !ok {
		return
	}
	c.emitSetEnclosingStateFlag(forwardFlag)
	c.emitYieldReturn(ctx, false)
	program.PatchJump(c.Function, jumpSkip)
}

// emitYieldReturn emits instructions to return a boolean value from a range-over-func
// yield callback. Used for implicit end-of-body (true), break (false), continue (true),
// and return (false).
//
// Takes value (bool) which is the boolean to return, true to continue iteration and false
// to stop it. Returning true first clears the state flag's mark of a running call
// (isa.RangeFunctionStatePanicked). Returning false leaves the flag its caller set.
func (c *Compiler) emitYieldReturn(ctx context.Context, value bool) {
	boolInt := uint8(0)
	if value {
		boolInt = 1
		c.emitSetEnclosingStateFlag(0)
	}
	intRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadBoolImmediateAsInt64), intRegister, boolInt)
	source := program.VarLocation{Register: intRegister, Kind: isa.RegisterInt64}
	dest := program.VarLocation{Register: 0, Kind: isa.RegisterBool}
	c.emitMove(ctx, dest, source)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, intRegister)
	c.Scopes.Alloc.EnsureMin(isa.RegisterBool, 1)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 1)
}

// emitRangeOverFunctionBreak emits instructions to break out of a range-over-func loop.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) emitRangeOverFunctionBreak(ctx context.Context) (program.VarLocation, error) {
	return c.emitRangeOverFunctionLabelledBreak(ctx, 1)
}

// emitRangeOverFunctionLabelledBreak emits instructions to set the state flag and return
// false from the yield callback.
//
// Takes flagValue (int64) which is the state flag value to set (1 for plain break, 3+ for
// labelled break/continue).
//
// Returns the compiled location and any error encountered.
func (c *Compiler) emitRangeOverFunctionLabelledBreak(ctx context.Context, flagValue int64) (program.VarLocation, error) {
	rangeContext := c.rangeOverFunction
	index, err := program.AddIntConstant(c.Function, flagValue)
	if err != nil {
		return program.VarLocation{}, err
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, index)
	program.Emit(c.Function, isa.OpSetCapturedVariable, temporaryRegister, safeconv.MustIntToUint8(rangeContext.stateFlagUpvalueIndex), uint8(isa.RegisterInt64))
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, temporaryRegister)
	c.emitYieldReturn(ctx, false)
	return program.VarLocation{}, nil
}

// compileRangeOverFunctionReturn compiles a return statement inside a range-over-func
// yield body.
//
// Takes statement (*ast.ReturnStmt) which is the return statement to compile within the
// yield body.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileRangeOverFunctionReturn(ctx context.Context, statement *ast.ReturnStmt) (program.VarLocation, error) {
	rangeContext := c.rangeOverFunction

	if len(statement.Results) == 0 && len(rangeContext.returnStashUpvalueIndices) > 0 {
		c.emitSetEnclosingStateFlag(rangeOverFunctionBareReturnFlag)
		c.emitYieldReturn(ctx, false)
		return program.VarLocation{}, nil
	}

	if err := c.stashRangeOverFunctionReturnValues(ctx, statement); err != nil {
		return program.VarLocation{}, err
	}

	c.emitSetEnclosingStateFlag(rangeOverFunctionReturnPendingFlag)
	c.emitYieldReturn(ctx, false)
	return program.VarLocation{}, nil
}

// stashRangeOverFunctionReturnValues compiles the values of a return statement inside a
// range-over-func body against the enclosing function's result types and stores each in
// its stash upvalue.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
//
// Returns any compilation error, or one for a value count that does not match the
// results.
func (c *Compiler) stashRangeOverFunctionReturnValues(ctx context.Context, statement *ast.ReturnStmt) error {
	stashes := c.rangeOverFunction.returnStashUpvalueIndices
	if len(statement.Results) == 1 && len(stashes) > 1 {
		return c.stashRangeOverFunctionTupleReturn(ctx, statement)
	}
	if len(statement.Results) != len(stashes) {
		return fmt.Errorf("%w: %d values for %d results at %s", fault.ErrCompilation, len(statement.Results), len(stashes), c.positionString(statement.Pos()))
	}
	for i, result := range statement.Results {
		if err := c.stashRangeOverFunctionReturnValue(ctx, i, result); err != nil {
			return err
		}
	}
	return nil
}

// stashRangeOverFunctionTupleReturn stashes the results of `return f()` for a function of
// several results, one per result.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
//
// Returns any compilation error, or one when the statement is not a call of as many
// results as the function has.
func (c *Compiler) stashRangeOverFunctionTupleReturn(ctx context.Context, statement *ast.ReturnStmt) error {
	stashes := c.rangeOverFunction.returnStashUpvalueIndices
	call, ok := ast.Unparen(statement.Results[0]).(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("%w: one value for %d results at %s", fault.ErrCompilation, len(stashes), c.positionString(statement.Pos()))
	}
	watermark := c.Scopes.Alloc.Snapshot()
	defer c.Scopes.RestoreWatermark(watermark)
	values, err := c.compileReturnTupleCall(ctx, call)
	if err != nil {
		return err
	}
	if len(values) != len(stashes) {
		return fmt.Errorf("%w: %d values for %d results at %s", fault.ErrCompilation, len(values), len(stashes), c.positionString(statement.Pos()))
	}
	for i, value := range values {
		c.emitReturnStash(ctx, i, value, nil)
	}
	return nil
}

// stashRangeOverFunctionReturnValue compiles the i-th value of a return statement against
// its result's type and stashes it, freeing its registers afterwards.
//
// Takes i (int) which is the result index.
// Takes result (ast.Expr) which is the value.
//
// Returns any compilation error.
func (c *Compiler) stashRangeOverFunctionReturnValue(ctx context.Context, i int, result ast.Expr) error {
	watermark := c.Scopes.Alloc.Snapshot()
	defer c.Scopes.RestoreWatermark(watermark)
	var expectedType types.Type
	if resultTypes := c.rangeOverFunction.resultTypes; i < len(resultTypes) {
		expectedType = resultTypes[i]
	}
	location, handled, err := c.compileTypedNilOrExpression(ctx, result, expectedType)
	if err != nil {
		return err
	}
	if !handled {
		if location, err = c.compileExpression(ctx, result); err != nil {
			return err
		}
	}
	location = c.coerceEvalBoolResult(ctx, c.Info, result, location)
	location = c.snapshotReturnValueIfNeeded(result, location)
	c.emitReturnStash(ctx, i, location, c.staticTypeOf(result))
	return nil
}

// emitReturnStash stores one returned value into its stash upvalue, converting it to the
// result's register bank first when the expression produced another.
//
// Takes i (int) which is the result index.
// Takes location (VarLocation) which holds the value.
// Takes staticType (types.Type) which is the value's static type, or nil.
func (c *Compiler) emitReturnStash(ctx context.Context, i int, location program.VarLocation, staticType types.Type) {
	rangeContext := c.rangeOverFunction
	if i < len(rangeContext.returnKinds) && location.Kind != rangeContext.returnKinds[i] {
		converted := program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(rangeContext.returnKinds[i]), Kind: rangeContext.returnKinds[i]}
		c.emitMoveTyped(ctx, converted, location, staticType)
		location = converted
	}
	program.Emit(c.Function, isa.OpSetCapturedVariable, location.Register, safeconv.MustIntToUint8(rangeContext.returnStashUpvalueIndices[i]), uint8(location.Kind))
}

// emitRangeOverFunctionGotoExit compiles a goto that leaves a range-over-func body. It
// sets the state flag value given to the label and returns false from the yield closure,
// and the loop's dispatch performs the goto once the iterator returns.
//
// Takes statement (*ast.BranchStmt) which is the goto.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) emitRangeOverFunctionGotoExit(ctx context.Context, statement *ast.BranchStmt) (program.VarLocation, error) {
	exits := c.rangeOverFunction.gotoExits
	if exits == nil {
		return program.VarLocation{}, fmt.Errorf("%w: goto %s at %s", fault.ErrCompileGotoOutOfRangeOverFunction, statement.Label.Name, c.positionString(statement.Pos()))
	}
	for _, exit := range exits.targets {
		if exit.statement.Label.Name == statement.Label.Name {
			return c.emitRangeOverFunctionLabelledBreak(ctx, exit.flag)
		}
	}
	flag := exits.firstFlag + int64(len(exits.targets))
	exits.targets = append(exits.targets, rangeOverFunctionGotoExit{statement: statement, flag: flag})
	return c.emitRangeOverFunctionLabelledBreak(ctx, flag)
}
