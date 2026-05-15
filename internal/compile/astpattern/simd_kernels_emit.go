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

package astpattern

import (
	"context"
	"go/ast"
	"go/token"

	"pipit.sh/pipit/internal/compile/patterns"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

// resolvedSliceOperand pairs an operand's source expression with the typed-bank register
// resolveFloat64SliceOperand assigned it. emitSimdCountRegister uses these to Emit
// LEN_SLICE_FLOAT_DIRECT against the operand named in the loop's len() bound rather than
// assuming the bound is always sliceA.
type resolvedSliceOperand struct {
	// expr is the operand's source-level identifier expression.
	expr ast.Expr

	// register is the typed slicesFloat register resolveFloat64SliceOperand assigned it.
	register uint8
}

// emitSimdDotProductFloat64 emits the dot-product SIMD opcode.
//
// Computes `destinationScalar += dot(sliceA, sliceB)` via one tier-1 SIMD opcode plus one
// extension word. Both slice operands must resolve to the typed slicesFloat bank; the
// recogniser falls through to the standard scalar loop emission when either operand is on
// the reflect general-bank path (slice literals, function returns of unknown type, etc.).
// The destination is the scalar float register already holding `sum`. The count operand
// is materialised as a fresh int register populated either from len(slice) (LtLen shape)
// or a constant load (LtConst shape).
//
// Takes c (patterns.Emitter) which carries the active Emit state.
// Takes match (simdKernelMatch) which holds the matched operand expressions and bound
// info.
//
// Returns VarLocation which is always zero since for-statements do not produce values.
// Returns bool which is true when the SIMD opcode was emitted.
// Returns error when compilation fails.
func emitSimdDotProductFloat64(c patterns.Emitter, match simdKernelMatch) (program.VarLocation, bool, error) {
	destLocation, ok := resolveScalarDestination(c, match.destinationScalar)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	sliceALocation, ok, err := resolveFloat64SliceOperand(c, match.sliceA)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	sliceBLocation, ok, err := resolveFloat64SliceOperand(c, match.sliceB)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	countRegister, ok, err := emitSimdCountRegister(
		c,
		match,
		resolvedSliceOperand{expr: match.sliceA, register: sliceALocation.Register},
		resolvedSliceOperand{expr: match.sliceB, register: sliceBLocation.Register},
	)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpSimdDotProductFloat64), destLocation.Register, sliceALocation.Register)
	program.Emit(c.CurrentFunction(), isa.OpExt, sliceBLocation.Register, countRegister, 0)
	return program.VarLocation{}, true, nil
}

// emitSimdSumSliceFloat64 emits one tier-1 SIMD opcode (with one extension word for
// count) that computes `destinationScalar += sum(sliceA[:count])`. Refuses when sliceA is
// not on the typed slicesFloat bank.
//
// Takes c (patterns.Emitter).
// Takes match (simdKernelMatch).
//
// Returns the zero VarLocation, ok=true when the SIMD opcode was emitted, and any
// compilation error.
func emitSimdSumSliceFloat64(c patterns.Emitter, match simdKernelMatch) (program.VarLocation, bool, error) {
	destLocation, ok := resolveScalarDestination(c, match.destinationScalar)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	sliceALocation, ok, err := resolveFloat64SliceOperand(c, match.sliceA)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	countRegister, ok, err := emitSimdCountRegister(c, match, resolvedSliceOperand{expr: match.sliceA, register: sliceALocation.Register})
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpSimdSumSliceFloat64), destLocation.Register, sliceALocation.Register)
	program.Emit(c.CurrentFunction(), isa.OpExt, countRegister, 0, 0)
	return program.VarLocation{}, true, nil
}

// emitSimdAddSliceFloat64 emits one tier-1 SIMD opcode (with one extension word carrying
// sliceB and count) that computes `destinationSlice[i] = sliceA[i] + sliceB[i]` for the
// first `count` indices. All three slices must be on the typed slicesFloat bank.
//
// Takes c (patterns.Emitter).
// Takes match (simdKernelMatch).
//
// Returns the zero VarLocation, ok=true when the SIMD opcode was emitted, and any
// compilation error.
func emitSimdAddSliceFloat64(c patterns.Emitter, match simdKernelMatch) (program.VarLocation, bool, error) {
	destLocation, ok, err := resolveFloat64SliceOperand(c, match.destinationSlice)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	sliceALocation, ok, err := resolveFloat64SliceOperand(c, match.sliceA)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	sliceBLocation, ok, err := resolveFloat64SliceOperand(c, match.sliceB)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	countRegister, ok, err := emitSimdCountRegister(
		c,
		match,
		resolvedSliceOperand{expr: match.sliceA, register: sliceALocation.Register},
		resolvedSliceOperand{expr: match.sliceB, register: sliceBLocation.Register},
	)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpSimdAddSliceFloat64), destLocation.Register, sliceALocation.Register)
	program.Emit(c.CurrentFunction(), isa.OpExt, sliceBLocation.Register, countRegister, 0)
	return program.VarLocation{}, true, nil
}

// emitSimdScaleSliceFloat64 emits one tier-1 SIMD opcode (with one extension word for
// count) that multiplies the first `count` elements of sliceA in place by scalarOperand.
// Refuses when sliceA is not on the typed slicesFloat bank.
//
// Takes c (patterns.Emitter).
// Takes match (simdKernelMatch).
//
// Returns the zero VarLocation, ok=true when the SIMD opcode was emitted, and any
// compilation error.
func emitSimdScaleSliceFloat64(ctx context.Context, c patterns.Emitter, match simdKernelMatch) (program.VarLocation, bool, error) {
	sliceLocation, ok, err := resolveFloat64SliceOperand(c, match.sliceA)
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	scalarLocation, err := c.CompileExpression(ctx, match.scalarOperand)
	if err != nil {
		return program.VarLocation{}, false, err
	}
	if scalarLocation.Kind != isa.RegisterFloat {
		return program.VarLocation{}, false, nil
	}
	countRegister, ok, err := emitSimdCountRegister(c, match, resolvedSliceOperand{expr: match.sliceA, register: sliceLocation.Register})
	if !ok || err != nil {
		return program.VarLocation{}, false, err
	}
	program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpSimdScaleSliceFloat64), sliceLocation.Register, scalarLocation.Register)
	program.Emit(c.CurrentFunction(), isa.OpExt, countRegister, 0, 0)
	return program.VarLocation{}, true, nil
}

// resolveOuterLoopVarWriteTarget inspects the loop's init clause to decide whether the
// optimised emission must write the loop variable's Go-mandated post-loop value. A
// pre-declared `for i = 0` shape requires writeback because Go observes the outer
// variable after the loop.
//
// Takes c (patterns.Emitter) which owns the scope table.
// Takes statement (*ast.ForStmt) whose init clause is inspected.
//
// Returns target (VarLocation) which is the outer loop variable to write.
// Returns needsWriteback (bool) which reports whether the writeback is required.
// Returns ok (bool) which reports whether the optimised path may proceed.
func resolveOuterLoopVarWriteTarget(c patterns.Emitter, statement *ast.ForStmt) (target program.VarLocation, needsWriteback, ok bool) {
	assign, ok := statement.Init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 {
		return program.VarLocation{}, false, true
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return program.VarLocation{}, false, true
	}
	location, found := c.ScopeStack().LookupVar(ident.Name)
	if !found || location.Kind != isa.RegisterInt || location.IsSpilled || location.IsIndirect || location.IsCaptured {
		return program.VarLocation{}, false, false
	}
	return location, true, true
}

// emitSimdLoopVarWriteback stores the loop's Go-mandated post-loop value into the
// pre-declared outer loop variable after the kernel opcode.
//
// Takes c (patterns.Emitter) which owns the emission target.
// Takes match (simdKernelMatch) which carries the recognised loop bound shape.
// Takes outerRegister (uint8) which is the register receiving the post-loop value.
//
// Returns error when the bound operand cannot be resolved or a constant cannot be added.
func emitSimdLoopVarWriteback(c patterns.Emitter, match simdKernelMatch, outerRegister uint8) error {
	switch match.boundShape {
	case simdBoundLenSlice:
		boundLocation, ok, err := resolveFloat64SliceOperand(c, match.boundSlice)
		if err != nil || !ok {
			return err
		}
		program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLenSliceFloatDirect), outerRegister, boundLocation.Register)
	case simdBoundConst:
		if match.boundConstValue < 0 {
			return nil
		}
		if match.boundConstValue <= maxUint8Value {
			program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), outerRegister, uint8(match.boundConstValue))
			return nil
		}
		constantIndex, err := program.AddIntConstant(c.CurrentFunction(), match.boundConstValue)
		if err != nil {
			return err
		}
		program.EmitWide(c.CurrentFunction(), isa.OpLoadIntConst, outerRegister, constantIndex)
	default:
	}
	return nil
}

// selectBoundSliceRegister returns the slicesFloat register of the loop's bound operand.
//
// The recogniser guarantees (via isOneOfSlices in Match) that the bound slice is one of
// the resolved operands, so this matches match.boundSlice by name.
//
// Takes match (simdKernelMatch) which names the operand used as the loop bound.
// Takes operands ([]resolvedSliceOperand) which are the resolved slice operands.
//
// Returns uint8 which is the bound operand's register.
// Returns bool which is false when no operand matches (the caller treats that as a
// refusal).
func selectBoundSliceRegister(match simdKernelMatch, operands []resolvedSliceOperand) (uint8, bool) {
	boundIdent, ok := match.boundSlice.(*ast.Ident)
	if !ok {
		return 0, false
	}
	for _, operand := range operands {
		identExpr, isIdent := operand.expr.(*ast.Ident)
		if isIdent && identExpr.Name == boundIdent.Name {
			return operand.register, true
		}
	}
	return 0, false
}

// emitSimdCountRegister materialises the runtime iteration count into a fresh int
// register, triggering refusal when the count cannot be represented (for instance a
// negative constant).
//
// Takes c (patterns.Emitter) which carries the active Emit state.
// Takes match (simdKernelMatch) which holds the bound info.
// Takes operands (...resolvedSliceOperand) which are the resolved operand slices.
//
// Returns uint8 which is the count int register.
// Returns bool which is true when emission succeeded.
// Returns error when constant pool allocation fails.
func emitSimdCountRegister(c patterns.Emitter, match simdKernelMatch, operands ...resolvedSliceOperand) (uint8, bool, error) {
	countRegister := c.ScopeStack().Alloc.Alloc(isa.RegisterInt)
	switch match.boundShape {
	case simdBoundLenSlice:
		boundRegister, ok := selectBoundSliceRegister(match, operands)
		if !ok {
			return 0, false, nil
		}
		program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLenSliceFloatDirect), countRegister, boundRegister)
		return countRegister, true, nil
	case simdBoundConst:
		if match.boundConstValue < 0 {
			return 0, false, nil
		}
		if match.boundConstValue <= maxUint8Value {
			program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), countRegister, uint8(match.boundConstValue))
			return countRegister, true, nil
		}
		constantIndex, err := program.AddIntConstant(c.CurrentFunction(), match.boundConstValue)
		if err != nil {
			return 0, false, err
		}
		program.EmitWide(c.CurrentFunction(), isa.OpLoadIntConst, countRegister, constantIndex)
		return countRegister, true, nil
	default:
	}
	return 0, false, nil
}

// resolveScalarDestination looks up the register holding the already-declared scalar
// destination of a reduction kernel.
//
// Takes c (patterns.Emitter).
// Takes destinationExpression (ast.Expr) which names the float64 destination variable.
//
// Returns the location and true on success.
func resolveScalarDestination(c patterns.Emitter, destinationExpression ast.Expr) (program.VarLocation, bool) {
	ident, ok := destinationExpression.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, false
	}
	location, found := c.ScopeStack().LookupVar(ident.Name)
	if !found {
		return program.VarLocation{}, false
	}
	if location.Kind != isa.RegisterFloat {
		return program.VarLocation{}, false
	}
	return location, true
}

// resolveFloat64SliceOperand resolves a typed slicesFloat operand. General-bank operands
// are adopted into a fresh slicesFloat register, while spilled and indirect operands fall
// back to scalar emission.
//
// Takes c (patterns.Emitter) which carries the active Emit state.
// Takes operandExpression (ast.Expr) which names the []float64 variable.
//
// Returns VarLocation for the typed-bank location, true when resolved, and error on
// adoption failure.
func resolveFloat64SliceOperand(c patterns.Emitter, operandExpression ast.Expr) (program.VarLocation, bool, error) {
	ident, ok := operandExpression.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	location, found := c.ScopeStack().LookupVar(ident.Name)
	if !found {
		return program.VarLocation{}, false, nil
	}
	if location.IsSpilled || location.IsIndirect {
		return program.VarLocation{}, false, nil
	}
	switch location.Kind {
	case isa.RegisterSliceFloat:
		return location, true, nil
	case isa.RegisterGeneral:
		adoptedRegister := c.ScopeStack().Alloc.Alloc(isa.RegisterSliceFloat)
		program.Emit(c.CurrentFunction(), isa.OpDrillTier1, uint8(isa.SubOpAdoptGeneralToSlicesFloat), adoptedRegister, location.Register)
		return program.VarLocation{Register: adoptedRegister, Kind: isa.RegisterSliceFloat}, true, nil
	default:
	}
	return program.VarLocation{}, false, nil
}
