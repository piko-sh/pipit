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

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/compile/inline"

	"pipit.sh/pipit/internal/engine"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

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
	program.Emit(c.Function, isa.OpDereference, tempGen, location.Register, 0)
	if location.OriginalKind == isa.RegisterGeneral {
		return program.VarLocation{Register: tempGen, Kind: isa.RegisterGeneral}, nil
	}
	dest := c.Scopes.Alloc.Alloc(location.OriginalKind)
	program.Emit(c.Function, isa.OpUnboxFromInterface, dest, tempGen, uint8(location.OriginalKind))
	c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, tempGen)
	return program.VarLocation{Register: dest, Kind: location.OriginalKind}, nil
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
	case isa.RegisterInt64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), 0, location.Register)
	case isa.RegisterFloat64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveFloat64), 0, location.Register)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveString), 0, location.Register)
	case isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpMoveGeneral, 0, location.Register, generalMoveModeFor(nil))
	case isa.RegisterBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveBool), 0, location.Register)
	case isa.RegisterUint64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveUint64), 0, location.Register)
	case isa.RegisterComplex128:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveComplex128), 0, location.Register)
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

// emitMoveTyped emits a move using the source's static type, preserving value-copy
// semantics when a general-bank value is spilled through a scratch register.
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
		if source.Kind == dest.Kind && dest.Kind != isa.RegisterGeneral {
			c.emitSpillStore(ctx, source.Register, dest.Kind, dest.SpillSlot)
		} else if source.Kind == dest.Kind {
			scratch := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
			c.emitSameKindMoveTyped(ctx, program.VarLocation{Register: scratch, Kind: isa.RegisterGeneral}, source, sourceType)
			c.emitSpillStore(ctx, scratch, isa.RegisterGeneral, dest.SpillSlot)
			c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, scratch)
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

// emitSetSharedCellIfCaptured emits isa.SubOpSetSharedCell if the destination variable
// has been captured by a closure, keeping the upvalue cell in sync with the register.
// Suppressed inside for-loop post statements where per-iteration scoping causes the post
// to mutate a fresh per-iteration copy rather than the captured cell.
//
// Takes dest (VarLocation) which is the destination variable location to synchronise.
func (c *Compiler) emitSetSharedCellIfCaptured(_ context.Context, dest program.VarLocation) {
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
	program.EmitTier1(c.Function, isa.SubOpSetSharedCell, dest.Register, uint8(dest.Kind))
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
	case isa.RegisterInt64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), dest.Register, source.Register)
	case isa.RegisterFloat64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveFloat64), dest.Register, source.Register)
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveString), dest.Register, source.Register)
	case isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpMoveGeneral, dest.Register, source.Register, generalMoveModeFor(sourceType))
	case isa.RegisterBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveBool), dest.Register, source.Register)
	case isa.RegisterUint64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveUint64), dest.Register, source.Register)
	case isa.RegisterComplex128:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveComplex128), dest.Register, source.Register)
	case isa.RegisterSliceInt64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceInt64), dest.Register, source.Register)
	case isa.RegisterSliceFloat64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceFloat64), dest.Register, source.Register)
	case isa.RegisterSliceString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceString), dest.Register, source.Register)
	case isa.RegisterSliceBool:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceBool), dest.Register, source.Register)
	case isa.RegisterSliceUint64:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveSliceUint64), dest.Register, source.Register)
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
			program.Emit(c.Function, isa.OpBoxAsInterface, dest.Register, source.Register, uint8(source.Kind))
		}
	case source.Kind == isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpUnboxFromInterface, dest.Register, source.Register, uint8(dest.Kind))
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
		program.Emit(c.Function, isa.OpExtension, 0, location.Register, uint8(location.Kind))
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
		sc.okRegister = c.Scopes.Alloc.Alloc(isa.RegisterInt64)
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
	chosenRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
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

// emitSelectExtWords emits isa.OpExtension extension words for each select case, encoding
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
			program.Emit(c.Function, isa.OpExtension, isa.SelectDirectionReceive, sc.channelRegister, okFlag)
			program.Emit(c.Function, isa.OpExtension, sc.destinationRegister, uint8(sc.destinationKind), sc.okRegister)
		case isa.SelectDirectionSend:
			program.Emit(c.Function, isa.OpExtension, isa.SelectDirectionSend, sc.channelRegister, 0)
			program.Emit(c.Function, isa.OpExtension, sc.valueRegister, uint8(sc.valueKind), 0)
		case isa.SelectDirectionDefault:
			program.Emit(c.Function, isa.OpExtension, isa.SelectDirectionDefault, 0, 0)
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
	temporaryRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	comparisonRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)

	for i := range numCases {
		index, err := program.AddIntConstant(c.Function, int64(i))
		if err != nil {
			c.recordStickyError(err)
			return caseJumps
		}
		program.EmitWide(c.Function, isa.OpLoadInt64Constant, temporaryRegister, index)
		program.Emit(c.Function, isa.OpEqualInt64, comparisonRegister, chosenRegister, temporaryRegister)
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
		c.Scopes.BindVar(selected.assignmentName, location)
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
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToBool), okLocation.Register, selected.okRegister)
	if selected.okName != "" {
		c.Scopes.BindVar(selected.okName, okLocation)
	}
	if selected.okTarget != nil {
		if _, err := c.emitAssignTarget(ctx, selected.okTarget, okLocation); err != nil {
			return err
		}
	}
	return nil
}

// compileSelectCaseBody compiles a select case's statement list, reclaiming temporaries
// and dead locals after each statement.
//
// Takes body ([]ast.Stmt) which are the statements forming the select case body.
//
// Returns the first compilation error encountered, or nil on success.
func (c *Compiler) compileSelectCaseBody(ctx context.Context, body []ast.Stmt) error {
	_, err := c.compileStmtList(ctx, body)
	return err
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

	program.Emit(c.Function, isa.OpSendToChannel, channelLocation.Register, valueLocation.Register, uint8(valueLocation.Kind))

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
	return nodeContainsFunctionLit(body)
}

// expressionContainsFunctionLit reports whether an expression holds a function literal.
//
// Takes expression (ast.Expr) which is the expression to scan.
//
// Returns true when a function literal is found.
func expressionContainsFunctionLit(expression ast.Expr) bool {
	return expression != nil && nodeContainsFunctionLit(expression)
}

// nodeContainsFunctionLit reports whether a syntax tree holds a function literal.
//
// Takes root (ast.Node) which is the tree to scan.
//
// Returns true when a function literal is found.
func nodeContainsFunctionLit(root ast.Node) bool {
	for node := range ast.Preorder(root) {
		if isFunctionLiteral(node) {
			return true
		}
	}
	return false
}
