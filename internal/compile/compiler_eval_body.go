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
	"reflect"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/compile/passes"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// CompileNonEvalFuncDecls compiles all function declarations in the file except the
// synthetic _eval_ function.
//
// Takes declarations ([]ast.Decl) which are the AST declarations to compile.
//
// Returns error when compilation of any function declaration fails.
func (c *Compiler) CompileNonEvalFuncDecls(ctx context.Context, declarations []ast.Decl) error {
	for _, declaration := range declarations {
		functionDeclaration, ok := declaration.(*ast.FuncDecl)
		if !ok || functionDeclaration.Name.Name == EvalFunctionName {
			continue
		}
		if err := c.compileFunctionDecl(ctx, functionDeclaration); err != nil {
			return fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
		}
	}
	return nil
}

// CompileEvalBody finds the _eval_ function and compiles its body.
//
// Execution is deliberately not part of this: running the result needs a Service and a
// VM, which belong to the orchestration layer, and keeping them out of a Compiler method
// is what lets the Compiler stand alone as a package.
//
// Takes file (*ast.File) which contains the _eval_ function.
// Takes info (*types.Info) which holds type-checking information.
// Takes lastExpr (ast.Expr) which is the last expression for result extraction.
// Takes hasResult (bool) which is true when the last statement was an expression.
//
// Returns *CompiledFunction which is the compiled body, or nil when no _eval_ exists.
// Returns error when compilation fails.
func (c *Compiler) CompileEvalBody(
	ctx context.Context,
	file *ast.File,
	info *types.Info,
	lastExpr ast.Expr,
	hasResult bool,
) (*program.CompiledFunction, error) {
	functionDeclaration := findEvalFunctionDeclaration(file)
	if functionDeclaration == nil {
		return nil, nil
	}

	c.Scopes.PushScope()
	c.closureCapturedNames = escape.CollectClosureCapturedNamesAll(functionDeclaration.Body)
	c.classifyTypedSliceLocals(functionDeclaration.Body)

	lastLocation, err := c.compileStmtList(ctx, functionDeclaration.Body.List)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}

	if hasResult {
		lastLocation = c.coerceEvalBoolResult(ctx, info, lastExpr, lastLocation)
		c.Function.ResultKinds = []isa.RegisterKind{lastLocation.Kind}
		c.Function.ResultReflectTypes = []reflect.Type{lastLocation.SourceType}
		c.emitMoveToRegisterZero(ctx, lastLocation)
	}

	if err := c.resourceError(); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}
	c.Function.NumRegisters = c.Scopes.PeakRegisters()
	if err := passes.OptimiseFunction(ctx, c.passOptions, c.Function); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}
	c.Scopes.PopScope()

	if err := inline.RunBytecodeInliner(ctx, c.Function, c.passOptions); err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}

	return c.Function, nil
}

// CompileEvalFunction finds and compiles the _eval_ function body.
//
// Takes file (*ast.File) which contains the _eval_ function.
// Takes info (*types.Info) which holds type-checking information.
// Takes lastExpr (ast.Expr) which is the last expression for result extraction.
// Takes hasResult (bool) which is true when the last statement was an expression.
// Takes evalFunction (*CompiledFunction) which is the compiled function shell to
// populate.
//
// Returns *CompiledFunction which is the compiled eval function, or evalFunction when no
// _eval_ function exists.
// Returns error when compilation fails.
func (c *Compiler) CompileEvalFunction(ctx context.Context,
	file *ast.File,
	info *types.Info,
	lastExpr ast.Expr,
	hasResult bool,
	evalFunction *program.CompiledFunction,
) (*program.CompiledFunction, error) {
	functionDeclaration := findEvalFunctionDeclaration(file)
	if functionDeclaration == nil {
		return evalFunction, nil
	}

	c.Scopes.PushScope()
	c.classifyTypedSliceLocals(functionDeclaration.Body)

	lastLocation, err := c.compileStmtList(ctx, functionDeclaration.Body.List)
	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}

	if hasResult {
		lastLocation = c.coerceEvalBoolResult(ctx, info, lastExpr, lastLocation)
		c.Function.ResultKinds = []isa.RegisterKind{lastLocation.Kind}
		c.Function.ResultReflectTypes = []reflect.Type{lastLocation.SourceType}
		c.emitMoveToRegisterZero(ctx, lastLocation)
	}

	if err := c.resourceError(); err != nil {
		return nil, fmt.Errorf("compiling eval function: %w", err)
	}
	c.Function.NumRegisters = c.Scopes.PeakRegisters()
	if err := passes.OptimiseFunction(ctx, c.passOptions, c.Function); err != nil {
		return nil, fmt.Errorf("compiling eval function: %w", err)
	}
	c.Scopes.PopScope()
	return c.Function, nil
}
