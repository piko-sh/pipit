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
	"container/heap"
	"encoding"
	"net/http"
	"reflect"
)

var (
	// TextMarshalerReflectType is the reflect.Type for encoding.TextMarshaler, cached for
	// the adapter whitelist check.
	TextMarshalerReflectType = reflect.TypeFor[encoding.TextMarshaler]()

	// TextUnmarshalerReflectType is the reflect.Type for encoding.TextUnmarshaler.
	TextUnmarshalerReflectType = reflect.TypeFor[encoding.TextUnmarshaler]()

	// HTTPHandlerReflectType is the reflect.Type for net/http.Handler.
	HTTPHandlerReflectType = reflect.TypeFor[http.Handler]()

	// HeapInterfaceReflectType is the reflect.Type for container/heap.Interface.
	HeapInterfaceReflectType = reflect.TypeFor[heap.Interface]()

	// anyReflectType is the reflect.Type for the empty interface, used to box heap elements
	// crossing the adapter boundary.
	anyReflectType = reflect.TypeFor[any]()

	_ encoding.TextMarshaler = (*pipitTextMarshalerAdapter)(nil)

	_ encoding.TextUnmarshaler = (*pipitTextUnmarshalerAdapter)(nil)

	_ http.Handler = (*pipitHTTPHandlerAdapter)(nil)

	_ heap.Interface = (*pipitHeapInterfaceAdapter)(nil)
)

// pipitTextMarshalerAdapter wraps a pipit value to satisfy encoding.TextMarshaler for a
// source-level `MarshalText() ([]byte, error)` method.
type pipitTextMarshalerAdapter struct {
	pipitAdapterCore
}

// MarshalText invokes the wrapped pipit value's MarshalText method.
//
// Returns the bytes and error reported by the pipit-side method.
func (a *pipitTextMarshalerAdapter) MarshalText() ([]byte, error) {
	results, err := a.invoke("text marshaler", nil)
	if err != nil {
		return nil, err
	}
	return extractBytesResult(results), extractErrorResult(results)
}

// pipitTextUnmarshalerAdapter wraps a pipit pointer value to satisfy
// encoding.TextUnmarshaler for a source-level `UnmarshalText([]byte) error` method.
type pipitTextUnmarshalerAdapter struct {
	pipitAdapterCore
}

// UnmarshalText invokes the wrapped pipit value's UnmarshalText method with the text.
//
// Takes text ([]byte) which is the input to decode.
//
// Returns the error reported by the pipit-side method.
func (a *pipitTextUnmarshalerAdapter) UnmarshalText(text []byte) error {
	results, err := a.invoke("text unmarshaler", []reflect.Value{reflect.ValueOf(text)})
	if err != nil {
		return err
	}
	return extractErrorResult(results)
}

// pipitHTTPHandlerAdapter wraps a pipit value to satisfy net/http.Handler for a
// source-level `ServeHTTP(http.ResponseWriter, *http.Request)` method. Both arguments are
// genuine native objects the interpreted body calls through the normal native-dispatch
// path.
type pipitHTTPHandlerAdapter struct {
	pipitAdapterCore
}

// ServeHTTP invokes the wrapped pipit value's ServeHTTP method.
//
// Takes w (http.ResponseWriter) which receives the response.
// Takes r (*http.Request) which is the request being served.
//
// Panics with the interpreted panic value when the pipit body panics outside an
// interpreted dispatch, matching how a native handler's panic reaches the server.
func (a *pipitHTTPHandlerAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	writer := reflect.New(reflect.TypeFor[http.ResponseWriter]()).Elem()
	if w != nil {
		writer.Set(reflect.ValueOf(w))
	}
	if _, err := a.invoke("http handler", []reflect.Value{writer, reflect.ValueOf(r)}); err != nil {
		panic(err)
	}
}

// pipitHeapInterfaceAdapter extends the sort adapter with Push and Pop to satisfy
// container/heap.Interface.
type pipitHeapInterfaceAdapter struct {
	*pipitSortInterfaceAdapter

	// push dispatches the source-level Push method.
	push pipitAdapterCore

	// pop dispatches the source-level Pop method.
	pop pipitAdapterCore
}

// Push invokes the wrapped pipit value's Push method with the element. The element is
// passed as its concrete value because the interpreter stores `any` values unboxed; only
// a nil element travels as a typed nil interface.
//
// Takes x (any) which is the element to add.
func (a *pipitHeapInterfaceAdapter) Push(x any) {
	element := reflect.New(anyReflectType).Elem()
	if x != nil {
		element = reflect.ValueOf(x)
	}
	_, _ = a.push.invoke("heap push", []reflect.Value{element})
}

// Pop invokes the wrapped pipit value's Pop method.
//
// Returns the element the pipit-side method removed, or nil when it returned nothing.
func (a *pipitHeapInterfaceAdapter) Pop() any {
	results, err := a.pop.invoke("heap pop", nil)
	if err != nil || len(results) == 0 || !results[0].IsValid() {
		return nil
	}
	if !results[0].CanInterface() {
		return nil
	}
	return results[0].Interface()
}

// buildTextMarshalerAdapterIfRegistered returns a pipitTextMarshalerAdapter wrapping
// argument when the type-name has a registered MarshalText method, or an invalid value
// otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing encoding.TextMarshaler, or an invalid
// reflect.Value.
func buildTextMarshalerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "MarshalText")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitTextMarshalerAdapter{pipitAdapterCore: core})
}

// buildTextUnmarshalerAdapterIfRegistered returns a pipitTextUnmarshalerAdapter wrapping
// argument when the type-name has a registered UnmarshalText method whose receiver kind
// admits argument, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing encoding.TextUnmarshaler, or an invalid
// reflect.Value.
func buildTextUnmarshalerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "UnmarshalText")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitTextUnmarshalerAdapter{pipitAdapterCore: core})
}

// buildHTTPHandlerAdapterIfRegistered returns a pipitHTTPHandlerAdapter wrapping argument
// when the type-name has a registered ServeHTTP method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing net/http.Handler, or an invalid
// reflect.Value.
func buildHTTPHandlerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "ServeHTTP")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitHTTPHandlerAdapter{pipitAdapterCore: core})
}

// buildHeapInterfaceAdapterIfRegistered composes the sort adapter with Push and Pop when
// all five heap methods are registered for the type-name in the same compilation root.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing container/heap.Interface, or an invalid
// reflect.Value.
func buildHeapInterfaceAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	sorter, sorterOK := reflect.TypeAssert[*pipitSortInterfaceAdapter](buildSortInterfaceAdapterIfRegistered(vm, argument, typeName))
	if !sorterOK {
		return reflect.Value{}
	}
	push, pushOK := lookupAdapterCore(vm, argument, typeName, "Push")
	pop, popOK := lookupAdapterCore(vm, argument, typeName, "Pop")
	if !pushOK || !popOK || push.methodRoot != sorter.methodRoot || pop.methodRoot != sorter.methodRoot {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitHeapInterfaceAdapter{pipitSortInterfaceAdapter: sorter, push: push, pop: pop})
}
