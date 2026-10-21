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

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

// countedLoop names the registers and PCs the latch of a counted range loop needs.
type countedLoop struct {
	// position is the range statement's for keyword, where loop breakpoints are set.
	position token.Pos

	// loopStart is the PC of the test at the top.
	loopStart int

	// bodyStart is the PC the test at the top falls into.
	bodyStart int

	// indexRegister is the int register counting the iterations.
	indexRegister uint8

	// limitRegister is the int register holding the count to stop at.
	limitRegister uint8
}

// snapshotArrayRangeCollection takes a defensive snapshot of the underlying array when
// the range subject is an array (rather than a slice), so subsequent indexing reads from
// the snapshot's general register.
//
// Takes statement (*ast.RangeStmt) which carries the range subject.
// Takes collectionLocation (VarLocation) which is the original collection location.
//
// Returns VarLocation which is the (possibly rewritten) collection location after
// snapshotting.
func (c *Compiler) snapshotArrayRangeCollection(statement *ast.RangeStmt, collectionLocation program.VarLocation) program.VarLocation {
	if _, ok := c.Info.Types[statement.X].Type.Underlying().(*types.Array); !ok {
		return collectionLocation
	}
	snapshotRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDereference, snapshotRegister, collectionLocation.Register, engine.DerefSnapshot)
	return program.VarLocation{Register: snapshotRegister, Kind: isa.RegisterGeneral}
}

// emitSliceRangeLength emits the len() opcode for the collection, preferring a typed
// direct sub-op when the collection's static type supports it.
//
// Takes collectionLocation (VarLocation) which is the collection whose length is
// required.
//
// Returns the int-bank register holding the collection length.
func (c *Compiler) emitSliceRangeLength(collectionLocation program.VarLocation) uint8 {
	lengthRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	if directLenSubOp, ok := isaselect.TypedSliceDirectLenSubOp(collectionLocation.Kind); ok {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(directLenSubOp), lengthRegister, collectionLocation.Register)
	} else {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLengthGeneral), lengthRegister, collectionLocation.Register)
	}
	return lengthRegister
}

// emitSliceRangeZeroIndex allocates the loop's index register and initialises it to zero.
//
// Returns the index register and any error from registering the zero constant in the
// int-pool.
func (c *Compiler) emitSliceRangeZeroIndex() (uint8, error) {
	indexRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	zeroIndex, err := program.AddIntConstant(c.Function, 0)
	if err != nil {
		return 0, err
	}
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, indexRegister, zeroIndex)
	return indexRegister, nil
}

// emitSliceRangeLatch emits the index increment and the way back into a range loop over a
// slice, an array or a byte view.
//
// The words carry the position of the for keyword, so a breakpoint on the loop's line
// stops in every iteration though its test at the top runs once.
//
// Takes loop (countedLoop) which names the loop's registers and PCs.
//
// Returns any error encountered when registering the increment constant.
func (c *Compiler) emitSliceRangeLatch(ctx context.Context, loop countedLoop) error {
	if !c.loopTestsAtBottom() {
		return c.emitSliceRangeIncrementAndJump(loop.indexRegister, loop.loopStart)
	}
	c.setDebugPosition(ctx, loop.position)
	program.EmitTier2(c.Function, isa.SubOpTier2IncrementInt64, loop.indexRegister)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpLessThanInt64, comparisonRegister, loop.indexRegister, loop.limitRegister)
	c.emitJumpBackIfTrue(comparisonRegister, loop.bodyStart)
	return nil
}

// emitSliceRangeIncrementAndJump emits the index increment and the unconditional jump
// back to the loop header.
//
// Takes indexRegister (uint8) which is the loop's index register.
// Takes loopStart (int) which is the PC of the loop header to jump to.
//
// Returns any error encountered when registering the increment constant.
func (c *Compiler) emitSliceRangeIncrementAndJump(indexRegister uint8, loopStart int) error {
	oneIndex, oneErr := program.AddIntConstant(c.Function, 1)
	if oneErr != nil {
		return oneErr
	}
	temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, oneIndex)
	program.Emit(c.Function, isa.OpAddInt64, indexRegister, indexRegister, temporaryRegister)

	backOffset := loopStart - program.CurrentPC(c.Function) - 1
	lo, hi := program.EncodeJumpOffset(c.Function, backOffset)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi)
	return nil
}

// checkRangeLoopAllowed refuses a range loop the feature policy disables, or whose
// declared variables the file's Go version would share across iterations
// (checkLoopVariableLanguageVersion).
//
// Takes statement (*ast.RangeStmt) which is the loop.
//
// Returns the refusal, or nil.
func (c *Compiler) checkRangeLoopAllowed(statement *ast.RangeStmt) error {
	if err := c.checkFeature(policy.InterpFeatureRangeLoops, statement.For); err != nil {
		return err
	}
	if statement.Tok != token.DEFINE {
		return nil
	}
	return c.checkLoopVariableLanguageVersion(statement.For, statement.Key, statement.Value)
}

// compileForRange compiles a for-range statement.
//
// Takes statement (*ast.RangeStmt) which is the AST range statement to compile.
//
// Returns a zero VarLocation and an error if compilation of any part of the range loop
// fails.
func (c *Compiler) compileForRange(ctx context.Context, statement *ast.RangeStmt) (program.VarLocation, error) {
	if err := c.checkRangeLoopAllowed(statement); err != nil {
		return program.VarLocation{}, err
	}
	if c.rangeTargetsNeedTemps(statement) {
		return c.compileRangeAssignTargets(ctx, statement)
	}
	c.Scopes.PushScope()
	defer c.Scopes.PopScope()
	c.loopDepth++
	defer func() { c.loopDepth-- }()

	collectionLocation, err := c.compileExpression(ctx, statement.X)
	if err != nil {
		return program.VarLocation{}, err
	}

	rangeType, ok := c.underlyingTypeOf(statement.X)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for range expression at %s", fault.ErrCompilation, c.positionString(statement.X.Pos()))
	}
	if basic, ok := rangeType.(*types.Basic); ok && isIntegerBasicKind(basic.Kind()) {
		return c.compileIntRange(ctx, statement, collectionLocation)
	}

	if _, isSlice := rangeType.(*types.Slice); isSlice && collectionLocation.Kind == isa.RegisterString {
		return c.compileByteViewRange(ctx, statement, collectionLocation)
	}

	if signature, ok := rangeType.(*types.Signature); ok {
		c.boxToGeneral(ctx, &collectionLocation)
		return c.compileRangeOverFunction(ctx, statement, collectionLocation, signature)
	}

	if isa.IsTypedSliceKind(collectionLocation.Kind) {
		switch rangeType.(type) {
		case *types.Slice, *types.Array:
			return c.compileSliceRange(ctx, statement, collectionLocation)
		}
	}

	c.boxToGeneral(ctx, &collectionLocation)

	switch rangeType.(type) {
	case *types.Slice, *types.Array:
		return c.compileSliceRange(ctx, statement, collectionLocation)
	}

	return c.compileGenericRange(ctx, statement, collectionLocation)
}

// compileGenericRange compiles a for-range over maps, channels, and strings using the
// isa.SubOpRangeInitialise/isa.SubOpRangeNext generic path.
//
// Takes statement (*ast.RangeStmt) which is the AST range statement to compile.
// Takes collectionLocation (VarLocation) which is the register location of the collection
// to iterate.
//
// Returns a zero VarLocation and an error if compilation fails.
func (c *Compiler) compileGenericRange(ctx context.Context, statement *ast.RangeStmt, collectionLocation program.VarLocation) (program.VarLocation, error) {
	iteratorRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitTier1(c.Function, isa.SubOpRangeInitialise, iteratorRegister, collectionLocation.Register)

	c.breakables = append(c.breakables, breakableContext{
		isLoop: true,
		label:  c.consumePendingLabel(ctx),
	})

	doneRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	keyLocation, valueLocation, err := c.declareRangeKeyVal(ctx, statement)
	if err != nil {
		return program.VarLocation{}, err
	}
	keyDestination := c.rangeNextDestination(keyLocation)
	valueDestination := c.rangeNextDestination(valueLocation)

	loopStart := program.CurrentPC(c.Function)

	program.EmitTier1(c.Function, isa.SubOpRangeNext, iteratorRegister, doneRegister)

	c.emitRangeNextExt(ctx, statement, keyDestination, valueDestination)

	jumpToEnd := program.EmitJump(c.Function, isa.OpJumpIfFalse, doneRegister)

	c.storeSpilledRangeVar(ctx, keyLocation, keyDestination)
	c.storeSpilledRangeVar(ctx, valueLocation, valueDestination)
	c.emitRangeSharedCellResets(statement, keyLocation, valueLocation)

	if _, err := c.compileStmt(ctx, statement.Body); err != nil {
		return program.VarLocation{}, err
	}

	c.patchContinueJumps(ctx)

	backOffset := loopStart - program.CurrentPC(c.Function) - 1
	lo, hi := program.EncodeJumpOffset(c.Function, backOffset)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpJump), lo, hi)

	program.PatchJump(c.Function, jumpToEnd)
	c.patchBreakJumpsAndPop(ctx)

	return program.VarLocation{}, nil
}

// rangeNextDestination returns the range variable's register or a temporary for a spilled
// variable, since isa.SubOpRangeNext only addresses registers.
//
// Takes location (program.VarLocation) which is the range variable, or the zero location
// when the variable is absent or blank.
//
// Returns program.VarLocation which is the destination register.
func (c *Compiler) rangeNextDestination(location program.VarLocation) program.VarLocation {
	if !location.IsSpilled {
		return location
	}
	return program.VarLocation{Register: c.Scopes.Alloc.Alloc(location.Kind), Kind: location.Kind}
}

// storeSpilledRangeVar copies a range variable from the temporary isa.SubOpRangeNext
// wrote into its spill slot; a variable held in a register needs nothing.
//
// Takes location (program.VarLocation) which is the range variable.
// Takes destination (program.VarLocation) which is the register the iteration wrote.
func (c *Compiler) storeSpilledRangeVar(ctx context.Context, location, destination program.VarLocation) {
	if !location.IsSpilled {
		return
	}
	c.emitSpillStore(ctx, destination.Register, location.Kind, location.SpillSlot)
}

// emitRangeSharedCellResets resets the shared cells holding range-key and range-value
// variables when the body captures them.
//
// When the range body contains a function literal that captures one of the variables, the
// Reset prevents every captured closure from observing the same mutated cell across
// iterations.
//
// Takes statement (*ast.RangeStmt) which is the range statement being compiled.
// Takes keyLocation (VarLocation) which holds the range key variable.
// Takes valueLocation (VarLocation) which holds the range value variable.
func (c *Compiler) emitRangeSharedCellResets(statement *ast.RangeStmt, keyLocation, valueLocation program.VarLocation) {
	if statement.Tok != token.DEFINE || !bodyContainsFunctionLit(statement.Body) {
		return
	}
	c.emitRangeSharedCellResetFor(statement.Key, keyLocation)
	c.emitRangeSharedCellResetFor(statement.Value, valueLocation)
}

// emitRangeSharedCellResetFor emits an isa.SubOpResetSharedCell for the given identifier
// when it is a captured range variable.
//
// Takes expression (ast.Expr) which is the identifier expression for the range key or
// value.
// Takes location (VarLocation) which holds the range variable.
func (c *Compiler) emitRangeSharedCellResetFor(expression ast.Expr, location program.VarLocation) {
	if expression == nil || isBlankIdent(expression) || location.IsSpilled {
		return
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok || !c.closureCapturedNames[identifier.Name] {
		return
	}
	program.EmitTier1(c.Function, isa.SubOpResetSharedCell, location.Register, uint8(location.Kind))
}

// declareRangeKeyVal declares or resolves the key and value variables for a generic
// for-range loop, returning their locations.
//
// Takes statement (*ast.RangeStmt) which is the range statement whose key and value
// variables are declared.
//
// Returns the key and value variable locations, or an error if the key or value is not an
// identifier.
func (c *Compiler) declareRangeKeyVal(_ context.Context, statement *ast.RangeStmt) (keyLocation, valueLocation program.VarLocation, err error) {
	if statement.Key != nil && !isBlankIdent(statement.Key) {
		keyLocation, err = c.declareRangeVar(statement.Key, statement.Tok, "key")
		if err != nil {
			return program.VarLocation{}, program.VarLocation{}, err
		}
	}
	if statement.Value != nil && !isBlankIdent(statement.Value) {
		valueLocation, err = c.declareRangeVar(statement.Value, statement.Tok, "value")
		if err != nil {
			return program.VarLocation{}, program.VarLocation{}, err
		}
	}
	return keyLocation, valueLocation, nil
}

// declareRangeVar declares (for a `:=` range) or resolves (for an `=` range) the single
// range variable named by expression.
//
// Takes expression (ast.Expr) which must be the key or value identifier of a range
// statement.
// Takes rangeToken (token.Token) which is the range statement's assignment token,
// distinguishing declaration from reuse.
// Takes role (string) which names the variable ("key" or "value") for diagnostic
// messages.
//
// Returns the variable's location, or an error when the expression is not an identifier
// or its type information is missing.
func (c *Compiler) declareRangeVar(expression ast.Expr, rangeToken token.Token, role string) (program.VarLocation, error) {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("%w: range %s is not an identifier (%T) at %s", fault.ErrCompilation, role, expression, c.positionString(expression.Pos()))
	}
	if rangeToken != token.DEFINE {
		location, _ := c.Scopes.LookupVar(identifier.Name)
		return location, nil
	}
	typeObject := c.Info.Defs[identifier]
	if typeObject == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for range %s %q at %s", fault.ErrCompilation, role, identifier.Name, c.positionString(identifier.Pos()))
	}
	return c.Scopes.DeclareVar(identifier.Name, c.kindFor(typeObject.Type())), nil
}

// emitRangeNextExt emits the extension words for isa.SubOpRangeNext encoding key and
// value destinations.
//
// Takes statement (*ast.RangeStmt) which is the range statement to determine which
// variables are active.
// Takes keyLocation (VarLocation) which is the register location for the key variable.
// Takes valueLocation (VarLocation) which is the register location for the value
// variable.
func (c *Compiler) emitRangeNextExt(_ context.Context, statement *ast.RangeStmt, keyLocation, valueLocation program.VarLocation) {
	hasKey := statement.Key != nil && !isBlankIdent(statement.Key)
	hasValue := statement.Value != nil && !isBlankIdent(statement.Value)

	keyRegister := uint8(0)
	keyKind := uint8(0)
	if hasKey {
		keyRegister = keyLocation.Register
		keyKind = uint8(keyLocation.Kind)
	}
	valueRegister := uint8(0)
	valueKind := uint8(0)
	if hasValue {
		valueRegister = valueLocation.Register
		valueKind = uint8(valueLocation.Kind)
	}

	flags := uint8(0)
	if hasKey {
		flags |= rangeKeyFlag
	}
	if hasValue {
		flags |= rangeValueFlag
	}
	program.Emit(c.Function, isa.OpExtension, flags, keyRegister, keyKind)
	program.Emit(c.Function, isa.OpExtension, 0, valueRegister, valueKind)
}

// patchBreakJumpsAndPop patches all break jumps in the current breakable context and pops
// it from the stack.
func (c *Compiler) patchBreakJumpsAndPop(_ context.Context) {
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.breakJumps {
		program.PatchJump(c.Function, pc)
	}
	c.breakables = c.breakables[:len(c.breakables)-1]
}

// compileSliceRange compiles a range over a slice or array.
//
// Takes statement (*ast.RangeStmt) which is the AST range statement to compile.
// Takes collectionLocation (VarLocation) which is the register location of the slice or
// array collection.
//
// Returns program.VarLocation which is zero.
// Returns error which reports a compilation failure.
func (c *Compiler) compileSliceRange(ctx context.Context, statement *ast.RangeStmt, collectionLocation program.VarLocation) (program.VarLocation, error) {
	collectionLocation = c.snapshotArrayRangeCollection(statement, collectionLocation)
	lengthRegister := c.emitSliceRangeLength(collectionLocation)

	indexRegister, err := c.emitSliceRangeZeroIndex()
	if err != nil {
		return program.VarLocation{}, err
	}

	c.breakables = append(c.breakables, breakableContext{
		isLoop: true,
		label:  c.consumePendingLabel(ctx),
	})

	loopStart := program.CurrentPC(c.Function)

	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpLessThanInt64, comparisonRegister, indexRegister, lengthRegister)
	jumpToEnd := program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
	bodyStart := program.CurrentPC(c.Function)

	needsReset := bodyContainsFunctionLit(statement.Body)
	if err := c.emitSliceRangeKey(ctx, statement, indexRegister, needsReset); err != nil {
		return program.VarLocation{}, err
	}
	if err := c.emitSliceRangeValue(ctx, statement, collectionLocation, indexRegister, needsReset); err != nil {
		return program.VarLocation{}, err
	}

	if _, err := c.compileStmt(ctx, statement.Body); err != nil {
		return program.VarLocation{}, err
	}

	c.patchContinueJumps(ctx)

	latch := countedLoop{position: statement.For, loopStart: loopStart, bodyStart: bodyStart, indexRegister: indexRegister, limitRegister: lengthRegister}
	if err := c.emitSliceRangeLatch(ctx, latch); err != nil {
		return program.VarLocation{}, err
	}

	program.PatchJump(c.Function, jumpToEnd)
	c.patchBreakJumpsAndPop(ctx)

	return program.VarLocation{}, nil
}

// emitSliceRangeKey declares and populates the key variable for a slice/array range loop,
// if present.
//
// Takes statement (*ast.RangeStmt) which is the range statement whose key variable is
// populated.
// Takes indexRegister (uint8) which is the register holding the current loop index.
// Takes needsResetSharedCell (bool) which indicates whether the range body contains
// closures that may capture the key variable.
//
// Returns an error if the key expression is not an identifier.
func (c *Compiler) emitSliceRangeKey(ctx context.Context, statement *ast.RangeStmt, indexRegister uint8, needsResetSharedCell bool) error {
	hasKey := statement.Key != nil && !isBlankIdent(statement.Key)
	if !hasKey {
		return nil
	}
	if statement.Tok != token.DEFINE {
		return c.emitRangeKeyAssign(ctx, statement, indexRegister, isa.RegisterInt64)
	}
	keyIdent, ok := statement.Key.(*ast.Ident)
	if !ok {
		return fmt.Errorf("range key is not an identifier: %T", statement.Key)
	}
	keyLocation := c.Scopes.DeclareVar(keyIdent.Name, isa.RegisterInt64)
	if needsResetSharedCell && !keyLocation.IsSpilled && c.closureCapturedNames[keyIdent.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, keyLocation.Register, uint8(keyLocation.Kind))
	}
	if keyLocation.IsSpilled {
		c.emitSpillStore(ctx, indexRegister, isa.RegisterInt64, keyLocation.SpillSlot)
	} else {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), keyLocation.Register, indexRegister)
	}
	return nil
}

// emitSliceRangeValue declares and populates the value variable for a slice/array range
// loop, using typed fast-paths where possible.
//
// Takes statement (*ast.RangeStmt) which is the range statement whose value variable is
// populated.
// Takes collectionLocation (VarLocation) which is the register location of the collection
// being iterated.
// Takes indexRegister (uint8) which is the register holding the current loop index.
// Takes needsResetSharedCell (bool) which indicates whether the range body contains
// closures that may capture the value variable.
//
// Returns an error if the value expression is not an identifier.
func (c *Compiler) emitSliceRangeValue(ctx context.Context, statement *ast.RangeStmt, collectionLocation program.VarLocation, indexRegister uint8, needsResetSharedCell bool) error {
	hasValue := statement.Value != nil && !isBlankIdent(statement.Value)
	if !hasValue {
		return nil
	}
	if statement.Tok != token.DEFINE {
		return c.emitSliceRangeValueAssign(ctx, statement, collectionLocation, indexRegister)
	}

	valueIdentifier, ok := statement.Value.(*ast.Ident)
	if !ok {
		return fmt.Errorf("%w: range value is not an identifier (%T) at %s", fault.ErrCompilation, statement.Value, c.positionString(statement.Value.Pos()))
	}
	typeObject := c.Info.Defs[valueIdentifier]
	if typeObject == nil {
		return fmt.Errorf("%w: missing type information for range value %q at %s", fault.ErrCompilation, valueIdentifier.Name, c.positionString(valueIdentifier.Pos()))
	}
	valueKind := c.kindFor(typeObject.Type())

	valueLocation := c.Scopes.DeclareVar(valueIdentifier.Name, valueKind)
	if needsResetSharedCell && !valueLocation.IsSpilled && c.closureCapturedNames[valueIdentifier.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, valueLocation.Register, uint8(valueLocation.Kind))
	}

	target := valueLocation
	if valueLocation.IsSpilled {
		target = program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(valueKind), Kind: valueKind}
	}
	done, err := c.emitDirectSliceRangeValue(ctx, statement, valueIdentifier, target, collectionLocation, indexRegister, typeObject.Type())
	if valueLocation.IsSpilled {
		if done && err == nil {
			c.emitSpillStore(ctx, target.Register, valueKind, valueLocation.SpillSlot)
		}
		c.Scopes.Alloc.FreeTemp(valueKind, target.Register)
	}
	if done || err != nil {
		return err
	}

	generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpGetElement, generalRegister, collectionLocation.Register, indexRegister)
	c.storeGeneralRangeValue(ctx, generalRegister, valueLocation, valueKind, typeObject.Type())
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
	return nil
}

// emitDirectSliceRangeValue handles the unspilled range value: a typed-bank element uses
// the typed slice get, and a general-bank element is indexed straight into the value's
// register.
//
// Takes statement (*ast.RangeStmt) which is the range loop.
// Takes valueIdentifier (*ast.Ident) which is the loop's value variable.
// Takes valueLocation (VarLocation) which is the value's register.
// Takes collectionLocation (VarLocation) which holds the ranged slice.
// Takes indexRegister (uint8) which holds the current index.
// Takes valueType (types.Type) which is the element's static type.
//
// Returns true when the value was emitted, and any error from type resolution.
func (c *Compiler) emitDirectSliceRangeValue(ctx context.Context,
	statement *ast.RangeStmt,
	valueIdentifier *ast.Ident,
	valueLocation, collectionLocation program.VarLocation,
	indexRegister uint8,
	valueType types.Type,
) (bool, error) {
	rangeType, ok := c.underlyingTypeOf(statement.X)
	if !ok {
		return false, fmt.Errorf("%w: missing type information for range expression at %s", fault.ErrCompilation, c.positionString(statement.X.Pos()))
	}
	if c.emitTypedSliceGet(ctx, valueLocation, collectionLocation, indexRegister, rangeType, true) {
		return true, nil
	}
	if valueLocation.Kind != isa.RegisterGeneral {
		return false, nil
	}
	c.emitGeneralRangeValueInPlace(statement, valueIdentifier, valueLocation, collectionLocation.Register, indexRegister, valueType)
	return true, nil
}

// emitGeneralRangeValueInPlace indexes the element straight into the range value's
// general register and, when the element type needs value semantics, snapshots it in
// place; a struct value the body only reads is marked as an alias candidate for
// AliasReadOnlyRangeValues to resolve once heap purity is known.
//
// Takes statement (*ast.RangeStmt) which is the range loop.
// Takes valueIdentifier (*ast.Ident) which is the loop's value variable.
// Takes valueLocation (VarLocation) which is the value's general register.
// Takes collectionRegister (uint8) which holds the ranged slice.
// Takes indexRegister (uint8) which holds the current index.
// Takes valueType (types.Type) which is the element's static type.
func (c *Compiler) emitGeneralRangeValueInPlace(statement *ast.RangeStmt, valueIdentifier *ast.Ident, valueLocation program.VarLocation,
	collectionRegister, indexRegister uint8, valueType types.Type) {
	register := valueLocation.Register
	program.Emit(c.Function, isa.OpGetElement, register, collectionRegister, indexRegister)
	mode := generalMoveModeFor(valueType)
	if mode == engine.MoveGeneralModeAlias {
		return
	}
	if mode == engine.MoveGeneralModeSnapshot && c.rangeValueMayAlias(statement, valueIdentifier) {
		mode = isa.MoveGeneralModeSnapshotRangeCandidate
	}
	program.Emit(c.Function, isa.OpMoveGeneral, register, register, mode)
}

// storeGeneralRangeValue moves a general-bank range element produced by isa.OpGetElement
// into the range value variable, unpacking it into a typed bank or spill slot as the
// variable's location requires.
//
// Takes generalRegister (uint8) which holds the indexed element.
// Takes valueLocation (VarLocation) which is the range value variable.
// Takes valueKind (isa.RegisterKind) which is the variable's register bank.
// Takes valueType (types.Type) which is the variable's static type, used to pick the
// general move mode for general-bank destinations.
func (c *Compiler) storeGeneralRangeValue(ctx context.Context, generalRegister uint8, valueLocation program.VarLocation, valueKind isa.RegisterKind, valueType types.Type) {
	if valueLocation.IsSpilled {
		if valueKind != isa.RegisterGeneral {
			scratch := c.Scopes.Alloc.AllocTemp(valueKind)
			program.Emit(c.Function, isa.OpUnboxFromInterface, scratch, generalRegister, uint8(valueKind))
			c.emitSpillStore(ctx, scratch, valueKind, valueLocation.SpillSlot)
			c.Scopes.Alloc.FreeTemp(valueKind, scratch)
		} else {
			c.emitSpillStore(ctx, generalRegister, isa.RegisterGeneral, valueLocation.SpillSlot)
		}
		return
	}
	if valueKind != isa.RegisterGeneral {
		program.Emit(c.Function, isa.OpUnboxFromInterface, valueLocation.Register, generalRegister, uint8(valueKind))
		return
	}
	program.Emit(c.Function, isa.OpMoveGeneral, valueLocation.Register, generalRegister, generalMoveModeFor(valueType))
}

// emitTypedSliceGet emits a typed slice get instruction if the element type matches a
// fast-path register kind.
//
// Takes valueLocation (VarLocation) which receives the element.
// Takes collectionLocation (VarLocation) which holds the slice.
// Takes indexRegister (uint8) which holds the element index.
// Takes rangeType (types.Type) which is the collection's underlying type.
// Takes boundsSafe (bool) which selects the bounds-unchecked opcode when the index is
// proven in range.
//
// Returns true when a typed fast-path instruction was emitted.
func (c *Compiler) emitTypedSliceGet(ctx context.Context, valueLocation, collectionLocation program.VarLocation, indexRegister uint8, rangeType types.Type, boundsSafe bool) bool {
	elementRegisterKind, ok := c.sliceElemRegisterKind(rangeType)
	if !ok || valueLocation.Kind != elementRegisterKind {
		return false
	}
	plan, ok := isaselect.PlanSliceGet(collectionLocation.Kind, elementRegisterKind, boundsSafe)
	if !ok {
		return false
	}
	c.emitSliceGet(ctx, plan, valueLocation, collectionLocation, program.VarLocation{Register: indexRegister, Kind: isa.RegisterInt64})
	return true
}

// compileIntRange compiles a range over an integer.
//
// Takes statement (*ast.RangeStmt) which is the AST range statement to compile.
// Takes limitLocation (VarLocation) which is the register location holding the upper
// bound integer.
//
// Returns program.VarLocation which is zero.
// Returns error which reports a compilation failure.
func (c *Compiler) compileIntRange(ctx context.Context, statement *ast.RangeStmt, limitLocation program.VarLocation) (program.VarLocation, error) {
	indexRegister := c.emitIntRangeInitialise(ctx, limitLocation)

	c.breakables = append(c.breakables, breakableContext{
		isLoop: true,
		label:  c.consumePendingLabel(ctx),
	})

	loopStart := program.CurrentPC(c.Function)

	jumpToEnd := c.emitIntRangeCondition(ctx, indexRegister, limitLocation)
	bodyStart := program.CurrentPC(c.Function)

	needsReset := bodyContainsFunctionLit(statement.Body)
	if err := c.emitIntRangeKey(ctx, statement, indexRegister, limitLocation.Kind, needsReset); err != nil {
		return program.VarLocation{}, err
	}

	if _, err := c.compileStmt(ctx, statement.Body); err != nil {
		return program.VarLocation{}, err
	}

	c.patchContinueJumps(ctx)

	c.emitIntRangeLatch(ctx, statement, indexRegister, limitLocation, loopStart, bodyStart)

	program.PatchJump(c.Function, jumpToEnd)
	c.patchBreakJumpsAndPop(ctx)

	return program.VarLocation{}, nil
}

// emitIntRangeLatch emits the index increment and the way back into an integer range
// loop.
//
// Takes statement (*ast.RangeStmt) which is the loop.
// Takes indexRegister (uint8) which holds the index.
// Takes limitLocation (program.VarLocation) which holds the upper bound.
// Takes loopStart (int) which is the PC of the test at the top.
// Takes bodyStart (int) which is the PC the test at the top falls into.
func (c *Compiler) emitIntRangeLatch(ctx context.Context, statement *ast.RangeStmt, indexRegister uint8, limitLocation program.VarLocation, loopStart, bodyStart int) {
	if !c.loopTestsAtBottom() {
		c.emitIntRangeIncrement(ctx, indexRegister, limitLocation.Kind)
		c.emitJumpBack(loopStart)
		return
	}
	c.setDebugPosition(ctx, statement.For)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	if limitLocation.Kind == isa.RegisterUint64 {
		c.emitIntRangeIncrement(ctx, indexRegister, limitLocation.Kind)
		program.Emit(c.Function, isa.OpLessThanUint64, comparisonRegister, indexRegister, limitLocation.Register)
	} else {
		program.EmitTier2(c.Function, isa.SubOpTier2IncrementInt64, indexRegister)
		program.Emit(c.Function, isa.OpLessThanInt64, comparisonRegister, indexRegister, limitLocation.Register)
	}
	c.emitJumpBackIfTrue(comparisonRegister, bodyStart)
}

// emitIntRangeInitialise allocates and zero-initialises the index counter register for an
// integer range loop.
//
// Takes limitLocation (VarLocation) whose Kind selects the register bank.
//
// Returns the allocated index register number.
func (c *Compiler) emitIntRangeInitialise(_ context.Context, limitLocation program.VarLocation) uint8 {
	indexRegister := c.Scopes.Alloc.Alloc(limitLocation.Kind)
	switch limitLocation.Kind {
	case isa.RegisterUint64:
		zeroIndex, err := program.AddUintConstant(c.Function, 0)
		if err != nil {
			c.recordStickyError(err)
			return indexRegister
		}
		program.EmitWide(c.Function, isa.OpLoadUint64Constant, indexRegister, zeroIndex)
	default:
		zeroIndex, err := program.AddIntConstant(c.Function, 0)
		if err != nil {
			c.recordStickyError(err)
			return indexRegister
		}
		program.EmitWide(c.Function, isa.OpLoadInt64Constant, indexRegister, zeroIndex)
	}
	return indexRegister
}

// emitIntRangeCondition emits the comparison and conditional jump for the integer range
// loop.
//
// Takes indexRegister (uint8) which holds the loop index.
// Takes limitLocation (VarLocation) which holds the upper bound.
//
// Returns the jump instruction PC to patch when the loop exits.
func (c *Compiler) emitIntRangeCondition(_ context.Context, indexRegister uint8, limitLocation program.VarLocation) int {
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	switch limitLocation.Kind {
	case isa.RegisterUint64:
		program.Emit(c.Function, isa.OpLessThanUint64, comparisonRegister, indexRegister, limitLocation.Register)
	default:
		program.Emit(c.Function, isa.OpLessThanInt64, comparisonRegister, indexRegister, limitLocation.Register)
	}
	return program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
}

// emitIntRangeKey declares or resolves the key variable and emits a move from the index
// register.
//
// Takes statement (*ast.RangeStmt) whose key variable is assigned.
// Takes indexRegister (uint8) which holds the loop index.
// Takes kind (isa.RegisterKind) which selects the key variable's bank.
// Takes needsResetSharedCell (bool) which is true when the body captures the key in a
// closure.
//
// Returns an error if the key expression is not an identifier.
func (c *Compiler) emitIntRangeKey(ctx context.Context, statement *ast.RangeStmt, indexRegister uint8, kind isa.RegisterKind, needsResetSharedCell bool) error {
	hasKey := statement.Key != nil && !isBlankIdent(statement.Key)
	if !hasKey {
		return nil
	}

	if statement.Tok != token.DEFINE {
		return c.emitRangeKeyAssign(ctx, statement, indexRegister, kind)
	}
	keyIdent, ok := statement.Key.(*ast.Ident)
	if !ok {
		return fmt.Errorf("range key is not an identifier: %T", statement.Key)
	}
	keyLocation := c.Scopes.DeclareVar(keyIdent.Name, kind)
	if needsResetSharedCell && !keyLocation.IsSpilled && c.closureCapturedNames[keyIdent.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, keyLocation.Register, uint8(keyLocation.Kind))
	}

	if keyLocation.IsSpilled {
		c.emitSpillStore(ctx, indexRegister, kind, keyLocation.SpillSlot)
	} else {
		switch kind {
		case isa.RegisterUint64:
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveUint64), keyLocation.Register, indexRegister)
		default:
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), keyLocation.Register, indexRegister)
		}
	}
	return nil
}

// emitIntRangeIncrement emits the index++ operation for an integer range loop, using the
// appropriate typed opcode.
//
// Takes indexRegister (uint8) which holds the index.
// Takes kind (isa.RegisterKind) which selects the typed add opcode.
func (c *Compiler) emitIntRangeIncrement(_ context.Context, indexRegister uint8, kind isa.RegisterKind) {
	switch kind {
	case isa.RegisterUint64:
		oneIndex, err := program.AddUintConstant(c.Function, 1)
		if err != nil {
			c.recordStickyError(err)
			return
		}
		temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterUint64)
		program.EmitWide(c.Function, isa.OpLoadUint64Constant, temporaryRegister, oneIndex)
		program.Emit(c.Function, isa.OpAddUint64, indexRegister, indexRegister, temporaryRegister)
	default:
		oneIndex, err := program.AddIntConstant(c.Function, 1)
		if err != nil {
			c.recordStickyError(err)
			return
		}
		temporaryRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
		program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, oneIndex)
		program.Emit(c.Function, isa.OpAddInt64, indexRegister, indexRegister, temporaryRegister)
	}
}

// emitRangeKeyAssign stores the loop index into the assignment-form key target (`for i =
// range xs`, `for a[f()] = range xs`) through the general assignment path, which covers
// identifiers living in a cell, a global slot or an upvalue as well as index, field and
// dereference targets.
//
// Takes statement (*ast.RangeStmt) whose Key is the target expression.
// Takes indexRegister (uint8) which holds the current index.
// Takes kind (isa.RegisterKind) which is the index register's bank.
//
// Returns an error when the target cannot be assigned.
func (c *Compiler) emitRangeKeyAssign(ctx context.Context, statement *ast.RangeStmt, indexRegister uint8, kind isa.RegisterKind) error {
	_, err := c.emitAssignTarget(ctx, statement.Key, program.VarLocation{Register: indexRegister, Kind: kind})
	return err
}

// emitSliceRangeValueAssign stores the current element into the assignment-form value
// target for a range loop.
//
// Takes statement (*ast.RangeStmt) whose Value is the target expression.
// Takes collectionLocation (program.VarLocation) which holds the ranged collection.
// Takes indexRegister (uint8) which holds the current index.
//
// Returns an error when type information is missing or the target cannot be assigned.
func (c *Compiler) emitSliceRangeValueAssign(ctx context.Context, statement *ast.RangeStmt, collectionLocation program.VarLocation, indexRegister uint8) error {
	rangeType, ok := c.underlyingTypeOf(statement.X)
	if !ok {
		return fmt.Errorf("%w: missing type information for range expression at %s", fault.ErrCompilation, c.positionString(statement.X.Pos()))
	}
	valueType := c.Info.TypeOf(statement.Value)
	if valueType == nil {
		return fmt.Errorf("%w: missing type information for range value at %s", fault.ErrCompilation, c.positionString(statement.Value.Pos()))
	}
	valueKind := c.kindFor(valueType)
	temp := program.VarLocation{Register: c.Scopes.Alloc.AllocTemp(valueKind), Kind: valueKind, SourceType: c.exactReflectTypeForBoxing(ctx, valueType)}
	if !c.emitTypedSliceGet(ctx, temp, collectionLocation, indexRegister, rangeType, true) {
		if valueKind == isa.RegisterGeneral {
			program.Emit(c.Function, isa.OpGetElement, temp.Register, collectionLocation.Register, indexRegister)
			if mode := generalMoveModeFor(valueType); mode != engine.MoveGeneralModeAlias {
				program.Emit(c.Function, isa.OpMoveGeneral, temp.Register, temp.Register, mode)
			}
		} else {
			general := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
			program.Emit(c.Function, isa.OpGetElement, general, collectionLocation.Register, indexRegister)
			c.storeGeneralRangeValue(ctx, general, temp, valueKind, valueType)
			c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, general)
		}
	}
	_, err := c.emitAssignTarget(ctx, statement.Value, temp)
	c.Scopes.Alloc.FreeTemp(valueKind, temp.Register)
	return err
}
