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

package symtab

import (
	"errors"
	"fmt"
	"go/types"
	"reflect"
	"runtime"
	"sync"
	"weak"

	"piko.sh/safeconv"
)

// maxLayoutDepth bounds the inline structs and arrays layoutDisagreement descends into.
const maxLayoutDepth = 8

// ErrTypesPackageLayout is returned by RegisterTypesPackage, and by Import for the
// package it rejected, when a pre-built go/types package (a provider's embedded export
// data) declares a type whose layout disagrees with the native type registered under the
// same name.
var ErrTypesPackageLayout = errors.New("symbol registry: types package layout disagrees with the native type")

// layoutSizes is the gc compiler's size and alignment model for the running architecture,
// which the native reflect types were laid out by.
var layoutSizes = types.SizesFor("gc", runtime.GOARCH)

// layoutMemo remembers layoutDisagreement's answer for each declaration and native type
// compared, across every registry in the process.
var layoutMemo = layoutMemoTable{entries: make(map[layoutMemoKey]string), mu: sync.RWMutex{}}

// layoutMemoKey names one comparison of a pre-built declaration with a native type.
type layoutMemoKey struct {
	// native is the registered native type.
	native reflect.Type

	// typeName is the go/types declaration, held weakly so the memo never keeps a discarded
	// package alive.
	typeName weak.Pointer[types.TypeName]
}

// layoutMemoTable is the process-wide memo of layout comparisons.
type layoutMemoTable struct {
	// entries maps a comparison to its answer, "" when the layouts agree.
	entries map[layoutMemoKey]string

	// mu guards entries.
	mu sync.RWMutex
}

// checkTypesPackageLayoutLocked compares every type the package's native exports register
// (a typed nil pointer, (*T)(nil)) with the go/types type of the same name in pkg. r.mu
// must be held.
//
// Takes packagePath (string) which is the import path.
// Takes pkg (*types.Package) which is the pre-built package.
//
// Returns error wrapping ErrTypesPackageLayout at the first disagreement, naming the
// type, the field and both sides.
func (r *SymbolRegistry) checkTypesPackageLayoutLocked(packagePath string, pkg *types.Package) error {
	if pkg == nil || layoutSizes == nil {
		return nil
	}
	exports, ok := r.symbols[packagePath]
	if !ok {
		return nil
	}
	scope := pkg.Scope()
	for name, value := range exports {
		if problem := exportLayoutDisagreement(scope, name, value); problem != "" {
			return fmt.Errorf("%w: %s.%s: %s; regenerate the provider's types export data (for sdk/selfhost: make generate-selfhost-symbols)",
				ErrTypesPackageLayout, packagePath, name, problem)
		}
	}
	return nil
}

// lookup returns the remembered answer for a comparison.
//
// Takes key (layoutMemoKey) which names the comparison.
//
// Returns string which is the remembered answer.
// Returns bool which is false when the comparison has not been made.
//
// Concurrency: takes mu for reading.
func (m *layoutMemoTable) lookup(key layoutMemoKey) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	problem, ok := m.entries[key]
	return problem, ok
}

// remember records a comparison's answer unless one is already recorded.
//
// Takes key (layoutMemoKey) which names the comparison.
// Takes problem (string) which is its answer.
//
// Returns bool which is true when this call added the entry.
//
// Concurrency: takes mu for writing.
func (m *layoutMemoTable) remember(key layoutMemoKey, problem string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entries[key]; ok {
		return false
	}
	m.entries[key] = problem
	return true
}

// forget drops a comparison whose declaration is no longer reachable.
//
// Takes key (layoutMemoKey) which names the comparison.
//
// Concurrency: takes mu for writing.
func (m *layoutMemoTable) forget(key layoutMemoKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
}

// exportLayoutDisagreement compares one native export that registers a type with the
// go/types type of the same name.
//
// Takes scope (*types.Scope) which is the pre-built package's scope.
// Takes name (string) which is the export's name.
// Takes value (reflect.Value) which is the export, a typed nil pointer when it registers
// a type.
//
// Returns string which describes the disagreement, or "" when they agree or the export
// registers no non-generic type of that name.
func exportLayoutDisagreement(scope *types.Scope, name string, value reflect.Value) string {
	if !value.IsValid() || value.Type().Kind() != reflect.Pointer || !value.IsNil() {
		return ""
	}
	typeName, ok := scope.Lookup(name).(*types.TypeName)
	if !ok {
		return ""
	}
	if named, generic := typeName.Type().(*types.Named); generic && named.TypeParams().Len() > 0 {
		return ""
	}
	key := layoutMemoKey{typeName: weak.Make(typeName), native: value.Type().Elem()}
	if problem, ok := layoutMemo.lookup(key); ok {
		return problem
	}
	problem := layoutDisagreement(typeName.Type(), key.native, 0)
	if layoutMemo.remember(key, problem) {
		runtime.AddCleanup(typeName, layoutMemo.forget, key)
	}
	return problem
}

// layoutDisagreement compares a go/types layout with a reflect layout.
//
// Takes typ (types.Type) which is the go/types side.
// Takes native (reflect.Type) which is the native side.
// Takes depth (int) which counts the inline levels descended.
//
// Returns string which describes the first disagreement, or "" when they agree.
func layoutDisagreement(typ types.Type, native reflect.Type, depth int) (problem string) {
	defer func() {
		if recover() != nil {
			problem = ""
		}
	}()
	underlying := typ.Underlying()
	if kind := layoutKind(underlying); kind != native.Kind() {
		return fmt.Sprintf("kind %s in the export data, %s natively", kind, native.Kind())
	}
	if depth < maxLayoutDepth {
		if problem := compositeLayoutDisagreement(underlying, native, depth); problem != "" {
			return problem
		}
	}
	if size := layoutSizes.Sizeof(typ); size != safeconv.UintptrToInt64(native.Size()) {
		return fmt.Sprintf("size %d in the export data, %d natively", size, native.Size())
	}
	return ""
}

// compositeLayoutDisagreement is layoutDisagreement's comparison of what a struct, an
// array, a pointer or a slice holds, for two types of the same kind.
//
// Takes underlying (types.Type) which is the go/types side's underlying type.
// Takes native (reflect.Type) which is the native side.
// Takes depth (int) which counts the inline levels descended.
//
// Returns string which describes the first disagreement, or "" when they agree.
func compositeLayoutDisagreement(underlying types.Type, native reflect.Type, depth int) string {
	switch t := underlying.(type) {
	case *types.Struct:
		return structLayoutDisagreement(t, native, depth)
	case *types.Array:
		if t.Len() != int64(native.Len()) {
			return fmt.Sprintf("array length %d in the export data, %d natively", t.Len(), native.Len())
		}
		if problem := layoutDisagreement(t.Elem(), native.Elem(), depth+1); problem != "" {
			return "element: " + problem
		}
	case *types.Pointer:
		return elementKindDisagreement(t.Elem(), native.Elem())
	case *types.Slice:
		return elementKindDisagreement(t.Elem(), native.Elem())
	default:
	}
	return ""
}

// structLayoutDisagreement is layoutDisagreement for a struct.
//
// Takes structType (*types.Struct) which is the go/types side.
// Takes native (reflect.Type) which is the native struct.
// Takes depth (int) which counts the inline levels descended.
//
// Returns string which describes the first disagreement, or "" when they agree.
func structLayoutDisagreement(structType *types.Struct, native reflect.Type, depth int) string {
	if structType.NumFields() != native.NumField() {
		return fmt.Sprintf("%d fields in the export data, %d natively", structType.NumFields(), native.NumField())
	}
	fields := make([]*types.Var, structType.NumFields())
	for index := range fields {
		fields[index] = structType.Field(index)
	}
	offsets := layoutSizes.Offsetsof(fields)
	for index, field := range fields {
		nativeField := native.Field(index)
		if field.Name() != nativeField.Name || field.Embedded() != nativeField.Anonymous {
			return fmt.Sprintf("field %d is %s in the export data, %s natively", index, describeField(field.Name(), field.Embedded()), describeField(nativeField.Name, nativeField.Anonymous))
		}
		if offsets[index] != safeconv.UintptrToInt64(nativeField.Offset) {
			return fmt.Sprintf("field %d (%s) at offset %d in the export data, %d natively", index, field.Name(), offsets[index], nativeField.Offset)
		}
		if problem := layoutDisagreement(field.Type(), nativeField.Type, depth+1); problem != "" {
			return fmt.Sprintf("field %d (%s): %s", index, field.Name(), problem)
		}
	}
	return ""
}

// elementKindDisagreement compares the kinds of the element types behind a pointer or
// slice.
//
// Takes elem (types.Type) which is the go/types element.
// Takes native (reflect.Type) which is the native element.
//
// Returns string which describes a disagreement, or "".
func elementKindDisagreement(elem types.Type, native reflect.Type) string {
	if kind := layoutKind(elem.Underlying()); kind != native.Kind() {
		return fmt.Sprintf("element kind %s in the export data, %s natively", kind, native.Kind())
	}
	return ""
}

// describeField names a field for an error.
//
// Takes name (string) which is the field's name.
// Takes embedded (bool) which is true for an embedded field.
//
// Returns string which is the description.
func describeField(name string, embedded bool) string {
	if embedded {
		return "embedded " + name
	}
	return name
}

// layoutKind is the reflect kind a go/types underlying type is laid out as.
//
// Takes typ (types.Type) which is an underlying type.
//
// Returns reflect.Kind which is the kind, or reflect.Invalid.
func layoutKind(typ types.Type) reflect.Kind {
	switch t := typ.(type) {
	case *types.Basic:
		for kind, basic := range reflectKindToBasicType {
			if basic == t.Kind() {
				return kind
			}
		}
		return reflect.Invalid
	case *types.Pointer:
		return reflect.Pointer
	case *types.Slice:
		return reflect.Slice
	case *types.Array:
		return reflect.Array
	case *types.Map:
		return reflect.Map
	case *types.Chan:
		return reflect.Chan
	case *types.Signature:
		return reflect.Func
	case *types.Interface:
		return reflect.Interface
	case *types.Struct:
		return reflect.Struct
	default:
		return reflect.Invalid
	}
}
