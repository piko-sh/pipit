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
	"reflect"

	"pipit.sh/pipit/internal/symtab/typemodel"
)

var (
	// reflectMakeFuncStubPointer caches the shared trampoline code pointer that every
	// reflect.MakeFunc result reports, identifying MakeFunc closures by a single address.
	reflectMakeFuncStubPointer = reflect.MakeFunc(
		reflect.FuncOf(nil, nil, false),
		func([]reflect.Value) []reflect.Value { return nil },
	).Pointer()
)

// IsSupportedAdapterInterface reports whether expectedType is one of the stdlib
// interfaces that tryBuildInterfaceAdapter knows how to satisfy with a pipit-side
// adapter, or an interface a host registered through RegisterInterfaceAdapter().
//
// Takes expectedType (reflect.Type) which is the parameter's declared interface type.
//
// Returns true when an exact-match adapter exists for expectedType.
func IsSupportedAdapterInterface(expectedType reflect.Type) bool {
	switch expectedType {
	case errorReflectType,
		stringerReflectType,
		jsonMarshalerReflectType,
		jsonUnmarshalerReflectType,
		IOReaderReflectType,
		iOWriterReflectType,
		fmtFormatterReflectType,
		fmtScannerReflectType,
		sortInterfaceReflectType,
		IOCloserReflectType,
		IOReadCloserReflectType,
		IOWriteCloserReflectType,
		IoReadWriterReflectType,
		IoReaderFromReflectType,
		IoWriterToReflectType,
		TextMarshalerReflectType,
		TextUnmarshalerReflectType,
		HTTPHandlerReflectType,
		HeapInterfaceReflectType:
		return true
	}
	_, registered := registeredAdapterBuilder(expectedType)
	return registered
}

// isPipitMakeFunctionClosure reports whether reflectedFunction is a reflect.MakeFunc
// closure rather than a genuine native Go function.
//
// Takes reflectedFunction (reflect.Value) which is the resolved callee.
//
// Returns true when the callee is a pipit-side reflect.MakeFunc closure.
func isPipitMakeFunctionClosure(reflectedFunction reflect.Value) bool {
	return reflectedFunction.Kind() == reflect.Func &&
		reflectedFunction.Pointer() == reflectMakeFuncStubPointer
}

// tryBuildInterfaceAdapter returns a reflect.Value that satisfies the expected interface
// type by wrapping the argument in a pipit-side adapter, or an invalid reflect.Value when
// no adapter is applicable.
//
// Takes vm (*VM) which provides access to the method registry.
// Takes argument (reflect.Value) which is the value being passed through
// coerceReflectArgument.
// Takes expectedType (reflect.Type) which is the parameter's declared interface type.
// Takes typeCtx (argumentTypeContext) which carries the static type name from the call
// site.
//
// Returns a wrapped reflect.Value implementing expectedType, or an invalid reflect.Value
// when no adapter applies.
func tryBuildInterfaceAdapter(vm *VM, argument reflect.Value, expectedType reflect.Type, typeCtx argumentTypeContext) reflect.Value {
	if vm == nil || vm.rootFunction == nil || expectedType == nil {
		return reflect.Value{}
	}
	if expectedType.Kind() != reflect.Interface {
		return reflect.Value{}
	}
	allowAny := expectedType.NumMethod() == 0
	if !allowAny && !IsSupportedAdapterInterface(expectedType) {
		return reflect.Value{}
	}
	if !argument.IsValid() {
		return reflect.Value{}
	}
	typeName := resolveAdapterTypeName(vm, argument, typeCtx)
	if typeName == "" {
		return reflect.Value{}
	}
	if !allowAny {
		return buildAdapterForInterface(vm, argument, expectedType, typeName)
	}
	return buildAnyInterfaceAdapter(vm, argument, typeName)
}

// resolveAdapterTypeName determines the source-level type name used to look up adapter
// methods for argument. Pipit-synthesised types resolve via the typeNames registry,
// falling back to the call site's recorded static name; all other types use the static
// name directly.
//
// Takes vm (*VM) which provides the typeNames registry.
// Takes argument (reflect.Value) which is the value being adapted.
// Takes typeCtx (argumentTypeContext) which carries the static name.
//
// Returns the resolved source-level type name, or "" when unknown.
func resolveAdapterTypeName(vm *VM, argument reflect.Value, typeCtx argumentTypeContext) string {
	if argument.Kind() == reflect.Pointer && argument.Type().Elem().Kind() == reflect.Pointer {
		return ""
	}
	if info, ok := typemodel.LookupNamedScalarPoolInfo(argument.Type()); ok {
		return info.BareName
	}
	if !isPipitSynthesisedReflectType(argument.Type()) && typeCtx.staticTypeName != "" {
		return typeCtx.staticTypeName
	}
	if typeName, ok := pipitTypeName(vm, argument); ok {
		return typeName
	}
	return typeCtx.staticTypeName
}

// buildAdapterForInterface dispatches to the exact-match adapter builder for a known
// stdlib interface type.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes expectedType (reflect.Type) which is the declared interface.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing expectedType, or an invalid reflect.Value
// when no adapter applies.
func buildAdapterForInterface(vm *VM, argument reflect.Value, expectedType reflect.Type, typeName string) reflect.Value {
	switch expectedType {
	case errorReflectType:
		return buildErrorAdapterIfRegistered(vm, argument, typeName)
	case stringerReflectType:
		return buildStringerAdapterIfRegistered(vm, argument, typeName)
	case jsonMarshalerReflectType:
		return buildMarshalerAdapterIfRegistered(vm, argument, typeName)
	case IOReaderReflectType:
		return buildReaderAdapterIfRegistered(vm, argument, typeName)
	case iOWriterReflectType:
		return buildWriterAdapterIfRegistered(vm, argument, typeName)
	case jsonUnmarshalerReflectType:
		return buildUnmarshalerAdapterIfRegistered(vm, argument, typeName)
	case fmtFormatterReflectType:
		return buildFormatterAdapterIfRegistered(vm, argument, typeName)
	case fmtScannerReflectType:
		return buildScannerAdapterIfRegistered(vm, argument, typeName)
	case sortInterfaceReflectType:
		return buildSortInterfaceAdapterIfRegistered(vm, argument, typeName)
	}
	return buildExtendedAdapterForInterface(vm, argument, expectedType, typeName)
}

// buildExtendedAdapterForInterface dispatches to the builders for the io, encoding,
// net/http and container/heap interfaces added after the original closed set.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes expectedType (reflect.Type) which is the declared interface.
// Takes typeName (string) which is the source-level type name.
//
// Returns a wrapped reflect.Value implementing expectedType, or an invalid reflect.Value
// when no adapter applies.
func buildExtendedAdapterForInterface(vm *VM, argument reflect.Value, expectedType reflect.Type, typeName string) reflect.Value {
	switch expectedType {
	case IOCloserReflectType:
		return buildCloserAdapterIfRegistered(vm, argument, typeName)
	case IOReadCloserReflectType:
		return buildReadCloserAdapterIfRegistered(vm, argument, typeName)
	case IOWriteCloserReflectType:
		return buildWriteCloserAdapterIfRegistered(vm, argument, typeName)
	case IoReadWriterReflectType:
		return buildReadWriterAdapterIfRegistered(vm, argument, typeName)
	case IoReaderFromReflectType:
		return buildReaderFromAdapterIfRegistered(vm, argument, typeName)
	case IoWriterToReflectType:
		return buildWriterToAdapterIfRegistered(vm, argument, typeName)
	case TextMarshalerReflectType:
		return buildTextMarshalerAdapterIfRegistered(vm, argument, typeName)
	case TextUnmarshalerReflectType:
		return buildTextUnmarshalerAdapterIfRegistered(vm, argument, typeName)
	case HTTPHandlerReflectType:
		return buildHTTPHandlerAdapterIfRegistered(vm, argument, typeName)
	case HeapInterfaceReflectType:
		return buildHeapInterfaceAdapterIfRegistered(vm, argument, typeName)
	}
	return buildRegisteredAdapter(vm, argument, expectedType, typeName)
}

// buildAnyInterfaceAdapter tries each adapter builder in turn for an empty-interface
// (any) parameter, returning the first applicable adapter or the Stringer adapter as the
// final fallback.
//
// Takes vm (*VM) which provides the method registry.
// Takes argument (reflect.Value) which is the value being wrapped.
// Takes typeName (string) which is the source-level type name.
//
// Returns the first applicable wrapped reflect.Value, or an invalid reflect.Value when no
// adapter applies.
func buildAnyInterfaceAdapter(vm *VM, argument reflect.Value, typeName string) reflect.Value {
	builders := []func(*VM, reflect.Value, string) reflect.Value{
		buildScannerAdapterIfRegistered,
		buildErrorAdapterIfRegistered,
		buildMarshalerAdapterIfRegistered,
		buildUnmarshalerAdapterIfRegistered,
		buildReaderAdapterIfRegistered,
		buildWriterAdapterIfRegistered,
	}
	for _, builder := range builders {
		if adapter := builder(vm, argument, typeName); adapter.IsValid() {
			return adapter
		}
	}
	return buildStringerAdapterIfRegistered(vm, argument, typeName)
}
