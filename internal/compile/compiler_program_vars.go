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
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// errInitOrderSpecMissing reports an initialiser in types.Info.InitOrder whose value spec
// is not among the files being compiled, which happens in session mode when the type
// checker carries entries from earlier submissions.
var errInitOrderSpecMissing = errors.New("compiling variable init specs: no value spec")

// RegisterPackageLevelVarsFromFiles scans all files for package-level var declarations
// and registers them in the Compiler.
//
// Takes files ([]*ast.File) which are the parsed AST files to scan.
func (c *Compiler) RegisterPackageLevelVarsFromFiles(ctx context.Context, files []*ast.File) {
	for _, file := range files {
		c.markAddressTakenPackageVars(file.Decls)
	}
	for _, file := range files {
		c.registerPackageLevelVarsFromDecls(ctx, file.Decls)
	}
}

// RegisterPackageLevelVarsFromDecls scans declarations for package- level var specs and
// registers them in the Compiler.
//
// Takes decls ([]ast.Decl) which are the AST declarations to scan.
func (c *Compiler) RegisterPackageLevelVarsFromDecls(ctx context.Context, decls []ast.Decl) {
	c.markAddressTakenPackageVars(decls)
	c.registerPackageLevelVarsFromDecls(ctx, decls)
}

// TwoPassCompileFuncs performs a two-pass compilation: first register all function
// declarations across files so cross-file references resolve, then compile all function
// bodies.
//
// Takes files ([]*ast.File) which are the parsed AST files to compile.
// Takes packageLabel (string) which is included in error messages for multi-package
// compilations and is empty for single-package ones.
//
// Returns error when registration or body compilation fails.
func (c *Compiler) TwoPassCompileFuncs(ctx context.Context, files []*ast.File, packageLabel string) error {
	c.runtimePackageName = runtimePackageName(files, packageLabel)
	var entries []functionDeclEntry
	for _, file := range files {
		for _, declaration := range file.Decls {
			functionDeclaration, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			compiledFunction, err := c.registerFunctionDecl(ctx, functionDeclaration)
			if err != nil {
				return c.wrapCompileError(ctx, err, packageLabel)
			}
			entries = append(entries, functionDeclEntry{declaration: functionDeclaration, compiledFunction: compiledFunction})
		}
	}

	for _, entry := range entries {
		if err := c.compileFunctionBody(ctx, entry.declaration, entry.compiledFunction); err != nil {
			if isGenericTemplate(entry.declaration, entry.compiledFunction) {
				stubUnspecialisedGeneric(entry.compiledFunction, err)
				continue
			}
			return c.wrapCompileError(ctx, err, packageLabel)
		}
	}
	return nil
}

// CompileVariableInitFunction compiles package-level variable initialisers from all files
// into a dedicated function.
//
// Takes files ([]*ast.File) which are the parsed AST files with variable declarations.
//
// Returns *CompiledFunction which is the initialiser function, or nil when no global
// variables exist.
// Returns error when compilation of any initialiser fails.
func (c *Compiler) CompileVariableInitFunction(ctx context.Context, files []*ast.File) (*program.CompiledFunction, error) {
	if len(c.globalVariables) == 0 {
		return nil, nil
	}

	initFunction := &program.CompiledFunction{Name: "<varinit>"}
	savedFunction := c.Function
	c.Function = initFunction
	c.Scopes.PushScope()

	err := c.compileVarInitSpecs(ctx, files)

	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	if overflowErr := c.resourceError(); overflowErr != nil {
		err = overflowErr
	}
	initFunction.NumRegisters = c.Scopes.PeakRegisters()
	c.Scopes.PopScope()
	c.Function = savedFunction

	if err != nil {
		return nil, fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
	}
	return initFunction, nil
}

// registerPackageLevelVarsFromDecls registers the var specs among decls; the
// address-taken pre-pass must already have run over every declaration that can name them,
// since it decides which variables get a pointer cell.
//
// Takes decls ([]ast.Decl) which are scanned for var declarations.
func (c *Compiler) registerPackageLevelVarsFromDecls(ctx context.Context, decls []ast.Decl) {
	for _, declaration := range decls {
		genDecl, ok := declaration.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.VAR {
			continue
		}
		for _, spec := range genDecl.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok {
				c.registerPackageLevelVar(ctx, vs)
			}
		}
	}
}

// wrapCompileError wraps an error with fault.ErrCompilation and an optional package label
// for multi-package compilations.
//
// Takes err (error) which is the original compilation error.
// Takes packageLabel (string) which is the package label for error context, or empty for
// single-package compilations.
//
// Returns error wrapped with fault.ErrCompilation and the package label.
func (*Compiler) wrapCompileError(_ context.Context, err error, packageLabel string) error {
	if packageLabel != "" {
		return fmt.Errorf(fault.ErrChainMessageFmt, fault.ErrCompilation, packageLabel, err)
	}
	return fmt.Errorf(fault.ErrChainFmt, fault.ErrCompilation, err)
}

// compileVarInitSpecs compiles each package-level variable initialiser in Go's required
// dependency order.
//
// Go's spec mandates that package vars be initialised so each initialiser sees the
// fully-initialised values of every var it depends on, regardless of file order. go/types
// computes the correct order during type-checking and exposes it as types.Info.InitOrder;
// we use it directly here.
//
// When InitOrder is unavailable (eval mode, custom info object, ...) we fall back to
// file-declaration order, which resolves simple cases correctly; elaborate dependency
// orderings require the InitOrder path.
//
// Takes files ([]*ast.File) which are the parsed AST files. The fallback path walks
// these; the InitOrder path indexes into them by position.
//
// Returns error when any variable initialiser fails to
func (c *Compiler) compileVarInitSpecs(ctx context.Context, files []*ast.File) error {
	c.emitPackageVarPrologue(ctx, files)
	if c.Info != nil && len(c.Info.InitOrder) > 0 {
		err := c.compileVarInitFromInitOrder(ctx, files)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errInitOrderSpecMissing) {
			return err
		}
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			watermark := c.Scopes.Alloc.Snapshot()
			if err := c.compileVarDeclInit(ctx, declaration); err != nil {
				return fmt.Errorf("compiling variable init specs: %w", err)
			}
			c.Scopes.RestoreWatermark(watermark)
		}
	}
	return nil
}

// compileVarInitFromInitOrder walks types.Info.InitOrder and emits each initialiser by
// locating its source ast.ValueSpec via the first LHS variable's position.
//
// Takes files ([]*ast.File) which provide the value specs.
//
// Returns error when any initialiser fails to compile or when a position is missing from
// the index.
func (c *Compiler) compileVarInitFromInitOrder(ctx context.Context, files []*ast.File) error {
	specsByPos := indexValueSpecsByPosition(files)
	for _, initialiser := range c.Info.InitOrder {
		if len(initialiser.Lhs) == 0 {
			continue
		}
		spec, ok := specsByPos[initialiser.Lhs[0].Pos()]
		if !ok {
			return fmt.Errorf("%w: %s", errInitOrderSpecMissing, initialiser.Lhs[0].Name())
		}
		watermark := c.Scopes.Alloc.Snapshot()
		if err := c.compilePackageLevelVarInit(ctx, initialiserSpec(spec, initialiser)); err != nil {
			return fmt.Errorf("compiling variable init specs: %w", err)
		}
		c.Scopes.RestoreWatermark(watermark)
	}
	return nil
}

// compileVarDeclInit compiles variable initialisers from a single declaration. Non-var
// declarations are silently skipped.
//
// Takes declaration (ast.Decl) which is the AST declaration to compile.
//
// Returns error when compilation of any variable initialiser fails.
func (c *Compiler) compileVarDeclInit(ctx context.Context, declaration ast.Decl) error {
	genDecl, ok := declaration.(*ast.GenDecl)
	if !ok || genDecl.Tok != token.VAR {
		return nil
	}
	for _, spec := range genDecl.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if err := c.compilePackageLevelVarInit(ctx, vs); err != nil {
			return fmt.Errorf("compiling variable declaration: %w", err)
		}
	}
	return nil
}

// isGenericTemplate reports whether compiledFunction is the uninstantiated body of a
// generic function or method, compiled with its type parameters unresolved.
//
// Takes declaration (*ast.FuncDecl) which carries the receiver's type expression.
// Takes compiledFunction (*CompiledFunction) which is the function.
//
// Returns true for generic templates.
func isGenericTemplate(declaration *ast.FuncDecl, compiledFunction *program.CompiledFunction) bool {
	if compiledFunction.GenericTypeParams != nil && compiledFunction.GenericTypeParams.Len() > 0 {
		return true
	}
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return false
	}

	receiverType := declaration.Recv.List[0].Type
	if star, ok := receiverType.(*ast.StarExpr); ok {
		receiverType = star.X
	}
	switch receiverType.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	default:
		return false
	}
}

// stubUnspecialisedGeneric replaces a generic template's body with a panic naming the
// cause.
//
// Fires when the template did not compile (a make, a literal or an operator that needs
// the concrete type). Calls through specialisations compile the body again with concrete
// types, so the template only runs when a call could not be specialised.
//
// Takes compiledFunction (*CompiledFunction) which is the template.
// Takes cause (error) which is the template's compile error.
func stubUnspecialisedGeneric(compiledFunction *program.CompiledFunction, cause error) {
	compiledFunction.Body = compiledFunction.Body[:0]
	if sourceMap := compiledFunction.DebugSourceMap; sourceMap != nil {
		sourceMap.Positions = sourceMap.Positions[:0]
	}
	message, err := program.AddStringConstant(compiledFunction, fmt.Sprintf("generic function %s cannot run without specialisation: %v", compiledFunction.Name, cause))
	if err != nil {
		return
	}
	compiledFunction.NumRegisters[isa.RegisterString] = max(compiledFunction.NumRegisters[isa.RegisterString], 1)
	compiledFunction.NumRegisters[isa.RegisterGeneral] = max(compiledFunction.NumRegisters[isa.RegisterGeneral], 1)
	program.EmitWide(compiledFunction, isa.OpLoadStringConst, 0, message)
	program.Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpMoveStringToGeneral), 0, 0)
	program.Emit(compiledFunction, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Panic), 0)
	syncStubSourceMap(compiledFunction)
}

// syncStubSourceMap pads or trims the debug source map so its length matches the body.
//
// Takes compiledFunction (*CompiledFunction) which is the template to synchronise.
func syncStubSourceMap(compiledFunction *program.CompiledFunction) {
	sourceMap := compiledFunction.DebugSourceMap
	if sourceMap == nil {
		return
	}
	for len(sourceMap.Positions) < len(compiledFunction.Body) {
		sourceMap.Positions = append(sourceMap.Positions, program.SourcePosition{Line: 0, Column: 0, FileID: 0, Inlined: false})
	}
	sourceMap.Positions = sourceMap.Positions[:len(compiledFunction.Body)]
}

// initialiserSpec returns the value spec to compile for one InitOrder entry.
//
// A `var x, y, z = f(), g(), h()` spec yields one entry per variable; compiling the whole
// spec for each entry would run every initialiser once per variable, so such an entry
// gets a single-name spec built from its own expression.
//
// Takes spec (*ast.ValueSpec) which declares the entry's variables.
// Takes initialiser (*types.Initializer) which is the InitOrder entry.
//
// Returns *ast.ValueSpec which is the spec to compile for this entry.
func initialiserSpec(spec *ast.ValueSpec, initialiser *types.Initializer) *ast.ValueSpec {
	if len(initialiser.Lhs) != 1 || len(spec.Names) <= 1 || len(spec.Values) != len(spec.Names) {
		return spec
	}
	for i, name := range spec.Names {
		if name.Pos() != initialiser.Lhs[0].Pos() {
			continue
		}
		return &ast.ValueSpec{Doc: spec.Doc, Names: []*ast.Ident{name}, Type: spec.Type, Values: []ast.Expr{spec.Values[i]}, Comment: spec.Comment}
	}
	return spec
}

// runtimePackageName picks the qualifier runtime function names carry: Go reports the
// import path for a library package and the bare name for main, and a package compiled
// without a path (a single file set) has only its name.
//
// Takes files ([]*ast.File) which are the package's files.
// Takes packageLabel (string) which is the import path, empty when unknown.
//
// Returns string which is the qualifier.
func runtimePackageName(files []*ast.File, packageLabel string) string {
	name := "main"
	if len(files) > 0 && files[0].Name != nil {
		name = files[0].Name.Name
	}
	if name == "main" || packageLabel == "" {
		return name
	}
	return packageLabel
}
