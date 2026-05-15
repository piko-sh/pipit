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
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// compileTypeSwitch compiles a type switch statement.
//
// Takes statement (*ast.TypeSwitchStmt) which is the type switch statement AST node to
// compile.
//
// Returns the compiled location and any error encountered.
func (c *Compiler) compileTypeSwitch(ctx context.Context, statement *ast.TypeSwitchStmt) (program.VarLocation, error) {
	c.Scopes.PushScope()
	defer c.Scopes.PopScope()

	if statement.Init != nil {
		if _, err := c.compileStmt(ctx, statement.Init); err != nil {
			return program.VarLocation{}, err
		}
	}

	sourceLocation, assignName, err := c.compileTypeSwitchAssign(ctx, statement.Assign)
	if err != nil {
		return program.VarLocation{}, err
	}

	c.breakables = append(c.breakables, breakableContext{
		isLoop: false,
		label:  c.consumePendingLabel(ctx),
	})

	cases, defaultCase, err := c.collectSwitchCases(ctx, statement.Body)
	if err != nil {
		return program.VarLocation{}, err
	}

	tabled := false
	if caseTypes, eligible := c.typeSwitchJumpTableTypes(cases); eligible {
		tabled, err = c.compileTypeSwitchJumpTable(ctx, cases, caseTypes, defaultCase, sourceLocation, assignName)
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	if !tabled {
		if err := c.compileTypeSwitchChain(ctx, cases, defaultCase, sourceLocation, assignName); err != nil {
			return program.VarLocation{}, err
		}
	}
	breakable := &c.breakables[len(c.breakables)-1]
	for _, pc := range breakable.breakJumps {
		program.PatchJump(c.Function, pc)
	}
	c.breakables = c.breakables[:len(c.breakables)-1]

	return program.VarLocation{}, nil
}

// compileTypeSwitchChain compiles a type switch as a chain of per-clause type-assertion
// probes, each falling through to the next on a miss.
//
// Takes cases ([]*ast.CaseClause) which are the non-default clauses.
// Takes defaultCase (*ast.CaseClause) which is the default clause, or nil.
// Takes sourceLocation (VarLocation) which holds the switched value.
// Takes assignName (string) which is the bound variable name, or empty.
//
// Returns any error encountered during compilation.
func (c *Compiler) compileTypeSwitchChain(ctx context.Context,
	cases []*ast.CaseClause,
	defaultCase *ast.CaseClause,
	sourceLocation program.VarLocation,
	assignName string,
) error {
	var endJumps []int
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	for _, cc := range cases {
		endJump, err := c.compileTypeSwitchCase(ctx, cc, sourceLocation, assignName, okRegister)
		if err != nil {
			return err
		}
		endJumps = append(endJumps, endJump)
	}

	if defaultCase != nil {
		if err := c.compileTypeSwitchDefault(ctx, defaultCase, sourceLocation, assignName); err != nil {
			return err
		}
	}

	for _, pc := range endJumps {
		program.PatchJump(c.Function, pc)
	}
	return nil
}

// compileTypeSwitchAssign compiles the assign portion of a type switch.
//
// Takes assign (ast.Stmt) which is the assignment or expression statement from the type
// switch header.
//
// Returns the source location, the assignment name (empty if none), and any error.
func (c *Compiler) compileTypeSwitchAssign(ctx context.Context, assign ast.Stmt) (program.VarLocation, string, error) {
	var sourceLocation program.VarLocation
	var assignName string
	var err error

	switch a := assign.(type) {
	case *ast.AssignStmt:
		if identifier, ok := a.Lhs[0].(*ast.Ident); ok {
			assignName = identifier.Name
		}
		typeAssert, ok := a.Rhs[0].(*ast.TypeAssertExpr)
		if !ok {
			return program.VarLocation{}, "", fault.ErrCompileTypeSwitchAssignNotTypeAssert
		}
		sourceLocation, err = c.compileExpression(ctx, typeAssert.X)
	case *ast.ExprStmt:
		typeAssert, ok := a.X.(*ast.TypeAssertExpr)
		if !ok {
			return program.VarLocation{}, "", fault.ErrCompileTypeSwitchExprNotTypeAssert
		}
		sourceLocation, err = c.compileExpression(ctx, typeAssert.X)
	}
	if err != nil {
		return program.VarLocation{}, "", err
	}

	c.boxToGeneral(ctx, &sourceLocation)
	return sourceLocation, assignName, nil
}

// compileTypeSwitchCase compiles a single non-default case clause in a type switch.
//
// Takes cc (*ast.CaseClause) which is the case clause AST node to compile.
// Takes sourceLocation (VarLocation) which is the location of the source value being
// switched on.
// Takes assignName (string) which is the variable name for the narrowed type, or empty if
// none.
// Takes okRegister (uint8) which is the register to use for the type assertion ok flag.
//
// Returns the end-of-case jump offset and any error encountered.
func (c *Compiler) compileTypeSwitchCase(ctx context.Context,
	cc *ast.CaseClause,
	sourceLocation program.VarLocation,
	assignName string,
	okRegister uint8,
) (int, error) {
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLoadBool), okRegister, 0)
	destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)

	for _, typeExpr := range cc.List {
		tv := c.Info.Types[typeExpr]

		caseType := c.substitutedType(tv.Type)
		var reflectType reflect.Type
		if basic, ok := caseType.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
			reflectType = nil
		} else {
			reflectType = c.typeAssertReflectType(ctx, caseType)
		}
		methodNames := interfaceTargetMethodNames(c.substitutedType(caseType))
		typeIndex, err := program.AddTypeRefWithMethods(c.Function, reflectType, methodNames)
		if err != nil {
			return 0, err
		}

		temporaryOk := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.Emit(c.Function, isa.OpTypeAssert, destinationRegister, sourceLocation.Register, temporaryOk)
		program.EmitExtension(c.Function, typeIndex, engine.TypeAssertModeTypeSwitch)
		program.Emit(c.Function, isa.OpBitOr, okRegister, okRegister, temporaryOk)
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt, temporaryOk)
	}

	nextCaseJump := program.EmitJump(c.Function, isa.OpJumpIfFalse, okRegister)
	endJump, err := c.compileTypeSwitchCaseBody(ctx, cc, destinationRegister, assignName)
	if err != nil {
		return 0, err
	}
	program.PatchJump(c.Function, nextCaseJump)
	return endJump, nil
}

// compileTypeSwitchCaseBody compiles the body of a matched type-switch clause in its own
// scope, binding the narrowed variable first, and emits the jump to the end of the
// switch.
//
// Takes cc (*ast.CaseClause) which is the clause whose body is compiled.
// Takes destinationRegister (uint8) which holds the narrowed value.
// Takes assignName (string) which is the bound variable name, or empty.
//
// Returns the end-of-case jump offset for later patching and any error encountered.
func (c *Compiler) compileTypeSwitchCaseBody(ctx context.Context, cc *ast.CaseClause, destinationRegister uint8, assignName string) (int, error) {
	c.Scopes.PushScope()
	if assignName != "" {
		c.declareNarrowedTypeSwitchVar(ctx, assignName, cc.List, destinationRegister)
	}
	for _, bodyStmt := range cc.Body {
		if _, err := c.compileStmt(ctx, bodyStmt); err != nil {
			c.Scopes.PopScope()
			return 0, err
		}
	}
	c.Scopes.PopScope()
	return program.EmitTier1Jump(c.Function), nil
}

// declareNarrowedTypeSwitchVar declares the type-switched variable with a narrowed kind
// so handlers downstream of the case clause can read it from the appropriate register
// bank rather than the general bank.
//
// Takes assignName (string) which is the variable name to declare.
// Takes typeList ([]ast.Expr) which is the type expressions for the case clause.
// Takes destinationRegister (uint8) which is the register holding the type-asserted
// value.
func (c *Compiler) declareNarrowedTypeSwitchVar(ctx context.Context, assignName string, typeList []ast.Expr, destinationRegister uint8) {
	var narrowedKind isa.RegisterKind
	var narrowedType types.Type
	if len(typeList) == 1 {
		tv := c.Info.Types[typeList[0]]
		narrowedType = tv.Type
		narrowedKind = c.kindFor(tv.Type)
	} else {
		narrowedKind = isa.RegisterGeneral
	}
	location := c.Scopes.DeclareVar(assignName, narrowedKind)
	if location.IsSpilled {
		if narrowedKind == isa.RegisterGeneral {
			c.emitSpillStore(ctx, destinationRegister, isa.RegisterGeneral, location.SpillSlot)
		} else {
			scratch := c.Scopes.Alloc.AllocTemp(narrowedKind)
			program.Emit(c.Function, isa.OpUnpackInterface, scratch, destinationRegister, uint8(narrowedKind))
			c.emitSpillStore(ctx, scratch, narrowedKind, location.SpillSlot)
			c.Scopes.Alloc.FreeTemp(narrowedKind, scratch)
		}
	} else if narrowedKind == isa.RegisterGeneral {
		program.Emit(c.Function, isa.OpMoveGeneral, location.Register, destinationRegister, generalMoveModeFor(narrowedType))
	} else {
		program.Emit(c.Function, isa.OpUnpackInterface, location.Register, destinationRegister, uint8(narrowedKind))
	}
}

// compileTypeSwitchDefault compiles the default case of a type switch statement.
//
// Takes defaultCase (*ast.CaseClause) which is the default case clause AST node.
// Takes sourceLocation (VarLocation) which is the location of the source value being
// switched on.
// Takes assignName (string) which is the variable name for the default case, or empty if
// none.
//
// Returns any error encountered during compilation.
func (c *Compiler) compileTypeSwitchDefault(ctx context.Context,
	defaultCase *ast.CaseClause,
	sourceLocation program.VarLocation,
	assignName string,
) error {
	c.Scopes.PushScope()
	if assignName != "" {
		location := c.Scopes.DeclareVar(assignName, sourceLocation.Kind)
		c.emitMove(ctx, location, sourceLocation)
	}
	for _, bodyStmt := range defaultCase.Body {
		if _, err := c.compileStmt(ctx, bodyStmt); err != nil {
			c.Scopes.PopScope()
			return err
		}
	}
	c.Scopes.PopScope()
	return nil
}

// interfaceTargetMethodNames returns the case target's method names.
//
// Gathers the explicit and embedded method names of the interface type a type-switch case
// targets, or nil when the target is not an interface (or is the empty interface). The
// returned slice is recorded in CompiledFunction.TypeTableInterfaceMethods so
// handleTypeAssert can enforce method-set membership at runtime; pipit collapses every
// interface type to typeFor[any]() during reflect synthesis (no reflect.InterfaceOf
// exists), making the runtime Implements check useless for distinguishing `case error:`
// from `case fmt.Stringer:` without this sidecar.
//
// Takes target (types.Type) which is the case-clause target type.
//
// Returns []string with the sorted, deduplicated method names a value must expose to
// satisfy the interface; nil for non-interfaces and for empty interface targets (any).
func interfaceTargetMethodNames(target types.Type) []string {
	if target == nil {
		return nil
	}
	intf, ok := target.Underlying().(*types.Interface)
	if !ok {
		return nil
	}
	intf = intf.Complete()
	count := intf.NumMethods()
	if count == 0 {
		return nil
	}
	names := make([]string, 0, count)
	seen := make(map[string]struct{}, count)
	for i := range count {
		method := intf.Method(i)
		methodName := method.Name()
		if _, ok := seen[methodName]; ok {
			continue
		}
		seen[methodName] = struct{}{}
		signature := method.Signature()
		if typemap.ContainsTypeParameter(signature) {
			signature = types.NewSignatureType(nil, nil, nil, signature.Params(), signature.Results(), signature.Variadic())
			names = append(names, program.EncodeInterfaceMethodRequirementArity(methodName, signature))
			continue
		}
		names = append(names, program.EncodeInterfaceMethodRequirement(methodName, signature))
	}
	return names
}
