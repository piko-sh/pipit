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

package engine

import (
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// reflectMethodOutOfRange is the message Go's reflect uses for a bad method index.
const reflectMethodOutOfRange = "reflect: Method index out of range"

// pipitMethodEntry is one method of a script type's method set as reflect exposes it.
type pipitMethodEntry struct {
	// callee is the compiled method.
	callee *program.CompiledFunction

	// methodRoot is the root function owning the method, or nil for the current root.
	methodRoot *program.CompiledFunction

	// name is the method name.
	name string
}

// pipitTypeStringer carries a rendered type name for fmt.
type pipitTypeStringer string

// String returns the rendered type name.
//
// Returns string which is the rendered type name.
func (s pipitTypeStringer) String() string { return string(s) }

// pipitReflectTypeName returns the script type name behind a reflect type: a synthesised
// struct's sentinel name, a pool type's bare name, or a registered type name; and whether
// rt is a pointer to it.
//
// Takes vm (*VM) which owns the type name registry.
// Takes rt (reflect.Type) which is the type reflect was asked about.
//
// Returns name (string) which is "" when rt is not a script type.
// Returns pointer (bool) which is true when rt is a pointer to the script type.
func pipitReflectTypeName(vm *VM, rt reflect.Type) (name string, pointer bool) {
	if named, ok := rt.(pipitNamedType); ok {
		return named.sourceName, false
	}
	base := rt
	if base.Kind() == reflect.Pointer {
		base = base.Elem()
		pointer = true
	}
	if info, ok := typemodel.LookupNamedScalarPoolInfo(base); ok {
		return info.BareName, pointer
	}
	if sentinel := bareSentinelName(base); sentinel != "" {
		return sentinel, pointer
	}
	if vm != nil && vm.rootFunction != nil && vm.rootFunction.TypeNames != nil {
		if registered := vm.rootFunction.TypeNames[base]; registered != "" {
			return registered, pointer
		}
	}
	return "", pointer
}

// pipitMethodSet lists the exported methods reflect would show for a value of the script
// type typeName: every exported value-receiver method, plus the pointer-receiver ones
// when the value is a pointer, sorted by name as reflect orders them.
//
// Takes vm (*VM) which owns the method table.
// Takes typeName (string) which is the script type's bare name.
// Takes pointer (bool) which is true for the pointer type's method set.
//
// Returns []pipitMethodEntry which is the sorted method set.
func pipitMethodSet(vm *VM, typeName string, pointer bool) []pipitMethodEntry {
	if vm == nil || vm.rootFunction == nil {
		return nil
	}
	prefix := typeName + "."
	var set []pipitMethodEntry
	for key := range vm.rootFunction.MethodTable() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		methodName := key[len(prefix):]
		if strings.Contains(methodName, ".") || !token.IsExported(methodName) {
			continue
		}
		callee, methodRoot, ok := resolvePipitMethodCallee(vm, typeName, methodName)
		if !ok || (callee.IsPointerReceiver && !pointer) {
			continue
		}
		set = append(set, pipitMethodEntry{callee: callee, methodRoot: methodRoot, name: methodName})
	}
	slices.SortFunc(set, func(a, b pipitMethodEntry) int { return strings.Compare(a.name, b.name) })
	return set
}

// pipitMethodInOutTypes returns a compiled method's parameter and result types, without
// the receiver, from its recorded signature, falling back to the erased shape.
//
// Takes callee (*program.CompiledFunction) which is the method.
//
// Returns []reflect.Type which holds the parameter types.
// Returns []reflect.Type which holds the result types.
func pipitMethodInOutTypes(callee *program.CompiledFunction) (in, out []reflect.Type) {
	if signature := callee.SignatureReflectType; signature != nil && signature.Kind() == reflect.Func {
		return slices.Collect(signature.Ins()), slices.Collect(signature.Outs())
	}
	in = make([]reflect.Type, max(len(callee.ParameterKinds)-1, 0))
	for i := range in {
		in[i] = reflect.TypeFor[any]()
	}
	if callee.IsVariadic && len(in) > 0 {
		in[len(in)-1] = reflect.TypeFor[[]any]()
	}
	out = make([]reflect.Type, len(callee.ResultKinds))
	for i, k := range callee.ResultKinds {
		out[i] = program.KindDefaultReflectType(k)
	}
	return in, out
}

// pipitReflectMethod builds the reflect.Method for entry as seen on receiverType: its
// Func takes the receiver as the first parameter and runs the compiled method.
//
// Takes vm (*VM) which runs the method.
// Takes entry (pipitMethodEntry) which is the method.
// Takes receiverType (reflect.Type) which is the type reflect was asked about.
// Takes index (int) which is the method's index in the method set.
//
// Returns reflect.Method which is the descriptor.
func pipitReflectMethod(vm *VM, entry pipitMethodEntry, receiverType reflect.Type, index int) reflect.Method {
	if named, ok := receiverType.(pipitNamedType); ok {
		receiverType = named.Type
	}
	in, out := pipitMethodInOutTypes(entry.callee)
	withReceiver := append([]reflect.Type{receiverType}, in...)
	functionType := reflect.FuncOf(entry.callee.VariadicSafeInTypes(withReceiver), out, entry.callee.IsVariadic)
	callee, methodRoot := entry.callee, entry.methodRoot
	function := reflect.MakeFunc(functionType, func(arguments []reflect.Value) []reflect.Value {
		bound := newCrossPackageBoundMethod(vm, methodRoot, callee)
		receiver := receiverValueFor(callee, arguments[0])
		results := bound.invoke(receiver, unwrapInterfaceArguments(arguments[1:]), identityArg)
		return shapeBoundMethodResults(results, out)
	})
	return reflect.Method{Name: entry.name, PkgPath: "", Type: functionType, Func: function, Index: index}
}

// pipitBoundMethodValue builds the func value reflect.Value.Method returns: the compiled
// method bound to receiver.
//
// Takes vm (*VM) which runs the method.
// Takes entry (pipitMethodEntry) which is the method.
// Takes receiver (reflect.Value) which is the bound receiver.
//
// Returns reflect.Value which is the callable.
func pipitBoundMethodValue(vm *VM, entry pipitMethodEntry, receiver reflect.Value) reflect.Value {
	in, out := pipitMethodInOutTypes(entry.callee)
	functionType := reflect.FuncOf(entry.callee.VariadicSafeInTypes(in), out, entry.callee.IsVariadic)
	callee, methodRoot := entry.callee, entry.methodRoot
	return reflect.MakeFunc(functionType, func(arguments []reflect.Value) []reflect.Value {
		bound := newCrossPackageBoundMethod(vm, methodRoot, callee)
		boundReceiver := receiverValueFor(callee, receiver)
		results := bound.invoke(boundReceiver, unwrapInterfaceArguments(arguments), identityArg)
		return shapeBoundMethodResults(results, out)
	})
}

// interceptReflectTypeMethods handles NumMethod, Method and MethodByName on a
// reflect.Type that describes a script type.
//
// Takes vm (*VM) which owns the method table.
// Takes registers (*Registers) which receive the results.
// Takes site (*program.CallSite) which describes the call.
// Takes rt (reflect.Type) which is the receiver.
// Takes methodName (string) which is the reflect.Type method being called.
//
// Returns OpResult and true when handled.
func interceptReflectTypeMethods(vm *VM, registers *Registers, site *program.CallSite, rt reflect.Type, methodName string) (OpResult, bool) {
	var arguments []reflect.Value
	if len(site.Arguments) > 1 {
		arguments = make([]reflect.Value, 0, len(site.Arguments)-1)
		for _, location := range site.Arguments[1:] {
			arguments = append(arguments, registerToReflectValue(vm.Arena, registers, location.Kind, location.Register))
		}
	}
	results, panicMessage, handled := pipitReflectTypeMethodCall(vm, rt, methodName, arguments)
	if !handled {
		return opContinue, false
	}
	if panicMessage != "" {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("%s", panicMessage)), true
	}
	storeReflectResults(registers, site.Returns, results)
	return opContinue, true
}

// pipitReflectTypeMethodCall evaluates NumMethod, Method or MethodByName on a
// reflect.Type that describes a script type, whether the call arrives as a method call or
// as a method expression.
//
// Takes vm (*VM) which owns the method table.
// Takes rt (reflect.Type) which is the receiver.
// Takes methodName (string) which is the reflect.Type method.
// Takes arguments ([]reflect.Value) which are the call's arguments after the receiver.
//
// Returns results ([]reflect.Value) which are the method's results when handled.
// Returns panicMessage (string) which is non-empty when the call must panic.
// Returns handled (bool) which is false when the receiver is not a script type or the
// arguments do not fit.
func pipitReflectTypeMethodCall(vm *VM, rt reflect.Type, methodName string, arguments []reflect.Value) (results []reflect.Value, panicMessage string, handled bool) {
	typeName, pointer := pipitReflectTypeName(vm, rt)
	if typeName == "" {
		return nil, "", false
	}
	set := pipitMethodSet(vm, typeName, pointer)
	switch methodName {
	case "NumMethod":
		return []reflect.Value{reflect.ValueOf(len(set))}, "", true
	case "Method":
		return pipitReflectMethodAt(vm, rt, set, arguments)
	case "MethodByName":
		return pipitReflectMethodNamed(vm, rt, set, arguments)
	default:
		return nil, "", false
	}
}

// pipitReflectMethodAt answers reflect.Type.Method(i) over a script type's sorted method
// set, with Go's out-of-range panic.
//
// Takes vm (*VM) which owns the method tables.
// Takes rt (reflect.Type) which is the receiver type.
// Takes set ([]pipitMethodEntry) which is the sorted method set.
// Takes arguments ([]reflect.Value) which carry the index.
//
// Returns the reflect.Method result, a panic message, and whether the call was handled.
func pipitReflectMethodAt(vm *VM, rt reflect.Type, set []pipitMethodEntry, arguments []reflect.Value) ([]reflect.Value, string, bool) {
	if len(arguments) < 1 || !arguments[0].IsValid() || !arguments[0].CanInt() {
		return nil, "", false
	}
	index := int(arguments[0].Int())
	if index < 0 || index >= len(set) {
		return nil, reflectMethodOutOfRange, true
	}
	return []reflect.Value{reflect.ValueOf(pipitReflectMethod(vm, set[index], rt, index))}, "", true
}

// pipitReflectMethodNamed answers reflect.Type.MethodByName over a script type's sorted
// method set.
//
// Takes vm (*VM) which owns the method tables.
// Takes rt (reflect.Type) which is the receiver type.
// Takes set ([]pipitMethodEntry) which is the sorted method set.
// Takes arguments ([]reflect.Value) which carry the name.
//
// Returns the reflect.Method and found results, an empty panic message, and whether the
// call was handled.
func pipitReflectMethodNamed(vm *VM, rt reflect.Type, set []pipitMethodEntry, arguments []reflect.Value) ([]reflect.Value, string, bool) {
	if len(arguments) < 1 || !arguments[0].IsValid() || arguments[0].Kind() != reflect.String {
		return nil, "", false
	}
	name := arguments[0].String()
	for index, entry := range set {
		if entry.name == name {
			return []reflect.Value{reflect.ValueOf(pipitReflectMethod(vm, entry, rt, index)), reflect.ValueOf(true)}, "", true
		}
	}
	return []reflect.Value{reflect.ValueOf(reflect.Method{}), reflect.ValueOf(false)}, "", true
}

// interceptReflectValueMethodIndex handles reflect.Value.Method(i) on a script value.
//
// Takes vm (*VM) which owns the method table.
// Takes registers (*Registers) which receive the result.
// Takes site (*program.CallSite) which describes the call.
// Takes inner (reflect.Value) which is the script value.
// Takes typeName (string) which is its bare type name.
//
// Returns OpResult and true when handled.
func interceptReflectValueMethodIndex(vm *VM, registers *Registers, site *program.CallSite, inner reflect.Value, typeName string) (OpResult, bool) {
	index, ok := intArgument(vm, registers, site, 1)
	if !ok {
		return opContinue, false
	}
	set := pipitMethodSet(vm, typeName, inner.Kind() == reflect.Pointer)
	if index < 0 || index >= len(set) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("%s", reflectMethodOutOfRange)), true
	}
	storeReflectResults(registers, site.Returns, []reflect.Value{reflect.ValueOf(pipitBoundMethodValue(vm, set[index], inner))})
	return opContinue, true
}

// intArgument reads the call's argument at position as an int.
//
// Takes vm (*VM) which owns the arena.
// Takes registers (*Registers) which hold the arguments.
// Takes site (*program.CallSite) which describes the call.
// Takes position (int) which is the argument index.
//
// Returns the int and true, or false when the argument is absent or not an integer.
func intArgument(vm *VM, registers *Registers, site *program.CallSite, position int) (int, bool) {
	if position >= len(site.Arguments) {
		return 0, false
	}
	argument := registerToReflectValue(vm.Arena, registers, site.Arguments[position].Kind, site.Arguments[position].Register)
	if !argument.IsValid() || !argument.CanInt() {
		return 0, false
	}
	return int(argument.Int()), true
}

// renderPipitTypeString renders a reflect type that involves script types the way Go
// prints the source type: `main.T`, `*main.T`, `[]main.T`, `func(main.T, int) int`.
//
// Takes rt (reflect.Type) which is the type to render.
//
// Returns string which is the rendering, or "" when rt involves no script type.
func renderPipitTypeString(rt reflect.Type) string {
	if rt == nil {
		return ""
	}
	if named, ok := rt.(pipitNamedType); ok {
		if named.sourcePackage != "" {
			return named.sourcePackage + "." + named.sourceName
		}
		return named.sourceName
	}
	if info, ok := typemodel.LookupNamedScalarPoolInfo(rt); ok {
		return info.QualifiedName
	}
	if rt.Kind() == reflect.Struct {
		if isPipitSynthesisedReflectType(rt) {
			return qualifiedNameFromStructSentinel(rt)
		}
		return ""
	}
	return renderPipitCompositeTypeString(rt)
}

// renderPipitCompositeTypeString renders a pointer, slice, array, map, channel or func
// type whose components include a script type; anything else renders empty.
//
// Takes rt (reflect.Type) which is the composite type.
//
// Returns string which is the rendered type, or "".
func renderPipitCompositeTypeString(rt reflect.Type) string {
	switch rt.Kind() {
	case reflect.Pointer:
		return renderPipitElemTypeString("*", rt.Elem())
	case reflect.Slice:
		return renderPipitElemTypeString("[]", rt.Elem())
	case reflect.Array:
		return renderPipitElemTypeString("["+strconv.Itoa(rt.Len())+"]", rt.Elem())
	case reflect.Chan:
		return renderPipitElemTypeString(rt.ChanDir().String()+" ", rt.Elem())
	case reflect.Map:
		return renderPipitMapTypeString(rt)
	case reflect.Func:
		return renderPipitFuncTypeString(rt)
	default:
		return ""
	}
}

// renderPipitElemTypeString prefixes the rendering of elem, when it has one.
//
// Takes prefix (string) which is the composite's own notation.
// Takes elem (reflect.Type) which is the element type.
//
// Returns string which is the rendered type, or "".
func renderPipitElemTypeString(prefix string, elem reflect.Type) string {
	if inner := renderPipitTypeString(elem); inner != "" {
		return prefix + inner
	}
	return ""
}

// renderPipitMapTypeString renders a map type when its key or value is a script type,
// falling back to reflect's rendering for the other component.
//
// Takes rt (reflect.Type) which is the map type.
//
// Returns string which is the rendered type, or "".
func renderPipitMapTypeString(rt reflect.Type) string {
	key, elem := renderPipitTypeString(rt.Key()), renderPipitTypeString(rt.Elem())
	if key == "" && elem == "" {
		return ""
	}
	if key == "" {
		key = rt.Key().String()
	}
	if elem == "" {
		elem = rt.Elem().String()
	}
	return "map[" + key + "]" + elem
}

// renderPipitFuncTypeString renders a func type whose parameters or results involve
// script types, or "" when none do.
//
// Takes rt (reflect.Type) which is a func type.
//
// Returns string which is the rendering or "".
func renderPipitFuncTypeString(rt reflect.Type) string {
	involved := false
	parts := make([]string, 0, rt.NumIn())
	for i := range rt.NumIn() {
		in := rt.In(i)
		rendered := renderPipitTypeString(in)
		if rendered == "" {
			rendered = in.String()
		} else {
			involved = true
		}
		if rt.IsVariadic() && i == rt.NumIn()-1 && in.Kind() == reflect.Slice {
			elem := renderPipitTypeString(in.Elem())
			if elem == "" {
				elem = in.Elem().String()
			}
			rendered = "..." + elem
		}
		parts = append(parts, rendered)
	}
	outs := make([]string, 0, rt.NumOut())
	for out := range rt.Outs() {
		rendered := renderPipitTypeString(out)
		if rendered == "" {
			rendered = out.String()
		} else {
			involved = true
		}
		outs = append(outs, rendered)
	}
	if !involved {
		return ""
	}
	text := "func(" + strings.Join(parts, ", ") + ")"
	switch len(outs) {
	case 0:
	case 1:
		text += " " + outs[0]
	default:
		text += " (" + strings.Join(outs, ", ") + ")"
	}
	return text
}
