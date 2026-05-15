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
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/symtab"
)

// CompilerConfig carries everything the orchestration layer supplies to a new compiler.
type CompilerConfig struct {
	// FileSet resolves AST positions to files and lines.
	FileSet *token.FileSet

	// Info carries the go/types results for the package being compiled.
	Info *types.Info

	// Function is the function whose body the compiler emits into.
	Function *program.CompiledFunction

	// RootFunction owns the function table shared across the compilation.
	RootFunction *program.CompiledFunction

	// FunctionTable seeds the name -> function-index table; nil allocates an empty one.
	FunctionTable map[string]uint16

	// GlobalVariables seeds the global-variable table; nil allocates an empty one.
	GlobalVariables map[string]program.GlobalVariableInfo

	// Symbols resolves native Go symbols referenced by the source.
	Symbols *symtab.SymbolRegistry

	// Globals is the store package-level variables are allocated in.
	Globals *engine.GlobalStore

	// Patterns is the for-statement recogniser registry; nil selects
	// astpattern.DefaultRegistry, which holds the SIMD kernels and the loop unroller.
	Patterns *patterns.Registry

	// ScopeName labels the compiler's outermost scope in diagnostics.
	ScopeName string

	// Features gates optional language features.
	Features policy.InterpFeature

	// Passes selects which optimisations run: the bytecode passes, the inliner's
	// self-recursive unrolling and the AST-pattern recognisers. The zero value switches
	// every one of them off, so callers start from passes.DefaultOptions.
	Passes passes.Options

	// DebugEnabled records whether source maps and variable tables are emitted.
	DebugEnabled bool

	// MaxLiteralElements caps composite-literal size; 0 means no cap.
	MaxLiteralElements int

	// MaxExpressionDepth caps expression nesting; 0 means no cap.
	MaxExpressionDepth int
}

// EscapeContext builds a fresh read-only view of the current compilation state for the
// escape analyses.
//
// Returns *escape.Context describing the compilation as it stands.
func (c *Compiler) EscapeContext() *escape.Context {
	return &escape.Context{
		Info:              c.Info,
		Function:          c.Function,
		RootFunction:      c.RootFunction,
		FunctionTable:     c.functionTable,
		Substitutions:     c.typeSubstitutions,
		SubstitutionCache: c.typeSubstitutionsCache,
	}
}

// NewCompiler builds a compiler from the orchestration layer's configuration.
//
// Takes config (CompilerConfig) which describes what to compile and under which limits.
//
// Returns *Compiler with its debug info initialised and ready to Emit.
func NewCompiler(ctx context.Context, config CompilerConfig) *Compiler {
	scopeName := config.ScopeName
	if scopeName == "" {
		scopeName = "<root>"
	}
	return newFunctionCompiler(ctx, newProgramContext(config), config.Function, functionOptions{
		scopeName:         scopeName,
		upvalues:          nil,
		substitutions:     nil,
		substitutionCache: nil,
		rangeOverFunction: nil,
	})
}
