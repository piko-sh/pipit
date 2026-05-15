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
	"os"

	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/logging"
)

const (
	// loweringTableAssign is the trace name of the assignment lowering table.
	loweringTableAssign = "assign"

	// loweringRoute is the pseudo-lowering that records which assignment route was taken.
	loweringRoute = "route"

	// loweringInPlaceAppend names the in-place append lowering.
	loweringInPlaceAppend = "inplace-append"

	// loweringStructIntoCollection names the struct-into-collection lowering.
	loweringStructIntoCollection = "struct-into-collection"

	// reasonValueKind is the refusal reason for a value in an unexpected register bank.
	reasonValueKind = "value-kind"

	// attrWant labels the expected side of a refusal note.
	attrWant = "want"

	// attrGot labels the actual side of a refusal note.
	attrGot = "got"
)

// traceLoweringEnabled mirrors PIPIT_TRACE_LOWERING, read once at start-up so the hot
// path pays one branch per applied lowering.
var traceLoweringEnabled = os.Getenv("PIPIT_TRACE_LOWERING") != ""

// assignLowering is one specialised form of a single-value assignment.
type assignLowering struct {
	// try emits the specialised form and reports applied=true when it did. A try that
	// declines may still return a non-nil error; the chain drops it and moves on, which is
	// the behaviour the hand-written chain had (tightening it is a separate change).
	try func(c *Compiler, ctx context.Context, leftHandSide, rightHandSide ast.Expr) (program.VarLocation, bool, error)

	// name identifies the row in traces.
	name string
}

// assignLowerings are the specialised forms of `lhs = rhs`, tried before the general
// assignment.
var assignLowerings []assignLowering

// shortVarDeclLowering is one specialised form of a short variable declaration.
type shortVarDeclLowering struct {
	// try emits the specialised form. The chain stops on handled=true or a non-nil error;
	// each try checks the arity it handles itself.
	try func(c *Compiler, ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error)

	// name identifies the row in traces.
	name string
}

// shortVarDeclLowerings are the specialised forms of `a, b := rhs`, tried before the
// sequential declaration. Order: the multi-value form is checked first because it owns
// every `a, b := f()` shape; the destructured slice element form handles only `x :=
// s[i]`.
var shortVarDeclLowerings []shortVarDeclLowering

// indexLowering is one specialised form of a read through an index expression.
type indexLowering struct {
	// try emits the specialised form and reports whether it did.
	try func(c *Compiler, ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, bool)

	// name identifies the row in traces.
	name string
}

// indexLowerings are the specialised forms of `x[i]` as a value, tried before the general
// collection read. Order: constant string folding first because it emits no instructions
// at all; the fused array-field form before the deref-slice form because a `p.arr[i]`
// shape satisfies both and the fused form skips the field load.
var indexLowerings []indexLowering

// selectorFieldLowering is one specialised form of reading a struct field through a
// selector whose receiver sits in the general bank.
type selectorFieldLowering struct {
	// try emits the specialised form and reports whether it did.
	try func(c *Compiler, ctx context.Context, selection *types.Selection, receiver program.VarLocation, resultKind isa.RegisterKind) (program.VarLocation, bool)

	// name identifies the row in traces.
	name string
}

// selectorFieldLowerings are the fast paths for a field read, tried before the per-hop
// OpGetField chain. Order: the typed-slice field read first because it lands the header
// straight in a typed slice bank; the scalar field read only when the leaf kind has a
// fast-path opcode.
var selectorFieldLowerings []selectorFieldLowering

// noteLowering logs that a named lowering applied when tracing is on.
//
// Takes table (string) which names the table the lowering came from.
// Takes name (string) which is the lowering's row name.
func (c *Compiler) noteLowering(ctx context.Context, table, name string) {
	if !traceLoweringEnabled {
		return
	}
	logging.LoggerFrom(ctx).Debug("lowering applied",
		"table", table, "lowering", name, "function", c.Function.Name, "pc", program.CurrentPC(c.Function))
}

// noteLoweringRefused logs why a named lowering declined when tracing is on.
//
// Refusals matter as much as applications when two compilers are compared: the
// self-hosting lane diffs both compilers' traces to find the first decision that differs.
//
// Takes table (string) which names the table the lowering came from.
// Takes name (string) which is the lowering's row name.
// Takes reason (string) which names the gate that declined.
// Takes attrs (...any) which are extra slog key-value pairs describing the gate's inputs.
func (c *Compiler) noteLoweringRefused(ctx context.Context, table, name, reason string, attrs ...any) {
	if !traceLoweringEnabled {
		return
	}
	fields := append([]any{"table", table, "lowering", name, "reason", reason, "function", c.Function.Name, "pc", program.CurrentPC(c.Function)}, attrs...)
	logging.LoggerFrom(ctx).Debug("lowering refused", fields...)
}

// tryAssignLowerings runs assignLowerings in order.
//
// Takes leftHandSide (ast.Expr) which is the assignment target.
// Takes rightHandSide (ast.Expr) which is the assigned expression.
//
// Returns program.VarLocation which is the assignment's result.
// Returns bool which is true when a lowering applied.
// Returns error when the lowering that applied fails.
func (c *Compiler) tryAssignLowerings(ctx context.Context, leftHandSide, rightHandSide ast.Expr) (program.VarLocation, bool, error) {
	for _, lowering := range assignLowerings {
		location, applied, err := lowering.try(c, ctx, leftHandSide, rightHandSide)
		c.noteLoweringRefused(ctx, loweringTableAssign, lowering.name, "returned", "applied", applied, "err", fmt.Sprint(err))
		if applied {
			c.noteLowering(ctx, loweringTableAssign, lowering.name)
			return location, true, err
		}
	}
	return program.VarLocation{}, false, nil
}

// tryShortVarDeclLowerings runs shortVarDeclLowerings in order.
//
// Takes statement (*ast.AssignStmt) which is the declaration.
//
// Returns program.VarLocation which is the declaration's result.
// Returns bool which is true when the chain handled the statement or failed.
// Returns error when the lowering that ran fails.
func (c *Compiler) tryShortVarDeclLowerings(ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
	for _, lowering := range shortVarDeclLowerings {
		location, handled, err := lowering.try(c, ctx, statement)
		if err != nil {
			return location, true, err
		}
		if handled {
			c.noteLowering(ctx, "short-var-decl", lowering.name)
			return location, true, nil
		}
	}
	return program.VarLocation{}, false, nil
}

// tryIndexLowerings runs indexLowerings in order.
//
// Takes expression (*ast.IndexExpr) which is the index expression.
//
// Returns program.VarLocation which holds the value.
// Returns bool which is true when a lowering applied.
func (c *Compiler) tryIndexLowerings(ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, bool) {
	for _, lowering := range indexLowerings {
		if location, applied := lowering.try(c, ctx, expression); applied {
			c.noteLowering(ctx, "index", lowering.name)
			return location, true
		}
	}
	return program.VarLocation{}, false
}

// trySelectorFieldLowerings runs selectorFieldLowerings in order. The caller has already
// established that the receiver is in the general bank and the leaf type is fast-path
// eligible.
//
// Takes selection (*types.Selection) which is the field selection.
// Takes receiver (program.VarLocation) which holds the struct.
// Takes resultKind (isa.RegisterKind) which is the leaf's register kind.
//
// Returns program.VarLocation which holds the field value.
// Returns bool which is true when a lowering applied.
func (c *Compiler) trySelectorFieldLowerings(ctx context.Context, selection *types.Selection, receiver program.VarLocation, resultKind isa.RegisterKind) (program.VarLocation, bool) {
	for _, lowering := range selectorFieldLowerings {
		if location, applied := lowering.try(c, ctx, selection, receiver, resultKind); applied {
			c.noteLowering(ctx, "selector-field", lowering.name)
			return location, true
		}
	}
	return program.VarLocation{}, false
}

func init() {
	assignLowerings = []assignLowering{
		{name: loweringStructIntoCollection, try: (*Compiler).tryCompileStructIntoCollection},
		{name: "star-append-byte-fast", try: (*Compiler).tryCompileStarAppendByteFast},
		{name: loweringInPlaceAppend, try: (*Compiler).tryCompileInPlaceAppend},
	}

	shortVarDeclLowerings = []shortVarDeclLowering{
		{name: "multi-value", try: func(c *Compiler, ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
			if len(statement.Lhs) < 2 || len(statement.Rhs) != 1 {
				return program.VarLocation{}, false, nil
			}
			return c.compileMultiValueShortVar(ctx, statement)
		}},
		{name: "destructured-slice-elem", try: func(c *Compiler, ctx context.Context, statement *ast.AssignStmt) (program.VarLocation, bool, error) {
			if len(statement.Lhs) != 1 || len(statement.Rhs) != 1 {
				return program.VarLocation{}, false, nil
			}
			handled, err := c.tryCompileDestructuredSliceElem(ctx, statement)
			return program.VarLocation{}, handled, err
		}},
	}

	indexLowerings = []indexLowering{
		{name: "fold-string-index", try: (*Compiler).tryFoldStringIndex},
		{name: "fused-array-field-index", try: func(c *Compiler, ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, bool) {
			return c.tryCompileFusedArrayFieldIndex(ctx, expression, false, program.VarLocation{})
		}},
		{name: "deref-slice-index", try: func(c *Compiler, ctx context.Context, expression *ast.IndexExpr) (program.VarLocation, bool) {
			return c.tryCompileDerefSliceIndex(ctx, expression, false, program.VarLocation{})
		}},
	}

	selectorFieldLowerings = []selectorFieldLowering{
		{name: "field-slice-fast-path", try: func(c *Compiler, ctx context.Context, selection *types.Selection,
			receiver program.VarLocation, _ isa.RegisterKind) (program.VarLocation, bool) {
			return c.tryEmitSelectorFieldSliceFastPath(ctx, selection, receiver)
		}},
		{name: "field-scalar-fast-path", try: func(c *Compiler, ctx context.Context, selection *types.Selection,
			receiver program.VarLocation, resultKind isa.RegisterKind) (program.VarLocation, bool) {
			if !isaselect.StructFieldFastPathKindEnabled(resultKind) {
				return program.VarLocation{}, false
			}
			return c.tryEmitSelectorFieldFastPath(ctx, selection, receiver, resultKind)
		}},
	}
}
