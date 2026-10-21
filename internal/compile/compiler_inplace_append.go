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
	"go/token"
	"go/types"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// interfacePrefix is the leading substring of an interface{...} type description as
	// printed by types.Type.String, used by isInterfaceElementKind to recognise non-empty
	// interface element types whose method set is spelt out.
	interfacePrefix = "interface{"
)

// byteElementKinds are the underlying type names of a byte element, which has its own
// in-place opcodes.
var byteElementKinds = []string{"byte", "uint8"}

// inPlaceAppendForm is the typed in-place append for one element kind.
type inPlaceAppendForm struct {
	// emit appends the element in register value to the slice in register slice.
	emit func(c *Compiler, slice, value uint8)

	// bank is the register bank the element is read from.
	bank isa.RegisterKind
}

// inPlaceAppendForms maps an element's underlying type name to its typed in-place form.
// An element kind missing here is boxed and appended through the general-bank opcode.
var inPlaceAppendForms = func() map[string]inPlaceAppendForm {
	byteForm := inPlaceAppendForm{bank: isa.RegisterUint64, emit: func(c *Compiler, slice, value uint8) {
		program.Emit(c.Function, isa.OpAppendByteInPlace, slice, slice, value)
	}}
	uintForm := inPlaceAppendForm{bank: isa.RegisterUint64, emit: func(c *Compiler, slice, value uint8) {
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendUint64WideInPlace), slice, slice)
		program.Emit(c.Function, isa.OpExtension, value, 0, 0)
	}}
	intForm := inPlaceAppendForm{bank: isa.RegisterInt64, emit: func(c *Compiler, slice, value uint8) {
		program.Emit(c.Function, isa.OpAppendInt64, slice, slice, value)
	}}
	floatForm := inPlaceAppendForm{bank: isa.RegisterFloat64, emit: func(c *Compiler, slice, value uint8) {
		program.Emit(c.Function, isa.OpAppendFloat64, slice, slice, value)
	}}
	forms := map[string]inPlaceAppendForm{
		"uint16":  uintForm,
		"uint32":  uintForm,
		"uint64":  uintForm,
		"uint":    uintForm,
		"uintptr": uintForm,
		"int":     intForm,
		"int8":    intForm,
		"int16":   intForm,
		"int32":   intForm,
		"int64":   intForm,
		"float32": floatForm,
		"float64": floatForm,
		"string": {bank: isa.RegisterString, emit: func(c *Compiler, slice, value uint8) {
			program.Emit(c.Function, isa.OpAppendString, slice, slice, value)
		}},
		"bool": {bank: isa.RegisterBool, emit: func(c *Compiler, slice, value uint8) {
			program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendBoolWide), slice, slice)
			program.Emit(c.Function, isa.OpExtension, value, 0, 0)
		}},
	}
	for _, name := range byteElementKinds {
		forms[name] = byteForm
	}
	return forms
}()

// tryCompileInPlaceAppend emits an in-place append for `x = append(x, e)` or `x =
// append(x, source...)` when the local is alias-free and satisfies the in-place safety
// predicate.
//
// Takes leftHandSide (ast.Expr) which is the LHS of the assignment.
// Takes rightHandSide (ast.Expr) which is the RHS expression.
//
// Returns VarLocation which is the destination when applied.
// Returns bool which reports whether the fast path emitted code.
// Returns error when element compilation fails.
func (c *Compiler) tryCompileInPlaceAppend(ctx context.Context, leftHandSide ast.Expr, rightHandSide ast.Expr) (program.VarLocation, bool, error) {
	match := c.inPlaceAppendCandidate(leftHandSide, rightHandSide)
	if !match.ok {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, match.refusal)
		return program.VarLocation{}, false, nil
	}

	if match.spread {
		return c.emitInPlaceAppendSpread(ctx, match.location, match.callRHS, match.elementKind)
	}
	return c.emitInPlaceAppendSingle(ctx, match.location, match.callRHS, match.elementKind)
}

// emitInPlaceAppendSingle emits the `x = append(x, e)` in-place form.
//
// All fallback decisions precede element compilation because the ordinary append path
// compiles the element again. Refusing afterwards would evaluate the element twice.
//
// Takes location (VarLocation) which is the destination slot.
// Takes callRHS (*ast.CallExpr) which is the append call expression.
// Takes elementKind (string) which is the element type's underlying Go type name.
//
// Returns VarLocation which is the destination location when applied.
// Returns bool which reports whether the fast path emitted code.
// Returns error when sub-expression compilation of the element fails.
func (c *Compiler) emitInPlaceAppendSingle(ctx context.Context, location program.VarLocation, callRHS *ast.CallExpr, elementKind string) (program.VarLocation, bool, error) {
	if isInterfaceElementKind(elementKind) {
		c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, "interface-element")
		return program.VarLocation{}, false, nil
	}
	form, typed := inPlaceAppendForms[elementKind]
	if typed {
		if got := c.kindFor(c.Info.TypeOf(callRHS.Args[1])); got != form.bank {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, form.bank.String(), attrGot, got.String())
			return program.VarLocation{}, false, nil
		}
	}

	valueLocation, err := c.compileExpression(ctx, callRHS.Args[1])
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if typed {
		valueLocation = c.coerceToKind(ctx, valueLocation, form.bank)
		if valueLocation.Kind == form.bank {
			form.emit(c, location.Register, valueLocation.Register)
			return location, true, nil
		}
	}
	c.boxToGeneralTemp(ctx, &valueLocation)
	c.emitInPlaceGeneralAppend(ctx, location, valueLocation, callRHS)
	return location, true, nil
}

// emitInPlaceGeneralAppend emits the general-bank form of a same-register append: the
// struct fast opcode when the element is a pointer-free struct or array, otherwise the
// generic in-place opcode.
//
// Takes location (VarLocation) which is the slice's own general register.
// Takes valueLocation (VarLocation) which is the boxed element.
// Takes callRHS (*ast.CallExpr) which is the append call, used to recover the slice type.
func (c *Compiler) emitInPlaceGeneralAppend(ctx context.Context, location, valueLocation program.VarLocation, callRHS *ast.CallExpr) {
	if sliceTypeInfo, found := c.Info.Types[callRHS.Args[0]]; found {
		if c.emitAppendStructInto(ctx, location.Register, location, valueLocation, sliceTypeInfo.Type) {
			return
		}
	}
	program.Emit(c.Function, isa.OpAppendInPlace, location.Register, location.Register, valueLocation.Register)
}

// emitInPlaceAppendSpread emits the `x = append(x, source...)` form.
//
// Emits the in-place byte-spread sub-op only when both the destination's element type and
// the source's static type are []byte (this rules out Go's special-case `append([]byte,
// string...)` which the byte-fast handler does not model). For all other shapes, returns
// (zero, false, nil) so the generic compileBuiltinAppend path emits the standard
// allocate-fresh-slot opcode.
//
// Takes location (VarLocation) which is the destination slot.
// Takes callRHS (*ast.CallExpr) which is the append call expression.
// Takes elementKind (string) which is the element type's underlying Go type name.
//
// Returns VarLocation which is the destination location when applied.
// Returns bool which reports whether the fast path emitted code.
// Returns error when sub-expression compilation of the source fails.
func (c *Compiler) emitInPlaceAppendSpread(ctx context.Context, location program.VarLocation, callRHS *ast.CallExpr, elementKind string) (program.VarLocation, bool, error) {
	if callRHS.Ellipsis == token.NoPos {
		return program.VarLocation{}, false, nil
	}
	if !slices.Contains(byteElementKinds, elementKind) {
		return program.VarLocation{}, false, nil
	}
	sourceTypeInfo, ok := c.Info.Types[callRHS.Args[1]]
	if !ok || sourceTypeInfo.Type == nil {
		return program.VarLocation{}, false, nil
	}
	sourceSlice, ok := sourceTypeInfo.Type.Underlying().(*types.Slice)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	if !slices.Contains(byteElementKinds, sourceSlice.Elem().Underlying().String()) {
		return program.VarLocation{}, false, nil
	}

	valueLocation, err := c.compileExpression(ctx, callRHS.Args[1])
	if err != nil {
		return program.VarLocation{}, true, err
	}
	c.boxToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendSpreadByteInPlace), location.Register, valueLocation.Register)
	return location, true, nil
}

// isInterfaceElementKind reports whether elementKind names an interface type. The
// in-place fast path excludes interfaces because a nil-source fallback would produce a
// concretely-typed slice, not the declared interface type.
//
// Takes elementKind (string) which is the element's underlying Go type name.
//
// Returns bool which is true when the element is an interface.
func isInterfaceElementKind(elementKind string) bool {
	if elementKind == "interface{}" || elementKind == "any" {
		return true
	}
	return strings.HasPrefix(elementKind, interfacePrefix)
}
