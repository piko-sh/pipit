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
	"go/token"
	"go/types"
	"maps"
	"reflect"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/liveness"
	"pipit.sh/pipit/internal/compile/scope"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/compile/passes"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// rangeOverFunctionReturnPendingFlag is the state-flag value that signals a return
	// statement was encountered inside the yield body.
	rangeOverFunctionReturnPendingFlag int64 = 2

	// rangeOverFunctionFirstLabelFlag is the first state-flag value assigned to labelled
	// outer-loop targets in range-over-func bodies. Values 0-2 are reserved
	// (normal/break/return-pending).
	rangeOverFunctionFirstLabelFlag int64 = 3

	// commaOkResultCount is the number of LHS variables in a comma-ok assignment (v, ok :=
	// ...).
	commaOkResultCount = 2

	// rangeKeyFlag is the bit flag indicating a key variable is present in a range
	// iteration.
	rangeKeyFlag uint8 = 1

	// rangeValueFlag is the bit flag indicating a value variable is present in a range
	// iteration.
	rangeValueFlag uint8 = 2

	// identTrue is the predeclared boolean true identifier.
	identTrue = "true"

	// identFalse is the predeclared boolean false identifier.
	identFalse = "false"

	// identNil is the predeclared nil identifier.
	identNil = "nil"

	// InitFunctionName is the name of Go init functions.
	InitFunctionName = "init"

	// EvalFunctionName is the synthetic function name used by the interpreter for eval
	// snippet execution.
	EvalFunctionName = "_eval_"

	// replaceAllArgCount is the expected argument count for strings.ReplaceAll intrinsic
	// compilation.
	replaceAllArgCount = 3

	// makeSliceMinCapArgs is the minimum argument count for a make() call to include an
	// explicit capacity argument.
	makeSliceMinCapArgs = 3

	// initialFileTableCapacity is the initial capacity for the source map's shared file
	// table during debug compilation.
	initialFileTableCapacity = 4

	// defaultMaxExpressionDepth caps expression recursion to prevent stack exhaustion.
	defaultMaxExpressionDepth = 1024

	// maxStatementDepth caps recursion when compiling statements. The cap is per-function
	// because each closure body uses a fresh Compiler.
	maxStatementDepth = 1024

	// compileLoopCheckMask is the iteration-count mask used by the main statement-list walk
	// to amortise the cost of polling ctx.Err() across every (mask+1) statements, mirroring
	// OptimisationLoopCheckMask.
	compileLoopCheckMask = 1023
)

const (
	// identTargetUnknown means no binding was found; callers report the error.
	identTargetUnknown identTargetKind = iota

	// identTargetLocal is a variable of the current function's scopes.
	identTargetLocal

	// identTargetUpvalue is a variable captured from an enclosing function.
	identTargetUpvalue

	// identTargetGlobal is a package-level variable.
	identTargetGlobal
)

// Compiler translates type-checked Go AST into bytecode.
//
//exhaustruct:ignore
type Compiler struct {
	// programContext is the per-program state shared by every function compiler in one
	// compilation. Embedded by pointer so sub-compilers share it.
	*programContext

	// stickyError records the first non-recoverable Emit error, surfaced by resourceError
	// after the body completes.
	stickyError error

	// Function is the current function being compiled.
	Function *program.CompiledFunction

	// Scopes tracks nested lexical scopes for register allocation.
	Scopes *scope.ScopeStack

	// rangeOverFunction is non-nil when compiling a range-over-func yield callback body.
	// Controls break/continue/return transformation.
	rangeOverFunction *rangeOverFunctionContext

	// typeSubstitutions maps generic TypeParams to concrete instantiation types. Nil for
	// non-specialised compilers; when non-nil, kindFor / reflectFor / staticTypeOf consult
	// this map so Emit decisions resolve TypeParams to the concrete type and pick typed-bank
	// opcodes.
	typeSubstitutions map[*types.TypeParam]types.Type

	// typeSubstitutionsCache memoises substituteType results within the lifetime of a single
	// specialisation Reset alongside typeSubstitutions when descending into a
	// non-specialised closure.
	typeSubstitutionsCache map[types.Type]types.Type

	// upvalueMap maps captured variable names to their upvalue index and register kind. Only
	// set when compiling a closure body.
	upvalueMap map[string]upvalueReference

	// heapPromotedNames lists local names that must be heap-promoted at declaration.
	// Populated by a pre-pass at function entry so each declareVar site emits
	// opAllocIndirect immediately, giving the variable a stable addressable cell that
	// closures and goroutines can read and write by pointer.
	heapPromotedNames map[string]bool

	// typedSliceLocals maps qualifying typed-slice local names to their registerKind. Only
	// locals whose every reference is an element access, len/cap, range, or make initialiser
	// qualify.
	typedSliceLocals map[string]isa.RegisterKind

	// heapTypedSliceLocals names the typedSliceLocals whose make([]T, ...) is placed on the
	// Go heap rather than in the register arena because the local flows out of the frame
	// (container store, return, closure capture, channel send, go or defer argument). See
	// classifyHeapTypedSliceLocals.
	heapTypedSliceLocals map[string]bool

	// captureCheckNames holds the names the enclosing body assigns as a whole, computed only
	// when checkCaptureEnabled, so a closure that captures one of them by value can be
	// reported as a compile error (the capture-by-reference rule missed it).
	captureCheckNames map[string]bool

	// closureCapturedNames is the set of names captured by any inner closure, gating
	// opResetSharedCell so each loop iteration gets a fresh cell.
	closureCapturedNames map[string]bool

	// debugFileIDs deduplicates file names to fileID indices in the source map's files
	// slice.
	debugFileIDs map[string]uint16

	// writtenLocalNames lists locals written after their declaration, letting
	// emitValueCopyForLocalAssignment skip the snapshot copy for struct/array initialisers
	// when the name is absent.
	writtenLocalNames map[string]bool

	// structLayoutIndex deduplicates struct-field layout entries across access sites, keyed
	// by (reflect.Type, field path). Lazily allocated per function body.
	structLayoutIndex map[fieldlayout.StructFieldLayoutKey]uint16

	// labelTable maps label names to their instruction PCs. Set when an *ast.LabeledStmt is
	// encountered during compilation.
	labelTable map[string]int

	// forwardGotos holds goto jumps targeting labels that have not yet been declared,
	// indexed by label name and patched once the label is visited.
	forwardGotos map[string][]int

	// inPlaceAppendAliases names locals with potentially aliased slices, which are refused
	// the in-place append opcode because the slot mutation would be visible through the
	// alias.
	inPlaceAppendAliases map[string]bool

	// debugSourceMap is the source map being built during compilation. Nil when debug info
	// is disabled.
	debugSourceMap *program.SourceMap

	// escapeAllocSitePCs maps each heap-promoted local to the PC of its opAllocIndirect.
	escapeAllocSitePCs map[string]int

	// escapeMakeSitePCs maps each heap-promoted local initialised with make() to the PC of
	// that make's extension word, whose placement flag classifyLocalEscapes clears when the
	// slice backing provably stays in the frame. Per-function scratch only.
	escapeMakeSitePCs map[string]int

	// destructuredLocals maps a destructured slice-element local to its per-field scalar
	// register bindings. Populated by tryCompileDestructuredSliceElem when a `local :=
	// slice[i]` struct copy is compiled as per-field fused loads instead of a boxed element;
	// compileSelectorExpression consults it before any other strategy.
	destructuredLocals map[types.Object]map[string]program.VarLocation

	// currentBody is the AST body of the function this Compiler instance is compiling. Set
	// alongside the per-body analysis maps; the struct-copy destructuring pass scans it to
	// prove a slice-element local is consumed only by scalar field selects.
	currentBody *ast.BlockStmt

	// currentResultTypes records declared result types in source order, used to promote bare
	// nil returns to typed-zero loads.
	currentResultTypes []types.Type

	// trivialDeferPCs records the program counter of every isa.OpDefer emitted in
	// engine.DeferModeTrivial, so finaliseSimpleDeferClassification can downgrade them
	// without rescanning the body.
	trivialDeferPCs []int

	// stmtScratch is the per-statement scratch state, reset as compilation descends into and
	// returns from nested statements.
	stmtScratch

	// deferCount counts defer statements compiled in the current function body. Combined
	// with hasRecover, deferInLoop, and the per-defer thisDeferTrivial classification it
	// determines whether the function qualifies for the trivial-defer fast path.
	deferCount int

	// closureCount numbers the function literals compiled in the current function body, in
	// source order.
	closureCount int

	// callParen is the opening parenthesis of the call expression being compiled, the
	// position Go attributes a call instruction to; token.NoPos outside a call.
	callParen token.Pos

	// statementDepth tracks compileStmt recursion depth, capped at maxStatementDepth.
	statementDepth int

	// lastCallPC is the program counter of the most recent isa.SubOpCall emitted through
	// emitCall, or -1 before the first. rewriteTrailingCallAsTailCall upgrades that call
	// only when it is still the body's last instruction.
	lastCallPC int

	// syntheticCount numbers the identifiers the Compiler synthesises for rewritten
	// statements (range and comma-ok forms whose targets are not identifiers).
	syntheticCount int

	// deferInLoop is true when a defer statement was compiled inside a loop body (for,
	// range, select with persistent select-case loop). Per-iteration registration
	// disqualifies the trivial-defer fast path because there is more than one defer per
	// call.
	deferInLoop bool

	// hasDefers is set to true when a defer statement is compiled. Used to suppress tail
	// call optimisation when defers are present.
	hasDefers bool

	// thisDeferTrivial is true when the most recently compiled defer matched the trivial
	// shape (direct method-value or top-level func target, no closure capture, args
	// resolvable from existing registers). The downgrade pass combines this with deferCount
	// and hasRecover to decide fast-path eligibility.
	thisDeferTrivial bool

	// simpleDeferArgCount records the deferred call's evaluated argument count when
	// classification succeeds. Used by the runtime arena's EnsureCapacity to pre-size the
	// SimpleDeferRecord argument buffer in callee frames for any function that registers a
	// trivial defer.
	simpleDeferArgCount uint8

	// hasRecover is true when an AST walk over the current function body found a call to the
	// predeclared recover() builtin. Set once at the start of compileFunctionBody so
	// compileDefer can read it before classifying any defer it encounters.
	hasRecover bool
}

// GlobalVariables returns the compiler's global-variable table.
//
// Returns map[string]GlobalVariableInfo which is the live table; callers must not mutate
// it.
func (c *Compiler) GlobalVariables() map[string]program.GlobalVariableInfo {
	return c.globalVariables
}

// InitFunctionIndices returns the indices of init() functions, in source order.
//
// Returns []uint16 which is the live slice; callers must not mutate it.
func (c *Compiler) InitFunctionIndices() []uint16 { return c.initFunctionIndices }

// FunctionTable returns the compiler's name -> function-index table.
//
// The orchestration layer copies it into the compiled file set's entrypoint map.
//
// Returns map[string]uint16 which is the live table; callers must not mutate it.
func (c *Compiler) FunctionTable() map[string]uint16 { return c.functionTable }

// TypeToReflect converts a go/types.Type to a reflect.Type.
//
// Threads the Compiler's shared cache so mutually recursive named types yield identical
// reflect.Types across all call sites.
//
// Takes t (types.Type) which is the source type to convert.
//
// Returns the reflect.Type matching t after generic substitution.
func (c *Compiler) TypeToReflect(ctx context.Context, t types.Type) reflect.Type {
	if c.reflectTypeCache == nil {
		c.reflectTypeCache = make(map[types.Type]reflect.Type)
	}
	t = c.substitutedType(t)
	return typemap.TypeToReflectCached(ctx, t, c.symbols, c.reflectTypeCache, c.globals)
}

// recordStickyError stores the first non-recoverable Emit error.
//
// Subsequent errors are dropped to avoid thrashing the field on cascaded failures.
//
// Takes err (error) which is the candidate error to record.
func (c *Compiler) recordStickyError(err error) {
	if err == nil || c.stickyError != nil {
		return
	}
	c.stickyError = err
}

// resourceError returns the first sticky error recorded during Emit.
//
// Returns the overflowError result when no sticky error has been recorded. Used by
// compile entry points to fail fast when a void helper observed a
// pool/method/specialisation overflow.
//
// Returns error which is the first sticky or overflow error, or nil.
func (c *Compiler) resourceError() error {
	if c.stickyError != nil {
		return c.stickyError
	}
	return c.Scopes.OverflowError()
}

// expressionDepthLimit returns the configured expression-depth ceiling.
//
// Substitutes the package default when none was set.
//
// Returns int which is the active expression-depth ceiling.
func (c *Compiler) expressionDepthLimit() int {
	if c.maxExpressionDepth > 0 {
		return c.maxExpressionDepth
	}
	return defaultMaxExpressionDepth
}

// substitutedType returns t with each TypeParam replaced by its concrete instantiation.
//
// When the substitution map is nil (the common non-generic case), t is returned
// unchanged.
//
// Takes t: the type to substitute through the active generic map.
//
// Returns the substituted type, or t unchanged when no substitutions apply.
func (c *Compiler) substitutedType(t types.Type) types.Type {
	if c == nil || c.typeSubstitutions == nil || t == nil {
		return t
	}
	return typemap.SubstituteType(t, c.typeSubstitutions, c.typeSubstitutionsCache)
}

// kindFor maps t through the active substitution and then to a isa.RegisterKind.
//
// Specialised bodies pick typed-bank kinds for the concrete instantiation rather than
// isa.RegisterGeneral.
//
// Takes t: the type whose register kind is required.
//
// Returns the isa.RegisterKind selected for the substituted type.
func (c *Compiler) kindFor(t types.Type) isa.RegisterKind {
	return typemap.KindForType(c.substitutedType(t))
}

// promotionContext builds a KindPromotionContext seeded with the Compiler's generic
// substitution map.
//
// Returns *KindPromotionContext, or nil when no substitutions are active.
func (c *Compiler) promotionContext() *typemap.KindPromotionContext {
	if c == nil || c.typeSubstitutions == nil {
		return nil
	}
	return &typemap.KindPromotionContext{
		Substitutions:     c.typeSubstitutions,
		SubstitutionCache: c.typeSubstitutionsCache,

		Disqualified:          nil,
		BindingName:           "",
		CalleeParamPromotions: nil,
		CalleeParamIndex:      0,
	}
}

// kindForCallSlot picks the call-slot register kind for t.
//
// Takes t (types.Type) which is the call-slot type.
//
// Returns the register kind for the slot.
func (c *Compiler) kindForCallSlot(t types.Type) isa.RegisterKind {
	kind, _ := typemap.KindForPromotedSlot(t, c.promotionContext())
	return kind
}

// parameterSlotKind picks the register-kind for a parameter slot. The variadic last
// parameter stays on the general bank because varargs use a reflect-backed slice.
//
// Takes signature (*types.Signature) which carries the variadic flag.
// Takes parameterType (types.Type) which is the parameter's type.
// Takes parameterIndex (int) which is the parameter's position within Params().
// Takes parameterCount (int) which is the total number of parameters.
//
// Returns the register kind for the slot.
func (c *Compiler) parameterSlotKind(signature *types.Signature, parameterType types.Type, parameterIndex, parameterCount int) isa.RegisterKind {
	if signature.Variadic() && parameterIndex == parameterCount-1 {
		return c.kindFor(parameterType)
	}
	return c.kindForCallSlot(parameterType)
}

// underlyingTypeOf returns the underlying type recorded by go/types for expr. Returns
// (nil, false) when the type map has no entry or the recorded Type is nil.
//
// Takes expr (ast.Expr) which is the expression whose underlying type is required.
//
// Returns the underlying type and true, or (nil, false) when unavailable.
func (c *Compiler) underlyingTypeOf(expr ast.Expr) (types.Type, bool) {
	tv, ok := c.Info.Types[expr]
	if !ok || tv.Type == nil {
		return nil, false
	}
	return tv.Type.Underlying(), true
}

// checkFeature returns an error when feature is not enabled in the Compiler's feature
// set.
//
// Takes feature: the interpreter feature being checked.
// Takes pos: the source position formatted into the error message.
//
// Returns nil when the feature is enabled.
//
// Returns fault.ErrFeatureNotAllowed when the feature is disabled.
func (c *Compiler) checkFeature(feature policy.InterpFeature, pos token.Pos) error {
	if c.features.Has(feature) {
		return nil
	}
	return fmt.Errorf("%w: %s at %s", fault.ErrFeatureNotAllowed, feature, c.FileSet.Position(pos))
}

// coerceEvalBoolResult converts a isa.RegisterInt result to isa.RegisterBool when the
// expression's static type is bool.
//
// Eval returns a bool regardless of whether the value was constant-folded or computed at
// runtime.
//
// Takes info: type information used to read the expression's static type.
// Takes expression: the AST node whose result is being coerced.
// Takes location: the source register holding the comparison or logical result.
//
// Returns the coerced VarLocation in isa.RegisterBool, or the original location when no
// coercion is required.
func (c *Compiler) coerceEvalBoolResult(_ context.Context, info *types.Info, expression ast.Expr, location program.VarLocation) program.VarLocation {
	if location.Kind != isa.RegisterInt {
		return location
	}
	tv, ok := info.Types[expression]
	if !ok {
		return location
	}
	basic, bOk := tv.Type.Underlying().(*types.Basic)
	if !bOk || basic.Kind() != types.Bool {
		return location
	}
	booleanRegister := c.Scopes.Alloc.Alloc(isa.RegisterBool)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpIntToBool), booleanRegister, location.Register)
	return program.VarLocation{Register: booleanRegister, Kind: isa.RegisterBool}
}

// compileStmtList compiles a list of statements in order.
//
// Intermediate registers from non-declaring statements are reclaimed via a watermark
// Restore between statements. Declaring statements (:= and var) are exempt because their
// registers must survive to subsequent statements.
//
// Takes statements: the statements to compile in source order.
//
// Returns the result location of the last statement, or the zero VarLocation when the
// list is empty.
//
// Returns the first compilation error encountered.
func (c *Compiler) compileStmtList(ctx context.Context, statements []ast.Stmt) (program.VarLocation, error) {
	lastUseIndices := liveness.ComputeLastUseIndices(statements)

	var activeDeclarations []activeDeclaration
	var lastLocation program.VarLocation
	for i, statement := range statements {
		if i&compileLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return program.VarLocation{}, err
			}
		}
		watermark := c.Scopes.Alloc.Snapshot()
		location, err := c.compileStmt(ctx, statement)
		if err != nil {
			return program.VarLocation{}, err
		}
		lastLocation = location

		activeDeclarations = c.recycleOrTrackDeclarations(statement, watermark, activeDeclarations)
		activeDeclarations = c.recycleDeadDeclarations(activeDeclarations, lastUseIndices, i)
	}
	return lastLocation, nil
}

// compileStmt compiles a single statement, dispatching by AST node type.
//
// Takes statement: the statement node to compile by dispatching on its AST type.
//
// Returns the result location produced by the compiled statement.
//
// Returns an error when the statement type is unsupported.
func (c *Compiler) compileStmt(ctx context.Context, statement ast.Stmt) (program.VarLocation, error) {
	if c.statementDepth >= maxStatementDepth {
		return program.VarLocation{}, fmt.Errorf("%w: statement nesting exceeded %d at %s", fault.ErrCompileDepthLimit, maxStatementDepth, c.positionString(statement.Pos()))
	}
	c.statementDepth++
	defer func() { c.statementDepth-- }()

	c.setDebugPosition(ctx, statement.Pos())
	if location, handled, err := c.compileConcurrencyStmt(ctx, statement); handled {
		return location, err
	}
	switch s := statement.(type) {
	case *ast.ExprStmt:
		return c.compileExpression(ctx, s.X)
	case *ast.AssignStmt:
		return c.compileAssign(ctx, s)
	case *ast.DeclStmt:
		return c.compileDecl(ctx, s.Decl)
	case *ast.ReturnStmt:
		return c.compileReturn(ctx, s)
	case *ast.BlockStmt:
		c.Scopes.PushScope()
		location, err := c.compileStmtList(ctx, s.List)
		c.Scopes.PopScope()
		return location, err
	case *ast.IfStmt:
		return c.compileIf(ctx, s)
	case *ast.ForStmt:
		return c.compileFor(ctx, s)
	case *ast.IncDecStmt:
		return c.compileIncDec(ctx, s)
	case *ast.BranchStmt:
		return c.compileBranch(ctx, s)
	case *ast.SwitchStmt:
		return c.compileSwitch(ctx, s)
	case *ast.RangeStmt:
		return c.compileForRange(ctx, s)
	case *ast.EmptyStmt:
		return program.VarLocation{}, nil
	case *ast.LabeledStmt:
		return c.compileLabeledStmt(ctx, s)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported statement type: %T at %s", statement, c.positionString(statement.Pos()))
	}
}

// compileConcurrencyStmt compiles the goroutine, deferral, and channel statement kinds,
// keeping the main compileStmt dispatch small enough to stay within the
// cyclomatic-complexity budget.
//
// Takes statement (ast.Stmt) which is the statement node to compile.
//
// Returns the compiled statement's result location, a handled flag that is true only when
// statement was one of the concurrency-related kinds, and any compilation error.
func (c *Compiler) compileConcurrencyStmt(ctx context.Context, statement ast.Stmt) (location program.VarLocation, handled bool, err error) {
	switch s := statement.(type) {
	case *ast.DeferStmt:
		location, err = c.compileDefer(ctx, s)
		return location, true, err
	case *ast.GoStmt:
		location, err = c.compileGo(ctx, s)
		return location, true, err
	case *ast.SendStmt:
		location, err = c.compileSend(ctx, s)
		return location, true, err
	case *ast.TypeSwitchStmt:
		location, err = c.compileTypeSwitch(ctx, s)
		return location, true, err
	case *ast.SelectStmt:
		location, err = c.compileSelect(ctx, s)
		return location, true, err
	default:
		return program.VarLocation{}, false, nil
	}
}

// compileDecl compiles a declaration node, dispatching by concrete AST type.
//
// Takes declaration: the declaration node to compile by dispatching on its concrete AST
// type.
//
// Returns the location of the last compiled spec.
//
// Returns an error when the declaration type is unsupported.
func (c *Compiler) compileDecl(ctx context.Context, declaration ast.Decl) (program.VarLocation, error) {
	switch d := declaration.(type) {
	case *ast.GenDecl:
		return c.compileGenDecl(ctx, d)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported declaration type: %T at %s", declaration, c.positionString(declaration.Pos()))
	}
}

// compileMultiValueSpec lowers `var a, b = f()` through the short-variable-declaration
// path, which already splits tuple calls and the comma-ok forms across their targets; the
// names are declared objects either way.
//
// Takes spec (*ast.ValueSpec) which is the declaration.
//
// Returns the first declared location and any compilation error.
func (c *Compiler) compileMultiValueSpec(ctx context.Context, spec *ast.ValueSpec) (program.VarLocation, error) {
	leftHandSide := make([]ast.Expr, len(spec.Names))
	for i, name := range spec.Names {
		leftHandSide[i] = name
	}
	statement := &ast.AssignStmt{Lhs: leftHandSide, TokPos: spec.Pos(), Tok: token.DEFINE, Rhs: spec.Values}
	return c.compileShortVarDecl(ctx, statement)
}

// compileGenDecl compiles a general declaration (var, const, type).
//
// Type specs do not emit bytecode.
//
// Takes declaration: the general declaration node to compile.
//
// Returns the location of the last value spec emitted, or the zero VarLocation when no
// value specs were emitted.
//
// Returns the first compilation error encountered while emitting a value spec.
func (c *Compiler) compileGenDecl(ctx context.Context, declaration *ast.GenDecl) (program.VarLocation, error) {
	var lastLocation program.VarLocation
	for _, spec := range declaration.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			location, err := c.compileValueSpec(ctx, s)
			if err != nil {
				return program.VarLocation{}, err
			}
			lastLocation = location
		case *ast.TypeSpec:

			_ = s
		}
	}
	return lastLocation, nil
}

// compileValueSpec compiles a var or const declaration.
//
// Declares each name in the current scope and emits either its initialiser or the
// zero-value for its type.
//
// Takes spec: the value specification to compile.
//
// Returns the location of the first declared variable, or the zero VarLocation when no
// variables are declared.
//
// Returns the first compilation error encountered while emitting an initialiser.
func (c *Compiler) compileValueSpec(ctx context.Context, spec *ast.ValueSpec) (program.VarLocation, error) {
	if multiValueSpec(spec) {
		return c.compileMultiValueSpec(ctx, spec)
	}
	for i, name := range spec.Names {
		if err := c.compileValueSpecName(ctx, spec, i, name); err != nil {
			return program.VarLocation{}, err
		}
	}

	if len(spec.Names) > 0 {
		location, ok := c.Scopes.LookupVar(spec.Names[0].Name)
		if ok {
			return location, nil
		}
	}

	return program.VarLocation{}, nil
}

// compileValueSpecName declares and initialises a single name from a var or const
// declaration, emitting either the initialiser at index or the zero value for the name's
// type.
//
// Takes spec (*ast.ValueSpec) which is the value specification.
// Takes index (int) which is the position of name within spec.Names.
// Takes name (*ast.Ident) which is the identifier being declared.
//
// Returns the first compilation error encountered while emitting the initialiser, or nil
// when the name is blank or has no type info.
func (c *Compiler) compileValueSpecName(ctx context.Context, spec *ast.ValueSpec, index int, name *ast.Ident) error {
	if name.Name == typemap.BlankIdentName {
		return nil
	}

	typeObject := c.Info.Defs[name]
	if typeObject == nil {
		return nil
	}

	kind := c.kindFor(typeObject.Type())
	location := c.Scopes.DeclareVar(name.Name, kind)

	if index < len(spec.Values) {
		if err := c.emitValueSpecInitialiser(ctx, name.Name, spec.Values[index], typeObject.Type(), location); err != nil {
			return err
		}
	} else {
		c.emitLocalZeroValue(ctx, typeObject, location)
	}
	c.tryHeapPromoteCapturedLocal(ctx, name.Name, name)
	return nil
}

// emitValueSpecInitialiser compiles an initialiser expression and moves its value into
// the declared variable's location.
//
// Takes name (string) which is the declared variable name, used to place a heap-promoted
// local's make([]T, ...) backing on the Go heap.
// Takes value (ast.Expr) which is the initialiser expression.
// Takes declaredType (types.Type) which is the declared variable type.
// Takes location (VarLocation) which is the destination location.
//
// Returns the first compilation error encountered.
func (c *Compiler) emitValueSpecInitialiser(ctx context.Context, name string, value ast.Expr, declaredType types.Type, location program.VarLocation) error {
	valueLocation, handled, herr := c.compileTypedNilOrExpression(ctx, value, declaredType)
	if herr != nil {
		return herr
	}
	if !handled {
		var err error
		valueLocation, err = c.compileLocalInitialiser(ctx, name, value)
		if err != nil {
			return err
		}
	}
	valueLocation = c.coerceEvalBoolResult(ctx, c.Info, value, valueLocation)
	c.snapshotIndirectLocalRead(value, valueLocation)
	c.emitMoveTyped(ctx, location, valueLocation, c.staticTypeOf(value))
	return nil
}

// emitLocalZeroValue initialises a local variable to its zero value.
//
// Typed-bank locals use isa.SubOpLoadZero directly; general-bank locals attempt a
// named-type zero from the symbol registry, then a composite (array/struct) zero, then a
// generic reflect.Zero before falling back to isa.SubOpLoadZero.
//
// Takes typeObject: the type object describing the local being initialised.
// Takes location: the destination register location.
func (c *Compiler) emitLocalZeroValue(ctx context.Context, typeObject types.Object, location program.VarLocation) {
	if location.Kind != isa.RegisterGeneral {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), location.Register, uint8(location.Kind))
		return
	}
	if c.tryEmitNamedTypeZero(ctx, typeObject, location) {
		return
	}
	if c.tryEmitCompositeTypeZero(ctx, typeObject, location) {
		return
	}
	if c.tryEmitReflectZero(ctx, typeObject, location) {
		return
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadZero), location.Register, uint8(location.Kind))
}

// tryEmitNamedTypeZero loads a zero value for a named type.
//
// Consults the symbol registry for the registered zero value.
//
// Takes typeObject (types.Object) which names the type.
// Takes location (VarLocation) which is the destination register.
//
// Returns bool which is true when the load was emitted.
func (c *Compiler) tryEmitNamedTypeZero(ctx context.Context, typeObject types.Object, location program.VarLocation) bool {
	if c.symbols == nil {
		return false
	}
	zeroValue, ok := c.zeroValueForNamedType(ctx, typeObject.Type())
	if !ok {
		return false
	}
	named, isNamed := types.Unalias(typeObject.Type()).(*types.Named)
	if !isNamed {
		return false
	}
	constIndex, err := program.AddGeneralConstant(c.Function, zeroValue, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantNamedTypeZero,
		PackagePath: named.Obj().Pkg().Path(),
		SymbolName:  named.Obj().Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
	if err != nil {
		c.recordStickyError(err)
		return true
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, location.Register, constIndex)
	return true
}

// tryEmitCompositeTypeZero emits a composite zero constant load.
//
// Acts only when typeObject's underlying type is array or struct.
//
// Takes typeObject (types.Object) which names the type.
// Takes location (VarLocation) which is the destination register.
//
// Returns bool which is true when the load was emitted.
func (c *Compiler) tryEmitCompositeTypeZero(ctx context.Context, typeObject types.Object, location program.VarLocation) bool {
	zeroValue, ok := c.zeroValueForCompositeType(ctx, typeObject.Type())
	if !ok {
		return false
	}
	constIndex, err := program.AddGeneralConstant(c.Function, zeroValue, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero,
		TypeDescriptor: descriptor.ReflectTypeToDescriptor(c.TypeToReflect(ctx, typeObject.Type())), PackagePath: "", SymbolName: ""})
	if err != nil {
		c.recordStickyError(err)
		return true
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, location.Register, constIndex)
	return true
}

// tryEmitReflectZero emits a reflect.Zero general constant load.
//
// Acts only for non-interface types whose reflect.Type is resolvable.
//
// Takes typeObject (types.Object) which names the type.
// Takes location (VarLocation) which is the destination register.
//
// Returns bool which is true when the load was emitted.
func (c *Compiler) tryEmitReflectZero(ctx context.Context, typeObject types.Object, location program.VarLocation) bool {
	if _, isInterface := typeObject.Type().Underlying().(*types.Interface); isInterface {
		return false
	}
	reflectType := c.TypeToReflect(ctx, typeObject.Type())
	if reflectType == nil {
		return false
	}
	constIndex, err := program.AddGeneralConstant(c.Function, reflect.Zero(reflectType), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero,
		TypeDescriptor: descriptor.ReflectTypeToDescriptor(reflectType), PackagePath: "", SymbolName: ""})
	if err != nil {
		c.recordStickyError(err)
		return true
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, location.Register, constIndex)
	return true
}

// zeroValueForCompositeType returns an addressable zero reflect.Value for an array or
// struct type.
//
// Takes t: the type whose zero value is required.
//
// Returns the addressable zero reflect.Value and true when t's underlying type is an
// array or struct, and (reflect.Value{}, false) otherwise.
func (c *Compiler) zeroValueForCompositeType(ctx context.Context, t types.Type) (reflect.Value, bool) {
	reflectType := c.TypeToReflect(ctx, t)
	if reflectType == nil {
		return reflect.Value{}, false
	}
	switch t.Underlying().(type) {
	case *types.Array, *types.Struct:
		return reflect.New(reflectType).Elem(), true
	}
	return reflect.Value{}, false
}

// zeroValueForNamedType returns an addressable zero reflect.Value for a registered named
// type.
//
// Takes t: the type whose zero value is required.
//
// Returns the addressable zero reflect.Value and true when t is a named type registered
// in the symbol registry, and (reflect.Value{}, false) otherwise.
func (c *Compiler) zeroValueForNamedType(_ context.Context, t types.Type) (reflect.Value, bool) {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return reflect.Value{}, false
	}
	typeObject := named.Obj()
	if typeObject.Pkg() == nil {
		return reflect.Value{}, false
	}
	return c.symbols.ZeroValueForType(typeObject.Pkg().Path(), typeObject.Name())
}

// compileAssign compiles an assignment statement, dispatching to the appropriate
// specialised path.
//
// Recognised paths: short-variable, compound, multi-assignment, index-RMW rewrite, or
// generic single-pair.
//
// Takes statement: the assignment statement to compile.
//
// Returns the location of the final assignment target.
//
// Returns the first compilation error encountered.
func (c *Compiler) compileAssign(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	if statement.Tok == token.DEFINE {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringRoute, "define")
		return c.compileShortVarDecl(ctx, statement)
	}
	if isCompoundAssign(statement.Tok) {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringRoute, "compound", "tok", statement.Tok.String())
		return c.compileCompoundAssign(ctx, statement)
	}
	if len(statement.Lhs) > 1 {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringRoute, "multi")
		return c.compileMultiAssign(ctx, statement)
	}
	if rewritten, ok := c.tryRewriteIndexRMWAssign(statement); ok {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringRoute, "rmw-rewrite")
		return c.compileCompoundAssign(ctx, rewritten)
	}
	c.noteLoweringRefused(ctx, loweringTableAssign, loweringRoute, "single")
	return c.compileSingleAssignPairs(ctx, statement)
}

// compileSingleAssignPairs walks the LHS/RHS pairs of a non-compound, non-define
// assignment and emits each store.
//
// Each pair is first offered to the structural fast paths (struct-into-collection,
// star-append-byte) before falling back to the generic expression-then-store sequence.
//
// Takes statement: the assignment statement whose pairs are being emitted.
//
// Returns the location of the final emitted assignment, or the zero VarLocation when no
// pairs were processed.
//
// Returns the first compilation error encountered.
func (c *Compiler) compileSingleAssignPairs(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, error) {
	var lastLocation program.VarLocation
	for i, leftHandSide := range statement.Lhs {
		location, applied, err := c.tryAssignLowerings(ctx, leftHandSide, statement.Rhs[i])
		if err != nil {
			return program.VarLocation{}, err
		}
		if applied {
			lastLocation = location
			continue
		}
		lastLocation, err = c.compileAssignPair(ctx, leftHandSide, statement.Rhs[i])
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	return lastLocation, nil
}

// compileAssignPair compiles the RHS expression and emits the store into the supplied LHS
// target.
//
// Takes leftHandSide: the LHS expression receiving the value.
// Takes rightHandSide: the RHS expression producing the value.
//
// Returns the location holding the stored value.
//
// Returns the compilation error from the RHS expression or the store.
func (c *Compiler) compileAssignPair(ctx context.Context, leftHandSide, rightHandSide ast.Expr) (program.VarLocation, error) {
	var expectedType types.Type
	if c.Info != nil {
		if tv, ok := c.Info.Types[leftHandSide]; ok {
			expectedType = tv.Type
		}
	}
	if c.assignmentNeedsOrdering(leftHandSide, rightHandSide) {
		return c.compileOrderedAssignPair(ctx, leftHandSide, rightHandSide, expectedType)
	}
	valueLocation, handled, herr := c.compileTypedNilOrExpression(ctx, rightHandSide, expectedType)
	if herr != nil {
		return program.VarLocation{}, herr
	}
	if !handled {
		var err error
		valueLocation, err = c.compileAssignValue(ctx, leftHandSide, rightHandSide)
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	return c.emitAssignTarget(ctx, leftHandSide, valueLocation)
}

// compileOrderedAssignPair lowers `target = value` in Go's evaluation order: the target's
// operands first, then the value, then the store.
//
// Takes leftHandSide (ast.Expr) which is the target.
// Takes rightHandSide (ast.Expr) which is the value.
// Takes expectedType (types.Type) which is the target's static type for typed nils.
//
// Returns the stored location, or an error.
func (c *Compiler) compileOrderedAssignPair(ctx context.Context, leftHandSide, rightHandSide ast.Expr, expectedType types.Type) (program.VarLocation, error) {
	prepared, err := c.prepareAssignTarget(ctx, leftHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	valueLocation, handled, err := c.compileTypedNilOrExpression(ctx, rightHandSide, expectedType)
	if err != nil {
		return program.VarLocation{}, err
	}
	if !handled {
		valueLocation, err = c.compileExpression(ctx, rightHandSide)
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	return c.emitPreparedStore(ctx, prepared, valueLocation)
}

// compileAssignValue compiles the right-hand side of a single assignment: straight into
// the target's register when it is a direct scalar local, otherwise as a local
// initialiser for an identifier target (heap-placed make, cell-read snapshot) or a plain
// expression for container, field and pointer targets.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
// Takes rightHandSide (ast.Expr) which is the value expression.
//
// Returns the location holding the value and any compilation error.
func (c *Compiler) compileAssignValue(ctx context.Context, leftHandSide, rightHandSide ast.Expr) (program.VarLocation, error) {
	if dest, direct := c.directAssignDestination(leftHandSide); direct {
		return c.compileExpressionInto(ctx, rightHandSide, dest)
	}
	target, isIdent := leftHandSide.(*ast.Ident)
	if !isIdent {
		return c.compileExpression(ctx, rightHandSide)
	}
	valueLocation, err := c.compileLocalInitialiser(ctx, target.Name, rightHandSide)
	if err != nil {
		return program.VarLocation{}, err
	}
	c.snapshotIndirectLocalRead(rightHandSide, valueLocation)
	return valueLocation, nil
}

// emitAssignTarget stores valueLocation into the given LHS target.
//
// Dispatches on the AST node type (identifier, index, selector, or star expression).
//
// Takes leftHandSide: the LHS expression receiving the value.
// Takes valueLocation: the source location holding the value to store.
//
// Returns the resulting location of the store.
//
// Returns an error when the LHS target type is unsupported.
func (c *Compiler) emitAssignTarget(ctx context.Context, leftHandSide ast.Expr, valueLocation program.VarLocation) (program.VarLocation, error) {
	switch target := leftHandSide.(type) {
	case *ast.Ident:
		return c.emitIdentAssign(ctx, target, valueLocation)
	case *ast.IndexExpr:
		if err := c.compileIndexAssign(ctx, target, valueLocation); err != nil {
			return program.VarLocation{}, err
		}
		return valueLocation, nil
	case *ast.SelectorExpr:
		if err := c.compileSelectorAssign(ctx, target, valueLocation); err != nil {
			return program.VarLocation{}, err
		}
		return valueLocation, nil
	case *ast.StarExpr:
		if err := c.compileStarAssign(ctx, target, valueLocation); err != nil {
			return program.VarLocation{}, err
		}
		return valueLocation, nil
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported assignment target: %T at %s", leftHandSide, c.positionString(leftHandSide.Pos()))
	}
}

// emitIdentAssign stores valueLocation into an identifier target.
//
// Resolves the identifier against the upvalue map, the global variable table, and the
// lexical scope chain in that order. Blank identifiers are silently dropped.
//
// Takes target: the identifier receiving the value.
// Takes valueLocation: the source location holding the value to store.
//
// Returns the location of the resolved destination, or valueLocation when the target is
// blank.
//
// Returns an error when the identifier resolves to no known binding.
func (c *Compiler) emitIdentAssign(ctx context.Context, target *ast.Ident, valueLocation program.VarLocation) (program.VarLocation, error) {
	if target.Name == typemap.BlankIdentName {
		return valueLocation, nil
	}
	switch c.resolveIdentTarget(target) {
	case identTargetUpvalue:
		reference := c.upvalueMap[target.Name]
		coerced := c.coerceToKind(ctx, valueLocation, reference.kind)
		program.Emit(c.Function, isa.OpSetUpvalue, coerced.Register, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
		if coerced.Register != valueLocation.Register || coerced.Kind != valueLocation.Kind {
			c.Scopes.Alloc.FreeTemp(coerced.Kind, coerced.Register)
		}
		return valueLocation, nil
	case identTargetGlobal:
		c.emitSetGlobal(ctx, c.globalVariables[target.Name], valueLocation)
		return valueLocation, nil
	case identTargetLocal, identTargetUnknown:
	}
	destLocation, found := c.Scopes.LookupVar(target.Name)
	if !found {
		return program.VarLocation{}, fmt.Errorf("undefined variable: %s at %s", target.Name, c.positionString(target.Pos()))
	}
	var destType types.Type
	if c.Info != nil {
		if object := c.Info.ObjectOf(target); object != nil {
			destType = object.Type()
		}
	}
	c.emitMoveTyped(ctx, destLocation, valueLocation, destType)
	c.emitWriteSharedCellIfCaptured(ctx, destLocation)
	return destLocation, nil
}

// positionString formats pos as a "file:line:col" string for error messages.
//
// Takes pos: the source position to format.
//
// Returns the formatted position, or "<unknown>" when pos is invalid or the file set is
// nil.
func (c *Compiler) positionString(pos token.Pos) string {
	if !pos.IsValid() || c.FileSet == nil {
		return "<unknown>"
	}
	position := c.FileSet.Position(pos)
	return position.String()
}

// setDebugPosition records pos as the current source position for debug source mapping.
//
// No-op when debug info is disabled.
//
// Takes pos: the source position to record.
func (c *Compiler) setDebugPosition(_ context.Context, pos token.Pos) {
	c.currentPosition = pos
}

// initDebugInfo wires up the debug source map, file ID table, and Emit hook on the
// current function.
//
// The file table is the program context's debugFiles, shared by every sub-compiler; the
// first compiler that needs it allocates it. No-op when debug info is disabled.
func (c *Compiler) initDebugInfo(ctx context.Context) {
	if c.debugFiles == nil {
		c.debugFiles = new(make([]string, 0, initialFileTableCapacity))
	}
	files := c.debugFiles

	sm := &program.SourceMap{Files: files, Positions: nil, Epilogue: program.SourcePosition{Line: 0, Column: 0, FileID: 0, Inlined: false}}
	c.debugSourceMap = sm
	c.debugFileIDs = make(map[string]uint16)
	c.Function.DebugSourceMap = sm

	if c.debugEnabled {
		c.Function.DebugVarTable = &program.DebugVarTable{Entries: nil}
		c.Scopes.DebugVarTable = c.Function.DebugVarTable
		c.Scopes.DebugBodyLenFunc = func() int { return len(c.Function.Body) }
	}

	var lastPosition token.Pos
	var lastResolved program.SourcePosition
	c.Function.DebugEmitHook = func(pc int) {
		if !c.currentPosition.IsValid() {
			for len(sm.Positions) <= pc {
				sm.Positions = append(sm.Positions, program.SourcePosition{Line: 0, Column: 0, FileID: 0, Inlined: false})
			}
			return
		}
		if c.currentPosition != lastPosition {
			pos := c.FileSet.Position(c.currentPosition)
			lastResolved = program.SourcePosition{
				Line:    safeconv.IntToInt32(pos.Line),
				Column:  safeconv.IntToInt16(pos.Column),
				FileID:  c.resolveFileID(ctx, pos.Filename),
				Inlined: false,
			}
			lastPosition = c.currentPosition
		}

		for len(sm.Positions) <= pc {
			sm.Positions = append(sm.Positions, program.SourcePosition{Line: 0, Column: 0, FileID: 0, Inlined: false})
		}
		sm.Positions[pc] = lastResolved
	}
}

// resolveFileID returns the uint16 file ID for filename.
//
// Appends the filename to the source map's shared files slice when not already present.
//
// Takes filename: the source file name to resolve.
//
// Returns the uint16 file ID assigned to the filename.
func (c *Compiler) resolveFileID(_ context.Context, filename string) uint16 {
	if id, ok := c.debugFileIDs[filename]; ok {
		return id
	}
	id := safeconv.IntToUint16(len(*c.debugSourceMap.Files))
	*c.debugSourceMap.Files = append(*c.debugSourceMap.Files, filename)
	c.debugFileIDs[filename] = id
	return id
}

// resolveIdentTarget decides where an assignable identifier lives, following Go's scoping
// order.
//
// Takes ident (*ast.Ident) which is the identifier being written.
//
// Returns identTargetKind which selects the store the caller emits.
func (c *Compiler) resolveIdentTarget(ident *ast.Ident) identTargetKind {
	if c.Info != nil {
		if object := c.Info.ObjectOf(ident); object != nil && object.Pkg() != nil {
			return c.resolveTypedIdentTarget(ident.Name, object)
		}
	}
	return c.resolveUntypedIdentTarget(ident.Name)
}

// resolveTypedIdentTarget classifies an identifier from its go/types object: a
// package-scope object is a package variable (when registered), anything else is the
// innermost local, then a captured upvalue.
//
// Takes name (string) which is the identifier.
// Takes object (types.Object) which is the identifier's resolved object.
//
// Returns identTargetKind which is where the identifier lives.
func (c *Compiler) resolveTypedIdentTarget(name string, object types.Object) identTargetKind {
	if object.Parent() == object.Pkg().Scope() {
		if _, ok := c.globalVariables[name]; ok {
			return identTargetGlobal
		}
		return identTargetUnknown
	}
	if _, ok := c.Scopes.LookupVar(name); ok {
		return identTargetLocal
	}
	if _, ok := c.upvalueMap[name]; ok {
		return identTargetUpvalue
	}
	return identTargetUnknown
}

// resolveUntypedIdentTarget classifies an identifier without type information, in the
// order the read path uses: local, upvalue, package variable.
//
// Takes name (string) which is the identifier.
//
// Returns identTargetKind which is where the identifier lives.
func (c *Compiler) resolveUntypedIdentTarget(name string) identTargetKind {
	if _, ok := c.Scopes.LookupVar(name); ok {
		return identTargetLocal
	}
	if _, ok := c.upvalueMap[name]; ok {
		return identTargetUpvalue
	}
	if _, ok := c.globalVariables[name]; ok {
		return identTargetGlobal
	}
	return identTargetUnknown
}

// markCallPosition moves the debug position to the opening parenthesis of the call
// expression being compiled, so the call instruction emitted next carries the line Go
// reports for it rather than the line of its last argument.
func (c *Compiler) markCallPosition() {
	if c.callParen.IsValid() {
		c.currentPosition = c.callParen
	}
}

// identTargetKind says where an assignable identifier lives.
type identTargetKind uint8

// rangeOverFunctionContext holds state for compiling the yield callback body of a
// range-over-func loop. It transforms break/continue/return into yield return values and
// state flag mutations.
type rangeOverFunctionContext struct {
	// returnStashUpvalueIndices are the upvalue indices for stashing return values when a
	// return statement is encountered inside the range-over-func body.
	returnStashUpvalueIndices []int

	// returnKinds are the register kinds of the enclosing function's return values, used to
	// Emit the correct opGetUpvalue after the iterator call.
	returnKinds []isa.RegisterKind

	// outerLabels are labelled break/continue targets from enclosing loops. Enables
	// cross-closure labelled break/continue by encoding the target as a state flag value.
	outerLabels []outerLabelTarget

	// stateFlagUpvalueIndex is the upvalue index for the state flag register in the yield
	// closure. 0=normal, 1=break, 2=return-pending, 3+=labelled break/continue to outer
	// loops.
	stateFlagUpvalueIndex int
}

// outerLabelTarget describes a labelled loop in an enclosing scope that can be targeted
// by break/continue from within a range-over-func yield body.
type outerLabelTarget struct {
	// label is the name of the labelled loop target in the enclosing scope.
	label string

	// breakFlag is the state flag value for labelled break.
	breakFlag int64

	// continueFlag is the state flag value for labelled continue (0 if not a loop).
	continueFlag int64

	// breakableIndex is the index into the outer Compiler's breakables slice.
	breakableIndex int
}

// upvalueReference tracks a captured variable's upvalue index and kind within a closure
// being compiled.
type upvalueReference struct {
	// index is the upvalue slot index within the closure's upvalue table.
	index int

	// kind is the register kind the closure body sees the upvalue as. Equals originalKind
	// for heap-promoted captures.
	kind isa.RegisterKind

	// isIndirect is true when the captured variable is heap-promoted.
	isIndirect bool

	// originalKind preserves the typed bank the variable had pre-promotion, meaningful only
	// when isIndirect is true.
	originalKind isa.RegisterKind
}

// breakableContext tracks pending break and continue jumps for a loop or switch
// statement.
//
//exhaustruct:ignore
type breakableContext struct {
	// label is the optional label name for labelled break/continue.
	label string

	// breakJumps holds instruction offsets of break jumps to patch.
	breakJumps []int

	// continueJumps holds instruction offsets of continue jumps to patch. Only meaningful
	// for loops.
	continueJumps []int

	// fallthroughJumps holds instruction offsets of fallthrough jumps to patch. Only
	// meaningful for switch statements.
	fallthroughJumps []int

	// isLoop is true for for-loops, false for switch statements.
	isLoop bool
}

// copyFunctionTable merges already-resolved entries into the compiler's function table.
//
// Used to seed a package compiler with cross-package method indices discovered earlier in
// the compilation order.
//
// Takes c (*Compiler) whose table receives the entries.
// Takes entries (map[string]uint16) which are merged in; existing keys are overwritten.
func copyFunctionTable(c *Compiler, entries map[string]uint16) {
	maps.Copy(c.functionTable, entries)
}

// CompileEvalExpression compiles a single expression as the body of a synthetic "<eval>"
// function.
//
// The expression result is moved into register 0 of its bank and the function's
// resultKinds records that bank for the runtime to extract.
//
// Takes config (CompilerConfig) which supplies the file set, type information, symbols,
// features, limits and optimisation selection; its Function and RootFunction are replaced
// by the synthetic eval function.
// Takes expression (ast.Expr) which is the expression to compile.
//
// Returns the compiled synthetic function on success.
//
// Returns the first compilation error encountered.
func CompileEvalExpression(ctx context.Context, config CompilerConfig, expression ast.Expr) (*program.CompiledFunction, error) {
	evalFunction := &program.CompiledFunction{Name: "<eval>"}
	config.Function = evalFunction
	config.RootFunction = evalFunction
	c := newFunctionCompiler(ctx, newProgramContext(config), evalFunction, functionOptions{
		scopeName:         "<eval>",
		upvalues:          nil,
		substitutions:     nil,
		substitutionCache: nil,
		rangeOverFunction: nil,
	})
	c.Scopes.PushScope()

	location, err := c.compileExpression(ctx, expression)
	if err != nil {
		return nil, fmt.Errorf("compiling eval expression: %w", err)
	}

	location = c.coerceEvalBoolResult(ctx, config.Info, expression, location)

	c.emitMoveToRegisterZero(ctx, location)

	c.Function.ResultKinds = []isa.RegisterKind{location.Kind}
	c.Function.ResultReflectTypes = []reflect.Type{location.SourceType}
	if err := c.resourceError(); err != nil {
		return nil, fmt.Errorf("compiling eval expression: %w", err)
	}
	c.Function.NumRegisters = c.Scopes.PeakRegisters()
	if err := passes.OptimiseFunction(ctx, c.passOptions, c.Function); err != nil {
		return nil, fmt.Errorf("compiling eval expression: %w", err)
	}
	c.Scopes.PopScope()

	return c.Function, nil
}

// classifyTypedSliceParameters picks typed-slice-bank parameters.
//
// Mirrors the local-side classifyTypedSliceLocals analysis but seeds the candidate set
// from the parameter list (instead of make-allocation sites). Parameters whose static
// type maps to a typed-slice bank via kindForCallSlot AND whose body usage doesn't
// trigger any typed-bank disqualifier
// (append/copy/address-of/closure-capture/type-assertion/etc.) survive. Survivors stay on
// the typed bank; the rest fall back to general bank for ABI safety.
//
// Takes typeContext (*Compiler) which provides go/types info and access to the function
// table (FunctionTable) for the call-site permissive rule.
// Takes body (*ast.BlockStmt) which is walked for typed-slice survivors. The parameter
// list comes from signature rather than the AST.
// Takes signature (*types.Signature) which carries parameter types.
//
// Returns map[string]isa.RegisterKind which is the surviving parameter name to
// typed-slice kind map, or nil when declaration has no body, no parameters, or no
// typed-slice-eligible parameters.
func classifyTypedSliceParameters(typeContext *Compiler, body *ast.BlockStmt, signature *types.Signature) map[string]isa.RegisterKind {
	if body == nil || signature == nil {
		return nil
	}
	parameterCount := signature.Params().Len()
	candidates := map[string]isa.RegisterKind{}
	parameterIndex := 0
	for p := range signature.Params().Variables() {
		isVariadicLast := signature.Variadic() && parameterIndex == parameterCount-1
		parameterIndex++
		if isVariadicLast {
			continue
		}
		if typemap.IsTypeParameter(p.Type()) || typemap.ContainsTypeParameter(p.Type()) {
			continue
		}
		kind := typemap.KindForCallSlot(typeContext.substitutedType(p.Type()))
		if !isa.IsTypedSliceKind(kind) {
			continue
		}
		if p.Name() == "" || p.Name() == "_" {
			continue
		}
		candidates[p.Name()] = kind
	}
	if len(candidates) == 0 {
		return nil
	}
	return escape.ClassifyTypedSliceParamNames(typeContext.EscapeContext(), body, candidates)
}

// collectHeapPromotedParamNames returns the names of parameters the escape analysis
// promoted to the heap.
//
// Takes typeContext (*Compiler) which carries the escape context.
// Takes functionType (*ast.FuncType) which supplies the parameter list.
// Takes body (*ast.BlockStmt) which is walked for heap promotions.
//
// Returns the promoted parameter names, empty when the function has no body.
func collectHeapPromotedParamNames(typeContext *Compiler, functionType *ast.FuncType, body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return map[string]bool{}
	}
	promoted := escape.CollectHeapPromotedNames(typeContext.EscapeContext(), body)
	if promoted == nil {
		return map[string]bool{}
	}
	parameterNames := map[string]bool{}
	if functionType != nil && functionType.Params != nil {
		for _, field := range functionType.Params.List {
			for _, name := range field.Names {
				parameterNames[name.Name] = true
			}
		}
	}
	result := map[string]bool{}
	for name := range promoted {
		if parameterNames[name] {
			result[name] = true
		}
	}
	return result
}

// isCompoundAssign reports whether operatorToken is one of Go's compound assignment
// operators.
//
// The recognised set is +=, -=, *=, /=, %=, &=, |=, ^=, &^=, <<=, >>=.
//
// Takes operatorToken: the token to classify.
//
// Returns true when operatorToken is a compound assignment operator; false otherwise.
func isCompoundAssign(operatorToken token.Token) bool {
	switch operatorToken {
	case token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN,
		token.QUO_ASSIGN, token.REM_ASSIGN,
		token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN,
		token.AND_NOT_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN:
		return true
	default:
	}
	return false
}

// compoundToOp maps a compound assignment token to its plain binary operator.
//
// For example, ADD_ASSIGN maps to ADD.
//
// Takes operatorToken: the token to map.
//
// Returns the matching binary operator token, or operatorToken unchanged when it is not a
// compound assignment.
func compoundToOp(operatorToken token.Token) token.Token {
	switch operatorToken {
	case token.ADD_ASSIGN:
		return token.ADD
	case token.SUB_ASSIGN:
		return token.SUB
	case token.MUL_ASSIGN:
		return token.MUL
	case token.QUO_ASSIGN:
		return token.QUO
	case token.REM_ASSIGN:
		return token.REM
	case token.AND_ASSIGN:
		return token.AND
	case token.OR_ASSIGN:
		return token.OR
	case token.XOR_ASSIGN:
		return token.XOR
	case token.AND_NOT_ASSIGN:
		return token.AND_NOT
	case token.SHL_ASSIGN:
		return token.SHL
	case token.SHR_ASSIGN:
		return token.SHR
	default:
		return operatorToken
	}
}
