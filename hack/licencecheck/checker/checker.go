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

package checker

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const (
	// opcodeHandlerParameters is the number of parameters an opcode handler takes.
	opcodeHandlerParameters = 4

	// maxReferenceDepth bounds how deeply carriesReferences descends into arrays and
	// structs; a deeper type is assumed to carry references.
	maxReferenceDepth = 8
)

// unsafeAnalyser is the unsafe-writes rule.
var unsafeAnalyser = &analysis.Analyzer{
	Name: "licenceunsafe",
	Doc:  "unsafe-writes: unsafe and reflective write capability only in allow-listed functions",
	Run:  runUnsafe,
}

// structAnalyser checks the struct-field rules.
var structAnalyser = &analysis.Analyzer{
	Name: "licencestruct",
	Doc:  "roots, closure-fields and program-lifetime: every field of a listed struct is classified",
	Run:  runStructs,
}

// exhaustiveAnalyser is the exhaustive-switch rule.
var exhaustiveAnalyser = &analysis.Analyzer{
	Name: "licenceexhaustive",
	Doc:  "exhaustive-switch: switches over listed kinds name every kind or justify their default",
	Run:  runExhaustive,
}

// handlerAnalyser is the handler-barriers rule.
var handlerAnalyser = &analysis.Analyzer{
	Name: "licencehandler",
	Doc:  "handler-barriers: a handler storing a reference calls a barrier, or is justified",
	Run:  runHandlers,
}

// handlerStores is what a scan of one opcode handler found.
type handlerStores struct {
	// first is the position of the first reference store, or token.NoPos.
	first token.Pos

	// count is the number of reference stores.
	count int

	// barrier is true when the handler calls a barrier function.
	barrier bool
}

// Analysers returns every licencecheck analyser.
//
// Returns []*analysis.Analyzer which holds the unsafe-writes rule, the struct-field rules
// (roots, closure-fields and program-lifetime), the exhaustive-switch rule and the
// handler-barriers rule.
func Analysers() []*analysis.Analyzer {
	return []*analysis.Analyzer{unsafeAnalyser, structAnalyser, exhaustiveAnalyser, handlerAnalyser}
}

// productionFiles returns the pass's files that are not tests.
//
// The rules concern the engine Pipit runs, and go vet hands test files to the analysers
// too.
//
// Takes pass (*analysis.Pass) which holds the files.
//
// Returns []*ast.File which holds the non-test files.
func productionFiles(pass *analysis.Pass) []*ast.File {
	var files []*ast.File
	for _, file := range pass.Files {
		if !strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
			files = append(files, file)
		}
	}
	return files
}

// funcKey names declared functions using types.Func.FullName, as the configuration does.
//
// Examples include "pipit.sh/pipit/internal/engine.foo" and
// "(*pipit.sh/pipit/internal/engine.T).foo".
//
// Takes pass (*analysis.Pass) which holds the type information.
// Takes decl (*ast.FuncDecl) which is the function.
//
// Returns string which is the key, or "" when the function has no object.
func funcKey(pass *analysis.Pass, decl *ast.FuncDecl) string {
	object, ok := pass.TypesInfo.Defs[decl.Name].(*types.Func)
	if !ok {
		return ""
	}
	return object.FullName()
}

// calleeOf resolves the function or method a call names, or nil.
//
// Takes info (*types.Info) which holds the type information.
// Takes call (*ast.CallExpr) which is the call.
//
// Returns types.Object which is a *types.Func or *types.Builtin, or nil.
func calleeOf(info *types.Info, call *ast.CallExpr) types.Object {
	fun := ast.Unparen(call.Fun)
	if index, ok := fun.(*ast.IndexExpr); ok {
		fun = index.X
	}
	if index, ok := fun.(*ast.IndexListExpr); ok {
		fun = index.X
	}
	switch fun := fun.(type) {
	case *ast.Ident:
		return info.Uses[fun]
	case *ast.SelectorExpr:
		if selection, ok := info.Selections[fun]; ok {
			return selection.Obj()
		}
		return info.Uses[fun.Sel]
	}
	return nil
}

// unsafePrimitive names the unsafe or reflective write capability a call or conversion
// creates, or "".
//
// Takes info (*types.Info) which holds the type information.
// Takes call (*ast.CallExpr) which is the call or conversion.
//
// Returns string which is the primitive's name.
func unsafePrimitive(info *types.Info, call *ast.CallExpr) string {
	if typeAndValue, ok := info.Types[call.Fun]; ok && typeAndValue.IsType() && len(call.Args) == 1 {
		return conversionPrimitive(info, typeAndValue.Type.Underlying(), call.Args[0])
	}
	switch callee := calleeOf(info, call).(type) {
	case *types.Builtin:
		return builtinPrimitive(callee)
	case *types.Func:
		return funcPrimitive(callee)
	}
	return ""
}

// conversionPrimitive names the unsafe capability a conversion creates, or "".
//
// Takes info (*types.Info) which holds the type information.
// Takes to (types.Type) which is the underlying type converted to.
// Takes argument (ast.Expr) which is the converted expression.
//
// Returns string which is the primitive's name.
func conversionPrimitive(info *types.Info, to types.Type, argument ast.Expr) string {
	from := info.TypeOf(argument)
	if from == nil {
		return ""
	}
	fromBasic, ok := from.Underlying().(*types.Basic)
	if !ok {
		return ""
	}
	if _, isPointer := to.(*types.Pointer); isPointer && fromBasic.Kind() == types.UnsafePointer {
		return "conv-unsafe->ptr"
	}
	if toBasic, isBasic := to.(*types.Basic); isBasic && toBasic.Kind() == types.UnsafePointer && fromBasic.Kind() == types.Uintptr {
		return "conv-uintptr->unsafe"
	}
	return ""
}

// builtinPrimitive names the unsafe capability a call to a builtin creates, or "".
//
// Takes callee (*types.Builtin) which is the builtin.
//
// Returns string which is the primitive's name.
func builtinPrimitive(callee *types.Builtin) string {
	switch callee.Name() {
	case "Slice", "String", "SliceData", "StringData", "Add":
		if callee.Pkg() == nil || callee.Pkg().Path() == "unsafe" {
			return "unsafe." + callee.Name()
		}
	}
	return ""
}

// funcPrimitive names the unsafe or reflective write capability a call to a function or
// method creates, or "".
//
// Takes callee (*types.Func) which is the function.
//
// Returns string which is the primitive's name.
func funcPrimitive(callee *types.Func) string {
	pkg := ""
	if callee.Pkg() != nil {
		pkg = callee.Pkg().Path()
	}
	name := callee.Name()
	switch {
	case pkg == "reflect" && name == "NewAt":
		return "reflect.NewAt"
	case pkg == "reflect" && name == "SetPointer":
		return "reflect.Value.SetPointer"
	case pkg == "sync/atomic" && isAtomicPointerStore(name):
		return "atomic." + name
	case pkg == "sync/atomic" && isAtomicPointerMethod(callee):
		return "atomic.Pointer." + name
	case isMemoryMover(name):
		return "memmove:" + name
	}
	return ""
}

// isAtomicPointerStore reports whether name is one of sync/atomic's StorePointer,
// SwapPointer or CompareAndSwapPointer family.
//
// Takes name (string) which is the function's name.
//
// Returns bool which is true when it is.
func isAtomicPointerStore(name string) bool {
	if !strings.HasSuffix(name, "Pointer") {
		return false
	}
	return strings.HasPrefix(name, "Store") || strings.HasPrefix(name, "Swap") || strings.HasPrefix(name, "CompareAndSwap")
}

// isMemoryMover reports whether a function's name marks it as a runtime memory move or
// clear (memmove, memclr, typedslicecopy and their variants).
//
// Takes name (string) which is the function's name.
//
// Returns bool which is true when it does.
func isMemoryMover(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "memmove") || strings.Contains(lower, "memclr") || strings.Contains(lower, "typedslicecopy")
}

// isAtomicPointerMethod reports whether function is Store, Swap or CompareAndSwap of
// sync/atomic.Pointer[T].
//
// Takes function (*types.Func) which is the method.
//
// Returns bool which is true when it is.
func isAtomicPointerMethod(function *types.Func) bool {
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return false
	}
	switch function.Name() {
	case "Store", "Swap", "CompareAndSwap":
	default:
		return false
	}
	receiverType := signature.Recv().Type()
	if pointer, ok := receiverType.(*types.Pointer); ok {
		receiverType = pointer.Elem()
	}
	named, ok := receiverType.(*types.Named)
	return ok && named.Obj().Name() == "Pointer"
}

// runUnsafe reports unapproved unsafe or reflective write capabilities.
//
// Takes pass (*analysis.Pass) which supplies the package syntax and type information.
//
// Returns any which is nil because findings are reported through pass.
// Returns error when the checker configuration cannot be loaded.
func runUnsafe(pass *analysis.Pass) (any, error) {
	config, err := activeConfig()
	if err != nil {
		return nil, err
	}
	for _, file := range productionFiles(pass) {
		for _, declaration := range file.Decls {
			if decl, ok := declaration.(*ast.FuncDecl); ok && decl.Body != nil {
				checkUnsafeFunc(pass, config, decl)
			}
		}
	}
	return nil, nil
}

// checkUnsafeFunc reports decl when it creates unsafe or reflective write capability and
// is not allow-listed.
//
// Takes pass (*analysis.Pass) which supplies the type information and receives findings.
// Takes config (*checkerConfig) which holds the allow-list.
// Takes decl (*ast.FuncDecl) which is the function, with a body.
func checkUnsafeFunc(pass *analysis.Pass, config *checkerConfig, decl *ast.FuncDecl) {
	names := unsafePrimitivesIn(pass.TypesInfo, decl.Body)
	if len(names) == 0 {
		return
	}
	key := funcKey(pass, decl)
	if _, allowed := config.UnsafeAllowed[key]; allowed {
		return
	}
	pass.Reportf(decl.Name.Pos(), "licencecheck unsafe-writes: %s creates unsafe or reflective write capability (%s) and is not in unsafe_allowed", key, strings.Join(names, ", "))
}

// unsafePrimitivesIn lists the unsafe or reflective write capabilities a function body
// creates.
//
// Takes info (*types.Info) which holds the type information.
// Takes body (*ast.BlockStmt) which is the body.
//
// Returns []string which holds each primitive's name once, sorted.
func unsafePrimitivesIn(info *types.Info, body *ast.BlockStmt) []string {
	var names []string
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if primitive := unsafePrimitive(info, call); primitive != "" && !slices.Contains(names, primitive) {
			names = append(names, primitive)
		}
		return true
	})
	slices.Sort(names)
	return names
}

// runStructs checks that the configured structural field classifications remain complete.
//
// Takes pass (*analysis.Pass) which supplies the package syntax and type information.
//
// Returns any which is nil because findings are reported through pass.
// Returns error when the checker configuration cannot be loaded.
func runStructs(pass *analysis.Pass) (any, error) {
	config, err := activeConfig()
	if err != nil {
		return nil, err
	}
	for _, file := range productionFiles(pass) {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				if typeSpec, isType := spec.(*ast.TypeSpec); isType {
					checkStructSpec(pass, config, typeSpec)
				}
			}
		}
	}
	return nil, nil
}

// checkStructSpec reports the unclassified fields of a listed struct type, and the
// classified fields it no longer has.
//
// Takes pass (*analysis.Pass) which receives findings.
// Takes config (*checkerConfig) which holds the struct rules.
// Takes typeSpec (*ast.TypeSpec) which is the type declaration.
func checkStructSpec(pass *analysis.Pass, config *checkerConfig, typeSpec *ast.TypeSpec) {
	structType, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return
	}
	rule, listed := config.Structs[pass.Pkg.Path()+"."+typeSpec.Name.Name]
	if !listed {
		return
	}
	seen := map[string]bool{}
	for _, field := range structType.Fields.List {
		for _, name := range fieldNames(field) {
			seen[name.Name] = true
			if _, classified := rule.Fields[name.Name]; !classified {
				pass.Reportf(name.Pos(), "licencecheck %s: field %s.%s is not classified in licencecheck.json (%s)", rule.Rule, typeSpec.Name.Name, name.Name, rule.Why)
			}
		}
	}
	var stale []string
	for name := range rule.Fields {
		if !seen[name] {
			stale = append(stale, name)
		}
	}
	slices.Sort(stale)
	for _, name := range stale {
		pass.Reportf(typeSpec.Name.Pos(), "licencecheck %s: licencecheck.json classifies %s.%s, which no longer exists", rule.Rule, typeSpec.Name.Name, name)
	}
}

// fieldNames returns the names a struct field declares, naming an embedded field by its
// type.
//
// Takes field (*ast.Field) which is the field.
//
// Returns []*ast.Ident which holds the names, positioned at the field.
func fieldNames(field *ast.Field) []*ast.Ident {
	if len(field.Names) > 0 {
		return field.Names
	}
	name := ast.NewIdent(embeddedName(field.Type))
	name.NamePos = field.Pos()
	return []*ast.Ident{name}
}

// embeddedName names an embedded field by its type.
//
// Takes expr (ast.Expr) which is the field's type.
//
// Returns string which is the field's name.
func embeddedName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.StarExpr:
		return embeddedName(expr.X)
	case *ast.SelectorExpr:
		return expr.Sel.Name
	case *ast.Ident:
		return expr.Name
	case *ast.IndexExpr:
		return embeddedName(expr.X)
	case *ast.IndexListExpr:
		return embeddedName(expr.X)
	}
	return "?"
}

// runExhaustive checks that configured enum switches cover every constant.
//
// Takes pass (*analysis.Pass) which supplies the package syntax and type information.
//
// Returns any which is nil because findings are reported through pass.
// Returns error when the checker configuration cannot be loaded.
func runExhaustive(pass *analysis.Pass) (any, error) {
	config, err := activeConfig()
	if err != nil {
		return nil, err
	}
	enums := map[string]bool{}
	for _, name := range config.Exhaustive.Types {
		enums[name] = true
	}
	for _, file := range productionFiles(pass) {
		for _, declaration := range file.Decls {
			decl, ok := declaration.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			checkEnumSwitches(pass, config, enums, decl)
		}
	}
	return nil, nil
}

// checkEnumSwitches checks every tagged switch in a function's body.
//
// Takes pass (*analysis.Pass) which supplies the type information and receives findings.
// Takes config (*checkerConfig) which holds the justified defaults.
// Takes enums (map[string]bool) which holds the listed enum types.
// Takes decl (*ast.FuncDecl) which is the function, with a body.
func checkEnumSwitches(pass *analysis.Pass, config *checkerConfig, enums map[string]bool, decl *ast.FuncDecl) {
	key := funcKey(pass, decl)
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		if sw, isSwitch := node.(*ast.SwitchStmt); isSwitch && sw.Tag != nil {
			checkEnumSwitch(pass, config, enums, key, sw)
		}
		return true
	})
}

// checkEnumSwitch reports a switch over a listed enum type that names neither every
// constant nor a justified default.
//
// Takes pass (*analysis.Pass) which supplies the type information and receives findings.
// Takes config (*checkerConfig) which holds the justified defaults.
// Takes enums (map[string]bool) which holds the listed enum types.
// Takes key (string) which names the enclosing function.
// Takes sw (*ast.SwitchStmt) which is the switch, with a tag.
func checkEnumSwitch(pass *analysis.Pass, config *checkerConfig, enums map[string]bool, key string, sw *ast.SwitchStmt) {
	named, ok := types.Unalias(pass.TypesInfo.TypeOf(sw.Tag)).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !enums[named.Obj().Pkg().Path()+"."+named.Obj().Name()] {
		return
	}
	listed, hasDefault := switchCases(pass.TypesInfo, sw)
	missing := missingConstants(named, listed)
	if len(missing) == 0 {
		return
	}
	if _, justified := config.Exhaustive.AllowedDefaults[key]; hasDefault && justified {
		return
	}
	pass.Reportf(sw.Pos(), "licencecheck exhaustive-switch: switch on %s in %s does not name %s, and its default is not justified in licencecheck.json", named.Obj().Name(), key,
		strings.Join(missing, ", "))
}

// switchCases collects the constants a switch's cases name.
//
// Takes info (*types.Info) which holds the type information.
// Takes sw (*ast.SwitchStmt) which is the switch.
//
// Returns listed (map[string]bool) which holds the named constants.
// Returns hasDefault (bool) which is true when the switch has a default clause.
func switchCases(info *types.Info, sw *ast.SwitchStmt) (listed map[string]bool, hasDefault bool) {
	listed = map[string]bool{}
	for _, statement := range sw.Body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		if clause.List == nil {
			hasDefault = true
		}
		for _, expr := range clause.List {
			if object, isConst := info.Uses[identOf(expr)].(*types.Const); isConst {
				listed[object.Name()] = true
			}
		}
	}
	return listed, hasDefault
}

// missingConstants lists the constants of an enum type a switch does not name.
//
// Takes named (*types.Named) which is the enum type.
// Takes listed (map[string]bool) which holds the constants the switch names.
//
// Returns []string which holds the missing constants in scope order.
func missingConstants(named *types.Named, listed map[string]bool) []string {
	var missing []string
	scope := named.Obj().Pkg().Scope()
	for _, name := range scope.Names() {
		constant, ok := scope.Lookup(name).(*types.Const)
		if ok && types.Identical(constant.Type(), named) && !listed[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// identOf returns the identifier a case expression names, or nil.
//
// Takes expr (ast.Expr) which is the expression.
//
// Returns *ast.Ident which is the identifier.
func identOf(expr ast.Expr) *ast.Ident {
	switch expr := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return expr
	case *ast.SelectorExpr:
		return expr.Sel
	}
	return nil
}

// runHandlers reports opcode handlers whose reference stores lack approved barriers.
//
// Takes pass (*analysis.Pass) which supplies the package syntax and type information.
//
// Returns any which is nil because findings are reported through pass.
// Returns error when the checker configuration cannot be loaded.
func runHandlers(pass *analysis.Pass) (any, error) {
	config, err := activeConfig()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(config.Handlers.Packages, pass.Pkg.Path()) {
		return nil, nil
	}
	barriers := map[string]bool{}
	for _, name := range config.Handlers.Barriers {
		barriers[name] = true
	}
	for _, file := range productionFiles(pass) {
		for _, declaration := range file.Decls {
			decl, ok := declaration.(*ast.FuncDecl)
			if !ok || decl.Body == nil || decl.Recv != nil || !strings.HasPrefix(decl.Name.Name, config.Handlers.Prefix) || !isOpcodeHandler(pass, decl) {
				continue
			}
			checkHandler(pass, config, barriers, decl)
		}
	}
	return nil, nil
}

// checkHandler reports an opcode handler that stores references into memory without a
// barrier, beyond its reviewed allowance.
//
// Takes pass (*analysis.Pass) which supplies the type information and receives findings.
// Takes config (*checkerConfig) which holds the reviewed handlers.
// Takes barriers (map[string]bool) which holds the barrier functions' keys.
// Takes decl (*ast.FuncDecl) which is the handler.
func checkHandler(pass *analysis.Pass, config *checkerConfig, barriers map[string]bool, decl *ast.FuncDecl) {
	var found handlerStores
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			found.noteCall(pass.TypesInfo, barriers, node)
		case *ast.AssignStmt:
			for index, lhs := range node.Lhs {
				if index < len(node.Rhs) && isMemoryStore(pass, decl, lhs, node.Rhs[index]) {
					found.noteStore(lhs.Pos())
				}
			}
		}
		return true
	})
	if !found.first.IsValid() || found.barrier {
		return
	}
	key := funcKey(pass, decl)
	if allowed, ok := config.Handlers.Allowed[key]; ok && found.count <= allowed.Stores {
		return
	}
	pass.Reportf(found.first, "licencecheck handler-barriers: %s makes %d reference stores into memory and calls no barrier; add a barrier or review it in licencecheck.json", key, found.count)
}

// noteCall records a call to a barrier function or a reflective store.
//
// Takes info (*types.Info) which holds the type information.
// Takes barriers (map[string]bool) which holds the barrier functions' keys.
// Takes call (*ast.CallExpr) which is the call.
func (h *handlerStores) noteCall(info *types.Info, barriers map[string]bool, call *ast.CallExpr) {
	callee, ok := calleeOf(info, call).(*types.Func)
	if !ok {
		return
	}
	if barriers[callee.FullName()] {
		h.barrier = true
	}
	if isReflectStore(callee) {
		h.noteStore(call.Pos())
	}
}

// noteStore records one reference store.
//
// Takes position (token.Pos) which is where the store is.
func (h *handlerStores) noteStore(position token.Pos) {
	h.count++
	if !h.first.IsValid() {
		h.first = position
	}
}

// isOpcodeHandler reports whether decl has the opcode-handler signature: four parameters
// and one result named OpResult.
//
// Takes pass (*analysis.Pass) which holds the type information.
// Takes decl (*ast.FuncDecl) which is the function.
//
// Returns bool which is true when it does.
func isOpcodeHandler(pass *analysis.Pass, decl *ast.FuncDecl) bool {
	object, ok := pass.TypesInfo.Defs[decl.Name].(*types.Func)
	if !ok {
		return false
	}
	signature, ok := object.Type().(*types.Signature)
	if !ok || signature.Params().Len() != opcodeHandlerParameters || signature.Results().Len() != 1 {
		return false
	}
	named, ok := signature.Results().At(0).Type().(*types.Named)
	return ok && named.Obj().Name() == "OpResult"
}

// isReflectStore reports whether function writes through a reflect.Value by calling Set,
// SetMapIndex, Send or reflect.Copy.
//
// Takes function (*types.Func) which is the callee.
//
// Returns bool which is true when it does.
func isReflectStore(function *types.Func) bool {
	if function.Pkg() == nil || function.Pkg().Path() != "reflect" {
		return false
	}
	switch function.Name() {
	case "Set", "SetMapIndex", "Send", "Copy", "SetIterValue":
		return true
	}
	return false
}

// isMemoryStore reports whether an assignment writes a reference-carrying value through a
// pointer, slice element, map entry or field of a value the handler did not create, other
// than the VM's own registers, frame and VM state.
//
// Takes pass (*analysis.Pass) which holds the type information.
// Takes decl (*ast.FuncDecl) which is the handler.
// Takes lhs (ast.Expr) which is the assignment's destination expression.
// Takes rhs (ast.Expr) which is the assignment's source expression.
//
// Returns bool which is true when it does.
func isMemoryStore(pass *analysis.Pass, decl *ast.FuncDecl, lhs, rhs ast.Expr) bool {
	switch lhs.(type) {
	case *ast.IndexExpr, *ast.StarExpr, *ast.SelectorExpr:
	default:
		return false
	}
	if !carriesReferences(pass.TypesInfo.TypeOf(rhs), 0) {
		return false
	}
	root := rootIdent(lhs)
	if root == nil {
		return true
	}
	object, ok := pass.TypesInfo.Uses[root].(*types.Var)
	if !ok {
		return true
	}

	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			if pass.TypesInfo.Defs[name] == object {
				return name.Name != "registers" && name.Name != "frame"
			}
		}
	}

	return !freshLocal(pass, decl, object)
}

// freshLocal reports whether object is a local variable of decl assigned only fresh
// storage.
//
// Takes pass (*analysis.Pass) which holds the type information.
// Takes decl (*ast.FuncDecl) which is the handler.
// Takes object (*types.Var) which is the variable.
//
// Returns bool which is true when every assignment to it is fresh.
func freshLocal(pass *analysis.Pass, decl *ast.FuncDecl, object *types.Var) bool {
	if object.Parent() == nil || object.Parent() == pass.Pkg.Scope() {
		return false
	}
	assigned, fresh := false, true
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		for _, rhs := range assignedValues(pass.TypesInfo, node, object) {
			assigned = true
			fresh = fresh && isFreshExpr(pass, rhs)
		}
		return true
	})
	return assigned && fresh
}

// assignedValues returns the values an assignment or variable declaration gives object.
//
// Takes info (*types.Info) which holds the type information.
// Takes node (ast.Node) which is the statement or specification.
// Takes object (*types.Var) which is the variable.
//
// Returns []ast.Expr which holds the values, or nil.
func assignedValues(info *types.Info, node ast.Node, object *types.Var) []ast.Expr {
	var values []ast.Expr
	switch node := node.(type) {
	case *ast.AssignStmt:
		if len(node.Lhs) != len(node.Rhs) {
			return nil
		}
		for index, lhs := range node.Lhs {
			if namesVariable(info, lhs, object) {
				values = append(values, node.Rhs[index])
			}
		}
	case *ast.ValueSpec:
		for index, name := range node.Names {
			if info.Defs[name] == object && index < len(node.Values) {
				values = append(values, node.Values[index])
			}
		}
	}
	return values
}

// namesVariable reports whether an assignment's destination is the variable itself.
//
// Takes info (*types.Info) which holds the type information.
// Takes lhs (ast.Expr) which is the destination.
// Takes object (*types.Var) which is the variable.
//
// Returns bool which is true when it is.
func namesVariable(info *types.Info, lhs ast.Expr, object *types.Var) bool {
	ident, ok := lhs.(*ast.Ident)
	return ok && (info.Defs[ident] == object || info.Uses[ident] == object)
}

// isFreshExpr reports whether expr creates new storage through a composite literal, its
// address, or a call to the builtins make or new.
//
// Takes pass (*analysis.Pass) which holds the type information.
// Takes expr (ast.Expr) which is the expression.
//
// Returns bool which is true when it does.
func isFreshExpr(pass *analysis.Pass, expr ast.Expr) bool {
	switch typed := ast.Unparen(expr).(type) {
	case *ast.CompositeLit:
		return true
	case *ast.UnaryExpr:
		_, literal := ast.Unparen(typed.X).(*ast.CompositeLit)
		return typed.Op == token.AND && literal
	case *ast.CallExpr:
		if builtin, ok := calleeOf(pass.TypesInfo, typed).(*types.Builtin); ok {
			return builtin.Name() == "make" || builtin.Name() == "new"
		}
	}
	return false
}

// rootIdent returns the variable an addressing expression starts from, or nil.
//
// Takes expr (ast.Expr) which is the expression.
//
// Returns *ast.Ident which is the root.
func rootIdent(expr ast.Expr) *ast.Ident {
	for {
		switch typed := ast.Unparen(expr).(type) {
		case *ast.Ident:
			return typed
		case *ast.IndexExpr:
			expr = typed.X
		case *ast.SelectorExpr:
			expr = typed.X
		case *ast.StarExpr:
			expr = typed.X
		case *ast.CallExpr:
			return nil
		default:
			return nil
		}
	}
}

// carriesReferences reports whether values of goType can hold pointers.
//
// Takes goType (types.Type) which is the type.
// Takes depth (int) which bounds the recursion.
//
// Returns bool which is true when they can.
func carriesReferences(goType types.Type, depth int) bool {
	if goType == nil || depth > maxReferenceDepth {
		return goType != nil
	}
	switch underlying := goType.Underlying().(type) {
	case *types.Basic:
		return underlying.Kind() == types.String || underlying.Kind() == types.UnsafePointer || underlying.Kind() == types.UntypedString
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	case *types.Array:
		return carriesReferences(underlying.Elem(), depth+1)
	case *types.Struct:
		for field := range underlying.Fields() {
			if carriesReferences(field.Type(), depth+1) {
				return true
			}
		}
	}
	return false
}
