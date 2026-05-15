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
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
)

// newSyntheticIdent declares a compiler-synthesised local of type t.
//
// Takes pos (token.Pos) which positions the identifier for diagnostics.
// Takes t (types.Type) which is the local's type.
//
// Returns the defining identifier and its object.
func (c *Compiler) newSyntheticIdent(pos token.Pos, t types.Type) (*ast.Ident, *types.Var) {
	c.syntheticCount++
	name := scope.SyntheticNamePrefix + "t" + strconv.Itoa(c.syntheticCount)
	object := types.NewVar(pos, nil, name, t)
	definition := &ast.Ident{NamePos: pos, Name: name, Obj: nil}
	c.Info.Defs[definition] = object
	return definition, object
}

// syntheticUse creates a use of a synthesised local.
//
// Takes pos (token.Pos) which positions the identifier.
// Takes object (*types.Var) which is the local's object.
//
// Returns the identifier.
func (c *Compiler) syntheticUse(pos token.Pos, object *types.Var) *ast.Ident {
	use := &ast.Ident{NamePos: pos, Name: object.Name(), Obj: nil}
	c.Info.Uses[use] = object
	return use
}

// rangeTargetsNeedTemps reports whether a range statement assigns to targets other than
// identifiers.
//
// Takes statement (*ast.RangeStmt) which is the loop.
//
// Returns true when the rewrite applies.
func (c *Compiler) rangeTargetsNeedTemps(statement *ast.RangeStmt) bool {
	if statement.Tok != token.ASSIGN || c.Info == nil {
		return false
	}
	return (statement.Key != nil && !isPlainTarget(statement.Key)) ||
		(statement.Value != nil && !isPlainTarget(statement.Value))
}

// compileRangeAssignTargets rewrites `for k, v = range x { body }` with non-identifier
// targets into `for ~k, ~v := range x { k = ~k; v = ~v; body }` and compiles that.
//
// Takes statement (*ast.RangeStmt) which is the loop.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileRangeAssignTargets(ctx context.Context, statement *ast.RangeStmt) (program.VarLocation, error) {
	body := make([]ast.Stmt, 0, len(statement.Body.List)+2)
	key, keyAssign := c.rangeTargetTemp(statement, statement.Key)
	value, valueAssign := c.rangeTargetTemp(statement, statement.Value)
	if keyAssign != nil {
		body = append(body, keyAssign)
	}
	if valueAssign != nil {
		body = append(body, valueAssign)
	}
	body = append(body, statement.Body.List...)
	rewritten := &ast.RangeStmt{
		For:    statement.For,
		Key:    key,
		Value:  value,
		TokPos: statement.TokPos,
		Tok:    token.DEFINE,
		Range:  statement.Range,
		X:      statement.X,
		Body:   &ast.BlockStmt{Lbrace: statement.Body.Lbrace, List: body, Rbrace: statement.Body.Rbrace},
	}
	return c.compileForRange(ctx, rewritten)
}

// rangeTargetTemp replaces one range target with a synthesised local and builds the
// assignment that copies the local to the original target each iteration. A nil or blank
// target is returned unchanged with no assignment.
//
// Takes statement (*ast.RangeStmt) which positions the synthesised nodes.
// Takes target (ast.Expr) which is the key or value target.
//
// Returns the target to bind and the per-iteration assignment, or nil.
func (c *Compiler) rangeTargetTemp(statement *ast.RangeStmt, target ast.Expr) (ast.Expr, ast.Stmt) {
	if target == nil || isBlankIdent(target) {
		return target, nil
	}
	definition, object := c.newSyntheticIdent(target.Pos(), c.Info.TypeOf(target))
	assign := &ast.AssignStmt{
		Lhs:    []ast.Expr{target},
		TokPos: statement.TokPos,
		Tok:    token.ASSIGN,
		Rhs:    []ast.Expr{c.syntheticUse(target.Pos(), object)},
	}
	return definition, assign
}

// commaOkTargetsNeedTemps reports whether a two-target assignment from a comma-ok
// expression (map index, type assertion, channel receive) has a non-identifier target.
//
// Takes statement (*ast.AssignStmt) which is the assignment.
//
// Returns true when the rewrite applies.
func (c *Compiler) commaOkTargetsNeedTemps(statement *ast.AssignStmt) bool {
	if len(statement.Lhs) != 2 || c.Info == nil || !c.isCommaOkExpression(statement.Rhs[0]) {
		return false
	}
	return !isPlainTarget(statement.Lhs[0]) || !isPlainTarget(statement.Lhs[1])
}

// isCommaOkExpression reports whether expression can yield a (value, ok) pair: a map
// index, a type assertion or a channel receive.
//
// Takes expression (ast.Expr) which is the right-hand side.
//
// Returns true for the comma-ok shapes.
func (c *Compiler) isCommaOkExpression(expression ast.Expr) bool {
	switch e := ast.Unparen(expression).(type) {
	case *ast.IndexExpr:
		_, isMap := c.Info.TypeOf(e.X).Underlying().(*types.Map)
		return isMap
	case *ast.TypeAssertExpr:
		return e.Type != nil
	case *ast.UnaryExpr:
		return e.Op == token.ARROW
	default:
		return false
	}
}

// compileCommaOkAssignTargets rewrites `t1, t2 = <comma-ok>` with non-identifier targets
// into `~a, ~b := <comma-ok>; t1 = ~a; t2 = ~b` inside its own scope and compiles that.
//
// Takes statement (*ast.AssignStmt) which is the assignment.
//
// Returns an empty location and any compilation error.
func (c *Compiler) compileCommaOkAssignTargets(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	c.Scopes.PushScope()
	defer c.Scopes.PopScope()
	definitions := make([]ast.Expr, len(statement.Lhs))
	uses := make([]*ast.Ident, len(statement.Lhs))
	for i, target := range statement.Lhs {
		if identifier, ok := target.(*ast.Ident); ok && identifier.Name == typemap.BlankIdentName {
			definitions[i] = target
			continue
		}
		definition, object := c.newSyntheticIdent(target.Pos(), c.Info.TypeOf(target))
		definitions[i] = definition
		uses[i] = c.syntheticUse(target.Pos(), object)
	}
	declaration := &ast.AssignStmt{Lhs: definitions, TokPos: statement.TokPos, Tok: token.DEFINE, Rhs: statement.Rhs}
	if _, err := c.compileShortVarDecl(ctx, declaration); err != nil {
		return program.VarLocation{}, err
	}
	for i, target := range statement.Lhs {
		if uses[i] == nil {
			continue
		}
		if _, err := c.compileAssignPair(ctx, target, uses[i]); err != nil {
			return program.VarLocation{}, err
		}
	}
	return program.VarLocation{}, nil
}

// isPlainTarget reports whether target is an identifier, blank included.
//
// Takes target (ast.Expr) which is an assignment target.
//
// Returns true for identifiers.
func isPlainTarget(target ast.Expr) bool {
	_, ok := target.(*ast.Ident)
	return ok
}
