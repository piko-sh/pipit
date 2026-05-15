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

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

// compileExpressionInto compiles expression into dest.
//
// Emits the final arithmetic instruction straight into the destination register when the
// expression shape allows it, and otherwise compiles into a temporary followed by a typed
// move. Callers pass a plain scalar register they own (see directAssignDestination() and
// directReturnDestination()), so the only instruction that writes dest is the last one
// and every operand read precedes it.
//
// Takes expression (ast.Expr) which is the expression to compile.
// Takes dest (VarLocation) which is the register the value must end up in.
//
// Returns dest and any compilation error.
func (c *Compiler) compileExpressionInto(ctx context.Context, expression ast.Expr, dest program.VarLocation) (program.VarLocation, error) {
	handled, err := c.tryCompileArithmeticInto(ctx, expression, dest)
	if err != nil {
		return program.VarLocation{}, err
	}
	if handled {
		return dest, nil
	}
	location, err := c.compileExpression(ctx, expression)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.emitMoveTyped(ctx, dest, location, c.staticTypeOf(expression))
	return dest, nil
}

// tryCompileArithmeticInto emits a non-constant arithmetic binary expression with dest as
// the destination register.
//
// Handles + - * / % on the scalar banks. Parentheses are transparent. Comparisons,
// short-circuit operators, constant-folded expressions and every other shape are left to
// the caller's fallback.
//
// Takes expression (ast.Expr) which is the candidate expression.
// Takes dest (VarLocation) which is the destination register.
//
// Returns true when the expression was emitted into dest, and any compilation error.
func (c *Compiler) tryCompileArithmeticInto(ctx context.Context, expression ast.Expr, dest program.VarLocation) (bool, error) {
	if paren, ok := expression.(*ast.ParenExpr); ok {
		return c.tryCompileArithmeticInto(ctx, paren.X, dest)
	}
	binary, ok := expression.(*ast.BinaryExpr)
	if !ok || !isDestinationDrivenArithmetic(binary.Op) {
		return false, nil
	}
	if tv, found := c.Info.Types[expression]; found && tv.Value != nil {
		return false, nil
	}
	c.setDebugPosition(ctx, expression.Pos())
	left, err := c.compileExpression(ctx, binary.X)
	if err != nil {
		return false, err
	}
	right, err := c.compileExpression(ctx, binary.Y)
	if err != nil {
		return false, err
	}
	result, err := c.emitArithmetic(ctx, binary.Op, left, right, &dest)
	if err != nil {
		return false, err
	}
	if result.Kind != dest.Kind {
		c.finishArithmeticResult(ctx, result, dest, expression)
		return true, nil
	}
	if t := c.staticTypeOf(expression); t != nil {
		c.EmitNarrowIntegerTruncation(dest, t)
	}
	return true, nil
}

// finishArithmeticResult applies the narrowing truncation the plain path applies to an
// arithmetic result and moves it into dest when the result bank differs from the
// destination bank.
//
// Takes result (VarLocation) which holds the freshly computed value.
// Takes dest (VarLocation) which is the destination register.
// Takes expression (ast.Expr) which supplies the static type for narrowing.
func (c *Compiler) finishArithmeticResult(ctx context.Context, result, dest program.VarLocation, expression ast.Expr) {
	if t := c.staticTypeOf(expression); t != nil {
		c.EmitNarrowIntegerTruncation(result, t)
	}
	c.emitMoveTyped(ctx, dest, result, c.staticTypeOf(expression))
}

// directAssignDestination resolves an assignment target to a register the right-hand side
// may be emitted into directly: a non-blank local identifier held in a plain scalar
// register, so no upvalue, global, heap-promoted or spilled variable is written through
// this path. emitIdentAssign still runs afterwards to keep captured cells in sync.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
//
// Returns the destination register and true when the target qualifies.
func (c *Compiler) directAssignDestination(leftHandSide ast.Expr) (program.VarLocation, bool) {
	target, ok := leftHandSide.(*ast.Ident)
	if !ok || target.Name == typemap.BlankIdentName {
		return program.VarLocation{}, false
	}
	if c.resolveIdentTarget(target) != identTargetLocal {
		return program.VarLocation{}, false
	}
	destination, found := c.Scopes.LookupVar(target.Name)
	if !found || !isDirectScalarRegister(destination) {
		return program.VarLocation{}, false
	}
	return program.VarLocation{Register: destination.Register, Kind: destination.Kind}, true
}

// directReturnDestination resolves the canonical return slot a single-value return
// expression may be emitted into directly: bank slot zero of the function's first result
// kind, when the statement returns exactly one value whose static type lives in that
// scalar bank.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
// Takes result (ast.Expr) which is the single result expression.
//
// Returns the return-slot register and true when the expression qualifies.
func (c *Compiler) directReturnDestination(statement *ast.ReturnStmt, result ast.Expr) (program.VarLocation, bool) {
	if len(statement.Results) != 1 || len(c.Function.ResultKinds) != 1 {
		return program.VarLocation{}, false
	}
	kind := c.Function.ResultKinds[0]
	staticType := c.staticTypeOf(result)
	if staticType == nil || typemap.KindForType(staticType) != kind {
		return program.VarLocation{}, false
	}
	slot := program.VarLocation{Register: 0, Kind: kind}
	if !isDirectScalarRegister(slot) {
		return program.VarLocation{}, false
	}
	return slot, true
}

// isDestinationDrivenArithmetic reports whether op is one of the arithmetic operators
// emitArithmetic lowers to a single three-operand instruction.
//
// Takes op (token.Token) which is the binary operator.
//
// Returns true for + - * / and %.
func isDestinationDrivenArithmetic(op token.Token) bool {
	switch op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
		return true
	default:
		return false
	}
}

// isDirectScalarRegister reports whether location is a plain register in one of the
// scalar banks that a three-operand instruction may write directly.
//
// Takes location (VarLocation) which is the candidate destination.
//
// Returns true for non-indirect, non-spilled, non-upvalue int, float, string, uint and
// complex registers. General and typed-slice banks return false because neither has a
// three-operand encoding.
func isDirectScalarRegister(location program.VarLocation) bool {
	if location.IsIndirect || location.IsSpilled || location.IsUpvalue {
		return false
	}
	switch location.Kind {
	case isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterUint, isa.RegisterComplex:
		return true
	default:
		return false
	}
}
