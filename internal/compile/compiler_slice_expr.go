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

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// compileSliceExpression compiles a[lo:hi] or a[lo:hi:max]. Dispatches to
// compileStringSlice when the operand is a string with no max bound; otherwise to
// compileGeneralSlice.
//
// Takes expression (*ast.SliceExpr) which is the slice expression.
//
// Returns the sliced location and any compilation error.
func (c *Compiler) compileSliceExpression(ctx context.Context, expression *ast.SliceExpr) (program.VarLocation, error) {
	collectionLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}

	collectionType, ok := c.underlyingTypeOf(expression.X)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for sliced collection at %s", fault.ErrCompilation, c.positionString(expression.X.Pos()))
	}
	if basic, ok := collectionType.(*types.Basic); ok && basic.Info()&types.IsString != 0 && expression.Max == nil {
		return c.compileStringSlice(ctx, expression, collectionLocation)
	}

	return c.compileGeneralSlice(ctx, expression, collectionLocation)
}

// compileStringSlice compiles s[lo:hi] for a string operand via isa.OpSliceString plus an
// isa.OpExt extension word carrying the low and high registers.
//
// Takes expression (*ast.SliceExpr) which is the slice expression.
// Takes collectionLocation (VarLocation) which is the string operand location.
//
// Returns the sliced string location and any compilation error.
func (c *Compiler) compileStringSlice(ctx context.Context, expression *ast.SliceExpr, collectionLocation program.VarLocation) (program.VarLocation, error) {
	dest := c.Scopes.Alloc.Alloc(isa.RegisterString)
	flags := uint8(0)
	var lowRegister, highRegister uint8

	if expression.Low != nil {
		reg, err := c.compileSliceBound(ctx, expression.Low, true)
		if err != nil {
			return program.VarLocation{}, err
		}
		lowRegister = reg
		flags |= isa.SliceLowBoundFlag
	}
	if expression.High != nil {
		reg, err := c.compileSliceBound(ctx, expression.High, true)
		if err != nil {
			return program.VarLocation{}, err
		}
		highRegister = reg
		flags |= isa.SliceHighBoundFlag
	}

	c.setDebugPosition(ctx, expression.Lbrack)
	program.Emit(c.Function, isa.OpSliceString, dest, collectionLocation.Register, flags)
	program.Emit(c.Function, isa.OpExt, lowRegister, highRegister, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterString}, nil
}

// compileGeneralSlice compiles a[lo:hi] or a[lo:hi:max] for a non-string collection via
// isa.SubOpSliceOp with one or two isa.OpExt extension words holding the bounds.
//
// Takes expression (*ast.SliceExpr) which is the slice expression.
// Takes collectionLocation (VarLocation) which is the collection operand location.
//
// Returns the sliced collection location and any compilation error.
func (c *Compiler) compileGeneralSlice(ctx context.Context, expression *ast.SliceExpr, collectionLocation program.VarLocation) (program.VarLocation, error) {
	if directOp, ok := isaselect.TypedSliceDirectSliceSliceSubOp(collectionLocation.Kind); ok {
		if result, ok, err := c.tryCompileTypedDirectSlice(ctx, expression, collectionLocation, directOp); ok || err != nil {
			return result, err
		}
	}
	c.boxToGeneral(ctx, &collectionLocation)

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	flags := uint8(0)
	var lowRegister, highRegister, maxRegister uint8

	if expression.Low != nil {
		reg, err := c.compileSliceBound(ctx, expression.Low, false)
		if err != nil {
			return program.VarLocation{}, err
		}
		lowRegister = reg
		flags |= isa.SliceLowBoundFlag
	}
	if expression.High != nil {
		reg, err := c.compileSliceBound(ctx, expression.High, false)
		if err != nil {
			return program.VarLocation{}, err
		}
		highRegister = reg
		flags |= isa.SliceHighBoundFlag
	}
	if expression.Max != nil {
		reg, err := c.compileSliceBound(ctx, expression.Max, false)
		if err != nil {
			return program.VarLocation{}, err
		}
		maxRegister = reg
		flags |= isa.SliceMaxBitFlag
	}

	c.setDebugPosition(ctx, expression.Lbrack)
	program.EmitTier1(c.Function, isa.SubOpSliceOp, dest, collectionLocation.Register)
	program.Emit(c.Function, isa.OpExt, flags, lowRegister, highRegister)
	if expression.Max != nil {
		program.Emit(c.Function, isa.OpExt, maxRegister, 0, 0)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// tryCompileTypedDirectSlice emits a typed-bank-direct sub-slice opcode.
//
// The opcode handles three-way slicing for collections on the typed-slice bank. The
// destination location stays on the same typed bank as the source, with no reflect
// boxing. Encoding mirrors isa.SubOpSliceByteSlice: an isa.OpDrillTier1 carrying directOp
// with destination/source in B/C, an isa.OpExt with flags plus low and high registers,
// and an optional trailing isa.OpExt carrying maxReg when isa.SliceMaxBitFlag is set.
//
// Takes expression (*ast.SliceExpr).
// Takes collectionLocation (VarLocation) which is the typed-bank source location.
// Takes directOp (isa.SubOpcode) which is the typed-direct sub-op for the bank.
//
// Returns the typed-bank destination location, ok=true when the opcode was emitted, and
// any error from compiling the bound expressions.
func (c *Compiler) tryCompileTypedDirectSlice(ctx context.Context, expression *ast.SliceExpr, collectionLocation program.VarLocation, directOp isa.SubOpcode) (program.VarLocation, bool, error) {
	dest := c.Scopes.Alloc.Alloc(collectionLocation.Kind)
	flags := uint8(0)
	var lowRegister, highRegister, maxRegister uint8

	if expression.Low != nil {
		reg, err := c.compileSliceBound(ctx, expression.Low, false)
		if err != nil {
			return program.VarLocation{}, false, err
		}
		lowRegister = reg
		flags |= isa.SliceLowBoundFlag
	}
	if expression.High != nil {
		reg, err := c.compileSliceBound(ctx, expression.High, false)
		if err != nil {
			return program.VarLocation{}, false, err
		}
		highRegister = reg
		flags |= isa.SliceHighBoundFlag
	}
	if expression.Max != nil {
		reg, err := c.compileSliceBound(ctx, expression.Max, false)
		if err != nil {
			return program.VarLocation{}, false, err
		}
		maxRegister = reg
		flags |= isa.SliceMaxBitFlag
	}

	program.Emit(c.Function, isa.OpDrillTier1, uint8(directOp), dest, collectionLocation.Register)
	program.Emit(c.Function, isa.OpExt, flags, lowRegister, highRegister)
	if expression.Max != nil {
		program.Emit(c.Function, isa.OpExt, maxRegister, 0, 0)
	}

	return program.VarLocation{Register: dest, Kind: collectionLocation.Kind}, true, nil
}

// compileSliceBound compiles a single slice bound expression and returns the register
// holding the result. When ensureInt is true the bound is coerced into the int bank
// (required by isa.OpSliceString).
//
// Takes boundExpr (ast.Expr) which is the bound expression.
// Takes ensureInt (bool) which selects int-bank coercion.
//
// Returns the register holding the bound and any compilation error.
func (c *Compiler) compileSliceBound(ctx context.Context, boundExpr ast.Expr, ensureInt bool) (uint8, error) {
	location, err := c.compileExpression(ctx, boundExpr)
	if err != nil {
		return 0, err
	}
	if ensureInt {
		c.ensureIntRegister(ctx, &location)
	}
	return location.Register, nil
}

// tryCompileSliceIndexStructField fuses `slice[i].field` into a single
// opSliceIndexStructFieldXxx when the slice element type is a struct and the leaf field
// maps to a fast-path register kind.
//
// Takes expression (*ast.SelectorExpr) which carries the field selection.
// Takes selection (*types.Selection) which provides the field's type and index path.
//
// Returns (location, true, nil) on a successful fusion; (zero, false, nil) when the AST
// shape does not match; or (zero, false, err) when sub-expression compilation fails.
func (c *Compiler) tryCompileSliceIndexStructField(ctx context.Context, expression *ast.SelectorExpr, selection *types.Selection) (program.VarLocation, bool, error) {
	classified, eligible := c.classifySliceIndexStructField(ctx, expression, selection)
	if !eligible {
		return program.VarLocation{}, false, nil
	}
	sliceLocation, err := c.compileExpression(ctx, classified.indexExpr.X)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	c.boxToGeneral(ctx, &sliceLocation)

	indexLocation, err := c.compileExpression(ctx, classified.indexExpr.Index)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	if indexLocation.Kind != isa.RegisterInt {
		return program.VarLocation{}, false, nil
	}

	dest := c.Scopes.Alloc.Alloc(classified.resultKind)
	program.Emit(c.Function, classified.op, dest, sliceLocation.Register, indexLocation.Register)
	c.emitStructFieldLayoutExtension(classified.layoutIdx)
	return program.VarLocation{Register: dest, Kind: classified.resultKind}, true, nil
}

// classifySliceIndexStructField inspects an AST `slice[i].Field` selector and reports
// whether the fused slice-index-struct-field fast path applies.
//
// Eligibility requires the selector receiver to be an indexExpr over a slice of struct,
// the reflect slice element type to be a struct, the leaf-field kind to be on the
// fast-path allowlist, and the layout to resolve with a matching opcode.
//
// Takes expression (*ast.SelectorExpr) which is the selector node.
// Takes selection (*types.Selection) which is the type-checker's resolution of the
// selector.
//
// Returns the classification populated with the resolved opcode and layout, and true when
// the fused fast path applies; false otherwise.
func (c *Compiler) classifySliceIndexStructField(ctx context.Context, expression *ast.SelectorExpr, selection *types.Selection) (sliceIndexStructFieldClassification, bool) {
	indexExpr, ok := expression.X.(*ast.IndexExpr)
	if !ok {
		return sliceIndexStructFieldClassification{}, false
	}
	collectionTypeAndValue, ok := c.Info.Types[indexExpr.X]
	if !ok {
		return sliceIndexStructFieldClassification{}, false
	}
	sliceType, ok := collectionTypeAndValue.Type.Underlying().(*types.Slice)
	if !ok {
		return sliceIndexStructFieldClassification{}, false
	}
	if _, isStructElement := sliceType.Elem().Underlying().(*types.Struct); !isStructElement {
		return sliceIndexStructFieldClassification{}, false
	}
	reflectSliceType := c.TypeToReflect(ctx, collectionTypeAndValue.Type)
	if reflectSliceType == nil || reflectSliceType.Kind() != reflect.Slice ||
		reflectSliceType.Elem().Kind() != reflect.Struct {
		return sliceIndexStructFieldClassification{}, false
	}
	resultKind := c.kindFor(selection.Type())
	if !isaselect.StructFieldFastPathKindEnabled(resultKind) || resultKind == isa.RegisterGeneral {
		return sliceIndexStructFieldClassification{}, false
	}
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok {
		return sliceIndexStructFieldClassification{}, false
	}
	op, hasOp := isaselect.PickSliceIndexStructFieldOp(resultKind)
	if !hasOp {
		return sliceIndexStructFieldClassification{}, false
	}
	return sliceIndexStructFieldClassification{
		indexExpr:  indexExpr,
		op:         op,
		resultKind: resultKind,
		layoutIdx:  layoutIdx,
	}, true
}
