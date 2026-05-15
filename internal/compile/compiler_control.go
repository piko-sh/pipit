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
	"strings"

	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

// loopPostDeclKey identifies a register slot occupied by a for-stmt-init-declared
// variable, scoped by its register-bank kind so the same numeric index in different banks
// doesn't collide.
type loopPostDeclKey struct {
	// kind is the register-bank kind (registerInt, registerGeneral, etc.).
	kind isa.RegisterKind

	// register is the per-bank slot index.
	register uint8
}

// isTailCallEligible checks whether a return statement can be compiled as a tail call.
//
// Tail calls require: exactly one return expression, that expression is a direct
// *ast.CallExpr (not type conversion), callee is a known compiled function (in
// functionTable) that is not a generic template, no defers in the current function, and
// callee and caller have matching result signatures.
//
// Takes statement (*ast.ReturnStmt) which is the return statement to check for tail call
// eligibility.
//
// Returns the call expression if eligible, or nil otherwise.
func (c *Compiler) isTailCallEligible(_ context.Context, statement *ast.ReturnStmt) *ast.CallExpr {
	if c.hasDefers || len(statement.Results) != 1 {
		return nil
	}
	callExpression, ok := statement.Results[0].(*ast.CallExpr)
	if !ok {
		return nil
	}

	if tv, ok := c.Info.Types[callExpression.Fun]; ok && tv.IsType() {
		return nil
	}

	if _, ok := callExpression.Fun.(*ast.SelectorExpr); ok {
		return nil
	}
	identifier, ok := callExpression.Fun.(*ast.Ident)
	if !ok {
		return nil
	}

	typeObject, ok := c.Info.Uses[identifier]
	if !ok {
		return nil
	}
	if _, isFunc := typeObject.(*types.Func); !isFunc {
		return nil
	}
	functionIndex, found := c.functionTable[identifier.Name]
	if !found {
		return nil
	}

	if !c.tailCallCalleeCompatible(c.RootFunction.Functions[functionIndex]) {
		return nil
	}
	return callExpression
}

// tailCallCalleeCompatible reports whether callee can replace the current frame.
//
// A generic template is refused because its results sit in the general bank and the
// direct-call path specialises the callee for the call site's type arguments instead; a
// callee that recovers or inspects the call stack needs its own frame; and the result
// kinds must match slot for slot so the caller's return registers receive the values.
//
// Takes callee (*program.CompiledFunction) which is the tail-call candidate.
//
// Returns bool which is true when the frame can be reused for callee.
func (c *Compiler) tailCallCalleeCompatible(callee *program.CompiledFunction) bool {
	if callee.IsGenericFunction || callee.HasRecover || callee.InspectsCallStack {
		return false
	}
	if len(callee.ResultKinds) != len(c.Function.ResultKinds) {
		return false
	}
	for i, k := range callee.ResultKinds {
		if k != c.Function.ResultKinds[i] {
			return false
		}
	}
	return true
}

// compileTailCall compiles a tail call for the given call expression.
//
// Takes callExpression (*ast.CallExpr) which is the call expression to compile as a tail
// call.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileTailCall(ctx context.Context, callExpression *ast.CallExpr) (program.VarLocation, error) {
	identifier, ok := callExpression.Fun.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fault.ErrCompileTailCallTargetNotIdent
	}
	functionIndex := c.functionTable[identifier.Name]
	callee := c.RootFunction.Functions[functionIndex]

	argumentLocations, err := c.compileCallArguments(ctx, callExpression, callee)
	if err != nil {
		return program.VarLocation{}, err
	}

	tailReuseFrameInPlace := callee == c.Function && c.Function.NumRegisters == callee.NumRegisters
	site := program.CallSite{
		FunctionIndex:         functionIndex,
		Arguments:             argumentLocations,
		TailReuseFrameInPlace: tailReuseFrameInPlace,
		TailArgsAlias:         tailReuseFrameInPlace && program.DetectTailCallArgsAlias(argumentLocations, callee.ParameterKinds),
		CachedCallee:          callee,
		ArgCopyProgram:        program.BuildCallArgCopyProgram(argumentLocations, callee.ParameterKinds, callee.ParameterRegisters),
	}
	siteIndex, addErr := program.AddCallSite(c.Function, &site)
	if addErr != nil {
		return program.VarLocation{}, addErr
	}
	program.EmitTier1Wide(c.Function, isa.SubOpTailCall, siteIndex)

	return program.VarLocation{}, nil
}

// rewriteTrailingCallAsTailCall upgrades a void function's trailing SubOpCall to
// SubOpTailCall when eligible. Runs after compileStmtList so liveness analysis sees real
// last-use indices.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose body has just
// been emitted into c.Function.
func (c *Compiler) rewriteTrailingCallAsTailCall(compiledFunction *program.CompiledFunction) {
	if len(compiledFunction.ResultKinds) != 0 {
		return
	}
	if c.hasDefers {
		return
	}
	if c.lastCallPC < 0 || c.lastCallPC != len(compiledFunction.Body)-1 {
		return
	}
	lastInstruction := &compiledFunction.Body[c.lastCallPC]
	if !isa.InstrIsTier1SubOp(*lastInstruction, isa.SubOpCall) {
		return
	}
	siteIndex := lastInstruction.WideIndex()
	if int(siteIndex) >= len(compiledFunction.CallSites) {
		return
	}
	site := &compiledFunction.CallSites[siteIndex]
	if site.IsClosure || site.IsNative || site.IsMethod || site.IsEllipsisSpread {
		return
	}
	if site.CachedCallee == nil {
		return
	}
	callee := site.CachedCallee
	if len(callee.ResultKinds) != 0 {
		return
	}
	if callee.IsVariadic || callee.HasRecover || callee.InspectsCallStack {
		return
	}
	if site.RuntimeVariadicSliceType != nil {
		return
	}
	tailReuseFrameInPlace := callee == compiledFunction && compiledFunction.NumRegisters == callee.NumRegisters
	site.TailReuseFrameInPlace = tailReuseFrameInPlace
	site.TailArgsAlias = tailReuseFrameInPlace && program.DetectTailCallArgsAlias(site.Arguments, callee.ParameterKinds)

	lastInstruction.A = uint8(isa.SubOpTailCall)
}

// compileReturn compiles a return statement.
//
// Takes statement (*ast.ReturnStmt) which is the return statement to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileReturn(ctx context.Context, statement *ast.ReturnStmt) (program.VarLocation, error) {
	if c.rangeOverFunction != nil {
		return c.compileRangeOverFunctionReturn(ctx, statement)
	}

	if len(statement.Results) == 0 {
		return c.compileBareReturn(ctx)
	}

	if len(c.Function.NamedResultLocations) > 0 {
		return c.compileNamedExplicitReturn(ctx, statement)
	}

	if callExpression := c.isTailCallEligible(ctx, statement); callExpression != nil {
		return c.compileTailCall(ctx, callExpression)
	}

	return c.compileExplicitReturn(ctx, statement)
}

// compileBareReturn compiles a return statement with no explicit values.
//
// Uses named result variables if present, otherwise emits a void return.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBareReturn(ctx context.Context) (program.VarLocation, error) {
	if len(c.Function.NamedResultLocations) > 0 {
		c.emitNamedResultReturn(ctx)
		return program.VarLocation{}, nil
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	return program.VarLocation{}, nil
}

// emitNamedResultReturn emits the opReturn for a bare named-result return.
//
// Named results are materialised into canonical bank-0 slots before the return so the
// ASM-inline fast path sees the correct value for heap-promoted results.
func (c *Compiler) emitNamedResultReturn(ctx context.Context) {
	c.canonicaliseNamedResults(ctx)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), safeconv.MustIntToUint8(len(c.Function.NamedResultLocations)))
}

// canonicaliseNamedResults moves every named result into the canonical return slot the
// caller reads.
//
// Named results are locals declared above those slots (reserveCanonicalResultSlots), so
// the moves never overwrite a value still to be read. Captured results are read out of
// their cells first; a read that lands in a low register is lifted to a fresh one so a
// later move cannot clobber it.
func (c *Compiler) canonicaliseNamedResults(ctx context.Context) {
	named := c.Function.NamedResultLocations
	var counts [isa.NumRegisterKinds]uint32
	for _, location := range named {
		counts[resultBank(location)]++
	}
	sources := make([]program.VarLocation, len(named))
	for i, location := range named {
		if !location.IsIndirect {
			sources[i] = location
			continue
		}
		dereffed, err := c.EmitIndirectRead(ctx, location)
		if err != nil {
			c.recordStickyError(err)
			return
		}
		if uint32(dereffed.Register) < counts[dereffed.Kind] {
			lifted := program.VarLocation{Register: c.Scopes.Alloc.Alloc(dereffed.Kind), Kind: dereffed.Kind}
			c.emitMove(ctx, lifted, dereffed)
			c.Scopes.Alloc.FreeTemp(dereffed.Kind, dereffed.Register)
			dereffed = lifted
		}
		sources[i] = dereffed
	}
	var bankCounters [isa.NumRegisterKinds]uint8
	for i, location := range named {
		kind := resultBank(location)
		destRegister := bankCounters[kind]
		bankCounters[kind]++
		c.Scopes.Alloc.EnsureMin(kind, uint32(destRegister)+1)
		if sources[i].Kind == kind && sources[i].Register == destRegister {
			continue
		}
		c.emitMove(ctx, program.VarLocation{Register: destRegister, Kind: kind}, sources[i])
	}
}

// compileNamedExplicitReturn compiles a return statement with explicit values when the
// function has named result variables.
//
// Takes statement (*ast.ReturnStmt) which is the return statement containing the explicit
// values.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileNamedExplicitReturn(ctx context.Context, statement *ast.ReturnStmt) (program.VarLocation, error) {
	locs, err := c.compileReturnExprs(ctx, statement, true)
	if err != nil {
		return program.VarLocation{}, err
	}
	for i, location := range locs {
		dest := c.Function.NamedResultLocations[i]
		c.emitMoveTyped(ctx, dest, location, c.returnValueStaticType(statement, locs, i))

		if !dest.IsIndirect && !strings.HasPrefix(c.Function.NamedResultNames[i], scope.SyntheticNamePrefix) {
			program.EmitTier1(c.Function, isa.SubOpWriteSharedCell, dest.Register, uint8(dest.Kind))
		}
	}

	c.emitNamedResultReturn(ctx)
	return program.VarLocation{}, nil
}

// returnValueStaticType resolves the static type of the i-th returned value: the
// expression's own type when the statement lists one expression per result, otherwise (a
// single tuple-valued call) the function's declared result type.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
// Takes locs ([]VarLocation) which holds one location per returned value.
// Takes i (int) which is the value index.
//
// Returns the static type, or nil when it is unknown.
func (c *Compiler) returnValueStaticType(statement *ast.ReturnStmt, locs []program.VarLocation, i int) types.Type {
	if len(statement.Results) == len(locs) {
		return c.staticTypeOf(statement.Results[i])
	}
	if i < len(c.currentResultTypes) {
		return c.currentResultTypes[i]
	}
	return nil
}

// compileExplicitReturn compiles a return statement with explicit values for non-named
// results.
//
// Takes statement (*ast.ReturnStmt) which is the return statement containing the explicit
// values.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileExplicitReturn(ctx context.Context, statement *ast.ReturnStmt) (program.VarLocation, error) {
	locs, err := c.compileReturnExprs(ctx, statement, false)
	if err != nil {
		return program.VarLocation{}, err
	}

	bankCounters := c.moveLocsToReturnPositions(ctx, locs)

	for k := range bankCounters {
		if bankCounters[k] > 0 {
			c.Scopes.Alloc.EnsureMin(isa.RegisterKind(k), uint32(bankCounters[k]))
		}
	}

	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), safeconv.MustIntToUint8(len(locs)))
	return program.VarLocation{}, nil
}

// compileReturnExprs compiles all return expressions into temporary registers to avoid
// clobbering.
//
// For example, "return b, a" where a and b are params would clobber without temporaries.
//
// Takes statement (*ast.ReturnStmt) which is the return statement whose expressions are
// compiled.
// Takes named (bool) which indicates whether the function uses named return values.
//
// Returns the compiled locations for each return expression and any error encountered.
func (c *Compiler) compileReturnExprs(ctx context.Context, statement *ast.ReturnStmt, named bool) ([]program.VarLocation, error) {
	if call, ok := c.returnTupleCall(statement); ok {
		return c.compileReturnTupleCall(ctx, call)
	}
	locs := make([]program.VarLocation, len(statement.Results))
	for i, result := range statement.Results {
		location, err := c.compileReturnValue(ctx, statement, i, named)
		if err != nil {
			return nil, err
		}
		if len(statement.Results) > 1 {
			temp := c.Scopes.Alloc.AllocTemp(location.Kind)
			tempLocation := program.VarLocation{Register: temp, Kind: location.Kind}
			c.emitMoveTyped(ctx, tempLocation, location, c.staticTypeOf(result))
			locs[i] = tempLocation
		} else {
			locs[i] = location
		}
	}
	return locs, nil
}

// compileReturnValue compiles the i-th expression of a return statement.
//
// Emits a typed nil against the declared result type when possible, otherwise the
// expression itself, emitted straight into the canonical return slot when the unnamed
// single-result shape allows. The value is then bool-coerced and detached from any heap
// cell it was read out of.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
// Takes i (int) which is the result index.
// Takes named (bool) which is true when the values go to named result locations (no
// direct emission into the return slot).
//
// Returns the location holding the value and any compilation error.
func (c *Compiler) compileReturnValue(ctx context.Context, statement *ast.ReturnStmt, i int, named bool) (program.VarLocation, error) {
	result := statement.Results[i]
	var expectedType types.Type
	if i < len(c.currentResultTypes) {
		expectedType = c.currentResultTypes[i]
	}
	location, handled, err := c.compileTypedNilOrExpression(ctx, result, expectedType)
	if err != nil {
		return program.VarLocation{}, err
	}
	if !handled {
		if dest, direct := c.directReturnDestination(statement, result); direct && !named {
			location, err = c.compileExpressionInto(ctx, result, dest)
		} else {
			location, err = c.compileExpression(ctx, result)
		}
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	location = c.coerceEvalBoolResult(ctx, c.Info, result, location)
	return c.snapshotReturnValueIfNeeded(result, location), nil
}

// returnTupleCall reports whether statement returns a single call whose tuple result
// supplies every result of the current function (`return g()` with g multi-valued).
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
//
// Returns the call and true when the tuple shape matches; nil and false otherwise.
func (c *Compiler) returnTupleCall(statement *ast.ReturnStmt) (*ast.CallExpr, bool) {
	if len(statement.Results) != 1 || len(c.Function.ResultKinds) < 2 || c.Info == nil {
		return nil, false
	}
	call, ok := ast.Unparen(statement.Results[0]).(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	tv, ok := c.Info.Types[call]
	if !ok || tv.Type == nil {
		return nil, false
	}
	tuple, ok := tv.Type.(*types.Tuple)
	if !ok || tuple.Len() != len(c.Function.ResultKinds) {
		return nil, false
	}
	return call, true
}

// compileReturnTupleCall compiles `return g()` for a multi-valued g into one temporary
// per result through the multi-return call path, so every value reaches the caller. The
// plain expression path would keep only the first value.
//
// Takes call (*ast.CallExpr) which is the tuple-valued call.
//
// Returns one location per returned value and any compilation error.
func (c *Compiler) compileReturnTupleCall(ctx context.Context, call *ast.CallExpr) ([]program.VarLocation, error) {
	kinds, err := c.callResultKinds(ctx, call)
	if err != nil {
		return nil, err
	}
	locs := make([]program.VarLocation, len(kinds))
	for i, kind := range kinds {
		locs[i] = program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(kind), Kind: kind}
	}
	if err := c.emitMultiReturnCall(ctx, call, locs); err != nil {
		return nil, err
	}
	return locs, nil
}

// snapshotReturnValueIfNeeded copies the return value when it is a heap-promoted captured
// local, preventing deferred closures from mutating the caller's result through the
// aliased cell.
//
// Takes expression (ast.Expr) which is the return expression.
// Takes location (VarLocation) which is the compiled value location.
//
// Returns a snapshot temporary when a copy was emitted, or location unchanged.
func (c *Compiler) snapshotReturnValueIfNeeded(expression ast.Expr, location program.VarLocation) program.VarLocation {
	if location.Kind != isa.RegisterGeneral {
		return location
	}
	if !c.rhsReadsIndirectLocal(expression) {
		return location
	}
	dest := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, dest, location.Register, engine.DerefSnapshot)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
}

// moveLocsToReturnPositions moves compiled expression locations into their return-slot
// positions.
//
// Uses the function's declared ResultKinds so cross-bank conversions are emitted (e.g.
// isa.RegisterInt -> isa.RegisterBool).
//
// Takes locs ([]VarLocation) which is the compiled expression locations to move.
//
// Returns the bank counters array tracking register usage per kind.
func (c *Compiler) moveLocsToReturnPositions(ctx context.Context, locs []program.VarLocation) [isa.NumRegisterKinds]uint8 {
	var bankCounters [isa.NumRegisterKinds]uint8
	for i, location := range locs {
		destinationKind := location.Kind
		if i < len(c.Function.ResultKinds) {
			destinationKind = c.Function.ResultKinds[i]
		}
		destinationRegister := bankCounters[destinationKind]
		bankCounters[destinationKind]++
		dest := program.VarLocation{Register: destinationRegister, Kind: destinationKind}
		if location.Register != dest.Register || location.Kind != dest.Kind {
			c.emitMove(ctx, dest, location)
		}
	}
	return bankCounters
}

// compileIf compiles an if statement.
//
// Takes statement (*ast.IfStmt) which is the if statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileIf(ctx context.Context, statement *ast.IfStmt) (program.VarLocation, error) {
	if statement.Init != nil {
		c.Scopes.PushScope()
		defer c.Scopes.PopScope()
		if _, err := c.compileStmt(ctx, statement.Init); err != nil {
			return program.VarLocation{}, err
		}
	}

	watermark := c.Scopes.Alloc.Snapshot()
	condLocation, err := c.compileExpression(ctx, statement.Cond)
	if err != nil {
		return program.VarLocation{}, err
	}

	condLocation = c.ensureIntForBranch(ctx, condLocation)

	jumpToElse := program.EmitJump(c.Function, isa.OpJumpIfFalse, condLocation.Register)
	c.Scopes.RestoreWatermark(watermark)

	if _, err := c.compileStmt(ctx, statement.Body); err != nil {
		return program.VarLocation{}, err
	}

	if statement.Else != nil {
		jumpToEnd := program.EmitTier1Jump(c.Function)
		program.PatchJump(c.Function, jumpToElse)

		if _, err := c.compileStmt(ctx, statement.Else); err != nil {
			return program.VarLocation{}, err
		}

		program.PatchJump(c.Function, jumpToEnd)
	} else {
		program.PatchJump(c.Function, jumpToElse)
	}

	return program.VarLocation{}, nil
}

// compileFor compiles a for statement.
//
// Takes statement (*ast.ForStmt) which is the for statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileFor(ctx context.Context, statement *ast.ForStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureForLoops, statement.For); err != nil {
		return program.VarLocation{}, err
	}
	if recogniser, recogniserToken, ok := c.patterns.TryRecogniseForStmt(c.astPatternRecogniseContext(), statement); ok {
		return recogniser.Emit(ctx, c, statement, recogniserToken)
	}
	return c.compileForFallback(ctx, statement)
}

// compileForFallback is the scalar emission path for a for-statement.
//
// Extracted so AST-pattern recognisers can delegate back to it when their Emit declines
// partway (e.g. an operand resolves to a register kind the SIMD opcode cannot index).
// Mirrors compileFor's body sans the recogniser hook and the feature gate (already
// enforced).
//
// Takes statement (*ast.ForStmt) which is the loop.
//
// Returns the loop's VarLocation (always zero) and any error.
func (c *Compiler) compileForFallback(ctx context.Context, statement *ast.ForStmt) (program.VarLocation, error) {
	c.Scopes.PushScope()
	defer c.Scopes.PopScope()
	c.loopDepth++
	defer func() { c.loopDepth-- }()

	if statement.Init != nil {
		if _, err := c.compileStmt(ctx, statement.Init); err != nil {
			return program.VarLocation{}, err
		}
	}

	c.breakables = append(c.breakables, breakableContext{
		isLoop: true,
		label:  c.consumePendingLabel(ctx),
	})

	loopStart := program.CurrentPC(c.Function)

	jumpToEnd, hasCondJump, err := c.compileForCondition(ctx, statement.Cond)
	if err != nil {
		return program.VarLocation{}, err
	}

	if bodyContainsFunctionLit(statement.Body) {
		c.resetSharedCellsForInit(ctx, statement.Init)
	}

	if _, err := c.compileStmt(ctx, statement.Body); err != nil {
		return program.VarLocation{}, err
	}

	c.patchContinueJumps(ctx)

	if err := c.compileForPost(ctx, statement); err != nil {
		return program.VarLocation{}, err
	}

	lo, hi := program.EncodeJumpOffset(c.Function, loopStart-program.CurrentPC(c.Function)-1)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi)

	if hasCondJump {
		program.PatchJump(c.Function, jumpToEnd)
	}
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.breakJumps {
		program.PatchJump(c.Function, pc)
	}

	c.breakables = c.breakables[:len(c.breakables)-1]
	return program.VarLocation{}, nil
}

// compileForPost compiles the post statement of a for loop, tracking the init-declared
// registers so post-statement writes mirror into shared cells for closures captured in
// the loop body.
//
// Takes statement (*ast.ForStmt) which is the for statement whose post clause is
// compiled.
//
// Returns any compilation error from the post statement.
func (c *Compiler) compileForPost(ctx context.Context, statement *ast.ForStmt) error {
	if statement.Post == nil {
		return nil
	}
	return c.withLoopPost(c.collectForInitDeclaredRegisters(statement.Init), func() error {
		_, err := c.compileStmt(ctx, statement.Post)
		return err
	})
}

// compileForCondition compiles the loop condition expression if present.
//
// Takes condition (ast.Expr) which is the condition expression to compile, or nil if
// absent.
//
// Returns the jump-to-end offset, whether a condition jump was emitted, and any error.
func (c *Compiler) compileForCondition(ctx context.Context, condition ast.Expr) (int, bool, error) {
	if condition == nil {
		return 0, false, nil
	}
	condLocation, err := c.compileExpression(ctx, condition)
	if err != nil {
		return 0, false, err
	}
	condLocation = c.ensureIntForBranch(ctx, condLocation)
	jumpToEnd := program.EmitJump(c.Function, isa.OpJumpIfFalse, condLocation.Register)
	return jumpToEnd, true, nil
}

// resetSharedCellsForInit emits isa.SubOpResetSharedCell for each variable declared in a
// for-loop init statement.
//
// This ensures closures captured in the loop body see per-iteration values.
//
// Takes init (ast.Stmt) which is the for-loop init statement to scan for declared
// variables.
func (c *Compiler) resetSharedCellsForInit(_ context.Context, init ast.Stmt) {
	initAssign, ok := init.(*ast.AssignStmt)
	if !ok || initAssign.Tok != token.DEFINE {
		return
	}
	for _, leftHandSide := range initAssign.Lhs {
		identifier, ok := leftHandSide.(*ast.Ident)
		if !ok || identifier.Name == typemap.BlankIdentName {
			continue
		}
		if location, found := c.Scopes.LookupVar(identifier.Name); found && !location.IsSpilled && c.closureCapturedNames[identifier.Name] {
			program.EmitTier1(c.Function, isa.SubOpResetSharedCell, location.Register, uint8(location.Kind))
		}
	}
}

// collectForInitDeclaredRegisters returns init-declared registers.
//
// Used by emitWriteSharedCellIfCaptured to scope the isa.SubOpWriteSharedCell suppression
// to variables that participate in Go 1.22+ per-iteration scoping; variables in the
// enclosing scope (`for ; i < 3; i++`) are not in this set and continue to receive sync
// writes so captured closures see post-loop state.
//
// Takes init (ast.Stmt) which is the for-stmt init clause; nil and non-DEFINE forms
// return an empty set.
//
// Returns a key -> struct{} set; nil when no DEFINE-declared names resolve to a register
// location.
func (c *Compiler) collectForInitDeclaredRegisters(init ast.Stmt) map[loopPostDeclKey]struct{} {
	initAssign, ok := init.(*ast.AssignStmt)
	if !ok || initAssign.Tok != token.DEFINE {
		return nil
	}
	result := make(map[loopPostDeclKey]struct{}, len(initAssign.Lhs))
	for _, leftHandSide := range initAssign.Lhs {
		identifier, ok := leftHandSide.(*ast.Ident)
		if !ok || identifier.Name == typemap.BlankIdentName {
			continue
		}
		location, found := c.Scopes.LookupVar(identifier.Name)
		if !found || location.IsSpilled {
			continue
		}
		result[loopPostDeclKey{kind: location.Kind, register: location.Register}] = struct{}{}
	}
	return result
}

// patchContinueJumps patches all continue jumps in the current breakable context to the
// current PC (the post statement or back-jump location).
func (c *Compiler) patchContinueJumps(_ context.Context) {
	breakable := &c.breakables[len(c.breakables)-1]
	continueTarget := program.CurrentPC(c.Function)
	for _, pc := range breakable.continueJumps {
		lo, hi := program.EncodeJumpOffset(c.Function, continueTarget-pc-1)
		c.Function.Body[pc].B = lo
		c.Function.Body[pc].C = hi
	}
}

// compileBranch compiles a break, continue, goto, or fallthrough statement.
//
// Takes statement (*ast.BranchStmt) which is the branch statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBranch(ctx context.Context, statement *ast.BranchStmt) (program.VarLocation, error) {
	switch statement.Tok {
	case token.BREAK:
		return c.compileBranchBreak(ctx, statement)
	case token.CONTINUE:
		return c.compileBranchContinue(ctx, statement)
	case token.GOTO:
		return c.compileBranchGoto(ctx, statement)
	case token.FALLTHROUGH:
		return c.compileBranchFallthrough(ctx)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported branch: %s at %s", statement.Tok, c.positionString(statement.Pos()))
	}
}

// compileBranchBreak compiles a break statement by searching the breakable context stack.
//
// Falls back to range-over-func state-flag unwinding when the target is outside the yield
// closure.
//
// Takes statement (*ast.BranchStmt) which is the break statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBranchBreak(ctx context.Context, statement *ast.BranchStmt) (program.VarLocation, error) {
	labelName := branchLabelName(statement)
	for i := range slices.Backward(c.breakables) {
		breakable := &c.breakables[i]
		if labelName != "" && breakable.label != labelName {
			continue
		}
		jumpPC := program.EmitTier1Jump(c.Function)
		breakable.breakJumps = append(breakable.breakJumps, jumpPC)
		return program.VarLocation{}, nil
	}

	if c.rangeOverFunction != nil && labelName != "" {
		for _, ol := range c.rangeOverFunction.outerLabels {
			if ol.label == labelName {
				return c.emitRangeOverFunctionLabelledBreak(ctx, ol.breakFlag)
			}
		}
	}

	if c.rangeOverFunction != nil {
		return c.emitRangeOverFunctionBreak(ctx)
	}
	return program.VarLocation{}, fault.ErrCompileBreakOutsideLoopOrSwitch
}

// compileBranchContinue compiles a continue statement by searching the breakable context
// stack.
//
// Falls back to range-over-func state-flag unwinding when the target loop is outside the
// yield closure.
//
// Takes statement (*ast.BranchStmt) which is the continue statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBranchContinue(ctx context.Context, statement *ast.BranchStmt) (program.VarLocation, error) {
	labelName := branchLabelName(statement)
	for i := range slices.Backward(c.breakables) {
		breakable := &c.breakables[i]
		if !breakable.isLoop {
			continue
		}
		if labelName != "" && breakable.label != labelName {
			continue
		}
		jumpPC := program.EmitTier1Jump(c.Function)
		breakable.continueJumps = append(breakable.continueJumps, jumpPC)
		return program.VarLocation{}, nil
	}

	if c.rangeOverFunction != nil && labelName != "" {
		for _, ol := range c.rangeOverFunction.outerLabels {
			if ol.label == labelName && ol.continueFlag > 0 {
				return c.emitRangeOverFunctionLabelledBreak(ctx, ol.continueFlag)
			}
		}
	}

	if c.rangeOverFunction != nil {
		c.emitYieldReturn(ctx, true)
		return program.VarLocation{}, nil
	}
	return program.VarLocation{}, fault.ErrCompileContinueOutsideLoop
}

// compileBranchGoto compiles a goto statement.
//
// Emits a backward jump if the label target is already known, otherwise records a forward
// goto for later patching.
//
// Takes statement (*ast.BranchStmt) which is the goto statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBranchGoto(_ context.Context, statement *ast.BranchStmt) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureGoto, statement.TokPos); err != nil {
		return program.VarLocation{}, err
	}
	label := statement.Label.Name
	if pc, found := c.labelTable[label]; found {
		lo, hi := program.EncodeJumpOffset(c.Function, pc-program.CurrentPC(c.Function)-1)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi)
		return program.VarLocation{}, nil
	}

	jumpPC := program.EmitTier1Jump(c.Function)
	if c.forwardGotos == nil {
		c.forwardGotos = make(map[string][]int)
	}
	c.forwardGotos[label] = append(c.forwardGotos[label], jumpPC)
	return program.VarLocation{}, nil
}

// compileBranchFallthrough compiles a fallthrough statement.
//
// Finds the nearest switch (non-loop) breakable context and records a fallthrough jump.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileBranchFallthrough(_ context.Context) (program.VarLocation, error) {
	for i := range slices.Backward(c.breakables) {
		breakable := &c.breakables[i]
		if !breakable.isLoop {
			jumpPC := program.EmitTier1Jump(c.Function)
			breakable.fallthroughJumps = append(breakable.fallthroughJumps, jumpPC)
			return program.VarLocation{}, nil
		}
	}
	return program.VarLocation{}, fault.ErrCompileFallthroughOutsideSwitch
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
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, temporaryRegister, index)
	program.Emit(c.Function, isa.OpSetUpvalue, temporaryRegister, safeconv.MustIntToUint8(rangeContext.stateFlagUpvalueIndex), uint8(isa.RegisterInt))
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, temporaryRegister)
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

	for i, result := range statement.Results {
		location, err := c.compileExpression(ctx, result)
		if err != nil {
			return program.VarLocation{}, err
		}
		program.Emit(c.Function, isa.OpSetUpvalue, location.Register, safeconv.MustIntToUint8(rangeContext.returnStashUpvalueIndices[i]), uint8(location.Kind))
	}

	returnPendingIndex, err := program.AddIntConstant(c.Function, rangeOverFunctionReturnPendingFlag)
	if err != nil {
		return program.VarLocation{}, err
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, temporaryRegister, returnPendingIndex)
	program.Emit(c.Function, isa.OpSetUpvalue, temporaryRegister, safeconv.MustIntToUint8(rangeContext.stateFlagUpvalueIndex), uint8(isa.RegisterInt))
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, temporaryRegister)

	c.emitYieldReturn(ctx, false)
	return program.VarLocation{}, nil
}

// compileLabeledStmt compiles a labelled statement.
//
// The label is recorded for goto targets and attached to any inner loop/switch for
// labelled break/continue.
//
// Takes statement (*ast.LabeledStmt) which is the labelled statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileLabeledStmt(ctx context.Context, statement *ast.LabeledStmt) (program.VarLocation, error) {
	label := statement.Label.Name

	if c.labelTable == nil {
		c.labelTable = make(map[string]int)
	}
	c.labelTable[label] = program.CurrentPC(c.Function)

	if jumps, ok := c.forwardGotos[label]; ok {
		for _, pc := range jumps {
			program.PatchJump(c.Function, pc)
		}
		delete(c.forwardGotos, label)
	}

	c.pendingLabel = label
	location, err := c.compileStmt(ctx, statement.Stmt)
	c.pendingLabel = ""
	return location, err
}

// consumePendingLabel returns the current pending label and clears it.
//
// Returns the pending label string, or empty string if no label was pending.
func (c *Compiler) consumePendingLabel(_ context.Context) string {
	label := c.pendingLabel
	c.pendingLabel = ""
	return label
}

// compileSwitch compiles a switch statement.
//
// Takes statement (*ast.SwitchStmt) which is the switch statement AST node to compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileSwitch(ctx context.Context, statement *ast.SwitchStmt) (program.VarLocation, error) {
	if statement.Init != nil {
		c.Scopes.PushScope()
		defer c.Scopes.PopScope()
		if _, err := c.compileStmt(ctx, statement.Init); err != nil {
			return program.VarLocation{}, err
		}
	}

	c.breakables = append(c.breakables, breakableContext{
		isLoop: false,
		label:  c.consumePendingLabel(ctx),
	})

	var tagLocation program.VarLocation
	var tagType types.Type
	hasTag := statement.Tag != nil
	if hasTag {
		var err error
		tagLocation, err = c.compileExpression(ctx, statement.Tag)
		if err != nil {
			return program.VarLocation{}, err
		}
		tagType = c.staticTypeOf(statement.Tag)
	}

	cases, defaultCase, err := c.collectSwitchCases(ctx, statement.Body)
	if err != nil {
		return program.VarLocation{}, err
	}
	allCases := make([]*ast.CaseClause, 0, len(cases)+1)
	allCases = append(allCases, cases...)
	if defaultCase != nil {
		allCases = append(allCases, defaultCase)
	}

	var endJumps []int

	for i, cc := range allCases {
		endJump, err := c.compileSwitchCaseClause(ctx, cc, hasTag, tagType, tagLocation, i == len(allCases)-1)
		if err != nil {
			return program.VarLocation{}, err
		}
		if endJump >= 0 {
			endJumps = append(endJumps, endJump)
		}
	}

	for _, pc := range endJumps {
		program.PatchJump(c.Function, pc)
	}
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.breakJumps {
		program.PatchJump(c.Function, pc)
	}
	c.breakables = c.breakables[:len(c.breakables)-1]

	return program.VarLocation{}, nil
}

// compileSwitchCaseClause compiles a single case clause within a switch statement.
//
// Takes cc (*ast.CaseClause) which is the case clause AST node to compile.
// Takes hasTag (bool) which indicates whether the switch has a tag expression.
// Takes tagType (types.Type) which is the static type of the tag expression, used to
// select a scalar-bank comparison when tag and case values share a concrete scalar type.
// Takes tagLocation (VarLocation) which is the location of the compiled tag expression.
// Takes isLastCase (bool) which indicates whether this is the final case in the switch.
//
// Returns the end-of-case jump offset (or -1 if a fallthrough was emitted) and any error.
func (c *Compiler) compileSwitchCaseClause(ctx context.Context,
	cc *ast.CaseClause,
	hasTag bool,
	tagType types.Type,
	tagLocation program.VarLocation,
	isLastCase bool,
) (int, error) {
	var nextCaseJump int
	isDefault := cc.List == nil
	if !isDefault {
		if hasTag {
			nextCaseJump = c.compileCaseMatch(ctx, tagType, tagLocation, cc.List)
		} else {
			nextCaseJump = c.compileCaseCondition(ctx, cc.List)
		}
	}

	c.patchAndClearFallthroughJumps(ctx)

	if err := c.compileScopedBody(ctx, cc.Body); err != nil {
		return -1, err
	}

	breakable := &c.breakables[len(c.breakables)-1]
	hasFallthrough := len(breakable.fallthroughJumps) > 0

	endJump := -1
	if !hasFallthrough || isLastCase {
		endJump = program.EmitTier1Jump(c.Function)
	}

	if !isDefault {
		program.PatchJump(c.Function, nextCaseJump)
	}

	return endJump, nil
}

// patchAndClearFallthroughJumps patches all pending fallthrough jumps in the current
// breakable context to the current PC, then clears the list.
func (c *Compiler) patchAndClearFallthroughJumps(_ context.Context) {
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.fallthroughJumps {
		program.PatchJump(c.Function, pc)
	}
	breakable.fallthroughJumps = breakable.fallthroughJumps[:0]
}

// compileScopedBody compiles a list of statements within a new scope.
//
// Takes statements ([]ast.Stmt) which is the list of statement AST nodes to compile
// within the new scope.
//
// Returns any error encountered during compilation.
func (c *Compiler) compileScopedBody(ctx context.Context, statements []ast.Stmt) error {
	c.Scopes.PushScope()
	for _, bodyStmt := range statements {
		if _, err := c.compileStmt(ctx, bodyStmt); err != nil {
			c.Scopes.PopScope()
			return err
		}
	}
	c.Scopes.PopScope()
	return nil
}

// compileCaseMatch compiles the condition for a tagged switch case using OR logic. Each
// tag-versus-case comparison is routed through emitStaticComparison so a switch over a
// named integer type (its receiver banked general) compares the tag and case in the
// scalar bank rather than as dynamic-type interface values.
//
// Takes tagType (types.Type) which is the static type of the tag expression.
// Takes tagLocation (VarLocation) which is the location of the compiled tag expression.
// Takes exprs ([]ast.Expr) which is the list of case value expressions to compare
// against.
//
// Returns the jump instruction offset to patch for the no-match path.
func (c *Compiler) compileCaseMatch(ctx context.Context, tagType types.Type, tagLocation program.VarLocation, exprs []ast.Expr) int {
	equalityOps, _ := comparisonOpcodes(token.EQL)
	if len(exprs) == 1 {
		valueLocation, _ := c.compileExpression(ctx, exprs[0])
		cmpLocation, _ := c.emitStaticComparison(ctx, token.EQL, equalityOps,
			tagType, c.staticTypeOf(exprs[0]), tagLocation, valueLocation,
		)
		return program.EmitJump(c.Function, isa.OpJumpIfFalse, cmpLocation.Register)
	}

	resultRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadBool), resultRegister, 0)

	for _, expression := range exprs {
		valueLocation, _ := c.compileExpression(ctx, expression)
		cmpLocation, _ := c.emitStaticComparison(ctx, token.EQL, equalityOps,
			tagType, c.staticTypeOf(expression), tagLocation, valueLocation,
		)

		program.Emit(c.Function, isa.OpBitOr, resultRegister, resultRegister, cmpLocation.Register)
	}

	return program.EmitJump(c.Function, isa.OpJumpIfFalse, resultRegister)
}

// compileCaseCondition compiles the condition for a tagless switch case.
//
// Takes exprs ([]ast.Expr) which is the list of boolean case expressions to evaluate.
//
// Returns the jump instruction offset to patch for the no-match path.
func (c *Compiler) compileCaseCondition(ctx context.Context, exprs []ast.Expr) int {
	if len(exprs) == 1 {
		condLocation, _ := c.compileExpression(ctx, exprs[0])
		condLocation = c.ensureIntForBranch(ctx, condLocation)
		return program.EmitJump(c.Function, isa.OpJumpIfFalse, condLocation.Register)
	}

	resultRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadBool), resultRegister, 0)

	for _, expression := range exprs {
		condLocation, _ := c.compileExpression(ctx, expression)
		condLocation = c.ensureIntForBranch(ctx, condLocation)
		program.Emit(c.Function, isa.OpBitOr, resultRegister, resultRegister, condLocation.Register)
	}

	return program.EmitJump(c.Function, isa.OpJumpIfFalse, resultRegister)
}

// collectSwitchCases separates the case clauses from the default clause in a switch body.
//
// Takes body (*ast.BlockStmt) which is the switch body block statement to scan.
//
// Returns the non-default case clauses, the default case clause (or nil), and any error.
func (*Compiler) collectSwitchCases(_ context.Context, body *ast.BlockStmt) ([]*ast.CaseClause, *ast.CaseClause, error) {
	var cases []*ast.CaseClause
	var defaultCase *ast.CaseClause
	for _, s := range body.List {
		cc, ok := s.(*ast.CaseClause)
		if !ok {
			return nil, nil, fmt.Errorf("switch body statement is not a case clause: %T", s)
		}
		if cc.List == nil {
			defaultCase = cc
		} else {
			cases = append(cases, cc)
		}
	}
	return cases, defaultCase, nil
}

// emitCall emits the call instruction for siteIndex and, when it is a plain
// isa.SubOpCall, records its program counter so rewriteTrailingCallAsTailCall can tell
// whether the body ends with an upgradable call without inspecting whatever instruction
// happens to be last. isa.SubOpCallScalar sites are never upgraded, so they are not
// recorded.
//
// Takes subOp (isa.SubOpcode) which is isa.SubOpCall or isa.SubOpCallScalar.
// Takes siteIndex (uint16) which indexes the function's call sites.
func (c *Compiler) emitCall(subOp isa.SubOpcode, siteIndex uint16) {
	c.markCallPosition()
	if subOp == isa.SubOpCall {
		c.lastCallPC = program.CurrentPC(c.Function)
	}
	program.EmitTier1Wide(c.Function, subOp, siteIndex)
}

// resultBank returns the register bank a named result's value belongs to: its own bank,
// or for a captured result the bank the cell was promoted from.
//
// Takes location (program.VarLocation) which is the named result's location.
//
// Returns isa.RegisterKind which is the bank of the canonical return slot.
func resultBank(location program.VarLocation) isa.RegisterKind {
	if location.IsIndirect {
		return location.OriginalKind
	}
	return location.Kind
}

// branchLabelName returns the label name from a branch statement, or the empty string if
// no label is present.
//
// Takes statement (*ast.BranchStmt) which is the branch statement to extract the label
// from.
//
// Returns the label name string, or empty string if unlabelled.
func branchLabelName(statement *ast.BranchStmt) string {
	if statement.Label != nil {
		return statement.Label.Name
	}
	return ""
}
