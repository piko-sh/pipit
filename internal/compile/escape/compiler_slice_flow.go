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
)

// CallFlow is what a call's possible callees do with its operands, as the slice-flow
// analysis sees it.
type CallFlow struct {
	// Arguments flags, in call order, the arguments a callee may store beyond the call.
	Arguments []bool

	// Receiver reports that a method value's callee stores its receiver.
	Receiver bool

	// PacksVariadic reports that the compiled code packs the call's variadic arguments into
	// a slice itself, which stores them.
	PacksVariadic bool
}

// CallLookup resolves a call to what its possible callees do with its operands.
type CallLookup func(call *ast.CallExpr) CallFlow

// extent is the source range of a function, so a variable declared outside it is storage
// that outlives a call of it.
type extent struct {
	// from is the position of the function's func keyword.
	from token.Pos

	// to is the position just past the function's closing brace.
	to token.Pos
}

// contains reports whether a declaration position lies inside the function.
//
// Takes position (token.Pos) which is where a variable is declared.
//
// Returns bool which is true for a parameter, result or local of the function.
func (e extent) contains(position token.Pos) bool {
	return position >= e.from && position < e.to
}

// flowWalker gathers the flowing names of one function body.
type flowWalker struct {
	// info resolves callees, conversions and variables.
	info *types.Info

	// lookup resolves what a call's callees store, or is nil.
	lookup CallLookup

	// flowing is the set of names found to flow.
	flowing map[string]bool

	// aliases maps each local to the names whose backing it shares.
	aliases map[string][]string

	// path holds the nodes from the body down to the one being visited.
	path []ast.Node

	// extents holds the analysed function's extent and then one per enclosing function
	// literal, innermost last.
	extents []extent
}

// CollectFlowingSliceNames names the identifiers in body whose value is stored somewhere
// that outlives the variable's own register.
//
// Takes info (*types.Info) which resolves callees, builtins and variables.
// Takes signature (*ast.FuncType) which is the function's signature, or nil to bound the
// function by its body.
// Takes body (*ast.BlockStmt) which is the function body to inspect.
// Takes lookup (CallLookup) which resolves callees, or nil when no call stores anything.
//
// Returns the set of flowing names, or nil when there are none.
func CollectFlowingSliceNames(info *types.Info, signature *ast.FuncType, body *ast.BlockStmt, lookup CallLookup) map[string]bool {
	if body == nil {
		return nil
	}
	function := extent{from: body.Pos(), to: body.End()}
	if signature != nil {
		function.from = signature.Pos()
	}
	walker := &flowWalker{
		info:    info,
		lookup:  lookup,
		flowing: make(map[string]bool),
		aliases: make(map[string][]string),
		path:    nil,
		extents: []extent{function},
	}
	ast.Inspect(body, walker.visit)
	propagateThroughAliases(walker.flowing, walker.aliases)
	if len(walker.flowing) == 0 {
		return nil
	}
	return walker.flowing
}

// visit records what one node stores, and tracks the function literals it is inside.
//
// Takes node (ast.Node) which is the node entered, or nil when leaving one.
//
// Returns bool which is always true, to visit every node.
func (w *flowWalker) visit(node ast.Node) bool {
	if node == nil {
		w.leave()
		return true
	}
	w.path = append(w.path, node)
	switch statement := node.(type) {
	case *ast.FuncLit:
		w.extents = append(w.extents, extent{from: statement.Pos(), to: statement.End()})
	case *ast.CompositeLit:
		markCompositeElements(statement, w.mark)
	case *ast.AssignStmt:
		w.markStoredValues(statement)
		if statement.Tok == token.ASSIGN || statement.Tok == token.DEFINE {
			recordAliases(w.info, statement.Lhs, statement.Rhs, w.aliases)
		}
	case *ast.ValueSpec:
		recordAliases(w.info, identifierExpressions(statement.Names), statement.Values, w.aliases)
	case *ast.SendStmt:
		w.mark(statement.Value)
	case *ast.ReturnStmt:
		markEach(statement.Results, w.mark)
	case *ast.GoStmt:
		markEach(statement.Call.Args, w.mark)
	case *ast.DeferStmt:
		markEach(statement.Call.Args, w.mark)
	case *ast.CallExpr:
		markFlowingArguments(w.info, statement, w.lookup, w.mark)
	}
	return true
}

// leave pops the node being left, and the extent of a function literal it closes.
func (w *flowWalker) leave() {
	top := w.path[len(w.path)-1]
	w.path = w.path[:len(w.path)-1]
	if _, isLiteral := top.(*ast.FuncLit); isLiteral {
		w.extents = w.extents[:len(w.extents)-1]
	}
}

// mark records the name a flowing expression's slice header comes from, as
// sliceSourceName finds it.
//
// Takes expression (ast.Expr) which is the flowing expression.
func (w *flowWalker) mark(expression ast.Expr) {
	if name, found := sliceSourceName(w.info, expression); found {
		w.flowing[name] = true
	}
}

// markStoredValues marks the values an assignment stores into a field, an element, a
// pointee or a variable declared outside the innermost function making the assignment.
//
// Takes statement (*ast.AssignStmt) which is the assignment.
func (w *flowWalker) markStoredValues(statement *ast.AssignStmt) {
	if statement.Tok != token.ASSIGN || len(statement.Lhs) != len(statement.Rhs) {
		return
	}
	innermost := w.extents[len(w.extents)-1]
	for i, target := range statement.Lhs {
		if isStoreTarget(w.info, target, innermost) {
			w.mark(statement.Rhs[i])
		}
	}
}

// isStoreTarget reports whether an assignment target is storage beyond a local register.
//
// Takes info (*types.Info) which resolves identifiers.
// Takes target (ast.Expr) which is the left-hand side.
// Takes function (extent) which bounds the innermost function making the assignment.
//
// Returns bool which is true for a field, element or pointee, or for a package or
// captured variable.
func isStoreTarget(info *types.Info, target ast.Expr, function extent) bool {
	switch lhs := ast.Unparen(target).(type) {
	case *ast.IndexExpr, *ast.SelectorExpr, *ast.StarExpr:
		return true
	case *ast.Ident:
		variable, ok := info.Uses[lhs].(*types.Var)
		return ok && !function.contains(variable.Pos())
	default:
		return false
	}
}

// recordAliases records, for each local a pair of assignment operands binds to another
// name's slice header, that the local shares that name's backing.
//
// Takes info (*types.Info) which recognises conversions.
// Takes targets ([]ast.Expr) which are the assigned operands.
// Takes values ([]ast.Expr) which are the values, one per target.
// Takes aliases (map[string][]string) which maps each local to the names it aliases.
func recordAliases(info *types.Info, targets, values []ast.Expr, aliases map[string][]string) {
	if len(targets) != len(values) {
		return
	}
	for i, target := range targets {
		local, isLocal := ast.Unparen(target).(*ast.Ident)
		source, found := sliceSourceName(info, values[i])
		if isLocal && found && local.Name != "_" && local.Name != source {
			aliases[local.Name] = append(aliases[local.Name], source)
		}
	}
}

// identifierExpressions widens declared names to expressions for recordAliases.
//
// Takes names ([]*ast.Ident) which are a value spec's names.
//
// Returns []ast.Expr which holds the same identifiers.
func identifierExpressions(names []*ast.Ident) []ast.Expr {
	expressions := make([]ast.Expr, len(names))
	for i, name := range names {
		expressions[i] = name
	}
	return expressions
}

// propagateThroughAliases marks every name a flowing local aliases, transitively, since
// storing the local stores their shared backing.
//
// Takes flowing (map[string]bool) which is extended in place.
// Takes aliases (map[string][]string) which maps each local to the names it aliases.
func propagateThroughAliases(flowing map[string]bool, aliases map[string][]string) {
	pending := make([]string, 0, len(flowing))
	for name := range flowing {
		pending = append(pending, name)
	}
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, source := range aliases[name] {
			if !flowing[source] {
				flowing[source] = true
				pending = append(pending, source)
			}
		}
	}
}

// markCompositeElements marks every element value of a composite literal.
//
// Takes literal (*ast.CompositeLit) which is the literal.
// Takes mark (func(ast.Expr)) which records a flowing expression.
func markCompositeElements(literal *ast.CompositeLit, mark func(ast.Expr)) {
	for _, element := range literal.Elts {
		if pair, ok := element.(*ast.KeyValueExpr); ok {
			mark(pair.Value)
			continue
		}
		mark(element)
	}
}

// markFlowingArguments marks the arguments a call stores: the elements append adds, the
// arguments the compiled code packs into a declared callee's variadic parameter, and the
// arguments and receiver a possible callee stores.
//
// Takes info (*types.Info) which resolves builtins and types the callee.
// Takes call (*ast.CallExpr) which is the call.
// Takes lookup (CallLookup) which resolves callees, or nil.
// Takes mark (func(ast.Expr)) which records a flowing expression.
func markFlowingArguments(info *types.Info, call *ast.CallExpr, lookup CallLookup, mark func(ast.Expr)) {
	if identifier, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if _, builtin := info.Uses[identifier].(*types.Builtin); builtin {
			if identifier.Name == "append" && !call.Ellipsis.IsValid() && len(call.Args) > 1 {
				markEach(call.Args[1:], mark)
			}
			return
		}
	}
	if lookup == nil {
		return
	}
	flow := lookup(call)
	if selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && flow.Receiver {
		mark(selector.X)
	}
	if flow.PacksVariadic {
		markEach(packedArguments(info, call), mark)
	}
	for i, argument := range call.Args {
		if i < len(flow.Arguments) && flow.Arguments[i] {
			mark(argument)
		}
	}
}

// packedArguments returns the arguments a call packs into its callee's variadic
// parameter, or nil when the callee is not variadic or the call spreads a slice.
//
// Takes info (*types.Info) which types the callee.
// Takes call (*ast.CallExpr) which is the call.
//
// Returns []ast.Expr which holds the packed arguments.
func packedArguments(info *types.Info, call *ast.CallExpr) []ast.Expr {
	signature, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok || !signature.Variadic() || call.Ellipsis.IsValid() {
		return nil
	}
	return call.Args[min(signature.Params().Len()-1, len(call.Args)):]
}

// markEach marks every expression in a list.
//
// Takes expressions ([]ast.Expr) which are the flowing expressions.
// Takes mark (func(ast.Expr)) which records a flowing expression.
func markEach(expressions []ast.Expr, mark func(ast.Expr)) {
	for _, expression := range expressions {
		mark(expression)
	}
}

// sliceSourceName names the variable an expression's slice header comes from, looking
// through parentheses, re-slicing and the conversions that share the backing.
//
// Takes info (*types.Info) which recognises conversions.
// Takes expression (ast.Expr) which is the expression.
//
// Returns the name, and false when the header comes from anything but a variable.
func sliceSourceName(info *types.Info, expression ast.Expr) (string, bool) {
	for {
		switch value := ast.Unparen(expression).(type) {
		case *ast.Ident:
			return value.Name, true
		case *ast.SliceExpr:
			expression = value.X
		case *ast.CallExpr:
			target := info.Types[value.Fun]
			if len(value.Args) != 1 || !target.IsType() || conversionCopies(target.Type) {
				return "", false
			}
			expression = value.Args[0]
		default:
			return "", false
		}
	}
}

// conversionCopies reports whether converting a slice to a type copies its elements, as a
// string or array conversion does, rather than sharing the backing.
//
// Takes target (types.Type) which is the conversion's target type.
//
// Returns bool which is true when the result holds its own copy.
func conversionCopies(target types.Type) bool {
	switch target.Underlying().(type) {
	case *types.Basic, *types.Array:
		return true
	default:
		return false
	}
}

// fieldNames lists the names a field list declares in order, with "" for an unnamed one.
//
// Takes fields (*ast.FieldList) which is a receiver or parameter list, or nil.
//
// Returns []string which holds one entry per declared operand.
func fieldNames(fields *ast.FieldList) []string {
	if fields == nil {
		return nil
	}
	var names []string
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			names = append(names, "")
			continue
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}
