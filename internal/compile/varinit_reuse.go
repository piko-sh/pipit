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
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
)

// VarInitReusePlan is the result of AnalyseVarInitReuse.
type VarInitReusePlan struct {
	// Reason says why the file set is ineligible; empty when Eligible.
	Reason string

	// Slots are every global of the file set.
	Slots []program.GlobalVariableInfo

	// Eligible is true when the initialisers may be skipped while the run-time checks hold.
	Eligible bool
}

// pureNativePackages lists the native packages whose functions are deterministic and
// touch no host state, for the purity condition.
var pureNativePackages = map[string]bool{
	"strings": true, "strconv": true, "unicode": true, "unicode/utf8": true, "unicode/utf16": true,
	"math": true, "math/bits": true, "bytes": true, "sort": true, "slices": true, "maps": true,
	"cmp": true, "errors": true, "path": true, "encoding/hex": true,
}

// pureNativeFunctions lists further deterministic native functions, by "path.Name".
var pureNativeFunctions = map[string]bool{
	"fmt.Sprintf": true, "fmt.Sprint": true, "fmt.Sprintln": true, "fmt.Errorf": true,
	"regexp.MustCompile": true, "regexp.Compile": true, "regexp.QuoteMeta": true,
}

// impureBuiltins lists the builtins with an effect outside the initialiser.
var impureBuiltins = map[string]bool{"print": true, "println": true, "recover": true, "close": true}

// varInitAnalysis holds one file set's analysis state.
type varInitAnalysis struct {
	// info is the type information.
	info *types.Info

	// globals maps each package-level variable to its name.
	globals map[*types.Var]string

	// functions maps each declared function and method to its declaration.
	functions map[*types.Func]*ast.FuncDecl

	// parents maps each node to its parent.
	parents map[ast.Node]ast.Node

	// pureChecked holds the guest functions already checked for purity (or being checked).
	pureChecked map[*types.Func]bool

	// initialisers are the package-level initialiser expressions.
	initialisers []ast.Expr
}

// AnalyseVarInitReuse decides whether one file set's variable initialisers may be skipped
// on a later run (global reuse).
//
// Takes files ([]*ast.File) which are the file set's files.
// Takes info (*types.Info) which is their type information.
// Takes globals (map[string]program.GlobalVariableInfo) which are the compiled globals by
// name.
//
// Returns VarInitReusePlan which says whether the initialisers may be skipped.
func AnalyseVarInitReuse(files []*ast.File, info *types.Info, globals map[string]program.GlobalVariableInfo) VarInitReusePlan {
	plan := VarInitReusePlan{}
	for _, name := range slices.Sorted(mapKeys(globals)) {
		plan.Slots = append(plan.Slots, globals[name])
	}
	analysis := &varInitAnalysis{info: info, globals: map[*types.Var]string{}, functions: map[*types.Func]*ast.FuncDecl{},
		initialisers: nil,
		parents:      map[ast.Node]ast.Node{}, pureChecked: map[*types.Func]bool{}}
	if reason := analysis.run(files, globals); reason != "" {
		plan.Reason = reason
		return plan
	}
	plan.Eligible = true
	return plan
}

// mapKeys returns the keys of globals.
//
// Takes globals (map[string]program.GlobalVariableInfo) which is read.
//
// Returns func(func(string) bool) which yields the keys.
func mapKeys(globals map[string]program.GlobalVariableInfo) func(func(string) bool) {
	return func(yield func(string) bool) {
		for key := range globals {
			if !yield(key) {
				return
			}
		}
	}
}

// run performs the analysis.
//
// Takes files ([]*ast.File) which are analysed.
// Takes globals (map[string]program.GlobalVariableInfo) which are the compiled globals.
//
// Returns string which is the reason for ineligibility, or "".
func (a *varInitAnalysis) run(files []*ast.File, globals map[string]program.GlobalVariableInfo) string {
	if len(globals) == 0 {
		return "no package variables"
	}
	for name, slot := range globals {
		if slot.IsIndirect {
			return "the address of global " + name + " is taken"
		}
	}
	for _, file := range files {
		a.index(file)
	}
	for _, expression := range a.initialisers {
		if reason := a.pure(expression); reason != "" {
			return reason
		}
	}
	for _, file := range files {
		if reason := a.uses(file); reason != "" {
			return reason
		}
	}
	return ""
}

// index records the file's parents, globals, functions and initialisers.
//
// Takes file (*ast.File) which is indexed.
func (a *varInitAnalysis) index(file *ast.File) {
	a.indexParents(file)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if function, ok := a.info.Defs[typed.Name].(*types.Func); ok {
				a.functions[function] = typed
			}
		case *ast.GenDecl:
			if typed.Tok == token.VAR {
				a.indexVariables(typed)
			}
		default:
		}
	}
}

// indexParents records every node's parent in the file.
//
// Takes file (*ast.File) which is indexed.
func (a *varInitAnalysis) indexParents(file *ast.File) {
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			a.parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
}

// indexVariables records a var declaration's globals and initialisers.
//
// Takes declaration (*ast.GenDecl) which is the var declaration.
func (a *varInitAnalysis) indexVariables(declaration *ast.GenDecl) {
	for _, spec := range declaration.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, name := range valueSpec.Names {
			if variable, isVariable := a.info.Defs[name].(*types.Var); isVariable {
				a.globals[variable] = name.Name
			}
		}
		a.initialisers = append(a.initialisers, valueSpec.Values...)
	}
}

// pure checks an initialiser, or a function body it reaches, for purity.
//
// Takes root (ast.Node) which is checked, nested function literals included.
//
// Returns string which names the first impurity, or "".
func (a *varInitAnalysis) pure(root ast.Node) string {
	reason := ""
	ast.Inspect(root, func(node ast.Node) bool {
		if reason != "" || node == nil {
			return false
		}
		switch typed := node.(type) {
		case *ast.FuncLit:
			if _, called := a.parents[typed].(*ast.CallExpr); !called {
				return false
			}
		case *ast.GoStmt:
			reason = "a goroutine is started"
		case *ast.SendStmt:
			reason = "a channel send"
		case *ast.SelectStmt:
			reason = "a select"
		case *ast.UnaryExpr:
			if typed.Op == token.ARROW {
				reason = "a channel receive"
			}
		case *ast.SelectorExpr:
			reason = a.pureSelector(typed)
		case *ast.CallExpr:
			reason = a.pureCall(typed)
		default:
		}
		return reason == ""
	})
	return reason
}

// pureSelector rejects reads of a native package's variables.
//
// Takes selector (*ast.SelectorExpr) which is checked.
//
// Returns string which names the impurity, or "".
func (a *varInitAnalysis) pureSelector(selector *ast.SelectorExpr) string {
	identifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	if _, isPackage := a.info.Uses[identifier].(*types.PkgName); !isPackage {
		return ""
	}
	if variable, isVar := a.info.Uses[selector.Sel].(*types.Var); isVar {
		return "reads host variable " + variable.Pkg().Path() + "." + variable.Name()
	}
	return ""
}

// pureCall checks one call for purity, recursing into guest functions.
//
// Takes call (*ast.CallExpr) which is checked.
//
// Returns string which names the impurity, or "".
func (a *varInitAnalysis) pureCall(call *ast.CallExpr) string {
	function := ast.Unparen(call.Fun)
	if typeAndValue, ok := a.info.Types[function]; ok && typeAndValue.IsType() {
		return ""
	}
	if _, ok := function.(*ast.FuncLit); ok {
		return ""
	}
	if index, ok := function.(*ast.IndexExpr); ok {
		function = index.X
	}
	if index, ok := function.(*ast.IndexListExpr); ok {
		function = index.X
	}
	var object types.Object
	switch typed := function.(type) {
	case *ast.Ident:
		object = a.info.Uses[typed]
	case *ast.SelectorExpr:
		if selection, ok := a.info.Selections[typed]; ok {
			object = selection.Obj()
			if _, isInterface := selection.Recv().Underlying().(*types.Interface); isInterface {
				return "calls interface method " + typed.Sel.Name
			}
		} else {
			object = a.info.Uses[typed.Sel]
		}
	default:
		return "calls a computed function"
	}
	switch typed := object.(type) {
	case *types.Builtin:
		if impureBuiltins[typed.Name()] {
			return "calls builtin " + typed.Name()
		}
		return ""
	case *types.Func:
		return a.pureFunction(typed)
	default:
		return "calls through a function value"
	}
}

// pureFunction checks a called function for purity.
//
// Takes function (*types.Func) which is the callee.
//
// Returns string which names the impurity, or "".
func (a *varInitAnalysis) pureFunction(function *types.Func) string {
	function = function.Origin()
	if declaration, guest := a.functions[function]; guest {
		if a.pureChecked[function] {
			return ""
		}
		a.pureChecked[function] = true
		if declaration.Body == nil {
			return "calls bodiless " + function.Name()
		}
		return a.pure(declaration.Body)
	}
	if function.Pkg() == nil {
		return "calls " + function.Name()
	}
	path := function.Pkg().Path()
	if pureNativePackages[path] || pureNativeFunctions[path+"."+function.Name()] {
		return ""
	}
	return "calls native " + path + "." + function.Name()
}

// uses checks every use of a global outside the initialisers.
//
// Takes file (*ast.File) which is checked.
//
// Returns string which is the reason for ineligibility, or "".
func (a *varInitAnalysis) uses(file *ast.File) string {
	reason := ""
	ast.Inspect(file, func(node ast.Node) bool {
		if reason != "" {
			return false
		}
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		variable, ok := a.info.Uses[identifier].(*types.Var)
		if !ok {
			return true
		}
		name, isGlobal := a.globals[variable]
		if !isGlobal || !a.outsideInitialisers(identifier) {
			return true
		}
		if problem := a.use(identifier, variable); problem != "" {
			reason = fmt.Sprintf("global %s: %s", name, problem)
		}
		return true
	})
	return reason
}

// outsideInitialisers reports whether node runs inside a function declaration's body or a
// function literal, outside the variable initialisers.
//
// Takes node (ast.Node) which is a use.
//
// Returns bool which is true for code outside the initialisers.
func (a *varInitAnalysis) outsideInitialisers(node ast.Node) bool {
	for current := a.parents[node]; current != nil; current = a.parents[current] {
		switch current.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return true
		case *ast.GenDecl:
			if _, topLevel := a.parents[current].(*ast.File); topLevel {
				return false
			}
		default:
		}
	}
	return false
}

// use classifies one use of a global.
//
// Takes identifier (*ast.Ident) which is the use.
// Takes variable (*types.Var) which is the global.
//
// Returns string which says why the use disqualifies the file set, or "".
func (a *varInitAnalysis) use(identifier *ast.Ident, variable *types.Var) string {
	rootByValue := byValueType(variable.Type())
	var expression ast.Expr = identifier
	for {
		parent, projects := a.projectionOf(expression)
		if !projects {
			break
		}
		if selector, isSelector := parent.(*ast.SelectorExpr); isSelector {
			if reason, isMethod := a.methodUse(selector, rootByValue); isMethod {
				return reason
			}
		}
		expression = parent
	}
	return a.context(expression, rootByValue)
}

// projectionOf returns expression's parent when the parent projects it: parentheses, a
// selector, an index, a slice, a dereference, a type assertion or a conversion.
//
// Takes expression (ast.Expr) which is the global or a projection of it.
//
// Returns ast.Expr which is the parent.
// Returns bool which is false when the parent is not a projection of expression.
func (a *varInitAnalysis) projectionOf(expression ast.Expr) (ast.Expr, bool) {
	switch typedParent := a.parents[expression].(type) {
	case *ast.ParenExpr:
		return typedParent, true
	case *ast.SelectorExpr:
		return typedParent, typedParent.X == expression
	case *ast.IndexExpr:
		return typedParent, typedParent.X == expression
	case *ast.SliceExpr:
		return typedParent, typedParent.X == expression
	case *ast.StarExpr:
		return typedParent, typedParent.X == expression
	case *ast.TypeAssertExpr:
		return typedParent, typedParent.X == expression
	case *ast.CallExpr:
		typeAndValue, ok := a.info.Types[typedParent.Fun]
		return typedParent, ok && typeAndValue.IsType()
	default:
		return nil, false
	}
}

// methodUse classifies a selector on a global's projection that selects a method.
//
// Takes selector (*ast.SelectorExpr) which selects from the projection.
// Takes rootByValue (bool) which is true when the global's type is by value.
//
// Returns string which says why the use disqualifies the file set, or "".
// Returns bool which is false when the selector selects a field, not a method.
func (a *varInitAnalysis) methodUse(selector *ast.SelectorExpr, rootByValue bool) (string, bool) {
	selection, ok := a.info.Selections[selector]
	if !ok || selection.Kind() == types.FieldVal {
		return "", false
	}
	if rootByValue && !pointerReceiver(selection) && selection.Kind() == types.MethodVal && a.isCallee(selector) {
		return "", true
	}
	return "its method " + selector.Sel.Name + " is used", true
}

// isCallee reports whether selector is the function of a call.
//
// Takes selector (*ast.SelectorExpr) which is a method selection.
//
// Returns bool which is true when it is called directly.
func (a *varInitAnalysis) isCallee(selector *ast.SelectorExpr) bool {
	call, ok := a.parents[selector].(*ast.CallExpr)
	return ok && ast.Unparen(call.Fun) == selector
}

// pointerReceiver reports whether a method selection has a pointer receiver.
//
// Takes selection (*types.Selection) which is the method.
//
// Returns bool which is true for a pointer receiver.
func pointerReceiver(selection *types.Selection) bool {
	signature, ok := selection.Obj().Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return true
	}
	_, pointer := signature.Recv().Type().(*types.Pointer)
	return pointer
}

// context classifies the use of a global's projection by where it appears.
//
// Takes expression (ast.Expr) which is the global or a projection of it.
// Takes rootByValue (bool) which is true when the global's type is by value.
//
// Returns string which says why the use disqualifies the file set, or "".
func (a *varInitAnalysis) context(expression ast.Expr, rootByValue bool) string {
	expressionType := a.info.TypeOf(expression)
	byValue := expressionType != nil && byValueType(expressionType)
	parent := a.parents[expression]
	switch typedParent := parent.(type) {
	case *ast.AssignStmt:
		return assignContext(slices.Contains(typedParent.Lhs, expression), rootByValue, byValue)
	case *ast.IncDecStmt:
		return assignContext(true, rootByValue, byValue)
	case *ast.UnaryExpr:
		return unaryContext(typedParent.Op)
	case *ast.SendStmt:
		return "used in a send"
	case *ast.RangeStmt:
		return a.rangeContext(typedParent, expression, rootByValue, byValue)
	case *ast.CallExpr:
		return a.callContext(typedParent, expression, byValue)
	case *ast.BinaryExpr, *ast.ExprStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.CaseClause, *ast.ForStmt:
		return ""
	case *ast.IndexExpr:
		if typedParent.Index == expression {
			if byValue {
				return ""
			}
			return "used as an index"
		}
	default:
	}
	if byValue {
		return ""
	}
	return "escapes (" + strings.TrimPrefix(fmt.Sprintf("%T", parent), "*ast.") + ")"
}

// assignContext classifies a global's projection in an assignment or increment.
//
// Takes written (bool) which is true when the projection is assigned to.
// Takes rootByValue (bool) which is true when the global's type is by value.
// Takes byValue (bool) which is true when the projection's type is by value.
//
// Returns string which says why the use disqualifies the file set, or "".
func assignContext(written, rootByValue, byValue bool) string {
	switch {
	case written && rootByValue:
		return ""
	case written:
		return "written in place"
	case byValue:
		return ""
	default:
		return "assigned to another variable"
	}
}

// unaryContext classifies a global's projection as the operand of a unary operator.
//
// Takes operator (token.Token) which is the operator.
//
// Returns string which says why the use disqualifies the file set, or "".
func unaryContext(operator token.Token) string {
	switch operator {
	case token.AND:
		return "its address is taken"
	case token.ARROW:
		return "received from"
	default:
		return ""
	}
}

// rangeContext classifies a range over, or into, a global.
//
// Takes statement (*ast.RangeStmt) which is the range.
// Takes expression (ast.Expr) which is the global's projection.
// Takes rootByValue (bool) which is true when the global's root type has value semantics.
// Takes byValue (bool) which is true when the projected expression's type has value
// semantics.
//
// Returns string which says why the use disqualifies the file set, or "".
func (a *varInitAnalysis) rangeContext(statement *ast.RangeStmt, expression ast.Expr, rootByValue, byValue bool) string {
	if statement.X != expression {
		if rootByValue {
			return ""
		}
		return "written by a range"
	}
	if byValue {
		return ""
	}
	if _, isChannel := a.info.TypeOf(expression).Underlying().(*types.Chan); isChannel {
		return "received from by a range"
	}
	for _, variable := range []ast.Expr{statement.Key, statement.Value} {
		if variable == nil {
			continue
		}
		if identifier, ok := variable.(*ast.Ident); ok && identifier.Name == "_" {
			continue
		}
		variableType := a.info.TypeOf(variable)
		if variableType == nil || !byValueType(variableType) {
			return "ranged over into a reference-typed variable"
		}
	}
	return ""
}

// callContext classifies a global's projection used in a call.
//
// Takes call (*ast.CallExpr) which is the call.
// Takes expression (ast.Expr) which is the global's projection.
// Takes byValue (bool) which is true when the projection's type is by value.
//
// Returns string which says why the use disqualifies the file set, or "".
func (a *varInitAnalysis) callContext(call *ast.CallExpr, expression ast.Expr, byValue bool) string {
	if ast.Unparen(call.Fun) == expression {
		return ""
	}
	if byValue {
		return ""
	}
	switch a.parents[call].(type) {
	case *ast.GoStmt, *ast.DeferStmt:
		return "passed to a go or defer call"
	default:
	}
	function := ast.Unparen(call.Fun)
	var object types.Object
	switch typed := function.(type) {
	case *ast.Ident:
		object = a.info.Uses[typed]
	case *ast.SelectorExpr:
		if _, isSelection := a.info.Selections[typed]; isSelection {
			return "passed to a method"
		}
		object = a.info.Uses[typed.Sel]
	case *ast.IndexExpr:
		if _, instantiated := a.info.TypeOf(typed.X).(*types.Signature); instantiated {
			return "passed to a generic function"
		}
		return "passed to a function value"
	case *ast.IndexListExpr:
		return "passed to a generic function"
	default:
		return "passed to a computed function"
	}
	switch typed := object.(type) {
	case *types.Builtin:
		switch typed.Name() {
		case "len", "cap":
			return ""
		default:
			return "passed to builtin " + typed.Name()
		}
	case *types.Func:
		if _, guest := a.functions[typed.Origin()]; guest || typed.Pkg() == nil {
			return "passed to guest function " + typed.Name()
		}
		return ""
	default:
		return "passed to a function value"
	}
}

// byValueType reports whether goType contains no references.
//
// Takes goType (types.Type) which is classified.
//
// Returns bool which is true for a by-value type.
func byValueType(goType types.Type) bool {
	switch underlying := goType.Underlying().(type) {
	case *types.Basic:
		return underlying.Kind() != types.UnsafePointer && underlying.Kind() != types.UntypedNil && underlying.Kind() != types.Invalid
	case *types.Array:
		return byValueType(underlying.Elem())
	case *types.Struct:
		for field := range underlying.Fields() {
			if !byValueType(field.Type()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
