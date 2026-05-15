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

package debug

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"reflect"
	"slices"
	"sync"
	"time"

	"pipit.sh/pipit/internal/compile"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// defaultEvalTimeout bounds an expression evaluation when the caller sets none.
const defaultEvalTimeout = 2 * time.Second

// compiledExpression is an expression compiled against one frame position, kept so a
// breakpoint condition compiles once and runs on every hit.
type compiledExpression struct {
	// function is the compiled expression body.
	function *program.CompiledFunction

	// resultType is the Go type the expression evaluates to.
	resultType types.Type

	// plan lists the frame variables the expression reads, in argument order.
	plan []program.DebugVarEntry
}

// conditionKey identifies a compiled breakpoint condition by the breakpoint and the site
// it fired at.
type conditionKey struct {
	// breakpoint is the breakpoint whose condition was compiled.
	breakpoint *engine.DebugBreakpoint

	// function is the function the breakpoint fired in.
	function *program.CompiledFunction

	// pc is the program counter where the breakpoint fired.
	pc int
}

// evaluator compiles and runs expressions against a paused frame of a bound program.
type evaluator struct {
	// binding is the program the evaluator compiles against.
	binding *ProgramBinding

	// conditions caches compiled breakpoint conditions by site.
	conditions map[conditionKey]*compiledExpression

	// mu guards conditions.
	mu sync.Mutex
}

// newEvaluator builds an evaluator over a binding.
//
// Takes binding (*ProgramBinding) which supplies types and compiler configuration.
//
// Returns *evaluator which is ready to evaluate.
func newEvaluator(binding *ProgramBinding) *evaluator {
	return &evaluator{binding: binding, conditions: make(map[conditionKey]*compiledExpression), mu: sync.Mutex{}}
}

// evaluate type-checks an expression at the frame's position, compiles it as a function
// of the frame variables it reads, runs it on a scratch VM with copies of those values
// and returns the result.
//
// Takes vm (*engine.VM) which is the paused VM.
// Takes frame (engine.DebugFrame) which is the frame's source position.
// Takes callFrame (*engine.CallFrame) which holds the frame's variables.
// Takes expression (string) which is the Go expression.
// Takes opts (EvalOptions) which tune the evaluation.
//
// Returns EvalResult which is the value and its type.
// Returns error when the expression fails to compile or run.
func (e *evaluator) evaluate(ctx context.Context, vm *engine.VM, frame engine.DebugFrame, callFrame *engine.CallFrame, expression string, opts EvalOptions) (EvalResult, error) {
	compiled, err := e.compile(ctx, frame, callFrame, expression, opts.AllowCalls)
	if err != nil {
		return EvalResult{Value: nil, Type: ""}, err
	}
	value, err := e.run(ctx, vm, callFrame, compiled, opts.Timeout)
	if err != nil {
		return EvalResult{Value: nil, Type: ""}, err
	}
	pkg := e.binding.packageAt(frame)
	typeName := ""
	if compiled.resultType != nil {
		qualifier := types.Qualifier(nil)
		if pkg != nil && pkg.Package != nil {
			qualifier = types.RelativeTo(pkg.Package)
		}
		typeName = types.TypeString(compiled.resultType, qualifier)
	}
	return EvalResult{Value: value, Type: typeName}, nil
}

// condition evaluates a breakpoint condition in the paused thread's innermost frame,
// compiling it on the first hit at each site.
//
// Takes thread (*engine.DebugThread) which is the paused thread.
// Takes breakpoint (*engine.DebugBreakpoint) which is the breakpoint to evaluate.
//
// Returns bool which is the condition's value.
// Returns error when it could not be evaluated or is not boolean.
//
// Concurrency: safe for concurrent use; acquires e.mu.
func (e *evaluator) condition(thread *engine.DebugThread, breakpoint *engine.DebugBreakpoint) (bool, error) {
	frame, callFrame, ok := thread.EvaluationFrame()
	if !ok {
		return false, fault.ErrVariableUnavailable
	}
	key := conditionKey{breakpoint: breakpoint, function: frame.Function, pc: frame.PC}
	e.mu.Lock()
	compiled, cached := e.conditions[key]
	e.mu.Unlock()
	if !cached {
		var err error
		compiled, err = e.compile(context.Background(), frame, callFrame, breakpoint.Condition, true)
		if err != nil {
			return false, err
		}
		if basic, isBasic := compiled.resultType.Underlying().(*types.Basic); !isBasic || basic.Kind() != types.Bool && basic.Kind() != types.UntypedBool {
			return false, fault.ErrConditionNotBool
		}
		e.mu.Lock()
		e.conditions[key] = compiled
		e.mu.Unlock()
	}
	value, err := e.run(context.Background(), thread.VM(), callFrame, compiled, 0)
	if err != nil {
		return false, err
	}
	holds, isBool := value.(bool)
	if !isBool {
		return false, fault.ErrConditionNotBool
	}
	return holds, nil
}

// compile parses, type-checks and compiles an expression at a frame position.
//
// Takes frame (engine.DebugFrame) which is the frame's source position.
// Takes callFrame (*engine.CallFrame) which holds the frame's variables.
// Takes expression (string) which is the Go expression.
// Takes allowCalls (bool) which permits calls other than builtins.
//
// Returns *compiledExpression which is ready to run.
// Returns error when any stage fails.
func (e *evaluator) compile(ctx context.Context, frame engine.DebugFrame, callFrame *engine.CallFrame, expression string, allowCalls bool) (*compiledExpression, error) {
	binding := e.binding
	pkg := binding.packageAt(frame)
	if pkg == nil || pkg.Package == nil || binding.Compiled == nil {
		return nil, fault.ErrNoProgramBinding
	}
	position := binding.positionOf(frame.File, frame.Line, frame.Column)
	if !position.IsValid() {
		return nil, fmt.Errorf("%w: the paused frame has no source position", fault.ErrVariableUnavailable)
	}
	parsed, err := parser.ParseExprFrom(binding.FileSet, "<debug-eval>", expression, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing expression: %w", err)
	}
	info := newTypesInfo()
	if err := types.CheckExpr(binding.FileSet, pkg.Package, position, parsed, info); err != nil {
		return nil, fmt.Errorf("type-checking expression: %w", err)
	}
	if !allowCalls {
		if err := rejectCalls(parsed, info); err != nil {
			return nil, err
		}
	}
	parameters, plan, err := planParameters(info, callFrame, frame.PC, pkg.Package)
	if err != nil {
		return nil, err
	}
	config := binding.Compiler
	config.FileSet = binding.FileSet
	config.Info = info
	config.FunctionTable = pkg.FunctionTable
	config.GlobalVariables = pkg.GlobalVariables
	config.Globals = binding.Globals
	config.Symbols = binding.Symbols
	config.ScopeName = "<debug-eval>"
	config.DebugEnabled = false
	function, err := compile.CompileDebugExpression(ctx, config, compile.DebugExpressionSpec{Expression: parsed, Root: binding.Compiled.Root(), Parameters: parameters})
	if err != nil {
		return nil, err
	}
	return &compiledExpression{function: function, resultType: info.Types[parsed].Type, plan: plan}, nil
}

// run copies the plan's variables out of the paused frame and runs the compiled
// expression on a scratch VM.
//
// Takes vm (*engine.VM) which is the paused VM.
// Takes callFrame (*engine.CallFrame) which holds the frame's variables.
// Takes compiled (*compiledExpression) which is the expression to run.
// Takes timeout (time.Duration) which bounds the run; zero selects the default.
//
// Returns any which is the result.
// Returns error when a variable is unavailable or the run fails.
func (e *evaluator) run(ctx context.Context, vm *engine.VM, callFrame *engine.CallFrame, compiled *compiledExpression, timeout time.Duration) (any, error) {
	arguments := make([]reflect.Value, 0, len(compiled.plan))
	for _, entry := range compiled.plan {
		value := vm.ReadFrameVariable(callFrame, entry)
		if !value.IsValid() {
			return nil, fmt.Errorf("%w: %s has no value", fault.ErrVariableUnavailable, entry.Name)
		}
		arguments = append(arguments, value)
	}
	if timeout <= 0 {
		timeout = defaultEvalTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	scratch := engine.NewScratchVM(runCtx, e.binding.Globals, e.binding.Symbols, e.binding.Limits)
	return scratch.RunCompiledFunction(e.binding.Compiled, compiled.function, arguments)
}

// planParameters lists the frame variables the type-checked expression uses, matched to
// the var-table entries live at pc.
//
// Takes info (*types.Info) which is the expression's type information.
// Takes callFrame (*engine.CallFrame) which is the live call frame.
// Takes pc (int) which is the program counter.
// Takes pkg (*types.Package) whose scope holds the globals, which are not frame
// variables.
//
// Returns []compile.DebugParameter and the matching []program.DebugVarEntry in the same
// order, or error which wraps fault.ErrVariableUnavailable.
func planParameters(info *types.Info, callFrame *engine.CallFrame, pc int, pkg *types.Package) ([]compile.DebugParameter, []program.DebugVarEntry, error) {
	seen := make(map[string]types.Type)
	for _, object := range info.Uses {
		variable, isVar := object.(*types.Var)
		if !isVar || variable.IsField() || variable.Parent() == nil || variable.Parent() == pkg.Scope() || variable.Parent() == types.Universe {
			continue
		}
		seen[variable.Name()] = variable.Type()
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	slices.Sort(names)
	parameters := make([]compile.DebugParameter, 0, len(names))
	plan := make([]program.DebugVarEntry, 0, len(names))
	for _, name := range names {
		entry, ok := lookupEntry(callFrame, pc, name)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %s", fault.ErrVariableUnavailable, name)
		}
		parameters = append(parameters, compile.DebugParameter{Type: seen[name], Name: name, Kind: parameterKind(entry.Location)})
		plan = append(plan, entry)
	}
	return parameters, plan, nil
}

// parameterKind picks the bank a frame variable's value is handed to the expression in.
//
// Takes location (program.VarLocation) which is the variable's location.
//
// Returns isa.RegisterKind which is the typed bank for a heap-promoted variable, else the
// location's own bank.
func parameterKind(location program.VarLocation) isa.RegisterKind {
	if location.IsIndirect && location.OriginalKind != 0 {
		return location.OriginalKind
	}
	return location.Kind
}

// rejectCalls refuses an expression that calls anything but a builtin or a conversion.
//
// Takes expression (ast.Expr) which is the parsed expression.
// Takes info (*types.Info) which is its type information.
//
// Returns error which is fault.ErrCallsNotAllowed when a call is present.
func rejectCalls(expression ast.Expr, info *types.Info) error {
	var found error
	ast.Inspect(expression, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall || found != nil {
			return found == nil
		}
		funType := info.Types[call.Fun]
		if funType.IsBuiltin() || funType.IsType() {
			return true
		}
		found = fault.ErrCallsNotAllowed
		return false
	})
	return found
}

// newTypesInfo creates a types.Info with every map the compiler reads.
//
// Returns *types.Info ready for types.CheckExpr.
func newTypesInfo() *types.Info {
	return &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Instances:    make(map[*ast.Ident]types.Instance),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:    nil,
		Scopes:       nil,
		InitOrder:    nil,
		FileVersions: nil,
	}
}
