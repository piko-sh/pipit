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

	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine/program"
)

var _ patterns.Emitter = (*Compiler)(nil)

// CurrentFunction returns the function whose body is being emitted.
//
// Returns *program.CompiledFunction which is the function being emitted.
func (c *Compiler) CurrentFunction() *program.CompiledFunction { return c.Function }

// ScopeStack returns the scope stack and register allocator of the current function.
//
// Returns *scope.ScopeStack which allocates and resolves registers.
func (c *Compiler) ScopeStack() *scope.ScopeStack { return c.Scopes }

// SetDebugPosition records pos as the source position of the instructions emitted next.
//
// Takes pos (token.Pos) which is the source position.
func (c *Compiler) SetDebugPosition(ctx context.Context, pos token.Pos) { c.setDebugPosition(ctx, pos) }

// CompileExpression compiles expression through the standard walker.
//
// Takes expression (ast.Expr) which is the expression to compile.
//
// Returns program.VarLocation which holds the result.
// Returns error when compilation fails.
func (c *Compiler) CompileExpression(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	return c.compileExpression(ctx, expression)
}

// CompileStmt compiles statement through the standard walker.
//
// Takes statement (ast.Stmt) which is the statement to compile.
//
// Returns program.VarLocation which holds the result.
// Returns error when compilation fails.
func (c *Compiler) CompileStmt(ctx context.Context, statement ast.Stmt) (program.VarLocation, error) {
	return c.compileStmt(ctx, statement)
}

// CompileForFallback compiles a for statement through the scalar path, bypassing
// recognition.
//
// Takes statement (*ast.ForStmt) which is the loop to compile.
//
// Returns program.VarLocation which holds the result.
// Returns error when compilation fails.
func (c *Compiler) CompileForFallback(ctx context.Context, statement *ast.ForStmt) (program.VarLocation, error) {
	return c.compileForFallback(ctx, statement)
}

// astPatternRecogniseContext builds the read-only context the recogniser registry passes
// to Match. Called once per for-statement compile from compileFor; cheap struct
// construction with no allocation.
//
// Returns the patterns.RecogniseContext snapshot.
func (c *Compiler) astPatternRecogniseContext() patterns.RecogniseContext {
	return patterns.RecogniseContext{
		Info:     c.Info,
		Function: c.Function,
		Options:  c.passOptions,
	}
}
