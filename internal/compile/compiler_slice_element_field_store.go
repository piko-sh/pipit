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

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// sliceElementFieldTarget is the compiled target of a fused store into a field of a slice
// element.
type sliceElementFieldTarget struct {
	// classified says which field is stored into and which fused read matches it.
	classified sliceIndexStructFieldClassification

	// slice holds the slice, in the general bank.
	slice program.VarLocation

	// index holds the index, in the int bank.
	index program.VarLocation

	// store is the fused store opcode.
	store isa.Opcode
}

// classifySliceElementFieldStore reports whether an assignment to `slice[i].Field` of a
// value of one kind can be one fused store.
//
// Takes target (*ast.SelectorExpr) which is the field being assigned.
// Takes valueKind (isa.RegisterKind) which is the bank the stored value is in.
//
// Returns sliceIndexStructFieldClassification which names the field.
// Returns isa.Opcode which is the fused store.
// Returns bool which is false when the target is anything else, the field is of another
// kind than the value, or no fused store exists for the kind.
func (c *Compiler) classifySliceElementFieldStore(
	ctx context.Context, target *ast.SelectorExpr, valueKind isa.RegisterKind,
) (sliceIndexStructFieldClassification, isa.Opcode, bool) {
	selection := c.Info.Selections[target]
	if selection == nil || selection.Kind() != types.FieldVal {
		return sliceIndexStructFieldClassification{}, 0, false
	}
	store, hasStore := isaselect.PickSliceIndexStructFieldStoreOp(valueKind)
	if !hasStore || !isaselect.StructFieldFastPathWriteKindEnabled(valueKind) {
		return sliceIndexStructFieldClassification{}, 0, false
	}
	classified, eligible := c.classifySliceIndexStructField(ctx, target, selection)
	if !eligible || classified.resultKind != valueKind {
		return sliceIndexStructFieldClassification{}, 0, false
	}
	if isa.RegisterKind(c.Function.StructLayoutTable[classified.layoutIdx].RegisterKind) != valueKind {
		return sliceIndexStructFieldClassification{}, 0, false
	}
	return classified, store, true
}

// compileSliceElementFieldTarget compiles the slice and the index of a fused read or
// store, in that order.
//
// Once they have been compiled the fusion is committed. An index of any integer type is
// brought to the int bank, since declining after emission would leave the caller to
// compile the operands, and any calls in them, a second time.
//
// Takes classified (sliceIndexStructFieldClassification) which names the field.
// Takes store (isa.Opcode) which is the fused store, or zero for a target that is only
// read.
//
// Returns sliceElementFieldTarget which holds the compiled operands.
// Returns error when an operand fails to compile or the index is not an integer.
func (c *Compiler) compileSliceElementFieldTarget(
	ctx context.Context, classified sliceIndexStructFieldClassification, store isa.Opcode,
) (sliceElementFieldTarget, error) {
	sliceLocation, err := c.compileExpression(ctx, classified.indexExpr.X)
	if err != nil {
		return sliceElementFieldTarget{}, err
	}
	c.boxToGeneral(ctx, &sliceLocation)
	indexLocation, err := c.compileExpression(ctx, classified.indexExpr.Index)
	if err != nil {
		return sliceElementFieldTarget{}, err
	}
	c.ensureIntRegister(ctx, &indexLocation)
	if indexLocation.Kind != isa.RegisterInt64 {
		return sliceElementFieldTarget{}, fmt.Errorf(
			"%w: fused slice-field index compiled to %s, expected int at %s",
			fault.ErrCompilation, indexLocation.Kind, c.positionString(classified.indexExpr.Index.Pos()))
	}
	return sliceElementFieldTarget{slice: sliceLocation, index: indexLocation, classified: classified, store: store}, nil
}

// emitSliceElementFieldStore emits the fused store of a value into the target's field.
//
// Takes target (sliceElementFieldTarget) which holds the compiled slice and index.
// Takes value (program.VarLocation) which holds the value, in the field's bank.
func (c *Compiler) emitSliceElementFieldStore(target sliceElementFieldTarget, value program.VarLocation) {
	element := c.Scopes.Alloc.AllocTemp(isa.RegisterGeneral)
	program.Emit(c.Function, target.store, value.Register, target.slice.Register, target.index.Register)
	c.emitStructFieldLayoutExtensionNaming(target.classified.layoutIdx, element)
}

// emitSliceElementFieldRead emits the fused read of the target's field into a register of
// its bank.
//
// Takes target (sliceElementFieldTarget) which holds the compiled slice and index.
// Takes destination (uint8) which is the register to read into.
//
// Returns program.VarLocation which holds the field's value.
func (c *Compiler) emitSliceElementFieldRead(target sliceElementFieldTarget, destination uint8) program.VarLocation {
	program.Emit(c.Function, target.classified.op, destination, target.slice.Register, target.index.Register)
	c.emitStructFieldLayoutExtension(target.classified.layoutIdx)
	return program.VarLocation{Register: destination, Kind: target.classified.resultKind}
}

// tryCompileCompoundAssignSliceElementField compiles `slice[i].Field op= v` as a fused
// read, the operation and a fused store, with the slice and the index evaluated once.
//
// The fused read comes before the right-hand side, so an index out of range still fails
// before the right-hand side is evaluated. That reads the field earlier than the plain
// form does, which nothing can tell apart when the right-hand side makes no call and
// receives from no channel. Any other right-hand side is left to the plain form. So are
// the shifts, whose count is of another type than the field.
//
// Takes target (*ast.SelectorExpr) which is the field being assigned.
// Takes rightHandSide (ast.Expr) which is the right-hand operand.
// Takes binaryOperation (token.Token) which is the underlying binary operator.
//
// Returns program.VarLocation which holds the assignment result.
// Returns bool which is false when nothing was emitted and the plain form applies.
// Returns error when an operand fails to compile.
func (c *Compiler) tryCompileCompoundAssignSliceElementField(
	ctx context.Context, target *ast.SelectorExpr, rightHandSide ast.Expr, binaryOperation token.Token,
) (program.VarLocation, bool, error) {
	if isShiftOperation(binaryOperation) || c.exprHasCallOrReceive(rightHandSide) {
		return program.VarLocation{}, false, nil
	}
	fieldKind := c.kindFor(c.Info.TypeOf(target))
	if c.kindFor(c.Info.TypeOf(rightHandSide)) != fieldKind {
		return program.VarLocation{}, false, nil
	}
	classified, store, fused := c.classifySliceElementFieldStore(ctx, target, fieldKind)
	if !fused {
		return program.VarLocation{}, false, nil
	}
	elementField, err := c.compileSliceElementFieldTarget(ctx, classified, store)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	current := c.emitSliceElementFieldRead(elementField, c.Scopes.Alloc.AllocTemp(classified.resultKind))
	operand, err := c.compileCompoundOperand(ctx, binaryOperation, rightHandSide)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	result, err := c.emitBinaryOp(ctx, binaryOperation, current, operand)
	if err != nil {
		return program.VarLocation{}, true, err
	}
	if result.Kind != fieldKind {
		return program.VarLocation{}, true, fmt.Errorf(
			"%w: compound assignment to a slice element's field produced %s, expected %s at %s",
			fault.ErrCompilation, result.Kind, fieldKind, c.positionString(target.Pos()))
	}
	c.emitSliceElementFieldStore(elementField, result)
	return program.VarLocation{}, true, nil
}
