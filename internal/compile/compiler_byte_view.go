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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// byteViewUse is one use of a candidate byte-view variable.
type byteViewUse struct {
	// identifier is the use.
	identifier *ast.Ident

	// parent is the use's parent node, and grandparent the parent's.
	parent, grandparent ast.Node

	// owner is the innermost function literal or range-over-func body holding the use.
	owner ast.Node
}

// byteViewPosition is where a node sits in the byte view walk.
type byteViewPosition struct {
	// parent is the node's parent, and grandparent the parent's, or nil.
	parent, grandparent ast.Node

	// owner is the innermost function literal or range-over-func body holding the node.
	owner ast.Node
}

// byteViewCandidates is what the byte view walk gathers about a body's variables.
type byteViewCandidates struct {
	// conversions maps each variable declared from a []byte(s) conversion to it.
	conversions map[types.Object]*ast.CallExpr

	// owners maps each such variable to the body that declared it.
	owners map[types.Object]ast.Node

	// uses maps each variable to its uses.
	uses map[types.Object][]byteViewUse

	// definitions counts each object's definitions.
	definitions map[types.Object]int
}

// readOnlyByteViews holds the read-only byte view decisions for one function body.
type readOnlyByteViews struct {
	// conversions are the []byte(s) conversions compiled as views of s.
	conversions map[*ast.CallExpr]bool

	// variables are the locals declared from such a conversion and held as strings.
	variables map[types.Object]bool
}

// collectReadOnlyByteViews decides which conversions and locals of body are read-only
// byte views.
//
// Takes body (*ast.BlockStmt) which is the function body about to be compiled.
//
// Returns readOnlyByteViews which is empty in a debug build or when nothing qualifies.
func (c *Compiler) collectReadOnlyByteViews(body *ast.BlockStmt) readOnlyByteViews {
	views := readOnlyByteViews{}
	if body == nil || c.Info == nil || c.debugEnabled {
		return views
	}
	found := byteViewCandidates{
		conversions: map[types.Object]*ast.CallExpr{},
		owners:      map[types.Object]ast.Node{},
		uses:        map[types.Object][]byteViewUse{},
		definitions: map[types.Object]int{},
	}
	var stack []ast.Node
	var owners []ast.Node
	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			stack, owners = popByteViewNode(stack, owners)
			return true
		}
		at := byteViewPositionAt(stack, owners)
		c.noteByteViewNode(&views, found, node, at)
		stack = append(stack, node)
		if c.startsByteViewOwner(at.parent, node) {
			owners = append(owners, node)
		}
		return true
	})
	for object, conversion := range found.conversions {
		if found.definitions[object] != 1 || !c.byteViewUsesAreReads(found.uses[object], found.owners[object]) {
			continue
		}
		c.markByteView(&views, conversion, object)
	}
	return views
}

// noteByteViewNode records what one node contributes to the byte view decision: a
// conversion that declares a candidate variable or is consumed in place, a use of a
// variable, or a definition.
//
// Takes views (*readOnlyByteViews) which receives conversions consumed in place.
// Takes found (byteViewCandidates) which receives candidates, uses and definitions.
// Takes node (ast.Node) which is the node being entered.
// Takes at (byteViewPosition) which is where the node sits.
func (c *Compiler) noteByteViewNode(views *readOnlyByteViews, found byteViewCandidates, node ast.Node, at byteViewPosition) {
	switch typed := node.(type) {
	case *ast.CallExpr:
		if !c.isStringToBytesConversion(typed) {
			return
		}
		if object, ok := byteViewDeclaration(c.Info, at.parent, typed); ok {
			found.conversions[object] = typed
			found.owners[object] = at.owner
		} else if c.byteViewConsumedInPlace(at.parent, at.grandparent, typed) {
			c.markByteView(views, typed, nil)
		}
	case *ast.Ident:
		if object, ok := c.Info.Uses[typed].(*types.Var); ok {
			found.uses[object] = append(found.uses[object], byteViewUse{identifier: typed, parent: at.parent, grandparent: at.grandparent, owner: at.owner})
		}
		if object := c.Info.Defs[typed]; object != nil {
			found.definitions[object]++
		}
	default:
	}
}

// byteViewPositionAt reads a node's parent, grandparent and owner from the walk's stacks.
//
// Takes stack ([]ast.Node) which holds the enclosing nodes, innermost last.
// Takes owners ([]ast.Node) which holds the enclosing owners, innermost last.
//
// Returns byteViewPosition which holds nil for each one absent.
func byteViewPositionAt(stack, owners []ast.Node) byteViewPosition {
	var at byteViewPosition
	if len(stack) > 0 {
		at.parent = stack[len(stack)-1]
	}
	if len(stack) > 1 {
		at.grandparent = stack[len(stack)-2]
	}
	if len(owners) > 0 {
		at.owner = owners[len(owners)-1]
	}
	return at
}

// popByteViewNode leaves the innermost node, and its owner entry when it is one.
//
// Takes stack ([]ast.Node) which holds the enclosing nodes, innermost last.
// Takes owners ([]ast.Node) which holds the enclosing owners, innermost last.
//
// Returns enclosing ([]ast.Node) which is the shortened stack.
// Returns enclosingOwners ([]ast.Node) which is the owners, shortened when the node was
// the innermost one.
func popByteViewNode(stack, owners []ast.Node) (enclosing, enclosingOwners []ast.Node) {
	top := stack[len(stack)-1]
	enclosing, enclosingOwners = stack[:len(stack)-1], owners
	if len(owners) > 0 && owners[len(owners)-1] == top {
		enclosingOwners = owners[:len(owners)-1]
	}
	return enclosing, enclosingOwners
}

// markByteView records a lowered conversion and, when not nil, its variable.
//
// Takes views (*readOnlyByteViews) which receives the decision.
// Takes conversion (*ast.CallExpr) which is the conversion.
// Takes object (types.Object) which is the variable declared from it, or nil.
func (*Compiler) markByteView(views *readOnlyByteViews, conversion *ast.CallExpr, object types.Object) {
	if views.conversions == nil {
		views.conversions = map[*ast.CallExpr]bool{}
	}
	views.conversions[conversion] = true
	if object == nil {
		return
	}
	if views.variables == nil {
		views.variables = map[types.Object]bool{}
	}
	views.variables[object] = true
}

// startsByteViewOwner reports whether node opens a body compiled by another compiler.
//
// Takes parent (ast.Node) which is node's parent, or nil.
// Takes node (ast.Node) which is the node entered.
//
// Returns bool which is true for such a body.
func (c *Compiler) startsByteViewOwner(parent, node ast.Node) bool {
	if _, ok := node.(*ast.FuncLit); ok {
		return true
	}
	rangeStatement, ok := parent.(*ast.RangeStmt)
	if !ok || rangeStatement.Body != node {
		return false
	}
	rangeType, ok := c.underlyingTypeOf(rangeStatement.X)
	if !ok {
		return true
	}
	_, isFunction := rangeType.(*types.Signature)
	return isFunction
}

// isStringToBytesConversion reports whether call is the conversion []byte(s) of a string
// s.
//
// Takes call (*ast.CallExpr) which is the candidate.
//
// Returns bool which is true for such a conversion.
func (c *Compiler) isStringToBytesConversion(call *ast.CallExpr) bool {
	if len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return false
	}
	function, ok := c.Info.Types[call.Fun]
	if !ok || !function.IsType() || !isSliceOfByte(function.Type) {
		return false
	}
	argument, ok := c.Info.Types[call.Args[0]]
	if !ok || argument.Type == nil {
		return false
	}
	basic, ok := argument.Type.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsString != 0
}

// byteViewDeclaration reports whether conversion is the whole right-hand side of a
// one-variable short declaration that declares a new variable.
//
// Takes info (*types.Info) which resolves the declared identifier.
// Takes parent (ast.Node) which is the conversion's parent.
// Takes conversion (*ast.CallExpr) which is the conversion.
//
// Returns types.Object which is the declared variable.
// Returns bool which is true then.
func byteViewDeclaration(info *types.Info, parent ast.Node, conversion *ast.CallExpr) (types.Object, bool) {
	assign, ok := parent.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || assign.Rhs[0] != conversion {
		return nil, false
	}
	identifier, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || identifier.Name == "_" {
		return nil, false
	}
	object, ok := info.Defs[identifier].(*types.Var)
	if !ok || object == nil {
		return nil, false
	}
	return object, true
}

// byteViewConsumedInPlace reports whether a conversion is read where it appears.
//
// Takes parent (ast.Node) which is the conversion's parent node.
// Takes grandparent (ast.Node) which is the parent node's enclosing syntax node.
// Takes conversion (*ast.CallExpr) which is the conversion.
//
// Returns bool which is true for those three forms.
func (c *Compiler) byteViewConsumedInPlace(parent, grandparent ast.Node, conversion *ast.CallExpr) bool {
	switch typedParent := parent.(type) {
	case *ast.RangeStmt:
		return typedParent.X == conversion && byteViewRangeDeclares(typedParent)
	case *ast.CallExpr:
		return c.isBuiltinLen(typedParent) && typedParent.Args[0] == conversion
	case *ast.IndexExpr:
		return typedParent.X == conversion && byteViewIndexIsRead(typedParent, grandparent)
	default:
		return false
	}
}

// byteViewUsesAreReads reports whether every use of a candidate variable is a read the
// byte view lowering handles, in the body that declared it.
//
// Takes uses ([]byteViewUse) which are the variable's uses.
// Takes owner (ast.Node) which is the body that declared it.
//
// Returns bool which is true when the variable may be held as a string.
func (c *Compiler) byteViewUsesAreReads(uses []byteViewUse, owner ast.Node) bool {
	for _, use := range uses {
		if use.owner != owner || !c.byteViewUseIsRead(use) {
			return false
		}
	}
	return true
}

// byteViewUseIsRead reports whether one use of a candidate variable is a read the byte
// view lowering handles: a declaring range over it, len of it, or a read of an element.
//
// Takes use (byteViewUse) which is the use.
//
// Returns bool which is true for such a read.
func (c *Compiler) byteViewUseIsRead(use byteViewUse) bool {
	switch typedParent := use.parent.(type) {
	case *ast.RangeStmt:
		return typedParent.X == use.identifier && byteViewRangeDeclares(typedParent)
	case *ast.CallExpr:
		return c.isBuiltinLen(typedParent) && typedParent.Args[0] == use.identifier
	case *ast.IndexExpr:
		return typedParent.X == use.identifier && byteViewIndexIsRead(typedParent, use.grandparent)
	default:
		return false
	}
}

// byteViewRangeDeclares reports whether a range declares its variables or has none, so
// its targets are fresh locals rather than arbitrary assignment targets.
//
// Takes statement (*ast.RangeStmt) which is the range.
//
// Returns bool which is true then.
func byteViewRangeDeclares(statement *ast.RangeStmt) bool {
	if statement.Key == nil && statement.Value == nil {
		return true
	}
	return statement.Tok == token.DEFINE
}

// byteViewIndexIsRead reports whether an index expression is read, not written.
//
// Takes index (*ast.IndexExpr) which is the index expression.
// Takes parent (ast.Node) which is its parent.
//
// Returns bool which is true for a read.
func byteViewIndexIsRead(index *ast.IndexExpr, parent ast.Node) bool {
	switch typedParent := parent.(type) {
	case *ast.AssignStmt:
		for _, target := range typedParent.Lhs {
			if target == index {
				return false
			}
		}
	case *ast.IncDecStmt:
		return false
	case *ast.UnaryExpr:
		return typedParent.Op != token.AND
	case *ast.RangeStmt:
		return typedParent.Key != index && typedParent.Value != index
	default:
	}
	return true
}

// isBuiltinLen reports whether call is a call of the builtin len with one argument.
//
// Takes call (*ast.CallExpr) which is the call.
//
// Returns bool which is true then.
func (c *Compiler) isBuiltinLen(call *ast.CallExpr) bool {
	identifier, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || len(call.Args) != 1 {
		return false
	}
	builtin, ok := c.Info.Uses[identifier].(*types.Builtin)
	return ok && builtin.Name() == "len"
}

// compileByteViewConversion compiles a conversion lowered as a read-only byte view.
//
// Takes call (*ast.CallExpr) which is the conversion.
//
// Returns program.VarLocation which identifies the string register holding the byte view.
// Returns error when compiling the conversion fails; nil on success.
func (c *Compiler) compileByteViewConversion(ctx context.Context, call *ast.CallExpr) (program.VarLocation, error) {
	source, err := c.compileExpression(ctx, call.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	if source.Kind != isa.RegisterString {
		return program.VarLocation{}, fmt.Errorf("%w: []byte view of a %s register at %s", fault.ErrCompilation, source.Kind, c.positionString(call.Pos()))
	}
	view := program.VarLocation{Register: c.Scopes.Alloc.Alloc(isa.RegisterString), Kind: isa.RegisterString}
	c.emitMove(ctx, view, source)
	return view, nil
}

// compileByteViewRange compiles `range b` over a []byte held as a read-only byte view in
// the string register collection.
//
// Takes statement (*ast.RangeStmt) which is the range, with declared or absent variables.
// Takes collection (program.VarLocation) which is the string register.
//
// Returns program.VarLocation which is zero.
// Returns error which reports a compilation failure.
func (c *Compiler) compileByteViewRange(ctx context.Context, statement *ast.RangeStmt, collection program.VarLocation) (program.VarLocation, error) {
	lengthRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLengthString), lengthRegister, collection.Register)
	indexRegister, err := c.emitSliceRangeZeroIndex()
	if err != nil {
		return program.VarLocation{}, err
	}
	c.breakables = append(c.breakables, breakableContext{isLoop: true, label: c.consumePendingLabel(ctx)})
	loopStart := program.CurrentPC(c.Function)
	comparisonRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpLessThanInt64, comparisonRegister, indexRegister, lengthRegister)
	jumpToEnd := program.EmitJump(c.Function, isa.OpJumpIfFalse, comparisonRegister)
	bodyStart := program.CurrentPC(c.Function)

	needsReset := bodyContainsFunctionLit(statement.Body)
	if err := c.emitSliceRangeKey(ctx, statement, indexRegister, needsReset); err != nil {
		return program.VarLocation{}, err
	}
	if err := c.emitByteViewRangeValue(ctx, statement, collection, indexRegister, needsReset); err != nil {
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

// emitByteViewRangeValue declares the range's value variable, when it has one, and loads
// the byte at the index into it.
//
// Takes statement (*ast.RangeStmt) which is the range.
// Takes collection (program.VarLocation) which is the string register.
// Takes indexRegister (uint8) which holds the index.
// Takes needsResetSharedCell (bool) which is true when the body holds a closure.
//
// Returns any compilation error.
func (c *Compiler) emitByteViewRangeValue(ctx context.Context, statement *ast.RangeStmt, collection program.VarLocation, indexRegister uint8, needsResetSharedCell bool) error {
	if statement.Value == nil || isBlankIdent(statement.Value) {
		return nil
	}
	valueIdentifier, ok := statement.Value.(*ast.Ident)
	if !ok || statement.Tok != token.DEFINE {
		return fmt.Errorf("%w: []byte view range value is not a declared identifier at %s", fault.ErrCompilation, c.positionString(statement.Value.Pos()))
	}
	typeObject := c.Info.Defs[valueIdentifier]
	if typeObject == nil || c.kindFor(typeObject.Type()) != isa.RegisterUint64 {
		return fmt.Errorf("%w: []byte view range value %q is not a byte at %s", fault.ErrCompilation, valueIdentifier.Name, c.positionString(valueIdentifier.Pos()))
	}
	valueLocation := c.Scopes.DeclareVar(valueIdentifier.Name, isa.RegisterUint64)
	if needsResetSharedCell && !valueLocation.IsSpilled && c.closureCapturedNames[valueIdentifier.Name] {
		program.EmitTier1(c.Function, isa.SubOpResetSharedCell, valueLocation.Register, uint8(valueLocation.Kind))
	}
	if !valueLocation.IsSpilled {
		program.Emit(c.Function, isa.OpGetStringByte, valueLocation.Register, collection.Register, indexRegister)
		return nil
	}
	scratch := c.Scopes.Alloc.AllocTemp(isa.RegisterUint64)
	program.Emit(c.Function, isa.OpGetStringByte, scratch, collection.Register, indexRegister)
	c.emitSpillStore(ctx, scratch, isa.RegisterUint64, valueLocation.SpillSlot)
	c.Scopes.Alloc.FreeTemp(isa.RegisterUint64, scratch)
	return nil
}
