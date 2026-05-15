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

package escape

import (
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// LocalAllocationSites carries the Emit-time program counters of a function's
// heap-promoted locals: the isa.OpAllocIndirect word that creates each local's cell and,
// for a local initialised with make(), the isa.OpExt word of that make whose placement
// flag decides where the slice backing lives.
type LocalAllocationSites struct {
	// IndirectCellPCs maps a heap-promoted local name to the PC of its cell allocation.
	IndirectCellPCs map[string]int

	// MakeSliceExtensionPCs maps a heap-promoted local name to the PC of the extension word
	// of the make() that initialised it, when it was initialised that way.
	MakeSliceExtensionPCs map[string]int
}

// escapeAnalysisVerdict captures a single local's escape status from the per-function AST
// walk. CompiledFunction stores only the per-PC outcome for runtime consumption.
type escapeAnalysisVerdict struct {
	// reasonCode names the first rule that classified the local as escaping. Unset when
	// escapes is false.
	reasonCode string

	// escapes is true when any use of the local violates a conservative escape rule. The
	// default is true, and only an explicit proof of safety clears it.
	escapes bool
}

// escapeWalkState carries the cumulative state of a single classifyLocalEscape AST walk.
// The visit method is the ast.Inspect callback and delegates to focused per-node helpers.
type escapeWalkState struct {
	// body is the function body being walked, consulted when a call's callee name must be
	// checked for a local that shadows the package-level function.
	body *ast.BlockStmt

	// declarations indexes the package's top-level functions by name, so an address passed
	// to one of them can be followed into the callee.
	declarations map[string]*ast.FuncDecl

	// Name is the local being classified.
	Name string

	// reasonCode names the first rule that produced an escape verdict. Empty when escapes is
	// false.
	reasonCode string

	// addressOfCount counts &name occurrences seen so far; a second occurrence triggers the
	// multiple-address-of escape rule.
	addressOfCount int

	// escapes is true once any rule has fired; further visits short-circuit.
	escapes bool
}

// visit dispatches on the node kind, delegating each case to a focused helper.
//
// Takes node (ast.Node) which is the AST node visited by the walk.
//
// Returns false once an escape verdict is reached to short-circuit the walk, true to
// continue.
func (state *escapeWalkState) visit(node ast.Node) bool {
	if state.escapes {
		return false
	}
	switch typed := node.(type) {
	case *ast.CallExpr:
		return state.visitCallExpr(typed)
	case *ast.UnaryExpr:
		return state.visitUnaryExpr(typed)
	case *ast.ReturnStmt:
		return state.visitReturnStmt(typed)
	case *ast.FuncLit:
		return state.visitFunctionLit(typed)
	}
	return true
}

// visitCallExpr walks a call expression, allowing a confined address-of argument through
// when the callee keeps the pointer inside its frame.
//
// Takes call (*ast.CallExpr) which is the call being visited.
//
// Returns false, because the operands are walked here rather than by the caller.
func (state *escapeWalkState) visitCallExpr(call *ast.CallExpr) bool {
	ast.Inspect(call.Fun, state.visit)
	for index, argument := range call.Args {
		if state.escapes {
			return false
		}
		if state.isConfinedAddressArgument(call, index, argument) {
			state.addressOfCount++
			if state.addressOfCount > 1 {
				state.escapes = true
				state.reasonCode = "multiple-address-of"
				return false
			}
			continue
		}
		ast.Inspect(argument, state.visit)
	}
	return false
}

// isConfinedAddressArgument reports whether argument is `&name` and the callee confines
// it.
//
// Takes call (*ast.CallExpr) which is the call.
// Takes index (int) which is the argument's position.
// Takes argument (ast.Expr) which is the argument expression.
//
// Returns true when the argument is a confined address of the local.
func (state *escapeWalkState) isConfinedAddressArgument(call *ast.CallExpr, index int, argument ast.Expr) bool {
	unary, ok := argument.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return false
	}
	ident, isIdent := unary.X.(*ast.Ident)
	if !isIdent || ident.Name != state.Name {
		return false
	}
	return addressArgumentConfined(call, index, state.body, state.declarations)
}

// visitUnaryExpr applies the address-of rules.
//
// &name appearing more than once, or in any non-immediate-deref context, escapes.
//
// Takes expression (*ast.UnaryExpr) which is the unary expression under inspection.
//
// Returns false once an escape verdict is reached, true to continue the walk.
func (state *escapeWalkState) visitUnaryExpr(expression *ast.UnaryExpr) bool {
	if expression.Op != token.AND {
		return true
	}
	rootIdent := rootIdentForMutation(expression.X)
	if rootIdent == nil || rootIdent.Name != state.Name {
		return true
	}
	state.addressOfCount++
	if state.addressOfCount > 1 {
		state.escapes = true
		state.reasonCode = "multiple-address-of"
		return false
	}
	state.escapes = true
	state.reasonCode = "address-of-non-deref"
	return false
}

// visitReturnStmt flags the local as escaping when it appears in any result expression of
// a return statement.
//
// Takes statement (*ast.ReturnStmt) which is the return statement under inspection.
//
// Returns false once an escape verdict is reached, true to continue the walk.
func (state *escapeWalkState) visitReturnStmt(statement *ast.ReturnStmt) bool {
	for _, result := range statement.Results {
		if isCopyingStringConversion(result, state.Name) {
			continue
		}
		if usesIdent(result, state.Name) {
			state.escapes = true
			state.reasonCode = "returned"
			return false
		}
	}
	return true
}

// visitFunctionLit flags the local as escaping when any closure literal references it,
// since closure literals can outlive the parent frame.
//
// Takes literal (*ast.FuncLit) which is the function literal under inspection.
//
// Returns false once an escape verdict is reached, true to continue the walk.
func (state *escapeWalkState) visitFunctionLit(literal *ast.FuncLit) bool {
	if usesIdent(literal.Body, state.Name) {
		state.escapes = true
		state.reasonCode = "closure-capture"
		return false
	}
	return true
}

// ClassifyLocalEscapes populates compiledFunction.ArenaSafeAllocPCs with the PCs of
// isa.OpAllocIndirect sites whose target local is statically proven not to escape
// compiledFunction's frame.
//
// Operates on compiledFunction's source AST plus the Compiler's per-name Emit-PC map
// (recorded by promoteToIndirect at Emit time). heapPromotedNames names the candidate
// locals.
//
// A local whose cell stays in the frame and whose slice value is used only in ways that
// keep its backing store private (sliceBackingEscapes()) also has the heap-placement flag
// cleared on the make() that initialised it, so the backing is carved from the arena like
// any other frame-local slice.
//
// Takes compiledFunction (*CompiledFunction) which receives the arenaSafeAllocPCs update;
// compiledFunction.body must be finalised so PCs are stable.
// Takes body (*ast.BlockStmt) which is the function body to scan.
// Takes sites (LocalAllocationSites) which maps each heap-promoted local name to the PCs
// of its cell allocation and, when present, its initialising make().
// Takes heapPromotedNames (map[string]bool) which lists candidate locals from the
// heap-promotion pre-pass.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's top-level
// functions so an address passed to one can be followed into the callee.
//
// Returns error when the context is cancelled mid-scan.
func ClassifyLocalEscapes(
	ctx context.Context,
	compiledFunction *program.CompiledFunction,
	body *ast.BlockStmt,
	sites LocalAllocationSites,
	heapPromotedNames map[string]bool,
	declarations map[string]*ast.FuncDecl,
) error {
	if compiledFunction == nil || body == nil || len(sites.IndirectCellPCs) == 0 || len(heapPromotedNames) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("classifyLocalEscapes cancelled: %w", err)
	}
	for name, pc := range sites.IndirectCellPCs {
		if !heapPromotedNames[name] {
			continue
		}
		verdict := classifyLocalEscape(name, body, declarations)
		if verdict.escapes {
			continue
		}
		if compiledFunction.ArenaSafeAllocPCs == nil {
			compiledFunction.ArenaSafeAllocPCs = make(map[int]bool)
		}
		compiledFunction.ArenaSafeAllocPCs[pc] = true
		if makePC, ok := sites.MakeSliceExtensionPCs[name]; ok && !sliceBackingEscapes(name, body, declarations) {
			clearMakeSliceHeapFlag(compiledFunction, makePC)
		}
	}
	return nil
}

// clearMakeSliceHeapFlag clears isa.MakeSliceExtHeapFlag on the extension word at pc,
// leaving the word alone when it is not an extension word.
//
// Takes compiledFunction (*CompiledFunction) whose body is rewritten.
// Takes pc (int) which is the extension word's program counter.
func clearMakeSliceHeapFlag(compiledFunction *program.CompiledFunction, pc int) {
	if pc < 0 || pc >= len(compiledFunction.Body) || compiledFunction.Body[pc].Op != isa.OpExt {
		return
	}
	compiledFunction.Body[pc].C &^= isa.MakeSliceExtHeapFlag
}

// classifyLocalEscape returns the escape verdict for a single named local by walking the
// function body's AST.
//
// Takes name (string) which is the local being classified.
// Takes body (*ast.BlockStmt) which is the function body AST.
// Takes declarations (map[string]*ast.FuncDecl) which indexes the package's functions.
//
// Returns the escape verdict for name in body.
func classifyLocalEscape(name string, body *ast.BlockStmt, declarations map[string]*ast.FuncDecl) escapeAnalysisVerdict {
	if nameDeclaredMoreThanOnce(body, name) {
		return escapeAnalysisVerdict{escapes: true, reasonCode: "shadowed-name"}
	}
	state := escapeWalkState{body: body, declarations: declarations, Name: name, reasonCode: "", addressOfCount: 0, escapes: false}
	ast.Inspect(body, state.visit)
	if state.escapes {
		return escapeAnalysisVerdict{escapes: true, reasonCode: state.reasonCode}
	}
	return escapeAnalysisVerdict{escapes: false, reasonCode: ""}
}

// nameDeclaredMoreThanOnce reports whether the identifier name is introduced by more than
// one declaration site within body, which shadows it beyond what the bare-name escape
// walk can resolve.
//
// Takes body (*ast.BlockStmt) which is the function body AST.
// Takes name (string) which is the local name being checked.
//
// Returns true when at least two declaration sites introduce name.
func nameDeclaredMoreThanOnce(body *ast.BlockStmt, name string) bool {
	count := 0
	tally := func(ident *ast.Ident) bool {
		if ident == nil || ident.Name != name {
			return false
		}
		count++
		return count >= 2
	}
	stop := false
	ast.Inspect(body, func(node ast.Node) bool {
		if stop {
			return false
		}
		if countDeclarationSite(node, tally) {
			stop = true
			return false
		}
		return true
	})
	return count >= 2
}

// countDeclarationSite feeds every declaring identifier of node to tally, returning true
// as soon as tally reports the saturation threshold was reached.
//
// Takes node (ast.Node) which is the AST node under inspection.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier and returns
// true once the count threshold is met.
//
// Returns true when tally signalled the threshold; false otherwise.
func countDeclarationSite(node ast.Node, tally func(*ast.Ident) bool) bool {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		return countDefineAssignDeclarations(typed, tally)
	case *ast.ValueSpec:
		return slices.ContainsFunc(typed.Names, tally)
	case *ast.RangeStmt:
		return countRangeDeclarations(typed, tally)
	case *ast.TypeSwitchStmt:
		return countTypeSwitchDeclarations(typed, tally)
	case *ast.FuncLit:
		return countFunctionLitParamDeclarations(typed, tally)
	}
	return false
}

// countDefineAssignDeclarations feeds every left-hand-side identifier of a short variable
// declaration (`:=`) to tally. Plain assignments declare nothing and are skipped.
//
// Takes assign (*ast.AssignStmt) which is the candidate assignment.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier.
//
// Returns true when tally signalled the saturation threshold.
func countDefineAssignDeclarations(assign *ast.AssignStmt, tally func(*ast.Ident) bool) bool {
	if assign.Tok != token.DEFINE {
		return false
	}
	return countIdentExprs(assign.Lhs, tally)
}

// countRangeDeclarations feeds the key and value identifiers of a range statement to
// tally when the statement declares them with `:=`.
//
// Takes statement (*ast.RangeStmt) which is the range statement.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier.
//
// Returns true when tally signalled the saturation threshold.
func countRangeDeclarations(statement *ast.RangeStmt, tally func(*ast.Ident) bool) bool {
	if statement.Tok != token.DEFINE {
		return false
	}
	return countIdentExprs([]ast.Expr{statement.Key, statement.Value}, tally)
}

// countTypeSwitchDeclarations feeds the bound identifier of a type switch guard (`switch
// v := x.(type)`) to tally.
//
// Takes statement (*ast.TypeSwitchStmt) which is the type switch.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier.
//
// Returns true when tally signalled the saturation threshold.
func countTypeSwitchDeclarations(statement *ast.TypeSwitchStmt, tally func(*ast.Ident) bool) bool {
	assign, ok := statement.Assign.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE {
		return false
	}
	return countIdentExprs(assign.Lhs, tally)
}

// countIdentExprs feeds every expression in exprs that is a plain identifier to tally,
// ignoring non-identifier expressions.
//
// Takes exprs ([]ast.Expr) which is the expression list to scan.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier.
//
// Returns true when tally signalled the saturation threshold.
func countIdentExprs(exprs []ast.Expr, tally func(*ast.Ident) bool) bool {
	for _, expr := range exprs {
		if ident, ok := expr.(*ast.Ident); ok && tally(ident) {
			return true
		}
	}
	return false
}

// countFunctionLitParamDeclarations feeds every parameter and result name of a function
// literal to tally.
//
// A closure parameter or named result sharing the promoted local's name shadows it within
// the literal's body, which the bare-name escape walk would otherwise misattribute.
//
// Takes literal (*ast.FuncLit) which is the function literal.
// Takes tally (func(*ast.Ident) bool) which records a declaring identifier.
//
// Returns true when tally signalled the saturation threshold.
func countFunctionLitParamDeclarations(literal *ast.FuncLit, tally func(*ast.Ident) bool) bool {
	if literal.Type == nil {
		return false
	}
	for _, fieldList := range []*ast.FieldList{literal.Type.Params, literal.Type.Results} {
		if fieldList == nil {
			continue
		}
		for _, field := range fieldList.List {
			if slices.ContainsFunc(field.Names, tally) {
				return true
			}
		}
	}
	return false
}

// usesIdent reports whether node's subtree contains any identifier reference to name.
// Bare identifiers, selector receivers, index targets, and call function expressions all
// count.
//
// Takes node (ast.Node) which is the AST subtree to scan.
// Takes name (string) which is the identifier to look for.
//
// Returns true when name appears at least once in node.
func usesIdent(node ast.Node, name string) bool {
	if node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}
