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
	"fmt"
	"io"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
)

var (
	// IOCloserReflectType is the reflect.Type for io.Closer, cached for the adapter
	// whitelist check.
	IOCloserReflectType = reflect.TypeFor[io.Closer]()

	// IOReadCloserReflectType is the reflect.Type for io.ReadCloser.
	IOReadCloserReflectType = reflect.TypeFor[io.ReadCloser]()

	// IOWriteCloserReflectType is the reflect.Type for io.WriteCloser.
	IOWriteCloserReflectType = reflect.TypeFor[io.WriteCloser]()

	// IoReadWriterReflectType is the reflect.Type for io.ReadWriter.
	IoReadWriterReflectType = reflect.TypeFor[io.ReadWriter]()

	// IoReaderFromReflectType is the reflect.Type for io.ReaderFrom.
	IoReaderFromReflectType = reflect.TypeFor[io.ReaderFrom]()

	// IoWriterToReflectType is the reflect.Type for io.WriterTo.
	IoWriterToReflectType = reflect.TypeFor[io.WriterTo]()

	_ io.ReadCloser = (*pipitReadCloserAdapter)(nil)

	_ io.WriteCloser = (*pipitWriteCloserAdapter)(nil)

	_ io.ReadWriter = (*pipitReadWriterAdapter)(nil)

	_ io.ReaderFrom = (*pipitReaderFromAdapter)(nil)

	_ io.WriterTo = (*pipitWriterToAdapter)(nil)

	_ io.Closer = (*pipitCloserAdapter)(nil)
)

// pipitAdapterCore carries the state every single-method adapter needs to dispatch one
// source-level method on a wrapped pipit value, with the panic containment the other
// adapters share.
type pipitAdapterCore struct {
	// vm is the parent virtual machine used to dispatch the source method.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type the method belongs to.
	underlying reflect.Value

	// methodIndex is the index of the compiled method inside methodRoot (or vm.RootFunction
	// when methodRoot is nil).
	methodIndex uint16
}

// invoke dispatches the wrapped value's method through boundMethodVM.
//
// Takes label (string) which names the adapter in the panic-to-error message.
// Takes arguments ([]reflect.Value) which are the method arguments.
//
// Returns the method's reflect results and an error when the method is unavailable or its
// body panicked outside an interpreted dispatch.
//
// Panics when an interpreted panic occurs while the interpreter is still on the dispatch
// stack, re-raising so an upstream interpreted defer/recover can catch it.
func (c *pipitAdapterCore) invoke(label string, arguments []reflect.Value) (results []reflect.Value, err error) {
	callee, root, ok := resolveAdapterCallee(c.vm, c.methodRoot, c.methodIndex)
	if !ok {
		return nil, fmt.Errorf("pipit %s: method unavailable", label)
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if c.vm.Globals != nil && c.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		results = nil
		err = fmt.Errorf("pipit %s panicked: %v", label, recovered)
	}()
	bound := newCrossPackageBoundMethod(c.vm, root, callee)
	receiver := receiverValueFor(callee, c.underlying)
	return bound.invoke(receiver, arguments, identityArg), nil
}

// pipitCloserAdapter wraps a pipit value to satisfy io.Closer for a source-level `Close()
// error` method.
type pipitCloserAdapter struct {
	pipitAdapterCore
}

// Close invokes the wrapped pipit value's Close method.
//
// Returns the error returned by the pipit-side method.
func (a *pipitCloserAdapter) Close() error {
	results, err := a.invoke("closer", nil)
	if err != nil {
		return err
	}
	return extractErrorResult(results)
}

// pipitReadCloserAdapter combines the reader and closer adapters to satisfy
// io.ReadCloser.
type pipitReadCloserAdapter struct {
	*pipitReaderAdapter

	*pipitCloserAdapter
}

// pipitWriteCloserAdapter combines the writer and closer adapters to satisfy
// io.WriteCloser.
type pipitWriteCloserAdapter struct {
	*pipitWriterAdapter

	*pipitCloserAdapter
}

// pipitReadWriterAdapter combines the reader and writer adapters to satisfy
// io.ReadWriter.
type pipitReadWriterAdapter struct {
	*pipitReaderAdapter

	*pipitWriterAdapter
}

// pipitReaderFromAdapter wraps a pipit value to satisfy io.ReaderFrom for a source-level
// `ReadFrom(io.Reader) (int64, error)` method.
type pipitReaderFromAdapter struct {
	pipitAdapterCore
}

// ReadFrom invokes the wrapped pipit value's ReadFrom method with the native reader.
//
// Takes r (io.Reader) which is the source the pipit method consumes.
//
// Returns the byte count and error reported by the pipit-side method.
func (a *pipitReaderFromAdapter) ReadFrom(r io.Reader) (int64, error) {
	results, err := a.invoke("reader-from", []reflect.Value{readerArgument(r)})
	if err != nil {
		return 0, err
	}
	return max(int64Result(results), 0), extractErrorResult(results)
}

// pipitWriterToAdapter wraps a pipit value to satisfy io.WriterTo for a source-level
// `WriteTo(io.Writer) (int64, error)` method.
type pipitWriterToAdapter struct {
	pipitAdapterCore
}

// WriteTo invokes the wrapped pipit value's WriteTo method with the native writer.
//
// Takes w (io.Writer) which is the destination the pipit method writes to.
//
// Returns the byte count and error reported by the pipit-side method.
func (a *pipitWriterToAdapter) WriteTo(w io.Writer) (int64, error) {
	results, err := a.invoke("writer-to", []reflect.Value{writerArgument(w)})
	if err != nil {
		return 0, err
	}
	return max(int64Result(results), 0), extractErrorResult(results)
}

// lookupAdapterCore resolves the named method for typeName and builds the dispatch core
// when the method exists and its receiver kind admits argument.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
// Takes methodName (string) which is the method to resolve.
//
// Returns the core and true, or a zero core and false when the method is not registered.
func lookupAdapterCore(vm *VM, argument reflect.Value, typeName, methodName string) (pipitAdapterCore, bool) {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+"."+methodName)
	if !ok {
		return pipitAdapterCore{}, false
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return pipitAdapterCore{}, false
	}
	return pipitAdapterCore{vm: vm, methodRoot: methodRoot, underlying: argument, methodIndex: methodIndex}, true
}

// int64Result reads the first result as an int64 count, or zero when absent.
//
// Takes results ([]reflect.Value) which are the method results.
//
// Returns the count.
func int64Result(results []reflect.Value) int64 {
	if len(results) == 0 || !results[0].IsValid() || !results[0].CanInt() {
		return 0
	}
	return results[0].Int()
}

// readerArgument boxes a native reader as an io.Reader-typed reflect.Value so a nil
// reader arrives as a typed nil interface rather than an invalid value.
//
// Takes r (io.Reader) which may be nil.
//
// Returns the reflect.Value to pass to the interpreted method.
func readerArgument(r io.Reader) reflect.Value {
	value := reflect.New(IOReaderReflectType).Elem()
	if r != nil {
		value.Set(reflect.ValueOf(r))
	}
	return value
}

// writerArgument boxes a native writer as an io.Writer-typed reflect.Value so a nil
// writer arrives as a typed nil interface rather than an invalid value.
//
// Takes w (io.Writer) which may be nil.
//
// Returns the reflect.Value to pass to the interpreted method.
func writerArgument(w io.Writer) reflect.Value {
	value := reflect.New(iOWriterReflectType).Elem()
	if w != nil {
		value.Set(reflect.ValueOf(w))
	}
	return value
}

// buildCloserAdapterIfRegistered returns a pipitCloserAdapter wrapping argument when the
// type-name has a registered Close method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.Closer, or an invalid reflect.Value.
func buildCloserAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "Close")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitCloserAdapter{pipitAdapterCore: core})
}

// buildReadCloserAdapterIfRegistered composes the reader and closer adapters when both
// Read and Close are registered for the type-name.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.ReadCloser, or an invalid
// reflect.Value.
func buildReadCloserAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	reader, readerOK := reflect.TypeAssert[*pipitReaderAdapter](buildReaderAdapterIfRegistered(vm, argument, typeName))
	closer, closerOK := reflect.TypeAssert[*pipitCloserAdapter](buildCloserAdapterIfRegistered(vm, argument, typeName))
	if !readerOK || !closerOK {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitReadCloserAdapter{pipitReaderAdapter: reader, pipitCloserAdapter: closer})
}

// buildWriteCloserAdapterIfRegistered composes the writer and closer adapters when both
// Write and Close are registered for the type-name.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.WriteCloser, or an invalid
// reflect.Value.
func buildWriteCloserAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	writer, writerOK := reflect.TypeAssert[*pipitWriterAdapter](buildWriterAdapterIfRegistered(vm, argument, typeName))
	closer, closerOK := reflect.TypeAssert[*pipitCloserAdapter](buildCloserAdapterIfRegistered(vm, argument, typeName))
	if !writerOK || !closerOK {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitWriteCloserAdapter{pipitWriterAdapter: writer, pipitCloserAdapter: closer})
}

// buildReadWriterAdapterIfRegistered composes the reader and writer adapters when both
// Read and Write are registered for the type-name.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.ReadWriter, or an invalid
// reflect.Value.
func buildReadWriterAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	reader, readerOK := reflect.TypeAssert[*pipitReaderAdapter](buildReaderAdapterIfRegistered(vm, argument, typeName))
	writer, writerOK := reflect.TypeAssert[*pipitWriterAdapter](buildWriterAdapterIfRegistered(vm, argument, typeName))
	if !readerOK || !writerOK {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitReadWriterAdapter{pipitReaderAdapter: reader, pipitWriterAdapter: writer})
}

// buildReaderFromAdapterIfRegistered returns a pipitReaderFromAdapter wrapping argument
// when the type-name has a registered ReadFrom method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.ReaderFrom, or an invalid
// reflect.Value.
func buildReaderFromAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "ReadFrom")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitReaderFromAdapter{pipitAdapterCore: core})
}

// buildWriterToAdapterIfRegistered returns a pipitWriterToAdapter wrapping argument when
// the type-name has a registered WriteTo method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing io.WriterTo, or an invalid reflect.Value.
func buildWriterToAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	core, ok := lookupAdapterCore(vm, argument, typeName, "WriteTo")
	if !ok {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitWriterToAdapter{pipitAdapterCore: core})
}

// unwrapExtendedAdapterUnderlying returns the reflect.Value boxed inside one of the io,
// encoding, net/http or container/heap adapters, or v unchanged.
//
// Takes v (reflect.Value) which is a non-nil pointer value.
//
// Returns the unwrapped reflect.Value.
func unwrapExtendedAdapterUnderlying(v reflect.Value) reflect.Value {
	switch a := v.Interface().(type) {
	case *pipitCloserAdapter:
		return a.underlying
	case *pipitReadCloserAdapter:
		return a.underlying
	case *pipitWriteCloserAdapter:
		return a.underlying
	case *pipitReadWriterAdapter:
		return a.pipitReaderAdapter.underlying
	case *pipitReaderFromAdapter:
		return a.underlying
	case *pipitWriterToAdapter:
		return a.underlying
	case *pipitTextMarshalerAdapter:
		return a.underlying
	case *pipitTextUnmarshalerAdapter:
		return a.underlying
	case *pipitHTTPHandlerAdapter:
		return a.underlying
	case *pipitHeapInterfaceAdapter:
		return a.underlying
	}
	return v
}
