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
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/compile/appendlower"
	"pipit.sh/pipit/internal/compile/astpattern"
	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab"
)

// programContext is the per-compilation state shared by all function compilers.
type programContext struct {
	// reflectTypeCache caches reflect.Type synthesis for named types, preventing mutually
	// recursive types from producing structurally distinct reflect.Types per cycle entry
	// point.
	reflectTypeCache map[types.Type]reflect.Type

	// RootFunction is the top-level function where all compiled functions are registered.
	// For sub-compilers, this points to the parent's root function.
	RootFunction *program.CompiledFunction

	// globals is the runtime global store used for allocating package-level variable slots
	// at compile time.
	globals *engine.GlobalStore

	// symbols provides access to pre-registered native symbols for resolving imported
	// package references at compile time.
	symbols *symtab.SymbolRegistry

	// patterns is the for-statement recogniser registry compileFor consults; nil recognises
	// nothing.
	patterns *patterns.Registry

	// Info is the type information from go/types.Check().
	Info *types.Info

	// globalVariables maps package-level variable names to their location in the
	// GlobalStore. Shared across all Compiler instances for the same compilation unit.
	globalVariables map[string]program.GlobalVariableInfo

	// addressTakenGlobals names the package variables whose address the program takes;
	// filled by markAddressTakenPackageVars before registerPackageLevelVar runs, so those
	// variables get a pointer cell (program.GlobalVariableInfo.IsIndirect).
	addressTakenGlobals map[string]bool

	// functionTable maps function names to indices in rootFunction.functions.
	functionTable map[string]uint16

	// functionDeclarations maps each top-level function name to its declaration, so the
	// escape classifier can follow an address passed to a package function into its body.
	// Methods are not recorded.
	functionDeclarations map[string]*ast.FuncDecl

	// FileSet is the file set from parsing.
	FileSet *token.FileSet

	// debugFiles is the file table every function's source map shares when debug info is
	// enabled. The root compiler allocates it; sub-compilers reuse the pointer.
	debugFiles *[]string

	// runtimePackageName qualifies runtime function names: the import path of the package
	// being compiled, or its package name for main and path-less packages.
	runtimePackageName string

	// initFunctionIndices holds indices (into rootFunction.functions) of init() functions in
	// source order, for auto-execution before _eval_.
	initFunctionIndices []uint16

	// maxExpressionDepth caps the recursion depth when compiling nested expressions. Zero
	// means use defaultMaxExpressionDepth (1024).
	maxExpressionDepth int

	// maxLiteralElements is the maximum number of elements in a single composite literal.
	// Zero means unlimited.
	maxLiteralElements int

	// features controls which Go language constructs are allowed during compilation.
	features policy.InterpFeature

	// passOptions selects which optimisations this compilation runs; the optimiser, the
	// inliner and the AST-pattern recognisers all consult it.
	passOptions passes.Options

	// debugEnabled is true when debug info generation is active.
	debugEnabled bool
}

// newProgramContext builds the shared per-program state from a CompilerConfig.
//
// Takes config (CompilerConfig) which provides the inputs.
//
// Returns *programContext ready for use by function compilers.
func newProgramContext(config CompilerConfig) *programContext {
	functionTable := config.FunctionTable
	if functionTable == nil {
		functionTable = make(map[string]uint16)
	}
	globalVariables := config.GlobalVariables
	if globalVariables == nil {
		globalVariables = make(map[string]program.GlobalVariableInfo)
	}
	return new(programContext{
		FileSet:              config.FileSet,
		Info:                 config.Info,
		RootFunction:         config.RootFunction,
		globals:              config.Globals,
		symbols:              config.Symbols,
		globalVariables:      globalVariables,
		addressTakenGlobals:  nil,
		functionTable:        functionTable,
		functionDeclarations: make(map[string]*ast.FuncDecl),
		initFunctionIndices:  nil,
		features:             config.Features,
		passOptions:          config.Passes,
		patterns:             patternRegistry(config.Patterns),
		debugEnabled:         config.DebugEnabled,
		runtimePackageName:   "",
		maxLiteralElements:   config.MaxLiteralElements,
		maxExpressionDepth:   config.MaxExpressionDepth,
		reflectTypeCache:     nil,
		debugFiles:           nil,
	})
}

// stmtScratch is the per-statement state a function compiler mutates as it descends into
// nested statements and expressions. Grouped so its lifetime is visible: nothing here
// outlives the statement being compiled.
type stmtScratch struct {
	// loopPost is non-nil while compiling a for-loop post statement (e.g. i++), entered and
	// left through withLoopPost() so it can never leak past the post statement.
	loopPost *loopPostScope

	// pendingLabel is set when compiling a labelled statement, so that the inner loop or
	// switch can attach it to its breakable context for labelled break/continue.
	pendingLabel string

	// breakables is a stack of contexts for break/continue targets. Loops and switches push
	// onto this stack.
	breakables []breakableContext

	// expressionDepth tracks the current depth of compileExpression recursion, capped at
	// expressionDepthLimit so that pathologically deep input cannot exhaust the host stack.
	expressionDepth int

	// currentPosition is the current source position set before compiling each statement or
	// expression. Used by the Emit hook to record source positions.
	currentPosition token.Pos

	// loopDepth tracks nested loop bodies so compileDefer can check whether the defer it is
	// compiling lives inside a loop. compileFor / compileForRange / compileSelect with loop
	// semantics increment on entry and decrement on exit.
	loopDepth int
}

// loopPostScope tracks the for-loop init-clause variables so the post statement can
// suppress opWriteSharedCell for them under Go 1.22+ per-iteration scoping, while keeping
// it for outer-scope variables.
type loopPostScope struct {
	// initDecls holds the (register, kind) pairs of the variables declared in the init
	// clause.
	initDecls map[loopPostDeclKey]struct{}
}

// functionOptions carries the per-function inputs that differ between the kinds of
// function compiler: the scope name, the closure's upvalues, a specialisation's type
// substitutions and the range-over-func yield context.
type functionOptions struct {
	// upvalues maps captured names to their upvalue references; nil for non-closures.
	upvalues map[string]upvalueReference

	// substitutions maps type parameters to concrete types inside a specialisation.
	substitutions map[*types.TypeParam]types.Type

	// substitutionCache memoises substituted types for the specialisation.
	substitutionCache map[types.Type]types.Type

	// rangeOverFunction is the yield-body context for a range-over-func loop body.
	rangeOverFunction *rangeOverFunctionContext

	// scopeName names the register scope, for diagnostics.
	scopeName string
}

// bodySpec names what prepareBody analyses before a body is compiled.
type bodySpec struct {
	// body is the function body.
	body *ast.BlockStmt

	// params is the parameter list, consulted by the in-place append alias analysis.
	params *ast.FieldList

	// resultTypes are the declared result types, in order.
	resultTypes []types.Type
}

// finishFlags selects the optional epilogue steps.
type finishFlags struct {
	// tailCall rewrites a trailing direct call into a tail call.
	tailCall bool

	// finaliseDefer classifies the body's defers as trivial or general.
	finaliseDefer bool

	// classifyEscapes records which allocation sites stay arena-safe.
	classifyEscapes bool
}

// withLoopPost runs fn with loopPost set for the init-declared registers in initDecls,
// restoring the previous scope afterwards even when fn fails.
//
// Takes initDecls (map[loopPostDeclKey]struct{}) which are the init-declared registers.
// Takes fn (func() error) which compiles the post statement.
//
// Returns error from fn.
func (c *Compiler) withLoopPost(initDecls map[loopPostDeclKey]struct{}, fn func() error) error {
	previous := c.loopPost
	c.loopPost = &loopPostScope{initDecls: initDecls}
	defer func() { c.loopPost = previous }()
	return fn()
}

// prepareBody pushes the body scope and runs the per-body analyses every function kind
// needs: heap promotion, closure capture, written locals, typed-slice locals, in-place
// append aliases, recover detection, and the body and result types the emitters consult.
//
// Takes spec (bodySpec) which names the body, its parameters and its result types.
func (c *Compiler) prepareBody(spec bodySpec) {
	c.Scopes.PushScope()
	c.heapPromotedNames = escape.CollectHeapPromotedNames(c.EscapeContext(), spec.body)
	c.closureCapturedNames = escape.CollectClosureCapturedNamesAll(spec.body)
	c.writtenLocalNames = escape.CollectWrittenLocalNames(spec.body)
	if checkCaptureEnabled {
		c.captureCheckNames = escape.CollectDirectlyAssignedNames(c.Info, spec.body)
	}
	c.classifyTypedSliceLocals(spec.body)
	c.inPlaceAppendAliases = appendlower.CollectAliases(c.Info, spec.params, spec.body)
	c.hasRecover = bodyContainsRecoverCall(c.Info, spec.body)
	c.Function.HasRecover = c.hasRecover
	c.Function.InspectsCallStack = bodyInspectsCallStack(c.Info, spec.body)
	c.hasDefers = bodyContainsDefer(spec.body)
	c.currentBody = spec.body
	c.currentResultTypes = spec.resultTypes
}

// finishBody runs the epilogue every function kind shares: the tail-call rewrite, the
// resource check, the register count, defer classification, escape classification, the
// optimiser and the scope pop. Errors come back unwrapped; the caller adds the function
// name.
//
// Takes compiledFunction (*program.CompiledFunction) which received the body.
// Takes body (*ast.BlockStmt) which is the compiled body, for escape classification.
// Takes flags (finishFlags) which selects the optional steps.
//
// Returns the first error from the resource check, escape classification or optimiser.
func (c *Compiler) finishBody(ctx context.Context, compiledFunction *program.CompiledFunction, body *ast.BlockStmt, flags finishFlags) error {
	if flags.tailCall {
		c.rewriteTrailingCallAsTailCall(compiledFunction)
	}
	if err := c.resourceError(); err != nil {
		return err
	}
	compiledFunction.NumRegisters = c.Scopes.PeakRegisters()
	if flags.finaliseDefer {
		finaliseSimpleDeferClassification(c, compiledFunction)
	}
	if flags.classifyEscapes {
		sites := escape.LocalAllocationSites{IndirectCellPCs: c.escapeAllocSitePCs, MakeSliceExtensionPCs: c.escapeMakeSitePCs}
		if err := escape.ClassifyLocalEscapes(ctx, compiledFunction, body, sites, c.heapPromotedNames, c.functionDeclarations); err != nil {
			return err
		}
	}
	if err := passes.OptimiseFunction(ctx, c.passOptions, compiledFunction); err != nil {
		return err
	}
	c.recordEpiloguePosition(ctx, body)
	c.Scopes.PopScope()
	return nil
}

// recordEpiloguePosition gives the slot one past the body the position of the closing
// brace, so a frame that ran off the end of its body reports the brace's line the way Go
// reports an implicit return.
//
// Takes body (*ast.BlockStmt) which supplies the closing brace.
func (c *Compiler) recordEpiloguePosition(ctx context.Context, body *ast.BlockStmt) {
	sm := c.debugSourceMap
	if sm == nil || body == nil || !body.Rbrace.IsValid() || c.FileSet == nil {
		return
	}
	position := c.FileSet.Position(body.Rbrace)
	sm.Epilogue = program.SourcePosition{
		Line:    safeconv.IntToInt32(position.Line),
		Column:  safeconv.IntToInt16(position.Column),
		FileID:  c.resolveFileID(ctx, position.Filename),
		Inlined: false,
	}
}

// newFunctionCompiler builds the compiler for one function body. Every function compiler,
// from the root down to closures, yield bodies and specialisations, is created here so
// the per-function state is initialised in exactly one place.
//
// Takes prog (*programContext) which is the shared per-program state.
// Takes fn (*program.CompiledFunction) which receives the emitted bytecode.
// Takes opts (functionOptions) which carries the inputs that differ per function kind.
//
// Returns the ready compiler; the caller pushes the body scope through prepareBody or
// explicitly.
func newFunctionCompiler(ctx context.Context, prog *programContext, fn *program.CompiledFunction, opts functionOptions) *Compiler {
	c := new(Compiler{
		programContext:         prog,
		Function:               fn,
		Scopes:                 scope.NewScopeStack(opts.scopeName),
		upvalueMap:             opts.upvalues,
		typeSubstitutions:      opts.substitutions,
		typeSubstitutionsCache: opts.substitutionCache,
		rangeOverFunction:      opts.rangeOverFunction,

		lastCallPC:         -1,
		escapeAllocSitePCs: nil,
		escapeMakeSitePCs:  nil,
		destructuredLocals: nil,
		labelTable:         nil,
		forwardGotos:       nil,
		structLayoutIndex:  nil,
	})
	c.initDebugInfo(ctx)
	return c
}

// patternRegistry returns the configured recogniser registry, or the astpattern default
// when the configuration left it nil.
//
// Takes configured (*patterns.Registry) which is CompilerConfig.Patterns.
//
// Returns *patterns.Registry which compileFor consults.
func patternRegistry(configured *patterns.Registry) *patterns.Registry {
	if configured != nil {
		return configured
	}
	return astpattern.DefaultRegistry()
}

// resultTypesOf lists a signature's result types in order.
//
// Takes signature (*types.Signature) which is the function signature.
//
// Returns []types.Type which lists the result types, or nil when the signature has none.
func resultTypesOf(signature *types.Signature) []types.Type {
	if signature == nil || signature.Results() == nil || signature.Results().Len() == 0 {
		return nil
	}
	results := signature.Results()
	out := make([]types.Type, results.Len())
	for i := range results.Len() {
		out[i] = results.At(i).Type()
	}
	return out
}
