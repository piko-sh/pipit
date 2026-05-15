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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

// compileUnsafeBuiltinCall compiles a call to an unsafe package built-in function.
//
// Takes name (string) which is the name of the unsafe builtin.
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the unsafe operation result and any compilation error.
func (c *Compiler) compileUnsafeBuiltinCall(ctx context.Context, name string, expression *ast.CallExpr) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureUnsafeOps, expression.Lparen); err != nil {
		return program.VarLocation{}, err
	}
	switch name {
	case "Sizeof", "Alignof", "Offsetof":
		tv := c.Info.Types[expression]
		if tv.Value != nil {
			return c.compileConstant(ctx, tv)
		}
		return program.VarLocation{}, fmt.Errorf("unsafe.%s: expected compile-time constant", name)
	case "String":
		return c.compileUnsafeString(ctx, expression)
	case "StringData":
		return c.compileUnsafeStringData(ctx, expression)
	case "Slice":
		return c.compileUnsafeSlice(ctx, expression)
	case "SliceData":
		return c.compileUnsafeSliceData(ctx, expression)
	case "Add":
		return c.compileUnsafeAdd(ctx, expression)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported unsafe builtin: %s at %s", name, c.positionString(expression.Pos()))
	}
}

// compileUnsafeString compiles an unsafe.String(ptr, len) call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the resulting string and any compilation error.
func (c *Compiler) compileUnsafeString(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileUnsafeBinaryOp(ctx, expression, isa.OpUnsafeString, isa.RegisterString, "unsafe.String")
}

// compileUnsafeStringData compiles an unsafe.StringData(str) call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the underlying data pointer and any compilation error.
func (c *Compiler) compileUnsafeStringData(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileUnsafeStringDataArgCount
	}

	strLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpUnsafeStringData), dest, strLocation.Register)

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileUnsafeSlice compiles an unsafe.Slice(ptr, len) call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the resulting slice and any compilation error.
func (c *Compiler) compileUnsafeSlice(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileUnsafeBinaryOp(ctx, expression, isa.OpUnsafeSlice, isa.RegisterGeneral, "unsafe.Slice")
}

// compileUnsafeSliceData compiles an unsafe.SliceData(slice) call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the underlying data pointer and any compilation error.
func (c *Compiler) compileUnsafeSliceData(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileUnsafeSliceDataArgCount
	}

	sliceLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &sliceLocation)

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpUnsafeSliceData), dest, sliceLocation.Register)

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileUnsafeAdd compiles an unsafe.Add(ptr, len) call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the resulting pointer and any compilation error.
func (c *Compiler) compileUnsafeAdd(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileUnsafeBinaryOp(ctx, expression, isa.OpUnsafeAdd, isa.RegisterGeneral, "unsafe.Add")
}

// compileUnsafeBinaryOp is the shared implementation for unsafe binary operations such as
// unsafe.String, unsafe.Slice, and unsafe.Add.
//
// Takes expression (*ast.CallExpr) which is the AST call expression containing the two
// arguments.
// Takes op (opcode) which is the opcode to Emit.
// Takes destinationKind (isa.RegisterKind) which is the register kind for the
// destination.
// Takes name (string) which is the function name for error messages.
//
// Returns VarLocation holding the operation result and any compilation error.
func (c *Compiler) compileUnsafeBinaryOp(ctx context.Context, expression *ast.CallExpr, op isa.Opcode, destinationKind isa.RegisterKind, name string) (program.VarLocation, error) {
	if len(expression.Args) != 2 {
		return program.VarLocation{}, fmt.Errorf("%s requires 2 arguments", name)
	}

	pointerLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &pointerLocation)

	intLocation, err := c.compileExpression(ctx, expression.Args[1])
	if err != nil {
		return program.VarLocation{}, err
	}
	c.ensureIntRegister(ctx, &intLocation)

	dest := c.Scopes.Alloc.Alloc(destinationKind)
	program.Emit(c.Function, op, dest, pointerLocation.Register, intLocation.Register)

	return program.VarLocation{Register: dest, Kind: destinationKind}, nil
}
