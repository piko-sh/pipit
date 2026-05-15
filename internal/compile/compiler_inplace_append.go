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
// Picks the right in-place opcode for the element kind.
//
// Takes location (VarLocation) which is the destination slot.
// Takes callRHS (*ast.CallExpr) which is the append call expression.
// Takes elementKind (string) which is the element type's underlying Go type name.
//
// Returns VarLocation which is the destination location when applied.
// Returns bool which reports whether the fast path emitted code.
// Returns error when sub-expression compilation of the element fails.
func (c *Compiler) emitInPlaceAppendSingle(ctx context.Context, location program.VarLocation, callRHS *ast.CallExpr, elementKind string) (program.VarLocation, bool, error) {
	valueLocation, err := c.compileExpression(ctx, callRHS.Args[1])
	if err != nil {
		return program.VarLocation{}, true, err
	}

	switch elementKind {
	case "byte", "uint8":
		if valueLocation.Kind != isa.RegisterUint {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "Uint", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpAppendByteFastInPlace, location.Register, location.Register, valueLocation.Register)
		return location, true, nil
	case "uint16", "uint32", "uint64", "uint", "uintptr":
		if valueLocation.Kind != isa.RegisterUint {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "Uint", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendUintInPlace), location.Register, location.Register)
		program.Emit(c.Function, isa.OpExt, valueLocation.Register, 0, 0)
		return location, true, nil
	case "int", "int8", "int16", "int32", "int64":
		if valueLocation.Kind != isa.RegisterInt {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "Int", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpAppendIntFast, location.Register, location.Register, valueLocation.Register)
		return location, true, nil
	case "string":
		if valueLocation.Kind != isa.RegisterString {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "String", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpAppendStringFast, location.Register, location.Register, valueLocation.Register)
		return location, true, nil
	case "float32", "float64":
		if valueLocation.Kind != isa.RegisterFloat {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "Float", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpAppendFloatFast, location.Register, location.Register, valueLocation.Register)
		return location, true, nil
	case "bool":
		if valueLocation.Kind != isa.RegisterBool {
			c.noteLoweringRefused(ctx, loweringTableAssign, loweringInPlaceAppend, reasonValueKind, attrWant, "Bool", attrGot, valueLocation.Kind.String())
			return program.VarLocation{}, false, nil
		}
		program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendBool), location.Register, location.Register)
		program.Emit(c.Function, isa.OpExt, valueLocation.Register, 0, 0)
		return location, true, nil
	}

	if isInterfaceElementKind(elementKind) {
		return program.VarLocation{}, false, nil
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
		if c.emitStructFastAppendInto(ctx, location.Register, location, valueLocation, sliceTypeInfo.Type) {
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
	if elementKind != "byte" && elementKind != "uint8" {
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
	elementName := sourceSlice.Elem().Underlying().String()
	if elementName != "byte" && elementName != "uint8" {
		return program.VarLocation{}, false, nil
	}

	valueLocation, err := c.compileExpression(ctx, callRHS.Args[1])
	if err != nil {
		return program.VarLocation{}, true, err
	}
	c.boxToGeneralTemp(ctx, &valueLocation)
	program.Emit(c.Function, isa.OpDrillTier1, uint8(isa.SubOpAppendByteSpreadInPlace), location.Register, valueLocation.Register)
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
