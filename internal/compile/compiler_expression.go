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
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"reflect"

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

var (
	// exactBasicReflectTypes maps each go/types basic kind to the exact Go reflect.Type,
	// preserving int vs int64 so boxed values keep their source-level identity.
	exactBasicReflectTypes = map[types.BasicKind]reflect.Type{
		types.Bool:          reflect.TypeFor[bool](),
		types.Int:           reflect.TypeFor[int](),
		types.Int8:          reflect.TypeFor[int8](),
		types.Int16:         reflect.TypeFor[int16](),
		types.Int32:         reflect.TypeFor[int32](),
		types.Int64:         reflect.TypeFor[int64](),
		types.Uint:          reflect.TypeFor[uint](),
		types.Uint8:         reflect.TypeFor[uint8](),
		types.Uint16:        reflect.TypeFor[uint16](),
		types.Uint32:        reflect.TypeFor[uint32](),
		types.Uint64:        reflect.TypeFor[uint64](),
		types.Uintptr:       reflect.TypeFor[uintptr](),
		types.Float32:       reflect.TypeFor[float32](),
		types.Float64:       reflect.TypeFor[float64](),
		types.Complex64:     reflect.TypeFor[complex64](),
		types.Complex128:    reflect.TypeFor[complex128](),
		types.String:        reflect.TypeFor[string](),
		types.UntypedBool:   reflect.TypeFor[bool](),
		types.UntypedInt:    reflect.TypeFor[int](),
		types.UntypedRune:   reflect.TypeFor[int32](),
		types.UntypedFloat:  reflect.TypeFor[float64](),
		types.UntypedString: reflect.TypeFor[string](),
	}
)

// comparisonOpcodeSet holds the four bank-specific opcodes that implement one comparison
// operator: the int-bank, float-bank, string-bank, and general-bank variants. A zero
// floatOp, strOp, or genOp marks that the operator is unsupported for that bank.
type comparisonOpcodeSet struct {
	// intOp is the int-bank comparison opcode.
	intOp isa.Opcode

	// floatOp is the float-bank comparison opcode, or zero when unsupported.
	floatOp isa.Opcode

	// strOp is the string-bank comparison opcode, or zero when unsupported.
	strOp isa.Opcode

	// genOp is the general-bank comparison opcode, or zero when unsupported.
	genOp isa.Opcode
}

// EmitNarrowIntegerTruncation emits isa.OpTruncateToWidth when staticType is a narrow
// integer, preserving Go's modular wrap semantics.
//
// For float32 or complex64 it emits the matching rounding sub-op instead. A type
// parameter is narrowed as the type the body being compiled gives it.
//
// Takes location (program.VarLocation) which is the register holding the value to
// potentially truncate.
// Takes staticType (types.Type) which is the static Go type that determines the bit
// width.
func (c *Compiler) EmitNarrowIntegerTruncation(location program.VarLocation, staticType types.Type) {
	staticType = c.substitutedType(staticType)
	if subOp, bank, ok := floatNarrowingSubOp(staticType); ok {
		if location.Kind == bank {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), location.Register, location.Register)
		}
		return
	}
	bitWidth := typemap.NarrowIntegerBitWidth(staticType)
	if bitWidth == 0 {
		return
	}
	if location.Kind != isa.RegisterInt64 && location.Kind != isa.RegisterUint64 {
		return
	}
	program.Emit(c.Function, isa.OpTruncateToWidth, location.Register, bitWidth, uint8(location.Kind))
}

// compileExpression compiles expression and returns the register location holding the
// result. The Compiler caps recursion at expressionDepthLimit (defaultMaxExpressionDepth,
// 1024) to defend against pathologically deep input.
//
// Takes expression (ast.Expr) which is the AST expression to compile.
//
// Returns the register location holding the result, or an error when the expression
// nesting limit is exceeded or the form is unsupported.
func (c *Compiler) compileExpression(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	if destination, retype := c.namedRetypes[expression]; retype {
		return c.compileRetypedExpression(ctx, expression, destination)
	}
	return c.compileExpressionWithoutRetype(ctx, expression)
}

// compileExpressionWithoutRetype is compileExpression without the implicit conversion to
// a named composite type that compileRetypedExpression adds.
//
// Takes expression (ast.Expr) which is the AST expression to compile.
//
// Returns the register location holding the result, or an error when the expression
// nesting limit is exceeded or the form is unsupported.
func (c *Compiler) compileExpressionWithoutRetype(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	location, err := c.compileExpressionDispatch(ctx, expression)
	if err != nil {
		return location, err
	}
	if location.SourceType == nil && location.Kind != isa.RegisterGeneral {
		if tv, ok := c.Info.Types[expression]; ok && tv.Type != nil {
			location.SourceType = c.exactReflectTypeForBoxing(ctx, c.substitutedType(tv.Type))
		}
	}
	return location, nil
}

// compileGenericInstantiationValue compiles a generic instantiation used as a value
// rather than a call callee (e.g. `f := identity[int]`).
//
// Takes expression (ast.Expr) which is the IndexExpr or IndexListExpr.
// Takes operand (ast.Expr) which is the expression being instantiated.
//
// Returns the location and true when the expression was a generic instantiation.
func (c *Compiler) compileGenericInstantiationValue(ctx context.Context, expression, operand ast.Expr) (program.VarLocation, bool, error) {
	if c.Info == nil {
		return program.VarLocation{}, false, nil
	}
	ident := unwrapInstanceIdent(operand)
	if ident == nil {
		return program.VarLocation{}, false, nil
	}
	if _, ok := c.Info.Instances[ident]; !ok {
		return program.VarLocation{}, false, nil
	}

	if _, isFunc := c.Info.Uses[ident].(*types.Func); !isFunc {
		return program.VarLocation{}, false, nil
	}

	if plain, isIdent := operand.(*ast.Ident); isIdent {
		if functionIndex, found := c.functionTable[plain.Name]; found {
			location, err := c.bindInstantiatedFunctionValue(ctx, expression, ident, functionIndex)
			return location, true, err
		}
	}

	location, err := c.compileExpression(ctx, operand)
	return location, true, err
}

// bindInstantiatedFunctionValue monomorphises an explicitly instantiated generic function
// and binds the specialised body as a closure.
//
// Takes expression (ast.Expr) which is the instantiation expression, for error messages.
// Takes ident (*ast.Ident) which names the generic function.
// Takes functionIndex (uint16) which is the function table entry.
//
// Returns VarLocation which is the bound closure.
// Returns error when the instantiation could not be monomorphised.
func (c *Compiler) bindInstantiatedFunctionValue(
	ctx context.Context,
	expression ast.Expr,
	ident *ast.Ident,
	functionIndex uint16,
) (program.VarLocation, error) {
	specIndex, specialised, err := c.specialiseGenericValue(ctx, ident, functionIndex)
	if err != nil {
		return program.VarLocation{}, err
	}
	if !specialised {
		return program.VarLocation{}, fmt.Errorf("%w: %s at %s",
			fault.ErrCompileGenericValueNotSpecialisable,
			types.ExprString(expression), c.positionString(expression.Pos()))
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpMakeClosure, dest, specIndex)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileExpressionDispatch is the body of compileExpression. It dispatches on the AST
// node kind.
//
// Takes expression (ast.Expr) which is the AST node to compile.
//
// Returns the result location and any compilation error.
func (c *Compiler) compileExpressionDispatch(ctx context.Context, expression ast.Expr) (program.VarLocation, error) {
	limit := c.expressionDepthLimit()
	if c.expressionDepth >= limit {
		return program.VarLocation{}, fmt.Errorf("%w: expression nesting exceeded %d at %s", fault.ErrCompileDepthLimit, limit, c.positionString(expression.Pos()))
	}
	c.expressionDepth++
	defer func() { c.expressionDepth-- }()

	c.setDebugPosition(ctx, expression.Pos())

	if tv, ok := c.Info.Types[expression]; ok && tv.Value != nil {
		return c.compileConstant(ctx, tv)
	}

	switch e := expression.(type) {
	case *ast.BasicLit:
		return c.compileBasicLit(ctx, e)

	case *ast.Ident:
		return c.compileIdent(ctx, e)

	case *ast.BinaryExpr:
		return c.compileBinaryExpression(ctx, e)

	case *ast.UnaryExpr:
		return c.compileUnaryExpression(ctx, e)

	case *ast.ParenExpr:
		return c.compileExpression(ctx, e.X)

	case *ast.CallExpr:
		return c.compileCallExpression(ctx, e)

	case *ast.CompositeLit:
		return c.compileCompositeLit(ctx, e)

	case *ast.IndexExpr:
		return c.compileIndexOrInstantiation(ctx, e)

	case *ast.IndexListExpr:
		return c.compileIndexListInstantiation(ctx, e)

	case *ast.FuncLit:
		return c.compileFunctionLit(ctx, e)

	case *ast.SliceExpr:
		return c.compileSliceExpression(ctx, e)

	case *ast.SelectorExpr:
		return c.compileSelectorExpression(ctx, e)

	case *ast.StarExpr:
		return c.compileStarExpression(ctx, e)

	case *ast.TypeAssertExpr:
		return c.compileTypeAssertExpression(ctx, e)

	default:
		return program.VarLocation{}, fmt.Errorf("unsupported expression type: %T at %s", expression, c.positionString(expression.Pos()))
	}
}

// compileIndexOrInstantiation compiles an IndexExpr as either a collection index or a
// single-parameter generic instantiation.
//
// Takes expression (*ast.IndexExpr) which is the node to compile.
//
// Returns the compiled location and any compilation error.
func (c *Compiler) compileIndexOrInstantiation(ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, error) {
	if location, handled, err := c.compileGenericInstantiationValue(ctx, expression, expression.X); handled {
		return location, err
	}
	return c.compileIndexExpression(ctx, expression)
}

// compileIndexListInstantiation compiles an IndexListExpr, which is always a
// multi-parameter generic instantiation used as a value (g := mk[int, string]).
//
// Unlike IndexExpr it is never a collection index, so there is no non-generic reading to
// fall back to when no instantiation was recorded.
//
// Takes expression (*ast.IndexListExpr) which is the node to compile.
//
// Returns the compiled location and any compilation error.
func (c *Compiler) compileIndexListInstantiation(ctx context.Context, expression *ast.IndexListExpr) (program.VarLocation, error) {
	if location, handled, err := c.compileGenericInstantiationValue(ctx, expression, expression.X); handled {
		return location, err
	}
	return program.VarLocation{}, fmt.Errorf("%w: %s",
		fault.ErrCompileUninstantiatedGeneric, types.ExprString(expression))
}

// exactReflectTypeForBoxing returns the exact reflect.Type for boxing a scalar or a typed
// slice so that user-declared named types (such as `type Celsius float64` or `type Path
// []string`) keep a dynamic type distinct from their underlying type.
//
// Takes t (types.Type) which is the go/types static type of the value.
//
// Returns the exact reflect.Type, or nil when the general-purpose boxing path applies.
func (c *Compiler) exactReflectTypeForBoxing(ctx context.Context, t types.Type) reflect.Type {
	if t == nil {
		return nil
	}
	if c.isNamedCompositeType(t) {
		return c.namedCompositeReflectType(ctx, t)
	}
	underlying := t.Underlying()
	if slice, isSlice := types.Unalias(t).(*types.Slice); isSlice {
		if elem := c.exactReflectTypeForBoxing(ctx, slice.Elem()); elem != nil {
			return reflect.SliceOf(elem)
		}
		return nil
	}
	basic, ok := underlying.(*types.Basic)
	if !ok {
		return nil
	}
	if named, isNamed := types.Unalias(t).(*types.Named); isNamed {
		if poolType := c.namedScalarBoxType(ctx, named); poolType != nil {
			return poolType
		}
	}
	return exactBasicReflectTypes[basic.Kind()]
}

// namedScalarBoxType resolves the pool type a user-declared named basic type boxes as.
//
// Symbol-registered named types (native-backed generics and linked reflect.Types such as
// time.Duration) are excluded: those use the general-purpose box because their runtime
// identity is owned by the symbol registry and native code exchanges values of the real
// type.
//
// Takes named (*types.Named) which is the candidate named type.
//
// Returns the assigned pool reflect.Type, or nil when the type uses general-purpose
// boxing.
func (c *Compiler) namedScalarBoxType(ctx context.Context, named *types.Named) reflect.Type {
	if rt := typemap.ResolveNativeBackedType(named.Obj(), c.symbols); rt != nil {
		return rt
	}
	if rt := typemap.ResolveRegisteredType(named.Obj(), c.symbols); rt != nil {
		return rt
	}
	poolType, exhausted := typemodel.NamedScalarPoolTypeForResult(ctx, named)
	if exhausted {
		c.recordStickyError(fmt.Errorf("%w: %s", fault.ErrCompileNamedScalarPoolExhausted, named.Obj().Name()))
	}
	return poolType
}

// typeAssertReflectType returns the reflect.Type a type assertion should match against,
// consistent with exactReflectTypeForBoxing.
//
// Takes targetType (types.Type) which is the assertion's target type.
//
// Returns the reflect.Type to match against, or nil when targetType is nil.
func (c *Compiler) typeAssertReflectType(ctx context.Context, targetType types.Type) reflect.Type {
	if exact := c.exactReflectTypeForBoxing(ctx, targetType); exact != nil {
		return exact
	}
	return c.TypeToReflect(ctx, targetType)
}

// compileConstant emits a load for a compile-time constant value already folded by
// go/types. For interface-typed constants the scalar value is boxed through
// isa.OpBoxAsInterface into the general bank.
//
// Takes tv (types.TypeAndValue) which is the folded type-and-value record from go/types.
//
// Returns the register location holding the loaded constant, or an error when the
// constant cannot be loaded into the target bank.
func (c *Compiler) compileConstant(ctx context.Context, tv types.TypeAndValue) (program.VarLocation, error) {
	tv.Value = roundConstantToType(tv.Value, tv.Type)
	kind := c.kindFor(tv.Type)
	if kind != isa.RegisterGeneral {
		location, err := c.emitScalarConstant(ctx, tv.Value, kind)
		if err != nil {
			return program.VarLocation{}, err
		}

		location.SourceType = c.exactReflectTypeForBoxing(ctx, tv.Type)
		return location, nil
	}

	scalarKind := scalarKindForConstant(tv.Value)
	scalarLocation, err := c.emitScalarConstant(ctx, tv.Value, scalarKind)
	if err != nil {
		return program.VarLocation{}, err
	}
	scalarLocation.SourceType = c.exactReflectTypeForBoxing(ctx, tv.Type)
	generalRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	if c.emitTypedBox(generalRegister, scalarLocation) {
		return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}, nil
	}
	program.Emit(c.Function, isa.OpBoxAsInterface, generalRegister, scalarLocation.Register, uint8(scalarLocation.Kind))
	return program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}, nil
}

// emitScalarConstant adds value to the typed constant pool and emits a wide load into a
// fresh register.
//
// Takes value (constant.Value) which is the folded constant.
// Takes kind (isa.RegisterKind) which is the target register bank.
//
// Returns the register location holding the constant, or an error when the bank has no
// scalar pool.
func (c *Compiler) emitScalarConstant(_ context.Context, value constant.Value, kind isa.RegisterKind) (program.VarLocation, error) {
	switch kind {
	case isa.RegisterBool:
		return c.emitBoolScalarConstant(value)
	case isa.RegisterInt64:
		return c.emitIntScalarConstant(value)
	case isa.RegisterUint64:
		return c.emitUintScalarConstant(value)
	case isa.RegisterFloat64:
		return c.emitFloatScalarConstant(value)
	case isa.RegisterString:
		return c.emitStringScalarConstant(value)
	case isa.RegisterComplex128:
		return c.emitComplexScalarConstant(value)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported constant kind %v for register bank (value: %v)", kind, value)
	}
}

// emitLoadBoolConstant loads value into a freshly allocated bool register from the
// function's bool constant pool.
//
// Takes value (bool) which is the constant.
// Takes position (token.Pos) which is the expression it belongs to, for an error.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the bool constant pool is exhausted or the index does not fit.
func (c *Compiler) emitLoadBoolConstant(value bool, position token.Pos) (program.VarLocation, error) {
	index, err := program.AddBoolConstant(c.Function, value)
	if err != nil {
		return program.VarLocation{}, err
	}
	operand, err := c.uint8Operand(int(index), "bool constant index", position)
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterBool)
	program.EmitTier1(c.Function, isa.SubOpLoadBoolConstant, register, operand)
	return program.VarLocation{Register: register, Kind: isa.RegisterBool}, nil
}

// emitBoolScalarConstant loads value into a freshly allocated bool register.
//
// Takes value (constant.Value) which is the folded bool constant to load.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the bool constant pool is exhausted.
func (c *Compiler) emitBoolScalarConstant(value constant.Value) (program.VarLocation, error) {
	return c.emitLoadBoolConstant(constant.BoolVal(value), token.NoPos)
}

// emitIntScalarConstant loads value into a freshly allocated int register.
//
// Takes value (constant.Value) which is the folded integer constant to load.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the value cannot be represented as int64 or the constant pool is
// exhausted.
func (c *Compiler) emitIntScalarConstant(value constant.Value) (program.VarLocation, error) {
	value = constant.ToInt(value)
	v, ok := constant.Int64Val(value)
	if !ok && value.Kind() == constant.Int && constant.Sign(value) > 0 {
		v, ok = math.MaxInt64, true
	}
	if !ok {
		return program.VarLocation{}, fmt.Errorf("cannot convert constant to int64: %v", value)
	}
	index, err := program.AddIntConstant(c.Function, v)
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	program.EmitWide(c.Function, isa.OpLoadInt64Constant, register, index)
	return program.VarLocation{Register: register, Kind: isa.RegisterInt64}, nil
}

// emitUintScalarConstant loads value into a freshly allocated uint register.
//
// Takes value (constant.Value) which is the folded unsigned constant to load.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the value cannot be represented as uint64 or the constant pool is
// exhausted.
func (c *Compiler) emitUintScalarConstant(value constant.Value) (program.VarLocation, error) {
	value = constant.ToInt(value)
	if value.Kind() != constant.Int {
		return program.VarLocation{}, fmt.Errorf("cannot convert constant to uint64: %v", value)
	}
	u, ok := constant.Uint64Val(value)
	if !ok {
		v, valueOk := constant.Int64Val(value)
		if valueOk {
			u = safeconv.Int64ToUint64Reinterpret(v)
			ok = true
		}
	}
	if !ok {
		return program.VarLocation{}, fmt.Errorf("cannot convert constant to uint64: %v", value)
	}
	index, err := program.AddUintConstant(c.Function, u)
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterUint64)
	program.EmitWide(c.Function, isa.OpLoadUint64Constant, register, index)
	return program.VarLocation{Register: register, Kind: isa.RegisterUint64}, nil
}

// emitFloatScalarConstant loads value into a freshly allocated float register.
//
// Takes value (constant.Value) which is the folded floating-point constant.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the float constant pool is exhausted.
func (c *Compiler) emitFloatScalarConstant(value constant.Value) (program.VarLocation, error) {
	v, _ := constant.Float64Val(value)
	index, err := program.AddFloatConstant(c.Function, v)
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterFloat64)
	program.EmitWide(c.Function, isa.OpLoadFloat64Constant, register, index)
	return program.VarLocation{Register: register, Kind: isa.RegisterFloat64}, nil
}

// emitStringScalarConstant loads value into a freshly allocated string register.
//
// Takes value (constant.Value) which is the folded string constant.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the string constant pool is exhausted.
func (c *Compiler) emitStringScalarConstant(value constant.Value) (program.VarLocation, error) {
	v := constant.StringVal(value)
	index, err := program.AddStringConstant(c.Function, v)
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterString)
	program.EmitWide(c.Function, isa.OpLoadStringConstant, register, index)
	return program.VarLocation{Register: register, Kind: isa.RegisterString}, nil
}

// emitComplexScalarConstant loads value into a freshly allocated complex register.
//
// Takes value (constant.Value) which is the folded complex constant.
//
// Returns VarLocation which holds the loaded register's metadata.
// Returns error when the complex constant pool is exhausted.
func (c *Compiler) emitComplexScalarConstant(value constant.Value) (program.VarLocation, error) {
	realPart, _ := constant.Float64Val(constant.Real(value))
	imaginaryPart, _ := constant.Float64Val(constant.Imag(value))
	index, err := program.AddComplexConstant(c.Function, complex(realPart, imaginaryPart))
	if err != nil {
		return program.VarLocation{}, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterComplex128)
	program.EmitWide(c.Function, isa.OpLoadComplex128Constant, register, index)
	return program.VarLocation{Register: register, Kind: isa.RegisterComplex128}, nil
}

// compileBasicLit compiles a basic literal (number, string, etc.). Reached only when
// compileExpression's pre-check did not find a folded constant for the literal in
// c.Info.Types.
//
// Takes lit (*ast.BasicLit) which is the basic literal AST node to compile.
//
// Returns the register location holding the literal, or an error when no type information
// is available for the literal.
func (c *Compiler) compileBasicLit(ctx context.Context, lit *ast.BasicLit) (program.VarLocation, error) {
	tv, ok := c.Info.Types[lit]
	if ok && tv.Value != nil {
		return c.compileConstant(ctx, tv)
	}
	return program.VarLocation{}, fmt.Errorf("basic literal without type info: %s", lit.Value)
}

// compileIdent compiles an identifier reference, resolving the predeclared literals
// (true, false, nil), local scope variables (including spilled and indirect locations),
// upvalues, globals, and top-level functions in that order.
//
// Takes identifier (*ast.Ident) which is the identifier AST node to resolve and load.
//
// Returns the register location holding the identifier's value, or an error when the
// identifier is not defined in any scope.
func (c *Compiler) compileIdent(ctx context.Context, identifier *ast.Ident) (program.VarLocation, error) {
	if identifier.Name == identTrue || identifier.Name == identFalse {
		return c.emitLoadBoolConstant(identifier.Name == identTrue, identifier.Pos())
	}
	if identifier.Name == identNil {
		register := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2LoadNil), register)
		return program.VarLocation{Register: register, Kind: isa.RegisterGeneral}, nil
	}

	location, found := c.Scopes.LookupVar(identifier.Name)
	if found {
		if location.IsIndirect {
			return c.EmitIndirectRead(ctx, location)
		}
		if location.IsSpilled {
			return c.emitReloadIfSpilled(ctx, location), nil
		}
		return location, nil
	}

	if reference, ok := c.upvalueMap[identifier.Name]; ok {
		dest := c.Scopes.Alloc.Alloc(reference.kind)
		program.Emit(c.Function, isa.OpGetCapturedVariable, dest, safeconv.MustIntToUint8(reference.index), uint8(reference.kind))
		return program.VarLocation{Register: dest, Kind: reference.kind}, nil
	}

	if gv, ok := c.globalVariables[identifier.Name]; ok {
		return c.emitGetGlobal(ctx, gv), nil
	}

	if functionIndex, found := c.functionTable[identifier.Name]; found {
		return c.bindFunctionValue(ctx, identifier, functionIndex)
	}

	return program.VarLocation{}, fmt.Errorf("undefined: %s at %s", identifier.Name, c.positionString(identifier.Pos()))
}

// unboxScalarOperand moves an operand that sits in the general bank, but whose static
// type lives in a scalar bank (a type assertion result, a value of a substituted type
// parameter), into that bank so the typed arithmetic emitters apply.
//
// Takes location (VarLocation) which holds the operand.
// Takes staticType (types.Type) which is the operand's static type; nil leaves it alone.
//
// Returns the operand's location in its scalar bank, or unchanged.
func (c *Compiler) unboxScalarOperand(_ context.Context, location program.VarLocation, staticType types.Type) (program.VarLocation, error) {
	if location.Kind != isa.RegisterGeneral || staticType == nil {
		return location, nil
	}
	kind := c.kindFor(staticType)
	switch kind {
	case isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterString, isa.RegisterBool, isa.RegisterUint64, isa.RegisterComplex128:
		return c.emitUnboxFromGeneral(location.Register, kind), nil
	default:
		return location, nil
	}
}

// compileBinaryExpression compiles a binary expression.
//
// Takes expression (*ast.BinaryExpr) which is the binary expression AST node to compile.
//
// Returns program.VarLocation which holds the result.
// Returns error when an operand or the operator cannot be compiled.
func (c *Compiler) compileBinaryExpression(ctx context.Context, expression *ast.BinaryExpr) (program.VarLocation, error) {
	c.noteNamedRetypes(expression)
	if expression.Op == token.LAND {
		return c.compileShortCircuitAnd(ctx, expression)
	}
	if expression.Op == token.LOR {
		return c.compileShortCircuitOr(ctx, expression)
	}

	if location, ok, err := c.tryCompileInterfaceNilComparison(ctx, expression); ok || err != nil {
		return location, err
	}

	mark := c.Scopes.Alloc.Snapshot()
	left, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	afterLeft := c.Scopes.Alloc.Snapshot()

	right, err := c.compileShiftCountOrExpression(ctx, expression)
	if err != nil {
		return program.VarLocation{}, err
	}

	if ops, isComparison := comparisonOpcodes(expression.Op); isComparison {
		return c.emitStaticComparison(ctx, expression.Op, ops,
			c.staticTypeOf(expression.X), c.staticTypeOf(expression.Y), left, right)
	}

	if left, err = c.unboxScalarOperand(ctx, left, c.staticTypeOf(expression.X)); err != nil {
		return program.VarLocation{}, err
	}
	if right, err = c.unboxScalarOperand(ctx, right, c.staticTypeOf(expression.Y)); err != nil {
		return program.VarLocation{}, err
	}
	destination := binaryResultDestination(expression, mark, afterLeft, left, right)
	result, err := c.emitBinaryOpInto(ctx, expression.Op, left, right, destination)
	if err != nil {
		return result, err
	}
	if destination != nil {
		c.Scopes.RestoreWatermark(afterLeft)
		c.Scopes.KeepLocation(result)
	}
	if t := c.staticTypeOf(expression); t != nil && !typemap.BinaryResultFitsNarrowType(expression.Op, t) {
		c.EmitNarrowIntegerTruncation(result, t)
	}
	return result, nil
}

// tryCompileInterfaceNilComparison emits isa.SubOpEqualInterfaceNil or
// isa.SubOpNotEqualInterfaceNil when expression is an interface-vs-nil equality
// comparison, preserving Go's distinction between a nil interface and an interface
// holding a typed nil. Returns applied=false when the pattern does not match.
//
// Takes expression (*ast.BinaryExpr) which is the binary expression to inspect for the
// pattern.
//
// Returns the register location holding the comparison result, a flag set when the
// pattern matched, and an error when compiling the interface side failed.
func (c *Compiler) tryCompileInterfaceNilComparison(ctx context.Context, expression *ast.BinaryExpr) (program.VarLocation, bool, error) {
	if expression.Op != token.EQL && expression.Op != token.NEQ {
		return program.VarLocation{}, false, nil
	}
	interfaceSide, nilSide, ok := c.classifyInterfaceNilOperands(expression.X, expression.Y)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	_ = nilSide
	location, err := c.compileExpression(ctx, interfaceSide)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	c.boxToGeneral(ctx, &location)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	subOp := isa.SubOpEqualInterfaceNil
	if expression.Op == token.NEQ {
		subOp = isa.SubOpNotEqualInterfaceNil
	}
	program.EmitTier1(c.Function, subOp, dest, location.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}, true, nil
}

// classifyInterfaceNilOperands returns the interface-typed and the nil-literal operand
// from a binary equality expression, with matched set when either side ordering produces
// the pattern.
//
// Takes left (ast.Expr) which is the left-hand operand of the equality expression.
// Takes right (ast.Expr) which is the right-hand operand of the equality expression.
//
// Returns the interface-typed operand, the nil-literal operand, and a flag set when one
// side is interface-typed and the other is nil.
func (c *Compiler) classifyInterfaceNilOperands(left, right ast.Expr) (interfaceExpr ast.Expr, nilExpr ast.Expr, matched bool) {
	if c.isNilLiteral(right) && c.expressionIsInterfaceTyped(left) {
		return left, right, true
	}
	if c.isNilLiteral(left) && c.expressionIsInterfaceTyped(right) {
		return right, left, true
	}
	return nil, nil, false
}

// expressionIsInterfaceTyped reports whether expression's static type underlies as an
// interface (empty or non-empty).
//
// Takes expression (ast.Expr) which is the AST expression to inspect.
//
// Returns true when the underlying static type is an interface.
func (c *Compiler) expressionIsInterfaceTyped(expression ast.Expr) bool {
	staticType := c.staticTypeOf(expression)
	if staticType == nil {
		return false
	}
	_, ok := staticType.Underlying().(*types.Interface)
	return ok
}

// isNilLiteral reports whether expression resolves to the universe-scope nil identifier
// (as confirmed by go/types when type info is available).
//
// Takes expression (ast.Expr) which is the AST expression to test.
//
// Returns true when expression is the predeclared nil identifier.
func (c *Compiler) isNilLiteral(expression ast.Expr) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok || identifier.Name != "nil" {
		return false
	}
	if c.Info == nil {
		return true
	}
	if object := c.Info.Uses[identifier]; object != nil {
		_, isNil := object.(*types.Nil)
		return isNil
	}
	return true
}

// compileTypedNilOrExpression emits a typed-zero load for typed `nil` literals.
//
// Fires when expression is the predeclared `nil` identifier and expectedType is a
// concrete nil-bearing type (pointer, slice, map, chan, signature/func). Returns
// (location, true, nil) when the typed-nil path fired so the caller skips the regular
// compileExpression path; otherwise returns (zero, false, nil) to signal a fall-through.
//
// Without this, `nil` always compiles to opLoadNil which stores reflect.Value{} (truly
// zero, no type tag). For typed destinations like *Holder that reach interface-conversion
// or dynamic method dispatch, the lost type tag panics handleCallMethod's receiver.Type()
// and breaks `s == nil` semantics for typed-nil interfaces. Interface destinations
// intentionally keep the untyped nil so a == nil stays true.
//
// Takes expression (ast.Expr) which is the source expression about to be compiled.
// Takes expectedType (types.Type) which is the static type at the consumption site
// (declared variable type, return slot type, parameter type, etc).
//
// Returns a location holding the typed-zero reflect.Value when the fast path fired, true
// to indicate the caller should skip compileExpression, and any compilation error from
// the constant pool addition.
func (c *Compiler) compileTypedNilOrExpression(ctx context.Context, expression ast.Expr, expectedType types.Type) (program.VarLocation, bool, error) {
	if expectedType == nil {
		return program.VarLocation{}, false, nil
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok || identifier.Name != identNil {
		return program.VarLocation{}, false, nil
	}
	if c.Info != nil {
		if object := c.Info.Uses[identifier]; object != nil {
			if _, isNil := object.(*types.Nil); !isNil {
				return program.VarLocation{}, false, nil
			}
		}
	}
	underlying := expectedType.Underlying()
	if underlying == nil {
		return program.VarLocation{}, false, nil
	}
	switch underlying.(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature:
	default:
		return program.VarLocation{}, false, nil
	}
	reflectType := c.TypeToReflect(ctx, expectedType)
	if reflectType == nil {
		return program.VarLocation{}, false, nil
	}
	constIndex, err := program.AddGeneralConstant(c.Function, reflect.Zero(reflectType), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero,
		TypeDescriptor: descriptor.TypeDescriptor{}, PackagePath: "", SymbolName: ""})
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false, err
	}
	register := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpLoadGeneralConstant, register, constIndex)
	return program.VarLocation{Register: register, Kind: isa.RegisterGeneral}, true, nil
}

// compileShiftCountOrExpression compiles the right operand of expression, loading a
// constant shift count into the uint bank.
//
// Takes expression (*ast.BinaryExpr) which is the binary expression being compiled.
//
// Returns the right operand's register location, or an error when it fails to compile.
func (c *Compiler) compileShiftCountOrExpression(ctx context.Context, expression *ast.BinaryExpr) (program.VarLocation, error) {
	if expression.Op == token.SHL || expression.Op == token.SHR {
		if tv, ok := c.Info.Types[expression.Y]; ok && tv.Value != nil {
			return c.emitScalarConstant(ctx, constant.ToInt(tv.Value), isa.RegisterUint64)
		}
	}
	return c.compileExpression(ctx, expression.Y)
}

// compileShortCircuitAnd compiles a logical AND with short-circuit evaluation, skipping
// the right operand when the left is false. The result is an int register holding 0 or 1.
//
// Takes expression (*ast.BinaryExpr) which is the logical AND binary expression to
// compile.
//
// Returns the int register location holding 0 or 1, or an error when either operand fails
// to compile.
func (c *Compiler) compileShortCircuitAnd(ctx context.Context, expression *ast.BinaryExpr) (program.VarLocation, error) {
	return c.compileShortCircuit(ctx, expression, isa.OpJumpIfFalse)
}

// compileShortCircuitOr compiles a logical OR with short-circuit evaluation, skipping the
// right operand when the left is true. The result is an int register holding 0 or 1.
//
// Takes expression (*ast.BinaryExpr) which is the logical OR binary expression to
// compile.
//
// Returns the int register location holding 0 or 1, or an error when either operand fails
// to compile.
func (c *Compiler) compileShortCircuitOr(ctx context.Context, expression *ast.BinaryExpr) (program.VarLocation, error) {
	return c.compileShortCircuit(ctx, expression, isa.OpJumpIfTrue)
}

// compileShortCircuit compiles a boolean expression that skips the right operand when the
// left determines the result.
//
// The left-to-result move is emitted even when both registers are the same because
// range-check fusion depends on that instruction pattern.
//
// Takes expression (*ast.BinaryExpr) which is the binary expression to compile.
// Takes skipOp (isa.Opcode) which is the conditional branch opcode used to skip the right
// operand.
//
// Returns the int register location holding 0 or 1, or an error when either operand fails
// to compile.
func (c *Compiler) compileShortCircuit(ctx context.Context, expression *ast.BinaryExpr, skipOp isa.Opcode) (program.VarLocation, error) {
	mark := c.Scopes.Alloc.Snapshot()
	left, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	left = c.ensureIntForBranch(ctx, left)
	dest := left.Register
	if !reusableShortCircuitResult(expression.X, left, mark) {
		dest = c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), dest, left.Register)
	afterLeft := c.Scopes.Alloc.Snapshot()
	jumpToEnd := program.EmitJump(c.Function, skipOp, dest)
	right, err := c.compileExpression(ctx, expression.Y)
	if err != nil {
		return program.VarLocation{}, err
	}
	right = c.ensureIntForBranch(ctx, right)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMoveInt64), dest, right.Register)
	program.PatchJump(c.Function, jumpToEnd)
	c.Scopes.RestoreWatermark(afterLeft)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}, nil
}

// emitBinaryOp emits the typed instruction for binary operator op, routing arithmetic,
// comparison, and bitwise/shift tokens to the matching specialised emitter.
//
// Takes op (token.Token) which is the binary operator token.
// Takes left (program.VarLocation) which is the left operand register location.
// Takes right (program.VarLocation) which is the right operand register location.
//
// Returns the register location holding the result, or an error when the operator is
// unsupported for the operand banks.
func (c *Compiler) emitBinaryOp(ctx context.Context, op token.Token, left, right program.VarLocation) (program.VarLocation, error) {
	return c.emitBinaryOpInto(ctx, op, left, right, nil)
}

// emitBinaryOpInto is emitBinaryOp writing an arithmetic or bitwise result into dest when
// dest is non-nil and of the result's bank.
//
// Comparisons always take a fresh register.
//
// Takes op (token.Token) which is the binary operator.
// Takes left (VarLocation) which is the left operand.
// Takes right (VarLocation) which is the right operand.
// Takes dest (*program.VarLocation) which is the register to overwrite, or nil.
//
// Returns program.VarLocation which holds the result.
// Returns error which reports a compilation failure.
func (c *Compiler) emitBinaryOpInto(ctx context.Context, op token.Token, left, right program.VarLocation, dest *program.VarLocation) (program.VarLocation, error) {
	switch op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM:
		return c.emitArithmetic(ctx, op, left, right, dest)

	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		ops, _ := comparisonOpcodes(op)
		return c.emitCompareOp(ctx, ops.intOp, ops.floatOp, ops.strOp, ops.genOp, left, right)

	case token.AND:
		return c.emitIntOnlyOp(ctx, isa.OpBitAndInt64, left, right, dest)
	case token.OR:
		return c.emitIntOnlyOp(ctx, isa.OpBitOrInt64, left, right, dest)
	case token.XOR:
		return c.emitIntOnlyOp(ctx, isa.OpBitXorInt64, left, right, dest)
	case token.AND_NOT:
		return c.emitIntOnlyOp(ctx, isa.OpBitAndNotInt64, left, right, dest)
	case token.SHL:
		c.emitShiftCountCheck(right)
		return c.emitIntOnlyOp(ctx, isa.OpShiftLeftInt64, left, right, dest)
	case token.SHR:
		c.emitShiftCountCheck(right)
		return c.emitIntOnlyOp(ctx, isa.OpShiftRightInt64, left, right, dest)

	default:
		return program.VarLocation{}, fmt.Errorf("unsupported binary operator: %s (left=%v, right=%v)", op, left.Kind, right.Kind)
	}
}

// emitCompareOp emits the bank-specialised comparison for left and right.
//
// Takes intOp (isa.Opcode) which is the int-bank opcode.
// Takes floatOp (isa.Opcode) which is the float opcode, or zero when unsupported.
// Takes strOp (isa.Opcode) which is the string opcode, or zero when unsupported.
// Takes genOp (isa.Opcode) which is the general-bank opcode, or zero when unsupported.
// Takes left (VarLocation) which is the left operand.
// Takes right (VarLocation) which is the right operand.
//
// Returns the int-bank 0/1 result, or an error when the bank is unsupported.
func (c *Compiler) emitCompareOp(ctx context.Context, intOp, floatOp, strOp, genOp isa.Opcode, left, right program.VarLocation) (program.VarLocation, error) {
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	destinationLocation := program.VarLocation{Register: dest, Kind: isa.RegisterInt64}

	switch left.Kind {
	case isa.RegisterInt64:
		c.EmitTyped(ctx, intOp, destinationLocation, left, right)
	case isa.RegisterFloat64:
		if floatOp == 0 {
			return program.VarLocation{}, fault.ErrCompileCompareFloatUnsupported
		}
		c.EmitTyped(ctx, floatOp, destinationLocation, left, right)
	case isa.RegisterString:
		if strOp == 0 {
			return program.VarLocation{}, fault.ErrCompileCompareStringUnsupported
		}
		c.EmitTyped(ctx, strOp, destinationLocation, left, right)
	case isa.RegisterBool:
		c.emitBoolCompare(ctx, intOp, dest, left, right)
	case isa.RegisterUint64:
		c.emitUintCompare(ctx, intOp, genOp, dest, left, right)
	case isa.RegisterComplex128:
		if err := c.emitComplexCompare(ctx, intOp, dest, left, right); err != nil {
			return program.VarLocation{}, err
		}
	default:
		if genOp == 0 {
			return program.VarLocation{}, fault.ErrCompileCompareGeneralUnsupported
		}
		c.EmitTyped(ctx, genOp, destinationLocation, left, right)
	}

	return destinationLocation, nil
}

// emitStaticComparison emits a comparison honouring Go's static-type scalar rule,
// unboxing operands to the scalar bank when both are non-interface.
//
// Takes op (token.Token) which distinguishes equality from ordering.
// Takes ops (comparisonOpcodeSet) which is the bank-specific opcode set.
// Takes leftType (types.Type) which is the left operand's static type.
// Takes rightType (types.Type) which is the right operand's static type.
// Takes left (VarLocation) which is the compiled left operand.
// Takes right (VarLocation) which is the compiled right operand.
//
// Returns the int-bank 0/1 result, or an error.
func (c *Compiler) emitStaticComparison(ctx context.Context, op token.Token, ops comparisonOpcodeSet, leftType, rightType types.Type, left, right program.VarLocation) (program.VarLocation, error) {
	if kind, ok := comparisonScalarKind(leftType, rightType); ok {
		left = c.coerceToKind(ctx, left, kind)
		right = c.coerceToKind(ctx, right, kind)
		return c.emitCompareOp(ctx, ops.intOp, ops.floatOp, ops.strOp, ops.genOp, left, right)
	}
	if op == token.EQL || op == token.NEQ {
		c.boxElementToGeneralTemp(ctx, &left)
		c.boxElementToGeneralTemp(ctx, &right)
		if strictInterfaceComparison(leftType, rightType) {
			return c.emitStrictInterfaceComparison(op, left, right), nil
		}
	}
	return c.emitCompareOp(ctx, ops.intOp, ops.floatOp, ops.strOp, ops.genOp, left, right)
}

// emitStrictInterfaceComparison emits isa.SubOpEqualInterfaceStrict or
// isa.SubOpNotEqualInterfaceStrict for two general-bank operands, with the int
// destination carried by the trailing extension word.
//
// Takes op (token.Token) which is token.EQL or token.NEQ.
// Takes left (program.VarLocation) which is the boxed left operand.
// Takes right (program.VarLocation) which is the boxed right operand.
//
// Returns the int register location holding 0 or 1.
func (c *Compiler) emitStrictInterfaceComparison(op token.Token, left, right program.VarLocation) program.VarLocation {
	subOp := isa.SubOpEqualInterfaceStrict
	if op == token.NEQ {
		subOp = isa.SubOpNotEqualInterfaceStrict
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), left.Register, right.Register)
	program.EmitExtension(c.Function, uint16(dest), 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}
}

// emitBoolCompare converts both bool operands to int via isa.SubOpConvertBoolToInt64 in
// temporary registers and emits the integer comparison into dest.
//
// Takes intOp (isa.Opcode) which is the integer comparison opcode to Emit.
// Takes dest (uint8) which is the destination int register receiving the 0/1 result.
// Takes left (program.VarLocation) which is the left bool operand register location.
// Takes right (program.VarLocation) which is the right bool operand register location.
func (c *Compiler) emitBoolCompare(_ context.Context, intOp isa.Opcode, dest uint8, left, right program.VarLocation) {
	leftInt := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	rightInt := c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), leftInt, left.Register)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), rightInt, right.Register)
	program.Emit(c.Function, intOp, dest, leftInt, rightInt)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, leftInt)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, rightInt)
}

// emitUintCompare maps intOp to its uint comparison counterpart and emits it into dest,
// falling back to genOp when no uint mapping exists and genOp is non-zero.
//
// Takes intOp (isa.Opcode) which is the int comparison opcode whose uint counterpart is
// sought.
// Takes genOp (isa.Opcode) which is the general-bank fallback opcode, or zero when no
// fallback.
// Takes dest (uint8) which is the destination int register receiving the 0/1 result.
// Takes left (program.VarLocation) which is the left uint operand register location.
// Takes right (program.VarLocation) which is the right uint operand register location.
func (c *Compiler) emitUintCompare(_ context.Context, intOp, genOp isa.Opcode, dest uint8, left, right program.VarLocation) {
	uintCmpOp, ok := intToUintCmpOp(intOp)
	if !ok {
		if genOp != 0 {
			program.Emit(c.Function, genOp, dest, left.Register, right.Register)
		}
		return
	}
	program.Emit(c.Function, uintCmpOp, dest, left.Register, right.Register)
}

// emitComplexCompare emits isa.OpEqualComplex128 or isa.OpNotEqualComplex128 into dest.
// Returns an error for ordering operators because complex numbers support only == and !=
// in Go.
//
// Takes intOp (isa.Opcode) which is the int-bank comparison opcode determining equality
// or inequality.
// Takes dest (uint8) which is the destination int register receiving the 0/1 result.
// Takes left (program.VarLocation) which is the left complex operand register location.
// Takes right (program.VarLocation) which is the right complex operand register location.
//
// Returns a nil error for == and !=, or an error for ordering operators.
func (c *Compiler) emitComplexCompare(_ context.Context, intOp isa.Opcode, dest uint8, left, right program.VarLocation) error {
	switch intOp {
	case isa.OpEqualInt64:
		program.Emit(c.Function, isa.OpEqualComplex128, dest, left.Register, right.Register)
	case isa.OpNotEqualInt64:
		program.Emit(c.Function, isa.OpNotEqualComplex128, dest, left.Register, right.Register)
	default:
		return fault.ErrCompileCompareComplexOrdering
	}
	return nil
}

// intOnlyDestination returns into's register when it matches kind, or a fresh register
// otherwise.
//
// Takes kind (isa.RegisterKind) which is the result's bank.
// Takes into (*program.VarLocation) which is the preferred register, or nil.
//
// Returns uint8 which is the destination register.
func (c *Compiler) intOnlyDestination(kind isa.RegisterKind, into *program.VarLocation) uint8 {
	if into != nil && into.Kind == kind {
		return into.Register
	}
	return c.Scopes.Alloc.Alloc(kind)
}

// emitIntOnlyOp emits a bitwise or shift operation that requires integer operands. uint
// operands are routed to their uint-specific opcode; mixed int/uint operand pairs are
// bridged through an IntToUint or UintToInt temporary so the dispatched instruction
// receives matching banks.
//
// Takes op (isa.Opcode) which is the int-bank opcode to Emit (or whose uint counterpart
// applies).
// Takes left (program.VarLocation) which is the left operand register location.
// Takes right (program.VarLocation) which is the right operand register location.
// Takes into (*program.VarLocation) which is a register of the result's bank to write the
// result into, or nil for a fresh one.
//
// Returns the register location holding the result, or an error when the operand banks
// are unsupported for the operation.
func (c *Compiler) emitIntOnlyOp(_ context.Context, op isa.Opcode, left, right program.VarLocation, into *program.VarLocation) (program.VarLocation, error) {
	if left.Kind == isa.RegisterUint64 {
		var uintOp isa.Opcode
		switch op {
		case isa.OpBitAndInt64:
			uintOp = isa.OpBitAndUint64
		case isa.OpBitOrInt64:
			uintOp = isa.OpBitOrUint64
		case isa.OpBitXorInt64:
			uintOp = isa.OpBitXorUint64
		case isa.OpBitAndNotInt64:
			uintOp = isa.OpBitAndNotUint64
		case isa.OpShiftLeftInt64:
			uintOp = isa.OpShiftLeftUint64
		case isa.OpShiftRightInt64:
			uintOp = isa.OpShiftRightUint64
		default:
			return program.VarLocation{}, fault.ErrCompileArithUintUnsupported
		}
		dest := c.intOnlyDestination(isa.RegisterUint64, into)
		rightRegister := right.Register
		if right.Kind == isa.RegisterInt64 {
			rightRegister = c.Scopes.Alloc.AllocTemp(isa.RegisterUint64)
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertInt64ToUint64), rightRegister, right.Register)
		}
		program.Emit(c.Function, uintOp, dest, left.Register, rightRegister)
		if right.Kind == isa.RegisterInt64 {
			c.Scopes.Alloc.FreeTemp(isa.RegisterUint64, rightRegister)
		}
		return program.VarLocation{Register: dest, Kind: isa.RegisterUint64}, nil
	}
	if left.Kind != isa.RegisterInt64 {
		return program.VarLocation{}, fault.ErrCompileBitwiseRequiresInteger
	}
	dest := c.intOnlyDestination(isa.RegisterInt64, into)
	rightRegister := right.Register
	if right.Kind == isa.RegisterUint64 {
		rightRegister = c.Scopes.Alloc.AllocTemp(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), rightRegister, right.Register)
	}
	program.Emit(c.Function, op, dest, left.Register, rightRegister)
	if right.Kind == isa.RegisterUint64 {
		c.Scopes.Alloc.FreeTemp(isa.RegisterInt64, rightRegister)
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}, nil
}

// ensureIntRegister converts location to the int bank in place.
//
// Takes location (*VarLocation) which is the register to coerce.
func (c *Compiler) ensureIntRegister(_ context.Context, location *program.VarLocation) {
	if location.Kind == isa.RegisterInt64 {
		return
	}
	switch location.Kind {
	case isa.RegisterUint64:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertUint64ToInt64), dest, location.Register)
		location.Register = dest
		location.Kind = isa.RegisterInt64
	case isa.RegisterGeneral:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpUnboxInt64), dest, location.Register)
		location.Register = dest
		location.Kind = isa.RegisterInt64
	case isa.RegisterBool:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), dest, location.Register)
		location.Register = dest
		location.Kind = isa.RegisterInt64
	case isa.RegisterFloat64:
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertFloat64ToInt64), dest, location.Register)
		location.Register = dest
		location.Kind = isa.RegisterInt64
	default:
	}
}

// ensureIntForBranch returns location as an int register suitable for isa.OpJumpIfFalse /
// isa.OpJumpIfTrue. Bool locations are converted via isa.SubOpConvertBoolToInt64.
//
// Takes location (program.VarLocation) which is the register location to coerce to the
// int bank.
//
// Returns an int-bank register location holding the branch-ready value.
func (c *Compiler) ensureIntForBranch(_ context.Context, location program.VarLocation) program.VarLocation {
	if location.Kind == isa.RegisterInt64 {
		return location
	}
	if location.Kind == isa.RegisterBool {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), dest, location.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}
	}
	if location.Kind == isa.RegisterGeneral {
		booleanRegister := c.Scopes.Alloc.Alloc(isa.RegisterBool)
		program.Emit(c.Function, isa.OpUnboxFromInterface, booleanRegister, location.Register, uint8(isa.RegisterBool))
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt64)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpConvertBoolToInt64), dest, booleanRegister)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt64}
	}
	return location
}

// emitArithmetic emits the arithmetic instruction for op on left and right.
//
// The opcode and result kind follow the left operand's kind via
// isaselect.ResolveArithOpcode(). The result lands in dest when dest is non-nil and of
// that kind, else in a fresh register.
//
// Takes op (token.Token) which is one of + - * / %.
// Takes left (program.VarLocation) which is the left operand.
// Takes right (program.VarLocation) which is the right operand.
// Takes dest (*program.VarLocation) which is the preferred destination, or nil.
//
// Returns program.VarLocation which holds the result.
// Returns error when the left operand's kind does not support op.
func (c *Compiler) emitArithmetic(ctx context.Context, op token.Token, left, right program.VarLocation, dest *program.VarLocation) (program.VarLocation, error) {
	intOp, floatOp, strOp, genOp := isaselect.ArithmeticOpcodes(op)
	opcode, kind, err := isaselect.ResolveArithOpcode(intOp, floatOp, strOp, genOp, left.Kind)
	if err != nil {
		return program.VarLocation{}, err
	}
	if dest != nil && dest.Kind == kind {
		c.EmitTyped(ctx, opcode, *dest, left, right)
		return *dest, nil
	}
	location := program.VarLocation{Register: c.Scopes.Alloc.Alloc(kind), Kind: kind}
	c.EmitTyped(ctx, opcode, location, left, right)
	return location, nil
}

// floatNarrowingSubOp reports the rounding sub-op and register bank for a static type
// whose values are stored at a wider precision than Go gives them: float32 in the float
// bank and complex64 in the complex bank.
//
// Takes staticType (types.Type) which is the static Go type of the value.
//
// Returns the sub-op, the bank it operates on, and true; false for every other type.
func floatNarrowingSubOp(staticType types.Type) (isa.SubOpcode, isa.RegisterKind, bool) {
	if staticType == nil {
		return 0, 0, false
	}
	basic, ok := staticType.Underlying().(*types.Basic)
	if !ok {
		return 0, 0, false
	}
	switch basic.Kind() {
	case types.Float32:
		return isa.SubOpRoundToFloat32, isa.RegisterFloat64, true
	case types.Complex64:
		return isa.SubOpRoundToComplex64, isa.RegisterComplex128, true
	default:
		return 0, 0, false
	}
}

// roundConstantToType rounds a folded constant to the precision of its static type.
// go/types keeps float32 and complex64 constants exact, so without this a float32 literal
// would be loaded at float64 precision and differ from Go by one rounding.
//
// Takes value (constant.Value) which is the folded constant.
// Takes staticType (types.Type) which is the constant's static type.
//
// Returns the rounded constant, or value itself when the type needs no rounding.
func roundConstantToType(value constant.Value, staticType types.Type) constant.Value {
	if staticType == nil || value == nil {
		return value
	}
	basic, ok := staticType.Underlying().(*types.Basic)
	if !ok {
		return value
	}
	switch basic.Kind() {
	case types.Float32:
		if value.Kind() != constant.Int && value.Kind() != constant.Float {
			return value
		}
		rounded, _ := constant.Float32Val(value)
		return constant.MakeFloat64(float64(rounded))
	case types.Complex64:
		realPart, _ := constant.Float32Val(constant.Real(value))
		imaginaryPart, _ := constant.Float32Val(constant.Imag(value))
		return constant.BinaryOp(constant.MakeFloat64(float64(realPart)), token.ADD,
			constant.MakeImag(constant.MakeFloat64(float64(imaginaryPart))))
	default:
		return value
	}
}

// strictInterfaceComparison reports whether an == or != between the given static types
// must use strict interface equality.
//
// Takes leftType (types.Type) which is the left operand's static type.
// Takes rightType (types.Type) which is the right operand's static type.
//
// Returns true when at least one operand is an interface and the other is not the untyped
// nil literal.
func strictInterfaceComparison(leftType, rightType types.Type) bool {
	if leftType == nil || rightType == nil {
		return false
	}
	leftInterface := isInterfaceType(leftType)
	rightInterface := isInterfaceType(rightType)
	switch {
	case leftInterface && rightInterface:
		return true
	case leftInterface:
		return !isUntypedNil(rightType)
	case rightInterface:
		return !isUntypedNil(leftType)
	default:
		return false
	}
}

// isUntypedNil reports whether t is the type of the untyped nil literal.
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns true for untyped nil.
func isUntypedNil(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.UntypedNil
}

// isInterfaceType reports whether t's underlying type is an interface.
//
// Takes t (types.Type) which is the type to inspect.
//
// Returns true for interface types, including named interfaces.
func isInterfaceType(t types.Type) bool {
	_, ok := t.Underlying().(*types.Interface)
	return ok
}

// comparisonScalarKind returns the scalar register bank shared by both operands.
//
// It returns the bank and true when both static types are non-interface and collapse to
// the same scalar bank. It returns (isa.RegisterGeneral, false) when either operand is
// interface-typed or does not reduce to a scalar bank, so the comparison must run through
// the general bank.
//
// Takes leftType (types.Type) which is the left operand's static type.
// Takes rightType (types.Type) which is the right operand's static type.
//
// Returns the shared scalar register bank and true, or (isa.RegisterGeneral, false).
func comparisonScalarKind(leftType, rightType types.Type) (isa.RegisterKind, bool) {
	leftKind, leftOk := scalarComparableKind(leftType)
	rightKind, rightOk := scalarComparableKind(rightType)
	if !leftOk || !rightOk || leftKind != rightKind {
		return isa.RegisterGeneral, false
	}
	return leftKind, true
}

// scalarComparableKind maps a static type to the scalar register bank Go compares it in.
//
// It returns the bank and true when the type is non-interface and its underlying type is
// a basic scalar. A named scalar type such as `type Status int` resolves through its
// underlying basic kind. It returns (isa.RegisterGeneral, false) for interfaces,
// composites, and pointers, which the general bank compares.
//
// Takes t (types.Type) which is the operand's static type; nil returns false.
//
// Returns the scalar register bank and true, or (isa.RegisterGeneral, false).
func scalarComparableKind(t types.Type) (isa.RegisterKind, bool) {
	if t == nil {
		return isa.RegisterGeneral, false
	}
	underlying := t.Underlying()
	if _, isInterface := underlying.(*types.Interface); isInterface {
		return isa.RegisterGeneral, false
	}
	basic, isBasic := underlying.(*types.Basic)
	if !isBasic {
		return isa.RegisterGeneral, false
	}
	kind := typemap.KindForBasic(basic.Kind())
	if kind == isa.RegisterGeneral {
		return isa.RegisterGeneral, false
	}
	return kind, true
}

// comparisonOpcodes maps a comparison operator token to its bank-specific opcode set, and
// true. Non-comparison tokens return (zero set, false).
//
// Takes op (token.Token) which is the operator to classify.
//
// Returns the opcode set and true for comparison tokens, or (zero set, false).
func comparisonOpcodes(op token.Token) (comparisonOpcodeSet, bool) {
	switch op {
	case token.EQL:
		return comparisonOpcodeSet{intOp: isa.OpEqualInt64, floatOp: isa.OpEqualFloat64, strOp: isa.OpEqualString, genOp: isa.OpEqualGeneral}, true
	case token.NEQ:
		return comparisonOpcodeSet{intOp: isa.OpNotEqualInt64, floatOp: isa.OpNotEqualFloat64, strOp: isa.OpNotEqualString, genOp: isa.OpNotEqualGeneral}, true
	case token.LSS:
		return comparisonOpcodeSet{intOp: isa.OpLessThanInt64, floatOp: isa.OpLessThanFloat64, strOp: isa.OpLessThanString, genOp: isa.OpLessThanGeneral}, true
	case token.LEQ:
		return comparisonOpcodeSet{intOp: isa.OpLessOrEqualInt64, floatOp: isa.OpLessOrEqualFloat64, strOp: isa.OpLessOrEqualString, genOp: isa.OpLessOrEqualGeneral}, true
	case token.GTR:
		return comparisonOpcodeSet{intOp: isa.OpGreaterThanInt64, floatOp: isa.OpGreaterThanFloat64, strOp: isa.OpGreaterThanString, genOp: isa.OpGreaterThanGeneral}, true
	case token.GEQ:
		return comparisonOpcodeSet{intOp: isa.OpGreaterOrEqualInt64, floatOp: isa.OpGreaterOrEqualFloat64, strOp: isa.OpGreaterOrEqualString, genOp: isa.OpGreaterOrEqualGeneral}, true
	default:
		return comparisonOpcodeSet{}, false
	}
}

// scalarKindForConstant maps a go/constant kind to the scalar isa.RegisterKind used for
// loading the value, returning isa.RegisterGeneral for unknown kinds.
//
// Takes value (constant.Value) which is the constant.Value whose kind selects the bank.
//
// Returns the matching register bank, or isa.RegisterGeneral for unknown kinds.
func scalarKindForConstant(value constant.Value) isa.RegisterKind {
	switch value.Kind() {
	case constant.Bool:
		return isa.RegisterBool
	case constant.Int:
		return isa.RegisterInt64
	case constant.Float:
		return isa.RegisterFloat64
	case constant.String:
		return isa.RegisterString
	case constant.Complex:
		return isa.RegisterComplex128
	default:
		return isa.RegisterGeneral
	}
}

// intToUintCmpOp maps an int comparison opcode to its uint counterpart, returning (0,
// false) when no mapping exists.
//
// Takes intOp (isa.Opcode) which is the int-bank comparison opcode to translate.
//
// Returns the matching uint opcode and true, or (0, false) when no mapping exists.
func intToUintCmpOp(intOp isa.Opcode) (isa.Opcode, bool) {
	switch intOp {
	case isa.OpEqualInt64:
		return isa.OpEqualUint64, true
	case isa.OpNotEqualInt64:
		return isa.OpNotEqualUint64, true
	case isa.OpLessThanInt64:
		return isa.OpLessThanUint64, true
	case isa.OpLessOrEqualInt64:
		return isa.OpLessOrEqualUint64, true
	case isa.OpGreaterThanInt64:
		return isa.OpGreaterThanUint64, true
	case isa.OpGreaterOrEqualInt64:
		return isa.OpGreaterOrEqualUint64, true
	default:
		return 0, false
	}
}

// reusableShortCircuitResult reports whether the left operand is a short-circuit
// expression with a temporary result register that can be reused.
//
// Takes operand (ast.Expr) which is the left operand.
// Takes left (program.VarLocation) which is where its value is.
// Takes mark ([isa.NumRegisterKinds]uint32) which is the state before the operand.
//
// Returns bool which is true when the left register may be overwritten.
func reusableShortCircuitResult(operand ast.Expr, left program.VarLocation, mark [isa.NumRegisterKinds]uint32) bool {
	return isShortCircuitOperand(operand) && isDirectScalarRegister(left) && allocatedSince(left, mark)
}

// allocatedSince reports whether location's register was allocated after mark, so it
// holds a temporary of the expression being compiled.
//
// Takes location (program.VarLocation) which is a register location.
// Takes mark ([isa.NumRegisterKinds]uint32) which is an earlier allocation state.
//
// Returns bool which is true for a register at or above the mark.
func allocatedSince(location program.VarLocation, mark [isa.NumRegisterKinds]uint32) bool {
	return uint32(location.Register) >= mark[location.Kind]
}

// canHoldResultOf reports whether the right operand allocated a temporary register of
// kind after the left operand was evaluated.
//
// Takes right (program.VarLocation) which is the right operand.
// Takes kind (isa.RegisterKind) which is the left operand's bank.
// Takes afterLeft ([isa.NumRegisterKinds]uint32) which is the state after the left
// operand.
//
// Returns bool which is true when the result may overwrite the right operand.
func canHoldResultOf(right program.VarLocation, kind isa.RegisterKind, afterLeft [isa.NumRegisterKinds]uint32) bool {
	return isDirectScalarRegister(right) && right.Kind == kind && allocatedSince(right, afterLeft)
}

// binaryResultDestination selects a temporary operand register for a chained arithmetic
// or bitwise result.
//
// Operations outside a chain keep a fresh destination to preserve the instruction
// patterns used by later optimisation passes.
//
// Takes expression (*ast.BinaryExpr) which is the operation.
// Takes mark ([isa.NumRegisterKinds]uint32) which is the allocation state before the left
// operand was compiled.
// Takes afterLeft ([isa.NumRegisterKinds]uint32) which is the state after it.
// Takes left (program.VarLocation) which is the left operand.
// Takes right (program.VarLocation) which is the right operand.
//
// Returns *program.VarLocation which is the operand to overwrite, or nil for a fresh
// register.
func binaryResultDestination(expression *ast.BinaryExpr, mark, afterLeft [isa.NumRegisterKinds]uint32, left, right program.VarLocation) *program.VarLocation {
	if !isChainOperator(expression.Op) || !isDirectScalarRegister(left) {
		return nil
	}
	leftInChain, rightInChain := isChainOperand(expression.X), isChainOperand(expression.Y)
	if (leftInChain || rightInChain) && allocatedSince(left, mark) {
		return &left
	}
	if rightInChain && canHoldResultOf(right, left.Kind, afterLeft) {
		return &right
	}
	return nil
}

// isChainOperator reports whether op is an arithmetic or bitwise operator whose result a
// chain can accumulate in place.
//
// Takes op (token.Token) which is the operator.
//
// Returns bool which is true for + - * / % & | ^ &^ << >>.
func isChainOperator(op token.Token) bool {
	switch op {
	case token.ADD, token.SUB, token.MUL, token.QUO, token.REM,
		token.AND, token.OR, token.XOR, token.AND_NOT, token.SHL, token.SHR:
		return true
	default:
		return false
	}
}

// isChainOperand reports whether operand is itself an arithmetic or bitwise expression,
// so its value is a temporary the enclosing operation may overwrite.
//
// Takes operand (ast.Expr) which is one side of a binary expression.
//
// Returns bool which is true for an inner chain step.
func isChainOperand(operand ast.Expr) bool {
	inner, ok := ast.Unparen(operand).(*ast.BinaryExpr)
	return ok && isChainOperator(inner.Op)
}

// isShortCircuitOperand reports whether operand is itself an && or || expression, whose
// result register the enclosing one may reuse.
//
// Takes operand (ast.Expr) which is the left side of a short-circuit expression.
//
// Returns bool which is true for an inner && or ||.
func isShortCircuitOperand(operand ast.Expr) bool {
	inner, ok := ast.Unparen(operand).(*ast.BinaryExpr)
	return ok && (inner.Op == token.LAND || inner.Op == token.LOR)
}
