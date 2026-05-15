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
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/isa"
)

// compileLiteralElement compiles a single composite-literal element with typed-nil
// awareness so that bare `nil` keeps its concrete type tag when the element/key/value
// type is a pointer, slice, map, chan or function. Falls through to compileExpression for
// every other shape.
//
// Takes element (ast.Expr) which is the element expression.
// Takes expectedType (types.Type) which is the static target type at the element
// position.
//
// Returns the compiled location and any compilation error.
func (c *Compiler) compileLiteralElement(ctx context.Context, element ast.Expr, expectedType types.Type) (program.VarLocation, error) {
	if expectedType != nil {
		if location, handled, err := c.compileTypedNilOrExpression(ctx, element, expectedType); err != nil {
			return program.VarLocation{}, err
		} else if handled {
			return location, nil
		}
	}
	return c.compileExpression(ctx, element)
}

// literalElementType returns the static Go type at element index 0 of the composite
// literal. Used to feed compileLiteralElement so bare `nil` keeps its concrete typing.
//
// Takes lit (*ast.CompositeLit) whose declared container type drives the lookup.
//
// Returns the element Go type, or nil when go/types cannot resolve the container's type
// or its underlying kind has no element notion.
func (c *Compiler) literalElementType(lit *ast.CompositeLit) types.Type {
	if c.Info == nil {
		return nil
	}
	tv, ok := c.Info.Types[lit]
	if !ok || tv.Type == nil {
		return nil
	}
	switch container := tv.Type.Underlying().(type) {
	case *types.Slice:
		return container.Elem()
	case *types.Array:
		return container.Elem()
	case *types.Map:
		return container.Elem()
	default:
		return nil
	}
}

// literalKeyType returns the static key type of a map literal.
//
// Takes lit (*ast.CompositeLit) which is the candidate literal.
//
// Returns types.Type which is the map key type, or nil for non-map literals.
func (c *Compiler) literalKeyType(lit *ast.CompositeLit) types.Type {
	if c.Info == nil {
		return nil
	}
	tv, ok := c.Info.Types[lit]
	if !ok || tv.Type == nil {
		return nil
	}
	if container, isMap := tv.Type.Underlying().(*types.Map); isMap {
		return container.Key()
	}
	return nil
}

// compileCompositeLit compiles a composite literal (slice, map, struct).
//
// Takes lit (*ast.CompositeLit) which is the AST composite literal node to compile.
//
// Returns VarLocation holding the compiled literal value and any compilation error.
func (c *Compiler) compileCompositeLit(ctx context.Context, lit *ast.CompositeLit) (program.VarLocation, error) {
	tv := c.Info.Types[lit]
	reflectType := c.TypeToReflect(ctx, c.substitutedType(tv.Type))

	switch reflectType.Kind() {
	case reflect.Slice:
		return c.compileSliceLiteral(ctx, lit, reflectType)
	case reflect.Array:
		return c.compileArrayLiteral(ctx, lit, reflectType)
	case reflect.Map:
		return c.compileMapLiteral(ctx, lit, reflectType)
	case reflect.Struct:
		return c.compileStructLiteral(ctx, lit, reflectType)
	case reflect.Pointer:
		return c.compilePointerCompositeLit(ctx, lit, reflectType)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported composite literal type: %v (%v) at %s", reflectType.Kind(), reflectType, c.positionString(lit.Pos()))
	}
}

// compileArrayLiteral compiles an array literal like [5]int{2, 4, 6, 8, 10}.
//
// Takes lit (*ast.CompositeLit) which is the AST composite literal node.
// Takes reflectType (reflect.Type) which is the reflect.Type of the array.
//
// Returns VarLocation holding the compiled array and any compilation error.
func (c *Compiler) compileArrayLiteral(ctx context.Context, lit *ast.CompositeLit, reflectType reflect.Type) (program.VarLocation, error) {
	if c.maxLiteralElements > 0 && len(lit.Elts) > c.maxLiteralElements {
		return program.VarLocation{}, fmt.Errorf("%w: %d elements exceeds limit %d at %s",
			fault.ErrLiteralElementLimit, len(lit.Elts), c.maxLiteralElements, c.positionString(lit.Lbrace))
	}
	zeroValue := reflect.New(reflectType).Elem()
	constIndex, err := program.AddGeneralConstant(c.Function, zeroValue, descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantCompositeZero,
		TypeDescriptor: descriptor.ReflectTypeToDescriptor(reflectType), PackagePath: "", SymbolName: ""})
	if err != nil {
		return program.VarLocation{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitWide(c.Function, isa.OpLoadGeneralConst, dest, constIndex)

	elementType := c.literalElementType(lit)
	cursor := 0
	for _, elt := range lit.Elts {
		watermark := c.Scopes.Alloc.Snapshot()
		elementExpr, index, indexErr := c.resolveKeyedLiteralIndex(elt, cursor)
		if indexErr != nil {
			return program.VarLocation{}, indexErr
		}
		cursor = index + 1
		elementLocation, exprErr := c.compileLiteralElement(ctx, elementExpr, elementType)
		if exprErr != nil {
			return program.VarLocation{}, exprErr
		}
		elementLocation = c.coerceEvalBoolResult(ctx, c.Info, elementExpr, elementLocation)

		idxConst, idxErr := program.AddIntConstant(c.Function, int64(index))
		if idxErr != nil {
			return program.VarLocation{}, idxErr
		}
		indexRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.EmitWide(c.Function, isa.OpLoadIntConst, indexRegister, idxConst)

		if elementLocation.Kind != isa.RegisterGeneral {
			generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
			c.emitBoxElementToGeneral(ctx, generalRegister, elementLocation)
			program.Emit(c.Function, isa.OpIndexSet, dest, indexRegister, generalRegister)
			c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
		} else {
			program.Emit(c.Function, isa.OpIndexSet, dest, indexRegister, elementLocation.Register)
		}

		c.Scopes.Alloc.FreeTemp(isa.RegisterInt, indexRegister)
		c.Scopes.RestoreWatermark(watermark)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// emitSliceLiteralMake allocates the slice a literal fills, sized by its highest element
// index, into a fresh general register.
//
// Takes lit (*ast.CompositeLit) which is the slice literal.
// Takes typeIndex (uint16) which is the slice type's type-table index.
//
// Returns the destination register and any error from a malformed key.
func (c *Compiler) emitSliceLiteralMake(lit *ast.CompositeLit, typeIndex uint16) (uint8, error) {
	literalLength, err := c.keyedLiteralLength(lit)
	if err != nil {
		return 0, err
	}
	lenIndex, err := program.AddIntConstant(c.Function, int64(literalLength))
	if err != nil {
		return 0, err
	}
	lengthRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
	program.EmitWide(c.Function, isa.OpLoadIntConst, lengthRegister, lenIndex)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpMakeSlice, dest, lengthRegister, lengthRegister)
	program.EmitExtension(c.Function, typeIndex, 0)
	c.Scopes.Alloc.FreeTemp(isa.RegisterInt, lengthRegister)
	return dest, nil
}

// keyedLiteralLength returns the length of a slice literal: one past its highest element
// index, which keyed elements (`[]T{7: x}`) can push beyond the element count.
//
// Takes lit (*ast.CompositeLit) which is the slice literal.
//
// Returns the length and any error from a malformed key.
func (c *Compiler) keyedLiteralLength(lit *ast.CompositeLit) (int, error) {
	length := 0
	cursor := 0
	for _, elt := range lit.Elts {
		_, index, err := c.resolveKeyedLiteralIndex(elt, cursor)
		if err != nil {
			return 0, err
		}
		cursor = index + 1
		length = max(length, cursor)
	}
	return length, nil
}

// resolveKeyedLiteralIndex returns the destination index for an element.
//
// Plain elements use the running cursor as their index. *ast.KeyValueExpr forms (sparse
// `index: value` syntax from `[10]int{0:1, 5:7}`) use the keyed index instead. Unkeyed
// elements that follow a keyed one continue from `key+1` per Go's composite-literal index
// semantics.
//
// Takes element (ast.Expr) which is the literal element to inspect.
// Takes cursor (int) which is the next implicit-index position.
//
// Returns the underlying value expression to compile, the index it should land at, and
// any error when the key is not a constant integer expression.
func (c *Compiler) resolveKeyedLiteralIndex(element ast.Expr, cursor int) (ast.Expr, int, error) {
	kv, ok := element.(*ast.KeyValueExpr)
	if !ok {
		return element, cursor, nil
	}
	if c.Info == nil {
		return kv.Value, cursor, fmt.Errorf("composite literal index has no type info at %s", c.positionString(kv.Key.Pos()))
	}
	tv, hasType := c.Info.Types[kv.Key]
	if !hasType || tv.Value == nil {
		return kv.Value, cursor, fmt.Errorf("composite literal index must be a constant at %s", c.positionString(kv.Key.Pos()))
	}
	keyInt, ok := constant.Int64Val(tv.Value)
	if !ok {
		return kv.Value, cursor, fmt.Errorf("composite literal index out of range at %s", c.positionString(kv.Key.Pos()))
	}
	return kv.Value, int(keyInt), nil
}

// compileSliceLiteral compiles a slice literal like []int{1, 2, 3}.
//
// Takes lit (*ast.CompositeLit) which is the AST composite literal node.
// Takes reflectType (reflect.Type) which is the reflect.Type of the slice.
//
// Returns VarLocation holding the compiled slice and any compilation error.
func (c *Compiler) compileSliceLiteral(ctx context.Context, lit *ast.CompositeLit, reflectType reflect.Type) (program.VarLocation, error) {
	if c.maxLiteralElements > 0 && len(lit.Elts) > c.maxLiteralElements {
		return program.VarLocation{}, fmt.Errorf("%w: %d elements exceeds limit %d at %s",
			fault.ErrLiteralElementLimit, len(lit.Elts), c.maxLiteralElements, c.positionString(lit.Lbrace))
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}

	dest, err := c.emitSliceLiteralMake(lit, typeIndex)
	if err != nil {
		return program.VarLocation{}, err
	}

	elementType := c.literalElementType(lit)
	cursor := 0
	for _, elt := range lit.Elts {
		watermark := c.Scopes.Alloc.Snapshot()
		elementExpr, index, indexErr := c.resolveKeyedLiteralIndex(elt, cursor)
		if indexErr != nil {
			return program.VarLocation{}, indexErr
		}
		cursor = index + 1
		elementLocation, exprErr := c.compileLiteralElement(ctx, elementExpr, elementType)
		if exprErr != nil {
			return program.VarLocation{}, exprErr
		}
		elementLocation = c.coerceEvalBoolResult(ctx, c.Info, elementExpr, elementLocation)

		idxConst, idxErr := program.AddIntConstant(c.Function, int64(index))
		if idxErr != nil {
			return program.VarLocation{}, idxErr
		}
		indexRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterInt)
		program.EmitWide(c.Function, isa.OpLoadIntConst, indexRegister, idxConst)

		if elementLocation.Kind != isa.RegisterGeneral {
			generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
			c.emitBoxElementToGeneral(ctx, generalRegister, elementLocation)
			elementLocation = program.VarLocation{Register: generalRegister, Kind: isa.RegisterGeneral}
			program.Emit(c.Function, isa.OpIndexSet, dest, indexRegister, elementLocation.Register)
			c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
		} else {
			program.Emit(c.Function, isa.OpIndexSet, dest, indexRegister, elementLocation.Register)
		}

		c.Scopes.Alloc.FreeTemp(isa.RegisterInt, indexRegister)
		c.Scopes.RestoreWatermark(watermark)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileMapLiteral compiles a map literal like map[string]int{"a": 1}.
//
// Takes lit (*ast.CompositeLit) which is the AST composite literal node.
// Takes reflectType (reflect.Type) which is the reflect.Type of the map.
//
// Returns VarLocation holding the compiled map and any compilation error.
func (c *Compiler) compileMapLiteral(ctx context.Context, lit *ast.CompositeLit, reflectType reflect.Type) (program.VarLocation, error) {
	if c.maxLiteralElements > 0 && len(lit.Elts) > c.maxLiteralElements {
		return program.VarLocation{}, fmt.Errorf("%w: %d elements exceeds limit %d at %s",
			fault.ErrLiteralElementLimit, len(lit.Elts), c.maxLiteralElements, c.positionString(lit.Lbrace))
	}
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.EmitTier2(c.Function, isa.SubOpTier2MakeMap, dest)
	program.EmitExtension(c.Function, typeIndex, mapSizeHintLog2(len(lit.Elts)))

	keyType := c.literalKeyType(lit)
	valueType := c.literalElementType(lit)
	for _, elt := range lit.Elts {
		watermark := c.Scopes.Alloc.Snapshot()
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return program.VarLocation{}, fault.ErrCompileMapLiteralExpectKeyValue
		}

		keyLocation, err := c.compileLiteralElement(ctx, kv.Key, keyType)
		if err != nil {
			return program.VarLocation{}, err
		}
		keyLocation = c.coerceEvalBoolResult(ctx, c.Info, kv.Key, keyLocation)
		valueLocation, err := c.compileLiteralElement(ctx, kv.Value, valueType)
		if err != nil {
			return program.VarLocation{}, err
		}
		valueLocation = c.coerceEvalBoolResult(ctx, c.Info, kv.Value, valueLocation)

		c.boxElementToGeneralTemp(ctx, &keyLocation)
		c.boxElementToGeneralTemp(ctx, &valueLocation)

		program.Emit(c.Function, isa.OpMapSet, dest, keyLocation.Register, valueLocation.Register)
		c.Scopes.RestoreWatermark(watermark)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compilePointerCompositeLit compiles a composite literal whose type is a pointer, as
// produced by elided forms such as map[K]*T{"k": {...}} or []*T{{...}} where the inner
// literal is sugar for &T{...}.
//
// Takes lit (*ast.CompositeLit) which is the AST composite literal node.
// Takes reflectType (reflect.Type) which is the pointer reflect.Type recorded for lit by
// the go/types checker.
//
// Returns VarLocation holding the pointer value and any compilation error.
func (c *Compiler) compilePointerCompositeLit(ctx context.Context, lit *ast.CompositeLit, reflectType reflect.Type) (program.VarLocation, error) {
	elementType := reflectType.Elem()
	var elementLocation program.VarLocation
	var err error
	switch elementType.Kind() {
	case reflect.Struct:
		elementLocation, err = c.compileStructLiteral(ctx, lit, elementType)
	case reflect.Array:
		elementLocation, err = c.compileArrayLiteral(ctx, lit, elementType)
	case reflect.Slice:
		elementLocation, err = c.compileSliceLiteral(ctx, lit, elementType)
	case reflect.Map:
		elementLocation, err = c.compileMapLiteral(ctx, lit, elementType)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported composite literal type: %v (%v) at %s", reflectType.Kind(), reflectType, c.positionString(lit.Pos()))
	}
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAddr, dest, elementLocation.Register, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}
