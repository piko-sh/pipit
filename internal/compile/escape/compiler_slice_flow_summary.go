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
	"cmp"
	"go/ast"
	"go/types"
	"slices"
)

// SliceFlows summarises, for every function, method and function literal a program
// defines, which operands its body stores beyond a call (see CollectFlowingSliceNames),
// and resolves a call against every callee it might reach.
type SliceFlows struct {
	// info resolves callees and types candidates.
	info *types.Info

	// declared holds the summary of each declared function and method, by declared object.
	declared map[types.Object]*flowUnit

	// literals holds the summary of each function literal.
	literals map[*ast.FuncLit]*flowUnit

	// functionValues lists every candidate a call through a function value might reach, by
	// signature shape.
	functionValues map[signatureShape][]flowCandidate

	// methods lists every declared method as an interface call might reach it, by name.
	methods map[string][]flowCandidate
}

// flowUnit is one function body and what it is known to store of its operands.
type flowUnit struct {
	// signature is the function's signature.
	signature *ast.FuncType

	// body is the function's body, or nil for a declaration without one.
	body *ast.BlockStmt

	// names lists the operand names, the receiver first for a method.
	names []string

	// flags marks the operands the body stores, aligned with names.
	flags []bool
}

// flowCandidate is a way a dynamic call might reach a unit.
type flowCandidate struct {
	// unit is the function reached.
	unit *flowUnit

	// signature is the signature the call sees: a method value's has no receiver, a method
	// expression's takes the receiver first.
	signature *types.Signature

	// skip is how many leading operands the call does not pass as arguments: one for a
	// method value's receiver.
	skip int

	// generic reports that the signature mentions a type parameter, so it is matched by
	// shape alone.
	generic bool
}

// signatureShape buckets signatures that could be identical.
type signatureShape struct {
	// parameters is the parameter count.
	parameters int

	// results is the result count.
	results int

	// variadic reports a variadic final parameter.
	variadic bool
}

// ComputeSliceFlows summarises every declaration given and every function literal info
// records, iterating to a fixed point so an operand passed on to a stored operand of any
// callee it may reach is stored too.
//
// Takes info (*types.Info) which resolves declarations, literals and callees.
// Takes declarations ([]*ast.FuncDecl) which are the functions and methods to summarise.
//
// Returns *SliceFlows which resolves calls; with a nil info it resolves none.
func ComputeSliceFlows(info *types.Info, declarations []*ast.FuncDecl) *SliceFlows {
	flows := &SliceFlows{
		info:           info,
		declared:       make(map[types.Object]*flowUnit, len(declarations)),
		literals:       make(map[*ast.FuncLit]*flowUnit),
		functionValues: make(map[signatureShape][]flowCandidate),
		methods:        make(map[string][]flowCandidate),
	}
	if info == nil {
		return flows
	}
	units := make([]*flowUnit, 0, len(declarations))
	for _, declaration := range declarations {
		if unit := flows.addDeclaration(declaration); unit != nil {
			units = append(units, unit)
		}
	}
	units = append(units, flows.addLiterals()...)
	for changed := true; changed; {
		changed = false
		for _, unit := range units {
			next := unit.operandFlows(info, flows.Lookup)
			if !slices.Equal(unit.flags, next) {
				unit.flags = next
				changed = true
			}
		}
	}
	return flows
}

// Lookup resolves what the callees a call may reach do with its operands. It is the
// CallLookup the compiler and the fixed point use.
//
// Takes call (*ast.CallExpr) which is the call.
//
// Returns CallFlow which is the zero value for a conversion, a builtin or a native
// callee.
func (f *SliceFlows) Lookup(call *ast.CallExpr) CallFlow {
	none := CallFlow{Arguments: nil, Receiver: false, PacksVariadic: false}
	if f == nil || f.info == nil || f.info.Types[call.Fun].IsType() {
		return none
	}
	function := ast.Unparen(call.Fun)
	if literal, ok := function.(*ast.FuncLit); ok {
		if unit := f.literals[literal]; unit != nil {
			return CallFlow{Arguments: unit.flags, Receiver: false, PacksVariadic: false}
		}
	}
	if method, signature, ok := interfaceMethodCall(f.info, function); ok {
		return CallFlow{Arguments: unionFlags(f.methods[method], signature), Receiver: false, PacksVariadic: false}
	}
	if callee := calleeObject(f.info, call); callee != nil {
		unit, declared := f.declared[callee]
		if !declared {
			return none
		}
		return declaredCallFlow(f.info, call, unit.flags)
	}
	if _, isBuiltin := f.info.Uses[identifierOf(function)].(*types.Builtin); isBuiltin {
		return none
	}
	signature, ok := f.info.TypeOf(call.Fun).Underlying().(*types.Signature)
	if !ok {
		return none
	}
	return CallFlow{Arguments: unionFlags(f.functionValues[shapeOf(signature)], signature), Receiver: false, PacksVariadic: false}
}

// addDeclaration registers a function or method and the ways a call might reach it.
//
// Takes declaration (*ast.FuncDecl) which is the declaration.
//
// Returns *flowUnit which is the new summary, or nil when info does not define it.
func (f *SliceFlows) addDeclaration(declaration *ast.FuncDecl) *flowUnit {
	function, ok := f.info.Defs[declaration.Name].(*types.Func)
	if !ok {
		return nil
	}
	names := append(fieldNames(declaration.Recv), fieldNames(declaration.Type.Params)...)
	unit := &flowUnit{
		signature: declaration.Type,
		body:      declaration.Body,
		names:     names,
		flags:     make([]bool, len(names)),
	}
	f.declared[function] = unit
	signature := function.Signature()
	if signature.Recv() == nil {
		f.addFunctionValue(unit, signature, 0)
		return unit
	}
	f.methods[function.Name()] = append(f.methods[function.Name()], newFlowCandidate(unit, signature, 1))
	f.addFunctionValue(unit, signature, 1)
	f.addFunctionValue(unit, methodExpressionSignature(signature), 0)
	return unit
}

// addLiterals registers every function literal info records, in source order.
//
// Returns []*flowUnit which holds the new summaries.
func (f *SliceFlows) addLiterals() []*flowUnit {
	var literals []*ast.FuncLit
	for expression := range f.info.Types {
		if literal, ok := expression.(*ast.FuncLit); ok {
			literals = append(literals, literal)
		}
	}
	slices.SortFunc(literals, func(a, b *ast.FuncLit) int { return cmp.Compare(a.Pos(), b.Pos()) })
	units := make([]*flowUnit, 0, len(literals))
	for _, literal := range literals {
		signature, ok := f.info.TypeOf(literal).(*types.Signature)
		if !ok {
			continue
		}
		names := fieldNames(literal.Type.Params)
		unit := &flowUnit{
			signature: literal.Type,
			body:      literal.Body,
			names:     names,
			flags:     make([]bool, len(names)),
		}
		f.literals[literal] = unit
		f.addFunctionValue(unit, signature, 0)
		units = append(units, unit)
	}
	return units
}

// addFunctionValue registers a way a call through a function value might reach a unit.
//
// Takes unit (*flowUnit) which is the function reached.
// Takes signature (*types.Signature) which is the signature the call sees.
// Takes skip (int) which counts the leading operands the call does not pass.
func (f *SliceFlows) addFunctionValue(unit *flowUnit, signature *types.Signature, skip int) {
	shape := shapeOf(signature)
	f.functionValues[shape] = append(f.functionValues[shape], newFlowCandidate(unit, signature, skip))
}

// operandFlows flags the operands the unit's body stores, given what the callees it may
// reach are known to store so far.
//
// Takes info (*types.Info) which resolves callees.
// Takes lookup (CallLookup) which resolves calls.
//
// Returns []bool which holds one flag per operand.
func (u *flowUnit) operandFlows(info *types.Info, lookup CallLookup) []bool {
	flags := make([]bool, len(u.names))
	if u.body == nil {
		return flags
	}
	flowing := CollectFlowingSliceNames(info, u.signature, u.body, lookup)
	for i, name := range u.names {
		flags[i] = name != "" && name != "_" && flowing[name]
	}
	return flags
}

// interfaceMethodCall recognises a call of an interface's method, including one promoted
// through an embedded interface and one a type parameter's constraint declares.
//
// Takes info (*types.Info) which records method selections.
// Takes function (ast.Expr) which is the unparenthesised callee.
//
// Returns the method name, the signature the call sees, and false for any other callee.
func interfaceMethodCall(info *types.Info, function ast.Expr) (string, *types.Signature, bool) {
	selector, ok := function.(*ast.SelectorExpr)
	if !ok {
		return "", nil, false
	}
	selection, isSelection := info.Selections[selector]
	if !isSelection || selection.Kind() != types.MethodVal {
		return "", nil, false
	}
	method, isMethod := selection.Obj().(*types.Func)
	if !isMethod || method.Signature().Recv() == nil || !types.IsInterface(method.Signature().Recv().Type()) {
		return "", nil, false
	}
	signature, isSignature := selection.Type().(*types.Signature)
	return method.Name(), signature, isSignature
}

// declaredCallFlow aligns a declared callee's operand flags with a call's arguments: a
// method value's receiver is the selector's operand rather than an argument.
//
// Takes info (*types.Info) which classifies the selector.
// Takes call (*ast.CallExpr) which is the call.
// Takes operands ([]bool) which are the callee's operand flags.
//
// Returns CallFlow which reports the compiled code packing variadic arguments itself.
func declaredCallFlow(info *types.Info, call *ast.CallExpr, operands []bool) CallFlow {
	if selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && len(operands) > 0 {
		if selection, isSelection := info.Selections[selector]; isSelection && selection.Kind() == types.MethodVal {
			return CallFlow{Arguments: operands[1:], Receiver: operands[0], PacksVariadic: true}
		}
	}
	return CallFlow{Arguments: operands, Receiver: false, PacksVariadic: true}
}

// unionFlags combines the argument flags of every candidate a call of the given signature
// might reach.
//
// Takes candidates ([]flowCandidate) which share the call's signature shape or method
// name.
// Takes signature (*types.Signature) which is the signature the call sees.
//
// Returns []bool which flags an argument any matching candidate stores, or nil when none
// matches.
func unionFlags(candidates []flowCandidate, signature *types.Signature) []bool {
	if signature == nil {
		return nil
	}
	generic := mentionsTypeParameter(signature)
	var flags []bool
	for _, candidate := range candidates {
		if !candidate.matches(signature, generic) {
			continue
		}
		operands := candidate.unit.flags[min(candidate.skip, len(candidate.unit.flags)):]
		if flags == nil {
			flags = make([]bool, signature.Params().Len())
		}
		for i := range min(len(flags), len(operands)) {
			flags[i] = flags[i] || operands[i]
		}
	}
	return flags
}

// matches reports whether a call of the given signature might reach the candidate. A
// signature mentioning a type parameter matches any candidate of the same shape.
//
// Takes signature (*types.Signature) which is the signature the call sees.
// Takes generic (bool) which reports that signature mentions a type parameter.
//
// Returns bool which is true when the call might reach the candidate.
func (c flowCandidate) matches(signature *types.Signature, generic bool) bool {
	if shapeOf(signature) != shapeOf(c.signature) {
		return false
	}
	return generic || c.generic || types.Identical(signature, c.signature)
}

// newFlowCandidate builds a candidate, noting whether its signature is generic.
//
// Takes unit (*flowUnit) which is the function reached.
// Takes signature (*types.Signature) which is the signature the call sees.
// Takes skip (int) which counts the leading operands the call does not pass.
//
// Returns flowCandidate which is the candidate.
func newFlowCandidate(unit *flowUnit, signature *types.Signature, skip int) flowCandidate {
	return flowCandidate{unit: unit, signature: signature, skip: skip, generic: mentionsTypeParameter(signature)}
}

// shapeOf buckets a signature by its parameter and result counts and variadicity.
//
// Takes signature (*types.Signature) which is the signature.
//
// Returns signatureShape which is its bucket.
func shapeOf(signature *types.Signature) signatureShape {
	return signatureShape{
		parameters: signature.Params().Len(),
		results:    signature.Results().Len(),
		variadic:   signature.Variadic(),
	}
}

// methodExpressionSignature is the signature of a method expression: the method's, with
// the receiver as the first parameter.
//
// Takes method (*types.Signature) which is the method's signature.
//
// Returns *types.Signature which takes the receiver first.
func methodExpressionSignature(method *types.Signature) *types.Signature {
	parameters := make([]*types.Var, 0, method.Params().Len()+1)
	parameters = append(parameters, method.Recv())
	for parameter := range method.Params().Variables() {
		parameters = append(parameters, parameter)
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), method.Results(), method.Variadic())
}

// calleeObject resolves the function or method a call names directly, as declared rather
// than instantiated, or nil for a function value, a builtin or a conversion.
//
// Takes info (*types.Info) which records identifier uses and method selections.
// Takes call (*ast.CallExpr) which is the call.
//
// Returns types.Object which is the callee, or nil.
func calleeObject(info *types.Info, call *ast.CallExpr) types.Object {
	function := ast.Unparen(call.Fun)
	if selector, ok := function.(*ast.SelectorExpr); ok {
		if selection, isSelection := info.Selections[selector]; isSelection {
			method, isMethod := selection.Obj().(*types.Func)
			if !isMethod || selection.Kind() == types.FieldVal {
				return nil
			}
			return method.Origin()
		}
	}
	if object, ok := info.Uses[identifierOf(function)].(*types.Func); ok {
		return object.Origin()
	}
	return nil
}

// identifierOf finds the identifier a callee expression names, looking through generic
// instantiation and package qualification, or nil for any other expression.
//
// Takes function (ast.Expr) which is the unparenthesised callee.
//
// Returns *ast.Ident which is the named identifier, or nil.
func identifierOf(function ast.Expr) *ast.Ident {
	switch indexed := function.(type) {
	case *ast.IndexExpr:
		function = indexed.X
	case *ast.IndexListExpr:
		function = indexed.X
	}
	switch callee := function.(type) {
	case *ast.Ident:
		return callee
	case *ast.SelectorExpr:
		return callee.Sel
	default:
		return nil
	}
}

// mentionsTypeParameter reports whether a type refers to a type parameter anywhere in its
// structure, without looking inside named types other than their type arguments.
//
// Takes t (types.Type) which is the type.
//
// Returns bool which is true when t mentions a type parameter.
func mentionsTypeParameter(t types.Type) bool {
	if _, isTypeParameter := types.Unalias(t).(*types.TypeParam); isTypeParameter {
		return true
	}
	return slices.ContainsFunc(componentTypes(t), mentionsTypeParameter)
}

// componentTypes lists the types a type is built from: element, key, parameter, result,
// field and type-parameter types, and a named type's type arguments.
//
// Takes t (types.Type) which is the type.
//
// Returns []types.Type which holds the components, or nil for a basic or interface type.
func componentTypes(t types.Type) []types.Type {
	switch typed := types.Unalias(t).(type) {
	case *types.Pointer:
		return []types.Type{typed.Elem()}
	case *types.Slice:
		return []types.Type{typed.Elem()}
	case *types.Array:
		return []types.Type{typed.Elem()}
	case *types.Chan:
		return []types.Type{typed.Elem()}
	case *types.Map:
		return []types.Type{typed.Key(), typed.Elem()}
	case *types.Signature:
		var components []types.Type
		for parameter := range typed.TypeParams().TypeParams() {
			components = append(components, parameter)
		}
		for variable := range typed.Params().Variables() {
			components = append(components, variable.Type())
		}
		for variable := range typed.Results().Variables() {
			components = append(components, variable.Type())
		}
		return components
	case *types.Struct:
		var components []types.Type
		for field := range typed.Fields() {
			components = append(components, field.Type())
		}
		return components
	case *types.Named:
		return slices.Collect(typed.TypeArgs().Types())
	default:
		return nil
	}
}
