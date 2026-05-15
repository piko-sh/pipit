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

package typemodel

import (
	"reflect"
)

// NamedInterfaceWrapper wraps reflect.Type so user-declared named interface types report
// the source-level identity Go's reflect cannot preserve natively. Native reflect
// functions that type-assert to *rtype (such as reflect.PointerTo) will panic if passed a
// wrapper directly.
type NamedInterfaceWrapper struct {
	reflect.Type

	// pipit carries the source-level type identity overriding the embedded reflect.Type for
	// identity-bearing methods.
	pipit *Type
}

// NewNamedInterfaceWrapper builds a wrapper around nativeRType.
//
// The source-level identity is described by pipit. nativeRType is usually
// reflect.TypeFor[any]() for the bare interface case, or a reflect.PointerTo /
// reflect.SliceOf / etc. for composite shapes.
//
// Takes nativeRType (reflect.Type) which is the underlying reflect type.
// Takes pipit (*Type) which carries source-level identity.
//
// Returns NamedInterfaceWrapper which combines both.
func NewNamedInterfaceWrapper(nativeRType reflect.Type, pipit *Type) NamedInterfaceWrapper {
	return NamedInterfaceWrapper{Type: nativeRType, pipit: pipit}
}

// Name returns the source-level bare name of the wrapped type.
//
// For pointer/slice/etc. wrappers the bare name is empty (Go convention); for the inner
// interface it is e.g. "myiface".
//
// Returns string which is the bare name.
func (w NamedInterfaceWrapper) Name() string {
	if w.pipit == nil {
		return w.Type.Name()
	}
	return w.pipit.name()
}

// String returns the package-qualified source-level rendering.
//
// Examples: "*main.myiface" or "main.myiface".
//
// Returns string which is the rendered name.
func (w NamedInterfaceWrapper) String() string {
	if w.pipit == nil {
		return w.Type.String()
	}
	return w.pipit.String()
}

// PkgPath returns the full import path of the defining package.
//
// Returns string which is the import path.
func (w NamedInterfaceWrapper) PkgPath() string {
	if w.pipit == nil {
		return w.Type.PkgPath()
	}
	return w.pipit.PkgPath()
}

// Elem returns the element type for Pointer/Slice/Array/Chan/Map.
//
// Re-wrapped with the corresponding Type so chained introspection like
// `t.Elem().String()` reports the source name.
//
// Returns reflect.Type which is the wrapped element type.
func (w NamedInterfaceWrapper) Elem() reflect.Type {
	if w.pipit == nil || w.pipit.Elem() == nil {
		return w.Type.Elem()
	}
	return NewNamedInterfaceWrapper(w.Type.Elem(), w.pipit.Elem())
}

// Key returns the map key type, re-wrapped via Type.Key.
//
// Returns reflect.Type which is the wrapped key type.
func (w NamedInterfaceWrapper) Key() reflect.Type {
	if w.pipit == nil || w.pipit.Key() == nil {
		return w.Type.Key()
	}
	return NewNamedInterfaceWrapper(w.Type.Key(), w.pipit.Key())
}

// NumMethod returns the source-level method count.
//
// For interfaces the count is the size of Type.methodNames; for non-interface wrappers it
// delegates to the embedded reflect.Type.
//
// Returns int which is the method count.
func (w NamedInterfaceWrapper) NumMethod() int {
	if w.pipit == nil {
		return w.Type.NumMethod()
	}
	return w.pipit.numMethod()
}

// Method returns the i-th method record.
//
// For interfaces, synthesises from Type.methodNames so user code observes the same method
// names native Go would report.
//
// Takes i (int) which is the method index.
//
// Returns reflect.Method which is the method record.
func (w NamedInterfaceWrapper) Method(i int) reflect.Method {
	if w.pipit == nil {
		return w.Type.Method(i)
	}
	return w.pipit.method(i)
}

// MethodByName looks up a method by name.
//
// Interface lookup walks Type.methodNames; non-interface lookup delegates to the embedded
// reflect.Type.
//
// Takes name (string) which is the method name.
//
// Returns reflect.Method which is the resolved method record.
// Returns bool which is true when the method exists.
func (w NamedInterfaceWrapper) MethodByName(name string) (reflect.Method, bool) {
	if w.pipit == nil {
		return w.Type.MethodByName(name)
	}
	return w.pipit.methodByName(name)
}

// Implements reports whether the wrapped type satisfies u.
//
// Computed pipit-side when both are wrappers (or u is wrappable); falls back to native
// reflect.Implements otherwise.
//
// Takes u (reflect.Type) which is the target interface type.
//
// Returns bool which is true when the wrapped type satisfies u.
func (w NamedInterfaceWrapper) Implements(u reflect.Type) bool {
	if w.pipit == nil {
		return w.Type.Implements(u)
	}
	if other, ok := unwrapPipitNamedInterfaceWrapper(u); ok && other.pipit != nil {
		return w.pipit.implements(other.pipit)
	}
	if u.Kind() == reflect.Interface && u.NumMethod() == 0 {
		return true
	}
	return w.Type.Implements(u)
}

// AssignableTo reports whether a value of the wrapped type is assignable.
//
// Same delegation rules as Implements apply.
//
// Takes u (reflect.Type) which is the target type.
//
// Returns bool which is true when the wrapped type is assignable to u.
func (w NamedInterfaceWrapper) AssignableTo(u reflect.Type) bool {
	if w.pipit == nil {
		return w.Type.AssignableTo(u)
	}
	if other, ok := unwrapPipitNamedInterfaceWrapper(u); ok && other.pipit != nil {
		return w.pipit.assignableTo(other.pipit)
	}
	return w.Type.AssignableTo(u)
}

// ConvertibleTo reports whether a value of the wrapped type is convertible.
//
// Takes u (reflect.Type) which is the target type.
//
// Returns bool which is true when the wrapped type is convertible to u.
func (w NamedInterfaceWrapper) ConvertibleTo(u reflect.Type) bool {
	if w.pipit == nil {
		return w.Type.ConvertibleTo(u)
	}
	if other, ok := unwrapPipitNamedInterfaceWrapper(u); ok && other.pipit != nil {
		return w.pipit.convertibleTo(other.pipit)
	}
	return w.Type.ConvertibleTo(u)
}

// unwrapPipitNamedInterfaceWrapper returns the wrapper when t holds one.
//
// Used at the native-call boundary to strip the wrapper before passing into reflect
// functions that internally type-assert to *rtype.
//
// Takes t (reflect.Type) which is the type to unwrap.
//
// Returns NamedInterfaceWrapper which is the unwrapped wrapper, or the zero value when t
// is not a wrapper.
// Returns bool which is true when t is a wrapper.
func unwrapPipitNamedInterfaceWrapper(t reflect.Type) (NamedInterfaceWrapper, bool) {
	w, ok := t.(NamedInterfaceWrapper)
	return w, ok
}

// nativeReflectTypeFromWrapper returns the underlying reflect.Type.
//
// Passes t through unchanged when t is not a wrapper. Use at any boundary that feeds a
// reflect.Type back into native reflect functions (PointerTo, SliceOf, MakeMap, New,
// Zero, etc.).
//
// Takes t (reflect.Type) which may or may not be a wrapper.
//
// Returns reflect.Type which is the underlying native type, or t itself when t is not a
// wrapper.
func nativeReflectTypeFromWrapper(t reflect.Type) reflect.Type {
	if w, ok := unwrapPipitNamedInterfaceWrapper(t); ok {
		return w.Type
	}
	return t
}
