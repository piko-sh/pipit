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

package selfhost

import (
	"context"
	"go/ast"
	"go/token"
	"reflect"

	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

// emitterAdapter lets an interpreted *Compiler satisfy patterns.Emitter when the native
// pattern recognisers call back into it, so the self-hosting lane can run the compiler's
// specialised emission paths under the interpreter. It is hand-written in the shape the
// extractor's adapter generator will produce; the generated file replaces it.
type emitterAdapter struct {
	// core is the adapter bridge to the interpreted Compiler methods.
	core *engine.AdapterCore
}

var _ patterns.Emitter = (*emitterAdapter)(nil)

func init() {
	engine.RegisterInterfaceAdapter(reflect.TypeFor[patterns.Emitter](), func(core *engine.AdapterCore) reflect.Value {
		return reflect.ValueOf(&emitterAdapter{core: core})
	})
}

// CurrentFunction returns the function whose body is being emitted.
//
// Returns *program.CompiledFunction which is the function being built, or nil when the
// interpreted body failed.
func (a *emitterAdapter) CurrentFunction() *program.CompiledFunction {
	results := a.core.InvokeRecordingError("CurrentFunction")
	function, _ := resultAs[*program.CompiledFunction](results, 0)
	return function
}

// ScopeStack returns the scope stack and register allocator of the current function.
//
// Returns *scope.ScopeStack which is the active scope, or nil when the interpreted body
// failed.
func (a *emitterAdapter) ScopeStack() *scope.ScopeStack {
	results := a.core.InvokeRecordingError("ScopeStack")
	stack, _ := resultAs[*scope.ScopeStack](results, 0)
	return stack
}

// SetDebugPosition records pos as the source position of the instructions emitted next.
//
// Takes pos (token.Pos) which is the source position.
func (a *emitterAdapter) SetDebugPosition(ctx context.Context, pos token.Pos) {
	a.core.InvokeRecordingError("SetDebugPosition", interfaceArgument[context.Context](ctx), reflect.ValueOf(pos))
}

// CompileExpression compiles expression through the standard walker.
//
// Takes expression (ast.Expr) which is the AST node.
//
// Returns program.VarLocation which is the result destination.
// Returns error when compilation fails.
func (a *emitterAdapter) CompileExpression(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	results, err := a.core.Invoke("CompileExpression", interfaceArgument[context.Context](ctx), interfaceArgument[ast.Expr](expression))
	return locationAndError(results, err)
}

// CompileStmt compiles statement through the standard walker.
//
// Takes statement (ast.Stmt) which is the AST node.
//
// Returns program.VarLocation which is the result destination.
// Returns error when compilation fails.
func (a *emitterAdapter) CompileStmt(ctx context.Context, statement ast.Stmt) (program.VarLocation, error) {
	results, err := a.core.Invoke("CompileStmt", interfaceArgument[context.Context](ctx), interfaceArgument[ast.Stmt](statement))
	return locationAndError(results, err)
}

// CompileForFallback compiles a for statement through the scalar path, bypassing
// recognition.
//
// Takes statement (*ast.ForStmt) which is the for statement AST node.
//
// Returns program.VarLocation which is the result destination.
// Returns error when compilation fails.
func (a *emitterAdapter) CompileForFallback(ctx context.Context, statement *ast.ForStmt) (program.VarLocation, error) {
	results, err := a.core.Invoke("CompileForFallback", interfaceArgument[context.Context](ctx), reflect.ValueOf(statement))
	return locationAndError(results, err)
}

// locationAndError unpacks the (program.VarLocation, error) result pair of a compile
// method, preferring the invocation error when the body did not complete.
//
// Takes results ([]reflect.Value) which are the method's results.
// Takes err (error) which is the invocation error, or nil.
//
// Returns program.VarLocation which is the destination the body reported.
// Returns error which is the invocation error or the body's error result.
func locationAndError(results []reflect.Value, err error) (program.VarLocation, error) {
	if err != nil {
		return program.VarLocation{}, err
	}
	location, _ := resultAs[program.VarLocation](results, 0)
	failure, _ := resultAs[error](results, 1)
	return location, failure
}

// resultAs extracts result index as T, treating a missing, invalid or nil-interface
// result as the zero value.
//
// Takes results ([]reflect.Value) which are the method's results.
// Takes index (int) which selects the result.
//
// Returns T which is the converted result.
// Returns bool which is true when the result was present and valid.
func resultAs[T any](results []reflect.Value, index int) (T, bool) {
	var zero T
	if index >= len(results) {
		return zero, false
	}
	value := results[index]
	if !value.IsValid() {
		return zero, false
	}
	if (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) && value.IsNil() {
		return zero, false
	}
	return reflect.TypeAssert[T](value)
}

// interfaceArgument wraps an interface-typed argument so a nil interface still crosses as
// a valid, typed reflect.Value.
//
// Takes value (T) which is the argument.
//
// Returns reflect.Value which is addressed to the interface type T.
func interfaceArgument[T any](value T) reflect.Value {
	return reflect.ValueOf(&value).Elem()
}
