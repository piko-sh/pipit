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
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/typemap"

	"pipit.sh/pipit/internal/isa"
)

// maxClosureNestingDepth caps recursion into nested closure literals to prevent stack
// exhaustion on pathologically deep input.
const maxClosureNestingDepth = 256

// scopeStartRecorder accumulates local scope starts for litLocalScopeStarts.
type scopeStartRecorder struct {
	// starts holds the earliest scope-start position per name.
	starts map[string]token.Pos

	// defining records every identifier that introduces a new local declaration.
	defining map[*ast.Ident]bool
}

// note records one defining identifier and the position its scope starts at, keeping the
// earliest start for a name declared more than once.
//
// Takes id (*ast.Ident) which is the defining identifier; nil and blank are ignored.
// Takes start (token.Pos) which is where the local comes into scope.
func (r *scopeStartRecorder) note(id *ast.Ident, start token.Pos) {
	if id == nil || id.Name == "_" {
		return
	}
	r.defining[id] = true
	if existing, ok := r.starts[id.Name]; !ok || start < existing {
		r.starts[id.Name] = start
	}
}

// recordAssign records the locals a short variable declaration introduces.
//
// Takes s (*ast.AssignStmt) which is the statement.
func (r *scopeStartRecorder) recordAssign(s *ast.AssignStmt) {
	if s.Tok != token.DEFINE {
		return
	}
	for _, lhs := range s.Lhs {
		if id, ok := lhs.(*ast.Ident); ok {
			r.note(id, s.End())
		}
	}
}

// recordValueSpec records the locals a var declaration introduces.
//
// Takes s (*ast.ValueSpec) which is the spec.
func (r *scopeStartRecorder) recordValueSpec(s *ast.ValueSpec) {
	for _, name := range s.Names {
		r.note(name, s.End())
	}
}

// recordRange records the locals a range clause with := introduces; they come into scope
// after the range expression.
//
// Takes s (*ast.RangeStmt) which is the loop.
func (r *scopeStartRecorder) recordRange(s *ast.RangeStmt) {
	if s.Tok != token.DEFINE {
		return
	}
	if id, ok := s.Key.(*ast.Ident); ok {
		r.note(id, s.X.End())
	}
	if id, ok := s.Value.(*ast.Ident); ok {
		r.note(id, s.X.End())
	}
}

// CollectClosureCapturedNamesFiltered returns names that warrant heap promotion so each
// declareVar site can follow with isa.OpAllocIndirect and the closure cell carries a
// stable pointer. Typed-bank captures are excluded because they sync via
// isa.SubOpWriteSharedCell.
//
// Takes typeContext (*Compiler) which provides go/types information for filtering
// candidates by static kind.
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the set of names to heap-promote, or nil when body contains no closures.
func CollectClosureCapturedNamesFiltered(typeContext *Context, body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return nil
	}
	mutated := make(map[string]bool)
	reassigned := make(map[string]bool)
	freeVars := make(map[string]bool)
	hasClosure := false
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		hasClosure = true
		collectMutatedCapturesForLit(lit, mutated)
		collectReassignedCapturesForLit(lit, reassigned)
		CollectFreeVarsForLit(lit, freeVars)
		return false
	})
	if !hasClosure {
		return nil
	}
	captured := make(map[string]bool, len(mutated)+len(freeVars))
	for name := range mutated {
		if !reassigned[name] && isPrimitiveSliceCapturedName(typeContext, body, name) {
			continue
		}
		captured[name] = true
	}
	addByReferenceCaptures(typeContext, body, freeVars, captured)
	return captured
}

// CollectClosureCapturedNamesAll returns every free variable captured by closures inside
// body, without the struct/array filter. Used as the gate for isa.SubOpResetSharedCell
// emission, because even scalar captures need the shared-cell map cleared per iteration
// so handleMakeClosure produces a fresh snapshot.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the set of captured names; returns nil when body contains no closures.
func CollectClosureCapturedNamesAll(body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return nil
	}
	mutated := make(map[string]bool)
	freeVars := make(map[string]bool)
	hasClosure := false
	ast.Inspect(body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		hasClosure = true
		collectMutatedCapturesForLit(lit, mutated)
		CollectFreeVarsForLit(lit, freeVars)
		return false
	})
	if !hasClosure {
		return nil
	}
	captured := make(map[string]bool, len(mutated)+len(freeVars))
	for name := range mutated {
		captured[name] = true
	}
	for name := range freeVars {
		captured[name] = true
	}
	return captured
}

// CollectWrittenLocalNames returns local names written after their declaration, so
// snapshot emission can skip the byte-slab snapshot for read-only struct/array
// initialisers.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the set of written names, or nil when body is nil.
func CollectWrittenLocalNames(body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return nil
	}
	written := make(map[string]bool)
	ast.Inspect(body, func(n ast.Node) bool {
		recordWriteFromNode(n, written)
		return true
	})
	return written
}

// CollectFreeVarsForLit walks lit and adds every identifier referenced inside, but not
// declared inside, to captured. Nested closures are descended into so transitive captures
// are also recorded.
//
// Takes lit (*ast.FuncLit) which is the function literal.
// Takes captured (map[string]bool) which receives free-variable names.
func CollectFreeVarsForLit(lit *ast.FuncLit, captured map[string]bool) {
	localDefs := litLocalDefs(lit)
	scopeStarts, definingIdents := litLocalScopeStarts(lit)
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if nestedLit, ok := n.(*ast.FuncLit); ok {
			CollectFreeVarsForLit(nestedLit, captured)
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || id.Name == "_" || definingIdents[id] {
			return true
		}
		if !localDefs[id.Name] {
			captured[id.Name] = true
			return true
		}

		if start, declared := scopeStarts[id.Name]; declared && start.IsValid() && id.Pos() < start {
			captured[id.Name] = true
		}
		return true
	})
}

// CollectHeapPromotedNames populates the unified heapPromotedNames set used by
// compileFunctionBody and compileClosureBody.
//
// Merges names captured by inner closures that need heap promotion (via
// CollectClosureCapturedNamesFiltered) with names whose address is taken anywhere in the
// function body (via collectAddressTakenLocals). The merge guarantees every name needing
// a stable heap address receives exactly one isa.OpAllocIndirect at its declaration site
// rather than one per `&` expression, which would re-allocate inside loops and drop
// iteration-to-iteration state.
//
// Takes typeContext (*Compiler) which is forwarded to the closure pre-pass for
// go/types-driven filtering of read-only captures.
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the union of names to heap-promote at declaration, or nil when both sources are
// empty.
func CollectHeapPromotedNames(typeContext *Context, body *ast.BlockStmt) map[string]bool {
	closureCaptures := CollectClosureCapturedNamesFiltered(typeContext, body)
	addressTaken := collectAddressTakenLocals(body)
	if closureCaptures == nil && addressTaken == nil {
		return nil
	}
	merged := make(map[string]bool, len(closureCaptures)+len(addressTaken))
	for name := range closureCaptures {
		merged[name] = true
	}
	for name := range addressTaken {
		merged[name] = true
	}
	return merged
}

// CollectLocalDefs walks body and records every variable name defined within it via short
// declarations, var specs, and range statements. Nested function literals are not
// descended into.
//
// Takes body (*ast.BlockStmt) which is the block to walk.
// Takes definitions (map[string]bool) which accumulates the defined variable names.
func CollectLocalDefs(body *ast.BlockStmt, definitions map[string]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, isFuncLit := n.(*ast.FuncLit); isFuncLit {
			return false
		}

		switch s := n.(type) {
		case *ast.AssignStmt:
			collectAssignDefs(s, definitions)
		case *ast.ValueSpec:
			for _, name := range s.Names {
				definitions[name.Name] = true
			}
		case *ast.RangeStmt:
			collectRangeDefs(s, definitions)
		}
		return true
	})
}

// addByReferenceCaptures adds to captured every free variable that must be captured by
// reference although no closure mutates it: struct and array variables (a snapshot would
// copy them) and variables the enclosing function assigns as a whole (a snapshot would
// miss the later writes).
//
// Takes typeContext (*Context) which supplies type information; may be nil.
// Takes body (*ast.BlockStmt) which is the enclosing function body.
// Takes freeVars (map[string]bool) which names the closures' free variables.
// Takes captured (map[string]bool) which receives the promoted names.
func addByReferenceCaptures(typeContext *Context, body *ast.BlockStmt, freeVars, captured map[string]bool) {
	var info *types.Info
	if typeContext != nil {
		info = typeContext.Info
	}
	assigned := CollectDirectlyAssignedNames(info, body)
	for name := range freeVars {
		if captured[name] {
			continue
		}
		if assigned[name] || isStructOrArrayCapturedName(typeContext, body, name) {
			captured[name] = true
		}
	}
}

// collectReassignedCapturesForLit records captured names that are reassigned whole-value
// inside lit, which require an indirect cell so the change reaches the declaring frame.
//
// Takes lit (*ast.FuncLit) which is the function literal to inspect.
// Takes reassigned (map[string]bool) which receives the reassigned names.
func collectReassignedCapturesForLit(lit *ast.FuncLit, reassigned map[string]bool) {
	collectReassignedCapturesForLitAtDepth(lit, reassigned, 0)
}

// collectReassignedCapturesForLitAtDepth walks a closure literal at the given nesting
// depth, recording reassigned captured names.
//
// Takes lit (*ast.FuncLit) which is the closure literal to walk.
// Takes reassigned (map[string]bool) which accumulates names.
// Takes depth (int) which is the current nesting depth.
func collectReassignedCapturesForLitAtDepth(lit *ast.FuncLit, reassigned map[string]bool, depth int) {
	walkCapturedAssignments(lit, reassigned, recordCapturedReassignment, depth,
		collectReassignedCapturesForLitAtDepth)
}

// walkCapturedAssignments is the shared spine for capture scanners.
//
// Stops at maxClosureNestingDepth as defence-in-depth against stack overflow.
//
// Takes lit (*ast.FuncLit) which is the literal being scanned.
// Takes captured (map[string]bool) which accumulates names the recordFunc marks.
// Takes recordFunc (func(ast.Node, func(*ast.Ident))) which decides whether a given AST
// node implies the desired flavour of capture.
// Takes depth (int) which tracks the current closure nesting level.
// Takes recurse (func(*ast.FuncLit, map[string]bool, int)) which the walker invokes on
// nested closures so the caller's flavour applies uniformly across the closure forest.
func walkCapturedAssignments(
	lit *ast.FuncLit,
	captured map[string]bool,
	recordFunc func(ast.Node, func(*ast.Ident)),
	depth int,
	recurse func(*ast.FuncLit, map[string]bool, int),
) {
	if depth >= maxClosureNestingDepth {
		return
	}
	localDefs := litLocalDefs(lit)
	markName := func(id *ast.Ident) {
		if id == nil || id.Name == "_" || localDefs[id.Name] {
			return
		}
		captured[id.Name] = true
	}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		if nestedLit, ok := n.(*ast.FuncLit); ok {
			recurse(nestedLit, captured, depth+1)
			return false
		}
		recordFunc(n, markName)
		return true
	})
}

// recordCapturedReassignment reports identifiers whose entire value is replaced
// (bare-ident LHS of assignment, inc/dec, or address-of), excluding indexed and selector
// LHS forms which preserve the header. Used by collectReassignedCapturesForLit.
//
// Takes n (ast.Node) which is the node to test.
// Takes markName (func(*ast.Ident)) which is invoked for each reassigned identifier.
func recordCapturedReassignment(n ast.Node, markName func(*ast.Ident)) {
	switch node := n.(type) {
	case *ast.AssignStmt:
		if node.Tok == token.DEFINE {
			return
		}
		for _, lhs := range node.Lhs {
			if id, ok := lhs.(*ast.Ident); ok {
				markName(id)
			}
		}
	case *ast.IncDecStmt:
		if id, ok := node.X.(*ast.Ident); ok {
			markName(id)
		}
	case *ast.UnaryExpr:
		if node.Op != token.AND {
			return
		}
		if id, ok := node.X.(*ast.Ident); ok {
			markName(id)
		}
	}
}

// isPrimitiveSliceCapturedName reports whether name is a primitive slice.
//
// True when name resolves to one of the six primitive slice types (`[]int64`,
// `[]float64`, `[]string`, `[]bool`, `[]uint64`, `[]byte`) and therefore qualifies for
// the typed-bank snapshot capture path instead of heap promotion. Slices are reference
// types: the captured slice header aliases the parent's array, so element writes
// propagate without an indirect pointer. Heap-promoting these would route the snapshot
// through a general-bank *T pointer cell and defeat the typed-bank routing.
//
// Takes typeContext (*Compiler) which provides go/types information.
// Takes body (*ast.BlockStmt) where the declaration lives.
// Takes name (string) which is the captured variable name.
//
// Returns bool which reports whether name resolves to a primitive slice type.
func isPrimitiveSliceCapturedName(typeContext *Context, body *ast.BlockStmt, name string) bool {
	if typeContext == nil || typeContext.Info == nil {
		return false
	}
	ident := findCapturedNameIdent(body, name)
	if ident == nil {
		return false
	}
	typeObject := typeContext.Info.Defs[ident]
	if typeObject == nil {
		typeObject = typeContext.Info.Uses[ident]
	}
	if typeObject == nil {
		return false
	}
	return isa.IsTypedSliceKind(typemap.KindForTypedSlice(typeObject.Type()))
}

// recordWriteFromNode inspects a single AST node and records any implicit or explicit
// write of a named local into written.
//
// `:=` is a fresh declaration, not a write to an existing local. Compound assignments and
// `=` are real writes. Address-of is treated as a write because it permits later writes
// through the pointer.
//
// Takes node (ast.Node) which is the candidate AST node.
// Takes written (map[string]bool) which accumulates written names.
func recordWriteFromNode(node ast.Node, written map[string]bool) {
	switch typed := node.(type) {
	case *ast.AssignStmt:
		recordAssignmentWrites(typed, written)
	case *ast.IncDecStmt:
		recordWriteRoot(typed.X, written)
	case *ast.RangeStmt:
		recordRangeWrites(typed, written)
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			recordWriteRoot(typed.X, written)
		}
	}
}

// recordAssignmentWrites records every non-define assignment LHS as a write to the
// underlying local.
//
// Takes statement (*ast.AssignStmt) which is the assignment to inspect.
// Takes written (map[string]bool) which accumulates written names.
func recordAssignmentWrites(statement *ast.AssignStmt, written map[string]bool) {
	if statement.Tok == token.DEFINE {
		return
	}
	for _, lhs := range statement.Lhs {
		recordWriteRoot(lhs, written)
	}
}

// recordRangeWrites records the key and value targets of a range statement when they are
// non-define assignments.
//
// Takes statement (*ast.RangeStmt) which is the range statement.
// Takes written (map[string]bool) which accumulates written names.
func recordRangeWrites(statement *ast.RangeStmt, written map[string]bool) {
	if statement.Tok == token.DEFINE {
		return
	}
	recordWriteRoot(statement.Key, written)
	recordWriteRoot(statement.Value, written)
}

// recordWriteRoot adds the root identifier name of expression to the written set.
// Nil-safe: a nil expression or non-identifier root is a no-op.
//
// Takes expression (ast.Expr) which is the LHS expression to peel.
// Takes written (map[string]bool) which accumulates written names.
func recordWriteRoot(expression ast.Expr, written map[string]bool) {
	if expression == nil {
		return
	}
	if name := extractAssignmentRoot(expression); name != "" {
		written[name] = true
	}
}

// extractAssignmentRoot returns the root identifier name for an LHS expression by peeling
// selector, index, star, slice and paren wrappers.
//
// Takes expression (ast.Expr) which is the LHS expression to peel.
//
// Returns the root identifier name, or "" when no non-blank identifier is found.
func extractAssignmentRoot(expression ast.Expr) string {
	for {
		switch e := expression.(type) {
		case *ast.Ident:
			if e.Name == "_" {
				return ""
			}
			return e.Name
		case *ast.SelectorExpr:
			expression = e.X
		case *ast.IndexExpr:
			expression = e.X
		case *ast.StarExpr:
			expression = e.X
		case *ast.SliceExpr:
			expression = e.X
		case *ast.ParenExpr:
			expression = e.X
		default:
			return ""
		}
	}
}

// isStructOrArrayCapturedName reports whether name resolves to a struct or array static
// type within body.
//
// Takes typeContext (*Compiler) which provides go/types information.
// Takes body (*ast.BlockStmt) where the declaration lives.
// Takes name (string) which is the captured variable name.
//
// Returns true when name resolves to a struct or array static type.
func isStructOrArrayCapturedName(typeContext *Context, body *ast.BlockStmt, name string) bool {
	if typeContext == nil || typeContext.Info == nil {
		return false
	}
	ident := findCapturedNameIdent(body, name)
	if ident == nil {
		return false
	}
	typeObject := typeContext.Info.Defs[ident]
	if typeObject == nil {
		typeObject = typeContext.Info.Uses[ident]
	}
	if typeObject == nil {
		return false
	}
	return shouldHeapPromoteCapturedKind(typeObject.Type())
}

// findCapturedNameIdent locates the *ast.Ident that introduces name as a local in body.
// Walks AssignStmt (token.DEFINE) and ValueSpec declarations and returns the first
// matching identifier.
//
// Takes body (*ast.BlockStmt) which scopes the search.
// Takes name (string) which is the variable name to find.
//
// Returns the declaring ident, or nil when none is found.
func findCapturedNameIdent(body *ast.BlockStmt, name string) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		found = matchCapturedIdent(n, name)
		return found == nil
	})
	return found
}

// matchCapturedIdent returns the declaring identifier for name when n is an AssignStmt
// with token.DEFINE or a ValueSpec that introduces it.
//
// Takes n (ast.Node) which is the candidate node.
// Takes name (string) which is the identifier being searched for.
//
// Returns the declaring identifier, or nil when n does not declare name.
func matchCapturedIdent(n ast.Node, name string) *ast.Ident {
	switch node := n.(type) {
	case *ast.AssignStmt:
		if node.Tok != token.DEFINE {
			return nil
		}
		for _, lhs := range node.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == name {
				return id
			}
		}
	case *ast.ValueSpec:
		for _, id := range node.Names {
			if id.Name == name {
				return id
			}
		}
	}
	return nil
}

// litLocalScopeStarts records where each local declared inside lit comes into scope (the
// end of its short declaration, value spec or range clause) and which identifiers are the
// declarations themselves. Parameters and results are in scope throughout and get no
// entry.
//
// Takes lit (*ast.FuncLit) which is the closure.
//
// Returns the earliest scope start per name and the set of defining identifiers.
func litLocalScopeStarts(lit *ast.FuncLit) (map[string]token.Pos, map[*ast.Ident]bool) {
	recorder := scopeStartRecorder{starts: make(map[string]token.Pos), defining: make(map[*ast.Ident]bool)}
	ast.Inspect(lit.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			recorder.recordAssign(s)
		case *ast.ValueSpec:
			recorder.recordValueSpec(s)
		case *ast.RangeStmt:
			recorder.recordRange(s)
		default:
		}
		return true
	})
	return recorder.starts, recorder.defining
}

// collectMutatedCapturesForLit records captured names mutated inside lit or any nested
// closure. Read-only captures remain on the snapshot-cell path.
//
// Takes lit (*ast.FuncLit) which is the function literal to inspect.
// Takes mutated (map[string]bool) which receives the mutated names.
func collectMutatedCapturesForLit(lit *ast.FuncLit, mutated map[string]bool) {
	collectMutatedCapturesForLitAtDepth(lit, mutated, 0)
}

// collectMutatedCapturesForLitAtDepth walks a closure literal at the given nesting depth,
// recording mutated captured names.
//
// Takes lit (*ast.FuncLit) which is the closure literal to walk.
// Takes mutated (map[string]bool) which accumulates names.
// Takes depth (int) which is the current nesting depth.
func collectMutatedCapturesForLitAtDepth(lit *ast.FuncLit, mutated map[string]bool, depth int) {
	walkCapturedAssignments(lit, mutated, recordCapturedMutation, depth,
		collectMutatedCapturesForLitAtDepth)
}

// recordCapturedMutation inspects a single AST node and reports each mutated identifier
// through markName. `*p = ...` does not mark `p` because the snapshot already aliases the
// same heap memory.
//
// Takes n (ast.Node) which is the node to test.
// Takes markName (func(*ast.Ident)) which is invoked for each mutated identifier.
func recordCapturedMutation(n ast.Node, markName func(*ast.Ident)) {
	switch node := n.(type) {
	case *ast.AssignStmt:
		if node.Tok == token.DEFINE {
			return
		}
		for _, lhs := range node.Lhs {
			if id := rootIdentForMutation(lhs); id != nil {
				markName(id)
			}
		}
	case *ast.IncDecStmt:
		if id := rootIdentForMutation(node.X); id != nil {
			markName(id)
		}
	case *ast.UnaryExpr:
		if node.Op == token.AND {
			if id := rootIdentForMutation(node.X); id != nil {
				markName(id)
			}
		}
	}
}

// collectAddressTakenLocals returns local names whose address is taken anywhere in body.
// These names receive a single isa.OpAllocIndirect at their declaration site rather than
// one per `&` encounter.
//
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the set of address-taken names, or nil when body is nil.
func collectAddressTakenLocals(body *ast.BlockStmt) map[string]bool {
	if body == nil {
		return nil
	}
	var result map[string]bool
	ast.Inspect(body, func(n ast.Node) bool {
		unary, ok := n.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		ident := rootIdentForMutation(unary.X)
		if ident == nil || ident.Name == "_" {
			return true
		}
		if result == nil {
			result = make(map[string]bool)
		}
		result[ident.Name] = true
		return true
	})
	return result
}

// rootIdentForMutation returns the root identifier of an L-value expression.
//
// Selector (`x.field`), index (`x[i]`), and generic index (`x[T]`) peel to their receiver
// because mutating a field, element, or subscript reaches into the parent struct or array
// and therefore requires the parent to be heap-promoted. StarExpr is intentionally not
// peeled: `*p` is reachable through the closure's snapshot of `p`, so writing through
// `*p` does not require `p` itself to be heap-promoted.
//
// Takes expression (ast.Expr) which is the expression on the mutation side.
//
// Returns the root identifier or nil when the expression bottoms out at something other
// than an identifier.
func rootIdentForMutation(expression ast.Expr) *ast.Ident {
	for {
		switch e := expression.(type) {
		case *ast.Ident:
			return e
		case *ast.SelectorExpr:
			expression = e.X
		case *ast.IndexExpr:
			expression = e.X
		case *ast.IndexListExpr:
			expression = e.X
		default:
			return nil
		}
	}
}

// litLocalDefs returns the set of names declared by lit's parameters, named results, and
// any local declarations inside its body.
//
// Takes lit (*ast.FuncLit) whose declared names are collected.
//
// Returns the populated set of locally-declared names.
func litLocalDefs(lit *ast.FuncLit) map[string]bool {
	localDefs := make(map[string]bool)
	addFieldListNames(lit.Type.Params, localDefs)
	addFieldListNames(lit.Type.Results, localDefs)
	CollectLocalDefs(lit.Body, localDefs)
	return localDefs
}

// addFieldListNames adds every identifier in fields to set. A nil fields argument (no
// parameters or no results) is tolerated.
//
// Takes fields (*ast.FieldList) which holds the parameter or result field declarations.
// Takes set (map[string]bool) which receives the names.
func addFieldListNames(fields *ast.FieldList, set map[string]bool) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			set[name.Name] = true
		}
	}
}

// shouldHeapPromoteCapturedKind reports whether a static type warrants heap promotion.
// Structs and arrays qualify because a later auto-address-of for a pointer-receiver call
// would miss already-created closure cells.
//
// Takes t (types.Type) which is the variable's static type.
//
// Returns true when t.Underlying() is a struct or array.
func shouldHeapPromoteCapturedKind(t types.Type) bool {
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

// collectAssignDefs records the left-hand side identifiers of a short declaration (:=)
// into definitions.
//
// Takes s (*ast.AssignStmt) which is the assignment to inspect.
// Takes definitions (map[string]bool) which accumulates the defined variable names.
func collectAssignDefs(s *ast.AssignStmt, definitions map[string]bool) {
	if s.Tok != token.DEFINE {
		return
	}
	for _, leftHandSide := range s.Lhs {
		if id, ok := leftHandSide.(*ast.Ident); ok {
			definitions[id.Name] = true
		}
	}
}

// collectRangeDefs records the key and value identifiers of a range statement declared
// with := into definitions.
//
// Takes s (*ast.RangeStmt) which is the range statement to inspect.
// Takes definitions (map[string]bool) which accumulates the defined variable names.
func collectRangeDefs(s *ast.RangeStmt, definitions map[string]bool) {
	if s.Tok != token.DEFINE {
		return
	}
	if id, ok := s.Key.(*ast.Ident); ok {
		definitions[id.Name] = true
	}
	if s.Value == nil {
		return
	}
	if id, ok := s.Value.(*ast.Ident); ok {
		definitions[id.Name] = true
	}
}
