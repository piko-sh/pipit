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
	"strings"

	"pipit.sh/pipit/internal/engine/program"
)

// promotedMethodCandidate is one method or field name met at one depth of a promoted
// method walk.
type promotedMethodCandidate struct {
	// entry is the method, unset for a field.
	entry pipitMethodEntry

	// method is true for a method and false for a field.
	method bool

	// inSet is true when the method belongs to the method set being listed.
	inSet bool
}

// promotedMethodLevel is one struct a promoted method walk reaches, with the embedded
// fields it was reached through.
type promotedMethodLevel struct {
	// structType is the struct reached.
	structType reflect.Type

	// path is the runtime indices of the embedded fields from the receiver's struct.
	path []int

	// addressable is true when the struct is reached through a pointer, so its
	// pointer-receiver methods are promoted into the set being listed.
	addressable bool
}

// promotedMethodWalk lists the methods promoted into a script struct type's method set
// from its embedded fields, by Go's selector rules.
type promotedMethodWalk struct {
	// vm owns the method tables.
	vm *VM

	// hidden holds the names met at shallower depths.
	hidden map[string]struct{}

	// visited holds the struct types already walked, so a type embedded in itself through a
	// pointer is walked once.
	visited map[reflect.Type]struct{}
}

// pipitReceiverMethodSet returns the method set reflect shows for receiverType, a script
// type or a pointer to one, sorted by name. For a struct type it includes the methods
// promoted from embedded fields along their embedding paths.
//
// Takes vm (*VM) which owns the method tables.
// Takes typeName (string) which is the script type's bare name.
// Takes receiverType (reflect.Type) which is the type reflect was asked about.
//
// Returns []pipitMethodEntry which is the sorted method set.
func pipitReceiverMethodSet(vm *VM, typeName string, receiverType reflect.Type) []pipitMethodEntry {
	pointer := receiverType.Kind() == reflect.Pointer
	own := pipitMethodSet(vm, typeName, pointer)
	structType := receiverType
	if pointer {
		structType = structType.Elem()
	}
	if named, ok := structType.(pipitNamedType); ok {
		structType = named.Type
	}
	if vm == nil || structType.Kind() != reflect.Struct {
		return own
	}
	walk := promotedMethodWalk{vm: vm, hidden: make(map[string]struct{}), visited: map[reflect.Type]struct{}{structType: {}}}
	for _, entry := range pipitMethodSet(vm, typeName, true) {
		walk.hidden[entry.name] = struct{}{}
	}
	promoted := walk.promoted(structType, pointer)
	if len(promoted) == 0 {
		return own
	}
	set := slices.Concat(own, promoted)
	slices.SortFunc(set, func(a, b pipitMethodEntry) int { return strings.Compare(a.name, b.name) })
	return set
}

// promoted walks structType's embedded fields depth by depth.
//
// Takes structType (reflect.Type) which is the receiver's struct type.
// Takes pointer (bool) which is true when the set listed is the pointer type's.
//
// Returns []pipitMethodEntry which are the promoted methods, unsorted.
func (w *promotedMethodWalk) promoted(structType reflect.Type, pointer bool) []pipitMethodEntry {
	for name := range structFieldNames(structType) {
		w.hidden[name] = struct{}{}
	}
	var result []pipitMethodEntry
	level := []promotedMethodLevel{{structType: structType, path: nil, addressable: pointer}}
	for depth := 0; depth < maxPromotedMethodDepth && len(level) > 0; depth++ {
		found := make(map[string][]promotedMethodCandidate)
		var next []promotedMethodLevel
		for _, current := range level {
			next = w.embedded(current, found, next)
		}
		result = w.promoteDepth(found, result)
		level = next
	}
	return result
}

// promoteDepth appends the methods one depth promotes to result and hides every name the
// depth declares from the depths below it. A name is promoted only when the depth
// declares it once, as a method in the set listed, and no shallower depth declared it.
//
// Takes found (map[string][]promotedMethodCandidate) which holds the depth's names.
// Takes result ([]pipitMethodEntry) which holds the methods promoted so far.
//
// Returns []pipitMethodEntry which is result with the depth's methods appended.
func (w *promotedMethodWalk) promoteDepth(found map[string][]promotedMethodCandidate, result []pipitMethodEntry) []pipitMethodEntry {
	for name, candidates := range found {
		if _, hidden := w.hidden[name]; hidden {
			continue
		}
		if len(candidates) == 1 && candidates[0].method && candidates[0].inSet {
			result = append(result, candidates[0].entry)
		}
	}
	for name := range found {
		w.hidden[name] = struct{}{}
	}
	return result
}

// embedded records the methods and fields of each embedded field of one struct in found
// and appends the embedded structs to walk at the next depth.
//
// Takes current (promotedMethodLevel) which is the struct whose fields are visited.
// Takes found (map[string][]promotedMethodCandidate) which collects this depth's names.
// Takes next ([]promotedMethodLevel) which collects the next depth's structs.
//
// Returns []promotedMethodLevel which is next with current's embedded structs added.
func (w *promotedMethodWalk) embedded(current promotedMethodLevel, found map[string][]promotedMethodCandidate, next []promotedMethodLevel) []promotedMethodLevel {
	for index := range embeddableFieldCount(current.structType) {
		field := current.structType.Field(index)
		if !program.IsAnonymousField(field) {
			continue
		}
		path := append(slices.Clip(current.path), index)
		base := field.Type
		addressable := current.addressable
		if base.Kind() == reflect.Pointer {
			base = base.Elem()
			addressable = true
		}
		for _, candidate := range w.declared(base, path, addressable) {
			found[candidate.entry.name] = append(found[candidate.entry.name], candidate)
		}
		if base.Kind() != reflect.Struct {
			continue
		}
		for name := range structFieldNames(base) {
			field := pipitMethodEntry{callee: nil, methodRoot: nil, embedded: nil, nativeType: nil, name: name, path: nil}
			found[name] = append(found[name], promotedMethodCandidate{entry: field, method: false, inSet: false})
		}
		if _, seen := w.visited[base]; !seen {
			w.visited[base] = struct{}{}
			next = append(next, promotedMethodLevel{structType: base, path: path, addressable: addressable})
		}
	}
	return next
}

// declared returns the exported methods the type of an embedded field declares, as
// candidates reached through path.
//
// Takes base (reflect.Type) which is the embedded field's type without its pointer.
// Takes path ([]int) which is the embedded field's index path.
// Takes addressable (bool) which is true when pointer-receiver methods are promoted.
//
// Returns []promotedMethodCandidate which are the methods.
func (w *promotedMethodWalk) declared(base reflect.Type, path []int, addressable bool) []promotedMethodCandidate {
	if typeName, _ := pipitReflectTypeName(w.vm, base); typeName != "" && (isPipitSynthesisedReflectType(base) || base.NumMethod() == 0) {
		set := pipitMethodSet(w.vm, typeName, true)
		candidates := make([]promotedMethodCandidate, 0, len(set))
		for _, entry := range set {
			entry.path = path
			entry.embedded = base
			candidates = append(candidates, promotedMethodCandidate{entry: entry, method: true, inSet: addressable || !entry.callee.IsPointerReceiver})
		}
		return candidates
	}
	return nativePromotedMethods(base, path, addressable)
}

// nativePromotedMethods returns the exported methods of an embedded native type or
// interface as candidates reached through path.
//
// Takes base (reflect.Type) which is the embedded field's type without its pointer.
// Takes path ([]int) which is the embedded field's index path.
// Takes addressable (bool) which is true when pointer-receiver methods are promoted.
//
// Returns []promotedMethodCandidate which are the methods.
func nativePromotedMethods(base reflect.Type, path []int, addressable bool) []promotedMethodCandidate {
	declaring := base
	if base.Kind() != reflect.Interface {
		declaring = reflect.PointerTo(base)
	}
	var candidates []promotedMethodCandidate
	for method := range declaring.Methods() {
		if !token.IsExported(method.Name) {
			continue
		}
		inSet := addressable || base.Kind() == reflect.Interface
		if !inSet {
			_, inSet = base.MethodByName(method.Name)
		}
		entry := pipitMethodEntry{callee: nil, methodRoot: nil, name: method.Name, path: path, embedded: base, nativeType: method.Type}
		candidates = append(candidates, promotedMethodCandidate{entry: entry, method: true, inSet: inSet})
	}
	return candidates
}

// structFieldNames yields the names of a struct's fields as the program declared them.
//
// Takes structType (reflect.Type) which is the struct.
//
// Returns func(func(string) bool) which yields each name.
func structFieldNames(structType reflect.Type) func(func(string) bool) {
	return func(yield func(string) bool) {
		for index := range embeddableFieldCount(structType) {
			if !yield(restorePipitStructField(structType.Field(index)).Name) {
				return
			}
		}
	}
}

// embeddableFieldCount returns how many of a struct's fields the program declared, which
// leaves out a script struct's identity sentinel.
//
// Takes structType (reflect.Type) which is the struct.
//
// Returns int which is the count.
func embeddableFieldCount(structType reflect.Type) int {
	if isPipitSynthesisedReflectType(structType) {
		return pipitUserFieldCount(structType)
	}
	return structType.NumField()
}

// promotedBoundMethod returns the method value reflect.Value.MethodByName gives for a
// method promoted into the receiver's method set.
//
// Takes vm (*VM) which owns the method tables.
// Takes receiver (reflect.Value) which is the script value.
// Takes typeName (string) which is its bare type name.
// Takes methodName (string) which is the method's name.
//
// Returns reflect.Value which is the method value, or the zero Value.
// Returns bool which is false when no method of that name is promoted.
func promotedBoundMethod(vm *VM, receiver reflect.Value, typeName, methodName string) (reflect.Value, bool) {
	entry, found := findPipitMethodEntry(pipitReceiverMethodSet(vm, typeName, receiver.Type()), methodName)
	if !found || entry.path == nil {
		return reflect.Value{}, false
	}
	return pipitBoundMethodValue(vm, entry, receiver), true
}

// lookupAdapterMethodOrPromoted finds the script method an adapter calls for methodName
// on argument, the type's own method first and then one promoted from an embedded field
// of a script type, as Go's method set has it.
//
// Takes vm (*VM) which owns the method tables.
// Takes argument (reflect.Value) which is the value the adapter wraps.
// Takes typeName (string) which is its script type name.
// Takes methodName (string) which is the method's name.
//
// Returns methodRoot (*program.CompiledFunction) which owns the method, nil for the VM's
// own root.
// Returns methodIndex (uint16) which is the method's slot in the root's function table.
// Returns path ([]int) which is the embedding path, nil for the type's own method.
// Returns ok (bool) which is false when the value has no such script method.
func lookupAdapterMethodOrPromoted(vm *VM, argument reflect.Value, typeName, methodName string) (methodRoot *program.CompiledFunction, methodIndex uint16, path []int, ok bool) {
	if root, index, found := lookupAdapterMethod(vm, typeName+"."+methodName); found {
		return root, index, nil, true
	}
	if vm == nil || !argument.IsValid() {
		return nil, 0, nil, false
	}
	entry, found := findPipitMethodEntry(pipitReceiverMethodSet(vm, typeName, argument.Type()), methodName)
	if !found || entry.path == nil || entry.callee == nil {
		return nil, 0, nil, false
	}
	embeddedName, _ := pipitReflectTypeName(vm, entry.embedded)
	root, index, found := lookupAdapterMethod(vm, embeddedName+"."+methodName)
	if !found {
		return nil, 0, nil, false
	}
	return root, index, entry.path, true
}

// promotedMethodReceiver follows a promoted method's embedding path from the receiver to
// the embedded value that declares the method, as Go's promotion wrapper does when the
// method is called.
//
// Takes receiver (reflect.Value) which is the outer receiver.
// Takes path ([]int) which is the embedding path.
//
// Returns reflect.Value which is the embedded value.
//
// Panics with Go's nil pointer dereference when the path crosses a nil pointer.
func promotedMethodReceiver(receiver reflect.Value, path []int) reflect.Value {
	value := receiver
	if value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}
	for _, index := range path {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				panic(newRuntimePanicError(nilDereferenceMessage))
			}
			value = value.Elem()
		}
		value = launderReadOnlyFieldValue(value.Field(index))
	}
	return value
}

// callPromotedMethod calls a promoted method on the outer receiver.
//
// Takes vm (*VM) which runs a script method.
// Takes entry (pipitMethodEntry) which is the promoted method.
// Takes receiver (reflect.Value) which is the outer receiver.
// Takes arguments ([]reflect.Value) which are the call's arguments.
// Takes out ([]reflect.Type) which are the method's result types.
//
// Returns []reflect.Value which are the results.
//
// Panics with Go's nil pointer dereference for a nil pointer or interface on the path,
// and with the fault.ErrNoMethodBodyForReceiver error when a generic method has no body
// for the embedded value's type arguments.
func callPromotedMethod(vm *VM, entry pipitMethodEntry, receiver reflect.Value, arguments []reflect.Value, out []reflect.Type) []reflect.Value {
	embedded := promotedMethodReceiver(receiver, entry.path)
	if entry.callee == nil {
		return callNativePromotedMethod(embedded, entry.name, arguments)
	}
	owner := entry.methodRoot
	if owner == nil {
		owner = vm.rootFunction
	}
	callee, err := receiverMethodBody(owner, embedded, entry.callee)
	if err != nil {
		panic(err)
	}
	bound := newCrossPackageBoundMethod(vm, entry.methodRoot, callee)
	results := bound.invoke(receiverValueFor(callee, embedded), unwrapInterfaceArguments(arguments), identityArg)
	return shapeBoundMethodResults(results, out)
}

// callNativePromotedMethod calls a method of an embedded native value or interface.
//
// Takes embedded (reflect.Value) which is the embedded value.
// Takes name (string) which is the method's name.
// Takes arguments ([]reflect.Value) which are the call's arguments.
//
// Returns []reflect.Value which are the results.
//
// Panics with Go's nil pointer dereference for a nil interface.
func callNativePromotedMethod(embedded reflect.Value, name string, arguments []reflect.Value) []reflect.Value {
	if embedded.Kind() == reflect.Interface && embedded.IsNil() {
		panic(newRuntimePanicError(nilDereferenceMessage))
	}
	method := embedded.MethodByName(name)
	if !method.IsValid() && embedded.CanAddr() {
		method = embedded.Addr().MethodByName(name)
	}
	if !method.IsValid() {
		panic(newInvariantError("promoted method %s not found on %s", name, embedded.Type()))
	}
	return method.Call(arguments)
}

// nativeMethodInOutTypes returns a native method's parameter and result types without the
// receiver.
//
// Takes methodType (reflect.Type) which is the function type of the method of a concrete
// type or an interface.
// Takes declaring (reflect.Type) which is the embedded type the method belongs to.
//
// Returns []reflect.Type which holds the parameter types.
// Returns []reflect.Type which holds the result types.
func nativeMethodInOutTypes(methodType reflect.Type, declaring reflect.Type) (in, out []reflect.Type) {
	in = slices.Collect(methodType.Ins())
	if declaring.Kind() != reflect.Interface && len(in) > 0 {
		in = in[1:]
	}
	return in, slices.Collect(methodType.Outs())
}
