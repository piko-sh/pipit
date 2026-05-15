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
	"cmp"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"math"
	"reflect"
	"slices"

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// destructuredFieldPlan describes one consumed scalar field of a destructured
// slice-element local.
type destructuredFieldPlan struct {
	// fieldIndex is the field's positional index within the element struct.
	fieldIndex int

	// kind is the destination register bank.
	kind isa.RegisterKind
}

// destructuredElem carries the resolved sides of a `local := slice[i]` (or `local :=
// recv.field[i]`) destructuring through the Emit helpers. The compilation context is
// threaded separately as the leading argument and never stored here.
type destructuredElem struct {
	// localObject keys the field-to-register binding map for the destructured local.
	localObject types.Object

	// indexExpr is the `slice[i]` (or `recv.field[i]`) right-hand side.
	indexExpr *ast.IndexExpr

	// elementType is the element struct type whose fields are loaded.
	elementType reflect.Type

	// consumedFields lists the fields to load, keyed by field name.
	consumedFields map[string]destructuredFieldPlan

	// fieldSelector is set only for the via-receiver strategy, where the indexed collection
	// is a slice-typed struct field read in place through the receiver.
	fieldSelector *ast.SelectorExpr

	// fieldSelection is the go/types selection for the via-receiver strategy, paired with
	// fieldSelector.
	fieldSelection *types.Selection

	// orderedNames lists the consumed field names in deterministic field-offset order,
	// populated by emitDestructuredSliceElem before dispatch. Kept last so its slice
	// header's length and capacity words trail the pointerful fields.
	orderedNames []string
}

// tryCompileDestructuredSliceElem compiles a struct-element slice index as per-field
// fused scalar loads when every use of the local is a read of a full-width scalar field.
// The scalars snapshot at the declaration site, preserving Go's copy semantics.
//
// Takes statement (*ast.AssignStmt) which is a single-Lhs, single-Rhs := declaration.
//
// Returns whether the declaration was fully compiled here, and any compilation error.
func (c *Compiler) tryCompileDestructuredSliceElem(ctx context.Context, statement *ast.AssignStmt) (bool, error) {
	localObject, ok := c.destructurableSliceElemLocal(statement)
	if !ok {
		return false, nil
	}
	indexExpr, ok := ast.Unparen(statement.Rhs[0]).(*ast.IndexExpr)
	if !ok {
		return false, nil
	}
	elementType, ok := c.resolveDestructuredElementType(ctx, indexExpr)
	if !ok {
		return false, nil
	}

	consumedFields, eligible := c.collectDestructurableUses(localObject, elementType)
	if !eligible || len(consumedFields) == 0 {
		return false, nil
	}

	return c.emitDestructuredSliceElem(ctx, destructuredElem{localObject: localObject,
		indexExpr:      indexExpr,
		elementType:    elementType,
		consumedFields: consumedFields, fieldSelector: nil, fieldSelection: nil, orderedNames: nil})
}

// destructurableSliceElemLocal validates the left-hand side of a `local := slice[i]`
// declaration and resolves the local's object. The local must be a named (non-blank)
// identifier defined here, and must not be written through, captured by a closure, or
// heap-promoted - any of which would break the fused per-field snapshot.
//
// Takes statement (*ast.AssignStmt) which is the single-Lhs, single-Rhs := declaration.
//
// Returns the local's object and whether the left-hand side is eligible.
func (c *Compiler) destructurableSliceElemLocal(statement *ast.AssignStmt) (types.Object, bool) {
	localIdent, ok := statement.Lhs[0].(*ast.Ident)
	if !ok || localIdent.Name == "_" || c.currentBody == nil || c.Info == nil {
		return nil, false
	}
	localObject := c.Info.Defs[localIdent]
	if localObject == nil {
		return nil, false
	}
	if c.writtenLocalNames[localIdent.Name] || c.closureCapturedNames[localIdent.Name] || c.heapPromotedNames[localIdent.Name] {
		return nil, false
	}
	return localObject, true
}

// resolveDestructuredElementType validates the `slice[i]` right-hand side and resolves
// the reflected element struct type. The indexed collection must be a slice of structs
// indexed by an int, and the element struct must have an encodable size.
//
// Takes indexExpr (*ast.IndexExpr) which is the `slice[i]` right-hand side.
//
// Returns the element struct's reflected layout and whether it qualifies.
func (c *Compiler) resolveDestructuredElementType(ctx context.Context, indexExpr *ast.IndexExpr) (reflect.Type, bool) {
	collectionType, ok := c.Info.Types[indexExpr.X]
	if !ok || collectionType.Type == nil {
		return nil, false
	}
	sliceType, ok := types.Unalias(collectionType.Type).Underlying().(*types.Slice)
	if !ok {
		return nil, false
	}
	if _, isStructElem := sliceType.Elem().Underlying().(*types.Struct); !isStructElem {
		return nil, false
	}
	indexType, ok := c.Info.Types[indexExpr.Index]
	if !ok || indexType.Type == nil || c.kindFor(indexType.Type) != isa.RegisterInt {
		return nil, false
	}
	reflectSliceType := c.TypeToReflect(ctx, collectionType.Type)
	if reflectSliceType == nil || reflectSliceType.Kind() != reflect.Slice || reflectSliceType.Elem().Kind() != reflect.Struct {
		return nil, false
	}
	elementType := reflectSliceType.Elem()
	if elementType.Size() > math.MaxUint16 {
		return nil, false
	}
	return elementType, true
}

// collectDestructurableUses scans the current function body for uses of the local and
// reports the consumed fields.
//
// Every use must be an rvalue read of a full-width scalar field of the element struct.
// Uses inside nested function literals, whole-value uses, address-taken selects, and
// writes through the local all disqualify it.
//
// Takes localObject (types.Object) which identifies the local across the body.
// Takes elementType (reflect.Type) which is the element struct's reflected layout.
//
// Returns the consumed-field plans keyed by field name, and whether the local is eligible
// for destructuring.
func (c *Compiler) collectDestructurableUses(localObject types.Object, elementType reflect.Type) (map[string]destructuredFieldPlan, bool) {
	consumed := make(map[string]destructuredFieldPlan)
	eligible := true
	var visit func(node ast.Node, parent ast.Node, grandparent ast.Node, insideFunctionLit bool)
	visit = func(node ast.Node, parent, grandparent ast.Node, insideFunctionLit bool) {
		if node == nil || !eligible {
			return
		}
		if identifier, isIdent := node.(*ast.Ident); isIdent {
			if c.Info.Uses[identifier] != localObject {
				return
			}
			name, plan, useOK := classifyDestructurableIdentUse(identifier, parent, grandparent, insideFunctionLit, elementType)
			if !useOK {
				eligible = false
				return
			}
			consumed[name] = plan
			return
		}
		_, isFuncLit := node.(*ast.FuncLit)
		childInsideFuncLit := insideFunctionLit || isFuncLit
		for _, child := range childNodes(node) {
			visit(child, node, parent, childInsideFuncLit)
		}
	}
	visit(c.currentBody, nil, nil, false)
	return consumed, eligible
}

// emitDestructuredSliceElem emits the fused per-field loads for an eligible destructured
// local and records the bindings.
//
// Resolves the receiver-vs-slice strategy, orders the consumed fields by offset, and
// dispatches to the matching emitter.
//
// Takes elem (destructuredElem) which carries the resolved sides of the destructuring.
//
// Returns whether emission happened, and any compilation error.
func (c *Compiler) emitDestructuredSliceElem(ctx context.Context, elem destructuredElem) (bool, error) {
	elem.fieldSelector, elem.fieldSelection = c.classifyReceiverSliceField(ctx, elem.indexExpr.X)

	elem.orderedNames = make([]string, 0, len(elem.consumedFields))
	for name := range elem.consumedFields {
		elem.orderedNames = append(elem.orderedNames, name)
	}
	slices.SortFunc(elem.orderedNames, func(nameA, nameB string) int {
		return cmp.Or(cmp.Compare(
			elem.elementType.Field(elem.consumedFields[nameA].fieldIndex).Offset,
			elem.elementType.Field(elem.consumedFields[nameB].fieldIndex).Offset,
		), cmp.Compare(nameA, nameB))
	})

	if elem.fieldSelection != nil {
		return c.emitDestructuredViaReceiver(ctx, elem)
	}
	return c.emitDestructuredViaSliceRegister(ctx, elem)
}

// classifyReceiverSliceField reports whether the indexed collection is itself a
// slice-typed struct field on a general-bank receiver with a tier-0 layout, which enables
// the header-in-place isa.OpGetStructFieldSliceIndexScalar strategy.
//
// Takes collection (ast.Expr) which is the indexed expression.
//
// Returns the selector and selection when the receiver strategy applies; (nil, nil)
// otherwise.
func (c *Compiler) classifyReceiverSliceField(ctx context.Context, collection ast.Expr) (*ast.SelectorExpr, *types.Selection) {
	selector, ok := ast.Unparen(collection).(*ast.SelectorExpr)
	if !ok {
		return nil, nil
	}
	selection, ok := c.Info.Selections[selector]
	if !ok || selection.Kind() != types.FieldVal {
		return nil, nil
	}
	leafType := types.Unalias(selection.Type())
	if _, isNamed := leafType.(*types.Named); isNamed && !namedTypeHasNoMethods(leafType) {
		return nil, nil
	}
	receiverType := c.Info.Types[selector.X].Type
	if receiverType == nil || c.kindFor(receiverType) != isa.RegisterGeneral {
		return nil, nil
	}
	layoutIdx, layoutOK := c.tryResolveStructFieldLayout(ctx, selection)
	if !layoutOK || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		return nil, nil
	}
	return selector, selection
}

// emitDestructuredViaReceiver emits isa.OpGetStructFieldSliceIndexScalar loads, reading
// the slice header in place through the receiver.
//
// Takes elem (destructuredElem) which carries the resolved destructuring plan.
//
// Returns whether emission happened, and any compilation error.
func (c *Compiler) emitDestructuredViaReceiver(ctx context.Context, elem destructuredElem) (bool, error) {
	layoutIdx, layoutOK := c.tryResolveStructFieldLayout(ctx, elem.fieldSelection)
	if !layoutOK || !fieldlayout.StructFieldLayoutIndexFitsTier0(layoutIdx) {
		return false, nil
	}

	receiverLocation, err := c.compileExpression(ctx, elem.fieldSelector.X)
	if err != nil {
		return false, err
	}
	if receiverLocation.Kind != isa.RegisterGeneral {
		return false, fmt.Errorf(
			"%w: destructured slice-field receiver compiled to %s, expected general at %s",
			fault.ErrCompilation,
			receiverLocation.Kind,
			c.positionString(elem.fieldSelector.Pos()),
		)
	}
	indexLocation, err := c.compileExpression(ctx, elem.indexExpr.Index)
	if err != nil {
		return false, err
	}
	if indexLocation.Kind != isa.RegisterInt {
		return false, fmt.Errorf("%w: destructured slice-field index compiled to %s, expected int at %s", fault.ErrCompilation, indexLocation.Kind, c.positionString(elem.indexExpr.Index.Pos()))
	}

	elementSize := safeconv.MustIntToUint16(int(elem.elementType.Size()))
	bindings := make(map[string]program.VarLocation, len(elem.orderedNames))
	for _, name := range elem.orderedNames {
		plan := elem.consumedFields[name]
		dest := c.Scopes.Alloc.Alloc(plan.kind)
		subOffset := safeconv.MustIntToUint16(int(elem.elementType.Field(plan.fieldIndex).Offset))
		program.Emit(c.Function, isa.OpGetStructFieldSliceIndexScalar, dest, receiverLocation.Register, safeconv.Uint16ToUint8(layoutIdx))
		program.EmitExtension(c.Function, elementSize, indexLocation.Register)
		program.EmitExtension(c.Function, subOffset, uint8(plan.kind))
		bindings[name] = program.VarLocation{Register: dest, Kind: plan.kind}
	}
	c.recordDestructuredLocal(elem.localObject, bindings)
	return true, nil
}

// emitDestructuredViaSliceRegister compiles the slice expression to a general register
// and emits fused loads.
//
// Takes elem (destructuredElem) which carries the resolved destructuring plan.
//
// Returns whether emission happened, and any compilation error.
func (c *Compiler) emitDestructuredViaSliceRegister(ctx context.Context, elem destructuredElem) (bool, error) {
	type perField struct {
		op        isa.Opcode
		layoutIdx uint16
	}
	plans := make(map[string]perField, len(elem.orderedNames))
	for _, name := range elem.orderedNames {
		plan := elem.consumedFields[name]
		op, hasOp := isaselect.PickSliceIndexStructFieldOp(plan.kind)
		if !hasOp {
			return false, nil
		}
		layoutIdx, ok := c.registerStructFieldLayoutFromReflect(elem.elementType, []int{plan.fieldIndex})
		if !ok {
			return false, nil
		}
		plans[name] = perField{op: op, layoutIdx: layoutIdx}
	}

	sliceLocation, err := c.compileExpression(ctx, elem.indexExpr.X)
	if err != nil {
		return false, err
	}
	c.boxToGeneral(ctx, &sliceLocation)
	indexLocation, err := c.compileExpression(ctx, elem.indexExpr.Index)
	if err != nil {
		return false, err
	}
	if indexLocation.Kind != isa.RegisterInt {
		return false, fmt.Errorf("%w: destructured slice index compiled to %s, expected int at %s", fault.ErrCompilation, indexLocation.Kind, c.positionString(elem.indexExpr.Index.Pos()))
	}

	bindings := make(map[string]program.VarLocation, len(elem.orderedNames))
	for _, name := range elem.orderedNames {
		plan := elem.consumedFields[name]
		dest := c.Scopes.Alloc.Alloc(plan.kind)
		program.Emit(c.Function, plans[name].op, dest, sliceLocation.Register, indexLocation.Register)
		c.emitStructFieldLayoutExtension(plans[name].layoutIdx)
		bindings[name] = program.VarLocation{Register: dest, Kind: plan.kind}
	}
	c.recordDestructuredLocal(elem.localObject, bindings)
	return true, nil
}

// recordDestructuredLocal stores the per-field register bindings for a destructured
// local.
//
// Takes localObject (types.Object) which identifies the local.
// Takes bindings (map[string]program.VarLocation) which maps field names to their
// register locations.
func (c *Compiler) recordDestructuredLocal(localObject types.Object, bindings map[string]program.VarLocation) {
	if c.destructuredLocals == nil {
		c.destructuredLocals = make(map[types.Object]map[string]program.VarLocation)
	}
	c.destructuredLocals[localObject] = bindings
}

// lookupDestructuredSelector resolves a selector against the destructured-local bindings.
//
// Takes expression (*ast.SelectorExpr) which is the selector to look up.
//
// Returns the bound location, true if matched, or an error when the consumed set lacks
// the field (an analysis bug).
func (c *Compiler) lookupDestructuredSelector(expression *ast.SelectorExpr) (program.VarLocation, bool, error) {
	if c.destructuredLocals == nil {
		return program.VarLocation{}, false, nil
	}
	identifier, ok := expression.X.(*ast.Ident)
	if !ok {
		return program.VarLocation{}, false, nil
	}
	localObject := c.Info.Uses[identifier]
	if localObject == nil {
		return program.VarLocation{}, false, nil
	}
	bindings, ok := c.destructuredLocals[localObject]
	if !ok {
		return program.VarLocation{}, false, nil
	}
	location, ok := bindings[expression.Sel.Name]
	if !ok {
		return program.VarLocation{}, false, fmt.Errorf(
			"%w: destructured local %s has no binding for field %s at %s",
			fault.ErrCompilation,
			identifier.Name,
			expression.Sel.Name,
			c.positionString(expression.Pos()),
		)
	}
	return location, true, nil
}

// classifyDestructurableIdentUse validates a single use of the destructured local and,
// when the use is an rvalue read of a full-width scalar field, reports the consumed field
// name and its plan. Uses inside nested function literals, whole-value uses, non-rvalue
// selects, and selects of unsupported fields are all rejected.
//
// Takes identifier (*ast.Ident) which is the use of the local being classified.
// Takes parent (ast.Node) which is the identifier's enclosing node.
// Takes grandparent (ast.Node) which is the parent's enclosing node.
// Takes insideFunctionLit (bool) which reports whether the use sits inside a function
// literal.
// Takes elementType (reflect.Type) which is the element struct's reflected layout.
//
// Returns the consumed field name, its plan, and whether the use is eligible.
func classifyDestructurableIdentUse(identifier *ast.Ident, parent, grandparent ast.Node, insideFunctionLit bool, elementType reflect.Type) (string, destructuredFieldPlan, bool) {
	if insideFunctionLit {
		return "", destructuredFieldPlan{}, false
	}
	selector, parentIsSelector := parent.(*ast.SelectorExpr)
	if !parentIsSelector || selector.X != identifier {
		return "", destructuredFieldPlan{}, false
	}
	if !selectorIsRValue(selector, grandparent) {
		return "", destructuredFieldPlan{}, false
	}
	plan, planOK := destructuredFieldPlanFor(elementType, selector.Sel.Name)
	if !planOK {
		return "", destructuredFieldPlan{}, false
	}
	return selector.Sel.Name, plan, true
}

// childNodes returns the direct AST children of a node, in source order.
//
// Takes node (ast.Node) which is the parent node.
//
// Returns the children collected via a one-level ast.Inspect walk.
func childNodes(node ast.Node) []ast.Node {
	var children []ast.Node
	first := true
	ast.Inspect(node, func(n ast.Node) bool {
		if first {
			first = false
			return true
		}
		if n != nil {
			children = append(children, n)
		}
		return false
	})
	return children
}

// selectorIsRValue reports whether a field selector appears in a value (read) context.
//
// Takes selector (*ast.SelectorExpr) which is the field select on the local.
// Takes parent (ast.Node) which is the selector's enclosing node.
//
// Returns false for assignment targets, address-of operands, inc/dec targets, and
// range-clause targets.
func selectorIsRValue(selector *ast.SelectorExpr, parent ast.Node) bool {
	switch p := parent.(type) {
	case *ast.AssignStmt:
		return !slices.Contains(p.Lhs, ast.Expr(selector))
	case *ast.UnaryExpr:
		return p.Op != token.AND
	case *ast.IncDecStmt:
		return p.X != ast.Expr(selector)
	case *ast.RangeStmt:
		return p.Key != ast.Expr(selector) && p.Value != ast.Expr(selector)
	default:
		return true
	}
}

// destructuredFieldPlanFor resolves a consumed field name against the element struct and
// reports its plan when the field is a full-width scalar (int, int64, uint, uint64,
// float64, or bool) at an encodable offset.
//
// Takes elementType (reflect.Type) which is the element struct type.
// Takes fieldName (string) which is the selected field.
//
// Returns the plan and whether the field qualifies.
func destructuredFieldPlanFor(elementType reflect.Type, fieldName string) (destructuredFieldPlan, bool) {
	field, ok := elementType.FieldByName(fieldName)
	if !ok || len(field.Index) != 1 || field.Offset > math.MaxUint16 {
		return destructuredFieldPlan{}, false
	}
	var kind isa.RegisterKind
	switch field.Type.Kind() {
	case reflect.Int, reflect.Int64:
		kind = isa.RegisterInt
	case reflect.Uint, reflect.Uint64:
		kind = isa.RegisterUint
	case reflect.Float64:
		kind = isa.RegisterFloat
	case reflect.Bool:
		kind = isa.RegisterBool
	default:
		return destructuredFieldPlan{}, false
	}
	return destructuredFieldPlan{fieldIndex: field.Index[0], kind: kind}, true
}
