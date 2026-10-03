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
	"go/types"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// compileLocalInitialiser compiles the initial value of a local variable.
//
// When name is heap-promoted, captured by a closure or stored beyond its register (see
// escape.CollectFlowingSliceNames) and value is make([]T, ...) or a slice literal, the
// slice backing is placed on the Go heap. A closure cell or container that receives an
// arena-backed slice materialises its own copy, which splits it off from the variable's
// backing, whereas Go shares one backing between every alias.
//
// Takes name (string) which is the local receiving the value.
// Takes value (ast.Expr) which is the right-hand side expression.
//
// Returns the location holding the value and any compilation error.
func (c *Compiler) compileLocalInitialiser(ctx context.Context, name string, value ast.Expr) (program.VarLocation, error) {
	heapPromoted := c.localIsHeapPromoted(name)
	if !heapPromoted && !c.closureCapturedNames[name] && !c.flowingSliceNames[name] {
		return c.compileExpression(ctx, value)
	}
	if c.isSliceLiteral(value) {
		return c.compileHeapPlacedSliceLiteral(ctx, value)
	}
	call, ok := c.makeSliceCall(value)
	if !ok {
		return c.compileExpression(ctx, value)
	}
	location, err := c.compileBuiltinMakeWithPlacement(ctx, call, true)
	if err != nil || !heapPromoted {
		return location, err
	}

	extensionPC := program.CurrentPC(c.Function) - 1
	if extensionPC >= 1 && c.Function.Body[extensionPC-1].Op == isa.OpMakeSlice && c.Function.Body[extensionPC].Op == isa.OpExt {
		if c.escapeMakeSitePCs == nil {
			c.escapeMakeSitePCs = make(map[string]int)
		}
		c.escapeMakeSitePCs[name] = extensionPC
	}
	return location, nil
}

// compileHeapPlacedSliceLiteral compiles a slice literal whose backing must live on the
// Go heap, setting isa.MakeSliceExtHeapFlag on the allocation the literal fills.
//
// Takes literal (ast.Expr) which is the slice literal.
//
// Returns the location holding the slice and any compilation error.
func (c *Compiler) compileHeapPlacedSliceLiteral(ctx context.Context, literal ast.Expr) (program.VarLocation, error) {
	start := program.CurrentPC(c.Function)
	location, err := c.compileExpression(ctx, literal)
	if err != nil || location.Kind != isa.RegisterGeneral {
		return location, err
	}
	body := c.Function.Body
	for pc := start; pc+1 < len(body); pc++ {
		if body[pc].Op == isa.OpMakeSlice && body[pc].A == location.Register && body[pc+1].Op == isa.OpExt {
			body[pc+1].C |= isa.MakeSliceExtHeapFlag
			break
		}
	}
	return location, nil
}

// compileHeapPlacedSliceAllocation compiles a slice literal or make([]T, ...) with its
// backing on the Go heap.
//
// Takes expression (ast.Expr) which is the allocation.
//
// Returns the location holding the slice, false when expression is neither form, and any
// compilation error.
func (c *Compiler) compileHeapPlacedSliceAllocation(ctx context.Context, expression ast.Expr) (program.VarLocation, bool, error) {
	if c.isSliceLiteral(expression) {
		location, err := c.compileHeapPlacedSliceLiteral(ctx, expression)
		return location, true, err
	}
	if call, ok := c.makeSliceCall(expression); ok {
		location, err := c.compileBuiltinMakeWithPlacement(ctx, call, true)
		return location, true, err
	}
	return program.VarLocation{}, false, nil
}

// isSliceLiteral reports whether expression is a composite literal of slice type.
//
// Takes expression (ast.Expr) which is the candidate right-hand side.
//
// Returns true when expression is a []T{...} literal.
func (c *Compiler) isSliceLiteral(expression ast.Expr) bool {
	literal, ok := ast.Unparen(expression).(*ast.CompositeLit)
	if !ok || c.Info == nil {
		return false
	}
	tv, ok := c.Info.Types[literal]
	if !ok || tv.Type == nil {
		return false
	}
	_, isSlice := tv.Type.Underlying().(*types.Slice)
	return isSlice
}

// localIsHeapPromoted reports whether the general-bank local name either is flagged for
// heap promotion at its declaration or already lives behind an indirect cell.
//
// Takes name (string) which is the local to classify.
//
// Returns true when stores to name go through a heap cell.
func (c *Compiler) localIsHeapPromoted(name string) bool {
	if c.heapPromotedNames[name] {
		return true
	}
	location, found := c.Scopes.LookupVar(name)
	return found && location.IsIndirect && location.OriginalKind == isa.RegisterGeneral
}

// makeSliceCall reports whether expression is a call of the builtin make producing a
// slice.
//
// Takes expression (ast.Expr) which is the candidate right-hand side.
//
// Returns the call and true when it is make([]T, ...); nil and false otherwise.
func (c *Compiler) makeSliceCall(expression ast.Expr) (*ast.CallExpr, bool) {
	call, ok := ast.Unparen(expression).(*ast.CallExpr)
	if !ok || c.Info == nil {
		return nil, false
	}
	identifier, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || identifier.Name != "make" {
		return nil, false
	}
	if _, builtin := c.Info.Uses[identifier].(*types.Builtin); !builtin {
		return nil, false
	}
	tv, ok := c.Info.Types[call]
	if !ok || tv.Type == nil {
		return nil, false
	}
	if _, isSlice := tv.Type.Underlying().(*types.Slice); !isSlice {
		return nil, false
	}
	return call, true
}

// tryHeapPromoteCapturedLocal heap-promotes a captured local when flagged in
// c.heapPromotedNames.
//
// Looks up the name's static type from go/types and emits isa.OpAllocIndirect via
// promoteToIndirect. No-ops when the name is not flagged, when the variable is already
// indirect, or when the static type is unknown.
//
// Called from each declareVar site that introduces a function-scope local: parameters,
// named results, var-spec declarations, and short variable declarations.
//
// Takes name (string) which is the freshly declared variable name.
// Takes ident (*ast.Ident) which is the AST identifier whose static type drives the heap
// cell's element type.
func (c *Compiler) tryHeapPromoteCapturedLocal(ctx context.Context, name string, ident *ast.Ident) {
	if c.heapPromotedNames == nil || !c.heapPromotedNames[name] {
		return
	}
	if ident == nil {
		return
	}
	typeObject := c.Info.Defs[ident]
	if typeObject == nil {
		typeObject = c.Info.Uses[ident]
	}
	if typeObject == nil {
		return
	}
	reflectType := c.TypeToReflect(ctx, typeObject.Type())
	if reflectType == nil {
		return
	}
	promoted, ok := c.promoteToIndirect(ctx, name, reflectType)
	if !ok {
		return
	}
	c.refreshNamedResultLocation(name, promoted)
}

// refreshNamedResultLocation rewrites the function's named-result location entry for name
// when the variable was just promoted, so the runtime sync paths (syncNamedResults,
// handleReturn) recover the up-to-date value via emitIndirectRead semantics rather than
// reading the typed-bank slot held before the promotion.
//
// Takes name (string) which is the named-result identifier.
// Takes promoted (VarLocation) which is the post-promotion location the Compiler scope
// has adopted.
func (c *Compiler) refreshNamedResultLocation(name string, promoted program.VarLocation) {
	for index, namedName := range c.Function.NamedResultNames {
		if namedName != name {
			continue
		}
		c.Function.NamedResultLocations[index] = promoted
		return
	}
}

// classifyTypedSliceLocals classifies body's typed-slice locals and records which of them
// must be allocated on the Go heap, populating typedSliceLocals and heapTypedSliceLocals
// together so the two views never drift apart.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
func (c *Compiler) classifyTypedSliceLocals(body *ast.BlockStmt) {
	c.typedSliceLocals = escape.ClassifyTypedSliceLocals(c.EscapeContext(), body)
	c.heapTypedSliceLocals = escape.ClassifyHeapTypedSliceLocals(body, c.typedSliceLocals)
	for name := range c.typedSliceLocals {
		if c.flowingSliceNames[name] {
			if c.heapTypedSliceLocals == nil {
				c.heapTypedSliceLocals = make(map[string]bool)
			}
			c.heapTypedSliceLocals[name] = true
		}
	}
}

// lookupSliceFlow resolves what the callees a call may reach do with its operands,
// recomputing the program's slice flows when more functions have been declared since they
// were last computed. It is the compiler's escape.CallLookup.
//
// Takes call (*ast.CallExpr) which is the call.
//
// Returns escape.CallFlow which is the zero value when no callee stores anything.
func (c *Compiler) lookupSliceFlow(call *ast.CallExpr) escape.CallFlow {
	if c.programContext == nil || c.Info == nil {
		return escape.CallFlow{Arguments: nil, Receiver: false, PacksVariadic: false}
	}
	if c.sliceFlows == nil || c.sliceFlowsDeclared != len(c.sliceFlowDeclarations) {
		c.sliceFlows = escape.ComputeSliceFlows(c.Info, c.sliceFlowDeclarations)
		c.sliceFlowsDeclared = len(c.sliceFlowDeclarations)
	}
	return c.sliceFlows.Lookup(call)
}
