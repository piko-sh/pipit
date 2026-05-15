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

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

// selectorResult is what compiling a selector expression produces: the value's location
// and, for a method value, the operand compileSelectorNativeCall records on the native
// call site as its MethodReceiverRegister.
type selectorResult struct {
	// location holds the selector's value.
	location program.VarLocation

	// methodValueHint is the general register holding the receiver the method value was
	// bound to; a native method call site records it as MethodReceiverRegister so the
	// fast-path cache can tell when the receiver changed. Meaningful only when
	// hasMethodValueHint is set.
	methodValueHint uint8

	// hasMethodValueHint is true when the selector compiled as a method value.
	hasMethodValueHint bool
}

// sliceIndexStructFieldClassification bundles the fused `slice[i].Field` plan derived
// from a candidate selector.
type sliceIndexStructFieldClassification struct {
	// indexExpr is the underlying `slice[i]` index expression.
	indexExpr *ast.IndexExpr

	// op is the fused opSliceIndexStructFieldXxx opcode matching the leaf register kind.
	op isa.Opcode

	// resultKind is the leaf field's register kind.
	resultKind isa.RegisterKind

	// layoutIdx is the struct-field layout table index encoding the byte offset and kind tag
	// for the leaf field.
	layoutIdx uint16
}

// compileSelectorExpression compiles a selector expression and returns the value's
// location; compileSelectorExpressionDetailed also reports the method-value operand.
//
// Takes expression (*ast.SelectorExpr) which is the selector.
//
// Returns program.VarLocation which holds the value.
// Returns error when compilation fails.
func (c *Compiler) compileSelectorExpression(ctx context.Context, expression *ast.SelectorExpr) (program.VarLocation, error) {
	result, err := c.compileSelectorExpressionDetailed(ctx, expression)
	return result.location, err
}

// compileSelectorExpressionDetailed compiles a selector expression (s.Field, s.Method, or
// pkg.Symbol for an imported package).
//
// Takes expression (*ast.SelectorExpr) which is the selector node.
//
// Returns the selected value location and any compilation error.
func (c *Compiler) compileSelectorExpressionDetailed(ctx context.Context, expression *ast.SelectorExpr) (selectorResult, error) {
	if location, ok := c.compilePackageSymbol(ctx, expression); ok {
		return selectorValue(location), nil
	}

	if location, destructured, err := c.lookupDestructuredSelector(expression); destructured || err != nil {
		return selectorValue(location), err
	}

	selection, ok := c.Info.Selections[expression]
	if !ok {
		return selectorResult{}, fmt.Errorf("unresolved selector: %s", expression.Sel.Name)
	}

	if selection.Kind() == types.MethodExpr {
		location, err := c.compileMethodExprValue(ctx, expression, selection)
		return selectorValue(location), err
	}

	if selection.Kind() == types.FieldVal {
		if location, fused, err := c.tryCompileSliceIndexStructField(ctx, expression, selection); fused || err != nil {
			return selectorValue(location), err
		}
	}

	receiverLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return selectorResult{}, err
	}
	c.boxToGeneral(ctx, &receiverLocation)

	switch selection.Kind() {
	case types.FieldVal:
		location, err := c.compileSelectorFieldValue(ctx, selection, receiverLocation)
		return selectorValue(location), err
	case types.MethodVal:
		return c.compileSelectorMethodValue(ctx, expression, selection, receiverLocation)
	default:
		return selectorResult{}, fmt.Errorf("unsupported selector kind: %v for %s at %s", selection.Kind(), expression.Sel.Name, c.positionString(expression.Pos()))
	}
}

// compilePackageSymbol resolves a selector referring to a package-qualified symbol (e.g.
// fmt.Println) by loading it as a general-bank constant via the symbol registry.
//
// Takes expression (*ast.SelectorExpr) which is the selector node.
//
// Returns (location, true) on resolution, or (zero, false) when the selector is not a
// package symbol.
func (c *Compiler) compilePackageSymbol(_ context.Context, expression *ast.SelectorExpr) (program.VarLocation, bool) {
	if _, isSelection := c.Info.Selections[expression]; isSelection {
		return program.VarLocation{}, false
	}
	typeObject, ok := c.Info.Uses[expression.Sel]
	if !ok || typeObject.Pkg() == nil || c.symbols == nil {
		return program.VarLocation{}, false
	}
	value, found := c.symbols.Lookup(typeObject.Pkg().Path(), typeObject.Name())
	if !found {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, value, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbol,
		PackagePath: typeObject.Pkg().Path(),
		SymbolName:  typeObject.Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, dest, constIndex)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// compilePackageSymbolIdent resolves a bare dot-imported identifier.
//
// `import . "strings"` makes `ToUpper` reference `strings.ToUpper` unqualified. go/types
// fully resolves the identifier (`c.info.Uses[ident]` is the imported package's object),
// so the helper loads it as a general-bank constant via the symbol registry, exactly as
// compilePackageSymbol does for the qualified `pkg.Sym` form. Returns (zero, false) when
// the identifier is not a registered package symbol (a local variable, a current-package
// declaration, or an unregistered package), so callers fall through to their existing
// resolution.
//
// Takes identifier (*ast.Ident) which is the bare identifier.
//
// Returns VarLocation which is the loaded location on resolution.
// Returns bool which is true on successful resolution.
func (c *Compiler) compilePackageSymbolIdent(identifier *ast.Ident) (program.VarLocation, bool) {
	if c.symbols == nil || c.Info == nil {
		return program.VarLocation{}, false
	}
	object, ok := c.Info.Uses[identifier]
	if !ok || object.Pkg() == nil {
		return program.VarLocation{}, false
	}
	switch object.(type) {
	case *types.Func, *types.Var, *types.Const:
	default:
		return program.VarLocation{}, false
	}
	value, found := c.symbols.Lookup(object.Pkg().Path(), object.Name())
	if !found {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	constIndex, err := program.AddGeneralConstant(c.Function, value, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbol,
		PackagePath: object.Pkg().Path(),
		SymbolName:  object.Name(), TypeDescriptor: descriptor.TypeDescriptor{}})
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false
	}
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, dest, constIndex)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// compileSelectorFieldValue compiles s.Field, walking embedded field paths as needed.
// Tries the unsafe-pointer fast path first for named types without methods, falls back to
// a chain of isa.OpGetField loads.
//
// Takes selection (*types.Selection) which is the field selection.
// Takes receiverLocation (VarLocation) which is the receiver location.
//
// Returns the field value location and any compilation error.
func (c *Compiler) compileSelectorFieldValue(ctx context.Context, selection *types.Selection, receiverLocation program.VarLocation) (program.VarLocation, error) {
	resultKind := c.kindFor(selection.Type())
	leafType := types.Unalias(selection.Type())
	_, isNamedType := leafType.(*types.Named)

	fastPathEligibleNamed := !isNamedType || resultKind != isa.RegisterGeneral || namedTypeHasNoMethods(leafType)

	if receiverLocation.Kind == isa.RegisterGeneral && fastPathEligibleNamed {
		if location, applied := c.trySelectorFieldLowerings(ctx, selection, receiverLocation, resultKind); applied {
			return location, nil
		}
	}

	currentRegister := receiverLocation.Register
	for _, fieldIndex := range selection.Index() {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetField, dest, currentRegister, safeconv.MustIntToUint8(fieldIndex))
		currentRegister = dest
	}

	if resultKind != isa.RegisterGeneral {
		return c.emitUnboxFromGeneral(currentRegister, resultKind), nil
	}
	return program.VarLocation{Register: currentRegister, Kind: isa.RegisterGeneral}, nil
}

// tryEmitSelectorFieldSliceFastPath emits a typed-slice read sub-op when the leaf field's
// element kind matches a typed-slice bank.
//
// Takes selection (*types.Selection) which carries the field index path.
// Takes receiverLocation (VarLocation) which is the receiver location.
//
// Returns the result location and true when a typed-slice sub-op was emitted; (zero,
// false) otherwise.
func (c *Compiler) tryEmitSelectorFieldSliceFastPath(ctx context.Context, selection *types.Selection, receiverLocation program.VarLocation) (program.VarLocation, bool) {
	sub, destKind, ok := isaselect.PickGetStructFieldSliceSubOp(selection.Type())
	if !ok {
		return program.VarLocation{}, false
	}
	layoutIdx, layoutOK := c.tryResolveStructFieldLayout(ctx, selection)
	if !layoutOK {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(destKind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), dest, receiverLocation.Register)
	c.emitStructFieldLayoutExtension(layoutIdx)
	return program.VarLocation{Register: dest, Kind: destKind}, true
}

// tryEmitSelectorAssignSliceFastPath emits a typed-slice write sub-op when the leaf
// field's element kind and the value's bank both match a typed-slice bank.
//
// Takes selection (*types.Selection) which carries the field index path.
// Takes receiverLocation (VarLocation) which is the receiver register.
// Takes valueLocation (VarLocation) which is the source-value register.
//
// Returns true when a typed-slice sub-op was emitted; false otherwise.
func (c *Compiler) tryEmitSelectorAssignSliceFastPath(ctx context.Context, selection *types.Selection, receiverLocation, valueLocation program.VarLocation) bool {
	sub, sourceBankKind, ok := isaselect.PickSetStructFieldSliceSubOp(selection.Type())
	if !ok {
		return false
	}
	if valueLocation.Kind != sourceBankKind {
		return false
	}
	layoutIdx, layoutOK := c.tryResolveStructFieldLayout(ctx, selection)
	if !layoutOK {
		return false
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), receiverLocation.Register, valueLocation.Register)
	c.emitStructFieldLayoutExtension(layoutIdx)
	return true
}

// tryEmitSelectorFieldFastPath emits the unsafe-pointer fast path for a struct-field
// read, selecting either the tier-0 opcode when the layout index fits or a tier-1 sub-op
// otherwise.
//
// Takes selection (*types.Selection) which carries the field index path.
// Takes receiverLocation (VarLocation) which is the receiver location.
// Takes resultKind (isa.RegisterKind) which is the leaf field's register kind.
//
// Returns the result location and true when an opcode was emitted; (zero, false) when the
// layout cannot be resolved or no matching opcode exists.
func (c *Compiler) tryEmitSelectorFieldFastPath(ctx context.Context, selection *types.Selection, receiverLocation program.VarLocation, resultKind isa.RegisterKind) (program.VarLocation, bool) {
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok {
		return program.VarLocation{}, false
	}
	if fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		if op, hasOp := isaselect.PickGetStructFieldTier0Op(resultKind); hasOp {
			op = c.maybeRetargetCycleBrokenInterface(op, layoutIdx)
			dest := c.Scopes.Alloc.Alloc(resultKind)
			program.Emit(c.Function, op, dest, receiverLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
			return program.VarLocation{Register: dest, Kind: resultKind}, true
		}
	}
	sub, hasSubOp := isaselect.PickGetStructFieldUnsafeSubOp(resultKind)
	if !hasSubOp {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(resultKind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), dest, receiverLocation.Register)
	c.emitStructFieldLayoutExtension(layoutIdx)
	return program.VarLocation{Register: dest, Kind: resultKind}, true
}

// maybeRetargetCycleBrokenInterface swaps isa.OpGetStructFieldGeneral for
// isa.OpGetStructFieldRawPointerT0 when warranted.
//
// The runtime held value of a cycle-broken interface field is provably a pointer of the
// cycle-causing type (because convertFieldBreakingCycles substituted `any` for a
// self-referential pointer or container field at compile time), so the handler reads the
// pointer header directly. Gated on structFieldLayoutFlagCycleBroken so plain `any`
// fields, where the held value can be any concrete type, retain the generic handler.
//
// Takes op (opcode) which is the candidate get-field opcode.
// Takes layoutIdx (uint16) which indexes the field's structFieldLayout entry.
//
// Returns the retargeted opcode when the layout marks a cycle-broken interface, or the
// original op otherwise.
func (c *Compiler) maybeRetargetCycleBrokenInterface(op isa.Opcode, layoutIdx uint16) isa.Opcode {
	if op != isa.OpGetStructFieldGeneral {
		return op
	}
	if int(layoutIdx) >= len(c.Function.StructLayoutTable) {
		return op
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if layout.Kind != uint8(reflect.Interface) {
		return op
	}
	if layout.Flags&structFieldLayoutFlagCycleBroken == 0 {
		return op
	}
	return isa.OpGetStructFieldRawPointerT0
}

// compileSelectorMethodValue compiles a method value s.Method. Binds the receiver to the
// method via isa.OpBindMethod when the method resolves through the function table,
// otherwise emits isa.SubOpGetMethod through isa.OpDrillTier1.
//
// Takes expression (*ast.SelectorExpr) which is the selector node.
// Takes selection (*types.Selection) which is the type selection.
// Takes receiverLocation (VarLocation) which is the receiver location.
//
// Returns the bound method location and any compilation error.
func (c *Compiler) compileSelectorMethodValue(ctx context.Context, expression *ast.SelectorExpr, selection *types.Selection, receiverLocation program.VarLocation) (selectorResult, error) {
	if tableName, ok := c.resolveMethodTableName(ctx, expression); ok {
		if functionIndex, found := c.functionTable[tableName]; found {
			specialised, err := c.maybeSpecialiseMethod(ctx, expression, functionIndex)
			if err != nil {
				return selectorResult{}, err
			}
			return c.emitBoundMethod(ctx, selection, receiverLocation, specialised)
		}
	}

	receiverLocation = c.recastBoxedReceiverToNamedType(ctx, expression, receiverLocation)

	methodName := expression.Sel.Name
	nameIndex, err := program.AddStringConstant(c.Function, methodName)
	if err != nil {
		return selectorResult{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	getMethodPC := safeconv.IntToUint32Truncate(len(c.Function.Body))
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpGetMethod), dest, receiverLocation.Register)
	program.EmitExtension(c.Function, nameIndex, 0)

	if typeName := c.staticReceiverTypeName(expression.X); typeName != "" {
		if c.Function.GetMethodReceiverTypeNames == nil {
			c.Function.GetMethodReceiverTypeNames = make(map[uint32]string)
		}
		c.Function.GetMethodReceiverTypeNames[getMethodPC] = typeName
	}
	return selectorResult{
		location:           program.VarLocation{Register: dest, Kind: isa.RegisterGeneral},
		methodValueHint:    receiverLocation.Register,
		hasMethodValueHint: true,
	}, nil
}

// staticReceiverTypeName resolves the bare named source-level type of a method-call
// receiver expression, unwrapping a single pointer layer. Returns "" when the receiver's
// static type is anonymous.
//
// Takes receiver (ast.Expr) which is the receiver expression.
//
// Returns the bare type name, or "" when no named type resolves.
func (c *Compiler) staticReceiverTypeName(receiver ast.Expr) string {
	if c.Info == nil {
		return ""
	}
	tv, ok := c.Info.Types[receiver]
	if !ok || tv.Type == nil {
		return ""
	}
	return bareNamedTypeName(tv.Type)
}

// recastBoxedReceiverToNamedType emits isa.OpConvert to re-attach the receiver's named Go
// type when the generic boxToGeneral path stored it under the bank-default reflect.Type.
// Without this, methods defined on named numeric types such as time.Duration cannot be
// resolved by reflect.Type.MethodByName because the boxed value carries the underlying
// int64/uint64/float64 type.
//
// Skipped for struct and interface underlying types, where the boxed receiver already
// carries the named type.
//
// Takes expression (*ast.SelectorExpr) which carries the receiver expression whose static
// named type drives the recast.
// Takes receiverLocation (VarLocation) which is the existing receiver register slot.
//
// Returns a VarLocation pointing at the recast receiver register.
func (c *Compiler) recastBoxedReceiverToNamedType(ctx context.Context, expression *ast.SelectorExpr, receiverLocation program.VarLocation) program.VarLocation {
	if receiverLocation.Kind != isa.RegisterGeneral {
		return receiverLocation
	}
	staticType := c.staticTypeOf(expression.X)
	if staticType == nil {
		return receiverLocation
	}
	if pointer, ok := types.Unalias(staticType).(*types.Pointer); ok {
		staticType = pointer.Elem()
	}
	named, ok := types.Unalias(staticType).(*types.Named)
	if !ok {
		return receiverLocation
	}
	if named.Obj() == nil || named.Obj().Pkg() == nil {
		return receiverLocation
	}
	if _, isStruct := named.Underlying().(*types.Struct); isStruct {
		return receiverLocation
	}
	if _, isInterface := named.Underlying().(*types.Interface); isInterface {
		return receiverLocation
	}
	reflectType := c.TypeToReflect(ctx, named)
	if reflectType == nil {
		return receiverLocation
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		c.recordStickyError(err)
		return receiverLocation
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpConvert, dest, receiverLocation.Register, 0)
	program.EmitExtension(c.Function, typeIndex, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
}

// emitBoundMethod walks the embedded field path to reach the true receiver, then emits
// isa.OpBindMethod plus an extension word carrying the function-table index.
//
// Takes selection (*types.Selection) which is the type selection.
// Takes receiverLocation (VarLocation) which is the receiver location.
// Takes functionIndex (uint16) which is the function-table index of the method.
//
// Returns the bound method location and any compilation error.
func (c *Compiler) emitBoundMethod(_ context.Context, selection *types.Selection, receiverLocation program.VarLocation, functionIndex uint16) (selectorResult, error) {
	var fieldPath []int
	if index := selection.Index(); len(index) > 1 {
		fieldPath = index[:len(index)-1]
	}

	for _, fieldIndex := range fieldPath {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetField, dest, receiverLocation.Register, safeconv.MustIntToUint8(fieldIndex))
		receiverLocation = program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpBindMethod, dest, receiverLocation.Register, 0)
	program.EmitExtension(c.Function, functionIndex, 0)
	return selectorResult{
		location:           program.VarLocation{Register: dest, Kind: isa.RegisterGeneral},
		methodValueHint:    receiverLocation.Register,
		hasMethodValueHint: true,
	}, nil
}

// compileInterfaceMethodExpr compiles a method expression on an interface type.
//
// Produces a function value that takes the receiver first and dispatches the method on
// its dynamic type when called. The function type comes from go/types and the method name
// travels as a string constant. The static receiver type is recorded for the site as for
// GET_METHOD so external (pipit-declared) interfaces resolve.
//
// Takes expression (*ast.SelectorExpr) which is the method expression.
//
// Returns the location holding the function value and any compilation error.
func (c *Compiler) compileInterfaceMethodExpr(ctx context.Context, expression *ast.SelectorExpr) (program.VarLocation, error) {
	reflectType := c.TypeToReflect(ctx, c.substitutedType(c.Info.TypeOf(expression)))
	if reflectType == nil || reflectType.Kind() != reflect.Func {
		return program.VarLocation{}, fmt.Errorf("unsupported method expression: %s at %s", expression.Sel.Name, c.positionString(expression.Pos()))
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}
	nameIndex, err := program.AddStringConstant(c.Function, expression.Sel.Name)
	if err != nil {
		return program.VarLocation{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	sitePC := safeconv.IntToUint32Truncate(len(c.Function.Body))
	program.EmitTier2(c.Function, isa.SubOpTier2MakeInterfaceMethodExpr, dest)
	program.EmitExtension(c.Function, typeIndex, 0)
	program.EmitExtension(c.Function, nameIndex, 0)
	if typeName := c.staticReceiverTypeName(expression.X); typeName != "" {
		if c.Function.GetMethodReceiverTypeNames == nil {
			c.Function.GetMethodReceiverTypeNames = make(map[uint32]string)
		}
		c.Function.GetMethodReceiverTypeNames[sitePC] = typeName
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileMethodExprValue compiles a method expression used as a value (e.g. f :=
// Type.Method).
//
// The result is a function whose first parameter is the receiver. Emits
// isa.SubOpMakeMethodExpr plus a function-table index extension and one isa.OpExt per
// embedded field hop.
//
// Takes expression (*ast.SelectorExpr) which is the selector node.
// Takes selection (*types.Selection) which is the type selection.
//
// Returns the method expression location and any compilation error.
func (c *Compiler) compileMethodExprValue(ctx context.Context, expression *ast.SelectorExpr, selection *types.Selection) (program.VarLocation, error) {
	if _, isInterface := selection.Recv().Underlying().(*types.Interface); isInterface {
		return c.compileInterfaceMethodExpr(ctx, expression)
	}
	tableName, ok := c.resolveMethodTableName(ctx, expression)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("unsupported method expression: %s at %s", expression.Sel.Name, c.positionString(expression.Pos()))
	}
	functionIndex, found := c.functionTable[tableName]
	if !found {
		return program.VarLocation{}, fmt.Errorf("method not found: %s (receiver type: %v) at %s", tableName, selection.Recv(), c.positionString(expression.Pos()))
	}
	functionIndex, err := c.maybeSpecialiseMethod(ctx, expression, functionIndex)
	if err != nil {
		return program.VarLocation{}, err
	}

	var fieldPath []int
	if index := selection.Index(); len(index) > 1 {
		fieldPath = index[:len(index)-1]
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)

	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMakeMethodExpr), dest, safeconv.MustIntToUint8(len(fieldPath)))
	program.EmitExtension(c.Function, functionIndex, 0)
	for _, index := range fieldPath {
		program.Emit(c.Function, isa.OpExt, safeconv.MustIntToUint8(index), 0, 0)
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileStarExpression compiles a pointer dereference *p via isa.OpDeref. Unboxes the
// result when the pointee resolves to a typed bank.
//
// Takes expression (*ast.StarExpr) which is the star expression.
//
// Returns the dereferenced value location and any compilation error.
func (c *Compiler) compileStarExpression(ctx context.Context, expression *ast.StarExpr) (program.VarLocation, error) {
	pointerLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	return c.compileStarExpressionFrom(ctx, expression, pointerLocation)
}

// compileStarExpressionFrom reads through an already compiled pointer, so a compound
// assignment can evaluate `*p()` once for both the read and the store.
//
// Takes expression (*ast.StarExpr) which is the dereference, for its static type.
// Takes pointerLocation (program.VarLocation) which holds the pointer.
//
// Returns the location of the pointee value, or an error when the pointer is not in the
// general bank.
func (c *Compiler) compileStarExpressionFrom(_ context.Context, expression *ast.StarExpr, pointerLocation program.VarLocation) (program.VarLocation, error) {
	if pointerLocation.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, fault.ErrCompileDereferenceRequiresPointer
	}

	tv := c.Info.Types[expression]
	elementKind := c.kindFor(tv.Type)

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDeref, dest, pointerLocation.Register, 0)

	if elementKind != isa.RegisterGeneral {
		return c.emitUnboxFromGeneral(dest, elementKind), nil
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// selectorValue wraps a plain value location as a selectorResult with no method-value
// operand.
//
// Takes location (program.VarLocation) which holds the value.
//
// Returns selectorResult which carries only the location.
func selectorValue(location program.VarLocation) selectorResult {
	return selectorResult{location: location, methodValueHint: 0, hasMethodValueHint: false}
}

// namedTypeHasNoMethods reports an empty method set on a named type, checking both value
// and pointer receivers. Fail-closed so an uninspectable type stays on the general bank
// where method lookup can still find it.
//
// Takes t (types.Type) which is the candidate type.
//
// Returns true when t has zero methods, or is not a named type.
func namedTypeHasNoMethods(t types.Type) bool {
	unaliased := types.Unalias(t)
	if pointer, isPointer := unaliased.(*types.Pointer); isPointer {
		unaliased = types.Unalias(pointer.Elem())
	}
	named, ok := unaliased.(*types.Named)
	if !ok {
		return types.NewMethodSet(unaliased).Len() == 0 &&
			types.NewMethodSet(types.NewPointer(unaliased)).Len() == 0
	}
	if named.NumMethods() > 0 {
		return false
	}
	pointer := types.NewPointer(named)
	pointerMethodSet := types.NewMethodSet(pointer)
	return pointerMethodSet.Len() == 0
}
