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
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
)

// maxAdapterReentryDepth bounds how deep registered adapters may nest: each level is a
// native method call that runs an interpreted body on a child VM, which may call native
// code that calls the adapter again on the same receiver.
const maxAdapterReentryDepth = 512

// errAdapterReentryDepth reports that adapter re-entry exceeded maxAdapterReentryDepth.
var errAdapterReentryDepth = errors.New("interface adapter re-entry depth exceeded")

// adapterBuilder wraps core in a Go value that implements the interface the builder was
// registered for. Each generated method packs its arguments, calls core.Invoke() and
// unpacks the results.
type adapterBuilder func(core *AdapterCore) reflect.Value

// adapterRegistry is the process-wide table of registered builders, published
// copy-on-write so the lookup on the native-call path is one atomic load and a map read.
var adapterRegistry atomic.Pointer[map[reflect.Type]adapterBuilder]

// adapterRegistryMu serialises writers to adapterRegistry.
var adapterRegistryMu sync.Mutex

// RegisterInterfaceAdapter makes interpreted values satisfy iface at native call sites.
//
// When a native parameter of type iface receives an interpreted value whose type declares
// every method of iface with a matching shape, the engine builds an AdapterCore for the
// value and hands it to build, and passes the returned value instead. Registration is
// process-wide and meant for init(); a later registration for the same interface replaces
// the earlier one.
//
// Takes iface (reflect.Type) which must be an interface type with at least one method.
// Takes build (adapterBuilder) which constructs the adapter; must not be nil.
//
// Panics when iface is not such an interface or build is nil, as these are programming
// errors at package initialisation.
//
// Concurrency: safe for concurrent use; acquires adapterRegistryMu.
func RegisterInterfaceAdapter(iface reflect.Type, build adapterBuilder) {
	if iface == nil || iface.Kind() != reflect.Interface || iface.NumMethod() == 0 {
		panic(fmt.Sprintf("engine.RegisterInterfaceAdapter: %v is not an interface with methods", iface))
	}
	if build == nil {
		panic("engine.RegisterInterfaceAdapter: nil builder for " + iface.String())
	}
	adapterRegistryMu.Lock()
	defer adapterRegistryMu.Unlock()
	current := adapterRegistry.Load()
	next := make(map[reflect.Type]adapterBuilder, 1)
	if current != nil {
		next = make(map[reflect.Type]adapterBuilder, len(*current)+1)
		maps.Copy(next, *current)
	}
	next[iface] = build
	adapterRegistry.Store(&next)
}

// registeredAdapterBuilder returns the builder registered for iface.
//
// Takes iface (reflect.Type) which is the parameter's declared interface type.
//
// Returns adapterBuilder and true when one is registered.
func registeredAdapterBuilder(iface reflect.Type) (adapterBuilder, bool) {
	current := adapterRegistry.Load()
	if current == nil {
		return nil, false
	}
	build, ok := (*current)[iface]
	return build, ok
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
		return nil, fmt.Errorf("pipit adapter for %s: method %s unavailable", c.typeName, method)
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
	if c.vm.evalError == nil {
		c.vm.evalError = err
	}
	if ref, ok := c.methods[method]; ok {
		if callee, _, resolved := resolveAdapterCallee(c.vm, ref.root, ref.index); resolved {
			return reflectResults(nil, callee.ResultKinds)
		}
	}
	return nil
}

// buildRegisteredAdapter builds the adapter a registered builder provides for
// expectedType, after checking that the interpreted type declares every method of the
// interface with the interface's shape and a receiver the argument can supply.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes expectedType (reflect.Type) which is the declared interface.
// Takes typeName (string) which is the source-level type name.
//
// Returns a value implementing expectedType, or an invalid reflect.Value when no builder
// is registered or a method is missing, so the caller reports the argument as
// unsupported.
func buildRegisteredAdapter(vm *VM, argument reflect.Value, expectedType reflect.Type, typeName string) reflect.Value {
	build, ok := registeredAdapterBuilder(expectedType)
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
	adapter := build(&AdapterCore{vm: vm, underlying: argument, typeName: typeName, methods: methods})
	if !adapter.IsValid() || !adapter.Type().Implements(expectedType) {
		return reflect.Value{}
	}
	return adapter
}
