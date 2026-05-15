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
	"math"
	"reflect"

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// snapshotMode classifies whether a general-bank store of a value of a given static type
// must invoke ValueCopyForBoundary to preserve Go's value semantics. The Compiler emits
// isa.OpMoveGeneral with the matching moveGeneralMode operand (MoveGeneralModeAlias,
// MoveGeneralModeSnapshot, MoveGeneralModeDynamic) respectively.
type snapshotMode uint8

const (
	// snapshotNever marks types whose reflect.Value header copy already matches Go's
	// reference semantics (pointer, slice, map, chan, signature, basic). The Compiler emits
	// isa.OpMoveGeneral with MoveGeneralModeAlias.
	snapshotNever snapshotMode = iota

	// snapshotAlways marks types that must be copied to preserve Go's value semantics
	// (struct, array). The Compiler emits isa.OpMoveGeneral with MoveGeneralModeSnapshot,
	// which calls the arena boundary-copy helper unconditionally, eliding the runtime kind
	// switch.
	snapshotAlways

	// snapshotDynamic marks types whose runtime kind is not known at compile time
	// (interface, type parameter, nil). The Compiler emits isa.OpMoveGeneral which performs
	// a runtime kind switch.
	snapshotDynamic
)

var (
	// typedMapGetOpcodes holds the typed map-get opcodes, used by selectTypedMapGetOpcode.
	typedMapGetOpcodes = typedMapOpcodes{
		intInt: isa.OpMapGetIntInt, intString: isa.OpMapGetIntString, intGeneral: isa.OpMapGetIntGeneral,
		stringInt: isa.OpMapGetStringInt, stringString: isa.OpMapGetStringString, stringGeneral: isa.OpMapGetStringGeneral,
	}

	// typedMapIndexOkOpcodes holds the typed `v, ok := m[k]` opcodes, used by
	// selectTypedMapIndexOkOpcode.
	typedMapIndexOkOpcodes = typedMapOpcodes{
		intInt: isa.OpMapIndexOkIntInt, intString: isa.OpMapIndexOkIntString, intGeneral: isa.OpMapIndexOkIntGeneral,
		stringInt: isa.OpMapIndexOkStringInt, stringString: isa.OpMapIndexOkStringString, stringGeneral: isa.OpMapIndexOkStringGeneral,
	}

	// typedMapSetOpcodes holds the typed map-set opcodes, used by selectTypedMapSetOpcode.
	typedMapSetOpcodes = typedMapOpcodes{
		intInt: isa.OpMapSetIntInt, intString: isa.OpMapSetIntString, intGeneral: isa.OpMapSetIntGeneral,
		stringInt: isa.OpMapSetStringInt, stringString: isa.OpMapSetStringString, stringGeneral: isa.OpMapSetStringGeneral,
	}
)

// fusedArrayFieldMetadata carries the static-analysis results that survive the fusion
// gates in resolveFusedArrayFieldMetadata through to the Emit step: the tier-0 layout
// index plus the runtime array length, element size and element kind that the extension
// words encode.
type fusedArrayFieldMetadata struct {
	// layoutIdx is the tier-0 struct-field layout table index.
	layoutIdx uint16

	// arrayLength is the runtime length of the array.
	arrayLength int

	// elementSize is the byte size of one array element.
	elementSize uintptr

	// elementKind is the reflect.Kind of the array element.
	elementKind reflect.Kind
}

// compileIndexExpression compiles an index expression (a[i]).
//
// Dispatches to tryFoldStringIndex first to pre-fold s[i] when both the string subject
// and the integer index are compile-time constants. Falls through to runtime emission for
// maps via compileMapIndex, and slices/arrays/strings via compileSliceOrArrayIndex.
//
// Takes expression (*ast.IndexExpr) which is the AST index expression node.
//
// Returns VarLocation holding the indexed value and any compilation error.
func (c *Compiler) compileIndexExpression(ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, error) {
	if location, applied := c.tryIndexLowerings(ctx, expression); applied {
		return location, nil
	}

	collectionLocation, err := c.compileExpression(ctx, expression.X)
	if err != nil {
		return program.VarLocation{}, err
	}
	indexLocation, err := c.compileExpression(ctx, expression.Index)
	if err != nil {
		return program.VarLocation{}, err
	}

	typeAndValue, ok := c.Info.Types[expression]
	if !ok || typeAndValue.Type == nil {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for index expression at %s", fault.ErrCompilation, c.positionString(expression.Pos()))
	}
	elementKind := c.kindFor(typeAndValue.Type)
	collectionType, ok := c.underlyingTypeOf(expression.X)
	if !ok {
		return program.VarLocation{}, fmt.Errorf("%w: missing type information for indexed collection at %s", fault.ErrCompilation, c.positionString(expression.X.Pos()))
	}

	if mapType, isMap := collectionType.(*types.Map); isMap {
		c.setDebugPosition(ctx, expression.Lbrack)
		return c.compileMapIndex(ctx, mapType, collectionLocation, indexLocation, elementKind)
	}
	c.setDebugPosition(ctx, expression.Lbrack)
	return c.compileSliceOrArrayIndex(ctx, collectionType, collectionLocation, indexLocation, elementKind)
}

// tryFoldStringIndex pre-computes the byte at constant offset of a constant string at
// compile time. Returns the folded location and true when both subject and index are
// compile-time constants; the caller is expected to fall through to runtime emission
// otherwise.
//
// The Go expression s[i] returns a byte (uint8); the helper emits a uint-const load
// directly into the uint register bank. The peephole optimiser later rewrites this to
// isa.SubOpLoadUintConstSmall when the value fits in 8 bits.
//
// Takes expression (*ast.IndexExpr) which is the source-level indexing expression.
//
// Returns the folded constant location.
// Returns true when folding succeeded; false when the caller must fall through to runtime
// emission.
func (c *Compiler) tryFoldStringIndex(ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, bool) {
	xTV, xOk := c.Info.Types[expression.X]
	if !xOk || xTV.Value == nil || xTV.Value.Kind() != constant.String {
		return program.VarLocation{}, false
	}
	indexTypeValue, indexOk := c.Info.Types[expression.Index]
	if !indexOk || indexTypeValue.Value == nil || indexTypeValue.Value.Kind() != constant.Int {
		return program.VarLocation{}, false
	}
	s := constant.StringVal(xTV.Value)
	index, exact := constant.Int64Val(indexTypeValue.Value)
	if !exact || index < 0 || int(index) >= len(s) {
		return program.VarLocation{}, false
	}
	byteValue := uint64(s[index])
	register := c.Scopes.Alloc.Alloc(isa.RegisterUint)
	uintConstIndex, err := program.AddUintConstant(c.Function, byteValue)
	if err != nil {
		return program.VarLocation{}, false
	}
	program.EmitWide(c.Function, isa.OpLoadUintConst, register, uintConstIndex)
	_ = ctx
	return program.VarLocation{Register: register, Kind: isa.RegisterUint}, true
}

// compileMapIndex compiles a map index expression m[k].
//
// Takes mapType (*types.Map) which is the go/types map type for the collection.
// Takes collectionLocation (VarLocation) which is the VarLocation of the map collection.
// Takes indexLocation (VarLocation) which is the VarLocation of the index key.
// Takes elementKind (isa.RegisterKind) which is the expected register kind of the
// element.
//
// Returns VarLocation holding the map element value and any compilation error.
func (c *Compiler) compileMapIndex(ctx context.Context, mapType *types.Map, collectionLocation, indexLocation program.VarLocation, elementKind isa.RegisterKind) (program.VarLocation, error) {
	keyKind := c.kindFor(mapType.Key())
	if op, ok := selectTypedMapGetOpcode(keyKind, elementKind, indexLocation.Kind); ok {
		destinationRegister := c.Scopes.Alloc.Alloc(elementKind)
		destinationLocation := program.VarLocation{Register: destinationRegister, Kind: elementKind}
		c.EmitTyped(ctx, op, destinationLocation, collectionLocation, indexLocation)
		return destinationLocation, nil
	}

	c.boxElementToGeneralTemp(ctx, &indexLocation)
	destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpMapIndex, destinationRegister, collectionLocation.Register, indexLocation.Register)

	if elementKind != isa.RegisterGeneral {
		return c.emitUnboxFromGeneral(destinationRegister, elementKind), nil
	}
	return program.VarLocation{Register: destinationRegister, Kind: isa.RegisterGeneral}, nil
}

// compileSliceOrArrayIndex compiles a slice, array, or string index expression.
//
// Takes collectionType (types.Type) which is the go/types type of the collection.
// Takes collectionLocation (VarLocation) which is the VarLocation of the collection.
// Takes indexLocation (VarLocation) which is the VarLocation of the index.
// Takes elementKind (isa.RegisterKind) which is the expected register kind of the
// element.
//
// Returns VarLocation holding the indexed element and any compilation error.
func (c *Compiler) compileSliceOrArrayIndex(
	ctx context.Context,
	collectionType types.Type,
	collectionLocation,
	indexLocation program.VarLocation,
	elementKind isa.RegisterKind,
) (program.VarLocation, error) {
	c.ensureIntRegister(ctx, &indexLocation)
	if indexLocation.Kind != isa.RegisterInt {
		return program.VarLocation{}, fault.ErrCompileSliceIndexMustBeInteger
	}

	if basic, ok := collectionType.(*types.Basic); ok && basic.Info()&types.IsString != 0 {
		destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterUint)
		program.Emit(c.Function, isa.OpStringIndex, destinationRegister, collectionLocation.Register, indexLocation.Register)
		return program.VarLocation{Register: destinationRegister, Kind: isa.RegisterUint}, nil
	}

	if location, ok := c.tryTypedSliceGet(ctx, collectionType, collectionLocation, indexLocation); ok {
		return location, nil
	}

	c.boxToGeneral(ctx, &collectionLocation)
	destinationRegister := c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
	program.Emit(c.Function, isa.OpIndex, destinationRegister, collectionLocation.Register, indexLocation.Register)
	if elementKind != isa.RegisterGeneral {
		return c.emitUnboxFromGeneral(destinationRegister, elementKind), nil
	}
	return program.VarLocation{Register: destinationRegister, Kind: isa.RegisterGeneral}, nil
}

// staticTypeOf returns an expression's static type under active generic substitutions.
//
// Takes expression (ast.Expr) which is the expression to resolve, or nil.
//
// Returns the substituted types.Type, or nil when unavailable.
func (c *Compiler) staticTypeOf(expression ast.Expr) types.Type {
	if c == nil || c.Info == nil || expression == nil {
		return nil
	}
	tv, ok := c.Info.Types[expression]
	if !ok {
		return nil
	}
	return c.substitutedType(tv.Type)
}

// tryTypedSliceGet emits a typed slice/array get if the element maps to a specialised
// register kind.
//
// Takes collectionType (types.Type) which is the collection's static type.
// Takes collectionLocation (VarLocation) which is the collection.
// Takes indexLocation (VarLocation) which is the index.
//
// Returns the element location and true, or (zero, false) for the general-path fallback.
func (c *Compiler) tryTypedSliceGet(ctx context.Context, collectionType types.Type, collectionLocation, indexLocation program.VarLocation) (program.VarLocation, bool) {
	elementRegisterKind, ok := c.sliceElemRegisterKind(collectionType)
	if !ok {
		return program.VarLocation{}, false
	}
	destinationRegister := c.Scopes.Alloc.Alloc(elementRegisterKind)
	destinationLocation := program.VarLocation{Register: destinationRegister, Kind: elementRegisterKind}
	plan, ok := isaselect.PlanSliceGet(collectionLocation.Kind, elementRegisterKind, false)
	if !ok {
		return program.VarLocation{}, false
	}
	c.emitSliceGet(ctx, plan, destinationLocation, collectionLocation, indexLocation)
	return destinationLocation, true
}

// sliceElemRegisterKind returns the register kind for a slice element.
//
// Applies to slice or array element types when they map to a specialised register.
// Specialised bodies route TypeParam elements like `V` to the concrete instantiation kind
// via c.substitutedType before classifying the element; without this routing a generic
// `[]V` body classifies its elements as isa.RegisterGeneral even when the specialisation
// pins V to int/float/string/bool/uint, and the typed-slice fast-paths silently refuse,
// falling back to a general-bank indexing path that reads from the wrong bank at runtime
// (e.g. a `makeMap[K,V]` body returning zero values).
//
// Takes t (types.Type) which is the go/types type to inspect.
//
// Returns isa.RegisterKind which is the specialised bank or isa.RegisterGeneral.
// Returns bool which is true when a specialised bank applies.
func (c *Compiler) sliceElemRegisterKind(t types.Type) (isa.RegisterKind, bool) {
	t = c.substitutedType(t)
	var element types.Type
	switch u := t.Underlying().(type) {
	case *types.Slice:
		element = u.Elem()
	case *types.Array:
		element = u.Elem()
	case *types.Pointer:
		if array, ok := u.Elem().Underlying().(*types.Array); ok {
			element = array.Elem()
			break
		}
		return isa.RegisterGeneral, false
	default:
		return isa.RegisterGeneral, false
	}
	element = c.substitutedType(element)
	k := typemap.KindForType(element)
	if k == isa.RegisterInt || k == isa.RegisterFloat || k == isa.RegisterString || k == isa.RegisterBool || k == isa.RegisterUint {
		return k, true
	}
	return isa.RegisterGeneral, false
}

// tryCompileFusedArrayFieldIndex fuses `recv.field[index]` into the
// opGet/SetStructFieldIndexGeneral opcodes when the collection is an array-typed struct
// field with pointer- or interface-kind elements.
//
// Takes expression (*ast.IndexExpr) whose X must be a struct-field selector.
// Takes isAssign (bool) which selects the SET form.
// Takes valueLocation (VarLocation) which is the pre-compiled value for the SET form.
//
// Returns the element location and true when the fused opcode was emitted, or (zero,
// false) when any gate fails.
func (c *Compiler) tryCompileFusedArrayFieldIndex(ctx context.Context, expression *ast.IndexExpr, isAssign bool, valueLocation program.VarLocation) (program.VarLocation, bool) {
	selector, ok := expression.X.(*ast.SelectorExpr)
	if !ok {
		return program.VarLocation{}, false
	}
	meta, ok := c.resolveFusedArrayFieldMetadata(ctx, selector, expression)
	if !ok {
		return program.VarLocation{}, false
	}
	receiverLocation, indexLocation, ok := c.compileFusedArrayFieldOperands(ctx, selector, expression)
	if !ok {
		return program.VarLocation{}, false
	}
	c.setDebugPosition(ctx, expression.Lbrack)
	return c.emitFusedArrayFieldIndex(ctx, meta, receiverLocation, indexLocation, isAssign, valueLocation)
}

// resolveFusedArrayFieldMetadata runs the static admission gates for the fused
// receiver-field-index opcodes.
//
// Emits no code, so a false return leaves the caller free to compile the generic path.
//
// Takes selector (*ast.SelectorExpr) which is the struct field selector.
// Takes expression (*ast.IndexExpr) which is the index expression.
//
// Returns fusedArrayFieldMetadata which carries the layout and element details.
// Returns bool which is true when all gates passed.
func (c *Compiler) resolveFusedArrayFieldMetadata(ctx context.Context, selector *ast.SelectorExpr, expression *ast.IndexExpr) (fusedArrayFieldMetadata, bool) {
	selection, ok := c.Info.Selections[selector]
	if !ok || selection.Kind() != types.FieldVal {
		return fusedArrayFieldMetadata{}, false
	}
	leafType := types.Unalias(selection.Type())
	if _, isArray := leafType.Underlying().(*types.Array); !isArray {
		return fusedArrayFieldMetadata{}, false
	}
	if named, isNamed := leafType.(*types.Named); isNamed && !namedTypeHasNoMethods(named) {
		return fusedArrayFieldMetadata{}, false
	}
	indexType, ok := c.Info.Types[expression.Index]
	if !ok || indexType.Type == nil || c.kindFor(indexType.Type) != isa.RegisterInt {
		return fusedArrayFieldMetadata{}, false
	}
	return c.resolveFusedArrayLayoutMetadata(ctx, selection)
}

// resolveFusedArrayLayoutMetadata runs the layout-level fusion gates once the type-level
// gates have passed.
//
// The field must resolve to a tier-0 layout index describing an array-kind field whose
// element is Pointer or Interface, with a runtime array length and element size that each
// fit sixteen bits. Emits no code, so a false return leaves the caller free to compile
// the generic path.
//
// Takes selection (*types.Selection) which is the field selection.
//
// Returns fusedArrayFieldMetadata which carries the layout details.
// Returns bool which is true when all gates passed.
func (c *Compiler) resolveFusedArrayLayoutMetadata(ctx context.Context, selection *types.Selection) (fusedArrayFieldMetadata, bool) {
	layoutIdx, ok := c.tryResolveStructFieldLayout(ctx, selection)
	if !ok || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		return fusedArrayFieldMetadata{}, false
	}
	layout := c.Function.StructLayoutTable[layoutIdx]
	if reflect.Kind(layout.Kind) != reflect.Array || int(layout.FieldTypeIndex) >= len(c.Function.TypeTable) {
		return fusedArrayFieldMetadata{}, false
	}
	arrayType := c.Function.TypeTable[layout.FieldTypeIndex]
	if arrayType == nil || arrayType.Kind() != reflect.Array {
		return fusedArrayFieldMetadata{}, false
	}
	elementType := arrayType.Elem()
	elementKind := elementType.Kind()
	if elementKind != reflect.Pointer && elementKind != reflect.Interface {
		return fusedArrayFieldMetadata{}, false
	}
	arrayLength := arrayType.Len()
	elementSize := elementType.Size()
	if arrayLength > math.MaxUint16 || elementSize > math.MaxUint16 {
		return fusedArrayFieldMetadata{}, false
	}
	return fusedArrayFieldMetadata{
		layoutIdx:   layoutIdx,
		arrayLength: arrayLength,
		elementSize: elementSize,
		elementKind: elementKind,
	}, true
}

// compileFusedArrayFieldOperands compiles the receiver and index expressions for the
// fused path.
//
// Confirms each landed in the expected bank (general receiver, int index). A compile
// error is recorded as sticky and reported as a false return, matching the generic path's
// error handling.
//
// Takes selector (*ast.SelectorExpr) which is the struct field selector.
// Takes expression (*ast.IndexExpr) which is the index expression.
//
// Returns program.VarLocation which is the general-bank receiver.
// Returns program.VarLocation which is the int-bank index.
// Returns bool which is true when both operands compiled into the expected banks.
func (c *Compiler) compileFusedArrayFieldOperands(ctx context.Context, selector *ast.SelectorExpr, expression *ast.IndexExpr) (receiverLocation, indexLocation program.VarLocation, ok bool) {
	var err error
	receiverLocation, err = c.compileExpression(ctx, selector.X)
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, program.VarLocation{}, false
	}
	if receiverLocation.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, program.VarLocation{}, false
	}
	indexLocation, err = c.compileExpression(ctx, expression.Index)
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, program.VarLocation{}, false
	}
	if indexLocation.Kind != isa.RegisterInt {
		return program.VarLocation{}, program.VarLocation{}, false
	}
	return receiverLocation, indexLocation, true
}

// emitFusedArrayFieldIndex emits the fused opGet/SetStructFieldIndexGeneral opcode.
//
// Appends two extension words (array length with the index register, element size with
// the element kind).
//
// Takes meta (fusedArrayFieldMetadata) which carries the layout details.
// Takes receiverLocation (VarLocation) which is the general-bank receiver.
// Takes indexLocation (VarLocation) which is the int-bank index.
// Takes isAssign (bool) which selects the SET form.
// Takes valueLocation (VarLocation) which is the pre-compiled value for the SET form.
//
// Returns VarLocation which holds the element for the GET form, zero for the SET form.
// Returns bool which is true when the opcode was emitted.
func (c *Compiler) emitFusedArrayFieldIndex(
	ctx context.Context,
	meta fusedArrayFieldMetadata,
	receiverLocation, indexLocation program.VarLocation,
	isAssign bool,
	valueLocation program.VarLocation,
) (program.VarLocation, bool) {
	layoutOperand := safeconv.Uint16ToUint8(meta.layoutIdx)
	var dest uint8
	if isAssign {
		c.boxElementToGeneralTemp(ctx, &valueLocation)
		program.Emit(c.Function, isa.OpSetStructFieldIndexGeneral, receiverLocation.Register, valueLocation.Register, layoutOperand)
	} else {
		dest = c.Scopes.Alloc.Alloc(isa.RegisterGeneral)
		program.Emit(c.Function, isa.OpGetStructFieldIndexGeneral, dest, receiverLocation.Register, layoutOperand)
	}
	program.EmitExtension(c.Function, safeconv.MustIntToUint16(meta.arrayLength), indexLocation.Register)
	program.EmitExtension(c.Function, safeconv.MustIntToUint16(int(meta.elementSize)), safeconv.MustIntToUint8(int(meta.elementKind)))
	if isAssign {
		return program.VarLocation{}, true
	}
	return program.VarLocation{Register: dest, Kind: isa.RegisterGeneral}, true
}

// tryCompileDerefSliceIndex fuses `(*p)[i]` reads and writes into the
// opDerefSliceGet/SetInt opcodes when p is statically a pointer to an int-kinded slice
// and the index is int-kinded (dijkstra-style heap helpers taking *[]int parameters).
//
// The emitted opcode re-reads the slice header through the pointer on every access, so
// append or reslice through the same pointer between accesses behaves identically to the
// composed DEREF+INDEX pair it replaces. All static gates run before any code is emitted,
// so a false return leaves the caller free to compile the generic path.
//
// Takes expression (*ast.IndexExpr) whose X must be a star-dereference.
// Takes isAssign (bool) which selects the SET form.
// Takes valueLocation (VarLocation) which is the pre-compiled value for the SET form.
//
// Returns the element location and true when the fused opcode was emitted (zero value for
// the SET form); (zero, false) when any gate fails.
func (c *Compiler) tryCompileDerefSliceIndex(ctx context.Context, expression *ast.IndexExpr, isAssign bool, valueLocation program.VarLocation) (program.VarLocation, bool) {
	star, ok := ast.Unparen(expression.X).(*ast.StarExpr)
	if !ok {
		return program.VarLocation{}, false
	}
	pointerType, ok := c.underlyingTypeOf(star.X)
	if !ok {
		return program.VarLocation{}, false
	}
	pointer, ok := pointerType.(*types.Pointer)
	if !ok {
		return program.VarLocation{}, false
	}
	slice, ok := pointer.Elem().Underlying().(*types.Slice)
	if !ok || c.kindFor(slice.Elem()) != isa.RegisterInt {
		return program.VarLocation{}, false
	}
	indexType, ok := c.Info.Types[expression.Index]
	if !ok || indexType.Type == nil || c.kindFor(indexType.Type) != isa.RegisterInt {
		return program.VarLocation{}, false
	}
	if isAssign && valueLocation.Kind != isa.RegisterInt {
		return program.VarLocation{}, false
	}

	pointerLocation, err := c.compileExpression(ctx, star.X)
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false
	}
	if pointerLocation.Kind != isa.RegisterGeneral {
		return program.VarLocation{}, false
	}
	indexLocation, err := c.compileExpression(ctx, expression.Index)
	if err != nil {
		c.recordStickyError(err)
		return program.VarLocation{}, false
	}
	if indexLocation.Kind != isa.RegisterInt {
		return program.VarLocation{}, false
	}

	if isAssign {
		c.setDebugPosition(ctx, expression.Lbrack)
		program.Emit(c.Function, isa.OpDerefSliceSetInt, pointerLocation.Register, indexLocation.Register, valueLocation.Register)
		return program.VarLocation{}, true
	}
	dest := c.Scopes.Alloc.Alloc(isa.RegisterInt)
	c.setDebugPosition(ctx, expression.Lbrack)
	program.Emit(c.Function, isa.OpDerefSliceGetInt, dest, pointerLocation.Register, indexLocation.Register)
	return program.VarLocation{Register: dest, Kind: isa.RegisterInt}, true
}

// typedMapOpcodes lists the typed opcodes of one map operation for each bank pair that
// has one: int or string keys, with int, string or general elements.
type typedMapOpcodes struct {
	// intInt is the opcode for int-keyed maps with int elements.
	intInt isa.Opcode

	// intString is the opcode for int-keyed maps with string elements.
	intString isa.Opcode

	// intGeneral is the opcode for int-keyed maps with general elements.
	intGeneral isa.Opcode

	// stringInt is the opcode for string-keyed maps with int elements.
	stringInt isa.Opcode

	// stringString is the opcode for string-keyed maps with string elements.
	stringString isa.Opcode

	// stringGeneral is the opcode for string-keyed maps with general elements.
	stringGeneral isa.Opcode
}

// lookup returns the opcode for a (keyKind, elementKind) pair.
//
// Takes keyKind (isa.RegisterKind) which is the map's declared key kind.
// Takes elementKind (isa.RegisterKind) which is the map's declared element kind.
//
// Returns the typed opcode and true, or (zero, false) when no typed opcode exists.
func (t *typedMapOpcodes) lookup(keyKind, elementKind isa.RegisterKind) (isa.Opcode, bool) {
	var intOp, stringOp, generalOp isa.Opcode
	switch keyKind {
	case isa.RegisterInt:
		intOp, stringOp, generalOp = t.intInt, t.intString, t.intGeneral
	case isa.RegisterString:
		intOp, stringOp, generalOp = t.stringInt, t.stringString, t.stringGeneral
	default:
		return 0, false
	}
	switch elementKind {
	case isa.RegisterInt:
		return intOp, true
	case isa.RegisterString:
		return stringOp, true
	case isa.RegisterGeneral:
		return generalOp, true
	default:
		return 0, false
	}
}

// selectTypedMapGetOpcode picks the typed map-get opcode for a (key, element, index) kind
// tuple.
//
// Takes keyKind (isa.RegisterKind) which is the map's declared key kind.
// Takes elementKind (isa.RegisterKind) which is the map's declared element kind.
// Takes indexKind (isa.RegisterKind) which is the compiled key expression's kind.
//
// Returns the typed opcode and whether a typed opcode applies.
func selectTypedMapGetOpcode(keyKind, elementKind, indexKind isa.RegisterKind) (isa.Opcode, bool) {
	if indexKind != keyKind {
		return 0, false
	}
	return typedMapGetOpcodes.lookup(keyKind, elementKind)
}

// selectTypedMapIndexOkOpcode returns the typed map-index-with-ok opcode for `v, ok :=
// m[k]` when both key and value kinds match a supported (keyKind, valueKind) pair AND the
// compiled key expression has the same kind as the map's declared key.
//
// Takes keyKind (isa.RegisterKind) which is the map's declared key kind.
// Takes valueKind (isa.RegisterKind) which is the map's declared element kind.
// Takes indexKind (isa.RegisterKind) which is the kind of the compiled key expression.
//
// Returns the typed opcode and a bool indicating whether a typed opcode applies.
func selectTypedMapIndexOkOpcode(keyKind, valueKind, indexKind isa.RegisterKind) (isa.Opcode, bool) {
	if indexKind != keyKind {
		return 0, false
	}
	return typedMapIndexOkOpcodes.lookup(keyKind, valueKind)
}

// selectTypedMapSetOpcode returns the typed map-set opcode for the given (keyKind,
// valueKind, observedIndexKind, observedValueKind) tuple, mirroring
// selectTypedMapGetOpcode for assignments.
//
// Takes keyKind (isa.RegisterKind) which is the map's declared key kind.
// Takes valueKind (isa.RegisterKind) which is the map's declared element kind.
// Takes indexKind (isa.RegisterKind) which is the kind of the compiled key expression.
// Takes valueObservedKind (isa.RegisterKind) which is the kind of the compiled value
// expression.
//
// Returns the typed opcode and a bool indicating whether a typed opcode applies.
func selectTypedMapSetOpcode(keyKind, valueKind, indexKind, valueObservedKind isa.RegisterKind) (isa.Opcode, bool) {
	if indexKind != keyKind || valueObservedKind != valueKind {
		return 0, false
	}
	return typedMapSetOpcodes.lookup(keyKind, valueKind)
}

// typeIsStructOrArray reports whether t's underlying type is a struct or array, the
// value-types where copy-on-read matters per Go's value semantics. Pointers, slices,
// maps, chans, funcs are reference-typed and aliasing is the correct Go behaviour.
//
// Takes t (types.Type) which is the type to test.
//
// Returns true when t.Underlying() is *types.Struct or *types.Array.
func typeIsStructOrArray(t types.Type) bool {
	if t == nil {
		return false
	}
	switch t.Underlying().(type) {
	case *types.Struct, *types.Array:
		return true
	default:
		return false
	}
}

// snapshotModeFor picks the snapshot mode for a general-bank store of t.
//
// For pointer/slice/map/chan/func/basic kinds the reflect.Value header copy already gives
// the right semantics, so the runtime kind switch in ValueCopyForBoundary is wasted work.
// For struct/array kinds the helper must run. For interface and type parameter kinds the
// runtime kind is unknown at compile time, so the conservative answer is snapshotDynamic.
//
// Recurses through *types.Named and *types.Alias so user-defined named struct types and
// Go 1.22+ type aliases are classified correctly.
//
// Takes t (types.Type) which is the source operand's static type. May be nil when the
// caller has no static type information available.
//
// Returns the snapshotMode the Compiler should use to pick the move opcode.
func snapshotModeFor(t types.Type) snapshotMode {
	if t == nil {
		return snapshotDynamic
	}
	switch u := t.Underlying().(type) {
	case *types.Struct, *types.Array:
		return snapshotAlways
	case *types.Interface:
		return snapshotDynamic
	case *types.TypeParam:
		return snapshotDynamic
	case *types.Basic:
		return snapshotNever
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature:
		return snapshotNever
	case *types.Tuple:
		return snapshotDynamic
	case *types.Named:
		return snapshotModeFor(u.Underlying())
	default:
		return snapshotDynamic
	}
}
