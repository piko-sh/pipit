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
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

var (
	// typedSliceLocalsAllowedBuiltins lists builtin calls that do not disqualify a
	// typed-slice candidate. `append` is handled separately in disqualifyCallExpr.
	typedSliceLocalsAllowedBuiltins = map[string]bool{
		"len": true,
	}
)

// ClassifyTypedSliceParamNames returns the surviving typed-slice parameters after walking
// body to disqualify candidates whose usage forbids typed-bank routing.
//
// Takes typeContext (*Compiler) which provides go/types info and the functionTable for
// callee-kind resolution.
// Takes body (*ast.BlockStmt) which is the function body.
// Takes candidates (map[string]isa.RegisterKind) which is the per-parameter typed-bank
// kind, keyed by parameter name.
//
// Returns the subset of candidates that survived disqualification, or nil when body is
// nil or candidates is empty.
func ClassifyTypedSliceParamNames(typeContext *Context, body *ast.BlockStmt, candidates map[string]isa.RegisterKind) map[string]isa.RegisterKind {
	if body == nil || len(candidates) == 0 {
		return nil
	}
	candidateSet := make(map[string]bool, len(candidates))
	for name := range candidates {
		candidateSet[name] = true
	}
	disqualified := make(map[string]bool)
	walkTypedSliceDisqualifiers(typeContext, body, candidateSet, candidates, disqualified)
	demoteUnamortisedNarrowCandidates(typeContext, body, candidateSet, disqualified)
	survivors := make(map[string]isa.RegisterKind, len(candidates))
	for name, kind := range candidates {
		if !disqualified[name] {
			survivors[name] = kind
		}
	}
	return survivors
}

// ApplySpecialisationTypedSliceSurvivors overlays the survivor verdict on a specialised
// stub, demoting typed-slice-bank parameters that failed the disqualifier walk back to
// the general bank.
//
// Takes typeContext (*Compiler) which is the active Compiler.
// Takes declaration (*ast.FuncDecl) which is the generic declaration.
// Takes specCF (*CompiledFunction) which is the specialisation stub.
func ApplySpecialisationTypedSliceSurvivors(typeContext *Context, declaration *ast.FuncDecl, specCF *program.CompiledFunction) {
	if declaration == nil || declaration.Type == nil || declaration.Type.Params == nil || specCF == nil {
		return
	}
	survivors := classifyTypedSliceSpecialisationParameters(typeContext, declaration, specCF)
	if survivors == nil {
		return
	}
	parameterCount := len(specCF.ParameterKinds)
	parameterIndex := 0
	if specCF.HasReceiver {
		parameterIndex = 1
	}
	for _, field := range declaration.Type.Params.List {
		if len(field.Names) == 0 {
			if parameterIndex >= parameterCount {
				break
			}
			parameterIndex++
			continue
		}
		for _, name := range field.Names {
			if parameterIndex >= parameterCount {
				break
			}
			applySpecialisationSurvivorVerdict(typeContext, specCF, name.Name, parameterIndex, survivors)
			parameterIndex++
		}
	}
}

// ClassifyTypedSliceLocals maps typed-slice-bank-eligible locals in body to their
// typed-slice register kind, disqualifying any candidate whose usage is incompatible with
// typed-bank routing.
//
// Takes typeContext (*Compiler) which provides go/types information.
// Takes body (*ast.BlockStmt) which is the function body to inspect.
//
// Returns the map from local name to typed-slice register kind; returns nil when no
// candidates survive classification.
func ClassifyTypedSliceLocals(typeContext *Context, body *ast.BlockStmt) map[string]isa.RegisterKind {
	if body == nil || typeContext == nil {
		return nil
	}
	candidates := collectTypedSliceCandidates(typeContext, body)
	if len(candidates) == 0 {
		return nil
	}
	candidateSet := make(map[string]bool, len(candidates))
	for name := range candidates {
		candidateSet[name] = true
	}
	disqualified := make(map[string]bool)
	walkTypedSliceDisqualifiers(typeContext, body, candidateSet, candidates, disqualified)
	for name := range disqualified {
		delete(candidates, name)
	}
	if len(candidates) == 0 {
		return nil
	}
	return candidates
}

// classifyTypedSliceSpecialisationParameters seeds the survivor map for a specialisation.
//
// Specialised-stub counterpart of classifyTypedSliceParameters. Seeds the candidate set
// from the already-substituted parameter types on specCF (rather than from a
// types.Signature) and walks the shared generic body so that the survivor map carries the
// post-substitution name -> kind pairs to be applied as parameterKinds overrides in
// compileSpecialisedBody.
//
// The receiver slot (when present) occupies index 0 of specCF.ParameterTypeRefs but is
// not represented in declaration.Type.Params.List, so the walk starts at index 1 in that
// case. Unnamed fields advance the parameter index without contributing a candidate.
// Variadic last and still-generic-typed slots are skipped for the same reasons as in the
// non-specialised classifier.
//
// Takes typeContext (*Compiler) which provides go/types info.
// Takes declaration (*ast.FuncDecl) which is the generic declaration.
// Takes specCF (*CompiledFunction) which is the specialisation stub.
//
// Returns map[string]isa.RegisterKind which maps surviving parameter names to their
// typed-slice kind, or nil when none survived.
func classifyTypedSliceSpecialisationParameters(typeContext *Context, declaration *ast.FuncDecl, specCF *program.CompiledFunction) map[string]isa.RegisterKind {
	if declaration == nil || declaration.Body == nil || specCF == nil {
		return nil
	}
	if declaration.Type == nil || declaration.Type.Params == nil {
		return nil
	}
	parameterCount := len(specCF.ParameterTypeRefs)
	candidates := map[string]isa.RegisterKind{}
	parameterIndex := 0
	if specCF.HasReceiver {
		parameterIndex = 1
	}
	for _, field := range declaration.Type.Params.List {
		if len(field.Names) == 0 {
			if parameterIndex >= parameterCount {
				break
			}
			parameterIndex++
			continue
		}
		if !collectSpecialisationCandidates(field.Names, &parameterIndex, parameterCount, specCF, candidates) {
			break
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	return ClassifyTypedSliceParamNames(typeContext, declaration.Body, candidates)
}

// collectSpecialisationCandidates walks the names within one declaration.Type.Params.List
// field, advancing parameterIndex per name and recording typed-slice-eligible names into
// candidates. Returns false when parameterIndex outruns parameterCount so the outer
// walker can break.
//
// Takes names ([]*ast.Ident) which are the identifiers in the field.
// Takes parameterIndex (*int) which is the running slot index (mutated).
// Takes parameterCount (int) which is the slot upper bound.
// Takes specCF (*CompiledFunction) which provides parameterTypeRefs + IsVariadic.
// Takes candidates (map[string]isa.RegisterKind) which receives accepted parameter names
// mapped to their typed-slice kind.
//
// Returns true if the walk should continue with the next field; false when it ran out of
// slots and the outer loop should break.
func collectSpecialisationCandidates(names []*ast.Ident, parameterIndex *int, parameterCount int, specCF *program.CompiledFunction, candidates map[string]isa.RegisterKind) bool {
	for _, name := range names {
		if *parameterIndex >= parameterCount {
			return false
		}
		paramType := specCF.ParameterTypeRefs[*parameterIndex]
		isVariadicLast := specCF.IsVariadic && *parameterIndex == parameterCount-1
		*parameterIndex++
		if isVariadicLast {
			continue
		}
		if typemap.IsTypeParameter(paramType) || typemap.ContainsTypeParameter(paramType) {
			continue
		}
		kind := typemap.KindForCallSlot(paramType)
		if !isa.IsTypedSliceKind(kind) {
			continue
		}
		if name.Name == "" || name.Name == "_" {
			continue
		}
		candidates[name.Name] = kind
	}
	return true
}

// applySpecialisationSurvivorVerdict applies the typed-slice survivor decision for one
// parameter slot of the specialisation: demotes the kind to either the survivor map's
// entry, or the substituted-type re-derivation when the parameter was disqualified; then
// refreshes the verdict-vector entry so cross-function consumers see the final kind via
// KindForPromotedSlot.
//
// Takes typeContext (*Compiler) which is the active Compiler.
// Takes specCF (*CompiledFunction) which is the specialisation stub.
// Takes name (string) which is the parameter's declared identifier.
// Takes index (int) which is the parameter slot index.
// Takes survivors (map[string]isa.RegisterKind) which is the survivor map.
func applySpecialisationSurvivorVerdict(typeContext *Context, specCF *program.CompiledFunction, name string, index int, survivors map[string]isa.RegisterKind) {
	if isa.IsTypedSliceKind(specCF.ParameterKinds[index]) {
		if survivorKind, ok := survivors[name]; ok {
			specCF.ParameterKinds[index] = survivorKind
		} else {
			specCF.ParameterKinds[index] = typeContext.kindFor(specCF.ParameterTypeRefs[index])
		}
	}
	if index < len(specCF.ParameterTypedSlicePromoted) {
		specCF.ParameterTypedSlicePromoted[index] = isa.IsTypedSliceKind(specCF.ParameterKinds[index])
	}
}

// collectTypedSliceCandidates lists typed-bank-eligible local declarations from
// `make([]T, ...)`, `var name []T = make(...)` and map-lookup `:=` initialisers.
//
// Takes typeContext (*Compiler) which is consulted for go/types information.
// Takes body (*ast.BlockStmt) which is the function body.
//
// Returns the candidate map keyed by local name; returns an empty map when no candidates
// are found.
func collectTypedSliceCandidates(typeContext *Context, body *ast.BlockStmt) map[string]isa.RegisterKind {
	candidates := make(map[string]isa.RegisterKind)
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if statement.Tok == token.DEFINE {
				collectMakeCandidates(typeContext, statement.Lhs, statement.Rhs, candidates, true)
			}
		case *ast.DeclStmt:
			collectMakeCandidatesFromDecl(typeContext, statement, candidates)
		}
		return true
	})
	return candidates
}

// collectMakeCandidatesFromDecl harvests typed-slice candidates from a `var name =
// make([]T, ...)` declaration statement.
//
// Takes typeContext (*Compiler) which carries go/types information.
// Takes statement (*ast.DeclStmt) which is the declaration to scan.
// Takes candidates (map[string]isa.RegisterKind) which receives matched candidates.
func collectMakeCandidatesFromDecl(typeContext *Context, statement *ast.DeclStmt, candidates map[string]isa.RegisterKind) {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok || declaration.Tok != token.VAR {
		return
	}
	for _, spec := range declaration.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		identExprs := make([]ast.Expr, len(valueSpec.Names))
		for i, name := range valueSpec.Names {
			identExprs[i] = name
		}
		collectMakeCandidates(typeContext, identExprs, valueSpec.Values, candidates, false)
	}
}

// collectMakeCandidates inspects matched LHS/RHS pairs from a declaration and admits an
// LHS name to the candidate map when the RHS is a `make([]T, ...)` call, or a map lookup
// yielding an unnamed `[]T`, whose element type maps to one of the typed-slice banks.
//
// Takes typeContext (*Compiler) which carries go/types information.
// Takes leftSides ([]ast.Expr) which are the declaration's LHS expressions; only
// *ast.Ident entries are considered.
// Takes rightSides ([]ast.Expr) which are the matched RHS expressions.
// Takes candidates (map[string]isa.RegisterKind) which receives admitted names paired
// with their typed-slice register kind.
// Takes allowMapLookup (bool) which admits map-lookup initialisers; only the `:=` form
// sets it, because the var declaration path never consults the classification.
func collectMakeCandidates(typeContext *Context, leftSides, rightSides []ast.Expr, candidates map[string]isa.RegisterKind, allowMapLookup bool) {
	for i, leftSide := range leftSides {
		identifier, ok := leftSide.(*ast.Ident)
		if !ok || identifier.Name == typemap.BlankIdentName {
			continue
		}
		if i >= len(rightSides) {
			continue
		}
		kind, ok := typedSliceInitialiserKind(typeContext, rightSides[i], allowMapLookup)
		if !ok {
			continue
		}
		candidates[identifier.Name] = kind
	}
}

// typedSliceInitialiserKind classifies a declaration's right-hand side as a typed-slice
// source: a `make([]T, ...)` call, or a map lookup whose element type is an unnamed `[]T`
// of a typed-bank element.
//
// Takes typeContext (*Context) which carries go/types information.
// Takes expression (ast.Expr) which is the right-hand side of the declaration.
// Takes allowMapLookup (bool) which admits the map-lookup source.
//
// Returns the matched typed-slice register kind and true on success; isa.RegisterGeneral
// and false for every other initialiser.
func typedSliceInitialiserKind(typeContext *Context, expression ast.Expr, allowMapLookup bool) (isa.RegisterKind, bool) {
	switch source := expression.(type) {
	case *ast.CallExpr:
		return typedMakeSliceCallKind(typeContext, source)
	case *ast.IndexExpr:
		if !allowMapLookup {
			return isa.RegisterGeneral, false
		}
		return typedMapLookupSliceKind(typeContext, source)
	default:
		return isa.RegisterGeneral, false
	}
}

// typedMapLookupSliceKind reports whether expression indexes a map whose element type is
// an unnamed slice of a typed-bank element. Named slice types are refused because the
// adopt sub-op requires an exact `[]T` type assertion.
//
// Takes typeContext (*Context) which carries go/types information.
// Takes expression (*ast.IndexExpr) which is the suspected map lookup.
//
// Returns the matched typed-slice register kind and true on success; isa.RegisterGeneral
// and false otherwise.
func typedMapLookupSliceKind(typeContext *Context, expression *ast.IndexExpr) (isa.RegisterKind, bool) {
	tv, ok := typeContext.Info.Types[expression.X]
	if !ok || tv.Type == nil {
		return isa.RegisterGeneral, false
	}
	mapType, ok := tv.Type.Underlying().(*types.Map)
	if !ok {
		return isa.RegisterGeneral, false
	}
	element := types.Unalias(mapType.Elem())
	if _, named := element.(*types.Named); named {
		return isa.RegisterGeneral, false
	}
	kind := typemap.KindForTypedSlice(element)
	if !isa.IsTypedSliceKind(kind) {
		return isa.RegisterGeneral, false
	}
	return kind, true
}

// typedMakeSliceCallKind reports whether expression is a `make([]T, ...)` call whose
// element type maps to one of the typed-slice register kinds.
//
// Takes typeContext (*Compiler) which provides go/types lookups.
// Takes expression (*ast.CallExpr) which is the suspected make call.
//
// Returns the matched typed-slice register kind and true on success; returns
// isa.RegisterGeneral and false otherwise.
func typedMakeSliceCallKind(typeContext *Context, expression *ast.CallExpr) (isa.RegisterKind, bool) {
	identifier, ok := expression.Fun.(*ast.Ident)
	if !ok || identifier.Name != "make" {
		return isa.RegisterGeneral, false
	}
	if len(expression.Args) == 0 {
		return isa.RegisterGeneral, false
	}
	tv, ok := typeContext.Info.Types[expression.Args[0]]
	if !ok {
		return isa.RegisterGeneral, false
	}
	slice, ok := tv.Type.Underlying().(*types.Slice)
	if !ok {
		return isa.RegisterGeneral, false
	}
	kind := typemap.KindForTypedSlice(tv.Type)
	if isa.IsTypedSliceKind(kind) {
		return kind, true
	}
	_ = slice
	return isa.RegisterGeneral, false
}

// walkTypedSliceDisqualifiers inspects body for any usage of a candidate name
// incompatible with typed-slice bank routing and records the offending names in
// disqualified.
//
// Takes typeContext (*Compiler) which carries go/types information for parameter-kind
// classification of call sites.
// Takes body (*ast.BlockStmt) which is the function body.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank, used by call-site/return-site disqualifiers to compare against the
// callee's parameter kinds and the current function's result kinds.
// Takes disqualified (map[string]bool) which collects disqualified names; the caller
// deletes them from candidates after the walk completes.
func walkTypedSliceDisqualifiers(typeContext *Context, body *ast.BlockStmt, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind, disqualified map[string]bool) {
	ast.Inspect(body, func(node ast.Node) bool {
		return disqualifyTypedSliceNode(typeContext, node, candidates, candidateKinds, disqualified)
	})
}

// disqualifyTypedSliceNode dispatches one AST node from the disqualifier walk to the
// matching kind-specific helper.
//
// Takes typeContext (*Compiler) which is forwarded to call-site helpers.
// Takes node (ast.Node) which is the candidate AST node.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
//
// Returns the recurse-into-children flag for ast.Inspect: false for FuncLit so the
// closure-capture helper can claim every identifier in its body, true otherwise.
func disqualifyTypedSliceNode(typeContext *Context, node ast.Node, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind, disqualified map[string]bool) bool {
	switch statement := node.(type) {
	case *ast.FuncLit:
		disqualifyClosureCapture(statement, candidates, candidateKinds, disqualified)
		return false
	case *ast.UnaryExpr:
		disqualifyAddressOf(statement, candidates, disqualified)
	case *ast.CallExpr:
		disqualifyCallExpr(typeContext, statement, candidates, candidateKinds, disqualified)
	case *ast.ReturnStmt:
		disqualifyReturnIdents(typeContext, statement, candidates, candidateKinds, disqualified)
	case *ast.AssignStmt:
		disqualifyAssignStmt(statement, candidates, disqualified)
		disqualifyNarrowElementAssign(typeContext, statement, candidates, disqualified)
	case *ast.IncDecStmt:
		disqualifyNarrowElementIncDec(typeContext, statement, candidates, disqualified)
	case *ast.TypeAssertExpr:
		disqualifyIdentRef(statement.X, candidates, disqualified)
	case *ast.TypeSwitchStmt:
		disqualifyTypeSwitch(statement, candidates, disqualified)
	case *ast.SliceExpr:

		_ = statement.X
	case *ast.SendStmt:

		_ = statement.Value
	case *ast.CompositeLit:
		disqualifyCompositeLit(statement, candidates, disqualified)
	}
	return true
}

// candidateHasNarrowElements reports whether the identifier names a candidate slice whose
// element type is a sub-64-bit integer.
//
// Narrow elements are widened to 64 bits inside the typed banks, so passing such a slice
// across a call boundary materialises a widened copy - element writes in the callee would
// mutate the copy instead of the caller's backing array, silently breaking Go's slice
// aliasing semantics. Mutating callees therefore demote narrow-element parameters to the
// general bank, while read-only callees keep the typed-bank win.
//
// Takes typeContext (*Compiler) which provides go/types info for the body.
// Takes expr (ast.Expr) which should be the indexed collection expression.
// Takes candidates (map[string]bool) which is the candidate set.
//
// Returns the candidate name and true when it is a narrow-element candidate.
func candidateHasNarrowElements(typeContext *Context, expr ast.Expr, candidates map[string]bool) (string, bool) {
	ident, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok || !candidates[ident.Name] {
		return "", false
	}
	identType, ok := typeContext.Info.Types[ident]
	if !ok || identType.Type == nil {
		return ident.Name, true
	}
	slice, ok := identType.Type.Underlying().(*types.Slice)
	if !ok {
		return ident.Name, true
	}
	elem, ok := slice.Elem().Underlying().(*types.Basic)
	if !ok {
		return ident.Name, false
	}

	if elem.Kind() == types.Uint8 {
		return ident.Name, false
	}
	return ident.Name, typemap.NarrowIntegerBitWidth(slice.Elem()) != 0
}

// disqualifyNarrowElementAssign demotes narrow-element candidates that appear as an
// indexed store target on the left-hand side of an assignment (plain or compound).
//
// Takes typeContext (*Compiler) which provides go/types info.
// Takes statement (*ast.AssignStmt) which is the assignment being inspected.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyNarrowElementAssign(typeContext *Context, statement *ast.AssignStmt, candidates, disqualified map[string]bool) {
	for _, lhs := range statement.Lhs {
		indexExpression, ok := ast.Unparen(lhs).(*ast.IndexExpr)
		if !ok {
			continue
		}
		if name, narrow := candidateHasNarrowElements(typeContext, indexExpression.X, candidates); narrow && name != "" {
			disqualified[name] = true
		}
	}
}

// disqualifyNarrowElementIncDec demotes narrow-element candidates mutated through an
// indexed increment or decrement statement.
//
// Takes typeContext (*Compiler) which provides go/types info.
// Takes statement (*ast.IncDecStmt) which is the inc/dec being inspected.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyNarrowElementIncDec(typeContext *Context, statement *ast.IncDecStmt, candidates, disqualified map[string]bool) {
	indexExpression, ok := ast.Unparen(statement.X).(*ast.IndexExpr)
	if !ok {
		return
	}
	if name, narrow := candidateHasNarrowElements(typeContext, indexExpression.X, candidates); narrow && name != "" {
		disqualified[name] = true
	}
}

// disqualifyAddressOf disqualifies candidates whose address is taken, either directly
// (`&s`) or through an index (`&s[i]`).
//
// Takes statement (*ast.UnaryExpr) which is the candidate unary expression.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyAddressOf(statement *ast.UnaryExpr, candidates, disqualified map[string]bool) {
	if statement.Op != token.AND {
		return
	}
	disqualifyIdentRef(statement.X, candidates, disqualified)
	if indexExpression, ok := statement.X.(*ast.IndexExpr); ok {
		disqualifyIdentRef(indexExpression.X, candidates, disqualified)
	}
}

// disqualifyReturnIdents disqualifies candidates returned with mismatched banks.
//
// Candidates that appear in the result list of a return statement are disqualified except
// when the typed bank matches the corresponding result slot's kind on the current
// function. Matching means the typed-slice value can flow through the return ABI without
// demotion; mismatch (general bank, missing result-kind metadata, arity-mismatched
// returns) demotes the candidate to the general bank.
//
// Takes typeContext (*Compiler) which provides the current function's resultKinds.
// Takes statement (*ast.ReturnStmt) which is the return statement.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyReturnIdents(typeContext *Context, statement *ast.ReturnStmt, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind, disqualified map[string]bool) {
	resultKinds := currentFunctionResultKinds(typeContext)
	for resultIndex, result := range statement.Results {
		identifier, isIdent := result.(*ast.Ident)
		if !isIdent {
			disqualifyIdentRef(result, candidates, disqualified)
			continue
		}
		if !candidates[identifier.Name] {
			continue
		}
		if !returnSlotAcceptsTypedSlice(resultKinds, resultIndex, candidateKinds[identifier.Name]) {
			disqualified[identifier.Name] = true
		}
	}
}

// currentFunctionResultKinds returns the current compile target's resultKinds slice, or
// nil when typeContext is nil or the current function metadata has not been populated
// yet.
//
// Takes typeContext (*Compiler).
//
// Returns the result-kind slice or nil.
func currentFunctionResultKinds(typeContext *Context) []isa.RegisterKind {
	if typeContext == nil || typeContext.Function == nil {
		return nil
	}
	return typeContext.Function.ResultKinds
}

// returnSlotAcceptsTypedSlice reports whether the result slot at position resultIndex on
// the current function accepts the typed bank named by candidateKind. The Compiler
// classifies primitive slice return slots onto their typed banks via kindForCallSlot, so
// matching kinds permit the typed local to flow through the return directly.
//
// Takes resultKinds ([]isa.RegisterKind) which is the current function's resultKinds.
// Takes resultIndex (int) which is the slot index in the return statement.
// Takes candidateKind (isa.RegisterKind) which is the candidate's typed bank.
//
// Returns true when the slot's kind matches candidateKind.
func returnSlotAcceptsTypedSlice(resultKinds []isa.RegisterKind, resultIndex int, candidateKind isa.RegisterKind) bool {
	if resultIndex < 0 || resultIndex >= len(resultKinds) {
		return false
	}
	return resultKinds[resultIndex] == candidateKind
}

// disqualifyIdentRef records the candidate name carried by expr as disqualified when expr
// is an *ast.Ident.
//
// Takes expression (ast.Expr) which is the candidate expression.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyIdentRef(expression ast.Expr, candidates, disqualified map[string]bool) {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return
	}
	if candidates[identifier.Name] {
		disqualified[identifier.Name] = true
	}
}

// disqualifyCallExpr disqualifies candidates passed to a call when the callee's matching
// parameter slot does not accept the candidate's typed bank.
//
// Takes typeContext (*Compiler) which carries go/types information.
// Takes expression (*ast.CallExpr) which is the call being inspected.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
// Takes disqualified (map[string]bool) which collects disqualified names.
func disqualifyCallExpr(typeContext *Context, expression *ast.CallExpr, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind, disqualified map[string]bool) {
	if callIsAllowedTypedSliceConsumer(typeContext, expression) {
		return
	}
	if appendAcceptedTypedSliceShape(expression, candidates, candidateKinds) {
		return
	}
	if copyAcceptedTypedSliceShape(expression, candidates, candidateKinds) {
		return
	}
	calleeParameterKinds := tryResolveCalleeParameterKinds(typeContext, expression)
	for argumentIndex, argument := range expression.Args {
		identifier, isIdent := argument.(*ast.Ident)
		if !isIdent {
			continue
		}
		if !candidates[identifier.Name] {
			continue
		}
		if !callSlotAcceptsTypedSlice(calleeParameterKinds, argumentIndex, candidateKinds[identifier.Name]) {
			disqualified[identifier.Name] = true
		}
	}
}

// copyAcceptedTypedSliceShape reports whether expression is a `copy(destination, source)`
// call where destination and source name typed-slice candidates on the same bank. When
// the shape matches, the candidates are NOT disqualified; the compile-Emit picks the
// matching subOpCopySliceXDirect at copy-emission time so the typed bank flows through
// correctly.
//
// The accepted shape is the builtin `copy` ident with exactly two bare ident arguments
// naming candidates whose candidateKinds entries sit on the same typed-slice bank.
// Mismatched-bank copies (one typed, one general, or two typed banks of different kinds)
// are not accepted because the compile-Emit path cannot fuse a typed-direct copy across
// banks; the survivor walk demotes the typed operand to general so the existing
// isa.OpCopy reflect path handles the general/general copy.
//
// Takes expression (*ast.CallExpr) which is the call AST node.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
//
// Returns true when the call matches the accepted shape.
func copyAcceptedTypedSliceShape(expression *ast.CallExpr, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind) bool {
	if expression == nil {
		return false
	}
	calleeIdent, ok := expression.Fun.(*ast.Ident)
	if !ok || calleeIdent.Name != "copy" {
		return false
	}
	if len(expression.Args) != 2 {
		return false
	}
	destinationIdent, ok := expression.Args[0].(*ast.Ident)
	if !ok || !candidates[destinationIdent.Name] {
		return false
	}
	sourceIdent, ok := expression.Args[1].(*ast.Ident)
	if !ok || !candidates[sourceIdent.Name] {
		return false
	}
	destinationKind := candidateKinds[destinationIdent.Name]
	sourceKind := candidateKinds[sourceIdent.Name]
	if !isa.IsTypedSliceKind(destinationKind) || destinationKind != sourceKind {
		return false
	}
	return true
}

// appendAcceptedTypedSliceShape reports whether the call matches the typed append shape:
// `append(candidate, element)` with no spread and a single element.
//
// Takes expression (*ast.CallExpr).
// Takes candidates (map[string]bool) which is the candidate set.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
//
// Returns true when the call matches the accepted shape.
func appendAcceptedTypedSliceShape(expression *ast.CallExpr, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind) bool {
	if expression == nil {
		return false
	}
	calleeIdent, ok := expression.Fun.(*ast.Ident)
	if !ok || calleeIdent.Name != "append" {
		return false
	}
	if len(expression.Args) != 2 {
		return false
	}
	if expression.Ellipsis != token.NoPos {
		return false
	}
	sliceIdent, ok := expression.Args[0].(*ast.Ident)
	if !ok {
		return false
	}
	if !candidates[sliceIdent.Name] {
		return false
	}
	if !isa.IsTypedSliceKind(candidateKinds[sliceIdent.Name]) {
		return false
	}
	return true
}

// tryResolveCalleeParameterKinds looks up the callee's parameterKinds.
//
// Falls back to nil when the callee cannot be resolved at compile time, including
// indirect/dynamic calls, interface method dispatch, native calls, and generic functions
// with type parameters whose specialised body is not known here. Conservative nil maps to
// demotion.
//
// Takes typeContext (*Compiler) which provides go/types lookups.
// Takes expression (*ast.CallExpr) which is the call being inspected.
//
// Returns []isa.RegisterKind which holds the callee's parameterKinds, or nil when the
// callee cannot be resolved.
func tryResolveCalleeParameterKinds(typeContext *Context, expression *ast.CallExpr) []isa.RegisterKind {
	if typeContext == nil || typeContext.FunctionTable == nil || typeContext.RootFunction == nil {
		return nil
	}
	identifier, ok := expression.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	functionIndex, ok := typeContext.FunctionTable[identifier.Name]
	if !ok {
		return nil
	}
	if int(functionIndex) >= len(typeContext.RootFunction.Functions) {
		return nil
	}
	callee := typeContext.RootFunction.Functions[functionIndex]
	if callee == nil {
		return nil
	}
	return callee.ParameterKinds
}

// callSlotAcceptsTypedSlice reports whether the callee's parameter slot at argumentIndex
// accepts the candidate's typed bank. The argumentIndex is the position within the call's
// args list, adjusted for a receiver slot when the callee has one.
//
// Takes parameterKinds ([]isa.RegisterKind) which is the callee's parameterKinds slice.
// Takes argumentIndex (int) which is the position of the candidate argument in
// expression.Args.
// Takes candidateKind (isa.RegisterKind) which is the candidate's typed bank.
//
// Returns true when the parameter slot's kind matches candidateKind.
func callSlotAcceptsTypedSlice(parameterKinds []isa.RegisterKind, argumentIndex int, candidateKind isa.RegisterKind) bool {
	if argumentIndex < 0 || argumentIndex >= len(parameterKinds) {
		return false
	}
	return parameterKinds[argumentIndex] == candidateKind
}

// callIsAllowedTypedSliceConsumer reports whether expression is a call shape safe to pass
// a typed-slice candidate through without forcing it onto the general bank.
//
// Allowed shapes are the allowlisted builtins (see typedSliceLocalsAllowedBuiltins) and
// single-argument `string(<bytes>)` conversions, which lower to
// isa.SubOpSliceByteToString reading slicesByte directly.
//
// Takes typeContext (*Compiler) which holds type information for the expression.
// Takes expression (*ast.CallExpr) which is the call AST node under inspection.
//
// Returns true when the call matches an allowed shape; false otherwise.
func callIsAllowedTypedSliceConsumer(typeContext *Context, expression *ast.CallExpr) bool {
	identifier, ok := expression.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	typeObject, ok := typeContext.Info.Uses[identifier]
	if !ok {
		return false
	}
	if _, isBuiltin := typeObject.(*types.Builtin); isBuiltin && typedSliceLocalsAllowedBuiltins[identifier.Name] {
		return true
	}
	if typeName, ok := typeObject.(*types.TypeName); ok && typeName.Name() == "string" && len(expression.Args) == 1 {
		return true
	}
	return false
}

// disqualifyAssignStmt disqualifies candidates touched by an assignment.
//
// Records candidate names appearing on the right-hand side of any assignment (other than
// the declaration that introduced them) or on the left-hand side of a non-indexed
// assignment. Indexed writes `s[i] = v` are tolerated; whole-slice reassignment `s = ...`
// is not, because the new RHS may not be typed-slice-bank compatible.
//
// Takes statement (*ast.AssignStmt) which is the assignment being inspected.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes disqualified (map[string]bool) which collects disqualified names.
func disqualifyAssignStmt(statement *ast.AssignStmt, candidates, disqualified map[string]bool) {
	for _, leftSide := range statement.Lhs {
		identifier, ok := leftSide.(*ast.Ident)
		if !ok {
			continue
		}
		if statement.Tok == token.DEFINE {
			continue
		}
		if candidates[identifier.Name] {
			disqualified[identifier.Name] = true
		}
	}
	relaxRHSForContainerWrite := assignIsContainerWrite(statement)
	for _, rightSide := range statement.Rhs {
		identifier, ok := rightSide.(*ast.Ident)
		if !ok || !candidates[identifier.Name] {
			continue
		}
		if relaxRHSForContainerWrite {
			continue
		}
		disqualified[identifier.Name] = true
	}
}

// assignIsContainerWrite reports whether the statement is a single-slot container set
// (`outer[i] = v` or `m[k] = v`). The Emit path boxes the RHS through
// isa.OpPackInterface, so the candidate can stay on the typed bank.
//
// Takes statement (*ast.AssignStmt) which is the assignment being inspected.
//
// Returns true when the statement is a container-write shape.
func assignIsContainerWrite(statement *ast.AssignStmt) bool {
	if statement.Tok != token.ASSIGN {
		return false
	}
	if len(statement.Lhs) != 1 || len(statement.Rhs) != 1 {
		return false
	}
	_, isIndex := statement.Lhs[0].(*ast.IndexExpr)
	return isIndex
}

// disqualifyTypeSwitch disqualifies a candidate used as a type-switch subject.
//
// Type switches always force the value through a reflect.Value boundary so any candidate
// routed through the typed bank must fall back to the general bank.
//
// Takes statement (*ast.TypeSwitchStmt) which is the type switch statement.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes disqualified (map[string]bool) which collects disqualified names.
func disqualifyTypeSwitch(statement *ast.TypeSwitchStmt, candidates, disqualified map[string]bool) {
	if assignment, ok := statement.Assign.(*ast.AssignStmt); ok {
		for _, rightSide := range assignment.Rhs {
			disqualifyTypeAssertSubject(rightSide, candidates, disqualified)
		}
		return
	}
	if expressionStmt, ok := statement.Assign.(*ast.ExprStmt); ok {
		disqualifyTypeAssertSubject(expressionStmt.X, candidates, disqualified)
	}
}

// disqualifyTypeAssertSubject marks the subject identifier of an `x.(T)` assertion as
// disqualified when it names a candidate.
//
// Takes expression (ast.Expr) which is the candidate type-assertion expression.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates disqualified names.
func disqualifyTypeAssertSubject(expression ast.Expr, candidates, disqualified map[string]bool) {
	assertion, ok := expression.(*ast.TypeAssertExpr)
	if !ok {
		return
	}
	disqualifyIdentRef(assertion.X, candidates, disqualified)
}

// disqualifyClosureCapture disqualifies non-typed-slice candidates captured by a closure.
// Typed-slice candidates survive because the upvalueCell snapshot stores a copy of the
// slice header while the underlying array stays shared with the declaring frame.
//
// Takes literal (*ast.FuncLit) which is the closure being inspected.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes candidateKinds (map[string]isa.RegisterKind) which records each candidate's
// typed-slice bank.
// Takes disqualified (map[string]bool) which collects disqualified names.
func disqualifyClosureCapture(literal *ast.FuncLit, candidates map[string]bool, candidateKinds map[string]isa.RegisterKind, disqualified map[string]bool) {
	if literal == nil || literal.Body == nil {
		return
	}
	ast.Inspect(literal.Body, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if !candidates[identifier.Name] {
			return true
		}
		if isa.IsTypedSliceKind(candidateKinds[identifier.Name]) {
			return true
		}
		disqualified[identifier.Name] = true
		return true
	})
}

// disqualifyCompositeLit records candidate names appearing inside composite literals
// (slice, array, map, struct). Composite literals store their element values via
// reflect.Value, so a typed-bank candidate inserted into one would need to be boxed; the
// classifier disqualifies rather than Emit a box.
//
// Takes literal (*ast.CompositeLit) which is the composite literal being inspected.
// Takes candidates (map[string]bool) which is the set of names being considered.
// Takes disqualified (map[string]bool) which collects disqualified names.
func disqualifyCompositeLit(literal *ast.CompositeLit, candidates, disqualified map[string]bool) {
	for _, element := range literal.Elts {
		switch entry := element.(type) {
		case *ast.Ident:
			if candidates[entry.Name] {
				disqualified[entry.Name] = true
			}
		case *ast.KeyValueExpr:
			if identifier, ok := entry.Value.(*ast.Ident); ok && candidates[identifier.Name] {
				disqualified[identifier.Name] = true
			}
		}
	}
}

// collectNarrowLoopRanges scans body for loop bodies and records their position spans,
// and marks any narrow-element candidate ranged over directly as both used and amortised
// (a range walks the whole slice, so the widening copy pays off).
//
// Takes typeContext (*Compiler) which provides go/types info for element-width checks.
// Takes body (*ast.BlockStmt) which is the function body to scan.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes amortised (map[string]bool) which collects names whose widening copy amortises.
// Takes narrowUse (map[string]bool) which collects narrow-element candidate uses.
//
// Returns the position spans of every loop body, for later in-loop membership tests.
func collectNarrowLoopRanges(typeContext *Context, body *ast.BlockStmt, candidates, amortised, narrowUse map[string]bool) [][2]token.Pos {
	const loopRangeHint = 8
	loopRanges := make([][2]token.Pos, 0, loopRangeHint)
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.ForStmt:
			if n.Body != nil {
				loopRanges = append(loopRanges, [2]token.Pos{n.Body.Pos(), n.Body.End()})
			}
		case *ast.RangeStmt:
			if n.Body != nil {
				loopRanges = append(loopRanges, [2]token.Pos{n.Body.Pos(), n.Body.End()})
			}
			if name, narrow := candidateHasNarrowElements(typeContext, n.X, candidates); narrow && name != "" {
				amortised[name] = true
				narrowUse[name] = true
			}
		}
		return true
	})
	return loopRanges
}

// positionWithinLoop reports whether pos falls inside any of the recorded loop body
// spans.
//
// Takes pos (token.Pos) which is the position being tested.
// Takes loopRanges ([][2]token.Pos) which are the loop body spans to test against.
//
// Returns true when pos falls inside at least one loop body span.
func positionWithinLoop(pos token.Pos, loopRanges [][2]token.Pos) bool {
	for _, r := range loopRanges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// recordIndexedNarrowUses scans body for index expressions on narrow-element candidates,
// marking each as used and, when the index sits inside a loop body, as amortised.
//
// Takes typeContext (*Compiler) which provides go/types info for element-width checks.
// Takes body (*ast.BlockStmt) which is the function body to scan.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes loopRanges ([][2]token.Pos) which are the loop body spans for in-loop tests.
// Takes amortised (map[string]bool) which collects names whose widening copy amortises.
// Takes narrowUse (map[string]bool) which collects narrow-element candidate uses.
func recordIndexedNarrowUses(typeContext *Context, body *ast.BlockStmt, candidates map[string]bool, loopRanges [][2]token.Pos, amortised, narrowUse map[string]bool) {
	ast.Inspect(body, func(node ast.Node) bool {
		indexExpression, ok := node.(*ast.IndexExpr)
		if !ok {
			return true
		}
		name, narrow := candidateHasNarrowElements(typeContext, indexExpression.X, candidates)
		if !narrow || name == "" {
			return true
		}
		narrowUse[name] = true
		if positionWithinLoop(indexExpression.Pos(), loopRanges) {
			amortised[name] = true
		}
		return true
	})
}

// demoteUnamortisedNarrowCandidates demotes narrow-element candidates the body never
// consumes inside a loop, because the O(n) widening copy only pays off when the callee
// walks the slice.
//
// Takes typeContext (*Compiler) which provides go/types info for element-width checks.
// Takes body (*ast.BlockStmt) which is the function body to scan.
// Takes candidates (map[string]bool) which is the candidate set.
// Takes disqualified (map[string]bool) which accumulates demoted names.
func demoteUnamortisedNarrowCandidates(typeContext *Context, body *ast.BlockStmt, candidates, disqualified map[string]bool) {
	amortised := make(map[string]bool)
	narrowUse := make(map[string]bool)
	loopRanges := collectNarrowLoopRanges(typeContext, body, candidates, amortised, narrowUse)
	recordIndexedNarrowUses(typeContext, body, candidates, loopRanges, amortised, narrowUse)
	for name := range narrowUse {
		if !amortised[name] {
			disqualified[name] = true
		}
	}
}
