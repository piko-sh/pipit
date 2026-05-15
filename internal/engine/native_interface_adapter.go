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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// pipitReaderMaxNoProgressCalls caps zero-byte no-error Read calls to bound a
	// pathological (0, nil) loop.
	pipitReaderMaxNoProgressCalls = 100

	// pipitReaderMaxCalls is the per-adapter overall ceiling on Read invocations. Set
	// generously so a legitimate large stream copied through a small buffer is never
	// truncated, while still capping a pathological progress-making infinite loop to bounded
	// CPU rather than a host freeze.
	pipitReaderMaxCalls = 100_000_000

	// pipitUnmarshalerMaxDepth caps recursive UnmarshalJSON entries to defend against
	// pathological re-entry.
	pipitUnmarshalerMaxDepth = 32
)

var (
	// errorsAsPointer caches the function pointer of stdlib errors.As so the native-call
	// dispatcher can recognise it by identity and route to pipitErrorsAs without scanning
	// function metadata. Resolved once at package init.
	errorsAsPointer = reflect.ValueOf(errors.As).Pointer()

	// errorsUnwrapPointer caches the function pointer of stdlib errors.Unwrap so
	// handleCallNativeReflect can route pipit-synthesised errors through pipitErrorsUnwrap
	// without scanning function metadata. Without this, calling errors.Unwrap on a pipit
	// *DBError triggers reflect: Call using *struct{...} as type error because the
	// synthesised type's reflect.Type has no method-set.
	errorsUnwrapPointer = reflect.ValueOf(errors.Unwrap).Pointer()

	// errorsIsPointer caches the function pointer of stdlib errors.Is for the same reason as
	// errorsAsPointer.
	errorsIsPointer = reflect.ValueOf(errors.Is).Pointer()

	// reflectTypeOfPointer caches reflect.TypeOf's function pointer so the dispatcher can
	// re-clothe the result in a pipitNamedType for named primitives.
	reflectTypeOfPointer = reflect.ValueOf(reflect.TypeOf).Pointer()

	// reflectTypeConstructorPointers caches the function pointers of the reflect
	// type-constructor family. These functions internally type-assert their reflect.Type
	// argument to the concrete *reflect.rtype and panic when handed a pipitNamedType
	// wrapper, so the wrapper must be unwrapped before the native call.
	reflectTypeConstructorPointers = map[uintptr]struct{}{
		reflect.ValueOf(reflect.PointerTo).Pointer(): {},
		reflect.ValueOf(reflect.SliceOf).Pointer():   {},
		reflect.ValueOf(reflect.ChanOf).Pointer():    {},
		reflect.ValueOf(reflect.MapOf).Pointer():     {},
		reflect.ValueOf(reflect.ArrayOf).Pointer():   {},
		reflect.ValueOf(reflect.FuncOf).Pointer():    {},
		reflect.ValueOf(reflect.New).Pointer():       {},
		reflect.ValueOf(reflect.Zero).Pointer():      {},
		reflect.ValueOf(reflect.MakeSlice).Pointer(): {},
		reflect.ValueOf(reflect.MakeChan).Pointer():  {},
		reflect.ValueOf(reflect.MakeMap).Pointer():   {},
	}

	// reflectIdentityFunctionPointers caches the reflect functions pointers that inspects a
	// value's identity rather than consuming it behaviourally.
	reflectIdentityFunctionPointers = map[uintptr]struct{}{
		reflect.ValueOf(reflect.TypeOf).Pointer():    {},
		reflect.ValueOf(reflect.ValueOf).Pointer():   {},
		reflect.ValueOf(reflect.DeepEqual).Pointer(): {},
		reflect.ValueOf(reflect.Indirect).Pointer():  {},
	}

	// reflectMakeFuncPointer caches reflect.MakeFunc's function pointer.
	reflectMakeFuncPointer = reflect.ValueOf(reflect.MakeFunc).Pointer()

	// runtimeGoexitPointer caches the function pointer of runtime.Goexit so the native-call
	// dispatcher can intercept it before it executes on the host goroutine. Calling the real
	// runtime.Goexit would terminate the host goroutine running the VM, violating the
	// isolation contract; instead the interpreter routes to its own goexit unwind machinery.
	runtimeGoexitPointer = reflect.ValueOf(runtime.Goexit).Pointer()

	// errorReflectType is the reflect.Type for the built-in error interface. Cached for
	// cheap comparisons in tryBuildInterfaceAdapter.
	errorReflectType = reflect.TypeFor[error]()

	// stringerReflectType is the reflect.Type for fmt.Stringer. Cached for cheap comparisons
	// in tryBuildInterfaceAdapter.
	stringerReflectType = reflect.TypeFor[fmt.Stringer]()

	// jsonMarshalerReflectType is the reflect.Type for json.Marshaler. Cached for cheap
	// comparisons in tryBuildInterfaceAdapter.
	jsonMarshalerReflectType = reflect.TypeFor[json.Marshaler]()

	// IOReaderReflectType is the reflect.Type for io.Reader. Used so pipit-synthesised
	// structs declaring a `Read([]byte) (int, error)` method can satisfy io.Reader at the
	// native call boundary (io.Copy, io.ReadAll, bufio.NewReader, etc.) despite their
	// reflect.Type carrying an empty method set.
	IOReaderReflectType = reflect.TypeFor[io.Reader]()

	// jsonUnmarshalerReflectType is the reflect.Type for json.Unmarshaler. Used so
	// pipit-synthesised pointer-receiver types declaring an `UnmarshalJSON([]byte) error`
	// method can satisfy json.Unmarshaler at the native call boundary (json.Unmarshal,
	// json.Decoder), with the adapter forwarding the JSON bytes to the source-level method
	// and surfacing its error.
	jsonUnmarshalerReflectType = reflect.TypeFor[json.Unmarshaler]()

	// iOWriterReflectType is the reflect.Type for io.Writer. Used so pipit-synthesised
	// pointer-receiver types declaring a `Write([]byte) (int, error)` method can satisfy
	// io.Writer at the native call boundary (fmt.Fprintf, io.Copy, bufio.NewWriter, etc.).
	iOWriterReflectType = reflect.TypeFor[io.Writer]()

	// wellKnownNamedInterfaceRegistry maps qualified names to types, consulted by
	// WellKnownNamedInterfaceReflectType.
	wellKnownNamedInterfaceRegistry = map[string]reflect.Type{
		"fmt.Stringer":              stringerReflectType,
		"io.Reader":                 IOReaderReflectType,
		"io.Writer":                 iOWriterReflectType,
		"encoding/json.Marshaler":   jsonMarshalerReflectType,
		"encoding/json.Unmarshaler": jsonUnmarshalerReflectType,
	}

	// fmtFormatterReflectType is the reflect.Type for fmt.Formatter, cached for
	// tryBuildInterfaceAdapter's whitelist check.
	fmtFormatterReflectType = reflect.TypeFor[fmt.Formatter]()

	// fmtScannerReflectType is the reflect.Type for fmt.Scanner, cached for
	// tryBuildInterfaceAdapter's whitelist check.
	fmtScannerReflectType = reflect.TypeFor[fmt.Scanner]()

	// sortInterfaceReflectType is the reflect.Type for sort.Interface, cached for
	// tryBuildInterfaceAdapter's whitelist check.
	sortInterfaceReflectType = reflect.TypeFor[sort.Interface]()
)

// pipitNamedType wraps reflect.Type to report a pipit source-level name. Overrides Name()
// and String() so named primitives like `type Score int` are not collapsed to "int".
type pipitNamedType struct {
	reflect.Type

	// sourceName is the pipit source-level bare type name ("Score").
	sourceName string

	// sourcePackage is the defining package's short name ("main").
	sourcePackage string
}

// Name returns the pipit source-level type name.
//
// Returns string which is the source-level name.
func (t pipitNamedType) Name() string { return t.sourceName }

// String returns the package-qualified source-level type name.
//
// Returns string which is the qualified rendering.
func (t pipitNamedType) String() string {
	if t.sourcePackage == "" {
		return t.sourceName
	}
	return t.sourcePackage + "." + t.sourceName
}

// pipitMarshalerAdapter mirrors pipitStringerAdapter for json.Marshaler: it wraps a
// pipit-synthesised value so encoding/json can dispatch to the source-level MarshalJSON
// method even though the synthesised reflect.Type has no MethodSet.
type pipitMarshalerAdapter struct {
	// vm is the parent virtual machine used to dispatch the source MarshalJSON method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the cross-package rootFunction that owns the method body.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the MarshalJSON method to.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled MarshalJSON method, cached at adapter construction.
	methodIndex uint16
}

// MarshalJSON invokes the wrapped value's MarshalJSON method.
//
// Interpreted-body panics are recovered and re-raised when the interpreter is still on
// the dispatch stack (dispatchDepth > 0) so an upstream interpreted defer/recover can
// catch them; otherwise they are surfaced as the returned error so the host stays alive.
//
// Returns the JSON bytes produced by the source-level method, or nil when no usable
// result is produced.
// Returns an error when the method body panics outside a pipit dispatch frame.
//
// Panics when an interpreted panic occurs while the interpreter is still on the dispatch
// stack, re-raising so an upstream interpreted defer/recover can catch it.
func (a *pipitMarshalerAdapter) MarshalJSON() (raw []byte, returnedError error) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return nil, nil
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		raw = nil
		returnedError = fmt.Errorf("pipit marshaler panicked: %v", recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	results := bound.invoke(receiver, nil, identityArg)
	if len(results) == 0 {
		return nil, nil
	}
	raw = extractBytesResult(results)
	returnedError = extractErrorResult(results)
	return raw, returnedError
}

// pipitStringerAdapter is the Stringer mirror of pipitErrorAdapter: it wraps a
// pipit-synthesised value so it satisfies fmt.Stringer even though the value's
// reflect.Type carries no String method.
type pipitStringerAdapter struct {
	// vm is the parent virtual machine used to dispatch the source String method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the cross-CompileProgram-batch rootFunction owning the method body
	// (nil for in-package targets).
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the String method to.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled String method, cached at adapter construction.
	methodIndex uint16
}

// String invokes the wrapped pipit value's String() method through the interpreter's
// bound-method dispatch. Returns an empty string when the bound method produces no usable
// result, matching pipitErrorAdapter for parity.
//
// Returns the string produced by the source-level method, or an empty string when no
// result is produced.
func (a *pipitStringerAdapter) String() string {
	return invokeStringReturnMethod(a.vm, a.methodRoot, a.methodIndex, a.underlying)
}

// adapterUnderlying returns the script value the adapter wraps.
//
// Returns reflect.Value which is the wrapped script value.
func (a *pipitStringerAdapter) adapterUnderlying() reflect.Value { return a.underlying }

// pipitErrorAdapter wraps a pipit-synthesised value so it satisfies the stdlib error
// interface. Supplies a real Go Error method that delegates back into the interpreter.
type pipitErrorAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Error() method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the Error() method to. Holds either the value or a pointer depending on the
	// receiver kind of the declared method.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled Error() method, cached at adapter construction so
	// each Error() call avoids a method-table lookup.
	methodIndex uint16
}

// Error invokes the wrapped pipit value's Error() method via the interpreter's
// bound-method dispatch path.
//
// Returns the error message produced by the wrapped value's Error() method.
func (a *pipitErrorAdapter) Error() string {
	return invokeStringReturnMethod(a.vm, a.methodRoot, a.methodIndex, a.underlying)
}

// adapterUnderlying returns the script value the adapter wraps.
//
// Returns reflect.Value which is the wrapped script value.
func (a *pipitErrorAdapter) adapterUnderlying() reflect.Value { return a.underlying }

// pipitWriterAdapter wraps a pipit value to satisfy io.Writer.
type pipitWriterAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Write method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the Write method to.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled Write method, cached at adapter construction.
	methodIndex uint16
}

// Write invokes the wrapped pipit value's Write method through the interpreter's
// bound-method dispatch.
//
// Takes p ([]byte) which is the source buffer Write should consume.
//
// Returns n (int) which is the number of bytes accepted by the pipit-side Write
// implementation.
// Returns err (error) which carries any error returned by the pipit-side method.
//
// Panics when an interpreted panic occurs while the interpreter is still on the dispatch
// stack, re-raising so an upstream interpreted defer/recover can catch it.
func (a *pipitWriterAdapter) Write(p []byte) (n int, err error) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return 0, errors.New("pipit writer: method unavailable")
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		n = 0
		err = fmt.Errorf("pipit writer panicked: %v", recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	results := bound.invoke(receiver, []reflect.Value{reflect.ValueOf(p)}, identityArg)
	if len(results) >= 1 && results[0].IsValid() {
		n = int(results[0].Int())
	}
	err = extractErrorResult(results)

	if n < 0 {
		n = 0
	}
	if n > len(p) {
		n = len(p)
	}
	if n < len(p) && err == nil {
		err = io.ErrShortWrite
	}
	return n, err
}

// pipitFormatterAdapter wraps a pipit value to satisfy fmt.Formatter.
type pipitFormatterAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Format method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type the Format method is
	// attached to.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled Format method.
	methodIndex uint16
}

// Format invokes the wrapped pipit value's Format method, forwarding fmt's State and
// verb. Panics from the interpreted method are contained unless raised inside an active
// native dispatch.
//
// Takes state (fmt.State) which fmt supplies for output + flags.
// Takes verb (rune) which is the format verb.
func (a *pipitFormatterAdapter) Format(state fmt.State, verb rune) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		_, _ = fmt.Fprintf(state, "%%!%c(pipit formatter panicked: %v)", verb, recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	bound.invoke(receiver, []reflect.Value{reflect.ValueOf(state), reflect.ValueOf(verb)}, identityArg)
}

// adapterUnderlying returns the script value the adapter wraps.
//
// Returns reflect.Value which is the wrapped script value.
func (a *pipitFormatterAdapter) adapterUnderlying() reflect.Value { return a.underlying }

// pipitScannerAdapter wraps a pipit pointer to satisfy fmt.Scanner.
type pipitScannerAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Scan method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type the Scan method is
	// attached to. Held as a pointer because Scan mutates the receiver.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled Scan method.
	methodIndex uint16
}

// Scan invokes the wrapped pipit value's Scan method.
//
// Forwards fmt's ScanState and verb and returns the interpreted method's error.
//
// Takes state (fmt.ScanState) which fmt supplies for token reading.
// Takes verb (rune) which is the scan verb.
//
// Returns error which is produced by the source-level Scan method.
//
// Panics when an interpreted panic occurs while the interpreter is still on the dispatch
// stack, re-raising so an upstream interpreted defer/recover can catch it.
func (a *pipitScannerAdapter) Scan(state fmt.ScanState, verb rune) (returnedError error) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return errors.New("pipit scanner: method unavailable")
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		returnedError = fmt.Errorf("pipit scanner panicked: %v", recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	results := bound.invoke(receiver, []reflect.Value{reflect.ValueOf(state), reflect.ValueOf(verb)}, identityArg)
	if len(results) >= 1 && results[0].IsValid() {
		if e, ok := reflect.TypeAssert[error](results[0]); ok && e != nil {
			return e
		}
	}
	return nil
}

// pipitSortInterfaceAdapter wraps a pipit-synthesised value (typically a named slice such
// as `type ByLen []string`) so it satisfies sort.Interface -
// `sort.Sort`/`sort.Stable`/`sort.IsSorted` then dispatch Len/Less/Swap back into the
// interpreter.
type pipitSortInterfaceAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Len/Less/Swap methods.
	vm *VM

	// methodRoot pins the rootFunction owning Len/Less/Swap when they live in a different
	// CompileProgram batch than vm.RootFunction. Nil for in-package types.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit value the sort methods are attached to.
	underlying reflect.Value

	// lenIndex is the index of the compiled Len method.
	lenIndex uint16

	// lessIndex is the index of the compiled Less method.
	lessIndex uint16

	// swapIndex is the index of the compiled Swap method.
	swapIndex uint16
}

// Len dispatches the source-level Len method.
//
// Returns int which is the slice length reported by the bound Len.
func (a *pipitSortInterfaceAdapter) Len() int {
	results := a.dispatch(a.lenIndex, nil)
	if len(results) >= 1 && results[0].IsValid() && results[0].CanInt() {
		return int(results[0].Int())
	}
	return 0
}

// Less dispatches the source-level Less method.
//
// Takes i (int) which is the first index to compare.
// Takes j (int) which is the second index to compare.
//
// Returns bool which is the comparison result from the bound Less.
func (a *pipitSortInterfaceAdapter) Less(i, j int) bool {
	results := a.dispatch(a.lessIndex, []reflect.Value{reflect.ValueOf(i), reflect.ValueOf(j)})
	if len(results) >= 1 && results[0].IsValid() && results[0].Kind() == reflect.Bool {
		return results[0].Bool()
	}
	return false
}

// Swap dispatches the source-level Swap method.
//
// Takes i (int) which is the first index to exchange.
// Takes j (int) which is the second index to exchange.
func (a *pipitSortInterfaceAdapter) Swap(i, j int) {
	a.dispatch(a.swapIndex, []reflect.Value{reflect.ValueOf(i), reflect.ValueOf(j)})
}

// dispatch invokes one of the wrapped value's sort methods through boundMethodVM, with
// the panic-containment boilerplate shared by the other pipit adapters.
//
// Takes methodIndex (uint16) which selects Len/Less/Swap.
// Takes arguments ([]reflect.Value) which are the method arguments.
//
// Returns the method's reflect results.
func (a *pipitSortInterfaceAdapter) dispatch(methodIndex uint16, arguments []reflect.Value) (results []reflect.Value) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, methodIndex)
	if !ok {
		return nil
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		results = nil
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	return bound.invoke(receiver, arguments, identityArg)
}

// pipitReaderAdapter wraps a pipit value to satisfy io.Reader.
type pipitReaderAdapter struct {
	// vm is the parent virtual machine used to dispatch the source Read method on the
	// wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the Read method to.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled Read method, cached at adapter construction.
	methodIndex uint16

	// callCount tracks the total number of times Read has been invoked. Defensive overall
	// cap against a progress-making infinite loop; the adapter forces io.EOF after
	// pipitReaderMaxCalls invocations to prevent host hangs.
	callCount int

	// noProgressCount tracks how many times Read has returned with no bytes copied and no
	// error. The adapter forces io.EOF once this crosses pipitReaderMaxNoProgressCalls, the
	// true runaway signature, without penalising a legitimate large stream that makes steady
	// small-count progress.
	noProgressCount int
}

// Read invokes the wrapped pipit value's Read method through the interpreter's
// bound-method dispatch.
//
// Takes p ([]byte) which is the destination buffer Read should fill.
//
// Returns n (int) which is the number of bytes copied into p.
// Returns err (error) which carries any error returned by the pipit-side method (io.EOF
// or otherwise).
//
// Panics when an interpreted panic occurs while the interpreter is still on the dispatch
// stack, re-raising so an upstream interpreted defer/recover can catch it.
func (a *pipitReaderAdapter) Read(p []byte) (n int, err error) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return 0, io.EOF
	}
	a.callCount++
	if a.callCount > pipitReaderMaxCalls {
		return 0, io.EOF
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		n = 0
		err = fmt.Errorf("pipit reader panicked: %v", recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	results := bound.invoke(receiver, []reflect.Value{reflect.ValueOf(p)}, identityArg)
	if len(results) >= 1 && results[0].IsValid() {
		n = int(results[0].Int())
	}
	err = extractErrorResult(results)

	if n < 0 {
		n = 0
	}
	if n > len(p) {
		n = len(p)
	}
	if n == 0 && err == nil && len(p) > 0 {
		a.noProgressCount++
		if a.noProgressCount > pipitReaderMaxNoProgressCalls {
			return 0, io.EOF
		}
		err = io.ErrNoProgress
	}
	return n, err
}

// pipitUnmarshalerAdapter wraps a pipit-synthesised pointer value so it satisfies the
// encoding/json.Unmarshaler interface. Always wraps a *T because UnmarshalJSON mutates
// the receiver.
type pipitUnmarshalerAdapter struct {
	// vm is the parent virtual machine used to dispatch the source UnmarshalJSON method on
	// the wrapped value.
	vm *VM

	// methodRoot pins the rootFunction owning the method body for the cross-package case, or
	// nil for in-package methods.
	methodRoot *program.CompiledFunction

	// underlying is the reflect.Value of the pipit-synthesised type that source code
	// attaches the UnmarshalJSON method to. Held as a *T pointer so mutations performed by
	// the method body are visible to the caller.
	underlying reflect.Value

	// methodIndex is the index into methodRoot.functions (or vm.RootFunction.functions when
	// methodRoot is nil) of the compiled UnmarshalJSON method, cached at adapter
	// construction.
	methodIndex uint16

	// callDepth tracks recursive entry into this adapter; capped at pipitUnmarshalerMaxDepth
	// to bail before a host stack overflow.
	callDepth int
}

// UnmarshalJSON invokes the wrapped pipit value's UnmarshalJSON method.
// Returns the error produced by the source-level method, or any panic captured in the
// dispatch boundary.
//
// Takes data ([]byte) which is the JSON bytes to decode.
//
// Returns the error from the source-level UnmarshalJSON method, or nil on success.
func (a *pipitUnmarshalerAdapter) UnmarshalJSON(data []byte) (returnedError error) {
	callee, root, ok := resolveAdapterCallee(a.vm, a.methodRoot, a.methodIndex)
	if !ok {
		return nil
	}
	a.callDepth++
	defer func() { a.callDepth-- }()
	if a.callDepth > pipitUnmarshalerMaxDepth {
		return errors.New("pipit unmarshaler recursion depth exceeded")
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if a.vm.Globals != nil && a.vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		returnedError = fmt.Errorf("pipit unmarshaler panicked: %v", recovered)
	}()
	bound := newCrossPackageBoundMethod(a.vm, root, callee)
	receiver := receiverValueFor(callee, a.underlying)
	results := bound.invoke(receiver, []reflect.Value{reflect.ValueOf(data)}, identityArg)
	if len(results) >= 1 && results[0].IsValid() {
		if e, ok := reflect.TypeAssert[error](results[0]); ok && e != nil {
			return e
		}
	}
	return nil
}

// fmtAdapterUnwrapper is implemented by the fmt adapters that stand in for a script value
// so fmt can call its String, Error or Format method; %T must name the script value's
// type, not the adapter's.
type fmtAdapterUnwrapper interface {
	// adapterUnderlying returns the script value the adapter wraps.
	//
	// Returns reflect.Value which is the wrapped script value.
	adapterUnderlying() reflect.Value
}

// WellKnownNamedInterfaceReflectType returns the cached reflect.Type for a well-known
// stdlib named interface so compileType does not collapse it to `interface{}`.
//
// Takes pkgPath (string) which is the importer-visible package path.
// Takes typeName (string) which is the bare type name.
//
// Returns reflect.Type which is the cached reflect.Type on hit.
// Returns bool which is true on hit; false otherwise so callers fall through to
// convertUnderlying.
func WellKnownNamedInterfaceReflectType(pkgPath, typeName string) (reflect.Type, bool) {
	if pkgPath == "" {
		if typeName == "error" {
			return errorReflectType, true
		}
		return nil, false
	}
	key := pkgPath + "." + typeName
	rt, ok := wellKnownNamedInterfaceRegistry[key]
	return rt, ok
}

// unwrapPipitNamedType returns the embedded concrete reflect.Type when v holds a
// pipitNamedType wrapper, else v unchanged.
//
// Takes v (reflect.Value).
//
// Returns the unwrapped reflect.Value.
func unwrapPipitNamedType(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	if named, ok := reflect.TypeAssert[pipitNamedType](v); ok {
		return reflect.ValueOf(named.Type)
	}
	if wrapped, ok := reflect.TypeAssert[typemodel.NamedInterfaceWrapper](v); ok {
		return reflect.ValueOf(wrapped.Type)
	}
	if v.Kind() == reflect.Slice && v.Type().Elem() == reflectTypeReflectType {
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			out.Index(i).Set(unwrapReflectTypeSliceElement(v.Index(i)))
		}
		return out
	}
	return v
}

// unwrapAdapterUnderlying returns the embedded reflect.Value boxed inside a pipit
// interface adapter.
//
// Takes v (reflect.Value).
//
// Returns the unwrapped reflect.Value.
func unwrapAdapterUnderlying(v reflect.Value) reflect.Value {
	if v.Kind() != reflect.Pointer || v.IsNil() || !v.CanInterface() {
		return v
	}
	if a, ok := reflect.TypeAssert[*pipitStringerAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitMarshalerAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitUnmarshalerAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitReaderAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitFormatterAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitErrorAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitScannerAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitSortInterfaceAdapter](v); ok {
		return a.underlying
	}
	if a, ok := reflect.TypeAssert[*pipitWriterAdapter](v); ok {
		return a.underlying
	}
	return unwrapExtendedAdapterUnderlying(v)
}

// unwrapReflectTypeSliceElement strips a pipitNamedType or symtab.NamedInterfaceWrapper
// from a single []reflect.Type element so reflect type-constructor families don't see
// pipit wrappers when they internally type-assert to *rtype.
//
// Takes element (reflect.Value) which is one slice element of type reflect.Type.
//
// Returns the unwrapped reflect.Value, or element unchanged when it is not a known pipit
// wrapper.
func unwrapReflectTypeSliceElement(element reflect.Value) reflect.Value {
	if named, ok := reflect.TypeAssert[pipitNamedType](element); ok {
		return reflect.ValueOf(named.Type)
	}
	if wrapped, ok := reflect.TypeAssert[typemodel.NamedInterfaceWrapper](element); ok {
		return reflect.ValueOf(wrapped.Type)
	}
	return element
}

// unwrapPipitNamedTypeArguments strips wrappers for reflect constructors.
//
// Unwraps any pipitNamedType wrappers in the argument slice when reflectedFunction is one
// of the reflect type-constructor family. reflect.PointerTo / SliceOf / ... panic on a
// pipitNamedType because they down-assert to *reflect.rtype; the wrapper exists only so
// reflect.TypeOf(...).Name() reports the pipit source name, and that identity is not
// needed by the constructors.
//
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which is mutated in place.
func unwrapPipitNamedTypeArguments(reflectedFunction reflect.Value, arguments []reflect.Value) {
	if reflectedFunction.Kind() != reflect.Func {
		return
	}
	if _, ok := reflectTypeConstructorPointers[reflectedFunction.Pointer()]; !ok {
		return
	}
	for i := range arguments {
		arguments[i] = unwrapPipitNamedType(arguments[i])
	}
}

// unwrapPipitAdapterArguments strips pipit interface adapters from arguments for reflect
// constructors.
//
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which is mutated in place.
func unwrapPipitAdapterArguments(reflectedFunction reflect.Value, arguments []reflect.Value) {
	if reflectedFunction.Kind() != reflect.Func {
		return
	}
	functionPointer := reflectedFunction.Pointer()
	_, isReflectIdentity := reflectIdentityFunctionPointers[functionPointer]
	isIdentityTransparent := typemodel.IsIdentityTransparentFunction(functionPointer)
	if !isReflectIdentity && !isIdentityTransparent {
		return
	}
	for i := range arguments {
		arguments[i] = unwrapAdapterUnderlying(arguments[i])
	}
}

// shimReflectMakeFuncImpl rewrites the impl argument of a reflect.MakeFunc call so the
// values it returns are converted to the exact element types the target functionType
// declares.
//
// pipit boxes scalars from its register banks at canonical widths (int64/float64/...).
// When an interpreted reflect.MakeFunc impl returns, say, an int64 but the functionType
// declares an int result, reflect.MakeFunc's trampoline rejects the assignment and
// panics. The shim intercepts each returned []reflect.Value and Converts any element
// whose type differs from functionType.Out(i).
//
// Takes reflectedFunction (reflect.Value) which is the native callee.
// Takes arguments ([]reflect.Value) which is mutated in place when the callee is
// reflect.MakeFunc.
func shimReflectMakeFuncImpl(reflectedFunction reflect.Value, arguments []reflect.Value) {
	if reflectedFunction.Kind() != reflect.Func || reflectedFunction.Pointer() != reflectMakeFuncPointer {
		return
	}
	if len(arguments) != 2 || !arguments[0].IsValid() || !arguments[1].IsValid() {
		return
	}
	functionType, ok := reflect.TypeAssert[reflect.Type](arguments[0])
	if !ok || functionType == nil || functionType.Kind() != reflect.Func {
		return
	}
	inner := arguments[1]
	if inner.Kind() != reflect.Func {
		return
	}
	arguments[1] = makeFuncImplConvertingShim(functionType, inner)
}

// makeFuncImplConvertingShim builds a replacement reflect.MakeFunc impl that forwards to
// inner and converts each value the impl returns to the exact element type functionType
// declares.
//
// Takes functionType (reflect.Type) which is the target function type whose Out(i)
// element types the returned values must match.
// Takes inner (reflect.Value) which is the original interpreted impl closure.
//
// Returns a reflect.Value holding the wrapped impl closure.
func makeFuncImplConvertingShim(functionType reflect.Type, inner reflect.Value) reflect.Value {
	return reflect.MakeFunc(inner.Type(), func(in []reflect.Value) []reflect.Value {
		innerOut := inner.Call(in)
		if len(innerOut) != 1 || !innerOut[0].IsValid() {
			return innerOut
		}
		results, ok := reflect.TypeAssert[[]reflect.Value](innerOut[0])
		if !ok {
			return innerOut
		}
		convertMakeFuncResults(functionType, results)
		return []reflect.Value{reflect.ValueOf(results)}
	})
}

// convertMakeFuncResults converts each element of results in place to the corresponding
// declared Out(i) type of functionType when the runtime type differs and a conversion is
// possible.
//
// Takes functionType (reflect.Type) which declares the target Out types.
// Takes results ([]reflect.Value) which is mutated in place.
func convertMakeFuncResults(functionType reflect.Type, results []reflect.Value) {
	for i := range results {
		if i >= functionType.NumOut() || !results[i].IsValid() {
			continue
		}
		want := functionType.Out(i)
		if results[i].Type() != want && results[i].Type().ConvertibleTo(want) {
			results[i] = results[i].Convert(want)
		}
	}
}

// applyPipitReflectTypeOfNaming re-clothes the result of a reflect.TypeOf call so the
// rendered name preserves source-level identity that pipit's type converter would
// otherwise lose.
//
// When the argument was a pipit-defined named primitive, the result is wrapped in
// pipitNamedType so .Name() / .String() report the source-level name (e.g. `type Score
// int` reports "Score" not "int"). When the argument was a typed-nil pointer to a
// user-declared named interface (e.g. `(*myiface)(nil)`), the result is wrapped in
// symtab.NamedInterfaceWrapper so .Elem().String() reports the source-level interface
// name (e.g. "main.myiface") instead of the lossy "interface {}" that Go's reflect
// produces for pipit's synthesised `*interface{}` type.
//
// Takes vm (*VM) which provides the GlobalStore for user-interface lookups. May be nil
// for tests; the user-interface wrap is then skipped.
// Takes reflectedFunction (reflect.Value) which is the native callee; the wrap is applied
// only when it is reflect.TypeOf.
// Takes site (*CallSite) which carries the per-argument static type metadata recorded by
// the Compiler.
// Takes results ([]reflect.Value) which is the native call's return slice.
//
// Returns results unchanged unless a wrap applies.
func applyPipitReflectTypeOfNaming(vm *VM, reflectedFunction reflect.Value, site *program.CallSite, results []reflect.Value) []reflect.Value {
	if reflectedFunction.Kind() != reflect.Func || reflectedFunction.Pointer() != reflectTypeOfPointer {
		return results
	}
	if len(results) != 1 || !results[0].IsValid() {
		return results
	}
	resolvedType, ok := reflect.TypeAssert[reflect.Type](results[0])
	if !ok || resolvedType == nil {
		return results
	}
	if info, isPool := typemodel.LookupNamedScalarPoolInfo(resolvedType); isPool {
		results[0] = reflect.ValueOf(pipitNamedType{
			Type:          resolvedType,
			sourceName:    info.BareName,
			sourcePackage: info.PkgName,
		})
		return results
	}
	if len(site.ArgumentStaticTypeNames) == 0 {
		return results
	}
	sourceName := site.ArgumentStaticTypeNames[0]
	if sourceName == "" {
		return results
	}
	if resolvedType.Name() == sourceName {
		return results
	}
	sourcePackage := pipitTypeOfSourcePackage(site)
	if isScalarReflectKind(resolvedType.Kind()) && !isBuiltinScalarTypeName(sourceName) {
		results[0] = reflect.ValueOf(pipitNamedType{
			Type:          resolvedType,
			sourceName:    sourceName,
			sourcePackage: sourcePackage,
		})
		return results
	}
	if wrapped, ok := tryWrapNamedInterfacePointer(vm, site, sourceName, sourcePackage, resolvedType); ok {
		results[0] = reflect.ValueOf(wrapped)
		return results
	}
	return results
}

// pipitTypeOfSourcePackage extracts the package short name.
//
// Reads the call site's recorded static type string for the first argument; e.g.
// "*main.myiface" -> "main".
//
// Takes site (*CallSite) which carries the recorded static type strings.
//
// Returns string which is the package short name, or empty.
func pipitTypeOfSourcePackage(site *program.CallSite) string {
	if len(site.ArgumentStaticTypeStrings) == 0 {
		return ""
	}
	s := site.ArgumentStaticTypeStrings[0]

	for len(s) > 0 && (s[0] == '*' || s[0] == '[' || s[0] == ']') {
		if s[0] == '[' {
			if closeIdx := indexByteString(s, ']'); closeIdx > 0 {
				s = s[closeIdx+1:]
				continue
			}
			return ""
		}
		s = s[1:]
	}
	if dot := indexByteString(s, '.'); dot > 0 {
		return s[:dot]
	}
	return ""
}

// tryWrapNamedInterfacePointer wraps named-interface pointer types.
//
// Detects a *interface{} reflect.Type (the pipit-synth shape for a typed-nil pointer to a
// user-declared named interface) and wraps it in a symtab.NamedInterfaceWrapper carrying
// the source-level identity from the per-Service registry. The static type string is
// required to be "*"-prefixed because that is how pipit's Compiler renders
// `(*myiface)(nil)` for the call site.
//
// Takes vm (*VM) which provides GlobalStore; nil means no wrap.
// Takes site (*CallSite) which carries argumentStaticTypeStrings.
// Takes sourceName (string) which is the bare interface name.
// Takes sourcePackage (string) which is the short package name.
// Takes resolvedType (reflect.Type) which the call produced.
//
// Returns symtab.NamedInterfaceWrapper which carries the wrapped type.
// Returns bool which is true on success.
func tryWrapNamedInterfacePointer(vm *VM, site *program.CallSite, sourceName, sourcePackage string, resolvedType reflect.Type) (typemodel.NamedInterfaceWrapper, bool) {
	if vm == nil || vm.Globals == nil {
		return typemodel.NamedInterfaceWrapper{}, false
	}
	if resolvedType.Kind() != reflect.Pointer || resolvedType.Elem().Kind() != reflect.Interface {
		return typemodel.NamedInterfaceWrapper{}, false
	}
	if sourcePackage == "" {
		return typemodel.NamedInterfaceWrapper{}, false
	}
	qualifiedInner := sourcePackage + "." + sourceName
	innerPipit, ok := vm.Globals.lookupUserNamedInterface(qualifiedInner)
	if !ok {
		return typemodel.NamedInterfaceWrapper{}, false
	}

	if len(site.ArgumentStaticTypeStrings) > 0 {
		if !startsWithStarOfQualified(site.ArgumentStaticTypeStrings[0], qualifiedInner) {
			return typemodel.NamedInterfaceWrapper{}, false
		}
	}
	ptrPipit := typemodel.TypeOfPointer(innerPipit)
	return typemodel.NewNamedInterfaceWrapper(resolvedType, ptrPipit), true
}

// startsWithStarOfQualified checks for a "*<qualified>" prefix.
//
// Verifies the call site's static type is a pointer to the named interface that was
// resolved.
//
// Takes s (string) which is the static type string to inspect.
// Takes qualified (string) which is the expected qualified name.
//
// Returns bool which is true when s starts with "*<qualified>".
func startsWithStarOfQualified(s, qualified string) bool {
	if len(s) < len(qualified)+1 || s[0] != '*' {
		return false
	}
	return s[1:len(qualified)+1] == qualified
}

// isScalarReflectKind reports whether k is a numeric, boolean, or string kind - the only
// kinds for which pipitNamedType wrapping is applied. Struct / slice / map / pointer
// types are left untouched so reflect.Type identity comparisons and the existing
// struct-field sentinel filtering keep working.
//
// Takes k (reflect.Kind).
//
// Returns true for scalar kinds.
func isScalarReflectKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Complex64, reflect.Complex128,
		reflect.String:
		return true
	default:
	}
	return false
}

// indexByteString returns the index of the first occurrence of b in s, or -1. Thin
// wrapper over strings.IndexByte, which the Compiler intrinsifies to an
// architecture-tuned scan; the wrapper keeps the call sites' intent legible.
//
// Takes s (string) which is the string to search.
// Takes b (byte) which is the byte to find.
//
// Returns the index, or -1 when absent.
func indexByteString(s string, b byte) int {
	return strings.IndexByte(s, b)
}

// extractBytesResult returns the first reflect.Value in results as []byte when valid and
// type-compatible, or nil otherwise.
//
// Takes results ([]reflect.Value) which are the method invocation results.
//
// Returns []byte which is the converted first result, or nil.
func extractBytesResult(results []reflect.Value) []byte {
	if len(results) < 1 || !results[0].IsValid() {
		return nil
	}
	bytes, ok := reflect.TypeAssert[[]byte](results[0])
	if !ok {
		return nil
	}
	return bytes
}

// extractErrorResult returns the second reflect.Value in results as error when valid and
// type-compatible, or nil otherwise.
//
// Takes results ([]reflect.Value) which are the method invocation results.
//
// Returns error which is the converted second result, or nil.
func extractErrorResult(results []reflect.Value) error {
	if len(results) < 2 || !results[1].IsValid() {
		return nil
	}
	errInterface, ok := reflect.TypeAssert[error](results[1])
	if !ok {
		return nil
	}
	return errInterface
}

// buildMarshalerAdapterIfRegistered returns a pipitMarshalerAdapter wrapping argument
// when the type-name has a registered MarshalJSON method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing json.Marshaler, or an invalid
// reflect.Value when no MarshalJSON method is registered.
func buildMarshalerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".MarshalJSON")
	if !ok {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitMarshalerAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// invokeStringReturnMethod dispatches a bound method whose first result is a string,
// returning that string or "" when dispatch is not possible (nil VM, unresolved method
// index, or empty/invalid first result).
//
// When methodRoot is non-nil it overrides vm.RootFunction for the dispatch - the
// cross-package case where the method body lives in another CompileProgram's
// rootFunction.
//
// Takes vm (*VM) which provides the dispatch root.
// Takes methodRoot (*CompiledFunction) which holds the method body when it lives outside
// vm.RootFunction; nil for the in-package case.
// Takes methodIndex (uint16) which selects the compiled method inside the given root (or
// vm.RootFunction when none is given).
// Takes underlying (reflect.Value) which is the receiver value.
//
// Returns string which is the first result of the bound method or "".
func invokeStringReturnMethod(vm *VM, methodRoot *program.CompiledFunction, methodIndex uint16, underlying reflect.Value) string {
	callee, root, ok := resolveAdapterCallee(vm, methodRoot, methodIndex)
	if !ok {
		return ""
	}
	bound := newCrossPackageBoundMethod(vm, root, callee)
	receiver := receiverValueFor(callee, underlying)
	results := bound.invoke(receiver, nil, identityArg)
	if len(results) == 0 || !results[0].IsValid() {
		return ""
	}
	return results[0].String()
}

// adapterMethodHasShape reports whether the compiled method behind an adapter takes
// params arguments (receiver excluded) and returns results values.
//
// Takes vm (*VM) which owns the function table.
// Takes methodRoot (*program.CompiledFunction) which is the method's root.
// Takes methodIndex (uint16) which is the method's function-table index.
// Takes params (int) which is the expected parameter count.
// Takes results (int) which is the expected result count.
//
// Returns bool which is true when the shape matches; an unresolved method counts as a
// match so the receiver check decides.
func adapterMethodHasShape(vm *VM, methodRoot *program.CompiledFunction, methodIndex uint16, params, results int) bool {
	callee, _, ok := resolveAdapterCallee(vm, methodRoot, methodIndex)
	if !ok || callee == nil {
		return true
	}
	parameterCount := len(callee.ParameterKinds)
	if callee.HasReceiver {
		parameterCount--
	}
	return parameterCount == params && len(callee.ResultKinds) == results
}

// resolveAdapterCallee picks the callee and its rootFunction.
//
// When methodRoot is non-nil the entry is in that root; otherwise it is in
// vm.RootFunction. Returns ok=false when either input is malformed, matching the adapter
// behaviour of returning a zero result rather than panicking.
//
// Takes vm (*VM) which is the parent VM.
// Takes methodRoot (*CompiledFunction) which is the cross-package override; nil for
// in-package methods.
// Takes methodIndex (uint16) which is the slot inside the chosen rootFunction's
// functions.
//
// Returns *CompiledFunction which is the resolved callee.
// Returns *CompiledFunction which is the owning root function.
// Returns bool which is true on success; false on lookup failure.
func resolveAdapterCallee(vm *VM, methodRoot *program.CompiledFunction, methodIndex uint16) (callee, rootFunction *program.CompiledFunction, ok bool) {
	if vm == nil {
		return nil, nil, false
	}
	root := methodRoot
	if root == nil {
		root = vm.rootFunction
	}
	if root == nil {
		return nil, nil, false
	}
	if int(methodIndex) >= len(root.Functions) {
		return nil, nil, false
	}
	callee = root.Functions[methodIndex]
	if callee == nil {
		return nil, nil, false
	}
	return callee, root, true
}

// newCrossPackageBoundMethod constructs a boundMethodVM whose rootFunctionOverride is set
// when the method body lives in a different CompileProgram batch than the parent VM's
// root. When the method is local (root == vm.RootFunction) the override stays nil and
// boundMethodVM.invoke takes its existing in-package path.
//
// Takes vm (*VM) which is the parent VM.
// Takes root (*CompiledFunction) which owns callee.
// Takes callee (*CompiledFunction) which is the method body to run.
//
// Returns a boundMethodVM ready for invoke().
func newCrossPackageBoundMethod(vm *VM, root *program.CompiledFunction, callee *program.CompiledFunction) *boundMethodVM {
	bound := &boundMethodVM{vm: vm, callee: callee, limits: vm.Limits, rootFunctionOverride: nil}
	if root != nil && root != vm.rootFunction {
		bound.rootFunctionOverride = root
	}
	return bound
}

// lookupAdapterMethod resolves a "TypeName.MethodName" entry to (rootFunction,
// methodIndex, ok). The in-package methodTable wins over the cross-package external
// registry - matching the spirit of Go's import lookup where a same-named
// locally-declared method shadows an imported one.
//
// Takes vm (*VM) which provides both lookup spaces.
// Takes key (string) which is "TypeName.MethodName".
//
// Returns the resolved entry. methodRoot is nil when the entry is local to
// vm.RootFunction.
func lookupAdapterMethod(vm *VM, key string) (methodRoot *program.CompiledFunction, methodIndex uint16, ok bool) {
	if vm == nil {
		return nil, 0, false
	}
	if vm.rootFunction != nil {
		if index, found := vm.rootFunction.MethodTable()[key]; found {
			return nil, index, true
		}
	}
	if vm.Globals == nil {
		return nil, 0, false
	}
	entry, found := vm.Globals.lookupExternalMethod(key)
	if !found {
		return nil, 0, false
	}
	return entry.rootFunction, entry.methodIndex, true
}

// buildFormatterAdapterIfRegistered returns a pipitFormatterAdapter wrapping argument
// when the type-name has a registered Format method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing fmt.Formatter, or an invalid reflect.Value
// when no Format method is registered.
func buildFormatterAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".Format")
	if !ok || !adapterMethodHasShape(vm, methodRoot, methodIndex, 2, 0) {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitFormatterAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// buildSortInterfaceAdapterIfRegistered returns a pipitSortInterfaceAdapter wrapping
// argument when the type-name has registered Len, Less and Swap methods, or an invalid
// value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing sort.Interface, or an invalid
// reflect.Value.
func buildSortInterfaceAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	lenRoot, lenIndex, hasLen := lookupAdapterMethod(vm, typeName+".Len")
	lessRoot, lessIndex, hasLess := lookupAdapterMethod(vm, typeName+".Less")
	swapRoot, swapIndex, hasSwap := lookupAdapterMethod(vm, typeName+".Swap")
	if !hasLen || !hasLess || !hasSwap {
		if recovered, ok := pipitTypeName(vm, argument); ok && recovered != typeName {
			lenRoot, lenIndex, hasLen = lookupAdapterMethod(vm, recovered+".Len")
			lessRoot, lessIndex, hasLess = lookupAdapterMethod(vm, recovered+".Less")
			swapRoot, swapIndex, hasSwap = lookupAdapterMethod(vm, recovered+".Swap")
		}
	}
	if !hasLen || !hasLess || !hasSwap {
		return reflect.Value{}
	}

	if lenRoot != lessRoot || lessRoot != swapRoot {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(lenRoot, vm, lenIndex, argument) ||
		!methodReceiverSatisfiesValueIn(lessRoot, vm, lessIndex, argument) ||
		!methodReceiverSatisfiesValueIn(swapRoot, vm, swapIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitSortInterfaceAdapter{
		vm:         vm,
		methodRoot: lenRoot,
		underlying: argument,
		lenIndex:   lenIndex,
		lessIndex:  lessIndex,
		swapIndex:  swapIndex,
	})
}

// buildScannerAdapterIfRegistered returns a pipitScannerAdapter wrapping argument when
// the type-name has a registered Scan method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing fmt.Scanner, or an invalid reflect.Value
// when no Scan method is registered.
func buildScannerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".Scan")
	if !ok {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitScannerAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// buildWriterAdapterIfRegistered returns a pipitWriterAdapter wrapping argument when the
// type-name has a registered Write method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing io.Writer, or an invalid reflect.Value
// when no Write method is registered.
func buildWriterAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".Write")
	if !ok {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitWriterAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// buildUnmarshalerAdapterIfRegistered returns a pipitUnmarshalerAdapter wrapping argument
// when the type-name has a registered UnmarshalJSON method, or an invalid value
// otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing json.Unmarshaler, or an invalid
// reflect.Value when no UnmarshalJSON method is registered (or when the method has a
// pointer receiver but the argument is not addressable).
func buildUnmarshalerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".UnmarshalJSON")
	if !ok {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitUnmarshalerAdapter{vm: vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex, callDepth: 0})
}

// buildReaderAdapterIfRegistered returns a pipitReaderAdapter wrapping argument when the
// type-name has a registered Read method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing io.Reader, or an invalid reflect.Value
// when no Read method is registered.
func buildReaderAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".Read")
	if !ok {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitReaderAdapter{vm: vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex, callCount: 0, noProgressCount: 0})
}

// receiverValueFor coerces an adapter's underlying value to match the callee's receiver
// kind. Pointer-receiver methods get the address, value-receiver methods get the raw
// value.
//
// Takes callee (*CompiledFunction) which records isPointerReceiver for the source-level
// method.
// Takes underlying (reflect.Value) which is the adapter's wrapped value.
//
// Returns the receiver shape suitable for boundMethodVM.invoke.
func receiverValueFor(callee *program.CompiledFunction, underlying reflect.Value) reflect.Value {
	if callee != nil && !callee.IsPointerReceiver {
		return underlying
	}
	return pointerReceiverFor(underlying)
}

// pointerReceiverFor coerces a value to the address shape needed by pointer-receiver
// dispatch.
//
// Already-pointer values pass through; addressable values return their Addr;
// non-addressable struct values are copied into a fresh reflect.New so the callee can
// take Addr in the standard way bound method dispatch expects.
//
// Takes value (reflect.Value) which is the pipit-synthesised value.
//
// Returns a reflect.Value usable as the receiver argument to bound method dispatch.
func pointerReceiverFor(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Kind() == reflect.Pointer {
		return value
	}
	if value.CanAddr() {
		return value.Addr()
	}
	addressable := reflect.New(value.Type())
	addressable.Elem().Set(value)
	return addressable
}

// buildErrorAdapterIfRegistered returns a pipitErrorAdapter wrapping argument when the
// type-name has a registered Error method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing error, or an invalid reflect.Value when no
// Error method is registered.
func buildErrorAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".Error")
	if !ok || !adapterMethodHasShape(vm, methodRoot, methodIndex, 0, 1) {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitErrorAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// methodReceiverSatisfiesValueIn enforces Go's method-set rule.
//
// A method declared on `*T` is in T's method set only when the T value is addressable; a
// method declared on `T` (value receiver) is in both T's and *T's method set. Variant of
// methodReceiverSatisfiesValue that is cross-package aware: when methodRoot is non-nil
// the callee is looked up in methodRoot.functions instead of vm.RootFunction.functions,
// the same indirection invokeStringReturnMethod uses.
//
// Takes methodRoot (*CompiledFunction) which owns the callee; nil for the in-package
// case.
// Takes vm (*VM) which is the parent virtual machine.
// Takes methodIndex (uint16) which selects the callee.
// Takes argument (reflect.Value) which is the value about to be adapted.
//
// Returns true when adapter construction may proceed.
func methodReceiverSatisfiesValueIn(methodRoot *program.CompiledFunction, vm *VM, methodIndex uint16, argument reflect.Value) bool {
	callee, _, ok := resolveAdapterCallee(vm, methodRoot, methodIndex)
	if !ok || callee == nil {
		return true
	}
	if !callee.IsPointerReceiver {
		return true
	}
	if !argument.IsValid() {
		return false
	}
	probe := argument
	if probe.Kind() == reflect.Interface {
		if probe.IsNil() {
			return false
		}
		probe = probe.Elem()
	}
	return probe.Kind() == reflect.Pointer
}

// buildStringerAdapterIfRegistered returns a pipitStringerAdapter wrapping argument when
// the type-name has a registered String method, or an invalid value otherwise.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name used for method-table
// lookup.
//
// Returns a wrapped reflect.Value implementing fmt.Stringer, or an invalid reflect.Value
// when no String method is registered.
func buildStringerAdapterIfRegistered(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	methodRoot, methodIndex, ok := lookupAdapterMethod(vm, typeName+".String")
	if !ok || !adapterMethodHasShape(vm, methodRoot, methodIndex, 0, 1) {
		return reflect.Value{}
	}
	if !methodReceiverSatisfiesValueIn(methodRoot, vm, methodIndex, argument) {
		return reflect.Value{}
	}
	return reflect.ValueOf(&pipitStringerAdapter{
		vm:          vm,
		methodRoot:  methodRoot,
		underlying:  argument,
		methodIndex: methodIndex,
	})
}

// pipitTypeName resolves the source-level type name for a value whose reflect.Type was
// synthesised by pipit (via reflect.StructOf with the _pipitID_ sentinel field). Looks
// the type up in the root function's typeNames registry, transparently unwrapping pointer
// kinds so that both *MyErr and MyErr resolve to the same name.
//
// Takes vm (*VM) which provides typeNames access.
// Takes value (reflect.Value) which is the runtime value to identify.
//
// Returns the source-level type name and true on success, or "" and false when the
// value's type is not a pipit-synthesised type.
func pipitTypeName(vm *VM, value reflect.Value) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	if vm == nil || vm.rootFunction == nil {
		return "", false
	}
	t := value.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if name, ok := vm.rootFunction.TypeNames[t]; ok {
		return name, true
	}

	if name := bareSentinelName(t); name != "" {
		return name, true
	}
	return "", false
}
