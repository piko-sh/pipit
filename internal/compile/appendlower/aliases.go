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

package appendlower

import (
	"go/ast"
	"go/token"
	"go/types"

	"pipit.sh/pipit/internal/compile/sliceshare"
)

const (
	// blankIdentifier is the Go blank identifier, which never names a slice worth tracking.
	blankIdentifier = "_"

	// nilIdentifier is the predeclared nil, which names no variable.
	nilIdentifier = "nil"
)

// Body is the code one alias analysis covers.
type Body struct {
	// Receiver is the method's receiver list; nil for a function.
	Receiver *ast.FieldList

	// Parameters is the parameter list; nil for none.
	Parameters *ast.FieldList

	// Block is the body itself; nil for none.
	Block *ast.BlockStmt

	// Entry names further variables whose header is shared on entry, such as the key and
	// value of a range-over-func body.
	Entry []ast.Expr

	// Repeats makes sharing events count from entry because an enclosing loop repeats
	// the body.
	Repeats bool
}

// Aliases records when each slice variable's header may become shared, preventing
// unsafe in-place appends. A nil *Aliases treats every variable as shared.
//
// Sharing events in loops count from the outermost loop's start because later
// iterations repeat them. Events in function literals or bodies using goto count from
// entry.
type Aliases struct {
	// info resolves identifiers to the variables they denote.
	info *types.Info

	// objects maps each shared variable to the position from which it is shared; token.NoPos
	// means from entry.
	objects map[types.Object]token.Pos

	// names holds the same for identifiers that resolve to no object, keyed by name.
	names map[string]token.Pos
}

// CollectAliases runs the alias analysis over body.
//
// Takes info (*types.Info) which resolves identifiers, builtins and conversions; nil
// makes every variable shared.
// Takes body (Body) which is the code to analyse.
//
// Returns *Aliases which answers Shared, or nil when info is nil.
func CollectAliases(info *types.Info, body Body) *Aliases {
	if info == nil {
		return nil
	}
	aliases := &Aliases{
		info:    info,
		objects: make(map[types.Object]token.Pos),
		names:   make(map[string]token.Pos),
	}
	aliases.recordFields(body.Receiver)
	aliases.recordFields(body.Parameters)
	for _, entry := range body.Entry {
		aliases.record(entry, token.NoPos)
	}
	if body.Block == nil {
		return aliases
	}
	walker := &aliasWalker{
		aliases:   aliases,
		fromEntry: body.Repeats || containsGoto(body.Block),
		stack:     nil,
	}
	ast.Inspect(body.Block, walker.visit)
	return aliases
}

// Shared reports whether an append to target that ends at until may run while another
// handle shares target's header.
//
// Takes target (*ast.Ident) which is the appended-to variable.
// Takes until (token.Pos) which is the end of the append expression; events inside it run
// before the append does.
//
// Returns bool which is true when the append must not grow the header in place.
func (a *Aliases) Shared(target *ast.Ident, until token.Pos) bool {
	if a == nil {
		return true
	}
	if from, found := a.names[target.Name]; found && from < until {
		return true
	}
	object := a.info.ObjectOf(target)
	if object == nil {
		return false
	}
	from, found := a.objects[object]
	return found && from < until
}

// recordFields records every named receiver or parameter as shared from entry.
//
// Takes fields (*ast.FieldList) which is a receiver or parameter list; may be nil.
func (a *Aliases) recordFields(fields *ast.FieldList) {
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			a.record(name, token.NoPos)
		}
	}
}

// record marks the variable expression names, after parentheses and sharing conversions,
// as shared from position, keeping an earlier position already recorded.
//
// Takes expression (ast.Expr) which is the candidate; anything but a named variable is
// ignored.
// Takes position (token.Pos) which is the event's effective position.
func (a *Aliases) record(expression ast.Expr, position token.Pos) {
	if expression == nil {
		return
	}
	ident, ok := sliceshare.StripSharing(a.info, expression).(*ast.Ident)
	if !ok || ident.Name == "" || ident.Name == blankIdentifier || isNil(a.info, ident) {
		return
	}
	object := a.info.ObjectOf(ident)
	if object == nil {
		if _, declared := a.info.Defs[ident]; declared {
			return
		}
		a.names[ident.Name] = earlier(a.names, ident.Name, position)
		return
	}
	if _, isVariable := object.(*types.Var); !isVariable {
		return
	}
	a.objects[object] = earlier(a.objects, object, position)
}

// recordObject marks object as shared from position, keeping an earlier position already
// recorded.
//
// Takes object (types.Object) which is the variable; nil is ignored.
// Takes position (token.Pos) which is the event's effective position.
func (a *Aliases) recordObject(object types.Object, position token.Pos) {
	if object == nil {
		return
	}
	a.objects[object] = earlier(a.objects, object, position)
}

// earlier returns the smaller of position and the position already recorded for key.
//
// Takes recorded (map[K]token.Pos) which holds the positions so far.
// Takes key (K) which is the variable.
// Takes position (token.Pos) which is the new event's position.
//
// Returns token.Pos which is the earliest position for key.
func earlier[K comparable](recorded map[K]token.Pos, key K, position token.Pos) token.Pos {
	if previous, found := recorded[key]; found && previous < position {
		return previous
	}
	return position
}

// aliasWalker walks one body, recording each event at its effective position.
type aliasWalker struct {
	// aliases receives the events.
	aliases *Aliases

	// stack holds the nodes from the body down to the node being visited.
	stack []ast.Node

	// fromEntry makes every event count from entry.
	fromEntry bool
}

// visit maintains the node stack and records sharing events during ast.Inspect.
//
// Takes node (ast.Node) which is the node entered, or nil when the last one is left.
//
// Returns bool which is always true, so every node is entered and left.
func (w *aliasWalker) visit(node ast.Node) bool {
	if node == nil {
		w.stack = w.stack[:len(w.stack)-1]
		return true
	}
	w.stack = append(w.stack, node)
	w.recordEvents(node)
	return true
}

// recordEvents records the sharing events one node introduces.
//
// Takes node (ast.Node) which is the node under inspection.
func (w *aliasWalker) recordEvents(node ast.Node) {
	info := w.aliases.info
	switch typed := node.(type) {
	case *ast.AssignStmt:
		position := w.position(typed)
		for _, value := range typed.Rhs {
			w.aliases.record(value, position)
		}
		w.recordTargets(typed.Lhs, typed.Rhs, position)
	case *ast.ValueSpec:
		position := w.position(typed)
		for _, value := range typed.Values {
			w.aliases.record(value, position)
		}
		if len(typed.Values) > 0 {
			w.recordTargets(identExpressions(typed.Names), typed.Values, position)
		}
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			w.aliases.record(typed.X, w.position(typed))
		}
	case *ast.RangeStmt:
		position := w.position(typed)
		w.aliases.record(typed.Key, position)
		w.aliases.record(typed.Value, position)
	case *ast.SendStmt:
		w.aliases.record(typed.Value, w.position(typed))
	case *ast.SelectorExpr:
		if selection, found := info.Selections[typed]; found && selection.Kind() == types.MethodVal {
			w.aliases.record(typed.X, w.position(typed))
		}
	case *ast.CallExpr:
		w.recordCallArguments(typed)
	case *ast.TypeSwitchStmt:
		w.recordTypeSwitchBindings(typed)
	}
}

// recordTargets records every assigned variable whose new value may already have another
// handle. When the counts differ the values come from one multi-valued expression, which
// may hand on anything.
//
// Takes targets ([]ast.Expr) which are the assigned variables.
// Takes values ([]ast.Expr) which are the assigned values.
// Takes position (token.Pos) which is the event's effective position.
func (w *aliasWalker) recordTargets(targets, values []ast.Expr, position token.Pos) {
	for index, target := range targets {
		if len(targets) != len(values) || !buildsFreshHeader(w.aliases.info, values[index]) {
			w.aliases.record(target, position)
		}
	}
}

// recordCallArguments records every argument of a call to a function. A builtin keeps
// nothing (append copies the headers it is given, and its result is a new header), and a
// conversion is not a call; a sharing conversion is seen through where its result is
// used.
//
// Takes call (*ast.CallExpr) which is the call.
func (w *aliasWalker) recordCallArguments(call *ast.CallExpr) {
	info := w.aliases.info
	if _, builtin := sliceshare.BuiltinName(info, call); builtin {
		return
	}
	if typeAndValue, found := info.Types[call.Fun]; found && typeAndValue.IsType() {
		return
	}
	position := w.position(call)
	for _, argument := range call.Args {
		w.aliases.record(argument, position)
	}
}

// recordTypeSwitchBindings records each clause variable as sharing the switched value.
// The variables are found through their uses because the compiler keeps no implicit
// objects for them.
//
// Takes statement (*ast.TypeSwitchStmt) which is the type switch.
func (w *aliasWalker) recordTypeSwitchBindings(statement *ast.TypeSwitchStmt) {
	assignment, binds := statement.Assign.(*ast.AssignStmt)
	if !binds || len(assignment.Lhs) != 1 {
		return
	}
	binding, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok {
		return
	}
	position := w.position(statement)
	ast.Inspect(statement.Body, func(node ast.Node) bool {
		ident, isIdent := node.(*ast.Ident)
		if !isIdent || ident.Name != binding.Name {
			return true
		}
		if object := w.aliases.info.Uses[ident]; object != nil && object.Pos() == binding.Pos() {
			w.aliases.recordObject(object, position)
		}
		return true
	})
}

// position returns the position from which an event at node counts: entry inside a
// function literal or when the whole body counts from entry, the start of the outermost
// enclosing loop inside a loop, and node's own position otherwise.
//
// Takes node (ast.Node) which is the event's node.
//
// Returns token.Pos which is the effective position.
func (w *aliasWalker) position(node ast.Node) token.Pos {
	if w.fromEntry {
		return token.NoPos
	}
	for _, enclosing := range w.stack {
		if _, literal := enclosing.(*ast.FuncLit); literal {
			return token.NoPos
		}
	}
	for _, enclosing := range w.stack {
		switch enclosing.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return enclosing.Pos()
		}
	}
	return node.Pos()
}

// containsGoto reports whether block uses goto, which can run an event again after a
// later append and so defeats the positional rule.
//
// Takes block (*ast.BlockStmt) which is the body.
//
// Returns bool which is true when a goto appears anywhere in block.
func containsGoto(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		if branch, ok := node.(*ast.BranchStmt); ok && branch.Tok == token.GOTO {
			found = true
		}
		return !found
	})
	return found
}

// identExpressions returns names as expressions.
//
// Takes names ([]*ast.Ident) which are the declared names.
//
// Returns []ast.Expr which holds the same identifiers.
func identExpressions(names []*ast.Ident) []ast.Expr {
	expressions := make([]ast.Expr, len(names))
	for index, name := range names {
		expressions[index] = name
	}
	return expressions
}

// buildsFreshHeader reports whether expression yields a slice header that no other handle
// can reach yet: a composite literal, nil, a slice expression, a conversion that copies,
// or a call to make or append, after any parentheses and sharing conversions.
//
// Takes info (*types.Info) which identifies builtins, conversions and nil.
// Takes expression (ast.Expr) which is the assigned value.
//
// Returns bool which is true for a fresh header.
func buildsFreshHeader(info *types.Info, expression ast.Expr) bool {
	switch typed := sliceshare.StripSharing(info, expression).(type) {
	case *ast.CompositeLit, *ast.SliceExpr:
		return true
	case *ast.Ident:
		return isNil(info, typed)
	case *ast.CallExpr:
		if typeAndValue, found := info.Types[typed.Fun]; found && typeAndValue.IsType() {
			return true
		}
		name, builtin := sliceshare.BuiltinName(info, typed)
		return builtin && (name == "make" || name == "append")
	}
	return false
}

// isNil reports whether ident is the predeclared nil.
//
// Takes info (*types.Info) which resolves ident; the name decides when it does not.
// Takes ident (*ast.Ident) which is the candidate.
//
// Returns bool which is true for nil.
func isNil(info *types.Info, ident *ast.Ident) bool {
	if object, found := info.Uses[ident]; found {
		_, isNilObject := object.(*types.Nil)
		return isNilObject
	}
	return ident.Name == nilIdentifier
}
