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

package app

import (
	"context"
	"go/ast"
	"go/types"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
)

// executeCompiledEvalBody executes an already compiled submission and records its cost.
//
// Takes compiled (*CompiledFunction) which is the compiled eval body.
//
// Returns the result and any execution error, or nil values when no body exists.
func (s *Service) executeCompiledEvalBody(ctx context.Context, compiled *program.CompiledFunction) (any, error) {
	if compiled == nil {
		return nil, nil
	}
	vm, cancelExecution := s.newExecutionVM(ctx, engine.DebugRoleEval)
	defer cancelExecution(fault.ErrMainReturned)
	result, executeErr := vm.Execute(compiled)
	s.recordCost(vm)

	return result, executeErr
}

// compileAndRunEvalBody compiles the _eval_ function body and executes it.
//
// Takes c (*Compiler) which compiles the body.
// Takes s (*Service) which supplies the execution VM and records its cost.
// Takes file (*ast.File) which contains the _eval_ function.
// Takes info (*types.Info) which holds type-checking information.
// Takes lastExpr (ast.Expr) which is the last expression for result extraction.
// Takes hasResult (bool) which is true when the last statement was an expression.
//
// Returns any which is the result, or nil when no _eval_ exists.
// Returns error when compilation or execution fails.
func compileAndRunEvalBody(
	ctx context.Context,
	c *compile.Compiler,
	s *Service,
	file *ast.File,
	info *types.Info,
	lastExpr ast.Expr,
	hasResult bool,
) (any, error) {
	compiled, err := c.CompileEvalBody(ctx, file, info, lastExpr, hasResult)
	if err != nil || compiled == nil {
		return nil, err
	}
	return s.executeCompiledEvalBody(ctx, compiled)
}
