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
	"math/bits"
	"reflect"

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/engine"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// maxMapSizeHintLog2 caps the log2 map-size hint encoded in isa.SubOpTier2MakeMap's
	// extension word. Values above 31 cannot be encoded (1 << 31 exceeds the addressable map
	// slot space we model).
	maxMapSizeHintLog2 = 31
)

// compileBuiltinCall compiles a call to a built-in function.
//
// Takes name (string) which is the name of the built-in function to dispatch on.
// Takes expression (*ast.CallExpr) which is the AST call expression.
//
// Returns VarLocation holding the built-in call result and any compilation error.
func (c *Compiler) compileBuiltinCall(ctx context.Context, name string, expression *ast.CallExpr) (program.VarLocation, error) {
	switch name {
	case "len":
		return c.compileBuiltinLen(ctx, expression)
	case "append":
		return c.compileBuiltinAppend(ctx, expression)
	case "make":
		return c.compileBuiltinMake(ctx, expression)
	case "delete":
		return c.compileBuiltinDelete(ctx, expression)
	case "cap":
		return c.compileBuiltinCap(ctx, expression)
	case "copy":
		return c.compileBuiltinCopy(ctx, expression)
	case "new":
		return c.compileBuiltinNew(ctx, expression)
	case "panic", "recover", "close":
		return c.compileBuiltinFeatureGated(ctx, name, expression)
	case "print":
		return c.compileBuiltinPrint(ctx, expression, isa.BuiltinPrint)
	case "println":
		return c.compileBuiltinPrint(ctx, expression, isa.BuiltinPrintln)
	case "min":
		return c.compileBuiltinMinMax(ctx, expression, true)
	case "max":
		return c.compileBuiltinMinMax(ctx, expression, false)
	case "clear":
		return c.compileBuiltinClear(ctx, expression)
	case "real":
		return c.compileBuiltinReal(ctx, expression)
	case "imag":
		return c.compileBuiltinImag(ctx, expression)
	case "complex":
		return c.compileBuiltinComplex(ctx, expression)
	default:
		return program.VarLocation{}, fmt.Errorf("unsupported built-in: %s at %s", name, c.positionString(expression.Pos()))
	}
}

// compileBuiltinFeatureGated compiles built-in calls that require a feature gate check
// before compilation (panic, recover, close).
//
// Takes name (string) which is the built-in function name.
// Takes expression (*ast.CallExpr) which is the AST call expression to compile.
//
// Returns VarLocation holding the call result and any compilation error.
func (c *Compiler) compileBuiltinFeatureGated(ctx context.Context, name string, expression *ast.CallExpr) (program.VarLocation, error) {
	switch name {
	case "panic":
		if err := c.checkFeature(policy.InterpFeaturePanicRecover, expression.Lparen); err != nil {
			return program.VarLocation{}, err
		}
		return c.compileBuiltinPanic(ctx, expression)
	case "recover":
		if err := c.checkFeature(policy.InterpFeaturePanicRecover, expression.Lparen); err != nil {
			return program.VarLocation{}, err
		}
		return c.compileBuiltinRecover(ctx, expression)
	default:
		if err := c.checkFeature(policy.InterpFeatureChannels, expression.Lparen); err != nil {
			return program.VarLocation{}, err
		}
		return c.compileBuiltinClose(ctx, expression)
	}
}

// compileBuiltinLen compiles len(x).
//
// Takes expression (*ast.CallExpr) which is the len() call expression.
//
// Returns VarLocation holding the length and any compilation error.
func (c *Compiler) compileBuiltinLen(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileBuiltinLenArgCount
	}

	if location, emitted, err := c.tryCompileStructFieldSliceLen(ctx, expression.Args[0]); err != nil {
		return program.VarLocation{}, err
	} else if emitted {
		return location, nil
	}

	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	switch argumentLocation.Kind {
	case isa.RegisterString:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLenString), dest, argumentLocation.Register)
	case isa.RegisterGeneral:
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpLen), dest, argumentLocation.Register)
	default:
		if directLenSubOp, ok := isaselect.TypedSliceDirectLenSubOp(argumentLocation.Kind); ok {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(directLenSubOp), dest, argumentLocation.Register)
			return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
		}
		return program.VarLocation{}, fmt.Errorf("len not supported for register kind %s", argumentLocation.Kind)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
}

// tryCompileStructFieldSliceLen compiles len(recv.sliceField) to the fused
// isa.OpGetStructFieldSliceLen, which reads the header's length word in place instead of
// snapshotting a 24-byte slice header into the register arena and dispatching a separate
// LEN. All static gates run before any bytecode is emitted so a refusal never leaves
// partial receiver code behind.
//
// Takes argument (ast.Expr) which is the single len() argument.
//
// Returns the int result location, whether the fused op was emitted, and any compilation
// error from compiling the receiver.
func (c *Compiler) tryCompileStructFieldSliceLen(ctx context.Context, argument ast.Expr) (program.VarLocation, bool, error) {
	selector, ok := ast.Unparen(argument).(*ast.SelectorExpr)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	selection, ok := c.Info.Selections[selector]
	if !ok || selection.Kind() != types.FieldVal {
		return program.VarLocation{}, false, nil
	}
	leafType := types.Unalias(selection.Type())
	if _, isSlice := leafType.Underlying().(*types.Slice); !isSlice {
		return program.VarLocation{}, false, nil
	}
	if _, isNamed := leafType.(*types.Named); isNamed && !namedTypeHasNoMethods(leafType) {
		return program.VarLocation{}, false, nil
	}
	receiverType := c.Info.Types[selector.X].Type
	if receiverType == nil || c.kindFor(receiverType) != isa.RegisterGeneral {
		return program.VarLocation{}, false, nil
	}
	layoutIdx, layoutOK := c.tryResolveStructFieldLayout(ctx, selection)
	if !layoutOK || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		return program.VarLocation{}, false, nil
	}

	receiverLocation, err := c.compileExpression(ctx, selector.X)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	if receiverLocation.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, false, fmt.Errorf(
			"%w: struct-field slice len receiver compiled to %s, expected general at %s",
			fault.ErrCompilation,
			receiverLocation.Kind,
			c.positionString(selector.Pos()),
		)
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpGetStructFieldSliceLen, dest, receiverLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, true, nil
}

// compileBuiltinAppend compiles append(slice, elems...).
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the append call.
//
// Returns VarLocation holding the resulting slice and any compilation error.
func (c *Compiler) compileBuiltinAppend(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) < 1 {
		return program.VarLocation{}, fault.ErrCompileBuiltinAppendArgCount
	}

	sliceLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	sliceType := c.Info.Types[expression.Args[0]].Type
	typedAppendSubOp, typedAppendKind := c.pickTypedAppendVariant(sliceType)

	lastArgIndex := len(expression.Args) - 1
	for i := 1; i < len(expression.Args); i++ {
		location, err := c.compileExpression(ctx, expression.Args[i])
		if err != nil {
			return program.VarLocation{}, err
		}
		location = c.coerceEvalBoolResult(ctx, c.Info, expression.Args[i], location)
		isSpread := i == lastArgIndex && expression.Ellipsis != token.NoPos
		sliceLocation = c.emitAppendStep(ctx, sliceLocation, location, isSpread, typedAppendSubOp, typedAppendKind, sliceType)
	}

	return sliceLocation, nil
}

// pickTypedAppendVariant selects the tier-1 typed-append sub-op (and the matching
// register kind) for the slice's element type, or returns (0, 0) when no typed variant
// applies.
//
// Takes sliceType (types.Type) which is the static type of the destination slice.
//
// Returns the tier-1 sub-opcode, or 0 when not eligible.
//
// Returns the matching element register kind.
func (*Compiler) pickTypedAppendVariant(sliceType types.Type) (isa.SubOpcode, isa.RegisterKind) {
	if sliceType == nil {
		return 0, 0
	}
	sliceValue, ok := sliceType.Underlying().(*types.Slice)
	if !ok {
		return 0, 0
	}

	basic, ok := types.Unalias(sliceValue.Elem()).(*types.Basic)
	if !ok {
		return 0, 0
	}
	switch basic.Kind() {
	case types.Int, types.Int64:
		return isa.SubOpAppendInt, isa.RegisterInt
	case types.String:
		return isa.SubOpAppendString, isa.RegisterString
	case types.Float64:
		return isa.SubOpAppendFloat, isa.RegisterFloat
	case types.Bool:
		return isa.SubOpAppendBool, isa.RegisterBool
	case types.Uint, types.Uint64, types.Uint8:
		return isa.SubOpAppendUint, isa.RegisterUint
	default:
	}
	return 0, 0
}

// emitAppendStep emits the opcode for a single argument of append.
//
// Takes sliceLocation (VarLocation) which is the current destination slice (chained
// across arguments).
// Takes location (VarLocation) which is the compiled value to append.
// Takes isSpread (bool) which is true when the argument uses the `...` spread form.
// Takes typedAppendSubOp (isa.SubOpcode) selecting the tier-1 fast path, 0 to force the
// generic isa.OpAppend path.
// Takes typedAppendKind (isa.RegisterKind) which must match location.Kind for the typed
// path to fire.
// Takes sliceType (types.Type) used to detect the byte-slice specialisation.
//
// Returns the new sliceLocation after the Emit.
func (c *Compiler) emitAppendStep(
	ctx context.Context,
	sliceLocation,
	location program.VarLocation,
	isSpread bool,
	typedAppendSubOp isa.SubOpcode,
	typedAppendKind isa.RegisterKind,
	sliceType types.Type,
) program.VarLocation {
	if isSpread {
		c.boxToGeneralTemp(ctx, &sliceLocation)
		c.boxToGeneralTemp(ctx, &location)
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpAppendSpread, dest, sliceLocation.Register, location.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
	}
	if isa.IsTypedSliceKind(sliceLocation.Kind) {
		if directOp, ok := isaselect.TypedDirectAppendSubOp(sliceLocation.Kind); ok && location.Kind == isa.ElementKindForTypedSlice(sliceLocation.Kind) {
			return c.emitTypedSliceDirectAppend(sliceLocation, location, directOp)
		}
	}
	if typedAppendSubOp != 0 && location.Kind == typedAppendKind {
		if dest, ok := c.tryEmitByteFastAppend(sliceLocation, location, typedAppendSubOp, sliceType); ok {
			return dest
		}
		if fastOp, ok := fastAppendOpcodeFor(typedAppendSubOp); ok {
			dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
			program.Emit(c.Function, fastOp, dest, sliceLocation.Register, location.Register)
			return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
		}
		return c.emitTypedAppend(sliceLocation, location, typedAppendSubOp)
	}
	c.boxElementToGeneralTemp(ctx, &location)
	if dest, ok := c.tryEmitStructFastAppend(ctx, sliceLocation, location, sliceType); ok {
		return dest
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAppend, dest, sliceLocation.Register, location.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
}

// emitTypedSliceDirectAppend emits a typed-bank-direct append, bypassing the general-bank
// reflect-boxing round trip.
//
// Takes sliceLocation (VarLocation) which is the source slice.
// Takes location (VarLocation) which is the element to append.
// Takes directOp (isa.SubOpcode) which is the typed-direct sub-op.
//
// Returns the new typed-bank slice location.
func (c *Compiler) emitTypedSliceDirectAppend(sliceLocation, location program.VarLocation, directOp isa.SubOpcode) program.VarLocation {
	dest := c.Scopes.Alloc.Alloc(sliceLocation.Kind)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(directOp), dest, sliceLocation.Register)
	program.Emit(c.Function, isa.OpExt, location.Register, 0, 0)
	return program.VarLocation{Register: dest, Kind: sliceLocation.Kind}
}

// tryEmitByteFastAppend emits isa.OpAppendByteFast when the slice element is statically a
// byte.
//
// Skips the tier-1 sub-op dispatch entirely and emits a single tier-0 opcode that goes
// through a per-op direct exit (no processExitPathB indirection) AND a hot-path Go
// handler that omits the reflect.TypeAssert cascade. Serves byte-builder patterns such as
// `*output = append(*output, '(')`.
//
// Takes sliceLocation (VarLocation) which is the destination slice.
// Takes location (VarLocation) which is the value to append.
// Takes typedAppendSubOp (isa.SubOpcode) which gates the path on isa.SubOpAppendUint.
// Takes sliceType (types.Type) used to confirm the element type is canonically a byte.
//
// Returns the new destination location and true on success; the zero value and false
// otherwise.
func (c *Compiler) tryEmitByteFastAppend(sliceLocation, location program.VarLocation, typedAppendSubOp isa.SubOpcode, sliceType types.Type) (program.VarLocation, bool) {
	if typedAppendSubOp != isa.SubOpAppendUint || sliceType == nil {
		return program.VarLocation{}, false
	}
	sliceValue, ok := sliceType.Underlying().(*types.Slice)
	if !ok {
		return program.VarLocation{}, false
	}
	elemKind := sliceValue.Elem().Underlying().String()
	if elemKind != "byte" && elemKind != "uint8" {
		return program.VarLocation{}, false
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAppendByteFast, dest, sliceLocation.Register, location.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// emitTypedAppend emits the tier-1 subOpAppend encoding.
//
// Encoding:
//
//	op = isa.OpDrillTier1, a = sub-op id, b = dest reg,
//	c = slice reg, extension.a = element reg.
//
// Takes sliceLocation (VarLocation) which is the destination slice.
// Takes location (VarLocation) which is the value to append.
// Takes typedAppendSubOp (isa.SubOpcode) which selects the typed variant.
//
// Returns the new destination location.
func (c *Compiler) emitTypedAppend(sliceLocation, location program.VarLocation, typedAppendSubOp isa.SubOpcode) program.VarLocation {
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(typedAppendSubOp), dest, sliceLocation.Register)
	program.Emit(c.Function, isa.OpExt, location.Register, 0, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
}

// compileBuiltinMake compiles make(type, arguments...) with the slice backing placed on
// the register arena.
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the make call.
//
// Returns VarLocation holding the newly created value and any compilation error.
func (c *Compiler) compileBuiltinMake(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileBuiltinMakeWithPlacement(ctx, expression, false)
}

// compileBuiltinMakeWithPlacement compiles make(type, arguments...), placing a slice's
// backing on the Go heap when heapPlaced is set. Maps and channels ignore the flag.
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the make call.
// Takes heapPlaced (bool) which selects the heap backing for make([]T, ...); the Compiler
// sets it for heap-promoted locals (see compileLocalInitialiser).
//
// Returns VarLocation holding the newly created value and any compilation error.
func (c *Compiler) compileBuiltinMakeWithPlacement(ctx context.Context, expression *ast.CallExpr, heapPlaced bool) (program.VarLocation, error) {
	tv, ok := c.Info.Types[expression]
	if !ok || tv.Type == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for make at %s", fault.ErrCompilation, c.positionString(expression.Pos()))
	}
	reflectType := c.TypeToReflect(ctx, c.substitutedType(tv.Type))
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)

	switch reflectType.Kind() {
	case reflect.Slice:
		return c.compileMakeSlice(ctx, expression, dest, typeIndex, heapPlaced)
	case reflect.Map:
		hint := c.extractMapSizeHint(expression)
		program.EmitTier2(c.Function, isa.SubOpTier2MakeMap, dest)
		program.EmitExtension(c.Function, typeIndex, hint)
	case reflect.Chan:
		return c.compileMakeChannel(ctx, expression, dest, typeIndex)
	default:
		return program.VarLocation{}, fmt.Errorf("make not supported for type %v at %s", reflectType, c.positionString(expression.Pos()))
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// extractMapSizeHint reads the second argument of make(mapType, hint) when it is a
// constant non-negative integer, and returns its encoded log2 hint (0 when no constant
// hint is present).
//
// Takes expression (*ast.CallExpr) which is the make call.
//
// Returns the encoded log2 hint clamped to [0, maxMapSizeHintLog2].
func (c *Compiler) extractMapSizeHint(expression *ast.CallExpr) uint8 {
	if len(expression.Args) < 2 {
		return 0
	}
	tvHint, ok := c.Info.Types[expression.Args[1]]
	if !ok || tvHint.Value == nil {
		return 0
	}
	hn, exact := constant.Int64Val(tvHint.Value)
	if !exact || hn <= 0 {
		return 0
	}
	return mapSizeHintLog2(int(hn))
}

// compileMakeSlice emits bytecode for make([]T, len[, cap]).
//
// Takes expression (*ast.CallExpr) which is the AST call expression containing the make
// arguments.
// Takes dest (uint8) which is the destination general register for the new slice.
// Takes typeIndex (uint16) which is the type reference index for the slice type.
// Takes heapPlaced (bool) which sets isa.MakeSliceExtHeapFlag on the extension word so
// handleMakeSlice allocates the backing on the Go heap rather than the arena.
//
// Returns VarLocation holding the new slice and any compilation error.
func (c *Compiler) compileMakeSlice(ctx context.Context, expression *ast.CallExpr, dest uint8, typeIndex uint16, heapPlaced bool) (program.VarLocation, error) {
	var lenLocation program.VarLocation
	if len(expression.Args) >= 2 {
		var err error
		lenLocation, err = c.compileExpression(ctx, expression.Args[1])
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	capLocation := lenLocation
	if len(expression.Args) >= makeSliceMinCapArgs {
		var err error
		capLocation, err = c.compileExpression(ctx, expression.Args[2])
		if err != nil {
			return program.VarLocation{}, err
		}
	}
	var placement uint8
	if heapPlaced {
		placement = isa.MakeSliceExtHeapFlag
	}
	program.Emit(c.Function, isa.OpMakeSlice, dest, lenLocation.Register, capLocation.Register)
	program.EmitExtension(c.Function, typeIndex, placement)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileMakeChannel emits bytecode for make(chan T[, size]).
//
// Takes expression (*ast.CallExpr) which is the AST call expression containing the make
// arguments.
// Takes dest (uint8) which is the destination general register for the new channel.
// Takes typeIndex (uint16) which is the type reference index for the channel type.
//
// Returns VarLocation holding the new channel and any compilation error.
func (c *Compiler) compileMakeChannel(ctx context.Context, expression *ast.CallExpr, dest uint8, typeIndex uint16) (program.VarLocation, error) {
	if err := c.checkFeature(policy.InterpFeatureChannels, expression.Lparen); err != nil {
		return program.VarLocation{}, err
	}
	var sizeLocation program.VarLocation
	if len(expression.Args) >= 2 {
		var err error
		sizeLocation, err = c.compileExpression(ctx, expression.Args[1])
		if err != nil {
			return program.VarLocation{}, err
		}
	} else {
		sizeLocation.Register = c.Scopes.Alloc.Alloc(isa.RegisterInt)
		sizeLocation.Kind = isa.RegisterInt
		constIndex, err := program.AddIntConstant(c.Function, 0)
		if err != nil {
			return program.VarLocation{}, err
		}
		program.EmitWide(c.Function, isa.OpLoadIntConst, sizeLocation.Register, constIndex)
	}
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMakeChannel), dest, sizeLocation.Register)
	program.EmitExtension(c.Function, typeIndex, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileBuiltinDelete compiles delete(map, key).
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the delete call.
//
// Returns an empty VarLocation and any compilation error.
func (c *Compiler) compileBuiltinDelete(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	operands, err := c.compileBuiltinArgLocations(ctx, expression, 2, fault.ErrCompileBuiltinDeleteArgCount)
	if err != nil {
		return program.VarLocation{}, err
	}
	mapLocation, keyLocation := operands[0], operands[1]

	c.boxElementToGeneral(ctx, &keyLocation)

	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpMapDelete), mapLocation.Register, keyLocation.Register)
	return program.VarLocation{}, nil
}

// compileBuiltinReal compiles the built-in real() function call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the real call.
//
// Returns VarLocation holding the extracted real component and any compilation error.
func (c *Compiler) compileBuiltinReal(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileComplexExtract(ctx, expression, "real", isa.SubOpRealComplex)
}

// compileBuiltinImag compiles the built-in imag() function call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the imag call.
//
// Returns VarLocation holding the extracted imaginary component and any compilation
// error.
func (c *Compiler) compileBuiltinImag(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileComplexExtract(ctx, expression, "imag", isa.SubOpImagComplex)
}

// compileComplexExtract compiles a complex number component extraction (real or imag) via
// the umbrella opcode's sub-op dispatch.
//
// Takes expression (*ast.CallExpr) which is the AST call expression.
// Takes name (string) which is the builtin function name for error messages.
// Takes subOp (isa.SubOpcode) which selects the umbrella sub-handler
// (isa.SubOpRealComplex or isa.SubOpImagComplex).
//
// Returns VarLocation holding the extracted float component and any compilation error.
func (c *Compiler) compileComplexExtract(ctx context.Context, expression *ast.CallExpr, name string, subOp isa.SubOpcode) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fmt.Errorf("%s requires exactly 1 argument", name)
	}
	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	if argumentLocation, err = c.unboxScalarOperand(ctx, argumentLocation, c.staticTypeOf(expression.Args[0])); err != nil {
		return program.VarLocation{}, err
	}
	if argumentLocation.Kind != isa.RegisterComplex {
		return program.VarLocation{}, fmt.Errorf("%s requires a complex argument", name)
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterFloat)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), dest, argumentLocation.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterFloat}, nil
}

// compileBuiltinComplex compiles the built-in complex() function call.
//
// Takes expression (*ast.CallExpr) which is the AST call expression for the complex call.
//
// Returns VarLocation holding the constructed complex value and any compilation error.
func (c *Compiler) compileBuiltinComplex(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	operands, err := c.compileBuiltinArgLocations(ctx, expression, 2, fault.ErrCompileBuiltinComplexArgCount)
	if err != nil {
		return program.VarLocation{}, err
	}
	realLocation, imagLocation := operands[0], operands[1]
	if realLocation.Kind != isa.RegisterFloat {
		return program.VarLocation{}, fault.ErrCompileBuiltinComplexRequiresFloat
	}
	if imagLocation.Kind != isa.RegisterFloat {
		return program.VarLocation{}, fault.ErrCompileBuiltinComplexRequiresFloat
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterComplex)
	program.Emit(c.Function, isa.OpBuildComplex, dest, realLocation.Register, imagLocation.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterComplex}, nil
}

// compileBuiltinCap compiles cap(x).
//
// Takes expression (*ast.CallExpr) which is the call expression containing the argument.
//
// Returns the capacity value location and any compilation error.
func (c *Compiler) compileBuiltinCap(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileBuiltinCapArgCount
	}

	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	if argumentLocation.Kind == isa.RegisterGeneral {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpCap), dest, argumentLocation.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
	}

	if directCapSubOp, ok := isaselect.TypedSliceDirectCapSubOp(argumentLocation.Kind); ok {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(directCapSubOp), dest, argumentLocation.Register)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
	}

	return program.VarLocation{}, fmt.Errorf("cap not supported for register kind %s", argumentLocation.Kind)
}

// compileBuiltinCopy compiles copy(destination, source).
//
// Takes expression (*ast.CallExpr) which is the call expression containing the arguments.
//
// Returns the number of elements copied as an int location and any compilation error.
func (c *Compiler) compileBuiltinCopy(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	operands, err := c.compileBuiltinArgLocations(ctx, expression, 2, fault.ErrCompileBuiltinCopyArgCount)
	if err != nil {
		return program.VarLocation{}, err
	}
	dstLocation, sourceLocation := operands[0], operands[1]

	if subOp, ok := isaselect.TypedSliceDirectCopySubOp(dstLocation.Kind); ok && dstLocation.Kind == sourceLocation.Kind {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
		program.Emit(c.Function, isa.OpDrillTier1, uint8(subOp), dstLocation.Register, sourceLocation.Register)
		program.Emit(c.Function, isa.OpExt, dest, 0, 0)
		return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
	}

	c.boxToGeneralTemp(ctx, &dstLocation)
	c.boxToGeneralTemp(ctx, &sourceLocation)
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	program.Emit(c.Function, isa.OpCopy, dest, dstLocation.Register, sourceLocation.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, nil
}

// compileBuiltinNew compiles new(T) and new(expr) (Go 1.26+).
//
// Takes expression (*ast.CallExpr) which is the call expression containing the type or
// expression argument.
//
// Returns the pointer variable location and any compilation error.
func (c *Compiler) compileBuiltinNew(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fmt.Errorf("%w: new requires exactly 1 argument at %s", fault.ErrCompilation, c.positionString(expression.Pos()))
	}

	tv, ok := c.Info.Types[expression]
	if !ok || tv.Type == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for new at %s", fault.ErrCompilation, c.positionString(expression.Pos()))
	}

	ptrType, ok := tv.Type.(*types.Pointer)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("expected *types.Pointer, got %T", tv.Type)
	}
	reflectType := c.TypeToReflect(ctx, ptrType.Elem())
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}

	argTV := c.Info.Types[expression.Args[0]]
	if argTV.IsType() {
		dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpConvert, dest, 0, 1)
		program.EmitExtension(c.Function, typeIndex, 0)
		return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
	}

	valueLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpAllocIndirect, dest, valueLocation.Register, uint8(valueLocation.Kind))
	program.EmitExtension(c.Function, typeIndex, 0)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileBuiltinPanic compiles panic(v).
//
// Takes expression (*ast.CallExpr) which is the call expression containing the panic
// value.
//
// Returns an empty variable location and any compilation error.
func (c *Compiler) compileBuiltinPanic(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileSingleArgGeneralTier2(ctx, expression, isa.SubOpTier2Panic, fault.ErrCompileBuiltinPanicArgCount)
}

// compileBuiltinRecover compiles recover().
//
// Returns the recovered value location and any compilation error.
func (c *Compiler) compileBuiltinRecover(_ context.Context, _ *ast.CallExpr) (program.VarLocation, error) {
	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Recover), dest)
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileBuiltinClose compiles close(ch).
//
// Takes expression (*ast.CallExpr) which is the call expression containing the channel.
//
// Returns an empty variable location and any compilation error.
func (c *Compiler) compileBuiltinClose(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	return c.compileSingleArgGeneralTier2(ctx, expression, isa.SubOpTier2ChannelClose, fault.ErrCompileBuiltinCloseArgCount)
}

// compileSingleArgGeneralTier2 compiles a builtin that takes one argument, boxes it to
// general if needed, and emits the tier-2 drill-down form (isa.OpDrillTier1 ->
// isa.SubOpDrillTier2 -> tier2SubOp) with the boxed argument register in operand C.
//
// Takes expression (*ast.CallExpr) which is the call expression containing the argument.
// Takes tier2SubOp (isa.SubOpcodeTier2) which selects the tier-2 dispatcher arm.
// Takes errArgCount (error) which is the sentinel returned when the argument count is
// wrong.
//
// Returns an empty variable location and any compilation error.
func (c *Compiler) compileSingleArgGeneralTier2(ctx context.Context, expression *ast.CallExpr, tier2SubOp isa.SubOpcodeTier2, errArgCount error) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, errArgCount
	}
	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	c.boxElementToGeneral(ctx, &argumentLocation)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(tier2SubOp), argumentLocation.Register)
	return program.VarLocation{}, nil
}

// compileStructLiteral compiles a struct literal like Point{X: 1, Y: 2}.
//
// Takes lit (*ast.CompositeLit) which is the composite literal AST node.
// Takes reflectType (reflect.Type) which is the reflect type of the struct.
//
// Returns the struct variable location and any compilation error.
func (c *Compiler) compileStructLiteral(ctx context.Context, lit *ast.CompositeLit, reflectType reflect.Type) (program.VarLocation, error) {
	typeIndex, err := program.AddTypeRef(c.Function, reflectType)
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)

	program.EmitTier2(c.Function, engine.StructLiteralAllocationSubOp(reflectType), dest)
	program.EmitExtension(c.Function, typeIndex, 0)

	for i, elt := range lit.Elts {
		watermark := c.Scopes.Alloc.Snapshot()
		if err := c.compileStructField(ctx, dest, i, elt, lit, reflectType); err != nil {
			return program.VarLocation{}, err
		}
		c.Scopes.RestoreWatermark(watermark)
	}

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compileStructField compiles a single field initialiser within a struct literal and
// emits the appropriate set-field opcode.
//
// Takes dest (uint8) which is the destination register of the struct.
// Takes positionalIndex (int) which is the positional index for unkeyed fields.
// Takes elt (ast.Expr) which is the field element expression.
// Takes literal (*ast.CompositeLit) which supplies the literal's static type for both the
// key lookup and the addressable walk used by the deep-path fallback.
// Takes reflectType (reflect.Type) which is the reflect type of the struct.
//
// Returns any compilation error.
func (c *Compiler) compileStructField(ctx context.Context, dest uint8, positionalIndex int, elt ast.Expr, literal *ast.CompositeLit, reflectType reflect.Type) error {
	var literalType types.Type
	if c.Info != nil {
		literalType = c.Info.Types[literal].Type
	}

	fieldPath, valExpr, err := c.resolveStructFieldPath(positionalIndex, elt, literalType, reflectType)
	if err != nil {
		return err
	}

	valueLocation, err := c.compileExpression(ctx, valExpr)
	if err != nil {
		return err
	}
	valueLocation = c.coerceEvalBoolResult(ctx, c.Info, valExpr, valueLocation)

	c.emitStructFieldSet(ctx, dest, fieldPath, valueLocation, reflectType, literal)
	return nil
}

// emitStructFieldSet emits the correct set-field opcode for the given value location,
// using the typed-bank fast path when possible.
//
// Takes dest (uint8) which is the struct's destination register.
// Takes fieldPath ([]int) which is the field index path.
// Takes valueLocation (VarLocation) which is the compiled value.
// Takes structType (reflect.Type) which is the struct type, or nil to disable tier-0
// routing.
// Takes literal (*ast.CompositeLit) which supplies the static type chain for deep paths.
func (c *Compiler) emitStructFieldSet(ctx context.Context, dest uint8, fieldPath []int, valueLocation program.VarLocation, structType reflect.Type, literal *ast.CompositeLit) {
	if c.tryEmitTypedStructFieldSet(dest, fieldPath, valueLocation, structType) {
		return
	}
	if len(fieldPath) > 1 {
		parentLocation := c.compileEmbeddedParentPointerReusing(
			program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, literal, fieldPath)
		c.boxElementToGeneralTemp(ctx, &valueLocation)
		program.Emit(c.Function, isa.OpSetField, parentLocation.Register,
			safeconv.MustIntToUint8(fieldPath[len(fieldPath)-1]), valueLocation.Register)
		return
	}
	fieldIndex := safeconv.MustIntToUint8(fieldPath[0])
	if valueLocation.Kind == isa.RegisterInt && !structFieldIsInterface(structType, fieldIndex) {
		destinationLocation := program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}
		c.EmitTyped(ctx, isa.OpSetFieldInt, destinationLocation, rawOperand(fieldIndex), valueLocation)
		return
	}
	if valueLocation.Kind != isa.RegisterGeneral {
		generalRegister := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
		c.emitBoxElementToGeneral(ctx, generalRegister, valueLocation)
		program.Emit(c.Function, isa.OpSetField, dest, fieldIndex, generalRegister)
		c.Scopes.Alloc.FreeTemp(isa.RegisterGeneral, generalRegister)
		return
	}
	program.Emit(c.Function, isa.OpSetField, dest, fieldIndex, valueLocation.Register)
}

// tryEmitTypedStructFieldSet attempts to Emit one of the tier-0 or tier-1 unsafe-write
// opcodes for a typed struct field. Returns true when the typed fast path was taken; the
// caller should fall back to the boxing path on false.
//
// Takes dest (uint8) which is the destination register of the struct.
// Takes fieldPath ([]int) which is the target field index path.
// Takes valueLocation (VarLocation) which is the compiled value location.
// Takes structType (reflect.Type) which is the literal's declared struct type. nil
// disables the fast path.
//
// Returns true when a fast-path opcode was emitted.
func (c *Compiler) tryEmitTypedStructFieldSet(dest uint8, fieldPath []int, valueLocation program.VarLocation, structType reflect.Type) bool {
	if structType == nil || !isaselect.StructFieldFastPathWriteKindEnabled(valueLocation.Kind) {
		return false
	}
	layoutIdx, ok := c.registerStructFieldLayoutFromReflect(structType, fieldPath)
	if !ok {
		return false
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if isa.RegisterKind(layout.RegisterKind) != valueLocation.Kind {
		return false
	}
	if fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		if op, hasOp := isaselect.PickSetStructFieldTier0Op(valueLocation.Kind); hasOp {
			program.Emit(c.Function, op, dest, valueLocation.Register, safeconv.MustUintToUint8(uint(layoutIdx)))
			return true
		}
	}
	if sub, hasSubOp := isaselect.PickSetStructFieldUnsafeSubOp(valueLocation.Kind); hasSubOp {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(sub), dest, valueLocation.Register)
		c.emitStructFieldLayoutExtension(layoutIdx)
		return true
	}
	return false
}

// compileTypeAssertExpression compiles a type assertion expression (x.(T)).
//
// Takes expression (*ast.TypeAssertExpr) which is the type assertion expression AST node.
//
// Returns the asserted value location and any compilation error.
func (c *Compiler) compileTypeAssertExpression(ctx context.Context, expression *ast.TypeAssertExpr) (program.VarLocation, error) {
	sourceLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}

	c.boxToGeneral(ctx, &sourceLocation)

	targetTypeAndValue, ok := c.Info.Types[expression.Type]
	if !ok || targetTypeAndValue.Type == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for type assertion at %s", fault.ErrCompilation, c.positionString(expression.Type.Pos()))
	}
	targetType := targetTypeAndValue.Type
	reflectType := c.typeAssertReflectType(ctx, targetType)
	methodNames := interfaceTargetMethodNames(c.substitutedType(targetType))
	typeIndex, err := program.AddTypeRefWithMethods(c.Function, reflectType, methodNames)
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	okRegister := c.Scopes.Alloc.Alloc(isa.RegisterInt)

	program.Emit(c.Function, isa.OpTypeAssert, dest, sourceLocation.Register, okRegister)
	program.EmitExtension(c.Function, typeIndex, 1)

	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, nil
}

// compilePrintArguments compiles the operands of print or println. A single multi-valued
// call (`println(f())`) spreads into one operand per result, and a comparison result held
// in the int bank is moved to the bool bank so it prints as true or false.
//
// Takes expression (*ast.CallExpr) which is the print call.
//
// Returns one location per printed value and any compilation error.
func (c *Compiler) compilePrintArguments(ctx context.Context, expression *ast.CallExpr) ([]program.VarLocation, error) {
	if len(expression.Args) == 1 {
		if call, ok := ast.Unparen(expression.Args[0]).(*ast.CallExpr); ok {
			if tuple, isTuple := c.Info.TypeOf(call).(*types.Tuple); isTuple && tuple.Len() > 1 {
				return c.compileReturnTupleCall(ctx, call)
			}
		}
	}
	argumentLocations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		argumentLocations[i] = c.coerceEvalBoolResult(ctx, c.Info, argument, location)
	}
	return argumentLocations, nil
}

// compileBuiltinPrint compiles print() or println() to isa.SubOpCallBuiltin.
//
// Takes expression (*ast.CallExpr) which is the call expression containing print
// arguments.
// Takes builtinID (uint8) which is the builtin identifier for the print variant.
//
// Returns an empty variable location and any compilation error.
func (c *Compiler) compileBuiltinPrint(ctx context.Context, expression *ast.CallExpr, builtinID uint8) (program.VarLocation, error) {
	argumentLocations, err := c.compilePrintArguments(ctx, expression)
	if err != nil {
		return program.VarLocation{}, err
	}
	argumentCount := len(argumentLocations)

	program.EmitTier1(c.Function, isa.SubOpCallBuiltin, builtinID, safeconv.MustIntToUint8(argumentCount))
	for _, location := range argumentLocations {
		program.Emit(c.Function, isa.OpExt, location.Register, uint8(location.Kind), 0)
	}

	return program.VarLocation{}, nil
}

// compileBuiltinClear compiles clear(x) to isa.SubOpCallBuiltin.
//
// Takes expression (*ast.CallExpr) which is the call expression containing the argument.
//
// Returns an empty variable location and any compilation error.
func (c *Compiler) compileBuiltinClear(ctx context.Context, expression *ast.CallExpr) (program.VarLocation, error) {
	if len(expression.Args) != 1 {
		return program.VarLocation{}, fault.ErrCompileBuiltinClearArgCount
	}

	argumentLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}
	c.boxToGeneral(ctx, &argumentLocation)

	program.EmitTier1(c.Function, isa.SubOpCallBuiltin, isa.BuiltinClear, 1)
	program.Emit(c.Function, isa.OpExt, argumentLocation.Register, uint8(argumentLocation.Kind), 0)

	return program.VarLocation{}, nil
}

// compileBuiltinMinMax compiles min(...) or max(...) using inline comparison chains for
// int and float operands.
//
// Takes expression (*ast.CallExpr) which is the call expression containing the arguments.
// Takes isMin (bool) which controls whether this compiles min (true) or max (false).
//
// Returns the result variable location and any compilation error.
func (c *Compiler) compileBuiltinMinMax(ctx context.Context, expression *ast.CallExpr, isMin bool) (program.VarLocation, error) {
	if len(expression.Args) < 2 {
		return program.VarLocation{}, fault.ErrCompileBuiltinMinMaxArgCount
	}

	resultLocation, err := c.compileExpression(ctx, expression.Args[0])
	if err != nil {
		return program.VarLocation{}, err
	}

	dest := c.Scopes.Alloc.Alloc(resultLocation.Kind)
	destLocation := program.VarLocation{Register: dest, Kind: resultLocation.Kind}
	c.emitMoveTyped(ctx, destLocation, resultLocation, c.staticTypeOf(expression.Args[0]))

	for _, argument := range expression.Args[1:] {
		argumentLocation, err := c.compileExpression(ctx, argument)
		if err != nil {
			return program.VarLocation{}, err
		}

		var cmpLocation program.VarLocation
		if isMin {
			cmpLocation, err = c.emitBinaryOp(ctx, token.LSS, argumentLocation, destLocation)
		} else {
			cmpLocation, err = c.emitBinaryOp(ctx, token.GTR, argumentLocation, destLocation)
		}
		if err != nil {
			return program.VarLocation{}, err
		}

		skipJump := program.EmitJump(c.Function, isa.OpJumpIfFalse, cmpLocation.Register)
		c.emitMoveTyped(ctx, destLocation, argumentLocation, c.staticTypeOf(argument))
		program.PatchJump(c.Function, skipJump)
	}

	return destLocation, nil
}

// compileBuiltinArgLocations compiles a builtin's operands: each argument in turn, or,
// for the single-argument form `copy(f())` / `delete(h(m))`, the results of one
// multi-value call, which land in fresh registers of their own banks.
//
// Takes expression (*ast.CallExpr) which is the builtin call.
// Takes expected (int) which is the builtin's operand count.
// Takes arityError (error) which is reported when the operands do not match.
//
// Returns []program.VarLocation which holds exactly expected operands.
// Returns error when an operand fails to compile or the arity does not match.
func (c *Compiler) compileBuiltinArgLocations(ctx context.Context, expression *ast.CallExpr, expected int, arityError error) ([]program.VarLocation, error) {
	if locations, handled, err := c.compileBuiltinTupleArg(ctx, expression, expected); handled {
		return locations, err
	}
	if len(expression.Args) != expected {
		return nil, arityError
	}
	locations := make([]program.VarLocation, len(expression.Args))
	for i, argument := range expression.Args {
		location, err := c.compileExpression(ctx, argument)
		if err != nil {
			return nil, err
		}
		locations[i] = location
	}
	return locations, nil
}

// compileBuiltinTupleArg handles the single-operand form of a builtin whose one operand
// is a call returning exactly expected values (`copy(f())`): the results land in fresh
// registers of their own banks.
//
// Takes expression (*ast.CallExpr) which is the builtin call.
// Takes expected (int) which is the builtin's operand count.
//
// Returns the operand locations, whether the form applied, and any compile error.
func (c *Compiler) compileBuiltinTupleArg(ctx context.Context, expression *ast.CallExpr, expected int) ([]program.VarLocation, bool, error) {
	if len(expression.Args) != 1 || expected <= 1 {
		return nil, false, nil
	}
	call, ok := ast.Unparen(expression.Args[0]).(*ast.CallExpr)
	if !ok {
		return nil, false, nil
	}
	tuple, isTuple := c.Info.TypeOf(call).(*types.Tuple)
	if !isTuple || tuple.Len() != expected {
		return nil, false, nil
	}
	locations := make([]program.VarLocation, tuple.Len())
	for i := range tuple.Len() {
		resultType := tuple.At(i).Type()
		kind := c.kindFor(resultType)
		locations[i] = program.VarLocation{Register: c.Scopes.Alloc.Alloc(kind), Kind: kind, SourceType: c.exactReflectTypeForBoxing(resultType)}
	}
	if err := c.emitMultiReturnCall(ctx, call, locations); err != nil {
		return nil, true, err
	}
	return locations, true, nil
}

// mapSizeHintLog2 packs a log2 map-size hint for isa.SubOpTier2MakeMap.
//
// Encodes into the extension word's C byte: 0 means "no hint" and values
// 1..maxMapSizeHintLog2 indicate a hint of 1<<n. The hint is rounded up to the next power
// of two so the underlying map is sized to hold at least n entries without resizing.
//
// Takes n (int) which is the desired number of entries.
//
// Returns the encoded log2 hint, clamped to [0, maxMapSizeHintLog2].
func mapSizeHintLog2(n int) uint8 {
	if n <= 0 {
		return 0
	}
	lg := bits.Len(uint(n - 1))
	if lg <= 0 {
		lg = 1
	}
	if lg > maxMapSizeHintLog2 {
		lg = maxMapSizeHintLog2
	}
	return uint8(lg)
}

// structFieldIsInterface reports whether a struct field has interface kind.
//
// isa.OpSetFieldInt writes an int64 directly into the field's storage slot via
// reflect.Value.SetInt, which panics with "reflect.Value.SetInt on interface Value" when
// the field is declared as an interface (any). Detecting the case at compile time routes
// such writes through the boxing path (emitBoxToGeneral + isa.OpSetField) so the value is
// wrapped in reflect.Value before assignment.
//
// Takes structType (reflect.Type) which is the struct's reflect.Type (pointer wrappers
// are peeled by the caller chain).
// Takes fieldIndex (uint8) which is the zero-based field index.
//
// Returns true when the field's reflect.Kind is Interface; false when the kind is
// concrete or the type/index lookup fails.
func structFieldIsInterface(structType reflect.Type, fieldIndex uint8) bool {
	if structType == nil {
		return false
	}
	t := structType
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}
	index := int(fieldIndex)
	if index < 0 || index >= t.NumField() {
		return false
	}
	return t.Field(index).Type.Kind() == reflect.Interface
}
