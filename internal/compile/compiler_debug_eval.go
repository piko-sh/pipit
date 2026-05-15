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

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// DebugParameter is one variable of a paused frame that a debugger expression reads. The
// expression function takes it as a parameter in the bank the frame holds it in.
type DebugParameter struct {
	// Type is the variable's static type, as the type checker resolved it at the paused
	// position.
	Type types.Type

	// Name is the variable's source name.
	Name string

	// Kind is the register bank the frame holds the variable in.
	Kind isa.RegisterKind
}

// DebugExpressionSpec describes a debugger expression to compile.
type DebugExpressionSpec struct {
	// Expression is the type-checked expression; config.Info must carry its types.
	Expression ast.Expr

	// Root is the paused program's root function, whose function table the expression may
	// call into and which receives any closure the expression creates.
	Root *program.CompiledFunction

	// Parameters are the frame variables the expression reads, in the order the caller
	// passes their values.
	Parameters []DebugParameter
}

// CompileDebugExpression compiles a debugger expression as the body of a synthetic
// function whose parameters are the paused frame's variables.
//
// Takes config (CompilerConfig) which supplies the compilation environment.
// Takes spec (DebugExpressionSpec) which describes the expression and frame variables.
//
// Returns *program.CompiledFunction which holds the result in register 0.
// Returns error when compilation fails.
func CompileDebugExpression(ctx context.Context, config CompilerConfig, spec DebugExpressionSpec) (*program.CompiledFunction, error) {
	evalFunction := &program.CompiledFunction{Name: "<debug-eval>", RuntimeName: "debug.eval"}
	config.Function = evalFunction
	config.RootFunction = spec.Root
	c := newFunctionCompiler(ctx, newProgramContext(config), evalFunction, functionOptions{
		scopeName:         "<debug-eval>",
		upvalues:          nil,
		substitutions:     nil,
		substitutionCache: nil,
		rangeOverFunction: nil,
	})
	c.Scopes.PushScope()
	c.declareDebugParameters(spec.Parameters)

	location, err := c.compileExpression(ctx, spec.Expression)
	if err != nil {
		return nil, fmt.Errorf("compiling debug expression: %w", err)
	}
	location = c.coerceEvalBoolResult(ctx, config.Info, spec.Expression, location)
	c.emitMoveToRegisterZero(ctx, location)

	c.Function.ResultKinds = []isa.RegisterKind{location.Kind}
	c.Function.ResultReflectTypes = []reflect.Type{location.SourceType}
	if err := c.resourceError(); err != nil {
		return nil, fmt.Errorf("compiling debug expression: %w", err)
	}
	c.Function.NumRegisters = c.Scopes.PeakRegisters()
	if err := passes.OptimiseFunction(ctx, c.passOptions, c.Function); err != nil {
		return nil, fmt.Errorf("compiling debug expression: %w", err)
	}
	c.Scopes.PopScope()
	evalFunction.ShareFunctionsWith(spec.Root)
	return c.Function, nil
}

// declareDebugParameters declares each frame variable as a parameter in its own bank.
//
// Takes parameters ([]DebugParameter) which are the frame variables, in argument order.
func (c *Compiler) declareDebugParameters(parameters []DebugParameter) {
	for _, parameter := range parameters {
		location := c.Scopes.DeclareVar(parameter.Name, parameter.Kind)
		if isa.IsTypedSliceKind(parameter.Kind) {
			if c.typedSliceLocals == nil {
				c.typedSliceLocals = make(map[string]isa.RegisterKind)
			}
			c.typedSliceLocals[parameter.Name] = parameter.Kind
		}
		c.Function.ParameterKinds = append(c.Function.ParameterKinds, parameter.Kind)
		c.Function.ParameterRegisters = append(c.Function.ParameterRegisters, location.Register)
		c.Function.ParameterTypeRefs = append(c.Function.ParameterTypeRefs, parameter.Type)
	}
}

// recordUpvalueDebugEntries adds the closure's captured variables to its debug var table,
// so the debugger can show them alongside the locals.
//
// Takes upvalueMap (map[string]upvalueReference) which is the closure's capture table.
func (c *Compiler) recordUpvalueDebugEntries(upvalueMap map[string]upvalueReference) {
	table := c.Function.DebugVarTable
	if table == nil {
		return
	}
	for name, reference := range upvalueMap {
		table.Entries = append(table.Entries, program.DebugVarEntry{
			Name: name,
			Location: program.VarLocation{
				SourceType:   nil,
				UpvalueIndex: reference.index,
				SpillSlot:    0,
				Register:     0,
				Kind:         reference.kind,
				IsUpvalue:    true,
				IsIndirect:   reference.isIndirect,
				OriginalKind: reference.originalKind,
				IsCaptured:   false,
				IsSpilled:    false,
			},
			StartPC: 0,
			EndPC:   0,
		})
	}
}
