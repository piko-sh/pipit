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
	"go/constant"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// emitShiftCountCheck checks for a negative signed shift count.
//
// Takes count (program.VarLocation) which is the shift count's register.
func (c *Compiler) emitShiftCountCheck(count program.VarLocation) {
	if count.Kind != isa.RegisterInt64 {
		return
	}
	program.EmitTier2(c.Function, isa.SubOpTier2CheckShiftCount, count.Register)
}

// compileCompoundOperand compiles the right operand of a compound assignment, loading a
// constant shift count into the uint bank as compileShiftCountOrExpression() does for a
// binary shift.
//
// Takes operation (token.Token) which is the binary operator the assignment applies.
// Takes operand (ast.Expr) which is the right operand.
//
// Returns program.VarLocation which is the operand's register location.
// Returns error when the operand fails to compile.
func (c *Compiler) compileCompoundOperand(ctx context.Context, operation token.Token, operand ast.Expr) (program.VarLocation, error) {
	if isShiftOperation(operation) {
		if typed, ok := c.Info.Types[operand]; ok && typed.Value != nil {
			return c.emitScalarConstant(ctx, constant.ToInt(typed.Value), isa.RegisterUint64)
		}
	}
	return c.compileExpression(ctx, operand)
}

// compoundValueKind returns the bank a compound assignment reads its target's current
// value into.
//
// Takes operation (token.Token) which is the binary operator the assignment applies.
// Takes target (types.Type) which is the type of the assigned target.
// Takes operand (program.VarLocation) which holds the right operand.
//
// Returns isa.RegisterKind which is the bank for the target's value.
func (c *Compiler) compoundValueKind(operation token.Token, target types.Type, operand program.VarLocation) isa.RegisterKind {
	if isShiftOperation(operation) && target != nil {
		return c.kindFor(target)
	}
	return operand.Kind
}

// isShiftOperation reports whether operation is a left or right shift.
//
// Takes operation (token.Token) which is the binary operator.
//
// Returns bool which is true for token.SHL and token.SHR.
func isShiftOperation(operation token.Token) bool {
	return operation == token.SHL || operation == token.SHR
}
