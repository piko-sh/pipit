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
	"reflect"

	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// namedRetypeCollector finds the expressions whose value Go converts implicitly to a
// named array, slice, map, channel or function type.
type namedRetypeCollector struct {
	// compiler answers which types are named composite types and receives the operands in
	// its namedRetypes.
	compiler *Compiler

	// info is the type checker's record.
	info *types.Info

	// results are the result types of the function being compiled.
	results []types.Type
}

// visitNode records the conversions node makes to its own operands. Nested nodes are
// recorded when they are compiled.
//
// Takes node (ast.Node) which is a statement or expression about to be compiled.
func (collector *namedRetypeCollector) visitNode(node ast.Node) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		collector.visitAssign(typed)
	case *ast.DeclStmt:
		collector.visitDecl(typed.Decl)
	case *ast.ReturnStmt:
		collector.visitReturn(typed)
	case *ast.CallExpr:
		collector.visitCall(typed)
	case *ast.ExprStmt:
		if call, ok := typed.X.(*ast.CallExpr); ok {
			collector.visitCall(call)
		}
	case *ast.GoStmt:
		collector.visitCall(typed.Call)
	case *ast.DeferStmt:
		collector.visitCall(typed.Call)
	case *ast.CompositeLit:
		collector.visitCompositeLit(typed)
	case *ast.SendStmt:
		if channel, ok := collector.coreType(typed.Chan).(*types.Chan); ok {
			collector.note(typed.Value, channel.Elem())
		}
	case *ast.BinaryExpr:
		collector.visitComparison(typed)
	case *ast.SwitchStmt:
		collector.visitSwitch(typed)
	}
}

// visitDecl records the operands of each variable declaration in decl.
//
// Takes decl (ast.Decl) which is a declaration. Only variable declarations convert.
func (collector *namedRetypeCollector) visitDecl(decl ast.Decl) {
	general, ok := decl.(*ast.GenDecl)
	if !ok || general.Tok != token.VAR {
		return
	}
	for _, spec := range general.Specs {
		if valueSpec, isValue := spec.(*ast.ValueSpec); isValue {
			collector.visitValueSpec(valueSpec)
		}
	}
}

// visitAssign records the operands of a plain assignment. A short variable declaration
// gives each new variable its operand's type, so it converts nothing.
//
// Takes statement (*ast.AssignStmt) which is the assignment.
func (collector *namedRetypeCollector) visitAssign(statement *ast.AssignStmt) {
	if statement.Tok != token.ASSIGN || len(statement.Lhs) != len(statement.Rhs) {
		return
	}
	for index, target := range statement.Lhs {
		if identifier, ok := target.(*ast.Ident); ok && identifier.Name == "_" {
			continue
		}
		collector.note(statement.Rhs[index], collector.info.TypeOf(target))
	}
}

// visitValueSpec records the operands of a variable declaration with a written type.
//
// Takes spec (*ast.ValueSpec) which is the declaration.
func (collector *namedRetypeCollector) visitValueSpec(spec *ast.ValueSpec) {
	if spec.Type == nil || len(spec.Values) != len(spec.Names) {
		return
	}
	declared := collector.info.TypeOf(spec.Type)
	for _, value := range spec.Values {
		collector.note(value, declared)
	}
}

// visitReturn records the operands of a return statement against the enclosing function's
// results.
//
// Takes statement (*ast.ReturnStmt) which is the return statement.
func (collector *namedRetypeCollector) visitReturn(statement *ast.ReturnStmt) {
	if len(statement.Results) != len(collector.results) {
		return
	}
	for index, value := range statement.Results {
		collector.note(value, collector.results[index])
	}
}

// visitCall records the arguments of a call against the callee's parameters. A conversion
// converts explicitly and a built-in function has no parameters to convert to, except
// append, whose added elements convert to the slice's element type.
//
// Takes call (*ast.CallExpr) which is the call.
func (collector *namedRetypeCollector) visitCall(call *ast.CallExpr) {
	callee := collector.info.Types[call.Fun]
	if callee.IsType() {
		return
	}
	if callee.IsBuiltin() {
		collector.visitAppend(call)
		return
	}
	signature, ok := types.Unalias(callee.Type).Underlying().(*types.Signature)
	if !ok {
		return
	}
	parameters := signature.Params()
	if parameters.Len() == 0 {
		return
	}
	for index, argument := range call.Args {
		if index >= parameters.Len()-1 && signature.Variadic() {
			last := parameters.At(parameters.Len() - 1).Type()
			if call.Ellipsis.IsValid() {
				collector.note(argument, last)
			} else if slice, isSlice := last.Underlying().(*types.Slice); isSlice {
				collector.note(argument, slice.Elem())
			}
			continue
		}
		if index < parameters.Len() {
			collector.note(argument, parameters.At(index).Type())
		}
	}
}

// visitAppend records the added elements of an append call against the slice's element
// type.
//
// Takes call (*ast.CallExpr) which is a call of a built-in function.
func (collector *namedRetypeCollector) visitAppend(call *ast.CallExpr) {
	identifier, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || identifier.Name != "append" || len(call.Args) < 2 || call.Ellipsis.IsValid() {
		return
	}
	slice, ok := collector.coreType(call.Args[0]).(*types.Slice)
	if !ok {
		return
	}
	for _, element := range call.Args[1:] {
		collector.note(element, slice.Elem())
	}
}

// visitCompositeLit records a composite literal's elements, keys and fields against the
// literal type's element, key and field types.
//
// Takes literal (*ast.CompositeLit) which is the literal.
func (collector *namedRetypeCollector) visitCompositeLit(literal *ast.CompositeLit) {
	switch shape := collector.coreType(literal).(type) {
	case *types.Struct:
		collector.visitStructLit(literal, shape)
	case *types.Slice:
		collector.visitElements(literal, nil, shape.Elem())
	case *types.Array:
		collector.visitElements(literal, nil, shape.Elem())
	case *types.Map:
		collector.visitElements(literal, shape.Key(), shape.Elem())
	}
}

// visitStructLit records a struct literal's field values against the field types.
//
// Takes literal (*ast.CompositeLit) which is the literal.
// Takes shape (*types.Struct) which is its struct type.
func (collector *namedRetypeCollector) visitStructLit(literal *ast.CompositeLit, shape *types.Struct) {
	for index, element := range literal.Elts {
		keyed, isKeyed := element.(*ast.KeyValueExpr)
		if !isKeyed {
			if index < shape.NumFields() {
				collector.note(element, shape.Field(index).Type())
			}
			continue
		}
		name, ok := keyed.Key.(*ast.Ident)
		if !ok {
			continue
		}
		for field := range shape.Fields() {
			if field.Name() == name.Name {
				collector.note(keyed.Value, field.Type())
				break
			}
		}
	}
}

// visitElements records an array, slice or map literal's elements, and a map literal's
// keys.
//
// Takes literal (*ast.CompositeLit) which is the literal.
// Takes key (types.Type) which is the map key type, or nil for an array or slice.
// Takes element (types.Type) which is the element type.
func (collector *namedRetypeCollector) visitElements(literal *ast.CompositeLit, key, element types.Type) {
	for _, entry := range literal.Elts {
		keyed, isKeyed := entry.(*ast.KeyValueExpr)
		if !isKeyed {
			collector.note(entry, element)
			continue
		}
		if key != nil {
			collector.note(keyed.Key, key)
		}
		collector.note(keyed.Value, element)
	}
}

// visitComparison records the unnamed operand of == or != between a named composite type
// and its unnamed underlying type, which Go converts to the named type.
//
// Takes expression (*ast.BinaryExpr) which is the binary expression.
func (collector *namedRetypeCollector) visitComparison(expression *ast.BinaryExpr) {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return
	}
	if !collector.isNil(expression.Y) {
		collector.note(expression.Y, collector.info.TypeOf(expression.X))
	}
	if !collector.isNil(expression.X) {
		collector.note(expression.X, collector.info.TypeOf(expression.Y))
	}
}

// isNil reports whether expression is the predeclared nil, which a comparison tests for
// directly rather than converting.
//
// Takes expression (ast.Expr) which is the operand.
//
// Returns bool which is true for nil.
func (collector *namedRetypeCollector) isNil(expression ast.Expr) bool {
	return collector.info.Types[expression].IsNil()
}

// visitSwitch records the case values of an expression switch against the tag's type.
//
// Takes statement (*ast.SwitchStmt) which is the switch statement.
func (collector *namedRetypeCollector) visitSwitch(statement *ast.SwitchStmt) {
	if statement.Tag == nil {
		return
	}
	tagType := collector.info.TypeOf(statement.Tag)
	for _, clause := range statement.Body.List {
		caseClause, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, value := range caseClause.List {
			collector.note(value, tagType)
		}
	}
}

// note records operand when Go converts it implicitly to a named composite type, which is
// when the destination is such a type and the operand's own type differs from it.
//
// Takes operand (ast.Expr) which is the operand.
// Takes destination (types.Type) which is the type it is assigned to. It may be nil.
func (collector *namedRetypeCollector) note(operand ast.Expr, destination types.Type) {
	if destination == nil || !collector.compiler.isNamedCompositeType(destination) {
		return
	}
	source := collector.info.TypeOf(operand)
	if source == nil || types.Identical(source, destination) {
		return
	}
	if _, isInterface := source.Underlying().(*types.Interface); isInterface {
		return
	}
	if collector.compiler.namedRetypes == nil {
		collector.compiler.namedRetypes = make(map[ast.Expr]types.Type)
	}
	collector.compiler.namedRetypes[operand] = destination
}

// coreType returns the underlying type of expression's type, or nil.
//
// Takes expression (ast.Expr) which is the expression.
//
// Returns types.Type which is the underlying type.
func (collector *namedRetypeCollector) coreType(expression ast.Expr) types.Type {
	t := collector.info.TypeOf(expression)
	if t == nil {
		return nil
	}
	return t.Underlying()
}

// noteNamedRetypes records the operands of node that Go converts implicitly to a named
// composite type (namedRetypeCollector). Called as the compiler starts on a statement, a
// call, a composite literal or a binary expression, before it compiles the operands.
//
// Takes node (ast.Node) which is the node about to be compiled.
func (c *Compiler) noteNamedRetypes(node ast.Node) {
	if c.Info == nil {
		return
	}
	collector := namedRetypeCollector{compiler: c, info: c.Info, results: c.sourceResultTypes()}
	collector.visitNode(node)
}

// sourceResultTypes returns the result types a return statement being compiled returns
// to, which are the enclosing function's, also inside the yield closure of a
// range-over-func loop.
//
// Returns []types.Type which are the declared result types in source order.
func (c *Compiler) sourceResultTypes() []types.Type {
	if c.rangeOverFunction != nil {
		return c.rangeOverFunction.resultTypes
	}
	return c.currentResultTypes
}

// notePackageVariableRetypes records the operands of every package-level variable
// declaration in files, for the variable initialisers.
//
// Takes files ([]*ast.File) which are the files.
func (c *Compiler) notePackageVariableRetypes(files []*ast.File) {
	for _, file := range files {
		for _, decl := range file.Decls {
			c.noteNamedRetypes(&ast.DeclStmt{Decl: decl})
		}
	}
}

// compileRetypedExpression compiles an operand that Go converts implicitly to a named
// composite type and gives its value that type's runtime type.
//
// Takes expression (ast.Expr) which is the operand.
// Takes destination (types.Type) which is the named composite type.
//
// Returns the register location holding the retagged value, or an error from compiling
// the operand.
func (c *Compiler) compileRetypedExpression(ctx context.Context, expression ast.Expr, destination types.Type) (program.VarLocation, error) {
	if location, handled, err := c.compileTypedNilOrExpression(ctx, expression, destination); handled {
		return location, err
	}
	location, err := c.compileExpressionWithoutRetype(ctx, expression)
	if err != nil {
		return location, err
	}
	reflectType := c.namedCompositeReflectType(ctx, destination)
	if reflectType == nil {
		return location, nil
	}
	if location.Kind != isa.RegisterGeneral {
		location.SourceType = reflectType
		return location, nil
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}
	destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpConvertGeneral, destinationRegister, location.Register, 0)
	program.EmitExtension(c.Function, typeIndex, 0)
	return program.VarLocation{Register: destinationRegister, Kind: isa.RegisterGeneral}, nil
}

// isNamedCompositeType is typemap.IsNamedCompositeType remembered per named type for the
// compile. The compiler asks it of the static type of every expression it boxes and of
// every operand the retype collector visits, and the answer for a self-referential type
// costs a walk of its underlying type.
//
// Takes t (types.Type) which may be an alias or a type with substitutions pending.
//
// Returns bool which is true for a named composite type.
func (c *Compiler) isNamedCompositeType(t types.Type) bool {
	named, ok := types.Unalias(c.substitutedType(t)).(*types.Named)
	if !ok {
		return false
	}
	if known, seen := c.namedCompositeTypes[named]; seen {
		return known
	}
	result := typemap.IsNamedCompositeType(named)
	if c.namedCompositeTypes == nil {
		c.namedCompositeTypes = make(map[*types.Named]bool)
	}
	c.namedCompositeTypes[named] = result
	return result
}

// namedCompositeReflectType returns the runtime type of a named array, slice, map,
// channel or function type.
//
// Takes t (types.Type) which typemap.IsNamedCompositeType accepted.
//
// Returns reflect.Type which is the type's own runtime type, or nil when building it
// failed (TypeToReflect has then failed the compile).
func (c *Compiler) namedCompositeReflectType(ctx context.Context, t types.Type) reflect.Type {
	reflectType := c.TypeToReflect(ctx, t)
	if !typemodel.IsNamedCompositeType(reflectType) {
		return nil
	}
	return reflectType
}

// checkMethodTypeIsDistinct fails the compile when two non-generic named types that
// declare methods share one runtime type, which method dispatch through an interface,
// formatting and assertions could not tell apart.
//
// Every named type has a runtime type of its own, self-referential groups included,
// except in a build without the descriptor forging code (the safe and js/wasm builds).
// There a named array, slice, map, channel or function type shares its underlying type's
// runtime type (typemodel.NamedCompositeTypesSupported), so the check fires for two such
// types with the same underlying type that both declare methods. A generic type's methods
// are registered under its erased form, which its instantiations share by design.
//
// Takes named (*types.Named) which declares the method being registered.
// Takes reflectType (reflect.Type) which is named's runtime type.
//
// Returns error which wraps fault.ErrCompileNamedTypeIdentity when another named type
// already owns reflectType.
func (c *Compiler) checkMethodTypeIsDistinct(named *types.Named, reflectType reflect.Type) error {
	if reflectType == nil || named.TypeParams().Len() > 0 {
		return nil
	}
	if c.methodTypeOwners == nil {
		c.methodTypeOwners = make(map[reflect.Type]*types.Named)
	}
	owner, exists := c.methodTypeOwners[reflectType]
	if !exists {
		c.methodTypeOwners[reflectType] = named
		return nil
	}
	if owner.Obj() != named.Obj() {
		return fmt.Errorf("%w: %s and %s share the runtime type %s",
			fault.ErrCompileNamedTypeIdentity, owner.Obj().Name(), named.Obj().Name(), reflectType)
	}
	return nil
}
