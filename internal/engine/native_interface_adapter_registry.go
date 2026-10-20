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
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// maxAdapterReentryDepth bounds how deep registered adapters may nest: each level is a
// native method call that runs an interpreted body on a child VM, which may call native
// code that calls the adapter again on the same receiver.
const maxAdapterReentryDepth = 512

var (
	// errAdapterReentryDepth reports that adapter re-entry exceeded maxAdapterReentryDepth.
	errAdapterReentryDepth = errors.New("interface adapter re-entry depth exceeded")

	// errAdapterMethodUnavailable reports that an adapter's method body can no longer be
	// found, so the call cannot run.
	errAdapterMethodUnavailable = errors.New("method unavailable")
)

// adapterUnwrapperReflectType is the reflect.Type for adapterUnwrapper.
var adapterUnwrapperReflectType = reflect.TypeFor[adapterUnwrapper]()

// adapterRegistry is the process-wide table of registered adapters, published
// copy-on-write so the lookup on the native-call path is one atomic load and a map read.
var adapterRegistry atomic.Pointer[map[reflect.Type]registeredAdapter]

// adapterRegistryMu serialises writers to adapterRegistry.
var adapterRegistryMu sync.Mutex

// adapterBuilder wraps an AdapterCore in a Go value implementing its registered
// interface.
type adapterBuilder func(core *AdapterCore) reflect.Value

// registeredAdapter is what the registry holds for one interface.
type registeredAdapter struct {
	// build constructs the adapter.
	build adapterBuilder

	// optional are interfaces whose methods the adapter calls only when the value declares
	// all of them; it checks core.has first.
	optional []reflect.Type
}

// adapterMethodRef locates one interpreted method body.
type adapterMethodRef struct {
	// root is the compiled root owning the method, or nil for the VM's own root.
	root *program.CompiledFunction

	// index is the method's slot in root's function table.
	index uint16
}

// AdapterCore is the engine side of a registered adapter: the interpreted value being
// wrapped and the methods the interface named, resolved once when the adapter was built.
type AdapterCore struct {
	// vm is the VM that was executing when the value crossed into native code.
	vm *VM

	// methods maps an interface method name to its interpreted body.
	methods map[string]adapterMethodRef

	// typeName is the value's source-level type name, for messages.
	typeName string

	// underlying is the interpreted value the adapter wraps.
	underlying reflect.Value
}

// Underlying returns the interpreted value the adapter wraps.
//
// Returns reflect.Value which is the wrapped value.
func (c *AdapterCore) Underlying() reflect.Value {
	return c.underlying
}

// Invoke runs the interpreted method named method with arguments and returns its results.
//
// The body runs on a child VM that shares the parent's global store and symbols, as every
// callback into interpreted code does. An interpreted panic is re-raised when an
// interpreted dispatch is still on the Go stack, so an interpreted defer/recover upstream
// can catch it; otherwise it is returned as an error.
//
// Takes method (string) which is the interface method name.
// Takes arguments ([]reflect.Value) which are the method's arguments in order; an
// interface-typed argument may carry its dynamic type.
//
// Returns []reflect.Value which are the method's results, one per declared result.
// Returns error when the method is unknown, re-entry is too deep, or the body failed.
//
// Panics when the interpreted body panics while an interpreted dispatch is still on the
// Go stack, so upstream defer/recover can catch it.
func (c *AdapterCore) Invoke(method string, arguments ...reflect.Value) (results []reflect.Value, err error) {
	ref, ok := c.methods[method]
	if !ok {
		return nil, fmt.Errorf("pipit adapter for %s: no method %s", c.typeName, method)
	}
	callee, root, ok := resolveAdapterCallee(c.vm, ref.root, ref.index)
	if !ok {
		return nil, fmt.Errorf("pipit adapter for %s: %w: %s", c.typeName, errAdapterMethodUnavailable, method)
	}
	globals := c.vm.Globals
	if globals != nil {
		if globals.adapterDepth.Add(1) > maxAdapterReentryDepth {
			globals.adapterDepth.Add(-1)
			return nil, fmt.Errorf("%w: %s.%s", errAdapterReentryDepth, c.typeName, method)
		}
		defer globals.adapterDepth.Add(-1)
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if globals != nil && globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		results = nil
		err = fmt.Errorf("pipit adapter for %s: %s panicked: %v", c.typeName, method, recovered)
	}()
	bound := newCrossPackageBoundMethod(c.vm, root, callee)
	receiver := receiverValueFor(callee, c.underlying)
	return bound.invokeResult(receiver, arguments, identityArg)
}

// InvokeRecordingError is Invoke for interface methods that declare no error result: a
// failure of the interpreted body is recorded on the calling VM, as the closed-set
// adapters do, and zero results come back so the native caller can continue.
//
// Takes method (string) which is the interface method name.
// Takes arguments ([]reflect.Value) which are the method's arguments in order.
//
// Returns []reflect.Value which are the method's results, zero-valued on failure.
func (c *AdapterCore) InvokeRecordingError(method string, arguments ...reflect.Value) []reflect.Value {
	results, err := c.Invoke(method, arguments...)
	if err == nil {
		return results
	}
	c.recordError(err)
	if ref, ok := c.methods[method]; ok {
		if callee, _, resolved := resolveAdapterCallee(c.vm, ref.root, ref.index); resolved {
			return reflectResults(nil, callee.ResultKinds)
		}
	}
	return nil
}

// ResultError converts an interpreted method result into a native error, using the
// error adapter when needed.
//
// Takes results ([]reflect.Value) which are the method's results.
// Takes index (int) which selects the error result.
//
// Returns error which is the result, or an error saying the result is not an error.
func (c *AdapterCore) ResultError(results []reflect.Value, index int) error {
	return resultError(c.vm, results, index)
}

// recordError records err on the calling VM, as the closed-set adapters do for a method
// that has no error result, unless an earlier error is already recorded.
//
// Takes err (error) which is the failure.
func (c *AdapterCore) recordError(err error) {
	if c.vm != nil && c.vm.evalError == nil {
		c.vm.evalError = err
	}
}

// adapterUnderlying returns the interpreted value the adapter wraps; an adapter that
// embeds *AdapterCore inherits it, which lets interpreted code see the value again when
// the adapter returns from native code.
//
// Returns reflect.Value which is the wrapped value.
func (c *AdapterCore) adapterUnderlying() reflect.Value {
	return c.underlying
}

// adapterCore returns the core itself; an adapter that embeds *AdapterCore inherits it,
// which marks the adapter as registered.
//
// Returns *AdapterCore which is the core.
func (c *AdapterCore) adapterCore() *AdapterCore {
	return c
}

// satisfies checks a declared method against the required signature, falling back to
// parameter and result counts when its shape is unavailable.
//
// Takes requirement (program.InterfaceMethodRequirement) which describes the method.
//
// Returns bool which is true when the method meets it.
func (c *AdapterCore) satisfies(requirement program.InterfaceMethodRequirement) bool {
	ref, ok := c.methods[requirement.Name]
	if !ok {
		return false
	}
	callee, root, resolved := resolveAdapterCallee(c.vm, ref.root, ref.index)
	if !resolved || callee == nil {
		return false
	}
	if shape, known := root.MethodSignatures[c.typeName+"."+requirement.Name]; known && requirement.Shape != "" {
		return program.SubstituteShapeTypeArgs(shape, sentinelTypeArgs(c.underlying.Type())) == requirement.Shape
	}
	return calleeArityMatches(callee, requirement.Params, requirement.Results)
}

// has reports whether method belongs to the adapter's main interface or an optional
// interface the value implements in full.
//
// Takes method (string) which is the method name.
//
// Returns bool which is true when the method can be invoked.
func (c *AdapterCore) has(method string) bool {
	_, ok := c.methods[method]
	return ok
}

// adaptResult converts results[index] to T, adapting interpreted values as needed. Nil
// becomes the zero T, and native values implementing T pass through unchanged.
//
// Takes method (string) which names the method, for the error.
// Takes results ([]reflect.Value) which are the method's results.
// Takes index (int) which selects the result.
//
// Returns T which is the result.
// Returns error when the result is not nil and cannot be used as T.
func (c *AdapterCore) adaptResult[T any](method string, results []reflect.Value, index int) (T, error) {
	var zero T
	value, present := presentResult(results, index)
	if !present {
		return zero, nil
	}
	if typed, ok := reflect.TypeAssert[T](value); ok {
		return typed, nil
	}
	target := reflect.TypeFor[T]()
	if adapted, ok := adaptForInterfaceSlot(c.vm, value, target); ok {
		if typed, isTarget := reflect.TypeAssert[T](adapted); isTarget {
			return typed, nil
		}
	}
	return zero, fmt.Errorf("pipit adapter for %s: %s returned %s, which does not implement %s",
		c.typeName, method, scriptTypeString(value.Type()), target)
}

// typedResult returns results[index] as T, for an adapter method whose result has a
// concrete type, such as the []string a GlobFS adapter's Glob returns.
//
// Takes method (string) which names the method, for the error.
// Takes results ([]reflect.Value) which are the method's results.
// Takes index (int) which selects the result.
//
// Returns T which is the result, or the zero T for a missing or nil result.
// Returns error when the result has another type.
func (c *AdapterCore) typedResult[T any](method string, results []reflect.Value, index int) (T, error) {
	var zero T
	value, present := presentResult(results, index)
	if !present {
		return zero, nil
	}
	if typed, ok := reflect.TypeAssert[T](value); ok {
		return typed, nil
	}
	return zero, fmt.Errorf("pipit adapter for %s: %s returned %s, not %s",
		c.typeName, method, scriptTypeString(value.Type()), reflect.TypeFor[T]())
}

// RegisterInterfaceAdapter makes interpreted values satisfy iface at native call sites.
// Registration is process-wide, and later registrations replace earlier ones.
//
// Registration is safe for concurrent use. registerAdapterFamily validates iface, build
// and the adapter's embedded *AdapterCore, panicking on invalid registrations.
//
// Takes iface (reflect.Type) which must be an interface type with at least one method.
// Takes build (adapterBuilder) which constructs the adapter; must not be nil. The
// adapter must embed *AdapterCore.
func RegisterInterfaceAdapter(iface reflect.Type, build adapterBuilder) {
	registerAdapterFamily([]reflect.Type{iface}, nil, build)
}

// registerAdapterFamily registers one builder for several interfaces whose adapter also
// dispatches the methods of optional when the value declares them.
//
// Takes members ([]reflect.Type) which are the interfaces the builder serves.
// Takes optional ([]reflect.Type) which are the interfaces resolved as well; each counts
// only when the value declares all of its methods.
// Takes build (adapterBuilder) which constructs the adapter; must not be nil.
//
// Panics when a member or optional interface is not an interface with methods, build is
// nil, or the adapter it builds does not embed *AdapterCore; build is called once with an
// empty core to check, so it must not call the core's methods.
//
// Concurrency: safe for concurrent use; acquires adapterRegistryMu.
func registerAdapterFamily(members, optional []reflect.Type, build adapterBuilder) {
	for _, iface := range slices.Concat(members, optional) {
		if !isInterfaceWithMethods(iface) {
			panic(fmt.Sprintf("engine.RegisterInterfaceAdapter: %v is not an interface with methods", iface))
		}
	}
	if build == nil {
		panic(fmt.Sprintf("engine.RegisterInterfaceAdapter: nil builder for %v", members))
	}
	if !buildsUnwrappableAdapter(build) {
		panic(fmt.Sprintf("engine.RegisterInterfaceAdapter: the adapter registered for %v must embed *AdapterCore", members))
	}
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	current := adapterRegistry.Load()
	next := make(map[reflect.Type]registeredAdapter, len(members))
	if current != nil {
		next = make(map[reflect.Type]registeredAdapter, len(*current)+len(members))
		maps.Copy(next, *current)
	}
	for _, iface := range members {
		next[iface] = registeredAdapter{build: build, optional: optional}
	}
	adapterRegistry.Store(&next)
}

// lookupRegisteredAdapter returns the adapter registered for iface.
//
// Takes iface (reflect.Type) which is the parameter's declared interface type.
//
// Returns registeredAdapter and true when one is registered.
func lookupRegisteredAdapter(iface reflect.Type) (registeredAdapter, bool) {
	current := adapterRegistry.Load()
	if current == nil {
		return registeredAdapter{}, false
	}
	registered, ok := (*current)[iface]
	return registered, ok
}

// registeredAdapterCore returns the core of a registered adapter.
//
// Takes value (reflect.Value) which may hold an adapter.
//
// Returns *AdapterCore and true when value is a registered adapter.
func registeredAdapterCore(value reflect.Value) (*AdapterCore, bool) {
	if !isInterfaceablePointer(value) {
		return nil, false
	}
	holder, ok := reflect.TypeAssert[interface{ adapterCore() *AdapterCore }](value)
	if !ok {
		return nil, false
	}
	return holder.adapterCore(), true
}

// buildRegisteredAdapter builds the adapter registered for expectedType, after checking
// that the interpreted type declares every method of the interface with the interface's
// shape and a receiver the argument can supply. The methods of each optional interface
// are resolved as well when the type declares all of them.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes expectedType (reflect.Type) which is the declared interface.
// Takes typeName (string) which is the source-level type name.
//
// Returns a value implementing expectedType, or an invalid reflect.Value when no adapter
// is registered or a method is missing, so the caller reports the argument as
// unsupported.
func buildRegisteredAdapter(vm *VM, argument reflect.Value, expectedType reflect.Type, typeName string) reflect.Value {
	registered, ok := lookupRegisteredAdapter(expectedType)
	if !ok {
		return reflect.Value{}
	}
	methods := make(map[string]adapterMethodRef, expectedType.NumMethod())
	for method := range expectedType.Methods() {
		root, index, found := lookupAdapterMethod(vm, typeName+"."+method.Name)
		if !found {
			return reflect.Value{}
		}
		if !adapterMethodHasShape(vm, root, index, method.Type.NumIn(), method.Type.NumOut()) {
			return reflect.Value{}
		}
		if !methodReceiverSatisfiesValueIn(root, vm, index, argument) {
			return reflect.Value{}
		}
		methods[method.Name] = adapterMethodRef{root: root, index: index}
	}
	for _, iface := range registered.optional {
		resolveOptionalMethods(vm, argument, typeName, iface, methods)
	}
	adapter := registered.build(&AdapterCore{vm: vm, underlying: argument, typeName: typeName, methods: methods})
	if !adapter.IsValid() || !adapter.Type().Implements(expectedType) || !adapter.Type().Implements(adapterUnwrapperReflectType) {
		return reflect.Value{}
	}
	return adapter
}

// isInterfaceWithMethods reports whether iface is an interface type an adapter can serve.
//
// Takes iface (reflect.Type) which may be nil.
//
// Returns bool which is true for an interface with at least one method.
func isInterfaceWithMethods(iface reflect.Type) bool {
	return iface != nil && iface.Kind() == reflect.Interface && iface.NumMethod() > 0
}

// buildsUnwrappableAdapter reports whether build returns an adapter that embeds
// *AdapterCore, by building one for an empty core.
//
// Takes build (adapterBuilder) which is the builder to check.
//
// Returns bool which is true when the adapter can be unwrapped.
func buildsUnwrappableAdapter(build adapterBuilder) bool {
	probe := build(&AdapterCore{vm: nil, methods: nil, typeName: "", underlying: reflect.Value{}})
	return probe.IsValid() && probe.Type().Implements(adapterUnwrapperReflectType)
}

// resolveOptionalMethods adds the methods of iface to methods when typeName declares all
// of them. Static typing does not check these methods, so each must also agree with the
// interface on the register bank of every scalar parameter and result.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
// Takes iface (reflect.Type) which is the optional interface.
// Takes methods (map[string]adapterMethodRef) which receives the methods; it is left
// unchanged when one is missing.
func resolveOptionalMethods(vm *VM, argument reflect.Value, typeName string, iface reflect.Type, methods map[string]adapterMethodRef) {
	resolved := make(map[string]adapterMethodRef, iface.NumMethod())
	for method := range iface.Methods() {
		if _, done := methods[method.Name]; done {
			continue
		}
		root, index, found := lookupAdapterMethod(vm, typeName+"."+method.Name)
		if !found || !adapterMethodHasSignature(vm, root, index, method.Type) ||
			!methodReceiverSatisfiesValueIn(root, vm, index, argument) {
			return
		}
		resolved[method.Name] = adapterMethodRef{root: root, index: index}
	}
	maps.Copy(methods, resolved)
}

// adapterMethodHasSignature checks parameter and result counts and scalar register
// banks against an interface signature. When a static Go type was recorded, all types
// must match exactly.
//
// Takes vm (*VM) which resolves the method.
// Takes methodRoot (*program.CompiledFunction) which owns the method; nil for the VM's
// own root.
// Takes methodIndex (uint16) which selects the method.
// Takes signature (reflect.Type) which is the interface method's func type.
//
// Returns bool which is true when the method can stand for the interface method.
func adapterMethodHasSignature(vm *VM, methodRoot *program.CompiledFunction, methodIndex uint16, signature reflect.Type) bool {
	callee, _, ok := resolveAdapterCallee(vm, methodRoot, methodIndex)
	if !ok || callee == nil {
		return false
	}
	parameters := callee.ParameterKinds
	if callee.HasReceiver && len(parameters) > 0 {
		parameters = parameters[1:]
	}
	if !calleeArityMatches(callee, signature.NumIn(), signature.NumOut()) || !recordedSignatureIs(callee, signature) {
		return false
	}
	for index, kind := range parameters {
		if kind != scalarBankOrGeneral(kind, signature.In(index)) {
			return false
		}
	}
	for index, kind := range callee.ResultKinds {
		if kind != scalarBankOrGeneral(kind, signature.Out(index)) {
			return false
		}
	}
	return true
}

// scalarBankOrGeneral returns the scalar register bank values of t travel in. For a type
// no scalar bank carries it returns kind itself when kind is not a scalar bank, so any
// general or typed-slice bank agrees, and the general bank otherwise.
//
// Takes kind (isa.RegisterKind) which is the bank the interpreted method uses.
// Takes t (reflect.Type) which is the interface method's parameter or result type.
//
// Returns isa.RegisterKind which equals kind when the two agree.
func scalarBankOrGeneral(kind isa.RegisterKind, t reflect.Type) isa.RegisterKind {
	switch t.Kind() {
	case reflect.String:
		return isa.RegisterString
	case reflect.Complex64, reflect.Complex128:
		return isa.RegisterComplex
	default:
	}
	if bank := tinyLeafScalarFieldBank(t.Kind()); bank != isa.RegisterGeneral {
		return bank
	}
	switch kind {
	case isa.RegisterInt, isa.RegisterUint, isa.RegisterFloat, isa.RegisterBool, isa.RegisterString, isa.RegisterComplex:
		return isa.RegisterGeneral
	default:
		return kind
	}
}

// presentResult returns results[index] with an interface unwrapped to its dynamic value.
//
// Takes results ([]reflect.Value) which are a method's results.
// Takes index (int) which selects the result.
//
// Returns reflect.Value which is the result.
// Returns bool which is false when the result is missing, invalid or a nil interface.
func presentResult(results []reflect.Value, index int) (reflect.Value, bool) {
	if index >= len(results) || !results[index].IsValid() {
		return reflect.Value{}, false
	}
	value := results[index]
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, true
}

// recordedSignatureIs reports whether a compiled method's recorded static type has
// exactly the parameter and result types of signature. A method compiled without a
// recorded type is not judged here.
//
// Takes callee (*program.CompiledFunction) which is the method.
// Takes signature (reflect.Type) which is the interface method's func type.
//
// Returns bool which is false only when the recorded type differs.
func recordedSignatureIs(callee *program.CompiledFunction, signature reflect.Type) bool {
	declared := callee.SignatureReflectType
	if declared == nil || declared.Kind() != reflect.Func {
		return true
	}
	return declared.IsVariadic() == signature.IsVariadic() &&
		slices.Equal(slices.Collect(declared.Ins()), slices.Collect(signature.Ins())) &&
		slices.Equal(slices.Collect(declared.Outs()), slices.Collect(signature.Outs()))
}

// calleeArityMatches reports whether a compiled function takes params parameters, its
// receiver not counted, and returns results values; a negative count is not checked.
//
// Takes callee (*program.CompiledFunction) which is the function.
// Takes params (int) which is the parameter count, or -1.
// Takes results (int) which is the result count, or -1.
//
// Returns bool which is true when the counts agree.
func calleeArityMatches(callee *program.CompiledFunction, params, results int) bool {
	if params < 0 || results < 0 {
		return true
	}
	parameterCount := len(callee.ParameterKinds)
	if callee.HasReceiver {
		parameterCount--
	}
	return parameterCount == params && len(callee.ResultKinds) == results
}
