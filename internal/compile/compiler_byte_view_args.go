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
	"errors"
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// maxByteViewArguments bounds the argument index a mark can name.
const maxByteViewArguments = 64

// errByteViewConversion reports a marked conversion the conversion compiler did not take,
// which the candidate test rules out.
var errByteViewConversion = errors.New("compile: a marked []byte(s) argument did not compile as a conversion")

// errByteViewOperand reports a marked conversion's string that did not compile into a
// plain string register; compileExpression returns every string operand in one, reloading
// spilled, indirect and captured variables first.
var errByteViewOperand = errors.New("compile: a marked []byte(s) argument's string is not in a string register")

// takeByteViewCandidates returns the mask of expression's arguments marked as byte-view
// arguments, when expression is the call compileNativeCallFromLocation is compiling; it
// records the mask for that caller.
//
// Takes expression (*ast.CallExpr) which is the native call.
// Takes signature (*types.Signature) which is the callee's signature, or nil.
//
// Returns uint64 which has bit i set for each marked argument i.
func (c *Compiler) takeByteViewCandidates(expression *ast.CallExpr, signature *types.Signature) uint64 {
	if c.byteViewCall != expression {
		return 0
	}
	c.byteViewCall = nil
	if c.debugEnabled || signature == nil || expression.Ellipsis.IsValid() ||
		len(expression.Args) > maxByteViewArguments || c.Info == nil {
		return 0
	}
	parameters := signature.Params()
	fixed := parameters.Len()
	if signature.Variadic() {
		fixed--
	}
	var mask uint64
	for i, argument := range expression.Args {
		if !c.byteViewSimpleExpression(argument) {
			return 0
		}
		call, ok := argument.(*ast.CallExpr)
		if !ok || i >= fixed || !c.isStringToBytesConversion(call) || c.byteViews.conversions[call] {
			continue
		}
		if !isSliceOfByte(parameters.At(i).Type()) {
			continue
		}
		mask |= 1 << i
	}
	c.byteViewMarked = mask
	return mask
}

// byteViewSimpleExpression reports whether expression evaluates without running any call,
// receive or other code with side effects.
//
// Takes expression (ast.Expr) which is an argument or a part of one.
//
// Returns bool which is true for simple expressions.
func (c *Compiler) byteViewSimpleExpression(expression ast.Expr) bool {
	switch typed := expression.(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return c.byteViewSimpleExpression(typed.X)
	case *ast.SelectorExpr:
		if identifier, ok := typed.X.(*ast.Ident); ok {
			if _, isPackage := c.Info.Uses[identifier].(*types.PkgName); isPackage {
				return true
			}
		}
		if selection, ok := c.Info.Selections[typed]; ok && selection.Kind() != types.FieldVal {
			return false
		}
		return c.byteViewSimpleExpression(typed.X)
	case *ast.UnaryExpr:
		return typed.Op != token.ARROW && c.byteViewSimpleExpression(typed.X)
	case *ast.BinaryExpr:
		return c.byteViewSimpleExpression(typed.X) && c.byteViewSimpleExpression(typed.Y)
	case *ast.IndexExpr:
		return c.byteViewSimpleExpression(typed.X) && c.byteViewSimpleExpression(typed.Index)
	case *ast.StarExpr:
		return c.byteViewSimpleExpression(typed.X)
	case *ast.CallExpr:
		function, ok := c.Info.Types[typed.Fun]
		if !ok || !function.IsType() || len(typed.Args) != 1 {
			return false
		}
		return c.byteViewSimpleExpression(typed.Args[0])
	default:
		return false
	}
}

// compileByteViewArguments compiles marked conversions after every other argument.
//
// Takes expression (*ast.CallExpr) which is the native call.
// Takes mask (uint64) which marks the conversions.
// Takes locations ([]program.VarLocation) which receives their locations.
//
// Returns error which is any compilation error.
func (c *Compiler) compileByteViewArguments(ctx context.Context, expression *ast.CallExpr, mask uint64, locations []program.VarLocation) error {
	operands := make(map[int]program.VarLocation, 2)
	if err := c.compileByteViewOperands(ctx, expression, mask, operands); err != nil {
		return err
	}
	for i, argument := range expression.Args {
		if mask&(1<<i) == 0 {
			continue
		}
		conversion, isConversion := argument.(*ast.CallExpr)
		if !isConversion {
			return errByteViewConversion
		}
		operand := operands[i]
		srcType := c.Info.Types[conversion.Args[0]].Type
		location, ok := c.compileByteStringConversion(ctx, operand, isa.RegisterString, isa.RegisterGeneral, srcType, c.Info.Types[conversion].Type)
		if !ok {
			return errByteViewConversion
		}
		locations[i] = location
	}
	return nil
}

// compileByteViewOperands compiles the strings the marked conversions convert, each into
// a string register of its own.
//
// Takes expression (*ast.CallExpr) which is the native call.
// Takes mask (uint64) which marks the conversions.
// Takes operands (map[int]program.VarLocation) which receives each marked argument's
// string operand.
//
// Returns error which is any compilation error.
func (c *Compiler) compileByteViewOperands(ctx context.Context, expression *ast.CallExpr, mask uint64, operands map[int]program.VarLocation) error {
	for i, argument := range expression.Args {
		if mask&(1<<i) == 0 {
			continue
		}
		conversion, isConversion := argument.(*ast.CallExpr)
		if !isConversion {
			return errByteViewConversion
		}
		operand, err := c.compileExpression(ctx, conversion.Args[0])
		if err != nil {
			return err
		}
		operand = c.unpackConversionArgument(operand, c.kindFor(c.Info.Types[conversion.Args[0]].Type))
		if operand.Kind != isa.RegisterString || operand.IsSpilled || operand.IsIndirect || operand.IsUpvalue {
			return errByteViewOperand
		}
		operands[i] = operand
	}
	return nil
}
